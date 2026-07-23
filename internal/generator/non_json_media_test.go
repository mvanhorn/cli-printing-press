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

func TestGeneratedCommandsHonorNonJSONRequestAndResponseMediaTypes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mediaapi")
	apiSpec.Resources = map[string]spec.Resource{
		"uploads": {
			Description: "Manage uploads",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/uploads",
					Description: "List uploads",
				},
				"create": {
					Method:             "POST",
					Path:               "/uploads",
					Description:        "Upload raw media",
					RequestContentType: "application/octet-stream",
					BodyRequired:       true,
					Params: []spec.Param{{
						Name:        "upload_id",
						Type:        "string",
						Required:    true,
						Description: "Upload ID",
					}},
				},
				"purge": {
					Method:             "DELETE",
					Path:               "/uploads/purge",
					Description:        "Delete by raw manifest",
					RequestContentType: "application/octet-stream",
					BodyRequired:       true,
				},
			},
		},
		"captions": {
			Description: "Fetch captions",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         "GET",
					Path:           "/captions",
					Description:    "Fetch captions",
					ResponseFormat: spec.ResponseFormatText,
					HeaderOverrides: []spec.RequiredHeader{{
						Name:  "Accept",
						Value: "text/plain",
					}},
				},
			},
		},
		"widgets": {
			Description: "Create widgets",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:             "POST",
					Path:               "/widgets",
					Description:        "Create a widget",
					RequestContentType: "application/json",
					BodyRequired:       true,
					Body: []spec.Param{{
						Name:        "name",
						Type:        "string",
						Required:    true,
						Description: "Widget name",
					}},
				},
			},
		},
	}
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	uploadSource := readGeneratedFile(t, outputDir, "internal", "cli", "uploads_create.go")
	assert.Contains(t, uploadSource, `cmd.Flags().StringVar(&rawBodyFile, "file"`)
	assert.Contains(t, uploadSource, `cmd.Flags().BoolVar(&stdinBody, "stdin"`)
	assert.Contains(t, uploadSource, `c.SendRaw(cmd.Context(), "POST", path, params, rawBody, rawContentType, nil)`)
	assert.NotContains(t, uploadSource, "parsing stdin JSON")

	purgeSource := readGeneratedFile(t, outputDir, "internal", "cli", "uploads_purge.go")
	assert.Contains(t, purgeSource, `c.SendRaw(cmd.Context(), "DELETE", path, params, rawBody, rawContentType, nil)`)
	assert.Contains(t, purgeSource, `cmd.Flags().BoolVar(&stdinBody, "stdin"`)

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSource, `mcplib.WithString("body_base64", mcplib.Required()`)
	assert.Contains(t, mcpSource, `c.SendRaw(ctx, method, path, params, rawBody, rawContentType, headers)`)
	assert.Contains(t, mcpSource, `client.TextResponseHeader: "true"`)

	codeOrchSpec := *apiSpec
	codeOrchSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	codeOrchOutputDir := filepath.Join(t.TempDir(), naming.CLI(codeOrchSpec.Name))
	require.NoError(t, New(&codeOrchSpec, codeOrchOutputDir).Generate())
	codeOrchSource := readGeneratedFile(t, codeOrchOutputDir, "internal", "mcp", "code_orch.go")
	assert.Contains(t, codeOrchSource, `RequestContentType: "application/octet-stream"`)
	assert.Contains(t, codeOrchSource, `rawBody, err = base64.StdEncoding.DecodeString(encoded)`)
	assert.Contains(t, codeOrchSource, `data, _, err = c.SendRaw(ctx, ep.Method, path, query, rawBody, rawContentType, hdrs)`)
	assert.Contains(t, codeOrchSource, `"request_body"] = map[string]any`)
	assert.Contains(t, codeOrchSource, `"default_content_type":   r.ep.RequestContentType`)
	assert.Contains(t, codeOrchSource, `"X-Printing-Press-Text-Response": "true"`)

	codeOrchRuntimeTest := `package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func codeOrchRequest(args map[string]any) mcplib.CallToolRequest {
	return mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: args}}
}

func codeOrchResultText(t *testing.T, result *mcplib.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("missing MCP result content")
	}
	text, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("MCP result content = %T, want TextContent", result.Content[0])
	}
	return text.Text
}

func TestCodeOrchestrationRawRequestRuntime(t *testing.T) {
	rawPayload := []byte{0x00, 0x01, 0xfe, 0xff, 'z'}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Equal(got, rawPayload) {
			t.Fatalf("raw body = %v, want %v", got, rawPayload)
		}
		if got := r.Header.Get("Content-Type"); got != "audio/wav" {
			t.Fatalf("Content-Type = %q, want audio/wav", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("MEDIAAPI_BASE_URL", server.URL)

	search, err := handleCodeOrchSearch(context.Background(), codeOrchRequest(map[string]any{"query": "raw media"}))
	if err != nil || search.IsError {
		t.Fatalf("search failed: result=%#v err=%v", search, err)
	}
	var searchEnvelope struct {
		Results []struct {
			EndpointID  string         ` + "`json:\"endpoint_id\"`" + `
			RequestBody map[string]any ` + "`json:\"request_body\"`" + `
		} ` + "`json:\"results\"`" + `
	}
	if err := json.Unmarshal([]byte(codeOrchResultText(t, search)), &searchEnvelope); err != nil {
		t.Fatalf("decode search result: %v", err)
	}
	foundInstructions := false
	for _, result := range searchEnvelope.Results {
		if result.EndpointID == "uploads.create" {
			foundInstructions = result.RequestBody["parameter"] == "body_base64" &&
				result.RequestBody["default_content_type"] == "application/octet-stream"
		}
	}
	if !foundInstructions {
		t.Fatalf("search omitted raw request instructions: %#v", searchEnvelope.Results)
	}

	encoded := base64.StdEncoding.EncodeToString(rawPayload)
	for _, endpointID := range []string{"uploads.create", "uploads.purge"} {
		result, err := handleCodeOrchExecute(context.Background(), codeOrchRequest(map[string]any{
			"endpoint_id": endpointID,
			"params": map[string]any{
				"body_base64": encoded,
				"content_type": "audio/wav",
				"upload_id": "asset-1",
			},
		}))
		if err != nil || result.IsError {
			t.Fatalf("%s failed: result=%#v err=%v", endpointID, result, err)
		}
	}
	if calls != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls)
	}

	for _, params := range []map[string]any{
		{},
		{"body_base64": "%%%"},
	} {
		result, err := handleCodeOrchExecute(context.Background(), codeOrchRequest(map[string]any{
			"endpoint_id": "uploads.create",
			"params": params,
		}))
		if err != nil || !result.IsError {
			t.Fatalf("invalid raw body result=%#v err=%v", result, err)
		}
	}
	if calls != 2 {
		t.Fatalf("invalid inputs reached HTTP server: calls = %d", calls)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(codeOrchOutputDir, "internal", "mcp", "non_json_media_runtime_test.go"), []byte(codeOrchRuntimeTest), 0o600))
	requireGeneratedCompiles(t, codeOrchOutputDir)
	runGoCommand(t, codeOrchOutputDir, "test", "./internal/mcp", "-run", "^TestCodeOrchestrationRawRequestRuntime$", "-count=1")

	runtimeTest := `package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func executeMediaCommand(t *testing.T, asJSON bool, args ...string) ([]byte, error) {
	t.Helper()
	flags := &rootFlags{}
	cmd := newRootCmd(flags)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if asJSON {
		args = append(args, "--json")
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.Bytes(), err
}

func TestRawUploadAndTextResponseRuntime(t *testing.T) {
	rawPayload := []byte{0x00, 0x01, 0x7f, 0xff, 'a'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads":
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
				return
			}
			if got := r.URL.Query().Get("upload_id"); got != "asset-1" {
				t.Fatalf("upload_id query = %q, want asset-1", got)
			}
			if got := r.Header.Get("Content-Type"); got != "audio/wav" {
				t.Fatalf("Content-Type = %q, want audio/wav", got)
			}
			got, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read upload: %v", err)
			}
			if !bytes.Equal(got, rawPayload) {
				t.Fatalf("raw body = %v, want %v", got, rawPayload)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"ok\":true}"))
		case "/uploads/purge":
			if r.Method != http.MethodDelete {
				t.Fatalf("purge method = %q, want DELETE", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Fatalf("purge Content-Type = %q, want application/octet-stream", got)
			}
			got, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read purge body: %v", err)
			}
			if !bytes.Equal(got, rawPayload) {
				t.Fatalf("purge raw body = %v, want %v", got, rawPayload)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"deleted\":true}"))
		case "/captions":
			if got := r.Header.Get("Accept"); got != "text/plain" {
				t.Fatalf("Accept = %q, want text/plain", got)
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"))
		case "/widgets":
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode widget JSON: %v", err)
			}
			if body["name"] != "plain-json" {
				t.Fatalf("widget body = %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":\"widget-1\"}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("MEDIAAPI_BASE_URL", server.URL)

	file := filepath.Join(t.TempDir(), "audio.bin")
	if err := os.WriteFile(file, rawPayload, 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}
	if _, err := executeMediaCommand(t, true, "uploads", "create", "--upload-id", "asset-1", "--file", file, "--content-type", "audio/wav"); err != nil {
		t.Fatalf("raw upload: %v", err)
	}
	if _, err := executeMediaCommand(t, true, "uploads", "purge", "--file", file); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	if _, err := executeMediaCommand(t, true, "uploads", "purge"); err == nil {
		t.Fatal("missing required raw body succeeded")
	}

	bare, err := executeMediaCommand(t, false, "captions")
	if err != nil {
		t.Fatalf("bare captions: %v", err)
	}
	wantText := "1\n00:00:00,000 --> 00:00:01,000\nHello\n"
	if string(bare) != wantText {
		t.Fatalf("bare captions = %q, want %q", bare, wantText)
	}

	structured, err := executeMediaCommand(t, true, "captions")
	if err != nil {
		t.Fatalf("JSON captions: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(structured, &envelope); err != nil {
		t.Fatalf("JSON captions emitted invalid JSON: %v\n%s", err, structured)
	}
	if envelope["format"] != "text/plain" || envelope["body"] != wantText {
		t.Fatalf("text envelope = %#v", envelope)
	}
	if _, err := executeMediaCommand(t, false, "captions", "--data-source", "local"); err == nil {
		t.Fatal("text response unexpectedly used the local JSON data source")
	}

	if _, err := executeMediaCommand(t, true, "widgets", "--name", "plain-json"); err != nil {
		t.Fatalf("JSON counter-case: %v", err)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "non_json_media_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(runtimeTest), 0o600))

	mcpRuntimeTest := `package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func rawMCPRequest(args map[string]any) mcplib.CallToolRequest {
	return mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: args}}
}

func TestEndpointMCPRawRequestRuntime(t *testing.T) {
	rawPayload := []byte{0x00, 0x01, 0xfe, 0xff, 'z'}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Equal(got, rawPayload) {
			t.Fatalf("raw body = %v, want %v", got, rawPayload)
		}
		if got := r.Header.Get("Content-Type"); got != "audio/wav" {
			t.Fatalf("Content-Type = %q, want audio/wav", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("MEDIAAPI_BASE_URL", server.URL)

	bindings := []mcpParamBinding{
		{Location: "raw", RequestContentType: "application/octet-stream", RawBodyRequired: true},
		{PublicName: "upload_id", WireName: "upload_id", Location: "query"},
	}
	encoded := base64.StdEncoding.EncodeToString(rawPayload)
	for _, method := range []string{"POST", "DELETE"} {
		handler := makeAPIHandler(method, "/uploads", false, false, nil, mcpPageConfig{}, bindings, nil)
		result, err := handler(context.Background(), rawMCPRequest(map[string]any{
			"body_base64": encoded,
			"content_type": "audio/wav",
			"upload_id": "asset-1",
		}))
		if err != nil || result.IsError {
			t.Fatalf("%s failed: result=%#v err=%v", method, result, err)
		}
	}
	if calls != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls)
	}

	handler := makeAPIHandler("POST", "/uploads", false, false, nil, mcpPageConfig{}, bindings, nil)
	for _, args := range []map[string]any{
		{},
		{"body_base64": "%%%"},
	} {
		result, err := handler(context.Background(), rawMCPRequest(args))
		if err != nil || !result.IsError {
			t.Fatalf("invalid raw body result=%#v err=%v", result, err)
		}
	}
	if calls != 2 {
		t.Fatalf("invalid inputs reached HTTP server: calls = %d", calls)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "non_json_media_runtime_test.go"), []byte(mcpRuntimeTest), 0o600))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestRawUploadAndTextResponseRuntime$", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestEndpointMCPRawRequestRuntime$", "-count=1")
}
