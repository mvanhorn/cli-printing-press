package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mapKeyedStoreTestSource exercises the flattener and the two id resolvers that
// must agree with it. Anything the flattener lifts out of a map-keyed
// collection has to resolve to an id, or the rows vanish from the local mirror
// without an error.
const mapKeyedStoreTestSource = `package store

import (
	"encoding/json"
	"testing"
)

func TestMapKeyedFlattenDepthOne(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"7788990011223344":{"id":"7788990011223344","title":"weekly sync"}}` + "`" + `))
	if !ok {
		t.Fatalf("id-keyed object should be recognized as a map-keyed collection")
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	var obj map[string]any
	if err := json.Unmarshal(items[0], &obj); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if obj["title"] != "weekly sync" {
		t.Fatalf("expected the leaf record, got %#v", obj)
	}
	if got := ExtractResourceID("calls", obj); got != "7788990011223344" {
		t.Fatalf("expected the record's own id, got %q", got)
	}
}

func TestMapKeyedFlattenDepthTwo(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"2026-08-19":{"7788990011223344":{"id":"7788990011223344","title":"a"}},"2026-08-20":{"1122334455667788":{"id":"1122334455667788","title":"b"}}}` + "`" + `))
	if !ok {
		t.Fatalf("bucketed id-keyed object should be recognized as a map-keyed collection")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items across 2 buckets, got %d", len(items))
	}
	// Entries are key-sorted, so bucket order is stable across runs.
	var first map[string]any
	if err := json.Unmarshal(items[0], &first); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if first["title"] != "a" {
		t.Fatalf("expected deterministic key-sorted order, got %#v", first)
	}
}

func TestMapKeyedFlattenStampsKeyOnIDLessRecord(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"2026-08-19":{"7788990011223344":{"title":"no id field"}}}` + "`" + `))
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 flattened item, ok=%v len=%d", ok, len(items))
	}
	var obj map[string]any
	if err := json.Unmarshal(items[0], &obj); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if obj[MapKeyIDField] != "7788990011223344" {
		t.Fatalf("expected the leaf map key stamped on an id-less record, got %#v", obj)
	}
	if got := ExtractResourceID("calls", obj); got != "7788990011223344" {
		t.Fatalf("ExtractResourceID must fall back to the stamped map key, got %q", got)
	}
	if got := extractObjectID(obj); got != "7788990011223344" {
		t.Fatalf("extractObjectID must fall back to the stamped map key, got %q", got)
	}
}

func TestMapKeyedStampNeverOutranksARealID(t *testing.T) {
	obj := map[string]any{"id": "real-id", MapKeyIDField: "map-key"}
	if got := ExtractResourceID("calls", obj); got != "real-id" {
		t.Fatalf("a real id field must win over the stamped map key, got %q", got)
	}
	if got := extractObjectID(obj); got != "real-id" {
		t.Fatalf("a real id field must win over the stamped map key, got %q", got)
	}
}

// A detail object whose fields happen to hold sub-objects is the shape most at
// risk of being misread as a collection. Field names are words, record keys are
// not, and that is the whole discriminator.
func TestMapKeyedRejectsOrdinaryNestedObjects(t *testing.T) {
	rejected := []string{
		` + "`" + `{"user":{"id":"u1"},"account":{"id":"a1"}}` + "`" + `,
		` + "`" + `{"metadata":{"created":"now"}}` + "`" + `,
		` + "`" + `{"line_items":{"sku":"a"},"shipping_address":{"zip":"1"}}` + "`" + `,
		` + "`" + `{"oauth2":{"scope":"read"}}` + "`" + `,
		` + "`" + `{"id":"7788990011223344","title":"a detail record"}` + "`" + `,
		` + "`" + `{"2026-08-19":{"id":"x"},"total":3}` + "`" + `,
		` + "`" + `{}` + "`" + `,
		` + "`" + `[{"id":"1"}]` + "`" + `,
	}
	for _, payload := range rejected {
		if _, ok := FlattenMapKeyedCollection(json.RawMessage(payload)); ok {
			t.Fatalf("payload must not be treated as a map-keyed collection: %s", payload)
		}
	}
}

func TestMapKeyedAcceptsOpaqueAndUUIDKeys(t *testing.T) {
	accepted := []string{
		` + "`" + `{"d41d8cd98f00b204e9800998ecf8427e":{"n":1}}` + "`" + `,
		` + "`" + `{"3f2504e0-4f89-11d3-9a0c-0305e82c3301":{"n":1}}` + "`" + `,
		` + "`" + `{"-MxYz1234abcdEFGHijk":{"n":1}}` + "`" + `,
	}
	for _, payload := range accepted {
		items, ok := FlattenMapKeyedCollection(json.RawMessage(payload))
		if !ok || len(items) != 1 {
			t.Fatalf("payload should flatten to one item: %s (ok=%v len=%d)", payload, ok, len(items))
		}
	}
}

// A bucket holding no records is an empty page, not a row keyed by the bucket.
func TestMapKeyedEmptyBucketYieldsNoRows(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"2026-08-19":{}}` + "`" + `))
	if !ok {
		t.Fatalf("an empty bucket is still a recognized collection")
	}
	if len(items) != 0 {
		t.Fatalf("an empty bucket must yield no rows, got %d", len(items))
	}
}

func TestMapKeyedUpsertBatchPersistsRows(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/map-keyed.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"2026-08-19":{"7788990011223344":{"title":"no id field"}}}` + "`" + `))
	if !ok {
		t.Fatalf("expected a recognized map-keyed collection")
	}
	if _, _, err := db.UpsertBatch("calls", items); err != nil {
		t.Fatalf("upsert batch: %v", err)
	}
	ids, err := db.ListIDs("calls")
	if err != nil {
		t.Fatalf("list ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "7788990011223344" {
		t.Fatalf("expected the map key to become the row id, got %#v", ids)
	}
}
`

// mapKeyedCLITestSource asserts the sync extractor and the live write-through
// path pull the same records out of the same map-keyed payloads.
const mapKeyedCLITestSource = `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestMapKeyedExtractPageItems(t *testing.T) {
	cases := []struct {
		name      string
		payload   string
		paths     []string
		wantCount int
	}{
		{
			name:      "wrapper key holds a bucketed collection",
			payload:   ` + "`" + `{"results":{"2026-08-19":{"7788990011223344":{"id":"7788990011223344"},"1122334455667788":{"id":"1122334455667788"}}}}` + "`" + `,
			wantCount: 2,
		},
		{
			name:      "wrapper key holds a flat id-keyed collection",
			payload:   ` + "`" + `{"data":{"7788990011223344":{"id":"7788990011223344"}}}` + "`" + `,
			wantCount: 1,
		},
		{
			name:      "payload is the collection itself",
			payload:   ` + "`" + `{"7788990011223344":{"id":"7788990011223344"}}` + "`" + `,
			wantCount: 1,
		},
		{
			name:      "payload is a bucketed collection",
			payload:   ` + "`" + `{"2026-08-19":{"7788990011223344":{"id":"7788990011223344"}}}` + "`" + `,
			wantCount: 1,
		},
		{
			name:      "declared response path resolves to a collection",
			payload:   ` + "`" + `{"payload":{"7788990011223344":{"id":"7788990011223344"}}}` + "`" + `,
			paths:     []string{"payload"},
			wantCount: 1,
		},
		{
			name:      "collection under a resource-named key beside scalar metadata",
			payload:   ` + "`" + `{"calls_by_day":{"2026-08-19":{"7788990011223344":{"id":"7788990011223344"}}},"total":1}` + "`" + `,
			wantCount: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, _, _ := extractPageItems(json.RawMessage(tc.payload), "cursor", tc.paths...)
			if len(items) != tc.wantCount {
				t.Fatalf("expected %d items, got %d", tc.wantCount, len(items))
			}
		})
	}
}

// An array-shaped payload must keep taking the array path even when a
// map-keyed strategy could also match something in the envelope.
func TestMapKeyedDoesNotPreemptArrayExtraction(t *testing.T) {
	items, _, _ := extractPageItems(json.RawMessage(` + "`" + `{"results":[{"id":"a"},{"id":"b"}],"index":{"7788990011223344":{"id":"a"}}}` + "`" + `), "cursor")
	if len(items) != 2 {
		t.Fatalf("array wrapper must win, got %d items", len(items))
	}
}

func TestMapKeyedExtractPageItemsRejectsDetailObject(t *testing.T) {
	items, _, _ := extractPageItems(json.RawMessage(` + "`" + `{"user":{"id":"u1"},"account":{"id":"a1"}}` + "`" + `), "cursor")
	if len(items) != 0 {
		t.Fatalf("a detail object with nested sub-objects must not be flattened, got %d items", len(items))
	}
}

// The live path and sync must agree: both cache the records, not the envelope.
func TestMapKeyedWriteThroughCachesRecords(t *testing.T) {
	// Redirect every path root the store resolver consults, not just HOME, so
	// the test cannot reach a real profile on a machine that sets XDG vars.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	ctx := context.Background()
	writeThroughCache(ctx, "calls", json.RawMessage(` + "`" + `{"results":{"2026-08-19":{"7788990011223344":{"id":"7788990011223344","title":"a"},"1122334455667788":{"id":"1122334455667788","title":"b"}}}}` + "`" + `))
	writeThroughCache(ctx, "notes", json.RawMessage(` + "`" + `{"7788990011223344":{"title":"no id field"}}` + "`" + `))

	db, err := openStoreForRead(ctx, "map-keyed-pp-cli")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if db == nil {
		t.Fatalf("expected a store after write-through cache")
	}
	defer db.Close()

	calls, err := db.List("calls", 10)
	if err != nil {
		t.Fatalf("list calls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 cached call rows, got %d", len(calls))
	}

	noteIDs, err := db.ListIDs("notes")
	if err != nil {
		t.Fatalf("list note ids: %v", err)
	}
	if len(noteIDs) != 1 || noteIDs[0] != "7788990011223344" {
		t.Fatalf("expected the map key to key the id-less record, got %#v", noteIDs)
	}
}
`

// TestMapKeyedCollectionExtraction covers a collection that files each record
// under its identifier instead of listing records in an array, at both nesting
// depths, across the three surfaces that have to agree on it: the sync
// extractor, the live write-through path, and the store's id resolvers.
func TestMapKeyedCollectionExtraction(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("map-keyed")
	outputDir := filepath.Join(t.TempDir(), "map-keyed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "store", "map_keyed_collection_test.go"),
		[]byte(mapKeyedStoreTestSource), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "map_keyed_collection_test.go"),
		[]byte(mapKeyedCLITestSource), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/store", "./internal/cli", "-run", "TestMapKeyed", "-count=1")
}
