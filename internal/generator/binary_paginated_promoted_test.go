package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: promoted endpoints that combine UsesBinaryResponse with
// either pagination or store-backed read previously called paginatedGet /
// resolvePaginatedRead / resolveRead with nil headers. The live API call
// then went out without the BinaryResponseHeader sentinel, so the client
// ran sanitizeJSONResponse on raw binary bytes (corrupting audio) and
// advertised Accept: application/json. The fix hoists headerOverrides
// when UsesBinaryResponse is set and threads it into every helper call.
func TestGenerateBinaryPaginatedPromotedThreadsHeader(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"voices": {
			Description: "Voices",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         "GET",
					Path:           "/voices",
					Description:    "List voices",
					ResponseFormat: spec.ResponseFormatBinary,
					Pagination: &spec.Pagination{
						Type:           "cursor",
						LimitParam:     "limit",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Params: []spec.Param{
						{Name: "limit", Type: "integer", Description: "Page size"},
						{Name: "after", Type: "string", Description: "Cursor"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	// Force no-store so the paginatedGet branch is exercised; resolvePaginatedRead
	// would be wired the same way (both accept the headers map). Export keeps
	// VisionSet.IsZero() false so the generator doesn't recompute from profile.
	gen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, gen.Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_voices.go")
	assert.Contains(t, endpointSrc, `headerOverrides := map[string]string{`,
		"binary paginated promoted must declare headerOverrides")
	assert.Contains(t, endpointSrc, `"X-Printing-Press-Binary-Response": "true",`,
		"binary paginated promoted must include the binary sentinel")
	assert.Contains(t, endpointSrc, `paginatedGet(cmd.Context(), c, path, map[string]string{`,
		"non-HasStore pagination must use paginatedGet")
	assert.NotContains(t, endpointSrc, `}, nil, flagAll,`,
		"paginated binary endpoint must pass headerOverrides, not nil")
	assert.Contains(t, endpointSrc, `}, headerOverrides, flagAll,`,
		"paginated binary endpoint must thread headerOverrides into paginatedGet")
}

// Regression: store-backed binary GET previously passed nil to resolveRead,
// so the live API call dispatched without the binary sentinel header.
func TestGenerateBinaryStoreBackedPromotedThreadsHeader(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"voices": {
			Description: "Voices",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         "GET",
					Path:           "/voices/{voice_id}",
					Description:    "Get voice sample",
					ResponseFormat: spec.ResponseFormatBinary,
					Params: []spec.Param{
						{Name: "voice_id", Type: "string", Required: true, Positional: true, PathParam: true, Description: "Voice ID"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_voices.go")
	assert.Contains(t, endpointSrc, `headerOverrides := map[string]string{`,
		"store-backed binary GET must declare headerOverrides")
	assert.Contains(t, endpointSrc, `"X-Printing-Press-Binary-Response": "true",`,
		"store-backed binary GET must include the binary sentinel")
	assert.Contains(t, endpointSrc, `resolveReadWithStrategy(cmd.Context(), c, flags, "auto", "voices", false, path, params, headerOverrides, cmd.ErrOrStderr())`,
		"store-backed binary GET must thread headerOverrides through the strategy-aware resolver")
}

func TestGenerateBinaryMCPToolsThreadHeaderOverrides(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"voices": {
			Description: "Voices",
			Endpoints: map[string]spec.Endpoint{
				"download": {
					Method:         "GET",
					Path:           "/voices/{voice_id}/download",
					Description:    "Download voice sample",
					ResponseFormat: spec.ResponseFormatBinary,
					HeaderOverrides: []spec.RequiredHeader{
						{Name: "Accept", Value: "application/octet-stream"},
					},
					Params: []spec.Param{
						{Name: "voice_id", Type: "string", Required: true, Positional: true, PathParam: true, Description: "Voice ID"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `makeAPIHandler("GET", "/voices/{voice_id}/download", true, true, map[string]string{"Accept": "application/octet-stream"},`,
		"typed MCP tools must carry per-endpoint header overrides")
	assert.Contains(t, mcpSrc, `headers[client.BinaryResponseHeader] = "true"`,
		"typed MCP tools must still add the binary sentinel")
	assert.Contains(t, mcpSrc, `data, err = c.GetWithHeaders(ctx, path, params, headers)`,
		"typed MCP tools must dispatch through WithHeaders when headers are present")
}
