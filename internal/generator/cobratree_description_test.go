package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateCobratreePrefersShortMCPDescription pins the emitted
// cobratree catalog helper: MCP tool descriptions use Short (or the
// first Long paragraph), not the full operator --help manual.
func TestGenerateCobratreePrefersShortMCPDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cobra-desc")
	outputDir := filepath.Join(t.TempDir(), "cobra-desc-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	walker := readEmitted(t, outputDir, "internal", "mcp", "cobratree", "walker.go")
	require.Contains(t, walker, "func cobratreeToolDescription(",
		"walker must emit the shared Short-preferring catalog helper")
	require.Contains(t, walker, `if desc := strings.TrimSpace(short); desc != ""`,
		"catalog text must prefer Short over Long")
	require.Contains(t, walker, "func firstHelpParagraph(",
		"Long fallback must take the lead paragraph, not the full manual")
	require.Contains(t, walker, `strings.Cut(s, "\n\n")`,
		"first paragraph split must use strings.Cut")
	require.NotContains(t, walker, "if cmd.Long != \"\" {\n\t\treturn cmd.Long",
		"walker must not dump Long --help into the MCP catalog")

	requireGeneratedCompiles(t, outputDir)
}
