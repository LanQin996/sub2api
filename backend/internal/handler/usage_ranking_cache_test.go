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
	spendingCalls atomic.Int32
	tokenCalls    atomic.Int32
}

func (r *userRankingRepoCacheProbe) GetPublicUserSpendingRanking(ctx context.Context, startTime, endTime time.Time, currentUserID int64, limit int) (*usagestats.PublicUserSpendingRankingResponse, error) {
	r.spendingCalls.Add(1)
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

func (r *userRankingRepoCacheProbe) GetPublicUserTokenRanking(ctx context.Context, startTime, endTime time.Time, currentUserID int64, limit int) (*usagestats.PublicUserTokenRankingResponse, error) {
	r.tokenCalls.Add(1)
	return &usagestats.PublicUserTokenRankingResponse{
		Ranking: []usagestats.PublicUserTokenRankingItem{
			{
				Rank:     1,
				UserID:   currentUserID,
				Email:    "current@example.com",
				Username: "Current",
				Requests: 8,
				Tokens:   800,
			},
			{
				Rank:     2,
				UserID:   99,
				Email:    "other@example.com",
				Username: "Other",
				Requests: 5,
				Tokens:   500,
			},
		},
		TotalRequests: 13,
		TotalTokens:   1300,
	}, nil
}

func resetUsageRankingCacheForTest() {
	usageRankingCache = newSnapshotCache(5 * time.Minute)
	usageModelRankingCache = newSnapshotCache(5 * time.Minute)
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
	router.GET("/usage/ranking/tokens", handler.TokenRanking)
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
	require.Equal(t, int32(1), repo.spendingCalls.Load())
	require.Contains(t, rec2.Body.String(), "\"display_name\":\"Current\"")
	require.NotContains(t, rec2.Body.String(), "actual_cost")
	require.NotContains(t, rec2.Body.String(), "total_actual_cost")
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
	require.Equal(t, int32(2), repo.spendingCalls.Load())
}

func TestUsageTokenRankingUsesCacheAndSeparateKey(t *testing.T) {
	t.Cleanup(resetUsageRankingCacheForTest)
	resetUsageRankingCacheForTest()

	repo := &userRankingRepoCacheProbe{}
	router := newUserRankingCacheTestRouter(42, repo)

	req1 := httptest.NewRequest(http.MethodGet, "/usage/ranking/tokens?period=daily&limit=10", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/usage/ranking/tokens?period=daily&limit=10", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.tokenCalls.Load())
	require.Equal(t, int32(0), repo.spendingCalls.Load())
	require.Contains(t, rec2.Body.String(), "\"display_name\":\"Current\"")
	require.NotContains(t, rec2.Body.String(), "actual_cost")
	require.NotContains(t, rec2.Body.String(), "total_actual_cost")

	req3 := httptest.NewRequest(http.MethodGet, "/usage/ranking?period=daily&limit=10", nil)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
	require.Equal(t, "miss", rec3.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.spendingCalls.Load())
}

type modelRankingRepoCacheProbe struct {
	service.UsageLogRepository
	calls atomic.Int32
}

func (r *modelRankingRepoCacheProbe) GetModelUsageRanking(ctx context.Context, currentStart, currentEnd, previousStart, previousEnd time.Time, limit int) (*usagestats.ModelUsageRankingResponse, error) {
	r.calls.Add(1)
	previousRank := int64(2)
	return &usagestats.ModelUsageRankingResponse{
		Models: []usagestats.ModelUsageRankingItem{
			{
				Rank:         1,
				PreviousRank: &previousRank,
				RankDelta:    1,
				ModelName:    "gpt-5.4",
				Vendor:       "OpenAI",
				VendorIcon:   "OpenAI",
				TotalTokens:  100,
				Requests:     2,
				Share:        1,
				GrowthPct:    100,
			},
		},
		Vendors: []usagestats.VendorUsageRankingItem{
			{
				Rank:        1,
				Vendor:      "OpenAI",
				VendorIcon:  "OpenAI",
				TotalTokens: 100,
				Requests:    2,
				Share:       1,
				GrowthPct:   100,
				ModelsCount: 1,
				TopModel:    "gpt-5.4",
			},
		},
		TotalTokens:   100,
		TotalRequests: 2,
		TotalModels:   1,
		TotalVendors:  1,
	}, nil
}

func newModelRankingCacheTestRouter(repo *modelRankingRepoCacheProbe) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.GET("/usage/ranking/models", handler.ModelRanking)
	return router
}

func TestUsageModelRankingUsesCache(t *testing.T) {
	t.Cleanup(resetUsageRankingCacheForTest)
	resetUsageRankingCacheForTest()

	repo := &modelRankingRepoCacheProbe{}
	router := newModelRankingCacheTestRouter(repo)

	req1 := httptest.NewRequest(http.MethodGet, "/usage/ranking/models?period=weekly&limit=20&timezone=Asia%2FShanghai", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/usage/ranking/models?period=weekly&limit=20&timezone=Asia%2FShanghai", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.calls.Load())
	require.Contains(t, rec2.Body.String(), "\"model_name\":\"gpt-5.4\"")
}
