package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/k001_rate_limits.sql
var migrationRateLimit string

//go:embed migrations/k002_tools.sql
var migrationTools string

//go:embed migrations/k003_downloads.sql
var migrationDownloads string

//go:embed migrations/k004_payments.sql
var migrationPayments string

//go:embed migrations/k005_idempotency.sql
var migrationIdempotency string

//go:embed migrations/k006_token_hash.sql
var migrationTokenHash string

//go:embed migrations/k007_idempotency_ttl.sql
var migrationIdempotencyTTL string

//go:embed migrations/k008_docgen_downloads.sql
var migrationDocgenDownloads string

//go:embed migrations/k010_trial_pass.sql
var migrationTrialPass string

var kitMigrations = []struct {
	version string
	sql     string
}{
	{"k001_rate_limits", migrationRateLimit},
	{"k002_tools", migrationTools},
	{"k003_downloads", migrationDownloads},
	{"k004_payments", migrationPayments},
	{"k005_idempotency", migrationIdempotency},
	{"k006_token_hash", migrationTokenHash},
	{"k007_idempotency_ttl", migrationIdempotencyTTL},
	{"k008_docgen_downloads", migrationDocgenDownloads},
	{"k010_trial_pass", migrationTrialPass},
}

var Conn *sql.DB

func Open(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("crear carpeta db: %w", err)
	}

	var err error
	Conn, err = sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("abrir sqlite: %w", err)
	}

	if err = Conn.Ping(); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}

	return nil
}

func Close() error {
	return Conn.Close()
}

func Migrate(migrationsDir string) error {
	if err := ensureSchemaTable(); err != nil {
		return err
	}

	for _, m := range kitMigrations {
		if err := applyMigration(m.version, m.sql); err != nil {
			return fmt.Errorf("kit migration %s: %w", m.version, err)
		}
	}

	return migrateDomain(migrationsDir)
}

func ensureSchemaTable() error {
	_, err := Conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     TEXT PRIMARY KEY,
			applied_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	return err
}

func applyMigration(version, sql string) error {
	var count int
	err := Conn.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := Conn.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(sql); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func migrateDomain(migrationsDir string) error {
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("leer migraciones: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	for _, f := range files {
		if filepath.Ext(f.Name()) != ".sql" {
			continue
		}

		version := f.Name()[:len(f.Name())-4]

		var count int
		err := Conn.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("verificar migracion %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, f.Name()))
		if err != nil {
			return fmt.Errorf("leer archivo %s: %w", version, err)
		}

		tx, err := Conn.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("ejecutar %s: %w", version, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			_ = tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commitar %s: %w", version, err)
		}
	}

	return nil
}
