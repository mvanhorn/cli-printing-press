package generator

import (
	"os"
	"testing"
)

// TestMain sets GOPRIVATE=github.com/mvanhorn/* before any test runs.
// Every test in this package that compiles a generated CLI (via runGo*,
// exec.Command("go", ...), or t.Helper-style fixtures) inherits the env.
//
// Without this, generated CLIs that import pkg/agentcookiesecret fail
// sumdb verification against pre-release agentcookie tags that the
// public proxy has not yet indexed. The setting is harmless when the
// agentcookie dep is absent (cookie-only or no-auth generated CLIs).
func TestMain(m *testing.M) {
	if os.Getenv("GOPRIVATE") == "" {
		_ = os.Setenv("GOPRIVATE", "github.com/mvanhorn/*")
	}
	os.Exit(m.Run())
}
