package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritableStoreColumnsOmitsGenerated(t *testing.T) {
	t.Parallel()

	cols := []ColumnDef{
		{Name: "id", Type: "TEXT", PrimaryKey: true},
		{Name: "data", Type: "JSON", NotNull: true},
		{Name: storeBareIDColumn, Type: storeBareIDType, Generated: true},
	}

	got := writableStoreColumns(cols)
	require.Len(t, got, 2)
	assert.Equal(t, `"id", "data"`, columnNames(cols))
	assert.Equal(t, "?, ?", columnPlaceholders(cols))
	assert.Equal(t, `"data" = excluded."data"`, updateSet(cols))
}

func TestAttachBareIDColumnsAddsQueryableColumn(t *testing.T) {
	t.Parallel()

	tables := []TableDef{
		{
			Name:            "contacts",
			Resource:        "contacts",
			ParentKeyColumn: "lists_id",
			Columns: []ColumnDef{
				{Name: "id", Type: "TEXT", PrimaryKey: true},
				{Name: "lists_id", Type: "TEXT", NotNull: true},
				{Name: "data", Type: "JSON", NotNull: true},
				{Name: "synced_at", Type: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
			},
		},
		{
			Name:     "lists",
			Resource: "lists",
			Columns:  append([]ColumnDef(nil), baseTableColumns...),
		},
	}

	attachBareIDColumns(tables, nil)

	contacts := tables[0]
	require.True(t, hasNamedColumn(contacts, storeBareIDColumn))
	var bare ColumnDef
	for _, col := range contacts.Columns {
		if col.Name == storeBareIDColumn {
			bare = col
			break
		}
	}
	assert.True(t, bare.Generated)
	assert.Equal(t, storeBareIDType, bare.Type)
	assert.Contains(t, contacts.Indexes, IndexDef{
		Name:      "idx_contacts_bare_id",
		TableName: "contacts",
		Columns:   storeBareIDColumn,
	})

	assert.False(t, hasNamedColumn(tables[1], storeBareIDColumn),
		"non-parent-keyed tables must not grow a bare_id column")
}

func TestAttachBareIDColumnsJSONOnlyFallbackDependent(t *testing.T) {
	t.Parallel()

	tables := []TableDef{{
		Name:             "items",
		Resource:         "items",
		JSONOnlyFallback: true,
		Columns:          append([]ColumnDef(nil), baseTableColumns...),
	}}
	attachBareIDColumns(tables, []profiler.DependentResource{{Name: "items"}})

	require.True(t, hasNamedColumn(tables[0], storeBareIDColumn),
		"JSON-only dependent tables still store a composite id and need a queryable bare_id")
	assert.True(t, tables[0].Columns[len(tables[0].Columns)-1].Generated)
}

func TestAttachBareIDColumnsSkipsExistingBareID(t *testing.T) {
	t.Parallel()

	tables := []TableDef{{
		Name:            "nodes",
		Resource:        "nodes",
		ParentKeyColumn: "files_id",
		Columns: []ColumnDef{
			{Name: "id", Type: "TEXT", PrimaryKey: true},
			{Name: "files_id", Type: "TEXT", NotNull: true},
			{Name: "data", Type: "JSON", NotNull: true},
			{Name: "synced_at", Type: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
			{Name: storeBareIDColumn, Type: "TEXT"},
		},
	}}
	attachBareIDColumns(tables, nil)

	var count int
	for _, col := range tables[0].Columns {
		if col.Name == storeBareIDColumn {
			count++
			assert.False(t, col.Generated, "existing API bare_id column must stay writable")
		}
	}
	assert.Equal(t, 1, count)
}

func parentKeyedBareIDSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"domains": {
			Description: "Manage domains",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/domains", Description: "List domains"},
			},
			SubResources: map[string]spec.Resource{
				"verify": {
					Description: "Verify a domain",
					Endpoints: map[string]spec.Endpoint{
						"get": {
							Method:      "GET",
							Path:        "/domains/{domainId}/verify",
							Description: "Get verification status",
							Params:      []spec.Param{{Name: "domainId", Type: "string", Required: true, Positional: true}},
						},
					},
				},
			},
		},
		"widgets": {
			Description: "Read widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
					Response:    spec.ResponseDef{Type: "array", Item: "Widget"},
				},
				"get": {
					Method:      "GET",
					Path:        "/widgets/{id}",
					Description: "Get widget",
					Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true}},
					Response:    spec.ResponseDef{Type: "object", Item: "Widget"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Widget": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
				{Name: "description", Type: "string"},
			},
		},
	}
	return apiSpec
}

// TestGeneratedStoreBareIDQueryable proves the emitted store contract: a
// parent-keyed typed table writes the NUL-composite primary key and exposes
// a generated bare_id that SQL can filter on. Assertions cover emitted
// CREATE/INSERT shape and compile-plus-runtime behavior through the real
// UpsertBatch writer, including the existing-database ADD COLUMN path.
func TestGeneratedStoreBareIDQueryable(t *testing.T) {
	t.Parallel()

	apiSpec := parentKeyedBareIDSpec("bare-id-query")
	outputDir := filepath.Join(t.TempDir(), "bare-id-query-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeSrc)

	require.Contains(t, src, `"bare_id" TEXT GENERATED ALWAYS AS (substr(id, 1, coalesce(nullif(instr(id, char(0)), 0) - 1, length(id)))) VIRTUAL`,
		"parent-keyed typed table must declare a generated bare_id")
	require.Contains(t, src, `"idx_verify_bare_id" ON "verify"("bare_id")`,
		"bare_id must be indexed for SQL lookups")
	require.Contains(t, src, `{table: "verify", column: "bare_id", decl: "TEXT GENERATED ALWAYS AS (substr(id, 1, coalesce(nullif(instr(id, char(0)), 0) - 1, length(id)))) VIRTUAL"}`,
		"existing databases must backfill bare_id on open")
	require.Contains(t, src, `INSERT INTO "verify" ("id", "domains_id", "data", "synced_at")`,
		"generated bare_id must not appear in INSERT")
	require.NotContains(t, src, `lookupFieldValue(obj, "bare_id")`,
		"upsert must not write the generated bare_id column")
	require.Contains(t, src, "WHERE bare_id = ? finds them",
		"BareResourceID doc must point SQL callers at bare_id")

	// Non-parent-keyed widgets stay on a bare primary key; no generated column.
	require.Contains(t, src, `CREATE TABLE IF NOT EXISTS "widgets"`)
	require.NotContains(t, src, `"idx_widgets_bare_id"`)
	require.NotContains(t, src, `{table: "widgets", column: "bare_id"`)

	testPath := filepath.Join(outputDir, "internal", "store", "bare_id_query_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(generatedBareIDQueryTest), 0o644))

	// Compile the emitted store package rather than `go build ./...`:
	// requireGeneratedCompiles also builds the MCP binary, which this
	// store-only fixture cannot resolve under the harness tidy no-op.
	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestBareID_", "-count=1")
}

const generatedBareIDQueryTest = `package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBareID_UpsertBatchQueryableByBareID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	items := []json.RawMessage{
		json.RawMessage(` + "`" + `{"id":"rec-1","domains_id":"parent-a"}` + "`" + `),
		json.RawMessage(` + "`" + `{"id":"rec-1","domains_id":"parent-b"}` + "`" + `),
	}
	if _, _, err := s.UpsertBatch("verify", items); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	db := s.DB()
	var n int
	if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM "verify" WHERE id = ?` + "`" + `, "rec-1").Scan(&n); err != nil {
		t.Fatalf("count by composite id: %v", err)
	}
	if n != 0 {
		t.Fatalf("WHERE id = bare API id matched %d rows; composite primary keys must miss", n)
	}
	if err := db.QueryRow(` + "`" + `SELECT COUNT(*) FROM "verify" WHERE bare_id = ?` + "`" + `, "rec-1").Scan(&n); err != nil {
		t.Fatalf("count by bare_id: %v", err)
	}
	if n != 2 {
		t.Fatalf("WHERE bare_id = rec-1 matched %d rows, want 2 (one per parent)", n)
	}

	var storageID string
	if err := db.QueryRow(` + "`" + `SELECT id FROM "verify" WHERE bare_id = ? AND domains_id = ?` + "`" + `, "rec-1", "parent-a").Scan(&storageID); err != nil {
		t.Fatalf("select storage id: %v", err)
	}
	if BareResourceID(storageID) != "rec-1" {
		t.Fatalf("BareResourceID(%q) = %q, want rec-1", storageID, BareResourceID(storageID))
	}
	if !strings.Contains(storageID, "\x00") {
		t.Fatalf("storage id %q is not the NUL-composite the writer produces", storageID)
	}
}

func TestBareID_UpgradeExistingCompositeRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(` + "`" + `CREATE TABLE "verify" (
		id TEXT PRIMARY KEY,
		domains_id TEXT NOT NULL,
		data JSON NOT NULL,
		synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)` + "`" + `); err != nil {
		raw.Close()
		t.Fatalf("create old table: %v", err)
	}
	if _, err := raw.Exec(` + "`" + `INSERT INTO "verify" (id, domains_id, data) VALUES (?, ?, ?)` + "`" + `,
		"rec-1\x00parent-a", "parent-a", ` + "`" + `{"id":"rec-1"}` + "`" + `); err != nil {
		raw.Close()
		t.Fatalf("insert composite row: %v", err)
	}
	raw.Close()

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open upgraded store: %v", err)
	}
	defer s.Close()

	var n int
	if err := s.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM "verify" WHERE bare_id = ?` + "`" + `, "rec-1").Scan(&n); err != nil {
		t.Fatalf("count upgraded bare_id: %v", err)
	}
	if n != 1 {
		t.Fatalf("upgraded bare_id match = %d, want 1 (generated column must cover pre-existing composite rows)", n)
	}
}
`
