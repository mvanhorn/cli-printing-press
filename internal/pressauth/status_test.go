package pressauth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// makeJWT builds a header.payload.signature triple where the payload
// claims include exp. Header and signature are filler — DecodeJWT only
// parses the payload. Test-only helper to keep status_test.go hermetic.
func makeJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims := map[string]any{}
	if !exp.IsZero() {
		claims["exp"] = exp.Unix()
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + payload + "." + sig
}

// makeStateForStatus builds a State with the carrier cookie set to a JWT
// expiring at jwtExp. The cached JWTExpiry is also set; mirrors what
// Refresh writes after a real call.
func makeStateForStatus(domain string, now time.Time, jwtExp time.Time, carrierValue string) *State {
	carrier := carrierValue
	return &State{
		Domain:           domain,
		CapturedAt:       now,
		Cookies:          map[string]string{"sessionid": "test-session-token-1", "guestsession": carrier},
		RefreshEndpoint:  "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        jwtExp,
	}
}

func TestClassifyStatusTable(t *testing.T) {
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	cases := []struct {
		name     string
		state    *State
		wantKind statusState
	}{
		{
			name:     "valid (30m remaining)",
			state:    &State{Domain: "a.example", JWTExpiry: now.Add(30 * time.Minute), CapturedAt: now},
			wantKind: statusValid,
		},
		{
			name:     "valid (5m remaining)",
			state:    &State{Domain: "a.example", JWTExpiry: now.Add(5 * time.Minute), CapturedAt: now},
			wantKind: statusValid,
		},
		{
			name:     "near-expiry (45s remaining)",
			state:    &State{Domain: "a.example", JWTExpiry: now.Add(45 * time.Second), CapturedAt: now},
			wantKind: statusNearExpiry,
		},
		{
			name:     "expired (5m ago)",
			state:    &State{Domain: "a.example", JWTExpiry: now.Add(-5 * time.Minute), CapturedAt: now.Add(-time.Hour)},
			wantKind: statusExpired,
		},
		{
			name:     "expired (2d ago)",
			state:    &State{Domain: "a.example", JWTExpiry: now.Add(-48 * time.Hour), CapturedAt: now.Add(-72 * time.Hour)},
			wantKind: statusExpired,
		},
		{
			name:     "fresh, no expiry",
			state:    &State{Domain: "a.example", JWTExpiry: time.Time{}, CapturedAt: now},
			wantKind: statusFreshNoExp,
		},
		{
			name:     "nil state -> not captured",
			state:    nil,
			wantKind: statusNotCaptured,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := classifyStatus("a.example", tc.state, now)
			if r.State != tc.wantKind {
				t.Errorf("got state=%q want %q", r.State, tc.wantKind)
			}
		})
	}
}

func TestClassifyStatusExpiredRecoveryHint(t *testing.T) {
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	// Within 24h: refresh suggested.
	freshExpired := &State{Domain: "a.example", JWTExpiry: now.Add(-10 * time.Minute)}
	r := classifyStatus("a.example", freshExpired, now)
	if r.Recovery != "press-auth refresh a.example" {
		t.Errorf("recently expired: recovery=%q, want refresh", r.Recovery)
	}
	// Past 24h: login recommended.
	staleExpired := &State{Domain: "a.example", JWTExpiry: now.Add(-48 * time.Hour)}
	r = classifyStatus("a.example", staleExpired, now)
	if r.Recovery != "press-auth login a.example" {
		t.Errorf("long expired: recovery=%q, want login", r.Recovery)
	}
}

func runStatusForTest(t *testing.T, domain string, state *State, now time.Time, jsonMode bool) (string, error) {
	t.Helper()
	if state != nil {
		if err := Save(state); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	var buf bytes.Buffer
	gf := &GlobalFlags{JSON: jsonMode}
	err := runStatus(&buf, domain, gf, now)
	return buf.String(), err
}

func TestStatusValidExitsZero(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	jwt := makeJWT(t, now.Add(30*time.Minute))
	st := makeStateForStatus(domain, now, now.Add(30*time.Minute), jwt)

	out, err := runStatusForTest(t, domain, st, now, false)
	if err != nil {
		t.Fatalf("runStatus returned err: %v", err)
	}
	if !strings.Contains(out, "valid until") || !strings.Contains(out, "30m remaining") {
		t.Errorf("output missing valid+remaining: %q", out)
	}
}

func TestStatusNearExpiry(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	expiry := now.Add(45 * time.Second)
	jwt := makeJWT(t, expiry)
	st := makeStateForStatus(domain, now, expiry, jwt)

	out, err := runStatusForTest(t, domain, st, now, false)
	if err != nil {
		t.Fatalf("runStatus returned err: %v", err)
	}
	if !strings.Contains(out, "near-expiry") {
		t.Errorf("output missing near-expiry: %q", out)
	}
}

func TestStatusExpiredRecentSuggestsRefresh(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	expiry := now.Add(-5 * time.Minute)
	jwt := makeJWT(t, expiry)
	st := makeStateForStatus(domain, now, expiry, jwt)

	out, err := runStatusForTest(t, domain, st, now, false)
	if err == nil {
		t.Fatal("expected ExitError, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitNotCaptured {
		t.Errorf("expected ExitNotCaptured (%d), got %d", ExitNotCaptured, ee.Code)
	}
	if !strings.Contains(out, "press-auth refresh") {
		t.Errorf("expected refresh hint in output: %q", out)
	}
}

func TestStatusExpiredOldSuggestsLogin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	expiry := now.Add(-48 * time.Hour)
	jwt := makeJWT(t, expiry)
	st := makeStateForStatus(domain, now, expiry, jwt)

	out, err := runStatusForTest(t, domain, st, now, false)
	if err == nil {
		t.Fatal("expected ExitError, got nil")
	}
	if !strings.Contains(out, "press-auth login") {
		t.Errorf("expected login hint in output: %q", out)
	}
}

func TestStatusInvalidJWT(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	// Carrier cookie is set but the value is gibberish.
	st := &State{
		Domain:           domain,
		CapturedAt:       now,
		Cookies:          map[string]string{"guestsession": "not.a.jwt.payload"},
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        now.Add(30 * time.Minute),
	}

	out, err := runStatusForTest(t, domain, st, now, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitInvalidJWT {
		t.Errorf("expected ExitInvalidJWT (%d), got %d", ExitInvalidJWT, ee.Code)
	}
	if !strings.Contains(out, "invalid JWT") {
		t.Errorf("expected invalid JWT message in output: %q", out)
	}
}

func TestStatusNotCaptured(t *testing.T) {
	useTempHome(t)
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	out, err := runStatusForTest(t, "never-saved.example.invalid", nil, now, false)
	if err == nil {
		t.Fatal("expected ExitError for missing state, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitNotCaptured {
		t.Errorf("expected ExitNotCaptured (%d), got %d", ExitNotCaptured, ee.Code)
	}
	if !strings.Contains(out, "not captured") {
		t.Errorf("expected not-captured message: %q", out)
	}
	if !strings.Contains(out, "press-auth login") {
		t.Errorf("expected login recovery hint: %q", out)
	}
}

func TestStatusFreshNoExpiry(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	// No carrier cookie name and no JWTExpiry — fresh capture path.
	st := &State{
		Domain:     domain,
		CapturedAt: now,
		Cookies:    map[string]string{"sessionid": "test-session-token-1"},
	}

	out, err := runStatusForTest(t, domain, st, now, false)
	if err != nil {
		t.Fatalf("runStatus returned err: %v", err)
	}
	if !strings.Contains(out, "fresh capture") {
		t.Errorf("expected fresh-capture message: %q", out)
	}
}

func TestStatusJSONOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	expiry := now.Add(29 * time.Minute)
	jwt := makeJWT(t, expiry)
	st := makeStateForStatus(domain, now, expiry, jwt)

	out, err := runStatusForTest(t, domain, st, now, true)
	if err != nil {
		t.Fatalf("runStatus returned err: %v", err)
	}
	// Single line valid JSON.
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Errorf("expected single-line JSON, got: %q", out)
	}
	var parsed statusJSON
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		t.Fatalf("unmarshal JSON: %v\noutput=%q", jerr, out)
	}
	if parsed.Domain != domain {
		t.Errorf("domain mismatch: got %q want %q", parsed.Domain, domain)
	}
	if parsed.State != "valid" {
		t.Errorf("state mismatch: got %q want valid", parsed.State)
	}
	if parsed.Expiry == "" {
		t.Error("expected non-empty expiry in JSON")
	}
	if parsed.RemainingSeconds == nil || *parsed.RemainingSeconds <= 0 {
		t.Errorf("expected positive remaining_seconds, got %v", parsed.RemainingSeconds)
	}
	// Sanity: cookie values must never appear in JSON output.
	for _, leak := range []string{"test-session-token-1", jwt} {
		if strings.Contains(out, leak) {
			t.Errorf("status JSON leaked secret %q", leak)
		}
	}
}

func TestHumanDurationFormats(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{2 * time.Minute, "2m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
		{-30 * time.Minute, "30m"},
	}
	for _, tc := range cases {
		got := humanDuration(tc.d)
		if got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestStatusOutputDoesNotLeakCookies is the cross-cutting safety check: no
// matter what classification path we hit, the captured cookie values must
// not surface in the text or JSON output.
func TestStatusOutputDoesNotLeakCookies(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	expiry := now.Add(30 * time.Minute)
	jwt := makeJWT(t, expiry)
	secret := fmt.Sprintf("super-secret-%d", now.UnixNano())
	st := &State{
		Domain:           domain,
		CapturedAt:       now,
		Cookies:          map[string]string{"sessionid": secret, "guestsession": jwt},
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        expiry,
	}
	if err := Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for _, mode := range []bool{false, true} {
		var buf bytes.Buffer
		gf := &GlobalFlags{JSON: mode}
		_ = runStatus(&buf, domain, gf, now)
		if strings.Contains(buf.String(), secret) {
			t.Errorf("status (json=%v) leaked cookie value: %q", mode, buf.String())
		}
	}
}
