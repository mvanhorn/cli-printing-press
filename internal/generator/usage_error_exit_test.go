package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUsageErrorExitCode_EmittedInRoot asserts the generator emits the
// Cobra usage-error wrap in every printed CLI: the isCobraUsageError
// helper is defined, Execute() wraps Cobra/pflag pre-RunE errors in
// usageErr() before they reach ExitCode(), and the hint-suggestion
// rewrap preserves the original error chain (so isCobraUsageError still
// classifies it after the wrap).
//
// Behavior is exercised by the unit tests in root_test.go.tmpl, which
// ship alongside root.go in every generated CLI. This canary just
// proves the structural contract — the helper exists, the wrap is
// wired, and the hint path was switched from `return ...` to `err = ...`.
func TestUsageErrorExitCode_EmittedInRoot(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("usage-exit-canary")
	outputDir := filepath.Join(t.TempDir(), "usage-exit-canary-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err, "root.go must be emitted")
	src := string(rootSrc)

	require.Contains(t, src, "func isCobraUsageError(",
		"isCobraUsageError helper must be emitted into the printed CLI")
	require.Contains(t, src, "if err != nil && isCobraUsageError(err) {",
		"Execute() must wrap Cobra usage errors before returning")
	require.Contains(t, src, "return usageErr(err)",
		"the wrap must route through usageErr so ExitCode yields 2")

	// The hint-suggestion rewrap path was switched from `return
	// fmt.Errorf(...)` to `err = fmt.Errorf(...)` so the rewrapped
	// error falls through to the usage wrap below. Pin that switch.
	require.NotContains(t, src, `return fmt.Errorf("%w\nhint: did you mean --%s?", err, suggestion)`,
		"hint path must assign to err so the usage wrap can catch the rewrapped error")
	require.Contains(t, src, `err = fmt.Errorf("%w\nhint: did you mean --%s?", err, suggestion)`,
		"hint path must assign to err (not early-return) so the usage wrap runs after")

	// Test file must ship alongside so the consumer's `go test` exercises
	// the behavior end-to-end.
	testPath := filepath.Join(outputDir, "internal", "cli", "root_test.go")
	testSrc, err := os.ReadFile(testPath)
	require.NoError(t, err, "root_test.go must ship into the generated CLI")
	require.Contains(t, string(testSrc), "TestExitCode_UsageError_WrappedAsCode2",
		"emitted test file must cover the wrap → code-2 contract")
}
