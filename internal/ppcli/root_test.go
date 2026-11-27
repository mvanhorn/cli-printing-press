package ppcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))
	return tmp
}

func runPP(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func TestPPListInfoRouteRecordAuditAndEject(t *testing.T) {
	tmp := isolateHome(t)

	out, err := runPP(t, "list", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"espn"`)
	assert.Contains(t, out, `"hackernews"`)

	out, err = runPP(t, "info", "espn", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"site.api.espn.com"`)

	out, err = runPP(t, "route", "https://www.espn.com/nba/scoreboard", "--json")
	require.NoError(t, err)
	var routeDoc map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &routeDoc))
	assert.Equal(t, true, routeDoc["matched"])
	assert.Equal(t, "espn", routeDoc["app"])

	// Record from a synthetic HAR file and verify install + lockfile pin + clean audit.
	harPath := filepath.Join(tmp, "capture.har")
	harBody := `{"log":{"entries":[{"request":{"method":"GET","url":"https://api.demo.test/v1/scores","headers":[]},"response":{"status":200,"headers":[{"name":"Content-Type","value":"application/json"}],"content":{"mimeType":"application/json","size":24,"text":"{\"scores\":[{\"id\":\"1\"}]}"}}}]}}`
	require.NoError(t, os.WriteFile(harPath, []byte(harBody), 0o600))

	out, err = runPP(t, "record", "--from-har", harPath, "--name", "demo-scores", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"demo-scores"`)

	out, err = runPP(t, "audit", "--no-default-mcp-scan", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"grade": "A"`)

	out, err = runPP(t, "ardia", "hook-snippet")
	require.NoError(t, err)
	assert.Contains(t, out, `"printing-press-omnibox"`)

	ejectDir := filepath.Join(tmp, "ejected")
	out, err = runPP(t, "eject", "espn", "--output", ejectDir)
	require.NoError(t, err)
	assert.Contains(t, out, "Ejected espn")
	assert.FileExists(t, filepath.Join(ejectDir, "cartridge.yaml"))
	assert.FileExists(t, filepath.Join(ejectDir, "SKILL.md"))
}
