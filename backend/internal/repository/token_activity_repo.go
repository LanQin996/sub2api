package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GetTokenActivityWatermark returns the last server-local date materialized by
// the once-daily snapshot job.
func (r *dashboardAggregationRepository) GetTokenActivityWatermark(ctx context.Context) (time.Time, error) {
	var day time.Time
	rows, err := r.sql.QueryContext(ctx, `
		SELECT last_processed_date
		FROM user_token_activity_job_state
		WHERE id = 1
	`)
	if err != nil {
		return day, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return day, err
		}
		return day, sql.ErrNoRows
	}
	return day, rows.Scan(&day)
}

// AggregateTokenActivityDay atomically replaces one settled local day. Only
// users with usage in that day receive a row.
func (r *dashboardAggregationRepository) AggregateTokenActivityDay(ctx context.Context, day, start, end time.Time) error {
	dayKey := day.Format("2006-01-02")
	_, err := r.sql.ExecContext(ctx, `
		WITH deleted AS (
			DELETE FROM user_token_activity_daily
			WHERE activity_date = $1::date
			RETURNING user_id
		)
		INSERT INTO user_token_activity_daily (
			user_id, activity_date, input_tokens, output_tokens,
			cache_creation_tokens, cache_read_tokens, total_tokens, refreshed_at
		)
		SELECT
			user_id,
			$1::date,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(input_tokens::bigint + output_tokens::bigint + cache_creation_tokens::bigint + cache_read_tokens::bigint), 0),
			NOW()
		FROM usage_logs
		WHERE created_at >= $2 AND created_at < $3
		GROUP BY user_id
	`, dayKey, start, end)
	return err
}

func (r *dashboardAggregationRepository) UpdateTokenActivityWatermark(ctx context.Context, day time.Time) error {
	_, err := r.sql.ExecContext(ctx, `
		INSERT INTO user_token_activity_job_state (id, last_processed_date, updated_at)
		VALUES (1, $1::date, NOW())
		ON CONFLICT (id) DO UPDATE SET
			last_processed_date = EXCLUDED.last_processed_date,
			updated_at = EXCLUDED.updated_at
	`, day.Format("2006-01-02"))
	return err
}

func (r *dashboardAggregationRepository) CleanupTokenActivity(ctx context.Context, cutoff time.Time) error {
	_, err := r.sql.ExecContext(ctx, `DELETE FROM user_token_activity_daily WHERE activity_date < $1::date`, cutoff.Format("2006-01-02"))
	return err
}

// GetUserTokenActivity reads only the materialized snapshot table.
func (r *usageLogRepository) GetUserTokenActivity(ctx context.Context, userID int64, startDate, endDate time.Time) ([]usagestats.TokenActivityDay, time.Time, *time.Time, error) {
	stateRows, err := r.sql.QueryContext(ctx, `
		SELECT last_processed_date, updated_at
		FROM user_token_activity_job_state
		WHERE id = 1
	`)
	if err != nil {
		return nil, time.Time{}, nil, err
	}
	var dataThrough time.Time
	var updatedAt time.Time
	if !stateRows.Next() {
		_ = stateRows.Close()
		if err := stateRows.Err(); err != nil {
			return nil, time.Time{}, nil, err
		}
		return nil, time.Time{}, nil, sql.ErrNoRows
	}
	if err := stateRows.Scan(&dataThrough, &updatedAt); err != nil {
		_ = stateRows.Close()
		return nil, time.Time{}, nil, err
	}
	if err := stateRows.Close(); err != nil {
		return nil, time.Time{}, nil, err
	}
	if dataThrough.Format("2006-01-02") < endDate.Format("2006-01-02") {
		endDate = dataThrough
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT activity_date, total_tokens
		FROM user_token_activity_daily
		WHERE user_id = $1
		  AND activity_date >= $2::date
		  AND activity_date <= $3::date
		ORDER BY activity_date ASC
	`, userID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, time.Time{}, nil, err
	}
	defer rows.Close()

	days := make([]usagestats.TokenActivityDay, 0, 370)
	for rows.Next() {
		var day time.Time
		var point usagestats.TokenActivityDay
		if err := rows.Scan(&day, &point.TotalTokens); err != nil {
			return nil, time.Time{}, nil, err
		}
		point.Date = day.Format("2006-01-02")
		days = append(days, point)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, nil, err
	}
	return days, dataThrough, &updatedAt, nil
}
