package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncSeedsRequiredNumericFirstPageWithoutTokenCursor(t *testing.T) {
	zero := 0.0
	one := 1.0
	minimum := 3.0
	apiSpec := minimalSpec("first-page-seed")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"zero_pages": {Description: "Zero-based page records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/zero-pages", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "page", In: "query", Type: "integer", Required: true, Minimum: &zero},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "limit"},
			},
		}},
		"one_offsets": {Description: "One-based offset records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/one-offsets", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "offset", In: "query", Type: "integer", Required: true, Minimum: &one},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit"},
			},
		}},
		"pages": {Description: "Page records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/pages", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "page", In: "query", Type: "integer", Required: true},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "limit"},
			},
		}},
		"minimum_pages": {Description: "Minimum page records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/minimum-pages", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "page", In: "query", Type: "integer", Required: true, Minimum: &minimum},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "limit"},
			},
		}},
		"resume_pages": {Description: "Resumable page records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/resume-pages", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "page", In: "query", Type: "integer", Required: true, Minimum: &zero},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "limit"},
			},
		}},
		"tokens": {Description: "Token records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/tokens", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "starting_after", In: "query", Type: "string"},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "cursor", CursorParam: "starting_after", LimitParam: "limit"},
			},
		}},
		"parents": {Description: "Parent records", Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/parents", Response: spec.ResponseDef{Type: "array"}},
		}},
		"dependent_pages": {Description: "Dependent page records", Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/parents/{parent_id}/dependent-pages", Response: spec.ResponseDef{Type: "array"},
				Params: []spec.Param{
					{Name: "parent_id", In: "path", Type: "string", Required: true},
					{Name: "page", In: "query", Type: "integer", Required: true, Minimum: &zero},
					{Name: "limit", In: "query", Type: "integer", Default: 2},
				},
				Pagination: &spec.Pagination{Type: "page", CursorParam: "page", LimitParam: "limit"},
			},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	inlineTest := fmt.Sprintf(`package cli

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	%q
)

type firstPageSeedClient struct { params []map[string]string }

func (c *firstPageSeedClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copy := map[string]string{}
	for key, value := range params { copy[key] = value }
	c.params = append(c.params, copy)
	return json.RawMessage("[{\"id\":\"one\"},{\"id\":\"two\"}]"), nil
}

func (*firstPageSeedClient) RateLimit() float64 { return 0 }

func TestSyncFirstPageNumericSeedsAdvanceFromSeed(t *testing.T) {
	for _, tc := range []struct { resource, key string; want []string }{
		{resource: "zero_pages", key: "page", want: []string{"0", "1"}},
		{resource: "one_offsets", key: "offset", want: []string{"1", "3"}},
		{resource: "pages", key: "page", want: []string{"1", "2"}},
		{resource: "minimum_pages", key: "page", want: []string{"3", "4"}},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
			if err != nil { t.Fatal(err) }
			defer db.Close()
			client := &firstPageSeedClient{}
			result := syncResource(context.Background(), client, db, tc.resource, "", true, 2, false, false, &syncUserParams{}, nil)
			if result.Err != nil || result.Warn != nil { t.Fatalf("sync result = %%+v", result) }
			if len(client.params) != len(tc.want) { t.Fatalf("requests = %%#v, want %%d", client.params, len(tc.want)) }
			for i, want := range tc.want {
				if got := client.params[i][tc.key]; got != want { t.Fatalf("request %%d params = %%#v, want %%s=%%q", i, client.params[i], tc.key, want) }
			}
		})
	}
}

func TestSyncResumeCursorPrecedesFirstPageSeed(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Upsert("resume_pages", "existing", json.RawMessage("{\"id\":\"existing\"}")); err != nil { t.Fatal(err) }
	if err := db.SaveSyncProgress("resume_pages", "7", 1); err != nil { t.Fatal(err) }
	client := &firstPageSeedClient{}
	result := syncResource(context.Background(), client, db, "resume_pages", "", true, 1, false, false, &syncUserParams{}, nil)
	if result.Err != nil || result.Warn != nil { t.Fatalf("sync result = %%+v", result) }
	if len(client.params) != 1 || client.params[0]["page"] != "7" { t.Fatalf("params = %%#v, want resumed page 7", client.params) }
}

func TestDependentSyncFirstPageSeedAdvancesFromZero(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	client := &firstPageSeedClient{}
	report := syncOneParent(
		context.Background(), client, db,
		dependentResourceDef{Name: "dependent_pages", ParentTable: "parents", PathTemplate: "/parents/{parent_id}/dependent-pages", KeyField: "id"},
		map[string]string{"id": "parent-a"}, []dependentPathParamDef{{Param: "parent_id", Field: "id"}},
		paginationDefaults{cursorParam: "page", cursorType: "page", initialCursor: "0", limitParam: "limit", limit: 2},
		"", "", 2, false, false, &syncUserParams{}, io.Discard,
	)
	if report.failure != nil { t.Fatalf("dependent sync error: %%v", report.failure) }
	if len(client.params) != 2 || client.params[0]["page"] != "0" || client.params[1]["page"] != "1" {
		t.Fatalf("params = %%#v, want dependent pages 0 then 1", client.params)
	}
}

func TestSyncFirstPageTokenCursorStaysUnset(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	client := &firstPageSeedClient{}
	result := syncResource(context.Background(), client, db, "tokens", "", true, 1, false, false, &syncUserParams{}, nil)
	if result.Err != nil || result.Warn != nil { t.Fatalf("sync result = %%+v", result) }
	if len(client.params) != 1 { t.Fatalf("requests = %%d, want one", len(client.params)) }
	if _, ok := client.params[0]["starting_after"]; ok { t.Fatalf("first token request sent starting_after: %%#v", client.params[0]) }
}
`, naming.CLI(apiSpec.Name)+"/internal/store")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_first_page_seed_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(SyncFirstPage|SyncResumeCursor|DependentSyncFirstPage)")
}
