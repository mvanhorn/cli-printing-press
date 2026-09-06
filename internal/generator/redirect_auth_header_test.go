package generator

import (
	"os"
	"path/filepath"
	"strings"
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

		requireRedirectOriginHelper(t, client)
		require.Contains(t, closure, `if redirectLeavesOrigin(req.URL, via) {`)
		require.Contains(t, closure, `if !redirectLeavesOrigin(req.URL, via) {`)
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

		requireRedirectOriginHelper(t, client)
		require.Contains(t, closure, `if !redirectLeavesOrigin(req.URL, via) {`)
		require.Contains(t, closure, `req.Header.Set("X-Kit-Api-Key", h)`)
		require.Contains(t, closure, `} else {`)
		require.Contains(t, closure, `req.Header.Del("X-Kit-Api-Key")`)
		require.NotContains(t, closure, "Cross-host hop:")
		require.NotContains(t, closure, "cross-host hops")
		require.NotContains(t, closure, "same-host redirects")
	})
}

func TestClientCheckRedirectOriginHelperAtAllSites(t *testing.T) {
	t.Parallel()

	t.Run("custom header and query strip", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-site-strip")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			In:      "query",
			Header:  "api_key",
			EnvVars: []string{"REDIRECT_SITE_STRIP_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)
		requireRedirectOriginHelper(t, client)
		require.Contains(t, closure, `if redirectLeavesOrigin(req.URL, via) {`)
		require.Contains(t, closure, "Query credentials are removed above when the hop leaves the origin")
		require.NotContains(t, closure, "cross-host hops")
		require.NotContains(t, closure, "same-host redirects")
	})

	t.Run("tier-routing header names", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-site-tier")
		apiSpec.TierRouting = spec.TierRoutingConfig{
			DefaultTier: "free",
			Tiers: map[string]spec.TierConfig{
				"free": {Auth: spec.AuthConfig{Type: "none"}},
				"paid": {
					Auth: spec.AuthConfig{
						Type:    "api_key",
						Header:  "X-Tier-Key",
						EnvVars: []string{"REDIRECT_SITE_TIER_TOKEN"},
					},
				},
			},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)
		requireRedirectOriginHelper(t, client)
		require.Equal(t, 2, strings.Count(closure, "if redirectLeavesOrigin(req.URL, via) {"),
			"tier-routing must use the shared helper for both the custom-header strip and the tier header-name strip")
		require.Contains(t, closure, `req.Header.Del("X-Tier-Key")`)
	})

	t.Run("session handshake re-stamp", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-site-session")
		apiSpec.Auth = spec.AuthConfig{
			Type:           "session_handshake",
			TokenParamIn:   "header",
			TokenParamName: "X-Kit-Api-Key",
			EnvVars:        []string{"REDIRECT_SITE_SESSION_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)
		requireRedirectOriginHelper(t, client)
		require.Contains(t, closure, `if redirectLeavesOrigin(req.URL, via) {`)
		require.Contains(t, closure, `if !redirectLeavesOrigin(req.URL, via) {`)
	})

	t.Run("in cookie non-jar drop by name", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-site-cookie")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			In:      "cookie",
			Header:  "session_token",
			EnvVars: []string{"REDIRECT_SITE_COOKIE_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)
		requireRedirectOriginHelper(t, client)
		require.Equal(t, 2, strings.Count(closure, "if redirectLeavesOrigin(req.URL, via) {"),
			"in:cookie non-jar must use the shared helper for both the credential strip and the drop-by-name path")
		require.Contains(t, closure, `if ck.Name != "session_token"`)
	})

	t.Run("default and composed re-stamp", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-site-composed")
		apiSpec.Auth = spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "Bearer {token}",
			CookieDomain: ".auth.example.com",
			Cookies:      []string{"session_id"},
			EnvVars:      []string{"REDIRECT_SITE_COMPOSED_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)
		requireRedirectOriginHelper(t, client)
		require.Contains(t, closure, `if !redirectLeavesOrigin(req.URL, via) && c.credentialAppliesToURL(req.URL.String()) {`)
	})
}

// Same-origin keeps a custom auth header; https -> http drops it; http -> https
// keeps it. The shared helper is the only origin predicate.
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

	client := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	closure := checkRedirectClosureBody(t, client)
	requireRedirectOriginHelper(t, client)
	require.Contains(t, closure, `if redirectLeavesOrigin(req.URL, via) {`)
	require.Contains(t, closure, `if !redirectLeavesOrigin(req.URL, via) {`)
	require.NotContains(t, closure, `req.URL.Host != via[len(via)-1].URL.Host || req.URL.Scheme != via[len(via)-1].URL.Scheme`)
	require.NotContains(t, closure, `req.URL.Host == via[len(via)-1].URL.Host && req.URL.Scheme == via[len(via)-1].URL.Scheme {`)

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

	t.Run("same-origin keeps header", func(t *testing.T) {
		check(t, "https://api.example.com/start", "https://api.example.com/done", "secret-key")
	})
	t.Run("https to http drops header", func(t *testing.T) {
		check(t, "https://api.example.com/start", "http://api.example.com/done", "")
	})
	t.Run("http to https keeps header", func(t *testing.T) {
		check(t, "http://api.example.com/start", "https://api.example.com/done", "secret-key")
	})
	t.Run("https to http to http drops header", func(t *testing.T) {
		httpsStart, err := url.Parse("https://api.example.com/start")
		if err != nil {
			t.Fatal(err)
		}
		httpMid, err := url.Parse("http://api.example.com/mid")
		if err != nil {
			t.Fatal(err)
		}
		httpEnd, err := url.Parse("http://api.example.com/done")
		if err != nil {
			t.Fatal(err)
		}
		via := []*http.Request{{URL: httpsStart}, {URL: httpMid}}
		req := &http.Request{URL: httpEnd, Header: http.Header{"X-Api-Key": {"secret-key"}}}
		if err := c.HTTPClient.CheckRedirect(req, via); err != nil {
			t.Fatalf("CheckRedirect returned error: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("https -> http -> http: X-API-Key = %q, want empty", got)
		}
	})
	t.Run("http to https to http drops header", func(t *testing.T) {
		httpStart, err := url.Parse("http://api.example.com/start")
		if err != nil {
			t.Fatal(err)
		}
		httpsMid, err := url.Parse("https://api.example.com/mid")
		if err != nil {
			t.Fatal(err)
		}
		httpEnd, err := url.Parse("http://api.example.com/done")
		if err != nil {
			t.Fatal(err)
		}
		via := []*http.Request{{URL: httpStart}, {URL: httpsMid}}
		req := &http.Request{URL: httpEnd, Header: http.Header{"X-Api-Key": {"secret-key"}}}
		if err := c.HTTPClient.CheckRedirect(req, via); err != nil {
			t.Fatalf("CheckRedirect returned error: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("http -> https -> http: X-API-Key = %q, want empty", got)
		}
	})
	t.Run("http to https to http to http drops header", func(t *testing.T) {
		httpStart, err := url.Parse("http://api.example.com/start")
		if err != nil {
			t.Fatal(err)
		}
		httpsMid, err := url.Parse("https://api.example.com/mid")
		if err != nil {
			t.Fatal(err)
		}
		httpThird, err := url.Parse("http://api.example.com/third")
		if err != nil {
			t.Fatal(err)
		}
		httpEnd, err := url.Parse("http://api.example.com/done")
		if err != nil {
			t.Fatal(err)
		}
		via := []*http.Request{{URL: httpStart}, {URL: httpsMid}, {URL: httpThird}}
		req := &http.Request{URL: httpEnd, Header: http.Header{"X-Api-Key": {"secret-key"}}}
		if err := c.HTTPClient.CheckRedirect(req, via); err != nil {
			t.Fatalf("CheckRedirect returned error: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("http -> https -> http -> http: X-API-Key = %q, want empty", got)
		}
	})
	t.Run("foreign host then same-host hop drops header", func(t *testing.T) {
		start, err := url.Parse("https://a.example/start")
		if err != nil {
			t.Fatal(err)
		}
		mid, err := url.Parse("https://b.example/mid")
		if err != nil {
			t.Fatal(err)
		}
		done, err := url.Parse("https://b.example/done")
		if err != nil {
			t.Fatal(err)
		}
		via := []*http.Request{{URL: start}, {URL: mid}}
		req := &http.Request{URL: done, Header: http.Header{"X-Api-Key": {"secret-key"}}}
		if err := c.HTTPClient.CheckRedirect(req, via); err != nil {
			t.Fatalf("CheckRedirect returned error: %v", err)
		}
		if got := req.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("https://a.example -> https://b.example -> https://b.example: X-API-Key = %q, want empty", got)
		}
	})
}
`
	runtimePath := filepath.Join(outputDir, "internal", "client", "redirect_scheme_runtime_test.go")
	require.NoError(t, os.WriteFile(runtimePath, []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestRedirectSchemeUpgradeKeepsCustomAuth$", "-count=1")
}
