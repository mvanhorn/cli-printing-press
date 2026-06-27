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
	outputDir := filepath.Join(t.TempDir(), "derive-scope-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
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
	stored, _, err := s.UpsertBatch("modules", items)
	if err != nil {
		t.Fatalf("UpsertBatch must not error on typed-table NOT NULL failure: %v", err)
	}
	if stored != 1 {
		t.Fatalf("stored = %d, want 1 (generic row must land)", stored)
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

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store",
		"-run", "TestUpsertBatch_DerivesChildScopeFromProjectField|TestUpsertBatch_NoFabricatedScopeWhenSourceAbsent",
		"-count=1", "-v")
}
