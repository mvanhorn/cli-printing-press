package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointIsWriteCommand covers the semantic-aware write classifier from
// retro #423 (F1). The previous methodIsWrite returned true for every POST,
// which produced the wrong README boilerplate for read-only APIs that use
// POST for queries (search-all endpoints, GraphQL operations, RPC-style
// search APIs). The new endpointIsWriteCommand combines verb + operationId
// prefix + body shape + mcp:read-only annotation so genuine queries flip
// HasWriteCommands false and genuine mutations stay true.
func TestEndpointIsWriteCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		opName   string
		endpoint spec.Endpoint
		want     bool
	}{
		{
			name:     "GET endpoint is read",
			opName:   "listUsers",
			endpoint: spec.Endpoint{Method: "GET", Path: "/users"},
			want:     false,
		},
		{
			name:     "HEAD endpoint is read",
			opName:   "headStatus",
			endpoint: spec.Endpoint{Method: "HEAD", Path: "/status"},
			want:     false,
		},
		{
			name:   "POST search endpoint is read (operationId prefix searchAll)",
			opName: "searchAll",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/search-all",
				Body: []spec.Param{
					{Name: "queryText", Type: "string"},
					{Name: "size", Type: "integer"},
					{Name: "from", Type: "integer"},
				},
			},
			want: false,
		},
		{
			name:   "POST GraphQL is read (operationId prefix query)",
			opName: "query",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/graphql",
				Body: []spec.Param{
					{Name: "query", Type: "string"},
					{Name: "variables", Type: "object"},
				},
			},
			want: false,
		},
		{
			name:   "POST list-style is read (operationId prefix list)",
			opName: "listFilteredItems",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/items/list",
				Body: []spec.Param{
					{Name: "filter", Type: "object"},
				},
			},
			want: false,
		},
		{
			name:     "POST find-style is read",
			opName:   "findCustomers",
			endpoint: spec.Endpoint{Method: "POST", Path: "/customers/find"},
			want:     false,
		},
		{
			name:     "POST count-style is read",
			opName:   "countOrders",
			endpoint: spec.Endpoint{Method: "POST", Path: "/orders/count"},
			want:     false,
		},
		{
			name:     "POST fetch-style is read",
			opName:   "fetchEvents",
			endpoint: spec.Endpoint{Method: "POST", Path: "/events/fetch"},
			want:     false,
		},
		{
			name:     "POST describe-style is read",
			opName:   "describeWorkspace",
			endpoint: spec.Endpoint{Method: "POST", Path: "/workspaces/describe"},
			want:     false,
		},
		{
			name:   "POST create endpoint stays write",
			opName: "createUser",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/users",
				Body: []spec.Param{
					{Name: "name", Type: "string"},
					{Name: "email", Type: "string"},
					{Name: "role", Type: "string"},
				},
			},
			want: true,
		},
		{
			name:     "POST add endpoint stays write",
			opName:   "addItemToCart",
			endpoint: spec.Endpoint{Method: "POST", Path: "/cart/items"},
			want:     true,
		},
		{
			name:     "PUT update endpoint stays write",
			opName:   "updateUser",
			endpoint: spec.Endpoint{Method: "PUT", Path: "/users/{id}"},
			want:     true,
		},
		{
			name:     "PATCH partial-update endpoint stays write",
			opName:   "patchOrder",
			endpoint: spec.Endpoint{Method: "PATCH", Path: "/orders/{id}"},
			want:     true,
		},
		{
			name:     "DELETE endpoint stays write",
			opName:   "deleteUser",
			endpoint: spec.Endpoint{Method: "DELETE", Path: "/users/{id}"},
			want:     true,
		},
		{
			name:     "POST endpoint with no body and no semantic signal is write (fail-closed)",
			opName:   "doSomething",
			endpoint: spec.Endpoint{Method: "POST", Path: "/something"},
			want:     true,
		},
		{
			name:   "POST endpoint with mcp:read-only annotation is read regardless of name",
			opName: "doMutation",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/widgets",
				Meta:   map[string]string{"mcp:read-only": "true"},
			},
			want: false,
		},
		{
			name:     "operationId prefix matching is case-insensitive",
			opName:   "SearchCollections",
			endpoint: spec.Endpoint{Method: "POST", Path: "/search/collections"},
			want:     false,
		},
		{
			name:   "POST with only filter-shape body params is read",
			opName: "doQuery",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/widgets/query",
				Body: []spec.Param{
					{Name: "filter", Type: "object"},
					{Name: "limit", Type: "integer"},
					{Name: "offset", Type: "integer"},
					{Name: "sort", Type: "string"},
				},
			},
			want: false,
		},
		{
			name:   "POST with mixed filter and write-shape body params stays write",
			opName: "doStuff",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/widgets",
				Body: []spec.Param{
					{Name: "filter", Type: "object"},
					{Name: "name", Type: "string"}, // write-shape
				},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := endpointIsWriteCommand(tc.endpoint, tc.opName)
			assert.Equal(t, tc.want, got, "endpointIsWriteCommand(%q) returned wrong classification", tc.opName)
		})
	}
}

// TestHasWriteCommands_PostAsQueryFlipsHasWriteFalse verifies that the upgrade
// to endpointIsWriteCommand propagates correctly through resourceHasWriteCommand
// and hasWriteCommands. A spec with only POST search endpoints should report
// HasWriteCommands as false so the README emits "Read-only by default" instead
// of the Retryable/Confirmable/Piped-input boilerplate.
func TestHasWriteCommands_PostAsQueryFlipsHasWriteFalse(t *testing.T) {
	t.Parallel()

	resources := map[string]spec.Resource{
		"search": {
			Description: "Search the public network",
			Endpoints: map[string]spec.Endpoint{
				"searchAll": {
					Method: "POST",
					Path:   "/search-all",
					Body: []spec.Param{
						{Name: "queryText", Type: "string"},
					},
				},
			},
		},
	}

	assert.False(t, hasWriteCommands(resources),
		"a resource with only POST search endpoints should classify as read-only")
}

// TestPromotedCommand_PostEndpointEmitsPost is the integration counterpart
// to TestEndpointIsWriteCommand. The previous template hardcoded
// `c.Get(path, params)` for every promoted endpoint, so promoting a POST
// endpoint produced a runtime HTTP 400 from the wrong-verb mismatch. The
// fix adds a verb branch in command_promoted.go.tmpl that emits
// `c.Post(path, body)` for non-GET endpoints. This test builds a fixture
// CLI with a single POST endpoint (which gets promoted) and asserts the
// emitted promoted command file contains c.Post, not c.Get.
func TestPromotedCommand_PostEndpointEmitsPost(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("post-promoted")
	// Replace the default GET-only resource with one that has a single
	// POST endpoint — single-endpoint resources get promoted to top-level
	// commands.
	apiSpec.Resources = map[string]spec.Resource{
		"queries": {
			Description: "Search the network",
			Endpoints: map[string]spec.Endpoint{
				"searchAll": {
					Method:      "POST",
					Path:        "/search-all",
					Description: "Search collections by free text",
					Body: []spec.Param{
						{Name: "queryText", Type: "string"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "post-promoted-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Locate the promoted command file. The generator's promote logic
	// emits `promoted_<resource-or-endpoint>.go`; check both possibilities.
	candidates := []string{
		filepath.Join(outputDir, "internal", "cli", "promoted_queries.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_search-all.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_searchall.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_searchAll.go"),
	}
	var found string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			found = c
			break
		}
	}
	require.NotEmpty(t, found, "expected a promoted command file in internal/cli/")

	content, err := os.ReadFile(found)
	require.NoError(t, err)
	src := string(content)

	require.Contains(t, src, "c.Post(",
		"a POST-only promoted endpoint should emit c.Post, not c.Get")
	require.NotContains(t, src, "c.Get(path, params)",
		"the promoted POST endpoint should NOT contain a c.Get(path, params) call (verb-incorrect path)")
}

// TestPromotedCommand_GetEndpointStillEmitsGet is the negative guard for
// the verb branch. GET endpoints continue to emit c.Get(path, params)
// byte-identically — the verb branch only changes behavior for non-GET.
func TestPromotedCommand_GetEndpointStillEmitsGet(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("get-promoted")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Public items",
			Endpoints: map[string]spec.Endpoint{
				"listItems": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "get-promoted-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	candidates := []string{
		filepath.Join(outputDir, "internal", "cli", "promoted_items.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_list-items.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_listitems.go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_listItems.go"),
	}
	var found string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			found = c
			break
		}
	}
	require.NotEmpty(t, found, "expected a promoted command file in internal/cli/")

	content, err := os.ReadFile(found)
	require.NoError(t, err)
	src := string(content)

	// GET endpoints should never emit c.Post/Put/Patch/Delete in the
	// promoted command, even when the HasStore branch routes through
	// resolveRead instead of c.Get directly. The verb branch only
	// changes behavior for non-GET methods.
	require.NotContains(t, src, "c.Post(",
		"a GET promoted endpoint must not contain c.Post — verb branch only fires for non-GET")
	require.NotContains(t, src, "c.Put(",
		"a GET promoted endpoint must not contain c.Put")
	require.NotContains(t, src, "c.Patch(",
		"a GET promoted endpoint must not contain c.Patch")
}

// TestHasWriteCommands_GenuineMutationsStayTrue is the negative guard: an
// API with POST createUser still classifies as write so its README keeps
// the Retryable boilerplate. Prevents over-broad application of the new
// semantic signals.
func TestHasWriteCommands_GenuineMutationsStayTrue(t *testing.T) {
	t.Parallel()

	resources := map[string]spec.Resource{
		"users": {
			Description: "User accounts",
			Endpoints: map[string]spec.Endpoint{
				"createUser": {
					Method: "POST",
					Path:   "/users",
					Body: []spec.Param{
						{Name: "name", Type: "string"},
						{Name: "email", Type: "string"},
					},
				},
			},
		},
	}

	assert.True(t, hasWriteCommands(resources),
		"a POST endpoint with a write-shape operationId and body should classify as write")
}
