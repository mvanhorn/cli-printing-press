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

// TestDoctorOAuth2PerCallRequiredEnvVarDefersToConfigAuth pins the
// authConfigured short-circuit on the kind-aware EnvVarSpecs path for
// oauth2 specs (issue #879). When a user authenticates via `auth login`,
// AccessToken populates the config and AuthHeader() returns a Bearer; a
// missing per_call+Required env var must surface as "credentials available
// from" and route through the "OK" arm of the env_vars switch, never as
// "ERROR missing required".
func TestDoctorOAuth2PerCallRequiredEnvVarDefersToConfigAuth(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-oauth2-envspec")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "DOCTOR_OAUTH2_ENVSPEC_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "doctor-oauth2-envspec-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)

	require.Contains(t, doctor, "authConfigured := false")
	require.Contains(t, doctor, "authConfigured = true")
	require.Contains(t, doctor, `case len(authEnvInfo) > 0 && authConfigured:`,
		"env_vars switch needs the authConfigured arm to elevate INFO to OK")

	// Pin the full else-if-else chain as a contiguous substring. A weaker
	// "both substrings exist" check would pass even if a refactor flattened
	// authEnvRequiredMissing back to an unconditional append (the exact
	// shape of the original #879 bug). Asserting the contiguous block
	// guarantees the missing-required append is the trailing else, gated
	// by authConfigured.
	require.Contains(t, doctor, `if os.Getenv("DOCTOR_OAUTH2_ENVSPEC_TOKEN") != "" {
				authEnvSet = append(authEnvSet, "DOCTOR_OAUTH2_ENVSPEC_TOKEN")
			} else if authConfigured {
				authSource, _ := report["auth_source"].(string)
				if authSource == "" {
					authSource = "config"
				}
				authEnvInfo = append(authEnvInfo, "credentials available from "+authSource)
			} else {
				authEnvRequiredMissing = append(authEnvRequiredMissing, "DOCTOR_OAUTH2_ENVSPEC_TOKEN")
			}`,
		"per_call+Required env-var check must route missing-required through the authConfigured else chain, not as an unconditional append")
}
