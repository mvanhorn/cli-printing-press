package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveHappyReuseFixture is a CLI with every command shape reuse must handle:
// render is an annotated mutator with --dry-run, charge is one without it,
// fetch is an annotated read that runs for real unless held back, and wipe is
// the unannotated mutator that must never run at publish.
type liveHappyReuseFixture struct {
	dir, binaryName, argvLog, proofsDir, researchDir, acceptance string
}

func newLiveHappyReuseFixture(t *testing.T) liveHappyReuseFixture {
	t.Helper()
	fixture := liveHappyReuseFixture{
		dir:         t.TempDir(),
		binaryName:  "fixture-pp-cli",
		argvLog:     filepath.Join(t.TempDir(), "argv.log"),
		proofsDir:   t.TempDir(),
		researchDir: t.TempDir(),
	}
	fixture.acceptance = filepath.Join(fixture.proofsDir, Phase5AcceptanceFilename)
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", fixture.argvLog)
	writeTestManifestForLiveDogfood(t, fixture.dir)
	require.NoError(t, writeResearchJSON(&ResearchResult{NovelFeatures: []NovelFeature{
		{Name: "Render", Command: "render"},
		{Name: "Charge", Command: "charge"},
		{Name: "Fetch", Command: "fetch"},
	}}, fixture.researchDir))
	writeStubBinary(t, fixture.dir, fixture.binaryName, `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"render","annotations":{"pp:method":"POST","pp:live-happy-path":"true"}},
  {"name":"charge","annotations":{"pp:method":"POST","pp:live-happy-path":"true"}},
  {"name":"fetch","annotations":{"mcp:read-only":"true","pp:live-happy-path":"true"}},
  {"name":"wipe","annotations":{"pp:method":"DELETE"}}
]}
JSON
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  example="fixture-pp-cli $1 --prompt=demo"
  globals="      --json      Output as JSON"
  if [ "$1" = "render" ] || [ "$1" = "fetch" ]; then
    example="$example --dry-run"
    globals="      --dry-run   Show request without sending
$globals"
  fi
  cat <<HELP
Do the thing.

Usage:
  fixture-pp-cli $1 [flags]

Examples:
  $example

Flags:
      --prompt string   Prompt

Global Flags:
$globals
HELP
  exit 0
fi
printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
for a in "$@"; do
  if [ "$a" = "--dry-run" ]; then echo '{"dry_run":true,"action":"preview"}'; exit 0; fi
done
if [ -n "${PRINTING_PRESS_TEST_BAD_JSON:-}" ]; then echo 'not json'; exit 0; fi
echo '{"id":"live-1"}'
`)
	return fixture
}

// runApproved is the Phase 5 run: the operator approved real execution.
func (f liveHappyReuseFixture) runApproved(t *testing.T) *LiveDogfoodReport {
	t.Helper()
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              f.dir,
		BinaryName:          f.binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		ResearchDir:         f.researchDir,
		WriteAcceptancePath: f.acceptance,
		AllowDestructive:    true,
	})
	require.NoError(t, err)
	require.False(t, report.CoverageHollow, report.HollowReasons)
	return report
}

// runPublishGate mirrors publish Step 4.5. The marker it reads is the one it
// overwrites, and the argv log is cleared so it shows only this run.
func (f liveHappyReuseFixture) runPublishGate(t *testing.T) *LiveDogfoodReport {
	t.Helper()
	require.NoError(t, os.RemoveAll(f.argvLog))
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              f.dir,
		BinaryName:          f.binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		ResearchDir:         f.researchDir,
		WriteAcceptancePath: f.acceptance,
		ReuseLiveHappyPath:  f.acceptance,
	})
	require.NoError(t, err)
	return report
}

func (f liveHappyReuseFixture) gate(t *testing.T) Phase5GateValidation {
	t.Helper()
	manifest, err := ReadCLIManifest(f.dir)
	require.NoError(t, err)
	return ValidatePhase5Gate(f.proofsDir, manifest, f.dir)
}

func (f liveHappyReuseFixture) rewriteMarker(t *testing.T, edit func(*Phase5GateMarker)) {
	t.Helper()
	data, err := os.ReadFile(f.acceptance)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	edit(&marker)
	data, err = json.Marshal(marker)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.acceptance, data, 0o644))
}

// assertNothingRanForReal is the core contract: no second paid call, and no
// unannotated mutator, whatever the run's outcome was.
func assertNothingRanForReal(t *testing.T, argvLog string) {
	t.Helper()
	for _, line := range liveHappyArgvLines(t, argvLog) {
		assert.False(t, strings.HasPrefix(line, "charge") || strings.HasPrefix(line, "fetch") || strings.HasPrefix(line, "wipe"),
			"must not execute at publish: %s", line)
		if strings.HasPrefix(line, "render") {
			assert.Contains(t, line, "--dry-run", "render must stay dry-run at publish")
		}
	}
}

func TestPublishGateReusesRecordedLiveHappyPathWithoutRunningAnything(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	approved := fixture.runApproved(t)
	require.Len(t, approved.LiveHappyPathRuns, 3)
	for _, run := range approved.LiveHappyPathRuns {
		assert.False(t, run.Reused, run.Command)
		assert.NotEmpty(t, run.SourceFingerprint, run.Command)
	}

	report := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.Equal(t, "PASS", report.Verdict)
	assert.False(t, report.CoverageHollow, report.HollowReasons)
	require.Len(t, report.LiveHappyPathRuns, 3)
	for i, run := range report.LiveHappyPathRuns {
		assert.True(t, run.Reused, run.Command)
		assert.True(t, approved.LiveHappyPathRuns[i].RanAt.Equal(run.RanAt), "reuse keeps the time the command really ran")
	}
	charge := findResultByCommandKind(report, "charge", LiveDogfoodTestHappy)
	require.NotNil(t, charge)
	assert.Equal(t, LiveDogfoodStatusSkip, charge.Status)
	assert.Equal(t, reasonLiveHappyReused, charge.Reason)

	result := fixture.gate(t)
	assert.True(t, result.Passed, result.Detail)

	// A failed publish gets retried, and the rerun reads the marker the last
	// gate wrote, so evidence has to survive being carried forward.
	retry := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.False(t, retry.CoverageHollow, retry.HollowReasons)
	result = fixture.gate(t)
	assert.True(t, result.Passed, result.Detail)
}

func TestPublishGateRejectsRecordedRunForChangedSource(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	fixture.runApproved(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.dir, "edited.go"), []byte("package main\n"), 0o644))

	report := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.Empty(t, report.LiveHappyPathRuns)
	assert.ElementsMatch(t, []string{"render", "charge", "fetch"}, report.HollowFeatures)
	assert.Contains(t, report.HollowReasons["render"], "no longer matches the current CLI source")
	assert.Contains(t, report.HollowReasons["charge"], "edited.go")

	result := fixture.gate(t)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Detail, "hollow coverage")
}

func TestPublishGateRejectsMarkerFromAnotherRun(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	fixture.runApproved(t)
	fixture.rewriteMarker(t, func(marker *Phase5GateMarker) { marker.RunID = "run-older" })

	report := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.True(t, report.CoverageHollow)
	assert.Contains(t, report.HollowReasons["charge"], `from run "run-older"`)
}

func TestPublishGateIgnoresRunBoundToOtherSource(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	fixture.runApproved(t)
	// The marker matches the tree, but this one entry was carried in from
	// another source, so it must not count.
	fixture.rewriteMarker(t, func(marker *Phase5GateMarker) {
		for i := range marker.LiveHappyPathRuns {
			if marker.LiveHappyPathRuns[i].Command == "charge" {
				marker.LiveHappyPathRuns[i].SourceFingerprint = "other-source"
			}
		}
	})

	report := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.Equal(t, []string{"charge"}, report.HollowFeatures)
	assert.Equal(t, reasonLiveHappyNotRecorded, report.HollowReasons["charge"])
}

func TestPublishGateMissingMarkerRunsNothingAndStaysHollow(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)

	report := fixture.runPublishGate(t)
	assertNothingRanForReal(t, fixture.argvLog)
	assert.ElementsMatch(t, []string{"render", "charge", "fetch"}, report.HollowFeatures)
	assert.Equal(t, reasonLiveHappyNotRecorded, report.HollowReasons["charge"])
}

func TestApprovedRunWithFailedRowRecordsNoEvidence(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_BAD_JSON", "1")

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              fixture.dir,
		BinaryName:          fixture.binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		ResearchDir:         fixture.researchDir,
		WriteAcceptancePath: fixture.acceptance,
		AllowDestructive:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "FAIL", report.Verdict)
	assert.Empty(t, report.LiveHappyPathRuns, "a run with invalid JSON must not vouch for the command later")
}

func TestReuseLiveHappyPathCannotCombineWithAllowDestructive(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	_, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:             fixture.dir,
		BinaryName:         fixture.binaryName,
		Level:              "full",
		AllowDestructive:   true,
		ReuseLiveHappyPath: fixture.acceptance,
	})
	require.ErrorContains(t, err, "cannot be combined with --allow-destructive")
}

func TestReuseLiveHappyPathRejectsCorruptMarker(t *testing.T) {
	fixture := newLiveHappyReuseFixture(t)
	require.NoError(t, os.WriteFile(fixture.acceptance, []byte("{"), 0o644))
	_, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:             fixture.dir,
		BinaryName:         fixture.binaryName,
		Level:              "full",
		ReuseLiveHappyPath: fixture.acceptance,
	})
	require.ErrorContains(t, err, "parsing recorded live happy path marker")
}

func TestValidatePhase5LiveHappyRuns(t *testing.T) {
	marker := Phase5GateMarker{SourceFingerprint: "current"}
	assert.Empty(t, validatePhase5LiveHappyRuns(marker))

	marker.LiveHappyPathRuns = []LiveHappyPathRun{{Command: "render", SourceFingerprint: "current"}}
	assert.Empty(t, validatePhase5LiveHappyRuns(marker))

	marker.LiveHappyPathRuns = []LiveHappyPathRun{
		{Command: "render", SourceFingerprint: "older"},
		{SourceFingerprint: "current"},
	}
	issues := validatePhase5LiveHappyRuns(marker)
	require.Len(t, issues, 2)
	assert.Contains(t, issues[0], `"render" was recorded against different source`)
	assert.Contains(t, issues[1], "missing command")
}

// The publish gate must never gain --allow-destructive: it is run-wide, so
// it would also execute unannotated mutators and credential-changing commands.
func TestPublishLiveGateReusesRecordedRunWithoutAllowDestructive(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))
	start := strings.Index(skill, "LIVE_GATE_ARGS=(")
	require.GreaterOrEqual(t, start, 0)
	end := strings.Index(skill[start:], "rm -f \"$PROOFS_DIR/phase5-skip.json\"")
	require.Greater(t, end, 0)
	block := skill[start : start+end]
	assert.Contains(t, block, `--reuse-live-happy-path "$PROOFS_DIR/phase5-acceptance.json"`)
	assert.NotContains(t, block, "--allow-destructive")
}
