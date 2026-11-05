// Package store implements SQLite data access for EDUCORE domain entities.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"educore/internal/models"
)

// Store provides domain data access over a SQLite connection.
type Store struct {
	DB *sql.DB
}

// New wraps an existing connection.
func New(db *sql.DB) *Store { return &Store{DB: db} }

// Common sentinel errors surfaced to handlers for useful messages.
var (
	ErrNotFound   = errors.New("resource not found")
	ErrDuplicate  = errors.New("duplicate resource")
	ErrForbidden  = errors.New("not permitted")
	ErrInvalid    = errors.New("validation failed")
	ErrTimeFormat = errors.New("invalid date/time format")
)

// TimeLayout is the canonical stored datetime format (UTC).
const TimeLayout = "2006-01-02 15:04"

// TimeLayoutInput matches <input type="datetime-local"> values.
const TimeLayoutInput = "2006-01-02 15:04"

// ToInput maps a stored time string to the datetime-local format.
func ToInput(stored string) string {
	t, err := ParseStored(stored)
	if err != nil {
		return stored
	}
	return t.Local().Format(TimeLayoutInput)
}

// ParseStored parses stored UTC time.
func ParseStored(s string) (time.Time, error) {
	for _, layout := range []string{TimeLayout, "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrTimeFormat
}

// ParseDueInput parses a datetime-local input; naive values are interpreted
// in the server's local zone and stored as UTC.
func ParseDueInput(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ErrTimeFormat
	}
	if strings.HasSuffix(s, "Z") {
		return time.Parse("2006-01-02T15:04Z", s)
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		t, err := time.ParseInLocation(layout, s, time.Local)
		if err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrTimeFormat
}

// FormatDue renders a stored time for display.
func FormatDue(stored string) string {
	t, err := ParseStored(stored)
	if err != nil {
		return stored
	}
	return t.Local().Format("02 Jan 2006, 15:04")
}

// ---------- Courses ----------

// Course is one academic subject.
type Course struct {
	ID          int64
	FacultyID   int64
	FacultyName string
	Name        string
	Code        string
	Description string
	CreatedAt   string
	Students    int
	Assignments int
}

// CreateCourse inserts a course owned by faculty.
func (s *Store) CreateCourse(facultyID int64, name, code, description string) (*Course, error) {
	name, code, description = strings.TrimSpace(name), strings.TrimSpace(strings.ToUpper(code)), strings.TrimSpace(description)
	if name == "" || code == "" {
		return nil, fmt.Errorf("%w: name and code are required", ErrInvalid)
	}
	res, err := s.DB.Exec(`INSERT INTO courses (faculty_id, name, code, description) VALUES (?, ?, ?, ?)`,
		facultyID, name, code, description)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: course code %q is already used", ErrDuplicate, code)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.CourseByID(id)
}

// UpdateCourse edits a course after ownership verification.
func (s *Store) UpdateCourse(id, facultyID int64, name, code, description string) error {
	c, err := s.CourseByID(id)
	if err != nil {
		return err
	}
	if c.FacultyID != facultyID {
		return ErrForbidden
	}
	name, code, description = strings.TrimSpace(name), strings.TrimSpace(strings.ToUpper(code)), strings.TrimSpace(description)
	if name == "" || code == "" {
		return fmt.Errorf("%w: name and code are required", ErrInvalid)
	}
	_, err = s.DB.Exec(`UPDATE courses SET name = ?, code = ?, description = ? WHERE id = ?`,
		name, code, description, id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: course code %q is already used", ErrDuplicate, code)
	}
	return err
}

// CourseByID loads one course with aggregate counts.
func (s *Store) CourseByID(id int64) (*Course, error) {
	c := &Course{}
	err := s.DB.QueryRow(`SELECT c.id, c.faculty_id, u.name, c.name, c.code, COALESCE(c.description,''),
		c.created_at,
		(SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id),
		(SELECT COUNT(*) FROM assignments a WHERE a.course_id = c.id)
		FROM courses c JOIN users u ON u.id = c.faculty_id WHERE c.id = ?`, id).
		Scan(&c.ID, &c.FacultyID, &c.FacultyName, &c.Name, &c.Code, &c.Description, &c.CreatedAt, &c.Students, &c.Assignments)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// CoursesByFaculty lists a faculty member's courses with counts.
func (s *Store) CoursesByFaculty(facultyID int64) ([]*Course, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.faculty_id, c.name, c.code, COALESCE(c.description,''), c.created_at,
		(SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id),
		(SELECT COUNT(*) FROM assignments a WHERE a.course_id = c.id)
		FROM courses c WHERE c.faculty_id = ? ORDER BY c.name`, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Course
	for rows.Next() {
		c := &Course{}
		if err := rows.Scan(&c.ID, &c.FacultyID, &c.Name, &c.Code, &c.Description, &c.CreatedAt, &c.Students, &c.Assignments); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CoursesByStudent lists courses a student is enrolled in.
func (s *Store) CoursesByStudent(studentID int64) ([]*Course, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.faculty_id, u.name, c.name, c.code, COALESCE(c.description,''), c.created_at,
		(SELECT COUNT(*) FROM enrollments e2 WHERE e2.course_id = c.id),
		(SELECT COUNT(*) FROM assignments a WHERE a.course_id = c.id AND a.published = 1)
		FROM courses c
		JOIN enrollments e ON e.course_id = c.id AND e.student_id = ?
		JOIN users u ON u.id = c.faculty_id
		ORDER BY c.name`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Course
	for rows.Next() {
		c := &Course{}
		if err := rows.Scan(&c.ID, &c.FacultyID, &c.FacultyName, &c.Name, &c.Code, &c.Description, &c.CreatedAt, &c.Students, &c.Assignments); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FacultyOwnsCourse reports whether the faculty member owns the course.
func (s *Store) FacultyOwnsCourse(courseID, facultyID int64) (bool, error) {
	var fid int64
	err := s.DB.QueryRow(`SELECT faculty_id FROM courses WHERE id = ?`, courseID).Scan(&fid)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return fid == facultyID, err
}

// CourseAccessFor resolves what a given user may see on a course.
// For students it is true only when they are enrolled.
func (s *Store) CourseAccessFor(courseID, userID int64, role string) (bool, error) {
	switch role {
	case "FACULTY":
		return s.FacultyOwnsCourse(courseID, userID)
	case "STUDENT":
		ok, err := s.Enrolled(courseID, userID)
		return ok, err
	}
	return false, ErrForbidden
}

// ---------- Enrollment ----------

// StudentRow is a student in a listing.
type StudentRow struct {
	ID            int64
	Name          string
	Email         string
	CoursesJoined bool
}

// StudentsOfCourse lists enrolled students for ownership-verified callers.
func (s *Store) StudentsOfCourse(courseID, facultyID int64) ([]*StudentRow, error) {
	if ok, err := s.FacultyOwnsCourse(courseID, facultyID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrForbidden
	}
	rows, err := s.DB.Query(`SELECT u.id, u.name, u.email FROM enrollments e
		JOIN users u ON u.id = e.student_id WHERE e.course_id = ? ORDER BY u.name`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStudents(rows)
}

func collectStudents(rows *sql.Rows) ([]*StudentRow, error) {
	var out []*StudentRow
	for rows.Next() {
		r := &StudentRow{}
		if err := rows.Scan(&r.ID, &r.Name, &r.Email); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Enrolled reports whether the student is enrolled in the course.
func (s *Store) Enrolled(courseID, studentID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(1) FROM enrollments WHERE course_id = ? AND student_id = ?`,
		courseID, studentID).Scan(&n)
	return n > 0, err
}

// Enroll adds an existing student to a course; duplicates rejected.
func (s *Store) Enroll(courseID, studentID, facultyID int64) error {
	ok, err := s.FacultyOwnsCourse(courseID, facultyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	var role string
	if err := s.DB.QueryRow(`SELECT role FROM users WHERE id = ?`, studentID).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if role != "STUDENT" {
		return fmt.Errorf("%w: only student accounts can be enrolled", ErrInvalid)
	}
	res, err := s.DB.Exec(`INSERT INTO enrollments (student_id, course_id) VALUES (?, ?)`, studentID, courseID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("%w: student is already enrolled in this course", ErrDuplicate)
		}
		return err
	}
	_ = res
	return nil
}

// Unenroll removes a student from an owned course.
func (s *Store) Unenroll(courseID, studentID, facultyID int64) error {
	ok, err := s.FacultyOwnsCourse(courseID, facultyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	res, err := s.DB.Exec(`DELETE FROM enrollments WHERE course_id = ? AND student_id = ?`, courseID, studentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Accounts (faculty-managed student creation) ----------

// StudentUser is a minimal student profile view (no hash exposure).
type StudentUser struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// UsersModel forwards to the models package for template compatibility.
var UsersModel = models.User{}

// EnrolledStudents lists students enrolled in any of the faculty's courses.
func (s *Store) EnrolledStudents(facultyID int64) ([]*StudentRow, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT u.id, u.name, u.email FROM enrollments e
		JOIN users u ON u.id = e.student_id
		WHERE e.course_id IN (SELECT id FROM courses WHERE faculty_id = ?)
		ORDER BY u.name`, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStudents(rows)
}

// StudentByID fetches a student profile by ID.
func (s *Store) StudentByID(id int64) (*StudentUser, error) {
	r := &StudentUser{}
	err := s.DB.QueryRow(`SELECT id, name, email, role FROM users WHERE id = ? AND role = 'STUDENT'`, id).
		Scan(&r.ID, &r.Name, &r.Email, &r.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// UnusedTime is a compile-time reference kept out of the hot path.
var _ = TimeLayout
