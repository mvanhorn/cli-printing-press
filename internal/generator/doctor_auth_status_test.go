package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestDoctorReportsConfigAuthAsEnvVarsSatisfied(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-auth-status")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"DOCTOR_AUTH_STATUS_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "doctor-auth-status-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)

	require.Contains(t, doctor, "authConfigured := false", "doctor should remember when cfg.AuthHeader() satisfied auth")
	require.Contains(t, doctor, "credentials available from", "doctor env-var check should explain config-file credentials")
	require.Contains(t, doctor, `report["env_vars"] = "OK " + strings.Join(authEnvInfo, "; ")`, "config credentials must not degrade env_vars to INFO/WARN")
	require.NotContains(t, doctor, `if os.Getenv("DOCTOR_AUTH_STATUS_TOKEN") != "" {
				authEnvSet++
			}

			if authEnvSet == 0 {`, "legacy EnvVars branch must not report zero env vars when config auth is already valid")
}
