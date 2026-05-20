package pressauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"
)

// freshJWT builds a JWT with a given exp offset from now. Reused across the
// refresh test cases so each scenario can express expiry in plain English.
func freshJWT(t *testing.T, offset time.Duration) (string, time.Time) {
	t.Helper()
	exp := time.Now().UTC().Add(offset).Truncate(time.Second)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(map[string]any{"exp": exp.Unix()})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + payload + "." + sig, exp
}

// withRefreshClient swaps the package-level refreshClient for the duration
// of one subtest. The test server's Client() returns a transport that
// trusts the server's TLS cert (httptest uses an in-process cert), which is
// the only thing the surrounding code cares about.
func withRefreshClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := refreshClient
	refreshClient = srv.Client()
	refreshClient.Timeout = refreshHTTPTimeout
	t.Cleanup(func() { refreshClient = orig })
}

func TestRefreshSuccessRotatesCookies(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Refresh persists via Save which requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	originalJWT, _ := freshJWT(t, 30*time.Second) // about to expire
	newJWT, newExp := freshJWT(t, 30*time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Carrier cookie rotated; a session-binding cookie also rotated;
		// a third cookie is left untouched so we verify preservation.
		http.SetCookie(w, &http.Cookie{Name: "guestsession", Value: newJWT, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "rotated-csrf", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	// Use the absolute test-server URL as the refresh endpoint so the request
	// reaches httptest. Domain stays a placeholder.
	st := &State{
		Domain:     domain,
		CapturedAt: time.Now().UTC(),
		Cookies: map[string]string{
			"guestsession": originalJWT,
			"csrftoken":    "test-csrf-1",
			"sessionid":    "test-session-token-1",
		},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	updated, err := Refresh(context.Background(), st)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if updated.Cookies["guestsession"] != newJWT {
		t.Error("carrier cookie was not rotated")
	}
	if updated.Cookies["csrftoken"] != "rotated-csrf" {
		t.Errorf("csrftoken = %q, want rotated-csrf", updated.Cookies["csrftoken"])
	}
	if updated.Cookies["sessionid"] != "test-session-token-1" {
		t.Errorf("non-rotated cookie not preserved: %q", updated.Cookies["sessionid"])
	}
	if !updated.JWTExpiry.Equal(newExp) {
		t.Errorf("JWTExpiry = %v, want %v", updated.JWTExpiry, newExp)
	}

	reloaded, err := Load(domain)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Cookies["guestsession"] != newJWT {
		t.Error("reloaded state did not persist rotated carrier cookie")
	}
}

func TestRefreshAuthRequired(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "401 unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
		},
		{
			name: "403 forbidden",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
		},
		{
			name: "302 to login",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "/login")
				w.WriteHeader(http.StatusFound)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain := uniqueDomain(t)
			home := useTempHome(t, domain)

			srv := httptest.NewServer(http.HandlerFunc(tc.handler))
			defer srv.Close()
			withRefreshClient(t, srv)

			originalJWT, originalExp := freshJWT(t, 30*time.Second)
			st := &State{
				Domain:           domain,
				CapturedAt:       time.Now().UTC(),
				Cookies:          map[string]string{"guestsession": originalJWT, "csrftoken": "test-csrf-1"},
				RefreshEndpoint:  srv.URL + "/account/token",
				JWTCarrierCookie: "guestsession",
				JWTExpiry:        originalExp,
			}
			if err := Save(st); err != nil {
				t.Fatalf("seed Save: %v", err)
			}

			_, err := Refresh(context.Background(), st)
			if err == nil {
				t.Fatal("Refresh succeeded, want auth-required ExitError")
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected ExitError, got %T: %v", err, err)
			}
			if exitErr.Code != ExitRefreshFailed {
				t.Errorf("Code = %d, want ExitRefreshFailed (%d)", exitErr.Code, ExitRefreshFailed)
			}
			if !strings.Contains(err.Error(), "press-auth login") {
				t.Errorf("error should name recovery command: %v", err)
			}

			// State on disk must be unchanged.
			reloaded, err := Load(domain)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if reloaded.Cookies["guestsession"] != originalJWT {
				t.Error("state was mutated on auth-required failure")
			}
			_ = home
		})
	}
}

func TestRefreshNoNewCookies(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	originalJWT, originalExp := freshJWT(t, 30*time.Second)
	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"guestsession": originalJWT},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        originalExp,
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	_, err := Refresh(context.Background(), st)
	if err == nil {
		t.Fatal("Refresh succeeded, want ExitNoNewCookies")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitNoNewCookies {
		t.Errorf("Code = %d, want ExitNoNewCookies (%d)", exitErr.Code, ExitNoNewCookies)
	}
}

func TestRefreshNonRotatedCarrierAcceptable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	originalJWT, originalExp := freshJWT(t, 30*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Server only rotates a binding cookie; carrier untouched.
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "new-csrf", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"guestsession": originalJWT, "csrftoken": "test-csrf-1"},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        originalExp,
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	updated, err := Refresh(context.Background(), st)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if updated.Cookies["csrftoken"] != "new-csrf" {
		t.Errorf("csrftoken not rotated: %q", updated.Cookies["csrftoken"])
	}
	if updated.Cookies["guestsession"] != originalJWT {
		t.Errorf("carrier should be preserved when server doesn't rotate it")
	}
	if !updated.JWTExpiry.Equal(originalExp) {
		t.Errorf("JWTExpiry should match unchanged carrier exp: got %v want %v", updated.JWTExpiry, originalExp)
	}
}

func TestRefreshRelativeAndAbsoluteURLs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}

	newJWT, _ := freshJWT(t, 30*time.Minute)
	originalJWT, originalExp := freshJWT(t, 30*time.Second)

	t.Run("absolute URL on a different host", func(t *testing.T) {
		domain := uniqueDomain(t)
		useTempHome(t, domain)

		var gotHost string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHost = r.Host
			http.SetCookie(w, &http.Cookie{Name: "guestsession", Value: newJWT, Path: "/"})
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		withRefreshClient(t, srv)

		st := &State{
			Domain:           domain,
			CapturedAt:       time.Now().UTC(),
			Cookies:          map[string]string{"guestsession": originalJWT},
			RefreshEndpoint:  srv.URL + "/token",
			JWTCarrierCookie: "guestsession",
			JWTExpiry:        originalExp,
		}
		if err := Save(st); err != nil {
			t.Fatalf("seed Save: %v", err)
		}
		if _, err := Refresh(context.Background(), st); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		wantHost, _ := hostOf(srv.URL)
		if gotHost != wantHost {
			t.Errorf("request host = %q, want %q (absolute URL not honored)", gotHost, wantHost)
		}
	})

	t.Run("relative path resolves against https://<domain>", func(t *testing.T) {
		// We can't actually hit https://<fake>.example.invalid, so this case
		// inspects resolveRefreshURL's output directly. Hitting a real server
		// is covered by the absolute-URL subtest above.
		got, err := resolveRefreshURL("example.test", "/account/token")
		if err != nil {
			t.Fatalf("resolveRefreshURL: %v", err)
		}
		if got != "https://example.test/account/token" {
			t.Errorf("resolveRefreshURL = %q", got)
		}
	})
}

// hostOf is a tiny helper to pull the host:port out of an httptest server URL.
// Splitting on // and / keeps the test free of net/url imports it doesn't
// need elsewhere.
func hostOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}

func TestRefreshMultipleSetCookieHeaders(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	newJWT, _ := freshJWT(t, 30*time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "guestsession", Value: newJWT, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf-rotated", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "tracking_id", Value: "new-tid", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	originalJWT, originalExp := freshJWT(t, 30*time.Second)
	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"guestsession": originalJWT, "csrftoken": "test-csrf-1"},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        originalExp,
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	updated, err := Refresh(context.Background(), st)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	wants := map[string]string{
		"guestsession": newJWT,
		"csrftoken":    "csrf-rotated",
		"tracking_id":  "new-tid",
	}
	for name, want := range wants {
		if updated.Cookies[name] != want {
			t.Errorf("cookie %q not merged correctly", name)
		}
	}
}

func TestRefreshMissingEndpoint(t *testing.T) {
	st := &State{
		Domain:           "example.test",
		Cookies:          map[string]string{"guestsession": "ignored"},
		JWTCarrierCookie: "guestsession",
	}
	_, err := Refresh(context.Background(), st)
	if err == nil {
		t.Fatal("expected error for missing endpoint, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitMissingEndpoint {
		t.Errorf("Code = %d, want ExitMissingEndpoint (%d)", exitErr.Code, ExitMissingEndpoint)
	}
}

func TestFormatCookieHeaderDeterministic(t *testing.T) {
	cookies := map[string]string{
		"zebra":    "z",
		"alpha":    "a",
		"midpoint": "m",
	}
	got := formatCookieHeader(cookies)
	want := "alpha=a; midpoint=m; zebra=z"
	if got != want {
		t.Errorf("formatCookieHeader = %q, want %q", got, want)
	}
	if formatCookieHeader(nil) != "" {
		t.Errorf("empty input should return empty string, got %q", formatCookieHeader(nil))
	}
}

func TestResolveRefreshURL(t *testing.T) {
	cases := []struct {
		name     string
		domain   string
		endpoint string
		want     string
	}{
		{"relative path", "example.test", "/account/token", "https://example.test/account/token"},
		{"absolute different host", "example.test", "https://auth.other.test/refresh", "https://auth.other.test/refresh"},
		{"absolute same host", "example.test", "https://example.test/refresh", "https://example.test/refresh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRefreshURL(tc.domain, tc.endpoint)
			if err != nil {
				t.Fatalf("resolveRefreshURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveRefreshURL = %q, want %q", got, tc.want)
			}
		})
	}
}
