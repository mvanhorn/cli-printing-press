package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// An unpaged GET list read through code orchestration must be resumable
// inside the list the server already returned. The local cursor stays out of
// the upstream request, and a paging profile keeps its own cursor path.
func TestCodeOrchestrationPagesUnpagedLists(t *testing.T) {
	t.Parallel()

	mut := true
	apiSpec := minimalSpec("unpagedread")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"alerts": {
			Description: "Alerts",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/alerts",
					Description: "List alerts",
					Params: []spec.Param{
						{Name: "status", In: "query", Type: "string", Description: "Status"},
					},
				},
			},
		},
		"sessions": {
			Description: "Sessions",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/sessions", Description: "List sessions"},
			},
		},
		"small": {
			Description: "Small payloads",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/small", Description: "Small list"},
				"object": {Method: "GET", Path: "/object", Description: "Small object"},
			},
		},
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/items/{id}",
					Description: "Get one item",
					Params: []spec.Param{
						{Name: "id", Type: "string", Positional: true, PathParam: true, Description: "Item ID"},
					},
				},
				"list-paged": {
					Method:      "GET",
					Path:        "/paged",
					Description: "Paged items",
					Params: []spec.Param{
						{Name: "cursor", In: "query", Type: "string", Description: "Page cursor"},
					},
					Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor", NextCursorPath: "nextCursor"},
				},
				"create": {Method: "POST", Path: "/items", Description: "Create item"},
			},
		},
		"ops": {
			Description: "Ops",
			Endpoints: map[string]spec.Endpoint{
				"reboot": {Method: "GET", Path: "/reboot", Description: "Reboot", Mutation: &mut},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "unpagedread-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runtimeTest := strings.ReplaceAll(codeOrchUnpagedRuntimeTest, "__MODULE_PATH__", generatedModulePath(t, outputDir))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_unpaged_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/bound", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestUnpaged", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

const codeOrchUnpagedRuntimeTest = `package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"__MODULE_PATH__/internal/mcp/bound"
)

type fakeAPI struct {
	server *httptest.Server
	hits   []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	api := &fakeAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.hits = append(api.hits, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/alerts", "/reboot":
			_, _ = w.Write([]byte(numberedList("a", 131)))
		case "/sessions":
			_, _ = w.Write([]byte(sessionObject(131)))
		case "/small":
			_, _ = w.Write([]byte(numberedList("s", 3)))
		case "/object":
			_, _ = w.Write([]byte(` + "`" + `{"id":"one","ok":true}` + "`" + `))
		case "/paged":
			_, _ = w.Write([]byte(numberedList("p", 131)))
		case "/items/abc":
			_, _ = w.Write([]byte(` + "`" + `{"id":"abc"}` + "`" + `))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.server.Close)
	t.Setenv("UNPAGEDREAD_BASE_URL", api.server.URL)
	return api
}

func (a *fakeAPI) assertNoCursor(t *testing.T, tokens ...string) {
	t.Helper()
	for _, hit := range a.hits {
		if strings.Contains(hit, "_pp_cursor") || strings.Contains(hit, "admin=1") || strings.Contains(hit, "smuggle") || strings.Contains(hit, "UPSTREAM") {
			t.Fatalf("upstream request carried a local cursor: %s", hit)
		}
		for _, token := range tokens {
			if token != "" && strings.Contains(hit, token) {
				t.Fatalf("upstream request carried %q: %s", token, hit)
			}
		}
	}
}

func numberedList(prefix string, n int) string {
	rows := make([]string, n)
	for i := 0; i < n; i++ {
		rows[i] = fmt.Sprintf("{\"id\":%q}", fmt.Sprintf("%s-%03d", prefix, i))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func sessionObject(n int) string {
	rows := make([]string, n)
	for i := 0; i < n; i++ {
		rows[i] = fmt.Sprintf("{\"id\":%q,\"name\":%q}", fmt.Sprintf("s-%03d", i), strings.Repeat("x", 500))
	}
	return "{\"context\":\"kept\",\"value\":[" + strings.Join(rows, ",") + "]}"
}

func callTool(t *testing.T, execute bool, id string, confirm bool, params map[string]any) (map[string]any, string, bool) {
	t.Helper()
	args := map[string]any{"endpoint_id": id}
	if params != nil {
		args["params"] = params
	}
	if confirm {
		args["confirm"] = true
	}
	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: args}}
	var result *mcplib.CallToolResult
	var err error
	if execute {
		result, err = handleCodeOrchExecute(context.Background(), req)
	} else {
		result, err = handleCodeOrchRead(context.Background(), req)
	}
	if err != nil || result == nil || len(result.Content) == 0 {
		t.Fatalf("call transport: result=%#v err=%v", result, err)
	}
	text := result.Content[0].(mcplib.TextContent).Text
	if result.IsError {
		return nil, text, true
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	return out, text, false
}

func rawCursor(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func idsOf(t *testing.T, out map[string]any, field string) []string {
	t.Helper()
	if out[field] == nil {
		return nil
	}
	raw, ok := out[field].([]any)
	if !ok {
		t.Fatalf("%s = %#v", field, out[field])
	}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item = %#v", item)
		}
		id, _ := obj["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func wantIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%03d", prefix, i)
	}
	return ids
}

func collectPages(t *testing.T, api *fakeAPI, endpoint, field string, extra map[string]any) []string {
	t.Helper()
	var ids []string
	var cursors []string
	cursor := ""
	for page := 0; page < 8; page++ {
		params := map[string]any{}
		for k, v := range extra {
			params[k] = v
		}
		if cursor != "" {
			params[bound.LocalPageCursorParam] = "  " + cursor + "  "
		}
		out, text, isErr := callTool(t, false, endpoint, false, params)
		if isErr {
			t.Fatalf("page %d failed: %s", page, text)
		}
		if field == "value" {
			if out["context"] != "kept" {
				t.Fatalf("page dropped context: %s", text)
			}
		}
		batch := idsOf(t, out, field)
		ids = append(ids, batch...)
		next, _ := out["next_cursor"].(string)
		if next == "" {
			if out["truncated"] == true {
				t.Fatalf("truncated page has no cursor: %s", text)
			}
			api.assertNoCursor(t, cursors...)
			return ids
		}
		if len(batch) == 0 {
			t.Fatalf("empty page returned a cursor: %s", text)
		}
		note, _ := out["note"].(string)
		if strings.Contains(note, "limit") || strings.Contains(note, "offset") || !strings.Contains(note, bound.LocalPageCursorParam) || !strings.Contains(note, "fetches the list again") {
			t.Fatalf("note = %q", note)
		}
		if err := bound.LocalPageCursor(next); err != nil {
			t.Fatalf("emitted cursor rejected: %v", err)
		}
		cursors = append(cursors, next, "  "+next+"  ")
		cursor = next
	}
	t.Fatal("pages did not finish")
	return nil
}

func TestUnpagedReadPagesBareList(t *testing.T) {
	api := newFakeAPI(t)
	got := collectPages(t, api, "alerts.list", "items", map[string]any{"status": "open"})
	want := wantIDs("a", 131)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v", got)
	}
	if len(api.hits) < 2 {
		t.Fatalf("expected multiple refetches, got %v", api.hits)
	}
	for _, hit := range api.hits {
		if !strings.Contains(hit, "status=open") || strings.Contains(hit, "_pp_cursor") {
			t.Fatalf("query = %s", hit)
		}
	}
}

func TestUnpagedReadPagesObjectList(t *testing.T) {
	api := newFakeAPI(t)
	got := collectPages(t, api, "sessions.list", "value", nil)
	want := wantIDs("s", 131)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v", got)
	}
	if len(api.hits) < 2 {
		t.Fatalf("expected multiple refetches, got %v", api.hits)
	}
}

func TestUnpagedReadSmallResponsesStayUnchanged(t *testing.T) {
	api := newFakeAPI(t)
	list, text, isErr := callTool(t, false, "small.list", false, nil)
	if isErr {
		t.Fatal(text)
	}
	if list["truncated"] != nil || list["next_cursor"] != nil || list["returned_count"] != nil || list["note"] != nil {
		t.Fatalf("small list changed shape: %s", text)
	}
	if got := idsOf(t, list, "items"); strings.Join(got, ",") != "s-000,s-001,s-002" {
		t.Fatalf("small list = %v", got)
	}
	if _, ok := list["count"].(float64); !ok || list["count"].(float64) != 3 {
		t.Fatalf("count = %#v", list["count"])
	}
	_, objectText, isErr := callTool(t, false, "small.object", false, nil)
	if isErr {
		t.Fatal(objectText)
	}
	if objectText != "{\"id\":\"one\",\"ok\":true}" {
		t.Fatalf("small object = %s", objectText)
	}
	api.assertNoCursor(t)
}

func TestUnpagedReadRejectsBadCursorsWithoutDialing(t *testing.T) {
	api := newFakeAPI(t)
	validTail := rawCursor("{\"v\":1,\"o\":50,\"extra\":\"ignored\"}")
	cases := []any{
		"not-a-cursor",
		rawCursor("{\"v\":1,\"o\":50,\"u\":\"smuggle\"}"),
		rawCursor("{\"v\":1,\"u\":\"UPSTREAM\"}"),
		rawCursor("{\"v\":1,\"u\":\" \"}"),
		rawCursor("{\"v\":2,\"o\":1}"),
		rawCursor("{\"v\":1,\"o\":-1}"),
		rawCursor("{\"o\":10}"),
		rawCursor("{\"v\":1,\"o\":\"50\"}"),
		rawCursor("{\"v\":1,\"o\":1e2}"),
		rawCursor("{\"v\":1,\"o\":9999999999999999999}"),
		rawCursor("null"),
		rawCursor("[]"),
		base64.StdEncoding.EncodeToString([]byte("{\"v\":1,\"o\":50}")),
		base64.URLEncoding.EncodeToString([]byte("{\"v\":1,\"u\":\"a+b/c==\"}")),
		base64.RawStdEncoding.EncodeToString([]byte("{\"v\":1,\"u\":\">>>>>>>>>>\"}")),
		rawCursor(rawCursor("{\"v\":1,\"o\":1}")),
		"SERVERNEXT",
		"ok&admin=1",
		"{\"v\":1,\"o\":50,\"u\":\"smuggle\"}",
		map[string]any{"v": 1, "o": 50, "u": "smuggle"},
		[]any{"smuggle"},
		50.0,
		true,
		"  " + rawCursor("{\"v\":1,\"o\":1,\"u\":\"smuggle\"}") + "  ",
	}
	for _, cursor := range cases {
		before := len(api.hits)
		_, text, isErr := callTool(t, false, "alerts.list", false, map[string]any{"_pp_cursor": cursor, "status": "open"})
		if !isErr {
			t.Fatalf("accepted cursor %#v: %s", cursor, text)
		}
		if strings.Contains(text, "smuggle") || strings.Contains(text, "UPSTREAM") || strings.Contains(text, "a+b/c") {
			t.Fatalf("error echoed the cursor: %s", text)
		}
		if len(api.hits) != before {
			t.Fatalf("invalid cursor dialed the API: %v", api.hits[before:])
		}
	}
	before := len(api.hits)
	out, text, isErr := callTool(t, false, "alerts.list", false, map[string]any{"_pp_cursor": validTail})
	if isErr {
		t.Fatal(text)
	}
	if ids := idsOf(t, out, "items"); len(ids) == 0 || ids[0] != "a-050" {
		t.Fatalf("extra-field cursor did not resume at a-050: %v", ids)
	}
	if len(api.hits) != before+1 {
		t.Fatalf("valid cursor hits = %v", api.hits[before:])
	}
	api.assertNoCursor(t, validTail)
}

func TestUnpagedReadPastEndStops(t *testing.T) {
	api := newFakeAPI(t)
	cursor := rawCursor("{\"v\":1,\"o\":10000}")
	out, text, isErr := callTool(t, false, "alerts.list", false, map[string]any{"_pp_cursor": cursor})
	if isErr {
		t.Fatal(text)
	}
	if ids := idsOf(t, out, "items"); len(ids) != 0 || out["next_cursor"] != nil {
		t.Fatalf("past-end page = %s", text)
	}
	api.assertNoCursor(t, cursor)
}

func TestUnpagedReadCursorDoesNotEnterPath(t *testing.T) {
	api := newFakeAPI(t)
	bad := rawCursor("{\"v\":1,\"u\":\"smuggle\"}")
	_, text, isErr := callTool(t, false, "items.get", false, map[string]any{"id": "abc", "_pp_cursor": bad})
	if !isErr {
		t.Fatalf("path read accepted an upstream cursor: %s", text)
	}
	if len(api.hits) != 0 {
		t.Fatalf("invalid path cursor dialed: %v", api.hits)
	}
	out, text, isErr := callTool(t, false, "items.get", false, map[string]any{"id": "abc", "_pp_cursor": rawCursor("{\"v\":1,\"o\":0}")})
	if isErr {
		t.Fatal(text)
	}
	if out["id"] != "abc" {
		t.Fatalf("item = %#v", out)
	}
	if len(api.hits) != 1 || api.hits[0] != "GET /items/abc" {
		t.Fatalf("path request = %v", api.hits)
	}
}

func TestUnpagedReadMutatingGetIsNotPaged(t *testing.T) {
	api := newFakeAPI(t)
	_, text, isErr := callTool(t, false, "ops.reboot", false, map[string]any{"_pp_cursor": rawCursor("{\"v\":1,\"u\":\"smuggle\"}")})
	if !isErr || !strings.Contains(text, "changes state") {
		t.Fatalf("read of mutating GET = err:%v %s", isErr, text)
	}
	if len(api.hits) != 0 {
		t.Fatalf("read dialed a mutating GET: %v", api.hits)
	}
	out, text, isErr := callTool(t, true, "ops.reboot", true, map[string]any{"_pp_cursor": "ok&admin=1"})
	if isErr {
		t.Fatal(text)
	}
	if out["next_cursor"] != nil {
		t.Fatalf("mutating GET returned a cursor: %s", text)
	}
	if out["truncated"] != true {
		t.Fatalf("mutating GET was not bounded: %s", text)
	}
	if n := len(idsOf(t, out, "items")); n == 0 || n >= 131 {
		t.Fatalf("mutating page size = %d, text %s", n, text)
	}
	if len(api.hits) != 1 || strings.Contains(api.hits[0], "_pp_cursor") || strings.Contains(api.hits[0], "admin") {
		t.Fatalf("mutating request = %v", api.hits)
	}
}

func TestUnpagedReadPagedEndpointKeepsItsCursor(t *testing.T) {
	api := newFakeAPI(t)
	first, text, isErr := callTool(t, false, "items.list-paged", false, map[string]any{"_pp_cursor": "!!!"})
	if isErr {
		t.Fatal(text)
	}
	next, _ := first["next_cursor"].(string)
	if next == "" {
		t.Fatalf("paged list missing next_cursor: %s", text)
	}
	second, text, isErr := callTool(t, false, "items.list-paged", false, map[string]any{"cursor": next, "_pp_cursor": "smuggle"})
	if isErr {
		t.Fatal(text)
	}
	if ids := idsOf(t, second, "items"); len(ids) == 0 || ids[0] == "p-000" {
		t.Fatalf("paged continuation did not advance: %v", ids)
	}
	raw, text, isErr := callTool(t, false, "items.list-paged", false, map[string]any{"cursor": "PAGE2", "_pp_cursor": rawCursor("{\"v\":1,\"u\":\"UPSTREAM\"}")})
	if isErr {
		t.Fatal(text)
	}
	if raw["next_cursor"] == nil && raw["items"] == nil && raw["preview"] == nil {
		t.Fatalf("raw cursor response = %s", text)
	}
	for _, hit := range api.hits {
		if strings.Contains(hit, "_pp_cursor") || strings.Contains(hit, "smuggle") || strings.Contains(hit, "UPSTREAM") || strings.Contains(hit, "!!!") {
			t.Fatalf("paged request carried the local cursor: %s", hit)
		}
	}
	var sawRaw bool
	for _, hit := range api.hits {
		if strings.Contains(hit, "cursor=PAGE2") {
			sawRaw = true
		}
	}
	if !sawRaw {
		t.Fatalf("paged endpoint did not send the raw upstream cursor: %v", api.hits)
	}
}

func TestUnpagedReadDropsCursorFromWritePreview(t *testing.T) {
	api := newFakeAPI(t)
	out, text, isErr := callTool(t, true, "items.create", false, map[string]any{"name": "widget", "_pp_cursor": "ok&admin=1"})
	if isErr {
		t.Fatal(text)
	}
	if out["preview"] != true {
		t.Fatalf("write preview = %s", text)
	}
	body, _ := out["body"].(map[string]any)
	if body["_pp_cursor"] != nil || body["name"] != "widget" {
		t.Fatalf("preview body = %#v", body)
	}
	query, _ := out["query"].(map[string]any)
	if query["_pp_cursor"] != nil {
		t.Fatalf("preview query = %#v", query)
	}
	if len(api.hits) != 0 {
		t.Fatalf("preview dialed: %v", api.hits)
	}
}
`
