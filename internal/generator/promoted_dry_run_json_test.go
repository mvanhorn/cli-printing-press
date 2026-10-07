package generator

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestPromotedCommandsIncludeMutationMatchesOutputGate(t *testing.T) {
	t.Parallel()

	withMutation := promotedDryRunSpec("promoted-dry-run-gate")
	require.True(t, promotedCommandsIncludeMutation(withMutation, buildPromotedCommands(withMutation)))

	readOnly := minimalSpec("promoted-dry-run-reads")
	readOnly.Learn.Disabled = true
	readOnly.Auth = spec.AuthConfig{Type: "none"}
	readOnly.Resources = map[string]spec.Resource{
		"pings": {
			Description: "Pings",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/pings", Description: "Get pings"},
			},
		},
		"searches": {
			Description: "Searches",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "POST",
					Path:        "/searches",
					Description: "Search",
					Body:        []spec.Param{{Name: "query", Type: "string"}},
				},
			},
		},
	}
	require.False(t, promotedCommandsIncludeMutation(readOnly, buildPromotedCommands(readOnly)))
	require.False(t, promotedCommandsIncludeMutation(nil, nil))

	outputDir := filepath.Join(t.TempDir(), naming.CLI(readOnly.Name))
	gen := New(readOnly, outputDir)
	// MCP emits internal/mcp, which the always-generated server imports.
	// Export-only leaves that package empty, so ./... cannot compile.
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())
	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.NotContains(t, helpers, "func stampDryRunEnvelope(")
	require.NotContains(t, helpers, "func printStampedDryRunOutput(")
	require.Contains(t, helpers, "func printNoStoreReadDryRun(")
	requireGeneratedCompiles(t, outputDir)
}

func TestPromotedMutationDryRunJSONEnvelope(t *testing.T) {
	t.Parallel()

	store := buildPromotedDryRunCLI(t, true)
	assertPromotedDryRunSources(t, store.dir, true)
	assertPromotedDryRunRuntime(t, store, true)

	storeless := buildPromotedDryRunCLI(t, false)
	assertPromotedDryRunSources(t, storeless.dir, false)
	assertPromotedDryRunRuntime(t, storeless, false)
}

func promotedDryRunSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Disabled = true
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"create": {
					Method:      "POST",
					Path:        "/items",
					Description: "Create an item",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/widgets",
					Description: "Create a widget",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
		"sessions": {
			Description: "Sessions",
			Endpoints: map[string]spec.Endpoint{
				"revoke": {
					Method:      "DELETE",
					Path:        "/sessions/{id}",
					Description: "Revoke a session",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
		"searches": {
			Description: "Searches",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "POST",
					Path:        "/searches",
					Description: "Search",
					Body:        []spec.Param{{Name: "query", Type: "string"}},
				},
			},
		},
		"pings": {
			Description: "Pings",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/pings", Description: "Get pings"},
			},
		},
	}
	return apiSpec
}

type promotedDryRunCLI struct {
	dir    string
	binary string
	hits   *atomic.Int64
	env    []string
}

func buildPromotedDryRunCLI(t *testing.T, withStore bool) promotedDryRunCLI {
	t.Helper()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"made-1","name":"Made"}`))
	}))
	t.Cleanup(server.Close)

	name := "promoted-dry-run-store"
	if !withStore {
		name = "promoted-dry-run-nostore"
	}
	apiSpec := promotedDryRunSpec(name)
	apiSpec.BaseURL = server.URL

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	if withStore {
		gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	} else {
		gen.VisionSet = VisionTemplateSet{Export: true}
	}
	require.NoError(t, gen.Generate())

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	env := frameworkDryRunEnv(t, t.TempDir(), naming.EnvPrefix(apiSpec.Name))
	env = append(env, naming.EnvPrefix(apiSpec.Name)+"_BASE_URL="+server.URL)
	return promotedDryRunCLI{dir: outputDir, binary: binaryPath, hits: &hits, env: env}
}

func assertPromotedDryRunSources(t *testing.T, outputDir string, withStore bool) {
	t.Helper()

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "func stampDryRunEnvelope(")
	require.Contains(t, helpers, "func printStampedDryRunOutput(")
	if withStore {
		require.NotContains(t, helpers, "func printNoStoreReadDryRun(")
	} else {
		require.Contains(t, helpers, "func printNoStoreReadDryRun(")
	}
	stampFn := helpers[strings.Index(helpers, "func printStampedDryRunOutput("):]
	formatAt := strings.Index(stampFn, "printOutputWithFlagsMeta(")
	stampAt := strings.Index(stampFn, "stampDryRunEnvelope(")
	require.Greater(t, stampAt, formatAt, "dry-run keys must be stamped after --agent/--select/--compact formatting")

	widget := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_widgets.go")
	session := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_sessions.go")
	if withStore {
		for _, src := range []string{widget, session} {
			wrapAt := strings.LastIndex(src, "wrapPlatformStructuredOutput(")
			cmdStampAt := strings.LastIndex(src, "stampDryRunEnvelope(")
			require.GreaterOrEqual(t, wrapAt, 0)
			require.Greater(t, cmdStampAt, wrapAt, "dry-run keys must be stamped after the last wrap")
			require.Contains(t, src, "flags.dryRun")
		}
		require.Contains(t, widget, "wrapWithProvenance(")
		require.Contains(t, widget, `stampDryRunEnvelope(wrapped, "post")`)
		require.Contains(t, session, `stampDryRunEnvelope(wrapped, "delete")`)
	} else {
		require.NotContains(t, widget, "wrapWithProvenance(")
		require.Contains(t, widget, "printStampedDryRunOutput(")
		require.Contains(t, widget, `"post"`)
		require.Contains(t, session, "printStampedDryRunOutput(")
		require.Contains(t, session, `"delete"`)
		require.NotContains(t, widget, "stampDryRunEnvelope(")
		require.NotContains(t, session, "stampDryRunEnvelope(")
	}

	readSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_pings.go")
	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_searches.go")
	require.NotContains(t, readSrc, "stampDryRunEnvelope(")
	require.NotContains(t, readSrc, "printStampedDryRunOutput(")
	require.NotContains(t, searchSrc, "stampDryRunEnvelope(")
	require.NotContains(t, searchSrc, "printStampedDryRunOutput(")
	if withStore {
		require.NotContains(t, readSrc, "printNoStoreReadDryRun(")
		require.NotContains(t, searchSrc, "printNoStoreReadDryRun(")
	} else {
		require.Contains(t, readSrc, "printNoStoreReadDryRun(")
		require.Contains(t, readSrc, `"get"`)
		require.Contains(t, readSrc, `"pings"`)
		require.Contains(t, searchSrc, "printNoStoreReadDryRun(")
		require.Contains(t, searchSrc, `"post"`)
		require.Contains(t, searchSrc, `"searches"`)
	}

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_create.go")
	require.Contains(t, createSrc, `envelope["dry_run"] = true`)
	require.NotContains(t, createSrc, "stampDryRunEnvelope(")
	require.NotContains(t, createSrc, "printNoStoreReadDryRun(")
}

func assertPromotedDryRunRuntime(t *testing.T, cli promotedDryRunCLI, withStore bool) {
	t.Helper()

	before := cli.hits.Load()
	stdout, _ := runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--dry-run", "--json")
	require.Equal(t, before, cli.hits.Load(), "promoted POST --dry-run must not dial")
	assertTopLevelDryRun(t, stdout, "post")
	if withStore {
		payload := decodeJSONObject(t, stdout)
		meta, _ := payload["meta"].(map[string]any)
		require.Equal(t, "live", meta["source"])
		results, _ := payload["results"].(map[string]any)
		require.Equal(t, true, results["dry_run"])
	}

	stdout, _ = runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--dry-run", "--json", "--select", "missing")
	assertTopLevelDryRun(t, stdout, "post")

	if !withStore {
		stdout, _ = runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--dry-run", "--json", "--agent")
		agentPayload := decodeJSONObject(t, stdout)
		require.Equal(t, true, agentPayload["dry_run"], "stdout: %s", stdout)
		require.Equal(t, "post", agentPayload["action"], "stdout: %s", stdout)
		agentMeta, _ := agentPayload["meta"].(map[string]any)
		require.Equal(t, "live", agentMeta["source"], "stdout: %s", stdout)
		agentResults, ok := agentPayload["results"].(map[string]any)
		require.True(t, ok, "agent envelope results: %s", stdout)
		require.Equal(t, true, agentResults["dry_run"], "stdout: %s", stdout)

		stdout, _ = runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--dry-run", "--json", "--agent", "--select", "dry_run")
		assertTopLevelDryRun(t, stdout, "post")
		selected := decodeJSONObject(t, stdout)
		selectedMeta, _ := selected["meta"].(map[string]any)
		require.Equal(t, "live", selectedMeta["source"], "stdout: %s", stdout)

		stdout, _ = runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--dry-run", "--json", "--compact")
		assertTopLevelDryRun(t, stdout, "post")
	}

	before = cli.hits.Load()
	stdout, _ = runPromotedDryRun(t, cli, "sessions", "sess-1", "--dry-run", "--json")
	require.Equal(t, before, cli.hits.Load(), "promoted DELETE --dry-run must not dial")
	assertTopLevelDryRun(t, stdout, "delete")

	stdout, _ = runPromotedDryRun(t, cli, "items", "create", "--name", "Ada", "--dry-run", "--json")
	assertTopLevelDryRun(t, stdout, "post")

	stdout, _ = runPromotedDryRun(t, cli, "pings", "--dry-run", "--json")
	readPayload := decodeJSONObject(t, stdout)
	if withStore {
		_, hasAction := readPayload["action"]
		require.False(t, hasAction, "store-backed promoted reads must not gain an action: %s", stdout)
		_, stamped := readPayload["dry_run"]
		require.False(t, stamped, "store-backed promoted reads keep the nested sentinel: %s", stdout)
	} else {
		assertTopLevelDryRun(t, stdout, "get")
		require.Equal(t, "pings", readPayload["resource"], "stdout: %s", stdout)
		require.Equal(t, "/pings", readPayload["path"], "stdout: %s", stdout)
		require.Equal(t, false, readPayload["success"], "stdout: %s", stdout)
		require.Equal(t, float64(0), readPayload["status"], "stdout: %s", stdout)

		stdout, _ = runPromotedDryRun(t, cli, "pings", "--dry-run", "--json", "--agent")
		agentRead := decodeJSONObject(t, stdout)
		require.Equal(t, true, agentRead["dry_run"], "stdout: %s", stdout)
		require.Equal(t, "get", agentRead["action"], "stdout: %s", stdout)
		agentMeta, _ := agentRead["meta"].(map[string]any)
		require.Equal(t, "dry-run", agentMeta["source"], "stdout: %s", stdout)
	}

	stdout, _ = runPromotedDryRun(t, cli, "searches", "--query", "q", "--dry-run", "--json")
	searchPayload := decodeJSONObject(t, stdout)
	if withStore {
		_, hasAction := searchPayload["action"]
		require.False(t, hasAction, "store-backed read-only POST must not stamp an action: %s", stdout)
		_, stamped := searchPayload["dry_run"]
		require.False(t, stamped, "store-backed read-only POST must not stamp dry_run: %s", stdout)
	} else {
		assertTopLevelDryRun(t, stdout, "post")
		require.Equal(t, "searches", searchPayload["resource"], "stdout: %s", stdout)
		require.Equal(t, "/searches", searchPayload["path"], "stdout: %s", stdout)
	}

	before = cli.hits.Load()
	stdout, _ = runPromotedDryRun(t, cli, "widgets", "--name", "Ada", "--json")
	require.Equal(t, before+1, cli.hits.Load(), "live promoted POST must dial once")
	live := decodeJSONObject(t, stdout)
	dryRun, _ := live["dry_run"].(bool)
	require.False(t, dryRun, "live promoted POST must not wear the dry-run envelope: %s", stdout)
}

func assertTopLevelDryRun(t *testing.T, stdout, action string) {
	t.Helper()
	payload := decodeJSONObject(t, stdout)
	require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
	require.Equal(t, action, payload["action"], "stdout: %s", stdout)
}

func runPromotedDryRun(t *testing.T, cli promotedDryRunCLI, args ...string) (string, string) {
	t.Helper()
	return runFrameworkBinary(t, cli.binary, cli.env, args...)
}
