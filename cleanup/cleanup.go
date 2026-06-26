package cleanup

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tmontenegrop/kit-microsaas/ratelimit"
)

func Run(db *sql.DB, interval time.Duration) {
	ctx := context.Background()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := expireOldDownloads(ctx, db); err != nil {
			slog.Error("cleanup downloads", "error", err)
		}
		if err := deleteExpiredIdempotencyKeys(ctx, db); err != nil {
			slog.Error("cleanup idempotency keys", "error", err)
		}
		ratelimit.Cleanup(ctx, db)
	}
}

func expireOldDownloads(ctx context.Context, db *sql.DB) error {
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")

	rows, err := db.QueryContext(ctx, `
		SELECT d.id, d.file_path
		FROM downloads d
		WHERE d.status IN ('draft', 'ready', 'pending') AND d.created_at < ?
	`, cutoff)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, filePath string
		if err := rows.Scan(&id, &filePath); err != nil {
			slog.Error("cleanup scan", "error", err)
			continue
		}

		if err := expireDownload(ctx, db, id, filePath); err != nil {
			slog.Error("cleanup expire download", "id", id, "error", err)
		}
	}

	return rows.Err()
}

func expireDownload(ctx context.Context, db *sql.DB, id, filePath string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, "UPDATE downloads SET status = 'expired' WHERE id = ? AND status IN ('draft', 'ready', 'pending')", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM payments WHERE download_id = ? AND status = 'pending'", id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if filePath != "" {
		_ = os.RemoveAll(filepath.Dir(filePath))
	}

	slog.Info("cleanup expired download", "id", id)
	return nil
}

func deleteExpiredIdempotencyKeys(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE expires_at IS NOT NULL AND expires_at < ?", time.Now().UTC().Format("2006-01-02 15:04:05"))
	return err
}
