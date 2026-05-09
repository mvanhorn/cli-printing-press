package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorTemplateRendersKindAwareAuthEnvPresence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "RICH_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "RICH_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "RICH_AUTH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
			{Name: "RICH_AUTH_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			{Name: "RICH_AUTH_OPTIONAL_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Optional elevated-read token."},
			{Name: "RICH_AUTH_ALT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR RICH_AUTH_ALT_KEY."},
			{Name: "RICH_AUTH_ALT_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR RICH_AUTH_ALT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "doctor-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorSrc)

	require.Contains(t, content, `report["env_vars"] = "ERROR missing required: " + strings.Join(authEnvRequiredMissing, ", ")`)
	require.Contains(t, content, `authEnvInfo = append(authEnvInfo, "RICH_AUTH_CLIENT_ID set during auth login")`)
	require.Contains(t, content, `authEnvInfo = append(authEnvInfo, "RICH_AUTH_COOKIES set with auth set-token")`)
	require.NotContains(t, content, `RICH_AUTH_COOKIES populated automatically by auth login --chrome`)
	require.Contains(t, content, `report["env_vars"] = "INFO set one of: " + strings.Join(authEnvOptionalNames, " or ")`)
}

func TestAuthStatusHintsOnlyRequestCredentialEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STATUS_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "STATUS_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "STATUS_AUTH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
			{Name: "STATUS_AUTH_SESSION_COOKIE", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			{Name: "STATUS_AUTH_OPTIONAL_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	start := strings.Index(content, `fmt.Fprintln(w, "Set your token:")`)
	require.NotEqual(t, -1, start, "auth status hint block should be emitted:\n%s", content)
	hintBlock := content[start:]
	end := strings.Index(hintBlock, `auth set-token <token>`)
	require.NotEqual(t, -1, end, "auth set-token fallback should terminate status hint block:\n%s", hintBlock)
	hintBlock = hintBlock[:end]

	require.Contains(t, hintBlock, `export STATUS_AUTH_TOKEN=\"your-token-here\"`)
	require.Contains(t, hintBlock, `export STATUS_AUTH_OPTIONAL_TOKEN=\"your-token-here\"`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_CLIENT_ID`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_CLIENT_SECRET`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_SESSION_COOKIE`)
}

func TestMCPContextOmitsHarvestedAuthEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MCP_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "MCP_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "MCP_AUTH_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "mcp-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mcpSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(mcpSrc)

	require.Contains(t, content, `"name": "MCP_AUTH_TOKEN"`)
	require.Contains(t, content, `"name": "MCP_AUTH_CLIENT_ID"`)
	require.NotContains(t, content, "MCP_AUTH_COOKIES")
}

func TestAgentContextOmitsHarvestedAuthEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agent-context-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "AGENT_CONTEXT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "AGENT_CONTEXT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "AGENT_CONTEXT_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "agent-context-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	agentContextSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "agent_context.go"))
	require.NoError(t, err)
	content := string(agentContextSrc)

	require.Contains(t, content, `Name:      "AGENT_CONTEXT_TOKEN"`)
	require.Contains(t, content, `Name:      "AGENT_CONTEXT_CLIENT_ID"`)
	require.NotContains(t, content, "AGENT_CONTEXT_COOKIES")
}

func TestConfigTemplateRendersAuthHeaderORCaseFanOut(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("slack-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "bearer_token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SLACK_BOT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
			{Name: "SLACK_USER_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "slack-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(configSrc)

	require.True(t,
		strings.Contains(content, "if c.SlackBotToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackBotToken"),
		"generated AuthHeader should read SLACK_BOT_TOKEN:\n%s", content)
	require.True(t,
		strings.Contains(content, "if c.SlackUserToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackUserToken"),
		"generated AuthHeader should fall back to SLACK_USER_TOKEN:\n%s", content)
}

func TestAuthHeaderBearerORCaseFallsThroughToAccessToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("slack-auth-token")
	apiSpec.Auth = spec.AuthConfig{
		Type: "bearer_token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SLACK_BOT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
			{Name: "SLACK_USER_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "slack-auth-token-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = outputDir
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = outputDir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(configSrc)

	fanOutIdx := strings.Index(content, `if c.SlackUserToken != ""`)
	accessTokenIdx := strings.Index(content, `if c.AccessToken != ""`)
	require.NotEqual(t, -1, fanOutIdx, "generated AuthHeader should include OR-case fan-out:\n%s", content)
	require.NotEqual(t, -1, accessTokenIdx, "generated AuthHeader should include AccessToken fallback:\n%s", content)
	assert.Less(t, fanOutIdx, accessTokenIdx, "AccessToken fallback should remain reachable after OR fan-out")
	require.NotContains(t, content[fanOutIdx:accessTokenIdx], `return ""`)
}

func TestAuthHeaderTokenEnvVarsDoNotEmitDuplicateMapKeys(t *testing.T) {
	t.Parallel()

	orTokenEnvVars := []spec.AuthEnvVar{
		{Name: "PRIMARY_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SECONDARY_TOKEN."},
		{Name: "SECONDARY_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR PRIMARY_TOKEN."},
	}

	tests := []struct {
		name string
		auth spec.AuthConfig
	}{
		{
			name: "bearer-canonical-token",
			auth: spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"CANONICAL_TOKEN"},
			},
		},
		{
			name: "bearer-or-token",
			auth: spec.AuthConfig{
				Type:        "bearer_token",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				EnvVarSpecs: orTokenEnvVars,
			},
		},
		{
			name: "api-key-or-token",
			auth: spec.AuthConfig{
				Type:        "api_key",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				EnvVarSpecs: orTokenEnvVars,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}
