package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestClientCheckRedirectDeletesHeaderOnCrossHost(t *testing.T) {
	t.Parallel()

	t.Run("api_key custom header deletes cross-host", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-cross-host-apikey")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Api-Key",
			EnvVars: []string{"REDIRECT_CROSS_HOST_APIKEY"},
		}

		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, `if req.URL.Host == via[0].URL.Host {`)
		require.Contains(t, closure, `req.Header.Set("X-Api-Key", h)`)
		require.Contains(t, closure, `} else {`)
		require.Contains(t, closure, `req.Header.Del("X-Api-Key")`)
	})

	t.Run("session handshake header deletes cross-host", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-cross-host-session-header")
		apiSpec.Auth = spec.AuthConfig{
			Type:           "session_handshake",
			TokenParamIn:   "header",
			TokenParamName: "X-Kit-Api-Key",
			EnvVars:        []string{"REDIRECT_CROSS_HOST_SESSION_HEADER"},
		}

		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, `if req.URL.Host == via[0].URL.Host {`)
		require.Contains(t, closure, `req.Header.Set("X-Kit-Api-Key", h)`)
		require.Contains(t, closure, `} else {`)
		require.Contains(t, closure, `req.Header.Del("X-Kit-Api-Key")`)
	})
}
