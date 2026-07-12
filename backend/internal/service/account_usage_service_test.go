package service

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

type accountUsageCodexProbeRepo struct {
	stubOpenAIAccountRepo
	updateExtraCh chan map[string]any
	rateLimitCh   chan time.Time
}

func (r *accountUsageCodexProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.updateExtraCh != nil {
		copied := make(map[string]any, len(updates))
		for k, v := range updates {
			copied[k] = v
		}
		r.updateExtraCh <- copied
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	if r.rateLimitCh != nil {
		r.rateLimitCh <- resetAt
	}
	return nil
}

func TestBoundedAccountUsageCacheTTLBoundary(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cache := &boundedAccountUsageCache{maxEntries: 2}
	cache.storeAt(1, "cached", time.Minute, base)

	if got, ok := cache.loadAt(1, base.Add(time.Minute-time.Nanosecond)); !ok || got != "cached" {
		t.Fatalf("entry before TTL boundary = (%v, %v), want (cached, true)", got, ok)
	}
	if got, ok := cache.loadAt(1, base.Add(time.Minute)); ok || got != nil {
		t.Fatalf("entry at TTL boundary = (%v, %v), want (nil, false)", got, ok)
	}
	if got := cache.Len(); got != 0 {
		t.Fatalf("cache length after expired load = %d, want 0", got)
	}
}

func TestBoundedAccountUsageCacheCapacityAndReplacement(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cache := &boundedAccountUsageCache{maxEntries: 2}
	cache.storeAt(1, "first", time.Hour, base)
	cache.storeAt(2, "second", time.Hour, base)
	cache.storeAt(1, "updated", time.Hour, base)

	if got := cache.Len(); got != 2 {
		t.Fatalf("cache length after replacement = %d, want 2", got)
	}
	if got, ok := cache.loadAt(1, base); !ok || got != "updated" {
		t.Fatalf("replacement entry = (%v, %v), want (updated, true)", got, ok)
	}
	if _, ok := cache.loadAt(2, base); !ok {
		t.Fatal("replacing an existing key must not evict another entry")
	}

	cache.storeAt(3, "third", time.Hour, base)
	if got := cache.Len(); got != 2 {
		t.Fatalf("cache length above capacity = %d, want 2", got)
	}
}

func TestBoundedAccountUsageCacheDefaultCapacity(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cache := &boundedAccountUsageCache{}
	for key := int64(1); key <= accountUsageCacheMaxEntries+1; key++ {
		cache.storeAt(key, key, time.Hour, base)
	}

	if got := cache.Len(); got != accountUsageCacheMaxEntries {
		t.Fatalf("default cache length = %d, want %d", got, accountUsageCacheMaxEntries)
	}
}

func TestBoundedAccountUsageCachePrefersExpiredEntriesAtCapacity(t *testing.T) {
	t.Parallel()

	const maxEntries = accountUsageCacheCleanupBatch * 2
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cache := &boundedAccountUsageCache{maxEntries: maxEntries}
	cache.storeAt(1, "expired", time.Minute, base)
	for key := int64(2); key <= maxEntries; key++ {
		cache.storeAt(key, key, time.Hour, base)
	}

	now := base.Add(2 * time.Minute)
	cache.storeAt(maxEntries+1, "new", time.Hour, now)

	if got := cache.Len(); got != maxEntries {
		t.Fatalf("cache length after capacity cleanup = %d, want %d", got, maxEntries)
	}
	if _, ok := cache.loadAt(1, now); ok {
		t.Fatal("expired entry should be removed at the capacity boundary")
	}
	for key := int64(2); key <= maxEntries; key++ {
		if _, ok := cache.loadAt(key, now); !ok {
			t.Fatalf("live entry %d was evicted while an expired entry existed", key)
		}
	}
	if got, ok := cache.loadAt(maxEntries+1, now); !ok || got != "new" {
		t.Fatalf("new entry = (%v, %v), want (new, true)", got, ok)
	}
}

func TestBoundedAccountUsageCacheConcurrentAccessStaysBounded(t *testing.T) {
	t.Parallel()

	const (
		maxEntries = 64
		workers    = 16
		iterations = 200
	)
	cache := &boundedAccountUsageCache{maxEntries: maxEntries}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := int64(worker*iterations + i)
				cache.Store(key, key, time.Minute)
				cache.Load(key)
			}
		}()
	}
	wg.Wait()

	if got := cache.Len(); got > maxEntries {
		t.Fatalf("cache length after concurrent writes = %d, want <= %d", got, maxEntries)
	}
}

func TestBoundedAccountUsageCacheNonPositiveTTLInvalidatesEntry(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cache := &boundedAccountUsageCache{maxEntries: 2}
	cache.storeAt(1, "cached", time.Minute, base)
	cache.storeAt(1, "ignored", 0, base)

	if got, ok := cache.loadAt(1, base); ok || got != nil {
		t.Fatalf("entry after non-positive TTL = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestAccountUsageServiceShouldProbeOpenAICodexSnapshotUsesTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc := &AccountUsageService{cache: NewUsageCache()}
	if !svc.shouldProbeOpenAICodexSnapshot(42, now) {
		t.Fatal("first probe should be allowed")
	}
	if svc.shouldProbeOpenAICodexSnapshot(42, now.Add(openAIProbeCacheTTL-time.Nanosecond)) {
		t.Fatal("probe before TTL expiry should be suppressed")
	}
	if !svc.shouldProbeOpenAICodexSnapshot(42, now.Add(openAIProbeCacheTTL)) {
		t.Fatal("probe at TTL expiry should be allowed")
	}
	if !svc.shouldProbeOpenAICodexSnapshot(42, now.Add(openAIProbeCacheTTL+time.Second), true) {
		t.Fatal("forced probe should bypass the cache")
	}
	if svc.shouldProbeOpenAICodexSnapshot(42, now.Add(openAIProbeCacheTTL+time.Second)) {
		t.Fatal("forced probe should refresh the cache timestamp")
	}
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited account to force codex snapshot refresh")
	}

	if shouldRefreshOpenAICodexSnapshot(&Account{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !shouldRefreshOpenAICodexSnapshot(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

// TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2 外审第9轮 P1:spark 影子用量走
// QueryUsage(/wham/usage,与 WSv2 无关),staleness 不得被 WSv2 门控,否则首刷后窗口永久冻结。
func TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}
	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	freshAt := now.Add(-time.Minute).Format(time.RFC3339)
	parentID := int64(7001)

	// 影子无 WSv2,但首刷后窗口已存在;过期 codex_usage_updated_at 必须触发再刷新。
	shadowStale := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": staleAt},
	}
	if !shouldRefreshOpenAICodexSnapshot(shadowStale, usage, now) {
		t.Fatal("expected stale spark shadow (no WSv2) to trigger refresh")
	}

	// 影子时间戳仍新鲜→不刷(TTL 生效)。
	shadowFresh := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": freshAt},
	}
	if shouldRefreshOpenAICodexSnapshot(shadowFresh, usage, now) {
		t.Fatal("expected fresh spark shadow to skip refresh (TTL not elapsed)")
	}

	// 反向对照:普通账号无 WSv2 + 过期时间戳→仍不刷(WSv2 门控普通账号的 probe 刷新)。
	normalNoWS := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_usage_updated_at": staleAt},
	}
	if shouldRefreshOpenAICodexSnapshot(normalNoWS, usage, now) {
		t.Fatal("expected non-WSv2 normal account to skip codex probe refresh")
	}
}

func TestExtractOpenAICodexProbeUpdatesAccepts429WithCodexHeaders(t *testing.T) {
	t.Parallel()

	headers := make(http.Header)
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
	if err != nil {
		t.Fatalf("extractOpenAICodexProbeUpdates() error = %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected codex probe updates from 429 headers")
	}
	if got := updates["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestAccountUsageService_PersistOpenAICodexProbeSnapshotOnlyUpdatesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	svc.persistOpenAICodexProbeSnapshot(321, map[string]any{
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
	})

	select {
	case updates := <-repo.updateExtraCh:
		if got := updates["codex_7d_used_percent"]; got != 100.0 {
			t.Fatalf("codex_7d_used_percent = %v, want 100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 codex 探测快照写入 extra 超时")
	}

	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将探测快照写入运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetOpenAIUsage_DoesNotPromoteCodexExtraToRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		rateLimitCh: make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	usage, err := svc.getOpenAIUsage(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getOpenAIUsage() error = %v", err)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 100.0 {
		t.Fatalf("预期 7 天用量仍然可见，实际为 %#v", usage.SevenDay)
	}
	if account.RateLimitResetAt != nil {
		t.Fatalf("不应让已耗尽的 codex extra 改写运行时限流状态: %v", account.RateLimitResetAt)
	}
	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将已耗尽的 codex extra 持久化为运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBuildCodexUsageProgressFromExtra_ZerosExpiredWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)

	t.Run("expired 5h window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     "2026-03-16T10:00:00Z", // 2h ago
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired window, got %v", progress.Utilization)
		}
		if progress.RemainingSeconds != 0 {
			t.Fatalf("expected RemainingSeconds=0, got %v", progress.RemainingSeconds)
		}
	})

	t.Run("active 5h window keeps utilization", func(t *testing.T) {
		resetAt := now.Add(2 * time.Hour).Format(time.RFC3339)
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     resetAt,
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 42.0 {
			t.Fatalf("expected Utilization=42, got %v", progress.Utilization)
		}
	})

	t.Run("expired 7d window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_7d_used_percent": 88.0,
			"codex_7d_reset_at":     "2026-03-15T00:00:00Z", // yesterday
		}
		progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired 7d window, got %v", progress.Utilization)
		}
	})
}
