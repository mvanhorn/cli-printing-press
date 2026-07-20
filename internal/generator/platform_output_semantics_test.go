package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOutputSemanticsPreserveCalendarAndStrictAnalytics(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("output-semantics-contract")
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	metadata := readGeneratedFile(t, outputDir, "internal", "platform", "metadata.go")
	for _, marker := range []string{
		`json:"month,omitempty"`, `json:"complete_periods,omitempty"`, `MaximumDays`,
		`json:"freshness_status"`, `json:"remediation,omitempty"`,
		`reject("missing_client_profile")`, `reject("missing_verified_tenant")`,
		`reject("missing_join_key_declaration")`, `reject("missing_freshness_diagnostics")`,
		`reject("missing_business_metrics")`, `reject("all_business_metrics_zero_or_null")`,
	} {
		require.Contains(t, metadata, marker)
	}
	require.NotContains(t, metadata, "Maximum time.Duration")

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.Contains(t, root, "adoptPlatformCommandWindow(cmd, flags)")

	window := readGeneratedFile(t, outputDir, "internal", "cli", "platform_window.go")
	for _, marker := range []string{
		"func adoptPlatformCommandWindow", "func AdoptMCPOutputSemantics",
		"platform.WindowRequest{Month:", "CompletePeriods:", "policy.MaximumDays = registeredPlatformSource.WindowMaximumDays",
	} {
		require.Contains(t, window, marker)
	}

	mcpTools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpTools, "cli.AdoptMCPOutputSemantics(platformSession, args)")
	require.Contains(t, mcpTools, `mcpToolTextWithPlatform("already exists (no-op)", platformSession)`)
	require.Contains(t, mcpTools, `mcpToolTextWithPlatform("already deleted (no-op)", platformSession)`)

	conformance := readGeneratedFile(t, outputDir, "internal", "platform", "conformance_test.go")
	require.Contains(t, conformance, "TestResolvedWindowCalendarConformance")
	require.Contains(t, conformance, `assertFailure("metrics", "missing_business_metrics"`)

	windowTest := readGeneratedFile(t, outputDir, "internal", "cli", "platform_window_test.go")
	require.Contains(t, windowTest, "TestPlatformCommandWindowPreservesCalendarInputs")
	require.Contains(t, windowTest, "TestPlatformMCPWindowPreservesCalendarMonthInput")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/platform", "./internal/cli", "./internal/mcp", "-run", "^(TestResolvedWindowCalendarConformance|TestPlatform(Command|MCP)Window)", "-count=1")

	codeSpec := minimalSpec("output-semantics-code")
	codeSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}, Orchestration: "code"}
	codeOutputDir := filepath.Join(t.TempDir(), naming.CLI(codeSpec.Name))
	codeGen := New(codeSpec, codeOutputDir)
	codeGen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, codeGen.Generate())
	codeOrch := readGeneratedFile(t, codeOutputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, codeOrch, "cli.AdoptMCPOutputSemantics(platformSession, params)")
	requireGeneratedCompiles(t, codeOutputDir)

	intentSpec := minimalSpec("output-semantics-intent")
	intentSpec.MCP = spec.MCPConfig{
		Transport:     []string{"stdio"},
		Orchestration: "intent",
		Intents: []spec.Intent{{
			Name:        "recent_items",
			Description: "List recent items in one resolved window.",
			Params:      []spec.IntentParam{{Name: "since", Type: "string", Description: "Resolved lookback window."}},
			Steps:       []spec.IntentStep{{Endpoint: "items.list", Capture: "items"}},
			Returns:     "items",
		}},
	}
	intentOutputDir := filepath.Join(t.TempDir(), naming.CLI(intentSpec.Name))
	intentGen := New(intentSpec, intentOutputDir)
	intentGen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, intentGen.Generate())
	intents := readGeneratedFile(t, intentOutputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intents, "cli.AdoptMCPOutputSemantics(platformSession, input)")
	requireGeneratedCompiles(t, intentOutputDir)
}
