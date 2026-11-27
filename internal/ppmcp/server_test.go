package ppmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHandshakeToolsListAndRoute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
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
}
