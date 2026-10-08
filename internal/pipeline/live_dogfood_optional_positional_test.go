package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPositionalPlaceholderSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage string
		want  []positionalPlaceholder
	}{
		{name: "required", usage: "<batch-id> [flags]", want: []positionalPlaceholder{{name: "batch-id"}}},
		{name: "optional", usage: "[batch-id] [flags]", want: []positionalPlaceholder{{name: "batch-id", optional: true}}},
		{name: "optional wrapping angle brackets", usage: "[<batch-id>] [flags]", want: []positionalPlaceholder{{name: "batch-id", optional: true}}},
		{
			name:  "required then optional",
			usage: "<id> [field] [flags]",
			want:  []positionalPlaceholder{{name: "id"}, {name: "field", optional: true}},
		},
		{name: "flag descriptors ignored", usage: "<id> [--tags=<csv>] [flags]", want: []positionalPlaceholder{{name: "id"}}},
		{name: "no positionals", usage: "[flags]", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractPositionalPlaceholderSpecs(tt.usage))
		})
	}
}

func TestLiveDogfoodPlaceholdersToResolve(t *testing.T) {
	t.Parallel()

	help := func(usage string) string {
		return "Usage:\n  cli " + usage + "\n\nFlags:\n      --agent       Agent mode\n  -l, --limit int   Max rows\n  -v, --verbose     Verbose\n"
	}
	tests := []struct {
		name      string
		path      []string
		usage     string
		happyArgs []string
		want      []string
		wantDepth int
	}{
		{
			name:      "optional positional absent from example is not resolved",
			path:      []string{"journal"},
			usage:     "journal [batch-id] [flags]",
			happyArgs: []string{"journal", "--agent", "--limit", "5"},
			want:      []string{},
			wantDepth: 1,
		},
		{
			name:      "shorthand value flag does not count as a positional",
			path:      []string{"journal"},
			usage:     "journal [batch-id] [flags]",
			happyArgs: []string{"journal", "-l", "5"},
			want:      []string{},
			wantDepth: 1,
		},
		{
			name:      "boolean shorthand before a positional keeps the positional",
			path:      []string{"journal"},
			usage:     "journal [batch-id] [flags]",
			happyArgs: []string{"journal", "-v", "batch-1"},
			want:      []string{"batch-id"},
			wantDepth: 1,
		},
		{
			name:      "optional positional supplied by example still resolves",
			path:      []string{"journal"},
			usage:     "journal [batch-id] [flags]",
			happyArgs: []string{"journal", "batch-1", "--limit", "5"},
			want:      []string{"batch-id"},
			wantDepth: 1,
		},
		{
			name:      "required positional always resolves",
			path:      []string{"undo"},
			usage:     "undo <batch-id> [flags]",
			happyArgs: []string{"undo", "--limit", "5"},
			want:      []string{"batch-id"},
			wantDepth: 1,
		},
		{
			name:      "trailing optional dropped after required",
			path:      []string{"widgets", "get"},
			usage:     "widgets get <id> [field] [flags]",
			happyArgs: []string{"widgets", "get", "w-1"},
			want:      []string{"id"},
			wantDepth: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := liveDogfoodCommand{Path: tt.path, Help: help(tt.usage)}
			got, depth := liveDogfoodPlaceholdersToResolve(cmd, tt.happyArgs)
			assert.Equal(t, tt.wantDepth, depth, "depth always counts every Usage positional")
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLiveDogfoodPlaceholdersToResolveShorthandsAreCaseSensitive(t *testing.T) {
	t.Parallel()

	// -v takes a value and -V is boolean; folding case would make -V eat
	// the positional that follows it.
	cmd := liveDogfoodCommand{
		Path: []string{"journal"},
		Help: "Usage:\n  cli journal [batch-id] [flags]\n\nFlags:\n  -v, --view string   View\n  -V, --verbose       Verbose\n",
	}
	got, _ := liveDogfoodPlaceholdersToResolve(cmd, []string{"journal", "-V", "batch-1"})
	assert.Equal(t, []string{"batch-id"}, got, "batch-1 is the positional after a boolean -V")

	got, _ = liveDogfoodPlaceholdersToResolve(cmd, []string{"journal", "-v", "table"})
	assert.Empty(t, got, "table is the value of -v, not a positional")

	got, _ = liveDogfoodPlaceholdersToResolve(cmd, []string{"journal", "-Vv", "table"})
	assert.Empty(t, got, "a cluster ending in a value shorthand reads the next argument")
}

func TestResolveCommandPositionalsKeepsUsageDepthWhenOptionalTailDropped(t *testing.T) {
	t.Parallel()

	// Both companions are pre-cached so no subprocess runs. With the full
	// Usage depth, <project-id> resolves from `projects list`; a shortened
	// depth would wrongly ask `projects tasks list` instead.
	ctx := resolveCtx{
		siblings: map[string][]liveDogfoodCommand{
			"projects":       {{Path: []string{"projects", "list"}}},
			"projects tasks": {{Path: []string{"projects", "tasks", "list"}}},
		},
		cache:   newCompanionCache(),
		timeout: time.Second,
	}
	ctx.cache.helps["projects list"] = ""
	ctx.cache.helps["projects tasks list"] = ""
	ctx.cache.results[strings.Join([]string{"projects", "list", "--json"}, "\x00")] = "P1"
	ctx.cache.results[strings.Join([]string{"projects", "tasks", "list", "--json"}, "\x00")] = "T1"

	cmd := liveDogfoodCommand{
		Path: []string{"projects", "tasks", "get"},
		Help: "Usage:\n  cli projects tasks get <project-id> [task-id] [flags]\n",
	}
	args, skipped, reason, _ := resolveCommandPositionals(cmd, []string{"projects", "tasks", "get", "project-1"}, 0, ctx)
	require.False(t, skipped, reason)
	assert.Equal(t, []string{"projects", "tasks", "get", "P1"}, args)
}

func TestRunLiveDogfoodOptionalPositionalRunsExampleWithoutCompanion(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	// `index` is a sibling list companion that returns no ids, so any
	// positional that requires companion resolution cannot resolve.
	script := `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"index","annotations":{"mcp:read-only":"true"}},
  {"name":"journal","annotations":{"mcp:read-only":"true"}},
  {"name":"replay","annotations":{"mcp:read-only":"true"}}
]}
JSON
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  case "$1" in
    index) usage="index"; example="index --json" ;;
    journal) usage="journal [batch-id]"; example="journal --agent -l 5" ;;
    replay) usage="replay <batch-id>"; example="replay batch-1" ;;
  esac
  cat <<HELP
Command.

Usage:
  fixture-pp-cli $usage [flags]

Examples:
  fixture-pp-cli $example

Flags:
      --agent       Agent mode
  -l, --limit int   Max rows
      --json        Output JSON
HELP
  exit 0
fi
case "$1" in
  index) echo '{"files":[]}' ;;
  *) echo '{"entries":[]}' ;;
esac
exit 0
`
	writeStubBinary(t, dir, binaryName, script)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	journal := findResultByCommandKind(report, "journal", LiveDogfoodTestHappy)
	require.NotNil(t, journal)
	assert.Equal(t, LiveDogfoodStatusPass, journal.Status, journal.Reason)
	assert.Equal(t, []string{"journal", "--agent", "-l", "5"}, journal.Args,
		"the optional positional runs the Example as written, shorthand value flag included")

	// A required positional keeps companion resolution and its honest skip.
	replay := findResultByCommandKind(report, "replay", LiveDogfoodTestHappy)
	require.NotNil(t, replay)
	assert.Equal(t, LiveDogfoodStatusSkip, replay.Status)
	assert.Contains(t, replay.Reason, "no id parseable from companion")
}
