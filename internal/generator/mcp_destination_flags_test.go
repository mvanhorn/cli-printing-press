package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPExportBlocksFilesystemDestinationFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcpdest")
	outputDir := filepath.Join(t.TempDir(), "mcpdest-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	const runtimeTest = `package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPExportDestinationFlagsBlocked(t *testing.T) {
	t.Setenv("MCPDEST_CLI_PATH", "fixture-cli")

	s := server.NewMCPServer("mcpdest", "test")
	RegisterTools(s)
	tools := s.ListTools()
	entry, ok := tools["export"]
	if !ok {
		t.Fatalf("export tool missing from tools/list: %#v", tools)
	}
	props := entry.Tool.InputSchema.Properties
	for _, want := range []string{"resource", "id", "format", "limit", "no-cache"} {
		if _, ok := props[want]; !ok {
			t.Fatalf("export tool schema missing %q: %#v", want, props)
		}
	}
	for _, hidden := range []string{"audit-dir", "o", "output", "receipt-file"} {
		if _, ok := props[hidden]; ok {
			t.Fatalf("filesystem destination %q leaked into export tool schema: %#v", hidden, props)
		}
		result, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
			Name:      "export",
			Arguments: map[string]any{"resource": "items", hidden: "/tmp/evil"},
		}})
		if err != nil {
			t.Fatalf("export handler returned transport error for %q: %v", hidden, err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("export handler accepted filesystem destination %q: %#v", hidden, result)
		}
		text := mcpTextContent(t, result)
		if !strings.Contains(text, "unknown MCP parameter") || !strings.Contains(text, hidden) {
			t.Fatalf("export handler error for %q = %q", hidden, text)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "export_destination_flags_test.go"), []byte(runtimeTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCPExportDestinationFlagsBlocked$", "-count=1")
}
