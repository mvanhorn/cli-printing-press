package generator

import (
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
		name        string
		specName    string
		mutate      func(*spec.APISpec)
		methodDecl  string
		saveCall    string
		extraImport string
		extraCases  bool
		compile     bool
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
			methodDecl:  "func (c *Config) SaveTokens(clientID, clientSecret, accessToken, refreshToken string, expiry time.Time) error {",
			saveCall:    `cfg.SaveTokens("client-id", "client-secret", "new-access-token", "new-refresh-token", time.Unix(123, 0))`,
			extraImport: `"time"`,
			extraCases:  true,
			compile:     true,
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
			apiSpec := minimalSpec(tt.specName)
			tt.mutate(apiSpec)
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			helper := funcBody(t, configSrc, "func (c *Config) saveCredentialsThenConfig() error {")
			require.Contains(t, helper, "cliutil.WithFileLock(")
			locked := funcBody(t, configSrc, "func (c *Config) saveCredentialsThenConfigLocked(credsPath string) error {")
			require.Contains(t, locked, "snapshotCredentialsFile(")
			require.Contains(t, locked, "restoreCredentialsFile(")
			snapFn := funcBody(t, configSrc, "func snapshotCredentialsFile(path string) (credentialsSnapshot, error) {")
			require.Contains(t, snapFn, "os.Lstat(")
			require.Contains(t, snapFn, "os.ModeSymlink")
			restoreFn := funcBody(t, configSrc, "func restoreCredentialsFile(snap credentialsSnapshot) error {")
			require.Contains(t, restoreFn, "os.Chmod(")
			require.Contains(t, restoreFn, "os.Symlink(")

			method := funcBody(t, configSrc, tt.methodDecl)
			require.Contains(t, method, "return c.saveCredentialsThenConfig()")
			require.NotContains(t, method, "saveCredentialsFirst")
			require.NotContains(t, method, "return c.save()")

			runtimeTest := credentialSaveRollbackRuntimeTest(naming.EnvPrefix(apiSpec.Name), tt.saveCall, tt.extraImport, tt.extraCases)
			require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "save_rollback_test.go"), []byte(runtimeTest), 0o644))
			runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveRollsBackCredentialsWhenConfigWriteFails", "-count=1")
			if tt.compile {
				requireGeneratedCompiles(t, outputDir)
			}
		})
	}
}

func credentialSaveRollbackRuntimeTest(envPrefix, saveCall, extraImport string, extraCases bool) string {
	extraImportLine := ""
	if extraImport != "" {
		extraImportLine = "\n\t" + extraImport
	}
	syncImport := ""
	runtimeImport := ""
	if extraCases {
		syncImport = "\n\t\"sync\""
		runtimeImport = "\n\t\"runtime\""
	}
	src := `package config

import (
	"bytes"
	"os"
	"path/filepath"` + runtimeImport + syncImport + `
	"testing"` + extraImportLine + `
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
		if err := ` + saveCall + `; err == nil {
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
		if err := ` + saveCall + `; err == nil {
			t.Fatal("expected config write failure")
		}

		if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
			data, _ := os.ReadFile(credentialsPath)
			t.Fatalf("credentials.toml should be absent after failed save, stat err = %v contents = %q", err, data)
		}
	})
`

	if extraCases {
		src += `
	t.Run("snapshotErrorDoesNotMutate", func(t *testing.T) {
		dataDir, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		if err := os.MkdirAll(credentialsPath, 0o700); err != nil {
			t.Fatalf("mkdir credentials path as directory: %v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := ` + saveCall + `; err == nil {
			t.Fatal("expected snapshot failure")
		}
		info, err := os.Lstat(credentialsPath)
		if err != nil {
			t.Fatalf("credentials path after snapshot failure: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("snapshot failure replaced credentials path with a file")
		}
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		for _, entry := range entries {
			if entry.Name() == "credentials.toml" && !entry.IsDir() {
				t.Fatal("snapshot failure published a credentials file")
			}
		}
	})

	t.Run("restoresPriorMode", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX credential mode bits")
		}
		dataDir, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		prior := []byte("refresh_token = \"old-refresh-token\"\n")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("mkdir data dir: %v", err)
		}
		if err := os.WriteFile(credentialsPath, prior, 0o400); err != nil {
			t.Fatalf("write prior credentials: %v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := ` + saveCall + `; err == nil {
			t.Fatal("expected config write failure")
		}
		info, err := os.Stat(credentialsPath)
		if err != nil {
			t.Fatalf("stat credentials after rollback: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o400 {
			t.Fatalf("credentials mode = %o, want 0400", got)
		}
		after, err := os.ReadFile(credentialsPath)
		if err != nil {
			t.Fatalf("read credentials after rollback: %v", err)
		}
		if !bytes.Equal(after, prior) {
			t.Fatalf("credentials.toml = %q, want pre-call contents %q", after, prior)
		}
	})

	t.Run("restoresSymlinkLayout", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX symlink credentials layout")
		}
		dataDir, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("mkdir data dir: %v", err)
		}
		target := filepath.Join(dataDir, "real-credentials.toml")
		prior := []byte("refresh_token = \"old-refresh-token\"\n")
		if err := os.WriteFile(target, prior, 0o600); err != nil {
			t.Fatalf("write symlink target: %v", err)
		}
		if err := os.Symlink(target, credentialsPath); err != nil {
			t.Fatalf("symlink credentials: %v", err)
		}

		cfg := &Config{Path: blockedConfigPath(t)}
		if err := ` + saveCall + `; err == nil {
			t.Fatal("expected config write failure")
		}
		info, err := os.Lstat(credentialsPath)
		if err != nil {
			t.Fatalf("lstat credentials after rollback: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("rollback replaced symlink credentials.toml with a regular file")
		}
		gotTarget, err := os.Readlink(credentialsPath)
		if err != nil {
			t.Fatalf("readlink: %v", err)
		}
		if gotTarget != target {
			t.Fatalf("symlink target = %q, want %q", gotTarget, target)
		}
		after, err := os.ReadFile(credentialsPath)
		if err != nil {
			t.Fatalf("read credentials through symlink: %v", err)
		}
		if !bytes.Equal(after, prior) {
			t.Fatalf("symlink target = %q, want pre-call contents %q", after, prior)
		}
	})

	t.Run("lockKeepsSuccessfulSave", func(t *testing.T) {
		dataDir, credentialsPath := isolateRollbackCredentials(t, "` + envPrefix + `_DATA_DIR")
		prior := []byte("refresh_token = \"old-refresh-token\"\n")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("mkdir data dir: %v", err)
		}
		if err := os.WriteFile(credentialsPath, prior, 0o600); err != nil {
			t.Fatalf("write prior credentials: %v", err)
		}
		successPath := filepath.Join(t.TempDir(), "config.toml")
		blocked := blockedConfigPath(t)

		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			cfg := &Config{Path: blocked}
			errCh <- ` + saveCall + `
		}()
		go func() {
			defer wg.Done()
			cfg := &Config{Path: successPath}
			errCh <- ` + saveCall + `
		}()
		wg.Wait()
		close(errCh)
		var sawSuccess, sawFailure bool
		for err := range errCh {
			if err == nil {
				sawSuccess = true
			} else {
				sawFailure = true
			}
		}
		if !sawSuccess || !sawFailure {
			t.Fatalf("want one success and one failure, success=%v failure=%v", sawSuccess, sawFailure)
		}
		after, err := os.ReadFile(credentialsPath)
		if err != nil {
			t.Fatalf("read credentials after concurrent saves: %v", err)
		}
		if bytes.Contains(after, []byte("old-refresh-token")) && !bytes.Contains(after, []byte("new-access-token")) && !bytes.Contains(after, []byte("new-refresh-token")) {
			t.Fatalf("successful save was rolled back to prior bytes:\n%s", after)
		}
	})
`
	}

	src += `}

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
	return src
}
