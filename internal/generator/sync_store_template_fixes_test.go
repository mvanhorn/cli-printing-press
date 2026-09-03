package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncStoreBatch4TemplateFixes(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "batch4syncstore",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/batch4syncstore-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"currencies": {
				Description: "Currencies",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/currencies",
						Description: "List currencies",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
			"things": {
				Description: "Things",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/things",
						Description: "List things",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
			"parents": {
				Description: "Parents",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/parents",
						Description: "List parents",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
			"children": {
				Description: "Children",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/parents/{parentId}/children",
						Description: "List children",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "currencies", Path: "/currencies", Method: "GET"},
			{Name: "things", Path: "/things", Method: "GET"},
			{Name: "parents", Path: "/parents", Method: "GET"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{Name: "children", ParentResource: "parents", ParentIDParam: "parentId", Path: "/parents/{parentId}/children", Method: "GET"},
		},
	}
	require.NoError(t, gen.Generate())

	storeTest := `package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCurrencyCodeSuffixExtractsResourceID(t *testing.T) {
	if got := ExtractResourceID("spots", map[string]any{"_id": "mongo-1"}); got != "mongo-1" {
		t.Fatalf("MongoDB _id fallback = %q, want mongo-1", got)
	}
	if got := ExtractResourceID("spots", map[string]any{"_id": map[string]any{"$oid": "mongo-object-1"}, "name": "Jack's"}); got != "mongo-object-1" {
		t.Fatalf("MongoDB Extended JSON _id fallback = %q, want mongo-object-1", got)
	}
	if got := ExtractResourceID("spots", map[string]any{"id": "canonical-1", "name": "Jack's"}); got != "canonical-1" {
		t.Fatalf("stable id must win over display name, got %q", got)
	}

	obj := map[string]any{"currency_code": "USD", "name": "US Dollar"}
	if got := ExtractResourceID("currencies", obj); got != "USD" {
		t.Fatalf("resource-specific id must win over display name, got %q", got)
	}

	obj = map[string]any{"currency_code": "USD", "symbol": "$"}
	if got := ExtractResourceID("currencies", obj); got != "USD" {
		t.Fatalf("suffix fallback id = %q, want USD", got)
	}
	obj = map[string]any{"currency_code": nil, "symbol": "$"}
	if got := ExtractResourceID("currencies", obj); got != "" {
		t.Fatalf("nil suffix fallback id = %q, want empty", got)
	}
	if got := ExtractResourceID("get-currencies", map[string]any{"currency_code": "USD"}); got != "USD" {
		t.Fatalf("verb-prefixed suffix fallback id = %q, want USD", got)
	}
	if got := ExtractResourceID("things", map[string]any{"account_id": "acct-1"}); got != "" {
		t.Fatalf("foreign-key suffix fallback id = %q, want empty", got)
	}
	for i := 0; i < 50; i++ {
		got := ExtractResourceID("currencies", map[string]any{"currency_code": "USD", "region_id": "NA", "account_id": "x"})
		if got != "USD" {
			t.Fatalf("deterministic suffix fallback id on iteration %d = %q, want USD", i, got)
		}
	}

	// Soft-e plurals (#2713): the singular ends in a silent "e", so the plural
	// adds only "s" (cases->case, databases->database, licenses->license). The
	// id-base depluralizer must strip just "s", not "es", or the <singular>_id
	// probe misses and the row is silently dropped.
	if got := ExtractResourceID("cases", map[string]any{"case_id": "C-1"}); got != "C-1" {
		t.Fatalf("soft-e plural cases: id = %q, want C-1", got)
	}
	if got := ExtractResourceID("databases", map[string]any{"database_id": "DB-1"}); got != "DB-1" {
		t.Fatalf("soft-e plural databases: id = %q, want DB-1", got)
	}
	if got := ExtractResourceID("licenses", map[string]any{"license_key": "LK-1"}); got != "LK-1" {
		t.Fatalf("soft-e plural licenses: id = %q, want LK-1", got)
	}
	// Genuine "-es" plurals of a sibilant base still strip "es": classes->class
	// (sses), boxes->box (xes).
	if got := ExtractResourceID("classes", map[string]any{"class_id": "CL-1"}); got != "CL-1" {
		t.Fatalf("sses plural classes: id = %q, want CL-1", got)
	}
	if got := ExtractResourceID("boxes", map[string]any{"box_code": "BX-1"}); got != "BX-1" {
		t.Fatalf("xes plural boxes: id = %q, want BX-1", got)
	}

	db, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	items := []json.RawMessage{json.RawMessage(` + "`" + `{"currency_code":"EUR","symbol":"EUR"}` + "`" + `)}
	stored, failures, err := db.UpsertBatch("currencies", items)
	if err != nil {
		t.Fatalf("upsert batch: %v", err)
	}
	if stored != 1 || failures != 0 {
		t.Fatalf("stored/failures = %d/%d, want 1/0", stored, failures)
	}
	rows, err := db.List("currencies", 10)
	if err != nil {
		t.Fatalf("list currencies: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(rows))
	}

	items = []json.RawMessage{
		json.RawMessage(` + "`" + `{"_id":"spot-1","name":"Jack's"}` + "`" + `),
		json.RawMessage(` + "`" + `{"_id":"spot-2","name":"Jack's"}` + "`" + `),
		json.RawMessage(` + "`" + `{"_id":{"$oid":"spot-3"},"name":"Jack's"}` + "`" + `),
	}
	stored, failures, err = db.UpsertBatch("spots", items)
	if err != nil {
		t.Fatalf("upsert MongoDB rows: %v", err)
	}
	if stored != 3 || failures != 0 {
		t.Fatalf("MongoDB stored/failures = %d/%d, want 3/0", stored, failures)
	}
	rows, err = db.List("spots", 10)
	if err != nil {
		t.Fatalf("list spots: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("MongoDB rows = %d, want 3 distinct rows", len(rows))
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "store", "suffix_id_test.go"), []byte(storeTest), 0o644))

	cliTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type fixedBodyClient struct {
	body json.RawMessage
}

func (c fixedBodyClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	return c.body, nil
}

func (c fixedBodyClient) RateLimit() float64 {
	return 0
}

type pathBodyClient struct {
	bodies map[string]json.RawMessage
}

func (c pathBodyClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if body, ok := c.bodies[path]; ok {
		return body, nil
	}
	return json.RawMessage(` + "`" + `[]` + "`" + `), nil
}

func (c pathBodyClient) RateLimit() float64 {
	return 0
}

func TestSyncExtractIDSuffixFallbackIsGuarded(t *testing.T) {
	if got := extractID("currencies", map[string]any{"id": "id-wins", "currency_code": "USD"}); got != "id-wins" {
		t.Fatalf("exact id fallback should win, got %q", got)
	}
	if got := extractID("currencies", map[string]any{"currency_code": "USD"}); got != "USD" {
		t.Fatalf("suffix fallback id = %q, want USD", got)
	}
	if got := extractID("get-currencies", map[string]any{"currency_code": "USD"}); got != "USD" {
		t.Fatalf("verb-prefixed suffix fallback id = %q, want USD", got)
	}
	if got := extractID("things", map[string]any{"account_id": "acct-1"}); got != "" {
		t.Fatalf("foreign-key suffix fallback id = %q, want empty", got)
	}
	if got := extractID("get-expenses", map[string]any{"user_id": "u1"}); got != "" {
		t.Fatalf("verb-prefixed foreign-key suffix fallback id = %q, want empty", got)
	}
	for i := 0; i < 50; i++ {
		got := extractID("currencies", map[string]any{"currency_code": "USD", "region_id": "NA", "account_id": "x"})
		if got != "USD" {
			t.Fatalf("deterministic suffix fallback id on iteration %d = %q, want USD", i, got)
		}
	}
	if got := extractID("currencies", map[string]any{"currency_code": map[string]any{"nested": "USD"}}); got != "" {
		t.Fatalf("non-scalar suffix fallback id = %q, want empty", got)
	}
	// Soft-e plural depluralization on the sync side too (#2713): cases->case,
	// not cas — otherwise case_id never resolves and items are dropped.
	if got := extractID("cases", map[string]any{"case_id": "C-1"}); got != "C-1" {
		t.Fatalf("soft-e plural cases: extractID = %q, want C-1", got)
	}
	if got := extractID("databases", map[string]any{"database_id": "DB-1"}); got != "DB-1" {
		t.Fatalf("soft-e plural databases: extractID = %q, want DB-1", got)
	}
}

func TestExtractPageItemsDRFTopLevelNextURL(t *testing.T) {
	body := json.RawMessage(` + "`" + `{"count":2,"next":"http://h.example/api/things?cursor=abc","results":[{"id":"one"}]}` + "`" + `)
	items, cursor, hasMore := extractPageItems(body, "cursor")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if cursor != "abc" {
		t.Fatalf("cursor = %q, want abc", cursor)
	}
	if !hasMore {
		t.Fatalf("hasMore = false, want true")
	}
}

func TestExtractPageItemsDRFTopLevelRelativeNextURL(t *testing.T) {
	body := json.RawMessage(` + "`" + `{"count":2,"next":"/api/things?cursor=rel-abc","results":[{"id":"one"}]}` + "`" + `)
	items, cursor, hasMore := extractPageItems(body, "cursor")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if cursor != "rel-abc" {
		t.Fatalf("cursor = %q, want rel-abc", cursor)
	}
	if !hasMore {
		t.Fatalf("hasMore = false, want true")
	}
}

func TestExtractPageItemsBareTokenCursorUnaffected(t *testing.T) {
	body := json.RawMessage(` + "`" + `{"next_cursor":"bare-token","next":"http://h.example/api/things?cursor=url-token","results":[{"id":"one"}]}` + "`" + `)
	_, cursor, _ := extractPageItems(body, "cursor")
	if cursor != "bare-token" {
		t.Fatalf("cursor = %q, want bare-token", cursor)
	}
}

func TestSyncResourceNonJSONBodyEmitsAnomaly(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `<html><body>wrong app</body></html>` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Warn == nil {
		t.Fatalf("syncResource returned success for a non-JSON body; events: %s", events.String())
	}
	if !strings.Contains(events.String(), "\"reason\":\"non_json_200_body\"") {
		t.Fatalf("events did not contain non_json_200_body anomaly: %s", events.String())
	}
	if got := db.GetLastSyncedAt("things"); got != "" {
		t.Fatalf("stamped last_synced_at = %q, want empty", got)
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stored %d rows for a non-JSON body, want 0", len(rows))
	}
}

func TestSyncResourceValidEmptyJSONDoesNotEmitNonJSONAnomaly(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `[]` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if strings.Contains(events.String(), "\"reason\":\"non_json_200_body\"") {
		t.Fatalf("valid empty JSON emitted non_json_200_body anomaly: %s", events.String())
	}
}

func TestSyncResourceZeroStoredIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `[{"metadata":{"nested":true}}]` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for consumed-but-zero-stored page; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if !strings.Contains(events.String(), "\"reason\":\"all_items_failed_id_extraction\"") {
		t.Fatalf("events did not contain all_items_failed_id_extraction anomaly: %s", events.String())
	}
}

func TestSyncResourceDeclaredFailureIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"success":false,"data":null}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for declared-failure body; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if !strings.Contains(events.String(), "response declared failure") {
		t.Fatalf("events did not contain declared-failure sync error: %s", events.String())
	}
}

func TestSyncResourceDeclaredFailureWithItemsIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"success":false,"data":[{"id":"bad"}],"next_cursor":"still-more"}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for declared-failure body with items; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	if !strings.Contains(events.String(), "response declared failure") {
		t.Fatalf("events did not contain declared-failure sync error: %s", events.String())
	}
}

func TestSyncResourceStatusErrorWithItemsIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"status":"error","items":[{"id":"bad"}],"next_cursor":"still-more"}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for status=error body with items; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	if !strings.Contains(events.String(), "response declared failure") {
		t.Fatalf("events did not contain status=error sync error: %s", events.String())
	}
}

func TestSyncDependentResourceNonJSONBodyEmitsAnomaly(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("parents", "p1", []byte(` + "`" + `{"id":"p1"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	var events bytes.Buffer
	res := syncDependentResource(
		context.Background(),
		fixedBodyClient{body: json.RawMessage(` + "`" + `<html><body>wrong app</body></html>` + "`" + `)},
		db,
		dependentResourceDef{Name: "children", ParentTable: "parents", ParentIDParam: "parentId", PathTemplate: "/parents/{parentId}/children"},
		"", false, 1, false, false, nil, &events, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Warn == nil {
		t.Fatalf("syncDependentResource returned success for a non-JSON body; events: %s", events.String())
	}
	if !strings.Contains(events.String(), "\"reason\":\"non_json_200_body\"") {
		t.Fatalf("events did not contain dependent non_json_200_body anomaly: %s", events.String())
	}
	if got := db.GetLastSyncedAt("children"); got != "" {
		t.Fatalf("stamped last_synced_at = %q, want empty", got)
	}
}

func TestSyncDependentResourceMixedJSONAndNonJSONBodyWarnsWithoutStamp(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("parents", "p1", []byte(` + "`" + `{"id":"p1"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent p1: %v", err)
	}
	if err := db.Upsert("parents", "p2", []byte(` + "`" + `{"id":"p2"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent p2: %v", err)
	}

	var events bytes.Buffer
	res := syncDependentResource(
		context.Background(),
		pathBodyClient{bodies: map[string]json.RawMessage{
			"/parents/p1/children": json.RawMessage(` + "`" + `[{"id":"c1"}]` + "`" + `),
			"/parents/p2/children": json.RawMessage(` + "`" + `<html><body>wrong app</body></html>` + "`" + `),
		}},
		db,
		dependentResourceDef{Name: "children", ParentTable: "parents", ParentIDParam: "parentId", PathTemplate: "/parents/{parentId}/children"},
		"", false, 1, false, false, nil, &events, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Warn == nil {
		t.Fatalf("syncDependentResource returned success for mixed JSON/non-JSON parents; events: %s", events.String())
	}
	if !strings.Contains(events.String(), "\"reason\":\"non_json_200_body\"") {
		t.Fatalf("events did not contain dependent non_json_200_body anomaly: %s", events.String())
	}
	if got := db.GetLastSyncedAt("children"); got != "" {
		t.Fatalf("stamped last_synced_at = %q, want empty", got)
	}
	rows, err := db.List("children", 10)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored rows = %d, want 1 from the JSON parent", len(rows))
	}
}

func TestSyncDependentResourceDeclaredFailureWithItemsIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("parents", "p1", []byte(` + "`" + `{"id":"p1"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	var events bytes.Buffer
	res := syncDependentResource(
		context.Background(),
		fixedBodyClient{body: json.RawMessage(` + "`" + `{"success":false,"data":[{"id":"c1"}],"next_cursor":"still-more"}` + "`" + `)},
		db,
		dependentResourceDef{Name: "children", ParentTable: "parents", ParentIDParam: "parentId", PathTemplate: "/parents/{parentId}/children"},
		"", false, 1, false, false, nil, &events, 1,
	)
	if res.Err == nil {
		t.Fatalf("syncDependentResource returned clean success for declared-failure body with items; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	if !strings.Contains(events.String(), "response declared failure") {
		t.Fatalf("events did not contain dependent declared-failure sync error: %s", events.String())
	}
}

func TestSyncResourceOkFalseEnvelopeIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"ok":false,"error":"not_authed"}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for ok:false body; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	if !strings.Contains(events.String(), "response declared failure") {
		t.Fatalf("events did not contain ok:false sync error: %s", events.String())
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stored rows = %d, want 0 (ok:false body must not upsert)", len(rows))
	}
}

func TestSyncResourceOkFalseEnvelopeWithItemsIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"ok":false,"error":"not_authed","data":[{"id":"bad"}],"next_cursor":"still-more"}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for ok:false body with items; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stored rows = %d, want 0 (ok:false body must not upsert)", len(rows))
	}
}

func TestSyncResourcePascalOkFalseIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"Ok":false,"error":"not_authed"}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err == nil {
		t.Fatalf("syncResource returned clean success for Ok:false body; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stored rows = %d, want 0 (Ok:false body must not upsert)", len(rows))
	}
}

func TestSyncResourceOkTrueEnvelopeStoresItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `{"ok":true,"data":[{"id":"good-1"},{"id":"good-2"}]}` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error for ok:true body: %v; events: %s", res.Err, events.String())
	}
	if res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = true, want false")
	}
	if res.Count != 2 {
		t.Fatalf("Count = %d, want 2", res.Count)
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored rows = %d, want 2", len(rows))
	}
}

func TestSyncResourceWithoutOkFieldStillStores(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	var events bytes.Buffer
	res := syncResource(context.Background(), fixedBodyClient{body: json.RawMessage(` + "`" + `[{"id":"plain-1"},{"id":"plain-2"}]` + "`" + `)}, db, "things", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error for body without ok: %v; events: %s", res.Err, events.String())
	}
	if res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = true, want false")
	}
	if res.Count != 2 {
		t.Fatalf("Count = %d, want 2", res.Count)
	}
	rows, err := db.List("things", 10)
	if err != nil {
		t.Fatalf("list things: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored rows = %d, want 2", len(rows))
	}
}

func TestSyncDependentResourceOkFalseEnvelopeIsIntegrityFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("parents", "p1", []byte(` + "`" + `{"id":"p1"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	var events bytes.Buffer
	res := syncDependentResource(
		context.Background(),
		fixedBodyClient{body: json.RawMessage(` + "`" + `{"ok":false,"error":"not_authed","data":[{"id":"c1"}]}` + "`" + `)},
		db,
		dependentResourceDef{Name: "children", ParentTable: "parents", ParentIDParam: "parentId", PathTemplate: "/parents/{parentId}/children"},
		"", false, 1, false, false, nil, &events, 1,
	)
	if res.Err == nil {
		t.Fatalf("syncDependentResource returned clean success for ok:false body; events: %s", events.String())
	}
	if !res.IntegrityFailure {
		t.Fatalf("IntegrityFailure = false, want true")
	}
	if res.Count != 0 {
		t.Fatalf("Count = %d, want 0", res.Count)
	}
	rows, err := db.List("children", 10)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stored rows = %d, want 0 (ok:false body must not upsert)", len(rows))
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "batch4_sync_test.go"), []byte(cliTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "TestCurrencyCodeSuffixExtractsResourceID", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(SyncExtractIDSuffixFallbackIsGuarded|ExtractPageItemsDRFTopLevelNextURL|ExtractPageItemsDRFTopLevelRelativeNextURL|ExtractPageItemsBareTokenCursorUnaffected|SyncResourceNonJSONBodyEmitsAnomaly|SyncResourceValidEmptyJSONDoesNotEmitNonJSONAnomaly|SyncResourceZeroStoredIsIntegrityFailure|SyncResourceDeclaredFailure|SyncDependentResourceNonJSONBodyEmitsAnomaly|SyncDependentResourceMixedJSONAndNonJSONBodyWarnsWithoutStamp|SyncDependentResourceDeclaredFailure|SyncResourceOkFalseEnvelope|SyncResourcePascalOkFalse|SyncResourceOkTrueEnvelopeStoresItems|SyncResourceWithoutOkFieldStillStores|SyncDependentResourceOkFalseEnvelope)", "-count=1")
}
