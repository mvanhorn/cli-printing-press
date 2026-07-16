package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestFlatReconcile_SkipOnUnknownTenant is the central safety case for Task 8.
// A flat resource classified reconcileMode=="flat" (projects, with
// TenantScopeColumn="workspace") must prune its tenant partition after a
// COMPLETE --full sync — BUT only when the active tenant is known. When
// resolveTenantID()=="" the flat reconcile path SKIPS entirely (zero deletes):
// this is the OPPOSITE of the dependent fan-out fallback (which goes unscoped).
//
// It is a generated-run test: we generate a CLI with a flat "projects" resource
// carrying a tenant scope column, then write an in-package test into the
// generated internal/cli directory and drive syncResource with a stubbed client.
func TestFlatReconcile_SkipOnUnknownTenant(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("flat-reconcile")
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

	// Caller-wiring assertions: the flat reconcile machinery must be present in
	// the generated sync.go so it cannot silently regress.
	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)
	require.Contains(t, src, "flatReconcilable := resourceReconcileMode(resource) == \"flat\"",
		"sync.go must declare flatReconcilable in syncResource")
	require.Contains(t, src, "func resourceReconcileMode(resource string) string",
		"sync.go must emit the resourceReconcileMode lookup")
	require.Contains(t, src, "func flatReconcileDef(resource string)",
		"sync.go must emit the flatReconcileDef lookup")
	require.Contains(t, src, `"unknown-tenant"`,
		"sync.go must emit the unknown-tenant skip reason")
	require.Contains(t, src, `"unsupported-resource-shape"`,
		"sync.go must make unsupported full-sync pruning observable")

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

// stubFlatClient returns a fixed first page, then an empty page to terminate.
// When fullPage is true the first page is treated as "exactly the limit" so a
// maxPages cap can hit with outcome.complete still false.
type stubFlatClient struct {
	items []json.RawMessage
	calls int
}

func (s *stubFlatClient) Get(_ context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
	s.calls++
	if s.calls > 1 {
		return json.RawMessage("[]"), nil
	}
	payload, _ := json.Marshal(s.items)
	return json.RawMessage(payload), nil
}

func (s *stubFlatClient) RateLimit() float64 { return 0 }

func seedProjects(t *testing.T, db *store.Store, items map[string]string) {
	t.Helper()
	var batch []json.RawMessage
	for id, ws := range items {
		raw, err := json.Marshal(map[string]any{"id": id, "workspace": ws, "name": "proj-" + id})
		if err != nil {
			t.Fatalf("marshal project %s: %v", id, err)
		}
		batch = append(batch, raw)
	}
	if _, _, err := db.UpsertBatch("projects", batch); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
}

func projectExists(t *testing.T, db *store.Store, id string) bool {
	t.Helper()
	_, err := db.Get("projects", id)
	return err == nil
}

// runFlatSync drives syncResource on a fresh store seeded with the multi-tenant
// fixture, with the stub returning only {p1@ws-A} on the first page so p2 is now
// stale within ws-A's partition. maxPages=0 means unlimited => the single short
// page is a natural-end (outcome.complete==true).
func runFlatSync(t *testing.T, resolver func() string, prune bool, maxPages int) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	seedProjects(t, db, map[string]string{"p1": "ws-A", "p2": "ws-A", "p3": "ws-B"})

	p1, _ := json.Marshal(map[string]any{"id": "p1", "workspace": "ws-A", "name": "proj-p1"})
	client := &stubFlatClient{items: []json.RawMessage{json.RawMessage(p1)}}

	prev := resolveTenantID
	resolveTenantID = resolver
	defer func() { resolveTenantID = prev }()

	syncResource(context.Background(), client, db, "projects", "", true, maxPages, false, prune, nil, nil)
	return db
}

// TestFlatReconcile_SkipOnUnknownTenant: unknown tenant must delete NOTHING.
func TestFlatReconcile_SkipOnUnknownTenant(t *testing.T) {
	db := runFlatSync(t, func() string { return "" }, true, 0)
	defer db.Close()

	count, err := db.Count("projects")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("unknown-tenant reconcile = %d projects, want 3 (skip-on-unknown deletes nothing)", count)
	}
	for _, id := range []string{"p1", "p2", "p3"} {
		if !projectExists(t, db, id) {
			t.Fatalf("project %s missing after unknown-tenant sync; nothing should have been pruned", id)
		}
	}
}

// TestFlatReconcile_KnownTenantPrunesOnlyThatPartition: ws-A known => only ws-A
// stale p2 pruned; ws-A p1 kept; ws-B p3 untouched.
func TestFlatReconcile_KnownTenantPrunesOnlyThatPartition(t *testing.T) {
	db := runFlatSync(t, func() string { return "ws-A" }, true, 0)
	defer db.Close()

	if !projectExists(t, db, "p1") {
		t.Fatalf("p1 (ws-A, still returned) was pruned; it must be kept")
	}
	if projectExists(t, db, "p2") {
		t.Fatalf("p2 (ws-A, stale) was NOT pruned; known-tenant reconcile must delete it")
	}
	if !projectExists(t, db, "p3") {
		t.Fatalf("p3 (ws-B) was pruned; reconcile must scope to ws-A only")
	}
}

// TestFlatReconcile_PruneFalsePrunesNothing: prune=false (e.g. --no-prune) must
// skip the reconcile entirely even with a known tenant and a complete sync.
func TestFlatReconcile_PruneFalsePrunesNothing(t *testing.T) {
	db := runFlatSync(t, func() string { return "ws-A" }, false, 0)
	defer db.Close()

	count, err := db.Count("projects")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("no-prune reconcile = %d projects, want 3 (prune=false deletes nothing)", count)
	}
}

// TestFlatReconcile_CapHitPrunesNothing: a maxPages cap hit leaves
// outcome.complete==false, so the known-tenant reconcile SKIPS (no deletes).
func TestFlatReconcile_CapHitPrunesNothing(t *testing.T) {
	// maxPages=1 with a full first page (>= limit) trips the cap before any
	// natural-end break, so outcome.complete stays false.
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seedProjects(t, db, map[string]string{"p1": "ws-A", "p2": "ws-A", "p3": "ws-B"})

	// One full page == the default pagination limit so the cap counts as truncation.
	limit := determinePaginationDefaults("projects").limit
	page := make([]json.RawMessage, 0, limit)
	for i := 0; i < limit; i++ {
		raw, _ := json.Marshal(map[string]any{"id": "p1", "workspace": "ws-A", "name": "proj-p1"})
		page = append(page, json.RawMessage(raw))
	}
	client := &stubFlatClient{items: page}

	prev := resolveTenantID
	resolveTenantID = func() string { return "ws-A" }
	defer func() { resolveTenantID = prev }()

	syncResource(context.Background(), client, db, "projects", "", true, 1, false, true, nil, nil)

	count, err := db.Count("projects")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("cap-hit reconcile = %d projects, want 3 (incomplete partition prunes nothing)", count)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "flat_reconcile_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestFlatReconcile", "./internal/cli")
}
