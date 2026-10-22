package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedOutputSafetyContracts(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("output-safety")
	outputDir := filepath.Join(t.TempDir(), "output-safety-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliTest, err := os.ReadFile("testdata/output_safety/cli_test.go")
	require.NoError(t, err)
	clientTest, err := os.ReadFile("testdata/output_safety/client_test.go")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "output_safety_test.go"), cliTest, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "credential_echo_test.go"), clientTest, 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli/", "./internal/client/", "-count=1",
		"-run", "TestOutputSafety|TestCredentialEcho")
}
