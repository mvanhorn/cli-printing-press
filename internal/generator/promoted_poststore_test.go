package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromotedHasStorePostRoutesThroughVerbBranch guards #425's interim
// fix: a HasStore + POST endpoint emitted as a promoted command must
// route through c.Post (live API call) instead of resolveRead, which is
// GET-only internally. Pre-fix the template called resolveRead unconditionally
// for HasStore and produced broken code (params built but no body, calling
// a POST endpoint as if it were GET-with-params).
//
// The cached-read path for POST searches needs a body-aware helper
// (resolvePostRead) that doesn't exist yet. Until a second store-backed
// POST-search consumer ships, the promoted command makes a live call —
// no worse than the typed `<resource> <endpoint>` form, which has the
// same behavior for the same shape.
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
		"HasStore + POST must NOT route through resolveRead (GET-only internally; was the broken pre-fix behavior)")
	assert.Contains(t, got, "c.Post(path, body)",
		"HasStore + POST must route through the verb branch with a built body, mirroring command_endpoint.go.tmpl")
}

// TestPromotedHasStoreGetStillUsesResolveRead is the byte-compat guard:
// the fix narrows the resolveRead branch to GET only, and we want to
// confirm GET endpoints (the existing happy path) keep routing through
// resolveRead. The golden harness also covers this; the test is a
// faster, more explicit signal when the template gate gets touched.
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
