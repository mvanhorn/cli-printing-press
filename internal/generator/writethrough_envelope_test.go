package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteThroughCacheEnvelopeExtractionParity verifies writeThroughCache keeps
// list-envelope extraction behavior aligned with sync extraction: known wrapper
// keys continue to work, resource-named wrappers are accepted via single-array
// fallback, and detail objects with empty wrapper-named fields are not
// misclassified as list envelopes.
func TestWriteThroughCacheEnvelopeExtractionParity(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("writethrough-envelope")
	outputDir := filepath.Join(t.TempDir(), "writethrough-envelope-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "writethrough_envelope_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func captureWriteThroughStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = old
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(data)
}

func TestWriteThroughCacheEnvelopeExtractionParity(t *testing.T) {
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	ctx := context.Background()

	writeThroughCache(ctx, "events", json.RawMessage(`+"`"+`{"events":[{"id":1,"name":"launch"}]}`+"`"+`))
	writeThroughCache(ctx, "results", json.RawMessage(`+"`"+`{"results":[{"id":"r1","name":"ok"}]}`+"`"+`))
	writeThroughCache(ctx, "orders", json.RawMessage(`+"`"+`{"id":"x","items":[],"status":"y"}`+"`"+`))
	// Detail object carrying ONE multi-element object-array alongside a scalar
	// id must cache as a single row, not have its sub-array misread as the list.
	writeThroughCache(ctx, "lineitemorders", json.RawMessage(`+"`"+`{"id":"o-2","line_items":[{"id":"li-1","sku":"a"},{"id":"li-2","sku":"b"}]}`+"`"+`))
	// Empty page of a newly-promoted wrapper key alongside a pagination cursor
	// is an empty list envelope, not a detail object: nothing should be cached.
	writeThroughCache(ctx, "emptyrecords", json.RawMessage(`+"`"+`{"records":[],"cursor":"abc"}`+"`"+`))

	db, err := openStoreForRead(ctx, "writethrough-envelope-pp-cli")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if db == nil {
		t.Fatalf("expected store to exist after write-through cache")
	}
	defer db.Close()

	events, err := db.List("events", 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 cached events row, got %d", len(events))
	}

	results, err := db.List("results", 10)
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 cached results row, got %d", len(results))
	}

	orders, err := db.List("orders", 10)
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected detail object with empty items to cache as single row, got %d", len(orders))
	}

	var order map[string]any
	if err := json.Unmarshal(orders[0], &order); err != nil {
		t.Fatalf("decode cached order: %v", err)
	}
	if got, ok := order["id"].(string); !ok || got != "x" {
		t.Fatalf("expected cached detail object id to be x, got %#v", order["id"])
	}

	lineOrders, err := db.List("lineitemorders", 10)
	if err != nil {
		t.Fatalf("list lineitemorders: %v", err)
	}
	if len(lineOrders) != 1 {
		t.Fatalf("detail object with a multi-element sub-array must cache as one row (not its line_items), got %d", len(lineOrders))
	}

	emptyRecords, err := db.List("emptyrecords", 10)
	if err != nil {
		t.Fatalf("list emptyrecords: %v", err)
	}
	if len(emptyRecords) != 0 {
		t.Fatalf("empty list envelope with a records wrapper must cache nothing, got %d", len(emptyRecords))
	}
}

func TestWriteThroughCacheWarnsOnlyWhenNoRowsPersist(t *testing.T) {
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	ctx := context.Background()
	stderr := captureWriteThroughStderr(t, func() {
		writeThroughCache(ctx, "snapshots", json.RawMessage(`+"`"+`[{"symbol":"NIFTY"},{"symbol":"BANKNIFTY"}]`+"`"+`))
	})
	if !strings.Contains(stderr, "not cached locally") {
		t.Fatalf("zero-persist write-through cache should warn, got stderr %q", stderr)
	}

	stderr = captureWriteThroughStderr(t, func() {
		writeThroughCache(ctx, "mixedsnapshots", json.RawMessage(`+"`"+`[{"id":"ok-1"},{"symbol":"NIFTY"}]`+"`"+`))
	})
	if stderr != "" {
		t.Fatalf("mixed write-through cache should not warn when at least one row persists, got stderr %q", stderr)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestWriteThroughCacheEnvelopeExtractionParity|TestWriteThroughCacheWarnsOnlyWhenNoRowsPersist", "-count=1")
}
