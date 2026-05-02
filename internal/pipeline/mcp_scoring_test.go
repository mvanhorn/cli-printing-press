package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMCPFile is a table-test friendly helper that mkdir-p's the path and
// writes body. Kept local so mcp_scoring_test.go stays self-contained.
func writeMCPFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

// stdioOnlyMain mimics the template output for a spec that did not opt into
// remote transport. The scorer inspects strings, so the exact surrounding
// code doesn't matter — only that ServeStdio appears and ServeStreamableHTTP /
// NewStreamableHTTPServer do not.
const stdioOnlyMain = `package main
func main() { server.ServeStdio(s) }
`

// remoteOnlyMain simulates a hypothetical http-only spec. The generator
// doesn't emit this today (stdio is always in the effective list when http
// is declared), but the scorer should still award the correct middle band
// so it remains valid if a future template change lands.
const remoteOnlyMain = `package main
func main() { httpSrv := server.NewStreamableHTTPServer(s); httpSrv.Start(":7777") }
`

// bothTransportsMain is what the current template emits when a spec declares
// transport: [stdio, http]. Both branches show up in the same source file.
const bothTransportsMain = `package main
func main() {
	switch *transport {
	case "stdio":
		server.ServeStdio(s)
	case "http":
		httpSrv := server.NewStreamableHTTPServer(s)
		httpSrv.Start(*addr)
	}
}
`

func TestScoreMCPRemoteTransport(t *testing.T) {
	t.Run("unscored when no MCP emitted", func(t *testing.T) {
		dir := t.TempDir()
		score, scored := scoreMCPRemoteTransport(dir)
		assert.False(t, scored, "no MCP surface → unscored")
		assert.Equal(t, 0, score)
	})

	t.Run("stdio only scores baseline", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		score, scored := scoreMCPRemoteTransport(dir)
		assert.True(t, scored)
		assert.Equal(t, 5, score, "stdio-only gets middle-low band (remote-unreachable)")
	})

	t.Run("http only scores partial", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", remoteOnlyMain)
		score, scored := scoreMCPRemoteTransport(dir)
		assert.True(t, scored)
		assert.Equal(t, 7, score)
	})

	t.Run("both transports score full", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", bothTransportsMain)
		score, scored := scoreMCPRemoteTransport(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score)
	})

	t.Run("manifest cli_name selects canonical mcp dir over duplicates", func(t *testing.T) {
		dir := t.TempDir()
		// Duplicate -pp-mcp dirs must not shadow the canonical one. The
		// duplicate sorts lexically first, so a suffix-only scan would pick
		// it and read the wrong main.go.
		writeMCPFile(t, dir, "cmd/demo-pp-cli-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", bothTransportsMain)
		writeMCPFile(t, dir, ".printing-press.json", `{"cli_name": "demo-pp-cli"}`)

		score, scored := scoreMCPRemoteTransport(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score, "scorer must read the canonical demo-pp-mcp main.go, not the lexically-first duplicate")
	})

	t.Run("falls back to suffix scan when manifest is missing", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", bothTransportsMain)

		score, scored := scoreMCPRemoteTransport(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score, "no manifest → legacy suffix scan still works")
	})
}

// buildToolsGo fabricates an internal/mcp/tools.go containing `n` endpoint
// tool registrations. The scorer counts mcplib.NewTool( occurrences, so the
// surrounding code is irrelevant.
func buildToolsGo(n int) string {
	var b strings.Builder
	b.WriteString("package mcp\nfunc RegisterTools(s *server.MCPServer) {\n")
	for i := range n {
		b.WriteString("\tmcplib.NewTool(\"endpoint_")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\",)\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestScoreMCPToolDesign(t *testing.T) {
	t.Run("unscored when no MCP emitted", func(t *testing.T) {
		dir := t.TempDir()
		score, scored := scoreMCPToolDesign(dir)
		assert.False(t, scored)
		assert.Equal(t, 0, score)
	})

	t.Run("unscored when endpoint count under threshold", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(5))
		score, scored := scoreMCPToolDesign(dir)
		assert.False(t, scored, "small surfaces don't get docked for not using intents")
		assert.Equal(t, 0, score)
	})

	t.Run("endpoint mirror at scale scores baseline", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(20))
		score, scored := scoreMCPToolDesign(dir)
		assert.True(t, scored)
		assert.Equal(t, 5, score, "plain endpoint-mirror at scale gets baseline 5, not zero")
	})

	t.Run("code orchestration wins full marks", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", "package mcp\n")
		writeMCPFile(t, dir, "internal/mcp/code_orch.go", "package mcp\nfunc RegisterCodeOrchestrationTools() {}\n")
		score, scored := scoreMCPToolDesign(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score)
	})

	t.Run("intents with good coverage score full marks", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(10))
		// 5 intents vs 10 endpoints → ratio 0.33, above 0.3 threshold.
		writeMCPFile(t, dir, "internal/mcp/intents.go", buildToolsGo(5))
		score, scored := scoreMCPToolDesign(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score)
	})

	t.Run("sparse intents score partial", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(20))
		// 2 intents vs 20 endpoints → ratio 0.09, well below 0.3.
		writeMCPFile(t, dir, "internal/mcp/intents.go", buildToolsGo(2))
		score, scored := scoreMCPToolDesign(dir)
		assert.True(t, scored)
		assert.Equal(t, 7, score)
	})
}

func TestScoreMCPSurfaceStrategy(t *testing.T) {
	t.Run("unscored at small scale", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(20))
		score, scored := scoreMCPSurfaceStrategy(dir)
		assert.False(t, scored, "below 50-endpoint threshold → strategy doesn't matter")
		assert.Equal(t, 0, score)
	})

	t.Run("endpoint mirror at scale is the article's anti-pattern", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(80))
		score, scored := scoreMCPSurfaceStrategy(dir)
		assert.True(t, scored)
		assert.Equal(t, 2, score, "endpoint mirror at 80 endpoints scores low, not zero")
	})

	t.Run("intents at scale score partial", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", buildToolsGo(60))
		writeMCPFile(t, dir, "internal/mcp/intents.go", buildToolsGo(4))
		score, scored := scoreMCPSurfaceStrategy(dir)
		assert.True(t, scored)
		assert.Equal(t, 7, score)
	})

	t.Run("code orchestration wins at any scale", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPFile(t, dir, "cmd/demo-pp-mcp/main.go", stdioOnlyMain)
		writeMCPFile(t, dir, "internal/mcp/tools.go", "package mcp\n")
		writeMCPFile(t, dir, "internal/mcp/code_orch.go", "package mcp\n")
		score, scored := scoreMCPSurfaceStrategy(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score)
	})
}
