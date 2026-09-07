package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBrowserClearanceAuthLoginSucceedsAsGenerated(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:          "clearance-auth",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChrome,
		Auth: spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "Bearer {session}",
			CookieDomain: "www.example.com",
			Cookies:      []string{"cf_clearance", "session"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/clearance-auth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "clearance-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authGo, "client.NewProbeHTTPClient(5*time.Second, false)")
	assert.NotContains(t, authGo, "&http.Client{Timeout: 5 * time.Second}")
	assert.Contains(t, authGo, `"skip-validation"`)
	assert.Contains(t, authGo, "validateComposedAuthProbe(composed, validationCookies, !skipValidation)")
	assert.Contains(t, authGo, "cookieNameLooksCSRF")
	assert.Contains(t, authGo, "decodeCookieValue")
	assert.Contains(t, authGo, "url.PathUnescape")
	assert.Contains(t, authGo, `exec.Command("sqlite3", "-separator", "\t", tmpPath, "SELECT host_key, name FROM cookies")`)
	assert.Contains(t, authGo, "cookieDomainMatches(hostKey, domain)")
	assert.NotContains(t, authGo, "host_key LIKE")
	assert.NotContains(t, authGo, "domainPattern")

	clientGo := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientGo, "func NewProbeHTTPClient(timeout time.Duration, skipTLSVerify bool) *http.Client")
	assert.Contains(t, clientGo, "return newHTTPClient(timeout, nil, skipTLSVerify)")

	requireGeneratedCompiles(t, outputDir)

	modulePath := generatedModulePath(t, outputDir)
	testSrc := fmt.Sprintf(`package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	%q
)

func TestCookieDomainMatchesParentHost(t *testing.T) {
	if !cookieDomainMatches(".example.com", "www.example.com") {
		t.Fatal("cookie on .example.com must match cookie_domain www.example.com")
	}
	if cookieDomainMatches(".other.com", "www.example.com") {
		t.Fatal("unrelated domain cookies must not match")
	}
	if cookieDomainMatches(".com", "www.example.com") {
		t.Fatal("bare TLD cookies must not match")
	}
}

func TestInspectCookiesForDomainFindsParentDomainCookie(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for Chrome cookie discovery")
	}
	db := filepath.Join(t.TempDir(), "Cookies")
	sql := "CREATE TABLE cookies (host_key TEXT, name TEXT);" +
		"INSERT INTO cookies VALUES ('.example.com', 'cf_clearance');" +
		"INSERT INTO cookies VALUES ('www.example.com', 'session');" +
		"INSERT INTO cookies VALUES ('.other.com', 'cf_clearance');" +
		"INSERT INTO cookies VALUES ('.com', 'evil');"
	if out, err := exec.Command("sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 fixture: %%v\n%%s", err, out)
	}

	count, requiredCount, missing := inspectCookiesForDomain(db, "www.example.com", []string{"cf_clearance", "session"})
	if count != 2 {
		t.Fatalf("count = %%d, want 2 (parent + host; not unrelated or TLD)", count)
	}
	if requiredCount != 2 {
		t.Fatalf("requiredCount = %%d, want 2", requiredCount)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %%v, want none", missing)
	}
}

func TestInspectCookiesForDomainRejectsUnrelatedDomain(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for Chrome cookie discovery")
	}
	db := filepath.Join(t.TempDir(), "Cookies")
	sql := "CREATE TABLE cookies (host_key TEXT, name TEXT);" +
		"INSERT INTO cookies VALUES ('.other.com', 'cf_clearance');" +
		"INSERT INTO cookies VALUES ('.other.com', 'session');"
	if out, err := exec.Command("sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 fixture: %%v\n%%s", err, out)
	}

	count, requiredCount, missing := inspectCookiesForDomain(db, "www.example.com", []string{"cf_clearance", "session"})
	if count != 0 || requiredCount != 0 {
		t.Fatalf("count=%%d requiredCount=%%d, want 0/0", count, requiredCount)
	}
	if strings.Join(missing, ",") != "cf_clearance,session" {
		t.Fatalf("missing = %%v, want cf_clearance,session", missing)
	}
}

func TestNewProbeHTTPClientIsUsable(t *testing.T) {
	httpClient := client.NewProbeHTTPClient(5*time.Second, false)
	if httpClient == nil {
		t.Fatal("NewProbeHTTPClient returned nil")
	}
	if httpClient.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %%v, want 5s", httpClient.Timeout)
	}
}
`, modulePath+"/internal/client")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "browser_clearance_auth_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestCookieDomainMatchesParentHost|TestInspectCookiesForDomain|TestNewProbeHTTPClientIsUsable", "-count=1")
}
