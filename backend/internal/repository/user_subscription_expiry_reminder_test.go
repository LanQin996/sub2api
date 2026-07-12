package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newUserSubscriptionReminderRepoSQLite(t *testing.T) (*userSubscriptionRepository, *dbent.Client) {
	t.Helper()

	dsn := fmt.Sprintf("file:subscription_expiry_reminder_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &userSubscriptionRepository{client: client}, client
}

func TestUserSubscriptionRepositoryListActiveExpiringBetweenFiltersAndPages(t *testing.T) {
	repo, client := newUserSubscriptionReminderRepoSQLite(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)
	startsAt := now.Add(7 * 24 * time.Hour)
	endsAt := startsAt.Add(24 * time.Hour)

	userModel, err := client.User.Create().
		SetEmail("expiry-reminder@test.com").
		SetUsername("expiry-user").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	createGroup := func(name string) *dbent.Group {
		groupModel, createErr := client.Group.Create().
			SetName(name).
			SetStatus(service.StatusActive).
			Save(ctx)
		require.NoError(t, createErr)
		return groupModel
	}
	createSubscription := func(groupID int64, expiresAt time.Time, status string) *dbent.UserSubscription {
		sub, createErr := client.UserSubscription.Create().
			SetUserID(userModel.ID).
			SetGroupID(groupID).
			SetStartsAt(now.Add(-24 * time.Hour)).
			SetExpiresAt(expiresAt).
			SetStatus(status).
			SetAssignedAt(now).
			SetNotes("").
			Save(ctx)
		require.NoError(t, createErr)
		return sub
	}

	sharedExpiry := startsAt
	beforeWindow := startsAt.Add(-time.Second)
	require.Equal(t, 6, int(beforeWindow.Sub(now)/(24*time.Hour)))
	require.Equal(t, 7, int(sharedExpiry.Sub(now)/(24*time.Hour)))
	require.Equal(t, 8, int(endsAt.Sub(now)/(24*time.Hour)))
	first := createSubscription(createGroup("reminder-first").ID, sharedExpiry, service.SubscriptionStatusActive)
	second := createSubscription(createGroup("reminder-second").ID, sharedExpiry, service.SubscriptionStatusActive)
	createSubscription(createGroup("reminder-before-boundary").ID, beforeWindow, service.SubscriptionStatusActive)
	createSubscription(createGroup("reminder-end-boundary").ID, endsAt, service.SubscriptionStatusActive)
	createSubscription(createGroup("reminder-expired-status").ID, startsAt.Add(time.Hour), service.SubscriptionStatusExpired)
	deleted := createSubscription(createGroup("reminder-deleted").ID, startsAt.Add(2*time.Hour), service.SubscriptionStatusActive)
	require.NoError(t, repo.Delete(ctx, deleted.ID))

	page1, err := repo.ListActiveExpiringBetween(ctx, startsAt, endsAt, time.Time{}, 0, 1)
	require.NoError(t, err)
	require.Len(t, page1, 1)
	require.Equal(t, first.ID, page1[0].ID)
	require.NotNil(t, page1[0].User)
	require.Equal(t, "expiry-reminder@test.com", page1[0].User.Email)
	require.Equal(t, "expiry-user", page1[0].User.Username)
	require.NotNil(t, page1[0].Group)
	require.Equal(t, "reminder-first", page1[0].Group.Name)

	page2, err := repo.ListActiveExpiringBetween(ctx, startsAt, endsAt, page1[0].ExpiresAt, page1[0].ID, 10)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Equal(t, second.ID, page2[0].ID)
}
