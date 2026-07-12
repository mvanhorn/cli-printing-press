package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestReconcilePartition_MalformedJSONRow verifies that a single non-JSON row in
// the resources table does NOT abort ReconcilePartition with "malformed JSON".
// The malformed row must be silently skipped (never a victim, never deleted).
// This is the TDD test for the CASE WHEN json_valid(data) fix.
func TestReconcilePartition_MalformedJSONRow(t *testing.T) {
	t.Parallel()

	// Minimal spec with a "things" resource that has list+get (gravity >= 2) so a
	// typed table is emitted. We use a scope column "scope" so ReconcilePartition
	// with genericScopeJSONPath="$.scope" runs against the right field.
	apiSpec := minimalSpec("reconcile-malformed")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"things": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/things",
					Response: spec.ResponseDef{Type: "array", Item: "Thing"},
					IDField:  "id",
				},
				"get": {
					Method:   "GET",
					Path:     "/things/{thingId}",
					Response: spec.ResponseDef{Type: "object", Item: "Thing"},
					IDField:  "id",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Thing": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "scope", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "reconcile-malformed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Confirm the generated store.go has a "scope" column in the typed table
	// (validates our spec causes the right schema).
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err, "generated store.go must exist")
	require.Contains(t, string(storeSrc), `"scope"`, "generated store.go must contain a scope column")

	// Write an inline test into the generated store package.
	inlineTest := `package store

import (
	"path/filepath"
	"testing"
)

func TestReconcilePartition_MalformedJSONRow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// Insert three rows directly into the generic resources table:
	//   keep-1  — valid JSON, in seenIDs → kept
	//   stale-1 — valid JSON, NOT in seenIDs → must be deleted
	//   junk-1  — malformed JSON → must NOT abort the query, must NOT be deleted
	db := s.DB()
	rows := []struct{ id, data string }{
		{"keep-1",  ` + "`" + `{"id":"keep-1","scope":"wsA"}` + "`" + `},
		{"stale-1", ` + "`" + `{"id":"stale-1","scope":"wsA"}` + "`" + `},
		{"junk-1",  "<!DOCTYPE html><html></html>"},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			` + "`" + `INSERT INTO resources (resource_type, id, data) VALUES (?, ?, ?)` + "`" + `,
			"things", r.id, r.data,
		); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}

	// Call ReconcilePartition with keep-1 in seen. stale-1 is the only valid victim.
	// junk-1 is malformed and must be skipped, not deleted.
	deleted, err := s.ReconcilePartition("things", "$.scope", "wsA", []string{"keep-1"}, "things", nil)
	if err != nil {
		t.Fatalf("ReconcilePartition returned error (want nil): %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only stale-1)", deleted)
	}

	// keep-1 must still be present.
	if _, err := db.Exec(` + "`" + `SELECT 1 FROM resources WHERE resource_type='things' AND id='keep-1'` + "`" + `); err != nil {
		t.Fatalf("keep-1 existence check: %v", err)
	}
	var keepCount int
	if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type='things' AND id='keep-1'` + "`" + `).Scan(&keepCount); err != nil {
		t.Fatalf("keep-1 count: %v", err)
	}
	if keepCount != 1 {
		t.Fatalf("keep-1 missing after reconcile; must be kept (was in seenIDs)")
	}

	// stale-1 must be gone.
	var staleCount int
	if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type='things' AND id='stale-1'` + "`" + `).Scan(&staleCount); err != nil {
		t.Fatalf("stale-1 count: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale-1 still present; must have been deleted as a victim")
	}

	// junk-1 must still be present (malformed row must not be deleted).
	var junkCount int
	if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type='things' AND id='junk-1'` + "`" + `).Scan(&junkCount); err != nil {
		t.Fatalf("junk-1 count: %v", err)
	}
	if junkCount != 1 {
		t.Fatalf("junk-1 missing after reconcile; malformed rows must not be deleted")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "store", "reconcile_malformed_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	// This MUST fail before the template fix (malformed JSON error) and pass after.
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestReconcilePartition_MalformedJSONRow", "-count=1")
}
