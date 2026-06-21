package generator

import (
	"os"
	"path/filepath"
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
		Type:    "cookie",
		Header:  "Cookie",
		Cookies: []string{"session_id", "csrf_token"},
		EnvVars: []string{"COOKIESEED_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "cookieseed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")

	// New() must load the persistent jar AND seed it from the cookie credential.
	assert.Contains(t, clientSrc, "cookieJar := LoadCookieJar()",
		"cookie-auth New() must build the persistent jar")
	assert.Contains(t, clientSrc, "SeedCookieJar(cookieJar, cfg.BaseURL, cfg.CookieCredential())",
		"cookie-auth New() must seed the jar from the stored cookie credential")
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

	// The seed/parse helpers must be emitted (gated on HasCookies).
	jarSrc := readGeneratedFile(t, outputDir, "internal", "client", "cookiejar.go")
	assert.Contains(t, jarSrc, "func SeedCookieJar(jar http.CookieJar, baseURL, cookieStr string)")
	assert.Contains(t, jarSrc, "func parseCookieJar(s string) []*http.Cookie")
	assert.Contains(t, jarSrc, "func looksLikeCookieJar(s string) bool")

	// Runtime proof: a seeded jar attaches the stored cookies to a request for
	// the base URL, and parseCookieJar handles the "k=v; k=v" wire format.
	runtimeTest := `package client

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
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
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "seed_runtime_test.go"),
		[]byte(runtimeTest), 0o600))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/client/", "-run", "TestSeedCookieJar|TestLooksLikeCookieJar")
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
