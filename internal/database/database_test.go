package database

import (
	"path/filepath"
	"testing"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func TestOpenFreshDatabase(t *testing.T) {
	db, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer Close(db)
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n); err != nil {
		t.Fatalf("query users: %v", err)
	}
	// Sanity: core tables exist.
	for _, table := range []string{"users", "courses", "enrollments", "assignments",
		"rubric_criteria", "submissions", "evaluations", "criterion_results", "sessions"} {
		if _, err := db.Exec("SELECT 1 FROM " + table); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	path := tempPath(t)
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	Close(db1)
	db2, err := Open(path) // second open re-runs Migrate on existing data
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer Close(db2)
	var n int
	if err := db2.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if n == 0 {
		t.Fatalf("no migrations recorded")
	}
	// A third explicit Migrate must be a no-op.
	if err := Migrate(db2); err != nil {
		t.Fatalf("repeat Migrate: %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer Close(db)
	if _, err := db.Exec(`INSERT INTO courses (faculty_id, name, code) VALUES (999, 'X', 'X1')`); err == nil {
		t.Fatalf("expected FK violation for missing faculty")
	}
}

func TestWALBusyTimeout(t *testing.T) {
	db, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer Close(db)
	var mode, timeout string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode=%q err=%v", mode, err)
	}
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout == "0" {
		t.Fatalf("busy_timeout=%q err=%v", timeout, err)
	}
}
