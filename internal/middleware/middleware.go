// Package middleware provides session authentication, role gating, and CSRF
// protection for the net/http server.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"log"
	"net/http"
	"time"

	"educore/internal/auth"
	"educore/internal/models"
)

type ctxKey int

const (
	userKey ctxKey = iota
	tokenKey
)

// UserFrom extracts the authenticated user from the request context.
func UserFrom(r *http.Request) (models.User, bool) {
	u, ok := r.Context().Value(userKey).(models.User)
	return u, ok && u.ID != 0
}

// TokenFrom extracts the current session token from the request context.
func TokenFrom(r *http.Request) string {
	t, _ := r.Context().Value(tokenKey).(string)
	return t
}

// CookieName is the session cookie name.
const CookieName = "educore_session"

// SetSessionCookie writes an HttpOnly session cookie (Secure only under HTTPS).
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	SetSessionCookie(w, r, "", -1)
}

// Auth loads the session (if any) into the context. Unauthenticated requests
// proceed with an empty user.
type Auth struct {
	Service *auth.Service
}

// Authenticated is true when a valid session user exists.
func (a Auth) Authenticated(r *http.Request) bool {
	_, ok := UserFrom(r)
	return ok
}

// WithSession resolves the session cookie and attaches the user.
func (a Auth) WithSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
			if u, err := a.Service.LookupSession(c.Value); err == nil {
				ctx := context.WithValue(r.Context(), userKey, u)
				ctx = context.WithValue(ctx, tokenKey, c.Value)
				r = r.WithContext(ctx)
			} else { // unknown/expired session
				// Expired or unknown cookie: clear it so the UI starts fresh.
				ClearSessionCookie(w, r)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// roleGuard enforces a required role for authenticated users.
func roleGuard(next http.Handler, role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		if u.Role != role {
			// Wrong role: redirect to that user's home instead of leaking access.
			http.Redirect(w, r, homeFor(u.Role), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireFaculty limits a route to FACULTY sessions.
func RequireFaculty(next http.Handler) http.Handler { return roleGuard(next, "FACULTY") }

// RequireStudent limits a route to STUDENT sessions.
func RequireStudent(next http.Handler) http.Handler { return roleGuard(next, "STUDENT") }

// RequireAuth limits a route to any authenticated session.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r); !ok {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HomePath returns the landing route for a role.
func HomePath(role string) string { return homeFor(role) }

func homeFor(role string) string {
	if role == "FACULTY" {
		return "/faculty"
	}
	return "/student"
}

// CSRF carries the secret used to mint and verify form tokens.
type CSRF struct {
	Secret string // stable per-process secret
}

func (c CSRF) hash(token string) string {
	return auth.TokenHash(c.Secret + ":" + token)
}

// Token derives a CSRF token for a session: HMAC-like over the session token
// so the token rotates implicitly with session rotation. Anonymous forms (the
// login page) use a dedicated random salt mixed into the secret pool.
func (c CSRF) Token(sessionToken string) string {
	if sessionToken == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			// crypto/rand failure is unrecoverable in practice; fall back to
			// the secret alone rather than panicking in a request path.
			log.Printf("csrf: random salt unavailable: %v", err)
		} else {
			sessionToken = string(b)
		}
	}
	return c.hash(sessionToken)
}

// Verify checks a submitted CSRF token against the session token.
func (c CSRF) Verify(sessionToken, submitted string) bool {
	if submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.hash(sessionToken)), []byte(submitted)) == 1
}

const anonymousCookieName = "educore_csrf"

// AnonymousToken returns (and lazily issues via cookie) a CSRF token for
// pre-authentication forms such as the login page.
func (c CSRF) AnonymousToken(w http.ResponseWriter, r *http.Request) string {
	val := ""
	if ck, err := r.Cookie(anonymousCookieName); err == nil {
		val = ck.Value
	}
	if val == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			log.Printf("csrf: anonymous token entropy failed: %v", err)
			return ""
		}
		val = string(auth.TokenHex(b))
		http.SetCookie(w, &http.Cookie{
			Name:     anonymousCookieName,
			Value:    val,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
	}
	return c.hash(val)
}

// VerifyAnonymous validates a pre-authentication CSRF submission.
func (c CSRF) VerifyAnonymous(r *http.Request, submitted string) bool {
	if submitted == "" {
		return false
	}
	ck, err := r.Cookie(anonymousCookieName)
	if err != nil || ck.Value == "" {
		return false
	}
	return c.Verify(ck.Value, submitted)
}

// RequireCSRF rejects state-changing POSTs without a valid token.
func (c CSRF) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			token := TokenFrom(r)
			submitted := r.FormValue("_csrf")
			if submitted == "" && stringsHasPrefix(r.Header.Get("Content-Type"), "application/json") {
				submitted = r.Header.Get("X-CSRF-Token")
			}
			if !c.Verify(token, submitted) {
				log.Printf("csrf rejected: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
				http.Error(w, "Your session expired or the form token is invalid. Reload the page and try again.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func stringsHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// Guest redirects authenticated users away from login/logout-adjacent pages.
func Guest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFrom(r); ok {
			http.Redirect(w, r, homeFor(u.Role), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PurgeSessions periodically purges expired sessions until the context ends.
func PurgeSessions(svc *auth.Service, stop <-chan struct{}) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if err := svc.PurgeExpired(); err != nil {
				log.Printf("session purge: %v", err)
			}
		case <-stop:
			return
		}
	}
}
