package analytics

import (
	"testing"
)

func mkRows() []ApprovedResult {
	// Three topics; deliberately uneven per-student submission counts.
	return []ApprovedResult{
		{SubmissionID: 1, StudentID: 1, CourseID: 10, Topic: "Binary Trees", Assignment: "BT", Points: 90, MaxPoints: 100},
		{SubmissionID: 2, StudentID: 2, CourseID: 10, Topic: "Binary Trees", Assignment: "BT", Points: 50, MaxPoints: 100},
		{SubmissionID: 3, StudentID: 1, CourseID: 10, Topic: "Graph Traversal", Assignment: "GT", Points: 40, MaxPoints: 100},
		// Student 1 has 2 more approved results in "Binary Trees" topic (should not overweight)
		{SubmissionID: 4, StudentID: 1, CourseID: 10, Topic: "Binary Trees", Assignment: "BT2", Points: 100, MaxPoints: 100},
	}
}

func TestTopicMasteryStudentFirstRule(t *testing.T) {
	st := TopicMastery(mkRows())
	var bt *TopicStat
	var gt *TopicStat
	for i := range st {
		if st[i].Topic == "Binary Trees" {
			bt = &st[i]
		}
		if st[i].Topic == "Graph Traversal" {
			gt = &st[i]
		}
	}
	if bt == nil || gt == nil {
		t.Fatal("missing topics")
	}
	// BT per student: S1=95, S2=50 → avg 72.5 (not weighted by 3 vs 1 submissions)
	if bt.Average != 72.5 {
		t.Fatalf("BT average %.2f, want 72.5", bt.Average)
	}
	if bt.Students != 2 || bt.Evaluations != 3 {
		t.Fatalf("BT counts: %d/%d", bt.Students, bt.Evaluations)
	}
	if gt.Average != 40 || !gt.AlertLow {
		t.Fatalf("GT average %v", gt)
	}
}

func TestSubjectComprehension(t *testing.T) {
	cs := SubjectComprehension(mkRows(), 10)
	// S1: (90+40+100)/300*100 = 76.67; S2: 50 → mean 63.33
	if cs.Students != 2 {
		t.Fatalf("students %d", cs.Students)
	}
	if cs.Comprehension < 63.3 || cs.Comprehension > 63.35 {
		t.Fatalf("comprehension %.2f", cs.Comprehension)
	}
	// unsupported course yields zero, not invented values
	if empty := SubjectComprehension(mkRows(), 99); empty.Comprehension != 0 || empty.Students != 0 {
		t.Fatalf("invented metrics for empty course: %+v", empty)
	}
}

func TestStudentAverages(t *testing.T) {
	m := StudentAverages(mkRows())
	s1 := m[1]
	if s1.AvgPercent < 76.6 || s1.AvgPercent > 76.7 {
		t.Fatalf("avg %.2f", s1.AvgPercent)
	}
	if s1.Strongest != "Binary Trees" || s1.Weakest != "Graph Traversal" || s1.WeakestPct != 40 {
		t.Fatalf("insights: %+v", s1)
	}
}
