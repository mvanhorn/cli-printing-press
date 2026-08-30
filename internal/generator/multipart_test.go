package generator

import (
	"os"
	"path/filepath"
	"strings"
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
	assert.Contains(t, clientSrc, `mime.TypeByExtension(filepath.Ext(path))`)
	assert.Contains(t, clientSrc, `writer.CreatePart(h)`)
	assert.NotContains(t, clientSrc, `writer.CreateFormFile(`)
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
	assert.Contains(t, mcpSrc, `makeAPIHandler("POST", "/assets", false, false, nil, mcpPageConfig{}, []mcpParamBinding`)
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

	assertMultipartDryRunSkipsFileIO(t, clientSrc)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestMultipartDryRunDoesNotOpenFileFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multipart-dryrun")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"upload": {
					Method:             "POST",
					Path:               "/assets",
					Description:        "Upload an asset",
					RequestContentType: "multipart/form-data",
					Body: []spec.Param{
						{Name: "assetData", Type: "string", Format: "binary", Required: true, Description: "Asset file"},
						{Name: "filename", Type: "string", Required: true, Description: "File name"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assertMultipartDryRunSkipsFileIO(t, clientSrc)

	const runtimeTest = `package client

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"multipart-dryrun-pp-cli/internal/config"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = original }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing stderr pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading stderr pipe: %v", err)
	}
	return string(out)
}

func TestMultipartDryRunDoesNotOpenFileFields(t *testing.T) {
	c := New(&config.Config{BaseURL: "https://api.example.invalid"}, time.Second, 0)
	c.DryRun = true
	c.NoCache = true

	missing := filepath.Join(t.TempDir(), "does-not-exist.bin")
	stderr := captureStderr(t, func() {
		_, _, err := c.PostMultipartWithParams(context.Background(), "/assets", nil,
			map[string]string{"filename": "clip.wav"},
			map[string]string{"assetData": missing})
		if err != nil {
			t.Fatalf("dry-run opened or failed on missing file: %v", err)
		}
	})
	if !strings.Contains(stderr, "@"+missing) {
		t.Fatalf("dry-run preview did not name the file field; stderr=%s", stderr)
	}
	if !strings.Contains(stderr, "assetData") {
		t.Fatalf("dry-run preview omitted the file field name; stderr=%s", stderr)
	}
	if !strings.Contains(stderr, "clip.wav") {
		t.Fatalf("dry-run preview did not name the text field; stderr=%s", stderr)
	}
}

func TestMultipartLivePathStillOpensFileFields(t *testing.T) {
	c := New(&config.Config{BaseURL: "https://api.example.invalid"}, time.Second, 0)
	c.NoCache = true

	missing := filepath.Join(t.TempDir(), "does-not-exist.bin")
	_, _, err := c.PostMultipartWithParams(context.Background(), "/assets", nil,
		map[string]string{"filename": "clip.wav"},
		map[string]string{"assetData": missing})
	if err == nil {
		t.Fatal("live multipart path must still open FileFields")
	}
	if !strings.Contains(err.Error(), "opening multipart file field") {
		t.Fatalf("live error = %v, want opening multipart file field", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "multipart_dryrun_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestMultipart(DryRunDoesNotOpenFileFields|LivePathStillOpensFileFields)$", "-count=1")
}

func assertMultipartDryRunSkipsFileIO(t *testing.T, clientSrc string) {
	t.Helper()

	implStart := strings.Index(clientSrc, "func (c *Client) doInternal(")
	require.NotEqual(t, -1, implStart, "client.go must contain Client.doInternal")
	implBody := clientSrc[implStart:]
	if next := strings.Index(implBody[1:], "\nfunc "); next != -1 {
		implBody = implBody[:next+1]
	}
	dryRunIdx := strings.Index(implBody, "if c.DryRun {")
	encodeIdx := strings.Index(implBody, "b, ct, err := encodeMultipartBody(multipartBody)")
	require.GreaterOrEqual(t, dryRunIdx, 0, "doInternal must short-circuit multipart encoding under DryRun")
	require.GreaterOrEqual(t, encodeIdx, 0, "doInternal must still call encodeMultipartBody on the live path")
	assert.Less(t, dryRunIdx, encodeIdx, "doInternal must skip encodeMultipartBody (and its os.Open) under DryRun")
	assert.Contains(t, implBody, `preview[name] = "@" + filePath`)
	assert.Contains(t, clientSrc, "file, err := os.Open(filePath)")
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
  /attachments:
    post:
      operationId: uploadAttachments
      summary: Upload attachments
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: array
              items:
                type: object
                properties:
                  name:
                    type: string
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

	attachmentsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_attachments.go")
	assert.Contains(t, attachmentsSrc, `cmd.Flags().StringVar(&bodyFile, "file", "", "Path to the file to upload.")`)
	assert.Contains(t, attachmentsSrc, `return fmt.Errorf("required flag \"%s\" not set", "file")`)
	assert.Contains(t, attachmentsSrc, `fileFields["file"] = bodyFile`)
	assert.Contains(t, attachmentsSrc, `c.PostMultipartWithParams(cmd.Context(), path, params, fields, fileFields)`)
	assert.NotContains(t, attachmentsSrc, `"stdin"`)

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_notes.go")
	assert.Contains(t, createSrc, `bodyMap["title"] = bodyTitle`)
	assert.Contains(t, createSrc, `c.PostWithParams(cmd.Context(), path, params, body)`)
	assert.NotContains(t, createSrc, `c.PostMultipartWithParams`)

	requireGeneratedCompiles(t, outputDir)
}

func TestMultipartFilePartContentTypeFromExtension(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multipart-part-type")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"upload": {
					Method:             "POST",
					Path:               "/assets",
					Description:        "Upload an asset",
					RequestContentType: "multipart/form-data",
					Body: []spec.Param{
						{Name: "assetData", Type: "string", Format: "binary", Required: true, Description: "Asset file"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "cli", "assets_upload.go")
	assert.Contains(t, clientSrc, `fileFields["assetData"] = bodyAssetData`)
	assert.NotContains(t, clientSrc, `fields["assetData"]`)

	const runtimeTest = `package client

import (
	"bytes"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeMultipartFilePartContentType(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		ext  string
		want string
	}{
		{name: "pdf", ext: ".pdf", want: "application/pdf"},
		{name: "png", ext: ".png", want: "image/png"},
		{name: "jpeg", ext: ".jpeg", want: "image/jpeg"},
		{name: "unknown", ext: ".nope123", want: "application/octet-stream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "sample"+tc.ext)
			if err := os.WriteFile(path, []byte("sample"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if got := multipartFileContentType(path); got != tc.want {
				t.Fatalf("multipartFileContentType(%q) = %q, want %q", path, got, tc.want)
			}
			body, contentType, err := encodeMultipartBody(multipartRequestBody{
				FileFields: map[string]string{"assetData": path},
			})
			if err != nil {
				t.Fatalf("encodeMultipartBody() error = %v", err)
			}
			_, params, err := mime.ParseMediaType(contentType)
			if err != nil {
				t.Fatalf("ParseMediaType() error = %v", err)
			}
			part, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).NextPart()
			if err != nil {
				t.Fatalf("NextPart() error = %v", err)
			}
			got := part.Header.Get("Content-Type")
			if got != tc.want {
				t.Fatalf("part Content-Type = %q, want %q", got, tc.want)
			}
			if tc.want != "application/octet-stream" && strings.EqualFold(got, "application/octet-stream") {
				t.Fatalf("known extension %q still encoded as application/octet-stream", tc.ext)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "multipart_part_type_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestEncodeMultipartFilePartContentType$", "-count=1")
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
