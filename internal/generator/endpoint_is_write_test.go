package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointIsWriteCommand covers the four classification signals: HTTP
// verb, operationId prefix, body shape, and the mcp:read-only annotation.
// POST endpoints used as queries (search, GraphQL, RPC-style) must classify
// as reads; genuine mutations must classify as writes regardless of body
// shape coincidence.
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
		{
			name:     "POST getOrCreate flips back to write (read-shaped leading token, mutation in tail)",
			opName:   "getOrCreateUser",
			endpoint: spec.Endpoint{Method: "POST", Path: "/users"},
			want:     true,
		},
		{
			name:     "POST fetchAndUpdate flips back to write (mutation token in tail)",
			opName:   "fetchAndUpdateProfile",
			endpoint: spec.Endpoint{Method: "POST", Path: "/profile"},
			want:     true,
		},
		{
			name:     "POST listAndDelete flips back to write",
			opName:   "listAndDeleteOrphans",
			endpoint: spec.Endpoint{Method: "POST", Path: "/orphans"},
			want:     true,
		},
		{
			name:     "leading-token match is whole-word, not prefix substring (getter is not get)",
			opName:   "getter",
			endpoint: spec.Endpoint{Method: "POST", Path: "/getter"},
			want:     true, // single-token "getter" — not the literal "get" verb, fail-closed
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := endpointIsWriteCommand(tc.endpoint, tc.opName)
			assert.Equal(t, tc.want, got, "endpointIsWriteCommand(%q) returned wrong classification", tc.opName)
		})
	}
}

// TestHasWriteCommands_PostAsQueryFlipsHasWriteFalse verifies the
// classifier propagates through resourceHasWriteCommand and hasWriteCommands
// so a resource containing only a POST search endpoint flips HasWriteCommands
// to false — that signal drives the README's read-only branching.
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

// TestPromotedCommandVerbBranching covers the integration path: the
// rendered promoted command emits the same HTTP verb the spec declared,
// so a POST-only endpoint hits c.Post and a GET-only endpoint stays on
// c.Get/resolveRead.
func TestPromotedCommandVerbBranching(t *testing.T) {
	cases := []struct {
		name         string
		apiName      string
		resourceName string
		endpointName string
		endpoint     spec.Endpoint
		mustContain  []string
		mustNotHave  []string
	}{
		{
			name:         "POST endpoint emits c.Post",
			apiName:      "post-promoted",
			resourceName: "queries",
			endpointName: "searchAll",
			endpoint: spec.Endpoint{
				Method:      "POST",
				Path:        "/search-all",
				Description: "Search collections by free text",
				Body:        []spec.Param{{Name: "queryText", Type: "string"}},
			},
			mustContain: []string{"c.Post("},
			mustNotHave: []string{"c.Get(path, params)"},
		},
		{
			name:         "GET endpoint keeps c.Get / resolveRead",
			apiName:      "get-promoted",
			resourceName: "items",
			endpointName: "listItems",
			endpoint: spec.Endpoint{
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			mustNotHave: []string{"c.Post(", "c.Put(", "c.Patch("},
		},
		{
			// HEAD / OPTIONS aren't supported by the generated client.
			// Falling back to c.Get keeps generation compileable; the only
			// alternative would be emitting an undefined method like c.Head.
			name:         "HEAD endpoint falls back to c.Get",
			apiName:      "head-promoted",
			resourceName: "probes",
			endpointName: "headStatus",
			endpoint: spec.Endpoint{
				Method:      "HEAD",
				Path:        "/status",
				Description: "Probe status",
			},
			mustContain: []string{"c.Get(path, params)"},
			mustNotHave: []string{"c.Head(", "c.Options("},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.apiName)
			apiSpec.Resources = map[string]spec.Resource{
				tc.resourceName: {
					Description: tc.resourceName,
					Endpoints:   map[string]spec.Endpoint{tc.endpointName: tc.endpoint},
				},
			}

			outputDir := filepath.Join(t.TempDir(), tc.apiName+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			src := readPromotedCommandFile(t, outputDir)
			for _, want := range tc.mustContain {
				require.Contains(t, src, want)
			}
			for _, banned := range tc.mustNotHave {
				require.NotContains(t, src, banned)
			}
		})
	}
}

// readPromotedCommandFile finds the single promoted_*.go file the generator
// emits for a fixture spec with one resource. Naming varies (resource name
// vs. kebabed endpoint name vs. camelCase), so the lookup glob-matches.
func readPromotedCommandFile(t *testing.T, outputDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "promoted_*.go"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "expected exactly one promoted command file in internal/cli/")
	content, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	return string(content)
}

// TestHasWriteCommands_GenuineMutationsStayTrue is the negative guard: a
// POST createUser endpoint with a write-shape body must still classify as a
// write so the README keeps emitting the mutation-aware Agent Usage bullets.
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
