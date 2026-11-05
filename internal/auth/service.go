package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"educore/internal/models"
)

// ErrSessionExpired is returned when a session exists but has timed out.
var ErrSessionExpired = errors.New("session expired")

// ErrInvalidCredentials is returned for wrong email or password.
var ErrInvalidCredentials = errors.New("invalid email or password")

// Service wraps the database for user and session operations.
type Service struct {
	db *sql.DB
}

// NewService builds an auth service over the SQLite connection.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateAccount inserts a user with a bcrypt hash; idempotent by unique email.
func (s *Service) CreateAccount(email, name, plain, role string) (int64, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return 0, err
	}
	if name == "" || (role != "FACULTY" && role != "STUDENT") || len(plain) < 8 {
		return 0, errors.New("invalid account parameters")
	}
	hash, err := HashPassword(plain)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(
		`INSERT INTO users (email, name, password_hash, role) VALUES (?, ?, ?, ?)
		 ON CONFLICT(email) DO NOTHING`,
		email, name, hash, role)
	if err != nil {
		return 0, fmt.Errorf("create account: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Existing account: leave the stored hash untouched.
		var id int64
		qerr := s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
		if qerr != nil {
			return 0, qerr
		}
		return id, nil
	}
	return res.LastInsertId()
}

// Authenticate validates credentials and returns the user.
func (s *Service) Authenticate(email, plain string) (models.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return models.User{}, ErrInvalidCredentials
	}
	var u models.User
	var hash string
	row := s.db.QueryRow(`SELECT id, email, name, password_hash, role FROM users WHERE email = ?`, email)
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &hash, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrInvalidCredentials
		}
		return models.User{}, err
	}
	if !VerifyPassword(hash, plain) {
		return models.User{}, ErrInvalidCredentials
	}
	return u, nil
}

// GetUser resolves a user by ID.
func (s *Service) GetUser(id int64) (models.User, error) {
	var u models.User
	err := s.db.QueryRow(`SELECT id, email, name, role FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role)
	return u, err
}

// CreateSession stores only the token hash with an expiry.
func (s *Service) CreateSession(userID int64, lifetime time.Duration) (token string, err error) {
	token, err = NewToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		TokenHash(token), userID, time.Now().UTC().Add(lifetime).Format(time.DateTime))
	if err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return token, nil
}

// LookupSession returns the session owner for a raw token, or an error when
// the token is unknown or expired. Expired sessions are lazily purged.
func (s *Service) LookupSession(token string) (models.User, error) {
	var u models.User
	var expires string
	err := s.db.QueryRow(
		`SELECT u.id, u.email, u.name, u.role, s.expires_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = ?`, TokenHash(token)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrSessionExpired
		}
		return models.User{}, err
	}
	exp, err := time.Parse(time.DateTime, expires)
	if err != nil {
		return models.User{}, fmt.Errorf("parse expiry: %w", err)
	}
	if time.Now().UTC().After(exp.UTC()) {
		_ = s.Revoke(token)
		return models.User{}, ErrSessionExpired
	}
	return u, nil
}

// Revoke deletes a session by raw token (logout / rotation).
func (s *Service) Revoke(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, TokenHash(token))
	return err
}

// Rotate invalidates the old session and creates a fresh one.
func (s *Service) Rotate(token string, userID int64, lifetime time.Duration) (string, error) {
	if err := s.Revoke(token); err != nil {
		return "", err
	}
	return s.CreateSession(userID, lifetime)
}

// RevokeUserSessions invalidates every session of a user (login rotation).
func (s *Service) RevokeUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// PurgeExpired removes expired sessions; safe to call on a timer/startup.
func (s *Service) PurgeExpired() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.DateTime))
	return err
}

// SessionLifetime default of 7 days.
const SessionLifetime = 7 * 24 * time.Hour
