package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"educore/internal/evaluator"
)

// Evaluation lifecycle statuses on a submission.
const (
	EvalNotRequested = "NOT_REQUESTED"
	EvalPending      = "PENDING"
	EvalRunning      = "RUNNING"
	EvalNeedsReview  = "NEEDS_REVIEW"
	EvalApproved     = "APPROVED"
	EvalFailed       = "FAILED"
)

// EnqueueEvaluation supersedes outstanding jobs for the submission and
// creates a fresh PENDING job for the latest content.
func (s *Store) EnqueueEvaluation(submissionID int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE evaluation_jobs SET status = 'SUPERSEDED', updated_at = ?
		WHERE submission_id = ? AND status IN ('PENDING','RUNNING')`, NowUTC(), submissionID); err != nil {
		return err
	}
	now := NowUTC()
	if _, err := tx.Exec(`INSERT INTO evaluation_jobs (submission_id, status, created_at, updated_at) VALUES (?, 'PENDING', ?, ?)`,
		submissionID, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE submissions SET eval_status = ? WHERE id = ?`, EvalPending, submissionID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimNextJob atomically claims the oldest pending job for a worker.
func (s *Store) ClaimNextJob() (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var jobID int64
	err = tx.QueryRow(`SELECT id FROM evaluation_jobs WHERE status = 'PENDING' ORDER BY id LIMIT 1`).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // no work
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE evaluation_jobs SET status = 'RUNNING', tries = tries + 1, updated_at = ? WHERE id = ? AND status = 'PENDING'`, NowUTC(), jobID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return jobID, nil
}

// FailJob records a failure and moves the submission to FAILED (the
// submission itself and any prior attempts remain intact).
func (s *Store) FailJob(jobID int64, errSummary string) {
	_, _ = s.DB.Exec(`UPDATE evaluation_jobs SET status = 'FAILED', error = ?, updated_at = ? WHERE id = ?`,
		truncateString(errSummary, 500), NowUTC(), jobID)
	_, _ = s.DB.Exec(`UPDATE submissions SET eval_status = ? WHERE id = (SELECT submission_id FROM evaluation_jobs WHERE id = ?)`, EvalFailed, jobID)
}

// CompleteJob marks the queue row completed.
func (s *Store) CompleteJob(jobID int64) {
	_, _ = s.DB.Exec(`UPDATE evaluation_jobs SET status = 'COMPLETED', error = '', updated_at = ? WHERE id = ?`, NowUTC(), jobID)
}

// JobSubmission resolves the submission behind a job.
func (s *Store) JobSubmission(jobID int64) (int64, error) {
	var sid int64
	err := s.DB.QueryRow(`SELECT submission_id FROM evaluation_jobs WHERE id = ?`, jobID).Scan(&sid)
	return sid, err
}

// RecoverStuckJobs returns interrupted RUNNING jobs to PENDING at startup.
func (s *Store) RecoverStuckJobs() (int, error) {
	res, err := s.DB.Exec(`UPDATE evaluation_jobs SET status = 'PENDING', updated_at = ? WHERE status = 'RUNNING'`, NowUTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	// Any submission marked RUNNING without a live job goes back to PENDING.
	_, _ = s.DB.Exec(`UPDATE submissions SET eval_status = ? WHERE eval_status = 'RUNNING'
		AND NOT EXISTS (SELECT 1 FROM evaluation_jobs j WHERE j.submission_id = submissions.id AND j.status = 'RUNNING')`, EvalPending)
	return int(n), nil
}

// ---------- Engine input building ----------

// BuildEvaluatorInput assembles the engine input from the latest submission,
// its assignment, rubric and course. The submission's stored extracted text
// is preferred; EXTRACTION_FAILED markers disable it.
func (s *Store) BuildEvaluatorInput(submissionID int64) (evaluator.Input, error) {
	var in evaluator.Input
	raw, err := s.SubmissionRaw(submissionID)
	if err != nil {
		return in, err
	}
	a, err := s.AssignmentByID(raw.AssignmentID)
	if err != nil {
		return in, err
	}
	if !a.Published {
		return in, ErrForbidden
	}
	var course store_course
	if err := s.DB.QueryRow(`SELECT name FROM courses WHERE id = ?`, a.CourseID).Scan(&course.Name); err != nil {
		return in, err
	}
	extractionOK := !strings.HasPrefix(raw.ExtractedText, "EXTRACTION_FAILED")
	text := raw.Content
	if raw.ExtractedText != "" && extractionOK {
		text = raw.ExtractedText
	} else if raw.ExtractedText != "" && !extractionOK {
		// plain content is kept below; extraction failure is recorded elsewhere
	}
	if !extractionOK {
		if t := strings.TrimSpace(raw.Content); t != "" {
			text = t
		}
	}
	in.AssignmentID = a.ID
	in.SubmissionID = submissionID
	in.AssignmentTitle = a.Title
	in.Instructions = a.Description
	in.Topic = a.Topic
	in.CourseName = course.Name
	in.SubmissionText = text
	in.ExtractionOK = extractionOK
	for _, c := range a.Criteria {
		in.Criteria = append(in.Criteria, evaluator.CriterionInput{
			ID:          c.ID,
			Title:       c.Title,
			Description: c.Description,
			MaxPoints:   float64(c.MaxPoints),
			Topic:       c.Topic,
			Keywords:    c.Keywords,
		})
	}
	return in, nil
}

// rawSubmission is the internal payload for _and the input builder.
type store_course struct {
	Name string
}

// SubmissionRaw exposes the raw record to internal packages.
func (s *Store) SubmissionRaw(id int64) (*Submission, error) {
	sub := &Submission{}
	err := s.DB.QueryRow(`SELECT id, assignment_id, student_id, content, COALESCE(filename,''),
		COALESCE(file_path,''), COALESCE(extracted_text,''), status, COALESCE(eval_status,'NOT_REQUESTED'), submitted_at
		FROM submissions WHERE id = ?`, id).
		Scan(&sub.ID, &sub.AssignmentID, &sub.StudentID, &sub.Content, &sub.Filename, &sub.FilePath,
			&sub.ExtractedText, &sub.Status, &sub.EvalStatus, &sub.SubmittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// ---------- Persistence of results ----------

// PersistEvaluation stores one validated attempt transactionally: the
// evaluation row, its criterion results, and the queue outcome.
func (s *Store) PersistEvaluation(submissionID int64, res evaluator.Result) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(attempt),0)+1 FROM evaluations WHERE submission_id = ?`, submissionID).Scan(&next); err != nil {
		return 0, err
	}
	warnings, _ := json.Marshal(res.Warnings)
	if warnings == nil {
		warnings = []byte("[]")
	}
	total := roundScore25(res.TotalScore)
	if total > 100 {
		return 0, fmt.Errorf("total %.2f exceeds 100", total)
	}
	resev, err := tx.Exec(`INSERT INTO evaluations (submission_id, attempt, total_score, overall_feedback,
		strengths, learning_gaps, suggestions, confidence, method, approved, confidence_reason, warnings, extraction_ok)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		submissionID, next, total, res.OverallFeedback,
		joinJSON(res.Strengths), joinJSON(res.LearningGaps), joinJSON(res.Suggestions),
		res.Confidence, res.Method, res.ConfidenceReason, string(warnings), boolInt(!res.NeedsReview || extractionHadText(res)))
	if err != nil {
		return 0, err
	}
	evid, _ := resev.LastInsertId()
	for _, cr := range res.Criteria {
		evj, _ := json.Marshal(cr.Evidence)
		_, err := tx.Exec(`INSERT INTO criterion_results (evaluation_id, criterion_id, awarded_points, evidence, feedback, evidence_problems)
			VALUES (?, ?, ?, ?, ?, ?)`,
			evid, cr.CriterionID, cr.AwardedPoints, string(evj), cr.Feedback, len(cr.DroppedEvidence))
		if err != nil {
			return 0, err
		}
	}
	status := EvalNeedsReview
	if _, err := tx.Exec(`UPDATE submissions SET eval_status = ? WHERE id = ?`, status, submissionID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return evid, nil
}

// extractionHadText is unused currently; kept for clarity of extraction_ok.
func extractionHadText(_ evaluator.Result) bool { return true }

// ValidateManual validates faculty-entered scores with the same strict rules
// (no evidence: marks come from the faculty themselves).
func ValidateManual(in evaluator.Input, outs []evaluator.CriterionResultOut, overall string) (*evaluator.Result, error) {
	return evaluator.ValidateManualResult(in, outs, overall)
}

// roundScore25 clamps and rounds a total.
func roundScore25(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		v = 100
	}
	return evalRound(v)
}

func evalRound(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func joinJSON(list []string) string {
	b, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---------- Reading evaluations ----------

// CriterionView is one effective criterion row for UI/tests.
type CriterionView struct {
	CriterionID  int64
	Title        string
	Description  string
	MaxPoints    float64
	ModelPoints  float64
	Evidence     []evaluator.Evidence
	DroppedCount int
	Feedback     string
	Override     *OverrideView
	EffectivePts float64
}

// OverrideView records a faculty override.
type OverrideView struct {
	Points      float64
	Feedback    string
	FacultyName string
	CreatedAt   string
}

// EvaluationView is the review-facing aggregate.
type EvaluationView struct {
	ID               int64
	SubmissionID     int64
	Attempt          int
	TotalScore       float64 // model total (historical)
	EffectiveTotal   float64 // after overrides
	MaxTotal         float64
	OverallFeedback  string
	Strengths        []string
	LearningGaps     []string
	Suggestions      []string
	Confidence       string
	ConfidenceReason string
	Method           string
	Warnings         []evaluator.Warning
	ExtractionOK     bool
	Approved         bool
	ApprovedBy       string
	ApprovedAt       string
	CreatedAt        string
	Criteria         []*CriterionView
}

var _ = log.Printf

// LatestEvaluation loads the newest attempt for a submission (nil if none).
func (s *Store) LatestEvaluation(submissionID int64) (*EvaluationView, error) {
	return s.EvaluationAttempt(submissionID, 0)
}

// EvaluationAttempt loads a specific attempt (0 = latest).
func (s *Store) EvaluationAttempt(submissionID, attempt int64) (*EvaluationView, error) {
	query := `SELECT e.id, e.submission_id, e.attempt, e.total_score, e.overall_feedback, e.strengths, e.learning_gaps, e.suggestions,
		e.confidence, e.confidence_reason, e.method, e.approved, e.warnings, e.extraction_ok, e.created_at, e.approved_at,
		COALESCE(u.name,'')
		FROM evaluations e LEFT JOIN users u ON u.id = e.approved_by WHERE e.submission_id = ?`
	args := []any{submissionID}
	if attempt > 0 {
		query += " AND e.attempt = ?"
		args = append(args, attempt)
	} else {
		query += " ORDER BY e.attempt DESC LIMIT 1"
	}
	var v EvaluationView
	var strengths, gaps, sug, warnings string
	var approved int
	var extractionOK int
	err := s.DB.QueryRow(query, args...).Scan(&v.ID, &v.SubmissionID, &v.Attempt, &v.TotalScore, &v.OverallFeedback,
		&strengths, &gaps, &sug, &v.Confidence, &v.ConfidenceReason, &v.Method, &approved, &warnings,
		&extractionOK, &v.CreatedAt, &v.ApprovedAt, &v.ApprovedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Approved = approved == 1
	v.ExtractionOK = extractionOK == 1
	_ = json.Unmarshal([]byte(strengths), &v.Strengths)
	_ = json.Unmarshal([]byte(gaps), &v.LearningGaps)
	_ = json.Unmarshal([]byte(sug), &v.Suggestions)
	_ = json.Unmarshal([]byte(warnings), &v.Warnings)

	// Criteria with effective points (overrides overlay).
	rows, err := s.DB.Query(`SELECT cr.criterion_id, c.title, c.description, c.max_points, cr.awarded_points,
			COALESCE(cr.evidence,'[]'), cr.evidence_problems, COALESCE(cr.feedback,'')
		FROM criterion_results cr JOIN rubric_criteria c ON c.id = cr.criterion_id
		WHERE cr.evaluation_id = ? ORDER BY c.position`, v.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overridesTot := 0.0
	modelTot := 0.0
	for rows.Next() {
		cv := &CriterionView{}
		var evJSON string
		if err := rows.Scan(&cv.CriterionID, &cv.Title, &cv.Description, &cv.MaxPoints, &cv.ModelPoints, &evJSON, &cv.DroppedCount, &cv.Feedback); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evJSON), &cv.Evidence)
		cv.EffectivePts = cv.ModelPoints
		v.Criteria = append(v.Criteria, cv)
		v.MaxTotal += cv.MaxPoints
	}
	rows.Close()

	// Apply overrides.
	oRows, err := s.DB.Query(`SELECT o.criterion_id, o.awarded_points, o.faculty_feedback, o.created_at, COALESCE(u.name,'unknown')
		FROM criteria_overrides o LEFT JOIN users u ON u.id = o.faculty_id WHERE o.evaluation_id = ?`, v.ID)
	if err != nil {
		return nil, err
	}
	defer oRows.Close()
	byCrit := map[int64]*CriterionView{}
	for _, c := range v.Criteria {
		byCrit[c.CriterionID] = c
	}
	for oRows.Next() {
		var cid int64
		var ov OverrideView
		var pts float64
		if err := oRows.Scan(&cid, &pts, &ov.Feedback, &ov.CreatedAt, &ov.FacultyName); err != nil {
			return nil, err
		}
		if cv, ok := byCrit[cid]; ok {
			ov.Points = pts
			cv.Override = &ov
			cv.EffectivePts = pts
		}
	}
	for _, c := range v.Criteria {
		v.EffectiveTotal += c.EffectivePts
		modelTot += c.ModelPoints
		overridesTot += c.EffectivePts
	}
	v.TotalScore = evalRound(modelTot)
	v.EffectiveTotal = evalRound(overridesTot)
	return &v, nil
}

// ListEvaluationsByStatus summarizes faculty-facing evaluation overview rows.
type EvalSummaryRow struct {
	SubmissionID int64
	AssignmentID int64
	Assignment   string
	CourseCode   string
	StudentName  string
	EvalStatus   string
	Method       string
	Confidence   string
	Attempt      int
	Score        float64
	UpdatedAt    string
}

// EvalOverview lists the evaluation workflow states for one faculty member.
func (s *Store) EvalOverview(facultyID int64) ([]*EvalSummaryRow, error) {
	rows, err := s.DB.Query(`SELECT sub.id, a.id, a.title, c.code, u.name, COALESCE(sub.eval_status,'NOT_REQUESTED'),
			COALESCE(e.method,''), COALESCE(e.confidence,''), COALESCE(e.attempt,0), COALESCE(o.effective,0),
			COALESCE(j.updated_at, sub.submitted_at)
		FROM submissions sub
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id AND c.faculty_id = ?
		JOIN users u ON u.id = sub.student_id
		LEFT JOIN evaluations e ON e.id = (SELECT id FROM evaluations x WHERE x.submission_id = sub.id ORDER BY x.attempt DESC LIMIT 1)
		LEFT JOIN (
			SELECT cr.evaluation_id, SUM(COALESCE(ov.awarded_points, cr.awarded_points)) AS effective
			FROM criterion_results cr LEFT JOIN criteria_overrides ov ON ov.evaluation_id = cr.evaluation_id AND ov.criterion_id = cr.criterion_id
			GROUP BY cr.evaluation_id
		) o ON o.evaluation_id = e.id
		LEFT JOIN evaluation_jobs j ON j.id = (SELECT id FROM evaluation_jobs y WHERE y.submission_id = sub.id ORDER BY y.id DESC LIMIT 1)
		ORDER BY COALESCE(j.updated_at, sub.submitted_at) DESC`, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EvalSummaryRow
	for rows.Next() {
		r := &EvalSummaryRow{}
		var method, confidence interface{}
		if err := rows.Scan(&r.SubmissionID, &r.AssignmentID, &r.Assignment, &r.CourseCode, &r.StudentName,
			&r.EvalStatus, &method, &confidence, &r.Attempt, &r.Score, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if m, ok := method.(string); ok {
			r.Method = m
		}
		if cf, ok := confidence.(string); ok {
			r.Confidence = cf
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- Overrides / approval / retry ----------

// OverrideError describes over-validation failures for handlers.
var ErrCriterionOutOfRange = fmt.Errorf("%w: awarded points exceed the criterion maximum", ErrInvalid)

// OverrideCriterion records a faculty override on one criterion (audit trail).
func (s *Store) OverrideCriterion(evaluationID, facultyID, criterionID int64, points float64, feedback string) error {
	// Ownership check: evaluation → submission → assignment → course faculty.
	var ownerID int64
	err := s.DB.QueryRow(`SELECT c.faculty_id FROM evaluations e
		JOIN submissions sub ON sub.id = e.submission_id
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id WHERE e.id = ?`, evaluationID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != facultyID {
		return ErrForbidden
	}
	var maxPts float64
	if err := s.DB.QueryRow(`SELECT max_points FROM rubric_criteria WHERE id = ?`, criterionID).Scan(&maxPts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if points < 0 || points > maxPts {
		return ErrCriterionOutOfRange
	}
	_, err = s.DB.Exec(`INSERT INTO criteria_overrides (evaluation_id, criterion_id, awarded_points, faculty_feedback, faculty_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(evaluation_id, criterion_id) DO UPDATE SET
			awarded_points = excluded.awarded_points,
			faculty_feedback = excluded.faculty_feedback,
			faculty_id = excluded.faculty_id,
			created_at = excluded.created_at`,
		evaluationID, criterionID, evalRound(points), feedback, facultyID, NowUTC())
	return err
}

// ApproveEvaluation validates, freezes the effective scores, and publishes.
func (s *Store) ApproveEvaluation(evaluationID, facultyID int64) (float64, error) {
	v, err := s.EvaluationAttemptByID(evaluationID)
	if err != nil {
		return 0, err
	}
	var ownerID int64
	if err := s.DB.QueryRow(`SELECT c.faculty_id FROM evaluations e
		JOIN submissions sub ON sub.id = e.submission_id
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id WHERE e.id = ?`, evaluationID).Scan(&ownerID); err != nil {
		return 0, ErrNotFound
	}
	if ownerID != facultyID {
		return 0, ErrForbidden
	}
	if len(v.Criteria) == 0 {
		return 0, fmt.Errorf("%w: evaluation has no criterion results", ErrInvalid)
	}
	total := 0.0
	for _, c := range v.Criteria {
		if c.EffectivePts < 0 || c.EffectivePts > c.MaxPoints {
			return 0, fmt.Errorf("%w: criterion %q effective points %.2f out of range", ErrInvalid, c.Title, c.EffectivePts)
		}
		total += c.EffectivePts
	}
	total = evalRound(total)
	now := NowUTC()
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE evaluations SET approved = 1, approved_by = ?, approved_at = ? WHERE id = ?`, facultyID, now, evaluationID); err != nil {
		return 0, err
	}
	// Persist effective per-criterion points as overrides for anything not already overridden.
	for _, c := range v.Criteria {
		if c.EffectivePts != c.ModelPoints {
			if c.Override == nil {
				if _, err := tx.Exec(`INSERT INTO criteria_overrides (evaluation_id, criterion_id, awarded_points, faculty_feedback, faculty_id, created_at)
					VALUES (?, ?, ?, 'auto: approved as effective', ?, ?)`, evaluationID, c.CriterionID, c.EffectivePts, facultyID, now); err != nil {
					return 0, err
				}
			}
		}
	}
	if _, err := tx.Exec(`UPDATE submissions SET eval_status = ? WHERE id = ?`, EvalApproved, v.SubmissionID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// EvaluationAttemptByID loads by evaluation id.
func (s *Store) EvaluationAttemptByID(id int64) (*EvaluationView, error) {
	var sid, attempt int64
	err := s.DB.QueryRow(`SELECT submission_id, attempt FROM evaluations WHERE id = ?`, id).Scan(&sid, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.EvaluationAttempt(sid, attempt)
}

// CancelEvaluation resets an approved evaluation to provisional (unpublish).
func (s *Store) UnpublishEvaluation(evaluationID, facultyID int64) error {
	v, err := s.EvaluationAttemptByID(evaluationID)
	if err != nil {
		return err
	}
	var ownerID int64
	if err := s.DB.QueryRow(`SELECT c.faculty_id FROM evaluations e
		JOIN submissions sub ON sub.id = e.submission_id
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id WHERE e.id = ?`, evaluationID).Scan(&ownerID); err != nil {
		return ErrNotFound
	}
	if ownerID != facultyID {
		return ErrForbidden
	}
	_, err = s.DB.Exec(`UPDATE evaluations SET approved = 0, approved_by = NULL, approved_at = '' WHERE id = ?`, evaluationID)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE submissions SET eval_status = ? WHERE id = ?`, EvalNeedsReview, v.SubmissionID)
	return err
}

// ---------- Manual grading ----------

// ManualEvaluationInput is one faculty-entered score row.
type ManualScore struct {
	CriterionID int64
	Points      float64
	Feedback    string
}

// ManualEvaluation creates a MANUAL attempt (provisional until approved).
func (s *Store) ManualEvaluation(submissionID, facultyID int64, overall string, scores []ManualScore) (int64, error) {
	a, err := s.assignmentForSubmission(submissionID)
	if err != nil {
		return 0, err
	}
	if a.FacultyID != facultyID {
		return 0, ErrForbidden
	}
	in := evaluator.Input{}
	in.AssignmentID = a.ID
	in.SubmissionID = submissionID
	in.ExtractionOK = true
	for _, c := range a.Criteria {
		in.Criteria = append(in.Criteria, evaluator.CriterionInput{ID: c.ID, MaxPoints: float64(c.MaxPoints), Title: c.Title})
	}
	var outs []evaluator.CriterionResultOut
	maxBy := map[int64]float64{}
	for _, c := range a.Criteria {
		maxBy[c.ID] = float64(c.MaxPoints)
	}
	seen := map[int64]bool{}
	for _, sc := range scores {
		if seen[sc.CriterionID] {
			return 0, fmt.Errorf("%w: duplicate criterion %d", ErrInvalid, sc.CriterionID)
		}
		max, known := maxBy[sc.CriterionID]
		if !known {
			return 0, fmt.Errorf("%w: unknown criterion %d", ErrInvalid, sc.CriterionID)
		}
		if sc.Points < 0 || sc.Points > max {
			return 0, fmt.Errorf("%w: score %.2f exceeds the criterion maximum %.0f", ErrInvalid, sc.Points, max)
		}
		seen[sc.CriterionID] = true
		outs = append(outs, evaluator.CriterionResultOut{CriterionID: sc.CriterionID, AwardedPoints: sc.Points, Feedback: sc.Feedback})
	}
	for _, c := range in.Criteria {
		if !seen[c.ID] {
			return 0, fmt.Errorf("%w: missing score for criterion %d", ErrInvalid, c.ID)
		}
	}
	res, err := evaluator.ValidateManualResult(in, outs, overall)
	if err != nil {
		return 0, err
	}
	return s.PersistEvaluation(submissionID, *res)
}

func (s *Store) assignmentForSubmission(submissionID int64) (*Assignment, error) {
	var aid int64
	if err := s.DB.QueryRow(`SELECT assignment_id FROM submissions WHERE id = ?`, submissionID).Scan(&aid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.AssignmentByID(aid)
}

var _ = time.Now
