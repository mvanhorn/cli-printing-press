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

// TestLiveCheck_ResearchDirOverride verifies that ResearchDir, when set,
// is used to locate research.json instead of CLIDir. This matters when a
// CLI's working dir and the run-state's research.json live in different
// directories — without this option, callers had to copy research.json
// into the CLI dir as a workaround.
func TestLiveCheck_ResearchDirOverride(t *testing.T) {
	cliDir := t.TempDir()      // no research.json here
	researchDir := t.TempDir() // research.json lives here
	writeStubBinary(t, cliDir, "bin", `exit 0`)
	writeTestResearchJSON(t, researchDir, []NovelFeature{
		{Name: "Feature A", Command: "foo", Description: "no example"},
	})

	// Default behavior: looks under CLIDir, finds nothing, reports Unable.
	r1 := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "bin", Timeout: time.Second})
	require.True(t, r1.Unable)
	require.Contains(t, r1.Reason, "research.json")

	// With ResearchDir: looks at the override, finds research.json. Now the
	// reason is "no Example commands" — different failure mode, proves we
	// did locate research.json successfully.
	r2 := RunLiveCheck(LiveCheckOptions{
		CLIDir:      cliDir,
		ResearchDir: researchDir,
		BinaryName:  "bin",
		Timeout:     time.Second,
	})
	require.True(t, r2.Unable)
	require.Contains(t, r2.Reason, "Example", "after locating research.json, the next gate is the Example-command check")
	require.NotContains(t, r2.Reason, "no research.json", "should have read research.json from ResearchDir")
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

func TestLiveCheck_FallsBackToGeneratedCommandTree(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"products","subcommands":[{"name":"list"},{"name":"reviews","subcommands":[{"name":"list"}]}]},
  {"name":"doctor"},
  {"name":"auth","subcommands":[{"name":"refresh"}]}
]}
JSON
  exit 0
fi
case "$1 $2 $3" in
  "products list --json") echo '{"data":[{"id":"p1"}]}' ;;
  "products reviews list") echo '{"data":[{"id":"r1"}]}' ;;
  *) echo "unexpected $*" >&2; exit 2 ;;
esac
`)
	writeTestResearchJSON(t, dir, nil)

	result := RunLiveCheck(LiveCheckOptions{CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second})
	require.False(t, result.Unable, "result was Unable: %s", result.Reason)
	require.Equal(t, 2, result.Checked())
	require.Equal(t, 2, result.Passed)
	require.Equal(t, "products list", result.Features[0].Command)
	require.Equal(t, "stub products list --json", result.Features[0].Example)
	require.Equal(t, "products reviews list", result.Features[1].Command)
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
		args        []string
		commandPath string
		want        string
	}{
		{[]string{"goat", "brownies", "--limit", "5"}, "goat", "brownies"},
		{[]string{"sub", "buttermilk"}, "sub", "buttermilk"},
		{[]string{"recipe", "get", "https://example.com/recipe"}, "recipe get", ""},
		{[]string{"recipe", "open", "42"}, "recipe open", ""},
		{[]string{"tonight", "--max-time", "30m"}, "tonight", ""},
		{[]string{"cookbook", "list", "--json"}, "cookbook list", ""},
		{[]string{"cookbook"}, "cookbook", ""},
		// Verb-shaped command paths: trailing word names the view, not a
		// search query. Without this, the relevance check would fail
		// commands like `slices today --json` whose structured output has
		// no reason to echo the verb. The commonCommandVerb fallback
		// covers these even without the cobra-path lookup.
		{[]string{"slices", "today", "--json"}, "slices today", ""},
		{[]string{"store", "tonight", "--json"}, "store tonight", ""},
		{[]string{"events", "now"}, "events now", ""},
		{[]string{"orders", "pending", "--json"}, "orders pending", ""},
		{[]string{"deals", "current"}, "deals current", ""},

		// Cobra-tree-aware: command paths whose trailing word is an
		// English-shaped verb that is NOT in the static commonCommandVerb
		// list (e.g., `find`, `top`). The commandPath argument tells us
		// the word names a subcommand, so it's not a query.
		{[]string{"leaderboard", "top"}, "leaderboard top", ""},
		{[]string{"recipes", "find"}, "recipes find", ""},
		{[]string{"hot", "browse"}, "hot browse", ""},

		// But a positional that LOOKS like a command-path word but isn't
		// in the commandPath should still be treated as a query.
		{[]string{"recipes", "find", "ramen"}, "recipes find", "ramen"},
	}
	for _, tc := range cases {
		got := extractQueryToken(tc.args, tc.commandPath)
		require.Equal(t, tc.want, got, "args=%v cmd=%q", tc.args, tc.commandPath)
	}
}

// TestOutputMentionsQuery ensures case-insensitive per-token matching.
func TestOutputMentionsQuery(t *testing.T) {
	require.True(t, outputMentionsQuery("Found 5 Brownie Recipes", "brownies"))
	require.True(t, outputMentionsQuery("chicken tikka masala results", "chicken"))
	require.False(t, outputMentionsQuery("Texas Chili Recipes", "brownies"))
	// Tokens under 3 chars are ignored (too generic).
	require.False(t, outputMentionsQuery("irrelevant", "to"))

	// Comma-separated query tokens — each name should be checked
	// independently against the output. Without comma-splitting, the
	// whole "pikachu,charizard,blastoise" string was treated as one
	// token and never matched a JSON-array shape like
	// `["pikachu","charizard","blastoise"]` (quote+comma+quote
	// separators break the substring match).
	require.True(t, outputMentionsQuery(`["pikachu","charizard","blastoise"]`, "pikachu,charizard,blastoise"))
	require.True(t, outputMentionsQuery("blastoise sighting", "pikachu,charizard,blastoise"))
	// Mixed delimiters in the query (commas + spaces) still tokenize
	// independently so a single-name match counts.
	require.True(t, outputMentionsQuery("found pikachu", "pikachu, charizard"))
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
		{"decimal apostrophe", "The Food Lab&#39;s Chocolate Chip Cookies"},
		{"decimal typographic apostrophe", "Ben&#8217;s Pizza"},
		{"decimal mid-line", "Row 1\nRow 2 with &#34; in it\nRow 3"},
		// Hex numeric entities are equally common — APIs that encode
		// apostrophes as &#x27; should trip the same check. Decimal-only
		// regex was an oversight flagged by Wave B ce:review.
		{"hex lowercase", "Ben&#x27;s Pizza"},
		{"hex uppercase x", "foo&#X27;bar"},
		{"hex multi-char", "quote&#x2019;end"},
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
	// Cobra accepts `--json=true` / `--json=false` as distinct tokens
	// from bare `--json`. Adversarial review flagged the exact-match
	// form as a bypass hole.
	require.Empty(t, detectRawHTMLEntities(out, []string{"--json=true"}))
	require.Empty(t, detectRawHTMLEntities(out, []string{"cmd", "--json=false", "--limit", "5"}))
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

func TestRunOneFeatureCheck_PopulatesOutputSample(t *testing.T) {
	// Phase 4.85's agentic reviewer needs the captured stdout to judge
	// output plausibility without re-invoking the binary. OutputSample
	// must be populated on pass results.
	binary := buildFakeCLI(t, `#!/usr/bin/env bash
printf 'Hello cookie world\n'
`)
	feature := NovelFeature{
		Name:    "demo",
		Command: "demo",
		Example: "bin demo cookie",
	}
	result := runOneFeatureCheck(t.TempDir(), binary, feature, 5*time.Second)
	require.Equal(t, StatusPass, result.Status, "reason: %s", result.Reason)
	require.Contains(t, result.OutputSample, "Hello cookie world")
}

func TestSampleOutput_TruncatesLargeCapture(t *testing.T) {
	// Guard the serialized-sample size so one feature can't bloat the
	// scorecard JSON or overwhelm an agentic reviewer's context window.
	big := strings.Repeat("x", outputSampleMaxBytes*2)
	got := sampleOutput(big)
	require.Contains(t, got, "…[truncated]", "truncation marker missing")
	require.LessOrEqual(t, len(got), outputSampleMaxBytes+len("…[truncated]"))
}

func TestSampleOutput_ShortCapturePassesThrough(t *testing.T) {
	// Small captures must not be truncated or rewritten — they need to
	// survive a JSON round-trip byte-identical so downstream tests can
	// assert exact content.
	short := "hello world"
	require.Equal(t, short, sampleOutput(short))
}

func TestDetectRawHTMLEntities_TruncatesLongMatchInMessage(t *testing.T) {
	// Regex is bounded to 10 digits so this shouldn't trigger in
	// practice, but defend against future regex broadening: warning
	// message must never embed an unbounded match string.
	//
	// Construct an entity at the upper regex bound (10 digits).
	out := "text before " + "&#9999999999;" + " text after"
	msg := detectRawHTMLEntities(out, nil)
	require.NotEmpty(t, msg)
	require.LessOrEqual(t, len(msg), 200, "warning message should stay bounded regardless of match length")
}

// TestLiveCheck_PassOnGracefulEmpty proves the wiring: when the CLI exits
// non-zero with stderr that gracefully reports "no matching record" and
// echoes the user's input, RunLiveCheck counts it as PASS rather than FAIL
// (issue #484). One canonical case here; per-phrase coverage lives in
// TestIsGracefulEmptyResponse, which exercises the helper directly.
func TestLiveCheck_PassOnGracefulEmpty(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub",
		`echo "Error: post not found: my-launch-slug" >&2; exit 1`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Lookup", Command: "posts get",
			Example: `stub posts get my-launch-slug --json`},
	})
	result := RunLiveCheck(LiveCheckOptions{
		CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second,
	})
	require.Equal(t, 1, result.Passed, "graceful empty should count as PASS")
	require.Equal(t, 0, result.Failed, "graceful empty must not count as FAIL")
	require.Contains(t, result.Features[0].Reason, "graceful empty",
		"reason should label the graceful-empty branch so reviewers know why it passed")
}

// TestLiveCheck_FailOnGenericExitErrorWithoutArgEcho proves the negative
// wiring: a non-zero exit whose stderr lacks the arg echo (a config error,
// auth failure, or generic error) still counts as FAIL. One canonical case
// here; the boundary cases live in TestIsGracefulEmptyResponse.
func TestLiveCheck_FailOnGenericExitErrorWithoutArgEcho(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "stub", `echo "config file not found" >&2; exit 1`)
	writeTestResearchJSON(t, dir, []NovelFeature{
		{Name: "Lookup", Command: "posts get",
			Example: `stub posts get my-launch-slug --json`},
	})
	result := RunLiveCheck(LiveCheckOptions{
		CLIDir: dir, BinaryName: "stub", Timeout: 5 * time.Second,
	})
	require.Equal(t, 0, result.Passed, "phrase-without-arg-echo must NOT pass via graceful-empty")
	require.Equal(t, 1, result.Failed)
}

// TestIsGracefulEmptyResponse exercises the helper directly across phrase
// and arg-echo dimensions. Table-driven to keep the boundary explicit.
// Caller contract guarantees non-zero exit, so no exit-code dimension here.
func TestIsGracefulEmptyResponse(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		args   []string
		want   bool
	}{
		{"phrase + arg echo → graceful",
			"Error: post not found: my-launch-slug",
			[]string{"posts", "get", "my-launch-slug"}, true},
		{"phrase but no arg echo → not graceful (config error case)",
			"config file not found",
			[]string{"posts", "get", "my-launch-slug"}, false},
		{"arg echo but no phrase → not graceful",
			"Error: 500 internal server error processing my-launch-slug",
			[]string{"posts", "get", "my-launch-slug"}, false},
		{"no positional args → not graceful (no input to echo)",
			"Error: post not found", []string{"posts", "list"}, false},
		{"flag-only args → not graceful",
			"Error: post not found", []string{"--limit", "5"}, false},
		{"no results phrase + arg echo → graceful",
			"no results for cursor-ide", []string{"posts", "get", "cursor-ide"}, true},
		{"empty stderr → not graceful",
			"", []string{"posts", "get", "my-slug"}, false},
		{"case-insensitive phrase match",
			"Post NOT FOUND: my-slug", []string{"posts", "get", "my-slug"}, true},
		{"short positional args (< 3 chars) skipped to avoid coincidence",
			"no results for go", []string{"posts", "get", "go"}, false},
		{"flag value as arg-echo candidate is fine",
			"no match for query: notion",
			[]string{"posts", "search", "--query", "notion"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isGracefulEmptyResponse(tc.stderr, tc.args)
			require.Equal(t, tc.want, got)
		})
	}
}
