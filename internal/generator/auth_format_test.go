package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestApplyAuthFormatPreservesBraceBearingValues(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-format-braces")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"AUTH_FORMAT_BRACES_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-format-braces-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), "authFormatPlaceholderRe",
		"applyAuthFormat must match placeholders on the template, not scan substituted output")
	require.NotContains(t, string(configSrc), `if strings.Contains(format, "{")`)

	testSrc := `package config

import "testing"

func TestApplyAuthFormatJSONBlobValue(t *testing.T) {
	got := applyAuthFormat("Bearer {token}", map[string]string{
		"token": "{\"data\":{\"accessToken\":\"abc\"}}",
	})
	want := "Bearer {\"data\":{\"accessToken\":\"abc\"}}"
	if got != want {
		t.Fatalf("applyAuthFormat() = %q, want %q", got, want)
	}
}

func TestApplyAuthFormatValueContainingPlaceholderName(t *testing.T) {
	got := applyAuthFormat("Bearer {token}", map[string]string{
		"token": "abc{token}def",
	})
	want := "Bearer abc{token}def"
	if got != want {
		t.Fatalf("applyAuthFormat() = %q, want %q", got, want)
	}
}

func TestApplyAuthFormatUnmappedPlaceholder(t *testing.T) {
	got := applyAuthFormat("Bearer {token}", map[string]string{
		"access_token": "abc",
	})
	if got != "" {
		t.Fatalf("applyAuthFormat() = %q, want empty", got)
	}
}

func TestAuthHeaderJSONBlobCredential(t *testing.T) {
	cfg := &Config{AccessToken: "{\"data\":{\"accessToken\":\"abc\"}}"}
	got := cfg.AuthHeader()
	want := "Bearer {\"data\":{\"accessToken\":\"abc\"}}"
	if got != want {
		t.Fatalf("AuthHeader() = %q, want %q", got, want)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "auth_format_runtime_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestApplyAuthFormat|TestAuthHeaderJSONBlob", "-count=1")
}

func TestApplyAuthFormatOmittedWithoutFormat(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-format-omitted")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		Header:  "X-API-Key",
		EnvVars: []string{"AUTH_FORMAT_OMITTED_KEY"},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-format-omitted-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.NotContains(t, string(configSrc), `"regexp"`)
	require.NotContains(t, string(configSrc), "func applyAuthFormat")
}

func TestNovelAuthHeaderHelperUsesConfigLoad(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novel-auth-header")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"NOVEL_AUTH_HEADER_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "novel-auth-header-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpers := string(helpersSrc)
	require.Contains(t, helpers, "func novelAuthHeader(flags *rootFlags) (string, error)")
	require.Contains(t, helpers, "config.Load(flags.configPath)")
	require.Contains(t, helpers, "return cfg.AuthHeader(), nil")
	require.NotContains(t, helpers, `os.Getenv("NOVEL_AUTH_HEADER_TOKEN")`)
}
