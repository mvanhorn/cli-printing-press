package pipeline

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const PhaseReceiptSchemaVersion = 1

const (
	PhaseReceiptEntered   = "entered"
	PhaseReceiptCompleted = "completed"
	PhaseReceiptSkipped   = "skipped"
	PhaseReceiptBlocked   = "blocked"
	PhaseReceiptFailed    = "failed"
)

var printingPressReceiptPhases = []string{
	"02-run-initialization",
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
	"13-sync-param-drop-gate",
	"14-agentic-skill-review",
	"15-readme-skill-agents-correctness-audit",
	"16-agentic-output-review",
	"17-local-code-review",
	"18-dogfood-testing",
	"19-polish",
	"20-promote-and-archive",
	"21-next-steps",
}

// printingPressAlternateNext is the skill's documented non-linear control-flow
// graph: every handoff the phase text prescribes that departs from the canonical
// linear order. The absorb and reachability gates route discovery rework back to
// the sniff gates, a build that proves infeasible returns to the absorb gate for
// re-approval, the shipcheck hold jumps straight to promote-and-archive, a local
// code review that uncovers a scope change returns to the absorb gate, and the
// promote gate backtracks to dogfood when an acceptance marker is missing. Every
// other handoff stays on the canonical linear order.
var printingPressAlternateNext = map[string][]string{
	"08-ecosystem-absorb-gate": {"06-browser-sniff-gate", "07-crowd-sniff-gate"},
	"09-api-reachability-gate": {"06-browser-sniff-gate"},
	"11-build-the-goat":        {"08-ecosystem-absorb-gate"},
	"12-shipcheck":             {"20-promote-and-archive"},
	"17-local-code-review":     {"08-ecosystem-absorb-gate"},
	"20-promote-and-archive":   {"18-dogfood-testing"},
}

// PhaseReceipt is one append-only transition in a skill-run phase ledger.
// Domain artifacts remain authoritative; receipts only record execution order
// and point at the evidence a resumed agent should load.
type PhaseReceipt struct {
	SchemaVersion int       `json:"schema_version"`
	Sequence      int       `json:"sequence"`
	RunID         string    `json:"run_id"`
	Phase         string    `json:"phase"`
	Event         string    `json:"event"`
	PhaseFile     string    `json:"phase_file,omitempty"`
	Next          string    `json:"next,omitempty"`
	Evidence      []string  `json:"evidence,omitempty"`
	Note          string    `json:"note,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type PhaseReceiptOptions struct {
	Path     string
	RunID    string
	Phase    string
	Next     string
	Evidence []string
	Note     string
	Resume   bool
}

// InitPhaseReceipts creates a new ledger with the run-initialization phase
// completed. Preflight runs before a run ID and pipeline directory exist, so
// run initialization is the first phase that can durably record a receipt.
func InitPhaseReceipts(opts PhaseReceiptOptions) (*PhaseReceipt, bool, error) {
	if err := validatePhaseReceiptIdentity(opts); err != nil {
		return nil, false, err
	}
	if err := rejectExplicitNext(opts); err != nil {
		return nil, false, err
	}
	if opts.Phase != printingPressReceiptPhases[0] {
		return nil, false, fmt.Errorf("phase receipt ledger must initialize with %q", printingPressReceiptPhases[0])
	}

	if info, err := os.Lstat(opts.Path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("refusing symlinked phase receipt ledger: %s", opts.Path)
		}
		if info.Size() > 0 {
			receipts, readErr := ReadPhaseReceipts(opts.Path)
			if readErr != nil {
				return nil, false, readErr
			}
			last, lastErr := lastPhaseReceipt(receipts, opts.RunID)
			if lastErr != nil {
				return nil, false, lastErr
			}
			if len(receipts) == 1 && last.Phase == opts.Phase && last.Event == PhaseReceiptCompleted {
				return last, false, nil
			}
			return nil, false, fmt.Errorf("phase receipt ledger already initialized: %s", opts.Path)
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("inspecting phase receipt ledger: %w", err)
	}

	if err := validateEvidence(opts.Evidence); err != nil {
		return nil, false, err
	}
	receipt := newPhaseReceipt(1, opts, PhaseReceiptCompleted, expectedNextPhase(opts.Phase))
	if err := appendPhaseReceipt(opts.Path, receipt); err != nil {
		return nil, false, err
	}
	return receipt, true, nil
}

// EnterPhase records that the agent loaded and entered the next expected phase.
// Re-entering the same active phase is idempotent. A blocked or failed phase can
// be re-entered only with Resume set.
func EnterPhase(opts PhaseReceiptOptions) (*PhaseReceipt, bool, error) {
	if err := validatePhaseReceiptIdentity(opts); err != nil {
		return nil, false, err
	}
	if err := rejectExplicitNext(opts); err != nil {
		return nil, false, err
	}
	receipts, err := ReadPhaseReceipts(opts.Path)
	if err != nil {
		return nil, false, err
	}
	last, err := lastPhaseReceipt(receipts, opts.RunID)
	if err != nil {
		return nil, false, err
	}

	switch {
	case last.Event == PhaseReceiptEntered && last.Phase == opts.Phase:
		return last, false, nil
	case (last.Event == PhaseReceiptBlocked || last.Event == PhaseReceiptFailed) && last.Phase == opts.Phase:
		if !opts.Resume {
			return nil, false, fmt.Errorf("phase %s is %s; pass --resume after resolving the recorded blocker", opts.Phase, last.Event)
		}
	case last.Event == PhaseReceiptCompleted || last.Event == PhaseReceiptSkipped:
		if last.Next != opts.Phase {
			return nil, false, fmt.Errorf("phase transition mismatch: receipt names next phase %q, cannot enter %q", last.Next, opts.Phase)
		}
	default:
		return nil, false, fmt.Errorf("cannot enter phase %q after %s event for phase %q", opts.Phase, last.Event, last.Phase)
	}

	receipt := newPhaseReceipt(last.Sequence+1, opts, PhaseReceiptEntered, "")
	if err := appendPhaseReceipt(opts.Path, receipt); err != nil {
		return nil, false, err
	}
	return receipt, true, nil
}

// CompletePhase records a completed or explicitly skipped phase and the next
// phase to enter. Skips require a reason so optional routes cannot disappear
// silently.
func CompletePhase(opts PhaseReceiptOptions, skipped bool) (*PhaseReceipt, bool, error) {
	if err := validatePhaseReceiptIdentity(opts); err != nil {
		return nil, false, err
	}
	if skipped && strings.TrimSpace(opts.Next) != "" {
		return nil, false, errors.New("--next cannot be combined with --skip")
	}
	if skipped && strings.TrimSpace(opts.Note) == "" {
		return nil, false, errors.New("skipped phase requires --note")
	}
	if err := validateEvidence(opts.Evidence); err != nil {
		return nil, false, err
	}
	next, err := resolveCompleteNext(opts.Phase, opts.Next)
	if err != nil {
		return nil, false, err
	}
	if next != expectedNextPhase(opts.Phase) && strings.TrimSpace(opts.Note) == "" {
		return nil, false, fmt.Errorf("alternate handoff to %q requires --note", next)
	}

	receipts, err := ReadPhaseReceipts(opts.Path)
	if err != nil {
		return nil, false, err
	}
	last, err := lastPhaseReceipt(receipts, opts.RunID)
	if err != nil {
		return nil, false, err
	}

	event := PhaseReceiptCompleted
	if skipped {
		event = PhaseReceiptSkipped
	}
	if last.Phase == opts.Phase && last.Event == event {
		if last.Next != next {
			return nil, false, fmt.Errorf("phase %q already completed with next %q; re-completing with %q is not allowed", opts.Phase, last.Next, next)
		}
		return last, false, nil
	}
	if last.Event != PhaseReceiptEntered || last.Phase != opts.Phase {
		return nil, false, fmt.Errorf("cannot complete phase %q: latest receipt is %s for phase %q", opts.Phase, last.Event, last.Phase)
	}

	receipt := newPhaseReceipt(last.Sequence+1, opts, event, next)
	if err := appendPhaseReceipt(opts.Path, receipt); err != nil {
		return nil, false, err
	}
	return receipt, true, nil
}

// StopPhase records a resumable blocker or a failed phase. Both statuses require
// a concise reason and deliberately carry no next phase.
func StopPhase(opts PhaseReceiptOptions, failed bool) (*PhaseReceipt, bool, error) {
	if err := validatePhaseReceiptIdentity(opts); err != nil {
		return nil, false, err
	}
	if err := rejectExplicitNext(opts); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(opts.Note) == "" {
		return nil, false, errors.New("stopped phase requires --note")
	}
	if err := validateEvidence(opts.Evidence); err != nil {
		return nil, false, err
	}

	receipts, err := ReadPhaseReceipts(opts.Path)
	if err != nil {
		return nil, false, err
	}
	last, err := lastPhaseReceipt(receipts, opts.RunID)
	if err != nil {
		return nil, false, err
	}

	event := PhaseReceiptBlocked
	if failed {
		event = PhaseReceiptFailed
	}
	if last.Phase == opts.Phase && last.Event == event {
		return last, false, nil
	}
	if last.Event != PhaseReceiptEntered || last.Phase != opts.Phase {
		return nil, false, fmt.Errorf("cannot stop phase %q: latest receipt is %s for phase %q", opts.Phase, last.Event, last.Phase)
	}

	receipt := newPhaseReceipt(last.Sequence+1, opts, event, "")
	if err := appendPhaseReceipt(opts.Path, receipt); err != nil {
		return nil, false, err
	}
	return receipt, true, nil
}

func LatestPhaseReceipt(path, runID string) (*PhaseReceipt, error) {
	if err := validatePhaseReceiptLookup(path, runID); err != nil {
		return nil, err
	}
	receipts, err := ReadPhaseReceipts(path)
	if err != nil {
		return nil, err
	}
	return lastPhaseReceipt(receipts, runID)
}

func ReadPhaseReceipts(path string) ([]PhaseReceipt, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("reading phase receipt ledger: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlinked phase receipt ledger: %s", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening phase receipt ledger: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var receipts []PhaseReceipt
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			return nil, fmt.Errorf("parsing phase receipt ledger line %d: blank lines are not allowed", line)
		}
		var receipt PhaseReceipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil {
			return nil, fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
		}
		if err := validateStoredPhaseReceipt(receipt, line); err != nil {
			return nil, err
		}
		if receipt.Sequence != line {
			return nil, fmt.Errorf("parsing phase receipt ledger line %d: sequence is %d", line, receipt.Sequence)
		}
		receipts = append(receipts, receipt)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading phase receipt ledger: %w", err)
	}
	if len(receipts) == 0 {
		return nil, errors.New("phase receipt ledger is empty")
	}
	if err := validateStoredPhaseTransitions(receipts); err != nil {
		return nil, err
	}
	return receipts, nil
}

func newPhaseReceipt(sequence int, opts PhaseReceiptOptions, event, next string) *PhaseReceipt {
	phase := strings.TrimSpace(opts.Phase)
	return &PhaseReceipt{
		SchemaVersion: PhaseReceiptSchemaVersion,
		Sequence:      sequence,
		RunID:         strings.TrimSpace(opts.RunID),
		Phase:         phase,
		Event:         event,
		PhaseFile:     "phases/" + phase + ".md",
		Next:          next,
		Evidence:      append([]string(nil), opts.Evidence...),
		Note:          strings.TrimSpace(opts.Note),
		RecordedAt:    time.Now().UTC(),
	}
}

func appendPhaseReceipt(path string, receipt *PhaseReceipt) (returnErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating phase receipt directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked phase receipt ledger: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspecting phase receipt ledger: %w", err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encoding phase receipt: %w", err)
	}
	data = append(data, '\n')

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening phase receipt ledger for append: %w", err)
	}
	defer func() {
		if err := file.Close(); returnErr == nil && err != nil {
			returnErr = fmt.Errorf("closing phase receipt ledger: %w", err)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("appending phase receipt: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("setting phase receipt permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing phase receipt ledger: %w", err)
	}
	return nil
}

func lastPhaseReceipt(receipts []PhaseReceipt, runID string) (*PhaseReceipt, error) {
	if len(receipts) == 0 {
		return nil, errors.New("phase receipt ledger is empty")
	}
	last := receipts[len(receipts)-1]
	if last.RunID != strings.TrimSpace(runID) {
		return nil, fmt.Errorf("run ID mismatch: ledger is for %q, command named %q", last.RunID, runID)
	}
	return &last, nil
}

func validatePhaseReceiptIdentity(opts PhaseReceiptOptions) error {
	if err := validatePhaseReceiptLookup(opts.Path, opts.RunID); err != nil {
		return err
	}
	if strings.TrimSpace(opts.Phase) == "" {
		return errors.New("phase is required")
	}
	phase := strings.TrimSpace(opts.Phase)
	if phaseIndex(phase) == -1 {
		return fmt.Errorf("unknown Printing Press phase %q", phase)
	}
	if err := validateReceiptText("note", opts.Note, 512); err != nil {
		return err
	}
	return nil
}

func validatePhaseReceiptLookup(path, runID string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("phase receipt path is required")
	}
	if !filepath.IsAbs(path) {
		return errors.New("phase receipt path must be absolute")
	}
	if strings.TrimSpace(runID) == "" {
		return errors.New("run ID is required")
	}
	return validateReceiptText("run ID", runID, 256)
}

func validateEvidence(paths []string) error {
	if len(paths) > 16 {
		return errors.New("phase receipt cannot contain more than 16 evidence paths")
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return errors.New("evidence path cannot be empty")
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("evidence path must be absolute: %q", path)
		}
		if err := validateReceiptText("evidence path", path, 4096); err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("checking evidence path %q: %w", path, err)
		}
	}
	return nil
}

func validateStoredPhaseReceipt(receipt PhaseReceipt, line int) error {
	if receipt.SchemaVersion != PhaseReceiptSchemaVersion {
		return fmt.Errorf("parsing phase receipt ledger line %d: unsupported schema version %d", line, receipt.SchemaVersion)
	}
	if receipt.Sequence < 1 || receipt.RunID == "" || receipt.Phase == "" || receipt.Event == "" {
		return fmt.Errorf("parsing phase receipt ledger line %d: missing required identity field", line)
	}
	if phaseIndex(receipt.Phase) == -1 || receipt.PhaseFile != "phases/"+receipt.Phase+".md" {
		return fmt.Errorf("parsing phase receipt ledger line %d: invalid phase identity", line)
	}
	if err := validateReceiptText("run ID", receipt.RunID, 256); err != nil {
		return fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
	}
	if err := validateReceiptText("note", receipt.Note, 512); err != nil {
		return fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
	}
	if len(receipt.Evidence) > 16 {
		return fmt.Errorf("parsing phase receipt ledger line %d: too many evidence paths", line)
	}
	for _, path := range receipt.Evidence {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("parsing phase receipt ledger line %d: evidence path is not absolute", line)
		}
		if err := validateReceiptText("evidence path", path, 4096); err != nil {
			return fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
		}
	}
	switch receipt.Event {
	case PhaseReceiptEntered:
		if receipt.Next != "" || len(receipt.Evidence) != 0 || receipt.Note != "" {
			return fmt.Errorf("parsing phase receipt ledger line %d: entered receipt has completion fields", line)
		}
	case PhaseReceiptCompleted:
		if err := validateExpectedNext(receipt.Phase, receipt.Next); err != nil {
			return fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
		}
	case PhaseReceiptSkipped:
		if err := validateExpectedNext(receipt.Phase, receipt.Next); err != nil {
			return fmt.Errorf("parsing phase receipt ledger line %d: %w", line, err)
		}
		if receipt.Note == "" {
			return fmt.Errorf("parsing phase receipt ledger line %d: skipped receipt has no reason", line)
		}
	case PhaseReceiptBlocked, PhaseReceiptFailed:
		if receipt.Next != "" || receipt.Note == "" {
			return fmt.Errorf("parsing phase receipt ledger line %d: stopped receipt has invalid fields", line)
		}
	default:
		return fmt.Errorf("parsing phase receipt ledger line %d: unknown event %q", line, receipt.Event)
	}
	if receipt.RecordedAt.IsZero() {
		return fmt.Errorf("parsing phase receipt ledger line %d: missing recorded_at", line)
	}
	return nil
}

func validateStoredPhaseTransitions(receipts []PhaseReceipt) error {
	first := receipts[0]
	if first.Event != PhaseReceiptCompleted || first.Phase != printingPressReceiptPhases[0] {
		return fmt.Errorf("parsing phase receipt ledger line 1: invalid initialization receipt")
	}

	for i := 1; i < len(receipts); i++ {
		previous := receipts[i-1]
		current := receipts[i]
		line := i + 1
		if current.RunID != first.RunID {
			return fmt.Errorf("parsing phase receipt ledger line %d: run ID differs from initialization receipt", line)
		}

		if current.Event == PhaseReceiptEntered {
			switch previous.Event {
			case PhaseReceiptCompleted, PhaseReceiptSkipped:
				if previous.Next != current.Phase {
					return fmt.Errorf("parsing phase receipt ledger line %d: entered phase does not match previous next phase", line)
				}
			case PhaseReceiptBlocked, PhaseReceiptFailed:
				if previous.Phase != current.Phase {
					return fmt.Errorf("parsing phase receipt ledger line %d: resumed phase differs from stopped phase", line)
				}
			default:
				return fmt.Errorf("parsing phase receipt ledger line %d: entered receipt does not follow a handoff", line)
			}
			continue
		}

		if previous.Event != PhaseReceiptEntered || previous.Phase != current.Phase {
			return fmt.Errorf("parsing phase receipt ledger line %d: phase exit does not follow matching entry", line)
		}
	}
	return nil
}

func validateExpectedNext(phase, next string) error {
	expected := expectedNextPhase(phase)
	if expected == "" {
		return fmt.Errorf("unknown Printing Press phase %q", phase)
	}
	trimmed := strings.TrimSpace(next)
	if trimmed == expected {
		return nil
	}
	alternates := printingPressAlternateNext[phase]
	for _, alternate := range alternates {
		if trimmed == alternate {
			return nil
		}
	}
	if len(alternates) > 0 {
		return fmt.Errorf("phase %q must hand off to %q or a documented alternate (%s)", phase, expected, strings.Join(alternates, ", "))
	}
	return fmt.Errorf("phase %q must hand off to %q", phase, expected)
}

// resolveCompleteNext picks the next phase a completion records. An empty request
// keeps the canonical linear order; an explicit request must name either the
// canonical next or one of the documented alternates for the phase.
func resolveCompleteNext(phase, next string) (string, error) {
	canonical := expectedNextPhase(phase)
	if canonical == "" {
		return "", fmt.Errorf("unknown Printing Press phase %q", phase)
	}
	trimmed := strings.TrimSpace(next)
	if trimmed == "" || trimmed == canonical {
		return canonical, nil
	}
	for _, alternate := range printingPressAlternateNext[phase] {
		if trimmed == alternate {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("phase %q cannot hand off to %q; allowed: %s", phase, trimmed, strings.Join(allowedNextPhases(phase), ", "))
}

// allowedNextPhases orders the canonical next phase first so a rejected handoff
// tells the caller the default route before the documented exceptions.
func allowedNextPhases(phase string) []string {
	allowed := []string{expectedNextPhase(phase)}
	return append(allowed, printingPressAlternateNext[phase]...)
}

// rejectExplicitNext guards the paths that do not record a next phase. Init,
// enter, and stop derive sequencing themselves, so a caller passing --next there
// has misunderstood the command rather than requested a documented alternate.
func rejectExplicitNext(opts PhaseReceiptOptions) error {
	if strings.TrimSpace(opts.Next) != "" {
		return errors.New("--next is only valid when completing a phase")
	}
	return nil
}

func expectedNextPhase(phase string) string {
	index := phaseIndex(strings.TrimSpace(phase))
	if index == -1 {
		return ""
	}
	if index+1 < len(printingPressReceiptPhases) {
		return printingPressReceiptPhases[index+1]
	}
	return "done"
}

func phaseIndex(phase string) int {
	for index, candidate := range printingPressReceiptPhases {
		if candidate == phase {
			return index
		}
	}
	return -1
}

func validateReceiptText(field, value string, maxLength int) error {
	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", field, maxLength)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s cannot contain a newline", field)
	}
	return nil
}
