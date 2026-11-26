package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedTotalOnlyPageReportsHasMore(t *testing.T) {
	t.Parallel()

	apiSpec := totalPageSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	modulePath := naming.CLI(apiSpec.Name)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "total_page_behavior_test.go"), []byte(totalPageCLITest()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "total_page_mcp_test.go"), []byte(totalPageMCPTest(modulePath)), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "./internal/mcp", "-run", "TestTotalPage", "-count=1")
}

func totalPageSpec() *spec.APISpec {
	apiSpec := minimalSpec("totalpage")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	apiSpec.Resources = map[string]spec.Resource{
		"offsets": totalPageResource("Offset lists", "/offsets", "offset", "offset", "limit", ""),
		"pages":   totalPageResource("Page lists", "/pages", "page", "page", "per_page", ""),
		"plain":   totalPageResource("Lists with no total", "/plain", "offset", "offset", "limit", ""),
		"halted":  totalPageResource("Lists whose has-more field says stop", "/halted", "offset", "offset", "limit", "has_more"),
		"linked":  totalPageResource("Lists with a next link and a total", "/linked", "offset", "offset", "limit", ""),
		"tokens":  totalPageResource("Lists with an explicit next offset", "/tokens", "offset", "offset", "limit", "next_offset"),
		"cursors": totalPageResource("Cursor lists that also report a total", "/cursors", "cursor", "pageCursor", "", "next_cursor"),
		"stalled": totalPageResource("Cursor lists with a total and no next cursor", "/stalled", "cursor", "pageCursor", "", "next_cursor"),
	}
	return apiSpec
}

func totalPageResource(description, path, paginationType, cursor, limit, hasMoreOrNext string) spec.Resource {
	return spec.Resource{
		Description: description,
		Endpoints: map[string]spec.Endpoint{
			"list": totalPageEndpoint(path, paginationType, cursor, limit, hasMoreOrNext),
			"get": {
				Method:      "GET",
				Path:        path + "/{id}",
				Description: "Get one " + path,
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}
}

// hasMoreOrNext is a has-more field when it is "has_more", and a next-cursor
// path for any other non-empty value.
func totalPageEndpoint(path, paginationType, cursor, limit, hasMoreOrNext string) spec.Endpoint {
	pag := &spec.Pagination{Type: paginationType, CursorParam: cursor, LimitParam: limit}
	paramType := "integer"
	if paginationType == "cursor" {
		paramType = "string"
	}
	params := []spec.Param{{Name: cursor, Type: paramType}}
	if limit != "" {
		params = append(params, spec.Param{Name: limit, Type: "integer"})
	}
	switch hasMoreOrNext {
	case "":
	case "has_more":
		pag.HasMoreField = "has_more"
	default:
		pag.NextCursorPath = hasMoreOrNext
	}
	return spec.Endpoint{
		Method:       "GET",
		Path:         path,
		Description:  "List " + path,
		Response:     spec.ResponseDef{Type: "array"},
		ResponsePath: "results",
		Params:       params,
		Pagination:   pag,
	}
}

func totalPageCLITest() string {
	return `package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestTotalPageCLIReportsHasMore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(totalPageHandler))
	t.Cleanup(server.Close)
	t.Setenv("TOTALPAGE_BASE_URL", server.URL)

	firstOut, firstErr := runTotalPage(t, "offsets", "list", "--limit", "20", "--json")
	first := totalPageMeta(t, firstOut)
	if first["has_more"] != true || first["next_cursor"] != "20" || first["next_cursor_flag"] != "--offset" {
		t.Fatalf("offset meta = %#v\n%s", first, firstOut)
	}
	if !strings.Contains(firstOut, "o0") || strings.Contains(firstOut, "o20") {
		t.Fatalf("offset items = %s", firstOut)
	}
	if !strings.Contains(firstErr, "--offset <meta.next_cursor>") {
		t.Fatalf("offset stderr = %s", firstErr)
	}

	secondOut, _ := runTotalPage(t, "offsets", "list", "--offset", "20", "--limit", "20", "--json")
	second := totalPageMeta(t, secondOut)
	if second["has_more"] != true || second["next_cursor"] != "40" || !strings.Contains(secondOut, "o20") || strings.Contains(secondOut, "o0") {
		t.Fatalf("second offset = %s meta %#v", secondOut, second)
	}

	lastOut, lastErr := runTotalPage(t, "offsets", "list", "--offset", "40", "--limit", "20", "--json")
	last := totalPageMeta(t, lastOut)
	if last["has_more"] != false {
		t.Fatalf("last offset meta = %#v", last)
	}
	if _, ok := last["next_cursor"]; ok {
		t.Fatalf("last offset has next_cursor: %#v", last)
	}
	if strings.Contains(lastErr, "\"event\":\"truncated\"") || !strings.Contains(lastOut, "o40") || strings.Contains(lastOut, "o39") {
		t.Fatalf("last offset = %s\nstderr = %s", lastOut, lastErr)
	}

	pageOut, _ := runTotalPage(t, "pages", "list", "--per-page", "20", "--json")
	page := totalPageMeta(t, pageOut)
	if page["has_more"] != true || page["next_cursor"] != "2" || page["next_cursor_flag"] != "--page" || !strings.Contains(pageOut, "p0") || strings.Contains(pageOut, "p20") {
		t.Fatalf("page meta = %#v\n%s", page, pageOut)
	}
	pageLastOut, pageLastErr := runTotalPage(t, "pages", "list", "--page", "3", "--per-page", "20", "--json")
	pageLast := totalPageMeta(t, pageLastOut)
	if pageLast["has_more"] != false {
		t.Fatalf("last page meta = %#v", pageLast)
	}
	if _, ok := pageLast["next_cursor"]; ok || strings.Contains(pageLastErr, "\"event\":\"truncated\"") || !strings.Contains(pageLastOut, "p40") {
		t.Fatalf("last page = %s meta %#v\nstderr = %s", pageLastOut, pageLast, pageLastErr)
	}

	plainOut, plainErr := runTotalPage(t, "plain", "list", "--limit", "20", "--json")
	plain := totalPageMeta(t, plainOut)
	if plain["has_more"] != false {
		t.Fatalf("plain meta = %#v", plain)
	}
	if _, ok := plain["next_cursor"]; ok || strings.Contains(plainErr, "\"event\":\"truncated\"") {
		t.Fatalf("plain invented a next page: %#v\n%s", plain, plainErr)
	}

	haltedOut, haltedErr := runTotalPage(t, "halted", "list", "--limit", "20", "--json")
	halted := totalPageMeta(t, haltedOut)
	if halted["has_more"] != false {
		t.Fatalf("halted meta = %#v", halted)
	}
	if _, ok := halted["next_cursor"]; ok || strings.Contains(haltedErr, "\"event\":\"truncated\"") {
		t.Fatalf("explicit has_more false lost: %#v\n%s", halted, haltedErr)
	}

	linkedOut, _ := runTotalPage(t, "linked", "list", "--limit", "20", "--json")
	linked := totalPageMeta(t, linkedOut)
	if linked["has_more"] != true || linked["next_cursor"] != "99" {
		t.Fatalf("linked meta = %#v", linked)
	}
	resumed, _ := runTotalPage(t, "linked", "list", "--offset", "99", "--limit", "20", "--json")
	if !strings.Contains(resumed, "resume-99") {
		t.Fatalf("linked resume = %s", resumed)
	}

	tokenOut, _ := runTotalPage(t, "tokens", "list", "--limit", "20", "--json")
	token := totalPageMeta(t, tokenOut)
	if token["has_more"] != true || token["next_cursor"] != "99" {
		t.Fatalf("token meta = %#v", token)
	}

	cursorOut, _ := runTotalPage(t, "cursors", "list", "--json")
	cursor := totalPageMeta(t, cursorOut)
	if cursor["has_more"] != true || cursor["next_cursor"] != "n2" {
		t.Fatalf("cursor meta = %#v", cursor)
	}
	stalledOut, _ := runTotalPage(t, "stalled", "list", "--json")
	stalled := totalPageMeta(t, stalledOut)
	if stalled["has_more"] != false {
		t.Fatalf("stalled cursor meta = %#v", stalled)
	}
	if _, ok := stalled["next_cursor"]; ok {
		t.Fatalf("cursor total invented next_cursor: %#v", stalled)
	}
}

func TestTotalPageCursorBranches(t *testing.T) {
	full := totalPageItems("o", 20)
	short := totalPageItems("o", 17)
	cases := []struct {
		name       string
		body       string
		nextPath   string
		hasMore    string
		kind       string
		cursor     string
		limit      string
		params     map[string]string
		wantNext   string
		wantSaw    bool
		wantOK     bool
	}{
		{name: "offset mid", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantNext: "20", wantSaw: true, wantOK: true},
		{name: "offset last", body: ` + "`" + `{"results":` + "`" + ` + short + ` + "`" + `,"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"offset": "40", "limit": "20"}, wantSaw: true, wantOK: true},
		{name: "offset past end", body: ` + "`" + `{"results":` + "`" + ` + short + ` + "`" + `,"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"offset": "50", "limit": "20"}, wantSaw: true, wantOK: true},
		{name: "total_count", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total_count":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantNext: "20", wantSaw: true, wantOK: true},
		{name: "page mid", body: ` + "`" + `{"results":` + "`" + ` + totalPageItems("p", 20) + ` + "`" + `,"pagination":{"total":57}}` + "`" + `, kind: "page", cursor: "page", limit: "per_page", params: map[string]string{"per_page": "20"}, wantNext: "2", wantSaw: true, wantOK: true},
		{name: "page last", body: ` + "`" + `{"results":` + "`" + ` + totalPageItems("p", 17) + ` + "`" + `,"pagination":{"total":57}}` + "`" + `, kind: "page", cursor: "page", limit: "per_page", params: map[string]string{"page": "3", "per_page": "20"}, wantSaw: true, wantOK: true},
		{name: "no total", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantOK: true},
		{name: "explicit stop", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57,"has_more":false}` + "`" + `, hasMore: "has_more", kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantSaw: true, wantOK: true},
		{name: "explicit continue", body: ` + "`" + `{"results":` + "`" + ` + short + ` + "`" + `,"total":57,"has_more":true}` + "`" + `, hasMore: "has_more", kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"offset": "40", "limit": "20"}, wantNext: "60", wantSaw: true, wantOK: true},
		{name: "next link wins", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57,"next":"https://example.test/offsets?offset=99"}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantNext: "99", wantSaw: true, wantOK: true},
		{name: "next token wins", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57,"next_offset":"99"}` + "`" + `, nextPath: "next_offset", kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantNext: "99", wantSaw: true, wantOK: true},
		{name: "non-numeric offset", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"offset": "nope", "limit": "20"}, wantSaw: true, wantOK: true},
		{name: "negative offset", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"offset": "-1", "limit": "20"}, wantSaw: true, wantOK: true},
		{name: "empty page", body: ` + "`" + `{"results":[],"total":57}` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantSaw: true, wantOK: true},
		{name: "cursor token kept", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57,"next_cursor":"abc"}` + "`" + `, nextPath: "next_cursor", kind: "cursor", cursor: "pageCursor", wantNext: "abc", wantSaw: true, wantOK: true},
		{name: "cursor total ignored", body: ` + "`" + `{"results":` + "`" + ` + full + ` + "`" + `,"total":57}` + "`" + `, nextPath: "next_cursor", kind: "cursor", cursor: "pageCursor", wantSaw: true, wantOK: true},
		{name: "malformed", body: ` + "`" + `not-json` + "`" + `},
		{name: "bare array", body: ` + "`" + `[{"id":"a"}]` + "`" + `, kind: "offset", cursor: "offset", limit: "limit", params: map[string]string{"limit": "20"}, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, saw, ok := SinglePageNextCursor(json.RawMessage(tc.body), tc.nextPath, tc.hasMore, tc.kind, tc.cursor, tc.limit, tc.params, "/items")
			if ok != tc.wantOK || saw != tc.wantSaw || next != tc.wantNext {
				t.Fatalf("next %q saw %v ok %v, want next %q saw %v ok %v", next, saw, ok, tc.wantNext, tc.wantSaw, tc.wantOK)
			}
		})
	}
}

func totalPageItems(prefix string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("{\"id\":\"%s%d\"}", prefix, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func totalPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	switch r.URL.Path {
	case "/offsets":
		totalPageWriteSlice(w, "o", 57, totalPageIndex(q.Get("offset"), 0), totalPageIndex(q.Get("limit"), 20), true, nil)
	case "/pages":
		page := totalPageIndex(q.Get("page"), 1)
		size := totalPageIndex(q.Get("per_page"), 20)
		if page < 1 {
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		totalPageWriteSlice(w, "p", 57, (page-1)*size, size, true, map[string]any{"pagination": true})
	case "/plain":
		if q.Get("offset") != "" && q.Get("offset") != "0" {
			http.Error(w, "plain has one page", http.StatusBadRequest)
			return
		}
		totalPageWriteSlice(w, "n", 20, 0, 20, false, nil)
	case "/halted":
		totalPageWriteSlice(w, "h", 57, 0, 20, true, map[string]any{"has_more": false})
	case "/linked":
		offset := q.Get("offset")
		if offset == "" || offset == "0" {
			totalPageWriteSlice(w, "l", 57, 0, 20, true, map[string]any{"next": "https://example.test/linked?offset=99"})
			return
		}
		if offset != "99" {
			http.Error(w, "linked offset "+offset, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"id": "resume-99"}}, "total": 57})
	case "/tokens":
		if q.Get("offset") != "" && q.Get("offset") != "0" {
			http.Error(w, "token offset", http.StatusBadRequest)
			return
		}
		totalPageWriteSlice(w, "t", 57, 0, 20, true, map[string]any{"next_offset": "99"})
	case "/cursors":
		if q.Get("pageCursor") == "n2" {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"id": "c2"}}, "total": 57})
			return
		}
		if q.Get("pageCursor") != "" {
			http.Error(w, "bad cursor", http.StatusBadRequest)
			return
		}
		totalPageWriteSlice(w, "c", 57, 0, 20, true, map[string]any{"next_cursor": "n2"})
	case "/stalled":
		totalPageWriteSlice(w, "s", 57, 0, 20, true, nil)
	default:
		http.NotFound(w, r)
	}
}

func totalPageWriteSlice(w http.ResponseWriter, prefix string, total, start, size int, withTotal bool, extra map[string]any) {
	if start < 0 || size < 0 || start > total {
		http.Error(w, "bad window", http.StatusBadRequest)
		return
	}
	end := start + size
	if end > total {
		end = total
	}
	items := make([]map[string]string, 0, end-start)
	for i := start; i < end; i++ {
		items = append(items, map[string]string{"id": fmt.Sprintf("%s%d", prefix, i)})
	}
	body := map[string]any{"results": items}
	if withTotal {
		if extra != nil && extra["pagination"] == true {
			body["pagination"] = map[string]any{"total": total}
			delete(extra, "pagination")
		} else {
			body["total"] = total
		}
	}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

func totalPageIndex(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func runTotalPage(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var flags rootFlags
	root := newRootCmd(&flags)
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"--home", t.TempDir()}, args...))
	var execErr error
	stderr := captureTotalPageStderr(t, func() {
		execErr = root.Execute()
	})
	if execErr != nil {
		t.Fatalf("%v: %v\nstderr: %s\nstdout: %s", args, execErr, stderr, stdout.String())
	}
	return stdout.String(), stderr
}

func captureTotalPageStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = write
	done := make(chan string, 1)
	go func() {
		buf := &bytes.Buffer{}
		_, _ = io.Copy(buf, read)
		done <- buf.String()
	}()
	fn()
	_ = write.Close()
	os.Stderr = old
	return <-done
}

func totalPageMeta(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope struct {
		Meta map[string]any ` + "`" + `json:"meta"` + "`" + `
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("unmarshal meta: %v\n%s", err, stdout)
	}
	if envelope.Meta == nil {
		t.Fatalf("missing meta: %s", stdout)
	}
	return envelope.Meta
}
`
}

func totalPageMCPTest(modulePath string) string {
	return `package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"` + modulePath + `/internal/mcp/bound"
)

func TestTotalPageMCPReportsHasMore(t *testing.T) {
	serverHits := httptest.NewServer(http.HandlerFunc(totalPageMCPHandler))
	t.Cleanup(serverHits.Close)
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		dir := filepath.Join(root, strings.ToLower(name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
	t.Setenv("TOTALPAGE_BASE_URL", serverHits.URL)

	s := server.NewMCPServer("totalpage", "test")
	RegisterTools(s)
	tools := s.ListTools()

	first := callTotalPage(t, tools, "offsets_list", map[string]any{"limit": float64(20)})
	if first["has_more"] != true {
		t.Fatalf("offset has_more = %#v", first["has_more"])
	}
	if upstream := totalPageUpstream(t, first); upstream != "20" {
		t.Fatalf("offset upstream = %q", upstream)
	}
	if !totalPageHasID(first, "o0") || totalPageHasID(first, "o20") {
		t.Fatalf("offset page = %#v", first["results"])
	}
	second := callTotalPage(t, tools, "offsets_list", map[string]any{"limit": float64(20), "cursor": first["next_cursor"]})
	if second["has_more"] != true || totalPageUpstream(t, second) != "40" || !totalPageHasID(second, "o20") {
		t.Fatalf("second offset = %#v", second)
	}
	last := callTotalPage(t, tools, "offsets_list", map[string]any{"limit": float64(20), "cursor": second["next_cursor"]})
	if last["has_more"] != false {
		t.Fatalf("last offset has_more = %#v", last["has_more"])
	}
	if _, ok := last["next_cursor"]; ok || !totalPageHasID(last, "o40") {
		t.Fatalf("last offset = %#v", last)
	}

	page := callTotalPage(t, tools, "pages_list", map[string]any{"per_page": float64(20)})
	if page["has_more"] != true || totalPageUpstream(t, page) != "2" || !totalPageHasID(page, "p0") {
		t.Fatalf("page = %#v", page)
	}
	mid := callTotalPage(t, tools, "pages_list", map[string]any{"per_page": float64(20), "cursor": page["next_cursor"]})
	if mid["has_more"] != true || totalPageUpstream(t, mid) != "3" || !totalPageHasID(mid, "p20") {
		t.Fatalf("mid page = %#v", mid)
	}
	pageLast := callTotalPage(t, tools, "pages_list", map[string]any{"per_page": float64(20), "cursor": mid["next_cursor"]})
	if pageLast["has_more"] != false || !totalPageHasID(pageLast, "p40") {
		t.Fatalf("last page = %#v", pageLast)
	}
	if _, ok := pageLast["next_cursor"]; ok {
		t.Fatalf("last page cursor = %#v", pageLast["next_cursor"])
	}

	plain := callTotalPage(t, tools, "plain_list", map[string]any{"limit": float64(20)})
	if _, ok := plain["has_more"]; ok {
		t.Fatalf("plain invented has_more: %#v", plain["has_more"])
	}
	if _, ok := plain["next_cursor"]; ok {
		t.Fatalf("plain invented next_cursor")
	}

	halted := callTotalPage(t, tools, "halted_list", map[string]any{"limit": float64(20)})
	if halted["has_more"] != false {
		t.Fatalf("halted has_more = %#v", halted["has_more"])
	}
	if _, ok := halted["next_cursor"]; ok {
		t.Fatalf("halted next_cursor = %#v", halted["next_cursor"])
	}

	linked := callTotalPage(t, tools, "linked_list", map[string]any{"limit": float64(20)})
	if linked["has_more"] != true || totalPageUpstream(t, linked) != "99" {
		t.Fatalf("linked = %#v upstream %s", linked["has_more"], totalPageUpstream(t, linked))
	}
	resumed := callTotalPage(t, tools, "linked_list", map[string]any{"limit": float64(20), "cursor": linked["next_cursor"]})
	if !totalPageHasID(resumed, "resume-99") {
		t.Fatalf("linked resume = %#v", resumed["results"])
	}

	token := callTotalPage(t, tools, "tokens_list", map[string]any{"limit": float64(20)})
	if token["has_more"] != true || totalPageUpstream(t, token) != "99" {
		t.Fatalf("token upstream = %q has_more %#v", totalPageUpstream(t, token), token["has_more"])
	}

	cursor := callTotalPage(t, tools, "cursors_list", nil)
	if _, ok := cursor["has_more"]; ok {
		t.Fatalf("cursor list invented has_more: %#v", cursor["has_more"])
	}
	if totalPageUpstream(t, cursor) != "n2" {
		t.Fatalf("cursor upstream = %q", totalPageUpstream(t, cursor))
	}
	stalled := callTotalPage(t, tools, "stalled_list", nil)
	if _, ok := stalled["has_more"]; ok || stalled["next_cursor"] != nil {
		t.Fatalf("stalled cursor = %#v", stalled)
	}
}

func callTotalPage(t *testing.T, tools map[string]*server.ServerTool, name string, args map[string]any) map[string]any {
	t.Helper()
	tool, ok := tools[name]
	if !ok {
		t.Fatalf("missing tool %s", name)
	}
	result, err := tool.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := totalPageToolText(t, result)
	if result.IsError {
		t.Fatalf("%s tool error: %s", name, text)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("%s decode: %v\n%s", name, err, text)
	}
	if data, ok := obj["data"].(map[string]any); ok {
		if _, wrapped := obj["meta"]; wrapped {
			return data
		}
	}
	return obj
}

func totalPageToolText(t *testing.T, result *mcplib.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("empty tool result")
	}
	text, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content %#v", result.Content[0])
	}
	return text.Text
}

func totalPageUpstream(t *testing.T, page map[string]any) string {
	t.Helper()
	raw, _ := page["next_cursor"].(string)
	if raw == "" {
		t.Fatalf("missing next_cursor: %#v", page)
	}
	upstream, err := bound.UpstreamCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	return upstream
}

func totalPageHasID(page map[string]any, id string) bool {
	items, _ := page["results"].([]any)
	for _, item := range items {
		obj, _ := item.(map[string]any)
		if obj["id"] == id {
			return true
		}
	}
	return false
}

func totalPageMCPHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	switch r.URL.Path {
	case "/offsets":
		totalPageMCPSlice(w, "o", 57, totalPageMCPIndex(q.Get("offset"), 0), totalPageMCPIndex(q.Get("limit"), 20), true, nil)
	case "/pages":
		page := totalPageMCPIndex(q.Get("page"), 1)
		size := totalPageMCPIndex(q.Get("per_page"), 20)
		if page < 1 {
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		totalPageMCPSlice(w, "p", 57, (page-1)*size, size, true, map[string]any{"pagination": true})
	case "/plain":
		if q.Get("offset") != "" && q.Get("offset") != "0" {
			http.Error(w, "plain has one page", http.StatusBadRequest)
			return
		}
		totalPageMCPSlice(w, "n", 20, 0, 20, false, nil)
	case "/halted":
		totalPageMCPSlice(w, "h", 57, 0, 20, true, map[string]any{"has_more": false})
	case "/linked":
		offset := q.Get("offset")
		if offset == "" || offset == "0" {
			totalPageMCPSlice(w, "l", 57, 0, 20, true, map[string]any{"next": "https://example.test/linked?offset=99"})
			return
		}
		if offset != "99" {
			http.Error(w, "linked offset "+offset, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"id": "resume-99"}}, "total": 57})
	case "/tokens":
		totalPageMCPSlice(w, "t", 57, 0, 20, true, map[string]any{"next_offset": "99"})
	case "/cursors":
		if q.Get("pageCursor") != "" && q.Get("pageCursor") != "n2" {
			http.Error(w, "bad cursor", http.StatusBadRequest)
			return
		}
		extra := map[string]any{}
		if q.Get("pageCursor") == "" {
			extra["next_cursor"] = "n2"
		}
		totalPageMCPSlice(w, "c", 57, 0, 20, true, extra)
	case "/stalled":
		totalPageMCPSlice(w, "s", 57, 0, 20, true, nil)
	default:
		http.NotFound(w, r)
	}
}

func totalPageMCPSlice(w http.ResponseWriter, prefix string, total, start, size int, withTotal bool, extra map[string]any) {
	if start < 0 || size < 0 || start > total {
		http.Error(w, "bad window", http.StatusBadRequest)
		return
	}
	end := start + size
	if end > total {
		end = total
	}
	items := make([]map[string]string, 0, end-start)
	for i := start; i < end; i++ {
		items = append(items, map[string]string{"id": prefix + strconvI(i)})
	}
	body := map[string]any{"results": items}
	if withTotal {
		if extra != nil && extra["pagination"] == true {
			body["pagination"] = map[string]any{"total": total}
			delete(extra, "pagination")
		} else {
			body["total"] = total
		}
	}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

func strconvI(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func totalPageMCPIndex(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
`
}
