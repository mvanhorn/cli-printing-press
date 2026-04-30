package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckPrintJSONFiltered_FlagsAntipattern_FlagsAndLine writes a
// realistic command file containing the flags.printJSON(cmd, v)
// antipattern and confirms the check finds it with file + line +
// snippet recorded.
func TestCheckPrintJSONFiltered_FlagsAntipattern(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	const novelGo = `package cli

import "github.com/spf13/cobra"

func newWidgetsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "widgets",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := computeRows()
			return flags.printJSON(cmd, rows)
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(cliPkg, "widgets.go"), []byte(novelGo), 0o644); err != nil {
		t.Fatalf("write widgets.go: %v", err)
	}

	result := checkPrintJSONFiltered(cliDir)

	if result.Skipped {
		t.Fatalf("expected check to run, was skipped")
	}
	if result.Checked != 1 {
		t.Errorf("Checked = %d, want 1", result.Checked)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("Findings = %d, want 1: %+v", len(result.Findings), result.Findings)
	}
	f := result.Findings[0]
	if f.File != "internal/cli/widgets.go" {
		t.Errorf("File = %q, want internal/cli/widgets.go", f.File)
	}
	if f.Line != 10 {
		t.Errorf("Line = %d, want 10", f.Line)
	}
	if f.Snippet != "return flags.printJSON(cmd, rows)" {
		t.Errorf("Snippet = %q, want clean trimmed source line", f.Snippet)
	}
}

// TestCheckPrintJSONFiltered_CleanCLI confirms the check returns no
// findings when every novel command uses the printJSONFiltered helper.
func TestCheckPrintJSONFiltered_CleanCLI(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	const cleanGo = `package cli

import "github.com/spf13/cobra"

func newWidgetsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "widgets",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := computeRows()
			return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(cliPkg, "widgets.go"), []byte(cleanGo), 0o644); err != nil {
		t.Fatalf("write widgets.go: %v", err)
	}

	result := checkPrintJSONFiltered(cliDir)

	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got: %+v", result.Findings)
	}
	if result.Checked != 1 {
		t.Errorf("Checked = %d, want 1", result.Checked)
	}
}

// TestCheckPrintJSONFiltered_SkipsTestFilesAndHelpers verifies the
// check ignores files that legitimately reference flags.printJSON: the
// helpers.go file (where the method is defined) and any _test.go file
// (test fixtures may demonstrate the antipattern in regression tests).
func TestCheckPrintJSONFiltered_SkipsTestFilesAndHelpers(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// helpers.go contains the method definition, which the regex would
	// not match anyway (no `cmd,` after `(`), but the explicit skip is
	// belt-and-suspenders for future helper additions.
	const helpersGo = `package cli

func (f *rootFlags) printJSON(w *cobra.Command, v any) error {
	return nil
}
`
	if err := os.WriteFile(filepath.Join(cliPkg, "helpers.go"), []byte(helpersGo), 0o644); err != nil {
		t.Fatalf("write helpers.go: %v", err)
	}

	// A _test.go file containing the antipattern — should be skipped.
	const testGo = `package cli

import "testing"

func TestRegression(t *testing.T) {
	_ = flags.printJSON(cmd, struct{}{})
}
`
	if err := os.WriteFile(filepath.Join(cliPkg, "regression_test.go"), []byte(testGo), 0o644); err != nil {
		t.Fatalf("write regression_test.go: %v", err)
	}

	result := checkPrintJSONFiltered(cliDir)

	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings (helpers.go and *_test.go skipped), got: %+v", result.Findings)
	}
}

// TestCheckPrintJSONFiltered_SkipsWhenNoCliPkg returns Skipped when
// the cliDir doesn't have an internal/cli directory at all (e.g.,
// dogfood was pointed at a non-CLI tree).
func TestCheckPrintJSONFiltered_SkipsWhenNoCliPkg(t *testing.T) {
	t.Parallel()

	result := checkPrintJSONFiltered(t.TempDir())

	if !result.Skipped {
		t.Errorf("expected Skipped=true when internal/cli is missing, got: %+v", result)
	}
}

// TestCheckPrintJSONFiltered_MultipleFindingsAcrossFiles confirms the
// check accumulates findings across files and across multiple call
// sites within a single file.
func TestCheckPrintJSONFiltered_MultipleFindingsAcrossFiles(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Two files, three findings total: jobs.go has two, feedback.go
	// has one. Lines are deliberately different to verify line numbers
	// are reported correctly.
	const jobsGo = `package cli

func A(flags *rootFlags, cmd *cobra.Command) error {
	return flags.printJSON(cmd, "first")
}

func B(flags *rootFlags, cmd *cobra.Command) error {
	return flags.printJSON(cmd, "second")
}
`
	const feedbackGo = `package cli

func C(flags *rootFlags, cmd *cobra.Command) error {
	rows := []int{1, 2, 3}
	return flags.printJSON(cmd, rows)
}
`
	if err := os.WriteFile(filepath.Join(cliPkg, "jobs.go"), []byte(jobsGo), 0o644); err != nil {
		t.Fatalf("write jobs.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cliPkg, "feedback.go"), []byte(feedbackGo), 0o644); err != nil {
		t.Fatalf("write feedback.go: %v", err)
	}

	result := checkPrintJSONFiltered(cliDir)

	if result.Checked != 2 {
		t.Errorf("Checked = %d, want 2", result.Checked)
	}
	if len(result.Findings) != 3 {
		t.Fatalf("Findings = %d, want 3: %+v", len(result.Findings), result.Findings)
	}
}
