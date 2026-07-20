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

	conformance := readGeneratedFile(t, outputDir, "internal", "platform", "conformance_test.go")
	require.Contains(t, conformance, "TestResolvedWindowCalendarConformance")
	require.Contains(t, conformance, `assertFailure("metrics", "missing_business_metrics"`)

	windowTest := readGeneratedFile(t, outputDir, "internal", "cli", "platform_window_test.go")
	require.Contains(t, windowTest, "TestPlatformCommandWindowPreservesCalendarInputs")
	require.Contains(t, windowTest, "TestPlatformMCPWindowPreservesCalendarMonthInput")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/platform", "./internal/cli", "./internal/mcp", "-run", "^(TestResolvedWindowCalendarConformance|TestPlatform(Command|MCP)Window)", "-count=1")
}
