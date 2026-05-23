package pipeline

import (
	"os"
	"testing"
)

// TestMain sets GOPRIVATE=github.com/mvanhorn/* so subprocess go builds
// against generated CLIs can resolve pkg/agentcookiesecret on pre-release
// agentcookie tags that the public sumdb has not yet indexed. The setting
// is harmless when the agentcookie dep is absent (cookie-only or no-auth
// generated CLIs).
func TestMain(m *testing.M) {
	if os.Getenv("GOPRIVATE") == "" {
		_ = os.Setenv("GOPRIVATE", "github.com/mvanhorn/*")
	}
	os.Exit(m.Run())
}
