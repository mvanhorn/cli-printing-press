package ppcli

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
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
	t.Setenv("CLAUDE_CONFIG_DIR", "")
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

func TestPPRunVerifiesBinaryPinAndForwardsChildFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires POSIX")
	}
	isolateHome(t)
	ppHome, err := cartridge.HomeDir()
	require.NoError(t, err)

	binDir := filepath.Join(ppHome, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o700))
	binPath := filepath.Join(binDir, "demoapp-pp-cli")
	script := []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n")
	require.NoError(t, os.WriteFile(binPath, script, 0o755))

	// Unpinned binary must be refused.
	_, err = runPP(t, "run", "demoapp", "list", "--dry-run")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not pinned")

	// Pin exact SHA-256 and verify all child flags pass through verbatim.
	sum := sha256.Sum256(script)
	lockPath, err := cartridge.LockfilePath()
	require.NoError(t, err)
	require.NoError(t, cartridge.UpdateLockfile(lockPath, func(lf *cartridge.Lockfile) error {
		lf.Pin(cartridge.LockEntry{
			Kind:   "binary",
			Name:   "demoapp",
			SHA256: hex.EncodeToString(sum[:]),
			Path:   binPath,
		})
		return nil
	}))

	out, err := runPP(t, "run", "demoapp", "orders", "--dry-run", "--limit", "5", "--custom-flag=ok")
	require.NoError(t, err)
	assert.Equal(t, "orders --dry-run --limit 5 --custom-flag=ok\n", out)

	// Tampering with the binary on disk must fail closed before exec.
	require.NoError(t, os.WriteFile(binPath, append(script, []byte("# tampered\n")...), 0o755))
	_, err = runPP(t, "run", "demoapp", "orders", "--dry-run")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHA-256 drift")
}

func TestPPBatchAndSetupCommands(t *testing.T) {
	isolateHome(t)
	t.Setenv("PP_NO_AUTO_INSTALL", "1")
	t.Setenv("PP_NO_ARDIA_COOKIES", "1")

	out, err := runPP(t, "batch", `[{"op":"route","target":"https://www.espn.com/nba/scoreboard"},{"op":"run","app":"espn","command":"scoreboard","dry_run":true}]`)
	require.NoError(t, err)
	assert.Contains(t, out, "PP_UNTRUSTED_DATA_")
	assert.Contains(t, out, `"espn"`)
	assert.Contains(t, out, `"dry_run": true`)

	outNoFence, err := runPP(t, "batch", "--no-content-boundaries", `[{"op":"route","target":"https://www.espn.com/nba/scoreboard"}]`)
	require.NoError(t, err)
	assert.NotContains(t, outNoFence, "PP_UNTRUSTED_DATA_")

	setupOut, err := runPP(t, "setup", "--json")
	require.NoError(t, err)
	assert.Contains(t, setupOut, `"printing-press"`)

	// buildBoundedRunJSON must always produce valid JSON with truncated=true.
	bigRows := make([]any, 0, 50)
	for i := range 50 {
		bigRows = append(bigRows, map[string]any{"id": i, "body": "0123456789abcdefghijklmnopqrstuvwxyz0123456789"})
	}
	bounded := buildBoundedRunJSON(&cartridge.RunResult{
		Cartridge: "demo",
		Command:   "list",
		Driver:    cartridge.DriverHTTP,
		RowCount:  len(bigRows),
		Data:      bigRows,
	}, 900, 4000)
	rawBounded, err := json.MarshalIndent(bounded, "", "  ")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(rawBounded), 900)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rawBounded, &parsed))
	assert.Equal(t, true, parsed["truncated"])
	assert.Less(t, len(parsed["data"].([]any)), 50)
}

func TestPPRunViaBrowseDryRunAndAuthSyncChromeAndCacheSkip(t *testing.T) {
	tmp := isolateHome(t)
	savedStates := make(map[string]*pressauth.State)
	origSave := savePressAuthState
	origLoad := loadPressAuthState
	savePressAuthState = func(s *pressauth.State) error {
		savedStates[s.Domain] = s
		return nil
	}
	loadPressAuthState = func(domain string) (*pressauth.State, error) {
		if s, ok := savedStates[domain]; ok {
			return s, nil
		}
		return nil, pressauth.ErrStateNotFound
	}
	t.Cleanup(func() {
		savePressAuthState = origSave
		loadPressAuthState = origLoad
	})

	// 1. `--via browse --dry-run` previews OpenCode browse transport cleanly.
	out, err := runPP(t, "run", "espn", "scoreboard", "--via", "browse", "--dry-run", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"transport": "opencode-browse-fetch"`)
	assert.Contains(t, out, `"auth_source": "opencode-browse"`)

	// Invalid --via transport must return usage error.
	_, err = runPP(t, "run", "espn", "scoreboard", "--via", "bogus", "--dry-run")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --via transport")

	// 2. `pp auth sync --browser chrome` without agentcookie sidecar returns
	// structured HandoffHint pointing to --via browse and press-auth login.
	t.Setenv("AGENTCOOKIE_SIDECAR_DB", filepath.Join(tmp, "missing-agentcookie.db"))
	_, err = runPP(t, "auth", "sync", "instacart", "--domain", "instacart.com", "--browser", "chrome", "--json")
	require.Error(t, err)
	ee, ok := errors.AsType[*cartridge.ExecError](err)
	require.True(t, ok, "expected structured *cartridge.ExecError")
	require.NotNil(t, ee.Handoff)
	assert.Contains(t, ee.Hint, "--via browse")

	// 3. `pp auth sync --browser chrome` with an agentcookie sidecar DB extracts
	// strictly the requested domain into encrypted press-auth state.
	sidecarDB := filepath.Join(tmp, "cookies-plain.db")
	db, err := sql.Open("sqlite", sidecarDB)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, path TEXT, is_secure INTEGER);
INSERT INTO cookies VALUES ('.instacart.com', '_ic_sess', 'tok_chrome_1', '/', 1);
INSERT INTO cookies VALUES ('.google.com', 'SID', 'do_not_leak', '/', 1);`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	t.Setenv("AGENTCOOKIE_SIDECAR_DB", sidecarDB)

	syncOut, err := runPP(t, "auth", "sync", "instacart", "--domain", "instacart.com", "--browser", "chrome", "--json")
	require.NoError(t, err)
	var syncDoc map[string]any
	require.NoError(t, json.Unmarshal([]byte(syncOut), &syncDoc))
	assert.Equal(t, "agentcookie", syncDoc["source"])
	assert.Equal(t, float64(1), syncDoc["cookie_count"])

	loaded, err := loadPressAuthState("instacart.com")
	require.NoError(t, err)
	assert.Equal(t, "tok_chrome_1", loaded.Cookies["_ic_sess"])
	assert.NotContains(t, loaded.Cookies, "SID")
	assert.WithinDuration(t, time.Now().UTC(), loaded.CapturedAt, 10*time.Second)

	// 4. Updating cookies via mergePressAuthCookies must preserve existing
	// RefreshEndpoint and JWTCarrierCookie and recompute JWTExpiry.
	// eyJhbGciOiJub25lIn0.eyJleHAiOjQxMDI0NDQ4MDB9.sig -> exp = 2100-01-01T00:00:00Z
	freshJWT := "eyJhbGciOiJub25lIn0.eyJleHAiOjQxMDI0NDQ4MDB9.sig"
	existing := &pressauth.State{
		Domain:           "instacart.com",
		CapturedAt:       time.Now().UTC().Add(-48 * time.Hour),
		RefreshEndpoint:  "https://www.instacart.com/api/refresh",
		JWTCarrierCookie: "session_jwt",
		JWTExpiry:        time.Now().UTC().Add(-1 * time.Hour),
		Cookies:          map[string]string{"session_jwt": "old"},
	}
	merged := mergePressAuthCookies(existing, "instacart.com", map[string]string{"session_jwt": freshJWT})
	assert.Equal(t, "https://www.instacart.com/api/refresh", merged.RefreshEndpoint)
	assert.Equal(t, "session_jwt", merged.JWTCarrierCookie)
	assert.Equal(t, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), merged.JWTExpiry)
	assert.Equal(t, freshJWT, merged.Cookies["session_jwt"])
}

func TestPPSetupApplyWiresClaudeCodexCursorIdempotently(t *testing.T) {
	tmp := isolateHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".claude"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codex"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".cursor"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".gemini", "config"), 0o700))
	t3AntiDir := filepath.Join(tmp, ".t3", "userdata", "providers", "antigravity", "profile123", "config")
	require.NoError(t, os.MkdirAll(t3AntiDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(t3AntiDir, "mcp_config.json"), []byte(`{"mcpServers":{"browse":{"command":"/usr/local/bin/browse","args":["mcp"]}}}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".agents", "skills"), 0o700))

	// Pre-populate ~/.claude.json with existing unrelated settings to verify preservation.
	claudeJSON := filepath.Join(tmp, ".claude.json")
	require.NoError(t, os.WriteFile(claudeJSON, []byte(`{"theme":"dark","mcpServers":{"existing":{"command":"demo"}}}`), 0o600))
	codexTOML := filepath.Join(tmp, ".codex", "config.toml")
	require.NoError(t, os.WriteFile(codexTOML, []byte("model = \"o3\"\n"), 0o600))

	out, err := runPP(t, "setup", "--apply", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"applied"`)

	// Verify ~/.claude.json preserved existing keys and added printing-press.
	rawClaude, err := os.ReadFile(claudeJSON)
	require.NoError(t, err)
	var claudeDoc map[string]any
	require.NoError(t, json.Unmarshal(rawClaude, &claudeDoc))
	assert.Equal(t, "dark", claudeDoc["theme"])
	mcps := claudeDoc["mcpServers"].(map[string]any)
	assert.Contains(t, mcps, "existing")
	assert.Contains(t, mcps, "printing-press")

	// Verify Cursor, Gemini/Antigravity, T3 Antigravity, and ~/.agents/skills were
	// wired while preserving existing entries, and ~/.claude/ was never written to.
	assert.FileExists(t, filepath.Join(tmp, ".cursor", "mcp.json"))
	assert.FileExists(t, filepath.Join(tmp, ".gemini", "config", "mcp_config.json"))
	assert.FileExists(t, filepath.Join(tmp, ".gemini", "config", "skills", "printing-press-pp", "SKILL.md"))
	assert.FileExists(t, filepath.Join(tmp, ".agents", "skills", "printing-press-pp", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(tmp, ".claude", "skills", "printing-press-pp", "SKILL.md"))

	rawT3Anti, err := os.ReadFile(filepath.Join(t3AntiDir, "mcp_config.json"))
	require.NoError(t, err)
	var t3AntiDoc map[string]any
	require.NoError(t, json.Unmarshal(rawT3Anti, &t3AntiDoc))
	t3Servers := t3AntiDoc["mcpServers"].(map[string]any)
	assert.Contains(t, t3Servers, "browse")
	assert.Contains(t, t3Servers, "printing-press")
	assert.FileExists(t, filepath.Join(t3AntiDir, "skills", "printing-press-pp", "SKILL.md"))

	// Verify Codex TOML is idempotent on second run and updates quoted headers in place.
	_, err = runPP(t, "setup", "--apply", "--json")
	require.NoError(t, err)
	rawCodex, err := os.ReadFile(codexTOML)
	require.NoError(t, err)
	assert.Contains(t, string(rawCodex), "[mcp_servers.printing-press]")
	assert.Equal(t, 1, bytes.Count(rawCodex, []byte("[mcp_servers.printing-press]")))

	quotedTOML := "[mcp_servers.\"printing-press\"]\ncommand = \"/old/pp\"\nargs = [\n  \"mcp\",\n  \"--old\",\n]\ntool_timeout_sec = 60\n"
	updatedTOML := updateCodexTOMLMCPTable(quotedTOML, "printing-press", "/new/pp")
	assert.Equal(t, 1, bytes.Count([]byte(updatedTOML), []byte("mcp_servers")))
	assert.Contains(t, updatedTOML, `command = "/new/pp"`)
	assert.Contains(t, updatedTOML, `args = ["mcp"]`)
	assert.NotContains(t, updatedTOML, "--old")
	assert.Contains(t, updatedTOML, "tool_timeout_sec = 60")

	// Verify platform executable name checks accept Windows pp.exe.
	assert.True(t, isPPExecutableName("pp.exe", "windows"))
	assert.True(t, isPPExecutableName("PP.EXE", "windows"))
	assert.True(t, isPPExecutableName("pp", "darwin"))
	assert.False(t, isPPExecutableName("ppcli.test", "darwin"))

	// Verify T3 Antigravity discovery works when HOME contains glob metacharacters.
	globHome := filepath.Join(t.TempDir(), "dev[1]")
	globT3Dir := filepath.Join(globHome, ".t3", "userdata", "providers", "antigravity", "profA", "config")
	require.NoError(t, os.MkdirAll(globT3Dir, 0o700))
	assert.Equal(t, []string{globT3Dir}, findT3AntigravityConfigDirs(globHome))

	// Verify CLAUDE_CONFIG_DIR wires custom profile without touching ~/.claude/.
	customClaudeDir := filepath.Join(tmp, ".claude-gmail")
	require.NoError(t, os.MkdirAll(customClaudeDir, 0o700))
	t.Setenv("CLAUDE_CONFIG_DIR", customClaudeDir)
	_, err = runPP(t, "setup", "--apply", "--json")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(customClaudeDir, ".claude.json"))
	assert.NoFileExists(t, filepath.Join(tmp, ".claude", ".claude.json"))
}
