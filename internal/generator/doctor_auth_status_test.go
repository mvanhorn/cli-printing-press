package generator

import (
	"os"
	"path/filepath"
	"strings"
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
	require.Contains(t, doctor, `} else if authConfigured {`,
		"per_call+Required env var must defer to authConfigured before flagging missing")
	require.Contains(t, doctor, `authEnvInfo = append(authEnvInfo, "credentials available from "+authSource)`,
		"missing env var should explain that config auth covers it")
	require.Contains(t, doctor, `case len(authEnvInfo) > 0 && authConfigured:`,
		"env_vars switch needs the authConfigured arm to elevate INFO to OK")

	// The error-arm string is generated as a fallback (still present in
	// the file), but it must not fire when only the per_call+Required var
	// is missing — that branch lives behind the authConfigured guard
	// above. Verify the structural order: the missing-required append is
	// inside the inner else, after the authConfigured branch.
	idxConfigured := strings.Index(doctor, `} else if authConfigured {`)
	idxMissing := strings.Index(doctor, `authEnvRequiredMissing = append(authEnvRequiredMissing, "DOCTOR_OAUTH2_ENVSPEC_TOKEN")`)
	require.NotEqual(t, -1, idxConfigured, "authConfigured branch must be emitted for this case")
	require.NotEqual(t, -1, idxMissing, "fallback missing-required append must still exist for the unauthenticated case")
	require.Less(t, idxConfigured, idxMissing,
		"missing-required append must be the trailing else, not run unconditionally")
}
