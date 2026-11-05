package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// AgentRun is one bounded agent execution over a submission.
type AgentRun struct {
	ID                 int64
	SubmissionID       int64
	FacultyID          int64
	Goal               string
	Kind               string // EVALUATE | RECONSIDER | FOLLOWUP
	Status             string
	Iterations         int
	MaxIterations      int
	Draft              string // JSON: accumulated criterion draft
	Error              string
	ResultEvaluationID int64
	Readonly           bool // follow-up conversations may not mutate marks
	CreatedAt          string
	UpdatedAt          string
}

// AgentEvent is one trace entry.
type AgentEvent struct {
	ID        int64
	RunID     int64
	Seq       int
	Kind      string
	Payload   string
	CreatedAt string
}

// CreateAgentRun inserts a pending run.
func (s *Store) CreateAgentRun(submissionID, facultyID int64, kind, goal string, maxIter int, readonly bool) (int64, error) {
	now := NowUTC()
	ro := 0
	if readonly {
		ro = 1
	}
	res, err := s.DB.Exec(`INSERT INTO agent_runs (submission_id, faculty_id, goal, kind, status, max_iterations, followup_readonly, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'PENDING', ?, ?, ?, ?)`, submissionID, facultyID, goal, kind, maxIter, ro, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AgentRunByID loads one run after an ownership check.
func (s *Store) AgentRunByID(id, facultyID int64) (*AgentRun, error) {
	r := &AgentRun{}
	var ro int
	err := s.DB.QueryRow(`SELECT id, submission_id, faculty_id, goal, kind, status, iterations, max_iterations,
		draft, error, COALESCE(result_evaluation_id,0), followup_readonly, created_at, updated_at
		FROM agent_runs WHERE id = ?`, id).
		Scan(&r.ID, &r.SubmissionID, &r.FacultyID, &r.Goal, &r.Kind, &r.Status, &r.Iterations, &r.MaxIterations,
			&r.Draft, &r.Error, &r.ResultEvaluationID, &ro, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.FacultyID != facultyID {
		return nil, ErrForbidden
	}
	r.Readonly = ro == 1
	return r, nil
}

// AgentRunsForSubmission lists runs for the faculty-member console.
func (s *Store) AgentRunsForSubmission(submissionID, facultyID int64) ([]*AgentRun, error) {
	rows, err := s.DB.Query(`SELECT id, submission_id, faculty_id, goal, kind, status, iterations, max_iterations,
		draft, error, COALESCE(result_evaluation_id,0), followup_readonly, created_at, updated_at
		FROM agent_runs WHERE submission_id = ? AND faculty_id = ? ORDER BY id DESC`, submissionID, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AgentRun
	for rows.Next() {
		r := &AgentRun{}
		var ro int
		if err := rows.Scan(&r.ID, &r.SubmissionID, &r.FacultyID, &r.Goal, &r.Kind, &r.Status, &r.Iterations,
			&r.MaxIterations, &r.Draft, &r.Error, &r.ResultEvaluationID, &ro, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Readonly = ro == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClaimAgentRun atomically transitions PENDING→RUNNING for the drainer.
func (s *Store) ClaimAgentRun(runID int64) (bool, error) {
	res, err := s.DB.Exec(`UPDATE agent_runs SET status = 'RUNNING', updated_at = ?
		WHERE id = ? AND status IN ('PENDING','FAILED')`, NowUTC(), runID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RecoverAgentRuns returns interrupted RUNNING runs to PENDING on startup.
func (s *Store) RecoverAgentRuns() (int, error) {
	res, err := s.DB.Exec(`UPDATE agent_runs SET status = 'PENDING', updated_at = ? WHERE status = 'RUNNING'`, NowUTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// UpdateAgentRunMessages checkpoints the transcript after each turn.
func (s *Store) UpdateAgentRunMessages(runID int64, iterations int, messages string, draft string) error {
	_, err := s.DB.Exec(`UPDATE agent_runs SET iterations = ?, messages = ?, draft = ?, updated_at = ? WHERE id = ?`,
		iterations, messages, draft, NowUTC(), runID)
	return err
}

// FinishAgentRun transitions to DONE/FAILED with optional result link.
func (s *Store) FinishAgentRun(runID int64, status string, errMsg string, resultEvalID int64) error {
	var resID any
	if resultEvalID > 0 {
		resID = resultEvalID
	}
	_, err := s.DB.Exec(`UPDATE agent_runs SET status = ?, error = ?, result_evaluation_id = ?, updated_at = ? WHERE id = ?`,
		status, errMsg, resID, NowUTC(), runID)
	return err
}

// AgentRunPendingBySubmission finds a queued run for a submission.
func (s *Store) AgentRunPendingBySubmission(submissionID int64) (int64, bool, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM agent_runs WHERE submission_id = ? AND status = 'PENDING' ORDER BY id LIMIT 1`, submissionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// AppendAgentEvent writes one trace entry, allocating the next sequence.
func (s *Store) AppendAgentEvent(runID int64, kind string, payload string) error {
	var seq int
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM agent_events WHERE run_id = ?`, runID).Scan(&seq); err != nil {
		return err
	}
	clip := payload
	if len(clip) > 8000 {
		clip = clip[:8000] + "…[clipped]"
	}
	_, err := s.DB.Exec(`INSERT INTO agent_events (run_id, seq, kind, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		runID, seq, kind, clip, NowUTC())
	return err
}

// AgentEvents lists the trace of a run (ascending seq).
func (s *Store) AgentEvents(runID int64) ([]*AgentEvent, error) {
	rows, err := s.DB.Query(`SELECT id, run_id, seq, kind, payload, created_at FROM agent_events WHERE run_id = ? ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AgentEvent
	for rows.Next() {
		e := &AgentEvent{}
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.Kind, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AgentEventCount is a helper for console badges.
func (s *Store) AgentEventCount(runID int64) int {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(1) FROM agent_events WHERE run_id = ?`, runID).Scan(&n)
	return n
}

var _ = fmt.Sprintf
var _ = json.Marshal

// AgentRunByIDFromEngine loads a run for the internal drainer (trusted path).
func (s *Store) AgentRunByIDFromEngine(id int64) (*AgentRun, error) {
	r := &AgentRun{}
	var ro int
	err := s.DB.QueryRow(`SELECT id, submission_id, faculty_id, goal, kind, status, iterations, max_iterations,
		draft, error, COALESCE(result_evaluation_id,0), followup_readonly, created_at, updated_at
		FROM agent_runs WHERE id = ?`, id).
		Scan(&r.ID, &r.SubmissionID, &r.FacultyID, &r.Goal, &r.Kind, &r.Status, &r.Iterations, &r.MaxIterations,
			&r.Draft, &r.Error, &r.ResultEvaluationID, &ro, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.Readonly = ro == 1
	return r, nil
}
