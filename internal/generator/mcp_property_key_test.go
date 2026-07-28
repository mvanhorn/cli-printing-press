package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBracketParamPublicSurfacesAreSanitized parses an OpenAPI doc with a
// vendor bracket param (fathom-style recorded_by[]), generates, and asserts
// every model-facing surface exposes the sanitized name while the wire name
// keeps the brackets. Companion to the sanitizer unit tests in internal/spec.
func TestBracketParamPublicSurfacesAreSanitized(t *testing.T) {
	t.Parallel()

	const doc = `
openapi: 3.0.0
info:
  title: Bracket API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /meetings:
    get:
      operationId: listMeetings
      tags: [meetings]
      parameters:
        - name: "recorded_by[]"
          in: query
          style: form
          explode: true
          schema:
            type: array
            items:
              type: string
        - name: "date[start]"
          in: query
          schema:
            type: string
        - name: "date[stop]"
          in: query
          schema:
            type: string
        - name: cursor
          in: query
          schema:
            type: string
      responses:
        '200':
          description: ok
`
	apiSpec, err := openapi.Parse([]byte(doc))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "bracket-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	// Schema key + binding: public sanitized, wire raw.
	require.Contains(t, toolsSrc, `"recorded_by"`)
	require.Contains(t, toolsSrc, `PublicName: "recorded_by", WireName: "recorded_by[]"`)
	require.Contains(t, toolsSrc, `PublicName: "date_start", WireName: "date[start]"`)
	require.Contains(t, toolsSrc, `PublicName: "date_stop", WireName: "date[stop]"`)
	// The illegal forms must appear ONLY as WireName values, never as
	// schema property keys: no With* declaration may carry them.
	require.NotRegexp(t, regexp.MustCompile(`mcplib\.With\w+\("recorded_by\[\]"`), toolsSrc)
	require.NotRegexp(t, regexp.MustCompile(`mcplib\.With\w+\("date\[`), toolsSrc)
	// Tool description param list teaches the sanitized names — positive
	// half (the description mentions recorded_by) AND negative half (never
	// the bracket form).
	assert.Regexp(t, regexp.MustCompile(`WithDescription\([^)]*recorded_by`), toolsSrc)
	assert.NotRegexp(t, regexp.MustCompile(`WithDescription\([^)]*recorded_by\[\]`), toolsSrc)

	// Corpus-style guard (the upstream analog of fathom's
	// TestRegisterTools_PropertyKeysAreAnthropicSafe): EVERY quoted first
	// argument of a mcplib.With* schema declaration in tools.go must match
	// the Anthropic grammar.
	withKeyRe := regexp.MustCompile(`mcplib\.With(?:String|Number|Boolean|Array|Object)\("([^"]+)"`)
	for _, m := range withKeyRe.FindAllStringSubmatch(toolsSrc, -1) {
		assert.Regexp(t, spec.MCPPropertyKeyRe, m[1], "illegal MCP schema key emitted: %q", m[1])
	}

	requireGeneratedCompiles(t, outputDir)
}

// TestBracketParamWireNameIsPreserved proves sanitization never touches the
// wire: the generated CLI still sends the literal bracket key repeated, and
// the REAL registered MCP tool handler — driven with the SANITIZED argument
// name — emits the same bracket wire key on its HTTP request.
func TestBracketParamWireNameIsPreserved(t *testing.T) {
	t.Parallel()

	requests := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()["recorded_by[]"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("bracket-wire")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"meetings": {
			Description: "Meetings",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/meetings",
					Description: "List meetings",
					Params: []spec.Param{
						{Name: "recorded_by[]", Type: "array", ItemType: "string", Description: "Recorder emails"},
					},
					Response: spec.ResponseDef{Type: "array", Item: "Meeting"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Meeting": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "bracket-wire-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "bracket-wire-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/bracket-wire-pp-cli")
	runGeneratedBinary(t, binaryPath, "meetings", "get", "--recorded-by", "a@x.com,b@x.com")
	require.Equal(t, []string{"a@x.com", "b@x.com"}, <-requests)

	// Registered-handler wire proof: write a Go test into the generated
	// module (the array_query_param_test.go in-module idiom) that drives the
	// makeAPIHandler-registered meetings_get tool with the sanitized arg name
	// and asserts the bracket wire key reaches the server. The generated
	// module's baked BaseURL is THIS test's httptest server, so the in-module
	// test redirects to its own server via the generated config's
	// <PREFIX>_BASE_URL env hook. Pin the exact env names against the
	// generated config source before relying on them.
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `"BRACKET_WIRE_BASE_URL"`,
		"generated config must expose the BASE_URL env override the in-module test relies on")
	require.Contains(t, configSrc, "MYAPI_TOKEN",
		"generated config must reference the spec's credential env var")

	const handlerWireTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// TestBracketHandlerWire drives the REAL registered MCP tool handler with the
// SANITIZED argument name and proves the literal bracket wire key goes out on
// the HTTP request — sanitization is public-surface only.
func TestBracketHandlerWire(t *testing.T) {
	requests := make(chan []string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()["recorded_by[]"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("BRACKET_WIRE_BASE_URL", srv.URL)
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("HOME", t.TempDir())

	s := server.NewMCPServer("test", "0.0.0")
	RegisterTools(s)
	tool := s.ListTools()["meetings_get"]
	require.NotNil(t, tool, "meetings_get tool not registered")

	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "meetings_get",
		Arguments: map[string]any{"recorded_by": []any{"a@x.com"}},
	}}
	result, err := tool.Handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "tool handler returned error result: %+v", result)
	require.Equal(t, []string{"a@x.com"}, <-requests)
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "bracket_handler_wire_test.go"),
		[]byte(handlerWireTest), 0o600))
	// go test exits 0 when -run matches nothing, so assert the PASS line to
	// prove the in-module handler test actually executed. (Short mode never
	// reaches this point: the build runGoCommand above already skips.)
	output, err := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/mcp", "-run", "^TestBracketHandlerWire$")
	require.NoError(t, err, output)
	require.Contains(t, output, "--- PASS: TestBracketHandlerWire",
		"in-module registered-handler wire test must actually run and pass:\n%s", output)
}
