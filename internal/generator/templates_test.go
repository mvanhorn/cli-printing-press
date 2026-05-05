package generator

import (
	"os"
	"path/filepath"
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
