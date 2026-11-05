package store

import (
	"testing"
	"time"

	"educore/internal/evaluator"
)

func setupEvalWorld(t *testing.T) (*Store, int64, int64, *Assignment, int64) {
	t.Helper()
	s := tmpDB(t)
	f1, student := facultyAndStudent(s, t)
	c, _ := s.CreateCourse(f1, "DS", "DS9", "")
	if err := s.Enroll(c.ID, student, f1); err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(24 * time.Hour).Format(TimeLayoutInput)
	a, err := s.CreateAssignment(f1, c.ID, "T", "d", "graphs", due, 100,
		[]*RubricCriterion{
			{Title: "Correctness", Description: "algo", MaxPoints: 60, Keywords: "queue,stack"},
			{Title: "Depth", Description: "analysis", MaxPoints: 40, Keywords: "vertices"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishAssignment(a.ID, f1); err != nil {
		t.Fatal(err)
	}
	futureRes, err := s.CreateOrUpdateSubmission(a.ID, student, "BFS uses a queue; DFS uses a stack. They visit vertices.", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, f1, student, a, futureRes.Submission.ID
}

func TestEnqueueClaimPersist(t *testing.T) {
	s, _, _, _, subID := setupEvalWorld(t)

	// A pending job exists after submission.
	jid, err := s.ClaimNextJob()
	if err != nil || jid == 0 {
		t.Fatalf("claim: %v job=%d", err, jid)
	}
	// claim is exclusive
	jid2, _ := s.ClaimNextJob()
	if jid2 != 0 {
		t.Fatalf("concurrent claim duplicated work (job %d)", jid2)
	}
	if got, _ := s.JobSubmission(jid); got != subID {
		t.Fatalf("job points to wrong submission")
	}

	in, err := s.BuildEvaluatorInput(subID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := evaluator.DemoEvaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PersistEvaluation(subID, *res); err != nil {
		t.Fatal(err)
	}
	s.CompleteJob(jid)

	ev, err := s.LatestEvaluation(subID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Method != evaluator.MethodDemo || ev.Confidence != evaluator.ConfLow {
		t.Fatalf("labels: %s %s", ev.Method, ev.Confidence)
	}
	if ev.Approved {
		t.Fatal("demo result must not auto-approve")
	}
	var evalStatus string
	_ = s.DB.QueryRow(`SELECT eval_status FROM submissions WHERE id = ?`, subID).Scan(&evalStatus)
	if evalStatus != EvalNeedsReview {
		t.Fatalf("eval_status = %s", evalStatus)
	}
	if len(ev.Criteria) != 2 || ev.TotalScore > 100 || ev.MaxTotal != 100 {
		t.Fatalf("criteria: %+v max=%.1f", ev.Criteria, ev.MaxTotal)
	}
}

func TestResubmissionSupersedesJobs(t *testing.T) {
	s, _, student, _, oldSub := setupEvalWorld(t)
	// outstanding job exists; resubmission must supersede it and enqueue again
	if _, err := s.CreateOrUpdateSubmission(
		func() int64 {
			var aid int64
			_ = s.DB.QueryRow(`SELECT assignment_id FROM submissions WHERE id = ?`, oldSub).Scan(&aid)
			return aid
		}(),
		student, "Updated answer version 2", nil, ""); err != nil {
		t.Fatal(err)
	}
	var superseded, pending int
	_ = s.DB.QueryRow(`SELECT COUNT(1) FROM evaluation_jobs WHERE status = 'SUPERSEDED'`).Scan(&superseded)
	_ = s.DB.QueryRow(`SELECT COUNT(1) FROM evaluation_jobs WHERE status = 'PENDING'`).Scan(&pending)
	if superseded < 1 || pending == 0 {
		t.Fatalf("resubmission queue state: superseded=%d pending=%d", superseded, pending)
	}
}

func TestRecoverStuckJobs(t *testing.T) {
	s, f1 := setupEvalWorld2(t)
	jid, _ := s.ClaimNextJob()
	if jid == 0 {
		t.Fatal("no job to interrupt")
	}
	// simulate crash: job stays RUNNING
	n, err := s.RecoverStuckJobs()
	if err != nil || n < 1 {
		t.Fatalf("recover: n=%d err=%v", n, err)
	}
	var status string
	_ = s.DB.QueryRow(`SELECT status FROM evaluation_jobs WHERE id = ?`, jid).Scan(&status)
	if status != "PENDING" {
		t.Fatalf("job not recovered: %s", status)
	}
	_ = f1
}

func setupEvalWorld2(t *testing.T) (*Store, int64) {
	s, f1, _, _, _ := setupEvalWorld(t)
	return s, f1
}

func TestFailedEvaluationRetainsSubmission(t *testing.T) {
	s, _, _, _, subID := setupEvalWorld(t)
	jid, _ := s.ClaimNextJob()
	s.FailJob(jid, "provider unreachable")
	var status, content string
	if err := s.DB.QueryRow(`SELECT eval_status, content FROM submissions WHERE id = ?`, subID).Scan(&status, &content); err != nil {
		t.Fatal(err)
	}
	if status != EvalFailed {
		t.Fatalf("eval status: %s", status)
	}
	if content == "" {
		t.Fatal("submission content lost on failure")
	}
	// retry path enqueues a new job
	if err := s.EnqueueEvaluation(subID); err != nil {
		t.Fatal(err)
	}
	jid2, _ := s.ClaimNextJob()
	if jid2 == 0 || jid2 == jid {
		t.Fatalf("retry did not create a fresh job (old=%d new=%d, must supersede old)", jid, jid2)
	}
}

func TestOverrideAndApprovalFlow(t *testing.T) {
	s, f1, _, _, subID := setupEvalWorld(t)
	jid, _ := s.ClaimNextJob()
	in, _ := s.BuildEvaluatorInput(subID)
	res, _ := evaluator.DemoEvaluate(in)
	evid, _ := s.PersistEvaluation(subID, *res)
	s.CompleteJob(jid)

	ev, _ := s.EvaluationAttemptByID(evid)
	// demo scores all criteria; override criterion 1
	c1 := ev.Criteria[0].CriterionID
	if err := s.OverrideCriterion(evid, f1, c1, 55.50, "better argument than demo saw"); err != nil {
		t.Fatalf("override: %v", err)
	}
	// out-of-range rejected
	if err := s.OverrideCriterion(evid, f1, c1, 999, ""); err == nil {
		t.Fatal("out-of-range override accepted")
	}
	// wrong faculty rejected
	if err := s.OverrideCriterion(evid, 12345, c1, 5, ""); err == nil {
		t.Fatal("foreign faculty override accepted")
	}

	ev, _ = s.EvaluationAttemptByID(evid)
	// original preserved: overrides separate from model points
	if ev.Criteria[0].ModelPoints == 0 {
		t.Fatalf("model points wiped (audit trail broken)")
	}
	if ev.Criteria[0].Override == nil || ev.Criteria[0].Override.Points != 55.50 {
		t.Fatalf("override missing: %+v", ev.Criteria[0].Override)
	}
	if ev.Criteria[0].EffectivePts != 55.50 {
		t.Fatalf("effective not applied: %.2f", ev.Criteria[0].EffectivePts)
	}

	// approve with the wrong faculty forbidden
	if _, err := s.ApproveEvaluation(evid, 999); err == nil {
		t.Fatal("foreign approval allowed")
	}
	total, err := s.ApproveEvaluation(evid, f1)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if total != 100.50 {
		var w float64
		_ = s.DB.QueryRow(`SELECT SUM(COALESCE(ov.awarded_points, cr.awarded_points)) FROM criterion_results cr
			LEFT JOIN criteria_overrides ov ON ov.evaluation_id = cr.evaluation_id AND ov.criterion_id = cr.criterion_id
			WHERE cr.evaluation_id = ?`, evid).Scan(&w)
		if total != w {
			t.Fatalf("approved total %.2f != effective %.2f", total, w)
		}
	}
	var approved int
	_ = s.DB.QueryRow(`SELECT approved FROM evaluations WHERE id = ?`, evid).Scan(&approved)
	if approved != 1 {
		t.Fatal("not approved")
	}
	var evalStatus string
	_ = s.DB.QueryRow(`SELECT eval_status FROM submissions WHERE id = ?`, subID).Scan(&evalStatus)
	if evalStatus != EvalApproved {
		t.Fatalf("submission status after approval: %s", evalStatus)
	}
}

// (kept for reference; the effective-vs-approved assertion lives in the test)

func TestManualEvaluationAndValidation(t *testing.T) {
	s, f1, _, a, subID := setupEvalWorld(t)
	// missing criterion rejected
	if _, err := s.ManualEvaluation(subID, f1, "", []ManualScore{{CriterionID: a.Criteria[0].ID, Points: 40}}); err == nil {
		t.Fatal("manual with missing criterion accepted")
	}
	// out-of-range rejected
	if _, err := s.ManualEvaluation(subID, f1, "", []ManualScore{
		{CriterionID: a.Criteria[0].ID, Points: 500},
		{CriterionID: a.Criteria[1].ID, Points: 10},
	}); err == nil {
		t.Fatal("manual over-max accepted")
	}
	// valid manual attempt
	evid, err := s.ManualEvaluation(subID, f1, "clear work", []ManualScore{
		{CriterionID: a.Criteria[0].ID, Points: 50, Feedback: "good"},
		{CriterionID: a.Criteria[1].ID, Points: 25.25},
	})
	if err != nil {
		t.Fatalf("manual: %v", err)
	}
	ev, _ := s.EvaluationAttemptByID(evid)
	if ev.Method != evaluator.MethodManual || ev.EffectiveTotal != 75.25 {
		t.Fatalf("manual result: method=%s total=%f", ev.Method, ev.EffectiveTotal)
	}
	if ev.TotalScore > 100 || ev.EffectiveTotal > 100 {
		t.Fatal("total > 100")
	}
}

func TestEvalOverviewOwnership(t *testing.T) {
	s, f1, _, _, _ := setupEvalWorld(t)
	rows, err := s.EvalOverview(f1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("overview rows: %d", len(rows))
	}
	// A second faculty's overview must be empty.
	var f2 int64
	if err := s.DB.QueryRow(`INSERT INTO users (email, name, password_hash, role) VALUES ('f2@x.edu','F2','x','FACULTY') RETURNING id`).Scan(&f2); err != nil {
		t.Fatal(err)
	}
	rows2, err := s.EvalOverview(f2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 0 {
		t.Fatalf("foreign faculty saw %d submissions", len(rows2))
	}
}
