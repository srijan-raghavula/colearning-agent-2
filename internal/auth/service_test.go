package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"educore/internal/database"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close(db) })
	// A seeded faculty account for session tests.
	if _, err := CreateAccount(db, "faculty@educore.demo", "Dr. Demo", "Faculty123!", "FACULTY"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return NewService(db)
}

// CreateAccount inserts (or updates) a user using raw SQL, for test setup only.
func CreateAccount(db *sql.DB, email, name, pass, role string) (int64, error) {
	hash, err := HashPassword(pass)
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(`INSERT INTO users (email, name, password_hash, role) VALUES (?, ?, ?, ?)
	                     ON CONFLICT(email) DO UPDATE SET password_hash = excluded.password_hash`,
		email, name, hash, role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func TestPasswordHashedNotPlaintext(t *testing.T) {
	hash, err := HashPassword("Student123!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, "Student123!") {
		t.Fatalf("hash contains plaintext")
	}
	if !VerifyPassword(hash, "Student123!") {
		t.Fatalf("verify failed for correct password")
	}
	if VerifyPassword(hash, "WrongPass!") {
		t.Fatalf("verify passed for wrong password")
	}
}

func TestAuthenticateValidAndInvalid(t *testing.T) {
	s := newSvc(t)
	if _, err := s.Authenticate("faculty@educore.demo", "Faculty123!"); err != nil {
		t.Fatalf("valid credentials rejected: %v", err)
	}
	if _, err := s.Authenticate("faculty@educore.demo", "WrongPass!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password accepted: %v", err)
	}
	if _, err := s.Authenticate("nobody@educore.demo", "Faculty123!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user accepted: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newSvc(t)
	u, err := s.Authenticate("faculty@educore.demo", "Faculty123!")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	token, err := s.CreateSession(u.ID, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := s.LookupSession(token)
	if err != nil || got.ID != u.ID {
		t.Fatalf("lookup: user=%+v err=%v", got, err)
	}

	// Expired sessions must be rejected and purged.
	if _, err := s.db.Exec(`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.DateTime), TokenHash(token)); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if _, err := s.LookupSession(token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session accepted: err=%v", err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatalf("expired session not purged (n=%d)", n)
	}

	// Logout revocation.
	token2, _ := s.CreateSession(u.ID, time.Hour)
	if err := s.Revoke(token2); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.LookupSession(token2); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("revoked session still valid")
	}
}

func TestPurgeExpiredOnly(t *testing.T) {
	s := newSvc(t)
	u, _ := s.Authenticate("faculty@educore.demo", "Faculty123!")
	keep, _ := s.CreateSession(u.ID, time.Hour)
	drop, _ := s.CreateSession(u.ID, -time.Minute)
	if err := s.PurgeExpired(); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := s.LookupSession(keep); err != nil {
		t.Fatalf("live session purged: %v", err)
	}
	if _, err := s.LookupSession(drop); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session survived purge")
	}
}
