INSERT INTO settings (key, value)
VALUES ('invitation_high_spender_enabled', 'false')
ON CONFLICT (key) DO NOTHING;
