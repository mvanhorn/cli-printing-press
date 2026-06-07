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
