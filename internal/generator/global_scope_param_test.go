package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGlobalScopeParamRequiresExplicitFlag(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cipp")
	apiSpec.Resources = map[string]spec.Resource{
		"users": {
			Description: "Manage users",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/users",
					Description: "List users",
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
	assert.Contains(t, content, `StringVar(&flagTenantFilter, "tenant-filter", "", "Tenant scope")`)
	assert.Contains(t, content, `return fmt.Errorf("required flag \"%s\" not set", "tenant-filter")`)
	assert.Contains(t, content, `params["TenantFilter"] = fmt.Sprintf("%v", flagTenantFilter)`)
	assert.NotContains(t, content, `CIPP_TENANT_FILTER`)
	assert.NotContains(t, content, `defaults from`)
	assert.NotContains(t, strings.ToLower(content), `markflagrequired`)

	binaryPath := filepath.Join(outputDir, "cipp-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/cipp-pp-cli")

	cmd := exec.Command(binaryPath, "users", "list", "--json", "--limit", "10")
	cmd.Env = append(os.Environ(), "CIPP_TENANT_FILTER=tenant-a")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), `required flag "tenant-filter" not set`)
}
