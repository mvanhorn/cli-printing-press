package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOutput_DeprecatedEndpointSurfacesInHelpAndMCP(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "resendshape",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"RS_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/resendshape-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"audiences": {
				Description: "Audiences",
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:      "POST",
						Path:        "/audiences",
						Description: "Create an audience",
						Deprecated:  true,
					},
					"list": {
						Method:      "GET",
						Path:        "/audiences",
						Description: "List audiences",
					},
				},
			},
			"concepts": {
				Description: "Deprecated concepts shortcut",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/concepts",
						Description: "List concepts",
						Deprecated:  true,
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "resendshape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	createSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "audiences_create.go"))
	require.NoError(t, err)
	assertDeprecatedCommandSurface(t, string(createSrc), "Create an audience")

	promotedSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_concepts.go"))
	require.NoError(t, err)
	assertDeprecatedCommandSurface(t, string(promotedSrc), "List concepts")

	listSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "audiences_list.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(listSrc), "(deprecated)")
	assert.NotContains(t, string(listSrc), `"pp:deprecated"`)

	toolsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	assert.Contains(t, string(toolsSrc), "Deprecated.")
	assert.Regexp(t, `audiences_create[\s\S]{0,400}Deprecated\.`, string(toolsSrc))

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "resendshape-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/resendshape-pp-cli")

	parentHelp, err := exec.Command(binaryPath, "audiences", "--help").Output()
	require.NoError(t, err)
	assert.Contains(t, string(parentHelp), "create")
	assert.Contains(t, string(parentHelp), "(deprecated)")
	assert.NotContains(t, string(parentHelp), "Command \"create\" is deprecated")

	createHelp, err := exec.Command(binaryPath, "audiences", "create", "--help").Output()
	require.NoError(t, err)
	assert.Contains(t, string(createHelp), "Deprecated:")
	assert.Contains(t, string(createHelp), "(deprecated)")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedOutput_NoDeprecatedMarkersWhenSpecHasNone(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "firecrawlshape",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"FC_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/firecrawlshape-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"scrape": {
				Description: "Scrape",
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/scrape", Description: "Scrape a URL"},
				},
			},
			"crawl": {
				Description: "Crawl",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/crawl", Description: "List crawls"},
					"create": {Method: "POST", Path: "/crawl", Description: "Start a crawl"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "firecrawlshape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	err := filepath.WalkDir(filepath.Join(outputDir, "internal", "cli"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)
		assert.NotContains(t, content, "(deprecated)", filepath.Base(path))
		assert.NotContains(t, content, `"pp:deprecated"`, filepath.Base(path))
		assert.NotRegexp(t, `(?m)^\s+Deprecated:`, content, filepath.Base(path))
		return nil
	})
	require.NoError(t, err)

	requireGeneratedCompiles(t, outputDir)
}

func TestParseAndGenerate_OpenAPIDeprecatedOperation(t *testing.T) {
	t.Parallel()

	parsed, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Deprecated Audiences API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /audiences:
    post:
      deprecated: true
      operationId: createAudience
      summary: Create an audience
      responses:
        "200":
          description: ok
    get:
      operationId: listAudiences
      summary: List audiences
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	var deprecatedCount int
	for _, resource := range parsed.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Deprecated {
				deprecatedCount++
				assert.Equal(t, "POST", endpoint.Method)
				assert.Equal(t, "/audiences", endpoint.Path)
			}
		}
	}
	require.Equal(t, 1, deprecatedCount)

	outputDir := filepath.Join(t.TempDir(), "deprecateparse-pp-cli")
	require.NoError(t, New(parsed, outputDir).Generate())

	createSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "audiences_create.go"))
	require.NoError(t, err)
	assertDeprecatedCommandSurface(t, string(createSrc), "Create an audience")
}

func assertDeprecatedCommandSurface(t *testing.T, src, summary string) {
	t.Helper()
	assert.Contains(t, src, summary+" (deprecated)")
	assert.Contains(t, src, `"pp:deprecated": "true"`)
	assert.Contains(t, src, "Deprecated: this operation is marked deprecated in the API spec.")
	assert.NotRegexp(t, `(?m)^\s+Deprecated:`, src)
	assert.NotRegexp(t, regexp.MustCompile(`Deprecated:\s+"[^"]+"`), src)
}
