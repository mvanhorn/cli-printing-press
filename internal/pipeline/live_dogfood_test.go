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
			name:  "quick 1-command all pass — 4 entries should PASS via min(5, M) threshold",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
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

func TestExtractFirstIDFromJSON(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   string
		ok     bool
	}{
		{
			name:   "TMDb results shape",
			stdout: `{"results":[{"id":"42"}],"total_results":1}`,
			want:   "42", ok: true,
		},
		{
			name:   "top-level array",
			stdout: `[{"id":"first"},{"id":"second"}]`,
			want:   "first", ok: true,
		},
		{
			name:   "items shape (GitHub REST)",
			stdout: `{"items":[{"id":"abc"}],"total_count":1}`,
			want:   "abc", ok: true,
		},
		{
			name:   "data array (Stripe)",
			stdout: `{"object":"list","data":[{"id":"cus_xyz"}],"has_more":false}`,
			want:   "cus_xyz", ok: true,
		},
		{
			name:   "list shape (long-tail)",
			stdout: `{"list":[{"id":"L1"}]}`,
			want:   "L1", ok: true,
		},
		{
			name:   "GraphQL nodes (Shopify)",
			stdout: `{"data":{"products":{"nodes":[{"id":"gid://shopify/Product/42"}]}}}`,
			want:   "gid://shopify/Product/42", ok: true,
		},
		{
			name:   "GraphQL edges (Relay-style)",
			stdout: `{"data":{"viewer":{"repos":{"edges":[{"node":{"id":"R_kgABC123"}}]}}}}`,
			want:   "R_kgABC123", ok: true,
		},
		{
			name:   "numeric id preserved as string",
			stdout: `{"results":[{"id":12345}]}`,
			want:   "12345", ok: true,
		},
		{
			name:   "snowflake-size numeric id (no scientific notation)",
			stdout: `{"results":[{"id":1234567890123456789}]}`,
			want:   "1234567890123456789", ok: true,
		},
		{
			name:   "empty results — no id",
			stdout: `{"results":[]}`,
			want:   "", ok: false,
		},
		{
			name:   "results without id field",
			stdout: `{"results":[{"name":"thing"}]}`,
			want:   "", ok: false,
		},
		{
			name:   "invalid JSON",
			stdout: `not json at all`,
			want:   "", ok: false,
		},
		{
			name:   "matches REST results before GraphQL — REST wins",
			stdout: `{"results":[{"id":"REST"}],"data":{"x":{"nodes":[{"id":"GQL"}]}}}`,
			want:   "REST", ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractFirstIDFromJSON(tt.stdout)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSiblingMap(t *testing.T) {
	commands := []liveDogfoodCommand{
		{Path: []string{"projects", "list"}},
		{Path: []string{"projects", "get"}},
		{Path: []string{"projects", "tasks", "list"}},
		{Path: []string{"projects", "tasks", "update"}},
		{Path: []string{"users", "get"}},
	}
	siblings := buildSiblingMap(commands)

	// Top-level commands keyed by "" (parent path).
	assert.Len(t, siblings["projects"], 2, "projects subcommands")
	assert.Len(t, siblings["projects tasks"], 2, "projects tasks subcommands")
	assert.Len(t, siblings["users"], 1, "users subcommands")
}

func TestFindListCompanion(t *testing.T) {
	candidates := []liveDogfoodCommand{
		{Path: []string{"widgets", "get"}},
		{Path: []string{"widgets", "list"}},
		{Path: []string{"widgets", "delete"}},
	}
	got := findListCompanion(candidates)
	if assert.NotNil(t, got) {
		assert.Equal(t, []string{"widgets", "list"}, got.Path)
	}

	// Cinema verb fallback.
	cinema := []liveDogfoodCommand{
		{Path: []string{"movies", "get"}},
		{Path: []string{"movies", "popular"}},
	}
	got = findListCompanion(cinema)
	if assert.NotNil(t, got) {
		assert.Equal(t, []string{"movies", "popular"}, got.Path)
	}

	// No allowlisted leaf.
	none := []liveDogfoodCommand{
		{Path: []string{"x", "delete"}},
		{Path: []string{"x", "update"}},
	}
	assert.Nil(t, findListCompanion(none))
}

func TestSubstitutePositionals(t *testing.T) {
	tests := []struct {
		name        string
		happyArgs   []string
		commandPath []string
		resolved    []string
		want        []string
	}{
		{
			name:        "single positional",
			happyArgs:   []string{"widgets", "get", "example-value"},
			commandPath: []string{"widgets", "get"},
			resolved:    []string{"42"},
			want:        []string{"widgets", "get", "42"},
		},
		{
			name:        "two positionals",
			happyArgs:   []string{"projects", "tasks", "update", "ph1", "ph2"},
			commandPath: []string{"projects", "tasks", "update"},
			resolved:    []string{"P1", "T1"},
			want:        []string{"projects", "tasks", "update", "P1", "T1"},
		},
		{
			name:        "positional before flag",
			happyArgs:   []string{"widgets", "update", "ph1", "--name", "thing"},
			commandPath: []string{"widgets", "update"},
			resolved:    []string{"abc"},
			want:        []string{"widgets", "update", "abc", "--name", "thing"},
		},
		{
			name:        "no positionals (resolved empty)",
			happyArgs:   []string{"widgets", "list", "--limit", "5"},
			commandPath: []string{"widgets", "list"},
			resolved:    nil,
			want:        []string{"widgets", "list", "--limit", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substitutePositionals(tt.happyArgs, tt.commandPath, tt.resolved)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveCommandPositionalsSkipPaths(t *testing.T) {
	ctx := resolveCtx{
		siblings: map[string][]liveDogfoodCommand{},
		cache:    newCompanionCache(),
		timeout:  time.Second,
	}

	// No positionals → not-skipped, happyArgs unchanged.
	cmd := liveDogfoodCommand{
		Path: []string{"widgets", "list"},
		Help: "Usage:\n  cli widgets list [flags]\n",
	}
	args, skipped, _ := resolveCommandPositionals(cmd, []string{"widgets", "list"}, ctx)
	assert.False(t, skipped)
	assert.Equal(t, []string{"widgets", "list"}, args)

	// Non-id-shape positional (<query>) at depth 0 → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"widgets", "search"},
		Help: "Usage:\n  cli widgets search <query> [flags]\n",
	}
	_, skipped, reason := resolveCommandPositionals(cmd, []string{"widgets", "search", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "non-id positional")

	// id-shape positional (bare `id`) but no companion → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"widgets", "get"},
		Help: "Usage:\n  cli widgets get <id> [flags]\n",
	}
	_, skipped, reason = resolveCommandPositionals(cmd, []string{"widgets", "get", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "no list companion")

	// camelCase id-shape positional (movieId) but no companion → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"movies", "get"},
		Help: "Usage:\n  cli movies get <movieId> [flags]\n",
	}
	_, skipped, reason = resolveCommandPositionals(cmd, []string{"movies", "get", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "no list companion")

	// Path shorter than placeholders + 1 → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"get"},
		Help: "Usage:\n  cli get <id> <name> [flags]\n",
	}
	_, skipped, _ = resolveCommandPositionals(cmd, []string{"get", "x", "y"}, ctx)
	assert.True(t, skipped)
}

func TestCommandSupportsSearch(t *testing.T) {
	tests := []struct {
		name string
		help string
		want bool
	}{
		{
			name: "search via --query flag",
			help: `Usage:
  fixture-pp-cli widgets search [flags]

Flags:
      --query string   Search query
      --json           Output JSON
`,
			want: true,
		},
		{
			name: "search via positional <query>",
			help: `Usage:
  fixture-pp-cli widgets search <query> [flags]

Flags:
      --json   Output JSON
`,
			want: true,
		},
		{
			name: "non-search list command — no query signal",
			help: `Usage:
  fixture-pp-cli widgets list [flags]

Flags:
      --limit int   Max items
      --json        Output JSON
`,
			want: false,
		},
		{
			name: "exact-match flag — --queue must not match --query",
			help: `Usage:
  fixture-pp-cli widgets dispatch [flags]

Flags:
      --queue string   Job queue name
`,
			want: false,
		},
		{
			name: "Examples block mentioning --query does NOT trigger search-shape (Flags-section scoping)",
			help: `Usage:
  fixture-pp-cli widgets delete <id> [flags]

Examples:
  fixture-pp-cli widgets delete 42
  # related: fixture-pp-cli widgets list --query=foo

Flags:
      --yes   Confirm
`,
			want: false,
		},
		{
			name: "Long block mentioning --query does NOT trigger search-shape",
			help: `Long: To delete by filter, see the related --query syntax in widgets list.

Usage:
  fixture-pp-cli widgets purge <id> [flags]

Flags:
      --force   Skip confirmation
`,
			want: false,
		},
		{
			name: "mutation command — no query signal",
			help: `Usage:
  fixture-pp-cli widgets delete <id> [flags]

Flags:
      --yes   Confirm
`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commandSupportsSearch(tt.help))
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
