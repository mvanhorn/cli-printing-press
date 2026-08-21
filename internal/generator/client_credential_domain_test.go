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
		"environment auth overrides must clear a stale browser binding so they do not load the persisted jar")
	assert.Contains(t, configSrc, "UsePersistedCookieJar",
		"cookie clients must be able to suppress stale persisted browser cookies")
	assert.Contains(t, credentialsSrc, "credential_domain",
		"external credentials must carry the browser binding with the credential")
	assert.Contains(t, authSrc, `cfg.CredentialDomain = ".auth.example.com"`,
		"browser login and refresh must bind the stored credential to the capture domain")
	assert.Contains(t, clientSrc, "const canonicalCredentialDomain = \".auth.example.com\"",
		"every token source must bind to the spec cookie_domain")
	assert.Contains(t, clientSrc, "seedDomain = canonicalCredentialDomain",
		"empty CredentialDomain must fall back to the spec cookie_domain")
	assert.Contains(t, clientSrc, `seedBaseURL := "https://" + strings.TrimPrefix(seedDomain, ".")`,
		"cookie seeding must use the https canonical host instead of the merged BaseURL")
	assert.Contains(t, clientSrc, "SeedCookieJarForDomain(cookieJar, seedBaseURL, cfg.CookieCredential(), seedDomain)",
		"cookie seeding must preserve the canonical domain across its subdomains")
	assert.NotContains(t, clientSrc, "SeedCookieJar(cookieJar, cfg.BaseURL, cfg.CookieCredential())",
		"cookie seeding must never attach a session to an operator-overridable BaseURL")
	assert.Contains(t, clientSrc, "credentialAllowed := c.credentialAppliesToURL(targetURL)",
		"requests must check the captured credential domain before injection")
	assert.Contains(t, clientSrc, "if authHeader != \"\" && credentialAllowed {",
		"the primary auth header must be withheld from unrelated hosts")
	assert.Contains(t, clientSrc, "if !credentialAllowed && isCredentialHeader(k)",
		"config Headers must not restore Authorization or Cookie after a deny")
	assert.Contains(t, clientSrc, "req.URL.Host == via[0].URL.Host && req.URL.Scheme == via[0].URL.Scheme && c.credentialAppliesToURL(req.URL.String())",
		"same-host redirect re-stamping must retain the credential-domain gate")
	assert.NotContains(t, clientSrc, "publicsuffix.EffectiveTLDPlusOne",
		"credential matching must not authorize eTLD+1 siblings of the canonical host")
	assert.NotContains(t, clientSrc, "golang.org/x/net/publicsuffix",
		"credential binding must not import publicsuffix")
	assert.NotContains(t, goMod, "golang.org/x/net v0.55.0",
		"cookie binding must not pull x/net just for host matching")

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
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"credentialdomain-pp-cli/internal/config"
)

func TestCredentialAppliesToURL(t *testing.T) {
	bound := &Client{Config: &config.Config{CredentialDomain: ".auth.example.com"}}
	hostSpecific := &Client{Config: &config.Config{CredentialDomain: ".app.example.com"}}
	unbound := &Client{Config: &config.Config{}}
	envTok := &Client{Config: &config.Config{AuthSource: "env:CREDENTIALDOMAIN_SESSION"}}
	for _, tc := range []struct {
		name   string
		client *Client
		url    string
		want   bool
	}{
		{name: "captured host", client: bound, url: "https://login.auth.example.com/session", want: true},
		{name: "canonical subdomain", client: bound, url: "https://api.auth.example.com/items", want: true},
		{name: "sibling eTLD+1 host", client: bound, url: "https://api.example.com/items", want: false},
		{name: "sibling other.example.com", client: bound, url: "https://other.example.com/items", want: false},
		{name: "host-specific exact", client: hostSpecific, url: "https://app.example.com/session", want: true},
		{name: "host-specific subdomain", client: hostSpecific, url: "https://login.app.example.com/session", want: true},
		{name: "host-specific sibling", client: hostSpecific, url: "https://api.example.com/items", want: false},
		{name: "http canonical host", client: bound, url: "http://login.auth.example.com/session", want: false},
		{name: "credential-free host", client: bound, url: "https://api.credential-free.example/items", want: false},
		{name: "empty domain uses spec https host", client: unbound, url: "https://login.auth.example.com/session", want: true},
		{name: "empty domain denies http", client: unbound, url: "http://login.auth.example.com/session", want: false},
		{name: "empty domain denies wrong host", client: unbound, url: "https://api.credential-free.example/items", want: false},
		{name: "empty domain denies sibling eTLD+1", client: unbound, url: "https://api.example.com/items", want: false},
		{name: "env token uses spec https host", client: envTok, url: "https://api.auth.example.com/items", want: true},
		{name: "env token denies http", client: envTok, url: "http://api.auth.example.com/items", want: false},
		{name: "env token denies wrong host", client: envTok, url: "https://api.credential-free.example/items", want: false},
		{name: "env token denies sibling eTLD+1", client: envTok, url: "https://api.example.com/items", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.client.credentialAppliesToURL(tc.url); got != tc.want {
				t.Fatalf("credentialAppliesToURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	if !bound.credentialAppliesToURL("http://127.0.0.1:12345/items") {
		t.Fatal("verified live HTTP must preserve mock-host credential behavior")
	}
	if bound.credentialAppliesToURL("https://api.credential-free.example/items") {
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
	if client.credentialAppliesToURL("https://api.credential-free.example/items") {
		t.Fatal("environment override must stay bound to the https canonical host")
	}
	httpTarget, _ := url.Parse("http://api.auth.example.com/items")
	if cs := client.HTTPClient.Jar.Cookies(httpTarget); len(cs) != 0 {
		t.Fatalf("env cookie override leaked to http://: %v", cs)
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
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		want := "session_id=env"
		if requests == 2 {
			want = "session_id=rotated"
		}
		if got := r.Header.Get("Cookie"); got != want {
			t.Errorf("request %d Cookie = %q, want %q", requests, got, want)
		}
		if requests == 1 {
			http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "rotated", Path: "/", Secure: true, Domain: "auth.example.com"})
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:          "https://api.auth.example.com",
		AccessToken:      "session_id=env",
		AuthSource:       "env:CREDENTIALDOMAIN_SESSION",
		CredentialSource: "env:CREDENTIALDOMAIN_SESSION",
	}
	client := New(cfg, time.Second, 0)
	client.HTTPClient.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	for i := 0; i < 2; i++ {
		resp, err := client.HTTPClient.Get("https://api.auth.example.com/rotate")
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

func TestEnvCookieBindsToCanonicalHTTPSHost(t *testing.T) {
	cfg := &config.Config{
		BaseURL:          "http://evil.example.net",
		AccessToken:      "session_id=env",
		AuthSource:       "env:CREDENTIALDOMAIN_SESSION",
		CredentialSource: "env:CREDENTIALDOMAIN_SESSION",
	}
	client := New(cfg, time.Second, 0)
	good, _ := url.Parse("https://api.auth.example.com/items")
	if cs := client.HTTPClient.Jar.Cookies(good); len(cs) != 1 || cs[0].Value != "env" {
		t.Fatalf("env cookie was not bound to the https canonical host: %v", cs)
	}
	httpCanon, _ := url.Parse("http://api.auth.example.com/items")
	if cs := client.HTTPClient.Jar.Cookies(httpCanon); len(cs) != 0 {
		t.Fatalf("env cookie leaked to http://: %v", cs)
	}
	evil, _ := url.Parse("https://evil.example.net/items")
	if cs := client.HTTPClient.Jar.Cookies(evil); len(cs) != 0 {
		t.Fatalf("env cookie leaked to a wrong host: %v", cs)
	}
	if !client.credentialAppliesToURL(good.String()) {
		t.Fatal("env token must apply to the https canonical host")
	}
	if client.credentialAppliesToURL(httpCanon.String()) || client.credentialAppliesToURL(evil.String()) {
		t.Fatal("env token must not apply to http:// or a wrong host")
	}
}

func TestSetTokenCredentialBindsToCanonicalHTTPSHost(t *testing.T) {
	const jwt = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzZXR0b2tlbiIsIm5hbWUiOiJwcC10ZXN0IiwiaWF0IjoxNTE2MjM5MDIyfQ.extra-padding-to-satisfy-the-one-hundred-fifty-character-jwt-shape-gate"
	cfg := &config.Config{
		BaseURL:          "https://api.auth.example.com",
		AuthHeaderVal:    "",
		AccessToken:      jwt,
		CredentialDomain: "",
	}
	client := New(cfg, time.Second, 0)
	if !client.credentialAppliesToURL("https://login.auth.example.com/session") {
		t.Fatal("set-token must bind to the https canonical host")
	}
	if client.credentialAppliesToURL("http://login.auth.example.com/session") {
		t.Fatal("set-token must not send Authorization over http://")
	}
	if client.credentialAppliesToURL("https://api.credential-free.example/items") {
		t.Fatal("set-token must not send Authorization to a wrong host")
	}
	if client.credentialAppliesToURL("https://api.example.com/items") {
		t.Fatal("set-token must not send Authorization to an eTLD+1 sibling")
	}
}

func TestConfigHeadersCannotPunchThroughDeniedHost(t *testing.T) {
	var gotAuth, gotCookie, gotExtra, gotKeep string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotExtra = r.Header.Get("X-Extra")
		gotKeep = r.Header.Get("X-API-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:          server.URL,
		AuthHeaderVal:    "Bearer primary",
		AccessToken:      "session_id=env",
		AuthSource:       "env:CREDENTIALDOMAIN_SESSION",
		CredentialSource: "env:CREDENTIALDOMAIN_SESSION",
		Headers: map[string]string{
			"Authorization": "Bearer punched",
			"Cookie":        "session_id=punched",
			"X-Extra":       "additional",
			"X-API-Version": "keep-me",
		},
	}
	client := New(cfg, time.Second, 0)
	client.NoCache = true
	if _, err := client.Get(context.Background(), "/items", nil); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization reached a denied host: %q", gotAuth)
	}
	if gotCookie != "" {
		t.Fatalf("Cookie reached a denied host: %q", gotCookie)
	}
	if gotExtra != "" {
		t.Fatalf("additional auth header reached a denied host: %q", gotExtra)
	}
	if gotKeep != "keep-me" {
		t.Fatalf("non-credential header = %q, want keep-me", gotKeep)
	}
}
`
	runtimePath := filepath.Join(outputDir, "internal", "client", "credential_domain_runtime_test.go")
	require.NoError(t, os.WriteFile(runtimePath, []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^Test(CredentialAppliesToURL|CookieOverrideDoesNotInheritPersistedBrowserJar|CookieOverrideRotatesInMemoryWithoutPersisting|EnvCookieBindsToCanonicalHTTPSHost|SetTokenCredentialBindsToCanonicalHTTPSHost|ConfigHeadersCannotPunchThroughDeniedHost)$", "-count=1")
}
