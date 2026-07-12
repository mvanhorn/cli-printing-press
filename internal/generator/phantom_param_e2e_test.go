package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPhantomBracketParamAbsentFromMCPSurface is the end-to-end companion to
// the mapParameters unit test for issue #1670: it parses a real OpenAPI doc
// whose operation declares a phantom "[]" query parameter, generates the CLI,
// and asserts the emitted MCP tool schemas (which the tools-manifest.json is
// built from) never expose it — while a legitimate sibling param survives.
func TestPhantomBracketParamAbsentFromMCPSurface(t *testing.T) {
	t.Parallel()

	const doc = `
openapi: 3.0.0
info:
  title: Phantom API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /broadcasts:
    get:
      operationId: listBroadcasts
      tags: [broadcasts]
      parameters:
        - name: "[]"
          in: query
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: ok
`
	apiSpec, err := openapi.Parse([]byte(doc))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "phantom-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(toolsSrc)

	// The quoted param name "[]" must never appear (Go slice literals like
	// []mcpParamBinding{ contain unquoted [] and are not matched here).
	assert.NotContains(t, content, `"[]"`,
		"phantom []-named param must not reach the MCP tool surface")
	assert.True(t, strings.Contains(content, `"limit"`),
		"the legitimate sibling param must still be emitted")
}
