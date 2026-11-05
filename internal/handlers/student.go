package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"educore/internal/analytics"
	"educore/internal/middleware"
	"educore/internal/store"
)

// studentNav builds the Phase 3 student sidebar with section-aware active state.
func (d *Deps) studentNav(r *http.Request) []NavEntry {
	p := r.URL.Path
	var active string
	switch {
	case strings.HasPrefix(p, "/student/courses"):
		active = "/student/courses"
	case strings.HasPrefix(p, "/student/assignments"):
		active = "/student/assignments"
	case strings.HasPrefix(p, "/student/performance"):
		active = "/student/performance"
	default:
		active = "/student"
	}
	entries := []struct{ href, label, icon string }{
		{"/student", "Overview", "layout"},
		{"/student/courses", "My Courses", "book-open"},
		{"/student/assignments", "My Assignments", "clipboard-list"},
		{"/student/performance", "Performance", "bar-chart"},
	}
	out := make([]NavEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, NavEntry{Href: e.href, Label: e.label, Icon: e.icon, Active: e.href == active})
	}
	return out
}

// StudentCourses lists enrolled courses with per-subject academic context.
func (d *Deps) StudentCourses(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	courses, err := d.Store.CoursesByStudent(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	approved, aerr := d.Store.ApprovedRowsForStudent(sid)
	if aerr != nil {
		http.Error(w, aerr.Error(), http.StatusInternalServerError)
		return
	}
	states, serr := d.Store.StudentAssignmentStates(sid)
	if serr != nil {
		http.Error(w, serr.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		C             *store.Course
		Comprehension float64
		HasResults    bool
		Evaluated     int
		Published     int
		Completed     int
	}
	rows := make([]*row, 0, len(courses))
	publishedPerCourse := map[int64]int{}
	completedPerCourse := map[int64]int{}
	for _, st := range states {
		publishedPerCourse[st.Assignment.CourseID]++
		if st.Status != "NOT_SUBMITTED" {
			completedPerCourse[st.Assignment.CourseID]++
		}
	}
	for _, c := range courses {
		cs := analytics.SubjectComprehension(approved, c.ID)
		rows = append(rows, &row{
			C:             c,
			Comprehension: cs.Comprehension,
			HasResults:    cs.Students > 0,
			Evaluated:     cs.Evaluations,
			Published:     publishedPerCourse[c.ID],
			Completed:     completedPerCourse[c.ID],
		})
	}
	p := d.pageFrom(r)
	p.Title = "My Courses · EDUCORE"
	p.Nav = d.studentNav(r)
	d.render(w, r, "student_courses.html", struct {
		PageData
		Rows []*row
	}{p, rows}, http.StatusOK)
}

// StudentCourseDetail shows one enrolled course and its published assignments.
func (d *Deps) StudentCourseDetail(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok, aerr := d.Store.CourseAccessFor(id, sid, "STUDENT"); aerr != nil || !ok {
		d.renderShellError(w, r, http.StatusForbidden, "Not enrolled", "You can view only the courses you are enrolled in. Ask your faculty member to enroll you.")
		return
	}
	course, err := d.Store.CourseByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rows, qerr := d.Store.DB.Query(`SELECT a.id, a.title, a.topic, a.due_date, a.total_marks,
			COALESCE(s.status,''), COALESCE(s.submitted_at,'')
		FROM assignments a
		LEFT JOIN submissions s ON s.assignment_id = a.id AND s.student_id = ?
		WHERE a.course_id = ? AND a.published = 1
		ORDER BY a.due_date`, sid, id)
	if qerr != nil {
		http.Error(w, qerr.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type courseAssign struct {
		ID         int64
		Title      string
		Topic      string
		DueDate    string
		TotalMarks int
		Status     string
		Submitted  string
	}
	var items []courseAssign
	for rows.Next() {
		var it courseAssign
		var status, at string
		if err := rows.Scan(&it.ID, &it.Title, &it.Topic, &it.DueDate, &it.TotalMarks, &status, &at); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if status == "" {
			it.Status = "NOT_SUBMITTED"
		} else {
			it.Status = status
			it.Submitted = at
		}
		items = append(items, it)
	}
	p := d.pageFrom(r)
	p.Title = course.Name + " · EDUCORE"
	p.Nav = d.studentNav(r)
	p.Values = map[string]string{}
	d.render(w, r, "student_course.html", struct {
		PageData
		Course      *store.Course
		Assignments []courseAssign
	}{p, course, items}, http.StatusOK)
}

// StatusLabel and StatusClass format submission states for templates.
func StatusLabel(st string) string {
	switch st {
	case "NOT_SUBMITTED":
		return "Not submitted"
	case "SUBMITTED":
		return "Submitted"
	case "EVALUATED":
		return "Evaluated"
	case "NEEDS_REVIEW":
		return "Needs review"
	}
	return st
}

// StatusClass maps statuses to CSS color classes.
func StatusClass(st string) string {
	switch st {
	case "NOT_SUBMITTED":
		return "badge-neutral"
	case "SUBMITTED":
		return "badge-info"
	case "EVALUATED":
		return "badge-success"
	case "NEEDS_REVIEW":
		return "badge-warning"
	}
	return ""
}

// StudentAssignments lists the student's published assignments with filters.
// All statuses come from the shared StudentAssignmentStates resolver so this
// page, the dashboard and the detail page always agree.
func (d *Deps) StudentAssignments(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	states, err := d.Store.StudentAssignmentStates(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		A       *store.Assignment
		Status  string
		EvalSt  string
		Late    bool
		Overdue bool
	}
	var rows []*row
	for _, st := range states {
		rows = append(rows, &row{A: st.Assignment, Status: st.Status, EvalSt: st.EvalStatus, Late: st.Late, Overdue: st.Overdue})
	}
	filter := strings.ToLower(r.URL.Query().Get("filter"))
	var kept []*row
	switch filter {
	case "pending":
		for _, x := range rows {
			if x.Status == "NOT_SUBMITTED" && !x.Overdue {
				kept = append(kept, x)
			}
		}
	case "submitted":
		for _, x := range rows {
			if x.Status != "NOT_SUBMITTED" {
				kept = append(kept, x)
			}
		}
	case "overdue":
		for _, x := range rows {
			if x.Overdue {
				kept = append(kept, x)
			}
		}
	default:
		kept = rows
	}
	// stable order: attention first, then due date
	sort.SliceStable(kept, func(i, j int) bool {
		if (kept[i].Overdue || kept[i].Status == "NOT_SUBMITTED") != (kept[j].Overdue || kept[j].Status == "NOT_SUBMITTED") {
			return kept[i].Overdue || kept[i].Status == "NOT_SUBMITTED"
		}
		return kept[i].A.DueDate < kept[j].A.DueDate
	})
	p := d.pageFrom(r)
	p.Title = "My Assignments · EDUCORE"
	p.Nav = d.studentNav(r)
	d.render(w, r, "student_assignments.html", struct {
		PageData
		Items  []*row
		Filter string
	}{p, kept, filter}, http.StatusOK)
}

// StudentAssignmentDetail renders one enrolled assignment with rubric + submission form.
func (d *Deps) StudentAssignmentDetail(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := d.Store.AssignmentByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.Published {
		http.NotFound(w, r)
		return
	}
	authorized, aerr := d.Store.CourseAccessFor(a.CourseID, sid, "STUDENT")
	if aerr != nil || !authorized {
		d.renderShellError(w, r, http.StatusForbidden, "Not enrolled", "You can view only the courses you are enrolled in. Ask your faculty member to enroll you.")
		return
	}
	sub, _ := d.Store.SubmissionFor(id, sid)
	status, submittedAt, late := d.Store.SubmissionStatusFor(a, sid)
	duePast := store.OverdueDue(a)
	p := d.pageFrom(r)
	p.Title = a.Title + " · EDUCORE"
	p.Nav = d.studentNav(r)
	d.applyFlash(r, &p)
	if r.Method == http.MethodGet && r.URL.Query().Get("flash") != "" {
		p.Flash = r.URL.Query().Get("flash")
	}
	// Only faculty-approved results are shown to students; provisional
	// evaluations stay in the faculty review workspace.
	var eval *store.EvaluationView
	if sub != nil && sub.EvalStatus == store.EvalApproved {
		eval, _ = d.Store.LatestEvaluation(sub.ID)
		if eval != nil && !eval.Approved {
			eval = nil
		}
	}
	d.render(w, r, "student_assignment.html", struct {
		PageData
		Assignment     *store.Assignment
		Submission     *store.Submission
		Status         string
		StatusLabel    string
		StatusClass    string
		SubmittedAt    string
		LateSubmission bool
		DuePast        bool
		Eval           *store.EvaluationView
	}{p, a, sub, status, StatusLabel(status), StatusClass(status), submittedAt, late, duePast, eval}, http.StatusOK)
}

// StudentSubmit accepts text and/or file submission with replacement.
func (d *Deps) StudentSubmit(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/student/assignments/%d", id)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(store.MaxUploadBytes + (1 << 20)); err != nil {
			d.errRedirect(w, r, back, "The form could not be processed (is the file too large?)")
			return
		}
	} else if err := r.ParseForm(); err != nil {
		d.errRedirect(w, r, back, "The form could not be processed (is the file too large?)")
		return
	}
	_ = middleware.TokenFrom(r)
	text := strings.TrimSpace(r.FormValue("content"))

	var savedFile *store.SavedFile
	var extracted string
	file, fh, ferr := r.FormFile("document")
	if ferr == nil {
		defer file.Close()
		sf, err := d.Store.SaveSubmissionFile(fh.Filename, file, fh.Size)
		if err != nil {
			d.errRedirect(w, r, back, humanError(err))
			return
		}
		savedFile = sf
		// Extract text best-effort; failures do not block the submission but
		// are recorded so the evaluation engine can flag them.
		if data, rerr := os.ReadFile(sf.StoragePath); rerr == nil {
			if res := store.ExtractFromBytes(fh.Filename, data); res.Failed {
				extracted = "EXTRACTION_FAILED: " + res.Reason
			} else {
				extracted = res.Text
			}
		} else {
			extracted = "EXTRACTION_FAILED: stored file could not be read"
		}
	}

	res, err := d.Store.CreateOrUpdateSubmission(id, sid, text, savedFile, extracted)
	if err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	// Kick the evaluation worker immediately (best effort).
	if d.WorkerNotify != nil {
		select {
		case d.WorkerNotify <- struct{}{}:
		default:
		}
	}
	msg := "Submission received."
	if res.Late {
		msg = "Submission received — recorded as LATE (after the deadline)."
	}
	http.Redirect(w, r, back+"?flash="+strings.ReplaceAll(msg, " ", "+"), http.StatusSeeOther)
}

// StudentSubmissionDownload lets a student fetch their own stored file.
func (d *Deps) StudentSubmissionDownload(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := d.Store.AssignmentByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	authorized, aerr := d.Store.CourseAccessFor(a.CourseID, sid, "STUDENT")
	if aerr != nil || !authorized {
		d.renderShellError(w, r, http.StatusForbidden, "Not permitted", "You can download only your own submissions.")
		return
	}
	sub, err := d.Store.SubmissionFor(id, sid)
	if err != nil || sub.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	srv := strings.TrimPrefix(sub.FilePath, "./data/uploads/")
	file, err := d.Store.OpenStoredFile(srv)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(sub.Filename, `"`, ``)+`"`)
	_, _ = io.Copy(w, file)
}
