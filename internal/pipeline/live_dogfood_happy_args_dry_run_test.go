package pipeline

import (
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Example lines on generated novel commands often end in --dry-run. When the
// author sets pp:happy-args without that flag, the happy path must run for
// real. A --dry-run the author wrote, and the mutating dry-run path, stay.
func TestLiveDogfoodHappyArgsDropExampleDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName, argvLog := writeHappyArgsDryRunFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{
			{Command: "recheck"},
			{Command: "kept"},
			{Command: "bare"},
			{Command: "copied"},
			{Command: "quiet"},
			{Command: "shorthand"},
			{Command: "publish"},
			{Command: "purge"},
			{Command: "rename"},
			{Command: "archive"},
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

	recheck := findResultByCommandKind(report, "recheck", LiveDogfoodTestHappy)
	require.NotNil(t, recheck)
	assert.Equal(t, LiveDogfoodStatusPass, recheck.Status, recheck.Reason)
	assert.NotContains(t, recheck.Args, "--dry-run")
	assert.False(t, liveDogfoodArgsContainDryRun(recheck.Args), "argv: %v", recheck.Args)
	assert.NotContains(t, report.HollowFeatures, "recheck")

	kept := findResultByCommandKind(report, "kept", LiveDogfoodTestHappy)
	require.NotNil(t, kept)
	assert.Equal(t, LiveDogfoodStatusPass, kept.Status, kept.Reason)
	assert.True(t, liveDogfoodArgsContainDryRun(kept.Args), "argv: %v", kept.Args)
	assert.Contains(t, report.HollowFeatures, "kept")
	assert.Contains(t, report.HollowReasons["kept"], "pp:happy-args")
	assert.Contains(t, report.HollowReasons["kept"], strings.Join(kept.Args, " "))

	bare := findResultByCommandKind(report, "bare", LiveDogfoodTestHappy)
	require.NotNil(t, bare)
	assert.True(t, liveDogfoodArgsContainDryRun(bare.Args), "argv: %v", bare.Args)
	assert.Contains(t, report.HollowFeatures, "bare")
	assert.Contains(t, report.HollowReasons["bare"], "pp:happy-args")
	assert.Contains(t, report.HollowReasons["bare"], strings.Join(bare.Args, " "))

	fromExample := findResultByCommandKind(report, "copied", LiveDogfoodTestHappy)
	require.NotNil(t, fromExample)
	assert.Equal(t, LiveDogfoodStatusPass, fromExample.Status, fromExample.Reason)
	assert.True(t, liveDogfoodArgsContainDryRun(fromExample.Args), "argv: %v", fromExample.Args)
	assert.Contains(t, report.HollowFeatures, "copied")
	assert.Contains(t, report.HollowReasons["copied"], "Example")
	assert.Contains(t, report.HollowReasons["copied"], strings.Join(fromExample.Args, " "))
	assert.NotContains(t, report.HollowReasons["copied"], "pp:happy-args")

	off := findResultByCommandKind(report, "quiet", LiveDogfoodTestHappy)
	require.NotNil(t, off)
	assert.Equal(t, LiveDogfoodStatusPass, off.Status, off.Reason)
	assert.False(t, liveDogfoodEffectiveDryRun(off.Args, ""), "argv: %v", off.Args)
	assert.Contains(t, off.Args, "--dry-run=false")
	assert.NotContains(t, report.HollowFeatures, "quiet")

	short := findResultByCommandKind(report, "shorthand", LiveDogfoodTestHappy)
	require.NotNil(t, short)
	assert.Equal(t, LiveDogfoodStatusPass, short.Status, short.Reason)
	assert.NotContains(t, short.Args, "-n")
	assert.False(t, liveDogfoodArgsContainDryRun(short.Args), "argv: %v", short.Args)
	assert.NotContains(t, report.HollowFeatures, "shorthand")

	publish := findResultByCommandKind(report, "publish", LiveDogfoodTestHappy)
	require.NotNil(t, publish)
	assert.Equal(t, LiveDogfoodStatusPass, publish.Status, publish.Reason)
	assert.True(t, liveDogfoodArgsContainDryRun(publish.Args), "mutating dry-run must keep --dry-run: %v", publish.Args)
	assert.Contains(t, report.HollowFeatures, "publish")
	assert.Contains(t, report.HollowReasons["publish"], "mutating dry-run")
	assert.Contains(t, report.HollowReasons["publish"], strings.Join(publish.Args, " "))

	for _, name := range []string{"purge", "rename", "archive"} {
		got := findResultByCommandKind(report, name, LiveDogfoodTestHappy)
		require.NotNil(t, got, name)
		assert.Equal(t, LiveDogfoodStatusPass, got.Status, "%s: %s", name, got.Reason)
		assert.True(t, liveDogfoodArgsContainDryRun(got.Args), "%s must still dry-run: %v", name, got.Args)
		assert.NotContains(t, got.Args, "--dry-run=false", name)
		assert.NotContains(t, got.Args, "--dry-run=maybe", name)
	}

	for _, line := range liveHappyArgvLines(t, argvLog) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "publish", "purge", "rename", "archive":
			assert.True(t, liveDogfoodArgsContainDryRun(fields), "mutator ran without an enabled --dry-run: %s", line)
		}
	}
}

func writeHappyArgsDryRunFixture(t *testing.T) (dir, binaryName, argvLog string) {
	t.Helper()
	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	argvLog = filepathJoinArgv(t)
	script := `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"recheck","annotations":{"pp:method":"GET","mcp:read-only":"true","pp:happy-args":"--region=nyc"}},
  {"name":"kept","annotations":{"pp:method":"GET","mcp:read-only":"true","pp:happy-args":"--region=nyc;--dry-run=true"}},
  {"name":"bare","annotations":{"pp:method":"GET","mcp:read-only":"true","pp:happy-args":"--region=nyc;--dry-run"}},
  {"name":"copied","annotations":{"pp:method":"GET","mcp:read-only":"true"}},
  {"name":"quiet","annotations":{"pp:method":"GET","mcp:read-only":"true","pp:happy-args":"--region=nyc;--dry-run=false"}},
  {"name":"shorthand","annotations":{"pp:method":"GET","mcp:read-only":"true","pp:happy-args":"--region=nyc"}},
  {"name":"publish","annotations":{"pp:method":"POST","pp:happy-args":"--name=ada"}},
  {"name":"purge","annotations":{"pp:method":"POST"}},
  {"name":"rename","annotations":{"pp:method":"POST"}},
  {"name":"archive","annotations":{"pp:method":"POST"}}
]}
JSON
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  example="fixture-pp-cli $1 --region nyc --dry-run"
  drydecl="      --dry-run   Show request without sending"
  case "$1" in
    publish) example="fixture-pp-cli $1 --name ada" ;;
    purge) example="fixture-pp-cli $1 --dry-run=false" ;;
    rename) example="fixture-pp-cli $1 --dry-run=maybe" ;;
    archive) example="fixture-pp-cli $1 -- --dry-run" ;;
    shorthand)
      example="fixture-pp-cli $1 -n --region nyc"
      drydecl="  -n, --dry-run   Show request without sending"
      ;;
  esac
  cat <<HELP
Do the thing.

Usage:
  fixture-pp-cli $1 [flags]

Examples:
  $example

Flags:
      --region string   Region
      --name string     Name

Global Flags:
      --json      Output as JSON
  $drydecl
HELP
  exit 0
fi
printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
dry=0
stop=0
for a in "$@"; do
  if [ "$stop" = 1 ]; then
    break
  fi
  case "$a" in
    --) stop=1 ;;
    --dry-run|--dry-run=true|--dry-run=TRUE|--dry-run=True|--dry-run=1|--dry-run=t|--dry-run=T) dry=1 ;;
    --dry-run=false|--dry-run=FALSE|--dry-run=False|--dry-run=0|--dry-run=f|--dry-run=F) dry=0 ;;
  esac
done
if [ "$dry" = 1 ]; then
  echo '{"dry_run":true,"action":"preview"}'
  exit 0
fi
echo '{"ok":true}'
exit 0
`
	writeStubBinary(t, dir, binaryName, script)
	return dir, binaryName, argvLog
}

func filepathJoinArgv(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "argv.log")
}

func TestLiveDogfoodDryRunArgvEdges(t *testing.T) {
	t.Parallel()

	const help = `
Flags:
  -n, --dry-run         Show request without sending
  -v, --verbose         Verbose
  -o, --output string   Output file
      --region string   Region
`

	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=false"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=FALSE"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=0"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=f"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=maybe"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=true"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=TRUE"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run", "--dry-run=false"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--dry-run=false", "--dry-run"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--", "--dry-run"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "--output", "--dry-run"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-n"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-n=false"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-vn"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-o", "--dry-run"}, help), "-o consumes the next token")
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-oname"}, help))

	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--region", "nyc", "--dry-run"}, help))
	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--region", "nyc", "--dry-run=true"}, help))
	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--region", "nyc", "--dry-run=false"}, help))
	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--region", "nyc", "--dry-run=maybe"}, help))
	assert.Equal(t, []string{"cmd", "--output", "--dry-run"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--output", "--dry-run"}, help))
	assert.Equal(t, []string{"cmd", "--", "--dry-run"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--", "--dry-run"}, help))
	assert.Equal(t, []string{"cmd", "--dry-run-output", "preview.json"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "--dry-run-output", "preview.json"}, help))
	assert.Equal(t, []string{"cmd", "-v", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-vn", "--region", "nyc"}, help))
	assert.Equal(t, []string{"cmd", "-v"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-vn=false"}, help))
	assert.False(t, liveDogfoodEffectiveDryRun([]string{"cmd", "-vn=false"}, help))
	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-n", "--region", "nyc"}, help))
	assert.Equal(t, []string{"cmd", "--region", "nyc"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-n=false", "--region", "nyc"}, help))
	assert.Equal(t, []string{"cmd", "-o", "out.txt"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-no", "out.txt"}, help))
	assert.Equal(t, []string{"cmd", "-oname"}, stripLiveDogfoodDryRunFlags([]string{"cmd", "-oname"}, help))
	assert.Equal(t, []string{"render", "--prompt", "x", "--", "--dry-run"}, removeDryRunArgs([]string{"render", "--dry-run", "--prompt", "x", "--dry-run=true", "--", "--dry-run"}))

	for _, args := range [][]string{
		{"cmd"},
		{"cmd", "--dry-run=false"},
		{"cmd", "--dry-run=FALSE"},
		{"cmd", "--dry-run=0"},
		{"cmd", "--dry-run=maybe"},
		{"cmd", "-n=false"},
		{"cmd", "--", "--dry-run"},
		{"cmd", "--output", "--dry-run"},
		{"cmd", "-o", "--dry-run"},
	} {
		got := ensureLiveDogfoodDryRunArg(args, help)
		assert.True(t, liveDogfoodEffectiveDryRun(got, help), "ensure %v -> %v", args, got)
		assert.True(t, liveDogfoodArgsContainDryRun(got), "visible long form %v -> %v", args, got)
	}
	ensured := ensureLiveDogfoodDryRunArg([]string{"cmd", "--output", "--dry-run", "--", "--dry-run"}, help)
	assert.Equal(t, []string{"cmd", "--output", "--dry-run", "--dry-run", "--", "--dry-run"}, ensured)
	// -n is the dry-run shorthand here, so "--" is a real separator.
	assert.Equal(t, []string{"cmd", "--dry-run", "--"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-n", "--"}, help))
	assert.Equal(t, []string{"cmd", "-v", "--dry-run", "--"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-vn", "--"}, help))
	// -o takes the "--" value; inserting between them would make -o consume --dry-run.
	assert.Equal(t, []string{"cmd", "-o", "--", "--dry-run"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-o", "--"}, help))
	assert.Equal(t, []string{"cmd", "-o", "--", "--dry-run"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-no", "--"}, help))
	assert.Equal(t, []string{"cmd", "-on", "--dry-run", "--"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-on", "--"}, help))
	assert.Equal(t, []string{"cmd", "--dry-run", "-o"}, ensureLiveDogfoodDryRunArg([]string{"cmd", "-o"}, help))
	assert.True(t, liveDogfoodEffectiveDryRun(ensureLiveDogfoodDryRunArg([]string{"cmd", "-o"}, help), help))
	assert.True(t, liveDogfoodEffectiveDryRun(ensureLiveDogfoodDryRunArg([]string{"cmd", "-o", "--"}, help), help))

	parsed := parseHappyArgsAnnotation("--region=nyc;--dry-run=false")
	assert.True(t, happyArgsMentionDryRun(parsed))
	assert.False(t, happyArgsEnableDryRun(parsed))
	assert.True(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run")))
	assert.True(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run=true")))
	assert.False(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run=maybe")))
	assert.False(t, happyArgsMentionDryRun(parseHappyArgsAnnotation("--region=nyc")))
}

func TestEnsureLiveDogfoodDryRunArgHonorsFlagValues(t *testing.T) {
	t.Parallel()

	const help = `
Flags:
  -n, --name string     Name
  -o, --output string   Output file
  -v, --verbose         Verbose
      --dry-run         Show request without sending
`

	assert.Equal(t, []string{"publish", "--name", "--"}, stripLiveDogfoodDryRunFlags([]string{"publish", "--dry-run", "--name", "--"}, help))

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "long flag value double dash",
			args: []string{"publish", "--dry-run", "--name", "--"},
			want: []string{"publish", "--name", "--", "--dry-run"},
		},
		{
			name: "real separator still precedes positionals",
			args: []string{"publish", "--verbose", "--", "keep"},
			want: []string{"publish", "--verbose", "--dry-run", "--", "keep"},
		},
		{
			name: "bool flag does not consume separator",
			args: []string{"publish", "--verbose", "--"},
			want: []string{"publish", "--verbose", "--dry-run", "--"},
		},
		{
			name: "shorthand value double dash",
			args: []string{"publish", "-n", "--"},
			want: []string{"publish", "-n", "--", "--dry-run"},
		},
		{
			name: "bool shorthand does not consume separator",
			args: []string{"publish", "-v", "--"},
			want: []string{"publish", "-v", "--dry-run", "--"},
		},
		{
			name: "cluster value double dash",
			args: []string{"publish", "-vn", "--"},
			want: []string{"publish", "-vn", "--", "--dry-run"},
		},
		{
			name: "attached shorthand value leaves the separator",
			args: []string{"publish", "-nv", "--"},
			want: []string{"publish", "-nv", "--dry-run", "--"},
		},
		{
			name: "cluster attached double dash value",
			args: []string{"publish", "-vn--"},
			want: []string{"publish", "-vn--", "--dry-run"},
		},
		{
			name: "shorthand with separate value appends",
			args: []string{"publish", "-n", "ada"},
			want: []string{"publish", "-n", "ada", "--dry-run"},
		},
		{
			name: "equals double dash then disabled dry-run",
			args: []string{"publish", "--name=--", "--dry-run=false"},
			want: []string{"publish", "--name=--", "--dry-run"},
		},
		{
			name: "disabled then enabled dry-run then name value",
			args: []string{"publish", "--dry-run=false", "--dry-run=true", "--name", "--"},
			want: []string{"publish", "--name", "--", "--dry-run"},
		},
		{
			name: "disabled dry-run after a real separator stays positional",
			args: []string{"publish", "--verbose", "--", "--dry-run=false"},
			want: []string{"publish", "--verbose", "--dry-run", "--", "--dry-run=false"},
		},
		{
			name: "equals value double dash",
			args: []string{"publish", "--name=--"},
			want: []string{"publish", "--name=--", "--dry-run"},
		},
		{
			name: "shorthand equals value double dash",
			args: []string{"publish", "-n=--"},
			want: []string{"publish", "-n=--", "--dry-run"},
		},
		{
			name: "dangling long flag",
			args: []string{"publish", "--name"},
			want: []string{"publish", "--dry-run", "--name"},
		},
		{
			name: "valued long flag appends",
			args: []string{"publish", "--name", "ada"},
			want: []string{"publish", "--name", "ada", "--dry-run"},
		},
		{
			name: "dangling shorthand",
			args: []string{"publish", "-n"},
			want: []string{"publish", "--dry-run", "-n"},
		},
		{
			name: "dangling cluster",
			args: []string{"publish", "-vn"},
			want: []string{"publish", "--dry-run", "-vn"},
		},
		{
			name: "bool shorthand appends",
			args: []string{"publish", "-v"},
			want: []string{"publish", "-v", "--dry-run"},
		},
		{
			name: "repeated separator after consumed double dash",
			args: []string{"publish", "--name", "--", "--", "pos"},
			want: []string{"publish", "--name", "--", "--dry-run", "--", "pos"},
		},
		{
			name: "repeated separator without a value flag",
			args: []string{"publish", "--", "--"},
			want: []string{"publish", "--dry-run", "--", "--"},
		},
		{
			name: "disabled dry-run then name value double dash",
			args: []string{"publish", "--dry-run=false", "--name", "--"},
			want: []string{"publish", "--name", "--", "--dry-run"},
		},
		{
			name: "disabled dry-run after consumed double dash",
			args: []string{"publish", "--name", "--", "--dry-run=false"},
			want: []string{"publish", "--name", "--", "--dry-run"},
		},
		{
			name: "name value is dry-run false token",
			args: []string{"publish", "--name", "--dry-run=false"},
			want: []string{"publish", "--name", "--dry-run=false", "--dry-run"},
		},
		{
			name: "output value is dry-run token then name consumes separator",
			args: []string{"publish", "--output", "--dry-run", "--name", "--"},
			want: []string{"publish", "--output", "--dry-run", "--name", "--", "--dry-run"},
		},
		{
			name: "separator before a value-looking flag",
			args: []string{"publish", "--", "--name", "--"},
			want: []string{"publish", "--dry-run", "--", "--name", "--"},
		},
		{
			name: "dangling flag after a consumed double dash",
			args: []string{"publish", "--name", "--", "--dry-run=false", "--output"},
			want: []string{"publish", "--name", "--", "--dry-run", "--output"},
		},
		{
			name: "two value flags where the second is the first value",
			args: []string{"publish", "--output", "--name"},
			want: []string{"publish", "--output", "--name", "--dry-run"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ensureLiveDogfoodDryRunArg(tt.args, help)
			assert.Equal(t, tt.want, got)
			assert.True(t, liveDogfoodEffectiveDryRun(got, help), "argv %v", got)
			assert.True(t, pflagHonorsDryRun(got), "pflag argv %v", got)
		})
	}
}

// pflagHonorsDryRun parses argv the way a printed CLI does. args[0] is the
// command name. A missing value still counts when --dry-run was already set.
func pflagHonorsDryRun(args []string) bool {
	fs := pflag.NewFlagSet("publish", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "")
	_ = fs.StringP("name", "n", "", "")
	_ = fs.StringP("output", "o", "", "")
	_ = fs.BoolP("verbose", "v", false, "")
	_ = fs.Parse(args[1:])
	return *dry
}
