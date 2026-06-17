package service

import (
	"context"
	"testing"

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
