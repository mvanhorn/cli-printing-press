package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateProjectsCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		specPath      string
		expectedFiles int
	}{
		{name: "stytch", specPath: filepath.Join("..", "..", "testdata", "stytch.yaml"), expectedFiles: 30},
		{name: "clerk", specPath: filepath.Join("..", "..", "testdata", "clerk.yaml"), expectedFiles: 35},
		{name: "loops", specPath: filepath.Join("..", "..", "testdata", "loops.yaml"), expectedFiles: 33},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiSpec, err := spec.Parse(tt.specPath)
			require.NoError(t, err)

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			require.Equal(t, tt.expectedFiles, countFiles(t, outputDir))

			runGoCommand(t, outputDir, "mod", "tidy")
			runGoCommand(t, outputDir, "build", "./...")

			binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
			runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

			info, err := os.Stat(binaryPath)
			require.NoError(t, err)
			require.False(t, info.IsDir())
			require.NotZero(t, info.Size())
		})
	}
}

func TestGenerateOAuth2AuthTemplateConditionally(t *testing.T) {
	t.Parallel()

	t.Run("oauth2 spec includes auth command", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
		require.NoError(t, err)

		apiSpec, err := openapi.Parse(data)
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
	})

	t.Run("non-oauth2 spec generates simple auth command", func(t *testing.T) {
		apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		// auth.go is always generated (simple token management for non-OAuth specs)
		_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
	})
}

func countFiles(t *testing.T, root string) int {
	t.Helper()

	total := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if !d.IsDir() {
			total++
		}
		return nil
	})
	require.NoError(t, err)
	return total
}

func runGoCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cacheDir, err := goBuildCacheDir(dir)
	require.NoError(t, err)
	cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

// --- Unit 1: Template Regression Tests ---

func TestGenerateWithNoAuth(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "noauth",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "",
			EnvVars: nil,
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/noauth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List all items",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "noauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())
	require.NoError(t, gen.Validate())
	assert.NoFileExists(t, filepath.Join(outputDir, naming.ValidationBinary("noauth")))
}

func TestGenerateWithOwnerField(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "owned",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Owner:   "testowner",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"OWNED_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/owned-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"things": {
				Description: "Manage things",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/things",
						Description: "List things",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "owned-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	// Module path uses bare CLI name (no github.com/owner prefix)
	assert.Contains(t, string(gomod), "module owned-pp-cli")
	// Owner is still used for copyright
	mainGo, err := os.ReadFile(filepath.Join(outputDir, "cmd", "owned-pp-cli", "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(mainGo), "testowner")
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "go install github.com/testowner/owned-pp-cli/cmd/owned-pp-cli@latest")
}

func TestGenerateWithEmptyOwner(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "unowned",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Owner:   "",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"UNOWNED_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/unowned-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Manage widgets",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/widgets/{id}",
						Description: "Get a widget",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "unowned-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	// Module path uses bare CLI name regardless of Owner
	assert.Contains(t, string(gomod), "module unowned-pp-cli")
	// Module line should not have a github.com prefix
	assert.NotContains(t, string(gomod), "module github.com/")
}

// --- Unit 7: Feature Verification Tests ---

func generatePetstore(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	apiSpec, err := openapi.Parse(data)
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	return outputDir
}

func TestGeneratedOutput_HasSelectFlag(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(rootGo), "select"), "root.go should contain the --select flag")
}

func TestGeneratedOutput_HasErrorHints(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(helpersGo), "hint:"), "helpers.go should contain error hints")
}

func TestGeneratedOutput_HasGenerationComment(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	// Find the actual cmd directory (name derived from spec title)
	entries, err := os.ReadDir(filepath.Join(outputDir, "cmd"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "cmd/ should have at least one subdirectory")
	mainGo, err := os.ReadFile(filepath.Join(outputDir, "cmd", entries[0].Name(), "main.go"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(mainGo), "Generated by CLI Printing Press"), "main.go should contain generation comment")
}

func TestGeneratedOutput_READMEHasQuickStart(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)
	assert.Contains(t, content, "Quick Start")
	assert.Contains(t, content, "Output Formats")
	assert.Contains(t, content, "Agent Usage")
}

func TestGeneratedOutput_READMESourcesSection(t *testing.T) {
	t.Parallel()

	minSpec := &spec.APISpec{
		Name:    "testapi",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"TESTAPI_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/testapi-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {Description: "Items", Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
			}},
		},
	}

	t.Run("sources section appears with 2+ sources", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.Sources = []ReadmeSource{
			{Name: "big-tool", URL: "https://github.com/org/big-tool", Language: "python", Stars: 5000},
			{Name: "small-tool", URL: "https://github.com/org/small-tool", Language: "go", Stars: 100},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		assert.Contains(t, content, "## Sources & Inspiration")
		assert.Contains(t, content, "[**big-tool**](https://github.com/org/big-tool)")
		assert.Contains(t, content, "5000 stars")
		assert.Contains(t, content, "[**small-tool**](https://github.com/org/small-tool)")
	})

	t.Run("sources section omitted with 0-1 sources", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.Sources = []ReadmeSource{
			{Name: "only-one", URL: "https://github.com/org/only-one", Language: "go", Stars: 50},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		assert.NotContains(t, string(readme), "Sources & Inspiration")
	})

	t.Run("sources section omitted with no sources", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		assert.NotContains(t, string(readme), "Sources & Inspiration")
	})

	t.Run("discovery pages shown even with 0 sources", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.DiscoveryPages = []string{"https://example.com/app", "https://example.com/dashboard"}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		assert.Contains(t, content, "## Sources & Inspiration")
		assert.Contains(t, content, "https://example.com/app")
		assert.Contains(t, content, "https://example.com/dashboard")
		assert.Contains(t, content, "Discovery")
	})

	t.Run("source with missing language omits language", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.Sources = []ReadmeSource{
			{Name: "tool-a", URL: "https://github.com/org/a", Stars: 100},
			{Name: "tool-b", URL: "https://github.com/org/b", Language: "go", Stars: 50},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		assert.Contains(t, content, "[**tool-a**](https://github.com/org/a)")
		assert.NotContains(t, content, "tool-a**](https://github.com/org/a) — ")
	})

	t.Run("section appears before Generated by footer", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.Sources = []ReadmeSource{
			{Name: "a", URL: "https://github.com/org/a", Stars: 100},
			{Name: "b", URL: "https://github.com/org/b", Stars: 50},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		sourcesIdx := strings.Index(content, "Sources & Inspiration")
		footerIdx := strings.Index(content, "Generated by")
		assert.Greater(t, footerIdx, sourcesIdx, "Sources section should appear before Generated by footer")
	})
}

func TestGeneratedOutput_MutatingCommandsHaveEnvelope(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	// POST command should have confirmation envelope
	addGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pet_add.go"))
	require.NoError(t, err)
	content := string(addGo)
	assert.Contains(t, content, `envelope := map[string]any{`)
	assert.Contains(t, content, `"action":`)
	assert.Contains(t, content, `"resource":`)
	assert.Contains(t, content, `"status":   statusCode`)
	assert.Contains(t, content, `"success":  statusCode >= 200 && statusCode < 300`)
	// Envelope fires on both --json and auto-JSON (piped/non-TTY)
	assert.Contains(t, content, `flags.asJSON || !isTerminal(cmd.OutOrStdout())`)

	// --quiet is respected before envelope output
	assert.Contains(t, content, "if flags.quiet {")

	// --select and --compact are applied to inner data before wrapping in envelope
	assert.Contains(t, content, "filtered := data")
	assert.Contains(t, content, "compactFields(filtered)")
	assert.Contains(t, content, "filterFields(filtered, flags.selectFields)")
	assert.Contains(t, content, `json.Unmarshal(filtered, &parsed)`)

	// Envelope bypasses printOutputWithFlags to avoid double-filtering
	assert.Contains(t, content, `printOutput(cmd.OutOrStdout(), json.RawMessage(envelopeJSON), true)`)

	// Dry-run is flagged honestly in the envelope
	assert.Contains(t, content, `flags.dryRun`)
	assert.Contains(t, content, `envelope["dry_run"] = true`)
	assert.Contains(t, content, `envelope["status"] = 0`)
	assert.Contains(t, content, `envelope["success"] = false`)
}

func TestGeneratedOutput_GetCommandsLackEnvelope(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	// GET command should NOT have confirmation envelope
	getGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pet_get-by-id.go"))
	require.NoError(t, err)
	content := string(getGo)
	assert.NotContains(t, content, "envelope")
	assert.NotContains(t, content, "statusCode")
}

// --- Unit 2: Proxy Route Data Propagation Tests ---

func TestGeneratedOutput_ProxyEnvelopeRouting(t *testing.T) {
	t.Parallel()

	t.Run("proxy-envelope with routes emits serviceForPath routing", func(t *testing.T) {
		t.Parallel()

		apiSpec := &spec.APISpec{
			Name:          "proxyapi",
			Version:       "0.1.0",
			BaseURL:       "https://example.com/proxy",
			ClientPattern: "proxy-envelope",
			ProxyRoutes: map[string]string{
				"/search-all": "search",
				"/v1/api":     "publishing",
				"/v2/api":     "publishing",
			},
			Auth: spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PROXY_API_KEY"}},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/proxyapi-pp-cli/config.toml",
			},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/v1/api/items", Description: "List items"},
					},
				},
				"search": {
					Description: "Search",
					Endpoints: map[string]spec.Endpoint{
						"all": {Method: "POST", Path: "/search-all", Description: "Search all"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "proxyapi-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
		require.NoError(t, err)
		content := string(clientGo)

		// Proxy-envelope template should be active
		assert.Contains(t, content, "proxyEnvelope")
		assert.Contains(t, content, "serviceForPath")

		// Route table should contain the configured prefixes
		assert.Contains(t, content, `"/search-all": "search"`)
		assert.Contains(t, content, `"/v1/api": "publishing"`)
		assert.Contains(t, content, `"/v2/api": "publishing"`)

		// Default fallback uses API name
		assert.Contains(t, content, `return "proxyapi"`)
	})

	t.Run("proxy-envelope without routes falls back to API name", func(t *testing.T) {
		t.Parallel()

		apiSpec := &spec.APISpec{
			Name:          "proxybare",
			Version:       "0.1.0",
			BaseURL:       "https://example.com/proxy",
			ClientPattern: "proxy-envelope",
			// ProxyRoutes intentionally nil
			Auth: spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PROXY_API_KEY"}},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/proxybare-pp-cli/config.toml",
			},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/items", Description: "List items"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "proxybare-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
		require.NoError(t, err)
		content := string(clientGo)

		// Proxy-envelope template should still be active
		assert.Contains(t, content, "proxyEnvelope")
		assert.Contains(t, content, "serviceForPath")

		// No route table entries (nil map range produces nothing)
		assert.NotContains(t, content, `bestMatch`)

		// Default fallback uses API name
		assert.Contains(t, content, `return "proxybare"`)
	})

	t.Run("standard REST spec omits proxy-envelope code", func(t *testing.T) {
		t.Parallel()

		apiSpec := &spec.APISpec{
			Name:    "restapi",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			// ClientPattern empty = default REST
			Auth: spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"REST_API_KEY"}},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/restapi-pp-cli/config.toml",
			},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/items", Description: "List items"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "restapi-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
		require.NoError(t, err)
		content := string(clientGo)

		// Standard REST client should NOT contain proxy-envelope constructs
		assert.NotContains(t, content, "proxyEnvelope")
		assert.NotContains(t, content, "serviceForPath")
		assert.NotContains(t, content, "buildProxyPath")
	})
}

// --- Unit 5: UpsertBatch Entity Method Tests ---

// highGravitySpec builds an APISpec with a high-gravity "collections" resource
// (gravity >= 8) that triggers per-entity batch method generation, plus a
// low-gravity "tags" resource that should NOT get a batch method.
func highGravitySpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "batchtest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"BATCH_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/batchtest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"collections": {
				Description: "Manage collections",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/collections", Description: "List collections", Params: highGravityParams()},
					"get":    {Method: "GET", Path: "/collections/{id}", Description: "Get collection", Params: highGravityParams()},
					"create": {Method: "POST", Path: "/collections", Description: "Create collection", Body: highGravityParams()},
					"update": {Method: "PUT", Path: "/collections/{id}", Description: "Update collection", Body: highGravityParams()},
					"delete": {Method: "DELETE", Path: "/collections/{id}", Description: "Delete collection"},
				},
			},
			"tags": {
				Description: "Manage tags",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/tags", Description: "List tags"},
				},
			},
		},
	}
}

func highGravityParams() []spec.Param {
	return []spec.Param{
		{Name: "name", Type: "string"},
		{Name: "title", Type: "string"},
		{Name: "description", Type: "string"},
		{Name: "content", Type: "string"},
		{Name: "status", Type: "string"},
		{Name: "workspace_id", Type: "string"},
		{Name: "owner_id", Type: "string"},
		{Name: "created_at", Type: "string", Format: "date-time"},
		{Name: "updated_at", Type: "string", Format: "date-time"},
		{Name: "item_count", Type: "integer"},
	}
}

func TestGeneratedOutput_UpsertBatchEntityMethods(t *testing.T) {
	t.Parallel()

	t.Run("high-gravity entity gets UpsertBatch method", func(t *testing.T) {
		t.Parallel()

		apiSpec := highGravitySpec()
		outputDir := filepath.Join(t.TempDir(), "batchtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
		require.NoError(t, err)
		content := string(storeGo)

		// High-gravity entity should have a typed batch method
		assert.Contains(t, content, "func (s *Store) UpsertBatchCollections(items []json.RawMessage) (int, error)")

		// Batch method should use typed INSERT into the entity table
		assert.Contains(t, content, "INSERT INTO collections")

		// Batch method should also upsert into generic resources table
		assert.Contains(t, content, `upsertGenericResourceTx(tx, "collections"`)

		// Batch method should return count
		assert.Contains(t, content, "return count, nil")

		// Generic UpsertBatch should still exist
		assert.Contains(t, content, "func (s *Store) UpsertBatch(resourceType string, items []json.RawMessage) error")
	})

	t.Run("low-gravity entity does not get UpsertBatch method", func(t *testing.T) {
		t.Parallel()

		apiSpec := highGravitySpec()
		outputDir := filepath.Join(t.TempDir(), "batchtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
		require.NoError(t, err)
		content := string(storeGo)

		// Low-gravity "tags" entity should NOT have a typed batch method
		assert.NotContains(t, content, "UpsertBatchTags")
	})

	t.Run("batch method compiles for high-gravity spec", func(t *testing.T) {
		t.Parallel()

		apiSpec := highGravitySpec()
		outputDir := filepath.Join(t.TempDir(), "batchtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		runGoCommand(t, outputDir, "mod", "tidy")
		runGoCommand(t, outputDir, "build", "./...")
	})
}

// --- buildSyncResources Tests ---

func TestBuildSyncResources(t *testing.T) {
	t.Parallel()

	t.Run("nil profile returns nil", func(t *testing.T) {
		t.Parallel()
		g := &Generator{Spec: &spec.APISpec{}}
		assert.Nil(t, g.buildSyncResources())
	})

	t.Run("empty SyncableResources returns nil", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec:    &spec.APISpec{},
			profile: &profiler.APIProfile{SyncableResources: []string{}},
		}
		assert.Nil(t, g.buildSyncResources())
	})

	t.Run("resource not in spec gets fallback path", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{Resources: map[string]spec.Resource{}},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"orphan"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, "orphan", result[0].Name)
		assert.Equal(t, "/orphan", result[0].Path)
		assert.Equal(t, "", result[0].PaginationType)
		assert.Equal(t, 100, result[0].DefaultLimit)
	})

	t.Run("resource with no GET+Pagination gets fallback", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"items": {
						Endpoints: map[string]spec.Endpoint{
							"create": {Method: "POST", Path: "/items"},
							"list":   {Method: "GET", Path: "/items"}, // no Pagination
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"items"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, "/items", result[0].Path) // fallback path
		assert.Equal(t, "", result[0].PaginationType)
	})

	t.Run("offset pagination extracted correctly", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"collections": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method: "GET",
								Path:   "/v1/api/networkentity",
								Pagination: &spec.Pagination{
									Type:       "offset",
									LimitParam: "limit",
									CursorParam: "offset",
								},
								ResponsePath: "data",
							},
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"collections"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, "collections", result[0].Name)
		assert.Equal(t, "/v1/api/networkentity", result[0].Path)
		assert.Equal(t, "offset", result[0].PaginationType)
		assert.Equal(t, "limit", result[0].LimitParam)
		assert.Equal(t, "offset", result[0].CursorParam)
		assert.Equal(t, "data", result[0].ResponsePath)
	})

	t.Run("cursor pagination extracted correctly", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"issues": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method: "GET",
								Path:   "/issues",
								Pagination: &spec.Pagination{
									Type:           "cursor",
									LimitParam:     "per_page",
									CursorParam:    "after",
									NextCursorPath: "next_cursor",
									HasMoreField:   "has_more",
								},
							},
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"issues"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, "cursor", result[0].PaginationType)
		assert.Equal(t, "per_page", result[0].LimitParam)
		assert.Equal(t, "after", result[0].CursorParam)
		assert.Equal(t, "next_cursor", result[0].NextCursorPath)
		assert.Equal(t, "has_more", result[0].HasMoreField)
	})

	t.Run("prefers list endpoint by name", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"items": {
						Endpoints: map[string]spec.Endpoint{
							"archived": {
								Method: "GET",
								Path:   "/items/archived",
								Pagination: &spec.Pagination{
									Type:       "cursor",
									CursorParam: "after",
								},
							},
							"list": {
								Method: "GET",
								Path:   "/items",
								Pagination: &spec.Pagination{
									Type:        "offset",
									CursorParam: "offset",
								},
							},
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"items"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, "/items", result[0].Path)
		assert.Equal(t, "offset", result[0].PaginationType)
	})

	t.Run("default limit from profiler", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"items": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/items", Pagination: &spec.Pagination{Type: "cursor"}},
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"items"},
				Pagination: profiler.PaginationProfile{DefaultPageSize: 50},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 1)
		assert.Equal(t, 50, result[0].DefaultLimit)
	})

	t.Run("multiple resources with mixed pagination", func(t *testing.T) {
		t.Parallel()
		g := &Generator{
			Spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"collections": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method: "GET", Path: "/collections",
								Pagination: &spec.Pagination{Type: "offset", LimitParam: "limit", CursorParam: "offset"},
								ResponsePath: "data",
							},
						},
					},
					"teams": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/teams", Pagination: &spec.Pagination{Type: "cursor", CursorParam: "after"}},
						},
					},
				},
			},
			profile: &profiler.APIProfile{
				SyncableResources: []string{"collections", "teams"},
			},
		}
		result := g.buildSyncResources()
		require.Len(t, result, 2)

		// Find each by name (order matches SyncableResources)
		assert.Equal(t, "offset", result[0].PaginationType)
		assert.Equal(t, "data", result[0].ResponsePath)
		assert.Equal(t, "cursor", result[1].PaginationType)
		assert.Equal(t, "", result[1].ResponsePath)
	})
}

// --- Pagination-Aware Sync Generation Tests ---

// paginatedSyncSpec builds a spec with multiple pagination types to test sync template output.
func paginatedSyncSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "paginationtest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PAG_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/paginationtest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"collections": {
				Description: "Browse collections",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET", Path: "/v1/collections", Description: "List collections",
						Params: highGravityParams(),
						Pagination: &spec.Pagination{
							Type: "offset", LimitParam: "limit", CursorParam: "offset",
						},
						ResponsePath: "data",
					},
					"get":    {Method: "GET", Path: "/collections/{id}", Description: "Get collection", Params: highGravityParams()},
					"create": {Method: "POST", Path: "/collections", Description: "Create", Body: highGravityParams()},
					"update": {Method: "PUT", Path: "/collections/{id}", Description: "Update", Body: highGravityParams()},
					"delete": {Method: "DELETE", Path: "/collections/{id}", Description: "Delete"},
				},
			},
			"teams": {
				Description: "Browse teams",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET", Path: "/v1/teams", Description: "List teams",
						Params: []spec.Param{{Name: "name", Type: "string"}, {Name: "description", Type: "string"}},
						Pagination: &spec.Pagination{
							Type: "cursor", LimitParam: "per_page", CursorParam: "after",
							NextCursorPath: "next_cursor", HasMoreField: "has_more",
						},
					},
				},
			},
			"categories": {
				Description: "Browse categories",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET", Path: "/v1/categories", Description: "List categories",
						Params: []spec.Param{{Name: "name", Type: "string"}},
						// No Pagination — single-page endpoint
					},
				},
			},
		},
	}
}

func TestGeneratedOutput_PaginationAwareSync(t *testing.T) {
	t.Parallel()

	t.Run("syncConfigs contains per-resource pagination metadata", func(t *testing.T) {
		t.Parallel()

		apiSpec := paginatedSyncSpec()
		outputDir := filepath.Join(t.TempDir(), "paginationtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
		require.NoError(t, err)
		content := string(syncGo)

		// Verify syncConfigs map exists
		assert.Contains(t, content, "syncConfigs")
		assert.Contains(t, content, "syncResourceConfig")

		// Verify offset-paginated resource
		assert.Contains(t, content, `"collections"`)
		assert.Contains(t, content, `paginationType: "offset"`)
		assert.Contains(t, content, `path:           "/v1/collections"`)
		assert.Contains(t, content, `responsePath:   "data"`)

		// Verify cursor-paginated resource
		assert.Contains(t, content, `"teams"`)
		assert.Contains(t, content, `paginationType: "cursor"`)
		assert.Contains(t, content, `cursorParam:    "after"`)
		assert.Contains(t, content, `nextCursorPath: "next_cursor"`)
		assert.Contains(t, content, `hasMoreField:   "has_more"`)
	})

	t.Run("generated sync compiles with populated syncConfigs", func(t *testing.T) {
		t.Parallel()

		apiSpec := paginatedSyncSpec()
		outputDir := filepath.Join(t.TempDir(), "paginationtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		runGoCommand(t, outputDir, "mod", "tidy")
		runGoCommand(t, outputDir, "build", "./...")
	})

	t.Run("upsertBatchForResource dispatches to entity-specific methods", func(t *testing.T) {
		t.Parallel()

		apiSpec := paginatedSyncSpec()
		outputDir := filepath.Join(t.TempDir(), "paginationtest-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
		require.NoError(t, err)
		content := string(syncGo)

		// Verify the batch dispatch function exists and routes to entity-specific methods
		assert.Contains(t, content, "func upsertBatchForResource")
		assert.Contains(t, content, "UpsertBatchCollections")
	})
}

// --- defaultDBPath Consolidation Tests ---

func TestGeneratedOutput_DefaultDBPathConsolidation(t *testing.T) {
	t.Parallel()

	apiSpec := paginatedSyncSpec()
	outputDir := filepath.Join(t.TempDir(), "dbpathtest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// helpers.go should have the single definition
	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersContent := string(helpersGo)
	assert.Contains(t, helpersContent, "func defaultDBPath()")
	assert.Contains(t, helpersContent, `".local", "share"`)

	// sync.go should call defaultDBPath(), not construct path inline
	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent, "defaultDBPath()")
	assert.NotContains(t, syncContent, `filepath.Join(home, ".local"`)

	// channel_workflow.go should call defaultDBPath(), not construct path inline
	workflowGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
	require.NoError(t, err)
	workflowContent := string(workflowGo)
	assert.Contains(t, workflowContent, "defaultDBPath()")
	assert.NotContains(t, workflowContent, `filepath.Join(home, ".config"`)
	// Help text should show the correct path
	assert.Contains(t, workflowContent, ".local/share/")
	assert.NotContains(t, workflowContent, "~/.config/")

	// analytics.go should call defaultDBPath()
	analyticsGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "analytics.go"))
	require.NoError(t, err)
	assert.Contains(t, string(analyticsGo), "defaultDBPath()")
}
