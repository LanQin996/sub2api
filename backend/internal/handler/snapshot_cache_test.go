//go:build unit

package handler

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotCacheSetEvictsOldestAtCapacity(t *testing.T) {
	c := newSnapshotCache(5 * time.Second)
	c.maxEntries = 2

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")

	_, ok := c.Get("key1")
	require.False(t, ok)
	_, ok = c.Get("key2")
	require.True(t, ok)
	_, ok = c.Get("key3")
	require.True(t, ok)
	require.Len(t, c.items, 2)
}

func TestSnapshotCacheRefreshMovesEntryBehindOlderExpirations(t *testing.T) {
	c := newSnapshotCache(5 * time.Second)
	c.maxEntries = 2

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key1", "refreshed")
	c.Set("key3", "value3")

	entry, ok := c.Get("key1")
	require.True(t, ok)
	require.Equal(t, "refreshed", entry.Payload)
	_, ok = c.Get("key2")
	require.False(t, ok)
	_, ok = c.Get("key3")
	require.True(t, ok)
}

func TestSnapshotCacheSetActivelyRemovesExpiredEntries(t *testing.T) {
	c := newSnapshotCache(time.Millisecond)
	c.maxEntries = snapshotCacheCleanupBatchSize + 2
	for i := 0; i < snapshotCacheCleanupBatchSize; i++ {
		c.Set(fmt.Sprintf("expired-%d", i), i)
	}
	time.Sleep(5 * time.Millisecond)

	c.Set("fresh", "value")

	require.Len(t, c.items, 1)
	_, ok := c.Get("fresh")
	require.True(t, ok)
}

func TestSnapshotCacheLongKeyIsStoredInBoundedForm(t *testing.T) {
	c := newSnapshotCache(5 * time.Second)
	key := strings.Repeat("x", snapshotCacheMaxKeyBytes+1)

	c.Set(key, "value")

	_, ok := c.Get(key)
	require.True(t, ok)
	require.Len(t, c.items, 1)
	for storedKey := range c.items {
		require.LessOrEqual(t, len(storedKey), len("sha256:")+sha256.Size*2)
	}
}
