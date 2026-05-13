ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS aff_enabled BOOLEAN NOT NULL DEFAULT false;

UPDATE user_affiliates
SET aff_enabled = true,
    updated_at = NOW()
WHERE aff_enabled = false
  AND (
      aff_code_custom = true
      OR aff_rebate_rate_percent IS NOT NULL
      OR aff_count > 0
      OR aff_quota > 0
      OR aff_history_quota > 0
      OR aff_frozen_quota > 0
  );

CREATE INDEX IF NOT EXISTS idx_user_affiliates_admin_visible
    ON user_affiliates (updated_at)
    WHERE aff_enabled = true
       OR aff_code_custom = true
       OR aff_rebate_rate_percent IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_affiliates_aff_enabled
    ON user_affiliates (user_id)
    WHERE aff_enabled = true;

COMMENT ON COLUMN user_affiliates.aff_enabled IS 'Whether this user is allowed to use affiliate invite rebate as an inviter.';
