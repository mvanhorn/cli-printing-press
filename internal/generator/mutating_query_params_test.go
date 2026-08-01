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

func TestGeneratedPostPreservesEmptyValuesAndHeaderParameters(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("emptywire")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"messages": {
			Description: "Manage messages",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/messages",
					Description: "List messages",
					Params:      []spec.Param{{Name: "offset", In: "query", Type: "int"}},
				},
				"create": {
					Method:      "POST",
					Path:        "/messages",
					Description: "Create a message",
					Params: []spec.Param{
						{Name: "mode", In: "query", Type: "string"},
						{Name: "X-Request-ID", In: "header", Type: "string"},
					},
					Body: []spec.Param{
						{Name: "text", Type: "string"},
						{Name: "offset", Type: "int"},
					},
				},
				"reset": {
					Method:      "PATCH",
					Path:        "/messages/{id}",
					Description: "Reset message credentials",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "X-Request-ID", In: "header", Type: "string"},
					},
					Body: []spec.Param{
						{Name: "currentPassword", Type: "string", Required: true},
						{Name: "password", Type: "string", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "messages_create.go")
	listSrc := readGeneratedFile(t, outputDir, "internal", "cli", "messages_list.go")
	assert.Contains(t, listSrc, `if flagOffset != "" {`, "GET query flags must retain their existing zero-value omission")
	assert.Contains(t, endpointSrc, `PostWithParamsAndHeaders(cmd.Context(), path, params, body, headerOverrides)`)
	assert.Contains(t, endpointSrc, `headerOverrides["X-Request-ID"]`)
	assert.Contains(t, endpointSrc, `cmd.Flags().Changed("mode")`)
	assert.Contains(t, endpointSrc, `cmd.Flags().Changed("text")`)
	assert.Contains(t, endpointSrc, `cmd.Flags().Changed("offset")`)
	assert.Contains(t, endpointSrc, `var bodyOffset int`)
	patchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "messages_reset.go")
	assert.Contains(t, patchSrc, `var bodyCurrentPassword string`)
	assert.Contains(t, patchSrc, `var bodyPassword string`)
	assert.Contains(t, patchSrc, `headerOverrides["X-Request-ID"]`)
	assert.Contains(t, patchSrc, `PatchWithParamsAndHeaders(cmd.Context(), path, params, body, headerOverrides)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `PublicName: "X-Request-ID", WireName: "X-Request-ID", Location: "header"`)
	assert.Contains(t, mcpSrc, "case \"header\":")

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPostPreservesExplicitEmptyAndZeroValues(t *testing.T) {
	var gotQuery url.Values
	var gotHeader string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotHeader = r.Header.Get("X-Request-ID")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()

	t.Setenv("EMPTYWIRE_BASE_URL", server.URL)
	flags := &rootFlags{asJSON: true}
	cmd := newMessagesCreateCmd(flags)
	cmd.SetArgs([]string{"--mode", "", "--text", "", "--offset", "0", "--x-request-id", "request-1"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if values, ok := gotQuery["mode"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("mode query = %#v, want one explicit empty value", gotQuery)
	}
	if gotHeader != "request-1" {
		t.Fatalf("X-Request-ID = %q, want request-1", gotHeader)
	}
	if value, ok := gotBody["text"]; !ok || value != "" {
		t.Fatalf("text body = %#v, want explicit empty string", gotBody)
	}
	if value, ok := gotBody["offset"]; !ok || value != float64(0) {
		t.Fatalf("offset body = %#v, want numeric zero", gotBody)
	}
	if _, ok := gotBody["mode"]; ok {
		t.Fatalf("query param leaked into body: %#v", gotBody)
	}
	if _, ok := gotBody["X-Request-ID"]; ok {
		t.Fatalf("header param leaked into body: %#v", gotBody)
	}
}
	`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "empty_wire_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPostPreservesExplicitEmptyAndZeroValues", "-count=1")

	mcpRuntimeTest := `package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestEndpointMCPPreservesHeaderAndEmptyValues(t *testing.T) {
	var gotQuery url.Values
	var gotHeader string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotHeader = r.Header.Get("X-Request-ID")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()

	t.Setenv("EMPTYWIRE_BASE_URL", server.URL)
	bindings := []mcpParamBinding{
		{PublicName: "mode", WireName: "mode", Location: "query"},
		{PublicName: "X-Request-ID", WireName: "X-Request-ID", Location: "header"},
		{PublicName: "text", WireName: "text", Location: "body"},
		{PublicName: "offset", WireName: "offset", Location: "body"},
	}
	handler := makeAPIHandler("POST", "/messages", false, false, nil, mcpPageConfig{}, bindings, nil)
	request := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"mode": "", "X-Request-ID": "request-1", "text": "", "offset": 0,
	}}}
	result, err := handler(context.Background(), request)
	if err != nil || result.IsError {
		t.Fatalf("MCP request failed: result=%#v err=%v", result, err)
	}
	if values, ok := gotQuery["mode"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("mode query = %#v, want one explicit empty value", gotQuery)
	}
	if gotHeader != "request-1" {
		t.Fatalf("X-Request-ID = %q, want request-1", gotHeader)
	}
	if value, ok := gotBody["text"]; !ok || value != "" {
		t.Fatalf("text body = %#v, want explicit empty string", gotBody)
	}
	if value, ok := gotBody["offset"]; !ok || value != float64(0) {
		t.Fatalf("offset body = %#v, want numeric zero", gotBody)
	}
	if _, ok := gotBody["mode"]; ok {
		t.Fatalf("query param leaked into body: %#v", gotBody)
	}
	if _, ok := gotBody["X-Request-ID"]; ok {
		t.Fatalf("header param leaked into body: %#v", gotBody)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "empty_wire_runtime_test.go"), []byte(mcpRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "TestEndpointMCPPreservesHeaderAndEmptyValues", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

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
						{Name: "output_format", In: "query", Type: "string", Description: "Output format"},
						{Name: "X-Request-ID", In: "header", Type: "string", Description: "Request identifier"},
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
	assert.Contains(t, clientSrc, `func (c *Client) PostWithParams(ctx context.Context, path string, params map[string]string, body any) (json.RawMessage, int, error)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_text-to-speech.go")
	assert.Contains(t, endpointSrc, `params := map[string]string{}`)
	assert.Contains(t, endpointSrc, `params["output_format"] = formatCLIParamValue(flagOutputFormat)`)
	assert.Contains(t, endpointSrc, `headerOverrides["X-Request-ID"] = formatCLIParamValue(flagXRequestID)`)
	assert.Contains(t, endpointSrc, `c.PostWithParamsAndHeaders(cmd.Context(), path, params, body, headerOverrides)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `PublicName: "output_format", WireName: "output_format", Location: "query"`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)`)
	assert.Contains(t, mcpSrc, `"content_encoding": "base64"`)
	assert.Contains(t, mcpSrc, `encoded := base64.StdEncoding.EncodeToString(data)`)
	assert.Contains(t, mcpSrc, `if len(out) > bound.MaxBytes {`)
	assert.Contains(t, mcpSrc, `binary response is too large for MCP text output`)
	assert.NotContains(t, mcpSrc, `bound.JSON(map[string]any{`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
