package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPSharedBoundPackageAndConsumers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-bound-consumers")
	apiSpec.Resources = map[string]spec.Resource{
		"groups": {
			Description: "Groups",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/groups",
					Description: "List groups",
					Response:    spec.ResponseDef{Type: "array", Item: "Group"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Group": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	boundSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "bound", "bound.go"))
	require.NoError(t, err, "generated MCP CLIs must ship a shared bound package")
	boundCode := string(boundSrc)
	assert.Contains(t, boundCode, "package bound")
	assert.Contains(t, boundCode, "MaxBytes = 60000")
	assert.Contains(t, boundCode, "MaxItems = 50")
	assert.Contains(t, boundCode, "func EndpointResponse(")
	assert.Contains(t, boundCode, "func JSON(")
	assert.Contains(t, boundCode, "func Text(")

	_, err = os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "bound", "bound_test.go"))
	require.NoError(t, err, "the shared bound package must ship generated regression tests")

	toolsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	toolsCode := stripGoComments(string(toolsSrc))
	assert.Contains(t, toolsCode, `/internal/mcp/bound"`)
	assert.Contains(t, toolsCode, "bound.EndpointResponse(method, data)")
	assert.Contains(t, toolsCode, "bound.JSON(v)")
	assert.NotContains(t, toolsCode, "func mcpBoundedListEnvelope(")
	assert.NotContains(t, toolsCode, "func mcpOversizedPreviewEnvelope(")

	shelloutSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "shellout.go"))
	require.NoError(t, err)
	shelloutCode := stripGoComments(string(shelloutSrc))
	assert.Contains(t, shelloutCode, `/internal/mcp/bound"`)
	assert.Contains(t, shelloutCode, "bound.Text(out)")
	assert.NotContains(t, shelloutCode, "NewToolResultText(out)")

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/mcp/bound")
	runGoCommand(t, outputDir, "test", "./internal/mcp/cobratree")
}

func TestGenerateMCPToolResultJSONBoundsLargeResults(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-json-budget")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	testSrc := `package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolResultJSONBoundsLargeSearchLikeArray(t *testing.T) {
	rows := make([]map[string]string, 0, 80)
	for i := 0; i < 80; i++ {
		rows = append(rows, map[string]string{
			"id":   strings.Repeat("id", 80),
			"text": strings.Repeat("search result payload ", 90),
		})
	}

	result, err := toolResultJSON(rows)
	if err != nil {
		t.Fatalf("toolResultJSON returned error: %v", err)
	}
	text := mcpTextContent(t, result)
	if len(text) > 60000 {
		t.Fatalf("toolResultJSON length = %d, want <= 60000", len(text))
	}

	var envelope struct {
		Truncated     bool            ` + "`json:\"truncated\"`" + `
		OriginalBytes int             ` + "`json:\"original_bytes\"`" + `
		Items         json.RawMessage ` + "`json:\"items\"`" + `
		Preview       string          ` + "`json:\"preview\"`" + `
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("bounded result must remain JSON: %v\n%s", err, text)
	}
	if !envelope.Truncated {
		t.Fatalf("large result did not mark truncation: %s", text)
	}
	if envelope.OriginalBytes == 0 {
		t.Fatalf("large result did not include original_bytes: %s", text)
	}
	if len(envelope.Items) == 0 && envelope.Preview == "" {
		t.Fatalf("large result should include bounded items or a preview: %s", text)
	}
}
`

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "tool_result_json_budget_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "TestToolResultJSONBoundsLargeSearchLikeArray")
}
