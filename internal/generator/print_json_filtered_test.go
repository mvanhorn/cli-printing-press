package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPrintJSONFiltered_EmittedIntoHelpers verifies the generator emits the
// printJSONFiltered helper into every CLI's internal/cli/helpers.go. The
// helper composes printOutputWithFlags so novel commands honor --select,
// --compact, --csv, and --quiet identically to endpoint-mirror commands;
// asserting it delegates rather than re-implements keeps the two paths
// aligned when printOutputWithFlags evolves.
func TestPrintJSONFiltered_EmittedIntoHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("filtered-print")
	outputDir := filepath.Join(t.TempDir(), "filtered-print-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersPath := filepath.Join(outputDir, "internal", "cli", "helpers.go")
	content, err := os.ReadFile(helpersPath)
	require.NoError(t, err)
	src := string(content)

	require.Contains(t, src, "func printJSONFiltered(",
		"helpers.go must export printJSONFiltered for novel commands")
	require.Contains(t, src, "printOutputWithFlags(w, json.RawMessage(raw), flags)",
		"printJSONFiltered must delegate to printOutputWithFlags so flag handling stays unified")
}

// TestRootFlagsPrintJSONHonorsOutputFlags guards the receiver-style helper
// agents naturally reach for when hand-authoring novel commands. It must route
// through the same filtered output pipeline as generated endpoint commands so
// --select, --compact, --csv, and --quiet cannot be silently bypassed.
func TestRootFlagsPrintJSONHonorsOutputFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("receiver-print")
	outputDir := filepath.Join(t.TempDir(), "receiver-print-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "print_json_receiver_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootFlagsPrintJSONHonorsSelect(t *testing.T) {
	flags := &rootFlags{asJSON: true, selectFields: "id"}
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := flags.printJSON(cmd, []map[string]any{{"id": "one", "name": "hidden"}}); err != nil {
		t.Fatalf("printJSON returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\"id\"") {
		t.Fatalf("expected selected field in output, got %s", got)
	}
	if strings.Contains(got, "hidden") || strings.Contains(got, "\"name\"") {
		t.Fatalf("printJSON bypassed --select filtering, got %s", got)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestRootFlagsPrintJSONHonorsSelect", "-count=1")
}
