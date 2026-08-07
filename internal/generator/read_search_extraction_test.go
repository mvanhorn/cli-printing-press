package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWrapWithProvenanceRejectsNonJSON(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("provenance-nonjson")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "provenance_nonjson_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWrapWithProvenanceRejectsNonJSON(t *testing.T) {
	_, err := wrapWithProvenance(json.RawMessage("<html><form>login</form></html>"), DataProvenance{Source: "live"})
	if err == nil {
		t.Fatalf("wrapWithProvenance accepted non-JSON live data")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("HTML live body should be classified as an auth/session problem, got: %v", err)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestWrapWithProvenanceRejectsNonJSON", "-count=1")
}

func TestGeneratedLiveReadGuardsHTMLBeforeCache(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("read-html-guard")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	require.Contains(t, dataSourceSrc, "assertLiveJSONBody(data)")
	require.Less(t,
		strings.Index(dataSourceSrc, "assertLiveJSONBody(data)"),
		strings.Index(dataSourceSrc, "writeThroughCache(ctx, resourceType, data)"),
		"live auto reads must reject HTML/non-JSON before write-through caching")
}

func TestGeneratedSyncExtractionHonorsResponsePath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("sync-response-path")
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/widgets",
					Description:  "List widgets",
					Response:     spec.ResponseDef{Type: "array", Item: "Widget"},
					ResponsePath: "response.data",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Widget": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "name", Type: "string"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, `responsePathForResource(resource, path)`)
	require.Contains(t, syncSrc, `extractPageItems(data, pageSize.cursorParam, responsePathForResource(resource, path)...)`)

	testPath := filepath.Join(outputDir, "internal", "cli", "sync_response_path_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractPageItemsHonorsResponsePath(t *testing.T) {
	body := json.RawMessage(`+"`"+`{"message":"ok","response":{"data":[{"id":"w1"},{"id":"w2"}],"next_cursor":"page-2","has_more":true}}`+"`"+`)
	items, cursor, hasMore := extractPageItems(body, "cursor", "response.data")
	if len(items) != 2 {
		t.Fatalf("response_path extraction got %d items, want 2", len(items))
	}
	if cursor != "page-2" || !hasMore {
		t.Fatalf("response_path cursor = %q/%v, want page-2/true", cursor, hasMore)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestExtractPageItemsHonorsResponsePath", "-count=1")
}

func TestGeneratedSyncHydratesScalarIDListItems(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scalar-id-hydration")
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{
			{Name: "id", Type: "integer"},
			{Name: "title", Type: "string"},
		}},
		"Updates": {Fields: []spec.TypeField{
			{Name: "items", Type: "array"},
			{Name: "profiles", Type: "array"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"stories": {
			Description: "Stories",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/jobstories.json",
					Description: "List story IDs",
					Response:    spec.ResponseDef{Type: "array", Item: "int"},
					Params:      []spec.Param{{Name: "limit", Type: "integer", Default: 2}, {Name: "offset", Type: "integer"}},
					Pagination:  &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit"},
				},
			},
		},
		"updates": {
			Description: "Updates",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/updates.json",
					Description: "List update IDs",
					Response:    spec.ResponseDef{Type: "object", Item: "Updates"},
				},
			},
		},
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/item/{id}.json",
					Description: "Get item",
					Response:    spec.ResponseDef{Type: "object", Item: "Item"},
					IDField:     "id",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, `"stories": {path: "/item/{id}.json", idParam: "id"}`)
	require.Contains(t, syncSrc, `"updates": {path: "/item/{id}.json", idParam: "id"}`)
	require.Contains(t, syncSrc, `fetchedThisPage := len(items)`)
	require.Contains(t, syncSrc, `consumedTotal += fetchedThisPage`)
	require.Contains(t, syncSrc, `truncatedByCap = truncatedByCap && !shortPageEndsPagination(pageSize.cursorType, fetchedThisPage, pageSize.limit)`)

	testPath := filepath.Join(outputDir, "internal", "cli", "sync_hydration_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"`+naming.CLI(apiSpec.Name)+`/internal/store"
)

type fakeHydrateClient struct {
	responses map[string]json.RawMessage
	errs      map[string]error
	calls     []string
}

func (f *fakeHydrateClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	f.calls = append(f.calls, path)
	if err, ok := f.errs[path]; ok {
		return nil, err
	}
	if response, ok := f.responses[path]; ok {
		return response, nil
	}
	return json.RawMessage(`+"`"+`null`+"`"+`), nil
}

func (f *fakeHydrateClient) RateLimit() float64 {
	return 0
}

func openHydrationTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	return db
}

func TestHydrateScalarItemsHydratesDirectAndWrapperIDs(t *testing.T) {
	body := json.RawMessage(`+"`"+`{"items":[1,"two words"],"profiles":[99]}`+"`"+`)
	items, _, _ := extractPageItems(body, "")
	if len(items) != 2 {
		t.Fatalf("wrapper scalar item extraction got %d items, want 2", len(items))
	}
	client := &fakeHydrateClient{responses: map[string]json.RawMessage{
		"/item/1.json": json.RawMessage(`+"`"+`{"id":1,"title":"one"}`+"`"+`),
		"/item/two%20words.json": json.RawMessage(`+"`"+`{"id":"two words","title":"two"}`+"`"+`),
	}}

	out, failures := hydrateScalarItems(context.Background(), client, "updates", items)
	if failures != 0 || len(out) != 2 {
		t.Fatalf("hydrateScalarItems failures=%d len=%d, want 0/2", failures, len(out))
	}
	if client.calls[1] != "/item/two%20words.json" {
		t.Fatalf("escaped hydrate path = %q, want /item/two%%20words.json", client.calls[1])
	}
}

func TestHydrateScalarItemsCountsFailedHydration(t *testing.T) {
	client := &fakeHydrateClient{
		responses: map[string]json.RawMessage{"/item/2.json": json.RawMessage(`+"`"+`null`+"`"+`)},
		errs:      map[string]error{"/item/1.json": errors.New("boom")},
	}

	out, failures := hydrateScalarItems(context.Background(), client, "stories", []json.RawMessage{
		json.RawMessage(`+"`"+`1`+"`"+`),
		json.RawMessage(`+"`"+`2`+"`"+`),
	})
	if failures != 2 || len(out) != 0 {
		t.Fatalf("hydrateScalarItems failures=%d len=%d, want 2/0", failures, len(out))
	}
}

func TestSyncResourceWarnsOnPartialHydrationFailure(t *testing.T) {
	db := openHydrationTestStore(t)
	client := &fakeHydrateClient{
		responses: map[string]json.RawMessage{
			"/jobstories.json": json.RawMessage(`+"`"+`{"items":[1,2],"has_more":false}`+"`"+`),
			"/item/1.json":    json.RawMessage(`+"`"+`{"id":1,"title":"one"}`+"`"+`),
		},
		errs: map[string]error{"/item/2.json": errors.New("boom")},
	}
	var events bytes.Buffer

	res := syncResource(context.Background(), client, db, "stories", "", true, 0, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Count != 1 {
		t.Fatalf("syncResource count = %d, want 1", res.Count)
	}
	got := events.String()
	for _, want := range []string{`+"`"+`"reason":"scalar_item_hydration_failed"`+"`"+`, `+"`"+`"consumed":2`+"`"+`, `+"`"+`"stored":1`+"`"+`, `+"`"+`"count":1`+"`"+`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sync events missing %s:\n%s", want, got)
		}
	}
}

func TestSyncResourceUsesFetchedPageForHydrationCapWarning(t *testing.T) {
	db := openHydrationTestStore(t)
	client := &fakeHydrateClient{
		responses: map[string]json.RawMessage{
			"/jobstories.json": json.RawMessage(`+"`"+`{"items":[1,2],"has_more":true}`+"`"+`),
		},
		errs: map[string]error{
			"/item/1.json": errors.New("boom"),
			"/item/2.json": errors.New("boom"),
		},
	}
	var events bytes.Buffer

	res := syncResource(context.Background(), client, db, "stories", "", true, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Warn == nil || !strings.Contains(res.Warn.Error(), "scalar item hydration failed") {
		t.Fatalf("syncResource warning = %v, want hydration failure", res.Warn)
	}
	got := events.String()
	for _, want := range []string{`+"`"+`"reason":"all_items_failed_hydration"`+"`"+`, `+"`"+`"reason":"max_pages_cap_hit"`+"`"+`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sync events missing %s:\n%s", want, got)
		}
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "Test(HydrateScalarItems|SyncResource)", "-count=1")
}

func TestGeneratedSearchExtractionHonorsResponsePaths(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("search-response-path")
	apiSpec.Resources = map[string]spec.Resource{
		"photos": {
			Description: "Photos",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/photos",
					Description:  "List photos",
					Response:     spec.ResponseDef{Type: "array", Item: "Photo"},
					ResponsePath: "catalog.items",
				},
				"search": {
					Method:       "GET",
					Path:         "/photos/search",
					Params:       []spec.Param{{Name: "query", Type: "string"}},
					ResponsePath: "photos",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Photo": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "description", Type: "string"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "search.go")
	require.Contains(t, searchSrc, `extractSearchResults(data, searchResponsePaths...)`)

	testPath := filepath.Join(outputDir, "internal", "cli", "search_response_path_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractSearchResultsHonorsResponsePath(t *testing.T) {
	results := extractSearchResults(json.RawMessage(`+"`"+`{"photos":[{"id":"p1"},{"id":"p2"}],"page":1}`+"`"+`), "photos")
	if len(results) != 2 {
		t.Fatalf("response_path search extraction got %d results, want 2", len(results))
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestExtractSearchResultsHonorsResponsePath", "-count=1")
}

func TestGeneratedSearchKeepsFTSHitsWithDomainIdentifier(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("search-domain-id")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "search_domain_id_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
	"testing"
)

func TestSearchFilterKeepsNonEmptyDomainKeyedRows(t *testing.T) {
	row := json.RawMessage(`+"`"+`{"codigo":"1102","descricao":"Compra para comercializacao"}`+"`"+`)
	if isNilOrEmpty(row) {
		t.Fatalf("FTS-matched row with non-empty scalar fields was dropped")
	}
	if !isNilOrEmpty(json.RawMessage(`+"`"+`{"id":null,"name":null}`+"`"+`)) {
		t.Fatalf("null-shell row should still be suppressed")
	}
	if isNilOrEmpty(json.RawMessage(`+"`"+`{"score":0.9,"document":{"codigo":"1102","descricao":"Compra"}}`+"`"+`)) {
		t.Fatalf("search wrapper result should still pass through")
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestSearchFilterKeepsNonEmptyDomainKeyedRows", "-count=1")
}

func TestGeneratedSearchHelpSpecializesToLocalOnlyAndRealTypes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("readwise-like")
	apiSpec.Resources = map[string]spec.Resource{
		"books": {
			Description: "Books",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/books", Description: "List books", Response: spec.ResponseDef{Type: "array", Item: "Book"}},
			},
		},
		"highlights": {
			Description: "Highlights",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/highlights", Description: "List highlights", Response: spec.ResponseDef{Type: "array", Item: "Highlight"}},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Book":      {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "title", Type: "string"}}},
		"Highlight": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "text", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "search.go")
	require.Contains(t, searchSrc, `Short: "Search locally synced data"`)
	require.NotContains(t, searchSrc, "payment failed")
	require.NotContains(t, searchSrc, "--type transactions")
	require.Contains(t, searchSrc, `--type books`)
	require.NotContains(t, searchSrc, "live API")
	require.NotContains(t, searchSrc, "API search endpoint if the API has one")
}

func TestGeneratedPromotedArrayResponseEmitsResults(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-array")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"geo": {
			Description: "Geocoding",
			Endpoints: map[string]spec.Endpoint{
				"geocode": {
					Method:      "GET",
					Path:        "/geocode",
					Description: "Geocode an address",
					Response:    spec.ResponseDef{Type: "array", Item: "Geocode"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Geocode": {Fields: []spec.TypeField{
			{Name: "lat", Type: "number"},
			{Name: "lng", Type: "number"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "promoted_array_response_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromotedArrayResponseEmitsResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/geocode" {
			t.Fatalf("path = %q, want /geocode", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`[{"lat":1.25,"lng":2.5}]`+"`"+`)
	}))
	defer server.Close()
	t.Setenv("PROMOTED_ARRAY_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"geo", "geocode", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}
	var out struct {
		Results []map[string]float64 `+"`json:\"results\"`"+`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(out.Results) != 1 || out.Results[0]["lat"] != 1.25 || out.Results[0]["lng"] != 2.5 {
		t.Fatalf("results = %#v; stdout=%s stderr=%s", out.Results, stdout.String(), stderr.String())
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPromotedArrayResponseEmitsResults", "-count=1")
}
