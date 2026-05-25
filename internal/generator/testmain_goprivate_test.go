package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain sets private agentcookie test wiring before any test runs.
// Every test in this package that compiles a generated CLI (via runGo*,
// exec.Command("go", ...), or t.Helper-style fixtures) inherits the env.
//
// The local replace keeps generated-CLI compile tests independent of the
// private github.com/mvanhorn/agentcookie repository. The setting is harmless
// when the agentcookie dep is absent (cookie-only or no-auth generated CLIs).
func TestMain(m *testing.M) {
	if os.Getenv("GOPRIVATE") == "" {
		_ = os.Setenv("GOPRIVATE", "github.com/mvanhorn/*")
	}
	setAgentcookieReplace()
	os.Exit(m.Run())
}

func setAgentcookieReplace() {
	if os.Getenv("PRINTING_PRESS_AGENTCOOKIE_REPLACE") != "" {
		return
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	_ = os.Setenv("PRINTING_PRESS_AGENTCOOKIE_REPLACE", filepath.Join(repoRoot, "internal", "testdata", "agentcookie"))
}
