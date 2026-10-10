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

func TestGeneratedDefaultDBPathStableOAuthScope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("db-scope-oauth")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://api.example.com/oauth/authorize",
		TokenURL:         "https://api.example.com/oauth/token",
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	testSrc := strings.ReplaceAll(oauthStableScopeTestSource, "__MODULE_PATH__", generatedModulePath(t, outputDir))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "oauth_stable_scope_test.go"), []byte(testSrc), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestStableStoreScope", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedDefaultDBPathStableBearerRefreshScope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("db-scope-bearer")
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.BearerRefresh = spec.BearerRefreshConfig{
		BundleURL: "https://cdn.example.com/main.js",
		Pattern:   `"(AAAAAAAA[^"]+)"`,
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"DB_SCOPE_BEARER_TOKEN"},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	testSrc := strings.ReplaceAll(bearerStableScopeTestSource, "__MODULE_PATH__", generatedModulePath(t, outputDir))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "bearer_stable_scope_test.go"), []byte(testSrc), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestStableStoreScope", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

const oauthStableScopeTestSource = `package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/config"
)

func TestStableStoreScopeOAuthIgnoresAccessToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_OAUTH_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(false)
	t.Cleanup(func() { config.StoreScopeIdentity = nil })

	firstPath := filepath.Join(t.TempDir(), "first.toml")
	writeOAuthScopeConfig(t, firstPath, "https://api.example.com/v1", "session-a", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(firstPath)
	first := defaultDBPath("db-scope-oauth-pp-cli")
	assertStableScopeFile(t, first, firstPath)

	secondPath := filepath.Join(t.TempDir(), "second.toml")
	writeOAuthScopeConfig(t, secondPath, "https://API.Example.com/v1/", "session-b", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(secondPath)
	second := defaultDBPath("db-scope-oauth-pp-cli")
	if first != second {
		t.Fatalf("renewed access token changed store path:\n first %s\nsecond %s", first, second)
	}

	if err := os.MkdirAll(filepath.Dir(first), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	refreshPath := filepath.Join(t.TempDir(), "refresh.toml")
	writeOAuthScopeConfig(t, refreshPath, "https://api.example.com/v1", "session-a", "refresh-other", "client-1", "secret-1")
	configureDefaultDBScope(refreshPath)
	otherRefresh := defaultDBPath("db-scope-oauth-pp-cli")
	if otherRefresh == first {
		t.Fatalf("different refresh token reused %s", first)
	}
	if data, err := os.ReadFile(first); err != nil || string(data) != "kept" {
		t.Fatalf("first store bytes = %q, err %v", data, err)
	}

	basePath := filepath.Join(t.TempDir(), "base.toml")
	writeOAuthScopeConfig(t, basePath, "https://other.example.com/v1", "session-a", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(basePath)
	otherBase := defaultDBPath("db-scope-oauth-pp-cli")
	if otherBase == first || otherBase == otherRefresh {
		t.Fatalf("different base URL reused an existing store: %s", otherBase)
	}

	config.StoreScopeIdentity = func(*config.Config) string { return "account-42" }
	configureDefaultDBScope(firstPath)
	hooked := defaultDBPath("db-scope-oauth-pp-cli")
	if !strings.HasSuffix(filepath.Base(hooked), stableScopeHash("account-42")+".db") {
		t.Fatalf("identity hook path = %s", hooked)
	}
	if hooked == first {
		t.Fatal("identity hook reused the default scope path")
	}
}

func TestStableStoreScopeOAuthWithoutAccountMaterialStaysLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_OAUTH_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(false)

	barePath := filepath.Join(t.TempDir(), "bare.toml")
	writeOAuthScopeConfig(t, barePath, "https://api.example.com/v1", "", "", "", "")
	configureDefaultDBScope(barePath)
	if got := defaultDBPath("db-scope-oauth-pp-cli"); filepath.Base(got) != "data.db" {
		t.Fatalf("logged-out path = %s, want data.db", got)
	}

	tokenA := filepath.Join(t.TempDir(), "token-a.toml")
	writeOAuthScopeConfig(t, tokenA, "https://api.example.com/v1", "session-a", "", "", "")
	configureDefaultDBScope(tokenA)
	cfg, err := config.Load(tokenA)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StoreScopeCredential() != cfg.StoreScopeLegacyCredential() || !strings.Contains(cfg.StoreScopeCredential(), "session-a") {
		t.Fatalf("access-token-only scope = %q legacy %q", cfg.StoreScopeCredential(), cfg.StoreScopeLegacyCredential())
	}
	first := defaultDBPath("db-scope-oauth-pp-cli")

	tokenB := filepath.Join(t.TempDir(), "token-b.toml")
	writeOAuthScopeConfig(t, tokenB, "https://api.example.com/v1", "session-b", "", "", "")
	configureDefaultDBScope(tokenB)
	if got := defaultDBPath("db-scope-oauth-pp-cli"); got == first {
		t.Fatalf("distinct access tokens shared %s", first)
	}
}

func TestStableStoreScopeOAuthAdoptsTokenHashFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_OAUTH_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(false)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeOAuthScopeConfig(t, configPath, "https://api.example.com/v1", "session-a", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	stable := cfg.StoreScopeCredential()
	legacy := cfg.StoreScopeLegacyCredential()
	if stable == "" || stable == legacy {
		t.Fatalf("stable scope %q collapsed onto legacy %q", stable, legacy)
	}
	if strings.Contains(stable, "session-a") {
		t.Fatalf("stable scope includes the access token: %s", stable)
	}

	fresh := defaultDBPath("db-scope-oauth-pp-cli")
	dir := filepath.Dir(fresh)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyFile := filepath.Join(dir, "data-"+stableScopeHash(legacy)+".db")
	if err := os.WriteFile(legacyFile, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyFile+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}

	adopted := defaultDBPath("db-scope-oauth-pp-cli")
	if adopted != filepath.Join(dir, "data-"+stableScopeHash(stable)+".db") {
		t.Fatalf("adopted path = %s", adopted)
	}
	if data, err := os.ReadFile(adopted); err != nil || string(data) != "kept" {
		t.Fatalf("adopted bytes = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(adopted + "-wal"); err != nil || string(data) != "wal" {
		t.Fatalf("adopted wal = %q, err %v", data, err)
	}
	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Fatalf("legacy scope file still present: %v", err)
	}

	renewed := filepath.Join(t.TempDir(), "renewed.toml")
	writeOAuthScopeConfig(t, renewed, "https://api.example.com/v1", "session-b", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(renewed)
	if got := defaultDBPath("db-scope-oauth-pp-cli"); got != adopted {
		t.Fatalf("renewed token path = %s, want %s", got, adopted)
	}
	if data, err := os.ReadFile(adopted); err != nil || string(data) != "kept" {
		t.Fatalf("renewed read = %q, err %v", data, err)
	}
}

func TestStableStoreScopeDryRunDoesNotAdoptLegacyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_OAUTH_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(true)
	t.Cleanup(func() { setLegacyDBClaimSuppressed(false) })

	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeOAuthScopeConfig(t, configPath, "https://api.example.com/v1", "session-a", "refresh-stable", "client-1", "secret-1")
	configureDefaultDBScope(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(defaultDBPath("db-scope-oauth-pp-cli"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "data-"+stableScopeHash(cfg.StoreScopeLegacyCredential())+".db")
	stable := filepath.Join(dir, "data-"+stableScopeHash(cfg.StoreScopeCredential())+".db")
	if err := os.WriteFile(legacy, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDBPath("db-scope-oauth-pp-cli"); got != legacy {
		t.Fatalf("dry-run path = %s, want %s", got, legacy)
	}
	if _, err := os.Stat(stable); !os.IsNotExist(err) {
		t.Fatalf("dry-run published the stable database: %v", err)
	}
	if _, err := os.Stat(stable + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("dry-run published the stable wal: %v", err)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "kept" {
		t.Fatalf("legacy bytes = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(legacy + "-wal"); err != nil || string(data) != "wal" {
		t.Fatalf("legacy wal = %q, err %v", data, err)
	}
}

func TestStableStoreScopeConcurrentAdoptKeepsLoserFile(t *testing.T) {
	dir := t.TempDir()
	stable := filepath.Join(dir, "data-stable.db")
	legacyA := filepath.Join(dir, "data-a.db")
	legacyB := filepath.Join(dir, "data-b.db")
	writeStoreBytes(t, legacyA, "AAAA", "walA")
	writeStoreBytes(t, legacyB, "BBBB", "walB")
	setLegacyDBClaimSuppressed(false)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		adoptLegacyScopedStore(stable, legacyA)
	}()
	go func() {
		defer wg.Done()
		adoptLegacyScopedStore(stable, legacyB)
	}()
	wg.Wait()
	assertOneStoreSurvived(t, stable, legacyA, "AAAA", "walA", legacyB, "BBBB", "walB")
}

func TestStableStoreScopeAdoptDoesNotReplaceAcrossProcesses(t *testing.T) {
	if role := os.Getenv("PP_ADOPT_ROLE"); role != "" {
		runAdoptRoleChild(role)
		return
	}

	dir := t.TempDir()
	stable := filepath.Join(dir, "data-stable.db")
	legacy := filepath.Join(dir, "data-legacy.db")
	writeStoreBytes(t, legacy, "legacy-bytes", "wal-bytes")
	release := filepath.Join(dir, "release")
	ready := filepath.Join(dir, "holder-ready")
	started := filepath.Join(dir, "adopter-started")

	holder := startAdoptChild(t, "hold", stable, legacy, release, ready, started)
	defer func() {
		if holder.Process != nil && holder.ProcessState == nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	if !waitForFile(ready, 5*time.Second) {
		t.Fatalf("holder did not acquire the lock: %s", holder.stderr.String())
	}
	adopter := startAdoptChild(t, "adopt", stable, legacy, release, ready, started)
	adoptDone := make(chan error, 1)
	go func() { adoptDone <- adopter.Wait() }()
	adopterFinished := false
	defer func() {
		if adopterFinished {
			return
		}
		if adopter.Process != nil {
			_ = adopter.Process.Kill()
		}
		<-adoptDone
	}()
	if !waitForFile(started, 5*time.Second) {
		t.Fatalf("adopter did not start: %s", adopter.stderr.String())
	}
	select {
	case err := <-adoptDone:
		adopterFinished = true
		t.Fatalf("adopt finished while the scope lock was held: %v: %s", err, adopter.stderr.String())
	case <-time.After(500 * time.Millisecond):
	}
	if err := os.WriteFile(stable, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-adoptDone; err != nil {
		adopterFinished = true
		t.Fatalf("adopter: %v: %s", err, adopter.stderr.String())
	}
	adopterFinished = true
	if data, err := os.ReadFile(stable); err != nil || string(data) != "kept" {
		t.Fatalf("stable bytes = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "legacy-bytes" {
		t.Fatalf("legacy bytes = %q, err %v", data, err)
	}
	if _, err := os.Stat(stable + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("adopt published a wal onto the existing database: %v", err)
	}
	if data, err := os.ReadFile(legacy + "-wal"); err != nil || string(data) != "wal-bytes" {
		t.Fatalf("legacy wal = %q, err %v", data, err)
	}
}

func TestStableStoreScopeAdoptLeavesForeignSidecar(t *testing.T) {
	dir := t.TempDir()
	stable := filepath.Join(dir, "data-stable.db")
	legacy := filepath.Join(dir, "data-legacy.db")
	writeStoreBytes(t, legacy, "legacy-bytes", "wal-bytes")
	if err := os.WriteFile(stable+"-wal", []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	setLegacyDBClaimSuppressed(false)
	got, ok := adoptLegacyScopedStore(stable, legacy)
	if !ok || got != legacy {
		t.Fatalf("adopt = %s, %v; want the legacy path", got, ok)
	}
	if _, err := os.Stat(stable); !os.IsNotExist(err) {
		t.Fatalf("main database published beside a foreign wal: %v", err)
	}
	if data, err := os.ReadFile(stable + "-wal"); err != nil || string(data) != "foreign" {
		t.Fatalf("foreign wal = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "legacy-bytes" {
		t.Fatalf("legacy bytes = %q, err %v", data, err)
	}
}

func writeStoreBytes(t *testing.T, path, db, wal string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(db), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-wal", []byte(wal), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertOneStoreSurvived(t *testing.T, stable, legacyA, dataA, walA, legacyB, dataB, walB string) {
	t.Helper()
	stableData, err := os.ReadFile(stable)
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(stable + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	var loser, loserData, loserWal string
	switch string(stableData) {
	case dataA:
		loser, loserData, loserWal = legacyB, dataB, walB
		if string(wal) != walA {
			t.Fatalf("wal %q does not match the published database", wal)
		}
	case dataB:
		loser, loserData, loserWal = legacyA, dataA, walA
		if string(wal) != walB {
			t.Fatalf("wal %q does not match the published database", wal)
		}
	default:
		t.Fatalf("stable database = %q", stableData)
	}
	if data, err := os.ReadFile(loser); err != nil || string(data) != loserData {
		t.Fatalf("loser database = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(loser + "-wal"); err != nil || string(data) != loserWal {
		t.Fatalf("loser wal = %q, err %v", data, err)
	}
}

type adoptChild struct {
	*exec.Cmd
	stderr bytes.Buffer
}

func startAdoptChild(t *testing.T, role, stable, legacy, release, ready, started string) *adoptChild {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStableStoreScopeAdoptDoesNotReplaceAcrossProcesses$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"PP_ADOPT_ROLE="+role,
		"PP_ADOPT_STABLE="+stable,
		"PP_ADOPT_LEGACY="+legacy,
		"PP_ADOPT_RELEASE="+release,
		"PP_ADOPT_READY="+ready,
		"PP_ADOPT_STARTED="+started,
	)
	child := &adoptChild{Cmd: cmd}
	cmd.Stderr = &child.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", role, err)
	}
	return child
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func runAdoptRoleChild(role string) {
	stable := os.Getenv("PP_ADOPT_STABLE")
	legacy := os.Getenv("PP_ADOPT_LEGACY")
	switch role {
	case "hold":
		err := cliutil.WithFileLock(stable, func() error {
			if err := os.WriteFile(os.Getenv("PP_ADOPT_READY"), []byte("1"), 0o600); err != nil {
				return err
			}
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(os.Getenv("PP_ADOPT_RELEASE")); err == nil {
					return nil
				}
				time.Sleep(10 * time.Millisecond)
			}
			return os.ErrDeadlineExceeded
		})
		if err != nil {
			os.Exit(1)
		}
	case "adopt":
		if err := os.WriteFile(os.Getenv("PP_ADOPT_STARTED"), []byte("1"), 0o600); err != nil {
			os.Exit(1)
		}
		adoptLegacyScopedStore(stable, legacy)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func writeOAuthScopeConfig(t *testing.T, path, baseURL, accessToken, refreshToken, clientID, clientSecret string) {
	t.Helper()
	body := "base_url = " + quoteTOML(baseURL) + "\n" +
		"access_token = " + quoteTOML(accessToken) + "\n" +
		"refresh_token = " + quoteTOML(refreshToken) + "\n" +
		"client_id = " + quoteTOML(clientID) + "\n" +
		"client_secret = " + quoteTOML(clientSecret) + "\n"
	writePrivateFile(t, path, body)
}

func assertStableScopeFile(t *testing.T, path, configPath string) {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := stableScopeHash(cfg.StoreScopeCredential())
	if !strings.HasSuffix(filepath.Base(path), want+".db") {
		t.Fatalf("path %s does not include stable hash %s (scope %q)", path, want, cfg.StoreScopeCredential())
	}
	if filepath.Base(path) == "data.db" {
		t.Fatalf("stable credential selected the unscoped database: %s", path)
	}
}

func stableScopeHash(credential string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(credential)))
	return hex.EncodeToString(sum[:])[:defaultDBScopeHashLen]
}

func quoteTOML(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "\\\"") + "\""
}

func writePrivateFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
`

const bearerStableScopeTestSource = `package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/config"
)

func TestStableStoreScopeBearerRefreshUsesBaseURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_BEARER_TOKEN", "")
	t.Setenv("DB_SCOPE_BEARER_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(false)

	firstPath := filepath.Join(t.TempDir(), "first.toml")
	writeBearerScopeConfig(t, firstPath, "https://api.example.com/v1", "session-a")
	configureDefaultDBScope(firstPath)
	cfg, err := config.Load(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.StoreScopeCredential(), "session-a") {
		t.Fatalf("bearer scope includes the rotating token: %s", cfg.StoreScopeCredential())
	}
	if cfg.StoreScopeCredential() != "base_url=https://api.example.com/v1" {
		t.Fatalf("bearer scope = %q", cfg.StoreScopeCredential())
	}
	first := defaultDBPath("db-scope-bearer-pp-cli")
	if !strings.HasSuffix(filepath.Base(first), stableScopeHash(cfg.StoreScopeCredential())+".db") {
		t.Fatalf("path %s is not the base URL scope", first)
	}

	secondPath := filepath.Join(t.TempDir(), "second.toml")
	writeBearerScopeConfig(t, secondPath, "https://API.Example.com/v1/", "session-b")
	configureDefaultDBScope(secondPath)
	if got := defaultDBPath("db-scope-bearer-pp-cli"); got != first {
		t.Fatalf("renewed bearer token changed store path: got %s want %s", got, first)
	}

	otherPath := filepath.Join(t.TempDir(), "other.toml")
	writeBearerScopeConfig(t, otherPath, "https://other.example.com/v1", "session-a")
	configureDefaultDBScope(otherPath)
	if got := defaultDBPath("db-scope-bearer-pp-cli"); got == first {
		t.Fatalf("different backend reused %s", first)
	}

	loggedOut := filepath.Join(t.TempDir(), "logged-out.toml")
	writeBearerScopeConfig(t, loggedOut, "https://api.example.com/v1", "")
	configureDefaultDBScope(loggedOut)
	loggedOutCfg, err := config.Load(loggedOut)
	if err != nil {
		t.Fatal(err)
	}
	if loggedOutCfg.StoreScopeCredential() != "" {
		t.Fatalf("logged-out bearer scope = %q, want empty", loggedOutCfg.StoreScopeCredential())
	}
	if got := defaultDBPath("db-scope-bearer-pp-cli"); filepath.Base(got) != "data.db" {
		t.Fatalf("logged-out bearer path = %s, want data.db", got)
	}
}

func TestStableStoreScopeBearerRefreshAdoptsTokenHashFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DB_SCOPE_BEARER_TOKEN", "")
	t.Setenv("DB_SCOPE_BEARER_BASE_URL", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	setDefaultDBScopeIdentity("", "", "")
	setLegacyDBClaimSuppressed(false)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeBearerScopeConfig(t, configPath, "https://api.example.com/v1", "session-a")
	configureDefaultDBScope(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fresh := defaultDBPath("db-scope-bearer-pp-cli")
	dir := filepath.Dir(fresh)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyFile := filepath.Join(dir, "data-"+stableScopeHash(cfg.StoreScopeLegacyCredential())+".db")
	if err := os.WriteFile(legacyFile, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	adopted := defaultDBPath("db-scope-bearer-pp-cli")
	if data, err := os.ReadFile(adopted); err != nil || string(data) != "kept" {
		t.Fatalf("adopted bytes = %q, err %v (path %s)", data, err, adopted)
	}
	renewed := filepath.Join(t.TempDir(), "renewed.toml")
	writeBearerScopeConfig(t, renewed, "https://api.example.com/v1", "session-b")
	configureDefaultDBScope(renewed)
	if got := defaultDBPath("db-scope-bearer-pp-cli"); got != adopted {
		t.Fatalf("renewed bearer path = %s, want %s", got, adopted)
	}
	if data, err := os.ReadFile(adopted); err != nil || string(data) != "kept" {
		t.Fatalf("renewed read = %q, err %v", data, err)
	}
}

func writeBearerScopeConfig(t *testing.T, path, baseURL, accessToken string) {
	t.Helper()
	body := "base_url = " + quoteTOML(baseURL) + "\n" +
		"access_token = " + quoteTOML(accessToken) + "\n"
	writePrivateFile(t, path, body)
}

func stableScopeHash(credential string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(credential)))
	return hex.EncodeToString(sum[:])[:defaultDBScopeHashLen]
}

func quoteTOML(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "\\\"") + "\""
}

func writePrivateFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
`
