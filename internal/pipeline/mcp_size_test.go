package pipeline

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMCPTools writes a generated tools.go with the provided RegisterTools
// body and returns the CLI dir. The body is inserted into a minimal file
// stub that matches what the MCP template produces.
func writeMCPTools(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "internal", "mcp")
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))
	content := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterTools(s *server.MCPServer) {
` + body + `
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"), []byte(content), 0o644))
	return dir
}

func writeMCPCLISource(t *testing.T, dir, name, content string) {
	t.Helper()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	writeFile(t, filepath.Join(cliDir, name), content)
}

func writeCodeOrchSurface(t *testing.T, endpointCount int) string {
	t.Helper()
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "internal", "mcp")
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))

	toolsBody := `package mcp

func RegisterTools(s *server.MCPServer) {
	RegisterCodeOrchestrationTools(s)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"), []byte(toolsBody), 0o644))

	var endpoints strings.Builder
	for i := range endpointCount {
		endpoints.WriteString(`
	{ID: "items.endpoint_`)
		endpoints.WriteString(strconv.Itoa(i))
		endpoints.WriteString(`", Method: "GET", Path: "/items", Summary: "`)
		endpoints.WriteString(strings.Repeat("x", 40))
		endpoints.WriteString(`"},
`)
	}
	codeOrchBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

type codeOrchEndpoint struct {
	ID      string
	Method  string
	Path    string
	Summary string
}

var codeOrchEndpoints = []codeOrchEndpoint{` + endpoints.String() + `}

func RegisterCodeOrchestrationTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("demo_search", mcplib.WithDescription("Search the API.")), nil)
	s.AddTool(mcplib.NewTool("demo_execute", mcplib.WithDescription("Execute one endpoint.")), nil)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "code_orch.go"), []byte(codeOrchBody), 0o644))
	return dir
}

func TestEstimateMCPTokens_NoMCPSurface(t *testing.T) {
	dir := t.TempDir()
	est := estimateMCPTokens(dir)
	assert.Equal(t, 0, est.TotalChars)
	assert.Equal(t, 0, est.ToolCount)
}

func TestEstimateMCPTokens_StubWithoutRegisterTools(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "internal", "mcp")
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"),
		[]byte(`package mcp`), 0o644))

	est := estimateMCPTokens(dir)
	assert.Equal(t, 0, est.ToolCount, "stub without RegisterTools treated as no MCP surface")
}

func TestEstimateMCPTokens_SingleSmallTool(t *testing.T) {
	dir := writeMCPTools(t, `
	s.AddTool(
		mcplib.NewTool("get_user",
			mcplib.WithDescription("Retrieve a user by ID."),
			mcplib.WithString("id", mcplib.Description("User ID"))),
		handleGetUser)
`)
	est := estimateMCPTokens(dir)
	require.Equal(t, 1, est.ToolCount)
	assert.Equal(t, "get_user", est.PerTool[0].Name)
	assert.Greater(t, est.TotalChars, 0)
	assert.Greater(t, est.TotalTokens, 0)
	// small tool should be well under 80 tokens
	assert.Less(t, est.PerTool[0].Tokens, 80)
}

func TestEstimateMCPTokens_MultipleToolsTopHeaviest(t *testing.T) {
	// Three tools with increasing description length so TopHeaviest
	// ordering is deterministic.
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("small", mcplib.WithDescription("tiny")), nil)
	s.AddTool(mcplib.NewTool("medium", mcplib.WithDescription("a medium description here")), nil)
	s.AddTool(mcplib.NewTool("huge", mcplib.WithDescription("`+strings.Repeat("x", 500)+`")), nil)
`)
	est := estimateMCPTokens(dir)
	require.Equal(t, 3, est.ToolCount)
	require.Len(t, est.TopHeaviest, 3)
	assert.Equal(t, "huge", est.TopHeaviest[0].Name, "largest tool listed first in TopHeaviest")
	assert.Greater(t, est.TopHeaviest[0].Chars, est.TopHeaviest[1].Chars)
	assert.Greater(t, est.TopHeaviest[1].Chars, est.TopHeaviest[2].Chars)
}

func TestEstimateMCPTokens_IncludesCobratreeRuntimeTools(t *testing.T) {
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("typed_get", mcplib.WithDescription("Typed endpoint.")), nil)
	cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)
`)
	writeMCPCLISource(t, dir, "root.go", `package cli

import "github.com/spf13/cobra"

func RootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "demo-pp-cli"}
	rootCmd.AddCommand(newDigestCmd())
	rootCmd.AddCommand(newTrendsCmd())
	rootCmd.AddCommand(newEndpointCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newHiddenCmd())
	return rootCmd
}

func newDigestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "digest",
		Short: "Build a compact daily digest.",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}

func newTrendsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trends",
		Long:  "Compare trending entities across synced data and explain what changed.",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}

func newEndpointCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "typed-endpoint",
		Short:       "Already covered by a typed MCP tool.",
		Annotations: map[string]string{"pp:endpoint": "items.get"},
		RunE:        func(cmd *cobra.Command, args []string) error { return nil },
	}
}

func newAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Framework command skipped by cobratree.",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
}

func newHiddenCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "debug-hidden",
		Short:       "Hidden from MCP.",
		Annotations: map[string]string{"mcp:hidden": "true"},
		RunE:        func(cmd *cobra.Command, args []string) error { return nil },
	}
}
`)

	est := estimateMCPTokens(dir)
	require.Equal(t, 3, est.ToolCount, "typed tool plus two cobratree runtime tools should all count")
	names := make([]string, 0, len(est.PerTool))
	for _, tool := range est.PerTool {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "typed_get")
	assert.Contains(t, names, "cobratree:digest")
	assert.Contains(t, names, "cobratree:trends")
}

func TestScoreMCPTokenEfficiency_FullMarksForLeanSurface(t *testing.T) {
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("get", mcplib.WithDescription("get item")), nil)
	s.AddTool(mcplib.NewTool("list", mcplib.WithDescription("list items")), nil)
`)
	score, scored := scoreMCPTokenEfficiency(dir)
	assert.True(t, scored)
	assert.Equal(t, 10, score, "small per-tool size earns full marks")
}

func TestScoreMCPTokenEfficiency_NotScoredWhenNoMCP(t *testing.T) {
	dir := t.TempDir()
	score, scored := scoreMCPTokenEfficiency(dir)
	assert.False(t, scored, "missing MCP surface must be unscored, not zero-scored")
	assert.Equal(t, 0, score)
}

func TestScoreMCPTokenEfficiency_PenalizesBloatedSurface(t *testing.T) {
	// One tool with a ~1600-char description → ~400 tokens, above the
	// 320-token ceiling → 0 score.
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("bloated", mcplib.WithDescription("`+strings.Repeat("x", 1600)+`")), nil)
`)
	score, scored := scoreMCPTokenEfficiency(dir)
	assert.True(t, scored)
	assert.Equal(t, 0, score, "oversized per-tool description scores zero")
}

func TestScoreMCPTokenEfficiency_PartialCreditMedium(t *testing.T) {
	// One tool at ~500 chars → ~125 tokens → partial credit (7)
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("medium", mcplib.WithDescription("`+strings.Repeat("x", 500)+`")), nil)
`)
	score, scored := scoreMCPTokenEfficiency(dir)
	assert.True(t, scored)
	assert.Equal(t, 7, score)
}

func TestScoreMCPTokenEfficiency_UnscoredForLargeCodeOrchCatalog(t *testing.T) {
	dir := writeCodeOrchSurface(t, 200)

	score, scored := scoreMCPTokenEfficiency(dir)
	assert.False(t, scored, "large code-orchestrated catalogs should be unscored instead of zero-scored")
	assert.Equal(t, 0, score)

	sc := &Scorecard{}
	scoreInfrastructureDimensions(sc, dir)
	assert.Contains(t, sc.UnscoredDimensions, DimMCPTokenEfficiency)
}

func TestScoreMCPTokenEfficiency_ScoresSmallCodeOrchCatalog(t *testing.T) {
	dir := writeCodeOrchSurface(t, 20)

	score, scored := scoreMCPTokenEfficiency(dir)
	assert.True(t, scored, "small code-orchestrated catalogs should still use the scoring bands")
	assert.Greater(t, score, 0)
}

func TestScoreMCPTokenEfficiency_EndpointMirrorBehaviorUnchanged(t *testing.T) {
	dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("bloated", mcplib.WithDescription("`+strings.Repeat("x", 1600)+`")), nil)
`)

	score, scored := scoreMCPTokenEfficiency(dir)
	assert.True(t, scored)
	assert.Equal(t, 0, score)
}

// TestEstimateMCPTokens_RuntimeSurfaceSelection captures WU-A4 from
// retro umbrella #516: when the MCP main.go has surface-selection logic
// defaulting to a thin code-orchestration surface, the agent loads the
// thin surface (e.g., dub_search + dub_execute), NOT the full
// endpoint-mirror in tools.go. The token-efficiency score must reflect
// the actual default surface, not the static tools.go.
func TestEstimateMCPTokens_RuntimeSurfaceSelection(t *testing.T) {
	t.Run("code orchestration delegation selects code_orch.go", func(t *testing.T) {
		dir := t.TempDir()
		mcpDir := filepath.Join(dir, "internal", "mcp")
		require.NoError(t, os.MkdirAll(mcpDir, 0o755))

		toolsBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterTools(s *server.MCPServer) {
	RegisterCodeOrchestrationTools(s)
}

func helperOne() string { return "` + strings.Repeat("x", 900) + `" }
func helperTwo() string { return "` + strings.Repeat("y", 900) + `" }
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"), []byte(toolsBody), 0o644))

		codeOrchBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterCodeOrchestrationTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("gohighlevel_search", mcplib.WithDescription("Search the API.")), nil)
	s.AddTool(mcplib.NewTool("gohighlevel_execute", mcplib.WithDescription("Execute one endpoint.")), nil)
}
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "code_orch.go"), []byte(codeOrchBody), 0o644))

		cmdDir := filepath.Join(dir, "cmd", "gohighlevel-pp-mcp")
		require.NoError(t, os.MkdirAll(cmdDir, 0o755))
		mainBody := `package main

import (
	"os"
	mcptools "demo-pp-cli/internal/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("Demo", "1.0.0")
	mcptools.RegisterTools(s)
	_ = os.Getenv("PP_MCP_TRANSPORT")
}
`
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainBody), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"),
			[]byte(`{"cli_name":"gohighlevel-pp-cli"}`), 0o644))

		est := estimateMCPTokens(dir)
		require.Equal(t, 2, est.ToolCount, "current code-orch CLIs delegate from RegisterTools into code_orch.go")
		assert.Less(t, est.TotalTokens, 100)
	})

	t.Run("thin default selects code_orch.go for token counting", func(t *testing.T) {
		dir := t.TempDir()
		mcpDir := filepath.Join(dir, "internal", "mcp")
		require.NoError(t, os.MkdirAll(mcpDir, 0o755))

		// Write a "tools.go" with 3 verbose endpoint mirrors that would
		// score badly on token efficiency if read directly.
		toolsBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("verbose_endpoint_one", mcplib.WithDescription("` + strings.Repeat("x", 400) + `")), nil)
	s.AddTool(mcplib.NewTool("verbose_endpoint_two", mcplib.WithDescription("` + strings.Repeat("y", 400) + `")), nil)
	s.AddTool(mcplib.NewTool("verbose_endpoint_three", mcplib.WithDescription("` + strings.Repeat("z", 400) + `")), nil)
}
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"), []byte(toolsBody), 0o644))

		// Write a "code_orch.go" with the thin 2-tool surface — short descriptions.
		codeOrchBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterCodeOrchestrationTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("dub_search", mcplib.WithDescription("Search the API.")), nil)
	s.AddTool(mcplib.NewTool("dub_execute", mcplib.WithDescription("Execute one endpoint.")), nil)
}
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "code_orch.go"), []byte(codeOrchBody), 0o644))

		// Write a "main.go" that defaults DUB_MCP_SURFACE to "thin".
		cmdDir := filepath.Join(dir, "cmd", "demo-pp-mcp")
		require.NoError(t, os.MkdirAll(cmdDir, 0o755))
		mainBody := `package main

import (
	"os"
	mcptools "demo-pp-cli/internal/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("Demo", "1.0.0")
	surface := os.Getenv("DUB_MCP_SURFACE")
	if surface == "" {
		surface = "thin"
	}
	switch surface {
	case "thin":
		mcptools.RegisterCodeOrchestrationTools(s)
	case "full":
		mcptools.RegisterTools(s)
	}
}
`
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainBody), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"),
			[]byte(`{"cli_name":"demo-pp-cli"}`), 0o644))

		est := estimateMCPTokens(dir)
		// EXPECT: scorer sees the thin surface (2 tools, short descriptions),
		// NOT the full surface (3 tools with 400-char descriptions each).
		require.Equal(t, 2, est.ToolCount, "should count code_orch.go's 2 tools, not tools.go's 3")
		assert.Less(t, est.TotalTokens, 100, "thin surface tools have short descriptions; should be well under 100 total tokens")
	})

	t.Run("no surface selection falls through to tools.go (no regression)", func(t *testing.T) {
		// Existing behavior preserved when there's no main.go surface
		// selection: just read tools.go.
		dir := writeMCPTools(t, `
	s.AddTool(mcplib.NewTool("get_thing", mcplib.WithDescription("Retrieve a thing.")), nil)
`)
		est := estimateMCPTokens(dir)
		require.Equal(t, 1, est.ToolCount)
		assert.Equal(t, "get_thing", est.PerTool[0].Name)
	})

	t.Run("full default selects tools.go (DUB_MCP_SURFACE=full not thin)", func(t *testing.T) {
		dir := t.TempDir()
		mcpDir := filepath.Join(dir, "internal", "mcp")
		require.NoError(t, os.MkdirAll(mcpDir, 0o755))

		// tools.go has 1 tool (chosen by default since surface=full)
		toolsBody := `package mcp

import mcplib "github.com/mark3labs/mcp-go/mcp"

func RegisterTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("typed_tool", mcplib.WithDescription("Typed.")), nil)
}
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "tools.go"), []byte(toolsBody), 0o644))

		// code_orch.go exists but main.go default is "full"
		codeOrchBody := `package mcp

func RegisterCodeOrchestrationTools(s *server.MCPServer) {
	s.AddTool(mcplib.NewTool("thin_search", mcplib.WithDescription("S.")), nil)
	s.AddTool(mcplib.NewTool("thin_execute", mcplib.WithDescription("E.")), nil)
}
`
		require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "code_orch.go"), []byte(codeOrchBody), 0o644))

		cmdDir := filepath.Join(dir, "cmd", "demo-pp-mcp")
		require.NoError(t, os.MkdirAll(cmdDir, 0o755))
		mainBody := `package main

import "os"

func main() {
	surface := os.Getenv("DUB_MCP_SURFACE")
	if surface == "" {
		surface = "full"
	}
	_ = surface
}
`
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainBody), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"),
			[]byte(`{"cli_name":"demo-pp-cli"}`), 0o644))

		est := estimateMCPTokens(dir)
		require.Equal(t, 1, est.ToolCount, "full default → tools.go")
		assert.Equal(t, "typed_tool", est.PerTool[0].Name)
	})
}
