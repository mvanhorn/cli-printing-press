package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCookieAuthLoginEmitsCookiesFileImport(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cookiefile")
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"session_id", "csrf"},
		EnvVars:      []string{"COOKIEFILE_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "cookiefile-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authGo, `"cookies-file"`)
	assert.Contains(t, authGo, "loadCookiesFromFile(cookiesFile, domain)")
	assert.Contains(t, authGo, "parseCookieHeaderCookies")
	assert.Contains(t, authGo, "cookieDomainMatches")
	assert.Contains(t, authGo, "client.WriteCookieJarFromMap(domain, jarCookies)")

	readme := readGeneratedFile(t, outputDir, "README.md")
	assert.Contains(t, readme, "auth login --cookies-file storage-state.json")

	cliTest := `package cli

import (
	"os"
	"strings"
	"testing"
)

func TestLoadCookiesFromFileFiltersPlaywrightState(t *testing.T) {
	path := t.TempDir() + "/state.json"
	data := []byte(` + "`" + `{"cookies":[
		{"name":"session_id","value":"abc","domain":".example.com","path":"/","secure":true,"httpOnly":true},
		{"name":"tld","value":"drop","domain":".com","path":"/"},
		{"name":"other","value":"drop","domain":".not-example.com","path":"/"}
	]}` + "`" + `)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadCookiesFromFile(path, ".example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Header != "session_id=abc" {
		t.Fatalf("Header = %q, want session_id=abc", got.Header)
	}
	if len(got.Cookies) != 1 || got.Cookies[0].Name != "session_id" {
		t.Fatalf("Cookies = %#v, want one filtered session cookie", got.Cookies)
	}
}

func TestLoadCookiesFromFileAcceptsRawCookieHeader(t *testing.T) {
	path := t.TempDir() + "/cookies.txt"
	if err := os.WriteFile(path, []byte("Cookie: session_id=abc; csrf=def"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadCookiesFromFile(path, ".example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Header != "session_id=abc; csrf=def" {
		t.Fatalf("Header = %q", got.Header)
	}
	if len(got.Cookies) != 2 {
		t.Fatalf("Cookies len = %d, want 2", len(got.Cookies))
	}
}

func TestLoadCookiesFromFileRejectsEmptyStorageState(t *testing.T) {
	path := t.TempDir() + "/state.json"
	if err := os.WriteFile(path, []byte(` + "`" + `{"cookies":[]}` + "`" + `), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadCookiesFromFile(path, ".example.com")
	if err == nil || !strings.Contains(err.Error(), "empty cookies array") {
		t.Fatalf("err = %v, want empty cookies array error", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "cookies_file_test.go"), []byte(cliTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestLoadCookiesFromFile")
	requireGeneratedCompiles(t, outputDir)
}

func TestSessionHandshakeLoginEmitsCookiesFileImport(t *testing.T) {
	t.Parallel()

	apiSpec := canonicalSessionHandshakeSpec()
	apiSpec.Auth.CookieDomain = ""
	require.NoError(t, apiSpec.Validate())
	require.Equal(t, ".query1.example.com", apiSpec.Auth.CookieDomain)

	outputDir := filepath.Join(t.TempDir(), "sessionfile-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authGo, "newAuthLoginCmd(flags)")
	assert.Contains(t, authGo, `"cookies-file"`)
	assert.Contains(t, authGo, "loadSessionCookiesFromFile")
	assert.Contains(t, authGo, `strings.Contains(candidate, ".") && strings.HasSuffix(target, "."+candidate)`)
	assert.Contains(t, authGo, `c.Session.ImportSession("https://query1.example.com", imported, "")`)
	assert.Contains(t, authGo, "c.Session.EnsureToken()")

	sessionGo := readGeneratedFile(t, outputDir, "internal", "client", "session.go")
	assert.Contains(t, sessionGo, "strings.TrimPrefix(ck.Domain, \".\")")

	cliTest := `package cli

import (
	"os"
	"strings"
	"testing"
)

func TestLoadSessionCookiesFromFileRejectsBareTLDMatch(t *testing.T) {
	path := t.TempDir() + "/state.json"
	data := []byte(` + "`" + `{"cookies":[
		{"name":"session","value":"drop","domain":".com","path":"/"},
		{"name":"crumb","value":"abc","domain":".query1.example.com","path":"/"}
	]}` + "`" + `)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSessionCookiesFromFile(path, ".query1.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "crumb" {
		t.Fatalf("Cookies = %#v, want one scoped cookie", got)
	}
}

func TestLoadSessionCookiesFromFileRejectsEmptyStorageState(t *testing.T) {
	path := t.TempDir() + "/state.json"
	if err := os.WriteFile(path, []byte(` + "`" + `{"cookies":[]}` + "`" + `), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadSessionCookiesFromFile(path, ".query1.example.com")
	if err == nil || !strings.Contains(err.Error(), "empty cookies array") {
		t.Fatalf("err = %v, want empty cookies array error", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "session_cookies_file_test.go"), []byte(cliTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestLoadSessionCookiesFromFile")
	requireGeneratedCompiles(t, outputDir)
}
