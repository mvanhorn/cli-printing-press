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
