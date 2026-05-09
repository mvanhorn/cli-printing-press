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

	require.Contains(t, src, `report["credentials"] = "present (not verified — set auth.verify_path in spec for an API acceptance check)"`,
		"doctor must not report API credential validity from a bare base URL probe")
	require.NotContains(t, src, `report["credentials"] = "valid"`,
		"without auth.verify_path, a 2xx base URL response does not prove the API accepted the credentials")
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
