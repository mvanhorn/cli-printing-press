package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveDogfoodConfirmFlagIgnoresTypeWordsInDescriptions(t *testing.T) {
	t.Parallel()

	// "String" in -v's description must not make the boolean -v look
	// value-taking, or -vy would hide the -y that pflag sets.
	help := "Flags:\n  -v, --verbose        String diagnostics\n  -q, --query string   Text\n  -y, --yes            Apply\n"
	shorthands := liveDogfoodShorthandTypes(help)
	assert.Equal(t, map[byte]bool{'v': false, 'q': true, 'y': false}, shorthands)
	assert.Equal(t, "-vy", liveDogfoodConfirmFlag([]string{"tidy", "-vy"}, shorthands))
	assert.Equal(t, "", liveDogfoodConfirmFlag([]string{"tidy", "-qy"}, shorthands))
}

func TestParseLiveDogfoodFlagDecl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want liveDogfoodFlagDecl
		ok   bool
	}{
		{line: "  -v, --verbose        String diagnostics", want: liveDogfoodFlagDecl{shorthand: 'v', long: "verbose"}, ok: true},
		{line: "  -q, --query string   Text", want: liveDogfoodFlagDecl{shorthand: 'q', long: "query", takesValue: true}, ok: true},
		{line: "      --limit int   Max rows", want: liveDogfoodFlagDecl{long: "limit", takesValue: true}, ok: true},
		{line: "      --dry-run     Int preview only", want: liveDogfoodFlagDecl{long: "dry-run"}, ok: true},
		{line: "  -l int   Max rows", want: liveDogfoodFlagDecl{shorthand: 'l', takesValue: true}, ok: true},
		{line: "      --tags strings   Tags", want: liveDogfoodFlagDecl{long: "tags", takesValue: true}, ok: true},
		{line: "                     continued description --other string", ok: false},
		{line: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			got, ok := parseLiveDogfoodFlagDecl(tt.line)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}

	valueFlags := liveDogfoodFlagValueNames("Flags:\n      --dry-run     Int preview only\n      --limit int   Max rows\n")
	assert.Equal(t, map[string]struct{}{"limit": {}}, valueFlags)
}

func TestLiveDogfoodConfirmFlag(t *testing.T) {
	t.Parallel()

	// -q takes a string value, -v is boolean, -y is the confirm flag.
	help := "Flags:\n  -q, --query string   Query\n  -v, --verbose        Verbose\n  -y, --yes            Apply\n"
	shorthands := liveDogfoodShorthandTypes(help)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no confirm flag", args: []string{"tidy", "--limit", "5"}, want: ""},
		{name: "yes", args: []string{"tidy", "--yes"}, want: "--yes"},
		{name: "short yes", args: []string{"tidy", "-y"}, want: "-y"},
		{name: "confirm with value", args: []string{"tidy", "--confirm=true"}, want: "--confirm=true"},
		{name: "force", args: []string{"tidy", "--force"}, want: "--force"},
		{name: "execute", args: []string{"tidy", "--execute"}, want: "--execute"},
		{name: "apply", args: []string{"tidy", "--apply"}, want: "--apply"},
		{name: "send", args: []string{"tidy", "--send"}, want: "--send"},
		{name: "launch", args: []string{"tidy", "--launch"}, want: "--launch"},
		{name: "explicit false does not confirm", args: []string{"tidy", "--yes=false"}, want: ""},
		{name: "short explicit false does not confirm", args: []string{"tidy", "-y=false"}, want: ""},
		{name: "repeated shorthand cluster", args: []string{"tidy", "-yy"}, want: "-yy"},
		{name: "shorthand cluster ending in y", args: []string{"tidy", "-vy"}, want: "-vy"},
		{name: "shorthand cluster starting with y", args: []string{"tidy", "-yv"}, want: "-yv"},
		{name: "cluster with y set false", args: []string{"tidy", "-vy=false"}, want: ""},
		{name: "cluster without y", args: []string{"tidy", "-vq"}, want: ""},
		{name: "y inside an attached string value", args: []string{"tidy", "-qquery"}, want: ""},
		{name: "y as the value of a string shorthand", args: []string{"tidy", "-qy"}, want: ""},
		{name: "string shorthand mid-cluster takes the rest as its value", args: []string{"tidy", "-vqy"}, want: ""},
		{name: "unknown shorthand is treated as boolean", args: []string{"tidy", "-zy"}, want: "-zy"},
		{name: "y before a string shorthand confirms", args: []string{"tidy", "-yq", "x"}, want: "-yq"},
		{name: "after terminator is not a flag", args: []string{"tidy", "--", "--yes"}, want: ""},
		{name: "positional named like a flag value", args: []string{"tidy", "yes"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, liveDogfoodConfirmFlag(tt.args, shorthands))
		})
	}
}

// writePreviewHappyPathFixture builds a CLI whose mutators preview unless a
// confirm flag is passed. Every non-help invocation is logged with its
// working directory so tests can prove which runs were real and where.
func writePreviewHappyPathFixture(t *testing.T) (dir, binaryName, argvLog string) {
	t.Helper()
	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	argvLog = filepath.Join(t.TempDir(), "argv.log")
	script := `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"tidy","annotations":{"pp:preview-happy-path":"true"}},
  {"name":"purge","annotations":{"pp:method":"POST"}},
  {"name":"wipe","annotations":{"pp:preview-happy-path":"true","pp:happy-args":"--yes"}},
  {"name":"rotate-keys","annotations":{"pp:preview-happy-path":"true"}},
  {"name":"archive","annotations":{"pp:preview-happy-path":"true","pp:happy-args":"--cursor=example-value"}}
]}
JSON
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  cat <<HELP
Preview by default; --yes writes.

Usage:
  fixture-pp-cli $1 [flags]

Examples:
  fixture-pp-cli $1 --dry-run

Flags:
      --cursor string   Continuation cursor
      --yes             Apply the change

Global Flags:
      --dry-run   Show request without sending
      --json      Output as JSON
HELP
  exit 0
fi
printf '%s|%s\n' "$*" "$(pwd)" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
for a in "$@"; do
  case "$a" in
    --dry-run) echo '{"dry_run":true,"action":"preview"}'; exit 0 ;;
    --yes) echo '{"applied":true}' > applied.json ;;
  esac
done
echo '{"preview":true}' > preview.json
echo '{"preview":true,"changes":[]}'
exit 0
`
	writeStubBinary(t, dir, binaryName, script)
	return dir, binaryName, argvLog
}

type previewArgvLine struct {
	args string
	dir  string
}

func previewArgvLines(t *testing.T, path string) []previewArgvLine {
	t.Helper()
	var out []previewArgvLine
	for _, line := range liveHappyArgvLines(t, path) {
		args, dir, _ := strings.Cut(line, "|")
		out = append(out, previewArgvLine{args: args, dir: dir})
	}
	return out
}

func TestRunLiveDogfoodPreviewHappyPathRunsWithoutAllowDestructive(t *testing.T) {
	dir, binaryName, argvLog := writePreviewHappyPathFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{
			{Name: "Tidy", Command: "tidy"},
			{Name: "Purge", Command: "purge"},
		},
	}, researchDir))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:      dir,
		BinaryName:  binaryName,
		Level:       "full",
		Timeout:     2 * time.Second,
		ResearchDir: researchDir,
	})
	require.NoError(t, err)

	tidy := findResultByCommandKind(report, "tidy", LiveDogfoodTestHappy)
	require.NotNil(t, tidy)
	assert.Equal(t, LiveDogfoodStatusPass, tidy.Status, tidy.Reason)
	assert.NotContains(t, tidy.Args, "--dry-run", "a preview happy path must run without --dry-run")
	assert.NotContains(t, tidy.Args, "--yes")
	tidyJSON := findResultByCommandKind(report, "tidy", LiveDogfoodTestJSON)
	require.NotNil(t, tidyJSON)
	assert.Equal(t, LiveDogfoodStatusPass, tidyJSON.Status, tidyJSON.Reason)

	// The unannotated mutator keeps its dry-run happy path.
	purge := findResultByCommandKind(report, "purge", LiveDogfoodTestHappy)
	require.NotNil(t, purge)
	assert.Contains(t, purge.Args, "--dry-run")

	// A confirm flag in the preview args is refused before anything runs.
	wipe := findResultByCommandKind(report, "wipe", LiveDogfoodTestHappy)
	require.NotNil(t, wipe)
	assert.Equal(t, LiveDogfoodStatusFail, wipe.Status)
	assert.True(t, strings.HasPrefix(wipe.Reason, reasonPreviewHappyConfirmFlag), wipe.Reason)

	// The annotation never unlocks destructive-at-auth commands.
	rotate := findResultByCommandKind(report, "rotate-keys", LiveDogfoodTestHappy)
	require.NotNil(t, rotate)
	assert.Equal(t, LiveDogfoodStatusSkip, rotate.Status)
	assert.Equal(t, reasonDestructiveAtAuth, rotate.Reason)

	// A declared placeholder never reaches the API on a real preview run.
	archive := findResultByCommandKind(report, "archive", LiveDogfoodTestHappy)
	require.NotNil(t, archive)
	assert.Equal(t, LiveDogfoodStatusSkip, archive.Status)
	assert.Equal(t, reasonRequiredParamFixture, archive.Reason)

	realTidy := 0
	for _, line := range previewArgvLines(t, argvLog) {
		fields := strings.Fields(line.args)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "tidy":
			if !strings.Contains(line.args, "--dry-run") {
				realTidy++
				assert.NotEqual(t, dir, line.dir, "the preview run must use a scratch directory")
			}
		case "wipe", "rotate-keys", "archive":
			t.Errorf("%s must not run: %q", fields[0], line.args)
		case "purge":
			assert.Contains(t, line.args, "--dry-run", "an unannotated mutator must not run for real")
		}
	}
	assert.Equal(t, 1, realTidy, "the preview run happens once and JSON fidelity reuses it")
	for _, name := range []string{"preview.json", "applied.json"} {
		_, statErr := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(statErr), "%s must not land in the CLI directory", name)
	}

	// The passing preview clears hollow coverage; the dry-run-only mutator
	// stays hollow.
	assert.Equal(t, []string{"purge"}, report.HollowFeatures)
}
