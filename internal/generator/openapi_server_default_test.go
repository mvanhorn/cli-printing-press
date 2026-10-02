package generator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParamOmitsOpenAPIServerDefaultOnly(t *testing.T) {
	t.Parallel()

	serverDefault := spec.Param{Name: "resource_subtype", Type: "string", Default: "milestone", ServerDefault: true}
	assert.Equal(t, `""`, defaultVal(serverDefault))
	assert.False(t, paramHasDefault(serverDefault))
	_, ok := mcpParamDefaultValue(serverDefault)
	assert.False(t, ok)
	assert.Equal(t, `cmd.Flags().Changed("resource-subtype")`, paramPresenceExpr(serverDefault))
	assert.Equal(t, " (default: milestone)", serverDefaultHint(serverDefault))

	native := spec.Param{Name: "location", Type: "string", Default: "city"}
	assert.Equal(t, `"city"`, defaultVal(native))
	assert.True(t, paramHasDefault(native))
	got, ok := mcpParamDefaultValue(native)
	assert.True(t, ok)
	assert.Equal(t, "city", got)
	assert.Equal(t, "", serverDefaultHint(native))

	zeroNative := spec.Param{Name: "defaultOffset", In: "query", Type: "int", Default: 0}
	assert.True(t, paramHasDefault(zeroNative))
	assert.Equal(t, "true", paramPresenceExpr(zeroNative))

	optionalInt := spec.Param{Name: "opt_count", Type: "integer", Default: 5, ServerDefault: true}
	assert.Equal(t, "0", defaultVal(optionalInt))
	assert.Equal(t, `cmd.Flags().Changed("opt-count")`, paramPresenceExpr(optionalInt))

	limit := spec.Param{Name: "limit", Type: "number", Default: 25.0, ServerDefault: true}
	assert.Equal(t, "0", defaultValForParamRequired(limit, false, true))

	active := spec.Param{Name: "active", Type: "boolean", Default: false, ServerDefault: true}
	assert.Equal(t, "false", defaultVal(active))
	assert.Equal(t, `cmd.Flags().Changed("active")`, paramPresenceExpr(active))

	required := spec.Param{Name: "view", Type: "string", Required: true, Default: "summary", ServerDefault: true}
	assert.Equal(t, `"summary"`, defaultVal(required))
	def, ok := mcpParamDefaultValue(required)
	assert.True(t, ok)
	assert.Equal(t, "summary", def)

	pathParam := spec.Param{Name: "page", Type: "integer", Default: 1, ServerDefault: true, PathParam: true}
	assert.Equal(t, "1", defaultVal(pathParam))

	quoted := spec.Param{Name: "q", Type: "string", Default: `say "hi"`, ServerDefault: true}
	assert.Equal(t, ` (default: say "hi")`, serverDefaultHint(quoted))
	backslash := spec.Param{Name: "path", Type: "string", Default: `a\b`, ServerDefault: true}
	assert.Equal(t, ` (default: a\b)`, serverDefaultHint(backslash))
	multiline := spec.Param{Name: "text", Type: "string", Default: "line1\nline2", ServerDefault: true}
	assert.Equal(t, " (default: line1 line2)", serverDefaultHint(multiline))
	long := spec.Param{Name: "note", Type: "string", Default: strings.Repeat("n", 40), ServerDefault: true}
	assert.Equal(t, " (default: "+strings.Repeat("n", 30)+"...)", serverDefaultHint(long))
	split := spec.Param{Name: "mark", Type: "string", Default: strings.Repeat("a", 29) + "é", ServerDefault: true}
	assert.Equal(t, " (default: "+strings.Repeat("a", 29)+"...)", serverDefaultHint(split))

	nested := bodyFlagRegs(spec.Endpoint{Body: []spec.Param{{
		Name: "settings", Type: "object",
		Fields: []spec.Param{{
			Name: "mode", Type: "string", Description: "Mode",
			Default: `a\b "quote"`, ServerDefault: true,
		}},
	}}})
	assert.Contains(t, nested, `"settings-mode", ""`)
	assert.Contains(t, nested, strconv.Quote(`Mode (default: a\b "quote")`))
	assert.NotContains(t, nested, `"settings-mode", "a`)
}

func TestGeneratedOpenAPIOptionalDefaultsStayOffTheWire(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(omitDefaultsSpec))
	require.NoError(t, err)
	items, ok := apiSpec.Resources["items"]
	require.True(t, ok, "resources: %s", resourceNames(apiSpec))
	for _, name := range []string{"search", "list", "create"} {
		_, ok := items.Endpoints[name]
		require.True(t, ok, "items endpoints: %s", endpointNames(items))
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_search.go")
	listSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_list.go")
	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_create.go")
	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")

	assert.Contains(t, searchSrc, `"resource-subtype", ""`)
	assert.NotContains(t, searchSrc, `"resource-subtype", "milestone"`)
	assert.Contains(t, searchSrc, "(default: milestone)")
	assert.Contains(t, searchSrc, `Changed("resource-subtype")`)
	assert.Contains(t, searchSrc, `Changed("include-completed")`)
	assert.Contains(t, searchSrc, `Changed("opt-count")`)
	assert.Contains(t, searchSrc, `Changed("active")`)
	assert.Contains(t, searchSrc, `Changed("x-mode")`)
	assert.Regexp(t, `BoolVar\(&flag\w+, "include-completed", false,`, searchSrc)
	assert.NotRegexp(t, `BoolVar\(&flag\w+, "include-completed", true,`, searchSrc)
	assert.Regexp(t, `IntVar\(&flag\w+, "opt-count", 0,`, searchSrc)
	assert.Contains(t, searchSrc, `"view", "summary"`)
	assert.Contains(t, searchSrc, `"x-api-version", "2026-04-01"`)
	assert.Contains(t, searchSrc, `"x-mode", ""`)
	assert.Contains(t, searchSrc, `"label", ""`)
	assert.Contains(t, searchSrc, `"note", ""`)
	assert.Contains(t, searchSrc, `(default: say \"hi\")`)
	assert.Contains(t, searchSrc, "(default: "+strings.Repeat("n", 30)+"...)")
	assert.NotContains(t, searchSrc, strings.Repeat("n", 40))

	assert.Contains(t, listSrc, "retainCLIQueryParams")
	assert.Contains(t, listSrc, `"resource-subtype", ""`)
	assert.NotContains(t, listSrc, `"resource-subtype", "milestone"`)
	assert.Regexp(t, `IntVar\(&flag\w+, "limit", 0,`, listSrc)
	assert.NotRegexp(t, `IntVar\(&flag\w+, "limit", 25,`, listSrc)
	assert.Contains(t, listSrc, "(default: 25)")

	assert.Contains(t, createSrc, `"resource-subtype", ""`)
	assert.Contains(t, createSrc, "(default: milestone)")
	assert.Contains(t, createSrc, "(default: 5)")
	assert.Contains(t, createSrc, `"name", "untitled"`)
	assert.Regexp(t, `IntVar\(&body\w+, "count", 0,`, createSrc)
	assert.Contains(t, createSrc, `Changed("count")`)
	assert.Contains(t, createSrc, `Changed("resource-subtype")`)
	assert.Contains(t, createSrc, `"settings-mode", ""`)
	assert.Contains(t, createSrc, "(default: compact)")
	assert.Contains(t, createSrc, `Changed("settings-mode")`)
	assert.Contains(t, createSrc, `"settings-kind", "box"`)
	assert.NotContains(t, createSrc, `"settings-mode", "compact"`)
	assert.Contains(t, createSrc, `cmd.Flags().Changed("settings-kind") || cmd.Flags().Changed("settings-layout") || (cmd.Flags().Changed("settings-mode") || bodySettingsMode != "")`)
	assert.Contains(t, createSrc, `nestedSettings["kind"] = bodySettingsKind`)
	assert.Contains(t, createSrc, `json.Unmarshal([]byte(bodySettingsLayout), &parsedSettingsLayout)`)
	assert.Contains(t, createSrc, `nestedSettings["layout"] = parsedSettingsLayout`)
	assert.NotContains(t, createSrc, `nestedSettings["layout"] = bodySettingsLayout`)
	assert.NotContains(t, createSrc, `bodySettingsKind != ""`)

	assert.NotContains(t, tools, `Default: "milestone"`)
	assert.NotContains(t, tools, `Default: "full"`)
	assert.NotContains(t, tools, `Default: "true"`)
	assert.NotContains(t, tools, `Default: "5"`)
	assert.NotContains(t, tools, `Default: "25"`)
	assert.Contains(t, tools, `Default: "summary"`)
	assert.Contains(t, tools, `Default: "2026-04-01"`)
	assert.Contains(t, tools, "(default: milestone)")
	assert.Contains(t, tools, `(default: say \"hi\")`)
	assert.Contains(t, tools, "(default: compact)")
	assert.NotContains(t, tools, `Default: "compact"`)
	assert.NotContains(t, tools, `Default: "say`)
	assert.Contains(t, tools, `Default: "\"box\""`)
	assert.Contains(t, tools, `Default: "{\"kind\":\"box\"}"`)
	assert.NotContains(t, tools, `Default: "\"{\\\"kind\\\":\\\"box\\\"}\""`)
	assert.Contains(t, tools, `DefaultScope: []string{"settings"}`)

	var mu sync.Mutex
	var got capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = capturedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: append([]byte(nil), body...)}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/items":
			_, _ = w.Write([]byte(`{"data":[],"next_cursor":""}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(server.Close)

	envName := naming.EnvPrefix(apiSpec.Name) + "_BASE_URL"
	t.Setenv(envName, server.URL)
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	help, _ := runGeneratedBinary(t, binaryPath, "items", "search", "--help")
	assert.Contains(t, help, "(default: milestone)")
	assert.Contains(t, help, `(default: say "hi")`)
	assert.Contains(t, help, "(default: "+strings.Repeat("n", 30)+"...)")
	assert.NotContains(t, help, strings.Repeat("n", 40))

	take := func() capturedRequest {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		return got
	}

	runGeneratedBinary(t, binaryPath, "items", "search", "--json")
	omitted := take()
	assert.NotContains(t, omitted.Query, "resource_subtype")
	assert.NotContains(t, omitted.Query, "include_completed")
	assert.NotContains(t, omitted.Query, "opt_count")
	assert.NotContains(t, omitted.Query, "active")
	assert.Equal(t, []string{"summary"}, omitted.Query["view"])
	assert.Equal(t, "2026-04-01", omitted.Header.Get("X-Api-Version"))
	assert.Empty(t, omitted.Header.Get("X-Mode"))
	assert.NotContains(t, omitted.Query, "label")
	assert.NotContains(t, omitted.Query, "note")

	runGeneratedBinary(t, binaryPath, "items", "search", "--json",
		"--resource-subtype", "task",
		"--include-completed=false",
		"--opt-count", "0",
		"--active=false",
		"--x-mode", "compact",
		"--label", `say "hi"`,
		"--note", strings.Repeat("n", 40),
	)
	explicit := take()
	assert.Equal(t, []string{"task"}, explicit.Query["resource_subtype"])
	assert.Equal(t, []string{"false"}, explicit.Query["include_completed"])
	assert.Equal(t, []string{"0"}, explicit.Query["opt_count"])
	assert.Equal(t, []string{"false"}, explicit.Query["active"])
	assert.Equal(t, []string{"summary"}, explicit.Query["view"])
	assert.Equal(t, "compact", explicit.Header.Get("X-Mode"))
	assert.Equal(t, "2026-04-01", explicit.Header.Get("X-Api-Version"))
	assert.Equal(t, []string{`say "hi"`}, explicit.Query["label"])
	assert.Equal(t, []string{strings.Repeat("n", 40)}, explicit.Query["note"])

	runGeneratedBinary(t, binaryPath, "items", "list", "--json")
	listed := take()
	assert.Equal(t, "/items", listed.Path)
	assert.NotContains(t, listed.Query, "resource_subtype")
	assert.NotContains(t, listed.Query, "limit")

	runGeneratedBinary(t, binaryPath, "items", "list", "--json", "--resource-subtype", "task", "--limit", "0")
	listedExplicit := take()
	assert.Equal(t, []string{"task"}, listedExplicit.Query["resource_subtype"])
	assert.Equal(t, []string{"0"}, listedExplicit.Query["limit"])

	runGeneratedBinary(t, binaryPath, "items", "create", "--json")
	created := take()
	assert.Equal(t, http.MethodPost, created.Method)
	body := decodeObjectBody(t, created.Body)
	assert.Equal(t, "untitled", scalarString(body["name"]))
	assert.NotContains(t, body, "resource_subtype")
	assert.NotContains(t, body, "count")
	assert.NotContains(t, body, "settings")

	runGeneratedBinary(t, binaryPath, "items", "create", "--json", "--settings-kind", "crate")
	kindOnly := take()
	body = decodeObjectBody(t, kindOnly.Body)
	settings, _ := body["settings"].(map[string]any)
	assert.Equal(t, "crate", scalarString(settings["kind"]))
	assert.Equal(t, map[string]any{"kind": "box"}, settings["layout"])
	assert.NotContains(t, settings, "mode")

	runGeneratedBinary(t, binaryPath, "items", "create", "--json", "--name", "widget", "--resource-subtype", "approval", "--count", "0", "--settings-mode", "compact")
	createdExplicit := take()
	body = decodeObjectBody(t, createdExplicit.Body)
	assert.Equal(t, "widget", scalarString(body["name"]))
	assert.Equal(t, "approval", scalarString(body["resource_subtype"]))
	assert.Equal(t, "0", scalarString(body["count"]))
	settings, _ = body["settings"].(map[string]any)
	assert.Equal(t, "compact", scalarString(settings["mode"]))
	assert.Equal(t, "box", scalarString(settings["kind"]))
	assert.Equal(t, map[string]any{"kind": "box"}, settings["layout"])

	mcpRuntime := `package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestOpenAPIOptionalDefaultsStayOffMCPWire(t *testing.T) {
	var gotMethod, gotPath string
	var gotQuery = map[string][]string{}
	var gotHeader http.Header
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotHeader = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte("{\"ok\":true}"))
		case r.URL.Path == "/items":
			_, _ = w.Write([]byte("{\"data\":[],\"next_cursor\":\"\"}"))
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer upstream.Close()

	t.Setenv("` + envName + `", upstream.URL)
	s := server.NewMCPServer("omit-defaults", "test")
	RegisterTools(s)
	tools := s.ListTools()

	search := mustTool(t, tools, "items_search")
	if !contains(search.Tool.Description, "(default: milestone)") {
		t.Fatalf("tool description missing server default: %s", search.Tool.Description)
	}
	call(t, search.Handler, map[string]any{})
	if _, ok := gotQuery["resource_subtype"]; ok {
		t.Fatalf("omitted resource_subtype was sent: %v", gotQuery)
	}
	if _, ok := gotQuery["include_completed"]; ok {
		t.Fatalf("omitted include_completed was sent: %v", gotQuery)
	}
	if _, ok := gotQuery["opt_count"]; ok {
		t.Fatalf("omitted opt_count was sent: %v", gotQuery)
	}
	if _, ok := gotQuery["active"]; ok {
		t.Fatalf("omitted active was sent: %v", gotQuery)
	}
	if got := gotQuery["view"]; len(got) != 1 || got[0] != "summary" {
		t.Fatalf("view = %v, want summary", gotQuery["view"])
	}
	if gotHeader.Get("X-Api-Version") != "2026-04-01" {
		t.Fatalf("X-Api-Version = %q", gotHeader.Get("X-Api-Version"))
	}
	if gotHeader.Get("X-Mode") != "" {
		t.Fatalf("omitted X-Mode = %q", gotHeader.Get("X-Mode"))
	}
	if _, ok := gotQuery["label"]; ok || len(gotQuery["note"]) > 0 {
		t.Fatalf("omitted label/note were sent: %v", gotQuery)
	}

	call(t, search.Handler, map[string]any{
		"resource_subtype":  "task",
		"include_completed": false,
		"opt_count":         0,
		"active":            false,
		"X-Mode":            "compact",
	})
	if got := gotQuery["resource_subtype"]; len(got) != 1 || got[0] != "task" {
		t.Fatalf("resource_subtype = %v", gotQuery["resource_subtype"])
	}
	if got := gotQuery["include_completed"]; len(got) != 1 || got[0] != "false" {
		t.Fatalf("include_completed = %v", gotQuery["include_completed"])
	}
	if got := gotQuery["opt_count"]; len(got) != 1 || got[0] != "0" {
		t.Fatalf("opt_count = %v", gotQuery["opt_count"])
	}
	if got := gotQuery["active"]; len(got) != 1 || got[0] != "false" {
		t.Fatalf("active = %v", gotQuery["active"])
	}
	if gotHeader.Get("X-Mode") != "compact" {
		t.Fatalf("X-Mode = %q", gotHeader.Get("X-Mode"))
	}

	list := mustTool(t, tools, "items_list")
	call(t, list.Handler, map[string]any{})
	if gotPath != "/items" {
		t.Fatalf("list path = %s", gotPath)
	}
	if _, ok := gotQuery["resource_subtype"]; ok || len(gotQuery["limit"]) > 0 {
		t.Fatalf("omitted list filters were sent: %v", gotQuery)
	}
	call(t, list.Handler, map[string]any{"resource_subtype": "task", "limit": 0})
	if got := gotQuery["resource_subtype"]; len(got) != 1 || got[0] != "task" {
		t.Fatalf("list resource_subtype = %v", gotQuery["resource_subtype"])
	}
	if got := gotQuery["limit"]; len(got) != 1 || got[0] != "0" {
		t.Fatalf("limit = %v", gotQuery["limit"])
	}

	create := mustTool(t, tools, "items_create")
	call(t, create.Handler, map[string]any{})
	if gotMethod != http.MethodPost {
		t.Fatalf("create method = %s", gotMethod)
	}
	body := decodeBody(t, gotBody)
	if _, ok := body["resource_subtype"]; ok {
		t.Fatalf("omitted body resource_subtype = %#v", body["resource_subtype"])
	}
	if _, ok := body["count"]; ok {
		t.Fatalf("omitted body count = %#v", body["count"])
	}
	if _, ok := body["settings"]; ok {
		t.Fatalf("omitted settings = %#v", body["settings"])
	}
	call(t, create.Handler, map[string]any{"name": "widget", "resource_subtype": "approval", "count": 0, "settings-mode": "compact"})
	body = decodeBody(t, gotBody)
	if body["name"] != "widget" || body["resource_subtype"] != "approval" {
		t.Fatalf("explicit body = %#v", body)
	}
	switch count := body["count"].(type) {
	case float64:
		if count != 0 {
			t.Fatalf("count = %v", count)
		}
	default:
		t.Fatalf("count = %#v", body["count"])
	}
	settings, ok := body["settings"].(map[string]any)
	if !ok || settings["mode"] != "compact" || settings["kind"] != "box" {
		t.Fatalf("settings = %#v", body["settings"])
	}
	layout, _ := settings["layout"].(map[string]any)
	if layout["kind"] != "box" {
		t.Fatalf("layout = %#v", settings["layout"])
	}
	call(t, create.Handler, map[string]any{"settings-kind": "crate"})
	body = decodeBody(t, gotBody)
	settings, ok = body["settings"].(map[string]any)
	if !ok || settings["kind"] != "crate" {
		t.Fatalf("explicit kind settings = %#v", body["settings"])
	}
	layout, _ = settings["layout"].(map[string]any)
	if layout["kind"] != "box" {
		t.Fatalf("explicit kind layout = %#v", settings["layout"])
	}
	if _, hasMode := settings["mode"]; hasMode {
		t.Fatalf("explicit kind included mode = %#v", settings)
	}
}

func mustTool[T any](t *testing.T, tools map[string]T, name string) T {
	t.Helper()
	tool, ok := tools[name]
	if !ok {
		names := make([]string, 0, len(tools))
		for n := range tools {
			names = append(names, n)
		}
		t.Fatalf("missing tool %s in %v", name, names)
	}
	return tool
}

func call(t *testing.T, handler func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error), args map[string]any) {
	t.Helper()
	result, err := handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: args}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("handler result=%#v err=%v", result, err)
	}
}

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %s: %v", raw, err)
	}
	if body == nil {
		return map[string]any{}
	}
	return body
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "server_default_runtime_test.go"), []byte(mcpRuntime), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "TestOpenAPIOptionalDefaultsStayOffMCPWire", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

type capturedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

func decodeObjectBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), "body %s", raw)
	return body
}

func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%v", t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		return fmt.Sprint(v)
	}
}

func resourceNames(api *spec.APISpec) string {
	names := make([]string, 0, len(api.Resources))
	for name := range api.Resources {
		names = append(names, name)
	}
	return strings.Join(names, ",")
}

func endpointNames(resource spec.Resource) string {
	names := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		names = append(names, name)
	}
	return strings.Join(names, ",")
}

const omitDefaultsSpec = `openapi: 3.0.3
info:
  title: Omit Defaults
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items/search:
    get:
      operationId: searchItems
      tags: [items]
      parameters:
        - name: resource_subtype
          in: query
          description: Subtype filter
          schema:
            type: string
            enum: [milestone, task, approval]
            default: milestone
        - name: include_completed
          in: query
          description: Include completed items
          schema:
            type: boolean
            default: true
        - name: opt_count
          in: query
          description: Optional count
          schema:
            type: integer
            default: 5
        - name: active
          in: query
          description: Active only
          schema:
            type: boolean
            default: false
        - name: view
          in: query
          required: true
          description: Response view
          schema:
            type: string
            default: summary
        - name: X-Mode
          in: header
          description: Response mode
          schema:
            type: string
            default: full
        - name: X-Api-Version
          in: header
          required: true
          description: API version
          schema:
            type: string
            default: "2026-04-01"
        - name: label
          in: query
          description: Label
          schema:
            type: string
            default: 'say "hi"'
        - name: note
          in: query
          description: Note
          schema:
            type: string
            default: nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
  /items:
    get:
      operationId: listItems
      tags: [items]
      parameters:
        - name: resource_subtype
          in: query
          description: Subtype filter
          schema:
            type: string
            default: milestone
        - name: limit
          in: query
          description: Page size
          schema:
            type: integer
            default: 25
        - name: cursor
          in: query
          description: Pagination cursor
          schema:
            type: string
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                  next_cursor:
                    type: string
    post:
      operationId: createItem
      tags: [items]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name:
                  type: string
                  description: Item name
                  default: untitled
                resource_subtype:
                  type: string
                  description: Subtype
                  default: milestone
                count:
                  type: integer
                  description: Count
                  default: 5
                settings:
                  type: object
                  description: Settings
                  properties:
                    mode:
                      type: string
                      description: Mode
                      default: compact
                    kind:
                      type: string
                      description: Kind
                      default: box
                    layout:
                      type: string
                      format: json
                      description: Layout
                      default: '{"kind":"box"}'
                  required: [kind, layout]
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
`
