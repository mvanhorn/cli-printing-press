package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedStoreMigratesLegacyParentKeyRows exercises the emitted store,
// not the template text. It seeds the pre-upgrade bare-id shape alongside
// current composite rows, downgrades the version stamp, and proves Open
// migrates the generic table, typed projection, and both FTS indexes.
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
	source := string(storeSource)
	require.Contains(t, source, `"children": {"parent_id"}`)
	require.Contains(t, source, "migrateParentKeyStorageIDs")
	require.Contains(t, source, "s.migrateParentKeyStorageIDs(ctx, conn)")
	require.NotContains(t, source, "parentKeyStorageIDSchemaVersion")
	require.Contains(t, source, "parentKeyLegacyBatchSize")
	require.Contains(t, source, "idx_resources_legacy_parent_key")
	require.Contains(t, source, "SELECT 1 FROM resources INDEXED BY ")
	require.Contains(t, source, "if !bare {")
	require.Contains(t, source, "const StoreSchemaVersion = 7")
	require.Contains(t, source, "const resourcesFTSTokenizerSchemaVersion = 6")

	testPath := filepath.Join(outputDir, "internal", "store", "parent_key_migration_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(parentKeyMigrationInlineTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestMigrateParentKeyStorageIDs", "-count=1")
}

const parentKeyMigrationInlineTest = `package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	query := fmt.Sprintf("SELECT data FROM %q WHERE id = ?", table)
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

func requireParentKeyProbeUsesIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	var indexSQL string
	if err := db.QueryRow(` + "`" + `SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_resources_legacy_parent_key'` + "`" + `).Scan(&indexSQL); err != nil {
		t.Fatalf("read parent-key legacy index: %v", err)
	}
	const wantPrefix = "CREATE INDEX idx_resources_legacy_parent_key ON resources(resource_type, id) WHERE "
	if !strings.HasPrefix(indexSQL, wantPrefix) || !strings.Contains(indexSQL, "instr(id, char(0)) = 0") || !strings.Contains(indexSQL, "'children'") {
		t.Fatalf("index sql = %q", indexSQL)
	}
	where := strings.TrimPrefix(indexSQL, wantPrefix)
	rows, err := db.Query(` + "`" + `EXPLAIN QUERY PLAN SELECT 1 FROM resources INDEXED BY idx_resources_legacy_parent_key WHERE ` + "`" + ` + where + ` + "`" + ` LIMIT 1` + "`" + `)
	if err != nil {
		t.Fatalf("explain probe: %v", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("explain columns: %v", err)
	}
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var plan strings.Builder
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("explain scan: %v", err)
		}
		for _, v := range vals {
			plan.WriteString(v.String)
			plan.WriteByte(' ')
		}
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	got := plan.String()
	if !strings.Contains(got, "idx_resources_legacy_parent_key") || strings.Contains(strings.ToUpper(got), "SCAN") {
		t.Fatalf("probe plan = %q, want a seek of idx_resources_legacy_parent_key", got)
	}
}

func requireParentKeySearch(t *testing.T, s *Store, token string, want int) {
	t.Helper()
	hits, err := s.Search(token, 10, "children")
	if err != nil {
		t.Fatalf("search %s: %v", token, err)
	}
	if len(hits) != want {
		t.Fatalf("search %s hits = %d, want %d (%s)", token, len(hits), want, hits)
	}
	for _, hit := range hits {
		if !strings.Contains(string(hit), token) {
			t.Fatalf("search %s hit %s does not contain the token", token, hit)
		}
	}
}

func TestMigrateParentKeyStorageIDs(t *testing.T) {
	if StoreSchemaVersion != 7 {
		t.Fatalf("StoreSchemaVersion = %d, want 7 for the v7 migration fixture", StoreSchemaVersion)
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
	noParent := json.RawMessage("{\"id\":\"child-noparent\",\"name\":\"noparenttoken3691\",\"description\":\"noparent description\",\"summary\":\"noparent summary\"}")
	// Same bare entity under two parents. Only the parent-A association is
	// still in the legacy shape; parent-B was written by a current upsert.
	sharedLegacy := json.RawMessage("{\"id\":\"child-shared\",\"parent_id\":\"parent-A\",\"name\":\"sharedparentatoken3691\",\"description\":\"shared a description\",\"summary\":\"shared a summary\"}")
	sharedCurrent := json.RawMessage("{\"id\":\"child-shared\",\"parent_id\":\"parent-B\",\"name\":\"sharedparentbtoken3691\",\"description\":\"shared b description\",\"summary\":\"shared b summary\"}")

	if err := s.UpsertChildren(duplicateCurrent); err != nil {
		t.Fatalf("seed maintained duplicate row: %v", err)
	}
	seedLegacyParentKeyRow(t, s, "child-duplicate", duplicateStale)
	seedLegacyParentKeyRow(t, s, "child-legacy", legacyOnly)
	if err := s.UpsertChildren(compositeOnly); err != nil {
		t.Fatalf("seed composite-only row: %v", err)
	}
	seedLegacyParentKeyRow(t, s, "child-noparent", noParent)
	seedLegacyParentKeyRow(t, s, "child-shared", sharedLegacy)
	if err := s.UpsertChildren(sharedCurrent); err != nil {
		t.Fatalf("seed other-parent composite row: %v", err)
	}
	if _, err := s.DB().Exec(` + "`" + `INSERT INTO resources (id, resource_type, data) VALUES ('note-1', 'notes', '{"name":"notesentinel3691"}')` + "`" + `); err != nil {
		t.Fatalf("seed unrelated resource: %v", err)
	}
	if _, err := s.DB().Exec(` + "`" + `INSERT INTO resources_fts (rowid, id, resource_type, content) VALUES (?, 'note-1', 'notes', 'notesentinel3691')` + "`" + `, ftsRowID("notes", "note-1")); err != nil {
		t.Fatalf("seed unrelated FTS row: %v", err)
	}

	requireParentKeyCount(t, s.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, 7)
	requireParentKeyCount(t, s.DB(), ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, 7)

	version, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("read fresh version: %v", err)
	}
	if version != StoreSchemaVersion {
		t.Fatalf("fresh version = %d, want %d", version, StoreSchemaVersion)
	}
	if _, err := s.DB().Exec(fmt.Sprintf("PRAGMA user_version = %d", StoreSchemaVersion-1)); err != nil {
		t.Fatalf("stamp pre-migration version: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	upgraded, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open pre-v7 store: %v", err)
	}
	db := upgraded.DB()

	const duplicateComposite = "child-duplicate\x00parent-A"
	const legacyComposite = "child-legacy\x00parent-B"
	const currentComposite = "child-current\x00parent-C"
	const sharedLegacyComposite = "child-shared\x00parent-A"
	const sharedCurrentComposite = "child-shared\x00parent-B"

	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, 6)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, 6)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children' AND instr(id, char(0)) = 0` + "`" + `, 1)
	for _, bare := range []string{"child-duplicate", "child-legacy", "child-current", "child-shared"} {
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
	requireParentKeyRowData(t, db, "resources", "child-noparent", noParent)
	requireParentKeyRowData(t, db, "children", "child-noparent", noParent)
	requireParentKeyRowData(t, db, "resources", sharedLegacyComposite, sharedLegacy)
	requireParentKeyRowData(t, db, "children", sharedLegacyComposite, sharedLegacy)
	requireParentKeyRowData(t, db, "resources", sharedCurrentComposite, sharedCurrent)
	requireParentKeyRowData(t, db, "children", sharedCurrentComposite, sharedCurrent)

	for _, pair := range []struct{ id, bare string }{
		{duplicateComposite, "child-duplicate"},
		{legacyComposite, "child-legacy"},
		{currentComposite, "child-current"},
		{sharedLegacyComposite, "child-shared"},
		{sharedCurrentComposite, "child-shared"},
		{"child-noparent", "child-noparent"},
	} {
		var bareID string
		if err := db.QueryRow(` + "`" + `SELECT bare_id FROM children WHERE id = ?` + "`" + `, pair.id).Scan(&bareID); err != nil {
			t.Fatalf("read bare_id for %q: %v", pair.id, err)
		}
		if bareID != pair.bare {
			t.Fatalf("bare_id for %q = %q, want %q", pair.id, bareID, pair.bare)
		}
	}

	if _, err := upgraded.Get("children", "child-duplicate"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("bare duplicate lookup err = %v, want sql.ErrNoRows", err)
	}
	if got, err := upgraded.Get("children", duplicateComposite); err != nil {
		t.Fatalf("get composite duplicate: %v", err)
	} else if string(got) != string(duplicateCurrent) {
		t.Fatalf("get composite duplicate = %s, want current payload", got)
	}

	for _, composite := range []string{duplicateComposite, legacyComposite, currentComposite, sharedLegacyComposite, sharedCurrentComposite} {
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ? AND id = ? AND resource_type = 'children'` + "`" + `, 1, ftsRowID("children", composite), composite)
	}
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ? AND id = ? AND resource_type = 'children'` + "`" + `, 1, ftsRowID("children", "child-noparent"), "child-noparent")
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 0, FTSMatchQuery("staletoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 1, FTSMatchQuery("currenttoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 1, FTSMatchQuery("sharedparentatoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 1, FTSMatchQuery("sharedparentbtoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts` + "`" + `, 6)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE rowid NOT IN (SELECT rowid FROM children)` + "`" + `, 0)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE children_fts MATCH ?` + "`" + `, 0, FTSMatchQuery("staletoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE children_fts MATCH ?` + "`" + `, 1, FTSMatchQuery("currenttoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts AS f LEFT JOIN resources AS r ON r.resource_type = f.resource_type AND r.id = f.id WHERE r.id IS NULL` + "`" + `, 0)

	var sentinel string
	var sentinelRowID int64
	if err := db.QueryRow(` + "`" + `SELECT rowid, content FROM resources_fts WHERE resource_type = 'notes' AND id = 'note-1'` + "`" + `).Scan(&sentinelRowID, &sentinel); err != nil {
		t.Fatalf("read unrelated FTS row: %v", err)
	}
	if sentinel != "notesentinel3691" || sentinelRowID != ftsRowID("notes", "note-1") {
		t.Fatalf("unrelated FTS row = rowid %d content %q, want preserved sentinel", sentinelRowID, sentinel)
	}

	requireParentKeySearch(t, upgraded, "currenttoken3691", 1)
	requireParentKeySearch(t, upgraded, "staletoken3691", 0)
	requireParentKeySearch(t, upgraded, "legacyonlytoken3691", 1)
	requireParentKeySearch(t, upgraded, "noparenttoken3691", 1)
	requireParentKeySearch(t, upgraded, "sharedparentatoken3691", 1)
	requireParentKeySearch(t, upgraded, "sharedparentbtoken3691", 1)

	if version, err := upgraded.SchemaVersion(); err != nil {
		t.Fatalf("read upgraded version: %v", err)
	} else if version != StoreSchemaVersion {
		t.Fatalf("upgraded version = %d, want %d", version, StoreSchemaVersion)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded store: %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen current store: %v", err)
	}
	defer reopened.Close()
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, 6)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, 6)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resource_type = 'children'` + "`" + `, 6)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM children_fts` + "`" + `, 6)
	requireParentKeyRowData(t, reopened.DB(), "resources", duplicateComposite, duplicateCurrent)
	requireParentKeyRowData(t, reopened.DB(), "resources", sharedLegacyComposite, sharedLegacy)
	var reopenedSentinel string
	if err := reopened.DB().QueryRow(` + "`" + `SELECT content FROM resources_fts WHERE resource_type = 'notes' AND id = 'note-1'` + "`" + `).Scan(&reopenedSentinel); err != nil {
		t.Fatalf("read sentinel after reopen: %v", err)
	}
	if reopenedSentinel != "notesentinel3691" {
		t.Fatalf("sentinel after reopen = %q", reopenedSentinel)
	}
	requireParentKeyProbeUsesIndex(t, reopened.DB())
}

func TestMigrateParentKeyStorageIDsAtCurrentVersion(t *testing.T) {
	if StoreSchemaVersion != 7 {
		t.Fatalf("StoreSchemaVersion = %d, want 7 for the v7 migration fixture", StoreSchemaVersion)
	}
	dbPath := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("create current store: %v", err)
	}
	version, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("read fresh version: %v", err)
	}
	if version != StoreSchemaVersion {
		t.Fatalf("fresh version = %d, want %d", version, StoreSchemaVersion)
	}

	duplicateCurrent := json.RawMessage("{\"id\":\"child-duplicate\",\"parent_id\":\"parent-A\",\"name\":\"currenttoken3691\",\"description\":\"current description\",\"summary\":\"current summary\"}")
	duplicateStale := json.RawMessage("{\"id\":\"child-duplicate\",\"parent_id\":\"parent-A\",\"name\":\"staletoken3691\",\"description\":\"stale description\",\"summary\":\"stale summary\"}")
	noParent := json.RawMessage("{\"id\":\"child-noparent\",\"name\":\"noparenttoken3691\",\"description\":\"noparent description\",\"summary\":\"noparent summary\"}")
	if err := s.UpsertChildren(duplicateCurrent); err != nil {
		t.Fatalf("seed maintained duplicate row: %v", err)
	}
	seedLegacyParentKeyRow(t, s, "child-duplicate", duplicateStale)
	seedLegacyParentKeyRow(t, s, "child-noparent", noParent)

	batchN := parentKeyLegacyBatchSize + 1
	batchPayloads := make([]json.RawMessage, batchN)
	for i := 0; i < batchN; i++ {
		id := fmt.Sprintf("child-b-%03d", i)
		batchPayloads[i] = json.RawMessage(fmt.Sprintf("{\"id\":%q,\"parent_id\":\"parent-batch\",\"name\":\"batchtoken3691%03d\",\"description\":\"batch description\",\"summary\":\"batch summary\"}", id, i))
		seedLegacyParentKeyRow(t, s, id, batchPayloads[i])
	}
	requireParentKeyCount(t, s.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, batchN+3)
	if err := s.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	upgraded, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open current-version store: %v", err)
	}
	db := upgraded.DB()
	if version, err := upgraded.SchemaVersion(); err != nil {
		t.Fatalf("read version: %v", err)
	} else if version != StoreSchemaVersion {
		t.Fatalf("version = %d, want %d", version, StoreSchemaVersion)
	}

	const duplicateComposite = "child-duplicate\x00parent-A"
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, batchN+2)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, batchN+2)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children' AND instr(id, char(0)) = 0` + "`" + `, 1)
	requireParentKeyRowData(t, db, "resources", duplicateComposite, duplicateCurrent)
	requireParentKeyRowData(t, db, "children", duplicateComposite, duplicateCurrent)
	requireParentKeyRowData(t, db, "resources", "child-noparent", noParent)
	for _, i := range []int{0, parentKeyLegacyBatchSize - 1, parentKeyLegacyBatchSize} {
		id := fmt.Sprintf("child-b-%03d", i)
		storageID := id + "\x00parent-batch"
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children' AND id = ?` + "`" + `, 0, id)
		requireParentKeyRowData(t, db, "resources", storageID, batchPayloads[i])
		requireParentKeyRowData(t, db, "children", storageID, batchPayloads[i])
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ?` + "`" + `, 0, ftsRowID("children", id))
		requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE rowid = ? AND id = ?` + "`" + `, 1, ftsRowID("children", storageID), storageID)
	}
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 0, FTSMatchQuery("staletoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts WHERE resources_fts MATCH ?` + "`" + `, 1, FTSMatchQuery("currenttoken3691"))
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM children_fts WHERE rowid NOT IN (SELECT rowid FROM children)` + "`" + `, 0)
	requireParentKeyCount(t, db, ` + "`" + `SELECT COUNT(*) FROM resources_fts AS f LEFT JOIN resources AS r ON r.resource_type = f.resource_type AND r.id = f.id WHERE r.id IS NULL` + "`" + `, 0)
	requireParentKeySearch(t, upgraded, "currenttoken3691", 1)
	requireParentKeySearch(t, upgraded, "staletoken3691", 0)
	requireParentKeySearch(t, upgraded, fmt.Sprintf("batchtoken3691%03d", parentKeyLegacyBatchSize), 1)
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded store: %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen current store: %v", err)
	}
	defer reopened.Close()
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children'` + "`" + `, batchN+2)
	requireParentKeyCount(t, reopened.DB(), ` + "`" + `SELECT COUNT(*) FROM children` + "`" + `, batchN+2)
	requireParentKeyRowData(t, reopened.DB(), "resources", duplicateComposite, duplicateCurrent)
	requireParentKeyProbeUsesIndex(t, reopened.DB())
}

func TestMigrateParentKeyStorageIDsRebuildsStaleIndex(t *testing.T) {
	if StoreSchemaVersion != 7 {
		t.Fatalf("StoreSchemaVersion = %d, want 7 for the v7 migration fixture", StoreSchemaVersion)
	}
	dbPath := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("create current store: %v", err)
	}
	legacyOnly := json.RawMessage("{\"id\":\"child-legacy\",\"parent_id\":\"parent-B\",\"name\":\"legacyonlytoken3691\",\"description\":\"legacy description\",\"summary\":\"legacy summary\"}")
	seedLegacyParentKeyRow(t, s, "child-legacy", legacyOnly)
	// A predicate that omits children is what a reprint leaves behind when
	// the type list grows. The next open has to rebuild it and still re-key.
	if _, err := s.DB().Exec(` + "`" + `DROP INDEX idx_resources_legacy_parent_key` + "`" + `); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if _, err := s.DB().Exec(` + "`" + `CREATE INDEX idx_resources_legacy_parent_key ON resources(resource_type, id) WHERE instr(id, char(0)) = 0 AND resource_type IN ('not_children')` + "`" + `); err != nil {
		t.Fatalf("create stale index: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	upgraded, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store with stale index: %v", err)
	}
	defer upgraded.Close()
	const legacyComposite = "child-legacy\x00parent-B"
	requireParentKeyRowData(t, upgraded.DB(), "resources", legacyComposite, legacyOnly)
	requireParentKeyCount(t, upgraded.DB(), ` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'children' AND id = ?` + "`" + `, 0, "child-legacy")
	var indexSQL string
	if err := upgraded.DB().QueryRow(` + "`" + `SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_resources_legacy_parent_key'` + "`" + `).Scan(&indexSQL); err != nil {
		t.Fatalf("read rebuilt index: %v", err)
	}
	if !strings.Contains(indexSQL, "'children'") || strings.Contains(indexSQL, "not_children") {
		t.Fatalf("index sql = %q", indexSQL)
	}
	if version, err := upgraded.SchemaVersion(); err != nil {
		t.Fatalf("read version: %v", err)
	} else if version != StoreSchemaVersion {
		t.Fatalf("version = %d, want %d", version, StoreSchemaVersion)
	}
	requireParentKeyProbeUsesIndex(t, upgraded.DB())
}
`
