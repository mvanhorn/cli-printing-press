package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoctorWithoutVerifyPathDoesNotClaimCredentialsValid(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-no-verify")

	outputDir := filepath.Join(t.TempDir(), "doctor-no-verify-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	src := string(doctorSrc)

	require.Contains(t, src, `"present, not verified. Run `,
		"doctor must not report API credential validity from a bare base URL probe; the unverified copy must use the new INFO phrasing")
	require.Contains(t, src, "suggestion := suggestReadCommand(cmd.Root())",
		"doctor must walk the cobra tree for a runnable read command instead of nagging the user to edit a spec they don't own")
	require.NotContains(t, src, `set auth.verify_path in spec`,
		"the old WARN copy that pointed users at a spec field they can't edit must be gone")
	// Both probe branches (VerifyPath and VerifyQuery) emit `report["credentials"] = "valid"`
	// when the probe succeeds; anchor on the verify-path-shaped invocation so this
	// assertion stays specific to "no REST probe ran" rather than "the literal `valid` never appears."
	require.NotContains(t, src, `c.GetWithHeaders(cmd.Context(), verifyPath`,
		"without auth.verify_path, doctor must not emit a REST verification call")
	require.NotContains(t, src, "but auth was accepted",
		"without auth.verify_path, non-auth HTTP statuses do not prove the API accepted the credentials")
}

func TestDoctorWithVerifyPathCanClaimCredentialsValid(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-verify")
	apiSpec.Auth.VerifyPath = "/account"

	outputDir := filepath.Join(t.TempDir(), "doctor-verify-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	src := string(doctorSrc)

	require.Contains(t, src, `verifyPath := "/account"`)
	require.Contains(t, src, `report["credentials"] = "valid"`,
		"doctor may report valid credentials after a configured authenticated verification probe succeeds")
	require.Contains(t, src, "but auth was accepted",
		"non-auth HTTP statuses only imply accepted auth when they come from the configured verification path")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestDoctorClassifiesHTTP401AsInvalidAnd403AsScopeLimited(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-scope")
	apiSpec.Auth.VerifyPath = "/account"

	outputDir := filepath.Join(t.TempDir(), "doctor-scope-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	src := string(doctorSrc)

	require.Contains(t, src, "case authAPIErr.StatusCode == 401:",
		"doctor must keep a dedicated 401 branch so HTTP 401 reports as invalid credentials")
	require.Contains(t, src, `"invalid (HTTP %d) — check your credentials"`,
		"doctor's 401 message must continue to direct users to check the credential value")

	require.Contains(t, src, "case authAPIErr.StatusCode == 403:",
		"doctor must split 401 and 403 so 403 (scope-limited) is not misreported as invalid")
	require.Contains(t, src, `"scope-limited (HTTP %d) — credentials are valid but lack permission for this endpoint. Check your dashboard's API key scope."`,
		"doctor's 403 message must surface as scope-limited and point at dashboard scope, not the credential value")

	require.NotContains(t, src, "authAPIErr.StatusCode == 401 || authAPIErr.StatusCode == 403",
		"doctor must not collapse 401 and 403 into a single invalid branch")

	require.Contains(t, src, `case strings.Contains(s, "scope-limited"):`,
		"doctor's human-readable indicator switch must classify scope-limited as WARN, not FAIL")
}

func TestAuthStatusReportsCredentialsPresentNotVerified(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status")

	outputDir := filepath.Join(t.TempDir(), "auth-status-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	src := string(authSrc)

	require.Contains(t, src, "Credentials present (not verified)")
	require.Contains(t, src, `"verified":      false`)
	require.NotContains(t, src, `fmt.Fprintln(w, green("Authenticated"))`)
}
