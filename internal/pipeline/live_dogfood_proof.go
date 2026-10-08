package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// verifiedByProofAnnotation names an operator-written proof file, in the
// same proofs directory as the phase5 acceptance marker, that records a
// real run of a novel command whose happy path only works on state a prior
// real write created. It is never a substitute for a runnable happy path:
// it applies only when live dogfood found no non-dry-run happy pass.
const verifiedByProofAnnotation = "pp:verified-by-proof"

// ProofCoveredFeature records a novel feature counted as covered by a proof
// file instead of a live happy-path pass, so the exception stays auditable
// on the report and the phase5 marker.
type ProofCoveredFeature struct {
	Command string `json:"command"`
	Proof   string `json:"proof"`
}

type liveDogfoodProofContext struct {
	commands  []liveDogfoodCommand
	proofsDir string
}

// Proof files must survive the manuscript copy into the published proofs
// directory and stay human-reviewable, so only plain-text extensions count.
var proofFileExtensions = []string{".md", ".txt"}

func liveDogfoodAcceptanceProofsDir(writeAcceptancePath string) string {
	if strings.TrimSpace(writeAcceptancePath) == "" {
		return ""
	}
	return filepath.Dir(writeAcceptancePath)
}

// validateProofFileName accepts a bare filename only: no directory
// components, no traversal, no hidden files, and a plain-text extension.
func validateProofFileName(name string) error {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return errors.New("proof file name is empty")
	case trimmed != name:
		return fmt.Errorf("proof file name %q has surrounding whitespace", name)
	case strings.ContainsAny(name, `/\`) || strings.Contains(name, ".."):
		return fmt.Errorf("proof file name %q must be a bare filename", name)
	case strings.HasPrefix(name, "."):
		return fmt.Errorf("proof file name %q must not be hidden", name)
	case !slices.Contains(proofFileExtensions, strings.ToLower(filepath.Ext(name))):
		return fmt.Errorf("proof file name %q must end in %s", name, strings.Join(proofFileExtensions, " or "))
	}
	return nil
}

// readProofFile returns the proof contents from proofsDir, rejecting
// anything that is not a non-empty regular file.
func readProofFile(proofsDir, name string) ([]byte, error) {
	if err := validateProofFileName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(proofsDir) == "" {
		return nil, errors.New("no proofs directory is known for this run")
	}
	path := filepath.Join(proofsDir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("proof file %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("proof file %s is not a regular file", name)
	}
	if proofFileTooLarge(info.Size()) {
		// The manuscript copy drops files this large, so the proof would not
		// travel with the marker into the published proofs directory.
		return nil, fmt.Errorf("proof file %s is %d bytes; proofs must be under %d bytes to be published", name, info.Size(), publishableManuscriptMaxCaptureBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("proof file %s: %w", name, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("proof file %s is empty", name)
	}
	return data, nil
}

func proofFileTooLarge(size int64) bool {
	return size >= publishableManuscriptMaxCaptureBytes
}

// liveDogfoodProofCoverage reports whether a novel feature without a live
// happy pass is backed by a proof file. All of these must hold for the
// matching command: it carries the annotation, the named proof exists in
// the run's proofs directory, the proof mentions the command path, and this
// run still passed its help check plus a dry-run happy_path or dry_run_json
// check, so the command demonstrably works at all.
func liveDogfoodProofCoverage(feature NovelFeature, tests []LiveDogfoodTestResult, proofs liveDogfoodProofContext) (ProofCoveredFeature, bool) {
	for _, command := range proofs.commands {
		path := strings.Join(command.Path, " ")
		if !matchNovelFeature(feature, map[string]bool{commandPath(path): true}, nil) {
			continue
		}
		proofName, annotated := command.Annotations[verifiedByProofAnnotation]
		if !annotated {
			continue
		}
		reason := liveDogfoodProofRejection(path, proofName, tests, proofs.proofsDir)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "warning: %s %s=%q not accepted: %s; feature stays hollow\n", path, verifiedByProofAnnotation, proofName, reason)
			continue
		}
		return ProofCoveredFeature{Command: feature.Command, Proof: proofName}, true
	}
	return ProofCoveredFeature{}, false
}

func liveDogfoodProofRejection(path, proofName string, tests []LiveDogfoodTestResult, proofsDir string) string {
	data, err := readProofFile(proofsDir, proofName)
	if err != nil {
		return err.Error()
	}
	if !bytes.Contains(bytes.ToLower(data), []byte(strings.ToLower(path))) {
		return fmt.Sprintf("proof file %s does not mention %q", proofName, path)
	}
	helpPassed := false
	dryRunPassed := false
	for _, result := range tests {
		if result.Command != path || result.Status != LiveDogfoodStatusPass {
			continue
		}
		switch result.Kind {
		case LiveDogfoodTestHelp:
			helpPassed = true
		case LiveDogfoodTestDryRunJSON:
			dryRunPassed = true
		case LiveDogfoodTestHappy:
			if slices.Contains(result.Args, "--dry-run") {
				dryRunPassed = true
			}
		}
	}
	if !helpPassed {
		return "help check did not pass in this run"
	}
	if !dryRunPassed {
		return "no dry-run happy_path or dry_run_json check passed in this run"
	}
	return ""
}

// copyProofFiles copies the proof files a marker references from srcDir to
// dstDir so a mirrored marker stays verifiable on its own.
func copyProofFiles(srcDir, dstDir string, covered []ProofCoveredFeature) error {
	var errs []error
	for _, feature := range covered {
		if err := validateProofFileName(feature.Proof); err != nil {
			errs = append(errs, err)
			continue
		}
		src := filepath.Join(srcDir, feature.Proof)
		dst := filepath.Join(dstDir, feature.Proof)
		info, err := os.Stat(src)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// A symlinked proofs dir can make src and dst one file under two
		// paths; copying would truncate the proof before reading it.
		if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(info, dstInfo) {
			continue
		}
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// validatePhase5ProofCoverage checks that every proof a pass marker relies on
// sits beside it, so a marker cannot claim proof coverage the packaged
// proofs directory does not carry.
func validatePhase5ProofCoverage(marker Phase5GateMarker, proofsDir string) []string {
	var issues []string
	hollow := make(map[string]bool, len(marker.HollowFeatures))
	for _, feature := range marker.HollowFeatures {
		hollow[feature] = true
	}
	for _, feature := range marker.ProofCoveredFeatures {
		if strings.TrimSpace(feature.Command) == "" {
			issues = append(issues, "phase5 proof_covered_features entry missing command")
			continue
		}
		if hollow[feature.Command] {
			issues = append(issues, fmt.Sprintf("phase5 feature %q is listed as both hollow and proof-covered", feature.Command))
		}
		if _, err := readProofFile(proofsDir, feature.Proof); err != nil {
			issues = append(issues, fmt.Sprintf("phase5 proof for %q: %v", feature.Command, err))
		}
	}
	return issues
}
