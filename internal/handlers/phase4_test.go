package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"educore/internal/evaluator"
	"educore/internal/store"
)

func phase4Worker(t *testing.T, deps *Deps) *store.Worker {
	t.Helper()
	return store.NewWorker(deps.Store, &evaluator.Engine{})
}

// runWorker processes every queued job synchronously.
func runWorker(t *testing.T, deps *Deps) {
	t.Helper()
	w := phase4Worker(t, deps)
	for w.ProcessNext() {
	}
}

func setupEvalApp(t *testing.T) (*Deps, *httptest.Server, *http.Client, *http.Client) {
	deps, server := newDeps(t)
	fc := client(t, false)
	loginAs(t, fc, server.URL, "faculty@educore.demo", "Faculty123!")
	sc := client(t, false)
	loginAs(t, sc, server.URL, "student@educore.demo", "Student123!")
	cid := createCourse(t, server.URL, fc)
	enrollSeedStudent(t, server.URL, fc, cid)
	due := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02T15:04")
	loc := createAssignment(t, server.URL, fc, cid, due, 60, 40)
	aid := loc[strings.LastIndex(loc, "/")+1:]
	publishAssignment(t, server.URL, fc, aid)
	csrf := extractCSRF(getPage(t, sc, server.URL+"/student/assignments/"+aid))
	resp := postForm(t, sc, server.URL+"/student/assignments/"+aid+"/submit", url.Values{"_csrf": {csrf},
		"content": {"BFS uses a queue; DFS uses a stack and visits all vertices."},
	})
	resp.Body.Close()
	runWorker(t, deps)
	return deps, server, fc, sc
}

func TestPhase4FullFlow(t *testing.T) {
	deps, server, fc, sc := setupEvalApp(t)

	// 1: DEMO evaluation landed, provisional, low confidence.
	depsState := getPage(t, fc, server.URL+"/faculty/submissions/1")
	if !strings.Contains(depsState, "DEMO") || !strings.Contains(depsState, "Awaiting faculty review") {
		t.Fatalf("review page missing demo result: %d bytes", len(depsState))
	}
	if !strings.Contains(depsState, "Provisional result") {
		t.Fatalf("no provisional banner: %s", snippet(depsState, "notice"))
	}

	// 2: student must NOT see provisional marks.
	studentPage := getPage(t, sc, server.URL+"/student/assignments/1")
	if strings.Contains(studentPage, "Published results") || strings.Contains(studentPage, "badge-success") {
		t.Fatalf("provisional marks leaked to student")
	}
	if !strings.Contains(studentPage, "awaiting faculty review") {
		t.Fatalf("student status missing: %s", snippet(studentPage, "notice"))
	}

	// 3: override criterion 1 → 45, then approve.
	csrf := extractCSRF(depsState)
	c1 := traversalCriterion(t, deps)
	resp := postForm(t, fc, server.URL+"/faculty/evaluations/1/range/"+it64(c1), url.Values{
		"_csrf": {csrf}, "points": {"45"}, "feedback": {"argue complexity"},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("override failed: %s", loc)
	}
	csrf = extractCSRF(getPage(t, fc, server.URL+"/faculty/submissions/1"))
	resp = postForm(t, fc, server.URL+"/faculty/evaluations/1/approve", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("approve failed: %s", loc)
	}

	// 4: student now sees published results.
	studentPage = getPage(t, sc, server.URL+"/student/assignments/1")
	if !strings.Contains(studentPage, "Published results") || !strings.Contains(studentPage, "45.00") {
		t.Fatalf("approved result missing for student")
	}

	// 5: re-evaluate → new attempt, history preserved.
	csrf = extractCSRF(getPage(t, fc, server.URL+"/faculty/submissions/1"))
	resp = postForm(t, fc, server.URL+"/faculty/submissions/1/evaluate", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("re-evaluate failed: %s", loc)
	}
	runWorker(t, deps)
	deps2 := deps.Store
	var attempts int
	if err := deps2.DB.QueryRow(`SELECT COUNT(1) FROM evaluations WHERE submission_id = 1`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempt history lost: %d", attempts)
	}
	// re-evaluation is provisional again
	var st string
	_ = deps2.DB.QueryRow(`SELECT eval_status FROM submissions WHERE id = 1`).Scan(&st)
	if st != store.EvalNeedsReview {
		t.Fatalf("re-evaluations should be provisional: %s", st)
	}
}

func traversalCriterion(t *testing.T, deps *Deps) int64 {
	t.Helper()
	var id int64
	if err := deps.Store.DB.QueryRow(`SELECT criterion_id FROM criterion_results WHERE evaluation_id = 1 ORDER BY criterion_id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func it64(i int64) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func snippet(s, tag string) string { return s } // replaced below if needed

func TestAuthzEvaluationEndpoints(t *testing.T) {
	_, server, fc, sc := setupEvalApp(t)

	// Student cannot trigger evaluation / approve / override.
	csrfPage := getPage(t, sc, server.URL+"/student/assignments/1")
	_ = csrfPage
	resp := postForm(t, sc, server.URL+"/faculty/submissions/1/evaluate", url.Values{"_csrf": {"x"}})
	resp.Body.Close()
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student triggered evaluation: %s", resp.Header.Get("Location"))
	}
	resp = postForm(t, sc, server.URL+"/faculty/evaluations/1/approve", url.Values{"_csrf": {"x"}})
	resp.Body.Close()
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student approved: %s", resp.Header.Get("Location"))
	}
	resp = postForm(t, sc, server.URL+"/faculty/evaluations/1/range/1", url.Values{"_csrf": {"x"}, "points": {"99"}})
	resp.Body.Close()
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student overrode: %s", resp.Header.Get("Location"))
	}

	// Faculty review page itself blocks students.
	resp, err := sc.Get(server.URL + "/faculty/submissions/1")
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student reached review page")
	}

	// CSRF on the real mutation: a faculty POST without a valid token → 403.
	fenced := fc
	fresp := postForm(t, fenced, server.URL+"/faculty/submissions/1/evaluate", url.Values{"_csrf": {"forged"}})
	if fresp.StatusCode != 403 {
		t.Fatalf("forged CSRF not rejected: %d", fresp.StatusCode)
	}
	fresp.Body.Close()

}
