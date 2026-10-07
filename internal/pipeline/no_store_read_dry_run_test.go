package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestNoStoreReadDryRunMeetsLiveDogfoodContract compiles a no-store CLI and
// checks read --json --dry-run output with the same function live dogfood
// uses, so the generator envelope and the probe cannot drift apart.
func TestNoStoreReadDryRunMeetsLiveDogfoodContract(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"item-1","name":"One"}]`))
	}))
	t.Cleanup(server.Close)

	noStore := buildDryRunContractCLI(t, false, server.URL)
	store := buildDryRunContractCLI(t, true, server.URL)

	before := hits.Load()
	stdout, stderr := runDryRunContractCLI(t, noStore, "items", "list", "--json", "--dry-run")
	require.Equal(t, before, hits.Load(), "no-store read --dry-run must not dial\nstderr:\n%s", stderr)
	require.Contains(t, stderr, "(dry run - no request sent)")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "items", "/items")
	plain := decodeDryRunContractJSON(t, stdout)
	plainMeta, _ := plain["meta"].(map[string]any)
	require.Equal(t, "dry-run", plainMeta["source"], "stdout: %s", stdout)

	stdout, _ = runDryRunContractCLI(t, noStore, "items", "list", "--json", "--dry-run", "--agent")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "items", "/items")
	agent := decodeDryRunContractJSON(t, stdout)
	meta, _ := agent["meta"].(map[string]any)
	require.Equal(t, "dry-run", meta["source"], "stdout: %s", stdout)
	results, ok := agent["results"].(map[string]any)
	require.True(t, ok, "agent results: %s", stdout)
	require.Equal(t, true, results["dry_run"], "stdout: %s", stdout)

	stdout, _ = runDryRunContractCLI(t, noStore, "items", "list", "--json", "--dry-run", "--select", "missing")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "items", "/items")

	stdout, _ = runDryRunContractCLI(t, noStore, "items", "get", "item-1", "--json", "--dry-run")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "items", "/items/item-1")

	stdout, _ = runDryRunContractCLI(t, noStore, "pings", "--json", "--dry-run")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "pings", "/pings")

	stdout, _ = runDryRunContractCLI(t, noStore, "pings", "--json", "--dry-run", "--agent")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "get", "pings", "/pings")
	promotedAgent := decodeDryRunContractJSON(t, stdout)
	promotedMeta, _ := promotedAgent["meta"].(map[string]any)
	require.Equal(t, "dry-run", promotedMeta["source"], "stdout: %s", stdout)

	stdout, _ = runDryRunContractCLI(t, noStore, "items", "create", "--name", "Ada", "--json", "--dry-run")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "post", "items", "/items")
	require.Equal(t, before, hits.Load(), "mutation --dry-run must not dial")

	stdout, _ = runDryRunContractCLI(t, store, "items", "list", "--json", "--dry-run")
	requireDryRunContractSkip(t, stdout)
	stored := decodeDryRunContractJSON(t, stdout)
	_, hasAction := stored["action"]
	require.False(t, hasAction, "store-backed reads stay unchanged: %s", stdout)
	_, topDry := stored["dry_run"]
	require.False(t, topDry, "store-backed reads keep dry_run nested: %s", stdout)

	stdout, _ = runDryRunContractCLI(t, store, "items", "list", "--json", "--dry-run", "--agent")
	requireDryRunContractSkip(t, stdout)

	stdout, _ = runDryRunContractCLI(t, store, "pings", "--json", "--dry-run")
	requireDryRunContractSkip(t, stdout)

	stdout, _ = runDryRunContractCLI(t, store, "items", "create", "--name", "Ada", "--json", "--dry-run")
	requireDryRunContractPass(t, stdout)
	assertReadDryRunEnvelope(t, stdout, "post", "items", "/items")
	require.Equal(t, before, hits.Load(), "store-backed mutation --dry-run must not dial")

	stdout, _ = runDryRunContractCLI(t, noStore, "items", "list", "--json")
	require.Equal(t, before+1, hits.Load(), "live read must still dial")
	require.NotContains(t, stdout, `"action"`)
}

func dryRunContractSpec(name, baseURL string) *spec.APISpec {
	apiSpec := &spec.APISpec{
		Name:      name,
		Version:   "0.1.0",
		BaseURL:   baseURL,
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + name + "-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
					"get": {
						Method:      "GET",
						Path:        "/items/{id}",
						Description: "Get an item",
						Params: []spec.Param{{
							Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
						}},
					},
					"create": {
						Method:      "POST",
						Path:        "/items",
						Description: "Create an item",
						Body:        []spec.Param{{Name: "name", Type: "string"}},
					},
				},
			},
			"pings": {
				Description: "Pings",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/pings", Description: "Get pings"},
				},
			},
		},
	}
	apiSpec.Learn.Disabled = true
	return apiSpec
}

func buildDryRunContractCLI(t *testing.T, withStore bool, baseURL string) dryRunContractCLI {
	t.Helper()

	name := "dry-run-contract-nostore"
	if withStore {
		name = "dry-run-contract-store"
	}
	apiSpec := dryRunContractSpec(name, baseURL)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := generator.New(apiSpec, outputDir)
	if withStore {
		gen.VisionSet = generator.VisionTemplateSet{Store: true, Sync: true}
	} else {
		gen.VisionSet = generator.VisionTemplateSet{Export: true}
	}
	require.NoError(t, gen.Generate())

	cliName := naming.CLI(apiSpec.Name)
	binary := filepath.Join(outputDir, cliName)
	cmd := exec.Command("go", "build", "-mod=mod", "-o", binary, "./cmd/"+cliName)
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	env := dryRunContractEnv(t, naming.EnvPrefix(apiSpec.Name), baseURL)
	return dryRunContractCLI{binary: binary, env: env}
}

type dryRunContractCLI struct {
	binary string
	env    []string
}

func dryRunContractEnv(t *testing.T, prefix, baseURL string) []string {
	t.Helper()
	home := t.TempDir()
	configHome := filepath.Join(home, ".config")
	dataHome := filepath.Join(home, ".local", "share")
	stateHome := filepath.Join(home, ".local", "state")
	cacheHome := filepath.Join(home, ".cache")
	for _, dir := range []string{configHome, dataHome, stateHome, cacheHome} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	return append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_DATA_HOME="+dataHome,
		"XDG_STATE_HOME="+stateHome,
		"XDG_CACHE_HOME="+cacheHome,
		prefix+"_CONFIG=",
		prefix+"_CONFIG_DIR=",
		prefix+"_DATA_DIR=",
		prefix+"_STATE_DIR=",
		prefix+"_CACHE_DIR=",
		prefix+"_HOME=",
		prefix+"_BASE_URL="+baseURL,
	)
}

func runDryRunContractCLI(t *testing.T, cli dryRunContractCLI, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(cli.binary, args...)
	cmd.Env = cli.env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "args: %v\nstdout:\n%s\nstderr:\n%s", args, stdout.String(), stderr.String())
	return stdout.String(), stderr.String()
}

func requireDryRunContractPass(t *testing.T, stdout string) {
	t.Helper()
	status, reason := liveDogfoodDryRunJSONContract(liveDogfoodRun{exitCode: 0, stdout: stdout}, false)
	require.Equal(t, LiveDogfoodStatusPass, status, "reason=%s stdout=%s", reason, stdout)
	require.Empty(t, reason)
}

func requireDryRunContractSkip(t *testing.T, stdout string) {
	t.Helper()
	status, reason := liveDogfoodDryRunJSONContract(liveDogfoodRun{exitCode: 0, stdout: stdout}, false)
	require.Equal(t, LiveDogfoodStatusSkip, status, "reason=%s stdout=%s", reason, stdout)
	require.Equal(t, "command does not honour --dry-run", reason)
}

func assertReadDryRunEnvelope(t *testing.T, stdout, action, resource, path string) {
	t.Helper()
	payload := decodeDryRunContractJSON(t, stdout)
	require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
	require.Equal(t, action, payload["action"], "stdout: %s", stdout)
	require.Equal(t, resource, payload["resource"], "stdout: %s", stdout)
	require.Equal(t, path, payload["path"], "stdout: %s", stdout)
	require.Equal(t, false, payload["success"], "stdout: %s", stdout)
	require.Equal(t, float64(0), payload["status"], "stdout: %s", stdout)
}

func decodeDryRunContractJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout: %s", stdout)
	return payload
}
