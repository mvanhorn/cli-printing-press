package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// emittedTestHelperFiles are the generated test files that sandbox a
// test's user directories. Every one of them must isolate through
// testenv, whose containment check is what turns a missed environment
// variable into a failed test instead of a write into the operator's
// real config.
var emittedTestHelperFiles = []string{
	filepath.Join("internal", "cliutil", "paths_test.go"),
	filepath.Join("internal", "cliutil", "credentials_test.go"),
	filepath.Join("internal", "cliutil", "credentials_perms_test.go"),
	filepath.Join("internal", "mcp", "tools_test.go"),
	filepath.Join("internal", "cli", "teach_test.go"),
	filepath.Join("internal", "learn", "teach_log_test.go"),
	filepath.Join("internal", "learn", "journal_test.go"),
}

// TestGeneratedTestsIsolateThroughTestenv fails if an emitted test
// file goes back to redirecting HOME by hand. HOME alone is a silent
// no-op on Windows, where os.UserHomeDir reads USERPROFILE.
func TestGeneratedTestsIsolateThroughTestenv(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	for _, rel := range emittedTestHelperFiles {
		data, err := os.ReadFile(filepath.Join(outputDir, rel))
		require.NoError(t, err, rel)
		source := string(data)

		require.NotContains(t, source, `t.Setenv("HOME"`,
			"%s redirects HOME by hand; isolate through testenv.Isolate so the redirect is verified", rel)
		require.Contains(t, source, "testenv.Isolate(t",
			"%s must sandbox its user directories through testenv.Isolate", rel)
	}
}

// TestGeneratedTestEnvDetectsSandboxEscape is the load-bearing proof:
// it drops a test into the generated module whose resolver reports a
// path outside the sandbox, and requires the guard to fail the run.
// An isolation helper that only sets environment variables would let
// this pass, which is exactly the failure mode that let generated
// tests overwrite a real credentials file.
func TestGeneratedTestEnvDetectsSandboxEscape(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated-module test runs happen in the full generated-test CI lane")
	}

	outputDir := generatePetstore(t)

	inlineTest := `package testenv_test

import (
	"os"
	"testing"

	"petstore-pp-cli/internal/cliutil/testenv"
)

// The resolver reports the parent of the sandbox, standing in for a
// lookup that still reads a real environment variable.
func TestSandboxEscapeIsCaught(t *testing.T) {
	testenv.Isolate(t, func() (string, error) {
		return os.TempDir(), nil
	})
}
`
	testPath := filepath.Join(outputDir, "internal", "cliutil", "testenv", "escape_probe_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	output, err := runGoCommandOutput(t, outputDir, "test", "./internal/cliutil/testenv", "-run", "TestSandboxEscapeIsCaught")
	require.Error(t, err, "escaping the sandbox must fail the test run, got:\n%s", output)
	require.True(t, strings.Contains(output, "test sandbox escaped"),
		"guard should name the escape; got:\n%s", output)
}
