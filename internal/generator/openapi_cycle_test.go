package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOpenAPIWritableRequestBodyCycles(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(writableCycleRequestSpec))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.ModulePath = "example.com/writable-cycle"
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")
	for _, tc := range []struct {
		file    string
		present []string
		absent  []string
	}{
		{
			file:    "records_create.go",
			present: []string{`"parent-job"`, `"kind"`},
			absent:  []string{`"parent-job-id"`, `"parent-job-kind"`, `"parent-job-parent-job"`},
		},
		{
			file:    "composed-records_create.go",
			present: []string{`"parent-job"`},
			absent:  []string{`"parent-job-id"`, `"parent-job-parent-job"`, `"parent-job-parent-job-parent-job"`},
		},
		{
			file:    "moves_create.go",
			present: []string{`"job-id"`, `"job-kind"`, `"job-parent-job"`, `"destination"`},
			absent:  []string{`"job-parent-job-id"`, `"job-parent-job-kind"`, `"job-parent-job-parent-job"`},
		},
		{
			file:    "direct-moves_create.go",
			present: []string{`"job-parent-job"`},
			absent:  []string{`"job-parent-job-id"`, `"job-parent-job-parent-job"`},
		},
		{
			file:    "nodes_create.go",
			present: []string{`"other-label"`, `"other-node"`, `"name"`},
			absent:  []string{`"other-node-name"`, `"other-node-label"`, `"other-node-other"`},
		},
		{
			file:    "long-records_create.go",
			present: []string{`"parent-background-job"`},
			absent:  []string{`"parent-background-job-kind"`, `"parent-background-job-parent-background-job"`},
		},
	} {
		body := string(mustRead(t, filepath.Join(cliDir, tc.file)))
		for _, needle := range tc.present {
			require.Contains(t, body, needle, tc.file)
		}
		for _, needle := range tc.absent {
			require.NotContains(t, body, needle, tc.file)
		}
	}

	tools := string(mustRead(t, filepath.Join(outputDir, "internal", "mcp", "tools.go")))
	for _, needle := range []string{
		`"parentJob"`,
		`"job-parent-job"`,
		`"other-node"`,
		`"parentBackgroundJob"`,
	} {
		require.Contains(t, tools, needle)
	}
	for _, needle := range []string{
		`"parent-job-parent-job"`,
		`"parent-job-id"`,
		`"job-parent-job-parent-job"`,
		`"job-parent-job-id"`,
		`"other-node-other"`,
		`"other-node-name"`,
		`"parent-background-job-parent-background-job"`,
		`"parent-background-job-kind"`,
		"parent-background-job-parent-background-job-parent-background-job",
	} {
		require.NotContains(t, tools, needle)
	}

	runtimeTest := strings.ReplaceAll(writableCycleRuntimeTest, "__BASE_URL_ENV__", naming.EnvPrefix(apiSpec.Name)+"_BASE_URL")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "writable_cycle_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestWritableCycleRequestInputs$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err, path)
	return body
}

const writableCycleRequestSpec = `
openapi: 3.0.3
info:
  title: Writable Cycle API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /records:
    get:
      operationId: listRecords
      responses:
        '200': {description: ok}
    post:
      operationId: createRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Job'
      responses:
        '200': {description: ok}
  /composed-records:
    get:
      operationId: listComposedRecords
      responses:
        '200': {description: ok}
    post:
      operationId: createComposedRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/JobPostRequestBody'
      responses:
        '200': {description: ok}
  /moves:
    get:
      operationId: listMoves
      responses:
        '200': {description: ok}
    post:
      operationId: createMove
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/MoveRequest'
      responses:
        '200': {description: ok}
  /direct-moves:
    get:
      operationId: listDirectMoves
      responses:
        '200': {description: ok}
    post:
      operationId: createDirectMove
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/DirectMoveRequest'
      responses:
        '200': {description: ok}
  /nodes:
    get:
      operationId: listNodes
      responses:
        '200': {description: ok}
    post:
      operationId: createNode
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Node'
      responses:
        '200': {description: ok}
  /long-records:
    get:
      operationId: listLongRecords
      responses:
        '200': {description: ok}
    post:
      operationId: createLongRecord
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/LongJob'
      responses:
        '200': {description: ok}
components:
  schemas:
    Job:
      type: object
      required: [kind]
      properties:
        id: {type: integer}
        kind: {type: string}
        parentJob:
          $ref: '#/components/schemas/Job'
    JobAllOf:
      type: object
      required: [kind]
      properties:
        id: {type: integer}
        kind: {type: string}
        parentJob:
          allOf:
            - $ref: '#/components/schemas/JobAllOf'
    JobPostRequestBody:
      required: [kind]
      allOf:
        - $ref: '#/components/schemas/JobAllOf'
    MoveRequest:
      type: object
      required: [destination]
      properties:
        destination: {type: string}
        job:
          allOf:
            - $ref: '#/components/schemas/JobAllOf'
    DirectMoveRequest:
      type: object
      required: [destination]
      properties:
        destination: {type: string}
        job:
          $ref: '#/components/schemas/Job'
    Node:
      type: object
      required: [name]
      properties:
        name: {type: string}
        other:
          allOf:
            - $ref: '#/components/schemas/Other'
    Other:
      type: object
      properties:
        label: {type: string}
        node:
          allOf:
            - $ref: '#/components/schemas/Node'
    LongJob:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
        parentBackgroundJob:
          allOf:
            - $ref: '#/components/schemas/LongJob'
`

const writableCycleRuntimeTest = `package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"example.com/writable-cycle/internal/cli"
)

func TestWritableCycleRequestInputs(t *testing.T) {
	requests := make(chan []byte, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(upstream.Close)
	t.Setenv("__BASE_URL_ENV__", upstream.URL)

	s := server.NewMCPServer("writable-cycle", "test")
	RegisterTools(s)
	tools := s.ListTools()

	type surface struct {
		command []string
		tool    string
		cliHas  []string
		cliMiss []string
		mcpHas  []string
		mcpMiss []string
	}
	for _, tc := range []surface{
		{
			command: []string{"records", "create"},
			tool:    "records_create",
			cliHas:  []string{"kind", "id", "parent-job", "stdin"},
			cliMiss: []string{"parent-job-id", "parent-job-kind", "parent-job-parent-job"},
			mcpHas:  []string{"kind", "id", "parentJob"},
			mcpMiss: []string{"parent-job-id", "parent-job-kind", "parent-job-parent-job", "parentJobId"},
		},
		{
			command: []string{"composed-records", "create"},
			tool:    "composed-records_create",
			cliHas:  []string{"kind", "parent-job"},
			cliMiss: []string{"parent-job-id", "parent-job-parent-job", "parent-job-parent-job-parent-job"},
			mcpHas:  []string{"kind", "parentJob"},
			mcpMiss: []string{"parent-job-parent-job", "parent-job-parent-job-parent-job"},
		},
		{
			command: []string{"moves", "create"},
			tool:    "moves_create",
			cliHas:  []string{"destination", "job-id", "job-kind", "job-parent-job"},
			cliMiss: []string{"job-parent-job-id", "job-parent-job-kind", "job-parent-job-parent-job"},
			mcpHas:  []string{"destination", "job-id", "job-kind", "job-parent-job"},
			mcpMiss: []string{"job-parent-job-id", "job-parent-job-parent-job"},
		},
		{
			command: []string{"direct-moves", "create"},
			tool:    "direct-moves_create",
			cliHas:  []string{"job-parent-job"},
			cliMiss: []string{"job-parent-job-id", "job-parent-job-parent-job"},
			mcpHas:  []string{"job-parent-job"},
			mcpMiss: []string{"job-parent-job-id", "job-parent-job-parent-job"},
		},
		{
			command: []string{"nodes", "create"},
			tool:    "nodes_create",
			cliHas:  []string{"name", "other-label", "other-node"},
			cliMiss: []string{"other-node-name", "other-node-label", "other-node-other"},
			mcpHas:  []string{"name", "other-label", "other-node"},
			mcpMiss: []string{"other-node-name", "other-node-other"},
		},
		{
			command: []string{"long-records", "create"},
			tool:    "long-records_create",
			cliHas:  []string{"kind", "parent-background-job"},
			cliMiss: []string{"parent-background-job-kind", "parent-background-job-parent-background-job"},
			mcpHas:  []string{"kind", "parentBackgroundJob"},
			mcpMiss: []string{"parent-background-job-kind", "parent-background-job-parent-background-job", "parentBackgroundJobKind"},
		},
	} {
		t.Run(strings.Join(tc.command, " "), func(t *testing.T) {
			command, _, err := cli.RootCmd().Find(tc.command)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.cliHas {
				if command.Flags().Lookup(name) == nil {
					t.Errorf("missing CLI flag %s", name)
				}
			}
			for _, name := range tc.cliMiss {
				if command.Flags().Lookup(name) != nil {
					t.Errorf("unexpected CLI flag %s", name)
				}
			}
			tool, ok := tools[tc.tool]
			if !ok {
				t.Fatalf("missing MCP tool %s", tc.tool)
			}
			for _, name := range tc.mcpHas {
				if _, ok := tool.Tool.InputSchema.Properties[name]; !ok {
					t.Errorf("missing MCP input %s", name)
				}
				if len(name) > 64 {
					t.Errorf("MCP input %s is longer than 64", name)
				}
			}
			for _, name := range tc.mcpMiss {
				if _, ok := tool.Tool.InputSchema.Properties[name]; ok {
					t.Errorf("unexpected MCP input %s", name)
				}
			}
		})
	}

	nestedJob := map[string]any{"kind": "REDO", "parentJob": map[string]any{"kind": "AGAIN"}}
	cases := []struct {
		name    string
		command []string
		stdin   string
		cliArgs []string
		tool    string
		mcpArgs map[string]any
		want    string
	}{
		{
			name:    "direct JSON flag",
			command: []string{"records", "create"},
			cliArgs: []string{"--kind", "UNDO", "--parent-job", "{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}"},
			tool:    "records_create",
			mcpArgs: map[string]any{"kind": "UNDO", "parentJob": nestedJob},
			want:    "{\"kind\":\"UNDO\",\"parentJob\":{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}}",
		},
		{
			name:    "direct stdin",
			command: []string{"records", "create"},
			stdin:   "{\"kind\":\"UNDO\",\"parentJob\":{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}}",
			tool:    "records_create",
			mcpArgs: map[string]any{"kind": "UNDO", "parentJob": nestedJob},
			want:    "{\"kind\":\"UNDO\",\"parentJob\":{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}}",
		},
		{
			name:    "allOf JSON flag",
			command: []string{"composed-records", "create"},
			cliArgs: []string{"--kind", "UNDO", "--parent-job", "{\"kind\":\"REDO\"}"},
			tool:    "composed-records_create",
			mcpArgs: map[string]any{"kind": "UNDO", "parentJob": map[string]any{"kind": "REDO"}},
			want:    "{\"kind\":\"UNDO\",\"parentJob\":{\"kind\":\"REDO\"}}",
		},
		{
			name:    "nested allOf",
			command: []string{"moves", "create"},
			cliArgs: []string{"--destination", "desk", "--job-kind", "UNDO", "--job-parent-job", "{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}"},
			tool:    "moves_create",
			mcpArgs: map[string]any{"destination": "desk", "job-kind": "UNDO", "job-parent-job": nestedJob},
			want:    "{\"destination\":\"desk\",\"job\":{\"kind\":\"UNDO\",\"parentJob\":{\"kind\":\"REDO\",\"parentJob\":{\"kind\":\"AGAIN\"}}}}",
		},
		{
			name:    "mutual allOf",
			command: []string{"nodes", "create"},
			cliArgs: []string{"--name", "root", "--other-label", "L", "--other-node", "{\"name\":\"inner\"}"},
			tool:    "nodes_create",
			mcpArgs: map[string]any{"name": "root", "other-label": "L", "other-node": map[string]any{"name": "inner"}},
			want:    "{\"name\":\"root\",\"other\":{\"label\":\"L\",\"node\":{\"name\":\"inner\"}}}",
		},
		{
			name:    "long allOf name",
			command: []string{"long-records", "create"},
			cliArgs: []string{"--kind", "UNDO", "--parent-background-job", "{\"kind\":\"REDO\",\"parentBackgroundJob\":{\"kind\":\"AGAIN\"}}"},
			tool:    "long-records_create",
			mcpArgs: map[string]any{"kind": "UNDO", "parentBackgroundJob": map[string]any{"kind": "REDO", "parentBackgroundJob": map[string]any{"kind": "AGAIN"}}},
			want:    "{\"kind\":\"UNDO\",\"parentBackgroundJob\":{\"kind\":\"REDO\",\"parentBackgroundJob\":{\"kind\":\"AGAIN\"}}}",
		},
	}
	for _, tc := range cases {
		for _, surface := range []string{"CLI", "MCP"} {
			if surface == "MCP" && tc.stdin != "" {
				continue
			}
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				if surface == "CLI" {
					cmd := cli.RootCmd()
					cmd.SetOut(&bytes.Buffer{})
					cmd.SetErr(&bytes.Buffer{})
					args := append(append([]string{}, tc.command...), "--json")
					if tc.stdin != "" {
						reader, writer, err := os.Pipe()
						if err != nil {
							t.Fatal(err)
						}
						orig := os.Stdin
						os.Stdin = reader
						if _, err := writer.Write([]byte(tc.stdin)); err != nil {
							t.Fatal(err)
						}
						if err := writer.Close(); err != nil {
							t.Fatal(err)
						}
						args = append(args, "--stdin")
						defer func() {
							os.Stdin = orig
							_ = reader.Close()
						}()
					}
					cmd.SetArgs(append(args, tc.cliArgs...))
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
					var actual, expected map[string]any
					if err := json.Unmarshal(got, &actual); err != nil {
						t.Fatalf("decode body %s: %v", got, err)
					}
					if err := json.Unmarshal([]byte(tc.want), &expected); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("body = %s, want %s", got, tc.want)
					}
				default:
					t.Fatal("no HTTP request captured")
				}
			})
		}
	}
}
`
