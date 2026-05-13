//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveRebateRatePercent_PerUserOverride verifies that per-inviter
// AffRebateRatePercent overrides the global rate, that NULL falls back to the
// global rate, and that out-of-range exclusive rates are clamped silently.
//
// SettingService is left nil here so globalRebateRatePercent returns the
// documented default (AffiliateRebateRateDefault = 20%) — this exercises the
// fallback path without spinning up a settings stub.
func TestResolveRebateRatePercent_PerUserOverride(t *testing.T) {
	t.Parallel()
	svc := &AffiliateService{}

	// nil exclusive rate → falls back to global default (20%)
	require.InDelta(t, AffiliateRebateRateDefault,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{}), 1e-9)

	// exclusive rate set → overrides global
	rate := 50.0
	require.InDelta(t, 50.0,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &rate}), 1e-9)

	// exclusive rate 0 → returns 0 (no rebate, intentional)
	zero := 0.0
	require.InDelta(t, 0.0,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &zero}), 1e-9)

	// exclusive rate above max → clamped to Max
	tooHigh := 250.0
	require.InDelta(t, AffiliateRebateRateMax,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &tooHigh}), 1e-9)

	// exclusive rate below min → clamped to Min
	tooLow := -5.0
	require.InDelta(t, AffiliateRebateRateMin,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &tooLow}), 1e-9)
}

// TestIsEnabled_NilSettingServiceReturnsDefault verifies that IsEnabled
// safely handles a nil settingService dependency by returning the default
// (off). This protects callers from nil-pointer crashes in misconfigured
// environments.
func TestIsEnabled_NilSettingServiceReturnsDefault(t *testing.T) {
	t.Parallel()
	svc := &AffiliateService{}
	require.False(t, svc.IsEnabled(context.Background()))
	require.Equal(t, AffiliateEnabledDefault, svc.IsEnabled(context.Background()))
}

func TestHasAffiliatePermission(t *testing.T) {
	t.Parallel()

	svc := &AffiliateService{repo: affiliatePermissionRepoStub{enabled: true}}
	allowed, err := svc.HasAffiliatePermission(context.Background(), 123)
	require.NoError(t, err)
	require.True(t, allowed)

	svc = &AffiliateService{repo: affiliatePermissionRepoStub{enabled: false}}
	allowed, err = svc.HasAffiliatePermission(context.Background(), 123)
	require.NoError(t, err)
	require.False(t, allowed)
}

// TestValidateExclusiveRate_BoundaryAndInvalid covers the validator used by
// admin-facing rate setters: nil is always valid (clear), in-range values
// are accepted, NaN/Inf and out-of-range values produce a typed BadRequest.
func TestValidateExclusiveRate_BoundaryAndInvalid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateExclusiveRate(nil))

	for _, v := range []float64{0, 0.01, 50, 99.99, 100} {
		v := v
		require.NoError(t, validateExclusiveRate(&v), "value %v should be valid", v)
	}

	for _, v := range []float64{-0.01, 100.01, -100, 200} {
		v := v
		require.Error(t, validateExclusiveRate(&v), "value %v should be rejected", v)
	}

	nan := math.NaN()
	require.Error(t, validateExclusiveRate(&nan))
	posInf := math.Inf(1)
	require.Error(t, validateExclusiveRate(&posInf))
	negInf := math.Inf(-1)
	require.Error(t, validateExclusiveRate(&negInf))
}

type affiliatePermissionRepoStub struct {
	enabled bool
}

func (r affiliatePermissionRepoStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	return &AffiliateSummary{UserID: userID, AffCode: "SELF", AffEnabled: r.enabled}, nil
}

func (r affiliatePermissionRepoStub) GetAffiliateByCode(context.Context, string) (*AffiliateSummary, error) {
	return nil, ErrAffiliateProfileNotFound
}

func (r affiliatePermissionRepoStub) BindInviter(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r affiliatePermissionRepoStub) AccrueQuota(context.Context, int64, int64, float64, int, *int64) (bool, error) {
	return false, nil
}

func (r affiliatePermissionRepoStub) GetAccruedRebateFromInvitee(context.Context, int64, int64) (float64, error) {
	return 0, nil
}

func (r affiliatePermissionRepoStub) ThawFrozenQuota(context.Context, int64) (float64, error) {
	return 0, nil
}

func (r affiliatePermissionRepoStub) TransferQuotaToBalance(context.Context, int64) (float64, float64, error) {
	return 0, 0, nil
}

func (r affiliatePermissionRepoStub) ListInvitees(context.Context, int64, int) ([]AffiliateInvitee, error) {
	return nil, nil
}

func (r affiliatePermissionRepoStub) UpdateUserAffCode(context.Context, int64, string) error {
	return nil
}

func (r affiliatePermissionRepoStub) ResetUserAffCode(context.Context, int64) (string, error) {
	return "", nil
}

func (r affiliatePermissionRepoStub) SetUserAffiliateEnabled(context.Context, int64, bool) error {
	return nil
}

func (r affiliatePermissionRepoStub) BatchSetUserAffiliateEnabled(context.Context, []int64, bool) error {
	return nil
}

func (r affiliatePermissionRepoStub) SetUserRebateRate(context.Context, int64, *float64) error {
	return nil
}

func (r affiliatePermissionRepoStub) BatchSetUserRebateRate(context.Context, []int64, *float64) error {
	return nil
}

func (r affiliatePermissionRepoStub) ListUsersWithCustomSettings(context.Context, AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error) {
	return nil, 0, nil
}

func (r affiliatePermissionRepoStub) ListAffiliateInviteRecords(context.Context, AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error) {
	return nil, 0, nil
}

func (r affiliatePermissionRepoStub) ListAffiliateRebateRecords(context.Context, AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error) {
	return nil, 0, nil
}

func (r affiliatePermissionRepoStub) ListAffiliateTransferRecords(context.Context, AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error) {
	return nil, 0, nil
}

func (r affiliatePermissionRepoStub) GetAffiliateUserOverview(context.Context, int64) (*AffiliateUserOverview, error) {
	return nil, nil
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a***@g***.com", maskEmail("alice@gmail.com"))
	require.Equal(t, "x***@d***", maskEmail("x@domain"))
	require.Equal(t, "", maskEmail(""))
}

func TestIsValidAffiliateCodeFormat(t *testing.T) {
	t.Parallel()

	// 邀请码格式校验同时服务于：
	// 1) 系统自动生成的 12 位随机码（A-Z 去 I/O，2-9 去 0/1）
	// 2) 管理员设置的自定义专属码（如 "VIP2026"、"NEW_USER-1"）
	// 因此校验放宽到 [A-Z0-9_-]{4,32}（要求调用方先 ToUpper）。
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"valid canonical 12-char", "ABCDEFGHJKLM", true},
		{"valid all digits 2-9", "234567892345", true},
		{"valid mixed", "A2B3C4D5E6F7", true},
		{"valid admin custom short", "VIP1", true},
		{"valid admin custom with hyphen", "NEW-USER", true},
		{"valid admin custom with underscore", "VIP_2026", true},
		{"valid 32-char max", "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345", true},
		// Previously-excluded chars (I/O/0/1) are now allowed since admins may use them.
		{"letter I now allowed", "IBCDEFGHJKLM", true},
		{"letter O now allowed", "OBCDEFGHJKLM", true},
		{"digit 0 now allowed", "0BCDEFGHJKLM", true},
		{"digit 1 now allowed", "1BCDEFGHJKLM", true},
		{"too short (3 chars)", "ABC", false},
		{"too long (33 chars)", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456", false},
		{"lowercase rejected (caller must ToUpper first)", "abcdefghjklm", false},
		{"empty", "", false},
		{"utf8 non-ascii", "ÄÄÄÄÄÄ", false}, // bytes out of charset
		{"ascii punctuation .", "ABCDEFGHJK.M", false},
		{"whitespace", "ABCDEFGHJK M", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isValidAffiliateCodeFormat(tc.in))
		})
	}
}
