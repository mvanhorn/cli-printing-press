package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// The read-only switch must leave only tools annotated readOnlyHint=true:
// typed endpoint tools, mirrored local-store writers, and <api>_execute too.
func TestGenerateMCPReadOnlySwitchDropsEveryNonReadOnlyTool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		orchestration string
		readTool      string
		writeTools    string
	}{
		{name: "mcp-ro-all-tools", readTool: "items_list", writeTools: `"items_create", "items_remove", "teach"`},
		{name: "mcp-ro-code-orch", orchestration: "code", readTool: "mcp-ro-code-orch_read", writeTools: `"mcp-ro-code-orch_execute", "teach"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runMCPReadOnlyAllToolsCase(t, tc.name, tc.orchestration, tc.readTool, tc.writeTools)
		})
	}
}

func runMCPReadOnlyAllToolsCase(t *testing.T, name, orchestration, readTool, writeTools string) {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Description: "List items"},
				"create": {Method: "POST", Path: "/items", Description: "Create an item"},
				"remove": {Method: "DELETE", Path: "/items/{id}", Description: "Delete an item"},
			},
		},
	}
	apiSpec.MCP = spec.MCPConfig{Orchestration: orchestration}
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsSrc := readGenerated(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, toolsSrc, "dropNonReadOnlyTools(s)")
	requireGeneratedCompiles(t, outputDir)

	envVar := naming.EnvPrefix(apiSpec.Name) + "_MCP_READ_ONLY"
	runtimeTest := strings.ReplaceAll(`package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func registeredTools(t *testing.T, readOnly string) map[string]*server.ServerTool {
	t.Helper()
	t.Setenv("ENV_VAR", readOnly)
	s := server.NewMCPServer("ro-all-tools", "test")
	RegisterTools(s)
	return s.ListTools()
}

func isReadOnly(entry *server.ServerTool) bool {
	hint := entry.Tool.Annotations.ReadOnlyHint
	return hint != nil && *hint
}

func TestReadOnlySwitchDropsEveryNonReadOnlyTool(t *testing.T) {
	off := registeredTools(t, "")
	for _, name := range []string{WRITE_TOOLS} {
		if entry, ok := off[name]; !ok || isReadOnly(entry) {
			t.Fatalf("switch off: %s should be registered as a write tool: %#v", name, off[name])
		}
	}

	on := registeredTools(t, "1")
	if _, ok := on["READ_TOOL"]; !ok {
		t.Fatalf("switch on: read tool READ_TOOL missing: %#v", on)
	}
	for name, entry := range on {
		if !isReadOnly(entry) {
			t.Errorf("switch on: %s is registered without readOnlyHint=true", name)
		}
	}
	for name, entry := range off {
		if isReadOnly(entry) {
			if _, ok := on[name]; !ok {
				t.Errorf("switch on: read-only tool %s was dropped", name)
			}
		}
	}
}
`, "ENV_VAR", envVar)
	runtimeTest = strings.ReplaceAll(runtimeTest, "WRITE_TOOLS", writeTools)
	runtimeTest = strings.ReplaceAll(runtimeTest, "READ_TOOL", readTool)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "read_only_all_tools_test.go"), []byte(runtimeTest), 0o644))
	output, err := runGoCommandOutputWithEnv(t, outputDir, []string{envVar + "="}, "test", "./internal/mcp", "-run", "^TestReadOnlySwitchDropsEveryNonReadOnlyTool$", "-count=1")
	require.NoError(t, err, output)
}
