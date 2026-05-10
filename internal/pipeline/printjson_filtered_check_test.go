package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckPrintJSONFiltered_AllowsReceiverHelper guards the cleanup for #826:
// flags.printJSON now delegates to printJSONFiltered in generated CLIs, so the
// dogfood check must not keep warning on the receiver-style helper agents are
// expected to use.
func TestCheckPrintJSONFiltered_AllowsReceiverHelper(t *testing.T) {
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
	if len(result.Findings) != 0 {
		t.Fatalf("flags.printJSON is safe after #826; expected no findings, got: %+v", result.Findings)
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

func TestCheckPrintJSONFiltered_SkipsWhenNoCliPkg(t *testing.T) {
	t.Parallel()

	result := checkPrintJSONFiltered(t.TempDir())

	if !result.Skipped {
		t.Errorf("expected Skipped=true when internal/cli is missing, got: %+v", result)
	}
}
