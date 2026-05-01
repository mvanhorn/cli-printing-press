package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateNarrativeCmd_RequiresFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no flags", nil, "--research is required"},
		{"only research", []string{"--research", "/dev/null"}, "--binary is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := newValidateNarrativeCmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestValidateNarrativeCmd_StrictExitCode confirms --strict wraps a
// ResearchEmpty (or HasFailures) result in ExitError so callers can
// switch on shell exit codes consistently with verify-skill/shipcheck.
func TestValidateNarrativeCmd_StrictExitCode(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newValidateNarrativeCmd()
	cmd.SetArgs([]string{"--strict", "--research", research, "--binary", "/nonexistent-but-not-invoked"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --strict to fail on empty narrative, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("Code = %d, want ExitInputError (%d)", exitErr.Code, ExitInputError)
	}
}
