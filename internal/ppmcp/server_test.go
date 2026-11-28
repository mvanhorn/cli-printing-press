package ppmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHandshakeToolsListAndRoute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))

	srv := &Server{
		Registry: &registry.Client{
			CachePath: filepath.Join(tmp, "registry.json"),
		},
		LakePath: filepath.Join(tmp, "lake.db"),
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pp_route","arguments":{"target":"https://www.espn.com/nba/scoreboard"}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	require.NoError(t, srv.ServeStdio(context.Background(), strings.NewReader(input), &out))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)

	var initResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &initResp))
	initMap, ok := initResp.Result.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, initMap["instructions"], "pp_run")

	var listResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &listResp))
	resMap, ok := listResp.Result.(map[string]any)
	require.True(t, ok)
	tools, ok := resMap["tools"].([]any)
	require.True(t, ok)
	assert.Len(t, tools, 5)

	var routeResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &routeResp))
	assert.Nil(t, routeResp.Error)
	routeResMap, ok := routeResp.Result.(map[string]any)
	require.True(t, ok)
	contentList, ok := routeResMap["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentList, 1)
	firstContent, ok := contentList[0].(map[string]any)
	require.True(t, ok)
	var decision map[string]any
	require.NoError(t, json.Unmarshal([]byte(firstContent["text"].(string)), &decision))
	assert.Equal(t, "espn", decision["app"])
	assert.Equal(t, "pp run espn scoreboard --compact", decision["recommended_cmd"])

	// pp_discover must include local cartridges with their commands.
	discResp := srv.Handle(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"pp_discover","arguments":{"query":"espn"}}`),
	})
	discMap, ok := discResp.Result.(map[string]any)
	require.True(t, ok)
	discContent := discMap["content"].([]map[string]any)
	assert.Contains(t, discContent[0]["text"], `"local_cartridges"`)
	assert.Contains(t, discContent[0]["text"], `"scoreboard"`)
}

func TestMCPRunVerifiedBinaryGuardsConfirmationBooleansAndOverflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires POSIX")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))

	ppHome, err := cartridge.HomeDir()
	require.NoError(t, err)
	binDir := filepath.Join(ppHome, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o700))
	binPath := filepath.Join(binDir, "demo-pp-cli")
	script := []byte("#!/bin/sh\nif [ \"$1\" = \"flood\" ]; then head -c 600000 /dev/zero | tr '\\0' 'a'; exit 0; fi\nprintf '%s\\n' \"$*\"\n")
	require.NoError(t, os.WriteFile(binPath, script, 0o755))

	sum := sha256.Sum256(script)
	lockPath, err := cartridge.LockfilePath()
	require.NoError(t, err)
	require.NoError(t, cartridge.UpdateLockfile(lockPath, func(lf *cartridge.Lockfile) error {
		lf.Pin(cartridge.LockEntry{
			Kind:   "binary",
			Name:   "demo",
			SHA256: hex.EncodeToString(sum[:]),
			Path:   binPath,
		})
		return nil
	}))

	srv := &Server{Registry: &registry.Client{CachePath: filepath.Join(tmp, "reg.json")}}

	// 1. Confirmation bypass via command string or --yes=true must be blocked.
	for _, rawParams := range []string{
		`{"name":"pp_run","arguments":{"app":"demo","command":"profile delete saved --yes"}}`,
		`{"name":"pp_run","arguments":{"app":"demo","command":"profile delete","args":["--yes=true"]}}`,
		`{"name":"pp_run","arguments":{"app":"demo","command":"profile delete","params":{"yes":true}}}`,
	} {
		resp := srv.Handle(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 10, Method: "tools/call", Params: json.RawMessage(rawParams)})
		resMap := resp.Result.(map[string]any)
		assert.Equal(t, true, resMap["isError"], "expected confirmation bypass to be blocked: %s", rawParams)
	}

	// 2. Boolean params must encode as --flag=false, not --flag false.
	resp := srv.Handle(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      11,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"pp_run","arguments":{"app":"demo","command":"profile show","params":{"json":false}}}`),
	})
	resMap := resp.Result.(map[string]any)
	assert.Nil(t, resMap["isError"])
	txt := resMap["content"].([]map[string]any)[0]["text"].(string)
	assert.Contains(t, txt, "--json=false")

	// 3. Output exceeding 512 KiB budget must return an explicit error.
	floodResp := srv.Handle(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      12,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"pp_run","arguments":{"app":"demo","command":"flood"}}`),
	})
	floodMap := floodResp.Result.(map[string]any)
	assert.Equal(t, true, floodMap["isError"])
	floodTxt := floodMap["content"].([]map[string]any)[0]["text"].(string)
	assert.Contains(t, floodTxt, "exceeded")
}
