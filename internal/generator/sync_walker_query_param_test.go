package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func walkerQueryParamSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"rooms": {
			Description: "Rooms",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/rooms",
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
		"messages": {
			Description: "Messages scoped by room query param",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/messages",
					Response: spec.ResponseDef{Type: "array"},
					Params:   []spec.Param{{Name: "roomId", In: "query", Type: "string", Required: true}},
					Walker: &spec.WalkerConfig{
						Parent:   "rooms",
						KeyField: "id",
						KeyParam: "roomId",
					},
				},
			},
		},
	}
	return apiSpec
}

func queryParamDependentSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"organizations": {
			Description: "Organizations",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/organizations",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
				},
			},
		},
		"application_files": {
			Description: "Files scoped by organization query param",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/application_files",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					Params:     []spec.Param{{Name: "organization_id", In: "query", Type: "string", Required: true}},
				},
			},
		},
	}
	return apiSpec
}

func TestGeneratedSyncDetectsQueryParamDependent(t *testing.T) {
	t.Parallel()

	apiSpec := queryParamDependentSpec("query-dep")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `ParentTable: "organizations"`,
		"auto-detect must wire the child under the matching parent resource")
	assert.Contains(t, syncSrc, `ParentIDParam: "organization_id"`,
		"required query parent key must become ParentIDParam without x-pp-sync-walker")
	assert.Contains(t, syncSrc, `PathTemplate: "/application_files"`,
		"child path stays flat; the parent key is a query param")
	assert.Contains(t, syncSrc, `{Param: "organization_id"`,
		"generated dependent PathParams must include the query parent key")
	defaultIdx := strings.Index(syncSrc, "func defaultSyncResources()")
	knownIdx := strings.Index(syncSrc, "func knownSyncResourceNames()")
	require.NotEqual(t, -1, defaultIdx)
	require.NotEqual(t, -1, knownIdx)
	assert.NotContains(t, syncSrc[defaultIdx:knownIdx], `"application_files"`,
		"query-param child must not remain a flat default-sync resource")
}

// TestGeneratedSyncWalkerSendsQueryParamParentKey proves that a walker whose
// key_param is a query name (child path has no {placeholder}) emits that
// param on each parent fetch, and that a walk whose every fetch fails is
// counted as a resource error rather than success.
func TestGeneratedSyncWalkerSendsQueryParamParentKey(t *testing.T) {
	t.Parallel()

	apiSpec := walkerQueryParamSpec("walker-query")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `ParentIDParam: "roomId"`,
		"walker key_param must be stored as ParentIDParam even when it is a query name")
	assert.Contains(t, syncSrc, `PathTemplate: "/messages"`,
		"child path must stay flat; spec authors must not have to template ?roomId={roomId} into the path")
	assert.Contains(t, syncSrc, `{Param: "roomId", Field: "id"}`,
		"query-param key_param must be emitted on the dependent so sync can send it")
	assert.Contains(t, syncSrc, `params[queryParam.Param] = dependentParentKeyValue(dep.ParentTable, queryParam.Field, value)`,
		"generated sync must write a non-placeholder key_param into the request params map")
	injectIdx := strings.Index(syncSrc, `params[queryParam.Param] = dependentParentKeyValue`)
	applyIdx := strings.Index(syncSrc, `userParams.applyTo(dep.Name, params, true)`)
	require.NotEqual(t, -1, injectIdx)
	require.NotEqual(t, -1, applyIdx)
	assert.Less(t, injectIdx, applyIdx,
		"query parent keys must be injected before user flags so --resource-param can override")
	assert.Contains(t, syncSrc, "outcome.reason = \"fetch_error\"",
		"dependent fetch errors must still be classified")
	failureAfterFetch := strings.Index(syncSrc, "outcome.reason = \"fetch_error\"")
	require.NotEqual(t, -1, failureAfterFetch)
	assert.Contains(t, syncSrc[failureAfterFetch:failureAfterFetch+200], "rep.failure = err",
		"fetch_error must set rep.failure so the aggregator counts the resource as errored")

	requireGeneratedCompiles(t, outputDir)

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type walkerQueryClient struct {
	t      *testing.T
	got    []map[string]string
	fail   error
}

func (c *walkerQueryClient) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if path != "/messages" {
		c.t.Fatalf("path = %q, want /messages", path)
	}
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.got = append(c.got, copied)
	if c.fail != nil {
		return nil, c.fail
	}
	return json.RawMessage(` + "`" + `[{"id":"msg-1","text":"hello"}]` + "`" + `), nil
}

func (*walkerQueryClient) RateLimit() float64 { return 0 }

func seedRooms(t *testing.T, db *store.Store) {
	t.Helper()
	if err := db.Upsert("rooms", "room-a", []byte(` + "`" + `{"id":"room-a","title":"General"}` + "`" + `)); err != nil {
		t.Fatalf("insert room: %v", err)
	}
	if err := db.Upsert("rooms", "room-b", []byte(` + "`" + `{"id":"room-b","title":"Random"}` + "`" + `)); err != nil {
		t.Fatalf("insert room: %v", err)
	}
}

func TestSyncDependentResourceSendsQueryParamParentKey(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seedRooms(t, db)

	client := &walkerQueryClient{t: t}
	res := syncDependentResource(
		context.Background(),
		client,
		db,
		dependentResourceDef{
			Name:          "messages",
			ParentTable:   "rooms",
			ParentIDParam: "roomId",
			PathTemplate:  "/messages",
			KeyField:      "id",
			PathParams:    []dependentPathParamDef{{Param: "roomId", Field: "id"}},
		},
		"", false, 1, false, false, nil, nil, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 2 {
		t.Fatalf("synced count = %d, want 2", res.Count)
	}
	if len(client.got) != 2 {
		t.Fatalf("fetches = %d, want 2", len(client.got))
	}
	seen := map[string]bool{}
	for _, params := range client.got {
		roomID := params["roomId"]
		if roomID == "" {
			t.Fatalf("query params = %#v, missing roomId", params)
		}
		seen[roomID] = true
	}
	if !seen["room-a"] || !seen["room-b"] {
		t.Fatalf("roomId values = %#v, want room-a and room-b", seen)
	}
}

func TestSyncDependentResourceFetchErrorsAreFailures(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seedRooms(t, db)

	client := &walkerQueryClient{t: t, fail: errors.New("HTTP 400: roomId is required")}
	res := syncDependentResource(
		context.Background(),
		client,
		db,
		dependentResourceDef{
			Name:          "messages",
			ParentTable:   "rooms",
			ParentIDParam: "roomId",
			PathTemplate:  "/messages",
			KeyField:      "id",
			PathParams:    []dependentPathParamDef{{Param: "roomId", Field: "id"}},
		},
		"", false, 1, false, false, nil, nil, 1,
	)
	if res.Err == nil {
		t.Fatal("expected syncDependentResource to return Err when every parent fetch fails")
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want nil (all-failed walk is an error, not a warning)", res.Warn)
	}
	if res.Count != 0 {
		t.Fatalf("synced count = %d, want 0", res.Count)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "walker_query_param_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSyncDependentResource(SendsQueryParamParentKey|FetchErrorsAreFailures)")
}
