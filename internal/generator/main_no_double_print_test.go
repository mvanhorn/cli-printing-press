package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMainNoDoublePrintErrors asserts the generated cmd/<cli>/main.go does
// not print err.Error() to stderr, since root.go.tmpl leaves Cobra's
// default error printing on (SilenceUsage:true but no SilenceErrors). A
// second main.go-side print causes every failing command to emit the
// error message twice — once with Cobra's "Error:" prefix and once as
// the raw message — which is what schoology/epic-myhealth shipped before
// this fix.
//
// We pin the structural shape: no `fmt` import in main.go, no
// `fmt.Fprintln(os.Stderr, err.Error())` call. Behavior is covered
// by the emitted root.go's own tests (root_test.go.tmpl).
func TestMainNoDoublePrintErrors(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("nodbl-canary")
	outputDir := filepath.Join(t.TempDir(), "nodbl-canary-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mainPath := filepath.Join(outputDir, "cmd", "nodbl-canary-pp-cli", "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	require.NoError(t, err, "main.go must be emitted")
	src := string(mainSrc)

	require.NotContains(t, src, `"fmt"`,
		`main.go must not import "fmt" — the only prior use was the double-print line`)
	require.NotContains(t, src, "fmt.Fprintln(os.Stderr, err.Error())",
		"main.go must not print err.Error(); Cobra prints it once via SilenceUsage=true/SilenceErrors=false")
	require.NotContains(t, src, "fmt.Fprint(os.Stderr, err.Error())",
		"main.go must not print err.Error() via Fprint either")

	// Exit-code propagation must still flow through cli.ExitCode so
	// usageErr → 2, configErr → 10, authErr → 5 stay intact.
	require.Contains(t, src, "os.Exit(cli.ExitCode(err))",
		"main.go must still propagate the typed exit code")
}

// TestRootEmitsUnknownFlagHintToStderr asserts root.go.tmpl emits an
// explicit stderr write of the suggestion alongside the err wrap. Before
// this fix, the hint was only ever visible because main.go re-printed
// err.Error(); now that main.go is silent, Cobra prints `Error: unknown
// flag: --foob` (without the hint, since Cobra prints before the wrap
// happens), and root.go owns surfacing the hint.
func TestRootEmitsUnknownFlagHintToStderr(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hint-canary")
	outputDir := filepath.Join(t.TempDir(), "hint-canary-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err, "root.go must be emitted")
	src := string(rootSrc)

	require.Contains(t, src, `fmt.Fprintf(os.Stderr, "hint: did you mean --%s?\n", suggestion)`,
		"root.go must print the unknown-flag hint to stderr directly; main.go no longer does")
	// And the wrap still has to happen so the err.Error() chain carries
	// the hint for downstream consumers and isCobraUsageError keeps its
	// classification (covered by TestUsageErrorExitCode_EmittedInRoot).
	require.Contains(t, src, `err = fmt.Errorf("%w\nhint: did you mean --%s?", err, suggestion)`,
		"root.go must still wrap err with the hint for downstream consumers")
}
