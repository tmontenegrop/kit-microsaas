CREATE TABLE IF NOT EXISTS trial_tracking (
    key         TEXT PRIMARY KEY,
    doc_count   INTEGER NOT NULL DEFAULT 0,
    window_start TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT
);
