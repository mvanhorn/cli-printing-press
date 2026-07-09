package generator

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
	gen.VisionSet = VisionTemplateSet{Import: true, Store: true, Search: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.NotContains(t, rootSrc, "newSyncCmd(flags)")
	require.NotContains(t, rootSrc, "newSearchCmd(flags)")
	require.NotContains(t, doctorSrc, `report["cache"]`)
	require.NotContains(t, doctorSrc, "collectCacheReport")

	requireGeneratedCompiles(t, outputDir)
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
