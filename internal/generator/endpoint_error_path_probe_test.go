package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointSkipsErrorPathProbe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		endpoint spec.Endpoint
		want     bool
	}{
		{
			name: "GET with one required free-text query positional skips probe",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{{Name: "q", Type: "string", Required: true, Positional: true}},
			},
			want: true,
		},
		{
			name: "HEAD with one required free-text query positional skips probe",
			endpoint: spec.Endpoint{
				Method: "HEAD",
				Path:   "/images/search",
				Params: []spec.Param{{Name: "q", Type: "string", Required: true, Positional: true}},
			},
			want: true,
		},
		{
			name: "untyped free-text query positional skips probe",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{{Name: "q", Required: true, Positional: true}},
			},
			want: true,
		},
		{
			name: "untyped JSON-described positional still gets probed",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{{
					Name:        "filter",
					Required:    true,
					Positional:  true,
					Description: "{\"field\":\"value\"}",
				}},
			},
			want: false,
		},
		{
			name: "path positional still gets probed",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/{id}",
				Params: []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true}},
			},
			want: false,
		},
		{
			name: "required enum flag still gets probed",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true, Positional: true},
					{Name: "orientation", Type: "string", Required: true, Enum: []string{"landscape", "portrait"}},
				},
			},
			want: false,
		},
		{
			name: "formatted positional still gets probed",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, Format: "uuid"}},
			},
			want: false,
		},
		{
			name: "multiple positionals still get probed",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/images/search",
				Params: []spec.Param{
					{Name: "collection", Type: "string", Required: true, Positional: true},
					{Name: "q", Type: "string", Required: true, Positional: true},
				},
			},
			want: false,
		},
		{
			name: "mutating verb still gets probed",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/images/search",
				Params: []spec.Param{{Name: "q", Type: "string", Required: true, Positional: true}},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, endpointSkipsErrorPathProbe(tc.endpoint))
		})
	}
}

func TestGeneratedEndpointNoErrorPathProbeAnnotation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("error-path-probe")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"search": {
				Method:      "GET",
				Path:        "/items/search",
				Description: "Search items by free text",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true, Positional: true, Description: "Search query"},
				},
			},
			"filtered": {
				Method:      "GET",
				Path:        "/items/filtered",
				Description: "Search items with required validated filter",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true, Positional: true, Description: "Search query"},
					{Name: "orientation", Type: "string", Required: true, Enum: []string{"landscape", "portrait"}},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/items/{id}",
				Description: "Get one item",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
				},
			},
		},
	}
	apiSpec.Resources["lookup"] = spec.Resource{
		Description: "Lookup",
		Endpoints: map[string]spec.Endpoint{
			"similar": {
				Method:      "GET",
				Path:        "/similar",
				Description: "Find similar records",
				Params: []spec.Param{
					{Name: "term", Type: "string", Required: true, Positional: true},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "error-path-probe-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	search := readEndpointProbeFile(t, outputDir, "items_search.go")
	require.Contains(t, search, `"pp:no-error-path-probe": "true"`,
		"free-text query positional endpoint should skip the live dogfood error_path probe")

	promoted := readEndpointProbeFile(t, outputDir, "promoted_lookup.go")
	require.Contains(t, promoted, `"pp:no-error-path-probe": "true"`,
		"promoted free-text query positional endpoint should skip the live dogfood error_path probe")

	filtered := readEndpointProbeFile(t, outputDir, "items_filtered.go")
	require.NotContains(t, filtered, `"pp:no-error-path-probe"`,
		"required enum flags still provide a real validation error path")

	get := readEndpointProbeFile(t, outputDir, "items_get.go")
	require.NotContains(t, get, `"pp:no-error-path-probe"`,
		"path ID lookups should keep the normal error_path probe")

	requireGeneratedCompiles(t, outputDir)
}

func readEndpointProbeFile(t *testing.T, outputDir, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", name))
	require.NoError(t, err)
	return string(src)
}
