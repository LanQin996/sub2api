CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_subscriptions_active_expiry_reminder
    ON user_subscriptions (expires_at, id)
    WHERE deleted_at IS NULL AND status = 'active';
