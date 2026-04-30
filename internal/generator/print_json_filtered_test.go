package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPrintJSONFiltered_EmittedIntoHelpers verifies the generator emits the
// printJSONFiltered helper into every CLI's internal/cli/helpers.go. Hand-written
// novel commands rely on this helper to honor --select and --compact on JSON
// output; without it, agents fall back to flags.printJSON which silently drops
// both filters. The audit in retro #423 confirmed the broken pattern across
// recipe-goat, dub, espn, yahoo-finance, and postman-explore — landing the
// helper in the generator template means the next regeneration of any CLI
// picks it up.
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
	require.Contains(t, src, "OutOrStdout() io.Writer",
		"the helper signature should accept the minimal cobra interface so it stays testable")
	require.Contains(t, src, "flags.selectFields",
		"the helper should branch on --select via the existing flag field")
	require.Contains(t, src, "filterFields(filtered, flags.selectFields)",
		"the helper should reuse the existing filterFields helper to honor --select")
	require.Contains(t, src, "compactFields(filtered)",
		"the helper should reuse the existing compactFields helper to honor --compact")
}
