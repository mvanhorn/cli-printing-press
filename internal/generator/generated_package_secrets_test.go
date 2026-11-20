package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedTestsPassPublishSecretScan is the publish-scan contract for
// generated tests: a fresh cookie-auth print whose declared cookie names
// collide with generated fixtures (historically `token=` / `key=`) must
// not trip the mandatory secret scan. Injecting a live-looking vendor key
// into any generated file, including a test, still fails.
func TestGeneratedTestsPassPublishSecretScan(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("secretscancookie")
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"token", "key", "session_id"},
		EnvVars:      []string{"SECRETSCANCOOKIE_COOKIES"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliutilTestSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "cliutil_test.go")
	require.Contains(t, cliutilTestSrc, "token=your-token-here",
		"emitted cliutil tests must still cover token=<value> credential redaction")
	require.Contains(t, cliutilTestSrc, "key=your-key-here",
		"emitted cliutil tests must still cover key=<value> credential redaction")

	shelloutSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout_test.go")
	require.Contains(t, shelloutSrc, `"token=your-stolen-token-here"`,
		"emitted cobratree tests must still cover '='-containing blocked MCP keys")

	findings, err := artifacts.FindPackageSecrets(outputDir, apiSpec.Auth.Cookies)
	require.NoError(t, err)
	require.Empty(t, findings, "generated package must not trip the publish secret scan:\n%s",
		artifacts.FormatVendorPrefixSecretFindings(findings))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cliutil/", "./internal/mcp/cobratree/")

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cliutil", "planted_secret_test.go"),
		[]byte("package cliutil\n\nconst planted = \""+strings.Join([]string{"sk", "_live_", "1234567890abcdefghijklmnop"}, "")+"\"\n"),
		0o644,
	))
	planted, err := artifacts.FindPackageSecrets(outputDir, apiSpec.Auth.Cookies)
	require.NoError(t, err)
	require.NotEmpty(t, planted, "planting a live-looking sk_live_ value in a generated test must still fail the scan")
	kinds := make([]string, 0, len(planted))
	for _, finding := range planted {
		kinds = append(kinds, finding.Kind)
	}
	require.Contains(t, kinds, "stripe-secret-key")
}

// storedMatchingOriginToken is the OAuth config-perms fixture. The publish
// scanner allowlists the word "example" only when it sits inside the value
// it reads next to AccessToken, so the generated literal has to keep both.
const storedMatchingOriginToken = "STORED-EXAMPLE-TOKEN-STAYS-ON-MATCHING-ORIGIN"

// TestGeneratedOAuthTokenURLCLIPassesPublishSecretScan generates each OAuth
// token-URL golden CLI and runs the same package secret scan publish uses
// for non-cookie auth (a nil cookie list). The generated tree must be clean,
// and the config-perms fixture must still assert that the stored token stays
// when a loose file's base URL matches the default.
func TestGeneratedOAuthTokenURLCLIPassesPublishSecretScan(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"golden-api-oauth2-cc.yaml",
		"golden-api-oauth2-authcode.yaml",
		"golden-api-oauth2-device-code.yaml",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			apiSpec, outputDir := generateGoldenFixture(t, fixture)
			require.NotEmpty(t, apiSpec.Auth.TokenURL)
			authType := strings.ToLower(strings.TrimSpace(apiSpec.Auth.Type))
			if authType == "cookie" || authType == "composed" {
				t.Fatalf("token-URL fixture parsed as %q; publish would scan declared cookie names", apiSpec.Auth.Type)
			}

			permsSrc := readGeneratedFile(t, outputDir, "internal", "config", "config_perms_test.go")
			require.Contains(t, permsSrc, `AccessToken: "`+storedMatchingOriginToken+`"`)
			require.Contains(t, permsSrc, `cfg.AccessToken != "`+storedMatchingOriginToken+`"`)
			require.Contains(t, permsSrc, `cfg.RefreshToken != "REFRESH-DO-NOT-POST"`)
			require.Contains(t, permsSrc, "want the loose file's token endpoint dropped")
			require.NotContains(t, permsSrc, "STORED-TOKEN-STAYS-ON-MATCHING-ORIGIN")

			findings, err := artifacts.FindPackageSecrets(outputDir, nil)
			require.NoError(t, err)
			require.Empty(t, findings, "generated %s package must not trip the publish secret scan:\n%s",
				fixture, artifacts.FormatVendorPrefixSecretFindings(findings))

			requireGeneratedCompiles(t, outputDir)

			if fixture == "golden-api-oauth2-cc.yaml" && !testing.Short() {
				runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestLoad_LooseConfigDropsOAuthEndpointOverrides", "-count=1")
				for _, goos := range []string{"linux", "darwin", "windows"} {
					runGeneratedCrossGo(t, outputDir, goos, "amd64", "vet", "./internal/config")
					runGeneratedCrossGo(t, outputDir, goos, "amd64", "build", "./internal/config")
				}
			}
		})
	}
}

// TestPublishSecretScanFlagsOpaqueTokenInGeneratedTest is the negative half
// of the OAuth fixture contract: a real-looking opaque token in a generated
// test file is still opaque-credential:token. The previous fixture value
// stays a finding, and putting an allowlisted word only in a comment does
// not clear it.
func TestPublishSecretScanFlagsOpaqueTokenInGeneratedTest(t *testing.T) {
	t.Parallel()

	_, outputDir := generateGoldenFixture(t, "golden-api-oauth2-cc.yaml")
	realToken := strings.Join([]string{"pQr7sT9u", "V1wX3yZ5", "aB2cD4eF", "6gH8jK0m"}, "")
	previousFixture := strings.Join([]string{"STORED-TOKEN-", "STAYS-ON-", "MATCHING-ORIGIN"}, "")
	require.GreaterOrEqual(t, len(realToken), 32)
	require.GreaterOrEqual(t, len(previousFixture), 32)
	require.NotContains(t, strings.ToLower(realToken), "example")
	require.NotContains(t, strings.ToLower(previousFixture), "example")

	writePlantedAccessTokenTest(t, outputDir, "planted_real_opaque_token_test.go", realToken, "")
	writePlantedAccessTokenTest(t, outputDir, "planted_previous_fixture_test.go", previousFixture, "")
	writePlantedAccessTokenTest(t, outputDir, "planted_comment_example_test.go", previousFixture, " // example")
	writePlantedAccessTokenTest(t, outputDir, "planted_allowlisted_fixture_test.go", storedMatchingOriginToken, "")

	findings, err := artifacts.FindPackageSecrets(outputDir, nil)
	require.NoError(t, err)
	byPath := map[string][]string{}
	for _, finding := range findings {
		byPath[finding.Path] = append(byPath[finding.Path], finding.Kind)
	}
	require.Equal(t, map[string][]string{
		"internal/config/planted_real_opaque_token_test.go": {"opaque-credential:token"},
		"internal/config/planted_previous_fixture_test.go":  {"opaque-credential:token"},
		"internal/config/planted_comment_example_test.go":   {"opaque-credential:token"},
	}, byPath)
}

func TestCredentialFieldSelectorTripsPackageScan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		field string
		trips bool
	}{
		{field: "PrintingPressOauth2ClientSecret", trips: true},
		{field: "PrintingPressOauth2ClientId", trips: false},
		{field: "RichAuthClientSecret", trips: false},
		{field: "PrintingPressOauth2Oauth2AuthCode", trips: false},
		{field: "", trips: false},
		{field: "   ", trips: false},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.trips, (credentialField{GoField: tc.field}).TripsPackageScan())
		})
	}
	require.False(t, (*configTemplateData)(nil).CredentialFieldsTripPackageScan())
	require.True(t, (&configTemplateData{CredentialFields: []credentialField{{GoField: "PrintingPressOauth2ClientSecret"}}}).CredentialFieldsTripPackageScan())
	require.False(t, (&configTemplateData{CredentialFields: []credentialField{{GoField: "RichAuthClientSecret"}}}).CredentialFieldsTripPackageScan())
}

func writePlantedAccessTokenTest(t *testing.T, outputDir, name, value, trailer string) {
	t.Helper()
	body := "package config\n\nvar planted = struct{ AccessToken string }{AccessToken: \"" + value + "\"}" + trailer + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", name), []byte(body), 0o644))
}
