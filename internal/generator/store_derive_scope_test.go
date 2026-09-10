package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// deriveScopeSpec builds a spec with "projects" (flat, list endpoint) and
// "modules" as a SubResource of "projects" (path /projects/{projectId}/modules).
// The SubResource path causes buildSubResourceTable to emit a "projects_id" TEXT
// NOT NULL column on the modules table, which is the NOT NULL scope column
// deriveScopeColumns must backfill from the item's "project" field.
func deriveScopeSpec() *spec.APISpec {
	s := minimalSpec("derive-scope")
	s.Auth = spec.AuthConfig{Type: "none"}
	s.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/projects",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
			SubResources: map[string]spec.Resource{
				"modules": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:     "GET",
							Path:       "/projects/{projectId}/modules",
							Response:   spec.ResponseDef{Type: "array"},
							Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
							IDField:    "id",
						},
					},
				},
			},
		},
	}
	return s
}

// TestGenerate_EmitsDeriveScope verifies that the generator emits the
// childScopeColumnSources map and deriveScopeColumns function for a spec with a
// SubResource "modules" parented by "projects". The generated store must
// compile, and a behavioral test confirms that UpsertBatch backfills projects_id
// from the item's "project" field when the path injection is absent.
func TestGenerate_EmitsDeriveScope(t *testing.T) {
	t.Parallel()

	apiSpec := deriveScopeSpec()
	projects := apiSpec.Resources["projects"]
	list := projects.Endpoints["list"]
	list.Response.Item = "Project"
	projects.Endpoints["list"] = list
	projects.Endpoints["get"] = spec.Endpoint{Method: "GET", Path: "/projects/{id}", Response: spec.ResponseDef{Type: "object", Item: "Project"}}
	apiSpec.Resources["projects"] = projects
	apiSpec.Types = map[string]spec.TypeDef{"Project": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}}}
	outputDir := filepath.Join(t.TempDir(), "derive-scope-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	// Verify the generated store.go contains deriveScopeColumns wiring.
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err, "generated store.go must exist")
	storeSrcStr := string(storeSrc)
	require.Contains(t, storeSrcStr, "childScopeColumnSources", "generated store.go must contain childScopeColumnSources map")
	require.Contains(t, storeSrcStr, "deriveScopeColumns", "generated store.go must contain deriveScopeColumns func")
	require.Contains(t, storeSrcStr, `"projects_id": "project"`, "childScopeColumnSources must map projects_id -> project")

	// Confirm the modules typed table has a NOT NULL projects_id column.
	require.Contains(t, storeSrcStr, `"projects_id" TEXT NOT NULL`, "modules table must have NOT NULL projects_id column")

	// Write the behavioral test into the generated store package.
	testSrc := `package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func openTestStoreDerive(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestUpsertBatch_DerivesChildScopeFromProjectField verifies that raw API items
// carrying "project" (but not "projects_id") land in the typed modules table with
// projects_id populated after deriveScopeColumns backfills the scope column.
func TestUpsertBatch_DerivesChildScopeFromProjectField(t *testing.T) {
	s := openTestStoreDerive(t)
	items := []json.RawMessage{
		json.RawMessage(` + "`" + `{"id":"wt-001","project":"proj-X","name":"Mod 1"}` + "`" + `),
		json.RawMessage(` + "`" + `{"id":"wt-002","project":"proj-X","name":"Mod 2"}` + "`" + `),
	}
	stored, _, err := s.UpsertBatch("modules", items)
	if err != nil || stored != 2 {
		t.Fatalf("UpsertBatch stored=%d err=%v", stored, err)
	}
	var typed int
	s.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM "modules" WHERE projects_id = ?` + "`" + `, "proj-X").Scan(&typed)
	if typed != 2 {
		t.Fatalf("typed modules with projects_id=proj-X = %d, want 2 (scope not derived)", typed)
	}
}

// TestUpsertBatch_NoFabricatedScopeWhenSourceAbsent verifies that an item with
// NEITHER "project" NOR "projects_id" strands in the generic resources table
// (savepoint rollback) — deriveScopeColumns must NOT fabricate a scope from nothing.
func TestUpsertBatch_NoFabricatedScopeWhenSourceAbsent(t *testing.T) {
	s := openTestStoreDerive(t)
	items := []json.RawMessage{
		json.RawMessage(` + "`" + `{"id":"orphan-001","name":"No Parent"}` + "`" + `),
	}
	stored, _, typedFailures, err := s.UpsertBatchDetailed("modules", items)
	if err != nil {
		t.Fatalf("UpsertBatchDetailed must not error on typed-table NOT NULL failure: %v", err)
	}
	if stored != 1 {
		t.Fatalf("stored = %d, want 1 (generic row must land)", stored)
	}
	if typedFailures != 1 {
		t.Fatalf("typedFailures = %d, want 1", typedFailures)
	}
	var typed int
	s.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM "modules" WHERE id = 'orphan-001'` + "`" + `).Scan(&typed)
	if typed != 0 {
		t.Fatalf("typed modules count = %d, want 0 (item without source must strand in generic)", typed)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "store", "derive_scope_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(testSrc), 0o644))

	cliTestSrc := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"derive-scope-pp-cli/internal/store"
)

type projectionFailureClient struct{}
func (projectionFailureClient) Get(context.Context, string, map[string]string) (json.RawMessage, error) {
	return json.RawMessage(` + "`" + `[{"id":"new-project"}]` + "`" + `), nil
}
func (projectionFailureClient) RateLimit() float64 { return 0 }

func TestSyncTypedProjectionFailureInvalidatesCompletion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil { t.Fatal(err) }
	defer s.Close()
	if _, _, err := s.UpsertBatch("projects", []json.RawMessage{json.RawMessage(` + "`" + `{"id":"old-project"}` + "`" + `)}); err != nil { t.Fatal(err) }
	watermark := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.SaveSyncStateAt("projects", "page-2", 1, watermark); err != nil { t.Fatal(err) }
	if _, err := s.DB().Exec("CREATE TRIGGER reject_project BEFORE INSERT ON projects BEGIN SELECT RAISE(FAIL, 'projection rejected'); END"); err != nil { t.Fatal(err) }
	var events bytes.Buffer
	res := syncResource(context.Background(), projectionFailureClient{}, s, "projects", "", false, 1, false, false, nil, &events)
	if res.Err == nil || !res.IntegrityFailure { t.Fatalf("result = %+v", res) }
	cursor, gotTime, _, err := s.GetSyncState("projects")
	if err != nil || cursor != "page-2" || !gotTime.Equal(watermark) { t.Fatalf("checkpoint = %q %s %v", cursor, gotTime, err) }
	var complete int
	if err := s.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type = 'projects'").Scan(&complete); err != nil || complete != 0 { t.Fatalf("completion = %d, %v", complete, err) }
}

func TestUpsertResourceBatchReportsTypedProjectionFailure(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	items := []json.RawMessage{
		json.RawMessage(` + "`" + `{"id":"orphan-001","name":"No Parent"}` + "`" + `),
	}
	stored, extractFailures, typedFailures, err := upsertResourceBatch(s, "modules", items)
	if err != nil {
		t.Fatalf("upsertResourceBatch: %v", err)
	}
	if stored != 1 || extractFailures != 0 || typedFailures != 1 {
		t.Fatalf("stored/extractFailures/typedFailures = %d/%d/%d, want 1/0/1", stored, extractFailures, typedFailures)
	}
}
`
	cliTestPath := filepath.Join(outputDir, "internal", "cli", "derive_scope_sync_test.go")
	require.NoError(t, os.WriteFile(cliTestPath, []byte(cliTestSrc), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store",
		"-run", "TestUpsertBatch_DerivesChildScopeFromProjectField|TestUpsertBatch_NoFabricatedScopeWhenSourceAbsent",
		"-count=1", "-v")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli",
		"-run", "TestUpsertResourceBatchReportsTypedProjectionFailure|TestSyncTypedProjectionFailureInvalidatesCompletion",
		"-count=1")
	requireGeneratedCompiles(t, outputDir)
}
