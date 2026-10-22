package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// An over-budget GET list read through the code-orchestration surface must
// resume inside the bounded page. Handing back the server's next-page cursor
// would skip every row the MCP bound dropped.
func TestCodeOrchestrationPagesBoundedLists(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("corchpage")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
				Params: []spec.Param{
					{Name: "cursor", In: "query", Type: "string", Description: "Page cursor"},
				},
				Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor", NextCursorPath: "nextCursor"},
			},
			"list-offset": {
				Method:      "GET",
				Path:        "/items-by-offset",
				Description: "List items by offset",
				Params: []spec.Param{
					{Name: "offset", In: "query", Type: "integer", Description: "Rows to skip"},
				},
				Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset"},
			},
			"get": {
				Method:      "GET",
				Path:        "/items/{id}",
				Description: "Get one item",
				Params: []spec.Param{
					{Name: "id", Type: "string", Positional: true, PathParam: true, Description: "Item ID"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "corchpage-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Regexp(t, `Page:\s+mcpPageConfig\{CursorParam: "cursor", NextCursorPath: "nextCursor"\}`, src,
		"paginated GET list must carry its page config into the code-orchestration registry")
	require.Regexp(t, `if ep\.Page\.CursorParam != "" \{\s+out\["cursor_param"\] = ep\.Page\.CursorParam`, src,
		"endpoint metadata must name the params key that receives next_cursor")

	runtimeTest := `package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func codeOrchPageRead(t *testing.T, params map[string]any) map[string]any {
	t.Helper()
	result, err := handleCodeOrchRead(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "items.list", "params": params,
	}}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("read failed: result=%#v err=%v", result, err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcplib.TextContent).Text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestCodeOrchestrationPagesBoundedListsRuntime(t *testing.T) {
	var gotCursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursors = append(gotCursors, r.URL.Query().Get("cursor"))
		rows := make([]string, 0, 400)
		for i := 0; i < 400; i++ {
			rows = append(rows, fmt.Sprintf("{\"id\":\"i%03d\",\"name\":%q}", i, strings.Repeat("x", 200)))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"data\":[" + strings.Join(rows, ",") + "],\"nextCursor\":\"SERVERNEXT\"}"))
	}))
	defer server.Close()
	t.Setenv("CORCHPAGE_BASE_URL", server.URL)

	first := codeOrchPageRead(t, map[string]any{})
	next, _ := first["next_cursor"].(string)
	if next == "" || next == "SERVERNEXT" {
		t.Fatalf("bounded page must return a local continuation cursor, got %q", next)
	}
	second := codeOrchPageRead(t, map[string]any{"cursor": next})
	rows, _ := second["data"].([]any)
	if len(rows) == 0 {
		t.Fatalf("continuation returned no rows: %v", second)
	}
	firstRows, _ := first["data"].([]any)
	if got := rows[0].(map[string]any)["id"]; got != fmt.Sprintf("i%03d", len(firstRows)) {
		t.Fatalf("continuation starts at %v, want the first omitted row i%03d", got, len(firstRows))
	}
	if gotCursors[1] != "" {
		t.Fatalf("resuming the first page must not send an upstream cursor, sent %q", gotCursors[1])
	}

	// A raw upstream cursor goes to the API unchanged and anchors the continuation to that page.
	raw := codeOrchPageRead(t, map[string]any{"cursor": "RAWPAGE2"})
	if gotCursors[2] != "RAWPAGE2" {
		t.Fatalf("raw cursor sent as %q, want RAWPAGE2", gotCursors[2])
	}
	cont, _ := raw["next_cursor"].(string)
	codeOrchPageRead(t, map[string]any{"cursor": cont})
	if gotCursors[3] != "RAWPAGE2" {
		t.Fatalf("continuation of a raw-cursor page re-requested %q, want RAWPAGE2", gotCursors[3])
	}
}

func TestCodeOrchestrationKeepsNumericStartingOffset(t *testing.T) {
	var gotOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOffsets = append(gotOffsets, r.URL.Query().Get("offset"))
		rows := make([]string, 0, 400)
		for i := 0; i < 400; i++ {
			rows = append(rows, fmt.Sprintf("{\"id\":\"i%03d\",\"name\":%q}", i, strings.Repeat("x", 200)))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Join(rows, ",") + "]"))
	}))
	defer server.Close()
	t.Setenv("CORCHPAGE_BASE_URL", server.URL)

	read := func(params map[string]any) map[string]any {
		t.Helper()
		result, err := handleCodeOrchRead(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
			"endpoint_id": "items.list-offset", "params": params,
		}}})
		if err != nil || result == nil || result.IsError {
			t.Fatalf("read failed: result=%#v err=%v", result, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].(mcplib.TextContent).Text), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	first := read(map[string]any{"offset": 100.0})
	next, _ := first["next_cursor"].(string)
	if next == "" {
		t.Fatalf("bounded page must return a continuation cursor: %v", first)
	}
	read(map[string]any{"offset": next})
	if len(gotOffsets) != 2 || gotOffsets[0] != "100" || gotOffsets[1] != "100" {
		t.Fatalf("continuation must re-request the numeric starting offset, sent %q", gotOffsets)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_page_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^(TestCodeOrchestrationPagesBoundedListsRuntime|TestCodeOrchestrationKeepsNumericStartingOffset)$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
