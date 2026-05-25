package pipeline

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain sets private agentcookie test wiring before generated-CLI compile
// tests run. The local replace keeps tests independent of the private
// github.com/mvanhorn/agentcookie repository.
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
