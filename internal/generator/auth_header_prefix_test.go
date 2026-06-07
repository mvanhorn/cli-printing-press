package generator

import (
	"os"
	"path/filepath"
	"testing"

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
