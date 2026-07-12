//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledTestRunnerRepoStub struct {
	claim func(context.Context, time.Time, time.Time, int) ([]*ScheduledTestPlan, error)
}

func (s *scheduledTestRunnerRepoStub) Create(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return nil, nil
}

func (s *scheduledTestRunnerRepoStub) GetByID(context.Context, int64) (*ScheduledTestPlan, error) {
	return nil, nil
}

func (s *scheduledTestRunnerRepoStub) ListByAccountID(context.Context, int64) ([]*ScheduledTestPlan, error) {
	return nil, nil
}

func (s *scheduledTestRunnerRepoStub) ClaimDue(ctx context.Context, now time.Time, leaseUntil time.Time, limit int) ([]*ScheduledTestPlan, error) {
	if s.claim == nil {
		return nil, nil
	}
	return s.claim(ctx, now, leaseUntil, limit)
}

func (s *scheduledTestRunnerRepoStub) Update(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return nil, nil
}

func (s *scheduledTestRunnerRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (s *scheduledTestRunnerRepoStub) CompleteClaim(context.Context, int64, time.Time, time.Time, time.Time) (bool, error) {
	return true, nil
}

func TestScheduledTestRunnerSkipsOverlappingRun(t *testing.T) {
	started := make(chan struct{})
	var startOnce sync.Once
	var claims atomic.Int64
	repo := &scheduledTestRunnerRepoStub{
		claim: func(ctx context.Context, _ time.Time, _ time.Time, _ int) ([]*ScheduledTestPlan, error) {
			claims.Add(1)
			startOnce.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	runner := NewScheduledTestRunnerService(repo, nil, nil, nil, nil)
	runner.startDelay = 0
	t.Cleanup(runner.Stop)

	firstDone := make(chan struct{})
	go func() {
		runner.runScheduled()
		close(firstDone)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first run did not reach ClaimDue")
	}

	secondDone := make(chan struct{})
	go func() {
		runner.runScheduled()
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("overlapping run was not skipped")
	}
	require.EqualValues(t, 1, claims.Load())

	runner.Stop()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("Stop returned before the active run exited")
	}
}

func TestScheduledTestRunnerStopCancelsStartDelayAndWaits(t *testing.T) {
	var claims atomic.Int64
	repo := &scheduledTestRunnerRepoStub{
		claim: func(context.Context, time.Time, time.Time, int) ([]*ScheduledTestPlan, error) {
			claims.Add(1)
			return nil, nil
		},
	}
	runner := NewScheduledTestRunnerService(repo, nil, nil, nil, nil)
	runner.startDelay = time.Hour
	t.Cleanup(runner.Stop)

	done := make(chan struct{})
	go func() {
		runner.runScheduled()
		close(done)
	}()
	require.Eventually(t, func() bool {
		runner.lifecycleMu.Lock()
		defer runner.lifecycleMu.Unlock()
		return runner.running
	}, time.Second, time.Millisecond)

	startedAt := time.Now()
	runner.Stop()
	require.Less(t, time.Since(startedAt), time.Second)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("delayed run remained alive after Stop")
	}
	require.Zero(t, claims.Load())
}

func TestScheduledTestRunnerClaimsBoundedBatchWithLease(t *testing.T) {
	var claimedAt time.Time
	var leaseUntil time.Time
	var limit int
	repo := &scheduledTestRunnerRepoStub{
		claim: func(_ context.Context, now time.Time, lease time.Time, batchLimit int) ([]*ScheduledTestPlan, error) {
			claimedAt = now
			leaseUntil = lease
			limit = batchLimit
			return nil, nil
		},
	}
	runner := NewScheduledTestRunnerService(repo, nil, nil, nil, nil)
	runner.startDelay = 0
	t.Cleanup(runner.Stop)

	runner.runScheduled()

	require.False(t, claimedAt.IsZero())
	require.Equal(t, scheduledTestDefaultMaxWorkers, limit)
	require.Equal(t, scheduledTestClaimLease, leaseUntil.Sub(claimedAt))
}
