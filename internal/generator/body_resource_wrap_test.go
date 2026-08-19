package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateResourceRootSchemaWrapsMutationBody(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.ParseBytes([]byte(`
name: resource-wrap
base_url: https://api.example.com
auth:
  type: none
resources:
  issues:
    description: Issues
    endpoints:
      list:
        method: GET
        path: /issues
        description: List issues
      create:
        method: POST
        path: /issues
        description: Create an issue
        body:
          properties:
            issue:
              type: object
              properties:
                notes:
                  type: string
                subject:
                  type: string
      update:
        method: PUT
        path: /issues/{id}
        description: Update an issue
        params:
          - name: id
            type: string
            positional: true
            required: true
        body:
          properties:
            issue:
              type: object
              properties:
                notes:
                  type: string
  watchers:
    description: Watchers
    endpoints:
      list:
        method: GET
        path: /watchers
        description: List watchers
      create:
        method: POST
        path: /issues/{id}/watchers
        description: Add a watcher
        params:
          - name: id
            type: string
            positional: true
            required: true
        body:
          properties:
            user_id:
              type: integer
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
      create:
        method: POST
        path: /items
        description: Create an item
        body:
          - name: id
            type: int
          - name: name
            type: string
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "issues_create.go")
	require.Contains(t, createSrc, `body = map[string]any{"issue": bodyMap}`)
	require.Contains(t, createSrc, `bodyMap["notes"] = bodyIssueNotes`)
	require.Contains(t, createSrc, `bodyMap["subject"] = bodyIssueSubject`)
	require.NotContains(t, createSrc, `bodyMap["issue"]`)
	require.Contains(t, createSrc, "body = jsonBody")
	require.NotContains(t, createSrc, `body = map[string]any{"issue": jsonBody}`)

	updateSrc := readGeneratedFile(t, outputDir, "internal", "cli", "issues_update.go")
	require.Contains(t, updateSrc, `body = map[string]any{"issue": bodyMap}`)
	require.Contains(t, updateSrc, `bodyMap["notes"] = bodyIssueNotes`)

	watcherSrc := readGeneratedFile(t, outputDir, "internal", "cli", "watchers_create.go")
	require.Contains(t, watcherSrc, "body = bodyMap")
	require.NotContains(t, watcherSrc, `body = map[string]any{`)
	require.Contains(t, watcherSrc, `bodyMap["user_id"] = bodyUserId`)

	itemSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_create.go")
	require.Contains(t, itemSrc, "body = bodyMap")
	require.NotContains(t, itemSrc, `body = map[string]any{`)
	require.Contains(t, itemSrc, `bodyMap["id"] = bodyId`)
	require.Contains(t, itemSrc, `bodyMap["name"] = bodyName`)
	require.Contains(t, itemSrc, "body = jsonBody")

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func runResourceWrap(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRootCmd(&rootFlags{asJSON: true})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestResourceWrapSendsWrappedIssueBody(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	t.Cleanup(server.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RESOURCE_WRAP_BASE_URL", server.URL)

	if err := runResourceWrap(t, "issues", "create", "--issue-notes", "hello"); err != nil {
		t.Fatalf("create: %v", err)
	}
	issue, ok := gotBody["issue"].(map[string]any)
	if !ok {
		t.Fatalf("create body = %#v, want issue wrapper", gotBody)
	}
	if issue["notes"] != "hello" {
		t.Fatalf("create notes = %#v", issue["notes"])
	}
	if _, ok := gotBody["notes"]; ok {
		t.Fatalf("create body leaked flat notes: %#v", gotBody)
	}

	if err := runResourceWrap(t, "issues", "update", "42", "--issue-notes", "edited"); err != nil {
		t.Fatalf("update: %v", err)
	}
	issue, ok = gotBody["issue"].(map[string]any)
	if !ok || issue["notes"] != "edited" {
		t.Fatalf("update body = %#v", gotBody)
	}

	if err := runResourceWrap(t, "watchers", "create", "42", "--user-id", "9"); err != nil {
		t.Fatalf("watchers: %v", err)
	}
	if fmt.Sprint(gotBody["user_id"]) != "9" {
		t.Fatalf("watchers body = %#v, want flat user_id", gotBody)
	}
	if _, ok := gotBody["issue"]; ok {
		t.Fatalf("watchers body unexpectedly wrapped: %#v", gotBody)
	}

	if err := runResourceWrap(t, "items", "create", "--id", "7", "--name", "widget"); err != nil {
		t.Fatalf("items: %v", err)
	}
	if fmt.Sprint(gotBody["id"]) != "7" || gotBody["name"] != "widget" {
		t.Fatalf("items body = %#v, want top-level id and name", gotBody)
	}
}

func TestResourceWrapStdinJSONStaysCallerShaped(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	t.Cleanup(server.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RESOURCE_WRAP_BASE_URL", server.URL)

	restore := replaceStdin(t, ` + "`" + `{"issue":{"notes":"from-stdin"}}` + "`" + `)
	defer restore()
	if err := runResourceWrap(t, "issues", "create", "--stdin"); err != nil {
		t.Fatalf("stdin wrap: %v", err)
	}
	issue, ok := gotBody["issue"].(map[string]any)
	if !ok || issue["notes"] != "from-stdin" {
		t.Fatalf("stdin wrapped body = %#v", gotBody)
	}

	restore = replaceStdin(t, ` + "`" + `{"id":3,"name":"from-stdin"}` + "`" + `)
	defer restore()
	if err := runResourceWrap(t, "items", "create", "--stdin"); err != nil {
		t.Fatalf("stdin id: %v", err)
	}
	if fmt.Sprint(gotBody["id"]) != "3" || gotBody["name"] != "from-stdin" {
		t.Fatalf("stdin top-level id body = %#v", gotBody)
	}
}

func replaceStdin(t *testing.T, data string) func() {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	if _, err := w.WriteString(data); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	return func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "resource_wrap_runtime_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestResourceWrap", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratePromotedResourceRootWrapsMutationBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-wrap")
	apiSpec.Resources = map[string]spec.Resource{
		"tickets": {
			Description: "Tickets",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/tickets",
					Description: "Create a ticket",
					Body: []spec.Param{{
						Name: "ticket",
						Type: "object",
						Fields: []spec.Param{
							{Name: "title", Type: "string"},
						},
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_tickets.go")
	require.Contains(t, src, `var body any = map[string]any{"ticket": bodyMap}`)
	require.Contains(t, src, `bodyMap["title"] = bodyTicketTitle`)
	require.NotContains(t, src, `bodyMap["ticket"]`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateOpenAPIResourceRootWrapsMutationBody(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Schema Wrap
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /issues:
    get:
      tags: [issues]
      operationId: listIssues
      responses:
        "200":
          description: ok
    post:
      tags: [issues]
      operationId: createIssue
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                issue:
                  type: object
                  properties:
                    notes:
                      type: string
                    subject:
                      type: string
      responses:
        "201":
          description: created
  /issues/{id}:
    put:
      tags: [issues]
      operationId: updateIssue
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                issue:
                  type: object
                  properties:
                    notes:
                      type: string
      responses:
        "200":
          description: ok
  /issues/{id}/watchers:
    post:
      tags: [watchers]
      operationId: addWatcher
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                user_id:
                  type: integer
      responses:
        "201":
          description: created
  /watchers:
    get:
      tags: [watchers]
      operationId: listWatchers
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "issues_create.go")
	require.Contains(t, createSrc, `body = map[string]any{"issue": bodyMap}`)
	require.Contains(t, createSrc, `bodyMap["notes"] = bodyIssueNotes`)
	require.NotContains(t, createSrc, `bodyMap["issue"]`)
	require.Contains(t, createSrc, "body = jsonBody")

	updateSrc := readGeneratedFile(t, outputDir, "internal", "cli", "issues_update.go")
	require.Contains(t, updateSrc, `body = map[string]any{"issue": bodyMap}`)

	watcherSrc := readGeneratedCLIFileContaining(t, outputDir, `bodyMap["user_id"]`)
	require.Contains(t, watcherSrc, "body = bodyMap")
	require.NotContains(t, watcherSrc, `body = map[string]any{`)

	requireGeneratedCompiles(t, outputDir)
}
