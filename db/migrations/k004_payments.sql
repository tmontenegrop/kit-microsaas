CREATE TABLE IF NOT EXISTS payments (
    id              TEXT PRIMARY KEY,
    download_id     TEXT NOT NULL REFERENCES downloads(id),
    amount          INTEGER NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    payment_token   TEXT,
    flow_token      TEXT,
    flow_order      TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    confirmed_at    TEXT,
    error_message   TEXT
);
