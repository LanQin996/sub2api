-- Daily per-user Token activity snapshots used by the user dashboard.
-- The initial backfill intentionally reuses existing UTC daily rollups so the
-- migration never scans the potentially large usage_logs table.

CREATE TABLE IF NOT EXISTS user_token_activity_daily (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    activity_date DATE NOT NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, activity_date)
);

CREATE INDEX IF NOT EXISTS idx_user_token_activity_daily_date
    ON user_token_activity_daily (activity_date);

CREATE TABLE IF NOT EXISTS user_token_activity_job_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    last_processed_date DATE NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO user_token_activity_daily (
    user_id,
    activity_date,
    input_tokens,
    output_tokens,
    cache_creation_tokens,
    cache_read_tokens,
    total_tokens,
    refreshed_at
)
SELECT
    user_id,
    bucket_date,
    input_tokens,
    output_tokens,
    cache_creation_tokens,
    cache_read_tokens,
    input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens,
    NOW()
FROM usage_user_daily_spending
WHERE bucket_date >= CURRENT_DATE - 370
ON CONFLICT (user_id, activity_date) DO NOTHING;

INSERT INTO user_token_activity_job_state (id, last_processed_date)
VALUES (1, CURRENT_DATE - 1)
ON CONFLICT (id) DO NOTHING;

COMMENT ON TABLE user_token_activity_daily IS 'Once-daily per-user Token activity snapshots for the user dashboard.';
COMMENT ON TABLE user_token_activity_job_state IS 'Shared watermark for the daily Token activity snapshot job.';
