package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func postReadOutputSpec() *spec.APISpec {
	apiSpec := minimalSpec("post-read-output")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"sets": {
			Description: "Sets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/sets",
					Description: "List sets",
					Response:    spec.ResponseDef{Type: "array"},
				},
				"search": {
					Method:      "POST",
					Path:        "/sets/search",
					Description: "Search sets",
					Body:        []spec.Param{{Name: "query", Type: "string"}},
					Response:    spec.ResponseDef{Type: "array"},
				},
				"items": {
					Method:      "POST",
					Path:        "/sets/items",
					Description: "List set items",
					Mutation:    spec.OptionalBool(false),
					Body:        []spec.Param{{Name: "term", Type: "string"}},
					Response:    spec.ResponseDef{Type: "array"},
				},
				"create": {
					Method:      "POST",
					Path:        "/sets",
					Description: "Create a set",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
	}
	return apiSpec
}

func TestGeneratedPOSTReadCommandsPrintResponseBody(t *testing.T) {
	t.Parallel()

	apiSpec := postReadOutputSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sets_search.go")
	require.Contains(t, searchSrc, "c.PostQueryWithParams(",
		"POST search must use the read client path")
	require.Contains(t, searchSrc, "printOutputWithFlagsMeta(",
		"POST search must print the response body through the read output path")
	require.NotContains(t, searchSrc, `"action":   "post"`,
		"POST search must not wrap the body in the mutation acknowledgment envelope")

	itemsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sets_items.go")
	require.Contains(t, itemsSrc, "c.PostQueryWithParams(",
		"POST items with mutation:false must use the read client path")
	require.Contains(t, itemsSrc, "printOutputWithFlagsMeta(",
		"POST items with mutation:false must print the response body")
	require.NotContains(t, itemsSrc, `"action":   "post"`,
		"POST items with mutation:false must not use the mutation envelope")

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sets_create.go")
	require.Contains(t, createSrc, "c.PostWithParams(",
		"POST create must keep the mutation client path")
	require.Contains(t, createSrc, `"action":   "post"`,
		"POST create must keep the mutation acknowledgment envelope")

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPOSTReadCommandsPrintCollectionBody(t *testing.T) {
	const searchBody = "[{\"id\":\"set-1\",\"name\":\"Alpha\"}]"
	const itemsBody = "[{\"id\":\"row-1\",\"label\":\"First\"}]"
	const createBody = "{\"id\":\"set-2\",\"name\":\"Created\"}"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sets/search":
			fmt.Fprint(w, searchBody)
		case r.Method == http.MethodPost && r.URL.Path == "/sets/items":
			fmt.Fprint(w, itemsBody)
		case r.Method == http.MethodPost && r.URL.Path == "/sets":
			fmt.Fprint(w, createBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("POST_READ_OUTPUT_BASE_URL", server.URL)

	searchOut := runPostReadCommand(t, "sets", "search", "--query", "alpha", "--json")
	if strings.Contains(searchOut, "\"action\"") {
		t.Fatalf("POST search printed mutation envelope: %s", searchOut)
	}
	if !strings.Contains(searchOut, "set-1") || !strings.Contains(searchOut, "Alpha") {
		t.Fatalf("POST search did not print the response body: %s", searchOut)
	}

	itemsOut := runPostReadCommand(t, "sets", "items", "--term", "all", "--json")
	if strings.Contains(itemsOut, "\"action\"") {
		t.Fatalf("POST items printed mutation envelope: %s", itemsOut)
	}
	if !strings.Contains(itemsOut, "row-1") || !strings.Contains(itemsOut, "First") {
		t.Fatalf("POST items did not print the response body: %s", itemsOut)
	}

	createOut := runPostReadCommand(t, "sets", "create", "--name", "Created", "--json")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(createOut), &envelope); err != nil {
		t.Fatalf("POST create output is not JSON: %v\n%s", err, createOut)
	}
	if envelope["action"] != "post" {
		t.Fatalf("POST create lost the mutation envelope: %s", createOut)
	}
	if envelope["success"] != true {
		t.Fatalf("POST create success=%v, want true: %s", envelope["success"], createOut)
	}
}

func runPostReadCommand(t *testing.T, args ...string) string {
	t.Helper()
	var stdout bytes.Buffer
	var flags rootFlags
	cmd := newRootCmd(&flags)
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, stdout.String())
	}
	return stdout.String()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "post_read_output_test.go"), []byte(behaviorTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPOSTReadCommandsPrintCollectionBody", "-count=1")
}
