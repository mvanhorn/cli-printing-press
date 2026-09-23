package generator

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

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestFrameworkCommandsHonorDryRun pins the framework-command dry-run contract
// against a compiled CLI. Endpoint mirrors already short-circuit; these
// commands are emitted for every API and were skipping the guard.
func TestFrameworkCommandsHonorDryRun(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"item-1","name":"one"}]`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("framework-dry-run")
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

	home := t.TempDir()
	env := frameworkDryRunEnv(t, home, naming.EnvPrefix(apiSpec.Name))
	run := func(args ...string) (string, string) {
		t.Helper()
		return runFrameworkBinary(t, binaryPath, env, args...)
	}

	stdout, _ := run("doctor", "--dry-run", "--json")
	require.Zero(t, hits.Load(), "doctor --dry-run must not dial the API")
	assertDryRunEnvelope(t, stdout, "doctor")

	stdout, _ = run("doctor", "--dry-run")
	require.Zero(t, hits.Load(), "doctor --dry-run must not dial the API")
	require.Contains(t, stdout, "dry-run: would run doctor")
	require.False(t, json.Valid([]byte(stdout)), "human dry-run must stay prose")

	stdout, _ = run("feedback", "list", "--json", "--dry-run")
	assertDryRunEnvelope(t, stdout, "feedback list")
	stdout, _ = run("feedback", "list", "--json")
	requireJSONArray(t, stdout)

	stdout, _ = run("profile", "list", "--json", "--dry-run")
	assertDryRunEnvelope(t, stdout, "profile list")
	stdout, _ = run("profile", "list", "--json")
	requireJSONArray(t, stdout)

	// A local read that already emits a JSON object must keep doing that.
	// An empty dry-run envelope would hide the status it exists to report.
	stdout, _ = run("workflow", "status", "--json", "--dry-run", "--db", filepath.Join(t.TempDir(), "missing.db"))
	status := decodeJSONObject(t, stdout)
	_, stamped := status["dry_run"]
	require.False(t, stamped, "workflow status has no side effects to preview: %s", stdout)

	showCmd := exec.Command(binaryPath, "profile", "show", "missing", "--json", "--dry-run")
	showCmd.Env = env
	var showOut, showErr strings.Builder
	showCmd.Stdout = &showOut
	showCmd.Stderr = &showErr
	require.Error(t, showCmd.Run(), "profile show --dry-run must still resolve the named profile")
	require.NotContains(t, showOut.String(), `"dry_run"`)
	require.Contains(t, showErr.String()+showOut.String(), "not found")

	syncDB := filepath.Join(t.TempDir(), "sync-dry.db")
	before := hits.Load()
	stdout, stderr := run("sync", "--dry-run", "--json", "--db", syncDB)
	require.Equal(t, before, hits.Load(), "sync --dry-run must not dial the API\nstderr:\n%s", stderr)
	require.Contains(t, stderr, "dry run - no request sent")
	preview := decodeJSONObject(t, stdout)
	require.Equal(t, true, preview["dry_run"])
	require.Equal(t, "sync", preview["action"])
	for _, key := range []string{"total_records", "resources", "success", "warned", "errored", "duration_ms"} {
		_, ok := preview[key]
		require.True(t, ok, "sync dry-run preview missing %s: %s", key, stdout)
	}

	archiveDB := filepath.Join(t.TempDir(), "archive.db")
	sentinel := []byte("not-a-database")
	require.NoError(t, os.WriteFile(archiveDB, sentinel, 0o600))
	before = hits.Load()
	stdout, _ = run("workflow", "archive", "--dry-run", "--json", "--full", "--db", archiveDB)
	assertDryRunEnvelope(t, stdout, "workflow archive")
	require.Equal(t, before, hits.Load(), "workflow archive --dry-run must not dial the API")
	kept, err := os.ReadFile(archiveDB)
	require.NoError(t, err)
	require.Equal(t, sentinel, kept, "workflow archive --dry-run --full must not open or rewrite the store")

	before = hits.Load()
	stdout, _ = run("doctor", "--json")
	require.Greater(t, hits.Load(), before, "doctor without --dry-run must still probe the API")
	report := decodeJSONObject(t, stdout)
	_, stamped = report["dry_run"]
	require.False(t, stamped, "live doctor report must not wear the dry-run envelope: %s", stdout)

	liveSyncDB := filepath.Join(t.TempDir(), "sync-live.db")
	before = hits.Load()
	stdout, _ = run("sync", "--json", "--db", liveSyncDB)
	require.Greater(t, hits.Load(), before, "sync without --dry-run must still fetch")
	liveSummary := decodeJSONObject(t, stdout)
	_, stamped = liveSummary["dry_run"]
	require.False(t, stamped, "live sync summary must not wear the dry-run envelope: %s", stdout)
	_, ok := liveSummary["total_records"]
	require.True(t, ok, "live sync summary: %s", stdout)

	liveArchiveDB := filepath.Join(t.TempDir(), "archive-live.db")
	before = hits.Load()
	_, _ = run("workflow", "archive", "--json", "--db", liveArchiveDB)
	require.Greater(t, hits.Load(), before, "workflow archive without --dry-run must still archive")
	info, err := os.Stat(liveArchiveDB)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

func frameworkDryRunEnv(t *testing.T, home, prefix string) []string {
	t.Helper()
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
	)
}

func runFrameworkBinary(t *testing.T, binaryPath string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "args: %v\nstdout:\n%s\nstderr:\n%s", args, stdout.String(), stderr.String())
	return stdout.String(), stderr.String()
}

func assertDryRunEnvelope(t *testing.T, stdout, action string) {
	t.Helper()
	payload := decodeJSONObject(t, stdout)
	require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
	require.Equal(t, action, payload["action"], "stdout: %s", stdout)
	would, _ := payload["would"].(string)
	require.NotEmpty(t, would, "stdout: %s", stdout)
}

func decodeJSONObject(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout: %s", stdout)
	return payload
}

func requireJSONArray(t *testing.T, stdout string) {
	t.Helper()
	var payload []any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout: %s", stdout)
}
