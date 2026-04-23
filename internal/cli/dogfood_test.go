package cli

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
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

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	require.NoError(t, w.Close())

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return buf.String()
}
