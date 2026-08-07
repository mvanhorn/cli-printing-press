package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestHeaderCarriedCookieAuthAppliesFormat(t *testing.T) {
	t.Parallel()

	t.Run("canonical env and stored access token", func(t *testing.T) {
		t.Parallel()

		const hostileFormat = "Token \"quoted\" \\path\n{token}"
		apiSpec := minimalSpec("cookie-format-canonical")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "cookie",
			Header: "Authorization",
			In:     "header",
			Format: hostileFormat,
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "COOKIE_FORMAT_TOKEN",
				Kind:      spec.AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			}},
		}

		outputDir := filepath.Join(t.TempDir(), "cookie-format-canonical-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		runtimeTest := fmt.Sprintf(`package config

import "testing"

func TestCookieHeaderFormatCanonicalEnv(t *testing.T) {
	cfg := &Config{CookieFormatToken: "env-token"}
	if got, want := cfg.AuthHeader(), %q; got != want {
		t.Fatalf("AuthHeader() = %%q, want %%q", got, want)
	}
}

func TestCookieHeaderFormatStoredAccessToken(t *testing.T) {
	cfg := &Config{AccessToken: "stored-token"}
	if got, want := cfg.AuthHeader(), %q; got != want {
		t.Fatalf("AuthHeader() = %%q, want %%q", got, want)
	}
}
`,
			strings.ReplaceAll(hostileFormat, "{token}", "env-token"),
			strings.ReplaceAll(hostileFormat, "{token}", "stored-token"),
		)
		require.NoError(t, os.WriteFile(
			filepath.Join(outputDir, "internal", "config", "cookie_header_format_test.go"),
			[]byte(runtimeTest), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestCookieHeaderFormat")
	})

	t.Run("OR-case env aliases", func(t *testing.T) {
		t.Parallel()

		const hostileFormat = "Token \"quoted\" \\path\n{token}:{access_token}:{format_secondary}:{COOKIE_FORMAT_SECONDARY}"
		apiSpec := minimalSpec("cookie-format-aliases")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "cookie",
			Header: "Authorization",
			In:     "header",
			Format: hostileFormat,
			EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{
				"COOKIE_FORMAT_PRIMARY",
				"COOKIE_FORMAT_SECONDARY",
			}),
		}

		outputDir := filepath.Join(t.TempDir(), "cookie-format-aliases-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())
		configSource := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
		require.Contains(t, authHeaderBody(t, configSource), `if c.CookieFormatSecondary != ""`)

		expected := hostileFormat
		for _, placeholder := range []string{
			"{token}",
			"{access_token}",
			"{format_secondary}",
			"{COOKIE_FORMAT_SECONDARY}",
		} {
			expected = strings.ReplaceAll(expected, placeholder, "secondary-token")
		}
		runtimeTest := fmt.Sprintf(`package config

import "testing"

func TestCookieHeaderFormatORCaseAliases(t *testing.T) {
	cfg := &Config{CookieFormatSecondary: "secondary-token"}
	if got, want := cfg.AuthHeader(), %q; got != want {
		t.Fatalf("AuthHeader() = %%q, want %%q", got, want)
	}
}
`, expected)
		require.NoError(t, os.WriteFile(
			filepath.Join(outputDir, "internal", "config", "cookie_header_format_test.go"),
			[]byte(runtimeTest), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/config", "-run", "^TestCookieHeaderFormat")
	})
}
