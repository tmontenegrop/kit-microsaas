ALTER TABLE downloads ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_downloads_token_hash ON downloads(token_hash);
