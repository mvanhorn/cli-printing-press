package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestDependentSyncNDJSONLifecycle generates a CLI with a parent-keyed
// dependent and runs that CLI's sync. Flat resources already emit
// sync_start / sync_progress / sync_complete; the dependent phase must too,
// including a silence heartbeat that does not require a wall-clock sleep.
func TestDependentSyncNDJSONLifecycle(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("dep-sync-ndjson")
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
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	require.Contains(t, string(syncSrc), `{"event":"sync_start","resource":"%s","parents":%d}`)
	require.Contains(t, string(syncSrc), "parents_done")
	require.Contains(t, string(syncSrc), "dependentSyncProgressHeartbeat")

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type fakeNDJSONGetter struct {
	dryRun  bool
	rate    float64
	respond func(path string, params map[string]string) (json.RawMessage, error)
}

func (f *fakeNDJSONGetter) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if f.respond != nil {
		return f.respond(path, params)
	}
	return json.RawMessage("[]"), nil
}

func (f *fakeNDJSONGetter) RateLimit() float64 { return f.rate }
func (f *fakeNDJSONGetter) IsDryRun() bool     { return f.dryRun }

func modulesNDJSONDep() dependentResourceDef {
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

func seedNDJSONProjects(t *testing.T, n int) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	items := make([]json.RawMessage, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, json.RawMessage(fmt.Sprintf("{\"id\":\"p-%d\",\"workspace\":\"ws-test\"}", i)))
	}
	if _, _, err := db.UpsertBatch("projects", items); err != nil {
		db.Close()
		t.Fatalf("seed projects: %v", err)
	}
	return db
}

// suppressHeartbeat installs a ticker that never fires so exact progress
// counts cannot pick up a silence line when the run is slow.
func suppressHeartbeat(t *testing.T) {
	t.Helper()
	prev := dependentSyncProgressTicker
	t.Cleanup(func() { dependentSyncProgressTicker = prev })
	dependentSyncProgressTicker = func(time.Duration) (<-chan time.Time, func(), func()) {
		return nil, func() {}, func() {}
	}
}

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func parseNDJSON(t *testing.T, raw string) []map[string]any {
	t.Helper()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("ndjson %q: %v\n%s", line, err, raw)
		}
		out = append(out, ev)
	}
	return out
}

func eventName(ev map[string]any) string {
	name, _ := ev["event"].(string)
	return name
}

func numField(t *testing.T, ev map[string]any, key string) float64 {
	t.Helper()
	v, ok := ev[key].(float64)
	if !ok {
		t.Fatalf("event %v missing numeric %s", ev, key)
	}
	return v
}

func hasEvent(events []map[string]any, name string) bool {
	for _, ev := range events {
		if eventName(ev) == name {
			return true
		}
	}
	return false
}

func assertNoEvent(t *testing.T, events []map[string]any, name string) {
	t.Helper()
	if hasEvent(events, name) {
		t.Fatalf("unexpected %s in %v", name, events)
	}
}

func progressDone(t *testing.T, events []map[string]any) []int {
	t.Helper()
	var got []int
	for _, ev := range events {
		if eventName(ev) != "sync_progress" {
			continue
		}
		got = append(got, int(numField(t, ev, "parents_done")))
		if int(numField(t, ev, "parents")) <= 0 {
			t.Fatalf("progress missing parents: %v", ev)
		}
	}
	return got
}

func eventNames(events []map[string]any) []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = eventName(ev)
	}
	return names
}

func emptyPages() *fakeNDJSONGetter {
	return &fakeNDJSONGetter{respond: func(string, map[string]string) (json.RawMessage, error) {
		return json.RawMessage("[]"), nil
	}}
}

func TestDependentSyncNDJSON_EventOrder(t *testing.T) {
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 3)
	defer db.Close()
	fake := &fakeNDJSONGetter{
		rate: 2.5,
		respond: func(path string, _ map[string]string) (json.RawMessage, error) {
			return json.RawMessage(fmt.Sprintf("[{\"id\":%q}]", path)), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 1)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync err=%v warn=%v", res.Err, res.Warn)
	}
	if res.Count != 3 {
		t.Fatalf("Count = %d, want 3", res.Count)
	}
	parsed := parseNDJSON(t, events.String())
	wantNames := []string{"sync_start", "sync_progress", "sync_progress", "sync_complete"}
	if !slices.Equal(eventNames(parsed), wantNames) {
		t.Fatalf("events = %v, want %v\n%s", eventNames(parsed), wantNames, events.String())
	}
	if eventName(parsed[0]) != "sync_start" || numField(t, parsed[0], "parents") != 3 || parsed[0]["resource"] != "modules" {
		t.Fatalf("start = %v", parsed[0])
	}
	if !slices.Equal(progressDone(t, parsed), []int{1, 3}) {
		t.Fatalf("progress = %v, want [1 3] (parent 2 is inside the throttle window)", progressDone(t, parsed))
	}
	for _, ev := range parsed {
		if eventName(ev) != "sync_progress" {
			continue
		}
		if numField(t, ev, "parents") != 3 || numField(t, ev, "rate_rps") != 2.5 {
			t.Fatalf("progress shape = %v", ev)
		}
	}
	done := parsed[len(parsed)-1]
	if numField(t, done, "total") != 3 || numField(t, done, "duration_ms") < 0 || done["resource"] != "modules" {
		t.Fatalf("complete = %v", done)
	}
}

func TestDependentSyncNDJSON_ThrottlesThousandParents(t *testing.T) {
	suppressHeartbeat(t)
	const n = 1000
	db := seedNDJSONProjects(t, n)
	defer db.Close()
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), emptyPages(), db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 8)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync err=%v warn=%v", res.Err, res.Warn)
	}
	parsed := parseNDJSON(t, events.String())
	wantDone := []int{1, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	if !slices.Equal(progressDone(t, parsed), wantDone) {
		t.Fatalf("progress = %v, want %v", progressDone(t, parsed), wantDone)
	}
	for _, ev := range parsed {
		if eventName(ev) == "sync_progress" {
			if _, ok := ev["rate_rps"]; ok {
				t.Fatalf("rate_rps emitted with a zero rate: %v", ev)
			}
		}
	}
	if eventName(parsed[0]) != "sync_start" || numField(t, parsed[0], "parents") != n {
		t.Fatalf("start = %v", parsed[0])
	}
	last := parsed[len(parsed)-1]
	if eventName(last) != "sync_complete" || numField(t, last, "total") != 0 {
		t.Fatalf("complete = %v", last)
	}
}

func TestDependentSyncNDJSON_HeartbeatWithoutCompletions(t *testing.T) {
	ticks := make(chan time.Time)
	handled := make(chan struct{}, 8)
	prevTicker := dependentSyncProgressTicker
	prevNow := dependentSyncProgressNow
	t.Cleanup(func() {
		dependentSyncProgressTicker = prevTicker
		dependentSyncProgressNow = prevNow
	})
	dependentSyncProgressTicker = func(time.Duration) (<-chan time.Time, func(), func()) {
		return ticks, func() {}, func() {
			handled <- struct{}{}
		}
	}
	base := time.Unix(1_700_000_000, 0)
	var nowMu sync.Mutex
	now := base
	dependentSyncProgressNow = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	setNow := func(ts time.Time) {
		nowMu.Lock()
		now = ts
		nowMu.Unlock()
	}

	db := seedNDJSONProjects(t, 1)
	defer db.Close()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	entered := make(chan struct{})
	var once sync.Once
	fake := &fakeNDJSONGetter{
		respond: func(string, map[string]string) (json.RawMessage, error) {
			once.Do(func() { close(entered) })
			<-release
			return json.RawMessage("[]"), nil
		},
	}
	events := &safeBuf{}
	done := make(chan syncResult, 1)
	received := false
	go func() {
		done <- syncDependentResource(context.Background(), fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, events, 1)
	}()
	defer func() {
		unblock()
		if !received {
			<-done
		}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("parent fetch did not start")
	}
	fire := func() {
		t.Helper()
		select {
		case ticks <- time.Time{}:
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeat tick was not consumed")
		}
		select {
		case <-handled:
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeat tick was not handled")
		}
	}

	fire()
	if strings.Contains(events.String(), "\"event\":\"sync_progress\"") {
		t.Fatalf("progress emitted before the silence window: %s", events.String())
	}
	if !strings.Contains(events.String(), "\"event\":\"sync_start\"") {
		t.Fatalf("missing sync_start while the parent is in flight: %s", events.String())
	}

	setNow(base.Add(dependentSyncProgressHeartbeat))
	fire()
	mid := parseNDJSON(t, events.String())
	if !slices.Equal(progressDone(t, mid), []int{0}) {
		t.Fatalf("heartbeat progress = %v, want [0]\n%s", progressDone(t, mid), events.String())
	}
	assertNoEvent(t, mid, "sync_complete")

	unblock()
	var res syncResult
	select {
	case res = <-done:
		received = true
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not finish")
	}
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("clean finish err=%v warn=%v", res.Err, res.Warn)
	}
	final := parseNDJSON(t, events.String())
	want := []string{"sync_start", "sync_progress", "sync_progress", "sync_complete"}
	if !slices.Equal(eventNames(final), want) {
		t.Fatalf("events = %v, want %v\n%s", eventNames(final), want, events.String())
	}
	if !slices.Equal(progressDone(t, final), []int{0, 1}) {
		t.Fatalf("progress = %v, want [0 1]", progressDone(t, final))
	}
}

func TestDependentSyncNDJSON_NoCompleteOnDryRun(t *testing.T) {
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 2)
	defer db.Close()
	fake := &fakeNDJSONGetter{
		dryRun: true,
		respond: func(string, map[string]string) (json.RawMessage, error) {
			return json.RawMessage("{\"dry_run\":true}"), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 1)
	if res.Err != nil || res.Warn != nil || res.Count != 0 {
		t.Fatalf("dry-run result = %+v", res)
	}
	parsed := parseNDJSON(t, events.String())
	if !hasEvent(parsed, "sync_start") || !hasEvent(parsed, "sync_dryrun") {
		t.Fatalf("dry-run events = %v", eventNames(parsed))
	}
	assertNoEvent(t, parsed, "sync_complete")
	if n := strings.Count(events.String(), "\"event\":\"sync_dryrun\""); n != 1 {
		t.Fatalf("sync_dryrun count = %d, want 1\n%s", n, events.String())
	}
}

func TestDependentSyncNDJSON_NoCompleteOnCancel(t *testing.T) {
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 2)
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	entered := make(chan struct{})
	var once sync.Once
	fake := &fakeNDJSONGetter{
		respond: func(string, map[string]string) (json.RawMessage, error) {
			once.Do(func() { close(entered) })
			<-release
			return json.RawMessage("[]"), nil
		},
	}
	var events safeBuf
	done := make(chan syncResult, 1)
	received := false
	go func() {
		done <- syncDependentResource(ctx, fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 1)
	}()
	defer func() {
		unblock()
		if !received {
			<-done
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("parent fetch did not start")
	}
	cancel()
	unblock()
	var res syncResult
	select {
	case res = <-done:
		received = true
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not finish after cancel")
	}
	if res.Err != nil || res.Warn == nil {
		t.Fatalf("cancel result err=%v warn=%v", res.Err, res.Warn)
	}
	parsed := parseNDJSON(t, events.String())
	if !hasEvent(parsed, "sync_start") {
		t.Fatalf("cancel events = %v", eventNames(parsed))
	}
	assertNoEvent(t, parsed, "sync_complete")
}

func TestDependentSyncNDJSON_NoCompleteOnAllFailed(t *testing.T) {
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 4)
	defer db.Close()
	fake := &fakeNDJSONGetter{
		respond: func(string, map[string]string) (json.RawMessage, error) {
			return nil, fmt.Errorf("parent fetch failed")
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 4)
	if res.Err == nil || res.Warn != nil {
		t.Fatalf("all-failed result = %+v", res)
	}
	parsed := parseNDJSON(t, events.String())
	if !hasEvent(parsed, "sync_start") || !hasEvent(parsed, "sync_progress") || !hasEvent(parsed, "sync_error") {
		t.Fatalf("all-failed events = %v\n%s", eventNames(parsed), events.String())
	}
	assertNoEvent(t, parsed, "sync_complete")
}

func TestDependentSyncNDJSON_NoCompleteOnPartialFailure(t *testing.T) {
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 2)
	defer db.Close()
	stmt := "CREATE TRIGGER fail_module_upsert BEFORE INSERT ON resources WHEN NEW.resource_type = 'modules' AND json_extract(NEW.data, '$.parent_id') = 'p-0' BEGIN SELECT RAISE(ABORT, 'forced module upsert failure'); END"
	if _, err := db.DB().Exec(stmt); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	fake := &fakeNDJSONGetter{
		respond: func(path string, _ map[string]string) (json.RawMessage, error) {
			return json.RawMessage(fmt.Sprintf("[{\"id\":%q}]", path)), nil
		},
	}
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), fake, db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 2)
	if res.Err != nil || res.Warn == nil {
		t.Fatalf("partial result err=%v warn=%v", res.Err, res.Warn)
	}
	parsed := parseNDJSON(t, events.String())
	if !hasEvent(parsed, "sync_start") {
		t.Fatalf("partial events = %v", eventNames(parsed))
	}
	assertNoEvent(t, parsed, "sync_complete")
}

func TestDependentSyncNDJSON_HumanFriendlySuppresses(t *testing.T) {
	prev := humanFriendly
	humanFriendly = true
	t.Cleanup(func() { humanFriendly = prev })
	suppressHeartbeat(t)
	db := seedNDJSONProjects(t, 2)
	defer db.Close()
	var events bytes.Buffer
	res := syncDependentResource(context.Background(), emptyPages(), db, modulesNDJSONDep(), "", true, 0, false, false, &syncUserParams{}, &events, 1)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("human-friendly sync err=%v warn=%v", res.Err, res.Warn)
	}
	if events.Len() != 0 {
		t.Fatalf("human-friendly emitted NDJSON: %s", events.String())
	}
}

func TestDependentSyncNDJSON_SilenceWindow(t *testing.T) {
	prev := dependentSyncProgressNow
	t.Cleanup(func() { dependentSyncProgressNow = prev })
	base := time.Unix(1_700_000_000, 0)
	now := base
	dependentSyncProgressNow = func() time.Time { return now }

	p := &dependentProgress{total: 1000, lastEmit: now}
	var marks []int
	for i := 0; i < 1000; i++ {
		done, emit := p.noteParent()
		if emit {
			marks = append(marks, done)
		}
	}
	want := []int{1, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	if !slices.Equal(marks, want) {
		t.Fatalf("marks = %v, want %v", marks, want)
	}
	if _, emit := p.heartbeat(); emit {
		t.Fatal("heartbeat fired immediately after progress")
	}
	now = base.Add(dependentSyncProgressHeartbeat - time.Nanosecond)
	if _, emit := p.heartbeat(); emit {
		t.Fatal("heartbeat fired before the silence window elapsed")
	}
	now = base.Add(dependentSyncProgressHeartbeat)
	done, emit := p.heartbeat()
	if !emit || done != 1000 {
		t.Fatalf("heartbeat = done %d emit %v, want 1000 true", done, emit)
	}
	if _, emit := p.heartbeat(); emit {
		t.Fatal("heartbeat fired again without more silence")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "dependent_ndjson_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGeneratedCLITest := func(t *testing.T, args ...string) {
		t.Helper()
		out, err := runGoCommandOutputWithEnv(t, outputDir, []string{"PRINTING_PRESS_DOGFOOD=0"}, args...)
		require.NoError(t, err, out)
	}
	runGeneratedCLITest(t, "test", "-run", "TestDependentSyncNDJSON_", "./internal/cli")
	runGeneratedCLITest(t, "test", "-run", "TestDependentSyncNDJSON_", "-race", "./internal/cli")
}
