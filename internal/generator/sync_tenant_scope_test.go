package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedTenantScopedSyncStateAndPrune(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-state")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Cache.Enabled = true
	apiSpec.EndpointTemplateVars = []string{"tenant"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/items",
					Response: spec.ResponseDef{Type: "array", Item: "Item"},
					IDField:  "id",
					Params: []spec.Param{{
						Name:        "tenant",
						Type:        "integer",
						GlobalScope: true,
					}},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "syncScopeKey(syncScopeVars, pathContext, userParams)")
	require.Contains(t, syncSrc, "db.SetSyncScope(syncScope)")
	require.Contains(t, syncSrc, `"reason":"tenant_scoped_walk"`)
	require.Contains(t, syncSrc, `"reason":"tenant_scoped_checkpoint_exists"`)

	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	require.Contains(t, storeSrc, "PRIMARY KEY (resource_type, scope_key)")
	require.Contains(t, storeSrc, "func (s *Store) migrateSyncStateScope")
	require.Contains(t, storeSrc, "func (s *Store) HasScopedSyncState()")

	refreshSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auto_refresh.go")
	require.Contains(t, refreshSrc, "db.SetSyncScope(syncScope)")
	require.Contains(t, refreshSrc, "autoRefreshScopedDecision(probe, resources, policy)")

	modulePath := generatedModulePath(t, outputDir)
	testSrc := strings.ReplaceAll(tenantScopedSyncTestSource, "__MODULE_PATH__", modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "tenant_scoped_sync_test.go"), []byte(testSrc), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestTenantScoped", "-count=1")
	// The emitted store tests include the schema-upgrade cases; run them
	// against the tenant-scoped schema, not just the single-tenant one. The
	// multi-process writer test is load-sensitive and already runs in the
	// store hardening generator tests, so it is skipped here.
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-count=1", "-skip", "^TestHardenSQLiteFilesConcurrentWritersKeepIntegrity$")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedSingleTenantSyncKeepsWholeTablePrune(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("single-tenant-scope")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/items",
					Response: spec.ResponseDef{Type: "array", Item: "Item"},
					IDField:  "id",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "db.ReconcileAll(")
	require.NotContains(t, syncSrc, "syncScopeKey(")
	require.NotContains(t, syncSrc, "tenant_scoped_walk")

	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	require.Contains(t, storeSrc, "const StoreSchemaVersion = 12")
	require.Contains(t, storeSrc, "resource_type TEXT PRIMARY KEY")
	require.NotContains(t, storeSrc, "scope_key")
	require.NotContains(t, storeSrc, "migrateSyncStateScope")
	requireGeneratedCompiles(t, outputDir)
}

const tenantScopedSyncTestSource = `package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"__MODULE_PATH__/internal/store"
	_ "modernc.org/sqlite"
)

type tenantScopedClient struct {
	response json.RawMessage
	params   []map[string]string
}

func (c *tenantScopedClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copy := map[string]string{}
	for key, value := range params {
		copy[key] = value
	}
	c.params = append(c.params, copy)
	return c.response, nil
}

func (c *tenantScopedClient) RateLimit() float64 { return 0 }

func TestTenantScopedSyncKeepsCheckpointsAndRowsSeparate(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, _, err := db.UpsertBatch("items", []json.RawMessage{json.RawMessage(` + "`" + `{"id":"tenant-a-row"}` + "`" + `)}); err != nil {
		t.Fatalf("seed tenant A: %v", err)
	}
	paramsA := &syncUserParams{flatGlobal: map[string]string{"tenant": "tenant-a"}}
	paramsB := &syncUserParams{flatGlobal: map[string]string{"tenant": "tenant-b"}}
	scopeA := syncScopeKey(nil, nil, paramsA)
	scopeB := syncScopeKey(nil, nil, paramsB)
	if scopeA == "" || scopeB == "" || scopeA == scopeB {
		t.Fatalf("tenant scopes = %q / %q, want distinct non-empty values", scopeA, scopeB)
	}

	db.SetSyncScope(scopeA)
	if err := db.SaveSyncProgress("items", "tenant-a-cursor", 1); err != nil {
		t.Fatalf("save tenant A cursor: %v", err)
	}
	db.SetSyncScope(scopeB)
	if cursor, _, _, err := db.GetSyncState("items"); err != nil || cursor != "" {
		t.Fatalf("tenant B inherited tenant A cursor %q (err %v)", cursor, err)
	}

	client := &tenantScopedClient{response: json.RawMessage(` + "`" + `[{"id":"tenant-b-row"}]` + "`" + `)}
	var events bytes.Buffer
	result := syncResource(context.Background(), client, db, "items", "", true, 0, false, true, paramsB, &events)
	if result.Err != nil || result.Warn != nil {
		t.Fatalf("tenant B full sync = %+v", result)
	}
	if _, err := db.Get("items", "tenant-a-row"); err != nil {
		t.Fatalf("tenant A row was pruned by tenant B full sync: %v", err)
	}
	if !strings.Contains(events.String(), ` + "`" + `"reason":"tenant_scoped_walk"` + "`" + `) {
		t.Fatalf("full tenant walk did not report skipped whole-table prune: %s", events.String())
	}

	db.SetSyncScope(scopeA)
	if cursor, _, _, err := db.GetSyncState("items"); err != nil || cursor != "tenant-a-cursor" {
		t.Fatalf("tenant A checkpoint changed after tenant B sync: cursor=%q err=%v", cursor, err)
	}
	if len(client.params) != 1 || client.params[0]["tenant"] != "tenant-b" {
		t.Fatalf("tenant B request params = %#v", client.params)
	}
}

func TestTenantScopedSyncSkipsUnscopedPruneAfterScopedCheckpoint(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	params := &syncUserParams{flatGlobal: map[string]string{"tenant": "2"}}
	scoped := syncScopeKey(nil, nil, params)
	if scoped == "" {
		t.Fatal("tenant scope is empty")
	}
	db.SetSyncScope(scoped)
	seed := &tenantScopedClient{response: json.RawMessage(` + "`" + `[{"id":"tenant-2-row"}]` + "`" + `)}
	if result := syncResource(context.Background(), seed, db, "items", "", true, 0, false, true, params, io.Discard); result.Err != nil || result.Warn != nil {
		t.Fatalf("scoped sync = %+v", result)
	}
	if _, err := db.Get("items", "tenant-2-row"); err != nil {
		t.Fatalf("scoped row missing: %v", err)
	}

	db.SetSyncScope("")
	var events bytes.Buffer
	unscoped := &tenantScopedClient{response: json.RawMessage(` + "`" + `[]` + "`" + `)}
	if result := syncResource(context.Background(), unscoped, db, "items", "", true, 0, false, true, nil, &events); result.Err != nil || result.Warn != nil {
		t.Fatalf("unscoped full sync = %+v", result)
	}
	if _, err := db.Get("items", "tenant-2-row"); err != nil {
		t.Fatalf("unscoped full sync pruned tenant row: %v", err)
	}
	if !strings.Contains(events.String(), ` + "`" + `"reason":"tenant_scoped_checkpoint_exists"` + "`" + `) {
		t.Fatalf("unscoped full sync did not report scoped checkpoint: %s", events.String())
	}
}

func TestTenantScopedSyncResourceParamsUseSeparateCheckpoints(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	paramsA := &syncUserParams{perResource: map[string]map[string]string{"items": {"tenant": "tenant-a"}}}
	paramsB := &syncUserParams{perResource: map[string]map[string]string{"items": {"tenant": "tenant-b"}}}
	scopeA := syncScopeKey(nil, nil, paramsA)
	scopeB := syncScopeKey(nil, nil, paramsB)
	if scopeA == "" || scopeB == "" || scopeA == scopeB {
		t.Fatalf("resource scopes = %q / %q, want distinct non-empty values", scopeA, scopeB)
	}

	db.SetSyncScope(scopeA)
	if err := db.SaveSyncProgress("items", "tenant-a-cursor", 1); err != nil {
		t.Fatalf("save tenant A cursor: %v", err)
	}
	db.SetSyncScope(scopeB)
	if cursor, _, _, err := db.GetSyncState("items"); err != nil || cursor != "" {
		t.Fatalf("tenant B inherited tenant A cursor %q (err %v)", cursor, err)
	}
	if err := db.SaveSyncProgress("items", "tenant-b-cursor", 1); err != nil {
		t.Fatalf("save tenant B cursor: %v", err)
	}
	db.SetSyncScope(scopeA)
	if cursor, _, _, err := db.GetSyncState("items"); err != nil || cursor != "tenant-a-cursor" {
		t.Fatalf("tenant A checkpoint changed after tenant B sync: cursor=%q err=%v", cursor, err)
	}
}

func TestTenantScopedAutoRefreshUsesConfiguredScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("TENANT_STATE_TENANT", "tenant-a")

	db, err := store.Open(defaultDBPath("tenant-state-pp-cli"))
	if err != nil {
		t.Fatal(err)
	}
	scopeA := syncScopeKey(map[string]string{"tenant": "tenant-a"}, nil, nil)
	scopeB := syncScopeKey(map[string]string{"tenant": "tenant-b"}, nil, nil)
	db.SetSyncScope(scopeA)
	if err := db.SaveSyncState("items", "", 1); err != nil {
		db.Close()
		t.Fatalf("save configured tenant checkpoint: %v", err)
	}
	db.SetSyncScope(scopeB)
	if err := db.SaveSyncStateAt("items", "", 1, time.Now().Add(-48*time.Hour)); err != nil {
		db.Close()
		t.Fatalf("save other tenant checkpoint: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	meta := autoRefreshIfStale(ctx, &rootFlags{dataSource: "auto"}, []string{"items"})
	if meta.Decision != "fresh" || meta.Ran {
		t.Fatalf("auto-refresh = %+v, want fresh configured tenant checkpoint", meta)
	}
}

func TestTenantScopedSyncMigratesLegacyCheckpointToEmptyScope(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(` + "`" + `CREATE TABLE sync_state (
		resource_type TEXT PRIMARY KEY,
		last_cursor TEXT,
		last_synced_at DATETIME,
		total_count INTEGER DEFAULT 0,
		last_attempt_complete INTEGER NOT NULL DEFAULT 0
	)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(` + "`" + `INSERT INTO sync_state(resource_type, last_cursor, total_count) VALUES ('items', 'legacy-cursor', 3)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(` + "`" + `PRAGMA user_version = 7` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}
	defer db.Close()
	if cursor, _, count, err := db.GetSyncState("items"); err != nil || cursor != "legacy-cursor" || count != 3 {
		t.Fatalf("legacy checkpoint after migration = cursor %q count %d err %v", cursor, count, err)
	}
	if cursor, _, _, err := db.GetSyncStateScoped("items", "tenant-b"); err != nil || cursor != "" {
		t.Fatalf("new tenant scope inherited legacy checkpoint %q (err %v)", cursor, err)
	}
	var scope string
	if err := db.DB().QueryRow(` + "`" + `SELECT scope_key FROM sync_state WHERE resource_type = 'items'` + "`" + `).Scan(&scope); err != nil || scope != "" {
		t.Fatalf("migrated legacy scope = %q (err %v), want empty", scope, err)
	}
}

func TestTenantScopedSyncLeavesNonCheckpointSyncStateAlone(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "domain-sync-state.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(` + "`" + `CREATE TABLE sync_state (
		id TEXT PRIMARY KEY,
		data JSON NOT NULL,
		synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(` + "`" + `INSERT INTO sync_state(id, data) VALUES ('domain-row', '{}')` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open domain sync_state table: %v", err)
	}
	var data string
	if err := db.DB().QueryRow(` + "`" + `SELECT data FROM sync_state WHERE id = 'domain-row'` + "`" + `).Scan(&data); err != nil || data != "{}" {
		t.Fatalf("domain sync_state row after open = %q (err %v)", data, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen domain sync_state table: %v", err)
	}
	defer db.Close()
	if _, err := db.DB().Exec(` + "`" + `SELECT 1 FROM sync_state` + "`" + `); err != nil {
		t.Fatalf("domain sync_state table unavailable after migration: %v", err)
	}
}
`
