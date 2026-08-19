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

func TestGeneratedSyncUsesDeclaredCursorPathAndNumericCursor(t *testing.T) {
	apiSpec := minimalSpec("pagination-contract")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "cursorId", Type: "string"},
					},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "cursorId",
						LimitParam:     "limit",
						NextCursorPath: "pagination.continuation_marker",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Item"},
				},
			},
		},
		"archives": {
			Description: "Archives",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/archives",
					Description: "List archives",
					Params:      []spec.Param{{Name: "limit", Type: "integer"}},
					Pagination:  &spec.Pagination{Type: "cursor", LimitParam: "limit"},
					Response:    spec.ResponseDef{Type: "array", Item: "Archive"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent, `cursorParam:    "cursorId"`)
	assert.Contains(t, syncContent, `nextCursorPath: "pagination.continuation_marker"`)
	assert.Contains(t, syncContent, `case "archives":`)
	assert.NotContains(t, syncContent, `cursorParam: "after"`, "a resource without a declared cursor must not fall back to after")

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

func TestDeclaredPaginationCursorPath(t *testing.T) {
	items, cursor, hasMore := extractPageItemsWithPagination(
		json.RawMessage(`+"`"+`{"items":[{"id":"one"}],"pagination":{"continuation_marker":12345}}`+"`"+`),
		"cursorId", "pagination.continuation_marker",
	)
	if len(items) != 1 || cursor != "12345" || !hasMore {
		t.Fatalf("items=%%d cursor=%%q hasMore=%%v, want 1/12345/true", len(items), cursor, hasMore)
	}
}

type paginationSyncClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *paginationSyncClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func (c *paginationSyncClient) RateLimit() float64 { return 0 }

func TestSyncUsesDeclaredNumericCursor(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	defer db.Close()
	client := &paginationSyncClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"pagination\":{\"continuation_marker\":12345}}"),
		json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"pagination\":{\"continuation_marker\":null}}"),
	}}
	result := syncResource(context.Background(), client, db, "items", "", true, 0, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
	}
	if len(client.params) != 2 || client.params[1]["cursorId"] != "12345" {
		t.Fatalf("sync params = %%#v, want a second request with cursorId=12345", client.params)
	}
}

func TestSyncPreservesAdvancingEmptyCursorPages(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	defer db.Close()
	client := &paginationSyncClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[],\"pagination\":{\"continuation_marker\":\"page-2\"}}"),
		json.RawMessage("{\"items\":[],\"pagination\":{\"continuation_marker\":\"page-3\"}}"),
		json.RawMessage("{\"items\":[{\"id\":\"three\"}],\"pagination\":{}}"),
	}}
	result := syncResource(context.Background(), client, db, "items", "", true, 0, false, false, &syncUserParams{}, io.Discard)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("sync result error=%%v warning=%%v", result.Err, result.Warn)
	}
	if len(client.params) != 3 || client.params[1]["cursorId"] != "page-2" || client.params[2]["cursorId"] != "page-3" {
		t.Fatalf("sync params = %%#v, want cursors page-2 then page-3", client.params)
	}
}

func TestUndeclaredPaginationCursorDoesNotUseFallback(t *testing.T) {
	defaults := determinePaginationDefaults("archives")
	if defaults.cursorParam != "" {
		t.Fatalf("cursorParam=%%q, want empty", defaults.cursorParam)
	}
	_, cursor, hasMore := extractPageItemsWithPagination(
		json.RawMessage(`+"`"+`{"items":[{"id":"one"}],"pagination":{"next":"page-2"}}`+"`"+`),
		defaults.cursorParam, defaults.nextCursorPath,
	)
	if cursor != "" || hasMore {
		t.Fatalf("cursor=%%q hasMore=%%v, want empty/false", cursor, hasMore)
	}
}
`, modulePath+"/internal/store")
	testPath := filepath.Join(outputDir, "internal", "cli", "pagination_contract_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(DeclaredPaginationCursorPath|SyncUsesDeclaredNumericCursor|SyncPreservesAdvancingEmptyCursorPages|UndeclaredPaginationCursorDoesNotUseFallback)", "-count=1")
}

func TestGeneratedPostListDoesNotAdvertiseUnwiredAllFlag(t *testing.T) {
	apiSpec := minimalSpec("post-pagination-contract")
	apiSpec.Resources = map[string]spec.Resource{
		"posts": {
			Description: "Posts",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "POST",
					Path:        "/posts/list",
					Description: "List posts",
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "cursor", Type: "string"},
					},
					Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
					Response:   spec.ResponseDef{Type: "array", Item: "Post"},
				},
				"get": {
					Method:   "GET",
					Path:     "/posts/{id}",
					Response: spec.ResponseDef{Type: "object", Item: "Post"},
				},
			},
		},
		"lookup": {
			Description: "Lookup",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "POST",
					Path:        "/lookup",
					Description: "Lookup records",
					Params:      []spec.Param{{Name: "limit", Type: "integer"}, {Name: "cursor", Type: "string"}},
					Pagination:  &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
					Response:    spec.ResponseDef{Type: "array", Item: "Lookup"},
				},
			},
		},
		"things": {
			Description: "Things",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/things",
					Description: "List things",
					Params:      []spec.Param{{Name: "limit", Type: "integer"}, {Name: "cursor", Type: "string"}},
					Pagination:  &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
					Response:    spec.ResponseDef{Type: "array", Item: "Thing"},
				},
				"get": {
					Method:   "GET",
					Path:     "/things/{id}",
					Response: spec.ResponseDef{Type: "object", Item: "Thing"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	postEndpoint := readGeneratedFile(t, outputDir, "internal", "cli", "posts_list.go")
	postPromoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_lookup.go")
	getEndpoint := readGeneratedFile(t, outputDir, "internal", "cli", "things_list.go")
	assert.NotContains(t, postEndpoint, `"all"`)
	assert.NotContains(t, postEndpoint, "flagAll")
	assert.NotContains(t, postPromoted, `"all"`)
	assert.NotContains(t, postPromoted, "flagAll")
	assert.Contains(t, getEndpoint, `flagAll`)

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "build", "./internal/cli")
}
