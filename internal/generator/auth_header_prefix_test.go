package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAPIKeyAuthHeaderAppliesPrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("api-key-prefix")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		In:      "header",
		Header:  "Authorization",
		Prefix:  "Token",
		EnvVars: []string{"MAKE_API_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "api-key-prefix-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const inlineTest = `package config

import "testing"

func TestAPIKeyAuthHeaderPrefix(t *testing.T) {
	cfg := &Config{MakeApiToken: "secret"}
	if got := cfg.AuthHeader(); got != "Token secret" {
		t.Fatalf("AuthHeader() = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "auth_header_prefix_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config")
}

func TestGeneratedAPIKeyAuthHeaderORCaseAppliesPrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("api-key-prefix-or")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "api_key",
		In:     "header",
		Header: "Authorization",
		Prefix: "Token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "OR_PREFIX_PRIMARY_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR OR_PREFIX_FALLBACK_TOKEN."},
			{Name: "OR_PREFIX_FALLBACK_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR OR_PREFIX_PRIMARY_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "api-key-prefix-or-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const inlineTest = `package config

import "testing"

func TestAPIKeyAuthHeaderORCasePrefix(t *testing.T) {
	cfg := &Config{OrPrefixPrimaryToken: "primary"}
	if got := cfg.AuthHeader(); got != "Token primary" {
		t.Fatalf("AuthHeader() with primary token = %q", got)
	}

	cfg = &Config{OrPrefixFallbackToken: "fallback"}
	if got := cfg.AuthHeader(); got != "Token fallback" {
		t.Fatalf("AuthHeader() with fallback token = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "auth_header_prefix_or_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config")
}

func TestGeneratedOpenAPIAuthValuePrefixReachesAuthHeader(t *testing.T) {
	t.Parallel()

	parsed, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Sentinel Prefix
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiTokenAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-value-prefix: "ApiToken "
paths:
  /agents:
    get:
      operationId: listAgents
      security:
        - ApiTokenAuth: []
      responses: {"200": {description: ok}}
`))
	require.NoError(t, err)
	require.Equal(t, "ApiToken", parsed.Auth.Prefix)

	outputDir := filepath.Join(t.TempDir(), "sentinel-prefix-pp-cli")
	require.NoError(t, New(parsed, outputDir).Generate())
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `ensureAuthScheme("ApiToken"`)
}

func TestGeneratedAPIKeyAuthHeaderPrefixDoesNotDouble(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("api-key-prefix-once")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		In:      "header",
		Header:  "Authorization",
		Prefix:  "ApiToken",
		EnvVars: []string{"SENTINEL_API_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "api-key-prefix-once-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const inlineTest = `package config

import "testing"

func TestAPIKeyAuthHeaderPrefixOnce(t *testing.T) {
	cfg := &Config{SentinelApiToken: "secret"}
	if got := cfg.AuthHeader(); got != "ApiToken secret" {
		t.Fatalf("AuthHeader() = %q", got)
	}
	cfg = &Config{SentinelApiToken: "ApiToken secret"}
	if got := cfg.AuthHeader(); got != "ApiToken secret" {
		t.Fatalf("AuthHeader() doubled prefix = %q", got)
	}
	cfg = &Config{SentinelApiToken: "apitoken secret"}
	if got := cfg.AuthHeader(); got != "apitoken secret" {
		t.Fatalf("AuthHeader() case-insensitive prefix = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "auth_header_prefix_once_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config", "-run", "TestAPIKeyAuthHeaderPrefixOnce", "-count=1")
}

func TestGeneratedAPIKeyAuthHeaderWithoutPrefixKeepsRawToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("api-key-no-prefix")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		In:      "header",
		Header:  "X-API-Key",
		EnvVars: []string{"PLAIN_API_KEY"},
	}

	outputDir := filepath.Join(t.TempDir(), "api-key-no-prefix-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const inlineTest = `package config

import "testing"

func TestAPIKeyAuthHeaderNoPrefix(t *testing.T) {
	cfg := &Config{PlainApiKey: "secret"}
	if got := cfg.AuthHeader(); got != "secret" {
		t.Fatalf("AuthHeader() = %q", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "config", "auth_header_no_prefix_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/config")
}
