package middleware

import "net/http"

// valueCookie builds a fake *http.Cookie-like value for tests; the real type
// is returned since it's a struct, but avoid importing extra aliases.
func valueCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value}
}
