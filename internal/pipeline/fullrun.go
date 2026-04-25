package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/llmpolish"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"gopkg.in/yaml.v3"
)

// FullRunResult holds everything the press produced for one API.
type FullRunResult struct {
	APIName string
	Level   string // "EASY", "MEDIUM", "HARD"

	// Step 1: Research
	Research      *ResearchResult
	ResearchError string

	// Step 2: Generate
	OutputDir     string
	GatesPassed   int
	GatesFailed   int
	GatesOutput   string
	CommandCount  int
	ResourceCount int

	// Step 3: Coverage
	SpecEndpoints   int
	CoveragePercent int

	// Step 2.5: Polish
	PolishResult *llmpolish.PolishResult

	// Step 4: Dogfood
	Dogfood      *DogfoodReport
	DogfoodError string

	// Step 5.5: Verification
	Verification      *VerificationReport
	VerificationError string
	Remediation       *RemediationResult

	// Step 5.6: Workflow Verification
	WorkflowVerify      *WorkflowVerifyReport
	WorkflowVerifyError string

	// Step 5: Scorecard
	Scorecard      *Scorecard
	ScorecardError string

	// Step 6: Learnings
	FixPlans []string

	Errors   []string
	Duration time.Duration
}

// MakeBestCLI runs the full printing press pipeline for a single API and
// returns a result summarizing every phase.
func MakeBestCLI(apiName, level, specFlag, specURL, outputDir, pressBinary string) *FullRunResult {
	start := time.Now()
	runID, err := newRunID(start)
	if err != nil {
		return &FullRunResult{
			APIName: apiName,
			Level:   level,
			Errors:  []string{fmt.Sprintf("run id: %v", err)},
		}
	}
	workingDir := WorkingCLIDir(apiName, runID)
	state := NewStateWithRun(apiName, workingDir, runID, WorkspaceScope())
	state.SpecURL = specURL
	if specFlag == "--spec" {
		state.SpecPath = specURL
	}

	result := &FullRunResult{
		APIName:   apiName,
		Level:     level,
		OutputDir: state.EffectiveWorkingDir(),
	}

	pipelineDir := state.PipelineDir()
	researchDir := state.ResearchDir()
	proofsDir := state.ProofsDir()
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("pipeline dir: %v", err))
		result.Duration = time.Since(start)
		return result
	}
	if err := os.MkdirAll(researchDir, 0o755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("research dir: %v", err))
		result.Duration = time.Since(start)
		return result
	}
	if err := os.MkdirAll(proofsDir, 0o755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("proofs dir: %v", err))
		result.Duration = time.Since(start)
		return result
	}
	if err := state.Save(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("state save: %v", err))
		result.Duration = time.Since(start)
		return result
	}
	if err := WriteRunManifest(state); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("manifest: %v", err))
	}

	// Step 1: Research
	research, err := RunResearch(apiName, researchDir)
	if err != nil {
		result.ResearchError = err.Error()
		result.Errors = append(result.Errors, fmt.Sprintf("research: %v", err))
	}
	result.Research = research

	// Step 2: Generate
	repoRoot := findRepoRootFrom(pressBinary)
	qualitySpecPath := fullRunQualitySpecPath(specFlag, specURL)
	var genArgs []string
	if specFlag == "--docs" {
		genArgs = []string{"generate", "--docs", specURL, "--name", apiName, "--output", workingDir, "--force"}
	} else {
		genArgs = []string{"generate", "--spec", specURL, "--output", workingDir, "--force", "--lenient"}
	}

	cmd := exec.Command(pressBinary, genArgs...)
	cmd.Dir = repoRoot
	genOut, genErr := cmd.CombinedOutput()
	result.GatesOutput = string(genOut)

	for line := range strings.SplitSeq(result.GatesOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "PASS") {
			result.GatesPassed++
		}
		if strings.Contains(trimmed, "FAIL") {
			result.GatesFailed++
		}
	}

	if genErr != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("generate: %v", genErr))
		result.Duration = time.Since(start)
		return result
	}

	// Step 2.1: Copy spec into output dir for standalone scoring
	if err := copySpecToOutput(specFlag, specURL, workingDir); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("spec copy: %v", err))
	}
	// Step 2.5: LLM Polish
	polishResult, polishErr := llmpolish.Polish(llmpolish.PolishRequest{
		APIName:   apiName,
		OutputDir: workingDir,
	})
	if polishErr != nil {
		result.Errors = append(result.Errors, "polish: "+polishErr.Error())
	} else {
		result.PolishResult = polishResult
	}

	// Step 3: Count commands and resources
	resources := listResources(workingDir)
	result.ResourceCount = len(resources)
	result.CommandCount = len(resources) + 4 // +4 for root, help, version, doctor

	// Step 4: API Coverage estimate
	skippedCount := strings.Count(result.GatesOutput, "skipping") + strings.Count(result.GatesOutput, "Skipping")
	result.SpecEndpoints = result.ResourceCount + skippedCount
	if result.SpecEndpoints > 0 {
		result.CoveragePercent = (result.ResourceCount * 100) / result.SpecEndpoints
	}

	// Step 5: Dogfood
	cliBinaryPath := filepath.Join(workingDir, naming.CLI(apiName))
	buildCmd := exec.Command("go", "build", "-o", cliBinaryPath, "./cmd/...")
	buildCmd.Dir = workingDir
	if buildErr := buildCmd.Run(); buildErr != nil {
		result.DogfoodError = fmt.Sprintf("build failed: %v", buildErr)
		result.Errors = append(result.Errors, fmt.Sprintf("dogfood build: %v", buildErr))
	} else {
		defer func() { _ = os.Remove(cliBinaryPath) }()
		dogfood, dogErr := RunDogfood(workingDir, qualitySpecPath, WithResearchDir(researchDir))
		if dogErr != nil {
			result.DogfoodError = dogErr.Error()
			result.Errors = append(result.Errors, fmt.Sprintf("dogfood: %v", dogErr))
		} else if err := writeDogfoodResults(dogfood, proofsDir); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("dogfood write: %v", err))
		}
		result.Dogfood = dogfood
	}

	// Step 5.5: Proof of Behavior Verification
	verReport, verErr := RunVerification(workingDir, qualitySpecPath)
	if verErr != nil {
		result.VerificationError = verErr.Error()
		result.Errors = append(result.Errors, fmt.Sprintf("verification: %v", verErr))
	} else {
		result.Verification = verReport

		// Auto-remediate if WARN or FAIL
		if verReport.Verdict != "PASS" {
			remResult, remErr := Remediate(workingDir, verReport)
			if remErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("remediation: %v", remErr))
			} else {
				result.Remediation = remResult

				// Re-verify after remediation
				reVerReport, reVerErr := RunVerification(workingDir, qualitySpecPath)
				if reVerErr == nil {
					result.Verification = reVerReport
				}
			}
		}
	}

	// Step 5.6: Workflow Verification
	wfReport, wfErr := RunWorkflowVerification(workingDir)
	if wfErr != nil {
		result.WorkflowVerifyError = wfErr.Error()
		result.Errors = append(result.Errors, fmt.Sprintf("workflow verification: %v", wfErr))
	} else {
		result.WorkflowVerify = wfReport
	}

	// Step 6: Scorecard
	scorecard, scErr := RunScorecard(workingDir, proofsDir, qualitySpecPath, nil)
	if scErr != nil {
		result.ScorecardError = scErr.Error()
		result.Errors = append(result.Errors, fmt.Sprintf("scorecard: %v", scErr))
	}
	result.Scorecard = scorecard

	// Step 7: Fix Plans
	if scorecard != nil {
		plans, planErr := GenerateFixPlans(scorecard, pipelineDir)
		if planErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fix plans: %v", planErr))
		}
		result.FixPlans = plans
	}

	publishedDir, publishErr := PublishWorkingCLI(state, outputDir)
	if publishErr != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("publish: %v", publishErr))
	} else {
		result.OutputDir = publishedDir
	}
	if _, archiveErr := ArchiveRunArtifacts(state); archiveErr != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("archive: %v", archiveErr))
	}

	result.Duration = time.Since(start)
	return result
}

func fullRunQualitySpecPath(specFlag, specURL string) string {
	if specFlag == "--spec" {
		return specURL
	}
	return ""
}

// copySpecToOutput reads the spec from a local path or remote URL, converts
// YAML to JSON if needed, and writes it as <outputDir>/spec.json.
// Only runs when specFlag is "--spec". Errors are non-fatal.
func copySpecToOutput(specFlag, specURL, outputDir string) error {
	if specFlag != "--spec" || specURL == "" {
		return nil
	}
	data, err := readSpecBytes(specURL)
	if err != nil {
		return fmt.Errorf("reading spec %s: %w", specURL, err)
	}
	data, err = ensureJSON(data)
	if err != nil {
		return fmt.Errorf("converting spec to JSON: %w", err)
	}
	dst := filepath.Join(outputDir, "spec.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return nil
}

// readSpecBytes fetches spec content from a URL or reads it from a local file.
func readSpecBytes(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL) //nolint:gosec // spec URLs are operator-provided
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, specURL)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}

// ensureJSON converts YAML content to JSON. If the input is already valid
// JSON, it is returned as-is.
func ensureJSON(data []byte) ([]byte, error) {
	if json.Valid(data) {
		return data, nil
	}
	var obj any
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("not valid JSON or YAML: %w", err)
	}
	return json.Marshal(obj)
}

// listResources returns the resource names found in the generated CLI's
// internal/cli directory, excluding infrastructure files.
func listResources(outputDir string) []string {
	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return nil
	}

	infraFiles := map[string]bool{
		"helpers.go": true,
		"root.go":    true,
		"doctor.go":  true,
		"auth.go":    true,
	}

	var resources []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if infraFiles[name] {
			continue
		}
		// Resource name is the filename without .go
		resources = append(resources, strings.TrimSuffix(name, ".go"))
	}
	return resources
}

// findRepoRootFrom walks up from the binary path (or cwd) to find go.mod.
func findRepoRootFrom(binaryPath string) string {
	dir := filepath.Dir(binaryPath)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Fallback: try cwd
			cwd, _ := os.Getwd()
			return cwd
		}
		dir = parent
	}
}

// PrintComparisonTable produces a formatted comparison table showing all
// FullRunResults side by side with fixed-width columns.
func PrintComparisonTable(results []*FullRunResult) string {
	if len(results) == 0 {
		return "(no results)\n"
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("=== CLI Printing Press - Full Run Comparison ===\n\n")

	// Header
	fmt.Fprintf(&b, "%-25s", "Metric")
	for _, r := range results {
		fmt.Fprintf(&b, "| %-18s", r.APIName+" ("+r.Level+")")
	}
	b.WriteString("|\n")

	// Separator
	b.WriteString(strings.Repeat("-", 25))
	for range results {
		b.WriteString("|" + strings.Repeat("-", 19))
	}
	b.WriteString("|\n")

	// Quality Gates
	writeRow(&b, "Quality Gates", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d/7 PASS", r.GatesPassed)
	})

	// Commands
	writeRow(&b, "Commands", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d", r.CommandCount)
	})

	// Resources
	writeRow(&b, "Resources", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d", r.ResourceCount)
	})

	// API Coverage
	writeRow(&b, "API Coverage", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d%%", r.CoveragePercent)
	})

	// Steinberger dimensions
	steinDimensions := []struct {
		label string
		get   func(*Scorecard) int
	}{
		{"Output Modes", func(s *Scorecard) int { return s.Steinberger.OutputModes }},
		{"Auth", func(s *Scorecard) int { return s.Steinberger.Auth }},
		{"Error Handling", func(s *Scorecard) int { return s.Steinberger.ErrorHandling }},
		{"Terminal UX", func(s *Scorecard) int { return s.Steinberger.TerminalUX }},
		{"README", func(s *Scorecard) int { return s.Steinberger.README }},
		{"Doctor", func(s *Scorecard) int { return s.Steinberger.Doctor }},
		{"Agent Native", func(s *Scorecard) int { return s.Steinberger.AgentNative }},
		{"Local Cache", func(s *Scorecard) int { return s.Steinberger.LocalCache }},
	}

	for _, dim := range steinDimensions {
		writeRow(&b, "  "+dim.label, results, func(r *FullRunResult) string {
			if r.Scorecard == nil {
				return "n/a"
			}
			return fmt.Sprintf("%d/10", dim.get(r.Scorecard))
		})
	}

	// Steinberger total + %
	writeRow(&b, "Steinberger Total", results, func(r *FullRunResult) string {
		if r.Scorecard == nil {
			return "n/a"
		}
		return fmt.Sprintf("%d/80 (%d%%)", r.Scorecard.Steinberger.Total, r.Scorecard.Steinberger.Percentage)
	})

	// Grade
	writeRow(&b, "Grade", results, func(r *FullRunResult) string {
		if r.Scorecard == nil {
			return "n/a"
		}
		return r.Scorecard.OverallGrade
	})

	// Competitors found
	writeRow(&b, "Competitors Found", results, func(r *FullRunResult) string {
		if r.Research == nil {
			return "0"
		}
		return fmt.Sprintf("%d", len(r.Research.Alternatives))
	})

	// We Win?
	writeRow(&b, "We Win?", results, func(r *FullRunResult) string {
		if r.Scorecard == nil || len(r.Scorecard.CompetitorScores) == 0 {
			return "n/a"
		}
		wins := 0
		for _, cs := range r.Scorecard.CompetitorScores {
			if cs.WeWin {
				wins++
			}
		}
		return fmt.Sprintf("%d/%d", wins, len(r.Scorecard.CompetitorScores))
	})

	// Dogfood result
	writeRow(&b, "Dogfood", results, func(r *FullRunResult) string {
		if r.Dogfood == nil {
			return "n/a"
		}
		if r.Dogfood.PathCheck.Tested > 0 {
			return fmt.Sprintf("%s %d%%", r.Dogfood.Verdict, r.Dogfood.PathCheck.Pct)
		}
		return r.Dogfood.Verdict
	})

	// Verification
	writeRow(&b, "Verification", results, func(r *FullRunResult) string {
		if r.Verification == nil {
			return "n/a"
		}
		summary := r.Verification.Verdict
		if r.Verification.HallucinatedPaths > 0 {
			summary += fmt.Sprintf(" %dp", r.Verification.HallucinatedPaths)
		}
		if r.Verification.DeadFlags > 0 {
			summary += fmt.Sprintf(" %df", r.Verification.DeadFlags)
		}
		if r.Verification.GhostTables > 0 {
			summary += fmt.Sprintf(" %dg", r.Verification.GhostTables)
		}
		return summary
	})

	// Workflow Verification
	writeRow(&b, "Workflow Verify", results, func(r *FullRunResult) string {
		if r.WorkflowVerify == nil {
			return "n/a"
		}
		return string(r.WorkflowVerify.Verdict)
	})

	// LLM Polish
	writeRow(&b, "LLM Polish", results, func(r *FullRunResult) string {
		if r.PolishResult == nil {
			return "n/a"
		}
		if r.PolishResult.Skipped {
			return "skipped"
		}
		return fmt.Sprintf("%dh/%de/%v",
			r.PolishResult.HelpTextsImproved,
			r.PolishResult.ExamplesAdded,
			r.PolishResult.READMERewritten)
	})

	// Fix plans generated
	writeRow(&b, "Fix Plans", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d", len(r.FixPlans))
	})

	// Duration
	writeRow(&b, "Duration", results, func(r *FullRunResult) string {
		return r.Duration.Round(time.Second).String()
	})

	// Errors
	writeRow(&b, "Errors", results, func(r *FullRunResult) string {
		return fmt.Sprintf("%d", len(r.Errors))
	})

	b.WriteString("\n")
	return b.String()
}

func writeRow(b *strings.Builder, label string, results []*FullRunResult, fn func(*FullRunResult) string) {
	fmt.Fprintf(b, "%-25s", label)
	for _, r := range results {
		fmt.Fprintf(b, "| %-18s", fn(r))
	}
	b.WriteString("|\n")
}

// GenerateLearningsPlan writes a markdown plan summarizing consistent gaps
// and recommended fixes across all runs.
func GenerateLearningsPlan(results []*FullRunResult, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	var b strings.Builder
	b.WriteString("# Learnings Plan - CLI Printing Press Full Run\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// Summarize runs
	b.WriteString("## Runs\n\n")
	for _, r := range results {
		status := "OK"
		if len(r.Errors) > 0 {
			status = fmt.Sprintf("%d errors", len(r.Errors))
		}
		fmt.Fprintf(&b, "- **%s** (%s) - Gates %d/7, Duration %s, Status: %s\n",
			r.APIName, r.Level, r.GatesPassed, r.Duration.Round(time.Second), status)
	}
	b.WriteString("\n")

	// Find consistent gaps (dimensions scoring <5 across multiple runs)
	type dimTally struct {
		lowCount int
		total    int
		sum      int
	}
	dimNames := []string{"output_modes", "auth", "error_handling", "terminal_ux", "readme", "doctor", "agent_native", "local_cache"}
	dimGetters := []func(*SteinerScore) int{
		func(s *SteinerScore) int { return s.OutputModes },
		func(s *SteinerScore) int { return s.Auth },
		func(s *SteinerScore) int { return s.ErrorHandling },
		func(s *SteinerScore) int { return s.TerminalUX },
		func(s *SteinerScore) int { return s.README },
		func(s *SteinerScore) int { return s.Doctor },
		func(s *SteinerScore) int { return s.AgentNative },
		func(s *SteinerScore) int { return s.LocalCache },
	}

	tallies := make([]dimTally, len(dimNames))
	for _, r := range results {
		if r.Scorecard == nil {
			continue
		}
		for i, getter := range dimGetters {
			score := getter(&r.Scorecard.Steinberger)
			tallies[i].total++
			tallies[i].sum += score
			if score < 5 {
				tallies[i].lowCount++
			}
		}
	}

	b.WriteString("## Consistent Gaps\n\n")
	b.WriteString("Dimensions scoring below 5/10 across multiple runs:\n\n")
	hasGaps := false
	for i, name := range dimNames {
		t := tallies[i]
		if t.total == 0 {
			continue
		}
		avg := t.sum / t.total
		if t.lowCount > 0 {
			hasGaps = true
			fmt.Fprintf(&b, "- **%s** - low in %d/%d runs (avg %d/10)\n", name, t.lowCount, t.total, avg)
		}
	}
	if !hasGaps {
		b.WriteString("No consistent gaps found - all dimensions scored 5+ across runs.\n")
	}
	b.WriteString("\n")

	// Recommended fixes
	b.WriteString("## Recommended Fixes\n\n")
	b.WriteString("Priority order (most impactful first):\n\n")
	priority := 1
	for i, name := range dimNames {
		t := tallies[i]
		if t.total == 0 || t.lowCount == 0 {
			continue
		}
		fmt.Fprintf(&b, "%d. **Improve %s templates** - affects %d/%d APIs tested\n",
			priority, name, t.lowCount, t.total)
		if advice, ok := dimensionAdvice[name]; ok {
			// Include first line of advice
			lines := strings.SplitN(advice, "\n", 2)
			fmt.Fprintf(&b, "   - %s\n", strings.TrimSpace(lines[0]))
		}
		priority++
	}
	if priority == 1 {
		b.WriteString("No template fixes needed - all dimensions healthy.\n")
	}
	b.WriteString("\n")

	// Gate failures
	b.WriteString("## Gate Failures\n\n")
	for _, r := range results {
		if r.GatesFailed > 0 {
			fmt.Fprintf(&b, "- **%s** - %d gates failed\n", r.APIName, r.GatesFailed)
		}
	}
	allPassed := true
	for _, r := range results {
		if r.GatesFailed > 0 {
			allPassed = false
			break
		}
	}
	if allPassed {
		b.WriteString("All gates passed across all runs.\n")
	}
	b.WriteString("\n")

	// Dogfood summary
	b.WriteString("## Dogfood Summary\n\n")
	for _, r := range results {
		if r.Dogfood != nil {
			if r.Dogfood.PathCheck.Tested > 0 {
				fmt.Fprintf(&b, "- **%s** - %s, path validity %d/%d (%d%%)\n",
					r.APIName, r.Dogfood.Verdict, r.Dogfood.PathCheck.Valid, r.Dogfood.PathCheck.Tested, r.Dogfood.PathCheck.Pct)
			} else {
				fmt.Fprintf(&b, "- **%s** - %s\n", r.APIName, r.Dogfood.Verdict)
			}
		} else {
			fmt.Fprintf(&b, "- **%s** - dogfood not run (%s)\n", r.APIName, r.DogfoodError)
		}
	}
	b.WriteString("\n")

	// Verification summary
	b.WriteString("## Verification Summary\n\n")
	for _, r := range results {
		if r.Verification != nil {
			fmt.Fprintf(&b, "- **%s** - %s (paths:%d flags:%d ghost:%d fts:%d)\n",
				r.APIName, r.Verification.Verdict,
				r.Verification.HallucinatedPaths,
				r.Verification.DeadFlags,
				r.Verification.GhostTables,
				r.Verification.OrphanFTS)
		} else {
			fmt.Fprintf(&b, "- **%s** - not run (%s)\n", r.APIName, r.VerificationError)
		}
	}
	b.WriteString("\n")

	// Workflow Verification summary
	b.WriteString("## Workflow Verification Summary\n\n")
	for _, r := range results {
		if r.WorkflowVerify != nil {
			fmt.Fprintf(&b, "- **%s** - %s\n", r.APIName, r.WorkflowVerify.Verdict)
			for _, issue := range r.WorkflowVerify.Issues {
				fmt.Fprintf(&b, "  - %s\n", issue)
			}
		} else {
			fmt.Fprintf(&b, "- **%s** - not run (%s)\n", r.APIName, r.WorkflowVerifyError)
		}
	}
	b.WriteString("\n")

	// Next steps
	b.WriteString("## Next Steps\n\n")
	b.WriteString("1. Fix the highest-priority template gaps listed above\n")
	b.WriteString("2. Re-run this full comparison to verify improvements\n")
	b.WriteString("3. Add more APIs at each difficulty level to broaden coverage\n")

	return os.WriteFile(outputPath, []byte(b.String()), 0o644)
}
