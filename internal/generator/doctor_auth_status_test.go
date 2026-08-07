package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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
		In:         "header",
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
		Type:   "api_key",
		Header: "User-Agent",
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

func TestCookieAuthDiagnosticsUseCredentialPresence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cookie-credential-presence")
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{"COOKIE_CREDENTIAL_PRESENCE_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "cookie-credential-presence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	config := string(configSrc)
	require.Contains(t, config, "func (c *Config) CredentialConfigured() bool")
	require.Contains(t, funcBody(t, config, "func (c *Config) hasCredentialFields() bool {"),
		"if c.CookieCredential() != \"\" {")
	require.Contains(t, funcBody(t, config, "func (c *Config) hasCompleteCredentialFields() bool {"),
		"if c.CookieCredential() != \"\" {")

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.Contains(t, string(authSrc), "credentialConfigured := cfg.CredentialConfigured()")

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), "credentialConfigured, authSource := doctorAuthConfiguredState(cfg)")
	require.Contains(t, string(doctorSrc), "if cfg != nil && cfg.CredentialConfigured()")
	require.Contains(t, string(doctorSrc), "authHeader = cfg.CookieCredential()")

	runtimeTest := `package config

import "testing"

func TestCookieCredentialConfiguredUsesRawCookie(t *testing.T) {
	cfg := &Config{AccessToken: "session_id=abc123"}
	if !cfg.CredentialConfigured() {
		t.Fatal("raw cookie credential should count as configured")
	}

	if !(&Config{AuthHeaderVal: "Cookie: session_id=abc123"}).CredentialConfigured() {
		t.Fatal("explicit auth header should count as configured")
	}

	if (&Config{}).CredentialConfigured() {
		t.Fatal("empty cookie credential should not count as configured")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "cookie_credential_presence_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestCookieCredentialConfiguredUsesRawCookie$", "-count=1")
}

func TestComposedAuthDiagnosticsUseCredentialPresence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("composed-credential-presence")
	apiSpec.Auth = spec.AuthConfig{
		Type:         "composed",
		Header:       "Authorization",
		CookieDomain: ".example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{"COMPOSED_CREDENTIAL_PRESENCE_SESSION"},
	}

	outputDir := filepath.Join(t.TempDir(), "composed-credential-presence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), "func (c *Config) CredentialConfigured() bool")
	require.Contains(t, string(configSrc), `return c.AuthHeader() != "" || c.CookieCredential() != ""`)

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), "credentialConfigured, authSource := doctorAuthConfiguredState(cfg)")
	require.Contains(t, string(doctorSrc), "authHeader = cfg.CookieCredential()")

	runtimeTest := `package config

import "testing"

func TestComposedCredentialConfiguredUsesRawCookie(t *testing.T) {
	if !(&Config{AccessToken: "session_id=abc123"}).CredentialConfigured() {
		t.Fatal("raw composed cookie credential should count as configured")
	}

	if (&Config{}).CredentialConfigured() {
		t.Fatal("empty composed cookie credential should not count as configured")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "composed_credential_presence_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestComposedCredentialConfiguredUsesRawCookie$", "-count=1")

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := append(doctorEnv(t.TempDir(), naming.EnvPrefix(apiSpec.Name)), "COMPOSED_CREDENTIAL_PRESENCE_SESSION=session_id=abc123")
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "configured (browser session)", payload["auth"])

	cmd := exec.Command(binaryPath, "auth", "status")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "auth status output: %s", out)
	require.Contains(t, string(out), "Authenticated")
}

func TestGeneratedCookieDiagnosticsReportConfiguredCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cookie-diagnostics-runtime")
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{"COOKIE_DIAGNOSTICS_RUNTIME_SESSION"},
	}
	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	prefix := naming.EnvPrefix(apiSpec.Name)
	env := append(doctorEnv(t.TempDir(), prefix), "COOKIE_DIAGNOSTICS_RUNTIME_SESSION=session_id=abc123")

	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "configured (browser session)", payload["auth"])

	cmd := exec.Command(binaryPath, "auth", "status")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "auth status output: %s", out)
	require.Contains(t, string(out), "Authenticated")
}

func TestHeaderAuthDiagnosticsKeepAuthHeaderPresence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("header-credential-presence")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"HEADER_CREDENTIAL_PRESENCE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "header-credential-presence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runtimeTest := `package config

import "testing"

func TestHeaderCredentialConfiguredUsesAuthHeader(t *testing.T) {
	cfg := &Config{AuthHeaderVal: "Bearer header-token"}
	if !cfg.CredentialConfigured() {
		t.Fatal("header credential should count as configured")
	}

	if (&Config{}).CredentialConfigured() {
		t.Fatal("empty header credential should not count as configured")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "header_credential_presence_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestHeaderCredentialConfiguredUsesAuthHeader$", "-count=1")
}

func TestDoctorAuthHookSupportsHandCodedAuthAndSurvivesRegeneration(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("custom-doctor-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"CUSTOM_DOCTOR_AUTH_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "custom-doctor-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	customAuth := `package cli

func init() {
	doctorAuthConfiguredHook = func() (bool, string) {
		return true, "custom auth"
	}
}
`
	customAuthPath := filepath.Join(outputDir, "internal", "cli", "custom_auth.go")
	require.NoError(t, os.WriteFile(customAuthPath, []byte(customAuth), 0o644))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	gotCustomAuth, err := os.ReadFile(customAuthPath)
	require.NoError(t, err)
	require.Equal(t, customAuth, string(gotCustomAuth), "regeneration must preserve hand-coded auth")

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)
	require.Contains(t, doctor, "var doctorAuthConfiguredHook func() (bool, string)")
	require.Contains(t, doctor, "func doctorAuthConfiguredState(cfg *config.Config) (bool, string)")
	require.Contains(t, doctor, "configured, authSource := doctorAuthConfiguredState(cfg)")

	runtimeTest := `package cli

import "testing"

func TestDoctorAuthConfiguredHook(t *testing.T) {
	configured, source := doctorAuthConfiguredState(nil)
	if !configured || source != "custom auth" {
		t.Fatalf("doctor auth hook = (%v, %q), want (true, custom auth)", configured, source)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "custom_auth_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestDoctorAuthConfiguredHook$", "-count=1")

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := doctorEnv(t.TempDir(), naming.EnvPrefix(apiSpec.Name))
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"])
	require.Equal(t, "custom auth", payload["auth_source"])
	require.Contains(t, payload["env_vars"], "credentials available from custom auth")
}
