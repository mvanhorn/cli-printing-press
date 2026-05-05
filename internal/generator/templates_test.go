package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
	require.Contains(t, content, `authEnvInfo = append(authEnvInfo, "RICH_AUTH_COOKIES populated automatically by auth login --chrome")`)
	require.Contains(t, content, `report["env_vars"] = "INFO set one of: " + strings.Join(authEnvOptionalNames, " or ")`)
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

	require.Contains(t, content, "OR-case fan-out")
	require.True(t,
		strings.Contains(content, "if c.SlackBotToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackBotToken"),
		"generated AuthHeader should read SLACK_BOT_TOKEN:\n%s", content)
	require.True(t,
		strings.Contains(content, "if c.SlackUserToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackUserToken"),
		"generated AuthHeader should fall back to SLACK_USER_TOKEN:\n%s", content)
}
