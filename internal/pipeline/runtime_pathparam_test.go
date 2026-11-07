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

// TestPathParamRequiredSentinel pins the coarse "is required" regex.
// Generator wording for path-param validators is `Error: <name> is
// required`, so the regex must catch that case-insensitively, and must
// NOT match Cobra's separate `required flag(s) "X" not set` error.
// Flag wording such as `--email is required` still matches here on
// purpose; pathParamRejectionIsPositionalBinding decides whether the
// subject is a registered flag.
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

func TestPathParamFlagsFromHelp_RegistersLocalAndGlobalFlags(t *testing.T) {
	help := `Invite a person.

Usage:
  cli people contacts invite <user-id> [--tags=<csv>] [flags]

Flags:
  -e, --email string   Email address
      --dry-run        Preview the invite

Global Flags:
      --config string   Config file

Examples:
  cli people contacts invite abc --region us
`
	flags := pathParamFlagsFromHelp(help)
	for _, name := range []string{"email", "dry-run", "config"} {
		_, ok := flags.long[name]
		assert.True(t, ok, "missing long flag %s", name)
	}
	_, ok := flags.short["e"]
	assert.True(t, ok)
	for _, name := range []string{"help", "region", "tags", "user-id"} {
		_, registered := flags.long[name]
		assert.False(t, registered, "%s is not a registered flag", name)
	}
}

func TestPathParamRejectionIsPositionalBinding(t *testing.T) {
	flags := pathParamFlagSet{
		long:  map[string]struct{}{"email": {}, "user-id": {}, "format": {}, "dry-run": {}},
		short: map[string]struct{}{"e": {}},
	}
	placeholders := []string{"user-id", "region", "tagid", "company_id", "email"}

	notBinding := []string{
		`Error: --email is required`,
		"error: --EMAIL is required",
		"Error: --email is required.",
		"Error: --email is required!",
		`Error: "--email" is required`,
		"Error: '--email' is required",
		"Error: `--email` is required",
		"Error: (--email) is required",
		"Error: --email=user@example.com is required",
		"Error: --email= is required",
		`Error: --email="user@example.com" is required`,
		`Error: "--email=user@example.com" is required`,
		"Error: -e is required",
		"Error: -E is required",
		"Error: -e=user@example.com is required",
		"Error: format is required",
		"Error: --user-id is required",
		"Error: --email is required\r\n",
		"Error: --format=json is required",
		"Error: --dry-run is required",
		`Error: "--dry-run" is required`,
		"Error: --email=user@example.com. is required",
		"Error: --email=user@example.com, is required",
		"ERROR: --EMAIL IS REQUIRED",
		"Error:\t--email is required",
		"Error: '-e' is required",
		`Error: --email="a b" is required`,
		"Error: --email='a b' is required",
		`{"error":"--email is required"}`,
		"at least one of --email, --format is required",
		`Error: required flag(s) "email" not set`,
		"unknown command",
		"connection refused",
		"Error: --email given but nothing else",
	}
	for _, output := range notBinding {
		assert.False(t, pathParamRejectionIsPositionalBinding(output, placeholders, flags), "should not be a positional-binding failure: %q", output)
	}

	binding := []string{
		"Error: user-id is required",
		"Error: User-Id is required",
		"Error: USER-ID IS REQUIRED",
		`Error: "user-id" is required`,
		"Error: user-id, is required",
		"<region> is required",
		"Error: <region> is required.",
		"Error: tagId is required",
		"COMPANY_ID is required",
		"Error: --email given but tagId is required",
		"passed --email=a@b.c but user-id is required",
		"Error: --email is required; user-id is required",
		"Error: --email is required\nError: user-id is required",
		"Error: user-id or --email is required",
		"Error: --email or user-id is required",
		"Error: <user-id> is required",
		"at least one of --email, --missing is required",
		"Error: --email, tagId is required",
		"Error: user-id is required\r\n",
		"Error: email is required",
		"Error: <email> is required",
		"is required",
		"Error: -z is required",
		"Error: --email-address is required",
		"Error: --e-mail is required",
	}
	for _, output := range binding {
		assert.True(t, pathParamRejectionIsPositionalBinding(output, placeholders, flags), "should be a positional-binding failure: %q", output)
	}

	// A dashed token counts only when that flag is actually registered.
	// email is not registered on this command, so the SKILL wording still fails.
	unregistered := pathParamFlagSet{long: map[string]struct{}{}, short: map[string]struct{}{}}
	assert.True(t, pathParamRejectionIsPositionalBinding("Error: --email is required", []string{"user-id"}, unregistered))
	assert.True(t, pathParamRejectionIsPositionalBinding("Error: tagId is required", nil, pathParamFlagSet{}))
	assert.True(t, pathParamRejectionIsPositionalBinding("Error: --user-id is required", []string{"user-id"}, pathParamFlagSet{
		long:  map[string]struct{}{"email": {}},
		short: map[string]struct{}{},
	}))
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

	probes := discoverPathParamProbes(binary, nil)
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
	assert.Nil(t, discoverPathParamProbes("", nil))
}

// TestDiscoverPathParamProbes_SkipsParentGroupsWithPlaceholders pins the
// leaf-only contract: a parent/group command whose Usage line happens to
// carry a <placeholder> token (e.g. `cli zones <zone-id> [command]`)
// must NOT be probed. Probing it would invoke the synthetic value as a
// subcommand, producing an "unknown command" exit that silently passes
// the sentinel check and inflates Total / Passed without testing
// anything meaningful.
func TestDiscoverPathParamProbes_SkipsParentGroupsWithPlaceholders(t *testing.T) {
	binary := buildParentGroupWithPlaceholderFixture(t)
	probes := discoverPathParamProbes(binary, nil)
	paths := make([]string, 0, len(probes))
	for _, p := range probes {
		paths = append(paths, strings.Join(p.path, " "))
	}
	assert.NotContains(t, paths, "zones",
		"depth-1 leaves are skipped, but this would have caught a depth-1 'parent' bug regardless")
	assert.NotContains(t, paths, "zones records",
		"parent group with <zone-id> in Usage must not be probed; only true leaves qualify")
	assert.Contains(t, paths, "zones records list",
		"the true leaf at depth 3 should still be probed")
}

func buildParentGroupWithPlaceholderFixture(t *testing.T) string {
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
	joined := strings.Join(args, " ")
	switch {
	case joined == "" || joined == "--help":
		fmt.Println(`+"`"+`Test

Usage:
  cli [command]

Available Commands:
  zones  Manage zones (this group carries a placeholder in Usage)
`+"`"+`)
	case joined == "zones --help":
		// Parent group whose Usage carries a placeholder. Listing
		// children means it is NOT a leaf — the probe must skip it.
		fmt.Println(`+"`"+`Manage zones

Usage:
  cli zones <zone-id> [command]

Available Commands:
  records  Manage records under a zone
`+"`"+`)
	case joined == "zones records --help":
		// Same: parent group, listed children, has placeholder.
		fmt.Println(`+"`"+`Manage records

Usage:
  cli zones records <zone-id> [command]

Available Commands:
  list  List records
`+"`"+`)
	case joined == "zones records list --help":
		// True leaf at depth 3 — should be probed.
		fmt.Println(`+"`"+`List records

Usage:
  cli zones records list <zone-id> [flags]

Flags:
  -h, --help help
`+"`"+`)
	}
}
`)
	binaryPath := filepath.Join(dir, "cli")
	out, err := exec.Command("go", "build", "-o", binaryPath, mainFile).CombinedOutput()
	require.NoError(t, err, "build fixture: %s", string(out))
	return binaryPath
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
	assert.NotEmpty(t, broken.Positionals, "Positionals should capture the synthetic values injected for the placeholder")

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

// TestRunPathParamProbes_RegisteredFlagRequiredIsNotBinding runs the probe
// against a CLI whose help actually registers --email. Reverting the
// classifier to "any is required" fails invite. Treating every leading
// "--" as a flag, or dropping hyphenated names, fails link and bind.
func TestRunPathParamProbes_RegisteredFlagRequiredIsNotBinding(t *testing.T) {
	binary := buildFlagRequiredProbeFixture(t)

	results := runPathParamProbes(binary, subprocessEnv(), nil)
	require.Len(t, results, 4)

	byCommand := map[string]PathParamProbeResult{}
	for _, result := range results {
		byCommand[result.Command] = result
	}

	invite := byCommand["people contacts invite"]
	assert.True(t, invite.Passed, "registered --email is required is a flag check, not a positional-binding failure")
	assert.Empty(t, invite.Reason)

	bind := byCommand["people contacts bind"]
	assert.False(t, bind.Passed, "hyphenated positional user-id is required must still fail")
	assert.NotEmpty(t, bind.Reason)

	tag := byCommand["people contacts tag"]
	assert.False(t, tag.Passed, "a flag mentioned earlier does not excuse tagId is required")
	assert.NotEmpty(t, tag.Reason)

	link := byCommand["people contacts link"]
	assert.False(t, link.Passed, "an unregistered --user-id is required is still a positional-binding failure")
	assert.NotEmpty(t, link.Reason)
}

func buildFlagRequiredProbeFixture(t *testing.T) string {
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
	joined := strings.Join(os.Args[1:], " ")
	switch {
	case joined == "" || joined == "--help":
		fmt.Print("Test\n\nUsage:\n  test-cli [command]\n\nAvailable Commands:\n  people  People commands\n")
	case joined == "people --help":
		fmt.Print("People\n\nUsage:\n  test-cli people [command]\n\nAvailable Commands:\n  contacts  Contact commands\n")
	case joined == "people contacts --help":
		fmt.Print("Contacts\n\nUsage:\n  test-cli people contacts [command]\n\nAvailable Commands:\n  invite  Invite\n  bind    Bind\n  tag     Tag\n  link    Link\n")
	case joined == "people contacts invite --help":
		fmt.Print("Invite\n\nUsage:\n  test-cli people contacts invite <user-id> [flags]\n\nFlags:\n  -e, --email string   Email\n")
	case joined == "people contacts bind --help":
		fmt.Print("Bind\n\nUsage:\n  test-cli people contacts bind <user-id> [flags]\n\nFlags:\n      --email string   Email\n")
	case joined == "people contacts tag --help":
		fmt.Print("Tag\n\nUsage:\n  test-cli people contacts tag <tagId> [flags]\n\nFlags:\n      --email string   Email\n")
	case joined == "people contacts link --help":
		fmt.Print("Link\n\nUsage:\n  test-cli people contacts link <user-id> [flags]\n\nFlags:\n      --email string   Email\n")
	case strings.HasPrefix(joined, "people contacts invite "):
		fmt.Fprintln(os.Stderr, "Error: --email is required")
		os.Exit(2)
	case strings.HasPrefix(joined, "people contacts bind "):
		fmt.Fprintln(os.Stderr, "Error: user-id is required")
		os.Exit(1)
	case strings.HasPrefix(joined, "people contacts tag "):
		fmt.Fprintln(os.Stderr, "Error: --email given but tagId is required")
		os.Exit(1)
	case strings.HasPrefix(joined, "people contacts link "):
		fmt.Fprintln(os.Stderr, "Error: --user-id is required")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown:", joined)
		os.Exit(2)
	}
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	out, err := exec.Command("go", "build", "-o", binaryPath, mainFile).CombinedOutput()
	require.NoError(t, err, "building flag-required fixture: %s", string(out))
	return binaryPath
}
