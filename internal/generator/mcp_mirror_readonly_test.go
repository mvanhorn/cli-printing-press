package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPMirrorBlocksYesAndHonorsReadOnlySwitch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-mirror-readonly")
	outputDir := filepath.Join(t.TempDir(), "mcp-mirror-readonly-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	shellout := readGenerated(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	require.Contains(t, shellout, `confirmationBypassYesFlag = "yes"`)
	require.Contains(t, shellout, "guardMirroredMCPCall(")
	verifyenv := readGenerated(t, outputDir, "internal", "cliutil", "verifyenv.go")
	require.Contains(t, verifyenv, naming.EnvPrefix(apiSpec.Name)+`_MCP_READ_ONLY`)
	require.Contains(t, verifyenv, "func IsMCPReadOnlyEnv()")

	requireGeneratedCompiles(t, outputDir)
	// The parent process may already have the printed read-only switch on.
	// These tests must still see the switch-off registration set.
	readOnlyEnv := []string{naming.EnvPrefix(apiSpec.Name) + "_MCP_READ_ONLY=1"}
	output, err := runGoCommandOutputWithEnv(t, outputDir, readOnlyEnv, "test", "./internal/mcp/cobratree", "-run", "^Test(CliArgsFromMCP_DropsConfirmationBypassEvenIfUnblocked|LocalYesFlagStaysBlocked|MirroredDestructiveCommandRefusesWithoutYes|MCPReadOnlySwitch|BlockedStructuredArgsOnlyDropsInheritedRootFlags|ToolOptionsHideBlockedRootFlagsButKeepLocalCollisions|WriteSinkFlagsStayOutOfMCPSchemaAndArgv|ShadowedPersistentWriteFlagStaysAvailable|PositionalAlternationPlaceholderSanitizesKey|RegisterAllPreservesTypedToolsAndExposesHandBuiltSearchWithoutTypedEquivalent|RegisterAllDisambiguatesOnlyMirrorOwnedNameCollisions|RegisterAllDescendsThroughCobraHiddenButPrunesMCPHidden)$", "-count=1")
	require.NoError(t, err, output)
}

func TestGenerateMCPExecuteReadOnlyRefusesNonGET(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-exec-readonly")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Description: "List items"},
				"create": {Method: "POST", Path: "/items", Description: "Create an item"},
				"remove": {Method: "DELETE", Path: "/items/{id}", Description: "Delete an item"},
			},
		},
	}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	outputDir := filepath.Join(t.TempDir(), "mcp-exec-readonly-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	codeOrch := readGenerated(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Regexp(t, `Method:\s+"POST"`, codeOrch)
	require.Regexp(t, `Method:\s+"DELETE"`, codeOrch)
	require.Contains(t, codeOrch, "codeOrchBlockedByMCPReadOnly(ep.Method)")
	verifyenv := readGenerated(t, outputDir, "internal", "cliutil", "verifyenv.go")
	require.Contains(t, verifyenv, naming.EnvPrefix(apiSpec.Name)+`_MCP_READ_ONLY`)

	requireGeneratedCompiles(t, outputDir)
	readOnlyEnv := []string{naming.EnvPrefix(apiSpec.Name) + "_MCP_READ_ONLY=1"}
	output, err := runGoCommandOutputWithEnv(t, outputDir, readOnlyEnv, "test", "./internal/mcp", "-run", "^TestCodeOrchExecuteReadOnlyRefusesNonGET$", "-count=1")
	require.NoError(t, err, output)
}
