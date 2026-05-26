package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGlobalScopeParamUsesEnvDefault(t *testing.T) {
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
						EnvVar:      "CIPP_TENANT_FILTER",
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	content := generatedCLISourceContaining(t, outputDir, "flagTenantFilter")

	assert.Contains(t, content, `StringVar(&flagTenantFilter, "tenant-filter", os.Getenv("CIPP_TENANT_FILTER"), "Tenant scope (defaults from CIPP_TENANT_FILTER)")`)
	assert.Contains(t, content, `if !cmd.Flags().Changed("tenant-filter") && flagTenantFilter == "" && !flags.dryRun {`)
	assert.Contains(t, content, `return fmt.Errorf("required flag \"%s\" not set (or set %s)", "tenant-filter", "CIPP_TENANT_FILTER")`)
	assert.Contains(t, content, `params["TenantFilter"] = fmt.Sprintf("%v", flagTenantFilter)`)
	assert.NotContains(t, strings.ToLower(content), `markflagrequired`)
}
