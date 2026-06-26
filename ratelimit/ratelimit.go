package ratelimit

import (
	"context"
	"database/sql"
	"time"
)

const timeFmt = "2006-01-02 15:04:05.000"

func Check(ctx context.Context, db *sql.DB, key string, maxAttempts int, window time.Duration) (bool, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(window).Format(timeFmt)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var attempts int
	var existingExpires string
	err = tx.QueryRow("SELECT attempts, expires_at FROM rate_limits WHERE key = ?", key).Scan(&attempts, &existingExpires)
	if err == sql.ErrNoRows {
		_, err = tx.Exec("INSERT INTO rate_limits (key, attempts, expires_at) VALUES (?, 1, ?)", key, expiresAt)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}

	expires, err := time.Parse(timeFmt, existingExpires)
	if err != nil || now.After(expires) {
		_, err = tx.Exec("UPDATE rate_limits SET attempts = 1, expires_at = ? WHERE key = ?", expiresAt, key)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}

	if attempts >= maxAttempts {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	_, err = tx.Exec("UPDATE rate_limits SET attempts = attempts + 1 WHERE key = ?", key)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func Cleanup(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, "DELETE FROM rate_limits WHERE expires_at < ?", time.Now().UTC().Format(timeFmt))
}
