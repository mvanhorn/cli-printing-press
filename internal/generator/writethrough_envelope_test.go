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
	"os"
	"testing"
)

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
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestWriteThroughCacheEnvelopeExtractionParity", "-count=1")
}
