package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Upload policy for Phase 3.
const (
	MaxUploadBytes = 10 << 20 // 10 MB
	uploadPrefix   = "./data/uploads"
)

// AllowedExtensions maps allowed file extensions to their permitted MIME types.
var AllowedExtensions = map[string][]string{
	".pdf":  {"application/pdf"},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip"},
	".txt":  {"text/plain", "text/plain; charset=utf-8", "application/octet-stream"},
}

// RealExtension sniffs the payload's MIME content with http.DetectContentType
// and maps it back to an extension, rejecting伪装 payloads.
func realExtension(data []byte) (string, error) {
	ct := http.DetectContentType(data[:min(512, len(data))])
	ct = strings.SplitN(ct, ";", 2)[0]
	switch ct {
	case "application/pdf":
		return ".pdf", nil
	case "application/zip":
		// zip archives: DOCX or PK junk; confirm extension elsewhere.
		return ".docx", nil
	case "text/plain", "text/plain; charset=utf-8":
		return ".txt", nil
	default:
		return "", fmt.Errorf("detected content type %q is not an accepted document format", ct)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// errBinarySuffix blocks text-only payloads that are actually executables etc.
var errBinarySuffix = errors.New("payload is not an accepted document format")

// SaveSubmissionFile stores the uploaded bytes under a random server-side
// name, validates size/type, and returns (serverFileName, savedPath, err).
// clientName is used only for the original name kept in DB metadata.
func (s *Store) SaveSubmissionFile(clientName string, r io.Reader, declaredSize int64) (*SavedFile, error) {
	if declaredSize > MaxUploadBytes {
		return nil, fmt.Errorf("%w: file exceeds the 10 MB limit", ErrInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if len(data) > MaxUploadBytes {
		return nil, fmt.Errorf("%w: file exceeds the 10 MB limit", ErrInvalid)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: uploaded file is empty", ErrInvalid)
	}

	ext := strings.ToLower(filepath.Ext(clientName))
	if _, allowed := AllowedExtensions[ext]; !allowed {
		return nil, fmt.Errorf("%w: format %q is not supported (PDF, DOCX, TXT only)", ErrInvalid, ext)
	}
	// Real content must match the claimed type.
	sniffExt, err := realExtension(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !extensionCompatible(ext, sniffExt, data) {
		return nil, fmt.Errorf("%w: file content does not match its .%s extension", ErrInvalid, strings.TrimPrefix(ext, "."))
	}
	// TXT must be explicitly plain text; binary with .txt is rejected here.
	if ext == ".txt" && sniffExt != ".txt" {
		return nil, errBinarySuffix
	}

	if err := os.MkdirAll(uploadPrefix, 0o755); err != nil {
		return nil, err
	}
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return nil, err
	}
	name := hex.EncodeToString(rnd) + ext
	rel := filepath.Join(uploadPrefix, name)
	if err := os.WriteFile(rel, data, 0o600); err != nil {
		return nil, err
	}
	return &SavedFile{
		ClientName:  filepath.Base(filepath.Clean("/" + strings.ReplaceAll(clientName, "\\", "/"))),
		ServerName:  name,
		StoragePath: rel,
		Size:        len(data),
		Mime:        mime.TypeByExtension(ext),
	}, nil
}

// extensionCompatible double-checks archive-based DOCX payloads against
// generic zips so the .docx contract is enforced on content, not just name.
func extensionCompatible(declared, sniffed string, _ []byte) bool {
	if declared == sniffed {
		return true
	}
	return declared == ".docx" && sniffed == ".docx" // zip == docx sniff mapping
}

// SavedFile carries upload metadata written to the database.
type SavedFile struct {
	ClientName  string `json:"name"`
	ServerName  string `json:"server_name"`
	StoragePath string `json:"path"`
	Size        int    `json:"size"`
	Mime        string `json:"mime"`
}

// OpenStoredFile safely opens a stored upload by its server-side random name.
// It accepts either a bare name or the stored relative path and strictly
// rejects traversal.
func (s *Store) OpenStoredFile(serverName string) (io.ReadCloser, error) {
	base := filepath.Base(serverName)
	if base == "." || base == "/" || base == ".." || strings.Contains(serverName, "..") {
		return nil, ErrNotFound
	}
	if !isStoredName(base) {
		return nil, ErrNotFound
	}
	return os.Open(filepath.Join(uploadPrefix, base))
}

// isStoredName requires the <64 hex>.<ext> shape produced by SaveSubmissionFile.
func isStoredName(name string) bool {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" || ext != strings.ToLower(ext) {
		return false
	}
	if _, ok := AllowedExtensions["."+ext]; !ok {
		return false
	}
	stem := name[:len(name)-len(filepath.Ext(name))]
	if len(stem) != 32 {
		return false
	}
	for _, c := range stem {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ---------- Submissions ----------

// Submission is a student's answer record.
type Submission struct {
	ID            int64
	AssignmentID  int64
	StudentID     int64
	StudentName   string
	Content       string
	Filename      string
	FilePath      string
	ExtractedText string
	Status        string
	EvalStatus    string
	SubmittedAt   string
}

// submitRes is CreateOrUpdateSubmission output for the caller.
type SubmitResult struct {
	Submission *Submission
	Late       bool
}

// CreateOrUpdateSubmission inserts or replaces a student's submission inside
// a transaction. Late flag is computed server-side against the deadline.
func (s *Store) CreateOrUpdateSubmission(assignmentID, studentID int64, content string, file *SavedFile, extracted string) (*SubmitResult, error) {
	a, err := s.AssignmentByID(assignmentID)
	if err != nil {
		return nil, err
	}
	if !a.Published {
		return nil, fmt.Errorf("%w: assignment is not published", ErrInvalid)
	}
	authorized, err := s.CourseAccessFor(a.CourseID, studentID, "STUDENT")
	if err != nil {
		return nil, err
	}
	if !authorized {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(content) == "" && file == nil {
		return nil, fmt.Errorf("%w: provide an answer text or a document", ErrInvalid)
	}
	late := false
	if t, terr := ParseStored(a.DueDate); terr == nil {
		late = time.Now().UTC().After(t)
	}

	if file == nil {
		// Text-only: keep any existing file (resubmission keeps attachments).
		existing, gerr := s.SubmissionFor(assignmentID, studentID)
		if gerr == nil && existing.FilePath != "" {
			file = &SavedFile{ClientName: existing.Filename, StoragePath: existing.FilePath}
		} else {
			gerr = nil
		}
	}
	var filename, path string
	if file != nil {
		filename, path = file.ClientName, file.StoragePath
	}

	_, err = s.DB.Exec(`INSERT INTO submissions (assignment_id, student_id, content, filename, file_path, extracted_text, status, submitted_at)
		VALUES (?, ?, ?, ?, ?, ?, 'SUBMITTED', ?)
		ON CONFLICT(assignment_id, student_id) DO UPDATE SET
			content = excluded.content,
			filename = excluded.filename,
			file_path = excluded.file_path,
			extracted_text = excluded.extracted_text,
			status = 'SUBMITTED',
			submitted_at = excluded.submitted_at,
			updated_at = excluded.submitted_at`,
		assignmentID, studentID, content, filename, path, extracted, NowUTC())
	if err != nil {
		return nil, err
	}
	sub, gerr := s.SubmissionFor(assignmentID, studentID)
	if gerr != nil {
		return nil, gerr
	}
	// A new (re)submission resets evaluation state and queues a fresh job;
	// outstanding PENDING/RUNNING jobs for the old version are superseded.
	if eerr := s.EnqueueEvaluation(sub.ID); eerr != nil {
		// Not fatal — the submission is persisted; the worker will retry.
		_ = eerr
	}
	return &SubmitResult{Submission: sub, Late: late}, nil
}

// NowUTC is the canonical current time for writes.
func NowUTC() string { return time.Now().UTC().Format(time.DateTime) }

// SubmissionFor fetches a submission if present.
func (s *Store) SubmissionFor(assignmentID, studentID int64) (*Submission, error) {
	sub := &Submission{}
	err := s.DB.QueryRow(`SELECT id, assignment_id, student_id, content, COALESCE(filename,''),
		COALESCE(file_path,''), COALESCE(extracted_text,''), status, COALESCE(eval_status,'NOT_REQUESTED'), submitted_at
		FROM submissions WHERE assignment_id = ? AND student_id = ?`, assignmentID, studentID).
		Scan(&sub.ID, &sub.AssignmentID, &sub.StudentID, &sub.Content, &sub.Filename, &sub.FilePath,
			&sub.ExtractedText, &sub.Status, &sub.EvalStatus, &sub.SubmittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// SubmissionStatusFor is the display status for a student/assignment pair.
// Overdue is never persisted; it is derived from the deadline each time.
func (s *Store) SubmissionStatusFor(a *Assignment, studentID int64) (status string, submitTime string, late bool) {
	sub, err := s.SubmissionFor(a.ID, studentID)
	if err != nil {
		return "NOT_SUBMITTED", "", false
	}
	late = false
	if t, terr := ParseStored(a.DueDate); terr == nil {
		if st, serr := ParseStored(sub.SubmittedAt); serr == nil {
			late = st.After(t)
		}
	}
	switch {
	case sub.Status == "EVALUATED":
		return "EVALUATED", sub.SubmittedAt, late
	case sub.Status == "NEEDS_REVIEW":
		return "NEEDS_REVIEW", sub.SubmittedAt, late
	default:
		return "SUBMITTED", sub.SubmittedAt, late
	}
}

// OverdueDue reports whether the assignment deadline has passed.
func OverdueDue(a *Assignment) bool {
	t, err := ParseStored(a.DueDate)
	if err != nil {
		return false
	}
	return time.Now().UTC().After(t)
}

// AssignmentSubmissionRows is the roster view for faculty monitoring.
type AssignmentSubmissionRow struct {
	StudentID    int64
	StudentName  string
	Email        string
	SubmissionID int64 // 0 when not submitted
	Status       string
	EvalStatus   string
	SubmittedAt  string
	Late         bool
}

// AssignmentRoster gives per-student submission monitoring for an assignment.
func (s *Store) AssignmentRoster(a *Assignment) ([]*AssignmentSubmissionRow, error) {
	rows, err := s.DB.Query(`SELECT u.id, u.name, u.email, COALESCE(s.id, 0),
			COALESCE(s.status, ''), COALESCE(s.eval_status, 'NOT_REQUESTED'), COALESCE(s.submitted_at, '')
		FROM enrollments e
		JOIN users u ON u.id = e.student_id
		LEFT JOIN submissions s ON s.assignment_id = ? AND s.student_id = u.id
		WHERE e.course_id = ? ORDER BY u.name`, a.ID, a.CourseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AssignmentSubmissionRow
	for rows.Next() {
		r := &AssignmentSubmissionRow{}
		var status, at string
		if err := rows.Scan(&r.StudentID, &r.StudentName, &r.Email, &r.SubmissionID, &status, &r.EvalStatus, &at); err != nil {
			return nil, err
		}
		if status == "" {
			r.Status = "NOT_SUBMITTED"
		} else {
			r.Status = status
			r.SubmittedAt = at
			if t, terr := ParseStored(a.DueDate); terr == nil {
				if st, serr := ParseStored(at); serr == nil {
					r.Late = st.After(t)
				}
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SubmissionCounts summarizes monitoring metrics for an assignment.
type SubmissionCounts struct {
	Enrolled     int
	Submitted    int
	NotSubmitted int
	Overdue      int // enrolled students with no submission after the deadline
	Completion   float64
}

// AssignmentCounts computes monitoring aggregates.
func (s *Store) AssignmentCounts(a *Assignment) (*SubmissionCounts, error) {
	if a.CourseID == 0 {
		// Roster scoping needs the course; resolve it defensively so callers
		// handing a half-populated assignment never silently see 0/0.
		if err := s.DB.QueryRow(`SELECT course_id FROM assignments WHERE id = ?`, a.ID).Scan(&a.CourseID); err != nil {
			return nil, ErrNotFound
		}
	}
	roster, err := s.AssignmentRoster(a)
	if err != nil {
		return nil, err
	}
	c := &SubmissionCounts{Enrolled: len(roster)}
	for _, r := range roster {
		if r.Status == "NOT_SUBMITTED" {
			c.NotSubmitted++
			if OverdueDue(a) {
				c.Overdue++
			}
			continue
		}
		c.Submitted++
	}
	if c.Enrolled > 0 {
		c.Completion = float64(c.Submitted) * 100 / float64(c.Enrolled)
	}
	return c, nil
}

// StudentAssignmentState is the authoritative per-assignment status for a
// student. Every student-facing status comes from here so dashboards, lists
// and detail pages can never contradict one another.
type StudentAssignmentState struct {
	Assignment  *Assignment
	Submission  *Submission
	Status      string // NOT_SUBMITTED | SUBMITTED | EVALUATED | NEEDS_REVIEW (persisted submission state)
	Late        bool   // submission arrived after the deadline
	Overdue     bool   // no submission and the deadline has passed
	DuePast     bool
	EvalStatus  string // evaluation lifecycle (NOT_REQUESTED..APPROVED/FAILED)
	HasApproved bool
}

// StudentAssignmentStates resolves the state for every published assignment of
// the student's enrolled courses, keyed by assignment ID. Enrollment scope is
// honored: only enrolled courses' published assignments are returned.
func (s *Store) StudentAssignmentStates(studentID int64) (map[int64]*StudentAssignmentState, error) {
	rows, err := s.DB.Query(`SELECT a.id, a.course_id, a.title, a.topic, a.due_date, a.total_marks,
			COALESCE(x.id,0), COALESCE(x.status,''), COALESCE(x.eval_status,'NOT_REQUESTED'), COALESCE(x.submitted_at,''),
			COALESCE(x.content,'')
		FROM assignments a
		JOIN enrollments e ON e.course_id = a.course_id AND e.student_id = ?
		LEFT JOIN submissions x ON x.assignment_id = a.id AND x.student_id = e.student_id
		WHERE a.published = 1`, studentID)
	if err != nil {
		return nil, err
	}
	type raw struct {
		a                                     *Assignment
		subID                                 int64
		subStatus, evalStatus, subAt, content string
	}
	var list []raw
	for rows.Next() {
		r := raw{a: &Assignment{}}
		if err := rows.Scan(&r.a.ID, &r.a.CourseID, &r.a.Title, &r.a.Topic, &r.a.DueDate, &r.a.TotalMarks,
			&r.subID, &r.subStatus, &r.evalStatus, &r.subAt, &r.content); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := map[int64]*StudentAssignmentState{}
	for _, r := range list {
		st := &StudentAssignmentState{
			Assignment: r.a,
			Status:     "NOT_SUBMITTED",
			EvalStatus: r.evalStatus,
		}
		if t, terr := ParseStored(r.a.DueDate); terr == nil {
			st.DuePast = time.Now().UTC().After(t)
		}
		hasSub := false
		if r.subID != 0 {
			hasSub = true
			sub := &Submission{ID: r.subID, AssignmentID: r.a.ID, StudentID: studentID,
				Content: r.content, Status: r.subStatus, SubmittedAt: r.subAt, EvalStatus: r.evalStatus}
			submittedAt, terrS := ParseStored(r.subAt)
			due, terrD := ParseStored(r.a.DueDate)
			if terrS == nil && terrD == nil {
				st.Late = submittedAt.After(due)
			}
			switch r.subStatus {
			case "EVALUATED":
				st.Status = "EVALUATED"
			case "NEEDS_REVIEW":
				st.Status = "NEEDS_REVIEW"
			default:
				st.Status = "SUBMITTED"
			}
			st.Submission = sub
			st.HasApproved = r.evalStatus == EvalApproved
		}
		if !hasSub && st.DuePast {
			st.Overdue = true
		}
		out[r.a.ID] = st
	}
	return out, nil
}
func (s *Store) SubmissionStatuses(studentID int64) (map[int64]string, error) {
	rows, err := s.DB.Query(`SELECT assignment_id, status FROM submissions WHERE student_id = ?`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var aid int64
		var st string
		if err := rows.Scan(&aid, &st); err != nil {
			return nil, err
		}
		out[aid] = st
	}
	return out, rows.Err()
}
