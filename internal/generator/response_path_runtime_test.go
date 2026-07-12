package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedReadCommandAppliesResponsePathToOutputAndWriteThrough(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("response-path-output")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"jobs": {
			Description: "Jobs",
			Endpoints: map[string]spec.Endpoint{
				"recommended": {
					Method:       "GET",
					Path:         "/recommended",
					Description:  "List recommended jobs",
					Response:     spec.ResponseDef{Type: "array", Item: "Job"},
					ResponsePath: "result.jobList",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Job": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "jobResult", Type: "object"},
				{Name: "keyword", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "response_path_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadCommandAppliesResponsePathToOutputAndWriteThrough(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/recommended" {
			t.Fatalf("path = %q, want /recommended", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`{"success":true,"result":{"jobList":[{"id":"job-1","jobResult":{"jobTitle":"Engineer"},"keyword":"uplifted"}],"folders":[{"id":"folder-1","name":"Archive"}]}}`+"`"+`)
	}))
	defer server.Close()
	t.Setenv("RESPONSE_PATH_OUTPUT_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"jobs", "recommended", "--json", "--select", "jobResult.jobTitle"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Engineer") {
		t.Fatalf("response_path output was not projected before --select; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "folder-1") || strings.Contains(stdout.String(), "success") {
		t.Fatalf("command output should render the response_path payload, not the raw envelope: %s", stdout.String())
	}

	db, err := openStoreForRead(context.Background(), "response-path-output-pp-cli")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if db == nil {
		t.Fatalf("expected write-through cache to create the local store")
	}
	defer db.Close()

	results, err := db.Search("uplifted", 10)
	if err != nil {
		t.Fatalf("search write-through cache: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search found %d rows, want 1; stdout=%s stderr=%s", len(results), stdout.String(), stderr.String())
	}
	var row map[string]any
	if err := json.Unmarshal(results[0], &row); err != nil {
		t.Fatalf("decode cached row: %v", err)
	}
	if row["id"] != "job-1" || row["keyword"] != "uplifted" {
		t.Fatalf("cached row = %#v, want response_path job row", row)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestReadCommandAppliesResponsePathToOutputAndWriteThrough", "-count=1")
}
