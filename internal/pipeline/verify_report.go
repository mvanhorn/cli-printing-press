package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerificationReport holds the results of a Proof of Behavior verification run.
type VerificationReport struct {
	Dir      string                `json:"dir"`
	SpecPath string                `json:"spec_path,omitempty"`
	Paths    []PathProofResult     `json:"paths"`
	Flags    []FlagProofResult     `json:"flags"`
	Pipeline []PipelineProofResult `json:"pipeline"`
	Auth     AuthProofResult       `json:"auth"`

	HallucinatedPaths int  `json:"hallucinated_paths"`
	DeadFlags         int  `json:"dead_flags"`
	DeadFunctions     int  `json:"dead_functions"`
	GhostTables       int  `json:"ghost_tables"`
	OrphanFTS         int  `json:"orphan_fts"`
	AuthMismatch      bool `json:"auth_mismatch"`

	Verdict string   `json:"verdict"`
	Issues  []string `json:"issues,omitempty"`
}

// RunVerification runs the full Proof of Behavior verification suite against a generated CLI.
func RunVerification(dir, specPath string) (*VerificationReport, error) {
	v, err := NewVerifier(dir, specPath)
	if err != nil {
		return nil, fmt.Errorf("creating verifier: %w", err)
	}

	if err := v.CompileGate(); err != nil {
		report := &VerificationReport{
			Dir:      dir,
			SpecPath: specPath,
			Verdict:  "FAIL",
			Issues:   []string{fmt.Sprintf("compile gate failed: %s", err)},
		}
		if writeErr := writeReport(dir, report); writeErr != nil {
			return report, fmt.Errorf("writing report after compile failure: %w", writeErr)
		}
		return report, nil
	}

	paths := v.PathProof()
	flags := v.FlagProof()
	pipeline := v.PipelineProof()
	auth := v.AuthProof()

	report := &VerificationReport{
		Dir:      dir,
		SpecPath: specPath,
		Paths:    paths,
		Flags:    flags,
		Pipeline: pipeline,
		Auth:     auth,
	}

	// Compute summary counts.
	for _, p := range paths {
		if !p.Valid {
			report.HallucinatedPaths++
		}
	}
	for _, f := range flags {
		if f.References == 0 {
			report.DeadFlags++
		}
	}
	for _, p := range pipeline {
		if !p.HasWrite {
			report.GhostTables++
		}
		if p.HasFTS && !p.HasSearch {
			report.OrphanFTS++
		}
	}
	report.AuthMismatch = auth.Mismatch

	report.Issues = collectVerificationIssues(report)
	report.Verdict = deriveVerificationVerdict(report)

	if err := writeReport(dir, report); err != nil {
		return report, fmt.Errorf("writing verification report: %w", err)
	}

	return report, nil
}

// deriveVerificationVerdict determines PASS, WARN, or FAIL from the report.
func deriveVerificationVerdict(report *VerificationReport) string {
	// FAIL conditions.
	if report.HallucinatedPaths > 0 {
		return "FAIL"
	}
	if report.AuthMismatch {
		return "FAIL"
	}
	for _, p := range report.Pipeline {
		if !p.HasWrite && p.Columns >= 5 {
			return "FAIL"
		}
	}

	// WARN conditions.
	if report.DeadFlags > 0 {
		return "WARN"
	}
	if report.OrphanFTS > 0 {
		return "WARN"
	}
	for _, p := range report.Pipeline {
		if !p.HasWrite && p.Columns < 5 {
			return "WARN"
		}
	}

	return "PASS"
}

// collectVerificationIssues generates human-readable issue strings for each problem found.
func collectVerificationIssues(report *VerificationReport) []string {
	var issues []string

	for _, p := range report.Paths {
		if !p.Valid {
			issues = append(issues, fmt.Sprintf("hallucinated path: %s (not in spec)", p.Path))
		}
	}
	for _, f := range report.Flags {
		if f.References == 0 {
			issues = append(issues, fmt.Sprintf("dead flag: %s (0 references)", f.Flag))
		}
	}
	for _, p := range report.Pipeline {
		if !p.HasWrite {
			issues = append(issues, fmt.Sprintf("ghost table: %s (no WRITE path)", p.Table))
		}
		if p.HasFTS && !p.HasSearch {
			issues = append(issues, fmt.Sprintf("orphan FTS: %s_fts (no search command queries it)", p.Table))
		}
	}
	if report.Auth.Mismatch {
		issues = append(issues, fmt.Sprintf("auth mismatch: spec expects %s, generated uses %s", report.Auth.SpecScheme, report.Auth.GeneratedScheme))
	}

	return issues
}

// Markdown generates a human-readable markdown report.
func (r *VerificationReport) Markdown() string {
	var b strings.Builder

	b.WriteString("# Proof of Behavior Report\n\n")
	b.WriteString(fmt.Sprintf("**Verdict: %s**\n\n", r.Verdict))

	// Path Proof section.
	validPaths := 0
	for _, p := range r.Paths {
		if p.Valid {
			validPaths++
		}
	}
	b.WriteString("## Path Proof\n")
	b.WriteString(fmt.Sprintf("Tested: %d | Valid: %d | Hallucinated: %d\n\n", len(r.Paths), validPaths, r.HallucinatedPaths))

	if r.HallucinatedPaths > 0 {
		b.WriteString("| Path | Status |\n")
		b.WriteString("|------|--------|\n")
		for _, p := range r.Paths {
			if !p.Valid {
				b.WriteString(fmt.Sprintf("| `%s` | INVALID |\n", p.Path))
			}
		}
		b.WriteString("\n")
	}

	// Flag Proof section.
	b.WriteString("## Flag Proof\n")
	b.WriteString(fmt.Sprintf("Total: %d | Dead: %d\n\n", len(r.Flags), r.DeadFlags))

	if r.DeadFlags > 0 {
		for _, f := range r.Flags {
			if f.References == 0 {
				b.WriteString(fmt.Sprintf("- `%s` (0 references)\n", f.Flag))
			}
		}
		b.WriteString("\n")
	}

	// Pipeline Proof section.
	b.WriteString("## Pipeline Proof\n")
	b.WriteString(fmt.Sprintf("Tables: %d | Ghost: %d | Orphan FTS: %d\n\n", len(r.Pipeline), r.GhostTables, r.OrphanFTS))

	if len(r.Pipeline) > 0 {
		b.WriteString("| Table | WRITE | READ | SEARCH |\n")
		b.WriteString("|-------|-------|------|--------|\n")
		for _, p := range r.Pipeline {
			w := boolMark(p.HasWrite)
			rd := boolMark(p.HasRead)
			s := boolMark(p.HasSearch)
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", p.Table, w, rd, s))
		}
		b.WriteString("\n")
	}

	// Auth Proof section.
	b.WriteString("## Auth Proof\n")
	b.WriteString(fmt.Sprintf("Spec: %s | Generated: %s | Match: %t\n\n", r.Auth.SpecScheme, r.Auth.GeneratedScheme, !r.Auth.Mismatch))

	// Issues section.
	if len(r.Issues) > 0 {
		b.WriteString("## Issues\n")
		for _, issue := range r.Issues {
			b.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// LoadVerificationReport loads a verification report from verification-report.json in dir.
func LoadVerificationReport(dir string) (*VerificationReport, error) {
	path := filepath.Join(dir, "verification-report.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading verification report: %w", err)
	}

	var report VerificationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing verification report: %w", err)
	}

	return &report, nil
}

func writeReport(dir string, report *VerificationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	path := filepath.Join(dir, "verification-report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

func boolMark(v bool) string {
	if v {
		return "Y"
	}
	return "-"
}
