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
		"rubric_criteria", "submissions", "evaluations", "criterion_results", "sessions",
		"course_chapters"} {
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

func TestChapterMigrationPreservesExistingCourseData(t *testing.T) {
	path := tempPath(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (email, name, password_hash, role)
		VALUES ('migration-faculty@example.test', 'Faculty', 'hash', 'FACULTY')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO courses (faculty_id, name, code, description)
		VALUES (1, 'Existing course', 'EXISTING', 'Existing description')`); err != nil {
		t.Fatal(err)
	}
	if err := Close(db); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen after additive migration: %v", err)
	}
	defer Close(db)
	var name, description string
	if err := db.QueryRow(`SELECT name, description FROM courses WHERE code = 'EXISTING'`).Scan(&name, &description); err != nil {
		t.Fatalf("existing course unavailable after migration: %v", err)
	}
	if name != "Existing course" || description != "Existing description" {
		t.Fatalf("existing data changed: name=%q description=%q", name, description)
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = '005_course_chapters.sql'`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("chapter migration recorded %d times", migrations)
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
