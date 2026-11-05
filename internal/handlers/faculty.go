package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"educore/internal/middleware"
	"educore/internal/store"
)

// helpers ------------------------------------------------------------

func (d *Deps) fp(r *http.Request) (int64, bool) {
	u, ok := middleware.UserFrom(r)
	if !ok || !u.IsFaculty() {
		return 0, false
	}
	return u.ID, true
}

func (d *Deps) sp(r *http.Request) (int64, bool) {
	u, ok := middleware.UserFrom(r)
	if !ok || !u.IsStudent() {
		return 0, false
	}
	return u.ID, true
}

func idParam(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func (d *Deps) errRedirect(w http.ResponseWriter, r *http.Request, back string, msg string) {
	q := url.Values{}
	if msg != "" {
		q.Set("err", msg)
	}
	http.Redirect(w, r, back+(func() string {
		if s := q.Encode(); s != "" {
			return "?" + s
		}
		return ""
	}()), http.StatusSeeOther)
}

// formValue trims.
func fv(r *http.Request, key string) string { return strings.TrimSpace(r.PostFormValue(key)) }

// flashFromQuery surfaces redirect errors.
func flash(q url.Values) string { return q.Get("err") }

// FacultyStudents moved to students_page.go (academic context columns).

// FacultyStudentCreate creates a student account.
func (d *Deps) FacultyStudentCreate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	name, email, pass := fv(r, "name"), fv(r, "email"), r.PostFormValue("password")
	if _, err := d.Store.CreateStudentAccount(email, name, pass); err != nil {
		d.errRedirect(w, r, "/faculty/students", humanError(err))
		return
	}
	q := url.Values{}
	q.Set("ok", "Student account created for "+name+".")
	if err := r.PostFormValue("enroll_course"); err != "" {
		if cid, perr := strconv.ParseInt(r.PostFormValue("enroll_course"), 10, 64); perr == nil && cid > 0 {
			var uid int64
			_ = d.Store.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, strings.ToLower(email)).Scan(&uid)
			if uid > 0 {
				if eerr := d.Store.Enroll(cid, uid, fid); eerr != nil {
					q.Set("notice", "Account created but enrollment failed: "+humanError(eerr))
				}
			}
		}
	}
	http.Redirect(w, r, "/faculty/students"+(func() string {
		s := q.Encode()
		if s != "" {
			return "?" + s
		}
		return ""
	}()), http.StatusSeeOther)
}

// facultyNav builds the Phase 3 faculty sidebar. The active item is derived
// from the request path so detail pages (e.g. /faculty/assignments/7) still
// highlight their section.
func (d *Deps) facultyNav(r *http.Request) []NavEntry {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/faculty/courses"):
		return d.navSet(r, "/faculty/courses", "Courses")
	case strings.HasPrefix(p, "/faculty/assignments"), strings.HasPrefix(p, "/faculty/submissions"):
		return d.navSet(r, "/faculty/assignments", "Assignments")
	case strings.HasPrefix(p, "/faculty/students"):
		return d.navSet(r, "/faculty/students", "Students")
	default: // /faculty and anything else faculty-owned
		return d.navSet(r, "/faculty", "Overview")
	}
}

// navSet marks the given key as the active row.
func (d *Deps) navSet(_ *http.Request, active string, _ string) []NavEntry {
	entries := []struct{ href, label, icon string }{
		{"/faculty", "Overview", "layout"},
		{"/faculty/courses", "Courses", "book-open"},
		{"/faculty/assignments", "Assignments", "clipboard-list"},
		{"/faculty/students", "Students", "users"},
	}
	out := make([]NavEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, NavEntry{Href: e.href, Label: e.label, Icon: e.icon, Active: e.href == active})
	}
	return out
}

// Courses ------------------------------------------------------------

// FacultyCourses lists the faculty member's courses.
func (d *Deps) FacultyCourses(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courses, err := d.Store.CoursesByFaculty(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = "Courses · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_courses.html", struct {
		PageData
		Courses []*store.Course
	}{p, courses}, http.StatusOK)
}

// FacultyCourseCreate POSTs a new course.
func (d *Deps) FacultyCourseCreate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	name, code, desc := fv(r, "name"), fv(r, "code"), strings.TrimSpace(fv(r, "description"))
	c, err := d.Store.CreateCourse(fid, name, code, desc)
	if err != nil {
		d.errRedirect(w, r, "/faculty/courses", humanError(err)+" (name: "+name+")")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/faculty/courses/%d?ok=Course+created", c.ID), http.StatusSeeOther)
}

// FacultyCourseDetail shows one owned course with roster + assignments.
func (d *Deps) FacultyCourseDetail(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok, aerr := d.Store.FacultyOwnsCourse(id, fid); aerr != nil || !ok {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this course", "You can open only the courses you own. If you expect access, check the course code.")
		return
	}
	course, err := d.Store.CourseByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	students, _ := d.Store.StudentsOfCourse(id, fid)
	assignments, _ := d.Store.AssignmentsByCourse(id)
	all, _ := d.Store.CoursesByFaculty(fid)

	p := d.pageFrom(r)
	p.Title = course.Code + " · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)

	type enrollOpts struct {
		ID    int64
		Label string
	}
	var opts []enrollOpts
	// Only students not yet enrolled appear in the enroll dropdown.
	enrolled := map[int64]bool{}
	for _, s := range students {
		enrolled[s.ID] = true
	}
	for _, c := range all {
		_ = c
	}
	studentsAll, _ := d.Store.EnrolledStudents(fid)
	for _, s := range studentsAll {
		if !enrolled[s.ID] {
			opts = append(opts, enrollOpts{ID: s.ID, Label: s.Name + " <" + s.Email + ">"})
		}
	}
	d.render(w, r, "faculty_course.html", struct {
		PageData
		Course      *store.Course
		Students    []*store.StudentRow
		Assignments []*store.Assignment
		EnrollOpts  []enrollOpts
	}{p, course, students, assignments, opts}, http.StatusOK)
}

// FacultyCourseUpdate modifies a course.
func (d *Deps) FacultyCourseUpdate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/courses/%d", id)
	if err := d.Store.UpdateCourse(id, fid, fv(r, "name"), fv(r, "code"), fv(r, "description")); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, back+"?ok=Course+updated", http.StatusSeeOther)
}

// FacultyCourseEnroll adds a student to a course (student id or email in the form).
func (d *Deps) FacultyCourseEnroll(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/courses/%d", id)
	var sid int64
	if raw := fv(r, "student_id"); raw != "" {
		sid, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || sid <= 0 {
			d.errRedirect(w, r, back, "Choose a student to enroll.")
			return
		}
	} else if email := fv(r, "email"); email != "" {
		err := d.Store.DB.QueryRow(`SELECT id FROM users WHERE email = ? AND role = 'STUDENT'`,
			strings.ToLower(email)).Scan(&sid)
		if err != nil {
			d.errRedirect(w, r, back, "No student account with that email — create one from the Students page first.")
			return
		}
	} else {
		d.errRedirect(w, r, back, "Choose a student to enroll.")
		return
	}
	if err := d.Store.Enroll(id, sid, fid); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, back+"?ok=Student+enrolled", http.StatusSeeOther)
}

// FacultyCourseUnenroll removes a student.
func (d *Deps) FacultyCourseUnenroll(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/courses/%d", id)
	sid, err := idParam(r, "studentID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := d.Store.Unenroll(id, sid, fid); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, back+"?ok=Student+removed+from+course", http.StatusSeeOther)
}

// FacultyCourseNew shows the create-course form (reuses page).
func (d *Deps) FacultyCourseNew(w http.ResponseWriter, r *http.Request) {
	p := d.pageFrom(r)
	p.Title = "New course · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.render(w, r, "faculty_course_new.html", p, http.StatusOK)
}

// Assignments ---------------------------------------------------------

// FacultyAssignments lists assignments across courses with filters.
func (d *Deps) FacultyAssignments(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courses, _ := d.Store.CoursesByFaculty(fid)
	var list []*store.Assignment
	for _, c := range courses {
		aa, err := d.Store.AssignmentsByCourse(c.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, aa...)
	}
	filter := strings.ToLower(r.URL.Query().Get("filter"))
	switch filter {
	case "draft", "published":
		var kept []*store.Assignment
		for _, a := range list {
			if (filter == "draft" && !a.Published) || (filter == "published" && a.Published) {
				kept = append(kept, a)
			}
		}
		list = kept
	}
	p := d.pageFrom(r)
	p.Title = "Assignments · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_assignments.html", struct {
		PageData
		Assignments []*store.Assignment
		Filter      string
	}{p, list, filter}, http.StatusOK)
}

// FacultyAssignmentDetail renders assignment + rubric editor + submissions link.
func (d *Deps) FacultyAssignmentDetail(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
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
	if a.FacultyID != fid {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this assignment", "This assignment belongs to another faculty member. Only your own courses are listed in the navigation.")
		return
	}
	counts, err := d.Store.AssignmentCounts(a)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = a.Title + " · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	p.Values = map[string]string{"due_date": store.ToInput(a.DueDate)}
	d.render(w, r, "faculty_assignment.html", struct {
		PageData
		Assignment *store.Assignment
		Counts     *store.SubmissionCounts
	}{p, a, counts}, http.StatusOK)
}

// parseCriteriaFromForm turns rubric form fields into criteria structs.
// Fields: crit_title_i, crit_description_i, crit_points_i, crit_topic_i, crit_keywords_i
func parseCriteriaFromForm(r *http.Request) []*store.RubricCriterion {
	order := strings.Split(fv(r, "criteria_order")+",", ",")
	var out []*store.RubricCriterion
	for _, idx := range order {
		if strings.TrimSpace(idx) == "" {
			continue
		}
		title := fv(r, "crit_title_"+idx)
		if title == "" && fv(r, "crit_points_"+idx) == "" {
			continue
		}
		points, _ := strconv.Atoi(fv(r, "crit_points_"+idx))
		out = append(out, &store.RubricCriterion{
			Title:       title,
			Description: fv(r, "crit_description_"+idx),
			MaxPoints:   points,
			Topic:       fv(r, "crit_topic_"+idx),
			Keywords:    fv(r, "crit_keywords_"+idx),
		})
	}
	if len(out) == 0 {
		// Single-criterion fallback when the list was never expanded.
		if fv(r, "crit_title_1") != "" {
			points, _ := strconv.Atoi(fv(r, "crit_points_1"))
			out = append(out, &store.RubricCriterion{
				Title:       fv(r, "crit_title_1"),
				Description: fv(r, "crit_description_1"),
				MaxPoints:   points,
				Topic:       fv(r, "crit_topic_1"),
				Keywords:    fv(r, "crit_keywords_1"),
			})
		}
	}
	return out
}

// FacultyAssignmentCreate POSTs a new assignment with rubric.
func (d *Deps) FacultyAssignmentCreate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, err := idParam(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	total, _ := strconv.Atoi(fv(r, "total_marks"))
	if total == 0 {
		total = 100
	}
	a, err := d.Store.CreateAssignment(fid, courseID, fv(r, "title"), fv(r, "description"),
		fv(r, "topic"), fv(r, "due_date"), total, parseCriteriaFromForm(r))
	if err != nil {
		d.errRedirect(w, r, fmt.Sprintf("/faculty/courses/%d", courseID), humanError(err))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/faculty/assignments/%d", a.ID), http.StatusSeeOther)
}

// FacultyAssignmentUpdate edits assignment metadata (draft only).
func (d *Deps) FacultyAssignmentUpdate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/assignments/%d", id)
	total, _ := strconv.Atoi(fv(r, "total_marks"))
	if err := d.Store.UpdateAssignment(id, fid, fv(r, "title"), fv(r, "description"),
		fv(r, "topic"), fv(r, "due_date"), total, nil); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, back+"?ok=Assignment+updated", http.StatusSeeOther)
}

// FacultyRubricUpdate replaces a draft assignment's rubric.
func (d *Deps) FacultyRubricUpdate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/assignments/%d", id)
	criteria := parseCriteriaFromForm(r)
	total := 0
	for _, c := range criteria {
		total += c.MaxPoints
	}
	if err := d.Store.ReplaceCriteria(id, fid, criteria); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	msg := "Rubric saved."
	q := url.Values{}
	if total != 100 {
		q.Set("err", fmt.Sprintf("Rubric saved but totals %d — publication needs exactly 100.", total))
	} else {
		q.Set("ok", msg+" Rubric is publication-ready.")
	}
	http.Redirect(w, r, back+"?"+q.Encode(), http.StatusSeeOther)
}

// FacultyAssignmentPublish publishes after rubric validation.
func (d *Deps) FacultyAssignmentPublish(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := fmt.Sprintf("/faculty/assignments/%d", id)
	if err := d.Store.PublishAssignment(id, fid); err != nil {
		d.errRedirect(w, r, back, humanError(err))
		return
	}
	http.Redirect(w, r, back+"?ok=Assignment+published", http.StatusSeeOther)
}

// FacultyAssignmentDelete removes a draft.
func (d *Deps) FacultyAssignmentDelete(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	id, err := idParam(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := d.Store.DeleteAssignment(id, fid); err != nil {
		d.errRedirect(w, r, "/faculty/assignments", humanError(err))
		return
	}
	http.Redirect(w, r, "/faculty/assignments?ok=Assignment+deleted", http.StatusSeeOther)
}

// FacultyAssignmentSubmissions shows the monitoring roster view.
func (d *Deps) FacultyAssignmentSubmissions(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
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
	if a.FacultyID != fid {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this assignment", "This assignment belongs to another faculty member. Only your own courses are listed in the navigation.")
		return
	}
	roster, err := d.Store.AssignmentRoster(a)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	counts, _ := d.Store.AssignmentCounts(a)
	p := d.pageFrom(r)
	p.Title = "Submissions · " + a.Title
	p.Nav = d.facultyNav(r)
	p.Values = map[string]string{}
	d.render(w, r, "faculty_submissions.html", struct {
		PageData
		Assignment *store.Assignment
		Roster     []*store.AssignmentSubmissionRow
		Counts     *store.SubmissionCounts
	}{p, a, roster, counts}, http.StatusOK)
}

// FacultySubmissionDownload serves the stored file to authorized faculty.
func (d *Deps) FacultySubmissionDownload(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	sid, err := idParam(r, "submissionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var courseID, assignmentID, studentID int64
	var filePath, clientName string
	err = d.Store.DB.QueryRow(`SELECT a.course_id, a.id, s.student_id, COALESCE(s.file_path,''), COALESCE(s.filename,'')
		FROM submissions s JOIN assignments a ON a.id = s.assignment_id WHERE s.id = ?`, sid).
		Scan(&courseID, &assignmentID, &studentID, &filePath, &clientName)
	if err != nil || filePath == "" {
		http.NotFound(w, r)
		return
	}
	if ok, aerr := d.Store.FacultyOwnsCourse(courseID, fid); aerr != nil || !ok {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this course", "Assignment creation is limited to the courses you teach.")
		return
	}
	f := strings.TrimPrefix(filePath, "./data/uploads/")
	file, err := d.Store.OpenStoredFile(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(clientName)+`"`)
	if _, err := io.Copy(w, file); err != nil {
		_ = err
	}
}

// humanError converts sentinel store errors to useful messages.
func humanError(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "That record no longer exists."
	case errors.Is(err, store.ErrDuplicate):
		return err.Error()
	case errors.Is(err, store.ErrForbidden):
		return "You are not allowed to perform that action."
	case errors.Is(err, store.ErrInvalid):
		return err.Error()
	case errors.Is(err, store.ErrTimeFormat):
		return "Choose a valid due date and time."
	}
	return strings.TrimSpace(fmt.Sprint(err))
}
