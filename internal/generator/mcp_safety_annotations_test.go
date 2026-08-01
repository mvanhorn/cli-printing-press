package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPCodeOrchestrationSafetyAnnotations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-safety")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Description: "List items"},
				"create": {Method: "POST", Path: "/items", Description: "Create an item"},
			},
		},
	}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runtimeTest := `package mcp

import (
	"context"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestCodeOrchestrationSafetyMetadata(t *testing.T) {
	s := server.NewMCPServer("mcp-safety", "test")
	RegisterCodeOrchestrationTools(s)
	tools := s.ListTools()

	tests := map[string]struct {
		readOnly   bool
		destructive bool
		openWorld   bool
	}{
		"mcp-safety_search":  {readOnly: true, destructive: false, openWorld: false},
		"mcp-safety_get":     {readOnly: true, destructive: false, openWorld: false},
		"mcp-safety_execute": {readOnly: false, destructive: true, openWorld: true},
	}
	for name, want := range tests {
		entry, ok := tools[name]
		if !ok {
			t.Fatalf("tool %q missing from tools/list: %#v", name, tools)
		}
		if entry.Tool.Annotations.ReadOnlyHint == nil || *entry.Tool.Annotations.ReadOnlyHint != want.readOnly {
			t.Fatalf("%s readOnlyHint = %v, want %v", name, entry.Tool.Annotations.ReadOnlyHint, want.readOnly)
		}
		if entry.Tool.Annotations.DestructiveHint == nil || *entry.Tool.Annotations.DestructiveHint != want.destructive {
			t.Fatalf("%s destructiveHint = %v, want %v", name, entry.Tool.Annotations.DestructiveHint, want.destructive)
		}
		if entry.Tool.Annotations.OpenWorldHint == nil || *entry.Tool.Annotations.OpenWorldHint != want.openWorld {
			t.Fatalf("%s openWorldHint = %v, want %v", name, entry.Tool.Annotations.OpenWorldHint, want.openWorld)
		}
	}
}

func TestCodeOrchestrationGetRejectsWrites(t *testing.T) {
	tests := map[string]struct {
		args map[string]any
	}{
		"missing endpoint_id": {args: map[string]any{}},
		"non-string endpoint_id": {args: map[string]any{"endpoint_id": 42}},
		"unknown endpoint_id": {args: map[string]any{"endpoint_id": "items.unknown"}},
		"write endpoint":      {args: map[string]any{"endpoint_id": "items.create"}},
	}
	for name, test := range tests {
		result, err := handleCodeOrchGet(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: test.args}})
		if err != nil {
			t.Fatalf("handleCodeOrchGet(%s) returned transport error: %v", name, err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("handleCodeOrchGet(%s) IsError = %v, want true", name, result != nil && result.IsError)
		}
	}

	result, err := handleCodeOrchGet(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Arguments: map[string]any{"endpoint_id": "items.list"},
	}})
	if err != nil {
		t.Fatalf("handleCodeOrchGet(GET) returned transport error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("handleCodeOrchGet(GET) failed: %#v", result)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "mcp_safety_annotations_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestCodeOrchestration(SafetyMetadata|GetRejectsWrites)", "-count=1")
}

func TestGeneratePlatformClientMCPAnnotations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("platform-mcp-safety")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	modulePath := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(string(goMod), "\n", 2)[0], "module "))

	runtimeTest := fmt.Sprintf(`package cli

import (
	"testing"

	"%s/internal/mcp/cobratree"
	"github.com/mark3labs/mcp-go/server"
)

func TestPlatformClientMCPAnnotations(t *testing.T) {
	previousRegistration := registeredPlatformSource
	t.Cleanup(func() { registeredPlatformSource = previousRegistration })
	registeredPlatformSource = &platformSourceRegistration{Source: "test-source", Adapter: conformanceIdentityAdapter{}}

	s := server.NewMCPServer("platform-mcp-safety", "test")
	cobratree.RegisterAll(s, RootCmd(), func() (string, error) { return "fixture", nil })
	tools := s.ListTools()

	tests := map[string]struct {
		readOnly   bool
		destructive bool
		openWorld   bool
	}{
		"client_list":        {readOnly: true, destructive: false, openWorld: true},
		"client_show":        {readOnly: true, destructive: false, openWorld: true},
		"client_add":         {readOnly: false, destructive: true, openWorld: true},
		"client_source_set":  {readOnly: false, destructive: true, openWorld: true},
		"client_set_default": {readOnly: false, destructive: true, openWorld: true},
		"client_cache_clear": {readOnly: false, destructive: true, openWorld: true},
		"client_validate":    {readOnly: false, destructive: true, openWorld: true},
		"client_migrate":     {readOnly: false, destructive: true, openWorld: true},
		"client_delete":      {readOnly: false, destructive: true, openWorld: true},
		"whoami":             {readOnly: false, destructive: true, openWorld: true},
	}
	for name, want := range tests {
		entry, ok := tools[name]
		if !ok {
			t.Fatalf("tool %%q missing from tools/list: %%#v", name, tools)
		}
		annotations := entry.Tool.Annotations
		if annotations.ReadOnlyHint == nil || *annotations.ReadOnlyHint != want.readOnly {
			t.Fatalf("%%s readOnlyHint = %%v, want %%v", name, annotations.ReadOnlyHint, want.readOnly)
		}
		if annotations.DestructiveHint == nil || *annotations.DestructiveHint != want.destructive {
			t.Fatalf("%%s destructiveHint = %%v, want %%v", name, annotations.DestructiveHint, want.destructive)
		}
		if annotations.OpenWorldHint == nil || *annotations.OpenWorldHint != want.openWorld {
			t.Fatalf("%%s openWorldHint = %%v, want %%v", name, annotations.OpenWorldHint, want.openWorld)
		}
	}
}
`, modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "platform_mcp_annotations_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestPlatformClientMCPAnnotations$", "-count=1")
}
