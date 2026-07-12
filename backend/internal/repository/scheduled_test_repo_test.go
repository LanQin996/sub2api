package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestScheduledTestPlanRepositoryClaimDueUsesAtomicBoundedClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 7, 12, 12, 0, 10, 0, time.UTC)
	leaseUntil := now.Add(6 * time.Minute)
	createdAt := now.Add(-time.Hour)
	updatedAt := now
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "model_id", "cron_expression", "enabled", "max_results",
		"auto_recover", "last_run_at", "next_run_at", "created_at", "updated_at",
	}).AddRow(
		int64(7), int64(11), "gpt-test", "* * * * *", true, 50,
		false, nil, leaseUntil, createdAt, updatedAt,
	)

	mock.ExpectQuery(`(?s)WITH due AS \(.*ORDER BY next_run_at ASC.*LIMIT \$2.*FOR UPDATE SKIP LOCKED.*UPDATE scheduled_test_plans AS plan.*SET next_run_at = \$3.*RETURNING plan.id`).
		WithArgs(now, 10, leaseUntil).
		WillReturnRows(rows)

	repo := &scheduledTestPlanRepository{db: db}
	plans, err := repo.ClaimDue(context.Background(), now, leaseUntil, 10)

	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, int64(7), plans[0].ID)
	require.NotNil(t, plans[0].NextRunAt)
	require.Equal(t, leaseUntil, *plans[0].NextRunAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScheduledTestPlanRepositoryClaimDueSkipsNonPositiveLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &scheduledTestPlanRepository{db: db}
	plans, err := repo.ClaimDue(context.Background(), time.Now(), time.Now().Add(time.Minute), 0)

	require.NoError(t, err)
	require.Empty(t, plans)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScheduledTestPlanRepositoryCompleteClaimUsesLeaseFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	leaseUntil := time.Date(2026, 7, 12, 12, 6, 10, 0, time.UTC)
	lastRunAt := leaseUntil.Add(-5 * time.Minute)
	nextRunAt := leaseUntil.Add(time.Hour)
	mock.ExpectExec(`(?s)UPDATE scheduled_test_plans.*SET last_run_at = \$3, next_run_at = \$4.*WHERE id = \$1 AND next_run_at = \$2`).
		WithArgs(int64(7), leaseUntil, lastRunAt, nextRunAt).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := &scheduledTestPlanRepository{db: db}
	completed, err := repo.CompleteClaim(context.Background(), 7, leaseUntil, lastRunAt, nextRunAt)

	require.NoError(t, err)
	require.False(t, completed)
	require.NoError(t, mock.ExpectationsWereMet())
}
