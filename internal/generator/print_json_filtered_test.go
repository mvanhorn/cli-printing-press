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
