package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars pins that under
// OAuth2 client_credentials the setup inputs are never emitted as bearer
// headers. Only a minted AccessToken is usable for API requests.
func TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CC_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "CC_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
		},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "cc-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_ID") + ` != ""`
	envSecretCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_SECRET") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, tokenCheck, "AuthHeader must check AccessToken")

	body := authHeaderBody(t, content)
	require.Contains(t, body, tokenCheck)
	require.NotContains(t, body, envCheck, "client ID must not be used as a bearer token")
	require.NotContains(t, body, envSecretCheck, "client secret must not be used as a bearer token")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	verifyIdx := strings.Index(clientContent, `cliutil.IsVerifyEnv()`)
	mintIdx := strings.Index(clientContent, `c.mintClientCredentials(ctx, clientID, clientSecret)`)
	require.NotEqual(t, -1, verifyIdx, "mock verification should short-circuit before token minting")
	require.NotEqual(t, -1, mintIdx, "client_credentials mint path should still be emitted")
	assert.Less(t, verifyIdx, mintIdx, "mock verification must not dial the real token endpoint")

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientCredentialsAccessTokenPreservesConfigAuthSource(t *testing.T) {
	t.Setenv("CC_AUTH_TEST_CLIENT_ID", "")
	t.Setenv("CC_AUTH_TEST_CLIENT_SECRET", "")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("access_token = \"disk-access-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthSource != "config" {
		t.Fatalf("AuthSource after Load() = %q, want config", cfg.AuthSource)
	}
	if got := cfg.AuthHeader(); got != "Bearer disk-access-token" {
		t.Fatalf("AuthHeader() = %q, want Bearer disk-access-token", got)
	}
	if cfg.AuthSource != "config" {
		t.Fatalf("AuthSource after AuthHeader() = %q, want config", cfg.AuthSource)
	}
}

func TestClientCredentialsAccessTokenCorrectsEnvFlowInputSource(t *testing.T) {
	t.Setenv("CC_AUTH_TEST_CLIENT_ID", "client-id")
	t.Setenv("CC_AUTH_TEST_CLIENT_SECRET", "client-secret")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("access_token = \"disk-access-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthSource != "env:CC_AUTH_TEST_CLIENT_SECRET" {
		t.Fatalf("AuthSource after Load() = %q, want env:CC_AUTH_TEST_CLIENT_SECRET", cfg.AuthSource)
	}
	if got := cfg.AuthHeader(); got != "Bearer disk-access-token" {
		t.Fatalf("AuthHeader() = %q, want Bearer disk-access-token", got)
	}
	if cfg.AuthSource != "oauth2" {
		t.Fatalf("AuthSource after AuthHeader() = %q, want oauth2", cfg.AuthSource)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "client_credentials_source_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestClientCredentialsAccessToken")
}

func TestConfigSaveTokensDoesNotPersistEnvSourcedCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-env-save")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {access_token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "GOOGLE_ADS_ACCESS_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-env-save-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnvSourcedCredentialsStayOutOfConfigSave(t *testing.T) {
	t.Setenv("GOOGLE_ADS_ACCESS_TOKEN", "env-access-token")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("OAUTH_ENV_SAVE_DATA_DIR", dataDir)
	credentialsPath := filepath.Join(dataDir, "credentials.toml")
	initial := strings.Join([]string{
		"client_id = \"disk-client-id\"",
		"client_secret = \"disk-client-secret\"",
		"access_token = \"disk-access-token\"",
		"refresh_token = \"disk-refresh-token\"",
		"ads_access_token = \"disk-ads-access-token\"",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GoogleAdsAccessToken != "env-access-token" {
		t.Fatalf("GoogleAdsAccessToken after Load() = %q, want env-access-token", cfg.GoogleAdsAccessToken)
	}
	if err := cfg.save(); err != nil {
		t.Fatalf("save() error = %v", err)
	}
	afterSave, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() after save error = %v", err)
	}
	afterSaveText := string(afterSave)
	for _, leaked := range []string{"env-client-id", "env-client-secret", "env-access-token"} {
		if strings.Contains(afterSaveText, leaked) {
			t.Fatalf("config.toml leaked env value %q after save:\n%s", leaked, afterSaveText)
		}
	}
	for _, stale := range []string{"disk-client-id", "disk-client-secret", "disk-access-token", "disk-refresh-token", "disk-ads-access-token"} {
		if strings.Contains(afterSaveText, stale) {
			t.Fatalf("config.toml kept credential value %q after save:\n%s", stale, afterSaveText)
		}
	}

	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := cfg.SaveTokens(cfg.ClientID, cfg.ClientSecret, "refreshed-access-token", "refreshed-refresh-token", expiry); err != nil {
		t.Fatalf("SaveTokens() error = %v", err)
	}
	afterTokens, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.toml) after SaveTokens error = %v", err)
	}
	afterTokensText := string(afterTokens)
	for _, leaked := range []string{"env-client-id", "env-client-secret", "env-access-token"} {
		if strings.Contains(afterTokensText, leaked) {
			t.Fatalf("credentials.toml leaked env value %q after SaveTokens:\n%s", leaked, afterTokensText)
		}
	}
	for _, want := range []string{"refreshed-access-token", "refreshed-refresh-token"} {
		if !strings.Contains(afterTokensText, want) {
			t.Fatalf("credentials.toml missing %q after SaveTokens:\n%s", want, afterTokensText)
		}
	}
	for _, stale := range []string{"disk-access-token", "disk-refresh-token", "disk-ads-access-token"} {
		if strings.Contains(afterTokensText, stale) {
			t.Fatalf("credentials.toml kept stale value %q after SaveTokens:\n%s", stale, afterTokensText)
		}
	}
}

func TestConfigSaveRoundTripWithoutEnvIsStable(t *testing.T) {
	t.Setenv("CLIENT_ID", "")
	t.Setenv("CLIENT_SECRET", "")
	t.Setenv("GOOGLE_ADS_ACCESS_TOKEN", "")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	expiry := time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC)
	original := &Config{
		Path:         configPath,
		ClientID:     "disk-client-id",
		ClientSecret: "disk-client-secret",
		AccessToken:  "disk-access-token",
		RefreshToken: "disk-refresh-token",
		TokenExpiry:  expiry,
		GoogleAdsAccessToken: "disk-ads-access-token",
	}
	if err := original.save(); err != nil {
		t.Fatalf("initial save() error = %v", err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() before Load error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.save(); err != nil {
		t.Fatalf("second save() error = %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() after save error = %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed after no-env load+save\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "env_save_tokens_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestEnvSourcedCredentialsStayOutOfConfigSave|TestConfigSaveRoundTripWithoutEnvIsStable")
}

func TestAuthorizationCodeBearerUsesRefreshedAccessTokenOverStalePerCallToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-user-context-refresh")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "bearer_token",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "OAUTH_USER_CONTEXT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-user-context-refresh-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))
	accessIdx := strings.Index(body, `if c.AccessToken != ""`)
	perCallIdx := strings.Index(body, `if c.OauthUserContextToken != ""`)
	require.NotEqual(t, -1, accessIdx, "authorization_code bearer auth must consult OAuth access tokens")
	assert.Equal(t, -1, perCallIdx, "authorization_code bearer auth must not send stale per-call fields instead of refreshed OAuth access tokens")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.Contains(t, string(clientSrc), `"grant_type":    {"refresh_token"}`, "client must emit refresh-token exchange")

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"oauth-user-context-refresh-pp-cli/internal/cliutil"
)

func TestRefreshedAccessTokenWinsOverStalePerCallToken(t *testing.T) {
	// CI and parallel generator tests can carry API-shaped environment variables
	// for unrelated jobs. This test is about disk-persisted OAuth refresh
	// precedence, so run the generated config package with a sterile environment
	// before loading config from disk.
	oldEnv := os.Environ()
	os.Clearenv()
	if err := os.Setenv("HOME", t.TempDir()); err != nil {
		t.Fatalf("Setenv(HOME) error = %v", err)
	}
	defer func() {
		os.Clearenv()
		for _, kv := range oldEnv {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				_ = os.Setenv(parts[0], parts[1])
			}
		}
	}()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	initial := strings.Join([]string{
		"oauth_user_context_token = \"stale-user-context-token\"",
		"access_token = \"old-access-token\"",
		"refresh_token = \"old-refresh-token\"",
		"client_id = \"client-id\"",
		"client_secret = \"client-secret\"",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "Bearer old-access-token" {
		t.Fatalf("AuthHeader() before refresh = %q, want Bearer old-access-token", got)
	}
	expiry := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := cfg.SaveTokens(cfg.ClientID, cfg.ClientSecret, "new-access-token", "new-refresh-token", expiry); err != nil {
		t.Fatalf("SaveTokens() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "Bearer new-access-token" {
		t.Fatalf("AuthHeader() after refresh = %q, want Bearer new-access-token", got)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if text := string(after); strings.Contains(text, "new-refresh-token") || strings.Contains(text, "new-access-token") {
		t.Fatalf("config file should not retain rotated OAuth secrets after credentials split:\n%s", text)
	}
	creds, ok, err := cliutil.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCredentials() ok = false, want true")
	}
	if creds.RefreshToken != "new-refresh-token" {
		t.Fatalf("credentials refresh token = %q, want new-refresh-token", creds.RefreshToken)
	}
	if creds.AccessToken != "new-access-token" {
		t.Fatalf("credentials access token = %q, want new-access-token", creds.AccessToken)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "oauth_refresh_precedence_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestRefreshedAccessTokenWinsOverStalePerCallToken")
}

func TestConfigSaveBearerTokenPersistsBuiltinEnvCollisionWrite(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-refresh-collision")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {access_token}",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "REFRESH_ACCESS_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}
	apiSpec.BearerRefresh = spec.BearerRefreshConfig{
		BundleURL: "https://cdn.example.com/main.js",
		Pattern:   `"(AAAAAAAA[^"]+)"`,
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-refresh-collision-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveBearerTokenPersistsOverBuiltinEnvOverride(t *testing.T) {
	t.Setenv("REFRESH_ACCESS_TOKEN", "env-access-token")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("BEARER_REFRESH_COLLISION_DATA_DIR", dataDir)
	credentialsPath := filepath.Join(dataDir, "credentials.toml")
	if err := os.WriteFile(configPath, []byte("access_token = \"disk-access-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AccessToken != "env-access-token" {
		t.Fatalf("AccessToken after Load() = %q, want env-access-token", cfg.AccessToken)
	}

	refreshedAt := time.Date(2032, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := cfg.SaveBearerToken("refreshed-access-token", refreshedAt); err != nil {
		t.Fatalf("SaveBearerToken() error = %v", err)
	}
	after, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.toml) error = %v", err)
	}
	text := string(after)
	if !strings.Contains(text, "refreshed-access-token") {
		t.Fatalf("credentials.toml missing refreshed token:\n%s", text)
	}
	for _, stale := range []string{"disk-access-token", "env-access-token"} {
		if strings.Contains(text, stale) {
			t.Fatalf("credentials.toml kept stale token %q:\n%s", stale, text)
		}
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error = %v", err)
	}
	if strings.Contains(string(configAfter), "refreshed-access-token") {
		t.Fatalf("config.toml kept relocated refreshed token:\n%s", string(configAfter))
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "bearer_builtin_collision_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveBearerTokenPersistsOverBuiltinEnvOverride")
}

// TestConfigSaveBearerTokenClearsNonBuiltinEnvCollision covers the #2720 review
// gap: SaveBearerToken zeroed non-builtin custom env-var fields in memory but
// did not delete their envOverrides entries, so configForSave restored the stale
// on-disk value and the credential field was never cleared from config.toml on
// refresh. Also exercises the fileConfig map-isolation (deep-copy) fix.
func TestConfigSaveBearerTokenClearsNonBuiltinEnvCollision(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-refresh-custom-env")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {access_token}",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "API_TENANT", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}
	apiSpec.BearerRefresh = spec.BearerRefreshConfig{
		BundleURL: "https://cdn.example.com/main.js",
		Pattern:   `"(AAAAAAAA[^"]+)"`,
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-refresh-custom-env-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveBearerTokenClearsNonBuiltinEnvOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")

	// Seed a stale on-disk value for the non-builtin custom credential field,
	// with no env override in effect.
	seed, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	seed.ApiTenant = "disk-tenant"
	if err := seed.save(); err != nil {
		t.Fatalf("seed save() error = %v", err)
	}

	// Now the env var overrides the disk value.
	t.Setenv("API_TENANT", "env-tenant")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ApiTenant != "env-tenant" {
		t.Fatalf("ApiTenant after Load() = %q, want env-tenant", cfg.ApiTenant)
	}

	refreshedAt := time.Date(2032, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := cfg.SaveBearerToken("refreshed-access-token", refreshedAt); err != nil {
		t.Fatalf("SaveBearerToken() error = %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(after)
	// On refresh the custom credential field must be cleared: neither the env
	// value nor the stale on-disk value may survive. (Before the fix the
	// lingering envOverride entry made configForSave write the stale disk value.)
	for _, stale := range []string{"disk-tenant", "env-tenant"} {
		if strings.Contains(text, stale) {
			t.Fatalf("config.toml kept stale custom credential %q:\n%s", stale, text)
		}
	}
}

func TestSnapshotFileConfigIsolatesHeaderMap(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	cfg.snapshotFileConfig()
	// Mutating the live Headers map after snapshotting must not leak into the
	// fileConfig snapshot (reference-type isolation, #2720 P2).
	cfg.Headers["X-Isolation-Probe"] = "mutated"
	if cfg.fileConfig != nil {
		if _, leaked := cfg.fileConfig.Headers["X-Isolation-Probe"]; leaked {
			t.Fatalf("snapshot fileConfig shares the Headers map with the live config")
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "bearer_custom_collision_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveBearerTokenClearsNonBuiltinEnvOverride|TestSnapshotFileConfigIsolatesHeaderMap")
}

// TestConfigSaveCredentialClearsBuiltinEnvCollision covers the #2720 follow-up
// Greptile P1: SaveCredential zeroed AuthHeaderVal/AccessToken but only deleted
// the canonical env-var's override. A NON-canonical env var that collides with
// the access_token builtin tag (resolving to the AccessToken field) left its
// override active, so configForSave restored the stale on-disk value instead of
// the cleared "". SaveCredential is the one credential-write method in the family
// that previously had no builtin-collision test.
func TestConfigSaveCredentialClearsBuiltinEnvCollision(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("apikey-builtin-collision")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "api_key",
		Header: "X-API-Key",
		EnvVarSpecs: []spec.AuthEnvVar{
			// Canonical request credential -> non-builtin custom field.
			{Name: "SVC_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			// Non-canonical, collides with the access_token builtin tag -> AccessToken.
			{Name: "SVC_ACCESS_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "apikey-builtin-collision-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCredentialClearsBuiltinEnvOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("access_token = \"disk-access-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// SVC_ACCESS_TOKEN collides with the access_token builtin tag, so it resolves
	// to the AccessToken field and marks an override on Load.
	t.Setenv("SVC_ACCESS_TOKEN", "env-access-token")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AccessToken != "env-access-token" {
		t.Fatalf("AccessToken after Load() = %q, want env-access-token", cfg.AccessToken)
	}

	if err := cfg.SaveCredential("new-key"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(after)
	// SaveCredential clears the alternate AccessToken slot; without deleting its
	// env-override entry, configForSave would restore the stale on-disk value.
	for _, stale := range []string{"disk-access-token", "env-access-token"} {
		if strings.Contains(text, stale) {
			t.Fatalf("config.toml kept stale AccessToken %q after SaveCredential:\n%s", stale, text)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "save_credential_collision_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveCredentialClearsBuiltinEnvOverride")
}

func TestConfigClearTokensClearsBuiltinEnvCollisionCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("clear-builtin-collision")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {access_token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CLEAR_ACCESS_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
			{Name: "CLEAR_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "clear-builtin-collision-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearTokensClearsBuiltinEnvOverridesOnDisk(t *testing.T) {
	t.Setenv("CLEAR_ACCESS_TOKEN", "env-access-token")
	t.Setenv("CLEAR_CLIENT_ID", "env-client-id")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("CLEAR_BUILTIN_COLLISION_DATA_DIR", dataDir)
	credentialsPath := filepath.Join(dataDir, "credentials.toml")
	initial := strings.Join([]string{
		"client_id = \"disk-client-id\"",
		"client_secret = \"disk-client-secret\"",
		"access_token = \"disk-access-token\"",
		"refresh_token = \"disk-refresh-token\"",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AccessToken != "env-access-token" {
		t.Fatalf("AccessToken after Load() = %q, want env-access-token", cfg.AccessToken)
	}
	if cfg.ClientID != "env-client-id" {
		t.Fatalf("ClientID after Load() = %q, want env-client-id", cfg.ClientID)
	}

	if err := cfg.ClearTokens(); err != nil {
		t.Fatalf("ClearTokens() error = %v", err)
	}
	if _, err := os.Stat(credentialsPath); !os.IsNotExist(err) {
		t.Fatalf("credentials.toml should be absent after ClearTokens, stat err=%v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(after)
	for _, stale := range []string{"disk-client-id", "disk-client-secret", "disk-access-token", "disk-refresh-token", "env-client-id", "env-access-token"} {
		if strings.Contains(text, stale) {
			t.Fatalf("config.toml kept stale credential %q:\n%s", stale, text)
		}
	}
	for _, field := range []string{"client_id", "client_secret", "access_token", "refresh_token"} {
		if strings.Contains(text, field) {
			t.Fatalf("config.toml kept credential field %q after ClearTokens:\n%s", field, text)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "clear_builtin_collision_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestClearTokensClearsBuiltinEnvOverridesOnDisk")
}

func TestConfigSaveTokensPersistsClientIDBuiltinEnvCollisionWrite(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-client-id-collision")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {access_token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SAVE_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-client-id-collision-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveTokensPersistsClientIDOverBuiltinEnvOverride(t *testing.T) {
	t.Setenv("SAVE_CLIENT_ID", "env-client-id")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("OAUTH_CLIENT_ID_COLLISION_DATA_DIR", dataDir)
	credentialsPath := filepath.Join(dataDir, "credentials.toml")
	initial := strings.Join([]string{
		"client_id = \"disk-client-id\"",
		"client_secret = \"disk-client-secret\"",
		"access_token = \"disk-access-token\"",
		"refresh_token = \"disk-refresh-token\"",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ClientID != "env-client-id" {
		t.Fatalf("ClientID after Load() = %q, want env-client-id", cfg.ClientID)
	}

	expiry := time.Date(2033, 4, 5, 6, 7, 8, 0, time.UTC)
	if err := cfg.SaveTokens("new-client-id", cfg.ClientSecret, "new-access-token", "new-refresh-token", expiry); err != nil {
		t.Fatalf("SaveTokens() error = %v", err)
	}
	after, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.toml) error = %v", err)
	}
	text := string(after)
	for _, want := range []string{"new-client-id", "disk-client-secret", "new-access-token", "new-refresh-token"} {
		if !strings.Contains(text, want) {
			t.Fatalf("credentials.toml missing %q:\n%s", want, text)
		}
	}
	for _, stale := range []string{"disk-client-id", "env-client-id", "disk-access-token", "disk-refresh-token"} {
		if strings.Contains(text, stale) {
			t.Fatalf("credentials.toml kept stale credential %q:\n%s", stale, text)
		}
	}

	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error = %v", err)
	}
	for _, credential := range []string{"new-client-id", "new-access-token", "new-refresh-token"} {
		if strings.Contains(string(configAfter), credential) {
			t.Fatalf("config.toml kept relocated credential %q:\n%s", credential, string(configAfter))
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "save_tokens_client_id_collision_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestSaveTokensPersistsClientIDOverBuiltinEnvOverride")
}

func TestClientCredentialsEnvVarsSkipTenantSetupInput(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("entra-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "ENTRA_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "ENTRA_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "ENTRA_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "entra-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = strings.TrimSpace(os.Getenv("ENTRA_CC_CLIENT_ID"))`)
	require.Contains(t, authContent, `clientSecret = strings.TrimSpace(os.Getenv("ENTRA_CC_CLIENT_SECRET"))`)
	require.NotContains(t, authContent, `clientID = os.Getenv("ENTRA_CC_TENANT_ID")`)
	require.Contains(t, authContent, `resolveClientCredentialsTokenURL(tokenURL, cfg.`+resolveEnvVarField("ENTRA_CC_TENANT_ID")+`)`)
	require.Contains(t, authContent, `os.Getenv("ENTRA_CC_OAUTH_SCOPE")`)
	require.Contains(t, authContent, `strings.ReplaceAll("api://{client_id}/.default", "{client_id}", clientID)`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("ENTRA_CC_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("ENTRA_CC_CLIENT_SECRET")`)
	require.NotContains(t, clientContent, `id = os.Getenv("ENTRA_CC_TENANT_ID")`)
	require.Contains(t, clientContent, `tenant = c.Config.`+resolveEnvVarField("ENTRA_CC_TENANT_ID"))
	require.Contains(t, clientContent, `resolveClientCredentialsTokenURL(tokenURL, tenant)`)
	require.Contains(t, clientContent, `form.Set("scope", scope)`)

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"entra-cc-pp-cli/internal/config"
)

func TestClientCredentialsRuntimeEntraTenantAndScope(t *testing.T) {
	var gotPath string
	var gotScope string
	var gotClientID string
	var gotClientSecret string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var ok bool
		gotClientID, gotClientSecret, ok = r.BasicAuth()
		if !ok {
			t.Fatalf("missing HTTP Basic client credentials")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		gotScope = r.Form.Get("scope")
		if got := r.Form.Get("client_id"); got != "" {
			t.Fatalf("client_id leaked into form body: %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret leaked into form body: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"minted-token","expires_in":3600}` + "`" + `)
	}))
	defer server.Close()

	cfg := &config.Config{TokenURL: server.URL + "/COMMON/oauth2/v2.0/token", Path: filepath.Join(t.TempDir(), "config.toml")}
	cfg.{{TENANT_FIELD}} = "contoso"
	c := &Client{Config: cfg, HTTPClient: server.Client()}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if gotPath != "/contoso/oauth2/v2.0/token" {
		t.Fatalf("token request path = %q, want /contoso/oauth2/v2.0/token", gotPath)
	}
	if gotScope != "api://client-123/.default" {
		t.Fatalf("scope = %q, want api://client-123/.default", gotScope)
	}
	if gotClientID != "client-123" {
		t.Fatalf("client_id = %q, want client-123", gotClientID)
	}
	if gotClientSecret != "secret-456" {
		t.Fatalf("client_secret = %q, want secret-456", gotClientSecret)
	}

	t.Setenv("ENTRA_CC_OAUTH_SCOPE", "https://override.example/.default")
	if err := c.mintClientCredentials(context.Background(), "client-789", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() with scope override error = %v", err)
	}
	if gotScope != "https://override.example/.default" {
		t.Fatalf("override scope = %q, want https://override.example/.default", gotScope)
	}
}

func TestClientCredentialsRuntimeEntraRequiresTenant(t *testing.T) {
	cfg := &config.Config{TokenURL: "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token"}
	c := &Client{Config: cfg, HTTPClient: http.DefaultClient}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err == nil {
		t.Fatalf("mintClientCredentials() error = nil, want tenant-required error")
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "entra_runtime_test.go"),
		[]byte(strings.ReplaceAll(runtimeTest, "{{TENANT_FIELD}}", resolveEnvVarField("ENTRA_CC_TENANT_ID"))),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestClientCredentialsRuntimeEntra")
}

func TestClientCredentialsAuthLoginTrimsEnvCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("trim-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "TRIM_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "TRIM_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "trim-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = strings.TrimSpace(os.Getenv("TRIM_CC_CLIENT_ID"))`)
	require.Contains(t, authContent, `clientSecret = strings.TrimSpace(os.Getenv("TRIM_CC_CLIENT_SECRET"))`)

	const runtimeTest = `package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthLoginTrimsClientCredentialEnvVars(t *testing.T) {
	var gotClientID string
	var gotClientSecret string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		gotClientID, gotClientSecret, ok = r.BasicAuth()
		if !ok {
			t.Fatalf("missing HTTP Basic client credentials")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("client_id"); got != "" {
			t.Fatalf("client_id leaked into form body: %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret leaked into form body: %q", got)
		}
		if gotClientID != "cid.test123" || gotClientSecret != "secret.test456" {
			http.Error(w, "untrimmed credentials", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"minted-token","expires_in":3600}` + "`" + `)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("token_url = \""+server.URL+"\"\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	t.Setenv("TRIM_CC_CLIENT_ID", " cid.test123 ")
	t.Setenv("TRIM_CC_CLIENT_SECRET", "\tsecret.test456\n")

	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "login"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth login error = %v; output:\n%s", err, out.String())
	}
	if gotClientID != "cid.test123" {
		t.Fatalf("client_id = %q, want cid.test123", gotClientID)
	}
	if gotClientSecret != "secret.test456" {
		t.Fatalf("client_secret = %q, want secret.test456", gotClientSecret)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "auth_login_trim_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestAuthLoginTrimsClientCredentialEnvVars")
}

func TestClientCredentialsStaticTokenURLDoesNotEmitTenantSubstitution(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("static-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STATIC_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "STATIC_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "STATIC_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "static-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.NotContains(t, string(authSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.NotContains(t, string(clientSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"static-cc-pp-cli/internal/config"
)

func TestClientCredentialsRuntimeStaticTokenURLDoesNotSendDefaultScope(t *testing.T) {
	var hasScope bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		_, hasScope = r.Form["scope"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"minted-token","expires_in":3600}` + "`" + `)
	}))
	defer server.Close()

	cfg := &config.Config{TokenURL: server.URL, Path: filepath.Join(t.TempDir(), "config.toml")}
	c := &Client{Config: cfg, HTTPClient: server.Client()}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if hasScope {
		t.Fatalf("unexpected scope form value for static non-Microsoft token URL")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "static_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestClientCredentialsRuntimeStatic")
}

func TestClientCredentialsLegacyEnvVarsSubstituteTenantTokenURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacy-entra-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		EnvVars: []string{
			"LEGACY_ENTRA_TENANT_ID",
			"LEGACY_ENTRA_CLIENT_ID",
			"LEGACY_ENTRA_CLIENT_SECRET",
		},
	}
	ccEnvVars := clientCredentialsEnvVars(apiSpec.Auth)
	require.Len(t, ccEnvVars, 2)
	require.Equal(t, "LEGACY_ENTRA_CLIENT_ID", ccEnvVars[0].Name)
	require.False(t, ccEnvVars[0].Sensitive, "legacy client ID must not be synthesized as sensitive")
	require.Equal(t, "LEGACY_ENTRA_CLIENT_SECRET", ccEnvVars[1].Name)
	require.True(t, ccEnvVars[1].Sensitive, "legacy client secret must stay sensitive")

	outputDir := filepath.Join(t.TempDir(), "legacy-entra-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = strings.TrimSpace(os.Getenv("LEGACY_ENTRA_CLIENT_ID"))`)
	require.Contains(t, authContent, `clientSecret = strings.TrimSpace(os.Getenv("LEGACY_ENTRA_CLIENT_SECRET"))`)
	require.Contains(t, authContent, `resolveClientCredentialsTokenURL(tokenURL, cfg.`+resolveEnvVarField("LEGACY_ENTRA_TENANT_ID")+`)`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("LEGACY_ENTRA_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("LEGACY_ENTRA_CLIENT_SECRET")`)
	require.Contains(t, clientContent, `tenant = c.Config.`+resolveEnvVarField("LEGACY_ENTRA_TENANT_ID"))
	require.Contains(t, clientContent, `resolveClientCredentialsTokenURL(tokenURL, tenant)`)
}

func TestClientCredentialsTenantPrefixDoesNotHideClientCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multitenant-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MULTITENANT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "MULTITENANT_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "multitenant-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = strings.TrimSpace(os.Getenv("MULTITENANT_CLIENT_ID"))`)
	require.Contains(t, authContent, `clientSecret = strings.TrimSpace(os.Getenv("MULTITENANT_CLIENT_SECRET"))`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("MULTITENANT_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("MULTITENANT_CLIENT_SECRET")`)
}

func TestClientCredentialsPerCallTenantIDDoesNotTriggerConfigBackedTenantSubstitution(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("per-call-tenant-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "PER_CALL_TENANT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "PER_CALL_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "PER_CALL_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "per-call-tenant-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.NotContains(t, string(authSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.NotContains(t, string(clientSrc), `resolveClientCredentialsTokenURL(tokenURL`)
}

func TestClientCredentialsDeclaredScopesSuppressMicrosoftDefaultScope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scoped-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token",
		Scopes:      []string{"https://graph.microsoft.com/.default"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SCOPED_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "SCOPED_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "SCOPED_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "scoped-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `return "https://graph.microsoft.com/.default"`)
	require.NotContains(t, clientContent, `api://{client_id}/.default`)
}

func TestClientCredentialsMixedKindEnvVarsPreserveClientRoles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mixed-kind-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MIXED_KIND_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "MIXED_KIND_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "mixed-kind-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = strings.TrimSpace(os.Getenv("MIXED_KIND_CLIENT_ID"))`)
	require.Contains(t, authContent, `clientSecret = strings.TrimSpace(os.Getenv("MIXED_KIND_CLIENT_SECRET"))`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("MIXED_KIND_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("MIXED_KIND_CLIENT_SECRET")`)
}

func TestClientCredentialsHostileScopeEmitsValidGo(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hostile-scope-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		Scopes:      []string{"read\"with\\escapes\nwrite"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "HOSTILE_SCOPE_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "HOSTILE_SCOPE_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "hostile-scope-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "test", "./internal/client")
	runGoCommand(t, outputDir, "test", "./internal/cli")
}

// TestAuthHeader_OAuth2DoesNotUseSetupEnvVars pins that for every OAuth2
// grant (authorization_code via the default, client_credentials via explicit
// OAuth2Grant) the configured env vars (e.g. CLIENT_ID / CLIENT_SECRET) are
// never emitted as bearer headers. The minted AccessToken is the only usable
// bearer; sending CLIENT_ID as `Authorization: Bearer` surfaces as
// token_rejected at the API.
func TestAuthHeader_OAuth2DoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		grant string
	}{
		{"authorization_code", ""},
		{"client_credentials", spec.OAuth2GrantClientCredentials},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("oauth-precedence-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:   "oauth2",
				Header: "Authorization",
				Format: "Bearer {token}",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "OAUTH_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
					{Name: "OAUTH_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
				},
				AuthorizationURL: "https://example.com/auth",
				TokenURL:         "https://example.com/token",
				OAuth2Grant:      tc.grant,
			}

			outputDir := filepath.Join(t.TempDir(), "oauth-precedence-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			clientIDCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_ID") + ` != ""`
			clientSecretCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_SECRET") + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			body := authHeaderBody(t, content)
			require.Contains(t, body, tokenCheck, "AuthHeader must check AccessToken")
			require.Contains(t, body, `applyAuthFormat("Bearer {token}", map[string]string{"access_token": c.AccessToken, "token": c.AccessToken})`,
				"AuthHeader must return the AccessToken via applyAuthFormat")
			require.NotContains(t, body, clientIDCheck, "client ID must not be used as a bearer token")
			require.NotContains(t, body, clientSecretCheck, "client secret must not be used as a bearer token")
		})
	}
}

// TestAuthHeader_BearerAuthFlowInputsPreferAccessToken pins the OpenAPI
// session-handshake shape where the declared Bearer scheme lists setup inputs
// via x-auth-vars kind=auth_flow_input. Those values are only used by auth
// login; the stored AccessToken is the request credential.
func TestAuthHeader_BearerAuthFlowInputsPreferAccessToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-flow-input")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {token}",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "BEARER_FLOW_IDENTIFIER", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "BEARER_FLOW_APP_PASSWORD", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
			{Name: "BEARER_FLOW_FALLBACK_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-flow-input-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))

	identifierCheck := "if c." + resolveEnvVarField("BEARER_FLOW_IDENTIFIER") + ` != ""`
	passwordCheck := "if c." + resolveEnvVarField("BEARER_FLOW_APP_PASSWORD") + ` != ""`
	fallbackCheck := "if c." + resolveEnvVarField("BEARER_FLOW_FALLBACK_TOKEN") + ` != ""`
	require.NotContains(t, body, identifierCheck, "auth_flow_input identifier must not be used as a bearer token")
	require.NotContains(t, body, passwordCheck, "auth_flow_input secret must not be used as a bearer token")
	require.Contains(t, body, fallbackCheck, "per_call fallback token must remain usable")
	require.Contains(t, body, `if c.AccessToken != ""`, "stored AccessToken must remain the Bearer request credential")

	const runtimeTest = `package config

import "testing"

func TestBearerAuthFlowInputsUseAccessToken(t *testing.T) {
	t.Setenv("BEARER_FLOW_IDENTIFIER", "alice.example")
	t.Setenv("BEARER_FLOW_APP_PASSWORD", "app-password")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.AccessToken = "access-jwt"

	if got := cfg.AuthHeader(); got != "Bearer access-jwt" {
		t.Fatalf("AuthHeader() = %q, want Bearer access-jwt", got)
	}
	if cfg.AuthSource != "oauth2" {
		t.Fatalf("AuthSource after AuthHeader() = %q, want oauth2", cfg.AuthSource)
	}
}

func TestBearerAuthFlowInputsAloneDoNotAuthenticate(t *testing.T) {
	t.Setenv("BEARER_FLOW_IDENTIFIER", "alice.example")
	t.Setenv("BEARER_FLOW_APP_PASSWORD", "app-password")
	t.Setenv("BEARER_FLOW_FALLBACK_TOKEN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.AuthHeader(); got != "" {
		t.Fatalf("AuthHeader() = %q, want empty", got)
	}
}

func TestBearerAuthFlowPerCallFallbackStillAuthenticates(t *testing.T) {
	t.Setenv("BEARER_FLOW_IDENTIFIER", "alice.example")
	t.Setenv("BEARER_FLOW_APP_PASSWORD", "app-password")
	t.Setenv("BEARER_FLOW_FALLBACK_TOKEN", "fallback-token")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.AuthHeader(); got != "Bearer fallback-token" {
		t.Fatalf("AuthHeader() = %q, want Bearer fallback-token", got)
	}
	}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "bearer_flow_input_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestBearerAuthFlow")
}

// TestAuthHeader_BearerHarvestedInputsPreferAccessToken pins the harvested
// env-var variant of the non-request Bearer flow. Harvested values are setup
// artifacts, not values to replay directly as request credentials.
func TestAuthHeader_BearerHarvestedInputsPreferAccessToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-harvested-input")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {token}",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "BEARER_HARVESTED_SESSION", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-harvested-input-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))

	harvestedCheck := "if c." + resolveEnvVarField("BEARER_HARVESTED_SESSION") + ` != ""`
	require.NotContains(t, body, harvestedCheck, "harvested session material must not be used as a bearer token")
	require.Contains(t, body, `if c.AccessToken != ""`, "stored AccessToken must remain the Bearer request credential")

	const runtimeTest = `package config

import "testing"

func TestBearerHarvestedInputsUseAccessToken(t *testing.T) {
	t.Setenv("BEARER_HARVESTED_SESSION", "harvested-cookie")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.AccessToken = "access-jwt"

	if got := cfg.AuthHeader(); got != "Bearer access-jwt" {
		t.Fatalf("AuthHeader() = %q, want Bearer access-jwt", got)
	}
}

func TestBearerHarvestedInputsAloneDoNotAuthenticate(t *testing.T) {
	t.Setenv("BEARER_HARVESTED_SESSION", "harvested-cookie")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.AuthHeader(); got != "" {
		t.Fatalf("AuthHeader() = %q, want empty", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "bearer_harvested_input_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestBearerHarvestedInputs")
}

func TestAuthLoginEnvVarsUseShellSafePrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hyphen-api")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		AuthorizationURL: "https://example.com/auth",
		TokenURL:         "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "hyphen-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_ID")`)
	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_SECRET")`)
	require.NotContains(t, content, `HYPHEN-API_CLIENT_ID`)
}

func TestAuthHeader_LegacyEnvVarsList_OrSemantics(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacy-env-or")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"FOO_ACCESS_TOKEN", "FOO_API_KEY", "FOO_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "legacy-env-or-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const testSrc = `package config

import "testing"

func TestAuthHeaderUsesLegacyAliasEnvVar(t *testing.T) {
	t.Setenv("FOO_ACCESS_TOKEN", "")
	t.Setenv("FOO_API_KEY", "alias-token")
	t.Setenv("FOO_TOKEN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "Bearer alias-token" {
		t.Fatalf("AuthHeader() = %q, want %q", got, "Bearer alias-token")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "auth_header_legacy_envvars_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestAuthHeaderUsesLegacyAliasEnvVar")
}

// TestAuthHeader_EnvVarWinsOverFileToken pins env-first precedence for
// the non-client_credentials cases — plain bearer_token (PAT-style),
// cookie, and composed all follow the env > config convention so a
// freshly-rotated env var wins over a stale on-disk AccessToken.
func TestAuthHeader_EnvVarWinsOverFileToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		envVar   string
	}{
		{"bearer_token", "bearer_token", "BEARER_AUTH_TEST_TOKEN"},
		{"cookie", "cookie", "COOKIE_AUTH_TEST_TOKEN"},
		{"composed", "composed", "COMPOSED_AUTH_TEST_TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name + "-precedence")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tc.authType,
				Header:  "Authorization",
				EnvVars: []string{tc.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tc.name+"-precedence-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			envCheck := "if c." + resolveEnvVarField(tc.envVar) + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			require.Contains(t, content, envCheck)
			require.Contains(t, content, tokenCheck)

			body := authHeaderBody(t, content)
			envIdx := strings.Index(body, envCheck)
			tokenIdx := strings.Index(body, tokenCheck)
			assert.Less(t, envIdx, tokenIdx,
				"env-var check must appear BEFORE AccessToken check for type %q", tc.authType)
		})
	}
}

func TestCredentialsRoundTripTestsRunForBrowserAndDeviceAuthFlavors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		auth spec.AuthConfig
	}{
		{
			name: "cookie-credentials-roundtrip",
			auth: spec.AuthConfig{
				Type:    "cookie",
				Header:  "Authorization",
				EnvVars: []string{"COOKIE_ROUNDTRIP_TOKEN"},
			},
		},
		{
			name: "composed-credentials-roundtrip",
			auth: spec.AuthConfig{
				Type:    "composed",
				Header:  "Authorization",
				EnvVars: []string{"COMPOSED_ROUNDTRIP_TOKEN"},
			},
		},
		{
			name: "device-code-credentials-roundtrip",
			auth: spec.AuthConfig{
				Type:                   "oauth2",
				Header:                 "Authorization",
				Format:                 "Bearer {token}",
				OAuth2Grant:            spec.OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "https://login.example.com/device",
				TokenURL:               "https://login.example.com/token",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name)
			apiSpec.Auth = tc.auth
			outputDir := filepath.Join(t.TempDir(), tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())
			runGoCommand(t, outputDir, "test", "./internal/cliutil", "./internal/config")
		})
	}
}

func TestAuthHeader_PreservesConfigAuthSourceForStoredBearerToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-source")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"BEARER_SOURCE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-source-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuthHeaderPreservesConfigAuthSourceForStoredBearerToken(t *testing.T) {
	t.Setenv("BEARER_SOURCE_TOKEN", "")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("source_token = \"disk-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthSource != "config" {
		t.Fatalf("AuthSource after Load() = %q, want config", cfg.AuthSource)
	}
	if got := cfg.AuthHeader(); got != "Bearer disk-token" {
		t.Fatalf("AuthHeader() = %q, want Bearer disk-token", got)
	}
	if cfg.AuthSource != "config" {
		t.Fatalf("AuthSource after AuthHeader() = %q, want config", cfg.AuthSource)
	}
}

func TestAuthHeaderKeepsEnvAuthSourceWhenEnvOverridesConfig(t *testing.T) {
	t.Setenv("BEARER_SOURCE_TOKEN", "env-token")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("source_token = \"disk-token\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthSource != "env:BEARER_SOURCE_TOKEN" {
		t.Fatalf("AuthSource after Load() = %q, want env:BEARER_SOURCE_TOKEN", cfg.AuthSource)
	}
	if got := cfg.AuthHeader(); got != "Bearer env-token" {
		t.Fatalf("AuthHeader() = %q, want Bearer env-token", got)
	}
	if cfg.AuthSource != "env:BEARER_SOURCE_TOKEN" {
		t.Fatalf("AuthSource after AuthHeader() = %q, want env:BEARER_SOURCE_TOKEN", cfg.AuthSource)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "auth_source_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestAuthHeader")
}

// TestAuthHeader_BearerTokenPrefixOverride pins that a bearer_token spec
// declaring auth.prefix changes the rendered Authorization scheme word
// across both the env-var and AccessToken branches. APIs that require a
// non-Bearer scheme (e.g., "Token", "PRIVATE-TOKEN", lowercase "token")
// otherwise force operators to hand-edit generated config. When auth.prefix
// is unset, "Bearer" remains the default.
func TestAuthHeader_BearerTokenPrefixOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prefix   string
		expected string
	}{
		{"default", "", "Bearer"},
		{"token", "Token", "Token"},
		{"lowercase", "token", "token"},
		{"private_token", "PRIVATE-TOKEN", "PRIVATE-TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("prefix-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Prefix:  tc.prefix,
				EnvVars: []string{"PREFIX_TEST_TOKEN"},
			}

			outputDir := filepath.Join(t.TempDir(), "prefix-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			body := authHeaderBody(t, string(cfgSrc))

			envField := resolveEnvVarField("PREFIX_TEST_TOKEN")
			require.Contains(t, body, `return "`+tc.expected+` " + c.`+envField,
				"env-var branch must render configured prefix")
			require.Contains(t, body, `return "`+tc.expected+` " + c.AccessToken`,
				"AccessToken branch must render configured prefix")

			if tc.prefix != "" && tc.expected != "Bearer" {
				assert.NotContains(t, body, `return "Bearer " + c.`+envField,
					"default Bearer literal must not leak when prefix is overridden")
				assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
					"default Bearer literal must not leak when prefix is overridden")
			}
		})
	}
}

// TestAuthHeader_BearerTokenPrefixFormatPrecedence pins that Auth.Format
// wins over Auth.Prefix at the same call sites, so the documented "Ignored
// when Format is set" contract survives template restructuring.
func TestAuthHeader_BearerTokenPrefixFormatPrecedence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("prefix-format")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Prefix:  "Token",
		Format:  "Bearer {token}",
		EnvVars: []string{"PREFIX_FORMAT_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "prefix-format-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))

	envField := resolveEnvVarField("PREFIX_FORMAT_TOKEN")
	require.Contains(t, body, `applyAuthFormat("Bearer {token}"`,
		"Format must render via applyAuthFormat, not via the prefix literal")
	assert.NotContains(t, body, `return "Token " + c.`+envField,
		"Prefix must not leak into the env-var branch when Format is set")
	assert.NotContains(t, body, `return "Token " + c.AccessToken`,
		"Prefix must not leak into the AccessToken branch when Format is set")
}

// TestAuthHeader_BearerTokenPrefixMissedSites exercises the three
// non-default code paths in config.go.tmpl that the main override table
// does not reach: oauth2/client_credentials, BearerRefresh.Enabled, and
// the $isAuthEnvVarORCase branch. Without these cases a revert of any of
// those template sites back to a "Bearer " literal would ship undetected.
func TestAuthHeader_BearerTokenPrefixMissedSites(t *testing.T) {
	t.Parallel()

	t.Run("oauth2_client_credentials", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-oauth2-cc")
		apiSpec.Auth = spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Prefix:      "Token",
			EnvVarSpecs: []spec.AuthEnvVar{{Name: "CC_PREFIX_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput}, {Name: "CC_PREFIX_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: true}},
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://example.com/token",
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-oauth2-cc-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		require.Contains(t, body, `return "Token " + c.AccessToken`,
			"oauth2/client_credentials AccessToken branch must honor configured prefix")
		assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
			"default Bearer literal must not leak in the oauth2/cc branch when prefix is overridden")
	})

	t.Run("bearer_refresh_enabled", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-bearer-refresh")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Prefix:  "Token",
			EnvVars: []string{"REFRESH_PREFIX_TOKEN"},
		}
		apiSpec.BearerRefresh = spec.BearerRefreshConfig{
			BundleURL: "https://cdn.example.com/main.js",
			Pattern:   `"(AAAAAAAA[^"]+)"`,
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-bearer-refresh-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		require.Contains(t, body, `return "Token " + c.AccessToken`,
			"BearerRefresh AccessToken branch must honor configured prefix")
		assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
			"default Bearer literal must not leak in the bearer_refresh branch when prefix is overridden")
	})

	t.Run("env_var_or_case", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-or-case")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "bearer_token",
			Header: "Authorization",
			Prefix: "Token",
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "OR_PREFIX_A", Kind: spec.AuthEnvVarKindPerCall, Required: false},
				{Name: "OR_PREFIX_B", Kind: spec.AuthEnvVarKindPerCall, Required: false},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-or-case-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		fieldA := resolveEnvVarField("OR_PREFIX_A")
		fieldB := resolveEnvVarField("OR_PREFIX_B")
		require.Contains(t, body, `return "Token " + c.`+fieldA,
			"OR-case env-var branch must honor configured prefix (first env var)")
		require.Contains(t, body, `return "Token " + c.`+fieldB,
			"OR-case env-var branch must honor configured prefix (second env var)")
		assert.NotContains(t, body, `return "Bearer " + c.`+fieldA,
			"default Bearer literal must not leak in the OR-case branch when prefix is overridden")
	})
}

// TestTierRouting_BearerPrefix pins that the per-tier bearer scheme in
// client.go.tmpl honors auth.prefix on the tier's auth config, matching
// the default-tier AuthHeader() behavior.
func TestTierRouting_BearerPrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tier-prefix")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"TIER_PREFIX_TOKEN"},
	}
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "primary",
		Tiers: map[string]spec.TierConfig{
			"primary": {
				Auth: spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					Prefix:  "Token",
					EnvVars: []string{"TIER_PRIMARY_TOKEN"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tier-prefix-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	content := string(clientSrc)

	require.Contains(t, content, `value := "Token " + tierValue0`,
		"per-tier bearer auth must honor configured prefix")
	assert.NotContains(t, content, `value := "Bearer " + tierValue0`,
		"default Bearer literal must not leak when tier prefix is overridden")
}

func TestAuthNoneDoesNotEmitCredentialsScaffolding(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-none-paths")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}

	outputDir := filepath.Join(t.TempDir(), "auth-none-paths-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	if _, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "credentials.go")); !os.IsNotExist(err) {
		t.Fatalf("credentials.go stat error = %v, want no credentials scaffolding", err)
	}
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.NotContains(t, configSrc, "LoadCredentials")
	require.NotContains(t, configSrc, "SaveCredentials")
	require.NotContains(t, configSrc, "RemoveCredentials")

	requireGeneratedCompiles(t, outputDir)
}

// authHeaderBody slices out just the AuthHeader function body so precedence
// assertions can't be tricked by a matching pattern in unrelated code
// further down the file.
func authHeaderBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (c *Config) AuthHeader() string {")
	require.NotEqual(t, -1, start, "AuthHeader function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
