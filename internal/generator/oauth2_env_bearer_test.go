package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOAuth2AuthCodeHonorsDirectEnvBearer(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-env-bearer")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://accounts.example.com/oauth/authorize",
		TokenURL:         "https://accounts.example.com/oauth/token",
		EnvVars:          []string{"OAUTH_ENV_BEARER_OAUTH_2_0"},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-env-bearer-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	config := string(configSrc)
	require.Contains(t, config, "A directly-held bearer in OAUTH_ENV_BEARER_OAUTH_2_0")

	credTestSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "credentials_test.go"))
	require.NoError(t, err)
	require.Contains(t, string(credTestSrc), "TestCorruptCredentialsFallsBackToEnvCredential")

	// The emitted test exercises the real Load()+AuthHeader() path against the
	// emitted config — the artifact pair that contradicted each other before.
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-run", "TestCorruptCredentials")
}

func TestOAuth2ClientCredentialsOmitsDirectEnvBearerFallback(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-cc-no-bearer")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://accounts.example.com/oauth/token",
		EnvVars:     []string{"OAUTH_CC_NO_BEARER_CLIENT_ID", "OAUTH_CC_NO_BEARER_CLIENT_SECRET"},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-cc-no-bearer-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// client_credentials env vars are mint-flow inputs; neither the
	// direct-bearer fallback nor the env-credential AuthHeader test may be
	// emitted, or a client ID would be sent as Authorization: Bearer.
	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.NotContains(t, string(configSrc), "A directly-held bearer in")

	credTestSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "credentials_test.go"))
	require.NoError(t, err)
	require.NotContains(t, string(credTestSrc), "TestCorruptCredentialsFallsBackToEnvCredential")

	runGoCommand(t, outputDir, "test", "./internal/cliutil")
}
