package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedTestsPassPublishSecretScan is the publish-scan contract for
// generated tests: a fresh cookie-auth print whose declared cookie names
// collide with generated fixtures (historically `token=` / `key=`) must
// not trip the mandatory secret scan. Injecting a live-looking vendor key
// into any generated file, including a test, still fails.
func TestGeneratedTestsPassPublishSecretScan(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("secretscancookie")
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"token", "key", "session_id"},
		EnvVars:      []string{"SECRETSCANCOOKIE_COOKIES"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliutilTestSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "cliutil_test.go")
	require.Contains(t, cliutilTestSrc, "token=your-token-here",
		"emitted cliutil tests must still cover token=<value> credential redaction")
	require.Contains(t, cliutilTestSrc, "key=your-key-here",
		"emitted cliutil tests must still cover key=<value> credential redaction")

	shelloutSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout_test.go")
	require.Contains(t, shelloutSrc, `"token=your-stolen-token-here"`,
		"emitted cobratree tests must still cover '='-containing blocked MCP keys")

	findings, err := artifacts.FindPackageSecrets(outputDir, apiSpec.Auth.Cookies)
	require.NoError(t, err)
	require.Empty(t, findings, "generated package must not trip the publish secret scan:\n%s",
		artifacts.FormatVendorPrefixSecretFindings(findings))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cliutil/", "./internal/mcp/cobratree/")

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cliutil", "planted_secret_test.go"),
		[]byte("package cliutil\n\nconst planted = \""+strings.Join([]string{"sk", "_live_", "1234567890abcdefghijklmnop"}, "")+"\"\n"),
		0o644,
	))
	planted, err := artifacts.FindPackageSecrets(outputDir, apiSpec.Auth.Cookies)
	require.NoError(t, err)
	require.NotEmpty(t, planted, "planting a live-looking sk_live_ value in a generated test must still fail the scan")
	kinds := make([]string, 0, len(planted))
	for _, finding := range planted {
		kinds = append(kinds, finding.Kind)
	}
	require.Contains(t, kinds, "stripe-secret-key")
}
