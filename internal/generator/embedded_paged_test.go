package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedPagedHelperEmitted_NonPromoted ensures the generator emits
// fetchFull<Endpoint><Property> companion helpers for the non-promoted
// command-emission path. The multi-endpoint resource here keeps the GET
// from being promoted, so command_endpoint.go.tmpl is the active path.
func TestEmbeddedPagedHelperEmitted_NonPromoted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("embedded-paged-nonpromoted")
	apiSpec.Resources = map[string]spec.Resource{
		"playlists": {
			Description: "Manage playlists",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/playlists", Description: "List playlists"},
				"get": {
					Method:      "GET",
					Path:        "/playlists/{id}",
					Description: "Get one playlist",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
					},
					EmbeddedPagedSubresources: []spec.EmbeddedPagedSubresource{
						{
							Property:   "tracks",
							ChildPath:  "/playlists/{id}/tracks",
							ItemsField: "items",
							NextField:  "next",
						},
					},
				},
			},
		},
	}

	outDir := filepath.Join(t.TempDir(), "embedded-paged-nonpromoted-pp-cli")
	require.NoError(t, New(apiSpec, outDir).Generate())

	getSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "playlists_get.go"))
	require.NoError(t, err)
	body := string(getSrc)
	assert.Contains(t, body, "func fetchFullPlaylistsGetTracks(",
		"non-promoted GET should emit a fetchFull<Resource><Endpoint><Property> helper")
	assert.Contains(t, body, "childPath := \"/playlists/{id}/tracks\"",
		"helper should hard-code the conventional child path so callers pass only path-param values")
	assert.Contains(t, body, "fetchEmbeddedPagedSubresource(ctx, c, childPath,",
		"per-endpoint helper should delegate to the shared runtime helper rather than open-coding the page loop")

	helpersSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.Contains(t, string(helpersSrc), "func fetchEmbeddedPagedSubresource(",
		"helpers.go must emit the shared paginator when at least one endpoint has detected sub-resources")
	assert.Contains(t, string(helpersSrc), "GetWithHeadersValues(ctx context.Context, path string, params url.Values, headers map[string]string) (json.RawMessage, error)",
		"URL-following helpers must depend on the url.Values client path so repeated query params survive passthrough")
	assert.Contains(t, string(helpersSrc), "c.GetWithHeadersValues(ctx, target, query, nil)",
		"URL-following helpers must pass parsed query values rather than collapsing them to one string per key")
	assert.NotContains(t, string(helpersSrc), "func relativizeNextURL(raw string) string",
		"generated helpers should not emit a dead wrapper around the structured URL passthrough helper")
	assert.Contains(t, string(helpersSrc), "embeddedPagedSubresourcePageCap",
		"shared helper must carry the page-count cap so callers can't spin against a misbehaving server")

	runGoCommand(t, outDir, "build", "./internal/cli")
}

// TestEmbeddedPagedHelperEmitted_Promoted covers the single-endpoint
// resource case where the generator promotes the GET to a top-level
// command (command_promoted.go.tmpl is the active path). The helper
// must appear there too — otherwise the bug only fixes itself for
// CLIs whose specs happen to declare a sibling list endpoint.
func TestEmbeddedPagedHelperEmitted_Promoted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("embedded-paged-promoted")
	apiSpec.Resources = map[string]spec.Resource{
		"playlists": {
			Description: "Manage playlists",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/playlists/{id}",
					Description: "Get one playlist",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
					},
					EmbeddedPagedSubresources: []spec.EmbeddedPagedSubresource{
						{
							Property:   "tracks",
							ChildPath:  "/playlists/{id}/tracks",
							ItemsField: "items",
							NextField:  "next",
						},
					},
				},
			},
		},
	}

	outDir := filepath.Join(t.TempDir(), "embedded-paged-promoted-pp-cli")
	require.NoError(t, New(apiSpec, outDir).Generate())

	promotedSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "promoted_playlists.go"))
	require.NoError(t, err)
	assert.Contains(t, string(promotedSrc), "func fetchFullPlaylistsGetTracks(",
		"promoted-command file should carry the same helper as the non-promoted file")

	runGoCommand(t, outDir, "build", "./internal/cli")
}

// TestEmbeddedPagedHelperEmitted_HasMore exercises the boolean has_more
// branch — the emitted helper must declare it stops after page 1 rather
// than silently falling through and looping forever against a header-
// driven cursor it cannot synthesize.
func TestEmbeddedPagedHelperEmitted_HasMore(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("embedded-paged-hasmore")
	apiSpec.Resources = map[string]spec.Resource{
		"customers": {
			Description: "Customers",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/customers", Description: "List customers"},
				"get": {
					Method:      "GET",
					Path:        "/customers/{id}",
					Description: "Get customer",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
					},
					EmbeddedPagedSubresources: []spec.EmbeddedPagedSubresource{
						{
							Property:      "subscriptions",
							ChildPath:     "/customers/{id}/subscriptions",
							ItemsField:    "data",
							NextField:     "has_more",
							NextIsBoolean: true,
						},
					},
				},
			},
		},
	}

	outDir := filepath.Join(t.TempDir(), "embedded-paged-hasmore-pp-cli")
	require.NoError(t, New(apiSpec, outDir).Generate())

	getSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "customers_get.go"))
	require.NoError(t, err)
	body := string(getSrc)
	assert.Contains(t, body, "func fetchFullCustomersGetSubscriptions(")
	assert.Contains(t, body, `fetchEmbeddedPagedSubresource(ctx, c, childPath, "has_more", false, true)`,
		"has_more-style envelopes should delegate to the shared paginator with nextIsURL=false, nextIsBoolean=true")

	runGoCommand(t, outDir, "build", "./internal/cli")
}

// TestEmbeddedPagedHelperNotEmittedWhenEmpty confirms the helper block
// is gated correctly: endpoints without detected sub-resources must not
// emit any fetchFull function (avoids template noise and stray
// references to undefined identifiers).
func TestEmbeddedPagedHelperNotEmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("embedded-paged-empty")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"get": {
					Method:      "GET",
					Path:        "/items/{id}",
					Description: "Get item",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
					},
				},
			},
		},
	}

	outDir := filepath.Join(t.TempDir(), "embedded-paged-empty-pp-cli")
	require.NoError(t, New(apiSpec, outDir).Generate())

	getSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "items_get.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(getSrc), "fetchFull",
		"endpoints with no detected embedded paged sub-resources must not emit a helper")

	helpersSrc, err := os.ReadFile(filepath.Join(outDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(helpersSrc), "fetchEmbeddedPagedSubresource",
		"shared paginator must be gated on HasEmbeddedPaged so unused CLIs don't carry dead code")
}
