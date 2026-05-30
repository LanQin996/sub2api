package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedeemServiceGenerateRandomCodeFitsRedeemCodeColumn(t *testing.T) {
	svc := &RedeemService{}

	code, err := svc.GenerateRandomCode()

	require.NoError(t, err)
	require.Len(t, code, 32)
	for _, ch := range code {
		require.Truef(t, (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F'), "unexpected code character %q in %q", ch, code)
	}
}

func TestRedeemServiceCanDistributeInvitationsHighSpender(t *testing.T) {
	ctx := context.Background()
	regularUser := &User{ID: 1, Role: RoleUser, Status: StatusActive}

	settingRepo := &redeemEligibilitySettingRepo{values: map[string]string{
		SettingKeyInvitationHighSpenderEnabled: "false",
	}}
	usageRepo := &redeemEligibilityUsageRepo{sum: 2000.01}
	svc := &RedeemService{}
	svc.SetInvitationEligibilityDeps(NewSettingService(settingRepo, nil), usageRepo)

	require.False(t, svc.CanDistributeInvitations(ctx, regularUser))

	settingRepo.values[SettingKeyInvitationHighSpenderEnabled] = "true"
	usageRepo.sum = 2000
	require.False(t, svc.CanDistributeInvitations(ctx, regularUser))

	usageRepo.sum = 2000.01
	require.True(t, svc.CanDistributeInvitations(ctx, regularUser))
}

func TestRedeemServiceCanDistributeInvitationsWhitelistAndAdmin(t *testing.T) {
	svc := &RedeemService{}

	require.True(t, svc.CanDistributeInvitations(context.Background(), &User{ID: 1, Role: RoleUser, InvitationEnabled: true}))
	require.True(t, svc.CanDistributeInvitations(context.Background(), &User{ID: 2, Role: RoleAdmin}))
	require.False(t, svc.CanDistributeInvitations(context.Background(), nil))
}

type redeemEligibilitySettingRepo struct {
	values map[string]string
}

func (r *redeemEligibilitySettingRepo) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *redeemEligibilitySettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r == nil {
		return "", ErrSettingNotFound
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *redeemEligibilitySettingRepo) Set(ctx context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *redeemEligibilitySettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

func (r *redeemEligibilitySettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *redeemEligibilitySettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *redeemEligibilitySettingRepo) Delete(ctx context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type redeemEligibilityUsageRepo struct {
	UsageLogRepository
	sum float64
	err error
}

func (r *redeemEligibilityUsageRepo) SumActualCostByUser(ctx context.Context, userID int64) (float64, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.sum, nil
}

var _ SettingRepository = (*redeemEligibilitySettingRepo)(nil)
var _ UsageLogRepository = (*redeemEligibilityUsageRepo)(nil)
var errRedeemEligibilityUsage = errors.New("usage sum failed")

func TestRedeemServiceCanDistributeInvitationsUsageError(t *testing.T) {
	settingRepo := &redeemEligibilitySettingRepo{values: map[string]string{
		SettingKeyInvitationHighSpenderEnabled: "true",
	}}
	usageRepo := &redeemEligibilityUsageRepo{err: errRedeemEligibilityUsage}
	svc := &RedeemService{}
	svc.SetInvitationEligibilityDeps(NewSettingService(settingRepo, nil), usageRepo)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.False(t, svc.CanDistributeInvitations(ctx, &User{ID: 1, Role: RoleUser}))
}
