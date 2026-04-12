package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootLongIncludesTopNovelFeatures asserts the generated root.go sets
// a Long description naming the top novel features plus --agent and doctor
// pointers, so agents running `<cli> --help` can pick the right command
// without a second discovery round.
func TestRootLongIncludesTopNovelFeatures(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("helped")
	outputDir := filepath.Join(t.TempDir(), "helped-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline: "Every feature plus a local store",
	}
	gen.NovelFeatures = []NovelFeature{
		{Command: "portfolio perf", Description: "Compute unrealized P&L across synced lots"},
		{Command: "digest --watchlist tech", Description: "Biggest movers across a watchlist"},
		{Command: "auth login-chrome", Description: "Import a Chrome session when rate-limited"},
		{Command: "compare", Description: "Side-by-side quote comparison"}, // should be truncated
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	assert.True(t, strings.Contains(content, "Highlights (not in the official API docs):"),
		"root Long should introduce the highlights section")
	assert.True(t, strings.Contains(content, "portfolio perf"),
		"root Long should include the first novel feature")
	assert.True(t, strings.Contains(content, "digest --watchlist tech"),
		"root Long should include the second novel feature")
	assert.True(t, strings.Contains(content, "auth login-chrome"),
		"root Long should include the third novel feature")
	assert.False(t, strings.Contains(content, "Side-by-side quote comparison"),
		"root Long should cap at top 3 novel features; the fourth should not appear")
	assert.True(t, strings.Contains(content, "add --agent to any command"),
		"root Long should point at --agent mode for agent consumers")
	assert.True(t, strings.Contains(content, "helped-pp-cli doctor"),
		"root Long should point at doctor for auth/connectivity checks")
	assert.True(t, strings.Contains(content, "Every feature plus a local store"),
		"root Short and Long should incorporate the narrative headline")
}

// TestRootLongHandlesBackticksInNarrativeText asserts that backticks in
// LLM-authored narrative fields (common — e.g. "the `--agent` flag") do
// not produce invalid Go source. Root-template embeds Short/Long inside
// Go raw-string literals, which cannot contain backticks; without
// escaping, the generated root.go fails to compile.
func TestRootLongHandlesBackticksInNarrativeText(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ticks")
	outputDir := filepath.Join(t.TempDir(), "ticks-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline: "The `--agent`-native CLI",
	}
	gen.NovelFeatures = []NovelFeature{
		{Command: "portfolio perf", Description: "Uses the `sync` data via `--json`"},
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	// No backtick should appear inside the Short/Long raw strings.
	// Extract the Command block and assert it contains no raw backtick
	// other than the string delimiters themselves. Simplest check: count
	// backticks in the Short line — should be exactly 2 (the delimiters).
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Short:") && strings.Contains(line, "`") {
			count := strings.Count(line, "`")
			assert.Equal(t, 2, count,
				"Short line should have exactly two backticks (the raw-string delimiters); got %d. Line: %s", count, line)
		}
	}

	// Confirm the sanitizer rewrote backticks to apostrophes (preserving intent).
	assert.True(t, strings.Contains(content, "The '--agent'-native CLI"),
		"backticks in headline should be sanitized to apostrophes, not stripped")
	assert.True(t, strings.Contains(content, "'sync' data via '--json'"),
		"backticks in novel-feature description should be sanitized to apostrophes")

	// Most important: the generated Go must actually be parseable.
	// Run go vet to catch syntax errors without a full build.
	require.NoError(t, runGoVet(t, outputDir),
		"generated root.go with sanitized narrative should compile")
}

func runGoVet(t *testing.T, dir string) error {
	t.Helper()
	cacheDir, err := goBuildCacheDir(dir)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "vet", "./internal/cli/...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir, "GOFLAGS=-mod=mod")
	// go vet requires a valid module — run mod tidy first.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = cmd.Env
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Logf("mod tidy output: %s", string(out))
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("go vet output: %s", string(out))
	}
	return err
}

// TestRootLongFallsBackWhenNoNarrative asserts a sensible generic Long is
// emitted when no narrative or novel features exist — no hallucinated
// highlights, just pointer to --agent and doctor.
func TestRootLongFallsBackWhenNoNarrative(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain")
	outputDir := filepath.Join(t.TempDir(), "plain-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	assert.True(t, strings.Contains(content, "Manage plain resources via the plain API."),
		"fallback Long should restate the API")
	assert.True(t, strings.Contains(content, "Add --agent to any command"),
		"fallback Long should still point at --agent")
	assert.True(t, strings.Contains(content, "plain-pp-cli doctor"),
		"fallback Long should still point at doctor")
	assert.False(t, strings.Contains(content, "Highlights (not in the official API docs):"),
		"fallback Long should not render a Highlights header when no novel features exist")
}
