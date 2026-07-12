package service

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

const (
	autoConcurrencyUpgradeTimeout       = 5 * time.Second
	autoConcurrencyUpgradeCheckInterval = 10 * time.Minute
)

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
	mu                   sync.Mutex
	nextCheckAtByUser    map[int64]time.Time
	inFlightByUser       map[int64]struct{}
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
		nextCheckAtByUser:    make(map[int64]time.Time),
		inFlightByUser:       make(map[int64]struct{}),
	}
}

func (s *AutoConcurrencyUpgradeService) ScheduleCheckAfterUsage(ctx context.Context, userID int64) {
	s.scheduleCheckAfterUsage(ctx, userID, 0)
}

func (s *AutoConcurrencyUpgradeService) ScheduleCheckAfterUsageForUser(ctx context.Context, user *User) {
	if user == nil {
		return
	}
	s.scheduleCheckAfterUsage(ctx, user.ID, user.Concurrency)
}

func (s *AutoConcurrencyUpgradeService) scheduleCheckAfterUsage(ctx context.Context, userID int64, knownConcurrency int) {
	if s == nil || s.userRepo == nil || s.usageRepo == nil || s.settingService == nil || userID <= 0 {
		return
	}
	settings, err := s.settingService.GetAutoConcurrencyUpgradeSettings(ctx)
	if err != nil {
		slog.Warn("auto concurrency upgrade: load settings before schedule failed", "user_id", userID, "error", err)
		return
	}
	// The feature is disabled by default. Check the cached global setting before
	// touching per-user cooldown state or creating background work.
	if !settings.Enabled || knownConcurrency >= settings.MaxConcurrency {
		return
	}
	if !s.shouldSchedule(userID, time.Now()) {
		return
	}
	go s.checkAndUpgradeAfterUsage(ctx, userID, knownConcurrency)
}

func (s *AutoConcurrencyUpgradeService) CheckAndUpgradeAfterUsage(ctx context.Context, userID int64) {
	s.checkAndUpgradeAfterUsage(ctx, userID, 0)
}

func (s *AutoConcurrencyUpgradeService) checkAndUpgradeAfterUsage(ctx context.Context, userID int64, knownConcurrency int) {
	if s == nil || s.userRepo == nil || s.usageRepo == nil || s.settingService == nil || userID <= 0 {
		return
	}
	if !s.markInFlight(userID) {
		return
	}
	defer s.markDone(userID)

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
	if knownConcurrency >= settings.MaxConcurrency {
		return
	}
	if knownConcurrency <= 0 && s.userConcurrencyAtOrAbove(workCtx, userID, settings.MaxConcurrency) {
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

func (s *AutoConcurrencyUpgradeService) userConcurrencyAtOrAbove(ctx context.Context, userID int64, concurrency int) bool {
	if s == nil || s.userRepo == nil || userID <= 0 || concurrency <= 0 {
		return false
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		slog.Warn("auto concurrency upgrade: load user failed", "user_id", userID, "error", err)
		return false
	}
	return user != nil && user.Concurrency >= concurrency
}

func (s *AutoConcurrencyUpgradeService) shouldSchedule(userID int64, now time.Time) bool {
	if s == nil || userID <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextCheckAtByUser == nil {
		s.nextCheckAtByUser = make(map[int64]time.Time)
	}
	if s.inFlightByUser == nil {
		s.inFlightByUser = make(map[int64]struct{})
	}
	if _, ok := s.inFlightByUser[userID]; ok {
		return false
	}
	if next, ok := s.nextCheckAtByUser[userID]; ok && now.Before(next) {
		return false
	}
	s.nextCheckAtByUser[userID] = now.Add(autoConcurrencyUpgradeCheckInterval)
	return true
}

func (s *AutoConcurrencyUpgradeService) markInFlight(userID int64) bool {
	if s == nil || userID <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlightByUser == nil {
		s.inFlightByUser = make(map[int64]struct{})
	}
	if _, ok := s.inFlightByUser[userID]; ok {
		return false
	}
	s.inFlightByUser[userID] = struct{}{}
	return true
}

func (s *AutoConcurrencyUpgradeService) markDone(userID int64) {
	if s == nil || userID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.inFlightByUser, userID)
	s.mu.Unlock()
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
