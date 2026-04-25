package generator

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/internal/graphql"
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
		// +3 for cliutil package: fanout.go, text.go, cliutil_test.go
		// +1 for internal/cli/agent_context.go (Cloudflare-style runtime introspection)
		// +1 for internal/cli/profile.go (HeyGen-style named-profile system)
		// +1 for internal/cli/deliver.go (HeyGen-style --deliver output routing)
		// +1 for internal/cli/feedback.go (HeyGen-style in-band agent feedback channel)
		// +1 for internal/store/schema_version_test.go (PRAGMA user_version gate, discrawl-inspired)
		// +2 for internal/cli/which.go + which_test.go (capability-to-command resolver)
		// +1 for internal/store/upsert_batch_test.go (regression for issue #268: typed-table dispatch)
		{name: "stytch", specPath: filepath.Join("..", "..", "testdata", "stytch.yaml"), expectedFiles: 44},
		{name: "clerk", specPath: filepath.Join("..", "..", "testdata", "clerk.yaml"), expectedFiles: 49},
		{name: "loops", specPath: filepath.Join("..", "..", "testdata", "loops.yaml"), expectedFiles: 46},
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

// TestGenerateCliutilPackage verifies that every generated CLI ships with
// the shared internal/cliutil package (fanout + CleanText) and that its
// tests pass. This is the structural backstop for the Wave A plan's R1
// (fan-out helper) and R2 (text normalization) requirements — the package
// exists for agent-authored novel code to import as cliutil.FanoutRun /
// cliutil.CleanText without colliding with symbols in package cli.
func TestGenerateCliutilPackage(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// All three cliutil files must be emitted.
	cliutilDir := filepath.Join(outputDir, "internal", "cliutil")
	for _, name := range []string{"fanout.go", "text.go", "cliutil_test.go"} {
		_, err := os.Stat(filepath.Join(cliutilDir, name))
		require.NoError(t, err, "expected %s to be emitted", name)
	}

	// Rendered source must contain the key exported symbols so agent-authored
	// callers can rely on them being present.
	for _, probe := range []struct {
		file    string
		snippet string
	}{
		{"fanout.go", "func FanoutRun["},
		{"fanout.go", "func FanoutReportErrors("},
		{"fanout.go", "func WithConcurrency("},
		{"fanout.go", "type FanoutError struct"},
		{"fanout.go", "type FanoutResult["},
		{"text.go", "func CleanText("},
	} {
		data, err := os.ReadFile(filepath.Join(cliutilDir, probe.file))
		require.NoError(t, err)
		assert.Contains(t, string(data), probe.snippet, "%s missing %q", probe.file, probe.snippet)
	}

	// The generated cliutil package must compile and its tests must pass.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

// TestGenerateFreshnessHelperEmitted verifies that the cliutil freshness
// helper and auto-refresh wrapper are emitted when the spec opts into
// cache, and that the resulting CLI compiles end-to-end and its cliutil
// tests pass.
func TestGenerateFreshnessHelperEmitted(t *testing.T) {
	t.Parallel()

	// Start from stytch (has resources -> has store) and flip cache on.
	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Cache = spec.CacheConfig{
		Enabled:    true,
		StaleAfter: "6h",
		Commands: []spec.CacheCommand{
			{Name: "dashboard", Resources: []string{"users"}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	cliutilDir := filepath.Join(outputDir, "internal", "cliutil")
	for _, name := range []string{"freshness.go", "freshness_test.go"} {
		_, err := os.Stat(filepath.Join(cliutilDir, name))
		require.NoError(t, err, "expected %s to be emitted when cache is enabled", name)
	}

	// auto_refresh.go wires EnsureFresh into the root command.
	autoRefreshPath := filepath.Join(outputDir, "internal", "cli", "auto_refresh.go")
	data, err := os.ReadFile(autoRefreshPath)
	require.NoError(t, err, "auto_refresh.go must be emitted when cache is enabled")
	src := string(data)
	for _, snippet := range []string{
		"var readCommandResources = map[string][]string{",
		"func cachePolicy() cliutil.Policy",
		"func autoRefreshIfStale(",
		"func ensureFreshForResources(",
		"func ensureFreshForCommand(",
		"func runAutoRefresh(",
		`"stytch-pp-cli dashboard": {`,
		`"users",`,
		// Env opt-out is derived at runtime from the CLI name; probe the
		// expression that yields e.g. "STYTCH_NO_AUTO_REFRESH".
		`strings.ReplaceAll(strings.ToUpper("stytch"), "-", "_") + "_NO_AUTO_REFRESH"`,
	} {
		assert.Contains(t, src, snippet, "auto_refresh.go missing %q", snippet)
	}
	optOutIndex := strings.Index(src, "env_opt_out")
	openStoreIndex := strings.Index(src, "store.Open(dbPath)")
	require.NotEqual(t, -1, optOutIndex, "auto_refresh.go must report env opt-out")
	require.NotEqual(t, -1, openStoreIndex, "auto_refresh.go must open the store after opt-out checks")
	assert.Less(t, optOutIndex, openStoreIndex, "env opt-out must be checked before opening/migrating the store")

	dataSource, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "data_source.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(dataSource), "freshness_checked",
		"auto mode must stay API-first because local reads do not apply filters/scopes")

	// Root command must wire the hook.
	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "flags.freshnessMeta = autoRefreshIfStale(cmd.Context(), &flags, resources)",
		"root.go must invoke autoRefreshIfStale from PersistentPreRunE")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "## Freshness")
	assert.Contains(t, string(readme), "meta.freshness")
	assert.Contains(t, string(readme), "`stytch-pp-cli dashboard`")

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skill), "## Freshness Contract")
	assert.Contains(t, string(skill), "Covered paths:")

	// Generated helper must compile and its tests must pass end-to-end,
	// exercising the sync_state contract against a real SQLite DB.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

func TestGenerateFreshnessRejectsGeneratedCommandCollision(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Cache = spec.CacheConfig{
		Enabled: true,
		Commands: []spec.CacheCommand{
			{Name: "users list", Resources: []string{"users"}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	err = gen.Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already covered by generated resource freshness")
}

// TestGenerateShareEmittedWhenEnabled verifies the end-to-end share
// surface: share package, share commands, and the share subcommand
// registered on the root command. Exercises the generated share_test.go
// to confirm the round-trip export → import contract holds.
func TestGenerateShareEmittedWhenEnabled(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Cache = spec.CacheConfig{Enabled: true, StaleAfter: "6h"}
	apiSpec.Share = spec.ShareConfig{
		Enabled:        true,
		SnapshotTables: []string{"users", "sync_state"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// share package is emitted under internal/share.
	for _, name := range []string{"share.go", "share_test.go"} {
		_, err := os.Stat(filepath.Join(outputDir, "internal", "share", name))
		require.NoError(t, err, "expected internal/share/%s to be emitted when share is enabled", name)
	}

	// share cobra commands are emitted under internal/cli.
	shareCmdsPath := filepath.Join(outputDir, "internal", "cli", "share_commands.go")
	data, err := os.ReadFile(shareCmdsPath)
	require.NoError(t, err)
	for _, snippet := range []string{
		"func newShareCmd",
		"func newShareExportCmd",
		"func newShareImportCmd",
		"func newSharePublishCmd",
		"func newShareSubscribeCmd",
	} {
		assert.Contains(t, string(data), snippet, "share_commands.go missing %q", snippet)
	}

	// root.go registers the share parent command.
	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "rootCmd.AddCommand(newShareCmd(&flags))",
		"root.go must register newShareCmd when share is enabled")

	// The generated share package tests must compile and pass; this is
	// the round-trip safety net for the Unit 5 contract.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/share/...")
}

// TestGenerateShareSkippedWhenDisabled confirms share is not emitted for
// CLIs that don't opt in. Matters because share.go imports git via
// os/exec and pulls in a new emission path; absent specs should carry
// none of that overhead.
func TestGenerateShareSkippedWhenDisabled(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	require.False(t, apiSpec.Share.Enabled)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	for _, name := range []string{
		filepath.Join("internal", "share", "share.go"),
		filepath.Join("internal", "cli", "share_commands.go"),
	} {
		_, err := os.Stat(filepath.Join(outputDir, name))
		assert.True(t, os.IsNotExist(err), "%s must not be emitted when share is disabled", name)
	}
}

// TestGenerateFreshnessHelperSkippedWhenCacheOff verifies that a spec
// without cache or share does not receive the freshness helper.
// CLIs without a cache story should not carry dead code.
func TestGenerateFreshnessHelperSkippedWhenCacheOff(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	require.False(t, apiSpec.Cache.Enabled, "baseline stytch spec should not enable cache")
	require.False(t, apiSpec.Share.Enabled, "baseline stytch spec should not enable share")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	freshnessPath := filepath.Join(outputDir, "internal", "cliutil", "freshness.go")
	_, err = os.Stat(freshnessPath)
	assert.True(t, os.IsNotExist(err), "freshness.go must not be emitted when cache and share are both off")
}

// TestGenerateAgentContextCommand verifies that every generated CLI ships
// with the agent-context subcommand and that it emits valid JSON matching
// the documented schema. Inspired by Cloudflare's /cdn-cgi/explorer/api
// endpoint (2026-04-13 Wrangler post) — agents introspect at runtime.
func TestGenerateAgentContextCommand(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// The agent_context.go file must exist in internal/cli/.
	agentContextPath := filepath.Join(outputDir, "internal", "cli", "agent_context.go")
	data, err := os.ReadFile(agentContextPath)
	require.NoError(t, err, "agent_context.go must be generated")

	src := string(data)
	// Key symbols callers (root.go, tests, agents) rely on.
	for _, snippet := range []string{
		"func newAgentContextCmd",
		"agentContextSchemaVersion",
		`"schema_version"`,
		`Use:   "agent-context"`,
		"collectAgentCommands",
		`"pretty"`,
	} {
		assert.Contains(t, src, snippet, "agent_context.go missing %q", snippet)
	}

	// The subcommand must be registered in root.go so the CLI picks it up.
	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "newAgentContextCmd(rootCmd)",
		"agent-context command must be registered in root.go")

	// The CLI must build with the new subcommand.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	// Build the binary and run agent-context; output must be valid JSON
	// carrying the schema_version field at the top level.
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	out, err := exec.Command(binaryPath, "agent-context").Output()
	require.NoError(t, err, "running agent-context must succeed")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out, &payload), "agent-context must emit valid JSON")
	assert.Equal(t, "2", payload["schema_version"], "schema_version must be present")
	assert.Contains(t, payload, "cli")
	assert.Contains(t, payload, "auth")
	assert.Contains(t, payload, "commands")
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

func TestGeneratedOutput_READMEBearerTokenMCPSetup(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "bearer",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"BEARER_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/bearer-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)
	assert.Contains(t, content, "claude mcp add bearer bearer-pp-mcp -e BEARER_TOKEN=<your-token>")
	assert.NotContains(t, content, "bearer-pp-cli auth login\n\nclaude mcp add bearer bearer-pp-mcp")
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

func TestGenerateBrowserChromeTransport(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:       "websurface",
		Version:    "0.1.0",
		BaseURL:    "https://www.example.com",
		SpecSource: "sniffed",
		Auth:       spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/websurface-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/",
						Description: "List posts",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "websurface-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "go 1.25")
	assert.Contains(t, string(gomod), "github.com/enetx/surf")

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.Contains(t, string(clientGo), "Impersonate()")
	assert.Contains(t, string(clientGo), "Chrome()")
	assert.NotContains(t, string(clientGo), "ForceHTTP3()")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Chrome-compatible HTTP transport")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateBrowserChromeH3Transport(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:          "websurfaceh3",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChromeH3,
		Auth:          spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/websurfaceh3-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/",
						Description: "List posts",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "websurfaceh3-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.Contains(t, string(clientGo), "ForceHTTP3()")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateHTMLExtractionEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/docs/page":
			_, _ = w.Write([]byte(`<html><head><title>Docs</title></head><body><a href="child">Child page</a></body></html>`))
		case "/makers":
			_, _ = w.Write([]byte(`<html><head><title>Makers</title></head><body><a href="/@alice">Alice</a></body></html>`))
		default:
			_, _ = w.Write([]byte(`<html>
			<head><title>Product Hunt</title><meta name="description" content="New products"></head>
			<body>
				<a href="/products/speakon">1. SpeakON</a>
				<a href="/products/instant-db">2. InstantDB</a>
				<a href="/about">About</a>
			</body>
		</html>`))
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:    "webhtml",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/webhtml-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/",
						Description:    "List posts from HTML",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:         spec.HTMLExtractModeLinks,
							LinkPrefixes: []string{"/products"},
							Limit:        10,
						},
						Response: spec.ResponseDef{Type: "array", Item: "html_link"},
					},
				},
			},
			"docs": {
				Description: "Browse docs",
				Endpoints: map[string]spec.Endpoint{
					"page": {
						Method:         "GET",
						Path:           "/docs/page",
						Description:    "Fetch docs page links",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:         spec.HTMLExtractModeLinks,
							LinkPrefixes: []string{"/docs"},
							Limit:        10,
						},
						Response: spec.ResponseDef{Type: "array", Item: "html_link"},
					},
				},
			},
			"makers": {
				Description: "Browse makers",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/makers",
						Description:    "Fetch maker links",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:         spec.HTMLExtractModeLinks,
							LinkPrefixes: []string{"/@"},
							Limit:        10,
						},
						Response: spec.ResponseDef{Type: "array", Item: "html_link"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "webhtml-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "html_extract.go"))
	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "golang.org/x/net")

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "webhtml-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/webhtml-pp-cli")

	cmd := exec.Command(binaryPath, "posts", "list", "--json")
	cmd.Env = append(os.Environ(), "WEBHTML_BASE_URL="+server.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	var envelope struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	links := envelope.Results
	require.Len(t, links, 2)
	assert.Equal(t, "SpeakON", links[0]["name"])
	assert.Equal(t, "speakon", links[0]["slug"])
	assert.Equal(t, float64(1), links[0]["rank"])

	cmd = exec.Command(binaryPath, "posts", "list", "--dry-run", "--json")
	cmd.Env = append(os.Environ(), "WEBHTML_BASE_URL="+server.URL)
	out, err = cmd.Output()
	require.NoError(t, err, string(out))
	var dryRun map[string]any
	require.NoError(t, json.Unmarshal(out, &dryRun), string(out))
	if dryRun["dry_run"] != true {
		results, _ := dryRun["results"].(map[string]any)
		assert.Equal(t, true, results["dry_run"])
	}

	cmd = exec.Command(binaryPath, "docs", "page", "--json")
	cmd.Env = append(os.Environ(), "WEBHTML_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	assert.Equal(t, server.URL+"/docs/child", envelope.Results[0]["url"])

	cmd = exec.Command(binaryPath, "makers", "list", "--json")
	cmd.Env = append(os.Environ(), "WEBHTML_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	assert.Equal(t, server.URL+"/@alice", envelope.Results[0]["url"])
	assert.Equal(t, "alice", envelope.Results[0]["slug"])
}

func TestGenerateStandardTransportForOfficialAPI(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:       "officialapi",
		Version:    "0.1.0",
		BaseURL:    "https://api.example.com",
		SpecSource: "official",
		Auth:       spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/officialapi-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "officialapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "go 1.23")
	assert.NotContains(t, string(gomod), "github.com/enetx/surf")

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(clientGo), `"github.com/enetx/surf"`)
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
	assert.Contains(t, string(readme), "go install github.com/mvanhorn/printing-press-library/library/other/owned-pp-cli/cmd/owned-pp-cli@latest")
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

func TestGenerateStoreWithBatchResourceDoesNotDuplicateUpsertBatch(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "batch",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/batch-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"batch": {
				Description: "Manage batch jobs",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/batch",
						Description: "List batch jobs",
						Params: []spec.Param{
							{Name: "id", Type: "string"},
							{Name: "name", Type: "string"},
							{Name: "status", Type: "string"},
							{Name: "created_at", Type: "string", Format: "date-time"},
						},
					},
					"create": {
						Method:      "POST",
						Path:        "/batch",
						Description: "Create a batch job",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
							{Name: "description", Type: "string"},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(storeSrc), "func (s *Store) UpsertBatch("))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

// TestGenerateStoreUpsertBatchDispatchesToTypedTable is the regression test
// for issue #268. UpsertBatch was writing only to the generic resources
// table, leaving typed tables empty after every paginated sync. The fix
// added a switch dispatch inside UpsertBatch's transaction. This test
// generates a spec with a typed table, then runs the generated store
// tests — the emitted TestUpsertBatch_Populates*Table tests fail if the
// dispatch ever regresses.
func TestGenerateStoreUpsertBatchDispatchesToTypedTable(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "ads",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/ads-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"campaigns": {
				Description: "Manage campaigns",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/campaigns",
						Description: "List campaigns",
						Response:    spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "id", Type: "string"},
							{Name: "name", Type: "string"},
							{Name: "status", Type: "string"},
							{Name: "account_id", Type: "string"},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeSrc)

	// The generator must emit the typed-table helper plus a wrapping public
	// Upsert<Pascal> for the campaigns resource.
	assert.Contains(t, src, "func (s *Store) upsertCampaignsTx(", "typed Tx helper missing for campaigns")
	assert.Contains(t, src, "func (s *Store) UpsertCampaigns(", "public typed upsert missing for campaigns")

	// UpsertBatch must dispatch to the typed helper inside its switch.
	assert.Regexp(t, `(?s)func \(s \*Store\) UpsertBatch\(.*case "campaigns":\s+if err := s\.upsertCampaignsTx\(`, src,
		"UpsertBatch must dispatch to upsertCampaignsTx — without this, paginated syncs leave typed tables empty (issue #268)")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
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

func TestGeneratedOutput_READMENovelFeaturesSection(t *testing.T) {
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

	t.Run("section appears with novel features", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.NovelFeatures = []NovelFeature{
			{Name: "Health dashboard", Command: "health", Description: "See scheduling health metrics at a glance", Rationale: "Requires correlating bookings and schedules in the local store"},
			{Name: "Stale triage", Command: "triage", Description: "Find unconfirmed bookings older than N days", Rationale: "No existing tool offers batch triage"},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		assert.Contains(t, content, "## Unique Features")
		assert.Contains(t, content, "**`health`**")
		assert.Contains(t, content, "**`triage`**")
		assert.Contains(t, content, "See scheduling health metrics at a glance")
	})

	t.Run("section absent with no novel features", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		assert.NotContains(t, string(readme), "Unique Features")
	})

	t.Run("single novel feature still renders section", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.NovelFeatures = []NovelFeature{
			{Name: "Health dashboard", Command: "health", Description: "Metrics at a glance", Rationale: "Local data only"},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		assert.Contains(t, string(readme), "## Unique Features")
	})

	t.Run("novel features appear before usage", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "testapi-pp-cli")
		gen := New(minSpec, outputDir)
		gen.NovelFeatures = []NovelFeature{
			{Name: "Health dashboard", Command: "health", Description: "Metrics", Rationale: "Local data"},
		}
		require.NoError(t, gen.Generate())

		readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
		require.NoError(t, err)
		content := string(readme)
		novelIdx := strings.Index(content, "Unique Features")
		usageIdx := strings.Index(content, "## Usage")
		assert.Greater(t, usageIdx, novelIdx, "Unique Features should appear before Usage")
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

func TestGeneratedOutput_GetCommandsLackMutationEnvelope(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	// GET command should NOT have the mutation-style confirmation envelope
	// (action/resource/status/success fields). It MAY have provenance wrapping
	// via wrapWithProvenance when HasStore is true.
	getGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pet_get-by-id.go"))
	require.NoError(t, err)
	content := string(getGo)
	assert.NotContains(t, content, `"action"`)
	assert.NotContains(t, content, "statusCode")
}

// --- Unit 4: Conditional Helper Emission Tests ---

func TestComputeHelperFlags(t *testing.T) {
	t.Parallel()

	t.Run("spec with DELETE endpoints sets HasDelete", func(t *testing.T) {
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"list":   {Method: "GET", Path: "/items"},
						"delete": {Method: "DELETE", Path: "/items/{id}"},
					},
				},
			},
		}
		flags := computeHelperFlags(s)
		assert.True(t, flags.HasDelete)
	})

	t.Run("spec without DELETE endpoints clears HasDelete", func(t *testing.T) {
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"list":   {Method: "GET", Path: "/items"},
						"create": {Method: "POST", Path: "/items"},
					},
				},
			},
		}
		flags := computeHelperFlags(s)
		assert.False(t, flags.HasDelete)
	})

	t.Run("DELETE in sub-resource sets HasDelete", func(t *testing.T) {
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"projects": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/projects"},
					},
					SubResources: map[string]spec.Resource{
						"tasks": {
							Endpoints: map[string]spec.Endpoint{
								"delete": {Method: "DELETE", Path: "/projects/{id}/tasks/{task_id}"},
							},
						},
					},
				},
			},
		}
		flags := computeHelperFlags(s)
		assert.True(t, flags.HasDelete)
	})
}

func TestGeneratedHelpers_ConditionalClassifyDeleteError(t *testing.T) {
	t.Parallel()

	baseSpec := func(endpoints map[string]spec.Endpoint) *spec.APISpec {
		return &spec.APISpec{
			Name:    "testhelpers",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"TEST_API_KEY"}},
			Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/testhelpers-pp-cli/config.toml"},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints:   endpoints,
				},
			},
		}
	}

	t.Run("no DELETE endpoints omits classifyDeleteError", func(t *testing.T) {
		apiSpec := baseSpec(map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
		})

		outputDir := filepath.Join(t.TempDir(), "testhelpers-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
		require.NoError(t, err)
		content := string(helpersGo)
		assert.NotContains(t, content, "classifyDeleteError")
		// classifyAPIError should always be present
		assert.Contains(t, content, "classifyAPIError")
	})

	t.Run("with DELETE endpoints includes classifyDeleteError", func(t *testing.T) {
		apiSpec := baseSpec(map[string]spec.Endpoint{
			"list":   {Method: "GET", Path: "/items", Description: "List items"},
			"delete": {Method: "DELETE", Path: "/items/{id}", Description: "Delete item"},
		})

		outputDir := filepath.Join(t.TempDir(), "testhelpers-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
		require.NoError(t, err)
		content := string(helpersGo)
		assert.Contains(t, content, "classifyDeleteError")
		assert.Contains(t, content, "classifyAPIError")
	})
}

func TestGeneratedHelpers_ConditionalDataLayerFunctions(t *testing.T) {
	t.Parallel()

	// A simple spec with no data-layer features. The profiler will compute
	// VisionSet.Store = false, so HasDataLayer stays false and provenance
	// helpers should be omitted.
	apiSpec := &spec.APISpec{
		Name:    "testdatalayer",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"TEST_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/testdatalayer-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "testdatalayer-pp-cli")
	gen := New(apiSpec, outputDir)
	// Force VisionSet with Store=false to bypass profiler (which marks
	// read-heavy specs as offline-valuable). We're testing the template
	// conditional, not the profiler's decision.
	gen.VisionSet = VisionTemplateSet{Store: false, Export: true}
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	// Without data layer, provenance helpers should be omitted
	assert.NotContains(t, content, "DataProvenance")
	assert.NotContains(t, content, "printProvenance")
	assert.NotContains(t, content, "wrapWithProvenance")
	assert.NotContains(t, content, "defaultDBPath")

	// Core helpers should still be present
	assert.Contains(t, content, "classifyAPIError")
	assert.Contains(t, content, "printOutputWithFlags")
}

// --- Unit 3: Top-Level Command Promotion Tests ---

func TestToKebab(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"ISteamUser", "steam-user"},
		{"SteamUser", "steam-user"},
		{"users", "users"},
		{"IPlayerService", "player-service"},
		{"camelCase", "camel-case"},
		{"PascalCase", "pascal-case"},
		{"APIKey", "api-key"},
		{"simpleresource", "simpleresource"},
		{"ABC", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, toKebab(tt.input))
		})
	}
}

func TestBuildPromotedCommands(t *testing.T) {
	t.Parallel()

	t.Run("resource with list endpoint IS promoted (shortcut for resource group)", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"users": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/users", Description: "List users"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		require.Len(t, promoted, 1)
		assert.Equal(t, "users", promoted[0].PromotedName)
	})

	t.Run("ISteamUser resource IS promoted (shortcut for resource group)", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"ISteamUser": {
					Endpoints: map[string]spec.Endpoint{
						"get_player_summaries": {Method: "GET", Path: "/ISteamUser/GetPlayerSummaries/v2", Description: "Get player summaries"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		require.Len(t, promoted, 1)
		assert.Equal(t, "steam-user", promoted[0].PromotedName)
	})

	t.Run("resource named version is skipped (collides with built-in)", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"version": {
					Endpoints: map[string]spec.Endpoint{
						"get": {Method: "GET", Path: "/version", Description: "Get version"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		assert.Empty(t, promoted)
	})

	t.Run("resource with no GET endpoints is skipped", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"create": {Method: "POST", Path: "/items", Description: "Create item"},
						"delete": {Method: "DELETE", Path: "/items/{id}", Description: "Delete item"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		assert.Empty(t, promoted)
	})

	t.Run("multi-endpoint resources are not promoted even when they have a list endpoint", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"get": {Method: "GET", Path: "/items/{id}", Description: "Get item",
							Params: []spec.Param{{Name: "id", Type: "string", Positional: true}}},
						"list": {Method: "GET", Path: "/items", Description: "List items"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		assert.Empty(t, promoted, "multi-endpoint resources stay nested so unknown subcommands cannot run a promoted parent action")
	})

	t.Run("deterministically skips multi-endpoint resources", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"widgets": {
					Endpoints: map[string]spec.Endpoint{
						"search": {Method: "GET", Path: "/widgets/search", Description: "Search widgets"},
						"list":   {Method: "GET", Path: "/widgets", Description: "List widgets"},
					},
				},
			},
		}
		for i := 0; i < 20; i++ {
			promoted := buildPromotedCommands(s)
			assert.Empty(t, promoted)
		}
	})

	t.Run("deterministically orders promoted resources", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"widgets": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/widgets", Description: "List widgets"},
					},
				},
				"accounts": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/accounts", Description: "List accounts"},
					},
				},
			},
		}
		for i := 0; i < 20; i++ {
			promoted := buildPromotedCommands(s)
			require.Len(t, promoted, 2)
			assert.Equal(t, "accounts", promoted[0].ResourceName)
			assert.Equal(t, "widgets", promoted[1].ResourceName)
		}
	})

	t.Run("single-endpoint POST resource IS promoted (e.g. login, logout, register)", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"login": {
					Endpoints: map[string]spec.Endpoint{
						"login": {Method: "POST", Path: "/Login", Description: "Log in to account"},
					},
				},
				"password-forgot": {
					Endpoints: map[string]spec.Endpoint{
						"forgot-password": {Method: "POST", Path: "/PasswordForgot", Description: "Request password reset email"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		require.Len(t, promoted, 2, "both single-endpoint POST resources should promote — without this, UX becomes `<cli> login login --email …`")
		names := map[string]bool{}
		for _, p := range promoted {
			names[p.PromotedName] = true
		}
		assert.True(t, names["login"], "POST-only login resource should promote")
		assert.True(t, names["password-forgot"], "POST-only password-forgot resource should promote")
	})

	t.Run("multi-endpoint resource still requires GET for promotion (write-only resources stay nested)", func(t *testing.T) {
		t.Parallel()
		s := &spec.APISpec{
			Name:    "test",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Resources: map[string]spec.Resource{
				"mutations": {
					Endpoints: map[string]spec.Endpoint{
						"create": {Method: "POST", Path: "/m", Description: "create"},
						"delete": {Method: "DELETE", Path: "/m/{id}", Description: "delete"},
					},
				},
			},
		}
		promoted := buildPromotedCommands(s)
		assert.Empty(t, promoted, "multi-endpoint resources without a GET should not promote — picking one mutation as the 'default' is surprising")
	})

	t.Run("all built-in names are skipped", func(t *testing.T) {
		t.Parallel()
		resources := map[string]spec.Resource{}
		for name := range builtinCommands {
			resources[name] = spec.Resource{
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/" + name, Description: "List " + name},
				},
			}
		}
		s := &spec.APISpec{
			Name:      "test",
			Version:   "0.1.0",
			BaseURL:   "https://api.example.com",
			Resources: resources,
		}
		promoted := buildPromotedCommands(s)
		assert.Empty(t, promoted)
	})
}

func TestGeneratedOutput_PromotedCommandExists(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promtest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PROM_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/promtest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List all users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promtest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Promoted command file SHOULD exist — it provides a user-friendly shortcut.
	promotedFile := filepath.Join(outputDir, "internal", "cli", "promoted_users.go")
	assert.FileExists(t, promotedFile)

	// The resource parent command should NOT be generated — the promoted command replaces it.
	// Generating both would leave the parent as dead code (never wired to root).
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "users.go"))
}

func TestGeneratedOutput_PromotedCommandCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "compiletest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"CT_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/compiletest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"ISteamUser": {
				Description: "Steam user interface",
				Endpoints: map[string]spec.Endpoint{
					"get_player_summaries": {Method: "GET", Path: "/ISteamUser/GetPlayerSummaries/v2", Description: "Get player summaries",
						Params: []spec.Param{{Name: "steamids", Type: "string", Description: "Comma-separated Steam IDs"}}},
				},
			},
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/items", Description: "List items"},
					"create": {Method: "POST", Path: "/items", Description: "Create item"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "compiletest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Single-endpoint resources get promoted shortcuts; multi-endpoint resources
	// stay nested so unknown subcommands cannot run a parent action.
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_steam-user.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_items.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "items.go"))
	// API discovery command should also be generated
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "api_discovery.go"))

	// Must compile
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedOutput_PromotedCommandNotForBuiltins(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "builtintest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"BT_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/builtintest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"version": {
				Description: "Version info",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/version", Description: "Get version"},
				},
			},
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "builtintest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// "version" should NOT have a promoted command (collides with built-in)
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_version.go"))
	// "users" SHOULD have a promoted command (shortcut for the resource group)
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_users.go"))
}

// --- Unit 3: Auth Error Handling Tests ---

func TestGeneratedHelpers_AuthErrorWithEnvVarsAndKeyURL(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "steamauth",
		Version: "0.1.0",
		BaseURL: "https://api.steampowered.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "key",
			In:      "query",
			EnvVars: []string{"STEAM_API_KEY"},
			KeyURL:  "https://steamcommunity.com/dev/apikey",
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/steamauth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "steamauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	// 400 auth branch should be emitted
	assert.Contains(t, content, `HTTP 400`)
	assert.Contains(t, content, "looksLikeAuthError")
	// Env var should appear in error hints
	assert.Contains(t, content, "STEAM_API_KEY")
	// Key URL should appear in error hints
	assert.Contains(t, content, "https://steamcommunity.com/dev/apikey")
	// Doctor command hint
	assert.Contains(t, content, "steamauth-pp-cli doctor")
	// Sanitization helpers should be present
	assert.Contains(t, content, "sanitizeErrorBody")
}

func TestGeneratedHelpers_AuthErrorWithEnvVarsNoKeyURL(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "nourlauth",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"NOURL_API_KEY"},
			// KeyURL intentionally empty
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/nourlauth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "nourlauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	// Env var should appear
	assert.Contains(t, content, "NOURL_API_KEY")
	// Key URL should NOT appear
	assert.NotContains(t, content, "Get a key at:")
}

func TestGeneratedHelpers_BearerTokenAuth(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "bearerauth",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"BEARER_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/bearerauth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "bearerauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	// Bearer token hint should mention token setup
	assert.Contains(t, content, "check your token")
	assert.Contains(t, content, "BEARER_TOKEN")
	// 400 auth branch should be present (bearer_token is auth)
	assert.Contains(t, content, "looksLikeAuthError")
}

func TestGeneratedHelpers_NoAuth_No400Branch(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "noauthapi",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "",
			EnvVars: nil,
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/noauthapi-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "noauthapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	// Should NOT have 400 auth branch
	assert.NotContains(t, content, "looksLikeAuthError")
	assert.NotContains(t, content, "sanitizeErrorBody")
	// Should NOT import regexp
	assert.NotContains(t, content, `"regexp"`)
	// classifyAPIError should still exist
	assert.Contains(t, content, "classifyAPIError")
}

func TestGeneratedHelpers_AuthWithKeyURL_Compiles(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "compileauth",
		Version: "0.1.0",
		BaseURL: "https://api.steampowered.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "key",
			In:      "query",
			EnvVars: []string{"STEAM_API_KEY"},
			KeyURL:  "https://steamcommunity.com/dev/apikey",
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/compileauth-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "compileauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Must compile
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// --- Unit 4: Doctor Auth Hint Tests ---

func TestGeneratedDoctor_AuthHintsWithKeyURL(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "steamdoc",
		Version: "0.1.0",
		BaseURL: "https://api.steampowered.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "key",
			In:      "query",
			EnvVars: []string{"STEAM_API_KEY"},
			KeyURL:  "https://steamcommunity.com/dev/apikey",
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/steamdoc-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "steamdoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Should contain the env var hint
	assert.Contains(t, content, `export STEAM_API_KEY=<your-key>`)
	// Should contain the key URL
	assert.Contains(t, content, `https://steamcommunity.com/dev/apikey`)
}

func TestGeneratedDoctor_AuthHintsWithoutKeyURL(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "nourldoc",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"NOURL_API_KEY"},
			// KeyURL intentionally empty
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/nourldoc-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "nourldoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Should contain the env var hint
	assert.Contains(t, content, `export NOURL_API_KEY=<your-key>`)
	// Should NOT contain any key URL line
	assert.NotContains(t, content, "auth_key_url")
}

func TestGeneratedDoctor_AuthVerifyPathProbesEndpoint(t *testing.T) {
	t.Parallel()

	// Models the Meta Ads case from issue #267: a versioned base URL where
	// the bare root returns 401 regardless of token validity. The spec sets
	// auth.verify_path so doctor probes a known-good endpoint instead.
	apiSpec := &spec.APISpec{
		Name:    "metadoc",
		Version: "0.1.0",
		BaseURL: "https://graph.facebook.com/v23.0",
		Auth: spec.AuthConfig{
			Type:       "bearer_token",
			Header:     "Authorization",
			Format:     "Bearer {token}",
			EnvVars:    []string{"META_ADS_API_TOKEN"},
			VerifyPath: "/me?fields=id",
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/metadoc-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Manage ad accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/me/adaccounts", Description: "List accounts"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "metadoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Probe should target baseURL + verify_path, not bare baseURL
	assert.Contains(t, content, `verifyPath := "/me?fields=id"`)
	assert.Contains(t, content, `http.NewRequest("GET", baseURL+verifyPath, nil)`)
	// When verify_path is set, 401/403 keeps the strict "invalid" verdict
	assert.Contains(t, content, `"invalid (HTTP %d) — check your credentials"`)
	// And does NOT emit the inconclusive fallback wording
	assert.NotContains(t, content, "inconclusive (HTTP %d from base URL")
}

func TestGeneratedDoctor_NoVerifyPathSoftens401(t *testing.T) {
	t.Parallel()

	// Without auth.verify_path, the doctor probe falls back to the bare base
	// URL. 401/403 from the base URL must be reported as "inconclusive", not
	// "invalid", because many APIs return 401 from un-routed roots regardless
	// of token validity.
	apiSpec := &spec.APISpec{
		Name:    "softdoc",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"SOFT_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/softdoc-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "softdoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Probe should hit the bare base URL (no verify_path appended)
	assert.Contains(t, content, `http.NewRequest("GET", baseURL, nil)`)
	assert.NotContains(t, content, "verifyPath := ")
	// 401/403 fallback must be the soft "inconclusive" verdict
	assert.Contains(t, content, `"inconclusive (HTTP %d from base URL — set auth.verify_path in spec for a definitive probe)"`)
	// And must NOT use the strict "invalid" wording
	assert.NotContains(t, content, `"invalid (HTTP %d) — check your credentials"`)
	// Renderer must classify "inconclusive" as WARN before the FAIL clause
	assert.Contains(t, content, `case strings.HasPrefix(s, "inconclusive"):`)
	assert.Contains(t, content, `indicator = yellow("WARN")`)
}

func TestGeneratedDoctor_NoAuthShowsNotRequired(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "noauthdoc",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "",
			EnvVars: nil,
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/noauthdoc-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "noauthdoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Auth report should show "not required" — not "not configured"
	assert.Contains(t, content, `report["auth"] = "not required"`)
	// The auth section should NOT set report["auth"] to "not configured"
	assert.NotContains(t, content, `report["auth"] = "not configured"`)
}

func TestGeneratedHelpers_DeadCodeRemoved(t *testing.T) {
	t.Parallel()

	// Dead code should never appear regardless of spec contents
	apiSpec := &spec.APISpec{
		Name:    "deadcode",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"DEAD_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/deadcode-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/items", Description: "List items"},
					"delete": {Method: "DELETE", Path: "/items/{id}", Description: "Delete item"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "deadcode-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	content := string(helpersGo)

	assert.NotContains(t, content, "firstNonEmpty", "firstNonEmpty is dead code and should not be emitted")
	assert.NotContains(t, content, "printOutputFiltered", "printOutputFiltered is dead code and should not be emitted")
	assert.NotContains(t, content, "selectFieldsGlobal", "selectFieldsGlobal is dead code and should not be emitted")

	// Verify useful functions are still present
	assert.Contains(t, content, "printOutputWithFlags")
	assert.Contains(t, content, "filterFields")
	assert.Contains(t, content, "classifyAPIError")
}

func TestGenerate_CookieAuthUsesBrowserTemplate(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "cookieapp",
		Version: "0.1.0",
		BaseURL: "https://app.example.com",
		Auth: spec.AuthConfig{
			Type:                           "cookie",
			Header:                         "Cookie",
			In:                             "cookie",
			CookieDomain:                   ".example.com",
			EnvVars:                        []string{"COOKIEAPP_COOKIES"},
			RequiresBrowserSession:         true,
			BrowserSessionValidationPath:   "/api/items",
			BrowserSessionValidationMethod: "GET",
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/cookieapp-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/api/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cookieapp-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authGo)

	// Browser auth template indicators
	assert.Contains(t, content, "--chrome")
	assert.Contains(t, content, "detectCookieTool")
	assert.Contains(t, content, "extractCookies")
	assert.Contains(t, content, "cookieToolSupportsProfiles")
	assert.Contains(t, content, "--url")
	assert.Contains(t, content, "does not support --profile")
	assert.Contains(t, content, ".example.com")
	assert.Contains(t, content, "continuing without auto-detection")
	assert.Contains(t, content, "validateAndWriteBrowserSessionProof")
	assert.Contains(t, content, "validateAndWriteBrowserSessionProofWithRetry")
	assert.Contains(t, content, "browser-session-proof.json")
	assert.Contains(t, content, "newAuthRefreshCmd")
	assert.Contains(t, content, "auth refresh")
	assert.Contains(t, content, "openBrowserForCookieRefresh")
	assert.Contains(t, content, "waitForCookieRefreshBrowser")
	assert.Contains(t, content, "Complete any login or browser challenge in Chrome")
	assert.NotContains(t, content, "No browser runtime found.")
	assert.NotContains(t, content, "newAuthRefreshQueriesCmd")
	// Should NOT contain simple token template indicators
	assert.NotContains(t, content, "set-token")

	// Config should have cookie branch in AuthHeader
	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configContent := string(configGo)
	assert.Contains(t, configContent, `"browser"`)

	// Doctor should reference browser auth
	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctorContent := string(doctorGo)
	assert.Contains(t, doctorContent, "auth login --chrome")
	assert.Contains(t, doctorContent, "browser_session_proof")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerate_UserAgentOverrideGatedByBrowserTransport(t *testing.T) {
	t.Parallel()

	baseSpec := func(name string) *spec.APISpec {
		return &spec.APISpec{
			Name:    name,
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Auth:    spec.AuthConfig{Type: "none"},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/" + name + "-pp-cli/config.toml",
			},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/items", Description: "List items"},
					},
				},
			},
		}
	}

	standardDir := filepath.Join(t.TempDir(), "standard-pp-cli")
	standardSpec := baseSpec("standard")
	require.NoError(t, New(standardSpec, standardDir).Generate())
	standardClient, err := os.ReadFile(filepath.Join(standardDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(standardClient), `req.Header.Set("User-Agent", "standard-pp-cli/0.1.0")`)

	browserDir := filepath.Join(t.TempDir(), "browser-pp-cli")
	browserSpec := baseSpec("browser")
	browserSpec.HTTPTransport = spec.HTTPTransportBrowserChrome
	require.NoError(t, New(browserSpec, browserDir).Generate())
	browserClient, err := os.ReadFile(filepath.Join(browserDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(browserClient), `req.Header.Set("User-Agent"`)
	browserAuth, err := os.ReadFile(filepath.Join(browserDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(browserAuth), "newAuthRefreshCmd")
}

func TestGenerateObjectBodyDefaultsAreParsedAsJSON(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "graphqlbody-pp-cli")
	apiSpec := &spec.APISpec{
		Name:          "graphqlbody",
		Description:   "GraphQL body API",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChromeH3,
		Auth:          spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/graphqlbody-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"graphql": {
				Description: "GraphQL BFF operations",
				Endpoints: map[string]spec.Endpoint{
					"posts_today": {
						Method:      "POST",
						Path:        "/frontend/graphql",
						Description: "Run GraphQL operation PostsToday",
						Body: []spec.Param{
							{Name: "operationName", Type: "string", Required: true, Default: "PostsToday"},
							{Name: "variables", Type: "object", Default: map[string]any{"date": "2026-04-22"}},
							{Name: "extensions", Type: "object", Default: map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": "oldhash"}}},
						},
					},
					"product_page_launches": {
						Method:      "POST",
						Path:        "/frontend/graphql",
						Description: "Run GraphQL operation ProductPageLaunches",
						Body: []spec.Param{
							{Name: "operationName", Type: "string", Required: true, Default: "ProductPageLaunches"},
							{Name: "variables", Type: "object", Default: map[string]any{"slug": "sample"}},
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	var content string
	err := filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "bodyExtensions") {
			content = string(data)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, content)
	assert.Contains(t, content, `StringVar(&bodyVariables, "variables", "{\"date\":\"2026-04-22\"}"`)
	assert.Contains(t, content, `json.Unmarshal([]byte(bodyVariables), &parsedVariables)`)
	assert.Contains(t, content, `body["variables"] = parsedVariables`)
	assert.Contains(t, content, `json.Unmarshal([]byte(bodyExtensions), &parsedExtensions)`)
	assert.Contains(t, content, `body["extensions"] = parsedExtensions`)
	_, err = parser.ParseFile(token.NewFileSet(), "graphql_posts_today.go", content, parser.ParseComments)
	require.NoError(t, err)

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authGo)
	assert.NotContains(t, authContent, "newAuthRefreshCmd")
	assert.Contains(t, authContent, "newAuthRefreshQueriesCmd")
	assert.NotContains(t, authContent, "waitForBrowserRuntimeClearance")
	assert.NotContains(t, authContent, "Use --chrome to read cookies")
	assert.NotContains(t, authContent, "wait-timeout")
}

func TestGenerateGraphQLBFFUsesSemanticCommandSurface(t *testing.T) {
	t.Parallel()

	capture := &browsersniff.EnrichedCapture{
		TargetURL: "https://www.example.com",
		Entries: []browsersniff.EnrichedEntry{
			graphQLBFFCaptureEntry("ProductPageLaunches", `{"slug":"sample-product"}`, "aaa111"),
			graphQLBFFCaptureEntry("ProductPageMakers", `{"slug":"sample-product"}`, "bbb222"),
			graphQLBFFCaptureEntry("CategoryPageQuery", `{"slug":"productivity"}`, "ccc333"),
		},
	}
	apiSpec, err := browsersniff.AnalyzeCapture(capture)
	require.NoError(t, err)
	apiSpec.HTTPTransport = spec.HTTPTransportBrowserChromeH3
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/example-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "example-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootSrc := string(rootGo)
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newProductsCmd(&flags))")
	assert.NotContains(t, rootSrc, "rootCmd.AddCommand(newGraphqlCmd(&flags))")
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "products.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "products_launches.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "products_makers.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "graphql.go"))

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "example-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/example-pp-cli")
	helpOut, err := exec.Command(binaryPath, "--help").CombinedOutput()
	require.NoError(t, err, string(helpOut))
	assert.Contains(t, string(helpOut), "products")
	assert.NotContains(t, string(helpOut), "graphql")
	productsHelp, err := exec.Command(binaryPath, "products", "--help").CombinedOutput()
	require.NoError(t, err, string(productsHelp))
	assert.Contains(t, string(productsHelp), "launches")
	assert.Contains(t, string(productsHelp), "makers")
}

func TestGenerateWhichFallsBackToCommandTree(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "whichfallback-pp-cli")
	apiSpec := &spec.APISpec{
		Name:        "whichfallback",
		Description: "Which fallback API",
		Version:     "1.0.0",
		BaseURL:     "https://api.example.com",
		Auth:        spec.AuthConfig{Type: "none"},
		Config:      spec.ConfigSpec{Format: "toml", Path: "~/.config/whichfallback-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"products": {
				Description: "Product operations",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/products",
						Description: "List products",
					},
				},
				SubResources: map[string]spec.Resource{
					"reviews": {
						Description: "Review operations",
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:      "GET",
								Path:        "/products/{id}/reviews",
								Description: "List product reviews",
								Params: []spec.Param{
									{Name: "id", Type: "string", Required: true, Positional: true},
								},
							},
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}
	require.NoError(t, New(apiSpec, outputDir).Generate())

	whichGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "which.go"))
	require.NoError(t, err)
	whichSrc := string(whichGo)
	assert.Contains(t, whichSrc, `Command: "products list"`)
	assert.Contains(t, whichSrc, `Description: "List products"`)
	assert.Contains(t, whichSrc, `Command: "products reviews list"`)

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "whichfallback-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/whichfallback-pp-cli")
	whichOut, err := exec.Command(binaryPath, "which", "reviews", "--json").CombinedOutput()
	require.NoError(t, err, string(whichOut))
	assert.Contains(t, string(whichOut), "products reviews list")
}

func graphQLBFFCaptureEntry(operationName, variablesJSON, hash string) browsersniff.EnrichedEntry {
	return browsersniff.EnrichedEntry{
		Method:              "POST",
		URL:                 "https://www.example.com/frontend/graphql",
		RequestHeaders:      map[string]string{"Content-Type": "application/json"},
		RequestBody:         `{"operationName":"` + operationName + `","variables":` + variablesJSON + `,"extensions":{"persistedQuery":{"version":1,"sha256Hash":"` + hash + `"}}}`,
		ResponseStatus:      200,
		ResponseContentType: "application/json",
		ResponseBody:        `{"data":{"node":{"id":"1"}}}`,
	}
}

func TestGenerate_ComposedAuthUsesBrowserTemplate(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "pagliacci",
		Version: "0.1.0",
		BaseURL: "https://pag-api.azurewebsites.net/api",
		Auth: spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "PagliacciAuth {customerId}|{authToken}",
			CookieDomain: "pagliacci.com",
			Cookies:      []string{"customerId", "authToken"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/pagliacci-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"store": {
				Description: "Manage stores",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/Store", Description: "List stores"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "pagliacci-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authGo)

	// Should use browser auth template (shared with cookie type)
	assert.Contains(t, content, "--chrome")
	assert.Contains(t, content, "detectCookieTool")
	assert.Contains(t, content, "extractCookies")
	assert.Contains(t, content, "pagliacci.com")
	// Should NOT contain simple token template
	assert.NotContains(t, content, "set-token")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// --- Regression tests for machinery fixes (cal.com retro 2026-04-04) ---

func TestGeneratedOutput_NoMarkFlagRequired(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	// Read all generated endpoint command files
	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)

	foundRunEValidation := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cliDir, e.Name()))
		require.NoError(t, err)
		content := string(data)

		// No command should use MarkFlagRequired (except import.go which is not verify-tested)
		if e.Name() != "import.go" && strings.Contains(content, "MarkFlagRequired") {
			t.Errorf("%s still contains MarkFlagRequired", e.Name())
		}

		// Track whether we find the RunE-based validation
		if strings.Contains(content, `!flags.dryRun`) && strings.Contains(content, `required flag`) {
			foundRunEValidation = true
		}
	}

	// The petstore spec has required params, so we should find RunE validation
	assert.True(t, foundRunEValidation, "required params should use RunE validation with dryRun guard")
}

func TestGeneratedOutput_PromotedNoImportGuards(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "promoted_") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cliDir, e.Name()))
		require.NoError(t, err)
		content := string(data)

		assert.NotContains(t, content, "var _ =", "promoted command %s should not contain import guards", e.Name())
		assert.NotContains(t, content, "var _ json", "promoted command %s should not contain import guards", e.Name())
		assert.NotContains(t, content, `"io"`, "promoted command %s should not import io", e.Name())
		assert.NotContains(t, content, `"strings"`, "promoted command %s should not import strings", e.Name())
	}
}

func TestGeneratedOutput_ObjectFieldsUseRawMessage(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	typesGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "types", "types.go"))
	require.NoError(t, err)
	content := string(typesGo)

	// The petstore spec has object-typed schema fields; they should be json.RawMessage
	assert.Contains(t, content, "json.RawMessage", "types.go should use json.RawMessage for object/array fields")
	assert.Contains(t, content, `import "encoding/json"`, "types.go should import encoding/json when RawMessage is used")
}

func TestMCPDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		desc        string
		noAuth      bool
		authType    string
		publicCount int
		totalCount  int
		want        string
	}{
		{
			name: "all auth — no annotation",
			desc: "List orders", noAuth: false, authType: "api_key",
			publicCount: 0, totalCount: 10,
			want: "List orders",
		},
		{
			name: "all public — no annotation",
			desc: "List items", noAuth: true, authType: "none",
			publicCount: 10, totalCount: 10,
			want: "List items",
		},
		{
			name: "public minority — append (public)",
			desc: "Find stores", noAuth: true, authType: "api_key",
			publicCount: 3, totalCount: 10,
			want: "Find stores (public)",
		},
		{
			name: "public minority — auth endpoint not annotated",
			desc: "Create order", noAuth: false, authType: "api_key",
			publicCount: 3, totalCount: 10,
			want: "Create order",
		},
		{
			name: "auth minority api_key — append suffix",
			desc: "Create order", noAuth: false, authType: "api_key",
			publicCount: 8, totalCount: 10,
			want: "Create order (requires API key)",
		},
		{
			name: "auth minority cookie — append browser login",
			desc: "View account", noAuth: false, authType: "cookie",
			publicCount: 8, totalCount: 10,
			want: "View account (requires browser login)",
		},
		{
			name: "auth minority oauth2 — append requires auth",
			desc: "Update profile", noAuth: false, authType: "oauth2",
			publicCount: 8, totalCount: 10,
			want: "Update profile (requires auth)",
		},
		{
			name: "exact tie — no annotation on either side",
			desc: "Get item", noAuth: true, authType: "api_key",
			publicCount: 5, totalCount: 10,
			want: "Get item",
		},
		{
			name: "exact tie — auth side also not annotated",
			desc: "Delete item", noAuth: false, authType: "api_key",
			publicCount: 5, totalCount: 10,
			want: "Delete item",
		},
		{
			name: "oneline cleanup applied",
			desc: "First line\nSecond line", noAuth: false, authType: "none",
			publicCount: 0, totalCount: 5,
			want: "First line Second line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mcpDescription(tt.desc, tt.noAuth, tt.authType, tt.publicCount, tt.totalCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateMCPContextEscapesDomainStrings(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Description = `Stytch "quoted" API \ context`
	apiSpec.Auth.KeyURL = `https://example.test/keys?label="quoted"&path=\demo`

	users := apiSpec.Resources["users"]
	users.Description = `Manage "users" with \ backslashes`
	apiSpec.Resources["users"] = users

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet.MCP = true
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        `Quote "dashboard"`,
			Command:     `quote run --filter="active"`,
			Description: `Shows "quoted" data from C:\tmp.`,
			Rationale:   `Agents need "literal" strings without breaking generated Go.`,
		},
	}
	require.NoError(t, gen.Generate())

	mcpToolsPath := filepath.Join(outputDir, "internal", "mcp", "tools.go")
	data, err := os.ReadFile(mcpToolsPath)
	require.NoError(t, err)

	_, err = parser.ParseFile(token.NewFileSet(), mcpToolsPath, data, parser.AllErrors)
	require.NoError(t, err, "MCP tools source must remain valid Go when context strings contain quotes and backslashes")

	src := string(data)
	assert.Contains(t, src, `label=\"quoted\"&path=\\demo`)
	assert.Contains(t, src, `Quote \"dashboard\"`)
	assert.Contains(t, src, `filter=\"active\"`)
}

func TestEnvVarBuiltinFieldDedup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		envVar    string
		isBuiltin bool
		resolved  string
	}{
		{"HUBSPOT_ACCESS_TOKEN", true, "AccessToken"},
		{"DISCORD_ACCESS_TOKEN", true, "AccessToken"},
		{"MY_CLIENT_ID", true, "ClientID"},
		{"STRIPE_SECRET_KEY", false, "StripeSecretKey"},
		{"LINEAR_API_KEY", false, "LinearApiKey"},
		{"MY_REFRESH_TOKEN", true, "RefreshToken"},
		{"NOTION_TOKEN", false, "NotionToken"},
	}
	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.isBuiltin, envVarIsBuiltinField(tt.envVar))
			assert.Equal(t, tt.resolved, resolveEnvVarField(tt.envVar))
		})
	}
}

func TestGenerateDependentSyncCompiles(t *testing.T) {
	t.Parallel()

	// A spec with parent-child paths should generate compilable sync code
	// that includes the dependent sync functions.
	apiSpec := &spec.APISpec{
		Name:    "messaging",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MESSAGING_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/messaging-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"channels": {
				Description: "Manage channels",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/channels",
						Description: "List channels",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
					"create": {
						Method:      "POST",
						Path:        "/channels",
						Description: "Create a channel",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
							{Name: "description", Type: "string"},
						},
					},
				},
			},
			"messages": {
				Description: "Manage messages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/channels/{channelId}/messages",
						Description: "List messages in a channel",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
					"create": {
						Method:      "POST",
						Path:        "/channels/{channelId}/messages",
						Description: "Send a message",
						Body: []spec.Param{
							{Name: "content", Type: "string"},
							{Name: "title", Type: "string"},
						},
					},
				},
			},
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/users",
						Description: "List users",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Verify sync.go was generated with dependent sync content
	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent, "syncDependentResource", "sync.go should contain dependent sync function")
	assert.Contains(t, syncContent, "dependentResourceDefs", "sync.go should contain dependent resource definitions")
	assert.Contains(t, syncContent, `"messages"`, "sync.go should reference messages as a dependent resource")
	assert.Contains(t, syncContent, `"channels"`, "sync.go should reference channels as the parent")

	// The generated project should compile and the generated store tests
	// should pass — including TestUpsertBatch_SetsMessagesParentID, which
	// verifies dependent-resource sync fills the typed parent_id column
	// (issue #268).
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestGenerateGraphQLCompiles(t *testing.T) {
	t.Parallel()

	// Parse a GraphQL SDL fixture and verify the generated CLI compiles
	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	gen := New(gqlSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Verify GraphQL-specific files were generated
	_, err = os.Stat(filepath.Join(outputDir, "internal", "client", "graphql.go"))
	require.NoError(t, err, "graphql.go should be generated")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "client", "queries.go"))
	require.NoError(t, err, "queries.go should be generated")

	// Verify sync.go uses GraphQL patterns (POST /graphql, not GET-based REST)
	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent, "Query", "sync.go should use GraphQL Query method")
	assert.Contains(t, syncContent, "graphql", "sync.go should reference graphql")
	assert.NotContains(t, syncContent, "c.Get(path", "sync.go should not use REST GET pattern")

	// Verify queries.go has query constants
	queriesGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "queries.go"))
	require.NoError(t, err)
	queriesContent := string(queriesGo)
	assert.Contains(t, queriesContent, "ListQuery", "queries.go should contain list query constants")
	assert.Contains(t, queriesContent, "pageInfo", "queries.go should include pageInfo in queries")
	assert.Contains(t, queriesContent, "hasNextPage", "queries.go should include hasNextPage")
	assert.Contains(t, queriesContent, "endCursor", "queries.go should include endCursor")

	// The generated project should compile
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMCPMainStdioDefault confirms that a spec with no mcp: block
// produces the same stdio-only MCP entry point we've always emitted. Remote
// transport is opt-in; the default stays on the current behavior so existing
// published CLIs regenerate byte-compatibly. Guards against the template
// accidentally pulling in flag / StreamableHTTP imports for stdio-only specs.
func TestGenerateMCPMainStdioDefault(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	require.Empty(t, apiSpec.MCP.Transport, "baseline loops spec should not declare MCP transports")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	mainPath := filepath.Join(outputDir, "cmd", naming.MCP(apiSpec.Name), "main.go")
	data, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, "server.ServeStdio(s)", "stdio-only spec must still call ServeStdio")
	assert.NotContains(t, body, "flag.String", "stdio-only spec must not pull in the flag package")
	assert.NotContains(t, body, "NewStreamableHTTPServer", "stdio-only spec must not reference the HTTP transport")
	assert.NotContains(t, body, "PP_MCP_TRANSPORT", "stdio-only spec must not reference the transport env override")
}

// TestGenerateMCPMainRemoteOptIn confirms that declaring mcp.transport: [stdio, http]
// emits a flag-aware main with both transport branches, including the env-based
// default and the custom --addr. Uses a byte-level check on the template
// output rather than parsing the generated AST to match the Share test style.
func TestGenerateMCPMainRemoteOptIn(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{
		Transport: []string{"stdio", "http"},
		Addr:      ":8123",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	mainPath := filepath.Join(outputDir, "cmd", naming.MCP(apiSpec.Name), "main.go")
	data, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	body := string(data)

	for _, want := range []string{
		`"flag"`,
		`"strings"`,
		`defaultHTTPAddr = ":8123"`,
		`flag.String("transport"`,
		`flag.String("addr"`,
		`server.ServeStdio(s)`,
		`server.NewStreamableHTTPServer(s)`,
		`httpSrv.Start(*addr)`,
		`PP_MCP_TRANSPORT`,
	} {
		assert.Contains(t, body, want, "remote-opt-in main should contain %q", want)
	}
}

// TestGenerateMCPCodeOrchestrationEmitsSearchExecute proves that when the
// spec opts into code-orchestration, the generator emits only
// <api>_search and <api>_execute as MCP tools, covering every endpoint via
// a single registry. This is the thin surface pattern referenced by
// Anthropic's 2026-04-22 post (Cloudflare's ~2,500-endpoint server in ~1K
// tokens).
func TestGenerateMCPCodeOrchestrationEmitsSearchExecute(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	codeOrchPath := filepath.Join(outputDir, "internal", "mcp", "code_orch.go")
	data, err := os.ReadFile(codeOrchPath)
	require.NoError(t, err, "code_orch.go must be emitted when orchestration is code")
	body := string(data)

	for _, want := range []string{
		`func RegisterCodeOrchestrationTools(`,
		`mcplib.NewTool("loops_search"`,
		`mcplib.NewTool("loops_execute"`,
		`codeOrchEndpoints = []codeOrchEndpoint`,
		`func handleCodeOrchSearch(`,
		`func handleCodeOrchExecute(`,
	} {
		assert.Contains(t, body, want, "code_orch.go missing expected snippet %q", want)
	}

	toolsPath := filepath.Join(outputDir, "internal", "mcp", "tools.go")
	toolsData, err := os.ReadFile(toolsPath)
	require.NoError(t, err)
	toolsBody := string(toolsData)
	assert.Contains(t, toolsBody, "RegisterCodeOrchestrationTools(s)",
		"code-orchestration RegisterTools must call RegisterCodeOrchestrationTools")
	assert.NotContains(t, toolsBody, `mcplib.NewTool("contacts_list"`,
		"endpoint-mirror tools must be fully suppressed in code-orch mode")

	// End-to-end: the generated project must compile.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMCPCodeOrchestrationSkippedByDefault guards against the
// template accidentally emitting code_orch.go for specs that didn't opt in.
// Small APIs should keep today's endpoint-mirror shape; the thin surface
// costs a discovery round-trip the agent doesn't need when there are only
// a handful of tools.
func TestGenerateMCPCodeOrchestrationSkippedByDefault(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	require.False(t, apiSpec.MCP.IsCodeOrchestration())

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	_, err = os.Stat(filepath.Join(outputDir, "internal", "mcp", "code_orch.go"))
	assert.True(t, os.IsNotExist(err), "code_orch.go must not be emitted without orchestration: code")
}

// TestGenerateMCPIntentsEmittedWhenDeclared proves that a spec with mcp.intents
// emits internal/mcp/intents.go, wires the intent handler into RegisterTools
// via RegisterIntents, and keeps endpoint-mirror tools by default.
func TestGenerateMCPIntentsEmittedWhenDeclared(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{
		Intents: []spec.Intent{
			{
				Name:        "fetch_contact_then_noop",
				Description: "Fetch a contact, then do nothing (integration fixture)",
				Params: []spec.IntentParam{
					{Name: "contact_id", Type: "string", Required: true, Description: "contact id"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "contacts.list",
						Bind:     map[string]string{"limit": "${input.contact_id}"},
						Capture:  "contacts",
					},
				},
				Returns: "contacts",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	intentsPath := filepath.Join(outputDir, "internal", "mcp", "intents.go")
	data, err := os.ReadFile(intentsPath)
	require.NoError(t, err, "intents.go must be emitted when intents are declared")
	body := string(data)

	for _, want := range []string{
		`func RegisterIntents(`,
		`handleFetchContactThenNoop`,
		`mcplib.NewTool("fetch_contact_then_noop"`,
		`intentEndpoints = map[string]intentEndpointMeta{`,
		`"contacts.list"`,
		`func resolveIntentBinding(`,
		`func callIntentEndpoint(`,
	} {
		assert.Contains(t, body, want, "intents.go missing expected snippet %q", want)
	}

	toolsPath := filepath.Join(outputDir, "internal", "mcp", "tools.go")
	toolsData, err := os.ReadFile(toolsPath)
	require.NoError(t, err)
	assert.Contains(t, string(toolsData), "RegisterIntents(s)",
		"RegisterTools must wire in RegisterIntents when intents are declared")
	assert.Contains(t, string(toolsData), `mcplib.NewTool("contacts_list"`,
		"raw endpoint-mirror tools remain visible by default")

	// End-to-end signal — the whole generated project must compile.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMCPEndpointToolsHiddenSuppressesEndpointTools proves that
// endpoint_tools: hidden removes the raw per-endpoint MCP tools but keeps
// the intent registration wired in. This is the surface agents see when the
// intent declarations fully cover the useful operations.
func TestGenerateMCPEndpointToolsHiddenSuppressesEndpointTools(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{
		EndpointTools: "hidden",
		Intents: []spec.Intent{
			{
				Name:        "noop_intent",
				Description: "Fixture intent",
				Steps: []spec.IntentStep{
					{Endpoint: "contacts.list", Capture: "contacts"},
				},
				Returns: "contacts",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	toolsPath := filepath.Join(outputDir, "internal", "mcp", "tools.go")
	data, err := os.ReadFile(toolsPath)
	require.NoError(t, err)
	body := string(data)

	assert.NotContains(t, body, `mcplib.NewTool("contacts_list"`,
		"raw endpoint tools must be hidden when endpoint_tools: hidden")
	assert.Contains(t, body, "RegisterIntents(s)",
		"intent registration must still be called when endpoint tools are hidden")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMCPMainRemoteRuntime is the runtime signal for U1. Building the
// binary is necessary but not sufficient — we also want to catch the shape of
// failures the build cannot see (e.g., a panic in defaultTransport or an
// unreachable switch arm). This test spawns the generated binary with the
// default --help and with an unknown --transport value, then asserts on the
// exit codes + stderr. Full JSON-RPC handshake over stdio or HTTP is out of
// scope here — U4's scorecard integration test will cover that.
func TestGenerateMCPMainRemoteRuntime(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio", "http"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "mod", "tidy")

	mcpBinary := filepath.Join(outputDir, naming.MCP(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", mcpBinary, "./cmd/"+naming.MCP(apiSpec.Name))

	// --help should print both flags so an agent can discover transport + addr.
	helpOut, err := exec.Command(mcpBinary, "--help").CombinedOutput()
	// cobra-style --help exits 0 or 2 depending on the library; we only care
	// that the usage string mentions both flags.
	_ = err
	helpStr := string(helpOut)
	assert.Contains(t, helpStr, "-transport", "--help must mention the transport flag")
	assert.Contains(t, helpStr, "-addr", "--help must mention the addr flag")

	// An unknown transport should fail fast with exit code 2 and a stderr
	// message naming the valid set. This is the primary agent-facing error.
	cmd := exec.Command(mcpBinary, "--transport", "grpc")
	errOut, runErr := cmd.CombinedOutput()
	require.Error(t, runErr, "unknown transport must return a non-zero exit")
	assert.Contains(t, string(errOut), "unknown --transport",
		"stderr should name the unknown-transport failure mode")
	assert.Contains(t, string(errOut), "stdio, http",
		"stderr should enumerate the supported transports")
}

// TestGenerateMCPMainRemoteCompiles is the integration signal for U1: when a
// spec opts into the http transport, the generated project must still compile
// end to end. This is where a missing import or symbol mismatch in the
// template would blow up, so it catches what the string-based test cannot.
func TestGenerateMCPMainRemoteCompiles(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio", "http"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	mcpBinary := filepath.Join(outputDir, naming.MCP(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", mcpBinary, "./cmd/"+naming.MCP(apiSpec.Name))

	info, err := os.Stat(mcpBinary)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	require.NotZero(t, info.Size())
}
