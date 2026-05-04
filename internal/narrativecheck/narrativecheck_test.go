package narrativecheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
