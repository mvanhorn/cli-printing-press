package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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
	"XDG_STATE_HOME",
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
	for _, sub := range []string{".config", ".cache", filepath.Join(".local", "share"), filepath.Join(".local", "state")} {
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
// Optional cliRelocationEnvVars drop that CLI's relocation env vars so operator-level
// <PREFIX>_HOME / <PREFIX>_<KIND>_DIR values cannot leak through verify
// and dogfood subprocesses. Stripping is deliberately scoped to those
// explicit names plus the standard HOME/XDG set: a suffix-based sweep
// would also drop unrelated operator setup such as JAVA_HOME, CARGO_HOME,
// or API-specific *_DATA_DIR variables that subprocesses legitimately need.
// homeDir == "" returns env unchanged.
func applyScopedConfigHome(env []string, homeDir string, cliRelocationEnvVars ...string) []string {
	if homeDir == "" {
		return env
	}
	overrides := scopedConfigHomeOverrides(homeDir)
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		if isScopedConfigHomeEntry(kv) || isScopedCLIEnvEntry(kv, cliRelocationEnvVars) {
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
	stateDir := filepath.Join(homeDir, ".local", "state")
	return map[string]string{
		"HOME":            homeDir,
		"XDG_CONFIG_HOME": configDir,
		"XDG_CACHE_HOME":  cacheDir,
		"XDG_DATA_HOME":   dataDir,
		"XDG_STATE_HOME":  stateDir,
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

func scopedCLIRelocationEnvVars(cliNames ...string) []string {
	seen := map[string]bool{}
	var names []string
	for _, cliName := range cliNames {
		slug := strings.TrimSpace(naming.TrimCLISuffix(cliName))
		if slug == "" {
			continue
		}
		prefix := naming.EnvPrefix(slug)
		for _, suffix := range naming.PathKindEnvSuffixes() {
			name := prefix + "_" + suffix
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func isScopedCLIEnvEntry(kv string, names []string) bool {
	for _, name := range names {
		if strings.HasPrefix(kv, name+"=") {
			return true
		}
	}
	return false
}

// scopedHomeDir holds the active scoped home for child invocations of
// the printed CLI. Production entry points run sequentially, but
// pipeline tests use t.Parallel(), so the mutex protects against the
// data race go test -race detects without it.
var (
	scopedHomeDirMu       sync.RWMutex
	scopedHomeDir         string
	scopedHomeCLIEnvNames []string
)

// currentSubprocessHome returns the active scoped home or "" if none.
func currentSubprocessHome() string {
	scopedHomeDirMu.RLock()
	defer scopedHomeDirMu.RUnlock()
	return scopedHomeDir
}

func currentSubprocessCLIEnvNames() []string {
	scopedHomeDirMu.RLock()
	defer scopedHomeDirMu.RUnlock()
	return append([]string(nil), scopedHomeCLIEnvNames...)
}

// installScopedSubprocessHome installs homeDir as the active scoped
// home and returns a restore function the caller defers.
func installScopedSubprocessHome(homeDir string, cliNames ...string) func() {
	scopedHomeDirMu.Lock()
	prev := scopedHomeDir
	prevCLIEnvNames := append([]string(nil), scopedHomeCLIEnvNames...)
	scopedHomeDir = homeDir
	scopedHomeCLIEnvNames = scopedCLIRelocationEnvVars(cliNames...)
	scopedHomeDirMu.Unlock()
	return func() {
		scopedHomeDirMu.Lock()
		scopedHomeDir = prev
		scopedHomeCLIEnvNames = prevCLIEnvNames
		scopedHomeDirMu.Unlock()
	}
}

// scopeSubprocessHome installs a fresh scoped home for the current
// entry point. Callers defer the returned cleanup to restore the
// previous home and remove the tempdir. Returning the error rather
// than silently falling back is deliberate: the whole fix exists to
// prevent data corruption, and a torn scope would leave the bug
// exposed.
func scopeSubprocessHome(cliNames ...string) (func(), error) {
	homeDir, removeHome, err := newScopedConfigHome()
	if err != nil {
		return func() {}, err
	}
	restore := installScopedSubprocessHome(homeDir, cliNames...)
	return func() {
		restore()
		removeHome()
	}, nil
}

// subprocessEnv returns os.Environ() with the active scoped home
// overlaid, or os.Environ() unchanged when no session is active.
func subprocessEnv() []string {
	return applyScopedConfigHome(os.Environ(), currentSubprocessHome(), currentSubprocessCLIEnvNames()...)
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
