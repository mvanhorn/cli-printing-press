package generator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGlobalScopeParamDefaultsFromEnv(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seenQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users", r.URL.Path)
		mu.Lock()
		seenQueries = append(seenQueries, r.URL.Query().Encode())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"user-1","name":"Ada"}]`)
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("cipp")
	apiSpec.BaseURL = server.URL
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Types = map[string]spec.TypeDef{
		"User": {
			Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}},
		},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"users": {
			Description: "Manage users",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/users",
					Description: "List users",
					Response:    spec.ResponseDef{Type: "array", Item: "User"},
					Pagination:  &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "cursor"},
					Params: []spec.Param{{
						Name:        "TenantFilter",
						Type:        "string",
						Description: "Tenant scope",
						Required:    true,
						GlobalScope: true,
					}, {
						Name:        "limit",
						Type:        "integer",
						Description: "Maximum results",
					}},
				},
				"get": {
					Method:      "GET",
					Path:        "/users/{id}",
					Description: "Get user",
					Params: []spec.Param{{
						Name:        "id",
						Type:        "string",
						Required:    true,
						Positional:  true,
						Description: "User ID",
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	contentBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "users_list.go"))
	require.NoError(t, err)
	content := string(contentBytes)

	assert.Contains(t, content, `func newUsersListCmd`)
	assert.Contains(t, content, `StringVar(&flagTenantFilter, "tenant-filter", globalScopeParamDefault("CIPP_TENANT_FILTER", ""), "Tenant scope (env: CIPP_TENANT_FILTER)")`)
	assert.Contains(t, content, `!cmd.Flags().Changed("tenant-filter") && flagTenantFilter == ""`)
	assert.Contains(t, content, `"TenantFilter": formatCLIParamValue(flagTenantFilter)`)
	assert.NotContains(t, strings.ToLower(content), `markflagrequired`)

	syncContent := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncContent, `func applySyncGlobalScopeEnvDefaults(userParams *syncUserParams)`)
	assert.Contains(t, syncContent, `globalScopeParamDefault("CIPP_TENANT_FILTER", "")`)
	assert.Contains(t, syncContent, `userParams.setGlobalDefault("TenantFilter", v)`)
	assert.Contains(t, syncContent, `"users": "/users"`)

	binaryPath := filepath.Join(outputDir, "cipp-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/cipp-pp-cli")

	cmd := exec.Command(binaryPath, "users", "list", "--json", "--limit", "10")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), `required flag "tenant-filter" not set`)

	cmd = exec.Command(binaryPath, "users", "list", "--json", "--limit", "10")
	cmd.Env = append(os.Environ(), "CIPP_TENANT_FILTER=tenant-a")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dbPath := filepath.Join(t.TempDir(), "sync.db")
	cmd = exec.Command(binaryPath, "--json", "sync", "--resources", "users", "--max-pages", "1", "--db", dbPath)
	cmd.Env = append(os.Environ(), "CIPP_TENANT_FILTER=tenant-b")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seenQueries, 2)
	assert.Contains(t, seenQueries[0], "TenantFilter=tenant-a")
	assert.Contains(t, seenQueries[1], "TenantFilter=tenant-b")
}
