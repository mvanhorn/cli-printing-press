package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func firstPageOffsetSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Cache.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"bounces":     offsetList("Bounces", "/bounces", "count", true, 0, nil),
		"reviews":     offsetList("Reviews", "/reviews", "limit", true, 0, nil),
		"commands":    pageList("Commands", "/commands", true, nil),
		"zero_pages":  pageList("Zero-based pages", "/zero-pages", true, new(float64(0))),
		"positions":   offsetList("Positions", "/positions", "limit", true, nil, new(float64(1))),
		"tokens":      tokenList("Tokens", "/tokens", "starting_after", "cursor"),
		"page_tokens": tokenList("Page tokens", "/page-tokens", "page_token", "page_token"),
		"optional_offsets": {
			Description: "Optional offsets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/optional-offsets",
					Description: "List optional offsets",
					Response:    spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "offset", In: "query", Type: "integer"},
						{Name: "limit", In: "query", Type: "integer", Default: 2},
					},
					Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit"},
				},
			},
		},
		"servers": {
			Description: "Servers",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/servers",
					Description: "List servers",
					Response:    spec.ResponseDef{Type: "array"},
				},
			},
		},
		"child_reviews": childList("Child reviews", "/child-reviews", "offset", "offset", true, nil),
		"child_pages":   childList("Child pages", "/child-pages", "page", "page", true, new(float64(0))),
	}
	return apiSpec
}

func offsetList(description, path, limitParam string, required bool, defaultValue any, minimum *float64) spec.Resource {
	return spec.Resource{
		Description: description,
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        path,
				Description: "List " + description,
				Response:    spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "offset", In: "query", Type: "integer", Required: required, Default: defaultValue, Minimum: minimum},
					{Name: limitParam, In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: limitParam},
			},
		},
	}
}

func pageList(description, path string, required bool, minimum *float64) spec.Resource {
	return spec.Resource{
		Description: description,
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        path,
				Description: "List " + description,
				Response:    spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "page", In: "query", Type: "integer", Required: required, Minimum: minimum},
					{Name: "per_page", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "per_page"},
			},
		},
	}
}

func tokenList(description, path, cursorParam, cursorType string) spec.Resource {
	return spec.Resource{
		Description: description,
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        path,
				Description: "List " + description,
				Response:    spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: cursorParam, In: "query", Type: "string", Required: true},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: cursorType, CursorParam: cursorParam, LimitParam: "limit"},
			},
		},
	}
}

func childList(description, path, cursorParam, cursorType string, required bool, minimum *float64) spec.Resource {
	return spec.Resource{
		Description: description,
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        path,
				Description: "List " + description,
				Response:    spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "server_id", In: "query", Type: "string", Required: true},
					{Name: cursorParam, In: "query", Type: "integer", Required: required, Minimum: minimum},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: cursorType, CursorParam: cursorParam, LimitParam: "limit"},
				Walker:     &spec.WalkerConfig{Parent: "servers", KeyField: "id", KeyParam: "server_id"},
			},
		},
	}
}

func requireSyncable(t *testing.T, profile *profiler.APIProfile, name string) profiler.SyncableResource {
	t.Helper()
	for _, resource := range profile.SyncableResources {
		if resource.Name == name {
			return resource
		}
	}
	t.Fatalf("syncable resource %q missing", name)
	return profiler.SyncableResource{}
}

func requireDependent(t *testing.T, profile *profiler.APIProfile, name string) profiler.DependentResource {
	t.Helper()
	var names []string
	for _, resource := range profile.DependentSyncResources {
		names = append(names, resource.ParentResource+"/"+resource.Name)
		if resource.Name == name {
			return resource
		}
	}
	t.Fatalf("dependent resource %q missing from %v", name, names)
	return profiler.DependentResource{}
}

// TestGeneratedSyncSendsFirstPageOffsetAndPage is the generated-module proof
// that offset and page pagination send a position on request one, and that a
// token cursor does not.
func TestGeneratedSyncSendsFirstPageOffsetAndPage(t *testing.T) {
	t.Parallel()

	apiSpec := firstPageOffsetSpec("firstpage")
	profile := profiler.Profile(apiSpec)

	bounces := requireSyncable(t, profile, "bounces")
	assert.Equal(t, "offset", bounces.PaginationCursorType)
	assert.Equal(t, "0", bounces.PaginationInitialCursor)
	assert.Equal(t, 2, bounces.PaginationPageSize)
	assert.Contains(t, bounces.RequiredQueryParams, "offset")
	assert.NotContains(t, bounces.RequiredQueryParams, "count")

	reviews := requireSyncable(t, profile, "reviews")
	assert.Equal(t, "0", reviews.PaginationInitialCursor)
	assert.Equal(t, 2, reviews.PaginationPageSize)

	commands := requireSyncable(t, profile, "commands")
	assert.Equal(t, "page", commands.PaginationCursorType)
	assert.Equal(t, "1", commands.PaginationInitialCursor)
	assert.Contains(t, commands.RequiredQueryParams, "page")
	assert.NotContains(t, commands.RequiredQueryParams, "per_page")

	zeroPages := requireSyncable(t, profile, "zero_pages")
	assert.Equal(t, "0", zeroPages.PaginationInitialCursor)

	positions := requireSyncable(t, profile, "positions")
	assert.Equal(t, "1", positions.PaginationInitialCursor)
	assert.NotContains(t, positions.QueryParamDefaults, profiler.SyncQueryParamDefault{Name: "offset", Value: "1"})

	tokens := requireSyncable(t, profile, "tokens")
	assert.Empty(t, tokens.PaginationInitialCursor)
	assert.NotContains(t, tokens.RequiredQueryParams, "starting_after")

	pageTokens := requireSyncable(t, profile, "page_tokens")
	assert.Empty(t, pageTokens.PaginationInitialCursor)
	assert.NotContains(t, pageTokens.RequiredQueryParams, "page_token")

	optional := requireSyncable(t, profile, "optional_offsets")
	assert.Empty(t, optional.PaginationInitialCursor)

	childReviews := requireDependent(t, profile, "child_reviews")
	assert.Equal(t, "servers", childReviews.ParentResource)
	assert.Equal(t, "offset", childReviews.PaginationCursorType)
	assert.Equal(t, "0", childReviews.PaginationInitialCursor)
	assert.Equal(t, 2, childReviews.PaginationPageSize)

	childPages := requireDependent(t, profile, "child_pages")
	assert.Equal(t, "servers", childPages.ParentResource)
	assert.Equal(t, "page", childPages.PaginationCursorType)
	assert.Equal(t, "0", childPages.PaginationInitialCursor)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Equal(t, 2, strings.Count(syncSrc, "cursor = seedSyncPageCursor(params, pageSize, cursor)"))
	assert.Equal(t, 2, strings.Count(syncSrc, "nextCursor = nextSyncPageNumber(cursor)"))
	assert.Contains(t, syncSrc, `case "servers/child_reviews":`)
	assert.Contains(t, syncSrc, `case "servers/child_pages":`)

	autoSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auto_refresh.go")
	assert.Contains(t, autoSrc, "syncConditionalQueryParams(resource, determinePaginationDefaults(resource), \"\", effectiveSince, sortValue)")

	requireGeneratedCompiles(t, outputDir)

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type pagingClient struct {
	got []map[string]string
}

func (c *pagingClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.got = append(c.got, copied)
	if len(c.got) == 1 {
		return json.RawMessage(` + "`" + `[{"id":"one"},{"id":"two"}]` + "`" + `), nil
	}
	return json.RawMessage(` + "`" + `[{"id":"three"}]` + "`" + `), nil
}

func (*pagingClient) RateLimit() float64 { return 0 }

func openPagingStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func syncFlat(t *testing.T, resource string, maxPages int) *pagingClient {
	t.Helper()
	client := &pagingClient{}
	res := syncResource(context.Background(), client, openPagingStore(t), resource, "", false, maxPages, false, false, nil, nil)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync %s: err=%v warn=%v calls=%v", resource, res.Err, res.Warn, client.got)
	}
	return client
}

func assertCursorValues(t *testing.T, got []map[string]string, key string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s calls=%d params=%v, want %v", key, len(got), got, want)
	}
	for i, value := range want {
		if got[i][key] != value {
			t.Fatalf("call %d %s=%q, want %q (params %#v)", i, key, got[i][key], value, got[i])
		}
	}
}

func TestSyncBouncesSendsOffsetZeroThenPageSize(t *testing.T) {
	client := syncFlat(t, "bounces", 2)
	assertCursorValues(t, client.got, "offset", []string{"0", "2"})
	if client.got[0]["count"] != "2" {
		t.Fatalf("count=%q, want 2", client.got[0]["count"])
	}
}

func TestSyncReviewsSendsOffsetZero(t *testing.T) {
	client := syncFlat(t, "reviews", 1)
	assertCursorValues(t, client.got, "offset", []string{"0"})
}

func TestSyncCommandsSendsPageOneThenTwo(t *testing.T) {
	client := syncFlat(t, "commands", 2)
	assertCursorValues(t, client.got, "page", []string{"1", "2"})
}

func TestSyncZeroPagesSendsPageZeroThenOne(t *testing.T) {
	client := syncFlat(t, "zero_pages", 2)
	assertCursorValues(t, client.got, "page", []string{"0", "1"})
}

func TestSyncPositionsSendsOneBasedOffset(t *testing.T) {
	client := syncFlat(t, "positions", 2)
	assertCursorValues(t, client.got, "offset", []string{"1", "3"})
}

func TestSyncTokensOmitsCursorOnFirstPage(t *testing.T) {
	client := syncFlat(t, "tokens", 1)
	if len(client.got) != 1 {
		t.Fatalf("calls=%d, want 1 (%v)", len(client.got), client.got)
	}
	if _, ok := client.got[0]["starting_after"]; ok {
		t.Fatalf("starting_after=%q, want absent", client.got[0]["starting_after"])
	}
}

func TestSyncPageTokensOmitsCursorOnFirstPage(t *testing.T) {
	client := syncFlat(t, "page_tokens", 1)
	if _, ok := client.got[0]["page_token"]; ok {
		t.Fatalf("page_token=%q, want absent", client.got[0]["page_token"])
	}
}

func TestSyncOptionalOffsetOmitsCursorOnFirstPage(t *testing.T) {
	client := syncFlat(t, "optional_offsets", 1)
	if _, ok := client.got[0]["offset"]; ok {
		t.Fatalf("offset=%q, want absent", client.got[0]["offset"])
	}
}

func TestSyncUserOffsetOverridesFirstPageSeed(t *testing.T) {
	client := &pagingClient{}
	userParams, err := parseSyncUserParams([]string{"offset=9"}, nil, nil)
	if err != nil {
		t.Fatalf("parseSyncUserParams: %v", err)
	}
	res := syncResource(context.Background(), client, openPagingStore(t), "bounces", "", false, 1, false, false, userParams, nil)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync: err=%v warn=%v", res.Err, res.Warn)
	}
	assertCursorValues(t, client.got, "offset", []string{"9"})
}

func TestSyncResumeCursorWinsOverFirstPageSeed(t *testing.T) {
	db := openPagingStore(t)
	if err := db.Upsert("bounces", "saved", []byte(` + "`" + `{"id":"saved"}` + "`" + `)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.SaveSyncProgress("bounces", "40", 1); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	client := &pagingClient{}
	res := syncResource(context.Background(), client, db, "bounces", "", false, 1, false, false, nil, nil)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync: err=%v warn=%v calls=%v", res.Err, res.Warn, client.got)
	}
	assertCursorValues(t, client.got, "offset", []string{"40"})
}

func dependentDef(t *testing.T, name string) dependentResourceDef {
	t.Helper()
	for _, dep := range dependentResourceDefs() {
		if dep.Name == name {
			return dep
		}
	}
	t.Fatalf("dependent %s missing", name)
	return dependentResourceDef{}
}

func TestSyncDependentReviewsSendsOffsetZero(t *testing.T) {
	db := openPagingStore(t)
	if err := db.Upsert("servers", "srv-1", []byte(` + "`" + `{"id":"srv-1"}` + "`" + `)); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	client := &pagingClient{}
	res := syncDependentResource(context.Background(), client, db, dependentDef(t, "child_reviews"), "", false, 0, false, false, nil, nil, 1)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync: err=%v warn=%v calls=%v", res.Err, res.Warn, client.got)
	}
	assertCursorValues(t, client.got, "offset", []string{"0", "2"})
	if client.got[0]["server_id"] != "srv-1" {
		t.Fatalf("server_id=%q, want srv-1", client.got[0]["server_id"])
	}
}

func TestSyncDependentPagesSendsPageZeroThenOne(t *testing.T) {
	db := openPagingStore(t)
	if err := db.Upsert("servers", "srv-1", []byte(` + "`" + `{"id":"srv-1"}` + "`" + `)); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	client := &pagingClient{}
	res := syncDependentResource(context.Background(), client, db, dependentDef(t, "child_pages"), "", false, 0, false, false, nil, nil, 1)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync: err=%v warn=%v calls=%v", res.Err, res.Warn, client.got)
	}
	assertCursorValues(t, client.got, "page", []string{"0", "1"})
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_first_page_offset_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestSync(BouncesSendsOffsetZeroThenPageSize|ReviewsSendsOffsetZero|CommandsSendsPageOneThenTwo|ZeroPagesSendsPageZeroThenOne|PositionsSendsOneBasedOffset|TokensOmitsCursorOnFirstPage|PageTokensOmitsCursorOnFirstPage|OptionalOffsetOmitsCursorOnFirstPage|UserOffsetOverridesFirstPageSeed|ResumeCursorWinsOverFirstPageSeed|DependentReviewsSendsOffsetZero|DependentPagesSendsPageZeroThenOne)")
}
