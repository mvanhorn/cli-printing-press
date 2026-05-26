package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestRequiresRoleEmitsEndpointGate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("role-gate")
	apiSpec.Roles = []string{"parent", "student", "teacher", "admin"}
	items := apiSpec.Resources["items"]
	items.Endpoints["get"] = endpointForRoleGate("GET", "/items/{id}", "Get item")
	apiSpec.Resources["items"] = items
	endpoint := apiSpec.Resources["items"].Endpoints["list"]
	endpoint.RequiresRole = "admin"
	apiSpec.Resources["items"].Endpoints["list"] = endpoint

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpersSrc, `PersonaAdmin`)
	require.Contains(t, helpersSrc, `"admin"`)
	require.Contains(t, helpersSrc, "func RequireRoleWith")
	require.Contains(t, helpersSrc, "var resolveAuthRole = func")
	require.Contains(t, helpersSrc, "authenticated account role is unknown")

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "AuthRole")
	require.Contains(t, configSrc, "ROLE_GATE_AUTH_ROLE")
	require.Contains(t, configSrc, `roleFromEnv := false`)
	require.Contains(t, configSrc, `strings.HasPrefix(cfg.AuthSource, "env:")`)

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_list.go")
	require.Contains(t, commandSrc, "config.Load(flags.configPath)")
	require.Contains(t, commandSrc, "RequireRoleWith(flags, cfg, cmd.CommandPath(), PersonaAdmin)")

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, "requiredRole cli.Persona")
	require.Contains(t, mcpSrc, "cli.RequireRoleWith(nil, cfg, pathTemplate, requiredRole)")
	require.Contains(t, mcpSrc, "cli.PersonaAdmin")

	runGoCommand(t, outputDir, "test", "./internal/cli", "./internal/mcp", "./internal/config")
}

func TestRequiresRoleEmitsPromotedEndpointGate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-role-gate")
	apiSpec.Roles = []string{"admin"}
	endpoint := apiSpec.Resources["items"].Endpoints["list"]
	endpoint.RequiresRole = "admin"
	apiSpec.Resources["items"].Endpoints["list"] = endpoint

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, promotedSrc, "config.Load(flags.configPath)")
	require.Contains(t, promotedSrc, "RequireRoleWith(flags, cfg, cmd.CommandPath(), PersonaAdmin)")

	runGoCommand(t, outputDir, "test", "./internal/cli", "./internal/mcp", "./internal/config")
}

func TestRequiresRoleEmitsMCPOrchestrationGates(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-role-gate")
	apiSpec.Roles = []string{"admin"}
	endpoint := apiSpec.Resources["items"].Endpoints["list"]
	endpoint.RequiresRole = "admin"
	apiSpec.Resources["items"].Endpoints["list"] = endpoint
	apiSpec.MCP = spec.MCPConfig{
		Orchestration: "code",
		Intents: []spec.Intent{
			{
				Name:        "list_admin_items",
				Description: "List admin items",
				Steps: []spec.IntentStep{
					{Endpoint: "items.list", Capture: "items"},
				},
				Returns: "items",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	codeOrchSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, codeOrchSrc, "RequiredRole cli.Persona")
	require.Contains(t, codeOrchSrc, "cli.RequireRoleWith(nil, cfg, ep.ID, ep.RequiredRole)")
	require.Contains(t, codeOrchSrc, "RequiredRole: cli.PersonaAdmin")

	intentsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intentsSrc, "requiredRole cli.Persona")
	require.Contains(t, intentsSrc, "cli.RequireRoleWith(nil, cfg, ref, ep.requiredRole)")
	require.Contains(t, intentsSrc, "requiredRole: cli.PersonaAdmin")

	runGoCommand(t, outputDir, "test", "./internal/mcp", "./internal/cli", "./internal/config")
}

func endpointForRoleGate(method, path, description string) spec.Endpoint {
	return spec.Endpoint{Method: method, Path: path, Description: description}
}

func TestRequiresRoleHelpersStayAbsentWithoutAnnotations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-role-gate")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.NotContains(t, helpersSrc, "type Persona")
	require.NotContains(t, helpersSrc, "RequireRoleWith")

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.NotContains(t, configSrc, "AuthRole")
}
