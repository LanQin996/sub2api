package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

const (
	scheduledTestStartDelay = 10 * time.Second
	scheduledTestRunTimeout = 5 * time.Minute
	scheduledTestClaimLease = scheduledTestRunTimeout + time.Minute
)

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService
	cfg            *config.Config

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	stopping    bool
	running     bool
	runWG       sync.WaitGroup
	startDelay  time.Duration
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
		cfg:            cfg,
		ctx:            ctx,
		cancel:         cancel,
		startDelay:     scheduledTestStartDelay,
	}
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] not started (invalid schedule): %v", err)
			return
		}

		s.lifecycleMu.Lock()
		if s.stopping {
			s.lifecycleMu.Unlock()
			return
		}
		s.cron = c
		s.cron.Start()
		s.lifecycleMu.Unlock()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *ScheduledTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopping = true
		if s.cancel != nil {
			s.cancel()
		}
		c := s.cron
		s.lifecycleMu.Unlock()

		if c != nil {
			<-c.Stop().Done()
		}
		s.runWG.Wait()
	})
}

func (s *ScheduledTestRunnerService) runScheduled() {
	if !s.beginRun() {
		return
	}
	defer s.endRun()

	// Delay 10s so execution lands at ~:10 of each minute instead of :00.
	timer := time.NewTimer(s.startDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-s.ctx.Done():
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, scheduledTestRunTimeout)
	defer cancel()

	now := time.Now()
	plans, err := s.planRepo.ClaimDue(ctx, now, now.Add(scheduledTestClaimLease), scheduledTestDefaultMaxWorkers)
	if err != nil {
		if ctx.Err() == nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ClaimDue error: %v", err)
		}
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	var wg sync.WaitGroup

	for _, plan := range plans {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) beginRun() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.stopping || s.running {
		return false
	}
	s.running = true
	s.runWG.Add(1)
	return true
}

func (s *ScheduledTestRunnerService) endRun() {
	s.lifecycleMu.Lock()
	s.running = false
	s.lifecycleMu.Unlock()
	s.runWG.Done()
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	if plan.NextRunAt == nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d missing claim lease", plan.ID)
		return
	}
	leaseUntil := *plan.NextRunAt

	result, err := s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		return
	}

	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, err)
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		return
	}

	completed, err := s.planRepo.CompleteClaim(ctx, plan.ID, leaseUntil, time.Now(), nextRun)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d CompleteClaim error: %v", plan.ID, err)
	} else if !completed {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d claim no longer current", plan.ID)
	}
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s.rateLimitSvc == nil {
		return
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}
