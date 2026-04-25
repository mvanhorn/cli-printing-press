package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustWrite is the table-test friendly helper for this file. Keeps each
// fixture construction compact since the audit reads multiple file paths
// per CLI.
func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

// makeMCPTools returns a tools.go body with n endpoint-tool registrations.
func makeMCPTools(n int) string {
	var b strings.Builder
	b.WriteString("package mcp\nfunc RegisterTools() {\n")
	for range n {
		b.WriteString("mcplib.NewTool(\"x\",)\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestRunMCPAuditNoMCPSurface(t *testing.T) {
	lib := t.TempDir()
	// CLI without cmd/*-pp-mcp — audit should report no MCP.
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "bare-cli"), 0o755))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "bare-cli", findings[0].API)
	assert.False(t, findings[0].HasMCP)
	assert.Equal(t, "n/a", findings[0].Transport)
	assert.Equal(t, "n/a", findings[0].ToolDesign)
	assert.Contains(t, findings[0].Recommend, "mcp: block")
}

func TestRunMCPAuditStdioEndpointMirror(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "small-cli")
	mustWrite(t, cli, "cmd/small-cli-pp-mcp/main.go", "package main\nfunc main() { server.ServeStdio(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(6))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.True(t, f.HasMCP)
	assert.Equal(t, "stdio", f.Transport)
	assert.Equal(t, "endpoint-mirror", f.ToolDesign)
	assert.Equal(t, 6, f.EndpointCt)
	assert.Contains(t, f.Recommend, "[stdio, http]", "small stdio mirror still recommends remote")
	assert.NotContains(t, f.Recommend, "intents", "6 endpoints is below intent recommendation threshold")
}

func TestRunMCPAuditLargeMirrorRecommendsCodeOrch(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "huge-cli")
	// Both transports + endpoint mirror with many tools.
	mustWrite(t, cli, "cmd/huge-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(60))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "both", f.Transport)
	assert.Equal(t, "endpoint-mirror", f.ToolDesign)
	assert.Equal(t, 60, f.EndpointCt)
	assert.Contains(t, f.Recommend, "orchestration: code")
	assert.NotContains(t, f.Recommend, "[stdio, http]", "transport is already both — no remote recommendation")
}

func TestRunMCPAuditCodeOrchSurface(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "cloudy-cli")
	mustWrite(t, cli, "cmd/cloudy-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", "package mcp\n")
	mustWrite(t, cli, "internal/mcp/code_orch.go", "package mcp\n")

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "both", f.Transport)
	assert.Equal(t, "code-orch", f.ToolDesign)
	assert.Equal(t, "ok", f.Recommend, "fully-modernized CLI has nothing to recommend")
}

func TestRunMCPAuditJSONRoundtrip(t *testing.T) {
	lib := t.TempDir()
	mustWrite(t, lib, "alpha/cmd/alpha-pp-mcp/main.go", "package main\nfunc main() { server.ServeStdio(s) }\n")
	mustWrite(t, lib, "alpha/internal/mcp/tools.go", makeMCPTools(3))
	mustWrite(t, lib, "beta/cmd/beta-pp-mcp/main.go", "package main\nfunc main() { server.NewStreamableHTTPServer(s); server.ServeStdio(s) }\n")
	mustWrite(t, lib, "beta/internal/mcp/tools.go", makeMCPTools(12))
	mustWrite(t, lib, "beta/internal/mcp/intents.go", makeMCPTools(4))

	cmd := newMCPAuditCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--library", lib, "--json"})
	require.NoError(t, cmd.Execute())

	var findings []MCPAuditFinding
	require.NoError(t, json.Unmarshal(out.Bytes(), &findings))
	require.Len(t, findings, 2)
	assert.Equal(t, "alpha", findings[0].API)
	assert.Equal(t, "stdio", findings[0].Transport)
	assert.Equal(t, "beta", findings[1].API)
	assert.Equal(t, "both", findings[1].Transport)
	assert.Equal(t, "intent", findings[1].ToolDesign)
	assert.Equal(t, 4, findings[1].IntentCt)
}

func TestRunMCPAuditMissingLibraryErrors(t *testing.T) {
	_, err := runMCPAudit(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err, "missing library path should surface as a clear error")
}
