package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func receiptOptions(t *testing.T, path, phase string) PhaseReceiptOptions {
	t.Helper()
	return PhaseReceiptOptions{
		Path:  path,
		RunID: "run-123",
		Phase: phase,
	}
}

func TestPhaseReceiptsTrackOrderedTransitions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pipeline", "phase-receipts.jsonl")
	evidence := filepath.Join(t.TempDir(), "brief.md")
	require.NoError(t, os.WriteFile(evidence, []byte("brief"), 0o600))

	initOpts := receiptOptions(t, path, "02-run-initialization")
	initReceipt, recorded, err := InitPhaseReceipts(initOpts)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, 1, initReceipt.Sequence)
	assert.Equal(t, PhaseReceiptCompleted, initReceipt.Event)

	retriedInit, recorded, err := InitPhaseReceipts(initOpts)
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, initReceipt, retriedInit)

	enterOpts := receiptOptions(t, path, "03-resolve-and-reuse")
	enterReceipt, recorded, err := EnterPhase(enterOpts)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, 2, enterReceipt.Sequence)

	duplicate, recorded, err := EnterPhase(enterOpts)
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, enterReceipt.Sequence, duplicate.Sequence)

	completeOpts := enterOpts
	completeOpts.Evidence = []string{evidence}
	completeReceipt, recorded, err := CompletePhase(completeOpts, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, 3, completeReceipt.Sequence)
	assert.Equal(t, []string{evidence}, completeReceipt.Evidence)

	retriedComplete, recorded, err := CompletePhase(completeOpts, false)
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, completeReceipt, retriedComplete)

	latest, err := LatestPhaseReceipt(path, "run-123")
	require.NoError(t, err)
	assert.Equal(t, completeReceipt, latest)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPhaseReceiptsWalkCanonicalChainToDone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)

	for _, phase := range printingPressReceiptPhases[1:] {
		opts := receiptOptions(t, path, phase)
		_, recorded, err := EnterPhase(opts)
		require.NoError(t, err)
		require.True(t, recorded)
		_, _, err = CompletePhase(opts, false)
		require.NoError(t, err)
	}

	latest, err := LatestPhaseReceipt(path, "run-123")
	require.NoError(t, err)
	assert.Equal(t, "21-next-steps", latest.Phase)
	assert.Equal(t, PhaseReceiptCompleted, latest.Event)
	assert.Equal(t, "done", latest.Next)
}

func TestPhaseReceiptsRejectOutOfOrderEntry(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.NoError(t, err)

	wrong := receiptOptions(t, path, "04-research-brief")
	_, _, err = EnterPhase(wrong)
	require.ErrorContains(t, err, `receipt names next phase "03-resolve-and-reuse"`)
}

func TestPhaseReceiptsRequireAbsoluteLedgerPath(t *testing.T) {
	t.Parallel()

	opts := receiptOptions(t, "phase-receipts.jsonl", "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.ErrorContains(t, err, "phase receipt path must be absolute")
}

func TestPhaseReceiptsRequireEntryBeforeCompletion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.NoError(t, err)

	complete := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = CompletePhase(complete, false)
	require.ErrorContains(t, err, "latest receipt is completed")
}

func TestPhaseReceiptsEnforceCanonicalPhaseMetadataAndNext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	opts.Phase = "03-resolve-and-reuse"
	_, _, err := InitPhaseReceipts(opts)
	require.ErrorContains(t, err, `must initialize with "02-run-initialization"`)

	opts.Phase = "02-run-initialization"
	_, _, err = InitPhaseReceipts(opts)
	require.NoError(t, err)

	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	enter.Phase = "unknown"
	_, _, err = EnterPhase(enter)
	require.ErrorContains(t, err, `unknown Printing Press phase "unknown"`)

	enter.Phase = "03-resolve-and-reuse"
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)

	complete := enter
	receipt, _, err := CompletePhase(complete, false)
	require.NoError(t, err)
	assert.Equal(t, "phases/03-resolve-and-reuse.md", receipt.PhaseFile)
	assert.Equal(t, "04-research-brief", receipt.Next)
}

func TestPhaseReceiptsRequireSkipReasonAndExistingEvidence(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.NoError(t, err)

	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)

	skip := enter
	_, _, err = CompletePhase(skip, true)
	require.ErrorContains(t, err, "requires --note")

	skip.Note = "not applicable"
	skip.Evidence = []string{filepath.Join(t.TempDir(), "missing.json")}
	_, _, err = CompletePhase(skip, true)
	require.ErrorContains(t, err, "checking evidence path")
}

func TestPhaseReceiptsBlockAndResumeExplicitly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.NoError(t, err)

	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)

	stop := enter
	stop.Note = "waiting for user choice"
	blocked, recorded, err := StopPhase(stop, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, PhaseReceiptBlocked, blocked.Event)

	retriedBlock, recorded, err := StopPhase(stop, false)
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, blocked, retriedBlock)

	_, _, err = EnterPhase(enter)
	require.ErrorContains(t, err, "pass --resume")

	enter.Resume = true
	resumed, recorded, err := EnterPhase(enter)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, PhaseReceiptEntered, resumed.Event)
	assert.Equal(t, 4, resumed.Sequence)
}

func TestPhaseReceiptsRejectRunMismatchAndMalformedLedger(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	opts := receiptOptions(t, path, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.NoError(t, err)

	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	enter.RunID = "other-run"
	_, _, err = EnterPhase(enter)
	require.ErrorContains(t, err, "run ID mismatch")

	require.NoError(t, os.WriteFile(path, []byte("{broken\n"), 0o600))
	_, err = ReadPhaseReceipts(path)
	require.ErrorContains(t, err, "line 1")
}

func TestPhaseReceiptsRefuseSymlinkedLedger(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	link := filepath.Join(dir, "phase-receipts.jsonl")
	require.NoError(t, os.Symlink(target, link))

	opts := receiptOptions(t, link, "02-run-initialization")
	_, _, err := InitPhaseReceipts(opts)
	require.ErrorContains(t, err, "refusing symlinked")
}
