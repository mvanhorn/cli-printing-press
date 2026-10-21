package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCursorRequestParamsNilGenerator(t *testing.T) {
	t.Parallel()

	var gen *Generator
	require.NoError(t, gen.resolveCursorRequestParams())
	require.NoError(t, (&Generator{}).resolveCursorRequestParams())
}

func TestGenerateFillsCursorParamWithoutValidate(t *testing.T) {
	t.Parallel()

	apiSpec := cursorFillSpec("fillcursor")
	list := apiSpec.Resources["records"].Endpoints["list"]
	require.Empty(t, list.Pagination.CursorParam)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	assert.Equal(t, "offset", apiSpec.Resources["records"].Endpoints["list"].Pagination.CursorParam)
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "records_list.go"), "offset", "cursor")
	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, tools, `mcpPageConfig{CursorParam: "offset", NextCursorPath: "offset"}`)
}

func TestGenerateMCPSurfaceFillsCursorParam(t *testing.T) {
	t.Parallel()

	apiSpec := cursorFillSpec("mcpcursor")
	require.Empty(t, apiSpec.Resources["records"].Endpoints["list"].Pagination.CursorParam)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.GenerateMCPSurface())

	assert.Equal(t, "offset", apiSpec.Resources["records"].Endpoints["list"].Pagination.CursorParam)
	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, tools, `mcpPageConfig{CursorParam: "offset", NextCursorPath: "offset"}`)
}

func TestGenerateRejectsUndeterminableCursorRequestParam(t *testing.T) {
	t.Parallel()

	apiSpec := cursorFillSpec("dottedcursor")
	apiSpec.Resources = map[string]spec.Resource{
		"nodes": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method: "GET",
					Path:   "/nodes",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						NextCursorPath: "pageInfo.endCursor",
					},
					Params: []spec.Param{
						{Name: "after", Type: "string"},
						{Name: "first", Type: "integer"},
					},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "out")
	err := New(apiSpec, outputDir).Generate()
	require.ErrorContains(t, err, "pageInfo.endCursor")
	require.ErrorContains(t, err, "cursor_param")
	require.ErrorContains(t, err, "no request parameter")
	assert.Empty(t, apiSpec.Resources["nodes"].Endpoints["list"].Pagination.CursorParam)
	_, statErr := os.Stat(outputDir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestGenerateRejectsAmbiguousCursorRequestParam(t *testing.T) {
	t.Parallel()

	apiSpec := cursorFillSpec("ambiguouscursor")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method: "GET",
					Path:   "/items",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						NextCursorPath: "next_cursor",
					},
					Params: []spec.Param{
						{Name: "after", Type: "string"},
						{Name: "cursor", Type: "string"},
					},
				},
			},
		},
	}
	err := New(apiSpec, filepath.Join(t.TempDir(), "out")).Generate()
	require.ErrorContains(t, err, "next_cursor")
	require.ErrorContains(t, err, "after, cursor")
	require.ErrorContains(t, err, "cursor_param")
	assert.Empty(t, apiSpec.Resources["items"].Endpoints["list"].Pagination.CursorParam)
}

func TestGeneratedCursorFieldAllSendsCursor(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.ParseBytes([]byte(cursorPagesYAML))
	require.NoError(t, err)
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	assert.Equal(t, "offset", apiSpec.Resources["records"].Endpoints["list"].Pagination.CursorParam)
	assert.Equal(t, "pagination_cursor", apiSpec.Resources["categories"].Endpoints["list"].Pagination.CursorParam)
	assert.Equal(t, "after", apiSpec.Resources["kept"].Endpoints["list"].Pagination.CursorParam)
	assert.Empty(t, apiSpec.Resources["photos"].Endpoints["list"].Pagination.CursorParam)
	assert.Empty(t, apiSpec.Resources["skips"].Endpoints["list"].Pagination.CursorParam)
	assert.Equal(t, "endCursor", apiSpec.Resources["nodes"].Endpoints["list"].Pagination.CursorParam)

	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "records_list.go"), "offset", "cursor")
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "promoted_categories.go"), "pagination_cursor", "cursor")
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "kept_list.go"), "after", "cursor")
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "photos_list.go"), "", "page")
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "skips_list.go"), "", "offset")
	assertPaginatedCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "nodes_list.go"), "endCursor", "cursor")
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "categories_list.go"))
	require.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "cursor_field_all_test.go"), []byte(cursorFieldAllBehaviorTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestCursorFieldAllSendsCursor", "-count=1")
}

func cursorFillSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Response:    spec.ResponseDef{Type: "array"},
					Pagination:  &spec.Pagination{Type: "cursor", NextCursorPath: "offset"},
					Params:      []spec.Param{{Name: "offset", Type: "string"}},
				},
				"get": {
					Method:      "GET",
					Path:        "/records/{id}",
					Description: "Get a record",
					Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
				},
			},
		},
	}
	return apiSpec
}

func assertPaginatedCursorSlot(t *testing.T, src, cursorParam, paginationType string) {
	t.Helper()
	want := fmt.Sprintf("flagAll && !flags.dryRun, %q, %q", cursorParam, paginationType)
	assert.Contains(t, strings.Join(strings.Fields(src), " "), want)
}

const cursorPagesYAML = `
name: cursorpages
description: Cursor pages
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
learn:
  disabled: true
resources:
  records:
    description: Records
    endpoints:
      list:
        method: GET
        path: /records
        description: List records
        params:
          - name: offset
            type: string
        response:
          type: array
        pagination:
          type: cursor
          cursor_field: offset
      get:
        method: GET
        path: /records/{id}
        description: Get a record
        params:
          - name: id
            type: string
            required: true
            positional: true
  categories:
    description: Categories
    endpoints:
      list:
        method: GET
        path: /categories
        description: List categories
        params:
          - name: pagination_cursor
            type: string
          - name: pagination_limit
            type: integer
        response:
          type: array
        pagination:
          type: cursor
          cursor_field: next_cursor
  kept:
    description: Kept
    endpoints:
      list:
        method: GET
        path: /kept
        description: List kept
        params:
          - name: after
            type: string
        response:
          type: array
        pagination:
          type: cursor
          cursor_param: after
          cursor_field: next_cursor
      get:
        method: GET
        path: /kept/{id}
        description: Get a kept row
        params:
          - name: id
            type: string
            required: true
            positional: true
  photos:
    description: Photos
    endpoints:
      list:
        method: GET
        path: /photos
        description: List photos
        params:
          - name: page
            type: integer
        response:
          type: array
        pagination:
          type: page
          cursor_field: page
      get:
        method: GET
        path: /photos/{id}
        description: Get a photo
        params:
          - name: id
            type: string
            required: true
            positional: true
  skips:
    description: Skips
    endpoints:
      list:
        method: GET
        path: /skips
        description: List skips
        params:
          - name: offset
            type: integer
        response:
          type: array
        pagination:
          type: offset
          cursor_field: next_offset
      get:
        method: GET
        path: /skips/{id}
        description: Get a skip
        params:
          - name: id
            type: string
            required: true
            positional: true
  nodes:
    description: Nodes
    endpoints:
      list:
        method: GET
        path: /nodes
        description: List nodes
        params:
          - name: endCursor
            type: string
          - name: after
            type: string
        response:
          type: array
        pagination:
          type: cursor
          cursor_field: pageInfo.endCursor
      get:
        method: GET
        path: /nodes/{id}
        description: Get a node
        params:
          - name: id
            type: string
            required: true
            positional: true
`

const cursorFieldAllBehaviorTest = `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestCursorFieldAllSendsCursor(t *testing.T) {
	var mu sync.Mutex
	tokens := map[string][]string{}
	queries := map[string][]string{}
	record := func(name, token, raw string) {
		mu.Lock()
		defer mu.Unlock()
		tokens[name] = append(tokens[name], token)
		queries[name] = append(queries[name], raw)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch r.URL.Path {
		case "/records":
			tok := ""
			if _, ok := q["offset"]; ok {
				tok = q.Get("offset")
			}
			record("records", tok, r.URL.RawQuery)
			body := map[string]any{"records": cursorPageRows("row", tok)}
			if next := cursorPageNext(tok); next != "" {
				body["offset"] = next
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/categories":
			if _, ok := q["next_cursor"]; ok {
				http.Error(w, "next_cursor is not the request parameter", http.StatusBadRequest)
				return
			}
			tok := ""
			if _, ok := q["pagination_cursor"]; ok {
				tok = q.Get("pagination_cursor")
			}
			record("categories", tok, r.URL.RawQuery)
			body := map[string]any{"entries": cursorPageRows("cat", tok)}
			if next := cursorPageNext(tok); next != "" {
				body["next_cursor"] = next
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/kept":
			if q.Get("cursor") != "" {
				http.Error(w, "explicit cursor_param must stay after", http.StatusBadRequest)
				return
			}
			tok := ""
			if _, ok := q["after"]; ok {
				tok = q.Get("after")
			}
			record("kept", tok, r.URL.RawQuery)
			body := map[string]any{"items": cursorPageRows("kept", tok)}
			if next := cursorPageNext(tok); next != "" {
				body["next_cursor"] = next
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("CURSORPAGES_BASE_URL", server.URL)

	records := runCursorPages(t, "records", "list", "--all", "--json")
	assertCursorPageIDs(t, records, "row")
	categories := runCursorPages(t, "categories", "--all", "--json")
	assertCursorPageIDs(t, categories, "cat")
	kept := runCursorPages(t, "kept", "list", "--all", "--json")
	assertCursorPageIDs(t, kept, "kept")

	mu.Lock()
	defer mu.Unlock()
	assertCursorTokens(t, "offset", tokens["records"], queries["records"])
	assertCursorTokens(t, "pagination_cursor", tokens["categories"], queries["categories"])
	assertCursorTokens(t, "after", tokens["kept"], queries["kept"])
}

func runCursorPages(t *testing.T, args ...string) string {
	t.Helper()
	var flags rootFlags
	root := newRootCmd(&flags)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--home", t.TempDir(), "--json"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\nstderr: %s\nstdout: %s", args, err, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func assertCursorPageIDs(t *testing.T, stdout, prefix string) {
	t.Helper()
	for i := 1; i <= 6; i++ {
		id := prefix + "-" + strconv.Itoa(i)
		if !strings.Contains(stdout, id) {
			t.Fatalf("output missing %s: %s", id, stdout)
		}
	}
}

func assertCursorTokens(t *testing.T, param string, got, raw []string) {
	t.Helper()
	want := []string{"", "c1", "c2"}
	if len(got) != len(want) {
		t.Fatalf("%s tokens = %#v raw %#v", param, got, raw)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s tokens = %#v raw %#v", param, got, raw)
		}
	}
	if len(raw) != 3 {
		t.Fatalf("%s raw = %#v", param, raw)
	}
	if strings.Contains(raw[0], param+"=") {
		t.Fatalf("first %s request included the cursor: %s", param, raw[0])
	}
	if !strings.Contains(raw[1], param+"=c1") || !strings.Contains(raw[2], param+"=c2") {
		t.Fatalf("%s follow-ups = %#v", param, raw)
	}
}

func cursorPageRows(prefix, token string) []map[string]string {
	start := 0
	switch token {
	case "":
		start = 1
	case "c1":
		start = 3
	case "c2":
		start = 5
	default:
		return []map[string]string{}
	}
	return []map[string]string{
		{"id": prefix + "-" + strconv.Itoa(start)},
		{"id": prefix + "-" + strconv.Itoa(start+1)},
	}
}

func cursorPageNext(token string) string {
	switch token {
	case "":
		return "c1"
	case "c1":
		return "c2"
	default:
		return ""
	}
}
`
