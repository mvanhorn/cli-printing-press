package cli

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintDogfoodReportRespectsSkippedPathCheck(t *testing.T) {
	report := &pipeline.DogfoodReport{
		Dir:      t.TempDir(),
		SpecPath: "synthetic.yaml",
		PathCheck: pipeline.PathCheckResult{
			Skipped: true,
			Detail:  "synthetic spec: path validity not applicable",
		},
	}

	out := captureStdout(t, func() {
		printDogfoodReport(report)
	})

	assert.Contains(t, out, "Path Validity:     0/0 valid (SKIP)")
	assert.Contains(t, out, "synthetic spec: path validity not applicable")
	assert.NotContains(t, out, "Path Validity:     0/0 valid (FAIL)")
}

func TestDogfoodHelpIncludesLiveFlags(t *testing.T) {
	cmd := newDogfoodCmd()
	cmd.SetArgs([]string{"--help"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	assert.Contains(t, output, "--live")
	assert.Contains(t, output, "--level")
	assert.Contains(t, output, "--write-acceptance")
}
