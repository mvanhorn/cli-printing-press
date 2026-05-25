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

func TestValidateNarrativeCmd_StrictFullExamplesAllowsSideEffectUnsupported(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{
		"quickstart":[{"command":"stub auth login --client-id abc --client-secret def"}],
		"recipes":[{"command":"stub tickets age-out --status stale --apply"}]
	}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := buildShipcheckStub(t)

	cmd := newValidateNarrativeCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs([]string{"--strict", "--full-examples", "--research", research, "--binary", binary})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("side-effectful unsupported examples should not fail strict mode, got %v", err)
	}
	got := stdout.String() + stderr.String()
	if count := strings.Count(got, "UNSUPPORTED"); count != 2 {
		t.Fatalf("expected 2 unsupported warnings, got %d in %q", count, got)
	}
	if !strings.Contains(got, "2 unsupported") {
		t.Fatalf("output = %q, want summary with 2 unsupported warnings", got)
	}
}

func TestValidateNarrativeCmd_JSONReportsStrictFailureForUnsupported(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{
		"quickstart":[{"command":"stub auth login --client-id abc --client-secret def"}],
		"recipes":[{"command":"stub widgets list"}]
	}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := buildShipcheckStub(t)

	cmd := newValidateNarrativeCmd()
	var stdout bytes.Buffer
	cmd.SetArgs([]string{"--json", "--full-examples", "--research", research, "--binary", binary})
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected JSON report to succeed without --strict, got %v", err)
	}
	got := stdout.String()
	if count := strings.Count(got, `"status":"unsupported"`); count != 2 {
		t.Fatalf("expected 2 unsupported JSON results, got %d in %s", count, got)
	}
	if count := strings.Count(got, `"strict_failure":true`); count != 1 {
		t.Fatalf("expected only the dry-run-unavailable unsupported result to be strict_failure, got %d in %s", count, got)
	}
}

func TestValidateNarrativeCmd_MissingResearchIsNotApplicable(t *testing.T) {
	t.Parallel()

	missingResearch := filepath.Join(t.TempDir(), "missing", "research.json")
	cmd := newValidateNarrativeCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs([]string{
		"--strict",
		"--research", missingResearch,
		"--binary", "/nonexistent-but-not-invoked",
	})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("missing research.json should be treated as not applicable, got %v", err)
	}
	if got := stdout.String() + stderr.String(); !strings.Contains(got, "N/A: research.json not found") {
		t.Fatalf("stderr = %q, want N/A skip message", got)
	}
}

func TestValidateNarrativeCmd_FrameworkOnlyDoesNotRequireBinary(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{"quickstart":[{"command":"demo-pp-cli sync --resources users --since 7d"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newValidateNarrativeCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs([]string{"--strict", "--framework-only", "--research", research})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --framework-only without --binary to pass documented flags, got %v", err)
	}
	if got := stdout.String() + stderr.String(); !strings.Contains(got, "framework-command narrative examples passed static checks") {
		t.Fatalf("output = %q, want framework-only OK output", got)
	}
}

func TestValidateNarrativeCmd_FrameworkOnlyFailsInventedFlags(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{"quickstart":[{"command":"demo-pp-cli sync --entities users"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newValidateNarrativeCmd()
	cmd.SetArgs([]string{"--strict", "--framework-only", "--research", research})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --framework-only --strict to fail on invented framework flags")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("Code = %d, want ExitInputError (%d)", exitErr.Code, ExitInputError)
	}
}

func TestValidateNarrativeCmd_FrameworkOnlyReportsNoFrameworkCommands(t *testing.T) {
	t.Parallel()

	research := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(research, []byte(`{"narrative":{"quickstart":[{"command":"demo-pp-cli customers list --limit 5"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newValidateNarrativeCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs([]string{"--strict", "--framework-only", "--research", research})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected endpoint-only narrative to skip framework-only validation, got %v", err)
	}
	if got := stdout.String() + stderr.String(); !strings.Contains(got, "no framework-command narrative examples found") {
		t.Fatalf("output = %q, want no-framework-command skip output", got)
	}
}
