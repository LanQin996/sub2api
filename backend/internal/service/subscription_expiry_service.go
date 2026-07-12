package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const (
	subscriptionExpiryJobTimeout       = 10 * time.Second
	subscriptionExpiryReminderInterval = time.Hour
	subscriptionExpiryReminderPageSize = 200

	// subscriptionExpiryReminderLeaderLockKey gates the per-cycle reminder scan so
	// that only one instance scans due subscriptions and sends reminder emails,
	// avoiding redundant work and duplicate emails.
	subscriptionExpiryReminderLeaderLockKey = "subscription:expiry:reminder:leader"
	// subscriptionExpiryReminderLeaderLockTTL bounds crash recovery; the scan can
	// page through many subscriptions, so keep it comfortably above the job timeout.
	subscriptionExpiryReminderLeaderLockTTL = 5 * time.Minute
)

// SubscriptionExpiryService periodically updates expired subscription status.
type SubscriptionExpiryService struct {
	userSubRepo              UserSubscriptionRepository
	settingRepo              SettingRepository
	notificationEmailService *NotificationEmailService
	reminderRepo             SubscriptionExpiryReminderRepository
	expiryInterval           time.Duration
	reminderInterval         time.Duration
	stopCh                   chan struct{}
	startOnce                sync.Once
	stopOnce                 sync.Once
	wg                       sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

func NewSubscriptionExpiryService(userSubRepo UserSubscriptionRepository, interval time.Duration) *SubscriptionExpiryService {
	reminderRepo, _ := userSubRepo.(SubscriptionExpiryReminderRepository)
	return &SubscriptionExpiryService{
		userSubRepo:      userSubRepo,
		reminderRepo:     reminderRepo,
		expiryInterval:   interval,
		reminderInterval: subscriptionExpiryReminderInterval,
		stopCh:           make(chan struct{}),
		instanceID:       uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic expiry-reminder scan. When both are nil the scan runs
// ungated (single-instance / test behavior).
func (s *SubscriptionExpiryService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *SubscriptionExpiryService) SetSettingRepository(settingRepo SettingRepository) {
	s.settingRepo = settingRepo
}

func (s *SubscriptionExpiryService) SetNotificationEmailService(notificationEmailService *NotificationEmailService) {
	s.notificationEmailService = notificationEmailService
}

func (s *SubscriptionExpiryService) Start() {
	if s == nil || s.userSubRepo == nil || s.expiryInterval <= 0 || s.reminderInterval <= 0 {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			expiryTicker := time.NewTicker(s.expiryInterval)
			defer expiryTicker.Stop()
			reminderTicker := time.NewTicker(s.reminderInterval)
			defer reminderTicker.Stop()

			s.runOnce()
			for {
				select {
				case <-expiryTicker.C:
					s.updateExpiredStatuses()
				case <-reminderTicker.C:
					s.runExpiryReminders()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *SubscriptionExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SubscriptionExpiryService) runOnce() {
	s.updateExpiredStatuses()
	s.runExpiryReminders()
}

func (s *SubscriptionExpiryService) updateExpiredStatuses() {
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionExpiryJobTimeout)
	defer cancel()

	updated, err := s.userSubRepo.BatchUpdateExpiredStatus(ctx)
	if err != nil {
		log.Printf("[SubscriptionExpiry] Update expired subscriptions failed: %v", err)
		return
	}
	if updated > 0 {
		log.Printf("[SubscriptionExpiry] Updated %d expired subscriptions", updated)
	}

	// Expiry status must stay current, but reminder scans are intentionally
	// scheduled separately at a much lower frequency.
}

func (s *SubscriptionExpiryService) runExpiryReminders() {
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionExpiryJobTimeout)
	defer cancel()

	s.sendExpiryReminders(ctx)
}

func (s *SubscriptionExpiryService) sendExpiryReminders(ctx context.Context) {
	if s == nil || s.userSubRepo == nil || s.notificationEmailService == nil {
		return
	}
	if !s.expiryReminderEnabled(ctx) {
		return
	}

	// Multi-instance guard: only the leader scans due subscriptions and sends
	// reminders, avoiding N× work and duplicate reminder emails.
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, subscriptionExpiryReminderLeaderLockKey, s.instanceID, subscriptionExpiryReminderLeaderLockTTL)
	if !ok {
		return
	}
	defer release()

	if s.reminderRepo != nil {
		s.sendWindowedExpiryReminders(ctx, time.Now())
		return
	}

	// Compatibility path for custom repositories that have not implemented the
	// optimized extension. This now runs hourly rather than once per minute.
	s.sendLegacyExpiryReminders(ctx)
}

func (s *SubscriptionExpiryService) sendWindowedExpiryReminders(ctx context.Context, now time.Time) {
	// Each 24-hour bucket is revisited hourly. Successful deliveries are suppressed
	// by NotificationEmailService's ReminderKey, while failed deliveries can retry.
	for _, daysRemaining := range []int{7, 3, 1} {
		startsAt := now.Add(time.Duration(daysRemaining) * 24 * time.Hour)
		endsAt := startsAt.Add(24 * time.Hour)
		var afterExpiresAt time.Time
		var afterID int64

		for {
			subs, err := s.reminderRepo.ListActiveExpiringBetween(
				ctx,
				startsAt,
				endsAt,
				afterExpiresAt,
				afterID,
				subscriptionExpiryReminderPageSize,
			)
			if err != nil {
				log.Printf("[SubscriptionExpiry] List subscriptions in %d-day reminder window failed: %v", daysRemaining, err)
				return
			}
			for i := range subs {
				s.sendExpiryReminder(ctx, &subs[i], daysRemaining)
			}
			if len(subs) < subscriptionExpiryReminderPageSize {
				break
			}

			last := subs[len(subs)-1]
			if last.ExpiresAt.Before(afterExpiresAt) || (last.ExpiresAt.Equal(afterExpiresAt) && last.ID <= afterID) {
				log.Printf("[SubscriptionExpiry] Reminder cursor did not advance: expires_at=%s subscription=%d", last.ExpiresAt.Format(time.RFC3339Nano), last.ID)
				return
			}
			afterExpiresAt = last.ExpiresAt
			afterID = last.ID
		}
	}
}

func (s *SubscriptionExpiryService) sendLegacyExpiryReminders(ctx context.Context) {
	for page := 1; ; page++ {
		subs, pag, err := s.userSubRepo.List(ctx, pagination.PaginationParams{Page: page, PageSize: 200}, nil, nil, SubscriptionStatusActive, "", "expires_at", "asc")
		if err != nil {
			log.Printf("[SubscriptionExpiry] List active subscriptions for reminder failed: %v", err)
			return
		}
		for i := range subs {
			daysRemaining := subs[i].DaysRemaining()
			if daysRemaining == 7 || daysRemaining == 3 || daysRemaining == 1 {
				s.sendExpiryReminder(ctx, &subs[i], daysRemaining)
			}
		}
		if pag == nil || page >= pag.Pages || len(subs) == 0 {
			return
		}
	}
}

func (s *SubscriptionExpiryService) expiryReminderEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return true
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeySubscriptionExpiryNotifyEnabled)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return true
		}
		log.Printf("[SubscriptionExpiry] Read expiry reminder switch failed: %v", err)
		return false
	}
	return !isFalseSettingValue(value)
}

func (s *SubscriptionExpiryService) sendExpiryReminder(ctx context.Context, sub *UserSubscription, daysRemaining int) {
	if sub == nil || sub.User == nil || sub.Group == nil || sub.User.Email == "" {
		return
	}
	if daysRemaining != 7 && daysRemaining != 3 && daysRemaining != 1 {
		return
	}
	if err := s.notificationEmailService.Send(ctx, NotificationEmailSendInput{
		Event:          NotificationEmailEventSubscriptionExpiryReminder,
		RecipientEmail: sub.User.Email,
		RecipientName:  firstNonEmpty(sub.User.Username, sub.User.Email),
		UserID:         sub.UserID,
		SourceType:     "user_subscription",
		SourceID:       strconv.FormatInt(sub.ID, 10),
		ReminderKey:    fmt.Sprintf("%dd", daysRemaining),
		Variables: map[string]string{
			"subscription_group": sub.Group.Name,
			"expiry_time":        sub.ExpiresAt.Format("2006-01-02 15:04"),
			"days_remaining":     strconv.Itoa(daysRemaining),
		},
	}); err != nil {
		log.Printf("[SubscriptionExpiry] Send expiry reminder failed: subscription=%d user=%d err=%v", sub.ID, sub.UserID, err)
	}
}
