package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPrintJSONFilteredAgentWrapsTypedOutputs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/items" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"created","name":"Created item"}`))
	}))
	defer server.Close()

	apiSpec := minimalSpec("agent-envelope")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources["items"].Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create item",
		Response:    spec.ResponseDef{Type: "object"},
	}
	outputDir := filepath.Join(t.TempDir(), "agent-envelope-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, Analytics: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "agent_envelope_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func decodeAgentEnvelope(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, raw)
	}
	if _, ok := payload["meta"]; !ok {
		t.Fatalf("agent output missing top-level meta: %s", raw)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("agent output missing top-level results: %s", raw)
	}
	if _, ok := payload["data"]; ok {
		t.Fatalf("agent output must use results as the only data field, got data too: %s", raw)
	}
	return payload
}

func TestPrintJSONFilteredAgentWrapsBareArray(t *testing.T) {
	var out bytes.Buffer
	rows := []map[string]any{{"id": "one", "name": "Alpha"}, {"id": "two", "name": "Beta"}}
	flags := &rootFlags{agent: true, asJSON: true, compact: true}

	if err := printJSONFiltered(&out, rows, flags); err != nil {
		t.Fatalf("printJSONFiltered returned error: %v", err)
	}

	payload := decodeAgentEnvelope(t, out.Bytes())
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, out.String())
	}
	if meta.Source == "" {
		t.Fatalf("meta.source must be populated for agent output: %s", out.String())
	}
	var results []json.RawMessage
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must contain the original array shape: %v\n%s", err, out.String())
	}
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2; output=%s", len(results), out.String())
	}
}

func TestPrintJSONFilteredAgentWrapsDomainObject(t *testing.T) {
	var out bytes.Buffer
	result := map[string]any{"resource_type": "items", "count": 7}
	flags := &rootFlags{agent: true, asJSON: true, compact: true}

	if err := printJSONFiltered(&out, result, flags); err != nil {
		t.Fatalf("printJSONFiltered returned error: %v", err)
	}

	payload := decodeAgentEnvelope(t, out.Bytes())
	var results map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must contain the original object shape: %v\n%s", err, out.String())
	}
	if results["resource_type"] != "items" || results["count"] != float64(7) {
		t.Fatalf("results = %#v, want original domain object; output=%s", results, out.String())
	}
}

func TestPrintJSONFilteredJSONModeStaysBareObject(t *testing.T) {
	var out bytes.Buffer
	result := map[string]any{"resource_type": "items", "count": 7}
	flags := &rootFlags{asJSON: true}

	if err := printJSONFiltered(&out, result, flags); err != nil {
		t.Fatalf("printJSONFiltered returned error: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json output must be valid JSON: %v\n%s", err, out.String())
	}
	if _, ok := payload["meta"]; ok {
		t.Fatalf("--json without --agent must stay backward-compatible, got envelope: %s", out.String())
	}
	if _, ok := payload["results"]; ok {
		t.Fatalf("--json without --agent must stay backward-compatible, got results envelope: %s", out.String())
	}
	if _, ok := payload["count"]; !ok {
		t.Fatalf("--json output lost original object fields: %s", out.String())
	}
}

func TestGeneratedAnalyticsCommandAgentUsesEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"analytics", "--type", "items", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("analytics --agent failed: %v\nstderr=%s", err, stderr.String())
	}

	payload := decodeAgentEnvelope(t, stdout.Bytes())
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, stdout.String())
	}
	if meta.Source != "local" {
		t.Fatalf("analytics meta.source = %q, want local; output=%s", meta.Source, stdout.String())
	}
	var results map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must contain analytics object: %v\n%s", err, stdout.String())
	}
	if results["resource_type"] != "items" {
		t.Fatalf("analytics results = %#v, want resource_type items; output=%s", results, stdout.String())
	}
	if _, ok := results["count"]; !ok {
		t.Fatalf("analytics results missing count: %#v; output=%s", results, stdout.String())
	}
}

func TestGeneratedMutationCommandAgentUsesResultsEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"items", "create", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("items create --agent failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	payload := decodeAgentEnvelope(t, stdout.Bytes())
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, stdout.String())
	}
	if meta.Source != "live" {
		t.Fatalf("mutation meta.source = %q, want live; output=%s", meta.Source, stdout.String())
	}
	var results map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must contain mutation response object: %v\n%s", err, stdout.String())
	}
	if results["id"] != "created" {
		t.Fatalf("results should preserve live mutation response: %#v; output=%s", results, stdout.String())
	}
}

func TestGeneratedMutationCommandJSONKeepsDataEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"items", "create", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("items create --json failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json output must be valid JSON: %v\n%s", err, stdout.String())
	}
	if _, ok := payload["data"]; !ok {
		t.Fatalf("--json mutation output must keep data field for compatibility: %s", stdout.String())
	}
	if _, ok := payload["results"]; ok {
		t.Fatalf("--json without --agent must not add results field: %s", stdout.String())
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintJSONFiltered|TestGeneratedAnalyticsCommandAgentUsesEnvelope|TestGeneratedMutationCommand", "-count=1")
}

func TestGeneratedNoStoreEndpointAgentWrapsFallthroughOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/items":
			_, _ = w.Write([]byte(`[{"id":"one","name":"Alpha"}]`))
		case "/widgets":
			_, _ = w.Write([]byte(`{"meta":{"total":1,"page":1},"results":[{"id":"nested","name":"Nested shape"}]}`))
		case "/gadgets":
			_, _ = w.Write([]byte(`{"results":[{"id":"flat","name":"Single-key wrapper"}]}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiSpec := minimalSpec("agent-endpoint")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Manage widgets",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/widgets", Description: "List widgets"},
		},
	}
	apiSpec.Resources["gadgets"] = spec.Resource{
		Description: "Manage gadgets",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/gadgets", Description: "List gadgets"},
		},
	}
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "agent-endpoint-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: false, Export: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "agent_endpoint_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNoStoreEndpointAgentWrapsBareArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"items", "list", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("items list --agent failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, stdout.String())
	}
	if _, ok := payload["meta"]; !ok {
		t.Fatalf("agent endpoint output missing meta: %s", stdout.String())
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("agent endpoint output missing results: %s", stdout.String())
	}
	var meta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &meta); err != nil {
		t.Fatalf("meta must be an object: %v\n%s", err, stdout.String())
	}
	if meta.Source != "live" {
		t.Fatalf("endpoint meta.source = %q, want live; output=%s", meta.Source, stdout.String())
	}
	var results []map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must preserve endpoint array: %v\n%s", err, stdout.String())
	}
	if len(results) != 1 || results[0]["id"] != "one" {
		t.Fatalf("results = %#v, want live endpoint row; output=%s", results, stdout.String())
	}
}

func TestNoStoreEndpointJSONRemainsBareArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"items", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("items list --json failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("--json endpoint output must remain the bare array: %v\n%s", err, stdout.String())
	}
	if len(rows) != 1 || rows[0]["id"] != "one" {
		t.Fatalf("rows = %#v, want live endpoint row; output=%s", rows, stdout.String())
	}
}

func TestNoStoreEndpointAgentWrapsNaturalMetaResultsObject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"widgets", "list", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("widgets list --agent failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, stdout.String())
	}
	var topMeta struct {
		Source string `+"`json:\"source\"`"+`
	}
	if err := json.Unmarshal(payload["meta"], &topMeta); err != nil {
		t.Fatalf("top-level meta must be an object: %v\n%s", err, stdout.String())
	}
	if topMeta.Source != "live" {
		t.Fatalf("top-level meta.source = %q, want live; output=%s", topMeta.Source, stdout.String())
	}

	var results map[string]json.RawMessage
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results must preserve natural endpoint object: %v\n%s", err, stdout.String())
	}
	if _, ok := results["meta"]; !ok {
		t.Fatalf("results should preserve endpoint meta field: %s", stdout.String())
	}
	if _, ok := results["results"]; !ok {
		t.Fatalf("results should preserve endpoint results field: %s", stdout.String())
	}
}

func TestNoStoreEndpointAgentFlattensSingleKeyCollectionWrapper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"gadgets", "list", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gadgets list --agent failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("agent output must be valid JSON: %v\n%s", err, stdout.String())
	}
	var results []map[string]any
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("results should be the flattened endpoint collection: %v\n%s", err, stdout.String())
	}
	if len(results) != 1 || results[0]["id"] != "flat" {
		t.Fatalf("results = %#v, want flattened live endpoint row; output=%s", results, stdout.String())
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestNoStoreEndpoint", "-count=1")
}
