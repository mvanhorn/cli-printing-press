package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestConfigSaveMethodsRollBackCredentialsWhenConfigWriteFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		specName     string
		mutate       func(*spec.APISpec)
		methodDecl   string
		saveCall     string
		extraImport  string
		compileWhole bool
	}{
		{
			name:     "SaveTokens",
			specName: "save-rollback-tokens",
			mutate: func(apiSpec *spec.APISpec) {
				apiSpec.Auth = spec.AuthConfig{
					Type:             "oauth2",
					Header:           "Authorization",
					Format:           "Bearer {access_token}",
					OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
					AuthorizationURL: "https://example.com/oauth/authorize",
					TokenURL:         "https://example.com/oauth/token",
				}
			},
			methodDecl:   "func (c *Config) SaveTokens(clientID, clientSecret, accessToken, refreshToken string, expiry time.Time) error {",
			saveCall:     `cfg.SaveTokens("client-id", "client-secret", "new-access-token", "new-refresh-token", time.Unix(123, 0))`,
			extraImport:  `"time"`,
			compileWhole: true,
		},
		{
			name:     "SaveCredential",
			specName: "save-rollback-credential",
			mutate: func(apiSpec *spec.APISpec) {
				apiSpec.Auth = spec.AuthConfig{
					Type:    "api_key",
					Header:  "Authorization",
					Format:  "Bearer {token}",
					EnvVars: []string{"MYAPI_TOKEN"},
				}
			},
			methodDecl: "func (c *Config) SaveCredential(token string) error {",
			saveCall:   `cfg.SaveCredential("new-api-token")`,
		},
		{
			name:     "SaveCredentials",
			specName: "save-rollback-pair",
			mutate: func(apiSpec *spec.APISpec) {
				apiSpec.Auth = spec.AuthConfig{
					Type:    "api_key",
					In:      "header",
					Header:  "Authorization",
					Format:  "Basic {username}:{password}",
					EnvVars: []string{"ROLLBACK_USERNAME", "ROLLBACK_PASSWORD"},
					EnvVarSpecs: []spec.AuthEnvVar{
						{Name: "ROLLBACK_USERNAME", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
						{Name: "ROLLBACK_PASSWORD", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
					},
				}
			},
			methodDecl: "func (c *Config) SaveCredentials(value0, value1 string) error {",
			saveCall:   `cfg.SaveCredentials("new-user", "new-password")`,
		},
		{
			name:     "SaveBearerToken",
			specName: "save-rollback-bearer",
			mutate: func(apiSpec *spec.APISpec) {
				apiSpec.BearerRefresh = spec.BearerRefreshConfig{
					BundleURL: "https://cdn.example.com/main.js",
					Pattern:   `"(AAAAAAAA[^"]+)"`,
				}
				apiSpec.Auth = spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					Format:  "Bearer {token}",
					EnvVars: []string{"SAVE_ROLLBACK_BEARER_TOKEN"},
				}
			},
			methodDecl:  "func (c *Config) SaveBearerToken(accessToken string, refreshedAt time.Time) error {",
			saveCall:    `cfg.SaveBearerToken("new-access-token", time.Unix(123, 0))`,
			extraImport: `"time"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.specName)
			tt.mutate(apiSpec)
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			helper := funcBody(t, configSrc, "func (c *Config) saveCredentialsThenConfig() error {")
			require.Contains(t, helper, "snapshotCredentialsFile()")
			require.Contains(t, helper, "restoreCredentialsFile(")
			require.Contains(t, helper, "credentials file")
			require.Contains(t, helper, "was replaced")

			method := funcBody(t, configSrc, tt.methodDecl)
			require.Contains(t, method, "return c.saveCredentialsThenConfig()")
			require.NotContains(t, method, "saveCredentialsFirst")
			require.NotContains(t, method, "return c.save()")

			runtimeTest := credentialSaveRollbackRuntimeTest(naming.EnvPrefix(apiSpec.Name), tt.saveCall, tt.extraImport)
			require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "save_rollback_test.go"), []byte(runtimeTest), 0o644))
			runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveRollsBackCredentialsWhenConfigWriteFails", "-count=1")
			if tt.compileWhole {
				requireGeneratedCompiles(t, outputDir)
			}
		})
	}
}

func credentialSaveRollbackRuntimeTest(envPrefix, saveCall, extraImport string) string {
	extraImportLine := ""
	if extraImport != "" {
		extraImportLine = "\n\t" + extraImport
	}
	return fmt.Sprintf(`package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"%s
)

func TestSaveRollsBackCredentialsWhenConfigWriteFails(t *testing.T) {
	t.Run("restoresPriorBytes", func(t *testing.T) {
		dataDir, credentialsPath := isolateRollbackCredentials(t, %q)
		prior := []byte("refresh_token = \"old-refresh-token\"\naccess_token = \"old-access-token\"\n")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("mkdir data dir: %%v", err)
		}
		if err := os.WriteFile(credentialsPath, prior, 0o600); err != nil {
			t.Fatalf("write prior credentials: %%v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := %s; err == nil {
			t.Fatal("expected config write failure")
		}

		after, err := os.ReadFile(credentialsPath)
		if err != nil {
			t.Fatalf("read credentials after failed save: %%v", err)
		}
		if !bytes.Equal(after, prior) {
			t.Fatalf("credentials.toml = %%q, want pre-call contents %%q", after, prior)
		}
	})

	t.Run("removesCreatedFile", func(t *testing.T) {
		_, credentialsPath := isolateRollbackCredentials(t, %q)
		if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
			t.Fatalf("precondition: credentials.toml should be absent, stat err = %%v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := %s; err == nil {
			t.Fatal("expected config write failure")
		}

		if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
			data, _ := os.ReadFile(credentialsPath)
			t.Fatalf("credentials.toml should be absent after failed save, stat err = %%v contents = %%q", err, data)
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
		t.Fatalf("write config path blocker: %%v", err)
	}
	return filepath.Join(blocker, "config.toml")
}
`, extraImportLine, envPrefix+"_DATA_DIR", saveCall, envPrefix+"_DATA_DIR", saveCall)
}
