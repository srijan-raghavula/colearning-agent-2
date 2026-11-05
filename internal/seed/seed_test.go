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
