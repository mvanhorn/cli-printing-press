package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedLocalReadsAndWhichHonorSharedRuntimeContracts(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "shopsapi",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Learn:   spec.LearnConfig{Disabled: true},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopsapi-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"shops": {
				Description: "Shops",
				Endpoints: map[string]spec.Endpoint{
					"index": {
						Method:      "GET",
						Path:        "/shops",
						Description: "List shops",
						Params: []spec.Param{
							{Name: "status", Type: "string"},
							{Name: "per_page", Type: "integer"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Shop"},
					},
					"get": {
						Method:      "GET",
						Path:        "/shops/{id}",
						Description: "Get a shop",
						Params:      []spec.Param{{Name: "id", Type: "string", Positional: true, PathParam: true}},
						Response:    spec.ResponseDef{Type: "object", Item: "Shop"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Shop": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "status", Type: "string"},
					{Name: "description", Type: "string"},
					{Name: "revenue", Type: "number"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	gen.NovelFeatures = []NovelFeature{
		{Name: "Search shops", Command: "search", Description: "Full-text search across synced shops", Group: "Local state"},
	}
	require.NoError(t, gen.Generate())

	indexSrc := readGeneratedFile(t, outputDir, "internal", "cli", "shops_index.go")
	assert.Contains(t, indexSrc, `"shops", false, path, params`,
		"an unscoped collection not named list must still be generated as isList=false; resolveLocal has to recover the collection path")

	dataSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	assert.Contains(t, dataSrc, "func localReadPathIsCollection(",
		"resolveLocal must distinguish collection paths from object IDs")
	assert.Contains(t, dataSrc, "func applyLocalListFilters(",
		"local list reads must apply supported query filters")
	assert.Contains(t, dataSrc, "func loadLocalList(",
		"local list reads must bound/stream instead of List(resourceType, 0)")
	assert.NotContains(t, stripGoComments(dataSrc), "db.List(resourceType, 0)",
		"local list must not load the entire generic partition before take/filters")
	assert.Contains(t, dataSrc, "func applyLocalParentScope(",
		"nested collection paths must constrain List results to the parent segment")
	assert.Contains(t, dataSrc, "func localItemStoredParentMatches(",
		"parent scope must use stored parent_id / path parent FK, not every *_id field")
	assert.Contains(t, dataSrc, "func localReadImmediateParent(",
		"composite and JSON parent matches must use the immediate path parent, not any ancestor ID")
	assert.NotContains(t, dataSrc, "localFieldLooksLikeParentKey")
	assert.Contains(t, dataSrc, "localListControlParams",
		"query controls such as sort/order/search must not become equality filters")
	assert.Contains(t, dataSrc, "localQueryUnsupportedError",
		"a local read must fail closed when a row-selecting parameter cannot be applied")
	assert.Contains(t, dataSrc, "use --data-source live")
	assert.Contains(t, dataSrc, "func resolveStoredResourceType(")
	assert.NotContains(t, dataSrc, "local data is unfiltered")

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, "validateDataSourceStrategy(flags, commandDataSourceAnnotation(cmd))",
		"root must reject a --data-source value the command annotation cannot serve")

	requireGeneratedCompiles(t, outputDir)

	whichSrc := readGeneratedFile(t, outputDir, "internal", "cli", "which.go")
	assert.Contains(t, whichSrc, `return usageErr(fmt.Errorf("no match for %q;`)
	assert.Contains(t, whichSrc, `"matches": []whichMatch{}`)
	assert.NotContains(t, whichSrc, "Under --json, return an empty matches envelope at exit 0")
	assert.Contains(t, whichSrc, "if len(leafTokens) < 2 && unmatched >= score {",
		"single-token leaves must keep a positive score when specificity is the only remaining penalty")
	assert.Contains(t, whichSrc, "func whichIncidentalToken(token string) bool {",
		"incidental query words must not create a which match")
	assert.Contains(t, whichSrc, "whichIncidentalToken(qt) && !whichTokenMatch(qt, leaf)",
		"filler words credit a command only when they are the whole unsplit leaf")
	assert.Contains(t, whichSrc, "whichIncidentalToken(qt) && !whichTokenMatch(qt, group)",
		"filler words credit a group only when they are the whole group name")
	assert.NotContains(t, whichSrc, "func whichTokensContain(",
		"hyphen-split sub-tokens must not grant filler command credit")

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "machineFormat := wantsMachineOutput(flags)")
	assert.Contains(t, syncSrc, "printJSONFiltered(cmd.OutOrStdout()")

	testPath := filepath.Join(outputDir, "internal", "cli", "issue3549_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"shopsapi-pp-cli/internal/client"
	"shopsapi-pp-cli/internal/config"
	"shopsapi-pp-cli/internal/store"
)

func seedShopsStore(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	db, err := store.OpenWithContext(context.Background(), defaultDBPath("shopsapi-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	rows := []struct {
		id   string
		body string
	}{
		{"s1", `+"`"+`{"id":"s1","name":"Alpha","status":"active","description":"verbose","revenue":10}`+"`"+`},
		{"s2", `+"`"+`{"id":"s2","name":"Beta","status":"paused","description":"verbose","revenue":20}`+"`"+`},
	}
	for _, row := range rows {
		if err := db.Upsert("shops", row.id, json.RawMessage(row.body)); err != nil {
			t.Fatalf("upsert %s: %v", row.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func TestResolveLocalCollectionPathDoesNotUseResourceNameAsID(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, ioDiscard(), "shops", false, "/shops", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal collection path: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("listed %d shops, want 2: %s", len(items), data)
	}
}

func TestResolveLocalAppliesEqualityAndLimitFilters(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, ioDiscard(), "shops", true, "/shops", map[string]string{
		"status":   "active",
		"per_page": "1",
	}, "test")
	if err != nil {
		t.Fatalf("resolveLocal filters: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 1 || items[0]["id"] != "s1" {
		t.Fatalf("filtered shops = %#v, want the active row only", items)
	}
}

func TestResolveLocalGetByIDStillWorks(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, ioDiscard(), "shops", false, "/shops/s2", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal get: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("expected a JSON object, got %s: %v", data, err)
	}
	if obj["id"] != "s2" {
		t.Fatalf("got %#v, want shop s2", obj)
	}
}

func TestResolveLocalRejectsUnsupportedCursor(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "shops", true, "/shops", map[string]string{
		"cursor": "abc",
	}, "test")
	if data != nil {
		t.Fatalf("cursor read returned %s", data)
	}
	requireLocalUnsupported(t, err, "cursor")
}

func seedNestedShopsStore(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	db, err := store.OpenWithContext(context.Background(), defaultDBPath("shopsapi-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	rows := []struct {
		id   string
		body string
	}{
		{"s1\x00t1", `+"`"+`{"id":"s1","name":"Team One","status":"active","parent_id":"t1"}`+"`"+`},
		{"s2\x00t2", `+"`"+`{"id":"s2","name":"Team Two","status":"active","parent_id":"t2","owner_id":"t1"}`+"`"+`},
		{"s3\x00t1", `+"`"+`{"id":"s3","name":"Team One B","status":"paused","parent_id":"t1"}`+"`"+`},
	}
	for _, row := range rows {
		if err := db.Upsert("shops", row.id, json.RawMessage(row.body)); err != nil {
			t.Fatalf("upsert %s: %v", row.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func TestResolveLocalNestedCollectionStaysInParentScope(t *testing.T) {
	seedNestedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, ioDiscard(), "shops", true, "/teams/t1/shops", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal nested collection: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("listed %d shops for team t1, want 2: %s", len(items), data)
	}
	for _, item := range items {
		if item["parent_id"] != "t1" {
			t.Fatalf("nested list leaked shop %#v", item)
		}
		if item["id"] == "s2" {
			t.Fatalf("owner_id t1 must not pull a t2-scoped shop into /teams/t1/shops: %#v", item)
		}
	}
}

func seedDeepNestedShopsStore(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	db, err := store.OpenWithContext(context.Background(), defaultDBPath("shopsapi-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	rows := []struct {
		id   string
		body string
	}{
		{"s1\x00m1", `+"`"+`{"id":"s1","name":"Message One","parent_id":"m1","channel_id":"c1"}`+"`"+`},
		{"s2\x00c1", `+"`"+`{"id":"s2","name":"Channel Scoped","parent_id":"c1"}`+"`"+`},
		{"s3\x00m2", `+"`"+`{"id":"s3","name":"Message Two","parent_id":"m2","channel_id":"c1"}`+"`"+`},
	}
	for _, row := range rows {
		if err := db.Upsert("shops", row.id, json.RawMessage(row.body)); err != nil {
			t.Fatalf("upsert %s: %v", row.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func TestResolveLocalNestedCollectionUsesImmediateParent(t *testing.T) {
	seedDeepNestedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, ioDiscard(), "shops", true, "/channels/c1/messages/m1/shops", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal deep nested collection: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 1 || items[0]["id"] != "s1" {
		t.Fatalf("deep nested list = %#v, want only the m1-scoped row", items)
	}
}

func TestResolveLocalRejectsRowSelectingControls(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "shops", true, "/shops", map[string]string{
		"sort":   "name",
		"order":  "asc",
		"search": "alpha",
	}, "test")
	if data != nil {
		t.Fatalf("row-selecting read returned %s", data)
	}
	requireLocalUnsupported(t, err, "sort", "order", "search")
}

func TestResolveLocalProjectionStaysAWarning(t *testing.T) {
	seedShopsStore(t)
	var warn bytes.Buffer
	data, _, err := resolveLocal(context.Background(), nil, &warn, "shops", true, "/shops", map[string]string{
		"fields":  "id,name",
		"include": "owner",
	}, "test")
	if err != nil {
		t.Fatalf("resolveLocal projection: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("projection emptied the list: %s", data)
	}
	for _, key := range []string{"fields", "include"} {
		if !strings.Contains(warn.String(), key) {
			t.Fatalf("expected projection warning for %s, got %q", key, warn.String())
		}
	}
}

func TestResolveLocalUnmatchedEqualityKeyFailsClosed(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "shops", true, "/shops", map[string]string{
		"not_a_field": "x",
	}, "test")
	if data != nil {
		t.Fatalf("unmatched key returned %s", data)
	}
	requireLocalUnsupported(t, err, "not_a_field")
}

func TestResolveLocalFilterDoesNotReturnUnfilteredRows(t *testing.T) {
	seedShopsStore(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "unreachable", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := client.New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
	flags := &rootFlags{dataSource: "local"}
	data, _, err := resolveRead(context.Background(), c, flags, "shops", true, "/shops", map[string]string{
		"filter": "status:eq('active')",
	}, nil, io.Discard)
	if data != nil {
		t.Fatalf("filter read returned %s", data)
	}
	requireLocalUnsupported(t, err, "filter")
	if hits != 0 {
		t.Fatalf("local read made %d HTTP calls", hits)
	}
}

func TestResolveLocalOrderByWithLimitDoesNotCutUnorderedRows(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "shops", true, "/shops", map[string]string{
		"orderBy": "desc(id)",
		"limit":   "1",
	}, "test")
	if data != nil {
		t.Fatalf("orderBy+limit returned %s", data)
	}
	requireLocalUnsupported(t, err, "orderBy")
}

func TestResolveLocalEmptyStoreNamesMissingDataBeforeSelectors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	db, err := store.OpenWithContext(context.Background(), defaultDBPath("shopsapi-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "shops", true, "/shops", map[string]string{
		"filter": "status:eq('active')",
	}, "test")
	if data != nil {
		t.Fatalf("empty store returned %s", data)
	}
	if err == nil || !strings.Contains(err.Error(), "no local data for \"shops\"") {
		t.Fatalf("err = %v, want no local data", err)
	}
	if strings.Contains(err.Error(), "could not apply") {
		t.Fatalf("empty store reported an unapplied selector: %v", err)
	}
}

func TestResolveLocalAutoOfflineDoesNotReturnUnfilteredRows(t *testing.T) {
	seedShopsStore(t)
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c := client.New(&config.Config{BaseURL: "http://" + addr}, 5*time.Second, 0)
	flags := &rootFlags{dataSource: "auto"}
	data, _, err := resolveRead(context.Background(), c, flags, "shops", true, "/shops", map[string]string{
		"filter": "status:eq('active')",
	}, nil, io.Discard)
	if data != nil {
		t.Fatalf("auto offline filter returned %s", data)
	}
	if err == nil || !strings.Contains(err.Error(), "API unreachable") || !strings.Contains(err.Error(), "filter") || !strings.Contains(err.Error(), "--data-source live") {
		t.Fatalf("err = %v", err)
	}
	data, prov, err := resolveRead(context.Background(), c, flags, "shops", true, "/shops", nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("auto offline unfiltered: %v", err)
	}
	if prov.Source != "local" {
		t.Fatalf("source = %q, want local", prov.Source)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("auto offline listed %d shops, want 2: %s", len(items), data)
	}
}

func TestResolveLocalParentScopedSubCollection(t *testing.T) {
	seedTypedRows(t, "tagged_resources", []seedRow{
		{"r1", "{\"id\":\"r1\",\"parent_id\":\"tag-a\",\"name\":\"alpha\"}"},
		{"r2", "{\"id\":\"r2\",\"parent_id\":\"tag-b\",\"name\":\"beta\"}"},
	})
	for _, resourceType := range []string{"taggedResources", "tagged_resources"} {
		parents := localReadPathParents(resourceType, "/api/v2/tags/tag-a/taggedResources")
		if len(parents) != 1 || parents[0].ID != "tag-a" {
			t.Fatalf("%s parents = %#v, want tag-a", resourceType, parents)
		}
		data, _, err := resolveLocal(context.Background(), nil, io.Discard, resourceType, true, "/api/v2/tags/tag-a/taggedResources", nil, "test")
		if err != nil {
			t.Fatalf("%s resolveLocal: %v", resourceType, err)
		}
		var items []map[string]any
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("expected a JSON array, got %s: %v", data, err)
		}
		if len(items) != 1 || items[0]["id"] != "r1" || items[0]["parent_id"] != "tag-a" {
			t.Fatalf("%s rows = %#v, want only tag-a", resourceType, items)
		}
	}
}

func TestResolveLocalShardedParentSubCollection(t *testing.T) {
	seedTypedRows(t, "tags_tagged_resources", []seedRow{
		{"r1", "{\"id\":\"r1\",\"parent_id\":\"tag-a\",\"name\":\"alpha\"}"},
		{"r2", "{\"id\":\"r2\",\"parent_id\":\"tag-b\",\"name\":\"beta\"}"},
	})
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "taggedResources", true, "/api/v2/tags/tag-a/taggedResources", nil, "test")
	if err != nil {
		t.Fatalf("sharded resolveLocal: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 1 || items[0]["parent_id"] != "tag-a" {
		t.Fatalf("sharded rows = %#v, want only tag-a", items)
	}
}

func TestResolveLocalKebabTopLevelAlias(t *testing.T) {
	seedTypedRows(t, "reconciliation-policies", []seedRow{
		{"p1", "{\"id\":\"p1\",\"name\":\"one\"}"},
		{"p2", "{\"id\":\"p2\",\"name\":\"two\"}"},
	})
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "reconciliationPolicies", true, "/api/v2/reconciliationPolicies", nil, "test")
	if err != nil {
		t.Fatalf("kebab alias: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("kebab alias listed %d rows, want 2: %s", len(items), data)
	}
}

func TestResolveLocalUnrelatedSuffixDoesNotPanic(t *testing.T) {
	parents := localReadPathParents("html_posts", "/posts")
	if parents != nil {
		t.Fatalf("parents = %#v, want none", parents)
	}
	if !localReadPathIsCollection("html_posts", "/posts", true) {
		t.Fatal("isList must still be a collection")
	}
	if localReadPathIsCollection("html_posts", "/posts", false) {
		t.Fatal("a non-matching single segment must stay an object path")
	}
}

func TestResolveLocalUnknownResourceStillNamesTheRequest(t *testing.T) {
	seedShopsStore(t)
	data, _, err := resolveLocal(context.Background(), nil, io.Discard, "doesNotExist", true, "/doesNotExist", nil, "test")
	if data != nil {
		t.Fatalf("unknown resource returned %s", data)
	}
	if err == nil || !strings.Contains(err.Error(), "no local data for \"doesNotExist\"") {
		t.Fatalf("err = %v", err)
	}
}

func TestDataSourceAnnotationGatesFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	cases := []struct {
		name       string
		annotation string
		args       []string
		wantErr    string
	}{
		{"live rejects local", "live", []string{"--data-source", "local", "probe"}, "no local data source"},
		{"local rejects live", "local", []string{"--data-source", "live", "probe"}, "no live equivalent"},
		{"live allows live", "live", []string{"--data-source", "live", "probe"}, ""},
		{"live allows default auto", "live", []string{"probe"}, ""},
		{"local allows local", "local", []string{"--data-source", "local", "probe"}, ""},
		{"local allows auto", "local", []string{"--data-source", "auto", "probe"}, ""},
		{"computed allows live", "computed", []string{"--data-source", "live", "probe"}, ""},
		{"unannotated allows local", "", []string{"--data-source", "local", "probe"}, ""},
		{"auto allows local", "auto", []string{"--data-source", "local", "probe"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits = 0
			ran := false
			root := newRootCmd(&rootFlags{})
			probe := &cobra.Command{
				Use: "probe",
				RunE: func(cmd *cobra.Command, args []string) error {
					ran = true
					req, err := http.NewRequest(http.MethodGet, srv.URL+"/probe", nil)
					if err != nil {
						return err
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return err
					}
					resp.Body.Close()
					return nil
				},
			}
			if tc.annotation != "" {
				probe.Annotations = map[string]string{"pp:data-source": tc.annotation}
			}
			root.AddCommand(probe)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(tc.args)
			err := root.Execute()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if ExitCode(err) == 0 {
					t.Fatal("incompatible data source exited 0")
				}
				if ran {
					t.Fatal("RunE ran before the annotation gate")
				}
				if hits != 0 {
					t.Fatalf("incompatible request made %d HTTP calls", hits)
				}
				if _, statErr := os.Stat(defaultDBPath("shopsapi-pp-cli")); !os.IsNotExist(statErr) {
					t.Fatalf("store opened or created: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("compatible request: %v", err)
			}
			if !ran {
				t.Fatal("compatible request did not run")
			}
		})
	}
}

type seedRow struct {
	id   string
	body string
}

func seedTypedRows(t *testing.T, resourceType string, rows []seedRow) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	db, err := store.OpenWithContext(context.Background(), defaultDBPath("shopsapi-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, row := range rows {
		if err := db.Upsert(resourceType, row.id, json.RawMessage(row.body)); err != nil {
			t.Fatalf("upsert %s: %v", row.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func requireLocalUnsupported(t *testing.T, err error, params ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected local query to fail closed")
	}
	msg := err.Error()
	if !strings.Contains(msg, "use --data-source live") {
		t.Fatalf("error = %q, want --data-source live", msg)
	}
	for _, param := range params {
		if !strings.Contains(msg, param) {
			t.Fatalf("error = %q, missing %s", msg, param)
		}
	}
	if ExitCode(err) == 0 {
		t.Fatalf("exit 0 for %v", err)
	}
}

func ioDiscard() io.Writer { return io.Discard }
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestResolveLocal|TestDataSource|TestWhichJSONNoMatchExits2|TestWhichPipedNoMatchExits2WithEmptyEnvelope|TestRankWhich_SingleTokenLeaf|TestRankWhich_CompositeLeafLosesWhenQueryOmitsCapabilityTokens|TestRankWhich_ProseCreditDoesNotDoubleCount|TestRankWhich_IncidentalDescriptionWordDoesNotAdmitEntry|TestRankWhich_OneCharacterCommandLeafMatches|TestRankWhich_FillerSubtokenDoesNotAdmitHyphenatedLeaf", "-count=1")
}
