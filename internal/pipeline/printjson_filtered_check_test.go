package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckPrintJSONFiltered_FlagsAntipattern asserts that the check
// reports file + line + snippet for an offending call site. Without
// these three fields a reviewer can't act on the finding.
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

// TestCheckPrintJSONFiltered_SkipsTestFiles confirms _test.go files
// are excluded — regression-test fixtures may demonstrate the
// antipattern intentionally.
func TestCheckPrintJSONFiltered_SkipsTestFiles(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

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
		t.Fatalf("expected no findings (*_test.go skipped), got: %+v", result.Findings)
	}
}

func TestCheckPrintJSONFiltered_SkipsWhenNoCliPkg(t *testing.T) {
	t.Parallel()

	result := checkPrintJSONFiltered(t.TempDir())

	if !result.Skipped {
		t.Errorf("expected Skipped=true when internal/cli is missing, got: %+v", result)
	}
}

// TestCheckPrintJSONFiltered_MultipleFindingsAcrossFiles confirms the
// check accumulates findings across files and across multiple call
// sites within a single file. Findings are also sorted by file path
// (via listGoFiles), so output is deterministic across OSes.
func TestCheckPrintJSONFiltered_MultipleFindingsAcrossFiles(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	if err := os.MkdirAll(cliPkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

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
	// listGoFiles sorts alphabetically: feedback.go before jobs.go.
	if !strings.HasSuffix(result.Findings[0].File, "feedback.go") {
		t.Errorf("first finding should be from feedback.go, got %q", result.Findings[0].File)
	}
}

// TestTruncateSnippet covers the rune-safe truncation guard against
// dogfood.json bloat. UTF-8 splitting at byte boundaries would corrupt
// multi-byte characters; rune slicing keeps the output well-formed.
func TestTruncateSnippet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"under cap", "short line", 120, "short line"},
		{"exact cap", "abcde", 5, "abcde"},
		{"over cap", "abcdefghij", 5, "abcde…"},
		{"multibyte preserved", "café日本語abcde", 6, "café日本…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateSnippet(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("truncateSnippet(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}
