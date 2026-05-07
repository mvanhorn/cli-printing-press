package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromotedHasStorePostRoutesThroughVerbBranch is #425's interim-fix
// guard: HasStore + POST must route through c.Post (live API call) rather
// than resolveRead, which is GET-only internally. Pre-fix the template
// emitted resolveRead unconditionally for HasStore and produced
// uncompilable code for non-GET endpoints.
func TestPromotedHasStorePostRoutesThroughVerbBranch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("poststore")
	apiSpec.Resources = map[string]spec.Resource{
		"searches": {
			Description: "Search across resources by free-text query",
			Endpoints: map[string]spec.Endpoint{
				"searchAll": {
					Method:      "POST",
					Path:        "/search-all",
					Description: "Search by free-text query across all entities",
					Body: []spec.Param{
						{Name: "query", Type: "string", Description: "Free-text query"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	promotedPath := filepath.Join(outputDir, "internal", "cli", "promoted_searches.go")
	require.FileExists(t, promotedPath,
		"single-endpoint POST resource with read-shaped operationId 'searchAll' must produce a promoted command")

	src, err := os.ReadFile(promotedPath)
	require.NoError(t, err)
	got := string(src)

	assert.NotContains(t, got, "resolveRead(",
		"HasStore + POST must NOT route through resolveRead (GET-only internally)")
	assert.Contains(t, got, "c.Post(path, body)",
		"HasStore + POST must route through the verb branch with a built body")
	assert.Contains(t, got, `attachFreshness(DataProvenance{Source: "live"}, flags)`,
		"non-GET HasStore commands must synthesize a live-call prov so the downstream HasStore block compiles")
}

// TestPromotedHasStoreGetStillUsesResolveRead is the byte-compat guard for
// the GET happy path. The golden harness also covers this; the assertion
// is faster and more explicit when the template gate gets touched.
func TestPromotedHasStoreGetStillUsesResolveRead(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("getstore")
	apiSpec.Resources = map[string]spec.Resource{
		"status": {
			Description: "Public status",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/status",
					Description: "Get current status",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	promotedPath := filepath.Join(outputDir, "internal", "cli", "promoted_status.go")
	src, err := os.ReadFile(promotedPath)
	require.NoError(t, err)
	got := string(src)

	assert.Contains(t, got, "resolveRead(",
		"HasStore + GET must keep routing through resolveRead (the cached fast-path)")
	assert.NotContains(t, got, "c.Get(path, params)",
		"HasStore + GET must not also emit a direct c.Get call (would mean both branches fired)")
}

// TestPromotedHasStoreDeleteSynthesizesProv covers the DELETE branch the
// other tests don't reach. The provenance synthesis lives in a single
// post-chain block, so a regression that drops it from one verb shape
// drops it from all of them — but DELETE has its own pre-existing
// `data, _, err := c.Delete(path)` call site, so an explicit assertion
// guards against template refactors that re-introduce the dup.
func TestPromotedHasStoreDeleteSynthesizesProv(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("delstore")
	apiSpec.Resources = map[string]spec.Resource{
		"sessions": {
			Description: "Session management",
			Endpoints: map[string]spec.Endpoint{
				"revokeAll": {
					Method:      "DELETE",
					Path:        "/sessions",
					Description: "Revoke all active sessions",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	promotedPath := filepath.Join(outputDir, "internal", "cli", "promoted_sessions.go")
	src, err := os.ReadFile(promotedPath)
	require.NoError(t, err)
	got := string(src)

	assert.NotContains(t, got, "resolveRead(",
		"HasStore + DELETE must NOT route through resolveRead")
	assert.Contains(t, got, "c.Delete(path)",
		"HasStore + DELETE must route through c.Delete")
	assert.Contains(t, got, `attachFreshness(DataProvenance{Source: "live"}, flags)`,
		"HasStore + DELETE must synthesize a live-call prov for the downstream provenance block")
}
