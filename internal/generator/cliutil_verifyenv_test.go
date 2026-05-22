package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliutilVerifyEnvTemplateEmitsBothHelpers asserts the rendered
// cliutil package exposes the original IsVerifyEnv plus the new
// IsVerifyLiveHTTPEnv with their canonical env-var names. This pins the
// generator's contract so a future template edit cannot silently drop
// or rename either helper, which the transport-layer short-circuit and
// the verify pipeline both depend on.
func TestCliutilVerifyEnvTemplateEmitsBothHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("verifyenv-helpers")
	outputDir := filepath.Join(t.TempDir(), "verifyenv-helpers-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "verifyenv.go"))
	require.NoError(t, err)
	emitted := string(src)

	assert.Contains(t, emitted, `const VerifyEnvVar = "PRINTING_PRESS_VERIFY"`,
		"existing VerifyEnvVar constant should still be emitted")
	assert.Contains(t, emitted, `const VerifyLiveHTTPEnvVar = "PRINTING_PRESS_VERIFY_LIVE_HTTP"`,
		"new VerifyLiveHTTPEnvVar constant should be emitted with its canonical name")
	assert.Contains(t, emitted, "func IsVerifyEnv() bool",
		"existing IsVerifyEnv function should still be emitted")
	assert.Contains(t, emitted, "func IsVerifyLiveHTTPEnv() bool",
		"new IsVerifyLiveHTTPEnv function should be emitted")
	assert.Contains(t, emitted, `os.Getenv(VerifyLiveHTTPEnvVar) == "1"`,
		"helper should treat only the literal string \"1\" as truthy, matching IsVerifyEnv's contract")

	// Docstring widening: the file-level comment block should mention the
	// transport-layer use case so an external reader who hits this file
	// understands both gates without having to read client.go.
	assert.True(t,
		strings.Contains(emitted, "DELETE/POST/PUT/PATCH") ||
			strings.Contains(emitted, "mutating HTTP verbs"),
		"docstring should document the new transport-layer scope (DELETE/POST/PUT/PATCH or 'mutating HTTP verbs')")
}

// TestIsVerifyLiveHTTPEnv_OnlyOneIsTruthy mirrors the IsVerifyEnv
// truthiness contract: the helper returns true ONLY for the literal
// string "1". Common alternative truthy values (true, yes, 2, empty)
// must return false so the gate behavior is unambiguous across shells
// and CI runners that interpret env-var truthiness differently.
func TestIsVerifyLiveHTTPEnv_OnlyOneIsTruthy(t *testing.T) {
	apiSpec := minimalSpec("verifyenv-truthiness")
	outputDir := filepath.Join(t.TempDir(), "verifyenv-truthiness-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "verifyenv.go"))
	require.NoError(t, err)
	emitted := string(src)

	// The helper body is a one-liner. Asserting on its exact shape is
	// the simplest way to pin the truthiness contract without compiling
	// and executing the emitted package from this generator-side test.
	assert.Contains(t, emitted,
		`func IsVerifyLiveHTTPEnv() bool {
	return os.Getenv(VerifyLiveHTTPEnvVar) == "1"
}`,
		"IsVerifyLiveHTTPEnv body should be the canonical one-liner that treats only \"1\" as truthy")
}
