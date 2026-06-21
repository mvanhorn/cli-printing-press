package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
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
	assert.Contains(t, clientSrc, `func (c *Client) PostMultipart(ctx context.Context, path string, fields map[string]string, fileFields map[string]string) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `writer.CreateFormFile(fieldName, filepath.Base(filePath))`)
	assert.Contains(t, clientSrc, `req.Header.Set("Content-Type", contentType)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "assets_upload.go")
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "asset-data")`)
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "filename")`)
	assert.Contains(t, endpointSrc, `fileFields["assetData"] = bodyAssetData`)
	assert.Contains(t, endpointSrc, `fields["filename"] = bodyFilename`)
	assert.Contains(t, endpointSrc, `fields["metadata"] = bodyMetadata`)
	assert.Contains(t, endpointSrc, `c.PostMultipartWithParams(cmd.Context(), path, params, fields, fileFields)`)
	assert.NotContains(t, endpointSrc, `"stdin"`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_avatars.go")
	assert.Contains(t, promotedSrc, `return fmt.Errorf("required flag \"%s\" not set", "image")`)
	assert.Contains(t, promotedSrc, `fileFields["image"] = bodyImage`)
	assert.Contains(t, promotedSrc, `fields["label"] = bodyLabel`)
	assert.Contains(t, promotedSrc, `c.PostMultipartWithParams(cmd.Context(), path, params, fields, fileFields)`)
	assert.NotContains(t, promotedSrc, `"stdin"`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `makeAPIHandler("POST", "/assets", false, false, nil, []mcpParamBinding`)
	assert.Contains(t, mcpSrc, `Format: "binary"`)
	assert.Contains(t, mcpSrc, `RequestContentType: "multipart/form-data"`)
	assert.Contains(t, mcpSrc, `multipartFileFields[binding.WireName] = fmt.Sprintf("%v", v)`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostMultipartWithParams(ctx, path, params, multipartFields, multipartFileFields)`)

	// An object/array body param binds to its native JSON type even on a
	// multipart endpoint (not WithString). The non-file field then flows
	// through mcpMultipartFieldValue, which JSON-encodes a native composite
	// rather than rendering it with Go's "%v". This locks the multipart path
	// that became live once array/object params stopped binding as WithString —
	// without it, a future refactor of the helper could silently reintroduce a
	// malformed multipart field for composite params.
	assert.Contains(t, mcpSrc, `mcplib.WithObject("metadata"`)
	assert.Contains(t, mcpSrc, `multipartFields[binding.WireName] = mcpMultipartFieldValue(v)`)
	assert.Regexp(t, `(?s)func mcpMultipartFieldValue\(v any\) string \{.*?json\.Marshal\(v\)`, mcpSrc)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateOpenAPIMultipartRequestBodyUsesMultipartClient(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Upload API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /assets:
    post:
      operationId: uploadAsset
      summary: Upload asset
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [assetData, filename]
              properties:
                assetData:
                  type: string
                  format: binary
                  description: Asset file
                filename:
                  type: string
                  description: File name
      responses:
        "201":
          description: created
  /notes:
    post:
      operationId: createNote
      summary: Create note
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [title]
              properties:
                title:
                  type: string
      responses:
        "201":
          description: created
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	uploadSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_assets.go")
	assert.Contains(t, uploadSrc, `return fmt.Errorf("required flag \"%s\" not set", "asset-data")`)
	assert.Contains(t, uploadSrc, `return fmt.Errorf("required flag \"%s\" not set", "filename")`)
	assert.Contains(t, uploadSrc, `fileFields["assetData"] = bodyAssetData`)
	assert.Contains(t, uploadSrc, `fields["filename"] = bodyFilename`)
	assert.Contains(t, uploadSrc, `c.PostMultipartWithParams(cmd.Context(), path, params, fields, fileFields)`)
	assert.NotContains(t, uploadSrc, `body["assetData"] = bodyAssetData`)
	assert.NotContains(t, uploadSrc, `body["filename"] = bodyFilename`)

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_notes.go")
	assert.Contains(t, createSrc, `body["title"] = bodyTitle`)
	assert.Contains(t, createSrc, `c.PostWithParams(cmd.Context(), path, params, body)`)
	assert.NotContains(t, createSrc, `c.PostMultipartWithParams`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateBinaryResponseWritesRawBytes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"audio": {
			Description: "Audio",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:         "POST",
					Path:           "/audio",
					Description:    "Create audio",
					ResponseFormat: spec.ResponseFormatBinary,
					Body: []spec.Param{
						{Name: "text", Type: "string", Required: true, Description: "Text"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_audio.go")
	assert.Contains(t, endpointSrc, `binary response cannot be rendered as structured output`)
	assert.Contains(t, endpointSrc, `cmd.OutOrStdout().Write(data)`)
}
