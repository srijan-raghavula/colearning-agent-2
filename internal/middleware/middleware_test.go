package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFValidAndInvalid(t *testing.T) {
	c := CSRF{Secret: "unit-secret"}
	token := c.Token("session-abc")
	if token == "" {
		t.Fatal("empty token")
	}
	if !c.Verify("session-abc", token) {
		t.Fatal("valid token rejected")
	}
	if c.Verify("other-session", token) {
		t.Fatal("token from another session accepted")
	}
	if c.Verify("session-abc", "") {
		t.Fatal("empty submission accepted")
	}
}

func TestCSRFAnonymousFlow(t *testing.T) {
	c := CSRF{Secret: "unit-secret"}
	r := httptest.NewRequest("POST", "/login", nil)
	w := httptest.NewRecorder()
	anon := c.AnonymousToken(w, r)
	if anon == "" {
		t.Fatal("anonymous token empty")
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "educore_csrf=") {
		t.Fatal("anonymous flow must issue csrf cookie")
	}
	// Replay: request carries the issued cookie, submit the token.
	r2 := httptest.NewRequest("POST", "/login", nil)
	r2.AddCookie(valueCookie("educore_csrf", cookieValue(w)))
	if !c.VerifyAnonymous(r2, anon) {
		t.Fatal("anonymous verification failed")
	}
	if c.VerifyAnonymous(r2, "bob") {
		t.Fatal("forged anonymous token accepted")
	}
	if c.VerifyAnonymous(httptest.NewRequest("POST", "/login", nil), anon) {
		t.Fatal("missing cookie accepted")
	}
}

func cookieValue(w *httptest.ResponseRecorder) string {
	sc := w.Header().Get("Set-Cookie")
	for _, part := range strings.Split(sc, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "educore_csrf=") {
			return strings.TrimPrefix(part, "educore_csrf=")
		}
	}
	return ""
}

func TestCSRFLoginFlowShape(t *testing.T) {
	// Session token path must equal what RequireCSRF computes.
	c := CSRF{Secret: "s"}
	tok := c.Token("sess")
	if !c.Verify("sess", tok) {
		t.Fatal("sanity")
	}
}
