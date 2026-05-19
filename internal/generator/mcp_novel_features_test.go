package generator

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPRegistersCobraTreeMirror verifies that novel features no longer
// drive a static MCP list. RegisterTools wires the runtime Cobra-tree mirror,
// while RegisterNovelFeatureTools remains as a compatibility no-op for old
// generated mains.
func TestMCPRegistersCobraTreeMirror(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("noveltest")
	outputDir := filepath.Join(t.TempDir(), "noveltest-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Snapshot fanout",
			Command:     "snapshot",
			Description: "Look up a company across multiple sources in one call.",
			Rationale:   "Saves agent round-trips.",
		},
		{
			Name:        "Form D related-person graph",
			Command:     "funding --who",
			Description: "Show every Form D filing that names a given person.",
			Rationale:   "Reveals serial founders.",
		},
		{
			Name:        "Funding cadence",
			Command:     "funding-trend",
			Description: "Time series of Form D filings for a company.",
			Rationale:   "Spots silent-quarter signals.",
		},
	}
	require.NoError(t, gen.Generate())

	tools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(tools)

	// Compatibility function remains, but the static registration body is gone.
	assert.Contains(t, content, "func RegisterNovelFeatureTools(s *server.MCPServer) {")
	assert.Contains(t, content, "_ = s")
	assert.NotContains(t, content, `shellOutToCLI("snapshot")`)
	assert.Contains(t, content, "cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath, registerProfiledTool)")

	cobratreeCLIPath, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "cli_path.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cobratreeCLIPath), `const cliName = "noveltest-pp-cli"`)
	assert.Contains(t, string(cobratreeCLIPath), `os.Getenv("NOVELTEST_CLI_PATH")`)

	cobratreeShellout, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "shellout.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cobratreeShellout), "func shellOutToCLI(")
	assert.Contains(t, string(cobratreeShellout), "func splitShellArgs(s string)")

	root, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(root), "func RootCmd() *cobra.Command")

	// main.go calls only RegisterTools; RegisterTools owns endpoint tools and
	// the runtime command mirror.
	main, err := os.ReadFile(filepath.Join(outputDir, "cmd", "noveltest-pp-mcp", "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(main), "mcptools.RegisterTools(s)")
	assert.NotContains(t, string(main), "mcptools.RegisterNovelFeatureTools(s)")
}

// TestMCPNovelFeatureToolNameSanitization pins the snake-case tool-name
// derivation across the corner cases the catalog actually uses.
func TestMCPNovelFeatureToolNameSanitization(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"snapshot":         "snapshot",
		"funding-trend":    "funding_trend",
		"funding --who":    "funding_who",
		"compare":          "compare",
		"signal":           "signal",
		"FUNDING --WHO":    "funding_who", // uppercase folds
		"  weird   spaces": "weird_spaces",
		"trailing-":        "trailing", // trailing underscore stripped
		"":                 "",         // empty stays empty
	}

	apiSpec := minimalSpec("sanitize")
	outputDir := filepath.Join(t.TempDir(), "sanitize-pp-cli")
	gen := New(apiSpec, outputDir)
	for command := range cases {
		if command == "" {
			continue
		}
		gen.NovelFeatures = append(gen.NovelFeatures, NovelFeature{
			Name:        "Test " + command,
			Command:     command,
			Description: "test feature",
		})
	}
	require.NoError(t, gen.Generate())

	names, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "names.go"))
	require.NoError(t, err)
	content := string(names)

	assert.Contains(t, content, "func toolNameForPath(parts []string) string")

	var testSrc strings.Builder
	testSrc.WriteString(`package cobratree

import (
	"strings"
	"testing"
)

func TestToolNameForPathCases(t *testing.T) {
	cases := map[string]string{
`)
	for command, want := range cases {
		testSrc.WriteString("\t\t")
		testSrc.WriteString(strconv.Quote(command))
		testSrc.WriteString(": ")
		testSrc.WriteString(strconv.Quote(want))
		testSrc.WriteString(",\n")
	}
	testSrc.WriteString(`	}
	for command, want := range cases {
		if got := toolNameForPath(strings.Fields(command)); got != want {
			t.Fatalf("toolNameForPath(%q) = %q, want %q", command, got, want)
		}
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "names_extra_test.go"), []byte(testSrc.String()), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree")
}
