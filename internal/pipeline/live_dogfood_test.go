package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// widgets get happy_path must exercise the resolve-success chain
	// (companion widgets list returns parseable JSON, resolver substitutes the
	// id, get probe runs and passes), not silently skip on companion-parse
	// failure as it did before the fixture fix.
	widgetsGetHappy := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, widgetsGetHappy, "expected widgets get happy_path test result in report")
	assert.Equal(t, LiveDogfoodStatusPass, widgetsGetHappy.Status, widgetsGetHappy.Reason)

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
  fixture-pp-cli widgets list --json

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
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"123"}]}'
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

// writeLiveDogfoodRichFixture builds a fake binary with multi-resource
// command families (widgets, gizmos, projects/tasks, failing-resource) plus
// search/delete commands. Each family's purpose is named in its section
// header below; the test names that consume them follow the same naming.
//
// IMPORTANT: companionSupportsLimit operates on RAW help text (not
// flags-section-scoped), so any --limit token anywhere in a companion's
// help — Examples included — makes the resolver append --limit 1.
// Companions where the test expects a bare call must keep --limit out of
// the help entirely; companions where the test expects --limit 1 must
// declare --limit in Flags.
func writeLiveDogfoodRichFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

# Argv logging side channel. Every invocation appends its argv (space-joined)
# when PRINTING_PRESS_TEST_ARGV_LOG is set. Defaults to no-op so tests that
# don't care about argv tracking work unchanged.
if [ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]; then
  printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
fi

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"describe"},
      {"name":"search"},
      {"name":"search-no-json"},
      {"name":"search-positional"},
      {"name":"delete"}
    ]},
    {"name":"gizmos","subcommands":[
      {"name":"list"},
      {"name":"get"}
    ]},
    {"name":"projects","subcommands":[
      {"name":"list"},
      {"name":"tasks","subcommands":[
        {"name":"list"},
        {"name":"update"}
      ]}
    ]},
    {"name":"failing-resource","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"describe"}
    ]},
    {"name":"completion","subcommands":[{"name":"bash"}]}
  ]
}
JSON
  exit 0
fi

# ---------- widgets family ----------

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"42"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a widget.

Usage:
  fixture-pp-cli widgets get <id> [flags]

Examples:
  fixture-pp-cli widgets get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "widget $3"
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "describe" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Describe a widget.

Usage:
  fixture-pp-cli widgets describe <id> [flags]

Examples:
  fixture-pp-cli widgets describe 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "describe" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","description":"a widget"}'
    exit 0
  fi
  echo "description of widget $3"
  exit 0
fi

# ---------- gizmos family (companion-supports-limit) ----------

if [ "$1" = "gizmos" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List gizmos.

Usage:
  fixture-pp-cli gizmos list [flags]

Examples:
  fixture-pp-cli gizmos list --json

Flags:
      --json     Output JSON
      --limit    Maximum results to return
HELP
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "list" ]; then
  # Resolver appends --limit 1 because --limit is declared in Flags. Match
  # only when both --json AND --limit are present, so a regression that
  # stops appending --limit (bare --json) falls through to the failure
  # branch instead of being silently accepted.
  case "$*" in
    *"--json"*"--limit"*|*"--limit"*"--json"*)
      echo '{"results":[{"id":"42"}]}'
      exit 0
      ;;
  esac
  echo 'gizmo 1'
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a gizmo.

Usage:
  fixture-pp-cli gizmos get <id> [flags]

Examples:
  fixture-pp-cli gizmos get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "gizmo $3"
  exit 0
fi

# ---------- projects/tasks family (chained walk) ----------

if [ "$1" = "projects" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List projects.

Usage:
  fixture-pp-cli projects list [flags]

Examples:
  fixture-pp-cli projects list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"P1"}]}'
    exit 0
  fi
  echo 'project 1'
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "list" ] && [ "${4:-}" = "--help" ]; then
  cat <<'HELP'
List tasks within a project.

Usage:
  fixture-pp-cli projects tasks list <project-id> [flags]

Examples:
  fixture-pp-cli projects tasks list P1 --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "list" ]; then
  # ${4:-} is the resolved project-id (or the matrix walker's invalid-token
  # sentinel for error_path). ${5:-} is --json when supplied. We also handle
  # the self-companion case (4-arg "... list --json" with no project-id),
  # which fires when the resolver's findListCompanion picks "projects tasks
  # list" as the companion for itself; without this branch the matrix walker
  # would silently skip the bare list probe.
  if [ "${4:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid project' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ] || [ "${5:-}" = "--json" ]; then
    echo '{"results":[{"id":"T7"}]}'
    exit 0
  fi
  echo 'task 1'
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "update" ] && [ "${4:-}" = "--help" ]; then
  cat <<'HELP'
Update a task within a project.

Usage:
  fixture-pp-cli projects tasks update <project-id> <task-id> [flags]

Examples:
  fixture-pp-cli projects tasks update P1 T7

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "update" ]; then
  # ${4:-} is project-id (or __printing_press_invalid__ for error_path).
  # ${5:-} is task-id (or --json for malformed error_path argv).
  if [ "${4:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid project' >&2
    exit 2
  fi
  if [ "${6:-}" = "--json" ]; then
    echo '{"id":"T7","status":"updated"}'
    exit 0
  fi
  echo 'updated'
  exit 0
fi

# ---------- failing-resource family (negative cache) ----------

if [ "$1" = "failing-resource" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List failing-resource items.

Usage:
  fixture-pp-cli failing-resource list [flags]

Examples:
  fixture-pp-cli failing-resource list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "list" ]; then
  # Always fail on the actual --json call so the resolver caches a sentinel.
  if [ "${3:-}" = "--json" ]; then
    echo 'upstream service unavailable' >&2
    exit 2
  fi
  echo 'failing-resource 1'
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a failing-resource item.

Usage:
  fixture-pp-cli failing-resource get <id> [flags]

Examples:
  fixture-pp-cli failing-resource get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "failing-resource $3"
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "describe" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Describe a failing-resource item.

Usage:
  fixture-pp-cli failing-resource describe <id> [flags]

Examples:
  fixture-pp-cli failing-resource describe 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "describe" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","description":"a thing"}'
    exit 0
  fi
  echo "description of failing-resource $3"
  exit 0
fi

# ---------- widgets search family (U3 — search-shape error_path) ----------

# widgets search: --query flag + --json flag.
# Mode dispatch via PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE only affects the
# error_path probe (query == __printing_press_invalid__). Walker's happy_path
# and json_fidelity probes use a different query and always return valid JSON
# so they don't pollute test signal.
if [ "$1" = "widgets" ] && [ "$2" = "search" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets.

Usage:
  fixture-pp-cli widgets search <query> [flags]

Examples:
  fixture-pp-cli widgets search --query foo --json

Flags:
      --json     Output JSON
      --query    Search query
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search" ]; then
  # Args shape: widgets search --query <q> [--json]
  query="${4:-}"
  if [ "$query" = "__printing_press_invalid__" ]; then
    case "${PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE:-empty}" in
      fallback)
        echo '{"results":[{"id":"recent-1"},{"id":"recent-2"}]}'
        exit 0
        ;;
      nonzero)
        exit 4
        ;;
      invalid)
        echo '{not-json'
        exit 0
        ;;
      *)
        echo '{"results":[]}'
        exit 0
        ;;
    esac
  fi
  echo '{"results":[]}'
  exit 0
fi

# widgets search-no-json: --query flag, NO --json flag. Used to verify that
# search-shape error_path passes on exit 0 even without --json.
if [ "$1" = "widgets" ] && [ "$2" = "search-no-json" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets without JSON support.

Usage:
  fixture-pp-cli widgets search-no-json <query> [flags]

Examples:
  fixture-pp-cli widgets search-no-json --query foo

Flags:
      --query    Search query
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search-no-json" ]; then
  echo '0 results found.'
  exit 0
fi

# widgets search-positional: positional <query>, no --query flag. Used to
# verify error_path constructs the positional argv shape (no --query flag).
if [ "$1" = "widgets" ] && [ "$2" = "search-positional" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets via a positional query.

Usage:
  fixture-pp-cli widgets search-positional <query> [flags]

Examples:
  fixture-pp-cli widgets search-positional foo --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search-positional" ]; then
  # Args shape: widgets search-positional <query> [--json]
  echo '{"results":[]}'
  exit 0
fi

# widgets delete: mutation-shape (no --query flag, no <query> positional;
# accepts <id>). error_path uses non-zero-required strategy (mutating-leaf
# deny-list overrides search-shape detection).
if [ "$1" = "widgets" ] && [ "$2" = "delete" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Delete a widget.

Usage:
  fixture-pp-cli widgets delete <id> [flags]

Examples:
  fixture-pp-cli widgets delete 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "delete" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid id' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","status":"deleted"}'
    exit 0
  fi
  echo 'deleted'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

// readArgvLog returns the lines from the argv log file, with empty lines
// filtered out. Used by resolve-success and search/error_path tests to
// assert on subprocess invocation count and content.
func readArgvLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// countArgvLines returns the number of argv-log lines whose content contains
// every substring in `must`. Tests filter on `--json` (the actual companion
// call) to exclude `--help` invocations from companionSupportsLimit, which
// would otherwise inflate companion-call counts.
func countArgvLines(lines []string, must ...string) int {
	count := 0
outer:
	for _, line := range lines {
		for _, m := range must {
			if !strings.Contains(line, m) {
				continue outer
			}
		}
		count++
	}
	return count
}

// findResultByCommandKind locates a single matrix-walker result by command
// path and test kind. Used by resolve-success tests to assert on the
// post-resolution status of a specific probe.
func findResultByCommandKind(report *LiveDogfoodReport, command string, kind LiveDogfoodTestKind) *LiveDogfoodTestResult {
	for i := range report.Tests {
		if report.Tests[i].Command == command && report.Tests[i].Kind == kind {
			return &report.Tests[i]
		}
	}
	return nil
}

// setupRichFixture is the shared preamble for U2/U3 tests: skip on Windows,
// build the rich fixture, and enable the argv-log side channel via a unique
// per-test tempfile path. Returns the fixture dir, binary name, and the
// argv-log path (tests that don't read the log can ignore it).
func setupRichFixture(t *testing.T) (dir, binaryName, argvLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName = writeLiveDogfoodRichFixture(t)
	argvLog = filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	return
}

// runRichFixtureMatrix runs the standard full-level matrix walk against the
// rich fixture with the same options every U2/U3 test uses. Fails the test on
// any RunLiveDogfood error.
func runRichFixtureMatrix(t *testing.T, dir, binaryName string) *LiveDogfoodReport {
	t.Helper()
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	return report
}

func TestRunLiveDogfoodResolveSuccessSinglePositional(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// The resolver substituted the id from companion widgets list --json
	// (which returned {"results":[{"id":"42"}]}) into widgets get,
	// producing argv = `widgets get 42`. The probe ran and returned exit 0.
	got := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, got, "expected widgets get happy_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// Companion-leaf invariant: the resolver picked widgets list, not some
	// other allowlisted sibling (widgets search is also in crossAPIListVerbs
	// but sorts later alphabetically). Pin both directions: widgets list
	// must appear AS a companion call, AND widgets search must NOT appear
	// as one. The bare-companion shape `widgets search --json` (path +
	// --json, nothing else) only fires when findListCompanion picks search;
	// the walker's own probe of widgets search uses --query foo --json from
	// Examples, which doesn't match the bare companion shape.
	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets list", "--json"), 1,
		"expected widgets list --json to appear in argv log as the chosen companion")
	bareSearchCompanion := 0
	for _, line := range lines {
		if line == "widgets search --json" {
			bareSearchCompanion++
		}
	}
	assert.Equal(t, 0, bareSearchCompanion,
		"widgets search must NOT be picked as companion when widgets list is available; saw bare `widgets search --json` in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets get 42"), 1,
		"expected widgets get 42 (post-substitution probe) to appear in argv log")
}

func TestRunLiveDogfoodResolveSuccessChainedMultiPositional(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// The chain walks projects list → projects tasks list P1, threading the
	// resolved P1 into the second list call. The final probe is
	// `projects tasks update P1 T7`.
	got := findResultByCommandKind(report, "projects tasks update", LiveDogfoodTestHappy)
	require.NotNil(t, got, "expected projects tasks update happy_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects list", "--json"), 1,
		"expected projects list --json (depth-0 companion) in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects tasks list", "P1", "--json"), 1,
		"expected projects tasks list P1 --json (depth-1 companion threading P1) in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects tasks update P1 T7"), 1,
		"expected projects tasks update P1 T7 (post-chain probe) in argv log")
}

func TestRunLiveDogfoodResolveSuccessCacheHit(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Both siblings successfully resolve and run their probes.
	getResult := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, getResult)
	assert.Equal(t, LiveDogfoodStatusPass, getResult.Status, getResult.Reason)
	descResult := findResultByCommandKind(report, "widgets describe", LiveDogfoodTestHappy)
	require.NotNil(t, descResult)
	assert.Equal(t, LiveDogfoodStatusPass, descResult.Status, descResult.Reason)

	// Cache hit: one cached id serves every widgets-family id-shape sibling.
	// The walker probes `widgets list` itself for both happy_path and
	// json_fidelity, both of which invoke argv `widgets list --json`
	// (Examples already has --json, so appendJSONArg dedups and json_fidelity
	// reuses the happy argv). The resolver invokes the companion exactly
	// once for the first id-shape sibling probed; subsequent siblings hit
	// the cache and add 0.
	const walkerProbes, resolverCallsWithCacheHit = 2, 1
	const expectedTotal = walkerProbes + resolverCallsWithCacheHit
	lines := readArgvLog(t, argvLog)
	companionCalls := countArgvLines(lines, "widgets list", "--json")
	// Equality assertion (not <=) catches both directions: cache miss inflates
	// to expectedTotal+1, walker-side dedup deflates to expectedTotal-1. Bare
	// upper bound would silently pass on the second case.
	assert.Equal(t, expectedTotal, companionCalls,
		"expected exactly %d widgets list --json invocations (%d walker + %d resolver with cache hit); got %d", expectedTotal, walkerProbes, resolverCallsWithCacheHit, companionCalls)

	// Both probe argvs landed in the log post-substitution.
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets get 42"), 1)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets describe 42"), 1)
}

func TestRunLiveDogfoodResolveSuccessCompanionLimit(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// gizmos list declares --limit in its Flags section, so the resolver
	// appends --limit 1 to the companion call before invoking it.
	got := findResultByCommandKind(report, "gizmos get", LiveDogfoodTestHappy)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	lines := readArgvLog(t, argvLog)
	// The resolver's gizmos list call must include both --json and --limit 1.
	// Order is resolver-dependent (currently --json --limit 1) so we assert
	// substring presence rather than exact ordering.
	limitCalls := countArgvLines(lines, "gizmos list", "--json", "--limit 1")
	assert.GreaterOrEqual(t, limitCalls, 1,
		"expected gizmos list call with both --json and --limit 1; got 0 such lines")
}

func TestRunLiveDogfoodResolveSuccessNegativeCacheSentinel(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// failing-resource list returns exit non-zero for --json. The walker
	// probes commands in alphabetical order: `failing-resource describe`
	// runs first and hits the FRESH failure (caches sentinel);
	// `failing-resource get` runs second and hits the cached sentinel.
	descResult := findResultByCommandKind(report, "failing-resource describe", LiveDogfoodTestHappy)
	require.NotNil(t, descResult)
	assert.Equal(t, LiveDogfoodStatusSkip, descResult.Status,
		"first sibling (describe) should skip with fresh companion-failure reason")
	assert.Contains(t, descResult.Reason, "list companion failed at depth",
		"first sibling reason should reference the actual depth-keyed failure, not the cached sentinel")

	getResult := findResultByCommandKind(report, "failing-resource get", LiveDogfoodTestHappy)
	require.NotNil(t, getResult)
	assert.Equal(t, LiveDogfoodStatusSkip, getResult.Status,
		"second sibling (get) should skip via the cached negative-cache sentinel")
	assert.Contains(t, getResult.Reason, "list companion previously failed at depth",
		"second sibling reason should reference the cached sentinel, not re-fail the companion")

	// Filter to the actual companion call (--json excludes --help). The
	// walker probes `failing-resource list` for both happy_path and
	// json_fidelity (both use argv `failing-resource list --json` since
	// Examples already includes --json). The resolver invokes the companion
	// once for the first sibling (describe), caches the sentinel, and the
	// second sibling (get) hits the sentinel without re-invoking.
	const walkerProbes, resolverCallsWithSentinel = 2, 1
	const expectedTotal = walkerProbes + resolverCallsWithSentinel
	lines := readArgvLog(t, argvLog)
	companionCalls := countArgvLines(lines, "failing-resource list", "--json")
	// Equality assertion (not <=) catches sentinel-bypass (4 calls) AND any
	// future walker-side dedup that would collapse to 2 calls without going
	// through the sentinel path.
	assert.Equal(t, expectedTotal, companionCalls,
		"expected exactly %d failing-resource list --json invocations (%d walker + %d resolver with sentinel); got %d", expectedTotal, walkerProbes, resolverCallsWithSentinel, companionCalls)
}

// ----- U3: search-aware error_path integration tests -----

func TestRunLiveDogfoodSearchErrorPathEmptyResults(t *testing.T) {
	// Mode unset → fixture returns exit 0 + {"results":[]} for the
	// __printing_press_invalid__ probe.
	dir, binaryName, _ := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got, "expected widgets search error_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

func TestRunLiveDogfoodSearchErrorPathFallbackResults(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "fallback")
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Recency-fallback APIs return content under unmatched queries — exit 0
	// with non-empty results is a valid "no match" signal, not a failure.
	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

func TestRunLiveDogfoodSearchErrorPathNoJSONSupport(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Search-shape command without --json flag declared. Exit 0 alone is
	// sufficient when --json wasn't supplied — no JSON validation possible.
	got := findResultByCommandKind(report, "widgets search-no-json", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// argv-log assertion: the error_path probe ran without --json.
	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets search-no-json", "--query", "__printing_press_invalid__"), 1,
		"expected error_path probe to use --query for the no-json search command")
}

func TestRunLiveDogfoodSearchErrorPathPositionalQuery(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search-positional", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// argv-log assertion: probe used the positional argv shape, not --query
	// (the command has <query> in Usage but no --query flag in Flags).
	lines := readArgvLog(t, argvLog)
	positionalCalls := countArgvLines(lines, "widgets search-positional __printing_press_invalid__", "--json")
	assert.GreaterOrEqual(t, positionalCalls, 1,
		"expected error_path probe to use positional <query>, not --query flag")
	flagCalls := countArgvLines(lines, "widgets search-positional", "--query")
	assert.Equal(t, 0, flagCalls,
		"expected --query flag NOT to appear in error_path argv when command uses positional <query>")
}

func TestRunLiveDogfoodSearchErrorPathNonZeroExit(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "nonzero")
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Non-zero exit is also a valid "no match" signal for some APIs;
	// search-shape error_path treats it as Pass, consistent with mutation.
	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

// TestRunLiveDogfoodSearchErrorPathInvalidJSON exercises the only fail mode
// currently produced by the search-shape error_path code in
// live_dogfood.go:693-711. INVARIANT: if the production code adds a new
// search-shape Fail branch (timeout, empty stdout under --json, schema
// mismatch), add a corresponding integration test in the same change.
func TestRunLiveDogfoodSearchErrorPathInvalidJSON(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "invalid")
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusFail, got.Status,
		"search + invalid JSON under --json is the only search-shape error_path Fail mode")
	assert.Contains(t, got.Reason, "invalid JSON")
}

func TestRunLiveDogfoodSearchErrorPathMutationFallthrough(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// widgets delete has no --query flag and no <query> positional, so
	// commandSupportsSearch returns false. Even if it had --query (it
	// doesn't), the mutating-leaf deny-list (delete is in mutatingVerbs)
	// would still suppress search-shape and route to the existing
	// non-zero-required strategy. Fixture exit 2 → Pass.
	got := findResultByCommandKind(report, "widgets delete", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	assert.Equal(t, 2, got.ExitCode)
}
