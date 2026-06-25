CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         TEXT PRIMARY KEY,
    response    TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
