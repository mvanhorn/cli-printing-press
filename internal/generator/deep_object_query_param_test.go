package generator

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// Case index (aos-build#165 plan, Task 5 Step 1):
//  1. CLI wire: array-of-objects param, style=deepObject -> indexed bracket
//     keys (sort[0][field]=Name) percent-encoded on r.RequestURI.
//  2. CLI wire: object-typed param, style=deepObject -> filter[status]=active.
//  3. deepObject-ONLY spec generates + compiles. The plain variant rides
//     TestGeneratedDeepObjectQueryParamCLIWire's module; the genuinely
//     gate-pinning variant (object-typed-only, so hasArrayQueryParams is
//     FALSE and the shared net/url import + codeOrchSplitQuery signature
//     gates must fire on the deepObject predicate alone) rides
//     TestGeneratedDeepObjectMCPRuntimeSerialization's module, which also
//     sets MCP.Orchestration "code" so mcp_code_orch.go is exercised.
//  4. Injection, BOTH channels: (4a) hostile VALUE percent-encoded; (4b)
//     hostile object FIELD NAME percent-encoded in the emitted KEY — the
//     server sees exactly one literal key `sort[0][a&b=c#d]`, nothing split.
//  5. Malformed CLI JSON -> non-zero exit naming the flag and the concrete
//     example [{"field":"Name","direction":"desc"}].
//  6. MCP runtime, THREE layers: (6a) bare appendMCPDeepObjectQueryParam
//     returned-path-string assertion; (6b) routed codeOrchSplitQuery with a
//     DeepObjectQuery binding; (6c) registered-handler wire proof through
//     makeAPIHandler's DeepObjectQuery branch (in-module, Task-2
//     TestBracketHandlerWire idiom).

type deepObjectCapturedRequest struct {
	uri   string
	query url.Values
}

// deepObjectWireSpec declares one promoted endpoint (records get) carrying an
// array-of-objects `sort` and an object-typed `filter`, both style=deepObject,
// plus a sub-resource endpoint (records views get) carrying `sort` so
// command_endpoint.go.tmpl renders the deepObject branch too (sub-resource
// endpoints never promote, making that template's coverage deterministic).
func deepObjectWireSpec(name, baseURL string) *spec.APISpec {
	explode := true
	sortParam := spec.Param{
		Name: "sort", Type: "array", ItemType: "object", Description: "Sort spec",
		QueryStyle: "deepObject", QueryExplode: &explode,
		Fields: []spec.Param{
			{Name: "field", Type: "string"},
			{Name: "direction", Type: "string"},
		},
	}
	apiSpec := minimalSpec(name)
	apiSpec.BaseURL = baseURL
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Params: []spec.Param{
						sortParam,
						{
							Name: "filter", Type: "object", Description: "Filter object",
							QueryStyle: "deepObject", QueryExplode: &explode,
						},
					},
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
			},
			SubResources: map[string]spec.Resource{
				"views": {
					Description: "Record views",
					Endpoints: map[string]spec.Endpoint{
						"get": {
							Method:      "GET",
							Path:        "/records/views",
							Description: "List record views",
							Params:      []spec.Param{sortParam},
							Response:    spec.ResponseDef{Type: "array", Item: "Record"},
						},
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}
	return apiSpec
}

// TestGeneratedDeepObjectQueryParamCLIWire covers cases 1, 2, 4a, 4b, 5 and
// 6c on one generated module (plus the plain-orchestration half of case 3:
// this spec has no form-style array query params, and it must generate,
// compile, and run).
func TestGeneratedDeepObjectQueryParamCLIWire(t *testing.T) {
	t.Parallel()

	requests := make(chan deepObjectCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- deepObjectCapturedRequest{uri: r.RequestURI, query: r.URL.Query()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := deepObjectWireSpec("deep-object-wire", server.URL)
	outputDir := filepath.Join(t.TempDir(), "deep-object-wire-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Emitted-source pins: CLI routes deepObject params through the new
	// helper (never appendArrayQueryParam), in BOTH command templates.
	promotedSrc := readPromotedCommandFile(t, outputDir)
	require.Contains(t, promotedSrc, `path, err = appendDeepObjectQueryParam(path, "sort", flagSort)`)
	require.Contains(t, promotedSrc, `path, err = appendDeepObjectQueryParam(path, "filter", flagFilter)`)
	require.NotContains(t, promotedSrc, `appendArrayQueryParam(path, "sort"`)
	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "records_views_get.go")
	require.Contains(t, endpointSrc, `path, err = appendDeepObjectQueryParam(path, "sort", flagSort)`)
	require.NotContains(t, endpointSrc, `appendArrayQueryParam(path, "sort"`)
	// MCP surface: binding marker + handler routing through the new helper.
	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, `PublicName: "sort", WireName: "sort", Location: "query", DeepObjectQuery: true`)
	require.Contains(t, mcpSrc, `PublicName: "filter", WireName: "filter", Location: "query", DeepObjectQuery: true`)
	require.Contains(t, mcpSrc, `appendMCPDeepObjectQueryParam(path, binding.WireName, v)`)

	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "deep-object-wire-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/deep-object-wire-pp-cli")

	// Case 1 + 2: indexed keys, percent-encoded, on the raw RequestURI.
	runGeneratedBinary(t, binaryPath, "records", "get",
		"--sort", `[{"field":"Name","direction":"desc"},{"field":"Price","direction":"asc"}]`,
		"--filter", `{"status":"active"}`)
	captured := <-requests
	require.Contains(t, captured.uri, "sort%5B0%5D%5Bfield%5D=Name")
	require.Contains(t, captured.uri, "sort%5B0%5D%5Bdirection%5D=desc")
	require.Contains(t, captured.uri, "sort%5B1%5D%5Bfield%5D=Price")
	require.Contains(t, captured.uri, "filter%5Bstatus%5D=active")
	// The flattened single-key blobs of the pre-fix behavior must be gone —
	// and the params-map path must not double-send either param.
	require.NotContains(t, captured.uri, "sort=")
	require.NotContains(t, captured.uri, "filter=")
	require.Equal(t, []string{"Name"}, captured.query["sort[0][field]"])
	require.Equal(t, []string{"active"}, captured.query["filter[status]"])

	// command_endpoint.go.tmpl at runtime (sub-resource endpoint).
	runGeneratedBinary(t, binaryPath, "records", "views", "get",
		"--sort", `[{"field":"Name","direction":"desc"}]`)
	captured = <-requests
	require.Contains(t, captured.uri, "sort%5B0%5D%5Bfield%5D=Name")

	// Case 4a: hostile VALUE stays one percent-encoded value.
	runGeneratedBinary(t, binaryPath, "records", "get",
		"--sort", `[{"field":"a&b=c#d"}]`)
	captured = <-requests
	require.Contains(t, captured.uri, "sort%5B0%5D%5Bfield%5D=a%26b%3Dc%23d")
	require.Equal(t, []string{"a&b=c#d"}, captured.query["sort[0][field]"])
	require.Len(t, captured.query, 1)

	// Case 4b: hostile FIELD NAME — the emitted KEY is percent-encoded; the
	// server sees exactly one literal key, nothing split into extra params.
	runGeneratedBinary(t, binaryPath, "records", "get",
		"--sort", `[{"a&b=c#d":"v"}]`)
	captured = <-requests
	require.Contains(t, captured.uri, "sort%5B0%5D%5Ba%26b%3Dc%23d%5D=v")
	require.Equal(t, []string{"v"}, captured.query["sort[0][a&b=c#d]"])
	require.Len(t, captured.query, 1)

	// Case 5: malformed JSON is a usage error naming the flag and showing
	// the concrete example (no HTTP request is made). Short mode never gets
	// here: the build runGoCommand above already skips.
	cmd := exec.Command(binaryPath, "records", "get", "--sort", `[{broken`)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "malformed --sort JSON must exit non-zero; output:\n%s", output)
	require.Contains(t, string(output), "--sort")
	require.Contains(t, string(output), `[{"field":"Name","direction":"desc"}]`)

	// Case 6c: registered-handler wire proof — drive the REAL
	// makeAPIHandler-registered records_get tool with the native MCP value
	// and assert the SERVER-SIDE r.RequestURI carries the indexed keys
	// percent-encoded, through the DeepObjectQuery routing branch.
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `"DEEP_OBJECT_WIRE_BASE_URL"`,
		"generated config must expose the BASE_URL env override the in-module test relies on")
	require.Contains(t, configSrc, "MYAPI_TOKEN",
		"generated config must reference the spec's credential env var")

	const handlerWireTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// TestDeepObjectHandlerWire drives the REAL registered MCP tool handler and
// proves style=deepObject params leave as indexed bracket keys on the HTTP
// request — the MCP-surface twin of the CLI wire proof.
func TestDeepObjectHandlerWire(t *testing.T) {
	uris := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris <- r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("DEEP_OBJECT_WIRE_BASE_URL", srv.URL)
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("HOME", t.TempDir())

	s := server.NewMCPServer("test", "0.0.0")
	RegisterTools(s)
	tool := s.ListTools()["records_get"]
	require.NotNil(t, tool, "records_get tool not registered")

	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name: "records_get",
		Arguments: map[string]any{
			"sort": []any{map[string]any{"field": "Name", "direction": "desc"}},
		},
	}}
	result, err := tool.Handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "tool handler returned error result: %+v", result)
	uri := <-uris
	require.Contains(t, uri, "sort%5B0%5D%5Bfield%5D=Name")
	require.Contains(t, uri, "sort%5B0%5D%5Bdirection%5D=desc")
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "deep_object_handler_wire_test.go"),
		[]byte(handlerWireTest), 0o600))
	// go test exits 0 when -run matches nothing, so assert the PASS line to
	// prove the in-module handler test actually executed.
	wireOutput, wireErr := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/mcp", "-run", "^TestDeepObjectHandlerWire$")
	require.NoError(t, wireErr, wireOutput)
	require.Contains(t, wireOutput, "--- PASS: TestDeepObjectHandlerWire",
		"in-module registered-handler wire test must actually run and pass:\n%s", wireOutput)
}

// TestGeneratedDeepObjectMCPRuntimeSerialization covers case 3's gate pin and
// cases 6a + 6b. The spec carries ONLY an object-typed deepObject query param
// — no array-typed query params anywhere — so hasArrayQueryParams is false
// and the shared surfaces (helpers.go/tools.go net/url imports,
// codeOrchSplitQuery's path-returning signature) must be gated by the
// deepObject predicate alone; a missed combined gate fails compilation here.
// MCP.Orchestration "code" makes mcp_code_orch.go render.
func TestGeneratedDeepObjectMCPRuntimeSerialization(t *testing.T) {
	t.Parallel()

	explode := true
	apiSpec := minimalSpec("deep-object-orch")
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Params: []spec.Param{{
						Name: "filter", Type: "object", Description: "Filter object",
						QueryStyle: "deepObject", QueryExplode: &explode,
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "deep-object-orch-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	codeOrchSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, codeOrchSrc, `PublicName: "filter", WireName: "filter", DeepObjectQuery: true`)
	require.Contains(t, codeOrchSrc, `appendMCPDeepObjectQueryParam(path, q.WireName, v)`)

	// Case 3: the deepObject-only module must compile (import + signature
	// gates fired on hasDeepObjectQueryParams alone).
	requireGeneratedCompiles(t, outputDir)

	// Cases 6a + 6b: in-module runtime test (array_query_param_test.go
	// test-2 idiom) — the bare helper's RETURNED PATH STRING carries
	// percent-encoded keys, and codeOrchSplitQuery routes a DeepObjectQuery
	// binding through it.
	const runtimeTest = `package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepObjectQuerySerialization(t *testing.T) {
	// 6a: bare helper, array-of-objects value — assert on the returned path
	// string BEFORE url.Parse, pinning percent-encoded key emission.
	path, err := appendMCPDeepObjectQueryParam("/records", "sort",
		[]any{map[string]any{"field": "Name", "direction": "desc"}})
	require.NoError(t, err)
	require.Contains(t, path, "sort%5B0%5D%5Bfield%5D=Name")
	require.Contains(t, path, "sort%5B0%5D%5Bdirection%5D=desc")

	// 6a: native map value.
	path, err = appendMCPDeepObjectQueryParam("/records", "filter",
		map[string]any{"status": "active"})
	require.NoError(t, err)
	require.Contains(t, path, "filter%5Bstatus%5D=active")

	// 6a: string values are JSON-decoded first (spec defaults / string
	// agent inputs), matching the CLI's --flag JSON contract.
	path, err = appendMCPDeepObjectQueryParam("/records", "filter", "{\"status\":\"active\"}")
	require.NoError(t, err)
	require.Contains(t, path, "filter%5Bstatus%5D=active")

	// 6a: malformed string JSON is a loud error, not a silent blob.
	_, err = appendMCPDeepObjectQueryParam("/records", "sort", "[{broken")
	require.Error(t, err)

	// 6b: routed code-orch binding — exercises the DeepObjectQuery routing
	// branch inside codeOrchSplitQuery, not just the helper.
	params := map[string]any{"sort": []any{map[string]any{"field": "Name"}}}
	routed, err := codeOrchSplitQuery("/records", []codeOrchParamBinding{{
		PublicName: "sort", WireName: "sort", DeepObjectQuery: true,
	}}, params)
	require.NoError(t, err)
	require.Contains(t, routed, "sort%5B0%5D%5Bfield%5D=Name")
	require.Empty(t, params)

	parsed, perr := url.Parse(routed)
	require.NoError(t, perr)
	require.Equal(t, []string{"Name"}, parsed.Query()["sort[0][field]"])
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "deep_object_query_test.go"),
		[]byte(runtimeTest), 0o600))
	output, err := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/mcp", "-run", "^TestDeepObjectQuerySerialization$")
	require.NoError(t, err, output)
	require.Contains(t, output, "--- PASS: TestDeepObjectQuerySerialization",
		"in-module runtime serialization test must actually run and pass:\n%s", output)
}
