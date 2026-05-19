package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupVerifyArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cache", "go-build"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "library", "sample-cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli-validation"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli-dogfood"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("finder"), 0o644))

	require.NoError(t, cleanupVerifyArtifacts(dir, true))

	assert.NoFileExists(t, filepath.Join(dir, "sample-cli"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-validation"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-dogfood"))
	assert.NoFileExists(t, filepath.Join(dir, ".DS_Store"))
	assert.NoDirExists(t, filepath.Join(dir, ".cache"))
	assert.NoDirExists(t, filepath.Join(dir, "cmd", "library"))
}

func TestCleanupVerifyArtifacts_NoOpWhenDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli"), []byte("bin"), 0o755))

	require.NoError(t, cleanupVerifyArtifacts(dir, false))

	assert.FileExists(t, filepath.Join(dir, "sample-cli"))
}

func TestVerifyCmdJSONFailReturnsExitErrorAfterWritingReport(t *testing.T) {
	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Failed:   1,
				PassRate: 0,
				Verdict:  "FAIL",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", t.TempDir(), "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitGenerationError, exitErr.Code)
	assert.True(t, exitErr.Silent)

	var payload struct {
		Verify pipeline.VerifyReport `json:"verify"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "FAIL", payload.Verify.Verdict)
}
