// Package fakelogin builds an httptest-style http.Handler that simulates a
// minimal login flow for press-auth chromedp tests. The same handler is
// reused by the standalone main binary (for hand smoke tests) and by the
// in-process chromedp test in internal/pressauth.
//
// Flow:
//
//	GET  /login            -> simple form with email + password
//	POST /submit           -> sets two cookies (one with explicit Domain,
//	                          one host-only) and 302-redirects to
//	                          /account/overview
//	GET  /account/overview -> page containing a "Sign out" link, the
//	                          heuristic completion signal used by chromedp.Capture
//	GET  /other            -> sets a cookie scoped to a different domain so
//	                          tests can assert it is filtered out
//
// The cookie values are obviously fake placeholders; nothing here should
// resemble a real token.
package fakelogin

import (
	"net/http"
)

// DefaultCookieDomain is the Domain attribute set on the "session" cookie
// when callers do not pass an explicit one. Tests override this so they can
// map their target domain through chromedp.
const DefaultCookieDomain = ".example.test"

// NewHandler returns the fake login HTTP handler. domain is the value
// stamped into the "session" cookie's Domain attribute; pass "" to omit
// the Domain attribute (host-only cookie).
func NewHandler(domain string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginForm)
	mux.HandleFunc("/submit", submitForm(domain))
	mux.HandleFunc("/account/overview", accountOverview)
	mux.HandleFunc("/other", otherDomainCookie)
	return mux
}

const loginHTML = `<!doctype html>
<html><head><title>Fake login</title></head>
<body>
<h1>Sign in</h1>
<form id="loginform" action="/submit" method="POST">
  <label>Email <input id="email" name="email" type="email" autocomplete="off"></label>
  <label>Password <input id="password" name="password" type="password" autocomplete="off"></label>
  <button id="submit" type="submit">Sign in</button>
</form>
</body></html>
`

const overviewHTML = `<!doctype html>
<html><head><title>Account overview</title></head>
<body>
<h1>Welcome</h1>
<p>You are signed in.</p>
<a id="signout" href="/signout">Sign out</a>
</body></html>
`

func loginForm(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(loginHTML))
}

func submitForm(domain string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// session cookie: optionally domain-scoped so the suffix-match
		// branch of cookieDomainMatches is exercised end-to-end.
		session := &http.Cookie{
			Name:  "session",
			Value: "test-session-token-1",
			Path:  "/",
		}
		if domain != "" {
			session.Domain = domain
		}
		http.SetCookie(w, session)

		// auth cookie: host-only (no Domain attribute), HttpOnly so it
		// is invisible to JS but still visible to storage.GetCookies.
		http.SetCookie(w, &http.Cookie{
			Name:     "auth",
			Value:    "test-auth-token-1",
			Path:     "/",
			HttpOnly: true,
		})

		http.Redirect(w, r, "/account/overview", http.StatusFound)
	}
}

func accountOverview(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(overviewHTML))
}

// otherDomainCookie sets a cookie under a different Domain so the test can
// confirm the suffix-match filter drops it from the captured set.
func otherDomainCookie(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   "stray",
		Value:  "test-stray-token",
		Path:   "/",
		Domain: ".other.example.test",
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body>other</body></html>"))
}
