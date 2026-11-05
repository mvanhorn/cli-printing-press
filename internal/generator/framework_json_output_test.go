package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestFrameworkJSONWithDisabledLearningAndEmptyTail(t *testing.T) {
	t.Parallel()
	var payload atomic.Value
	var hits atomic.Int64
	payload.Store(`[]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload.Load().(string)))
	}))
	defer server.Close()
	api := smallReadWriteSyncableOutputSpec("framework-json")
	api.BaseURL, api.Auth = server.URL, spec.AuthConfig{Type: "none"}
	api.Learn.Enabled = true
	output := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	gen := New(api, output)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Tail: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, output)
	binaryName := platform.ExecutablePath("framework-json-bin")
	runGoCommandRequired(t, output, "build", "-o", "./"+binaryName, "./cmd/"+naming.CLI(api.Name))
	run := func(args []string, disabled bool) []byte {
		t.Helper()
		cmd := exec.Command(filepath.Join(output, binaryName), args...)
		cmd.Env = sandboxHomeEnv(t)
		if disabled {
			cmd.Env = append(cmd.Env, "FRAMEWORK_JSON_NO_LEARN=true")
		}
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		require.NoError(t, cmd.Run(), "stdout=%s stderr=%s", &stdout, &stderr)
		require.True(t, json.Valid([]byte(stdout.String())), "stdout=%q stderr=%q", &stdout, &stderr)
		return []byte(stdout.String())
	}
	for _, command := range [][]string{{"teach"}, {"teach-playbook"}, {"playbook", "amend"}} {
		for _, flag := range []bool{false, true} {
			args := append(append([]string{}, command...), "--json")
			if flag {
				args = append(args, "--no-learn")
			}
			var body map[string]any
			require.NoError(t, json.Unmarshal(run(args, !flag), &body))
			require.Equal(t, true, body["disabled"])
			require.NoError(t, json.Unmarshal(run(append(args, "--dry-run"), !flag), &body))
			require.Equal(t, true, body["dry_run"])
		}
	}
	for _, sample := range []string{`[]`, `[{"id":"1"},{"id":"2"}]`} {
		payload.Store(sample)
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(run([]string{"tail", "deliveries", "--follow=false", "--json"}, true), &rows))
		var expected []map[string]any
		require.NoError(t, json.Unmarshal([]byte(sample), &expected))
		require.Equal(t, expected, rows)
		before := hits.Load()
		var dryRun map[string]any
		require.NoError(t, json.Unmarshal(run([]string{"tail", "deliveries", "--follow=false", "--json", "--dry-run"}, true), &dryRun))
		require.Equal(t, true, dryRun["dry_run"])
		require.Equal(t, "tail", dryRun["action"])
		require.Equal(t, before, hits.Load(), "tail dry-run must not contact the API")
	}
}
