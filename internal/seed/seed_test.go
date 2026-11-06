package seed

import (
	"path/filepath"
	"testing"

	"educore/internal/database"
)

func TestSeedIdempotent(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close(db)

	for round := 1; round <= 2; round++ {
		if err := Run(db); err != nil {
			t.Fatalf("seed round %d: %v", round, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 9 {
		t.Fatalf("users = %d, want 2 (seed not idempotent)", count)
	}
	var chapterCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM course_chapters ch
		JOIN courses c ON c.id = ch.course_id
		WHERE c.code = 'CS204' AND ch.published = 1`).Scan(&chapterCount); err != nil {
		t.Fatalf("count seeded chapters: %v", err)
	}
	if chapterCount != 2 {
		t.Fatalf("published demo chapters = %d, want 2", chapterCount)
	}
	var title string
	if err := db.QueryRow(`SELECT ch.title FROM course_chapters ch
		JOIN courses c ON c.id = ch.course_id
		WHERE c.code = 'CS204' AND ch.position = 2`).Scan(&title); err != nil {
		t.Fatalf("load demo Chapter 2: %v", err)
	}
	if title != "Relational Model, Keys and Integrity Constraints" {
		t.Fatalf("demo Chapter 2 title = %q", title)
	}

	// Password hashes must not contain plaintext.
	var hashFac, hashStu string
	_ = db.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, FacultyEmail).Scan(&hashFac)
	_ = db.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, StudentEmail).Scan(&hashStu)
	for h, p := range map[string]string{hashFac: FacultyPass, hashStu: StudentPass} {
		if len(h) < 40 || h == p {
			t.Fatalf("password not hashed properly")
		}
	}
	if hashFac == hashStu {
		t.Fatalf("identical hashes for different accounts")
	}
}

func TestSeedPreservesChangedPasswords(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "seed2.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close(db)
	if err := Run(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`UPDATE users SET password_hash = 'changed' WHERE email = ?`, FacultyEmail); err != nil {
		t.Fatalf("simulate rehash: %v", err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	var hash string
	_ = db.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, FacultyEmail).Scan(&hash)
	if hash != "changed" {
		t.Fatalf("seed overwrote existing password hash")
	}
}
