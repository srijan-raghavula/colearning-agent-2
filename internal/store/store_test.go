package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCourseCRUDAndOwnership(t *testing.T) {
	s := tmpDB(t)
	f1, _ := facultyAndStudent(s, t)

	// Create
	c, err := s.CreateCourse(f1, "Data Structures", "cs201", "desc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Code != "CS201" {
		t.Fatalf("code not uppercased: %q", c.Code)
	}
	if c.Students != 0 || c.Assignments != 0 {
		t.Fatalf("bad counts: %+v", c)
	}

	// Edit
	if err := s.UpdateCourse(c.ID, f1, "Data Structures II", "CS202", "more"); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Second faculty exists on the other DB; verify self-ownership checks here
	// via a nonexistent faculty id.
	if err := s.UpdateCourse(c.ID, 999, "Hijacked", "X", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if got, err := s.CourseByID(c.ID); err != nil || got.Name != "Data Structures II" {
		t.Fatalf("update lost: %+v %v", got, err)
	}

	// Duplicate code rejected
	if _, err := s.CreateCourse(f1, "Other", "CS202", ""); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup course: %v", err)
	}
	// Missing fields rejected
	if _, err := s.CreateCourse(f1, "", "XX", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no name: %v", err)
	}
}

func TestStudentAccountCreation(t *testing.T) {
	s := tmpDB(t)
	if _, err := s.CreateStudentAccount("New.Student@Edu.demo ", "Neo", "password9x"); err != nil {
		t.Fatalf("valid creation failed: %v", err)
	}
	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, "new.student@edu.demo").Scan(&hash); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if hash == "password9x" || len(hash) < 50 {
		t.Fatalf("password not stored as bcrypt hash")
	}
	if _, err := s.CreateStudentAccount("new.student@edu.demo", "Dup", "password9x"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate not rejected: %v", err)
	}
	for _, bad := range []struct{ email, pass string }{
		{"no-at-sign", "password9x"}, {"x@y.z", "short"}, {"x@y.z", "alllowercase"}, {"x@y.z", ""},
	} {
		if _, err := s.CreateStudentAccount(bad.email, "N", bad.pass); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad account accepted: %+v err=%v", bad, err)
		}
	}
}

func TestEnrollmentRules(t *testing.T) {
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	c, _ := s.CreateCourse(f1, "DS", "DS1", "")

	// enroll ok
	if err := s.Enroll(c.ID, student, f1); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	// duplicate rejected
	if err := s.Enroll(c.ID, student, f1); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup enrollment allowed: %v", err)
	}
	// other faculty cannot drive enrollment
	if err := s.Enroll(c.ID, student, 999); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign faculty enrolled: %v", err)
	}
	// students cannot see un-enrolled course
	ok, _ := s.CourseAccessFor(c.ID, student, "STUDENT")
	if !ok {
		t.Fatalf("enrolled student denied own course")
	}
	// a student not enrolled must be denied
	var sid2 int64
	_ = s.DB.QueryRow(`SELECT id FROM users WHERE email = 's2@x.edu'`).Scan(&sid2)
	ok, _ = s.CourseAccessFor(c.ID, sid2, "STUDENT")
	if ok {
		t.Fatalf("un-enrolled student allowed into course")
	}
	// enrollment by an invalid role rejected
	var fid = f1
	if err := s.Enroll(c.ID, fid, f1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("faculty enrolled as student: %v", err)
	}
	// removal
	if err := s.Unenroll(c.ID, student, f1); err != nil {
		t.Fatalf("unenroll: %v", err)
	}
	if err := s.Unenroll(c.ID, student, f1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double unenroll: %v", err)
	}
}

func TestAssignmentRubricValidationLifecycle(t *testing.T) {
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	c, _ := s.CreateCourse(f1, "DS", "DS2", "")
	if err := s.Enroll(c.ID, student, f1); err != nil {
		t.Fatal(err)
	}
	due := "2026-12-01T15:00"

	// Draft with partial rubric is fine.
	a, err := s.CreateAssignment(f1, c.ID, "Graph Traversal", "Do BFS & DFS", "Graphs", due, 100,
		[]*RubricCriterion{{Title: "Correctness", Description: "right algorithm", MaxPoints: 50}})
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	if a.Published {
		t.Fatalf("created as published")
	}

	// Publish with incomplete rubric must fail.
	if err := s.PublishAssignment(a.ID, f1); err == nil || !strings.Contains(err.Error(), "100") {
		t.Fatalf("published with rubric %d: %v", a.RubricTotal, err)
	}

	// Complete the rubric through ReplaceCriteria (transaction).
	full := criteria100()
	if err := s.ReplaceCriteria(a.ID, f1, full); err != nil {
		t.Fatalf("replace criteria: %v", err)
	}
	got, _ := s.AssignmentByID(a.ID)
	if got.RubricTotal != 100 || len(got.Criteria) != 2 {
		t.Fatalf("rubric not replaced: %d pts %d criteria", got.RubricTotal, len(got.Criteria))
	}

	// Invalid criteria rejected (zero points).
	if err := s.ReplaceCriteria(a.ID, f1, []*RubricCriterion{{Title: "X", Description: "Y", MaxPoints: 0}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero-max criterion accepted: %v", err)
	}
	// Empty names rejected.
	if err := s.ReplaceCriteria(a.ID, f1, []*RubricCriterion{{Title: " ", Description: "Y", MaxPoints: 100}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty title accepted: %v", err)
	}
	// Wrong-total rubric rejected only by publish rule; total 90 here.
	if err := s.ReplaceCriteria(a.ID, f1, []*RubricCriterion{
		{Title: "A", Description: "a", MaxPoints: 90},
	}); err == nil {
		// allowed while drafting
	} else {
		t.Fatalf("draft rubric with total 90 rejected: %v", err)
	}
	if err := s.PublishAssignment(a.ID, f1); err == nil {
		t.Fatalf("publish succeeded with rubric!=100")
	}
	if err := s.ReplaceCriteria(a.ID, f1, criteria100()); err != nil {
		t.Fatal(err)
	}
	// Ownership: foreign faculty cannot publish or replace.
	if err := s.PublishAssignment(a.ID, 999); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign publish: %v", err)
	}
	if err := s.PublishAssignment(a.ID, f1); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Published assignments are locked for rubric edits.
	if err := s.ReplaceCriteria(a.ID, f1, full); !errors.Is(err, ErrForbidden) {
		t.Fatalf("published rubric editable: %v", err)
	}
	// Only published assignments visible to students.
	visible, _ := s.PublishedAssignmentsByStudent(student)
	if len(visible) != 1 {
		t.Fatalf("student sees %d published", len(visible))
	}
}

func TestSubmissionsAndDeadlines(t *testing.T) {
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	c, _ := s.CreateCourse(f1, "DS", "DS3", "")

	// Deadline two days in the future.
	future := time.Now().UTC().Add(48 * time.Hour).Format(TimeLayoutInput)
	a, err := s.CreateAssignment(f1, c.ID, "T1", "d", "graph", future, 100, criteria100())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.PublishAssignment(a.ID, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.Enroll(c.ID, student, f1); err != nil {
		t.Fatal(err)
	}

	// Unauthorized student cannot submit.
	var outsider int64
	_ = s.DB.QueryRow(`SELECT id FROM users WHERE email = 's2@x.edu'`).Scan(&outsider)
	if _, err := s.CreateOrUpdateSubmission(a.ID, outsider, "hi", nil, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider submitted: %v", err)
	}
	// Empty submission rejected.
	if _, err := s.CreateOrUpdateSubmission(a.ID, student, "  ", nil, ""); err == nil {
		t.Fatalf("empty submission accepted")
	}
	// Text submission persists, status submitted, not late.
	res, err := s.CreateOrUpdateSubmission(a.ID, student, "My answer", nil, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Late {
		t.Fatalf("future deadline marked late")
	}
	sub, _ := s.SubmissionFor(a.ID, student)
	if sub.Content != "My answer" || sub.Status != "SUBMITTED" {
		t.Fatalf("bad submission: %+v", sub)
	}

	// Roster reflects 1 enrolled / 1 submitted.
	counts, err := s.AssignmentCounts(a)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Enrolled != 1 || counts.Submitted != 1 || counts.NotSubmitted != 0 || counts.Overdue != 0 {
		t.Fatalf("counts: %+v", counts)
	}
	if counts.Completion != 100 {
		t.Fatalf("completion: %f", counts.Completion)
	}

	// Deadline expiry is dynamic: an assignment created with a past due date.
	past := time.Now().UTC().Add(-48 * time.Hour).Format(TimeLayoutInput)
	a2, err := s.CreateAssignment(f1, c.ID, "T2", "d", "graph", past, 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishAssignment(a2.ID, f1); err != nil {
		t.Fatal(err)
	}
	counts2, _ := s.AssignmentCounts(a2)
	if counts2.Enrolled != 1 || counts2.NotSubmitted != 1 || counts2.Overdue != 1 {
		t.Fatalf("overdue counts: %+v", counts2)
	}
	// For the late resubmission case: separate past-due assignment, submit late.
	a3, err := s.CreateAssignment(f1, c.ID, "T3", "d", "graph", past, 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishAssignment(a3.ID, f1); err != nil {
		t.Fatal(err)
	}
	res2, err := s.CreateOrUpdateSubmission(a3.ID, student, "Late answer", nil, "")
	if err != nil {
		t.Fatalf("late submit: %v", err)
	}
	if !res2.Late {
		t.Fatalf("late submission not flagged")
	}
	// status remains SUBMITTED with late indicator, not a persisted OVERDUE.
	st, _, late := s.SubmissionStatusFor(a3, student)
	if st != "SUBMITTED" || !late {
		t.Fatalf("status: %s late=%v", st, late)
	}
	var dbStatus string
	if err := s.DB.QueryRow(`SELECT status FROM submissions WHERE assignment_id = ?`, a3.ID).Scan(&dbStatus); err != nil || dbStatus != "SUBMITTED" {
		t.Fatalf("overdue must not be persisted: %q err=%v", dbStatus, err)
	}
}

func TestSaveUploadPolicy(t *testing.T) {
	s := tmpDB(t)

	// accepted txt
	r := strings.NewReader("hello text")
	sf, err := s.SaveSubmissionFile("notes.txt", r, 10)
	if err != nil {
		t.Fatalf("txt: %v", err)
	}
	if sf.ClientName != "notes.txt" || sf.ServerName == "notes.txt" {
		t.Fatalf("bad names: %+v", sf)
	}
	if !isStoredName(sf.ServerName) {
		t.Fatalf("server name not sanitized shape: %s", sf.ServerName)
	}
	// Stored on disk
	f, err := s.OpenStoredFile(sf.ServerName)
	if err != nil {
		t.Fatalf("open stored: %v", err)
	}
	f.Close()

	// traversal attempts rejected
	for _, bad := range []string{"../etc/passwd", "a/b.txt", "..", "x/../../y.txt"} {
		if _, err := s.OpenStoredFile(bad); !errors.Is(err, ErrNotFound) {
			t.Fatalf("traversal allowed: %q -> %v", bad, err)
		}
	}
	// disallowed extension
	if _, err := s.SaveSubmissionFile("script.exe", strings.NewReader("MZ"), 2); err == nil {
		t.Fatalf("exe accepted")
	}
	// extension/content mismatch
	if _, err := s.SaveSubmissionFile("doc.docx", strings.NewReader("plain text really"), 16); err == nil {
		t.Fatalf("mismatched docx accepted")
	}
	// oversized rejected
	if _, err := s.SaveSubmissionFile("big.txt", strings.NewReader(string(make([]byte, 10<<20+5))), 10<<20+5); err == nil {
		t.Fatalf("oversized accepted")
	}
	// empty rejected
	if _, err := s.SaveSubmissionFile("e.txt", strings.NewReader(""), 0); err == nil {
		t.Fatalf("empty accepted")
	}
	// filename with path traversal never touches storage path
	sf2, err := s.SaveSubmissionFile("../../evil.txt", strings.NewReader("data"), 4)
	if err != nil {
		t.Fatalf("traversal filename rejected: %v", err)
	}
	if strings.Contains(sf2.ServerName, "..") || strings.Contains(sf2.ClientName, "..") {
		t.Fatalf("client path leaked: %+v", sf2)
	}
}
