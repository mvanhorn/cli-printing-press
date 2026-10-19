package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestCodeOrchEndpointNeedsConfirm(t *testing.T) {
	t.Parallel()

	readOnly := map[string]string{"mcp:read-only": "true"}
	mut := true
	tests := []struct {
		name string
		ep   spec.Endpoint
		op   string
		want bool
	}{
		{name: "get list", ep: spec.Endpoint{Method: "GET", Path: "/items"}, op: "list"},
		{name: "delete", ep: spec.Endpoint{Method: "DELETE", Path: "/items/1"}, op: "remove", want: true},
		{name: "delete read-only meta", ep: spec.Endpoint{Method: "DELETE", Path: "/items/1", Meta: readOnly}, op: "remove", want: true},
		{name: "lower delete read-only meta", ep: spec.Endpoint{Method: " delete ", Path: "/items/1", Meta: readOnly}, op: "remove", want: true},
		{name: "post", ep: spec.Endpoint{Method: "POST", Path: "/items"}, op: "create", want: true},
		{name: "post read-only meta", ep: spec.Endpoint{Method: "POST", Path: "/reports", Meta: readOnly}, op: "create"},
		{name: "get delete item", ep: spec.Endpoint{Method: "GET", Path: "/items/1"}, op: "delete-item", want: true},
		{name: "get list mutation", ep: spec.Endpoint{Method: "GET", Path: "/items", Mutation: &mut}, op: "list", want: true},
		{name: "get show", ep: spec.Endpoint{Method: "GET", Path: "/marked/1"}, op: "show"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := codeOrchEndpointNeedsConfirm(tt.ep, tt.op, nil)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCodeOrchHasEndpointClass(t *testing.T) {
	t.Parallel()

	require.False(t, codeOrchHasEndpointClass(nil, true))
	require.False(t, codeOrchHasEndpointClass(nil, false))
	require.False(t, codeOrchHasEndpointClass(map[string]spec.Resource{}, true))

	getOnly := map[string]spec.Resource{
		"items": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}}},
	}
	require.False(t, codeOrchHasEndpointClass(getOnly, true))
	require.True(t, codeOrchHasEndpointClass(getOnly, false))

	deleteOnly := map[string]spec.Resource{
		"items": {Endpoints: map[string]spec.Endpoint{"delete": {Method: "DELETE", Path: "/items/1"}}},
	}
	require.True(t, codeOrchHasEndpointClass(deleteOnly, true))
	require.False(t, codeOrchHasEndpointClass(deleteOnly, false))

	mixed := map[string]spec.Resource{
		"records": {
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/records"},
			},
			SubResources: map[string]spec.Resource{
				"notes": {Endpoints: map[string]spec.Endpoint{
					"delete": {Method: "DELETE", Path: "/records/{id}/notes/{note_id}"},
				}},
			},
		},
	}
	require.True(t, codeOrchHasEndpointClass(mixed, true))
	require.True(t, codeOrchHasEndpointClass(mixed, false))
}

func TestCodeOrchExecuteConfirmGate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("code-orch-confirm")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.EndpointTemplateVars = []string{"tenant_id"}
	apiSpec.GlobalPathTemplateVars = []string{"tenant_id"}
	apiSpec.Resources = codeOrchConfirmResources()

	outputDir := filepath.Join(t.TempDir(), "code-orch-confirm-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")

	require.Contains(t, configSrc, `cliutil.EnvOverride("CODE_ORCH_CONFIRM_BASE_URL")`)
	require.Contains(t, configSrc, `cliutil.EnvOverride("CODE_ORCH_CONFIRM_TENANT_ID")`)
	for _, snippet := range []string{
		`mcplib.NewTool("code-orch-confirm_read"`,
		`mcplib.NewTool("code-orch-confirm_execute"`,
		`mcplib.WithBoolean("confirm"`,
		`mcplib.WithReadOnlyHintAnnotation(true)`,
		`mcplib.WithDestructiveHintAnnotation(true)`,
		`func codeOrchResolvedNeedsConfirm(`,
		`func codeOrchConfirmAccepted(`,
		`resolvedPath = codeOrchPreviewPath(path, c.Config.TemplateVars)`,
		`NeedsConfirm bool`,
	} {
		require.Contains(t, src, snippet)
	}
	require.Regexp(t, `(?s)ID:\s+"records\.list",.*?NeedsConfirm:\s+false`, src)
	require.Regexp(t, `(?s)ID:\s+"records\.delete",.*?NeedsConfirm:\s+true`, src)
	require.Regexp(t, `(?s)ID:\s+"marked\.show",.*?NeedsConfirm:\s+true,\s+Mutating:\s+true`, src)
	require.Regexp(t, `(?s)ID:\s+"reports\.run",.*?NeedsConfirm:\s+false`, src)
	require.Regexp(t, `(?s)ID:\s+"catalog\.search",.*?NeedsConfirm:\s+false`, src)
	require.Regexp(t, `(?s)ID:\s+"catalog\.update",.*?NeedsConfirm:\s+true`, src)
	require.Regexp(t, `(?s)ID:\s+"reports\.wipe",.*?NeedsConfirm:\s+true`, src)
	require.Contains(t, src, "func codeOrchPatternSpecificity(")
	require.Regexp(t, `(?s)ID:\s+"rpc\.delete",.*?NeedsConfirm:\s+true,\s+Mutating:\s+true`, src)
	require.Regexp(t, `(?s)ID:\s+"rpc\.list",.*?NeedsConfirm:\s+false`, src)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_confirm_runtime_test.go"), []byte(codeOrchConfirmRuntimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestCodeOrchConfirmRuntime", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestCodeOrchOmitsExecuteWithoutMutations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("orch-reads")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {Description: "Items", Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), "orch-reads-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, src, `mcplib.NewTool("orch-reads_read"`)
	require.NotContains(t, src, `mcplib.NewTool("orch-reads_execute"`)
	require.Contains(t, src, "func handleCodeOrchExecute(")
	require.Contains(t, src, `endpoint_id %q is read-only; call orch-reads_read`)
	require.Contains(t, src, `resolved %s %s changes state`)
	require.NotContains(t, src, "call orch-reads_execute")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_reads_runtime_test.go"), []byte(codeOrchReadsRuntimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestCodeOrchReadsRuntime", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestCodeOrchOmitsReadWithoutReads(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("orch-writes")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {Description: "Items", Endpoints: map[string]spec.Endpoint{
			"delete": {Method: "DELETE", Path: "/items/{id}", Description: "Delete an item", Params: []spec.Param{
				{Name: "id", Type: "string", Positional: true, PathParam: true},
			}},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), "orch-writes-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, src, `mcplib.NewTool("orch-writes_execute"`)
	require.NotContains(t, src, `mcplib.NewTool("orch-writes_read"`)
	require.Contains(t, src, "func handleCodeOrchRead(")
	require.Contains(t, src, `endpoint_id %q is read-only"`)
	require.NotContains(t, src, "call orch-writes_read")
	require.Contains(t, src, "call orch-writes_execute with confirm set to boolean true")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_writes_runtime_test.go"), []byte(codeOrchWritesRuntimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestCodeOrchWritesRuntime", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func codeOrchConfirmResources() map[string]spec.Resource {
	mut := true
	return map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/records", Description: "List records"},
				"delete": {
					Method:      "DELETE",
					Path:        "/tenants/{tenant_id}/records/{id}",
					Description: "Delete a record",
					Params: []spec.Param{
						{Name: "id", Type: "string", Positional: true, PathParam: true},
					},
				},
			},
			SubResources: map[string]spec.Resource{
				"notes": {
					Description: "Notes",
					Endpoints: map[string]spec.Endpoint{
						"delete": {
							Method:      "DELETE",
							Path:        "/records/{id}/notes/{note_id}",
							Description: "Delete a note",
							Params: []spec.Param{
								{Name: "id", Type: "string", Positional: true, PathParam: true},
								{Name: "note_id", Type: "string", Positional: true, PathParam: true},
							},
						},
					},
				},
			},
		},
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/items/{id}",
					Description: "Get an item",
					Params:      []spec.Param{{Name: "id", Type: "string", Positional: true, PathParam: true}},
				},
				"delete": {
					Method:      "DELETE",
					Path:        "/items/{id}",
					Description: "Delete an item",
					Params:      []spec.Param{{Name: "id", Type: "string", Positional: true, PathParam: true}},
				},
			},
		},
		"marked": {
			Description: "Marks",
			Endpoints: map[string]spec.Endpoint{
				"show": {
					Method:      "GET",
					Path:        "/marked/{id}",
					Description: "Consume a mark",
					Mutation:    &mut,
					Params:      []spec.Param{{Name: "id", Type: "string", Positional: true, PathParam: true}},
				},
			},
		},
		"reports": {
			Description: "Reports",
			Endpoints: map[string]spec.Endpoint{
				"run": {
					Method:      "POST",
					Path:        "/reports/{name}",
					Description: "Run a report",
					Meta:        map[string]string{"mcp:read-only": "true"},
					Params:      []spec.Param{{Name: "name", Type: "string", Positional: true, PathParam: true}},
					Body:        []spec.Param{{Name: "format", Type: "string"}},
				},
				"wipe": {
					Method:      "POST",
					Path:        "/reports/wipe",
					Description: "Wipe reports",
					Body:        []spec.Param{{Name: "reason", Type: "string"}},
				},
			},
		},
		"catalog": {
			Description: "Catalog",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "POST",
					Path:        "/catalog/search",
					Description: "Search the catalog",
					Meta:        map[string]string{"mcp:read-only": "true"},
					Body:        []spec.Param{{Name: "q", Type: "string"}},
				},
				"update": {
					Method:      "POST",
					Path:        "/catalog/{id}",
					Description: "Update a catalog entry",
					Params:      []spec.Param{{Name: "id", Type: "string", Positional: true, PathParam: true}},
					Body:        []spec.Param{{Name: "note", Type: "string"}},
				},
			},
		},
		"rpc": {
			Description: "RPC",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/rpc?method=list", Description: "List via RPC"},
				"delete": {Method: "GET", Path: "/rpc?method=delete", Description: "Delete via RPC"},
			},
		},
	}
}

const codeOrchConfirmRuntimeTest = `package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type seenRequest struct {
	Method   string
	Path     string
	RawQuery string
}

func TestCodeOrchConfirmRuntime(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	decoy := t.TempDir()
	for _, sub := range []string{".config", ".local/share", ".local/state", ".cache"} {
		if err := os.MkdirAll(filepath.Join(decoy, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", decoy)
	t.Setenv("USERPROFILE", decoy)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(decoy, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(decoy, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(decoy, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(decoy, ".cache"))

	s := server.NewMCPServer("code-orch-confirm", "test")
	RegisterCodeOrchestrationTools(s)
	tools := s.ListTools()
	readTool, ok := tools["code-orch-confirm_read"]
	if !ok {
		t.Fatalf("read tool missing: %#v", tools)
	}
	execTool, ok := tools["code-orch-confirm_execute"]
	if !ok {
		t.Fatalf("execute tool missing: %#v", tools)
	}
	if readTool.Tool.Annotations.ReadOnlyHint == nil || !*readTool.Tool.Annotations.ReadOnlyHint {
		t.Fatal("read tool is not read-only")
	}
	if readTool.Tool.Annotations.DestructiveHint == nil || *readTool.Tool.Annotations.DestructiveHint {
		t.Fatal("read tool is destructive")
	}
	if readTool.Tool.Annotations.OpenWorldHint == nil || !*readTool.Tool.Annotations.OpenWorldHint {
		t.Fatal("read tool is not open-world")
	}
	if execTool.Tool.Annotations.ReadOnlyHint == nil || *execTool.Tool.Annotations.ReadOnlyHint {
		t.Fatal("execute tool is read-only")
	}
	if execTool.Tool.Annotations.DestructiveHint == nil || !*execTool.Tool.Annotations.DestructiveHint {
		t.Fatal("execute tool is not destructive")
	}
	if execTool.Tool.Annotations.OpenWorldHint == nil || !*execTool.Tool.Annotations.OpenWorldHint {
		t.Fatal("execute tool is not open-world")
	}
	if _, ok := execTool.Tool.InputSchema.Properties["confirm"]; !ok {
		t.Fatal("execute schema missing confirm")
	}
	for _, name := range execTool.Tool.InputSchema.Required {
		if name == "confirm" {
			t.Fatal("confirm must stay optional so a preview call is valid")
		}
	}
	if _, ok := readTool.Tool.InputSchema.Properties["confirm"]; ok {
		t.Fatal("read schema exposes confirm")
	}

	var mu sync.Mutex
	var seen []seenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, seenRequest{Method: r.Method, Path: r.URL.EscapedPath(), RawQuery: r.URL.RawQuery})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("CODE_ORCH_CONFIRM_BASE_URL", server.URL)
	t.Setenv("CODE_ORCH_CONFIRM_TENANT_ID", "tenant-preview")

	calls := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(seen)
	}
	last := func() seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return seen[len(seen)-1]
	}

	for _, confirm := range []any{nil, "true", false, float64(1)} {
		before := calls()
		args := map[string]any{"endpoint_id": "records.delete", "params": map[string]any{"id": "rec-1"}}
		if confirm != nil {
			args["confirm"] = confirm
		}
		_, text := mustCall(t, true, args)
		preview := mustPreview(t, text)
		if preview["method"] != "DELETE" || !strings.Contains(preview["path"].(string), "tenant-preview") || !strings.Contains(preview["path"].(string), "rec-1") {
			t.Fatalf("delete preview = %#v", preview)
		}
		if preview["body"] != nil {
			t.Fatalf("delete preview body = %#v", preview["body"])
		}
		if calls() != before {
			t.Fatalf("unconfirmed delete made %d requests", calls()-before)
		}
	}
	before := calls()
	_, text := mustCall(t, true, map[string]any{
		"endpoint_id": "records.delete",
		"params":      map[string]any{"id": "rec-1", "confirm": true},
	})
	if !strings.Contains(text, "confirmation_required") || calls() != before {
		t.Fatalf("params.confirm dispatched or skipped preview: calls=%d text=%s", calls()-before, text)
	}

	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "records.delete", "confirm": true, "params": map[string]any{"id": "rec-1"}})
	if strings.Contains(text, "confirmation_required") {
		t.Fatalf("confirmed delete previewed: %s", text)
	}
	if calls() != before+1 || last().Method != "DELETE" || !strings.Contains(last().Path, "/tenants/tenant-preview/records/rec-1") {
		t.Fatalf("confirmed delete = %d %#v", calls()-before, last())
	}

	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "records.list"})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "GET" || last().Path != "/records" {
		t.Fatalf("list read = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	result, text := mustCall(t, true, map[string]any{"endpoint_id": "records.list", "confirm": true})
	if !result.IsError || !strings.Contains(text, "read-only") || calls() != before {
		t.Fatalf("execute read = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}

	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "items.get", "params": map[string]any{"id": "1"}})
	if calls() != before+1 || last().Method != "GET" || last().Path != "/items/1" {
		t.Fatalf("items.get = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "items.delete", "params": map[string]any{"id": "1"}})
	preview := mustPreview(t, text)
	if preview["method"] != "DELETE" || preview["path"] != "/items/1" || calls() != before {
		t.Fatalf("items.delete preview = %#v calls=%d", preview, calls()-before)
	}

	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "marked.show", "params": map[string]any{"id": "mark-1"}})
	preview = mustPreview(t, text)
	if preview["method"] != "GET" || !strings.Contains(preview["path"].(string), "/marked/mark-1") || calls() != before {
		t.Fatalf("marked preview = %#v calls=%d", preview, calls()-before)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "marked.show", "confirm": true, "params": map[string]any{"id": "mark-1"}})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "GET" || last().Path != "/marked/mark-1" {
		t.Fatalf("marked confirm = %d %#v text=%s", calls()-before, last(), text)
	}

	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "quarterly", "format": "json"}})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "POST" || last().Path != "/reports/quarterly" {
		t.Fatalf("report read = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	result, text = mustCall(t, false, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "wipe", "format": "json"}})
	if !result.IsError || !strings.Contains(text, "changes state") || !strings.Contains(text, "/reports/wipe") || calls() != before {
		t.Fatalf("report wipe via read = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "wipe", "format": "json"}})
	preview = mustPreview(t, text)
	if preview["method"] != "POST" || preview["path"] != "/reports/wipe" || calls() != before {
		t.Fatalf("report wipe preview = %#v calls=%d", preview, calls()-before)
	}
	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "wipe/secret"}})
	if resultIsError(text) || calls() != before+1 || last().Path == "/reports/wipe" || !strings.Contains(last().Path, "wipe%2Fsecret") {
		t.Fatalf("encoded slash collapsed onto wipe: %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "reports.wipe", "confirm": true, "params": map[string]any{"reason": "cleanup"}})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "POST" || last().Path != "/reports/wipe" {
		t.Fatalf("wipe confirm = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "catalog.search", "params": map[string]any{"q": "open"}})
	if strings.Contains(text, "confirmation_required") || strings.Contains(text, "changes state") || calls() != before+1 || last().Method != "POST" || last().Path != "/catalog/search" {
		t.Fatalf("explicit search read = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "catalog.search", "params": map[string]any{"q": "open"}})
	if !result.IsError || !strings.Contains(text, "read-only") || calls() != before {
		t.Fatalf("explicit search via execute = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}

	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "rpc.list"})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "GET" || !strings.Contains(last().RawQuery, "method=list") {
		t.Fatalf("rpc list = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	result, text = mustCall(t, false, map[string]any{"endpoint_id": "rpc.list", "params": map[string]any{"method": "delete"}})
	if !result.IsError || !strings.Contains(text, "changes state") || calls() != before {
		t.Fatalf("rpc override via read = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "rpc.list", "params": map[string]any{"method": "explode"}})
	preview = mustPreview(t, text)
	query, _ := preview["query"].(map[string]any)
	if preview["method"] != "GET" || query["method"] != "explode" || calls() != before {
		t.Fatalf("rpc explode preview = %#v calls=%d", preview, calls()-before)
	}
	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "rpc.list", "confirm": true, "params": map[string]any{"method": "delete"}})
	if strings.Contains(text, "confirmation_required") || calls() != before+1 || !strings.Contains(last().RawQuery, "method=delete") {
		t.Fatalf("rpc delete confirm = %d %#v text=%s", calls()-before, last(), text)
	}

	before = calls()
	_, text = mustCall(t, true, map[string]any{"endpoint_id": "records.notes.delete", "params": map[string]any{"id": "rec-1", "note_id": "note-9"}})
	preview = mustPreview(t, text)
	if preview["method"] != "DELETE" || preview["path"] != "/records/rec-1/notes/note-9" || calls() != before {
		t.Fatalf("note delete preview = %#v calls=%d", preview, calls()-before)
	}

	for _, method := range []string{"delete", "Delete", " delete "} {
		ep := findCodeOrchEndpoint("items.delete")
		saved := *ep
		ep.Method = method
		ep.NeedsConfirm = false
		ep.Mutating = false
		before = calls()
		_, text = mustCall(t, true, map[string]any{"endpoint_id": "items.delete", "params": map[string]any{"id": "9"}})
		preview = mustPreview(t, text)
		if preview["method"] != "DELETE" || calls() != before {
			*ep = saved
			t.Fatalf("method %q preview = %#v calls=%d", method, preview, calls()-before)
		}
		before = calls()
		_, text = mustCall(t, true, map[string]any{"endpoint_id": "items.delete", "confirm": true, "params": map[string]any{"id": "9"}})
		*ep = saved
		if strings.Contains(text, "confirmation_required") || calls() != before+1 || last().Method != "DELETE" || last().Path != "/items/9" {
			t.Fatalf("method %q confirm = %d %#v text=%s", method, calls()-before, last(), text)
		}
	}

	ep := findCodeOrchEndpoint("items.delete")
	saved := *ep
	ep.Method = "TRACE"
	ep.NeedsConfirm = true
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "items.delete", "confirm": true, "params": map[string]any{"id": "9"}})
	*ep = saved
	if !result.IsError || !strings.Contains(text, "unsupported method") || calls() != before {
		t.Fatalf("TRACE = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}

	for _, args := range []map[string]any{nil, {"endpoint_id": "missing.endpoint"}} {
		before = calls()
		result, _ = mustCall(t, true, args)
		if !result.IsError || calls() != before {
			t.Fatalf("bad endpoint args %#v error:%v calls:%d", args, result.IsError, calls()-before)
		}
	}

	t.Setenv("CODE_ORCH_CONFIRM_MCP_READ_ONLY", "1")
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "records.delete", "confirm": true, "params": map[string]any{"id": "rec-1"}})
	if !result.IsError || !strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || strings.Contains(text, "loading config") || calls() != before {
		t.Fatalf("read-only delete = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "marked.show", "confirm": true, "params": map[string]any{"id": "mark-1"}})
	if !result.IsError || !strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || strings.Contains(text, "loading config") || calls() != before {
		t.Fatalf("read-only mutating GET = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "reports.run", "confirm": true, "params": map[string]any{"name": "quarterly"}})
	if !result.IsError || !strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || calls() != before {
		t.Fatalf("read-only execute POST = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	result, text = mustCall(t, true, map[string]any{"endpoint_id": "rpc.list", "confirm": true, "params": map[string]any{"method": "delete"}})
	if !result.IsError || !strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || strings.Contains(text, "loading config") || calls() != before {
		t.Fatalf("read-only resolved mutating GET = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	result, text = mustCall(t, false, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "wipe"}})
	if !result.IsError || !strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || calls() != before {
		t.Fatalf("read-only resolved write via read = error:%v calls:%d text:%s", result.IsError, calls()-before, text)
	}
	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "reports.run", "params": map[string]any{"name": "quarterly"}})
	if strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || calls() != before+1 || last().Method != "POST" {
		t.Fatalf("read-only switch blocked read POST = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "catalog.search"})
	if strings.Contains(text, "CODE_ORCH_CONFIRM_MCP_READ_ONLY") || strings.Contains(text, "changes state") || calls() != before+1 || last().Path != "/catalog/search" {
		t.Fatalf("read-only switch blocked explicit search = %d %#v text=%s", calls()-before, last(), text)
	}
	before = calls()
	_, text = mustCall(t, false, map[string]any{"endpoint_id": "records.list"})
	if calls() != before+1 || last().Method != "GET" || last().Path != "/records" {
		t.Fatalf("read-only list = %d %#v text=%s", calls()-before, last(), text)
	}
}

func mustCall(t *testing.T, execute bool, args map[string]any) (*mcplib.CallToolResult, string) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: args}}
	var result *mcplib.CallToolResult
	var err error
	if execute {
		result, err = handleCodeOrchExecute(context.Background(), req)
	} else {
		result, err = handleCodeOrchRead(context.Background(), req)
	}
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	text := ""
	if result != nil && len(result.Content) > 0 {
		if content, ok := result.Content[0].(mcplib.TextContent); ok {
			text = content.Text
		}
	}
	return result, text
}

func mustPreview(t *testing.T, text string) map[string]any {
	t.Helper()
	var preview map[string]any
	if err := json.Unmarshal([]byte(text), &preview); err != nil {
		t.Fatalf("preview json: %v\n%s", err, text)
	}
	if preview["preview"] != true || preview["confirmation_required"] != true {
		t.Fatalf("preview flags = %#v", preview)
	}
	return preview
}

func resultIsError(text string) bool {
	return strings.Contains(text, "changes state") || strings.Contains(text, "read-only")
}
`

const codeOrchReadsRuntimeTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestCodeOrchReadsRuntime(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer srv.Close()
	t.Setenv("ORCH_READS_BASE_URL", srv.URL)

	s := server.NewMCPServer("orch-reads", "test")
	RegisterCodeOrchestrationTools(s)
	if _, ok := s.ListTools()["orch-reads_execute"]; ok {
		t.Fatal("read-only spec registered execute")
	}
	if _, ok := s.ListTools()["orch-reads_read"]; !ok {
		t.Fatal("read-only spec missing read")
	}

	result, err := handleCodeOrchRead(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.list",
	}}})
	if err != nil || result.IsError || hits.Load() != 1 {
		t.Fatalf("list read failed: hits=%d result=%#v err=%v", hits.Load(), result, err)
	}

	ep := findCodeOrchEndpoint("items.list")
	saved := *ep
	ep.Method = "DELETE"
	ep.NeedsConfirm = false
	result, err = handleCodeOrchRead(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.list",
	}}})
	*ep = saved
	text := toolText(result)
	if err != nil || result == nil || !result.IsError || !strings.Contains(text, "changes state") || strings.Contains(text, "orch-reads_execute") || hits.Load() != 1 {
		t.Fatalf("forced delete via read = hits:%d text:%s err:%v", hits.Load(), text, err)
	}

	result, err = handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.list",
		"confirm":     true,
	}}})
	text = toolText(result)
	if err != nil || result == nil || !result.IsError || !strings.Contains(text, "orch-reads_read") || hits.Load() != 1 {
		t.Fatalf("execute on read-only spec = hits:%d text:%s err:%v", hits.Load(), text, err)
	}
}

func toolText(result *mcplib.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(mcplib.TextContent); ok {
		return content.Text
	}
	return ""
}
`

const codeOrchWritesRuntimeTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestCodeOrchWritesRuntime(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s", r.Method)
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer srv.Close()
	t.Setenv("ORCH_WRITES_BASE_URL", srv.URL)

	s := server.NewMCPServer("orch-writes", "test")
	RegisterCodeOrchestrationTools(s)
	if _, ok := s.ListTools()["orch-writes_read"]; ok {
		t.Fatal("write-only spec registered read")
	}

	result, err := handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.delete",
		"params":      map[string]any{"id": "1"},
	}}})
	text := writeToolText(result)
	if err != nil || result == nil || result.IsError || !strings.Contains(text, "confirmation_required") || hits.Load() != 0 {
		t.Fatalf("unconfirmed delete = hits:%d text:%s err:%v", hits.Load(), text, err)
	}

	result, err = handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.delete",
		"confirm":     "true",
		"params":      map[string]any{"id": "1"},
	}}})
	text = writeToolText(result)
	if err != nil || result == nil || result.IsError || !strings.Contains(text, "confirmation_required") || hits.Load() != 0 {
		t.Fatalf("string confirm = hits:%d text:%s err:%v", hits.Load(), text, err)
	}

	result, err = handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.delete",
		"confirm":     true,
		"params":      map[string]any{"id": "1"},
	}}})
	text = writeToolText(result)
	if err != nil || result == nil || result.IsError || strings.Contains(text, "confirmation_required") || hits.Load() != 1 {
		t.Fatalf("confirmed delete = hits:%d text:%s err:%v", hits.Load(), text, err)
	}

	ep := findCodeOrchEndpoint("items.delete")
	saved := *ep
	ep.Method = "GET"
	ep.NeedsConfirm = false
	ep.Mutating = false
	result, err = handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.delete",
		"confirm":     true,
	}}})
	*ep = saved
	text = writeToolText(result)
	if err != nil || result == nil || !result.IsError || !strings.Contains(text, "read-only") || strings.Contains(text, "orch-writes_read") || hits.Load() != 1 {
		t.Fatalf("execute read branch = hits:%d text:%s err:%v", hits.Load(), text, err)
	}
}

func writeToolText(result *mcplib.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(mcplib.TextContent); ok {
		return content.Text
	}
	return ""
}
`
