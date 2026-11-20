package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedAnalyticsReadOnlyContract(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("analyticsro")
	outputDir := filepath.Join(t.TempDir(), "analyticsro-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Analytics: true, MCP: true}
	require.NoError(t, gen.Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "analytics.go")
	require.Contains(t, src, "store.OpenReadOnlyContext(")
	require.NotContains(t, src, "store.OpenWithContext(")
	require.Contains(t, src, "cliutil.IsMCPChildEnv()")
	require.Contains(t, src, `unknown resource type %q; valid: %s`)
	require.Contains(t, src, "local store has not been synced yet. Run '%s' before trusting local results")
	require.Contains(t, src, "analytics --type items")
	require.NotContains(t, src, "--type messages")
	require.NotContains(t, src, "author_id")
	require.NotContains(t, src, "channel_id")
	require.Contains(t, src, "analyticsCompiledResourceTypes")

	runtimeTest := strings.ReplaceAll(analyticsReadOnlyRuntimeTest, "__BAKED_EXAMPLE_ARGS__", generatedAnalyticsExampleArgsLiteral(t, outputDir))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "analytics_readonly_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestAnalytics", "-count=1")
}

func TestGeneratedAnalyticsMissingStoreWithoutSyncHint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("analyticsplain")
	outputDir := filepath.Join(t.TempDir(), "analyticsplain-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Analytics: true, MCP: true}
	require.NoError(t, gen.Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "analytics.go")
	require.Contains(t, src, "local store has not been synced yet. Populate the local store through a custom store-backed command first")
	require.NotContains(t, src, "before trusting local results")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "analytics_missing_store_test.go"), []byte(analyticsMissingStoreRuntimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestAnalyticsAbsentStoreReportsNotSynced$", "-count=1")
}

const analyticsMissingStoreRuntimeTest = `package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyticsAbsentStoreReportsNotSynced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")

	dbPath := filepath.Join(t.TempDir(), "missing-parent", "data.db")
	root := RootCmd()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"analytics", "--db", dbPath})
	err := root.Execute()
	if err == nil {
		t.Fatalf("absent store exited 0; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	msg := err.Error() + "\n" + stderr.String()
	if !strings.Contains(msg, "has not been synced yet") {
		t.Fatalf("absent store message = %q, want not synced", msg)
	}
	if !strings.Contains(msg, "Populate the local store") {
		t.Fatalf("absent store message = %q, want populate guidance", msg)
	}
	if _, statErr := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(statErr) {
		t.Fatalf("analytics created the missing store directory: %v", statErr)
	}
}
`

const analyticsReadOnlyRuntimeTest = `package cli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"analyticsro-pp-cli/internal/cliutil"
	"analyticsro-pp-cli/internal/store"
)

func TestAnalyticsReadOnlyLeavesDeleteModeStoreUnchanged(t *testing.T) {
	dbPath := seedAnalyticsStore(t)
	convertAnalyticsStoreToDelete(t, dbPath)
	before := snapshotAnalyticsDB(t, dbPath)
	if !strings.EqualFold(before.journal, "delete") {
		t.Fatalf("journal_mode = %q, want delete", before.journal)
	}
	readOnlyEnforced := makeAnalyticsDBReadOnly(t, dbPath)
	before.perm = analyticsPerm(t, dbPath)
	issues := analyticsSQLCount(t, dbPath, "issues")
	emptyKind := analyticsSQLCount(t, dbPath, "empty_kind")
	if issues != 2 || emptyKind != 0 {
		t.Fatalf("seed counts issues=%d empty_kind=%d, want 2 and 0", issues, emptyKind)
	}

	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err != nil {
		t.Fatalf("summary: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "Resource Type\tCount") || !strings.Contains(stdout, "issues\t2") {
		t.Fatalf("table summary = %q", stdout)
	}
	if strings.Contains(stdout, "empty_kind") {
		t.Fatalf("summary listed a zero-row sync type: %q", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "issues")
	if err != nil {
		t.Fatalf("known type: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "issues: 2 records") {
		t.Fatalf("known type table = %q", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "issues", "--json")
	if err != nil {
		t.Fatalf("known type json: %v\nstderr=%s", err, stderr)
	}
	counted := decodeBareAnalyticsJSON(t, stdout)
	if counted["resource_type"] != "issues" || counted["count"] != float64(issues) {
		t.Fatalf("count = %#v, want issues/%d", counted, issues)
	}
	if got := analyticsSQLCount(t, dbPath, "issues"); got != issues {
		t.Fatalf("SELECT COUNT(*) = %d, command count = %d", got, issues)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "empty_kind", "--json")
	if err != nil {
		t.Fatalf("synced empty type: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	counted = decodeBareAnalyticsJSON(t, stdout)
	if counted["resource_type"] != "empty_kind" || counted["count"] != float64(0) {
		t.Fatalf("empty type count = %#v, want 0", counted)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "items", "--json")
	if err != nil {
		t.Fatalf("compiled-in type with no rows: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	counted = decodeBareAnalyticsJSON(t, stdout)
	if counted["resource_type"] != "items" || counted["count"] != float64(0) {
		t.Fatalf("unsynced compiled-in count = %#v, want 0", counted)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "issues", "--group-by", "status")
	if err != nil {
		t.Fatalf("group-by: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "status\tCount") || !strings.Contains(stdout, "open\t2") {
		t.Fatalf("group-by table = %q", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "nope")
	if err == nil {
		t.Fatalf("unknown type exited 0; stdout=%q stderr=%q", stdout, stderr)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("unknown type exit = %d, want 2; err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "unknown resource type \"nope\"") || !strings.Contains(err.Error(), "empty_kind") || !strings.Contains(err.Error(), "issues") || !strings.Contains(err.Error(), "items") {
		t.Fatalf("unknown type error = %q, want store and compiled-in types", err.Error())
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "nope", "--group-by", "status")
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("unknown group-by exit err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(err.Error(), "group-by field") {
		t.Fatalf("unknown type fell through to group-by: %v", err)
	}

	t.Setenv(cliutil.MCPChildEnvVar, "1")
	stdout, stderr, err = runAnalyticsCmd(t, dbPath)
	if err != nil {
		t.Fatalf("mcp summary: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	summary := decodeAnalyticsObject(t, stdout)
	if summary["issues"] != float64(2) {
		t.Fatalf("mcp summary = %#v, want issues 2", summary)
	}
	if strings.Contains(stdout, "Resource Type") {
		t.Fatalf("mcp summary rendered a table: %q", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "issues")
	if err != nil {
		t.Fatalf("mcp type: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	typed := decodeAnalyticsObject(t, stdout)
	if typed["resource_type"] != "issues" || typed["count"] != float64(2) {
		t.Fatalf("mcp type = %#v", typed)
	}
	if strings.Contains(stdout, "records") {
		t.Fatalf("mcp type rendered text: %q", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "issues", "--json")
	if err != nil {
		t.Fatalf("mcp explicit json: %v\nstderr=%s", err, stderr)
	}
	explicit := decodeBareAnalyticsJSON(t, stdout)
	if explicit["count"] != float64(2) {
		t.Fatalf("mcp explicit json count = %#v", explicit)
	}
	if _, ok := explicit["meta"]; ok {
		t.Fatalf("explicit --json must stay bare, got %s", stdout)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--json=false")
	if err != nil {
		t.Fatalf("mcp json=false: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Resource Type\tCount") {
		t.Fatalf("explicit --json=false = %q, want table", stdout)
	}
	t.Setenv(cliutil.MCPChildEnvVar, "")

	stdout, stderr, err = runAnalyticsCmd(t, dbPath)
	if err != nil {
		t.Fatalf("table after mcp: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Resource Type\tCount") || strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("non-mcp output = %q, want table", stdout)
	}

	after := snapshotAnalyticsDB(t, dbPath)
	if before.sum != after.sum {
		t.Fatalf("data.db bytes changed")
	}
	if before.ver != after.ver {
		t.Fatalf("schema version %d -> %d", before.ver, after.ver)
	}
	if !strings.EqualFold(after.journal, before.journal) {
		t.Fatalf("journal_mode %q -> %q", before.journal, after.journal)
	}
	if readOnlyEnforced && before.perm != after.perm {
		t.Fatalf("mode %o -> %o", before.perm, after.perm)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("analytics created %s: %v", suffix, err)
		}
	}
}

func TestAnalyticsAbsentStoreReportsNotSynced(t *testing.T) {
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "missing-parent", "data.db")
	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err == nil {
		t.Fatalf("absent store exited 0; stdout=%q stderr=%q", stdout, stderr)
	}
	msg := err.Error() + "\n" + stderr
	if !strings.Contains(msg, "has not been synced yet") || !strings.Contains(msg, "analyticsro-pp-cli sync") {
		t.Fatalf("absent store message = %q", msg)
	}
	if _, statErr := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(statErr) {
		t.Fatalf("analytics created the missing store directory: %v", statErr)
	}
}

func TestAnalyticsEmptyStoreStillHintsNotSynced(t *testing.T) {
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := snapshotAnalyticsDB(t, dbPath)
	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err != nil {
		t.Fatalf("empty store: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if !strings.Contains(stderr, "has not been synced yet") {
		t.Fatalf("stderr = %q, want not synced hint", stderr)
	}
	if !strings.Contains(stdout, "Resource Type\tCount") {
		t.Fatalf("empty summary = %q", stdout)
	}
	after := snapshotAnalyticsDB(t, dbPath)
	if before.sum != after.sum || !strings.EqualFold(before.journal, after.journal) {
		t.Fatalf("empty store changed bytes or journal")
	}

	examples := __BAKED_EXAMPLE_ARGS__
	if len(examples) == 0 {
		t.Fatalf("analytics help has no examples")
	}
	sawItems := false
	for _, args := range examples {
		for i := 0; i < len(args); i++ {
			if args[i] == "--type" && i+1 < len(args) && args[i+1] == "messages" {
				t.Fatalf("example still uses placeholder type: %v", args)
			}
			if args[i] == "--group-by" {
				t.Fatalf("items spec has no response fields; example = %v", args)
			}
			if args[i] == "--type" && i+1 < len(args) && args[i+1] == "items" {
				sawItems = true
			}
		}
		stdout, stderr, err = runAnalyticsCmd(t, dbPath, args...)
		if err != nil {
			t.Fatalf("empty example %v: %v\nstderr=%s\nstdout=%s", args, err, stderr, stdout)
		}
	}
	if !sawItems {
		t.Fatalf("examples = %v, want --type items", examples)
	}
	after = snapshotAnalyticsDB(t, dbPath)
	if before.sum != after.sum || !strings.EqualFold(before.journal, after.journal) {
		t.Fatalf("examples changed the empty store")
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "items", "--json")
	if err != nil {
		t.Fatalf("compiled-in type on empty store: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	counted := decodeBareAnalyticsJSON(t, stdout)
	if counted["resource_type"] != "items" || counted["count"] != float64(0) {
		t.Fatalf("empty compiled-in count = %#v, want items/0", counted)
	}
	if !strings.Contains(stderr, "has not been synced yet") {
		t.Fatalf("empty compiled-in stderr = %q, want not synced hint", stderr)
	}

	_, _, err = runAnalyticsCmd(t, dbPath, "--type", "messages")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown resource type \"messages\"") || !strings.Contains(err.Error(), "items") {
		t.Fatalf("placeholder type on empty store = %v", err)
	}
	_, _, err = runAnalyticsCmd(t, dbPath, "--type", "ITEMS")
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("case-mismatched type = %v", err)
	}
	_, _, err = runAnalyticsCmd(t, dbPath, "--type", "item")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown resource type \"item\"") {
		t.Fatalf("prefix type = %v", err)
	}

	_, _, err = runAnalyticsCmd(t, dbPath, "--type", "issues")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "valid: items") {
		t.Fatalf("unknown type on empty store = %v", err)
	}
	if strings.Contains(err.Error(), "valid: none") {
		t.Fatalf("empty store hid compiled-in types: %v", err)
	}

	seedAnalyticsItemsRow(t, dbPath)
	sqlCount := analyticsSQLCount(t, dbPath, "items")
	if sqlCount != 1 {
		t.Fatalf("seeded items count = %d, want 1", sqlCount)
	}
	for _, args := range examples {
		stdout, stderr, err = runAnalyticsCmd(t, dbPath, args...)
		if err != nil {
			t.Fatalf("seeded example %v: %v\nstderr=%s\nstdout=%s", args, err, stderr, stdout)
		}
	}
	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "items", "--json")
	if err != nil {
		t.Fatalf("seeded count: %v\nstderr=%s", err, stderr)
	}
	counted = decodeBareAnalyticsJSON(t, stdout)
	if counted["count"] != float64(sqlCount) {
		t.Fatalf("command count = %#v, SELECT COUNT(*) = %d", counted, sqlCount)
	}
}

func TestAnalyticsUnrelatedSQLiteFileIsNotMigrated(t *testing.T) {
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE resources (resource_type TEXT, id TEXT, data TEXT)"); err != nil {
		t.Fatalf("create partial resources: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO resources(resource_type, id, data) VALUES ('issues', '1', '{\"id\":\"1\",\"status\":\"open\"}')"); err != nil {
		t.Fatalf("seed partial: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}
	before := snapshotAnalyticsDB(t, dbPath)

	stdout, stderr, err := runAnalyticsCmd(t, dbPath, "--type", "issues", "--json")
	if err != nil {
		t.Fatalf("partial schema count: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	counted := decodeBareAnalyticsJSON(t, stdout)
	if counted["count"] != float64(1) {
		t.Fatalf("partial count = %#v, want 1", counted)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "missing")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "issues") {
		t.Fatalf("partial unknown type = %v stdout=%q stderr=%q", err, stdout, stderr)
	}

	after := snapshotAnalyticsDB(t, dbPath)
	if before.sum != after.sum {
		t.Fatalf("partial schema database was modified")
	}
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ro.Close()
	var tables int
	if err := ro.DB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'sync_state'").Scan(&tables); err != nil {
		t.Fatalf("schema probe: %v", err)
	}
	if tables != 0 {
		t.Fatalf("analytics created sync_state on a partial database")
	}
}

func TestAnalyticsMissingResourcesTableReportsListError(t *testing.T) {
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE other (id INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	before := snapshotAnalyticsDB(t, dbPath)
	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err == nil {
		t.Fatalf("summary on a store without resources: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "getting status") {
		t.Fatalf("summary error = %v, want getting status", err)
	}
	if strings.Contains(err.Error(), "has not been synced yet") {
		t.Fatalf("summary schema error reported not synced: %v", err)
	}

	stdout, stderr, err = runAnalyticsCmd(t, dbPath, "--type", "items")
	if err == nil {
		t.Fatalf("typed read on a store without resources: stdout=%q stderr=%q", stdout, stderr)
	}
	if ExitCode(err) == 2 {
		t.Fatalf("schema error must not be a usage error: %v", err)
	}
	if !strings.Contains(err.Error(), "listing resource types") {
		t.Fatalf("typed error = %v, want listing resource types", err)
	}
	if strings.Contains(err.Error(), "has not been synced yet") {
		t.Fatalf("typed schema error reported not synced: %v", err)
	}
	after := snapshotAnalyticsDB(t, dbPath)
	if before.sum != after.sum || before.journal != after.journal || before.ver != after.ver {
		t.Fatalf("schema-error read changed store: before=%+v after=%+v", before, after)
	}
}

func TestAnalyticsStatErrorIsNotNotSynced(t *testing.T) {
	isolateAnalyticsHome(t)
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}
	dbPath := filepath.Join(parent, "data.db")
	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err == nil {
		t.Fatalf("stat error exited 0; stdout=%q", stdout)
	}
	if strings.Contains(err.Error(), "has not been synced yet") {
		t.Fatalf("stat error reported not synced: %v", err)
	}
	if !strings.Contains(err.Error(), "opening local database") {
		t.Fatalf("stat error = %v, want open failure\nstderr=%s", err, stderr)
	}
	got, readErr := os.ReadFile(parent)
	if readErr != nil {
		t.Fatalf("reread parent: %v", readErr)
	}
	if string(got) != "x" {
		t.Fatalf("parent file changed: %q", got)
	}
}

func TestAnalyticsCorruptFileDoesNotReportNotSynced(t *testing.T) {
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	payload := []byte("this is not a database")
	if err := os.WriteFile(dbPath, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	stdout, stderr, err := runAnalyticsCmd(t, dbPath)
	if err == nil {
		t.Fatalf("corrupt file exited 0; stdout=%q", stdout)
	}
	if strings.Contains(err.Error(), "has not been synced yet") {
		t.Fatalf("corrupt file reported not synced: %v", err)
	}
	if !strings.Contains(err.Error(), "opening local database") {
		t.Fatalf("corrupt file error = %v, want open failure\nstderr=%s", err, stderr)
	}
	got, readErr := os.ReadFile(dbPath)
	if readErr != nil {
		t.Fatalf("reread: %v", readErr)
	}
	if string(got) != string(payload) {
		t.Fatalf("corrupt file bytes changed")
	}
}

func seedAnalyticsStore(t *testing.T) string {
	t.Helper()
	isolateAnalyticsHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	for _, raw := range []string{
		"{\"id\":\"issue-1\",\"status\":\"open\"}",
		"{\"id\":\"issue-2\",\"status\":\"open\"}",
	} {
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatalf("seed json: %v", err)
		}
		id, _ := obj["id"].(string)
		if err := db.Upsert("issues", id, json.RawMessage(raw)); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	if _, err := db.DB().Exec("INSERT INTO sync_state(resource_type, last_synced_at, total_count) VALUES (?, datetime('now'), ?)", "empty_kind", 0); err != nil {
		t.Fatalf("seed sync_state: %v", err)
	}
	return dbPath
}

func convertAnalyticsStoreToDelete(t *testing.T, dbPath string) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = raw.Close()
		t.Fatalf("checkpoint: %v", err)
	}
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		_ = raw.Close()
		t.Fatalf("journal_mode=DELETE: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}
	if !strings.EqualFold(mode, "delete") {
		t.Fatalf("journal_mode = %q, want delete", mode)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", suffix, err)
		}
	}
}

type analyticsDBSnap struct {
	sum     [32]byte
	perm    os.FileMode
	journal string
	ver     int
}

func snapshotAnalyticsDB(t *testing.T, dbPath string) analyticsDBSnap {
	t.Helper()
	journal, ver := analyticsPragmas(t, dbPath)
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	return analyticsDBSnap{
		sum:     sha256.Sum256(data),
		perm:    analyticsPerm(t, dbPath),
		journal: journal,
		ver:     ver,
	}
}

func analyticsPragmas(t *testing.T, dbPath string) (string, int) {
	t.Helper()
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer ro.Close()
	var journal string
	if err := ro.DB().QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	var ver int
	if err := ro.DB().QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	return journal, ver
}

func analyticsSQLCount(t *testing.T, dbPath, resourceType string) int {
	t.Helper()
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer ro.Close()
	var n int
	if err := ro.DB().QueryRow("SELECT COUNT(*) FROM resources WHERE resource_type = ?", resourceType).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", resourceType, err)
	}
	return n
}

func makeAnalyticsDBReadOnly(t *testing.T, dbPath string) bool {
	t.Helper()
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm() &^ 0o222
	if perm == 0 {
		perm = 0o400
	}
	if err := os.Chmod(dbPath, perm); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return analyticsPerm(t, dbPath)&0o222 == 0
}

func analyticsPerm(t *testing.T, dbPath string) os.FileMode {
	t.Helper()
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Mode().Perm()
}

func runAnalyticsCmd(t *testing.T, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	root := RootCmd()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	full := append([]string{"analytics"}, args...)
	full = append(full, "--db", dbPath)
	root.SetArgs(full)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func decodeBareAnalyticsJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return payload
}

func decodeAnalyticsObject(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if _, ok := payload["meta"]; !ok {
		t.Fatalf("mcp output missing meta: %s", stdout)
	}
	results, ok := payload["results"].(map[string]any)
	if !ok {
		t.Fatalf("mcp results = %#v\n%s", payload["results"], stdout)
	}
	return results
}

func seedAnalyticsItemsRow(t *testing.T, dbPath string) {
	t.Helper()
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	if err := db.Upsert("items", "item-1", json.RawMessage("{\"id\":\"item-1\"}")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

func isolateAnalyticsHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv(cliutil.MCPChildEnvVar, "")
}
`
