package narrativecheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestExtractSubcommandWords pins the wordlist rule against the bash
// recipe it replaces. Each case is a research.json `command` string;
// the want is what the bash recipe's awk pipeline would produce.
func TestExtractSubcommandWords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single subcommand", "mycli widgets", "widgets"},
		{"nested subcommands", "mycli reports stats", "reports stats"},
		{"hyphenated subcommand", "mycli list-projects", "list-projects"},
		{"deep nesting", "mycli a b c d", "a b c d"},
		{"trailing flag", "mycli widgets list --json", "widgets list"},
		{"trailing flag with value", "mycli widgets list --since 7d", "widgets list"},
		{"flag mid-tokens", "mycli widgets --since 7d list", "widgets"},
		{"positional value with equals", "mycli widgets q=hello", "widgets"},
		// awk matches the whole token against the non-identifier regex,
		// so "ns:resource" emits nothing (not "ns").
		{"positional value with colon", "mycli ns:resource list", ""},
		{"bare binary", "mycli", ""},
		{"binary plus flag only", "mycli --version", ""},
		{"empty string", "", ""},
		{"single token with leading dash", "--help", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := strings.Join(extractSubcommandWords(tc.in), " ")
			if got != tc.want {
				t.Errorf("extractSubcommandWords(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestLoadCommands_Shapes covers the JSON-parsing contract: missing
// file, malformed JSON, empty narrative (both sections empty), partial
// narrative (one section populated).
func TestLoadCommands_Shapes(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := loadCommands(filepath.Join(t.TempDir(), "nope.json"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error %q should mention 'not found'", err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		t.Parallel()
		path := writeFile(t, "{ not json")
		_, err := loadCommands(path)
		if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("error %v should mention 'not valid JSON'", err)
		}
	})

	t.Run("no narrative section at all", func(t *testing.T) {
		t.Parallel()
		path := writeFile(t, `{"other_field": "ignored"}`)
		got, err := loadCommands(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 commands, got %d", len(got))
		}
	})

	t.Run("only quickstart populated", func(t *testing.T) {
		t.Parallel()
		path := writeFile(t, `{"narrative":{"quickstart":[{"command":"mycli a"},{"command":"mycli b"}]}}`)
		got, err := loadCommands(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Section != SectionQuickstart {
			t.Errorf("expected 2 quickstart entries, got %+v", got)
		}
	})

	t.Run("both sections populated, order preserved", func(t *testing.T) {
		t.Parallel()
		path := writeFile(t, `{"narrative":{
			"quickstart":[{"command":"mycli q1"}],
			"recipes":[{"command":"mycli r1"},{"command":"mycli r2"}]
		}}`)
		got, err := loadCommands(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 commands, got %d", len(got))
		}
		if got[0].Section != SectionQuickstart || got[1].Section != SectionRecipes {
			t.Errorf("expected quickstart before recipes, got %+v", got)
		}
	})

	t.Run("empty command strings are dropped", func(t *testing.T) {
		t.Parallel()
		path := writeFile(t, `{"narrative":{"quickstart":[{"command":""},{"command":"  "},{"command":"mycli x"}]}}`)
		got, err := loadCommands(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Command != "mycli x" {
			t.Errorf("expected single non-empty command, got %+v", got)
		}
	})
}

// TestValidate_EndToEnd builds a tiny stub binary that responds OK to
// some commands and "unknown command" to others, then runs Validate
// across a fixture research.json. Confirms the resolution pipeline
// (parse → words → exec → classify) end-to-end.
func TestValidate_EndToEnd(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub widgets list"},
			{"command":"stub typo-here"},
			{"command":"stub --version"}
		],
		"recipes":[
			{"command":"stub widgets show 42"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}

	if report.Walked != 2 {
		t.Errorf("Walked = %d, want 2 (widgets-list, widgets-show)", report.Walked)
	}
	if report.Missing != 1 {
		t.Errorf("Missing = %d, want 1 (typo-here)", report.Missing)
	}
	if report.Empty != 1 {
		t.Errorf("Empty = %d, want 1 (--version is bare-flag)", report.Empty)
	}
	if !report.HasFailures() {
		t.Error("HasFailures should be true with missing+empty entries")
	}

	// Verify per-result classification + section attribution
	bySection := map[Section]int{}
	for _, r := range report.Results {
		bySection[r.Section]++
	}
	if bySection[SectionQuickstart] != 3 || bySection[SectionRecipes] != 1 {
		t.Errorf("section counts wrong: %+v", bySection)
	}
}

func TestValidateWithOptions_FullExamplesCatchesInvalidFlag(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub widgets list --bad-flag"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.Walked != 0 {
		t.Errorf("Walked = %d, want 0", report.Walked)
	}
	if report.ExampleFailed != 1 {
		t.Errorf("ExampleFailed = %d, want 1", report.ExampleFailed)
	}
	if !report.HasFailures() {
		t.Error("HasFailures should be true when a full narrative example fails")
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(report.Results))
	}
	got := report.Results[0]
	if got.Status != StatusExampleFailed {
		t.Fatalf("Status = %q, want %q", got.Status, StatusExampleFailed)
	}
	if !strings.Contains(got.Error, "--bad-flag") {
		t.Errorf("Error %q should mention the invalid flag", got.Error)
	}
}

func TestValidateWithOptions_FrameworkOnlyCatchesInventedFrameworkFlags(t *testing.T) {
	t.Parallel()

	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stripe-pp-cli sync --since 2026-01-01 --resources customers,charges"},
			{"command":"stripe-pp-cli sync --since 7d --entities customers,charges"},
			{"command":"stripe-pp-cli --json sync --since 7d --entities customers,charges"},
			{"command":"stripe-pp-cli --entities sync --since 7d --resources customers,charges"},
			{"command":"stripe-pp-cli search \"acme.co\" --entities customers,invoices --json"},
			{"command":"stripe-pp-cli customers list --entities ignored-for-endpoint-command"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, "", Options{FrameworkOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.Walked != 0 {
		t.Errorf("Walked = %d, want 0", report.Walked)
	}
	if report.ExampleFailed != 5 {
		t.Fatalf("ExampleFailed = %d, want 5; results=%+v", report.ExampleFailed, report.Results)
	}
	if !report.HasFailures() {
		t.Fatal("HasFailures should be true for invented framework flags")
	}
	gotErrors := []string{report.Results[0].Error, report.Results[1].Error, report.Results[2].Error, report.Results[3].Error, report.Results[4].Error}
	if !strings.Contains(gotErrors[0], "--since") {
		t.Errorf("first error should catch absolute --since duration, got %q", gotErrors[0])
	}
	if !strings.Contains(gotErrors[1], "--entities") {
		t.Errorf("second error should catch invented sync --entities flag, got %q", gotErrors[1])
	}
	if !strings.Contains(gotErrors[2], "--entities") {
		t.Errorf("third error should catch invented sync --entities after a global flag, got %q", gotErrors[2])
	}
	if !strings.Contains(gotErrors[3], "--entities") {
		t.Errorf("fourth error should catch invented --entities before a framework command, got %q", gotErrors[3])
	}
	if !strings.Contains(gotErrors[4], "--entities") {
		t.Errorf("fifth error should catch invented search --entities flag, got %q", gotErrors[4])
	}
}

func TestValidateWithOptions_FrameworkOnlyCatchesMissingFlagValues(t *testing.T) {
	t.Parallel()

	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stripe-pp-cli sync --resources"},
			{"command":"stripe-pp-cli search \"acme.co\" --type"},
			{"command":"stripe-pp-cli search \"acme.co\" --select"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, "", Options{FrameworkOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.ExampleFailed != 3 {
		t.Fatalf("ExampleFailed = %d, want 3; results=%+v", report.ExampleFailed, report.Results)
	}
	for _, result := range report.Results {
		if !strings.Contains(result.Error, "requires --") || !strings.Contains(result.Error, "to have a value") {
			t.Errorf("Error %q should report a missing flag value", result.Error)
		}
	}
}

func TestValidateWithOptions_FrameworkOnlyAcceptsDocumentedFrameworkFlags(t *testing.T) {
	t.Parallel()

	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stripe-pp-cli --json sync --since 7d --resources customers,charges"},
			{"command":"stripe-pp-cli sync --since 7d --resources customers,charges --max-pages 1 --json"},
			{"command":"stripe-pp-cli search \"acme.co\" --type customers --limit 10 --json"},
			{"command":"stripe-pp-cli search \"acme.co\" --type \"--temporary-resource\" --json"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, "", Options{FrameworkOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.HasFailures() {
		t.Fatalf("documented framework flags should pass, got %+v", report.Results)
	}
	if report.Walked != 4 {
		t.Errorf("Walked = %d, want 4", report.Walked)
	}
}

func TestValidateWithOptions_FullExamplesSeedsTemplateVarEnv(t *testing.T) {
	unsetEnv(t, "SHOPIFY_SHOP")
	unsetEnv(t, "SHOPIFY_API_VERSION")

	binary := buildTemplateVarStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub sync --full"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.HasFailures() {
		t.Fatalf("full example should pass with manifest-declared template var placeholders, got %+v", report)
	}
	if report.Walked != 1 {
		t.Errorf("Walked = %d, want 1", report.Walked)
	}
}

func TestValidateWithOptions_FullExamplesSeedsTemplateVarEnvWhenParentEnvEmpty(t *testing.T) {
	t.Setenv("SHOPIFY_SHOP", "")
	t.Setenv("SHOPIFY_API_VERSION", " ")

	binary := buildTemplateVarStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub sync --full"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.HasFailures() {
		t.Fatalf("full example should replace blank inherited template env vars with placeholders, got %+v", report)
	}
}

func TestValidateWithOptions_FullExamplesPreservesTemplateVarParentEnv(t *testing.T) {
	t.Setenv("SHOPIFY_SHOP", "custom-shop")
	t.Setenv("SHOPIFY_API_VERSION", "2026-01")

	binary := buildTemplateVarStubBinary(t, map[string]string{
		"SHOPIFY_SHOP":        "custom-shop",
		"SHOPIFY_API_VERSION": "2026-01",
	})
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub sync --full"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.HasFailures() {
		t.Fatalf("full example should preserve non-empty inherited template env vars, got %+v", report)
	}
}

func TestValidateWithOptions_FullExamplesUsesTemplateVarEnvOverrides(t *testing.T) {
	unsetEnv(t, "ST_TENANT_ID")

	binary := buildTemplateVarOverrideStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"quickstart":[
			{"command":"stub sync --full"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.HasFailures() {
		t.Fatalf("full example should seed manifest override env names, got %+v", report)
	}
}

func TestClassifyFullExample_ReportsUnsupportedWhenDryRunUnavailable(t *testing.T) {
	t.Parallel()

	got := classifyFullExample(
		context.Background(),
		"/not/invoked",
		"stub widgets list",
		[]byte("Usage: stub widgets list"),
		Result{Section: SectionQuickstart, Command: "stub widgets list", Words: "widgets list"},
		nil,
	)
	if got.Status != StatusUnsupported {
		t.Fatalf("Status = %q, want %q", got.Status, StatusUnsupported)
	}
	if !strings.Contains(got.Error, "does not advertise --dry-run") {
		t.Errorf("Error %q should explain why the full example was not run", got.Error)
	}
}

func TestRunFullExample_SkipsAuthSetToken(t *testing.T) {
	t.Parallel()

	got := classifyFullExample(
		context.Background(),
		"/not/invoked",
		"stub auth set-token YOUR_TOKEN_HERE",
		[]byte("      --dry-run   Show request without sending"),
		Result{Section: SectionQuickstart, Command: "stub auth set-token YOUR_TOKEN_HERE", Words: "auth set-token YOUR_TOKEN_HERE"},
		nil,
	)
	if got.Status != StatusUnsupported {
		t.Fatalf("Status = %q, want %q", got.Status, StatusUnsupported)
	}
	if got.Error != "full-example validation skipped: command is side-effectful (auth/launch/apply)" {
		t.Errorf("Error = %q", got.Error)
	}
}

func TestRunFullExample_SkipsAuthLogout(t *testing.T) {
	t.Parallel()

	got := classifyFullExample(
		context.Background(),
		"/not/invoked",
		"stub auth logout",
		[]byte("      --dry-run   Show request without sending"),
		Result{Section: SectionRecipes, Command: "stub auth logout", Words: "auth logout"},
		nil,
	)
	if got.Status != StatusUnsupported {
		t.Fatalf("Status = %q, want %q", got.Status, StatusUnsupported)
	}
	if got.Error != "full-example validation skipped: command is side-effectful (auth/launch/apply)" {
		t.Errorf("Error = %q", got.Error)
	}
}

func TestIsSideEffectfulNarrativeExample_UsesExactFlagMatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "launch flag is side effectful",
			args: []string{"widgets", "create", "--launch"},
			want: true,
		},
		{
			name: "launch true flag is side effectful",
			args: []string{"widgets", "create", "--launch=true"},
			want: true,
		},
		{
			name: "launch substring flag is not side effectful",
			args: []string{"widgets", "create", "--launch-app"},
			want: false,
		},
		{
			name: "apply without dry run is side effectful",
			args: []string{"widgets", "create", "--apply"},
			want: true,
		},
		{
			name: "apply with dry run is not side effectful",
			args: []string{"widgets", "create", "--apply", "--dry-run"},
			want: false,
		},
		{
			name: "apply substring flag is not side effectful",
			args: []string{"widgets", "create", "--apply-template"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isSideEffectfulNarrativeExample(tc.args)
			if got != tc.want {
				t.Fatalf("isSideEffectfulNarrativeExample(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestValidate_EmptyResearchFlagsResearchEmpty covers the LLM-omitted-
// both-sections case.
func TestValidate_EmptyResearchFlagsResearchEmpty(t *testing.T) {
	t.Parallel()

	research := writeFile(t, `{"narrative":{}}`)
	binary := buildStubBinary(t)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ResearchEmpty {
		t.Error("ResearchEmpty should be true when both sections are empty")
	}
	if report.Walked != 0 || report.Missing != 0 {
		t.Errorf("expected no walked or missing entries, got walked=%d missing=%d", report.Walked, report.Missing)
	}
}

func TestSplitShellChain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		in       string
		segments []chainSegment
		wantErr  bool
	}{
		{"plain command", "stub widgets list", []chainSegment{{Text: "stub widgets list"}}, false},
		{"and-chain", "stub sync && stub list --within 60d", []chainSegment{{Text: "stub sync"}, {Text: "stub list --within 60d"}}, false},
		{"semicolon-chain", "stub sync ; stub list", []chainSegment{{Text: "stub sync"}, {Text: "stub list"}}, false},
		{"or-chain", "stub sync || stub list", []chainSegment{{Text: "stub sync"}, {Text: "stub list"}}, false},
		{"top-level pipe splits, tail is AfterPipe", "stub list | grep foo", []chainSegment{{Text: "stub list"}, {Text: "grep foo", AfterPipe: true}}, false},
		{"pipe chain with three commands marks all tails AfterPipe", "stub list | jq | head", []chainSegment{{Text: "stub list"}, {Text: "jq", AfterPipe: true}, {Text: "head", AfterPipe: true}}, false},
		{"and after pipe resets the pipeline", "stub list | jq && stub show 42", []chainSegment{{Text: "stub list"}, {Text: "jq", AfterPipe: true}, {Text: "stub show 42"}}, false},
		{"or after pipe resets the pipeline", "stub list | jq || stub show 42", []chainSegment{{Text: "stub list"}, {Text: "jq", AfterPipe: true}, {Text: "stub show 42"}}, false},
		{"semicolon after pipe resets the pipeline", "stub list | jq ; stub show 42", []chainSegment{{Text: "stub list"}, {Text: "jq", AfterPipe: true}, {Text: "stub show 42"}}, false},
		{"and inside double quotes", `stub run --msg "a && b"`, []chainSegment{{Text: `stub run --msg "a && b"`}}, false},
		{"semicolon inside single quotes", "stub run --msg 'a ; b'", []chainSegment{{Text: "stub run --msg 'a ; b'"}}, false},
		{"pipe inside quotes is not top-level", `stub run --msg "a | b"`, []chainSegment{{Text: `stub run --msg "a | b"`}}, false},
		{"empty trailing segment dropped", "stub sync &&", []chainSegment{{Text: "stub sync"}}, false},
		{"unclosed quote errors", `stub run --msg "open`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			segs, err := splitShellChain(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitShellChain(%q) = nil error, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitShellChain(%q) errored: %v", tc.in, err)
			}
			want := tc.segments
			if want == nil {
				want = []chainSegment{}
			}
			got := segs
			if got == nil {
				got = []chainSegment{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("segments = %+v, want %+v", got, want)
			}
		})
	}
}

func TestValidate_ChainedRecipePassesWhenBothHalvesResolve(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list && stub widgets show 42"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if report.Walked != 1 || report.HasFailures() {
		t.Fatalf("chained recipe should walk OK, got walked=%d failures=%v results=%+v", report.Walked, report.HasFailures(), report.Results)
	}
}

func TestValidate_ChainedRecipeFlagsBrokenRHS(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list && stub typo-here"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 1 {
		t.Fatalf("chained recipe with broken RHS should report missing, got %+v", report)
	}
	got := report.Results[0]
	if !strings.Contains(got.Error, "segment 2") {
		t.Errorf("error should attribute failure to segment 2: %s", got.Error)
	}
	if got.Command != "stub widgets list && stub typo-here" {
		t.Errorf("Result.Command should preserve the original recipe, got %q", got.Command)
	}
}

func TestValidateWithOptions_ChainedRecipeRunsBothFullExamples(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list && stub widgets show 42"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Walked != 1 || report.HasFailures() {
		t.Fatalf("chained full-example recipe should pass, got %+v", report)
	}
}

// TestValidate_TrailingOperatorDoesNotLeakIntoArgs covers the
// single-segment fast path: when splitShellChain trims a trailing `&&`,
// classify must hand the trimmed segment (not the original) to
// classifySegment so FullExamples mode doesn't pass `&&` as a positional
// arg to the binary.
func TestValidate_TrailingOperatorDoesNotLeakIntoArgs(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list &&"}
		]
	}}`)

	report, err := ValidateWithOptions(context.Background(), research, binary, Options{FullExamples: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Walked != 1 || report.HasFailures() {
		t.Fatalf("trailing && should be trimmed and the lone segment should pass, got %+v", report)
	}
}

// TestValidate_PipeRecipeValidatesLeadingSegment pins the issue #1455 contract:
// recipes that pipe their CLI output into jq/head/xargs are now validated by
// running the leading segment only and recording each pipe tail as a
// `pipe-skipped:` note. Trailing pipes no longer disqualify the whole recipe.
func TestValidate_PipeRecipeValidatesLeadingSegment(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list | jq '.items[]' | head -c 2000"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatalf("piped recipe should validate cleanly on the leading segment, got %+v", report)
	}
	if report.Walked != 1 {
		t.Fatalf("Walked = %d, want 1", report.Walked)
	}
	got := report.Results[0]
	if got.Status != StatusOK {
		t.Fatalf("Status = %q, want %q", got.Status, StatusOK)
	}
	wantNotes := []string{
		"pipe-skipped: jq '.items[]'",
		"pipe-skipped: head -c 2000",
	}
	if !reflect.DeepEqual(got.Notes, wantNotes) {
		t.Errorf("Notes = %q, want %q", got.Notes, wantNotes)
	}
}

// TestValidate_MixedRedirectAndPipeNotesAreLeftToRight pins the textual
// ordering of Result.Notes: a recipe whose leading segment owns a redirect
// and whose tail is piped should record the redirect-stripped note before
// the pipe-skipped note, matching the order tokens appear in the source.
func TestValidate_MixedRedirectAndPipeNotesAreLeftToRight(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list < keywords.txt | jq '.'"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatalf("mixed redirect+pipe recipe should validate, got %+v", report)
	}
	got := report.Results[0]
	wantNotes := []string{
		"redirect-stripped: < keywords.txt",
		"pipe-skipped: jq '.'",
	}
	if !reflect.DeepEqual(got.Notes, wantNotes) {
		t.Errorf("Notes = %q, want %q (left-to-right textual order)", got.Notes, wantNotes)
	}
}

// TestValidate_RedirectStripValidatesCleanedHead covers the second leg of
// issue #1455: a `<file` input redirect is excised from the leading segment
// before validation and recorded as a `redirect-stripped:` note.
func TestValidate_RedirectStripValidatesCleanedHead(t *testing.T) {
	t.Parallel()

	binary := buildStubBinary(t)
	research := writeFile(t, `{"narrative":{
		"recipes":[
			{"command":"stub widgets list < keywords.txt"}
		]
	}}`)

	report, err := Validate(context.Background(), research, binary)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatalf("recipe with input redirect should validate the stripped command, got %+v", report)
	}
	got := report.Results[0]
	if got.Status != StatusOK {
		t.Fatalf("Status = %q, want %q", got.Status, StatusOK)
	}
	if !slices.Contains(got.Notes, "redirect-stripped: < keywords.txt") {
		t.Errorf("Notes should record the stripped redirect, got %q", got.Notes)
	}
}

// writeFile writes content to a temp file and returns the path.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "research.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

// buildStubBinary compiles a small Go program that simulates a printed
// CLI: it accepts `widgets list --help`, `widgets show <id> --help`,
// and exits non-zero for anything else. The stub is the most direct
// way to test the exec path without depending on a fully generated CLI.
//
// The build is cached across tests via sync.Once — go build is the
// slowest step in the package's test runtime.
var (
	stubOnce sync.Once
	stubPath string
	stubErr  error
)

func buildStubBinary(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		src := `package main

import (
	"fmt"
	"os"
	"strings"
)

var validPathPrefixes = []string{
	"widgets list",
	"widgets show",
}

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--bad-flag" {
			fmt.Fprintln(os.Stderr, "unknown flag: --bad-flag")
			os.Exit(1)
		}
	}
	var path []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		path = append(path, a)
	}
	joined := strings.Join(path, " ")
	for _, prefix := range validPathPrefixes {
		if joined == prefix || strings.HasPrefix(joined, prefix+" ") {
			fmt.Println("usage stub:", prefix)
			fmt.Println("      --dry-run   Show request without sending")
			return
		}
	}
	fmt.Fprintln(os.Stderr, "unknown command:", joined)
	os.Exit(1)
}
`
		dir, err := os.MkdirTemp("", "narrativecheck-stub-")
		if err != nil {
			stubErr = err
			return
		}
		srcPath := filepath.Join(dir, "stub.go")
		if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
			stubErr = err
			return
		}
		stubPath = filepath.Join(dir, "stub")
		if out, err := exec.Command("go", "build", "-o", stubPath, srcPath).CombinedOutput(); err != nil {
			stubErr = fmt.Errorf("building stub: %v\n%s", err, out)
		}
	})
	if stubErr != nil {
		t.Fatal(stubErr)
	}
	return stubPath
}

func buildTemplateVarStubBinary(t *testing.T, wantEnv ...map[string]string) string {
	t.Helper()
	want := map[string]string{
		"SHOPIFY_SHOP":        "shop_placeholder",
		"SHOPIFY_API_VERSION": "api_version_placeholder",
	}
	if len(wantEnv) > 0 {
		want = wantEnv[0]
	}
	wantChecks := renderEnvChecks(want)
	src := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	joined := strings.Join(args, " ")
	if len(args) >= 2 && args[0] == "sync" && args[1] == "--help" {
		fmt.Println("usage stub: sync")
		fmt.Println("      --dry-run   Show request without sending")
		return
	}
	if joined == "sync --full --dry-run" {
		missing := false
` + wantChecks + `
		if missing {
			os.Exit(1)
		}
		fmt.Println("dry run ok")
		return
	}
	fmt.Fprintln(os.Stderr, "unknown command:", joined)
	os.Exit(1)
}
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "stub.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "stub")
	if out, err := exec.Command("go", "build", "-o", binaryPath, srcPath).CombinedOutput(); err != nil {
		t.Fatalf("building template-var stub: %v\n%s", err, out)
	}
	manifest := `{
		"api_name": "shopify",
		"cli_name": "shopify-pp-cli",
		"endpoint_template_vars": ["shop", "api_version"]
	}`
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return binaryPath
}

func buildTemplateVarOverrideStubBinary(t *testing.T) string {
	t.Helper()
	binaryPath := buildTemplateVarStubBinary(t, map[string]string{
		"ST_TENANT_ID": "tenant_placeholder",
	})
	manifest := `{
		"api_name": "servicetitan",
		"cli_name": "servicetitan-pp-cli",
		"endpoint_template_vars": ["tenant"],
		"endpoint_template_env_overrides": {"tenant": "ST_TENANT_ID"}
	}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(binaryPath), ".printing-press.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return binaryPath
}

func renderEnvChecks(want map[string]string) string {
	var b strings.Builder
	keys := make([]string, 0, len(want))
	for envName := range want {
		keys = append(keys, envName)
	}
	sort.Strings(keys)
	for _, envName := range keys {
		envValue := want[envName]
		fmt.Fprintf(&b, "\t\tif got := os.Getenv(%q); got != %q {\n", envName, envValue)
		fmt.Fprintf(&b, "\t\t\tfmt.Fprintf(os.Stderr, %q, got)\n", envName+" = %q\\n")
		b.WriteString("\t\t\tmissing = true\n")
		b.WriteString("\t\t}\n")
	}
	return b.String()
}

func TestStripRedirects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		in        string
		wantText  string
		wantPaths []string
	}{
		{
			name:     "no redirects",
			in:       "stub widgets list --json",
			wantText: "stub widgets list --json",
		},
		{
			name:      "trailing input redirect",
			in:        "stub bulk --stdin --json < keywords.txt",
			wantText:  "stub bulk --stdin --json",
			wantPaths: []string{"< keywords.txt"},
		},
		{
			name:      "trailing output redirect",
			in:        "stub export > out.json",
			wantText:  "stub export",
			wantPaths: []string{"> out.json"},
		},
		{
			name:      "append redirect",
			in:        "stub log >> session.log",
			wantText:  "stub log",
			wantPaths: []string{">> session.log"},
		},
		{
			name:      "leading flag is preserved when redirect interleaves",
			in:        "stub run < in.txt --json",
			wantText:  "stub run --json",
			wantPaths: []string{"< in.txt"},
		},
		{
			name:     "fd duplication is left alone",
			in:       "stub run 2>&1",
			wantText: "stub run 2>&1",
		},
		{
			name:      "bare >&file is a real redirect target (no digit prefix)",
			in:        "stub run >&combined.log",
			wantText:  "stub run",
			wantPaths: []string{"> &combined.log"},
		},
		{
			name:      "single-quoted filename with spaces stays whole",
			in:        "stub bulk --stdin < 'file with spaces.txt'",
			wantText:  "stub bulk --stdin",
			wantPaths: []string{"< 'file with spaces.txt'"},
		},
		{
			name:      "double-quoted filename with spaces stays whole",
			in:        `stub export > "out file.json"`,
			wantText:  "stub export",
			wantPaths: []string{`> "out file.json"`},
		},
		{
			name:     "redirect inside single quote preserved",
			in:       "stub run --msg 'a < b > c'",
			wantText: "stub run --msg 'a < b > c'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotText, gotPaths := stripRedirects(tc.in)
			if gotText != tc.wantText {
				t.Errorf("text = %q, want %q", gotText, tc.wantText)
			}
			if !reflect.DeepEqual(gotPaths, tc.wantPaths) {
				t.Errorf("paths = %q, want %q", gotPaths, tc.wantPaths)
			}
		})
	}
}
