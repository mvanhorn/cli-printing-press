package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateDefaultsLearnLoopOn pins the flip: a spec with no learn block
// now prints with the loop (and therefore the store) on.
func TestGenerateDefaultsLearnLoopOn(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-flip-default")
	require.False(t, apiSpec.Learn.Enabled, "fixture must start learn-less")
	outputDir := filepath.Join(t.TempDir(), "learn-flip-default-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.True(t, apiSpec.Learn.Enabled, "Generate must apply the learn default")
	require.FileExists(t, filepath.Join(outputDir, "internal", "learn", "doc.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "learn", "journal.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "learnings_stats.go"))
}

// TestGenerateLearnDisabledOptsOut pins AE7: learn.disabled prints with no
// learn surface at all and no learn-driven store forcing.
func TestGenerateLearnDisabledOptsOut(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-flip-disabled")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-flip-disabled-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.False(t, apiSpec.Learn.Enabled, "disabled spec must stay off")
	require.NoDirExists(t, filepath.Join(outputDir, "internal", "learn"))
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	require.NotContains(t, string(rootGo), "newTeachCmd", "opt-out must not register learn commands")
}

// TestGenerateLearnEnabledFalseOptsOut preserves the pre-default-on meaning
// of specs that already said learn.enabled: false.
func TestGenerateLearnEnabledFalseOptsOut(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-flip-enabled-false")
	apiSpec.Learn.EnabledSet = true
	outputDir := filepath.Join(t.TempDir(), "learn-flip-enabled-false-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.False(t, apiSpec.Learn.Enabled)
	require.NoDirExists(t, filepath.Join(outputDir, "internal", "learn"))
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	require.NotContains(t, string(rootGo), "newTeachCmd")
}

// TestGenerateMCPSurfaceNeverAppliesLearnDefault pins the mcp-sync back-door
// shut: regenerating only the MCP surface (what mcp-sync does to published
// CLIs) must leave a learn-less spec learn-less and emit no learn-gated
// MCP content into the tree.
func TestGenerateMCPSurfaceNeverAppliesLearnDefault(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-flip-mcpsync")
	outputDir := filepath.Join(t.TempDir(), "learn-flip-mcpsync-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.GenerateMCPSurface())

	require.False(t, apiSpec.Learn.Enabled,
		"GenerateMCPSurface must never apply the learn default (mcp-sync runs it on published CLIs)")
	mcpTools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	require.NotContains(t, string(mcpTools), "RecallFirstProtocol",
		"mcp-sync over a learn-less spec must not advertise the learn protocol")
	require.NoDirExists(t, filepath.Join(outputDir, "internal", "learn"))
}
