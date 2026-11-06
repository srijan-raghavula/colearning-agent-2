// Package handlers implements the HTTP routes for Phase 2: authentication UI
// and authenticated role placeholders.
package handlers

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"
	"unicode"

	"strings"

	"educore/internal/agent"
	"educore/internal/auth"
	"educore/internal/charts"
	"educore/internal/middleware"
	"educore/internal/store"
)

// Deps bundles handler dependencies.
type Deps struct {
	Auth         *auth.Service
	CSRF         middleware.CSRF
	Tpl          map[string]*template.Template
	AIOn         bool
	LearningAI   LearningAIProvider
	Store        *store.Store
	WorkerNotify chan struct{}
	AgentEngine  *agent.Engine
	Dev          bool // hot-reload templates per request (development)
}

// NavEntry is one sidebar link.
type NavEntry struct {
	Href   string
	Label  string
	Icon   string
	Active bool
}

// PageData is the payload passed to every page template.
type PageData struct {
	Title     string
	UserName  string
	UserEmail string
	Role      string
	RoleLabel string
	CSRFValue string
	Error     string
	Notice    string
	Flash     string
	Nav       []NavEntry
	Values    map[string]string // form repopulation after validation errors
}

// LoadTemplates parses templates/pages/* with layout + partials merged.
func LoadTemplates() (map[string]*template.Template, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	shell, err := os.ReadFile(filepath.Join(root, "templates", "layout.html"))
	if err != nil {
		return nil, err
	}
	partialsDir := filepath.Join(root, "templates", "partials")
	partials, err := loadDir(partialsDir)
	if err != nil {
		return nil, err
	}
	pagesDir := filepath.Join(root, "templates", "pages")
	pageEntries, err := os.ReadDir(pagesDir)
	if err != nil {
		return nil, err
	}
	sets := map[string]*template.Template{}
	for _, e := range pageEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		name := e.Name()
		pageSrc, err := os.ReadFile(filepath.Join(pagesDir, name))
		if err != nil {
			return nil, err
		}
		t := template.New(name).Funcs(templateFuncs())
		t, err = t.New("layout").Parse(string(shell))
		if err != nil {
			return nil, err
		}
		for pname, psrc := range partials {
			if t, err = t.New(pname).Parse(psrc); err != nil {
				return nil, err
			}
		}
		if t, err = t.New(name).Parse(string(pageSrc)); err != nil {
			return nil, err
		}
		sets[name] = t
	}
	return sets, nil
}

// templateFuncs are shared formatting helpers available to all templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				if k, ok := kv[i].(string); ok {
					m[k] = kv[i+1]
				}
			}
			return m
		},
		"flashctx": func(d any) map[string]any {
			// Exposes Flash/Error/Notice fields of a PageData-ish value.
			out := map[string]any{}
			v := reflect.ValueOf(d)
			for _, f := range []string{"Flash", "Error", "Notice", "Ok"} {
				field := v.FieldByName(f)
				if field.IsValid() {
					out[f] = field.Interface()
				}
			}
			return out
		},
		"truncate": func(s string, n int) string {
			r := []rune(s)
			if len(r) <= n {
				return s
			}
			return string(r[:n-1]) + "…"
		},
		"formatDate": func(s string) string {
			t, err := store.ParseStored(s)
			if err != nil {
				return s
			}
			return t.Local().Format("02 Jan 2006")
		},
		"formatDue":   func(s string) string { return store.FormatDue(s) },
		"toInput":     func(s string) string { return store.ToInput(s) },
		"statusText":  func(st string) string { return StatusLabel(st) },
		"statusClass": func(st string) string { return StatusClass(st) },
		"add":         func(a, b int) int { return a + b },
		"sub":         func(a, b int) int { return a - b },
		"percent":     func(f float64) string { return fmt.Sprintf("%.0f%%", f) },
		"inc":         func(i int) int { return i + 1 },
		"seval":       func(i any) any { return i },
		"boolyes": func(b bool) string {
			if b {
				return "Yes"
			}
			return ""
		},
		"evalStatusText": evalStatusText,
		"agentStatusClass": func(s string) string {
			switch s {
			case "RUNNING":
				return "badge-info"
			case "DONE":
				return "badge-success"
			case "FAILED":
				return "badge-error"
			}
			return "badge-neutral"
		},
		"agentEventClass": func(s string) string {
			switch s {
			case "TOOL_RESULT":
				return "badge-success"
			case "TOOL_ERROR", "ERROR":
				return "badge-error"
			case "FINAL":
				return "badge-success"
			case "MODEL_CALL":
				return "badge-info"
			case "TOOL_CALL":
				return "badge-info"
			case "USER_MSG":
				return "badge-info"
			}
			return "badge-neutral"
		},
		"evalStatusClass": evalStatusClass,
		"hasPrefix":       strings.HasPrefix,
		"fmtPoints":       func(v float64) string { return fmt.Sprintf("%.2f", v) },
		"icon":            func(name string) template.HTML { return icon(name) },
		"initials":        initialsOf,
		"greet":           greetingPrefix,
		"progress":        func(p float64) template.HTML { return charts.Progress(p) },
		"percentClass":    func(p float64) string { return pctClass(p) },
	}
}

// initialsOf: first letters of up to two name words for the avatar chip.
func initialsOf(name string) string {
	if strings.TrimSpace(name) == "" {
		return "?"
	}
	parts := strings.Fields(name)
	var out []rune
	for i, p := range parts {
		if i >= 2 {
			break
		}
		r := []rune(p)
		out = append(out, unicode.ToUpper(r[0]))
	}
	return strings.TrimSpace(string(out))
}

func init() { /* icon/initials helpers live in icons.go; no init-time config needed */ }

// evalStatusText is the label for the evaluation lifecycle statuses.
func evalStatusText(st string) string {
	switch st {
	case store.EvalNotRequested:
		return "Received"
	case store.EvalPending:
		return "Evaluation in progress"
	case store.EvalRunning:
		return "Evaluation in progress"
	case store.EvalNeedsReview:
		return "Awaiting faculty review"
	case store.EvalApproved:
		return "Results published"
	case store.EvalFailed:
		return "Evaluation failed"
	}
	return st
}

// evalStatusClass maps evaluation statuses to restrained badge colors.
func evalStatusClass(st string) string {
	switch st {
	case store.EvalPending, store.EvalRunning:
		return "badge-info"
	case store.EvalNeedsReview:
		return "badge-warning"
	case store.EvalApproved:
		return "badge-success"
	case store.EvalFailed:
		return "badge-error"
	}
	return "badge-neutral"
}

func loadDir(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = string(src)
	}
	return out, nil
}

// render writes a named page template with any payload.
func (d *Deps) render(w http.ResponseWriter, _ *http.Request, page string, data any, status int) {
	t, ok := d.Tpl[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

// renderShellError renders an authorization or routing error inside the
// application shell so the sidebar stays available.
func (d *Deps) renderShellError(w http.ResponseWriter, r *http.Request, status int, headline, body string) {
	p := d.pageFrom(r)
	if p.UserName == "" { // not authenticated: plain error page
		http.Error(w, headline, status)
		return
	}
	p.Title = "Not available · EDUCORE"
	d.render(w, r, "error_inapp.html", struct {
		PageData
		Headline string
		Body     string
	}{p, headline, body}, status)
}

func (d *Deps) pageFrom(r *http.Request) PageData {
	p := PageData{}
	if u, ok := middleware.UserFrom(r); ok {
		p.UserName = u.Name
		p.UserEmail = u.Email
		p.Role = u.Role
		if u.IsFaculty() {
			p.RoleLabel = "Faculty"
			p.Nav = d.facultyNav(r)
		} else {
			p.RoleLabel = "Student"
			p.Nav = d.studentNav(r)
		}
		p.CSRFValue = d.CSRF.Token(middleware.TokenFrom(r))
	}
	return p
}

// Home routes "/" to the right landing page.
func (d *Deps) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if u, ok := middleware.UserFrom(r); ok {
		http.Redirect(w, r, middleware.HomePath(u.Role), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// LoginForm shows the login page.
func (d *Deps) LoginForm(w http.ResponseWriter, r *http.Request) {
	p := PageData{
		Title:     "Sign in · EDUCORE",
		CSRFValue: d.CSRF.AnonymousToken(w, r),
	}
	d.render(w, r, "login.html", p, http.StatusOK)
}

// LoginPost authenticates a user and rotates the session on success.
func (d *Deps) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		d.renderError(w, r, "Bad form submission.", http.StatusBadRequest)
		return
	}
	if !d.CSRF.VerifyAnonymous(r, r.PostFormValue("_csrf")) {
		d.renderError(w, r, "Your form token expired. Reload the page and try again.", http.StatusForbidden)
		return
	}
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")
	user, err := d.Auth.Authenticate(email, password)
	if err != nil {
		status, msg := http.StatusUnauthorized, "Invalid email or password."
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			log.Printf("login error for %q: %v", email, err)
			status, msg = http.StatusInternalServerError, "Something went wrong. Please try again."
		}
		p := d.pageFrom(r)
		p.Title = "Sign in · EDUCORE"
		p.Error = msg
		p.Notice = email
		d.render(w, r, "login.html", p, status)
		return
	}
	// Session rotation: drop all existing sessions for this user, then issue
	// a fresh token so a stolen/reused cookie is invalidated on login.
	if err := d.Auth.RevokeUserSessions(user.ID); err != nil {
		log.Printf("session rotation: %v", err)
	}
	token, err := d.Auth.CreateSession(user.ID, auth.SessionLifetime)
	if err != nil {
		log.Printf("create session: %v", err)
		d.renderError(w, r, "Could not start a session. Please try again.", http.StatusInternalServerError)
		return
	}
	middleware.SetSessionCookie(w, r, token, int(auth.SessionLifetime/time.Second))
	log.Printf("login ok: %s (%s)", user.Email, user.Role)
	http.Redirect(w, r, middleware.HomePath(user.Role), http.StatusSeeOther)
}

func (d *Deps) renderError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	p := PageData{Title: "Sign in · EDUCORE", Error: msg, CSRFValue: d.CSRF.AnonymousToken(w, r)}
	d.render(w, r, "login.html", p, status)
}

// Logout revokes the current session and shows a signed-out page.
func (d *Deps) Logout(w http.ResponseWriter, r *http.Request) {
	_ = d.Auth.Revoke(middleware.TokenFrom(r))
	middleware.ClearSessionCookie(w, r)
	p := PageData{Title: "Signed out · EDUCORE"}
	d.render(w, r, "loggedout.html", p, http.StatusOK)
}

// FacultyHome is the protected faculty placeholder.
func (d *Deps) FacultyHome(w http.ResponseWriter, r *http.Request) {
	d.render(w, r, "faculty.html", d.pageFrom(r), http.StatusOK)
}

// StudentHome is the protected student placeholder.
func (d *Deps) StudentHome(w http.ResponseWriter, r *http.Request) {
	d.render(w, r, "student.html", d.pageFrom(r), http.StatusOK)
}

// Health returns 200 for uptime checks.
func (d *Deps) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprint(w, "ok")
}

// Routes builds the Phase 2 router. State-changing POSTs are CSRF-protected;
// the session middleware wraps the whole mux.
func (d *Deps) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", d.Home)
	mux.Handle("GET /login", middleware.Guest(http.HandlerFunc(d.LoginForm)))
	mux.HandleFunc("POST /login", d.LoginPost)
	mux.Handle("/logout", middleware.RequireAuth(d.CSRF.RequireCSRF(http.HandlerFunc(d.Logout))))

	// ----- Faculty: courses -----
	mux.HandleFunc("GET /faculty", d.FacultyOverview)
	mux.HandleFunc("GET /faculty/courses", d.FacultyCourses)
	mux.HandleFunc("GET /faculty/courses/new", d.FacultyCourseNew)
	mux.HandleFunc("POST /faculty/courses", d.CSRFRequire(d.FacultyCourseCreate))
	mux.HandleFunc("GET /faculty/courses/{id}", d.FacultyCourseDetail)
	mux.HandleFunc("POST /faculty/courses/{id}", d.CSRFRequire(d.FacultyCourseUpdate))
	mux.HandleFunc("POST /faculty/courses/{id}/students", d.CSRFRequire(d.FacultyCourseEnroll))
	mux.HandleFunc("POST /faculty/courses/{id}/students/{studentID}/remove", d.CSRFRequire(d.FacultyCourseUnenroll))
	mux.HandleFunc("GET /faculty/courses/{courseID}/chapters", d.FacultyChapters)
	mux.HandleFunc("GET /faculty/courses/{courseID}/chapters/new", d.FacultyChapterNew)
	mux.HandleFunc("POST /faculty/courses/{courseID}/chapters", d.CSRFRequire(d.FacultyChapterCreate))
	mux.HandleFunc("GET /faculty/courses/{courseID}/chapters/{chapterID}", d.FacultyChapterEdit)
	mux.HandleFunc("POST /faculty/courses/{courseID}/chapters/{chapterID}", d.CSRFRequire(d.FacultyChapterUpdate))
	mux.HandleFunc("POST /faculty/courses/{courseID}/chapters/{chapterID}/delete", d.CSRFRequire(d.FacultyChapterDelete))

	// ----- Faculty: assignments and rubrics -----
	mux.HandleFunc("GET /faculty/assignments", d.FacultyAssignments)
	mux.HandleFunc("GET /faculty/assignments/{id}", d.FacultyAssignmentDetail)
	mux.HandleFunc("GET /faculty/assignments/{id}/submissions", d.FacultyAssignmentSubmissions)
	mux.HandleFunc("POST /faculty/assignments/{id}", d.CSRFRequire(d.FacultyAssignmentUpdate))
	mux.HandleFunc("POST /faculty/assignments/{id}/rubric", d.CSRFRequire(d.FacultyRubricUpdate))
	mux.HandleFunc("POST /faculty/assignments/{id}/publish", d.CSRFRequire(d.FacultyAssignmentPublish))
	mux.HandleFunc("POST /faculty/assignments/{id}/delete", d.CSRFRequire(d.FacultyAssignmentDelete))
	mux.HandleFunc("GET /faculty/courses/{courseID}/assignments/new", d.FacultyAssignmentNew)
	mux.HandleFunc("POST /faculty/courses/{courseID}/assignments", d.CSRFRequire(d.FacultyAssignmentCreate))

	// ----- Faculty: agent evaluation console -----
	mux.HandleFunc("GET /faculty/submissions/{submissionID}/agent", d.AgentConsole)
	mux.HandleFunc("POST /faculty/submissions/{submissionID}/agent/start", d.CSRFRequire(d.AgentRunStart))
	mux.HandleFunc("POST /faculty/submissions/{submissionID}/agent/reconsider", d.CSRFRequire(d.AgentRunReconsider))
	mux.HandleFunc("POST /faculty/submissions/{submissionID}/agent/followup", d.CSRFRequire(d.AgentRunFollowup))
	mux.HandleFunc("GET /faculty/agent/runs/{runID}", d.AgentRunDetail)
	mux.HandleFunc("POST /faculty/agent/runs/{runID}/start", d.CSRFRequire(d.AgentRunRetry))
	mux.HandleFunc("POST /faculty/agent/runs/{runID}/message", d.CSRFRequire(d.AgentRunMessage))
	mux.HandleFunc("POST /faculty/agent/runs/{runID}/reconsider", d.CSRFRequire(d.AgentRunReconsiderFrom))
	mux.HandleFunc("GET /faculty/submissions/{submissionID}", d.FacultySubmissionReview)
	mux.HandleFunc("GET /faculty/submissions/{submissionID}/download", d.FacultySubmissionDownload)
	mux.HandleFunc("POST /faculty/submissions/{submissionID}/evaluate", d.CSRFRequire(d.FacultyTriggerEvaluate))
	mux.HandleFunc("POST /faculty/submissions/{submissionID}/manual", d.CSRFRequire(d.FacultyManualGrade))
	mux.HandleFunc("GET /faculty/evaluations", d.FacultyEvalOverview)
	mux.HandleFunc("POST /faculty/evaluations/{evalID}/range/{criterionID}", d.CSRFRequire(d.FacultyOverrideCriterion))
	mux.HandleFunc("POST /faculty/evaluations/{evalID}/approve", d.CSRFRequire(d.FacultyApproveEvaluation))
	mux.HandleFunc("POST /faculty/evaluations/{evalID}/unpublish", d.CSRFRequire(d.FacultyUnpublishEvaluation))

	// ----- Faculty: students -----
	mux.HandleFunc("GET /faculty/students", d.FacultyStudents)
	mux.HandleFunc("POST /faculty/students", d.CSRFRequire(d.FacultyStudentCreate))

	// ----- Student -----
	mux.HandleFunc("GET /student", d.FacultyOverviewStudent)
	mux.HandleFunc("GET /student/courses", d.StudentCourses)
	mux.HandleFunc("GET /student/courses/{id}", d.StudentCourseDetail)
	mux.HandleFunc("GET /student/courses/{id}/chapters/{chapterID}", d.StudentChapterDetail)
	mux.HandleFunc("GET /student/learning-studio", d.StudentLearningStudio)
	mux.HandleFunc("POST /student/courses/{id}/chapters/{chapterID}/lesson", d.CSRFRequire(d.StudentChapterLesson))
	mux.HandleFunc("POST /student/courses/{id}/chapters/{chapterID}/chat", d.CSRFRequire(d.StudentChapterChat))
	mux.HandleFunc("POST /student/courses/{id}/chapters/{chapterID}/quiz", d.CSRFRequire(d.StudentChapterQuiz))
	mux.HandleFunc("POST /student/courses/{id}/chapters/{chapterID}/quiz/submit", d.CSRFRequire(d.StudentChapterQuizSubmit))
	mux.HandleFunc("GET /student/assignments", d.StudentAssignments)
	mux.HandleFunc("GET /student/assignments/{id}", d.StudentAssignmentDetail)
	mux.HandleFunc("POST /student/assignments/{id}/submit", d.CSRFRequire(d.StudentSubmit))
	mux.HandleFunc("GET /student/assignments/{id}/download", d.StudentSubmissionDownload)
	mux.HandleFunc("GET /student/performance", d.StudentPerformance)

	mux.HandleFunc("GET /healthz", d.Health)
	mux.Handle("GET /static/", http.NotFoundHandler())
	return mux
}

// CSRFRequire gates a state-changing handler with the CSRF check.
func (d *Deps) CSRFRequire(next http.HandlerFunc) http.HandlerFunc {
	return d.CSRF.RequireCSRF(next).ServeHTTP
}

// FacultyAssignmentNew shows the create-assignment form with rubric editor.
func (d *Deps) FacultyAssignmentNew(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	cid, err := idParam(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok, aerr := d.Store.FacultyOwnsCourse(cid, fid); aerr != nil || !ok {
		http.Error(w, "Not your course.", http.StatusForbidden)
		return
	}
	course, err := d.Store.CourseByID(cid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := d.pageFrom(r)
	p.Title = "New assignment · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.render(w, r, "faculty_assignment_new.html", struct {
		PageData
		Course *store.Course
	}{p, course}, http.StatusOK)
}

// Handler returns the fully-wrapped HTTP handler: session resolution plus
// role enforcement by URL prefix. Faculty paths require the FACULTY role,
// student paths require STUDENT; login/static/health are public.
func (d *Deps) Handler() http.Handler {
	routes := d.Routes()
	roleMiddleware := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, authed := middleware.UserFrom(r)
		switch {
		case strings.HasPrefix(r.URL.Path, "/faculty"):
			if !authed {
				http.Redirect(w, r, "/login?next=/faculty", http.StatusSeeOther)
				return
			}
			if u.Role != "FACULTY" {
				http.Redirect(w, r, middleware.HomePath(u.Role), http.StatusSeeOther)
				return
			}
		case strings.HasPrefix(r.URL.Path, "/student"):
			if !authed {
				http.Redirect(w, r, "/login?next=/student", http.StatusSeeOther)
				return
			}
			if u.Role != "STUDENT" {
				http.Redirect(w, r, middleware.HomePath(u.Role), http.StatusSeeOther)
				return
			}
		}
		routes.ServeHTTP(w, r)
	})
	return middleware.Auth{Service: d.Auth}.WithSession(roleMiddleware)
}

// applyFlash maps redirect messages (err/ok/flash) onto page data.
func (d *Deps) applyFlash(r *http.Request, p *PageData) {
	q := r.URL.Query()
	p.Error = q.Get("err")
	p.Notice = q.Get("ok")
	p.Flash = q.Get("flash")
}
