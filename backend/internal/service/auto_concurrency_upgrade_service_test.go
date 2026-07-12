//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAutoConcurrencyUpgradeServiceTargetConcurrency(t *testing.T) {
	svc := &AutoConcurrencyUpgradeService{}
	settings := AutoConcurrencyUpgradeSettings{
		Enabled:         true,
		SpendThreshold:  10,
		Step:            2,
		MaxConcurrency:  20,
		BaseConcurrency: 5,
	}

	target, ok := svc.TargetConcurrency(settings, 9.99)
	require.False(t, ok)
	require.Zero(t, target)

	target, ok = svc.TargetConcurrency(settings, 10)
	require.True(t, ok)
	require.Equal(t, 7, target)

	target, ok = svc.TargetConcurrency(settings, 80)
	require.True(t, ok)
	require.Equal(t, 20, target)
}

func TestAutoConcurrencyUpgradeServiceDoesNotLowerManualConcurrency(t *testing.T) {
	repo := &autoConcurrencyUpgradeUserRepoStub{user: &User{ID: 42, Concurrency: 50}}
	svc := &AutoConcurrencyUpgradeService{userRepo: repo}

	updated, err := svc.setUserConcurrencyIfLower(context.Background(), 42, 20)

	require.NoError(t, err)
	require.False(t, updated)
	require.Zero(t, repo.updateDelta)
}

func TestAutoConcurrencyUpgradeServiceRaisesLowerConcurrencyFallback(t *testing.T) {
	repo := &autoConcurrencyUpgradeUserRepoStub{user: &User{ID: 42, Concurrency: 5}}
	svc := &AutoConcurrencyUpgradeService{userRepo: repo}

	updated, err := svc.setUserConcurrencyIfLower(context.Background(), 42, 9)

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, 4, repo.updateDelta)
	require.Equal(t, 9, repo.user.Concurrency)
}

func TestAutoConcurrencyUpgradeServiceScheduleCooldown(t *testing.T) {
	svc := NewAutoConcurrencyUpgradeService(nil, nil, nil, nil)
	now := time.Unix(100, 0)

	require.True(t, svc.shouldSchedule(42, now))
	require.False(t, svc.shouldSchedule(42, now.Add(autoConcurrencyUpgradeCheckInterval-time.Second)))
	require.True(t, svc.shouldSchedule(42, now.Add(autoConcurrencyUpgradeCheckInterval)))
	require.True(t, svc.shouldSchedule(43, now))
}

func TestAutoConcurrencyUpgradeServiceDisabledDoesNotTrackOrSpawn(t *testing.T) {
	settingRepo := &autoConcurrencyUpgradeSettingRepoStub{values: map[string]string{
		SettingKeyAutoConcurrencyUpgradeEnabled:        "false",
		SettingKeyAutoConcurrencyUpgradeSpendThreshold: "10",
		SettingKeyAutoConcurrencyUpgradeStep:           "2",
		SettingKeyAutoConcurrencyUpgradeMax:            "20",
	}}
	usageRepo := &autoConcurrencyUpgradeUsageRepoStub{sum: 100}
	userRepo := &autoConcurrencyUpgradeUserRepoStub{user: &User{ID: 42, Concurrency: 5}}
	svc := NewAutoConcurrencyUpgradeService(userRepo, usageRepo, NewSettingService(settingRepo, &config.Config{}), nil)

	svc.ScheduleCheckAfterUsageForUser(context.Background(), userRepo.user)

	svc.mu.Lock()
	require.Empty(t, svc.nextCheckAtByUser)
	require.Empty(t, svc.inFlightByUser)
	svc.mu.Unlock()
	require.Zero(t, usageRepo.sumCalls)
	require.Equal(t, 1, settingRepo.getMultipleCalls)
}

func TestAutoConcurrencyUpgradeServiceKnownMaxDoesNotTrackOrSpawn(t *testing.T) {
	settingRepo := &autoConcurrencyUpgradeSettingRepoStub{values: map[string]string{
		SettingKeyAutoConcurrencyUpgradeEnabled:        "true",
		SettingKeyAutoConcurrencyUpgradeSpendThreshold: "10",
		SettingKeyAutoConcurrencyUpgradeStep:           "2",
		SettingKeyAutoConcurrencyUpgradeMax:            "20",
	}}
	usageRepo := &autoConcurrencyUpgradeUsageRepoStub{sum: 100}
	userRepo := &autoConcurrencyUpgradeUserRepoStub{user: &User{ID: 42, Concurrency: 20}}
	svc := NewAutoConcurrencyUpgradeService(userRepo, usageRepo, NewSettingService(settingRepo, &config.Config{}), nil)

	svc.ScheduleCheckAfterUsageForUser(context.Background(), userRepo.user)

	svc.mu.Lock()
	require.Empty(t, svc.nextCheckAtByUser)
	require.Empty(t, svc.inFlightByUser)
	svc.mu.Unlock()
	require.Zero(t, usageRepo.sumCalls)
}

func TestAutoConcurrencyUpgradeServiceSkipsSpendSumWhenKnownConcurrencyAtMax(t *testing.T) {
	settingRepo := &autoConcurrencyUpgradeSettingRepoStub{values: map[string]string{
		SettingKeyAutoConcurrencyUpgradeEnabled:        "true",
		SettingKeyAutoConcurrencyUpgradeSpendThreshold: "10",
		SettingKeyAutoConcurrencyUpgradeStep:           "2",
		SettingKeyAutoConcurrencyUpgradeMax:            "20",
		SettingKeyDefaultConcurrency:                   "5",
	}}
	usageRepo := &autoConcurrencyUpgradeUsageRepoStub{sum: 100}
	userRepo := &autoConcurrencyUpgradeUserRepoStub{user: &User{ID: 42, Concurrency: 50}}
	svc := NewAutoConcurrencyUpgradeService(userRepo, usageRepo, NewSettingService(settingRepo, &config.Config{}), nil)

	svc.checkAndUpgradeAfterUsage(context.Background(), 42, 50)

	require.Zero(t, usageRepo.sumCalls)
	require.Zero(t, repoUpdateDelta(userRepo))
}

func TestSettingServiceAutoConcurrencyUpgradeSettingsCached(t *testing.T) {
	settingRepo := &autoConcurrencyUpgradeSettingRepoStub{values: map[string]string{
		SettingKeyAutoConcurrencyUpgradeEnabled:        "true",
		SettingKeyAutoConcurrencyUpgradeSpendThreshold: "10",
		SettingKeyAutoConcurrencyUpgradeStep:           "2",
		SettingKeyAutoConcurrencyUpgradeMax:            "20",
		SettingKeyDefaultConcurrency:                   "5",
	}}
	svc := NewSettingService(settingRepo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 3}})

	first, err := svc.GetAutoConcurrencyUpgradeSettings(context.Background())
	require.NoError(t, err)
	second, err := svc.GetAutoConcurrencyUpgradeSettings(context.Background())
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.True(t, first.Enabled)
	require.Equal(t, 5, first.BaseConcurrency)
	require.Equal(t, 1, settingRepo.getMultipleCalls)
}

type autoConcurrencyUpgradeUserRepoStub struct {
	UserRepository
	user        *User
	updateDelta int
}

func (r *autoConcurrencyUpgradeUserRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	if r.user == nil || r.user.ID != id {
		return nil, ErrUserNotFound
	}
	return r.user, nil
}

func (r *autoConcurrencyUpgradeUserRepoStub) UpdateConcurrency(ctx context.Context, id int64, amount int) error {
	if r.user == nil || r.user.ID != id {
		return ErrUserNotFound
	}
	r.updateDelta = amount
	r.user.Concurrency += amount
	return nil
}

func repoUpdateDelta(repo *autoConcurrencyUpgradeUserRepoStub) int {
	if repo == nil {
		return 0
	}
	return repo.updateDelta
}

type autoConcurrencyUpgradeUsageRepoStub struct {
	UsageLogRepository
	sum      float64
	sumCalls int
}

func (r *autoConcurrencyUpgradeUsageRepoStub) SumActualCostByUser(ctx context.Context, userID int64) (float64, error) {
	r.sumCalls++
	return r.sum, nil
}

type autoConcurrencyUpgradeSettingRepoStub struct {
	values           map[string]string
	getMultipleCalls int
}

func (r *autoConcurrencyUpgradeSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if r == nil || r.values == nil {
		return "", ErrSettingNotFound
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.getMultipleCalls++
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *autoConcurrencyUpgradeSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(r.values, key)
	return nil
}
