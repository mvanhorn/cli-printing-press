package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDoctor_VerifyModeLine pins that the emitted doctor command surfaces
// verify-env state alongside the other env-driven readouts. An operator
// who unintentionally inherits PRINTING_PRESS_VERIFY=1 (parent shell, CI
// runner, container image) must detect the foot-gun by running
// `<cli> doctor`, without having to read a synthetic response body. This
// pairs with the synthetic envelope's verify_noop top-level signal as a
// second diagnosis anchor.
func TestDoctor_VerifyModeLine(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-verify-mode")
	outputDir := filepath.Join(t.TempDir(), "doctor-verify-mode-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	emitted := string(src)

	// cliutil import must be present — the verify-env helpers live there.
	// Module path varies per generated CLI, so match on the suffix only.
	assert.Contains(t, emitted, `/internal/cliutil"`,
		"doctor.go should import cliutil to call IsVerifyEnv / IsVerifyLiveHTTPEnv")

	// Both helpers must be called: IsVerifyEnv gates the active/inactive
	// branch; IsVerifyLiveHTTPEnv distinguishes short-circuit vs dial-out
	// modes inside the active branch.
	assert.Contains(t, emitted, "cliutil.IsVerifyEnv()",
		"doctor should call cliutil.IsVerifyEnv() to detect verify mode")
	assert.Contains(t, emitted, "cliutil.IsVerifyLiveHTTPEnv()",
		"doctor should call cliutil.IsVerifyLiveHTTPEnv() to distinguish dial-out mode")

	// All three branches populate report["verify_mode"] with content that
	// reads cleanly when prefixed by the OK / INFO indicator.
	assert.Contains(t, emitted, `report["verify_mode"] = "normal operation"`,
		"inactive branch should report normal operation")
	assert.Contains(t, emitted, `report["verify_mode"] = "INFO ACTIVE — mutating HTTP verbs short-circuit`,
		"verify-only branch should report the short-circuit state")
	assert.Contains(t, emitted, `report["verify_mode"] = "INFO ACTIVE — live HTTP opt-in (mutating verbs dial out)"`,
		"verify+live-http branch should report the dial-out state")

	// checkKeys must include the verify_mode entry so the line renders
	// in the human-readable output alongside the other env-driven rows.
	envVars := strings.Index(emitted, `{"env_vars", "Env Vars"}`)
	verifyMode := strings.Index(emitted, `{"verify_mode", "Verify Mode"}`)
	apiKey := strings.Index(emitted, `{"api", "API"}`)
	require.NotEqual(t, -1, envVars, `checkKeys must contain {"env_vars", "Env Vars"}`)
	require.NotEqual(t, -1, verifyMode, `checkKeys must contain {"verify_mode", "Verify Mode"}`)
	require.NotEqual(t, -1, apiKey, `checkKeys must contain {"api", "API"}`)
	assert.True(t, envVars < verifyMode && verifyMode < apiKey,
		"verify_mode line must render between Env Vars and API rows")
}
