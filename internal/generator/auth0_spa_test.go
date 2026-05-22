// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestGenerateAuth0SPAEmitsCDPLoginCmd verifies that the auth_browser
// template emits the `--auth0-spa` CDP code path when Auth.Subtype is
// auth0_spa_in_memory. Covers the WU-3b acceptance criterion:
//
//	spec carries auth.subtype: auth0_spa_in_memory AND
//	auth login --chrome --auth0-spa invokes the CDP path.
func TestGenerateAuth0SPAEmitsCDPLoginCmd(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("factor75")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Subtype: spec.AuthSubtypeAuth0SPAInMemory,
		Header:  "Authorization",
		EnvVars: []string{"FACTOR75_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "factor75-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")

	// CDP code path must be emitted.
	assert.Contains(t, authGo, "captureAuth0SPABearer(",
		"auth.go should define the CDP capture helper for the auth0_spa_in_memory subtype")
	assert.Contains(t, authGo, "chromedp.NewRemoteAllocator",
		"auth.go should attach to a running Chrome via the remote allocator")
	assert.Contains(t, authGo, "fetch.Enable()",
		"auth.go should enable the Fetch domain for outbound interception")
	assert.Contains(t, authGo, "EventRequestPaused",
		"auth.go should listen for paused requests to read the Authorization header")

	// --auth0-spa flag must be wired.
	assert.Contains(t, authGo, `"auth0-spa"`,
		"auth.go should register the --auth0-spa flag on `auth login`")
	assert.Contains(t, authGo, `"chrome"`,
		"auth.go should still register the --chrome flag")

	// Side-effect-command floor: verify-env + dogfood-env short-circuits.
	assert.Contains(t, authGo, "cliutil.IsVerifyEnv()",
		"CDP login must short-circuit under PRINTING_PRESS_VERIFY=1")
	assert.Contains(t, authGo, "cliutil.IsDogfoodEnv()",
		"CDP login must short-circuit under PRINTING_PRESS_DOGFOOD=1")

	// JWT shape gate before saving.
	assert.Contains(t, authGo, "cliutil.LooksLikeJWT(",
		"captured Authorization must be JWT-shape-validated before SaveTokens")

	// go.mod must include chromedp dependencies for this subtype.
	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, goMod, "github.com/chromedp/chromedp",
		"go.mod should require chromedp for the auth0_spa_in_memory subtype")
	assert.Contains(t, goMod, "github.com/chromedp/cdproto",
		"go.mod should require cdproto for the fetch.* event types")

	// Build check: confirm the emitted CLI compiles and `auth login
	// --chrome --auth0-spa --help` exits 0. Pulls chromedp via `go mod
	// tidy`, then builds the binary and probes the help surface.
	if testing.Short() {
		t.Skip("skipping build check in -short mode (downloads chromedp)")
	}
	runGoCommand(t, outputDir, "mod", "tidy")
	binPath := filepath.Join(outputDir, "factor75-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binPath, "./cmd/factor75-pp-cli")
	out, err := exec.Command(binPath, "auth", "login", "--chrome", "--auth0-spa", "--help").CombinedOutput()
	require.NoError(t, err, "`auth login --chrome --auth0-spa --help` failed: %s", string(out))
	assert.Contains(t, string(out), "--auth0-spa",
		"`--help` should list the --auth0-spa flag")
	assert.Contains(t, string(out), "CDP",
		"`--help` should mention the CDP capture path so users understand what --auth0-spa does")
}

// TestGenerateBearerTokenWithoutSubtypeUsesSimpleTemplate ensures that a
// plain bearer_token spec (no Auth0 SPA subtype) still routes to the
// auth_simple template — the CDP path is opt-in by detection, not the
// default for bearer_token auth.
func TestGenerateBearerTokenWithoutSubtypeUsesSimpleTemplate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("vanilla")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"VANILLA_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "vanilla-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.NotContains(t, authGo, "captureAuth0SPABearer",
		"plain bearer_token spec must NOT emit the CDP helper")
	assert.NotContains(t, authGo, "chromedp.NewRemoteAllocator",
		"plain bearer_token spec must NOT import or call chromedp")
	assert.NotContains(t, authGo, "--auth0-spa",
		"plain bearer_token spec must NOT register the --auth0-spa flag")

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.NotContains(t, goMod, "github.com/chromedp",
		"go.mod for a plain bearer_token spec must not pull in chromedp")
}
