package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"educore/internal/middleware"
	"educore/internal/store"
)

// ownershipSubmission confirms the owning faculty for a submission.
func (d *Deps) ownsSubmission(r *http.Request, sid int64) bool {
	fid, ok := d.fp(r)
	if !ok {
		return false
	}
	var courseID int64
	err := d.Store.DB.QueryRow(`SELECT a.course_id FROM submissions s
		JOIN assignments a ON a.id = s.assignment_id WHERE s.id = ?`, sid).Scan(&courseID)
	if err != nil {
		return false
	}
	okOwn, _ := d.Store.FacultyOwnsCourse(courseID, fid)
	return okOwn
}

// evaluationCtx bundles page payload for review screens.
type evaluationCtx struct {
	Assignment  *store.Assignment
	Submission  *store.Submission
	StudentName string
	EvalStatus  string
	Eval        *store.EvaluationView
	Late        bool
}

// FacultySubmissionReview is the grading workspace.
func (d *Deps) FacultySubmissionReview(w http.ResponseWriter, r *http.Request) {
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this submission", "It belongs to a course taught by another faculty member.")
		return
	}
	ctx, err := d.buildEvalCtx(sid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := d.pageFrom(r)
	p.Title = "Evaluate · " + ctx.Assignment.Title
	p.Nav = d.facultyNav(r)
	p.Flash = flash(r.URL.Query())
	p.Values = map[string]string{}
	d.render(w, r, "faculty_evaluation.html", struct {
		PageData
		*evaluationCtx
	}{p, ctx}, http.StatusOK)
}

func (d *Deps) buildEvalCtx(submissionID int64) (*evaluationCtx, error) {
	sub, err := d.Store.SubmissionRaw(submissionID)
	if err != nil {
		return nil, err
	}
	a, err := d.Store.AssignmentByID(sub.AssignmentID)
	if err != nil {
		return nil, err
	}
	ctx := &evaluationCtx{Assignment: a, Submission: sub, EvalStatus: sub.EvalStatus}
	var name string
	_ = d.Store.DB.QueryRow(`SELECT name FROM users WHERE id = ?`, sub.StudentID).Scan(&name)
	ctx.StudentName = name
	if t, terr := store.ParseStored(a.DueDate); terr == nil {
		if st, serr := store.ParseStored(sub.SubmittedAt); serr == nil {
			ctx.Late = st.After(t)
		}
	}
	if ev, err := d.Store.LatestEvaluation(submissionID); err == nil {
		ctx.Eval = ev
	}
	return ctx, nil
}

// FacultyEvalOverview lists the evaluation workflow for a faculty member.
func (d *Deps) FacultyEvalOverview(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	rows, err := d.Store.EvalOverview(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = "Evaluations · EDUCORE"
	p.Nav = d.facultyNav(r)
	p.Flash = flash(r.URL.Query())
	p.Values = map[string]string{}
	d.render(w, r, "faculty_evaluations.html", struct {
		PageData
		Rows []*store.EvalSummaryRow
	}{p, rows}, http.StatusOK)
}

// FacultyTriggerEvaluate enqueues a (re-)evaluation for the latest submission.
func (d *Deps) FacultyTriggerEvaluate(w http.ResponseWriter, r *http.Request) {
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := r.Referer()
	if back == "" {
		back = fmt.Sprintf("/faculty/submissions/%d", sid)
	}
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	if err := d.Store.EnqueueEvaluation(sid); err != nil {
		d.errRedirect(w, r, back, "Could not queue the evaluation: "+humanError(err))
		return
	}
	d.kickWorker()
	http.Redirect(w, r, noErrURL(back, "Evaluation queued."), http.StatusSeeOther)
}

// FacultyOverrideCriterion stores a score override (audit-trailed).
func (d *Deps) FacultyOverrideCriterion(w http.ResponseWriter, r *http.Request) {
	evid, err := idParam(r, "evalID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	back, subID := d.backToEval(r, evid)
	if !d.ownsEvaluation(r, evid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This evaluation belongs to another faculty member's course.")
		return
	}
	cid, _ := idParam(r, "criterionID")
	pts, _ := strconv.ParseFloat(r.PostFormValue("points"), 64)
	if perr := d.Store.OverrideCriterion(evid, fid, cid, pts, fv(r, "feedback")); perr != nil {
		d.errRedirect(w, r, back, humanError(perr))
		return
	}
	_ = subID
	http.Redirect(w, r, noErrURL(back, "Score updated. Approve to publish."), http.StatusSeeOther)
}

// FacultyApproveEvaluation freezes and publishes the effective scores.
func (d *Deps) FacultyApproveEvaluation(w http.ResponseWriter, r *http.Request) {
	evid, err := idParam(r, "evalID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	back, _ := d.backToEval(r, evid)
	if !d.ownsEvaluation(r, evid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This evaluation belongs to another faculty member's course.")
		return
	}
	total, err := d.Store.ApproveEvaluation(evid, fid)
	if err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, noErrURL(back, fmt.Sprintf("Approved and published. Final score: %.2f / 100 (server-calculated).", total)), http.StatusSeeOther)
}

// FacultyUnpublishEvaluation retracts an approved result.
func (d *Deps) FacultyUnpublishEvaluation(w http.ResponseWriter, r *http.Request) {
	evid, err := idParam(r, "evalID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	back, _ := d.backToEval(r, evid)
	if !d.ownsEvaluation(r, evid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This evaluation belongs to another faculty member's course.")
		return
	}
	if err := d.Store.UnpublishEvaluation(evid, fid); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, noErrURL(back, "Result unpublished; moved back to review."), http.StatusSeeOther)
}

// FacultyManualGrade creates a MANUAL evaluation attempt.
func (d *Deps) FacultyManualGrade(w http.ResponseWriter, r *http.Request) {
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	back := fmt.Sprintf("/faculty/submissions/%d", sid)
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	_ = fid
	// Collect score_num_i → criterion id, score_pts_i, score_feedback_i
	sub, serr := d.Store.SubmissionRaw(sid)
	if serr != nil {
		http.NotFound(w, r)
		return
	}
	a, aerr := d.Store.AssignmentByID(sub.AssignmentID)
	if aerr != nil {
		http.NotFound(w, r)
		return
	}
	var scores []store.ManualScore
	for _, c := range a.Criteria {
		ptsStr, provided := r.PostForm["score_pts_"+strings.TrimSpace(fmt.Sprint(c.ID))]
		if !provided || len(ptsStr) == 0 || strings.TrimSpace(ptsStr[0]) == "" {
			continue
		}
		pts, perr := strconv.ParseFloat(strings.TrimSpace(ptsStr[0]), 64)
		if perr != nil {
			d.errRedirect(w, r, back, fmt.Sprintf("Enter a number for %q.", c.Title))
			return
		}
		scores = append(scores, store.ManualScore{
			CriterionID: c.ID,
			Points:      pts,
			Feedback:    fv(r, "score_feedback_"+fmt.Sprint(c.ID)),
		})
	}
	if len(scores) == 0 {
		d.errRedirect(w, r, back, "Enter a score for at least one criterion, or all criteria.")
		return
	}
	if _, err := d.Store.ManualEvaluation(sid, fid, fv(r, "overall"), scores); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, noErrURL(back, "Manual evaluation saved as a new attempt. Not published until approved."), http.StatusSeeOther)
}

// ownsEvaluation verifies the owning faculty of an evaluation.
func (d *Deps) ownsEvaluation(r *http.Request, evalID int64) bool {
	_, ok := d.fp(r)
	if !ok {
		return false
	}
	var courseID int64
	err := d.Store.DB.QueryRow(`SELECT a.course_id FROM evaluations e
		JOIN submissions s ON s.id = e.submission_id
		JOIN assignments a ON a.id = s.assignment_id WHERE e.id = ?`, evalID).Scan(&courseID)
	if err != nil {
		return false
	}
	fid, _ := d.fp(r)
	okOwn, _ := d.Store.FacultyOwnsCourse(courseID, fid)
	return okOwn
}

// backToEval computes the review URL for an evaluation id.
func (d *Deps) backToEval(r *http.Request, evalID int64) (string, int64) {
	var sid int64
	_ = d.Store.DB.QueryRow(`SELECT submission_id FROM evaluations WHERE id = ?`, evalID).Scan(&sid)
	if ref := r.Referer(); ref != "" {
		return ref, sid
	}
	return fmt.Sprintf("/faculty/submissions/%d", sid), sid
}

// noErrURL injects an ok= message into a redirect target.
func noErrURL(target, msg string) string {
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + "ok=" + strings.ReplaceAll(msg, " ", "+")
}

// kickWorker nudges the evaluation worker queue.
func (d *Deps) kickWorker() {
	if d.WorkerNotify != nil {
		select {
		case d.WorkerNotify <- struct{}{}:
		default:
		}
	}
}

var _ = middleware.TokenFrom
