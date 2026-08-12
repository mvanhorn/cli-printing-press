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

func TestPhaseReceiptCompleteThreadsDocumentedNextFlag(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	initReceipt, _, err := pipeline.InitPhaseReceipts(pipeline.PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: "02-run-initialization",
	})
	require.NoError(t, err)
	require.NotNil(t, initReceipt)

	// Walk the canonical chain up to and including entry into 12-shipcheck.
	for _, phase := range []string{
		"03-resolve-and-reuse",
		"04-research-brief",
		"05-pre-browser-sniff-auth-intelligence",
		"06-browser-sniff-gate",
		"07-crowd-sniff-gate",
		"08-ecosystem-absorb-gate",
		"09-api-reachability-gate",
		"10-generate",
		"11-build-the-goat",
		"12-shipcheck",
	} {
		opts := pipeline.PhaseReceiptOptions{Path: path, RunID: "run-123", Phase: phase}
		_, _, err := pipeline.EnterPhase(opts)
		require.NoError(t, err)
		if phase == "12-shipcheck" {
			break
		}
		_, _, err = pipeline.CompletePhase(opts, false)
		require.NoError(t, err)
	}

	var stdout bytes.Buffer
	root := NewRootCommand(CanonicalBinaryName)
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "complete",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "12-shipcheck",
		"--next", "20-promote-and-archive",
		"--note", "hold: verify red",
	})
	require.NoError(t, root.Execute())

	var result struct {
		Recorded bool                  `json:"recorded"`
		Receipt  pipeline.PhaseReceipt `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result.Recorded)
	assert.Equal(t, "20-promote-and-archive", result.Receipt.Next)

	// An undocumented --next is rejected as an input error.
	root = NewRootCommand(CanonicalBinaryName)
	root.SetOut(&bytes.Buffer{})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{
		"phase-receipt", "complete",
		"--file", filepath.Join(t.TempDir(), "other.jsonl"),
		"--run-id", "run-123",
		"--phase", "12-shipcheck",
		"--next", "19-polish",
	})
	require.ErrorContains(t, root.Execute(), `phase "12-shipcheck" cannot hand off to "19-polish"`)
}

func TestPhaseReceiptCompleteRejectsSkipWithNext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := pipeline.InitPhaseReceipts(pipeline.PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: "02-run-initialization",
	})
	require.NoError(t, err)
	_, _, err = pipeline.EnterPhase(pipeline.PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: "03-resolve-and-reuse",
	})
	require.NoError(t, err)

	root := NewRootCommand(CanonicalBinaryName)
	root.SetOut(&bytes.Buffer{})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{
		"phase-receipt", "complete",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "03-resolve-and-reuse",
		"--skip",
		"--next", "04-research-brief",
		"--note", "skip and reroute",
	})
	require.ErrorContains(t, root.Execute(), "--next cannot be combined with --skip")
}

func TestPhaseReceiptStopFailedRequiresExplicitResume(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := pipeline.InitPhaseReceipts(pipeline.PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: "02-run-initialization",
	})
	require.NoError(t, err)
	_, _, err = pipeline.EnterPhase(pipeline.PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: "03-resolve-and-reuse",
	})
	require.NoError(t, err)

	var stdout bytes.Buffer
	root := NewRootCommand(CanonicalBinaryName)
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "stop",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "03-resolve-and-reuse",
		"--failed",
		"--note", "generation failed",
	})
	require.NoError(t, root.Execute())

	var stopped struct {
		Recorded bool                  `json:"recorded"`
		Receipt  pipeline.PhaseReceipt `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &stopped))
	assert.True(t, stopped.Recorded)
	assert.Equal(t, pipeline.PhaseReceiptFailed, stopped.Receipt.Event)
	assert.Equal(t, "generation failed", stopped.Receipt.Note)

	root = NewRootCommand(CanonicalBinaryName)
	root.SetOut(&bytes.Buffer{})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{
		"phase-receipt", "enter",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "03-resolve-and-reuse",
	})
	require.ErrorContains(t, root.Execute(), "pass --resume")

	stdout.Reset()
	root = NewRootCommand(CanonicalBinaryName)
	root.SetOut(&stdout)
	root.SetArgs([]string{
		"phase-receipt", "enter",
		"--file", path,
		"--run-id", "run-123",
		"--phase", "03-resolve-and-reuse",
		"--resume",
	})
	require.NoError(t, root.Execute())

	var resumed struct {
		Recorded bool                  `json:"recorded"`
		Receipt  pipeline.PhaseReceipt `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resumed))
	assert.True(t, resumed.Recorded)
	assert.Equal(t, pipeline.PhaseReceiptEntered, resumed.Receipt.Event)
}
