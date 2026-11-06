package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxChapterTitleRunes       = 120
	maxChapterDescriptionRunes = 1000
	maxChapterContentRunes     = 200000
	maxChapterPosition         = 10000
)

// Chapter is faculty-authored learning content within a course.
type Chapter struct {
	ID          int64
	CourseID    int64
	Title       string
	Description string
	Content     string
	Position    int
	Published   bool
	CreatedAt   string
	UpdatedAt   string
}

// ChaptersByCourseForFaculty lists chapters after verifying course ownership.
func (s *Store) ChaptersByCourseForFaculty(courseID, facultyID int64) ([]*Chapter, error) {
	if err := s.requireCourseOwner(courseID, facultyID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`SELECT id, course_id, title, description, content, position,
		published, created_at, updated_at
		FROM course_chapters WHERE course_id = ? ORDER BY position, id`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectChapters(rows)
}

// ChapterForFaculty loads a chapter only when it belongs to a faculty-owned course.
func (s *Store) ChapterForFaculty(courseID, chapterID, facultyID int64) (*Chapter, error) {
	if err := s.requireCourseOwner(courseID, facultyID); err != nil {
		return nil, err
	}
	return s.chapterByCourse(courseID, chapterID)
}

// PublishedChaptersForStudent lists published chapters only for an enrolled student.
func (s *Store) PublishedChaptersForStudent(courseID, studentID int64) ([]*Chapter, error) {
	rows, err := s.DB.Query(`SELECT ch.id, ch.course_id, ch.title, ch.description, ch.content,
		ch.position, ch.published, ch.created_at, ch.updated_at
		FROM course_chapters ch
		JOIN enrollments e ON e.course_id = ch.course_id AND e.student_id = ?
		JOIN users u ON u.id = e.student_id AND u.role = 'STUDENT'
		WHERE ch.course_id = ? AND ch.published = 1
		ORDER BY ch.position, ch.id`, studentID, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectChapters(rows)
}

// PublishedChapterForStudent resolves a single published chapter within an enrollment.
func (s *Store) PublishedChapterForStudent(courseID, chapterID, studentID int64) (*Chapter, error) {
	ch := &Chapter{}
	var published int
	err := s.DB.QueryRow(`SELECT ch.id, ch.course_id, ch.title, ch.description, ch.content,
		ch.position, ch.published, ch.created_at, ch.updated_at
		FROM course_chapters ch
		JOIN enrollments e ON e.course_id = ch.course_id AND e.student_id = ?
		JOIN users u ON u.id = e.student_id AND u.role = 'STUDENT'
		WHERE ch.course_id = ? AND ch.id = ? AND ch.published = 1`,
		studentID, courseID, chapterID).
		Scan(&ch.ID, &ch.CourseID, &ch.Title, &ch.Description, &ch.Content,
			&ch.Position, &published, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ch.Published = published == 1
	return ch, nil
}

// CreateChapter inserts a chapter after checking the faculty owner and all fields.
func (s *Store) CreateChapter(facultyID, courseID int64, title, description, content string, position int, published bool) (*Chapter, error) {
	if err := s.requireCourseOwner(courseID, facultyID); err != nil {
		return nil, err
	}
	title, description, content, err := validateChapter(title, description, content, position, published)
	if err != nil {
		return nil, err
	}
	now := NowUTC()
	res, err := s.DB.Exec(`INSERT INTO course_chapters
		(course_id, title, description, content, position, published, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		courseID, title, description, content, position, boolInt(published), now, now)
	if err != nil {
		return nil, fmt.Errorf("create chapter: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("resolve created chapter: %w", err)
	}
	return s.chapterByCourse(courseID, id)
}

// UpdateChapter saves chapter fields; publication is explicit and validated.
func (s *Store) UpdateChapter(facultyID, courseID, chapterID int64, title, description, content string, position int, published bool) error {
	if err := s.requireCourseOwner(courseID, facultyID); err != nil {
		return err
	}
	if _, err := s.chapterByCourse(courseID, chapterID); err != nil {
		return err
	}
	title, description, content, err := validateChapter(title, description, content, position, published)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(`UPDATE course_chapters
		SET title = ?, description = ?, content = ?, position = ?, published = ?, updated_at = ?
		WHERE id = ? AND course_id = ?`,
		title, description, content, position, boolInt(published), NowUTC(), chapterID, courseID)
	if err != nil {
		return fmt.Errorf("update chapter: %w", err)
	}
	updated, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve chapter update: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChapter removes a chapter after verifying the owning faculty member.
func (s *Store) DeleteChapter(facultyID, courseID, chapterID int64) error {
	if err := s.requireCourseOwner(courseID, facultyID); err != nil {
		return err
	}
	if _, err := s.chapterByCourse(courseID, chapterID); err != nil {
		return err
	}
	res, err := s.DB.Exec(`DELETE FROM course_chapters WHERE id = ? AND course_id = ?`, chapterID, courseID)
	if err != nil {
		return fmt.Errorf("delete chapter: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve chapter deletion: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) requireCourseOwner(courseID, facultyID int64) error {
	owned, err := s.FacultyOwnsCourse(courseID, facultyID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrForbidden
	}
	return nil
}

func (s *Store) chapterByCourse(courseID, chapterID int64) (*Chapter, error) {
	ch := &Chapter{}
	var published int
	err := s.DB.QueryRow(`SELECT id, course_id, title, description, content, position,
		published, created_at, updated_at
		FROM course_chapters WHERE course_id = ? AND id = ?`, courseID, chapterID).
		Scan(&ch.ID, &ch.CourseID, &ch.Title, &ch.Description, &ch.Content,
			&ch.Position, &published, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ch.Published = published == 1
	return ch, nil
}

func collectChapters(rows *sql.Rows) ([]*Chapter, error) {
	var out []*Chapter
	for rows.Next() {
		ch := &Chapter{}
		var published int
		if err := rows.Scan(&ch.ID, &ch.CourseID, &ch.Title, &ch.Description, &ch.Content,
			&ch.Position, &published, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Published = published == 1
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func validateChapter(title, description, content string, position int, published bool) (string, string, string, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		return "", "", "", fmt.Errorf("%w: chapter title is required", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > maxChapterTitleRunes {
		return "", "", "", fmt.Errorf("%w: chapter title must be at most %d characters", ErrInvalid, maxChapterTitleRunes)
	}
	if utf8.RuneCountInString(description) > maxChapterDescriptionRunes {
		return "", "", "", fmt.Errorf("%w: chapter description must be at most %d characters", ErrInvalid, maxChapterDescriptionRunes)
	}
	if utf8.RuneCountInString(content) > maxChapterContentRunes {
		return "", "", "", fmt.Errorf("%w: learning content must be at most %d characters", ErrInvalid, maxChapterContentRunes)
	}
	if position < 1 {
		return "", "", "", fmt.Errorf("%w: chapter order must be a positive number", ErrInvalid)
	}
	if position > maxChapterPosition {
		return "", "", "", fmt.Errorf("%w: chapter order must be %d or less", ErrInvalid, maxChapterPosition)
	}
	if published && strings.TrimSpace(content) == "" {
		return "", "", "", fmt.Errorf("%w: add learning content before publishing a chapter", ErrInvalid)
	}
	return title, description, content, nil
}
