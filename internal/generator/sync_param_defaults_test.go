package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncParamDefaultsSpec declares a flat list endpoint with a spec `default:`,
// a dependent (walker-driven) list endpoint with its own default, and a third
// resource with no defaults at all — the fallback shape that must keep
// emitting an empty params map.
func syncParamDefaultsSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"posts": {
			Description: "Posts",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/posts",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "sort", In: "query", Type: "string", Default: "new"},
						{Name: "include_drafts", In: "query", Type: "boolean", Default: true},
					},
				},
			},
		},
		"authors": {
			Description: "Authors",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/authors",
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
		"comments": {
			Description: "Comments scoped to a post",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/comments",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "postId", In: "query", Type: "string", Required: true},
						{Name: "depth", In: "query", Type: "integer", Default: 3},
					},
					Walker: &spec.WalkerConfig{
						Parent:   "posts",
						KeyField: "id",
						KeyParam: "postId",
					},
				},
			},
		},
	}
	return apiSpec
}

// TestGeneratedSyncSendsSpecParamDefaults is the generated-output proof that a
// spec-declared query `default:` reaches sync requests. Endpoint commands bind
// the same default to their cobra flag, so before this the two surfaces
// addressed different slices of the API: a list command sent the scope or the
// stable ordering and sync silently omitted it.
func TestGeneratedSyncSendsSpecParamDefaults(t *testing.T) {
	t.Parallel()

	apiSpec := syncParamDefaultsSpec("syncdefaults")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")

	// Flat and dependent resources both land in the same lookup, and a
	// resource without defaults gets no case at all.
	assert.Contains(t, syncSrc, "func syncResourceParamDefaults(resource string) map[string]string {")
	assert.Contains(t, syncSrc, `"sort":           "new"`)
	assert.Contains(t, syncSrc, `"include_drafts": "true"`)
	assert.Contains(t, syncSrc, `"depth": "3"`)
	defaultsIdx := strings.Index(syncSrc, "func syncResourceParamDefaults(resource string) map[string]string {")
	require.NotEqual(t, -1, defaultsIdx)
	defaultsFn := syncSrc[defaultsIdx : defaultsIdx+strings.Index(syncSrc[defaultsIdx:], "\n}\n")]
	assert.NotContains(t, defaultsFn, `case "authors":`,
		"a resource whose spec declares no param default must not get a lookup case")

	// The seeding happens before everything sync computes, so paging, the
	// since filter, and --param overrides all still win on key conflict.
	seedIdx := strings.Index(syncSrc, "for defaultKey, defaultValue := range syncResourceParamDefaults(resource)")
	limitIdx := strings.Index(syncSrc, "params[pageSize.limitParam] = strconv.Itoa(pageSize.limit)")
	applyIdx := strings.Index(syncSrc, "userParams.applyTo(resource, params, false)")
	require.NotEqual(t, -1, seedIdx, "flat sync loop must seed spec param defaults")
	require.NotEqual(t, -1, limitIdx)
	require.NotEqual(t, -1, applyIdx)
	assert.Less(t, seedIdx, limitIdx, "spec defaults must not overwrite sync-owned paging params")
	assert.Less(t, seedIdx, applyIdx, "--param must still override a spec default")

	depSeedIdx := strings.Index(syncSrc, "for defaultKey, defaultValue := range syncResourceParamDefaults(dep.Name)")
	depApplyIdx := strings.Index(syncSrc, "userParams.applyTo(dep.Name, params, true)")
	require.NotEqual(t, -1, depSeedIdx, "dependent sync loop must seed spec param defaults too")
	require.NotEqual(t, -1, depApplyIdx)
	assert.Less(t, depSeedIdx, depApplyIdx)

	requireGeneratedCompiles(t, outputDir)

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type paramDefaultsClient struct {
	got []map[string]string
}

func (c *paramDefaultsClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.got = append(c.got, copied)
	return json.RawMessage(` + "`" + `[{"id":"one"}]` + "`" + `), nil
}

func (*paramDefaultsClient) RateLimit() float64 { return 0 }

func openParamDefaultsStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSyncFlatResourceSendsSpecParamDefaults(t *testing.T) {
	db := openParamDefaultsStore(t)
	client := &paramDefaultsClient{}

	res := syncResource(context.Background(), client, db, "posts", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued")
	}
	if got := client.got[0]["sort"]; got != "new" {
		t.Fatalf("sort = %q, want %q (spec default must reach the sync request)", got, "new")
	}
	if got := client.got[0]["include_drafts"]; got != "true" {
		t.Fatalf("include_drafts = %q, want %q", got, "true")
	}
}

func TestSyncFlatResourceUserParamOverridesSpecDefault(t *testing.T) {
	db := openParamDefaultsStore(t)
	client := &paramDefaultsClient{}

	userParams, err := parseSyncUserParams([]string{"sort=top"}, nil, nil)
	if err != nil {
		t.Fatalf("parseSyncUserParams: %v", err)
	}
	res := syncResource(context.Background(), client, db, "posts", "", false, 1, false, false, userParams, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if got := client.got[0]["sort"]; got != "top" {
		t.Fatalf("sort = %q, want %q (--param must win over a spec default)", got, "top")
	}
}

func TestSyncResourceWithoutDefaultsSendsNone(t *testing.T) {
	db := openParamDefaultsStore(t)
	client := &paramDefaultsClient{}

	res := syncResource(context.Background(), client, db, "authors", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if _, ok := client.got[0]["sort"]; ok {
		t.Fatalf("params = %#v, want no sort key for a resource with no declared default", client.got[0])
	}
}

func TestSyncDependentResourceSendsSpecParamDefaults(t *testing.T) {
	db := openParamDefaultsStore(t)
	if err := db.Upsert("posts", "post-a", []byte(` + "`" + `{"id":"post-a"}` + "`" + `)); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	client := &paramDefaultsClient{}

	res := syncDependentResource(
		context.Background(),
		client,
		db,
		dependentResourceDef{
			Name:          "comments",
			ParentTable:   "posts",
			ParentIDParam: "postId",
			PathTemplate:  "/comments",
			KeyField:      "id",
			PathParams:    []dependentPathParamDef{{Param: "postId", Field: "id"}},
		},
		"", false, 1, false, false, nil, nil, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if len(client.got) == 0 {
		t.Fatal("no dependent request issued")
	}
	if got := client.got[0]["depth"]; got != "3" {
		t.Fatalf("depth = %q, want %q (spec default must reach dependent sync requests)", got, "3")
	}
	if got := client.got[0]["postId"]; got != "post-a" {
		t.Fatalf("postId = %q, want post-a (parent key must still be injected)", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_param_defaults_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSync(FlatResourceSendsSpecParamDefaults|FlatResourceUserParamOverridesSpecDefault|ResourceWithoutDefaultsSendsNone|DependentResourceSendsSpecParamDefaults)")
}

// TestGeneratedSyncOmitsParamDefaultsHelperWhenSpecDeclaresNone locks the
// fallback: CLIs whose specs carry no query defaults keep their previous
// emitted shape rather than gaining a dead lookup.
func TestGeneratedSyncOmitsParamDefaultsHelperWhenSpecDeclaresNone(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("nodefaults")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.NotContains(t, syncSrc, "syncResourceParamDefaults",
		"the defaults lookup and its call sites must be gated on the same condition")

	requireGeneratedCompiles(t, outputDir)
}

// TestGeneratedSyncSendsOpenAPIParamDefaults covers the other spec front-end:
// the same contract has to hold for OpenAPI-parsed specs, not only the
// internal YAML format.
func TestGeneratedSyncSendsOpenAPIParamDefaults(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Sync Defaults
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /posts:
    get:
      operationId: listPosts
      parameters:
        - name: sort
          in: query
          schema:
            type: string
            default: new
        - name: limit
          in: query
          schema:
            type: integer
            default: 25
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `"sort": "new"`,
		"an OpenAPI schema default must reach the sync request the same way an internal-spec default does")
	assert.NotContains(t, syncSrc, `"limit": "25"`,
		"the endpoint's own page-size param is sync-owned; a default there would fight the paging loop")

	requireGeneratedCompiles(t, outputDir)
}
