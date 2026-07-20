package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPEveryToolUsesExactlyOneFreshTenantGate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-tenant-gate")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, tools, "installFreshTenantGate(s)")

	gate := readGeneratedFile(t, outputDir, "internal", "mcp", "platform_gate.go")
	require.Contains(t, gate, "requireFreshTenantGate")
	require.Contains(t, gate, "platform.ContextWithSession(ctx, session)")
	require.Contains(t, gate, `owner == "child-cli"`)

	gateTest := readGeneratedFile(t, outputDir, "internal", "mcp", "platform_gate_test.go")
	require.Contains(t, gateTest, "TestMCPEveryRegisteredToolHasFreshTenantGate")
	require.Contains(t, gateTest, "TestMCPTypedInvocationUsesSingleFreshTenantGate")

	walker := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "walker.go")
	require.Contains(t, walker, `tool.Meta.AdditionalFields["pp:tenant-gate"] = "child-cli"`)

	platformClient := readGeneratedFile(t, outputDir, "internal", "cli", "platform_client.go")
	require.Contains(t, platformClient, "platform.SessionFromContext(ctx)")
	require.Contains(t, platformClient, "session, err := VerifyMCPInvocation(ctx)")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCP", "-count=1")
}
