package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrappedArrayProjectionHelpers_EmittedRuntime(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("wrapped-projection")
	outputDir := filepath.Join(t.TempDir(), "wrapped-projection-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "wrapped_projection_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func decodeObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid object JSON: %v\n%s", err, raw)
	}
	return out
}

func TestCompactFieldsProjectsWrappedArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"meta": {"total": 2},
		"results": [
			{"id":"a","name":"Alpha","description":"verbose","body":"payload","_links":{"self":"/a"}},
			{"id":"b","name":"Beta","description":"verbose","body":"payload","_links":{"self":"/b"}}
		]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	results, ok := got["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %#v, want two compacted items", got["results"])
	}
	first := results[0].(map[string]any)
	if first["id"] != "a" || first["name"] != "Alpha" {
		t.Fatalf("first compact row lost identity fields: %#v", first)
	}
	for _, key := range []string{"description", "body", "_links"} {
		if _, ok := first[key]; ok {
			t.Fatalf("compact wrapped item kept noisy %s field: %#v", key, first)
		}
	}
	if _, ok := got["meta"].(map[string]any); !ok {
		t.Fatalf("compact should preserve envelope metadata, got %#v", got)
	}
}

func TestCompactFieldsProjectsDomainSpecificArrayWrapper(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"request_id": "req-1",
		"widgets": [
			{"widget_id":"w1","label":"One","description":"verbose"},
			{"widget_id":"w2","label":"Two","description":"verbose"}
		]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	widgets, ok := got["widgets"].([]any)
	if !ok || len(widgets) != 2 {
		t.Fatalf("widgets = %#v, want two compacted items", got["widgets"])
	}
	first := widgets[0].(map[string]any)
	if first["widget_id"] != "w1" || first["label"] != "One" {
		t.Fatalf("domain wrapper lost frequent identity fields: %#v", first)
	}
	if _, ok := first["description"]; ok {
		t.Fatalf("domain wrapper kept verbose description: %#v", first)
	}
	if got["request_id"] != "req-1" {
		t.Fatalf("compact should preserve scalar envelope metadata, got %#v", got["request_id"])
	}
}

func TestCompactFieldsProjectsNestedDomainSpecificArrayWrapper(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"meta": {"total": 1},
		"links": {"next": "/artists?page=2"},
		"artists": {
			"items": [
				{"id":"artist-1","name":"Radiohead","description":"verbose"}
			]
		}
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	meta, ok := got["meta"].(map[string]any)
	if !ok || meta["total"] != float64(1) {
		t.Fatalf("compact should preserve nested envelope metadata, got %#v", got["meta"])
	}
	links, ok := got["links"].(map[string]any)
	if !ok || links["next"] != "/artists?page=2" {
		t.Fatalf("compact should preserve links metadata, got %#v", got["links"])
	}
	artists, ok := got["artists"].(map[string]any)
	if !ok {
		t.Fatalf("artists = %#v, want nested object envelope", got["artists"])
	}
	items, ok := artists["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("artists.items = %#v, want one item", artists["items"])
	}
	first := items[0].(map[string]any)
	if first["id"] != "artist-1" || first["name"] != "Radiohead" {
		t.Fatalf("nested compact row lost identity fields: %#v", first)
	}
	if _, ok := first["description"]; ok {
		t.Fatalf("nested compact row kept verbose description: %#v", first)
	}
}

func TestCompactFieldsPreservesNestedDetailPayload(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"data": {
			"id": "issue-1",
			"description": "actual answer",
			"tags": [{"id": "tag-1", "label": "bug"}]
		}
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	data, ok := got["data"].(map[string]any)
	if !ok || data["description"] != "actual answer" {
		t.Fatalf("nested detail payload was stripped: %#v", got["data"])
	}
}

func TestCompactFieldsStopsAtDepthBound(t *testing.T) {
	var input strings.Builder
	for i := 0; i < 33; i++ {
		fmt.Fprintf(&input, `+"`"+`{"level%d":`+"`"+`, i)
	}
	input.WriteString(`+"`"+`{"items":[{"id":"artist-1","description":"verbose"}]}`+"`"+`)
	for i := 0; i < 33; i++ {
		input.WriteByte('}')
	}

	current := decodeObject(t, compactFields(json.RawMessage(input.String())))
	for i := 0; i < 33; i++ {
		var ok bool
		current, ok = current[fmt.Sprintf("level%d", i)].(map[string]any)
		if !ok {
			t.Fatalf("level%d was not preserved while depth bound stopped descent", i)
		}
	}
	items, ok := current["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("deep items = %#v, want one unprojected item", current["items"])
	}
	if items[0].(map[string]any)["description"] != "verbose" {
		t.Fatalf("overly deep compact envelope was unexpectedly traversed: %#v", items[0])
	}
}

func TestCompactFieldsProjectsHALEmbeddedArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"_embedded": {
			"events": [
				{"id":"evt-1","name":"Launch","description":"verbose","_links":{"self":{"href":"/events/evt-1"}}}
			]
		},
		"_links": {"next": {"href": "/events?cursor=next"}}
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	embedded := got["_embedded"].(map[string]any)
	events := embedded["events"].([]any)
	first := events[0].(map[string]any)
	if first["id"] != "evt-1" || first["name"] != "Launch" {
		t.Fatalf("HAL embedded row lost identity fields: %#v", first)
	}
	for _, key := range []string{"description", "_links"} {
		if _, ok := first[key]; ok {
			t.Fatalf("HAL embedded compact row kept noisy %s field: %#v", key, first)
		}
	}
}

func TestCompactFieldsPreservesDocumentedSparseFields(t *testing.T) {
	input := json.RawMessage(`+"`"+`[
		{"id":"row-1","name":"One","odds":1.5,"ranking":7,"lean":"up","segment":"rare","undocumented":"drop"},
		{"id":"row-2","name":"Two"},
		{"id":"row-3","name":"Three"},
		{"id":"row-4","name":"Four"},
		{"id":"row-5","name":"Five"},
		{"id":"row-6","name":"Six"},
		{"id":"row-7","name":"Seven"},
		{"id":"row-8","name":"Eight"},
		{"id":"row-9","name":"Nine"},
		{"id":"row-10","name":"Ten"}
	]`+"`"+`)

	got := compactFields(input, map[string]bool{
		"odds": true, "ranking": true, "lean": true, "segment": true,
	})
	var rows []map[string]any
	if err := json.Unmarshal(got, &rows); err != nil {
		t.Fatalf("compactFields returned invalid JSON: %v\n%s", err, got)
	}
	first := rows[0]
	for _, key := range []string{"odds", "ranking", "lean", "segment"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("documented sparse field %q was dropped: %#v", key, first)
		}
	}
	if _, ok := first["undocumented"]; ok {
		t.Fatalf("undocumented sparse field survived compact projection: %#v", first)
	}
}

func TestCompactFieldsPreservesSoleVerbosePayloadArray(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"id": "issue-1",
		"title": "Bug",
		"description": "verbose metadata",
		"comments": [{"id":"comment-1","body":"comment payload"}]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	if _, ok := got["comments"].([]any); !ok {
		t.Fatalf("sole payload comments array was stripped: %#v", got)
	}
	if _, ok := got["description"]; ok {
		t.Fatalf("scalar description should remain blocklisted: %#v", got)
	}
}

func TestCompactFieldsStripsVerboseArrayWhenPayloadCompetes(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"id": "issue-1",
		"body": "primary payload",
		"comments": [{"id":"comment-1","body":"comment payload"}],
		"tags": [{"id":"tag-1","label":"bug"}]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	if _, ok := got["comments"]; ok {
		t.Fatalf("verbose competing comments array survived: %#v", got)
	}
	if _, ok := got["tags"].([]any); !ok {
		t.Fatalf("domain payload tags array was stripped: %#v", got)
	}
}

func TestCompactFieldsPreservesFetchFailuresAlongsidePayload(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"results": [{"id":"evt-1","name":"Launch","description":"verbose"}],
		"fetch_failures": [{"resource":"venues","error":"timeout"}]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	if _, ok := got["results"].([]any); !ok {
		t.Fatalf("results payload was stripped: %#v", got)
	}
	if _, ok := got["fetch_failures"].([]any); !ok {
		t.Fatalf("fetch_failures sidecar was stripped: %#v", got)
	}
}

func TestCompactFieldsKeepsObjectBlocklistForDetailObjectsWithArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"id": "issue-1",
		"title": "Bug",
		"description": "verbose metadata",
		"body": "primary payload",
		"comments": [
			{"id":"comment-1","body":"verbose comment"}
		],
		"tags": [{"id":"tag-1","label":"bug"}]
	}`+"`"+`)

	got := decodeObject(t, compactFields(input))
	if got["id"] != "issue-1" || got["title"] != "Bug" {
		t.Fatalf("detail object lost identity fields: %#v", got)
	}
	if got["body"] != "primary payload" {
		t.Fatalf("detail object should preserve primary body payload: %#v", got)
	}
	for _, key := range []string{"description", "comments"} {
		if _, ok := got[key]; ok {
			t.Fatalf("detail object kept object-path blocklisted %s field: %#v", key, got)
		}
	}
	if _, ok := got["tags"].([]any); !ok {
		t.Fatalf("detail object lost competing payload tags: %#v", got)
	}
}

func TestSelectFieldsProjectsWrappedSidecarArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"warnings": [{"code":"W1","message":"rate limited"}],
		"results": [{"id":"evt-1","name":"Launch","description":"verbose"}]
	}`+"`"+`)

	got := decodeObject(t, filterFields(input, "id"))
	warnings := got["warnings"].([]any)
	warning := warnings[0].(map[string]any)
	if warning["code"] != "W1" || warning["message"] != "rate limited" {
		t.Fatalf("select should preserve sidecar warning fields: %#v", warning)
	}
	results := got["results"].([]any)
	first := results[0].(map[string]any)
	if first["id"] != "evt-1" {
		t.Fatalf("select lost requested result id: %#v", first)
	}
	for _, key := range []string{"name", "description"} {
		if _, ok := first[key]; ok {
			t.Fatalf("select kept unrequested result %s field: %#v", key, first)
		}
	}
}

func TestSelectFieldsProjectsHALEmbeddedArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"_embedded": {
			"events": [
				{"id":"evt-1","name":"Launch","description":"verbose"}
			]
		},
		"_links": {"next": {"href": "/events?cursor=next"}}
	}`+"`"+`)

	got := decodeObject(t, filterFields(input, "id,name"))
	embedded := got["_embedded"].(map[string]any)
	events := embedded["events"].([]any)
	first := events[0].(map[string]any)
	if first["id"] != "evt-1" || first["name"] != "Launch" {
		t.Fatalf("HAL select lost requested fields: %#v", first)
	}
	if _, ok := first["description"]; ok {
		t.Fatalf("HAL select kept unrequested description: %#v", first)
	}
}
`), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestCompactFieldsProjects|TestCompactFieldsPreserves|TestCompactFieldsStrips|TestCompactFieldsKeeps|TestSelectFieldsProjects", "-count=1")
}
