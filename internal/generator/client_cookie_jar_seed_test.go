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

// TestCookieAuthClientSeedsJar pins the #2512 transport fix: a cookie-auth
// client must seed a real net/http cookie jar from the stored cookie credential
// (env-var session or browser AccessToken), so the captured session rides every
// request and net/http absorbs Set-Cookie rotation. Before the fix New() built
// the client with a nil/disk-only jar and the live request branch sent no
// cookies, so a correctly-authed cookie CLI 401'd on every call.
func TestCookieAuthClientSeedsJar(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cookieseed")
	apiSpec.BaseURL = "https://api.cookieseed.example"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		CookieDomain: ".cookieseed.example",
		Cookies:      []string{"session_id", "csrf_token"},
		EnvVars:      []string{"COOKIESEED_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "cookieseed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")

	// New() must load the persistent jar AND seed it from the cookie credential.
	assert.Contains(t, clientSrc, "cookieJar := LoadCookieJar()",
		"cookie-auth New() must build the persistent jar")
	assert.NotContains(t, clientSrc, "SeedCookieJar(cookieJar, cfg.BaseURL, cfg.CookieCredential())",
		"cookie-auth New() must not seed a session against an overridable BaseURL")
	assert.Contains(t, clientSrc, "seedDomain = canonicalCredentialDomain",
		"cookie-auth New() must fall back to the spec cookie domain for env and set-token")
	assert.Contains(t, clientSrc, "SeedCookieJarForDomain(cookieJar, seedBaseURL, cfg.CookieCredential(), seedDomain)",
		"cookie-auth New() must seed the jar from the stored cookie credential at the https canonical host")
	assert.Contains(t, clientSrc, "httpClient := newHTTPClient(timeout, cookieJar)",
		"cookie-auth client must use the seeded jar, not a nil jar")
	assert.NotContains(t, clientSrc, "newHTTPClient(timeout, nil)",
		"cookie-auth client must never construct an HTTP client with a nil jar")

	// CookieCredential() must return the RAW cookie-jar string (no Bearer
	// prefix); the env-var session wins over the file-stored browser cookie.
	assert.Contains(t, configSrc, "func (c *Config) CookieCredential() string {",
		"cookie-auth config must emit CookieCredential")
	assert.Contains(t, configSrc, "return c.CookieseedSession",
		"CookieCredential must return the env-var session unwrapped")
	assert.Contains(t, configSrc, "return c.AccessToken",
		"CookieCredential must fall back to the browser AccessToken unwrapped")
	cookieCredentialStart := strings.Index(configSrc, "func (c *Config) CookieCredential() string {")
	if cookieCredentialStart < 0 {
		t.Fatal("CookieCredential must be emitted")
	}
	cookieCredentialSrc := configSrc[cookieCredentialStart:]
	cookieCredentialEnd := strings.Index(cookieCredentialSrc, "\n}\n")
	if cookieCredentialEnd < 0 {
		t.Fatal("CookieCredential must have a complete body")
	}
	cookieCredentialSrc = cookieCredentialSrc[:cookieCredentialEnd+3]
	assert.NotContains(t, cookieCredentialSrc, "ensureAuthScheme(\"Bearer\", c.CookieseedSession)",
		"cookie auth must not prefix a raw session before placing it in the declared cookie")

	// The seed/parse helpers must be emitted (gated on HasCookies).
	jarSrc := readGeneratedFile(t, outputDir, "internal", "client", "cookiejar.go")
	assert.Contains(t, jarSrc, "func SeedCookieJar(jar http.CookieJar, baseURL, cookieStr string)")
	assert.Contains(t, jarSrc, "func parseCookieJar(s string) []*http.Cookie")
	assert.Contains(t, jarSrc, "func looksLikeCookieJar(s string) bool")
	assert.Contains(t, jarSrc, "cookie.Secure = true",
		"seeded session cookies must be https-only")
	assert.Contains(t, jarSrc, "not sending your session cookie to it",
		"rejected seed URLs must warn instead of attaching the credential")

	// Runtime proof: a seeded jar attaches the stored cookies to a request for
	// the base URL, and parseCookieJar handles the "k=v; k=v" wire format.
	runtimeTest := `package client

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestSeedCookieJarAttachesStoredCookies(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	SeedCookieJar(jar, "https://api.cookieseed.example", "session_id=abc; csrf_token=def")

	u, _ := url.Parse("https://api.cookieseed.example/items")
	got := map[string]string{}
	for _, c := range jar.Cookies(u) {
		got[c.Name] = c.Value
	}
	if got["session_id"] != "abc" || got["csrf_token"] != "def" {
		t.Fatalf("seeded jar did not attach stored cookies for the base URL: %v", got)
	}
}

func TestSeedCookieJarForDomainAttachesSubdomainCookies(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	SeedCookieJarForDomain(jar, "https://auth.example.com", "session_id=abc", ".auth.example.com")
	u, _ := url.Parse("https://api.auth.example.com/items")
	if cs := jar.Cookies(u); len(cs) != 1 || cs[0].Name != "session_id" || cs[0].Value != "abc" {
		t.Fatalf("domain-scoped seed did not attach to API subdomain: %v", cs)
	}
	u, _ = url.Parse("https://api.other.example/items")
	if cs := jar.Cookies(u); len(cs) != 0 {
		t.Fatalf("domain-scoped seed leaked outside its binding: %v", cs)
	}
}

func TestSeedCookieJarRejectsHTTPAndWrongHost(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	SeedCookieJar(jar, "http://api.cookieseed.example", "session_id=abc")
	httpsURL, _ := url.Parse("https://api.cookieseed.example/items")
	httpURL, _ := url.Parse("http://api.cookieseed.example/items")
	if cs := jar.Cookies(httpsURL); len(cs) != 0 {
		t.Fatalf("http seed must not attach cookies: %v", cs)
	}
	if cs := jar.Cookies(httpURL); len(cs) != 0 {
		t.Fatalf("http seed must not attach cookies for http://: %v", cs)
	}
	SeedCookieJar(jar, "https://evil.example.net", "session_id=abc")
	evil, _ := url.Parse("https://evil.example.net/items")
	if cs := jar.Cookies(evil); len(cs) != 0 {
		t.Fatalf("wrong-host seed must not attach cookies: %v", cs)
	}
	if cs := jar.Cookies(httpsURL); len(cs) != 0 {
		t.Fatalf("wrong-host seed must not attach cookies to the canonical host: %v", cs)
	}
}

func TestSeedCookieJarIgnoresBareToken(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	SeedCookieJar(jar, "https://api.cookieseed.example", "not-a-cookie-jar-token")
	u, _ := url.Parse("https://api.cookieseed.example/items")
	if cs := jar.Cookies(u); len(cs) != 0 {
		t.Fatalf("a bare token must not be parsed into cookies, got %v", cs)
	}
}

func TestSeedCookieJarNilJarIsNoop(t *testing.T) {
	var jar http.CookieJar
	SeedCookieJar(jar, "https://api.cookieseed.example", "session_id=abc")
}

// TestSeedCookieJarDoesNotPersist pins the no-clobber contract: seeding the
// persistent wrapper jar must only touch the in-memory inner jar, never write
// cookies.json. Otherwise a stale env/credential value overwrites a fresher
// rotation-refreshed cookie already on disk.
func TestSeedCookieJarDoesNotPersist(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	jar := LoadCookieJar()
	SeedCookieJar(jar, "https://api.cookieseed.example", "session_id=abc; csrf_token=def")

	// The seeded cookies must be live on the wrapper for the base URL...
	u, _ := url.Parse("https://api.cookieseed.example/items")
	if cs := jar.Cookies(u); len(cs) != 2 {
		t.Fatalf("seeded wrapper jar did not attach stored cookies: %v", cs)
	}
	// ...but seeding must not have written the on-disk cookie file.
	if _, err := os.Stat(cookieJarPath()); !os.IsNotExist(err) {
		t.Fatalf("SeedCookieJar must not persist to cookies.json (stat err=%v)", err)
	}
}

// TestLooksLikeCookieJarRejectsJWT pins the gate tightening: a base64-padded
// JWT contains "=" yet is a single bearer token; it must not be parsed into a
// bogus cookie, while a real name=value pair still passes.
func TestLooksLikeCookieJarRejectsJWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJlQQ=="
	if looksLikeCookieJar(jwt) {
		t.Fatalf("JWT-shaped token must be rejected by the cookie-jar gate")
	}
	jar, _ := cookiejar.New(nil)
	SeedCookieJar(jar, "https://api.cookieseed.example", jwt)
	u, _ := url.Parse("https://api.cookieseed.example/items")
	if cs := jar.Cookies(u); len(cs) != 0 {
		t.Fatalf("a JWT bearer token must not seed any cookie, got %v", cs)
	}
	if !looksLikeCookieJar("session_id=abc; csrf_token=def") {
		t.Fatalf("a real cookie-jar string must still pass the gate")
	}
	if !looksLikeCookieJar("session_id=abc") {
		t.Fatalf("a single legit name=value cookie must still pass the gate")
	}
}

func TestWriteCookieJarReplacesWWWShadow(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := cookieJarPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := []persistedCookie{{
		Name:   "session_id",
		Value:  "",
		Domain: ".www.cookieseed.example",
		Path:   "/",
		Secure: true,
	}}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteCookieJarFromMap(".cookieseed.example", map[string]string{"session_id": "fresh"}); err != nil {
		t.Fatal(err)
	}
	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var after []persistedCookie
	if err := json.Unmarshal(afterData, &after); err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Domain != ".cookieseed.example" || after[0].Value != "fresh" {
		t.Fatalf("shadowing www cookie was not replaced: %#v", after)
	}
}

func TestClearCookieJarRemovesPersistedCredentials(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := WriteCookieJarFromMap(".cookieseed.example", map[string]string{"session_id": "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := ClearCookieJar(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cookieJarPath())
	if err != nil {
		t.Fatal(err)
	}
	var rows []persistedCookie
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("ClearCookieJar left persisted credentials: %#v", rows)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "seed_runtime_test.go"),
		[]byte(runtimeTest), 0o600))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/client/", "-run", "TestSeedCookieJar|TestLooksLikeCookieJar|TestClearCookieJar")
}

// TestBearerAuthClientOmitsCookieJarSeed pins the negative: bearer/api_key auth
// must not emit any cookie-jar seeding, must construct the client with a nil
// jar, and must not emit the cookiejar.go file at all.
func TestBearerAuthClientOmitsCookieJarSeed(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearerseed")
	// minimalSpec already uses api_key/Bearer auth with no cookies.

	outputDir := filepath.Join(t.TempDir(), "bearerseed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.NotContains(t, clientSrc, "SeedCookieJar",
		"bearer auth must not seed a cookie jar")
	assert.NotContains(t, clientSrc, "LoadCookieJar",
		"bearer auth must not load a cookie jar")
	assert.NotContains(t, clientSrc, "CookieCredential",
		"bearer auth must not reference CookieCredential")
	assert.Contains(t, clientSrc, "newHTTPClient(timeout, nil)",
		"bearer auth must construct the client with a nil jar")

	if _, err := os.Stat(filepath.Join(outputDir, "internal", "client", "cookiejar.go")); !os.IsNotExist(err) {
		t.Fatalf("bearer auth must not emit cookiejar.go (stat err=%v)", err)
	}

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.NotContains(t, configSrc, "CookieCredential",
		"bearer auth config must not emit CookieCredential")
}
