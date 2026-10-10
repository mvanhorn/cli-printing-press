package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDependentSyncIncompleteIsError(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("dep-sync-fail")
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
					Response:   spec.ResponseDef{Type: "array", Item: "Module"},
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
		"Module": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	// The server imports internal/mcp. Without MCP that directory has no
	// non-test files, and the full-module build cannot succeed.
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "dependent_sync_failure_test.go"), []byte(dependentSyncFailureTestSource), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestDependentSync", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

const dependentSyncFailureTestSource = `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dep-sync-fail-pp-cli/internal/store"
)

type failingDependentGetter struct {
	dryRun  bool
	respond func(ctx context.Context, path string, params map[string]string) (json.RawMessage, error)
}

func (f *failingDependentGetter) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if f.respond != nil {
		return f.respond(ctx, path, params)
	}
	return json.RawMessage("[]"), nil
}

func (f *failingDependentGetter) RateLimit() float64 { return 0 }
func (f *failingDependentGetter) IsDryRun() bool     { return f.dryRun }

func failureModulesDep() dependentResourceDef {
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

func seedFailureProjects(t *testing.T, n int) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	items := make([]json.RawMessage, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, json.RawMessage(fmt.Sprintf("{\"id\":\"p-%d\",\"workspace\":\"ws-test\"}", i)))
	}
	if _, _, err := db.UpsertBatch("projects", items); err != nil {
		t.Fatalf("UpsertBatch projects: %v", err)
	}
	return db
}

func seedModuleWatermark(t *testing.T, db *store.Store) string {
	t.Helper()
	seeded := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.SaveSyncStateAt("modules", "cursor-1", 4, seeded); err != nil {
		t.Fatal(err)
	}
	return seeded.Format(time.RFC3339)
}

func assertModuleWatermark(t *testing.T, db *store.Store, wantSynced string, wantComplete int) {
	t.Helper()
	if got := db.GetLastSyncedAt("modules"); got != wantSynced {
		t.Fatalf("last_synced_at = %q, want %q", got, wantSynced)
	}
	var complete int
	if err := db.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type = ?", "modules").Scan(&complete); err != nil {
		t.Fatal(err)
	}
	if complete != wantComplete {
		t.Fatalf("last_attempt_complete = %d, want %d", complete, wantComplete)
	}
}

func TestDependentSyncAllParentsFailedKeepsStoredRowsAsError(t *testing.T) {
	db := seedFailureProjects(t, 1)
	wantSynced := seedModuleWatermark(t, db)
	fake := &failingDependentGetter{
		respond: func(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
			if params["after"] == "p2" {
				return nil, errors.New("page 2 failed")
			}
			return json.RawMessage("{\"data\":[{\"id\":\"m-1\"}],\"next_cursor\":\"p2\"}"), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, failureModulesDep(), "", false, 0, false, false, nil, &events, 1)
	if res.Err == nil {
		t.Fatal("expected Err when every parent failed after storing rows")
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want nil", res.Warn)
	}
	if res.Count != 1 {
		t.Fatalf("Count = %d, want 1", res.Count)
	}
	if strings.Contains(events.String(), "sync_complete") {
		t.Fatalf("events included sync_complete: %s", events.String())
	}
	assertModuleWatermark(t, db, wantSynced, 0)
}

func TestDependentSyncPartialParentFailureStaysWarning(t *testing.T) {
	db := seedFailureProjects(t, 2)
	wantSynced := seedModuleWatermark(t, db)
	fake := &failingDependentGetter{
		respond: func(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
			if strings.Contains(path, "p-1") {
				return nil, errors.New("parent failed")
			}
			return json.RawMessage("[{\"id\":\"m-ok\"}]"), nil
		},
	}
	res := syncDependentResource(context.Background(), fake, db, failureModulesDep(), "", false, 0, false, false, nil, nil, 1)
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if res.Warn == nil {
		t.Fatal("expected Warn when only some parents fail")
	}
	if res.Count != 1 {
		t.Fatalf("Count = %d, want 1", res.Count)
	}
	assertModuleWatermark(t, db, wantSynced, 0)
}

func TestDependentSyncCancelBeforeRemainingParentsIsError(t *testing.T) {
	db := seedFailureProjects(t, 2)
	wantSynced := seedModuleWatermark(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &failingDependentGetter{
		respond: func(_ context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
			cancel()
			return json.RawMessage("[{\"id\":\"m-1\"}]"), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(ctx, fake, db, failureModulesDep(), "", false, 0, false, false, nil, &events, 1)
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want nil", res.Warn)
	}
	if strings.Contains(events.String(), "sync_dryrun") || strings.Contains(events.String(), "sync_complete") {
		t.Fatalf("cancelled run reported success: %s", events.String())
	}
	assertModuleWatermark(t, db, wantSynced, 0)
}

func TestDependentSyncCancelledDryRunIsError(t *testing.T) {
	db := seedFailureProjects(t, 2)
	wantSynced := seedModuleWatermark(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &failingDependentGetter{
		dryRun: true,
		respond: func(_ context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
			cancel()
			return json.RawMessage("{\"dry_run\":true}"), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(ctx, fake, db, failureModulesDep(), "", false, 0, false, false, nil, &events, 1)
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", res.Err)
	}
	if strings.Contains(events.String(), "sync_dryrun") || strings.Contains(events.String(), "sync_complete") {
		t.Fatalf("cancelled dry-run reported success: %s", events.String())
	}
	assertModuleWatermark(t, db, wantSynced, 1)
}

func TestDependentSyncInFlightCancelIsError(t *testing.T) {
	db := seedFailureProjects(t, 2)
	wantSynced := seedModuleWatermark(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	var mu sync.Mutex
	started := 0
	fake := &failingDependentGetter{
		respond: func(callCtx context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
			mu.Lock()
			started++
			mu.Unlock()
			entered <- struct{}{}
			<-release
			if err := callCtx.Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		},
	}
	go func() {
		defer releaseAll()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for i := 0; i < 2; i++ {
			select {
			case <-entered:
			case <-timer.C:
				cancel()
				return
			}
		}
		cancel()
	}()
	var events bytes.Buffer
	res := syncDependentResource(ctx, fake, db, failureModulesDep(), "", false, 0, false, false, nil, &events, 2)
	mu.Lock()
	gotStarted := started
	mu.Unlock()
	if gotStarted != 2 {
		t.Fatalf("parents that reached Get = %d, want 2", gotStarted)
	}
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want nil", res.Warn)
	}
	if strings.Contains(events.String(), "sync_dryrun") || strings.Contains(events.String(), "sync_complete") {
		t.Fatalf("in-flight cancel reported success: %s", events.String())
	}
	assertModuleWatermark(t, db, wantSynced, 0)
}

func TestDependentSyncDeadlineIsError(t *testing.T) {
	db := seedFailureProjects(t, 2)
	wantSynced := seedModuleWatermark(t, db)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	fetched := false
	fake := &failingDependentGetter{
		respond: func(context.Context, string, map[string]string) (json.RawMessage, error) {
			fetched = true
			return nil, errors.New("unexpected fetch")
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(ctx, fake, db, failureModulesDep(), "", false, 0, false, false, nil, &events, 1)
	if fetched {
		t.Fatal("deadline already exceeded before fetch")
	}
	if !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("Err = %v, want context.DeadlineExceeded", res.Err)
	}
	if strings.Contains(events.String(), "sync_dryrun") || strings.Contains(events.String(), "sync_complete") {
		t.Fatalf("deadline reported success: %s", events.String())
	}
	assertModuleWatermark(t, db, wantSynced, 0)
}
`
