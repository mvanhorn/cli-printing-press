package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedStoreListIDsScoped verifies that the generated store exposes a
// ListIDsScoped method that filters by a typed-table column when the column
// exists, degrades to unscoped when the column is absent, and treats an empty
// scopeValue as unscoped (identical to ListIDs).
func TestGeneratedStoreListIDsScoped(t *testing.T) {
	t.Parallel()

	// Build a spec whose "projects" resource has a typed table with both "id"
	// and "workspace" columns.  We need gravity >= 2 so the schema builder
	// extracts columns beyond the base three (id/data/synced_at).
	// Two endpoints give score 2; the "workspace" text field contributes 1
	// more — well above the threshold.
	apiSpec := minimalSpec("listidsscoped")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/projects",
					Response: spec.ResponseDef{Type: "array", Item: "Project"},
					IDField:  "id",
				},
				"get": {
					Method:   "GET",
					Path:     "/projects/{projectId}",
					Response: spec.ResponseDef{Type: "object", Item: "Project"},
					IDField:  "id",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Project": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "workspace", Type: "string"},
				{Name: "name", Type: "string"},
				{Name: "description", Type: "string"},
				{Name: "created_at", Type: "string", Format: "date-time"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "listidsscoped-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Verify the generated store.go actually creates a typed `projects` table
	// with a `workspace` column so our test is exercising the right branch.
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err, "generated store.go must exist")
	require.Contains(t, string(storeSrc), "workspace", "generated store.go must contain a workspace column")

	// Write the inline test into the generated store package.
	testPath := filepath.Join(outputDir, "internal", "store", "listidsscoped_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func seedProjects(t *testing.T, s *Store, items map[string]string) {
	t.Helper()
	var batch []json.RawMessage
	for id, ws := range items {
		raw, err := json.Marshal(map[string]any{"id": id, "workspace": ws, "name": "proj-" + id})
		if err != nil {
			t.Fatalf("marshal project %s: %v", id, err)
		}
		batch = append(batch, raw)
	}
	_, _, err := s.UpsertBatch("projects", batch)
	if err != nil {
		t.Fatalf("UpsertBatch projects: %v", err)
	}
}

func TestListIDsScoped(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	seedProjects(t, s, map[string]string{
		"p1": "ws-A",
		"p2": "ws-A",
		"p3": "ws-B",
	})

	got, err := s.ListIDsScoped("projects", "workspace", "ws-A")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("scoped ws-A = %v, want 2 ids", got)
	}

	// Empty scope == unscoped.
	all, err := s.ListIDsScoped("projects", "workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("empty-scope = %v, want all 3", all)
	}

	// Unknown column degrades to unscoped (never zero).
	deg, err := s.ListIDsScoped("projects", "nonexistent_col", "ws-A")
	if err != nil {
		t.Fatal(err)
	}
	if len(deg) != 3 {
		t.Fatalf("missing-column = %v, want degrade-to-all 3", deg)
	}
}
`), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestListIDsScoped", "-count=1")
}
