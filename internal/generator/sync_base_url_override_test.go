package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectiveRequestPathHonorsBaseURLOverrides(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		BaseURL: "https://webapi.example.com",
		Resources: map[string]spec.Resource{
			"listings": {
				BaseURL: "https://resource.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:  "GET",
						Path:    "/api/v1/listings",
						BaseURL: "https://www.example.com",
					},
					"get": {Method: "GET", Path: "/api/v1/listings/{id}"},
				},
			},
			"users": {
				BaseURL: "https://users.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users"},
				},
			},
			"root": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/root"},
				},
			},
			"parent": {
				SubResources: map[string]spec.Resource{
					"children": {
						BaseURL: "https://child.example.com",
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/parent/{id}/children"},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		resource string
		path     string
		method   string
		want     string
	}{
		{
			name:     "endpoint override wins over resource",
			resource: "listings",
			path:     "/api/v1/listings",
			method:   "GET",
			want:     "https://www.example.com/api/v1/listings",
		},
		{
			name:     "resource override",
			resource: "users",
			path:     "/users",
			method:   "GET",
			want:     "https://users.example.com/users",
		},
		{
			name:     "single-host path stays relative",
			resource: "root",
			path:     "/root",
			method:   "GET",
			want:     "/root",
		},
		{
			name:     "sub-resource override",
			resource: "children",
			path:     "/parent/{id}/children",
			method:   "GET",
			want:     "https://child.example.com/parent/{id}/children",
		},
		{
			name:     "unknown path unchanged",
			resource: "",
			path:     "/missing",
			method:   "GET",
			want:     "/missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, effectiveRequestPath(api, tt.resource, tt.path, tt.method))
		})
	}
}

func TestEffectiveRequestPathRefusesAmbiguousQueryIdentity(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		BaseURL: "https://webapi.example.com",
		Resources: map[string]spec.Resource{
			"gadgets": {
				BaseURL: "https://gadgets.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/query"},
				},
			},
			"widgets": {
				BaseURL: "https://widgets.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/query"},
				},
			},
		},
	}

	assert.Equal(t, "/query", effectiveRequestPath(api, "", "/query", ""),
		"empty resource+method must not pick the first sorted host")
	assert.Equal(t, "https://widgets.example.com/query",
		effectiveRequestPath(api, "widgets", "/query", "GET"))
	assert.Equal(t, "https://gadgets.example.com/query",
		effectiveRequestPath(api, "gadgets", "/query", "GET"))
}

func TestGeneratedQuerySyncHonorsPerResourceBaseURL(t *testing.T) {
	t.Parallel()

	var (
		widgetMu    sync.Mutex
		widgetHits  int
		gadgetMu    sync.Mutex
		gadgetHits  int
		widgetQuery string
		gadgetQuery string
	)
	widgets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		widgetMu.Lock()
		widgetHits++
		widgetQuery = r.URL.Query().Get("query")
		widgetMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"QueryResponse":{"Widget":[{"id":"w1","name":"w"}]}}`))
	}))
	t.Cleanup(widgets.Close)
	gadgets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gadgetMu.Lock()
		gadgetHits++
		gadgetQuery = r.URL.Query().Get("query")
		gadgetMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"QueryResponse":{"Gadget":[{"id":"g1","name":"g"}]}}`))
	}))
	t.Cleanup(gadgets.Close)

	apiSpec := &spec.APISpec{
		Name:    "querymultihost",
		Version: "0.1.0",
		BaseURL: "https://webapi.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/querymultihost-pp-cli/config.toml",
		},
		QuerySync: &spec.QuerySyncConfig{
			Path:          "/query",
			QueryParam:    "query",
			QueryTemplate: "select * from {entity} startposition {start} maxresults {limit}",
			PageSize:      2,
			EnvelopeKey:   "QueryResponse",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				BaseURL:     widgets.URL,
				Description: "Widgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/query",
						Description:  "Query widgets",
						Response:     spec.ResponseDef{Type: "array", Item: "Widget"},
						ResponsePath: "QueryResponse.Widget",
					},
				},
			},
			"gadgets": {
				BaseURL:     gadgets.URL,
				Description: "Gadgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/query",
						Description:  "Query gadgets",
						Response:     spec.ResponseDef{Type: "array", Item: "Gadget"},
						ResponsePath: "QueryResponse.Gadget",
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Widget": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
			"Gadget": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `"widgets": "`+widgets.URL+`/query"`)
	assert.Contains(t, syncSrc, `"gadgets": "`+gadgets.URL+`/query"`)
	assert.Contains(t, syncSrc, `queryPath     = "/query"`)
	assert.Contains(t, syncSrc, "func isQuerySyncPath(resource, path string) bool")
	assert.NotContains(t, syncSrc, `effectiveRequestPath .APISpec ""`)

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "querymultihost-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/querymultihost-pp-cli")

	for _, resource := range []string{"widgets", "gadgets"} {
		dbPath := filepath.Join(t.TempDir(), resource+".db")
		cmd := exec.Command(binaryPath, "sync", "--resources", resource, "--max-pages", "1", "--json", "--db", dbPath)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	widgetMu.Lock()
	defer widgetMu.Unlock()
	gadgetMu.Lock()
	defer gadgetMu.Unlock()
	assert.GreaterOrEqual(t, widgetHits, 1, "widgets sync must hit the widgets host")
	assert.GreaterOrEqual(t, gadgetHits, 1, "gadgets sync must hit the gadgets host")
	assert.Contains(t, widgetQuery, "select * from Widget")
	assert.Contains(t, gadgetQuery, "select * from Gadget")
}

func TestWithEffectiveSyncableRequestPathsRewritesHydratePath(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		BaseURL: "https://webapi.example.com",
		Resources: map[string]spec.Resource{
			"listings": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/listings", BaseURL: "https://www.example.com"},
					"get":  {Method: "GET", Path: "/listings/{id}", BaseURL: "https://www.example.com"},
				},
			},
		},
	}
	got := withEffectiveSyncableRequestPaths(api, []profiler.SyncableResource{{
		Name:        "listings",
		Path:        "/listings",
		Method:      "GET",
		HydratePath: "/listings/{id}",
	}})
	require.Len(t, got, 1)
	assert.Equal(t, "https://www.example.com/listings", got[0].Path)
	assert.Equal(t, "https://www.example.com/listings/{id}", got[0].HydratePath)
}

func TestGeneratedSyncHonorsEndpointBaseURLOverride(t *testing.T) {
	t.Parallel()

	var (
		overrideMu    sync.Mutex
		overridePaths []string
	)
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrideMu.Lock()
		overridePaths = append(overridePaths, r.URL.Path)
		overrideMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1","name":"hat"}]`))
	}))
	t.Cleanup(override.Close)

	var (
		rootMu    sync.Mutex
		rootPaths []string
	)
	root := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rootMu.Lock()
		rootPaths = append(rootPaths, r.URL.Path)
		rootMu.Unlock()
		http.Error(w, "Forbidden - wrong host", http.StatusForbidden)
	}))
	t.Cleanup(root.Close)

	apiSpec := multiHostListingsSpec("syncbaseurl", root.URL, override.URL)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `"listings": "`+override.URL+`/api/v1/listings"`,
		"syncResourcePath must emit the list endpoint's base_url override")
	assert.NotContains(t, syncSrc, `"listings": "/api/v1/listings"`,
		"sync must not fall back to the root-relative list path")

	listSrc := readGeneratedFile(t, outputDir, "internal", "cli", "listings_list.go")
	assert.Contains(t, listSrc, `path := "`+override.URL+`/api/v1/listings"`,
		"list command and sync must share the same override host")

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "func isAbsoluteURL(path string) bool",
		"per-endpoint base_url must emit the absolute-URL client branch")

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "syncbaseurl-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/syncbaseurl-pp-cli")

	listCmd := exec.Command(binaryPath, "listings", "list", "--json")
	listCmd.Env = append(os.Environ(), "SYNCBASEURL_BASE_URL="+root.URL)
	listOut, err := listCmd.CombinedOutput()
	require.NoError(t, err, string(listOut))
	var listResp any
	require.NoError(t, json.Unmarshal(listOut, &listResp), string(listOut))

	dbPath := filepath.Join(t.TempDir(), "sync.db")
	syncCmd := exec.Command(binaryPath, "sync", "--resources", "listings", "--max-pages", "1", "--json", "--db", dbPath)
	syncCmd.Env = append(os.Environ(), "SYNCBASEURL_BASE_URL="+root.URL)
	syncOut, err := syncCmd.CombinedOutput()
	require.NoError(t, err, string(syncOut))

	overrideMu.Lock()
	defer overrideMu.Unlock()
	assert.Contains(t, overridePaths, "/api/v1/listings",
		"list and sync should request the override host")
	assert.GreaterOrEqual(t, countStrings(overridePaths, "/api/v1/listings"), 2,
		"both list and sync should hit the override host")

	rootMu.Lock()
	defer rootMu.Unlock()
	assert.NotContains(t, rootPaths, "/api/v1/listings",
		"root base_url host must not receive the overridden list/sync request")
}

func TestGeneratedSyncKeepsRelativePathWithoutOverride(t *testing.T) {
	t.Parallel()

	apiSpec := multiHostListingsSpec("syncsinglehost", "https://webapi.example.com", "")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, `"listings": "/api/v1/listings"`,
		"single-host syncer should keep the root-relative path")
	assert.NotContains(t, syncSrc, `"listings": "https://webapi.example.com/api/v1/listings"`,
		"single-host syncer must not bake the root base_url into the path")
}

func multiHostListingsSpec(name, rootURL, overrideURL string) *spec.APISpec {
	list := spec.Endpoint{
		Method:      "GET",
		Path:        "/api/v1/listings",
		Description: "List listings",
		Response:    spec.ResponseDef{Type: "array", Item: "Listing"},
	}
	if overrideURL != "" {
		list.BaseURL = overrideURL
	}
	get := spec.Endpoint{
		Method:      "GET",
		Path:        "/api/v1/listings/{id}",
		Description: "Get a listing",
		Response:    spec.ResponseDef{Type: "object", Item: "Listing"},
	}
	if overrideURL != "" {
		get.BaseURL = overrideURL
	}
	return &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: rootURL,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + name + "-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"listings": {
				Description: "Listings",
				Endpoints: map[string]spec.Endpoint{
					"list": list,
					"get":  get,
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Listing": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
				},
			},
		},
	}
}

func countStrings(values []string, want string) int {
	n := 0
	for _, value := range values {
		if value == want {
			n++
		}
	}
	return n
}
