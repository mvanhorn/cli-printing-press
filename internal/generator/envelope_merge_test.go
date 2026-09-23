package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedEnvelopeMergeKeepsCommandMeta(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "envmerge",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/envmerge-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "func mergeCommandMeta(")
	require.Contains(t, helpers, "mergeCommandMeta(existing, merged)")
	require.Contains(t, helpers, "mergeCommandMeta(existing, meta)")
	require.Contains(t, helpers, "mergeCommandMeta(meta, platformMeta)")
	require.Contains(t, helpers, "return \"computed\"")
	require.NotContains(t, helpers, "case \"local\", \"computed\":")

	requireGeneratedCompiles(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "envelope_merge_runtime_test.go"),
		[]byte(envelopeMergeRuntimeTest(generatedModulePath(t, outputDir))),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestEnvelopeMerge$", "-count=1")
}

func envelopeMergeRuntimeTest(modulePath string) string {
	return `package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"` + modulePath + `/internal/platform"
	"github.com/spf13/cobra"
)

func TestEnvelopeMerge(t *testing.T) {
	owned := []byte("{\"meta\":{\"source\":\"catalogue\",\"rows\":104},\"results\":[{\"id\":\"cov\"}]}")

	t.Run("wrapAgentOutput keeps command meta and records the other axis", func(t *testing.T) {
		out := mustWrapAgent(t, owned, map[string]any{"source": "local"})
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "catalogue" || meta["transport"] != "local" || meta["rows"] != 104.0 {
			t.Fatalf("meta = %#v", meta)
		}
		assertRows(t, results, "cov")
	})

	t.Run("wrapAgentOutput wraps a bare payload", func(t *testing.T) {
		out := mustWrapAgent(t, []byte("[{\"id\":\"bare\"}]"), map[string]any{"source": "local"})
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "local" {
			t.Fatalf("meta = %#v", meta)
		}
		if _, ok := meta["transport"]; ok {
			t.Fatalf("bare payload invented a transport axis: %#v", meta)
		}
		assertRows(t, results, "bare")
	})

	t.Run("same axis keeps the command source", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"live\"},\"results\":[{\"id\":\"live\"}]}")
		out := mustWrapAgent(t, body, map[string]any{"source": "local"})
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "live" {
			t.Fatalf("meta = %#v", meta)
		}
		if _, ok := meta["transport"]; ok {
			t.Fatalf("same-axis wrapper source leaked: %#v", meta)
		}
		assertRows(t, results, "live")
	})

	t.Run("live command keeps catalogue as data_origin", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"live\"},\"results\":[{\"id\":\"live\"}]}")
		out := mustWrapAgent(t, body, map[string]any{"source": "catalogue"})
		meta, _ := mustSingleEnvelope(t, out)
		if meta["source"] != "live" || meta["data_origin"] != "catalogue" {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("computed stays computed beside live transport", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"computed\"},\"results\":[{\"id\":\"n\"}]}")
		out := mustWrapAgent(t, body, map[string]any{"source": "live"})
		meta, _ := mustSingleEnvelope(t, out)
		if meta["source"] != "computed" || meta["transport"] != "live" {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("unclassified command source drops the wrapper source", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"warehouse\"},\"results\":[{\"id\":\"w\"}]}")
		out := mustWrapAgent(t, body, map[string]any{"source": "live"})
		meta, _ := mustSingleEnvelope(t, out)
		if meta["source"] != "warehouse" {
			t.Fatalf("meta = %#v", meta)
		}
		if _, ok := meta["transport"]; ok {
			t.Fatalf("unclassified source collapsed into transport: %#v", meta)
		}
	})

	t.Run("empty wrapper source defaults a bare payload to local", func(t *testing.T) {
		out := mustWrapAgent(t, []byte("[{\"id\":\"bare\"}]"), map[string]any{"source": "  "})
		meta, _ := mustSingleEnvelope(t, out)
		if meta["source"] != "local" {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("results-only object still flattens when live", func(t *testing.T) {
		out := mustWrapAgent(t, []byte("{\"results\":[{\"id\":\"flat\"}]}"), map[string]any{"source": "live"})
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "live" {
			t.Fatalf("meta = %#v", meta)
		}
		assertRows(t, results, "flat")
	})

	t.Run("wrapWithProvenance keeps catalogue and fills reason", func(t *testing.T) {
		out, err := wrapWithProvenance(owned, DataProvenance{Source: "live", Reason: "api"})
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "catalogue" || meta["transport"] != "live" || meta["reason"] != "api" || meta["rows"] != 104.0 {
			t.Fatalf("meta = %#v", meta)
		}
		assertRows(t, results, "cov")
	})

	t.Run("wrapWithProvenance wraps a bare array", func(t *testing.T) {
		out, err := wrapWithProvenance([]byte("[{\"id\":\"bare\"}]"), DataProvenance{Source: "live"})
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "live" {
			t.Fatalf("meta = %#v", meta)
		}
		assertRows(t, results, "bare")
	})

	t.Run("wrapWithProvenance flattens a results-only object", func(t *testing.T) {
		out, err := wrapWithProvenance([]byte("{\"results\":[{\"id\":\"flat\"}]}"), DataProvenance{Source: "live"})
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "live" {
			t.Fatalf("meta = %#v", meta)
		}
		assertRows(t, results, "flat")
	})

	t.Run("extra keys are not an owned envelope", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"catalogue\"},\"results\":[{\"id\":\"cov\"}],\"next\":\"x\"}")
		out, err := wrapWithProvenance(body, DataProvenance{Source: "live"})
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "live" {
			t.Fatalf("wrapper source lost: %#v", meta)
		}
		var inner map[string]any
		if err := json.Unmarshal(results, &inner); err != nil {
			t.Fatalf("extra-key payload should stay nested: %v %s", err, results)
		}
		innerMeta, _ := inner["meta"].(map[string]any)
		if innerMeta["source"] != "catalogue" {
			t.Fatalf("inner meta = %#v", inner)
		}
	})

	t.Run("platform merge keeps command source", func(t *testing.T) {
		flags := &rootFlags{platformSession: &platform.Session{}}
		out, err := wrapPlatformStructuredOutput(owned, flags, "results", true)
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, out)
		if meta["source"] != "catalogue" || meta["rows"] != 104.0 {
			t.Fatalf("meta = %#v", meta)
		}
		truncated, ok := meta["truncated"].(bool)
		if !ok || truncated {
			t.Fatalf("truncated = %#v, want false", meta["truncated"])
		}
		assertRows(t, results, "cov")
	})

	t.Run("platform merge false still nests the payload", func(t *testing.T) {
		flags := &rootFlags{platformSession: &platform.Session{}}
		out, err := wrapPlatformStructuredOutput(owned, flags, "data", false)
		if err != nil {
			t.Fatal(err)
		}
		meta, results := mustEnvelope(t, out, "data")
		if _, ok := meta["source"]; ok {
			t.Fatalf("merge-false promoted command source: %#v", meta)
		}
		var inner map[string]any
		if err := json.Unmarshal(results, &inner); err != nil {
			t.Fatalf("nested payload: %v %s", err, results)
		}
		innerMeta, _ := inner["meta"].(map[string]any)
		if innerMeta["source"] != "catalogue" {
			t.Fatalf("inner = %#v", inner)
		}
	})

	t.Run("command truncated wins over platform", func(t *testing.T) {
		body := []byte("{\"meta\":{\"source\":\"catalogue\",\"truncated\":true},\"results\":[{\"id\":\"cov\"}]}")
		flags := &rootFlags{platformSession: &platform.Session{}}
		out, err := wrapPlatformStructuredOutput(body, flags, "results", true)
		if err != nil {
			t.Fatal(err)
		}
		meta, _ := mustSingleEnvelope(t, out)
		truncated, ok := meta["truncated"].(bool)
		if meta["source"] != "catalogue" || !ok || !truncated {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("printOutputWithFlags keeps measured origin", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &rootFlags{agent: true, asJSON: true}
		if err := printOutputWithFlags(&buf, owned, flags); err != nil {
			t.Fatal(err)
		}
		meta, results := mustSingleEnvelope(t, buf.Bytes())
		if meta["source"] != "catalogue" || meta["rows"] != 104.0 {
			t.Fatalf("meta = %#v", meta)
		}
		if _, ok := meta["transport"]; ok {
			t.Fatalf("resolved source must not be rewritten as transport: %#v", meta)
		}
		assertRows(t, results, "cov")
	})

	t.Run("printOutputWithFlags dry-run stays beside catalogue", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &rootFlags{agent: true, asJSON: true, dryRun: true}
		if err := printOutputWithFlags(&buf, owned, flags); err != nil {
			t.Fatal(err)
		}
		meta, _ := mustSingleEnvelope(t, buf.Bytes())
		if meta["source"] != "catalogue" || meta["transport"] != "dry-run" {
			t.Fatalf("meta = %#v", meta)
		}
	})

	t.Run("declared computed is not local", func(t *testing.T) {
		cmd := &cobra.Command{Annotations: map[string]string{"pp:data-source": "computed"}}
		if got := declaredAgentSource(cmd, &rootFlags{}); got != "computed" {
			t.Fatalf("declaredAgentSource = %q", got)
		}
	})
}

func mustWrapAgent(t *testing.T, data []byte, meta map[string]any) []byte {
	t.Helper()
	out, err := wrapAgentOutput(data, meta)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustSingleEnvelope(t *testing.T, raw []byte) (map[string]any, json.RawMessage) {
	t.Helper()
	return mustEnvelope(t, raw, "results")
}

func mustEnvelope(t *testing.T, raw []byte, resultKey string) (map[string]any, json.RawMessage) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("envelope: %v\n%s", err, raw)
	}
	if len(payload) != 2 {
		t.Fatalf("envelope keys = %d, want 2: %s", len(payload), raw)
	}
	var meta map[string]any
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta: %v\n%s", err, raw)
	}
	body, ok := payload[resultKey]
	if !ok {
		t.Fatalf("missing %s: %s", resultKey, raw)
	}
	return meta, body
}

func assertRows(t *testing.T, raw []byte, id string) {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("results not a row array: %v %s", err, raw)
	}
	if len(rows) != 1 || rows[0]["id"] != id {
		t.Fatalf("rows = %#v", rows)
	}
}
`
}
