package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSearchSkipsUnboundScopedEndpoint(t *testing.T) {
	t.Parallel()

	hits, baseURL := searchHitServer(t)
	apiSpec := searchCLISpec("cobot-like", baseURL, map[string]spec.Resource{
		"spaces": {
			Description: "Spaces",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/spaces",
					Description: "List spaces",
					Response:    spec.ResponseDef{Type: "array", Item: "Space"},
				},
			},
			SubResources: map[string]spec.Resource{
				"contacts": {
					Description: "Contacts",
					Endpoints: map[string]spec.Endpoint{
						"search": {
							Method:      "GET",
							Path:        "/spaces/{spaceId}/contacts/search",
							Description: "Search contacts in a space",
							Params: []spec.Param{
								{Name: "spaceId", Type: "string", In: "path", Required: true, Positional: true, PathParam: true},
								{Name: "filter[query]", Type: "string", In: "query"},
							},
							Response: spec.ResponseDef{Type: "array", Item: "Contact"},
						},
					},
				},
			},
		},
	}, searchCLITypes())

	dir, binary := buildSearchCLI(t, apiSpec)
	searchSrc := readGeneratedFile(t, dir, "internal", "cli", "search.go")
	require.NotContains(t, searchSrc, "{spaceId}")
	require.NotContains(t, searchSrc, `"q": query`)
	require.NotContains(t, searchSrc, "c.Get(")
	require.Contains(t, searchSrc, `reason = "no_search_endpoint"`)
	require.Contains(t, searchSrc, `"mcp:hidden": "true"`)

	scoped := readGeneratedFile(t, dir, "internal", "cli", "spaces_contacts_search.go")
	require.Contains(t, scoped, `replacePathParam(path, "spaceId"`)
	require.Contains(t, scoped, `"filter[query]"`)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	stdout, stderr, err := runSearchCLI(t, binary, "search", "invoice", "--data-source", "auto", "--json", "--db", dbPath)
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.NotContains(t, stdout+stderr, "{spaceId}")
	assert.Contains(t, stderr, "no search endpoint")
	meta := searchMeta(t, stdout)
	assert.Equal(t, "local", meta.Source)
	assert.Equal(t, "no_search_endpoint", meta.Reason)

	stdout, stderr, err = runSearchCLI(t, binary, "search", "invoice", "--data-source", "live", "--dry-run")
	require.Error(t, err, "live search must fail when the only endpoint has an unbound path param\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.NotContains(t, stdout+stderr, "{spaceId}")
	assert.NotRegexp(t, `(?m)^(GET|POST) `, stderr)
	assert.Contains(t, stdout+stderr, "no search endpoint")

	stdout, stderr, err = runSearchCLI(t, binary, "spaces", "contacts", "search", "spc_123", "--filter-query", "invoice", "--data-source", "live", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.Contains(t, stderr, baseURL+"/spaces/spc_123/contacts/search")
	assert.Contains(t, stderr, "filter[query]=invoice")
	assert.NotContains(t, stdout+stderr, "{spaceId}")
	assert.NotContains(t, stderr, "?q=")
}

func TestGeneratedSearchSendsBracketedQueryOnBindableEndpoint(t *testing.T) {
	t.Parallel()

	hits, baseURL := searchHitServer(t)
	apiSpec := searchCLISpec("bracket-search", baseURL, map[string]spec.Resource{
		"short": searchCLIEndpoint("GET", "/x/{id}/search", []spec.Param{
			{Name: "id", Type: "string", In: "path", Required: true, Positional: true, PathParam: true},
		}, nil),
		"contacts": searchCLIEndpoint("GET", "/contacts/search", []spec.Param{
			{Name: "filter[query]", Type: "string", In: "query"},
		}, nil),
		"scoped": searchCLIEndpoint("GET", "/spaces/{spaceId}/contacts/search", []spec.Param{
			{Name: "spaceId", Type: "string", In: "path", Required: true, Positional: true, PathParam: true},
			{Name: "filter[query]", Type: "string", In: "query"},
		}, nil),
	}, searchCLITypes())

	dir, binary := buildSearchCLI(t, apiSpec)
	searchSrc := readGeneratedFile(t, dir, "internal", "cli", "search.go")
	require.Contains(t, searchSrc, `c.Get(cmd.Context(), "/contacts/search", map[string]string{`)
	require.Contains(t, searchSrc, `"filter[query]": query`)
	require.NotContains(t, searchSrc, `"q": query`)
	require.NotContains(t, searchSrc, "{id}")
	require.NotContains(t, searchSrc, "{spaceId}")

	stdout, stderr, err := runSearchCLI(t, binary, "search", "invoice", "--data-source", "live", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.Contains(t, stderr, baseURL+"/contacts/search")
	assert.Contains(t, stderr, "filter[query]=invoice")
	assertDryRunRequestHasNoPathTemplate(t, stderr)
	assert.NotContains(t, stderr, "?q=")
	assert.NotContains(t, stderr, "q=invoice")
}

func TestGeneratedSearchKeepsPlainQEndpoint(t *testing.T) {
	t.Parallel()

	hits, baseURL := searchHitServer(t)
	apiSpec := searchCLISpec("plain-search", baseURL, map[string]spec.Resource{
		"items": searchCLIEndpoint("GET", "/search", []spec.Param{
			{Name: "q", Type: "string", In: "query"},
		}, nil),
	}, searchCLITypes())

	dir, binary := buildSearchCLI(t, apiSpec)
	searchSrc := readGeneratedFile(t, dir, "internal", "cli", "search.go")
	require.Contains(t, searchSrc, `c.Get(cmd.Context(), "/search", map[string]string{`)
	require.Contains(t, searchSrc, `"q": query`)
	require.NotContains(t, searchSrc, "filter[query]")
	require.NotContains(t, searchSrc, "{id}")
	require.NotContains(t, searchSrc, "{spaceId}")

	stdout, stderr, err := runSearchCLI(t, binary, "search", "invoice", "--data-source", "live", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.Contains(t, stderr, baseURL+"/search")
	assert.Contains(t, stderr, "q=invoice")
	assert.NotContains(t, stderr, "filter[query]")
	assertDryRunRequestHasNoPathTemplate(t, stderr)
}

func TestGeneratedSearchDoesNotInventQWhenParamsAreNotSearch(t *testing.T) {
	t.Parallel()

	hits, baseURL := searchHitServer(t)
	apiSpec := searchCLISpec("limit-search", baseURL, map[string]spec.Resource{
		"items": searchCLIEndpoint("GET", "/search", []spec.Param{
			{Name: "limit", Type: "integer", In: "query"},
		}, nil),
		"contacts": searchCLIEndpoint("GET", "/contacts/search", []spec.Param{
			{Name: "query", Type: "string", In: "query"},
		}, nil),
	}, searchCLITypes())

	dir, binary := buildSearchCLI(t, apiSpec)
	searchSrc := readGeneratedFile(t, dir, "internal", "cli", "search.go")
	require.NotContains(t, searchSrc, `"q": query`)
	require.NotContains(t, searchSrc, "c.Get(")
	require.NotContains(t, searchSrc, "/contacts/search")
	require.Contains(t, searchSrc, `reason = "no_search_endpoint"`)

	dbPath := filepath.Join(t.TempDir(), "data.db")
	stdout, stderr, err := runSearchCLI(t, binary, "search", "invoice", "--data-source", "auto", "--json", "--db", dbPath)
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	meta := searchMeta(t, stdout)
	assert.Equal(t, "local", meta.Source)
	assert.Equal(t, "no_search_endpoint", meta.Reason)

	stdout, stderr, err = runSearchCLI(t, binary, "search", "invoice", "--data-source", "live", "--dry-run")
	require.Error(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.NotContains(t, stdout+stderr, "q=invoice")
	assert.NotContains(t, stdout+stderr, "/contacts/search")
	assert.Contains(t, stdout+stderr, "no search endpoint")
}

func TestGeneratedPostSearchSendsBracketedBodyField(t *testing.T) {
	t.Parallel()

	hits, baseURL := searchHitServer(t)
	apiSpec := searchCLISpec("post-search", baseURL, map[string]spec.Resource{
		"items": searchCLIEndpoint("POST", "/search", nil, []spec.Param{
			{Name: "filter[query]", Type: "string"},
			{Name: "limit", Type: "integer", Required: true},
		}),
	}, searchCLITypes())

	dir, binary := buildSearchCLI(t, apiSpec)
	searchSrc := readGeneratedFile(t, dir, "internal", "cli", "search.go")
	require.Contains(t, searchSrc, `c.Post(cmd.Context(), "/search", map[string]any{`)
	require.Contains(t, searchSrc, `"filter[query]": query`)
	require.Regexp(t, `"limit":\s+10\b`, searchSrc)
	require.NotContains(t, searchSrc, `"q": query`)

	stdout, stderr, err := runSearchCLI(t, binary, "search", "invoice", "--data-source", "live", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assertSearchDidNotDial(t, hits)
	assert.Contains(t, stderr, "POST "+baseURL+"/search")
	assert.Contains(t, stderr, `"filter[query]": "invoice"`)
	assert.Contains(t, stderr, `"limit": 10`)
	assert.NotContains(t, stderr, `"q"`)
	assertDryRunRequestHasNoPathTemplate(t, stderr)
}

func assertDryRunRequestHasNoPathTemplate(t *testing.T, stderr string) {
	t.Helper()
	var requestLine string
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "GET ") || strings.HasPrefix(line, "POST ") {
			requestLine = line
			break
		}
	}
	require.NotEmpty(t, requestLine, "dry-run stderr missing request line:\n%s", stderr)
	assert.NotContains(t, requestLine, "{")
	assert.NotContains(t, requestLine, "}")
}

func searchHitServer(t *testing.T) (*atomic.Int32, string) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return &hits, server.URL
}

func assertSearchDidNotDial(t *testing.T, hits *atomic.Int32) {
	t.Helper()
	assert.Zero(t, hits.Load(), "generic search must not dial when the live endpoint is unusable or dry-run")
}

func searchCLISpec(name, baseURL string, resources map[string]spec.Resource, types map[string]spec.TypeDef) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Printer = "tester"
	apiSpec.BaseURL = baseURL
	apiSpec.Resources = resources
	apiSpec.Types = types
	// A list endpoint keeps the generic sync command, which is what allows
	// the search vision template to be emitted. Search-only resources are
	// not a store population source, and requesting sync then strips search.
	if _, ok := apiSpec.Resources["records"]; !ok {
		apiSpec.Resources["records"] = spec.Resource{
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
				},
			},
		}
	}
	return apiSpec
}

func searchCLITypes() map[string]spec.TypeDef {
	return map[string]spec.TypeDef{
		"Space":   {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Contact": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Item":    {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
	}
}

func searchCLIEndpoint(method, path string, params, body []spec.Param) spec.Resource {
	return spec.Resource{
		Description: "Searchable items",
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method:      method,
				Path:        path,
				Description: "Search items",
				Params:      params,
				Body:        body,
				Response:    spec.ResponseDef{Type: "array", Item: "Item"},
			},
		},
	}
}

func buildSearchCLI(t *testing.T, apiSpec *spec.APISpec) (string, string) {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	// MCP emits internal/mcp, which the always-generated server imports.
	// Without it that package is empty and go build ./... cannot compile.
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)
	// go build appends .exe on Windows; the launch path has to match.
	binary := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binary, "./cmd/"+naming.CLI(apiSpec.Name))
	require.FileExists(t, binary)
	return outputDir, binary
}

func runSearchCLI(t *testing.T, binary string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = harnessFreeEnv(sandboxHomeEnv(t)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

type searchEnvelopeMeta struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

func searchMeta(t *testing.T, stdout string) searchEnvelopeMeta {
	t.Helper()
	var envelope struct {
		Meta searchEnvelopeMeta `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), stdout)
	return envelope.Meta
}
