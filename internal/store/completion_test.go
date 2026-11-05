package store

import (
	"testing"
	"time"
)

// Exact conflicting scenario from the QA screenshots: a late submission must
// be "SUBMITTED + late" everywhere, never "overdue & not submitted" in one
// view and "submitted late" in another. Also covers 0%-edge completion rows
// (zero enrollments / partial / complete) reconciled with the faculty KPI.
func TestLateSubmissionConsistentStates(t *testing.T) {
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	due := time.Now().UTC().Add(-24 * time.Hour).Format(TimeLayout)
	cX, err := s.CreateCourse(f1, "DS", "DSX", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAssignment(f1, cX.ID, "Graph Traversal", "d", "graphs", due, 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Enroll(cX.ID, student, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishAssignment(a.ID, f1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrUpdateSubmission(a.ID, student, "late but real answer", nil, ""); err != nil {
		t.Fatal(err)
	}

	state, err := s.StudentAssignmentStates(student)
	if err != nil {
		t.Fatal(err)
	}
	st := state[a.ID]
	if st == nil {
		t.Fatal("state missing for enrolled assignment")
	}
	if st.Status != "SUBMITTED" || !st.Late || st.Overdue {
		t.Fatalf("late submission resolves wrong: status=%s late=%v overdue=%v", st.Status, st.Late, st.Overdue)
	}
	if !st.DuePast {
		t.Fatal("deadline should be past")
	}
	// counts view must agree
	counts, _ := s.AssignmentCounts(&Assignment{ID: a.ID})
	if counts.Submitted != 1 || counts.Overdue != 0 {
		t.Fatalf("roster counts contradict the resolver: %+v", counts)
	}
}

func TestCompletionCornerCases(t *testing.T) {
	s := tmpDB(t)
	f1, _ := facultyAndStudent(s, t)

	// Zero enrollments: expected 0, not "0/0 complete".
	c, cerr := s.CreateCourse(f1, "Empty", "E1", "")
	if cerr != nil {
		t.Fatal(cerr)
	}
	a, err := s.CreateAssignment(f1, c.ID, "Dangling", "d", "topic", future_t(), 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishAssignment(a.ID, f1); err != nil {
		t.Fatal(err)
	}
	counts, err := s.AssignmentCounts(&Assignment{ID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Enrolled != 0 || counts.Submitted != 0 {
		t.Fatalf("enrollment-free assignment shows activity: %+v", counts)
	}
	if counts.Completion != 0 {
		t.Fatalf("completion must be 0 (N/A state rendered separately)")
	}
}

func TestCompletionReconciliationAcrossCourses(t *testing.T) {
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	// Course A: 2 enrollments, both submitted → 100%
	c1, _ := s.CreateCourse(f1, "A", "A1", "")
	_ = student
	var s1, s2 int64
	_ = s.DB.QueryRow(`INSERT INTO users (email,name,password_hash,role) VALUES ('x1@t.edu','X1','x','STUDENT') RETURNING id`).Scan(&s1)
	_ = s.DB.QueryRow(`INSERT INTO users (email,name,password_hash,role) VALUES ('x2@t.edu','X2','x','STUDENT') RETURNING id`).Scan(&s2)
	if err := s.Enroll(c1.ID, s1, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.Enroll(c1.ID, s2, f1); err != nil {
		t.Fatal(err)
	}
	a1, _ := s.CreateAssignment(f1, c1.ID, "Full", "d", "t", future_t(), 100, criteria100())
	if err := s.PublishAssignment(a1.ID, f1); err != nil {
		t.Fatal(err)
	}
	// Course B: 2 enrollments, one submitted → 50%
	c2, _ := s.CreateCourse(f1, "B", "B1", "")
	if err := s.Enroll(c2.ID, s1, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.Enroll(c2.ID, s2, f1); err != nil {
		t.Fatal(err)
	}
	a2, _ := s.CreateAssignment(f1, c2.ID, "Half", "d", "t", future_t(), 100, criteria100())
	_ = s.PublishAssignment(a2.ID, f1)
	if _, err := s.CreateOrUpdateSubmission(a1.ID, s1, "text", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrUpdateSubmission(a1.ID, s2, "text2", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrUpdateSubmission(a2.ID, s1, "text", nil, ""); err != nil {
		t.Fatal(err)
	}

	rows, err := s.AssignmentCompletionRows(f1, 0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]*CompletionRow{}
	for _, rr := range rows {
		byID[rr.AssignmentID] = rr
	}
	full := byID[a1.ID]
	if full.Enrolled != 2 || full.Submitted != 2 || full.Percent != 100 {
		t.Fatalf("full assignment wrong: %+v", full)
	}
	half := byID[a2.ID]
	if half.Enrolled != 2 || half.Submitted != 1 || half.Percent != 50 {
		t.Fatalf("half assignment wrong: %+v", half)
	}
	// KPI must reconcile: unique enrolled students across faculty = 2, published = 2, submitted rows = 2
	k, err := s.FacultyKPIs(f1)
	if err != nil {
		t.Fatal(err)
	}
	if k.Assignments != 2 {
		t.Fatalf("published count: %d", k.Assignments)
	}
	// Per-assignment rows: a1 → 2, a2 → 1 = 3; KPI must match that sum.
	if k.Submitted != 3 || k.Expected != 4 {
		t.Fatalf("KPI submitted=%d expected=%d — does not reconcile with rows", k.Submitted, k.Expected)
	}
}

func future_t() string {
	return time.Now().UTC().Add(24 * time.Hour).Format(TimeLayout)
}
