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

// TestDependentFanoutWorkerPoolNoRace is a generated-run test that proves the
// parallel per-parent worker pool emitted by sync.go.tmpl is race-free under
// the Go race detector. It generates a CLI with a typed "projects" parent and a
// paginated "modules" dependent (which makes the template emit
// syncDependentResource + syncOneParent + the bounded worker pool + lockedWriter),
// injects an in-package concurrency test into the generated internal/cli dir, and
// runs `go test -race` there. Mirrors the build-tree
// internal/cli/sync_concurrency_test.go so a regression in the ported pool
// (dropped parent, double-dial, mis-aggregated counts, or a data race on the
// shared store / event writer) fails CI on the generated artifact — not just on
// the hand-maintained build tree.
//
// -race on Windows needs CGO_ENABLED=1 + a C compiler on PATH (WinLibs gcc); it
// runs green in the Linux generated-test CI lane where the rest of the -race
// suite (cost_throttling, store_playbooks) already lives.
func TestDependentFanoutWorkerPoolNoRace(t *testing.T) {
	t.Parallel()

	// Same shape as TestDependentFanoutScoping: a typed "projects" parent with a
	// paginated single-PK "modules" dependent forces the fan-out code path.
	apiSpec := minimalSpec("dep-fanout-race")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:            "GET",
					Path:              "/projects",
					Response:          spec.ResponseDef{Type: "array", Item: "Project"},
					Pagination:        &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:           "id",
					TenantScopeColumn: "workspace",
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
				{Name: "workspace", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	// Guard the template wiring so the pool cannot silently regress to serial:
	// the bounded pool + its locked event writer must be present in the output.
	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)
	require.Contains(t, src, "func syncOneParent(", "sync.go must emit the per-parent worker entry")
	require.Contains(t, src, "lockedWriter", "sync.go must emit the mutex-guarded event writer for concurrent workers")
	require.Equal(t, 4, strings.Count(src, "totalSynced += res.Count"), "warning and success results must both contribute stored rows")

	// In-package test (package cli) for access to unexported syncDependentResource
	// / dependentResourceDef. This minimal spec has no membership scope, no
	// post-sync, no HTML sync and no date-range param, so syncDependentResource's
	// signature is the 12-arg form: (ctx, c, db, dep, sinceTS, full, maxPages,
	// latestOnly, prune, userParams, syncEvents, concurrency).
	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

// fakeDependentGetter satisfies the interface syncDependentResource consumes:
//
//	interface{ Get(context.Context, string, map[string]string) (json.RawMessage, error); RateLimit() float64 }
//
// onGet records each requested path under a mutex so a parallel fan-out can be
// asserted for "each parent fetched exactly once"; respond returns the page body.
type fakeDependentGetter struct {
	mu      sync.Mutex
	onGet   func(path string, params map[string]string)
	respond func(path string, params map[string]string) (json.RawMessage, error)
}

func (f *fakeDependentGetter) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if f.onGet != nil {
		f.mu.Lock()
		f.onGet(path, params)
		f.mu.Unlock()
	}
	if f.respond != nil {
		return f.respond(path, params)
	}
	return json.RawMessage(` + "`" + `[]` + "`" + `), nil
}

func (f *fakeDependentGetter) RateLimit() float64 { return 0 }

// modulesDep: a per_parent dependent keyed on projects.id, single path param.
func modulesDep() dependentResourceDef {
	return dependentResourceDef{
		Name:                 "modules",
		ParentTable:          "projects",
		ParentIDParam:        "projectId",
		PathTemplate:         "/projects/{projectId}/modules",
		ReconcileMode:        "per_parent",
		GenericScopeJSONPath: "$.project",
		PathParams:           []dependentPathParamDef{{Param: "projectId", Field: "id"}},
	}
}

func seedFanoutRaceProjects(t *testing.T, k int) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	items := make([]json.RawMessage, 0, k)
	for i := 0; i < k; i++ {
		items = append(items, json.RawMessage(fmt.Sprintf(` + "`" + `{"id":"p-%d","workspace":"ws-test"}` + "`" + `, i)))
	}
	if _, _, err := s.UpsertBatch("projects", items); err != nil {
		s.Close()
		t.Fatalf("UpsertBatch projects: %v", err)
	}
	return s
}

func sanitizeRacePath(path string) string {
	return strings.NewReplacer("/", "-").Replace(strings.Trim(path, "/"))
}

// TestSyncDependentResource_ConcurrentProcessesEachParentOnce drives the bounded
// worker pool: K parents, one child page each, concurrency=4. It asserts every
// distinct parent path is fetched exactly once (no double-dial, no dropped
// parent) and that per-worker counters aggregate to the correct total after the
// barrier. Meaningful under -race: the shared store, seenPaths map and event
// writer are all touched from every worker goroutine.
func TestSyncDependentResource_ConcurrentProcessesEachParentOnce(t *testing.T) {
	const k = 12
	db := seedFanoutRaceProjects(t, k)
	defer db.Close()

	var mu sync.Mutex
	seenPaths := map[string]int{}
	stateID := 0
	fake := &fakeDependentGetter{
		onGet: func(path string, _ map[string]string) {
			seenPaths[path]++
		},
		respond: func(path string, _ map[string]string) (json.RawMessage, error) {
			mu.Lock()
			stateID++
			id := stateID
			mu.Unlock()
			return json.RawMessage(fmt.Sprintf(` + "`" + `[{"id":"m-%s-%d"}]` + "`" + `, sanitizeRacePath(path), id)), nil
		},
	}

	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesDep(), "", true, 0, false, true, &syncUserParams{}, &events, 4)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(seenPaths) != k {
		t.Errorf("distinct parent paths fetched = %d, want %d", len(seenPaths), k)
	}
	for p, n := range seenPaths {
		if n != 1 {
			t.Errorf("path %s fetched %d times, want exactly 1", p, n)
		}
	}
	if res.Count != k {
		t.Errorf("Count = %d, want %d (aggregated across workers)", res.Count, k)
	}
}

// TestSyncDependentResource_DryRunShortCircuitsUnderConcurrency verifies the
// dry-run sentinel short-circuits the whole resource even under the parallel
// pool: Count 0, exactly one sync_dryrun event, no rows written — regardless of
// which worker drains the sentinel first.
func TestSyncDependentResource_DryRunShortCircuitsUnderConcurrency(t *testing.T) {
	const k = 8
	db := seedFanoutRaceProjects(t, k)
	defer db.Close()

	fake := &fakeDependentGetter{
		respond: func(_ string, _ map[string]string) (json.RawMessage, error) {
			return json.RawMessage(` + "`" + `{"dry_run": true}` + "`" + `), nil
		},
	}

	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesDep(), "", true, 0, false, true, &syncUserParams{}, &events, 4)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Count != 0 {
		t.Errorf("Count = %d, want 0 under dry-run", res.Count)
	}
	if n := strings.Count(events.String(), ` + "`" + `"event":"sync_dryrun"` + "`" + `); n != 1 {
		t.Errorf("sync_dryrun emitted %d times, want exactly 1; events:\n%s", n, events.String())
	}

	var cnt int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = ?` + "`" + `, "modules").Scan(&cnt); err != nil {
		t.Fatalf("count modules: %v", err)
	}
	if cnt != 0 {
		t.Errorf("stored modules rows = %d, want 0 (dry-run must not mutate)", cnt)
	}
}

func installModuleUpsertFailure(t *testing.T, db *store.Store, when string) {
	t.Helper()
	stmt := ` + "`" + `CREATE TRIGGER fail_module_upsert
		BEFORE INSERT ON resources
		WHEN NEW.resource_type = 'modules'` + "`" + `
	if when != "" {
		stmt += " AND " + when
	}
	stmt += ` + "`" + ` BEGIN SELECT RAISE(ABORT, 'forced module upsert failure'); END` + "`" + `
	if _, err := db.DB().Exec(stmt); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
}

type moduleSyncState struct {
	cursor   string
	syncedAt time.Time
	count    int
}

func seedModuleSyncState(t *testing.T, db *store.Store) moduleSyncState {
	t.Helper()
	if err := db.SaveSyncState("modules", "cursor-before", 7); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	return readModuleSyncState(t, db)
}

func readModuleSyncState(t *testing.T, db *store.Store) moduleSyncState {
	t.Helper()
	cursor, syncedAt, count, err := db.GetSyncState("modules")
	if err != nil {
		t.Fatalf("read sync state: %v", err)
	}
	return moduleSyncState{cursor: cursor, syncedAt: syncedAt, count: count}
}

func assertModuleSyncStateUnchanged(t *testing.T, db *store.Store, before moduleSyncState) {
	t.Helper()
	after := readModuleSyncState(t, db)
	if after.cursor != before.cursor || !after.syncedAt.Equal(before.syncedAt) || after.count != before.count {
		t.Fatalf("sync state changed after failed persistence: before=%+v after=%+v", before, after)
	}
}

func TestSyncDependentResource_AllUpsertsFailReturnsError(t *testing.T) {
	db := seedFanoutRaceProjects(t, 2)
	defer db.Close()
	before := seedModuleSyncState(t, db)
	installModuleUpsertFailure(t, db, "")

	fake := &fakeDependentGetter{respond: func(path string, _ map[string]string) (json.RawMessage, error) {
		return json.RawMessage(fmt.Sprintf(` + "`" + `[{"id":"m-%s"}]` + "`" + `, sanitizeRacePath(path))), nil
	}}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesDep(), "", true, 0, false, true, &syncUserParams{}, &events, 2)
	if res.Err == nil {
		t.Fatalf("expected all-parent upsert failure to return Err; result=%+v", res)
	}
	if res.Warn != nil {
		t.Fatalf("all-parent failure returned Warn instead of Err: %v", res.Warn)
	}
	if n := strings.Count(events.String(), ` + "`" + `"event":"sync_error"` + "`" + `); n != 2 {
		t.Fatalf("sync_error events = %d, want 2; events:\n%s", n, events.String())
	}
	assertModuleSyncStateUnchanged(t, db, before)
}

func TestSyncDependentResource_PartialUpsertFailureReturnsWarning(t *testing.T) {
	db := seedFanoutRaceProjects(t, 2)
	defer db.Close()
	before := seedModuleSyncState(t, db)
	installModuleUpsertFailure(t, db, ` + "`" + `json_extract(NEW.data, '$.parent_id') = 'p-0'` + "`" + `)

	fake := &fakeDependentGetter{respond: func(path string, _ map[string]string) (json.RawMessage, error) {
		return json.RawMessage(fmt.Sprintf(` + "`" + `[{"id":"m-%s"}]` + "`" + `, sanitizeRacePath(path))), nil
	}}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesDep(), "", true, 0, false, true, &syncUserParams{}, &events, 2)
	if res.Err != nil {
		t.Fatalf("partial failure returned Err: %v", res.Err)
	}
	if res.Warn == nil {
		t.Fatalf("expected partial upsert failure to return Warn; result=%+v", res)
	}
	if res.Count != 1 {
		t.Fatalf("Count = %d, want one successfully stored parent", res.Count)
	}
	if n := strings.Count(events.String(), ` + "`" + `"event":"sync_error"` + "`" + `); n != 1 {
		t.Fatalf("sync_error events = %d, want 1; events:\n%s", n, events.String())
	}
	assertModuleSyncStateUnchanged(t, db, before)
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "fanout_race_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	// Plain pass first to verify behavior, then a focused -race pass to prove the
	// bounded worker pool has no data race on the shared store / event writer.
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncDependentResource_", "./internal/cli")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncDependentResource_", "-race", "./internal/cli")
}
