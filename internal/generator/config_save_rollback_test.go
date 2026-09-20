package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestConfigSaveMethodsRollBackCredentialsWhenConfigWriteFails(t *testing.T) {
	t.Parallel()

	tmpl := readFileString(t, filepath.Join("templates", "config.go.tmpl"))
	require.Equal(t, 4, strings.Count(tmpl, "return c.saveCredentialsThenConfig()"),
		"SaveTokens, SaveCredentials, SaveCredential, and SaveBearerToken must share the two-file commit helper")

	apiSpec := minimalSpec("save-rollback-tokens")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {access_token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	helper := funcBody(t, configSrc, "func (c *Config) saveCredentialsThenConfig() error {")
	require.Contains(t, helper, "snapshotCredentialsFile()")
	require.Contains(t, helper, "restoreCredentialsFile(")
	require.Contains(t, helper, "credentials file")
	require.Contains(t, helper, "was replaced")

	saveTokens := funcBody(t, configSrc, "func (c *Config) SaveTokens(clientID, clientSecret, accessToken, refreshToken string, expiry time.Time) error {")
	require.Contains(t, saveTokens, "return c.saveCredentialsThenConfig()")
	require.NotContains(t, saveTokens, "saveCredentialsFirst")
	require.NotContains(t, saveTokens, "return c.save()")

	runtimeTest := credentialSaveRollbackRuntimeTest(naming.EnvPrefix(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "save_rollback_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveRollsBackCredentialsWhenConfigWriteFails", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func credentialSaveRollbackRuntimeTest(envPrefix string) string {
	return `package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveRollsBackCredentialsWhenConfigWriteFails(t *testing.T) {
	t.Run("restoresPriorBytes", func(t *testing.T) {
		dataDir, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		prior := []byte("refresh_token = \"old-refresh-token\"\naccess_token = \"old-access-token\"\n")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("mkdir data dir: %v", err)
		}
		if err := os.WriteFile(credentialsPath, prior, 0o600); err != nil {
			t.Fatalf("write prior credentials: %v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := cfg.SaveTokens("client-id", "client-secret", "new-access-token", "new-refresh-token", time.Unix(123, 0)); err == nil {
			t.Fatal("expected config write failure")
		}

		after, err := os.ReadFile(credentialsPath)
		if err != nil {
			t.Fatalf("read credentials after failed save: %v", err)
		}
		if !bytes.Equal(after, prior) {
			t.Fatalf("credentials.toml = %q, want pre-call contents %q", after, prior)
		}
	})

	t.Run("removesCreatedFile", func(t *testing.T) {
		_, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
			t.Fatalf("precondition: credentials.toml should be absent, stat err = %v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := cfg.SaveTokens("client-id", "client-secret", "new-access-token", "new-refresh-token", time.Unix(123, 0)); err == nil {
			t.Fatal("expected config write failure")
		}

		if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
			data, _ := os.ReadFile(credentialsPath)
			t.Fatalf("credentials.toml should be absent after failed save, stat err = %v contents = %q", err, data)
		}
	})
}

func isolateRollbackCredentials(t *testing.T, dataDirEnv string) (dataDir, credentialsPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	dataDir = filepath.Join(t.TempDir(), "data")
	t.Setenv(dataDirEnv, dataDir)
	return dataDir, filepath.Join(dataDir, "credentials.toml")
}

func blockedConfigPath(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write config path blocker: %v", err)
	}
	return filepath.Join(blocker, "config.toml")
}
`
}
