package service

import (
	"context"
	"log/slog"
	"math"
	"time"
)

const autoConcurrencyUpgradeTimeout = 5 * time.Second

type AutoConcurrencyUpgradeSettings struct {
	Enabled         bool
	SpendThreshold  float64
	Step            int
	MaxConcurrency  int
	BaseConcurrency int
}

type userConcurrencySetIfLowerRepository interface {
	SetConcurrencyIfLower(ctx context.Context, userID int64, target int) (bool, error)
}

type AutoConcurrencyUpgradeService struct {
	userRepo             UserRepository
	usageRepo            UsageLogRepository
	settingService       *SettingService
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

func NewAutoConcurrencyUpgradeService(
	userRepo UserRepository,
	usageRepo UsageLogRepository,
	settingService *SettingService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
) *AutoConcurrencyUpgradeService {
	return &AutoConcurrencyUpgradeService{
		userRepo:             userRepo,
		usageRepo:            usageRepo,
		settingService:       settingService,
		authCacheInvalidator: authCacheInvalidator,
	}
}

func (s *AutoConcurrencyUpgradeService) CheckAndUpgradeAfterUsage(ctx context.Context, userID int64) {
	if s == nil || s.userRepo == nil || s.usageRepo == nil || s.settingService == nil || userID <= 0 {
		return
	}

	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	workCtx, cancel := context.WithTimeout(base, autoConcurrencyUpgradeTimeout)
	defer cancel()

	settings, err := s.settingService.GetAutoConcurrencyUpgradeSettings(workCtx)
	if err != nil {
		slog.Warn("auto concurrency upgrade: load settings failed", "user_id", userID, "error", err)
		return
	}
	if !settings.Enabled {
		return
	}

	totalSpend, err := s.usageRepo.SumActualCostByUser(workCtx, userID)
	if err != nil {
		slog.Warn("auto concurrency upgrade: sum user spend failed", "user_id", userID, "error", err)
		return
	}
	target, ok := s.TargetConcurrency(settings, totalSpend)
	if !ok {
		return
	}

	updated, err := s.setUserConcurrencyIfLower(workCtx, userID, target)
	if err != nil {
		slog.Warn("auto concurrency upgrade: set user concurrency failed", "user_id", userID, "target", target, "error", err)
		return
	}
	if updated && s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(workCtx, userID)
	}
}

func (s *AutoConcurrencyUpgradeService) TargetConcurrency(settings AutoConcurrencyUpgradeSettings, totalSpend float64) (int, bool) {
	baseConcurrency := settings.BaseConcurrency
	settings = normalizeAutoConcurrencyUpgradeSettings(settings.Enabled, settings.SpendThreshold, settings.Step, settings.MaxConcurrency)
	settings.BaseConcurrency = baseConcurrency
	if !settings.Enabled || totalSpend < settings.SpendThreshold {
		return 0, false
	}
	if settings.BaseConcurrency < 0 {
		settings.BaseConcurrency = 0
	}
	levels := int(math.Floor(totalSpend / settings.SpendThreshold))
	if levels <= 0 {
		return 0, false
	}
	target := settings.BaseConcurrency + levels*settings.Step
	if target > settings.MaxConcurrency {
		target = settings.MaxConcurrency
	}
	if target <= 0 {
		return 0, false
	}
	return target, true
}

func (s *AutoConcurrencyUpgradeService) setUserConcurrencyIfLower(ctx context.Context, userID int64, target int) (bool, error) {
	if setter, ok := s.userRepo.(userConcurrencySetIfLowerRepository); ok {
		return setter.SetConcurrencyIfLower(ctx, userID, target)
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user.Concurrency >= target {
		return false, nil
	}
	return true, s.userRepo.UpdateConcurrency(ctx, userID, target-user.Concurrency)
}
