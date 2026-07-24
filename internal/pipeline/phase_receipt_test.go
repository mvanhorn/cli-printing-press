package pipeline

import (
	"os"
	"path/filepath"
	"strings"
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

// enterAfterCanonicalChain exists so an alternate-handoff test can start from a
// realistic ledger: it stops at the target phase entered but not completed, the
// only state from which that phase's exit can be exercised.
func enterAfterCanonicalChain(t *testing.T, path, target string) {
	t.Helper()
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)
	for _, phase := range printingPressReceiptPhases[1:] {
		opts := receiptOptions(t, path, phase)
		_, _, err := EnterPhase(opts)
		require.NoError(t, err)
		if phase == target {
			return
		}
		_, _, err = CompletePhase(opts, false)
		require.NoError(t, err)
	}
}

func TestPhaseReceiptsAllowShipcheckHoldHandoff(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "12-shipcheck")

	hold := receiptOptions(t, path, "12-shipcheck")
	hold.Next = "20-promote-and-archive"
	hold.Note = "hold: verify red"
	receipt, recorded, err := CompletePhase(hold, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, "20-promote-and-archive", receipt.Next)

	// Re-running the hold completion with the same --next is idempotent.
	retried, recorded, err := CompletePhase(hold, false)
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Equal(t, "20-promote-and-archive", retried.Next)

	// Re-running with a different documented next after completion is a conflict.
	conflict := hold
	conflict.Next = "13-sync-param-drop-gate"
	_, _, err = CompletePhase(conflict, false)
	require.ErrorContains(t, err, "already completed with next")

	promote := receiptOptions(t, path, "20-promote-and-archive")
	entered, recorded, err := EnterPhase(promote)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, PhaseReceiptEntered, entered.Event)
}

func TestPhaseReceiptsAllowBuildInfeasibleReturnAndReplayLoop(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "11-build-the-goat")

	infeasible := receiptOptions(t, path, "11-build-the-goat")
	infeasible.Next = "08-ecosystem-absorb-gate"
	infeasible.Note = "manifest needs rework"
	receipt, recorded, err := CompletePhase(infeasible, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, "08-ecosystem-absorb-gate", receipt.Next)

	// Re-enter the absorb gate, complete it canonically, and continue forward
	// again through the reachability gate. The loop must replay cleanly.
	absorb := receiptOptions(t, path, "08-ecosystem-absorb-gate")
	_, recorded, err = EnterPhase(absorb)
	require.NoError(t, err)
	assert.True(t, recorded)
	reapproved, _, err := CompletePhase(absorb, false)
	require.NoError(t, err)
	assert.Equal(t, "09-api-reachability-gate", reapproved.Next)

	reach := receiptOptions(t, path, "09-api-reachability-gate")
	_, recorded, err = EnterPhase(reach)
	require.NoError(t, err)
	assert.True(t, recorded)

	latest, err := LatestPhaseReceipt(path, "run-123")
	require.NoError(t, err)
	assert.Equal(t, "09-api-reachability-gate", latest.Phase)
	assert.Equal(t, PhaseReceiptEntered, latest.Event)
}

func TestPhaseReceiptsAllowPromoteBacktrackToDogfood(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "20-promote-and-archive")

	backtrack := receiptOptions(t, path, "20-promote-and-archive")
	backtrack.Next = "18-dogfood-testing"
	backtrack.Note = "acceptance marker missing"
	receipt, recorded, err := CompletePhase(backtrack, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, "18-dogfood-testing", receipt.Next)

	dogfood := receiptOptions(t, path, "18-dogfood-testing")
	entered, recorded, err := EnterPhase(dogfood)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, PhaseReceiptEntered, entered.Event)
}

func TestPhaseReceiptsRejectUndocumentedNext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "12-shipcheck")

	bogus := receiptOptions(t, path, "12-shipcheck")
	bogus.Next = "19-polish"
	_, _, err := CompletePhase(bogus, false)
	require.ErrorContains(t, err, `phase "12-shipcheck" cannot hand off to "19-polish"`)
	require.ErrorContains(t, err, "allowed: 13-sync-param-drop-gate, 20-promote-and-archive")
}

func TestPhaseReceiptsRejectNextOnEnterInitAndStop(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	initOpts := receiptOptions(t, path, "02-run-initialization")
	initOpts.Next = "03-resolve-and-reuse"
	_, _, err := InitPhaseReceipts(initOpts)
	require.ErrorContains(t, err, "--next is only valid when completing a phase")

	_, _, err = InitPhaseReceipts(receiptOptions(t, path, "02-run-initialization"))
	require.NoError(t, err)

	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	enter.Next = "04-research-brief"
	_, _, err = EnterPhase(enter)
	require.ErrorContains(t, err, "--next is only valid when completing a phase")

	_, _, err = EnterPhase(receiptOptions(t, path, "03-resolve-and-reuse"))
	require.NoError(t, err)

	stop := receiptOptions(t, path, "03-resolve-and-reuse")
	stop.Next = "04-research-brief"
	stop.Note = "waiting"
	_, _, err = StopPhase(stop, false)
	require.ErrorContains(t, err, "--next is only valid when completing a phase")
}

func TestReadPhaseReceiptsAcceptsAlternateStoredHandoff(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "12-shipcheck")
	hold := receiptOptions(t, path, "12-shipcheck")
	hold.Next = "20-promote-and-archive"
	hold.Note = "hold: verify red"
	_, _, err := CompletePhase(hold, false)
	require.NoError(t, err)

	receipts, err := ReadPhaseReceipts(path)
	require.NoError(t, err)
	assert.Equal(t, "20-promote-and-archive", receipts[len(receipts)-1].Next)

	// A stored next that is neither canonical nor a documented alternate is
	// still rejected on read.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":1,"sequence":1,"run_id":"run-123","phase":"02-run-initialization","event":"completed","phase_file":"phases/02-run-initialization.md","next":"19-polish","recorded_at":"2026-07-22T00:00:00Z"}`+"\n"), 0o600))
	_, err = ReadPhaseReceipts(path)
	require.ErrorContains(t, err, `phase "02-run-initialization" must hand off to "03-resolve-and-reuse"`)
}

func TestPhaseReceiptsAcceptEveryDocumentedAlternateHandoff(t *testing.T) {
	t.Parallel()

	edges := []struct {
		phase string
		next  string
	}{
		{"08-ecosystem-absorb-gate", "06-browser-sniff-gate"},
		{"08-ecosystem-absorb-gate", "07-crowd-sniff-gate"},
		{"09-api-reachability-gate", "06-browser-sniff-gate"},
		{"11-build-the-goat", "08-ecosystem-absorb-gate"},
		{"12-shipcheck", "20-promote-and-archive"},
		{"17-local-code-review", "08-ecosystem-absorb-gate"},
		{"20-promote-and-archive", "18-dogfood-testing"},
	}

	for _, edge := range edges {
		t.Run(edge.phase+"_to_"+edge.next, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
			enterAfterCanonicalChain(t, path, edge.phase)

			opts := receiptOptions(t, path, edge.phase)
			opts.Next = edge.next
			opts.Note = "documented rework"
			receipt, recorded, err := CompletePhase(opts, false)
			require.NoError(t, err)
			assert.True(t, recorded)
			assert.Equal(t, edge.next, receipt.Next)
		})
	}
}

func TestPhaseReceiptsRejectAlternateHandoffWithoutNote(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "12-shipcheck")

	hold := receiptOptions(t, path, "12-shipcheck")
	hold.Next = "20-promote-and-archive"
	_, _, err := CompletePhase(hold, false)
	require.ErrorContains(t, err, `alternate handoff to "20-promote-and-archive" requires --note`)
}

func TestPhaseReceiptsRejectNextCombinedWithSkip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "12-shipcheck")

	// The skip+next guard must fire before the skip note check, so an empty note
	// still surfaces the combination error rather than "requires --note".
	skip := receiptOptions(t, path, "12-shipcheck")
	skip.Next = "20-promote-and-archive"
	_, _, err := CompletePhase(skip, true)
	require.ErrorContains(t, err, "--next cannot be combined with --skip")
}

func TestPhaseReceiptsReplayDiscoveryReworkLoop(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "08-ecosystem-absorb-gate")

	rework := receiptOptions(t, path, "08-ecosystem-absorb-gate")
	rework.Next = "06-browser-sniff-gate"
	rework.Note = "browser-sniff decision missing"
	receipt, recorded, err := CompletePhase(rework, false)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, "06-browser-sniff-gate", receipt.Next)

	// Re-enter the browser-sniff gate, complete it and the crowd-sniff gate
	// canonically, then arrive back at the absorb gate: the discovery-rework
	// loop must replay cleanly.
	for _, phase := range []string{"06-browser-sniff-gate", "07-crowd-sniff-gate"} {
		opts := receiptOptions(t, path, phase)
		_, recorded, err := EnterPhase(opts)
		require.NoError(t, err)
		require.True(t, recorded)
		_, _, err = CompletePhase(opts, false)
		require.NoError(t, err)
	}

	absorb := receiptOptions(t, path, "08-ecosystem-absorb-gate")
	entered, recorded, err := EnterPhase(absorb)
	require.NoError(t, err)
	require.True(t, recorded)
	assert.Equal(t, PhaseReceiptEntered, entered.Event)
	assert.Equal(t, "08-ecosystem-absorb-gate", entered.Phase)
}

func TestReadPhaseReceiptsReportsAlternatesForReworkPhase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	enterAfterCanonicalChain(t, path, "08-ecosystem-absorb-gate")
	rework := receiptOptions(t, path, "08-ecosystem-absorb-gate")
	rework.Next = "06-browser-sniff-gate"
	rework.Note = "browser-sniff decision missing"
	_, _, err := CompletePhase(rework, false)
	require.NoError(t, err)

	receipts, err := ReadPhaseReceipts(path)
	require.NoError(t, err)
	assert.Equal(t, "06-browser-sniff-gate", receipts[len(receipts)-1].Next)

	// A stored next that is neither canonical nor a documented alternate is
	// rejected on read, and the message lists the alternates for a phase that
	// has them.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":1,"sequence":1,"run_id":"run-123","phase":"08-ecosystem-absorb-gate","event":"completed","phase_file":"phases/08-ecosystem-absorb-gate.md","next":"19-polish","recorded_at":"2026-07-22T00:00:00Z"}`+"\n"), 0o600))
	_, err = ReadPhaseReceipts(path)
	require.ErrorContains(t, err, `phase "08-ecosystem-absorb-gate" must hand off to "09-api-reachability-gate" or a documented alternate (06-browser-sniff-gate, 07-crowd-sniff-gate)`)
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

// appendTornReceipt simulates an interrupted append: a partial JSON object with
// no closing brace lands as the final line of the ledger, exactly what a torn
// write from appendPhaseReceipt leaves behind when the process dies mid-write.
func appendTornReceipt(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	_, err = file.WriteString(`{"schema_version":1,"sequence":99,"run_id":"run-123"`)
	require.NoError(t, err)
}

func TestReadPhaseReceiptsSurvivesTornFinalLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)

	// Build a valid three-receipt prefix: init completed, then entered + completed
	// for the resolve-and-reuse phase.
	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)
	_, _, err = CompletePhase(enter, false)
	require.NoError(t, err)

	// A torn append leaves a truncated final line.
	appendTornReceipt(t, path)

	// The reader treats the truncated tail as a write that never happened and
	// returns the valid prefix.
	receipts, err := ReadPhaseReceipts(path)
	require.NoError(t, err)
	assert.Len(t, receipts, 3)
	assert.Equal(t, 3, receipts[len(receipts)-1].Sequence)
	assert.Equal(t, PhaseReceiptCompleted, receipts[len(receipts)-1].Event)

	// Resuming the run must keep working: the next receipt appends with the
	// sequence number that follows the valid prefix, not the torn tail.
	enterNext := receiptOptions(t, path, "04-research-brief")
	receipt, recorded, err := EnterPhase(enterNext)
	require.NoError(t, err)
	assert.True(t, recorded)
	assert.Equal(t, 4, receipt.Sequence)
	assert.Equal(t, "04-research-brief", receipt.Phase)
}

func TestReadPhaseReceiptsSurvivesBlankFinalLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)

	// A newline-only trailing line is a torn write of just the record separator.
	// The init receipt already ends in a newline, so one more yields a single
	// blank final line.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))

	receipts, err := ReadPhaseReceipts(path)
	require.NoError(t, err)
	assert.Len(t, receipts, 1)
}

func TestReadPhaseReceiptsRejectsMalformedMiddleLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)
	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)

	// Inject a malformed line between the two valid receipts so the corruption
	// is in the middle of the ledger, not at its tail.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	injected := append([]string{lines[0], "{not-json"}, lines[1:]...)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(injected, "\n")), 0o600))

	_, err = ReadPhaseReceipts(path)
	require.ErrorContains(t, err, "line 2")
}

func TestLatestPhaseReceiptReturnsLastValidOnTornLedger(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "phase-receipts.jsonl")
	_, _, err := InitPhaseReceipts(receiptOptions(t, path, printingPressReceiptPhases[0]))
	require.NoError(t, err)
	enter := receiptOptions(t, path, "03-resolve-and-reuse")
	_, _, err = EnterPhase(enter)
	require.NoError(t, err)

	appendTornReceipt(t, path)

	receipt, err := LatestPhaseReceipt(path, "run-123")
	require.NoError(t, err)
	assert.Equal(t, "03-resolve-and-reuse", receipt.Phase)
	assert.Equal(t, PhaseReceiptEntered, receipt.Event)
	assert.Equal(t, 2, receipt.Sequence)
}
