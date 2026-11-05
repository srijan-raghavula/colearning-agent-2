package store

import (
	"strings"
	"testing"
	"time"
)

// Regression for the QA screenshot: the first two "Students requiring
// attention" rows showed blank names/emails. Root cause was identity lookups
// scoped to approved-evaluation owners only, so students flagged purely for
// missing work rendered blank. Every attention row must carry the correct,
// independent student identity, and several missing-work students must each
// appear separately.
func TestAttentionListIdentityCompleteness(t *testing.T) {
	s := tmpDB(t)
	f1, sid1 := facultyAndStudent(s, t)

	// Two courses with published, now-overdue assignments.
	c1, err := s.CreateCourse(f1, "A", "A1", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateCourse(f1, "B", "B1", "")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-72 * time.Hour).Format(TimeLayout)
	future := time.Now().UTC().Add(72 * time.Hour).Format(TimeLayout)
	a1, err := s.CreateAssignment(f1, c1.ID, "OverdueGraph", "d", "graphs", past, 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.CreateAssignment(f1, c2.ID, "OverdueSQL", "d", "sql", future, 100, criteria100())
	if err != nil {
		t.Fatal(err)
	}

	// Three extra students: s_a misses c1 work (has approved result on c2),
	// s_b misses both pieces of work (no evaluations at all), s_c has no gaps.
	mkStudent := func(email, name string) int64 {
		var id int64
		if err := s.DB.QueryRow(`INSERT INTO users (email, name, password_hash, role) VALUES (?, ?, 'x', 'STUDENT')
			RETURNING id`, email, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	sa, sb, sc := mkStudent("sa@t.edu", "Alpha Student"), mkStudent("sb@t.edu", "Beta Student"), mkStudent("sc@t.edu", "Sena Good")
	// Enroll everyone in both courses.
	for cid := range []int64{c1.ID, c2.ID} {
		cid := int64(cid) + 1
		for _, st := range []int64{sid1, sa, sb, sc} {
			if err := s.Enroll(cid, st, f1); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, arr := range []*Assignment{a1, a2} {
		if err := s.PublishAssignment(arr.ID, f1); err != nil {
			t.Fatal(err)
		}
	}
	// s_c submits both; s_a submits only c2's work (so their mastery topic is
	// known and they miss one assignment); s_b submits nothing.
	if _, err := s.CreateOrUpdateSubmission(a1.ID, sc, "good answer "+demoGraphText(), nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrUpdateSubmission(a2.ID, sc, "good answer for sql too", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrUpdateSubmission(a2.ID, sa, "partial answer", nil, ""); err != nil {
		t.Fatal(err)
	}
	_ = a2

	rows, err := s.AttentionList(f1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("attention list empty — missing-work detection broken")
	}
	byStudent := map[int64]*AttentionRow{}
	for _, r := range rows {
		if r.StudentName == "" || r.StudentEmail == "" {
			t.Fatalf("blank identity in attention row: %+v", r)
		}
		if _, dup := byStudent[r.StudentID]; dup {
			t.Fatalf("duplicate student row: %s", r.StudentName)
		}
		byStudent[r.StudentID] = r
	}
	// s_b missed two work items → must appear with its own correct identity.
	if r, ok := byStudent[sb]; !ok {
		t.Fatalf("missing-work student absent from the list: rows=%+v", rows)
	} else if r.StudentName != "Beta Student" || r.StudentEmail != "sb@t.edu" {
		t.Fatalf("wrong identity on missing-work row: %+v", r)
	}
	// s_a (one missing) must NOT be flagged by the missing rule (missing < 2)
	if r, ok := byStudent[sa]; ok && strings.Contains(r.Reason, "Missing work") {
		t.Fatalf("student with a single miss incorrectly listed for missing work: %+v", r)
	}
	// s_c (clean) must not appear.
	if r, ok := byStudent[sc]; ok {
		t.Fatalf("clean student flagged: %+v", r)
	}
	// Every row's course/code must match a course owned by this faculty.
	for _, r := range rows {
		if r.CourseCode != "A1" && r.CourseCode != "B1" {
			t.Fatalf("row references a foreign course: %+v", r)
		}
	}
}

func demoGraphText() string { return "BFS uses a queue; DFS a stack." }
