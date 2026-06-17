package generator

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

func TestGeneratorEmitsBoundCtxHelperForNovelCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("timeoutnovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Sibling client scan",
			Command:     "scan",
			Description: "Scan via a sibling client.",
			Example:     "timeoutnovel-pp-cli scan --json",
		},
	}
	require.NoError(t, gen.Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.Contains(t, helpers, "func boundCtx(parent context.Context, flags *rootFlags) (context.Context, context.CancelFunc)")
	assert.Contains(t, helpers, "return context.WithTimeout(parent, flags.timeout)")

	var runtimeTest strings.Builder
	runtimeTest.WriteString(`package cli

import (
	"context"
	"testing"
	"time"
)

func TestBoundCtxAppliesRootTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel := boundCtx(parent, &rootFlags{timeout: 25 * time.Millisecond})
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("boundCtx did not apply a deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("deadline already expired")
	}
}

func TestBoundCtxNoopsWithoutTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel := boundCtx(parent, &rootFlags{})
	defer cancel()
	if ctx != parent {
		t.Fatalf("boundCtx should return the parent context when timeout is unset")
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "bound_ctx_runtime_test.go"), []byte(runtimeTest.String()), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorSkipsNovelFeatureWiringForAbsorbedEndpointCollisions(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("make")
	apiSpec.Resources = map[string]spec.Resource{
		"scenarios": {
			Description: "Manage scenarios",
			Endpoints: map[string]spec.Endpoint{
				"get-qrcode": {Method: "GET", Path: "/scenarios/{id}/qrcode", Description: "Get scenario QR code"},
				"list":       {Method: "GET", Path: "/scenarios", Description: "List scenarios"},
				"run":        {Method: "POST", Path: "/scenarios/{id}/run", Description: "Run scenario"},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Blocking scenario run",
			Command:     "scenarios run --wait",
			Description: "Run a scenario and wait for completion.",
			Example:     "make-pp-cli scenarios run scenario-123 --wait",
		},
		{
			Name:        "Cross-team scenario list",
			Command:     "scenarios list --all-teams",
			Description: "List scenarios across every team.",
			Example:     "make-pp-cli scenarios list --all-teams",
		},
		{
			Name:        "Scenario QR watcher",
			Command:     "scenarios get-qrcode --watch",
			Description: "Watch a scenario QR code until it changes.",
			Example:     "make-pp-cli scenarios get-qrcode scenario-123 --watch",
		},
		{
			Name:        "Scenario health",
			Command:     "scenarios health --limit 10",
			Description: "Summarize scenario health.",
			Example:     "make-pp-cli scenarios health --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "scenarios.go")
	assert.Contains(t, parent, "cmd.AddCommand(newScenariosListCmd(flags))")
	assert.Contains(t, parent, "cmd.AddCommand(newScenariosRunCmd(flags))")
	assert.Contains(t, parent, "cmd.AddCommand(newNovelScenariosHealthCmd(flags))")
	assert.NotContains(t, parent, "newNovelScenariosGetQrcodeCmd")
	assert.NotContains(t, parent, "newNovelScenariosListCmd")
	assert.NotContains(t, parent, "newNovelScenariosRunCmd")

	health := readGeneratedFile(t, outputDir, "internal", "cli", "scenarios_health.go")
	assert.Contains(t, health, `TODO: implement novel feature %q", "scenarios health"`)
	requireGeneratedCompiles(t, outputDir)

	require.NoError(t, gen.Generate())
	parent = readGeneratedFile(t, outputDir, "internal", "cli", "scenarios.go")
	assert.Contains(t, parent, "cmd.AddCommand(newNovelScenariosHealthCmd(flags))")
	assert.NotContains(t, parent, "newNovelScenariosGetQrcodeCmd")
	assert.NotContains(t, parent, "newNovelScenariosListCmd")
	assert.NotContains(t, parent, "newNovelScenariosRunCmd")
}

func TestGeneratorSkipsNovelFeatureWiringForExistingCommandFileCollisions(t *testing.T) {
	apiSpec := minimalSpec("existingnovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "items_audit.go"), []byte(`package cli

import "github.com/spf13/cobra"

func newItemsAuditCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{Use: "audit"}
}
`), 0o644))

	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Bulk export",
			Command:     "export --format json",
			Description: "Export data with extra filtering.",
			Example:     "existingnovel-pp-cli export --format json",
		},
		{
			Name:        "Item audit",
			Command:     "items audit --dry-run",
			Description: "Audit items from a hand-authored child command.",
			Example:     "existingnovel-pp-cli items audit --dry-run",
		},
	}
	stderr, err := captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.Contains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go without expected constructor newNovelItemsAuditCmd; skipping novel stub`)
	assert.NotContains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go; leaving existing file unchanged`)

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newExportCmd(flags))")
	assert.NotContains(t, root, "newNovelExportCmd")

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.NotContains(t, parent, "newNovelItemsAuditCmd")
	requireGeneratedCompiles(t, outputDir)

	stderr, err = captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.Contains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go without expected constructor newNovelItemsAuditCmd; skipping novel stub`)
	assert.NotContains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go; leaving existing file unchanged`)
	root = readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newExportCmd(flags))")
	assert.NotContains(t, root, "newNovelExportCmd")
	parent = readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.NotContains(t, parent, "newNovelItemsAuditCmd")
	requireGeneratedCompiles(t, outputDir)
}

func captureNovelFeatureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldErr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		os.Stderr = oldErr
	}()
	callErr := fn()
	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return string(data), callErr
}

func TestGeneratorWiresNovelChildrenUnderPromotedResource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promonovel")
	apiSpec.Resources = map[string]spec.Resource{
		"qr": {
			Description: "Manage QR codes",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/qr/{id}", Description: "Get QR code"},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "QR watcher",
			Command:     "qr --watch",
			Description: "Watch the promoted QR command.",
			Example:     "promonovel-pp-cli qr qr-123 --watch",
		},
		{
			Name:        "QR health",
			Command:     "qr health --limit 10",
			Description: "Summarize QR health.",
			Example:     "promonovel-pp-cli qr health --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newQrPromotedCmd(flags))")
	assert.NotContains(t, root, "newNovelQrCmd")

	promoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_qr.go")
	assert.Contains(t, promoted, "cmd.AddCommand(newNovelQrHealthCmd(flags))")
	health := readGeneratedFile(t, outputDir, "internal", "cli", "qr_health.go")
	assert.Contains(t, health, `TODO: implement novel feature %q", "qr health"`)
	requireGeneratedCompiles(t, outputDir)
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
			Name:        "Filter",
			Command:     "filter --state [active|inactive]",
			Description: "Search items, filtered by flag.",
			Example:     "novelargs-pp-cli filter --state active",
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
	filter := readGeneratedFile(t, outputDir, "internal", "cli", "filter.go")
	assert.NotContains(t, filter, "return cmd.Help()")
	assert.Contains(t, filter, "// validate required flags here")
	assert.NotContains(t, filter, "[active|inactive]")
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
