package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestMembershipScopedFanout verifies the membership-aware dependent fan-out:
// a parent resource annotated with x-pp-membership-field emits a
// membershipScopedParents map + a generic Store.NonMemberParents method, and
// the generated code compiles + behaves (non-member parents are returned so the
// fan-out can skip them). It is a generated-run test: generate a CLI with a
// membership-annotated "projects" parent and a "modules" dependent, then compile
// + run an in-package behavioral test.
func TestMembershipScopedFanout(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("membership-fanout")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:          "GET",
					Path:            "/projects",
					Response:        spec.ResponseDef{Type: "array", Item: "Project"},
					Pagination:      &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:         "id",
					MembershipField: "is_member",
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
	apiSpec.Types = map[string]spec.TypeDef{
		"Project": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "is_member", Type: "boolean"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	// Template wiring must be present so it cannot silently regress.
	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	sync := string(syncSrc)
	require.Contains(t, sync, "var membershipScopedParents = map[string]string{")
	require.Contains(t, sync, `"projects": "is_member"`)
	require.Contains(t, sync, "func reportSkippedParents(")
	require.Contains(t, sync, "func parentSkippedJSON(")
	require.Contains(t, sync, "func syncOneParent(")

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	require.Contains(t, string(storeSrc), "func (s *Store) NonMemberParents(resourceType, membershipField string)")

	// Behavioral: seed projects with mixed membership + one malformed row, then
	// assert NonMemberParents returns exactly the non-member ids (json_valid guard
	// keeps the junk row from aborting the whole query). Mirrors build-tree
	// nonmember_projects_test seeding.
	inlineTest := `package store

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNonMemberParentsBehavior(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	items := []json.RawMessage{
		json.RawMessage(` + "`" + `{"id":"p1","name":"Alpha","is_member":true}` + "`" + `),
		json.RawMessage(` + "`" + `{"id":"p2","name":"Bravo","is_member":false}` + "`" + `),
		json.RawMessage(` + "`" + `{"id":"p3","name":"Charlie","is_member":false}` + "`" + `),
		json.RawMessage(` + "`" + `{"id":"p4","name":"Delta"}` + "`" + `),
	}
	if _, _, err := s.UpsertBatch("projects", items); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	// A single malformed-JSON row must not abort the whole query.
	if _, err := s.DB().Exec(
		` + "`" + `INSERT INTO resources (id, resource_type, data, synced_at, updated_at)
		 VALUES ('junk', 'projects', 'not valid json', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')` + "`" + `,
	); err != nil {
		t.Fatalf("insert junk: %v", err)
	}

	got, err := s.NonMemberParents("projects", "is_member")
	if err != nil {
		t.Fatalf("NonMemberParents: %v", err)
	}
	ids := make([]string, 0, len(got))
	for id := range got {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "p2" || ids[1] != "p3" {
		t.Fatalf("NonMemberParents = %v (names %v), want [p2 p3]", ids, got)
	}
	if got["p2"] != "Bravo" || got["p3"] != "Charlie" {
		t.Fatalf("names = %v, want p2=Bravo p3=Charlie", got)
	}

	// Invalid field name is a no-op (feature disabled), not an error.
	none, err := s.NonMemberParents("projects", "is member; DROP TABLE resources")
	if err != nil {
		t.Fatalf("invalid field should be no-op, got err: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("invalid field = %v, want empty", none)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "store", "nonmember_parents_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestNonMemberParentsBehavior", "./internal/store")
	// Also compile the cli package so the membership fan-out wiring (filter +
	// reportSkippedParents + worker pool) is guaranteed to build.
	runGoCommandRequired(t, outputDir, "build", "./internal/cli")
}
