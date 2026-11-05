package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"educore/internal/auth"
	"educore/internal/database"
	"educore/internal/middleware"
	"educore/internal/store"
)

// mockAI is reserved for later phases; Phase 2 handlers need none.
type mockAI struct{}

const testCSRFSecret = "test-csrf-secret-please-ignore-0001"

func newDeps(t *testing.T) (*Deps, *httptest.Server) {
	t.Helper()
	cwd, _ := os.Getwd()
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	db, err := database.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { database.Close(db) })
	svc := auth.NewService(db)
	if _, err := svc.CreateAccount("faculty@educore.demo", "Dr. Priya Sharma", "Faculty123!", "FACULTY"); err != nil {
		t.Fatalf("seed faculty: %v", err)
	}
	if _, err := svc.CreateAccount("student@educore.demo", "Arjun Mehta", "Student123!", "STUDENT"); err != nil {
		t.Fatalf("seed student: %v", err)
	}
	tpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	deps := &Deps{Auth: svc, CSRF: middleware.CSRF{Secret: testCSRFSecret}, Tpl: tpl, AIOn: false, Store: store.New(db)}
	server := httptest.NewServer(deps.Handler())
	t.Cleanup(server.Close)
	return deps, server
}

// followRedirects=false client with cookie jar.
func client(t *testing.T, follow bool) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !follow {
				return http.ErrUseLastResponse
			}
			if len(via) > 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func getCSRF(t *testing.T, c *http.Client, serverURL string) string {
	t.Helper()
	resp, err := c.Get(serverURL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login page status = %d", resp.StatusCode)
	}
	html := string(body)
	idx := strings.Index(html, `name="_csrf" value="`)
	if idx < 0 {
		t.Fatalf("csrf hidden input missing on login page")
	}
	rest := html[idx+len(`name="_csrf" value="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("csrf value unterminated")
	}
	return rest[:end]
}

func postForm(t *testing.T, c *http.Client, url string, form url.Values) *http.Response {
	t.Helper()
	resp, err := c.PostForm(url, form)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func eventually(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(format, args...)
	}
}

func assertRedirect(t *testing.T, resp *http.Response, want string, desc string) {
	t.Helper()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || loc != want {
		t.Fatalf("%s: status=%d location=%q want %d %q", desc, resp.StatusCode, loc, http.StatusSeeOther, want)
	}
}

func assertContains(t *testing.T, resp *http.Response, needle, desc string) {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), needle) {
		t.Fatalf("%s: body missing %q", desc, needle)
	}
}

func TestUnauthenticatedAccessBlocked(t *testing.T) {
	_, server := newDeps(t)
	c := client(t, false)
	resp, err := c.Get(server.URL + "/faculty")
	eventually(t, err == nil, "GET /faculty: %v", err)
	assertRedirect(t, resp, "/login?next=/faculty", "guest /faculty")
	resp, err = c.Get(server.URL + "/student")
	eventually(t, err == nil, "GET /student: %v", err)
	assertRedirect(t, resp, "/login?next=/student", "guest /student")
}

func TestRoleMiddleware(t *testing.T) {
	_, server := newDeps(t)

	// Student cannot reach faculty routes.
	cs := client(t, false)
	tok := getCSRF(t, cs, server.URL)
	resp := postForm(t, cs, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"student@educore.demo"}, "password": {"Student123!"},
	})
	defer resp.Body.Close()
	assertRedirect(t, resp, "/student", "student login")

	resp, err := cs.Get(server.URL + "/faculty")
	eventually(t, err == nil, "student GET /faculty: %v", err)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("student reached /faculty: status=%d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/student" {
		t.Fatalf("student /faculty redirect = %q, want /student", loc)
	}

	// Faculty cannot reach student routes.
	cf := client(t, false)
	tok = getCSRF(t, cf, server.URL)
	resp = postForm(t, cf, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"faculty@educore.demo"}, "password": {"Faculty123!"},
	})
	defer resp.Body.Close()
	assertRedirect(t, resp, "/faculty", "faculty login")

	resp, err = cf.Get(server.URL + "/student")
	eventually(t, err == nil, "faculty GET /student: %v", err)
	if resp.Header.Get("Location") != "/faculty" {
		t.Fatalf("faculty reached /student (location=%q)", resp.Header.Get("Location"))
	}
}

func TestLoginValidAndInvalid(t *testing.T) {
	_, server := newDeps(t)
	c := client(t, false)
	tok := getCSRF(t, c, server.URL)

	// Wrong password stays on the login page with an error.
	resp := postForm(t, c, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"faculty@educore.demo"}, "password": {"nope"},
	})
	assertContains(t, resp, "Invalid email or password.", "wrong password")
	// No session cookie was minted for a failed login.
	for _, ck := range c.Jar.Cookies(&url.URL{Scheme: "http", Host: strings.TrimPrefix(server.URL, "http://")}) {
		if ck.Name == "educore_session" && ck.Value != "" {
			t.Fatalf("session cookie issued on failed login")
		}
	}

	// Correct credentials redirect to the role home page.
	tok = getCSRF(t, c, server.URL)
	resp = postForm(t, c, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"faculty@educore.demo"}, "password": {"Faculty123!"},
	})
	defer resp.Body.Close()
	assertRedirect(t, resp, "/faculty", "valid login")
}

func TestLoginRejectsBadCSRF(t *testing.T) {
	_, server := newDeps(t)
	c := client(t, false)
	resp := postForm(t, c, server.URL+"/login", url.Values{
		"_csrf": {"forged-token"}, "email": {"faculty@educore.demo"}, "password": {"Faculty123!"},
	})
	assertContains(t, resp, "token expired", "bad csrf")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad csrf status = %d, want 403", resp.StatusCode)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	_, server := newDeps(t)
	c := client(t, false)
	tok := getCSRF(t, c, server.URL)
	resp := postForm(t, c, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"student@educore.demo"}, "password": {"Student123!"},
	})
	resp.Body.Close()

	// Authenticated placeholder reachable.
	resp, err := c.Get(server.URL + "/student")
	eventually(t, err == nil, "student home: %v", err)
	assertContains(t, resp, "Arjun Mehta", "student placeholder identity")

	// POST to logout is CSRF-protected as a state-changing call.
	resp2, err := c.PostForm(server.URL+"/logout", url.Values{})
	if err != nil {
		t.Fatalf("logout post: %v", err)
	}
	assertContains(t, resp2, "expired or the form token is invalid", "logout csrf")
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d, want 403", resp2.StatusCode)
	}
	resp2.Body.Close()

	// Logout with the CSRF token then the session must be dead.
	resp, err = c.Get(server.URL + "/student")
	eventually(t, err == nil, "student home prelogout: %v", err)
	html := readBody(t, resp)
	csrfField := extractCSRF(html)
	resp, err = c.PostForm(server.URL+"/logout", url.Values{"_csrf": {csrfField}})
	eventually(t, err == nil, "logout: %v", err)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	resp, err = c.Get(server.URL + "/student")
	eventually(t, err == nil, "student home after logout: %v", err)
	assertRedirect(t, resp, "/login?next=/student", "post-logout access")
}

func extractCSRF(html string) string {
	idx := strings.Index(html, `name="_csrf" value="`)
	if idx < 0 {
		return ""
	}
	rest := html[idx+len(`name="_csrf" value="`):]
	if end := strings.Index(rest, `"`); end >= 0 {
		return rest[:end]
	}
	return ""
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(body)
}

func TestSessionRotationOnLogin(t *testing.T) {
	deps, server := newDeps(t)
	c := client(t, false)
	u, err := deps.Auth.Authenticate("faculty@educore.demo", "Faculty123!")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	old, err := deps.Auth.CreateSession(u.ID, time.Hour)
	if err != nil {
		t.Fatalf("preseed session: %v", err)
	}
	tok := getCSRF(t, c, server.URL)
	resp := postForm(t, c, server.URL+"/login", url.Values{
		"_csrf": {tok}, "email": {"faculty@educore.demo"}, "password": {"Faculty123!"},
	})
	resp.Body.Close()
	if _, err := deps.Auth.LookupSession(old); err == nil {
		t.Fatalf("old session survived login rotation")
	}
}

// loginAs performs the CSRF-protected login round-trip for any account.
func loginAs(t *testing.T, c *http.Client, base, email, pass string) {
	t.Helper()
	token := getCSRF(t, c, base)
	resp := postForm(t, c, base+"/login", url.Values{
		"_csrf": {token}, "email": {email}, "password": {pass},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc == "" || strings.Contains(loc, "err") {
		t.Fatalf("login as %s failed: location=%q", email, loc)
	}
}
