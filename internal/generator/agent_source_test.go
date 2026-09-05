package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAgentSourceFollowsDeclaredProvenance(t *testing.T) {
	t.Parallel()

	serverSpec := minimalSpec("agent-source")
	serverSpec.Resources["items"].Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create item",
		Response:    spec.ResponseDef{Type: "object"},
	}
	outputDir := filepath.Join(t.TempDir(), "agent-source-pp-cli")
	gen := New(serverSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, Analytics: true}
	require.NoError(t, gen.Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "resolveAgentOutputSource(",
		"printOutputWithFlags must derive provenance instead of hardcoding local")
	require.NotContains(t, string(helpersSrc), `return printOutputWithFlagsMeta(w, data, flags, map[string]any{"source": "local"})`,
		"shared helper must not force local provenance")

	createSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_create.go"))
	require.NoError(t, err)
	require.Contains(t, string(createSrc), `map[string]any{"source": "live"}`,
		"mutation fallthrough must report live provenance")

	testPath := filepath.Join(outputDir, "internal", "cli", "agent_source_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrintJSONFilteredAgentDefaultsLocal(t *testing.T) {
	var out bytes.Buffer
	flags := &rootFlags{agent: true, asJSON: true, compact: true}
	if err := printJSONFiltered(&out, []map[string]any{{"id": "one"}}, flags); err != nil {
		t.Fatalf("printJSONFiltered returned error: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, out.String())
	}
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, out.String())
	}
	if meta.Source != "local" {
		t.Fatalf("unannotated typed output meta.source = %q, want local; output=%s", meta.Source, out.String())
	}
}

func TestPrintJSONFilteredAgentUsesDeclaredLiveSource(t *testing.T) {
	var out bytes.Buffer
	flags := &rootFlags{agent: true, asJSON: true, compact: true, agentSource: "live"}
	if err := printJSONFiltered(&out, []map[string]any{{"id": "one"}}, flags); err != nil {
		t.Fatalf("printJSONFiltered returned error: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, out.String())
	}
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, out.String())
	}
	if meta.Source != "live" {
		t.Fatalf("live-declared typed output meta.source = %q, want live; output=%s", meta.Source, out.String())
	}
}

func TestPrintOutputWithFlagsAgentPreservesProvenanceEnvelope(t *testing.T) {
	data := json.RawMessage(`+"`"+`{"results":[{"id":"one"}],"meta":{"source":"live","reason":"api"}}`+"`"+`)
	var out bytes.Buffer
	flags := &rootFlags{agent: true, asJSON: true}
	if err := printOutputWithFlags(&out, data, flags); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, out.String())
	}
	var meta struct {
		Source string `+"`json:\"source\"`"+`
		Reason string `+"`json:\"reason\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, out.String())
	}
	if meta.Source != "live" {
		t.Fatalf("provenance envelope meta.source = %q, want live; output=%s", meta.Source, out.String())
	}
	if meta.Reason != "api" {
		t.Fatalf("provenance envelope dropped reason: output=%s", out.String())
	}
	var results []map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must stay the inner collection: %v\n%s", err, out.String())
	}
	if len(results) != 1 || results[0]["id"] != "one" {
		t.Fatalf("results = %#v, want flattened live row; output=%s", results, out.String())
	}
}

func TestDeclaredAgentSourceLiveAnnotation(t *testing.T) {
	cmd := &cobra.Command{Annotations: map[string]string{"pp:data-source": "live"}}
	if got := declaredAgentSource(cmd, &rootFlags{}); got != "live" {
		t.Fatalf("declaredAgentSource live = %q, want live", got)
	}
	local := &cobra.Command{Annotations: map[string]string{"pp:data-source": "local"}}
	if got := declaredAgentSource(local, &rootFlags{}); got != "local" {
		t.Fatalf("declaredAgentSource local = %q, want local", got)
	}
	autoLive := &cobra.Command{Annotations: map[string]string{"pp:data-source": "auto"}}
	if got := declaredAgentSource(autoLive, &rootFlags{dataSource: "live"}); got != "live" {
		t.Fatalf("declaredAgentSource auto+live flag = %q, want live", got)
	}
	dry := &cobra.Command{Annotations: map[string]string{"pp:data-source": "live"}}
	if got := declaredAgentSource(dry, &rootFlags{dryRun: true}); got != "dry-run" {
		t.Fatalf("declaredAgentSource dry-run = %q, want dry-run", got)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintJSONFilteredAgentDefaultsLocal|TestPrintJSONFilteredAgentUsesDeclaredLiveSource|TestPrintOutputWithFlagsAgentPreservesProvenanceEnvelope|TestDeclaredAgentSourceLiveAnnotation", "-count=1")
}
