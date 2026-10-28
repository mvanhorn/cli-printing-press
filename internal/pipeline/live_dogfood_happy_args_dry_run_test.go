package pipeline

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

	parsed := parseHappyArgsAnnotation("--region=nyc;--dry-run=false")
	assert.True(t, happyArgsMentionDryRun(parsed))
	assert.False(t, happyArgsEnableDryRun(parsed))
	assert.True(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run")))
	assert.True(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run=true")))
	assert.False(t, happyArgsEnableDryRun(parseHappyArgsAnnotation("--dry-run=maybe")))
	assert.False(t, happyArgsMentionDryRun(parseHappyArgsAnnotation("--region=nyc")))
}
