package narrativecheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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

func TestClassifyFullExample_ReportsUnsupportedWhenDryRunUnavailable(t *testing.T) {
	t.Parallel()

	got := classifyFullExample(
		context.Background(),
		"/not/invoked",
		"stub widgets list",
		[]byte("Usage: stub widgets list"),
		Result{Section: SectionQuickstart, Command: "stub widgets list", Words: "widgets list"},
	)
	if got.Status != StatusUnsupported {
		t.Fatalf("Status = %q, want %q", got.Status, StatusUnsupported)
	}
	if !strings.Contains(got.Error, "does not advertise --dry-run") {
		t.Errorf("Error %q should explain why the full example was not run", got.Error)
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
