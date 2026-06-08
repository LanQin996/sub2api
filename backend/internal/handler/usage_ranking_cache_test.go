package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userRankingRepoCacheProbe struct {
	service.UsageLogRepository
	calls atomic.Int32
}

func (r *userRankingRepoCacheProbe) GetPublicUserSpendingRanking(ctx context.Context, startTime, endTime time.Time, currentUserID int64, limit int) (*usagestats.PublicUserSpendingRankingResponse, error) {
	r.calls.Add(1)
	return &usagestats.PublicUserSpendingRankingResponse{
		Ranking: []usagestats.PublicUserSpendingRankingItem{
			{
				Rank:       1,
				UserID:     currentUserID,
				Email:      "current@example.com",
				Username:   "Current",
				ActualCost: 12.34,
				Requests:   7,
				Tokens:     70,
			},
			{
				Rank:       2,
				UserID:     99,
				Email:      "other@example.com",
				Username:   "Other",
				ActualCost: 10,
				Requests:   5,
				Tokens:     50,
			},
		},
		TotalActualCost: 22.34,
		TotalRequests:   12,
		TotalTokens:     120,
	}, nil
}

func resetUsageRankingCacheForTest() {
	usageRankingCache = newSnapshotCache(30 * time.Second)
}

func newUserRankingCacheTestRouter(userID int64, repo *userRankingRepoCacheProbe) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
		c.Next()
	})
	router.GET("/usage/ranking", handler.Ranking)
	return router
}

func TestUsageRankingUsesCache(t *testing.T) {
	t.Cleanup(resetUsageRankingCacheForTest)
	resetUsageRankingCacheForTest()

	repo := &userRankingRepoCacheProbe{}
	router := newUserRankingCacheTestRouter(42, repo)

	req1 := httptest.NewRequest(http.MethodGet, "/usage/ranking?period=daily&limit=10", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/usage/ranking?period=daily&limit=10", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.calls.Load())
	require.Contains(t, rec2.Body.String(), "\"display_name\":\"Current\"")
}

func TestUsageRankingCacheIsUserScoped(t *testing.T) {
	t.Cleanup(resetUsageRankingCacheForTest)
	resetUsageRankingCacheForTest()

	repo := &userRankingRepoCacheProbe{}

	router42 := newUserRankingCacheTestRouter(42, repo)
	req1 := httptest.NewRequest(http.MethodGet, "/usage/ranking?period=daily&limit=10", nil)
	rec1 := httptest.NewRecorder()
	router42.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	router43 := newUserRankingCacheTestRouter(43, repo)
	req2 := httptest.NewRequest(http.MethodGet, "/usage/ranking?period=daily&limit=10", nil)
	rec2 := httptest.NewRecorder()
	router43.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "miss", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(2), repo.calls.Load())
}
