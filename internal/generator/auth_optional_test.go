package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestAuthOptional_DoctorReportsOptional verifies that a spec with
// `auth.optional: true` produces a doctor that emits "optional — not configured"
// (rendered as INFO by the indicator switch) instead of "not configured" (FAIL).
// A Grade-A CLI with a completely healthy doctor for its optional-auth state
// is now possible. Regression guard for #211.
func TestAuthOptional_DoctorReportsOptional(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("opt-auth")
	apiSpec.Auth.Optional = true

	outputDir := filepath.Join(t.TempDir(), "opt-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), `"optional — not configured"`,
		"doctor should emit optional-prefixed status when auth.optional is set")
}

// TestAuthNotOptional_DoctorReportsFailure guards the default branch: when
// auth.optional is unset, doctor emits the plain "not configured" string that
// renders as FAIL. Prevents over-broad application of the optional path.
func TestAuthNotOptional_DoctorReportsFailure(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("req-auth")
	// Optional left at zero-value false.

	outputDir := filepath.Join(t.TempDir(), "req-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), `"not configured"`,
		"doctor emits the standard not-configured message for required auth")
	require.NotContains(t, string(doctorSrc), `"optional — not configured"`,
		"default spec must not emit the optional-prefixed status")
}

// TestAuthOptional_AuthCmdShortUsesProseName verifies the `auth` subcommand's
// help Short description names the API, not a single env var, while preserving
// optional-auth framing.
func TestAuthOptional_AuthCmdShortUsesProseName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("opt-auth-short")
	apiSpec.Auth.Optional = true
	apiSpec.Auth.EnvVars = []string{"OPT_AUTH_KEY"}

	outputDir := filepath.Join(t.TempDir(), "opt-auth-short-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.Contains(t, string(authSrc), `"Manage optional authentication for Opt Auth Short"`,
		"auth parent command Short must name the API and flag optionality")
}

// TestAuthRequired_AuthCmdShortUsesProseName verifies the default (required)
// branch names the API instead of overfitting to one env var.
func TestAuthRequired_AuthCmdShortUsesProseName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("req-auth-short")
	apiSpec.Auth.EnvVars = []string{"REQ_AUTH_KEY"}

	outputDir := filepath.Join(t.TempDir(), "req-auth-short-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.Contains(t, string(authSrc), `"Manage authentication for Req Auth Short"`,
		"required-auth parent command Short names the API without optional framing")
}

// TestAuthOptional_ReadmeFramesAsOptional verifies the README template
// uses "Optional: API Key" + the "all core commands work without setup"
// preamble when auth.optional is true and a narrative auth_narrative is set.
func TestAuthOptional_ReadmeFramesAsOptional(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("opt-auth-readme")
	apiSpec.Auth.Optional = true

	outputDir := filepath.Join(t.TempDir(), "opt-auth-readme-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		AuthNarrative: "Use the OPTAUTH_KEY to unlock bonus features.",
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	body := string(readme)
	require.Contains(t, body, "## Optional: API Key",
		"README must use 'Optional: API Key' heading when auth.optional is set")
	require.Contains(t, body, "All core commands work without setup",
		"README must reassure users that core commands need no setup")
}

// TestAuthNotOptional_ReadmeKeepsAuthenticationHeader guards the default branch:
// when auth.optional is unset, README keeps the plain "## Authentication" heading.
func TestAuthNotOptional_ReadmeKeepsAuthenticationHeader(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("req-auth-readme")
	// Optional left false.

	outputDir := filepath.Join(t.TempDir(), "req-auth-readme-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		AuthNarrative: "Export REQ_AUTH_KEY to access the API.",
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	body := string(readme)
	require.Contains(t, body, "## Authentication",
		"README must keep the Authentication heading for required auth")
	require.NotContains(t, body, "## Optional: API Key",
		"required-auth README must not use the optional framing")
	require.NotContains(t, body, "All core commands work without setup",
		"required-auth README must not claim core commands work without setup")
}

// Sanity check that my spec field round-trips. Not really testing anything
// new after the previous tests; here to make the schema intent explicit.
func TestAuthConfig_Optional_ZeroValue(t *testing.T) {
	a := spec.AuthConfig{}
	require.False(t, a.Optional, "Optional must default to false")
	a.Optional = true
	require.True(t, a.Optional)
}

// TestSetToken_ClearsLegacyAuthHeader verifies the generated set-token handler
// clears cfg.AuthHeaderVal before saving. Without this clear, a pre-existing
// auth_header in config.toml (the common regenerate scenario) shadows the
// newly-saved credential via Config.AuthHeader()'s resolver order, and
// set-token silently has no effect on the active credential.
//
// Verification is a substring match on the generated source: the rendered
// auth.go must contain `cfg.AuthHeaderVal = ""` before the save call. The
// presence of the line is the contract; the exact behavior is exercised
// end-to-end by the generated CLI's own set-token command at runtime.
//
// For api_key auth, the save call is SaveCredential (writes to the env-var-
// derived field that AuthHeader() consults). For other auth types it is
// SaveTokens (writes to AccessToken).
//
// Refs cal-com retro #334 WU-4.
func TestSetToken_ClearsLegacyAuthHeader(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("clear-legacy-header")
	apiSpec.Auth.EnvVars = []string{"CLEAR_LEGACY_HEADER_API_KEY"}

	outputDir := filepath.Join(t.TempDir(), "clear-legacy-header-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	src := string(authSrc)

	// The clear must appear inside the set-token handler (not elsewhere).
	require.Contains(t, src, "func newAuthSetTokenCmd",
		"set-token handler must be emitted for the default auth template")
	require.Contains(t, src, `cfg.AuthHeaderVal = ""`,
		"set-token must clear legacy auth_header before saving; without this, a pre-existing auth_header shadows the new token")

	// Fixture uses Auth.Type=api_key, so the save call should be
	// SaveCredential — writing to the field AuthHeader() actually reads.
	clearIdx := strings.Index(src, `cfg.AuthHeaderVal = ""`)
	saveIdx := strings.Index(src, "cfg.SaveCredential(")
	require.NotEqual(t, -1, clearIdx, "auth_header clear must be present")
	require.NotEqual(t, -1, saveIdx, "SaveCredential call must be present for api_key auth")
	require.Less(t, clearIdx, saveIdx,
		"the auth_header clear must occur BEFORE SaveCredential — otherwise the save persists with the stale auth_header still set")
}

// TestConfigLoad_LabelsConfigFileAuthSource verifies that Config.Load() labels
// AuthSource as "config:<path>" when credentials come from the persisted
// config file rather than env vars. Without this, doctor's auth_source field
// stays blank and a user who saved a token via set-token cannot tell whether
// their persisted credentials are being picked up.
//
// Verification asserts substring presence in the rendered Load() body — the
// config-path label set must appear after the env-var override block and
// guard on a non-empty credential slot.
func TestConfigLoad_LabelsConfigFileAuthSource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("config-source-label")
	apiSpec.Auth.EnvVars = []string{"CONFIG_SOURCE_LABEL_API_KEY"}

	outputDir := filepath.Join(t.TempDir(), "config-source-label-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	src := string(configSrc)

	require.Contains(t, src, `cfg.AuthSource = "config"`,
		"Load() must label config-derived credentials so doctor's auth_source surface isn't blank")
	require.Contains(t, src, `cfg.AuthHeaderVal != "" || cfg.AccessToken != ""`,
		"the config-source label must guard on a populated credential slot — empty configs should not be labeled")

	// The label must apply to env-var-derived persisted fields too, not just
	// the bearer/access-token slots — for api_key auth the env-var field is
	// the *only* slot that matters.
	require.Contains(t, src, `cfg.ConfigSourceLabelApiKey != ""`,
		"the env-var-derived field must also trigger the config-source label")

	// The new logic must run after the env-var overrides — otherwise the env
	// override would set AuthSource = "env:..." which we'd then stomp.
	envOverrideIdx := strings.Index(src, `cfg.AuthSource = "env:CONFIG_SOURCE_LABEL_API_KEY"`)
	configLabelIdx := strings.Index(src, `cfg.AuthSource = "config"`)
	require.NotEqual(t, -1, envOverrideIdx, "env-var override block must be present")
	require.NotEqual(t, -1, configLabelIdx, "config-source label must be present")
	require.Less(t, envOverrideIdx, configLabelIdx,
		"env-var overrides must run before the config-source fallback so env wins over disk")

	// The label must not include cfg.Path — embedding the path leaks the
	// user's home directory through doctor's JSON envelope. config_path is
	// exposed as a separate field for callers that need the location.
	require.NotContains(t, src, `cfg.AuthSource = "config:" + cfg.Path`,
		"the label must be the literal \"config\" — embedding cfg.Path leaks the home directory through doctor JSON")
}

// TestSetToken_BearerTokenUsesSaveTokens verifies the else-branch of the
// auth_simple template's Auth.Type conditional: for non-api_key auth, set-token
// must call SaveTokens(...) (the bearer/access-token slot), not SaveCredential.
// Pinning both branches prevents a regression where the bearer path is
// accidentally rewired to SaveCredential and silently sends saved tokens to a
// slot AuthHeader() doesn't read for that auth type.
func TestSetToken_BearerTokenUsesSaveTokens(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-set-token")
	apiSpec.Auth.Type = "bearer_token"
	apiSpec.Auth.EnvVars = []string{"BEARER_SET_TOKEN_TOKEN"}

	outputDir := filepath.Join(t.TempDir(), "bearer-set-token-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	src := string(authSrc)

	require.Contains(t, src, "func newAuthSetTokenCmd",
		"set-token handler must be emitted for bearer_token auth")
	require.Contains(t, src, `cfg.AuthHeaderVal = ""`,
		"set-token must clear legacy auth_header before saving")
	require.Contains(t, src, "cfg.SaveTokens(",
		"bearer_token auth must use SaveTokens, not SaveCredential")
	require.NotContains(t, src, "cfg.SaveCredential(",
		"SaveCredential is api_key-only; bearer_token must not emit it")
}

// TestConfigLoad_LabelsConfigFileAuthSource_EnvVarSpecsBranch covers the
// EnvVarSpecs branch of the new config-source label block in Load(). The
// existing TestConfigLoad_LabelsConfigFileAuthSource exercises the legacy
// EnvVars branch via minimalSpec; this companion test pins the EnvVarSpecs
// branch so a future template change to the rich-auth path does not silently
// drop the labeling.
func TestConfigLoad_LabelsConfigFileAuthSource_EnvVarSpecsBranch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("envvar-specs-source-label")
	apiSpec.Auth.EnvVars = nil
	apiSpec.Auth.EnvVarSpecs = []spec.AuthEnvVar{
		{
			Name:      "ENVVAR_SPECS_SOURCE_LABEL_API_KEY",
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: true,
		},
	}

	outputDir := filepath.Join(t.TempDir(), "envvar-specs-source-label-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	src := string(configSrc)

	require.Contains(t, src, `cfg.AuthSource = "config"`,
		"EnvVarSpecs branch must emit the config-source label")
	require.Contains(t, src, `cfg.EnvvarSpecsSourceLabelApiKey != ""`,
		"EnvVarSpecs branch must guard on the rich-model env-var-derived field")
}
