package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMultipartRequestBodyUsesMultipartClient(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("uploadapi")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/assets",
					Description: "List assets",
				},
				"upload": {
					Method:             "POST",
					Path:               "/assets",
					Description:        "Upload an asset",
					RequestContentType: "multipart/form-data",
					Body: []spec.Param{
						{Name: "assetData", Type: "string", Format: "binary", Required: true, Description: "Asset file"},
						{Name: "filename", Type: "string", Required: true, Description: "File name"},
						{Name: "metadata", Type: "object", Description: "Metadata as JSON"},
					},
				},
			},
		},
		"avatars": {
			Description: "Manage avatars",
			Endpoints: map[string]spec.Endpoint{
				"upload": {
					Method:             "POST",
					Path:               "/avatars",
					Description:        "Upload an avatar",
					RequestContentType: "multipart/form-data",
					Body: []spec.Param{
						{Name: "image", Type: "string", Format: "binary", Required: true, Description: "Avatar image"},
						{Name: "label", Type: "string", Description: "Label"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `func (c *Client) PostMultipart(path string, fields map[string]string, fileFields map[string]string) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `writer.CreateFormFile(fieldName, filepath.Base(filePath))`)
	assert.Contains(t, clientSrc, `req.Header.Set("Content-Type", contentType)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "assets_upload.go")
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "asset-data")`)
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "filename")`)
	assert.Contains(t, endpointSrc, `fileFields["assetData"] = bodyAssetData`)
	assert.Contains(t, endpointSrc, `fields["filename"] = bodyFilename`)
	assert.Contains(t, endpointSrc, `fields["metadata"] = bodyMetadata`)
	assert.Contains(t, endpointSrc, `c.PostMultipart(path, fields, fileFields)`)
	assert.NotContains(t, endpointSrc, `"stdin"`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_avatars.go")
	assert.Contains(t, promotedSrc, `return fmt.Errorf("required flag \"%s\" not set", "image")`)
	assert.Contains(t, promotedSrc, `fileFields["image"] = bodyImage`)
	assert.Contains(t, promotedSrc, `fields["label"] = bodyLabel`)
	assert.Contains(t, promotedSrc, `c.PostMultipart(path, fields, fileFields)`)
	assert.NotContains(t, promotedSrc, `"stdin"`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `makeAPIHandler("POST", "/assets", []mcpParamBinding`)
	assert.Contains(t, mcpSrc, `Format: "binary"`)
	assert.Contains(t, mcpSrc, `RequestContentType: "multipart/form-data"`)
	assert.Contains(t, mcpSrc, `multipartFileFields[binding.WireName] = fmt.Sprintf("%v", v)`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostMultipart(path, multipartFields, multipartFileFields)`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
