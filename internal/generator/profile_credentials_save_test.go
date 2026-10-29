package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestProfileCredentialSaveContracts(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("profile-cred-save")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	helper := funcBody(t, configSrc, "func (c *Config) saveCredentialsThenConfig() error {")
	require.Contains(t, helper, "cliutil.WithFileLock(")
	require.Contains(t, helper, "credentialSavePlan(")
	require.NotContains(t, helper, "lockPath = c.Path")
	require.Contains(t, configSrc, "func explicitProfileLockPath(")
	require.Contains(t, funcBody(t, configSrc, "func (c *Config) save() error {"), "retargetExplicitConfig()")
	cliutilSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
	require.Contains(t, cliutilSrc, "func ResolveConfigFilePath(")

	mod := readModulePath(t, outputDir)
	runtime := profileCredentialSaveRuntimeTest(mod, naming.EnvPrefix(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "profile_save_test.go"), []byte(runtime), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-count=1", "-run", "TestResolveConfigFilePathForExplicitProfile")
	runGoCommand(t, outputDir, "test", "./internal/config", "-count=1", "-run", "TestProfileCredentialSave")
	requireGeneratedCompiles(t, outputDir)

	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			env := []string{"GOOS=" + goos, "GOARCH=amd64", "CGO_ENABLED=0"}
			runGoCommandWithEnv(t, outputDir, env, "vet", "-mod=mod", "./internal/cliutil", "./internal/config")
			runGoCommandWithEnv(t, outputDir, env, "build", "-mod=mod", "./internal/cliutil", "./internal/config")
		})
	}
}

func readModulePath(t *testing.T, outputDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	line, _, _ := strings.Cut(string(data), "\n")
	require.True(t, strings.HasPrefix(line, "module "))
	return strings.TrimPrefix(line, "module ")
}

func profileCredentialSaveRuntimeTest(modulePath, envPrefix string) string {
	return `package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"` + modulePath + `/internal/cliutil"
)

func TestProfileCredentialSave(t *testing.T) {
	t.Run("branches", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		plain := &Config{Path: "rel.toml"}
		if err := plain.retargetExplicitConfig(); err != nil {
			t.Fatal(err)
		}
		if plain.Path != "rel.toml" {
			t.Fatalf("non-explicit path = %q", plain.Path)
		}
		blank := &Config{explicitConfig: true, Path: "  "}
		if err := blank.retargetExplicitConfig(); err == nil {
			t.Fatal("empty explicit path should fail")
		}
		if _, err := explicitProfileLockPath(filepath.Join("profile", "credentials.toml")); err == nil {
			t.Fatal("credentials path outside data/ should fail")
		}
		_, creds, lock, err := plain.credentialSavePlan(plain.Path)
		if err != nil {
			t.Fatal(err)
		}
		if lock != creds {
			t.Fatalf("global lock = %q, credentials path = %q", lock, creds)
		}
	})

	t.Run("sharedLock", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		profile := filepath.Join(root, "profile")
		if err := os.MkdirAll(profile, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "side"), 0o700); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(root, "side", "..", filepath.Base(profile), "b.toml")
		release := holdProfileLock(t, profileLockPath(t, profile))
		t.Cleanup(func() { credentialSaveBeforeLockHook = nil })
		reached := make(chan struct{})
		credentialSaveBeforeLockHook = func() { close(reached) }
		errc := make(chan error, 1)
		go func() {
			errc <- profileConfig(cfgPath).SaveCredential("shared-token")
		}()
		select {
		case <-reached:
		case err := <-errc:
			release()
			t.Fatalf("save finished before the profile lock: %v", err)
		case <-time.After(5 * time.Second):
			release()
			t.Fatal("save did not reach the profile lock")
		}
		select {
		case err := <-errc:
			release()
			t.Fatalf("save finished while the profile lock was held: %v", err)
		case <-time.After(time.Second):
		}
		release()
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("save did not finish after the profile lock was released")
		}
		if _, err := os.Stat(filepath.Join(profile, "data", "credentials.toml.lock")); !os.IsNotExist(err) {
			t.Fatalf("lock file inside data/: %v", err)
		}
	})

	t.Run("perConfigLockDoesNotBlock", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		profile := t.TempDir()
		cfgPath := filepath.Join(profile, "a.toml")
		resolved, err := cliutil.ResolveConfigFilePath(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		release := holdProfileLock(t, resolved)
		defer release()
		errc := make(chan error, 1)
		go func() {
			errc <- profileConfig(cfgPath).SaveCredential("not-blocked")
		}()
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("save blocked on the config path instead of the shared profile lock")
		}
	})

	t.Run("otherProfileDoesNotBlock", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		profile := filepath.Join(root, "profile")
		other := filepath.Join(root, "other")
		if err := os.MkdirAll(profile, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(other, 0o700); err != nil {
			t.Fatal(err)
		}
		release := holdProfileLock(t, profileLockPath(t, other))
		defer release()
		errc := make(chan error, 1)
		go func() {
			errc <- profileConfig(filepath.Join(profile, "a.toml")).SaveCredential("own-profile")
		}()
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("save blocked on a different profile lock")
		}
		otherCreds := filepath.Join(other, "data", "credentials.toml")
		if _, err := os.Stat(otherCreds); !os.IsNotExist(err) {
			t.Fatalf("other profile credentials = %v", err)
		}
	})

	t.Run("failedSaveRollsBack", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		profile := t.TempDir()
		creds := filepath.Join(profile, "data", "credentials.toml")
		prior := []byte("token = \"prior-token\"\n")
		if err := os.MkdirAll(filepath.Dir(creds), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(creds, prior, 0o600); err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(profile, "bad.toml")
		if err := os.Mkdir(bad, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := profileConfig(bad).SaveCredential("should-not-stick"); err == nil {
			t.Fatal("expected config write failure")
		}
		after, err := os.ReadFile(creds)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, prior) {
			t.Fatalf("credentials = %q, want prior %q", after, prior)
		}
	})

	t.Run("concurrentSiblingKeepsSuccessfulLogin", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		profile := t.TempDir()
		creds := filepath.Join(profile, "data", "credentials.toml")
		if err := os.MkdirAll(filepath.Dir(creds), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(creds, []byte("token = \"prior-token\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(profile, "bad.toml")
		if err := os.Mkdir(bad, 0o700); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errc := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			errc <- profileConfig(filepath.Join(profile, "good.toml")).SaveCredential("from-good")
		}()
		go func() {
			defer wg.Done()
			errc <- profileConfig(bad).SaveCredential("from-bad")
		}()
		wg.Wait()
		close(errc)
		var sawOK, sawErr bool
		for err := range errc {
			if err == nil {
				sawOK = true
			} else {
				sawErr = true
			}
		}
		if !sawOK || !sawErr {
			t.Fatalf("want one success and one failure, success=%v failure=%v", sawOK, sawErr)
		}
		after, err := os.ReadFile(creds)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(after, []byte("from-good")) || bytes.Contains(after, []byte("from-bad")) {
			t.Fatalf("credentials after concurrent saves:\n%s", after)
		}
	})

	t.Run("symlinkReload", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		profile := filepath.Join(root, "profile")
		alias := filepath.Join(root, "alias")
		mid := filepath.Join(root, "mid")
		for _, dir := range []string{profile, alias, mid, filepath.Join(root, "side")} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(profile, "config.toml")
		if err := os.WriteFile(target, []byte("base_url = \"https://api.example.com\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		midLink := filepath.Join(mid, "config.toml")
		if err := os.Symlink(filepath.Join("..", "profile", "config.toml"), midLink); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		link := filepath.Join(alias, "config.toml")
		if err := os.Symlink(filepath.Join("..", "mid", "config.toml"), link); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(alias, "data"), 0o700); err != nil {
			t.Fatal(err)
		}
		decoy := []byte("token = \"decoy-token\"\n")
		if err := os.WriteFile(filepath.Join(alias, "data", "credentials.toml"), decoy, 0o600); err != nil {
			t.Fatal(err)
		}
		via := filepath.Join(root, "side", "..", "alias", "config.toml")
		cfg := profileConfig(via)
		if err := cfg.SaveCredential("saved-token"); err != nil {
			t.Fatal(err)
		}
		assertSymlink(t, link)
		assertSymlink(t, midLink)
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Path != resolved {
			t.Fatalf("saved path = %q, want resolved %q", cfg.Path, resolved)
		}
		profileCreds := filepath.Join(profile, "data", "credentials.toml")
		body, err := os.ReadFile(profileCreds)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("saved-token")) {
			t.Fatalf("profile credentials = %q", body)
		}
		decoyAfter, err := os.ReadFile(filepath.Join(alias, "data", "credentials.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoyAfter, decoy) {
			t.Fatalf("alias credentials = %q, want decoy to stay", decoyAfter)
		}
		loaded, err := Load(via)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.MyapiToken != "saved-token" {
			t.Fatalf("reload token = %q, want saved-token", loaded.MyapiToken)
		}
		if err := loaded.ClearTokens(); err != nil {
			t.Fatal(err)
		}
		assertSymlink(t, link)
		if _, err := os.Stat(profileCreds); !os.IsNotExist(err) {
			t.Fatalf("profile credentials after logout = %v", err)
		}
		again, err := Load(link)
		if err != nil {
			t.Fatal(err)
		}
		if again.MyapiToken != "" {
			t.Fatalf("reload after logout = %q", again.MyapiToken)
		}
	})

	t.Run("danglingSymlinkReload", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		alias := filepath.Join(root, "alias")
		if err := os.MkdirAll(alias, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(alias, "config.toml")
		if err := os.Symlink(filepath.Join(root, "missing", "config.toml"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := profileConfig(link).SaveCredential("dangling-token"); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(link)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.MyapiToken != "dangling-token" {
			t.Fatalf("reload token = %q", loaded.MyapiToken)
		}
	})

	t.Run("regularFileStaysBesideItself", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		profile := filepath.Join(root, "profile")
		alias := filepath.Join(root, "alias")
		if err := os.MkdirAll(profile, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(alias, 0o700); err != nil {
			t.Fatal(err)
		}
		plain := filepath.Join(alias, "plain.toml")
		if err := profileConfig(plain).SaveCredential("plain-token"); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(plain)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			t.Fatalf("plain config mode = %v", info.Mode())
		}
		if _, err := os.Stat(filepath.Join(profile, "data", "credentials.toml")); !os.IsNotExist(err) {
			t.Fatalf("unrelated profile credentials = %v", err)
		}
		loaded, err := Load(plain)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.MyapiToken != "plain-token" {
			t.Fatalf("reload token = %q", loaded.MyapiToken)
		}
	})

	t.Run("symlinkedDataDirIsNotLockTarget", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		profile := t.TempDir()
		foreign := t.TempDir()
		if err := os.Symlink(foreign, filepath.Join(profile, "data")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := profileConfig(filepath.Join(profile, "config.toml")).SaveCredential("nope")
		if err == nil {
			t.Fatal("expected symlinked data directory to be refused")
		}
		entries, readErr := os.ReadDir(foreign)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			t.Fatalf("symlinked data dir received %s", strings.Join(names, ", "))
		}
		if _, statErr := os.Stat(filepath.Join(profile, ".credentials.lock")); statErr != nil {
			t.Fatalf("profile lock = %v", statErr)
		}
	})

	t.Run("swappedSymlinkDoesNotPublish", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		profile := filepath.Join(root, "profile")
		other := filepath.Join(root, "other")
		alias := filepath.Join(root, "alias")
		for _, dir := range []string{profile, other, alias} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(profile, "config.toml")
		otherTarget := filepath.Join(other, "config.toml")
		for _, path := range []string{target, otherTarget} {
			if err := os.WriteFile(path, []byte("base_url = \"https://api.example.com\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		link := filepath.Join(alias, "config.toml")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		prior := []byte("token = \"stay-token\"\n")
		if err := os.MkdirAll(filepath.Join(profile, "data"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "data", "credentials.toml"), prior, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { credentialSaveBeforeLockHook = nil })
		credentialSaveBeforeLockHook = func() {
			if err := os.Remove(link); err != nil {
				t.Errorf("remove link: %v", err)
				return
			}
			if err := os.Symlink(otherTarget, link); err != nil {
				t.Errorf("retarget link: %v", err)
			}
		}
		err := profileConfig(link).SaveCredential("swapped-token")
		if err == nil || !strings.Contains(err.Error(), "profile changed while locking credentials") {
			t.Fatalf("error = %v", err)
		}
		body, readErr := os.ReadFile(filepath.Join(profile, "data", "credentials.toml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(body, prior) {
			t.Fatalf("original credentials = %q", body)
		}
		if _, statErr := os.Stat(filepath.Join(other, "data", "credentials.toml")); !os.IsNotExist(statErr) {
			t.Fatalf("swapped profile credentials = %v", statErr)
		}
	})

	t.Run("caseFoldedProfileSharesLogin", func(t *testing.T) {
		isolateProfileHome(t, "` + envPrefix + `")
		root := t.TempDir()
		if !caseInsensitiveDir(t, root) {
			t.Skip("case-sensitive filesystem")
		}
		profile := filepath.Join(root, "Mixed")
		if err := os.Mkdir(profile, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := profileConfig(filepath.Join(root, "mixed", "other.toml")).SaveCredential("case-token"); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(filepath.Join(profile, "config.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if loaded.MyapiToken != "case-token" {
			t.Fatalf("reload token = %q", loaded.MyapiToken)
		}
	})
}

func isolateProfileHome(t *testing.T, envPrefix string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("MYAPI_TOKEN", "")
	t.Setenv(envPrefix+"_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv(envPrefix+"_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
}

func profileConfig(path string) *Config {
	return &Config{Path: path, explicitConfig: true, BaseURL: "https://api.example.com"}
}

func profileLockPath(t *testing.T, profile string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(profile)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(resolved, ".credentials")
}

func holdProfileLock(t *testing.T, path string) func() {
	t.Helper()
	entered := make(chan struct{})
	letGo := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := cliutil.WithFileLock(path, func() error {
			close(entered)
			<-letGo
			return nil
		}); err != nil {
			t.Errorf("hold lock: %v", err)
		}
	}()
	<-entered
	var once sync.Once
	return func() {
		once.Do(func() {
			close(letGo)
			<-done
		})
	}
}

func assertSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", path)
	}
}

func caseInsensitiveDir(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}
`
}
