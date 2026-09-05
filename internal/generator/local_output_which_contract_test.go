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
	assert.Contains(t, dataSrc, "func applyLocalParentScope(",
		"nested collection paths must constrain List results to the parent segment")
	assert.Contains(t, dataSrc, "func localItemStoredParentMatches(",
		"parent scope must use stored parent_id / path parent FK, not every *_id field")
	assert.Contains(t, dataSrc, "func localReadImmediateParent(",
		"composite and JSON parent matches must use the immediate path parent, not any ancestor ID")
	assert.NotContains(t, dataSrc, "localFieldLooksLikeParentKey")
	assert.Contains(t, dataSrc, "localListControlParams",
		"query controls such as sort/order/search must not become equality filters")
	assert.NotContains(t, dataSrc, "local data is unfiltered")

	requireGeneratedCompiles(t, outputDir)

	whichSrc := readGeneratedFile(t, outputDir, "internal", "cli", "which.go")
	assert.Contains(t, whichSrc, `return usageErr(fmt.Errorf("no match for %q;`)
	assert.Contains(t, whichSrc, `"matches": []whichMatch{}`)
	assert.NotContains(t, whichSrc, "Under --json, return an empty matches envelope at exit 0")
	assert.Contains(t, whichSrc, "if len(leafTokens) < 2 && unmatched >= score {",
		"single-token leaves must keep a positive score when specificity is the only remaining penalty")
	assert.Contains(t, whichSrc, "func whichIncidentalToken(token string) bool {",
		"incidental query words must not create a which match")

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
	"path/filepath"
	"strings"
	"testing"

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

func TestResolveLocalWarnsOnUnsupportedCursor(t *testing.T) {
	seedShopsStore(t)
	var warn bytes.Buffer
	_, _, err := resolveLocal(context.Background(), nil, &warn, "shops", true, "/shops", map[string]string{
		"cursor": "abc",
	}, "test")
	if err != nil {
		t.Fatalf("resolveLocal cursor: %v", err)
	}
	if !strings.Contains(warn.String(), "cursor") {
		t.Fatalf("expected unsupported-cursor warning, got %q", warn.String())
	}
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

func TestResolveLocalQueryControlsDoNotEmptyTheList(t *testing.T) {
	seedShopsStore(t)
	var warn bytes.Buffer
	data, _, err := resolveLocal(context.Background(), nil, &warn, "shops", true, "/shops", map[string]string{
		"sort":   "name",
		"order":  "asc",
		"fields": "id,name",
		"search": "alpha",
	}, "test")
	if err != nil {
		t.Fatalf("resolveLocal query controls: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("query controls emptied the list: %s", data)
	}
	for _, key := range []string{"sort", "order", "fields", "search"} {
		if !strings.Contains(warn.String(), key) {
			t.Fatalf("expected unsupported %s warning, got %q", key, warn.String())
		}
	}
}

func TestResolveLocalUnmatchedEqualityKeyDoesNotEmptyTheList(t *testing.T) {
	seedShopsStore(t)
	var warn bytes.Buffer
	data, _, err := resolveLocal(context.Background(), nil, &warn, "shops", true, "/shops", map[string]string{
		"not_a_field": "x",
	}, "test")
	if err != nil {
		t.Fatalf("resolveLocal unmatched key: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("expected a JSON array, got %s: %v", data, err)
	}
	if len(items) != 2 {
		t.Fatalf("unmatched equality key emptied the list: %s", data)
	}
	if !strings.Contains(warn.String(), "not_a_field") {
		t.Fatalf("expected unmatched-key warning, got %q", warn.String())
	}
}

func ioDiscard() io.Writer { return io.Discard }
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestResolveLocal|TestWhichJSONNoMatchExits2|TestWhichPipedNoMatchExits2WithEmptyEnvelope|TestRankWhich_SingleTokenLeaf|TestRankWhich_CompositeLeafLosesWhenQueryOmitsCapabilityTokens|TestRankWhich_ProseCreditDoesNotDoubleCount|TestRankWhich_IncidentalDescriptionWordDoesNotAdmitEntry", "-count=1")
}
