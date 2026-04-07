package megamcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManifest returns a basic manifest for testing.
func newTestManifest(baseURL string) *ToolsManifest {
	return &ToolsManifest{
		APIName: "testapi",
		BaseURL: baseURL,
		Auth: ManifestAuth{
			Type: "none",
		},
	}
}

// newTestManifestWithAuth returns a manifest with API key auth.
func newTestManifestWithAuth(baseURL, authType string, envVars []string) *ToolsManifest {
	m := newTestManifest(baseURL)
	m.Auth = ManifestAuth{
		Type:    authType,
		Header:  "X-Api-Key",
		EnvVars: envVars,
	}
	return m
}

// makeToolRequest creates an MCP CallToolRequest with the given arguments.
func makeToolRequest(args map[string]any) mcp.CallToolRequest {
	// Arguments is typed as `any` in CallToolParams, but GetArguments()
	// casts it to map[string]any. We pass args as map[string]any.
	var arguments any
	if args != nil {
		arguments = map[string]any(args)
	}
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: arguments,
		},
	}
}

func TestMakeToolHandler_GetWithPathParam(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(200)
		w.Write([]byte(`{"id": 42}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "users_get",
		Method: "GET",
		Path:   "/users/{user_id}",
		Params: []ManifestParam{
			{Name: "user_id", Type: "string", Location: "path", Required: true},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"user_id": "123",
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "/users/123", capturedPath)
}

func TestMakeToolHandler_GetWithQueryParams(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(200)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "users_list",
		Method: "GET",
		Path:   "/users",
		Params: []ManifestParam{
			{Name: "page", Type: "integer", Location: "query"},
			{Name: "limit", Type: "integer", Location: "query"},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"page":  float64(2),
		"limit": float64(10),
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, capturedQuery, "page=2")
	assert.Contains(t, capturedQuery, "limit=10")
}

func TestMakeToolHandler_PostWithBodyParams(t *testing.T) {
	var capturedBody string
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(201)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "users_create",
		Method: "POST",
		Path:   "/orgs/{org_id}/users",
		Params: []ManifestParam{
			{Name: "org_id", Type: "string", Location: "path", Required: true},
			{Name: "name", Type: "string", Location: "body", Required: true},
			{Name: "email", Type: "string", Location: "body", Required: true},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"org_id": "acme",
		"name":   "Alice",
		"email":  "alice@example.com",
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "/orgs/acme/users", capturedPath)

	// Verify body contains name and email but NOT org_id (path param).
	var bodyMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedBody), &bodyMap))
	assert.Equal(t, "Alice", bodyMap["name"])
	assert.Equal(t, "alice@example.com", bodyMap["email"])
	_, hasOrgID := bodyMap["org_id"]
	assert.False(t, hasOrgID, "path param org_id should not appear in body")
}

func TestMakeToolHandler_ApiKeyAuthInHeader(t *testing.T) {
	var capturedAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("X-Api-Key")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "sk-test-12345")
	manifest := newTestManifestWithAuth(srv.URL, "api_key", []string{"TEST_API_KEY"})

	tool := ManifestTool{
		Name:   "status_get",
		Method: "GET",
		Path:   "/status",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "sk-test-12345", capturedAuthHeader)
}

func TestMakeToolHandler_ApiKeyAuthInQuery(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(200)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	t.Setenv("ESPN_KEY", "espn-abc")
	manifest := &ToolsManifest{
		APIName: "espn",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "api_key",
			Header:  "apikey",
			In:      "query",
			EnvVars: []string{"ESPN_KEY"},
		},
	}

	tool := ManifestTool{
		Name:   "scores_get",
		Method: "GET",
		Path:   "/scores",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, capturedQuery, "apikey=espn-abc")
}

func TestMakeToolHandler_BearerTokenAuth(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("DUB_TOKEN", "dub-tok-xyz")
	manifest := &ToolsManifest{
		APIName: "dub",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {DUB_TOKEN}",
			EnvVars: []string{"DUB_TOKEN"},
		},
	}

	tool := ManifestTool{
		Name:   "links_list",
		Method: "GET",
		Path:   "/links",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "Bearer dub-tok-xyz", capturedAuth)
}

func TestMakeToolHandler_RequiredHeaders(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	manifest.RequiredHeaders = []ManifestHeader{
		{Name: "Accept", Value: "application/json"},
		{Name: "X-Custom", Value: "custom-value"},
	}

	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "application/json", capturedHeaders.Get("Accept"))
	assert.Equal(t, "custom-value", capturedHeaders.Get("X-Custom"))
}

func TestMakeToolHandler_PerToolHeaderOverrides(t *testing.T) {
	var capturedVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedVersion = r.Header.Get("X-API-Version")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	manifest.RequiredHeaders = []ManifestHeader{
		{Name: "X-API-Version", Value: "2023-01-01"},
	}

	tool := ManifestTool{
		Name:   "v2_data_get",
		Method: "GET",
		Path:   "/v2/data",
		HeaderOverrides: []ManifestHeader{
			{Name: "X-API-Version", Value: "2024-06-01"},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Per-tool override should win over required headers.
	assert.Equal(t, "2024-06-01", capturedVersion)
}

func TestMakeToolHandler_NoAuth(t *testing.T) {
	var capturedAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)

	tool := ManifestTool{
		Name:   "public_get",
		Method: "GET",
		Path:   "/public",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Empty(t, capturedAuthHeader)
}

func TestMakeToolHandler_MissingEnvVarNonNoAuth(t *testing.T) {
	// Don't start a server — request should never be made.
	manifest := &ToolsManifest{
		APIName: "myapi",
		BaseURL: "https://api.example.com",
		Auth: ManifestAuth{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"MISSING_ENV_VAR"},
		},
	}

	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, &http.Client{Timeout: time.Second})
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Authentication not configured")
	assertResultContains(t, result, "setup_guide")
}

func TestMakeToolHandler_MissingEnvVarNoAuthEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"public": true}`))
	}))
	defer srv.Close()

	manifest := &ToolsManifest{
		APIName: "myapi",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"MISSING_ENV_VAR"},
		},
	}

	tool := ManifestTool{
		Name:   "public_get",
		Method: "GET",
		Path:   "/public",
		NoAuth: true, // This endpoint doesn't need auth.
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError, "NoAuth endpoint should succeed without env var")
}

func TestMakeToolHandler_401Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error": "Invalid token: sk-real-key-123"}`))
	}))
	defer srv.Close()

	t.Setenv("MY_TOKEN", "sk-real-key-123")
	manifest := &ToolsManifest{
		APIName: "myapi",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"MY_TOKEN"},
		},
	}

	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Authentication error")
	assertResultContains(t, result, "setup_guide")
	// Credential should be redacted.
	assertResultNotContains(t, result, "sk-real-key-123")
	assertResultContains(t, result, "[REDACTED]")
}

func TestMakeToolHandler_429Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error": "rate limit exceeded", "retry_after": 30}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Rate limited")
	assertResultContains(t, result, "rate limit exceeded")
}

func TestMakeToolHandler_LargeResponseTruncated(t *testing.T) {
	// Create a response larger than 10MB.
	largeBody := strings.Repeat("x", maxResponseBody+1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(largeBody))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	// The response should be truncated at maxResponseBody.
	text := extractResultText(result)
	assert.LessOrEqual(t, len(text), maxResponseBody)
}

func TestMakeToolHandler_MissingRequiredPathParam(t *testing.T) {
	manifest := &ToolsManifest{
		APIName: "myapi",
		BaseURL: "https://api.example.com",
		Auth:    ManifestAuth{Type: "none"},
	}

	tool := ManifestTool{
		Name:   "users_get",
		Method: "GET",
		Path:   "/users/{user_id}/posts/{post_id}",
		Params: []ManifestParam{
			{Name: "user_id", Type: "string", Location: "path", Required: true},
			{Name: "post_id", Type: "string", Location: "path", Required: true},
		},
	}

	handler := MakeToolHandler(manifest, tool, &http.Client{Timeout: time.Second})
	// Only provide user_id, not post_id.
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"user_id": "123",
	}))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Missing required path parameter")
	assertResultContains(t, result, "post_id")
}

func TestMakeToolHandler_PathParamWithTraversal(t *testing.T) {
	var capturedRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawPath = r.URL.RawPath
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "files_get",
		Method: "GET",
		Path:   "/files/{file_id}",
		Params: []ManifestParam{
			{Name: "file_id", Type: "string", Location: "path", Required: true},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"file_id": "../../../etc/passwd",
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError, "URL-encoded path traversal should not cause host mismatch")
	// The ../ should be URL-encoded in the raw path (Go's HTTP server decodes
	// %2F back to / in r.URL.Path, but RawPath preserves the encoding).
	assert.Contains(t, capturedRawPath, "%2F")
}

func TestMakeToolHandler_500Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_get",
		Method: "GET",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Server error (HTTP 500)")
}

func TestMakeToolHandler_IntegrationRoundTrip(t *testing.T) {
	// Full round-trip test: GET with path+query params, auth header, required headers.
	var capturedReq struct {
		Method  string
		Path    string
		Query   string
		Headers http.Header
		Body    string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq.Method = r.Method
		capturedReq.Path = r.URL.Path
		capturedReq.Query = r.URL.RawQuery
		capturedReq.Headers = r.Header
		body, _ := io.ReadAll(r.Body)
		capturedReq.Body = string(body)
		w.WriteHeader(200)
		w.Write([]byte(`{"result": "success"}`))
	}))
	defer srv.Close()

	t.Setenv("DUB_TOKEN", "tok-abc")
	manifest := &ToolsManifest{
		APIName: "dub",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {DUB_TOKEN}",
			EnvVars: []string{"DUB_TOKEN"},
		},
		RequiredHeaders: []ManifestHeader{
			{Name: "Accept", Value: "application/json"},
		},
	}

	tool := ManifestTool{
		Name:   "links_update",
		Method: "PATCH",
		Path:   "/links/{link_id}",
		Params: []ManifestParam{
			{Name: "link_id", Type: "string", Location: "path", Required: true},
			{Name: "url", Type: "string", Location: "body"},
			{Name: "title", Type: "string", Location: "body"},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"link_id": "lnk_42",
		"url":     "https://example.com",
		"title":   "Example",
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Verify request details.
	assert.Equal(t, "PATCH", capturedReq.Method)
	assert.Equal(t, "/links/lnk_42", capturedReq.Path)
	assert.Equal(t, "Bearer tok-abc", capturedReq.Headers.Get("Authorization"))
	assert.Equal(t, "application/json", capturedReq.Headers.Get("Accept"))
	assert.Equal(t, "application/json", capturedReq.Headers.Get("Content-Type"))

	// Verify body: url and title present, link_id absent.
	var bodyMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.Body), &bodyMap))
	assert.Equal(t, "https://example.com", bodyMap["url"])
	assert.Equal(t, "Example", bodyMap["title"])
	_, hasLinkID := bodyMap["link_id"]
	assert.False(t, hasLinkID, "path param link_id should not be in body")
}

func TestMakeToolHandler_403Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error": "forbidden"}`))
	}))
	defer srv.Close()

	t.Setenv("MY_KEY", "secret-key-value")
	manifest := &ToolsManifest{
		APIName: "myapi",
		BaseURL: srv.URL,
		Auth: ManifestAuth{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"MY_KEY"},
		},
	}

	tool := ManifestTool{
		Name:   "admin_get",
		Method: "GET",
		Path:   "/admin",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "Authentication error")
	assertResultContains(t, result, "setup_guide")
	// Env var name should not be in the error.
	assertResultNotContains(t, result, "MY_KEY")
}

func TestMakeToolHandler_4xxResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"error": "validation failed"}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_create",
		Method: "POST",
		Path:   "/data",
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(nil))

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "API error (HTTP 422)")
	assertResultContains(t, result, "validation failed")
}

func TestMakeToolHandler_ContentTypeForPOST(t *testing.T) {
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_create",
		Method: "POST",
		Path:   "/data",
		Params: []ManifestParam{
			{Name: "name", Type: "string", Location: "body"},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	_, err := handler(t.Context(), makeToolRequest(map[string]any{
		"name": "test",
	}))

	require.NoError(t, err)
	assert.Equal(t, "application/json", capturedContentType)
}

func TestMakeToolHandler_DeleteMethod(t *testing.T) {
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(204)
	}))
	defer srv.Close()

	manifest := newTestManifest(srv.URL)
	tool := ManifestTool{
		Name:   "data_delete",
		Method: "DELETE",
		Path:   "/data/{id}",
		Params: []ManifestParam{
			{Name: "id", Type: "string", Location: "path", Required: true},
		},
	}

	handler := MakeToolHandler(manifest, tool, srv.Client())
	result, err := handler(t.Context(), makeToolRequest(map[string]any{
		"id": "42",
	}))

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "DELETE", capturedMethod)
}

// --- Helper functions ---

// extractResultText extracts the text content from an MCP CallToolResult.
func extractResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// assertResultContains checks that the result text contains the expected substring.
func assertResultContains(t *testing.T, result *mcp.CallToolResult, expected string) {
	t.Helper()
	text := extractResultText(result)
	assert.Contains(t, text, expected, "expected result to contain %q, got %q", expected, text)
}

// assertResultNotContains checks that the result text does NOT contain the given substring.
func assertResultNotContains(t *testing.T, result *mcp.CallToolResult, unexpected string) {
	t.Helper()
	text := extractResultText(result)
	assert.NotContains(t, text, unexpected, "expected result NOT to contain %q, got %q", unexpected, text)
}
