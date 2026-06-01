-- Add multi-use and random-amount support for redeem codes.

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS max_redemptions INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS redeemed_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS per_user_limit BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS random_amount_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS random_min_value DECIMAL(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS random_max_value DECIMAL(20,8) NOT NULL DEFAULT 0;

UPDATE redeem_codes
SET redeemed_count = CASE WHEN status = 'used' THEN 1 ELSE 0 END
WHERE redeemed_count = 0;

CREATE TABLE IF NOT EXISTS redeem_code_usages (
    id BIGSERIAL PRIMARY KEY,
    redeem_code_id BIGINT NOT NULL REFERENCES redeem_codes(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    value DECIMAL(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS redeem_code_usages_redeem_code_id_user_id_key
    ON redeem_code_usages (redeem_code_id, user_id);

CREATE INDEX IF NOT EXISTS redeem_code_usages_user_id_created_at_idx
    ON redeem_code_usages (user_id, created_at);

CREATE INDEX IF NOT EXISTS redeem_code_usages_redeem_code_id_idx
    ON redeem_code_usages (redeem_code_id);
