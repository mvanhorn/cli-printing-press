package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestDependentFanoutScoping verifies that dependentParentRows scopes its
// single-id branch via ListIDsScoped when a scope column and value are
// provided, and falls back to enumerating all parents when scopeValue=="".
// It is a generated-run test: we generate a CLI with a typed "projects" parent
// and a paginated "modules" dependent, then write an in-package test into the
// generated internal/cli directory and run it there.
func TestDependentFanoutScoping(t *testing.T) {
	t.Parallel()

	// Spec: projects (typed table with workspace column) is the parent;
	// modules is a single-PK dependent requiring pagination, so the template
	// emits dependentParentRows and the {{if}} block around it.
	apiSpec := minimalSpec("dep-fanout-scoped")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:            "GET",
					Path:              "/projects",
					Response:          spec.ResponseDef{Type: "array", Item: "Project"},
					Pagination:        &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:           "id",
					TenantScopeColumn: "workspace",
				},
				"get": {
					Method:   "GET",
					Path:     "/projects/{projectId}",
					Response: spec.ResponseDef{Type: "object", Item: "Project"},
					IDField:  "id",
				},
			},
		},
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
	}
	// Project type with explicit workspace field so the schema builder creates a
	// typed `projects` table with a `workspace` column for scoped queries.
	apiSpec.Types = map[string]spec.TypeDef{
		"Project": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "workspace", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	// Assert the caller wiring is present in the generated sync.go so it cannot
	// silently regress (the test below covers behavior; this covers the template).
	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)
	require.Contains(t, src, "resolveTenantID()", "sync.go caller must call resolveTenantID()")
	require.Contains(t, src, "parentTenantScopeColumns[dep.ParentTable]", "sync.go caller must pass parentTenantScopeColumns[dep.ParentTable]")

	// Write the behavioral inline test into the generated internal/cli package.
	// Uses package cli (not package cli_test) for access to unexported
	// dependentParentRows (same-package test).
	inlineTest := `package cli

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

func seedFanoutProjects(t *testing.T, db *store.Store, items map[string]string) {
	t.Helper()
	var batch []json.RawMessage
	for id, ws := range items {
		raw, err := json.Marshal(map[string]any{"id": id, "workspace": ws, "name": "proj-" + id})
		if err != nil {
			t.Fatalf("marshal project %s: %v", id, err)
		}
		batch = append(batch, raw)
	}
	_, _, err := db.UpsertBatch("projects", batch)
	if err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
}

func TestDependentFanoutScoping(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	seedFanoutProjects(t, db, map[string]string{
		"p1": "ws-A",
		"p2": "ws-A",
		"p3": "ws-B",
	})

	// Single-id pathParams: one dependentPathParamDef with Field=="id", matching
	// the len(fields)==1 branch that dependentParentRows hits for simple parents.
	singleIDParams := []dependentPathParamDef{{Param: "projectId", Field: "id"}}

	// Scoped: only ws-A parents (p1, p2).
	rows, err := dependentParentRows(db, "projects", singleIDParams, "workspace", "ws-A")
	if err != nil {
		t.Fatalf("scoped ws-A: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row["id"])
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("scoped ws-A = %v, want [p1 p2]", got)
	}

	// Unscoped fallback: scopeValue=="" must enumerate all three.
	all, err := dependentParentRows(db, "projects", singleIDParams, "workspace", "")
	if err != nil {
		t.Fatalf("unscoped fallback: %v", err)
	}
	if len(all) != 3 {
		ids := make([]string, 0, len(all))
		for _, row := range all {
			ids = append(ids, row["id"])
		}
		t.Fatalf("unscoped fallback = %v, want all 3", ids)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "fanout_scoping_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestDependentFanoutScoping", "./internal/cli")
}
