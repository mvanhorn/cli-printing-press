package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOpenAPIReadOnlyRequestFields(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(readOnlyRequestSpec))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.ModulePath = "example.com/read-only"
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	runtimeTest := strings.ReplaceAll(readOnlyRequestRuntimeTest, "__BASE_URL_ENV__", naming.EnvPrefix(apiSpec.Name)+"_BASE_URL")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "readonly_request_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestReadOnlyRequestInputs$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestEndpointHasJSONBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint spec.Endpoint
		want     bool
	}{
		{name: "mapped body", endpoint: spec.Endpoint{Body: []spec.Param{{Name: "name", Type: "string"}}}, want: true},
		{name: "fallback", endpoint: spec.Endpoint{BodyJSONFallback: true}, want: true},
		{name: "mapped multipart", endpoint: spec.Endpoint{RequestContentType: "multipart/form-data", Body: []spec.Param{{Name: "name", Type: "string"}}}},
		{name: "fallback form", endpoint: spec.Endpoint{RequestContentType: "application/x-www-form-urlencoded", BodyJSONFallback: true}},
		{name: "required JSON", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: "application/json"}, want: true},
		{name: "required text JSON", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: "text/json"}, want: true},
		{name: "required vendor JSON with parameters", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: " application/vnd.example+json; charset=utf-8 "}, want: true},
		{name: "required empty content type", endpoint: spec.Endpoint{BodyRequired: true}},
		{name: "optional JSON", endpoint: spec.Endpoint{RequestContentType: "application/json"}},
		{name: "required multipart with parameters", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: "multipart/form-data; boundary=example"}},
		{name: "required form with parameters", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: "application/x-www-form-urlencoded; charset=utf-8"}},
		{name: "required raw with parameters", endpoint: spec.Endpoint{BodyRequired: true, RequestContentType: "text/plain; charset=utf-8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, endpointHasJSONBody(tc.endpoint))
		})
	}
}

const readOnlyRequestSpec = `
openapi: 3.0.3
info:
  title: Read Only API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /records:
    get:
      responses: {'200': {description: OK}}
    post:
      operationId: createRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/JobPostRequestBody'
      responses: {'200': {description: OK}}
  /required-records:
    get:
      responses: {'200': {description: OK}}
    post:
      operationId: createRequiredRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [metadata]
              properties:
                metadata:
                  $ref: '#/components/schemas/ReadOnlyMetadata'
      responses: {'200': {description: OK}}
  /empty-records:
    get:
      responses: {'200': {description: OK}}
    post:
      operationId: createEmptyRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ReadOnlyMetadata'
      responses: {'200': {description: OK}}
    delete:
      operationId: deleteEmptyRecord
      parameters:
        - name: reason
          in: query
          schema: {type: string}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ReadOnlyMetadata'
      responses: {'200': {description: OK}}
  /empty-items:
    get:
      responses: {'200': {description: OK}}
    delete:
      operationId: deleteEmptyItem
      requestBody:
        required: true
        content:
          text/json:
            schema: {$ref: '#/components/schemas/ReadOnlyMetadata'}
      responses: {'200': {description: OK}}
  /empty-delete:
    delete:
      operationId: deleteEmpty
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/ReadOnlyMetadata'}
      responses: {'200': {description: OK}}
  /empty-direct-delete:
    delete:
      operationId: deleteEmptyDirect
      parameters:
        - name: reason
          in: query
          schema: {type: string}
      requestBody:
        required: true
        content:
          'application/vnd.example+json; charset=utf-8':
            schema:
              $ref: '#/components/schemas/ReadOnlyMetadata'
      responses: {'200': {description: OK}}
components:
  schemas:
    Job:
      type: object
      required: [kind, targetId, comment, parentJob, serverId]
      properties:
        id: {type: integer}
        kind: {type: string, enum: [UNDO]}
        targetId: {type: integer}
        comment: {type: string, readOnly: true}
        parentJob:
          readOnly: true
          allOf:
            - $ref: '#/components/schemas/Job'
        serverId: {allOf: [{type: integer, readOnly: true}]}
        serverNote: {allOf: [{type: string, readOnly: true}]}
        password: {type: string, writeOnly: true}
        visible: {type: string, readOnly: false}
    JobPostRequestBody:
      type: object
      required: [kind, targetId]
      allOf:
        - $ref: '#/components/schemas/Job'
      properties:
        job:
          $ref: '#/components/schemas/Job'
        metadata:
          $ref: '#/components/schemas/ReadOnlyMetadata'
    ReadOnlyMetadata:
      type: object
      required: [serverId]
      properties:
        serverId: {allOf: [{type: integer}, {readOnly: true}]}
`

const readOnlyRequestRuntimeTest = `package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"example.com/read-only/internal/cli"
)

func TestReadOnlyRequestInputs(t *testing.T) {
	type request struct {
		method, path, query string
		body []byte
		err error
	}
	requests := make(chan request, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- request{r.Method, r.URL.Path, r.URL.RawQuery, body, err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(upstream.Close)
	t.Setenv("__BASE_URL_ENV__", upstream.URL)

	s := server.NewMCPServer("read-only", "test")
	RegisterTools(s)
	tools := s.ListTools()
	create, ok := tools["records_create"]
	if !ok {
		t.Fatal("records_create tool missing")
	}
	command, _, err := cli.RootCmd().Find([]string{"records", "create"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"comment", "parentJob", "serverId", "serverNote", "job-comment", "job-parent-job", "job-server-id", "job-server-note", "metadata-server-id"} {
		if _, ok := create.Tool.InputSchema.Properties[name]; ok {
			t.Errorf("read-only MCP input %s was exposed", name)
		}
	}
	for _, name := range []string{"comment", "parent-job", "server-id", "server-note", "job-comment", "job-parent-job", "job-server-id", "job-server-note", "metadata-server-id"} {
		if command.Flags().Lookup(name) != nil {
			t.Errorf("read-only CLI flag %s was exposed", name)
		}
	}
	for _, name := range []string{"kind", "targetId", "password", "visible", "job-kind", "job-target-id", "job-password", "metadata"} {
		if _, ok := create.Tool.InputSchema.Properties[name]; !ok {
			t.Errorf("writable MCP input %s missing", name)
		}
	}
	if !reflect.DeepEqual(create.Tool.InputSchema.Required, []string{"kind", "targetId"}) {
		t.Fatalf("required inputs = %v", create.Tool.InputSchema.Required)
	}
	required, ok := tools["required-records_create"]
	if !ok || !reflect.DeepEqual(required.Tool.InputSchema.Required, []string{"metadata"}) {
		t.Fatalf("writable parent requirement missing: %#v", required)
	}

	for _, resource := range []string{"records", "required-records"} {
		cmd := cli.RootCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{resource, "create", "--json"})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%s accepted missing required inputs", resource)
		}
		if len(requests) != 0 {
			t.Fatal("invalid CLI request reached the server")
		}
	}

	cases := []struct {
		name, resource, tool, path, want string
		cliArgs []string
		mcpArgs map[string]any
	}{
		{
			name: "writable root and nested fields", resource: "records", tool: "records_create", path: "/records",
			cliArgs: []string{"--kind", "UNDO", "--target-id", "7", "--password", "secret", "--visible", "yes", "--job-kind", "UNDO", "--job-target-id", "8", "--job-password", "nested"},
			mcpArgs: map[string]any{"kind": "UNDO", "targetId": 7, "password": "secret", "visible": "yes", "job-kind": "UNDO", "job-target-id": 8, "job-password": "nested"},
			want: "{\"kind\":\"UNDO\",\"targetId\":7,\"password\":\"secret\",\"visible\":\"yes\",\"job\":{\"kind\":\"UNDO\",\"targetId\":8,\"password\":\"nested\"}}",
		},
		{
			name: "optional writable parent with read-only children", resource: "records", tool: "records_create", path: "/records",
			cliArgs: []string{"--kind", "UNDO", "--target-id", "7", "--metadata", "{}"},
			mcpArgs: map[string]any{"kind": "UNDO", "targetId": 7, "metadata": map[string]any{}},
			want: "{\"kind\":\"UNDO\",\"targetId\":7,\"metadata\":{}}",
		},
		{
			name: "required writable parent with read-only children", resource: "required-records", tool: "required-records_create", path: "/required-records",
			cliArgs: []string{"--metadata", "{}"}, mcpArgs: map[string]any{"metadata": map[string]any{}},
			want: "{\"metadata\":{}}",
		},
		{
			name: "required body with only read-only fields", resource: "empty-records", tool: "empty-records_create", path: "/empty-records",
			mcpArgs: map[string]any{}, want: "{}",
		},
	}
	for _, tc := range cases {
		for _, surface := range []string{"CLI", "MCP"} {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				if surface == "CLI" {
					cmd := cli.RootCmd()
					cmd.SetOut(&bytes.Buffer{})
					cmd.SetErr(&bytes.Buffer{})
					cmd.SetArgs(append([]string{tc.resource, "create", "--json"}, tc.cliArgs...))
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
				} else {
					tool, ok := tools[tc.tool]
					if !ok {
						t.Fatalf("missing tool %s", tc.tool)
					}
					result, err := tool.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: tc.mcpArgs}})
					if err != nil || result == nil || result.IsError {
						t.Fatalf("MCP result = %#v, error = %v", result, err)
					}
				}
				select {
				case got := <-requests:
					if got.err != nil || got.method != http.MethodPost || got.path != tc.path {
						t.Fatalf("request = %#v", got)
					}
					var actual, expected map[string]any
					if err := json.Unmarshal(got.body, &actual); err != nil {
						t.Fatalf("decode body %s: %v", got.body, err)
					}
					if err := json.NewDecoder(strings.NewReader(tc.want)).Decode(&expected); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("body = %s, want %s", got.body, tc.want)
					}
				default:
					t.Fatal("no HTTP request captured")
				}
			})
		}
	}
	for _, tc := range []struct {
		name, path, query string
		args []string
	}{
		{name: "endpoint", path: "/empty-items", args: []string{"empty-items", "delete", "--json"}},
		{name: "endpoint with query", path: "/empty-records", query: "reason=cleanup", args: []string{"empty-records", "delete", "--json", "--reason", "cleanup"}},
		{name: "promoted", path: "/empty-delete", args: []string{"empty-delete", "--json"}},
		{name: "promoted with query", path: "/empty-direct-delete", query: "reason=cleanup", args: []string{"empty-direct-delete", "--json", "--reason", "cleanup"}},
	} {
		t.Run("required DELETE/"+tc.name, func(t *testing.T) {
			cmd := cli.RootCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			executed, err := cmd.ExecuteC()
			if err != nil {
				t.Fatal(err)
			}
			if executed.Flags().Lookup("body-json") != nil {
				t.Fatal("required object body was exposed as a body-json flag")
			}
			select {
			case got := <-requests:
				if got.err != nil || got.method != http.MethodDelete || got.path != tc.path || got.query != tc.query {
					t.Fatalf("request = %#v", got)
				}
				if string(bytes.TrimSpace(got.body)) != "{}" {
					t.Fatalf("DELETE body = %q, want {}", got.body)
				}
			default:
				t.Fatal("no DELETE request captured")
			}
		})
	}
}
`
