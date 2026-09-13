package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cli"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDogfoodBinaryFailurePrintsDiagnosticOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(`package cli
func unused() { cmd.Flags().StringVar(&flags.first, "first", "", ""); cmd.Flags().StringVar(&flags.second, "second", "", ""); cmd.Flags().StringVar(&flags.third, "third", "", "") }
`), 0o644))

	cmd := exec.Command(buildPrintingPressBinary(t), "dogfood", "--dir", dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, cli.ExitGenerationError, exitErr.ExitCode())
	assert.Contains(t, stdout.String(), "Verdict: FAIL")
	assert.Equal(t, "Error: dogfood failed\n", stderr.String())
}

func TestWorkflowVerifyBinaryFailurePrintsDiagnosticOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow_verify.yaml"), []byte("workflows: []\n"), 0o644))

	cmd := exec.Command(buildPrintingPressBinary(t), "workflow-verify", "--dir", dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, cli.ExitGenerationError, exitErr.ExitCode())
	assert.Contains(t, stdout.String(), string(pipeline.WorkflowVerdictFail))
	assert.Equal(t, "Error: workflow verification failed\n", stderr.String())
}
