package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

const (
	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"

	contentModerationAPIKeysModeAppend  = "append"
	contentModerationAPIKeysModeReplace = "replace"

	ContentModerationActionAllow        = "allow"
	ContentModerationActionBlock        = "block"
	ContentModerationActionHashBlock    = "hash_block"
	ContentModerationActionKeywordBlock = "keyword_block"
	ContentModerationActionError        = "error"
	ContentModerationActionCyberPolicy  = "cyber_policy" // cyber_policy 硬阻断的风控日志 action（封号计数排除按此值过滤）

	contentModerationKeywordCategory = "keyword"

	ContentModerationKeywordModeKeywordOnly   = "keyword_only"
	ContentModerationKeywordModeKeywordAndAPI = "keyword_and_api"
	ContentModerationKeywordModeAPIOnly       = "api_only"

	ContentModerationModelFilterAll     = "all"
	ContentModerationModelFilterInclude = "include"
	ContentModerationModelFilterExclude = "exclude"

	ContentModerationProtocolAnthropicMessages = "anthropic_messages"
	ContentModerationProtocolOpenAIResponses   = "openai_responses"
	ContentModerationProtocolOpenAIChat        = "openai_chat_completions"
	ContentModerationProtocolGemini            = "gemini"
	ContentModerationProtocolOpenAIImages      = "openai_images"

	defaultContentModerationBaseURL   = "https://api.openai.com"
	defaultContentModerationModel     = "omni-moderation-latest"
	defaultContentModerationTimeoutMS = 3000
	maxContentModerationTimeoutMS     = 30000
	maxModerationInputRunes           = 12000
	maxModerationExcerptRunes         = 240

	defaultContentModerationWorkerCount          = 4
	maxContentModerationWorkerCount              = 32
	defaultContentModerationQueueSize            = 32768
	maxContentModerationQueueSize                = 100000
	defaultContentModerationBanThreshold         = 10
	defaultContentModerationViolationWindowHours = 720
	defaultContentModerationBlockHTTPStatus      = http.StatusForbidden
	defaultContentModerationBlockMessage         = "内容审计命中风险规则，请调整输入后重试"
	defaultContentModerationRetryCount           = 2
	maxContentModerationRetryCount               = 5
	defaultContentModerationHitRetentionDays     = 180
	defaultContentModerationNonHitRetentionDays  = 3
	maxContentModerationRetentionDays            = 3650
	maxContentModerationNonHitRetentionDays      = 3
	contentModerationKeyRateLimitFreezeDuration  = time.Minute
	contentModerationKeyAuthFreezeDuration       = 10 * time.Minute
	contentModerationKeyHTTPErrorFreezeDuration  = 10 * time.Second
	maxContentModerationInputImages              = 1
	maxContentModerationTestImages               = maxContentModerationInputImages
	maxContentModerationTestImageBytes           = 8 * 1024 * 1024
	maxContentModerationTestImageDataURLBytes    = 12 * 1024 * 1024
	maxContentModerationBlockedKeywords          = 10000
	maxContentModerationBlockedKeywordRunes      = 200
	maxContentModerationModelFilterModels        = 1000
	maxContentModerationModelFilterRunes         = 200
	maxContentModerationKeyHealthEntries         = 1024
	contentModerationQueueRetainedCapacity       = 256
	contentModerationQueueByteLimit              = 64 * 1024 * 1024
	contentModerationTaskByteLimit               = 16 * 1024 * 1024

	contentModerationCleanupInterval       = 24 * time.Hour
	contentModerationCleanupTimeout        = 30 * time.Minute
	contentModerationCleanupDelay          = 5 * time.Minute
	contentModerationSettingsCacheTTL      = 5 * time.Second
	contentModerationSettingsErrorRetryTTL = 500 * time.Millisecond
	contentModerationSettingsLoadTimeout   = 2 * time.Second
	contentModerationRuntimeCacheTTL       = time.Second
	contentModerationRuntimeRefreshTimeout = 5 * time.Second
)

var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

type ContentModerationConfig struct {
	Enabled              bool                         `json:"enabled"`
	Mode                 string                       `json:"mode"`
	BaseURL              string                       `json:"base_url"`
	Model                string                       `json:"model"`
	APIKey               string                       `json:"api_key,omitempty"`
	APIKeys              []string                     `json:"api_keys,omitempty"`
	TimeoutMS            int                          `json:"timeout_ms"`
	SampleRate           int                          `json:"sample_rate"`
	AllGroups            bool                         `json:"all_groups"`
	GroupIDs             []int64                      `json:"group_ids"`
	RecordNonHits        bool                         `json:"record_non_hits"`
	Thresholds           map[string]float64           `json:"thresholds"`
	WorkerCount          int                          `json:"worker_count"`
	QueueSize            int                          `json:"queue_size"`
	BlockStatus          int                          `json:"block_status"`
	BlockMessage         string                       `json:"block_message"`
	EmailOnHit           bool                         `json:"email_on_hit"`
	AutoBanEnabled       bool                         `json:"auto_ban_enabled"`
	BanThreshold         int                          `json:"ban_threshold"`
	ViolationWindowHours int                          `json:"violation_window_hours"`
	RetryCount           int                          `json:"retry_count"`
	HitRetentionDays     int                          `json:"hit_retention_days"`
	NonHitRetentionDays  int                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled  bool                         `json:"pre_hash_check_enabled"`
	BlockedKeywords      []string                     `json:"blocked_keywords"`
	KeywordBlockingMode  string                       `json:"keyword_blocking_mode"`
	ModelFilter          ContentModerationModelFilter `json:"model_filter"`
	// CyberPolicyExcludeFromBanCount 为 true 时，cyber_policy 命中不参与自动封号计数：
	// 当次不判定封号，且历史 cyber 行在 CountFlaggedByUserSince 中被排除。
	// 默认 false（计入，与历史行为一致；旧配置 JSON 无此字段时反序列化为 false）。
	CyberPolicyExcludeFromBanCount bool `json:"cyber_policy_exclude_from_ban_count"`
	runtimeRevision                uint64
}

type ContentModerationConfigView struct {
	Enabled                        bool                            `json:"enabled"`
	Mode                           string                          `json:"mode"`
	BaseURL                        string                          `json:"base_url"`
	Model                          string                          `json:"model"`
	APIKeyConfigured               bool                            `json:"api_key_configured"`
	APIKeyMasked                   string                          `json:"api_key_masked"`
	APIKeyCount                    int                             `json:"api_key_count"`
	APIKeyMasks                    []string                        `json:"api_key_masks"`
	APIKeyStatuses                 []ContentModerationAPIKeyStatus `json:"api_key_statuses"`
	TimeoutMS                      int                             `json:"timeout_ms"`
	SampleRate                     int                             `json:"sample_rate"`
	AllGroups                      bool                            `json:"all_groups"`
	GroupIDs                       []int64                         `json:"group_ids"`
	RecordNonHits                  bool                            `json:"record_non_hits"`
	Thresholds                     map[string]float64              `json:"thresholds"`
	WorkerCount                    int                             `json:"worker_count"`
	QueueSize                      int                             `json:"queue_size"`
	BlockStatus                    int                             `json:"block_status"`
	BlockMessage                   string                          `json:"block_message"`
	EmailOnHit                     bool                            `json:"email_on_hit"`
	AutoBanEnabled                 bool                            `json:"auto_ban_enabled"`
	BanThreshold                   int                             `json:"ban_threshold"`
	ViolationWindowHours           int                             `json:"violation_window_hours"`
	RetryCount                     int                             `json:"retry_count"`
	HitRetentionDays               int                             `json:"hit_retention_days"`
	NonHitRetentionDays            int                             `json:"non_hit_retention_days"`
	PreHashCheckEnabled            bool                            `json:"pre_hash_check_enabled"`
	BlockedKeywords                []string                        `json:"blocked_keywords"`
	KeywordBlockingMode            string                          `json:"keyword_blocking_mode"`
	ModelFilter                    ContentModerationModelFilter    `json:"model_filter"`
	CyberPolicyExcludeFromBanCount bool                            `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationAPIKeyStatus struct {
	Index          int        `json:"index"`
	KeyHash        string     `json:"key_hash"`
	Masked         string     `json:"masked"`
	Status         string     `json:"status"`
	FailureCount   int        `json:"failure_count"`
	SuccessCount   int64      `json:"success_count"`
	LastError      string     `json:"last_error"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	LastLatencyMS  int        `json:"last_latency_ms"`
	LastHTTPStatus int        `json:"last_http_status"`
	LastTested     bool       `json:"last_tested"`
	Configured     bool       `json:"configured"`
}

type ContentModerationAPIKeyLoad struct {
	Index          int    `json:"index"`
	KeyHash        string `json:"key_hash"`
	Masked         string `json:"masked"`
	Status         string `json:"status"`
	Active         int64  `json:"active"`
	Total          int64  `json:"total"`
	Success        int64  `json:"success"`
	Errors         int64  `json:"errors"`
	AvgLatencyMS   int64  `json:"avg_latency_ms"`
	LastLatencyMS  int    `json:"last_latency_ms"`
	LastHTTPStatus int    `json:"last_http_status"`
}

type TestContentModerationAPIKeysInput struct {
	APIKeys   []string `json:"api_keys"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type TestContentModerationAPIKeysResult struct {
	Items       []ContentModerationAPIKeyStatus   `json:"items"`
	AuditResult *ContentModerationTestAuditResult `json:"audit_result,omitempty"`
	ImageCount  int                               `json:"image_count"`
}

type ContentModerationTestAuditResult struct {
	Flagged         bool               `json:"flagged"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CompositeScore  float64            `json:"composite_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Thresholds      map[string]float64 `json:"thresholds"`
}

type UpdateContentModerationConfigInput struct {
	Enabled                        *bool                         `json:"enabled"`
	Mode                           *string                       `json:"mode"`
	BaseURL                        *string                       `json:"base_url"`
	Model                          *string                       `json:"model"`
	APIKey                         *string                       `json:"api_key"`
	APIKeys                        *[]string                     `json:"api_keys"`
	APIKeysMode                    string                        `json:"api_keys_mode"`
	DeleteAPIKeyHashes             *[]string                     `json:"delete_api_key_hashes"`
	ClearAPIKey                    bool                          `json:"clear_api_key"`
	TimeoutMS                      *int                          `json:"timeout_ms"`
	SampleRate                     *int                          `json:"sample_rate"`
	AllGroups                      *bool                         `json:"all_groups"`
	GroupIDs                       *[]int64                      `json:"group_ids"`
	RecordNonHits                  *bool                         `json:"record_non_hits"`
	Thresholds                     *map[string]float64           `json:"thresholds"`
	WorkerCount                    *int                          `json:"worker_count"`
	QueueSize                      *int                          `json:"queue_size"`
	BlockStatus                    *int                          `json:"block_status"`
	BlockMessage                   *string                       `json:"block_message"`
	EmailOnHit                     *bool                         `json:"email_on_hit"`
	AutoBanEnabled                 *bool                         `json:"auto_ban_enabled"`
	BanThreshold                   *int                          `json:"ban_threshold"`
	ViolationWindowHours           *int                          `json:"violation_window_hours"`
	RetryCount                     *int                          `json:"retry_count"`
	HitRetentionDays               *int                          `json:"hit_retention_days"`
	NonHitRetentionDays            *int                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled            *bool                         `json:"pre_hash_check_enabled"`
	BlockedKeywords                *[]string                     `json:"blocked_keywords"`
	KeywordBlockingMode            *string                       `json:"keyword_blocking_mode"`
	ModelFilter                    *ContentModerationModelFilter `json:"model_filter"`
	CyberPolicyExcludeFromBanCount *bool                         `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationModelFilter struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

type ContentModerationCheckInput struct {
	RequestID  string
	UserID     int64
	UserEmail  string
	APIKeyID   int64
	APIKeyName string
	GroupID    *int64
	GroupName  string
	Endpoint   string
	Provider   string
	Model      string
	Protocol   string
	Body       []byte
}

type ContentModerationInput struct {
	Text   string
	Images []string
}

func (in *ContentModerationInput) Normalize() {
	if in == nil {
		return
	}
	in.Text = trimRunes(normalizeContentModerationText(in.Text), maxModerationInputRunes)
	in.Images = normalizeModerationImages(in.Images)
}

func (in ContentModerationInput) IsEmpty() bool {
	return strings.TrimSpace(in.Text) == "" && len(in.Images) == 0
}

func (in ContentModerationInput) ModerationInput() any {
	images := limitContentModerationImages(in.Images)
	if len(images) == 0 {
		return in.Text
	}
	parts := make([]moderationAPIInputPart, 0, len(images)+1)
	if strings.TrimSpace(in.Text) != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: in.Text})
	}
	for _, image := range images {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts
}

func (in ContentModerationInput) ExcerptText() string {
	return in.Text
}

func (in ContentModerationInput) Hash() string {
	h := sha256.New()
	_, _ = h.Write([]byte("text:"))
	_, _ = h.Write([]byte(in.Text))
	for _, image := range in.Images {
		imageHash := sha256.Sum256([]byte(image))
		_, _ = h.Write([]byte("\nimage:"))
		_, _ = h.Write([]byte(hex.EncodeToString(imageHash[:])))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type ContentModerationDecision struct {
	Allowed         bool               `json:"allowed"`
	Blocked         bool               `json:"blocked"`
	Flagged         bool               `json:"flagged"`
	Message         string             `json:"message"`
	StatusCode      int                `json:"status_code"`
	InputHash       string             `json:"input_hash,omitempty"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Action          string             `json:"action"`
}

type ContentModerationLog struct {
	ID                int64              `json:"id"`
	RequestID         string             `json:"request_id"`
	UserID            *int64             `json:"user_id,omitempty"`
	UserEmail         string             `json:"user_email"`
	APIKeyID          *int64             `json:"api_key_id,omitempty"`
	APIKeyName        string             `json:"api_key_name"`
	GroupID           *int64             `json:"group_id,omitempty"`
	GroupName         string             `json:"group_name"`
	Endpoint          string             `json:"endpoint"`
	Provider          string             `json:"provider"`
	Model             string             `json:"model"`
	Mode              string             `json:"mode"`
	Action            string             `json:"action"`
	Flagged           bool               `json:"flagged"`
	HighestCategory   string             `json:"highest_category"`
	HighestScore      float64            `json:"highest_score"`
	MatchedKeyword    string             `json:"matched_keyword"`
	CategoryScores    map[string]float64 `json:"category_scores"`
	ThresholdSnapshot map[string]float64 `json:"threshold_snapshot"`
	InputExcerpt      string             `json:"input_excerpt"`
	UpstreamLatencyMS *int               `json:"upstream_latency_ms,omitempty"`
	Error             string             `json:"error"`
	ViolationCount    int                `json:"violation_count"`
	AutoBanned        bool               `json:"auto_banned"`
	EmailSent         bool               `json:"email_sent"`
	UserStatus        string             `json:"user_status"`
	QueueDelayMS      *int               `json:"queue_delay_ms,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
}

type ContentModerationLogFilter struct {
	Pagination pagination.PaginationParams
	Result     string
	GroupID    *int64
	Endpoint   string
	Search     string
	From       *time.Time
	To         *time.Time
}

type ContentModerationCleanupResult struct {
	DeletedHit    int64     `json:"deleted_hit"`
	DeletedNonHit int64     `json:"deleted_non_hit"`
	FinishedAt    time.Time `json:"finished_at"`
}

type ContentModerationRuntimeStatus struct {
	Enabled                      bool                            `json:"enabled"`
	RiskControlEnabled           bool                            `json:"risk_control_enabled"`
	Mode                         string                          `json:"mode"`
	WorkerCount                  int                             `json:"worker_count"`
	MaxWorkers                   int                             `json:"max_workers"`
	ActiveWorkers                int                             `json:"active_workers"`
	IdleWorkers                  int                             `json:"idle_workers"`
	QueueSize                    int                             `json:"queue_size"`
	QueueLength                  int                             `json:"queue_length"`
	QueueUsagePercent            float64                         `json:"queue_usage_percent"`
	Enqueued                     int64                           `json:"enqueued"`
	Dropped                      int64                           `json:"dropped"`
	Processed                    int64                           `json:"processed"`
	Errors                       int64                           `json:"errors"`
	PreBlockActive               int                             `json:"pre_block_active"`
	PreBlockChecked              int64                           `json:"pre_block_checked"`
	PreBlockAllowed              int64                           `json:"pre_block_allowed"`
	PreBlockBlocked              int64                           `json:"pre_block_blocked"`
	PreBlockErrors               int64                           `json:"pre_block_errors"`
	PreBlockAvgLatencyMS         int64                           `json:"pre_block_avg_latency_ms"`
	PreBlockAPIKeyActive         int64                           `json:"pre_block_api_key_active"`
	PreBlockAPIKeyAvailableCount int64                           `json:"pre_block_api_key_available_count"`
	PreBlockAPIKeyTotalCalls     int64                           `json:"pre_block_api_key_total_calls"`
	PreBlockAPIKeyLoads          []ContentModerationAPIKeyLoad   `json:"pre_block_api_key_loads"`
	APIKeyStatuses               []ContentModerationAPIKeyStatus `json:"api_key_statuses"`
	FlaggedHashCount             int64                           `json:"flagged_hash_count"`
	LastCleanupAt                *time.Time                      `json:"last_cleanup_at,omitempty"`
	LastCleanupDeletedHit        int64                           `json:"last_cleanup_deleted_hit"`
	LastCleanupDeletedNonHit     int64                           `json:"last_cleanup_deleted_non_hit"`
}

type ContentModerationUnbanUserResult struct {
	UserID int64  `json:"user_id"`
	Status string `json:"status"`
}

type ContentModerationDeleteHashResult struct {
	InputHash string `json:"input_hash"`
	Deleted   bool   `json:"deleted"`
}

type ContentModerationClearHashesResult struct {
	Deleted int64 `json:"deleted"`
}

type ContentModerationRepository interface {
	CreateLog(ctx context.Context, log *ContentModerationLog) error
	ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error)
	// CountFlaggedByUserSince 统计窗口内计入封号的违规次数（排除 hash_block；
	// excludeCyberPolicy 为 true 时额外排除 cyber_policy 行）。
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error)
	CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*ContentModerationCleanupResult, error)
	// UpdateLogEmailSent 回写邮件发送结果（F7：CreateLog 先行后补 EmailSent）。
	UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error
}

type ContentModerationHashCache interface {
	RecordFlaggedInputHash(ctx context.Context, inputHash string) error
	HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	ClearFlaggedInputHashes(ctx context.Context) (int64, error)
	CountFlaggedInputHashes(ctx context.Context) (int64, error)
}

type contentModerationConfigCacheEntry struct {
	cfg       *ContentModerationConfig
	err       error
	expiresAt int64
}

type contentModerationRiskControlSnapshot struct {
	enabled   bool
	revision  uint64
	expiresAt int64
}

type ContentModerationService struct {
	settingRepo              SettingRepository
	repo                     ContentModerationRepository
	hashCache                ContentModerationHashCache
	groupRepo                GroupRepository
	userRepo                 UserRepository
	authCacheInvalidator     APIKeyAuthCacheInvalidator
	emailService             *EmailService
	httpClient               *http.Client
	asyncQueue               *contentModerationTaskQueue
	runtimeMu                sync.Mutex
	workerStops              map[uint64]*contentModerationWorkerControl
	desiredWorkers           int
	nextWorkerID             uint64
	runtimeConfigRevision    uint64
	runtimePaused            atomic.Bool
	stopCh                   chan struct{}
	serviceCtx               context.Context
	serviceCancel            context.CancelFunc
	backgroundWG             sync.WaitGroup
	runtimeClosed            bool
	closeOnce                sync.Once
	configLoadMu             sync.Mutex
	configCache              atomic.Pointer[contentModerationConfigCacheEntry]
	configRevision           uint64
	riskControlRefreshMu     sync.Mutex
	riskControlCache         atomic.Pointer[contentModerationRiskControlSnapshot]
	apiKeyCursor             atomic.Uint64
	asyncActive              atomic.Int64
	asyncEnqueued            atomic.Int64
	asyncDropped             atomic.Int64
	asyncProcessed           atomic.Int64
	asyncErrors              atomic.Int64
	preBlockActive           atomic.Int64
	preBlockChecked          atomic.Int64
	preBlockAllowed          atomic.Int64
	preBlockBlocked          atomic.Int64
	preBlockErrors           atomic.Int64
	preBlockLatencyTotalMS   atomic.Int64
	lastCleanupUnix          atomic.Int64
	lastCleanupDeletedHit    atomic.Int64
	lastCleanupDeletedNonHit atomic.Int64
	runtimeSnapshot          atomic.Pointer[contentModerationRuntimeSnapshot]
	runtimeRefreshMu         sync.Mutex
	runtimeRefreshPending    atomic.Bool
	runtimeCacheTTL          time.Duration
	runtimeRefreshRetryAt    atomic.Int64
	keyHealthMu              sync.Mutex
	keyHealth                map[string]*contentModerationKeyHealth
}

type contentModerationRuntimeSnapshot struct {
	riskControlEnabled bool
	config             *ContentModerationConfig
	keywordMatcher     *contentModerationKeywordMatcher
	configDigest       [sha256.Size]byte
	loadedAt           time.Time
}

type contentModerationTask struct {
	input            ContentModerationCheckInput
	content          ContentModerationInput
	inputHash        string
	log              *ContentModerationLog
	config           *ContentModerationConfig
	recordHash       bool
	applySideEffects bool
	enqueuedAt       time.Time
	retainedBytes    int64
}

type contentModerationWorkerControl struct {
	ctx      context.Context
	cancel   context.CancelFunc
	stopping bool
}

// contentModerationTaskQueue is a dynamically growing bounded queue. Unlike a
// buffered channel, an idle service does not reserve memory for the configured
// maximum number of tasks.
type contentModerationTaskQueue struct {
	mu               sync.Mutex
	items            []contentModerationTask
	head             int
	limit            int
	draining         bool
	closed           bool
	drainEpoch       uint64
	bytes            int64
	byteLimit        int64
	taskByteLimit    int64
	reservedSlots    int
	reservedRecords  int
	reservationsDone chan struct{}
	notify           chan struct{}
}

type contentModerationTaskReservation struct {
	queue         *contentModerationTaskQueue
	retainedBytes int64
	drainEpoch    uint64
	record        bool
	done          bool
}

type contentModerationDequeueResult uint8

const (
	contentModerationDequeueStopped contentModerationDequeueResult = iota
	contentModerationDequeueTask
	contentModerationDequeueDrained
)

func newContentModerationTaskQueue(limit int) *contentModerationTaskQueue {
	if limit <= 0 {
		limit = defaultContentModerationQueueSize
	}
	return &contentModerationTaskQueue{
		limit:         limit,
		byteLimit:     contentModerationQueueByteLimit,
		taskByteLimit: contentModerationTaskByteLimit,
		notify:        make(chan struct{}, 1),
	}
}

func (q *contentModerationTaskQueue) SetLimit(limit int) {
	if q == nil {
		return
	}
	if limit <= 0 {
		limit = defaultContentModerationQueueSize
	}
	q.mu.Lock()
	if q.limit == limit {
		q.mu.Unlock()
		return
	}
	q.limit = limit
	size := len(q.items) - q.head
	if size == 0 {
		q.items = nil
		q.head = 0
	} else if size <= limit && cap(q.items) > limit {
		items := make([]contentModerationTask, size)
		copy(items, q.items[q.head:])
		q.items = items
		q.head = 0
	}
	q.mu.Unlock()
}

func (q *contentModerationTaskQueue) SetDraining(draining bool) {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.closed && !draining {
		q.mu.Unlock()
		return
	}
	changed := q.draining != draining
	q.draining = draining
	if changed && draining {
		q.drainEpoch++
	}
	if draining {
		q.dropObserveTasksLocked()
	}
	if draining && q.head == len(q.items) {
		q.items = nil
		q.head = 0
	}
	q.mu.Unlock()
	if changed {
		q.signal()
	}
}

func (q *contentModerationTaskQueue) Limit() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.limit
}

func (q *contentModerationTaskQueue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) - q.head
}

func (q *contentModerationTaskQueue) ReleaseIfEmpty() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.head == len(q.items) {
		q.items = nil
		q.head = 0
	}
	q.mu.Unlock()
}

func (q *contentModerationTaskQueue) Enqueue(task contentModerationTask) bool {
	reservation, ok := q.Reserve(&task)
	if !ok {
		return false
	}
	return reservation.Commit(task)
}

func (q *contentModerationTaskQueue) Reserve(task *contentModerationTask) (*contentModerationTaskReservation, bool) {
	if q == nil || task == nil {
		return nil, false
	}
	retainedBytes := estimateContentModerationTaskBytes(task)
	record := task.log != nil
	q.mu.Lock()
	if !q.canEnqueueLocked(retainedBytes) {
		q.mu.Unlock()
		return nil, false
	}
	q.reservedSlots++
	if record {
		q.reservedRecords++
	}
	q.bytes += retainedBytes
	drainEpoch := q.drainEpoch
	q.mu.Unlock()
	return &contentModerationTaskReservation{
		queue:         q,
		retainedBytes: retainedBytes,
		drainEpoch:    drainEpoch,
		record:        record,
	}, true
}

func (q *contentModerationTaskQueue) canEnqueueLocked(retainedBytes int64) bool {
	size := len(q.items) - q.head + q.reservedSlots
	byteLimit := q.byteLimit
	if byteLimit <= 0 {
		byteLimit = contentModerationQueueByteLimit
	}
	taskByteLimit := q.taskByteLimit
	if taskByteLimit <= 0 {
		taskByteLimit = contentModerationTaskByteLimit
	}
	return !q.closed && !q.draining && size < q.limit && retainedBytes <= taskByteLimit && retainedBytes <= byteLimit && q.bytes <= byteLimit-retainedBytes
}

func (r *contentModerationTaskReservation) Commit(task contentModerationTask) bool {
	if r == nil || r.queue == nil {
		return false
	}
	q := r.queue
	q.mu.Lock()
	if r.done {
		q.mu.Unlock()
		return false
	}
	r.done = true
	if q.reservedSlots > 0 {
		q.reservedSlots--
	}
	if r.record && q.reservedRecords > 0 {
		q.reservedRecords--
	}
	q.notifyReservationsDoneLocked()
	if q.closed || (!r.record && (q.draining || r.drainEpoch != q.drainEpoch)) {
		q.bytes -= r.retainedBytes
		if q.bytes < 0 {
			q.bytes = 0
		}
		q.mu.Unlock()
		q.signal()
		return false
	}
	task.retainedBytes = r.retainedBytes
	size := len(q.items) - q.head
	if q.head > 0 && (len(q.items) == cap(q.items) || q.head >= len(q.items)/2) {
		copy(q.items, q.items[q.head:])
		clear(q.items[size:])
		q.items = q.items[:size]
		q.head = 0
	}
	q.items = append(q.items, task)
	q.mu.Unlock()
	q.signal()
	return true
}

func (r *contentModerationTaskReservation) Cancel() {
	if r == nil || r.queue == nil {
		return
	}
	q := r.queue
	q.mu.Lock()
	if r.done {
		q.mu.Unlock()
		return
	}
	r.done = true
	if q.reservedSlots > 0 {
		q.reservedSlots--
	}
	if r.record && q.reservedRecords > 0 {
		q.reservedRecords--
	}
	q.notifyReservationsDoneLocked()
	q.bytes -= r.retainedBytes
	if q.bytes < 0 {
		q.bytes = 0
	}
	q.mu.Unlock()
	q.signal()
}

func (q *contentModerationTaskQueue) Dequeue(workerStop <-chan struct{}, serviceStop <-chan struct{}) (contentModerationTask, contentModerationDequeueResult) {
	var zero contentModerationTask
	if q == nil {
		return zero, contentModerationDequeueStopped
	}
	for {
		select {
		case <-workerStop:
			q.signalIfActionable()
			return zero, contentModerationDequeueStopped
		case <-serviceStop:
			return zero, contentModerationDequeueStopped
		default:
		}
		q.mu.Lock()
		if q.head < len(q.items) {
			task := q.items[q.head]
			q.items[q.head] = zero
			q.head++
			hasMore := q.head < len(q.items)
			if !hasMore {
				if q.draining || cap(q.items) > contentModerationQueueRetainedCapacity {
					q.items = nil
				} else {
					q.items = q.items[:0]
				}
				q.head = 0
			}
			q.mu.Unlock()
			if hasMore {
				q.signal()
			}
			return task, contentModerationDequeueTask
		}
		if q.draining {
			if q.reservedRecords > 0 {
				q.mu.Unlock()
				select {
				case <-q.notify:
				case <-workerStop:
					q.signalIfActionable()
					return zero, contentModerationDequeueStopped
				case <-serviceStop:
					return zero, contentModerationDequeueStopped
				}
				continue
			}
			q.items = nil
			q.head = 0
			select {
			case <-q.notify:
			default:
			}
			q.mu.Unlock()
			return zero, contentModerationDequeueDrained
		}
		q.mu.Unlock()

		select {
		case <-q.notify:
		case <-workerStop:
			q.signalIfActionable()
			return zero, contentModerationDequeueStopped
		case <-serviceStop:
			return zero, contentModerationDequeueStopped
		}
	}
}

func (q *contentModerationTaskQueue) IsDrained() bool {
	if q == nil {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.draining && q.head == len(q.items) && q.reservedRecords == 0
}

func (q *contentModerationTaskQueue) HasDrainWork() bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.head < len(q.items) || q.reservedRecords > 0
}

func (q *contentModerationTaskQueue) Complete(retainedBytes int64) {
	if q == nil || retainedBytes <= 0 {
		return
	}
	q.mu.Lock()
	q.bytes -= retainedBytes
	if q.bytes < 0 {
		q.bytes = 0
	}
	q.mu.Unlock()
}

func (q *contentModerationTaskQueue) DropAll() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.closed = true
	q.draining = true
	q.drainEpoch++
	if q.reservedSlots > 0 && q.reservationsDone == nil {
		q.reservationsDone = make(chan struct{})
	}
	for i := q.head; i < len(q.items); i++ {
		q.bytes -= q.items[i].retainedBytes
		q.items[i] = contentModerationTask{}
	}
	if q.bytes < 0 {
		q.bytes = 0
	}
	q.items = nil
	q.head = 0
	q.mu.Unlock()
	q.signal()
}

func (q *contentModerationTaskQueue) WaitReservations() {
	if q == nil {
		return
	}
	for {
		q.mu.Lock()
		if q.reservedSlots == 0 {
			q.mu.Unlock()
			return
		}
		if q.reservationsDone == nil {
			q.reservationsDone = make(chan struct{})
		}
		done := q.reservationsDone
		q.mu.Unlock()
		<-done
	}
}

func (q *contentModerationTaskQueue) notifyReservationsDoneLocked() {
	if q.reservedSlots == 0 && q.reservationsDone != nil {
		close(q.reservationsDone)
		q.reservationsDone = nil
	}
}

func (q *contentModerationTaskQueue) dropObserveTasksLocked() {
	if q.head >= len(q.items) {
		return
	}
	write := 0
	for i := q.head; i < len(q.items); i++ {
		task := q.items[i]
		q.items[i] = contentModerationTask{}
		if task.log == nil {
			q.bytes -= task.retainedBytes
			continue
		}
		q.items[write] = task
		write++
	}
	clear(q.items[write:])
	q.items = q.items[:write]
	q.head = 0
	if q.bytes < 0 {
		q.bytes = 0
	}
}

func (q *contentModerationTaskQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *contentModerationTaskQueue) signalIfActionable() {
	q.mu.Lock()
	actionable := q.head < len(q.items) || q.draining
	q.mu.Unlock()
	if actionable {
		q.signal()
	}
}

func estimateContentModerationTaskBytes(task *contentModerationTask) int64 {
	if task == nil {
		return 0
	}
	n := int64(len(task.input.Body))
	n += int64(len(task.content.Text))
	n += int64(len(task.inputHash))
	n += contentModerationStringBytes(
		task.input.RequestID,
		task.input.UserEmail,
		task.input.APIKeyName,
		task.input.GroupName,
		task.input.Endpoint,
		task.input.Provider,
		task.input.Model,
		task.input.Protocol,
	)
	for _, image := range task.content.Images {
		n += int64(len(image))
	}
	if cfg := task.config; cfg != nil {
		n += contentModerationStringBytes(cfg.BaseURL, cfg.Model, cfg.APIKey, cfg.Mode)
		for _, key := range cfg.APIKeys {
			n += int64(len(key))
		}
		for category := range cfg.Thresholds {
			n += int64(len(category)) + 8
		}
	}
	if log := task.log; log != nil {
		n += contentModerationStringBytes(
			log.RequestID,
			log.UserEmail,
			log.APIKeyName,
			log.GroupName,
			log.Endpoint,
			log.Provider,
			log.Model,
			log.Mode,
			log.Action,
			log.HighestCategory,
			log.InputExcerpt,
			log.Error,
			log.MatchedKeyword,
			log.UserStatus,
		)
		for category := range log.CategoryScores {
			n += int64(len(category)) + 8
		}
		for category := range log.ThresholdSnapshot {
			n += int64(len(category)) + 8
		}
	}
	return n
}

func contentModerationStringBytes(values ...string) int64 {
	var n int64
	for _, value := range values {
		n += int64(len(value))
	}
	return n
}

type contentModerationKeyHealth struct {
	Hash           string
	Masked         string
	FailureCount   int
	SuccessCount   int64
	LastError      string
	LastCheckedAt  time.Time
	FrozenUntil    time.Time
	LastLatencyMS  int
	LastHTTPStatus int
	LastTested     bool
	SyncActive     int64
	SyncTotal      int64
	SyncSuccess    int64
	SyncErrors     int64
	SyncLatencyMS  int64
}

func NewContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
) *ContentModerationService {
	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	svc := &ContentModerationService{
		settingRepo:          settingRepo,
		repo:                 repo,
		hashCache:            hashCache,
		groupRepo:            groupRepo,
		userRepo:             userRepo,
		authCacheInvalidator: authCacheInvalidator,
		emailService:         emailService,
		httpClient:           servertiming.InstrumentClient(nil),
		stopCh:               make(chan struct{}),
		serviceCtx:           serviceCtx,
		serviceCancel:        serviceCancel,
		keyHealth:            make(map[string]*contentModerationKeyHealth),
	}
	if settingRepo != nil && repo != nil {
		svc.backgroundWG.Add(1)
		go func() {
			defer svc.backgroundWG.Done()
			svc.cleanupWorker()
		}()
	}
	svc.runtimePaused.Store(true)
	return svc
}

func (s *ContentModerationService) GetConfig(ctx context.Context) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.configView(cfg), nil
}

func (s *ContentModerationService) UpdateConfig(ctx context.Context, input UpdateContentModerationConfigInput) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if input.Enabled != nil {
		cfg.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		cfg.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		cfg.Model = strings.TrimSpace(*input.Model)
	}
	if input.TimeoutMS != nil {
		cfg.TimeoutMS = *input.TimeoutMS
	}
	if input.SampleRate != nil {
		cfg.SampleRate = *input.SampleRate
	}
	if input.WorkerCount != nil {
		cfg.WorkerCount = *input.WorkerCount
	}
	if input.QueueSize != nil {
		cfg.QueueSize = *input.QueueSize
	}
	if input.BlockStatus != nil {
		cfg.BlockStatus = *input.BlockStatus
	}
	if input.BlockMessage != nil {
		cfg.BlockMessage = strings.TrimSpace(*input.BlockMessage)
	}
	if input.EmailOnHit != nil {
		cfg.EmailOnHit = *input.EmailOnHit
	}
	if input.AutoBanEnabled != nil {
		cfg.AutoBanEnabled = *input.AutoBanEnabled
	}
	if input.BanThreshold != nil {
		cfg.BanThreshold = *input.BanThreshold
	}
	if input.ViolationWindowHours != nil {
		cfg.ViolationWindowHours = *input.ViolationWindowHours
	}
	if input.RetryCount != nil {
		cfg.RetryCount = *input.RetryCount
	}
	if input.HitRetentionDays != nil {
		cfg.HitRetentionDays = *input.HitRetentionDays
	}
	if input.NonHitRetentionDays != nil {
		cfg.NonHitRetentionDays = *input.NonHitRetentionDays
	}
	if input.PreHashCheckEnabled != nil {
		cfg.PreHashCheckEnabled = *input.PreHashCheckEnabled
	}
	if input.BlockedKeywords != nil {
		cfg.BlockedKeywords = normalizeBlockedKeywords(*input.BlockedKeywords)
	}
	if input.KeywordBlockingMode != nil {
		cfg.KeywordBlockingMode = strings.TrimSpace(*input.KeywordBlockingMode)
	}
	if input.ModelFilter != nil {
		cfg.ModelFilter = *input.ModelFilter
	}
	if input.AllGroups != nil {
		cfg.AllGroups = *input.AllGroups
	}
	if input.GroupIDs != nil {
		cfg.GroupIDs = normalizeInt64IDs(*input.GroupIDs)
	}
	if input.RecordNonHits != nil {
		cfg.RecordNonHits = *input.RecordNonHits
	}
	if input.CyberPolicyExcludeFromBanCount != nil {
		cfg.CyberPolicyExcludeFromBanCount = *input.CyberPolicyExcludeFromBanCount
	}
	if input.Thresholds != nil {
		cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), *input.Thresholds)
	}
	if input.ClearAPIKey {
		cfg.APIKey = ""
		cfg.APIKeys = []string{}
	} else {
		apiKeysMode := normalizeContentModerationAPIKeysMode(input.APIKeysMode)
		if input.DeleteAPIKeyHashes != nil && apiKeysMode != contentModerationAPIKeysModeReplace {
			cfg.APIKeys = deleteModerationAPIKeysByHash(cfg.apiKeys(), *input.DeleteAPIKeyHashes)
			cfg.APIKey = ""
		}
		if input.APIKeys != nil {
			if apiKeysMode == contentModerationAPIKeysModeReplace {
				cfg.APIKeys = normalizeModerationAPIKeys(*input.APIKeys)
			} else {
				cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.apiKeys(), *input.APIKeys...))
			}
			cfg.APIKey = ""
		}
		if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
			cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, *input.APIKey))
			cfg.APIKey = ""
		}
	}
	if err := s.validateConfig(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal content moderation config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContentModerationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save content moderation config: %w", err)
	}
	publishedCfg := s.cacheConfig(cfg, nil)
	s.replaceRuntimeConfig(publishedCfg, raw)
	s.pruneAPIKeyHealth(publishedCfg.apiKeys())
	riskEnabled, riskRevision := s.riskControlState(ctx)
	if riskEnabled {
		s.syncAsyncRuntimeForRisk(publishedCfg, riskRevision)
	} else {
		s.pauseAsyncRuntimeForRisk(riskRevision)
	}
	return s.configView(publishedCfg), nil
}

func (s *ContentModerationService) TestAPIKeys(ctx context.Context, input TestContentModerationAPIKeysInput) (*TestContentModerationAPIKeysResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	configuredKeys := cfg.apiKeys()
	keys := normalizeModerationAPIKeys(input.APIKeys)
	configured := false
	if len(keys) == 0 {
		keys = configuredKeys
		configured = true
	} else {
		defer s.removeTemporaryAPIKeyHealth(keys, configuredKeys)
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		cfg.BaseURL = input.BaseURL
	}
	if strings.TrimSpace(input.Model) != "" {
		cfg.Model = input.Model
	}
	if input.TimeoutMS > 0 {
		cfg.TimeoutMS = input.TimeoutMS
	}
	cfg.normalize()
	testInput, imageCount, err := buildModerationTestInput(input.Prompt, input.Images)
	if err != nil {
		return nil, err
	}
	auditOnly := contentModerationTestHasAuditInput(input.Prompt, input.Images)
	if configured && auditOnly {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			return &TestContentModerationAPIKeysResult{
				Items:      s.apiKeyStatuses(keys),
				ImageCount: imageCount,
			}, nil
		}
		keys = []string{key}
	}
	if len(keys) == 0 {
		return &TestContentModerationAPIKeysResult{Items: []ContentModerationAPIKeyStatus{}, ImageCount: imageCount}, nil
	}
	items := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	var auditResult *ContentModerationTestAuditResult
	for idx, key := range keys {
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, testInput, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		keyHash := moderationAPIKeyHash(key)
		if err != nil {
			s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		} else {
			s.markAPIKeySuccess(key, latency, httpStatus)
			if auditResult == nil {
				auditResult = buildContentModerationTestAuditResult(result, cfg.Thresholds)
			}
		}
		status := s.apiKeyStatusForHash(idx, keyHash, maskSecretTail(key), configured)
		status.LastTested = true
		items = append(items, status)
	}
	return &TestContentModerationAPIKeysResult{Items: items, AuditResult: auditResult, ImageCount: imageCount}, nil
}

func (s *ContentModerationService) Check(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.settingRepo == nil || s.repo == nil {
		slog.Debug("content_moderation.skip_unavailable",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	runtimeSnapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil {
		slog.Warn("content_moderation.skip_config_load_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return allow, nil
	}
	if !runtimeSnapshot.riskControlEnabled {
		s.pauseAsyncRuntime()
		slog.Debug("content_moderation.skip_feature_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	cfg := runtimeSnapshot.config
	if _, current := s.syncAsyncRuntimeForRisk(cfg, 0); !current {
		slog.Debug("content_moderation.skip_risk_state_changed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	inGroupScope := cfg.includesGroup(input.GroupID)
	inModelScope := cfg.includesModel(input.Model)
	slog.Debug("content_moderation.config_loaded",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"provider", input.Provider,
		"protocol", input.Protocol,
		"model", input.Model,
		"enabled", cfg.Enabled,
		"mode", cfg.Mode,
		"all_groups", cfg.AllGroups,
		"configured_group_ids", cfg.GroupIDs,
		"in_group_scope", inGroupScope,
		"model_filter_type", cfg.ModelFilter.Type,
		"configured_models", cfg.ModelFilter.Models,
		"in_model_scope", inModelScope,
		"sample_rate", cfg.SampleRate,
		"api_key_count", len(cfg.apiKeys()),
		"pre_hash_check_enabled", cfg.PreHashCheckEnabled,
		"record_non_hits", cfg.RecordNonHits)
	if !cfg.Enabled {
		slog.Debug("content_moderation.skip_config_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeOff {
		slog.Debug("content_moderation.skip_mode_off",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if !inGroupScope {
		slog.Info("content_moderation.skip_group_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"all_groups", cfg.AllGroups,
			"configured_group_ids", cfg.GroupIDs)
		return allow, nil
	}
	if !inModelScope {
		slog.Info("content_moderation.skip_model_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"model", input.Model,
			"model_filter_type", cfg.ModelFilter.Type,
			"configured_models", cfg.ModelFilter.Models)
		return allow, nil
	}
	content := ExtractContentModerationInput(input.Protocol, input.Body)
	if content.IsEmpty() {
		slog.Info("content_moderation.skip_empty_input",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"body_bytes", len(input.Body))
		return allow, nil
	}
	content.Normalize()
	slog.Info("content_moderation.input_extracted",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"text_runes", len([]rune(content.Text)),
		"image_count", len(content.Images))
	hashText := content.Hash()
	if cfg.Mode == ContentModerationModePreBlock {
		if cfg.KeywordBlockingMode != ContentModerationKeywordModeAPIOnly && len(cfg.BlockedKeywords) > 0 {
			if keyword, hit := runtimeSnapshot.matchBlockedKeyword(content.Text); hit {
				s.recordPreBlockSyncMetric(0, ContentModerationActionKeywordBlock)
				slog.Info("content_moderation.keyword_block",
					"user_id", input.UserID,
					"api_key_id", input.APIKeyID,
					"group_id", contentModerationLogGroupID(input.GroupID),
					"endpoint", input.Endpoint,
					"protocol", input.Protocol,
					"keyword_blocking_mode", cfg.KeywordBlockingMode,
					"keyword", keyword)
				scores := map[string]float64{contentModerationKeywordCategory: 1.0}
				log := s.buildLog(input, cfg, ContentModerationActionKeywordBlock, true, contentModerationKeywordCategory, 1.0, scores, content.ExcerptText(), nil, nil, "")
				log.MatchedKeyword = keyword
				s.enqueueRecord(input, cfg, log, hashText, false, true)
				return &ContentModerationDecision{
					Allowed:         false,
					Blocked:         true,
					Flagged:         true,
					Message:         cfg.BlockMessage,
					StatusCode:      cfg.BlockStatus,
					HighestCategory: contentModerationKeywordCategory,
					HighestScore:    1.0,
					CategoryScores:  scores,
					Action:          ContentModerationActionKeywordBlock,
				}, nil
			}
		}
		if cfg.KeywordBlockingMode == ContentModerationKeywordModeKeywordOnly {
			s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
			slog.Info("content_moderation.skip_api_keyword_only",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol)
			return allow, nil
		}
	}
	if cfg.PreHashCheckEnabled && s.hashCache != nil {
		matched, err := s.hashCache.HasFlaggedInputHash(ctx, hashText)
		if err != nil {
			slog.Warn("content_moderation.hash_check_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
		if matched {
			if cfg.Mode == ContentModerationModePreBlock {
				s.recordPreBlockSyncMetric(0, ContentModerationActionHashBlock)
			}
			slog.Info("content_moderation.hash_block",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"input_hash", hashText)
			message := cfg.BlockMessage
			if message != "" {
				message = fmt.Sprintf("%s（hash: %s）", message, hashText)
			}
			scores := map[string]float64{"hash": 1.0}
			log := s.buildLog(input, cfg, ContentModerationActionHashBlock, true, "hash", 1.0, scores, content.ExcerptText(), nil, nil, "")
			s.enqueueRecord(input, cfg, log, hashText, false, false)
			return &ContentModerationDecision{
				Allowed:    false,
				Blocked:    true,
				Flagged:    true,
				Message:    message,
				StatusCode: cfg.BlockStatus,
				InputHash:  hashText,
				Action:     ContentModerationActionHashBlock,
			}, nil
		}
	}
	if !cfg.shouldSample(hashText) {
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
		}
		slog.Info("content_moderation.skip_sample_rate",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"sample_rate", cfg.SampleRate)
		return allow, nil
	}
	if len(cfg.apiKeys()) == 0 {
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(0, ContentModerationActionError)
		}
		slog.Warn("content_moderation.skip_no_audit_api_keys",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeObserve {
		slog.Info("content_moderation.enqueue_observe",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"queue_len", s.asyncQueueLength())
		s.enqueueAsync(input, cfg, content, hashText)
		return allow, nil
	}

	return s.checkSync(ctx, input, cfg, content, hashText, nil, true), nil
}

func (s *ContentModerationService) checkSync(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, queueDelay *int, allowBlock bool) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	trackPreBlock := queueDelay == nil && allowBlock && cfg != nil && cfg.Mode == ContentModerationModePreBlock
	if trackPreBlock {
		s.preBlockActive.Add(1)
		defer s.preBlockActive.Add(-1)
	}
	start := time.Now()
	result, err := s.callModeration(ctx, cfg, content.ModerationInput(), trackPreBlock)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		if trackPreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionError)
		}
		slog.Warn("content_moderation.audit_api_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"mode", cfg.Mode,
			"allow_block", allowBlock,
			"queue_delay_ms", queueDelay,
			"latency_ms", latency,
			"error", err)
		if queueDelay != nil {
			s.asyncErrors.Add(1)
		}
		if cfg.RecordNonHits {
			log := s.buildLog(input, cfg, ContentModerationActionError, false, "", 0, nil, content.ExcerptText(), &latency, queueDelay, err.Error())
			_ = s.repo.CreateLog(ctx, log)
		}
		return allow
	}

	flagged, highestCategory, highestScore := evaluateModerationScores(result.CategoryScores, cfg.Thresholds)
	action := ContentModerationActionAllow
	blocked := false
	if allowBlock && flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	if trackPreBlock {
		s.recordPreBlockSyncMetric(latency, action)
	}
	slog.Info("content_moderation.audit_result",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"mode", cfg.Mode,
		"allow_block", allowBlock,
		"flagged", flagged,
		"blocked", blocked,
		"action", action,
		"highest_category", highestCategory,
		"highest_score", highestScore,
		"latency_ms", latency,
		"queue_delay_ms", queueDelay)
	if flagged || cfg.RecordNonHits {
		log := s.buildLog(input, cfg, action, flagged, highestCategory, highestScore, result.CategoryScores, content.ExcerptText(), &latency, queueDelay, "")
		if queueDelay == nil && cfg.Mode == ContentModerationModePreBlock {
			s.enqueueRecord(input, cfg, log, hashText, flagged, flagged)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, flagged, flagged)
		}
	}
	if blocked {
		return &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      cfg.BlockStatus,
			HighestCategory: highestCategory,
			HighestScore:    highestScore,
			CategoryScores:  result.CategoryScores,
			Action:          action,
		}
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         flagged,
		Message:         "",
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  result.CategoryScores,
		Action:          action,
	}
}

func (s *ContentModerationService) recordPreBlockSyncMetric(latencyMS int, action string) {
	if s == nil {
		return
	}
	s.preBlockChecked.Add(1)
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.preBlockLatencyTotalMS.Add(int64(latencyMS))
	switch action {
	case ContentModerationActionBlock, ContentModerationActionHashBlock, ContentModerationActionKeywordBlock:
		s.preBlockBlocked.Add(1)
	case ContentModerationActionError:
		s.preBlockErrors.Add(1)
	default:
		s.preBlockAllowed.Add(1)
	}
}

func (s *ContentModerationService) enqueueAsync(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) {
	queue := s.asyncQueueForConfig(cfg)
	if queue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	_, prepared := prepareContentModerationAsyncTask(queue, input, cfg, content, hashText)
	if !prepared {
		slog.Warn("content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint, "queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	s.asyncEnqueued.Add(1)
}

func (s *ContentModerationService) enqueueRecord(input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) {
	if s == nil || log == nil {
		return
	}
	queue := s.asyncQueueForConfig(cfg)
	if queue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	_, prepared := prepareContentModerationRecordTask(queue, input, cfg, log, inputHash, recordHash, applySideEffects)
	if !prepared {
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action,
			"queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	s.asyncEnqueued.Add(1)
}

type contentModerationAsyncTaskSnapshotter func(ContentModerationCheckInput, *ContentModerationConfig, ContentModerationInput, string) contentModerationTask

func prepareContentModerationAsyncTask(queue *contentModerationTaskQueue, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) (contentModerationTask, bool) {
	return prepareContentModerationAsyncTaskWithSnapshotter(queue, input, cfg, content, hashText, newContentModerationAsyncTask)
}

func prepareContentModerationAsyncTaskWithSnapshotter(queue *contentModerationTaskQueue, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, snapshotter contentModerationAsyncTaskSnapshotter) (contentModerationTask, bool) {
	var zero contentModerationTask
	if queue == nil || snapshotter == nil {
		return zero, false
	}
	input.Body = nil
	rawTask := contentModerationTask{
		input:     input,
		content:   content,
		inputHash: hashText,
		config:    cfg,
	}
	reservation, ok := queue.Reserve(&rawTask)
	if !ok {
		return zero, false
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	task := snapshotter(input, cfg, content, hashText)
	committed = reservation.Commit(task)
	if !committed {
		return zero, false
	}
	return task, true
}

func prepareContentModerationRecordTask(queue *contentModerationTaskQueue, input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) (contentModerationTask, bool) {
	var zero contentModerationTask
	if queue == nil || log == nil {
		return zero, false
	}
	input.Body = nil
	rawTask := contentModerationTask{
		input:     input,
		inputHash: inputHash,
		log:       log,
		config:    cfg,
	}
	reservation, ok := queue.Reserve(&rawTask)
	if !ok {
		return zero, false
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	task := newContentModerationRecordTask(input, cfg, log, inputHash, recordHash, applySideEffects)
	committed = reservation.Commit(task)
	if !committed {
		return zero, false
	}
	return task, true
}

func newContentModerationAsyncTask(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) contentModerationTask {
	return contentModerationTask{
		input:      snapshotContentModerationAsyncInput(input),
		content:    snapshotContentModerationAsyncContent(content),
		inputHash:  strings.Clone(hashText),
		config:     snapshotContentModerationAsyncConfig(cfg),
		enqueuedAt: time.Now(),
	}
}

func newContentModerationRecordTask(input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) contentModerationTask {
	return contentModerationTask{
		input:            snapshotContentModerationAsyncInput(input),
		inputHash:        strings.Clone(inputHash),
		log:              snapshotContentModerationAsyncLog(log),
		config:           snapshotContentModerationAsyncConfig(cfg),
		recordHash:       recordHash,
		applySideEffects: applySideEffects,
		enqueuedAt:       time.Now(),
	}
}

func snapshotContentModerationAsyncInput(input ContentModerationCheckInput) ContentModerationCheckInput {
	return ContentModerationCheckInput{
		RequestID:  strings.Clone(input.RequestID),
		UserID:     input.UserID,
		UserEmail:  strings.Clone(input.UserEmail),
		APIKeyID:   input.APIKeyID,
		APIKeyName: strings.Clone(input.APIKeyName),
		GroupID:    cloneInt64Ptr(input.GroupID),
		GroupName:  strings.Clone(input.GroupName),
		Endpoint:   strings.Clone(input.Endpoint),
		Provider:   strings.Clone(input.Provider),
		Model:      strings.Clone(input.Model),
		Protocol:   strings.Clone(input.Protocol),
	}
}

func snapshotContentModerationAsyncContent(content ContentModerationInput) ContentModerationInput {
	images := make([]string, len(content.Images))
	for i, image := range content.Images {
		images[i] = strings.Clone(image)
	}
	return ContentModerationInput{
		Text:   strings.Clone(content.Text),
		Images: images,
	}
}

func snapshotContentModerationAsyncLog(log *ContentModerationLog) *ContentModerationLog {
	if log == nil {
		return nil
	}
	snapshot := *log
	snapshot.RequestID = strings.Clone(log.RequestID)
	snapshot.UserID = cloneInt64Ptr(log.UserID)
	snapshot.UserEmail = strings.Clone(log.UserEmail)
	snapshot.APIKeyID = cloneInt64Ptr(log.APIKeyID)
	snapshot.APIKeyName = strings.Clone(log.APIKeyName)
	snapshot.GroupID = cloneInt64Ptr(log.GroupID)
	snapshot.GroupName = strings.Clone(log.GroupName)
	snapshot.Endpoint = strings.Clone(log.Endpoint)
	snapshot.Provider = strings.Clone(log.Provider)
	snapshot.Model = strings.Clone(log.Model)
	snapshot.Mode = strings.Clone(log.Mode)
	snapshot.Action = strings.Clone(log.Action)
	snapshot.HighestCategory = strings.Clone(log.HighestCategory)
	snapshot.MatchedKeyword = strings.Clone(log.MatchedKeyword)
	snapshot.CategoryScores = cloneContentModerationAsyncFloatMap(log.CategoryScores)
	snapshot.ThresholdSnapshot = cloneContentModerationAsyncFloatMap(log.ThresholdSnapshot)
	snapshot.InputExcerpt = strings.Clone(log.InputExcerpt)
	snapshot.UpstreamLatencyMS = cloneContentModerationIntPtr(log.UpstreamLatencyMS)
	snapshot.Error = strings.Clone(log.Error)
	snapshot.UserStatus = strings.Clone(log.UserStatus)
	snapshot.QueueDelayMS = cloneContentModerationIntPtr(log.QueueDelayMS)
	return &snapshot
}

func snapshotContentModerationAsyncConfig(cfg *ContentModerationConfig) *ContentModerationConfig {
	if cfg == nil {
		return nil
	}
	apiKeys := make([]string, len(cfg.APIKeys))
	for i, key := range cfg.APIKeys {
		apiKeys[i] = strings.Clone(key)
	}
	return &ContentModerationConfig{
		Enabled:                        cfg.Enabled,
		Mode:                           strings.Clone(cfg.Mode),
		BaseURL:                        strings.Clone(cfg.BaseURL),
		Model:                          strings.Clone(cfg.Model),
		APIKey:                         strings.Clone(cfg.APIKey),
		APIKeys:                        apiKeys,
		TimeoutMS:                      cfg.TimeoutMS,
		RecordNonHits:                  cfg.RecordNonHits,
		Thresholds:                     cloneContentModerationAsyncFloatMap(cfg.Thresholds),
		EmailOnHit:                     cfg.EmailOnHit,
		AutoBanEnabled:                 cfg.AutoBanEnabled,
		BanThreshold:                   cfg.BanThreshold,
		ViolationWindowHours:           cfg.ViolationWindowHours,
		RetryCount:                     cfg.RetryCount,
		CyberPolicyExcludeFromBanCount: cfg.CyberPolicyExcludeFromBanCount,
	}
}

func cloneContentModerationAsyncFloatMap(values map[string]float64) map[string]float64 {
	if values == nil {
		return nil
	}
	cloned := make(map[string]float64, len(values))
	for key, value := range values {
		cloned[strings.Clone(key)] = value
	}
	return cloned
}

func cloneContentModerationIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *ContentModerationService) worker(id uint64, queue *contentModerationTaskQueue, control *contentModerationWorkerControl) {
	for {
		task, result := queue.Dequeue(control.ctx.Done(), s.stopCh)
		switch result {
		case contentModerationDequeueStopped:
			queue.ReleaseIfEmpty()
			return
		case contentModerationDequeueDrained:
			if s.retireDrainedWorker(id, control, queue) {
				queue.ReleaseIfEmpty()
				return
			}
			continue
		}
		func() {
			defer queue.Complete(task.retainedBytes)
			taskContext := s.backgroundContext()
			ctx, cancel := context.WithTimeout(taskContext, maxContentModerationTimeoutMS*time.Millisecond+10*time.Second)
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("content_moderation.worker_panic", "worker_id", id, "recover", r)
				}
			}()
			if task.log != nil {
				s.asyncActive.Add(1)
				defer s.asyncActive.Add(-1)
				queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
				task.log.QueueDelayMS = &queueDelay
				taskCfg := task.config
				if taskCfg == nil {
					var err error
					taskCfg, err = s.loadConfigSnapshot(ctx)
					if err != nil {
						s.asyncErrors.Add(1)
						return
					}
				}
				s.persistContentModerationLog(ctx, taskCfg, task.log, task.inputHash, task.recordHash, task.applySideEffects)
				s.asyncProcessed.Add(1)
				return
			}
			cfg := task.config
			if cfg == nil {
				var err error
				cfg, err = s.loadConfigSnapshot(ctx)
				if err != nil {
					s.asyncErrors.Add(1)
					return
				}
			}
			if !cfg.Enabled || cfg.Mode == ContentModerationModeOff || len(cfg.apiKeys()) == 0 {
				return
			}
			s.asyncActive.Add(1)
			defer s.asyncActive.Add(-1)
			queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
			_ = s.checkSync(ctx, task.input, cfg, task.content, task.inputHash, &queueDelay, false)
			s.asyncProcessed.Add(1)
		}()
	}
}

func (s *ContentModerationService) retireDrainedWorker(id uint64, control *contentModerationWorkerControl, queue *contentModerationTaskQueue) bool {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	registeredControl, registered := s.workerStops[id]
	if !registered || registeredControl != control {
		return true
	}
	if s.runtimeClosed {
		return true
	}
	if queue.IsDrained() {
		s.desiredWorkers = 0
		control.stopping = true
		control.cancel()
		return true
	}
	return false
}

func (s *ContentModerationService) workerExited(id uint64, control *contentModerationWorkerControl) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if registeredControl, ok := s.workerStops[id]; ok && registeredControl == control {
		delete(s.workerStops, id)
	}
	if !s.runtimeClosed {
		s.reconcileWorkersLocked()
	}
}

func (s *ContentModerationService) syncAsyncRuntime(cfg *ContentModerationConfig) *contentModerationTaskQueue {
	queue, _ := s.syncAsyncRuntimeForRisk(cfg, 0)
	return queue
}

func (s *ContentModerationService) syncAsyncRuntimeForRisk(cfg *ContentModerationConfig, expectedRiskRevision uint64) (*contentModerationTaskQueue, bool) {
	if s == nil || s.repo == nil || cfg == nil {
		return nil, false
	}
	shouldRun := cfg.Enabled && cfg.Mode != ContentModerationModeOff
	targetWorkers := 0
	if shouldRun {
		targetWorkers = cfg.WorkerCount
		if targetWorkers <= 0 {
			targetWorkers = defaultContentModerationWorkerCount
		}
		if targetWorkers > maxContentModerationWorkerCount {
			targetWorkers = maxContentModerationWorkerCount
		}
	}

	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimeClosed {
		return nil, false
	}
	if expectedRiskRevision > 0 {
		riskSnapshot := s.riskControlCache.Load()
		if riskSnapshot == nil || !riskSnapshot.enabled || riskSnapshot.revision != expectedRiskRevision {
			return nil, false
		}
	}
	if cfg.runtimeRevision > 0 {
		if cfg.runtimeRevision < s.runtimeConfigRevision {
			return nil, true
		}
		if cfg.runtimeRevision > s.runtimeConfigRevision {
			s.runtimeConfigRevision = cfg.runtimeRevision
		}
	}
	if s.asyncQueue == nil {
		if !shouldRun {
			s.runtimePaused.Store(true)
			return nil, true
		}
		s.asyncQueue = newContentModerationTaskQueue(cfg.QueueSize)
	} else {
		s.asyncQueue.SetLimit(cfg.QueueSize)
		s.asyncQueue.SetDraining(!shouldRun)
		// One drain worker preserves record tasks captured before a disable.
		if !shouldRun && s.asyncQueue.HasDrainWork() {
			targetWorkers = 1
		}
	}
	s.resizeWorkersLocked(targetWorkers)
	s.runtimePaused.Store(!shouldRun)
	return s.asyncQueue, true
}

func (s *ContentModerationService) asyncQueueForConfig(cfg *ContentModerationConfig) *contentModerationTaskQueue {
	if s == nil || cfg == nil {
		return nil
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimeClosed || s.asyncQueue == nil {
		return nil
	}
	if cfg.runtimeRevision > 0 && cfg.runtimeRevision != s.runtimeConfigRevision {
		return nil
	}
	return s.asyncQueue
}

func (s *ContentModerationService) pauseAsyncRuntime() {
	if s == nil || s.runtimePaused.Load() {
		return
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimePaused.Load() {
		return
	}
	s.pauseAsyncRuntimeLocked()
}

func (s *ContentModerationService) pauseAsyncRuntimeForRisk(expectedRiskRevision uint64) bool {
	if s == nil || expectedRiskRevision == 0 {
		return false
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	riskSnapshot := s.riskControlCache.Load()
	if riskSnapshot == nil || riskSnapshot.enabled || riskSnapshot.revision != expectedRiskRevision {
		return false
	}
	s.pauseAsyncRuntimeLocked()
	return true
}

func (s *ContentModerationService) pauseAsyncRuntimeLocked() {
	if s.runtimeClosed || s.asyncQueue == nil {
		s.runtimePaused.Store(true)
		return
	}
	s.asyncQueue.SetDraining(true)
	targetWorkers := 0
	if s.asyncQueue.HasDrainWork() {
		targetWorkers = 1
	}
	s.resizeWorkersLocked(targetWorkers)
	s.runtimePaused.Store(true)
}

func (s *ContentModerationService) resizeWorkersLocked(target int) {
	if target < 0 {
		target = 0
	}
	if target > maxContentModerationWorkerCount {
		target = maxContentModerationWorkerCount
	}
	s.desiredWorkers = target
	s.reconcileWorkersLocked()
}

func (s *ContentModerationService) reconcileWorkersLocked() {
	if s.workerStops == nil {
		s.workerStops = make(map[uint64]*contentModerationWorkerControl)
	}
	actualWorkers := len(s.workerStops)
	stoppingWorkers := 0
	for _, control := range s.workerStops {
		if control.stopping {
			stoppingWorkers++
		}
	}
	workersToStop := actualWorkers - s.desiredWorkers
	if workersToStop < 0 {
		workersToStop = 0
	}
	for _, control := range s.workerStops {
		if stoppingWorkers >= workersToStop {
			break
		}
		if control.stopping {
			continue
		}
		control.stopping = true
		control.cancel()
		stoppingWorkers++
	}
	for len(s.workerStops) < s.desiredWorkers {
		id := s.nextWorkerID
		s.nextWorkerID++
		workerCtx, workerCancel := context.WithCancel(s.backgroundContext())
		control := &contentModerationWorkerControl{ctx: workerCtx, cancel: workerCancel}
		s.workerStops[id] = control
		s.backgroundWG.Add(1)
		go func(workerID uint64, queue *contentModerationTaskQueue, workerControl *contentModerationWorkerControl) {
			defer s.backgroundWG.Done()
			defer s.workerExited(workerID, workerControl)
			s.worker(workerID, queue, workerControl)
		}(id, s.asyncQueue, control)
	}
}

func (s *ContentModerationService) asyncQueueLength() int {
	if s == nil {
		return 0
	}
	s.runtimeMu.Lock()
	queue := s.asyncQueue
	s.runtimeMu.Unlock()
	return queue.Len()
}

func (s *ContentModerationService) asyncRuntimeWorkerCount() int {
	if s == nil {
		return 0
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	count := 0
	for _, control := range s.workerStops {
		if !control.stopping {
			count++
		}
	}
	return count
}

func (s *ContentModerationService) asyncActualWorkerCount() int {
	if s == nil {
		return 0
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	return len(s.workerStops)
}

func (s *ContentModerationService) backgroundContext() context.Context {
	if s != nil && s.serviceCtx != nil {
		return s.serviceCtx
	}
	return context.Background()
}

// Close stops content moderation background work. The application owns one
// service instance; tests and explicit lifecycle owners can use Close to avoid
// retaining workers after that instance is no longer needed.
func (s *ContentModerationService) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.runtimeMu.Lock()
		s.runtimeClosed = true
		s.runtimePaused.Store(true)
		s.desiredWorkers = 0
		serviceCancel := s.serviceCancel
		queue := s.asyncQueue
		if queue != nil {
			queue.SetDraining(true)
			queue.DropAll()
		}
		if s.stopCh != nil {
			close(s.stopCh)
		}
		for _, control := range s.workerStops {
			if !control.stopping {
				control.stopping = true
				control.cancel()
			}
		}
		s.runtimeMu.Unlock()
		if serviceCancel != nil {
			serviceCancel()
		}
		s.backgroundWG.Wait()
		queue.WaitReservations()
		s.runtimeMu.Lock()
		s.workerStops = nil
		s.runtimeMu.Unlock()
	})
}

func (s *ContentModerationService) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	return s.repo.ListLogs(ctx, filter)
}

func (s *ContentModerationService) UnbanUser(ctx context.Context, userID int64) (*ContentModerationUnbanUserResult, error) {
	if s == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_USER_REPOSITORY_UNAVAILABLE", "用户仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "用户不存在")
		}
		return nil, fmt.Errorf("get content moderation unban user: %w", err)
	}
	if user.Status != StatusActive {
		user.Status = StatusActive
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("update content moderation unban user: %w", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	return &ContentModerationUnbanUserResult{
		UserID: userID,
		Status: StatusActive,
	}, nil
}

func (s *ContentModerationService) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (*ContentModerationDeleteHashResult, error) {
	inputHash = normalizeContentModerationHash(inputHash)
	if inputHash == "" {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_HASH", "风险输入哈希无效")
	}
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.DeleteFlaggedInputHash(ctx, inputHash)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation flagged hash: %w", err)
	}
	return &ContentModerationDeleteHashResult{
		InputHash: inputHash,
		Deleted:   deleted,
	}, nil
}

func (s *ContentModerationService) ClearFlaggedInputHashes(ctx context.Context) (*ContentModerationClearHashesResult, error) {
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.ClearFlaggedInputHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation flagged hashes: %w", err)
	}
	return &ContentModerationClearHashesResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) GetStatus(ctx context.Context) (*ContentModerationRuntimeStatus, error) {
	if s == nil {
		return &ContentModerationRuntimeStatus{}, nil
	}
	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	riskEnabled := s.isRiskControlEnabled(ctx)
	apiKeys := cfg.apiKeys()
	apiKeySnapshot := s.snapshotAPIKeyRuntime(apiKeys)
	active := int(s.asyncActive.Load())
	if active < 0 {
		active = 0
	}
	idle := cfg.WorkerCount - active
	if idle < 0 {
		idle = 0
	}
	preBlockActive := int(s.preBlockActive.Load())
	if preBlockActive < 0 {
		preBlockActive = 0
	}
	preBlockChecked := s.preBlockChecked.Load()
	preBlockAvgLatency := int64(0)
	if preBlockChecked > 0 {
		preBlockAvgLatency = s.preBlockLatencyTotalMS.Load() / preBlockChecked
	}
	queueLength := s.asyncQueueLength()
	queueUsage := 0.0
	if cfg.QueueSize > 0 {
		queueUsage = float64(queueLength) * 100 / float64(cfg.QueueSize)
	}
	var flaggedHashCount int64
	if s.hashCache != nil {
		if n, err := s.hashCache.CountFlaggedInputHashes(ctx); err == nil {
			flaggedHashCount = n
		} else {
			slog.Warn("content_moderation.hash_count_failed", "error", err)
		}
	}
	var lastCleanupAt *time.Time
	if unix := s.lastCleanupUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastCleanupAt = &t
	}
	return &ContentModerationRuntimeStatus{
		Enabled:                      cfg.Enabled,
		RiskControlEnabled:           riskEnabled,
		Mode:                         cfg.Mode,
		WorkerCount:                  cfg.WorkerCount,
		MaxWorkers:                   maxContentModerationWorkerCount,
		ActiveWorkers:                active,
		IdleWorkers:                  idle,
		QueueSize:                    cfg.QueueSize,
		QueueLength:                  queueLength,
		QueueUsagePercent:            queueUsage,
		Enqueued:                     s.asyncEnqueued.Load(),
		Dropped:                      s.asyncDropped.Load(),
		Processed:                    s.asyncProcessed.Load(),
		Errors:                       s.asyncErrors.Load(),
		PreBlockActive:               preBlockActive,
		PreBlockChecked:              preBlockChecked,
		PreBlockAllowed:              s.preBlockAllowed.Load(),
		PreBlockBlocked:              s.preBlockBlocked.Load(),
		PreBlockErrors:               s.preBlockErrors.Load(),
		PreBlockAvgLatencyMS:         preBlockAvgLatency,
		PreBlockAPIKeyActive:         apiKeySnapshot.active,
		PreBlockAPIKeyAvailableCount: apiKeySnapshot.available,
		PreBlockAPIKeyTotalCalls:     apiKeySnapshot.total,
		PreBlockAPIKeyLoads:          apiKeySnapshot.loads,
		APIKeyStatuses:               apiKeySnapshot.statuses,
		FlaggedHashCount:             flaggedHashCount,
		LastCleanupAt:                lastCleanupAt,
		LastCleanupDeletedHit:        s.lastCleanupDeletedHit.Load(),
		LastCleanupDeletedNonHit:     s.lastCleanupDeletedNonHit.Load(),
	}, nil
}

func (s *ContentModerationService) cleanupWorker() {
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			s.runCleanupOnce()
			timer.Reset(contentModerationCleanupInterval)
		case <-s.stopCh:
			return
		}
	}
}

func (s *ContentModerationService) runCleanupOnce() {
	if s == nil || s.repo == nil || s.settingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.backgroundContext(), contentModerationCleanupTimeout)
	defer cancel()
	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		slog.Warn("content_moderation.cleanup_load_config_failed", "error", err)
		return
	}
	now := time.Now()
	hitBefore := now.AddDate(0, 0, -cfg.HitRetentionDays)
	nonHitBefore := now.AddDate(0, 0, -cfg.NonHitRetentionDays)
	result, err := s.repo.CleanupExpiredLogs(ctx, hitBefore, nonHitBefore)
	if err != nil {
		slog.Warn("content_moderation.cleanup_failed", "error", err)
		return
	}
	if result == nil {
		return
	}
	s.lastCleanupUnix.Store(result.FinishedAt.Unix())
	s.lastCleanupDeletedHit.Store(result.DeletedHit)
	s.lastCleanupDeletedNonHit.Store(result.DeletedNonHit)
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	cfg, err := s.loadConfigSnapshot(ctx)
	return cloneContentModerationConfig(cfg), err
}

// loadConfigSnapshot returns an immutable cache snapshot. Callers that need to
// modify the config must use loadConfig, which creates a private deep copy.
func (s *ContentModerationService) loadConfigSnapshot(ctx context.Context) (*ContentModerationConfig, error) {
	if s == nil || s.settingRepo == nil {
		cfg := defaultContentModerationConfig()
		cfg.normalize()
		return cfg, nil
	}
	now := time.Now()
	cachedCfg, cachedErr, fresh, cached := s.readConfigCache(now)
	if fresh {
		return cachedCfg, cachedErr
	}
	if cached {
		if !s.configLoadMu.TryLock() {
			return cachedCfg, cachedErr
		}
	} else {
		s.configLoadMu.Lock()
	}
	defer s.configLoadMu.Unlock()

	now = time.Now()
	if cachedCfg, cachedErr, fresh, _ = s.readConfigCache(now); fresh {
		return cachedCfg, cachedErr
	}

	cfg := defaultContentModerationConfig()
	loadCtx, cancel := contentModerationSettingsLoadContext(ctx)
	raw, err := s.settingRepo.GetValue(loadCtx, SettingKeyContentModerationConfig)
	cancel()
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			cfg.normalize()
			return s.storeLoadedConfig(cfg, nil, now)
		}
		loadErr := fmt.Errorf("get content moderation config: %w", err)
		return s.storeConfigRefreshFailure(cachedCfg, loadErr, now)
	}
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return s.storeLoadedConfig(cfg, nil, now)
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		loadErr := infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
		return s.storeConfigRefreshFailure(cachedCfg, loadErr, now)
	}
	cfg.normalize()
	return s.storeLoadedConfig(cfg, nil, now)
}

func parseContentModerationConfig(raw string) (*ContentModerationConfig, error) {
	cfg := defaultContentModerationConfig()
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
	}
	cfg.normalize()
	return cfg, nil
}

func (s *ContentModerationService) readConfigCache(now time.Time) (*ContentModerationConfig, error, bool, bool) {
	entry := s.configCache.Load()
	if entry == nil {
		return nil, nil, false, false
	}
	return entry.cfg, entry.err, now.UnixNano() < entry.expiresAt, true
}

func (s *ContentModerationService) storeLoadedConfig(cfg *ContentModerationConfig, cacheErr error, now time.Time) (*ContentModerationConfig, error) {
	entry := s.storeConfigCacheLocked(cfg, cacheErr, now, contentModerationSettingsCacheTTL)
	return entry.cfg, entry.err
}

func (s *ContentModerationService) storeConfigRefreshFailure(fallback *ContentModerationConfig, loadErr error, now time.Time) (*ContentModerationConfig, error) {
	if fallback != nil {
		entry := &contentModerationConfigCacheEntry{
			cfg:       fallback,
			expiresAt: now.Add(contentModerationSettingsErrorRetryTTL).UnixNano(),
		}
		s.configCache.Store(entry)
		slog.Warn("content_moderation.config_refresh_failed_using_stale", "error", loadErr)
		return fallback, nil
	}
	entry := &contentModerationConfigCacheEntry{
		err:       loadErr,
		expiresAt: now.Add(contentModerationSettingsErrorRetryTTL).UnixNano(),
	}
	s.configCache.Store(entry)
	return nil, loadErr
}

func (s *ContentModerationService) loadRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("content moderation setting repository unavailable")
	}
	now := time.Now()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		ttl := s.runtimeSnapshotTTL()
		if ttl >= time.Millisecond && now.Sub(snapshot.loadedAt) < ttl {
			return snapshot, nil
		}
		s.triggerRuntimeSnapshotRefresh()
		return snapshot, nil
	}

	s.runtimeRefreshMu.Lock()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		s.runtimeRefreshMu.Unlock()
		return snapshot, nil
	}
	snapshot, err := s.refreshRuntimeSnapshot(ctx)
	s.runtimeRefreshMu.Unlock()
	return snapshot, err
}

func (s *ContentModerationService) runtimeSnapshotTTL() time.Duration {
	if s != nil && s.runtimeCacheTTL > 0 {
		return s.runtimeCacheTTL
	}
	return contentModerationRuntimeCacheTTL
}

func (s *ContentModerationService) triggerRuntimeSnapshotRefresh() {
	if s == nil || s.runtimeRefreshDeferred() {
		return
	}
	if !s.runtimeRefreshMu.TryLock() {
		if s.runtimeRefreshPending.CompareAndSwap(false, true) {
			go s.runPendingRuntimeSnapshotRefresh()
		}
		return
	}
	if s.runtimeRefreshDeferred() {
		s.runtimeRefreshMu.Unlock()
		return
	}
	go func() {
		defer s.runtimeRefreshMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), contentModerationRuntimeRefreshTimeout)
		defer cancel()
		if _, err := s.refreshRuntimeSnapshot(ctx); err != nil {
			s.runtimeRefreshRetryAt.Store(time.Now().Add(s.runtimeSnapshotTTL()).UnixNano())
			slog.Warn("content_moderation.runtime_snapshot_refresh_failed", "error", err)
		}
	}()
	runtime.Gosched()
}

func (s *ContentModerationService) runPendingRuntimeSnapshotRefresh() {
	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	defer s.runtimeRefreshPending.Store(false)
	if s.runtimeRefreshDeferred() {
		return
	}
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		ttl := s.runtimeSnapshotTTL()
		if ttl >= time.Millisecond && time.Since(snapshot.loadedAt) < ttl {
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationRuntimeRefreshTimeout)
	defer cancel()
	if _, err := s.refreshRuntimeSnapshot(ctx); err != nil {
		s.runtimeRefreshRetryAt.Store(time.Now().Add(s.runtimeSnapshotTTL()).UnixNano())
		slog.Warn("content_moderation.runtime_snapshot_refresh_failed", "error", err)
	}
}

func (s *ContentModerationService) runtimeRefreshDeferred() bool {
	if s == nil {
		return false
	}
	return time.Now().UnixNano() < s.runtimeRefreshRetryAt.Load()
}

func (s *ContentModerationService) refreshRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyRiskControlEnabled,
		SettingKeyContentModerationConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("get content moderation runtime settings: %w", err)
	}
	rawConfig := values[SettingKeyContentModerationConfig]
	configDigest := sha256.Sum256([]byte(rawConfig))
	if current := s.runtimeSnapshot.Load(); current != nil && current.configDigest == configDigest {
		snapshot := &contentModerationRuntimeSnapshot{
			riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
			config:             current.config,
			keywordMatcher:     current.keywordMatcher,
			configDigest:       configDigest,
			loadedAt:           time.Now(),
		}
		s.runtimeSnapshot.Store(snapshot)
		s.runtimeRefreshRetryAt.Store(0)
		return snapshot, nil
	}
	cfg, err := parseContentModerationConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	snapshot := &contentModerationRuntimeSnapshot{
		riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
		config:             cfg,
		keywordMatcher:     newContentModerationKeywordMatcher(cfg.BlockedKeywords),
		configDigest:       configDigest,
		loadedAt:           time.Now(),
	}
	s.runtimeSnapshot.Store(snapshot)
	s.runtimeRefreshRetryAt.Store(0)
	return snapshot, nil
}

func (s *ContentModerationService) replaceRuntimeConfig(cfg *ContentModerationConfig, raw []byte) {
	if s == nil || cfg == nil {
		return
	}
	s.runtimeRefreshMu.Lock()
	hasSnapshot := s.runtimeSnapshot.Load() != nil
	s.runtimeRefreshMu.Unlock()
	if !hasSnapshot {
		return
	}
	config := cloneContentModerationConfig(cfg)
	keywordMatcher := newContentModerationKeywordMatcher(cfg.BlockedKeywords)
	configDigest := sha256.Sum256(raw)

	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	current := s.runtimeSnapshot.Load()
	if current == nil {
		return
	}
	s.runtimeSnapshot.Store(&contentModerationRuntimeSnapshot{
		riskControlEnabled: current.riskControlEnabled,
		config:             config,
		keywordMatcher:     keywordMatcher,
		configDigest:       configDigest,
		loadedAt:           time.Now(),
	})
}

func (s *contentModerationRuntimeSnapshot) matchBlockedKeyword(text string) (string, bool) {
	if s == nil || s.config == nil {
		return "", false
	}
	if s.keywordMatcher != nil {
		return s.keywordMatcher.Match(text)
	}
	return matchBlockedKeyword(text, s.config.BlockedKeywords)
}

func (s *ContentModerationService) isRiskControlEnabled(ctx context.Context) bool {
	enabled, _ := s.riskControlState(ctx)
	return enabled
}

func (s *ContentModerationService) riskControlState(ctx context.Context) (bool, uint64) {
	if s == nil || s.settingRepo == nil {
		return false, 0
	}
	now := time.Now()
	snapshot := s.riskControlCache.Load()
	if snapshot != nil && now.UnixNano() < snapshot.expiresAt {
		return snapshot.enabled, snapshot.revision
	}
	if snapshot != nil {
		if !s.riskControlRefreshMu.TryLock() {
			return snapshot.enabled, snapshot.revision
		}
	} else {
		s.riskControlRefreshMu.Lock()
	}
	defer s.riskControlRefreshMu.Unlock()

	now = time.Now()
	snapshot = s.riskControlCache.Load()
	if snapshot != nil && now.UnixNano() < snapshot.expiresAt {
		return snapshot.enabled, snapshot.revision
	}
	loadCtx, cancel := contentModerationSettingsLoadContext(ctx)
	raw, err := s.settingRepo.GetValue(loadCtx, SettingKeyRiskControlEnabled)
	cancel()
	if err != nil && snapshot != nil {
		refreshed := &contentModerationRiskControlSnapshot{
			enabled:   snapshot.enabled,
			revision:  snapshot.revision,
			expiresAt: now.Add(contentModerationSettingsErrorRetryTTL).UnixNano(),
		}
		s.riskControlCache.Store(refreshed)
		slog.Warn("content_moderation.risk_control_refresh_failed_using_stale", "error", err)
		return refreshed.enabled, refreshed.revision
	}
	enabled := err == nil && raw == "true"
	revision := uint64(1)
	if snapshot != nil {
		revision = snapshot.revision
		if enabled != snapshot.enabled {
			revision++
		}
	}
	ttl := contentModerationSettingsCacheTTL
	if err != nil {
		ttl = contentModerationSettingsErrorRetryTTL
	}
	refreshed := &contentModerationRiskControlSnapshot{
		enabled:   enabled,
		revision:  revision,
		expiresAt: now.Add(ttl).UnixNano(),
	}
	s.riskControlCache.Store(refreshed)
	return enabled, revision
}

func contentModerationSettingsLoadContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, contentModerationSettingsLoadTimeout)
}

func (s *ContentModerationService) cacheConfig(cfg *ContentModerationConfig, cacheErr error) *ContentModerationConfig {
	if s == nil {
		return nil
	}
	s.configLoadMu.Lock()
	defer s.configLoadMu.Unlock()
	return s.storeConfigCacheLocked(cfg, cacheErr, time.Now(), contentModerationSettingsCacheTTL).cfg
}

func (s *ContentModerationService) storeConfigCacheLocked(cfg *ContentModerationConfig, cacheErr error, now time.Time, ttl time.Duration) *contentModerationConfigCacheEntry {
	immutable := cloneContentModerationConfig(cfg)
	if immutable != nil {
		current := s.configCache.Load()
		if s.configRevision == 0 || current == nil || !contentModerationConfigsEqual(current.cfg, immutable) {
			s.configRevision++
		}
		immutable.runtimeRevision = s.configRevision
	}
	entry := &contentModerationConfigCacheEntry{
		cfg:       immutable,
		err:       cacheErr,
		expiresAt: now.Add(ttl).UnixNano(),
	}
	s.configCache.Store(entry)
	return entry
}

func contentModerationConfigsEqual(left *ContentModerationConfig, right *ContentModerationConfig) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftValue := *left
	rightValue := *right
	leftValue.runtimeRevision = 0
	rightValue.runtimeRevision = 0
	return reflect.DeepEqual(leftValue, rightValue)
}

func (s *ContentModerationService) validateConfig(ctx context.Context, cfg *ContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不能为空")
	}
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODE", "内容审计模式无效")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "OpenAI Base URL 无效")
	}
	if cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BLOCK_STATUS", "拦截 HTTP 状态码必须在 400-599 之间")
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterAll && len(cfg.ModelFilter.Models) == 0 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODEL_FILTER", "指定或排除模型时至少需要配置 1 个模型")
	}
	if !cfg.AllGroups && len(cfg.GroupIDs) > 0 && s.groupRepo != nil {
		for _, groupID := range cfg.GroupIDs {
			if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GROUP", fmt.Sprintf("审计分组不存在: %d", groupID))
			}
		}
	}
	return nil
}

func (s *ContentModerationService) callModeration(ctx context.Context, cfg *ContentModerationConfig, input any, trackKeyLoad ...bool) (*moderationAPIResult, error) {
	attempts := cfg.RetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > maxContentModerationRetryCount+1 {
		attempts = maxContentModerationRetryCount + 1
	}
	trackLoad := len(trackKeyLoad) > 0 && trackKeyLoad[0]
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			lastErr = errors.New("no moderation api key available")
			break
		}
		if trackLoad {
			s.beginModerationAPIKeyCall(key)
		}
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, input, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		if err == nil {
			if trackLoad {
				s.finishModerationAPIKeyCall(key, latency, true)
			}
			s.markAPIKeySuccess(key, latency, httpStatus)
			return result, nil
		}
		if trackLoad {
			s.finishModerationAPIKeyCall(key, latency, false)
		}
		s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		lastErr = err
		if httpStatus == http.StatusBadRequest {
			break
		}
		if attempt == attempts-1 {
			break
		}
		wait := time.Duration(100*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

func (s *ContentModerationService) callModerationOnceWithInput(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out moderationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return &out.Results[0], nil
}

func (s *ContentModerationService) buildLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, action string, flagged bool, highestCategory string, highestScore float64, scores map[string]float64, text string, latency *int, queueDelay *int, errText string) *ContentModerationLog {
	var userID *int64
	if input.UserID > 0 {
		userID = &input.UserID
	}
	var apiKeyID *int64
	if input.APIKeyID > 0 {
		apiKeyID = &input.APIKeyID
	}
	return &ContentModerationLog{
		RequestID:         input.RequestID,
		UserID:            userID,
		UserEmail:         input.UserEmail,
		APIKeyID:          apiKeyID,
		APIKeyName:        input.APIKeyName,
		GroupID:           cloneInt64Ptr(input.GroupID),
		GroupName:         input.GroupName,
		Endpoint:          input.Endpoint,
		Provider:          input.Provider,
		Model:             input.Model,
		Mode:              cfg.Mode,
		Action:            action,
		Flagged:           flagged,
		HighestCategory:   highestCategory,
		HighestScore:      highestScore,
		CategoryScores:    cloneFloatMap(scores),
		ThresholdSnapshot: cloneFloatMap(cfg.Thresholds),
		InputExcerpt:      trimRunes(redactContentModerationSecrets(text), maxModerationExcerptRunes),
		UpstreamLatencyMS: latency,
		QueueDelayMS:      queueDelay,
		Error:             errText,
	}
}

func (s *ContentModerationService) persistContentModerationLog(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, hashText string, recordHash bool, applySideEffects bool) {
	if s == nil || log == nil {
		return
	}
	if recordHash && s.hashCache != nil {
		if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
			slog.Warn("content_moderation.record_hash_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "error", err)
		}
	}
	autoBanJustApplied := false
	if applySideEffects {
		autoBanJustApplied = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
		s.sendFlaggedNotificationSideEffects(ctx, cfg, log, autoBanJustApplied)
	}
	if s.repo != nil {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.create_log_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "action", log.Action, "error", err)
			return
		}
	}
}

func (s *ContentModerationService) applyFlaggedAccountSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) bool {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return false
	}
	count := 1
	if s.repo != nil && cfg.ViolationWindowHours > 0 {
		since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
		if n, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {
			count = n + 1
		}
	}
	log.ViolationCount = count
	autoBanJustApplied := false
	if cfg.AutoBanEnabled && cfg.BanThreshold > 0 && count >= cfg.BanThreshold && s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, *log.UserID)
		if err != nil {
			slog.Warn("content_moderation.ban_get_user_failed", "user_id", *log.UserID, "error", err)
			return false
		}
		if user.IsAdmin() {
			slog.Warn("content_moderation.autoban_skipped_admin", "user_id", *log.UserID, "role", user.Role, "count", count, "threshold", cfg.BanThreshold)
			// TODO: Disable the triggering API key instead when API key mutation is available here.
			return false
		}
		if user.Status != StatusDisabled {
			user.Status = StatusDisabled
			if err := s.userRepo.Update(ctx, user); err != nil {
				slog.Warn("content_moderation.ban_update_user_failed", "user_id", *log.UserID, "error", err)
				return false
			}
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
			}
			autoBanJustApplied = true
		}
		log.AutoBanned = true
	}
	return autoBanJustApplied
}

func (s *ContentModerationService) sendFlaggedNotificationSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, autoBanJustApplied bool) {
	if s == nil || cfg == nil || log == nil || !log.Flagged {
		return
	}
	if s.emailService == nil || strings.TrimSpace(log.UserEmail) == "" {
		return
	}
	emailSent := false
	if cfg.EmailOnHit {
		if err := s.sendViolationEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	if autoBanJustApplied {
		if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.ban_email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	log.EmailSent = emailSent
}

func (s *ContentModerationService) sendViolationEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		if err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventContentModerationViolation,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      contentModerationEmailVariables(log, cfg),
		}); err == nil {
			return nil
		} else {
			if !shouldFallbackNotificationEmail(err) {
				return err
			}
			slog.Warn("template content moderation violation email failed; falling back to built-in body", "log_id", log.ID, "recipient_hash", notificationEmailHash(log.UserEmail), "err", err.Error())
		}
	}
	subject := fmt.Sprintf("[%s] 账户风控提醒 / Risk Control Notice", sanitizeEmailHeader(siteName))
	body := buildContentModerationViolationEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func (s *ContentModerationService) sendAccountDisabledEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		if err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventContentModerationDisabled,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      contentModerationEmailVariables(log, cfg),
		}); err == nil {
			return nil
		} else {
			if !shouldFallbackNotificationEmail(err) {
				return err
			}
			slog.Warn("template content moderation disabled email failed; falling back to built-in body", "log_id", log.ID, "recipient_hash", notificationEmailHash(log.UserEmail), "err", err.Error())
		}
	}
	subject := fmt.Sprintf("[%s] 账户已被禁用 / Account Disabled", sanitizeEmailHeader(siteName))
	body := buildContentModerationAccountDisabledEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func contentModerationEmailUserID(log *ContentModerationLog) int64 {
	if log == nil || log.UserID == nil {
		return 0
	}
	return *log.UserID
}

func contentModerationEmailSourceID(log *ContentModerationLog) string {
	if log == nil || log.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", log.ID)
}

func contentModerationEmailVariables(log *ContentModerationLog, cfg *ContentModerationConfig) map[string]string {
	variables := map[string]string{
		"triggered_at":        time.Now().UTC().Format(time.RFC3339),
		"group_name":          "-",
		"moderation_category": "-",
		"moderation_score":    "0.000",
		"violation_count":     "0",
		"ban_threshold":       "0",
	}
	if log != nil {
		if !log.CreatedAt.IsZero() {
			variables["triggered_at"] = log.CreatedAt.UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(log.GroupName) != "" {
			variables["group_name"] = strings.TrimSpace(log.GroupName)
		}
		if strings.TrimSpace(log.HighestCategory) != "" {
			variables["moderation_category"] = strings.TrimSpace(log.HighestCategory)
		}
		variables["moderation_score"] = fmt.Sprintf("%.3f", log.HighestScore)
		variables["violation_count"] = fmt.Sprintf("%d", log.ViolationCount)
	}
	if cfg != nil {
		variables["ban_threshold"] = fmt.Sprintf("%d", cfg.BanThreshold)
	}
	return variables
}

func (s *ContentModerationService) siteName(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return "Sub2API"
	}
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return "Sub2API"
	}
	return strings.TrimSpace(name)
}

func defaultContentModerationConfig() *ContentModerationConfig {
	return &ContentModerationConfig{
		Enabled:              false,
		Mode:                 ContentModerationModePreBlock,
		BaseURL:              defaultContentModerationBaseURL,
		Model:                defaultContentModerationModel,
		TimeoutMS:            defaultContentModerationTimeoutMS,
		SampleRate:           100,
		AllGroups:            true,
		GroupIDs:             []int64{},
		RecordNonHits:        false,
		Thresholds:           ContentModerationDefaultThresholds(),
		WorkerCount:          defaultContentModerationWorkerCount,
		QueueSize:            defaultContentModerationQueueSize,
		BlockStatus:          defaultContentModerationBlockHTTPStatus,
		BlockMessage:         defaultContentModerationBlockMessage,
		EmailOnHit:           true,
		AutoBanEnabled:       true,
		BanThreshold:         defaultContentModerationBanThreshold,
		ViolationWindowHours: defaultContentModerationViolationWindowHours,
		RetryCount:           defaultContentModerationRetryCount,
		HitRetentionDays:     defaultContentModerationHitRetentionDays,
		NonHitRetentionDays:  defaultContentModerationNonHitRetentionDays,
		PreHashCheckEnabled:  false,
		BlockedKeywords:      []string{},
		KeywordBlockingMode:  ContentModerationKeywordModeKeywordAndAPI,
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterAll,
			Models: []string{},
		},
		CyberPolicyExcludeFromBanCount: false,
	}
}

func cloneContentModerationConfig(cfg *ContentModerationConfig) *ContentModerationConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.APIKeys = append([]string(nil), cfg.APIKeys...)
	clone.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	clone.BlockedKeywords = append([]string(nil), cfg.BlockedKeywords...)
	clone.Thresholds = cloneFloatMap(cfg.Thresholds)
	clone.ModelFilter = ContentModerationModelFilter{
		Type:   cfg.ModelFilter.Type,
		Models: append([]string(nil), cfg.ModelFilter.Models...),
	}
	return &clone
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg.APIKey != "" {
		cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, cfg.APIKey))
		cfg.APIKey = ""
	} else {
		cfg.APIKeys = normalizeModerationAPIKeys(cfg.APIKeys)
	}
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationModePreBlock
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURL
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModel
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 100 {
		cfg.SampleRate = 100
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultContentModerationWorkerCount
	}
	if cfg.WorkerCount > maxContentModerationWorkerCount {
		cfg.WorkerCount = maxContentModerationWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultContentModerationQueueSize
	}
	if cfg.QueueSize > maxContentModerationQueueSize {
		cfg.QueueSize = maxContentModerationQueueSize
	}
	if strings.TrimSpace(cfg.BlockMessage) == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage)
	if cfg.BlockStatus <= 0 {
		cfg.BlockStatus = defaultContentModerationBlockHTTPStatus
	}
	if cfg.BanThreshold <= 0 {
		cfg.BanThreshold = defaultContentModerationBanThreshold
	}
	if cfg.ViolationWindowHours <= 0 {
		cfg.ViolationWindowHours = defaultContentModerationViolationWindowHours
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = 0
	}
	if cfg.RetryCount > maxContentModerationRetryCount {
		cfg.RetryCount = maxContentModerationRetryCount
	}
	if cfg.HitRetentionDays <= 0 {
		cfg.HitRetentionDays = defaultContentModerationHitRetentionDays
	}
	if cfg.HitRetentionDays > maxContentModerationRetentionDays {
		cfg.HitRetentionDays = maxContentModerationRetentionDays
	}
	if cfg.NonHitRetentionDays <= 0 {
		cfg.NonHitRetentionDays = defaultContentModerationNonHitRetentionDays
	}
	if cfg.NonHitRetentionDays > maxContentModerationNonHitRetentionDays {
		cfg.NonHitRetentionDays = maxContentModerationNonHitRetentionDays
	}
	cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
	cfg.BlockedKeywords = normalizeBlockedKeywords(cfg.BlockedKeywords)
	cfg.KeywordBlockingMode = normalizeKeywordBlockingMode(cfg.KeywordBlockingMode)
	cfg.ModelFilter = normalizeContentModerationModelFilter(cfg.ModelFilter)
}

func (cfg *ContentModerationConfig) includesGroup(groupID *int64) bool {
	if cfg.AllGroups {
		return true
	}
	if groupID == nil {
		return false
	}
	for _, id := range cfg.GroupIDs {
		if id == *groupID {
			return true
		}
	}
	return false
}

func (cfg *ContentModerationConfig) includesModel(model string) bool {
	if cfg == nil {
		return true
	}
	// Cached configs are normalized before publication. Re-normalizing here
	// allocated a slice and deduplication map on every audited request.
	switch cfg.ModelFilter.Type {
	case ContentModerationModelFilterInclude:
		return contentModerationModelListContains(cfg.ModelFilter.Models, model)
	case ContentModerationModelFilterExclude:
		return !contentModerationModelListContains(cfg.ModelFilter.Models, model)
	default:
		return true
	}
}

func contentModerationLogGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func (cfg *ContentModerationConfig) shouldSample(hashText string) bool {
	if cfg.SampleRate >= 100 {
		return true
	}
	if cfg.SampleRate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(hashText)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < cfg.SampleRate
}

func (cfg *ContentModerationConfig) apiKeys() []string {
	if cfg == nil {
		return nil
	}
	return cfg.APIKeys
}

func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	keys := cfg.apiKeys()
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for i := 0; i < len(keys); i++ {
		idx := int(s.apiKeyCursor.Add(1)-1) % len(keys)
		key := keys[idx]
		if !s.isAPIKeyFrozen(key, now) {
			return key, true
		}
	}
	return "", false
}

func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

func (s *ContentModerationService) beginModerationAPIKeyCall(key string) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.SyncActive++
}

func (s *ContentModerationService) finishModerationAPIKeyCall(key string, latencyMS int, success bool) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if state.SyncActive > 0 {
		state.SyncActive--
	}
	state.SyncTotal++
	state.SyncLatencyMS += int64(latencyMS)
	if success {
		state.SyncSuccess++
		return
	}
	state.SyncErrors++
}

func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	s.trimAPIKeyHealthLocked()
}

func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
	s.trimAPIKeyHealthLocked()
}

func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, http.StatusBadRequest:
		return 0
	case http.StatusUnauthorized, http.StatusForbidden:
		return contentModerationKeyAuthFreezeDuration
	case http.StatusTooManyRequests, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}

func (s *ContentModerationService) ensureAPIKeyHealthLocked(hash string, masked string) *contentModerationKeyHealth {
	if s.keyHealth == nil {
		s.keyHealth = make(map[string]*contentModerationKeyHealth)
	}
	state := s.keyHealth[hash]
	if state == nil {
		if len(s.keyHealth) >= maxContentModerationKeyHealthEntries {
			s.evictOldestInactiveAPIKeyHealthLocked()
		}
		state = &contentModerationKeyHealth{Hash: hash}
		s.keyHealth[hash] = state
	}
	if strings.TrimSpace(masked) != "" {
		state.Masked = masked
	}
	return state
}

func (s *ContentModerationService) evictOldestInactiveAPIKeyHealthLocked() {
	oldestHash := ""
	var oldest time.Time
	for hash, state := range s.keyHealth {
		if state == nil || state.SyncActive != 0 {
			continue
		}
		if oldestHash == "" || state.LastCheckedAt.Before(oldest) {
			oldestHash = hash
			oldest = state.LastCheckedAt
		}
	}
	if oldestHash != "" {
		delete(s.keyHealth, oldestHash)
	}
}

func (s *ContentModerationService) trimAPIKeyHealthLocked() {
	for len(s.keyHealth) > maxContentModerationKeyHealthEntries {
		before := len(s.keyHealth)
		s.evictOldestInactiveAPIKeyHealthLocked()
		if len(s.keyHealth) == before {
			return
		}
	}
}

func (s *ContentModerationService) pruneAPIKeyHealth(keys []string) {
	if s == nil {
		return
	}
	keep := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if hash := moderationAPIKeyHash(key); hash != "" {
			keep[hash] = struct{}{}
		}
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	for hash, state := range s.keyHealth {
		if _, ok := keep[hash]; ok || (state != nil && state.SyncActive != 0) {
			continue
		}
		delete(s.keyHealth, hash)
	}
}

func (s *ContentModerationService) removeTemporaryAPIKeyHealth(keys []string, configuredKeys []string) {
	if s == nil {
		return
	}
	configured := make(map[string]struct{}, len(configuredKeys))
	for _, key := range configuredKeys {
		if hash := moderationAPIKeyHash(key); hash != "" {
			configured[hash] = struct{}{}
		}
	}
	temporary := make([]string, 0, len(keys))
	for _, key := range keys {
		if hash := moderationAPIKeyHash(key); hash != "" {
			temporary = append(temporary, hash)
		}
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	for _, hash := range temporary {
		if _, ok := configured[hash]; ok {
			continue
		}
		if state := s.keyHealth[hash]; state == nil || state.SyncActive == 0 {
			delete(s.keyHealth, hash)
		}
	}
}

func (s *ContentModerationService) configView(cfg *ContentModerationConfig) *ContentModerationConfigView {
	keys := cfg.apiKeys()
	masks := make([]string, 0, len(keys))
	for _, key := range keys {
		masks = append(masks, maskSecretTail(key))
	}
	apiKeyMasked := ""
	if len(masks) > 0 {
		apiKeyMasked = masks[0]
	}
	return &ContentModerationConfigView{
		Enabled:                        cfg.Enabled,
		Mode:                           cfg.Mode,
		BaseURL:                        cfg.BaseURL,
		Model:                          cfg.Model,
		APIKeyConfigured:               len(keys) > 0,
		APIKeyMasked:                   apiKeyMasked,
		APIKeyCount:                    len(keys),
		APIKeyMasks:                    masks,
		APIKeyStatuses:                 s.apiKeyStatuses(keys),
		TimeoutMS:                      cfg.TimeoutMS,
		SampleRate:                     cfg.SampleRate,
		AllGroups:                      cfg.AllGroups,
		GroupIDs:                       append([]int64(nil), cfg.GroupIDs...),
		RecordNonHits:                  cfg.RecordNonHits,
		Thresholds:                     cloneFloatMap(cfg.Thresholds),
		WorkerCount:                    cfg.WorkerCount,
		QueueSize:                      cfg.QueueSize,
		BlockStatus:                    cfg.BlockStatus,
		BlockMessage:                   cfg.BlockMessage,
		EmailOnHit:                     cfg.EmailOnHit,
		AutoBanEnabled:                 cfg.AutoBanEnabled,
		BanThreshold:                   cfg.BanThreshold,
		ViolationWindowHours:           cfg.ViolationWindowHours,
		RetryCount:                     cfg.RetryCount,
		HitRetentionDays:               cfg.HitRetentionDays,
		NonHitRetentionDays:            cfg.NonHitRetentionDays,
		PreHashCheckEnabled:            cfg.PreHashCheckEnabled,
		BlockedKeywords:                append([]string(nil), cfg.BlockedKeywords...),
		KeywordBlockingMode:            cfg.KeywordBlockingMode,
		ModelFilter:                    cloneContentModerationModelFilter(cfg.ModelFilter),
		CyberPolicyExcludeFromBanCount: cfg.CyberPolicyExcludeFromBanCount,
	}
}

type contentModerationAPIKeyRuntimeSnapshot struct {
	statuses  []ContentModerationAPIKeyStatus
	loads     []ContentModerationAPIKeyLoad
	active    int64
	available int64
	total     int64
}

func (s *ContentModerationService) snapshotAPIKeyRuntime(keys []string) contentModerationAPIKeyRuntimeSnapshot {
	snapshot := contentModerationAPIKeyRuntimeSnapshot{
		statuses: make([]ContentModerationAPIKeyStatus, len(keys)),
		loads:    make([]ContentModerationAPIKeyLoad, len(keys)),
	}
	hashes := make([]string, len(keys))
	for index, key := range keys {
		hash := moderationAPIKeyHash(key)
		masked := maskSecretTail(key)
		hashes[index] = hash
		snapshot.statuses[index] = ContentModerationAPIKeyStatus{
			Index:      index,
			KeyHash:    hash,
			Masked:     masked,
			Status:     "unknown",
			Configured: true,
		}
		snapshot.loads[index] = ContentModerationAPIKeyLoad{
			Index:   index,
			KeyHash: hash,
			Masked:  masked,
			Status:  "unknown",
		}
	}
	if s == nil {
		snapshot.available = int64(len(keys))
		return snapshot
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	for index, hash := range hashes {
		state := s.keyHealth[hash]
		if state == nil {
			snapshot.available++
			continue
		}
		status := &snapshot.statuses[index]
		status.FailureCount = state.FailureCount
		status.SuccessCount = state.SuccessCount
		status.LastError = state.LastError
		status.LastLatencyMS = state.LastLatencyMS
		status.LastHTTPStatus = state.LastHTTPStatus
		status.LastTested = state.LastTested
		if !state.LastCheckedAt.IsZero() {
			lastCheckedAt := state.LastCheckedAt
			status.LastCheckedAt = &lastCheckedAt
		}
		if state.FrozenUntil.After(now) {
			frozenUntil := state.FrozenUntil
			status.FrozenUntil = &frozenUntil
			status.Status = "frozen"
		} else {
			snapshot.available++
			if state.LastError != "" {
				status.Status = "error"
			} else if state.SuccessCount > 0 || state.LastTested {
				status.Status = "ok"
			}
		}

		load := &snapshot.loads[index]
		load.Status = status.Status
		load.LastLatencyMS = state.LastLatencyMS
		load.LastHTTPStatus = state.LastHTTPStatus
		load.Active = state.SyncActive
		load.Total = state.SyncTotal
		load.Success = state.SyncSuccess
		load.Errors = state.SyncErrors
		if state.SyncTotal > 0 {
			load.AvgLatencyMS = state.SyncLatencyMS / state.SyncTotal
		}
		snapshot.active += state.SyncActive
		snapshot.total += state.SyncTotal
	}
	return snapshot
}

func (s *ContentModerationService) apiKeyStatuses(keys []string) []ContentModerationAPIKeyStatus {
	out := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.apiKeyStatusForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key), true))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyLoads(keys []string) []ContentModerationAPIKeyLoad {
	out := make([]ContentModerationAPIKeyLoad, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.preBlockAPIKeyLoadForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key)))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyActive(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Active
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyAvailableCount(keys []string) int64 {
	now := time.Now()
	var count int64
	for _, key := range keys {
		if !s.isAPIKeyFrozen(key, now) {
			count++
		}
	}
	return count
}

func (s *ContentModerationService) preBlockAPIKeyTotalCalls(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Total
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyLoadForHash(index int, hash string, masked string) ContentModerationAPIKeyLoad {
	load := ContentModerationAPIKeyLoad{
		Index:   index,
		KeyHash: hash,
		Masked:  masked,
		Status:  "unknown",
	}
	status := s.apiKeyStatusForHash(index, hash, masked, true)
	load.Status = status.Status
	load.LastLatencyMS = status.LastLatencyMS
	load.LastHTTPStatus = status.LastHTTPStatus
	if hash == "" || s == nil {
		return load
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return load
	}
	load.Active = state.SyncActive
	load.Total = state.SyncTotal
	load.Success = state.SyncSuccess
	load.Errors = state.SyncErrors
	if state.SyncTotal > 0 {
		load.AvgLatencyMS = state.SyncLatencyMS / state.SyncTotal
	}
	return load
}

func (s *ContentModerationService) apiKeyStatusForHash(index int, hash string, masked string, configured bool) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{
		Index:      index,
		KeyHash:    hash,
		Masked:     masked,
		Status:     "unknown",
		Configured: configured,
	}
	if hash == "" || s == nil {
		return status
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return status
	}
	status.FailureCount = state.FailureCount
	status.SuccessCount = state.SuccessCount
	status.LastError = state.LastError
	status.LastLatencyMS = state.LastLatencyMS
	status.LastHTTPStatus = state.LastHTTPStatus
	status.LastTested = state.LastTested
	if !state.LastCheckedAt.IsZero() {
		t := state.LastCheckedAt
		status.LastCheckedAt = &t
	}
	if state.FrozenUntil.After(now) {
		t := state.FrozenUntil
		status.FrozenUntil = &t
		status.Status = "frozen"
		return status
	}
	if state.LastError != "" {
		status.Status = "error"
		return status
	}
	if state.SuccessCount > 0 || state.LastTested {
		status.Status = "ok"
	}
	return status
}

func moderationAPIKeyHash(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func buildModerationTestInput(prompt string, images []string) (any, int, error) {
	prompt = trimRunes(normalizeContentModerationText(prompt), maxModerationInputRunes)
	normalizedImages := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if len(normalizedImages) >= maxContentModerationTestImages {
			return nil, 0, infraerrors.BadRequest("TOO_MANY_MODERATION_TEST_IMAGES", fmt.Sprintf("最多上传 %d 张测试图片", maxContentModerationTestImages))
		}
		if err := validateModerationTestImageDataURL(image); err != nil {
			return nil, 0, err
		}
		normalizedImages = append(normalizedImages, image)
	}
	if prompt == "" && len(normalizedImages) == 0 {
		return "hello", 0, nil
	}
	if len(normalizedImages) == 0 {
		return prompt, 0, nil
	}
	parts := make([]moderationAPIInputPart, 0, len(normalizedImages)+1)
	if prompt != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: prompt})
	}
	for _, image := range normalizedImages {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts, len(normalizedImages), nil
}

func contentModerationTestHasAuditInput(prompt string, images []string) bool {
	if normalizeContentModerationText(prompt) != "" {
		return true
	}
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			return true
		}
	}
	return false
}

func validateModerationTestImageDataURL(value string) error {
	if len(value) > maxContentModerationTestImageDataURLBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	if !strings.HasPrefix(value, "data:image/") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 data:image/* base64")
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 base64 data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片 base64 无效")
	}
	if len(raw) > maxContentModerationTestImageBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	return nil
}

func buildContentModerationTestAuditResult(result *moderationAPIResult, thresholds map[string]float64) *ContentModerationTestAuditResult {
	if result == nil {
		return nil
	}
	scores := make(map[string]float64, len(result.CategoryScores))
	for category, score := range result.CategoryScores {
		scores[category] = score
	}
	thresholdSnapshot := mergeContentModerationThresholds(ContentModerationDefaultThresholds(), thresholds)
	flagged, highestCategory, highestScore := evaluateModerationScores(scores, thresholdSnapshot)
	compositeScore := highestScore
	return &ContentModerationTestAuditResult{
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CompositeScore:  compositeScore,
		CategoryScores:  scores,
		Thresholds:      thresholdSnapshot,
	}
}

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIInputPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *moderationAPIImageURLRef `json:"image_url,omitempty"`
}

type moderationAPIImageURLRef struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

func evaluateModerationScores(scores map[string]float64, thresholds map[string]float64) (bool, string, float64) {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	for _, category := range contentModerationCategoryOrder {
		score := scores[category]
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
		if score >= thresholds[category] {
			flagged = true
		}
	}
	for category, score := range scores {
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
	}
	return flagged, highestCategory, highestScore
}

func mergeContentModerationThresholds(base map[string]float64, override map[string]float64) map[string]float64 {
	out := cloneFloatMap(base)
	if out == nil {
		out = map[string]float64{}
	}
	for _, category := range contentModerationCategoryOrder {
		if v, ok := override[category]; ok {
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			out[category] = v
		}
	}
	return out
}

func normalizeInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeBlockedKeywords(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		kw := strings.TrimSpace(raw)
		if kw == "" {
			continue
		}
		kw = trimRunes(kw, maxContentModerationBlockedKeywordRunes)
		key := strings.ToLower(kw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, kw)
		if len(out) >= maxContentModerationBlockedKeywords {
			break
		}
	}
	return out
}

func normalizeKeywordBlockingMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ContentModerationKeywordModeKeywordOnly:
		return ContentModerationKeywordModeKeywordOnly
	case ContentModerationKeywordModeAPIOnly:
		return ContentModerationKeywordModeAPIOnly
	case ContentModerationKeywordModeKeywordAndAPI:
		return ContentModerationKeywordModeKeywordAndAPI
	default:
		return ContentModerationKeywordModeKeywordAndAPI
	}
}

func normalizeContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	out := ContentModerationModelFilter{
		Type:   normalizeContentModerationModelFilterType(filter.Type),
		Models: normalizeContentModerationModelNames(filter.Models),
	}
	if out.Type == ContentModerationModelFilterAll {
		out.Models = []string{}
	}
	return out
}

func cloneContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	normalized := normalizeContentModerationModelFilter(filter)
	normalized.Models = append([]string(nil), normalized.Models...)
	return normalized
}

func normalizeContentModerationModelFilterType(filterType string) string {
	switch strings.ToLower(strings.TrimSpace(filterType)) {
	case ContentModerationModelFilterInclude:
		return ContentModerationModelFilterInclude
	case ContentModerationModelFilterExclude:
		return ContentModerationModelFilterExclude
	case ContentModerationModelFilterAll:
		return ContentModerationModelFilterAll
	default:
		return ContentModerationModelFilterAll
	}
}

func normalizeContentModerationModelNames(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		model := trimRunes(strings.TrimSpace(raw), maxContentModerationModelFilterRunes)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
		if len(out) >= maxContentModerationModelFilterModels {
			break
		}
	}
	return out
}

func contentModerationModelListContains(models []string, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, candidate := range models {
		if strings.ToLower(strings.TrimSpace(candidate)) == model {
			return true
		}
	}
	return false
}

func matchBlockedKeyword(text string, keywords []string) (string, bool) {
	if text == "" || len(keywords) == 0 {
		return "", false
	}
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			return kw, true
		}
	}
	return "", false
}

func normalizeModerationAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func deleteModerationAPIKeysByHash(keys []string, hashes []string) []string {
	keys = normalizeModerationAPIKeys(keys)
	deleteHashes := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = normalizeContentModerationHash(hash)
		if hash != "" {
			deleteHashes[hash] = struct{}{}
		}
	}
	if len(deleteHashes) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := deleteHashes[moderationAPIKeyHash(key)]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func normalizeContentModerationAPIKeysMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case contentModerationAPIKeysModeReplace:
		return contentModerationAPIKeysModeReplace
	default:
		return contentModerationAPIKeysModeAppend
	}
}

func normalizeContentModerationHash(inputHash string) string {
	inputHash = strings.ToLower(strings.TrimSpace(inputHash))
	if len(inputHash) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(inputHash); err != nil {
		return ""
	}
	return inputHash
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

func maskSecretTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return strings.Repeat("*", 8) + secret[len(secret)-4:]
}

// CyberPolicyRecordInput 是一次 cyber_policy 硬阻断的风控记录入参。
type CyberPolicyRecordInput struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	UpstreamMessage string
	UpstreamBody    string
	UpstreamStatus  int
	UpstreamInTok   int
	UpstreamOutTok  int
}

// RecordCyberPolicyEvent 把一次 cyber_policy 硬阻断写入风控中心日志、计入违规计数、
// 并给用户发邮件。当前请求已由 gateway 透传给用户；本方法仅做事后记录/通知/计数。
// 仅受 risk_control_enabled 总开关约束（不受内容审核 Enabled/Mode/scope/sample 约束）。
func (s *ContentModerationService) RecordCyberPolicyEvent(ctx context.Context, in CyberPolicyRecordInput) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.isRiskControlEnabled(ctx) {
		return
	}
	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		slog.Warn("content_moderation.cyber_load_config_failed", "error", err)
		cfg = &ContentModerationConfig{}
	}
	var userID *int64
	if in.UserID > 0 {
		userID = &in.UserID
	}
	var apiKeyID *int64
	if in.APIKeyID > 0 {
		apiKeyID = &in.APIKeyID
	}
	errBody := strings.TrimSpace(in.UpstreamMessage)
	if b := strings.TrimSpace(in.UpstreamBody); b != "" {
		// 原始 body 不在此预脱敏；写入 log.Error 前由 redactContentModerationSecrets 统一脱敏。
		errBody = strings.TrimSpace(errBody + "\n" + b)
	}
	if in.UpstreamInTok > 0 || in.UpstreamOutTok > 0 {
		errBody = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errBody, in.UpstreamInTok, in.UpstreamOutTok)
	}
	log := &ContentModerationLog{
		RequestID:       in.RequestID,
		UserID:          userID,
		UserEmail:       in.UserEmail,
		APIKeyID:        apiKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Provider:        "openai",
		Model:           in.Model,
		Mode:            "post_upstream",
		Action:          ContentModerationActionCyberPolicy,
		Flagged:         true,
		HighestCategory: "cyber_policy",
		HighestScore:    1.0,
		Error:           trimRunes(redactContentModerationSecrets(errBody), maxModerationExcerptRunes*4),
		CreatedAt:       time.Now(),
	}
	// 开关开时 cyber_policy 不参与封号计数：当次不判定（此处跳过），
	// 历史行由 CountFlaggedByUserSince 的 excludeCyberPolicy 排除。
	autoBanned := false
	if !cfg.CyberPolicyExcludeFromBanCount {
		autoBanned = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
	}
	log.EmailSent = false
	logPersisted := true
	if err := s.repo.CreateLog(ctx, log); err != nil {
		logPersisted = false
		slog.Warn("content_moderation.cyber_create_log_failed", "user_id", in.UserID, "error", err)
	}
	emailSent := false
	if s.emailService != nil && strings.TrimSpace(log.UserEmail) != "" {
		if err := s.sendCyberPolicyEmail(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_email_failed", "user_id", in.UserID, "error", err)
		} else {
			emailSent = true
		}
		if autoBanned {
			if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
				slog.Warn("content_moderation.cyber_ban_email_failed", "user_id", in.UserID, "error", err)
			} else {
				emailSent = true
			}
		}
	}
	if logPersisted && emailSent {
		if err := s.repo.UpdateLogEmailSent(ctx, log.ID, true); err != nil {
			slog.Warn("content_moderation.cyber_update_email_sent_failed", "log_id", log.ID, "error", err)
		}
	}
}

func (s *ContentModerationService) sendCyberPolicyEmail(ctx context.Context, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		variables := map[string]string{
			"triggered_at":     log.CreatedAt.UTC().Format(time.RFC3339),
			"model":            defaultContentModerationString(log.Model, "-"),
			"group_name":       defaultContentModerationString(log.GroupName, "-"),
			"upstream_message": defaultContentModerationString(log.Error, "-"),
		}
		err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventCyberPolicyNotice,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      variables,
		})
		if err == nil {
			return nil
		}
		if !shouldFallbackNotificationEmail(err) {
			return err
		}
		slog.Warn("template cyber policy email failed; falling back", "err", err.Error())
	}
	subject := fmt.Sprintf("[%s] 网络安全策略拦截 / Cyber Policy Notice", sanitizeEmailHeader(siteName))
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, buildCyberPolicyNoticeEmailBody(siteName, log))
}
