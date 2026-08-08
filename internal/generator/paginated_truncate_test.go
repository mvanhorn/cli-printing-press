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
	"github.com/stretchr/testify/assert"
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
	require.Contains(t, string(helpersSrc), "emitTruncationWarning(ctx, data, cursorLookupPath, hasMoreField, paginationType)",
		"paginatedGet should call emitTruncationWarning on the single-page path")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestPaginatedGetPreservesResourceNamedEnvelopeForSelection(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-envelope")
	apiSpec.Resources = map[string]spec.Resource{
		"services": {
			Description: "Manage services",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/services",
					Description: "List services",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "pageToken",
						NextCursorPath: "nextPageToken",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Service"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-envelope-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	var endpointSrc string
	err := filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "collectionItemsForOutput(data, path)") {
			endpointSrc = string(content)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, endpointSrc, "generated services list command should exist")
	require.Contains(t, endpointSrc, "outputData := collectionItemsForOutput(data, path)")
	require.Contains(t, endpointSrc, "formatData = outputData")
	requireGeneratedCompiles(t, outputDir)

	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"testing"
)

type envelopePaginationClient struct {
	responses []json.RawMessage
}

func (c *envelopePaginationClient) GetWithHeaders(_ context.Context, _ string, _ map[string]string, _ map[string]string) (json.RawMessage, error) {
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestPaginatedEnvelopeSelection(t *testing.T) {
	client := &envelopePaginationClient{responses: []json.RawMessage{
		json.RawMessage("{\"services\":[{\"name\":\"web\",\"status\":\"ready\"}],\"nextPageToken\":\"page-2\"}"),
		json.RawMessage("{\"services\":[{\"name\":\"worker\",\"status\":\"ready\"}]}"),
	}}
	data, err := paginatedGet(context.Background(), client, "/services", nil, nil, true, "pageToken", "cursor", "", 100, "nextPageToken", "")
	if err != nil {
		t.Fatalf("paginatedGet: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v; data=%s", err, data)
	}
	var services []map[string]any
	if err := json.Unmarshal(envelope["services"], &services); err != nil || len(services) != 2 {
		t.Fatalf("services = %v, err=%v; want two resources", services, err)
	}
	if _, ok := envelope["nextPageToken"]; ok {
		t.Fatalf("fully consumed nextPageToken remained in envelope: %s", data)
	}

	selected := filterFields(data, "services.name")
	var selectedEnvelope map[string][]map[string]any
	if err := json.Unmarshal(selected, &selectedEnvelope); err != nil || len(selectedEnvelope["services"]) != 2 {
		t.Fatalf("selected envelope = %s, err=%v", selected, err)
	}
	if selectedEnvelope["services"][0]["name"] != "web" || selectedEnvelope["services"][1]["name"] != "worker" {
		t.Fatalf("selected services lost names: %s", selected)
	}

	compacted := compactFields(data)
	var compactEnvelope map[string][]map[string]any
	if err := json.Unmarshal(compacted, &compactEnvelope); err != nil || len(compactEnvelope["services"]) != 2 {
		t.Fatalf("compact envelope = %s, err=%v", compacted, err)
	}

	outputData := collectionItemsForOutput(data, "/services")
	var outputItems []map[string]any
	if err := json.Unmarshal(outputData, &outputItems); err != nil || len(outputItems) != 2 {
		t.Fatalf("output items = %s, err=%v; want two resources", outputData, err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "pagination_envelope_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPaginatedEnvelopeSelection", "-count=1")
}

func TestGeneratedSyncShortPageTerminationRespectsPaginationType(t *testing.T) {
	t.Parallel()

	templateSrc, err := os.ReadFile(filepath.Join("templates", "sync.go.tmpl"))
	require.NoError(t, err)
	require.Equal(t, 13, strings.Count(string(templateSrc), "shortPageEndsPagination("),
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

func TestGeneratedSyncNormalizesPagePaginationProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		page spec.Pagination
		want string
	}{
		{name: "missing type", page: spec.Pagination{CursorParam: "page", LimitParam: "per_page"}, want: "page"},
		{name: "offset type on page parameter", page: spec.Pagination{Type: "offset", CursorParam: "page", LimitParam: "per_page"}, want: "page"},
		{name: "page type on offset parameter", page: spec.Pagination{Type: "page", CursorParam: "offset", LimitParam: "limit"}, want: "offset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiSpec := minimalSpec("page-profile-" + strings.ReplaceAll(tc.name, " ", "-"))
			apiSpec.Resources = map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:      "GET",
							Path:        "/items",
							Description: "List items",
							Params:      []spec.Param{{Name: "page", Type: "integer"}, {Name: "per_page", Type: "integer"}},
							Pagination:  &tc.page,
							Response:    spec.ResponseDef{Type: "array"},
						},
					},
				},
			}

			outputDir := filepath.Join(t.TempDir(), "page-profile-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())
			syncSrc := readGeneratedCLIFileContaining(t, outputDir, "func determinePaginationDefaults")
			require.Contains(t, syncSrc, fmt.Sprintf(`cursorType:  %q`, tc.want), "generated sync must use the normalized pagination strategy")
			requireGeneratedCompiles(t, outputDir)
		})
	}
}

func TestGeneratedSyncPreservesWatermarkAcrossPaginationCaps(t *testing.T) {
	apiSpec := minimalSpec("sync-watermark")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items updated after a timestamp, sorted by updated_at ascending.",
					Params: []spec.Param{
						{Name: "after", Type: "string"},
						{Name: "limit", Type: "integer", Default: 2},
						{Name: "updated_after", Type: "string"},
						{Name: "sort", Type: "string", Default: "updated_at:asc", Description: "Sort by updated_at ascending."},
					},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						LimitParam:     "limit",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "sync-watermark-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	syncSrc := readGeneratedCLIFileContaining(t, outputDir, "func syncResourceSortParam")
	require.Contains(t, syncSrc, "func syncResourceSortParam(resource string) string")
	require.Contains(t, syncSrc, "func syncResourceSortField(resource string) string")
	require.Contains(t, syncSrc, "restSyncTimestamp(item, sortField)")
	require.Contains(t, syncSrc, "db.SaveSyncStateAt(resource, finalCursor, cachedCount, watermark)")
	require.Contains(t, syncSrc, "db.SaveSyncProgress(resource, nextCursor, totalCount)")
	requireGeneratedCompiles(t, outputDir)

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	modulePath := strings.TrimPrefix(strings.SplitN(string(goMod), "\n", 2)[0], "module ")
	behaviorTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	` + fmt.Sprintf("%q", modulePath+"/internal/store") + `
)

type watermarkPagerClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *watermarkPagerClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func (c *watermarkPagerClient) RateLimit() float64 { return 0 }

func seedWatermark(t *testing.T, db *store.Store, at time.Time) {
	t.Helper()
	if err := db.Upsert("items", "existing", []byte("{\"id\":\"existing\",\"updated_at\":\"2025-12-31T00:00:00Z\"}")); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if err := db.SaveSyncStateAt("items", "", 1, at); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
}

func readWatermark(t *testing.T, db *store.Store) (string, time.Time, int) {
	t.Helper()
	cursor, synced, count, err := db.GetSyncState("items")
	if err != nil {
		t.Fatalf("read sync state: %v", err)
	}
	return cursor, synced, count
}

func TestCappedOrderedPageAdvancesToNewestStored(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"},{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	if client.params[0]["sort"] != "updated_at:asc" {
		t.Fatalf("sort param = %q, want updated_at:asc", client.params[0]["sort"])
	}
	cursor, synced, count := readWatermark(t, db)
	want := time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC)
	if cursor != "" || !synced.Equal(want) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want empty cursor, %s, 3", cursor, synced, count, want)
	}
}

func TestCappedNestedTimestampUsesRecordTimestamp(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"first\",\"updated_at\":\"2026-01-01T00:00:00Z\"},{\"id\":\"nested\",\"updated_at\":\"2026-01-02T00:00:00Z\",\"children\":[{\"updated_at\":\"2026-06-01T00:00:00Z\"}]}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	if timestamp, ok := restSyncTimestamp(json.RawMessage("{\"id\":\"nested\",\"updated_at\":\"2026-01-02T00:00:00Z\",\"children\":[{\"updated_at\":\"2026-06-01T00:00:00Z\"}]}"), "updated_at"); !ok || !timestamp.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("record timestamp = %s, ok=%v; want 2026-01-02, true", timestamp, ok)
	}
	var events bytes.Buffer
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, &events)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	want := time.Date(2026, 1, 1, 23, 59, 59, 0, time.UTC)
	if cursor != "" || !synced.Equal(want) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want empty cursor, %s, 3; events=%s", cursor, synced, count, want, events.String())
	}
}

func TestCappedMismatchedTimestampRetainsWatermark(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\",\"modified_at\":\"2026-06-01T00:00:00Z\"},{\"id\":\"two\",\"modified_at\":\"2026-06-02T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	if cursor != "page-2" || !synced.Equal(old) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want page-2, %s, 3", cursor, synced, count, old)
	}
}

func TestCappedTimestampUsesExpectedFieldWithUnrelatedTimestamp(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\",\"modified_at\":\"2026-06-01T00:00:00Z\"},{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\",\"modified_at\":\"2026-06-02T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	want := time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC)
	if cursor != "" || !synced.Equal(want) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want empty cursor, %s, 3", cursor, synced, count, want)
	}
}

func TestRestSyncTimestampRequiresExpectedField(t *testing.T) {
	if timestamp, ok := restSyncTimestamp(json.RawMessage("{\"modified_at\":\"2026-06-01T00:00:00Z\"}"), "updated_at"); ok || !timestamp.IsZero() {
		t.Fatalf("mismatched timestamp = %s, ok=%v; want zero, false", timestamp, ok)
	}
	if timestamp, ok := restSyncTimestamp(json.RawMessage("{\"updatedAt\":\"2026-01-02T00:00:00Z\",\"modified_at\":\"2026-06-01T00:00:00Z\"}"), "updated_at"); !ok || !timestamp.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("normalized timestamp = %s, ok=%v; want 2026-01-02, true", timestamp, ok)
	}
}

func TestCappedPageWithUnstoredItemRetainsWatermark(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"stored\",\"updated_at\":\"2026-01-02T00:00:00Z\"},{\"updated_at\":\"2026-01-03T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	if cursor != "page-2" || !synced.Equal(old) || count != 2 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want page-2, %s, 2", cursor, synced, count, old)
	}
}

func TestCappedUnorderedPageRetainsWatermark(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\"},{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	if cursor != "page-2" || !synced.Equal(old) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want page-2, %s, 3", cursor, synced, count, old)
	}
}

func TestCappedSortOverrideRetainsWatermark(t *testing.T) {
	for _, tc := range []struct {
		name       string
		userParams *syncUserParams
	}{
		{name: "param", userParams: &syncUserParams{flatGlobal: map[string]string{"sort": "name:asc"}}},
		{name: "global-param", userParams: &syncUserParams{trueGlobal: map[string]string{"sort": "name:asc"}}},
		{name: "resource-param", userParams: &syncUserParams{perResource: map[string]map[string]string{"items": {"sort": "name:asc"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer db.Close()
			old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
			seedWatermark(t, db, old)
			client := &watermarkPagerClient{responses: []json.RawMessage{
				json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"},{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
			}}
			result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, tc.userParams, io.Discard)
			if result.Err != nil || result.Warn != nil {
				t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
			}
			if client.params[0]["sort"] != "name:asc" {
				t.Fatalf("sort param = %q, want name:asc", client.params[0]["sort"])
			}
			cursor, synced, count := readWatermark(t, db)
			if cursor != "page-2" || !synced.Equal(old) || count != 3 {
				t.Fatalf("sync state = cursor %q, synced %s, count %d; want page-2, %s, 3", cursor, synced, count, old)
			}
		})
	}
}

func TestDrainedSyncUsesRequestWatermark(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	before := time.Now().UTC().Add(-2 * time.Second)
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"},{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\"}],\"next_cursor\":\"page-2\",\"has_more\":true}"),
		json.RawMessage("{\"items\":[{\"id\":\"three\",\"updated_at\":\"2026-01-04T00:00:00Z\"}],\"has_more\":false}"),
	}}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 0, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count := readWatermark(t, db)
	if cursor != "" || !synced.After(before) || !synced.Before(time.Now().UTC().Add(time.Second)) || count != 4 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want drained request watermark and count 4", cursor, synced, count)
	}
}

func TestLatestOnlySinglePageIsNaturalEnd(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	seedWatermark(t, db, old)
	if err := db.SaveSyncStateAt("items", "", 0, time.Time{}); err != nil {
		t.Fatalf("clear latest-only state: %v", err)
	}
	var events bytes.Buffer
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"head\",\"updated_at\":\"2026-01-05T00:00:00Z\"}],\"has_more\":false}"),
	}}
	result := syncResource(context.Background(), client, db, "items", "", false, 1, true, false, &syncUserParams{}, &events)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	if strings.Contains(events.String(), "max_pages_cap_hit") {
		t.Fatalf("latest-only natural end emitted a cap warning: %s", events.String())
	}
	if _, ok := client.params[0]["sort"]; ok {
		t.Fatalf("latest-only unexpectedly sent sort=%q", client.params[0]["sort"])
	}
	if _, ok := client.params[0]["updated_after"]; ok {
		t.Fatalf("latest-only unexpectedly sent updated_after=%q", client.params[0]["updated_after"])
	}
	_, synced, _ := readWatermark(t, db)
	if !synced.After(old) {
		t.Fatalf("latest-only watermark = %s, want it advanced past %s", synced, old)
	}
}

func TestFullSyncOmitsIncrementalSort(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	client := &watermarkPagerClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"}],\"has_more\":false}"),
	}}
	result := syncResource(context.Background(), client, db, "items", "", true, 0, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	if _, ok := client.params[0]["sort"]; ok {
		t.Fatalf("full sync unexpectedly sent sort=%q", client.params[0]["sort"])
	}
	if _, ok := client.params[0]["updated_after"]; ok {
		t.Fatalf("full sync unexpectedly sent updated_after=%q", client.params[0]["updated_after"])
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_watermark_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(Capped|Drained|LatestOnly|FullSync|RestSync)", "-count=1")
}

func TestGeneratedSyncClearsOffsetCursorWhenWatermarkMoves(t *testing.T) {
	apiSpec := minimalSpec("sync-watermark-offset")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items updated after a timestamp, sorted by updated_at ascending.",
					Params: []spec.Param{
						{Name: "offset", Type: "integer"},
						{Name: "limit", Type: "integer", Default: 2},
						{Name: "updated_after", Type: "string"},
						{Name: "sort", Type: "string", Default: "updated_at:asc", Description: "Sort by updated_at ascending."},
					},
					Pagination: &spec.Pagination{
						Type:           "offset",
						CursorParam:    "offset",
						LimitParam:     "limit",
						HasMoreField:   "has_more",
						NextCursorPath: "",
					},
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "sync-watermark-offset-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	modulePath := strings.TrimPrefix(strings.SplitN(string(goMod), "\n", 2)[0], "module ")
	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	` + fmt.Sprintf("%q", modulePath+"/internal/store") + `
)

type offsetWatermarkClient struct {
	response json.RawMessage
	params   []map[string]string
}

func (c *offsetWatermarkClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	return c.response, nil
}

func (c *offsetWatermarkClient) RateLimit() float64 { return 0 }

func TestOffsetWatermarkDoesNotKeepOldPosition(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	old := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	if err := db.Upsert("items", "existing", []byte("{\"id\":\"existing\"}")); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if err := db.SaveSyncStateAt("items", "", 1, old); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	client := &offsetWatermarkClient{response: json.RawMessage("{\"items\":[{\"id\":\"one\",\"updated_at\":\"2026-01-02T00:00:00Z\"},{\"id\":\"two\",\"updated_at\":\"2026-01-03T00:00:00Z\"}],\"has_more\":true}")}
	result := syncResource(context.Background(), client, db, "items", old.Format(time.RFC3339), false, 1, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%v warning=%v", result.Err, result.Warn)
	}
	cursor, synced, count, err := db.GetSyncState("items")
	if err != nil {
		t.Fatalf("read sync state: %v", err)
	}
	want := time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC)
	if cursor != "" || !synced.Equal(want) || count != 3 {
		t.Fatalf("sync state = cursor %q, synced %s, count %d; want empty cursor, %s, 3", cursor, synced, count, want)
	}
	if offset, ok := client.params[0]["offset"]; ok && offset != "0" {
		t.Fatalf("first offset = %q, want absent or 0", offset)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_watermark_offset_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestOffsetWatermarkDoesNotKeepOldPosition", "-count=1")
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
	require.Contains(t, endpointSrc, `flagAll, "cursor", "cursor", "limit", 0, "", ""`,
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

func TestPaginatedGetExtractsHALEmbeddedItems(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"_embedded":{"events":[{"id":"one"}]}}` + "`" + `),
		json.RawMessage(` + "`" + `{"_embedded":{"events":[{"id":"two"}]}}` + "`" + `),
		json.RawMessage(` + "`" + `{"_embedded":{"events":[]}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/events", map[string]string{"page":"1", "size":"0"}, nil, true, "page", "page", "size", 1, "", "")
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
	if got[0]["id"] != "one" || got[1]["id"] != "two" {
		t.Fatalf("got items %#v, want one followed by two", got)
	}
	if len(client.params) != 3 {
		t.Fatalf("got %d requests, want 3", len(client.params))
	}
	for i, wantPage := range []string{"1", "2", "3"} {
		if client.params[i]["page"] != wantPage {
			t.Fatalf("request %d page = %q, want %s", i+1, client.params[i]["page"], wantPage)
		}
		if client.params[i]["size"] != "1" {
			t.Fatalf("request %d size = %q, want 1", i+1, client.params[i]["size"])
		}
	}
}

func TestPaginatedGetSelectsHALEmbeddedCollectionMatchingPath(t *testing.T) {
	for _, requestPath := range []string{"/discovery/v2/events.json", "/events/search"} {
		t.Run(requestPath, func(t *testing.T) {
			client := &paginatedTestClient{responses: []json.RawMessage{
				json.RawMessage(` + "`" + `{
					"_embedded": {
						"events": [{"id":"event-one"}],
						"items": [{"id":"unrelated-item"}]
					}
				}` + "`" + `),
			}}
			data, err := paginatedGet(context.Background(), client, requestPath, map[string]string{"size":"2"}, nil, true, "page", "page", "size", 2, "", "")
			if err != nil {
				t.Fatalf("paginatedGet returned error: %v", err)
			}
			var got []map[string]string
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal data: %v", err)
			}
			if len(got) != 1 || got[0]["id"] != "event-one" {
				t.Fatalf("got items %#v, want only the events collection", got)
			}
		})
	}
}

func TestPaginatedGetKeepsUnmatchedHALEmbeddedCollectionsAmbiguous(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{
			"_embedded": {
				"events": [{"id":"event-one"}],
				"items": [{"id":"unrelated-item"}]
			}
		}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/discovery/v2/catalog.json", map[string]string{"size":"2"}, nil, true, "page", "page", "size", 2, "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got items %#v, want ambiguous HAL collections left unselected", got)
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
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, data)
	}
	var got []map[string]any
	if err := json.Unmarshal(envelope["charges"], &got); err != nil {
		t.Fatalf("unmarshal charges: %v\n%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if got[0]["id"] != "ch_1" || got[1]["id"] != "ch_2" {
		t.Fatalf("merged wrong collection: %#v", got)
	}
	if _, ok := envelope["warnings"]; !ok {
		t.Fatalf("preserved envelope missing warnings: %s", data)
	}
	if _, ok := envelope["cursor"]; ok {
		t.Fatalf("consumed cursor leaked into preserved envelope: %s", data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["cursor"] != "next-token" {
		t.Fatalf("second request cursor = %q, want next-token", client.params[1]["cursor"])
	}
}

func TestPaginatedGetRemovesPageEnvelopeMetadata(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"charges":[{"id":"ch_1"}],"page":1,"has_more":true}` + "`" + `),
		json.RawMessage(` + "`" + `{"charges":[{"id":"ch_2"}],"page":2,"has_more":false}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/charges", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", 1, "", "has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, data)
	}
	var got []map[string]any
	if err := json.Unmarshal(envelope["charges"], &got); err != nil {
		t.Fatalf("unmarshal charges: %v\n%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	for _, key := range []string{"page", "has_more"} {
		if _, ok := envelope[key]; ok {
			t.Fatalf("consumed %s metadata leaked into envelope: %s", key, data)
		}
	}
}

func TestPaginatedGetRejectsMixedEnvelopeShape(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"charges":[{"id":"ch_1"}],"cursor":"next-token"}` + "`" + `),
		json.RawMessage(` + "`" + `[{
			"id":"ch_2"
		}]` + "`" + `),
	}}
	_, err := paginatedGet(context.Background(), client, "/charges", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", 100, "cursor", "")
	if err == nil || !strings.Contains(err.Error(), "collection changed") {
		t.Fatalf("paginatedGet error = %v, want mixed collection shape error", err)
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
		name         string
		limitParam   spec.Param
		wantPageSize int
	}{
		{
			name:         "default",
			limitParam:   spec.Param{Name: "limit", Type: "integer", Default: 2},
			wantPageSize: 2,
		},
		{
			name:         "maximum",
			limitParam:   spec.Param{Name: "limit", Type: "integer", Maximum: &capped},
			wantPageSize: 0,
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

			generatedCLISourceContaining(t, outputDir, fmt.Sprintf(`flagAll && !flags.dryRun, "offset", "offset", "limit", %d, "", ""`, tc.wantPageSize))

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

func TestIssue3989UnknownPageSizeDoesNotStopOnShortPage(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("unknown-page-size")
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Response:    spec.ResponseDef{Type: "array", Item: "Record"},
					Pagination:  &spec.Pagination{Type: "page", CursorParam: "page"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "unknown-page-size-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	commandSrc := generatedCLISourceContaining(t, outputDir, `"page", "page", "", `)
	assert.Contains(t, commandSrc, `"page", "page", "", 0, "", ""`,
		"generated page pagination must not pass fabricated page size 100 when the spec has no page-size parameter")
	requireGeneratedCompiles(t, outputDir)

	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"testing"
)

type unknownPageSizeClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *unknownPageSizeClient) GetWithHeaders(_ context.Context, _ string, params map[string]string, _ map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestPaginatedGetKeepsFetchingWithUnknownPageSize(t *testing.T) {
	for _, test := range []struct {
		name        string
		cursorParam string
		pagination  string
		wantFirst   string
		wantSecond  string
		wantThird   string
	}{
		{name: "page", cursorParam: "page", pagination: "page", wantSecond: "2", wantThird: "3"},
		{name: "offset", cursorParam: "offset", pagination: "offset", wantFirst: "0", wantSecond: "2", wantThird: "3"},
		} {
			t.Run(test.name, func(t *testing.T) {
			client := &unknownPageSizeClient{responses: []json.RawMessage{
				json.RawMessage("[{\"id\":\"one\"},{\"id\":\"two\"}]"),
				json.RawMessage("[{\"id\":\"three\"}]"),
				json.RawMessage("[]"),
			}}
			params := map[string]string{}
			if test.wantFirst != "" {
				params[test.cursorParam] = test.wantFirst
			}
			data, err := paginatedGet(context.Background(), client, "/records", params, nil, true, test.cursorParam, test.pagination, "", 0, "", "")
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
			if len(client.params) != 3 {
				t.Fatalf("got %d requests, want 3", len(client.params))
			}
			if client.params[0][test.cursorParam] != test.wantFirst || client.params[1][test.cursorParam] != test.wantSecond || client.params[2][test.cursorParam] != test.wantThird {
				t.Fatalf("%s params = %#v, want %s, %s, %s", test.cursorParam, client.params, test.wantFirst, test.wantSecond, test.wantThird)
			}
		})
	}
	}

func TestPaginatedGetKeepsFetchingWithUnknownOffsetSizeAndHasMore(t *testing.T) {
	client := &unknownPageSizeClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"},{\"id\":\"two\"}],\"meta\":{\"has_more\":true}}"),
		json.RawMessage("{\"items\":[{\"id\":\"three\"}],\"meta\":{\"has_more\":true}}"),
		json.RawMessage("{\"items\":[],\"meta\":{\"has_more\":false}}"),
	}}
	data, err := paginatedGet(context.Background(), client, "/records", map[string]string{"offset":"0"}, nil, true, "offset", "offset", "", 0, "", "meta.has_more")
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
	if len(client.params) != 3 {
		t.Fatalf("got %d requests, want 3", len(client.params))
	}
	for i, want := range []string{"0", "2", "3"} {
		if got := client.params[i]["offset"]; got != want {
			t.Fatalf("request %d offset = %q, want %q", i+1, got, want)
		}
	}
}

func TestPaginatedGetKeepsFetchingWithUnknownOffsetAfterEmptyHasMorePage(t *testing.T) {
	client := &unknownPageSizeClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/records", map[string]string{"offset":"0"}, nil, true, "offset", "offset", "", 0, "", "meta.has_more")
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
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if got := client.params[1]["offset"]; got != "1" {
		t.Fatalf("second request offset = %q, want 1", got)
	}
}

func TestPaginatedGetKeepsFetchingWithUnknownOffsetAfterPopulatedEmptyHasMorePage(t *testing.T) {
	client := &unknownPageSizeClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/records", map[string]string{"offset":"0"}, nil, true, "offset", "offset", "", 0, "", "meta.has_more")
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
	if len(client.params) != 3 {
		t.Fatalf("got %d requests, want 3", len(client.params))
	}
	for i, want := range []string{"0", "2", "3"} {
		if got := client.params[i]["offset"]; got != want {
			t.Fatalf("request %d offset = %q, want %q", i+1, got, want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "unknown_page_size_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestPaginatedGetKeepsFetchingWithUnknown", "-count=1")
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
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", 0, "meta.nextPage", ""`,
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
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", 0, "", "has_more"`,
		"generated command must pass has-more-only page pagination metadata to resolvePaginatedRead")
}
