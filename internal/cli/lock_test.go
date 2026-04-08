package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLockCLITest(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)
}

func runLockCmd(args ...string) (stdout string, exitCode int) {
	cmd := newLockCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)

	// Redirect stdout to capture JSON output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout

	var captured bytes.Buffer
	_, _ = captured.ReadFrom(r)

	code := 0
	if err != nil {
		code = 1
		if exitErr, ok := err.(*ExitError); ok {
			code = exitErr.Code
		}
	}
	return captured.String(), code
}

func TestLockAcquire_Success(t *testing.T) {
	setupLockCLITest(t)

	stdout, code := runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-1")
	assert.Equal(t, 0, code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["acquired"])
	assert.Equal(t, false, result["blocked"])
	assert.Equal(t, "test-pp-cli", result["cli"])
}

func TestLockAcquire_MissingCLI(t *testing.T) {
	setupLockCLITest(t)

	_, code := runLockCmd("acquire", "--scope", "scope-1")
	assert.Equal(t, ExitInputError, code)
}

func TestLockAcquire_MissingScope(t *testing.T) {
	setupLockCLITest(t)

	_, code := runLockCmd("acquire", "--cli", "test-pp-cli")
	assert.Equal(t, ExitInputError, code)
}

func TestLockAcquire_Blocked(t *testing.T) {
	setupLockCLITest(t)

	// Acquire first.
	_, code := runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-1")
	require.Equal(t, 0, code)

	// Try from different scope.
	stdout, code := runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-2")
	assert.Equal(t, ExitInputError, code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, false, result["acquired"])
	assert.Equal(t, true, result["blocked"])
}

func TestLockStatus_JSON(t *testing.T) {
	setupLockCLITest(t)

	_, _ = runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-1")

	stdout, code := runLockCmd("status", "--cli", "test-pp-cli", "--json")
	assert.Equal(t, 0, code)

	var result pipeline.LockStatusResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.True(t, result.Held)
	assert.False(t, result.Stale)
	assert.Equal(t, "acquire", result.Phase)
	assert.Equal(t, "scope-1", result.Scope)
}

func TestLockUpdate(t *testing.T) {
	setupLockCLITest(t)

	_, _ = runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-1")

	stdout, code := runLockCmd("update", "--cli", "test-pp-cli", "--phase", "build")
	assert.Equal(t, 0, code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["updated"])
	assert.Equal(t, "build", result["phase"])
}

func TestLockRelease(t *testing.T) {
	setupLockCLITest(t)

	_, _ = runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "scope-1")

	stdout, code := runLockCmd("release", "--cli", "test-pp-cli")
	assert.Equal(t, 0, code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["released"])

	// Verify lock is gone.
	_, err := os.Stat(pipeline.LockFilePath("test-pp-cli"))
	assert.True(t, os.IsNotExist(err))
}

func TestLockPromote_MissingDir(t *testing.T) {
	setupLockCLITest(t)

	_, code := runLockCmd("promote", "--cli", "test-pp-cli", "--dir", "/nonexistent/path")
	assert.NotEqual(t, 0, code)
}

func TestLockPromote_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Create working dir with content.
	runID := "20260331T120000Z-abcd1234"
	workDir := filepath.Join(tmp, ".runstate", "test-scope", "runs", runID, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test-pp-cli\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))

	// Create state file for the run.
	state := pipeline.NewStateWithRun("test", workDir, runID, "test-scope")
	require.NoError(t, state.Save())

	// Acquire lock.
	_, code := runLockCmd("acquire", "--cli", "test-pp-cli", "--scope", "test-scope")
	require.Equal(t, 0, code)

	// Promote.
	stdout, code := runLockCmd("promote", "--cli", "test-pp-cli", "--dir", workDir)
	assert.Equal(t, 0, code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["promoted"])

	// Verify library dir exists (slug-keyed).
	libDir := filepath.Join(pipeline.PublishedLibraryRoot(), "test")
	_, err := os.Stat(filepath.Join(libDir, "go.mod"))
	assert.NoError(t, err)
}

func TestLockHelpOutput(t *testing.T) {
	cmd := newLockCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "acquire")
	assert.Contains(t, output, "update")
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "release")
	assert.Contains(t, output, "promote")
}
