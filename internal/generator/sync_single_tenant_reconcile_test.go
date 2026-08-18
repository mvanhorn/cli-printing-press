package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestSingleTenantReconcile_CompleteWalkPrunes proves the #4239 single-tenant
// prune / completeness-gate pair: a print with zero tenant-scoped flat
// resources emits a reconcilable mode, --full deletes local rows absent from a
// complete walk, and a full page with no followable cursor leaves complete=false
// so prune does not run.
func TestSingleTenantReconcile_CompleteWalkPrunes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("streconcile")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"devices": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/devices",
					Response:   spec.ResponseDef{Type: "array", Item: "Device"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Device": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)
	require.Contains(t, src, `"devices": "flat_global"`,
		"no-tenant-scope resource must get a reconcilable whole-table mode")
	require.Contains(t, src, "db.ReconcileAll(",
		"sync.go must call ReconcileAll for single-tenant whole-table prune")
	require.Contains(t, src, "func paginationEndUnprovable(",
		"sync.go must gate completeness on an unprovable full-page-with-no-cursor")
	require.Contains(t, src, "after a complete walk",
		"--no-prune help must describe prune-after-complete-walk, not a parent partition")
	require.NotContains(t, src, "fully-enumerated parent partition",
		"--no-prune help must not claim parent-partition prune on a single-tenant print")

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	require.Contains(t, string(storeSrc), "func (s *Store) ReconcileAll",
		"store.go must emit ReconcileAll")

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type stubDeviceClient struct {
	items []json.RawMessage
}

func (s *stubDeviceClient) Get(_ context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
	payload, _ := json.Marshal(s.items)
	return json.RawMessage(payload), nil
}

func (s *stubDeviceClient) RateLimit() float64 { return 0 }

func seedDevices(t *testing.T, db *store.Store, ids ...string) {
	t.Helper()
	var batch []json.RawMessage
	for _, id := range ids {
		raw, err := json.Marshal(map[string]any{"id": id, "name": "device-" + id})
		if err != nil {
			t.Fatalf("marshal device %s: %v", id, err)
		}
		batch = append(batch, raw)
	}
	if _, _, err := db.UpsertBatch("devices", batch); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
}

func deviceExists(t *testing.T, db *store.Store, id string) bool {
	t.Helper()
	_, err := db.Get("devices", id)
	return err == nil
}

func TestSingleTenantReconcile_CompleteWalkDeletesAbsentRows(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seedDevices(t, db, "keep-1", "ghost-1", "ghost-2")

	keep, _ := json.Marshal(map[string]any{"id": "keep-1", "name": "device-keep-1"})
	client := &stubDeviceClient{items: []json.RawMessage{json.RawMessage(keep)}}
	var events bytes.Buffer
	syncResource(context.Background(), client, db, "devices", "", true, 0, false, true, nil, &events)

	if !deviceExists(t, db, "keep-1") {
		t.Fatal("keep-1 was pruned; a complete walk must keep rows the API still returns")
	}
	if deviceExists(t, db, "ghost-1") {
		t.Fatal("ghost-1 was NOT pruned; --full must delete local rows absent from a complete walk")
	}
	if deviceExists(t, db, "ghost-2") {
		t.Fatal("ghost-2 was NOT pruned; --full must delete local rows absent from a complete walk")
	}
	if !strings.Contains(events.String(), "\"event\":\"reconcile\"") {
		t.Fatalf("expected reconcile event after a complete single-tenant walk, got %s", events.String())
	}
}

func TestSingleTenantReconcile_FullPageNoCursorSkipsPrune(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	seedDevices(t, db, "page-1", "ghost-beyond-page")

	limit := determinePaginationDefaults("devices").limit
	page := make([]json.RawMessage, 0, limit)
	for i := 0; i < limit; i++ {
		id := "page-item-" + strconv.Itoa(i)
		if i == 0 {
			id = "page-1"
		}
		raw, _ := json.Marshal(map[string]any{"id": id, "name": "device-" + id})
		page = append(page, json.RawMessage(raw))
	}
	client := &stubDeviceClient{items: page}
	var events bytes.Buffer
	syncResource(context.Background(), client, db, "devices", "", true, 0, false, true, nil, &events)

	if !deviceExists(t, db, "ghost-beyond-page") {
		t.Fatal("ghost-beyond-page was pruned; a full page with no followable cursor must leave complete=false and skip prune")
	}
	if !strings.Contains(events.String(), "\"event\":\"reconcile_skipped\"") || !strings.Contains(events.String(), "\"reason\":\"cursor_unavailable\"") {
		t.Fatalf("expected reconcile_skipped with cursor_unavailable, got %s", events.String())
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "single_tenant_reconcile_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSingleTenantReconcile", "./internal/cli")
}
