package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestClientCheckRedirectDeletesHeaderOnCrossHost(t *testing.T) {
	t.Parallel()

	t.Run("api_key custom header deletes cross-host", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-cross-host-apikey")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"REDIRECT_CROSS_HOST_APIKEY"},
		}

		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, `if req.URL.Host != via[0].URL.Host || (via[0].URL.Scheme == "https" && req.URL.Scheme == "http") {`)
		require.Contains(t, closure, `if req.URL.Host == via[0].URL.Host && (req.URL.Scheme == via[0].URL.Scheme || (via[0].URL.Scheme == "http" && req.URL.Scheme == "https")) {`)
		require.Contains(t, closure, `req.Header.Set("X-Api-Key", h)`)
		require.Contains(t, closure, `req.Header.Del("X-Api-Key")`)
	})

	t.Run("session handshake header deletes cross-host", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-cross-host-session-header")
		apiSpec.Auth = spec.AuthConfig{
			Type:           "session_handshake",
			TokenParamIn:   "header",
			TokenParamName: "X-Kit-Api-Key",
			EnvVars:        []string{"REDIRECT_CROSS_HOST_SESSION_HEADER"},
		}

		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, `if req.URL.Host == via[0].URL.Host && (req.URL.Scheme == via[0].URL.Scheme || (via[0].URL.Scheme == "http" && req.URL.Scheme == "https")) {`)
		require.Contains(t, closure, `req.Header.Set("X-Kit-Api-Key", h)`)
		require.Contains(t, closure, `} else {`)
		require.Contains(t, closure, `req.Header.Del("X-Kit-Api-Key")`)
	})
}

// Same-host http -> https must keep a custom auth header; https -> http must
// drop it. Exact-scheme equality treats the upgrade as an origin change and
// strips the credential, which is the Greptile P1 on #4292.
func TestClientCheckRedirectKeepsAuthOnHTTPUpgrade(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("redirect-scheme-upgrade")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		Header:  "X-API-Key",
		EnvVars: []string{"REDIRECT_SCHEME_UPGRADE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), apiSpec.Name+"-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	closure := checkRedirectClosureBody(t, readGeneratedFile(t, outputDir, "internal", "client", "client.go"))
	require.Contains(t, closure, `via[0].URL.Scheme == "http" && req.URL.Scheme == "https"`)
	require.Contains(t, closure, `via[0].URL.Scheme == "https" && req.URL.Scheme == "http"`)
	require.NotContains(t, closure, `req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme`)
	require.NotContains(t, closure, `req.URL.Host == via[0].URL.Host && req.URL.Scheme == via[0].URL.Scheme {`)

	modulePath := generatedModulePath(t, outputDir)
	runtimeTest := `package client

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"` + modulePath + `/internal/config"
)

func TestRedirectSchemeUpgradeKeepsCustomAuth(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "https://api.example.com",
		AuthHeaderVal: "secret-key",
	}
	c := New(cfg, time.Second, 0)

	check := func(t *testing.T, from, to, want string) {
		t.Helper()
		viaURL, err := url.Parse(from)
		if err != nil {
			t.Fatal(err)
		}
		targetURL, err := url.Parse(to)
		if err != nil {
			t.Fatal(err)
		}
		via := &http.Request{URL: viaURL}
		req := &http.Request{URL: targetURL, Header: http.Header{"X-Api-Key": {"secret-key"}}}
		if err := c.HTTPClient.CheckRedirect(req, []*http.Request{via}); err != nil {
			t.Fatalf("CheckRedirect returned error: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != want {
			t.Fatalf("redirect %s -> %s: X-API-Key = %q, want %q", from, to, got, want)
		}
	}

	t.Run("same-host http to https keeps header", func(t *testing.T) {
		check(t, "http://api.example.com/start", "https://api.example.com/done", "secret-key")
	})
	t.Run("same-host https to http drops header", func(t *testing.T) {
		check(t, "https://api.example.com/start", "http://api.example.com/done", "")
	})
	t.Run("same-origin https keeps header", func(t *testing.T) {
		check(t, "https://api.example.com/start", "https://api.example.com/done", "secret-key")
	})
	t.Run("cross-host drops header", func(t *testing.T) {
		check(t, "https://api.example.com/start", "https://evil.example.net/done", "")
	})
}
`
	runtimePath := filepath.Join(outputDir, "internal", "client", "redirect_scheme_runtime_test.go")
	require.NoError(t, os.WriteFile(runtimePath, []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestRedirectSchemeUpgradeKeepsCustomAuth$", "-count=1")
}
