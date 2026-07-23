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

func TestGeneratedDryRunReadGuardsEndpointAndPromotedOutputs(t *testing.T) {
	t.Parallel()

	apiSpec := dryRunReadSpec("dry-run-read-guards")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	dryRunIdx := strings.Index(dataSourceSrc, "if isDryRunResponse(data)")
	cacheIdx := strings.Index(dataSourceSrc, "writeThroughCache(ctx, resourceType, data)")
	require.GreaterOrEqual(t, dryRunIdx, 0)
	require.GreaterOrEqual(t, cacheIdx, 0)
	assert.Less(t, dryRunIdx, cacheIdx, "dry-run reads must return before the live cache write")
	assert.Contains(t, dataSourceSrc, `DataProvenance{Source: "dry-run"}`)

	noStoreDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name)+"-nostore")
	noStoreGen := New(apiSpec, noStoreDir)
	noStoreGen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, noStoreGen.Generate())

	endpointSrc := readGeneratedFile(t, noStoreDir, "internal", "cli", "items_list.go")
	assert.Contains(t, endpointSrc, "if isDryRunResponse(data)")
	assert.Contains(t, endpointSrc, `map[string]any{"source": "dry-run"}`)
	assert.Contains(t, endpointSrc, "flagAll && !flags.dryRun")

	promotedSrc := readGeneratedFile(t, noStoreDir, "internal", "cli", "promoted_widgets.go")
	assert.Contains(t, promotedSrc, "if isDryRunResponse(data)")
	assert.Contains(t, promotedSrc, `map[string]any{"source": "dry-run"}`)
	assert.Contains(t, promotedSrc, "flagAll && !flags.dryRun")
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
`
	testPath := filepath.Join(outputDir, "internal", "cli", "dry_run_read_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "Test(Promoted|Endpoint)DryRunReadDoesNotOpenStore|TestLiveReadsStillWriteThroughCache", "-count=1")
}
