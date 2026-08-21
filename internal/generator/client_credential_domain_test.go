package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBrowserCredentialBindsToCapturedDomain(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("credentialdomain")
	apiSpec.BaseURL = "https://api.credential-free.example"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "composed",
		Header:       "Authorization",
		Format:       "Bearer {token}",
		CookieDomain: ".auth.example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{"CREDENTIALDOMAIN_SESSION"},
		AdditionalHeaders: []spec.AdditionalAuthHeader{
			{Header: "X-Extra", In: "header", EnvVar: spec.AuthEnvVar{Name: "CREDENTIALDOMAIN_EXTRA"}},
			{Header: "extra_query", In: "query", EnvVar: spec.AuthEnvVar{Name: "CREDENTIALDOMAIN_QUERY"}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "credentialdomain-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	credentialsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	goMod := readGeneratedFile(t, outputDir, "go.mod")

	assert.Contains(t, configSrc, "CredentialDomain",
		"browser-session config must persist the captured credential domain")
	assert.Contains(t, configSrc, `toml:"credential_domain,omitempty"`,
		"browser-session config must serialize the captured credential domain")
	assert.Contains(t, configSrc, "CredentialDomain: c.CredentialDomain",
		"persisted config must carry the captured credential domain")
	assert.Contains(t, configSrc, "CredentialDomain = \"\"",
		"environment auth overrides must clear a stale browser binding")
	assert.Contains(t, configSrc, "UsePersistedCookieJar",
		"cookie clients must be able to suppress stale persisted browser cookies")
	assert.Contains(t, credentialsSrc, "credential_domain",
		"external credentials must carry the browser binding with the credential")
	assert.Contains(t, authSrc, `cfg.CredentialDomain = ".auth.example.com"`,
		"browser login and refresh must bind the stored credential to the capture domain")
	assert.Contains(t, clientSrc, `seedBaseURL := "https://" + strings.TrimPrefix(cfg.CredentialDomain, ".")`,
		"cookie seeding must use the captured domain instead of the merged BaseURL")
	assert.Contains(t, clientSrc, "SeedCookieJarForDomain(cookieJar, seedBaseURL, cfg.CookieCredential(), cfg.CredentialDomain)",
		"cookie seeding must preserve the captured domain across its subdomains")
	assert.Contains(t, clientSrc, "credentialAllowed := c.credentialAppliesToURL(targetURL)",
		"requests must check the captured credential domain before injection")
	assert.Contains(t, clientSrc, "if authHeader != \"\" && credentialAllowed {",
		"the primary auth header must be withheld from unrelated hosts")
	assert.Contains(t, clientSrc, "req.URL.Host == via[0].URL.Host && req.URL.Scheme == via[0].URL.Scheme && c.credentialAppliesToURL(req.URL.String())",
		"same-host redirect re-stamping must retain the credential-domain gate")
	assert.Contains(t, clientSrc, "publicsuffix.EffectiveTLDPlusOne",
		"credential matching must use registrable domains")
	assert.Contains(t, goMod, "golang.org/x/net v0.55.0",
		"browser-session clients must declare the publicsuffix dependency")

	// The negative path must remain unchanged for ordinary token auth: it has
	// no capture-time domain to bind and should not gain browser-only baggage.
	bearer := minimalSpec("credentialdomain-bearer")
	bearerDir := filepath.Join(t.TempDir(), "credentialdomain-bearer-pp-cli")
	require.NoError(t, New(bearer, bearerDir).Generate())
	bearerClient := readGeneratedFile(t, bearerDir, "internal", "client", "client.go")
	bearerConfig := readGeneratedFile(t, bearerDir, "internal", "config", "config.go")
	bearerAuth := readGeneratedFile(t, bearerDir, "internal", "cli", "auth.go")
	bearerMod := readGeneratedFile(t, bearerDir, "go.mod")
	requireGeneratedCompiles(t, bearerDir)
	assert.NotContains(t, bearerClient, "credentialAppliesToURL",
		"ordinary token auth must not emit browser credential binding")
	assert.NotContains(t, bearerConfig, "CredentialDomain",
		"ordinary token auth config must not emit browser credential binding")
	assert.NotContains(t, bearerAuth, "CredentialDomain",
		"ordinary token auth commands must not reference browser credential binding")
	assert.NotContains(t, bearerMod, "golang.org/x/net v0.55.0",
		"ordinary token auth must not gain the browser-only dependency")
	assert.Equal(t, 1, strings.Count(authSrc, `cfg.CredentialDomain = ".auth.example.com"`),
		"browser login should record the capture domain")

	runtimeTest := `package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"credentialdomain-pp-cli/internal/config"
)

func TestCredentialAppliesToURL(t *testing.T) {
	c := &Client{Config: &config.Config{CredentialDomain: ".auth.example.com"}}
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "captured host", url: "https://login.auth.example.com/session", want: true},
		{name: "same registrable domain", url: "https://api.auth.example.com/items", want: true},
		{name: "credential-free host", url: "https://api.credential-free.example/items", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.credentialAppliesToURL(tc.url); got != tc.want {
				t.Fatalf("credentialAppliesToURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	if !c.credentialAppliesToURL("http://127.0.0.1:12345/items") {
		t.Fatal("verified live HTTP must preserve mock-host credential behavior")
	}
	if c.credentialAppliesToURL("https://api.credential-free.example/items") {
		t.Fatal("verify-live HTTP must not bypass matching for real targets")
	}
}

func TestCredentialRedirectStripsCrossHostCredentials(t *testing.T) {
	cfg := &config.Config{
		BaseURL:          "https://api.auth.example.com",
		AuthHeaderVal:    "Bearer primary",
		AccessToken:      "session_id=browser",
		CredentialDomain: ".auth.example.com",
	}
	c := New(cfg, time.Second, 0)
	viaURL, _ := url.Parse("https://api.auth.example.com/start")
	targetURL, _ := url.Parse("https://evil.example.net/done?extra_query=secret&keep=1")
	via := &http.Request{URL: viaURL}
	req := &http.Request{URL: targetURL, Header: http.Header{
		"Authorization": {"Bearer primary"},
		"X-Extra":       {"additional"},
		"Cookie":        {"session_id=browser"},
	}}
	if err := c.HTTPClient.CheckRedirect(req, []*http.Request{via}); err != nil {
		t.Fatalf("CheckRedirect returned error: %v", err)
	}
	for _, name := range []string{"Authorization", "X-Extra", "Cookie"} {
		if got := req.Header.Get(name); got != "" {
			t.Fatalf("cross-host redirect retained %s=%q", name, got)
		}
	}
	if got := req.URL.Query().Get("extra_query"); got != "" {
		t.Fatalf("cross-host redirect retained additional query credential %q", got)
	}
}

func TestCookieOverrideDoesNotInheritPersistedBrowserJar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := WriteCookieJarFromMap(".auth.example.com", map[string]string{"session_id": "browser"}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		BaseURL:          "https://api.auth.example.com",
		AccessToken:      "session_id=env",
		AuthSource:       "env:CREDENTIALDOMAIN_SESSION",
		CredentialSource: "env:CREDENTIALDOMAIN_SESSION",
		CredentialDomain: ".auth.example.com",
	}
	client := New(cfg, time.Second, 0)
	target, _ := url.Parse("https://api.auth.example.com/items")
	cookies := client.HTTPClient.Jar.Cookies(target)
	if len(cookies) != 1 || cookies[0].Value != "env" {
		t.Fatalf("env cookie override inherited a persisted browser jar: %v", cookies)
	}
	cfg.AuthSource = "env:CREDENTIALDOMAIN_SESSION"
	if !client.credentialAppliesToURL("https://api.credential-free.example/items") {
		t.Fatal("environment override inherited a stale browser domain binding")
	}
}

func TestCookieOverrideRotatesInMemoryWithoutPersisting(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := WriteCookieJarFromMap(".auth.example.com", map[string]string{"session_id": "browser"}); err != nil {
		t.Fatal(err)
	}
	path := cookieJarPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		want := "session_id=env"
		if requests == 2 {
			want = "session_id=rotated"
		}
		if got := r.Header.Get("Cookie"); got != want {
			t.Errorf("request %d Cookie = %q, want %q", requests, got, want)
		}
		if requests == 1 {
			http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "rotated", Path: "/"})
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:          server.URL,
		AccessToken:      "session_id=env",
		AuthSource:       "env:CREDENTIALDOMAIN_SESSION",
		CredentialSource: "env:CREDENTIALDOMAIN_SESSION",
	}
	client := New(cfg, time.Second, 0)
	for i := 0; i < 2; i++ {
		resp, err := client.HTTPClient.Get(server.URL + "/rotate")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if requests != 2 {
		t.Fatalf("server saw %d requests, want 2", requests)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("override response cookie changed persisted browser jar: before=%s after=%s", before, after)
	}
}
`
	runtimePath := filepath.Join(outputDir, "internal", "client", "credential_domain_runtime_test.go")
	require.NoError(t, os.WriteFile(runtimePath, []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^Test(CredentialAppliesToURL|CookieOverrideDoesNotInheritPersistedBrowserJar|CookieOverrideRotatesInMemoryWithoutPersisting)$", "-count=1")
}
