package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasAPIResourceParents(t *testing.T) {
	t.Parallel()

	specWithParents := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"orders": {Endpoints: map[string]spec.Endpoint{"list": {}, "create": {}}},
			"status": {Endpoints: map[string]spec.Endpoint{"get": {}}},
		},
	}
	assert.True(t, hasAPIResourceParents(specWithParents, map[string]bool{"status": true}))
	assert.False(t, hasAPIResourceParents(specWithParents, map[string]bool{"orders": true, "status": true}))
	assert.False(t, hasAPIResourceParents(&spec.APISpec{}, nil))
	assert.False(t, hasAPIResourceParents(nil, nil))
}

func TestGeneratedOutput_OmitsHollowAPIBrowserWhenOnlyPromotedLeaves(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "openalexshape",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"OA_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/openalexshape-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"works": {
				Description: "Works",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/works", Description: "List works"},
				},
			},
			"authors": {
				Description: "Authors",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/authors", Description: "List authors"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "openalexshape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "api_discovery.go"))
	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(rootSrc), "newAPICmd(")

	requireGeneratedCompiles(t, outputDir)
}
