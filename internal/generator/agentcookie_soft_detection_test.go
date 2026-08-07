package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The soft agentcookie integration must satisfy three properties at template
// expansion time:
//
//  1. No printed CLI carries a Go-level dependency on github.com/mvanhorn/agentcookie.
//     The agentcookie repo is private; a require would break `go install` for
//     anyone without read access (the regression that #1972 introduced and
//     this skill removes).
//  2. CLIs with non-cookie auth still detect agentcookie's presence via a
//     stdlib-only on-disk marker, so doctor and auth-status can report the
//     bus state.
//  3. Cookie-only CLIs skip the marker detection entirely — they have no
//     env-var credentials for agentcookie to manage.
//
// This test pins all three. If any future template edit reintroduces an
// agentcookie import or removes the marker-detection block, this test fails.
func TestAgentcookieSoftDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		auth            spec.AuthConfig
		wantMarkerBlock bool
	}{
		{
			name: "non_cookie_auth_gets_marker_detection",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"MYAPI_TOKEN"},
			},
			wantMarkerBlock: true,
		},
		{
			name: "cookie_only_auth_skips_marker_detection",
			auth: spec.AuthConfig{
				Type:         "cookie",
				CookieDomain: "example.com",
			},
			wantMarkerBlock: false,
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
			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")

			// Property 1: no Go-level agentcookie dependency, ever.
			assert.NotContains(t, goMod, "github.com/mvanhorn/agentcookie",
				"go.mod must not require github.com/mvanhorn/agentcookie — it is a private repo and would break `go install`")
			assert.NotContains(t, configSrc, "github.com/mvanhorn/agentcookie",
				"config.go must not import github.com/mvanhorn/agentcookie")
			assert.NotContains(t, doctorSrc, "github.com/mvanhorn/agentcookie",
				"doctor.go must not import github.com/mvanhorn/agentcookie")

			// Property 2 & 3: marker detection appears for non-cookie auth,
			// not for cookie-only auth.
			if tt.wantMarkerBlock {
				assert.Contains(t, configSrc, ".agentcookie-managed",
					"non-cookie-auth CLIs must check for the agentcookie marker file")
				assert.Contains(t, configSrc, `cfg.AuthSource = "agentcookie"`,
					"non-cookie-auth CLIs must upgrade AuthSource when the marker is present")
				assert.Contains(t, doctorSrc, `report["agentcookie"]`,
					"non-cookie-auth CLIs must surface agentcookie status in doctor")
			} else {
				assert.NotContains(t, configSrc, ".agentcookie-managed",
					"cookie-only CLIs must not include marker detection")
				assert.NotContains(t, doctorSrc, `report["agentcookie"]`,
					"cookie-only CLIs must not surface agentcookie status in doctor")
			}
		})
	}
}
