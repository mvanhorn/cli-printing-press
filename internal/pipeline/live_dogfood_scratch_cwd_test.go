package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveDogfoodNewCLIPaths(t *testing.T) {
	t.Parallel()

	before := map[string]struct{}{
		"main.go":     {},
		"cmd/root.go": {},
	}
	after := map[string]struct{}{
		"main.go":       {},
		"cmd/root.go":   {},
		"photos.json":   {},
		"out/plan.json": {},
	}
	assert.Equal(t, []string{"out/plan.json", "photos.json"}, liveDogfoodNewCLIPaths(before, after))
	assert.Empty(t, liveDogfoodNewCLIPaths(before, before))
}

func TestLiveDogfoodStrayCLIFileReasonCapsList(t *testing.T) {
	t.Parallel()

	paths := make([]string, 21)
	for i := range paths {
		paths[i] = fmt.Sprintf("f%02d.txt", i)
	}
	reason := liveDogfoodStrayCLIFileReason(paths)
	assert.True(t, strings.HasPrefix(reason, reasonStrayCLIFiles+": "))
	assert.Contains(t, reason, "f00.txt")
	assert.Contains(t, reason, "f19.txt")
	assert.NotContains(t, reason, "f20.txt")
	assert.Contains(t, reason, "(+1 more)")
}

func TestOmitUnshippableCLIPathsKeepsPublishableIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build", "plan.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cache.log"), []byte("log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "probe.exe"), []byte("not-a-binary"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "probe.bin"), []byte{0x7f, 'E', 'L', 'F'}, 0o755))
	stage := ".printing-press-live-check-abc"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, stage), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, stage, "note.json"), []byte("{}\n"), 0o644))

	got := omitUnshippableCLIPaths(dir, []string{
		"build",
		"build/plan.json",
		"cache.log",
		"probe.exe",
		"probe.bin",
		stage,
		filepath.Join(stage, "note.json"),
		"photos.json",
	})
	assert.Equal(t, []string{"build", "build/plan.json", "cache.log", "photos.json"}, got)
}

func TestApplyLiveDogfoodSetupFailureIsNotAProbePass(t *testing.T) {
	run := liveDogfoodRun{
		exitCode:     -1,
		err:          errors.New("create live dogfood scratch dir: boom"),
		setupFailure: true,
	}

	errorResult := applyLiveDogfoodErrorPathVerdict(
		liveDogfoodResult("items get", LiveDogfoodTestError, []string{"items", "get", "__printing_press_invalid__"}, run, ""),
		run,
		false,
		false,
	)
	assert.Equal(t, LiveDogfoodStatusFail, errorResult.Status)
	assert.Equal(t, "create live dogfood scratch dir: boom", errorResult.Reason)

	searchResult := applyLiveDogfoodErrorPathVerdict(
		liveDogfoodResult("items search", LiveDogfoodTestError, nil, run, ""),
		run,
		true,
		true,
	)
	assert.Equal(t, LiveDogfoodStatusFail, searchResult.Status)
	assert.Equal(t, "create live dogfood scratch dir: boom", searchResult.Reason)

	dryResult := applyLiveDogfoodDryRunJSONVerdict(
		liveDogfoodResult("organize", LiveDogfoodTestDryRunJSON, []string{"organize", "--dry-run"}, run, ""),
		run,
		false,
	)
	assert.Equal(t, LiveDogfoodStatusFail, dryResult.Status)
	assert.Equal(t, "create live dogfood scratch dir: boom", dryResult.Reason)

	realError := applyLiveDogfoodErrorPathVerdict(
		liveDogfoodResult("items get", LiveDogfoodTestError, nil, liveDogfoodRun{exitCode: 1}, ""),
		liveDogfoodRun{exitCode: 1},
		false,
		false,
	)
	assert.Equal(t, LiveDogfoodStatusPass, realError.Status)
	assert.Empty(t, realError.Reason)
}

func TestRunLiveDogfoodFileWritesDoNotLandInCLIDir(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "album.txt"), []byte("album\n"), 0o644))
	cwdLog := filepath.Join(t.TempDir(), "cwd.log")
	t.Setenv("PRINTING_PRESS_TEST_CWD_LOG", cwdLog)
	writeStubBinary(t, dir, binaryName, `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"sync"},
  {"name":"organize","annotations":{"mcp:read-only":"true"}},
  {"name":"items","subcommands":[
    {"name":"list","annotations":{"pp:method":"GET","mcp:read-only":"true"}},
    {"name":"get","annotations":{"pp:method":"GET","mcp:read-only":"true"}}
  ]}
]}
JSON
  exit 0
fi
printf '%s\n' "$(pwd)" >> "$PRINTING_PRESS_TEST_CWD_LOG"
echo side > cwd-write.txt
if [ "${2:-}" = "--help" ] || [ "${3:-}" = "--help" ]; then
  case "$1" in
    organize)
      cat <<'HELP'
Organize a folder.

Usage:
  fixture-pp-cli organize [flags]

Examples:
  fixture-pp-cli organize --plan photos.json --input album.txt

Flags:
      --input string   Source notes
      --plan string    Plan to write

Global Flags:
      --json   Output as JSON
HELP
      ;;
    sync)
      cat <<'HELP'
Sync local state.

Usage:
  fixture-pp-cli sync [flags]

Examples:
  fixture-pp-cli sync

Global Flags:
      --json   Output as JSON
HELP
      ;;
    items)
      case "$2" in
        list)
          cat <<'HELP'
List items.

Usage:
  fixture-pp-cli items list [flags]

Examples:
  fixture-pp-cli items list

Global Flags:
      --json   Output as JSON
HELP
          ;;
        get)
          cat <<'HELP'
Get one item.

Usage:
  fixture-pp-cli items get <id> [flags]

Examples:
  fixture-pp-cli items get item-placeholder

Global Flags:
      --json   Output as JSON
HELP
          ;;
      esac
      ;;
  esac
  exit 0
fi
case "$1" in
  sync)
    echo synced > sync-out.txt
    exit 0
    ;;
  organize)
    if [ ! -f album.txt ]; then
      echo "missing fixture album.txt" >&2
      exit 1
    fi
    printf 'changed\n' > album.txt
    echo '{"ok":true}' > photos.json
    echo '{"ok":true}'
    exit 0
    ;;
  items)
    echo "$2" > "items-$2.txt"
    if [ "${3:-}" = "__printing_press_invalid__" ]; then
      echo invalid >&2
      exit 1
    fi
    if [ "$2" = "list" ]; then
      echo '{"results":[{"id":"item-1"}]}'
      exit 0
    fi
    echo '{"id":"item-1"}'
    exit 0
    ;;
esac
echo "unexpected args: $*" >&2
exit 99
`)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	organize := findResultByCommandKind(report, "organize", LiveDogfoodTestHappy)
	require.NotNil(t, organize)
	assert.Equal(t, LiveDogfoodStatusPass, organize.Status, organize.Reason)
	assert.Contains(t, organize.Args, "photos.json")
	assert.Contains(t, organize.Args, "album.txt")

	gotItem := findResultByCommandKind(report, "items get", LiveDogfoodTestHappy)
	require.NotNil(t, gotItem)
	assert.Equal(t, LiveDogfoodStatusPass, gotItem.Status, gotItem.Reason)

	listed := findResultByCommandKind(report, "items list", LiveDogfoodTestHappy)
	require.NotNil(t, listed)
	assert.Equal(t, LiveDogfoodStatusPass, listed.Status, listed.Reason)

	assert.Equal(t, "PASS", report.Verdict, "a clean matrix must not report stray CLI files")
	for _, name := range []string{"photos.json", "cwd-write.txt", "sync-out.txt", "items-list.txt", "items-get.txt"} {
		_, statErr := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(statErr), "%s must not land in the CLI directory", name)
	}
	body, err := os.ReadFile(filepath.Join(dir, "album.txt"))
	require.NoError(t, err)
	assert.Equal(t, "album\n", string(body), "overwriting a copied fixture must not touch the CLI tree")

	raw, err := os.ReadFile(cwdLog)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		cwd := strings.TrimSpace(line)
		require.NotEmpty(t, cwd)
		assert.NotEqual(t, dir, cwd, "matrix subprocess cwd must not be the CLI directory")
		rel, relErr := filepath.Rel(dir, cwd)
		if relErr == nil {
			outside := rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
			assert.True(t, outside, "cwd %s is inside the CLI directory", cwd)
		}
		_, statErr := os.Stat(cwd)
		assert.True(t, os.IsNotExist(statErr), "scratch dir %s was not cleaned up", cwd)
	}
}

func TestRunLiveDogfoodReportsFileWrittenIntoCLIDir(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	writeStubBinary(t, dir, binaryName, `set -u
if [ "$1" = "agent-context" ]; then
  echo '{"commands":[{"name":"organize","annotations":{"mcp:read-only":"true"}}]}'
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Organize a folder.

Usage:
  fixture-pp-cli organize [flags]

Examples:
  fixture-pp-cli organize --plan photos.json

Flags:
      --plan string   Plan to write

Global Flags:
      --json   Output as JSON
HELP
  exit 0
fi
echo '{"ok":true}' > photos.json
echo leaked > "$(dirname "$0")/leaked-plan.json"
echo '{"ok":true}'
exit 0
`)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	organize := findResultByCommandKind(report, "organize", LiveDogfoodTestHappy)
	require.NotNil(t, organize)
	assert.Equal(t, LiveDogfoodStatusPass, organize.Status, organize.Reason)
	_, statErr := os.Stat(filepath.Join(dir, "photos.json"))
	assert.True(t, os.IsNotExist(statErr), "relative plan output must stay out of the CLI directory")
	_, statErr = os.Stat(filepath.Join(dir, "leaked-plan.json"))
	assert.NoError(t, statErr, "a write beside the binary still lands in the CLI directory")

	var stray *LiveDogfoodTestResult
	for i := range report.Tests {
		if strings.Contains(report.Tests[i].Reason, "leaked-plan.json") {
			stray = &report.Tests[i]
			break
		}
	}
	require.NotNil(t, stray, "new files under the CLI directory must be reported")
	assert.Equal(t, LiveDogfoodStatusFail, stray.Status)
	assert.Equal(t, reasonStrayCLIFiles+": leaked-plan.json", stray.Reason)
	assert.NotContains(t, stray.Reason, "photos.json")
	assert.Equal(t, "FAIL", report.Verdict)
}
