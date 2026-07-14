package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedArrayQueryParamUsesRepeatedKeysByDefault(t *testing.T) {
	t.Parallel()

	requests := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()["fdcIds"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("array-query-param")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"foods": {
			Description: "Foods",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/foods",
					Description: "Get foods",
					Params: []spec.Param{
						{Name: "fdcIds", Type: "array", ItemType: "integer", Description: "Food IDs"},
					},
					Response: spec.ResponseDef{Type: "array", Item: "Food"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Food": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "array-query-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	promotedSrc := readPromotedCommandFile(t, outputDir)
	require.Contains(t, promotedSrc, `path = appendArrayQueryParam(path, "fdcIds", flagFdcIds, "form", true)`)
	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, `QueryArray: true, QueryStyle: "form", QueryExplode: true`)
	require.Contains(t, mcpSrc, `appendMCPArrayQueryParam(path, binding.WireName, v, binding.QueryStyle, binding.QueryExplode)`)
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "array-query-param-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/array-query-param-pp-cli")
	runGeneratedBinary(t, binaryPath, "foods", "get", "--fdc-ids", "534358,373052")
	require.Equal(t, []string{"534358", "373052"}, <-requests)
}

func TestGeneratedArrayQueryParamRuntimeHandlesNativeAndDefaultValues(t *testing.T) {
	t.Parallel()

	requests := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()["fdcIds"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("array-query-mcp")
	apiSpec.BaseURL = server.URL
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"foods": {
			Description: "Foods",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/foods",
					Description: "Get foods",
					Params: []spec.Param{{
						Name: "fdcIds", Type: "array", ItemType: "integer", Default: []any{534358, 373052},
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Food"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Food": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "array-query-mcp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	codeOrchSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, codeOrchSrc, `QueryArray: true, QueryStyle: "form", QueryExplode: true`)
	runtimeTest := `package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArrayQuerySerialization(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "native MCP array", value: []any{float64(534358), float64(373052)}, want: []string{"534358", "373052"}},
		{name: "JSON encoded default", value: "[534358,373052]", want: []string{"534358", "373052"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := appendMCPArrayQueryParam("/foods", "fdcIds", tt.value, "form", true)
			parsed, err := url.Parse(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, parsed.Query()["fdcIds"])
		})
	}

	params := map[string]any{"fdcIds": []any{float64(534358), float64(373052)}}
	path := codeOrchSplitQuery("/foods", []codeOrchParamBinding{{
		PublicName: "fdcIds", WireName: "fdcIds", QueryArray: true, QueryStyle: "form", QueryExplode: true,
	}}, params)
	parsed, err := url.Parse(path)
	require.NoError(t, err)
	require.Equal(t, []string{"534358", "373052"}, parsed.Query()["fdcIds"])
	require.Empty(t, params)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "array_query_test.go"), []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestArrayQuerySerialization$")

	binaryPath := filepath.Join(outputDir, "array-query-mcp-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/array-query-mcp-pp-cli")
	runGeneratedBinary(t, binaryPath, "foods", "get")
	require.Equal(t, []string{"534358", "373052"}, <-requests)
}

func TestGeneratedArrayQueryParamHonorsFormExplodeFalse(t *testing.T) {
	t.Parallel()

	requestURIs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	explode := false
	apiSpec := minimalSpec("compact-array-query-param")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"foods": {
			Description: "Foods",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/foods",
					Description: "Get foods",
					Params: []spec.Param{{
						Name: "fdcIds", Type: "array", ItemType: "integer", Description: "Food IDs",
						QueryStyle: "form", QueryExplode: &explode,
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Food"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Food": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "compact-array-query-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "compact-array-query-param-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/compact-array-query-param-pp-cli")
	runGeneratedBinary(t, binaryPath, "foods", "get", "--fdc-ids", "534358,373052")

	require.Contains(t, <-requestURIs, "fdcIds=534358%2C373052")
}
