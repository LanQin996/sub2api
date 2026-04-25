package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

func TestApplyUserDashboardDerivedStatsComputesCacheHitRates(t *testing.T) {
	stats := &usagestats.UserDashboardStats{
		TodayInputTokens:         100,
		TodayCacheCreationTokens: 50,
		TodayCacheReadTokens:     50,
		TotalInputTokens:         300,
		TotalCacheCreationTokens: 100,
		TotalCacheReadTokens:     100,
	}

	applyUserDashboardDerivedStats(stats)

	if got, want := stats.TodayCacheHitRate, 0.25; got != want {
		t.Fatalf("TodayCacheHitRate = %v, want %v", got, want)
	}
	if got, want := stats.TotalCacheHitRate, 0.2; got != want {
		t.Fatalf("TotalCacheHitRate = %v, want %v", got, want)
	}
}

func TestApplyUserDashboardDerivedStatsHandlesZeroTokens(t *testing.T) {
	stats := &usagestats.UserDashboardStats{}

	applyUserDashboardDerivedStats(stats)

	if stats.TodayCacheHitRate != 0 {
		t.Fatalf("TodayCacheHitRate = %v, want 0", stats.TodayCacheHitRate)
	}
	if stats.TotalCacheHitRate != 0 {
		t.Fatalf("TotalCacheHitRate = %v, want 0", stats.TotalCacheHitRate)
	}
}
