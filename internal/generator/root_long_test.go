package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestRootLongIncludesAllNovelFeatures asserts the generated root.go Long
// description names every verified-built novel feature (not just a
// hardcoded top-N), plus --agent and doctor pointers. The goal is that
// an agent running `<cli> --help` can pick the right novel command
// without a second discovery round — which requires seeing all the
// novel commands, not a curated subset.
func TestRootLongIncludesAllNovelFeatures(t *testing.T) {
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
		{Command: "compare", Description: "Side-by-side quote comparison"},
		{Command: "sparkline", Description: "Unicode chart for recent price action"},
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	assert.True(t, strings.Contains(content, "Highlights (not in the official API docs):"),
		"root Long should introduce the highlights section")
	// All five novel commands must appear — this is the whole point of
	// surfacing the novel features in Long rather than forcing discovery.
	for _, cmd := range []string{"portfolio perf", "digest --watchlist tech", "auth login-chrome", "compare", "sparkline"} {
		assert.True(t, strings.Contains(content, cmd),
			"root Long should include novel feature %q without a top-N cap", cmd)
	}
	// No overflow breadcrumb should appear when the full list fits under cap.
	assert.False(t, strings.Contains(content, "and 0 more"),
		"overflow breadcrumb should not render for a 5-feature CLI (cap is 15)")
	assert.True(t, strings.Contains(content, "add --agent to any command"),
		"root Long should point at --agent mode for agent consumers")
	assert.True(t, strings.Contains(content, "helped-pp-cli doctor"),
		"root Long should point at doctor for auth/connectivity checks")
	assert.True(t, strings.Contains(content, "Every feature plus a local store"),
		"root Short and Long should incorporate the narrative headline")
}

// TestRootLongOverflowsGracefullyAt16PlusFeatures asserts that a CLI with
// more novel features than the per-Long cap (15) renders the first 15 and
// trails with a "…and N more — see README" breadcrumb, preserving the
// size budget without silently dropping features.
func TestRootLongOverflowsGracefullyAt16PlusFeatures(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("overflow")
	outputDir := filepath.Join(t.TempDir(), "overflow-pp-cli")
	gen := New(apiSpec, outputDir)
	for i := range 20 {
		gen.NovelFeatures = append(gen.NovelFeatures, NovelFeature{
			Command:     "cmd-" + string(rune('a'+i)),
			Description: "novel feature number " + string(rune('a'+i)),
		})
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	// First 15 features render in full.
	for i := range 15 {
		assert.True(t, strings.Contains(content, "cmd-"+string(rune('a'+i))),
			"feature cmd-%s (rank %d) should render within the 15-item cap", string(rune('a'+i)), i)
	}
	// Remaining 5 are represented by the breadcrumb, not hidden silently.
	assert.True(t, strings.Contains(content, "…and 5 more — see README.md for the full list"),
		"overflow tail should render as a breadcrumb naming the hidden count; content:\n%s", content)
	// Breadcrumb count should be accurate — 20 features - 15 shown = 5 hidden.
	assert.False(t, strings.Contains(content, "…and 6 more"),
		"breadcrumb count should be exact (20 - 15 = 5, not 6)")
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
	lines := strings.SplitSeq(content, "\n")
	for line := range lines {
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

// yamlUnmarshalForTest is a tiny shim so root_long_test doesn't need to
// import yaml.v3 directly — delegates to skill_test's yaml import via
// the same package. Kept as a separate helper to make the intent clear
// at the call site.
func yamlUnmarshalForTest(body string, out any) error {
	return yaml.Unmarshal([]byte(body), out)
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

// TestRootLongStaysUnderSizeBudget asserts that an absorb output with
// ten novel features and a verbose headline does not produce a bloated
// --help Long. Agents running --help on a wall-of-text CLI is the same
// token-waste problem this change set is trying to solve — don't fix
// one discovery-loop problem by creating a different one.
//
// Budget (enforced by truncate helper in the template):
//   - Headline clipped to 120 runes
//   - Top 15 novel features (cap in Go; remaining dropped with overflow breadcrumb)
//   - Each feature description clipped to 200 runes
//
// Upper bound: Long should never exceed ~4000 chars even at the worst
// case (15 features × 200 chars + headline + framing). We assert a
// slightly looser cap (5000) so trivial copy tweaks don't break the test.
func TestRootLongStaysUnderSizeBudget(t *testing.T) {
	t.Parallel()

	longStr := strings.Repeat("x", 500) // intentionally over every cap
	apiSpec := minimalSpec("bounded")
	outputDir := filepath.Join(t.TempDir(), "bounded-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline: "A very verbose headline that exceeds the 120-rune budget: " + longStr,
	}
	// Ten features, each with a runaway description.
	for i := range 10 {
		gen.NovelFeatures = append(gen.NovelFeatures, NovelFeature{
			Command:     "cmd" + strings.Repeat("x", i),
			Description: "runaway description " + longStr,
		})
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	// Extract the Long raw-string body so we're measuring the actual --help
	// payload, not the whole generated file.
	longStart := strings.Index(content, "Long: `")
	require.NotEqual(t, -1, longStart, "Long field should be rendered")
	longStart += len("Long: `")
	longEnd := strings.Index(content[longStart:], "`,")
	require.NotEqual(t, -1, longEnd, "Long raw string should close")
	longBody := content[longStart : longStart+longEnd]

	assert.LessOrEqual(t, len(longBody), 5000,
		"root --help Long should stay under 5000 chars; got %d chars. Body:\n%s",
		len(longBody), longBody)

	// All 10 features render (under the 15-item cap) — we dropped the
	// top-N cap so novel capabilities aren't hidden from CLI-only agents.
	for i := range 10 {
		assert.True(t, strings.Contains(longBody, "cmd"+strings.Repeat("x", i)),
			"feature cmd%s should appear in Long (10 features is under the 15-cap)", strings.Repeat("x", i))
	}
	// No overflow breadcrumb at 10 features.
	assert.False(t, strings.Contains(longBody, "…and"),
		"overflow breadcrumb should not render at 10 features (cap is 15)")

	// No single feature description should contain the full 500-x runaway —
	// the truncate helper must clip it with an ellipsis.
	assert.False(t, strings.Contains(longBody, strings.Repeat("x", 200)),
		"truncate helper should clip long descriptions; found a 200-x run in Long body")
	assert.True(t, strings.Contains(longBody, "…"),
		"truncated content should carry the ellipsis marker")

	// Generated Go must still compile — truncation must not produce invalid syntax.
	require.NoError(t, runGoVet(t, outputDir),
		"bounded Long should still produce parseable Go")
}

// TestEndToEndGenerateWithFullNarrativeBuildsAndParses is the belt-and-suspenders
// integration test: populate a spec with a full Narrative + grouped novel
// features including adversarial characters (backticks, quotes, backslashes,
// apostrophes), generate the CLI, then (1) go build the output and (2)
// parse SKILL.md as YAML. Catches regressions in either the Go-source
// escaping path or the YAML-frontmatter escaping path in one shot.
//
// Every other test in this package covers one shape at a time. This one
// exercises the full combination the absorb skill is likely to produce
// so regressions in escape-helper wiring surface here even if narrower
// tests forget to add new adversarial inputs.
func TestEndToEndGenerateWithFullNarrativeBuildsAndParses(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("e2e")
	apiSpec.Description = "Multi-line\nspec description with \"quotes\" and \\backslashes."
	outputDir := filepath.Join(t.TempDir(), "e2e-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline:  "The `--agent`-native CLI with \"smart\" features",
		ValueProp: "Does things only this CLI does. Uses `sync` for local state.",
		WhenToUse: "When you need `--agent` output and can't use `curl`.",
		QuickStart: []QuickStartStep{
			{Command: "e2e-pp-cli items list --agent", Comment: "Get everything as JSON"},
		},
		Troubleshoots: []TroubleshootTip{
			{Symptom: "HTTP 429", Fix: "Wait and retry, or use `--rate-limit`"},
		},
		Recipes: []Recipe{
			{Title: "Daily sync", Command: "e2e-pp-cli items list --json", Explanation: "Useful for `cron` jobs"},
		},
		TriggerPhrases: []string{"what's the price", `use "e2e"`, "quote something"},
	}
	gen.NovelFeatures = []NovelFeature{
		{
			Command:      "items list",
			Description:  "List with `--select` filtering",
			Example:      "e2e-pp-cli items list --agent",
			WhyItMatters: "Agents skip `--help` discovery",
			Group:        "Agent-native",
		},
	}
	require.NoError(t, gen.Generate())

	// (1) Generated Go must compile. go vet is lighter than build and catches
	// the syntax errors that unescaped backticks in Short/Long produce.
	require.NoError(t, runGoVet(t, outputDir),
		"generated root.go with full adversarial narrative must be parseable Go")

	// (2) Generated SKILL.md frontmatter must be valid YAML.
	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)
	require.True(t, strings.HasPrefix(content, "---\n"))
	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end)
	body := strings.TrimSuffix(strings.TrimPrefix(content[:4+end+5], "---\n"), "---\n")
	var parsed map[string]any
	require.NoError(t, yamlUnmarshalForTest(body, &parsed),
		"generated SKILL.md frontmatter with full adversarial narrative must be valid YAML")
	require.Contains(t, parsed, "description")
	require.Contains(t, parsed, "name")

	// (3) Every fenced code block in README must be balanced. Unescaped
	// backticks in narrative content that rendered into fences would
	// produce an odd count.
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	fences := strings.Count(string(readme), "```")
	assert.Equal(t, 0, fences%2,
		"README fenced code blocks must be balanced; odd count means narrative text broke a fence")
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
