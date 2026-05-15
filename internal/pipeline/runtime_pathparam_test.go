package pipeline

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPositionalsFromUsage(t *testing.T) {
	tests := []struct {
		name     string
		helpOut  string
		expected []string
	}{
		{
			name: "leaf with one placeholder",
			helpOut: `Foo command

Usage:
  cli-name foo bar baz <id> [flags]

Flags:
  -h, --help   help`,
			expected: []string{"id"},
		},
		{
			name: "leaf with two placeholders",
			helpOut: `Compare two items.

Usage:
  cli-name items compare <owner> <repo> [flags]
`,
			expected: []string{"owner", "repo"},
		},
		{
			name: "parent group (no placeholders)",
			helpOut: `Group of foo commands

Usage:
  cli-name foo [command]

Available Commands:
  bar  Bar things
`,
			expected: nil,
		},
		{
			name: "leaf flag descriptors stripped",
			helpOut: `Tag a thing.

Usage:
  cli-name save <url> [--tags=<csv>] [flags]
`,
			expected: []string{"url"},
		},
		{
			name:     "no usage block",
			helpOut:  "Just some text without a Usage section",
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPositionalsFromUsage(tt.helpOut)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestPathParamRequiredSentinel pins the regex that flags "is required"
// usage errors in command output. Generator wording for path-param
// validators is `Error: <name> is required`, so the regex must catch
// that case-insensitively, and must NOT match Cobra's separate
// `required flag(s) "X" not set` error (which is a flag bug, not the
// positional-binding bug this probe targets).
func TestPathParamRequiredSentinel(t *testing.T) {
	matches := []string{
		"Error: tagId is required",
		"error: id is required\n",
		"<region> is required",
		"Error: COMPANY_ID is required",
	}
	for _, m := range matches {
		assert.True(t, pathParamRequiredSentinelRe.MatchString(m), "should match: %q", m)
	}

	doesNotMatch := []string{
		`Error: required flag(s) "owner" not set`,
		"unknown command",
		"connection refused",
	}
	for _, m := range doesNotMatch {
		assert.False(t, pathParamRequiredSentinelRe.MatchString(m), "should not match: %q", m)
	}
}

// buildPathParamProbeFixture builds a tiny CLI binary that simulates a
// nested cobra tree with three leaf commands plus a top-level leaf:
//
//   - `tags contacts list <tagId>`: broken. Prints `Error: tagId is
//     required` and exits 1 no matter what positionals the caller
//     supplies. This is the failure shape the probe targets.
//   - `tags contacts get <id>`: working. Accepts a positional and
//     prints a fake URL on `--dry-run`.
//   - `tags contacts ping <id>`: unrelated failure. Exits 1 with
//     `connection refused` regardless of positionals. The probe must
//     NOT flag this as a path-param failure (false-positive guard).
//   - `recipe <url>`: top-level placeholder-bearing leaf, deliberately
//     broken in the same shape as `list`. The probe must skip it
//     because depth-1 leaves are already exercised by the existing
//     matrix via inferPositionalArgs.
func buildPathParamProbeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		// root --help equivalent
		printRootHelp()
		return
	}
	joined := strings.Join(args, " ")

	switch {
	case joined == "--help":
		printRootHelp()
	case joined == "tags --help":
		printTagsHelp()
	case joined == "tags contacts --help":
		printTagsContactsHelp()
	case joined == "tags contacts list --help":
		printListLeafHelp()
	case joined == "tags contacts get --help":
		printGetLeafHelp()
	case joined == "tags contacts ping --help":
		printPingLeafHelp()
	case joined == "recipe --help":
		printRecipeLeafHelp()
	case strings.HasPrefix(joined, "tags contacts list "):
		fmt.Fprintln(os.Stderr, "Error: tagId is required")
		os.Exit(1)
	case strings.HasPrefix(joined, "tags contacts get "):
		fmt.Println("DRY-RUN: GET /v1/tags/contacts/" + lastArg(args, "--dry-run"))
	case strings.HasPrefix(joined, "tags contacts ping "):
		fmt.Fprintln(os.Stderr, "Error: connection refused")
		os.Exit(1)
	case strings.HasPrefix(joined, "recipe "):
		fmt.Fprintln(os.Stderr, "Error: url is required")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown:", joined)
		os.Exit(2)
	}
}

func lastArg(args []string, ignore string) string {
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] != ignore {
			return args[i]
		}
	}
	return ""
}

func printRootHelp() {
	fmt.Println(`+"`"+`Test CLI

Usage:
  test-cli [command]

Available Commands:
  tags    Manage tags
  recipe  Fetch a recipe
`+"`"+`)
}

func printTagsHelp() {
	fmt.Println(`+"`"+`Manage tags

Usage:
  test-cli tags [command]

Available Commands:
  contacts  Tag-scoped contact operations
`+"`"+`)
}

func printTagsContactsHelp() {
	fmt.Println(`+"`"+`Tag-scoped contact operations

Usage:
  test-cli tags contacts [command]

Available Commands:
  list  List contacts for a tag
  get   Get a single contact
  ping  Ping (simulates an unrelated failure)
`+"`"+`)
}

func printListLeafHelp() {
	fmt.Println(`+"`"+`List contacts for a tag

Usage:
  test-cli tags contacts list <tagId> [flags]

Flags:
  -h, --help   help
`+"`"+`)
}

func printGetLeafHelp() {
	fmt.Println(`+"`"+`Get a single contact

Usage:
  test-cli tags contacts get <id> [flags]

Flags:
  -h, --help   help
`+"`"+`)
}

func printPingLeafHelp() {
	fmt.Println(`+"`"+`Ping (simulates an unrelated failure)

Usage:
  test-cli tags contacts ping <id> [flags]

Flags:
  -h, --help   help
`+"`"+`)
}

func printRecipeLeafHelp() {
	fmt.Println(`+"`"+`Fetch a recipe (top-level leaf at depth 1)

Usage:
  test-cli recipe <url> [flags]

Flags:
  -h, --help   help
`+"`"+`)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	out, err := exec.Command("go", "build", "-o", binaryPath, mainFile).CombinedOutput()
	require.NoError(t, err, "building probe fixture: %s", string(out))
	return binaryPath
}

func TestDiscoverPathParamProbes_WalksNestedTree(t *testing.T) {
	binary := buildPathParamProbeFixture(t)

	probes := discoverPathParamProbes(binary)
	require.Len(t, probes, 3, "should find the 3 nested leaves at depth 3 and skip the depth-1 'recipe' leaf")

	paths := make([]string, 0, len(probes))
	for _, p := range probes {
		paths = append(paths, strings.Join(p.path, " "))
	}
	assert.Contains(t, paths, "tags contacts list")
	assert.Contains(t, paths, "tags contacts get")
	assert.Contains(t, paths, "tags contacts ping")
	assert.NotContains(t, paths, "recipe",
		"depth-1 leaves are exercised by the existing matrix via inferPositionalArgs; the probe must not double-count them")
}

func TestDiscoverPathParamProbes_EmptyBinaryReturnsNil(t *testing.T) {
	assert.Nil(t, discoverPathParamProbes(""))
}

func TestRunPathParamProbes_DistinguishesFailureModes(t *testing.T) {
	binary := buildPathParamProbeFixture(t)

	results := runPathParamProbes(binary, subprocessEnv(), nil)
	require.Len(t, results, 3)

	byCommand := map[string]PathParamProbeResult{}
	for _, r := range results {
		byCommand[r.Command] = r
	}

	broken := byCommand["tags contacts list"]
	assert.False(t, broken.Passed, "leaf that returns 'is required' is the bug shape; must be flagged")
	assert.NotEmpty(t, broken.Reason)
	assert.Contains(t, broken.Args, "--dry-run")

	working := byCommand["tags contacts get"]
	assert.True(t, working.Passed, "leaf that accepts the positional must pass")
	assert.Empty(t, working.Reason)

	unrelated := byCommand["tags contacts ping"]
	assert.True(t, unrelated.Passed,
		"leaf that fails for an unrelated reason ('connection refused') must NOT be flagged; that's a different check's bug")
	assert.Empty(t, unrelated.Reason)
}

// TestFinalizeVerifyReport_PathParamProbeFailureFlipsVerdict pins the
// scoring rule: a failed path-param probe is a critical failure, which
// blocks the PASS verdict per finalizeVerifyReport's `Critical == 0`
// gate.
func TestFinalizeVerifyReport_PathParamProbeFailureFlipsVerdict(t *testing.T) {
	report := &VerifyReport{
		Results: []CommandResult{
			{Command: "version", Score: 3, Help: true, DryRun: true, Execute: true},
		},
		PathParamProbes: []PathParamProbeResult{
			{Command: "tags contacts list", Passed: false, Reason: "is required"},
			{Command: "tags contacts get", Passed: true},
		},
		DataPipeline: true,
	}
	finalizeVerifyReport(report, 80, true)

	assert.Equal(t, 3, report.Total)
	assert.Equal(t, 2, report.Passed)
	assert.Equal(t, 1, report.Failed)
	assert.Equal(t, 1, report.Critical)
	assert.NotEqual(t, "PASS", report.Verdict, "critical probe failure should block PASS")
}

func TestFinalizeVerifyReport_AllPathParamProbesPassingKeepsVerdict(t *testing.T) {
	report := &VerifyReport{
		Results: []CommandResult{
			{Command: "version", Score: 3, Help: true, DryRun: true, Execute: true},
		},
		PathParamProbes: []PathParamProbeResult{
			{Command: "tags contacts get", Passed: true},
		},
		DataPipeline: true,
	}
	finalizeVerifyReport(report, 80, true)

	assert.Equal(t, 2, report.Total)
	assert.Equal(t, 2, report.Passed)
	assert.Equal(t, 0, report.Critical)
	assert.Equal(t, "PASS", report.Verdict)
}
