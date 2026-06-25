CREATE TABLE IF NOT EXISTS rate_limits (
    key         TEXT PRIMARY KEY,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL
);
