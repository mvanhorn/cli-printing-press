package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointNeedsClientLimit verifies the gate that controls when
// generated GET commands emit a `truncateJSONArray(data, flagLimit)` call
// after the API response returns. This is the core decision behind
// hackernews retro #350 finding F6 — APIs that accept ?limit=N without
// honoring it (Firebase, file dumps, RSS) silently broke --limit until
// the generator started truncating client-side.
func TestEndpointNeedsClientLimit(t *testing.T) {
	t.Parallel()

	mkParams := func(p ...spec.Param) []spec.Param { return p }
	limitParam := spec.Param{Name: "limit", Type: "int"}
	otherParam := spec.Param{Name: "page", Type: "int"}
	positionalLimit := spec.Param{Name: "limit", Type: "int", Positional: true}
	pathLimit := spec.Param{Name: "limit", Type: "int", PathParam: true}

	cases := []struct {
		name     string
		endpoint spec.Endpoint
		want     bool
	}{
		{
			name:     "GET with limit and no pagination → truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(limitParam)},
			want:     true,
		},
		{
			name:     "GET with limit and pagination block → defer to API",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(limitParam), Pagination: &spec.Pagination{}},
			want:     false,
		},
		{
			name:     "GET with no limit param → no truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(otherParam)},
			want:     false,
		},
		{
			name:     "GET with no params → no truncate",
			endpoint: spec.Endpoint{Method: "GET"},
			want:     false,
		},
		{
			name:     "POST with limit param → no truncate (mutation, not list)",
			endpoint: spec.Endpoint{Method: "POST", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "PUT with limit param → no truncate",
			endpoint: spec.Endpoint{Method: "PUT", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "DELETE with limit param → no truncate",
			endpoint: spec.Endpoint{Method: "DELETE", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "GET with positional 'limit' → no truncate (not a flag)",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(positionalLimit)},
			want:     false,
		},
		{
			name:     "GET with path-param 'limit' → no truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(pathLimit)},
			want:     false,
		},
		{
			name:     "GET with mixed params including limit → truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(otherParam, limitParam)},
			want:     true,
		},
		{
			name:     "lowercase method get → truncate (case-insensitive method check)",
			endpoint: spec.Endpoint{Method: "get", Params: mkParams(limitParam)},
			want:     true,
		},
		{
			name:     "uppercase param Limit → truncate (case-insensitive param-name check)",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(spec.Param{Name: "Limit", Type: "int"})},
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := endpointNeedsClientLimit(tc.endpoint)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClientLimitGenerationEmitsHelperAndBuilds(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("limit-truncate")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Params: []spec.Param{{
						Name:    "limit",
						Type:    "integer",
						Default: 5,
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "limit-truncate-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "func truncateJSONArray(")

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_orders.go")
	require.Contains(t, commandSrc, "truncateJSONArray(data,",
		"generated command should call truncateJSONArray for non-paginated limit endpoint")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}
