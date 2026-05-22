package pressauth

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runCookiesCmd(domain string) (string, string, error) {
	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"cookies", domain})
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestCookiesFarFromExpiryNoRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	// Server should never be called; an unexpected hit fails the test.
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	originalJWT, _ := freshJWT(t, 30*time.Minute)
	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"guestsession": originalJWT, "csrftoken": "test-csrf-1"},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        time.Now().UTC().Add(30 * time.Minute),
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	out, _, err := runCookiesCmd(domain)
	if err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}
	line := strings.TrimSpace(out)
	if !strings.Contains(line, "csrftoken=test-csrf-1") {
		t.Errorf("output missing csrftoken: %q", line)
	}
	if !strings.Contains(line, "guestsession=") {
		t.Errorf("output missing carrier cookie: %q", line)
	}
	if calls.Load() != 0 {
		t.Errorf("refresh server was called %d times; expected 0 (state was fresh)", calls.Load())
	}
}

func TestCookiesNearExpiryTriggersRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	newJWT, _ := freshJWT(t, 45*time.Minute)
	originalJWT, _ := freshJWT(t, 30*time.Second)

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "guestsession", Value: newJWT, Path: "/"})
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
		JWTExpiry:        time.Now().UTC().Add(30 * time.Second),
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	out, _, err := runCookiesCmd(domain)
	if err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("refresh server calls = %d, want 1", calls.Load())
	}
	if !strings.Contains(out, "guestsession="+newJWT) {
		t.Error("output did not include rotated carrier cookie")
	}
}

func TestCookiesZeroExpiryTriggersRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	newJWT, newExp := freshJWT(t, 30*time.Minute)
	originalJWT, _ := freshJWT(t, 30*time.Minute)

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "guestsession", Value: newJWT, Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withRefreshClient(t, srv)

	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"guestsession": originalJWT},
		RefreshEndpoint:  srv.URL + "/account/token",
		JWTCarrierCookie: "guestsession",
		// Zero JWTExpiry simulates a fresh U3 capture that hasn't seen a
		// server response yet.
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	if _, _, err := runCookiesCmd(domain); err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("refresh server calls = %d, want 1 (zero expiry should trigger)", calls.Load())
	}

	reloaded, err := Load(domain)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.JWTExpiry.Equal(newExp) {
		t.Errorf("JWTExpiry persisted = %v, want %v", reloaded.JWTExpiry, newExp)
	}
}

func TestCookiesCookieOnlySessionSkipsLazyRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	st := &State{
		Domain:     domain,
		CapturedAt: time.Now().UTC(),
		Cookies:    map[string]string{"session": "cookie-only", "csrftoken": "csrf"},
		// No JWTCarrierCookie and zero JWTExpiry is the normal cookie-only shape.
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	out, _, err := runCookiesCmd(domain)
	if err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}
	if !strings.Contains(out, "session=cookie-only") {
		t.Errorf("output missing cookie-only session: %q", out)
	}
}

func TestCookiesJWTCarrierWithoutRefreshEndpointSkipsLazyRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	st := &State{
		Domain:           domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          map[string]string{"session": "jwt-cookie"},
		JWTCarrierCookie: "session",
		// RefreshEndpoint intentionally omitted: the flag is optional.
		// Zero JWTExpiry is the shape Capture persists before a refresh call.
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	out, _, err := runCookiesCmd(domain)
	if err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}
	if !strings.Contains(out, "session=jwt-cookie") {
		t.Errorf("output missing stored JWT cookie: %q", out)
	}
}

func TestCookiesMissingStateExits2(t *testing.T) {
	useTempHome(t)
	_, _, err := runCookiesCmd("never-saved.example.invalid")
	if err == nil {
		t.Fatal("expected error from cookies cmd, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.Code != ExitNotCaptured {
		t.Errorf("Code = %d, want ExitNotCaptured (%d)", exitErr.Code, ExitNotCaptured)
	}
	if !strings.Contains(err.Error(), "press-auth login") {
		t.Errorf("error should name recovery command: %v", err)
	}
}

func TestCookiesOutputIsDeterministic(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	originalJWT, _ := freshJWT(t, 30*time.Minute)
	st := &State{
		Domain:     domain,
		CapturedAt: time.Now().UTC(),
		Cookies: map[string]string{
			"guestsession": originalJWT,
			"alpha":        "a",
			"midpoint":     "m",
			"zebra":        "z",
		},
		RefreshEndpoint:  "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        time.Now().UTC().Add(30 * time.Minute),
	}
	if err := Save(st); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	out, _, err := runCookiesCmd(domain)
	if err != nil {
		t.Fatalf("cookies cmd: %v", err)
	}

	line := strings.TrimSpace(out)
	// Alphabetical cookie order: alpha, guestsession, midpoint, zebra.
	parts := strings.Split(line, "; ")
	if len(parts) < 4 {
		t.Fatalf("output has fewer than 4 cookies: %q", line)
	}
	wantOrder := []string{"alpha=", "guestsession=", "midpoint=", "zebra="}
	for i, prefix := range wantOrder {
		if !strings.HasPrefix(parts[i], prefix) {
			t.Errorf("part[%d] = %q, want prefix %q (output: %q)", i, parts[i], prefix, line)
		}
	}
}
