package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDogfoodResults_RedactsHomePaths(t *testing.T) {
	dir := t.TempDir()
	leak := filepath.Join("/Users/operator/printing-press/library", filepath.Base(dir))
	specPath := filepath.Join(leak, ".manuscripts", "run1", "spec.yaml")
	report := &DogfoodReport{
		Dir:      leak,
		SpecPath: specPath,
		Verdict:  "PASS",
	}

	require.NoError(t, writeDogfoodResults(report, dir))

	data, err := os.ReadFile(filepath.Join(dir, "dogfood-results.json"))
	require.NoError(t, err)
	assertNoHomePrefix(t, data)

	var loaded DogfoodReport
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, filepath.Join(artifacts.CLIDirPlaceholder, filepath.Base(dir)), loaded.Dir)
	assert.Equal(t, filepath.Join(artifacts.CLIDirPlaceholder, ".manuscripts", "run1", "spec.yaml"), loaded.SpecPath)
	// In-memory report is unchanged so downstream callers still see real paths.
	assert.Equal(t, leak, report.Dir)
	assert.Equal(t, specPath, report.SpecPath)
}

func TestWriteWorkflowVerifyReport_RedactsHomePaths(t *testing.T) {
	dir := t.TempDir()
	leak := filepath.Join("/Users/operator/printing-press/library", filepath.Base(dir))
	report := &WorkflowVerifyReport{
		Dir:     leak,
		Verdict: WorkflowVerdictPass,
	}

	require.NoError(t, writeWorkflowVerifyReport(dir, report))

	data, err := os.ReadFile(filepath.Join(dir, "workflow-verify-report.json"))
	require.NoError(t, err)
	assertNoHomePrefix(t, data)

	loaded, err := LoadWorkflowVerifyReport(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(artifacts.CLIDirPlaceholder, filepath.Base(dir)), loaded.Dir)
	assert.Equal(t, leak, report.Dir)
}

func assertNoHomePrefix(t *testing.T, data []byte) {
	t.Helper()
	s := string(data)
	// Includes /var/folders/ and /tmp/ so a future test that drives
	// real t.TempDir() paths through the writer still trips the gate.
	for _, prefix := range []string{`"/Users/`, `"/home/`, `"C:\\Users\\`, `"/var/folders/`, `"/tmp/`} {
		if strings.Contains(s, prefix) {
			t.Fatalf("artifact must not contain %q prefix; got:\n%s", prefix, s)
		}
	}
}
