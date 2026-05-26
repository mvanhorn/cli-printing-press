package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// privateModulePrefixes lists module paths that must never appear as a
// require in any printed CLI's go.mod. The Printing Press emits CLIs that
// users `go install`; a require on a private module breaks installation
// for anyone without read access to that module.
//
// Expand this list whenever a new internal-by-default module is created.
// See AGENTS.md "No private-module requires in printed CLIs" for the rule
// and rationale.
var privateModulePrefixes = []string{
	// github.com/mvanhorn/agentcookie is a private project. PR #1972
	// introduced a require here and broke `go install` for every user
	// outside Matt's account; this prefix is the regression guard.
	"github.com/mvanhorn/agentcookie",

	// The generator itself must never appear as a dep of a generated CLI.
	// If it does, the printed CLI depends on a fast-moving repo whose
	// public install path may not match what the generated go.mod expects.
	"github.com/mvanhorn/cli-printing-press",
}

// TestNoPrivateRequiresInGeneratedGoMod regenerates a handful of CLIs
// covering each auth-type fork and asserts none of them carry a require
// on a private module. The test reads the emitted go.mod as a string;
// no network or go mod download is needed.
//
// Add a new sub-test whenever a new auth-type fork is added to the
// generator (Subtype variant, new bearer flavor, etc.) so the guard
// covers it.
func TestNoPrivateRequiresInGeneratedGoMod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth spec.AuthConfig
	}{
		{
			name: "bearer_token",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"BEARER_TOKEN"},
			},
		},
		{
			name: "api_key_header",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "X-API-Key",
				EnvVars: []string{"API_KEY"},
			},
		},
		{
			name: "basic_auth",
			auth: spec.AuthConfig{
				Type:    "basic",
				EnvVars: []string{"BASIC_USERNAME", "BASIC_PASSWORD"},
			},
		},
		{
			name: "oauth2_authorization_code",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				AuthorizationURL: "https://example.com/oauth/authorize",
				TokenURL:         "https://example.com/oauth/token",
				EnvVars:          []string{"OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET"},
			},
		},
		{
			name: "cookie_only",
			auth: spec.AuthConfig{
				Type:         "cookie",
				CookieDomain: "example.com",
			},
		},
		{
			name: "no_auth",
			auth: spec.AuthConfig{Type: "none"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			goMod := readGeneratedFile(t, outputDir, "go.mod")
			for _, prefix := range privateModulePrefixes {
				assert.False(t, strings.Contains(goMod, prefix),
					"go.mod for auth=%q contains private module prefix %q; printed CLIs must not depend on internal-by-default modules. Update privateModulePrefixes if this prefix is now public.",
					tt.name, prefix)
			}
		})
	}
}
