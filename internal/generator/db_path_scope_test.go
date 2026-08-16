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

func TestGeneratedDefaultDBPathScopesCredential(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("db-scope")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	modulePath := generatedModulePath(t, outputDir)
	testSrc := strings.ReplaceAll(defaultDBPathScopeTestSource, "__MODULE_PATH__", modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "default_db_path_scope_test.go"), []byte(testSrc), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestDefaultDBPathScope", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedDefaultDBPathScopesComposedSiblingCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("db-scope-composed")
	apiSpec.Auth = spec.AuthConfig{
		Type:         "composed",
		Header:       "Authorization",
		EnvVars:      []string{"COMPOSED_SCOPE_TOKEN"},
		CookieDomain: ".example.test",
		Cookies:      []string{"session_id"},
		AdditionalHeaders: []spec.AdditionalAuthHeader{
			{
				Header: "X-App-Key",
				In:     "header",
				EnvVar: spec.AuthEnvVar{
					Name:      "COMPOSED_SCOPE_APP_KEY",
					Kind:      spec.AuthEnvVarKindPerCall,
					Required:  true,
					Sensitive: true,
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	modulePath := generatedModulePath(t, outputDir)
	testSrc := strings.ReplaceAll(defaultDBPathComposedScopeTestSource, "__MODULE_PATH__", modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "default_db_path_composed_scope_test.go"), []byte(testSrc), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestDefaultDBPathScopeComposed", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

const defaultDBPathScopeTestSource = `package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
)

func TestDefaultDBPathScopeCredentialsAndLegacy(t *testing.T) {
	resetDefaultDBScopeTest(t)

	t.Setenv("MYAPI_TOKEN", "alpha")
	configureDefaultDBScope("")
	alpha := defaultDBPath("db-scope-pp-cli")
	assertScopedDBPath(t, alpha, "Bearer alpha")

	t.Setenv("MYAPI_TOKEN", "beta")
	configureDefaultDBScope("")
	beta := defaultDBPath("db-scope-pp-cli")
	assertScopedDBPath(t, beta, "Bearer beta")
	if alpha == beta {
		t.Fatalf("two credentials selected the same database path: %s", alpha)
	}

	t.Setenv("MYAPI_TOKEN", "")
	configureDefaultDBScope("")
	if got := filepath.Base(defaultDBPath("db-scope-pp-cli")); got != "data.db" {
		t.Fatalf("no credential selected %s, want data.db", got)
	}

	dir := filepath.Dir(alpha)
	legacy := filepath.Join(dir, "data.db")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MYAPI_TOKEN", "gamma")
	configureDefaultDBScope("")
	if got := defaultDBPath("db-scope-pp-cli"); got != legacy {
		t.Fatalf("legacy unscoped database was not preserved: got %s want %s", got, legacy)
	}

	scopedGamma := scopedDBPathForCredential(dir, "Bearer gamma")
	if err := os.WriteFile(scopedGamma, []byte("scoped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDBPath("db-scope-pp-cli"); got != scopedGamma {
		t.Fatalf("scoped sibling should win once present: got %s want %s", got, scopedGamma)
	}
}

func TestDefaultDBPathScopeUsesConfigLoadPrecedence(t *testing.T) {
	resetDefaultDBScopeTest(t)

	if err := cliutil.SaveCredentials(&cliutil.Credentials{MyapiToken: "from-credentials"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	configureDefaultDBScope("")
	credentialsPath := defaultDBPath("db-scope-pp-cli")
	assertScopedDBPath(t, credentialsPath, "Bearer from-credentials")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeTokenConfig(t, configPath, "from-config", 0o600)
	configureDefaultDBScope(configPath)
	configPathDB := defaultDBPath("db-scope-pp-cli")
	assertScopedDBPath(t, configPathDB, "Bearer from-config")
	if configPathDB == credentialsPath {
		t.Fatalf("--config did not override credentials-file scope: %s", configPathDB)
	}

	t.Setenv("DB_SCOPE_CONFIG", configPath)
	configureDefaultDBScope("")
	if got := defaultDBPath("db-scope-pp-cli"); got != configPathDB {
		t.Fatalf("config env var and equivalent --config disagreed: got %s want %s", got, configPathDB)
	}

	t.Setenv("MYAPI_TOKEN", "from-env")
	configureDefaultDBScope(configPath)
	envDB := defaultDBPath("db-scope-pp-cli")
	assertScopedDBPath(t, envDB, "Bearer from-env")
	if envDB == configPathDB {
		t.Fatalf("credential env var did not override --config scope: %s", envDB)
	}
}

func TestDefaultDBPathScopeRefusedConfigFallsBackToUnscoped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission refusal uses platform ACLs")
	}
	resetDefaultDBScopeTest(t)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeTokenConfig(t, configPath, "too-visible", 0o644)
	configureDefaultDBScope(configPath)
	if got := filepath.Base(defaultDBPath("db-scope-pp-cli")); got != "data.db" {
		t.Fatalf("refused credential config selected %s, want data.db", got)
	}
}

func resetDefaultDBScopeTest(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MYAPI_TOKEN", "")
	t.Setenv("DB_SCOPE_CONFIG", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeCredential("")
}

func writeTokenConfig(t *testing.T, path, token string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("token = "+quoteTOMLString(token)+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertScopedDBPath(t *testing.T, path, credential string) {
	t.Helper()
	if !strings.HasSuffix(filepath.Base(path), defaultDBScopeHash(credential)+".db") {
		t.Fatalf("path %s does not include credential hash %s", path, defaultDBScopeHash(credential))
	}
}

func scopedDBPathForCredential(dir, credential string) string {
	return filepath.Join(dir, "data-"+defaultDBScopeHash(credential)+".db")
}

func defaultDBScopeHash(credential string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(credential)))
	return hex.EncodeToString(sum[:])[:defaultDBScopeHashLen]
}

func quoteTOMLString(value string) string {
	return "\""+strings.ReplaceAll(value, "\"", "\\\"")+"\""
}
`

const defaultDBPathComposedScopeTestSource = `package cli

import (
	"os"
	"path/filepath"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
)

func TestDefaultDBPathScopeComposedSiblingCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMPOSED_SCOPE_TOKEN", "shared-primary")
	t.Setenv("COMPOSED_SCOPE_APP_KEY", "sibling-a")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeCredential("")

	configureDefaultDBScope("")
	first := defaultDBPath("db-scope-composed-pp-cli")
	if filepath.Base(first) == "data.db" {
		t.Fatalf("first composed identity selected unscoped database: %s", first)
	}

	t.Setenv("COMPOSED_SCOPE_APP_KEY", "sibling-b")
	configureDefaultDBScope("")
	second := defaultDBPath("db-scope-composed-pp-cli")
	if filepath.Base(second) == "data.db" {
		t.Fatalf("second composed identity selected unscoped database: %s", second)
	}
	if first == second {
		t.Fatalf("composed identities with different sibling credentials selected the same database: %s", first)
	}
}

func TestDefaultDBPathScopeComposedCookieAndSiblingCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMPOSED_SCOPE_TOKEN", "")
	t.Setenv("COMPOSED_SCOPE_APP_KEY", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeCredential("")

	firstConfig := filepath.Join(t.TempDir(), "first.toml")
	writeComposedScopeConfig(t, firstConfig, "Bearer shared-primary", "cookie-a", "same-sibling")
	configureDefaultDBScope(firstConfig)
	first := defaultDBPath("db-scope-composed-pp-cli")
	if filepath.Base(first) == "data.db" {
		t.Fatalf("first composed cookie identity selected unscoped database: %s", first)
	}

	secondConfig := filepath.Join(t.TempDir(), "second.toml")
	writeComposedScopeConfig(t, secondConfig, "Bearer shared-primary", "cookie-b", "same-sibling")
	configureDefaultDBScope(secondConfig)
	second := defaultDBPath("db-scope-composed-pp-cli")
	if filepath.Base(second) == "data.db" {
		t.Fatalf("second composed cookie identity selected unscoped database: %s", second)
	}
	if first == second {
		t.Fatalf("composed identities with different cookies selected the same database: %s", first)
	}
}

func writeComposedScopeConfig(t *testing.T, path, authHeader, cookie, sibling string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "auth_header = \""+authHeader+"\"\n"+
		"access_token = \""+cookie+"\"\n"+
		"scope_app_key = \""+sibling+"\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
`
