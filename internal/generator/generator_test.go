package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateProjectsCompile(t *testing.T) {
	t.Parallel()

	// expectedFiles is the total file count per fixture; mustInclude is
	// the set of paths that every generated CLI must ship regardless of
	// spec shape. Two assertions instead of one: count guards against
	// templates silently disappearing, mustInclude guards against
	// renames/moves that preserve the count but break consumers. When a
	// new always-emitted template is added, bump expectedFiles for each
	// fixture and add the path to mustInclude. When a template is
	// renamed, only mustInclude needs updating.
	// mustInclude lists files emitted by gen.Generate() directly. Files
	// produced by downstream steps (tools-manifest.json from
	// manifest-gen, workflow_verify.yaml from the publish pipeline,
	// dogfood-results.json from dogfood) aren't in scope for this test
	// — it only exercises the generator.
	mustInclude := []string{
		"go.mod",
		"Makefile",
		"AGENTS.md",
		"README.md",
		"SKILL.md",
		"internal/cli/root.go",
		"internal/cli/which.go",
		"internal/cli/profile.go",
		"internal/cli/feedback.go",
		"internal/cli/agent_context.go",
		"internal/cliutil/fanout.go",
		"internal/cliutil/text.go",
		"internal/cliutil/probe.go",
		"internal/cliutil/ratelimit.go",
		"internal/cliutil/verifyenv.go",
		"internal/cliutil/cliutil_test.go",
		"internal/client/client.go",
		"internal/config/config.go",
		"internal/mcp/cobratree/walker.go",
		"internal/mcp/cobratree/classify.go",
		"internal/mcp/cobratree/typemap.go",
		"internal/mcp/cobratree/shellout.go",
		"internal/mcp/cobratree/cli_path.go",
		"internal/mcp/cobratree/names.go",
	}

	tests := []struct {
		name          string
		specPath      string
		expectedFiles int
	}{
		// expectedFiles is total file count under the generated tree.
		// Bump it AND add to mustInclude above when adding always-emitted
		// templates. Per-spec dynamic files (per-resource command files,
		// generated tests) account for the difference between fixtures.
		{name: "stytch", specPath: filepath.Join("..", "..", "testdata", "stytch.yaml"), expectedFiles: 55},
		{name: "clerk", specPath: filepath.Join("..", "..", "testdata", "clerk.yaml"), expectedFiles: 60},
		{name: "loops", specPath: filepath.Join("..", "..", "testdata", "loops.yaml"), expectedFiles: 57},
	}

	for _, tt := range tests {
		tt := tt //nolint:modernize // keep the parallel subtest capture explicit
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec, err := spec.Parse(tt.specPath)
			require.NoError(t, err)

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			require.Equal(t, tt.expectedFiles, countFiles(t, outputDir))

			// Beyond the count, every fixture must contain these
			// always-emitted files. Catches the rename / move case
			// that preserves the count but breaks consumers.
			for _, rel := range mustInclude {
				path := filepath.Join(outputDir, rel)
				_, err := os.Stat(path)
				require.NoError(t, err, "must-include path missing: %s", rel)
			}

			runGoCommand(t, outputDir, "mod", "tidy")
			runGoCommand(t, outputDir, "build", "./...")
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

	apiSpec := minimalSpec("cliutil")

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
		{"text.go", "func LooksLikeAuthError("},
		{"text.go", "func SanitizeErrorBody("},
	} {
		data, err := os.ReadFile(filepath.Join(cliutilDir, probe.file))
		require.NoError(t, err)
		assert.Contains(t, string(data), probe.snippet, "%s missing %q", probe.file, probe.snippet)
	}

	// The generated cliutil package must compile and its tests must pass.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

// TestGenerateNoUnscopedStoreOpen guards every non-test code path in
// generated CLIs against silently re-introducing context-less store
// opens. After PR #441 (store.OpenWithContext) and PR #445 (wrapper-
// function ctx threading), every emitted call to the store should
// route through OpenWithContext so caller cancellation propagates
// through the migration retry loop. A future template addition that
// regresses to store.Open(...) would defeat the entire ctx story.
//
// Excludes test files (cliutil tests, share_test.go) since those
// don't have a Cobra context and Open(dbPath) → OpenWithContext(
// context.Background(), dbPath) is the correct fallback there.
func TestGenerateNoUnscopedStoreOpen(t *testing.T) {
	t.Parallel()

	// Use the share fixture: it exercises the broadest set of templates
	// (store, sync, cache, share, doctor, MCP, auto-refresh, workflows)
	// so the walk covers every emission point that historically called
	// store.Open.
	apiSpec := minimalSpec("storeopenguard")
	apiSpec.Cache.Enabled = true
	apiSpec.Share.Enabled = true
	apiSpec.Share.SnapshotTables = []string{"items"}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	// Walk every .go file under internal/, skipping test files.
	// strings.Contains("store.Open(") matches the no-ctx form but not
	// "store.OpenWithContext(" because the literal "Open(" never appears
	// in the longer name.
	internalDir := filepath.Join(outputDir, "internal")
	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "store.Open(") {
			rel := filepath.ToSlash(strings.TrimPrefix(path, outputDir+"/"))
			t.Errorf("%s contains store.Open( without ctx — use store.OpenWithContext(ctx, ...) instead", rel)
		}
		return nil
	})
	require.NoError(t, err, "walking generated CLI tree")
}

func TestGenerateDedupesResourceRegistryMapEntries(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("resource-registry-dedupe")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"locations": {
			Description: "Manage locations",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/locations",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
		},
		"contacts": {
			Description: "Manage contacts",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/contacts",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
					Critical:   true,
				},
				"list_by_location": {
					Method:     "GET",
					Path:       "/locations/{locationId}/contacts",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
					Critical:   true,
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(storeSrc), `"contacts": "id",`), "store.go should emit one ID override per resource")
	assert.Equal(t, 1, strings.Count(string(syncSrc), `"contacts": "id",`), "sync.go should emit one ID override per resource")
	assert.Equal(t, 1, strings.Count(string(syncSrc), `"contacts": true,`), "sync.go should emit one critical flag per resource")
	runGoCommand(t, outputDir, "test", "./internal/cli", "./internal/store")
}

// TestGenerateFreshnessHelperEmitted verifies that the cliutil freshness
// helper and auto-refresh wrapper are emitted when the spec opts into
// cache, and that the resulting CLI compiles end-to-end and its cliutil
// tests pass.
func TestGenerateFreshnessHelperEmitted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("freshness")
	apiSpec.Cache = spec.CacheConfig{
		Enabled:    true,
		StaleAfter: "6h",
		Commands: []spec.CacheCommand{
			{Name: "dashboard", Resources: []string{"items"}},
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
		`"freshness-pp-cli dashboard": {`,
		`"items",`,
		`envOptOut := "FRESHNESS_NO_AUTO_REFRESH"`,
	} {
		assert.Contains(t, src, snippet, "auto_refresh.go missing %q", snippet)
	}
	optOutIndex := strings.Index(src, "env_opt_out")
	openStoreIndex := strings.Index(src, "store.OpenWithContext(ctx, dbPath)")
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
	assert.Contains(t, string(rootSrc), "flags.freshnessMeta = autoRefreshIfStale(cmd.Context(), flags, resources)",
		"root.go must invoke autoRefreshIfStale from PersistentPreRunE")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "## Freshness")
	assert.Contains(t, string(readme), "meta.freshness")
	assert.Contains(t, string(readme), "`freshness-pp-cli dashboard`")

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skill), "## Freshness Contract")
	assert.Contains(t, string(skill), "Covered paths:")

	// Generated helper and hook packages must compile, and cliutil tests
	// exercise the sync_state contract against a real SQLite DB.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli/...", "./internal/cliutil/...")
}

func TestGenerateFreshnessOptOutUsesASCIIPrefix(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Name = "pokéapi"
	apiSpec.Cache = spec.CacheConfig{Enabled: true}
	apiSpec.Config.Path = "~/.config/pokeapi-pp-cli/config.toml"

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	autoRefresh, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))
	require.NoError(t, err)
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)

	for _, content := range []string{string(autoRefresh), string(readme), string(skill)} {
		assert.Contains(t, content, "POKEAPI_NO_AUTO_REFRESH")
		assert.NotContains(t, content, "POKÉAPI_NO_AUTO_REFRESH")
	}
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
	assert.Contains(t, string(rootSrc), "rootCmd.AddCommand(newShareCmd(flags))",
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
	apiSpec.Description = "# Introduction\nAPI reference prose that should stay out of compact agent copy."
	apiSpec.CLIDescription = "Manage Stytch users and sessions from the terminal."

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
		"collectAgentCommands",
		`"pretty"`,
	} {
		assert.Contains(t, src, snippet, "agent_context.go missing %q", snippet)
	}
	// Field/value pair matched as a regex so column alignment from gofmt
	// (which shifts when the longest field name in the literal grows)
	// doesn't break the assertion.
	assert.Regexp(t, `Use:\s+"agent-context"`, src, `agent_context.go missing Use: "agent-context"`)
	assert.Contains(t, src, `Description: "Manage Stytch users and sessions from the terminal."`)
	assert.NotContains(t, src, "# Introduction")
	// agent-context only reads CLI tree state and emits JSON to stdout.
	// The runtime walker uses this annotation to set readOnlyHint on
	// the resulting MCP tool so hosts skip the per-call permission prompt.
	assert.Regexp(t, `Annotations:\s+map\[string\]string\{"mcp:read-only":\s*"true"\}`, src,
		"agent_context.go must carry mcp:read-only annotation")

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
	assert.Equal(t, "3", payload["schema_version"], "schema_version must be present")
	assert.Contains(t, payload, "cli")
	assert.Contains(t, payload, "auth")
	assert.Contains(t, payload, "commands")

	annotatedEndpoint := findAgentContextCommand(payload["commands"], func(command map[string]any) bool {
		annotations, ok := command["annotations"].(map[string]any)
		if !ok {
			return false
		}
		return annotations["pp:endpoint"] != "" && annotations["mcp:read-only"] == "true"
	})
	require.NotNil(t, annotatedEndpoint, "agent-context must surface endpoint and read-only annotations")

	unannotatedParent := findAgentContextCommand(payload["commands"], func(command map[string]any) bool {
		_, hasAnnotations := command["annotations"]
		subcommands, hasSubcommands := command["subcommands"].([]any)
		return !hasAnnotations && hasSubcommands && len(subcommands) > 0
	})
	require.NotNil(t, unannotatedParent, "agent-context must keep annotations omitted when absent")
}

func findAgentContextCommand(root any, match func(map[string]any) bool) map[string]any {
	commands, ok := root.([]any)
	if !ok {
		return nil
	}
	for _, raw := range commands {
		command, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if match(command) {
			return command
		}
		if found := findAgentContextCommand(command["subcommands"], match); found != nil {
			return found
		}
	}
	return nil
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

func TestGenerateBearerRefreshDoctorCommand(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "refreshbearer",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		BearerRefresh: spec.BearerRefreshConfig{
			BundleURL: "https://cdn.example.com/main.js",
			Pattern:   `"(AAAAAAAA[^"]+)"`,
		},
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"REFRESHBEARER_PUBLIC_BEARER"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/refreshbearer-pp-cli/config.toml",
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
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorGo)
	assert.Contains(t, doctor, `"refresh-bearer"`)
	assert.Contains(t, doctor, `"https://cdn.example.com/main.js"`)
	assert.Contains(t, doctor, "func newRefreshBearerCmd(")
	assert.Contains(t, doctor, "func runBearerRefresh(")
	assert.Contains(t, doctor, "func extractBearerToken(")

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), "rootCmd.AddCommand(newRefreshBearerCmd(flags))",
		"bearer refresh must be a non-framework command so the Cobra-tree MCP mirror exposes it")

	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configContent := string(configGo)
	assert.Contains(t, configContent, "BearerTokenRefreshedAt time.Time")
	assert.Contains(t, configContent, "func (c *Config) SaveBearerToken(")
	assert.Contains(t, configContent, `c.AuthSource = "bearer_refresh"`)
	assert.Contains(t, configContent, "c.RefreshbearerPublicBearer = \"\"")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "refreshbearer-pp-cli doctor --refresh-bearer")
	assert.Contains(t, string(readme), "refreshbearer-pp-cli refresh-bearer")

	configTest := `package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBearerRefreshSavedTokenWinsOverEnvBackedField(t *testing.T) {
	cfg := &Config{AccessToken: "fresh-token", RefreshbearerPublicBearer: "stale-token"}
	if got := cfg.AuthHeader(); got != "Bearer fresh-token" {
		t.Fatalf("AuthHeader() = %q, want refreshed token", got)
	}
	if cfg.AuthSource != "bearer_refresh" {
		t.Fatalf("AuthSource = %q, want bearer_refresh", cfg.AuthSource)
	}
}

func TestSaveBearerTokenClearsEnvBackedField(t *testing.T) {
	cfg := &Config{
		Path:                filepath.Join(t.TempDir(), "config.toml"),
		RefreshbearerPublicBearer: "stale-token",
	}
	if err := cfg.SaveBearerToken("fresh-token", time.Unix(123, 0)); err != nil {
		t.Fatal(err)
	}
	if cfg.RefreshbearerPublicBearer != "" {
		t.Fatalf("RefreshbearerPublicBearer = %q, want cleared", cfg.RefreshbearerPublicBearer)
	}
	if got := cfg.AuthHeader(); got != "Bearer fresh-token" {
		t.Fatalf("AuthHeader() = %q, want refreshed token", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "bearer_refresh_test.go"), []byte(configTest), 0o644))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestBearerRefresh")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateOAuth2ClientCredentialsAuthTemplate(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "ccgrant",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://api.example.com/oauth/token",
			EnvVars:     []string{"CCGRANT_API_KEY", "CCGRANT_SECRET_KEY"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/ccgrant-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	authBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	body := string(authBytes)

	// New login command with the right token URL hardcoded.
	assert.Contains(t, body, `newAuthLoginCmd(flags)`,
		"client_credentials template emits a login command")
	assert.Contains(t, body, `"https://api.example.com/oauth/token"`,
		"login command POSTs to the spec's TokenURL")
	assert.Contains(t, body, `"grant_type":    {"client_credentials"}`,
		"login command uses client_credentials grant")
	assert.Contains(t, body, `os.Getenv("CCGRANT_API_KEY")`,
		"client-id defaults to first env var")
	assert.Contains(t, body, `os.Getenv("CCGRANT_SECRET_KEY")`,
		"client-secret defaults to second env var")
	// Verify-env short-circuit is present so the side-effect classifier
	// doesn't false-positive during shipcheck.
	assert.Contains(t, body, `cliutil.IsVerifyEnv()`,
		"login command short-circuits under PRINTING_PRESS_VERIFY=1")
	assert.Contains(t, body, `Annotations: map[string]string{"mcp:hidden": "true"}`,
		"auth login is hidden from MCP")
}

func TestGenerateOAuth2AuthorizationCodeRegression(t *testing.T) {
	t.Parallel()

	// Spec with OAuth2 authorization_code (the existing 3-legged flow):
	// AuthorizationURL non-empty, OAuth2Grant unset (defaults to
	// authorization_code). Must continue to select auth.go.tmpl, NOT
	// auth_client_credentials.go.tmpl.
	apiSpec := &spec.APISpec{
		Name:    "ac3legged",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:             "bearer_token",
			Header:           "Authorization",
			Format:           "Bearer {token}",
			AuthorizationURL: "https://api.example.com/oauth/authorize",
			TokenURL:         "https://api.example.com/oauth/token",
			EnvVars:          []string{"AC3LEGGED_TOKEN"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/ac3legged-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	authBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	body := string(authBytes)

	// auth.go.tmpl emits the authorization_code grant body — should be
	// present, NOT the client_credentials body.
	assert.Contains(t, body, `"grant_type":   {"authorization_code"}`,
		"authorization_code spec keeps the existing 3-legged template")
	assert.NotContains(t, body, `"grant_type":    {"client_credentials"}`,
		"authorization_code spec must NOT pick the client_credentials template")
}

func TestGenerateOAuth2ClientCredentialsClientRefresh(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "ccclient",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://api.example.com/oauth/token",
			EnvVars:     []string{"CCCLIENT_API_KEY", "CCCLIENT_SECRET_KEY"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/ccclient-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	body := string(clientBytes)

	assert.Contains(t, body, "func needsClientCredentialsMint",
		"client_credentials spec emits the safety-window helper")
	assert.Contains(t, body, "func resolveClientCredentials",
		"client_credentials spec emits the env-var-fallback resolver")
	assert.Contains(t, body, "func (c *Client) mintClientCredentials",
		"client_credentials spec emits the proactive-mint helper")
	assert.Contains(t, body, `"grant_type":    {"client_credentials"}`,
		"refresh path uses client_credentials grant, not refresh_token")
	assert.Contains(t, body, `"https://api.example.com/oauth/token"`,
		"mint POSTs to the spec's TokenURL")
	assert.Contains(t, body, "time.Until(cfg.TokenExpiry) < 60*time.Second",
		"60-second proactive refresh window")
	assert.Contains(t, body, `os.Getenv("CCCLIENT_API_KEY")`,
		"client-id resolver falls back to first env var")
	assert.Contains(t, body, `os.Getenv("CCCLIENT_SECRET_KEY")`,
		"client-secret resolver falls back to second env var")
	assert.Contains(t, body, "ccMu *sync.Mutex",
		"client_credentials spec emits a shared mutex to serialize concurrent mints")
	assert.Contains(t, body, "ccMu:       &sync.Mutex{}",
		"client_credentials spec initializes the shared mint mutex")
	assert.Contains(t, body, "c.ccMu.Lock()",
		"authHeader takes the mint mutex before re-checking the window")
}

func TestGenerateOAuth2AuthorizationCodeClientRefreshUnchanged(t *testing.T) {
	t.Parallel()

	// Spec with the existing 3-legged flow (AuthorizationURL non-empty,
	// no OAuth2Grant). The client.go template's refresh path must keep
	// using the refresh_token grant — no regression.
	apiSpec := &spec.APISpec{
		Name:    "acclient",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:             "bearer_token",
			Header:           "Authorization",
			Format:           "Bearer {token}",
			AuthorizationURL: "https://api.example.com/oauth/authorize",
			TokenURL:         "https://api.example.com/oauth/token",
			EnvVars:          []string{"ACCLIENT_TOKEN"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/acclient-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	body := string(clientBytes)

	assert.Contains(t, body, `"grant_type":    {"refresh_token"}`,
		"authorization_code spec keeps refresh_token grant in refresh path")
	assert.NotContains(t, body, "func mintClientCredentials",
		"authorization_code spec must NOT emit client_credentials helpers")
	assert.NotContains(t, body, "func needsClientCredentialsMint",
		"authorization_code spec must NOT emit safety-window helper")
	assert.NotContains(t, body, "ccMu *sync.Mutex",
		"authorization_code spec must NOT emit the client_credentials mint mutex")
}

func TestGenerateAPIKeyAuthFormatSupportsTokenPlaceholder(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "prefix",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			In:      "header",
			Header:  "Authorization",
			Format:  "Klaviyo-API-Key {token}",
			EnvVars: []string{"PREFIX_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/prefix-pp-cli/config.toml",
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

	const inlineTest = `package config

import "testing"

func TestAPIKeyFormatTokenPlaceholder(t *testing.T) {
	cfg := &Config{PrefixApiKey: "secret"}
	if got := cfg.AuthHeader(); got != "Klaviyo-API-Key secret" {
		t.Fatalf("AuthHeader() = %q, want %q", got, "Klaviyo-API-Key secret")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "auth_format_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config")
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
	if testing.Short() && len(args) > 0 && (args[0] == "build" || args[0] == "test") {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	runGoCommandRequired(t, dir, args...)
}

func runGoCommandRequired(t *testing.T, dir string, args ...string) {
	t.Helper()

	// Generated-project compile tests exercise module resolution via -mod=mod;
	// production Validate still owns the go mod tidy quality gate.
	if len(args) >= 2 && args[0] == "mod" && args[1] == "tidy" {
		return
	}
	if len(args) > 0 && (args[0] == "build" || args[0] == "test") {
		args = append([]string{args[0], "-mod=mod"}, args[1:]...)
	}

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

func TestGenerate403HintsFollowAuthMode(t *testing.T) {
	t.Parallel()

	type snippetCheck struct {
		want   []string
		reject []string
	}
	assertSnippets := func(t *testing.T, body string, check snippetCheck) {
		t.Helper()
		for _, want := range check.want {
			assert.Contains(t, body, want)
		}
		for _, reject := range check.reject {
			assert.NotContains(t, body, reject)
		}
	}

	tests := []struct {
		name    string
		auth    spec.AuthConfig
		helpers snippetCheck
		mcp     snippetCheck
	}{
		{
			name: "no auth",
			auth: spec.AuthConfig{Type: "none"},
			helpers: snippetCheck{
				want: []string{"This API is configured without credentials", "rate limit, geography, bot protection, or endpoint policy"},
				reject: []string{
					"Your credentials are valid but lack access",
					"Check that your API key has the required permissions",
				},
			},
			mcp: snippetCheck{
				want:   []string{"this API is configured without credentials", "rate limit, geography, bot protection, or endpoint policy"},
				reject: []string{"your credentials are valid but lack access"},
			},
		},
		{
			name: "api key",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"MYAPI_TOKEN"},
			},
			helpers: snippetCheck{
				want: []string{
					"Your credentials are valid but lack access",
					"Check that your API key has the required permissions",
					"Set it with: export MYAPI_TOKEN=<your-key>",
				},
				reject: []string{"This API is configured without credentials"},
			},
			mcp: snippetCheck{
				want: []string{
					"your credentials are valid but lack access",
					"Set it with: export MYAPI_TOKEN=<your-key>",
				},
				reject: []string{"this API is configured without credentials"},
			},
		},
		{
			name: "oauth2",
			auth: spec.AuthConfig{
				Type:    "oauth2",
				EnvVars: []string{"MYAPI_TOKEN"},
			},
			helpers: snippetCheck{
				want: []string{"Your token may lack required scopes", "auth login"},
				reject: []string{
					"This API is configured without credentials",
					"Check that your API key has the required permissions",
				},
			},
			mcp: snippetCheck{
				want: []string{"your token may lack required scopes", "auth login"},
				reject: []string{
					"this API is configured without credentials",
					"your credentials are valid but lack access",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("auth403" + strings.ReplaceAll(tt.name, " ", ""))
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
			assertSnippets(t, helpers, tt.helpers)

			tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
			assertSnippets(t, tools, tt.mcp)
		})
	}
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
	assert.Contains(t, string(gomod), "go 1.26.3")
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
	// The H3 sibling below compiles the same surf-backed client path; this
	// test stays focused on the non-H3 rendering branch.
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
	runGoCommand(t, outputDir, "test", "./internal/client")
}

func TestGenerateBrowserHTTPTransportDisablesHTTP2(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:          "websurfacehttp",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserHTTP,
		Auth:          spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/websurfacehttp-pp-cli/config.toml",
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

	outputDir := filepath.Join(t.TempDir(), "websurfacehttp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientGo := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientGo, `"crypto/tls"`)
	assert.Contains(t, clientGo, `transport := http.DefaultTransport.(*http.Transport).Clone()`)
	assert.Contains(t, clientGo, `transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)`)
	assert.NotContains(t, clientGo, `"github.com/enetx/surf"`)
	assert.NotContains(t, clientGo, `Impersonate()`)

	gomod := readGeneratedFile(t, outputDir, "go.mod")
	assert.NotContains(t, gomod, "github.com/enetx/surf")
}

func TestGenerateHTMLExtractionEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/docs/page":
			_, _ = w.Write([]byte(`<html><head><title>Docs</title></head><body><a href="child">Child page</a></body></html>`))
		case "/makers":
			// Lazy-load fixture: anchor's <img> has a placeholder src + real
			// data-src (Pinterest/NYT Cooking pattern). firstImageSrc must
			// prefer data-src over src.
			_, _ = w.Write([]byte(`<html><head><title>Makers</title></head><body><a href="/@alice"><img src="data:image/gif;base64,placeholder" data-src="/img/alice-real.jpg" alt="Alice">Alice</a><a href="/@bob"><img srcset="/img/bob-1x.jpg 1x, /img/bob-2x.jpg 2x" alt="Bob">Bob</a></body></html>`))
		default:
			// Anchor 1 wraps its image in <noscript> (Dotdash/Meredith pattern).
			// nodeTextSuppressing must skip the noscript subtree so "<img src=...>"
			// markup does not leak into the link's name field. firstImageSrc must
			// also skip the noscript image and look for a rendered <img> instead --
			// here SpeakON has both: a noscript fallback AND a rendered img after.
			// Anchor 2 has only a rendered <img> (no noscript fallback).
			_, _ = w.Write([]byte(`<html>
			<head><title>Product Hunt</title><meta name="description" content="New products"></head>
			<body>
				<a href="/products/speakon"><noscript><img src="/img/speakon-fallback.jpg" alt="SpeakON"></noscript><img src="/img/speakon.jpg" alt="SpeakON">1. SpeakON</a>
				<a href="/products/instant-db"><img src="/img/instant-db.jpg" alt="InstantDB">2. InstantDB</a>
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
	assert.Contains(t, string(gomod), "golang.org/x/net v0.53.0")

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
	// noscript suppression: the anchor wrapped its fallback <img> in <noscript>,
	// which the HTML5 spec parses as raw text. nodeTextSuppressing must skip
	// the noscript subtree so the link name does not leak "<img src=..." markup.
	assert.NotContains(t, links[0]["name"], "<img", "noscript-wrapped <img> markup must not leak into the link name")
	assert.NotContains(t, links[0]["text"], "<img", "noscript-wrapped <img> markup must not leak into the link text")
	// Image extraction: firstImageSrc walks the same suppression-aware tree
	// and surfaces the first non-suppressed <img src>. The rendered image
	// should win over the noscript fallback when both are present.
	assert.Contains(t, links[0]["image"], "speakon.jpg", "expected rendered img URL, got %v", links[0]["image"])
	assert.NotContains(t, links[0]["image"], "speakon-fallback.jpg", "noscript fallback must not be selected when a rendered image exists")
	// Anchor without noscript still produces a clean image URL.
	assert.Contains(t, links[1]["image"], "instant-db.jpg")

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
	require.Len(t, envelope.Results, 2)
	assert.Equal(t, server.URL+"/@alice", envelope.Results[0]["url"])
	assert.Equal(t, "alice", envelope.Results[0]["slug"])
	// Lazy-load priority: data-src wins over a placeholder src. The fixture
	// embeds a base64 1x1 gif in src and the real URL in data-src. The result
	// must be the data-src URL, NOT the placeholder.
	assert.Contains(t, envelope.Results[0]["image"], "alice-real.jpg",
		"data-src should win over a placeholder src; got %v", envelope.Results[0]["image"])
	assert.NotContains(t, envelope.Results[0]["image"], "data:image/gif",
		"placeholder src should not be selected when data-src is present")
	// srcset fallback: when src and data-src are absent, the first srcset URL
	// is taken. The fixture has only `srcset="/img/bob-1x.jpg 1x, ..."`, so
	// firstSrcsetURL should extract the 1x URL.
	assert.Equal(t, server.URL+"/@bob", envelope.Results[1]["url"])
	assert.Contains(t, envelope.Results[1]["image"], "bob-1x.jpg",
		"first srcset URL should be selected when src is absent; got %v", envelope.Results[1]["image"])
}

// TestGenerateHTMLExtractionEmbeddedJSONMode exercises the embedded-json mode
// against an SSR-React-style page where serialized state is embedded in a
// known script tag. This matches the Food52 retro motivation: extracting
// `__NEXT_DATA__` JSON and walking a dot-notation path into props.pageProps.
func TestGenerateHTMLExtractionEmbeddedJSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/recipes":
			// Canonical Next.js __NEXT_DATA__ shape: serialized page state
			// embedded in a script tag with id="__NEXT_DATA__".
			_, _ = w.Write([]byte(`<html><head><title>Recipes</title></head><body>
				<div id="__next">rendered</div>
				<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"recipes":[{"id":1,"name":"Pasta"},{"id":2,"name":"Soup"}]}},"page":"/recipes"}</script>
			</body></html>`))
		case "/articles":
			// Custom selector + empty json_path returns the entire parsed JSON.
			_, _ = w.Write([]byte(`<html><body>
				<script id="ARTICLE_DATA" type="application/json">{"items":[{"slug":"a"},{"slug":"b"}]}</script>
			</body></html>`))
		case "/missing":
			// No matching script tag — should produce an extractor error.
			_, _ = w.Write([]byte(`<html><body><p>nothing here</p></body></html>`))
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:    "embeddedjson",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/embeddedjson-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"recipes": {
				Description: "Browse recipes",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/recipes",
						Description:    "List recipes",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:     spec.HTMLExtractModeEmbeddedJSON,
							JSONPath: "props.pageProps.recipes",
						},
						Response: spec.ResponseDef{Type: "array", Item: "object"},
					},
				},
			},
			"articles": {
				Description: "Browse articles",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/articles",
						Description:    "List articles via custom selector",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:           spec.HTMLExtractModeEmbeddedJSON,
							ScriptSelector: "script#ARTICLE_DATA",
						},
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"missing": {
				Description: "Missing script tag",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/missing",
						Description:    "Page with no embedded JSON",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:     spec.HTMLExtractModeEmbeddedJSON,
							JSONPath: "anything",
						},
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "embeddedjson-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "html_extract.go"))

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "embeddedjson-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/embeddedjson-pp-cli")

	// Default selector + dot-notation path: returns the recipes array.
	cmd := exec.Command(binaryPath, "recipes", "list", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var recipesEnv struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &recipesEnv), string(out))
	require.Len(t, recipesEnv.Results, 2)
	assert.Equal(t, "Pasta", recipesEnv.Results[0]["name"])
	assert.Equal(t, "Soup", recipesEnv.Results[1]["name"])

	// Custom selector + empty json_path: returns the whole parsed JSON.
	cmd = exec.Command(binaryPath, "articles", "list", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var articleEnv struct {
		Results map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &articleEnv), string(out))
	items, ok := articleEnv.Results["items"].([]any)
	require.True(t, ok, "expected items array, got %T", articleEnv.Results["items"])
	require.Len(t, items, 2)

	// Missing script tag: extractor reports an actionable error rather
	// than silently returning empty data.
	cmd = exec.Command(binaryPath, "missing", "list", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	assert.Contains(t, string(out), "embedded-json")
}

// TestGenerateHTMLExtractionPerModeGating asserts that html_extract.go is
// emitted with only the helpers that match the mode(s) the spec actually
// uses. A spec that declares only mode: embedded-json must NOT emit the
// page-mode DOM walkers (htmlExtractedPage, applyMeta, htmlLink, etc.);
// a spec that declares only mode: page must NOT emit the embedded-json
// walker (extractEmbeddedJSON, walkJSONDotPath); a mixed spec emits both.
// This is the core U6 retro contract: per-mode gating eliminates the
// dead-helper count in the printed CLI.
func TestGenerateHTMLExtractionPerModeGating(t *testing.T) {
	t.Parallel()

	specWithMode := func(name, mode string) *spec.APISpec {
		s := &spec.APISpec{
			Name:    name,
			Version: "0.1.0",
			BaseURL: "https://example.com",
			Auth:    spec.AuthConfig{Type: "none"},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/" + name + "-pp-cli/config.toml",
			},
			Resources: map[string]spec.Resource{
				"posts": {
					Description: "Browse posts",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:         "GET",
							Path:           "/",
							Description:    "List posts",
							ResponseFormat: spec.ResponseFormatHTML,
							HTMLExtract: &spec.HTMLExtract{
								Mode:     mode,
								JSONPath: "props.pageProps.posts",
							},
							Response: spec.ResponseDef{Type: "array", Item: "object"},
						},
					},
				},
			},
		}
		return s
	}

	read := func(t *testing.T, dir string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "html_extract.go"))
		require.NoError(t, err)
		return string(data)
	}

	// Helpers are matched by their declaration line ("func name(" or
	// "type name") so a substring mention inside a comment doesn't
	// cause a false positive — the dispatcher's comment legitimately
	// names the page-mode walker even when the page-mode branch is
	// gated out.
	hasFunc := func(body, name string) bool {
		return strings.Contains(body, "func "+name+"(")
	}
	hasType := func(body, name string) bool {
		return strings.Contains(body, "type "+name+" ")
	}

	t.Run("embedded-json-only omits page+links helpers", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "ejonly-pp-cli")
		require.NoError(t, New(specWithMode("ejonly", spec.HTMLExtractModeEmbeddedJSON), dir).Generate())
		body := read(t, dir)
		// embedded-json branch present
		assert.True(t, hasFunc(body, "extractEmbeddedJSON"))
		assert.True(t, hasFunc(body, "walkJSONDotPath"))
		// page+links branch absent
		assert.False(t, hasFunc(body, "extractHTMLPageOrLinks"))
		assert.False(t, hasType(body, "htmlExtractedPage"))
		assert.False(t, hasFunc(body, "applyMeta"))
		assert.False(t, hasFunc(body, "extractHTMLLink"))
		assert.False(t, hasFunc(body, "looksLikeHTMLChallenge"))
		assert.False(t, hasFunc(body, "nodeTextSuppressing"))
		assert.False(t, hasFunc(body, "firstImageSrc"))
		assert.False(t, hasFunc(body, "cleanHTMLText"))
		// Imports that only the page+links branch needs are not emitted
		assert.NotContains(t, body, `"regexp"`)
		assert.NotContains(t, body, `"strconv"`)
		assert.NotContains(t, body, `stdhtml "html"`)
		// Must still build cleanly even with helpers gated out
		runGoCommand(t, dir, "mod", "tidy")
		runGoCommand(t, dir, "build", "./...")
	})

	t.Run("page-only omits embedded-json helpers", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "pageonly-pp-cli")
		require.NoError(t, New(specWithMode("pageonly", spec.HTMLExtractModePage), dir).Generate())
		body := read(t, dir)
		// page branch present
		assert.True(t, hasFunc(body, "extractHTMLPageOrLinks"))
		assert.True(t, hasType(body, "htmlExtractedPage"))
		assert.True(t, hasFunc(body, "applyMeta"))
		// embedded-json branch absent
		assert.False(t, hasFunc(body, "extractEmbeddedJSON"))
		assert.False(t, hasFunc(body, "walkJSONDotPath"))
		assert.False(t, hasFunc(body, "parseSimpleSelector"))
		assert.False(t, hasFunc(body, "findScriptByTagAndID"))
		// Must still build cleanly
		runGoCommand(t, dir, "mod", "tidy")
		runGoCommand(t, dir, "build", "./...")
	})

	t.Run("mixed modes emit both branches", func(t *testing.T) {
		t.Parallel()

		// One endpoint per mode in the same spec.
		mixedSpec := &spec.APISpec{
			Name:    "mixed",
			Version: "0.1.0",
			BaseURL: "https://example.com",
			Auth:    spec.AuthConfig{Type: "none"},
			Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/mixed-pp-cli/config.toml"},
			Resources: map[string]spec.Resource{
				"posts": {
					Description: "Posts",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET", Path: "/", Description: "List",
							ResponseFormat: spec.ResponseFormatHTML,
							HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
							Response:       spec.ResponseDef{Type: "object"},
						},
					},
				},
				"data": {
					Description: "Embedded",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET", Path: "/data", Description: "List from embedded JSON",
							ResponseFormat: spec.ResponseFormatHTML,
							HTMLExtract: &spec.HTMLExtract{
								Mode:     spec.HTMLExtractModeEmbeddedJSON,
								JSONPath: "props.pageProps.data",
							},
							Response: spec.ResponseDef{Type: "object"},
						},
					},
				},
			},
		}
		dir := filepath.Join(t.TempDir(), "mixed-pp-cli")
		require.NoError(t, New(mixedSpec, dir).Generate())
		body := read(t, dir)
		assert.Contains(t, body, "extractHTMLPageOrLinks")
		assert.Contains(t, body, "extractEmbeddedJSON")
		runGoCommand(t, dir, "mod", "tidy")
		runGoCommand(t, dir, "build", "./...")
	})
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
	assert.Contains(t, string(gomod), "go 1.26.3")
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
	assert.Contains(t, string(readme), "npx -y @mvanhorn/printing-press install owned")
	assert.NotContains(t, string(readme), "library/other/owned")
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

// TestGenerateStoreMigrateUsesBeginImmediate is a fast canary that the
// emitted store.go runs migrations inside a BEGIN IMMEDIATE transaction
// pinned to a single connection. Without it, parallel Open() against a
// fresh DB races per CREATE TABLE statement and trips SQLITE_BUSY despite
// the busy_timeout. The runtime concurrency check ships into every
// generated CLI's store package; this test fails the generator
// immediately on regression so the slower runtime check doesn't have
// to be the first signal.
func TestGenerateStoreMigrateUsesBeginImmediate(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "begin-immediate-canary",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/begin-immediate-canary-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"things": {
				Description: "Things",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/things",
						Description: "List things",
						Response:    spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "id", Type: "string"},
							{Name: "name", Type: "string"},
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

	// Stripping comments first prevents false negatives from a refactor that
	// removes the call sites but leaves explanatory prose containing the same
	// keywords behind. The check would otherwise pass on comments alone.
	codeOnly := stripGoComments(src)

	assert.Contains(t, codeOnly, `func OpenWithContext(`,
		"store package must expose OpenWithContext so callers can interrupt slow migrations with their own ctx")
	assert.Contains(t, codeOnly, `func (s *Store) migrate(ctx context.Context) error`,
		"migrate must accept a context.Context so caller cancellation propagates into the retry loop")
	assert.Contains(t, codeOnly, `withMigrationLock(`,
		"migrate must dispatch the lock helper — without this call, the BEGIN/COMMIT wrapper is unreachable")
	assert.Contains(t, codeOnly, `s.db.Conn(ctx)`,
		"migrate must pin a connection — BEGIN/COMMIT pairs must run on the same connection")
	assert.Contains(t, codeOnly, `BEGIN IMMEDIATE`,
		"migrate must wrap migrations in BEGIN IMMEDIATE so concurrent fresh-DB Opens serialize on the RESERVED lock instead of racing per-statement")
	assert.Contains(t, codeOnly, `COMMIT`,
		"migrate must commit the transaction explicitly")
	// Fast-path: reading user_version on the pinned connection BEFORE the
	// migration lock is what lets an old binary refuse a newer-schema DB
	// without waiting out migrationLockTimeout.
	assert.Regexp(t, `(?s)func \(s \*Store\) migrate\(ctx context\.Context\) error \{.*PRAGMA user_version.*withMigrationLock`, codeOnly,
		"migrate must read PRAGMA user_version BEFORE entering withMigrationLock so newer-DB rejection happens before lock acquisition")
}

// TestGenerateMCPSQLToolUsesReadOnlyStore guards the agent-native security
// model. The MCP sql and search tools advertise readOnlyHint=true to MCP
// hosts so the host auto-approves invocations; a false readOnlyHint on a
// mutating tool lets prompt-injected synced data exfiltrate the local
// database without a permission prompt. Two layers must both be present:
// (1) handleSQL gates the query through validateReadOnlyQuery — an
// allowlist of SELECT/WITH applied AFTER stripping leading SQL comments
// and semicolons that SQLite skips before parsing — and (2) the store
// handle is OpenReadOnly, whose mode=ro driver flag rejects direct and
// CTE-wrapped writes. Re-wiring these handlers to OpenWithContext, or
// reverting to a HasPrefix-on-blocklist gate that misses comment-prefixed
// bypasses, silently re-opens the exfiltration vector. Behavioral
// coverage (the stripper and allowlist functioning end-to-end on attack
// inputs) lives in the emitted tools_test.go; this test pins the
// machine-level emission shape so a regression fails fast.
func TestGenerateMCPSQLToolUsesReadOnlyStore(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ro-canary")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeCode := stripGoComments(string(storeSrc))
	assert.Contains(t, storeCode, `func OpenReadOnly(`,
		"store package must expose OpenReadOnly so MCP read tools open a write-rejecting connection")
	// modernc.org/sqlite only honors SQLite URI query parameters when
	// the DSN starts with "file:". Without the prefix, ?mode=ro is
	// silently dropped and writes succeed against the supposedly
	// read-only handle.
	assert.Contains(t, storeCode, `"file:"+dbPath+"?mode=ro`,
		"OpenReadOnly DSN must use the file: URI prefix with mode=ro")

	mcpSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	mcpCode := stripGoComments(string(mcpSrc))

	assert.NotRegexp(t, `(?s)func handleSQL\(.*store\.OpenWithContext\(`, mcpCode,
		"handleSQL must use store.OpenReadOnly, not OpenWithContext")
	assert.NotRegexp(t, `(?s)func handleSearch\(.*store\.OpenWithContext\(`, mcpCode,
		"handleSearch must use store.OpenReadOnly, not OpenWithContext")
	assert.Regexp(t, `(?s)func handleSQL\(.*store\.OpenReadOnly\(`, mcpCode,
		"handleSQL must open the store via OpenReadOnly")
	assert.Regexp(t, `(?s)func handleSearch\(.*store\.OpenReadOnly\(`, mcpCode,
		"handleSearch must open the store via OpenReadOnly")

	assert.Contains(t, mcpCode, `func validateReadOnlyQuery(`,
		"mcp package must expose validateReadOnlyQuery — the gate that handleSQL must call before opening the store")
	assert.Contains(t, mcpCode, `func stripLeadingSQLNoise(`,
		"mcp package must expose stripLeadingSQLNoise — without it the gate's HasPrefix check is bypassable by SQL comments and semicolons that SQLite skips before parsing")
	// handleSQL must dispatch through validateReadOnlyQuery; a regression
	// to inline HasPrefix-on-blocklist would silently restore the
	// comment-prefix bypass.
	assert.Regexp(t, `(?s)func handleSQL\(.*validateReadOnlyQuery\(`, mcpCode,
		"handleSQL must call validateReadOnlyQuery before opening the store")
	// Allowlist: SELECT and WITH only. WITH supports SELECT-form CTEs;
	// CTE-wrapped writes are caught one layer down by mode=ro.
	assert.Contains(t, mcpCode, `"SELECT"`,
		"validateReadOnlyQuery allowlist must include SELECT")
	assert.Contains(t, mcpCode, `"WITH"`,
		"validateReadOnlyQuery allowlist must include WITH for SELECT-form CTEs")

	mcpTestSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools_test.go"))
	require.NoError(t, err)
	mcpTestCode := string(mcpTestSrc)
	assert.Contains(t, mcpTestCode, "TestValidateReadOnlyQuery_RejectsBypassVectors",
		"behavioral coverage of comment-prefix and statement-separator bypass vectors must ship into every printed CLI's mcp package")
}

// stripGoComments removes // line comments and /* ... */ block comments from
// Go source. Crude but sufficient for canary assertions on emitted templates;
// it doesn't try to parse string literals (none of the asserted substrings
// appear in literals in the templates we check).
func stripGoComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
			continue
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
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
// TestSyncExtractPaginationNestedCursor verifies the emitted
// extractPaginationFromEnvelope finds cursors inside well-known wrapper
// objects (Slack response_metadata, etc.). Combines a generator-level
// emission canary with a behavioral test written into the generated
// tree, since either tier alone misses real regressions.
func TestSyncExtractPaginationNestedCursor(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "slacky",
		Version: "0.1.0",
		BaseURL: "https://slacky.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/slacky-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"messages": {
				Description: "Messages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/messages",
						Description: "List messages",
						Response:    spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "channel", Type: "string"},
							{Name: "cursor", Type: "string"},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)

	// Tier 1: emission canary. The wrapper-key recursion must ship.
	assert.Contains(t, src, "paginationWrapperKeys",
		"sync.go must declare paginationWrapperKeys for nested-cursor support")
	assert.Contains(t, src, "nextCursorFromLinks",
		"sync.go must parse JSON:API links.next cursors")
	assert.Contains(t, src, `"response_metadata"`,
		"paginationWrapperKeys must include response_metadata (Slack's envelope)")
	assert.Contains(t, src, `"pagination"`,
		"paginationWrapperKeys must include pagination (MongoDB Atlas-style)")

	// Tier 2: behavioral test, written into the generated tree as a
	// same-package _test.go so it can call the unexported helper.
	const inlineTest = `package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractPageItemsSlackEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"ok": true,
		"messages": [{"text":"hello"},{"text":"world"}],
		"response_metadata": {"next_cursor": "abc123"}
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if cursor != "abc123" {
		t.Fatalf("want cursor abc123, got %q", cursor)
	}
	if !hasMore {
		t.Fatalf("want hasMore=true when cursor is present")
	}
}

func TestExtractPageItemsMongoPaginationEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"results": [{"_id":"a"},{"_id":"b"}],
		"pagination": {"next_page_token": "tok-2"}
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if cursor != "tok-2" {
		t.Fatalf("want cursor tok-2, got %q", cursor)
	}
	if !hasMore {
		t.Fatalf("want hasMore=true when nested cursor is present")
	}
}

func TestExtractPageItemsJSONAPILinksNext(t *testing.T) {
	body := []byte(` + "`" + `{
		"data": [{"id":"pet_1"},{"id":"pet_2"}],
		"links": {
			"next": "https://api.example.com/pets?page%5Bcursor%5D=opaque-cursor&page%5Bsize%5D=2"
		}
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "page[cursor]")
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if cursor != "opaque-cursor" {
		t.Fatalf("want cursor opaque-cursor, got %q", cursor)
	}
	if !hasMore {
		t.Fatalf("want hasMore=true when links.next cursor is present")
	}
}

func TestExtractPageItemsJSONAPILinksNextUnencodedBrackets(t *testing.T) {
	body := []byte(` + "`" + `{
		"data": [{"id":"pet_1"}],
		"links": {
			"next": "https://api.example.com/pets?page[cursor]=raw-brackets"
		}
	}` + "`" + `)
	_, cursor, hasMore := extractPageItems(json.RawMessage(body), "page[cursor]")
	if cursor != "raw-brackets" {
		t.Fatalf("want cursor raw-brackets, got %q", cursor)
	}
	if !hasMore {
		t.Fatalf("want hasMore=true when links.next cursor is present")
	}
}

// Fast path: top-level cursor with no wrapper present at all. Proves
// the common case (Stripe, GitHub, Linear, Notion) doesn't enter the
// wrapper recursion. Pairs with TopLevelCursorWinsOverWrapper which
// pairs both forms; this isolates the no-wrapper shape.
func TestExtractPageItemsTopLevelCursorOnly(t *testing.T) {
	body := []byte(` + "`" + `{
		"items": [{"id":1},{"id":2}],
		"next_cursor": "page-2"
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if cursor != "page-2" {
		t.Fatalf("want top-level cursor page-2, got %q", cursor)
	}
	if !hasMore {
		t.Fatalf("want hasMore=true when cursor is present")
	}
}

// Negative case: top-level cursor should still win even when a
// non-cursor field happens to live in a wrapper-named key.
func TestExtractPageItemsTopLevelCursorWinsOverWrapper(t *testing.T) {
	body := []byte(` + "`" + `{
		"items": [{"id":1}],
		"next_cursor": "top-level-wins",
		"meta": {"next_cursor": "should-not-be-used"}
	}` + "`" + `)
	_, cursor, _ := extractPageItems(json.RawMessage(body), "cursor")
	if cursor != "top-level-wins" {
		t.Fatalf("want top-level cursor, got %q", cursor)
	}
}

// Negative case: no cursor anywhere returns empty without spurious
// nested matches on unrelated wrapper-named objects.
func TestExtractPageItemsNoCursor(t *testing.T) {
	body := []byte(` + "`" + `{
		"items": [{"id":1}],
		"meta": {"count": 1, "page": 1}
	}` + "`" + `)
	_, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if cursor != "" {
		t.Fatalf("want empty cursor, got %q", cursor)
	}
	if hasMore {
		t.Fatalf("want hasMore=false when no cursor present")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "sync_pagination_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestExtractPageItems", "./internal/cli")
}

func adsCampaignSpec() *spec.APISpec {
	return &spec.APISpec{
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
						// Column derivation reads the response shape (Types
						// entry below), not request Params, so the fixture
						// must declare Response.Item.
						Response: spec.ResponseDef{Type: "array", Item: "Campaign"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Campaign": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "status", Type: "string"},
					{Name: "account_id", Type: "string"},
				},
			},
		},
	}
}

func TestGenerateStoreUpsertBatchDispatchesToTypedTable(t *testing.T) {
	t.Parallel()

	apiSpec := adsCampaignSpec()
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

func TestLiveFetchWriteThroughCachePopulatesTypedTable(t *testing.T) {
	t.Parallel()

	apiSpec := adsCampaignSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

func TestWriteThroughCachePopulatesTypedTable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "campaigns", json.RawMessage(` + "`" + `[
		{"id":"camp_1","name":"Launch","status":"active","account_id":"acct_1"}
	]` + "`" + `))

	db, err := store.Open(defaultDBPath("ads-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var typedCount int
	var accountID string
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*), COALESCE(MAX(account_id), '') FROM campaigns` + "`" + `).Scan(&typedCount, &accountID); err != nil {
		t.Fatalf("query campaigns: %v", err)
	}
	if typedCount != 1 || accountID != "acct_1" {
		t.Fatalf("typed campaigns = %d/%q, want 1/acct_1", typedCount, accountID)
	}

	writeThroughCache(context.Background(), "events", json.RawMessage(` + "`" + `[
		{"id":"evt_1","name":"Seen"}
	]` + "`" + `))

	var genericCount int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'events'` + "`" + `).Scan(&genericCount); err != nil {
		t.Fatalf("query generic resources: %v", err)
	}
	if genericCount != 1 {
		t.Fatalf("generic events count = %d, want 1", genericCount)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "write_through_cache_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestWriteThroughCachePopulatesTypedTable", "./internal/cli")
}

func TestSyncDiscriminatorDispatchRoutesMixedItemsToTypedTables(t *testing.T) {
	t.Parallel()

	typedListEndpoint := func(path string) spec.Endpoint {
		// Response.Item drives typed-column emission via the TypedEntity
		// Types entry below. Without it the discriminator-routed tables
		// would degrade to id/data/synced_at and the dispatch test would
		// have nowhere to write rows.
		return spec.Endpoint{
			Method:      "GET",
			Path:        path,
			Description: "List typed entities",
			Response:    spec.ResponseDef{Type: "array", Item: "TypedEntity"},
		}
	}

	apiSpec := &spec.APISpec{
		Name:    "mixednet",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/mixednet-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"network_entities": {
				Description: "Mixed network entities",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/network-entities",
						Description: "List mixed network entities",
						Response:    spec.ResponseDef{Type: "array", Item: "NetworkEntity"},
						Params: []spec.Param{
							{Name: "limit", Type: "integer"},
							{Name: "cursor", Type: "string"},
						},
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
			"workspaces": {
				Description: "Workspaces",
				Endpoints:   map[string]spec.Endpoint{"list": typedListEndpoint("/workspaces")},
			},
			"collections": {
				Description: "Collections",
				Endpoints:   map[string]spec.Endpoint{"list": typedListEndpoint("/collections")},
			},
			"teams": {
				Description: "Teams",
				Endpoints:   map[string]spec.Endpoint{"list": typedListEndpoint("/teams")},
			},
		},
		Types: map[string]spec.TypeDef{
			"NetworkEntity": {
				Fields: []spec.TypeField{
					{Name: "type", Type: "string", Enum: []string{"workspace", "collection", "team"}},
					{Name: "id", Type: "string"},
				},
			},
			"TypedEntity": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "type", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)
	assert.Contains(t, src, `"workspace": "workspaces"`)
	assert.Contains(t, src, `upsertResourceBatch(db, resource, items)`)

	inlineTest := fmt.Sprintf(`package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"%s/internal/store"
)

func TestUpsertResourceBatchRoutesDiscriminatorItems(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open: %%v", err)
	}
	defer s.Close()

	items := []json.RawMessage{
		json.RawMessage(`+"`"+`{"type":"workspace","id":"w1","name":"Workspace","created_at":"2026-01-01T00:00:00Z"}`+"`"+`),
		json.RawMessage(`+"`"+`{"type":"collection","id":"c1","name":"Collection","created_at":"2026-01-01T00:00:00Z"}`+"`"+`),
		json.RawMessage(`+"`"+`{"type":"team","id":"t1","name":"Team","created_at":"2026-01-01T00:00:00Z"}`+"`"+`),
	}
	stored, extractFailures, err := upsertResourceBatch(s, "network_entities", items)
	if err != nil {
		t.Fatalf("upsertResourceBatch: %%v", err)
	}
	if stored != len(items) || extractFailures != 0 {
		t.Fatalf("stored/extractFailures = %%d/%%d, want %%d/0", stored, extractFailures, len(items))
	}

	for _, table := range []string{"workspaces", "collections", "teams"} {
		var count int
		if err := s.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %%s: %%v", table, err)
		}
		if count != 1 {
			t.Fatalf("%%s count = %%d, want 1", table, count)
		}
	}
}
`, naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_discriminator_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "./internal/store")
}

func TestGenerateStoreBackfillsIndexedColumnsOnUpgrade(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "indexedupgrade",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/indexedupgrade-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"emails": {
				Description: "Manage emails",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/emails",
						Description: "List emails",
						Response:    spec.ResponseDef{Type: "array", Item: "Email"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Email": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "email_id", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "description", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
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
	assert.Contains(t, src, `{table: "emails", column: "email_id", decl: "TEXT"}`)
	assert.Contains(t, src, `{table: "emails", column: "created_at", decl: "DATETIME"}`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestGenerateStoreQuotesNumericTableAndDerivedIdentifiers(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "numericstore",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/numericstore-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"0": {
				Description: "Manage numeric resources",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/0",
						Description: "List numeric resources",
						Response:    spec.ResponseDef{Type: "array", Item: "Numeric"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Numeric": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "replay_id", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "description", Type: "string"},
					{Name: "message", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
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
	assert.Contains(t, src, `CREATE TABLE IF NOT EXISTS "0"`)
	assert.Contains(t, src, `CREATE VIRTUAL TABLE IF NOT EXISTS "0_fts"`)
	assert.Contains(t, src, `CREATE TRIGGER IF NOT EXISTS "0_ai"`)
	assert.Contains(t, src, `content='0'`)
	assert.NotContains(t, src, "func (s *Store) upsert0Tx(")
	assert.Contains(t, src, "func (s *Store) upsertV0Tx(")

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

func TestGeneratedHelpers_IdempotentNoopsRequireOptIn(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "testidempotent",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Resources: map[string]spec.Resource{
			"teams": {
				Description: "Manage teams",
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/teams", Description: "Create team"},
					"delete": {Method: "DELETE", Path: "/teams/{id}", Description: "Delete team"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "testidempotent-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "idempotent_helpers_test.go")
	inlineTest := `package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
)

func captureStdoutStderr(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	oldOut := os.Stdout
	oldErr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	os.Stderr = errW
	callErr := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	out, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := io.ReadAll(errR)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), string(errOut), callErr
}

func requireNoopJSON(t *testing.T, body, reason string) {
	t.Helper()
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("noop output must be JSON: %v; body=%q", err, body)
	}
	if got["status"] != "noop" || got["reason"] != reason {
		t.Fatalf("unexpected noop envelope: %#v", got)
	}
}

func TestClassifyAPIError409RequiresIdempotent(t *testing.T) {
	err := classifyAPIError(errors.New("HTTP 409: conflict"), &rootFlags{})
	if err == nil {
		t.Fatal("409 without --idempotent must be an error")
	}
	if ExitCode(err) != 5 {
		t.Fatalf("409 should classify as API error, got exit %d", ExitCode(err))
	}

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		return classifyAPIError(errors.New("HTTP 409: conflict"), &rootFlags{idempotent: true, asJSON: true})
	})
	if err != nil {
		t.Fatalf("idempotent 409 returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("json noop should not write stderr, got %q", stderr)
	}
	requireNoopJSON(t, stdout, "already_exists")
}

func TestClassifyDeleteError404RequiresIgnoreMissing(t *testing.T) {
	err := classifyDeleteError(errors.New("HTTP 404: not found"), &rootFlags{})
	if err == nil {
		t.Fatal("404 delete without --ignore-missing must be an error")
	}
	if ExitCode(err) != 3 {
		t.Fatalf("404 should classify as not found, got exit %d", ExitCode(err))
	}

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		return classifyDeleteError(errors.New("HTTP 404: not found"), &rootFlags{ignoreMissing: true, asJSON: true})
	})
	if err != nil {
		t.Fatalf("ignore-missing 404 returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("json noop should not write stderr, got %q", stderr)
	}
	requireNoopJSON(t, stdout, "already_deleted")
}
`
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli")
}

func TestGeneratedExport_ValidatesResourceArgument(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "testexport",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/testexport-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"stories": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/stories"}}},
			"items":   {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}}},
			"users":   {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/users"}}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "testexport-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), "rootCmd.AddCommand(newExportCmd(flags))")

	exportGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "export.go"))
	require.NoError(t, err)
	exportContent := string(exportGo)
	assert.Contains(t, exportContent, `"items": true`)
	assert.Contains(t, exportContent, `"stories": true`)
	assert.Contains(t, exportContent, `"users": true`)
	assert.Contains(t, exportContent, `unknown resource %q; valid: %s`)

	runGoCommandRequired(t, outputDir, "build", "-o", "./testexport-pp-cli", "./cmd/testexport-pp-cli")
	cmd := exec.Command(filepath.Join(outputDir, "testexport-pp-cli"), "export", "storiez")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), `unknown resource "storiez"; valid: items, stories, users`)
	if exitErr, ok := err.(*exec.ExitError); ok {
		assert.Equal(t, 2, exitErr.ExitCode())
	} else {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestGeneratedExport_OmittedWithoutBareCollectionEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "testnoexport",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/testnoexport-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"stories": {Endpoints: map[string]spec.Endpoint{"get": {Method: "GET", Path: "/api/v1/stories/{id}"}}},
			"items":   {Endpoints: map[string]spec.Endpoint{"get": {Method: "GET", Path: "/api/v1/items/{id}"}}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "testnoexport-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(rootGo), "newExportCmd")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "export.go"))
	assert.True(t, errors.Is(err, os.ErrNotExist), "export.go should not be emitted for APIs without bare collection endpoints")

	runGoCommandRequired(t, outputDir, "build", "./cmd/testnoexport-pp-cli")
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
		for range 20 {
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
		for range 20 {
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

func TestGeneratedOutput_PromotedCommandKeepsSubresourceParents(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promsub",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PROM_SUB_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/promsub-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"account": {
				Description: "Manage accounts",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/account/{accountId}",
						Description: "Get account",
						Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
					},
				},
				SubResources: map[string]spec.Resource{
					"cards": {
						Description: "Manage cards",
						Endpoints: map[string]spec.Endpoint{
							"get-account": {
								Method:      "GET",
								Path:        "/account/{accountId}/cards",
								Description: "Get account cards",
								Alias:       "get",
								Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
							},
						},
					},
					"statements": {
						Description: "Manage statements",
						Endpoints: map[string]spec.Endpoint{
							"get-account": {
								Method:      "GET",
								Path:        "/account/{accountId}/statements",
								Description: "Get account statements",
								Alias:       "get",
								Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promsub-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	promotedSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_account.go"))
	require.NoError(t, err)
	assert.Contains(t, string(promotedSrc), "newAccountCardsCmd(flags)")
	assert.Contains(t, string(promotedSrc), "newAccountStatementsCmd(flags)")
	assert.NotContains(t, string(promotedSrc), "newAccountCardsGetAccountCmd(flags)")
	assert.NotContains(t, string(promotedSrc), "newAccountStatementsGetAccountCmd(flags)")

	cardsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "account_cards_get-account.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cardsSrc), `Example: "  promsub-pp-cli account cards get-account `)
	assert.NotContains(t, string(cardsSrc), `Example: "  promsub-pp-cli account get-account `)
}

func TestExampleLineUsesRenderedCommandAndFlagNames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("example-render")
	g := New(apiSpec, t.TempDir())

	endpoint := spec.Endpoint{
		Method: "POST",
		Path:   "/account/{accountId}/request-send-money",
		Params: []spec.Param{
			{Name: "accountId", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{
			{Name: "idempotencyKey", Type: "string", Required: true},
		},
	}

	got := g.exampleLine("account request-send-money", "request_send_money", endpoint)

	assert.Contains(t, got, "example-render-pp-cli account request-send-money request-send-money")
	assert.Contains(t, got, "--idempotency-key your-token-here")
	assert.NotContains(t, got, "request_send_money")
	assert.NotContains(t, got, "--idempotencyKey")
}

func TestDetectAgentMoneyWorkflowFromGenericMoneyMovementShape(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("treasury")
	apiSpec.Resources = map[string]spec.Resource{
		"account": {
			Description: "Manage accounts",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/account/{accountId}",
					Description: "Get account",
					Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
				},
			},
			SubResources: map[string]spec.Resource{
				"request-send-money": {
					Description: "Manage send-money requests",
					Endpoints: map[string]spec.Endpoint{
						"request_send_money": {
							Method:      "POST",
							Path:        "/account/{accountId}/request-send-money",
							Description: "Create send-money request",
							Alias:       "create",
							Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
							Body: []spec.Param{
								{Name: "amount", Type: "number", Required: true},
								{Name: "recipientId", Type: "string", Required: true},
								{Name: "paymentMethod", Type: "string", Required: true},
								{Name: "idempotencyKey", Type: "string", Required: true},
							},
						},
					},
				},
				"transactions": {
					Description: "Manage transactions",
					Endpoints: map[string]spec.Endpoint{
						"create": {
							Method:      "POST",
							Path:        "/account/{accountId}/transactions",
							Description: "Create transaction",
							Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
							Body: []spec.Param{
								{Name: "amount", Type: "number", Required: true},
								{Name: "recipientId", Type: "string", Required: true},
								{Name: "paymentMethod", Type: "string", Required: true},
								{Name: "idempotencyKey", Type: "string", Required: true},
								{Name: "externalMemo", Type: "string"},
								{Name: "purpose", Type: "object"},
							},
						},
					},
				},
			},
		},
		"transfer": {
			Description: "Move funds between accounts",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/transfer",
					Description: "Create transfer",
					Body: []spec.Param{
						{Name: "sourceAccountId", Type: "string", Required: true},
						{Name: "destinationAccountId", Type: "string", Required: true},
						{Name: "amount", Type: "number", Required: true},
						{Name: "idempotencyKey", Type: "string", Required: true},
					},
				},
			},
		},
	}

	workflow := detectAgentMoneyWorkflow(apiSpec, map[string]string{"transfer": "create"})

	require.NotNil(t, workflow.Payment)
	assert.Equal(t, []string{"account", "transactions", "create"}, workflow.Payment.CommandPath)
	assert.True(t, workflow.Payment.HasAccountIDPosition)
	assert.Equal(t, "amount", workflow.Payment.AmountFlag)
	assert.Equal(t, "recipient-id", workflow.Payment.RecipientIDFlag)
	assert.Equal(t, "payment-method", workflow.Payment.PaymentMethodFlag)
	assert.Equal(t, "idempotency-key", workflow.Payment.IdempotencyKeyFlag)
	assert.Equal(t, "external-memo", workflow.Payment.ExternalMemoFlag)
	assert.Equal(t, "purpose", workflow.Payment.PurposeFlag)

	require.NotNil(t, workflow.Request)
	assert.Equal(t, []string{"account", "request-send-money", "create"}, workflow.Request.CommandPath,
		"workflow plans should use generated endpoint aliases when available")

	require.NotNil(t, workflow.Transfer)
	assert.Equal(t, []string{"transfer"}, workflow.Transfer.CommandPath,
		"promoted single-endpoint resources must emit the actual registered command path")
	assert.Equal(t, "source-account-id", workflow.Transfer.SourceAccountIDFlag)
	assert.Equal(t, "destination-account-id", workflow.Transfer.DestinationAccountIDFlag)
	assert.True(t, workflow.Enabled())
}

func TestDetectAgentMoneyWorkflowRejectsIncompleteExecutablePlans(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("treasury")
	apiSpec.Resources = map[string]spec.Resource{
		"organizations": {
			Description: "Manage organization payments",
			Endpoints: map[string]spec.Endpoint{
				"create-payment": {
					Method:      "POST",
					Path:        "/organizations/{organizationId}/payments",
					Description: "Create payment",
					Params:      []spec.Param{{Name: "organizationId", Type: "string", Required: true, Positional: true}},
					Body: []spec.Param{
						{Name: "amount", Type: "number", Required: true},
						{Name: "recipientId", Type: "string", Required: true},
						{Name: "paymentMethod", Type: "string", Required: true},
						{Name: "idempotencyKey", Type: "string", Required: true},
					},
				},
			},
		},
		"payments": {
			Description: "Manage payments",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/payments",
					Description: "Create payment",
					Body: []spec.Param{
						{Name: "amount", Type: "number", Required: true},
						{Name: "recipientId", Type: "string", Required: true},
						{Name: "paymentMethod", Type: "string", Required: true},
						{Name: "idempotencyKey", Type: "string", Required: true},
						{Name: "currency", Type: "string", Required: true},
					},
				},
			},
		},
	}

	workflow := detectAgentMoneyWorkflow(apiSpec, nil)

	assert.False(t, workflow.Enabled(), "payment-plan must not emit execute commands that omit required positionals or body flags")
}

func TestDetectAgentMoneyWorkflowTracksTransferPositionalsAndIntegerAmounts(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("treasury")
	apiSpec.Resources = map[string]spec.Resource{
		"account": {
			Description: "Manage accounts",
			SubResources: map[string]spec.Resource{
				"transfers": {
					Description: "Move funds between account ledgers",
					Endpoints: map[string]spec.Endpoint{
						"create": {
							Method: "POST",
							Path:   "/account/{accountId}/transfers",
							Params: []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
							Body: []spec.Param{
								{Name: "sourceAccountId", Type: "string", Required: true},
								{Name: "destinationAccountId", Type: "string", Required: true},
								{Name: "amount", Type: "integer", Required: true},
								{Name: "idempotencyKey", Type: "string", Required: true},
							},
						},
					},
				},
			},
		},
	}

	workflow := detectAgentMoneyWorkflow(apiSpec, nil)

	require.NotNil(t, workflow.Transfer)
	assert.True(t, workflow.Transfer.HasAccountIDPosition)
	assert.True(t, workflow.Transfer.AmountInteger)
}

func TestGeneratedOutput_AgentMoneyWorkflowPaymentPlan(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("treasury")
	apiSpec.Resources["account"] = spec.Resource{
		Description: "Manage accounts",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/account/{accountId}",
				Description: "Get account",
				Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
			},
		},
		SubResources: map[string]spec.Resource{
			"transactions": {
				Description: "Manage transactions",
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:      "POST",
						Path:        "/account/{accountId}/transactions",
						Description: "Create transaction",
						Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
						Body: []spec.Param{
							{Name: "amount", Type: "number", Required: true},
							{Name: "recipientId", Type: "string", Required: true},
							{Name: "paymentMethod", Type: "string", Required: true},
							{Name: "idempotencyKey", Type: "string", Required: true},
							{Name: "purpose", Type: "object"},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "treasury-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	workflowGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
	require.NoError(t, err)
	workflowSrc := string(workflowGo)
	assert.Contains(t, workflowSrc, "func newWorkflowPaymentPlanCmd(flags *rootFlags) *cobra.Command")
	assert.Contains(t, workflowSrc, `Use:   "payment-plan"`)
	assert.Contains(t, workflowSrc, `base = []string{"treasury-pp-cli", "account", "transactions", "create"}`)
	assert.Contains(t, workflowSrc, `"dry_run_command": append(append([]string{}, base...), "--dry-run", "--agent")`)
	assert.Contains(t, workflowSrc, `"execute_command": append([]string{}, base...)`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	binaryPath := filepath.Join(outputDir, "treasury-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/treasury-pp-cli")

	cmd := exec.Command(binaryPath, "workflow", "payment-plan",
		"--kind", "payment",
		"--account-id", "acct_123",
		"--recipient-id", "rec_123",
		"--amount", "25",
		"--payment-method", "ach",
		"--idempotency-key", "idem",
		"--json",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	var plan map[string]any
	require.NoError(t, json.Unmarshal(out, &plan))
	assert.Equal(t, "payment", plan["kind"])
	assert.Equal(t, true, plan["read_only"])
	assert.Equal(t, true, plan["requires_approval"])
	assert.Equal(t, []any{"treasury-pp-cli", "account", "transactions", "create", "acct_123", "--amount", "25.00", "--recipient-id", "rec_123", "--payment-method", "ach", "--idempotency-key", "idem", "--dry-run", "--agent"}, plan["dry_run_command"])
	assert.Equal(t, []any{"treasury-pp-cli", "account", "transactions", "create", "acct_123", "--amount", "25.00", "--recipient-id", "rec_123", "--payment-method", "ach", "--idempotency-key", "idem"}, plan["execute_command"])

	cmd = exec.Command(binaryPath, "workflow", "payment-plan",
		"--kind", "payment",
		"--account-id", "acct_123",
		"--recipient-id", "rec_123",
		"--amount", "25",
		"--payment-method", "domesticWire",
		"--idempotency-key", "idem",
	)
	out, err = cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "--purpose is required for domesticWire payment plans")
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
	assert.Contains(t, content, "cliutil.LooksLikeAuthError")
	// Env var should appear in error hints
	assert.Contains(t, content, "STEAM_API_KEY")
	// Key URL should appear in error hints
	assert.Contains(t, content, "https://steamcommunity.com/dev/apikey")
	// Doctor command hint
	assert.Contains(t, content, "steamauth-pp-cli doctor")
	// Sanitization should route through shared cliutil helper
	assert.Contains(t, content, "cliutil.SanitizeErrorBody")
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
	assert.Contains(t, content, "cliutil.LooksLikeAuthError")
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
	assert.NotContains(t, content, "LooksLikeAuthError")
	assert.NotContains(t, content, "SanitizeErrorBody")
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

	// Probe should target baseURL + verify_path via the configured client,
	// not bare baseURL. The doctor uses flags.newClient() now (Surf-aware)
	// instead of stdlib http.Client.
	assert.Contains(t, content, `verifyPath := "/me?fields=id"`)
	assert.Contains(t, content, `c.GetWithHeaders(verifyPath`)
	assert.NotContains(t, content, `&http.Client{`)
	// When verify_path is set, 401/403 keeps the strict "invalid" verdict
	assert.Contains(t, content, `"invalid (HTTP %d) — check your credentials"`)
	// And does NOT emit the inconclusive fallback wording
	assert.NotContains(t, content, "inconclusive (HTTP %d from base URL")
}

func TestGeneratedDoctor_HealthCheckPathProbesEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("healthdoc")
	apiSpec.HealthCheckPath = "api/marketStatus"

	outputDir := filepath.Join(t.TempDir(), "healthdoc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	assert.Contains(t, doctorSrc, `healthPath := "api/marketStatus"`)
	assert.Contains(t, doctorSrc, `if !strings.HasPrefix(healthPath, "/") {`)
	assert.Contains(t, doctorSrc, `reachBody, reachErr := c.Get(healthPath, nil)`)
	assert.NotContains(t, doctorSrc, `reachBody, reachErr := c.Get("/", nil)`)
}

func TestGeneratedDoctor_InterstitialMarkersAreTitleAnchored(t *testing.T) {
	t.Parallel()

	// looksLikeDoctorInterstitial must anchor loose Cloudflare markers to the
	// <title> tag so a real recipe titled "Just A Moment of Pause Cookies"
	// (or similar benign content) is not mistakenly classified as a Cloudflare
	// challenge page. This guards against a regression where the marker list
	// was checked against the body's lowercased prefix without title context.
	apiSpec := &spec.APISpec{
		Name:    "interstitialdoc",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/interstitialdoc-pp-cli/config.toml",
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

	outputDir := filepath.Join(t.TempDir(), "interstitialdoc-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	// Cloudflare's loose "just a moment" marker must be anchored to <title>.
	assert.Contains(t, content, `"<title>just a moment"`,
		"Cloudflare 'just a moment' marker should be anchored to <title> to avoid false positives on benign content")
	// And the bare unanchored variant must NOT appear by itself in the
	// switch-case (the tightened version still contains "just a moment" as
	// a substring of the anchored marker, so we check for the anchored form's
	// presence rather than the bare form's absence).
	// The <title-anchored gate at the top of the function is also required.
	assert.Contains(t, content, `if !strings.Contains(prefix, "<title")`,
		"interstitial detector should bail early when no <title> tag is present")
	// DataDome marker must require an additional context word, not just the
	// vendor name (which appears in legitimate analytics scripts on many
	// sites that don't use DataDome for blocking).
	assert.Contains(t, content, `strings.Contains(prefix, "datadome") && (strings.Contains(prefix, "blocked") || strings.Contains(prefix, "captcha") || strings.Contains(prefix, "challenge"))`,
		"DataDome marker should require a context word (blocked/captcha/challenge) alongside the vendor name")
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

	// Without spec verify_path, the doctor probes "/" via the configured
	// client. The client is the same Surf-aware *client.Client used by
	// regular commands -- not a fresh stdlib http.Client.
	assert.Contains(t, content, `verifyPath := "/"`)
	assert.Contains(t, content, `c.GetWithHeaders(verifyPath`)
	assert.NotContains(t, content, `&http.Client{`)
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
	// auth.go is not emitted for auth.type:none specs (see Generator.renderAuthFiles).
	// Stronger assertion than the previous "no newAuthRefreshCmd in auth.go": there's
	// no auth.go at all for no-auth CLIs.
	_, err = os.Stat(filepath.Join(browserDir, "internal", "cli", "auth.go"))
	assert.True(t, os.IsNotExist(err), "auth.go should not be emitted for auth.type:none specs")
}

func TestGenerateRequiredUserAgentHeaderBeatsDefaultUserAgent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("browserheaders")
	apiSpec.RequiredHeaders = []spec.RequiredHeader{
		{Name: "User-Agent", Value: "Mozilla/5.0 Browser Sniff"},
		{Name: "Referer", Value: "https://www.example.com/"},
	}

	outputDir := filepath.Join(t.TempDir(), "browserheaders-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `req.Header.Set("User-Agent", "Mozilla/5.0 Browser Sniff")`)
	require.Contains(t, clientSrc, `req.Header.Set("Referer", "https://www.example.com/")`)
	assert.Contains(t, clientSrc, `if req.Header.Get("User-Agent") == "" {`)
	assert.Contains(t, clientSrc, `req.Header.Set("User-Agent", "browserheaders-pp-cli/0.1.0")`)

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, `authHeaders["User-Agent"] = "Mozilla/5.0 Browser Sniff"`)
	assert.NotContains(t, doctorSrc, `authHeaders["User-Agent"] = "browserheaders-pp-cli"`)
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
	// Browser/H3 transport has focused coverage above; this test is about
	// GraphQL BFF command shape, so keep its generated compile path lean.
	apiSpec.HTTPTransport = spec.HTTPTransportStandard
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/example-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "example-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootSrc := string(rootGo)
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newProductsCmd(flags))")
	assert.NotContains(t, rootSrc, "rootCmd.AddCommand(newGraphqlCmd(flags))")
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
	assert.Contains(t, whichSrc, `"pp:typed-exit-codes": "0,2"`)

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

		// No command should call MarkFlagRequired (except import.go which is not verify-tested).
		// Match call sites only (`.MarkFlagRequired(` with a quote arg) so that
		// docstrings or comments mentioning the symbol don't false-positive.
		if e.Name() != "import.go" && strings.Contains(content, `.MarkFlagRequired("`) {
			t.Errorf("%s still calls MarkFlagRequired", e.Name())
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
			got := naming.MCPDescription(tt.desc, tt.noAuth, tt.authType, tt.publicCount, tt.totalCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateMCPContextEscapesDomainStrings(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Description = `Stytch "quoted" API \ context`
	apiSpec.CLIDescription = `Manage "quoted" Stytch sessions from C:\tmp.`
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
	assert.Contains(t, src, `Manage \"quoted\" Stytch sessions from C:\\tmp.`)
	assert.Contains(t, src, `label=\"quoted\"&path=\\demo`)
	assert.Contains(t, src, `Quote \"dashboard\"`)
	assert.Contains(t, src, `filter=\"active\"`)
}

func TestGenerateMCPCompactsRepeatedParamDescriptions(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-dedupe")
	sharedDescription := "Select additional nested resource fields to include in the response. Use comma-separated field names such as owner, permissions, metadata, relationships, and auditTrail; unsupported values are ignored by the upstream API."
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
				},
				"search": {
					Method:      "GET",
					Path:        "/items/search",
					Description: "Search items",
					Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
				},
				"recent": {
					Method:      "GET",
					Path:        "/items/recent",
					Description: "List recent items",
					Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	toolsBody := string(toolsData)

	assert.NotContains(t, toolsBody, sharedDescription,
		"generated MCP runtime schema should not repeat long shared parameter descriptions verbatim")
	assert.Equal(t, 3, strings.Count(toolsBody, `mcplib.Description("Select additional nested resource fields to include in the response.")`),
		"shared param descriptions should stay understandable after compaction")
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

func TestGenerateDependentSyncReservedWordCompiles(t *testing.T) {
	t.Parallel()

	// A spec whose dependent-resource table name is a SQL reserved word
	// (e.g. references, trigger, view, order) must still produce a CLI
	// that builds and whose store tests pass. The store template's
	// backfillColumns slice and the upgrade-path test's t.Fatalf format
	// string both interpolate the table name into Go double-quoted
	// strings; safeSQLName quote-wraps reserved words for SQL contexts,
	// so applying it in a Go-string context would emit invalid Go for
	// any reserved-word resource. Regression for issue #272 follow-up.
	apiSpec := &spec.APISpec{
		Name:    "docstore",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"DOCSTORE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/docstore-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"documents": {
				Description: "Manage documents",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/documents",
						Description: "List documents",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
			// "references" snake_cases to "references" — a SQL reserved
			// word per internal/generator/schema_builder.go:322.
			"references": {
				Description: "Manage references attached to a document",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/documents/{documentId}/references",
						Description: "List references for a document",
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

	// The generated store.go must compile (no embedded "" from safeName
	// quote-wrapping) and the per-table upgrade test must run cleanly.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestGeneratedSyncTreatsAccessDeniedAsWarning(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "metasync",
		Version: "0.1.0",
		BaseURL: "https://graph.example.com",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			EnvVars: []string{"METASYNC_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/metasync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Manage accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/me/adaccounts",
						Description: "List accounts",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
			"targeting": {
				Description: "Manage targeting",
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method:      "GET",
						Path:        "/search",
						Description: "Search targeting",
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

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// Sync emits the structured warn event and routes to the warn-aware exit branch.
	assert.Contains(t, syncContent, `Warn     error`)
	assert.Contains(t, syncContent, `{"event":"sync_warning"`)
	assert.Contains(t, syncContent, `"status":%d,"reason":"%s"`)
	assert.Contains(t, syncContent, `{"event":"sync_summary"`)
	assert.Contains(t, syncContent, `Sync complete: %d records across %d resources (%d warned, %.1fs)`)
	assert.Contains(t, syncContent, `successCount == 0`)
	assert.Contains(t, syncContent, `skipped due to insufficient access`)
	assert.Contains(t, syncContent, `return nil`)
	// The classifier moved to helpers.go; sync.go must call into it, not redefine it.
	assert.Contains(t, syncContent, `isSyncAccessWarning(err)`)
	assert.NotContains(t, syncContent, `func isSyncAccessWarning`)
	assert.NotContains(t, syncContent, `func looksLikeSyncAccessDenial`)
	// AGENTS.md: do not hardcode one API into reusable machine artifacts. The
	// pre-fix patch leaked Meta-specific brand names into every printed CLI;
	// guard against regression.
	assert.NotContains(t, syncContent, `"workplace"`)

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersContent := string(helpersGo)
	assert.Contains(t, helpersContent, `func isSyncAccessWarning(err error) (*accessWarning, bool)`)
	assert.Contains(t, helpersContent, `func looksLikeAccessDenial(body string) bool`)
	assert.Contains(t, helpersContent, `*client.APIError`)
	assert.Contains(t, helpersContent, `errors.As`)
	assert.NotContains(t, helpersContent, `"workplace"`)

	// Build to catch template-syntax / import errors that substring assertions miss.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	// Inject a behavioral test that exercises isSyncAccessWarning against real
	// *client.APIError values plus the false-positive vectors flagged in code
	// review (path containing "auth", body "validation failed: token required",
	// "insufficient_funds", HTTP 500). This catches regressions where the helper
	// gets stubbed to "return nil, true" or its negative cases stop holding.
	behaviorTest := `package cli

import (
	"errors"
	"fmt"
	"testing"

	"metasync-pp-cli/internal/client"
)

func TestIsSyncAccessWarningClassification(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantOK     bool
		wantStatus int
		wantReason string
	}{
		{"nil error", nil, false, 0, ""},
		{"403 forbidden", &client.APIError{Method: "GET", Path: "/me/ads", StatusCode: 403, Body: "forbidden: missing scope"}, true, 403, "forbidden"},
		{"400 with insufficient scope", &client.APIError{Method: "GET", Path: "/search", StatusCode: 400, Body: "(#27) insufficient scope to call this method"}, true, 400, "insufficient_access"},
		{"400 with permission denied", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 400, Body: "permission denied for resource"}, true, 400, "insufficient_access"},
		{"400 with unauthorized", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 400, Body: "unauthorized"}, true, 400, "insufficient_access"},
		// Negative cases — these MUST NOT classify as access warnings.
		{"401 token expired (whole-CLI re-auth, not per-resource ACL)", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 401, Body: "token expired"}, false, 0, ""},
		{"500 server error", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 500, Body: "internal error"}, false, 0, ""},
		{"400 validation: missing token field", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 400, Body: "validation failed: token field is required"}, false, 0, ""},
		{"400 billing: insufficient_funds", &client.APIError{Method: "POST", Path: "/charges", StatusCode: 400, Body: "{\"error\":\"insufficient_funds\"}"}, false, 0, ""},
		{"400 with pagination_token in body", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 400, Body: "invalid pagination_token: malformed cursor"}, false, 0, ""},
		{"400 path /authors with no body keyword", &client.APIError{Method: "GET", Path: "/authors/123", StatusCode: 400, Body: "id not found"}, false, 0, ""},
		{"plain Go error", errors.New("connection refused"), false, 0, ""},
		{"wrapped 403 still detected via errors.As", fmt.Errorf("fetching foo: %w", &client.APIError{Method: "GET", Path: "/foo", StatusCode: 403, Body: "forbidden"}), true, 403, "forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := isSyncAccessWarning(tc.err)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (err=%v)", ok, tc.wantOK, tc.err)
			}
			if !ok {
				return
			}
			if w.Status != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Status, tc.wantStatus)
			}
			if w.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", w.Reason, tc.wantReason)
			}
		})
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "sync_classify_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestIsSyncAccessWarningClassification")
}

// TestGeneratedSyncMaxPagesAndStickyCursor verifies that the generated
// sync command (a) defaults --max-pages to 100 (covers <=10k items/resource
// at default page size; bigger resources opt in explicitly), (b) emits a
// structured sync_warning with reason "max_pages_cap_hit" on BOTH the flat
// and dependent-resource code paths when the cap is reached, and (c) breaks
// the pagination loop with a "stuck_pagination" sync_warning when the API
// echoes a non-advancing next cursor across consecutive pages. The literal
// "%s" interpolation pattern matches the sync_anomaly emission elsewhere in
// sync.go.tmpl rather than %q Go-escaping; this test pins both the JSON
// event shapes and the flag default at the template-emission layer so
// future template churn cannot silently regress them.
func TestGeneratedSyncMaxPagesAndStickyCursor(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "pagedsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"PAGEDSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/pagedsync-pp-cli/config.toml",
		},
		// Parent + child resources so the generated sync.go includes
		// both syncResource (flat path) and syncDependentResource paths,
		// each of which must emit the new max_pages_cap_hit warning and
		// the stuck_pagination detector.
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
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// (a) Default --max-pages is 100 (covers <=10k items per resource at
	// the default page size of 100; the old 10-page default silently
	// truncated reference resources at 1000 items).
	assert.Contains(t, syncContent, `cmd.Flags().IntVar(&maxPages, "max-pages", 100,`,
		"sync.go must declare --max-pages with default 100")
	assert.NotContains(t, syncContent, `cmd.Flags().IntVar(&maxPages, "max-pages", 10,`,
		"sync.go must not retain the old 10-page default")

	// (b1) Flat-path cap-hit emits structured sync_warning with reason
	// "max_pages_cap_hit". Use the literal %s embedded-quote shape — match
	// the emission in sync.go.tmpl line 374.
	assert.Contains(t, syncContent,
		`{"event":"sync_warning","resource":"%s","reason":"max_pages_cap_hit"`,
		"flat-path cap-hit must emit a structured sync_warning with reason max_pages_cap_hit")

	// (b2) Dependent-path cap-hit emits the same shape (with parent attached).
	// Pre-WU-2 the dependent-resource cap-hit branch was silent — verify
	// the new emission lands.
	assert.Contains(t, syncContent,
		`{"event":"sync_warning","resource":"%s","parent":"%s","reason":"max_pages_cap_hit"`,
		"dependent-resource cap-hit must emit a structured sync_warning with reason max_pages_cap_hit")

	// (c) Sticky-cursor detection on the flat path. The check must compare
	// against a tracked lastNextCursor and emit the structured warning when
	// the cursor doesn't advance.
	assert.Contains(t, syncContent, "lastNextCursor",
		"sync.go must track lastNextCursor for sticky-cursor detection")
	assert.Contains(t, syncContent, "nextCursor == lastNextCursor",
		"sync.go must compare nextCursor against lastNextCursor to detect stuck pagination")
	assert.Contains(t, syncContent,
		`{"event":"sync_warning","resource":"%s","reason":"stuck_pagination"`,
		"flat-path stuck-pagination must emit a structured sync_warning")

	// (c2) Dependent-path sticky-cursor emission. Includes parent context.
	assert.Contains(t, syncContent,
		`{"event":"sync_warning","resource":"%s","parent":"%s","reason":"stuck_pagination"`,
		"dependent-resource stuck-pagination must emit a structured sync_warning")

	// AGENTS.md: emission must use the "%s" embedded-quote pattern, not
	// %q. A %q usage here would be a real bug — JSON shapes for resource
	// names containing quotes/backslashes would diverge across emission
	// sites. The plan (docs/plans/2026-04-30-001) calls this out explicitly.
	assert.NotContains(t, syncContent,
		`"reason":%q,"message"`,
		"sync_warning must use literal %s interpolation, not %q Go-escaping")

	// Build the generated CLI to catch template-syntax / import errors that
	// substring assertions miss.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGeneratedSyncExitPolicy pins the generated sync command's exit-code
// contract: (a) a --strict flag for callers that want any per-resource
// failure to exit non-zero, (b) a `criticalResources` map literal at the
// top of newSyncCmd projecting SyncableResource.Critical (set from
// x-critical), (c) a four-branch exit-policy that downgrades non-critical
// failures to warnings unless --strict is set, and (d) a one-shot in-band
// sync_warning with reason "exit_policy_default_changed" emitted the first
// time the policy suppresses a non-critical failure, so external callers
// can detect partial-failure tolerance. The literal "%s" embedded-quote
// pattern matches the shape used elsewhere in sync.go.tmpl.
func TestGeneratedSyncExitPolicy(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "exitsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"EXITSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/exitsync-pp-cli/config.toml",
		},
		// Two flat resources: one annotated x-critical: true (channels) and
		// one unannotated (messages). The generator should emit channels in
		// the criticalResources map and omit messages.
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
						Critical:    true,
					},
				},
			},
			"messages": {
				Description: "Manage messages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/messages",
						Description: "List messages",
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

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// (a) --strict flag declared with a description that mentions the
	// per-resource-failure behavior so callers understand the trade-off
	// against the default critical-only exit policy.
	assert.Contains(t, syncContent,
		`cmd.Flags().BoolVar(&strict, "strict", false,`,
		"sync.go must declare --strict flag")
	assert.Contains(t, syncContent,
		"any per-resource failure",
		"--strict flag description should describe the per-resource-failure behavior")

	// (b) criticalResources map literal lists exactly the critical resources.
	assert.Contains(t, syncContent,
		`var criticalResources = map[string]bool{`,
		"sync.go must declare criticalResources as a package-level var")
	assert.Contains(t, syncContent,
		`"channels": true,`,
		"criticalResources must include channels (x-critical: true)")
	// messages is non-critical and must NOT appear with `: true,` in the map.
	// We can't easily assert absence-from-map without a substring scan; ensure
	// the unrelated literal isn't there.
	assert.NotContains(t, syncContent,
		`"messages": true,`,
		"criticalResources must NOT include messages (no x-critical)")

	// (c) Four-branch exit policy. The four guards land in sequence; assert
	// each individually so a refactor can't silently collapse two of them.
	assert.Contains(t, syncContent,
		`if strict && errCount > 0 {`,
		"strict-mode branch must exit non-zero on any error")
	assert.Contains(t, syncContent,
		`if criticalErrCount > 0 {`,
		"critical-failure branch must exit non-zero regardless of --strict")
	assert.Contains(t, syncContent,
		`if successCount == 0 {`,
		"nothing-synced branch must exit non-zero")

	// criticalErrCount must be tallied at result-aggregation time using the
	// criticalResources map. This pins the lookup mechanism so a refactor
	// can't accidentally drop the per-resource classification.
	assert.Contains(t, syncContent,
		`if criticalResources[res.Resource] {`,
		"sync.go must classify each errored resource against criticalResources")

	// (d) In-band default-flip signal. Fires once when the new default
	// suppressed a non-zero exit under the old contract.
	assert.Contains(t, syncContent,
		`"reason":"exit_policy_default_changed"`,
		"sync.go must emit one-shot sync_warning with reason exit_policy_default_changed")
	assert.Contains(t, syncContent,
		`if errCount > 0 && !strict && criticalErrCount == 0 && successCount > 0 {`,
		"default-flip signal must fire only when the new default suppressed a non-zero exit")

	// AGENTS.md: emission must use the "%s" embedded-quote pattern, not
	// %q. Match the sync_anomaly shape on line 374 of sync.go.tmpl.
	assert.NotContains(t, syncContent,
		`"reason":%q,"errored"`,
		"exit_policy_default_changed must use literal %s interpolation, not %q Go-escaping")

	// Build the generated CLI to catch template-syntax / import errors that
	// substring assertions miss.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGeneratedSyncIDFieldOverridesAndProbes pins the WU-2 U3 contract: the
// generated sync command and store layer (a) emit a resourceIDFieldOverrides
// map literal projecting SyncableResource.IDField (set by U1 from
// x-resource-id or response-schema fallback), (b) drop kalshi-specific names
// (ticker/event_ticker/series_ticker) from the runtime fallback list — no
// other public-library CLIs depend on them and the user owns kalshi, (c)
// emit per-item primary_key_unresolved sync_anomaly events the first time
// silent drops occur (rate-limited via anomalyEmitted), and (d) emit the
// F4b stored_count_zero_after_extraction probe at end-of-resource for the
// case where extraction succeeded but rows didn't land. The AGENTS.md
// "%s" embedded-quote pattern applies — same JSON-shape consistency as U2/U4.
func TestGeneratedSyncIDFieldOverridesAndProbes(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "idfieldsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"IDFIELDSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/idfieldsync-pp-cli/config.toml",
		},
		// One resource with an explicit IDField (templated path), one without
		// (runtime fallback). Both have a list endpoint so syncResource is
		// emitted; messages additionally exercises syncDependentResource via
		// path-templated parent ID.
		Resources: map[string]spec.Resource{
			"events": {
				Description: "Manage events",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/events",
						Description: "List events",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						IDField:     "event_id", // Templated override path
					},
				},
			},
			"channels": {
				Description: "Manage channels",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/channels",
						Description: "List channels",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						// No IDField — exercises the runtime fallback path.
					},
				},
			},
			"messages": {
				Description: "Manage messages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/channels/{channelId}/messages",
						Description: "List messages",
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

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeContent := string(storeGo)

	// (a) resourceIDFieldOverrides map declared in BOTH sync.go and store.go.
	// The map projects SyncableResource.IDField — events with IDField=event_id
	// must appear, channels (no IDField) must NOT.
	assert.Contains(t, syncContent,
		`var resourceIDFieldOverrides = map[string]string{`,
		"sync.go must declare resourceIDFieldOverrides map")
	assert.Contains(t, syncContent,
		`"events": "event_id",`,
		"sync.go resourceIDFieldOverrides must include events: event_id (from IDField)")
	// channels has no IDField — assert it doesn't appear inside the override
	// map. We can't use NotContains on `"channels":` blanket because the
	// resource also appears in syncResourcePath ("channels": "/channels"); pin
	// the absence by extracting the override-map block and asserting.
	overrideStart := strings.Index(syncContent, `var resourceIDFieldOverrides = map[string]string{`)
	require.GreaterOrEqual(t, overrideStart, 0)
	overrideEnd := strings.Index(syncContent[overrideStart:], "}")
	require.Greater(t, overrideEnd, 0)
	overrideBlock := syncContent[overrideStart : overrideStart+overrideEnd]
	assert.NotContains(t, overrideBlock, `"channels"`,
		"resourceIDFieldOverrides block must NOT include channels (no IDField)")
	assert.NotContains(t, overrideBlock, `"messages"`,
		"resourceIDFieldOverrides block must NOT include messages (no IDField)")

	assert.Contains(t, storeContent,
		`var resourceIDFieldOverrides = map[string]string{`,
		"store.go must declare resourceIDFieldOverrides map")
	assert.Contains(t, storeContent,
		`"events": "event_id",`,
		"store.go resourceIDFieldOverrides must include events: event_id")

	// (b) Generic fallback list reduced — kalshi-specific names dropped.
	// The user owns the kalshi CLI and will regenerate with x-resource-id
	// annotations; no other public-library CLIs depend on these names.
	assert.Contains(t, storeContent,
		`var genericIDFieldFallbacks = []string{"id", "ID", "name", "uuid", "slug", "key", "code", "uid"}`,
		"store.go genericIDFieldFallbacks must be the reduced WU-2 U3 list")
	// Negative: kalshi-specific names must not be in the fallback list.
	// We assert a robust shape: no occurrence of "ticker" inside the fallback
	// declaration. The generic check below also pins the absence at a
	// site-independent layer.
	assert.NotContains(t, storeContent, `"ticker"`,
		"store.go must not contain ticker as a fallback ID name (WU-2 U3 dropped it)")
	assert.NotContains(t, storeContent, `"event_ticker"`,
		"store.go must not contain event_ticker as a fallback ID name (WU-2 U3 dropped it)")
	assert.NotContains(t, storeContent, `"series_ticker"`,
		"store.go must not contain series_ticker as a fallback ID name (WU-2 U3 dropped it)")

	// (c) Per-item primary_key_unresolved sync_anomaly emission. Fires
	// when extractFailures > 0 but at least one item landed; rate-limited
	// to one event per resource per sync run via anomalyEmitted.
	assert.Contains(t, syncContent,
		`"reason":"primary_key_unresolved"`,
		"sync.go must emit primary_key_unresolved sync_anomaly")
	assert.Contains(t, syncContent,
		"anomalyEmitted",
		"sync.go must rate-limit primary_key_unresolved emission via anomalyEmitted flag")
	// Pin the literal "%s" interpolation pattern (AGENTS.md).
	assert.Contains(t, syncContent,
		`{"event":"sync_anomaly","resource":"%s","consumed":%d,"stored":%d,"count":%d,"reason":"primary_key_unresolved"}`,
		"primary_key_unresolved must use the literal %s interpolation pattern")

	// Existing roll-up sync_anomaly preserved — fires when 100% of items
	// fail extraction (entire page yields stored=0).
	assert.Contains(t, syncContent,
		`"reason":"all_items_failed_id_extraction"`,
		"sync.go must preserve the all_items_failed_id_extraction roll-up event")

	// (d) F4b symptom probe at end-of-resource. Fires when consumed > 0
	// AND totalCount (stored) == 0 AND extraction succeeded for at least
	// one item — the symptom that's currently held out for controlled
	// repro. Probe must land in BOTH syncResource and syncDependentResource.
	assert.Contains(t, syncContent,
		`"reason":"stored_count_zero_after_extraction"`,
		"sync.go must emit stored_count_zero_after_extraction sync_anomaly (F4b probe)")
	// Pin the F4b emission shape (literal "%s" embedded quotes).
	assert.Contains(t, syncContent,
		`{"event":"sync_anomaly","resource":"%s","consumed":%d,"stored":0,"extract_failures":%d,"reason":"stored_count_zero_after_extraction"}`,
		"F4b probe must use the literal %s interpolation pattern")

	// UpsertBatch's signature is (int, int, error) — sync.go must consume
	// all three return values. Without this contract, extractFailures would
	// not be observable from sync's per-item warning code.
	assert.Contains(t, syncContent,
		`stored, extractFailures, err := upsertResourceBatch(db, resource, items)`,
		"sync.go syncResource must consume the three-tuple batch upsert return")
	assert.Contains(t, syncContent,
		`stored, extractFailures, err := upsertResourceBatch(db, dep.Name, items)`,
		"sync.go syncDependentResource must consume the three-tuple batch upsert return")

	// store.go's UpsertBatch declaration matches the new signature.
	assert.Contains(t, storeContent,
		`func (s *Store) UpsertBatch(resourceType string, items []json.RawMessage) (int, int, error) {`,
		"store.go UpsertBatch must return (stored, extractFailures, err)")

	// AGENTS.md guard: literal "%s" interpolation, not %q Go-escaping.
	assert.NotContains(t, syncContent,
		`"reason":%q,"count"`,
		"primary_key_unresolved must not use %q Go-escaping")

	// Build the generated CLI to catch template-syntax / import errors that
	// substring assertions miss. Also run the generated tests so the new
	// per-resource override and fallback-list tests execute against real code.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/store/...", "-run", "TestUpsertBatch_TemplatedIDFieldOverrideWins|TestUpsertBatch_GenericFallbackList|TestUpsertBatch_ExtractFailuresReturnedForPerItemMisses")
}

func TestGenerateOperationRoutingPathParamDefault(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "routing",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/routing-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"graphql": {
				Description: "GraphQL endpoints",
				SubResources: map[string]spec.Resource{
					"followers": {
						Description: "Followers",
						Endpoints: map[string]spec.Endpoint{
							"get": {
								Method:      "GET",
								Path:        "/graphql/{pathQueryId}/Followers",
								Description: "Get followers",
								Params: []spec.Param{
									{Name: "pathQueryId", Type: "string", PathParam: true, Default: "followers123", Description: "Path query id"},
									{Name: "variables", Type: "string", Required: true},
								},
								Response: spec.ResponseDef{Type: "array"},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "graphql_followers_get.go"))
	require.NoError(t, err)
	cliContent := string(cliGo)
	assert.Regexp(t,
		regexp.MustCompile(`cmd\.Flags\(\)\.StringVar\(&flagPathQueryId,\s*"path-query-id",\s*"followers123",\s*"Path query id"\)`),
		cliContent,
		"defaulted operation-routing path param should be user-overridable")
	assert.Contains(t, cliContent,
		`path = replacePathParam(path, "pathQueryId", fmt.Sprintf("%v", flagPathQueryId))`,
		"generated command must substitute the path template before calling the API")

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.Contains(t, string(helpersGo), "func replacePathParam(path, name, value string) string",
		"helpers.go must emit replacePathParam for flag-shaped path params")

	mcpGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	assert.Regexp(t,
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/graphql/\{pathQueryId\}/Followers",\s*\[]mcpParamBinding\{.*WireName: "pathQueryId".*\},\s*\[]string\{[^}]*"pathQueryId"`),
		string(mcpGo),
		"MCP handler must receive the routing path param so it can substitute the URL")
}

func TestGenerateSyncRejectsUnknownResourcePath(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "syncpaths",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/syncpaths-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Widgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/widgets",
						Description: "List widgets",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent,
		`func syncResourcePath(resource string) (string, error)`,
		"syncResourcePath should report unsupported resource names explicitly")
	assert.Contains(t, syncContent,
		`return "", fmt.Errorf("unknown sync resource %q", resource)`,
		"sync must not invent REST-style paths for unknown or GraphQL-only resources")
	assert.NotContains(t, syncContent,
		`return "/" + resource`,
		"sync must not request fake /<resource> paths")
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

func TestGraphQLFieldSelectionSupportsNestedSelections(t *testing.T) {
	t.Parallel()

	got := graphqlFieldSelection("Order", map[string]spec.TypeDef{
		"Order": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "totalPriceSet", Type: "object", Selection: "{ shopMoney { amount currencyCode } }"},
				{Name: "customer", Type: "object", Selection: "{ id email }"},
			},
		},
	})

	assert.Equal(t, []string{
		"id",
		"totalPriceSet { shopMoney { amount currencyCode } }",
		"customer { id email }",
	}, got)
}

// TestGenerateGraphQLEndpointPathRendersTemplatedURL guards PR-1's contract:
// when a GraphQL spec sets BaseURL to a templated host and GraphQLEndpointPath
// to a templated path (the Shopify shape), the generated client must build
// the request URL by concatenating BaseURL + GraphQLEndpointPath without
// stripping or normalizing the {var} placeholders. PR-2 will substitute the
// placeholders against env vars at request time; this test simulates that
// substitution to prove the URL the runtime sees would resolve correctly.
//
// The test deliberately builds the spec by hand (instead of going through
// graphql.ParseSDL) because no real-world API in the parser's
// knownGraphQLDefaults table uses a templated host today — Shopify is the
// motivating case but ships outside this repo. Constructing the spec inline
// keeps the test focused on the template plumbing.
func TestGenerateGraphQLEndpointPathRendersTemplatedURL(t *testing.T) {
	t.Parallel()

	const (
		baseURL          = "https://{shop}"
		endpointPath     = "/admin/api/{version}/graphql.json"
		shopValue        = "foo.myshopify.com"
		versionValue     = "2026-04"
		expectedFinalURL = "https://foo.myshopify.com/admin/api/2026-04/graphql.json"
	)

	apiSpec := &spec.APISpec{
		Name:                 "shopify",
		Description:          "Shopify Admin GraphQL (test fixture)",
		Version:              "2026-04",
		BaseURL:              baseURL,
		GraphQLEndpointPath:  endpointPath,
		EndpointTemplateVars: []string{"shop", "version"},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// isGraphQLSpec gates emission of graphql.go on every list endpoint
			// having Path == "/graphql". The path field is a sentinel for
			// "this resource flows through the GraphQL transport"; it is
			// distinct from GraphQLEndpointPath, which is the URL path the
			// client posts to at runtime.
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List orders",
						ResponsePath: "data.orders.nodes",
						Pagination: &spec.Pagination{
							Type:           "cursor",
							LimitParam:     "first",
							CursorParam:    "after",
							NextCursorPath: "data.orders.pageInfo.endCursor",
							HasMoreField:   "data.orders.pageInfo.hasNextPage",
						},
						Response: spec.ResponseDef{Type: "array", Item: "Order"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Order": {Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	graphqlGoPath := filepath.Join(outputDir, "internal", "client", "graphql.go")
	graphqlGoBytes, err := os.ReadFile(graphqlGoPath)
	require.NoError(t, err, "graphql.go should be generated for a GraphQL spec")
	graphqlGo := string(graphqlGoBytes)

	// The endpoint path constant must render the templated value verbatim.
	// Anything that strips or pre-resolves the {var} placeholders here would
	// break PR-2's runtime substitution and quietly send requests to the
	// wrong host once a real shop is configured.
	assert.Contains(t, graphqlGo, `const graphqlEndpointPath = "`+endpointPath+`"`,
		"graphql.go must render GraphQLEndpointPath into a const")
	assert.NotContains(t, graphqlGo, `c.Post("/graphql"`,
		"graphql.go must not retain the hardcoded /graphql path after PR-1")
	assert.Contains(t, graphqlGo, "c.Post(graphqlEndpointPath",
		"Query/Mutate must post against the rendered constant, not a literal")

	// The config struct must carry the templated BaseURL through unchanged so
	// PR-2 can resolve {shop} against SHOPIFY_SHOP at Load() time without
	// re-deriving the URL shape.
	configGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	assert.Contains(t, string(configGoBytes), `BaseURL: "`+baseURL+`"`,
		"config.go must carry the templated BaseURL into Load()'s defaults")

	// Simulate the runtime URL build: client.do() concatenates BaseURL+path.
	// PR-1 must produce a URL that — once env vars are substituted — points
	// at the real Shopify endpoint. The substitution itself ships in PR-2;
	// here we mimic it inline to prove the structure is right.
	rawURL := baseURL + endpointPath
	resolved := strings.NewReplacer(
		"{shop}", shopValue,
		"{version}", versionValue,
	).Replace(rawURL)
	assert.Equal(t, expectedFinalURL, resolved,
		"BaseURL + GraphQLEndpointPath must resolve to the Shopify Admin endpoint after env substitution")
}

// TestGenerateEndpointTemplateVarsRuntimeSubstitution covers PR-2's contract:
// when a spec declares EndpointTemplateVars, the generated CLI gets a buildURL
// helper that resolves {placeholder} markers against env-backed
// Config.TemplateVars at request time. The test compiles the generated tree
// and runs an injected behavioral test that exercises every required path —
// successful substitution, missing env var (actionable error), and the
// passthrough case where vars is nil. Mirrors the inject-and-go-test pattern
// used by TestGenerateMetaSyncErrorClassification so we exercise the helper
// in its real package context.
func TestGenerateEndpointTemplateVarsRuntimeSubstitution(t *testing.T) {
	t.Parallel()

	// Variable names follow the {upper Name}_{upper var} convention. For
	// Shopify, this means the spec's var name is "api_version" (not
	// "version") because the real-world env var is SHOPIFY_API_VERSION.
	// The URL placeholder mirrors the var name, so both sides line up.
	apiSpec := &spec.APISpec{
		Name:                 "shopify",
		Description:          "Shopify Admin GraphQL (test fixture)",
		Version:              "2026-04",
		BaseURL:              "https://{shop}",
		GraphQLEndpointPath:  "/admin/api/{api_version}/graphql.json",
		EndpointTemplateVars: []string{"shop", "api_version"},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List orders",
						ResponsePath: "data.orders.nodes",
						Pagination: &spec.Pagination{
							Type:           "cursor",
							LimitParam:     "first",
							CursorParam:    "after",
							NextCursorPath: "data.orders.pageInfo.endCursor",
							HasMoreField:   "data.orders.pageInfo.hasNextPage",
						},
						Response: spec.ResponseDef{Type: "array", Item: "Order"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Order": {Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// url.go must exist; this is where buildURL and templateVarEnvNames live.
	urlGoPath := filepath.Join(outputDir, "internal", "client", "url.go")
	urlGoBytes, err := os.ReadFile(urlGoPath)
	require.NoError(t, err, "url.go should be generated when EndpointTemplateVars is set")
	urlGo := string(urlGoBytes)
	assert.Contains(t, urlGo, "func buildURL(",
		"url.go must define the buildURL helper")
	assert.Contains(t, urlGo, `"shop": "SHOPIFY_SHOP"`,
		"templateVarEnvNames must wire {shop} to SHOPIFY_SHOP")
	assert.Contains(t, urlGo, `"api_version": "SHOPIFY_API_VERSION"`,
		"templateVarEnvNames must wire {api_version} to SHOPIFY_API_VERSION")

	// config.go must populate Config.TemplateVars from env at Load() time.
	configGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configGo := string(configGoBytes)
	assert.Contains(t, configGo, "TemplateVars map[string]string",
		"config struct must carry the TemplateVars map")
	assert.Contains(t, configGo, `os.Getenv("SHOPIFY_SHOP")`,
		"config Load() must read SHOPIFY_SHOP from env")
	assert.Contains(t, configGo, `os.Getenv("SHOPIFY_API_VERSION")`,
		"config Load() must read SHOPIFY_API_VERSION from env (spec var name 'api_version')")

	// client.go must route requests through buildURL, not the old c.BaseURL+path concat.
	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)
	assert.Contains(t, clientGo, "buildURL(c.BaseURL, path,",
		"client.do() must call buildURL when EndpointTemplateVars is set")
	assert.NotContains(t, clientGo, "targetURL := c.BaseURL + path",
		"client.do() must not retain the raw concat once EndpointTemplateVars is set")
	// Cache key must include the template-var values so two shops never
	// collide on the same path. Without this, a warm SHOPIFY_SHOP=A cache
	// would feed responses to a SHOPIFY_SHOP=B caller.
	assert.Contains(t, clientGo, `c.Config.TemplateVars[name]`,
		"cacheKey must mix template-var values into the cache identity")

	// Compile the generated tree before injecting a behavioral test — a syntax
	// error in url.go.tmpl should surface here, not as a confusing test
	// runner failure.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	// Inject a unit test for buildURL in its real package context. Covers the
	// three acceptance paths from PR-2: full substitution, actionable error
	// for missing env var, and pure passthrough when no placeholders are
	// present. The test file is package-local so it sees buildURL and
	// templateVarEnvNames directly.
	behaviorTest := `package client

import (
	"strings"
	"testing"

	cfgpkg "shopify-pp-cli/internal/config"
)

func TestBuildURLSubstitutes(t *testing.T) {
	vars := map[string]string{
		"shop":        "foo.myshopify.com",
		"api_version": "2026-04",
	}
	got, err := buildURL("https://{shop}", "/admin/api/{api_version}/graphql.json", vars)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	const want = "https://foo.myshopify.com/admin/api/2026-04/graphql.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildURLMissingShopReturnsActionableError(t *testing.T) {
	vars := map[string]string{"api_version": "2026-04"}
	_, err := buildURL("https://{shop}", "/admin/api/{api_version}/graphql.json", vars)
	if err == nil {
		t.Fatal("expected error when {shop} cannot be resolved")
	}
	msg := err.Error()
	if !strings.Contains(msg, "SHOPIFY_SHOP not set") {
		t.Errorf("error must name SHOPIFY_SHOP; got %q", msg)
	}
	if !strings.Contains(msg, "export SHOPIFY_SHOP=") {
		t.Errorf("error must include export hint; got %q", msg)
	}
}

func TestBuildURLPassthroughWhenNoPlaceholders(t *testing.T) {
	got, err := buildURL("https://api.example.com", "/v1/items", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "https://api.example.com/v1/items"
	if got != want {
		t.Errorf("got %q, want %q (no substitution should be attempted)", got, want)
	}
}

func TestBuildURLEmptyVarValueIsTreatedAsUnset(t *testing.T) {
	// An env var that exists but is empty must produce the same actionable
	// error as one that's not set — otherwise a stray "export FOO=" silently
	// sends requests to literal "{foo}" URLs.
	vars := map[string]string{"shop": "", "api_version": "2026-04"}
	_, err := buildURL("https://{shop}", "/admin/api/{api_version}/graphql.json", vars)
	if err == nil {
		t.Fatal("expected error when {shop} value is empty")
	}
	if !strings.Contains(err.Error(), "SHOPIFY_SHOP") {
		t.Errorf("expected error to name SHOPIFY_SHOP; got %q", err.Error())
	}
}

// TestCacheKeyIncludesTemplateVars guards the per-tenant isolation contract:
// the same path under two different shops must produce two different cache
// keys, and flipping a value back to unset must miss the warm cache.
func TestCacheKeyIncludesTemplateVars(t *testing.T) {
	mk := func(shop, ver string) *Client {
		cfg := &cfgpkg.Config{TemplateVars: map[string]string{}}
		if shop != "" {
			cfg.TemplateVars["shop"] = shop
		}
		if ver != "" {
			cfg.TemplateVars["api_version"] = ver
		}
		return &Client{Config: cfg}
	}

	keyA := mk("storeA.myshopify.com", "2026-04").cacheKey("/v1/orders", nil)
	keyB := mk("storeB.myshopify.com", "2026-04").cacheKey("/v1/orders", nil)
	if keyA == keyB {
		t.Fatalf("cacheKey collided across shops: storeA and storeB share %q", keyA)
	}
	keyEmpty := mk("", "2026-04").cacheKey("/v1/orders", nil)
	if keyEmpty == keyA {
		t.Fatalf("cacheKey collided when SHOPIFY_SHOP unset matched a warm shop value: both produced %q", keyA)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "client", "url_behavior_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestBuildURL")
}

// TestGenerateNoEndpointTemplateVarsByteCompat guards the byte-compat
// promise: a spec that doesn't declare EndpointTemplateVars must regenerate
// without url.go and with the original c.BaseURL+path concat in client.do().
// Adding url.go to every printed CLI would silently bump file counts in
// downstream library mirrors; this test catches that regression.
func TestGenerateNoEndpointTemplateVarsByteCompat(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	require.Empty(t, apiSpec.EndpointTemplateVars,
		"loops fixture must keep EndpointTemplateVars empty for the byte-compat case")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	_, err = os.Stat(filepath.Join(outputDir, "internal", "client", "url.go"))
	assert.True(t, os.IsNotExist(err),
		"url.go must NOT be emitted for specs without EndpointTemplateVars; got err=%v", err)

	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)
	assert.Contains(t, clientGo, "targetURL := c.BaseURL + path",
		"client.do() must keep the raw concat when EndpointTemplateVars is empty")
	assert.NotContains(t, clientGo, "buildURL(",
		"client.do() must not call buildURL when EndpointTemplateVars is empty")

	configGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configGo := string(configGoBytes)
	assert.NotContains(t, configGo, "TemplateVars",
		"config struct must not carry TemplateVars when EndpointTemplateVars is empty")
}

// TestGenerateResourceBaseURLOverrideRoutesToOverrideHost — Open-Meteo
// shape: top-level BaseURL points at api.open-meteo.com, but the
// geocoding resource lives at geocoding-api.open-meteo.com. The
// generated handler must emit the override host into `path` so the
// client's absolute-URL branch routes the request to the right host.
//
// Each resource carries two endpoints so the generator emits per-
// endpoint handler files instead of promoting them to top-level
// commands (single-endpoint resources are inlined into a promoted
// command, which would skip the per-endpoint file emit path).
func TestGenerateResourceBaseURLOverrideRoutesToOverrideHost(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("multihost")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.Resources["forecast"] = spec.Resource{
		Description: "Weather forecast",
		Endpoints: map[string]spec.Endpoint{
			"now":    {Method: "GET", Path: "/forecast", Description: "Current"},
			"hourly": {Method: "GET", Path: "/forecast/hourly", Description: "Hourly"},
		},
	}
	apiSpec.Resources["geocoding"] = spec.Resource{
		Description: "Geocoding lookup",
		BaseURL:     "https://geocoding-api.example.com/v1",
		Endpoints: map[string]spec.Endpoint{
			"search":  {Method: "GET", Path: "/search", Description: "Search"},
			"reverse": {Method: "GET", Path: "/reverse", Description: "Reverse"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// The geocoding handler emits the absolute URL into `path`.
	geoHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "geocoding_search.go"))
	require.NoError(t, err)
	assert.Contains(t, string(geoHandler), `path := "https://geocoding-api.example.com/v1/search"`,
		"geocoding handler must emit the absolute URL into path")

	// The forecast handler keeps the relative path (no override on this resource).
	fcHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "forecast_now.go"))
	require.NoError(t, err)
	assert.Contains(t, string(fcHandler), `path := "/forecast"`,
		"forecast handler must keep the relative path when its resource has no override")

	// The client emits the absolute-URL branch + isAbsoluteURL helper.
	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), "func isAbsoluteURL(path string) bool",
		"client must emit isAbsoluteURL helper when at least one resource has a BaseURL override")
	assert.Contains(t, string(clientGo), "if isAbsoluteURL(path) {",
		"client.do() must branch on isAbsoluteURL")

	// Typed MCP endpoint tools must use the same effective path as the CLI
	// command. The cobratree walker skips pp:endpoint commands, so a raw
	// endpoint-mirror tool with "/search" would route agents to the spec-level
	// BaseURL instead of the resource override host.
	mcpTools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	mcpToolsStr := string(mcpTools)
	assert.Contains(t, mcpToolsStr, `makeAPIHandler("GET", "https://geocoding-api.example.com/v1/search"`,
		"geocoding MCP endpoint mirror must emit the absolute override URL")
	assert.Contains(t, mcpToolsStr, `makeAPIHandler("GET", "/forecast"`,
		"forecast MCP endpoint mirror must keep the relative path when its resource has no override")

	// Must compile.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateSubResourceInheritsParentBaseURL — a sub-resource without
// its own BaseURL inherits the parent resource's override. An explicit
// sub-resource override takes precedence.
func TestGenerateSubResourceInheritsParentBaseURL(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("multihost-sub")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.Resources["geocoding"] = spec.Resource{
		Description: "Geocoding",
		BaseURL:     "https://geocoding-api.example.com/v1",
		SubResources: map[string]spec.Resource{
			// Inherits parent override.
			"city": {
				Description: "City lookup",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/city", Description: "Get"},
				},
			},
			// Explicit override beats parent.
			"reverse": {
				Description: "Reverse geocoding",
				BaseURL:     "https://reverse-api.example.com/v1",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/reverse", Description: "Get"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	cityHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "geocoding_city_get.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cityHandler), `path := "https://geocoding-api.example.com/v1/city"`,
		"sub-resource without its own BaseURL must inherit the parent's override")

	reverseHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "geocoding_reverse_get.go"))
	require.NoError(t, err)
	assert.Contains(t, string(reverseHandler), `path := "https://reverse-api.example.com/v1/reverse"`,
		"sub-resource with its own BaseURL must use its own override")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateNoResourceBaseURLOverrideByteCompat — specs without any
// per-resource BaseURL override must regenerate without the
// isAbsoluteURL helper or the absolute-URL branch in client.do().
// Mirrors the EndpointTemplateVars byte-compat guarantee at the
// resource level.
func TestGenerateNoResourceBaseURLOverrideByteCompat(t *testing.T) {
	t.Parallel()
	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	for _, r := range apiSpec.Resources {
		require.Empty(t, r.BaseURL,
			"loops fixture must keep resource BaseURL empty for the byte-compat case")
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(clientGo), "isAbsoluteURL",
		"client.go must not carry the isAbsoluteURL helper when no resource has a BaseURL override")
	assert.Contains(t, string(clientGo), "targetURL := c.BaseURL + path",
		"client.do() must keep the raw concat when no resource has a BaseURL override")
}

// TestGenerateResourceBaseURLTrailingSlashTrimmed — the most likely
// spec-author mistake is `base_url: "https://api.example.com/v1/"`
// (trailing slash) paired with `endpoints[].path: "/search"` (leading
// slash). Without normalization the emitted handler concatenates to
// `https://api.example.com/v1//search`. Trim trailing slash off
// the override at the data-passing site so spec authors don't have
// to memorize the convention.
func TestGenerateResourceBaseURLTrailingSlashTrimmed(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("trailingslash")
	apiSpec.Resources["geocoding"] = spec.Resource{
		Description: "Geocoding lookup",
		BaseURL:     "https://geocoding-api.example.com/v1/",
		Endpoints: map[string]spec.Endpoint{
			"search":  {Method: "GET", Path: "/search", Description: "Search"},
			"reverse": {Method: "GET", Path: "/reverse", Description: "Reverse"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	geoHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "geocoding_search.go"))
	require.NoError(t, err)
	assert.Contains(t, string(geoHandler), `path := "https://geocoding-api.example.com/v1/search"`,
		"trailing slash on the override must be trimmed before concatenation")
	assert.NotContains(t, string(geoHandler), `path := "https://geocoding-api.example.com/v1//search"`,
		"double slash from base+path concat must not leak into the generated handler")
}

// TestGenerateResourceBaseURLWithEndpointTemplateVars — confirms
// per-resource BaseURL composes correctly with EndpointTemplateVars.
// A resource declares both a templated host (`{shop}` resolved from
// env at runtime) and a fixed override host. The generator emits the
// templated absolute URL into `path`; the client's buildURL substitutes
// the placeholder before the request fires.
func TestGenerateResourceBaseURLWithEndpointTemplateVars(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("multitenant-multihost")
	apiSpec.BaseURL = "https://{shop}.api.example.com/v1"
	apiSpec.EndpointTemplateVars = []string{"shop"}
	apiSpec.Resources["storefront"] = spec.Resource{
		Description: "Storefront API on a per-tenant host",
		BaseURL:     "https://{shop}.storefront-api.example.com/v1",
		Endpoints: map[string]spec.Endpoint{
			"products":    {Method: "GET", Path: "/products", Description: "List products"},
			"collections": {Method: "GET", Path: "/collections", Description: "List collections"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// Handler emits the override host with the placeholder intact;
	// resolution happens in the client at request time.
	handler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "storefront_products.go"))
	require.NoError(t, err)
	assert.Contains(t, string(handler),
		`path := "https://{shop}.storefront-api.example.com/v1/products"`,
		"override host with {placeholder} markers must flow into path verbatim")

	// Client emits both the EndpointTemplateVars resolution AND the
	// absolute-URL detection — the combined branch goes through
	// `buildURL("", path, endpointVars)` for absolute paths.
	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientStr := string(clientGo)
	assert.Contains(t, clientStr, "func isAbsoluteURL(path string) bool",
		"isAbsoluteURL helper must be emitted")
	assert.Contains(t, clientStr, `buildURL("", path, endpointVars)`,
		"absolute-URL branch must call buildURL with empty BaseURL so {placeholder} markers in path still substitute")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateResourceBaseURLOverrideRoutesAgentDispatchSurfaces covers MCP
// surfaces that execute through generated registries instead of Cobra
// commands. The CLI command templates already prepend Resource.BaseURL; these
// registries must do the same or agents route overridden resources to the
// wrong host.
func TestGenerateResourceBaseURLOverrideRoutesAgentDispatchSurfaces(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("multihost-agent")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.Resources["geocoding"] = spec.Resource{
		Description: "Geocoding lookup",
		BaseURL:     "https://geocoding-api.example.com/v1",
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method:      "GET",
				Path:        "/search",
				Description: "Search",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true, Description: "query"},
				},
			},
			"reverse": {Method: "GET", Path: "/reverse", Description: "Reverse"},
		},
	}
	apiSpec.MCP = spec.MCPConfig{
		Orchestration: "code",
		Intents: []spec.Intent{
			{
				Name:        "geocode",
				Description: "Geocode a query",
				Params: []spec.IntentParam{
					{Name: "q", Type: "string", Required: true, Description: "query"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "geocoding.search",
						Bind:     map[string]string{"q": "${input.q}"},
						Capture:  "results",
					},
				},
				Returns: "results",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	codeOrch, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "code_orch.go"))
	require.NoError(t, err)
	assert.Contains(t, string(codeOrch), `Path:    "https://geocoding-api.example.com/v1/search"`,
		"code-orchestration execute must use the effective resource override URL")

	intents, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "intents.go"))
	require.NoError(t, err)
	assert.Contains(t, string(intents), `"geocoding.search": {method: "GET", path: "https://geocoding-api.example.com/v1/search"}`,
		"intent dispatch must use the effective resource override URL")

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

// TestGenerateMCPNewClientSkipsCache proves that newMCPClient sets
// c.NoCache = true. Agents calling through MCP need fresh data every call;
// the on-disk response cache survives across MCP server invocations, so
// without this the next GET after a DELETE/PATCH returns the pre-mutation
// snapshot for up to the cache TTL. Interactive CLI commands construct
// their own client and are unaffected.
func TestGenerateMCPNewClientSkipsCache(t *testing.T) {
	t.Parallel()

	t.Run("default spec source", func(t *testing.T) {
		t.Parallel()
		assertNewMCPClientSkipsCache(t, "")
	})

	// SpecSource=sniffed takes the rate-limited branch in the template
	// (client.New(cfg, 30*time.Second, 2) instead of ..., 0). The cache
	// bypass must apply on both branches; this guards against a future
	// edit moving NoCache=true inside one of the if/else arms.
	t.Run("sniffed spec source", func(t *testing.T) {
		t.Parallel()
		assertNewMCPClientSkipsCache(t, "sniffed")
	})

	t.Run("interactive CLI client is not statically NoCache=true", func(t *testing.T) {
		t.Parallel()
		apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		rootData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		rootBody := string(rootData)

		assert.Contains(t, rootBody, "c.NoCache = f.noCache",
			"interactive CLI's newClient must drive cache from --no-cache flag, not statically")
	})
}

// assertNewMCPClientSkipsCache generates a CLI from loops.yaml with the
// given SpecSource and asserts the MCP server's newMCPClient bypasses the
// response cache.
func assertNewMCPClientSkipsCache(t *testing.T, specSource string) {
	t.Helper()
	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.SpecSource = specSource

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	assert.Contains(t, string(toolsData), "c.NoCache = true",
		"newMCPClient must disable the response cache so MCP-driven reads see fresh state across mutations")
}

// TestGenerateMCPHandlerPreservesQueryPositionals proves the makeAPIHandler
// body in generated MCP tools.go distinguishes real URL path placeholders
// (e.g. /movie/{movieId}) from CLI positional args that map to query
// params (e.g. `search <query>` -> ?query=). The bug was that the handler
// dropped every positionalParams entry from the query map, so a query-style
// positional like `query` silently disappeared from the request — TMDb
// returned an empty page for movies_search even with a non-empty query.
func TestGenerateMCPHandlerPreservesQueryPositionals(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcphandler")
	apiSpec.Resources = map[string]spec.Resource{
		"movies": {
			Description: "Movies",
			Endpoints: map[string]spec.Endpoint{
				// Query-style positional: `query` is a CLI positional but
				// must be sent as ?query=… because the path has no {query}.
				"search": {
					Method:      "GET",
					Path:        "/search/movie",
					Description: "Search movies",
					Params: []spec.Param{
						{Name: "query", Type: "string", Required: true, Positional: true, Description: "Search query"},
						{Name: "year", Type: "string", Description: "Release year"},
					},
				},
				// Real path param: `movieId` is substituted into the URL.
				"get": {
					Method:      "GET",
					Path:        "/movie/{movieId}",
					Description: "Get a movie",
					Params: []spec.Param{
						{Name: "movieId", Type: "string", Required: true, PathParam: true, Description: "Movie ID"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	toolsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	tools := string(toolsData)

	// Call sites still pass both names — the upstream emit is unchanged;
	// the fix lives entirely inside the handler body.
	assert.Regexp(t,
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/search/movie",\s*\[]mcpParamBinding\{.*WireName: "query".*\},\s*\[]string\{[^}]*"query"`),
		tools,
		"search call site must still pass `query` in positionalParams (handler decides path vs query at runtime)")
	assert.Regexp(t,
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/movie/\{movieId\}",\s*\[]mcpParamBinding\{.*WireName: "movieId".*\},\s*\[]string\{[^}]*"movieId"`),
		tools,
		"get-by-id call site must pass `movieId` in positionalParams")

	assert.Regexp(t,
		regexp.MustCompile(`strings\.Contains\(pathTemplate,`),
		tools,
		"makeAPIHandler must guard the path-substitution loop with a placeholder-presence check so query-style positionals stay in the query map")
	assert.NotContains(t, tools, "isPositional := false",
		"old handler shape skipped every positionalParams entry from query — must not regress")

	// Generated CLI must still compile.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratePublicParamNamesAcrossCLISurfaces(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("public-params")
	delete(apiSpec.Resources, "items")
	apiSpec.Resources["stores"] = spec.Resource{
		Description: "Stores",
		Endpoints: map[string]spec.Endpoint{
			"find": {
				Method:      "GET",
				Path:        "/power/store-locator",
				Description: "Find nearby stores by address",
				Params: []spec.Param{
					{Name: "s", FlagName: "address", Aliases: []string{"s"}, Type: "string", Required: true, Description: "Street address"},
					{Name: "c", FlagName: "city", Aliases: []string{"c"}, Type: "string", Required: true, Description: "City, state, zip"},
				},
			},
			"create": {
				Method:      "POST",
				Path:        "/stores",
				Description: "Create a store",
				Body: []spec.Param{
					{Name: "store_code", FlagName: "store-code", Type: "string", Required: true, Description: "Store code"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	findSource := readGeneratedFile(t, outputDir, "internal", "cli", "stores_find.go")
	assert.Contains(t, findSource, `public-params-pp-cli stores find --address example-value --city example-value`)
	assert.Contains(t, findSource, `StringVar(&flagS, "address", "", "Street address")`)
	assert.Contains(t, findSource, `StringVar(&flagS, "s", "", "Street address")`)
	assert.Contains(t, findSource, `_ = cmd.Flags().MarkHidden("s")`)
	assert.Contains(t, findSource, `if !(cmd.Flags().Changed("address") || cmd.Flags().Changed("s")) && !flags.dryRun`)
	assert.Contains(t, findSource, `params["s"] = fmt.Sprintf("%v", flagS)`)
	assert.NotContains(t, findSource, `required flag "s" not set`)

	createSource := readGeneratedFile(t, outputDir, "internal", "cli", "stores_create.go")
	assert.Contains(t, createSource, `public-params-pp-cli stores create --store-code example-value`)
	assert.Contains(t, createSource, `StringVar(&bodyStoreCode, "store-code", "", "Store code")`)
	assert.Contains(t, createSource, `body["store_code"] = bodyStoreCode`)

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSource, `mcplib.WithString("address", mcplib.Required(), mcplib.Description("Street address"))`)
	assert.Contains(t, mcpSource, `PublicName: "address", WireName: "s", Location: "query"`)
	assert.Contains(t, mcpSource, `params[binding.WireName] = fmt.Sprintf("%v", v)`)
	assert.Contains(t, mcpSource, `mcplib.WithString("store-code", mcplib.Required(), mcplib.Description("Store code"))`)
	assert.Contains(t, mcpSource, `PublicName: "store-code", WireName: "store_code", Location: "body"`)
	assert.Contains(t, mcpSource, `bodyArgs[binding.WireName] = v`)

	readme := readGeneratedFile(t, outputDir, "README.md")
	assert.Contains(t, readme, `public-params-pp-cli stores create --store-code example-value`)

	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	assert.Contains(t, skill, `public-params-pp-cli stores create --store-code example-value`)
}

func TestGeneratePublicParamNamesInPromotedExamples(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-public-params")
	apiSpec.Resources["checkout"] = spec.Resource{
		Description: "Checkout",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/checkout",
				Description: "Create a checkout",
				Body: []spec.Param{
					{Name: "store_code", FlagName: "store-code", Type: "string", Required: true, Description: "Store code"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	promotedSource := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_checkout.go")
	assert.Contains(t, promotedSource, `Example: "  promoted-public-params-pp-cli checkout --store-code example-value"`)
}

// TestGenerateMCPCodeOrchKeywordsHasStopwordFilter proves the keyword
// extractor in the code-orchestration thin surface filters short tokens
// and a stopword set. Without this, two-letter substrings like "is"/"in"
// inside endpoint descriptions match every query token via the
// substring-contains rank rule, polluting search results.
func TestGenerateMCPCodeOrchKeywordsHasStopwordFilter(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	codeOrchData, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "code_orch.go"))
	require.NoError(t, err)
	body := string(codeOrchData)

	assert.Contains(t, body, "var codeOrchStopwords = map[string]bool{",
		"code_orch.go must declare codeOrchStopwords")
	assert.Contains(t, body, `len(tok) < 3 || codeOrchStopwords[tok]`,
		"keyword extractor must filter short tokens and stopwords")
	for _, sw := range []string{`"is"`, `"in"`, `"the"`, `"and"`} {
		assert.Contains(t, body, sw, "stopword set missing %q", sw)
	}
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

func TestToKebab_SnakeCaseInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
		why      string
	}{
		{"customer_feedback", "customer-feedback", "snake_case spec key flows to user-facing kebab-case"},
		{"slot_list_for_date", "slot-list-for-date", "multi-segment snake → kebab"},
		{"window_days", "window-days", "two-word snake → kebab"},
		{"already-kebab", "already-kebab", "kebab passes through unchanged"},
		{"mixed_caseInput", "mixed-case-input", "mixed snake + camel both convert"},
		{"single", "single", "single-word lowercase unchanged"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := toKebab(tt.input)
			require.Equal(t, tt.expected, got, tt.why)
		})
	}
}

// TestTemplatesEmitReadOnlyAnnotation locks in the contract that
// novel-command templates whose only effect is reading from the API,
// the local store, or the CLI tree itself emit the mcp:read-only Cobra
// annotation. The runtime walker uses this annotation to set
// readOnlyHint on the resulting MCP tool so hosts skip the per-call
// permission prompt. See AGENTS.md "Tool safety annotations".
//
// Templates intentionally NOT in this list because they mutate state
// or write user-visible files outside the local cache:
// export.go.tmpl (--output writes user files), share_commands.go.tmpl
// (snapshot dirs, git pushes), import/sync/feedback/graphql_sync (writes).
func TestTemplatesEmitReadOnlyAnnotation(t *testing.T) {
	t.Parallel()
	annotationRE := regexp.MustCompile(`Annotations:\s+map\[string\]string\{"mcp:read-only":\s*"true"\}`)

	cases := []struct {
		template string
		// minMatches is the number of distinct cobra.Command literals
		// inside the template that should carry the annotation. Templates
		// emitting one command have minMatches=1; multi-command templates
		// list the read-only subcommands' count.
		minMatches int
		why        string
	}{
		{"analytics.go.tmpl", 1, "analytics queries on the local store"},
		{"agent_context.go.tmpl", 1, "walks cobra tree, emits introspection JSON"},
		{"api_discovery.go.tmpl", 1, "walks cobra tree, prints help"},
		{"tail.go.tmpl", 1, "polls API GETs, NDJSON to stdout"},
		{"jobs.go.tmpl", 2, "list and get; prune is omitted (mutates ledger)"},
		{"channel_workflow.go.tmpl", 2, "status and generated payment-plan are read-only; workflow parent and archive omitted"},
		{"workflows/pm_stale.go.tmpl", 1, "queries the local store for stale items"},
		{"workflows/pm_orphans.go.tmpl", 1, "queries the local store for missing fields"},
		{"workflows/pm_load.go.tmpl", 1, "queries the local store for workload distribution"},
	}

	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("templates", tc.template))
			require.NoError(t, err)
			matches := annotationRE.FindAllString(string(data), -1)
			assert.Len(t, matches, tc.minMatches,
				"%s should carry exactly %d mcp:read-only annotation(s) — %s",
				tc.template, tc.minMatches, tc.why)
		})
	}
}

func TestProjectManagementWorkflowsEmitReadOnlyAnnotations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pmworkflows")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store: true,
		Sync:  true,
		Workflows: []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		},
	}
	require.NoError(t, gen.Generate())

	annotationRE := regexp.MustCompile(`Annotations:\s+map\[string\]string\{"mcp:read-only":\s*"true"\}`)
	for _, file := range []string{"pm_stale.go", "pm_orphans.go", "pm_load.go"} {
		data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", file))
		require.NoError(t, err)
		assert.Len(t, annotationRE.FindAllString(string(data), -1), 1,
			"%s should emit exactly one mcp:read-only annotation", file)
	}
}

// TestSearchTemplateEmitsEmptyJSONEnvelope pins the contract: the
// generated `search` command in --json (or piped) mode must always emit
// a valid JSON envelope, including on no matches. Agents pipe stdout
// through json.loads / jq and a silent return-nil breaks parsing.
func TestSearchTemplateEmitsEmptyJSONEnvelope(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("templates", "search.go.tmpl"))
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, `jsonMode := flags.asJSON || !isTerminal(cmd.OutOrStdout())`,
		"search.go.tmpl must compute jsonMode so the envelope path runs even on no matches")

	// Ordering pin: the jsonMode block must come before the human-mode
	// "No results" stderr line. Reversing the order would skip the JSON
	// envelope on empty results — the original failure mode.
	jsonModeIdx := strings.Index(body, "jsonMode := flags.asJSON")
	noResultsIdx := strings.Index(body, `"No results (source: %s)\n"`)
	require.GreaterOrEqual(t, jsonModeIdx, 0)
	require.GreaterOrEqual(t, noResultsIdx, 0)
	assert.Less(t, jsonModeIdx, noResultsIdx,
		"jsonMode check must come before the human-mode 'No results' stderr line; otherwise the JSON-envelope path is skipped on empty results")
}

// TestStoreSkipsDeadTablesForResourcesWithoutTypedUpsert pins the gate that
// ties table creation to typed-Upsert generation. Without it, resources
// whose names collide with framework cobra commands get renamed
// `<api-slug>-<name>` and produce 3-column tables that nothing writes to —
// UpsertBatch's switch has no case for them, so inserts only land in
// `resources`. The dead typed table bloats schema without ever being
// populated.
func TestStoreSkipsDeadTablesForResourcesWithoutTypedUpsert(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "demo",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/demo-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"auth": { // Will rename to `demo-auth` (framework collision)
				Description: "Auth tokens",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/auth", Description: "List auth tokens"},
				},
			},
			"items": { // Has typed columns -> keeps its table + Upsert
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Response:    spec.ResponseDef{Type: "array", Item: "Item"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Item": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "status", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
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
	store := string(storeSrc)

	// Pin the exact CREATE TABLE set: only resources, sync_state (always
	// emitted), and items (has typed columns) survive. No demo_auth, no
	// renamed-with-suffix dead table, nothing else snuck in.
	createRe := regexp.MustCompile("`CREATE TABLE IF NOT EXISTS (\\w+) \\(")
	gotTables := map[string]bool{}
	for _, m := range createRe.FindAllStringSubmatch(store, -1) {
		gotTables[m[1]] = true
	}
	assert.Equal(t, map[string]bool{"resources": true, "sync_state": true, "items": true}, gotTables,
		"only typed-table resources + the always-on resources/sync_state should be created")

	// items keeps its typed Upsert; demo_auth/demo-auth gets none.
	assert.Contains(t, store, "func (s *Store) UpsertItems(", "typed Upsert should exist for items")
	assert.NotRegexp(t, regexp.MustCompile(`func \(s \*Store\) Upsert(Demo)?Auth\(`), store,
		"no typed Upsert for the renamed dead resource (under any casing)")

	// Renamed resources still flow through the generic path. Anchor the
	// claim that UpsertBatch keeps writing to `resources` for unknown types.
	assert.Contains(t, store, "upsertGenericResourceTx(tx, resourceType, id",
		"UpsertBatch must still call upsertGenericResourceTx so renamed resources land in `resources`")
}
