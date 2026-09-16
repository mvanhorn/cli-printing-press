package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryParamFlagNamesLiteralIncludesAliases(t *testing.T) {
	t.Parallel()

	got := queryParamFlagNamesLiteral(spec.Endpoint{
		Params: []spec.Param{
			{Name: "atlasFuzzySearchEnabled", FlagName: "fuzzy", Type: "bool"},
			{Name: "threshold", Type: "integer", Aliases: []string{"min"}},
			{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
		},
	})
	assert.Contains(t, got, `"atlasFuzzySearchEnabled":{"fuzzy"}`)
	assert.Contains(t, got, `"threshold":{"threshold","min"}`)
	assert.NotContains(t, got, `"id"`)
}

func explicitQuerySpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Learn.Enabled = false
	apiSpec.Learn.EnabledSet = false
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
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
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
					Pagination: &spec.Pagination{
						Type:        "offset",
						CursorParam: "offset",
						LimitParam:  "limit",
					},
					Params: []spec.Param{
						{
							Name:        "atlasFuzzySearchEnabled",
							FlagName:    "fuzzy",
							Type:        "bool",
							Default:     true,
							Description: "Fuzzy search",
						},
						{
							Name:        "threshold",
							Type:        "integer",
							Description: "Minimum score",
						},
						{Name: "offset", Type: "integer", Description: "Offset"},
						{Name: "limit", Type: "integer", Description: "Page size"},
					},
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
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
					Pagination: &spec.Pagination{
						Type:        "offset",
						CursorParam: "offset",
						LimitParam:  "limit",
					},
					Params: []spec.Param{
						{
							Name:        "atlasFuzzySearchEnabled",
							FlagName:    "fuzzy",
							Type:        "bool",
							Default:     true,
							Description: "Fuzzy search",
						},
						{
							Name:        "threshold",
							Type:        "integer",
							Description: "Minimum score",
						},
						{Name: "offset", Type: "integer", Description: "Offset"},
						{Name: "limit", Type: "integer", Description: "Page size"},
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}
	return apiSpec
}

func TestGeneratedPaginatedCommandsRetainExplicitFalseAndZero(t *testing.T) {
	t.Parallel()

	apiSpec := explicitQuerySpec("explicitq")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_list.go")
	assert.Contains(t, endpointSrc, "retainCLIQueryParams(cmd,",
		"paginated endpoint commands must filter unset 0/false before paginatedGet")
	assert.Contains(t, endpointSrc, `"atlasFuzzySearchEnabled": {"fuzzy"}`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_widgets.go")
	assert.Contains(t, promotedSrc, "retainCLIQueryParams(cmd,",
		"paginated promoted commands must filter unset 0/false before paginatedGet")
	requireGeneratedCompiles(t, outputDir)

	storeDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name)+"-store")
	storeGen := New(explicitQuerySpec("explicitq-store"), storeDir)
	storeGen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, storeGen.Generate())
	storeSrc := readGeneratedFile(t, storeDir, "internal", "cli", "items_list.go")
	assert.Contains(t, storeSrc, "retainCLIQueryParams(cmd,",
		"store-backed paginated commands must filter unset 0/false before resolvePaginatedRead")
	requireGeneratedCompiles(t, storeDir)
}

func TestGeneratedPaginatedQueryParamsOnTheWire(t *testing.T) {
	t.Parallel()

	apiSpec := explicitQuerySpec("explicitq")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, gen.Generate())

	behaviorTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type capturedQuery struct {
	path  string
	query string
}

func executeExplicitQuery(t *testing.T, args ...string) (capturedQuery, error) {
	t.Helper()
	var mu sync.Mutex
	var got capturedQuery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = capturedQuery{path: r.URL.Path, query: r.URL.RawQuery}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `[{"id":"one"}]` + "`" + `))
	}))
	t.Cleanup(server.Close)
	t.Setenv("EXPLICITQ_BASE_URL", server.URL)

	var flags rootFlags
	root := newRootCmd(&flags)
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs(append([]string{"--home", t.TempDir(), "--json"}, args...))
	err := root.Execute()
	mu.Lock()
	defer mu.Unlock()
	return got, err
}

func TestRetainCLIQueryParamsKeepsExplicitFalseAndZero(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	var fuzzy bool
	var threshold int
	cmd.Flags().BoolVar(&fuzzy, "fuzzy", true, "")
	cmd.Flags().IntVar(&threshold, "threshold", 0, "")
	if err := cmd.ParseFlags([]string{"--fuzzy=false", "--threshold=0"}); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{
		"atlasFuzzySearchEnabled": "false",
		"threshold":               "0",
		"offset":                  "0",
		"quiet":                   "false",
	}
	got := retainCLIQueryParams(cmd, params, map[string][]string{
		"atlasFuzzySearchEnabled": {"fuzzy"},
		"threshold":               {"threshold"},
		"offset":                  {"offset"},
		"quiet":                   {"quiet"},
	}, "offset", "offset")
	if got["atlasFuzzySearchEnabled"] != "false" {
		t.Fatalf("explicit false dropped: %#v", got)
	}
	if got["threshold"] != "0" {
		t.Fatalf("explicit 0 dropped: %#v", got)
	}
	if got["offset"] != "0" {
		t.Fatalf("offset=0 dropped: %#v", got)
	}
	if _, ok := got["quiet"]; ok {
		t.Fatalf("unset false leaked: %#v", got)
	}
}

func TestRetainCLIQueryParamsDropsUnsetDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	var fuzzy bool
	var threshold int
	cmd.Flags().BoolVar(&fuzzy, "fuzzy", true, "")
	cmd.Flags().IntVar(&threshold, "threshold", 0, "")
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{
		"atlasFuzzySearchEnabled": "true",
		"threshold":               "0",
		"offset":                  "0",
		"cursor":                  "0",
	}
	got := retainCLIQueryParams(cmd, params, map[string][]string{
		"atlasFuzzySearchEnabled": {"fuzzy"},
		"threshold":               {"threshold"},
		"offset":                  {"offset"},
		"cursor":                  {"cursor"},
	}, "offset", "offset")
	if got["atlasFuzzySearchEnabled"] != "true" {
		t.Fatalf("default true should still be sent: %#v", got)
	}
	if _, ok := got["threshold"]; ok {
		t.Fatalf("unset 0 leaked: %#v", got)
	}
	if got["offset"] != "0" {
		t.Fatalf("unset offset=0 must stay for offset pagination: %#v", got)
	}
	if _, ok := got["cursor"]; ok {
		t.Fatalf("unset id-cursor 0 leaked: %#v", got)
	}
}

type explicitQueryClient struct {
	params []map[string]string
}

func (c *explicitQueryClient) GetWithHeaders(_ context.Context, _ string, params map[string]string, _ map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.params = append(c.params, copied)
	return json.RawMessage(` + "`" + `[{"id":"one"}]` + "`" + `), nil
}

func TestPaginatedGetKeepsExplicitFalseAndZero(t *testing.T) {
	client := &explicitQueryClient{}
	_, err := paginatedGet(context.Background(), client, "/items", map[string]string{
		"atlasFuzzySearchEnabled": "false",
		"threshold":               "0",
		"offset":                  "0",
	}, nil, false, "offset", "offset", "limit", 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if client.params[0]["atlasFuzzySearchEnabled"] != "false" {
		t.Fatalf("false missing on the wire: %#v", client.params[0])
	}
	if client.params[0]["threshold"] != "0" {
		t.Fatalf("0 missing on the wire: %#v", client.params[0])
	}
	if client.params[0]["offset"] != "0" {
		t.Fatalf("offset=0 missing on the wire: %#v", client.params[0])
	}
}

func TestPaginatedGetStillDropsIdCursorZero(t *testing.T) {
	client := &explicitQueryClient{}
	_, err := paginatedGet(context.Background(), client, "/items", map[string]string{
		"after": "0",
		"limit": "10",
	}, nil, false, "after", "cursor", "limit", 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.params[0]["after"]; ok {
		t.Fatalf("id-cursor 0 must not go on the wire: %#v", client.params[0])
	}
}

func TestEndpointExplicitFalseAndZeroReachTheWire(t *testing.T) {
	got, err := executeExplicitQuery(t, "items", "list", "--fuzzy=false", "--threshold=0")
	if err != nil {
		t.Fatalf("items list: %v", err)
	}
	if got.path != "/items" {
		t.Fatalf("path = %q, want /items", got.path)
	}
	if !strings.Contains(got.query, "atlasFuzzySearchEnabled=false") {
		t.Fatalf("explicit false missing from query %q", got.query)
	}
	if !strings.Contains(got.query, "threshold=0") {
		t.Fatalf("explicit 0 missing from query %q", got.query)
	}
}

func TestEndpointUnsetFalseAndZeroStayOffTheWire(t *testing.T) {
	got, err := executeExplicitQuery(t, "items", "list")
	if err != nil {
		t.Fatalf("items list: %v", err)
	}
	if strings.Contains(got.query, "atlasFuzzySearchEnabled=false") {
		t.Fatalf("unset false leaked into query %q", got.query)
	}
	if strings.Contains(got.query, "threshold=0") {
		t.Fatalf("unset 0 leaked into query %q", got.query)
	}
}

func TestEndpointExplicitOffsetZeroReachesTheWire(t *testing.T) {
	got, err := executeExplicitQuery(t, "items", "list", "--offset=0")
	if err != nil {
		t.Fatalf("items list: %v", err)
	}
	if !strings.Contains(got.query, "offset=0") {
		t.Fatalf("explicit offset=0 missing from query %q", got.query)
	}
}

func TestPromotedExplicitFalseReachesTheWire(t *testing.T) {
	got, err := executeExplicitQuery(t, "widgets", "--fuzzy=false")
	if err != nil {
		t.Fatalf("widgets: %v", err)
	}
	if !strings.Contains(got.query, "atlasFuzzySearchEnabled=false") {
		t.Fatalf("promoted explicit false missing from query %q", got.query)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "explicit_query_params_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^Test(RetainCLIQueryParams|PaginatedGetKeepsExplicit|PaginatedGetStillDropsIdCursorZero|EndpointExplicit|EndpointUnset|PromotedExplicit)", "-count=1")
}
