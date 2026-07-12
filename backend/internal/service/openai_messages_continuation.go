package service

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Compatibility state contains client/upstream-controlled strings. The per-item
// limit allows large turn-state headers, while the total budget bounds the case
// where many entries approach that limit.
const (
	openAICompatCacheMaxEntries       = 4096
	openAICompatCacheCleanupBatchSize = 16
	openAICompatCacheMaxKeyBytes      = 512
	openAICompatCacheMaxPayloadBytes  = 64 * 1024
	openAICompatCacheMaxTotalBytes    = 32 * 1024 * 1024
)

type openAICompatCacheItem[T any] struct {
	key       string
	value     T
	expiresAt time.Time
	bytes     int
	heapIndex int
}

type openAICompatExpiryHeap[T any] []*openAICompatCacheItem[T]

func (h openAICompatExpiryHeap[T]) Len() int { return len(h) }

func (h openAICompatExpiryHeap[T]) Less(i, j int) bool {
	return h[i].expiresAt.Before(h[j].expiresAt)
}

func (h openAICompatExpiryHeap[T]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *openAICompatExpiryHeap[T]) Push(value any) {
	item := value.(*openAICompatCacheItem[T])
	item.heapIndex = len(*h)
	*h = append(*h, item)
}

func (h *openAICompatExpiryHeap[T]) Pop() any {
	old := *h
	last := len(old) - 1
	item := old[last]
	old[last] = nil
	item.heapIndex = -1
	*h = old[:last]
	return item
}

// openAICompatBoundedCache keeps compatibility session state bounded without
// adding a background goroutine. Writes incrementally drain the expiry heap.
type openAICompatBoundedCache[T any] struct {
	mu         sync.RWMutex
	maxEntries int
	maxBytes   int
	bytes      int
	items      map[string]*openAICompatCacheItem[T]
	expiry     openAICompatExpiryHeap[T]
}

func (c *openAICompatBoundedCache[T]) Load(key string) (T, bool) {
	var zero T
	if c == nil || key == "" {
		return zero, false
	}
	key = normalizeOpenAICompatCacheKey(key)
	now := time.Now()

	c.mu.RLock()
	item, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		return zero, false
	}
	if !now.After(item.expiresAt) {
		value := item.value
		c.mu.RUnlock()
		return value, true
	}
	c.mu.RUnlock()

	c.mu.Lock()
	item, ok = c.items[key]
	if !ok {
		c.mu.Unlock()
		return zero, false
	}
	if now.After(item.expiresAt) {
		c.removeLocked(item)
		c.mu.Unlock()
		return zero, false
	}
	value := item.value
	c.mu.Unlock()
	return value, true
}

func (c *openAICompatBoundedCache[T]) Store(key string, value T, expiresAt time.Time, payloadBytes int) bool {
	if c == nil || key == "" {
		return false
	}
	if expiresAt.IsZero() || payloadBytes < 0 || payloadBytes > openAICompatCacheMaxPayloadBytes {
		c.Delete(key)
		return false
	}
	now := time.Now()
	if now.After(expiresAt) {
		c.Delete(key)
		return false
	}
	key = strings.Clone(normalizeOpenAICompatCacheKey(key))

	c.mu.Lock()
	c.ensureInitializedLocked()
	if payloadBytes > c.byteCapacityLocked() {
		if item, ok := c.items[key]; ok {
			c.removeLocked(item)
		}
		c.mu.Unlock()
		return false
	}
	c.removeExpiredLocked(now, openAICompatCacheCleanupBatchSize)
	if item, ok := c.items[key]; ok {
		c.removeLocked(item)
	}
	for len(c.items) >= c.capacityLocked() || c.bytes+payloadBytes > c.byteCapacityLocked() {
		c.removeLocked(c.expiry[0])
	}
	item := &openAICompatCacheItem[T]{key: key, value: value, expiresAt: expiresAt, bytes: payloadBytes}
	c.items[key] = item
	c.bytes += payloadBytes
	heap.Push(&c.expiry, item)
	c.mu.Unlock()
	return true
}

func (c *openAICompatBoundedCache[T]) Delete(key string) {
	if c == nil || key == "" {
		return
	}
	key = normalizeOpenAICompatCacheKey(key)
	c.mu.Lock()
	if item, ok := c.items[key]; ok {
		c.removeLocked(item)
	}
	c.mu.Unlock()
}

func (c *openAICompatBoundedCache[T]) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return size
}

func (c *openAICompatBoundedCache[T]) ensureInitializedLocked() {
	if c.items == nil {
		c.items = make(map[string]*openAICompatCacheItem[T])
	}
	if c.expiry == nil {
		c.expiry = make(openAICompatExpiryHeap[T], 0)
	}
}

func (c *openAICompatBoundedCache[T]) capacityLocked() int {
	if c.maxEntries > 0 {
		return c.maxEntries
	}
	return openAICompatCacheMaxEntries
}

func (c *openAICompatBoundedCache[T]) byteCapacityLocked() int {
	if c.maxBytes > 0 {
		return c.maxBytes
	}
	return openAICompatCacheMaxTotalBytes
}

func (c *openAICompatBoundedCache[T]) removeExpiredLocked(now time.Time, limit int) {
	for removed := 0; removed < limit && len(c.expiry) > 0; removed++ {
		item := c.expiry[0]
		if !now.After(item.expiresAt) {
			return
		}
		c.removeLocked(item)
	}
}

func (c *openAICompatBoundedCache[T]) removeLocked(item *openAICompatCacheItem[T]) {
	if item == nil || item.heapIndex < 0 || item.heapIndex >= len(c.expiry) {
		return
	}
	delete(c.items, item.key)
	c.bytes -= item.bytes
	if c.bytes < 0 {
		c.bytes = 0
	}
	heap.Remove(&c.expiry, item.heapIndex)
}

func normalizeOpenAICompatCacheKey(key string) string {
	if len(key) <= openAICompatCacheMaxKeyBytes {
		return key
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(key)))
}

type openAICompatSessionResponseBinding struct {
	ResponseID           string
	TurnState            string
	ContinuationDisabled bool
	ExpiresAt            time.Time
}

func openAICompatContinuationEnabled(account *Account, model string) bool {
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	return shouldAutoInjectPromptCacheKeyForCompat(model)
}

func trimAnthropicCompatResponsesInputToLatestTurn(req *apicompat.ResponsesRequest) {
	if req == nil || len(req.Input) == 0 {
		return
	}

	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil || len(items) == 0 {
		return
	}

	start := latestAnthropicCompatResponsesInputTurnStart(items)
	trimmed := append([]apicompat.ResponsesInputItem(nil), items[start:]...)
	if len(trimmed) == len(items) {
		return
	}
	if input, err := json.Marshal(trimmed); err == nil {
		req.Input = input
	}
}

func latestAnthropicCompatResponsesInputTurnStart(items []apicompat.ResponsesInputItem) int {
	if len(items) == 0 {
		return 0
	}

	start := len(items) - 1
	last := items[start]
	switch {
	case last.Type == "function_call_output":
		for start > 0 && items[start-1].Type == "function_call_output" {
			start--
		}
	case last.Type == "message" && last.Role == "user":
		for start > 0 && items[start-1].Type == "function_call_output" {
			start--
		}
	default:
		return start
	}

	return expandAnthropicCompatResponsesInputToolCallStart(items, start)
}

func expandAnthropicCompatResponsesInputToolCallStart(items []apicompat.ResponsesInputItem, start int) int {
	if start < 0 || start >= len(items) {
		return start
	}

	needed := make(map[string]struct{})
	for i := start; i < len(items); i++ {
		if items[i].Type != "function_call_output" {
			continue
		}
		callID := strings.TrimSpace(items[i].CallID)
		if callID != "" {
			needed[callID] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return start
	}

	expandedStart := start
	for i := start - 1; i >= 0 && len(needed) > 0; i-- {
		if items[i].Type != "function_call" {
			continue
		}
		callID := strings.TrimSpace(items[i].CallID)
		if _, ok := needed[callID]; !ok {
			continue
		}
		delete(needed, callID)
		expandedStart = i
	}
	return expandedStart
}

func isOpenAICompatPreviousResponseNotFound(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusNotFound {
		return false
	}
	check := func(s string) bool {
		lower := strings.ToLower(strings.TrimSpace(s))
		return strings.Contains(lower, "previous_response_not_found") ||
			(strings.Contains(lower, "previous response") && strings.Contains(lower, "not found")) ||
			(strings.Contains(lower, "unsupported parameter") && strings.Contains(lower, "previous_response_id"))
	}
	if check(upstreamMsg) || check(string(upstreamBody)) {
		return true
	}
	return check(gjson.GetBytes(upstreamBody, "error.code").String()) ||
		check(gjson.GetBytes(upstreamBody, "error.message").String())
}

func isOpenAICompatPreviousResponseUnsupported(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	check := func(s string) bool {
		lower := strings.ToLower(strings.TrimSpace(s))
		if !strings.Contains(lower, "previous_response_id") {
			return false
		}
		return strings.Contains(lower, "unsupported parameter") ||
			strings.Contains(lower, "only supported on responses websocket") ||
			strings.Contains(lower, "not supported")
	}
	if check(upstreamMsg) || check(string(upstreamBody)) {
		return true
	}
	return check(gjson.GetBytes(upstreamBody, "error.code").String()) ||
		check(gjson.GetBytes(upstreamBody, "error.message").String())
}

func openAICompatSessionResponseKey(c *gin.Context, account *Account, promptCacheKey string) string {
	key := strings.TrimSpace(promptCacheKey)
	if account == nil || key == "" {
		return ""
	}
	apiKeyID := int64(0)
	if c != nil {
		apiKeyID = getAPIKeyIDFromContext(c)
	}
	return strings.Join([]string{
		strconv.FormatInt(account.ID, 10),
		strconv.FormatInt(apiKeyID, 10),
		key,
	}, "\x00")
}

func (s *OpenAIGatewayService) getOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey string) string {
	if s == nil {
		return ""
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return ""
	}
	binding, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return ""
	}
	if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
		s.openaiCompatSessionResponses.Delete(key)
		return ""
	}
	if binding.ContinuationDisabled {
		return ""
	}
	if strings.TrimSpace(binding.ResponseID) == "" {
		s.openaiCompatSessionResponses.Delete(key)
		return ""
	}
	return strings.TrimSpace(binding.ResponseID)
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey, responseID string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	id := strings.Clone(strings.TrimSpace(responseID))
	if key == "" || id == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		ResponseID: id,
		ExpiresAt:  time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if existing, ok := s.openaiCompatSessionResponses.Load(key); ok {
		if existing.ContinuationDisabled {
			existing.ResponseID = ""
			existing.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
			s.storeOpenAICompatSessionResponse(key, existing)
			return
		}
		binding.TurnState = existing.TurnState
	}
	s.storeOpenAICompatSessionResponse(key, binding)
}

func (s *OpenAIGatewayService) deleteOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return
	}
	binding, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return
	}
	binding.ResponseID = ""
	if strings.TrimSpace(binding.TurnState) == "" && !binding.ContinuationDisabled {
		s.openaiCompatSessionResponses.Delete(key)
		return
	}
	binding.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
	s.storeOpenAICompatSessionResponse(key, binding)
}

func (s *OpenAIGatewayService) disableOpenAICompatSessionContinuation(_ context.Context, c *gin.Context, account *Account, promptCacheKey string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		ContinuationDisabled: true,
		ExpiresAt:            time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if existing, ok := s.openaiCompatSessionResponses.Load(key); ok {
		binding.TurnState = existing.TurnState
	}
	s.storeOpenAICompatSessionResponse(key, binding)
}

func (s *OpenAIGatewayService) isOpenAICompatSessionContinuationDisabled(_ context.Context, c *gin.Context, account *Account, promptCacheKey string) bool {
	if s == nil {
		return false
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return false
	}
	binding, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return false
	}
	if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
		s.openaiCompatSessionResponses.Delete(key)
		return false
	}
	return binding.ContinuationDisabled
}

func (s *OpenAIGatewayService) getOpenAICompatSessionTurnState(_ context.Context, c *gin.Context, account *Account, promptCacheKey string) string {
	if s == nil {
		return ""
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return ""
	}
	binding, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return ""
	}
	if strings.TrimSpace(binding.TurnState) == "" {
		return ""
	}
	if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
		s.openaiCompatSessionResponses.Delete(key)
		return ""
	}
	return strings.TrimSpace(binding.TurnState)
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionTurnState(_ context.Context, c *gin.Context, account *Account, promptCacheKey, turnState string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	state := strings.Clone(strings.TrimSpace(turnState))
	if key == "" || state == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		TurnState: state,
		ExpiresAt: time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if existing, ok := s.openaiCompatSessionResponses.Load(key); ok {
		binding.ResponseID = existing.ResponseID
		binding.ContinuationDisabled = existing.ContinuationDisabled
	}
	s.storeOpenAICompatSessionResponse(key, binding)
}

func (s *OpenAIGatewayService) storeOpenAICompatSessionResponse(key string, binding openAICompatSessionResponseBinding) bool {
	payloadBytes := len(binding.ResponseID) + len(binding.TurnState)
	return s.openaiCompatSessionResponses.Store(key, binding, binding.ExpiresAt, payloadBytes)
}
