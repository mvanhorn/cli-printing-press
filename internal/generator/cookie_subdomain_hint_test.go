package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hostOnlySubdomainHintRuntime = `package cli

import (
	"strings"
	"testing"
)

func TestHostOnlySubdomainRecaptureHint(t *testing.T) {
	unnamed := hostOnlySubdomainRecapture("concursolutions.com", "")
	if !strings.Contains(unnamed, "add --include-subdomains") {
		t.Fatalf("unnamed = %q", unnamed)
	}
	if strings.Contains(unnamed, "press-auth login") {
		t.Fatalf("unnamed hint replaced the default capture command: %q", unnamed)
	}

	named := hostOnlySubdomainRecapture("concursolutions.com", "_csrf")
	wantCmd := "press-auth login concursolutions.com --login-url <login-url> --include-subdomains"
	if !strings.Contains(named, wantCmd) || !strings.Contains(named, "\"_csrf\"") {
		t.Fatalf("named = %q", named)
	}
	if named == unnamed {
		t.Fatal("named and unnamed hints collapsed")
	}
}
`

func TestGeneratedHostOnlySubdomainHint(t *testing.T) {
	t.Parallel()

	t.Run("cookie allowlist", func(t *testing.T) {
		t.Parallel()
		outputDir := generateCookieHintCLI(t, spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: ".example.com",
			Cookies:      []string{"session", "_csrf"},
			EnvVars:      []string{"HINTCOOKIE_COOKIES"},
		}, "hintcookie")
		auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
		assertHostOnlySubdomainHint(t, auth)
		assert.Contains(t, auth, "hostOnlySubdomainRecapture(domain, name)")
		requireGeneratedCompiles(t, outputDir)
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "host_only_subdomain_hint_test.go"), []byte(hostOnlySubdomainHintRuntime), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestHostOnlySubdomainRecaptureHint")
	})

	t.Run("composed", func(t *testing.T) {
		t.Parallel()
		outputDir := generateCookieHintCLI(t, spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "Bearer {session}",
			CookieDomain: "example.com",
			Cookies:      []string{"session", "_csrf"},
		}, "hintcomposed")
		auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
		assertHostOnlySubdomainHint(t, auth)
		assert.Contains(t, auth, `cookie %q not found for %s; %s`)
		requireGeneratedCompiles(t, outputDir)
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "host_only_subdomain_hint_test.go"), []byte(hostOnlySubdomainHintRuntime), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestHostOnlySubdomainRecaptureHint")
	})
}

func generateCookieHintCLI(t *testing.T, auth spec.AuthConfig, name string) string {
	t.Helper()
	apiSpec := &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://example.com",
		Auth:    auth,
		Config:  spec.ConfigSpec{Format: "toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	return outputDir
}

func assertHostOnlySubdomainHint(t *testing.T, auth string) {
	t.Helper()
	assert.Contains(t, auth, "func hostOnlySubdomainRecapture(domain, name string) string")
	assert.Contains(t, auth, `press-auth login %s --login-url <login-url>\n\n`)
	assert.Contains(t, auth, `hostOnlySubdomainRecapture(domain, "")`)
	assert.NotContains(t, auth, `"--include-subdomains"`)
}
