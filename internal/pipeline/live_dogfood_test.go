package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLiveDogfoodDetectsJSONParseFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, false)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Greater(t, report.MatrixSize, 0)
	assert.Greater(t, report.Failed, 0)

	var jsonFailure *LiveDogfoodTestResult
	for i := range report.Tests {
		if report.Tests[i].Command == "widgets broken" && report.Tests[i].Kind == LiveDogfoodTestJSON {
			jsonFailure = &report.Tests[i]
			break
		}
	}
	require.NotNil(t, jsonFailure)
	assert.Equal(t, LiveDogfoodStatusFail, jsonFailure.Status)
	assert.Contains(t, jsonFailure.Reason, "invalid JSON")
}

func TestRunLiveDogfoodWritesAcceptanceMarkerOnPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, "full", marker.Level)
	assert.Equal(t, report.MatrixSize, marker.MatrixSize)
	assert.Equal(t, report.Passed, marker.TestsPassed)
	assert.Equal(t, 0, marker.TestsFailed)

	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: marker.APIName, RunID: marker.RunID, AuthType: "none"})
	assert.True(t, validation.Passed, validation.Detail)
}

func TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	var errorPath *LiveDogfoodTestResult
	for i := range report.Tests {
		if report.Tests[i].Command == "widgets get" && report.Tests[i].Kind == LiveDogfoodTestError {
			errorPath = &report.Tests[i]
			break
		}
	}
	require.NotNil(t, errorPath)
	assert.Equal(t, LiveDogfoodStatusPass, errorPath.Status)
	assert.Equal(t, 2, errorPath.ExitCode)
}

func TestRunLiveDogfoodExplicitBinaryNameMustExist(t *testing.T) {
	dir := t.TempDir()

	_, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: "missing-pp-cli",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-pp-cli")
}

func TestRunLiveDogfoodAcceptanceRequiresManifestIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	require.NoError(t, os.Remove(filepath.Join(dir, CLIManifestFilename)))

	_, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: filepath.Join(t.TempDir(), Phase5AcceptanceFilename),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CLI manifest")
}

// TestFinalizeLiveDogfoodReportVerdictGate exercises the quick-level verdict
// switch directly against synthesized reports. The new gate must accept
// skip-with-reason as a non-failure (Passed + Skipped >= 5) with a MatrixSize
// floor of 4, while preserving Failed-dominance and full-level semantics.
func TestFinalizeLiveDogfoodReportVerdictGate(t *testing.T) {
	mkResult := func(status LiveDogfoodStatus) LiveDogfoodTestResult {
		return LiveDogfoodTestResult{Status: status}
	}

	tests := []struct {
		name    string
		level   string
		results []LiveDogfoodTestResult
		want    string
	}{
		{
			name:  "quick all pass classic",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
		},
		{
			name:  "quick 5 pass + 1 skip — companion missing",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusSkip),
			},
			want: "PASS",
		},
		{
			name:  "quick 4 pass + 2 skip — multi-positional skip + no-companion skip",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip),
			},
			want: "PASS",
		},
		{
			name:  "quick 3 pass + 3 skip — MatrixSize floor (3) below 4",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusSkip),
				mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip),
			},
			want: "FAIL",
		},
		{
			name:  "quick 4 pass + 1 fail — Failed dominates",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusFail),
			},
			want: "FAIL",
		},
		{
			name:    "quick all skip — MatrixSize 0",
			level:   "quick",
			results: []LiveDogfoodTestResult{mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip)},
			want:    "FAIL",
		},
		{
			name:  "full all pass — full-level PASS preserved (verdict default)",
			level: "full",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
		},
		{
			name:  "full one fail — Failed dominates at full level",
			level: "full",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusFail),
			},
			want: "FAIL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &LiveDogfoodReport{
				Level:   tt.level,
				Verdict: "PASS",
				Tests:   tt.results,
			}
			finalizeLiveDogfoodReport(report)
			assert.Equal(t, tt.want, report.Verdict, "Passed=%d Failed=%d Skipped=%d MatrixSize=%d",
				report.Passed, report.Failed, report.Skipped, report.MatrixSize)
		})
	}
}

func TestRunLiveDogfoodJSONFlagDetectionIsExact(t *testing.T) {
	help := `Usage:
  fixture-pp-cli widgets list [flags]

Flags:
      --json-output string   Write JSON to a file
`

	assert.False(t, commandSupportsJSON(help))
	assert.True(t, commandSupportsJSON(help+"\n      --json   Output JSON\n"))
}

func writeLiveDogfoodFixture(t *testing.T, brokenJSONFixed bool) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	brokenJSON := "{not-json"
	if brokenJSONFixed {
		brokenJSON = `{"ok":true}`
	}
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"broken"}
    ]},
    {"name":"completion","subcommands":[{"name":"bash"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list --limit 2

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a widget.

Usage:
  fixture-pp-cli widgets get <id> [flags]

Examples:
  fixture-pp-cli widgets get 123

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "broken" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Return malformed JSON.

Usage:
  fixture-pp-cli widgets broken [flags]

Examples:
  fixture-pp-cli widgets broken

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--limit" ] && [ "${4:-}" = "2" ] && [ "${5:-}" = "--json" ]; then
    echo '{"widgets":[{"id":"1"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"123"}'
    exit 0
  fi
  echo 'widget 123'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "broken" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '` + brokenJSON + `'
    exit 0
  fi
  echo 'broken'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeTestManifestForLiveDogfood(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       "fixture-pp-cli",
		RunID:         "run-live-dogfood",
		AuthType:      "none",
	}))
}
