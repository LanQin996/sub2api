package admin

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	snapshotCacheDefaultTTL       = 30 * time.Second
	snapshotCacheMaxEntries       = 256
	snapshotCacheCleanupBatchSize = 16
	snapshotCacheMaxKeyBytes      = 512
)

type snapshotCacheEntry struct {
	ETag      string
	Payload   any
	ExpiresAt time.Time
}

type snapshotCache struct {
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	items      map[string]snapshotCacheEntry
	order      list.List
	positions  map[string]*list.Element
	sf         singleflight.Group
}

type snapshotCacheLoadResult struct {
	Entry snapshotCacheEntry
	Hit   bool
}

func newSnapshotCache(ttl time.Duration) *snapshotCache {
	if ttl <= 0 {
		ttl = snapshotCacheDefaultTTL
	}
	return &snapshotCache{
		ttl:        ttl,
		maxEntries: snapshotCacheMaxEntries,
		items:      make(map[string]snapshotCacheEntry),
		positions:  make(map[string]*list.Element),
	}
}

func (c *snapshotCache) Get(key string) (snapshotCacheEntry, bool) {
	if c == nil || key == "" {
		return snapshotCacheEntry{}, false
	}
	key = normalizeSnapshotCacheKey(key)
	now := time.Now()

	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return snapshotCacheEntry{}, false
	}
	if now.After(entry.ExpiresAt) {
		c.mu.Lock()
		// Set may have refreshed the same key between the read and write locks.
		if current, exists := c.items[key]; exists && now.After(current.ExpiresAt) {
			c.removeLocked(key)
		} else if exists {
			entry = current
			ok = true
		}
		c.mu.Unlock()
		if !ok || now.After(entry.ExpiresAt) {
			return snapshotCacheEntry{}, false
		}
	}
	return entry, true
}

func (c *snapshotCache) Set(key string, payload any) snapshotCacheEntry {
	if c == nil {
		return snapshotCacheEntry{}
	}
	entry := snapshotCacheEntry{
		ETag:      buildETagFromAny(payload),
		Payload:   payload,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	if key == "" {
		return entry
	}
	key = strings.Clone(normalizeSnapshotCacheKey(key))
	c.mu.Lock()
	c.ensureInitializedLocked()
	c.removeExpiredLocked(time.Now(), snapshotCacheCleanupBatchSize)
	if position, exists := c.positions[key]; exists {
		c.items[key] = entry
		c.order.MoveToBack(position)
		c.mu.Unlock()
		return entry
	}
	for len(c.items) >= c.capacityLocked() {
		if !c.removeOldestLocked() {
			break
		}
	}
	c.items[key] = entry
	c.positions[key] = c.order.PushBack(key)
	c.mu.Unlock()
	return entry
}

func (c *snapshotCache) GetOrLoad(key string, load func() (any, error)) (snapshotCacheEntry, bool, error) {
	if load == nil {
		return snapshotCacheEntry{}, false, nil
	}
	if entry, ok := c.Get(key); ok {
		return entry, true, nil
	}
	if c == nil || key == "" {
		payload, err := load()
		if err != nil {
			return snapshotCacheEntry{}, false, err
		}
		return c.Set(key, payload), false, nil
	}

	singleflightKey := normalizeSnapshotCacheKey(key)
	value, err, _ := c.sf.Do(singleflightKey, func() (any, error) {
		if entry, ok := c.Get(key); ok {
			return snapshotCacheLoadResult{Entry: entry, Hit: true}, nil
		}
		payload, err := load()
		if err != nil {
			return nil, err
		}
		return snapshotCacheLoadResult{Entry: c.Set(key, payload), Hit: false}, nil
	})
	if err != nil {
		return snapshotCacheEntry{}, false, err
	}
	result, ok := value.(snapshotCacheLoadResult)
	if !ok {
		return snapshotCacheEntry{}, false, nil
	}
	return result.Entry, result.Hit, nil
}

func (c *snapshotCache) ensureInitializedLocked() {
	if c.items == nil {
		c.items = make(map[string]snapshotCacheEntry)
	}
	if c.positions == nil {
		c.positions = make(map[string]*list.Element)
	}
}

func (c *snapshotCache) capacityLocked() int {
	if c.maxEntries > 0 {
		return c.maxEntries
	}
	return snapshotCacheMaxEntries
}

func (c *snapshotCache) removeExpiredLocked(now time.Time, limit int) {
	// Each cache instance has a fixed TTL, and Set moves refreshed keys to the
	// back, so insertion order is also expiration order.
	for scanned := 0; scanned < limit; scanned++ {
		oldest := c.order.Front()
		if oldest == nil {
			return
		}
		key, _ := oldest.Value.(string)
		entry, ok := c.items[key]
		if ok && !now.After(entry.ExpiresAt) {
			return
		}
		c.removeLocked(key)
	}
}

func (c *snapshotCache) removeOldestLocked() bool {
	oldest := c.order.Front()
	if oldest == nil {
		return false
	}
	key, _ := oldest.Value.(string)
	c.removeLocked(key)
	return true
}

func (c *snapshotCache) removeLocked(key string) {
	delete(c.items, key)
	if position, ok := c.positions[key]; ok {
		c.order.Remove(position)
		delete(c.positions, key)
	}
}

func normalizeSnapshotCacheKey(key string) string {
	if len(key) <= snapshotCacheMaxKeyBytes {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func buildETagFromAny(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "\"" + hex.EncodeToString(sum[:]) + "\""
}

func parseBoolQueryWithDefault(raw string, def bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return def
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
