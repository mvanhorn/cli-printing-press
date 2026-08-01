package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedStoreMigratesLegacyParentKeyRows exercises the emitted store,
// not the template text. It seeds the pre-v5 bare-id shape alongside current
// composite rows, downgrades the version stamp, and proves Open migrates the
// generic table, typed projection, and both FTS indexes atomically.
func TestGeneratedStoreMigratesLegacyParentKeyRows(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parent-key-migration")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"parents": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/parents",
					Response: spec.ResponseDef{Type: "array", Item: "Parent"},
					IDField:  "id",
				},
			},
		},
		"children": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/parents/{parent_id}/children",
					Response: spec.ResponseDef{Type: "array", Item: "Child"},
					IDField:  "id",
					Walker: &spec.WalkerConfig{
						Parent:   "parents",
						KeyField: "id",
						KeyParam: "parent_id",
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Parent": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
		"Child": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "parent_id", Type: "string"},
				{Name: "name", Type: "string"},
				{Name: "description", Type: "string"},
				{Name: "summary", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "parent-key-migration-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	storeSource, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	require.Contains(t, string(storeSource), `"children": {"parent_id"}`)
	require.Contains(t, string(storeSource), "migrateParentKeyStorageIDs")
	require.Contains(t, string(storeSource), "const StoreSchemaVersion = 5")

	inlineTest := `package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func seedLegacyParentKeyRow(t *testing.T, s *Store, id string, data json.RawMessage) {
	t.Helper()
	obj, err := DecodeJSONObject(data)
	if err != nil {
		t.Fatalf("decode legacy row %s: %v", id, err)
	}
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatalf("begin legacy row %s: %v", id, err)
	}
	defer tx.Rollback()
	if err := s.upsertGenericResourceTx(tx, "children", id, data); err != nil {
		t.Fatalf("seed legacy generic row %s: %v", id, err)
	}
	if err := s.upsertChildrenTx(tx, id, obj, data); err != nil {
		t.Fatalf("seed legacy typed row %s: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy row %s: %v", id, err)
	}
}

func requireParentKeyRowData(t *testing.T, db *sql.DB, table, id string, want json.RawMessage) {
	t.Helper()
	var got string
	query := fmt.Sprintf("SELECT data FROM \"%s\" WHERE id = ?", table)
	if err := db.QueryRow(query, id).Scan(&got); err != nil {
		t.Fatalf("read %s/%q: %v", table, id, err)
	}
	if got != string(want) {
		t.Fatalf("%s/%q data = %s, want %s", table, id, got, want)
	}
}

func requireParentKeyCount(t *testing.T, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("count %q = %d, want %d", query, got, want)
	}
}

func TestMigrateParentKeyStorageIDs(t *testing.T) {
	if StoreSchemaVersion != 5 {
		t.Fatalf("StoreSchemaVersion = %d, want 5 for the v5 migration fixture", StoreSchemaVersion)
	}
	dbPath := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("create current store: %v", err)
	}

	duplicateCurrent := json.RawMessage("{\"id\":\"child-duplicate\",\"parent_id\":\"parent-A\",\"name\":\"currenttoken3691\",\"description\":\"current description\",\"summary\":\"current summary\"}")
	duplicateStale := json.RawMessage("{\"id\":\"child-duplicate\",\"parent_id\":\"parent-A\",\"name\":\"staletoken3691\",\"description\":\"stale description\",\"summary\":\"stale summary\"}")
	legacyOnly := json.RawMessage("{\"id\":\"child-legacy\",\"parent_id\":\"parent-B\",\"name\":\"legacyonlytoken3691\",\"description\":\"legacy description\",\"summary\":\"legacy summary\"}")
	compositeOnly := json.RawMessage("{\"id\":\"child-current\",\"parent_id\":\"parent-C\",\"name\":\"compositeonlytoken3691\",\"description\":\"composite description\",\"summary\":\"composite summary\"}")

	if err := s.UpsertChildren(duplicateCurrent); err != nil {
		t.Fatalf("seed maintained duplicate row: %v", err)
	}
	seedLegacyParentKeyRow(t, s, "child-duplicate", duplicateStale)
	seedLegacyParentKeyRow(t, s, "child-legacy", legacyOnly)
	if err := s.UpsertChildren(compositeOnly); err != nil {
		t.Fatalf("seed composite-only row: %v", err)
	}
	if _, err := s.DB().Exec(` + "`" + `PRAGMA user_version = 4` + "`" + `); err != nil {
		t.Fatalf("stamp pre-migration version: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	upgraded, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open pre-v5 store: %v", err)
	}
	db := upgraded.DB()

	const duplicateComposite = "child-duplicate\x00parent-A"
	const legacyComposite = "child-legacy\x00parent-B"
	const currentComposite = "child-current\x00parent-C"

	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, 3)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, 3)
	for _, bare := range []string{"child-duplicate", "child-legacy", "child-current"} {
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children' AND id = ?` + "`" + `, 0, bare)
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children WHERE id = ?` + "`" + `, 0, bare)
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ?` + "`" + `, 0, ftsRowID("children", bare))
	}

	requireParentKeyRowData(t, db, "resources", duplicateComposite, duplicateCurrent)
	requireParentKeyRowData(t, db, "children", duplicateComposite, duplicateCurrent)
	requireParentKeyRowData(t, db, "resources", legacyComposite, legacyOnly)
	requireParentKeyRowData(t, db, "children", legacyComposite, legacyOnly)
	requireParentKeyRowData(t, db, "resources", currentComposite, compositeOnly)
	requireParentKeyRowData(t, db, "children", currentComposite, compositeOnly)

	for _, composite := range []string{duplicateComposite, legacyComposite, currentComposite} {
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ? AND id = ? AND resource_type = 'children'` + "`" + `, 1, ftsRowID("children", composite), composite)
	}
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH 'staletoken3691'` + "`" + `, 0)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH 'currenttoken3691'` + "`" + `, 1)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts` + "`" + `, 3)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE children_fts MATCH 'staletoken3691'` + "`" + `, 0)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE children_fts MATCH 'currenttoken3691'` + "`" + `, 1)

	if version, err := upgraded.SchemaVersion(); err != nil {
		t.Fatalf("read upgraded version: %v", err)
	} else if version != StoreSchemaVersion {
		t.Fatalf("upgraded version = %d, want %d", version, StoreSchemaVersion)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded store: %v", err)
	}

	// A current-version reopen must be a data no-op.
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen current store: %v", err)
	}
	defer reopened.Close()
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, 3)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, 3)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resource_type = 'children'` + "`" + `, 3)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM children_fts` + "`" + `, 3)
}
`
	testPath := filepath.Join(outputDir, "internal", "store", "parent_key_migration_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestMigrateParentKeyStorageIDs", "-count=1")
}
