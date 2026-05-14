package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMutatingEndpointPassesQueryParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"text-to-speech": {
			Description: "Text to speech",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:         "POST",
					Path:           "/text-to-speech/{voice_id}",
					Description:    "Create speech",
					ResponseFormat: spec.ResponseFormatBinary,
					Params: []spec.Param{
						{Name: "voice_id", Type: "string", Required: true, Positional: true, PathParam: true, Description: "Voice ID"},
						{Name: "output_format", Type: "string", Description: "Output format"},
					},
					Body: []spec.Param{
						{Name: "text", Type: "string", Required: true, Description: "Text"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `func (c *Client) PostWithParams(path string, params map[string]string, body any) (json.RawMessage, int, error)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_text-to-speech.go")
	assert.Contains(t, endpointSrc, `params := map[string]string{}`)
	assert.Contains(t, endpointSrc, `params["output_format"] = fmt.Sprintf("%v", flagOutputFormat)`)
	assert.Contains(t, endpointSrc, `c.PostWithParamsAndHeaders(path, params, body, headerOverrides)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `PublicName: "output_format", WireName: "output_format", Location: "query"`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostWithParamsAndHeaders(path, params, bodyArgs, headers)`)
	assert.Contains(t, mcpSrc, `"content_encoding": "base64"`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
