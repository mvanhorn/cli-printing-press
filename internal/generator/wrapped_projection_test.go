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

func TestCompactFieldsKeepsObjectBlocklistForDetailObjectsWithArrays(t *testing.T) {
	input := json.RawMessage(`+"`"+`{
		"id": "issue-1",
		"title": "Bug",
		"description": "verbose metadata",
		"body": "primary payload",
		"comments": [
			{"id":"comment-1","body":"verbose comment"}
		]
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
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestCompactFieldsProjects|TestCompactFieldsKeeps|TestSelectFieldsProjects", "-count=1")
}
