package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCookieAuthCodegen_NamedTokenVsSessionHeader pins the two shapes of
// in:cookie auth produced by client.go.tmpl. Captured browser/native sessions
// carry a multi-pair `name=value; ...` Cookie header that net/http's AddCookie
// rejects (`invalid byte ';' in Cookie.Value`); the spec must be able to
// route those through req.Header.Set instead, while the prior api-key-in-
// cookie behavior stays on req.AddCookie.
func TestCookieAuthCodegen_NamedTokenVsSessionHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		cookieMode       string
		wantSessionLine  bool
		wantAddCookie    bool
		wantRedirectDrop bool // session_header: cross-host hop must Del("Cookie")
	}{
		{
			name:           "default_named_token_uses_AddCookie",
			cookieMode:     "", // default; preserves existing behavior
			wantAddCookie:  true,
		},
		{
			name:             "session_header_uses_HeaderSet",
			cookieMode:       spec.CookieModeSessionHeader,
			wantSessionLine:  true,
			wantRedirectDrop: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:       "cookie",
				Header:     "Cookie",
				In:         "cookie",
				CookieMode: tc.cookieMode,
				EnvVars:    []string{"COOKIE_AUTH_COOKIES"},
			}

			outputDir := filepath.Join(t.TempDir(), tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			client := readGeneratedFile(t, outputDir, "internal", "client", "client.go")

			if tc.wantSessionLine {
				assert.Contains(t, client, `req.Header.Set("Cookie", authHeader)`,
					"session_header mode must set the Cookie header verbatim — AddCookie rejects ';' in Value")
				assert.NotContains(t, client, `req.AddCookie(&http.Cookie{Name: "Cookie"`,
					"session_header mode must not also AddCookie")
			}
			if tc.wantAddCookie {
				assert.Contains(t, client, `req.AddCookie(&http.Cookie{Name: "Cookie"`,
					"named_token (default) mode must keep the AddCookie path")
				assert.NotContains(t, client, `req.Header.Set("Cookie", authHeader)`,
					"named_token mode must not emit the session_header path")
			}
			if tc.wantRedirectDrop {
				assert.Contains(t, client, `req.Header.Del("Cookie")`,
					"session_header mode must drop the Cookie header on cross-host redirects")
			}
		})
	}
}
