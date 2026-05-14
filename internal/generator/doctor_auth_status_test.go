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

// TestDoctorPreservesConfiguredUserAgentWhenAuthHeaderIsUserAgent pins the
// fix for the "User-Agent IS the auth credential" case. When the API spec
// declares Auth.Header == "User-Agent" + Auth.In == "header" (e.g. the
// weather.gov userAgent securityScheme), the credential-probe code path
// must keep the user's configured UA on authHeaders["User-Agent"]; the
// hardcoded "<name>-pp-cli" fallback must NOT emit, because it would
// overwrite the operator's identity and make the probe test the wrong UA.
func TestDoctorPreservesConfiguredUserAgentWhenAuthHeaderIsUserAgent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ua-auth-fixture")
	apiSpec.Auth = spec.AuthConfig{
		Type:       "api_key",
		Header:     "User-Agent",
		In:         "header",
		VerifyPath: "/alerts/active",
		EnvVars:    []string{"UA_AUTH_FIXTURE_USER_AGENT"},
	}

	outputDir := filepath.Join(t.TempDir(), "ua-auth-fixture-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)

	// The configured UA must be set on the probe.
	require.Contains(t, doctor, `authHeaders["User-Agent"] = authHeader`,
		"doctor probe must assign the configured UA (via authHeader) to authHeaders[\"User-Agent\"]")

	// The hardcoded fallback must NOT clobber the configured UA.
	require.NotContains(t, doctor, `authHeaders["User-Agent"] = "ua-auth-fixture-pp-cli"`,
		"doctor must not overwrite authHeaders[\"User-Agent\"] with the hardcoded fallback when Auth.Header itself is User-Agent")
}

// TestDoctorEmitsHardcodedUserAgentForBearerAuthSpecs guards the converse:
// when Auth.Header is Authorization (the common bearer case), the
// hardcoded User-Agent fallback must still emit so the probe identifies
// itself. This pins that the UA-preservation fix is scoped to the
// UA-as-auth case and does not regress the default path.
func TestDoctorEmitsHardcodedUserAgentForBearerAuthSpecs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-auth-fixture")
	apiSpec.Auth = spec.AuthConfig{
		Type:       "bearer_token",
		Header:     "Authorization",
		VerifyPath: "/me",
		EnvVars:    []string{"BEARER_AUTH_FIXTURE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-auth-fixture-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)

	require.Contains(t, doctor, `authHeaders["User-Agent"] = "bearer-auth-fixture-pp-cli"`,
		"doctor must continue to emit the hardcoded UA fallback for bearer-auth specs where the API does not use User-Agent itself as the credential")
}

// TestDoctorPreservesConfiguredUserAgentWhenAuthInIsEmpty pins the
// default-Auth.In behaviour: when Auth.In is empty, the doctor template
// treats it as the header-auth path (the query-auth branch is the
// special case). The UA-preservation gate must trip on this default
// too — otherwise a spec that omits Auth.In would silently regress.
func TestDoctorPreservesConfiguredUserAgentWhenAuthInIsEmpty(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ua-auth-empty-in")
	apiSpec.Auth = spec.AuthConfig{
		Type:       "api_key",
		Header:     "User-Agent",
		// Auth.In intentionally left empty to exercise the default.
		VerifyPath: "/alerts/active",
		EnvVars:    []string{"UA_AUTH_EMPTY_IN_USER_AGENT"},
	}

	outputDir := filepath.Join(t.TempDir(), "ua-auth-empty-in-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)

	require.Contains(t, doctor, `authHeaders["User-Agent"] = authHeader`,
		"doctor probe must assign the configured UA when Auth.In is empty (defaults to header)")
	require.NotContains(t, doctor, `authHeaders["User-Agent"] = "ua-auth-empty-in-pp-cli"`,
		"doctor must not emit the hardcoded UA fallback when Auth.Header is User-Agent, even with Auth.In empty")
}
