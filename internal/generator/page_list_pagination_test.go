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

func TestGenerateRejectsUndeterminablePageRequestParam(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pagegap-missing")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	cursorPag := &spec.Pagination{Type: "cursor", NextCursorPath: "cursor"}
	pagePag := &spec.Pagination{Type: "page", LimitParam: "limit"}
	apiSpec.Resources = map[string]spec.Resource{
		"rows": {
			Description: "Rows",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/rows",
					Description: "List rows",
					Pagination:  cursorPag,
					Params:      []spec.Param{{Name: "cursor", Type: "string"}},
				},
			},
		},
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
					Pagination:  pagePag,
					Params:      []spec.Param{{Name: "limit", Type: "integer"}},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "out")
	err := New(apiSpec, outputDir).Generate()
	require.ErrorContains(t, err, `resource "widgets" endpoint "list"`)
	require.ErrorContains(t, err, "no request parameter")
	require.ErrorContains(t, err, "cursor_param")
	assert.Empty(t, pagePag.CursorParam)
	assert.Empty(t, cursorPag.CursorParam)
	_, statErr := os.Stat(outputDir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestPaginationCursorFlagOmitsUndeclaredParam(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "--page-cursor", paginationCursorFlag(spec.Endpoint{
		Pagination: &spec.Pagination{CursorParam: "pageCursor"},
		Params:     []spec.Param{{Name: "pageCursor", Type: "string"}},
	}))
	assert.Empty(t, paginationCursorFlag(spec.Endpoint{
		Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor"},
	}))
	assert.Empty(t, paginationCursorFlag(spec.Endpoint{
		Pagination: &spec.Pagination{CursorParam: "cursor"},
		Params:     []spec.Param{{Name: "cursor", Type: "string", In: "header"}},
	}))
	assert.Empty(t, paginationCursorFlag(spec.Endpoint{
		Pagination: &spec.Pagination{CursorParam: "cursor"},
		Params:     []spec.Param{{Name: "cursor", Type: "string", Positional: true}},
	}))
}

func TestGeneratedPageListWalksAndKeepsMeta(t *testing.T) {
	t.Parallel()

	apiSpec := pageListSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	assert.Equal(t, "page", apiSpec.Resources["widgets"].Endpoints["list"].Pagination.CursorParam)
	assert.Equal(t, "pageCursor", apiSpec.Resources["cursors"].Endpoints["list"].Pagination.CursorParam)
	assert.Equal(t, "p", apiSpec.Resources["kept"].Endpoints["list"].Pagination.CursorParam)
	assert.Empty(t, apiSpec.Resources["skips"].Endpoints["list"].Pagination.CursorParam)
	assert.Nil(t, apiSpec.Resources["notes"].Endpoints["list"].Pagination)

	widgetsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "widgets_list.go")
	cursorsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "cursors_list.go")
	notesSrc := readGeneratedFile(t, outputDir, "internal", "cli", "notes_list.go")
	assert.Contains(t, widgetsSrc, `listPageContext(cmd.Context(), "--page")`)
	assert.Contains(t, cursorsSrc, `listPageContext(cmd.Context(), "--page-cursor")`)
	assert.NotContains(t, notesSrc, "listPageContext")
	assertPaginatedStoreCursorSlot(t, widgetsSrc, "page", "page")
	assertPaginatedStoreCursorSlot(t, cursorsSrc, "pageCursor", "cursor")
	assertPaginatedStoreCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "kept_list.go"), "p", "page")
	assertPaginatedStoreCursorSlot(t, readGeneratedFile(t, outputDir, "internal", "cli", "skips_list.go"), "", "offset")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "page_list_behavior_test.go"), []byte(pageListBehaviorTest(naming.CLI(apiSpec.Name))), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestPageList", "-count=1")
}

func assertPaginatedStoreCursorSlot(t *testing.T, src, cursorParam, paginationType string) {
	t.Helper()
	want := fmt.Sprintf("flagAll, %q, %q", cursorParam, paginationType)
	assert.Contains(t, strings.Join(strings.Fields(src), " "), want)
}

func pageListSpec() *spec.APISpec {
	apiSpec := minimalSpec("pagegap")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	getID := []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}}
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/widgets",
					Description:  "List widgets",
					Response:     spec.ResponseDef{Type: "array"},
					ResponsePath: "results",
					Params: []spec.Param{
						{Name: "page", Type: "integer"},
						{Name: "limit", Type: "integer"},
					},
					Pagination: &spec.Pagination{Type: "page", LimitParam: "limit", NextCursorPath: "next"},
				},
				"get": {Method: "GET", Path: "/widgets/{id}", Description: "Get a widget", Params: getID},
			},
		},
		"cursors": {
			Description: "Cursors",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/cursors",
					Description:  "List cursors",
					Response:     spec.ResponseDef{Type: "array"},
					ResponsePath: "results",
					Params:       []spec.Param{{Name: "pageCursor", Type: "string"}},
					Pagination:   &spec.Pagination{Type: "cursor", CursorParam: "pageCursor", NextCursorPath: "nextPageCursor"},
				},
				"get": {Method: "GET", Path: "/cursors/{id}", Description: "Get a cursor row", Params: getID},
			},
		},
		"notes": {
			Description: "Notes",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/notes",
					Description: "List notes",
					Response:    spec.ResponseDef{Type: "array"},
				},
				"get": {Method: "GET", Path: "/notes/{id}", Description: "Get a note", Params: getID},
			},
		},
		"kept": {
			Description: "Kept",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/kept",
					Description: "List kept",
					Response:    spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "p", Type: "integer"},
						{Name: "page", Type: "integer"},
					},
					Pagination: &spec.Pagination{Type: "page", CursorParam: "p"},
				},
				"get": {Method: "GET", Path: "/kept/{id}", Description: "Get a kept row", Params: getID},
			},
		},
		"skips": {
			Description: "Skips",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/skips",
					Description: "List skips",
					Response:    spec.ResponseDef{Type: "array"},
					Params:      []spec.Param{{Name: "offset", Type: "integer"}},
					Pagination:  &spec.Pagination{Type: "offset", NextCursorPath: "next_offset"},
				},
				"get": {Method: "GET", Path: "/skips/{id}", Description: "Get a skip row", Params: getID},
			},
		},
	}
	return apiSpec
}

func pageListBehaviorTest(modulePath string) string {
	return `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"` + modulePath + `/internal/platform"
)

func TestPageListWalksAndKeepsMeta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(pageListHandler))
	t.Cleanup(server.Close)
	t.Setenv("PAGEGAP_BASE_URL", server.URL)

	allOut, allErr := runPageList(t, "widgets", "list", "--all", "--json")
	if !strings.Contains(allErr, ` + "`" + `{"event":"complete","total":5,"pages":3}` + "`" + `) {
		t.Fatalf("all stderr = %s\nstdout = %s", allErr, allOut)
	}
	if strings.Contains(allErr, "pagination_cursor_missing") || strings.Contains(allErr, ` + "`" + `"event":"truncated"` + "`" + `) {
		t.Fatalf("all stderr truncated: %s", allErr)
	}
	for _, id := range []string{"w1", "w2", "w3", "w4", "w5"} {
		if !strings.Contains(allOut, id) {
			t.Fatalf("all output missing %s: %s", id, allOut)
		}
	}
	allMeta := pageListMeta(t, allOut)
	if _, ok := allMeta["next_cursor"]; ok {
		t.Fatalf("--all meta includes next_cursor: %#v", allMeta)
	}
	if _, ok := allMeta["has_more"]; ok {
		t.Fatalf("--all meta includes has_more: %#v", allMeta)
	}

	pageOut, pageErr := runPageList(t, "widgets", "list", "--json")
	meta := pageListMeta(t, pageOut)
	if meta["has_more"] != true || meta["next_cursor"] != "2" || meta["next_cursor_flag"] != "--page" || meta["count"] != float64(5) || meta["source"] != "live" {
		t.Fatalf("page meta = %#v\nstdout = %s", meta, pageOut)
	}
	if !strings.Contains(pageOut, "w1") || !strings.Contains(pageOut, "w2") || strings.Contains(pageOut, "w3") {
		t.Fatalf("first page items = %s", pageOut)
	}
	wantHint := ` + "`" + `{"event":"truncated","hint":"pass --all to fetch every page, or --page <meta.next_cursor> for the next page"}` + "`" + `
	if !strings.Contains(pageErr, wantHint) {
		t.Fatalf("stderr hint = %s", pageErr)
	}

	secondOut, _ := runPageList(t, "widgets", "list", "--page", "2", "--json")
	second := pageListMeta(t, secondOut)
	if second["next_cursor"] != "3" || second["has_more"] != true || !strings.Contains(secondOut, "w3") || !strings.Contains(secondOut, "w4") || strings.Contains(secondOut, "w1") {
		t.Fatalf("second page = %s meta %#v", secondOut, second)
	}

	lastOut, lastErr := runPageList(t, "widgets", "list", "--page", "3", "--json")
	last := pageListMeta(t, lastOut)
	if last["has_more"] != false || last["count"] != float64(5) {
		t.Fatalf("last meta = %#v", last)
	}
	if _, ok := last["next_cursor"]; ok {
		t.Fatalf("last page has next_cursor: %#v", last)
	}
	if strings.Contains(lastErr, ` + "`" + `"event":"truncated"` + "`" + `) {
		t.Fatalf("last page stderr = %s", lastErr)
	}
	if !strings.Contains(lastOut, "w5") || strings.Contains(lastOut, "w4") {
		t.Fatalf("last page items = %s", lastOut)
	}

	selected, _ := runPageList(t, "widgets", "list", "--json", "--select", "id")
	if strings.Contains(selected, "detail-w1") || !strings.Contains(selected, "w1") {
		t.Fatalf("select output = %s", selected)
	}
	selectedMeta := pageListMeta(t, selected)
	if selectedMeta["next_cursor"] != "2" || selectedMeta["has_more"] != true {
		t.Fatalf("select meta = %#v", selectedMeta)
	}

	compact, _ := runPageList(t, "widgets", "list", "--agent")
	if strings.Contains(compact, "detail-w1") || !strings.Contains(compact, "w1") {
		t.Fatalf("compact output = %s", compact)
	}
	compactMeta := pageListMeta(t, compact)
	if compactMeta["next_cursor"] != "2" || compactMeta["next_cursor_flag"] != "--page" || compactMeta["has_more"] != true {
		t.Fatalf("compact meta = %#v", compactMeta)
	}

	cursorOut, cursorErr := runPageList(t, "cursors", "list", "--json")
	cursorMeta := pageListMeta(t, cursorOut)
	if cursorMeta["next_cursor"] != "n2" || cursorMeta["next_cursor_flag"] != "--page-cursor" || cursorMeta["has_more"] != true {
		t.Fatalf("cursor meta = %#v\n%s", cursorMeta, cursorOut)
	}
	if !strings.Contains(cursorErr, "--page-cursor <meta.next_cursor>") {
		t.Fatalf("cursor stderr = %s", cursorErr)
	}
	resumed, resumedErr := runPageList(t, "cursors", "list", "--page-cursor", "n2", "--json")
	resumedMeta := pageListMeta(t, resumed)
	if resumedMeta["has_more"] != false {
		t.Fatalf("resumed meta = %#v", resumedMeta)
	}
	if _, ok := resumedMeta["next_cursor"]; ok {
		t.Fatalf("resumed has next_cursor: %#v", resumedMeta)
	}
	if strings.Contains(resumedErr, ` + "`" + `"event":"truncated"` + "`" + `) || !strings.Contains(resumed, "c3") || strings.Contains(resumed, "c1") {
		t.Fatalf("resumed output = %s\nstderr = %s", resumed, resumedErr)
	}
	cursorAll, cursorAllErr := runPageList(t, "cursors", "list", "--all", "--json")
	for _, id := range []string{"c1", "c2", "c3"} {
		if !strings.Contains(cursorAll, id) {
			t.Fatalf("cursor --all missing %s: %s\nstderr = %s", id, cursorAll, cursorAllErr)
		}
	}
	if !strings.Contains(cursorAllErr, ` + "`" + `"event":"complete"` + "`" + `) {
		t.Fatalf("cursor --all stderr = %s", cursorAllErr)
	}

	notes, _ := runPageList(t, "notes", "list", "--json")
	notesMeta := pageListMeta(t, notes)
	if _, ok := notesMeta["has_more"]; ok {
		t.Fatalf("notes meta = %#v", notesMeta)
	}
	if _, ok := notesMeta["next_cursor"]; ok {
		t.Fatalf("notes meta = %#v", notesMeta)
	}
	if notesMeta["source"] != "live" || !strings.Contains(notes, "n1") {
		t.Fatalf("notes output = %s", notes)
	}
}

func TestPageListEmptyCursorDoesNotSilentlyComplete(t *testing.T) {
	session := pageListSession(t)
	ctx := platform.ContextWithSession(context.Background(), session)

	blocked := pageListDirect(t, ctx, ` + "`" + `{"results":[{"id":"1"}],"next":"https://example.test/items?page=2","count":5}` + "`" + `, "", "page", "next", "")
	if strings.Contains(blocked.stderr, ` + "`" + `"event":"complete"` + "`" + `) || !strings.Contains(blocked.stderr, "pagination_cursor_missing") {
		t.Fatalf("blocked stderr = %s", blocked.stderr)
	}
	meta := session.OutputMetadata()
	if !meta.Truncated || len(meta.TruncationReasons) == 0 || meta.TruncationReasons[0].Kind != "pagination_cursor_missing" {
		t.Fatalf("blocked metadata = %+v", meta)
	}

	single := pageListDirect(t, context.Background(), ` + "`" + `{"results":[{"id":"1"}],"next":null,"count":1}` + "`" + `, "", "page", "next", "")
	if !strings.Contains(single.stderr, ` + "`" + `"event":"complete"` + "`" + `) || strings.Contains(single.stderr, "pagination_cursor_missing") || strings.Contains(single.stderr, ` + "`" + `"event":"truncated"` + "`" + `) {
		t.Fatalf("single stderr = %s", single.stderr)
	}

	gap := pageListDirect(t, context.Background(), ` + "`" + `{"results":[{"id":"1"}],"count":5}` + "`" + `, "", "page", "", "")
	if strings.Contains(gap.stderr, ` + "`" + `"event":"complete"` + "`" + `) || !strings.Contains(gap.stderr, "pagination_cursor_missing") {
		t.Fatalf("gap stderr = %s", gap.stderr)
	}

	offset := pageListDirect(t, context.Background(), ` + "`" + `{"results":[{"id":"1"}],"next":"https://example.test/items?offset=2","count":5}` + "`" + `, "", "offset", "next", "")
	if !strings.Contains(offset.stderr, ` + "`" + `"event":"complete"` + "`" + `) || strings.Contains(offset.stderr, "pagination_cursor_missing") {
		t.Fatalf("offset stderr = %s", offset.stderr)
	}

	cursor := pageListDirect(t, context.Background(), ` + "`" + `{"results":[{"id":"1"}],"has_more":true}` + "`" + `, "", "cursor", "", "has_more")
	if !strings.Contains(cursor.stderr, ` + "`" + `"event":"complete"` + "`" + `) || !strings.Contains(cursor.stderr, "pagination_cursor_missing") {
		t.Fatalf("cursor stderr = %s", cursor.stderr)
	}
}

type pageListDirectResult struct {
	stderr string
}

type pageListClient struct {
	body json.RawMessage
}

func (c *pageListClient) GetWithHeaders(context.Context, string, map[string]string, map[string]string) (json.RawMessage, error) {
	return append(json.RawMessage(nil), c.body...), nil
}

func pageListDirect(t *testing.T, ctx context.Context, body, cursorParam, paginationType, nextPath, hasMore string) pageListDirectResult {
	t.Helper()
	stderr := capturePageListStderr(t, func() {
		_, err := paginatedGet(ctx, &pageListClient{body: json.RawMessage(body)}, "/items", map[string]string{}, nil, true, cursorParam, paginationType, "", 0, nextPath, hasMore)
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	return pageListDirectResult{stderr: stderr}
}

func pageListSession(t *testing.T) *platform.Session {
	t.Helper()
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
	profile := &platform.Profile{
		SchemaVersion: platform.ProfileSchemaVersion,
		Name:          "synthetic",
		Sources: map[string]platform.SourceProfile{
			"sample": {CredentialRef: "fixture://Test/sample/token", ExpectedAccountID: "one"},
		},
	}
	session, err := platform.PrepareProfileSourceFromConfig(profile, "sample-pp-cli", "sample")
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func pageListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/widgets":
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		items := []map[string]string{
			{"id": "w1", "description": "detail-w1"},
			{"id": "w2", "description": "detail-w2"},
			{"id": "w3", "description": "detail-w3"},
			{"id": "w4", "description": "detail-w4"},
			{"id": "w5", "description": "detail-w5"},
		}
		start := 0
		var next any
		switch page {
		case "1":
			start = 0
			next = "http://" + r.Host + "/widgets?page=2"
		case "2":
			start = 2
			next = "http://" + r.Host + "/widgets?page=3"
		case "3":
			start = 4
			next = nil
		default:
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		end := start + 2
		if end > len(items) {
			end = len(items)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":    5,
			"next":     next,
			"previous": nil,
			"results":  items[start:end],
		})
	case "/cursors":
		if r.URL.Query().Get("pageCursor") == "n2" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]string{{"id": "c3", "description": "detail-c3"}},
			})
			return
		}
		if r.URL.Query().Get("pageCursor") != "" {
			http.Error(w, "bad cursor", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results":        []map[string]string{{"id": "c1", "description": "detail-c1"}, {"id": "c2", "description": "detail-c2"}},
			"nextPageCursor": "n2",
		})
	case "/notes":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "n1", "description": "detail-n1"}})
	default:
		http.NotFound(w, r)
	}
}

func runPageList(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var flags rootFlags
	root := newRootCmd(&flags)
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"--home", t.TempDir()}, args...))
	var stderr string
	execErr := error(nil)
	stderr = capturePageListStderr(t, func() {
		execErr = root.Execute()
	})
	if execErr != nil {
		t.Fatalf("%v: %v\nstderr: %s\nstdout: %s", args, execErr, stderr, stdout.String())
	}
	return stdout.String(), stderr
}

func capturePageListStderr(t *testing.T, fn func()) string {
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

func pageListMeta(t *testing.T, stdout string) map[string]any {
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

func TestGeneratedServerSizedOffsetPageAndUndeclaredCursorFlag(t *testing.T) {
	t.Parallel()

	apiSpec := serverSizedOffsetSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	rowsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "rows_list.go")
	marksSrc := readGeneratedFile(t, outputDir, "internal", "cli", "marks_list.go")
	assert.Contains(t, rowsSrc, `listPageContext(cmd.Context(), "--offset")`)
	assert.Contains(t, marksSrc, `listPageContext(cmd.Context(), "")`)
	assert.NotContains(t, marksSrc, "--cursor")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sized_page_behavior_test.go"), []byte(sizedPageBehaviorTest()), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSizedPage", "-count=1")
}

func serverSizedOffsetSpec() *spec.APISpec {
	apiSpec := minimalSpec("sizedpage")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	getID := []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}}
	apiSpec.Resources = map[string]spec.Resource{
		"rows": {
			Description: "Rows",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/rows",
					Description: "List rows",
					Response:    spec.ResponseDef{Type: "array"},
					Params:      []spec.Param{{Name: "offset", Type: "integer"}},
					Pagination: &spec.Pagination{
						Type:         "offset",
						CursorParam:  "offset",
						HasMoreField: "has_more",
					},
				},
				"get": {Method: "GET", Path: "/rows/{id}", Description: "Get a row", Params: getID},
			},
		},
		"marks": {
			Description: "Marks",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/marks",
					Description: "List marks",
					Response:    spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{
						Type:        "cursor",
						CursorParam: "cursor",
					},
				},
				"get": {Method: "GET", Path: "/marks/{id}", Description: "Get a mark", Params: getID},
			},
		},
	}
	return apiSpec
}

func sizedPageBehaviorTest() string {
	return `package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSizedPageOffsetAdvancesByFetchedCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(sizedPageHandler))
	t.Cleanup(server.Close)
	t.Setenv("SIZEDPAGE_BASE_URL", server.URL)

	allOut, allErr := runSizedPage(t, "rows", "list", "--all", "--json")
	if !strings.Contains(allErr, "{\"event\":\"complete\",\"total\":6,\"pages\":3}") {
		t.Fatalf("all stderr = %s\nstdout = %s", allErr, allOut)
	}
	for _, id := range []string{"r1", "r2", "r3", "r4", "r5", "r6"} {
		if !strings.Contains(allOut, id) {
			t.Fatalf("all output missing %s: %s", id, allOut)
		}
	}

	pageOut, pageErr := runSizedPage(t, "rows", "list", "--json")
	meta := sizedPageMeta(t, pageOut)
	if meta["has_more"] != true || meta["next_cursor"] != "3" || meta["next_cursor_flag"] != "--offset" {
		t.Fatalf("page meta = %#v\nstdout = %s", meta, pageOut)
	}
	if !strings.Contains(pageOut, "r1") || !strings.Contains(pageOut, "r3") || strings.Contains(pageOut, "r4") {
		t.Fatalf("first page items = %s", pageOut)
	}
	wantHint := "{\"event\":\"truncated\",\"hint\":\"pass --all to fetch every page, or --offset <meta.next_cursor> for the next page\"}"
	if !strings.Contains(pageErr, wantHint) {
		t.Fatalf("stderr hint = %s", pageErr)
	}

	secondOut, _ := runSizedPage(t, "rows", "list", "--offset", "3", "--json")
	second := sizedPageMeta(t, secondOut)
	if second["next_cursor"] != "5" || second["has_more"] != true || !strings.Contains(secondOut, "r4") || strings.Contains(secondOut, "r1") {
		t.Fatalf("second page = %s meta %#v", secondOut, second)
	}

	lastOut, lastErr := runSizedPage(t, "rows", "list", "--offset", "5", "--json")
	last := sizedPageMeta(t, lastOut)
	if last["has_more"] != false {
		t.Fatalf("last meta = %#v", last)
	}
	if _, ok := last["next_cursor"]; ok {
		t.Fatalf("last page has next_cursor: %#v", last)
	}
	if _, ok := last["next_cursor_flag"]; ok {
		t.Fatalf("last page has next_cursor_flag: %#v", last)
	}
	if strings.Contains(lastErr, "\"event\":\"truncated\"") || !strings.Contains(lastOut, "r6") || strings.Contains(lastOut, "r5") {
		t.Fatalf("last page = %s\nstderr = %s", lastOut, lastErr)
	}
}

func TestSizedPageUndeclaredCursorFlagIsOmitted(t *testing.T) {
	var flags rootFlags
	root := newRootCmd(&flags)
	list, _, err := root.Find([]string{"marks", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Flags().Lookup("cursor") != nil {
		t.Fatal("marks list registered --cursor")
	}
	if list.Flags().Lookup("all") == nil {
		t.Fatal("marks list missing --all")
	}

	server := httptest.NewServer(http.HandlerFunc(sizedPageHandler))
	t.Cleanup(server.Close)
	t.Setenv("SIZEDPAGE_BASE_URL", server.URL)

	out, stderr := runSizedPage(t, "marks", "list", "--json")
	meta := sizedPageMeta(t, out)
	if meta["has_more"] != true || meta["next_cursor"] != "abc" {
		t.Fatalf("marks meta = %#v\nstdout = %s", meta, out)
	}
	if _, ok := meta["next_cursor_flag"]; ok {
		t.Fatalf("marks meta includes next_cursor_flag: %#v", meta)
	}
	wantHint := "{\"event\":\"truncated\",\"hint\":\"pass --all to fetch every page\"}"
	if !strings.Contains(stderr, wantHint) || strings.Contains(stderr, "--cursor") {
		t.Fatalf("marks stderr = %s", stderr)
	}
}

func sizedPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/rows":
		if r.URL.Query().Get("limit") != "" {
			http.Error(w, "unexpected limit", http.StatusBadRequest)
			return
		}
		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "bad offset", http.StatusBadRequest)
				return
			}
			offset = n
		}
		all := []map[string]string{
			{"id": "r1"}, {"id": "r2"}, {"id": "r3"},
			{"id": "r4"}, {"id": "r5"}, {"id": "r6"},
		}
		size := 0
		switch offset {
		case 0:
			size = 3
		case 3:
			size = 2
		case 5:
			size = 1
		default:
			http.Error(w, "bad offset", http.StatusBadRequest)
			return
		}
		end := offset + size
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows":     all[offset:end],
			"extras":   []map[string]string{{"id": "extra"}},
			"has_more": end < len(all),
		})
	case "/marks":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{{"id": "m1"}},
			"cursor":  "abc",
		})
	default:
		http.NotFound(w, r)
	}
}

func runSizedPage(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var flags rootFlags
	root := newRootCmd(&flags)
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"--home", t.TempDir()}, args...))
	var stderr string
	execErr := error(nil)
	stderr = captureSizedPageStderr(t, func() {
		execErr = root.Execute()
	})
	if execErr != nil {
		t.Fatalf("%v: %v\nstderr: %s\nstdout: %s", args, execErr, stderr, stdout.String())
	}
	return stdout.String(), stderr
}

func captureSizedPageStderr(t *testing.T, fn func()) string {
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

func sizedPageMeta(t *testing.T, stdout string) map[string]any {
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
