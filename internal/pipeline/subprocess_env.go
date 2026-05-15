package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Subprocess HOME / XDG_CONFIG_HOME scoping for verify, dogfood, live-dogfood,
// live-check, and workflow-verify runs. Without it, the printed CLI's
// config-save paths (auth login, set-token, doctor repair, etc.) would
// persist the mock BASE_URL / token values that verify injects to the
// operator's real ~/.config/<api>-pp-cli/config.<format>.

// configHomeEnvVars is the closed set of env vars Go's os.UserHomeDir,
// os.UserConfigDir, and os.UserCacheDir consult (Unix + Windows). Rewriting
// all of them together gives a printed CLI no path back to the operator's
// real home regardless of which os.* helper its generated config code uses.
var configHomeEnvVars = []string{
	"HOME",
	"XDG_CONFIG_HOME",
	"XDG_CACHE_HOME",
	"XDG_DATA_HOME",
	"USERPROFILE",
	"APPDATA",
	"LOCALAPPDATA",
}

// newScopedConfigHome creates an ephemeral home root with the XDG
// subtrees pre-created. Returns the path, a cleanup function (safe to
// call once), and any creation error.
func newScopedConfigHome() (string, func(), error) {
	homeDir, err := os.MkdirTemp("", "printing-press-subprocess-")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating subprocess home: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(homeDir) }
	for _, sub := range []string{".config", ".cache", filepath.Join(".local", "share")} {
		if err := os.MkdirAll(filepath.Join(homeDir, sub), 0o700); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("creating subprocess %s: %w", sub, err)
		}
	}
	return homeDir, cleanup, nil
}

// applyScopedConfigHome overlays the home-related env vars in env with
// values rooted at homeDir so a receiving subprocess resolves
// os.UserConfigDir / os.UserHomeDir under homeDir rather than the
// parent's $HOME. Existing entries for the rewritten vars are dropped.
// homeDir == "" returns env unchanged.
func applyScopedConfigHome(env []string, homeDir string) []string {
	if homeDir == "" {
		return env
	}
	overrides := scopedConfigHomeOverrides(homeDir)
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		if isScopedConfigHomeEntry(kv) {
			continue
		}
		out = append(out, kv)
	}
	for _, name := range configHomeEnvVars {
		out = append(out, name+"="+overrides[name])
	}
	return out
}

// scopedConfigHomeOverrides maps each configHomeEnvVar to a path under
// homeDir. APPDATA / XDG_CONFIG_HOME share .config so Windows and Linux
// CLIs land in the same scoped dir; LOCALAPPDATA / XDG_CACHE_HOME share
// .cache.
func scopedConfigHomeOverrides(homeDir string) map[string]string {
	configDir := filepath.Join(homeDir, ".config")
	cacheDir := filepath.Join(homeDir, ".cache")
	dataDir := filepath.Join(homeDir, ".local", "share")
	return map[string]string{
		"HOME":            homeDir,
		"XDG_CONFIG_HOME": configDir,
		"XDG_CACHE_HOME":  cacheDir,
		"XDG_DATA_HOME":   dataDir,
		"USERPROFILE":     homeDir,
		"APPDATA":         configDir,
		"LOCALAPPDATA":    cacheDir,
	}
}

func isScopedConfigHomeEntry(kv string) bool {
	for _, name := range configHomeEnvVars {
		if strings.HasPrefix(kv, name+"=") {
			return true
		}
	}
	return false
}

// scopedHomeDir holds the active scoped home for child invocations of
// the printed CLI. Verify, dogfood, and workflow-verify are top-level
// CLI commands so they don't run concurrently in the same process; no
// locking needed.
var scopedHomeDir string

// currentSubprocessHome returns the active scoped home or "" if none.
func currentSubprocessHome() string { return scopedHomeDir }

// installScopedSubprocessHome installs homeDir as the active scoped
// home and returns a restore function the caller defers.
func installScopedSubprocessHome(homeDir string) func() {
	prev := scopedHomeDir
	scopedHomeDir = homeDir
	return func() { scopedHomeDir = prev }
}

// scopeSubprocessHome installs a fresh scoped home for the current
// entry point. Callers defer the returned cleanup to restore the
// previous home and remove the tempdir. Returning the error rather
// than silently falling back is deliberate: the whole fix exists to
// prevent data corruption, and a torn scope would leave the bug
// exposed.
func scopeSubprocessHome() (func(), error) {
	homeDir, removeHome, err := newScopedConfigHome()
	if err != nil {
		return func() {}, err
	}
	restore := installScopedSubprocessHome(homeDir)
	return func() {
		restore()
		removeHome()
	}, nil
}

// subprocessEnv returns os.Environ() with the active scoped home
// overlaid, or os.Environ() unchanged when no session is active.
func subprocessEnv() []string {
	return applyScopedConfigHome(os.Environ(), currentSubprocessHome())
}

// applyDefaultSubprocessEnv installs subprocessEnv() on cmd if the
// caller hasn't already chosen cmd.Env. Every exec site that runs the
// printed CLI calls this so the child inherits the scoped HOME.
func applyDefaultSubprocessEnv(cmd *exec.Cmd) {
	if cmd == nil || cmd.Env != nil {
		return
	}
	cmd.Env = subprocessEnv()
}
