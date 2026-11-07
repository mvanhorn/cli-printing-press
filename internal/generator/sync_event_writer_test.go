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

// TestGeneratedFlatSyncEventWriterLock generates a CLI with only flat
// resources (no dependent pool) and runs sync --concurrency 8 into a writer
// that records overlapping Write calls and stores them in an unsynchronized
// bytes.Buffer. The flat pool used to hand every worker the raw command
// writer. The dependent path already wrapped its writer; this CLI has no
// dependent path, so the lock has to be installed where the writer is created.
func TestGeneratedFlatSyncEventWriterLock(t *testing.T) {
	t.Parallel()

	resources := []string{"itema", "itemb", "itemc", "itemd", "iteme", "itemf", "itemg", "itemh"}
	apiSpec := eventLockFlatSpec("flateventlock", resources)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "syncEventWriter = ensureLockedWriter(syncEventWriter)")
	require.Equal(t, 1, strings.Count(syncSrc, "type lockedWriter struct {"))
	require.Equal(t, 1, strings.Count(syncSrc, "func ensureLockedWriter("))
	require.NotContains(t, syncSrc, "func syncDependentResource(")
	require.NotContains(t, syncSrc, "syncEvents = &lockedWriter{")

	behavior := strings.ReplaceAll(flatEventLockBehaviorTest, "FLATEVENTLOCK_BASE_URL", naming.EnvPrefix(apiSpec.Name)+"_BASE_URL")
	behavior = strings.ReplaceAll(behavior, "ITEM_LIST", strings.Join(resources, ","))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_event_writer_test.go"), []byte(behavior), 0o644))

	runGoCommand(t, outputDir, "test", "-count=1", "-run", flatEventLockRun, "./internal/cli")
	runGoCommand(t, outputDir, "test", "-count=1", "-race", "-run", flatEventLockRun, "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

// TestGeneratedDependentSyncEventWriterLock covers the other branch of
// ensureLockedWriter: a direct syncDependentResource caller passes a plain
// writer, and the dependent pool must still serialize it. A second generated
// CLI proves the shared type is not emitted twice when the dependent pool
// exists.
func TestGeneratedDependentSyncEventWriterLock(t *testing.T) {
	t.Parallel()

	apiSpec := eventLockDependentSpec("depeventlock")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "syncEventWriter = ensureLockedWriter(syncEventWriter)")
	require.Contains(t, syncSrc, "syncEvents = ensureLockedWriter(syncEvents)")
	require.Equal(t, 1, strings.Count(syncSrc, "type lockedWriter struct {"))
	require.NotContains(t, syncSrc, "syncEvents = &lockedWriter{")

	behavior := strings.ReplaceAll(dependentEventLockBehaviorTest, "DEPEVENTLOCK_MODULE", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_dependent_event_writer_test.go"), []byte(behavior), 0o644))

	runGoCommand(t, outputDir, "test", "-count=1", "-run", dependentEventLockRun, "./internal/cli")
	runGoCommand(t, outputDir, "test", "-count=1", "-race", "-run", dependentEventLockRun, "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

const (
	flatEventLockRun      = "^Test(SafeProbeSeesUnlockedOverlap|LockedWriterSerializesWrites|LockedWriterReturnsUnderlyingError|EnsureLockedWriterBranches|FlatSyncPoolSerializesEventWrites)$"
	dependentEventLockRun = "^TestDependentPoolSerializesPlainEventWriter$"
)

func eventLockFlatSpec(name string, resources []string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{}
	for _, resource := range resources {
		apiSpec.Resources[resource] = spec.Resource{
			Description: resource,
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/" + resource,
					Description: "List " + resource,
					Response:    spec.ResponseDef{Type: "array"},
				},
			},
		}
	}
	return apiSpec
}

func eventLockDependentSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/projects",
					Response: spec.ResponseDef{Type: "array", Item: "Project"},
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
		"Project": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Module":  {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
	}
	return apiSpec
}

const flatEventLockBehaviorTest = `package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// safeProbe counts overlapping Write calls. The buffer is locked, so the
// negative case below is not itself a data race.
type safeProbe struct {
	mu       sync.Mutex
	inFlight int
	overlaps int
	writes   int
	hold     time.Duration
	buf      bytes.Buffer
}

func (o *safeProbe) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.inFlight++
	if o.inFlight > 1 {
		o.overlaps++
	}
	hold := o.hold
	o.mu.Unlock()

	if hold > 0 {
		time.Sleep(hold)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.inFlight--
	o.writes++
	return o.buf.Write(p)
}

func (o *safeProbe) stats() (overlaps, writes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overlaps, o.writes
}

// eventProbe is the pool's stdout. The bytes.Buffer is not locked: Write
// callers must already serialize. A missing command lock races in grow and
// is visible to the race detector.
type eventProbe struct {
	mu       sync.Mutex
	inFlight int
	overlaps int
	writes   int
	hold     time.Duration
	buf      bytes.Buffer
}

func (o *eventProbe) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.inFlight++
	if o.inFlight > 1 {
		o.overlaps++
	}
	hold := o.hold
	o.mu.Unlock()

	if hold > 0 {
		time.Sleep(hold)
	}
	n, err := o.buf.Write(p)

	o.mu.Lock()
	o.inFlight--
	o.writes++
	o.mu.Unlock()
	return n, err
}

func (o *eventProbe) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func (o *eventProbe) stats() (overlaps, writes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overlaps, o.writes
}

func TestSafeProbeSeesUnlockedOverlap(t *testing.T) {
	w := &safeProbe{hold: 20 * time.Millisecond}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Write([]byte("{\"event\":\"probe\"}\n"))
		}()
	}
	wg.Wait()
	overlaps, writes := w.stats()
	if overlaps == 0 {
		t.Fatal("unlocked concurrent writes did not overlap")
	}
	if writes != 4 {
		t.Fatalf("writes = %d, want 4", writes)
	}
}

func TestLockedWriterSerializesWrites(t *testing.T) {
	det := &safeProbe{hold: 20 * time.Millisecond}
	w := &lockedWriter{w: det}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Write([]byte("{\"event\":\"probe\"}\n"))
		}()
	}
	wg.Wait()
	overlaps, writes := det.stats()
	if overlaps != 0 {
		t.Fatalf("overlaps = %d, want 0", overlaps)
	}
	if writes != 4 {
		t.Fatalf("writes = %d, want 4", writes)
	}
}

type eventFailWriter struct{ err error }

func (f eventFailWriter) Write([]byte) (int, error) { return 0, f.err }

func TestLockedWriterReturnsUnderlyingError(t *testing.T) {
	want := errors.New("disk full")
	n, err := (&lockedWriter{w: eventFailWriter{err: want}}).Write([]byte("x"))
	if n != 0 || !errors.Is(err, want) {
		t.Fatalf("Write = %d, %v", n, err)
	}
}

func TestEnsureLockedWriterBranches(t *testing.T) {
	plain := &bytes.Buffer{}
	wrapped := ensureLockedWriter(plain)
	locked, ok := wrapped.(*lockedWriter)
	if !ok || locked.w != plain {
		t.Fatal("plain writer was not wrapped")
	}
	if again := ensureLockedWriter(locked); again != locked {
		t.Fatal("already-locked writer was wrapped again")
	}
}

func TestFlatSyncPoolSerializesEventWrites(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")

	resources := strings.Split("ITEM_LIST", ",")
	var current atomic.Int32
	var peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := current.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		defer current.Add(-1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	}))
	defer server.Close()
	t.Setenv("FLATEVENTLOCK_BASE_URL", server.URL)

	det := &eventProbe{hold: 15 * time.Millisecond}
	cmd := newSyncCmd(&rootFlags{
		configPath: filepath.Join(t.TempDir(), "config.toml"),
		timeout:    5 * time.Second,
	})
	cmd.SetOut(det)
	cmd.SetErr(io.Discard)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--db", filepath.Join(t.TempDir(), "data.db"),
		"--concurrency", "8",
		"--resources", strings.Join(resources, ","),
	})
	err := cmd.Execute()
	body := det.String()
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, body)
	}
	if peak.Load() < 2 {
		t.Fatalf("max in-flight HTTP = %d, want at least 2 so the pool ran concurrently\n%s", peak.Load(), body)
	}
	overlaps, writes := det.stats()
	if overlaps != 0 {
		t.Fatalf("overlapping event writes = %d\n%s", overlaps, body)
	}
	if writes < len(resources) {
		t.Fatalf("event writes = %d, want at least one per resource\n%s", writes, body)
	}
	if got := strings.Count(body, "\"event\":\"sync_start\""); got != len(resources) {
		t.Fatalf("sync_start = %d, want %d\n%s", got, len(resources), body)
	}
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if line == "" || json.Valid([]byte(line)) {
			continue
		}
		t.Fatalf("event line is not one JSON value: %s", line)
	}
}
`

const dependentEventLockBehaviorTest = `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"DEPEVENTLOCK_MODULE/internal/store"
)

// eventProbe matches the flat-pool probe: the bytes.Buffer is not locked,
// so a missing wrap races.
type eventProbe struct {
	mu       sync.Mutex
	inFlight int
	overlaps int
	writes   int
	hold     time.Duration
	buf      bytes.Buffer
}

func (o *eventProbe) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.inFlight++
	if o.inFlight > 1 {
		o.overlaps++
	}
	hold := o.hold
	o.mu.Unlock()

	if hold > 0 {
		time.Sleep(hold)
	}
	n, err := o.buf.Write(p)

	o.mu.Lock()
	o.inFlight--
	o.writes++
	o.mu.Unlock()
	return n, err
}

func (o *eventProbe) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func (o *eventProbe) stats() (overlaps, writes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overlaps, o.writes
}

type eventLockGetter struct{}

func (eventLockGetter) Get(context.Context, string, map[string]string) (json.RawMessage, error) {
	return json.RawMessage("[]"), nil
}

func (eventLockGetter) RateLimit() float64 { return 0 }

func eventLockModulesDep() dependentResourceDef {
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

func seedEventLockProjects(t *testing.T, n int) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	items := make([]json.RawMessage, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, json.RawMessage(fmt.Sprintf("{\"id\":\"p-%d\"}", i)))
	}
	if _, _, err := db.UpsertBatch("projects", items); err != nil {
		db.Close()
		t.Fatalf("seed projects: %v", err)
	}
	return db
}

func TestDependentPoolSerializesPlainEventWriter(t *testing.T) {
	const parents = 6
	db := seedEventLockProjects(t, parents)
	defer db.Close()

	events := &eventProbe{hold: 15 * time.Millisecond}
	res := syncDependentResource(context.Background(), eventLockGetter{}, db, eventLockModulesDep(), "", true, 0, false, true, &syncUserParams{}, events, 4)
	if res.Err != nil {
		t.Fatalf("sync: %v\n%s", res.Err, events.String())
	}
	overlaps, writes := events.stats()
	if overlaps != 0 {
		t.Fatalf("overlaps = %d\n%s", overlaps, events.String())
	}
	if writes < parents {
		t.Fatalf("writes = %d, want at least one per parent\n%s", writes, events.String())
	}
}
`
