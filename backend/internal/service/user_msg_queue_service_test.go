//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type cleanupWorkerUserMsgQueueCache struct {
	reconcileCalls atomic.Int64
	maxCount       atomic.Int64
}

var _ UserMsgQueueCache = (*cleanupWorkerUserMsgQueueCache)(nil)

func (c *cleanupWorkerUserMsgQueueCache) AcquireLock(context.Context, int64, string, int) (bool, error) {
	return true, nil
}

func (c *cleanupWorkerUserMsgQueueCache) ReleaseLock(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (c *cleanupWorkerUserMsgQueueCache) GetLastCompletedMs(context.Context, int64) (int64, error) {
	return 0, nil
}

func (c *cleanupWorkerUserMsgQueueCache) GetCurrentTimeMs(context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

func (c *cleanupWorkerUserMsgQueueCache) ReconcileExpiredLockCandidates(_ context.Context, maxCount int) (int, error) {
	c.reconcileCalls.Add(1)
	c.maxCount.Store(int64(maxCount))
	return 1, nil
}

func TestStartCleanupWorker_ReconcilesExpiredLockCandidates(t *testing.T) {
	cache := &cleanupWorkerUserMsgQueueCache{}
	svc := NewUserMessageQueueService(cache, nil, nil)
	defer svc.Stop()

	svc.StartCleanupWorker(time.Millisecond)

	require.Eventually(t, func() bool {
		return cache.reconcileCalls.Load() > 0
	}, time.Second, 10*time.Millisecond)
	require.EqualValues(t, 1000, cache.maxCount.Load())
}

func TestProvideUserMessageQueueService_DisabledDoesNotStartCleanupWorker(t *testing.T) {
	cache := &cleanupWorkerUserMsgQueueCache{}
	cfg := &config.Config{}
	cfg.Gateway.UserMessageQueue.Enabled = false
	cfg.Gateway.UserMessageQueue.CleanupIntervalSeconds = 1

	svc := ProvideUserMessageQueueService(cache, nil, cfg)
	defer svc.Stop()

	svc.workerMu.Lock()
	workerStarted := svc.workerCancel != nil
	svc.workerMu.Unlock()
	require.False(t, workerStarted)
}

type blockingUserMsgQueueCache struct {
	cleanupWorkerUserMsgQueueCache
	started  chan struct{}
	canceled atomic.Bool
	once     sync.Once
}

func (c *blockingUserMsgQueueCache) ReconcileExpiredLockCandidates(ctx context.Context, _ int) (int, error) {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	c.canceled.Store(true)
	return 0, ctx.Err()
}

func TestUserMessageQueueService_StopCancelsAndWaitsForCleanup(t *testing.T) {
	cache := &blockingUserMsgQueueCache{started: make(chan struct{})}
	svc := NewUserMessageQueueService(cache, nil, nil)
	svc.StartCleanupWorker(time.Millisecond)

	select {
	case <-cache.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not start")
	}

	svc.Stop()
	require.True(t, cache.canceled.Load())
	require.NotPanics(t, svc.Stop)
}
