package handler

import (
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var usageRankingCache = newSnapshotCache(30 * time.Second)

func buildUsageRankingCacheKey(userID int64, period string, startTime time.Time, limit int) string {
	keyRaw, _ := json.Marshal(struct {
		UserID int64  `json:"user_id"`
		Period string `json:"period"`
		Start  string `json:"start"`
		Limit  int    `json:"limit"`
	}{
		UserID: userID,
		Period: period,
		Start:  startTime.UTC().Format(time.RFC3339),
		Limit:  limit,
	})
	return string(keyRaw)
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
