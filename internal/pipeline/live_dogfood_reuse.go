package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// A run given --reuse-live-happy-path never really executes an annotated
// command. These reasons mark the checks it leaves out. Skipping them is what
// keeps a paid call from being billed twice, including for read-only and
// stdin-fixture commands that otherwise run for real without
// --allow-destructive.
const (
	reasonLiveHappyReused      = "live happy path reused from the recorded run of this source"
	reasonLiveHappyNotRecorded = "no recorded live happy path for this source; rerun dogfood --live --allow-destructive with operator approval"
)

// LiveHappyPathRun records a pp:live-happy-path command that really ran and
// passed. It is bound to the source fingerprint of the run that executed it,
// so a later run can trust it only for that exact source.
type LiveHappyPathRun struct {
	Command           string    `json:"command"`
	SourceFingerprint string    `json:"source_fingerprint"`
	RanAt             time.Time `json:"ran_at"`
	// Reused is true on a run that carried this entry forward from an earlier
	// marker instead of executing the command.
	Reused bool `json:"reused,omitempty"`
}

// liveHappyReuse holds the runs a dogfood invocation may reuse. Rejection
// says why a supplied marker offered none, and becomes the hollow reason so
// the operator sees the cause rather than a generic skip.
type liveHappyReuse struct {
	requested bool
	runs      map[string]LiveHappyPathRun
	rejection string
}

// loadLiveHappyReuse reads the marker named by --reuse-live-happy-path. A
// corrupt marker is an error. A missing marker, or one for other source or
// another run, is a normal outcome: it offers no runs, so the affected
// features stay hollow and publish stops without executing them.
func loadLiveHappyReuse(path string, source SourceFingerprint, apiName, runID string) (liveHappyReuse, error) {
	if strings.TrimSpace(path) == "" {
		return liveHappyReuse{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return liveHappyReuse{requested: true, rejection: reasonLiveHappyNotRecorded}, nil
	}
	if err != nil {
		return liveHappyReuse{}, fmt.Errorf("reading recorded live happy path marker: %w", err)
	}
	var marker Phase5GateMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return liveHappyReuse{}, fmt.Errorf("parsing recorded live happy path marker: %w", err)
	}
	if rejection := liveHappyMarkerRejection(marker, source, apiName, runID); rejection != "" {
		return liveHappyReuse{requested: true, rejection: rejection}, nil
	}
	runs := make(map[string]LiveHappyPathRun, len(marker.LiveHappyPathRuns))
	for _, run := range marker.LiveHappyPathRuns {
		if strings.TrimSpace(run.Command) != "" && run.SourceFingerprint == source.Digest {
			runs[run.Command] = run
		}
	}
	return liveHappyReuse{requested: true, runs: runs}, nil
}

func liveHappyMarkerRejection(marker Phase5GateMarker, source SourceFingerprint, apiName, runID string) string {
	switch {
	case marker.SchemaVersion != 1:
		return fmt.Sprintf("recorded live happy path marker has unsupported schema_version %d", marker.SchemaVersion)
	case apiName != "" && marker.APIName != apiName:
		return fmt.Sprintf("recorded live happy path is for api %q, not %q", marker.APIName, apiName)
	case runID != "" && marker.RunID != runID:
		return fmt.Sprintf("recorded live happy path is from run %q, not %q", marker.RunID, runID)
	case marker.SourceFingerprint == "":
		return "recorded live happy path marker has no source_fingerprint"
	case marker.SourceFingerprint == source.Digest:
		return ""
	}
	rejection := "recorded live happy path no longer matches the current CLI source"
	if changed := changedSourceFingerprintFiles(marker.SourceFiles, source.Files); len(changed) > 0 {
		rejection += fmt.Sprintf(" (changed: %s)", strings.Join(changed, ", "))
	}
	return rejection + "; rerun dogfood --live --allow-destructive with operator approval"
}

// holdsBackLiveHappyPath reports whether this run must not really execute
// the command, and why. A dry-run still runs: it reaches nothing real and
// proves the command works on this binary.
func (ctx resolveCtx) holdsBackLiveHappyPath(command liveDogfoodCommand) (string, bool) {
	if !ctx.liveHappyReuse.requested || !annotationIsTrueValue(command.Annotations[liveHappyPathAnnotation]) {
		return "", false
	}
	if _, recorded := ctx.reusedLiveHappyRun(command); recorded {
		return reasonLiveHappyReused, true
	}
	if ctx.liveHappyReuse.rejection != "" {
		return ctx.liveHappyReuse.rejection, true
	}
	return reasonLiveHappyNotRecorded, true
}

func (ctx resolveCtx) reusedLiveHappyRun(command liveDogfoodCommand) (LiveHappyPathRun, bool) {
	run, recorded := ctx.liveHappyReuse.runs[strings.Join(command.Path, " ")]
	return run, recorded
}

// recordLiveHappyPathRuns lists the annotated commands this run really
// executed, plus the recorded runs it reused. Any failed row disqualifies a
// command, so a run whose JSON output was invalid cannot vouch for it later.
func recordLiveHappyPathRuns(report *LiveDogfoodReport, commands []liveDogfoodCommand, ctx resolveCtx, source SourceFingerprint) {
	for _, command := range commands {
		if !annotationIsTrueValue(command.Annotations[liveHappyPathAnnotation]) {
			continue
		}
		name := strings.Join(command.Path, " ")
		if run, recorded := ctx.reusedLiveHappyRun(command); recorded {
			run.Reused = true
			report.LiveHappyPathRuns = append(report.LiveHappyPathRuns, run)
			continue
		}
		// Without --allow-destructive, a preview annotation is what ran, and
		// a preview is not the approved real run.
		previewOnly := !ctx.allowDestructive && annotationIsTrueValue(command.Annotations[previewHappyPathAnnotation])
		if !previewOnly && liveDogfoodCommandRanForReal(report.Tests, name) {
			report.LiveHappyPathRuns = append(report.LiveHappyPathRuns, LiveHappyPathRun{
				Command:           name,
				SourceFingerprint: source.Digest,
				RanAt:             report.RanAt,
			})
		}
	}
	slices.SortFunc(report.LiveHappyPathRuns, func(a, b LiveHappyPathRun) int {
		return strings.Compare(a.Command, b.Command)
	})
}

func liveDogfoodCommandRanForReal(tests []LiveDogfoodTestResult, name string) bool {
	ranForReal := false
	for _, result := range tests {
		if result.Command != name {
			continue
		}
		if result.Status == LiveDogfoodStatusFail {
			return false
		}
		if result.Kind == LiveDogfoodTestHappy && result.Status == LiveDogfoodStatusPass && !liveDogfoodResultRequestsDryRun(result) {
			ranForReal = true
		}
	}
	return ranForReal
}

// liveDogfoodReusedHappyCoverage reports whether a feature with no live pass
// in this run is covered by a reused run of its command.
func liveDogfoodReusedHappyCoverage(feature NovelFeature, runs []LiveHappyPathRun) bool {
	for _, run := range runs {
		if run.Reused && matchNovelFeature(feature, map[string]bool{commandPath(run.Command): true}, nil) {
			return true
		}
	}
	return false
}

// liveHappyRejectionReason names why reuse did not cover an annotated
// feature. It returns "" for features with no annotated command.
func liveHappyRejectionReason(feature NovelFeature, proofs liveDogfoodProofContext) string {
	if proofs.liveHappyRejection == "" {
		return ""
	}
	// Nothing was recorded for this feature's command: a generic reason
	// below would only describe the dry-run that ran instead.
	for _, command := range proofs.commands {
		path := strings.Join(command.Path, " ")
		if annotationIsTrueValue(command.Annotations[liveHappyPathAnnotation]) &&
			matchNovelFeature(feature, map[string]bool{commandPath(path): true}, nil) {
			return proofs.liveHappyRejection
		}
	}
	return ""
}

// validatePhase5LiveHappyRuns rejects a marker whose recorded runs are not
// bound to the source the marker itself vouches for.
func validatePhase5LiveHappyRuns(marker Phase5GateMarker) []string {
	var issues []string
	for _, run := range marker.LiveHappyPathRuns {
		if strings.TrimSpace(run.Command) == "" {
			issues = append(issues, "phase5 live_happy_path_runs entry missing command")
			continue
		}
		if run.SourceFingerprint != marker.SourceFingerprint {
			issues = append(issues, fmt.Sprintf("phase5 live happy path run for %q was recorded against different source", run.Command))
		}
	}
	return issues
}
