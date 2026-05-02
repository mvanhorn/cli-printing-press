package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeader_EnvVarWinsOverFileToken pins that the generated
// Config.AuthHeader() checks the env-var-backed field BEFORE the
// file-stored AccessToken for bearer_token, cookie, and composed auth
// types — env > config convention (kubectl, gh, aws, gcloud).
func TestAuthHeader_EnvVarWinsOverFileToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		envVar   string
	}{
		{"bearer_token", "bearer_token", "BEARER_AUTH_TEST_TOKEN"},
		{"cookie", "cookie", "COOKIE_AUTH_TEST_TOKEN"},
		{"composed", "composed", "COMPOSED_AUTH_TEST_TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name + "-precedence")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tc.authType,
				Header:  "Authorization",
				EnvVars: []string{tc.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tc.name+"-precedence-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			envCheck := "if c." + resolveEnvVarField(tc.envVar) + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			require.Contains(t, content, envCheck,
				"AuthHeader must check the env-var field for type %q", tc.authType)
			require.Contains(t, content, tokenCheck,
				"AuthHeader must still check AccessToken for type %q", tc.authType)

			authHeaderStart := strings.Index(content, "func (c *Config) AuthHeader() string {")
			require.NotEqual(t, -1, authHeaderStart, "AuthHeader function must be emitted")
			body := content[authHeaderStart:]
			// Bound the search to AuthHeader's body. Skip the first byte so
			// the "next func" lookup finds the func AFTER AuthHeader, not
			// AuthHeader's own opener.
			if next := strings.Index(body[1:], "\nfunc "); next != -1 {
				body = body[:next+1]
			}

			envIdx := strings.Index(body, envCheck)
			tokenIdx := strings.Index(body, tokenCheck)
			require.NotEqual(t, -1, envIdx)
			require.NotEqual(t, -1, tokenIdx)
			assert.Less(t, envIdx, tokenIdx,
				"env-var check must appear BEFORE AccessToken check in AuthHeader for type %q", tc.authType)
		})
	}
}
