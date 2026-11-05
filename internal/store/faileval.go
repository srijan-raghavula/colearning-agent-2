package store

// FailEval marks a submission's evaluation FAILED while preserving the
// submission content and prior attempts exactly as the queue path does.
func (s *Store) FailEval(submissionID int64, errSummary string) error {
	_, uerr := s.DB.Exec(`UPDATE submissions SET eval_status = ? WHERE id = ?`, EvalFailed, submissionID)
	// Supersede any running/pending queue rows and record the error on the latest job.
	_, _ = s.DB.Exec(`UPDATE evaluation_jobs SET status = 'SUPERSEDED', error = ?, updated_at = ?
	                  WHERE submission_id = ? AND status IN ('PENDING','RUNNING')`,
		truncateString(errSummary, 500), NowUTC(), submissionID)
	return uerr
}
