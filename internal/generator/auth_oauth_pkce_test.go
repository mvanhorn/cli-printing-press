package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOAuthLoginUsesPKCEWhenNoClientSecret(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-pkce-login")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://accounts.example.com/oauth/authorize",
		TokenURL:         "https://accounts.example.com/oauth/token",
		KeyURL:           "https://console.example.com/oauth",
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-pkce-login-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)

	// Authorize request carries an S256 challenge on the secretless path.
	require.Contains(t, auth, "codeVerifier, err = generatePKCEVerifier()")
	require.Contains(t, auth, `params.Set("code_challenge", pkceCodeChallengeS256(codeVerifier))`)
	require.Contains(t, auth, `params.Set("code_challenge_method", "S256")`)

	// Token exchange authenticates with exactly one of client_secret or
	// code_verifier (mutual exclusivity is validated behaviorally below);
	// the bare authorization_code grant is gone.
	require.Contains(t, auth, `tokenParams.Set("client_secret", clientSecret)`)
	require.Contains(t, auth, `tokenParams.Set("code_verifier", codeVerifier)`)

	// RFC 8252 loopback redirect: IP literal, never "localhost".
	require.Contains(t, auth, `redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback"`)
	require.NotContains(t, auth, `http://localhost:%d/callback`)

	binPath := filepath.Join(outputDir, "oauth-pkce-login-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binPath, "./cmd/oauth-pkce-login-pp-cli")

	// Under verify-env the command prints the authorize URL it would launch;
	// use that to assert live flag→params behavior without opening a browser
	// or binding assumptions about the callback port.
	runVerifyLogin := func(extraArgs ...string) string {
		args := append([]string{"--config", filepath.Join(t.TempDir(), "config.toml"), "login", "--client-id", "test-client"}, extraArgs...)
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "verify-env login failed: %s", string(out))
		return string(out)
	}

	secretless := runVerifyLogin()
	require.Contains(t, secretless, "would launch:")
	require.Contains(t, secretless, "code_challenge=")
	require.Contains(t, secretless, "code_challenge_method=S256")
	require.Contains(t, secretless, "127.0.0.1%3A8085%2Fcallback")
	require.NotContains(t, secretless, "localhost")

	withSecret := runVerifyLogin("--client-secret", "test-secret")
	require.Contains(t, withSecret, "would launch:")
	require.NotContains(t, withSecret, "code_challenge")

	const runtimeTest = `package cli

import (
	"strings"
	"testing"
)

// Vector from RFC 7636 appendix B.
func TestPKCECodeChallengeS256MatchesRFC7636Vector(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceCodeChallengeS256(verifier); got != want {
		t.Fatalf("pkceCodeChallengeS256() = %q, want %q", got, want)
	}
}

func TestGeneratePKCEVerifierShapeAndUniqueness(t *testing.T) {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	first, err := generatePKCEVerifier()
	if err != nil {
		t.Fatalf("generatePKCEVerifier() error = %v", err)
	}
	if len(first) != 43 {
		t.Fatalf("verifier length = %d, want 43", len(first))
	}
	for _, r := range first {
		if !strings.ContainsRune(unreserved, r) {
			t.Fatalf("verifier contains %q, outside RFC 7636 unreserved set", r)
		}
	}
	second, err := generatePKCEVerifier()
	if err != nil {
		t.Fatalf("generatePKCEVerifier() second call error = %v", err)
	}
	if first == second {
		t.Fatalf("two verifiers are identical: %q", first)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "oauth_pkce_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "-race", "./internal/cli", "-run", "Test(PKCECodeChallengeS256|GeneratePKCEVerifier)")

	require.Contains(t, auth, "http.NewRequestWithContext(cmd.Context(), http.MethodPost, tokenURL", "token exchange should be context-cancellable")
	require.False(t, strings.Contains(auth, "http.PostForm(tokenURL"), "token exchange should use a client with its own timeout, not the shared default client")
}
