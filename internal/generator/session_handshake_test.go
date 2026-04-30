package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
