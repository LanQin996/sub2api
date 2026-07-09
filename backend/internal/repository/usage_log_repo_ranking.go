package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// PublicUserSpendingRankingItem represents a privacy-safe user spending ranking row.
type PublicUserSpendingRankingItem = usagestats.PublicUserSpendingRankingItem
type PublicUserSpendingRankingResponse = usagestats.PublicUserSpendingRankingResponse
type PublicUserTokenRankingItem = usagestats.PublicUserTokenRankingItem
type PublicUserTokenRankingResponse = usagestats.PublicUserTokenRankingResponse

func (r *usageLogRepository) SumActualCostByUser(ctx context.Context, userID int64) (float64, error) {
	if r == nil || r.sql == nil {
		return 0, errors.New("usage log repository is not configured")
	}
	var total float64
	err := scanSingleRow(ctx, r.sql, `
		SELECT COALESCE(SUM(actual_cost), 0)
		FROM usage_logs
		WHERE user_id = $1 AND actual_cost > 0
	`, []any{userID}, &total)
	if err != nil {
		return 0, err
	}
	return total, nil
}

const userSpendingRollupCTE = `
		bounds AS (
			SELECT
				$1::timestamptz AS start_time,
				$2::timestamptz AS end_time
		),
		daily_buckets AS (
			SELECT
				coverage.bucket_date,
				coverage.bucket_date::timestamp AT TIME ZONE 'UTC' AS range_start,
				(coverage.bucket_date::timestamp + interval '1 day') AT TIME ZONE 'UTC' AS range_end
			FROM usage_user_daily_spending_coverage coverage
			CROSS JOIN bounds
			WHERE coverage.bucket_date::timestamp AT TIME ZONE 'UTC' >= bounds.start_time
			  AND (coverage.bucket_date::timestamp + interval '1 day') AT TIME ZONE 'UTC' <= bounds.end_time
		),
		daily_rollup AS (
			SELECT
				spending.user_id,
				COALESCE(SUM(spending.actual_cost), 0) AS actual_cost,
				COALESCE(SUM(spending.requests), 0)::bigint AS requests,
				COALESCE(SUM(
					spending.input_tokens +
					spending.output_tokens +
					spending.cache_creation_tokens +
					spending.cache_read_tokens
				), 0)::bigint AS tokens
			FROM usage_user_daily_spending spending
			JOIN daily_buckets buckets ON buckets.bucket_date = spending.bucket_date
			GROUP BY spending.user_id
		),
		hourly_buckets AS (
			SELECT
				coverage.bucket_start,
				coverage.bucket_start AS range_start,
				coverage.bucket_start + interval '1 hour' AS range_end
			FROM usage_user_hourly_spending_coverage coverage
			CROSS JOIN bounds
			WHERE coverage.bucket_start >= bounds.start_time
			  AND coverage.bucket_start + interval '1 hour' <= bounds.end_time
			  AND NOT EXISTS (
				SELECT 1
				FROM daily_buckets daily
				WHERE coverage.bucket_start >= daily.range_start
				  AND coverage.bucket_start < daily.range_end
			  )
		),
		hourly_rollup AS (
			SELECT
				spending.user_id,
				COALESCE(SUM(spending.actual_cost), 0) AS actual_cost,
				COALESCE(SUM(spending.requests), 0)::bigint AS requests,
				COALESCE(SUM(
					spending.input_tokens +
					spending.output_tokens +
					spending.cache_creation_tokens +
					spending.cache_read_tokens
				), 0)::bigint AS tokens
			FROM usage_user_hourly_spending spending
			JOIN hourly_buckets buckets ON buckets.bucket_start = spending.bucket_start
			GROUP BY spending.user_id
		),
		raw_spend AS (
			SELECT
				logs.user_id,
				COALESCE(SUM(logs.actual_cost), 0) AS actual_cost,
				COUNT(*)::bigint AS requests,
				COALESCE(SUM(
					logs.input_tokens +
					logs.output_tokens +
					logs.cache_creation_tokens +
					logs.cache_read_tokens
				), 0)::bigint AS tokens
			FROM usage_logs logs
			CROSS JOIN bounds
			WHERE logs.created_at >= bounds.start_time
			  AND logs.created_at < bounds.end_time
			  AND NOT EXISTS (
				SELECT 1
				FROM daily_buckets daily
				WHERE logs.created_at >= daily.range_start
				  AND logs.created_at < daily.range_end
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM hourly_buckets hourly
				WHERE logs.created_at >= hourly.range_start
				  AND logs.created_at < hourly.range_end
			  )
			GROUP BY logs.user_id
		),
		user_spend AS (
			SELECT
				spend.user_id,
				COALESCE(SUM(spend.actual_cost), 0) AS actual_cost,
				COALESCE(SUM(spend.requests), 0)::bigint AS requests,
				COALESCE(SUM(spend.tokens), 0)::bigint AS tokens
			FROM (
				SELECT user_id, actual_cost, requests, tokens FROM daily_rollup
				UNION ALL
				SELECT user_id, actual_cost, requests, tokens FROM hourly_rollup
				UNION ALL
				SELECT user_id, actual_cost, requests, tokens FROM raw_spend
			) spend
			GROUP BY spend.user_id
		)
`

// GetPublicUserSpendingRanking returns privacy-safe ranking rows plus the current user's row.
func (r *usageLogRepository) GetPublicUserSpendingRanking(ctx context.Context, startTime, endTime time.Time, currentUserID int64, limit int) (result *PublicUserSpendingRankingResponse, err error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		WITH ` + userSpendingRollupCTE + `,
		top_ranked AS (
			SELECT
				ROW_NUMBER() OVER (ORDER BY top_spend.actual_cost DESC, top_spend.tokens DESC, top_spend.user_id ASC) AS rank,
				top_spend.user_id,
				COALESCE(users.email, '') AS email,
				COALESCE(users.username, '') AS username,
				COALESCE(user_avatars.url, '') AS avatar_url,
				top_spend.actual_cost,
				top_spend.requests,
				top_spend.tokens
			FROM (
				SELECT user_id, actual_cost, requests, tokens
				FROM user_spend
				ORDER BY actual_cost DESC, tokens DESC, user_id ASC
				LIMIT $3
			) top_spend
			LEFT JOIN users ON top_spend.user_id = users.id
			LEFT JOIN user_avatars ON top_spend.user_id = user_avatars.user_id
		),
		current_ranked AS (
			SELECT
				(
					SELECT COUNT(*) + 1
					FROM user_spend better
					WHERE better.actual_cost > current_spend.actual_cost
					   OR (
						better.actual_cost = current_spend.actual_cost
						AND (
							better.tokens > current_spend.tokens
							OR (better.tokens = current_spend.tokens AND better.user_id < current_spend.user_id)
						)
					   )
				)::bigint AS rank,
				current_spend.user_id,
				COALESCE(users.email, '') AS email,
				COALESCE(users.username, '') AS username,
				COALESCE(user_avatars.url, '') AS avatar_url,
				current_spend.actual_cost,
				current_spend.requests,
				current_spend.tokens
			FROM user_spend current_spend
			LEFT JOIN users ON current_spend.user_id = users.id
			LEFT JOIN user_avatars ON current_spend.user_id = user_avatars.user_id
			WHERE current_spend.user_id = $4
		),
		selected AS (
			SELECT * FROM top_ranked
			UNION ALL
			SELECT *
			FROM current_ranked
			WHERE NOT EXISTS (
				SELECT 1
				FROM top_ranked
				WHERE top_ranked.user_id = current_ranked.user_id
			)
		),
		totals AS (
			SELECT
				COALESCE(SUM(actual_cost), 0) AS total_actual_cost,
				COALESCE(SUM(requests), 0)::bigint AS total_requests,
				COALESCE(SUM(tokens), 0)::bigint AS total_tokens
			FROM user_spend
		)
		SELECT
			selected.rank,
			selected.user_id,
			selected.email,
			selected.username,
			selected.avatar_url,
			selected.actual_cost,
			selected.requests,
			selected.tokens,
			totals.total_actual_cost,
			totals.total_requests,
			totals.total_tokens
		FROM selected
		CROSS JOIN totals
		ORDER BY selected.rank ASC
	`

	rows, err := r.sql.QueryContext(ctx, query, startTime, endTime, limit, currentUserID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	ranking := make([]PublicUserSpendingRankingItem, 0)
	totalActualCost := 0.0
	totalRequests := int64(0)
	totalTokens := int64(0)
	for rows.Next() {
		var row PublicUserSpendingRankingItem
		if err = rows.Scan(
			&row.Rank,
			&row.UserID,
			&row.Email,
			&row.Username,
			&row.AvatarURL,
			&row.ActualCost,
			&row.Requests,
			&row.Tokens,
			&totalActualCost,
			&totalRequests,
			&totalTokens,
		); err != nil {
			return nil, err
		}
		row.IsCurrentUser = row.UserID == currentUserID
		ranking = append(ranking, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &PublicUserSpendingRankingResponse{
		Ranking:         ranking,
		TotalActualCost: totalActualCost,
		TotalRequests:   totalRequests,
		TotalTokens:     totalTokens,
	}, nil
}

// GetPublicUserTokenRanking returns privacy-safe user token ranking rows plus the current user's row.
func (r *usageLogRepository) GetPublicUserTokenRanking(ctx context.Context, startTime, endTime time.Time, currentUserID int64, limit int) (result *PublicUserTokenRankingResponse, err error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		WITH ` + userSpendingRollupCTE + `,
		top_ranked AS (
			SELECT
				ROW_NUMBER() OVER (ORDER BY top_spend.tokens DESC, top_spend.requests DESC, top_spend.user_id ASC) AS rank,
				top_spend.user_id,
				COALESCE(users.email, '') AS email,
				COALESCE(users.username, '') AS username,
				COALESCE(user_avatars.url, '') AS avatar_url,
				top_spend.requests,
				top_spend.tokens
			FROM (
				SELECT user_id, requests, tokens
				FROM user_spend
				ORDER BY tokens DESC, requests DESC, user_id ASC
				LIMIT $3
			) top_spend
			LEFT JOIN users ON top_spend.user_id = users.id
			LEFT JOIN user_avatars ON top_spend.user_id = user_avatars.user_id
		),
		current_ranked AS (
			SELECT
				(
					SELECT COUNT(*) + 1
					FROM user_spend better
					WHERE better.tokens > current_spend.tokens
					   OR (
						better.tokens = current_spend.tokens
						AND (
							better.requests > current_spend.requests
							OR (better.requests = current_spend.requests AND better.user_id < current_spend.user_id)
						)
					   )
				)::bigint AS rank,
				current_spend.user_id,
				COALESCE(users.email, '') AS email,
				COALESCE(users.username, '') AS username,
				COALESCE(user_avatars.url, '') AS avatar_url,
				current_spend.requests,
				current_spend.tokens
			FROM user_spend current_spend
			LEFT JOIN users ON current_spend.user_id = users.id
			LEFT JOIN user_avatars ON current_spend.user_id = user_avatars.user_id
			WHERE current_spend.user_id = $4
		),
		selected AS (
			SELECT * FROM top_ranked
			UNION ALL
			SELECT *
			FROM current_ranked
			WHERE NOT EXISTS (
				SELECT 1
				FROM top_ranked
				WHERE top_ranked.user_id = current_ranked.user_id
			)
		),
		totals AS (
			SELECT
				COALESCE(SUM(user_spend.requests), 0)::bigint AS total_requests,
				COALESCE(SUM(user_spend.tokens), 0)::bigint AS total_tokens
			FROM user_spend
		)
		SELECT
			selected.rank,
			selected.user_id,
			selected.email,
			selected.username,
			selected.avatar_url,
			selected.requests,
			selected.tokens,
			totals.total_requests,
			totals.total_tokens
		FROM selected
		CROSS JOIN totals
		ORDER BY selected.rank ASC
	`

	rows, err := r.sql.QueryContext(ctx, query, startTime, endTime, limit, currentUserID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	ranking := make([]PublicUserTokenRankingItem, 0)
	totalRequests := int64(0)
	totalTokens := int64(0)
	for rows.Next() {
		var row PublicUserTokenRankingItem
		if err = rows.Scan(
			&row.Rank,
			&row.UserID,
			&row.Email,
			&row.Username,
			&row.AvatarURL,
			&row.Requests,
			&row.Tokens,
			&totalRequests,
			&totalTokens,
		); err != nil {
			return nil, err
		}
		row.IsCurrentUser = row.UserID == currentUserID
		ranking = append(ranking, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &PublicUserTokenRankingResponse{
		Ranking:       ranking,
		TotalRequests: totalRequests,
		TotalTokens:   totalTokens,
	}, nil
}

type modelUsageRankingRow struct {
	ModelName      string
	Vendor         string
	VendorIcon     string
	TotalTokens    int64
	Requests       int64
	PreviousTokens int64
	PreviousRank   *int64
}

func modelRankingVendorSQLExpr() string {
	modelLower := "LOWER(model_name)"
	platformLower := "LOWER(platform)"
	expr := `
		CASE
			WHEN {{platform}} = 'openai' OR {{model}} ~ '(^|[/_-])(gpt|o[0-9]|text-|dall-e|whisper|tts-|codex)' THEN 'OpenAI'
			WHEN {{platform}} IN ('anthropic', 'claude') OR {{model}} LIKE '%%claude%%' THEN 'Anthropic'
			WHEN {{platform}} IN ('gemini', 'antigravity', 'google') OR {{model}} LIKE '%%gemini%%' OR {{model}} LIKE '%%gemma%%' OR {{model}} LIKE '%%imagen%%' OR {{model}} LIKE '%%veo%%' THEN 'Google'
			WHEN {{model}} LIKE '%%deepseek%%' THEN 'DeepSeek'
			WHEN {{model}} LIKE '%%qwen%%' OR {{model}} LIKE '%%qwq%%' THEN 'Qwen'
			WHEN {{model}} LIKE '%%glm%%' OR {{model}} LIKE '%%chatglm%%' THEN 'Zhipu'
			WHEN {{model}} LIKE '%%mistral%%' OR {{model}} LIKE '%%mixtral%%' OR {{model}} LIKE '%%codestral%%' THEN 'Mistral AI'
			WHEN {{model}} LIKE '%%llama%%' OR {{model}} LIKE '%%meta%%' THEN 'Meta'
			WHEN {{model}} LIKE '%%grok%%' OR {{model}} LIKE '%%xai%%' THEN 'xAI'
			WHEN {{model}} LIKE '%%moonshot%%' OR {{model}} LIKE '%%kimi%%' THEN 'Moonshot AI'
			WHEN {{model}} LIKE '%%doubao%%' THEN 'Doubao'
			ELSE 'Unknown'
		END`
	return strings.NewReplacer("{{platform}}", platformLower, "{{model}}", modelLower).Replace(expr)
}

func modelRankingVendorIconSQLExpr(vendorExpr string) string {
	return fmt.Sprintf(`
		CASE %s
			WHEN 'OpenAI' THEN 'OpenAI'
			WHEN 'Anthropic' THEN 'Claude'
			WHEN 'Google' THEN 'Gemini'
			WHEN 'DeepSeek' THEN 'DeepSeek'
			WHEN 'Qwen' THEN 'Qwen'
			WHEN 'Zhipu' THEN 'Zhipu'
			WHEN 'Mistral AI' THEN 'Mistral'
			WHEN 'Meta' THEN 'Meta'
			WHEN 'xAI' THEN 'xAI'
			WHEN 'Moonshot AI' THEN 'Moonshot'
			WHEN 'Doubao' THEN 'Doubao'
			ELSE ''
		END`, vendorExpr)
}

func modelUsageRankingCTE() string {
	modelExpr := resolveModelDimensionExpression(usagestats.ModelSourceRequested)
	vendorExpr := modelRankingVendorSQLExpr()
	vendorIconExpr := modelRankingVendorIconSQLExpr("cbm.vendor")
	return fmt.Sprintf(`
		bounds AS (
			SELECT
				$1::timestamptz AS current_start,
				$2::timestamptz AS current_end,
				$3::timestamptz AS previous_start,
				$4::timestamptz AS previous_end
		),
		raw_usage AS (
			SELECT
				CASE
					WHEN ul.created_at >= b.current_start AND ul.created_at < b.current_end THEN 'current'
					ELSE 'previous'
				END AS usage_period,
				TRIM(%s)::text AS model_name,
				COALESCE(NULLIF(g.platform, ''), a.platform, '') AS platform,
				COUNT(*)::bigint AS requests,
				COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0)::bigint AS total_tokens
			FROM usage_logs ul
			CROSS JOIN bounds b
			LEFT JOIN groups g ON g.id = ul.group_id
			LEFT JOIN accounts a ON a.id = ul.account_id
			WHERE ul.created_at >= LEAST(b.current_start, b.previous_start)
			  AND ul.created_at < b.current_end
			GROUP BY 1, 2, 3
		),
		current_usage AS (
			SELECT
				model_name,
				platform,
				requests,
				total_tokens
			FROM raw_usage
			WHERE usage_period = 'current'
		),
		previous_usage AS (
			SELECT
				model_name,
				platform,
				total_tokens
			FROM raw_usage
			WHERE usage_period = 'previous'
		),
		current_by_model AS (
			SELECT
				model_name,
				%s AS vendor,
				SUM(requests)::bigint AS requests,
				SUM(total_tokens)::bigint AS total_tokens
			FROM current_usage
			WHERE model_name <> ''
			GROUP BY model_name, %s
		),
		previous_by_model AS (
			SELECT
				model_name,
				%s AS vendor,
				SUM(total_tokens)::bigint AS total_tokens
			FROM previous_usage
			WHERE model_name <> ''
			GROUP BY model_name, %s
		),
		previous_ranked AS (
			SELECT
				model_name,
				vendor,
				total_tokens,
				ROW_NUMBER() OVER (ORDER BY total_tokens DESC, model_name ASC)::bigint AS previous_rank
			FROM previous_by_model
			WHERE total_tokens > 0
		),
		ranked AS (
			SELECT
				ROW_NUMBER() OVER (ORDER BY cbm.total_tokens DESC, cbm.model_name ASC)::bigint AS rank,
				cbm.model_name,
				cbm.vendor,
				%s AS vendor_icon,
				cbm.total_tokens AS model_tokens,
				cbm.requests,
				COALESCE(pr.total_tokens, 0)::bigint AS previous_tokens,
				pr.previous_rank,
				COALESCE(SUM(cbm.total_tokens) OVER (), 0)::bigint AS all_tokens,
				COALESCE(SUM(cbm.requests) OVER (), 0)::bigint AS total_requests,
				COUNT(*) OVER ()::bigint AS total_models
			FROM current_by_model cbm
			LEFT JOIN previous_ranked pr ON pr.model_name = cbm.model_name AND pr.vendor = cbm.vendor
			WHERE cbm.total_tokens > 0
		)`, modelExpr, vendorExpr, vendorExpr, vendorExpr, vendorExpr, vendorIconExpr)
}

// GetModelUsageRanking returns model and vendor leaderboards based on token usage.
func (r *usageLogRepository) GetModelUsageRanking(ctx context.Context, currentStart, currentEnd, previousStart, previousEnd time.Time, limit int) (result *usagestats.ModelUsageRankingResponse, err error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		WITH ` + modelUsageRankingCTE() + `,
		model_rows AS (
			SELECT
				'model'::text AS row_type,
				rank,
				model_name,
				vendor,
				vendor_icon,
				model_tokens,
				requests,
				NULL::bigint AS models_count,
				NULL::text AS top_model,
				previous_tokens,
				previous_rank,
				all_tokens,
				total_requests,
				total_models
			FROM ranked
			WHERE rank <= $5
		),
		vendor_current AS (
			SELECT
				vendor,
				MAX(` + modelRankingVendorIconSQLExpr("vendor") + `) AS vendor_icon,
				SUM(total_tokens)::bigint AS total_tokens,
				SUM(requests)::bigint AS requests,
				COUNT(*)::bigint AS models_count,
				(ARRAY_AGG(model_name ORDER BY total_tokens DESC, model_name ASC))[1] AS top_model,
				COALESCE(SUM(SUM(total_tokens)) OVER (), 0)::bigint AS all_tokens
			FROM current_by_model
			WHERE total_tokens > 0
			GROUP BY vendor
		),
		vendor_previous AS (
			SELECT
				vendor,
				SUM(total_tokens)::bigint AS total_tokens
			FROM previous_by_model
			WHERE total_tokens > 0
			GROUP BY vendor
		),
		vendor_rows AS (
			SELECT
				'vendor'::text AS row_type,
				ROW_NUMBER() OVER (ORDER BY vc.total_tokens DESC, vc.vendor ASC)::bigint AS rank,
				NULL::text AS model_name,
				vc.vendor,
				vc.vendor_icon,
				vc.total_tokens AS model_tokens,
				vc.requests,
				vc.models_count,
				vc.top_model,
				COALESCE(vp.total_tokens, 0)::bigint AS previous_tokens,
				NULL::bigint AS previous_rank,
				vc.all_tokens,
				NULL::bigint AS total_requests,
				NULL::bigint AS total_models
			FROM vendor_current vc
			LEFT JOIN vendor_previous vp ON vp.vendor = vc.vendor
			ORDER BY vc.total_tokens DESC, vc.vendor ASC
			LIMIT $5
		)
		SELECT
			row_type,
			rank,
			model_name,
			vendor,
			vendor_icon,
			model_tokens,
			requests,
			models_count,
			top_model,
			previous_tokens,
			previous_rank,
			all_tokens,
			total_requests,
			total_models
		FROM model_rows
		UNION ALL
		SELECT
			row_type,
			rank,
			model_name,
			vendor,
			vendor_icon,
			model_tokens,
			requests,
			models_count,
			top_model,
			previous_tokens,
			previous_rank,
			all_tokens,
			total_requests,
			total_models
		FROM vendor_rows
		ORDER BY row_type ASC, rank ASC
	`

	rows, err := r.sql.QueryContext(ctx, query, currentStart, currentEnd, previousStart, previousEnd, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	rawModels := make([]modelUsageRankingRow, 0)
	vendors := make([]usagestats.VendorUsageRankingItem, 0)
	totalTokens := int64(0)
	totalRequests := int64(0)
	totalModels := int64(0)
	for rows.Next() {
		var rowType string
		var rank int64
		var modelName sql.NullString
		var vendor string
		var vendorIcon string
		var totalTokensForRow int64
		var requests int64
		var modelsCount sql.NullInt64
		var topModel sql.NullString
		var previousTokens int64
		var previousRank sql.NullInt64
		var allTokens int64
		var totalRequestsRow sql.NullInt64
		var totalModelsRow sql.NullInt64
		if err = rows.Scan(
			&rowType,
			&rank,
			&modelName,
			&vendor,
			&vendorIcon,
			&totalTokensForRow,
			&requests,
			&modelsCount,
			&topModel,
			&previousTokens,
			&previousRank,
			&allTokens,
			&totalRequestsRow,
			&totalModelsRow,
		); err != nil {
			return nil, err
		}
		switch rowType {
		case "model":
			var previousRankPtr *int64
			if previousRank.Valid {
				value := previousRank.Int64
				previousRankPtr = &value
			}
			if totalTokens == 0 {
				totalTokens = allTokens
			}
			if totalRequests == 0 && totalRequestsRow.Valid {
				totalRequests = totalRequestsRow.Int64
			}
			if totalModels == 0 && totalModelsRow.Valid {
				totalModels = totalModelsRow.Int64
			}
			rawModels = append(rawModels, modelUsageRankingRow{
				ModelName:      modelName.String,
				Vendor:         vendor,
				VendorIcon:     vendorIcon,
				TotalTokens:    totalTokensForRow,
				Requests:       requests,
				PreviousTokens: previousTokens,
				PreviousRank:   previousRankPtr,
			})
		case "vendor":
			item := usagestats.VendorUsageRankingItem{
				Rank:        rank,
				Vendor:      normalizeRankingVendor(vendor),
				VendorIcon:  vendorIcon,
				TotalTokens: totalTokensForRow,
				Requests:    requests,
				ModelsCount: modelsCount.Int64,
				TopModel:    topModel.String,
				GrowthPct:   growthPercentage(totalTokensForRow, previousTokens),
			}
			if allTokens > 0 {
				item.Share = float64(totalTokensForRow) / float64(allTokens)
			}
			vendors = append(vendors, item)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	models := make([]usagestats.ModelUsageRankingItem, 0, len(rawModels))
	for i, row := range rawModels {
		rank := int64(i + 1)
		rankDelta := int64(0)
		if row.PreviousRank != nil {
			rankDelta = *row.PreviousRank - rank
		}
		share := 0.0
		if totalTokens > 0 {
			share = float64(row.TotalTokens) / float64(totalTokens)
		}
		models = append(models, usagestats.ModelUsageRankingItem{
			Rank:         rank,
			PreviousRank: row.PreviousRank,
			RankDelta:    rankDelta,
			ModelName:    row.ModelName,
			Vendor:       normalizeRankingVendor(row.Vendor),
			VendorIcon:   row.VendorIcon,
			Category:     "all",
			TotalTokens:  row.TotalTokens,
			Requests:     row.Requests,
			Share:        share,
			GrowthPct:    growthPercentage(row.TotalTokens, row.PreviousTokens),
		})
	}

	topMovers, topDroppers := buildModelRankingMovers(models, 5)

	return &usagestats.ModelUsageRankingResponse{
		Models:        models,
		Vendors:       vendors,
		TopMovers:     topMovers,
		TopDroppers:   topDroppers,
		TotalTokens:   totalTokens,
		TotalRequests: totalRequests,
		TotalModels:   totalModels,
		TotalVendors:  int64(len(vendors)),
	}, nil
}

func (r *usageLogRepository) getVendorUsageRanking(ctx context.Context, currentStart, currentEnd, previousStart, previousEnd time.Time, limit int) (result []usagestats.VendorUsageRankingItem, err error) {
	query := `
		WITH ` + modelUsageRankingCTE() + `,
		vendor_current AS (
			SELECT
				vendor,
				MAX(` + modelRankingVendorIconSQLExpr("vendor") + `) AS vendor_icon,
				SUM(total_tokens)::bigint AS total_tokens,
				SUM(requests)::bigint AS requests,
				COUNT(*)::bigint AS models_count,
				(ARRAY_AGG(model_name ORDER BY total_tokens DESC, model_name ASC))[1] AS top_model,
				COALESCE(SUM(SUM(total_tokens)) OVER (), 0)::bigint AS all_tokens
			FROM current_by_model
			WHERE total_tokens > 0
			GROUP BY vendor
		),
		vendor_previous AS (
			SELECT
				vendor,
				SUM(total_tokens)::bigint AS total_tokens
			FROM previous_by_model
			WHERE total_tokens > 0
			GROUP BY vendor
		)
		SELECT
			ROW_NUMBER() OVER (ORDER BY vc.total_tokens DESC, vc.vendor ASC)::bigint AS rank,
			vc.vendor,
			vc.vendor_icon,
			vc.total_tokens,
			vc.requests,
			vc.models_count,
			vc.top_model,
			COALESCE(vp.total_tokens, 0)::bigint AS previous_tokens,
			vc.all_tokens
		FROM vendor_current vc
		LEFT JOIN vendor_previous vp ON vp.vendor = vc.vendor
		ORDER BY vc.total_tokens DESC, vc.vendor ASC
		LIMIT $5
	`

	rows, err := r.sql.QueryContext(ctx, query, currentStart, currentEnd, previousStart, previousEnd, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	items := make([]usagestats.VendorUsageRankingItem, 0)
	for rows.Next() {
		var item usagestats.VendorUsageRankingItem
		var previousTokens int64
		var allTokens int64
		if err = rows.Scan(
			&item.Rank,
			&item.Vendor,
			&item.VendorIcon,
			&item.TotalTokens,
			&item.Requests,
			&item.ModelsCount,
			&item.TopModel,
			&previousTokens,
			&allTokens,
		); err != nil {
			return nil, err
		}
		item.Vendor = normalizeRankingVendor(item.Vendor)
		if allTokens > 0 {
			item.Share = float64(item.TotalTokens) / float64(allTokens)
		}
		item.GrowthPct = growthPercentage(item.TotalTokens, previousTokens)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func normalizeRankingVendor(vendor string) string {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		return "Unknown"
	}
	return vendor
}

func growthPercentage(current, previous int64) float64 {
	if previous <= 0 {
		if current > 0 {
			return 100
		}
		return 0
	}
	return (float64(current-previous) / float64(previous)) * 100
}

func buildModelRankingMovers(models []usagestats.ModelUsageRankingItem, limit int) ([]usagestats.ModelRankingMover, []usagestats.ModelRankingMover) {
	movers := make([]usagestats.ModelRankingMover, 0)
	droppers := make([]usagestats.ModelRankingMover, 0)
	for _, model := range models {
		if model.RankDelta == 0 {
			continue
		}
		item := usagestats.ModelRankingMover{
			ModelName:   model.ModelName,
			Vendor:      model.Vendor,
			VendorIcon:  model.VendorIcon,
			RankDelta:   model.RankDelta,
			CurrentRank: model.Rank,
			GrowthPct:   model.GrowthPct,
			TotalTokens: model.TotalTokens,
		}
		if model.RankDelta > 0 {
			movers = append(movers, item)
		} else {
			droppers = append(droppers, item)
		}
	}
	sortRankingMovers := func(items []usagestats.ModelRankingMover, positive bool) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].RankDelta != items[j].RankDelta {
				if positive {
					return items[i].RankDelta > items[j].RankDelta
				}
				return items[i].RankDelta < items[j].RankDelta
			}
			return items[i].TotalTokens > items[j].TotalTokens
		})
	}
	sortRankingMovers(movers, true)
	sortRankingMovers(droppers, false)
	if len(movers) > limit {
		movers = movers[:limit]
	}
	if len(droppers) > limit {
		droppers = droppers[:limit]
	}
	return movers, droppers
}
