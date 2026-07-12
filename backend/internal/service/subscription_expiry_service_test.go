package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type subscriptionExpiryRepoStub struct {
	listCalls         int
	reminderQueries   []subscriptionExpiryReminderQuery
	reminderListFn    func(startsAt, endsAt, afterExpiresAt time.Time, afterID int64, limit int) ([]UserSubscription, error)
	batchUpdateCalls  int
	batchUpdateCallCh chan struct{}
	batchUpdateFn     func(context.Context) (int64, error)
}

type subscriptionExpiryReminderQuery struct {
	startsAt       time.Time
	endsAt         time.Time
	afterExpiresAt time.Time
	afterID        int64
	limit          int
}

func (r *subscriptionExpiryRepoStub) Create(context.Context, *UserSubscription) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) GetByID(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) Update(context.Context, *UserSubscription) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) Restore(context.Context, int64, string) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (r *subscriptionExpiryRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (r *subscriptionExpiryRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *subscriptionExpiryRepoStub) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	r.listCalls++
	return nil, &pagination.PaginationResult{Page: 1, Pages: 1}, nil
}

func (r *subscriptionExpiryRepoStub) ListActiveExpiringBetween(_ context.Context, startsAt, endsAt, afterExpiresAt time.Time, afterID int64, limit int) ([]UserSubscription, error) {
	r.reminderQueries = append(r.reminderQueries, subscriptionExpiryReminderQuery{
		startsAt:       startsAt,
		endsAt:         endsAt,
		afterExpiresAt: afterExpiresAt,
		afterID:        afterID,
		limit:          limit,
	})
	if r.reminderListFn != nil {
		return r.reminderListFn(startsAt, endsAt, afterExpiresAt, afterID, limit)
	}
	return nil, nil
}

func (r *subscriptionExpiryRepoStub) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *subscriptionExpiryRepoStub) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *subscriptionExpiryRepoStub) ExtendExpiry(context.Context, int64, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) UpdateStatus(context.Context, int64, string) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) UpdateNotes(context.Context, int64, string) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ActivateWindows(context.Context, int64, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetWeeklyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetMonthlyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) IncrementUsage(context.Context, int64, float64) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {
	r.batchUpdateCalls++
	if r.batchUpdateCallCh != nil {
		select {
		case r.batchUpdateCallCh <- struct{}{}:
		default:
		}
	}
	if r.batchUpdateFn != nil {
		return r.batchUpdateFn(ctx)
	}
	return 0, nil
}

type subscriptionExpirySettingRepoStub struct {
	values map[string]string
	err    error
}

func (r *subscriptionExpirySettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *subscriptionExpirySettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *subscriptionExpirySettingRepoStub) Set(context.Context, string, string) error {
	return nil
}

func (r *subscriptionExpirySettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}

func (r *subscriptionExpirySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (r *subscriptionExpirySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}

func (r *subscriptionExpirySettingRepoStub) Delete(context.Context, string) error {
	return nil
}

func TestSubscriptionExpiryService_ExpiryReminderEnabledDefaultsToTrue(t *testing.T) {
	svc := NewSubscriptionExpiryService(nil, time.Minute)
	svc.SetSettingRepository(&subscriptionExpirySettingRepoStub{values: map[string]string{}})

	require.True(t, svc.expiryReminderEnabled(context.Background()))
}

func TestSubscriptionExpiryService_ExpiryReminderDisabledSkipsSubscriptionScan(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{}
	settingRepo := &subscriptionExpirySettingRepoStub{
		values: map[string]string{SettingKeySubscriptionExpiryNotifyEnabled: "false"},
	}
	svc := NewSubscriptionExpiryService(repo, time.Minute)
	svc.SetSettingRepository(settingRepo)
	svc.SetNotificationEmailService(NewNotificationEmailService(settingRepo, nil))

	svc.sendExpiryReminders(context.Background())

	require.Zero(t, repo.listCalls)
	require.Empty(t, repo.reminderQueries)
}

func TestSubscriptionExpiryService_ExpiryReminderSettingReadErrorFailsClosed(t *testing.T) {
	svc := NewSubscriptionExpiryService(nil, time.Minute)
	svc.SetSettingRepository(&subscriptionExpirySettingRepoStub{err: errors.New("db down")})

	require.False(t, svc.expiryReminderEnabled(context.Background()))
}

func TestSubscriptionExpiryService_WindowedReminderUsesStableCursorWithoutGenericList(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)
	sevenDayExpiry := fixedNow.Add(7*24*time.Hour + 12*time.Hour)
	repo := &subscriptionExpiryRepoStub{}
	repo.reminderListFn = func(startsAt, _ time.Time, afterExpiresAt time.Time, afterID int64, _ int) ([]UserSubscription, error) {
		if !startsAt.Equal(fixedNow.Add(7 * 24 * time.Hour)) {
			return nil, nil
		}
		if afterExpiresAt.IsZero() {
			subs := make([]UserSubscription, subscriptionExpiryReminderPageSize)
			for i := range subs {
				subs[i] = UserSubscription{ID: int64(i + 1), ExpiresAt: sevenDayExpiry}
			}
			return subs, nil
		}
		if afterExpiresAt.Equal(sevenDayExpiry) && afterID == subscriptionExpiryReminderPageSize {
			return []UserSubscription{{ID: 201, ExpiresAt: sevenDayExpiry}}, nil
		}
		return nil, nil
	}

	svc := NewSubscriptionExpiryService(repo, time.Minute)
	svc.sendWindowedExpiryReminders(context.Background(), fixedNow)

	require.Zero(t, repo.listCalls, "optimized reminder scan must not use the admin List path")
	require.Len(t, repo.reminderQueries, 4)
	require.Equal(t, fixedNow.Add(7*24*time.Hour), repo.reminderQueries[0].startsAt)
	require.Equal(t, fixedNow.Add(8*24*time.Hour), repo.reminderQueries[0].endsAt)
	require.True(t, repo.reminderQueries[0].afterExpiresAt.IsZero())
	require.Zero(t, repo.reminderQueries[0].afterID)
	require.Equal(t, sevenDayExpiry, repo.reminderQueries[1].afterExpiresAt)
	require.EqualValues(t, subscriptionExpiryReminderPageSize, repo.reminderQueries[1].afterID)
	require.Equal(t, fixedNow.Add(3*24*time.Hour), repo.reminderQueries[2].startsAt)
	require.Equal(t, fixedNow.Add(1*24*time.Hour), repo.reminderQueries[3].startsAt)
}

func TestSubscriptionExpiryService_ExpiryMaintenanceDoesNotRescanRemindersEveryCycle(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{batchUpdateCallCh: make(chan struct{}, 16)}
	settingRepo := &subscriptionExpirySettingRepoStub{values: map[string]string{}}
	svc := NewSubscriptionExpiryService(repo, 5*time.Millisecond)
	svc.SetSettingRepository(settingRepo)
	svc.SetNotificationEmailService(NewNotificationEmailService(settingRepo, nil))
	svc.Start()

	for i := 0; i < 3; i++ {
		select {
		case <-repo.batchUpdateCallCh:
		case <-time.After(time.Second):
			svc.Stop()
			t.Fatal("timed out waiting for expiry maintenance cycle")
		}
	}
	svc.Stop()

	require.GreaterOrEqual(t, repo.batchUpdateCalls, 3)
	require.Len(t, repo.reminderQueries, 3, "startup scan should query the three reminder windows only once")
	require.Zero(t, repo.listCalls)
}

func TestSubscriptionExpiryService_StartIsIdempotent(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	repo := &subscriptionExpiryRepoStub{}
	repo.batchUpdateFn = func(ctx context.Context) (int64, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return 0, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	svc := NewSubscriptionExpiryService(repo, time.Hour)

	svc.Start()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		svc.Stop()
		t.Fatal("timed out waiting for initial maintenance run")
	}
	svc.Start()

	duplicateStart := false
	select {
	case <-entered:
		duplicateStart = true
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	svc.Stop()

	require.False(t, duplicateStart, "a second Start call must not launch another scheduler")
	require.Equal(t, 1, repo.batchUpdateCalls)
}
