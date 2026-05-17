-- Mark usage records that were billed from partial streaming usage after
-- the client disconnected or upstream did not provide terminal usage.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS partial_usage BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN usage_logs.partial_usage IS 'True when streaming usage was estimated from partial output because terminal usage was unavailable';
