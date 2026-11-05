// Package auth implements bcrypt password hashing and SQLite-backed sessions.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HashCost is the bcrypt work factor.
const HashCost = 10

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), HashCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// VerifyPassword reports whether the plaintext matches the stored hash.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

var errWeakToken = errors.New("token entropy failure")

// NewToken generates a URL-safe random session token (256 bits).
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// TokenHash is the deterministic SHA-256 digest stored instead of the raw token.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenHex hex-encodes raw bytes (used for anonymous CSRF salt).
func TokenHex(b []byte) string { return hex.EncodeToString(b) }

// CheckHash is a constant-time token comparison helper for tests/reviews.
func CheckHash(hash, token string) bool {
	return subtle.ConstantTimeCompare([]byte(hash), []byte(TokenHash(token))) == 1
}

// NormalizeEmail trims and lowercases an email for storage and comparison.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return "", errors.New("invalid email address")
	}
	return email, nil
}
