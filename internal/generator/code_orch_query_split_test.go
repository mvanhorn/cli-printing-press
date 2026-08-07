package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestCodeOrchRoutesQueryParamsOnWriteMethods guards the write-method
// query/body split. Spec-declared in:query params on POST/PUT/PATCH must be
// emitted into codeOrchEndpoint.QueryParams and routed to the URL query
// string by codeOrchSplitQuery — never dumped into the JSON body.
//
// Regression guard for the latent defect found alongside the write-body fix:
// before this, handleCodeOrchExecute only built a query map for GET/DELETE,
// so a write endpoint's in:query param silently ended up in the body and the
// API ignored or rejected it.
func TestCodeOrchRoutesQueryParamsOnWriteMethods(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("qsplit")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["ledger"] = spec.Resource{
		Description: "Ledger",
		Endpoints: map[string]spec.Endpoint{
			"voucher-update": {
				Method:      "PUT",
				Path:        "/ledger/voucher/{id}",
				Description: "Update a voucher; sendToLedger is an in-query flag",
				Params: []spec.Param{
					{Name: "id", Type: "string", Positional: true, PathParam: true, Description: "Voucher ID"},
					{Name: "sendToLedger", In: "query", Type: "string", Description: "Post to ledger immediately (query)"},
					{Name: "X-Request-ID", In: "header", Type: "string", Description: "Request identifier"},
				},
				Body: []spec.Param{
					{Name: "voucherDescription", Type: "string", Description: "Voucher text (body)"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "qsplit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")

	// Struct carries the public-to-wire query binding field.
	require.Regexp(t, `QueryParams\s+\[\]codeOrchParamBinding`, src,
		"codeOrchEndpoint must declare a QueryParams field")
	// The PUT endpoint emits its in:query param into QueryParams.
	require.Regexp(t, `QueryParams:\s*\[\]codeOrchParamBinding\{\s*\{PublicName: "sendToLedger", WireName: "sendToLedger"\}\s*,?\s*\}`, src,
		"PUT endpoint must list sendToLedger in QueryParams")
	require.Regexp(t, `HeaderParams:\s*\[\]codeOrchParamBinding\{\s*\{PublicName: "X-Request-ID", WireName: "X-Request-ID"\}\s*,?\s*\}`, src,
		"PUT endpoint must list X-Request-ID in HeaderParams")
	// A body param must never be classified as a query param.
	require.NotRegexp(t, `QueryParams:\s*\[\]codeOrchParamBinding\{[^}]*voucherDescription[^}]*\}`, src,
		"body param must not appear inside any QueryParams literal")
	// GET/DELETE params and write-method split both translate public names
	// to wire names before calling the client.
	require.Contains(t, src, "codeOrchWireQueryName(ep.QueryParams, k)",
		"GET/DELETE query routing must translate public query names")
	// The split helper exists and is wired into the write path.
	require.Contains(t, src, "func codeOrchSplitQuery(",
		"codeOrchSplitQuery helper must be emitted")
	require.Contains(t, src, "codeOrchSplitQuery(ep.QueryParams, params)",
		"the write branch must route query params via codeOrchSplitQuery")
	require.Contains(t, src, "for _, binding := range ep.HeaderParams",
		"the execute path must route spec-declared header params before query/body splitting")
	require.NotContains(t, src, "delete(params, q.WireName)",
		"query routing must not delete a same-named body field when the public query name was consumed")

	runtimeTest := `package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCodeOrchestrationRoutesHeaderAndEmptyValues(t *testing.T) {
	var gotQuery url.Values
	var gotHeader string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotHeader = r.Header.Get("X-Request-ID")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("QSPLIT_BASE_URL", server.URL)

	request := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "ledger.voucher-update",
			"params": map[string]any{
				"id": "voucher-1", "sendToLedger": "", "X-Request-ID": "request-1", "voucherDescription": "",
		},
	}}}
	result, err := handleCodeOrchExecute(context.Background(), request)
	if err != nil || result.IsError {
		t.Fatalf("code orchestration request failed: result=%#v err=%v", result, err)
	}
	if values, ok := gotQuery["sendToLedger"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("sendToLedger query = %#v, want one explicit empty value", gotQuery)
	}
	if gotHeader != "request-1" {
		t.Fatalf("X-Request-ID = %q, want request-1", gotHeader)
	}
	if value, ok := gotBody["voucherDescription"]; !ok || value != "" {
		t.Fatalf("voucherDescription body = %#v, want explicit empty string", gotBody)
	}
	if _, ok := gotBody["sendToLedger"]; ok {
		t.Fatalf("query param leaked into body: %#v", gotBody)
	}
	if _, ok := gotBody["X-Request-ID"]; ok {
		t.Fatalf("header param leaked into body: %#v", gotBody)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_query_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "TestCodeOrchestrationRoutesHeaderAndEmptyValues", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestMCPParamBindingsHonorExplicitLocations(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method: "GET",
		Path:   "/things/{cursor}",
		Pagination: &spec.Pagination{
			CursorParam: "cursor",
			Type:        "cursor",
		},
		Params: []spec.Param{
			{Name: "cursor", In: "header", Type: "string", Default: "request-default"},
			{Name: "cursor", In: "path", Type: "string", PathParam: true},
			{Name: "page", In: "query", Type: "integer", Positional: true},
		},
	}

	bindings := mcpParamBindings(endpoint, endpoint.Path)
	var headerCount, queryCount int
	for _, binding := range bindings {
		switch binding.Location {
		case "header":
			headerCount++
			require.Equal(t, "cursor", binding.WireName)
			require.Equal(t, "request-default", binding.Default)
		case "query":
			queryCount++
			require.Equal(t, "page", binding.WireName)
		}
	}
	require.Equal(t, 1, headerCount, "same-named header must not be treated as the pagination cursor")
	require.Equal(t, 1, queryCount, "positional query params must remain routable")
}
