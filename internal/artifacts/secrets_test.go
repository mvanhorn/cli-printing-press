package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactArchivedSpecSecretsRemovesVendorTokenExamples(t *testing.T) {
	input := []byte(strings.Join([]string{
		"Examples:",
		"Authorization: Bearer secret-token:vendor_production_wma_24SCp4G81X3yHL4Wq8FgzuaP9ye3VKf2mgTDctXyRg5HY_example",
		"Authorization: Bearer " + testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz1234567890"),
		"Authorization: Bearer " + testSecret("sk", "_live_", "1234567890abcdefghijkl"),
		"Authorization: Bearer " + testSecret("ghp", "_1234567890abcdefghijklmnopqrstuvwx"),
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",
		"X-API-Key: abcdefghijklmnopqrstuvwxyz123456",
		`"apiKey": "abcdefghijklmnopqrstuvwxyz123456"`,
		"api_key: abcdefghijklmnopqrstuvwxyz123456",
		"https://api.example.com/widgets?access_token=abcdefghijklmnopqrstuvwxyz123456",
	}, "\n"))

	got := string(RedactArchivedSpecSecrets(input))

	require.Contains(t, got, "Authorization: Bearer secret-token:<REDACTED_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_OPENROUTER_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_STRIPE_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_GITHUB_TOKEN_EXAMPLE>")
	require.Contains(t, got, "Authorization: Bearer <REDACTED_BEARER_TOKEN_EXAMPLE>")
	require.Contains(t, got, "X-API-Key: <REDACTED_CREDENTIAL_EXAMPLE>")
	require.Contains(t, got, `"apiKey": "<REDACTED_CREDENTIAL_EXAMPLE>"`)
	require.Contains(t, got, "api_key: <REDACTED_CREDENTIAL_EXAMPLE>")
	require.Contains(t, got, "access_token=<REDACTED_CREDENTIAL_EXAMPLE>")
	require.NotContains(t, got, "vendor_production")
	require.NotContains(t, got, testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz"))
	require.NotContains(t, got, testSecret("sk", "_live_", "1234567890"))
	require.NotContains(t, got, "ghp_1234567890")
	require.NotContains(t, got, "abcdefghijklmnopqrstuvwxyz123456")
}

func TestRedactArchivedSpecSecretsKeepsPlaceholders(t *testing.T) {
	input := []byte("Use Authorization: Bearer TOKEN or MERCURY_BEARER_AUTH=your-token-here")

	got := string(RedactArchivedSpecSecrets(input))

	require.Equal(t, string(input), got)
}

func TestFindVendorPrefixSecretsReportsFileAndLine(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run-1", "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "spec.json"), []byte("{\n  \"token\": \""+testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz1234567890")+"\"\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "aws.txt"), []byte("key="+testSecret("AK", "IA", "1234567890ABCDEF")+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".manuscripts", "run-1", "research", "openapi.json"), []byte("Authorization: Bearer "+testSecret("sk", "_live_", "1234567890abcdefghijklmnop")+"\n"), 0o644))

	findings, err := FindVendorPrefixSecrets(root)
	require.NoError(t, err)
	require.Len(t, findings, 3)
	byPath := map[string]VendorPrefixSecretFinding{}
	for _, finding := range findings {
		byPath[finding.Path] = finding
	}
	require.Equal(t, 1, byPath["aws.txt"].Line)
	require.Equal(t, "aws-access-key", byPath["aws.txt"].Kind)
	require.Equal(t, 2, byPath["spec.json"].Line)
	require.Equal(t, "openrouter-api-key", byPath["spec.json"].Kind)
	require.Equal(t, 1, byPath[".manuscripts/run-1/research/openapi.json"].Line)
	require.Equal(t, "stripe-secret-key", byPath[".manuscripts/run-1/research/openapi.json"].Kind)
}

func TestFindVendorPrefixSecretsIgnoresPlaceholdersAndBinaryFiles(t *testing.T) {
	root := t.TempDir()
	readme := "Use sk-EXAMPLE-KEY, " + testSecret("AK", "IA", "IOSFODNN7EXAMPLE") + ", or your-key-here for setup.\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0, 's', 'k', '_', 'l', 'i', 'v', 'e', '_', '1', '2', '3'}, 0o644))

	findings, err := FindVendorPrefixSecrets(root)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestFindSpecDeclaredCookieSecretsReportsCookieNameOnly(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Cookie:session-id=actuallyrealcookievaluexyz; x-main=your-cookie-here; y-main=not-an-example-real-value\n"), 0o644))

	findings, err := FindSpecDeclaredCookieSecrets(root, []string{"session-id", "x-main", "y-main"})
	require.NoError(t, err)
	require.Len(t, findings, 2)
	byKind := map[string]VendorPrefixSecretFinding{}
	for _, finding := range findings {
		byKind[finding.Kind] = finding
	}
	require.Equal(t, "README.md", byKind["cookie-value:session-id"].Path)
	require.Equal(t, 1, byKind["cookie-value:session-id"].Line)
	require.Equal(t, "README.md", byKind["cookie-value:y-main"].Path)
	require.Equal(t, 1, byKind["cookie-value:y-main"].Line)
}

func TestFindSpecDeclaredCookieSecretsReportsStructuredNameValueCookies(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "cookies.json"), []byte(`{"name":"session-id","value":"actuallyrealcookievaluexyz"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cookies-reversed.json"), []byte(`{"value":"anotherrealcookievaluexyz","name":"x-main"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cookies-pretty.json"), []byte("{\n  \"name\": \"y-main\",\n  \"value\": \"prettyrealcookievaluexyz\"\n}\n"), 0o644))

	findings, err := FindSpecDeclaredCookieSecrets(root, []string{"session-id", "x-main", "y-main"})
	require.NoError(t, err)
	require.Len(t, findings, 3)
	byKind := map[string]VendorPrefixSecretFinding{}
	for _, finding := range findings {
		byKind[finding.Kind] = finding
	}
	require.Equal(t, "cookies.json", byKind["cookie-value:session-id"].Path)
	require.Equal(t, 1, byKind["cookie-value:session-id"].Line)
	require.Equal(t, "cookies-reversed.json", byKind["cookie-value:x-main"].Path)
	require.Equal(t, 1, byKind["cookie-value:x-main"].Line)
	require.Equal(t, "cookies-pretty.json", byKind["cookie-value:y-main"].Path)
	require.Equal(t, 3, byKind["cookie-value:y-main"].Line)
}

func TestFindPackageSecretsCombinesVendorPrefixAndDeclaredCookies(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Cookie: session-id=actuallyrealcookievaluexyz\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "spec.json"), []byte("\"token\":\""+testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz1234567890")+"\"\n"), 0o644))

	findings, err := FindPackageSecrets(root, []string{"session-id"})
	require.NoError(t, err)
	require.Len(t, findings, 2)
	byKind := map[string]VendorPrefixSecretFinding{}
	for _, finding := range findings {
		byKind[finding.Kind] = finding
	}
	require.Equal(t, "README.md", byKind["cookie-value:session-id"].Path)
	require.Equal(t, 1, byKind["cookie-value:session-id"].Line)
	require.Equal(t, "spec.json", byKind["openrouter-api-key"].Path)
	require.Equal(t, 1, byKind["openrouter-api-key"].Line)
}

func testSecret(parts ...string) string {
	return strings.Join(parts, "")
}
