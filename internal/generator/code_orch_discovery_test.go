package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestCodeOrchInputsUseExecutorNames(t *testing.T) {
	ep := spec.Endpoint{
		Params: []spec.Param{
			{Name: "orgId", FlagName: "organization", Type: "string", Positional: true},
			{Name: "$limit", FlagName: "limit", Type: "integer", In: "query"},
			{Name: "X-Mode", Type: "string", In: "header", Enum: []string{"preview"}},
		},
		Body: []spec.Param{{Name: "connection", BodyName: "connection_type", FlagName: "mode", Type: "string", Required: true, Enum: []string{"assistance"}}},
	}
	inputs := codeOrchInputs(ep, "/orgs/{orgId}/sessions")
	require.Equal(t, []codeOrchInput{
		{Name: "orgId", Location: "path", Type: "string", Required: true},
		{Name: "limit", Location: "query", Type: "integer"},
		{Name: "X-Mode", Location: "header", Type: "string", Enum: []string{"preview"}},
		{Name: "connection_type", Location: "body", Type: "string", Required: true, Enum: []string{"assistance"}},
	}, inputs)
	require.Contains(t, codeOrchInputKeywords(inputs), "assistance")
	globalInputs := codeOrchInputs(ep, "/tenants/{tenant_id}/orgs/{orgId}/sessions", []string{"tenant_id", "orgId"})
	require.Equal(t, codeOrchInput{Name: "tenant_id", Location: "path", Type: "string"}, globalInputs[3])
	require.Len(t, globalInputs, 5)
}

func TestCodeOrchInputsQueryAliases(t *testing.T) {
	ep := spec.Endpoint{Method: "GET", Pagination: &spec.Pagination{Type: "cursor", CursorParam: "$after"}, Params: []spec.Param{
		{Name: "page", URLName: "$page", FlagName: "page-number", In: "query", Type: "integer", Positional: true},
		{Name: "after", URLName: "$after", FlagName: "continuation", In: "query", Type: "string"},
	}}
	require.Equal(t, []codeOrchInput{
		{Name: "page-number", Location: "query", Type: "integer"},
		{Name: "$after", Location: "query", Type: "string"},
	}, codeOrchInputs(ep, "/items"))
}

func TestCodeOrchInputsFlaggedPath(t *testing.T) {
	ep := spec.Endpoint{Params: []spec.Param{{Name: "itemId", In: "path", PathParam: true, FlagName: "item-id", Type: "string", Required: true}}}
	require.Equal(t, []codeOrchInput{{Name: "itemId", Location: "path", Type: "string", Required: true}}, codeOrchInputs(ep, "/items/{itemId}"))
}

func TestCodeOrchInputsBodyShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		ep   spec.Endpoint
		want []codeOrchInput
	}{
		{"array", spec.Endpoint{BodyIsArray: true, BodyRequired: true}, []codeOrchInput{{Name: "body", Location: "body", Type: "array", Required: true}}},
		{"raw", spec.Endpoint{RequestContentType: "application/octet-stream", BodyRequired: true}, []codeOrchInput{{Name: "body_base64", Location: "body", Type: "string", Required: true}, {Name: "content_type", Location: "header", Type: "string"}}},
		{"opaque JSON", spec.Endpoint{BodyJSONFallback: true}, []codeOrchInput{}},
		{"none", spec.Endpoint{}, []codeOrchInput{}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, codeOrchInputs(tc.ep, "/items")) })
	}
}

func TestGeneratedCodeOrchDiscoversRequestInputs(t *testing.T) {
	t.Parallel()
	api := minimalSpec("discovery-contract")
	api.Auth = spec.AuthConfig{Type: "none"}
	api.MCP = spec.MCPConfig{Orchestration: "code"}
	ep := spec.Endpoint{Method: "POST", Path: "/sessions", Description: "Start a session", Body: []spec.Param{
		{Name: "connection_type", Type: "string", Required: true, Enum: []string{"assistance", "on"}},
		{Name: "q", Type: "string"},
		{Name: "current_ip", Type: "string"},
	}}
	flagged := spec.Endpoint{Method: "GET", Path: "/items/{itemId}", Description: "Fetch an item", Params: []spec.Param{
		{Name: "itemId", In: "path", PathParam: true, FlagName: "item-id", Type: "string", Required: true},
		{Name: "page", URLName: "$page", In: "query", Positional: true, FlagName: "page-number", Type: "integer"},
	}}
	api.Resources = map[string]spec.Resource{"sessions": {Endpoints: map[string]spec.Endpoint{"start": ep, "flagged": flagged}, SubResources: map[string]spec.Resource{
		"nested": {Endpoints: map[string]spec.Endpoint{"start": ep}},
	}}}
	dir := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	require.NoError(t, New(api, dir).Generate())
	source := readGeneratedFile(t, dir, "internal", "mcp", "code_orch.go")
	require.Contains(t, source, `"params":`)
	runtimeTest := `package mcp
import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
 mcplib "github.com/mark3labs/mcp-go/mcp"
)
func TestRequestInputDiscovery(t *testing.T) {
 for _, query := range []string{"assistance", "connection_type", "on", "q"} {
  req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{"query": query}}}
  result, err := handleCodeOrchSearch(context.Background(), req)
  if err != nil || result.IsError { t.Fatalf("search: %v %#v", err, result) }
  var data struct { Results []struct {
   ID string ` + "`json:\"endpoint_id\"`" + `
   Params []struct { Name string; Required bool; Enum []string }
  } }
  if err := json.Unmarshal([]byte(result.Content[0].(mcplib.TextContent).Text), &data); err != nil { t.Fatal(err) }
  found := map[string]bool{}
  for _, endpoint := range data.Results {
   for _, p := range endpoint.Params {
    if p.Name == "connection_type" && p.Required && len(p.Enum) == 2 && p.Enum[0] == "assistance" {
     found[endpoint.ID] = true
    }
   }
  }
  for _, id := range []string{"sessions.start", "sessions.nested.start"} {
   if !found[id] { t.Errorf("%s missing callable contract in %q: %#v", id, query, data) }
  }
 }
}
func TestRequestInputSearchAvoidsSubstringNoise(t *testing.T) {
 for _, query := range []string{"request", "only"} {
  req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{"query": query}}}
  result, err := handleCodeOrchSearch(context.Background(), req)
  if err != nil || result.IsError { t.Fatalf("search: %v %#v", err, result) }
  var data struct { Results []json.RawMessage }
  if err := json.Unmarshal([]byte(result.Content[0].(mcplib.TextContent).Text), &data); err != nil { t.Fatal(err) }
  if len(data.Results) != 0 { t.Errorf("short request keyword polluted %q: %#v", query, data) }
 }
}
func TestFlaggedPathInputExecutes(t *testing.T) {
 home := t.TempDir()
 for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} { t.Setenv(key, home) }
 srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.URL.Path != "/items/42" || r.URL.Query().Get("$page") != "2" { t.Errorf("wrong request: %s", r.URL.String()) }
  w.Header().Set("Content-Type", "application/json")
  w.Write([]byte("{\"ok\":true}"))
 }))
 defer srv.Close()
 t.Setenv("DISCOVERY_CONTRACT_BASE_URL", srv.URL)
 ep := findCodeOrchEndpoint("sessions.flagged")
 if ep == nil || len(ep.Positional) != 1 || ep.Positional[0] != "itemId" { t.Fatalf("wrong path inputs: %#v", ep) }
 req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
  "endpoint_id": "sessions.flagged", "params": map[string]any{"itemId": "42", "page-number": float64(2)},
 }}}
 result, err := handleCodeOrchExecute(context.Background(), req)
 if err != nil || result.IsError { t.Fatalf("execute: %v %#v", err, result) }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "mcp", "discovery_contract_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, dir, "test", "./internal/mcp", "-run", "^Test(RequestInputDiscovery|RequestInputSearchAvoidsSubstringNoise|FlaggedPathInputExecutes)$", "-count=1")
	requireGeneratedCompiles(t, dir)
}
