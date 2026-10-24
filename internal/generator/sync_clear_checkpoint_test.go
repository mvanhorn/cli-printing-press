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

func TestGeneratedSyncClearDoesNotMarkComplete(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("syncclear")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	// Leave cache off. Turning it on while learn is disabled emits an
	// auto-refresh call to a helper that exists only when learn is enabled.
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"devices": {
			Description: "Devices",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/devices", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"audits": {
			Description: "Audits",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/audits", Response: spec.ResponseDef{Type: "array"}},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	sinceAt := strings.Index(syncSrc, `invalid --since value`)
	clearAt := strings.Index(syncSrc, "ClearSyncScopeIfCurrent")
	stopAt := strings.Index(syncSrc, "cmd.Context().Err()")
	walkAt := strings.Index(syncSrc, "work <- resource")
	require.NotEqual(t, -1, sinceAt)
	require.NotEqual(t, -1, clearAt)
	require.NotEqual(t, -1, stopAt)
	require.NotEqual(t, -1, walkAt)
	require.Less(t, sinceAt, clearAt, "invalid --since must be rejected before any checkpoint clear")
	require.Less(t, strings.LastIndex(syncSrc, "ClearSyncScopeIfCurrent"), stopAt, "cancellation must be observed after the clears and before the walk")
	require.Less(t, stopAt, walkAt)
	require.Contains(t, storeSrc, "UPDATE sync_state SET last_cursor = '', last_synced_at = ?, total_count = 0, last_attempt_complete = 0 WHERE resource_type = ?")
	require.NotContains(t, storeSrc, "last_attempt_complete = 1 WHERE resource_type = ?")

	behaviorTest := strings.ReplaceAll(syncClearCheckpointBehaviorTest, "SYNCCLEAR_BASE_URL", naming.EnvPrefix(apiSpec.Name)+"_BASE_URL")
	behaviorTest = strings.ReplaceAll(behaviorTest, "syncclear-pp-cli", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_clear_checkpoint_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestSyncClearCheckpoint$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

const syncClearCheckpointBehaviorTest = `package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"syncclear-pp-cli/internal/store"
)

func TestSyncClearCheckpoint(t *testing.T) {
	var calls atomic.Int32
	var onRequestMu sync.Mutex
	var onRequest func(*http.Request)
	setOnRequest := func(fn func(*http.Request)) {
		onRequestMu.Lock()
		onRequest = fn
		onRequestMu.Unlock()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		onRequestMu.Lock()
		fn := onRequest
		onRequestMu.Unlock()
		if fn != nil {
			fn(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[{\"id\":\"one\"}]")
	}))
	defer server.Close()
	t.Setenv("SYNCCLEAR_BASE_URL", server.URL)

	stamp := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	const clearedAt = "0001-01-01T00:00:00Z"

	open := func(t *testing.T) (*store.Store, string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "data.db")
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if err := db.SaveSyncProgress("devices", "old-devices", 100); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSyncStateAt("audits", "old-audits", 50, stamp); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSyncStateAt("notes", "keep-notes", 7, stamp); err != nil {
			t.Fatal(err)
		}
		return db, path
	}

	read := func(t *testing.T, db *store.Store, resource string) checkpoint {
		t.Helper()
		var cp checkpoint
		var cursor, synced, scope sql.NullString
		err := db.DB().QueryRow("SELECT IFNULL(last_cursor, ''), IFNULL(last_synced_at, ''), total_count, last_attempt_complete, scope_key FROM sync_state WHERE resource_type = ?", resource).Scan(&cursor, &synced, &cp.count, &cp.complete, &scope)
		if err != nil {
			t.Fatalf("read %s: %v", resource, err)
		}
		cp.cursor, cp.synced, cp.scope = cursor.String, synced.String, scope.String
		return cp
	}

	snapshot := func(t *testing.T, db *store.Store) map[string]checkpoint {
		t.Helper()
		out := map[string]checkpoint{}
		for _, resource := range []string{"devices", "audits", "notes"} {
			out[resource] = read(t, db, resource)
		}
		return out
	}

	same := func(t *testing.T, got, want checkpoint) {
		t.Helper()
		if got != want {
			t.Fatalf("checkpoint = %+v, want %+v", got, want)
		}
	}

	run := func(t *testing.T, dbPath string, ctx context.Context, dryRun bool, args ...string) error {
		t.Helper()
		cmd := newSyncCmd(&rootFlags{
			configPath: filepath.Join(t.TempDir(), "config.toml"),
			timeout:    time.Second,
			dryRun:     dryRun,
		})
		var stdout lockedBuffer
		cmd.SetOut(&stdout)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--db", dbPath}, args...))
		if ctx != nil {
			cmd.SetContext(ctx)
		}
		events, openErr := os.CreateTemp(t.TempDir(), "stderr")
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer events.Close()
		oldStderr, oldHuman := os.Stderr, humanFriendly
		os.Stderr, humanFriendly = events, false
		defer func() { os.Stderr, humanFriendly = oldStderr, oldHuman }()
		err := cmd.Execute()
		output, readErr := os.ReadFile(events.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err != nil {
			return fmtError(err, string(output), stdout.String())
		}
		return nil
	}

	t.Run("full invalid since does not mutate", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		err := run(t, path, nil, false, "--full", "--since", "bogus", "--resources", "devices,audits")
		if err == nil || !strings.Contains(err.Error(), "invalid --since value \"bogus\"") {
			t.Fatalf("error = %v", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	t.Run("latest-only invalid since does not mutate", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		err := run(t, path, nil, false, "--latest-only", "--since", "bogus", "--resources", "devices,audits")
		if err == nil || !strings.Contains(err.Error(), "invalid --since value \"bogus\"") {
			t.Fatalf("error = %v", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	t.Run("full and latest-only invalid since does not mutate", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		err := run(t, path, nil, false, "--full", "--latest-only", "--since", "bogus", "--resources", "devices,audits")
		if err == nil || !strings.Contains(err.Error(), "invalid --since value \"bogus\"") {
			t.Fatalf("error = %v", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	assertCleared := func(t *testing.T, db *store.Store, resource string) {
		t.Helper()
		got := read(t, db, resource)
		if got.complete != 0 || got.count != 0 || got.cursor != "" || got.synced != clearedAt || got.scope != "" {
			t.Fatalf("%s cleared checkpoint = %+v, want incomplete zero rows at %s", resource, got, clearedAt)
		}
	}

	t.Run("cancelled before open does not mutate", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := run(t, path, ctx, false, "--full", "--resources", "devices,audits")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	t.Run("cancelled full leaves cleared checkpoints incomplete", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		notes := read(t, db, "notes")
		err := run(t, path, stopBeforeWalk{}, false, "--full", "--resources", "devices,audits")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		assertCleared(t, db, "devices")
		assertCleared(t, db, "audits")
		same(t, read(t, db, "notes"), notes)
	})

	t.Run("cancelled latest-only leaves cleared checkpoints incomplete", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		notes := read(t, db, "notes")
		err := run(t, path, stopBeforeWalk{}, false, "--latest-only", "--resources", "devices,audits")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		assertCleared(t, db, "devices")
		assertCleared(t, db, "audits")
		same(t, read(t, db, "notes"), notes)
	})

	t.Run("cancelled without clear does not mutate", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		err := run(t, path, stopBeforeWalk{}, false, "--resources", "devices,audits")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	assertComplete := func(t *testing.T, db *store.Store, resource string) {
		t.Helper()
		got := read(t, db, resource)
		if got.complete != 1 || got.count != 1 || got.cursor != "" || got.synced == "" || got.synced == clearedAt {
			t.Fatalf("%s finished checkpoint = %+v, want one row and complete", resource, got)
		}
	}

	t.Run("full success marks complete", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		notes := read(t, db, "notes")
		var seenMu sync.Mutex
		seen := map[string]checkpoint{}
		setOnRequest(func(r *http.Request) {
			resource := strings.Trim(r.URL.Path, "/")
			got := read(t, db, resource)
			seenMu.Lock()
			seen[resource] = got
			seenMu.Unlock()
			if strings.Contains(r.URL.RawQuery, "old-") {
				t.Errorf("full sync resumed a cleared cursor: %s", r.URL.RawQuery)
			}
		})
		t.Cleanup(func() { setOnRequest(nil) })
		if err := run(t, path, nil, false, "--full", "--resources", "devices,audits"); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 {
			t.Fatalf("calls = %d, want 2", calls.Load())
		}
		for _, resource := range []string{"devices", "audits"} {
			seenMu.Lock()
			got, ok := seen[resource]
			seenMu.Unlock()
			if !ok {
				t.Fatalf("no in-flight checkpoint for %s", resource)
			}
			if got.complete != 0 || got.cursor != "" {
				t.Fatalf("%s in-flight checkpoint = %+v, want incomplete with no cursor", resource, got)
			}
			assertComplete(t, db, resource)
		}
		same(t, read(t, db, "notes"), notes)
	})

	t.Run("full valid since marks complete", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		setOnRequest(func(r *http.Request) {
			if strings.Contains(r.URL.RawQuery, "old-") {
				t.Errorf("full sync resumed a cleared cursor: %s", r.URL.RawQuery)
			}
		})
		t.Cleanup(func() { setOnRequest(nil) })
		if err := run(t, path, nil, false, "--full", "--since", "7d", "--resources", "devices"); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Fatalf("calls = %d, want 1", calls.Load())
		}
		assertComplete(t, db, "devices")
		got := read(t, db, "audits")
		if got.cursor != "old-audits" || got.complete != 1 || got.count != 50 {
			t.Fatalf("unselected audits checkpoint = %+v", got)
		}
	})

	t.Run("latest-only with since does not clear before the walk", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		setOnRequest(func(r *http.Request) {
			if r.URL.Path != "/devices" {
				return
			}
			got := read(t, db, "devices")
			if got.cursor != "old-devices" || got.count != 100 || got.complete != 0 {
				t.Errorf("in-flight devices checkpoint = %+v, want the pre-walk row", got)
			}
		})
		t.Cleanup(func() { setOnRequest(nil) })
		if err := run(t, path, nil, false, "--latest-only", "--since", "7d", "--resources", "devices"); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Fatalf("calls = %d, want 1", calls.Load())
		}
		assertComplete(t, db, "devices")
	})

	t.Run("dry-run full does not clear", func(t *testing.T) {
		calls.Store(0)
		db, path := open(t)
		before := snapshot(t, db)
		if err := run(t, path, nil, true, "--full", "--resources", "devices,audits"); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0", calls.Load())
		}
		for resource, want := range before {
			same(t, read(t, db, resource), want)
		}
	})

	t.Run("clear leaves a matching scope incomplete and keeps the other", func(t *testing.T) {
		db, _ := open(t)
		if _, err := db.DB().Exec("UPDATE sync_state SET scope_key = ? WHERE resource_type = ?", "template:server=a", "devices"); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSyncStateAt("devices", "cur-a", 4, stamp); err != nil {
			t.Fatal(err)
		}
		if err := db.ClearSyncScopeIfCurrent("devices", "template:server=b"); err != nil {
			t.Fatal(err)
		}
		got := read(t, db, "devices")
		if got.scope != "template:server=a" || got.cursor != "cur-a" || got.count != 4 || got.complete != 1 {
			t.Fatalf("other scope = %+v", got)
		}
		var n int
		if err := db.ClearSyncScopeIfCurrent("missing", ""); err != nil {
			t.Fatal(err)
		}
		if err := db.DB().QueryRow("SELECT COUNT(*) FROM sync_state WHERE resource_type = 'missing'").Scan(&n); err != nil || n != 0 {
			t.Fatalf("clear missing inserted %d rows, err=%v", n, err)
		}
		if err := db.ClearSyncScopeIfCurrent("devices", "template:server=a"); err != nil {
			t.Fatal(err)
		}
		got = read(t, db, "devices")
		if got.scope != "template:server=a" || got.complete != 0 || got.count != 0 || got.cursor != "" || got.synced != clearedAt {
			t.Fatalf("matching clear = %+v", got)
		}
	})
}

// Sync workers write machine events to stdout at the same time.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type checkpoint struct {
	cursor   string
	synced   string
	scope    string
	count    int
	complete int
}

// stopBeforeWalk reports cancellation only from the sync command's post-clear
// check. Store open also calls Err, and a context that is already canceled
// never reaches the clear.
type stopBeforeWalk struct{}

func (stopBeforeWalk) Deadline() (time.Time, bool) { return time.Time{}, false }
func (stopBeforeWalk) Done() <-chan struct{}       { return nil }
func (stopBeforeWalk) Value(any) any               { return nil }
func (stopBeforeWalk) Err() error {
	var pc [1]uintptr
	if runtime.Callers(2, pc[:]) != 1 {
		return nil
	}
	fn := runtime.FuncForPC(pc[0])
	if fn == nil {
		return nil
	}
	file, _ := fn.FileLine(pc[0])
	if strings.HasSuffix(filepath.ToSlash(file), "/internal/cli/sync.go") {
		return context.Canceled
	}
	return nil
}

func fmtError(err error, stderr, stdout string) error {
	return errors.Join(err, errors.New("stderr: "+stderr+"\nstdout: "+stdout))
}
`
