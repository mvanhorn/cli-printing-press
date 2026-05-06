package artifacts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactArchivedSpecSecretsRemovesVendorTokenExamples(t *testing.T) {
	input := []byte(`Examples:
Authorization: Bearer secret-token:vendor_production_wma_24SCp4G81X3yHL4Wq8FgzuaP9ye3VKf2mgTDctXyRg5HY_example
Authorization: Bearer sk_live_1234567890abcdefghijkl
Authorization: Bearer ghp_1234567890abcdefghijklmnopqrstuvwx
Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456
X-API-Key: abcdefghijklmnopqrstuvwxyz123456
"apiKey": "abcdefghijklmnopqrstuvwxyz123456"
api_key: abcdefghijklmnopqrstuvwxyz123456
https://api.example.com/widgets?access_token=abcdefghijklmnopqrstuvwxyz123456
`)

	got := string(RedactArchivedSpecSecrets(input))

	require.Contains(t, got, "Authorization: Bearer secret-token:<REDACTED_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_STRIPE_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_GITHUB_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_BEARER_TOKEN_EXAMPLE>")
	require.Contains(t, got, "X-API-Key: <REDACTED_CREDENTIAL_EXAMPLE>")
	require.Contains(t, got, `"apiKey": "<REDACTED_CREDENTIAL_EXAMPLE>"`)
	require.Contains(t, got, "api_key: <REDACTED_CREDENTIAL_EXAMPLE>")
	require.Contains(t, got, "access_token=<REDACTED_CREDENTIAL_EXAMPLE>")
	require.NotContains(t, got, "vendor_production")
	require.NotContains(t, got, "sk_live_1234567890")
	require.NotContains(t, got, "ghp_1234567890")
	require.NotContains(t, got, "abcdefghijklmnopqrstuvwxyz123456")
}

func TestRedactArchivedSpecSecretsKeepsPlaceholders(t *testing.T) {
	input := []byte("Use Authorization: Bearer TOKEN or MERCURY_BEARER_AUTH=your-token-here")

	got := string(RedactArchivedSpecSecrets(input))

	require.Equal(t, string(input), got)
}
