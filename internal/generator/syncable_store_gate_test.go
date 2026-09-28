package generator

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateSyncableSmallAPIEmitsLocalDataLayer(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("small-syncable")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))

	// The store must expose BareResourceID so novel commands can recover bare
	// entity ids from the composite (id+NUL+parent) storage keys ListIDs returns
	// for parent-keyed dependent resources.
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	require.Contains(t, string(storeSrc), "func BareResourceID(",
		"store must expose BareResourceID for composite dependent-resource keys")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratePostOnlyAPIStillSkipsLocalDataLayer(t *testing.T) {
	t.Parallel()

	apiSpec := postOnlyOutputSpec("post-only-output")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))

	_, err := os.Stat(filepath.Join(outputDir, "internal", "store"))
	require.True(t, os.IsNotExist(err), "post-only API must not reserve internal/store")
}

func TestGenerateZeroSyncableAPIOmitsSyncAndDoctorCache(t *testing.T) {
	t.Parallel()

	apiSpec := zeroSyncableQuerySpec("zero-syncable-query")
	apiSpec.Cache.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Import: true,
		Store:  true,
		Search: true,
		Sync:   true,
		MCP:    true,
		Workflows: []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		},
	}
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "data_source.go"))
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.NotContains(t, rootSrc, "newSyncCmd(flags)")
	require.NotContains(t, rootSrc, "newSearchCmd(flags)")
	require.NotContains(t, doctorSrc, `report["cache"]`)
	require.NotContains(t, doctorSrc, "collectCacheReport")
	require.NotContains(t, dataSourceSrc, "emitSyncHints")
	require.NotContains(t, dataSourceSrc, "Run 'zero-syncable-query-pp-cli sync' first")
	require.Equal(t, 6, strings.Count(dataSourceSrc, "Populate the local store through a custom store-backed command first."))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync_hint.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync_hint_test.go"))
	require.Contains(t, mcpSrc, `mcplib.NewTool("sql"`)
	for _, file := range []string{"pm_stale.go", "pm_orphans.go", "pm_load.go"} {
		workflowSrc := readGeneratedFile(t, outputDir, "internal", "cli", file)
		require.NotContains(t, workflowSrc, "maybeEmitSyncHints")
	}

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./...")
}

func TestConstrainVisionTemplatesKeepsStreamingSyncWhenProfileHasNoBulkResources(t *testing.T) {
	t.Parallel()

	apiSpec := zeroSyncableQuerySpec("streaming-zero-syncable")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Streaming = spec.StreamingConfig{
		Transport:      spec.StreamingTransportWebSocket,
		URL:            "wss://api.example.com/v1/ws",
		SubscribeShape: `{"type":"subscribe","channels":["events"]}`,
		Framing:        spec.StreamingFramingNDJSON,
	}
	visionSet := constrainVisionTemplates(
		apiSpec,
		VisionTemplateSet{Store: true, Search: true, Sync: true, MCP: true},
		&profiler.APIProfile{},
		io.Discard,
	)

	require.True(t, visionSet.Store)
	require.True(t, visionSet.Sync)
}

// TestConstrainVisionTemplatesLearnPromotesStoreAndSyncForSyncable pins the
// learn store promotion inside constrain: a learn-enabled spec whose
// VisionSet skipped Store gets Store promoted, and because the profile has
// non-vestigial syncable resources the Store-forces-Sync invariant is
// re-derived (SelectVisionTemplates already ran, so constrain must reapply
// it). The info line tells operators why a thin CLI grew a store.
func TestConstrainVisionTemplatesLearnPromotesStoreAndSyncForSyncable(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("learn-syncable-promote")
	apiSpec.Learn.Enabled = true
	profile := profiler.Profile(apiSpec)
	require.True(t, hasSyncCommandResources(profile), "fixture must have non-vestigial syncable resources")

	var buf bytes.Buffer
	visionSet := constrainVisionTemplates(apiSpec, VisionTemplateSet{MCP: true}, profile, &buf)

	require.True(t, visionSet.Store)
	require.True(t, visionSet.Sync)
	require.Contains(t, buf.String(), "learn.enabled promotes VisionSet.Store=true")
}

// TestConstrainVisionTemplatesLearnZeroSyncableKeepsStoreDropsSync pins the
// zero-syncable half of the promotion: learn forces Store, but with no
// syncable resources the existing strip still drops sync/search/analytics,
// leaving the compile-pinned forced-store shape.
func TestConstrainVisionTemplatesLearnZeroSyncableKeepsStoreDropsSync(t *testing.T) {
	t.Parallel()

	apiSpec := zeroSyncableQuerySpec("learn-zero-syncable")
	apiSpec.Learn.Enabled = true

	var buf bytes.Buffer
	visionSet := constrainVisionTemplates(apiSpec, VisionTemplateSet{MCP: true}, profiler.Profile(apiSpec), &buf)

	require.True(t, visionSet.Store)
	require.False(t, visionSet.Sync)
	require.False(t, visionSet.Search)
	require.False(t, visionSet.Analytics)
	require.Contains(t, buf.String(), "learn.enabled promotes VisionSet.Store=true")
}

func TestGenerateLearnZeroSyncableKeepsLearnStoreWithoutSyncBackedSurfaces(t *testing.T) {
	t.Parallel()

	apiSpec := zeroSyncableQuerySpec("learn-only-store")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync_hint.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "data_source.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.NotContains(t, rootSrc, "newWorkflowCmd(flags)")
	require.NotContains(t, rootSrc, `"data-source"`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.NotContains(t, mcpSrc, `mcplib.NewTool("sql"`)
	require.NotContains(t, mcpSrc, "Run learn-only-store-pp-cli sync")

	recallSrc := readGeneratedFile(t, outputDir, "internal", "learn", "recall.go")
	require.NotContains(t, recallSrc, "run sync to refresh entity lookups")
	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")
	require.NotContains(t, skillSrc, "Run `learn-only-store-pp-cli sync` to refresh entity lookups")

	requireGeneratedCompiles(t, outputDir)
}

// TestConstrainVisionTemplatesLearnDisabledDoesNotPromote pins that a spec
// without learn enabled keeps today's behavior byte-for-byte: no promotion,
// no info line.
func TestConstrainVisionTemplatesLearnDisabledDoesNotPromote(t *testing.T) {
	t.Parallel()

	apiSpec := postOnlyOutputSpec("learn-off-no-promote")
	apiSpec.Learn.Disabled = true

	var buf bytes.Buffer
	visionSet := constrainVisionTemplates(apiSpec, VisionTemplateSet{MCP: true}, profiler.Profile(apiSpec), &buf)

	require.False(t, visionSet.Store)
	require.False(t, visionSet.Sync)
	require.Empty(t, buf.String())
}

func TestGenerateReadOnlyAPIWithoutCreateOmitsImportAndIdempotent(t *testing.T) {
	t.Parallel()

	apiSpec := readOnlyCollectionSpec("readonly-no-create")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Import: true, Store: true, Search: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "import.go"))

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	readmeSrc := readGeneratedFile(t, outputDir, "README.md")
	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")
	require.NotContains(t, rootSrc, "newImportCmd(flags)")
	require.NotContains(t, rootSrc, "idempotent")
	require.NotContains(t, readmeSrc, "--idempotent")
	require.NotContains(t, skillSrc, "--idempotent")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBareParentDeclaresTypedExitCodeTwo(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("typed-parent-exit")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	parentSrc := readGeneratedFile(t, outputDir, "internal", "cli", "deliveries.go")
	require.Contains(t, parentSrc, `"mcp:read-only": "true"`)
	require.Contains(t, parentSrc, `"pp:typed-exit-codes": "0,2"`)

	requireGeneratedCompiles(t, outputDir)
}

func smallReadWriteSyncableOutputSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"deliveries": {
			Description: "Manage deliveries",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/deliveries",
					Description: "List deliveries",
					Response:    spec.ResponseDef{Type: "object", Item: "DeliveriesResponse"},
				},
				"add": {
					Method:      "POST",
					Path:        "/add-delivery",
					Description: "Add delivery",
					Body: []spec.Param{
						{Name: "tracking_number", Type: "string", Required: true},
						{Name: "carrier_code", Type: "string", Required: true},
						{Name: "description", Type: "string", Required: true},
					},
					Response: spec.ResponseDef{Type: "object", Item: "SuccessResponse"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Delivery": {
			Fields: []spec.TypeField{
				{Name: "carrier_code", Type: "string"},
				{Name: "description", Type: "string"},
				{Name: "status_code", Type: "integer"},
				{Name: "tracking_number", Type: "string"},
			},
		},
		"DeliveriesResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
				{Name: "deliveries", Type: "array"},
			},
		},
		"SuccessResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
			},
		},
	}
	return apiSpec
}

func postOnlyOutputSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"deliveries": {
			Description: "Manage deliveries",
			Endpoints: map[string]spec.Endpoint{
				"add": {
					Method:      "POST",
					Path:        "/add-delivery",
					Description: "Add delivery",
					Body: []spec.Param{
						{Name: "tracking_number", Type: "string", Required: true},
					},
					Response: spec.ResponseDef{Type: "object", Item: "SuccessResponse"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"SuccessResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
			},
		},
	}
	return apiSpec
}

func zeroSyncableQuerySpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"recipes": {
			Description: "Search HTML pages",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         "GET",
					Path:           "/recipe/{recipe_id}/{slug}",
					Description:    "Get recipe page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
					Params: []spec.Param{
						{Name: "recipe_id", Type: "string", Required: true, PathParam: true},
						{Name: "slug", Type: "string", Required: true, PathParam: true},
					},
					Response: spec.ResponseDef{Type: "object"},
				},
				"query": {
					Method:         "GET",
					Path:           "/search",
					Description:    "Search pages",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
					Params: []spec.Param{
						{Name: "q", Type: "string", Required: true},
					},
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}
	return apiSpec
}

func readOnlyCollectionSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Read widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
					Response:    spec.ResponseDef{Type: "array"},
				},
				"get": {
					Method:      "GET",
					Path:        "/widgets/{id}",
					Description: "Get widget",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, PathParam: true, Positional: true},
					},
					Response: spec.ResponseDef{Type: "object"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Widget": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}
	return apiSpec
}

const typedExitCodesAnnotationLiteral = `"pp:typed-exit-codes": "0,2"`

const parentNoSubcommandCall = "parentNoSubcommandRunE(flags)"

// authParentNoSubcommandTemplates stay unannotated. Verify classifies auth
// as local and skips the Execute leg; live dogfood skips auth entirely.
var authParentNoSubcommandTemplates = map[string]struct{}{
	"auth.go.tmpl":                    {},
	"auth_browser.go.tmpl":            {},
	"auth_client_credentials.go.tmpl": {},
	"auth_device_code.go.tmpl":        {},
	"auth_simple.go.tmpl":             {},
}

func TestParentNoSubcommandRunESitesDeclareTypedExitCodes(t *testing.T) {
	t.Parallel()

	declared := map[string]int{}
	skipped := map[string]int{}
	err := filepath.WalkDir("templates", func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		srcBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		src := string(srcBytes)
		base := filepath.Base(path)
		_, allowAuth := authParentNoSubcommandTemplates[base]
		for _, site := range parentNoSubcommandSites(src) {
			if allowAuth {
				skipped[base]++
				continue
			}
			if !site.hasCommand || !strings.Contains(site.block, typedExitCodesAnnotationLiteral) {
				t.Errorf("%s:%d: parentNoSubcommandRunE site missing %s", path, site.line, typedExitCodesAnnotationLiteral)
				continue
			}
			declared[base]++
		}
		if base == "novel_feature_command.go.tmpl" {
			if !strings.Contains(src, `{{if not .Feature}}, "pp:typed-exit-codes": "0,2"{{end}}`) {
				t.Errorf("%s: typed exit codes must be gated to the non-feature parent branch", path)
			}
			if strings.Contains(src, `{{if .Feature}}, "pp:typed-exit-codes"`) {
				t.Errorf("%s: feature leaf branch must stay unannotated", path)
			}
		}
		return nil
	})
	require.NoError(t, err)

	for _, name := range []string{
		"profile.go.tmpl",
		"teach_playbook.go.tmpl",
		"channel_workflow.go.tmpl",
		"jobs.go.tmpl",
		"share_commands.go.tmpl",
		"novel_feature_command.go.tmpl",
		"command_parent.go.tmpl",
		"teach.go.tmpl",
	} {
		if declared[name] == 0 {
			t.Errorf("template %s has no parentNoSubcommandRunE site declaring %s", name, typedExitCodesAnnotationLiteral)
		}
	}
	for name := range authParentNoSubcommandTemplates {
		if skipped[name] == 0 {
			t.Errorf("auth allowlist entry %s has no parentNoSubcommandRunE site", name)
		}
	}
}

func TestGeneratedFrameworkParentsDeclareTypedExitCodes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("framework-parent-exits")
	apiSpec.Types = map[string]spec.TypeDef{
		"RenderJob": {Fields: []spec.TypeField{
			{Name: "job_id", Type: "string"},
			{Name: "status", Type: "string"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"renders": {
			Description: "Async render jobs",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/renders", Description: "List renders"},
				"submit": {Method: "POST", Path: "/renders", Description: "Submit a render", Response: spec.ResponseDef{Type: "object", Item: "RenderJob"}},
				"get":    {Method: "GET", Path: "/renders/{id}", Description: "Get a render", Response: spec.ResponseDef{Type: "object", Item: "RenderJob"}},
			},
		},
	}
	apiSpec.Share = spec.ShareConfig{Enabled: true, SnapshotTables: []string{"renders"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	for _, tc := range []struct {
		file   string
		parent string
		leaf   string
	}{
		{"profile.go", "newProfileCmd", "newProfileListCmd"},
		{"teach_playbook.go", "newPlaybookCmd", "newPlaybookListCmd"},
		{"channel_workflow.go", "newWorkflowCmd", "newWorkflowStatusCmd"},
		{"jobs.go", "newJobsCmd", "newJobsListCmd"},
		{"share_commands.go", "newShareCmd", "newShareExportCmd"},
	} {
		src := readGeneratedFile(t, outputDir, "internal", "cli", tc.file)
		parent, ok := commandFuncSource(src, tc.parent)
		require.Truef(t, ok, "missing %s in %s", tc.parent, tc.file)
		require.Containsf(t, parent, typedExitCodesAnnotationLiteral, "%s must declare typed exit codes", tc.parent)
		leaf, ok := commandFuncSource(src, tc.leaf)
		require.Truef(t, ok, "missing %s in %s", tc.leaf, tc.file)
		require.NotContainsf(t, leaf, typedExitCodesAnnotationLiteral, "%s must stay unannotated", tc.leaf)
	}
}

type parentNoSubcommandSite struct {
	line       int
	block      string
	hasCommand bool
}

func parentNoSubcommandSites(src string) []parentNoSubcommandSite {
	var sites []parentNoSubcommandSite
	searchFrom := 0
	for {
		rel := strings.Index(src[searchFrom:], parentNoSubcommandCall)
		if rel < 0 {
			return sites
		}
		pos := searchFrom + rel
		searchFrom = pos + len(parentNoSubcommandCall)
		lineStart := strings.LastIndex(src[:pos], "\n") + 1
		lineEnd := strings.Index(src[pos:], "\n")
		if lineEnd < 0 {
			lineEnd = len(src)
		} else {
			lineEnd = pos + lineEnd
		}
		if strings.Contains(src[lineStart:lineEnd], "func ") {
			continue
		}
		site := parentNoSubcommandSite{line: strings.Count(src[:pos], "\n") + 1}
		if cmdStart := strings.LastIndex(src[:pos], "&cobra.Command{"); cmdStart >= 0 {
			site.hasCommand = true
			site.block = src[cmdStart:pos]
		}
		sites = append(sites, site)
	}
}

func commandFuncSource(src, name string) (string, bool) {
	marker := "func " + name + "("
	start := strings.Index(src, marker)
	if start < 0 {
		return "", false
	}
	rest := src[start:]
	if next := strings.Index(rest[len(marker):], "\nfunc "); next >= 0 {
		return rest[:len(marker)+next], true
	}
	return rest, true
}
