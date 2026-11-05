// Package seed populates the demo dataset idempotently.
package seed

import (
	"database/sql"
	"log"

	"educore/internal/auth"
)

// Demo accounts required by the spec.
const (
	FacultyEmail = "faculty@educore.demo"
	FacultyPass  = "Faculty123!"
	StudentEmail = "student@educore.demo"
	StudentPass  = "Student123!"
)

// Run seeds demo accounts, then the showcase dataset. Repeatable.
func Run(db *sql.DB) error {
	if err := seedAccounts(db); err != nil {
		return err
	}
	if err := seedShowcase(db); err != nil {
		return err
	}
	return nil
}

func seedAccounts(db *sql.DB) error {
	svc := auth.NewService(db)
	specs := []struct{ email, name, pass, role string }{
		{FacultyEmail, "Dr. Priya Sharma", FacultyPass, "FACULTY"},
		{StudentEmail, "Arjun Mehta", StudentPass, "STUDENT"},
	}
	for _, sp := range specs {
		var exists bool
		err := db.QueryRow(`SELECT COUNT(1) FROM users WHERE email = ?`, sp.email).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			continue // never reset an existing account's password
		}
		if _, err := svc.CreateAccount(sp.email, sp.name, sp.pass, sp.role); err != nil {
			return err
		}
		log.Printf("seed: created %s account %s", sp.role, sp.email)
	}
	return nil
}
