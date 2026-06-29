package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestReconcilePartition_CompositeKeyPreservesLiveRows verifies that
// ReconcilePartition treats parent-keyed dependent rows — whose storage id is the
// NUL-composite "<id>\x00<parent>" that resourceStorageID builds — correctly:
// seen-set membership is tested against the BARE id (the form sync collects into
// seenIDs), and cascade junction deletes use the bare id (junction FKs never
// carry the composite suffix). Before the fix the SQL victim filter
// `id NOT IN reconcile_seen` compared the whole composite key against bare seen
// ids, so every still-live composite row was mis-selected as a victim and hard
// deleted on `sync --full`; the cascade delete then no-opped (composite id never
// matched a bare junction FK), orphaning junction rows.
func TestReconcilePartition_CompositeKeyPreservesLiveRows(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("reconcile-composite")
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

	outputDir := filepath.Join(t.TempDir(), "reconcile-composite-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	inlineTest := `package store

import (
	"path/filepath"
	"testing"
)

func TestReconcilePartition_CompositeKeyPreservesLiveRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	db := s.DB()

	// Composite storage ids: "<id>\x00<parent>" — the shape resourceStorageID
	// builds for parent-keyed dependents. seenIDs from sync hold the BARE id.
	const live = "child-live\x00parentA"
	const stale = "child-stale\x00parentA"

	resRows := []struct{ id, data string }{
		{live, ` + "`" + `{"id":"child-live","scope":"wsA"}` + "`" + `},
		{stale, ` + "`" + `{"id":"child-stale","scope":"wsA"}` + "`" + `},
	}
	for _, r := range resRows {
		if _, err := db.Exec(
			` + "`" + `INSERT INTO resources (resource_type, id, data) VALUES (?, ?, ?)` + "`" + `,
			"things", r.id, r.data,
		); err != nil {
			t.Fatalf("insert %q: %v", r.id, err)
		}
	}

	// Cascade junction keyed by the BARE child id; junction FKs never carry the
	// NUL-composite suffix.
	if _, err := db.Exec(` + "`" + `CREATE TABLE junc (thing_id TEXT, other TEXT)` + "`" + `); err != nil {
		t.Fatalf("create junc: %v", err)
	}
	for _, fk := range []string{"child-live", "child-stale"} {
		if _, err := db.Exec(` + "`" + `INSERT INTO junc (thing_id, other) VALUES (?, ?)` + "`" + `, fk, "x"); err != nil {
			t.Fatalf("insert junc %q: %v", fk, err)
		}
	}

	// seenIDs carries only the BARE live id. The live composite row must survive
	// (its bare id was seen); only the stale row is a victim.
	cascades := []CascadeJunction{{Table: "junc", FKColumn: "thing_id"}}
	deleted, err := s.ReconcilePartition("things", "$.scope", "wsA", []string{"child-live"}, "things", cascades)
	if err != nil {
		t.Fatalf("ReconcilePartition returned error (want nil): %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the stale composite row)", deleted)
	}

	count := func(q string, args ...any) int {
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", q, err)
		}
		return n
	}

	if count(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type='things' AND id=?` + "`" + `, live) != 1 {
		t.Fatalf("live composite row deleted; it must be kept (its bare id was in seenIDs)")
	}
	if count(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type='things' AND id=?` + "`" + `, stale) != 0 {
		t.Fatalf("stale composite row survived; it must be reconciled away")
	}
	// Cascade must delete by the BARE fk: the stale child's junction row is gone,
	// the live child's junction row is kept.
	if count(` + "`" + `SELECT COUNT(*) FROM junc WHERE thing_id='child-live'` + "`" + `) != 1 {
		t.Fatalf("live junction row deleted; cascade must not touch live rows")
	}
	if count(` + "`" + `SELECT COUNT(*) FROM junc WHERE thing_id='child-stale'` + "`" + `) != 0 {
		t.Fatalf("stale junction row survived; cascade must delete by the bare resource id")
	}
}

// TestReconcilePartition_CompositeKeyCrossParent pins the per-partition
// invariant that makes the bare-id seen-set test safe even though the same bare
// id can appear under different parents (composite keys "child\x00A" and
// "child\x00B"). ReconcilePartition is invoked once per parent — each call gets
// only that parent's scope (the victim query filters on it) and that parent's
// seen-set — so a child that is live under A but stale under B is correctly
// swept in B's partition and kept in A's. The bare-id collision never produces
// a cross-parent false-negative.
func TestReconcilePartition_CompositeKeyCrossParent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	db := s.DB()

	const childA = "child\x00A"
	const childB = "child\x00B"
	for _, r := range []struct{ id, data string }{
		{childA, ` + "`" + `{"id":"child","scope":"A"}` + "`" + `},
		{childB, ` + "`" + `{"id":"child","scope":"B"}` + "`" + `},
	} {
		if _, err := db.Exec(
			` + "`" + `INSERT INTO resources (resource_type, id, data) VALUES (?, ?, ?)` + "`" + `,
			"things", r.id, r.data,
		); err != nil {
			t.Fatalf("insert %q: %v", r.id, err)
		}
	}

	cnt := func(id string) int {
		var n int
		if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE id=?` + "`" + `, id).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	// Partition B: B's live items do NOT include child (it was removed under B).
	delB, err := s.ReconcilePartition("things", "$.scope", "B", []string{"other"}, "things", nil)
	if err != nil {
		t.Fatalf("reconcile B: %v", err)
	}
	// Partition A: child is still live under A.
	delA, err := s.ReconcilePartition("things", "$.scope", "A", []string{"child"}, "things", nil)
	if err != nil {
		t.Fatalf("reconcile A: %v", err)
	}

	if cnt(childB) != 0 {
		t.Fatalf("child under stale parent B survived; per-parent reconcile must sweep it (no cross-parent false-negative)")
	}
	if cnt(childA) != 1 {
		t.Fatalf("child under live parent A deleted; it must be kept")
	}
	if delB != 1 || delA != 0 {
		t.Fatalf("delB=%d delA=%d, want 1 and 0", delB, delA)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "store", "reconcile_composite_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	// MUST fail before the template fix (live row mis-deleted, cascade orphan) and
	// pass after.
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestReconcilePartition_CompositeKey", "-count=1")
}
