package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedReadOnlyStoreOpensDoNotMigrate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ro-store-open")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Cache.Enabled = true
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	rootSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "cli", "root.go"))
	assert.Contains(t, rootSrc, "commandMayWriteStore(cmd)",
		"PersistentPreRunE must skip learn/playbook RW init on read-only commands")
	assert.Contains(t, rootSrc, `ann["mcp:read-only"] == "true"`,
		"read-only detection must honor mcp:read-only")
	assert.Contains(t, rootSrc, "commandIsHelpInvocation(cmd)",
		"help must not trigger store-writing PreRun hooks")

	doctorSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go"))
	assert.Contains(t, doctorSrc, "store.OpenReadOnlyContext(ctx, dbPath)",
		"doctor cache report must open the store read-only")
	assert.NotContains(t, doctorSrc, "store.OpenWithContext(ctx, dbPath)",
		"doctor must not migrate the operator store")
	assert.Contains(t, doctorSrc, `report["migration_pending"]`,
		"doctor must report whether a schema migration is pending")
	assert.Contains(t, doctorSrc, `report["store_schema_version"]`)
	assert.Contains(t, doctorSrc, "store.StoreSchemaVersion",
		"store_schema_version must be the binary's supported schema, not the on-disk version")

	learnSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "cli", "learn_init.go"))
	assert.Contains(t, learnSrc, "store.OpenWithContext(ctx, dbPath)",
		"learn init still migrates when a write command actually runs it")

	refreshSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "cli", "auto_refresh.go"))
	assert.Contains(t, refreshSrc, "store.OpenReadOnlyContext(ctx, dbPath)")
	assert.Contains(t, refreshSrc, "store.OpenWithContext(ctx, dbPath)")
	assert.Less(t, strings.Index(refreshSrc, "store.OpenReadOnlyContext"), strings.Index(refreshSrc, "store.OpenWithContext"),
		"auto-refresh must probe read-only before opening read-write")

	rootTest := readGeneratedFile(t, outputDir, "internal", "cli", "root_test.go")
	assert.Contains(t, rootTest, "func TestMain(m *testing.M)",
		"generated cli tests must sandbox HOME/XDG before executing RootCmd")
	assert.Contains(t, rootTest, "testenv.RunSandboxed(m)")

	storeSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "store", "store.go"))
	assert.Contains(t, storeSrc, `?mode=ro&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(0)`)
	assert.NotContains(t, storeSrc, `?mode=ro&immutable=1&_pragma=busy_timeout(5000)`)
	assert.Contains(t, storeSrc, "idx_resources_type_updated")
	assert.Contains(t, storeSrc, "func (s *Store) ListScan(")
	assert.Contains(t, storeSrc, "func (s *Store) ListRange(")
	assert.Contains(t, storeSrc, "func (s *Store) typedNewestFirstOrder(")
	assert.Contains(t, storeSrc, `"synced_at"`)

	dataSrc := stripGoComments(readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go"))
	assert.Contains(t, dataSrc, "func loadLocalList(")
	assert.Contains(t, dataSrc, "scanLocalList(")
	assert.NotContains(t, dataSrc, "db.List(resourceType, 0)")
	assert.NotContains(t, dataSrc, "ListTypedRange(",
		"unfiltered local list must scan valid rows before take/offset, not SQL LIMIT")

	refreshSrc = stripGoComments(refreshSrc)
	assert.Contains(t, refreshSrc, "storeMissing")
	assert.NotContains(t, refreshSrc, `meta.Reason = "no-store"`,
		"absent store must enter hydration, not skip as no-store")
	assert.NotContains(t, refreshSrc, "DecisionFresh || decision == cliutil.DecisionNoStore")

	requireGeneratedCompiles(t, outputDir)

	testPath := filepath.Join(outputDir, "internal", "cli", "readonly_store_open_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ro-store-open-pp-cli/internal/store"
	_ "modernc.org/sqlite"
)

func TestDoctorAndReadCommandsDoNotMigrate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	dbPath := defaultDBPath("ro-store-open-pp-cli")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`+"`"+`CREATE TABLE resources (
		id TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		data JSON NOT NULL,
		synced_at DATETIME,
		updated_at DATETIME,
		PRIMARY KEY (resource_type, id)
	)`+"`"+`); err != nil {
		t.Fatalf("create resources: %v", err)
	}
	if _, err := raw.Exec(`+"`"+`CREATE TABLE sync_state (
		resource_type TEXT PRIMARY KEY,
		last_cursor TEXT,
		last_synced_at DATETIME,
		total_count INTEGER DEFAULT 0
	)`+"`"+`); err != nil {
		t.Fatalf("create sync_state: %v", err)
	}
	if _, err := raw.Exec(`+"`"+`INSERT INTO resources (id, resource_type, data) VALUES ('1', 'items', '{"id":"1"}')`+"`"+`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := raw.Exec(`+"`"+`PRAGMA user_version = 1`+"`"+`); err != nil {
		t.Fatalf("stamp v1: %v", err)
	}
	raw.Close()

	root := RootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"doctor", "--json"})
	_ = root.Execute()
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("doctor json: %v\nstderr=%s\nstdout=%s", err, errBuf.String(), out.String())
	}
	cache, _ := payload["cache"].(map[string]any)
	if cache == nil {
		t.Fatalf("doctor json missing cache: %s", out.String())
	}
	if pending, _ := cache["migration_pending"].(bool); !pending {
		t.Fatalf("expected migration_pending=true, cache=%v", cache)
	}

	versionAfterDoctor := pragmaUserVersion(t, dbPath)
	if versionAfterDoctor != 1 {
		t.Fatalf("doctor migrated user_version to %d", versionAfterDoctor)
	}

	out.Reset()
	errBuf.Reset()
	root = RootCmd()
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"items", "list", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("items list --help: %v", err)
	}
	if got := pragmaUserVersion(t, dbPath); got != 1 {
		t.Fatalf("read-only help migrated user_version to %d", got)
	}

	s, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("write open: %v", err)
	}
	defer s.Close()
	if got := pragmaUserVersion(t, dbPath); got == 1 {
		t.Fatalf("write open should migrate user_version away from 1")
	}
}

func pragmaUserVersion(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open probe: %v", err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	return v
}

func TestLoadLocalListSkipsEmptyBeforeTake(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	dbPath := defaultDBPath("ro-store-open-pp-cli")
	s, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Upsert("items", "valid", json.RawMessage(`+"`"+`{"id":"valid"}`+"`"+`)); err != nil {
		t.Fatalf("upsert valid: %v", err)
	}
	if err := s.Upsert("items", "empty", json.RawMessage("null")); err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	if _, err := s.DB().Exec(`+"`"+`UPDATE resources SET updated_at = '2020-01-01T00:00:00Z' WHERE id = 'valid'`+"`"+`); err != nil {
		t.Fatalf("age valid: %v", err)
	}
	if _, err := s.DB().Exec(`+"`"+`UPDATE resources SET updated_at = '2026-01-01T00:00:00Z' WHERE id = 'empty'`+"`"+`); err != nil {
		t.Fatalf("age empty: %v", err)
	}
	items, _, _, _, err := loadLocalList(s, "items", "/items", map[string]string{"take": "1"})
	if err != nil {
		t.Fatalf("loadLocalList: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("take 1 returned %d items, want the older valid row", len(items))
	}
	if !bytes.Contains(items[0], []byte("valid")) {
		t.Fatalf("take 1 returned %s, want the valid row", items[0])
	}
}

func TestAutoRefreshHydratesMissingStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	dbPath := defaultDBPath("ro-store-open-pp-cli")
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("store already exists at %s", dbPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	meta := autoRefreshIfStale(ctx, &rootFlags{dataSource: "auto"}, []string{"items"})
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("first-run refresh did not create the store; decision=%s reason=%s", meta.Decision, meta.Reason)
	}
	if !meta.Ran && meta.Reason == "no-store" {
		t.Fatalf("first-run skipped hydration: decision=%s reason=%s", meta.Decision, meta.Reason)
	}
}
`), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^Test(DoctorAndReadCommandsDoNotMigrate|LoadLocalListSkipsEmptyBeforeTake|AutoRefreshHydratesMissingStore)$", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "^Test(OpenAppliesPragmas|OpenReadOnly_RollbackJournalNoTornRead|ListScanStopsEarly|TypedNewestFirstOrder)$", "-count=1")
}
