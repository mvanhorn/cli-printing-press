package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cli"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// TestPrintCmd_AlreadyExists_ExitCode verifies that the "already exists"
// error from pipeline.Init still contains the substring matched by the
// print command's exit-code classification (root.go). If someone edits the
// pipeline error message and this test breaks, the exit-code switch must
// be updated to match.
func TestPrintCmd_AlreadyExists_ExitCode(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("PRINTING_PRESS_HOME", filepath.Join(tmp, "printing-press"))
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Create a fake state file so StateExists returns true.
	apiName := "exitcode-test"
	pipeDir := pipeline.PipelineDir(apiName)
	if err := os.MkdirAll(pipeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pipeDir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, initErr := pipeline.Init(apiName, pipeline.Options{})
	if initErr == nil {
		t.Fatal("expected error from pipeline.Init when state already exists")
	}

	if !strings.Contains(initErr.Error(), "already exists") {
		t.Errorf("pipeline.Init collision error no longer contains %q; exit-code classification in root.go will misroute this error.\ngot: %s", "already exists", initErr.Error())
	}

	// Simulate what the print command does: classify and verify the code.
	msg := initErr.Error()
	var exitErr *cli.ExitError
	switch {
	case strings.Contains(msg, "already exists"):
		exitErr = &cli.ExitError{Code: cli.ExitInputError, Err: initErr}
	case strings.Contains(msg, "discovering spec"):
		exitErr = &cli.ExitError{Code: cli.ExitSpecError, Err: initErr}
	default:
		exitErr = &cli.ExitError{Code: cli.ExitGenerationError, Err: initErr}
	}

	if exitErr.Code != cli.ExitInputError {
		t.Errorf("expected ExitInputError (%d) for collision, got %d", cli.ExitInputError, exitErr.Code)
	}

	// Verify errors.As works through the wrapper.
	var extracted *cli.ExitError
	if !errors.As(exitErr, &extracted) {
		t.Fatal("errors.As should extract ExitError")
	}
	if extracted.Code != cli.ExitInputError {
		t.Errorf("extracted code = %d, want %d", extracted.Code, cli.ExitInputError)
	}
}

// TestPipelineInitDiscoverSpec_ErrorSubstring verifies that a spec discovery
// failure from pipeline.Init contains "discovering spec", which the print
// command's exit-code classification depends on. This test uses a bogus API
// name with no catalog entry and no network to trigger the failure.
func TestPipelineInitDiscoverSpec_ErrorSubstring(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("PRINTING_PRESS_HOME", filepath.Join(tmp, "printing-press"))
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Use a name that won't match any known spec and will fail discovery.
	_, initErr := pipeline.Init("zzz-nonexistent-api-exitcode-test", pipeline.Options{})
	if initErr == nil {
		t.Skip("pipeline.Init succeeded unexpectedly (network may have resolved the name)")
	}

	if !strings.Contains(initErr.Error(), "discovering spec") {
		t.Errorf("pipeline.Init spec-discovery error no longer contains %q; exit-code classification in root.go will misroute this error.\ngot: %s", "discovering spec", initErr.Error())
	}
}
