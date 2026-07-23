package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestHeaderCarriedCookieAuthAppliesFormat(t *testing.T) {
	t.Parallel()

	t.Run("canonical env and stored access token", func(t *testing.T) {
		t.Parallel()

		apiSpec := minimalSpec("cookie-format-canonical")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "cookie",
			Header: "Authorization",
			In:     "header",
			Format: "Token {token}",
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "COOKIE_FORMAT_TOKEN",
				Kind:      spec.AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			}},
		}

		outputDir := filepath.Join(t.TempDir(), "cookie-format-canonical-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		const runtimeTest = `package config

import "testing"

func TestCookieHeaderFormatCanonicalEnv(t *testing.T) {
	cfg := &Config{CookieFormatToken: "env-token"}
	if got, want := cfg.AuthHeader(), "Token env-token"; got != want {
		t.Fatalf("AuthHeader() = %q, want %q", got, want)
	}
}

func TestCookieHeaderFormatStoredAccessToken(t *testing.T) {
	cfg := &Config{AccessToken: "stored-token"}
	if got, want := cfg.AuthHeader(), "Token stored-token"; got != want {
		t.Fatalf("AuthHeader() = %q, want %q", got, want)
	}
}
`
		require.NoError(t, os.WriteFile(
			filepath.Join(outputDir, "internal", "config", "cookie_header_format_test.go"),
			[]byte(runtimeTest), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestCookieHeaderFormat")
	})

	t.Run("OR-case env aliases", func(t *testing.T) {
		t.Parallel()

		apiSpec := minimalSpec("cookie-format-aliases")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "cookie",
			Header: "Authorization",
			In:     "header",
			Format: "Token {token}:{access_token}:{format_secondary}:{COOKIE_FORMAT_SECONDARY}",
			EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{
				"COOKIE_FORMAT_PRIMARY",
				"COOKIE_FORMAT_SECONDARY",
			}),
		}

		outputDir := filepath.Join(t.TempDir(), "cookie-format-aliases-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())
		configSource := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
		require.Contains(t, authHeaderBody(t, configSource), `if c.CookieFormatSecondary != ""`)

		const runtimeTest = `package config

import "testing"

func TestCookieHeaderFormatORCaseAliases(t *testing.T) {
	cfg := &Config{CookieFormatSecondary: "secondary-token"}
	if got, want := cfg.AuthHeader(), "Token secondary-token:secondary-token:secondary-token:secondary-token"; got != want {
		t.Fatalf("AuthHeader() = %q, want %q", got, want)
	}
}
`
		require.NoError(t, os.WriteFile(
			filepath.Join(outputDir, "internal", "config", "cookie_header_format_test.go"),
			[]byte(runtimeTest), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestCookieHeaderFormat")
	})
}
