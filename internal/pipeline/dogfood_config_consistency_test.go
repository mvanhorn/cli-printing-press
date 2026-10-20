package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckConfigConsistency_CookieAccessorLinesAreNotFields(t *testing.T) {
	dir := writeConfigConsistencyFixture(t, `package cli
func save(cfg *config) error {
	if err := cfg.SaveTokens("", "", token, "", time.Time{}); err != nil {
		return err
	}
	return cfg.SaveTokens("", "", cookies, "", time.Time{})
}
`, `package client
func (c *Config) CredentialConfigured() bool {
	return c.AuthHeader() != "" || c.CookieCredential() != ""
}
func ready(cfg *Config) bool {
	return cAuth(cfg) || strings.TrimSpace(cfg.CookieCredential()) != ""
}
func cAuth(cfg *Config) bool { return cfg.AuthHeader() != "" }
`)

	result := checkConfigConsistency(dir)
	assert.True(t, result.Consistent, "write=%v read=%v mismatched=%v", result.WriteFields, result.ReadFields, result.Mismatched)
	assert.Empty(t, result.WriteFields)
	assert.Empty(t, result.ReadFields)
	assert.Empty(t, result.Mismatched)
	assertNoExpressionCaptures(t, result)

	report := passingDogfoodReport()
	report.AuthCheck.Match = true
	report.WiringCheck.ConfigConsist = result
	assert.Equal(t, DogfoodVerdictPass, deriveDogfoodVerdict(report, true))
}

func TestCheckConfigConsistency_RealKeyMismatchSurvivesAccessorLines(t *testing.T) {
	dir := writeConfigConsistencyFixture(t, `package cli
func save(cfg *config) error {
	if err := cfg.SaveTokens("", "", token, "", time.Time{}); err != nil {
		return err
	}
	config.Set("access_token", token)
	viper.Set("auth.token", token)
	return nil
}
`, `package client
func read(c *Config) string {
	if c.AuthHeader() != "" || c.CookieCredential() != "" || strings.TrimSpace(c.CookieCredential()) != "" {
		return config.Get("refresh_token")
	}
	return viper.Get("api-key")
}
`)

	result := checkConfigConsistency(dir)
	assert.False(t, result.Consistent)
	assert.Equal(t, []string{"access_token", "auth.token"}, result.WriteFields)
	assert.Equal(t, []string{"api-key", "refresh_token"}, result.ReadFields)
	assert.ElementsMatch(t, []string{"access_token", "api-key", "auth.token", "refresh_token"}, result.Mismatched)
	assertNoExpressionCaptures(t, result)

	report := passingDogfoodReport()
	report.AuthCheck.Match = true
	report.WiringCheck.ConfigConsist = result
	assert.Equal(t, DogfoodVerdictFail, deriveDogfoodVerdict(report, true))
	var issue string
	for _, candidate := range collectDogfoodIssues(report, true) {
		if strings.Contains(candidate, "config inconsistency") {
			issue = candidate
			break
		}
	}
	require.NotEmpty(t, issue)
	assert.Contains(t, issue, "access_token")
	assert.NotContains(t, issue, "||")
	assert.NotContains(t, issue, ", token,")
}

func TestCheckConfigConsistency_SharedRealKeyWithAccessorLines(t *testing.T) {
	dir := writeConfigConsistencyFixture(t, `package cli
func save(cfg *config) error {
	cfg.SaveTokens("", "", token, "", time.Time{})
	config.Set("access_token", token)
	return nil
}
`, `package client
func read(c *Config) string {
	if c.AuthHeader() != "" || c.CookieCredential() != "" {
		return config.Get("access_token")
	}
	return ""
}
`)

	result := checkConfigConsistency(dir)
	assert.True(t, result.Consistent)
	assert.Equal(t, []string{"access_token"}, result.WriteFields)
	assert.Equal(t, []string{"access_token"}, result.ReadFields)
	assert.Empty(t, result.Mismatched)
}

func TestCheckConfigConsistency_DropsNonNamesAndComments(t *testing.T) {
	t.Run("format string and comment", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, `package cli
func save(cfg *config) error {
	// refresh_token is commentary, not a write
	return cfg.SaveTokens("pre \"token\" post", "", token, "", time.Time{}) // "secret_key"
}
`, `package client
func read(c *Config) string {
	if c.AuthHeader() != "" {
		return "Bearer {token}"
	}
	return ""
}
`)
		result := checkConfigConsistency(dir)
		assert.True(t, result.Consistent, "write=%v read=%v", result.WriteFields, result.ReadFields)
		assert.Empty(t, result.WriteFields)
		assert.Empty(t, result.ReadFields)
	})

	t.Run("raw string key is kept", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, "package cli\nfunc save(cfg *config) error {\n\treturn cfg.SaveTokens(`access_token`, \"\", token, \"\", time.Time{})\n}\n",
			"package client\nfunc read(c *Config) string {\n\tif c.AuthHeader() != \"\" { return `access_token` }\n\treturn \"\"\n}\n")
		result := checkConfigConsistency(dir)
		assert.True(t, result.Consistent, "write=%v read=%v mismatched=%v", result.WriteFields, result.ReadFields, result.Mismatched)
		assert.Equal(t, []string{"access_token"}, result.WriteFields)
		assert.Equal(t, []string{"access_token"}, result.ReadFields)
	})

	t.Run("spaced keys are not a mismatch", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, `package cli
func save() { config.Set("access token", token) }
`, `package client
func read(c *Config) string {
	if c.AuthHeader() != "" {
		return config.Get("refresh token")
	}
	return ""
}
`)
		result := checkConfigConsistency(dir)
		assert.True(t, result.Consistent, "write=%v read=%v mismatched=%v", result.WriteFields, result.ReadFields, result.Mismatched)
		assert.Empty(t, result.WriteFields)
		assert.Empty(t, result.ReadFields)
	})

	t.Run("non-credential literal on a token call is not a field", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, `package cli
func save(cfg *config) error {
	return cfg.SaveTokens("items", "", token, "", time.Time{})
}
`, `package client
func read(c *Config) string {
	if c.AuthHeader() != "" {
		return "ready"
	}
	return ""
}
`)
		result := checkConfigConsistency(dir)
		assert.True(t, result.Consistent, "write=%v read=%v", result.WriteFields, result.ReadFields)
		assert.Empty(t, result.WriteFields)
		assert.Empty(t, result.ReadFields)
	})

	t.Run("explicit key without a credential word", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, `package cli
func save() { config.Set("timeout", v) }
`, `package client
func read() string { return config.Get("timeout") }
`)
		result := checkConfigConsistency(dir)
		assert.True(t, result.Consistent)
		assert.Equal(t, []string{"timeout"}, result.WriteFields)
		assert.Equal(t, []string{"timeout"}, result.ReadFields)
	})

	t.Run("unclosed literal does not panic or invent a field", func(t *testing.T) {
		dir := writeConfigConsistencyFixture(t, "package cli\nfunc save(cfg *config) error {\n\treturn cfg.SaveTokens(\"access_token\n}\n",
			`package client
func read(c *Config) string { return c.AuthHeader() }
`)
		var result ConfigConsistResult
		require.NotPanics(t, func() {
			result = checkConfigConsistency(dir)
		})
		assert.True(t, result.Consistent)
		assert.Empty(t, result.WriteFields)
		assert.Empty(t, result.ReadFields)
	})
}

func TestRunDogfood_ConfigMismatchStillFails(t *testing.T) {
	dir := writeConfigConsistencyFixture(t, `package cli
func save() { config.Set("AccessToken", token) }
`, `package client
func read() string { return config.Get("DominosToken") }
`)

	report, err := RunDogfood(dir, "")
	require.NoError(t, err)
	assert.False(t, report.WiringCheck.ConfigConsist.Consistent)
	// A directory with no buildable command tree trips the example-check
	// WARN before this rule. The verdict that shipcheck acts on is the one
	// an otherwise-clean report gets.
	clean := passingDogfoodReport()
	clean.AuthCheck.Match = true
	clean.WiringCheck.ConfigConsist = report.WiringCheck.ConfigConsist
	assert.Equal(t, DogfoodVerdictFail, deriveDogfoodVerdict(clean, true))
	assert.Equal(t, []string{"AccessToken"}, report.WiringCheck.ConfigConsist.WriteFields)
	assert.Equal(t, []string{"DominosToken"}, report.WiringCheck.ConfigConsist.ReadFields)
	var found bool
	for _, issue := range report.Issues {
		if strings.Contains(issue, "config inconsistency") {
			found = true
			assert.Contains(t, issue, "AccessToken")
			assert.Contains(t, issue, "DominosToken")
			assert.NotContains(t, issue, ", token,")
		}
	}
	assert.True(t, found, "issues: %v", report.Issues)
}

func TestRunDogfood_GeneratedCookieCLIConfigFieldsConsistent(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:      "cookieconsist",
		Version:   "0.1.0",
		BaseURL:   "https://www.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: ".example.com",
			Cookies:      []string{"session_id"},
			EnvVars:      []string{"COOKIECONSIST_COOKIES"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/cookieconsist-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
	dir := filepath.Join(t.TempDir(), "cookieconsist-pp-cli")
	require.NoError(t, generator.New(apiSpec, dir).Generate())

	authGo, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.Contains(t, string(authGo), `SaveTokens("", "",`,
		"generated cookie auth must still call SaveTokens with empty literals; otherwise this proof no longer covers the false capture")
	configGo, err := os.ReadFile(filepath.Join(dir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configGo), `c.CookieCredential() != ""`,
		"generated cookie config must still read the credential through the accessor")

	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `name: cookieconsist
version: "0.1.0"
base_url: https://www.example.com
auth:
  type: cookie
  header: Cookie
  in: cookie
  cookie_domain: .example.com
  cookies:
    - session_id
  env_vars:
    - COOKIECONSIST_COOKIES
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)
	assert.True(t, report.WiringCheck.ConfigConsist.Consistent,
		"write=%v read=%v mismatched=%v",
		report.WiringCheck.ConfigConsist.WriteFields,
		report.WiringCheck.ConfigConsist.ReadFields,
		report.WiringCheck.ConfigConsist.Mismatched)
	assert.Empty(t, report.WiringCheck.ConfigConsist.WriteFields)
	assert.Empty(t, report.WiringCheck.ConfigConsist.ReadFields)
	assert.Empty(t, report.WiringCheck.ConfigConsist.Mismatched)
	assertNoExpressionCaptures(t, report.WiringCheck.ConfigConsist)
	for _, issue := range report.Issues {
		assert.NotContains(t, issue, "config inconsistency")
	}
	// Dead helpers and an unbuilt example check can WARN this minimal print
	// before the config rule. The gate that fails a clean CLI is the one
	// shipcheck reaches once those are clear.
	clean := passingDogfoodReport()
	clean.AuthCheck.Match = true
	clean.WiringCheck.ConfigConsist = report.WiringCheck.ConfigConsist
	assert.Equal(t, DogfoodVerdictPass, deriveDogfoodVerdict(clean, true))

	// A real key mismatch in the same generated tree still fails, including
	// while the empty-literal SaveTokens / CookieCredential lines are present.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "token_mismatch.go"), []byte(`package cli
func saveMismatch() { config.Set("access_token", token) }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "client", "token_mismatch.go"), []byte(`package client
func readMismatch(c *Config) string {
	if c.AuthHeader() != "" || c.CookieCredential() != "" {
		return config.Get("refresh_token")
	}
	return ""
}
`), 0o644))
	brokenReport, err := RunDogfood(dir, specPath)
	require.NoError(t, err)
	broken := brokenReport.WiringCheck.ConfigConsist
	assert.False(t, broken.Consistent)
	assert.Equal(t, []string{"access_token"}, broken.WriteFields)
	assert.Equal(t, []string{"refresh_token"}, broken.ReadFields)
	assertNoExpressionCaptures(t, broken)
	var mismatchIssue string
	for _, issue := range brokenReport.Issues {
		if strings.Contains(issue, "config inconsistency") {
			mismatchIssue = issue
			break
		}
	}
	require.NotEmpty(t, mismatchIssue, "issues: %v", brokenReport.Issues)
	assert.Contains(t, mismatchIssue, "access_token")
	assert.Contains(t, mismatchIssue, "refresh_token")
	assert.NotContains(t, mismatchIssue, ", token,")
	assert.NotContains(t, mismatchIssue, "||")
	clean.WiringCheck.ConfigConsist = broken
	assert.Equal(t, DogfoodVerdictFail, deriveDogfoodVerdict(clean, true))
}

func writeConfigConsistencyFixture(t *testing.T, writeSrc, readSrc string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), writeSrc)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), readSrc)
	return dir
}

func assertNoExpressionCaptures(t *testing.T, result ConfigConsistResult) {
	t.Helper()
	for _, field := range append(append([]string{}, result.WriteFields...), append(result.ReadFields, result.Mismatched...)...) {
		for _, bad := range []string{" ", "|", "!", "(", ")", ","} {
			assert.NotContains(t, field, bad, "field %q", field)
		}
	}
}
