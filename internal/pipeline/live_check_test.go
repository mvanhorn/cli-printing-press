package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeStubBinary drops a tiny shell script at cliDir/<name> that echoes a
// response based on its arguments. Used to simulate the CLI under test.
// Skips the test on Windows since we shell out via sh -c.
func writeStubBinary(t *testing.T, cliDir, name, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on Windows")
	}
	path := filepath.Join(cliDir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	return path
}

// writeTestResearchJSON writes a minimal research.json with the given features.
func writeTestResearchJSON(t *testing.T, cliDir string, features []NovelFeature) {
	t.Helper()
	data := map[string]any{
		"api_name":       "live-check-test",
		"novel_features": features,
	}
	body, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "research.json"), body, 0o644))
}

// TestLiveCheck_UnableWhenNoResearch verifies the check gracefully reports
// Unable=true when research.json is missing rather than treating the absent
// data as failure. A CLI without research.json should not be penalized by
// the live check.
func TestLiveCheck_UnableWhenNoResearch(t *testing.T) {
	dir := t.TempDir()
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "bin", Timeout: time.Second})
	require.True(t, result.Unable)
	require.Contains(t, result.Reason, "research.json")
	require.Zero(t, result.Checked())
}

// TestLiveCheck_UnableWhenNoExamples verifies the check skips when research
// exists but no novel feature has an Example command.
func TestLiveCheck_UnableWhenNoExamples(t *testing.T) {
	dir := t.TempDir()
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Feature A", Command: "foo", Description: "no example"},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "bin", Timeout: time.Second})
	require.True(t, result.Unable)
	require.Contains(t, result.Reason, "Example")
}

// TestLiveCheck_UnableWhenNoBinary verifies the check reports Unable when the
// binary doesn't exist — distinguishing "CLI wasn't built" from "CLI flunked".
func TestLiveCheck_UnableWhenNoBinary(t *testing.T) {
	dir := t.TempDir()
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "A", Command: "a", Example: "bin a --flag"},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "missing-binary", Timeout: time.Second})
	require.True(t, result.Unable)
	require.Contains(t, result.Reason, "binary")
}

// TestLiveCheck_PassOnHappyPath verifies a feature that exits 0 with output
// matching the query word passes.
func TestLiveCheck_PassOnHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `echo "Found 3 brownie recipes matching your query"`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Best ranker", Command: "goat", Example: `stub goat "brownies" --limit 5`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.False(t, result.Unable, "result was Unable: %s", result.Reason)
	require.Equal(t, 1, result.Checked())
	require.Equal(t, 1, result.Passed)
	require.Zero(t, result.Failed)
	require.Equal(t, 1.0, result.PassRate)
}

// TestLiveCheck_FailOnIrrelevantOutput verifies the relevance check catches
// the Recipe GOAT pattern: command runs successfully but returns results that
// don't match the query (e.g., "brownies" → Texas Chili).
func TestLiveCheck_FailOnIrrelevantOutput(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `echo "Found 5 Texas Chili recipes ranked by rating"`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Best ranker", Command: "goat", Example: `stub goat "brownies" --limit 5`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.False(t, result.Unable)
	require.Equal(t, 1, result.Failed, "expected irrelevant output to fail")
	require.Equal(t, 0.0, result.PassRate)
	require.Contains(t, result.Features[0].Reason, "query")
}

// TestLiveCheck_FailOnExitError verifies a command that exits non-zero is
// recorded as fail, not skip.
func TestLiveCheck_FailOnExitError(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `echo "something went wrong" >&2; exit 5`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Broken", Command: "b", Example: `stub b --flag`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.Equal(t, 1, result.Failed)
	require.Contains(t, result.Features[0].Reason, "exit 5")
}

// TestLiveCheck_FailOnEmptyOutput ensures stdout must be non-empty.
func TestLiveCheck_FailOnEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `exit 0`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Quiet", Command: "q", Example: `stub q`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.Equal(t, 1, result.Failed)
	require.Contains(t, result.Features[0].Reason, "empty output")
}

// TestLiveCheck_PrefersBuiltFeatures verifies the check samples the verified
// `novel_features_built` list (dogfood-validated) over the aspirational
// `novel_features` list when both are present.
func TestLiveCheck_PrefersBuiltFeatures(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `echo "matched built-feature output"`)
	built := []NovelFeature{
		{Name: "Built", Command: "b", Example: `stub b "built-feature" --flag`},
	}
	data := map[string]any{
		"api_name":             "live-check-test",
		"novel_features":       []NovelFeature{{Name: "Planned", Example: `stub p "planned" --flag`}},
		"novel_features_built": built,
	}
	body, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "research.json"), body, 0o644))

	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.Equal(t, 1, result.Checked())
	require.Equal(t, "Built", result.Features[0].Name,
		"should use novel_features_built when present")
}

// TestInsightCap verifies the pass-rate → cap mapping, which is the scorecard
// integration contract.
func TestInsightCap(t *testing.T) {
	cases := []struct {
		name    string
		input   *LiveCheckResult
		wantNil bool
		wantCap int
	}{
		{"nil", nil, true, 0},
		{"unable", &LiveCheckResult{Unable: true}, true, 0},
		{"zero checked", &LiveCheckResult{}, true, 0},
		{"100% pass", &LiveCheckResult{Passed: 5, PassRate: 1.0}, true, 0},
		{"80% pass", &LiveCheckResult{Passed: 8, Failed: 2, PassRate: 0.8}, true, 0},
		{"50% pass", &LiveCheckResult{Passed: 5, Failed: 5, PassRate: 0.5}, false, 7},
		{"30% pass", &LiveCheckResult{Passed: 3, Failed: 7, PassRate: 0.3}, false, 4},
		{"0% pass", &LiveCheckResult{Failed: 5, PassRate: 0.0}, false, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InsightCapFromLiveCheck(tc.input)
			if tc.wantNil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tc.wantCap, *got)
			}
		})
	}
}

// TestShellSplit covers the quoted-query parsing used for Example commands.
func TestShellSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`cli goat brownies`, []string{"cli", "goat", "brownies"}},
		{`cli goat "chicken tikka masala" --limit 5`, []string{"cli", "goat", "chicken tikka masala", "--limit", "5"}},
		{`cli  multiple   spaces`, []string{"cli", "multiple", "spaces"}},
	}
	for _, tc := range cases {
		got, err := shellSplit(tc.in)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "input=%q", tc.in)
	}
	_, err := shellSplit(`cli "unclosed`)
	require.Error(t, err)
}

// TestExtractQueryToken covers the query detection used for relevance checks.
// The extractor is deliberately simple: it returns the last positional
// argument before the first flag, after excluding URLs and numeric IDs
// (which won't appear as text in the CLI output). For multi-word command
// paths like `cookbook list`, the extractor will return the 2nd word and
// the downstream relevance check will usually succeed vacuously — that's
// an acceptable cost for a stateless heuristic.
func TestExtractQueryToken(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"goat", "brownies", "--limit", "5"}, "brownies"},
		{[]string{"sub", "buttermilk"}, "buttermilk"},
		{[]string{"recipe", "get", "https://example.com/recipe"}, ""},
		{[]string{"recipe", "open", "42"}, ""},
		{[]string{"tonight", "--max-time", "30m"}, ""},
		{[]string{"cookbook"}, ""},
	}
	for _, tc := range cases {
		got := extractQueryToken(tc.args)
		require.Equal(t, tc.want, got, "args=%v", tc.args)
	}
}

// TestOutputMentionsQuery ensures case-insensitive per-token matching.
func TestOutputMentionsQuery(t *testing.T) {
	require.True(t, outputMentionsQuery("Found 5 Brownie Recipes", "brownies"))
	require.True(t, outputMentionsQuery("chicken tikka masala results", "chicken"))
	require.False(t, outputMentionsQuery("Texas Chili Recipes", "brownies"))
	// Tokens under 3 chars are ignored (too generic).
	require.False(t, outputMentionsQuery("irrelevant", "to"))
}

// TestLiveCheckMarshalJSON verifies the custom marshaller emits pass_rate_pct.
func TestLiveCheckMarshalJSON(t *testing.T) {
	r := &LiveCheckResult{Passed: 2, PassRate: 2.0 / 3.0}
	body, err := json.Marshal(r)
	require.NoError(t, err)
	require.Contains(t, string(body), `"pass_rate_pct":67`)
	require.NotContains(t, string(body), "0.6666")
}

// smoke test that ties research, a stub binary, and the full RunLiveCheck
// path together.
func TestLiveCheck_SmokeTest(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `
case "$1" in
  goat) echo "Best brownie recipes: 1. Classic Brownies 2. Fudgy Brownies";;
  sub)  echo "Substitutions for buttermilk: milk + lemon juice";;
  *)    echo "unknown command $1" >&2; exit 2;;
esac
`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Ranker", Command: "goat", Example: `stub goat "brownies" --limit 5`},
		{Name: "Subs", Command: "sub", Example: `stub sub buttermilk`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.Equal(t, 2, result.Checked())
	require.Equal(t, 2, result.Passed)
	require.Equal(t, 1.0, result.PassRate)
	// Ensure pass_rate_pct marshals cleanly.
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), `"pass_rate_pct":100`))
}

// TestLiveCheck_ConcurrentExecutionPreservesOrder ensures the worker pool
// produces Features in the input order (not the order workers finish). A
// slow-first feature would otherwise land at the end of the results slice.
func TestLiveCheck_ConcurrentExecutionPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	// Each invocation sleeps inversely proportional to the argument so the
	// first feature is the slowest — if ordering leaked through the pool,
	// results would come back reversed.
	writeStubBinary(t, dir, "stub", `
case "$2" in
  aaaa) sleep 0.15; echo "AAAA matched aaaa";;
  bbbb) sleep 0.05; echo "BBBB matched bbbb";;
  cccc) sleep 0.01; echo "CCCC matched cccc";;
esac
`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "First", Command: "c", Example: `stub c aaaa`},
		{Name: "Second", Command: "c", Example: `stub c bbbb`},
		{Name: "Third", Command: "c", Example: `stub c cccc`},
	})
	result := RunLiveCheck(LiveCheckOptions{
		CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second, Concurrency: 3,
	})
	require.Equal(t, 3, result.Checked())
	require.Equal(t, "First", result.Features[0].Name)
	require.Equal(t, "Second", result.Features[1].Name)
	require.Equal(t, "Third", result.Features[2].Name)
}

// TestLiveCheck_OutputCap guards against OOM from a runaway feature that
// streams megabytes of output. The cap is MaxOutputBytes (1 MiB); the test
// writes 2 MiB so the limitedWriter has to truncate without blowing up the
// process. The Example has only one positional so no relevance check fires
// against the (mostly 'x') output.
func TestLiveCheck_OutputCap(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `head -c 2097152 /dev/zero | tr '\0' 'x'`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Noisy", Command: "n", Example: `stub n`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 10 * time.Second})
	require.Equal(t, 1, result.Passed, "run should complete despite bounded output")
}

// TestLiveCheck_BinaryAutoDerivation verifies RunLiveCheck finds the binary
// when BinaryName is empty by trying <base>-pp-cli then <base>.
func TestLiveCheck_BinaryAutoDerivation(t *testing.T) {
	dir := t.TempDir()
	// CLIDir basename is the last path segment. Build a stub named that way
	// and a stub named `<name>-pp-cli`; the latter should be preferred.
	base := filepath.Base(dir)
	writeStubBinary(t, dir, base+"-pp-cli", `echo "matched via -pp-cli"`)
	writeStubBinary(t, dir, base, `echo "matched via base"`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "X", Command: "x", Example: `stub x matched`},
	})
	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, Timeout: 5 * time.Second})
	require.False(t, result.Unable, "should have found a binary: %s", result.Reason)
	require.Equal(t, 1, result.Passed)
	require.Contains(t, result.Features[0].Example, "stub x matched")
}

// TestChecked_DerivedFromCounters ensures the Checked() method is a pure
// derivation — if it ever drifts from Passed+Failed+Skipped the live-check
// invariant is broken.
func TestChecked_DerivedFromCounters(t *testing.T) {
	cases := []struct {
		r    LiveCheckResult
		want int
	}{
		{LiveCheckResult{}, 0},
		{LiveCheckResult{Passed: 3}, 3},
		{LiveCheckResult{Passed: 1, Failed: 2, Skipped: 3}, 6},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.r.Checked())
	}
	// Also: nil receiver must not panic.
	var nilRes *LiveCheckResult
	require.Zero(t, nilRes.Checked())
}

// --- detectRawHTMLEntities (Wave B / R3) ---

func TestDetectRawHTMLEntities_CleanOutput(t *testing.T) {
	cases := []string{
		"The Food Lab's Cookies",
		"Recipe title with AT&T in it",
		"Ordinary scraped text. No entities here.",
		"",
		"   \n\t  ",
	}
	for _, out := range cases {
		require.Empty(t, detectRawHTMLEntities(out, nil),
			"unexpected warning for clean output %q", out)
	}
}

func TestDetectRawHTMLEntities_DetectsNumericEntity(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{"apostrophe", "The Food Lab&#39;s Chocolate Chip Cookies"},
		{"typographic apostrophe", "Ben&#8217;s Pizza"},
		{"entity mid-line", "Row 1\nRow 2 with &#34; in it\nRow 3"},
	}
	for _, tc := range cases {
		msg := detectRawHTMLEntities(tc.output, nil)
		require.NotEmpty(t, msg, "expected warning for %q", tc.name)
		require.Contains(t, msg, "raw HTML entity", tc.name)
	}
}

func TestDetectRawHTMLEntities_SkipsWhenJSONFlagPresent(t *testing.T) {
	// --json in args means agent-facing structured output. Entities in
	// string values are legitimate JSON, not a display bug.
	out := `{"title": "The Food Lab&#39;s Cookies"}`
	require.Empty(t, detectRawHTMLEntities(out, []string{"--json"}))
	require.Empty(t, detectRawHTMLEntities(out, []string{"goat", "brownies", "--json", "--limit", "5"}))
}

func TestDetectRawHTMLEntities_SkipsWhenOutputStartsWithJSON(t *testing.T) {
	// Defense in depth: a feature that always emits JSON regardless of
	// flags still shouldn't trip the check when the output is structured.
	require.Empty(t, detectRawHTMLEntities(`{"title":"x&#39;y"}`, nil))
	require.Empty(t, detectRawHTMLEntities(`[{"title":"x&#39;y"}]`, nil))
	// Leading whitespace before the JSON start still counts as JSON mode.
	require.Empty(t, detectRawHTMLEntities("  \n{\"title\":\"x&#39;y\"}", nil))
}

func TestDetectRawHTMLEntities_IgnoresNamedEntities(t *testing.T) {
	// Named entities are out of scope in Wave B — false-positive risk on
	// legitimate strings like "AT&amp;T" and README prose. Wave C can
	// revisit after calibrating on the library.
	require.Empty(t, detectRawHTMLEntities("AT&amp;T", nil))
	require.Empty(t, detectRawHTMLEntities("foo &quot;bar&quot; baz", nil))
	require.Empty(t, detectRawHTMLEntities("less than: &lt; greater than: &gt;", nil))
}

func TestDetectRawHTMLEntities_IgnoresPartialSequences(t *testing.T) {
	// Pattern requires digits AND a closing semicolon. "&#abc;" or "&#"
	// alone are not valid entities and shouldn't warn.
	require.Empty(t, detectRawHTMLEntities("price: $1&#USD", nil))
	require.Empty(t, detectRawHTMLEntities("foo & bar #39", nil))
}

func TestRunOneFeatureCheck_WarnsOnEntityButStaysPass(t *testing.T) {
	// Integration: a feature whose output is valid but contains a raw
	// numeric entity should still Pass (pass-rate unaffected) but carry
	// a warning in the result.
	binary := buildFakeCLI(t, `#!/usr/bin/env bash
printf 'The Food Lab&#39;\''s Chocolate Chip Cookies\n'
`)
	feature := NovelFeature{
		Name:    "goat",
		Command: "goat",
		Example: "bin goat chocolate chip cookies",
	}
	result := runOneFeatureCheck(t.TempDir(), binary, feature, 5*time.Second)
	require.Equal(t, StatusPass, result.Status, "reason: %s", result.Reason)
	require.NotEmpty(t, result.Warnings, "expected entity warning")
	require.Contains(t, result.Warnings[0], "raw HTML entity")
}

// buildFakeCLI writes a shell script to a temp file and returns its path.
// Used by entity tests to exercise runOneFeatureCheck end-to-end without
// depending on a real generated CLI binary.
func buildFakeCLI(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-cli")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}
