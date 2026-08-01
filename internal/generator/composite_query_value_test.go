package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// Task 6 of the aos-build#165 plan (severable commit):
//
//  1. CLI/MCP composite-value PARITY for a FORM-style array-of-objects query
//     param (Type "array", ItemType "object", no QueryStyle): the CLI wire
//     value must equal the MCP wire value, and both must be valid JSON
//     ({"field":"Name"} percent-encoded) — never fmt's map[field:Name]
//     rendering. formatMCPParamValue already JSON-encodes composites; this
//     pins formatCLIParamValue to the same behavior.
//  2. Mixed-pin (adversarial-reviewer-flagged gap from Task 5): one code-orch
//     spec carrying BOTH a form-style array param AND a deepObject param
//     generates, compiles, and routes both correctly — repeated k=A&k=B for
//     the array, percent-encoded indexed keys for the deepObject.

// compositeParitySpec declares one promoted endpoint (records get) with a
// single form-style array-of-objects query param. No QueryStyle: style
// defaults to "form", explode defaults to true, so both surfaces emit the
// value list as repeated sort=<item> pairs.
func compositeParitySpec(name, baseURL string) *spec.APISpec {
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
					Params: []spec.Param{{
						Name: "sort", Type: "array", ItemType: "object", Description: "Sort spec",
						Fields: []spec.Param{
							{Name: "field", Type: "string"},
						},
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}
	return apiSpec
}

// mcpParityWireTest is the in-module MCP half of the parity proof. The
// @WANT_SORT@ placeholder is replaced with the SAME expected wire values the
// outer test asserts for the CLI, so both surfaces are pinned to one literal.
const mcpParityWireTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// TestFormArrayOfObjectsHandlerWire drives the REAL registered MCP tool
// handler with a native array-of-objects value and proves each element leaves
// the wire as valid JSON — the MCP half of the CLI/MCP parity pin.
func TestFormArrayOfObjectsHandlerWire(t *testing.T) {
	type captured struct {
		uri   string
		query []string
	}
	requests := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- captured{uri: r.RequestURI, query: r.URL.Query()["sort"]}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("COMPOSITE_PARITY_BASE_URL", srv.URL)
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("HOME", t.TempDir())

	s := server.NewMCPServer("test", "0.0.0")
	RegisterTools(s)
	tool := s.ListTools()["records_get"]
	require.NotNil(t, tool, "records_get tool not registered")

	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name: "records_get",
		Arguments: map[string]any{
			"sort": []any{
				map[string]any{"field": "Name"},
				map[string]any{"field": "Price"},
			},
		},
	}}
	result, err := tool.Handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "tool handler returned error result: %+v", result)

	got := <-requests
	require.Equal(t, []string{@WANT_SORT@}, got.query)
	require.Contains(t, got.uri, "sort=%7B%22field%22%3A%22Name%22%7D")
}
`

// TestGeneratedFormArrayOfObjectsCLIMCPWireParity is the parity proof: both
// generated surfaces must serialize a form-style array-of-objects query param
// element as the same valid-JSON wire value.
func TestGeneratedFormArrayOfObjectsCLIMCPWireParity(t *testing.T) {
	t.Parallel()

	// The single source of truth both surfaces are asserted against.
	wantSortWire := []string{`{"field":"Name"}`, `{"field":"Price"}`}

	requests := make(chan deepObjectCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- deepObjectCapturedRequest{uri: r.RequestURI, query: r.URL.Query()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := compositeParitySpec("composite-parity", server.URL)
	outputDir := filepath.Join(t.TempDir(), "composite-parity-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Emitted-source pin: a form-style array param stays on the array
	// emitter (never the deepObject path) on both surfaces.
	promotedSrc := readPromotedCommandFile(t, outputDir)
	require.Contains(t, promotedSrc, `path = appendArrayQueryParam(path, "sort", flagSort, "form", true)`)
	require.NotContains(t, promotedSrc, "appendDeepObjectQueryParam")
	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, `PublicName: "sort", WireName: "sort", Location: "query", QueryArray: true, QueryStyle: "form", QueryExplode: true`)

	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "composite-parity-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/composite-parity-pp-cli")

	// CLI half: each decoded object element must reach the wire as valid
	// JSON, percent-encoded — never fmt's map[field:Name] rendering.
	runGeneratedBinary(t, binaryPath, "records", "get",
		"--sort", `[{"field":"Name"},{"field":"Price"}]`)
	captured := <-requests
	require.Equal(t, wantSortWire, captured.query["sort"],
		"CLI wire values must be the JSON elements, not fmt's map[...] rendering")
	require.Contains(t, captured.uri, "sort=%7B%22field%22%3A%22Name%22%7D")
	require.NotContains(t, captured.uri, "map%5B")
	for _, item := range captured.query["sort"] {
		require.Truef(t, json.Valid([]byte(item)), "CLI wire value %q must be valid JSON", item)
	}

	// MCP half: drive the REAL registered handler in-module and assert the
	// SAME wire values (injected below), which makes CLI wire == MCP wire.
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `"COMPOSITE_PARITY_BASE_URL"`,
		"generated config must expose the BASE_URL env override the in-module test relies on")
	require.Contains(t, configSrc, "MYAPI_TOKEN",
		"generated config must reference the spec's credential env var")

	handlerTest := strings.ReplaceAll(mcpParityWireTest, "@WANT_SORT@",
		fmt.Sprintf("%q, %q", wantSortWire[0], wantSortWire[1]))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "composite_parity_wire_test.go"),
		[]byte(handlerTest), 0o600))
	// go test exits 0 when -run matches nothing, so assert the PASS line to
	// prove the in-module handler test actually executed.
	wireOutput, wireErr := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/mcp", "-run", "^TestFormArrayOfObjectsHandlerWire$")
	require.NoError(t, wireErr, wireOutput)
	require.Contains(t, wireOutput, "--- PASS: TestFormArrayOfObjectsHandlerWire",
		"in-module registered-handler wire test must actually run and pass:\n%s", wireOutput)
}

// TestMixedFormArrayAndDeepObjectQueryParams is the Task-5 adversarial
// reviewer's flagged gap, pinned: a code-orch spec with BOTH a form-style
// array param AND a deepObject param generates, compiles, and routes both
// through codeOrchSplitQuery correctly — repeated ids=A&ids=B for the array,
// percent-encoded indexed keys for the deepObject.
func TestMixedFormArrayAndDeepObjectQueryParams(t *testing.T) {
	t.Parallel()

	explode := true
	apiSpec := minimalSpec("mixed-query-orch")
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Params: []spec.Param{
						{Name: "ids", Type: "array", ItemType: "string", Description: "IDs"},
						{
							Name: "filter", Type: "object", Description: "Filter object",
							QueryStyle: "deepObject", QueryExplode: &explode,
						},
					},
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "mixed-query-orch-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	codeOrchSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, codeOrchSrc, `PublicName: "ids", WireName: "ids", QueryArray: true, QueryStyle: "form", QueryExplode: true`)
	require.Contains(t, codeOrchSrc, `PublicName: "filter", WireName: "filter", DeepObjectQuery: true`)

	// Both template gates fire together: the module must compile with the
	// array helpers AND the deepObject helpers emitted side by side.
	requireGeneratedCompiles(t, outputDir)

	const runtimeTest = `package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMixedQueryRouting routes a form-style array binding and a deepObject
// binding through the same codeOrchSplitQuery call: repeated keys for the
// array, indexed bracket keys for the deepObject, params map fully consumed.
func TestMixedQueryRouting(t *testing.T) {
	params := map[string]any{
		"ids":    []any{"A", "B"},
		"filter": map[string]any{"status": "active"},
	}
	routed, err := codeOrchSplitQuery("/records", []codeOrchParamBinding{
		{PublicName: "ids", WireName: "ids", QueryArray: true, QueryStyle: "form", QueryExplode: true},
		{PublicName: "filter", WireName: "filter", DeepObjectQuery: true},
	}, params)
	require.NoError(t, err)
	require.Empty(t, params)
	require.Contains(t, routed, "ids=A&ids=B")
	require.Contains(t, routed, "filter%5Bstatus%5D=active")

	parsed, perr := url.Parse(routed)
	require.NoError(t, perr)
	require.Equal(t, []string{"A", "B"}, parsed.Query()["ids"])
	require.Equal(t, []string{"active"}, parsed.Query()["filter[status]"])
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "mixed_query_routing_test.go"),
		[]byte(runtimeTest), 0o600))
	output, err := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/mcp", "-run", "^TestMixedQueryRouting$")
	require.NoError(t, err, output)
	require.Contains(t, output, "--- PASS: TestMixedQueryRouting",
		"in-module mixed routing test must actually run and pass:\n%s", output)
}
