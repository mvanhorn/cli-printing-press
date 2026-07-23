package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhaseReceiptCommandIsHiddenAndRecordsTransitions(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(CanonicalBinaryName)
	command, _, err := root.Find([]string{"phase-receipt"})
	require.NoError(t, err)
	assert.True(t, command.Hidden)

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "init",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "02-run-initialization",
	})
	require.NoError(t, root.Execute())

	var result struct {
		Recorded bool                  `json:"recorded"`
		Receipt  pipeline.PhaseReceipt `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result.Recorded)
	assert.Equal(t, pipeline.PhaseReceiptCompleted, result.Receipt.Event)

	stdout.Reset()
	root = NewRootCommand(CanonicalBinaryName)
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "status",
		"--file", path,
		"--run-id", "run-123",
	})
	require.NoError(t, root.Execute())

	var latest pipeline.PhaseReceipt
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &latest))
	assert.Equal(t, "03-resolve-and-reuse", latest.Next)

	stdout.Reset()
	root = NewRootCommand(CanonicalBinaryName)
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "enter",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "03-resolve-and-reuse",
	})
	require.NoError(t, root.Execute())

	var entry struct {
		Previous pipeline.PhaseReceipt `json:"previous"`
		Receipt  pipeline.PhaseReceipt `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &entry))
	assert.Equal(t, "03-resolve-and-reuse", entry.Previous.Next)
	assert.Equal(t, pipeline.PhaseReceiptEntered, entry.Receipt.Event)
}
