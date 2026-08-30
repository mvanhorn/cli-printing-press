package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointNeedsClientLimit verifies the gate that controls when
// generated GET commands emit a `truncateJSONArray(cmd.Context(), data, flagLimit)` call
// after the API response returns. This is the core decision behind
// hackernews retro #350 finding F6 — APIs that accept ?limit=N without
// honoring it (Firebase, file dumps, RSS) silently broke --limit until
// the generator started truncating client-side.
func TestEndpointNeedsClientLimit(t *testing.T) {
	t.Parallel()

	mkParams := func(p ...spec.Param) []spec.Param { return p }
	limitParam := spec.Param{Name: "limit", Type: "int"}
	otherParam := spec.Param{Name: "page", Type: "int"}
	positionalLimit := spec.Param{Name: "limit", Type: "int", Positional: true}
	pathLimit := spec.Param{Name: "limit", Type: "int", PathParam: true}

	cases := []struct {
		name     string
		endpoint spec.Endpoint
		want     bool
	}{
		{
			name:     "GET with limit and no pagination → truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(limitParam)},
			want:     true,
		},
		{
			name:     "GET with limit and pagination block → defer to API",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(limitParam), Pagination: &spec.Pagination{}},
			want:     false,
		},
		{
			name:     "GET with no limit param → no truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(otherParam)},
			want:     false,
		},
		{
			name:     "GET with no params → no truncate",
			endpoint: spec.Endpoint{Method: "GET"},
			want:     false,
		},
		{
			name:     "POST with limit param → no truncate (mutation, not list)",
			endpoint: spec.Endpoint{Method: "POST", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "PUT with limit param → no truncate",
			endpoint: spec.Endpoint{Method: "PUT", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "DELETE with limit param → no truncate",
			endpoint: spec.Endpoint{Method: "DELETE", Params: mkParams(limitParam)},
			want:     false,
		},
		{
			name:     "GET with positional 'limit' → no truncate (not a flag)",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(positionalLimit)},
			want:     false,
		},
		{
			name:     "GET with path-param 'limit' → no truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(pathLimit)},
			want:     false,
		},
		{
			name:     "GET with mixed params including limit → truncate",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(otherParam, limitParam)},
			want:     true,
		},
		{
			name:     "lowercase method get → truncate (case-insensitive method check)",
			endpoint: spec.Endpoint{Method: "get", Params: mkParams(limitParam)},
			want:     true,
		},
		{
			name:     "uppercase param Limit → truncate (case-insensitive param-name check)",
			endpoint: spec.Endpoint{Method: "GET", Params: mkParams(spec.Param{Name: "Limit", Type: "int"})},
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := endpointNeedsClientLimit(tc.endpoint)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClientLimitGenerationEmitsHelperAndBuilds(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("limit-truncate")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Params: []spec.Param{{
						Name:    "limit",
						Type:    "integer",
						Default: 5,
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "limit-truncate-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "func truncateJSONArray(")

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_orders.go")
	require.Contains(t, commandSrc, "truncateJSONArray(cmd.Context(), data,",
		"generated command should call truncateJSONArray for non-paginated limit endpoint")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

// TestNumberTypedLimitParamCoercesToInt verifies that a spec declaring
// `limit` as `number` (the LLM-derived `--docs`-mode shape from #1082)
// still emits `var flagLimit int` and `IntVar`, so the generated
// `truncateJSONArray(cmd.Context(), data, flagLimit)` call compiles. OpenAPI specs
// declare `integer` and exercise the same path; this test pins the
// override that prevents `Float64Var`/`float64 flagLimit` regression.
func TestNumberTypedLimitParamCoercesToInt(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("limit-number")
	apiSpec.Resources = map[string]spec.Resource{
		"webhooks": {
			Description: "Manage webhooks",
			Endpoints: map[string]spec.Endpoint{
				"list-payloads": {
					Method:      "GET",
					Path:        "/webhooks/payloads",
					Description: "List webhook payloads",
					Params: []spec.Param{{
						Name:    "limit",
						Type:    "number",
						Default: 0.0,
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Payload"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "limit-number-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_webhooks.go")
	assert.Contains(t, commandSrc, "var flagLimit int",
		"number-typed limit param must coerce to int Go type")
	assert.Contains(t, commandSrc, "IntVar(&flagLimit",
		"number-typed limit param must bind via IntVar")
	assert.NotContains(t, commandSrc, "Float64Var(&flagLimit",
		"number-typed limit param must not emit Float64Var")
	assert.NotContains(t, commandSrc, "var flagLimit float64",
		"number-typed limit param must not declare flagLimit as float64")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestHTMLResponseLimitAppliesAfterExtraction(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orders":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"1"},{"id":"2"},{"id":"3"},{"id":"4"},{"id":"5"}]`))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body>
				<a href="/items/1">One</a>
				<a href="/items/2">Two</a>
				<a href="/items/3">Three</a>
				<a href="/items/4">Four</a>
				<a href="/items/5">Five</a>
			</body></html>`))
		}
	}))
	t.Cleanup(server.Close)

	limitParam := spec.Param{Name: "limit", Type: "integer", Default: 10}
	htmlList := spec.Endpoint{
		Method:         http.MethodGet,
		Path:           "/listings",
		Description:    "List listings from HTML",
		Params:         []spec.Param{limitParam},
		ResponseFormat: spec.ResponseFormatHTML,
		HTMLExtract: &spec.HTMLExtract{
			Mode:         spec.HTMLExtractModeLinks,
			LinkPrefixes: []string{"/items"},
		},
		Response: spec.ResponseDef{Type: "array", Item: "html_link"},
	}

	apiSpec := minimalSpec("html-limit")
	apiSpec.BaseURL = server.URL
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"listings": {
			Description: "HTML listings",
			Endpoints: map[string]spec.Endpoint{
				"list": htmlList,
			},
		},
		"catalog": {
			Description: "Nested HTML catalog",
			Endpoints: map[string]spec.Endpoint{
				"list": htmlList,
				"page": {
					Method:         http.MethodGet,
					Path:           "/catalog/page",
					Description:    "Read a catalog page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
				},
			},
		},
		"orders": {
			Description: "JSON orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/orders",
					Description: "List orders",
					Params:      []spec.Param{limitParam},
					Response:    spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	promotedHTML := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_listings.go")
	nestedHTML := readGeneratedFile(t, outputDir, "internal", "cli", "catalog_list.go")
	jsonSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_orders.go")
	assertHTMLLimitOrder(t, promotedHTML)
	assertHTMLLimitOrder(t, nestedHTML)
	require.Contains(t, jsonSrc, "truncateJSONArray(cmd.Context(), data,",
		"JSON list endpoints must keep client-side --limit truncation")
	require.NotContains(t, jsonSrc, "extractHTMLResponse(",
		"JSON list endpoints must not route through HTML extraction")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	baseEnv := isolatedCLIEnv(t, home,
		strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL,
	)
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "promoted html honors limit", args: []string{"listings", "--limit", "2", "--json"}, want: 2},
		{name: "nested html honors limit", args: []string{"catalog", "list", "--limit", "2", "--json"}, want: 2},
		{name: "json still truncates before parse", args: []string{"orders", "--limit", "2", "--json"}, want: 2},
		{name: "promoted html keeps all extracted items without tighter limit", args: []string{"listings", "--json"}, want: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath, tc.args...)
			cmd.Env = baseEnv
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Equal(t, tc.want, jsonResultCount(t, out), string(out))
		})
	}
}

func assertHTMLLimitOrder(t *testing.T, src string) {
	t.Helper()
	extractIdx := strings.Index(src, "extractHTMLResponse(")
	truncateIdx := strings.Index(src, "truncateJSONArray(")
	require.NotEqual(t, -1, extractIdx, "html-response command must call extractHTMLResponse")
	require.NotEqual(t, -1, truncateIdx, "html-response command with --limit must call truncateJSONArray")
	require.Less(t, extractIdx, truncateIdx,
		"truncateJSONArray must run after extractHTMLResponse so --limit applies to extracted items")
	if pathIdx := strings.Index(src, "applyResponsePath("); pathIdx != -1 {
		require.Less(t, pathIdx, truncateIdx,
			"truncateJSONArray must run after applyResponsePath so --limit applies to the selected array")
	}
}

func TestHTMLResponseLimitFollowsStorelessResponsePath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-limit-path")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"gallery": {
			Description: "HTML gallery page",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         http.MethodGet,
					Path:           "/gallery",
					Description:    "List gallery links from a page object",
					Params:         []spec.Param{{Name: "limit", Type: "integer", Default: 10}},
					ResponseFormat: spec.ResponseFormatHTML,
					ResponsePath:   "links",
					HTMLExtract: &spec.HTMLExtract{
						Mode:         spec.HTMLExtractModePage,
						LinkPrefixes: []string{"/items"},
					},
					Response: spec.ResponseDef{Type: "array", Item: "html_link"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_gallery.go")
	extractIdx := strings.Index(src, "extractHTMLResponse(")
	pathIdx := strings.Index(src, "applyResponsePath(")
	truncateIdx := strings.Index(src, "truncateJSONArray(")
	require.NotEqual(t, -1, extractIdx)
	require.NotEqual(t, -1, pathIdx)
	require.NotEqual(t, -1, truncateIdx)
	require.Less(t, extractIdx, pathIdx)
	require.Less(t, pathIdx, truncateIdx,
		"storeless html-response commands must truncate after ResponsePath unwraps extracted items")
}

func isolatedCLIEnv(t *testing.T, home string, extra ...string) []string {
	t.Helper()
	drop := map[string]struct{}{
		"HOME": {}, "USERPROFILE": {},
		"XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {},
		"XDG_STATE_HOME": {}, "XDG_CACHE_HOME": {},
	}
	env := make([]string, 0, len(os.Environ())+len(extra)+6)
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, skip := drop[key]; skip {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	return append(env, extra...)
}

func jsonResultCount(t *testing.T, out []byte) int {
	t.Helper()
	var envelope struct {
		Results []json.RawMessage `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	if envelope.Results != nil {
		return len(envelope.Results)
	}
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(out, &arr), string(out))
	return len(arr)
}
