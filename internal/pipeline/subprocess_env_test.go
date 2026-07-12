package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

func TestApplyScopedConfigHomeRewritesHomeVars(t *testing.T) {
	home := t.TempDir()
	env := []string{
		"HOME=/Users/operator",
		"XDG_CONFIG_HOME=/Users/operator/.config",
		"USERPROFILE=C:\\Users\\operator",
		"APPDATA=C:\\Users\\operator\\AppData\\Roaming",
		"UNRELATED=keepme",
	}

	got := applyScopedConfigHome(env, home)

	assertEnv(t, got, "HOME", home)
	assertEnv(t, got, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	assertEnv(t, got, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	assertEnv(t, got, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	assertEnv(t, got, "USERPROFILE", home)
	assertEnv(t, got, "APPDATA", filepath.Join(home, ".config"))
	assertEnv(t, got, "UNRELATED", "keepme")

	for _, kv := range got {
		if strings.HasPrefix(kv, "HOME=/Users/operator") ||
			strings.HasPrefix(kv, "XDG_CONFIG_HOME=/Users/operator") {
			t.Fatalf("operator's real HOME leaked into scoped env: %q", kv)
		}
	}
}

func TestApplyScopedConfigHomeEmptyHomeNoOps(t *testing.T) {
	in := []string{"HOME=/real", "XDG_CONFIG_HOME=/real/.config"}
	got := applyScopedConfigHome(in, "")
	if len(got) != len(in) {
		t.Fatalf("expected env unchanged, got %v", got)
	}
	for i, kv := range got {
		if kv != in[i] {
			t.Fatalf("entry %d changed: got %q want %q", i, kv, in[i])
		}
	}
}

func TestNewScopedConfigHomeCreatesXDGSubdirs(t *testing.T) {
	home, cleanup, err := newScopedConfigHome()
	if err != nil {
		t.Fatalf("newScopedConfigHome: %v", err)
	}
	defer cleanup()

	for _, sub := range []string{".config", ".cache", filepath.Join(".local", "share"), filepath.Join(".local", "state")} {
		path := filepath.Join(home, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s exists but is not a directory", path)
		}
	}
}

func TestScopeSubprocessHomeInstallsAndRestores(t *testing.T) {
	prev := currentSubprocessHome()
	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	scoped := currentSubprocessHome()
	if scoped == "" || scoped == prev {
		t.Fatalf("expected scoped home != prev (%q); got %q", prev, scoped)
	}
	cleanup()
	if got := currentSubprocessHome(); got != prev {
		t.Fatalf("currentSubprocessHome after cleanup = %q, want %q", got, prev)
	}
	if _, err := os.Stat(scoped); !os.IsNotExist(err) {
		t.Fatalf("scoped home %q not removed by cleanup (err=%v)", scoped, err)
	}
}

func TestApplyDefaultSubprocessEnvPreservesExplicitEnv(t *testing.T) {
	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	cmd := &exec.Cmd{Env: []string{"FOO=bar"}}
	applyDefaultSubprocessEnv(cmd)
	if len(cmd.Env) != 1 || cmd.Env[0] != "FOO=bar" {
		t.Fatalf("explicit env was overwritten: %v", cmd.Env)
	}
}

func TestApplyDefaultSubprocessEnvInstallsScopedHome(t *testing.T) {
	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()
	home := currentSubprocessHome()

	cmd := &exec.Cmd{}
	applyDefaultSubprocessEnv(cmd)
	if !containsEnv(cmd.Env, "HOME", home) {
		t.Fatalf("HOME=%s not present in scoped env: %v", home, cmd.Env)
	}
	if !containsEnv(cmd.Env, "XDG_CONFIG_HOME", filepath.Join(home, ".config")) {
		t.Fatalf("XDG_CONFIG_HOME not present in scoped env: %v", cmd.Env)
	}
}

func TestSubprocessEnvPreservesAPICredentialsAfterScopedHome(t *testing.T) {
	t.Setenv("FOO_API_TOKEN", "secret-token")
	t.Setenv("BAR_API_KEY", "secret-key")

	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	env := subprocessEnv()
	assertEnv(t, env, "FOO_API_TOKEN", "secret-token")
	assertEnv(t, env, "BAR_API_KEY", "secret-key")
}

func TestSubprocessEnvScrubsScopedCLIRelocationVars(t *testing.T) {
	poisoned := t.TempDir()
	t.Setenv("PRINTING_PRESS_RICH_HOME", filepath.Join(poisoned, "home"))
	t.Setenv("PRINTING_PRESS_RICH_CONFIG_DIR", filepath.Join(poisoned, "config"))
	t.Setenv("PRINTING_PRESS_RICH_DATA_DIR", filepath.Join(poisoned, "data"))
	t.Setenv("PRINTING_PRESS_RICH_STATE_DIR", filepath.Join(poisoned, "state"))
	t.Setenv("PRINTING_PRESS_RICH_CACHE_DIR", filepath.Join(poisoned, "cache"))
	t.Setenv("PRINTING_PRESS_RICH_API_TOKEN", "secret-token")

	cleanup, err := scopeSubprocessHome("printing-press-rich-pp-cli")
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()
	home := currentSubprocessHome()

	env := subprocessEnv()
	for _, suffix := range naming.PathKindEnvSuffixes() {
		name := "PRINTING_PRESS_RICH_" + suffix
		if envValue(env, name) != "" {
			t.Fatalf("%s leaked into scoped subprocess env: %v", name, env)
		}
	}
	assertEnv(t, env, "PRINTING_PRESS_RICH_API_TOKEN", "secret-token")
	assertEnv(t, env, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
}

// TestSubprocessEnvKeepsUnrelatedRelocationShapedVars pins the scrub's
// scope: only the active CLI's relocation vars and the standard HOME/XDG
// set are stripped. Operator setup that merely looks relocation-shaped
// (JAVA_HOME, CARGO_HOME, ANDROID_HOME, CHROME_USER_DATA_DIR, another
// API's *_DATA_DIR) must survive into verify/dogfood subprocesses, even
// while the target CLI's own relocation vars are removed.
func TestSubprocessEnvKeepsUnrelatedRelocationShapedVars(t *testing.T) {
	unrelated := map[string]string{
		"JAVA_HOME":            filepath.Join(t.TempDir(), "jdk"),
		"CARGO_HOME":           filepath.Join(t.TempDir(), "cargo"),
		"ANDROID_HOME":         filepath.Join(t.TempDir(), "android-sdk"),
		"CHROME_USER_DATA_DIR": filepath.Join(t.TempDir(), "chrome-profile"),
		"OTHERAPI_DATA_DIR":    filepath.Join(t.TempDir(), "otherapi-data"),
		"FOO_CONFIG_DIR":       filepath.Join(t.TempDir(), "foo-config"),
	}
	for name, value := range unrelated {
		t.Setenv(name, value)
	}
	t.Setenv("PRINTING_PRESS_RICH_DATA_DIR", filepath.Join(t.TempDir(), "poisoned-data"))

	cleanup, err := scopeSubprocessHome("printing-press-rich-pp-cli")
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	env := subprocessEnv()
	for name, value := range unrelated {
		assertEnv(t, env, name, value)
	}
	if envValue(env, "PRINTING_PRESS_RICH_DATA_DIR") != "" {
		t.Fatalf("target CLI relocation var leaked into scoped subprocess env: %v", env)
	}
}

// TestSubprocessEnvKeepsRelocationVarsWhenCLINameUnknown documents the
// trade-off of the scoped scrub: with no CLI names installed there is no
// prefix to match, so relocation-shaped vars pass through untouched. The
// production entry points all pass findCLINames(...), so this only arises
// for sessions scoped before the CLI directory exists.
func TestSubprocessEnvKeepsRelocationVarsWhenCLINameUnknown(t *testing.T) {
	fooData := filepath.Join(t.TempDir(), "foo-data")
	t.Setenv("FOO_DATA_DIR", fooData)
	t.Setenv("FOO_API_TOKEN", "secret-token")

	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	env := subprocessEnv()
	assertEnv(t, env, "FOO_DATA_DIR", fooData)
	assertEnv(t, env, "FOO_API_TOKEN", "secret-token")
}

func TestFindCLINamesAndScopedEnvCoverMultipleCommandVariants(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha-pp-cli", "beta-pp-cli"} {
		if err := os.MkdirAll(filepath.Join(dir, "cmd", name), 0o700); err != nil {
			t.Fatalf("mkdir cmd variant: %v", err)
		}
	}
	names := findCLINames(dir)
	if len(names) != 2 || names[0] != "alpha-pp-cli" || names[1] != "beta-pp-cli" {
		t.Fatalf("findCLINames() = %v, want both variants", names)
	}

	t.Setenv("ALPHA_DATA_DIR", filepath.Join(t.TempDir(), "alpha-data"))
	t.Setenv("BETA_DATA_DIR", filepath.Join(t.TempDir(), "beta-data"))
	t.Setenv("ALPHA_API_TOKEN", "alpha-secret")
	t.Setenv("BETA_API_TOKEN", "beta-secret")

	cleanup, err := scopeSubprocessHome(names...)
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	env := subprocessEnv()
	if envValue(env, "ALPHA_DATA_DIR") != "" || envValue(env, "BETA_DATA_DIR") != "" {
		t.Fatalf("multi-variant relocation vars leaked into scoped subprocess env: %v", env)
	}
	assertEnv(t, env, "ALPHA_API_TOKEN", "alpha-secret")
	assertEnv(t, env, "BETA_API_TOKEN", "beta-secret")
}

func TestApplyDefaultSubprocessEnvDogfoodAppendPreservesAPICredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	t.Setenv("FOO_API_TOKEN", "secret-token")

	dir := t.TempDir()
	binPath := writeStubBinary(t, dir, "echo-credential", `printf '%s|%s' "${FOO_API_TOKEN:-}" "${PRINTING_PRESS_DOGFOOD:-}"`)

	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()

	cmd := exec.Command(binPath)
	applyDefaultSubprocessEnv(cmd)
	cmd.Env = append(cmd.Env, dogfoodEnvVar+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run fixture: %v\n%s", err, out)
	}

	if string(out) != "secret-token|1" {
		t.Fatalf("credential or dogfood env missing from subprocess: %q", out)
	}
}

// TestSubprocessWriteDoesNotEscapeScopedHome is the acceptance test for
// issue #1409: a subprocess invoked under a scoped session must not write
// to the parent's HOME-resolved config dir. The parent's HOME is
// redirected to a sentinel tempdir via t.Setenv so the test never
// touches the operator's real ~/.config/, even on a crash before
// cleanup runs.
func TestSubprocessWriteDoesNotEscapeScopedHome(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a probe binary; skipped under -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("HOME-shaped probe is POSIX-only")
	}

	parentHome := t.TempDir()
	t.Setenv("HOME", parentHome)
	sentinelDir := filepath.Join(parentHome, ".config", "cli-printing-press-test")
	sentinelPath := filepath.Join(sentinelDir, "config.toml")
	if err := os.MkdirAll(sentinelDir, 0o700); err != nil {
		t.Fatalf("mkdir sentinel dir: %v", err)
	}
	const sentinelValue = "sentinel-must-survive"
	if err := os.WriteFile(sentinelPath, []byte(sentinelValue), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	probeDir := t.TempDir()
	probeSrc := filepath.Join(probeDir, "main.go")
	if err := os.WriteFile(probeSrc, []byte(probeProgram), 0o600); err != nil {
		t.Fatalf("write probe source: %v", err)
	}
	probeBin := filepath.Join(probeDir, "probe")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", probeBin, probeSrc)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}

	cleanup, err := scopeSubprocessHome()
	if err != nil {
		t.Fatalf("scopeSubprocessHome: %v", err)
	}
	defer cleanup()
	scopedHome := currentSubprocessHome()

	runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer runCancel()
	cmd := exec.CommandContext(runCtx, probeBin)
	applyDefaultSubprocessEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run probe: %v\n%s", err, out)
	}

	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("read sentinel after probe: %v", err)
	}
	if string(got) != sentinelValue {
		t.Fatalf("sentinel clobbered: got %q want %q", string(got), sentinelValue)
	}

	scopedConfig := filepath.Join(scopedHome, ".config", "cli-printing-press-test", "config.toml")
	if _, err := os.Stat(scopedConfig); err != nil {
		t.Fatalf("probe did not write into scoped home (%s): %v", scopedConfig, err)
	}
}

// probeProgram replicates the shape of the generated config-save path:
// resolves HOME, writes to $HOME/.config/<cli>/config.toml.
const probeProgram = `package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir := filepath.Join(home, ".config", "cli-printing-press-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("written-by-probe"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

func assertEnv(t *testing.T, env []string, name, want string) {
	t.Helper()
	if !containsEnv(env, name, want) {
		t.Fatalf("env missing %s=%q; got %v", name, want, env)
	}
}

func containsEnv(env []string, name, want string) bool {
	prefix := name + "="
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			continue
		}
		if strings.TrimPrefix(kv, prefix) == want {
			return true
		}
	}
	return false
}

func envValue(env []string, name string) string {
	prefix := name + "="
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, prefix); ok {
			return value
		}
	}
	return ""
}
