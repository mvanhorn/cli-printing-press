package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestPaginatedGetEmitsTruncationWarning verifies that generated CLIs include
// the emitTruncationWarning helper and that paginatedGet calls it on the
// single-page path. The warning is the signal agents rely on to detect
// page-1 truncation when --all is not passed (issue #1137).
func TestPaginatedGetEmitsTruncationWarning(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-warn")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-warn-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "func emitTruncationWarning(",
		"generated helpers.go should define emitTruncationWarning")
	require.Contains(t, string(helpersSrc), "emitTruncationWarning(data, cursorLookupPath, hasMoreField, paginationType)",
		"paginatedGet should call emitTruncationWarning on the single-page path")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestGeneratedSyncShortPageTerminationRespectsPaginationType(t *testing.T) {
	t.Parallel()

	templateSrc, err := os.ReadFile(filepath.Join("templates", "sync.go.tmpl"))
	require.NoError(t, err)
	require.Equal(t, 11, strings.Count(string(templateSrc), "shortPageEndsPagination("),
		"the helper definition and every termination and cap-classification variant must stay cursor-aware")
	require.Equal(t, 3, strings.Count(string(templateSrc), "cursorPageHasContinuation("),
		"the helper definition and both flat and dependent empty-page branches must preserve cursor continuation")

	apiSpec := minimalSpec("sync-short-page")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
		"tokens": {
			Description: "Manage tokens",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/tokens",
					Description: "List tokens",
					Pagination: &spec.Pagination{
						Type:           "page_token",
						CursorParam:    "page_token",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Token"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "sync-short-page-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	modulePath := strings.TrimPrefix(strings.SplitN(string(goMod), "\n", 2)[0], "module ")

	behaviorTest := fmt.Sprintf(`package cli

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	%q
)

type shortPageSyncClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *shortPageSyncClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func (c *shortPageSyncClient) RateLimit() float64 { return 0 }

func TestShortPageEndsPagination(t *testing.T) {
	tests := []struct {
		name       string
		cursorType string
		fetched    int
		limit      int
		want       bool
	}{
		{name: "cursor short page continues", cursorType: "cursor", fetched: 50, limit: 100, want: false},
		{name: "page token short page continues", cursorType: "page_token", fetched: 50, limit: 100, want: false},
		{name: "offset short page ends", cursorType: "offset", fetched: 50, limit: 100, want: true},
		{name: "page short page ends", cursorType: "page", fetched: 50, limit: 100, want: true},
		{name: "full cursor page continues", cursorType: "cursor", fetched: 100, limit: 100, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortPageEndsPagination(tt.cursorType, tt.fetched, tt.limit); got != tt.want {
				t.Fatalf("shortPageEndsPagination(%%q, %%d, %%d) = %%v, want %%v", tt.cursorType, tt.fetched, tt.limit, got, tt.want)
			}
		})
	}
}

func TestCursorPageHasContinuation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		cursorType string
		hasMore    bool
		nextCursor string
		want       bool
	}{
		{name: "cursor continues", cursorType: "cursor", hasMore: true, nextCursor: "page-2", want: true},
		{name: "page token continues", cursorType: "page_token", hasMore: true, nextCursor: "page-2", want: true},
		{name: "missing cursor stops", cursorType: "cursor", hasMore: true, want: false},
		{name: "has more false stops", cursorType: "cursor", nextCursor: "page-2", want: false},
		{name: "offset empty page stops", cursorType: "offset", hasMore: true, nextCursor: "2", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := cursorPageHasContinuation(tt.cursorType, tt.hasMore, tt.nextCursor); got != tt.want {
				t.Fatalf("cursorPageHasContinuation(%%q, %%v, %%q) = %%v, want %%v", tt.cursorType, tt.hasMore, tt.nextCursor, got, tt.want)
			}
		})
	}
}

func TestExtractItemsByKnownKeysFallsThroughEmptyPreferredKey(t *testing.T) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte("{\"items\":[],\"records\":[{\"id\":\"one\"}]}"), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %%v", err)
	}
	items, ok := extractItemsByKnownKeys(envelope)
	if !ok || len(items) != 1 {
		t.Fatalf("extractItemsByKnownKeys() = %%d items, ok=%%v; want 1 item, ok=true", len(items), ok)
	}
}

func TestSyncResourceFollowsShortCursorPages(t *testing.T) {
	for _, tc := range []struct {
		resource    string
		cursorParam string
	}{
		{resource: "orders", cursorParam: "after"},
		{resource: "tokens", cursorParam: "page_token"},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
			if err != nil {
				t.Fatalf("open store: %%v", err)
			}
			defer db.Close()

			client := &shortPageSyncClient{responses: []json.RawMessage{
				json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
				json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"next_cursor\":\"\",\"has_more\":false}"),
			}}
			result := syncResource(context.Background(), client, db, tc.resource, "", true, 0, false, false, &syncUserParams{}, io.Discard)
			if result.Err != nil || result.Warn != nil {
				t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
			}
			if result.Count != 2 || len(client.params) != 2 {
				t.Fatalf("sync count/calls = %%d/%%d, want 2/2", result.Count, len(client.params))
			}
			if got := client.params[1][tc.cursorParam]; got != "page-2" {
				t.Fatalf("second request %%s = %%q, want page-2", tc.cursorParam, got)
			}
		})
	}
}

func TestSyncResourceFollowsEmptyCursorPage(t *testing.T) {
	for _, tc := range []struct {
		resource    string
		cursorParam string
	}{
		{resource: "orders", cursorParam: "after"},
		{resource: "tokens", cursorParam: "page_token"},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
			if err != nil {
				t.Fatalf("open store: %%v", err)
			}
			defer db.Close()

			client := &shortPageSyncClient{responses: []json.RawMessage{
				json.RawMessage("{\"items\":[],\"next_cursor\":\"page-2\",\"has_more\":true}"),
				json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"next_cursor\":\"\",\"has_more\":false}"),
			}}
			result := syncResource(context.Background(), client, db, tc.resource, "", true, 0, false, false, &syncUserParams{}, io.Discard)
			if result.Err != nil || result.Warn != nil {
				t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
			}
			if result.Count != 1 || len(client.params) != 2 {
				t.Fatalf("sync count/calls = %%d/%%d, want 1/2", result.Count, len(client.params))
			}
			if got := client.params[1][tc.cursorParam]; got != "page-2" {
				t.Fatalf("second request %%s = %%q, want page-2", tc.cursorParam, got)
			}
		})
	}
}

func TestSyncResourceUsesPopulatedFallbackAfterEmptyItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	defer db.Close()

	client := &shortPageSyncClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[],\"records\":[{\"id\":\"one\"}],\"next_cursor\":\"\",\"has_more\":false}"),
	}}
	result := syncResource(context.Background(), client, db, "orders", "", true, 0, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
	}
	if result.Count != 1 {
		t.Fatalf("sync count = %%d, want 1", result.Count)
	}
}

func TestSyncResourcePreservesCursorWhenCapHitsShortPage(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	defer db.Close()

	client := &shortPageSyncClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "orders", "", true, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
	}
	cursor, _, _, err := db.GetSyncState("orders")
	if err != nil {
		t.Fatalf("get sync state: %%v", err)
	}
	if cursor != "page-2" {
		t.Fatalf("saved cursor = %%q, want page-2", cursor)
	}
}

func TestSyncResourceDoesNotAdvancePastNullItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	defer db.Close()

	client := &shortPageSyncClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":null,\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	_ = syncResource(context.Background(), client, db, "orders", "", true, 0, false, false, &syncUserParams{}, io.Discard)
	if len(client.params) != 1 {
		t.Fatalf("sync calls = %%d, want 1", len(client.params))
	}
	cursor, _, _, err := db.GetSyncState("orders")
	if err != nil {
		t.Fatalf("get sync state: %%v", err)
	}
	if cursor != "" {
		t.Fatalf("saved cursor = %%q, want empty", cursor)
	}
}
`, modulePath+"/internal/store")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_short_page_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestShortPageEndsPagination|TestCursorPageHasContinuation|TestExtractItemsByKnownKeys|TestSyncResource")
	requireGeneratedCompiles(t, outputDir)
}

func TestPaginatedGetHandlesNumericCursorAndMissingAllSignal(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-edge")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "cursor", Type: "string"},
					},
					Pagination: &spec.Pagination{
						Type:        "cursor",
						CursorParam: "cursor",
						LimitParam:  "limit",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-edge-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	endpointSrc := readGeneratedCLIFileContaining(t, outputDir, `flagAll, "cursor", "cursor", "limit"`)
	require.Contains(t, endpointSrc, `flagAll, "cursor", "cursor", "limit", 100, "", ""`,
		"generated list command must preserve an empty next-cursor path for the runtime fallback")

	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

type paginatedTestClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *paginatedTestClient) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) (json.RawMessage, error) {
	_ = ctx
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.params = append(c.params, copied)
	if len(c.responses) == 0 {
		return json.RawMessage(` + "`" + `[]` + "`" + `), nil
	}
	next := c.responses[0]
	c.responses = c.responses[1:]
	return next, nil
}

func capturePaginatedStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldErr }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}

func TestPaginatedGetAcceptsNumericNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":2}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 100, "meta.nextPage", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["page"] != "2" {
		t.Fatalf("second request page = %q, want 2", client.params[1]["page"])
	}
}

func TestPaginatedGetFallsBackToCursorParamResponseField(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"cursor":"page-2"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"cursor":""}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["cursor"] != "page-2" {
		t.Fatalf("second request cursor = %q, want page-2", client.params[1]["cursor"])
	}
}

func TestPaginatedGetWarnsForCursorParamFallbackWithoutAll(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"cursor":"page-2"}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, false, "cursor", "cursor", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"hint":"pass --all to fetch every page"` + "`" + `) {
		t.Fatalf("stderr missing cursor fallback truncation warning: %s", stderr)
	}
}

func TestPaginatedGetWarnsForUnusableFallbackCursor(t *testing.T) {
	for name, cursor := range map[string]string{
		"empty": ` + "`" + `""` + "`" + `,
		"null":  "null",
		"zero":  "0",
		"object": ` + "`" + `{"value":"next"}` + "`" + `,
	} {
		t.Run(name, func(t *testing.T) {
			client := &paginatedTestClient{responses: []json.RawMessage{
				json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"cursor":` + "`" + ` + cursor + ` + "`" + `}` + "`" + `),
			}}
			stderr := capturePaginatedStderr(t, func() {
				data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
				if err != nil {
					t.Fatalf("paginatedGet returned error: %v", err)
				}
				var got []map[string]string
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatalf("unmarshal data: %v", err)
				}
				if len(got) != 1 {
					t.Fatalf("got %d items, want 1", len(got))
				}
			})
			if !strings.Contains(stderr, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
				t.Fatalf("stderr missing pagination signal warning for unusable cursor: %s", stderr)
			}
		})
	}
}

func TestPaginatedGetWarnsWhenFallbackCursorFieldIsMissing(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}]}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !strings.Contains(stderr, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
		t.Fatalf("stderr missing pagination signal warning: %s", stderr)
	}
}

func TestPaginatedGetStopsWhenResponseRepeatsSentCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"cursor":"page-2"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"cursor":"page-2"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"unexpected"}],"cursor":""}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d items, want 2; data=%s", len(got), data)
		}
	})
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_cursor_repeated"` + "`" + `) {
		t.Fatalf("stderr missing repeated-cursor warning: %s", stderr)
	}
}

func TestPaginatedGetStopsWhenResponseCyclesToEarlierCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"cursor":"page-2"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"cursor":"page-3"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}],"cursor":"page-2"}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"unexpected"}],"cursor":""}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d items, want 3; data=%s", len(got), data)
		}
	})
	if len(client.params) != 3 {
		t.Fatalf("got %d requests, want 3", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_cursor_repeated"` + "`" + `) {
		t.Fatalf("stderr missing repeated-cursor warning: %s", stderr)
	}
}

func TestPaginatedGetStopsAtNumericZeroNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":0}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 100, "meta.nextPage", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1; params=%v", len(client.params), client.params)
	}
}

func TestPaginatedGetWarnsForSinglePageNumericNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":2}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, false, "page", "page", "limit", 100, "meta.nextPage", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"hint":"pass --all to fetch every page"` + "`" + `) {
		t.Fatalf("stderr missing numeric-cursor truncation warning: %s", stderr)
	}
}

func TestPaginatedGetWarnsForSinglePageHasMoreNumericPagination(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, false, "page", "page", "limit", 100, "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"hint":"pass --all to fetch every page"` + "`" + `) {
		t.Fatalf("stderr missing has-more numeric pagination truncation hint: %s", stderr)
	}
}

func TestPaginatedGetWarnsWhenAllHasNoAdvanceSignal(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `[{"id":"one"}]` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d items, want 1", len(got))
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
		t.Fatalf("stderr missing structured truncation warning: %s", stderr)
	}
}

func TestPaginatedGetIncrementsNumericCursorWhenHasMoreHasNoBodyCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"four"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 100, "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d items, want 4; data=%s", len(got), data)
		}
	})
	if len(client.params) != 4 {
		t.Fatalf("got %d requests, want 4", len(client.params))
	}
	for i, want := range []string{"2", "3", "4"} {
		if client.params[i+1]["page"] != want {
			t.Fatalf("request %d page = %q, want %s", i+2, client.params[i+1]["page"], want)
		}
	}
	if !containsAll(stderr, ` + "`" + `"event":"complete"` + "`" + `, ` + "`" + `"pages":4` + "`" + `) {
		t.Fatalf("stderr missing complete event for numeric has-more pagination: %s", stderr)
	}
	if strings.Contains(stderr, ` + "`" + `"reason":"pagination_cursor_missing"` + "`" + `) {
		t.Fatalf("stderr should not warn when numeric has-more pagination advances: %s", stderr)
	}
}

func TestPaginatedGetIncrementsNumericCursorWhenDeclaredBodyCursorIsMissing(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 100, "meta.nextPage", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["page"] != "2" {
		t.Fatalf("second request page = %q, want 2", client.params[1]["page"])
	}
}

func TestPaginatedGetAdvancesOffsetCursorByLimitWhenHasMoreHasNoBodyCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"100", "offset":"0"}, nil, true, "offset", "offset", "limit", 100, "", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["offset"] != "100" {
		t.Fatalf("second request offset = %q, want 100", client.params[1]["offset"])
	}
}

func TestPaginatedGetAdvancesOffsetAfterFullPageWithoutHasMore(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}]}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", 100, "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["offset"] != "2" {
		t.Fatalf("second request offset = %q, want 2", client.params[1]["offset"])
	}
}

func TestPaginatedGetAdvancesPageAfterFullPageWithoutHasMore(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}]}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}]}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"per_page":"2"}, nil, true, "page", "page", "per_page", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d items, want 3; data=%s", len(got), data)
		}
	})
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["page"] != "2" {
		t.Fatalf("second request page = %q, want 2", client.params[1]["page"])
	}
	if strings.Contains(stderr, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
		t.Fatalf("stderr should not warn when page pagination advances client-side: %s", stderr)
	}
}

func TestPaginatedGetDoesNotWarnWhenOffsetShortPageHasNoSignal(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}]}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", 100, "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d items, want 1; data=%s", len(got), data)
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if strings.Contains(stderr, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
		t.Fatalf("stderr should not warn after a short offset page: %s", stderr)
	}
}

func TestPaginatedGetStopsOffsetAtExplicitHasMoreFalseAfterFullPage(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", 100, "", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1; params=%v", len(client.params), client.params)
	}
}

func TestPaginatedGetStopsOffsetAfterShortPageWithoutHasMore(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}]}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", 100, "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
}

func TestPaginatedGetWarnsWhenHasMorePageParamIsNonNumeric(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1", "page":"not-a-number"}, nil, true, "page", "page", "limit", 100, "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d items, want 1", len(got))
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_cursor_missing"` + "`" + `) {
		t.Fatalf("stderr missing has-more truncation warning: %s", stderr)
	}
	if strings.Contains(stderr, ` + "`" + `"next_cursor_path":""` + "`" + `) {
		t.Fatalf("stderr should omit an empty next_cursor_path: %s", stderr)
	}
}

func TestPaginatedGetStopsAtMaxPageSafetyLimit(t *testing.T) {
	responses := make([]json.RawMessage, paginatedGetMaxPages+1)
	for i := range responses {
		responses[i] = json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `)
	}
	client := &paginatedTestClient{responses: responses}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 100, "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != paginatedGetMaxPages {
			t.Fatalf("got %d items, want %d", len(got), paginatedGetMaxPages)
		}
	})
	if len(client.params) != paginatedGetMaxPages {
		t.Fatalf("got %d requests, want %d", len(client.params), paginatedGetMaxPages)
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"max_pages_cap_hit"` + "`" + `) {
		t.Fatalf("stderr missing max-pages truncation warning: %s", stderr)
	}
}

func TestPaginatedGetStopsAtMaxPageSafetyLimitForBodyCursor(t *testing.T) {
	responses := make([]json.RawMessage, paginatedGetMaxPages+1)
	for i := range responses {
		responses[i] = json.RawMessage(fmt.Sprintf(` + "`" + `{"items":[{"id":"one"}],"meta":{"next":"next-token-%d"}}` + "`" + `, i+1))
	}
	client := &paginatedTestClient{responses: responses}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "meta.next", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != paginatedGetMaxPages {
			t.Fatalf("got %d items, want %d", len(got), paginatedGetMaxPages)
		}
	})
	if len(client.params) != paginatedGetMaxPages {
		t.Fatalf("got %d requests, want %d", len(client.params), paginatedGetMaxPages)
	}
	if client.params[1]["cursor"] != "next-token-1" {
		t.Fatalf("second request cursor = %q, want next-token-1", client.params[1]["cursor"])
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"max_pages_cap_hit"` + "`" + `) {
		t.Fatalf("stderr missing max-pages body-cursor truncation warning: %s", stderr)
	}
}

func TestPaginatedGetMergesDomainSpecificWrappedArrayWithMetadataArrays(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{
			"charges": [{"id":"ch_1","amount":10}],
			"warnings": [{"code":"slow"}],
			"cursor": "next-token"
		}` + "`" + `),
		json.RawMessage(` + "`" + `{
			"charges": [{"id":"ch_2","amount":20}],
			"warnings": [],
			"cursor": null
		}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/charges", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "cursor", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	if string(data) == "null" {
		t.Fatalf("paginatedGet returned null for populated wrapped pages")
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v\n%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if got[0]["id"] != "ch_1" || got[1]["id"] != "ch_2" {
		t.Fatalf("merged wrong collection: %#v", got)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["cursor"] != "next-token" {
		t.Fatalf("second request cursor = %q, want next-token", client.params[1]["cursor"])
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "paginated_get_issue1688_test.go"), []byte(behaviorTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestPaginatedGet")
	requireGeneratedCompiles(t, outputDir)
}

func TestIssue3497BareAllOffsetUsesEndpointPageSize(t *testing.T) {
	t.Parallel()

	capped := 2.0
	for _, tc := range []struct {
		name       string
		limitParam spec.Param
	}{
		{
			name:       "default",
			limitParam: spec.Param{Name: "limit", Type: "integer", Default: 2},
		},
		{
			name:       "maximum",
			limitParam: spec.Param{Name: "limit", Type: "integer", Maximum: &capped},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("issue3497-offset-" + tc.name)
			apiSpec.Resources = map[string]spec.Resource{
				"records": {
					Description: "Manage records",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:      "GET",
							Path:        "/records",
							Description: "List records",
							Params: []spec.Param{
								tc.limitParam,
								{Name: "offset", Type: "integer"},
							},
							Pagination: &spec.Pagination{
								Type:        "offset",
								CursorParam: "offset",
								LimitParam:  "limit",
							},
							Response: spec.ResponseDef{Type: "array", Item: "Record"},
						},
					},
				},
			}

			outputDir := filepath.Join(t.TempDir(), "issue3497-offset-"+tc.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			gen.VisionSet = VisionTemplateSet{Export: true}
			gen.profile = &profiler.APIProfile{
				Pagination: profiler.PaginationProfile{
					CursorParam:     "offset",
					CursorType:      "offset",
					PageSizeParam:   "limit",
					DefaultPageSize: 100,
				},
			}
			require.NoError(t, gen.Generate())

			generatedCLISourceContaining(t, outputDir, `flagAll, "offset", "offset", "limit", 2, "", ""`)

			behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"testing"
)

type issue3497Client struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *issue3497Client) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) (json.RawMessage, error) {
	_ = ctx
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.params = append(c.params, copied)
	if len(c.responses) == 0 {
		return json.RawMessage(` + "`" + `[]` + "`" + `), nil
	}
	next := c.responses[0]
	c.responses = c.responses[1:]
	return next, nil
}

func TestIssue3497BareAllOffsetUsesDefaultPageSize(t *testing.T) {
	client := &issue3497Client{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `[{"id":"one"},{"id":"two"}]` + "`" + `),
		json.RawMessage(` + "`" + `[{"id":"three"}]` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/records", map[string]string{"limit":"0", "offset":"0"}, nil, true, "offset", "offset", "limit", 2, "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[0]["limit"] != "2" {
		t.Fatalf("first request limit = %q, want 2; params=%v", client.params[0]["limit"], client.params[0])
	}
	if client.params[0]["offset"] != "0" {
		t.Fatalf("first request offset = %q, want 0", client.params[0]["offset"])
	}
	if client.params[1]["limit"] != "2" {
		t.Fatalf("second request limit = %q, want 2; params=%v", client.params[1]["limit"], client.params[1])
	}
	if client.params[1]["offset"] != "2" {
		t.Fatalf("second request offset = %q, want 2", client.params[1]["offset"])
	}
}
`
			cliDir := filepath.Join(outputDir, "internal", "cli")
			require.NoError(t, os.WriteFile(filepath.Join(cliDir, "paginated_get_issue3497_test.go"), []byte(behaviorTest), 0o644))
			runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestIssue3497BareAllOffsetUsesDefaultPageSize$", "-count=1")
		})
	}
}

func TestOpenAPINestedNextPageGeneratesPaginatedCommandSignal(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Nested Page API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /opportunities/search:
    get:
      operationId: searchOpportunities
      parameters:
        - name: page
          in: query
          schema: {type: integer}
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  meta:
                    type: object
                    properties:
                      nextPage:
                        type: integer
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "nested-page-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)
	var commandSrc strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(cliDir, entry.Name()))
		require.NoError(t, err)
		commandSrc.Write(src)
		commandSrc.WriteByte('\n')
	}
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", 100, "meta.nextPage", ""`,
		"generated command must pass parser-detected nested nextPage to resolvePaginatedRead")
}

func TestOpenAPIHasMoreOnlyPageGeneratesPaginatedCommandSignal(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Has More Page API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /folders:
    get:
      operationId: listFolders
      parameters:
        - name: page
          in: query
          schema: {type: integer}
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  has_more:
                    type: boolean
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "has-more-page-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)
	var commandSrc strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(cliDir, entry.Name()))
		require.NoError(t, err)
		commandSrc.Write(src)
		commandSrc.WriteByte('\n')
	}
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", 100, "", "has_more"`,
		"generated command must pass has-more-only page pagination metadata to resolvePaginatedRead")
}
