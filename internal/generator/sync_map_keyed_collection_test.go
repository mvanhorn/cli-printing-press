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

// A one-level date-keyed collection uses the date as the record id. That
// string is a valid collection key, so the last-resort stamp must keep it.
// CanonicalResourceID refuses ISO dates (a created_at-shaped field is not a
// row id); running the stamp through that helper would drop the row.
func TestMapKeyedOneLevelDateKeySurvivesUnusableIDGate(t *testing.T) {
	if got := CanonicalResourceID("2026-08-19"); got != "" {
		t.Fatalf("CanonicalResourceID must still refuse ISO dates, got %q", got)
	}
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"2026-08-19":{"title":"day bucket"}}` + "`" + `))
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 date-keyed item, ok=%v len=%d", ok, len(items))
	}
	var obj map[string]any
	if err := json.Unmarshal(items[0], &obj); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if obj[MapKeyIDField] != "2026-08-19" {
		t.Fatalf("expected the date key stamped, got %#v", obj)
	}
	if got := ExtractResourceID("calls", obj); got != "2026-08-19" {
		t.Fatalf("date-shaped map keys must resolve via ResourceIDString, got %q", got)
	}
	if got := extractObjectID(obj); got != "2026-08-19" {
		t.Fatalf("extractObjectID must keep a date-shaped map key, got %q", got)
	}

	// A leaf that also copies the date into id is refused by the
	// canonical chain; the stamp is the arm that recovers it.
	obj["id"] = "2026-08-19"
	if got := ExtractResourceID("calls", obj); got != "2026-08-19" {
		t.Fatalf("mapKeyIDFallback must recover after CanonicalResourceID refuses the date-shaped id field, got %q", got)
	}
	if got := extractObjectID(obj); got != "2026-08-19" {
		t.Fatalf("extractObjectID must recover a date-shaped id via the stamped key, got %q", got)
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
		` + "`" + `{"7788990011223344":{"id":"x"},"meta":{"total":3}}` + "`" + `,
		` + "`" + `{"id":"detail-1","title":"Alice","tags":["a"],"7788990011223344":{"role":"admin"}}` + "`" + `,
		` + "`" + `{"total":3,"next_cursor":"c2"}` + "`" + `,
		` + "`" + `{"7788990011223344":"not an object"}` + "`" + `,
		` + "`" + `{}` + "`" + `,
		` + "`" + `[{"id":"1"}]` + "`" + `,
	}
	for _, payload := range rejected {
		if _, ok := FlattenMapKeyedCollection(json.RawMessage(payload)); ok {
			t.Fatalf("payload must not be treated as a map-keyed collection: %s", payload)
		}
	}
}

// A detail object may carry one numeric-keyed child beside ordinary fields.
// Those siblings are not list metadata, so the child must not become the row.
func TestMapKeyedRejectsNumericChildOnDetailObject(t *testing.T) {
	payload := ` + "`" + `{"id":"detail-1","title":"Alice","tags":["a"],"7788990011223344":{"role":"admin"}}` + "`" + `
	if _, ok := FlattenMapKeyedCollection(json.RawMessage(payload)); ok {
		t.Fatalf("a detail object with one numeric-keyed child must not flatten: %s", payload)
	}
}

// Paging metadata filed beside the records must not cost the records: an API
// is free to mix a cursor into the collection object itself.
func TestMapKeyedCollectionKeepsRecordsBesideScalarMetadata(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"7788990011223344":{"id":"7788990011223344","title":"a"},"next_cursor":"c2","total":1}` + "`" + `))
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 item beside scalar metadata, ok=%v len=%d", ok, len(items))
	}
	var obj map[string]any
	if err := json.Unmarshal(items[0], &obj); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if obj["title"] != "a" {
		t.Fatalf("expected the record, got %#v", obj)
	}
	if _, leaked := obj["next_cursor"]; leaked {
		t.Fatalf("metadata must stay in the envelope, not ride along on the record: %#v", obj)
	}
}

// The pager needs the members the flattener skipped, so they come back beside
// the records instead of being dropped.
func TestMapKeyedFlattenReturnsSkippedMetadata(t *testing.T) {
	items, metadata, ok := FlattenMapKeyedCollectionWithMetadata(json.RawMessage(` + "`" + `{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"c2","total":1}` + "`" + `))
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 item, ok=%v len=%d", ok, len(items))
	}
	if string(metadata["next_cursor"]) != ` + "`" + `"c2"` + "`" + ` {
		t.Fatalf("expected the sibling cursor in the metadata return, got %#v", metadata)
	}
	if _, ok := metadata["total"]; !ok {
		t.Fatalf("every skipped member must come back, got %#v", metadata)
	}
}

// A bucket's own metadata fills gaps the outer level left, and never overrides it.
func TestMapKeyedBucketMetadataFillsGapsOnly(t *testing.T) {
	_, metadata, ok := FlattenMapKeyedCollectionWithMetadata(json.RawMessage(` + "`" + `{"2026-08-19":{"7788990011223344":{"id":"a"},"next_cursor":"inner","page":2},"next_cursor":"outer"}` + "`" + `))
	if !ok {
		t.Fatalf("bucketed collection with metadata at both levels should be recognized")
	}
	if string(metadata["next_cursor"]) != ` + "`" + `"outer"` + "`" + ` {
		t.Fatalf("the level closest to the caller must win, got %#v", metadata)
	}
	if string(metadata["page"]) != "2" {
		t.Fatalf("bucket metadata must fill what the outer level lacks, got %#v", metadata)
	}
}

// The enclosing key is the record's identity. A payload carrying the same field
// name would otherwise file the row under a value the API never keyed it by.
func TestMapKeyedStampOverwritesPayloadKeyField(t *testing.T) {
	items, ok := FlattenMapKeyedCollection(json.RawMessage(` + "`" + `{"7788990011223344":{"title":"no id field","_pp_map_key":"stale"}}` + "`" + `))
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 flattened item, ok=%v len=%d", ok, len(items))
	}
	var obj map[string]any
	if err := json.Unmarshal(items[0], &obj); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if obj[MapKeyIDField] != "7788990011223344" {
		t.Fatalf("the enclosing key must win over a payload field of the same name, got %#v", obj)
	}
	if got := ExtractResourceID("calls", obj); got != "7788990011223344" {
		t.Fatalf("id resolution must follow the enclosing key, got %q", got)
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
			name:      "records mixed with scalar metadata at the record level",
			payload:   ` + "`" + `{"7788990011223344":{"id":"7788990011223344"},"1122334455667788":{"id":"1122334455667788"},"next_cursor":"c2"}` + "`" + `,
			wantCount: 2,
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

// Recognizing the payload as a collection must not cost the continuation
// cursor sitting beside the records; dropping it would silently end the sync
// after one page.
func TestMapKeyedMixedMetadataKeepsCursor(t *testing.T) {
	items, cursor, hasMore := extractPageItems(json.RawMessage(` + "`" + `{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"c2"}` + "`" + `), "cursor")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if cursor != "c2" || !hasMore {
		t.Fatalf("expected the sibling cursor to survive extraction, got %q hasMore=%v", cursor, hasMore)
	}
}

// One case per site that lifts a map-keyed collection out of a sub-payload. A
// cursor filed beside the records belongs to the page those records came from,
// so it must outrank the envelope above them, and the envelope must still be
// the answer when the collection carries none.
func TestMapKeyedNestedCursorSurvives(t *testing.T) {
	cases := []struct {
		name       string
		payload    string
		paths      []string
		wantItems  int
		wantCursor string
	}{
		{
			name:       "declared response path holds the collection and its cursor",
			payload:    ` + "`" + `{"payload":{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"p1"}}` + "`" + `,
			paths:      []string{"payload"},
			wantItems:  1,
			wantCursor: "p1",
		},
		{
			name:       "wrapper key holds the collection and its cursor",
			payload:    ` + "`" + `{"data":{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"w1"}}` + "`" + `,
			wantItems:  1,
			wantCursor: "w1",
		},
		{
			name:       "resource-named sibling holds the collection and its cursor",
			payload:    ` + "`" + `{"calls_by_day":{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"s1"}}` + "`" + `,
			wantItems:  1,
			wantCursor: "s1",
		},
		{
			name:       "bucketed collection carries the cursor at bucket level",
			payload:    ` + "`" + `{"data":{"2026-08-19":{"7788990011223344":{"id":"7788990011223344"},"next_cursor":"b1"}}}` + "`" + `,
			wantItems:  1,
			wantCursor: "b1",
		},
		{
			name:       "envelope cursor still wins when the collection carries none",
			payload:    ` + "`" + `{"data":{"7788990011223344":{"id":"7788990011223344"}},"next_cursor":"o1"}` + "`" + `,
			wantItems:  1,
			wantCursor: "o1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, cursor, hasMore := extractPageItems(json.RawMessage(tc.payload), "cursor", tc.paths...)
			if len(items) != tc.wantItems {
				t.Fatalf("expected %d items, got %d", tc.wantItems, len(items))
			}
			if cursor != tc.wantCursor || !hasMore {
				t.Fatalf("expected cursor %q with hasMore, got %q hasMore=%v", tc.wantCursor, cursor, hasMore)
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

func TestMapKeyedExtractPageItemsRejectsNumericChildOnDetail(t *testing.T) {
	items, _, _ := extractPageItems(json.RawMessage(` + "`" + `{"id":"detail-1","title":"Alice","tags":["a"],"7788990011223344":{"role":"admin"}}` + "`" + `), "cursor")
	if len(items) != 0 {
		t.Fatalf("a detail object with one numeric-keyed child must not flatten, got %d items", len(items))
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
	writeThroughCache(ctx, "notes", json.RawMessage(` + "`" + `{"7788990011223344":{"title":"no id field"},"next_cursor":"c2"}` + "`" + `))

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

// A live detail response with one numeric-keyed child must cache the
// envelope, not the child. Flattening would drop title and key the row
// by the child's map key.
func TestMapKeyedWriteThroughLeavesDetailNumericChildIntact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	ctx := context.Background()
	writeThroughCache(ctx, "orders", json.RawMessage(` + "`" + `{"id":"detail-1","title":"Alice","tags":["a"],"7788990011223344":{"role":"admin"}}` + "`" + `))

	db, err := openStoreForRead(ctx, "map-keyed-pp-cli")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if db == nil {
		t.Fatalf("expected a store after write-through cache")
	}
	defer db.Close()

	ids, err := db.ListIDs("orders")
	if err != nil {
		t.Fatalf("list order ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "detail-1" {
		t.Fatalf("expected the detail object to stay the row, got %#v", ids)
	}
	rows, err := db.List("orders", 10)
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 cached detail row, got %d", len(rows))
	}
	var obj map[string]any
	if err := json.Unmarshal(rows[0], &obj); err != nil {
		t.Fatalf("decode cached row: %v", err)
	}
	if obj["title"] != "Alice" {
		t.Fatalf("detail fields must survive write-through, got %#v", obj)
	}
	if _, ok := obj["7788990011223344"]; !ok {
		t.Fatalf("the numeric-keyed child must remain on the detail row, got %#v", obj)
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
