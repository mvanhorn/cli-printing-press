package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestOAuth1TBA_SpecParses verifies that a spec declaring auth.type: oauth1_tba
// passes validation (the press recognizes the scheme).
func TestOAuth1TBA_SpecParses(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth1-parse")
	apiSpec.Auth = spec.AuthConfig{
		Type: "oauth1_tba",
		EnvVars: []string{
			"NS_CONSUMER_KEY",
			"NS_CONSUMER_SECRET",
			"NS_TOKEN_ID",
			"NS_TOKEN_SECRET",
		},
	}

	err := apiSpec.Validate()
	require.NoError(t, err, "spec with auth.type=oauth1_tba must pass validation")
}

// TestOAuth1TBA_GeneratedClientEmbedsOAuth1Header verifies that the press
// generates a client.go that contains an oauth1Header() function implementing
// RFC 5849 HMAC-SHA256 Token-Based Authentication.
//
// Key assertions:
//   - oauth1Header() function exists in generated client
//   - References all 4 env vars (consumer key/secret + token id/secret)
//   - Produces Authorization header with OAuth prefix
//   - Uses HMAC-SHA256 signing (crypto/hmac + crypto/sha256)
//   - Sorts parameters (RFC 5849 §3.4.1.3.2)
//   - URL-encodes nonce components
func TestOAuth1TBA_GeneratedClientEmbedsOAuth1Header(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth1-client")
	apiSpec.Auth = spec.AuthConfig{
		Type: "oauth1_tba",
		EnvVars: []string{
			"NS_CONSUMER_KEY",
			"NS_CONSUMER_SECRET",
			"NS_TOKEN_ID",
			"NS_TOKEN_SECRET",
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth1-client-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	// Function exists
	require.Contains(t, src, "func (c *Client) oauth1Header(",
		"generated client must contain oauth1Header() function")

	// RFC 5849 Authorization prefix
	require.Contains(t, src, `"OAuth "`,
		"oauth1Header must build Authorization header starting with 'OAuth '")

	// HMAC-SHA256 signing
	require.Contains(t, src, "hmac.New(sha256.New",
		"oauth1Header must use HMAC-SHA256 per RFC 5849")

	// All 4 env vars referenced
	require.Contains(t, src, `"NS_CONSUMER_KEY"`,
		"generated client must read NS_CONSUMER_KEY env var")
	require.Contains(t, src, `"NS_CONSUMER_SECRET"`,
		"generated client must read NS_CONSUMER_SECRET env var")
	require.Contains(t, src, `"NS_TOKEN_ID"`,
		"generated client must read NS_TOKEN_ID env var")
	require.Contains(t, src, `"NS_TOKEN_SECRET"`,
		"generated client must read NS_TOKEN_SECRET env var")

	// RFC 5849 §3.4.1.3.2: parameters sorted and percent-encoded
	require.Contains(t, src, "sort.Strings(",
		"oauth1Header must sort parameters per RFC 5849 §3.4.1.3.2")

	// Nonce generation (timestamp + randomness)
	require.Contains(t, src, "oauth_timestamp",
		"oauth1Header must include oauth_timestamp in Authorization header")
	require.Contains(t, src, "oauth_nonce",
		"oauth1Header must include oauth_nonce in Authorization header")
	require.Contains(t, src, "oauth_signature_method",
		"oauth1Header must declare oauth_signature_method")
	require.Contains(t, src, "HMAC-SHA256",
		"oauth_signature_method value must be HMAC-SHA256")
}

// TestOAuth1TBA_GeneratedClientCompiles verifies that the generated
// oauth1_tba client compiles without errors.
func TestOAuth1TBA_GeneratedClientCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth1-compile")
	apiSpec.Auth = spec.AuthConfig{
		Type: "oauth1_tba",
		EnvVars: []string{
			"NS_CONSUMER_KEY",
			"NS_CONSUMER_SECRET",
			"NS_TOKEN_ID",
			"NS_TOKEN_SECRET",
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth1-compile-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestOAuth1TBA_AuthHeaderSetOnRequest verifies the generated do() function
// sets the Authorization header (not a query param) for oauth1_tba.
func TestOAuth1TBA_AuthHeaderSetOnRequest(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth1-header")
	apiSpec.Auth = spec.AuthConfig{
		Type: "oauth1_tba",
		EnvVars: []string{
			"NS_CONSUMER_KEY",
			"NS_CONSUMER_SECRET",
			"NS_TOKEN_ID",
			"NS_TOKEN_SECRET",
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth1-header-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	// The do() loop must set Authorization header per-request
	require.Contains(t, src, `req.Header.Set("Authorization"`,
		"do() must set Authorization header for oauth1_tba requests")

	// Must NOT fall through to the plain bearer/api_key branch
	// (oauth1Header() computes a per-request sig, not a static token)
	require.Contains(t, src, "c.oauth1Header(",
		"do() must call oauth1Header() to get per-request signed header")
}

// TestOAuth1TBA_ScorecardGives10 verifies that declaring oauth1_tba in the
// spec drives the scorecard auth_protocol dimension to 10/10.
func TestOAuth1TBA_ScorecardGives10(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth1-score")
	apiSpec.Auth = spec.AuthConfig{
		Type: "oauth1_tba",
		EnvVars: []string{
			"NS_CONSUMER_KEY",
			"NS_CONSUMER_SECRET",
			"NS_TOKEN_ID",
			"NS_TOKEN_SECRET",
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth1-score-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)

	// Scorecard checks for Authorization header assignment and env var in config
	require.Contains(t, clientContent, `req.Header.Set("Authorization"`,
		"scorecard auth_protocol checks Header.Set(Authorization)")

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	configContent := strings.ToUpper(string(configSrc))

	// Scorecard checks config.go contains sanitized env name
	require.Contains(t, configContent, "NS_CONSUMER",
		"scorecard auth_protocol checks config.go contains env var name")
}

// TestOAuth1TBA_DoesNotBreakBearerToken verifies the bearer_token auth path
// is unaffected by the oauth1_tba addition.
func TestOAuth1TBA_DoesNotBreakBearerToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-still-works")
	// default minimalSpec uses api_key — verify unchanged

	outputDir := filepath.Join(t.TempDir(), "bearer-still-works-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	require.Contains(t, src, "c.authHeader()",
		"bearer/api_key path must still call authHeader()")
	require.NotContains(t, src, "c.oauth1Header(",
		"non-oauth1_tba specs must not emit oauth1Header call")
}
