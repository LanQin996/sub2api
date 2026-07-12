package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type upstreamContextTestKey string

type blockingPlatformQuotaRepository struct {
	UserPlatformQuotaRepository
	started chan struct{}
	release chan struct{}
}

func (r *blockingPlatformQuotaRepository) IncrementUsageWithReset(ctx context.Context, _ int64, _ string, _ float64, _ time.Time) error {
	close(r.started)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newStreamingResponseTestGatewayService() *GatewayService {
	return &GatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamDataIntervalTimeout: 0,
				MaxLineSize:               defaultMaxLineSize,
			},
		},
		rateLimitService: &RateLimitService{},
	}
}

func TestGatewayService_StreamingReusesScannerBufferAndStillParsesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		// Minimal SSE event to trigger parseSSEUsage
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
}

func TestGatewayService_StreamingKeepaliveUsesIdleTimer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), "event: ping")
}

func TestGatewayService_StreamingKeepaliveUsesNoopDeltaForAffectedClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.198 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: content_block_delta")
	require.Contains(t, body, `"delta":{"type":"text_delta","text":""}`)
}

func TestGatewayService_StreamingKeepaliveUsesNoopDeltaDuringToolUseForAffectedClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.198 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"Edit\",\"input\":{}}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: content_block_delta")
	require.Contains(t, body, `"index":1`)
	require.Contains(t, body, `"delta":{"type":"input_json_delta","partial_json":""}`)
}

func TestGatewayService_StreamingKeepaliveKeepsPingForOlderClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.187 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: ping")
	require.NotContains(t, body, `"delta":{"type":"text_delta","text":""}`)
}

func TestDetachUpstreamContextIgnoresClientCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), upstreamContextTestKey("test-key"), "test-value"))
	upstreamCtx, release := detachUpstreamContext(parent)
	defer release()

	cancel()

	require.NoError(t, upstreamCtx.Err())
	require.Equal(t, "test-value", upstreamCtx.Value(upstreamContextTestKey("test-key")))
}

func TestDetachUpstreamContextCancelsAfterDisconnectGrace(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	upstreamCtx, release := detachUpstreamContextWithGrace(parent, 20*time.Millisecond)
	defer release()

	cancelParent()
	require.NoError(t, upstreamCtx.Err(), "disconnect grace should allow a short billing drain")

	select {
	case <-upstreamCtx.Done():
		require.ErrorIs(t, upstreamCtx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("detached upstream context did not cancel after grace")
	}
}

func TestDetachUpstreamContextReleaseCancelsImmediately(t *testing.T) {
	upstreamCtx, release := detachUpstreamContextWithGrace(context.Background(), time.Hour)
	release()

	select {
	case <-upstreamCtx.Done():
		require.ErrorIs(t, upstreamCtx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("release did not cancel detached upstream context")
	}
}

func TestDetachUpstreamContextReleaseStopsPendingDisconnectTimer(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	upstreamCtx, release := detachUpstreamContextWithGrace(parent, time.Hour)
	cancelParent()
	// Let the parent callback install its grace timer before release races it.
	time.Sleep(10 * time.Millisecond)

	release()
	select {
	case <-upstreamCtx.Done():
		require.ErrorIs(t, upstreamCtx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("release did not cancel context with a pending disconnect timer")
	}
}

func TestDetachUpstreamContextGraceDoesNotRetainPerRequestGoroutines(t *testing.T) {
	const requestCount = 128
	baseline := runtime.NumGoroutine()
	releases := make([]context.CancelFunc, 0, requestCount)

	for i := 0; i < requestCount; i++ {
		parent, cancelParent := context.WithCancel(context.Background())
		_, release := detachUpstreamContextWithGrace(parent, time.Hour)
		releases = append(releases, release)
		cancelParent()
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()

	// The parent callbacks should only install runtime timers and return. The old
	// implementation blocked one callback goroutine per request for the full grace.
	time.Sleep(25 * time.Millisecond)
	require.Eventually(t, func() bool {
		runtime.Gosched()
		return runtime.NumGoroutine() <= baseline+16
	}, time.Second, 10*time.Millisecond)
}

func TestDetachedBillingContextHasFiniteDeadline(t *testing.T) {
	billingCtx, cancel := detachedBillingContext(context.Background())
	defer cancel()

	deadline, ok := billingCtx.Deadline()
	require.True(t, ok)
	remaining := time.Until(deadline)
	require.Greater(t, remaining, time.Duration(0))
	require.LessOrEqual(t, remaining, postUsageBillingTimeout)
}

func TestPersistUserPlatformQuotaUsageAppliesBackpressure(t *testing.T) {
	repo := &blockingPlatformQuotaRepository{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- persistUserPlatformQuotaUsage(context.Background(), repo, 1, PlatformOpenAI, 0.25)
	}()

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("quota persistence did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("quota persistence returned before the repository completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(repo.release)
	require.NoError(t, <-done)
}

func TestBindUpstreamContextToResponseReleasesOnBodyClose(t *testing.T) {
	upstreamCtx, release := detachUpstreamContextWithGrace(context.Background(), time.Hour)
	resp := bindUpstreamContextToResponse(&http.Response{Body: io.NopCloser(strings.NewReader("ok"))}, release)

	require.NoError(t, resp.Body.Close())
	select {
	case <-upstreamCtx.Done():
		require.ErrorIs(t, upstreamCtx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("response body close did not release upstream context")
	}
}

func TestNotificationChecksShortCircuitDefaultDisabledState(t *testing.T) {
	deps := &billingDeps{balanceNotifyService: &BalanceNotifyService{}}
	p := &postUsageBillingParams{
		Cost:    &CostBreakdown{ActualCost: 1, TotalCost: 1},
		User:    &User{},
		Account: &Account{Type: AccountTypeAPIKey},
	}

	require.False(t, shouldCheckBalanceLowNotification(context.Background(), p, deps))
	require.False(t, shouldCheckAccountQuotaNotification(context.Background(), p, deps))

	p.User.BalanceNotifyEnabled = true
	p.Account.Extra = map[string]any{
		"quota_notify_daily_enabled":   true,
		"quota_notify_daily_threshold": 10.0,
	}
	settingRepo := &gatewayTTLSettingRepo{data: map[string]string{
		SettingKeyBalanceLowNotifyEnabled:   "true",
		SettingKeyBalanceLowNotifyThreshold: "10",
		SettingKeyAccountQuotaNotifyEnabled: "true",
	}}
	deps.balanceNotifyService = NewBalanceNotifyService(NewEmailService(settingRepo, nil), settingRepo, nil)
	require.True(t, shouldCheckBalanceLowNotification(context.Background(), p, deps))
	require.True(t, shouldCheckAccountQuotaNotification(context.Background(), p, deps))
}
