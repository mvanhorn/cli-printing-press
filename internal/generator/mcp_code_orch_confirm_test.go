package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerateMCPCodeOrchestrationRequiresTopLevelConfirmation proves the
// generated execute handler previews destructive calls without dialing the
// API, then dispatches exactly once after explicit top-level confirmation.
// It also keeps ordinary GET endpoints confirmation-free.
func TestGenerateMCPCodeOrchestrationRequiresTopLevelConfirmation(t *testing.T) {
	apiSpec := minimalSpec("code-orch-confirm")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.EndpointTemplateVars = []string{"tenant_id"}
	apiSpec.GlobalPathTemplateVars = []string{"tenant_id"}
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
				},
				"create": {
					Method:      "POST",
					Path:        "/records",
					Description: "Create a record",
				},
				"delete": {
					Method:      "DELETE",
					Path:        "/tenants/{tenant_id}/records/{id}",
					Description: "Delete a record",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true, Description: "Record ID",
					}},
				},
			},
		},
		"rpc": {
			Description: "RPC",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/rpc?method=list",
					Description: "List records through RPC",
					Params: []spec.Param{{
						Name: "page", Type: "integer", Description: "Page number",
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "config", "config.go"), `cliutil.EnvOverride("CODE_ORCH_CONFIRM_BASE_URL")`)

	runtimeTest := `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func callCodeOrchExecute(t *testing.T, args map[string]any) string {
	t.Helper()
	result, err := handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: args},
	})
	if err != nil {
		t.Fatalf("handleCodeOrchExecute() error = %v", err)
	}
	if result == nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("handleCodeOrchExecute() result = %#v", result)
	}
	text, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want TextContent", result.Content[0])
	}
	return text.Text
}

func callCodeOrchExecuteResult(t *testing.T, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	result, err := handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: args},
	})
	if err != nil {
		t.Fatalf("handleCodeOrchExecute() error = %v", err)
	}
	return result
}

func TestCodeOrchExecuteRequiresTopLevelConfirmation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("XDG_DATA_HOME", home+"/.local/share")
	t.Setenv("XDG_STATE_HOME", home+"/.local/state")
	t.Setenv("XDG_CACHE_HOME", home+"/.cache")
	var calls atomic.Int32
	var rpcQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/rpc" {
			rpcQuery = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"method\":\"" + r.Method + "\",\"path\":\"" + r.URL.EscapedPath() + "\"}"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODE_ORCH_CONFIRM_BASE_URL", server.URL)
	t.Setenv("CODE_ORCH_CONFIRM_TENANT_ID", "tenant-preview")

	preview := callCodeOrchExecute(t, map[string]any{
		"endpoint_id": "records.delete",
		// Confirmation nested under params must not dispatch: only the
		// top-level schema field is an explicit confirmation.
		"params": map[string]any{"id": "dangerous/../target", "confirm": true},
	})
	if !strings.Contains(preview, "\"preview\": true") || !strings.Contains(preview, "\"confirmation_required\": true") {
		t.Fatalf("unconfirmed DELETE result = %s, want confirmation preview", preview)
	}
	if !strings.Contains(preview, "\"method\": \"DELETE\"") || !strings.Contains(preview, "/tenants/tenant-preview/records/dangerous%2F..%2Ftarget") {
		t.Fatalf("preview did not report resolved destructive target: %s", preview)
	}

	postPreview := callCodeOrchExecute(t, map[string]any{
		"endpoint_id": "records.create",
		"params": map[string]any{"name": "unconfirmed"},
	})
	if !strings.Contains(postPreview, "\"confirmation_required\": true") {
		t.Fatalf("unconfirmed POST result = %s, want confirmation preview", postPreview)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("unconfirmed destructive calls reached API %d times, want 0", got)
	}

	callCodeOrchExecute(t, map[string]any{
		"endpoint_id": "records.delete",
		"confirm":     true,
		"params":      map[string]any{"id": "confirmed"},
	})
	if got := calls.Load(); got != 1 {
		t.Fatalf("confirmed DELETE reached API %d times, want 1", got)
	}

	blocked := callCodeOrchExecuteResult(t, map[string]any{
		"endpoint_id": "rpc.list",
		"params":      map[string]any{"method": "delete"},
	})
	if blocked == nil || !blocked.IsError || len(blocked.Content) != 1 {
		t.Fatalf("fixed query override result = %#v, want tool error", blocked)
	}
	blockedText, ok := blocked.Content[0].(mcplib.TextContent)
	if !ok || !strings.Contains(blockedText.Text, "cannot override fixed query parameter") {
		t.Fatalf("fixed query override error = %#v", blocked)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fixed query override reached API %d times, want 1", got)
	}

	callCodeOrchExecute(t, map[string]any{
		"endpoint_id": "rpc.list",
		"params":      map[string]any{"page": float64(2)},
	})
	if got := calls.Load(); got != 2 {
		t.Fatalf("ordinary RPC query reached API %d times, want 2", got)
	}
	if rpcQuery.Get("method") != "list" || rpcQuery.Get("page") != "2" {
		t.Fatalf("ordinary RPC query = %#v, want method=list and page=2", rpcQuery)
	}

	read := callCodeOrchExecute(t, map[string]any{"endpoint_id": "records.list"})
	if strings.Contains(read, "\"confirmation_required\"") {
		t.Fatalf("GET unexpectedly required confirmation: %s", read)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("GET without confirmation reached API %d times, want 3", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_confirm_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestCodeOrchExecuteRequiresTopLevelConfirmation$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

// TestGenerateMCPCodeOrchestrationPreviewResolvesGlobalPathVars covers prints
// that promote global path placeholders without endpoint template vars: the
// confirmation preview must still name the configured tenant.
func TestGenerateMCPCodeOrchestrationPreviewResolvesGlobalPathVars(t *testing.T) {
	apiSpec := minimalSpec("code-orch-global-path")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.GlobalPathTemplateVars = []string{"tenant_id"}
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/tenants/{tenant_id}/records", Description: "List records"},
				"delete": {
					Method:      "DELETE",
					Path:        "/tenants/{tenant_id}/records/{id}",
					Description: "Delete a record",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true, Description: "Record ID",
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	source := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, source, "codeOrchPreviewPath(path, c.Config.TemplateVars)")
	requireGeneratedCompiles(t, outputDir)
}
