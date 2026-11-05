package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"educore/internal/agent"
	"educore/internal/store"
)

// AgentConsole lists runs + start forms for a submission.
func (d *Deps) AgentConsole(w http.ResponseWriter, r *http.Request) {
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this submission", "It belongs to a course taught by another faculty member.")
		return
	}
	sub, gerr := d.Store.SubmissionRaw(sid)
	if gerr != nil {
		http.NotFound(w, r)
		return
	}
	a, aerr := d.Store.AssignmentByID(sub.AssignmentID)
	if aerr != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	runs, rerr := d.Store.AgentRunsForSubmission(sid, fid)
	if rerr != nil {
		http.Error(w, rerr.Error(), http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = "Agent Evaluation Console · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_agent_console.html", struct {
		PageData
		Submission *store.Submission
		Assignment *store.Assignment
		Runs       []*store.AgentRun
	}{p, sub, a, runs}, http.StatusOK)
}

// AgentRunDetail shows the run console: trace, result, actions.
func (d *Deps) AgentRunDetail(w http.ResponseWriter, r *http.Request) {
	rid, err := idParam(r, "runID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	run, rerr := d.Store.AgentRunByID(rid, fid)
	if rerr != nil {
		msg, code := runAccessError(rerr)
		d.renderShellError(w, r, code, "Run not accessible", msg)
		return
	}
	sub, gerr := d.Store.SubmissionRaw(run.SubmissionID)
	if gerr != nil {
		http.NotFound(w, r)
		return
	}
	events, eerr := d.Store.AgentEvents(rid)
	if eerr != nil {
		http.Error(w, eerr.Error(), http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = fmt.Sprintf("Agent run #%d · EDUCORE", run.ID)
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_agent_run.html", struct {
		PageData
		Run    *store.AgentRun
		Sub    *store.Submission
		Events []*store.AgentEvent
	}{p, run, sub, events}, http.StatusOK)
}

func runAccessError(err error) (string, int) {
	if err == store.ErrNotFound {
		return "That run does not exist.", http.StatusNotFound
	}
	return "You do not have access to this run.", http.StatusForbidden
}

// AgentRunStart creates a fresh EVALUATE run.
func (d *Deps) AgentRunStart(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	goal := strings.TrimSpace(fv(r, "goal"))
	id, cerr := d.Store.CreateAgentRun(sid, fid, agent.KindEvaluate, goal, agent.DefaultMaxIterations, false)
	if cerr != nil {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/submissions/%d/agent", sid), humanError(cerr))
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", id), http.StatusSeeOther)
}

func whyNote(note string) string {
	if note == "" {
		return ""
	}
	return " Faculty note: " + note
}

// AgentRunReconsider starts a RECONSIDER run for one criterion.
func (d *Deps) AgentRunReconsider(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/submissions/%d/agent", sid)
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	cidStr := strings.TrimSpace(fv(r, "criterion_id"))
	note := strings.TrimSpace(fv(r, "note"))
	if cidStr == "" {
		d.errRedirect(w, r, back, "Specify the criterion id to reconsider.")
		return
	}
	goal := fmt.Sprintf("Reconsider criterion %s.%s", cidStr, whyNote(note))
	id, cerr := d.Store.CreateAgentRun(sid, fid, agent.KindReconsider, goal, agent.DefaultMaxIterations, false)
	if cerr != nil {
		d.errRedirect(w, r, back, humanError(cerr))
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", id), http.StatusSeeOther)
}

// AgentRunFollowup starts a read-only FOLLOWUP run.
func (d *Deps) AgentRunFollowup(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/submissions/%d/agent", sid)
	if !d.ownsSubmission(r, sid) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	q := strings.TrimSpace(fv(r, "question"))
	if q == "" {
		d.errRedirect(w, r, back, "Enter a question for the agent.")
		return
	}
	id, cerr := d.Store.CreateAgentRun(sid, fid, agent.KindFollowup, q, 6, true)
	if cerr != nil {
		d.errRedirect(w, r, back, humanError(cerr))
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", id), http.StatusSeeOther)
}

// AgentRunMessage answers a follow-up question in the context of prior run.
func (d *Deps) AgentRunMessage(w http.ResponseWriter, r *http.Request) {
	rid, err := idParam(r, "runID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	run, rerr := d.Store.AgentRunByID(rid, fid)
	if rerr != nil {
		msg, code := runAccessError(rerr)
		d.renderShellError(w, r, code, "Run not accessible", msg)
		return
	}
	q := strings.TrimSpace(fv(r, "question"))
	if q == "" {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), "Enter a question for the agent.")
		return
	}
	id, cerr := d.Store.CreateAgentRun(run.SubmissionID, fid, agent.KindFollowup, q, 6, true)
	if cerr != nil {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), humanError(cerr))
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", id), http.StatusSeeOther)
}

// AgentRunRetry re-runs a PENDING/FAILED run.
func (d *Deps) AgentRunRetry(w http.ResponseWriter, r *http.Request) {
	rid, err := idParam(r, "runID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	run, rerr := d.Store.AgentRunByID(rid, fid)
	if rerr != nil {
		msg, code := runAccessError(rerr)
		d.renderShellError(w, r, code, "Run not accessible", msg)
		return
	}
	claimed, cerr := d.Store.ClaimAgentRun(run.ID)
	if cerr != nil || !claimed {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), "Run cannot be restarted from its current state.")
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), http.StatusSeeOther)
}

// AgentRunReconsiderFrom starts a reconsider run tied to a prior run page.
func (d *Deps) AgentRunReconsiderFrom(w http.ResponseWriter, r *http.Request) {
	rid, err := idParam(r, "runID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fid, _ := d.fp(r)
	run, rerr := d.Store.AgentRunByID(rid, fid)
	if rerr != nil {
		msg, code := runAccessError(rerr)
		d.renderShellError(w, r, code, "Run not accessible", msg)
		return
	}
	if !d.ownsSubmission(r, run.SubmissionID) {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "This submission belongs to another faculty member's course.")
		return
	}
	cidStr := strings.TrimSpace(fv(r, "criterion_id"))
	if cidStr == "" {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), "Specify the criterion id to reconsider.")
		return
	}
	goal := fmt.Sprintf("Reconsider criterion %s.%s", cidStr, whyNote(strings.TrimSpace(fv(r, "note"))))
	id, cerr := d.Store.CreateAgentRun(run.SubmissionID, fid, agent.KindReconsider, goal, agent.DefaultMaxIterations, false)
	if cerr != nil {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", rid), humanError(cerr))
		return
	}
	d.AgentEngine.Kick()
	http.Redirect(w, r, fmt.Sprintf("/faculty/agent/runs/%d", id), http.StatusSeeOther)
}
