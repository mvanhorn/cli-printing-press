package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dryRunReadSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "cursor",
						LimitParam:     "limit",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
				},
				"get": {
					Method:      "GET",
					Path:        "/items/{id}",
					Description: "Get an item",
					Response:    spec.ResponseDef{Type: "object", Item: "Item"},
					Params: []spec.Param{{
						Name:       "id",
						Type:       "string",
						Required:   true,
						Positional: true,
						PathParam:  true,
					}},
				},
			},
		},
		"widgets": {
			Description: "Manage widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
					Response:    spec.ResponseDef{Type: "array", Item: "Widget"},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "cursor",
						LimitParam:     "limit",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item":   {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Widget": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
	}
	return apiSpec
}

func TestSpecEmitsReadDryRunBranch(t *testing.T) {
	t.Parallel()

	reads := dryRunReadSpec("gate-reads")
	require.True(t, specEmitsReadDryRunBranch(reads, buildPromotedCommands(reads)))

	mutations := minimalSpec("gate-mutations")
	mutations.Learn.Disabled = true
	mutations.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/items",
					Description: "Create an item",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
	}
	require.False(t, specEmitsReadDryRunBranch(mutations, buildPromotedCommands(mutations)))
	require.False(t, specEmitsReadDryRunBranch(nil, nil))

	outputDir := filepath.Join(t.TempDir(), naming.CLI(mutations.Name))
	gen := New(mutations, outputDir)
	// MCP emits internal/mcp, which the always-generated server imports.
	// Export-only leaves that package empty, so ./... cannot compile.
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())
	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.NotContains(t, helpers, "func printNoStoreReadDryRun(")
	promoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.NotContains(t, promoted, "printNoStoreReadDryRun(")
	requireGeneratedCompiles(t, outputDir)

	// A promoted read-only DELETE still takes the read dry-run branch.
	// Non-promoted DELETE commands do not.
	deleted := minimalSpec("gate-delete-read")
	deleted.Learn.Disabled = true
	deleted.Resources = map[string]spec.Resource{
		"cleanups": {
			Description: "Cleanups",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "DELETE",
					Path:        "/cleanups",
					Description: "Search cleanups",
					Body:        []spec.Param{{Name: "query", Type: "string"}},
				},
			},
		},
	}
	require.True(t, specEmitsReadDryRunBranch(deleted, buildPromotedCommands(deleted)))
	deleteDir := filepath.Join(t.TempDir(), naming.CLI(deleted.Name))
	deleteGen := New(deleted, deleteDir)
	deleteGen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, deleteGen.Generate())
	assert.Contains(t, readGeneratedFile(t, deleteDir, "internal", "cli", "helpers.go"), "func printNoStoreReadDryRun(")
	assert.Contains(t, readGeneratedFile(t, deleteDir, "internal", "cli", "promoted_cleanups.go"), "printNoStoreReadDryRun(")
	assert.Contains(t, readGeneratedFile(t, deleteDir, "internal", "cli", "promoted_cleanups.go"), `"delete"`)
	requireGeneratedCompiles(t, deleteDir)
}

func TestGeneratedDryRunReadGuardsEndpointAndPromotedOutputs(t *testing.T) {
	t.Parallel()

	apiSpec := dryRunReadSpec("dry-run-read-guards")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	dryRunIdx := strings.Index(dataSourceSrc, "if isDryRunResponse(c.IsDryRun(), data)")
	cacheIdx := strings.Index(dataSourceSrc, "writeThroughCache(ctx, resourceType, data)")
	require.GreaterOrEqual(t, dryRunIdx, 0)
	require.GreaterOrEqual(t, cacheIdx, 0)
	assert.Less(t, dryRunIdx, cacheIdx, "dry-run reads must return before the live cache write")
	assert.Contains(t, dataSourceSrc, `DataProvenance{Source: "dry-run"}`)

	noStoreDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name)+"-nostore")
	noStoreGen := New(apiSpec, noStoreDir)
	noStoreGen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, noStoreGen.Generate())

	endpointSrc := readGeneratedFile(t, noStoreDir, "internal", "cli", "items_list.go")
	assert.Contains(t, endpointSrc, "if isDryRunResponse(c.IsDryRun(), data)")
	assert.Contains(t, endpointSrc, "printNoStoreReadDryRun(")
	assert.Contains(t, endpointSrc, `"get"`)
	assert.Contains(t, endpointSrc, `"items"`)
	assert.Contains(t, endpointSrc, "flagAll && !flags.dryRun")

	promotedSrc := readGeneratedFile(t, noStoreDir, "internal", "cli", "promoted_widgets.go")
	assert.Contains(t, promotedSrc, "if isDryRunResponse(c.IsDryRun(), data)")
	assert.Contains(t, promotedSrc, "printNoStoreReadDryRun(")
	assert.Contains(t, promotedSrc, `"get"`)
	assert.Contains(t, promotedSrc, `"widgets"`)
	assert.Contains(t, promotedSrc, "flagAll && !flags.dryRun")

	noStoreHelpers := readGeneratedFile(t, noStoreDir, "internal", "cli", "helpers.go")
	assert.Contains(t, noStoreHelpers, "func printNoStoreReadDryRun(")
	storeHelpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.NotContains(t, storeHelpers, "func printNoStoreReadDryRun(")

	requireGeneratedCompiles(t, outputDir)
	requireGeneratedCompiles(t, noStoreDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(noStoreDir, "internal", "cli", "no_store_read_dry_run_runtime_test.go"),
		[]byte(noStoreReadDryRunRuntimeTest(generatedModulePath(t, noStoreDir))),
		0o644,
	))
	runGoCommand(t, noStoreDir, "test", "./internal/cli", "-run", "^TestPrintNoStoreReadDryRunKeepsContractFields$", "-count=1")
}

func TestGeneratedDryRunReadPreservesProvenanceAndSkipsStore(t *testing.T) {
	apiSpec := dryRunReadSpec("dry-run-read")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeDryRunRead(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var flags rootFlags
	root := newRootCmd(&flags)
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs(args)

	oldStderr := os.Stderr
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = pipeWriter
	stderrDone := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(pipeReader)
		stderrDone <- string(data)
	}()
	execErr := root.Execute()
	_ = pipeWriter.Close()
	os.Stderr = oldStderr
	stderr := <-stderrDone
	_ = pipeReader.Close()
	return stdout.String(), stderr, execErr
}

func assertDryRunEnvelope(t *testing.T, output string) {
	t.Helper()
	var payload struct {
		Meta struct {
			Source string ` + "`" + `json:"source"` + "`" + `
		} ` + "`" + `json:"meta"` + "`" + `
		Results json.RawMessage ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse agent output: %v\n%s", err, output)
	}
	if payload.Meta.Source != "dry-run" {
		t.Fatalf("meta.source = %q, want dry-run; output=%s", payload.Meta.Source, output)
	}
	if !bytes.Contains(payload.Results, []byte(` + "`" + `"dry_run"` + "`" + `)) {
		t.Fatalf("results = %s, want dry-run sentinel", payload.Results)
	}
}

func TestPromotedDryRunReadDoesNotOpenStore(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, err := executeDryRunRead(t, "--home", home, "--dry-run", "--agent", "widgets", "--all")
	if err != nil {
		t.Fatalf("promoted dry-run: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertDryRunEnvelope(t, stdout)
	if strings.Contains(stderr, "warning:") || strings.Contains(stderr, "not cached locally") {
		t.Fatalf("dry-run emitted cache warning: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "data.db")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created store: stat error = %v", err)
	}
}

func TestEndpointDryRunReadDoesNotOpenStore(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, err := executeDryRunRead(t, "--home", home, "--dry-run", "--agent", "items", "list", "--all")
	if err != nil {
		t.Fatalf("endpoint dry-run: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertDryRunEnvelope(t, stdout)
	if strings.Contains(stderr, "warning:") || strings.Contains(stderr, "not cached locally") {
		t.Fatalf("dry-run emitted cache warning: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "data.db")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created store: stat error = %v", err)
	}
}

func TestLiveReadsStillWriteThroughCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/items" {
			_, _ = w.Write([]byte(` + "`" + `[{"id":"item-1","name":"Live item"}]` + "`" + `))
			return
		}
		if r.URL.Path == "/widgets" {
			_, _ = w.Write([]byte(` + "`" + `[{"id":"widget-1","name":"Live widget"}]` + "`" + `))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("DRY_RUN_READ_BASE_URL", server.URL)

	home := t.TempDir()
	stdout, stderr, err := executeDryRunRead(t, "--home", home, "--agent", "items", "list")
	if err != nil {
		t.Fatalf("live endpoint read: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload struct {
		Meta struct {
			Source string ` + "`" + `json:"source"` + "`" + `
		} ` + "`" + `json:"meta"` + "`" + `
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse live output: %v\n%s", err, stdout)
	}
	if payload.Meta.Source != "live" {
		t.Fatalf("live meta.source = %q, want live; output=%s", payload.Meta.Source, stdout)
	}
	if calls != 1 {
		t.Fatalf("live endpoint request count = %d, want 1", calls)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "data.db")); err != nil {
		t.Fatalf("live read did not create cache store: %v", err)
	}
	if strings.Contains(stderr, "not cached locally") {
		t.Fatalf("live read unexpectedly warned about cacheability: %s", stderr)
	}
}

func TestLivePayloadMatchingDryRunSentinelRemainsLive(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"dry_run":true}` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("DRY_RUN_READ_BASE_URL", server.URL)

	stdout, _, err := executeDryRunRead(t, "--home", t.TempDir(), "--agent", "items", "list")
	if err != nil {
		t.Fatalf("live sentinel-shaped read: %v\nstdout=%s", err, stdout)
	}
	var payload struct {
		Meta struct {
			Source string ` + "`" + `json:"source"` + "`" + `
		} ` + "`" + `json:"meta"` + "`" + `
		Results json.RawMessage ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse live sentinel-shaped output: %v\n%s", err, stdout)
	}
	if payload.Meta.Source != "live" {
		t.Fatalf("sentinel-shaped live meta.source = %q, want live; output=%s", payload.Meta.Source, stdout)
	}
	if !bytes.Contains(payload.Results, []byte(` + "`" + `"dry_run"` + "`" + `)) {
		t.Fatalf("live response lost the sentinel-shaped payload: %s", payload.Results)
	}
	if calls != 1 {
		t.Fatalf("live sentinel-shaped endpoint request count = %d, want 1", calls)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "dry_run_read_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(Promoted|Endpoint)DryRunReadDoesNotOpenStore|TestLiveReadsStillWriteThroughCache|TestLivePayloadMatchingDryRunSentinelRemainsLive", "-count=1")
}

func noStoreReadDryRunRuntimeTest(modulePath string) string {
	return `package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"` + modulePath + `/internal/platform"
)

func TestPrintNoStoreReadDryRunKeepsContractFields(t *testing.T) {
	sentinel := json.RawMessage(` + "`" + `{"dry_run":true}` + "`" + `)
	assertKeepsDryRunFields(t, &rootFlags{asJSON: true}, sentinel)
	assertKeepsDryRunFields(t, &rootFlags{asJSON: true, agent: true}, sentinel)
	assertKeepsDryRunFields(t, &rootFlags{asJSON: true, platformSession: &platform.Session{}}, sentinel)
	assertKeepsDryRunFields(t, &rootFlags{asJSON: true, agent: true, platformSession: &platform.Session{}}, sentinel)
}

func assertKeepsDryRunFields(t *testing.T, flags *rootFlags, sentinel json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := printNoStoreReadDryRun(&buf, sentinel, flags, "get", "items", "/items"); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("parse %s: %v", buf.String(), err)
	}
	if payload["dry_run"] != true || payload["action"] != "get" || payload["resource"] != "items" || payload["path"] != "/items" {
		t.Fatalf("dry-run fields not top-level: %s", buf.String())
	}
	meta, _ := payload["meta"].(map[string]any)
	if meta["source"] != "dry-run" {
		t.Fatalf("meta = %#v\n%s", meta, buf.String())
	}
	if flags.agent {
		results, ok := payload["results"].(map[string]any)
		if !ok || results["dry_run"] != true {
			t.Fatalf("agent results: %s", buf.String())
		}
		return
	}
	data, ok := payload["data"].(map[string]any)
	if !ok || data["dry_run"] != true {
		t.Fatalf("data: %s", buf.String())
	}
}
`
}
