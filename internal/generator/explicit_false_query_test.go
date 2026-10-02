package generator

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonPaginatedGetSendsExplicitFalseAndRejectsStrayPositionals(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var hits []capturedUpstream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, capturedUpstream{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := explicitFalseSpec()
	apiSpec.BaseURL = server.URL
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.Contains(t, helpersSrc, "func endpointPositionalArgs(")
	assert.Contains(t, helpersSrc, `--flag=false`)
	assert.Contains(t, helpersSrc, `"error": err.Error()`)
	assert.Contains(t, helpersSrc, `"usage": endpointUsageLine(cmd)`)
	assert.Contains(t, helpersSrc, `flags.asJSON || (flags.agent && !jsonChanged)`)

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "tasks_search.go")
	assert.Contains(t, searchSrc, `cmd.Flags().Changed("completed") || flagCompleted != false`)
	assert.Contains(t, searchSrc, `cmd.Flags().Changed("fuzzy") || flagFuzzy != false`)
	assert.Contains(t, searchSrc, `cmd.Flags().Changed("threshold") || flagThreshold != 0`)
	assert.Contains(t, searchSrc, "cobra.MaximumNArgs(1)")
	assert.Contains(t, searchSrc, "endpointPositionalArgs(")
	assert.NotContains(t, searchSrc, `--flag=false`)

	probeSrc := readGeneratedFile(t, outputDir, "internal", "cli", "tasks_probe.go")
	assert.Contains(t, probeSrc, `cmd.Flags().Changed("completed") || flagCompleted != false`)
	assert.Contains(t, probeSrc, "cobra.MaximumNArgs(1)")

	listSrc := readGeneratedFile(t, outputDir, "internal", "cli", "tasks_list.go")
	assert.Contains(t, listSrc, "cobra.NoArgs")
	assert.NotContains(t, listSrc, "cobra.MaximumNArgs")
	assert.Contains(t, listSrc, `cmd.Flags().Changed("archived") || flagArchived != false`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_lookup.go")
	assert.Contains(t, promotedSrc, `cmd.Flags().Changed("completed") || flagCompleted != false`)
	assert.Contains(t, promotedSrc, `cmd.Flags().Changed("threshold") || flagThreshold != 0`)
	assert.Contains(t, promotedSrc, "cobra.MaximumNArgs(1)")

	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	home := t.TempDir()
	env := []string{
		naming.EnvPrefix(apiSpec.Name) + "_BASE_URL=" + server.URL,
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
	}
	baseArgs := []string{"--home", home, "--json"}

	resetHits := func() {
		t.Helper()
		mu.Lock()
		hits = nil
		mu.Unlock()
	}
	snapshotHits := func() []capturedUpstream {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		out := make([]capturedUpstream, len(hits))
		copy(out, hits)
		return out
	}

	t.Run("equals form reaches the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "ws", "--completed=false", "--fuzzy=false", "--threshold=0")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, http.MethodGet, got[0].method)
		assert.Equal(t, "/tasks/ws/search", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["completed"])
		assert.Equal(t, []string{"false"}, got[0].query["fuzzy"])
		assert.Equal(t, []string{"0"}, got[0].query["threshold"])
	})

	t.Run("unset false and zero stay off the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "ws")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.NotContains(t, got[0].query, "completed")
		assert.NotContains(t, got[0].query, "threshold")
		assert.Equal(t, []string{"true"}, got[0].query["fuzzy"], "a default of true is still sent when the flag is unset")
	})

	t.Run("space separated boolean is rejected", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "ws", "--completed", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "accepts at most 1 arg(s), received 2")
		assert.Contains(t, stderr, "--flag=false")
		assertJSONUsageError(t, stdout, "accepts at most 1 arg(s), received 2")
		assert.Empty(t, snapshotHits())
	})

	t.Run("stray positional is rejected without boolean hint", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "ws", "extra")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "accepts at most 1 arg(s), received 2")
		assert.NotContains(t, stderr, "--flag=false")
		assert.NotContains(t, stdout, "--flag=false")
		assertJSONUsageError(t, stdout, "accepts at most 1 arg(s), received 2")
		assert.Empty(t, snapshotHits())
	})

	t.Run("agent surplus positional is a json usage error", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, "--home", home, "--agent", "tasks", "search", "ws", "extra")
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.NotContains(t, stdout+stderr, "--flag=false")
		assertJSONUsageError(t, stdout, "accepts at most 1 arg(s), received 2")
		assert.Empty(t, snapshotHits())
	})

	t.Run("agent with explicit json false keeps a plain surplus error", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, "--home", home, "--agent", "--json=false", "tasks", "search", "ws", "extra")
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "accepts at most 1 arg(s), received 2")
		assert.NotContains(t, stdout+stderr, `"error"`)
		assert.NotContains(t, stdout+stderr, `"usage"`)
		assert.NotContains(t, stdout+stderr, "--flag=false")
		assert.Empty(t, snapshotHits())
	})

	t.Run("omitted id does not accept boolean word as the id", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "--completed", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "--completed")
		assert.Contains(t, stderr, "--flag=false")
		assert.NotContains(t, stderr, "accepts at most 1 arg(s)")
		assertJSONUsageError(t, stdout, "boolean flag --completed")
		assert.Empty(t, snapshotHits())
	})

	t.Run("boolean word before a bare flag is a legitimate id", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "false", "--completed")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/tasks/false/search", got[0].path)
		assert.Equal(t, []string{"true"}, got[0].query["completed"])
	})

	t.Run("boolean word is a legitimate id", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "search", "false", "--completed=false")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/tasks/false/search", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["completed"])
	})

	t.Run("missing positional still reports required argument", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "search")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stdout+stderr, "missing required argument")
		assert.Empty(t, snapshotHits())
	})

	t.Run("head equals form reaches the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "probe", "ws", "--completed=false")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/tasks/ws/probe", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["completed"])
	})

	t.Run("head space separated boolean is rejected", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "probe", "ws", "--completed", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "--flag=false")
		assert.Empty(t, snapshotHits())
	})

	t.Run("promoted equals form reaches the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "lookup", "id1", "--completed=false", "--threshold=0")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/lookup/id1", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["completed"])
		assert.Equal(t, []string{"0"}, got[0].query["threshold"])
	})

	t.Run("promoted space separated boolean is rejected", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "lookup", "id1", "--completed", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "accepts at most 1 arg(s), received 2")
		assert.Contains(t, stderr, "--flag=false")
		assertJSONUsageError(t, stdout, "accepts at most 1 arg(s), received 2")
		assert.Empty(t, snapshotHits())
	})

	t.Run("promoted omitted id does not accept boolean word", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "lookup", "--completed", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "--flag=false")
		assertJSONUsageError(t, stdout, "boolean flag --completed")
		assert.Empty(t, snapshotHits())
	})

	t.Run("zero positional space form stays rejected", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "list", "--archived", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Empty(t, snapshotHits())
	})

	t.Run("multi positional equals form reaches the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "reassign", "ws", "id1", "--done=false")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, http.MethodPatch, got[0].method)
		assert.Equal(t, "/tasks/ws/reassign/id1", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["done"])
	})

	t.Run("multi positional missing final id does not accept boolean word", func(t *testing.T) {
		resetHits()
		code, stdout, stderr := runGeneratedBinaryExitEnv(t, binaryPath, env, append(baseArgs, "tasks", "reassign", "ws", "--done", "false")...)
		assert.Equal(t, 2, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assert.Contains(t, stderr, "--done")
		assert.Contains(t, stderr, "--flag=false")
		assertJSONUsageError(t, stdout, "boolean flag --done")
		assert.Empty(t, snapshotHits())
	})

	t.Run("multi positional boolean word id stays legitimate", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "reassign", "false", "id1", "--done=false")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/tasks/false/reassign/id1", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["done"])
	})

	t.Run("zero positional equals form reaches the wire", func(t *testing.T) {
		resetHits()
		stdout, stderr, err := runGeneratedBinaryEnv(t, binaryPath, env, append(baseArgs, "tasks", "list", "--archived=false")...)
		require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got := snapshotHits()
		require.Len(t, got, 1)
		assert.Equal(t, "/tasks", got[0].path)
		assert.Equal(t, []string{"false"}, got[0].query["archived"])
	})
}

func explicitFalseSpec() *spec.APISpec {
	apiSpec := minimalSpec("falsezero")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Learn.Enabled = false
	apiSpec.Learn.EnabledSet = false
	apiSpec.Resources = map[string]spec.Resource{
		"tasks": {
			Description: "Manage tasks",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "GET",
					Path:        "/tasks/{workspace}/search",
					Description: "Search tasks",
					Params: []spec.Param{
						{Name: "workspace", In: "path", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "completed", In: "query", Type: "bool", Description: "Completed"},
						{Name: "fuzzy", In: "query", Type: "bool", Default: true, Description: "Fuzzy"},
						{Name: "threshold", In: "query", Type: "int", Description: "Threshold"},
					},
				},
				"list": {
					Method:      "GET",
					Path:        "/tasks",
					Description: "List tasks",
					Params: []spec.Param{
						{Name: "archived", In: "query", Type: "bool", Description: "Archived"},
					},
				},
				"probe": {
					Method:      "HEAD",
					Path:        "/tasks/{workspace}/probe",
					Description: "Probe tasks",
					Params: []spec.Param{
						{Name: "workspace", In: "path", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "completed", In: "query", Type: "bool", Description: "Completed"},
					},
				},
				"reassign": {
					Method:      "PATCH",
					Path:        "/tasks/{workspace}/reassign/{task_id}",
					Description: "Reassign a task",
					Params: []spec.Param{
						{Name: "workspace", In: "path", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "task_id", In: "path", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "done", In: "query", Type: "bool", Description: "Done"},
					},
				},
			},
		},
		"lookup": {
			Description: "Look up a record",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/lookup/{id}",
					Description: "Look up a record",
					Params: []spec.Param{
						{Name: "id", In: "path", Type: "string", Required: true, Positional: true, PathParam: true},
						{Name: "completed", In: "query", Type: "bool", Description: "Completed"},
						{Name: "threshold", In: "query", Type: "int", Description: "Threshold"},
					},
				},
			},
		},
	}
	return apiSpec
}

type capturedUpstream struct {
	method string
	path   string
	query  url.Values
}

func runGeneratedBinaryEnv(t *testing.T, binaryPath string, extraEnv []string, args ...string) (string, string, error) {
	t.Helper()
	stdout, stderr, err := execGeneratedBinary(t, binaryPath, extraEnv, args...)
	return stdout, stderr, err
}

func runGeneratedBinaryExitEnv(t *testing.T, binaryPath string, extraEnv []string, args ...string) (int, string, string) {
	t.Helper()
	stdout, stderr, err := execGeneratedBinary(t, binaryPath, extraEnv, args...)
	if err == nil {
		return 0, stdout, stderr
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	return exitErr.ExitCode(), stdout, stderr
}

func assertJSONUsageError(t *testing.T, stdout, needle string) {
	t.Helper()
	assert.Contains(t, stdout, `"error"`)
	assert.Contains(t, stdout, `"usage"`)
	assert.Contains(t, stdout, needle)
}

func execGeneratedBinary(t *testing.T, binaryPath string, extraEnv []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
