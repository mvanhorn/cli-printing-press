package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthSetTokenScheme(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		auth spec.AuthConfig
		want string
	}{
		{name: "bearer format", auth: spec.AuthConfig{Format: "Bearer {token}"}, want: "Bearer"},
		{name: "custom format", auth: spec.AuthConfig{Format: "Token {access_token}"}, want: "Token"},
		{name: "format without placeholder", auth: spec.AuthConfig{Format: "Bearer"}, want: ""},
		{name: "empty format defaults to bearer", auth: spec.AuthConfig{}, want: "Bearer"},
		{name: "empty format uses prefix", auth: spec.AuthConfig{Prefix: "Token"}, want: "Token"},
		{name: "format wins over prefix", auth: spec.AuthConfig{Format: "Token {token}", Prefix: "Bearer"}, want: "Token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, authSetTokenScheme(tc.auth))
		})
	}
}

func TestBearerRequestCredentialShadowsAccessToken(t *testing.T) {
	t.Parallel()

	authorizeOnly := spec.AuthConfig{
		Type:             "bearer_token",
		Format:           "Bearer {token}",
		AuthorizationURL: "https://auth.example.com/authorize",
		EnvVars:          []string{"COBOT_OAUTH2"},
	}
	require.True(t, bearerRequestCredentialShadowsAccessToken(authorizeOnly, false))
	require.Equal(t, []string{"CobotOauth2"}, shadowingRequestCredentialFields(authorizeOnly))

	withTokenURL := authorizeOnly
	withTokenURL.TokenURL = "https://auth.example.com/token"
	require.False(t, bearerRequestCredentialShadowsAccessToken(withTokenURL, false))
	require.Empty(t, shadowingRequestCredentialFields(withTokenURL))

	require.False(t, bearerRequestCredentialShadowsAccessToken(authorizeOnly, true))
	require.Equal(t, []string{"CobotOauth2"}, shadowingRequestCredentialFields(authorizeOnly))

	plain := spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"PLAIN_TOKEN"}}
	require.False(t, bearerRequestCredentialShadowsAccessToken(plain, false))

	noEnv := authorizeOnly
	noEnv.EnvVars = nil
	require.False(t, bearerRequestCredentialShadowsAccessToken(noEnv, false))

	flowInput := authorizeOnly
	flowInput.EnvVars = nil
	flowInput.EnvVarSpecs = []spec.AuthEnvVar{{
		Name: "COBOT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true,
	}}
	require.False(t, bearerRequestCredentialShadowsAccessToken(flowInput, false))

	builtin := authorizeOnly
	builtin.EnvVars = []string{"COBOT_ACCESS_TOKEN"}
	require.False(t, bearerRequestCredentialShadowsAccessToken(builtin, false))
	require.Empty(t, shadowingRequestCredentialFields(builtin))

	google := authorizeOnly
	google.Subtype = spec.AuthSubtypeGoogleServiceAccount
	require.False(t, bearerRequestCredentialShadowsAccessToken(google, false))

	orCase := authorizeOnly
	orCase.EnvVars = nil
	orCase.EnvVarSpecs = spec.NewORCaseEnvVarSpecs([]string{"COBOT_OAUTH2", "COBOT_ALT"})
	require.True(t, bearerRequestCredentialShadowsAccessToken(orCase, false))
	require.Equal(t, []string{"CobotOauth2", "CobotAlt"}, shadowingRequestCredentialFields(orCase))
}

func TestAuthCodeTemplateSelectionLocksSetToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		auth    spec.AuthConfig
		wantCmd bool
	}{
		{
			name: "oauth2 authorization code",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
			},
			wantCmd: true,
		},
		{
			name: "bearer authorization code with env",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				EnvVars:          []string{"COBOT_OAUTH2"},
			},
			wantCmd: true,
		},
		{
			name: "bearer authorization code without env",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
			},
			wantCmd: true,
		},
		{
			name: "bearer authorize url only with env",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				EnvVars:          []string{"COBOT_OAUTH2"},
			},
			wantCmd: true,
		},
		{
			name: "bearer authorize url only without env",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
			},
			wantCmd: false,
		},
		{
			name: "client credentials keeps its template",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				OAuth2Grant:      spec.OAuth2GrantClientCredentials,
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				EnvVars:          []string{"CC_CLIENT_ID", "CC_CLIENT_SECRET"},
			},
			wantCmd: false,
		},
		{
			name: "device code keeps its template",
			auth: spec.AuthConfig{
				Type:                   "oauth2",
				OAuth2Grant:            spec.OAuth2GrantDeviceCode,
				AuthorizationURL:       "https://auth.example.com/authorize",
				DeviceAuthorizationURL: "https://auth.example.com/device",
				TokenURL:               "https://auth.example.com/token",
			},
			wantCmd: false,
		},
		{
			name: "oauth2 without authorization url",
			auth: spec.AuthConfig{
				Type:    "oauth2",
				EnvVars: []string{"OAUTH2_TOKEN"},
			},
			wantCmd: false,
		},
		{
			name: "none with authorization url",
			auth: spec.AuthConfig{
				Type:             "none",
				AuthorizationURL: "https://auth.example.com/authorize",
			},
			wantCmd: false,
		},
		{
			name:    "plain bearer",
			auth:    spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"PLAIN_TOKEN"}},
			wantCmd: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := minimalSpec("sel-" + strings.ReplaceAll(tc.name, " ", "-"))
			apiSpec.Auth = tc.auth
			gen := New(apiSpec, filepath.Join(t.TempDir(), "out"))
			selected := gen.authTemplateName() == "auth.go.tmpl"
			require.Equal(t, authCodeTemplateSelected(tc.auth), selected)
			require.Equal(t, tc.wantCmd, authCodeSetTokenAvailable(tc.auth))
			if selected {
				require.Equal(t, tc.wantCmd, gen.emitsStdinSecretReader())
			} else {
				require.False(t, authCodeSetTokenAvailable(tc.auth))
			}
		})
	}
}

func TestGeneratedAuthSubcommandsMatchDocsAndHints(t *testing.T) {
	t.Parallel()

	cases := []authSurfaceCase{
		{name: "api-key", auth: spec.AuthConfig{Type: "api_key", Header: "X-API-Key", EnvVars: []string{"SURFACE_API_KEY"}}, wantSetToken: new(true)},
		{
			name: "api-key-authorize-url",
			auth: spec.AuthConfig{
				Type:             "api_key",
				Header:           "X-API-Key",
				AuthorizationURL: "https://auth.example.com/authorize",
				EnvVars:          []string{"SURFACE_API_KEY"},
			},
			wantSetToken: new(true),
		},
		{name: "bearer", auth: spec.AuthConfig{Type: "bearer_token", Header: "Authorization", Format: "Bearer {token}", EnvVars: []string{"SURFACE_TOKEN"}}, wantSetToken: new(true)},
		{
			name: "basic-pair",
			auth: spec.AuthConfig{
				Type:   "api_key",
				Header: "Authorization",
				Format: "Basic {username}:{password}",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "SURFACE_USERNAME", Kind: spec.AuthEnvVarKindPerCall, Required: true},
					{Name: "SURFACE_PASSWORD", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				},
			},
			wantSetToken: new(false),
		},
		{
			name: "bearer-authcode",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				EnvVars:          []string{"COBOT_OAUTH2"},
			},
			wantSetToken: new(true),
		},
		{
			name: "bearer-authcode-no-env",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
			},
			wantSetToken: new(true),
		},
		{
			name: "bearer-authorize-only",
			auth: spec.AuthConfig{
				Type:             "bearer_token",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				EnvVars:          []string{"COBOT_OAUTH2"},
			},
			wantSetToken: new(true),
		},
		{
			name: "oauth2-authcode",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
			},
			wantSetToken: new(true),
		},
		{
			name:         "oauth2-no-url",
			auth:         spec.AuthConfig{Type: "oauth2", EnvVars: []string{"OAUTH2_TOKEN"}},
			wantSetToken: new(false),
			forbid:       []string{"auth login", "auth set-token"},
		},
		{
			name: "client-credentials",
			auth: spec.AuthConfig{
				Type:        "oauth2",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				OAuth2Grant: spec.OAuth2GrantClientCredentials,
				TokenURL:    "https://auth.example.com/token",
				EnvVars:     []string{"CC_CLIENT_ID", "CC_CLIENT_SECRET"},
			},
			wantSetToken: new(true),
			forbid:       []string{"auth setup"},
		},
		{
			name: "device-code",
			auth: spec.AuthConfig{
				Type:                   "oauth2",
				Header:                 "Authorization",
				Format:                 "Bearer {token}",
				OAuth2Grant:            spec.OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "https://auth.example.com/device",
				TokenURL:               "https://auth.example.com/token",
				EnvVars:                []string{"DEVICE_CLIENT_ID"},
			},
			wantSetToken: new(true),
		},
		{
			name: "cookie",
			auth: spec.AuthConfig{
				Type:         "cookie",
				Header:       "Cookie",
				In:           "cookie",
				CookieDomain: "example.com",
				Cookies:      []string{"session"},
				EnvVars:      []string{"COOKIE_SESSION"},
			},
			wantSetToken: new(true),
		},
		{
			name: "composed",
			auth: spec.AuthConfig{
				Type:         "composed",
				Header:       "Authorization",
				Format:       "Bearer {token}",
				CookieDomain: "example.com",
				Cookies:      []string{"session"},
				EnvVars:      []string{"COMPOSED_SESSION"},
			},
			wantSetToken: new(true),
		},
		{
			name: "oauth2-refresh",
			auth: spec.AuthConfig{
				Type:     spec.AuthTypeOAuth2Refresh,
				Header:   "Authorization",
				TokenURL: "https://auth.example.com/token",
				EnvVars:  []string{"REFRESH_CLIENT_ID", "REFRESH_CLIENT_SECRET", "REFRESH_REFRESH_TOKEN"},
			},
			wantSetToken: new(false),
		},
		{
			name: "session-handshake",
			auth: spec.AuthConfig{
				Type:            "session_handshake",
				BootstrapURL:    "https://bootstrap.example.com/",
				SessionTokenURL: "https://auth.example.com/crumb",
				TokenParamName:  "crumb",
				TokenParamIn:    "query",
			},
			wantSetToken: new(false),
		},
		{
			name: "google-service-account",
			auth: spec.AuthConfig{
				Type:        "bearer_token",
				Subtype:     spec.AuthSubtypeGoogleServiceAccount,
				Header:      "Authorization",
				TokenURL:    "https://oauth2.example.com/token",
				EnvVars:     []string{"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_ACCESS_TOKEN"},
				EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_ACCESS_TOKEN"}),
			},
			wantSetToken: new(true),
		},
		{name: "none", auth: spec.AuthConfig{Type: "none"}, wantSetToken: new(false)},
		{
			name:         "none-with-authorize-url",
			auth:         spec.AuthConfig{Type: "none", AuthorizationURL: "https://auth.example.com/authorize"},
			wantSetToken: new(false),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := minimalSpec("surface-" + tc.name)
			apiSpec.Auth = tc.auth
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())
			assertAuthSurface(t, outputDir, tc.wantSetToken, tc.forbid)
			requireGeneratedCompiles(t, outputDir)
		})
	}
}

type authSurfaceCase struct {
	name         string
	auth         spec.AuthConfig
	wantSetToken *bool
	forbid       []string
}

func TestOAuth2BearerSetTokenPersistsWireToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cobot")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:             "bearer_token",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		AuthorizationURL: "https://auth.example.com/oauth/authorize",
		TokenURL:         "https://auth.example.com/oauth/access_token",
		EnvVars:          []string{"COBOT_OAUTH2"},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.Contains(t, authSrc, "cmd.AddCommand(newAuthSetTokenCmd(flags))")
	require.Contains(t, authSrc, "cfg.SaveTokens(cfg.ClientID, cfg.ClientSecret, token, cfg.RefreshToken, time.Time{})")
	require.NotContains(t, authSrc, "ClearShadowingRequestCredentials")
	require.NotContains(t, readGeneratedFile(t, outputDir, "internal", "config", "config.go"), "ClearShadowingRequestCredentials")
	require.NotContains(t, authSrc, `SaveTokens("", "", token, "")`)
	require.NotContains(t, authSrc, "set-token <token>")
	readme := readGeneratedFile(t, outputDir, "README.md")
	require.Contains(t, readme, `echo "$TOKEN" | cobot-pp-cli auth set-token`)
	require.NotContains(t, readme, "auth set-token <token>")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	require.Contains(t, skill, `echo "$TOKEN" | cobot-pp-cli auth set-token`)
	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, `remediationHint := "run auth set-token or auth logout"`)
	require.Contains(t, doctorSrc, `report["auth_hint"] = "cobot-pp-cli login"`)
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go"), "func readSecretFromStdin(")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "oauth_set_token_test.go"), []byte(oauthSetTokenClientTest), 0o644))

	binPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommandRequired(t, outputDir, "test", "-count=1", "-run", "TestPastedAccessTokenBeatsEnvAndSkipsRefresh", "./internal/client")
	runGoCommandRequired(t, outputDir, "build", "-o", binPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	env := cobotCLIEnv(home)
	credPath := cobotCredPath(home)

	help, code := runCLI(t, binPath, env, "", "auth", "--help")
	require.Equal(t, 0, code, help)
	require.Contains(t, help, "set-token")
	setHelp, code := runCLI(t, binPath, env, "", "auth", "set-token", "--help")
	require.Equal(t, 0, code, setHelp)
	require.Contains(t, setHelp, `echo "$TOKEN"`)
	require.NotContains(t, setHelp, "set-token <token>")

	doctorOut, code := runCLI(t, binPath, env, "", "doctor")
	require.Equal(t, 0, code, doctorOut)
	require.Contains(t, doctorOut, "hint:")
	require.Contains(t, doctorOut, "cobot-pp-cli login")
	loginHelp, code := runCLI(t, binPath, env, "", "login", "--help")
	require.Equal(t, 0, code, loginHelp)
	authLoginHelp, code := runCLI(t, binPath, env, "", "auth", "login", "--help")
	require.Equal(t, 0, code, authLoginHelp)

	saved, code := runCLI(t, binPath, env, "  pasted-token\n", "auth", "set-token")
	require.Equal(t, 0, code, saved)
	require.Contains(t, saved, "Token saved")
	body := readCredFile(t, credPath)
	requireAccessToken(t, body, "pasted-token")
	require.NotContains(t, body, "pasted-token\n")
	require.NotRegexp(t, accessTokenPattern(`  pasted-token`), body)
	if runtime.GOOS != "windows" {
		info, err := os.Lstat(credPath)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		require.Zero(t, info.Mode()&os.ModeSymlink)
	}

	payload, err := runDoctorJSON(t, binPath, env)
	require.NoError(t, err)
	require.Contains(t, fmt.Sprint(payload["auth"]), "configured")
	loc := fmt.Sprint(payload["credentials_location"])
	require.NotEmpty(t, loc)
	require.NotEqual(t, "none", loc)
	statusOut, code := runCLI(t, binPath, env, "", "auth", "status", "--json")
	require.Equal(t, 0, code, statusOut)
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(statusOut), &status), statusOut)
	require.Equal(t, true, status["authenticated"])

	prefixed, code := runCLI(t, binPath, env, "Bearer prefixed-token\n", "auth", "set-token")
	require.Equal(t, 0, code, prefixed)
	body = readCredFile(t, credPath)
	requireAccessToken(t, body, "prefixed-token")
	require.NotContains(t, body, "Bearer prefixed-token")
	cased, code := runCLI(t, binPath, env, "bEaReR cased-token\n", "auth", "set-token")
	require.Equal(t, 0, code, cased)
	requireAccessToken(t, readCredFile(t, credPath), "cased-token")

	refused, code := runCLI(t, binPath, env, "", "auth", "set-token")
	require.Equal(t, 4, code, refused)
	require.Contains(t, refused, "empty token on stdin")
	requireAccessToken(t, readCredFile(t, credPath), "cased-token")
	spaces, code := runCLI(t, binPath, env, " \n\t ", "auth", "set-token")
	require.Equal(t, 4, code, spaces)
	require.Contains(t, spaces, "empty token on stdin")
	schemeOnly, code := runCLI(t, binPath, env, "Bearer\n", "auth", "set-token")
	require.Equal(t, 4, code, schemeOnly)
	require.Contains(t, schemeOnly, "empty token on stdin")
	requireAccessToken(t, readCredFile(t, credPath), "cased-token")

	argv, code := runCLI(t, binPath, env, "stdin-should-not-save\n", "auth", "set-token", "argv-secret")
	require.Equal(t, 2, code, argv)
	require.Contains(t, argv, `unknown command "argv-secret"`)
	require.NotContains(t, readCredFile(t, credPath), "argv-secret")
	require.NotContains(t, readCredFile(t, credPath), "stdin-should-not-save")

	refreshHome := t.TempDir()
	refreshCred := cobotCredPath(refreshHome)
	require.NoError(t, os.MkdirAll(filepath.Dir(refreshCred), 0o700))
	require.NoError(t, os.WriteFile(refreshCred, []byte(strings.Join([]string{
		`client_id = "keep-id"`,
		`client_secret = "keep-secret"`,
		`refresh_token = "keep-refresh"`,
		`access_token = "old-token"`,
		`auth_header = "Bearer shadowed-header"`,
		`token_expiry = 2020-01-01T00:00:00Z`,
		"",
	}, "\n")), 0o600))
	refreshed, code := runCLI(t, binPath, cobotCLIEnv(refreshHome), "new-token\n", "auth", "set-token")
	require.Equal(t, 0, code, refreshed)
	refreshBody := readCredFile(t, refreshCred)
	requireAccessToken(t, refreshBody, "new-token")
	requireCredField(t, refreshBody, "refresh_token", "keep-refresh")
	requireCredField(t, refreshBody, "client_id", "keep-id")
	requireCredField(t, refreshBody, "client_secret", "keep-secret")
	require.NotContains(t, refreshBody, "old-token")
	require.NotContains(t, refreshBody, "shadowed-header")
	require.NotContains(t, refreshBody, "auth_header")
	require.NotContains(t, refreshBody, "2020-01-01")
	require.NotContains(t, refreshBody, "token_expiry")

	if runtime.GOOS != "windows" {
		linkHome := t.TempDir()
		linkCred := cobotCredPath(linkHome)
		target := filepath.Join(t.TempDir(), "credential-target")
		require.NoError(t, os.WriteFile(target, []byte("SENTINEL-NOT-TOKEN\n"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Dir(linkCred), 0o700))
		require.NoError(t, os.Symlink(target, linkCred))
		linked, code := runCLI(t, binPath, cobotCLIEnv(linkHome), "symlink-token\n", "auth", "set-token")
		require.Equal(t, 0, code, linked)
		linkInfo, err := os.Lstat(linkCred)
		require.NoError(t, err)
		require.Zero(t, linkInfo.Mode()&os.ModeSymlink)
		targetBody, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, "SENTINEL-NOT-TOKEN\n", string(targetBody))
		requireAccessToken(t, readCredFile(t, linkCred), "symlink-token")
		require.NotContains(t, string(targetBody), "symlink-token")
	}

	envHome := t.TempDir()
	envOut, code := runCLI(t, binPath, cobotCLIEnv(envHome, "COBOT_OAUTH2=env-token"), "stdin-wins\n", "auth", "set-token")
	require.Equal(t, 0, code, envOut)
	envBody := readCredFile(t, cobotCredPath(envHome))
	requireAccessToken(t, envBody, "stdin-wins")
	require.NotRegexp(t, accessTokenPattern("env-token"), envBody)
}

func TestAuthorizeOnlyBearerSetTokenReplacesSavedRequestCredential(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cobotauth")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:             "bearer_token",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		AuthorizationURL: "https://auth.example.com/oauth/authorize",
		EnvVars:          []string{"COBOT_OAUTH2"},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	setToken := generatedSourceBlock(t, authSrc, "func newAuthSetTokenCmd(", "func stripAuthSchemePrefix(")
	clearAt := strings.Index(setToken, "cfg.ClearShadowingRequestCredentials()")
	saveAt := strings.Index(setToken, "cfg.SaveTokens(")
	require.NotEqual(t, -1, clearAt)
	require.Less(t, clearAt, saveAt)
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "func (c *Config) ClearShadowingRequestCredentials()")
	require.Contains(t, configSrc, `c.CobotOauth2 = ""`)
	require.Contains(t, configSrc, `delete(c.envOverrides, "CobotOauth2")`)
	require.Contains(t, configSrc, `c.updateFileConfigField("CobotOauth2")`)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "authorize_only_set_token_test.go"), []byte(authorizeOnlySetTokenClientTest), 0o644))

	binName := naming.CLI(apiSpec.Name)
	binPath := filepath.Join(outputDir, binName)
	runGoCommandRequired(t, outputDir, "build", "-o", "./"+binName, "./cmd/"+binName)

	env := func(home string, extra ...string) []string {
		base := doctorEnv(home, naming.EnvPrefix("cobotauth"))
		base = append(base, "COBOT_OAUTH2=")
		return append(base, extra...)
	}
	replacedHome := t.TempDir()
	replacedCred := seedAuthorizeOnlyCreds(t, replacedHome)
	saved, code := runCLI(t, binPath, env(replacedHome), "new-token\n", "auth", "set-token")
	require.Equal(t, 0, code, saved)
	replacedBody := readCredFile(t, replacedCred)
	requireAccessToken(t, replacedBody, "new-token")
	requireCredField(t, replacedBody, "client_id", "keep-id")
	requireCredField(t, replacedBody, "client_secret", "keep-secret")
	requireCredField(t, replacedBody, "refresh_token", "keep-refresh")
	require.NotContains(t, replacedBody, "old-token")
	require.NotContains(t, replacedBody, "stale-access")
	require.NotContains(t, replacedBody, "shadowed-header")
	require.NotContains(t, replacedBody, "auth_header")
	require.NotContains(t, replacedBody, "token_expiry")

	envSaveHome := t.TempDir()
	envSaveCred := seedAuthorizeOnlyCreds(t, envSaveHome)
	envSaved, code := runCLI(t, binPath, env(envSaveHome, "COBOT_OAUTH2=env-during-save"), "stdin-token\n", "auth", "set-token")
	require.Equal(t, 0, code, envSaved)
	envSaveBody := readCredFile(t, envSaveCred)
	requireAccessToken(t, envSaveBody, "stdin-token")
	require.NotContains(t, envSaveBody, "old-token")
	require.NotContains(t, envSaveBody, "env-during-save")

	out, err := runGoCommandOutputWithEnv(t, outputDir, []string{
		"SET_TOKEN_HOME=" + replacedHome,
		"SET_TOKEN_ENVSAVE_HOME=" + envSaveHome,
	}, "test", "-count=1", "-run", "TestAuthorizeOnlySetTokenSendsReplacement", "./internal/client")
	require.NoError(t, err, out)
}

func seedAuthorizeOnlyCreds(t *testing.T, home string) string {
	t.Helper()
	cred := filepath.Join(home, ".local", "share", naming.CLI("cobotauth"), "credentials.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cred), 0o700))
	require.NoError(t, os.WriteFile(cred, []byte(strings.Join([]string{
		`client_id = "keep-id"`,
		`client_secret = "keep-secret"`,
		`refresh_token = "keep-refresh"`,
		`access_token = "stale-access"`,
		`oauth2 = "old-token"`,
		`auth_header = "Bearer shadowed-header"`,
		`token_expiry = 2020-01-01T00:00:00Z`,
		"",
	}, "\n")), 0o600))
	return cred
}

func TestAPIKeyAuthorizeURLSetTokenPersistsHeaderField(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("keyed")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:             "api_key",
		Header:           "X-API-Key",
		AuthorizationURL: "https://auth.example.com/authorize",
		EnvVars:          []string{"KEYED_API_KEY"},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	setToken := generatedSourceBlock(t, authSrc, "func newAuthSetTokenCmd(", "func stripAuthSchemePrefix(")
	require.Contains(t, setToken, "cfg.SaveCredential(token)")
	require.NotContains(t, setToken, "cfg.SaveTokens(")
	require.NotContains(t, setToken, "ClearShadowingRequestCredentials")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "func (c *Config) SaveCredential(")
	require.NotContains(t, configSrc, "ClearShadowingRequestCredentials")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "api_key_set_token_test.go"), []byte(apiKeySetTokenClientTest), 0o644))
	binPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommandRequired(t, outputDir, "test", "-count=1", "-run", "TestSavedAPIKeyIsTheHeader", "./internal/client")
	runGoCommandRequired(t, outputDir, "build", "-o", binPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	env := append(doctorEnv(home, naming.EnvPrefix("keyed")), "KEYED_API_KEY=")
	saved, code := runCLI(t, binPath, env, "pasted-key\n", "auth", "set-token")
	require.Equal(t, 0, code, saved)
	body := readCredFile(t, filepath.Join(home, ".local", "share", naming.CLI("keyed"), "credentials.toml"))
	require.Regexp(t, `(?m)^api_key = ['"]pasted-key['"]$`, body)
	require.NotRegexp(t, accessTokenPattern("pasted-key"), body)
}

func assertAuthSurface(t *testing.T, outputDir string, wantSetToken *bool, forbid []string) {
	t.Helper()
	authPath := filepath.Join(outputDir, "internal", "cli", "auth.go")
	authSrc := ""
	if data, err := os.ReadFile(authPath); err == nil {
		authSrc = string(data)
	}
	subs := registeredAuthSubs(authSrc)
	if wantSetToken != nil {
		if *wantSetToken {
			require.True(t, subs["set-token"], "expected auth set-token to be registered")
			require.Contains(t, authSrc, `echo \"$TOKEN\" |`)
			require.Contains(t, authSrc, "cobra.NoArgs")
			require.NotContains(t, authSrc, "set-token <token>")
			require.NotContains(t, authSrc, "set-token <jwt>")
		} else {
			require.False(t, subs["set-token"], "auth set-token must not be registered")
			require.NotContains(t, authSrc, "func newAuthSetTokenCmd")
		}
	}
	for _, parts := range [][]string{
		{"README.md"},
		{"SKILL.md"},
		{"AGENTS.md"},
		{"internal", "cli", "doctor.go"},
		{"internal", "cli", "helpers.go"},
		{"internal", "cli", "auth.go"},
		{"internal", "client", "client.go"},
		{"internal", "mcp", "tools.go"},
	} {
		path := filepath.Join(append([]string{outputDir}, parts...)...)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(data)
		rel := strings.Join(parts, "/")
		require.NotContains(t, text, "set-token <token>", rel)
		require.NotContains(t, text, "set-token <jwt>", rel)
		if rel == "README.md" || rel == "SKILL.md" {
			for _, phrase := range forbid {
				require.NotContains(t, text, phrase, rel)
			}
		}
		for _, loc := range authSubMention.FindAllStringSubmatchIndex(text, -1) {
			if !authMentionIsInvocation(text, loc[0]) {
				continue
			}
			word := strings.ToLower(text[loc[2]:loc[3]])
			if !mentionIsAuthCommand(word) {
				continue
			}
			if subs[word] {
				continue
			}
			start := max(loc[0]-60, 0)
			end := min(loc[1]+60, len(text))
			t.Errorf("%s mentions auth %s, which is not a registered subcommand\n%s", rel, word, text[start:end])
		}
		if wantSetToken != nil && !*wantSetToken {
			require.NotContains(t, text, "auth set-token", rel)
		}
		for _, hint := range doctorHintValues(text) {
			assertDoctorHintReal(t, rel, hint, subs)
		}
	}
}

func registeredAuthSubs(authSrc string) map[string]bool {
	subs := map[string]bool{}
	if authSrc == "" {
		return subs
	}
	for _, match := range authUseLine.FindAllStringSubmatch(authSrc, -1) {
		fields := strings.Fields(match[1])
		if len(fields) == 0 || fields[0] == "auth" {
			continue
		}
		subs[fields[0]] = true
	}
	return subs
}

func doctorHintValues(text string) []string {
	var hints []string
	for _, re := range []*regexp.Regexp{authHintAssign, remediationAssign} {
		for _, match := range re.FindAllStringSubmatch(text, -1) {
			hints = append(hints, match[1])
		}
	}
	return hints
}

func assertDoctorHintReal(t *testing.T, rel, hint string, subs map[string]bool) {
	t.Helper()
	for _, match := range authSubMention.FindAllStringSubmatch(hint, -1) {
		word := strings.ToLower(match[1])
		if mentionIsAuthCommand(word) && !subs[word] {
			t.Errorf("%s doctor hint %q names auth %s, which is not registered", rel, hint, word)
		}
	}
	for _, match := range ppCLICommand.FindAllStringSubmatch(hint, -1) {
		word := strings.ToLower(match[1])
		if word == "auth" || !mentionIsAuthCommand(word) {
			continue
		}
		if !subs[word] {
			t.Errorf("%s doctor hint %q names %s, which is not an auth subcommand", rel, hint, word)
		}
	}
}

func authMentionIsInvocation(text string, matchAt int) bool {
	lineStart := 0
	if i := strings.LastIndex(text[:matchAt], "\n"); i >= 0 {
		lineStart = i + 1
	}
	line := text[lineStart:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "//"), strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "/*"), strings.HasPrefix(trimmed, "*"):
		return false
	}
	start := max(matchAt-24, 0)
	return !strings.Contains(strings.ToLower(text[start:matchAt]), "during")
}

func requireAccessToken(t *testing.T, body, token string) {
	t.Helper()
	require.Regexp(t, accessTokenPattern(token), body)
}

func requireCredField(t *testing.T, body, key, value string) {
	t.Helper()
	require.Regexp(t, `(?m)^`+regexp.QuoteMeta(key)+` = ['"]`+regexp.QuoteMeta(value)+`['"]$`, body)
}

func accessTokenPattern(token string) string {
	return `(?m)^access_token = ['"]` + regexp.QuoteMeta(token) + `['"]$`
}

func mentionIsAuthCommand(word string) bool {
	switch word {
	case "login", "logout", "setup", "status", "set-token", "set-credentials", "service-account", "refresh", "whoami":
		return true
	default:
		return strings.Contains(word, "-")
	}
}

var (
	authSubMention    = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_-])auth[ \t]+([a-z][a-z0-9-]*)`)
	authUseLine       = regexp.MustCompile(`Use:\s+"([^"\n]+)"`)
	authHintAssign    = regexp.MustCompile(`report\["auth_hint"\] = "((?:\\.|[^"\\])*)"`)
	remediationAssign = regexp.MustCompile(`remediationHint := "((?:\\.|[^"\\])*)"`)
	ppCLICommand      = regexp.MustCompile(`(?i)-pp-cli(?:\.exe)?\s+([a-z][a-z0-9-]*)`)
)

func cobotCLIEnv(home string, extra ...string) []string {
	env := doctorEnv(home, naming.EnvPrefix("cobot"))
	env = append(env, "COBOT_OAUTH2=")
	return append(env, extra...)
}

func cobotCredPath(home string) string {
	return filepath.Join(home, ".local", "share", naming.CLI("cobot"), "credentials.toml")
}

func readCredFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func runCLI(t *testing.T, bin string, env []string, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	return "", -1
}

const apiKeySetTokenClientTest = `package client

import (
	"path/filepath"
	"testing"

	"keyed-pp-cli/internal/config"
)

func TestSavedAPIKeyIsTheHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localapp"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	for _, suffix := range []string{"HOME", "CONFIG", "CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR"} {
		t.Setenv("KEYED_"+suffix, "")
	}
	t.Setenv("KEYED_API_KEY", "env-key")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "env-key" {
		t.Fatalf("env-only AuthHeader() = %q, want env-key", got)
	}
	if err := cfg.SaveCredential("file-key"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
	withEnv, err := config.Load("")
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if got := withEnv.AuthHeader(); got != "env-key" {
		t.Fatalf("AuthHeader() with env set = %q, want the env key to win", got)
	}
	t.Setenv("KEYED_API_KEY", "")
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("reload without env error = %v", err)
	}
	if got := loaded.AuthHeader(); got != "file-key" {
		t.Fatalf("AuthHeader() = %q, want the saved API key", got)
	}
	if loaded.AccessToken != "" {
		t.Fatalf("AccessToken = %q, want the key stored on its own field", loaded.AccessToken)
	}
}
`

const oauthSetTokenClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cobot-pp-cli/internal/config"
)

func TestPastedAccessTokenBeatsEnvAndSkipsRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localapp"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	for _, suffix := range []string{"HOME", "CONFIG", "CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR"} {
		t.Setenv("COBOT_"+suffix, "")
	}
	t.Setenv("COBOT_OAUTH2", "env-token")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "Bearer env-token" {
		t.Fatalf("env-only AuthHeader() = %q, want Bearer env-token", got)
	}
	if err := cfg.SaveTokens("keep-id", "keep-secret", "file-token", "keep-refresh", time.Time{}); err != nil {
		t.Fatalf("SaveTokens() error = %v", err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if got := loaded.AuthHeader(); got != "Bearer file-token" {
		t.Fatalf("AuthHeader() = %q, want stored access token to win over COBOT_OAUTH2", got)
	}
	if loaded.RefreshToken != "keep-refresh" || loaded.ClientID != "keep-id" || loaded.ClientSecret != "keep-secret" {
		t.Fatalf("refresh inputs = id %q secret %q refresh %q", loaded.ClientID, loaded.ClientSecret, loaded.RefreshToken)
	}
	if !loaded.TokenExpiry.IsZero() {
		t.Fatalf("TokenExpiry = %s, want zero", loaded.TokenExpiry)
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "token endpoint should not run for a zero expiry", http.StatusInternalServerError)
	}))
	defer srv.Close()
	loaded.TokenURL = srv.URL
	c := New(loaded, time.Second, 0)
	c.HTTPClient = srv.Client()
	header, err := c.authHeader(context.Background())
	if err != nil {
		t.Fatalf("authHeader() error = %v", err)
	}
	if header != "Bearer file-token" {
		t.Fatalf("authHeader() = %q", header)
	}
	if hits != 0 {
		t.Fatalf("zero expiry dialed the token endpoint %d times", hits)
	}

	loaded.TokenExpiry = time.Now().Add(-time.Hour)
	hits = 0
	_, err = c.authHeader(context.Background())
	if hits == 0 {
		t.Fatalf("past expiry did not reach the token endpoint (err=%v); zero expiry would not prove the refresh gate", err)
	}
}
`

const authorizeOnlySetTokenClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"cobotauth-pp-cli/internal/config"
)

func TestAuthorizeOnlySetTokenSendsReplacement(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer srv.Close()

	assertSent := func(home, oauthEnv, want string) {
		t.Helper()
		got = nil
		header := requestAuthorization(t, home, srv.URL, oauthEnv)
		if header != want {
			t.Fatalf("Authorization = %q, want %q (seen %q)", header, want, got)
		}
		if len(got) == 0 {
			t.Fatal("request did not reach the server")
		}
		for _, sent := range got {
			if sent != want {
				t.Fatalf("server Authorization = %q, want %q", sent, want)
			}
		}
	}

	assertSent(os.Getenv("SET_TOKEN_HOME"), "", "Bearer new-token")
	assertSent(os.Getenv("SET_TOKEN_HOME"), "env-token", "Bearer env-token")
	assertSent(os.Getenv("SET_TOKEN_ENVSAVE_HOME"), "", "Bearer stdin-token")
}

func requestAuthorization(t *testing.T, home, baseURL, oauthEnv string) string {
	t.Helper()
	if home == "" {
		t.Fatal("seeded home is empty")
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	for _, suffix := range []string{"HOME", "CONFIG", "CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR", "BASE_URL"} {
		t.Setenv("COBOTAUTH_"+suffix, "")
	}
	t.Setenv("COBOTAUTH_BASE_URL", baseURL)
	t.Setenv("COBOT_OAUTH2", oauthEnv)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	return cfg.AuthHeader()
}
`
