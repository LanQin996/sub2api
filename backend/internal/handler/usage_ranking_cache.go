package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/redis/go-redis/v9"
)

const usageRankingRedisTTL = 5 * time.Minute

var (
	usageRankingCache      = newSnapshotCache(5 * time.Minute)
	usageModelRankingCache = newSnapshotCache(5 * time.Minute)
)

func buildUsageRankingCacheKey(kind string, userID int64, period string, startTime time.Time, limit int) string {
	keyRaw, _ := json.Marshal(struct {
		Kind   string `json:"kind"`
		UserID int64  `json:"user_id"`
		Period string `json:"period"`
		Start  string `json:"start"`
		Limit  int    `json:"limit"`
	}{
		Kind:   kind,
		UserID: userID,
		Period: period,
		Start:  startTime.UTC().Format(time.RFC3339),
		Limit:  limit,
	})
	return string(keyRaw)
}

func buildUsageModelRankingCacheKey(period string, startTime time.Time, limit int) string {
	keyRaw, _ := json.Marshal(struct {
		Kind   string `json:"kind"`
		Period string `json:"period"`
		Start  string `json:"start"`
		Limit  int    `json:"limit"`
	}{
		Kind:   "model",
		Period: period,
		Start:  startTime.UTC().Format(time.RFC3339),
		Limit:  limit,
	})
	return string(keyRaw)
}

func usageRankingRedisKey(cacheKey string) string {
	return "usage:ranking:v1:" + cacheKey
}

func getUsageRankingCached[T any](
	ctx context.Context,
	localCache *snapshotCache,
	redisClient *redis.Client,
	cacheKey string,
) (*T, string, bool) {
	if entry, ok := localCache.Get(cacheKey); ok {
		if ranking, ok := entry.Payload.(*T); ok {
			return ranking, "hit", true
		}
	}
	ranking, ok := getUsageRankingFromRedis[T](ctx, redisClient, cacheKey)
	if !ok {
		return nil, "", false
	}
	if localCache != nil {
		localCache.Set(cacheKey, ranking)
	}
	return ranking, "redis-hit", true
}

func getUsageRankingFromRedis[T any](ctx context.Context, redisClient *redis.Client, cacheKey string) (*T, bool) {
	if redisClient == nil || cacheKey == "" {
		return nil, false
	}
	raw, err := redisClient.Get(ctx, usageRankingRedisKey(cacheKey)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("usage ranking redis get failed", "err", err)
		}
		return nil, false
	}
	var ranking T
	if err := json.Unmarshal(raw, &ranking); err != nil {
		slog.Warn("usage ranking redis unmarshal failed", "err", err)
		return nil, false
	}
	return &ranking, true
}

func setUsageRankingRedis(ctx context.Context, redisClient *redis.Client, cacheKey string, payload any) {
	if redisClient == nil || cacheKey == "" || payload == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("usage ranking redis marshal failed", "err", err)
		return
	}
	if err := redisClient.Set(ctx, usageRankingRedisKey(cacheKey), raw, usageRankingRedisTTL).Err(); err != nil {
		slog.Warn("usage ranking redis set failed", "err", err)
	}
}

func clonePublicUserSpendingRankingResponse(
	src *usagestats.PublicUserSpendingRankingResponse,
) *usagestats.PublicUserSpendingRankingResponse {
	if src == nil {
		src = &usagestats.PublicUserSpendingRankingResponse{}
	}
	cloned := *src
	if len(src.Ranking) > 0 {
		cloned.Ranking = append([]usagestats.PublicUserSpendingRankingItem(nil), src.Ranking...)
	}
	if src.CurrentUser != nil {
		currentUser := *src.CurrentUser
		cloned.CurrentUser = &currentUser
	}
	return &cloned
}

func clonePublicUserTokenRankingResponse(
	src *usagestats.PublicUserTokenRankingResponse,
) *usagestats.PublicUserTokenRankingResponse {
	if src == nil {
		src = &usagestats.PublicUserTokenRankingResponse{}
	}
	cloned := *src
	if len(src.Ranking) > 0 {
		cloned.Ranking = append([]usagestats.PublicUserTokenRankingItem(nil), src.Ranking...)
	}
	if src.CurrentUser != nil {
		currentUser := *src.CurrentUser
		cloned.CurrentUser = &currentUser
	}
	return &cloned
}

func cloneModelUsageRankingResponse(
	src *usagestats.ModelUsageRankingResponse,
) *usagestats.ModelUsageRankingResponse {
	if src == nil {
		src = &usagestats.ModelUsageRankingResponse{}
	}
	cloned := *src
	if len(src.Models) > 0 {
		cloned.Models = append([]usagestats.ModelUsageRankingItem(nil), src.Models...)
		for i := range cloned.Models {
			if cloned.Models[i].PreviousRank != nil {
				previousRank := *cloned.Models[i].PreviousRank
				cloned.Models[i].PreviousRank = &previousRank
			}
		}
	}
	if len(src.Vendors) > 0 {
		cloned.Vendors = append([]usagestats.VendorUsageRankingItem(nil), src.Vendors...)
	}
	if len(src.TopMovers) > 0 {
		cloned.TopMovers = append([]usagestats.ModelRankingMover(nil), src.TopMovers...)
	}
	if len(src.TopDroppers) > 0 {
		cloned.TopDroppers = append([]usagestats.ModelRankingMover(nil), src.TopDroppers...)
	}
	return &cloned
}
