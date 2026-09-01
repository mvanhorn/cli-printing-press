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

func TestGenerateDoesNotPromoteOrdinaryPathParamsToTemplateVars(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "unipile-posts",
		Version: "0.1.0",
		BaseURL: "https://api.unipile.example",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			EnvVars: []string{"UNIPILE_POSTS_TOKEN"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/unipile-posts-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/api/v1/posts", Description: "List posts"},
					"get": {
						Method:      "GET",
						Path:        "/api/v1/posts/{post_id}",
						Description: "Get a post",
						Params: []spec.Param{
							{Name: "post_id", Type: "string", Required: true, PathParam: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	assert.NotContains(t, apiSpec.EndpointTemplateVars, "post_id",
		"ordinary per-request path params must not become tenant template vars")
	assert.NotContains(t, apiSpec.SyncPathContextVars, "post_id",
		"listable collections must not promote item-id path params into sync path-context")

	_, err := os.Stat(filepath.Join(outputDir, "internal", "client", "url.go"))
	require.True(t, os.IsNotExist(err), "url.go must not be emitted for ordinary path params")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(readme), "UNIPILE_POSTS_POST_ID")

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateStillPromotesServerTemplateVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("shop-tenant")
	apiSpec.BaseURL = "https://{shop}.example.com"
	apiSpec.Resources["items"].Endpoints["get"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/{item_id}",
		Description: "Get item",
		Params: []spec.Param{
			{Name: "item_id", Type: "string", Required: true, PathParam: true, Positional: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	assert.Equal(t, []string{"shop"}, apiSpec.EndpointTemplateVars)
	assert.NotContains(t, apiSpec.EndpointTemplateVars, "item_id")

	urlSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "url.go"))
	require.NoError(t, err)
	assert.Contains(t, string(urlSrc), `"shop":`)
	assert.NotContains(t, string(urlSrc), `"item_id"`)
	requireGeneratedCompiles(t, outputDir)
}
