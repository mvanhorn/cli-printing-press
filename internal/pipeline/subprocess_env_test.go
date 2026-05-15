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

	for _, sub := range []string{".config", ".cache", filepath.Join(".local", "share")} {
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
