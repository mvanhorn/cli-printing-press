// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestGenerateCookieAuthEmitsWebSocketEval verifies that cookie-auth and
// composed-auth CLIs ship a real Runtime.evaluate over the DevTools Protocol
// WebSocket in their extractViaCDP fallback, not the historical "WebSocket
// eval not yet implemented" stub. This is the runtime side of the PR #1645
// follow-up tracked in issue #1867.
func TestGenerateCookieAuthEmitsWebSocketEval(t *testing.T) {
	t.Parallel()

	for _, authType := range []string{"cookie", "composed"} {
		t.Run(authType, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("ws-eval-" + authType)
			apiSpec.Auth = spec.AuthConfig{
				Type:    authType,
				Header:  "Authorization",
				EnvVars: []string{"WS_EVAL_TOKEN"},
			}

			outputDir := filepath.Join(t.TempDir(), "ws-eval-"+authType+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")

			// The historical stub message must be gone.
			assert.NotContains(t, authGo, "WebSocket eval not yet implemented",
				"extractViaCDP must no longer return the not-implemented stub")

			// The WS eval helper and Runtime.evaluate request body must be present.
			assert.Contains(t, authGo, "evalDocumentCookieViaCDP(",
				"extractViaCDP should call the WebSocket eval helper")
			assert.Contains(t, authGo, "Runtime.evaluate",
				"the helper should issue a CDP Runtime.evaluate")
			assert.Contains(t, authGo, "document.cookie",
				"the helper should evaluate document.cookie in the page context")

			// The gorilla/websocket import is the only new dependency.
			assert.Contains(t, authGo, "github.com/gorilla/websocket",
				"auth.go should import gorilla/websocket for the CDP WS eval")

			// Bounded work: a hard deadline keeps a stuck CDP socket from
			// hanging the auth flow. Match the helper's SetReadDeadline /
			// SetWriteDeadline pattern.
			assert.Contains(t, authGo, "SetReadDeadline",
				"the helper must bound the WS read so a stuck connection cannot hang auth")
			assert.Contains(t, authGo, "SetWriteDeadline",
				"the helper must bound the WS write so a stuck connection cannot hang auth")

			// go.mod must include the gorilla/websocket dependency.
			goMod := readGeneratedFile(t, outputDir, "go.mod")
			assert.Contains(t, goMod, "github.com/gorilla/websocket",
				"go.mod should require gorilla/websocket for cookie/composed auth CLIs")
		})
	}
}

// TestGenerateNonCookieAuthSkipsWebSocketDep confirms the gorilla/websocket
// dependency is gated on cookie/composed auth and does not leak into the
// generated module for plain api_key or bearer_token specs.
func TestGenerateNonCookieAuthSkipsWebSocketDep(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-cookie-no-ws")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"NO_COOKIE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "no-cookie-no-ws-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.NotContains(t, goMod, "github.com/gorilla/websocket",
		"go.mod for non-cookie auth must not pull gorilla/websocket")

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.NotContains(t, authGo, "evalDocumentCookieViaCDP",
		"non-cookie auth must not emit the CDP WS eval helper")
}
