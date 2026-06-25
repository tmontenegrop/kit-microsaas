ALTER TABLE idempotency_keys ADD COLUMN expires_at TEXT;
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);
