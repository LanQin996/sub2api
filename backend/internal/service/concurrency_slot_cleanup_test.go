package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type slotCleanupCache struct {
	ConcurrencyCache
	calls atomic.Int64
}

func (c *slotCleanupCache) CleanupExpiredAccountSlotKeys(context.Context) error {
	c.calls.Add(1)
	return nil
}

func TestStartSlotCleanupWorker_UsesCacheWideCleanupWithoutAccountRepo(t *testing.T) {
	cache := &slotCleanupCache{}
	svc := NewConcurrencyService(cache)
	defer svc.Stop()

	svc.StartSlotCleanupWorker(nil, time.Hour)

	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if cache.calls.Load() > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("cleanup worker did not call cache-wide account slot cleanup")
		case <-ticker.C:
		}
	}
}

type blockingSlotCleanupCache struct {
	ConcurrencyCache
	started  chan struct{}
	canceled atomic.Bool
	once     sync.Once
}

func (c *blockingSlotCleanupCache) CleanupExpiredAccountSlotKeys(ctx context.Context) error {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	c.canceled.Store(true)
	return ctx.Err()
}

func TestConcurrencyService_StopCancelsAndWaitsForSlotCleanup(t *testing.T) {
	cache := &blockingSlotCleanupCache{started: make(chan struct{})}
	svc := NewConcurrencyService(cache)
	svc.StartSlotCleanupWorker(nil, time.Hour)

	select {
	case <-cache.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not start")
	}

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the active cleanup call")
	}
	if !cache.canceled.Load() {
		t.Fatal("cleanup context was not canceled")
	}

	// Stop is intentionally idempotent because cleanup may be invoked defensively.
	svc.Stop()
}
