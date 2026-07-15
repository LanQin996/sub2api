package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type tokenActivityRepoTestStub struct {
	dashboardAggregationRepoTestStub
	activityWatermark       time.Time
	activityDays            []string
	activityWatermarkDays   []string
	activityCleanupCutoff   time.Time
	activityAggregateErrDay string
}

func (s *tokenActivityRepoTestStub) GetTokenActivityWatermark(context.Context) (time.Time, error) {
	return s.activityWatermark, nil
}

func (s *tokenActivityRepoTestStub) AggregateTokenActivityDay(_ context.Context, day, _, _ time.Time) error {
	key := day.Format("2006-01-02")
	if key == s.activityAggregateErrDay {
		return errors.New("aggregate failed")
	}
	s.activityDays = append(s.activityDays, key)
	return nil
}

func (s *tokenActivityRepoTestStub) UpdateTokenActivityWatermark(_ context.Context, day time.Time) error {
	s.activityWatermarkDays = append(s.activityWatermarkDays, day.Format("2006-01-02"))
	return nil
}

func (s *tokenActivityRepoTestStub) CleanupTokenActivity(_ context.Context, cutoff time.Time) error {
	s.activityCleanupCutoff = cutoff
	return nil
}

func TestBuildTokenActivityResponse_MissingDaysBreakStreak(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, loc)
	through := time.Date(2026, 7, 5, 0, 0, 0, 0, loc)
	days := []usagestats.TokenActivityDay{
		{Date: "2026-07-01", TotalTokens: 10},
		{Date: "2026-07-02", TotalTokens: 20},
		{Date: "2026-07-04", TotalTokens: 30},
		{Date: "2026-07-05", TotalTokens: 40},
	}

	result := buildTokenActivityResponse(days, start, through.AddDate(0, 0, 1), through, "UTC", nil)

	require.Equal(t, int64(100), result.Summary.TotalTokens)
	require.Equal(t, 2, result.Summary.CurrentStreakDays)
	require.Equal(t, 2, result.Summary.LongestStreakDays)
}

func TestDashboardAggregationService_TokenActivityCatchesUpSettledDays(t *testing.T) {
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init("UTC") })
	repo := &tokenActivityRepoTestStub{activityWatermark: time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)}
	svc := &DashboardAggregationService{
		repo:       repo,
		instanceID: "test",
		nowFn: func() time.Time {
			return time.Date(2026, 7, 14, 3, 5, 0, 0, timezone.Location())
		},
	}

	svc.runTokenActivitySnapshot()

	require.Equal(t, []string{"2026-07-12", "2026-07-13"}, repo.activityDays)
	require.Equal(t, repo.activityDays, repo.activityWatermarkDays)
	require.Equal(t, "2025-07-09", repo.activityCleanupCutoff.Format("2006-01-02"))
}

func TestDashboardAggregationService_TokenActivityFailureDoesNotAdvanceFailedDay(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	repo := &tokenActivityRepoTestStub{
		activityWatermark:       time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC),
		activityAggregateErrDay: "2026-07-13",
	}
	svc := &DashboardAggregationService{
		repo:       repo,
		instanceID: "test",
		nowFn:      func() time.Time { return time.Date(2026, 7, 14, 3, 5, 0, 0, time.UTC) },
	}

	svc.runTokenActivitySnapshot()

	require.Equal(t, []string{"2026-07-12"}, repo.activityWatermarkDays)
	require.True(t, repo.activityCleanupCutoff.IsZero())
}

func TestDashboardAggregationService_TokenActivityWaitsUntilThree(t *testing.T) {
	require.NoError(t, timezone.Init("UTC"))
	repo := &tokenActivityRepoTestStub{activityWatermark: time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)}
	svc := &DashboardAggregationService{
		repo:  repo,
		nowFn: func() time.Time { return time.Date(2026, 7, 14, 2, 59, 0, 0, time.UTC) },
	}

	svc.runTokenActivitySnapshot()
	require.Empty(t, repo.activityDays)
}
