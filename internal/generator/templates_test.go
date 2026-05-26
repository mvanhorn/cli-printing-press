package generator

import (
	"go/parser"
	"go/token"
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

func TestAuthStatusHintsUseDescriptionAndNameShapePlaceholders(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-placeholders")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{
				Name:        "FRESHSERVICE_DOMAIN",
				Kind:        spec.AuthEnvVarKindPerCall,
				Required:    true,
				Sensitive:   false,
				Description: "Tenant domain (for example acme.freshservice.com), not your Freshworks org dashboard URL.",
			},
			{Name: "SEC_EDGAR_USER_AGENT", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "DOMINOS_USERNAME", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "GOOGLE_ADS_LOGIN_CUSTOMER_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "SENTRY_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-placeholders-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, `export FRESHSERVICE_DOMAIN=\"acme.freshservice.com\" # Tenant domain (for example acme.freshservice.com), not your Freshworks org dashboard URL.`)
	require.Contains(t, content, `export SEC_EDGAR_USER_AGENT=\"you@example.com (Your Tool Name)\"`)
	require.Contains(t, content, `export DOMINOS_USERNAME=\"your-username\"`)
	require.Contains(t, content, `export GOOGLE_ADS_LOGIN_CUSTOMER_ID=\"your-token-here\"`)
	require.Contains(t, content, `export SENTRY_AUTH_TOKEN=\"your-token-here\"`)
}

// An auth env var description containing a double-quote or backslash must not
// break the generated auth.go: the hint is embedded in a Go string literal, so
// the value has to be neutralized. Regression guard for the #1329 fix.
func TestAuthStatusHintEscapesAdversarialDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-hint-escape")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{
				Name:        "QUOTED_TOKEN",
				Kind:        spec.AuthEnvVarKindPerCall,
				Required:    true,
				Sensitive:   true,
				Description: `Use the "API key" from C:\creds (see docs).`,
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-hint-escape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authPath := filepath.Join(outputDir, "internal", "cli", "auth.go")
	authSrc, err := os.ReadFile(authPath)
	require.NoError(t, err)

	// The generated source must parse — a raw " or \ in the hint would produce
	// a string-literal syntax error.
	_, err = parser.ParseFile(token.NewFileSet(), authPath, authSrc, parser.AllErrors)
	require.NoError(t, err, "generated auth.go with an adversarial hint description must compile")

	content := string(authSrc)
	assert.NotContains(t, content, `"API key"`,
		"raw double-quotes from the description must be neutralized in the generated literal")
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

	require.Regexp(t, `"name":\s+"MCP_AUTH_TOKEN"`, content)
	require.Regexp(t, `"name":\s+"MCP_AUTH_CLIENT_ID"`, content)
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

	require.Regexp(t, `Name:\s+"AGENT_CONTEXT_TOKEN"`, content)
	require.Regexp(t, `Name:\s+"AGENT_CONTEXT_CLIENT_ID"`, content)
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

// TestAuthHeaderComposedAndCookieApplySchemePrefix pins the fix for #1419:
// composed and cookie auth types must apply the spec's HeaderPrefix (defaulting
// to "Bearer ") when emitting AuthHeader(), so env-var and chrome-composed
// token paths don't return raw tokens that the upstream API will reject as
// "Authorization: <raw>" instead of "Authorization: Bearer <raw>".
func TestAuthHeaderComposedAndCookieApplySchemePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authType   string
		envVar     string
		envField   string
		authSource string
	}{
		{
			name:       "composed-single-env-var",
			authType:   "composed",
			envVar:     "SUNO_TOKEN",
			envField:   "SunoToken",
			authSource: "chrome-composed",
		},
		{
			name:       "cookie-single-env-var",
			authType:   "cookie",
			envVar:     "NOTION_TOKEN",
			envField:   "NotionToken",
			authSource: "browser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    tt.authType,
				Header:  "Authorization",
				EnvVars: []string{tt.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(configSrc)

			// The raw-return literal must appear nowhere in the emitted config — every
			// composed/cookie return path now flows through ensureAuthScheme. Whole-file
			// NotContains avoids depending on a fragile slice boundary.
			require.NotContains(t, content, "return c."+tt.envField+"\n",
				"%s auth must not return raw env-var token without scheme prefix:\n%s", tt.authType, content)
			require.NotContains(t, content, "return c.AccessToken\n",
				"%s auth must not return raw AccessToken without scheme prefix:\n%s", tt.authType, content)

			// The replacement ensureAuthScheme call must be the wrapper for both paths.
			require.Contains(t, content,
				`return ensureAuthScheme("Bearer", c.`+tt.envField+")",
				"%s auth env-var path should wrap return in ensureAuthScheme:\n%s", tt.authType, content)
			require.Contains(t, content,
				`return ensureAuthScheme("Bearer", c.AccessToken)`,
				"%s auth AccessToken path should wrap return in ensureAuthScheme:\n%s", tt.authType, content)

			// Confirm AuthSource label preserved (no behavior regression on which branch we took).
			require.Contains(t, content, `c.AuthSource = "`+tt.authSource+`"`)

			// Generated config must compile and pass its own tests.
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}

// TestAuthHeaderSchemeHelperHandlesPreprefixedTokens compiles a composed-auth
// and a cookie-auth CLI and exercises the emitted ensureAuthScheme helper
// through AuthHeader() to confirm the positive (Bearer prefix applied) and
// negative (no double prefix when the user pre-prefixes the env var) cases
// for both auth types. The Bearer prefix here comes from HeaderPrefix()'s
// default (the composed and cookie branches do not consult Auth.Format), so
// the test specs intentionally omit Format.
func TestAuthHeaderSchemeHelperHandlesPreprefixedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		authType string
		envVar   string
		envField string
	}{
		{name: "composed", authType: "composed", envVar: "FOO_TOKEN", envField: "FooToken"},
		{name: "cookie", authType: "cookie", envVar: "BAR_TOKEN", envField: "BarToken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name + "-auth-runtime")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tt.authType,
				Header:  "Authorization",
				EnvVars: []string{tt.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-auth-runtime-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			testFile := filepath.Join(outputDir, "internal", "config", "auth_scheme_runtime_test.go")
			testSrc := `package config

import "testing"

func TestEnsureAuthSchemeAppliesBearerPrefix(t *testing.T) {
	c := &Config{` + tt.envField + `: "eyJxxx"}
	if got := c.AuthHeader(); got != "Bearer eyJxxx" {
		t.Fatalf("expected Bearer-prefixed header, got %q", got)
	}
	if c.AuthSource != "env:` + tt.envVar + `" {
		t.Fatalf("AuthSource = %q, want env:` + tt.envVar + `", c.AuthSource)
	}
}

func TestEnsureAuthSchemeDoesNotDoublePrefix(t *testing.T) {
	c := &Config{` + tt.envField + `: "Bearer eyJxxx"}
	if got := c.AuthHeader(); got != "Bearer eyJxxx" {
		t.Fatalf("expected single Bearer prefix, got %q", got)
	}
}

func TestEnsureAuthSchemeBlankReturnsEmpty(t *testing.T) {
	c := &Config{}
	if got := c.AuthHeader(); got != "" {
		t.Fatalf("expected empty header for blank config, got %q", got)
	}
}
`
			require.NoError(t, os.WriteFile(testFile, []byte(testSrc), 0o644))
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}
