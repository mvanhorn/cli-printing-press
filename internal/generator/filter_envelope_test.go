package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFilterFieldsEnvelopeUnwrap_EmittedInHelpers asserts the generator
// emits the --select envelope-unwrap support in every printed CLI:
// the selectEnvelopeArray helper is defined and filterFields wires
// through it before falling back to filterFieldsRec.
//
// Behavior is exercised by the unit tests in helpers_test.go.tmpl,
// which ship alongside helpers.go in every generated CLI. This canary
// just proves the structural contract — the function exists in the
// emitted source and the entry point calls it.
func TestFilterFieldsEnvelopeUnwrap_EmittedInHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("envelope-canary")
	outputDir := filepath.Join(t.TempDir(), "envelope-canary-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err, "helpers.go must be emitted")
	src := string(helpersSrc)

	require.Contains(t, src, "func selectEnvelopeArray(",
		"selectEnvelopeArray helper must be emitted into the printed CLI")
	require.Contains(t, src, "if inner, ok := selectEnvelopeArray(data, paths); ok {",
		"filterFields must call selectEnvelopeArray before filterFieldsRec")

	// Test file must ship alongside so consumer's `go test` exercises the
	// new behavior end-to-end.
	testPath := filepath.Join(outputDir, "internal", "cli", "helpers_test.go")
	testSrc, err := os.ReadFile(testPath)
	require.NoError(t, err, "helpers_test.go must ship into the generated CLI")
	require.Contains(t, string(testSrc), "TestFilterFields_EnvelopeUnwrap_DropsHeavyEnvelopeField",
		"emitted test file must cover the envelope-unwrap case")

	// Sanity: the recognized envelope keys are spelled out in the helper
	// so future maintenance has a single place to extend the list.
	for _, key := range []string{`"results"`, `"data"`, `"items"`, `"hits"`} {
		require.True(t, strings.Contains(src, key),
			"selectEnvelopeArray must declare envelope key %s explicitly", key)
	}
}
