package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// defaultMigrationDir holds the tracked SQL migration files at the project
// root. resolveMigrationsDir walks upward from the working directory so tests
// and tools run from any package directory; MIGRATIONS_DIR overrides it.
func defaultMigrationDir() string {
	if v := os.Getenv("MIGRATIONS_DIR"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "./migrations"
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			if _, s2 := os.Stat(filepath.Join(candidate, "001_init.sql")); s2 == nil {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "./migrations"
}

func migrationDir() string { return defaultMigrationDir() }

func init() {
	// modernc/sqlite: register the standard "sqlite" name; the driver
	// defaults to serial access; we set our own DSN options at open time.
}

// Open prepares the database directory and opens the SQLite connection with
// pragmas applied, then runs pending migrations.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// The pure-Go driver is safe for concurrent access; the small pool plus
	// WAL and a busy timeout keeps writes serialized. (A pool of 1 causes
	// self-deadlocks whenever one request's streaming rows re-query.)
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := pingFollowersUnsupported(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// pingFollowersUnsupported is a placeholder that verifies WAL mode took effect.
func pingFollowersUnsupported(db *sql.DB) error {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("check journal mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		log.Printf("warning: journal mode is %q, expected wal", mode)
	}
	return nil
}

// Close closes the database.
func Close(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

func migrationNames() ([]string, error) {
	entries, err := os.ReadDir(migrationDir())
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	return err
}

func applied(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		set[name] = true
	}
	return set, rows.Err()
}

// Migrate applies pending migrations inside individual transactions.
// It is idempotent: applied migrations are skipped.
func Migrate(db *sql.DB) error {
	if err := ensureMigrationTable(db); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	done, err := applied(db)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	names, err := migrationNames()
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	for _, name := range names {
		if done[name] {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrationDir(), name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err = tx.Exec(string(content)); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		log.Printf("migration applied: %s", name)
	}
	return nil
}

// Now is the canonical SQLite UTC timestamp format used across the app.
func Now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }
