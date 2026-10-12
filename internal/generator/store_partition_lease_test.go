package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncOwnedPartitionNames(t *testing.T) {
	t.Parallel()

	names := syncOwnedPartitionNames(
		[]profiler.SyncableResource{
			{Name: "resource-records"},
			{Name: "  "},
			{Name: "resourcerecords"},
			{Name: "resource-records"},
		},
		[]profiler.DependentResource{
			{Name: "audit_logs"},
			{Name: "resource-records"},
			{Name: ""},
		},
	)
	assert.Equal(t, []string{"audit_logs", "resource-records", "resourcerecords"}, names)
	assert.Empty(t, syncOwnedPartitionNames(nil, nil))
}

func TestGeneratedKebabPartitionAndLinuxLease(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("kebab-store")
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"resource-records": {
			Description: "Resource records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/resource-records",
					Description: "List resource records",
					Response:    spec.ResponseDef{Type: "array"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources()")
	assert.Contains(t, defaults, `"resource-records"`)
	assert.NotContains(t, defaults, `"resource_records"`)
	known := generatedFunctionBody(t, syncSrc, "func knownSyncResourceNames()")
	assert.Contains(t, known, `"resource-records"`)
	assert.Contains(t, syncSrc, "upsertResourceBatch(db, resource, items)")

	dataSource := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	assert.Contains(t, dataSource, `"resource-records": {},`)
	writeBody := generatedFunctionBody(t, dataSource, "func localWriteResourceType(")
	requested := strings.Index(writeBody, "stored == resourceType")
	lowered := strings.Index(writeBody, "strings.ToLower(resourceType)")
	require.NotEqual(t, -1, requested, "write path must keep a partition that already uses the requested key")
	require.Greater(t, lowered, requested, "legacy rename must not run before the requested-key guard")

	linuxSrc := readGeneratedFile(t, outputDir, "internal", "cli", "store_adopt_linux.go")
	assert.Contains(t, linuxSrc, "storeWriteLeaseHook")
	assert.Contains(t, linuxSrc, "A disabled lease and a busy file return the same error")
	assert.Contains(t, linuxSrc, "until a lease succeeds")
	assert.NotContains(t, linuxSrc, "fall back to a descriptor scan")

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "kebab_partition_test.go"),
		[]byte(strings.ReplaceAll(kebabPartitionTestSource, "__MODULE_PATH__", modulePath)),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "store_adopt_lease_test.go"),
		[]byte(storeAdoptLeaseTestSource),
		0o644,
	))

	run := "TestKebabSyncWriteThroughSecondSyncKeepsRows|TestKebabEmptyWriteThroughUsesSyncPartition|TestLegacyLowercaseCamelCaseStillMigrates"
	if runtime.GOOS == "linux" {
		run += "|TestAdoptDefersWhenWriteLeaseRefused|TestAdoptProceedsWhenWriteLeaseSucceeds"
	}
	output, err := runGoCommandOutput(t, outputDir, "test", "./internal/cli", "-run", run, "-count=1", "-v")
	require.NoError(t, err, output)
	require.Contains(t, output, "--- PASS: TestKebabSyncWriteThroughSecondSyncKeepsRows")
	require.Contains(t, output, "--- PASS: TestKebabEmptyWriteThroughUsesSyncPartition")
	require.Contains(t, output, "--- PASS: TestLegacyLowercaseCamelCaseStillMigrates")
	if runtime.GOOS == "linux" {
		require.Contains(t, output, "--- PASS: TestAdoptDefersWhenWriteLeaseRefused")
		if !strings.Contains(output, "--- PASS: TestAdoptProceedsWhenWriteLeaseSucceeds") {
			require.Contains(t, output, "--- SKIP: TestAdoptProceedsWhenWriteLeaseSucceeds")
		}
	}
	requireGeneratedCompiles(t, outputDir)
}

const kebabPartitionTestSource = `package cli

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"__MODULE_PATH__/internal/store"
)

func setKebabStoreHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

func openKebabStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.OpenWithContext(context.Background(), defaultDBPath("kebab-store-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return db
}

func assertKebabPartitionCounts(t *testing.T, db *store.Store, kebab, snake int) {
	t.Helper()
	gotKebab, err := db.Count("resource-records")
	if err != nil {
		t.Fatalf("count kebab: %v", err)
	}
	gotSnake, err := db.Count("resource_records")
	if err != nil {
		t.Fatalf("count snake: %v", err)
	}
	if gotKebab != kebab || gotSnake != snake {
		t.Fatalf("counts kebab=%d snake=%d, want %d and %d", gotKebab, gotSnake, kebab, snake)
	}
}

func TestKebabSyncWriteThroughSecondSyncKeepsRows(t *testing.T) {
	setKebabStoreHome(t)
	ctx := context.Background()

	db := openKebabStore(t)
	if _, _, _, err := upsertResourceBatch(db, "resource-records", []json.RawMessage{
		json.RawMessage("{\"id\":\"r1\",\"name\":\"synced\"}"),
	}); err != nil {
		t.Fatalf("sync upsert: %v", err)
	}
	if got := localWriteResourceType(db, "resource-records"); got != "resource-records" {
		t.Fatalf("write partition = %q", got)
	}
	assertKebabPartitionCounts(t, db, 1, 0)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	writeThroughCache(ctx, "resource-records", json.RawMessage("[{\"id\":\"r2\",\"name\":\"live\"}]"))

	db = openKebabStore(t)
	if got := localWriteResourceType(db, "resource-records"); got != "resource-records" {
		t.Fatalf("write partition after live read = %q", got)
	}
	assertKebabPartitionCounts(t, db, 2, 0)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db = openKebabStore(t)
	if _, _, _, err := upsertResourceBatch(db, "resource-records", []json.RawMessage{
		json.RawMessage("{\"id\":\"r3\",\"name\":\"resynced\"}"),
	}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	assertKebabPartitionCounts(t, db, 3, 0)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, _, err := resolveLocal(ctx, nil, io.Discard, "resource-records", true, "/resource-records", nil, "test")
	if err != nil {
		t.Fatalf("offline list: %v", err)
	}
	got := string(data)
	for _, name := range []string{"synced", "live", "resynced"} {
		if !strings.Contains(got, name) {
			t.Fatalf("offline list missing %s: %s", name, got)
		}
	}
}

func TestKebabEmptyWriteThroughUsesSyncPartition(t *testing.T) {
	setKebabStoreHome(t)
	ctx := context.Background()

	db := openKebabStore(t)
	if got := localWriteResourceType(db, "resource-records"); got != "resource-records" {
		t.Fatalf("empty write partition = %q", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	writeThroughCache(ctx, "resource-records", json.RawMessage("[{\"id\":\"r1\",\"name\":\"first\"}]"))

	db = openKebabStore(t)
	defer db.Close()
	if got := localWriteResourceType(db, "resource-records"); got != "resource-records" {
		t.Fatalf("write partition after empty write-through = %q", got)
	}
	assertKebabPartitionCounts(t, db, 1, 0)
}

func TestLegacyLowercaseCamelCaseStillMigrates(t *testing.T) {
	setKebabStoreHome(t)

	db := openKebabStore(t)
	defer db.Close()
	if _, _, _, err := upsertResourceBatch(db, "resourcerecords", []json.RawMessage{
		json.RawMessage("{\"id\":\"r1\",\"name\":\"legacy\"}"),
	}); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	if got := localWriteResourceType(db, "resourceRecords"); got != "resource_records" {
		t.Fatalf("migrated partition = %q", got)
	}
	legacy, err := db.Count("resourcerecords")
	if err != nil {
		t.Fatalf("count legacy: %v", err)
	}
	canonical, err := db.Count("resource_records")
	if err != nil {
		t.Fatalf("count canonical: %v", err)
	}
	if legacy != 0 || canonical != 1 {
		t.Fatalf("after migrate legacy=%d canonical=%d, want 0 and 1", legacy, canonical)
	}
	row, err := db.Get("resource_records", "r1")
	if err != nil {
		t.Fatalf("moved row: %v", err)
	}
	if !strings.Contains(string(row), "legacy") {
		t.Fatalf("moved row = %s", row)
	}
}
`

const storeAdoptLeaseTestSource = `//go:build linux

package cli

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	_ "modernc.org/sqlite"
)

func seedClosedSQLite(t *testing.T, path, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE rows (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO rows (v) VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func insertSQLiteValue(t *testing.T, path, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO rows (v) VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
}

func countSQLiteRows(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM rows").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdoptDefersWhenWriteLeaseRefused(t *testing.T) {
	dir := t.TempDir()
	stable := filepath.Join(dir, "data-stable.db")
	legacy := filepath.Join(dir, "data-legacy.db")
	seedClosedSQLite(t, legacy, "one")

	setLegacyDBClaimSuppressed(false)
	prev := storeWriteLeaseHook
	t.Cleanup(func() { storeWriteLeaseHook = prev })
	var calls int
	storeWriteLeaseHook = func(uintptr) error {
		calls++
		return unix.EAGAIN
	}

	got, ok := adoptLegacyScopedStore(stable, legacy)
	if calls == 0 {
		t.Fatal("F_SETLEASE was not attempted")
	}
	if !ok || got != legacy {
		t.Fatalf("adopt = %s, %v; want the legacy path", got, ok)
	}
	if _, err := os.Stat(stable); !os.IsNotExist(err) {
		t.Fatalf("stable database created without a lease: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy store missing: %v", err)
	}
	insertSQLiteValue(t, legacy, "two")
	if n := countSQLiteRows(t, legacy); n != 2 {
		t.Fatalf("legacy rows = %d, want 2", n)
	}
}

func writeLeaseSupported(t *testing.T, dir string) bool {
	t.Helper()
	path := filepath.Join(dir, "lease-probe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_WRLCK); err != nil {
		return false
	}
	_, _ = unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
	return true
}

func TestAdoptProceedsWhenWriteLeaseSucceeds(t *testing.T) {
	dir := t.TempDir()
	if !writeLeaseSupported(t, dir) {
		t.Skip("F_SETLEASE is refused on this filesystem")
	}
	stable := filepath.Join(dir, "data-stable.db")
	legacy := filepath.Join(dir, "data-legacy.db")
	seedClosedSQLite(t, legacy, "one")

	setLegacyDBClaimSuppressed(false)
	prev := storeWriteLeaseHook
	t.Cleanup(func() { storeWriteLeaseHook = prev })
	var calls int
	storeWriteLeaseHook = func(fd uintptr) error {
		calls++
		_, err := unix.FcntlInt(fd, unix.F_SETLEASE, unix.F_WRLCK)
		return err
	}

	got, ok := adoptLegacyScopedStore(stable, legacy)
	if calls == 0 {
		t.Fatal("F_SETLEASE was not attempted")
	}
	if !ok || got != stable {
		t.Fatalf("adopt = %s, %v; want the stable path", got, ok)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy name still present: %v", err)
	}
	if n := countSQLiteRows(t, stable); n != 1 {
		t.Fatalf("stable rows = %d, want 1", n)
	}
}
`
