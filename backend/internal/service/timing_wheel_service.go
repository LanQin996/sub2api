package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/zeromicro/go-zero/core/collection"
)

var newTimingWheel = collection.NewTimingWheel

// TimingWheelService wraps go-zero's TimingWheel for task scheduling
type TimingWheelService struct {
	tw       *collection.TimingWheel
	stopOnce sync.Once

	mu             sync.Mutex
	stopped        bool
	nextGeneration uint64
	recurring      map[string]uint64
}

// NewTimingWheelService creates a new TimingWheelService instance
func NewTimingWheelService() (*TimingWheelService, error) {
	// 1 second tick, 3600 slots = supports up to 1 hour delay
	// execute function: runs func() type tasks
	tw, err := newTimingWheel(1*time.Second, 3600, func(key, value any) {
		if fn, ok := value.(func()); ok {
			fn()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("创建 timing wheel 失败: %w", err)
	}
	return &TimingWheelService{tw: tw, recurring: make(map[string]uint64)}, nil
}

// Start starts the timing wheel
func (s *TimingWheelService) Start() {
	logger.LegacyPrintf("service.timing_wheel", "%s", "[TimingWheel] Started (auto-start by go-zero)")
}

// Stop stops the timing wheel
func (s *TimingWheelService) Stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		clear(s.recurring)
		s.mu.Unlock()
		s.tw.Stop()
		logger.LegacyPrintf("service.timing_wheel", "%s", "[TimingWheel] Stopped")
	})
}

// Schedule schedules a one-time task
func (s *TimingWheelService) Schedule(name string, delay time.Duration, fn func()) {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		logger.LegacyPrintf("service.timing_wheel", "[TimingWheel] SetTimer skipped after stop for %q", name)
		return
	}
	if err := s.tw.SetTimer(name, fn, delay); err != nil {
		logger.LegacyPrintf("service.timing_wheel", "[TimingWheel] SetTimer failed for %q: %v", name, err)
	}
}

// ScheduleRecurring schedules a recurring task
func (s *TimingWheelService) ScheduleRecurring(name string, interval time.Duration, fn func()) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		logger.LegacyPrintf("service.timing_wheel", "[TimingWheel] recurring SetTimer skipped after stop for %q", name)
		return
	}
	s.nextGeneration++
	generation := s.nextGeneration
	s.recurring[name] = generation

	var schedule func()
	schedule = func() {
		fn()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopped || s.recurring[name] != generation {
			return
		}
		if err := s.tw.SetTimer(name, schedule, interval); err != nil {
			logger.LegacyPrintf("service.timing_wheel", "[TimingWheel] recurring SetTimer failed for %q: %v", name, err)
		}
	}
	if err := s.tw.SetTimer(name, schedule, interval); err != nil {
		delete(s.recurring, name)
		logger.LegacyPrintf("service.timing_wheel", "[TimingWheel] initial SetTimer failed for %q: %v", name, err)
	}
	s.mu.Unlock()
}

// Cancel cancels a scheduled task
func (s *TimingWheelService) Cancel(name string) {
	s.mu.Lock()
	delete(s.recurring, name)
	_ = s.tw.RemoveTimer(name)
	s.mu.Unlock()
}
