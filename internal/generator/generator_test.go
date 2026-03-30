package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/openapi"
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
