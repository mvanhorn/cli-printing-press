package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestSessionHandshakeGeneration verifies the generator emits a working
// session.go helper and wires it into the client when auth.type is
// "session_handshake". This is the WU-2 acceptance test (retro issue #174).
func TestSessionHandshakeGeneration(t *testing.T) {
	t.Parallel()

	sp := &spec.APISpec{
		Name:        "demo",
		Version:     "1.0.0",
		Description: "test",
		BaseURL:     "https://query1.example.com",
		Auth: spec.AuthConfig{
			Type:               "session_handshake",
			BootstrapURL:       "https://bootstrap.example.com/",
			SessionTokenURL:    "https://query2.example.com/v1/getcrumb",
			TokenFormat:        "text",
			TokenParamName:     "crumb",
			TokenParamIn:       "query",
			In:                 "query",
			Header:             "crumb",
			InvalidateOnStatus: []int{401, 403},
			SessionTTLHours:    24,
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/demo-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"quote": {
				Description: "Quotes",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/v7/finance/quote",
						Description: "Get quotes",
						Params: []spec.Param{{
							Name: "symbols", Type: "string", Required: true,
						}},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	g := New(sp, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// session.go must exist alongside client.go
	sessionPath := filepath.Join(dir, "internal", "client", "session.go")
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("expected %s to exist, got error: %v", sessionPath, err)
	}

	sessionContent, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Sanity checks on the emitted helper
	wants := []string{
		`"https://bootstrap.example.com/"`, // BootstrapURL substituted
		`"https://query2.example.com/v1/getcrumb"`,
		`401: true`, // InvalidateOnStatus rendered
		`403: true`,
		`24 * time.Hour`, // TTL rendered
		`type SessionManager struct`,
		`func (m *SessionManager) EnsureToken()`,
		`func (m *SessionManager) Invalidate()`,
		`func (m *SessionManager) ImportSession(`,
	}
	for _, w := range wants {
		if !strings.Contains(string(sessionContent), w) {
			t.Errorf("session.go missing expected substring %q", w)
		}
	}

	// client.go must reference the SessionManager
	clientPath := filepath.Join(dir, "internal", "client", "client.go")
	clientContent, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientContent), "Session    *SessionManager") {
		t.Error("client.go missing Session field")
	}
	if !strings.Contains(string(clientContent), "c.Session.EnsureToken()") {
		t.Error("client.go doesn't call EnsureToken() on the live path")
	}
	if !strings.Contains(string(clientContent), "c.Session.ShouldInvalidate(") {
		t.Error("client.go doesn't check ShouldInvalidate on responses")
	}
	if !strings.Contains(string(clientContent), "c.Session.Invalidate()") {
		t.Error("client.go doesn't invalidate on status-code match")
	}
}

func TestSessionHandshakeBrowserTransportSharesJar(t *testing.T) {
	t.Parallel()

	sp := &spec.APISpec{
		Name:          "demo",
		Version:       "1.0.0",
		Description:   "test",
		BaseURL:       "https://query1.example.com",
		SpecSource:    "sniffed",
		HTTPTransport: spec.HTTPTransportBrowserChrome,
		Auth: spec.AuthConfig{
			Type:               "session_handshake",
			BootstrapURL:       "https://bootstrap.example.com/",
			SessionTokenURL:    "https://query2.example.com/v1/getcrumb",
			TokenFormat:        "text",
			TokenParamName:     "crumb",
			TokenParamIn:       "query",
			In:                 "query",
			Header:             "crumb",
			InvalidateOnStatus: []int{401, 403},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/demo-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"quote": {
				Description: "Quotes",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/v7/finance/quote", Description: "Get quotes"},
				},
			},
		},
	}

	dir := t.TempDir()
	g := New(sp, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	clientContent, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	sessionContent, err := os.ReadFile(filepath.Join(dir, "internal", "client", "session.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`"github.com/enetx/surf"`,
		"func newHTTPClient(timeout time.Duration, jar http.CookieJar) *http.Client",
		"if jar == nil",
		"builder = builder.Session()",
		"httpClient.Jar = jar",
		"newHTTPClient(timeout, sess.CookieJar())",
	} {
		if !strings.Contains(string(clientContent), want) {
			t.Errorf("client.go missing expected substring %q", want)
		}
	}
	if !strings.Contains(string(sessionContent), "func (m *SessionManager) CookieJar() http.CookieJar") {
		t.Error("session.go missing CookieJar accessor")
	}

	// The bootstrap + token-fetch calls inside the SessionManager must run
	// through the same newHTTPClient as the data path. Otherwise the
	// handshake itself uses vanilla net/http and the bot wall returns 429
	// on the very call that would have established the session, even when
	// the data client could have cleared it.
	if !strings.Contains(string(sessionContent), "client: newHTTPClient(timeout, jar)") {
		t.Error("session.go's newSessionManager must use newHTTPClient so the handshake inherits the browser-impersonated transport, not a vanilla &http.Client{}")
	}
	if strings.Contains(string(sessionContent), "&http.Client{Timeout: timeout, Jar: jar}") {
		t.Error("session.go still constructs a vanilla &http.Client{} — the handshake will bypass Surf impersonation")
	}
}

// canonicalSessionHandshakeSpec returns a session-handshake spec that uses
// only the canonical fields (TokenParamName / TokenParamIn) and omits the
// api_key In / Header pair so tests can verify session-handshake fields
// drive generation on their own.
func canonicalSessionHandshakeSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:        "demo",
		Version:     "1.0.0",
		Description: "test",
		BaseURL:     "https://query1.example.com",
		Auth: spec.AuthConfig{
			Type:               "session_handshake",
			BootstrapURL:       "https://bootstrap.example.com/",
			SessionTokenURL:    "https://query2.example.com/v1/getcrumb",
			TokenFormat:        "text",
			TokenParamName:     "crumb",
			TokenParamIn:       "query",
			InvalidateOnStatus: []int{401, 403},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/demo-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"quote": {
				Description: "Quotes",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/v7/finance/quote", Description: "Get quotes"},
				},
			},
		},
	}
}

// TestSessionHandshakeTokenAttachesAsQueryParam verifies that for
// session_handshake auth with TokenParamIn=query, the generated client.go
// attaches the token as a query parameter named TokenParamName — NOT as an
// Authorization header. The spec's canonical fields for session-handshake
// token placement are TokenParamIn / TokenParamName, distinct from the
// api_key In / Header pair. A spec that sets only the canonical fields
// must still produce a query-param attachment, not fall through to a
// header.
func TestSessionHandshakeTokenAttachesAsQueryParam(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := New(canonicalSessionHandshakeSpec(), dir).Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	clientContent, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(clientContent), `q.Set("crumb", authHeader)`) {
		t.Error("client.go does not attach the session-handshake token as a query parameter named \"crumb\" — likely fell through to header attachment")
	}
	if strings.Contains(string(clientContent), `req.Header.Set("Authorization", authHeader)`) {
		t.Error("client.go still emits req.Header.Set(\"Authorization\", authHeader) for session_handshake auth — token will reach the wrong place")
	}
}

// TestSessionHandshakeTokenAttachesAsHeader covers the alternate placement
// where session-handshake tokens travel as a request header (TokenParamIn
// "header") instead of a query parameter. The template branches on this
// value in both do() and dryRun(); without this test the header path is
// uncovered and a regression flipping the polarity of the conditional
// would not be caught.
func TestSessionHandshakeTokenAttachesAsHeader(t *testing.T) {
	t.Parallel()

	sp := canonicalSessionHandshakeSpec()
	sp.Auth.TokenParamIn = "header"
	sp.Auth.TokenParamName = "X-Crumb"

	dir := t.TempDir()
	if err := New(sp, dir).Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	clientContent, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(clientContent), `req.Header.Set("X-Crumb", authHeader)`) {
		t.Error("client.go does not attach the session-handshake token as a request header named \"X-Crumb\" — header branch is broken or unreachable")
	}
	if strings.Contains(string(clientContent), `q.Set("X-Crumb", authHeader)`) {
		t.Error("client.go also emits a query-param attachment for TokenParamIn=header — branches collapsed")
	}
}

// TestSessionHandshakeCookiePersistenceMultiHost verifies the generated
// session.go snapshots cookies for every host the session interacts with
// (bootstrap, token, data base) so cookies set during bootstrap survive
// across invocations and reach the data host that needs them. cookiejar
// strips Domain on Cookies(u), so a single-host snapshot loses any cookie
// not host-scoped to that host.
func TestSessionHandshakeCookiePersistenceMultiHost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := New(canonicalSessionHandshakeSpec(), dir).Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sessionContent, err := os.ReadFile(filepath.Join(dir, "internal", "client", "session.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`sessionDataBaseURL   = "https://query1.example.com"`,
		`for _, host := range []string{sessionBootstrapURL, sessionTokenURL, sessionDataBaseURL}`,
	} {
		if !strings.Contains(string(sessionContent), want) {
			t.Errorf("session.go missing expected substring %q", want)
		}
	}

	if strings.Contains(string(sessionContent), `// Persist cookies for the token endpoint host (broadest plausible scope).`) {
		t.Error("session.go still contains the single-host cookie-persistence comment — cross-host bootstrap cookies will be dropped on save")
	}
}

// TestSessionHandshakeNotEmittedForOtherAuth verifies the session helper is
// NOT emitted for non-session auth types — no file bloat for bearer_token CLIs.
func TestSessionHandshakeNotEmittedForOtherAuth(t *testing.T) {
	t.Parallel()

	sp := &spec.APISpec{
		Name:        "demo",
		Version:     "1.0.0",
		Description: "test",
		BaseURL:     "https://api.example.com",
		Auth:        spec.AuthConfig{Type: "bearer_token", Header: "Authorization", EnvVars: []string{"DEMO_TOKEN"}},
		Config:      spec.ConfigSpec{Format: "toml", Path: "~/.config/demo-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "list"},
				},
			},
		},
	}

	dir := t.TempDir()
	g := New(sp, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sessionPath := filepath.Join(dir, "internal", "client", "session.go")
	if _, err := os.Stat(sessionPath); err == nil {
		t.Errorf("session.go should NOT exist for bearer_token auth (got file at %s)", sessionPath)
	}
}
