package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type contentModerationTestSettingRepo struct {
	values           map[string]string
	getValueCalls    atomic.Int64
	getMultipleCalls atomic.Int64
	getValueStarted  chan struct{}
	getValueRelease  <-chan struct{}
	getValueOnce     sync.Once
	getValueErr      error
}

func (r *contentModerationTestSettingRepo) Get(ctx context.Context, key string) (*Setting, error) {
	if value, ok := r.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (r *contentModerationTestSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	r.getValueCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.getValueStarted != nil {
		r.getValueOnce.Do(func() { close(r.getValueStarted) })
	}
	if r.getValueRelease != nil {
		select {
		case <-r.getValueRelease:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if r.getValueErr != nil {
		return "", r.getValueErr
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *contentModerationTestSettingRepo) Set(ctx context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *contentModerationTestSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.getMultipleCalls.Add(1)
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *contentModerationTestSettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *contentModerationTestSettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *contentModerationTestSettingRepo) Delete(ctx context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func expireContentModerationConfigCache(svc *ContentModerationService) {
	entry := svc.configCache.Load()
	if entry == nil {
		return
	}
	expired := *entry
	expired.expiresAt = 0
	svc.configCache.Store(&expired)
}

func contentModerationConfigCacheExpiry(svc *ContentModerationService) time.Time {
	entry := svc.configCache.Load()
	if entry == nil {
		return time.Time{}
	}
	return time.Unix(0, entry.expiresAt)
}

func expireContentModerationRiskControlCache(svc *ContentModerationService) {
	snapshot := svc.riskControlCache.Load()
	if snapshot == nil {
		return
	}
	expired := *snapshot
	expired.expiresAt = 0
	svc.riskControlCache.Store(&expired)
}

type contentModerationTestRepo struct {
	mu            sync.Mutex
	logs          []ContentModerationLog
	createStarted chan struct{}
	createRelease <-chan struct{}
	createOnce    sync.Once
}

func (r *contentModerationTestRepo) CreateLog(ctx context.Context, log *ContentModerationLog) error {
	if r.createStarted != nil {
		r.createOnce.Do(func() { close(r.createStarted) })
	}
	if r.createRelease != nil {
		select {
		case <-r.createRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if log != nil {
		r.logs = append(r.logs, *log)
	}
	return nil
}

func (r *contentModerationTestRepo) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *contentModerationTestRepo) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, log := range r.logs {
		if log.UserID == nil || *log.UserID != userID || !log.Flagged || log.Action == ContentModerationActionHashBlock {
			continue
		}
		if excludeCyberPolicy && log.Action == ContentModerationActionCyberPolicy {
			continue
		}
		if log.CreatedAt.IsZero() || log.CreatedAt.Before(since) {
			continue
		}
		count++
	}
	return count, nil
}

func (r *contentModerationTestRepo) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*ContentModerationCleanupResult, error) {
	return &ContentModerationCleanupResult{}, nil
}

func (r *contentModerationTestRepo) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	return nil
}

func (r *contentModerationTestRepo) snapshotLogs() []ContentModerationLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ContentModerationLog, len(r.logs))
	copy(out, r.logs)
	return out
}

func requireContentModerationLogCount(t *testing.T, repo *contentModerationTestRepo, want int) []ContentModerationLog {
	t.Helper()
	var logs []ContentModerationLog
	require.Eventually(t, func() bool {
		logs = repo.snapshotLogs()
		return len(logs) == want
	}, time.Second, 10*time.Millisecond)
	return logs
}

func requireRecordedHashCount(t *testing.T, cache *contentModerationTestHashCache, want int) []string {
	t.Helper()
	var hashes []string
	require.Eventually(t, func() bool {
		hashes = cache.snapshotRecorded()
		return len(hashes) == want
	}, time.Second, 10*time.Millisecond)
	return hashes
}

type contentModerationTestHashCache struct {
	mu            sync.Mutex
	hashes        map[string]struct{}
	recorded      []string
	checked       []string
	deleted       []string
	hasResult     bool
	hasResultUsed bool
}

type contentModerationTestUserRepo struct {
	user    *User
	updated []User
}

func (r *contentModerationTestUserRepo) Create(ctx context.Context, user *User) error {
	panic("unexpected Create call")
}

func (r *contentModerationTestUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	if r.user == nil {
		return nil, ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *contentModerationTestUserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	panic("unexpected GetByEmail call")
}

func (r *contentModerationTestUserRepo) GetFirstAdmin(ctx context.Context) (*User, error) {
	panic("unexpected GetFirstAdmin call")
}

func (r *contentModerationTestUserRepo) Update(ctx context.Context, user *User) error {
	if user == nil {
		return nil
	}
	clone := *user
	r.updated = append(r.updated, clone)
	r.user = &clone
	return nil
}

func (r *contentModerationTestUserRepo) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}

func (r *contentModerationTestUserRepo) GetUserAvatar(ctx context.Context, userID int64) (*UserAvatar, error) {
	panic("unexpected GetUserAvatar call")
}

func (r *contentModerationTestUserRepo) UpsertUserAvatar(ctx context.Context, userID int64, input UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}

func (r *contentModerationTestUserRepo) DeleteUserAvatar(ctx context.Context, userID int64) error {
	panic("unexpected DeleteUserAvatar call")
}

func (r *contentModerationTestUserRepo) List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *contentModerationTestUserRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (r *contentModerationTestUserRepo) GetLatestUsedAtByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserIDs call")
}

func (r *contentModerationTestUserRepo) GetLatestUsedAtByUserID(ctx context.Context, userID int64) (*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserID call")
}

func (r *contentModerationTestUserRepo) UpdateUserLastActiveAt(ctx context.Context, userID int64, activeAt time.Time) error {
	panic("unexpected UpdateUserLastActiveAt call")
}

func (r *contentModerationTestUserRepo) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected UpdateBalance call")
}

func (r *contentModerationTestUserRepo) DeductBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected DeductBalance call")
}

func (r *contentModerationTestUserRepo) UpdateConcurrency(ctx context.Context, id int64, amount int) error {
	panic("unexpected UpdateConcurrency call")
}

func (r *contentModerationTestUserRepo) BatchSetConcurrency(ctx context.Context, userIDs []int64, value int) (int, error) {
	panic("unexpected BatchSetConcurrency call")
}

func (r *contentModerationTestUserRepo) BatchAddConcurrency(ctx context.Context, userIDs []int64, delta int) (int, error) {
	panic("unexpected BatchAddConcurrency call")
}
func (r *contentModerationTestUserRepo) BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error) {
	panic("unexpected BatchUpdateLimits call")
}

func (r *contentModerationTestUserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	panic("unexpected ExistsByEmail call")
}

func (r *contentModerationTestUserRepo) RemoveGroupFromAllowedGroups(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected RemoveGroupFromAllowedGroups call")
}

func (r *contentModerationTestUserRepo) AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	panic("unexpected AddGroupToAllowedGroups call")
}

func (r *contentModerationTestUserRepo) RemoveGroupFromUserAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	panic("unexpected RemoveGroupFromUserAllowedGroups call")
}

func (r *contentModerationTestUserRepo) ListUserAuthIdentities(ctx context.Context, userID int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected ListUserAuthIdentities call")
}

func (r *contentModerationTestUserRepo) UnbindUserAuthProvider(ctx context.Context, userID int64, provider string) error {
	panic("unexpected UnbindUserAuthProvider call")
}

func (r *contentModerationTestUserRepo) UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error {
	panic("unexpected UpdateTotpSecret call")
}

func (r *contentModerationTestUserRepo) EnableTotp(ctx context.Context, userID int64) error {
	panic("unexpected EnableTotp call")
}

func (r *contentModerationTestUserRepo) DisableTotp(ctx context.Context, userID int64) error {
	panic("unexpected DisableTotp call")
}

func (r *contentModerationTestUserRepo) GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	return r.GetByID(ctx, id)
}

type contentModerationTestAuthCacheInvalidator struct {
	userIDs []int64
}

func (i *contentModerationTestAuthCacheInvalidator) InvalidateAuthCacheByKey(ctx context.Context, key string) {
}

func (i *contentModerationTestAuthCacheInvalidator) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	i.userIDs = append(i.userIDs, userID)
}

func (i *contentModerationTestAuthCacheInvalidator) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
}

func (c *contentModerationTestHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hashes == nil {
		c.hashes = map[string]struct{}{}
	}
	c.hashes[inputHash] = struct{}{}
	c.recorded = append(c.recorded, inputHash)
	return nil
}

func (c *contentModerationTestHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = append(c.checked, inputHash)
	if c.hasResultUsed {
		return c.hasResult, nil
	}
	_, ok := c.hashes[inputHash]
	return ok, nil
}

func (c *contentModerationTestHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, inputHash)
	if c.hashes == nil {
		return false, nil
	}
	if _, ok := c.hashes[inputHash]; !ok {
		return false, nil
	}
	delete(c.hashes, inputHash)
	return true, nil
}

func (c *contentModerationTestHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	deleted := int64(len(c.hashes))
	c.hashes = map[string]struct{}{}
	return deleted, nil
}

func (c *contentModerationTestHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int64(len(c.hashes)), nil
}

func (c *contentModerationTestHashCache) snapshotRecorded() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.recorded))
	copy(out, c.recorded)
	return out
}

func (c *contentModerationTestHashCache) snapshotChecked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.checked))
	copy(out, c.checked)
	return out
}

func (c *contentModerationTestHashCache) hasHash(inputHash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.hashes[inputHash]
	return ok
}

func (c *contentModerationTestHashCache) snapshotDeleted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.deleted))
	copy(out, c.deleted)
	return out
}

func TestBuildContentModerationLog_RedactsInputExcerpt(t *testing.T) {
	svc := &ContentModerationService{}
	cfg := defaultContentModerationConfig()
	input := ContentModerationCheckInput{
		RequestID: "req-1",
		Endpoint:  "/v1/chat/completions",
		Provider:  "openai",
	}

	log := svc.buildLog(input, cfg, ContentModerationActionAllow, true, "sexual", 0.8, map[string]float64{"sexual": 0.8}, "hello sk-proj-1234567890abcdef", nil, nil, "")

	require.NotContains(t, log.InputExcerpt, "sk-proj-1234567890abcdef")
	require.Contains(t, log.InputExcerpt, "[已脱敏]")
}

func TestRedactContentModerationSecrets_LongHexAndTokens(t *testing.T) {
	input := "你哈市多大事cf5bbdc4cd508f3aaf0d2070d529d4a4ac29099f8ecc357f696df28e1df91554 token=abc123456789xyz Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signaturepart https://example.com/private/path?token=abc123"

	out := redactContentModerationSecrets(input)

	require.NotContains(t, out, "cf5bbdc4cd508f3aaf0d2070d529d4a4ac29099f8ecc357f696df28e1df91554")
	require.NotContains(t, out, "abc123456789xyz")
	require.NotContains(t, out, "eyJhbGciOiJIUzI1NiJ9")
	require.NotContains(t, out, "https://example.com/private/path")
	require.Contains(t, out, "[已脱敏]")
}

func TestContentModerationConfigNormalize_NonHitRetentionMaxThreeDays(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.NonHitRetentionDays = 30

	cfg.normalize()

	require.Equal(t, 3, cfg.NonHitRetentionDays)
}

func TestNormalizeBlockedKeywords_TrimsDedupesAndCaps(t *testing.T) {
	out := normalizeBlockedKeywords([]string{"  foo ", "FOO", "", "bar", "baz", "bar"})
	require.Equal(t, []string{"foo", "bar", "baz"}, out)
}

func TestMatchBlockedKeyword_CaseInsensitiveSubstring(t *testing.T) {
	keyword, hit := matchBlockedKeyword("Please ignore the BadWord here", []string{"badword"})
	require.True(t, hit)
	require.Equal(t, "badword", keyword)

	_, hit = matchBlockedKeyword("clean prompt", []string{"badword"})
	require.False(t, hit)

	_, hit = matchBlockedKeyword("anything", nil)
	require.False(t, hit)
}

func TestContentModerationCheck_PreBlockKeywordHitSkipsUpstreamCall(t *testing.T) {
	upstreamCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockedKeywords = []string{"secret-token"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)
	require.False(t, upstreamCalled, "keyword block must short-circuit upstream moderation call")
	logs := requireContentModerationLogCount(t, repo, 1)
	require.True(t, logs[0].Flagged)
	require.Equal(t, ContentModerationActionKeywordBlock, logs[0].Action)
	require.Equal(t, contentModerationKeywordCategory, logs[0].HighestCategory)
	require.Equal(t, "secret-token", logs[0].MatchedKeyword, "blocked log must record which keyword was hit")
}

func TestContentModerationCheck_KeywordsIgnoredInObserveMode(t *testing.T) {
	upstreamHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockedKeywords = []string{"secret-token"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed, "observe mode must let the request through even on keyword hit")
	require.Equal(t, ContentModerationActionAllow, decision.Action)
}

func TestContentModerationCheck_KeywordOnlyStrategySkipsAPIOnMiss(t *testing.T) {
	upstreamCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.99}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockedKeywords = []string{"never-matches"}
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"absolutely clean prompt"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed, "keyword-only must allow misses without calling the API")
	require.False(t, upstreamCalled, "keyword-only must not call the upstream moderation API")
	require.Len(t, repo.snapshotLogs(), 0)
}

func TestContentModerationCheck_APIOnlyStrategyIgnoresKeywordList(t *testing.T) {
	upstreamCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockedKeywords = []string{"secret-token"}
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed, "api-only must let the request through when API does not flag it")
	require.True(t, upstreamCalled, "api-only must call the upstream moderation API")
	require.NotEqual(t, ContentModerationActionKeywordBlock, decision.Action)
}

func TestNormalizeKeywordBlockingMode_UnknownFallsBackToDefault(t *testing.T) {
	require.Equal(t, ContentModerationKeywordModeKeywordAndAPI, normalizeKeywordBlockingMode(""))
	require.Equal(t, ContentModerationKeywordModeKeywordAndAPI, normalizeKeywordBlockingMode("bogus"))
	require.Equal(t, ContentModerationKeywordModeKeywordOnly, normalizeKeywordBlockingMode("keyword_only"))
	require.Equal(t, ContentModerationKeywordModeAPIOnly, normalizeKeywordBlockingMode("api_only"))
}

func TestContentModerationCheck_ModelFilterAllAuditsEveryModel(t *testing.T) {
	cfg := defaultContentModerationModelFilterTestConfig()
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterAll}
	svc, repo := newContentModerationModelFilterTestService(t, cfg)

	for _, model := range []string{"gpt-5.5", "gpt-5.4"} {
		decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
			Model:    model,
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
		})
		require.NoError(t, err)
		require.True(t, decision.Blocked)
		require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)
	}
	requireContentModerationLogCount(t, repo, 2)
}

func TestContentModerationCheck_ModelFilterIncludeOnlyAuditsListedModels(t *testing.T) {
	cfg := defaultContentModerationModelFilterTestConfig()
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterInclude, Models: []string{"gpt-5.5"}}
	svc, repo := newContentModerationModelFilterTestService(t, cfg)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)

	decision, err = svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5.4",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, "gpt-5.5", logs[0].Model)
}

func TestContentModerationCheck_ModelFilterExcludeSkipsListedModels(t *testing.T) {
	cfg := defaultContentModerationModelFilterTestConfig()
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterExclude, Models: []string{"gpt-5.4"}}
	svc, repo := newContentModerationModelFilterTestService(t, cfg)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)

	decision, err = svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5.4",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, "gpt-5.5", logs[0].Model)
}

func TestContentModerationLoadConfig_LegacyConfigDefaultsModelFilterToAll(t *testing.T) {
	raw := `{"enabled":true,"mode":"pre_block","base_url":"https://api.openai.com","model":"omni-moderation-latest","blocked_keywords":["secret-token"]}`
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyContentModerationConfig: raw,
		}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	cfg, err := svc.loadConfig(context.Background())

	require.NoError(t, err)
	require.Equal(t, ContentModerationModelFilterAll, cfg.ModelFilter.Type)
	require.Empty(t, cfg.ModelFilter.Models)
	require.True(t, cfg.includesModel("gpt-5.5"))
	require.True(t, cfg.includesModel("gpt-5.4"))
}

func TestContentModerationCheck_ModelFilterUsesRequestedModelNotBodyModel(t *testing.T) {
	cfg := defaultContentModerationModelFilterTestConfig()
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterInclude, Models: []string{"gpt-5.5"}}
	svc, repo := newContentModerationModelFilterTestService(t, cfg)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"model":"mapped-upstream-model","messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, "gpt-5.5", logs[0].Model)
}

func defaultContentModerationModelFilterTestConfig() *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BlockedKeywords = []string{"secret-token"}
	return cfg
}

func newContentModerationModelFilterTestService(t *testing.T, cfg *ContentModerationConfig) (*ContentModerationService, *contentModerationTestRepo) {
	t.Helper()
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)
	return svc, repo
}

func TestContentModerationService_DefaultDisabledDoesNotAllocateQueueOrPollSettings(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	time.Sleep(50 * time.Millisecond)

	require.Nil(t, svc.asyncQueue)
	require.Zero(t, svc.asyncRuntimeWorkerCount())
	require.Zero(t, settingRepo.getValueCalls.Load())

	for i := 0; i < 100; i++ {
		decision, err := svc.Check(context.Background(), ContentModerationCheckInput{})
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	}
	require.Zero(t, settingRepo.getValueCalls.Load())
	require.Equal(t, int64(1), settingRepo.getMultipleCalls.Load())
	require.True(t, svc.runtimePaused.Load())

	svc.runtimeMu.Lock()
	paused := make(chan struct{})
	go func() {
		svc.pauseAsyncRuntime()
		close(paused)
	}()
	fastPathReturned := false
	select {
	case <-paused:
		fastPathReturned = true
	case <-time.After(100 * time.Millisecond):
	}
	svc.runtimeMu.Unlock()
	require.True(t, fastPathReturned, "paused runtime should not acquire runtimeMu again")

	enabled := true
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Nil(t, svc.asyncQueue)
	require.Zero(t, svc.asyncRuntimeWorkerCount())
}

func TestContentModerationUpdateConfig_ResizesQueueAndWorkersWithoutPreallocation(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 2
	queueSize := 7
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
		QueueSize:   &queueSize,
	})
	require.NoError(t, err)

	svc.runtimeMu.Lock()
	queue := svc.asyncQueue
	svc.runtimeMu.Unlock()
	require.NotNil(t, queue)
	require.Equal(t, queueSize, queue.Limit())
	queue.mu.Lock()
	require.Zero(t, cap(queue.items))
	queue.mu.Unlock()
	require.Equal(t, workerCount, svc.asyncRuntimeWorkerCount())

	workerCount = 3
	queueSize = 19
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		WorkerCount: &workerCount,
		QueueSize:   &queueSize,
	})
	require.NoError(t, err)
	require.Equal(t, queueSize, queue.Limit())
	require.Equal(t, workerCount, svc.asyncRuntimeWorkerCount())

	enabled = false
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Zero(t, svc.asyncRuntimeWorkerCount())
}

func TestContentModerationAsyncTask_DropsRequestBodyAndLargeConfigCollections(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.APIKeys = []string{"sk-original"}
	cfg.Thresholds = map[string]float64{"sexual": 0.7}
	cfg.GroupIDs = []int64{1, 2, 3}
	cfg.BlockedKeywords = []string{"one", "two", "three"}
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterInclude, Models: []string{"gpt-test"}}
	input := ContentModerationCheckInput{UserID: 42, Body: make([]byte, 1024)}

	asyncTask := newContentModerationAsyncTask(input, cfg, ContentModerationInput{Text: "hello"}, "hash")
	recordTask := newContentModerationRecordTask(input, cfg, &ContentModerationLog{}, "hash", true, true)

	for _, task := range []contentModerationTask{asyncTask, recordTask} {
		require.Nil(t, task.input.Body)
		require.Empty(t, task.config.BlockedKeywords)
		require.Empty(t, task.config.GroupIDs)
		require.Empty(t, task.config.ModelFilter.Models)
		require.Equal(t, []string{"sk-original"}, task.config.APIKeys)
		require.Equal(t, 0.7, task.config.Thresholds["sexual"])
	}

	cfg.APIKeys[0] = "sk-updated"
	cfg.Thresholds["sexual"] = 0.1
	require.Equal(t, "sk-original", asyncTask.config.APIKeys[0])
	require.Equal(t, 0.7, asyncTask.config.Thresholds["sexual"])
}

func TestContentModerationAsyncTask_DetachesStringsFromLargeRequestBacking(t *testing.T) {
	backing := strings.Repeat("x", 1<<20) + "detached"
	short := backing[len(backing)-len("detached"):]
	backingStart := uintptr(unsafe.Pointer(unsafe.StringData(backing)))
	backingEnd := backingStart + uintptr(len(backing))
	withinBacking := func(value string) bool {
		ptr := uintptr(unsafe.Pointer(unsafe.StringData(value)))
		return ptr >= backingStart && ptr < backingEnd
	}
	require.True(t, withinBacking(short), "test substring must share the large source backing")

	groupID := int64(7)
	latency := 12
	input := ContentModerationCheckInput{
		RequestID: short, UserEmail: short, APIKeyName: short, GroupID: &groupID,
		GroupName: short, Endpoint: short, Provider: short, Model: short, Protocol: short,
		Body: []byte(backing),
	}
	cfg := &ContentModerationConfig{
		Enabled: true, Mode: short, BaseURL: short, Model: short, APIKey: short,
		APIKeys: []string{short}, Thresholds: map[string]float64{short: 0.7},
	}
	log := &ContentModerationLog{
		RequestID: short, UserEmail: short, APIKeyName: short, GroupID: &groupID,
		GroupName: short, Endpoint: short, Provider: short, Model: short, Mode: short,
		Action: short, HighestCategory: short, MatchedKeyword: short, InputExcerpt: short,
		Error: short, UserStatus: short, CategoryScores: map[string]float64{short: 0.8},
		ThresholdSnapshot: map[string]float64{short: 0.7}, UpstreamLatencyMS: &latency,
	}

	asyncTask := newContentModerationAsyncTask(input, cfg, ContentModerationInput{Text: short, Images: []string{short}}, short)
	recordTask := newContentModerationRecordTask(input, cfg, log, short, true, true)
	candidates := []string{
		asyncTask.input.RequestID, asyncTask.input.UserEmail, asyncTask.input.APIKeyName,
		asyncTask.input.GroupName, asyncTask.input.Endpoint, asyncTask.input.Provider,
		asyncTask.input.Model, asyncTask.input.Protocol, asyncTask.content.Text,
		asyncTask.content.Images[0], asyncTask.inputHash, asyncTask.config.Mode,
		asyncTask.config.BaseURL, asyncTask.config.Model, asyncTask.config.APIKey,
		asyncTask.config.APIKeys[0], recordTask.log.RequestID, recordTask.log.UserEmail,
		recordTask.log.APIKeyName, recordTask.log.GroupName, recordTask.log.Endpoint,
		recordTask.log.Provider, recordTask.log.Model, recordTask.log.Mode,
		recordTask.log.Action, recordTask.log.HighestCategory, recordTask.log.MatchedKeyword,
		recordTask.log.InputExcerpt, recordTask.log.Error, recordTask.log.UserStatus,
	}
	for _, candidate := range candidates {
		require.False(t, withinBacking(candidate), "async task string still references the request backing")
	}
	for key := range asyncTask.config.Thresholds {
		require.False(t, withinBacking(key), "config threshold key still references the request backing")
	}
	for key := range recordTask.log.CategoryScores {
		require.False(t, withinBacking(key), "log score key still references the request backing")
	}
	for key := range recordTask.log.ThresholdSnapshot {
		require.False(t, withinBacking(key), "log threshold key still references the request backing")
	}
	require.Nil(t, asyncTask.input.Body)
	require.NotSame(t, input.GroupID, asyncTask.input.GroupID)
	require.NotSame(t, log, recordTask.log)
	require.NotSame(t, log.UpstreamLatencyMS, recordTask.log.UpstreamLatencyMS)
}

func TestPrepareContentModerationAsyncTask_RejectsOversizedPayloadBeforeSnapshot(t *testing.T) {
	queue := newContentModerationTaskQueue(4)
	queue.byteLimit = 64
	queue.taskByteLimit = 16
	backing := strings.Repeat("x", 1024) + "0123456789abcdefg"
	oversized := backing[len(backing)-17:]
	snapshotCalled := false

	task, prepared := prepareContentModerationAsyncTaskWithSnapshotter(
		queue,
		ContentModerationCheckInput{Body: []byte(backing)},
		&ContentModerationConfig{},
		ContentModerationInput{Images: []string{oversized}},
		"",
		func(ContentModerationCheckInput, *ContentModerationConfig, ContentModerationInput, string) contentModerationTask {
			snapshotCalled = true
			return contentModerationTask{}
		},
	)

	require.False(t, prepared)
	require.False(t, snapshotCalled, "oversized payload must be rejected before strings.Clone snapshots")
	require.Equal(t, contentModerationTask{}, task)
	require.Zero(t, queue.Len())
}

func TestPrepareContentModerationAsyncTask_ReservesConcurrentSnapshotBudget(t *testing.T) {
	const (
		attempts     = 8
		queueLimit   = 4
		retainedSize = 16
	)
	queue := newContentModerationTaskQueue(queueLimit)
	queue.byteLimit = queueLimit * retainedSize
	queue.taskByteLimit = retainedSize
	start := make(chan struct{})
	snapshotStarted := make(chan struct{}, attempts)
	releaseSnapshots := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSnapshots) }) }
	t.Cleanup(release)
	results := make(chan bool, attempts)

	for i := 0; i < attempts; i++ {
		go func() {
			<-start
			_, prepared := prepareContentModerationAsyncTaskWithSnapshotter(
				queue,
				ContentModerationCheckInput{},
				&ContentModerationConfig{},
				ContentModerationInput{Text: strings.Repeat("x", retainedSize)},
				"",
				func(ContentModerationCheckInput, *ContentModerationConfig, ContentModerationInput, string) contentModerationTask {
					snapshotStarted <- struct{}{}
					<-releaseSnapshots
					return contentModerationTask{}
				},
			)
			results <- prepared
		}()
	}
	close(start)

	for i := 0; i < queueLimit; i++ {
		select {
		case <-snapshotStarted:
		case <-time.After(time.Second):
			require.FailNow(t, "snapshot reservation did not start")
		}
	}
	for i := 0; i < attempts-queueLimit; i++ {
		select {
		case prepared := <-results:
			require.False(t, prepared, "request without a reservation reached the snapshot stage")
		case <-time.After(time.Second):
			require.FailNow(t, "request was not rejected after reservation budget exhaustion")
		}
	}

	queue.mu.Lock()
	require.Equal(t, queueLimit, queue.reservedSlots)
	require.Equal(t, int64(queueLimit*retainedSize), queue.bytes)
	require.Empty(t, queue.items)
	queue.mu.Unlock()
	select {
	case <-snapshotStarted:
		require.Fail(t, "snapshot count exceeded the reserved queue budget")
	default:
	}

	release()
	for i := 0; i < queueLimit; i++ {
		select {
		case prepared := <-results:
			require.True(t, prepared)
		case <-time.After(time.Second):
			require.FailNow(t, "reserved snapshot did not commit")
		}
	}
	queue.mu.Lock()
	require.Zero(t, queue.reservedSlots)
	require.Equal(t, int64(queueLimit*retainedSize), queue.bytes)
	queue.mu.Unlock()
	require.Equal(t, queueLimit, queue.Len())
}

func TestPrepareContentModerationAsyncTask_RejectsReservationAcrossDrainEpoch(t *testing.T) {
	queue := newContentModerationTaskQueue(1)
	snapshotStarted := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	result := make(chan bool, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSnapshot) }) }
	t.Cleanup(release)

	go func() {
		_, prepared := prepareContentModerationAsyncTaskWithSnapshotter(
			queue,
			ContentModerationCheckInput{},
			&ContentModerationConfig{},
			ContentModerationInput{Text: "observe"},
			"",
			func(ContentModerationCheckInput, *ContentModerationConfig, ContentModerationInput, string) contentModerationTask {
				close(snapshotStarted)
				<-releaseSnapshot
				return contentModerationTask{}
			},
		)
		result <- prepared
	}()

	select {
	case <-snapshotStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "observe snapshot did not reserve queue budget")
	}
	queue.SetDraining(true)
	queue.SetDraining(false)
	release()
	select {
	case prepared := <-result:
		require.False(t, prepared, "observe task crossed a drain epoch")
	case <-time.After(time.Second):
		require.FailNow(t, "observe snapshot did not finish")
	}
	queue.mu.Lock()
	require.Zero(t, queue.reservedSlots)
	require.Zero(t, queue.bytes)
	queue.mu.Unlock()
	require.Zero(t, queue.Len())
}

func TestContentModerationTaskQueue_DrainWaitsForRecordReservation(t *testing.T) {
	queue := newContentModerationTaskQueue(1)
	rawTask := contentModerationTask{
		log: &ContentModerationLog{Action: ContentModerationActionAllow},
	}
	reservation, ok := queue.Reserve(&rawTask)
	require.True(t, ok)
	queue.SetDraining(true)

	type dequeueOutcome struct {
		task   contentModerationTask
		result contentModerationDequeueResult
	}
	workerStop := make(chan struct{})
	serviceStop := make(chan struct{})
	t.Cleanup(func() { close(serviceStop) })
	dequeued := make(chan dequeueOutcome, 1)
	go func() {
		task, result := queue.Dequeue(workerStop, serviceStop)
		dequeued <- dequeueOutcome{task: task, result: result}
	}()
	select {
	case outcome := <-dequeued:
		require.FailNow(t, "drain worker retired before record snapshot committed", "result=%v", outcome.result)
	case <-time.After(20 * time.Millisecond):
	}

	recordTask := newContentModerationRecordTask(
		ContentModerationCheckInput{},
		&ContentModerationConfig{},
		rawTask.log,
		"record",
		false,
		false,
	)
	require.True(t, reservation.Commit(recordTask))
	select {
	case outcome := <-dequeued:
		require.Equal(t, contentModerationDequeueTask, outcome.result)
		require.NotNil(t, outcome.task.log)
		queue.Complete(outcome.task.retainedBytes)
	case <-time.After(time.Second):
		require.FailNow(t, "drain worker did not receive committed record task")
	}
	_, result := queue.Dequeue(workerStop, serviceStop)
	require.Equal(t, contentModerationDequeueDrained, result)
}

func TestContentModerationTaskQueue_CloseRejectsLateRecordCommit(t *testing.T) {
	queue := newContentModerationTaskQueue(1)
	rawTask := contentModerationTask{
		log: &ContentModerationLog{UserStatus: "pending"},
	}
	reservation, ok := queue.Reserve(&rawTask)
	require.True(t, ok)
	queue.DropAll()
	waited := make(chan struct{})
	go func() {
		queue.WaitReservations()
		close(waited)
	}()
	select {
	case <-waited:
		require.FailNow(t, "closed queue did not wait for an outstanding reservation")
	case <-time.After(20 * time.Millisecond):
	}

	require.False(t, reservation.Commit(rawTask))
	select {
	case <-waited:
	case <-time.After(time.Second):
		require.FailNow(t, "closed queue did not release its reservation waiter")
	}
	queue.mu.Lock()
	require.True(t, queue.closed)
	require.Zero(t, queue.reservedSlots)
	require.Zero(t, queue.reservedRecords)
	require.Zero(t, queue.bytes)
	queue.mu.Unlock()
}

func TestPrepareContentModerationRecordTask_RejectsOversizedUserStatus(t *testing.T) {
	queue := newContentModerationTaskQueue(1)
	queue.byteLimit = 16
	queue.taskByteLimit = 16

	_, prepared := prepareContentModerationRecordTask(
		queue,
		ContentModerationCheckInput{},
		nil,
		&ContentModerationLog{UserStatus: strings.Repeat("x", 17)},
		"",
		false,
		false,
	)

	require.False(t, prepared)
	require.Zero(t, queue.Len())
}

func TestContentModerationTaskQueue_ReleasesBurstCapacityAfterDrain(t *testing.T) {
	queue := newContentModerationTaskQueue(1024)
	for i := 0; i < 512; i++ {
		require.True(t, queue.Enqueue(contentModerationTask{input: ContentModerationCheckInput{UserID: int64(i)}}))
	}
	queue.mu.Lock()
	burstCapacity := cap(queue.items)
	queue.mu.Unlock()
	require.Greater(t, burstCapacity, contentModerationQueueRetainedCapacity)

	workerStop := make(chan struct{})
	serviceStop := make(chan struct{})
	for i := 0; i < 512; i++ {
		task, result := queue.Dequeue(workerStop, serviceStop)
		require.Equal(t, contentModerationDequeueTask, result)
		require.Equal(t, int64(i), task.input.UserID)
	}
	queue.mu.Lock()
	retainedCapacity := cap(queue.items)
	queue.mu.Unlock()
	require.LessOrEqual(t, retainedCapacity, contentModerationQueueRetainedCapacity)
}

func TestContentModerationTaskQueue_CompactionClearsHiddenTailReferences(t *testing.T) {
	queue := newContentModerationTaskQueue(8)
	for i := 0; i < 4; i++ {
		require.True(t, queue.Enqueue(contentModerationTask{
			content: ContentModerationInput{Text: fmt.Sprintf("task-%d", i)},
			config:  &ContentModerationConfig{APIKeys: []string{fmt.Sprintf("key-%d", i)}},
		}))
	}

	workerStop := make(chan struct{})
	serviceStop := make(chan struct{})
	for i := 0; i < 2; i++ {
		_, result := queue.Dequeue(workerStop, serviceStop)
		require.Equal(t, contentModerationDequeueTask, result)
	}
	require.True(t, queue.Enqueue(contentModerationTask{
		content: ContentModerationInput{Text: "replacement"},
		config:  &ContentModerationConfig{APIKeys: []string{"replacement-key"}},
	}))

	queue.mu.Lock()
	backing := queue.items[:cap(queue.items)]
	for i := len(queue.items); i < len(backing); i++ {
		require.Empty(t, backing[i].content.Text, "hidden tail item %d retained content", i)
		require.Nil(t, backing[i].config, "hidden tail item %d retained config", i)
		require.Nil(t, backing[i].log, "hidden tail item %d retained log", i)
		require.Zero(t, backing[i].retainedBytes, "hidden tail item %d retained byte accounting", i)
	}
	queue.mu.Unlock()
}

func TestContentModerationTaskQueue_EnforcesPayloadByteBudget(t *testing.T) {
	queue := newContentModerationTaskQueue(10)
	queue.byteLimit = 16
	queue.taskByteLimit = 16
	first := contentModerationTask{content: ContentModerationInput{Images: []string{"0123456789"}}}
	second := contentModerationTask{content: ContentModerationInput{Images: []string{"abcdefghij"}}}
	oversized := contentModerationTask{content: ContentModerationInput{Images: []string{"0123456789abcdefg"}}}

	require.True(t, queue.Enqueue(first))
	require.False(t, queue.Enqueue(second))
	require.False(t, queue.Enqueue(oversized))
	queue.mu.Lock()
	retainedBytes := queue.bytes
	queue.mu.Unlock()
	require.Equal(t, int64(10), retainedBytes)

	activeTask, result := queue.Dequeue(make(chan struct{}), make(chan struct{}))
	require.Equal(t, contentModerationDequeueTask, result)
	queue.mu.Lock()
	retainedBytes = queue.bytes
	queue.mu.Unlock()
	require.Equal(t, int64(10), retainedBytes)
	require.False(t, queue.Enqueue(second), "active task must retain its byte reservation")
	queue.Complete(activeTask.retainedBytes)
	queue.mu.Lock()
	retainedBytes = queue.bytes
	queue.mu.Unlock()
	require.Zero(t, retainedBytes)
	require.True(t, queue.Enqueue(second))
}

func TestContentModerationRuntime_DrainWorkerRetiresWhenQueueBecomesEmpty(t *testing.T) {
	var moderationRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moderationRequests.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	defer server.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	repo := &contentModerationTestRepo{
		createStarted: started,
		createRelease: release,
	}
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	t.Cleanup(releaseAll)

	enabled := true
	workerCount := 1
	queueSize := 4
	baseURL := server.URL
	apiKeys := []string{"sk-test"}
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
		QueueSize:   &queueSize,
		BaseURL:     &baseURL,
		APIKeys:     &apiKeys,
	})
	require.NoError(t, err)
	cfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)

	svc.enqueueRecord(ContentModerationCheckInput{}, cfg, &ContentModerationLog{Action: ContentModerationActionAllow}, "first", false, false)
	select {
	case <-started:
	case <-time.After(time.Second):
		require.FailNow(t, "content moderation worker did not start")
	}
	svc.enqueueAsync(ContentModerationCheckInput{}, cfg, ContentModerationInput{Text: "observe"}, "observe")
	svc.enqueueRecord(ContentModerationCheckInput{}, cfg, &ContentModerationLog{Action: ContentModerationActionAllow}, "second", false, false)
	require.Eventually(t, func() bool { return svc.asyncQueueLength() == 2 }, time.Second, 10*time.Millisecond)

	enabled = false
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, 1, svc.asyncQueueLength(), "disable must drop queued observe tasks and retain record tasks")
	require.Equal(t, 1, svc.asyncRuntimeWorkerCount())

	releaseAll()
	requireContentModerationLogCount(t, repo, 2)
	require.Eventually(t, func() bool { return svc.asyncRuntimeWorkerCount() == 0 }, time.Second, 10*time.Millisecond)
	require.Zero(t, moderationRequests.Load())
}

func TestContentModerationRuntime_DrainCandidateContinuesAfterReenable(t *testing.T) {
	repo := &contentModerationTestRepo{}
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 1
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)

	svc.runtimeMu.Lock()
	queue := svc.asyncQueue
	queue.SetDraining(true)
	deadline := time.Now().Add(time.Second)
	for len(queue.notify) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	drainSignalConsumed := len(queue.notify) == 0
	queue.SetDraining(false)
	svc.runtimeMu.Unlock()
	require.True(t, drainSignalConsumed, "worker did not reach the drain retirement check")

	cfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	svc.enqueueRecord(ContentModerationCheckInput{}, cfg, &ContentModerationLog{Action: ContentModerationActionAllow}, "reenabled", false, false)
	requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, 1, svc.asyncRuntimeWorkerCount())
}

func TestContentModerationRuntime_RapidResizeKeepsExactOpenWorkerHandles(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 4
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)

	for i := 0; i < 20; i++ {
		workerCount = 1
		_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{WorkerCount: &workerCount})
		require.NoError(t, err)
		require.LessOrEqual(t, svc.asyncActualWorkerCount(), 4)
		workerCount = 4
		_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{WorkerCount: &workerCount})
		require.NoError(t, err)
		require.LessOrEqual(t, svc.asyncActualWorkerCount(), workerCount)
	}

	require.Eventually(t, func() bool {
		return svc.asyncActualWorkerCount() == workerCount && svc.asyncRuntimeWorkerCount() == workerCount
	}, time.Second, 10*time.Millisecond)
	svc.runtimeMu.Lock()
	workerControls := make([]*contentModerationWorkerControl, 0, len(svc.workerStops))
	for _, control := range svc.workerStops {
		if !control.stopping {
			workerControls = append(workerControls, control)
		}
	}
	svc.runtimeMu.Unlock()
	require.Len(t, workerControls, workerCount)
	for _, control := range workerControls {
		select {
		case <-control.ctx.Done():
			require.Fail(t, "runtime retained a closed worker handle")
		default:
		}
	}
}

func TestContentModerationRuntime_StaleConfigCannotReenableDisabledQueue(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 1
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)
	staleCfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.NotZero(t, staleCfg.runtimeRevision)

	enabled = false
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return svc.asyncActualWorkerCount() == 0 }, time.Second, 10*time.Millisecond)

	require.Nil(t, svc.syncAsyncRuntime(staleCfg))
	svc.enqueueAsync(ContentModerationCheckInput{}, staleCfg, ContentModerationInput{Text: "stale"}, "stale")
	require.Zero(t, svc.asyncQueueLength())
	require.Zero(t, svc.asyncActualWorkerCount())
	svc.runtimeMu.Lock()
	queue := svc.asyncQueue
	svc.runtimeMu.Unlock()
	require.NotNil(t, queue)
	queue.mu.Lock()
	require.True(t, queue.draining)
	queue.mu.Unlock()
}

func TestContentModerationRuntime_StaleRiskSnapshotCannotReenableDisabledQueue(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 1
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)
	_, staleRevision := svc.riskControlState(context.Background())
	require.NotZero(t, staleRevision)

	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyRiskControlEnabled, "false"))
	expireContentModerationRiskControlCache(svc)
	riskEnabled, currentRevision := svc.riskControlState(context.Background())
	require.False(t, riskEnabled)
	require.NotEqual(t, staleRevision, currentRevision)
	require.True(t, svc.pauseAsyncRuntimeForRisk(currentRevision))
	require.Eventually(t, func() bool { return svc.asyncActualWorkerCount() == 0 }, time.Second, 10*time.Millisecond)

	cfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	queue, current := svc.syncAsyncRuntimeForRisk(cfg, staleRevision)
	require.False(t, current)
	require.Nil(t, queue)
	require.Zero(t, svc.asyncActualWorkerCount())
}

func TestContentModerationRuntime_StaleDisabledRiskSnapshotCannotPauseEnabledQueue(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "false"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 1
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)
	_, staleRevision := svc.riskControlState(context.Background())
	require.NotZero(t, staleRevision)
	require.Nil(t, svc.asyncQueue)

	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyRiskControlEnabled, "true"))
	expireContentModerationRiskControlCache(svc)
	riskEnabled, currentRevision := svc.riskControlState(context.Background())
	require.True(t, riskEnabled)
	require.NotEqual(t, staleRevision, currentRevision)
	cfg, err := svc.loadConfigSnapshot(context.Background())
	require.NoError(t, err)
	queue, current := svc.syncAsyncRuntimeForRisk(cfg, currentRevision)
	require.True(t, current)
	require.NotNil(t, queue)
	require.Eventually(t, func() bool { return svc.asyncActualWorkerCount() == 1 }, time.Second, 10*time.Millisecond)

	require.False(t, svc.pauseAsyncRuntimeForRisk(staleRevision))
	require.Equal(t, 1, svc.asyncActualWorkerCount())
}

func TestContentModerationRuntime_ResizeDoesNotCancelActiveObserveTasks(t *testing.T) {
	started := make(chan struct{}, 2)
	canceled := make(chan struct{}, 2)
	releaseRequests := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRequests) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-releaseRequests:
			_ = json.NewEncoder(w).Encode(moderationAPIResponse{
				Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}},
			})
		case <-r.Context().Done():
			canceled <- struct{}{}
		}
	}))
	t.Cleanup(server.Close)

	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	t.Cleanup(release)
	enabled := true
	mode := ContentModerationModeObserve
	workerCount := 2
	queueSize := 2
	baseURL := server.URL
	apiKeys := []string{"sk-test"}
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		Mode:        &mode,
		WorkerCount: &workerCount,
		QueueSize:   &queueSize,
		BaseURL:     &baseURL,
		APIKeys:     &apiKeys,
		APIKeysMode: contentModerationAPIKeysModeReplace,
	})
	require.NoError(t, err)
	cfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)

	svc.enqueueAsync(ContentModerationCheckInput{}, cfg, ContentModerationInput{Text: "first"}, "first")
	svc.enqueueAsync(ContentModerationCheckInput{}, cfg, ContentModerationInput{Text: "second"}, "second")
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			require.FailNow(t, "observe request did not start")
		}
	}

	workerCount = 1
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{WorkerCount: &workerCount})
	require.NoError(t, err)
	select {
	case <-canceled:
		require.Fail(t, "worker resize canceled an active observe request")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	require.Eventually(t, func() bool { return svc.asyncProcessed.Load() == 2 }, time.Second, 10*time.Millisecond)
	require.Zero(t, svc.asyncErrors.Load())
}

func TestContentModerationStatus_DoesNotHideRetiringActiveWorkers(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.WorkerCount = 1
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	t.Cleanup(svc.Close)
	svc.asyncActive.Store(4)

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, status.ActiveWorkers)
	require.Zero(t, status.IdleWorkers)
}

func TestContentModerationClose_CancelsAndWaitsForActiveWorker(t *testing.T) {
	started := make(chan struct{})
	repo := &contentModerationTestRepo{
		createStarted: started,
		createRelease: make(chan struct{}),
	}
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true"}}
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	enabled := true
	workerCount := 1
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Enabled:     &enabled,
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)
	cfg, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	svc.enqueueRecord(ContentModerationCheckInput{}, cfg, &ContentModerationLog{Action: ContentModerationActionAllow}, "close", false, false)

	select {
	case <-started:
	case <-time.After(time.Second):
		require.FailNow(t, "content moderation worker did not start")
	}
	svc.enqueueRecord(ContentModerationCheckInput{}, cfg, &ContentModerationLog{Action: ContentModerationActionAllow, InputExcerpt: "pending"}, "pending", false, false)
	require.Eventually(t, func() bool { return svc.asyncQueueLength() == 1 }, time.Second, 10*time.Millisecond)
	closed := make(chan struct{})
	go func() {
		svc.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		require.FailNow(t, "Close did not cancel and wait for the active worker")
	}
	require.Zero(t, svc.asyncRuntimeWorkerCount())
	require.Zero(t, svc.asyncQueueLength())
	svc.asyncQueue.mu.Lock()
	retainedBytes := svc.asyncQueue.bytes
	retainedCapacity := cap(svc.asyncQueue.items)
	svc.asyncQueue.mu.Unlock()
	require.Zero(t, retainedBytes)
	require.Zero(t, retainedCapacity)
}

func TestContentModerationConfigCache_UpdateInvalidatesImmediatelyAndTTLRefreshes(t *testing.T) {
	initial := defaultContentModerationConfig()
	rawInitial, err := json.Marshal(initial)
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawInitial),
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	first, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	first.WorkerCount = maxContentModerationWorkerCount
	second, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, defaultContentModerationWorkerCount, second.WorkerCount)
	require.Equal(t, int64(1), settingRepo.getValueCalls.Load())

	enabled := true
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Enabled: &enabled})
	require.NoError(t, err)
	require.True(t, view.Enabled)
	view, err = svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.True(t, view.Enabled)
	require.Equal(t, int64(2), settingRepo.getValueCalls.Load())

	external := defaultContentModerationConfig()
	rawExternal, err := json.Marshal(external)
	require.NoError(t, err)
	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyContentModerationConfig, string(rawExternal)))
	expireContentModerationConfigCache(svc)

	view, err = svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.False(t, view.Enabled)
	require.Equal(t, int64(3), settingRepo.getValueCalls.Load())
}

func TestContentModerationConfigCache_ReturnsStaleConfigDuringRefresh(t *testing.T) {
	initial := defaultContentModerationConfig()
	rawInitial, err := json.Marshal(initial)
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawInitial),
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	first, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.False(t, first.Enabled)

	external := defaultContentModerationConfig()
	external.Enabled = true
	rawExternal, err := json.Marshal(external)
	require.NoError(t, err)
	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyContentModerationConfig, string(rawExternal)))
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRefresh) }) }
	t.Cleanup(release)
	settingRepo.getValueStarted = refreshStarted
	settingRepo.getValueRelease = releaseRefresh
	expireContentModerationConfigCache(svc)

	refreshed := make(chan *ContentModerationConfigView, 1)
	go func() {
		view, _ := svc.GetConfig(context.Background())
		refreshed <- view
	}()
	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "content moderation config refresh did not start")
	}

	stale := make(chan *ContentModerationConfigView, 1)
	go func() {
		view, _ := svc.GetConfig(context.Background())
		stale <- view
	}()
	select {
	case view := <-stale:
		require.NotNil(t, view)
		require.False(t, view.Enabled)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "stale config read blocked behind settings I/O")
	}

	release()
	select {
	case view := <-refreshed:
		require.NotNil(t, view)
		require.True(t, view.Enabled)
	case <-time.After(time.Second):
		require.FailNow(t, "content moderation config refresh did not finish")
	}
}

func TestContentModerationConfigCache_RefreshFailureKeepsLastGoodConfig(t *testing.T) {
	initial := defaultContentModerationConfig()
	initial.Enabled = true
	rawInitial, err := json.Marshal(initial)
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawInitial),
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	first, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.True(t, first.Enabled)
	settingRepo.getValueErr = fmt.Errorf("settings unavailable")
	expireContentModerationConfigCache(svc)

	stale, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, stale)
	require.True(t, stale.Enabled)
	retryAt := contentModerationConfigCacheExpiry(svc)
	require.WithinDuration(t, time.Now().Add(contentModerationSettingsErrorRetryTTL), retryAt, 100*time.Millisecond)
}

func TestContentModerationConfigCache_RefreshIgnoresCallerCancellation(t *testing.T) {
	cfg := defaultContentModerationConfig()
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawCfg),
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loaded, err := svc.loadConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, loaded)
}

func TestContentModerationConfigCache_HotReadDoesNotCloneLargeSnapshot(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.BlockedKeywords = make([]string, maxContentModerationBlockedKeywords)
	for i := range cfg.BlockedKeywords {
		cfg.BlockedKeywords[i] = fmt.Sprintf("blocked-%d", i)
	}
	cfg.ModelFilter = ContentModerationModelFilter{
		Type:   ContentModerationModelFilterInclude,
		Models: make([]string, maxContentModerationModelFilterModels),
	}
	for i := range cfg.ModelFilter.Models {
		cfg.ModelFilter.Models[i] = fmt.Sprintf("model-%d", i)
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawCfg),
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	ctx := context.Background()

	first, err := svc.loadConfigSnapshot(ctx)
	require.NoError(t, err)
	var current *ContentModerationConfig
	allocs := testing.AllocsPerRun(1000, func() {
		current, _ = svc.loadConfigSnapshot(ctx)
	})
	require.Zero(t, allocs)
	require.Same(t, first, current)

	modelAllocs := testing.AllocsPerRun(1000, func() {
		if !first.includesModel("model-999") {
			panic("expected model match")
		}
	})
	require.Zero(t, modelAllocs)
}

func TestContentModerationRiskControlCache_UsesTTL(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRiskControlEnabled: "true",
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)

	require.True(t, svc.isRiskControlEnabled(context.Background()))
	require.True(t, svc.isRiskControlEnabled(context.Background()))
	require.Equal(t, int64(1), settingRepo.getValueCalls.Load())

	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyRiskControlEnabled, "false"))
	require.True(t, svc.isRiskControlEnabled(context.Background()))
	expireContentModerationRiskControlCache(svc)

	require.False(t, svc.isRiskControlEnabled(context.Background()))
	require.Equal(t, int64(2), settingRepo.getValueCalls.Load())
}

func TestContentModerationRiskControlCache_ReturnsStaleValueDuringRefresh(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRiskControlEnabled: "true",
	}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	require.True(t, svc.isRiskControlEnabled(context.Background()))

	require.NoError(t, settingRepo.Set(context.Background(), SettingKeyRiskControlEnabled, "false"))
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRefresh) }) }
	t.Cleanup(release)
	settingRepo.getValueStarted = refreshStarted
	settingRepo.getValueRelease = releaseRefresh
	expireContentModerationRiskControlCache(svc)

	refreshed := make(chan bool, 1)
	go func() { refreshed <- svc.isRiskControlEnabled(context.Background()) }()
	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "risk control refresh did not start")
	}

	stale := make(chan bool, 1)
	go func() { stale <- svc.isRiskControlEnabled(context.Background()) }()
	select {
	case enabled := <-stale:
		require.True(t, enabled)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "stale risk control read blocked behind settings I/O")
	}

	release()
	select {
	case enabled := <-refreshed:
		require.False(t, enabled)
	case <-time.After(time.Second):
		require.FailNow(t, "risk control refresh did not finish")
	}
}

func TestContentModerationUpdateConfig_AppendsAndDeletesAPIKeys(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.APIKeys = []string{"sk-old-a", "sk-old-b"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawCfg),
	}}
	svc := NewContentModerationService(repo, nil, nil, nil, nil, nil, nil)
	deleteHashes := []string{moderationAPIKeyHash("sk-old-a")}
	addKeys := []string{"sk-new-c", "sk-old-b"}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIKeys:            &addKeys,
		DeleteAPIKeyHashes: &deleteHashes,
	})

	require.NoError(t, err)
	require.Equal(t, 2, view.APIKeyCount)
	require.Equal(t, []string{maskSecretTail("sk-old-b"), maskSecretTail("sk-new-c")}, view.APIKeyMasks)

	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, []string{"sk-old-b", "sk-new-c"}, saved.apiKeys())
}

func TestContentModerationUpdateConfig_ReplacesAPIKeysWhenRequested(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.APIKeys = []string{"sk-old-a", "sk-old-b"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawCfg),
	}}
	svc := NewContentModerationService(repo, nil, nil, nil, nil, nil, nil)
	deleteHashes := []string{moderationAPIKeyHash("sk-old-a")}
	replaceKeys := []string{"sk-new-only"}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIKeys:            &replaceKeys,
		APIKeysMode:        contentModerationAPIKeysModeReplace,
		DeleteAPIKeyHashes: &deleteHashes,
	})

	require.NoError(t, err)
	require.Equal(t, 1, view.APIKeyCount)
	require.Equal(t, []string{maskSecretTail("sk-new-only")}, view.APIKeyMasks)

	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, []string{"sk-new-only"}, saved.apiKeys())
}

func TestContentModerationUpdateConfig_SavesCustomThresholds(t *testing.T) {
	cfg := defaultContentModerationConfig()
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(rawCfg),
	}}
	svc := NewContentModerationService(repo, nil, nil, nil, nil, nil, nil)
	thresholds := map[string]float64{
		"sexual":     0.72,
		"harassment": 1.25,
		"unknown":    0.01,
	}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		Thresholds: &thresholds,
	})

	require.NoError(t, err)
	require.Equal(t, 0.72, view.Thresholds["sexual"])
	require.Equal(t, 1.0, view.Thresholds["harassment"])
	require.NotContains(t, view.Thresholds, "unknown")

	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, 0.72, saved.Thresholds["sexual"])
	require.Equal(t, 1.0, saved.Thresholds["harassment"])
	require.NotContains(t, saved.Thresholds, "unknown")
}

func TestExtractContentModerationInput_AnthropicImageSourceOnlyParticipatesInMemory(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"old"},
			{"role":"assistant","content":"ok"},
			{"role":"user","content":[
				{"type":"text","text":"检查这张图"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}
			]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)
	require.Equal(t, "检查这张图", input.Text)
	require.Equal(t, []string{"data:image/png;base64,aGVsbG8="}, input.Images)

	log := (&ContentModerationService{}).buildLog(ContentModerationCheckInput{}, defaultContentModerationConfig(), ContentModerationActionAllow, false, "", 0, nil, input.ExcerptText(), nil, nil, "")
	require.Equal(t, "检查这张图", log.InputExcerpt)
	require.NotContains(t, log.InputExcerpt, "aGVsbG8=")
}

func TestExtractContentModerationInput_AnthropicKeepsEphemeralUserTextAndSkipsSystemReminders(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "<system-reminder>工具说明</system-reminder>"},
					{"type": "text", "text": "<system-reminder>Ainder>\n\n"},
					{"type": "text", "text": "hid", "cache_control": {"type": "ephemeral"}}
				]
			}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "hid", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatUsesLastUserMessage(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"messages":[
			{"role":"system","content":"system prompt"},
			{"role":"user","content":"old user"},
			{"role":"assistant","content":"ok"},
			{"role":"user","content":[{"type":"text","text":"latest user"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "latest user", input.Text)
	require.Equal(t, []string{"https://example.com/a.png"}, input.Images)
	require.NotContains(t, input.Text, "old user")
	require.NotContains(t, input.Text, "system prompt")
}

func TestExtractContentModerationInput_OpenAIImagesIncludesPromptAndImages(t *testing.T) {
	body := []byte(`{
		"prompt":"replace background",
		"images":[
			{"image_url":"https://example.com/source.png"},
			{"image_url":"data:image/png;base64,aGVsbG8="}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, body)

	require.Equal(t, "replace background", input.Text)
	require.Equal(t, []string{"https://example.com/source.png", "data:image/png;base64,aGVsbG8="}, input.Images)
}

func TestContentModerationInput_NormalizeKeepsImagesAndModerationInputSamplesOneImage(t *testing.T) {
	images := []string{
		"data:image/png;base64,Zmlyc3Q=",
		"data:image/png;base64,c2Vjb25k",
	}
	input := ContentModerationInput{
		Text:   "check image",
		Images: append([]string(nil), images...),
	}
	input.Normalize()

	require.Equal(t, images, input.Images)

	parts, ok := input.ModerationInput().([]moderationAPIInputPart)
	require.True(t, ok)
	require.Len(t, parts, 2)
	require.Equal(t, "text", parts[0].Type)
	require.Equal(t, "image_url", parts[1].Type)
	require.NotNil(t, parts[1].ImageURL)
	require.Contains(t, images, parts[1].ImageURL.URL)
}

func TestBuildModerationTestInputRejectsMultipleImages(t *testing.T) {
	_, _, err := buildModerationTestInput("check image", []string{
		"data:image/png;base64,Zmlyc3Q=",
		"data:image/png;base64,c2Vjb25k",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "最多上传 1 张测试图片")
}

func TestExtractContentModerationInput_OpenAIResponsesCodexPayloadUsesLastUserMessage(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"instructions":"instructions.....",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer permissions sk-proj-1234567890abcdef"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first user prompt"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"last user prompt"}]}
		],
		"prompt_cache_key":"cache-key"
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "last user prompt", input.Text)
	require.Empty(t, input.Images)
	require.NotContains(t, input.Text, "developer permissions")
	require.NotContains(t, input.Text, "first user prompt")
}

func TestContentModerationCheck_OpenAIResponsesRecordsNonHitForCodexPayload(t *testing.T) {
	var moderationRequest moderationAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&moderationRequest))
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.01},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RecordNonHits = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer instructions should not be audited"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first user prompt"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"last user prompt"}]}
		]
	}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/responses",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     body,
	})

	require.NoError(t, err)
	require.False(t, decision.Blocked)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.False(t, logs[0].Flagged)
	require.Equal(t, ContentModerationActionAllow, logs[0].Action)
	require.Equal(t, "/responses", logs[0].Endpoint)
	require.Equal(t, "last user prompt", logs[0].InputExcerpt)
	require.Equal(t, "last user prompt", moderationRequest.Input)
}

func TestContentModerationCheck_PreBlockBlocksCodexResponsesLatestUserInput(t *testing.T) {
	var moderationRequest moderationAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&moderationRequest))
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockStatus = http.StatusUnavailableForLegalReasons
	cfg.BlockMessage = "内容审计测试阻断"
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{
		"model":"gpt-5.5",
		"instructions":"instructions.....",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer instructions should not be audited"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"environment context"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"latest blocked prompt"}]}
		]
	}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Endpoint: "/responses",
		Provider: "openai",
		Model:    "gpt-5.5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Equal(t, http.StatusUnavailableForLegalReasons, decision.StatusCode)
	require.Equal(t, "内容审计测试阻断", decision.Message)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.True(t, logs[0].Flagged)
	require.Equal(t, ContentModerationActionBlock, logs[0].Action)
	require.Equal(t, ContentModerationModePreBlock, logs[0].Mode)
	require.Equal(t, "latest blocked prompt", logs[0].InputExcerpt)
	require.Equal(t, "latest blocked prompt", moderationRequest.Input)
}

func TestContentModerationStatusTracksPreBlockSyncMetrics(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		score := 0.01
		if requestCount == 1 {
			score = 0.9
		}
		time.Sleep(5 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": score},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	for _, prompt := range []string{"blocked prompt", "clean prompt"} {
		_, err := svc.Check(context.Background(), ContentModerationCheckInput{
			UserID:   1001,
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":%q}]}`, prompt)),
		})
		require.NoError(t, err)
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), status.PreBlockChecked)
	require.Equal(t, int64(1), status.PreBlockAllowed)
	require.Equal(t, int64(1), status.PreBlockBlocked)
	require.Equal(t, int64(0), status.PreBlockErrors)
	require.Equal(t, 0, status.PreBlockActive)
	require.GreaterOrEqual(t, status.PreBlockAvgLatencyMS, int64(1))
}

func TestContentModerationStatusTracksPreBlockAPIKeyLoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.01},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-one", "sk-two"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	for idx := 0; idx < 4; idx++ {
		_, err := svc.Check(context.Background(), ContentModerationCheckInput{
			UserID:   1001,
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":"prompt %d"}]}`, idx)),
		})
		require.NoError(t, err)
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Len(t, status.PreBlockAPIKeyLoads, 2)
	require.Equal(t, int64(4), status.PreBlockAPIKeyTotalCalls)
	require.Equal(t, int64(2), status.PreBlockAPIKeyAvailableCount)
	require.Equal(t, int64(0), status.PreBlockAPIKeyActive)
	require.Equal(t, int64(0), status.PreBlockAPIKeyLoads[0].Active)
	require.Equal(t, int64(2), status.PreBlockAPIKeyLoads[0].Total)
	require.Equal(t, int64(2), status.PreBlockAPIKeyLoads[0].Success)
	require.Equal(t, int64(0), status.PreBlockAPIKeyLoads[0].Errors)
	require.Equal(t, int64(2), status.PreBlockAPIKeyLoads[1].Total)
	require.Equal(t, int64(2), status.PreBlockAPIKeyLoads[1].Success)
}

func TestContentModerationStatusTracksPreBlockLocalBlocks(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.BlockedKeywords = []string{"blocked"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	for _, prompt := range []string{"blocked prompt", "clean prompt"} {
		_, err := svc.Check(context.Background(), ContentModerationCheckInput{
			UserID:   1001,
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":%q}]}`, prompt)),
		})
		require.NoError(t, err)
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), status.PreBlockChecked)
	require.Equal(t, int64(1), status.PreBlockAllowed)
	require.Equal(t, int64(1), status.PreBlockBlocked)
	require.Equal(t, int64(0), status.PreBlockErrors)
}

func TestBuildContentModerationTestAuditResult_UsesConfiguredThresholdsOnly(t *testing.T) {
	result := buildContentModerationTestAuditResult(&moderationAPIResult{
		Flagged: true,
		CategoryScores: map[string]float64{
			"harassment": 0.65,
		},
	}, nil)

	require.NotNil(t, result)
	require.False(t, result.Flagged)
	require.Equal(t, "harassment", result.HighestCategory)
	require.Equal(t, 0.65, result.HighestScore)
	require.Equal(t, 0.65, result.CompositeScore)
	require.Equal(t, 0.98, result.Thresholds["harassment"])
}

func TestContentModerationCallModeration_400DoesNotFreezeAPIKey(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Number of images (5) exceeds maximum of 1","type":"invalid_request_error","param":"input","code":"too_many_images"}}`))
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 5
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	_, err := svc.callModeration(context.Background(), cfg, "hello")

	require.Error(t, err)
	require.Equal(t, 1, requestCount)
	status := svc.apiKeyStatusForHash(0, moderationAPIKeyHash("sk-test"), maskSecretTail("sk-test"), true)
	require.Equal(t, "error", status.Status)
	require.Equal(t, http.StatusBadRequest, status.LastHTTPStatus)
	require.Zero(t, status.FailureCount)
	require.Nil(t, status.FrozenUntil)
}

func TestContentModerationCallModeration_FreezesByHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		minFreeze  time.Duration
		maxFreeze  time.Duration
	}{
		{name: "401 freezes ten minutes", statusCode: http.StatusUnauthorized, minFreeze: 9*time.Minute + 55*time.Second, maxFreeze: 10*time.Minute + time.Second},
		{name: "403 freezes ten minutes", statusCode: http.StatusForbidden, minFreeze: 9*time.Minute + 55*time.Second, maxFreeze: 10*time.Minute + time.Second},
		{name: "429 freezes one minute", statusCode: http.StatusTooManyRequests, minFreeze: 55 * time.Second, maxFreeze: time.Minute + time.Second},
		{name: "529 freezes one minute", statusCode: 529, minFreeze: 55 * time.Second, maxFreeze: time.Minute + time.Second},
		{name: "500 freezes ten seconds", statusCode: http.StatusInternalServerError, minFreeze: 5 * time.Second, maxFreeze: 11 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream error"}}`))
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.BaseURL = server.URL
			cfg.APIKeys = []string{"sk-test"}
			cfg.RetryCount = 0
			svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

			_, err := svc.callModeration(context.Background(), cfg, "hello")

			require.Error(t, err)
			status := svc.apiKeyStatusForHash(0, moderationAPIKeyHash("sk-test"), maskSecretTail("sk-test"), true)
			require.Equal(t, "frozen", status.Status)
			require.Equal(t, tt.statusCode, status.LastHTTPStatus)
			require.Equal(t, 1, status.FailureCount)
			require.NotNil(t, status.FrozenUntil)
			remaining := time.Until(*status.FrozenUntil)
			require.GreaterOrEqual(t, remaining, tt.minFreeze)
			require.LessOrEqual(t, remaining, tt.maxFreeze)
		})
	}
}

func TestContentModerationTestAPIKeys_400DoesNotFreezeAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid moderation request"}}`))
	}))
	defer server.Close()

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	result, err := svc.TestAPIKeys(context.Background(), TestContentModerationAPIKeysInput{
		APIKeys: []string{"sk-test"},
		BaseURL: server.URL,
		Prompt:  "hello",
	})

	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "error", result.Items[0].Status)
	require.Equal(t, http.StatusBadRequest, result.Items[0].LastHTTPStatus)
	require.Zero(t, result.Items[0].FailureCount)
	require.Nil(t, result.Items[0].FrozenUntil)
}

func TestContentModerationTestAPIKeys_RemovesTemporaryKeyHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}},
		})
	}))
	defer server.Close()
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	t.Cleanup(svc.Close)

	for i := 0; i < 32; i++ {
		key := fmt.Sprintf("sk-temporary-%d", i)
		result, err := svc.TestAPIKeys(context.Background(), TestContentModerationAPIKeysInput{
			APIKeys: []string{key},
			BaseURL: server.URL,
			Prompt:  "hello",
		})
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		require.Equal(t, "ok", result.Items[0].Status)
	}
	svc.keyHealthMu.Lock()
	require.Empty(t, svc.keyHealth)
	svc.keyHealthMu.Unlock()
}

func TestContentModerationUpdateConfig_PrunesRemovedAPIKeyHealth(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	svc.markAPIKeySuccess("sk-old", 10, http.StatusOK)

	apiKeys := []string{"sk-new"}
	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIKeys:     &apiKeys,
		APIKeysMode: contentModerationAPIKeysModeReplace,
	})
	require.NoError(t, err)

	svc.keyHealthMu.Lock()
	_, retained := svc.keyHealth[moderationAPIKeyHash("sk-old")]
	svc.keyHealthMu.Unlock()
	require.False(t, retained)
}

func TestContentModerationAPIKeyHealth_IsBounded(t *testing.T) {
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	t.Cleanup(svc.Close)
	for i := 0; i < maxContentModerationKeyHealthEntries+32; i++ {
		svc.markAPIKeySuccess(fmt.Sprintf("sk-%d", i), 1, http.StatusOK)
	}
	svc.keyHealthMu.Lock()
	require.LessOrEqual(t, len(svc.keyHealth), maxContentModerationKeyHealthEntries)
	svc.keyHealthMu.Unlock()
}

func TestContentModerationCheck_PreHashUsesRedisHashCache(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.PreHashCheckEnabled = true
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockStatus = http.StatusConflict
	cfg.BlockMessage = "命中历史风险输入"
	cfg.AutoBanEnabled = true
	cfg.BanThreshold = 1
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	hashCache := &contentModerationTestHashCache{hashes: map[string]struct{}{}}
	content := ContentModerationInput{Text: "blocked prompt"}
	content.Normalize()
	hashCache.hashes[content.Hash()] = struct{}{}

	repo := &contentModerationTestRepo{}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: 1001, Status: StatusActive}}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		hashCache,
		nil,
		userRepo,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   1001,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"blocked prompt"}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionHashBlock, decision.Action)
	require.Equal(t, http.StatusConflict, decision.StatusCode)
	require.Equal(t, content.Hash(), decision.InputHash)
	require.Contains(t, decision.Message, "命中历史风险输入")
	require.Contains(t, decision.Message, content.Hash())
	require.Len(t, hashCache.snapshotChecked(), 1)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.True(t, logs[0].Flagged)
	require.Equal(t, ContentModerationActionHashBlock, logs[0].Action)
	require.Equal(t, 1.0, logs[0].CategoryScores["hash"])
	require.Equal(t, ContentModerationModePreBlock, logs[0].Mode)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.Empty(t, userRepo.updated)
}

func TestContentModerationCheck_HashBlockLogsDoNotIncreaseNextViolationCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.AutoBanEnabled = false
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	hashLog := &ContentModerationLog{
		UserID:          &userID,
		Action:          ContentModerationActionHashBlock,
		Flagged:         true,
		HighestCategory: "hash",
		HighestScore:    1,
		CreatedAt:       time.Now(),
	}
	require.NoError(t, repo.CreateLog(context.Background(), hashLog))

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   userID,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"new blocked prompt"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	logs := requireContentModerationLogCount(t, repo, 2)
	require.Equal(t, ContentModerationActionHashBlock, logs[0].Action)
	require.Equal(t, ContentModerationActionBlock, logs[1].Action)
	require.Equal(t, 1, logs[1].ViolationCount)
}

func TestContentModerationAutoBanSkipsAdminAccount(t *testing.T) {
	var slogOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&slogOutput, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 2
	cfg.ViolationWindowHours = 24

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.CreateLog(context.Background(), newContentModerationFlaggedLog(userID)))
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleAdmin, Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, invalidator, nil)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)

	logs := requireContentModerationLogCount(t, repo, 2)
	require.Equal(t, 2, logs[1].ViolationCount)
	require.False(t, logs[1].AutoBanned)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Empty(t, userRepo.updated)
	require.Empty(t, invalidator.userIDs)
	require.Contains(t, slogOutput.String(), "content_moderation.autoban_skipped_admin")
	require.Contains(t, slogOutput.String(), "user_id=1001")
	require.Contains(t, slogOutput.String(), "role=admin")
	require.Contains(t, slogOutput.String(), "count=2")
	require.Contains(t, slogOutput.String(), "threshold=2")
}

func TestContentModerationAutoBanDisablesRegularUserAtThreshold(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 2
	cfg.ViolationWindowHours = 24

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.CreateLog(context.Background(), newContentModerationFlaggedLog(userID)))
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, invalidator, nil)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)

	logs := requireContentModerationLogCount(t, repo, 2)
	require.Equal(t, 2, logs[1].ViolationCount)
	require.True(t, logs[1].AutoBanned)
	require.Len(t, userRepo.updated, 1)
	require.Equal(t, StatusDisabled, userRepo.user.Status)
	require.Equal(t, []int64{userID}, invalidator.userIDs)
}

func TestContentModerationAdminBelowBanThresholdRecordsViolationOnly(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 2
	cfg.ViolationWindowHours = 24

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleAdmin, Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, invalidator, nil)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)

	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, 1, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Empty(t, userRepo.updated)
	require.Empty(t, invalidator.userIDs)
}

func newContentModerationFlaggedLog(userID int64) *ContentModerationLog {
	return &ContentModerationLog{
		UserID:          &userID,
		Action:          ContentModerationActionBlock,
		Flagged:         true,
		HighestCategory: "sexual",
		HighestScore:    0.9,
		CreatedAt:       time.Now(),
	}
}

func TestContentModerationCheck_PreBlockFlaggedWritesRedisHashCache(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.PreHashCheckEnabled = true
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.BlockStatus = http.StatusConflict
	cfg.BlockMessage = "命中风险输入"
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		hashCache,
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"repeat blocked prompt"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Equal(t, 1, requestCount)
	recorded := requireRecordedHashCount(t, hashCache, 1)
	requireContentModerationLogCount(t, repo, 1)

	decision, err = svc.Check(context.Background(), ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionHashBlock, decision.Action)
	require.Equal(t, recorded[0], decision.InputHash)
	require.Equal(t, 1, requestCount)
	logs := requireContentModerationLogCount(t, repo, 2)
	require.Equal(t, ContentModerationActionBlock, logs[0].Action)
	require.Equal(t, ContentModerationActionHashBlock, logs[1].Action)
}

func TestContentModerationDeleteFlaggedInputHash_NormalizesAndDeletes(t *testing.T) {
	existingHash := strings.Repeat("a", 64)
	hashCache := &contentModerationTestHashCache{hashes: map[string]struct{}{
		existingHash: {},
	}}
	svc := &ContentModerationService{hashCache: hashCache}

	result, err := svc.DeleteFlaggedInputHash(context.Background(), strings.ToUpper(existingHash))

	require.NoError(t, err)
	require.Equal(t, existingHash, result.InputHash)
	require.True(t, result.Deleted)
	require.False(t, hashCache.hasHash(existingHash))
	require.Equal(t, []string{existingHash}, hashCache.snapshotDeleted())

	result, err = svc.DeleteFlaggedInputHash(context.Background(), existingHash)

	require.NoError(t, err)
	require.Equal(t, existingHash, result.InputHash)
	require.False(t, result.Deleted)
}

func TestContentModerationClearFlaggedInputHashesAndStatusCount(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	hashCache := &contentModerationTestHashCache{hashes: map[string]struct{}{
		strings.Repeat("a", 64): {},
		strings.Repeat("b", 64): {},
	}}
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		hashCache: hashCache,
		keyHealth: make(map[string]*contentModerationKeyHealth),
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), status.FlaggedHashCount)

	result, err := svc.ClearFlaggedInputHashes(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Deleted)

	status, err = svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), status.FlaggedHashCount)
}

func TestContentModerationCheck_AsyncFlaggedWritesRedisHashCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		hashCache,
		nil,
		nil,
		nil,
		nil,
	)

	decision := svc.checkSync(context.Background(), ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"bad prompt"}]}`),
	}, cfg, ContentModerationInput{Text: "bad prompt"}, strings.Repeat("b", 64), contentModerationIntPtr(25), false)

	require.False(t, decision.Blocked)
	requireRecordedHashCount(t, hashCache, 1)
	requireContentModerationLogCount(t, repo, 1)
}

func TestBuildContentModerationAccountDisabledEmailBody_ContainsBanDetails(t *testing.T) {
	userID := int64(1001)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 10
	body := buildContentModerationAccountDisabledEmailBody("Sub2API <Admin>", &ContentModerationLog{
		UserID:          &userID,
		UserEmail:       "user@example.com",
		GroupName:       "vip_2",
		HighestCategory: "sexual",
		HighestScore:    0.926,
		ViolationCount:  10,
	}, cfg)

	require.Contains(t, body, "账户已被自动禁用")
	require.Contains(t, body, "封禁详情")
	require.Contains(t, body, "账户当前处于封禁状态，所有 API 请求将被拒绝")
	require.Contains(t, body, "10 次（阈值 10）")
	require.Contains(t, body, "sexual / 0.926")
	require.Contains(t, body, "Sub2API &lt;Admin&gt;")
}

func TestContentModerationUnbanUser_ActivatesUserAndInvalidatesAuthCache(t *testing.T) {
	userRepo := &contentModerationTestUserRepo{user: &User{ID: 1001, Email: "user@example.com", Status: StatusDisabled}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, invalidator, nil)

	result, err := svc.UnbanUser(context.Background(), 1001)

	require.NoError(t, err)
	require.Equal(t, int64(1001), result.UserID)
	require.Equal(t, StatusActive, result.Status)
	require.Len(t, userRepo.updated, 1)
	require.Equal(t, StatusActive, userRepo.updated[0].Status)
	require.Equal(t, []int64{1001}, invalidator.userIDs)
}

func TestContentModerationUnbanUser_ActiveUserOnlyInvalidatesAuthCache(t *testing.T) {
	userRepo := &contentModerationTestUserRepo{user: &User{ID: 1001, Email: "user@example.com", Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, invalidator, nil)

	result, err := svc.UnbanUser(context.Background(), 1001)

	require.NoError(t, err)
	require.Equal(t, StatusActive, result.Status)
	require.Empty(t, userRepo.updated)
	require.Equal(t, []int64{1001}, invalidator.userIDs)
}

func contentModerationIntPtr(v int) *int {
	return &v
}

func TestContentModerationUpdateConfig_CyberPolicyExcludeFromBanCount(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)

	// 默认值必须是 false（计入，保持现状）
	view, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.False(t, view.CyberPolicyExcludeFromBanCount, "默认必须计入封号计数")

	// 指针式部分更新为 true
	exclude := true
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		CyberPolicyExcludeFromBanCount: &exclude,
	})
	require.NoError(t, err)
	require.True(t, view.CyberPolicyExcludeFromBanCount)

	// 持久化 JSON 含字段
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(settingRepo.values[SettingKeyContentModerationConfig]), &saved))
	require.True(t, saved.CyberPolicyExcludeFromBanCount)

	// 二次读取（从持久化 JSON 反序列化）roundtrip
	view, err = svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.True(t, view.CyberPolicyExcludeFromBanCount)

	// 不传该字段的更新不得改动它（指针 nil = 保留）
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{})
	require.NoError(t, err)
	require.True(t, view.CyberPolicyExcludeFromBanCount)

	// 主动回拨 false 必须生效（防止未来误加 if val 保护逻辑）
	revert := false
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		CyberPolicyExcludeFromBanCount: &revert,
	})
	require.NoError(t, err)
	require.False(t, view.CyberPolicyExcludeFromBanCount)
}
