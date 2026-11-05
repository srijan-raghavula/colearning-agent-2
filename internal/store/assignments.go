package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RubricCriterion is one evaluation criterion of an assignment.
type RubricCriterion struct {
	ID           int64
	AssignmentID int64
	Position     int
	Title        string
	Description  string
	MaxPoints    int
	Topic        string
	Keywords     string
}

// Assignment is faculty-created coursework.
type Assignment struct {
	ID          int64
	CourseID    int64
	CourseName  string
	CourseCode  string
	FacultyID   int64
	Title       string
	Description string
	Topic       string
	DueDate     string // stored UTC "2006-01-02 15:04"
	Published   bool
	TotalMarks  int
	Submissions int
	createdAt   string
	Criteria    []*RubricCriterion
	RubricTotal int
}

const assignmentCols = `a.id, a.course_id, c.name, c.code, c.faculty_id, a.title, a.description, a.topic, a.due_date, a.published, a.total_marks,
	(SELECT COUNT(*) FROM submissions s WHERE s.assignment_id = a.id)`

func scanAssignment(scan func(...any) error) (*Assignment, error) {
	a := &Assignment{}
	if err := scan(&a.ID, &a.CourseID, &a.CourseName, &a.CourseCode, &a.FacultyID, &a.Title,
		&a.Description, &a.Topic, &a.DueDate, &a.Published, &a.TotalMarks, &a.Submissions); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

func (s *Store) loadCriteria(a *Assignment) error {
	rows, err := s.DB.Query(`SELECT id, assignment_id, position, title, description, max_points, topic, keywords
		FROM rubric_criteria WHERE assignment_id = ? ORDER BY position, id`, a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		cr := &RubricCriterion{}
		if err := rows.Scan(&cr.ID, &cr.AssignmentID, &cr.Position, &cr.Title, &cr.Description, &cr.MaxPoints, &cr.Topic, &cr.Keywords); err != nil {
			return err
		}
		a.Criteria = append(a.Criteria, cr)
		a.RubricTotal += cr.MaxPoints
	}
	return rows.Err()
}

// AssignmentByID loads an assignment with its rubric.
func (s *Store) AssignmentByID(id int64) (*Assignment, error) {
	a, err := scanAssignment(func(dest ...any) error {
		return s.DB.QueryRow(`SELECT `+assignmentCols+` FROM assignments a
			JOIN courses c ON c.id = a.course_id WHERE a.id = ?`, id).Scan(dest...)
	})
	if err != nil {
		return nil, err
	}
	return a, s.loadCriteria(a)
}

// AssignmentsByCourse lists all assignments of a course.
func (s *Store) AssignmentsByCourse(courseID int64) ([]*Assignment, error) {
	rows, err := s.DB.Query(`SELECT `+assignmentCols+` FROM assignments a
		JOIN courses c ON c.id = a.course_id WHERE a.course_id = ? ORDER BY a.due_date, a.id`, courseID)
	if err != nil {
		return nil, err
	}
	return s.collectAssignments(rows)
}

// AssignmentsByFaculty lists assignments across the faculty's courses.
func (s *Store) AssignmentsByFaculty(facultyID int64) ([]*Assignment, error) {
	rows, err := s.DB.Query(`SELECT `+assignmentCols+` FROM assignments a
		JOIN courses c ON c.id = a.course_id WHERE c.faculty_id = ? ORDER BY a.due_date, a.id`, facultyID)
	if err != nil {
		return nil, err
	}
	return s.collectAssignments(rows)
}

// PublishedAssignmentsByStudent lists published assignments of the student's courses.
func (s *Store) PublishedAssignmentsByStudent(studentID int64) ([]*Assignment, error) {
	rows, err := s.DB.Query(`SELECT `+assignmentCols+` FROM assignments a
		JOIN courses c ON c.id = a.course_id
		JOIN enrollments e ON e.course_id = a.course_id AND e.student_id = ?
		WHERE a.published = 1 ORDER BY a.due_date, a.id`, studentID)
	if err != nil {
		return nil, err
	}
	return s.collectAssignments(rows)
}

func (s *Store) collectAssignments(rows *sql.Rows) ([]*Assignment, error) {
	defer rows.Close()
	var out []*Assignment
	for rows.Next() {
		a, err := scanAssignment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // close before per-assignment sub-queries (single shared pool)
	for _, a := range out {
		if err := s.loadCriteria(a); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------- Assignment validation ----------

var ErrInvalidDueDate = fmt.Errorf("%w: invalid due date", ErrInvalid)

func validateAssignment(title, topic, dueInput string, totalMarks int) (time.Time, error) {
	if strings.TrimSpace(title) == "" {
		return time.Time{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	if totalMarks <= 0 || totalMarks > 1000 {
		return time.Time{}, fmt.Errorf("%w: total marks must be positive", ErrInvalid)
	}
	due, err := ParseDueInput(dueInput)
	if err != nil {
		return time.Time{}, ErrInvalidDueDate
	}
	_ = topic
	return due, nil
}

// CreateAssignment inserts a draft assignment in a transaction with criteria.
func (s *Store) CreateAssignment(facultyID int64, courseID int64, title, description, topic, dueInput string, totalMarks int, criteria []*RubricCriterion) (*Assignment, error) {
	if ok, err := s.FacultyOwnsCourse(courseID, facultyID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrForbidden
	}
	due, err := validateAssignment(title, topic, dueInput, totalMarks)
	if err != nil {
		return nil, err
	}
	if err := validateCriteria(criteria); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO assignments (course_id, title, description, topic, due_date, total_marks, published)
		VALUES (?, ?, ?, ?, ?, ?, 0)`,
		courseID, strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(topic),
		due.UTC().Format(TimeLayout), totalMarks)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := insertCriteria(tx, id, criteria); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.AssignmentByID(id)
}

// UpdateAssignment edits title/instructions/topic/due/total for a draft
// assignment owned by the faculty member. Published assignments are locked
// except through semi-structured re-approval (not in Phase 3).
func (s *Store) UpdateAssignment(id, facultyID int64, title, description, topic, dueInput string, totalMarks int, criteria []*RubricCriterion) error {
	a, err := s.AssignmentByID(id)
	if err != nil {
		return err
	}
	if a.FacultyID != facultyID {
		return ErrForbidden
	}
	if a.Published {
		return fmt.Errorf("%w: published assignments cannot be edited; unpublish first", ErrForbidden)
	}
	due, err := validateAssignment(title, topic, dueInput, totalMarks)
	if err != nil {
		return err
	}
	_ = criteria // criteria are updated through ReplaceCriteria for clarity
	_, err = s.DB.Exec(`UPDATE assignments SET title = ?, description = ?, topic = ?, due_date = ?, total_marks = ?
		WHERE id = ?`, strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(topic),
		due.UTC().Format(TimeLayout), totalMarks, id)
	return err
}

// ReplaceCriteria replaces the rubric of a draft assignment atomically.
func (s *Store) ReplaceCriteria(assignmentID, facultyID int64, criteria []*RubricCriterion) error {
	a, err := s.AssignmentByID(assignmentID)
	if err != nil {
		return err
	}
	if a.FacultyID != facultyID {
		return ErrForbidden
	}
	if a.Published {
		return fmt.Errorf("%w: rubric of a published assignment is locked", ErrForbidden)
	}
	if err := validateCriteria(criteria); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM rubric_criteria WHERE assignment_id = ?`, assignmentID); err != nil {
		return err
	}
	if err := insertCriteria(tx, assignmentID, criteria); err != nil {
		return err
	}
	return tx.Commit()
}

// ValidateRubric returns the rubric total and whether it is publish-ready.
func (s *Store) ValidateRubric(assignmentID, facultyID int64) (int, error) {
	a, err := s.AssignmentByID(assignmentID)
	if err != nil {
		return 0, err
	}
	if a.FacultyID != facultyID {
		return 0, ErrForbidden
	}
	return a.RubricTotal, nil
}

// PublishAssignment flips a draft to published if the rubric totals 100.
func (s *Store) PublishAssignment(id, facultyID int64) error {
	a, err := s.AssignmentByID(id)
	if err != nil {
		return err
	}
	if a.FacultyID != facultyID {
		return ErrForbidden
	}
	if a.Published {
		return fmt.Errorf("%w: assignment is already published", ErrInvalid)
	}
	if err := validateCriteriaTotalForPublish(a.Criteria); err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE assignments SET published = 1 WHERE id = ?`, id)
	return err
}

// DeleteAssignment removes a draft assignment (with cascade).
func (s *Store) DeleteAssignment(id, facultyID int64) error {
	a, err := s.AssignmentByID(id)
	if err != nil {
		return err
	}
	if a.FacultyID != facultyID {
		return ErrForbidden
	}
	if a.Published {
		return fmt.Errorf("%w: published assignments must be unpublished before deletion", ErrForbidden)
	}
	_, err = s.DB.Exec(`DELETE FROM assignments WHERE id = ?`, id)
	return err
}

// validateCriteria enforces naming, positivity, and total == 100 only when
// publishing; the value is validated at publication time, partial rubrics are
// allowed while drafting.
func validateCriteria(criteria []*RubricCriterion) error {
	for i, c := range criteria {
		if strings.TrimSpace(c.Title) == "" {
			return fmt.Errorf("%w: criterion %d: name is required", ErrInvalid, i+1)
		}
		if strings.TrimSpace(c.Description) == "" {
			return fmt.Errorf("%w: criterion %d: description is required", ErrInvalid, i+1)
		}
		if c.MaxPoints <= 0 {
			return fmt.Errorf("%w: criterion %d: maximum score must be positive", ErrInvalid, i+1)
		}
		c.Topic = strings.TrimSpace(c.Topic)
		c.Keywords = strings.TrimSpace(c.Keywords)
	}
	return nil
}

// validateCriteriaTotalForPublish enforces the 100-point publication rule.
func validateCriteriaTotalForPublish(criteria []*RubricCriterion) error {
	total := 0
	for _, c := range criteria {
		total += c.MaxPoints
	}
	if len(criteria) == 0 {
		return fmt.Errorf("%w: at least one criterion required", ErrInvalid)
	}
	if total != 100 {
		return fmt.Errorf("%w: rubric must total exactly 100 (got %d)", ErrInvalid, total)
	}
	return nil
}

func insertCriteria(tx *sql.Tx, assignmentID int64, criteria []*RubricCriterion) error {
	// Enforce candidate position ordering.
	for i, c := range criteria {
		c.Position = i + 1
		if _, err := tx.Exec(`INSERT INTO rubric_criteria (assignment_id, position, title, description, max_points, topic, keywords)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			assignmentID, c.Position, strings.TrimSpace(c.Title), strings.TrimSpace(c.Description), c.MaxPoints, c.Topic, c.Keywords); err != nil {
			return err
		}
	}
	return nil
}
