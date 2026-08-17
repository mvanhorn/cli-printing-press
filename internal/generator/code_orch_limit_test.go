package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestCodeOrchestrationSearchLimitClamp(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("code-orch-limit")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}

	endpoints := make(map[string]spec.Endpoint, 125)
	for i := range 125 {
		name := fmt.Sprintf("list-%03d", i)
		endpoints[name] = spec.Endpoint{
			Method:      "GET",
			Path:        fmt.Sprintf("/items/%03d", i),
			Description: fmt.Sprintf("List searchable items %03d", i),
		}
	}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Searchable items",
			Endpoints:   endpoints,
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runtimeTest := `package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCodeOrchestrationSearchLimitClamp(t *testing.T) {
	tests := map[string]struct {
		limit any
		want  int
	}{
		"default": {want: codeOrchSearchDefaultLimit},
		"negative": {limit: -5.0, want: codeOrchSearchDefaultLimit},
		"normal": {limit: 3.0, want: 3},
		"huge": {limit: 1e19, want: codeOrchSearchMaxLimit},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"query": "searchable items"}
			if test.limit != nil {
				args["limit"] = test.limit
			}

			result, err := handleCodeOrchSearch(context.Background(), mcplib.CallToolRequest{
				Params: mcplib.CallToolParams{Arguments: args},
			})
			if err != nil || result == nil || result.IsError {
				t.Fatalf("search failed: result=%#v err=%v", result, err)
			}
			if len(result.Content) != 1 {
				t.Fatalf("content length = %d, want 1", len(result.Content))
			}
			text, ok := result.Content[0].(mcplib.TextContent)
			if !ok {
				t.Fatalf("content type = %T, want TextContent", result.Content[0])
			}
			var envelope struct {
				Count   int              ` + "`json:\"count\"`" + `
				Results []map[string]any ` + "`json:\"results\"`" + `
			}
			if err := json.Unmarshal([]byte(text.Text), &envelope); err != nil {
				t.Fatalf("decode search result: %v", err)
			}
			if envelope.Count != test.want || len(envelope.Results) != test.want {
				t.Fatalf("result count = %d/%d, want %d", envelope.Count, len(envelope.Results), test.want)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_limit_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestCodeOrchestrationSearchLimitClamp$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
