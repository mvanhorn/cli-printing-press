package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/japanese"
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
		"internal/cli/root_test.go",
		"internal/cli/sync_hint.go",
		"internal/cli/sync_hint_test.go",
		"internal/cliutil/fanout.go",
		"internal/cliutil/text.go",
		"internal/cliutil/probe.go",
		"internal/cliutil/ratelimit.go",
		"internal/cliutil/verifyenv.go",
		"internal/cliutil/extractnumber.go",
		"internal/cliutil/extractnumber_test.go",
		"internal/cliutil/jwtshape.go",
		"internal/cliutil/jwtshape_test.go",
		"internal/cliutil/duration.go",
		"internal/cliutil/duration_test.go",
		"internal/cliutil/odata_date.go",
		"internal/cliutil/odata_date_test.go",
		"internal/cliutil/cliutil_test.go",
		"internal/client/client.go",
		"internal/client/client_test.go",
		"internal/client/client_verify_short_circuit_test.go",
		"internal/config/config.go",
		"internal/store/extras.go",
		"internal/mcp/cobratree/walker.go",
		"internal/mcp/cobratree/classify.go",
		"internal/mcp/cobratree/typemap.go",
		"internal/mcp/cobratree/shellout.go",
		"internal/mcp/cobratree/shellout_test.go",
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
		{name: "stytch", specPath: filepath.Join("..", "..", "testdata", "stytch.yaml"), expectedFiles: 70},
		{name: "clerk", specPath: filepath.Join("..", "..", "testdata", "clerk.yaml"), expectedFiles: 75},
		{name: "loops", specPath: filepath.Join("..", "..", "testdata", "loops.yaml"), expectedFiles: 72},
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

func TestGenerateStoreExtrasHook(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("storeextras")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	extrasSrc := readGeneratedFile(t, outputDir, "internal", "store", "extras.go")
	assert.NotContains(t, extrasSrc, "DO NOT EDIT")
	assert.Contains(t, extrasSrc, "func (s *Store) migrateExtras(ctx context.Context, conn *sql.Conn) error")
	assert.Contains(t, extrasSrc, "Add CREATE TABLE IF NOT EXISTS statements here")

	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	migrationIndex := strings.Index(storeSrc, "for _, m := range migrations")
	extrasIndex := strings.Index(storeSrc, "s.migrateExtras(ctx, conn)")
	stampIndex := strings.Index(storeSrc, "PRAGMA user_version = %d")
	require.NotEqual(t, -1, migrationIndex, "store.go must still run generated migrations")
	require.NotEqual(t, -1, extrasIndex, "store.go must call migrateExtras")
	require.NotEqual(t, -1, stampIndex, "store.go must still stamp user_version")
	assert.Less(t, migrationIndex, extrasIndex, "extras must run after generated migrations")
	assert.Less(t, extrasIndex, stampIndex, "extras must run before the schema version stamp")

	runGoCommand(t, outputDir, "test", "./internal/store")
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

	// All cliutil files must be emitted.
	cliutilDir := filepath.Join(outputDir, "internal", "cliutil")
	for _, name := range []string{"fanout.go", "text.go", "extractnumber.go", "extractnumber_test.go", "cliutil_test.go"} {
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
		{"extractnumber.go", "func ExtractNumber("},
		{"extractnumber.go", "func ExtractInt("},
		{"jwtshape.go", "func LooksLikeJWT("},
		{"jwtshape.go", "func FindJWTInCookieJar("},
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
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
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

	contactsIDRE := regexp.MustCompile(`"contacts":\s+"id",`)
	contactsCriticalRE := regexp.MustCompile(`"contacts":\s+true,`)
	assert.Len(t, contactsIDRE.FindAllStringIndex(string(storeSrc), -1), 1, "store.go should emit one ID override per resource")
	assert.Len(t, contactsIDRE.FindAllStringIndex(string(syncSrc), -1), 1, "sync.go should emit one ID override per resource")
	assert.Len(t, contactsCriticalRE.FindAllStringIndex(string(syncSrc), -1), 1, "sync.go should emit one critical flag per resource")
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
		// Refresh failure must emit a structured JSON event to stderr so
		// agents reading novel-command output that bypasses
		// wrapWithProvenance still see a parseable degraded-state signal
		// (issue #1263). The prose warning remains for humans.
		"func emitCacheRefreshFailedEvent(",
		`emitCacheRefreshFailedEvent(resources, err)`,
		`"cache_warning"`,
		`"refresh_failed"`,
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

	// auto_refresh_test.go covers the structured cache_warning emitter so a
	// Go syntax error in auto_refresh_test.go.tmpl is caught at generation
	// time rather than propagating silently to every customer CLI (the
	// golden suite has no cache-enabled case, so the template is otherwise
	// never compiled by scripts/golden.sh verify).
	autoRefreshTestPath := filepath.Join(outputDir, "internal", "cli", "auto_refresh_test.go")
	testData, err := os.ReadFile(autoRefreshTestPath)
	require.NoError(t, err, "auto_refresh_test.go must be emitted when cache is enabled")
	testSrc := string(testData)
	for _, snippet := range []string{
		"func captureStderr(t *testing.T, fn func()) []byte",
		"func TestEmitCacheRefreshFailedEvent(t *testing.T)",
		"func TestEmitCacheRefreshFailedEvent_NilResources(t *testing.T)",
		`emitCacheRefreshFailedEvent([]string{"items"}`,
		`"cache_warning"`,
		`"refresh_failed"`,
	} {
		assert.Contains(t, testSrc, snippet, "auto_refresh_test.go missing %q", snippet)
	}

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

	t.Run("OpenAPI prose-inferred bearer spec generates simple auth command", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "prose-bearer-auth.yaml"))
		require.NoError(t, err)

		apiSpec, err := openapi.Parse(data)
		require.NoError(t, err)
		require.Equal(t, "bearer_token", apiSpec.Auth.Type)

		// Existing golden fixtures pin simple-auth template output; this test
		// covers the new OpenAPI inference path into that generated surface.
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
		configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		assert.Contains(t, string(configGo), "GITHUB_TOKEN")

		binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
		runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
		helpOut, err := exec.Command(binaryPath, "auth", "--help").CombinedOutput()
		require.NoError(t, err, string(helpOut))
		assert.Contains(t, string(helpOut), "status")
		assert.Contains(t, string(helpOut), "set-token")
		assert.Contains(t, string(helpOut), "logout")
	})
}

func TestGenerateOAuth2RefreshTokenMechanism(t *testing.T) {
	t.Parallel()

	baseSpec := func() *spec.APISpec {
		return &spec.APISpec{
			Name:    "oauthapi",
			Version: "0.1.0",
			BaseURL: "https://api.example.com",
			Auth: spec.AuthConfig{
				Type:             "bearer_token",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				EnvVars:          []string{"OAUTHAPI_TOKEN"},
				AuthorizationURL: "https://api.example.com/oauth/authorize",
				TokenURL:         "https://api.example.com/oauth/token",
				Scopes:           []string{"read"},
				OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
			},
			Config: spec.ConfigSpec{
				Format: "toml",
				Path:   "~/.config/oauthapi-pp-cli/config.toml",
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

	tests := []struct {
		name         string
		mechanism    string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:      "default omits Google-specific params",
			mechanism: "",
			wantAbsent: []string{
				`"access_type":`,
				`"prompt":`,
				`params.Set("access_type"`,
				`params.Set("prompt"`,
			},
		},
		{
			name:      "scope mechanism appends to scopes",
			mechanism: "scope:offline",
			wantContains: []string{
				`scopes = append(scopes, "offline")`,
			},
			wantAbsent: []string{
				`params.Set("access_type"`,
				`"access_type":`,
			},
		},
		{
			name:      "query mechanism emits exact param",
			mechanism: "query:access_type=offline",
			wantContains: []string{
				`params.Set("access_type", "offline")`,
			},
			wantAbsent: []string{
				`"access_type":   {"offline"}`,
				`scopes = append(scopes,`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := baseSpec()
			apiSpec.Auth.RefreshTokenMechanism = tt.mechanism

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
			require.NoError(t, err)
			content := string(authGo)
			for _, want := range tt.wantContains {
				assert.Contains(t, content, want, "missing expected emission")
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, content, absent, "should not emit")
			}
		})
	}

	t.Run("client_credentials grant ignores mechanism", func(t *testing.T) {
		t.Parallel()
		apiSpec := baseSpec()
		apiSpec.Auth.OAuth2Grant = spec.OAuth2GrantClientCredentials
		apiSpec.Auth.AuthorizationURL = ""
		apiSpec.Auth.RefreshTokenMechanism = "scope:offline"

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
		content := string(authGo)
		assert.NotContains(t, content, `scopes = append(scopes, "offline")`,
			"client_credentials grant must not emit authorization_code refresh-token mechanism")
		assert.NotContains(t, content, `params.Set(`,
			"client_credentials grant uses a token-endpoint POST, not an authorization URL")
	})
}

// TestGeneratedOutput_READMEBearerTokenClaudeDesktop covers the
// bearer_token rendering inside the `## Use with Claude Desktop`
// section (the canonicalEnvVar-present branch). Restored after the
// earlier `## Use with Claude Code` section — and its dedicated test —
// were removed; the bearer_token MCP-config branches still need a
// regression test since neither the api_key nor the rich-auth golden
// fixtures exercise the bearer_token paths.
func TestGeneratedOutput_READMEBearerTokenClaudeDesktop(t *testing.T) {
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

	// canonicalEnvVar-present branch: step 3 of the install list asks
	// the user to fill in the env var name when Claude Desktop prompts.
	assert.Contains(t, content, "Fill in `BEARER_TOKEN` when Claude Desktop prompts you.",
		"bearer_token + canonical env var must render the step-3 prompt naming the env var")

	// Manual JSON config <details> emits the env var in the mcpServers block.
	assert.Contains(t, content, `"BEARER_TOKEN": "<your-key>"`,
		"bearer_token + canonical env var must render the env var in the Manual JSON config block")

	// Negative: the canonicalEnvVar-absent preamble must NOT appear when a
	// canonical env var is set (that branch is mutually exclusive).
	assert.NotContains(t, content, "Store your token first if you haven't:",
		"bearer_token + canonical env var must not emit the auth-set-token preamble")
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
	assert.Regexp(t, `\bBearerTokenRefreshedAt\s+time\.Time\b`, configContent)
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

func TestBearerRefreshSavedTokenCorrectsStaleEnvSource(t *testing.T) {
	cfg := &Config{AccessToken: "fresh-token", RefreshbearerPublicBearer: "stale-token", AuthSource: "env:REFRESHBEARER_PUBLIC_BEARER"}
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

// Composed apiKey + OAuth2 cc (ServiceTitan shape): the primary auth is the
// OAuth bearer, but the API also requires a per-call ST-App-Key header carried
// by a sibling apiKey scheme. AdditionalHeaders on AuthConfig drives three
// generator emissions: a Config struct field, an os.Getenv loader in Load(),
// and a req.Header.Set in the client request hot-path. Without all three, the
// composed-auth API returns 401 even with a valid bearer.
func TestGenerateComposedApiKeyPlusBearerEmitsAdditionalHeader(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "stcompose",
		Version: "0.1.0",
		BaseURL: "https://api.servicetitan.io",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://auth.servicetitan.io/connect/token",
			EnvVars:     []string{"ST_CLIENT_ID", "ST_CLIENT_SECRET"},
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "ST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true},
				{Name: "ST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
			},
			AdditionalHeaders: []spec.AdditionalAuthHeader{
				{
					Header: "ST-App-Key",
					In:     "header",
					Scheme: "apiKeyHeader",
					EnvVar: spec.AuthEnvVar{
						Name:      "ST_APP_KEY",
						Kind:      spec.AuthEnvVarKindPerCall,
						Required:  true,
						Sensitive: true,
					},
				},
			},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/stcompose-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"customers": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/customers"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configSrc := string(configBytes)
	assert.Regexp(t, `StAppKey\s+string`, configSrc,
		"Config struct must carry a field for the sibling apiKey env var")
	assert.Contains(t, configSrc, `os.Getenv("ST_APP_KEY")`,
		"Load() must read ST_APP_KEY from env")
	assert.Contains(t, configSrc, `cfg.StAppKey = v`,
		"Load() must assign ST_APP_KEY into the Config field")

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientSrc := string(clientBytes)
	assert.Contains(t, clientSrc, `req.Header.Set("ST-App-Key", v)`,
		"client must set ST-App-Key on every outbound request when configured")

	doctorBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctorSrc := string(doctorBytes)
	assert.Contains(t, doctorSrc, `recordAdditionalAuthEnv("ST_APP_KEY", configuredValue)`,
		"doctor must check the sibling apiKey env var")
	assert.Contains(t, doctorSrc, `configuredValue = cfg.StAppKey`,
		"doctor must accept sibling apiKey credentials from config files")
	assert.Contains(t, doctorSrc, `} else if authConfigured {`,
		"doctor must apply the config fallback consistently for sibling apiKey credentials")
	assert.Contains(t, doctorSrc, `OK %d/%d available", len(authEnvSet), 3`,
		"doctor must include sibling apiKey credentials in the env-var count")

	agentContextBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "agent_context.go"))
	require.NoError(t, err)
	agentContextSrc := string(agentContextBytes)
	assert.Contains(t, agentContextSrc, `"ST_APP_KEY"`,
		"agent-context must expose sibling apiKey credentials to agents")

	mcpBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	mcpSrc := string(mcpBytes)
	assert.Contains(t, mcpSrc, `"ST_APP_KEY"`,
		"MCP context must expose sibling apiKey credentials to agents")
}

// Trello-shaped OpenAPI specs require two apiKey query credentials in the
// same security requirement. The first key follows the primary auth path; the
// sibling token must be loaded into config, counted by doctor, and attached to
// the request URL as its own query parameter.
func TestGenerateComposedQueryApiKeysEmitAdditionalQueryParam(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Trello Auth
  version: "1.0.0"
servers:
  - url: https://api.trello.com/1
security:
  - APIKey: []
    APIToken: []
components:
  securitySchemes:
    APIKey:
      type: apiKey
      in: query
      name: key
      x-auth-env-vars:
        - TRELLO_API_KEY
    APIToken:
      type: apiKey
      in: query
      name: token
      x-auth-env-vars:
        - TRELLO_TOKEN
paths:
  /members/me:
    get:
      operationId: getMe
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)
	apiSpec.Name = "trelloauth"
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/trelloauth-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configSrc := string(configBytes)
	assert.Regexp(t, `TrelloToken\s+string`, configSrc,
		"Config struct must carry a field for the sibling query apiKey env var")
	assert.Contains(t, configSrc, `os.Getenv("TRELLO_TOKEN")`,
		"Load() must read TRELLO_TOKEN from env")
	assert.Contains(t, configSrc, `cfg.TrelloToken = v`,
		"Load() must assign TRELLO_TOKEN into the Config field")

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientSrc := string(clientBytes)
	assert.Contains(t, clientSrc, `q.Set("key", authHeader)`,
		"client must keep setting the primary API key query parameter")
	assert.Contains(t, clientSrc, `q.Set("token", v)`,
		"client must set the sibling token query parameter on every outbound request")
	assert.NotContains(t, clientSrc, `req.Header.Set("token", v)`,
		"sibling query credentials must not be emitted as headers")

	doctorBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctorSrc := string(doctorBytes)
	assert.Contains(t, doctorSrc, `recordAdditionalAuthEnv("TRELLO_TOKEN", configuredValue)`,
		"doctor must check the sibling query apiKey env var")
	assert.Contains(t, doctorSrc, `OK %d/%d available", len(authEnvSet), 2`,
		"doctor must include sibling query apiKey credentials in the env-var count")

	clientTest := `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trelloauth-pp-cli/internal/config"
)

func TestGeneratedClientSendsBothQueryCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("key"); got != "key-value" {
			t.Fatalf("key query param = %q, want key-value", got)
		}
		if got := query.Get("token"); got != "token-value" {
			t.Fatalf("token query param = %q, want token-value", got)
		}
		w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:      server.URL,
		TrelloApiKey: "key-value",
		TrelloToken:  "token-value",
	}
	c := New(cfg, 5*time.Second, 0)
	if _, err := c.Get(context.Background(), "/members/me", nil); err != nil {
		t.Fatal(err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "additional_query_auth_test.go"), []byte(clientTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestGeneratedClientSendsBothQueryCredentials")
}

// OAuth2 client_credentials specs without a sibling apiKey scheme must not
// emit the additional-header block. Guards against the previous emission
// regressing into "every OAuth CLI now ships a useless extra Config field".
func TestGenerateOAuth2WithoutAdditionalHeadersIsClean(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "ccclean",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://api.example.com/oauth/token",
			EnvVars:     []string{"CCCLEAN_CLIENT_ID", "CCCLEAN_CLIENT_SECRET"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/ccclean-pp-cli/config.toml"},
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

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(clientBytes), "Composed-scheme per-call headers",
		"specs without sibling per_call schemes must not emit the additional-header block")
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

func TestGenerateOAuth2DeviceCodeAuth(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "deviceauth",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:                   "bearer_token",
			Header:                 "Authorization",
			Format:                 "Bearer {token}",
			OAuth2Grant:            spec.OAuth2GrantDeviceCode,
			DeviceAuthorizationURL: "https://login.example.com/device",
			TokenURL:               "https://login.example.com/token",
			Scopes:                 []string{"mail.read", "calendars.read"},
			DefaultClientID:        "public-client-id",
			EnvVars:                []string{"DEVICEAUTH_CLIENT_ID"},
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "DEVICEAUTH_CLIENT_ID",
				Kind:      spec.AuthEnvVarKindAuthFlowInput,
				Required:  false,
				Sensitive: false,
			}},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/deviceauth-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	authGo := string(readGeneratedFile(t, outputDir, "internal", "cli", "auth.go"))
	oauthGo := string(readGeneratedFile(t, outputDir, "internal", "oauth", "device.go"))
	configGo := string(readGeneratedFile(t, outputDir, "internal", "config", "config.go"))
	clientGo := string(readGeneratedFile(t, outputDir, "internal", "client", "client.go"))

	assert.Contains(t, authGo, `newAuthRefreshCmd(flags)`, "device-code template exposes auth refresh")
	assert.Contains(t, authGo, `newAuthPollCmd(flags)`, "device-code template exposes non-interactive follow-up polling")
	assert.Contains(t, authGo, `cmd.Flags().BoolVar(&deviceCode, "device-code", true`, "auth login accepts --device-code")
	assert.Contains(t, authGo, `cmd.Flags().BoolVar(&poll, "poll", outputIsTerminal()`, "headless login should print and return instead of blocking")
	assert.Contains(t, authGo, `savePendingDeviceCode(cfg, state)`, "headless login stores pending device_code locally")
	assert.Contains(t, authGo, `os.WriteFile(path, data, 0o600)`, "pending device_code state must be user-only readable")
	assert.Contains(t, authGo, `_ = clearPendingDeviceCode(cfg)`, "logout and completed poll must remove pending device_code state")
	assert.Contains(t, authGo, `fmt.Fprintln(w, "  deviceauth-pp-cli auth poll")`, "headless login resumes without exposing the raw device_code")
	assert.NotContains(t, authGo, `auth poll --device-code`, "raw device_code must not be printed into shell commands")
	assert.Contains(t, authGo, `cfg.AuthHeaderVal = ""`, "OAuth token saves must clear stale manual auth headers")
	assert.Contains(t, authGo, `deviceAuthorizationURL := cfg.DeviceAuthorizationURL`, "login honors device endpoint overrides")
	assert.Contains(t, authGo, `"https://login.example.com/device"`, "login falls back to the configured device endpoint")
	assert.Contains(t, authGo, `"public-client-id"`, "login uses the spec default client id")
	assert.Contains(t, authGo, `os.Getenv("DEVICEAUTH_CLIENT_ID")`, "client id can be overridden by env var")
	assert.Contains(t, authGo, `cliutil.IsVerifyEnv()`, "login and refresh short-circuit in verify mode")
	assert.Contains(t, authGo, `Annotations: map[string]string{"mcp:hidden": "true"}`, "interactive auth commands stay out of MCP")
	assert.Contains(t, oauthGo, `const DeviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"`)
	assert.Contains(t, oauthGo, `case "slow_down":`, "poller handles slow_down backoff")
	assert.Contains(t, configGo, `if c.AccessToken != ""`, "config auth uses stored OAuth access token")
	assert.Contains(t, configGo, `DeviceAuthorizationURL string`, "config supports device endpoint overrides")
	assert.Contains(t, configGo, `DEVICEAUTH_DEVICE_AUTHORIZATION_URL`, "device endpoint can be overridden by env var")
	assert.NotContains(t, configGo, `return "Bearer " + c.DeviceauthClientId`, "client id must not be sent as bearer token")
	assert.Contains(t, clientGo, `"grant_type":    {"refresh_token"}`, "client auto-refresh uses refresh_token grant")
	assert.Contains(t, clientGo, `resp.StatusCode == http.StatusUnauthorized`, "client retries once after refreshing on 401")
	assert.Contains(t, clientGo, `!(cliutil.IsVerifyEnv() && !cliutil.IsVerifyLiveHTTPEnv())`, "verify mode must not refresh against a live token endpoint")

	oauthTest := `package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceCodeHelpers(t *testing.T) {
	var tokenPolls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		switch r.URL.Path {
		case "/device":
			if got := r.Form.Get("client_id"); got != "public-client" {
				t.Fatalf("client_id = %q", got)
			}
			if got := r.Form.Get("scope"); got != "mail.read calendars.read" {
				t.Fatalf("scope = %q", got)
			}
			fmt.Fprint(w, ` + "`" + `{"device_code":"device-123","user_code":"USER-123","verification_uri":"https://login.example.com/device","interval":1}` + "`" + `)
		case "/token":
			switch r.Form.Get("grant_type") {
			case DeviceCodeGrant:
				tokenPolls++
				if got := r.Form.Get("device_code"); got != "device-123" {
					t.Fatalf("device_code = %q", got)
				}
				if tokenPolls == 1 {
					http.Error(w, ` + "`" + `{"error":"authorization_pending"}` + "`" + `, http.StatusBadRequest)
					return
				}
				fmt.Fprint(w, ` + "`" + `{"access_token":"access-123","refresh_token":"refresh-123","expires_in":3600}` + "`" + `)
			case "refresh_token":
				if got := r.Form.Get("refresh_token"); got != "refresh-123" {
					t.Fatalf("refresh_token = %q", got)
				}
				fmt.Fprint(w, ` + "`" + `{"access_token":"access-456","refresh_token":"refresh-456","expires_in":1800}` + "`" + `)
			default:
				t.Fatalf("unexpected grant_type %q", r.Form.Get("grant_type"))
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	device, err := RequestDeviceCode(context.Background(), srv.Client(), srv.URL+"/device", "public-client", []string{"mail.read", "calendars.read"})
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if device.DeviceCode != "device-123" || device.UserCode != "USER-123" {
		t.Fatalf("device response = %+v", device)
	}

	if _, retry, err := pollDeviceTokenOnce(context.Background(), srv.Client(), srv.URL+"/token", "public-client", device.DeviceCode); err == nil || retry != "authorization_pending" {
		t.Fatalf("first poll err = %v retry = %q", err, retry)
	}
	tok, retry, err := pollDeviceTokenOnce(context.Background(), srv.Client(), srv.URL+"/token", "public-client", device.DeviceCode)
	if err != nil || retry != "" || tok.AccessToken != "access-123" || tok.RefreshToken != "refresh-123" {
		t.Fatalf("second poll token = %+v retry = %q err = %v", tok, retry, err)
	}
	refreshed, err := RefreshToken(context.Background(), srv.Client(), srv.URL+"/token", "public-client", "", tok.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshed.AccessToken != "access-456" || refreshed.RefreshToken != "refresh-456" {
		t.Fatalf("refreshed token = %+v", refreshed)
	}
}

func TestDeviceCodeHTTPErrorBodiesAreSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") == DeviceCodeGrant {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, ` + "`" + `{"error":"invalid_grant","error_description":"device %s for client %s was rejected"}` + "`" + `, r.Form.Get("device_code"), r.Form.Get("client_id"))
			return
		}
		http.Error(w, ` + "`" + `{"error":"invalid_client","detail":"Bearer SECRET1234"}` + "`" + `, http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := RequestDeviceCode(context.Background(), srv.Client(), srv.URL, "public-client", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "Bearer SECRET1234") {
		t.Fatalf("error leaked credential: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error was not sanitized: %v", err)
	}

	_, _, err = pollDeviceTokenOnce(context.Background(), srv.Client(), srv.URL, "public-client", "device-secret")
	if err == nil {
		t.Fatal("expected poll error")
	}
	if strings.Contains(err.Error(), "device-secret") || strings.Contains(err.Error(), "public-client") {
		t.Fatalf("structured OAuth error leaked request values: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "oauth", "device_test.go"), []byte(oauthTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./...")
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

func TestGenerateHTTPBasicAuthHeaderEncodesUsernamePassword(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "twilio",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			In:      "header",
			Header:  "Authorization",
			Format:  "Basic {username}:{password}",
			EnvVars: []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN"},
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "TWILIO_ACCOUNT_SID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
				{Name: "TWILIO_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/twilio-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Manage accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/Accounts",
						Description: "List accounts",
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

func TestBasicAuthHeader(t *testing.T) {
	cfg := &Config{TwilioAccountSid: "AC123", TwilioAuthToken: "secret"}
	if got := cfg.AuthHeader(); got != "Basic QUMxMjM6c2VjcmV0" {
		t.Fatalf("AuthHeader() = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "basic_auth_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config")
}

func TestGenerateHTTPBasicAuthHeaderEncodesSingleCredential(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "basic-single",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			In:      "header",
			Header:  "Authorization",
			Format:  "Basic {token}:",
			EnvVars: []string{"BASIC_TOKEN"},
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "BASIC_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/basic-single/config.toml",
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

func TestSingleCredentialBasicAuthHeader(t *testing.T) {
	cfg := &Config{BasicToken: "secret"}
	if got := cfg.AuthHeader(); got != "Basic c2VjcmV0Og==" {
		t.Fatalf("AuthHeader() = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "basic_auth_single_test.go")
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

func runGeneratedBinary(t *testing.T, binaryPath string, args ...string) (string, string) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return stdout.String(), stderr.String()
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
	assert.Contains(t, string(clientGo), "ForceHTTP2()")
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
	// surf's Chrome impersonation manages the Accept header alongside
	// User-Agent on the H3 transport too; the generator must not emit a
	// competing default.
	assert.NotContains(t, string(clientGo), `req.Header.Set("Accept",`)

	// ResponseHeaderTimeout override must emit on the H3 path too —
	// surf's per-stage timeout (10s default) caps any browser-impersonate
	// transport regardless of the H2/H3 variant. Without these asserts a
	// future refactor could silently strip the override from the H3
	// branch and slow-streaming H3 endpoints would fail at surf's default
	// with no test catching it. Mirrors the assertions in
	// TestBrowserTransport_OverridesResponseHeaderTimeout (which exercises
	// the H2 default).
	assert.Contains(t, string(clientGo), `enetxhttp "github.com/enetx/http"`)
	assert.Contains(t, string(clientGo), "surfClient.GetTransport().(*enetxhttp.Transport)")
	assert.Contains(t, string(clientGo), "t.ResponseHeaderTimeout = timeout")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/client")
}

// TestGenerateBrowserChromeH2Transport pins the explicit
// browser-chrome-h2 enum: the client emits ForceHTTP2() and no
// ForceHTTP3(). Separate from the bare browser-chrome case (no version
// force) so a future refactor cannot collapse the two without a failing
// test.
func TestGenerateBrowserChromeH2Transport(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:          "websurfaceh2",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChromeH2,
		Auth:          spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/websurfaceh2-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/", Description: "List posts"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "websurfaceh2-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.Contains(t, string(clientGo), "ForceHTTP2()")
	assert.NotContains(t, string(clientGo), "ForceHTTP3()")
}

// TestGenerateBrowserChromeNoVersionForce pins the bare browser-chrome
// enum (no -h2 / -h3 suffix): the surf client is used but no
// ForceHTTPN() call is emitted, so Chrome's negotiated version wins.
// Operators who want an explicit H/2 force must set browser-chrome-h2.
func TestGenerateBrowserChromeNoVersionForce(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:          "websurfacenoforce",
		Version:       "0.1.0",
		BaseURL:       "https://www.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChrome,
		Auth:          spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/websurfacenoforce-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/", Description: "List posts"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "websurfacenoforce-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.NotContains(t, string(clientGo), "ForceHTTP2()")
	assert.NotContains(t, string(clientGo), "ForceHTTP3()")
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

// TestGenerateCookieAuthEmitsSetTokenSubcommand verifies that the
// auth_browser template emits the `set-token` subcommand for cookie-auth
// CLIs. The subcommand is the escape hatch for Auth0-SPA-in-memory sites
// (retro #1598 WU-3) where `auth login --chrome` can't reach the JWT, so
// users (or agents) paste the bearer from DevTools instead of hand-editing
// config.toml. Validation runs through cliutil.LooksLikeJWT (retro #1598
// WU-2) so short Cloudflare-shaped tracking cookies don't silently land
// as access tokens.
func TestGenerateCookieAuthEmitsSetTokenSubcommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("settokencookie")
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		EnvVars:      []string{"SETTOKENCOOKIE_COOKIES"},
	}

	outputDir := filepath.Join(t.TempDir(), "settokencookie-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authGo, "func newAuthSetTokenCmd(",
		"cookie-auth CLI should emit newAuthSetTokenCmd")
	assert.Contains(t, authGo, "cmd.AddCommand(newAuthSetTokenCmd(flags))",
		"newAuthCmd should register the set-token subcommand")
	assert.Contains(t, authGo, "cliutil.LooksLikeJWT(",
		"set-token should validate via the shared cliutil JWT-shape helper")
	assert.Contains(t, authGo, "settokencookie-pp-cli/internal/cliutil",
		"auth.go should import cliutil to call LooksLikeJWT")

	// Help wiring sanity: build the CLI and confirm `auth set-token --help`
	// exits 0 with the subcommand listed under `auth`.
	runGoCommand(t, outputDir, "mod", "tidy")
	binPath := filepath.Join(outputDir, "settokencookie-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binPath, "./cmd/settokencookie-pp-cli")
	out, err := exec.Command(binPath, "auth", "--help").CombinedOutput()
	require.NoError(t, err, "auth --help failed: %s", string(out))
	assert.Contains(t, string(out), "set-token",
		"set-token should appear in `auth --help` listing")
}

// TestGenerateNoAuthPersistedQueryOmitsSetToken verifies that the
// auth_browser template does NOT emit set-token (or import cliutil) when
// Auth.Type == "none" + a graphql_persisted_query hint routes the spec
// through auth_browser purely for the query-refresh flow. Saving a token
// has no meaning when there are no credentials to save; emitting the
// subcommand would also produce an unused cliutil import and break build.
func TestGenerateNoAuthPersistedQueryOmitsSetToken(t *testing.T) {
	t.Parallel()

	// auth.Type "none" alone wouldn't route to auth_browser, so the
	// generator's persisted-query traffic-analysis hint pulls it into the
	// browser-aware template purely for the query-refresh flow.
	apiSpec := minimalSpec("noauthpq")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}

	outputDir := filepath.Join(t.TempDir(), "noauthpq-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.NotContains(t, authGo, "newAuthSetTokenCmd",
		"auth.type=none should not emit a set-token subcommand")
	assert.NotContains(t, authGo, "cliutil.LooksLikeJWT",
		"auth.type=none should not reference the JWT-shape helper")
	assert.NotContains(t, authGo, "internal/cliutil\"",
		"auth.type=none must not import cliutil — would trigger unused-import")
}

func TestGenerateCookieHTMLDefaultsBrowserChromeTransport(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cookiehtml")
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		EnvVars:      []string{"COOKIEHTML_COOKIES"},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"diary": {
			Description: "Read diary pages",
			Endpoints: map[string]spec.Endpoint{
				"get_day": {
					Method:         "GET",
					Path:           "/food/diary",
					Description:    "Read a diary page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract: &spec.HTMLExtract{
						Mode: spec.HTMLExtractModePage,
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cookiehtml-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	gomod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, gomod, "github.com/enetx/surf")

	clientGo := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientGo, `"github.com/enetx/surf"`)
	assert.Contains(t, clientGo, "Impersonate()")
	assert.Contains(t, clientGo, "Chrome()")
	assert.NotContains(t, clientGo, `req.Header.Set("User-Agent", "cookiehtml-pp-cli/0.1.0")`)
}

func TestGenerateHTMLExtractionEndpoint(t *testing.T) {
	shiftJISHTML, err := japanese.ShiftJIS.NewEncoder().String(`<html><body><a href="/products/tokyo">東京</a></body></html>`)
	require.NoError(t, err)

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
		case "/latin1":
			w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
			_, _ = w.Write([]byte("<html><body><a href=\"/products/cafe\">Caf\xe9</a></body></html>"))
		case "/shift-jis":
			w.Header().Set("Content-Type", "text/html; charset=Shift_JIS")
			_, _ = w.Write([]byte(shiftJISHTML))
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
			"latin": {
				Description: "Browse Latin-1 pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/latin1",
						Description:    "Fetch Latin-1 links",
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
			"shiftjis": {
				Description: "Browse Shift-JIS pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/shift-jis",
						Description:    "Fetch Shift-JIS links",
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
		},
	}

	outputDir := filepath.Join(t.TempDir(), "webhtml-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "html_extract.go"))
	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "golang.org/x/net v0.55.0")

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

	cmd = exec.Command(binaryPath, "latin", "list", "--json")
	cmd.Env = append(os.Environ(), "WEBHTML_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	assert.Equal(t, "Caf\u00e9", envelope.Results[0]["name"])

	shiftJISEnv := append(os.Environ(), "WEBHTML_BASE_URL="+server.URL, "HOME="+t.TempDir())
	cmd = exec.Command(binaryPath, "shiftjis", "list", "--json")
	cmd.Env = shiftJISEnv
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	assert.Equal(t, "東京", envelope.Results[0]["name"])

	server.Close()
	cmd = exec.Command(binaryPath, "shiftjis", "list", "--json")
	cmd.Env = shiftJISEnv
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	assert.Equal(t, "東京", envelope.Results[0]["name"], "cached HTML responses must preserve the HTTP charset hint")
}

func TestGeneratedSyncHonorsHTMLExtraction(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "htmlsync",
		Version: "0.1.0",
		BaseURL: "https://example.test",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/htmlsync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"pages": {
				Description: "HTML pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/pages",
						Description:    "List pages from HTML",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:         spec.HTMLExtractModeLinks,
							LinkPrefixes: []string{"/pages"},
							Limit:        10,
						},
						Response: spec.ResponseDef{Type: "array", Item: "html_link"},
					},
				},
			},
			"json_pages": {
				Description: "JSON pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/json-pages",
						Description: "List pages from JSON",
						Response:    spec.ResponseDef{Type: "array", Item: "object"},
					},
				},
			},
			"channels": {
				Description: "Channel records",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/channels",
						Description: "List channels",
						Response:    spec.ResponseDef{Type: "array", Item: "object"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
			"messages": {
				Description: "HTML messages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/channels/{channelId}/messages",
						Description:    "List messages from HTML",
						ResponseFormat: spec.ResponseFormatHTML,
						Params:         []spec.Param{{Name: "channelId", Type: "string", Required: true, PathParam: true}},
						Pagination:     &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						HTMLExtract: &spec.HTMLExtract{
							Mode:           spec.HTMLExtractModeEmbeddedJSON,
							ScriptSelector: "script#MESSAGES_DATA",
							JSONPath:       "items",
						},
						Response: spec.ResponseDef{Type: "array", Item: "object"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "syncHTMLExtractionOptions",
		"sync.go must route HTML sync responses through extractHTMLResponse before extractPageItems")
	assert.Contains(t, syncSrc, `case "pages":`,
		"HTML extraction options must be emitted for the HTML sync resource")
	assert.Contains(t, syncSrc, `case "channels.messages":`,
		"dependent HTML extraction options must be keyed by parent and child resource")
	assert.NotContains(t, syncSrc, `case "json_pages":`,
		"JSON sync resources must not get HTML extraction options")

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type htmlSyncClient struct{}

func (htmlSyncClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	return json.RawMessage(` + "`" + `<html><body><a href="/pages/alpha">Alpha</a><a href="/pages/beta">Beta</a><a href="/other">Other</a></body></html>` + "`" + `), nil
}

func (htmlSyncClient) RequestBaseURL() string {
	return "https://example.test"
}

func (htmlSyncClient) LastContentType() string {
	return "text/html; charset=utf-8"
}

func (htmlSyncClient) RateLimit() float64 {
	return 0
}

type dependentHTMLSyncClient struct{}

func (dependentHTMLSyncClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if strings.Contains(path, "/bad/messages") {
		return json.RawMessage(` + "`" + `<html><body>missing embedded JSON</body></html>` + "`" + `), nil
	}
	return json.RawMessage(` + "`" + `<html><body><script id="MESSAGES_DATA" type="application/json">{"items":[{"id":"msg-good","text":"ok"}]}</script></body></html>` + "`" + `), nil
}

func (dependentHTMLSyncClient) RequestBaseURL() string {
	return "https://example.test"
}

func (dependentHTMLSyncClient) LastContentType() string {
	return "text/html; charset=utf-8"
}

func (dependentHTMLSyncClient) RateLimit() float64 {
	return 0
}

func TestSyncResourceExtractsHTMLLinks(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	res := syncResource(context.Background(), htmlSyncClient{}, db, "pages", "", false, 1, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Count != 2 {
		t.Fatalf("synced count = %d, want 2", res.Count)
	}

	rows, err := db.List("pages", 10)
	if err != nil {
		t.Fatalf("list pages: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored rows = %d, want 2", len(rows))
	}
	body := string(rows[0]) + "\n" + string(rows[1])
	if !strings.Contains(body, "\"slug\":\"alpha\"") || !strings.Contains(body, "\"slug\":\"beta\"") {
		t.Fatalf("stored rows did not include extracted HTML links: %s", body)
	}
}

func TestSyncDependentResourceContinuesAfterHTMLExtractionError(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("channels", "bad", []byte(` + "`" + `{"id":"bad"}` + "`" + `)); err != nil {
		t.Fatalf("insert bad parent: %v", err)
	}
	if err := db.Upsert("channels", "good", []byte(` + "`" + `{"id":"good"}` + "`" + `)); err != nil {
		t.Fatalf("insert good parent: %v", err)
	}

	res := syncDependentResource(
		context.Background(),
		dependentHTMLSyncClient{},
		db,
		dependentResourceDef{Name: "messages", ParentTable: "channels", ParentIDParam: "channelId", PathTemplate: "/channels/{channelId}/messages"},
		"", false, 1, false, nil, nil,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 1 {
		t.Fatalf("synced count = %d, want 1", res.Count)
	}

	rows, err := db.List("messages", 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(rows))
	}
	body := string(rows[0])
	if !strings.Contains(body, "\"id\":\"msg-good\"") || !strings.Contains(body, "\"channels_id\":\"good\"") {
		t.Fatalf("stored rows did not include the successful parent message with its parent FK: %s", body)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_html_extract_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSync(ResourceExtractsHTMLLinks|DependentResourceContinuesAfterHTMLExtractionError)", "./internal/cli")
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
		case "/jsonld":
			// JSON-LD structured data uses an attribute selector emitted by
			// browser sniffing for schema.org SSR pages.
			_, _ = w.Write([]byte(`<html><body>
				<script type="application/ld+json">{"@type":"Restaurant","name":"El Farolito","servesCuisine":["Tacos","Mexicana"]}</script>
			</body></html>`))
		case "/state-view":
			// Some SSR pages expose state in a classed script tag.
			_, _ = w.Write([]byte(`<html><body>
				<script class="state-view" type="application/json">{"items":[{"slug":"state-a"}]}</script>
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
			"jsonld": {
				Description: "Read schema.org JSON-LD",
				Endpoints: map[string]spec.Endpoint{
					"show": {
						Method:         "GET",
						Path:           "/jsonld",
						Description:    "Read structured data",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:           spec.HTMLExtractModeEmbeddedJSON,
							ScriptSelector: `script[type="application/ld+json"]`,
						},
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"stateview": {
				Description: "Read classed state view JSON",
				Endpoints: map[string]spec.Endpoint{
					"show": {
						Method:         "GET",
						Path:           "/state-view",
						Description:    "Read state view data",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:           spec.HTMLExtractModeEmbeddedJSON,
							ScriptSelector: "script.state-view",
						},
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"badattr": {
				Description: "Reject unsupported attribute selectors",
				Endpoints: map[string]spec.Endpoint{
					"show": {
						Method:         "GET",
						Path:           "/jsonld",
						Description:    "Read structured data with an invalid selector",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract: &spec.HTMLExtract{
							Mode:           spec.HTMLExtractModeEmbeddedJSON,
							ScriptSelector: "script[async]",
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
	// The extracted shape `{"items":[...]}` is a single-key wrapper that
	// wrapWithProvenance unwraps, so the envelope's `results` is the
	// inner array — consistent .results[] shape across APIs.
	cmd = exec.Command(binaryPath, "articles", "list", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var articleEnv struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &articleEnv), string(out))
	require.Len(t, articleEnv.Results, 2)
	assert.Equal(t, "a", articleEnv.Results[0]["slug"])
	assert.Equal(t, "b", articleEnv.Results[1]["slug"])

	// Attribute selector: returns schema.org JSON-LD blocks such as restaurants,
	// products, and recipes.
	cmd = exec.Command(binaryPath, "jsonld", "show", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var jsonLDEnv struct {
		Results map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &jsonLDEnv), string(out))
	assert.Equal(t, "Restaurant", jsonLDEnv.Results["@type"])
	assert.Equal(t, "El Farolito", jsonLDEnv.Results["name"])

	// Class selector: returns state blobs emitted as classed script tags.
	cmd = exec.Command(binaryPath, "stateview", "show", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var stateViewEnv struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &stateViewEnv), string(out))
	require.Len(t, stateViewEnv.Results, 1)
	assert.Equal(t, "state-a", stateViewEnv.Results[0]["slug"])

	// Unsupported attribute-existence selectors should fail explicitly instead
	// of silently degrading to a broad tag-only match.
	cmd = exec.Command(binaryPath, "badattr", "show", "--json")
	cmd.Env = append(os.Environ(), "EMBEDDEDJSON_BASE_URL="+server.URL)
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	assert.Contains(t, string(out), "embedded-json: invalid selector")
	assert.Contains(t, string(out), `tag[attr="value"]`)

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
	assert.Contains(t, string(readme), "npx -y @mvanhorn/printing-press-library install owned")
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

// Callers gating on existence rely on errors.Is(err, sql.ErrNoRows); the
// emitted Store.Get must surface the sentinel rather than swallow it into
// a nil-shape that bypasses the caller's err check.
func TestGenerateStoreGetPropagatesErrNoRows(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("errnorows-canary")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeCode := stripGoComments(string(storeSrc))

	assert.NotRegexp(t, `(?s)func \(s \*Store\) Get\([^)]*\) \([^)]*\) \{[^}]*sql\.ErrNoRows[^}]*return nil, nil`, storeCode,
		"Store.Get must propagate sql.ErrNoRows; callers gating on existence rely on errors.Is(err, sql.ErrNoRows)")
	assert.Regexp(t, `(?s)func \(s \*Store\) Get\([^)]*\) \([^)]*\) \{[^}]*return nil, err`, storeCode,
		"Store.Get must surface the underlying error so sql.ErrNoRows reaches callers; a refactor that adds a nested block before the return would silently bypass the NotRegexp above")

	dataSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "data_source.go"))
	require.NoError(t, err)
	dataCode := stripGoComments(string(dataSrc))

	assert.Contains(t, dataCode, "errors.Is(err, sql.ErrNoRows)",
		"data_source.go must detect missing rows via errors.Is(err, sql.ErrNoRows)")
	assert.NotContains(t, dataCode, "if item == nil {",
		"data_source.go must not use (item == nil) to detect missing rows; Get returns sql.ErrNoRows instead")

	storeTestSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "schema_version_test.go"))
	require.NoError(t, err)
	assert.Contains(t, string(storeTestSrc), "func TestGet_MissingRowReturnsErrNoRows(",
		"template-level Get contract test must land in the emitted store package; dropping it would leave the runtime contract uncovered in printed CLIs")
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
	assert.Contains(t, src, `"features"`,
		"pageItemKeys must include features (GeoJSON support)")
	assert.Contains(t, src, `"Features"`,
		"pageItemKeys must include Features (PascalCase GeoJSON support)")

	// Tier 2: behavioral test, written into the generated tree as a
	// same-package _test.go so it can call the unexported helper.
	const inlineTest = `package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractPageItemsGeoJSON(t *testing.T) {
	body := []byte(` + "`" + `{
		"type": "FeatureCollection",
		"bbox": [-180, -90, 180, 90],
		"features": [{"id":"1"},{"id":"2"}]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("GeoJSON: want 2 items, got %d", len(items))
	}
}

func TestExtractPageItemsGeoJSONPascal(t *testing.T) {
	body := []byte(` + "`" + `{
		"Type": "FeatureCollection",
		"Bbox": [-180, -90, 180, 90],
		"Features": [{"Id":"1"},{"Id":"2"}]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("GeoJSON Pascal: want 2 items, got %d", len(items))
	}
}

func TestExtractPageItemsNonArrayFeaturesFallback(t *testing.T) {
	// features: true should fall through to ambiguity scan or return nil.
	// In this case, there are NO arrays, so it returns nil.
	body := []byte(` + "`" + `{
		"features": true,
		"other": "val"
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 0 {
		t.Fatalf("Non-array features: want 0 items, got %d", len(items))
	}
}

func TestExtractPageItemsFeaturesPrecedenceConflict(t *testing.T) {
	// Greptile feedback: when "features" is a valid array of non-objects
	// (e.g. strings) but another array (e.g. "markets") exists, the fast-path
	// for "features" will fail unmarshal (into []RawMessage) if the items
	// are not objects, and fall through to the ambiguity scan.
	//
	// However, if "features" IS an array of objects but they aren't the
	// resources we want, the fast-path WILL match. This test confirms
	// the behavior for non-object arrays.
	body := []byte(` + "`" + `{
		"markets": [{"id":"m1"}],
		"features": ["dark_mode", "beta"]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 1 {
		t.Fatalf("Features conflict: want 1 item (from markets), got %d. Result should fall through 'features' because strings are not RawMessage objects.", len(items))
	}
}

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

// TestSyncPageIntPaginationAdvancesAfterFullPage guards #1296: APIs that
// paginate by integer ?page=N (Freshworks, HubSpot, Atlassian, …) emit
// no body cursor, so extractPageItems returns ("", false). The runtime
// must detect the paginator type (not the raw param name) and advance
// the integer counter when the page is full, otherwise sync stops after
// page 1. Parametrized over every canonical page-int spelling so all
// four variants share the same guard regardless of casing.
func TestSyncPageIntPaginationAdvancesAfterFullPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paramName string
	}{
		{"plain page", "page"},
		{"snake_case page_number", "page_number"},
		{"camelCase pageNumber", "pageNumber"},
		{"json:api page[number]", "page[number]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := &spec.APISpec{
				Name:    "freshy",
				Version: "0.1.0",
				BaseURL: "https://freshy.example.com",
				Auth:    spec.AuthConfig{Type: "none"},
				Config: spec.ConfigSpec{
					Format: "toml",
					Path:   "~/.config/freshy-pp-cli/config.toml",
				},
				Resources: map[string]spec.Resource{
					"tickets": {
						Description: "Tickets",
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:      "GET",
								Path:        "/tickets",
								Description: "List tickets",
								Response:    spec.ResponseDef{Type: "array"},
								Params: []spec.Param{
									{Name: tc.paramName, Type: "integer"},
									{Name: "per_page", Type: "integer"},
								},
								Pagination: &spec.Pagination{
									Type:        "page",
									CursorParam: tc.paramName,
									LimitParam:  "per_page",
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

			// The guard must compare on cursorType so every canonical
			// page-int spelling (page, page_number, pageNumber,
			// page[number]) fires the fallback.
			assert.Contains(t, src, `pageSize.cursorType == "page"`,
				"sync.go must guard the page-int fallback on cursorType, not the raw param name")
			assert.Contains(t, src, `strconv.Itoa(currentPage + 1)`,
				"page-int fallback must increment the integer cursor")
			assert.Contains(t, src, `cursorType:  "page"`,
				"determinePaginationDefaults must emit cursorType \"page\" when the profile selects it")
			// CursorParam still carries the original-case param name so
			// the HTTP request key matches the spec.
			assert.Contains(t, src, `cursorParam: "`+tc.paramName+`"`,
				"determinePaginationDefaults must preserve the original-case page-int param name")
		})
	}
}

func TestSyncIDWalkPostQueryAdvancesAfterFullPage(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "post-query",
		Version: "0.1.0",
		BaseURL: "https://post-query.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/post-query-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"tickets": {
				Description: "Tickets",
				Endpoints: map[string]spec.Endpoint{
					"query": {
						Method:      "POST",
						Path:        "/tickets/query",
						Description: "Query tickets",
						IDField:     "id",
						Body: []spec.Param{
							{Name: "MaxRecords", Type: "integer", Default: 500},
							{Name: "filter", Type: "array"},
						},
						Pagination: &spec.Pagination{
							Type:       spec.PaginationTypeIDWalk,
							LimitParam: "MaxRecords",
						},
						Response: spec.ResponseDef{Type: "object", Item: "TicketsResponse"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"TicketsResponse": {
				Fields: []spec.TypeField{
					{Name: "items", Type: "array"},
					{Name: "pageDetails", Type: "object"},
				},
			},
			"Ticket": {
				Fields: []spec.TypeField{{Name: "id", Type: "integer"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	queryCmd := generatedCLISourceContaining(t, outputDir, `"/tickets/query"`)
	assert.NotContains(t, queryCmd, `"all", false, "Fetch all pages"`)
	assert.NotContains(t, queryCmd, "paginatedGet(")
	assert.NotContains(t, queryCmd, "resolvePaginatedRead(")
	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type postQuerySyncClient struct {
	bodies []map[string]any
}

func (c *postQuerySyncClient) Get(context.Context, string, map[string]string) (json.RawMessage, error) {
	return nil, fmt.Errorf("unexpected GET")
}

func (c *postQuerySyncClient) PostWithParams(_ context.Context, path string, params map[string]string, body any) (json.RawMessage, int, error) {
	return nil, 0, fmt.Errorf("unexpected mutating POST")
}

func (c *postQuerySyncClient) PostQueryWithParams(_ context.Context, path string, params map[string]string, body any) (json.RawMessage, int, error) {
	if path != "/tickets/query" {
		return nil, 0, fmt.Errorf("path = %s, want /tickets/query", path)
	}
	if len(params) != 0 {
		return nil, 0, fmt.Errorf("query params = %#v, want none", params)
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("body type = %T, want map[string]any", body)
	}
	c.bodies = append(c.bodies, cloneBody(bodyMap))

	start := (len(c.bodies) - 1) * 500
	count := 500
	if len(c.bodies) == 3 {
		count = 200
	}
	items := make([]map[string]any, 0, count)
	for i := 1; i <= count; i++ {
		id := start + i
		if count == 500 && i == 499 {
			id = start + 500
		} else if count == 500 && i == 500 {
			id = start + 499
		}
		items = append(items, map[string]any{"id": id, "summary": fmt.Sprintf("ticket-%d", id)})
	}
	data, _ := json.Marshal(map[string]any{
		"items": items,
		"pageDetails": map[string]any{"nextPageUrl": "https://post-query.example.com/tickets/query?page=ignored"},
	})
	return data, 200, nil
}

func (c *postQuerySyncClient) RateLimit() float64 {
	return 0
}

func cloneBody(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestSyncResourceIDWalksPostQueryPages(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/data.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	client := &postQuerySyncClient{}
	res := syncResource(context.Background(), client, db, "tickets", "", true, 10, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Count != 1200 {
		t.Fatalf("synced count = %d, want 1200", res.Count)
	}
	if len(client.bodies) != 3 {
		t.Fatalf("POST calls = %d, want 3", len(client.bodies))
	}
	assertIDWalkBody(t, client.bodies[0], nil)
	assertIDWalkBody(t, client.bodies[1], int64(500))
	assertIDWalkBody(t, client.bodies[2], int64(1000))
}

func TestNextIDWalkCursorPreservesLargeJSONNumber(t *testing.T) {
	got, ok := nextIDWalkCursor([]json.RawMessage{
		json.RawMessage(` + "`" + `{"id":9007199254740993}` + "`" + `),
	}, "id")
	if !ok {
		t.Fatal("nextIDWalkCursor did not find an id")
	}
	if got != "9007199254740993" {
		t.Fatalf("cursor = %s, want 9007199254740993", got)
	}
}

func assertIDWalkBody(t *testing.T, body map[string]any, wantAfter any) {
	t.Helper()
	if body["MaxRecords"] != int64(500) {
		t.Fatalf("MaxRecords = %#v, want int64(500)", body["MaxRecords"])
	}
	filters, ok := body["filter"].([]any)
	if wantAfter == nil {
		if ok && len(filters) > 0 {
			t.Fatalf("first request filter = %#v, want empty", filters)
		}
		return
	}
	if !ok || len(filters) != 1 {
		t.Fatalf("filter = %#v, want one id-walk predicate", body["filter"])
	}
	predicate, ok := filters[0].(map[string]any)
	if !ok {
		t.Fatalf("filter[0] = %#v, want object", filters[0])
	}
	if predicate["field"] != "id" || predicate["op"] != "gt" || predicate["value"] != wantAfter {
		t.Fatalf("filter[0] = %#v, want id gt %v", predicate, wantAfter)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "sync_id_walk_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncResourceIDWalksPostQueryPages", "./internal/cli")
}

func generatedCLISourceContaining(t *testing.T, outputDir string, needle string) string {
	t.Helper()
	var found string
	require.NoError(t, filepath.WalkDir(filepath.Join(outputDir, "internal", "cli"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" || found != "" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), needle) {
			found = string(data)
		}
		return nil
	}))
	require.NotEmpty(t, found, "generated CLI source containing %q", needle)
	return found
}

// TestGeneratedSyncHandlesPascalCaseDotNetShape verifies the generated sync +
// store paths recognize .NET-shape PascalCase envelopes ("Items"), PKs ("Id"),
// and field keys (LookupFieldValue PascalCase pass). Parser-side tier-5 PK
// selection is covered separately in internal/openapi/parser_test.go.
func TestGeneratedSyncHandlesPascalCaseDotNetShape(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "dotnet",
		Version: "0.1.0",
		BaseURL: "https://dotnet.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/dotnet-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/orders",
						Description: "List orders",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"encoding/json"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

func TestExtractPageItemsPascalCaseItemsEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"Items": [{"Id": "ord-1"}, {"Id": "ord-2"}],
		"TotalItems": 2
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("Items envelope: want 2 items, got %d", len(items))
	}
	var first map[string]any
	if err := json.Unmarshal(items[0], &first); err != nil {
		t.Fatalf("unmarshal item[0]: %v", err)
	}
	if first["Id"] != "ord-1" {
		t.Fatalf("want item[0].Id=ord-1, got %v", first["Id"])
	}
}

func TestExtractPageItemsPascalCaseDataEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"Data": [{"Id": "a"}, {"Id": "b"}, {"Id": "c"}]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 3 {
		t.Fatalf("Data envelope: want 3 items, got %d", len(items))
	}
	var third map[string]any
	if err := json.Unmarshal(items[2], &third); err != nil {
		t.Fatalf("unmarshal item[2]: %v", err)
	}
	if third["Id"] != "c" {
		t.Fatalf("want item[2].Id=c, got %v", third["Id"])
	}
}

func TestExtractPageItemsPascalCaseResultsEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"Results": [{"Id": "x"}]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 1 {
		t.Fatalf("Results envelope: want 1 item, got %d", len(items))
	}
	var only map[string]any
	if err := json.Unmarshal(items[0], &only); err != nil {
		t.Fatalf("unmarshal item[0]: %v", err)
	}
	if only["Id"] != "x" {
		t.Fatalf("want item[0].Id=x, got %v", only["Id"])
	}
}

// TestExtractPageItemsLowercaseTakesPriorityOverPascal pins the documented
// scan ordering: when both lowercase and PascalCase wrapper keys appear in
// the same response, the lowercase entry wins.
func TestExtractPageItemsLowercaseTakesPriorityOverPascal(t *testing.T) {
	body := []byte(` + "`" + `{
		"items": [{"id": "lower"}],
		"Items": [{"Id": "pascal"}]
	}` + "`" + `)
	items, _, _ := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 1 {
		t.Fatalf("want 1 item from lowercase envelope, got %d", len(items))
	}
	var first map[string]any
	if err := json.Unmarshal(items[0], &first); err != nil {
		t.Fatalf("unmarshal item[0]: %v", err)
	}
	if first["id"] != "lower" {
		t.Fatalf("lowercase must win: want lower, got %v", first["id"])
	}
}

func TestExtractPageItemsJSendNestedDataEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{
		"success": true,
		"warnings": [{"code": "slow-page"}],
		"next_cursor": "next-page",
		"has_more": true,
		"data": {
			"orders": [{"Id": "ord-1"}, {"Id": "ord-2"}]
		}
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 2 {
		t.Fatalf("nested data envelope: want 2 items, got %d", len(items))
	}
	if cursor != "next-page" || !hasMore {
		t.Fatalf("nested pagination = %q/%v, want next-page/true", cursor, hasMore)
	}

	body = []byte(` + "`" + `{
		"success": true,
		"data": {
			"orders": [{"Id": "ord-3"}],
			"has_more": true
		},
		"pagination": {"next_cursor": "outer-page"}
	}` + "`" + `)
	items, cursor, hasMore = extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 1 || cursor != "outer-page" || !hasMore {
		t.Fatalf("nested data with split pagination = %d/%q/%v, want 1/outer-page/true", len(items), cursor, hasMore)
	}
}

func TestExtractPageItemsDetailObjectWrappedInData(t *testing.T) {
	body := []byte(` + "`" + `{
		"data": {
			"CertNo": "91000",
			"line_items": [{"id": "li_1"}],
			"Grade": "MS64"
		}
	}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 0 || cursor != "" || hasMore {
		t.Fatalf("detail object with child array = %d/%q/%v, want empty cursorless page", len(items), cursor, hasMore)
	}

	nullSibling := json.RawMessage(` + "`" + `{
		"data": {
			"user": null,
			"recent_orders": [{"id": "o1"}]
		}
	}` + "`" + `)
	items, cursor, hasMore = extractPageItems(nullSibling, "cursor")
	if len(items) != 0 || cursor != "" || hasMore {
		t.Fatalf("detail object with null sibling and child array = %d/%q/%v, want empty cursorless page", len(items), cursor, hasMore)
	}
}

func TestExtractPageItemsJSendNullDataEnvelope(t *testing.T) {
	body := []byte(` + "`" + `{"success": false, "data": null}` + "`" + `)
	items, cursor, hasMore := extractPageItems(json.RawMessage(body), "cursor")
	if len(items) != 0 || cursor != "" || hasMore {
		t.Fatalf("null data envelope = %d/%q/%v, want empty cursorless page", len(items), cursor, hasMore)
	}
	if !isEmptyPageResponse(json.RawMessage(body)) {
		t.Fatalf("failed JSend null data envelope should be treated as an empty page")
	}

	withErrors := json.RawMessage(` + "`" + `{"success": false, "errors": [{"code": "bad"}], "data": null}` + "`" + `)
	items, cursor, hasMore = extractPageItems(withErrors, "cursor")
	if len(items) != 0 || cursor != "" || hasMore {
		t.Fatalf("failed JSend errors envelope = %d/%q/%v, want empty cursorless page", len(items), cursor, hasMore)
	}

	statusFail := json.RawMessage(` + "`" + `{"status": "fail", "data": null}` + "`" + `)
	if !isEmptyPageResponse(statusFail) {
		t.Fatalf("status=fail null data envelope should be treated as an empty page")
	}

	emptyResultSibling := json.RawMessage(` + "`" + `{"data": null, "result": {"orders": []}}` + "`" + `)
	if !isEmptyPageResponse(emptyResultSibling) {
		t.Fatalf("null data plus empty result sibling should be treated as an empty page")
	}

	pascalSuccessFalse := json.RawMessage(` + "`" + `{"Success": false, "Data": null}` + "`" + `)
	items, cursor, hasMore = extractPageItems(pascalSuccessFalse, "cursor")
	if len(items) != 0 || cursor != "" || hasMore {
		t.Fatalf("PascalCase failed JSend null data envelope = %d/%q/%v, want empty cursorless page", len(items), cursor, hasMore)
	}
	if !isEmptyPageResponse(pascalSuccessFalse) {
		t.Fatalf("PascalCase failed JSend null data envelope should be treated as an empty page")
	}

	pascalStatusFail := json.RawMessage(` + "`" + `{"Status": "Failed", "Data": null}` + "`" + `)
	if !isEmptyPageResponse(pascalStatusFail) {
		t.Fatalf("PascalCase status=Failed null data envelope should be treated as an empty page")
	}

	statusSuccess := json.RawMessage(` + "`" + `{"status": "success", "data": null}` + "`" + `)
	if isEmptyPageResponse(statusSuccess) {
		t.Fatalf("status=success null data envelope should not be treated as an empty page")
	}
}

func TestLookupFieldValuePascalCase(t *testing.T) {
	obj := map[string]any{"Id": "abc", "OrderTotal": float64(42)}

	if v := store.LookupFieldValue(obj, "id"); v != "abc" {
		t.Fatalf("LookupFieldValue id: want abc, got %v", v)
	}
	if v := store.LookupFieldValue(obj, "order_total"); v != float64(42) {
		t.Fatalf("LookupFieldValue order_total: want 42, got %v", v)
	}
}

func TestLookupFieldValueCamelCaseStillResolves(t *testing.T) {
	obj := map[string]any{"orderTotal": float64(7)}
	if v := store.LookupFieldValue(obj, "order_total"); v != float64(7) {
		t.Fatalf("camelCase: want 7, got %v", v)
	}
}

func TestLookupFieldValueSnakeCasePrecedence(t *testing.T) {
	obj := map[string]any{
		"order_total": float64(1),
		"orderTotal":  float64(2),
		"OrderTotal":  float64(3),
	}
	if v := store.LookupFieldValue(obj, "order_total"); v != float64(1) {
		t.Fatalf("snake_case must win: got %v", v)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "sync_pascalcase_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	const storeInlineTest = `package store

import "testing"

func TestExtractObjectIDPascalCaseId(t *testing.T) {
	if got := extractObjectID(map[string]any{"Id": "ord-1"}); got != "ord-1" {
		t.Fatalf("PascalCase Id: want ord-1, got %q", got)
	}
}

func TestExtractObjectIDLowercaseIdWinsOverPascal(t *testing.T) {
	if got := extractObjectID(map[string]any{"id": "lower", "Id": "pascal"}); got != "lower" {
		t.Fatalf("lowercase id must win over PascalCase Id: got %q", got)
	}
}

func TestExtractObjectIDPascalCaseIdWinsOverUppercaseID(t *testing.T) {
	if got := extractObjectID(map[string]any{"ID": "upper", "Id": "pascal"}); got != "pascal" {
		t.Fatalf("PascalCase Id must precede uppercase ID: got %q", got)
	}
}
`
	storeTestPath := filepath.Join(outputDir, "internal", "store", "store_pascalcase_test.go")
	require.NoError(t, os.WriteFile(storeTestPath, []byte(storeInlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestExtractPageItems(PascalCase|JSend)|TestLookupFieldValue", "./internal/cli")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestExtractObjectID", "./internal/store")
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
	// The dispatch now captures the typed error so a savepoint can roll it
	// back without unwinding the outer transaction (issue #1392); the
	// shape went from `if err := s.upsertCampaignsTx(...)` to
	// `typedErr = s.upsertCampaignsTx(...)`.
	assert.Regexp(t, `(?s)func \(s \*Store\) UpsertBatch\(.*case "campaigns":\s+typedErr = s\.upsertCampaignsTx\(`, src,
		"UpsertBatch must dispatch to upsertCampaignsTx — without this, paginated syncs leave typed tables empty (issue #268)")

	// SAVEPOINT/RELEASE/ROLLBACK isolates typed-table failures so a NOT NULL
	// constraint in a generated typed table does not unwind the outer
	// transaction and strand the generic resources row (issue #1392).
	assert.Contains(t, src, `tx.Exec("SAVEPOINT " + savepoint)`,
		"UpsertBatch must open a SAVEPOINT before the typed dispatch (issue #1392)")
	assert.Contains(t, src, `tx.Exec("ROLLBACK TO SAVEPOINT " + savepoint)`,
		"UpsertBatch must ROLLBACK TO SAVEPOINT on typed-table failure so the generic row survives (issue #1392)")
	assert.Contains(t, src, `tx.Exec("RELEASE SAVEPOINT " + savepoint)`,
		"UpsertBatch must RELEASE the savepoint after either success or rollback so it is cleared from the stack (issue #1392)")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

// TestUpsertDispatchPreservesMultiWordResourceCasing is the regression test
// for issue #1064: dispatch case strings must use the spec resource key,
// not the snake-cased table name, or kebab multi-word resources silently
// skip the typed upsert and FTS search paths.
func TestUpsertDispatchPreservesMultiWordResourceCasing(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "media",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/media-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"audio-isolation": {
				Description: "Isolated audio tracks",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/v1/audio-isolation",
						Description: "List isolation jobs",
						Response:    spec.ResponseDef{Type: "array", Item: "AudioIsolation"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"AudioIsolation": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
					// Two FTS-eligible text fields force the schema builder
					// to emit FTS5, which in turn emits the search dispatch
					// switch that #1064 also affects.
					{Name: "description", Type: "string"},
					{Name: "status", Type: "string"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	store := string(storeSrc)

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	sync := string(syncSrc)

	searchSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "search.go"))
	require.NoError(t, err)
	search := string(searchSrc)

	// The typed helper name keeps its snake-derived PascalCase shape — the
	// fix only changes the case-string discriminant, not the Go identifier.
	assert.Contains(t, store, "func (s *Store) upsertAudioIsolationTx(",
		"typed Tx helper should still be PascalCase-named from snake schema name")
	assert.Contains(t, store, "func (s *Store) UpsertAudioIsolation(",
		"public typed upsert should still be PascalCase-named from snake schema name")
	assert.Contains(t, store, "func (s *Store) SearchAudioIsolation(",
		"FTS5 search helper should still be PascalCase-named from snake schema name")

	// UpsertBatch must dispatch on the runtime resource form (kebab) to
	// match defaultSyncResources/syncResourcePath/resourceIDFieldOverrides.
	// The dispatch captures the typed error into a local so a savepoint
	// can roll it back without unwinding the outer transaction (#1392).
	assert.Regexp(t,
		`(?s)func \(s \*Store\) UpsertBatch\(.*case "audio-isolation":\s+typedErr = s\.upsertAudioIsolationTx\(`,
		store,
		"UpsertBatch must dispatch on \"audio-isolation\" — the snake case string never matches the kebab runtime resource (issue #1064)")

	// upsertSingleObject must dispatch the same way.
	assert.Regexp(t,
		`(?s)func upsertSingleObject\(.*case "audio-isolation":\s+return db\.UpsertAudioIsolation\(data\)`,
		sync,
		"upsertSingleObject must dispatch on \"audio-isolation\" so single-object responses reach the typed table (issue #1064)")

	// search.go's --type filter has the same shape — the user passes the
	// kebab name (matching what `sync` advertises), so the case must too.
	assert.Regexp(t,
		`(?s)switch resourceType \{.*case "audio-isolation":\s+results, err = db\.SearchAudioIsolation\(query, limit\)`,
		search,
		"search dispatch must accept \"audio-isolation\" so --type <kebab-resource> reaches the FTS index (issue #1064)")

	// And the old snake-case discriminants must be absent — leaving them in
	// would mean the dispatch is fixed in one place but not the other.
	for _, src := range []string{store, sync, search} {
		assert.NotContains(t, src, `case "audio_isolation":`,
			"snake_case case string is the bug; only kebab-case should match the runtime resource")
	}

	// Compile and run the generated store package. The emitted
	// TestUpsertBatch_PopulatesAudioIsolationTable calls
	// UpsertBatch("audio-isolation", ...) against the kebab dispatch case;
	// a regression that re-broke either side would fail here at runtime.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

// TestGenerateStoreSubResourceUpsertBindingOrder asserts that the typed
// upsert for a sub-resource table binds its argument values in the same
// order as the SQL column declarations. buildSubResourceTable puts the
// FK column between id and data, so the bindings have to match —
// otherwise JSON blobs land in the FK column, timestamps land in data,
// and FK values land in synced_at, silently corrupting every row.
func TestGenerateStoreSubResourceUpsertBindingOrder(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "subres",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"SUBRES_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/subres-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"domains": {
				Description: "Manage domains",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/domains", Description: "List domains"},
				},
				SubResources: map[string]spec.Resource{
					"verify": {
						Description: "Verify a domain",
						Endpoints: map[string]spec.Endpoint{
							"get": {
								Method:      "GET",
								Path:        "/domains/{domainId}/verify",
								Description: "Get verification status",
								Params:      []spec.Param{{Name: "domainId", Type: "string", Required: true, Positional: true}},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeSrc)

	// buildSubResourceTable inserts the FK column between id and data, so
	// the column declaration order is (id, domains_id, data, synced_at).
	// safeSQLName quote-wraps every identifier in emitted SQL.
	assert.Contains(t, src, `INSERT INTO "verify" ("id", "domains_id", "data", "synced_at")`,
		"sub-resource table should declare FK column between id and data")

	// The argument bindings must follow that same order.
	assert.Regexp(t,
		`(?s)id,\s+lookupFieldValue\(obj, "domains_id"\),\s+string\(data\),\s+time\.Now\(\)\.UTC\(\)\.Format\(time\.RFC3339\),`,
		src,
		"upsertVerifyTx binding order must match (id, domains_id, data, synced_at) column order")

	// And the swapped order must be absent.
	assert.NotRegexp(t,
		`(?s)id,\s+string\(data\),\s+time\.Now\(\)\.UTC\(\)\.Format\(time\.RFC3339\),\s+lookupFieldValue\(obj, "domains_id"\),`,
		src,
		"swapped (id, data, synced_at, fk) binding order must not be emitted")
}

// TestGenerateStoreSubResourceUpsertBatchTestSatisfiesNotNullFK pins the
// emitted TestUpsertBatch_Populates<Name>Table fixture against the NOT NULL
// parent FK column declared by buildSubResourceTable. Without an injected
// FK value, the typed insert fails the NOT NULL constraint on a freshly
// generated CLI.
func TestGenerateStoreSubResourceUpsertBatchTestSatisfiesNotNullFK(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "subres-fk",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/subres-fk-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"domains": {
				Description: "Manage domains",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/domains", Description: "List domains"},
				},
				SubResources: map[string]spec.Resource{
					"verify": {
						Description: "Verify a domain",
						Endpoints: map[string]spec.Endpoint{
							"get": {
								Method:      "GET",
								Path:        "/domains/{domainId}/verify",
								Description: "Get verification status",
								Params:      []spec.Param{{Name: "domainId", Type: "string", Required: true, Positional: true}},
							},
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

	testSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "upsert_batch_test.go"))
	require.NoError(t, err)
	// Anchor all three FK injections to the function body so go test -short
	// (which skips runGoCommand build/test) still gates a regression that
	// moved the FK into a comment or only one of the three rows.
	assert.Regexp(t,
		`(?s)func TestUpsertBatch_PopulatesVerifyTable\(.*?"domains_id":\s*"[^"]+".*?"domains_id":\s*"[^"]+".*?"domains_id":\s*"[^"]+"`,
		string(testSrc),
		"TestUpsertBatch_PopulatesVerifyTable fixture must populate the parent FK column declared NOT NULL by buildSubResourceTable in all three items")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestGenerateDependentSyncInjectsSubResourceParentFK(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "dependent-parent-fk",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/dependent-parent-fk-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"lists": {
				Description: "Manage lists",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/lists",
						Description: "List lists",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
				SubResources: map[string]spec.Resource{
					"contacts": {
						Description: "Manage contacts",
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:      "GET",
								Path:        "/lists/{listId}/contacts",
								Description: "List contacts",
								Response:    spec.ResponseDef{Type: "array"},
								Params:      []spec.Param{{Name: "listId", Type: "string", Required: true, Positional: true}},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "lists", Path: "/lists", Method: "GET"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{Name: "contacts", ParentResource: "lists", ParentIDParam: "listId", Path: "/lists/{listId}/contacts", Method: "GET"},
		},
	}
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	assert.Contains(t, syncContent, `Name: "contacts", ParentTable: "lists"`,
		"test fixture should exercise the dependent-resource sync path")
	assert.Contains(t, syncContent, `obj["parent_id"] = parentIDJSON`,
		"dependent sync should keep populating the generic parent_id context column")
	assert.Contains(t, syncContent, `parentFKKey := dep.ParentTable + "_id"`,
		"dependent sync should derive the typed parent FK column from the parent table")
	assert.Contains(t, syncContent, `if _, ok := obj[parentFKKey]; !ok {`,
		"dependent sync should not overwrite a response body value for the typed parent FK")
	assert.Contains(t, syncContent, `obj[parentFKKey] = parentIDJSON`,
		"dependent sync should fill the typed parent FK column when the response omits it")

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	assert.Contains(t, string(storeGo), `lookupFieldValue(obj, "lists_id")`,
		"contacts typed upsert must read the generated NOT NULL parent FK column")

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type dependentParentFKClient struct{}

func (dependentParentFKClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	return json.RawMessage(` + "`" + `[
		{"id":"contact-injected","email":"injected@example.test"},
		{"id":"contact-preserved","lists_id":"api-list","email":"preserved@example.test"}
	]` + "`" + `), nil
}

func (dependentParentFKClient) RateLimit() float64 {
	return 0
}

func TestSyncDependentResourcePopulatesTypedParentFK(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("lists", "list-A", []byte(` + "`" + `{"id":"list-A"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent list: %v", err)
	}

	res := syncDependentResource(
		context.Background(),
		dependentParentFKClient{},
		db,
		dependentResourceDef{Name: "contacts", ParentTable: "lists", ParentIDParam: "listId", PathTemplate: "/lists/{listId}/contacts"},
		"", false, 1, false, nil, nil,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 2 {
		t.Fatalf("synced count = %d, want 2", res.Count)
	}

	rows, err := db.DB().Query(` + "`" + `SELECT id, lists_id FROM contacts ORDER BY id` + "`" + `)
	if err != nil {
		t.Fatalf("query contacts: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var id, listsID string
		if err := rows.Scan(&id, &listsID); err != nil {
			t.Fatalf("scan contact: %v", err)
		}
		got[id] = listsID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate contacts: %v", err)
	}

	if got["contact-injected"] != "list-A" {
		t.Fatalf("injected contact lists_id = %q, want list-A", got["contact-injected"])
	}
	if got["contact-preserved"] != "api-list" {
		t.Fatalf("preserved contact lists_id = %q, want api-list", got["contact-preserved"])
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "dependent_parent_fk_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncDependentResourcePopulatesTypedParentFK", "./internal/cli")
}

func TestSyncDependentResourceSubstitutesChainedPathParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("dependent-chained-params")
	apiSpec.Resources = map[string]spec.Resource{
		"channels": {
			Description: "Channels",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/channels",
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
		"messages": {
			Description: "Messages",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/channels/{channelId}/messages",
					Response: spec.ResponseDef{Type: "array"},
					Params:   []spec.Param{{Name: "channelId", Type: "string", Required: true, Positional: true}},
				},
			},
		},
		"reactions": {
			Description: "Reactions",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/channels/{channelId}/messages/{messageId}/reactions",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "channelId", Type: "string", Required: true, Positional: true},
						{Name: "messageId", Type: "string", Required: true, Positional: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "channels", Path: "/channels", Method: "GET"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{
				Name:           "messages",
				ParentResource: "channels",
				ParentIDParam:  "channelId",
				Path:           "/channels/{channelId}/messages",
				Method:         "GET",
				PathParams:     []profiler.DependentPathParam{{Param: "channelId", Field: "id"}},
			},
			{
				Name:           "messages_reactions",
				ParentResource: "messages",
				ParentIDParam:  "messageId",
				Path:           "/channels/{channelId}/messages/{messageId}/reactions",
				Method:         "GET",
				PathParams: []profiler.DependentPathParam{
					{Param: "channelId", Field: "channels_id"},
					{Param: "messageId", Field: "id"},
				},
			},
		},
	}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type dependentChainedParamClient struct {
	t *testing.T
}

func (c dependentChainedParamClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if path != "/channels/chan-1/messages/msg-1/reactions" {
		c.t.Fatalf("path = %q, want /channels/chan-1/messages/msg-1/reactions", path)
	}
	return json.RawMessage(` + "`" + `[{"id":"react-1","emoji":"thumbsup"}]` + "`" + `), nil
}

func (dependentChainedParamClient) RateLimit() float64 {
	return 0
}

func TestSyncDependentResourceSubstitutesChainedPathParams(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("messages", "msg-1", []byte(` + "`" + `{"id":"msg-1","channels_id":"chan-1","text":"hello"}` + "`" + `)); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	res := syncDependentResource(
		context.Background(),
		dependentChainedParamClient{t: t},
		db,
		dependentResourceDef{
			Name: "messages_reactions",
			ParentTable: "messages",
			ParentIDParam: "messageId",
			PathTemplate: "/channels/{channelId}/messages/{messageId}/reactions",
			PathParams: []dependentPathParamDef{
				{Param: "channelId", Field: "channels_id"},
				{Param: "messageId", Field: "id"},
			},
		},
		"", false, 1, false, nil, nil,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 1 {
		t.Fatalf("synced count = %d, want 1", res.Count)
	}

	rows, err := db.DB().Query(` + "`" + `SELECT id, json_extract(data, '$.messages_id'), json_extract(data, '$.channels_id') FROM resources WHERE resource_type = 'messages_reactions'` + "`" + `)
	if err != nil {
		t.Fatalf("query reactions: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatalf("expected one reaction row")
	}
	var id, messagesID, channelsID string
	if err := rows.Scan(&id, &messagesID, &channelsID); err != nil {
		t.Fatalf("scan reaction: %v", err)
	}
	if id != "react-1" || messagesID != "msg-1" || channelsID != "chan-1" {
		t.Fatalf("row = (%q, %q, %q), want (react-1, msg-1, chan-1)", id, messagesID, channelsID)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "dependent_chained_params_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncDependentResourceSubstitutesChainedPathParams", "./internal/cli")
}

func TestGenerateSimilarCommandUsesCompositeResourceKey(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("similar-composite")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:    true,
		Insights: []string{"insights/similar.go.tmpl"},
	}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

func TestSimilarDisambiguatesOverlappingIDs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.Upsert("biz", "shared", []byte(` + "`" + `{"id":"shared","name":"Pinky restaurant"}` + "`" + `)); err != nil {
		t.Fatalf("upsert biz: %v", err)
	}
	if err := db.Upsert("bookmark", "shared", []byte(` + "`" + `{"id":"shared","name":"Anniversary bookmark"}` + "`" + `)); err != nil {
		t.Fatalf("upsert bookmark: %v", err)
	}
	if err := db.Upsert("biz", "other", []byte(` + "`" + `{"id":"other","name":"Pinky restaurant north"}` + "`" + `)); err != nil {
		t.Fatalf("upsert peer: %v", err)
	}
	db.Close()

	cmd := newSimilarCmd(&rootFlags{asJSON: true})
	cmd.SetArgs([]string{"shared", "--db", dbPath})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "matches multiple resource types") {
		t.Fatalf("similar without --type err = %v, want overlap error", err)
	}

	cmd = newSimilarCmd(&rootFlags{asJSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"shared", "--db", dbPath, "--type", "biz"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("similar with --type: %v", err)
	}

	var got struct {
		SourceType string ` + "`" + `json:"source_type"` + "`" + `
		Similar []struct {
			ID           string ` + "`" + `json:"id"` + "`" + `
			ResourceType string ` + "`" + `json:"resource_type"` + "`" + `
		} ` + "`" + `json:"similar"` + "`" + `
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out.String())
	}
	if got.SourceType != "biz" {
		t.Fatalf("source_type = %q, want biz", got.SourceType)
	}
	for _, item := range got.Similar {
		if item.ID == "shared" && item.ResourceType == "biz" {
			t.Fatalf("source item was not excluded exactly: %+v", got.Similar)
		}
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "similar_composite_key_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestSimilarDisambiguatesOverlappingIDs")
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

// TestWriteThroughCacheNonIDPrimaryKeyResponse guards #1439: the previous
// `envelope["id"]` guard silently dropped single-object detail responses
// whose primary key field was named anything other than "id" (PCGS uses
// CertNo, Stripe uses object ids prefixed with their type, many ERP APIs
// use sku / invoiceId / etc.). After the fix the row reaches UpsertBatch
// and the existing resourceIDFieldOverrides path resolves the PK.
func TestLiveFetchWriteThroughCacheNonIDPrimaryKeyResponse(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "pcgs",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/pcgs-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"certs": {
				Description: "Look up coin certs by CertNo",
				Endpoints: map[string]spec.Endpoint{
					// list endpoint triggers typed-table emission (UpsertBatch
					// dispatches to it); lookup endpoint is the single-object
					// detail path the bug fires on at runtime.
					"list": {
						Method:      "GET",
						Path:        "/certs",
						Description: "List recent certs",
						IDField:     "CertNo",
						Response:    spec.ResponseDef{Type: "array", Item: "Cert"},
					},
					"lookup": {
						Method:      "GET",
						Path:        "/certs/{cert_no}",
						Description: "Fetch a single cert by CertNo",
						IDField:     "CertNo",
						Response:    spec.ResponseDef{Type: "object", Item: "Cert"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Cert": {
				Fields: []spec.TypeField{
					{Name: "CertNo", Type: "string"},
					{Name: "PCGSNo", Type: "string"},
					{Name: "Grade", Type: "string"},
				},
			},
		},
	}
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

func TestWriteThroughCacheNonIDPrimaryKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Single-object detail response keyed by CertNo (not "id"). Before
	// #1439, the envelope["id"] guard caused writeThroughCache to drop
	// this on the floor. After the fix the row reaches UpsertBatch and
	// the resourceIDFieldOverrides mechanism resolves the PK.
	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"CertNo":"12345678","PCGSNo":"7280","Grade":"MS65"}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var typedCount int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id = '12345678'` + "`" + `).Scan(&typedCount); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if typedCount != 1 {
		t.Fatalf("typed certs count = %d, want 1 (non-id primary key was dropped)", typedCount)
	}
}

// TestWriteThroughCacheSkipsEmptyListEnvelope guards the tighter
// single-object branch from misfiring on list responses whose array
// happened to be empty. {"items": []} must NOT write a row keyed by
// the entire envelope; the existing wrapper-key loop already handled
// this implicitly and the relaxed guard must preserve the invariant.
func TestWriteThroughCacheSkipsEmptyListEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"items": []}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 0 {
		t.Fatalf("certs count after empty-list envelope = %d, want 0 (envelope must not be upserted as a single object)", count)
	}
}

// TestWriteThroughCacheCachesObjectWithListWrapperFieldName guards the
// list-envelope guard from a field-name collision: a detail object whose
// own field happens to be named "data" / "results" / "items" with a
// scalar value (not an array) is a regular response, not an empty list
// envelope, and must still be cached. Skipping it would be a regression
// from the prior envelope["id"] path.
func TestWriteThroughCacheCachesObjectWithListWrapperFieldName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// "data" key is present but holds a string. Old code would have written
	// this via the id guard; new guard must still write it because the
	// wrapper key isn't a list.
	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"CertNo":"55555","data":"opaque-token","Grade":"AU58"}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id = '55555'` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 1 {
		t.Fatalf("certs count after field-name-collision response = %d, want 1 (scalar-valued wrapper-named field must not trip the list-envelope skip)", count)
	}
}

// TestWriteThroughCacheCachesObjectWithNullWrapperFieldName guards the
// specific JSON-null case Greptile flagged: json.Unmarshal("null", &arr)
// succeeds with arr=nil, so without an explicit arr != nil check the
// guard would misclassify {"CertNo":"x","items":null} as an empty-list
// envelope and silently drop the row.
func TestWriteThroughCacheCachesObjectWithNullWrapperFieldName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"CertNo":"77777","items":null,"Grade":"PR70"}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id = '77777'` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 1 {
		t.Fatalf("certs count after null-wrapper-field response = %d, want 1 (JSON null is not an empty array)", count)
	}
}

// TestWriteThroughCacheCachesObjectWithEmptyListWrapperAndOtherFields
// covers the multi-key case Greptile flagged in round 3: a detail object
// with real per-row fields PLUS a wrapper-named field that happens to be
// an empty array. The skip branch must only fire when EVERY top-level
// key is a list wrapper or known pagination metadata.
func TestWriteThroughCacheCachesObjectWithEmptyListWrapperAndOtherFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"CertNo":"88888","items":[],"Grade":"PR69"}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id = '88888'` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 1 {
		t.Fatalf("certs count after detail-with-empty-wrapper-field response = %d, want 1 (envelope has real per-row fields and must be cached)", count)
	}
}

func TestWriteThroughCacheNestedDataEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{
		"success": true,
		"warnings": [{"code": "slow-page"}],
		"data": {
			"certs": [
				{"CertNo":"90001","Grade":"MS65"},
				{"CertNo":"90002","Grade":"MS66"}
			],
			"pagination": {"has_more": false}
		}
	}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id IN ('90001', '90002')` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 2 {
		t.Fatalf("certs count after nested data envelope = %d, want 2", count)
	}
}

func TestWriteThroughCacheSkipsMetadataErrorArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{
		"success": true,
		"errors": [{"code": "deprecation"}],
		"warnings": [{"code": "slow-page"}],
		"data": null
	}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 0 {
		t.Fatalf("certs count after metadata error array envelope = %d, want 0", count)
	}
}

func TestWriteThroughSingleArraySiblingSkipsPrimitiveArrays(t *testing.T) {
	envelope := map[string]json.RawMessage{
		"ids":    json.RawMessage(` + "`" + `[1, 2, 3]` + "`" + `),
		"cursor": json.RawMessage(` + "`" + `"next"` + "`" + `),
	}
	items, ok := extractWriteThroughSingleArraySibling(envelope, decodeWriteThroughNonEmptyArray)
	if ok || len(items) != 0 {
		t.Fatalf("primitive array sibling = %d/%v, want no extracted resource items", len(items), ok)
	}
}

func TestWriteThroughCacheCachesObjectWithChildArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{
		"CertNo":"91000",
		"line_items":[{"id":"li_1"}],
		"Grade":"MS64"
	}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs WHERE id = '91000'` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 1 {
		t.Fatalf("certs count after child-array detail object = %d, want 1", count)
	}
}

func TestWriteThroughCacheSkipsNestedEmptyListEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{
		"success": true,
		"data": {
			"certs": [],
			"pagination": {"has_more": false}
		}
	}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 0 {
		t.Fatalf("certs count after nested empty-list envelope = %d, want 0", count)
	}
}

// TestWriteThroughCacheSkipsListEnvelopeWithPaginationMetadata guards the
// flip side: a real list envelope with pagination metadata fields
// (next_cursor, has_more, etc.) plus an empty array must still skip
// the single-object upsert path. The metadata allowlist is what keeps
// these envelopes recognized as lists.
func TestWriteThroughCacheSkipsListEnvelopeWithPaginationMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeThroughCache(context.Background(), "certs", json.RawMessage(` + "`" + `{"items":[],"next_cursor":"","has_more":false}` + "`" + `))

	db, err := store.Open(defaultDBPath("pcgs-pp-cli"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM certs` + "`" + `).Scan(&count); err != nil {
		t.Fatalf("query certs: %v", err)
	}
	if count != 0 {
		t.Fatalf("certs count after list-envelope-with-metadata response = %d, want 0 (no real per-row data, must skip)", count)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "write_through_cache_non_id_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestWriteThroughCacheNonIDPrimaryKey|TestWriteThroughCacheSkipsEmptyListEnvelope|TestWriteThroughCacheCachesObjectWithListWrapperFieldName|TestWriteThroughCacheCachesObjectWithNullWrapperFieldName|TestWriteThroughCacheCachesObjectWithEmptyListWrapperAndOtherFields|TestWriteThroughCacheNestedDataEnvelope|TestWriteThroughCacheSkipsMetadataErrorArray|TestWriteThroughSingleArraySiblingSkipsPrimitiveArrays|TestWriteThroughCacheCachesObjectWithChildArray|TestWriteThroughCacheSkipsNestedEmptyListEnvelope|TestWriteThroughCacheSkipsListEnvelopeWithPaginationMetadata", "./internal/cli")
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
	assert.Regexp(t, `"workspace":\s+"workspaces"`, src)
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

func TestGeneratedRootTimeoutDefaultIs60s(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), `rootCmd.PersistentFlags().DurationVar(&flags.timeout, "timeout", 60*time.Second, "Request timeout")`)
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
	// Mutate envelope's success depends on HTTP status AND the absence of a
	// body-level partial-failure (e.g. Google Ads partialFailureError), unless
	// the caller opted in via --allow-partial-failure (flags.allowPartialFailure)
	// in which case the envelope agrees with the exit-code 0.
	assert.Contains(t, content, `"success":  statusCode >= 200 && statusCode < 300 && (partialFailure == nil || flags.allowPartialFailure)`)
	// Detection runs before any output-mode branch so the exit code is
	// consistent across table, JSON envelope, and raw output paths.
	assert.Contains(t, content, `detectPartialFailure(data)`)
	// --allow-partial-failure downgrades the typed exit (code 6) to a warning.
	assert.Contains(t, content, `flags.allowPartialFailure`)
	assert.Contains(t, content, `partialFailureErr(`)

	// The generated helpers.go must carry the detection helper, the
	// typed-exit constructor, and the structured report shape. These are
	// what makes the partial-failure path machine-readable; if any goes
	// missing on regen the silent-swallow regression returns.
	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersContent := string(helpersGo)
	assert.Contains(t, helpersContent, `func detectPartialFailure(`)
	assert.Contains(t, helpersContent, `func partialFailureErr(`)
	assert.Contains(t, helpersContent, `type partialFailureReport struct`)
	assert.Contains(t, helpersContent, `&cliError{code: 6, err: err}`)

	// Root.go must expose the --allow-partial-failure persistent flag and
	// the matching rootFlags field.
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootContent := string(rootGo)
	assert.Contains(t, rootContent, `allow-partial-failure`)
	assert.Contains(t, rootContent, `allowPartialFailure bool`)

	// Envelope fires on --json and on piped output, but explicit format flags
	// (--csv, --quiet, --plain) opt out of the auto-JSON path so piped agents
	// that asked for a non-JSON format actually get it.
	assert.Contains(t, content, `flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain)`)

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

// TestGeneratedOutput_PartialFailureDetectionRuntime drops a runtime
// test into the generated CLI module and executes go test against the
// emitted detectPartialFailure helper. This is the load-bearing
// safeguard: the per-string assertions above prove the strings are
// emitted, but only running the generated code proves the detector
// actually identifies a Google-Ads-style partialFailureError body,
// extracts results[].resourceName, and returns nil on a clean 2xx.
//
// The fixture mirrors the shape the Google Ads API returns for a
// `campaigns:mutate` call where partial_failure=true and at least one
// operation failed.
func TestGeneratedOutput_PartialFailureDetectionRuntime(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)

	// Place the runtime test alongside helpers.go in the generated
	// internal/cli package so it can call the unexported detector.
	runtimeTest := `package cli

import (
	"encoding/json"
	"testing"
)

func TestDetectPartialFailure_GoogleAdsBatchShape(t *testing.T) {
	body := []byte(` + "`" + `{
		"results": [
			{"resourceName": "customers/123/campaigns/1"},
			{"resourceName": "customers/123/campaigns/2"}
		],
		"partialFailureError": {
			"code": 3,
			"message": "Some operations failed",
			"details": [
				{"errors": [{"errorCode": {"campaignError": "DUPLICATE_NAME"}}]}
			]
		}
	}` + "`" + `)
	got := detectPartialFailure(body)
	if got == nil {
		t.Fatalf("expected partialFailureReport, got nil")
	}
	if got.Field != "partialFailureError" {
		t.Errorf("Field = %q, want %q", got.Field, "partialFailureError")
	}
	if got.Code != 3 {
		t.Errorf("Code = %d, want 3", got.Code)
	}
	if got.Message != "Some operations failed" {
		t.Errorf("Message = %q, want %q", got.Message, "Some operations failed")
	}
	if len(got.ResourceNames) != 2 {
		t.Fatalf("ResourceNames length = %d, want 2", len(got.ResourceNames))
	}
	if got.ResourceNames[0] != "customers/123/campaigns/1" {
		t.Errorf("ResourceNames[0] = %q", got.ResourceNames[0])
	}
	// Report must round-trip to the envelope shape. Marshalling here
	// asserts the json tags match what mutate-envelope consumers expect.
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(report): %v", err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal(report): %v", err)
	}
	if roundTrip["field"] != "partialFailureError" {
		t.Errorf("round-trip field = %v", roundTrip["field"])
	}
	if rn, _ := roundTrip["resource_names"].([]any); len(rn) != 2 {
		t.Errorf("round-trip resource_names = %v", roundTrip["resource_names"])
	}
}

func TestDetectPartialFailure_CleanResponseReturnsNil(t *testing.T) {
	body := []byte(` + "`" + `{"results": [{"resourceName": "customers/123/campaigns/1"}]}` + "`" + `)
	if got := detectPartialFailure(body); got != nil {
		t.Errorf("expected nil for clean response, got %+v", got)
	}
}

func TestDetectPartialFailure_EmptyErrorObjectIgnored(t *testing.T) {
	// Some servers return an empty partialFailureError{} when
	// partial_failure mode was not enabled. Empty code + empty message
	// must not flag a partial failure or callers exit non-zero on
	// every batch.
	body := []byte(` + "`" + `{"results": [], "partialFailureError": {}}` + "`" + `)
	if got := detectPartialFailure(body); got != nil {
		t.Errorf("expected nil for empty partialFailureError, got %+v", got)
	}
}

func TestDetectPartialFailure_PartialFailureErrTypedExitCode(t *testing.T) {
	// partialFailureErr must wrap with the typed exit code (6) so the
	// CLI's ExitCode() emits a distinguishable code; HTTP failures use
	// apiErr (5), so 6 stays reserved for partial-failure semantics.
	err := partialFailureErr(&jsonSyntaxStub{msg: "x"})
	if got := ExitCode(err); got != 6 {
		t.Errorf("ExitCode = %d, want 6", got)
	}
}

type jsonSyntaxStub struct{ msg string }

func (e *jsonSyntaxStub) Error() string { return e.msg }
`
	testPath := filepath.Join(outputDir, "internal", "cli", "partialfailure_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "-run", "TestDetectPartialFailure", "./internal/cli")
}

// TestCompactListFieldsPreservesUnknownShapes pins the contracts that keep
// `--agent` / `--compact` from silently emitting {} or partial-strip rows
// for records that don't match the canonical id/name/title allowlist:
//
//  1. The static allow-list covers canonical scalars (price/fare/locale/code/...).
//  2. A data-driven extension keeps any scalar key present in 80%+ of rows,
//     so hand-written novel commands whose output keys aren't on the
//     allow-list (object_name, match_key, snippet) survive projection.
//  3. Verbose fields (description, body, ...) are excluded from the
//     extension regardless of frequency.
//  4. A per-item fallback preserves the original item when no key matches.
//
// Pinned at the template level because the helper renders into every
// printed CLI's helpers.go and a per-fixture assertion would miss future
// copies that drift in.
func TestCompactListFieldsPreservesUnknownShapes(t *testing.T) {
	t.Parallel()

	path := filepath.Join("templates", "helpers.go.tmpl")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "template must exist: %s", path)
	body := string(data)

	assert.Contains(t, body, "if len(compact) == 0 {",
		"compactListFields must preserve the original item when no keep keys match — otherwise records with domain-specific field names get reduced to {}")
	assert.Contains(t, body, "compact = item",
		"compactListFields must assign the original item as the per-item fallback")

	for _, key := range []string{
		`"price":`, `"fare":`, `"currency":`,
		`"rating":`,
		`"locale":`, `"language":`,
		`"code":`,
	} {
		assert.Contains(t, body, key,
			"compactListFields allowlist must include %s so canonical-schema records keep high-signal scalars across business/travel/commerce/locale APIs", key)
	}

	// Data-driven extension: pin the symbols that make the 80%-frequency
	// rule fire. Without these, partial-strip silently drops novel-command
	// fields whenever a row happens to also carry one allow-list key
	// (e.g. {"id":"x","object_name":"users","match_key":"uuid"} -> {"id":"x"}).
	assert.Contains(t, body, "keyCounts",
		"compactListFields must count per-key occurrence so frequent novel-command keys survive")
	assert.Contains(t, body, "isCompactScalar",
		"compactListFields must filter the data-driven extension by scalar type so nested objects/arrays don't bloat --compact output")
	assert.Contains(t, body, "compactVerboseListFields",
		"compactListFields must exclude description/body/content from the data-driven extension regardless of frequency")
	assert.Contains(t, body, "threshold",
		"compactListFields must compute a frequency threshold for the data-driven extension")
}

// matchClosingBrace walks s from start, finds the first `{`, then returns
// the index of the matching `}` by counting depth. Returns -1 if either no
// opening brace exists at/after start or the input is unbalanced.
func matchClosingBrace(s string, start int) int {
	depth := 0
	seenOpen := false
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
			seenOpen = true
		case '}':
			depth--
			if seenOpen && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TestCompactObjectFieldsPreservesPayloadFields pins the contract that
// single-object `get` responses retain their primary payload fields under
// `--agent`/`--compact`. The list-path blocklist correctly strips body/
// content/html/markdown from list items (verbose noise), but applying the
// same blocklist to single-object responses silently drops the field the
// caller asked for. Pinned at the template level so the two blocklists stay
// separate as the helper renders into every printed CLI.
func TestCompactObjectFieldsPreservesPayloadFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join("templates", "helpers.go.tmpl")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "template must exist: %s", path)
	body := string(data)

	require.Contains(t, body, "compactVerboseObjectFields",
		"compactObjectFields must read from a dedicated object-path blocklist, not the list-path one")
	require.Contains(t, body, "if !compactVerboseObjectFields[k] {",
		"compactObjectFields must consult the object-path blocklist when deciding which keys to keep")

	objStart := strings.Index(body, "compactVerboseObjectFields = map[string]bool{")
	require.GreaterOrEqual(t, objStart, 0, "compactVerboseObjectFields map literal must exist")
	objEnd := matchClosingBrace(body, objStart)
	require.GreaterOrEqual(t, objEnd, 0, "compactVerboseObjectFields map literal must close")
	objBody := body[objStart : objEnd+1]

	for _, payload := range []string{`"body"`, `"content"`, `"html"`, `"markdown"`} {
		assert.NotContains(t, objBody, payload,
			"compactVerboseObjectFields must not list %s — those fields are the primary payload on single-object get responses", payload)
	}
	for _, metadata := range []string{`"description"`, `"comments"`, `"attachments"`} {
		assert.Contains(t, objBody, metadata,
			"compactVerboseObjectFields must still strip %s — that field is metadata on single-object responses, not payload", metadata)
	}
}

// The cursor param's "0" must survive paginatedGet's zero-value strip:
// offset-paginated APIs require offset=0 on the first page request.
func TestPaginatedGetExemptsCursorParamFromZeroStripping(t *testing.T) {
	t.Parallel()

	path := filepath.Join("templates", "helpers.go.tmpl")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "template must exist: %s", path)
	body := string(data)

	cleanStart := strings.Index(body, "clean := map[string]string{}")
	require.GreaterOrEqual(t, cleanStart, 0, "paginatedGet must declare a clean map")
	loopEnd := strings.Index(body[cleanStart:], "if !fetchAll")
	require.GreaterOrEqual(t, loopEnd, 0, "expected fetchAll branch after clean loop")
	cleanBlock := body[cleanStart : cleanStart+loopEnd]

	assert.Contains(t, cleanBlock, "cursorParam",
		"paginatedGet's clean loop must reference cursorParam so the cursor key is exempt from zero-stripping")
	assert.NotContains(t, cleanBlock, `if v != "" && v != "0" && v != "false"`,
		`the unconditional v != "" && v != "0" && v != "false" filter incorrectly drops cursor="0" for offset-paginated APIs`)
}

// TestPipedJsonGateRespectsExplicitFormatFlags pins the contract: the
// piped-output auto-JSON gate must defer to explicit --csv / --quiet /
// --plain flags so piped consumers that asked for a non-JSON format
// actually get it. Before the fix, the gate read
// `flags.asJSON || !isTerminal(...)` and emitted JSON whenever stdout was
// piped, which is the common case for agents and shell pipelines, so
// `--csv | head` produced JSON instead of CSV. The fix adds the
// `&& !flags.csv && !flags.quiet && !flags.plain` clause so an explicit
// format choice opts out of the auto-JSON path.
//
// Read the templates directly to pin every gate site at once: the
// command_endpoint and command_promoted templates emit into hundreds of
// generated files (pet_add.go, pet_list.go, every promoted command), and
// a per-file assertion would miss any new gate copies that drift in.
func TestPipedJsonGateRespectsExplicitFormatFlags(t *testing.T) {
	t.Parallel()

	expected := `flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain)`
	stale := `flags.asJSON || !isTerminal(cmd.OutOrStdout())`

	for _, path := range []string{
		filepath.Join("templates", "command_endpoint.go.tmpl"),
		filepath.Join("templates", "command_promoted.go.tmpl"),
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "template must exist: %s", path)
		body := string(data)

		assert.Contains(t, body, expected,
			"%s must gate auto-JSON behind format-flag escape hatch so piped --csv/--quiet/--plain reach the standard pipeline", path)
		assert.NotContains(t, body, stale,
			"%s still contains the bare piped-pipe gate; every site must include the format-flag escape hatch", path)
	}
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

func TestClassifyAPIErrorPreservesTypedCLIError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		flags    *rootFlags
		wantCode int
	}{
		{"semantic envelope error", usageErr(errors.New("semantic API envelope rejected input")), &rootFlags{}, 2},
		{"HTTP-like usage error with idempotent flags", usageErr(errors.New("HTTP 409: conflict")), &rootFlags{idempotent: true, asJSON: true}, 2},
		{"HTTP-like auth error", authErr(errors.New("HTTP 401: unauthorized")), &rootFlags{}, 4},
		{"HTTP-like api error with idempotent flags", apiErr(errors.New("HTTP 409: conflict")), &rootFlags{idempotent: true, asJSON: true}, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, classified := captureStdoutStderr(t, func() error {
				return classifyAPIError(tc.err, tc.flags)
			})

			if classified != tc.err {
				t.Fatalf("classifyAPIError should preserve typed cliError unchanged, got %#v", classified)
			}
			if got := ExitCode(classified); got != tc.wantCode {
				t.Fatalf("ExitCode(classifyAPIError(%T)) = %d, want %d", tc.err, got, tc.wantCode)
			}
			if stdout != "" {
				t.Fatalf("typed cliError pass-through should not write stdout, got %q", stdout)
			}
			if stderr != "" {
				t.Fatalf("typed cliError pass-through should not write stderr, got %q", stderr)
			}
		})
	}
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
	assert.Regexp(t, `"items":\s+true`, exportContent)
	assert.Regexp(t, `"stories":\s+true`, exportContent)
	assert.Regexp(t, `"users":\s+true`, exportContent)
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
	assert.NotContains(t, content, "unwrapSingleKeyArray")
	assert.NotContains(t, content, "defaultDBPath")

	// Core helpers should still be present
	assert.Contains(t, content, "classifyAPIError")
	assert.Contains(t, content, "printOutputWithFlags")
}

// TestGeneratedHelpers_WrapWithProvenanceUnwrapsSingleKeyEnvelope guards
// the runtime contract of wrapWithProvenance for single-key API envelopes.
// Without the unwrap, --json output for APIs that wrap collections in
// {"results":[...]}, {"data":[...]}, etc. ends up double-nested
// ({"meta":..., "results":{"results":[...]}}), forcing agents into
// API-specific jq paths instead of a stable .results[]. The behavior test
// is injected into the generated CLI and run with `go test`, so a template
// regression on the unwrap shape fails this test before any printed CLI
// inherits the bad output shape.
func TestGeneratedHelpers_WrapWithProvenanceUnwrapsSingleKeyEnvelope(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "envelopeunwrap",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/envelopeunwrap-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	src := string(helpersGo)
	assert.Contains(t, src, "func unwrapSingleKeyArray(",
		"helpers.go must define the unwrap helper")
	assert.Contains(t, src, "unwrapSingleKeyArray(data)",
		"wrapWithProvenance must call the unwrap helper on valid JSON data")

	runGoCommand(t, outputDir, "mod", "tidy")
	behaviorTest := `package cli

import (
	"encoding/json"
	"testing"
)

func TestUnwrapSingleKeyArray_KnownWrapperKeysUnwrap(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"results", ` + "`{\"results\":[{\"id\":\"a\"}]}`" + `, ` + "`[{\"id\":\"a\"}]`" + `},
		{"data", ` + "`{\"data\":[{\"id\":\"a\"}]}`" + `, ` + "`[{\"id\":\"a\"}]`" + `},
		{"items", ` + "`{\"items\":[1,2,3]}`" + `, ` + "`[1,2,3]`" + `},
		{"nodes", ` + "`{\"nodes\":[]}`" + `, ` + "`[]`" + `},
		{"entries", ` + "`{\"entries\":[\"x\"]}`" + `, ` + "`[\"x\"]`" + `},
		{"records", ` + "`{\"records\":[null]}`" + `, ` + "`[null]`" + `},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := unwrapSingleKeyArray(json.RawMessage(tc.in))
			if string(got) != tc.want {
				t.Fatalf("unwrapSingleKeyArray(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestUnwrapSingleKeyArray_PassThroughs(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"bare array", ` + "`[{\"id\":\"a\"}]`" + `},
		{"multi-key object preserves cursor", ` + "`{\"results\":[],\"next_page_token\":\"abc\"}`" + `},
		{"unknown wrapper key", ` + "`{\"payload\":[1,2]}`" + `},
		{"single key but not array value", ` + "`{\"data\":{\"issues\":{\"nodes\":[]}}}`" + `},
		{"single key but value is null", ` + "`{\"results\":null}`" + `},
		{"single key but value is string", ` + "`{\"data\":\"hello\"}`" + `},
		{"empty object", ` + "`{}`" + `},
		{"non-object scalar", ` + "`42`" + `},
		{"invalid json", ` + "`<xml/>`" + `},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := unwrapSingleKeyArray(json.RawMessage(tc.in))
			if string(got) != tc.in {
				t.Fatalf("unwrapSingleKeyArray(%s) = %s, want unchanged", tc.in, got)
			}
		})
	}
}

func TestWrapWithProvenance_FlattensSingleKeyEnvelope(t *testing.T) {
	prov := DataProvenance{Source: "live"}
	wrapped, err := wrapWithProvenance(json.RawMessage(` + "`{\"results\":[{\"id\":\"a\"}]}`" + `), prov)
	if err != nil {
		t.Fatalf("wrapWithProvenance: %v", err)
	}
	var out struct {
		Meta    map[string]any    ` + "`json:\"meta\"`" + `
		Results []json.RawMessage ` + "`json:\"results\"`" + `
	}
	if err := json.Unmarshal(wrapped, &out); err != nil {
		t.Fatalf("output is not flat .results[]: %v\noutput: %s", err, wrapped)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want 1 result, got %d (output: %s)", len(out.Results), wrapped)
	}
}

func TestWrapWithProvenance_PreservesMultiKeyResponseForCursor(t *testing.T) {
	prov := DataProvenance{Source: "live"}
	wrapped, err := wrapWithProvenance(json.RawMessage(` + "`{\"results\":[{\"id\":\"a\"}],\"next_page_token\":\"xyz\"}`" + `), prov)
	if err != nil {
		t.Fatalf("wrapWithProvenance: %v", err)
	}
	var out struct {
		Results map[string]json.RawMessage ` + "`json:\"results\"`" + `
	}
	if err := json.Unmarshal(wrapped, &out); err != nil {
		t.Fatalf("results should still be the original object: %v\noutput: %s", err, wrapped)
	}
	if _, ok := out.Results["next_page_token"]; !ok {
		t.Fatalf("cursor must remain accessible; output: %s", wrapped)
	}
}

func TestWrapWithProvenance_BareArrayUnchanged(t *testing.T) {
	prov := DataProvenance{Source: "live"}
	wrapped, err := wrapWithProvenance(json.RawMessage(` + "`[{\"id\":\"a\"}]`" + `), prov)
	if err != nil {
		t.Fatalf("wrapWithProvenance: %v", err)
	}
	var out struct {
		Results []json.RawMessage ` + "`json:\"results\"`" + `
	}
	if err := json.Unmarshal(wrapped, &out); err != nil {
		t.Fatalf("bare array must round-trip into .results[]: %v\noutput: %s", err, wrapped)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want 1 result, got %d (output: %s)", len(out.Results), wrapped)
	}
}

func TestWrapWithProvenance_NonJSONEmbeddedAsString(t *testing.T) {
	prov := DataProvenance{Source: "live"}
	wrapped, err := wrapWithProvenance(json.RawMessage(` + "`<rss><channel/></rss>`" + `), prov)
	if err != nil {
		t.Fatalf("wrapWithProvenance: %v", err)
	}
	var out struct {
		Results string ` + "`json:\"results\"`" + `
	}
	if err := json.Unmarshal(wrapped, &out); err != nil {
		t.Fatalf("non-JSON payload must embed as a string: %v\noutput: %s", err, wrapped)
	}
	if out.Results != ` + "`<rss><channel/></rss>`" + ` {
		t.Fatalf("want raw payload preserved, got %q", out.Results)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "wrap_provenance_unwrap_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestUnwrapSingleKeyArray|TestWrapWithProvenance_")
}

// findMatchingBrace returns the index of the `}` that closes the `{` at
// openIdx. Treats `{` and `}` as raw bytes — only safe for generated Go
// regions known to contain no string literals or comments holding braces.
// Returns -1 when no match is found.
func findMatchingBrace(src string, openIdx int) int {
	if openIdx >= len(src) || src[openIdx] != '{' {
		return -1
	}
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TestGeneratedListCommand_ProvenanceGatedByHumanTable asserts that the
// `N results (live|cached ...)` diagnostic is gated by wantsHumanTable in
// every generated list command. Without the gate the line leaks into stdout
// of agent-piped consumers — every printed CLI ships the same list-handler
// shape, so a template regression here re-introduces the leak everywhere.
func TestGeneratedListCommand_ProvenanceGatedByHumanTable(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "provenancegate",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/provenancegate-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			// Multi-endpoint resource exercises command_endpoint.go.tmpl.
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Response:    spec.ResponseDef{Type: "array"},
					},
					"get": {
						Method:      "GET",
						Path:        "/items/{id}",
						Description: "Get one item",
						Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true}},
					},
				},
			},
			// Single-endpoint resource exercises command_promoted.go.tmpl.
			"things": {
				Description: "Things",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/things",
						Description: "List things",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	cases := []struct {
		name string
		file string
	}{
		{"endpoint template", "items_list.go"},
		{"promoted template", "promoted_things.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", tc.file))
			require.NoError(t, err, "generated list command file must exist")
			src := string(body)

			require.Equal(t, 1, strings.Count(src, "printProvenance(cmd,"),
				"exactly one printProvenance call should remain; a second ungated call would re-leak the diagnostic")

			gateIdx := strings.Index(src, "if wantsHumanTable(cmd.OutOrStdout(), flags) {")
			require.GreaterOrEqual(t, gateIdx, 0,
				"printProvenance call must be wrapped in a wantsHumanTable gate")
			require.Equal(t, -1, strings.Index(src[:gateIdx], "printProvenance(cmd,"),
				"no ungated printProvenance call may precede the wantsHumanTable gate")

			openBraceOffset := strings.Index(src[gateIdx:], "{")
			require.GreaterOrEqual(t, openBraceOffset, 0, "gate must have an opening brace")
			closeBraceIdx := findMatchingBrace(src, gateIdx+openBraceOffset)
			require.Greater(t, closeBraceIdx, gateIdx, "gate must have a matching closing brace")
			provIdx := strings.Index(src[gateIdx:], "printProvenance(cmd,")
			require.GreaterOrEqual(t, provIdx, 0, "printProvenance must appear after the gate opens")
			require.Less(t, gateIdx+provIdx, closeBraceIdx,
				"printProvenance must appear between the gate's open and close braces, not after the block")
		})
	}
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
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	// Promoted command file SHOULD exist — it provides a user-friendly shortcut.
	promotedFile := filepath.Join(outputDir, "internal", "cli", "promoted_users.go")
	assert.FileExists(t, promotedFile)

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.Contains(t, string(helpersSrc), "func extractResponseData(",
		"promoted commands call extractResponseData, so helpers.go must emit it when a promoted command exists")

	// The resource parent command should NOT be generated — the promoted command replaces it.
	// Generating both would leave the parent as dead code (never wired to root).
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "users.go"))
}

func TestGeneratedOutput_ExtractResponseDataHelperOnlyForPromotedCommands(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "noprom",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"NO_PROM_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/noprom-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List users"},
					"get": {
						Method:      "GET",
						Path:        "/users/{id}",
						Description: "Get user",
						Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "noprom-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(helpersSrc), "func extractResponseData(",
		"helpers.go must not emit promoted-command-only helpers when no promoted command can call them")
}

func TestGeneratedOutput_ExtractResponseDataHelperSkippedForPromotedNoStoreCLI(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promnostore",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PROM_NO_STORE_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/promnostore-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users", Description: "List all users"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promnostore-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	promotedSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_users.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(promotedSrc), "extractResponseData(",
		"promoted commands without a local store do not call the unwrap helper")

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(helpersSrc), "func extractResponseData(",
		"helpers.go must not emit extractResponseData when promoted commands cannot call it")
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
	assert.Regexp(t, `Example:\s+"  promsub-pp-cli account cards get-account `, string(cardsSrc))
	assert.NotRegexp(t, `Example:\s+"  promsub-pp-cli account get-account `, string(cardsSrc))
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

func TestGeneratedCommandExamplePrefersNarrativeQuickStart(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-examples")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Params: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-examples-pp-cli users list --pcgs-no 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "users_list.go")
	assert.Contains(t, source, `"  narrative-examples-pp-cli users list --pcgs-no 7356"`)
	assert.NotContains(t, source, "--pcgs-no example-value")
}

func TestGeneratedCommandExampleFallsBackWhenNarrativeDoesNotMatchCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-fallback")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Params: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-fallback-pp-cli projects list --project-id p_12345"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "users_list.go")
	assert.Contains(t, source, `narrative-fallback-pp-cli users list --pcgs-no example-value`)
	assert.NotContains(t, source, "p_12345")
}

func TestGeneratedCommandExampleUsesNarrativeRecipeWhenQuickStartDoesNotMatch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-recipes")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Params: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-recipes-pp-cli projects list --project-id p_12345"},
		},
		Recipes: []Recipe{
			{Title: "List users", Command: "narrative-recipes-pp-cli users list --pcgs-no 1106065"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "users_list.go")
	assert.Contains(t, source, `"  narrative-recipes-pp-cli users list --pcgs-no 1106065"`)
	assert.NotContains(t, source, "--pcgs-no example-value")
}

func TestGeneratedPromotedCommandExamplePrefersNarrativeQuickStart(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-promoted")
	apiSpec.Resources["lookup"] = spec.Resource{
		Description: "Lookup",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/lookup",
				Description: "Lookup an item",
				Body: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-promoted-pp-cli lookup --pcgs-no 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_lookup.go")
	assert.Contains(t, source, `"  narrative-promoted-pp-cli lookup --pcgs-no 7356"`)
	assert.NotContains(t, source, "--pcgs-no example-value")
}

func TestGeneratedCommandExampleEscapesQuotedNarrativeArgs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-quotes")
	apiSpec.Resources["search"] = spec.Resource{
		Description: "Search",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/search",
				Description: "Search items",
				Params: []spec.Param{
					{Name: "query", Type: "string", Required: true, Description: "Search query"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/search/{id}",
				Description: "Get a search result",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: `narrative-quotes-pp-cli search list --query "peace dollar"`},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "search_list.go")
	assert.Contains(t, source, `"  narrative-quotes-pp-cli search list --query \"peace dollar\""`)
	assert.NotContains(t, source, "--query example-value")
}

func TestGeneratedCommandExampleMatchesNarrativeWithLeadingGlobalFlag(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-global-flag")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Params: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-global-flag-pp-cli --json users list --pcgs-no 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "users_list.go")
	assert.Contains(t, source, `"  narrative-global-flag-pp-cli --json users list --pcgs-no 7356"`)
	assert.NotContains(t, source, "--pcgs-no example-value")
}

func TestGeneratedCommandExampleMatchesNarrativeAlias(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-alias")
	apiSpec.Resources["accounts"] = spec.Resource{
		Description: "Accounts",
		Endpoints: map[string]spec.Endpoint{
			"get-account": {
				Method:      "GET",
				Path:        "/accounts/{accountId}",
				Description: "Get account",
				Alias:       "get",
				Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
			},
			"list": {
				Method:      "GET",
				Path:        "/accounts",
				Description: "List accounts",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-alias-pp-cli accounts get acct_123"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "accounts_get-account.go")
	assert.Contains(t, source, `"  narrative-alias-pp-cli accounts get acct_123"`)
	assert.NotContains(t, source, "550e8400-e29b-41d4-a716-446655440000")
}

func TestGeneratedCommandExampleMatchesLocalFlagBeforePositional(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-local-flag")
	apiSpec.Resources["customers"] = spec.Resource{
		Description: "Customers",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/customers/{id}",
				Description: "Get customer",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Positional: true},
					{Name: "expand", Type: "string", Description: "Expansion"},
				},
			},
			"list": {
				Method:      "GET",
				Path:        "/customers",
				Description: "List customers",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-local-flag-pp-cli customers get --expand subscriptions cus_123"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "customers_get.go")
	assert.Contains(t, source, `"  narrative-local-flag-pp-cli customers get --expand subscriptions cus_123"`)
	assert.NotContains(t, source, "550e8400-e29b-41d4-a716-446655440000")
}

func TestGeneratedCommandExampleUsesMatchedNarrativeChainSegment(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-chain")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/users",
				Description: "List users",
				Params: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user",
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-chain-pp-cli users list --pcgs-no 7356 && narrative-chain-pp-cli users get 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "users_list.go")
	assert.Contains(t, source, `"  narrative-chain-pp-cli users list --pcgs-no 7356"`)
	assert.NotContains(t, source, "&& narrative-chain-pp-cli users get")
	assert.NotContains(t, source, "--pcgs-no example-value")
}

func TestGeneratedPromotedCommandExampleRejectsNarrativeChildCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-promoted-child")
	apiSpec.Resources["account"] = spec.Resource{
		Description: "Account",
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
				Description: "Cards",
				Endpoints: map[string]spec.Endpoint{
					"get-account": {
						Method:      "GET",
						Path:        "/account/{accountId}/cards",
						Description: "Get cards",
						Params:      []spec.Param{{Name: "accountId", Type: "string", Required: true, Positional: true}},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-promoted-child-pp-cli account cards get-account 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_account.go")
	assert.Contains(t, source, `"  narrative-promoted-child-pp-cli account 550e8400-e29b-41d4-a716-446655440000"`)
	assert.NotContains(t, source, "account cards get-account")
}

func TestGeneratedPromotedCommandExampleRejectsUnpromotedNarrativePath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrative-unpromoted-path")
	apiSpec.Resources["lookup"] = spec.Resource{
		Description: "Lookup",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/lookup",
				Description: "Lookup an item",
				Body: []spec.Param{
					{Name: "pcgs_no", FlagName: "pcgs-no", Type: "string", Required: true, Description: "PCGS number"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "narrative-unpromoted-path-pp-cli lookup create --pcgs-no 7356"},
		},
	}
	require.NoError(t, gen.Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_lookup.go")
	assert.Contains(t, source, `"  narrative-unpromoted-path-pp-cli lookup --pcgs-no example-value"`)
	assert.NotContains(t, source, `"  narrative-unpromoted-path-pp-cli lookup create --pcgs-no 7356"`)
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
	assert.Regexp(t, `Use:\s+"payment-plan"`, workflowSrc)
	assert.Contains(t, workflowSrc, `base = []string{"treasury-pp-cli", "account", "transactions", "create"}`)
	assert.Regexp(t, `"dry_run_command":\s+append\(append\(\[\]string\{\}, base\.\.\.\), "--dry-run", "--agent"\)`, workflowSrc)
	assert.Regexp(t, `"execute_command":\s+append\(\[\]string\{\}, base\.\.\.\)`, workflowSrc)

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

// TestGeneratedOutput_WorkflowArchiveDelegatesToSyncResource pins issue
// #1077's fix: workflow archive must delegate to syncResource (so it
// inherits the spec-driven path, envelope unwrap, and pagination params),
// and the archive subcommand must only emit when Sync is enabled.
// A regression that brings back the inline c.Get("/"+resource), bare
// json.Unmarshal into []json.RawMessage, or hardcoded params["after"]
// would silently re-introduce three P1 bugs across the printed library.
func TestGeneratedOutput_WorkflowArchiveDelegatesToSyncResource(t *testing.T) {
	t.Parallel()

	t.Run("sync_enabled emits archive delegating to syncResource", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("archive-sync-on")
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
		require.NoError(t, gen.Generate())

		workflowGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
		require.NoError(t, err)
		src := string(workflowGo)
		syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
		require.NoError(t, err)
		syncSrc := string(syncGo)

		assert.Contains(t, src, "func newWorkflowArchiveCmd(flags *rootFlags) *cobra.Command",
			"archive command must be emitted when Sync is enabled")
		assert.Contains(t, src, "syncResource(",
			"archive must delegate to syncResource so it inherits the hardened sync loop")
		assert.Contains(t, src, "syncEventWriter := cmd.OutOrStdout()",
			"archive should default syncResource JSON events to the command stdout writer for non-json wrapper runs")
		assert.Contains(t, src, "syncEventWriter = cmd.ErrOrStderr()",
			"archive --json must route syncResource event NDJSON away from stdout")
		assert.Contains(t, src, `nil, syncEventWriter)`,
			"archive must pass the wrapper-selected event writer into syncResource")
		assert.Contains(t, syncSrc, "syncEvents io.Writer",
			"syncResource should expose an event writer parameter for wrapper callers")
		assert.Contains(t, syncSrc, "syncEventWriter := cmd.OutOrStdout()",
			"direct sync should honor Cobra's stdout writer")
		assert.Contains(t, syncSrc, "userParams, syncEventWriter)",
			"direct sync --json should keep emitting NDJSON events on stdout")
		assert.Contains(t, syncSrc, `fmt.Fprintf(syncEvents, `+"`"+`{"event":"sync_start"`,
			"syncResource events should write through the selected event writer")

		assert.NotContains(t, src, `c.Get("/"+resource`,
			"archive must not hardcode `/<resource>` paths (#1077 Bug 1)")
		assert.NotContains(t, src, `json.Unmarshal(data, &items)`,
			"archive must not bare-unmarshal arrays; use extractPageItems via syncResource (#1077 Bug 2)")
		assert.NotContains(t, src, `params["after"] = cursor`,
			"archive must not hardcode `?after=` pagination; use determinePaginationDefaults via syncResource (#1077 Bug 3)")
	})

	t.Run("sync_disabled drops archive subcommand", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("archive-sync-off")
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{Store: true}
		require.NoError(t, gen.Generate())

		workflowGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
		require.NoError(t, err)
		src := string(workflowGo)

		assert.NotContains(t, src, "func newWorkflowArchiveCmd",
			"archive must not be emitted when Sync is disabled (syncResource is absent)")
		assert.NotContains(t, src, "newWorkflowArchiveCmd(flags)",
			"archive must not be registered when Sync is disabled")
		assert.Contains(t, src, "func newWorkflowStatusCmd",
			"status subcommand must still be emitted")
	})
}

func TestGeneratedOutput_WorkflowArchiveJSONKeepsSyncEventsOffStdout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/items", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"item-1","name":"one"}]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("archive-json-runtime")
	apiSpec.BaseURL = server.URL
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Manage items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
				Response:    spec.ResponseDef{Type: "array"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	archiveDB := filepath.Join(t.TempDir(), "archive.db")
	stdout, stderr := runGeneratedBinary(t, binaryPath, "workflow", "archive", "--json", "--db", archiveDB)
	assert.NotContains(t, stdout, `"event":"sync_`,
		"workflow archive --json stdout must be a single JSON document, not sync NDJSON")
	assert.Contains(t, stderr, `"event":"sync_start"`,
		"workflow archive --json should still surface sync progress on stderr")
	var archiveSummary map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &archiveSummary),
		"workflow archive --json stdout should be exactly one JSON document")

	syncJSONDB := filepath.Join(t.TempDir(), "sync-json.db")
	stdout, stderr = runGeneratedBinary(t, binaryPath, "sync", "--json", "--db", syncJSONDB)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, `"event":"sync_start"`,
		"direct sync --json should keep streaming sync events to stdout")
	assert.Contains(t, stdout, `"event":"sync_complete"`)
	assert.Contains(t, stdout, `"event":"sync_summary"`)

	syncHumanDB := filepath.Join(t.TempDir(), "sync-human.db")
	stdout, stderr = runGeneratedBinary(t, binaryPath, "sync", "--human-friendly", "--db", syncHumanDB)
	assert.NotContains(t, stdout, `"event":"sync_`,
		"human-friendly sync should not emit sync NDJSON on stdout")
	assert.Contains(t, stderr, "Sync complete:")
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

// A resource named `test` produces `promoted_test.go`; Go treats *_test.go as a
// test file and excludes it from the normal package build, so root.go's
// reference to newTestPromotedCmd fails to compile. Mirrors the safe-stem fix
// that #1021 applied to `<resource>_<verb>.go`.
func TestGeneratedOutput_PromotedCommand_TestResourceCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promotedtest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"PT_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/promotedtest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"test": {
				Description: "Single-endpoint resource whose name collides with Go's *_test.go convention",
				Endpoints: map[string]spec.Endpoint{
					"generate": {Method: "POST", Path: "/test/generate", Description: "Generate a test"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promotedtest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_test.go"),
		"promoted_test.go is excluded from go build; safeResourceFileStem must rewrite to promoted_test_cmd.go")
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_test_cmd.go"))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedOutput_ResourceParentsHiddenWhenAPIBrowserGenerated(t *testing.T) {
	t.Parallel()

	// Multi-endpoint resource -> parent group; single-endpoint resource -> promoted command.
	// The promoted command's presence is what triggers api_discovery.go emission, and the
	// api browser's RunE filters on child.Hidden. Without this fix the browser was empty by
	// construction (issue #872).
	apiSpec := &spec.APISpec{
		Name:    "hiddentest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"HT_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/hiddentest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Manage orders",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/orders", Description: "List orders"},
					"create": {Method: "POST", Path: "/orders", Description: "Create order"},
				},
			},
			"customers": {
				Description: "Single-endpoint customers resource (gets promoted)",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/customers", Description: "List customers"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "hiddentest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "api_discovery.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_customers.go"))

	orders, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "orders.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Hidden:\s+true`, string(orders),
		"raw resource parent must be Hidden so the api browser finds it")
}

func TestGeneratedOutput_ResourceParentsNotHiddenWithoutAPIBrowser(t *testing.T) {
	t.Parallel()

	// Without any single-endpoint resource to promote, api_discovery.go is not generated;
	// hiding the resources in that case would just collapse --help without giving users
	// a way to list them, so the parent files stay visible.
	apiSpec := &spec.APISpec{
		Name:    "novisibletest",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"NV_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/novisibletest-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Manage orders",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/orders", Description: "List orders"},
					"create": {Method: "POST", Path: "/orders", Description: "Create order"},
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

	outputDir := filepath.Join(t.TempDir(), "novisibletest-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "api_discovery.go"))

	orders, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "orders.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(orders), "Hidden: true",
		"raw resource parent must not be Hidden when no api browser is generated")
}

func TestGeneratedOutput_AgentContextIncludesHiddenResourceGroups(t *testing.T) {
	t.Parallel()

	// Cobra's Hidden flag is a --help curation tool; the agent-context surface
	// must still enumerate hidden resource parents and their endpoint subcommands
	// so agents can reach every action a CLI user could.
	apiSpec := &spec.APISpec{
		Name:    "agentctxhide",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", Header: "X-Api-Key", EnvVars: []string{"AC_API_KEY"}},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/agentctxhide-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Manage orders",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/orders", Description: "List orders"},
					"create": {Method: "POST", Path: "/orders", Description: "Create order"},
				},
			},
			"customers": {
				Description: "Single-endpoint customers resource (gets promoted)",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/customers", Description: "List customers"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "agentctxhide-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "agentctxhide-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/agentctxhide-pp-cli")

	out, err := exec.Command(binaryPath, "agent-context").Output()
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out, &payload))

	orders := findAgentContextCommand(payload["commands"], func(c map[string]any) bool {
		return c["name"] == "orders"
	})
	require.NotNil(t, orders, "hidden resource parent must appear in agent-context")
	subs, ok := orders["subcommands"].([]any)
	require.True(t, ok, "hidden resource parent must report its endpoint subcommands")
	assert.NotEmpty(t, subs, "hidden resource parent must report its endpoint subcommands")
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

// Adversarial Instructions values must not break generated Go. The auth setup,
// helpers (401/403 hint paths), and MCP tool description templates all embed
// .Auth.Instructions inside Go string literals; without %q escaping a value
// containing " or \ produces a syntax error at template-render time.
func TestGeneratedHelpers_AuthInstructionsWithSpecialChars_Compiles(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "instrescape",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:         "api_key",
			Header:       "Authorization",
			In:           "header",
			EnvVars:      []string{"INSTRESCAPE_API_KEY"},
			KeyURL:       "https://example.com/keys",
			Instructions: `Settings → "Personal access tokens" → \Generate new\`,
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/instrescape-pp-cli/config.toml",
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

	outputDir := filepath.Join(t.TempDir(), "instrescape-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

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
	assert.Contains(t, content, `c.GetWithHeaders(cmd.Context(), verifyPath`)
	assert.NotContains(t, content, `&http.Client{`)
	// When verify_path is set, HTTP 401 keeps the strict "invalid" verdict.
	// 403 is handled separately as scope-limited; see
	// TestDoctorClassifiesHTTP401AsInvalidAnd403AsScopeLimited.
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
	assert.Contains(t, doctorSrc, `reachBody, reachErr := c.Get(cmd.Context(), healthPath, nil)`)
	assert.NotContains(t, doctorSrc, `reachBody, reachErr := c.Get(cmd.Context(), "/", nil)`)
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

func TestGeneratedDoctor_NoVerifyPathReportsCredentialsPresent(t *testing.T) {
	t.Parallel()

	// Without auth.verify_path, doctor still checks API reachability, but it
	// must not turn any bare base URL response into a credential-validity claim.
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

	// Without spec verify_path, doctor reports local credential presence
	// without pretending the API accepted the key. The "not verified" copy
	// now suggests a read command (resolved at doctor-time from the cobra
	// tree) instead of pointing the user at a spec field they don't own.
	assert.NotContains(t, content, `verifyPath := "/"`)
	assert.NotContains(t, content, `c.GetWithHeaders(cmd.Context(), verifyPath`)
	assert.NotContains(t, content, `&http.Client{`)
	assert.Contains(t, content, `"present, not verified. Run `)
	assert.NotContains(t, content, `set auth.verify_path in spec`,
		"the old WARN copy that scolded the user about a spec field must be gone")
	assert.NotContains(t, content, `"inconclusive (HTTP %d from base URL — set auth.verify_path in spec for a definitive probe)"`)
	assert.NotContains(t, content, `"invalid (HTTP %d) — check your credentials"`)
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
	// Should NOT contain auth_simple template indicators. newAuthSetupCmd
	// is the auth_simple signature — auth_browser uses newAuthLoginCmd
	// for the cookie/chrome flow instead. (set-token used to be the
	// proxy here, but auth_browser now also emits it as the Auth0-SPA
	// escape hatch — retro #1598 WU-3a.)
	assert.NotContains(t, content, "newAuthSetupCmd")

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

// TestGenerate_CookieAuthWindowsCompatibility asserts the cookie-auth template
// emits a portable Python resolver and a Windows-aware fallback for the
// `auth login --chrome` flow. Regression for issue #1101: hardcoded `python3`
// and a non-Windows-friendly install hint made every cookie-auth CLI unusable
// on Windows.
func TestGenerate_CookieAuthWindowsCompatibility(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "winapp",
		Version: "0.1.0",
		BaseURL: "https://app.example.com",
		Auth: spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: ".example.com",
			EnvVars:      []string{"WINAPP_COOKIES"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/winapp-pp-cli/config.toml",
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

	outputDir := filepath.Join(t.TempDir(), "winapp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	auth, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(auth)

	// Python resolver tries all three common binary names rather than only `python3`.
	// Match the struct-literal needles so `"python"` isn't vacuously satisfied as
	// a substring of `"python3"`.
	assert.Contains(t, content, "resolvePythonBinary")
	assert.Contains(t, content, `{"python3", nil}`)
	assert.Contains(t, content, `{"python", nil}`)
	assert.Contains(t, content, `{"py", []string{"-3"}}`)

	// pycookiecheat is skipped on Windows because it raises OSError there.
	assert.Contains(t, content, `runtime.GOOS != "windows"`)
	assert.Contains(t, content, "pycookiecheat does not support Windows")

	// The resolved (bin, args) pair is carried on cookieTool so detection and
	// extraction cannot disagree about which interpreter to invoke.
	assert.Contains(t, content, "type cookieTool struct")
	assert.Contains(t, content, "pyBin")
	assert.Contains(t, content, "pyArgs")
	assert.NotContains(t, content, `exec.Command("python3", "-c", script)`)
	assert.Contains(t, content, `exec.Command(tool.pyBin,`)

	// Windows users get a workable next step instead of the Unix install hint.
	assert.Contains(t, content, "auth login --browser")
	assert.Contains(t, content, "cookie-scoop-cli")
}

// TestGenerate_CookieAuthFiltersAllowlistOnLogin pins that `auth login --chrome`
// stores only the cookies named in spec.auth.cookies when an allowlist is
// declared, and falls back to the unfiltered blob when the allowlist is
// empty (legacy specs).
func TestGenerate_CookieAuthFiltersAllowlistOnLogin(t *testing.T) {
	t.Parallel()

	t.Run("allowlist declared filters extracted cookies", func(t *testing.T) {
		t.Parallel()
		apiSpec := &spec.APISpec{
			Name:    "filterapp",
			Version: "0.1.0",
			BaseURL: "https://app.example.com",
			Auth: spec.AuthConfig{
				Type:         "cookie",
				Header:       "Cookie",
				In:           "cookie",
				CookieDomain: ".example.com",
				Cookies:      []string{"session_id"},
				EnvVars:      []string{"FILTERAPP_COOKIES"},
			},
			Config: spec.ConfigSpec{Format: "toml"},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/api/items", Description: "List items"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "filterapp-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		auth, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
		content := string(auth)

		assert.Contains(t, content, `requiredCookies := []string{"session_id"}`)
		assert.Contains(t, content, "cookieMap := parseCookieString(cookies)")
		assert.Contains(t, content, `cookies = strings.Join(kept, "; ")`)
		assert.Contains(t, content, `cookie %q not found for %s`)

		runGoCommand(t, outputDir, "build", "./...")
	})

	t.Run("empty allowlist preserves legacy behavior", func(t *testing.T) {
		t.Parallel()
		apiSpec := &spec.APISpec{
			Name:    "legacycookieapp",
			Version: "0.1.0",
			BaseURL: "https://app.example.com",
			Auth: spec.AuthConfig{
				Type:         "cookie",
				Header:       "Cookie",
				In:           "cookie",
				CookieDomain: ".example.com",
				EnvVars:      []string{"LEGACYCOOKIEAPP_COOKIES"},
			},
			Config: spec.ConfigSpec{Format: "toml"},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/api/items", Description: "List items"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "legacycookieapp-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		auth, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
		content := string(auth)

		assert.NotContains(t, content, "requiredCookies := []string{")
		assert.NotContains(t, content, `cookies = strings.Join(kept, "; ")`)
	})

	t.Run("refresh path also filters by allowlist", func(t *testing.T) {
		t.Parallel()
		apiSpec := &spec.APISpec{
			Name:    "refreshfilterapp",
			Version: "0.1.0",
			BaseURL: "https://app.example.com",
			Auth: spec.AuthConfig{
				Type:                           "cookie",
				Header:                         "Cookie",
				In:                             "cookie",
				CookieDomain:                   ".example.com",
				Cookies:                        []string{"session_id"},
				EnvVars:                        []string{"REFRESHFILTERAPP_COOKIES"},
				RequiresBrowserSession:         true,
				BrowserSessionValidationPath:   "/api/items",
				BrowserSessionValidationMethod: "GET",
			},
			Config: spec.ConfigSpec{Format: "toml"},
			Resources: map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/api/items", Description: "List items"},
					},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "refreshfilterapp-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		auth, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
		require.NoError(t, err)
		content := string(auth)

		require.Contains(t, content, "func refreshStoredBrowserCookies")
		_, refresh, _ := strings.Cut(content, "func refreshStoredBrowserCookies")
		// Without the fix, the refresh body has SaveTokens(cookies) with no
		// prior filter — the second SaveTokens call would then sit before any
		// allowlist parsing in the function.
		assert.Contains(t, refresh, `requiredCookies := []string{"session_id"}`)
		assert.Contains(t, refresh, `cookies = strings.Join(kept, "; ")`)

		runGoCommand(t, outputDir, "build", "./...")
	})
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
	assert.Contains(t, string(standardClient), `req.Header.Set("Accept", "application/json")`)
	assert.Contains(t, string(standardClient), `if req.Header.Get("Accept") == "" {`)

	browserDir := filepath.Join(t.TempDir(), "browser-pp-cli")
	browserSpec := baseSpec("browser")
	browserSpec.HTTPTransport = spec.HTTPTransportBrowserChrome
	require.NoError(t, New(browserSpec, browserDir).Generate())
	browserClient, err := os.ReadFile(filepath.Join(browserDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(browserClient), `req.Header.Set("User-Agent"`)
	// surf's Chrome impersonation manages the Accept header alongside
	// User-Agent; the generator must not emit a competing default for either.
	assert.NotContains(t, string(browserClient), `req.Header.Set("Accept",`)
	// auth.go is not emitted for auth.type:none specs (see Generator.renderAuthFiles).
	// Stronger assertion than the previous "no newAuthRefreshCmd in auth.go": there's
	// no auth.go at all for no-auth CLIs.
	_, err = os.Stat(filepath.Join(browserDir, "internal", "cli", "auth.go"))
	assert.True(t, os.IsNotExist(err), "auth.go should not be emitted for auth.type:none specs")
}

func TestGenerateRequiredAcceptHeaderBeatsDefaultAccept(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("acceptoverride")
	apiSpec.RequiredHeaders = []spec.RequiredHeader{
		{Name: "Accept", Value: "application/vnd.api.v3+json"},
	}
	apiSpec.Auth.VerifyPath = "/items"

	outputDir := filepath.Join(t.TempDir(), "acceptoverride-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	requiredIdx := strings.Index(clientSrc, `req.Header.Set("Accept", "application/vnd.api.v3+json")`)
	defaultIdx := strings.Index(clientSrc, `req.Header.Set("Accept", "application/json")`)
	require.GreaterOrEqual(t, requiredIdx, 0, "spec-required Accept header must be emitted")
	require.GreaterOrEqual(t, defaultIdx, 0, "Accept fallback default must still be emitted")
	assert.Less(t, requiredIdx, defaultIdx, "spec-required Accept must be set before the if-empty default")
	assert.Contains(t, clientSrc, `if req.Header.Get("Accept") == "" {`)
}

func TestGenerateRequiredUserAgentHeaderBeatsDefaultUserAgent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("browserheaders")
	apiSpec.RequiredHeaders = []spec.RequiredHeader{
		{Name: "User-Agent", Value: "Mozilla/5.0 Browser Sniff"},
		{Name: "Referer", Value: "https://www.example.com/"},
	}
	apiSpec.Auth.VerifyPath = "/items"

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
	// Multi-endpoint resources are now Hidden so the generated `api` browser can
	// surface them; --help stays curated. Direct invocation and the api browser
	// listing must both still work.
	assert.NotContains(t, string(helpOut), "graphql")
	apiOut, err := exec.Command(binaryPath, "api").CombinedOutput()
	require.NoError(t, err, string(apiOut))
	assert.Contains(t, string(apiOut), "products")
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
	// Should NOT contain auth_simple template signature. newAuthSetupCmd
	// is auth_simple-only — auth_browser uses newAuthLoginCmd instead.
	// (set-token used to be the proxy, but auth_browser now also emits
	// it as the Auth0-SPA escape hatch — retro #1598 WU-3a.)
	assert.NotContains(t, content, "newAuthSetupCmd")

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

	// --resources must filter the dependent fan-out the same way it filters
	// flat resources. The capture must happen before defaults expand the
	// slice, and the filter must flow through to syncDependentResources
	// where it gates each dependent.
	assert.Contains(t, syncContent, "parentFilter := append([]string(nil), resources...)",
		"sync.go should capture user --resources filter before default expansion")
	assert.Contains(t, syncContent, "effectiveLatestOnly, parentFilter",
		"sync.go should pass the captured filter into syncDependentResources")
	assert.Contains(t, syncContent, "parentFilter []string",
		"syncDependentResources should accept a parent filter")
	assert.Contains(t, syncContent, "!allow[dep.ParentTable] && !allow[dep.Name]",
		"syncDependentResources should gate dependents on parent name or dep name")

	// The generated project should compile and the generated store tests
	// should pass — including TestUpsertBatch_SetsMessagesParentID, which
	// verifies dependent-resource sync fills the typed parent_id column
	// (issue #268).
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestGenerateReservedWordResourceTableNamesCompileAndMigrate(t *testing.T) {
	t.Parallel()

	// A spec whose resource or response field snake_cases to a SQL
	// reserved word must still produce a CLI that builds and whose store
	// migration succeeds. The single fixture exercises three regression
	// classes in one compile cycle:
	//
	//   - "references" as a typed top-level resource: non-strict keyword.
	//     Surfaces as a Go compile failure if safeSQLName ever leaks into
	//     a Go-string context (backfillColumns slice, upgrade-path
	//     t.Fatalf format string).
	//   - "add" as a typed top-level resource: strict-reserved keyword.
	//     A typed CREATE TABLE for `add` is emitted into the migrations
	//     slice. SQLite rejects that unquoted with `near "add": syntax
	//     error`, so migrate() — called from every test in the generated
	//     store package — fails at parse time without quoting.
	//   - "from" as a response field on the shared type: strict-reserved
	//     keyword in column position. The columnNames helper feeds INSERT
	//     INTO and the CREATE TABLE column list; both fail at parse time
	//     unless the column identifier is quoted.
	//
	// Top-level resources are used deliberately. Sub-resources would emit
	// CREATE TABLE statements too, but the generated upsert_batch_test
	// has a NOT NULL constraint bug on FK columns that masks the keyword
	// regression with a different failure.
	mkField := func(name string) spec.TypeField {
		return spec.TypeField{Name: name, Type: "string"}
	}
	endpoints := func(path string) map[string]spec.Endpoint {
		return map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        path,
				Description: "List items",
				Response:    spec.ResponseDef{Type: "array", Item: "Item"},
				Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
			},
		}
	}
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
			"documents":  {Description: "Documents", Endpoints: endpoints("/documents")},
			"references": {Description: "References", Endpoints: endpoints("/references")},
			"add":        {Description: "Add operations", Endpoints: endpoints("/add")},
		},
		Types: map[string]spec.TypeDef{
			"Item": {
				Fields: []spec.TypeField{
					mkField("id"),
					mkField("title"),
					mkField("body"),
					// `from` is a SQLite strict-reserved keyword,
					// exercising the column-identifier path through
					// columnNames / updateSet.
					mkField("from"),
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	store := string(storeSrc)

	// Cheap pre-check: the bug-shaped strings must appear in the emitted
	// SQL so the compile-and-migrate gates below have something to catch.
	// A previous iteration of this test left `add` without a typed column
	// set — gravity stayed below the typed-table threshold and no CREATE
	// TABLE for `add` was emitted, leaving the regression unreached.
	assert.Contains(t, store, `CREATE TABLE IF NOT EXISTS "add"`,
		"strict-reserved table name must emit a quoted CREATE TABLE")
	assert.Contains(t, store, `CREATE TABLE IF NOT EXISTS "references"`,
		"non-strict-reserved table name must emit a quoted CREATE TABLE")
	assert.Contains(t, store, `"from" TEXT`,
		"strict-reserved column identifier must be quoted in the CREATE TABLE column list")
	assert.Contains(t, store, `INSERT INTO "add"`,
		"strict-reserved table name must be quoted in the upsert INSERT")

	// The generated store.go must compile (no embedded "" leaking into a
	// Go-string context) and the per-table migration must run cleanly —
	// the SQLite parse failure surfaces inside the store tests' migrate()
	// call.
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
// sync command (a) defaults --max-pages to 0 so normal runs are unlimited,
// with a bounded dogfood default only when the operator did not pass the flag,
// (b) emits a
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

	// (a) Default --max-pages is 0 (unlimited) for normal users. Dogfood
	// keeps a bounded default unless the operator explicitly passed the flag.
	assert.Contains(t, syncContent, `cmd.Flags().IntVar(&maxPages, "max-pages", 0,`,
		"sync.go must declare --max-pages with default 0")
	assert.Contains(t, syncContent, `if cliutil.IsDogfoodEnv() && !cmd.Flags().Changed("max-pages")`,
		"sync.go must bound dogfood syncs only when --max-pages was not explicitly set")
	assert.Contains(t, syncContent, `maxPages = 10`,
		"sync.go must keep a dogfood-only page cap")
	assert.NotContains(t, syncContent, `cmd.Flags().IntVar(&maxPages, "max-pages", 100,`,
		"sync.go must not retain the old finite 100-page default")
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

	// (b3) Issue #928: --latest-only pins maxPages=1 programmatically, so
	// the cap is hit on every paginated resource by user intent rather
	// than anomaly. Both cap-hit emit blocks must sit inside an
	// `if !latestOnly` guard, otherwise the warning flood masks real
	// sync_anomaly / sync_error events emitted by the same run.
	guardBefore := func(marker string) string {
		idx := strings.Index(syncContent, marker)
		require.NotEqualf(t, -1, idx, "marker %q must appear in generated sync.go", marker)
		return syncContent[max(0, idx-400):idx]
	}
	assert.Contains(t,
		guardBefore(`"resource":"%s","reason":"max_pages_cap_hit"`),
		"if !latestOnly {",
		"flat-path cap-hit emit must be guarded by `if !latestOnly` (issue #928)")
	assert.Contains(t,
		guardBefore(`"resource":"%s","parent":"%s","reason":"max_pages_cap_hit"`),
		"if !latestOnly {",
		"dependent-resource cap-hit emit must be guarded by `if !latestOnly` (issue #928)")

	// (b4) The cap-hit guard must consume an effective value derived from
	// both --latest-only AND --since. When --since is set, --latest-only is
	// already a no-op for the maxPages pin (block at sync.go.tmpl ~154),
	// and any cap hit reflects an explicit operator limit or the dogfood
	// safety limit, a real anomaly worth surfacing. Passing the raw
	// --latest-only flag value would
	// silently suppress legitimate warnings on the --latest-only --since
	// combined path. Pin the derivation and the callsites that consume it.
	assert.Contains(t, syncContent,
		`effectiveLatestOnly := latestOnly && since == ""`,
		"sync.go must derive effectiveLatestOnly so --since overrides re-enable cap-hit warnings (issue #928 Greptile follow-up)")
	assert.Contains(t, syncContent,
		"maxPages, effectiveLatestOnly",
		"syncResource call must pass effectiveLatestOnly, not the raw --latest-only flag (issue #928 Greptile follow-up)")
	assert.Contains(t, syncContent,
		"maxPages, effectiveLatestOnly, parentFilter",
		"syncDependentResources call must pass effectiveLatestOnly, not the raw --latest-only flag (issue #928 Greptile follow-up)")

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

// assertVerifyEnvConcurrencyPin pins the verify-mode worker-pool override
// in generated sync.go: cliutil import present, IsVerifyEnv() guard pins
// concurrency to 1, and the guard sits after the default-resolution block.
// Ordering is not load-bearing for correctness today (the default block's
// `if concurrency < 1 { concurrency = 4 }` is a no-op when concurrency is
// already 1), but pinning it as a defensive guard prevents future template
// churn from drifting the placement and obscuring the worker-pool comment
// block this override controls.
func assertVerifyEnvConcurrencyPin(t *testing.T, syncContent, cliutilImportPath, label string) {
	t.Helper()
	assert.Contains(t, syncContent, `"`+cliutilImportPath+`"`,
		label+": sync.go must import internal/cliutil to call IsVerifyEnv")
	assert.Contains(t, syncContent, "if cliutil.IsVerifyEnv() {",
		label+": sync.go must consult cliutil.IsVerifyEnv() before starting the worker pool")
	assert.Regexp(t,
		`if cliutil\.IsVerifyEnv\(\) \{\s*concurrency = 1\s*\}`,
		syncContent,
		label+": verify-env block must pin concurrency to 1")

	defaultIdx := strings.Index(syncContent, "concurrency = 4")
	verifyIdx := strings.Index(syncContent, "cliutil.IsVerifyEnv()")
	require.NotEqual(t, -1, defaultIdx, label+": default-resolution block must be present")
	require.NotEqual(t, -1, verifyIdx, label+": verify-env block must be present")
	assert.Less(t, defaultIdx, verifyIdx,
		label+": verify-env override must sit after the default-resolution block (as shipped)")
}

func assertSyncDefaultConcurrency(t *testing.T, syncContent string, want int, label string) {
	t.Helper()
	assert.Contains(t, syncContent,
		fmt.Sprintf(`cmd.Flags().IntVar(&concurrency, "concurrency", %d, "Number of parallel sync workers")`, want),
		label+": --concurrency flag default must match spec rate class")
	assert.Regexp(t,
		fmt.Sprintf(`if concurrency < 1 \{\s*concurrency = %d\s*\}`, want),
		syncContent,
		label+": invalid concurrency fallback must match spec rate class")
}

// TestGeneratedSyncForcesSingleWorkerUnderVerifyEnv pins the verify-mode
// override in the REST sync template. Without it, the worker pool races on
// SQLite writes once network latency disappears, tripping SQLITE_BUSY
// despite _busy_timeout — which fails validate-narrative --full-examples
// for any quickstart or recipe that exercises sync.
func TestGeneratedSyncForcesSingleWorkerUnderVerifyEnv(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "verifysync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"VERIFYSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/verifysync-pp-cli/config.toml",
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

	assertVerifyEnvConcurrencyPin(t, string(syncGo), naming.CLI(apiSpec.Name)+"/internal/cliutil", "REST sync.go")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGeneratedGraphQLSyncForcesSingleWorkerUnderVerifyEnv pins the same
// override in the GraphQL sync template. graphql_sync.go.tmpl drives
// sync.go when isGraphQLSpec selects it; the race-on-SQLite problem under
// PRINTING_PRESS_VERIFY=1 applies the same way.
func TestGeneratedGraphQLSyncForcesSingleWorkerUnderVerifyEnv(t *testing.T) {
	t.Parallel()

	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	gen := New(gqlSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)

	assertVerifyEnvConcurrencyPin(t, string(syncGo), naming.CLI(gqlSpec.Name)+"/internal/cliutil", "GraphQL sync.go")
	assert.Contains(t, string(syncGo), "sinceTS = ts.UTC().Format(time.RFC3339)",
		"GraphQL sync --since must normalize duration-derived timestamps to UTC before query filters consume them")
	assert.Contains(t, string(syncGo), `cmd.Flags().IntVar(&maxPages, "max-pages", 0,`,
		"GraphQL sync.go must declare --max-pages with default 0")
	assert.Contains(t, string(syncGo), `if cliutil.IsDogfoodEnv() && !cmd.Flags().Changed("max-pages")`,
		"GraphQL sync.go must bound dogfood syncs only when --max-pages was not explicitly set")
	assert.Contains(t, string(syncGo), `maxPages = 10`,
		"GraphQL sync.go dogfood cap must still bound generated syncs to 10 pages")
	assert.NotContains(t, string(syncGo), `cmd.Flags().IntVar(&maxPages, "max-pages", 10,`,
		"GraphQL sync.go must not retain the old 10-page default")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedGraphQLSyncConcurrencyDefaultHonorsRateClass(t *testing.T) {
	t.Parallel()

	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)
	gqlSpec.RateClass = spec.RateClassMonthly

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	gen := New(gqlSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)

	assertSyncDefaultConcurrency(t, string(syncGo), 1, "GraphQL sync.go")
}

func TestGeneratedSyncAdvancesOffsetWhenHasMoreWithoutCursor(t *testing.T) {
	t.Parallel()

	offsetPaged := func(path string) spec.Endpoint {
		return spec.Endpoint{
			Method:      "GET",
			Path:        path,
			Description: "List records",
			Response:    spec.ResponseDef{Type: "array"},
			Pagination:  &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit", HasMoreField: "has_more"},
		}
	}
	apiSpec := &spec.APISpec{
		Name:    "offsetsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"OFFSETSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/offsetsync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": offsetPaged("/accounts"),
				},
			},
			"transactions": {
				Description: "Transactions",
				Endpoints: map[string]spec.Endpoint{
					"list": offsetPaged("/accounts/{account_id}/transactions"),
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

	assert.NotContains(t, syncContent, `if !hasMore || len(items) < pageSize.limit || nextCursor == ""`,
		"sync loops must not require an API-returned next cursor for offset pagination")
	assert.Contains(t, syncContent, `if !hasMore || len(items) < pageSize.limit {`,
		"sync loops must break only on real done signals before handling cursor advancement")
	assert.Contains(t, syncContent, `if pageSize.cursorParam == "offset" {`,
		"offset pagination must advance the cursor client-side when has_more is true")
	assert.Contains(t, syncContent, `nextCursor = strconv.Itoa(currentOffset + pageSize.limit)`,
		"offset pagination must compute the next offset from the current cursor and limit")
	assert.GreaterOrEqual(t, strings.Count(syncContent, `currentOffset, _ := strconv.Atoi(cursor)`), 2,
		"offset advancement must be emitted in both flat and dependent-resource sync loops")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGeneratedSyncGatesSinceParamPerResource pins the fix for issue #900:
// generators must only inject the incremental-cursor query parameter on
// resources whose list endpoint actually declares it. Resources that don't
// (e.g. Notion's /v1/users) get a one-shot resource_not_incremental warning
// instead of a blind ?since=... that the API rejects with 400.
func TestGeneratedSyncGatesSinceParamPerResource(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "gatedsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"GATEDSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/gatedsync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// Declares since — sync should pass it through.
			"events": {
				Description: "Event log",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/events",
						Description: "List events",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						Params: []spec.Param{
							{Name: "since", Type: "string"},
						},
					},
				},
			},
			// No temporal-filter param — sync must skip the cursor and warn.
			"users": {
				Description: "User directory",
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
			// Dependent (parameterized child) path with no temporal-filter
			// param — exercises the second warning emission site in
			// syncDependentResource so parity assertions can verify both
			// code paths emit the same shape.
			"comments": {
				Description: "Comments per event",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/events/{event_id}/comments",
						Description: "List comments for an event",
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

	// Per-resource lookup replaces the blind global determineSinceParam().
	assert.NotContains(t, syncContent, "determineSinceParam()",
		"pre-fix helper that returned a blind global param name must be gone")
	assert.Contains(t, syncContent, "func syncResourceSinceParam(resource string) string",
		"per-resource helper must be emitted")

	sinceHelperStart := strings.Index(syncContent, "func syncResourceSinceParam(resource string) string")
	require.NotEqual(t, -1, sinceHelperStart, "sync.go must emit syncResourceSinceParam")
	sinceHelperBody := syncContent[sinceHelperStart:]
	if nextFunc := strings.Index(sinceHelperBody[1:], "\nfunc "); nextFunc != -1 {
		sinceHelperBody = sinceHelperBody[:nextFunc+1]
	}

	// events declares since → switch case present with the literal param name.
	assert.Contains(t, sinceHelperBody, `case "events":`,
		"events resource must appear in the per-resource switch")
	assert.Contains(t, sinceHelperBody, `return "since"`,
		"events resource must map to its declared param name")

	// users declares no since-like param → no case for it (falls through to "").
	assert.NotContains(t, sinceHelperBody, `case "users":`,
		"users resource has no since-like param and must not appear in the switch — empty result skips the cursor")

	// Pin the full warning JSON shape so future template churn can't silently
	// drop a key, rename the event, or break the agent-facing contract.
	const fullWarning = `{"event":"sync_warning","resource":"%s","reason":"resource_not_incremental","message":"endpoint does not declare a temporal filter parameter; incremental sync has no effect for this resource"}`
	assert.Contains(t, syncContent, fullWarning,
		"warning event must preserve the full agent-facing JSON shape")
	// Both syncResource (flat) and syncDependentResource (parameterized child
	// paths) must emit the warning. Counting occurrences guards against one
	// site being deleted while a single assert.Contains still passes.
	assert.GreaterOrEqual(t, strings.Count(syncContent, fullWarning), 2,
		"warning must appear on both the flat and dependent-resource code paths")

	// Human-mode warning to stderr — silent-fallback guard for TTY users.
	assert.Contains(t, syncContent,
		`"  %s: incremental sync ignored (endpoint declares no temporal filter; falling back to full pagination)\n"`,
		"human-mode users must see a stderr warning when --since is dropped")

	// Zeroing the temporal-filter timestamp is what actually prevents the
	// blind ?since= from reaching the API. Pin both sites; without these
	// the warning emission alone is cosmetic.
	assert.Contains(t, syncContent, `effectiveSince = ""`,
		"flat path must zero effectiveSince after warning so no cursor leaks through")
	assert.Contains(t, syncContent, `depSinceTS = ""`,
		"dependent path must zero depSinceTS after warning so no cursor leaks through")

	// Build to catch template-syntax / import errors.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedSyncInjectsFieldSelectorDefaults(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "fieldsync",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"FIELDSYNC_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/fieldsync-pp-cli/config.toml",
		},
		Types: map[string]spec.TypeDef{
			"Task": {Fields: []spec.TypeField{
				{Name: "gid", Type: "string"},
				{Name: "completed", Type: "bool"},
				{Name: "assignee", Type: "object"},
				{Name: "custom_fields", Type: "array"},
			}},
		},
		Resources: map[string]spec.Resource{
			"tasks": {
				Description: "Tasks",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/tasks",
						Description: "List tasks",
						Response:    spec.ResponseDef{Type: "array", Item: "Task"},
						Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						Params: []spec.Param{{
							Name:                 "opt_fields",
							Type:                 "string",
							Purpose:              spec.ParamPurposeFieldSelector,
							FieldSelectorDefault: "gid,completed,assignee.gid,custom_fields.gid",
						}},
					},
				},
			},
			"users": {
				Description: "Users",
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
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	assert.Contains(t, syncContent, "func syncResourceFieldSelector(resource string) (string, string)",
		"sync.go must emit per-resource field-selector defaults")

	fieldSelectorStart := strings.Index(syncContent, "func syncResourceFieldSelector(resource string) (string, string)")
	require.NotEqual(t, -1, fieldSelectorStart, "sync.go must emit syncResourceFieldSelector")
	fieldSelectorBody := syncContent[fieldSelectorStart:]
	if nextFunc := strings.Index(fieldSelectorBody[1:], "\nfunc "); nextFunc != -1 {
		fieldSelectorBody = fieldSelectorBody[:nextFunc+1]
	}

	assert.Contains(t, fieldSelectorBody, `case "tasks":`,
		"tasks must appear in the field-selector switch")
	assert.Contains(t, fieldSelectorBody, `return "opt_fields", "gid,completed,assignee.gid,custom_fields.gid"`,
		"tasks must map to the spec-declared field selector and generated default")
	assert.NotContains(t, fieldSelectorBody, `case "users":`,
		"resources without a field selector must not inject an opt_fields-like query param")

	defaultIdx := strings.Index(syncContent, "fieldSelectorKey, fieldSelectorValue := syncResourceFieldSelector(resource)")
	applyIdx := strings.Index(syncContent, "userParams.applyTo(resource, params, false)")
	require.NotEqual(t, -1, defaultIdx, "syncResource must apply field-selector defaults")
	require.NotEqual(t, -1, applyIdx, "syncResource must still apply user params")
	assert.Less(t, defaultIdx, applyIdx,
		"user-supplied --param/--resource-param must override field-selector defaults")
}

func TestGeneratedSyncGatesPaginationParamsPerResource(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "mixedpaging",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MIXEDPAGING_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/mixedpaging-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/accounts",
						Description: "List accounts",
						Response:    spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "limit", Type: "integer"},
							{Name: "offset", Type: "integer"},
						},
						Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit"},
					},
				},
			},
			"transactions": {
				Description: "Transactions",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/accounts/{account_id}/transactions",
						Description: "List transactions",
						Response:    spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "limit", Type: "integer"},
							{Name: "offset", Type: "integer"},
						},
						Pagination: &spec.Pagination{Type: "offset", CursorParam: "offset", LimitParam: "limit"},
					},
				},
			},
			"categories": {
				Description: "Categories",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/categories",
						Description: "List categories",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
			"budget_settings": {
				Description: "Budget settings",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/budgets/settings",
						Description: "Get budget settings",
						Response:    spec.ResponseDef{Type: "object"},
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

	helperStart := strings.Index(syncContent, "func resourceSupportsPagination(resource string) bool")
	require.NotEqual(t, -1, helperStart, "sync.go must emit resourceSupportsPagination")
	helperBody := syncContent[helperStart:]
	if nextFunc := strings.Index(helperBody[1:], "\nfunc "); nextFunc != -1 {
		helperBody = helperBody[:nextFunc+1]
	}

	assert.Contains(t, helperBody, `case "accounts":`,
		"accounts declares limit/offset and must opt into pagination params")
	assert.Contains(t, helperBody, `case "transactions":`,
		"dependent transactions declares limit/offset and must opt into pagination params")
	assert.NotContains(t, helperBody, `case "categories":`,
		"categories declares no limit param and must not receive pagination params")
	assert.NotContains(t, helperBody, `case "budget_settings":`,
		"/budgets/settings is non-paginated and must fall through to false")
	assert.Contains(t, syncContent, `if resourceSupportsPagination(resource) {`,
		"flat sync loop must gate page-size and cursor params per resource")
	assert.Contains(t, syncContent, `if resourceSupportsPagination(dep.Name) {`,
		"dependent sync loop must gate page-size and cursor params per resource")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedSyncUsesPOSTForRPCStyleListResources(t *testing.T) {
	t.Parallel()

	var capturedMethod string
	var capturedQuery url.Values
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedQuery = r.URL.Query()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"item_1"}]}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:    "rpcpostsync",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/rpcpostsync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"getList": {
						Method:      "POST",
						Path:        "/api/items/getList",
						Description: "List items",
						Body: []spec.Param{
							{Name: "limit", Type: "integer"},
							{Name: "cursor", Type: "string"},
							{Name: "view", Type: "string", Default: "summary"},
						},
						Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
						Response:   spec.ResponseDef{Type: "object", Item: "ItemsResponse"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"ItemsResponse": {
				Fields: []spec.TypeField{{Name: "items", Type: "array"}},
			},
			"Item": {
				Fields: []spec.TypeField{{Name: "id", Type: "string"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "rpcpostsync-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/rpcpostsync-pp-cli")

	dbPath := filepath.Join(t.TempDir(), "sync.db")
	cmd := exec.Command(binaryPath, "sync", "--resources", "items", "--max-pages", "1", "--db", dbPath)
	cmd.Env = append(os.Environ(), "RPCPOSTSYNC_BASE_URL="+server.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Empty(t, capturedQuery.Get("limit"), "body-declared pagination params must not leak into the query string")
	assert.Equal(t, float64(100), capturedBody["limit"])
	assert.Equal(t, "summary", capturedBody["view"])
}

func TestGeneratedSyncTreatsEmptyWrappedPageAsSuccessfulZeroRecords(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/empty-records":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/records":
			_, _ = w.Write([]byte(`{"results":[{"id":"rec_1","name":"One"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:    "emptywrapsync",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/emptywrapsync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"empty_records": {
				Description: "Manage empty records",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/empty-records",
						Description: "List empty records",
						Response:    spec.ResponseDef{Type: "array", Item: "Record"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
			"records": {
				Description: "Manage records",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/records",
						Description: "List records",
						Response:    spec.ResponseDef{Type: "array", Item: "Record"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Record": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	behaviorTest := `package cli

import (
	"encoding/json"
	"testing"
)

func TestIsEmptyPageResponseRejectsNullSingletonFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"known wrapper empty array", ` + "`" + `{"results":[]}` + "`" + `, true},
		{"unknown wrapper empty array", ` + "`" + `{"empty":[]}` + "`" + `, true},
		{"top-level null is not an empty page", ` + "`" + `null` + "`" + `, false},
		{"known wrapper null is not an empty page", ` + "`" + `{"results":null}` + "`" + `, false},
		{"known data wrapper null is not an empty page", ` + "`" + `{"data":null}` + "`" + `, false},
		{"single null field is not an empty page", ` + "`" + `{"user":null}` + "`" + `, false},
		{"singleton object with null field is not an empty page", ` + "`" + `{"id":"rec_1","user":null}` + "`" + `, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyPageResponse(json.RawMessage(tc.body)); got != tc.want {
				t.Fatalf("isEmptyPageResponse(%s) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_empty_page_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestIsEmptyPageResponseRejectsNullSingletonFields")

	binaryPath := filepath.Join(outputDir, "emptywrapsync-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/emptywrapsync-pp-cli")

	emptyDB := filepath.Join(t.TempDir(), "empty.db")
	cmd := exec.Command(binaryPath, "--json", "sync", "--resources", "empty_records", "--db", emptyDB, "--max-pages", "1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	output := string(out)
	assert.NotContains(t, output, `"event":"sync_error"`)
	assert.Contains(t, output, `{"event":"sync_complete","resource":"empty_records","total":0`)
	assert.Contains(t, output, `{"event":"sync_summary","total_records":0,"resources":1,"success":1,"warned":0,"errored":0`)

	recordsDB := filepath.Join(t.TempDir(), "records.db")
	cmd = exec.Command(binaryPath, "--json", "sync", "--resources", "records", "--db", recordsDB, "--max-pages", "1")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	output = string(out)
	assert.NotContains(t, output, `"event":"sync_error"`)
	assert.Contains(t, output, `{"event":"sync_complete","resource":"records","total":1`)
	assert.Contains(t, output, `{"event":"sync_summary","total_records":1,"resources":1,"success":1,"warned":0,"errored":0`)
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
	// Vendor identifiers (gid, sid, uid, uuid, guid) precede `name` so
	// APIs like Asana don't fall through to a display field — see #1394.
	assert.Contains(t, storeContent,
		`var genericIDFieldFallbacks = []string{"id", "ID", "gid", "sid", "uid", "uuid", "guid", "name", "slug", "key", "code"}`,
		"store.go genericIDFieldFallbacks must include vendor identifiers before name")
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

func TestGeneratedSyncIDFieldOverridesFromMemberPathParam(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Path Param IDs
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /identity-providers:
    get:
      operationId: listIdentityProviders
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    idpId: {type: string}
                    name: {type: string}
  /identity-providers/{idpId}:
    get:
      operationId: getIdentityProvider
      parameters:
        - name: idpId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  idpId: {type: string}
                  name: {type: string}
`))
	require.NoError(t, err)
	profile := profiler.Profile(apiSpec)
	require.Len(t, profile.SyncableResources, 1)
	assert.Equal(t, "identity-providers", profile.SyncableResources[0].Name)
	assert.Equal(t, "idpId", profile.SyncableResources[0].IDField)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)

	assert.Contains(t, string(syncGo), `"identity-providers": "idpId",`)
	assert.Contains(t, string(storeGo), `"identity-providers": "idpId",`)
}

func TestGeneratedDependentSyncIDFieldOverridesFromMemberPathParam(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Dependent Path Param IDs
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /sites:
    get:
      operationId: listSites
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: {type: string}
                    name: {type: string}
  /sites/{siteId}/identity-providers:
    get:
      operationId: listSiteIdentityProviders
      parameters:
        - name: siteId
          in: path
          required: true
          schema: {type: string}
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    idpId: {type: string}
                    name: {type: string}
  /sites/{siteId}/identity-providers/{idpId}:
    get:
      operationId: getSiteIdentityProvider
      parameters:
        - name: siteId
          in: path
          required: true
          schema: {type: string}
        - name: idpId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  idpId: {type: string}
                  name: {type: string}
`))
	require.NoError(t, err)
	profile := profiler.Profile(apiSpec)
	require.Len(t, profile.DependentSyncResources, 1)
	assert.Equal(t, "identity_providers", profile.DependentSyncResources[0].Name)
	assert.Equal(t, "idpId", profile.DependentSyncResources[0].IDField)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())
	require.Len(t, gen.profile.DependentSyncResources, 1)
	assert.Equal(t, "identity_providers", gen.profile.DependentSyncResources[0].Name)
	assert.Equal(t, "idpId", gen.profile.DependentSyncResources[0].IDField)

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)

	assert.Contains(t, string(syncGo), `"identity_providers": "idpId",`)
	assert.Contains(t, string(storeGo), `"identity_providers": "idpId",`)
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
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/graphql/\{pathQueryId\}/Followers",\s*true,\s*false,\s*nil,\s*\[]mcpParamBinding\{.*WireName: "pathQueryId".*\},\s*\[]string\{[^}]*"pathQueryId"`),
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

func TestGenerateSyncIncludesSiblingListResources(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "trading",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/trading-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"portfolio": {
				Description: "Portfolio",
				Endpoints: map[string]spec.Endpoint{
					"fills": {
						Method:      "GET",
						Path:        "/portfolio/fills",
						Description: "List fills",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
					"orders": {
						Method:      "GET",
						Path:        "/portfolio/orders",
						Description: "List orders",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
					"settlements": {
						Method:      "GET",
						Path:        "/portfolio/settlements",
						Description: "List settlements",
						Response:    spec.ResponseDef{Type: "array"},
						Pagination:  &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
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
	assert.Regexp(t, `"portfolio":\s+"/portfolio/fills"`, syncContent)
	assert.Regexp(t, `"portfolio-orders":\s+"/portfolio/orders"`, syncContent)
	assert.Regexp(t, `"portfolio-settlements":\s+"/portfolio/settlements"`, syncContent)
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

func TestGenerateGraphQLListWiresOptionalQueryVariable(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:                "shopify-query",
		Description:         "Shopify Admin GraphQL query fixture",
		Version:             "2026-04",
		BaseURL:             "https://example.myshopify.com",
		GraphQLEndpointPath: "/admin/api/2026-04/graphql.json",
		Auth:                spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-query-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "Get an order",
						ResponsePath: "data.order",
						Params:       []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
						Response:     spec.ResponseDef{Type: "object", Item: "Order"},
					},
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List orders",
						ResponsePath: "data.orders.nodes",
						Params: []spec.Param{
							{Name: "first", Type: "integer", Default: 100},
							{Name: "after", Type: "string"},
							{Name: "query", Type: "string", Description: "Shopify search filter"},
						},
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
			"fulfillment-orders": {
				Description: "Fulfillment orders",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "Get a fulfillment order",
						ResponsePath: "data.fulfillmentOrder",
						Params:       []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
						Response:     spec.ResponseDef{Type: "object", Item: "FulfillmentOrder"},
					},
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List fulfillment orders",
						ResponsePath: "data.fulfillmentOrders.nodes",
						Params: []spec.Param{
							{Name: "first", Type: "integer", Default: 100},
							{Name: "after", Type: "string"},
							{Name: "before", Type: "string"},
						},
						Pagination: &spec.Pagination{
							Type:           "cursor",
							LimitParam:     "first",
							CursorParam:    "after",
							NextCursorPath: "data.fulfillmentOrders.pageInfo.endCursor",
							HasMoreField:   "data.fulfillmentOrders.pageInfo.hasNextPage",
						},
						Response: spec.ResponseDef{Type: "array", Item: "FulfillmentOrder"},
					},
				},
			},
			"customers": {
				Description: "Customers",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List customers",
						ResponsePath: "data.customers.nodes",
						Params: []spec.Param{
							{Name: "status", Type: "string"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Customer"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Order":            {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
			"FulfillmentOrder": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "status", Type: "string"}}},
			"Customer":         {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "email", Type: "string"}}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	ordersContent := generatedCLISourceContaining(t, outputDir, `variables["query"] = flagQuery`)
	assert.Contains(t, ordersContent, `cmd.Flags().StringVar(&flagQuery, "query", "", "Shopify search filter")`)
	assert.Contains(t, ordersContent, `if cmd.Flags().Changed("query") {`)
	assert.Contains(t, ordersContent, `variables["query"] = flagQuery`)

	queriesGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "queries.go"))
	require.NoError(t, err)
	queriesContent := string(queriesGo)
	assert.Contains(t, queriesContent, "query($first: Int!, $after: String, $query: String)")
	assert.Contains(t, queriesContent, "orders(first: $first, after: $after, query: $query)")
	assert.Contains(t, queriesContent, "query($first: Int!, $after: String) {\n  fulfillmentOrders(first: $first, after: $after)")
	assert.NotContains(t, queriesContent, "before: $before")
	assert.NotContains(t, queriesContent, "fulfillmentOrders(first: $first, after: $after, query: $query)")
	assert.Contains(t, queriesContent, "query {\n  customers {\n")
	assert.NotContains(t, queriesContent, "query()")
	assert.NotContains(t, queriesContent, "customers()")
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
	queryBody := generatedFunctionBody(t, graphqlGo, "func (c *Client) Query(")
	assert.Contains(t, queryBody, "c.PostQueryWithParams(ctx, graphqlEndpointPath",
		"Query must use the read-only POST helper against the rendered constant")
	assert.NotContains(t, queryBody, "c.Post(ctx, graphqlEndpointPath",
		"GraphQL reads must not use the verify-gated POST helper")

	mutateBody := generatedFunctionBody(t, graphqlGo, "func (c *Client) Mutate(")
	assert.Contains(t, mutateBody, "c.Post(ctx, graphqlEndpointPath",
		"GraphQL mutations must stay on the verify-gated POST helper")
	assert.NotContains(t, mutateBody, "c.PostQueryWithParams(ctx, graphqlEndpointPath",
		"GraphQL mutations must not use the read-only POST helper")

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

func generatedFunctionBody(t *testing.T, src, signature string) string {
	t.Helper()

	start := strings.Index(src, signature)
	require.NotEqual(t, -1, start, "generated source must contain %s", signature)

	rest := src[start:]
	next := strings.Index(rest[1:], "\nfunc ")
	if next == -1 {
		return rest
	}
	return rest[:next+1]
}

// shopifyTemplateVarsTestSpec returns a Shopify-shape APISpec used by
// EndpointTemplateVars tests. Variable names follow the {upper Name}_{upper
// var} convention: the spec's var name is "api_version" (not "version")
// because the real-world env var is SHOPIFY_API_VERSION; the URL placeholder
// mirrors the var name so both sides line up.
func shopifyTemplateVarsTestSpec() *spec.APISpec {
	return &spec.APISpec{
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

	apiSpec := shopifyTemplateVarsTestSpec()

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
	assert.Regexp(t, `"shop":\s+"SHOPIFY_SHOP"`, urlGo,
		"templateVarEnvNames must wire {shop} to SHOPIFY_SHOP")
	assert.Regexp(t, `"api_version":\s+"SHOPIFY_API_VERSION"`, urlGo,
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

func TestBuildURLSubstitutesTemplatedSchemeAndHost(t *testing.T) {
	templateVarEnvNames["protocol"] = "SHOPIFY_PROTOCOL"
	templateVarEnvNames["host"] = "SHOPIFY_HOST"
	t.Cleanup(func() {
		delete(templateVarEnvNames, "protocol")
		delete(templateVarEnvNames, "host")
	})

	vars := map[string]string{
		"protocol": "http",
		"host":     "localhost:41184",
	}
	got, err := buildURL("{protocol}://{host}", "/notes", vars)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	const want = "http://localhost:41184/notes"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	for _, tc := range []struct {
		name    string
		vars    map[string]string
		wantErr string
	}{
		{
			name:    "missing protocol",
			vars:    map[string]string{"host": "localhost:41184"},
			wantErr: "SHOPIFY_PROTOCOL not set",
		},
		{
			name:    "missing host",
			vars:    map[string]string{"protocol": "http"},
			wantErr: "SHOPIFY_HOST not set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildURL("{protocol}://{host}", "/notes", tc.vars)
			if err == nil {
				t.Fatal("expected missing template variable error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error to contain %q; got %q", tc.wantErr, err.Error())
			}
		})
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

func TestGenerateInfersEndpointTemplateVarsFromBaseURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-api")
	apiSpec.BaseURL = "https://{tenant}.api.example.com"
	require.Empty(t, apiSpec.EndpointTemplateVars)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.Equal(t, []string{"tenant"}, apiSpec.EndpointTemplateVars)
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "client", "url.go"))
	require.NoError(t, err, "url.go should be generated when BaseURL contains a placeholder")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateEndpointTemplateVarsVerifyPlaceholderFallback covers the
// PRINTING_PRESS_VERIFY=1 fallback in config.Load(): when a spec declares
// EndpointTemplateVars and the runtime env var (e.g. SHOPIFY_SHOP) is unset,
// verify mode must seed Config.TemplateVars with a "<name>_placeholder" so
// dry-run / validate-narrative / dogfood legs reach Cobra. Production
// behavior — the actionable "export X=..." error from buildURL — must stay
// unchanged when verify mode is off.
func TestGenerateEndpointTemplateVarsVerifyPlaceholderFallback(t *testing.T) {
	t.Parallel()

	apiSpec := shopifyTemplateVarsTestSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configGo := string(configGoBytes)
	assert.Contains(t, configGo, `os.Getenv("PRINTING_PRESS_VERIFY") == "1"`,
		"config Load() must consult PRINTING_PRESS_VERIFY for the template-var fallback")
	assert.Contains(t, configGo, `cfg.TemplateVars["shop"] = "shop_placeholder"`,
		"config Load() must seed the {shop} placeholder under verify mode")
	assert.Contains(t, configGo, `cfg.TemplateVars["api_version"] = "api_version_placeholder"`,
		"config Load() must seed the {api_version} placeholder under verify mode")

	// Behavioral coverage: the Load() helper must populate TemplateVars only
	// when verify mode is on, and a real env var must always win over the
	// placeholder so production runs are unaffected. The injected `go test`
	// run also serves as the compile check — a template-emitted syntax error
	// in config.go surfaces as a build failure here.
	runGoCommand(t, outputDir, "mod", "tidy")
	behaviorTest := `package config

import (
	"path/filepath"
	"testing"
)

func clearTemplateEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"SHOPIFY_SHOP", "SHOPIFY_API_VERSION", "PRINTING_PRESS_VERIFY"} {
		t.Setenv(name, "")
	}
}

func TestLoadTemplateVarsVerifyPlaceholder(t *testing.T) {
	clearTemplateEnv(t)
	t.Setenv("PRINTING_PRESS_VERIFY", "1")

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.TemplateVars["shop"]; got != "shop_placeholder" {
		t.Errorf("TemplateVars[\"shop\"] = %q, want \"shop_placeholder\"", got)
	}
	if got := cfg.TemplateVars["api_version"]; got != "api_version_placeholder" {
		t.Errorf("TemplateVars[\"api_version\"] = %q, want \"api_version_placeholder\"", got)
	}
}

func TestLoadTemplateVarsProductionUnchanged(t *testing.T) {
	clearTemplateEnv(t)

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v, ok := cfg.TemplateVars["shop"]; ok {
		t.Errorf("TemplateVars[\"shop\"] should be absent without verify mode; got %q", v)
	}
	if v, ok := cfg.TemplateVars["api_version"]; ok {
		t.Errorf("TemplateVars[\"api_version\"] should be absent without verify mode; got %q", v)
	}
}

func TestLoadTemplateVarsRealEnvWinsOverPlaceholder(t *testing.T) {
	clearTemplateEnv(t)
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("SHOPIFY_SHOP", "real-store.myshopify.com")

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.TemplateVars["shop"]; got != "real-store.myshopify.com" {
		t.Errorf("real env must beat placeholder: TemplateVars[\"shop\"] = %q", got)
	}
	if got := cfg.TemplateVars["api_version"]; got != "api_version_placeholder" {
		t.Errorf("unset companion var should still get placeholder: got %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "verify_placeholder_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestLoadTemplateVars")
}

// freshserviceTemplateVarsTestSpec returns a Freshservice-shape APISpec
// used by EndpointTemplateVars-with-defaults tests. The `{domain}`
// placeholder carries a spec-declared default so config.Load can fall
// back to a real URL when the user's env var is unset.
func freshserviceTemplateVarsTestSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:                        "freshservice",
		Description:                 "Freshservice (test fixture)",
		Version:                     "v2",
		BaseURL:                     "https://{domain}/api/v2",
		EndpointTemplateVars:        []string{"domain"},
		EndpointTemplateVarDefaults: map[string]string{"domain": "yourcompany.freshservice.com"},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"FRESHSERVICE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/freshservice-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"tickets": {
				Description: "Tickets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/tickets",
						Description:  "List tickets",
						ResponsePath: "tickets",
						Response:     spec.ResponseDef{Type: "array", Item: "Ticket"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Ticket": {Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "subject", Type: "string"},
			}},
		},
	}
}

// TestGenerateEndpointTemplateVarsDefaultFallback covers the multi-tenant
// base-URL substitution path. When servers[0].url declares a `{var}`
// placeholder with a `default:` value, config.Load must:
//   - substitute the env-var value (normalized) into Config.TemplateVars
//     when the env var is set, and
//   - fall back to the spec-declared default when the env var is unset,
//
// so doctor and verify probe a real-shaped URL instead of one with a
// literal {domain} in it. The normalizer strips scheme + trailing slash
// because users routinely paste browser-bar URLs into shell exports.
func TestGenerateEndpointTemplateVarsDefaultFallback(t *testing.T) {
	t.Parallel()

	apiSpec := freshserviceTemplateVarsTestSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configGoStr := string(configGo)
	assert.Contains(t, configGoStr, `cfg.TemplateVars["domain"] = "yourcompany.freshservice.com"`,
		"config.Load must bake the spec default as the env-var-unset fallback")
	assert.Contains(t, configGoStr, "normalizeEndpointTemplateValue",
		"config.go must emit the normalizer helper when a server-URL default is present")
	assert.NotContains(t, configGoStr, `BaseURL: "https://yourcompany.freshservice.com/api/v2"`,
		"BaseURL must keep the {domain} placeholder, not bake the default into the constant")
	assert.Contains(t, configGoStr, `BaseURL: "https://{domain}/api/v2"`,
		"BaseURL constant must carry the placeholder for runtime substitution")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	behaviorTest := `package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearFreshserviceEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"FRESHSERVICE_DOMAIN", "FRESHSERVICE_BASE_URL", "FRESHSERVICE_CONFIG", "PRINTING_PRESS_VERIFY"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

func TestLoadDomainDefaultFallback(t *testing.T) {
	clearFreshserviceEnv(t)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.TemplateVars["domain"]; got != "yourcompany.freshservice.com" {
		t.Errorf("unset env var should fall back to spec default; got %q", got)
	}
}

func TestLoadDomainEnvVarOverridesDefault(t *testing.T) {
	clearFreshserviceEnv(t)
	t.Setenv("FRESHSERVICE_DOMAIN", "acme.freshservice.com")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.TemplateVars["domain"]; got != "acme.freshservice.com" {
		t.Errorf("env var should beat default; got %q", got)
	}
}

func TestLoadDomainNormalizesPastedURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"acme.freshservice.com", "acme.freshservice.com"},
		{"https://acme.freshservice.com", "acme.freshservice.com"},
		{"https://acme.freshservice.com/", "acme.freshservice.com"},
		{"http://acme.freshservice.com//", "acme.freshservice.com"},
		{"  acme.freshservice.com  ", "acme.freshservice.com"},
		{"HTTPS://acme.freshservice.com", "acme.freshservice.com"},
		{"Https://acme.freshservice.com/", "acme.freshservice.com"},
		{"HTTP://acme.freshservice.com", "acme.freshservice.com"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			clearFreshserviceEnv(t)
			t.Setenv("FRESHSERVICE_DOMAIN", tc.input)
			cfgPath := filepath.Join(t.TempDir(), "config.toml")
			cfg, err := Load(cfgPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.TemplateVars["domain"]; got != tc.want {
				t.Errorf("Load(%q): TemplateVars[\"domain\"] = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "domain_fallback_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestLoadDomain")
}

// TestGenerateEndpointTemplateDefaultIsEscaped guards against a malicious
// or hand-edited OpenAPI spec whose server-variable default contains Go
// string-literal syntax (a stray double quote, backslash, or newline).
// Without proper %q escaping the generator emits invalid Go and the
// printed CLI fails to compile — or worse, the default becomes a vector
// for injecting Go statements into config.Load() that run on every CLI
// invocation.
func TestGenerateEndpointTemplateDefaultIsEscaped(t *testing.T) {
	t.Parallel()

	apiSpec := freshserviceTemplateVarsTestSpec()
	apiSpec.EndpointTemplateVarDefaults = map[string]string{
		"domain": `evil"; os.Exit(1); //`,
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	got := string(configGo)
	assert.Contains(t, got, `cfg.TemplateVars["domain"] = "evil\"; os.Exit(1); //"`,
		"%q escaping must quote internal double quotes so the default can never close the Go string literal")
	assert.NotContains(t, got, `cfg.TemplateVars["domain"] = "evil"; os.Exit(1); //`,
		"unescaped emission would close the string and execute os.Exit on every Load")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
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

	// --path-context is profiler-driven: emitting it on a plain API would
	// add a flag with nothing to substitute and bloat the help surface for
	// every printed CLI that does not need it (#1332).
	syncGoPath := filepath.Join(outputDir, "internal", "cli", "sync.go")
	if syncGoBytes, syncErr := os.ReadFile(syncGoPath); syncErr == nil {
		syncGo := string(syncGoBytes)
		assert.NotContains(t, syncGo, "path-context",
			"sync must not emit --path-context when EndpointTemplateVars is empty")
		assert.NotContains(t, syncGo, "pathContextFlags",
			"sync must not declare pathContextFlags when EndpointTemplateVars is empty")
	}
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
	assert.Contains(t, mcpToolsStr, `makeAPIHandler("GET", "https://geocoding-api.example.com/v1/search", true, false`,
		"geocoding MCP endpoint mirror must emit the absolute override URL")
	assert.Contains(t, mcpToolsStr, `makeAPIHandler("GET", "/forecast", true, false`,
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

func TestGenerateEndpointBaseURLOverrideRoutesToOverrideHost(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("endpoint-multihost")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.Resources["forecast"] = spec.Resource{
		Description: "Weather forecast",
		Endpoints: map[string]spec.Endpoint{
			"now": {Method: "GET", Path: "/forecast", Description: "Current"},
			"geocode": {
				Method:      "GET",
				Path:        "/search",
				BaseURL:     "https://geocoding-api.example.com/v1",
				Description: "Geocode",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	geoHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "forecast_geocode.go"))
	require.NoError(t, err)
	assert.Contains(t, string(geoHandler), `path := "https://geocoding-api.example.com/v1/search"`,
		"endpoint override must emit the absolute URL into path")

	fcHandler, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "forecast_now.go"))
	require.NoError(t, err)
	assert.Contains(t, string(fcHandler), `path := "/forecast"`,
		"sibling endpoint without override must keep the relative path")

	mcpTools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	assert.Contains(t, string(mcpTools), `makeAPIHandler("GET", "https://geocoding-api.example.com/v1/search", true, false`,
		"typed MCP endpoint tool must use the endpoint override URL")
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
	assert.Regexp(t, `Path:\s+"https://geocoding-api\.example\.com/v1/search"`, string(codeOrch),
		"code-orchestration execute must use the effective resource override URL")

	intents, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "intents.go"))
	require.NoError(t, err)
	assert.Contains(t, string(intents), `"geocoding.search": {method: "GET", path: "https://geocoding-api.example.com/v1/search"}`,
		"intent dispatch must use the effective resource override URL")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMCPMainSmallAPIDefaultsHTTP confirms that a small-API spec
// (typed-endpoint count at or below spec.DefaultRemoteTransportEndpointThreshold)
// with no mcp: block gets http compiled in alongside stdio. The default lifts
// mcp_remote_transport from 5/10 to 10/10 at zero cost: there's no extra tool,
// no extra agent-context overhead, and the same binary can now reach
// cloud-hosted agents that cannot spawn a subprocess. Issue #1603.
func TestGenerateMCPMainSmallAPIDefaultsHTTP(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	require.Empty(t, apiSpec.MCP.Transport, "baseline loops spec should not declare MCP transports")
	require.LessOrEqual(t, apiSpec.TypedEndpointCount(), spec.DefaultRemoteTransportEndpointThreshold,
		"loops fixture must stay small enough to exercise the auto-http default")

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
		`server.ServeStdio(s)`,
		`server.NewStreamableHTTPServer(s)`,
		`flag.String("transport"`,
		`PP_MCP_TRANSPORT`,
	} {
		assert.Contains(t, body, want, "small-API auto-http default should emit %q", want)
	}
}

// TestGenerateMCPMainExplicitStdioOnlyHonored covers the negative half of
// the issue #1603 acceptance criteria: an explicit `mcp.transport: [stdio]`
// is honored even at small endpoint counts. The auto-http default only fires
// when the field is empty; an opt-out should not be silently overridden.
func TestGenerateMCPMainExplicitStdioOnlyHonored(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "loops.yaml"))
	require.NoError(t, err)
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	mainPath := filepath.Join(outputDir, "cmd", naming.MCP(apiSpec.Name), "main.go")
	data, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, "server.ServeStdio(s)", "explicit stdio-only spec must still call ServeStdio")
	assert.NotContains(t, body, "flag.String", "explicit stdio-only spec must not pull in the flag package")
	assert.NotContains(t, body, "NewStreamableHTTPServer", "explicit stdio-only spec must not reference the HTTP transport")
	assert.NotContains(t, body, "PP_MCP_TRANSPORT", "explicit stdio-only spec must not reference the transport env override")
}

// TestGenerateMCPMainLargeAPIStaysStdioOnly confirms that the auto-http
// default only fires for small APIs. Above the endpoint threshold the
// generator defers to the orchestration-pattern recommendation in
// warnUnenrichedLargeMCPSurface; auto-extending transport there would be
// the wrong fix because the dominant problem is tool-count, not reach.
func TestGenerateMCPMainLargeAPIStaysStdioOnly(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:      "demo",
		BaseURL:   "https://api.example.com",
		Auth:      spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{},
	}
	// Synthesize an above-threshold typed-endpoint surface.
	r := spec.Resource{Endpoints: map[string]spec.Endpoint{}}
	for i := range spec.DefaultRemoteTransportEndpointThreshold + 1 {
		name := fmt.Sprintf("get_%d", i)
		r.Endpoints[name] = spec.Endpoint{Method: "GET", Path: fmt.Sprintf("/items/%d", i)}
	}
	apiSpec.Resources["items"] = r
	require.Greater(t, apiSpec.TypedEndpointCount(), spec.DefaultRemoteTransportEndpointThreshold,
		"synthetic spec must exceed the small-API threshold")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	mainPath := filepath.Join(outputDir, "cmd", naming.MCP(apiSpec.Name), "main.go")
	data, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, "server.ServeStdio(s)", "large-API default must still call ServeStdio")
	assert.NotContains(t, body, "NewStreamableHTTPServer", "large-API default must not auto-enable http transport")
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

// TestGenerateWaitForJobBypassesResponseCache proves that the polling loop
// in WaitForJob uses GetNoCache, not Get. The response cache is keyed by
// (path, params), so a cached non-terminal status (Pending/Busy/Running)
// from the first poll would be returned to every subsequent poll for the
// cache TTL, locking the loop on the initial response even after the
// server-side job has completed. The fix exposes per-call GetNoCache /
// GetWithHeadersNoCache helpers on the generated client so polling code
// can bypass the cache without mutating the global c.NoCache toggle.
func TestGenerateWaitForJobBypassesResponseCache(t *testing.T) {
	t.Parallel()

	// Build a spec that triggers async-job detection (POST returning
	// {job_id, status} + sibling GET status endpoint) so jobs.go is
	// emitted; minimalSpec alone has no async surface.
	apiSpec := minimalSpec("asyncpoll")
	apiSpec.Types = map[string]spec.TypeDef{
		"RenderJob": {Fields: []spec.TypeField{
			{Name: "job_id", Type: "string"},
			{Name: "status", Type: "string"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"renders": {
			Description: "Async render jobs",
			Endpoints: map[string]spec.Endpoint{
				"submit": {Method: "POST", Path: "/renders", Description: "Submit a render", Response: spec.ResponseDef{Type: "object", Item: "RenderJob"}},
				"get":    {Method: "GET", Path: "/renders/{id}", Description: "Get a render", Response: spec.ResponseDef{Type: "object", Item: "RenderJob"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientData, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientBody := string(clientData)

	assert.Contains(t, clientBody, "func (c *Client) GetNoCache(",
		"client.go must expose GetNoCache so polling callers can bypass cache without mutating c.NoCache")
	assert.Contains(t, clientBody, "func (c *Client) GetWithHeadersNoCache(",
		"client.go must expose GetWithHeadersNoCache for header-bearing polling fetches")

	jobsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "jobs.go"))
	require.NoError(t, err)
	jobsBody := string(jobsData)

	assert.Contains(t, jobsBody, "c.GetNoCache(path, nil)",
		"WaitForJob must poll with GetNoCache to avoid locking on the cached initial response")
	assert.NotContains(t, jobsBody, "c.Get(ctx, path, nil)",
		"WaitForJob must not call c.Get(ctx, path, nil); cached non-terminal status would lock the poll")
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
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/search/movie",\s*true,\s*false,\s*nil,\s*\[]mcpParamBinding\{.*WireName: "query".*\},\s*\[]string\{[^}]*"query"`),
		tools,
		"search call site must still pass `query` in positionalParams (handler decides path vs query at runtime)")
	assert.Regexp(t,
		regexp.MustCompile(`makeAPIHandler\("GET",\s*"/movie/\{movieId\}",\s*true,\s*false,\s*nil,\s*\[]mcpParamBinding\{.*WireName: "movieId".*\},\s*\[]string\{[^}]*"movieId"`),
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

func TestGenerateBodyNameAcrossCLISurfaces(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("body-wire")
	delete(apiSpec.Resources, "items")
	apiSpec.Resources["contacts"] = spec.Resource{
		Description: "Contacts",
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method:      "POST",
				Path:        "/contacts/search",
				Description: "Search contacts",
				Body: []spec.Param{
					{Name: "startAfter", BodyName: "searchAfter", Type: "array", Description: "Pagination cursor"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	searchSource := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_contacts.go")
	assert.Contains(t, searchSource, `StringVar(&bodyStartAfter, "start-after", "", "Pagination cursor")`)
	assert.Contains(t, searchSource, `body["searchAfter"] = parsedStartAfter`)
	assert.NotContains(t, searchSource, `body["startAfter"] = parsedStartAfter`)

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSource, `mcplib.WithString("startAfter", mcplib.Description("Pagination cursor"))`)
	assert.Contains(t, mcpSource, `PublicName: "startAfter", WireName: "searchAfter", Location: "body"`)
}

// TestMCPHandlerPassesBodyArgsMap pins that POST/PUT/PATCH branches forward
// the bodyArgs map directly to the client, not via a pre-marshal. The client's
// do() json.Marshals what it receives; passing []byte causes encoding/json to
// base64-encode it into a quoted string, which strict APIs reject.
func TestMCPHandlerPassesBodyArgsMap(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-body-passthrough")
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widgets",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/widgets",
				Description: "Create a widget",
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
			"replace": {
				Method:      "PUT",
				Path:        "/widgets/{id}",
				Description: "Replace a widget",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Description: "Widget id"},
				},
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
			"update": {
				Method:      "PATCH",
				Path:        "/widgets/{id}",
				Description: "Update a widget",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Description: "Widget id"},
				},
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")

	assert.NotContains(t, mcpSource, "json.Marshal(bodyArgs)",
		"MCP handler must not pre-marshal bodyArgs: client.do() json.Marshals what it receives, "+
			"so a []byte arrives base64-encoded on the wire and strict APIs reject the payload")

	assert.Contains(t, mcpSource, "c.PostWithParams(ctx, path, params, bodyArgs)",
		"POST branch must forward bodyArgs directly to the client")
	assert.Contains(t, mcpSource, "c.PutWithParams(ctx, path, params, bodyArgs)",
		"PUT branch must forward bodyArgs directly to the client")
	assert.Contains(t, mcpSource, "c.PatchWithParams(ctx, path, params, bodyArgs)",
		"PATCH branch must forward bodyArgs directly to the client")
}

// TestMCPCodeOrchPassesParamsMap pins the same body-passthrough contract for
// the code-orchestration handler template (mcp_code_orch.go.tmpl). Sibling of
// TestMCPHandlerPassesBodyArgsMap: an earlier pre-marshal stored []byte in the
// body slot, which client.do() then json.Marshaled into a base64-encoded
// string that strict APIs reject.
func TestMCPCodeOrchPassesParamsMap(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-orch-body-passthrough")
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code", EndpointTools: "hidden"}
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widgets",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/widgets",
				Description: "Create a widget",
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
			"replace": {
				Method:      "PUT",
				Path:        "/widgets/{id}",
				Description: "Replace a widget",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Description: "Widget id"},
				},
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
			"update": {
				Method:      "PATCH",
				Path:        "/widgets/{id}",
				Description: "Update a widget",
				Params: []spec.Param{
					{Name: "id", Type: "string", Required: true, Description: "Widget id"},
				},
				Body: []spec.Param{
					{Name: "label", Type: "string", Required: true, Description: "Widget label"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")

	assert.NotContains(t, mcpSource, "json.Marshal(params)",
		"code-orch handler must not pre-marshal params: client.do() json.Marshals what it receives, "+
			"so a []byte arrives base64-encoded on the wire and strict APIs reject the payload")

	assert.Contains(t, mcpSource, "return codeOrchWriteBody(params)",
		"object write bodies must forward the structured params map to the client")
	assert.Contains(t, mcpSource, "c.Post(ctx, path, body)",
		"POST branch must forward the structured write body directly to the client")
	assert.Contains(t, mcpSource, "c.Put(ctx, path, body)",
		"PUT branch must forward the structured write body directly to the client")
	assert.Contains(t, mcpSource, "c.Patch(ctx, path, body)",
		"PATCH branch must forward the structured write body directly to the client")
}

// TestMCPBindingNumericTypesAcrossSpecShapes pins template wiring: both
// OpenAPI-parsed shapes ("int", "bool") and internal-spec literals
// ("integer", "boolean") flow through mcpBindingFunc to WithNumber /
// WithBoolean across body and query surfaces.
func TestMCPBindingNumericTypesAcrossSpecShapes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-numeric-binding")
	apiSpec.Resources["messages"] = spec.Resource{
		Description: "Messages",
		Endpoints: map[string]spec.Endpoint{
			"openapi-shape": {
				Method:      "POST",
				Path:        "/messages",
				Description: "Send a message (OpenAPI-parsed types)",
				Params: []spec.Param{
					{Name: "silent", Type: "bool", Description: "Suppress notifications"},
				},
				Body: []spec.Param{
					{Name: "priority", Type: "int", Required: true, Description: "Priority level"},
				},
			},
			"internal-shape": {
				Method:      "POST",
				Path:        "/messages/legacy",
				Description: "Send a message (internal-spec types)",
				Params: []spec.Param{
					{Name: "enabled", Type: "boolean", Description: "Enabled"},
				},
				Body: []spec.Param{
					{Name: "count", Type: "integer", Required: true, Description: "Count"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mcpSource := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")

	assert.Contains(t, mcpSource, `mcplib.WithBoolean("silent"`,
		"OpenAPI bool query param must bind WithBoolean")
	assert.Contains(t, mcpSource, `mcplib.WithNumber("priority", mcplib.Required()`,
		"OpenAPI int body field must bind WithNumber (Pushover trigger case)")
	assert.Contains(t, mcpSource, `mcplib.WithBoolean("enabled"`,
		"internal-spec boolean query param must bind WithBoolean")
	assert.Contains(t, mcpSource, `mcplib.WithNumber("count", mcplib.Required()`,
		"internal-spec integer body field must bind WithNumber")
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
	assert.Regexp(t, `Example:\s+"  promoted-public-params-pp-cli checkout --store-code example-value"`, promotedSource)
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
// export.go.tmpl (--output writes user files), import/sync/feedback/graphql_sync (writes).
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
		{"jobs.go.tmpl", 3, "parent help, list, and get; prune is omitted (mutates ledger)"},
		{"channel_workflow.go.tmpl", 3, "parent help, status, and generated payment-plan are read-only; archive omitted"},
		{"share_commands.go.tmpl", 1, "parent help only; share subcommands write files, local cache, or git state"},
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

func TestProjectManagementWorkflowsEmitSyncHints(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pmworkflows")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:     true,
		Sync:      true,
		Search:    true,
		Analytics: true,
		Workflows: []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		},
	}
	require.NoError(t, gen.Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "maxAge")
	assert.Contains(t, string(rootSrc), "time.Duration")
	assert.Contains(t, string(rootSrc), `DurationVar(&flags.maxAge, "max-age", 30*time.Minute`)

	syncHintSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync_hint.go"))
	require.NoError(t, err, "local-store-backed CLIs should emit sync_hint.go")
	for _, snippet := range []string{
		"func hintIfUnsynced(cmd *cobra.Command, db *store.Store, resourceType string) bool",
		"func hintIfStale(cmd *cobra.Command, db *store.Store, resourceType string, maxAge time.Duration) bool",
		"Run 'pmworkflows-pp-cli sync'",
	} {
		assert.Contains(t, string(syncHintSrc), snippet, "sync_hint.go missing %q", snippet)
	}

	for _, file := range []string{"pm_stale.go", "pm_orphans.go", "pm_load.go"} {
		data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", file))
		require.NoError(t, err)
		src := string(data)
		assert.Contains(t, src, `maybeEmitSyncHints(cmd, db, "", flags.maxAge)`,
			"%s should emit cold-start and stale-read hints before reading the local store", file)
	}
	searchSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "search.go"))
	require.NoError(t, err)
	assert.Contains(t, string(searchSrc), `maybeEmitSyncHints(cmd, db, resourceType, flags.maxAge)`,
		"search should emit sync hints for local FTS reads")
	analyticsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "analytics.go"))
	require.NoError(t, err)
	assert.Contains(t, string(analyticsSrc), `maybeEmitSyncHints(cmd, db, resourceType, flags.maxAge)`,
		"analytics should emit sync hints for local aggregate reads")
	dataSourceSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "data_source.go"))
	require.NoError(t, err)
	assert.Contains(t, string(dataSourceSrc), `emitSyncHints(hintWriter, db, resourceType, flags.maxAge)`,
		"data-source local fallback should emit sync hints for endpoint reads")

	testSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync_hint_test.go"))
	require.NoError(t, err, "sync_hint_test.go must compile the helper behavior in generated CLIs")
	for _, snippet := range []string{
		"func TestHintIfUnsynced_EmptySyncStateWritesHintToStderr(t *testing.T)",
		"func TestHintIfStale_BackdatedSyncStateWritesHintToStderr(t *testing.T)",
		"func TestHintIfStale_MaxAgeZeroDisablesHint(t *testing.T)",
		"func TestHintIfUnsynced_NullTimestampWritesHint(t *testing.T)",
		"func TestHintIfStale_AllResourcesIgnoresNullTimestampRows(t *testing.T)",
		"func TestHintIfStale_ResourceFilterUsesRequestedResource(t *testing.T)",
	} {
		assert.Contains(t, string(testSrc), snippet, "sync_hint_test.go missing %q", snippet)
	}

	commandTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pmworkflows-pp-cli/internal/store"
)

func runPMHintCommand(t *testing.T, dbPath string, args ...string) (string, string) {
	return runPMCommand(t, dbPath, true, args...)
}

func runPMCommand(t *testing.T, dbPath string, asJSON bool, args ...string) (string, string) {
	t.Helper()
	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	fullArgs := append(args, "--db", dbPath)
	if asJSON {
		fullArgs = append(fullArgs, "--json")
	}
	root.SetArgs(fullArgs)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v; stderr=%s", args, err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func TestPMWorkflowCommandEmitsSyncHints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	_, stderr := runPMHintCommand(t, dbPath, "load")
	if !strings.Contains(stderr, "has not been synced yet") {
		t.Fatalf("stderr = %q, want unsynced hint", stderr)
	}

	db, err = store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if _, err := db.DB().Exec(
		` + "`" + `INSERT INTO sync_state(resource_type, last_synced_at, total_count) VALUES (?, ?, ?)` + "`" + `,
		"issues", time.Now().Add(-2*time.Hour), 1,
	); err != nil {
		t.Fatalf("seed sync_state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	_, stderr = runPMHintCommand(t, dbPath, "load", "--max-age", "30m")
	if !strings.Contains(stderr, "older than --max-age=30m0s") {
		t.Fatalf("stderr = %q, want stale hint", stderr)
	}

	_, stderr = runPMHintCommand(t, dbPath, "load", "--max-age", "0")
	if strings.Contains(stderr, "older than --max-age") {
		t.Fatalf("stderr = %q, want max-age 0 to disable stale hint", stderr)
	}
}

func TestAnalyticsCommandWritesToConfiguredOutput(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, raw := range []string{
		` + "`" + `{"id":"issue-1","status":"open"}` + "`" + `,
		` + "`" + `{"id":"issue-2","status":"open"}` + "`" + `,
	} {
		var payload json.RawMessage = []byte(raw)
		var obj map[string]any
		if err := json.Unmarshal(payload, &obj); err != nil {
			t.Fatalf("unmarshal seed: %v", err)
		}
		id, _ := obj["id"].(string)
		if err := db.Upsert("issues", id, payload); err != nil {
			t.Fatalf("upsert seed: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	stdout, _ := runPMCommand(t, dbPath, false, "analytics")
	for _, want := range []string{"Resource Type\tCount", "issues\t2"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("analytics text summary stdout = %q, want %q in configured output", stdout, want)
		}
	}

	stdout, _ = runPMCommand(t, dbPath, false, "analytics", "--type", "issues")
	if !strings.Contains(stdout, "issues: 2 records") {
		t.Fatalf("analytics text count stdout = %q, want issues count in configured output", stdout)
	}

	stdout, _ = runPMCommand(t, dbPath, false, "analytics", "--type", "issues", "--group-by", "status")
	for _, want := range []string{"status\tCount", "open\t2"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("analytics text group-by stdout = %q, want %q in configured output", stdout, want)
		}
	}

	stdout, _ = runPMHintCommand(t, dbPath, "analytics")
	if !strings.Contains(stdout, ` + "`" + `"issues": 2` + "`" + `) {
		t.Fatalf("analytics summary stdout = %q, want issues count in configured output", stdout)
	}

	stdout, _ = runPMHintCommand(t, dbPath, "analytics", "--type", "issues", "--group-by", "status")
	for _, want := range []string{` + "`" + `"value": "open"` + "`" + `, ` + "`" + `"count": 2` + "`" + `} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("analytics group-by stdout = %q, want %q in configured output", stdout, want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "pm_sync_hint_command_test.go"), []byte(commandTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli")
}

func TestNetworkFallbackReason(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "api_unreachable", networkFallbackReason(nil))

	cases := []struct {
		name    string
		kind    string
		baseURL string
		want    string
	}{
		{
			name:    "synthetic kind",
			kind:    spec.KindSynthetic,
			baseURL: "https://example.com",
			want:    "synthetic_anchor_fallback",
		},
		{
			name:    "local placeholder host",
			baseURL: "https://example.local",
			want:    "synthetic_anchor_fallback",
		},
		{
			name:    "live api",
			baseURL: "https://api.example.com",
			want:    "api_unreachable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := adsCampaignSpec()
			apiSpec.Name = "fallback-" + strings.ReplaceAll(tc.name, " ", "-")
			apiSpec.Kind = tc.kind
			apiSpec.BaseURL = tc.baseURL
			assert.Equal(t, tc.want, networkFallbackReason(apiSpec))
		})
	}
}

func TestLocalReadIsList(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		supportsAllPagination bool
		apiSpec               *spec.APISpec
		endpointName          string
		endpoint              spec.Endpoint
		want                  bool
	}{
		{
			name:         "top-level list endpoint",
			endpointName: "list",
			endpoint:     spec.Endpoint{Method: "GET", Path: "/campaigns", Response: spec.ResponseDef{Type: "array"}},
			want:         true,
		},
		{
			name:         "non-synthetic array response without list name",
			endpointName: "search",
			endpoint:     spec.Endpoint{Method: "GET", Path: "/campaigns/search", Response: spec.ResponseDef{Type: "array"}},
			want:         false,
		},
		{
			name:         "synthetic array response without list name",
			apiSpec:      &spec.APISpec{Kind: spec.KindSynthetic},
			endpointName: "search",
			endpoint:     spec.Endpoint{Method: "GET", Path: "/campaigns/search", Response: spec.ResponseDef{Type: "array"}},
			want:         true,
		},
		{
			name:         "path-scoped array response stays out of unscoped fallback",
			endpointName: "list",
			endpoint: spec.Endpoint{
				Method:   "GET",
				Path:     "/teams/{team_id}/users",
				Params:   []spec.Param{{Name: "team_id", Type: "string", Positional: true, PathParam: true}},
				Response: spec.ResponseDef{Type: "array"},
			},
			want: false,
		},
		{
			name:                  "existing paginated local fallback behavior is preserved",
			supportsAllPagination: true,
			endpointName:          "list",
			endpoint: spec.Endpoint{
				Method:   "GET",
				Path:     "/teams/{team_id}/users",
				Params:   []spec.Param{{Name: "team_id", Type: "string", Positional: true, PathParam: true}},
				Response: spec.ResponseDef{Type: "array"},
			},
			want: true,
		},
		{
			name:         "single-object endpoint",
			endpointName: "get",
			endpoint:     spec.Endpoint{Method: "GET", Path: "/campaigns/{id}", Response: spec.ResponseDef{Type: "object"}},
			want:         false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, localReadIsList(tc.supportsAllPagination, tc.apiSpec, tc.endpointName, tc.endpoint))
		})
	}
}

func TestGeneratedSyntheticAnchorCommandFallsBackToLocalStore(t *testing.T) {
	t.Parallel()

	apiSpec := adsCampaignSpec()
	apiSpec.Name = "syntheticanchors"
	apiSpec.Kind = spec.KindSynthetic
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	unreachableAddr := ln.Addr().String()
	require.NoError(t, ln.Close())
	apiSpec.BaseURL = "http://" + unreachableAddr
	campaigns := apiSpec.Resources["campaigns"]
	listEndpoint := campaigns.Endpoints["list"]
	// This exercises endpointNeedsClientLimit and truncateJSONArray after
	// resolveRead falls back to the local store.
	listEndpoint.Params = append(listEndpoint.Params, spec.Param{Name: "limit", Type: "int"})
	campaigns.Endpoints["list"] = listEndpoint
	apiSpec.Resources["campaigns"] = campaigns
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"syntheticanchors-pp-cli/internal/store"
)

func TestSyntheticAnchorCommandFallsBackToLocalStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	db, err := store.OpenWithContext(context.Background(), defaultDBPath("syntheticanchors-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.DB().Exec(` + "`" + `INSERT INTO resources(resource_type, id, data) VALUES
		(?, ?, ?),
		(?, ?, ?),
		(?, ?, ?),
		(?, ?, ?)` + "`" + `,
		"campaigns", "camp_1", ` + "`" + `{"id":"camp_1","name":"Launch","status":"active","account_id":"acct_1"}` + "`" + `,
		"campaigns", "camp_2", ` + "`" + `{"id":"camp_2","name":"Sustain","status":"paused","account_id":"acct_1"}` + "`" + `,
		"campaigns", "camp_3", ` + "`" + `{"id":"camp_3","name":"Winback","status":"active","account_id":"acct_2"}` + "`" + `,
		"campaigns", "camp_4", ` + "`" + `{"id":"camp_4","name":"Nurture","status":"active","account_id":"acct_2"}` + "`" + `,
	); err != nil {
		t.Fatalf("seed campaigns: %v", err)
	}
	if err := db.SaveSyncState("campaigns", "", 1); err != nil {
		t.Fatalf("save sync state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"campaigns", "--limit", "3", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute campaigns: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Meta struct {
			Source       string ` + "`" + `json:"source"` + "`" + `
			Reason       string ` + "`" + `json:"reason"` + "`" + `
			ResourceType string ` + "`" + `json:"resource_type"` + "`" + `
		} ` + "`" + `json:"meta"` + "`" + `
		Results []json.RawMessage ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if payload.Meta.Source != "local" || payload.Meta.Reason != "synthetic_anchor_fallback" || payload.Meta.ResourceType != "campaigns" {
		t.Fatalf("meta = %+v, want local synthetic_anchor_fallback campaigns", payload.Meta)
	}
	if len(payload.Results) != 3 {
		t.Fatalf("results = %d, want 3; stdout=%s", len(payload.Results), stdout.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "synthetic_anchor_fallback_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyntheticAnchorCommandFallsBackToLocalStore", "./internal/cli")
}

func TestGeneratedLiveCommandPrefersAPIOverLocalStore(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		assert.Equal(t, "/campaigns", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"live_1","name":"Live","status":"active","account_id":"acct_live"}]`))
	}))
	defer server.Close()

	apiSpec := adsCampaignSpec()
	apiSpec.Name = "livefallback"
	apiSpec.BaseURL = server.URL
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"livefallback-pp-cli/internal/store"
)

func TestLiveCommandPrefersAPIOverLocalStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	db, err := store.OpenWithContext(context.Background(), defaultDBPath("livefallback-pp-cli"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.DB().Exec(` + "`" + `INSERT INTO resources(resource_type, id, data) VALUES (?, ?, ?)` + "`" + `,
		"campaigns", "local_1", ` + "`" + `{"id":"local_1","name":"Local","status":"paused","account_id":"acct_local"}` + "`" + `,
	); err != nil {
		t.Fatalf("seed campaigns: %v", err)
	}
	if err := db.SaveSyncState("campaigns", "", 1); err != nil {
		t.Fatalf("save sync state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"campaigns", "--agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute campaigns: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Meta struct {
			Source string ` + "`" + `json:"source"` + "`" + `
			Reason string ` + "`" + `json:"reason"` + "`" + `
		} ` + "`" + `json:"meta"` + "`" + `
		Results []map[string]any ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if payload.Meta.Source != "live" || payload.Meta.Reason != "" {
		t.Fatalf("meta = %+v, want live with no fallback reason", payload.Meta)
	}
	if len(payload.Results) != 1 || payload.Results[0]["id"] != "live_1" {
		t.Fatalf("results = %#v, want live payload only", payload.Results)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "live_preferred_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestLiveCommandPrefersAPIOverLocalStore", "./internal/cli")
	assert.Greater(t, requestCount.Load(), int32(0), "generated command should call the live API in auto mode")
}

// TestSearchTemplateEmptyTypeQueriesGenericFTS pins #1390 — the
// no-type branch must include a generic db.Search(query, limit) call
// alongside the per-table typed Search<Resource> loop. Rows indexed
// only in resources_fts (not in any typed FTS table) otherwise return
// zero on the default search path.
func TestSearchTemplateEmptyTypeQueriesGenericFTS(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("templates", "search.go.tmpl"))
	require.NoError(t, err)
	body := string(data)

	caseEmptyIdx := strings.Index(body, `case "":`)
	require.GreaterOrEqual(t, caseEmptyIdx, 0, "search.go.tmpl must have a case \"\": branch")

	// Anchor on the column-zero `default:` that closes the empty-type
	// branch, so a stray "default:" inside a comment can't shift the
	// slice boundary.
	defaultIdx := strings.Index(body[caseEmptyIdx:], "\n\t\t\tdefault:")
	require.GreaterOrEqual(t, defaultIdx, 0, "search.go.tmpl must have a tab-indented default: after case \"\":")
	emptyBranch := body[caseEmptyIdx : caseEmptyIdx+defaultIdx]

	assert.Contains(t, emptyBranch, "db.Search(query, limit)",
		"case \"\": must call db.Search(query, limit) so rows indexed only in resources_fts (not in a typed FTS table) are returned")
	assert.Contains(t, emptyBranch, `"search resources_fts failed:`,
		"the generic-search error path must mention resources_fts so the failure is debuggable")
}

// TestBrowserLikeUserAgentTemplates pins that BOTH the client.go.tmpl
// and auth_browser.go.tmpl branch User-Agent emission on
// UsesBrowserLikeUserAgent so that cookie/composed/session_handshake
// auth and Kind: synthetic ship a Chrome-shaped UA instead of the
// script-flavored `<cli>-pp-cli/<ver>`. Otherwise WAFs (Wordfence,
// Imperva, Akamai bot-mode, DataDome, Cloudflare bot-fight) bucket
// every CLI request as bot traffic — including the auth-verify call,
// which must clear the same bar or the user can't bootstrap session
// state.
func TestBrowserLikeUserAgentTemplates(t *testing.T) {
	t.Parallel()
	for _, tmpl := range []string{"client.go.tmpl", "auth_browser.go.tmpl"} {
		t.Run(tmpl, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("templates", tmpl))
			require.NoError(t, err)
			body := string(data)

			assert.Contains(t, body, "{{- if .UsesBrowserLikeUserAgent}}",
				"%s must branch UA emission on UsesBrowserLikeUserAgent", tmpl)
			assert.Contains(t, body, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
				"%s must emit a Chrome UA string on the browser-like branch", tmpl)
			assert.Contains(t, body, `"{{.Name}}-pp-cli/{{.Version}}"`,
				"%s must keep the script-flavored UA on the default branch so documented APIs stay identifiable", tmpl)
		})
	}
}

// TestStaleTemplateCoversCommonTimestampFields pins the timestamp-key
// list in pm_stale.go.tmpl so APIs that use modified_at (Asana),
// last_edited_time (Notion), updated (Stripe), and similar variants
// don't silently return zero stale items. The WHERE/ORDER BY/scan
// loop all read from the same constant, so testing the constant is
// the cheap-and-correct way to guard the bug.
func TestStaleTemplateCoversCommonTimestampFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("templates", "workflows", "pm_stale.go.tmpl"))
	require.NoError(t, err)
	body := string(data)

	for _, field := range []string{
		"updatedAt",
		"updated_at",
		"updatedDate",
		"modifiedAt",
		"modified_at",
		"lastModified",
		"last_modified",
		"lastEditedTime",
		"last_edited_time",
		"updated",
		"last_updated",
	} {
		assert.Contains(t, body, `"`+field+`"`,
			"pm_stale.go.tmpl must list %q as a stale-timestamp field; %s users return zero results otherwise",
			field, field)
	}

	// Pin the dynamic-build approach so the WHERE/ORDER BY/scan stay in
	// lockstep with the field list — a future maintainer adding a field
	// must update one place, not three.
	assert.Contains(t, body, "buildStaleTimestampPredicate()",
		"pm_stale.go.tmpl should build SQL from staleTimestampFields instead of hardcoding columns")

	// Pin the COALESCE-based ORDER BY so a future refactor doesn't
	// regress to a multi-column sort, which SQLite's NULL-first ASC
	// ordering would corrupt for rows from APIs that don't populate the
	// leading field.
	assert.Contains(t, body, `"COALESCE(" + strings.Join(coalesceArgs, ", ") + ") ASC"`,
		"pm_stale.go.tmpl ORDER BY must COALESCE across staleTimestampFields, not sort multi-column")

	// Pin the typeof-gated WHERE so integer-typed timestamps (Stripe's
	// `updated`) compare against an epoch cutoff rather than the
	// RFC 3339 cutoff. Without the gate SQLite's type-class ordering
	// makes every numeric row unconditionally match the predicate.
	assert.Contains(t, body, "typeof(",
		"pm_stale.go.tmpl WHERE must gate string/numeric comparisons with typeof()")
	assert.Contains(t, body, "cutoffEpoch",
		"pm_stale.go.tmpl must bind a numeric cutoff alongside the RFC 3339 cutoff for integer-typed timestamps")
}

// TestSyncTemplateShortCircuitsOnDryRunSentinel: client.dryRun returns
// `{"dry_run": true}` instead of a real response, and the sync loop
// must detect this with isDryRunResponse and emit a synthetic
// sync_dryrun event before falling through to upsertSingleObject —
// otherwise validate-narrative --full-examples (which auto-appends
// --dry-run) blocks shipcheck on a spurious "missing id for <resource>"
// error.
func TestSyncTemplateShortCircuitsOnDryRunSentinel(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("templates", "sync.go.tmpl"))
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, "func isDryRunResponse(data json.RawMessage) bool",
		"sync.go.tmpl must define isDryRunResponse helper that detects the client.dryRun sentinel")
	assert.Contains(t, body, `"sync_dryrun"`,
		"sync.go.tmpl must emit a sync_dryrun event on the short-circuit path so validate-narrative sees a structured success")

	// Ordering pin: the dry-run check must run BEFORE upsertSingleObject,
	// otherwise the sentinel reaches the upsert path and triggers a spurious
	// "missing id for <resource>" error.
	dryRunCheckIdx := strings.Index(body, "if isDryRunResponse(data)")
	upsertSingleIdx := strings.Index(body, "if err := upsertSingleObject(db, resource, data)")
	require.GreaterOrEqual(t, dryRunCheckIdx, 0, "sync.go.tmpl must call isDryRunResponse on the response")
	require.GreaterOrEqual(t, upsertSingleIdx, 0, "sync.go.tmpl must still call upsertSingleObject for live single-object responses")
	assert.Less(t, dryRunCheckIdx, upsertSingleIdx,
		"isDryRunResponse check must run before upsertSingleObject so the dry-run sentinel doesn't fall through to the missing-id error")
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
	// renamed-with-suffix dead table, nothing else snuck in. The framework
	// tables (resources, sync_state) are hand-written in the template
	// without safeSQLName, so the regex accepts both quoted and unquoted
	// forms.
	createRe := regexp.MustCompile("`CREATE TABLE IF NOT EXISTS \"?(\\w+)\"? \\(")
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

func TestGenerateStoreWideResourceFallsBackToJSONOnlyTable(t *testing.T) {
	t.Parallel()

	fields := []spec.TypeField{{Name: "id", Type: "string"}}
	for i := range maxStoreDomainTableColumns {
		fields = append(fields, spec.TypeField{Name: fmt.Sprintf("setting_%d", i), Type: "string"})
	}

	apiSpec := &spec.APISpec{
		Name:    "widecontrol",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/widecontrol-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"control": {
				Description: "Tenant-wide control settings",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/Control",
						Description: "Get control settings",
						Response:    spec.ResponseDef{Type: "object", Item: "Control"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Control": {Fields: fields},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	store := string(storeSrc)

	controlCreate := regexp.MustCompile("(?s)`CREATE TABLE IF NOT EXISTS \"control\" \\((.*?)\\)`").FindStringSubmatch(store)
	require.Len(t, controlCreate, 2, "control table should still be emitted as JSON-only")
	assert.Contains(t, controlCreate[1], `"id" TEXT PRIMARY KEY`)
	assert.Contains(t, controlCreate[1], `"data" JSON NOT NULL`)
	assert.Contains(t, controlCreate[1], `"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP`)
	assert.NotContains(t, controlCreate[1], "setting_1499", "wide typed columns must not be emitted")
	assert.Contains(t, store, "func (s *Store) UpsertControl(", "JSON-only fallback table still needs a typed upsert")

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store")
}

func TestSchemaWithDependentParentsFallsBackWhenParentColumnExceedsWideCap(t *testing.T) {
	t.Parallel()

	scalarFieldCount := maxStoreDomainTableColumns - len(baseTableColumns)
	fields := []spec.TypeField{}
	for i := range scalarFieldCount {
		fields = append(fields, spec.TypeField{Name: fmt.Sprintf("setting_%d", i), Type: "string"})
	}

	apiSpec := &spec.APISpec{
		Name:    "widechild",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/widechild-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"control": {
				Description: "Tenant-wide child settings",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/parents/{parent_id}/control",
						Description: "List child control settings",
						Response:    spec.ResponseDef{Type: "array", Item: "Control"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Control": {Fields: fields},
		},
	}

	gen := New(apiSpec, t.TempDir())
	gen.profile = &profiler.APIProfile{
		DependentSyncResources: []profiler.DependentResource{{Name: "control"}},
	}

	control := findTable(gen.schemaWithDependentParents(), "control")
	require.NotNil(t, control)
	assert.True(t, control.JSONOnlyFallback, "parent_id must not push dependent tables over the wide-table cap")
	assert.Equal(t, maxStoreDomainTableColumns+1, control.OriginalColumnCount)

	names := make([]string, 0, len(control.Columns))
	for _, c := range control.Columns {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"id", "data", "synced_at"}, names)
	assert.Empty(t, control.Indexes)
	assert.False(t, control.FTS5)
}

// TestGenerateEndpointTemplateEnvOverridesWireThrough: when the spec
// declares an explicit env-var name for a template placeholder (e.g.
// ST_TENANT_ID for {tenant}), every emitted artifact that touches env-var
// resolution must use the override instead of the default
// <APINAME>_<PLACEHOLDER> convention. Sync also needs to surface the
// template-resolvable path without skipping it as "requires parent context".
func TestGenerateEndpointTemplateEnvOverridesWireThrough(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:                 "servicetitan-crm",
		Description:          "ServiceTitan CRM (test fixture)",
		Version:              "1.0",
		BaseURL:              "https://api.servicetitan.io",
		EndpointTemplateVars: []string{"tenant"},
		EndpointTemplateEnvOverrides: map[string]string{
			"tenant": "ST_TENANT_ID",
		},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			EnvVars: []string{"ST_API_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/servicetitan-crm-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"customers": {
				Description: "Customers",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/tenant/{tenant}/customers",
						Pagination: &spec.Pagination{CursorParam: "pageToken", LimitParam: "pageSize"},
						Response:   spec.ResponseDef{Type: "array", Item: "Customer"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Customer": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	urlGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "url.go"))
	require.NoError(t, err)
	assert.Regexp(t, `"tenant":\s+"ST_TENANT_ID"`, string(urlGo),
		"templateVarEnvNames must use the spec-declared override, not the SERVICETITAN_CRM_TENANT default")
	assert.NotContains(t, string(urlGo), "SERVICETITAN_CRM_TENANT",
		"the default <APINAME>_<PLACEHOLDER> name must not leak when an override is declared")

	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	assert.Contains(t, string(configGo), `os.Getenv("ST_TENANT_ID")`,
		"config Load() must read the override env var name")
	assert.NotContains(t, string(configGo), `os.Getenv("SERVICETITAN_CRM_TENANT")`,
		"the default env var name must not appear when an override exists")

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)
	assert.Contains(t, syncSrc, `"customers": "/tenant/{tenant}/customers"`,
		"defaultSyncResources/syncResourcePath must include the tenant-scoped path with the placeholder preserved")
	assert.Contains(t, syncSrc, `endpointTemplateVarSet = map[string]bool`,
		"sync must declare the template-var set so the unresolved-key check ignores {tenant}")
	assert.Contains(t, syncSrc, `slices.DeleteFunc`,
		"sync must filter template-var placeholders out of the missing-keys list before warning")
	// --path-context is the runtime override hatch for spec-declared
	// EndpointTemplateVars (#1332). It must appear on the sync command when
	// the spec has template-var placeholders so users can fill any {key} at
	// the call site instead of re-exporting an env var.
	assert.Contains(t, syncSrc, `&pathContextFlags, "path-context"`,
		"sync must register the --path-context flag when EndpointTemplateVars is non-empty")
	assert.Contains(t, syncSrc, `parseSyncKVFlags(pathContextFlags, "--path-context")`,
		"sync RunE must parse --path-context entries via the shared KV parser")
	assert.Contains(t, syncSrc, "c.Config.TemplateVars[k] = v",
		"sync must merge --path-context values into Config.TemplateVars so buildURL substitution picks them up")

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "ST_TENANT_ID",
		"README must surface the override env var name in the runtime endpoint instructions")

	// Compile the generated tree so the --path-context emission and the
	// Config.TemplateVars merge land as valid Go; a template-level bracket
	// mistake would otherwise pass the string-Contains checks above but
	// blow up at install time for every printed CLI.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateParentNoSubcommandRunE_WiredOnResourceParents: every generated
// resource parent command must wire the parentNoSubcommandRunE helper so
// invocations without a subcommand in --agent / --json mode get a structured
// JSON error instead of human-readable cobra help on stdout. Before the fix,
// agents driving the CLI silently received help text and exit 0. The helper
// also has to be defined exactly once in helpers.go so the generated package
// compiles. Multi-endpoint resources are exercised so a parent is actually
// emitted (single-endpoint resources get promoted to a direct subcommand).
func TestGenerateParentNoSubcommandRunE_WiredOnResourceParents(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parent-runE-wired")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"get":  {Method: "GET", Path: "/items/{id}", Description: "Get one item"},
			},
		},
	}
	apiSpec.Share = spec.ShareConfig{Enabled: true, SnapshotTables: []string{"items"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	assert.Contains(t, string(helpersSrc), "func parentNoSubcommandRunE(flags *rootFlags)",
		"helpers.go must define parentNoSubcommandRunE so generated parents can wire it")
	assert.Contains(t, string(helpersSrc), `"subcommand required"`,
		"the helper must emit a structured JSON error envelope")
	assert.Contains(t, string(helpersSrc), `"valid_subcommands"`,
		"the JSON envelope must list valid subcommands so agents can self-correct")

	parentSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items.go"))
	require.NoError(t, err)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(parentSrc),
		"the resource parent must call the shared helper instead of falling through to cobra's default help")

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(authSrc),
		"the auth parent shares the same bug class and must wire the helper too")

	profileSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "profile.go"))
	require.NoError(t, err)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(profileSrc),
		"the profile parent shares the same bug class and must wire the helper too")

	workflowSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "channel_workflow.go"))
	require.NoError(t, err)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(workflowSrc),
		"the workflow parent shares the same bug class and must wire the helper too")

	shareSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "share_commands.go"))
	require.NoError(t, err)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(shareSrc),
		"the share parent shares the same bug class and must wire the helper too")
}

func TestGenerateParentGroupersEmitReadOnlyAnnotation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parent-grouper-readonly")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"get":  {Method: "GET", Path: "/items/{id}", Description: "Get one item"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	parentSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Annotations:\s+map\[string\]string\{"mcp:read-only":\s*"true"\}`, string(parentSrc),
		"parent groupers must always emit mcp:read-only=true so cobratree/tools-audit treat them as read-only")
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(parentSrc),
		"test fixture must exercise a parent grouper command, not a promoted single-endpoint command")
}

func TestGenerateParentCommandShorts_AreAgentGradeForGroupers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parent-short-agent-grade")
	apiSpec.Resources = map[string]spec.Resource{
		"accounts": {
			Description: "Manage accounts",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/accounts", Description: "List accounts"},
				"get":  {Method: "GET", Path: "/accounts/{id}", Description: "Get one account"},
			},
			SubResources: map[string]spec.Resource{
				"attachments": {
					Description: "Manage attachments",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/accounts/{account_id}/attachments", Description: "List account attachments"},
						"get":  {Method: "GET", Path: "/accounts/{account_id}/attachments/{id}", Description: "Get an account attachment"},
					},
				},
				"cancel": {
					Description: "Manage cancel",
					Endpoints: map[string]spec.Endpoint{
						"create": {Method: "POST", Path: "/accounts/{account_id}/cancel", Description: "Cancel an account"},
						"status": {Method: "GET", Path: "/accounts/{account_id}/cancel", Description: "Get cancellation status"},
					},
				},
				"reports": {
					Description: "Manage reports",
					Endpoints: map[string]spec.Endpoint{
						"query": {Method: "POST", Path: "/accounts/{account_id}/reports/query", Description: "Query account reports"},
					},
				},
				"jobs": {
					Description: "Manage jobs",
					Endpoints: map[string]spec.Endpoint{
						"run": {Method: "POST", Path: "/accounts/{account_id}/jobs/run", Description: "Run account jobs"},
					},
				},
			},
		},
		"teams": {
			Description: "Manage teams",
			SubResources: map[string]spec.Resource{
				"members": {
					Description: "Manage members",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/teams/{team_id}/members", Description: "List team members"},
						"get":  {Method: "GET", Path: "/teams/{team_id}/members/{id}", Description: "Get a team member"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	accountSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "accounts.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"List and get accounts"`, string(accountSrc),
		"top-level generated parent groupers need agent-grade descriptions, not thin Manage stubs")

	attachmentsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "accounts_attachments.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"List and get attachments for accounts"`, string(attachmentsSrc),
		"nested noun groupers should carry parent context in their Short")

	cancelSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "accounts_cancel.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"Run cancel operations for accounts"`, string(cancelSrc),
		"verb-shaped groupers should not emit nonsense like Manage cancel")

	reportsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "accounts_reports.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"Query reports for accounts"`, string(reportsSrc),
		"POST query-style groupers should describe read-shaped actions, not create operations")
	assert.NotRegexp(t, `Short:\s+"Create reports for accounts"`, string(reportsSrc),
		"read-shaped POST endpoints must not be mislabeled as creates")

	jobsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "accounts_jobs.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"Run jobs for accounts"`, string(jobsSrc),
		"common action-style POST endpoints should use their endpoint verb, not the generic create fallback")
	assert.NotRegexp(t, `Short:\s+"Create jobs for accounts"`, string(jobsSrc),
		"action-style POST endpoints must not be mislabeled as creates")

	teamsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "teams.go"))
	require.NoError(t, err)
	assert.Regexp(t, `Short:\s+"Manage teams command groups"`, string(teamsSrc),
		"endpoint-less parent groupers should still avoid thin Manage stubs")
	assert.False(t, naming.IsThinCommandShort("Manage teams command groups"),
		"endpoint-less parent fallback must stay above the tools-audit thin-short floor")

	topLevelReportsShort := parentCommandShort("reports", "", spec.Resource{
		Description: "Manage reports",
		Endpoints: map[string]spec.Endpoint{
			"query": {Method: "POST", Path: "/reports/query", Description: "Query reports"},
		},
	}, "")
	assert.Equal(t, "Manage reports command groups", topLevelReportsShort,
		"single-action top-level groupers should fall back when the action-derived Short is still thin")
	assert.NotEqual(t, "Query reports", topLevelReportsShort,
		"top-level parent groupers must not emit action-derived Shorts that tools-audit still flags")
}

func TestParentCommandInfoDescriptionShort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "first sentence",
			description: "Coordinate video publishing workflows across channels. This second sentence belongs in longer docs.",
			want:        "Coordinate video publishing workflows across channels.",
		},
		{
			name:        "thin first sentence does not fall through",
			description: "API. Coordinate video publishing workflows across channels.",
			want:        "",
		},
		{
			name:        "abbreviation period stays in first sentence",
			description: "The U.S. Census Bureau API provides access to datasets. This second sentence belongs in longer docs.",
			want:        "The U.S. Census Bureau API provides access to datasets.",
		},
		{
			name:        "decimal period stays in first sentence",
			description: "Use the v2.0 reporting API for exports. This second sentence belongs in longer docs.",
			want:        "Use the v2.0 reporting API for exports.",
		},
		{
			name:        "version placeholder period stays in first sentence",
			description: "Use the v1.x reporting API for exports. This second sentence belongs in longer docs.",
			want:        "Use the v1.x reporting API for exports.",
		},
		{
			name:        "long version placeholder sentence stays useful",
			description: "Use the v1.x reporting API for exporting analytics dashboards billing records team usage metrics attribution windows cohort slices retention curves and billing forecasts. This second sentence belongs in longer docs.",
			want:        "Use the v1.x reporting API for exporting analytics dashboards billing records team usage metrics attribution windows",
		},
		{
			name:        "long first sentence stays compact",
			description: "Coordinate video publishing workflows across channels, regions, partners, approval queues, localization states, analytics exports, campaign plans, compliance reviews, and publishing calendars. This second sentence belongs in longer docs.",
			want:        "Coordinate video publishing workflows across channels, regions, partners, approval queues, localization states",
		},
		{
			name:        "no punctuation uses full description",
			description: "Coordinate video publishing workflows across channels",
			want:        "Coordinate video publishing workflows across channels",
		},
		{
			name:        "long unpunctuated description stays compact",
			description: "Coordinate video publishing workflows across channels regions partners approval queues localization states analytics exports campaign plans compliance reviews and publishing calendars",
			want:        "Coordinate video publishing workflows across channels regions partners approval queues localization states analytics",
		},
		{
			name:        "thin unpunctuated description ignored",
			description: "Video API",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, parentCommandInfoDescriptionShort(tt.description))
		})
	}
}

func TestGenerateParentCommandShorts_UseOpenAPIProseFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("tag description", func(t *testing.T) {
		t.Parallel()

		data := []byte(`
openapi: 3.0.3
info:
  title: Billing API
  version: 1.0.0
  description: Generic billing API fallback.
servers:
  - url: https://api.example.com
tags:
  - name: Customers
    description: Manage customer records, subscriptions, and billing profiles.
paths:
  /customers:
    get:
      operationId: listCustomers
      tags: [Customers]
      summary: List customers
      responses:
        '200':
          description: OK
  /customers/{customer_id}:
    get:
      operationId: getCustomer
      tags: [Customers]
      summary: Get customer
      parameters:
        - name: customer_id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: OK
`)

		apiSpec, err := openapi.Parse(data)
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "customers.go"))
		require.NoError(t, err)
		assert.Regexp(t, `Short:\s+"Manage customer records, subscriptions, and billing profiles\."`, string(src))
		assert.NotRegexp(t, `Short:\s+"Generic billing API fallback\."`, string(src))
	})

	t.Run("info description first sentence", func(t *testing.T) {
		t.Parallel()

		data := []byte(`
openapi: 3.0.3
info:
  title: Video API
  version: 1.0.0
  description: Coordinate video publishing workflows across channels. This second sentence belongs in longer docs.
servers:
  - url: https://api.example.com
paths:
  /videos:
    get:
      operationId: listVideos
      summary: List videos
      responses:
        '200':
          description: OK
  /videos/{video_id}:
    get:
      operationId: getVideo
      summary: Get video
      parameters:
        - name: video_id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: OK
`)

		apiSpec, err := openapi.Parse(data)
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "videos.go"))
		require.NoError(t, err)
		assert.Regexp(t, `Short:\s+"Coordinate video publishing workflows across channels\."`, string(src))
		assert.NotRegexp(t, `This second sentence`, string(src))
	})

	t.Run("multi resource spec keeps resource-specific shorts", func(t *testing.T) {
		t.Parallel()

		data := []byte(`
openapi: 3.0.3
info:
  title: Commerce API
  version: 1.0.0
  description: Coordinate commerce workflows across customer and invoice records.
servers:
  - url: https://api.example.com
paths:
  /customers:
    get:
      operationId: listCustomers
      summary: List customers
      responses:
        '200':
          description: OK
  /customers/{customer_id}:
    get:
      operationId: getCustomer
      summary: Get customer
      parameters:
        - name: customer_id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: OK
  /invoices:
    get:
      operationId: listInvoices
      summary: List invoices
      responses:
        '200':
          description: OK
  /invoices/{invoice_id}:
    get:
      operationId: getInvoice
      summary: Get invoice
      parameters:
        - name: invoice_id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: OK
`)

		apiSpec, err := openapi.Parse(data)
		require.NoError(t, err)

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		require.NoError(t, New(apiSpec, outputDir).Generate())

		customersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "customers.go"))
		require.NoError(t, err)
		assert.Regexp(t, `Short:\s+"List and get customers"`, string(customersSrc))
		assert.NotRegexp(t, `Coordinate commerce workflows`, string(customersSrc))

		invoicesSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "invoices.go"))
		require.NoError(t, err)
		assert.Regexp(t, `Short:\s+"List and get invoices"`, string(invoicesSrc))
		assert.NotRegexp(t, `Coordinate commerce workflows`, string(invoicesSrc))
	})
}
