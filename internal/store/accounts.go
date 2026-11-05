package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"educore/internal/auth"
)

// CreateStudentAccount creates a new student account with a bcrypt hash.
// Duplicate email addresses are reported distinctly.
func (s *Store) CreateStudentAccount(email, name, plain string) (int64, error) {
	email, err := auth.NormalizeEmail(email)
	if err != nil {
		return 0, fmt.Errorf("%w: enter a valid email address", ErrInvalid)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(plain) < 8 {
		return 0, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalid)
	}
	hasLetter := strings.ContainsAny(plain, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	hasDigit := strings.ContainsAny(plain, "0123456789")
	if !hasLetter || !hasDigit {
		return 0, fmt.Errorf("%w: password needs letters and a number", ErrInvalid)
	}
	var existing int
	if err := s.DB.QueryRow(`SELECT COUNT(1) FROM users WHERE email = ?`, email).Scan(&existing); err == nil && existing > 0 {
		return 0, fmt.Errorf("%w: an account with email %s already exists", ErrDuplicate, email)
	}
	hash, err := auth.HashPassword(plain)
	if err != nil {
		return 0, err
	}
	res, err := s.DB.Exec(`INSERT INTO users (email, name, password_hash, role) VALUES (?, ?, ?, 'STUDENT')`,
		email, name, hash)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("%w: an account with email %s already exists", ErrDuplicate, email)
		}
		return 0, err
	}
	return res.LastInsertId()
}

// StudentPasswordChanging is disallowed entirely at account creation time.
var _ = errors.Is
var _ *sql.DB
