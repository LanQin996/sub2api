//go:build unit

package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompatBoundedCacheEvictsEarliestExpiryAtCapacity(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	cache.maxEntries = 2
	now := time.Now()

	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "late", "late", now.Add(3*time.Hour)))
	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "early", "early", now.Add(time.Hour)))
	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "middle", "middle", now.Add(2*time.Hour)))

	_, ok := cache.Load("early")
	require.False(t, ok)
	_, ok = cache.Load("late")
	require.True(t, ok)
	_, ok = cache.Load("middle")
	require.True(t, ok)
	require.Equal(t, 2, cache.Len())
}

func TestOpenAICompatBoundedCacheActivelyRemovesExpiredEntries(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	cache.maxEntries = openAICompatCacheCleanupBatchSize + 2
	expiresAt := time.Now().Add(20 * time.Millisecond)
	for i := 0; i < openAICompatCacheCleanupBatchSize; i++ {
		require.True(t, storeOpenAICompatCacheTestBinding(&cache, fmt.Sprintf("expired-%d", i), "value", expiresAt))
	}
	time.Sleep(30 * time.Millisecond)

	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "fresh", "value", time.Now().Add(time.Hour)))

	require.Equal(t, 1, cache.Len())
	_, ok := cache.Load("fresh")
	require.True(t, ok)
}

func TestOpenAICompatBoundedCacheNormalizesLongKeys(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	key := strings.Repeat("x", openAICompatCacheMaxKeyBytes+1)

	require.True(t, storeOpenAICompatCacheTestBinding(&cache, key, "response", time.Now().Add(time.Hour)))

	binding, ok := cache.Load(key)
	require.True(t, ok)
	require.Equal(t, "response", binding.ResponseID)
	cache.mu.RLock()
	require.Len(t, cache.items, 1)
	for storedKey := range cache.items {
		require.True(t, strings.HasPrefix(storedKey, "sha256:"))
		require.Len(t, storedKey, len("sha256:")+64)
	}
	cache.mu.RUnlock()
}

func TestOpenAICompatBoundedCacheRejectsOversizedPayload(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	binding := openAICompatSessionResponseBinding{
		ResponseID: strings.Repeat("r", openAICompatCacheMaxPayloadBytes+1),
		ExpiresAt:  time.Now().Add(time.Hour),
	}

	stored := cache.Store("key", binding, binding.ExpiresAt, len(binding.ResponseID))

	require.False(t, stored)
	require.Zero(t, cache.Len())
}

func TestOpenAICompatBoundedCacheOversizedReplacementRemovesStaleValue(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	expiresAt := time.Now().Add(time.Hour)
	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "key", "old-response", expiresAt))

	oversized := openAICompatSessionResponseBinding{
		ResponseID: strings.Repeat("r", openAICompatCacheMaxPayloadBytes+1),
		ExpiresAt:  expiresAt,
	}
	require.False(t, cache.Store("key", oversized, expiresAt, len(oversized.ResponseID)))

	_, ok := cache.Load("key")
	require.False(t, ok)
	require.Zero(t, cache.Len())
}

func TestOpenAICompatBoundedCacheEvictsToStayWithinByteBudget(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	cache.maxBytes = 10
	expiresAt := time.Now().Add(time.Hour)

	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "key1", "123456", expiresAt))
	require.True(t, storeOpenAICompatCacheTestBinding(&cache, "key2", "abcdef", expiresAt))

	_, ok := cache.Load("key1")
	require.False(t, ok)
	binding, ok := cache.Load("key2")
	require.True(t, ok)
	require.Equal(t, "abcdef", binding.ResponseID)
	require.Equal(t, 6, cache.bytes)
}

func TestOpenAICompatSessionResponseCacheIsBounded(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.openaiCompatSessionResponses.maxEntries = 2
	account := &Account{ID: 42, Type: AccountTypeAPIKey}

	svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, "key1", "response1")
	svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, "key2", "response2")
	svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, "key3", "response3")

	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), nil, account, "key1"))
	require.Equal(t, "response2", svc.getOpenAICompatSessionResponseID(context.Background(), nil, account, "key2"))
	require.Equal(t, "response3", svc.getOpenAICompatSessionResponseID(context.Background(), nil, account, "key3"))
	require.Equal(t, 2, svc.openaiCompatSessionResponses.Len())
}

func TestOpenAICompatSessionResponseCacheDoesNotRetainOversizedState(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Type: AccountTypeAPIKey}
	responseID := strings.Repeat("r", openAICompatCacheMaxPayloadBytes+1)

	svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, "key", responseID)

	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), nil, account, "key"))
	require.Zero(t, svc.openaiCompatSessionResponses.Len())
}

func TestOpenAICompatSessionResponseCacheSupportsLongHashedKey(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Type: AccountTypeAPIKey}
	promptCacheKey := strings.Repeat("session-key-", 100)

	svc.bindOpenAICompatSessionResponseID(context.Background(), nil, account, promptCacheKey, "response")

	require.Equal(t, "response", svc.getOpenAICompatSessionResponseID(context.Background(), nil, account, promptCacheKey))
	svc.openaiCompatSessionResponses.mu.RLock()
	require.Len(t, svc.openaiCompatSessionResponses.items, 1)
	for storedKey := range svc.openaiCompatSessionResponses.items {
		require.True(t, strings.HasPrefix(storedKey, "sha256:"))
	}
	svc.openaiCompatSessionResponses.mu.RUnlock()
}

func TestOpenAICompatAnthropicDigestCacheIsBoundedAndSupportsLongChains(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.openaiCompatAnthropicDigestSessions.maxEntries = 2
	account := &Account{ID: 42, Type: AccountTypeAPIKey}
	longChain := strings.Repeat("u:0123456789abcdef-", 40)

	svc.bindOpenAICompatAnthropicDigestPromptCacheKey(account, 7, "chain1", "prompt1", "")
	svc.bindOpenAICompatAnthropicDigestPromptCacheKey(account, 7, "chain2", "prompt2", "")
	svc.bindOpenAICompatAnthropicDigestPromptCacheKey(account, 7, longChain, "prompt3", "")

	key, _ := svc.findOpenAICompatAnthropicDigestPromptCacheKey(account, 7, "chain1")
	require.Empty(t, key)
	key, matched := svc.findOpenAICompatAnthropicDigestPromptCacheKey(account, 7, longChain)
	require.Equal(t, "prompt3", key)
	require.Equal(t, longChain, matched)
	require.Equal(t, 2, svc.openaiCompatAnthropicDigestSessions.Len())
}

func TestOpenAICompatBoundedCacheConcurrentAccessStaysWithinCapacity(t *testing.T) {
	var cache openAICompatBoundedCache[openAICompatSessionResponseBinding]
	cache.maxEntries = 32
	const goroutines = 16
	const operations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		go func(worker int) {
			defer wg.Done()
			for operation := 0; operation < operations; operation++ {
				key := fmt.Sprintf("%d-%d", worker, operation)
				storeOpenAICompatCacheTestBinding(&cache, key, key, time.Now().Add(time.Hour))
				cache.Load(key)
			}
		}(worker)
	}
	wg.Wait()

	require.LessOrEqual(t, cache.Len(), cache.maxEntries)
}

func storeOpenAICompatCacheTestBinding(
	cache *openAICompatBoundedCache[openAICompatSessionResponseBinding],
	key string,
	responseID string,
	expiresAt time.Time,
) bool {
	binding := openAICompatSessionResponseBinding{ResponseID: responseID, ExpiresAt: expiresAt}
	return cache.Store(key, binding, binding.ExpiresAt, len(binding.ResponseID))
}
