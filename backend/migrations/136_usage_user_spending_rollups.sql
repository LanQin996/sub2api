-- Per-user usage spending rollups for fast leaderboard queries.
-- Buckets are fixed to UTC so ranking requests from any user timezone can
-- combine complete buckets with raw boundary ranges exactly.

CREATE TABLE IF NOT EXISTS usage_user_hourly_spending (
    bucket_start TIMESTAMPTZ NOT NULL,
    user_id BIGINT NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_start, user_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_user_hourly_spending_bucket_cost
    ON usage_user_hourly_spending (bucket_start, actual_cost DESC, user_id);

CREATE TABLE IF NOT EXISTS usage_user_hourly_spending_coverage (
    bucket_start TIMESTAMPTZ PRIMARY KEY,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS usage_user_daily_spending (
    bucket_date DATE NOT NULL,
    user_id BIGINT NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_date, user_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_user_daily_spending_bucket_cost
    ON usage_user_daily_spending (bucket_date, actual_cost DESC, user_id);

CREATE TABLE IF NOT EXISTS usage_user_daily_spending_coverage (
    bucket_date DATE PRIMARY KEY,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE usage_user_hourly_spending IS 'UTC hourly per-user spending rollups for leaderboard queries.';
COMMENT ON TABLE usage_user_daily_spending IS 'UTC daily per-user spending rollups for leaderboard queries.';
COMMENT ON TABLE usage_user_hourly_spending_coverage IS 'UTC hourly rollup coverage markers; only completed buckets are used by ranking queries.';
COMMENT ON TABLE usage_user_daily_spending_coverage IS 'UTC daily rollup coverage markers; only completed buckets are used by ranking queries.';
