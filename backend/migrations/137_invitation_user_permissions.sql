ALTER TABLE users
    ADD COLUMN IF NOT EXISTS invitation_enabled BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS created_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_users_invitation_enabled
    ON users (id)
    WHERE invitation_enabled = true;

CREATE INDEX IF NOT EXISTS idx_redeem_codes_created_by_type_created_at
    ON redeem_codes (created_by, type, created_at);

COMMENT ON COLUMN users.invitation_enabled IS 'Whether this user can generate one-time invitation redeem codes.';
COMMENT ON COLUMN redeem_codes.created_by IS 'User ID that generated this redeem code; used for invitation distribution auditing.';
