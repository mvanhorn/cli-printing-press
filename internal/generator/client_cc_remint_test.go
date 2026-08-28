package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client_credentials grant issues no refresh token, so the generated
// 401-recovery branch must re-mint via the token endpoint instead of gating
// on RefreshToken (which structurally never fires for this grant), and a
// stored token with no recorded expiry must be re-minted once per process
// instead of trusted forever.
func ccRemintSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "ccremint",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://api.example.com/oauth/token",
			EnvVars:     []string{"CCREMINT_API_KEY", "CCREMINT_SECRET_KEY"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/ccremint-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}},
			},
		},
	}
}

func TestClientCredentials401RemintEmission(t *testing.T) {
	t.Parallel()

	apiSpec := ccRemintSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "client", "client.go")

	assert.Contains(t, body, "re-minting access token after 401",
		"client_credentials 401 branch recovers via a fresh mint")
	assert.NotContains(t, body, `c.Config.RefreshToken != ""`,
		"client_credentials 401 branch must not gate on a refresh token the grant never issues")
	assert.Contains(t, body, "func (c *Client) needsClientCredentialsMint()",
		"mint-window helper is a method so it can see the per-process unknown-expiry cap")
	assert.Contains(t, body, "ccMintedUnknownExpiry *atomic.Bool",
		"unknown-expiry cap is a pointer so WithTier copies share one process flag")
	assert.Contains(t, body, "ccMintedUnknownExpiry: &atomic.Bool{}",
		"New initializes the shared unknown-expiry cap")
}

func TestAuthorizationCode401RefreshUnchanged(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "acremint",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:             "bearer_token",
			Header:           "Authorization",
			Format:           "Bearer {token}",
			AuthorizationURL: "https://api.example.com/oauth/authorize",
			TokenURL:         "https://api.example.com/oauth/token",
			EnvVars:          []string{"ACREMINT_TOKEN"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/acremint-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "client", "client.go")

	assert.Contains(t, body, "refreshing access token after 401",
		"refresh-token grants keep the refresh-based 401 recovery")
	assert.Contains(t, body, `c.Config.RefreshToken != ""`,
		"refresh-token grants keep the RefreshToken gate")
	assert.NotContains(t, body, "re-minting access token after 401",
		"non-client_credentials specs must not emit the mint-based recovery")
}

// WithTier value-copies Client. The unknown-expiry cap must be a pointer so
// the copy shares one process flag and go vet copylocks stays clean.
func TestClientCredentialsWithTierSharesMintCap(t *testing.T) {
	t.Parallel()

	apiSpec := ccRemintSpec()
	apiSpec.Name = "cctier"
	apiSpec.Auth.EnvVars = []string{"CCTIER_API_KEY", "CCTIER_SECRET_KEY"}
	apiSpec.Config.Path = "~/.config/cctier-pp-cli/config.toml"
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "free",
		Tiers: map[string]spec.TierConfig{
			"free": {Auth: spec.AuthConfig{Type: "none"}},
			"paid": {
				BaseURL: "https://paid.example.com",
				Auth:    apiSpec.Auth,
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, body, "func (c *Client) WithTier")
	assert.Contains(t, body, "ccMintedUnknownExpiry *atomic.Bool")
	assert.Contains(t, body, "ccMintedUnknownExpiry: &atomic.Bool{}")
	assert.NotContains(t, body, "ccMintedUnknownExpiry atomic.Bool")

	requireGeneratedCompiles(t, outputDir)
}

// TestClientCredentials401RemintBehavior proves the emitted client actually
// recovers: a request rejected with 401 mints a fresh token and succeeds on
// the retry, and a stored token of unknown age costs exactly one mint per
// process. The behavior test is injected into the generated module and run
// with the module's own toolchain so the assertion is on compiled behavior,
// not template text.
func TestClientCredentials401RemintBehavior(t *testing.T) {
	t.Parallel()

	apiSpec := ccRemintSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	goModBytes, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	modulePath := ""
	for line := range strings.SplitSeq(string(goModBytes), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			modulePath = strings.TrimSpace(rest)
			break
		}
	}
	require.NotEmpty(t, modulePath, "generated go.mod must declare a module path")

	behaviorTest := fmt.Sprintf(`package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"%s/internal/config"
)

func newCCRemintServer(mints, apiCalls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			atomic.AddInt32(mints, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `+"`"+`{"access_token":"fresh-%%d"}`+"`"+`, atomic.LoadInt32(mints))
			return
		}
		atomic.AddInt32(apiCalls, 1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer fresh-") {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `+"`"+`{"error":"not_authenticated"}`+"`"+`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`{"ok":true}`+"`"+`)
	}))
}

func ccRemintConfig(t *testing.T, srvURL string, expiry time.Time) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localapp"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
	return &config.Config{
		BaseURL:      srvURL,
		TokenURL:     srvURL + "/oauth/token",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AccessToken:  "stale-token",
		TokenExpiry:  expiry,
		Path:         filepath.Join(tmp, "config.toml"),
	}
}

func TestDo401RemintsClientCredentials(t *testing.T) {
	var mints, apiCalls int32
	srv := newCCRemintServer(&mints, &apiCalls)
	defer srv.Close()

	// Expiry an hour out: the proactive window does not fire, so the stale
	// token is sent, rejected with 401, and recovery must come from the
	// 401 re-mint branch.
	cfg := ccRemintConfig(t, srv.URL, time.Now().Add(time.Hour))
	c := New(cfg, 10*time.Second, 0)
	c.NoCache = true

	body, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatalf("Get after 401 should recover via re-mint, got error: %%v", err)
	}
	if !strings.Contains(string(body), `+"`"+`"ok":true`+"`"+`) {
		t.Fatalf("unexpected body after re-mint retry: %%s", body)
	}
	if got := atomic.LoadInt32(&mints); got != 1 {
		t.Fatalf("token mints = %%d, want exactly 1", got)
	}
	if got := atomic.LoadInt32(&apiCalls); got != 2 {
		t.Fatalf("api calls = %%d, want 2 (401 then success)", got)
	}
}

func TestUnknownExpiryTokenMintsOncePerProcess(t *testing.T) {
	var mints, apiCalls int32
	srv := newCCRemintServer(&mints, &apiCalls)
	defer srv.Close()

	// Zero expiry: unknown age. The token server omits expires_in, so the
	// minted token's expiry stays unknown too — the second call must trust
	// it instead of minting again.
	cfg := ccRemintConfig(t, srv.URL, time.Time{})
	c := New(cfg, 10*time.Second, 0)
	c.NoCache = true

	for i := 0; i < 2; i++ {
		body, err := c.Get(context.Background(), "/items", nil)
		if err != nil {
			t.Fatalf("Get %%d: %%v", i, err)
		}
		if !strings.Contains(string(body), `+"`"+`"ok":true`+"`"+`) {
			t.Fatalf("Get %%d unexpected body: %%s", i, body)
		}
	}
	if got := atomic.LoadInt32(&mints); got != 1 {
		t.Fatalf("token mints = %%d, want exactly 1 per process for unknown expiry", got)
	}
	if got := atomic.LoadInt32(&apiCalls); got != 2 {
		t.Fatalf("api calls = %%d, want 2 successes", got)
	}
}
`, modulePath)

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "cc_remint_behavior_test.go"),
		[]byte(behaviorTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/client/...")
}
