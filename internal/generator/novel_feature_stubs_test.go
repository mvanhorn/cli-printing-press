package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratorEmitsNovelFeatureCommandStubs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("apify")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Actor call wrapper",
			Command:     "call",
			Description: "Call an actor with idempotent tags.",
			Rationale:   "Agents need to run actors without re-creating identical jobs.",
			Example:     "apify-pp-cli call apify/web-scraper --tag skill=reddit-digest --dedupe-key daily --ttl 24h --wait --agent",
		},
		{
			Name:        "Run classifier",
			Command:     "runs classify",
			Description: "Classify recent runs by failure mode.",
			Rationale:   "Agents need a bounded view of run failures.",
			Example:     "apify-pp-cli runs classify run-123 --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newNovelCallCmd(flags))")
	assert.Contains(t, root, "rootCmd.AddCommand(newNovelRunsCmd(flags))")

	call := readGeneratedFile(t, outputDir, "internal", "cli", "call.go")
	assert.Contains(t, call, `Use:         "call"`)
	assert.Contains(t, call, `Annotations: map[string]string{"mcp:read-only": "false"}`)
	assert.Contains(t, call, `StringSliceVar(&flagTag, "tag", nil`)
	assert.Contains(t, call, `StringVar(&flagDedupeKey, "dedupe-key", ""`)
	assert.Contains(t, call, `StringVar(&flagTtl, "ttl", ""`)
	assert.Contains(t, call, `BoolVar(&flagWait, "wait", false`)
	assert.NotContains(t, call, `"agent"`)
	assert.Contains(t, call, `TODO: implement novel feature %q", "call"`)

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "runs.go")
	assert.Contains(t, parent, `Use:         "runs"`)
	assert.Contains(t, parent, "RunE:        parentNoSubcommandRunE(flags)")
	assert.Contains(t, parent, "cmd.AddCommand(newNovelRunsClassifyCmd(flags))")

	classify := readGeneratedFile(t, outputDir, "internal", "cli", "runs_classify.go")
	assert.Contains(t, classify, `Use:         "classify"`)
	assert.Contains(t, classify, `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, classify, `StringVar(&flagLimit, "limit", ""`)
	assert.Contains(t, classify, `TODO: implement novel feature %q", "runs classify"`)

	testSrc := readGeneratedFile(t, outputDir, "internal", "cli", "call_test.go")
	assert.Contains(t, testSrc, `t.Skip("TODO: implement table-driven tests for call")`)

	var runtimeTest strings.Builder
	runtimeTest.WriteString(`package cli

import (
	"io"
	"testing"
)

func TestNovelFeatureStubsResolveAtRuntime(t *testing.T) {
	cases := [][]string{
		{"call", "--help"},
		{"runs", "classify", "--help"},
		{"call", "apify/web-scraper", "--dry-run"},
	}
	for _, args := range cases {
		cmd := RootCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("RootCmd(%v) error = %v", args, err)
		}
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_stub_runtime_test.go"), []byte(runtimeTest.String()), 0o644))
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli")
}

func TestGeneratorSkipsNovelFeatureStubsWhenNoCommandPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("stubless")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Flag-only feature",
		Command:     "--global-search",
		Description: "A cross-cutting flag should not emit a fake command.",
	}}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NotContains(t, root, "newNovel")
	_, err := os.Stat(filepath.Join(outputDir, "internal", "cli", "global_search.go"))
	assert.True(t, os.IsNotExist(err))
}

func TestGeneratorNovelFeatureHelpGuardRequiresPositionalUse(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novelargs")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Inspect item",
			Command:     "inspect <id>",
			Description: "Inspect one item.",
			Example:     "novelargs-pp-cli inspect item-123 --format json",
		},
		{
			Name:        "Metric report",
			Command:     "report",
			Description: "Report metrics selected by flags.",
			Example:     "novelargs-pp-cli report --metric latency",
		},
		{
			Name:        "Audit",
			Command:     "audit",
			Description: "Audit local cache state.",
			Example:     "novelargs-pp-cli audit",
		},
		{
			Name:        "Search",
			Command:     "search --filter [active|inactive]",
			Description: "Search items, filtered by flag.",
			Example:     "novelargs-pp-cli search --filter active",
		},
	}
	require.NoError(t, gen.Generate())

	inspect := readGeneratedFile(t, outputDir, "internal", "cli", "inspect.go")
	assert.Contains(t, inspect, `Use:         "inspect <id>"`)
	assert.Contains(t, inspect, "if len(args) == 0 {")
	assert.Contains(t, inspect, "return cmd.Help()")

	report := readGeneratedFile(t, outputDir, "internal", "cli", "report.go")
	assert.NotContains(t, report, "return cmd.Help()")
	assert.Contains(t, report, "// validate required flags here")
	assert.Contains(t, report, "if dryRunOK(flags) {")
	assert.Contains(t, report, `TODO: implement novel feature %q", "report"`)

	audit := readGeneratedFile(t, outputDir, "internal", "cli", "audit.go")
	assert.NotContains(t, audit, "return cmd.Help()")
	assert.Contains(t, audit, "// validate required flags here")
	assert.Contains(t, audit, "if dryRunOK(flags) {")
	assert.Contains(t, audit, `TODO: implement novel feature %q", "audit"`)

	// A bracket/angle placeholder inside a flag-value hint is NOT a positional
	// (#2592 regression guard): no args-based Help guard, and the flag-value
	// hint must not leak into the cobra Use string.
	search := readGeneratedFile(t, outputDir, "internal", "cli", "search.go")
	assert.NotContains(t, search, "return cmd.Help()")
	assert.Contains(t, search, "// validate required flags here")
	assert.NotContains(t, search, "[active|inactive]")
}

func TestGeneratorNovelFeatureParentShortHasNoTODO(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novelparent")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Snapshot diff",
			Command:     "snapshot diff",
			Description: "Compare two snapshots.",
			Example:     "novelparent-pp-cli snapshot diff before after",
		},
		{
			Name:        "Snapshot list",
			Command:     "snapshot list",
			Description: "List snapshots.",
			Example:     "novelparent-pp-cli snapshot list",
		},
		{
			Name:        "Single command",
			Command:     "single",
			Description: "A single-segment novel command.",
			Example:     "novelparent-pp-cli single",
		},
	}
	require.NoError(t, gen.Generate())

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "snapshot.go")
	assert.Contains(t, parent, `Short:       "snapshot subcommands: diff, list"`)
	assert.NotContains(t, parent, `Short:       "TODO`)

	single := readGeneratedFile(t, outputDir, "internal", "cli", "single.go")
	assert.Contains(t, single, `Short:       "A single-segment novel command."`)
	assert.NotContains(t, single, `subcommands:`)
}
