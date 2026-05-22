package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlanContext aggregates outputs from completed phases for dynamic plan generation.
type PlanContext struct {
	SeedData     SeedData
	Research     *ResearchResult
	Dogfood      *DogfoodReport
	Scorecard    *Scorecard
	Learnings    *LearningsDB
	Readiness    *AgentReadinessReport
	ReadinessErr error
}

type AgentReadinessVerdict string

const (
	AgentReadinessPass    AgentReadinessVerdict = "Pass"
	AgentReadinessWarn    AgentReadinessVerdict = "Warn"
	AgentReadinessDegrade AgentReadinessVerdict = "Degrade"
)

type AgentReadinessReport struct {
	Path     string
	Verdict  AgentReadinessVerdict
	Findings []string
}

// GenerateNextPlan writes a dynamic plan for the next phase, informed by
// all available prior phase outputs. Falls back to static seed if no
// dynamic generation is available for that phase.
func GenerateNextPlan(state *PipelineState, nextPhase string) (string, error) {
	pipeDir := state.PipelineDir()
	ctx := PlanContext{
		SeedData: SeedData{
			APIName:     state.APIName,
			OutputDir:   state.EffectiveWorkingDir(),
			SpecURL:     state.SpecURL,
			PipelineDir: pipeDir,
		},
	}

	// Load all available prior phase outputs (silently ignore missing ones).
	// New runstate layout stores research in research/ and audits in proofs/,
	// with pipeline/ retained as a compatibility fallback.
	ctx.Research, _ = loadResearchForPlanState(state)
	ctx.Dogfood, _ = loadDogfoodForPlanState(state)
	ctx.Scorecard, _ = loadScorecardForPlanState(state)
	ctx.Learnings, _ = LoadLearnings()

	switch nextPhase {
	case PhaseScaffold:
		return generateScaffoldPlan(ctx)
	case PhaseEnrich:
		return generateEnrichPlan(ctx)
	case PhaseReview:
		return generateReviewPlan(ctx)
	case PhaseComparative:
		return generateComparativePlan(ctx)
	case PhaseShip:
		ctx.Readiness, ctx.ReadinessErr = loadAgentReadinessForPlanState(state)
		return generateShipPlan(ctx)
	default:
		// Preflight, Research, AgentReadiness use static seeds
		return RenderSeed(nextPhase, ctx.SeedData)
	}
}

func generateScaffoldPlan(ctx PlanContext) (string, error) {
	var b strings.Builder
	writePlanHeader(&b, ctx.SeedData.APIName, "scaffold", "Generate the CLI with intelligence from research")

	b.WriteString("## Phase Goal\n\n")
	fmt.Fprintf(&b, "Generate the %s CLI from the validated OpenAPI spec, incorporating research insights.\n\n", ctx.SeedData.APIName)

	b.WriteString("## Context\n\n")
	writePipelineContext(&b, ctx.SeedData)

	// Dynamic section: research insights
	if ctx.Research != nil {
		b.WriteString("## Research Insights\n\n")
		fmt.Fprintf(&b, "- **Novelty score:** %d/10 (%s)\n", ctx.Research.NoveltyScore, ctx.Research.Recommendation)
		fmt.Fprintf(&b, "- **Alternatives found:** %d\n", len(ctx.Research.Alternatives))

		if ctx.Research.CompetitorInsights != nil {
			ci := ctx.Research.CompetitorInsights
			fmt.Fprintf(&b, "- **Command target:** %d (based on competitor analysis)\n", ci.CommandTarget)
			if len(ci.UnmetFeatures) > 0 {
				b.WriteString("- **Unmet features to include:**\n")
				for _, f := range ci.UnmetFeatures[:min(5, len(ci.UnmetFeatures))] {
					fmt.Fprintf(&b, "  - %s\n", f)
				}
			}
			if len(ci.PainPointsToAvoid) > 0 {
				b.WriteString("- **Pain points to avoid:**\n")
				for _, p := range ci.PainPointsToAvoid[:min(3, len(ci.PainPointsToAvoid))] {
					fmt.Fprintf(&b, "  - %s\n", p)
				}
			}
		}
		b.WriteString("\n")
	}

	// Dynamic section: learnings from past runs
	if ctx.Learnings != nil && len(ctx.Learnings.Learnings) > 0 {
		b.WriteString("## Known Pitfalls (from past runs)\n\n")
		suggestions := SuggestFlags(0, "openapi")
		if len(suggestions) > 0 {
			b.WriteString("Suggested flags based on past issues:\n")
			for _, s := range suggestions {
				fmt.Fprintf(&b, "- `%s`\n", s)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- Generated CLI source tree in %s\n", ctx.SeedData.OutputDir)
	b.WriteString("- All eight generator quality gates passing, including default-mode govulncheck\n")
	fmt.Fprintf(&b, "- Working CLI binary for %s\n\n", ctx.SeedData.APIName)

	b.WriteString("## Codebase Pointers\n\n")
	b.WriteString("- Generator entrypoint: cli-printing-press generate --spec <url> --output <dir>\n")
	b.WriteString("- Generator implementation: internal/generator/\n")
	b.WriteString("- Quality gate logic: internal/generator/validate.go\n")

	return b.String(), nil
}

func generateEnrichPlan(ctx PlanContext) (string, error) {
	var b strings.Builder
	writePlanHeader(&b, ctx.SeedData.APIName, "enrich", "Enrich the CLI using competitor intelligence")

	b.WriteString("## Phase Goal\n\n")
	b.WriteString("Produce an overlay that adds missing endpoints and improves descriptions based on competitor analysis.\n\n")

	writePipelineContext(&b, ctx.SeedData)

	if ctx.Research != nil && ctx.Research.CompetitorInsights != nil {
		ci := ctx.Research.CompetitorInsights
		b.WriteString("## Competitor-Driven Enrichments\n\n")
		if len(ci.UnmetFeatures) > 0 {
			b.WriteString("Features that competitors requested but never got (add these):\n")
			for _, f := range ci.UnmetFeatures {
				fmt.Fprintf(&b, "- [ ] %s\n", f)
			}
			b.WriteString("\n")
		}
		if ci.CommandTarget > 0 {
			fmt.Fprintf(&b, "**Target:** Generate at least %d commands (competitors max: %d)\n\n", ci.CommandTarget, int(float64(ci.CommandTarget)/1.2))
		}
	}

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- overlay.yaml in %s\n", ctx.SeedData.PipelineDir)
	b.WriteString("- At least one verified enrichment\n")
	b.WriteString("- Overlay valid for downstream merge and regeneration\n\n")

	b.WriteString("## Codebase Pointers\n\n")
	b.WriteString("- Overlay model: internal/pipeline/overlay.go\n")
	b.WriteString("- Overlay merge: internal/pipeline/merge.go\n")

	return b.String(), nil
}

func generateReviewPlan(ctx PlanContext) (string, error) {
	var b strings.Builder
	writePlanHeader(&b, ctx.SeedData.APIName, "review", "Review with automated scoring")

	b.WriteString("## Phase Goal\n\n")
	b.WriteString("Score the generated CLI against the Steinberger quality bar and competitor CLIs.\n\n")

	writePipelineContext(&b, ctx.SeedData)

	b.WriteString("## Steps\n\n")
	b.WriteString("1. Run dogfood on the generated CLI with the same spec used for generation\n")
	b.WriteString("2. Run verify with `--fix` to catch runtime issues and cheap auto-remediations\n")
	b.WriteString("3. Run the Steinberger scorecard\n")
	b.WriteString("4. Summarize the combined shipcheck result in review.md\n")
	b.WriteString("5. If any major dimension still fails, generate fix plans\n\n")

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- dogfood-results.json in %s\n", ctx.SeedData.PipelineDir)
	fmt.Fprintf(&b, "- scorecard.md in %s\n", ctx.SeedData.PipelineDir)
	fmt.Fprintf(&b, "- review.md in %s\n", ctx.SeedData.PipelineDir)
	b.WriteString("- Fix plans for any low-scoring dimensions\n")

	return b.String(), nil
}

func generateComparativePlan(ctx PlanContext) (string, error) {
	var b strings.Builder
	writePlanHeader(&b, ctx.SeedData.APIName, "comparative", "Compare against competitors")

	b.WriteString("## Phase Goal\n\n")
	b.WriteString("Score our CLI vs discovered alternatives on 6 dimensions.\n\n")

	writePipelineContext(&b, ctx.SeedData)

	if ctx.Scorecard != nil {
		b.WriteString("## Current Steinberger Score\n\n")
		fmt.Fprintf(&b, "- Overall: %d%% (%s)\n", ctx.Scorecard.Steinberger.Percentage, ctx.Scorecard.OverallGrade)
		if len(ctx.Scorecard.GapReport) > 0 {
			b.WriteString("- Gaps:\n")
			for _, g := range ctx.Scorecard.GapReport {
				fmt.Fprintf(&b, "  - %s\n", g)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- comparative-analysis.md in %s\n\n", ctx.SeedData.PipelineDir)

	return b.String(), nil
}

func generateShipPlan(ctx PlanContext) (string, error) {
	var b strings.Builder
	writePlanHeader(&b, ctx.SeedData.APIName, "ship", "Package and prepare for release")

	b.WriteString("## Phase Goal\n\n")
	b.WriteString("Package the CLI for distribution.\n\n")

	writePipelineContext(&b, ctx.SeedData)

	writeShipDecision(&b, ctx)

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- Git repository initialized in %s\n", ctx.SeedData.OutputDir)
	b.WriteString("- GoReleaser config validated\n")
	fmt.Fprintf(&b, "- Morning report in %s\n", ctx.SeedData.PipelineDir)

	return b.String(), nil
}

func writeShipDecision(b *strings.Builder, ctx PlanContext) {
	b.WriteString("## Ship Decision\n\n")

	ship := true
	switch {
	case ctx.Scorecard == nil:
		b.WriteString("- Quality: HOLD - missing scorecard.json; run the review phase before shipping.\n")
		ship = false
	case ctx.Scorecard.Steinberger.Percentage >= 65:
		fmt.Fprintf(b, "- Quality: PASS - score %d%% (grade %s) meets threshold.\n", ctx.Scorecard.Steinberger.Percentage, ctx.Scorecard.OverallGrade)
	default:
		fmt.Fprintf(b, "- Quality: HOLD - score %d%% (grade %s) is below 65%% threshold.\n", ctx.Scorecard.Steinberger.Percentage, ctx.Scorecard.OverallGrade)
		ship = false
	}

	switch {
	case ctx.Readiness == nil:
		if ctx.ReadinessErr != nil && !os.IsNotExist(ctx.ReadinessErr) {
			fmt.Fprintf(b, "- Agent readiness: HOLD - %s.\n", ctx.ReadinessErr)
		} else {
			b.WriteString("- Agent readiness: HOLD - missing pipeline/agent-readiness.md; run the agent-readiness phase before shipping.\n")
		}
		ship = false
	case ctx.Readiness.Verdict == AgentReadinessPass:
		fmt.Fprintf(b, "- Agent readiness: PASS - %s reports Pass.\n", ctx.Readiness.Path)
	default:
		fmt.Fprintf(b, "- Agent readiness: HOLD - %s reports %s.\n", ctx.Readiness.Path, ctx.Readiness.Verdict)
		for _, finding := range ctx.Readiness.Findings {
			fmt.Fprintf(b, "  - %s\n", finding)
		}
		b.WriteString("  - To ship anyway, record an explicit maintainer override and cite the readiness findings in the handoff report.\n")
		ship = false
	}

	if ship {
		b.WriteString("\n**SHIP** - Quality and agent-readiness gates both pass.\n\n")
		return
	}
	b.WriteString("\n**HOLD** - Do not package or promote the CLI until the failed gate is resolved or explicitly overridden.\n\n")
}

// Helper functions

func writePlanHeader(b *strings.Builder, apiName, phase, title string) {
	b.WriteString("---\n")
	fmt.Fprintf(b, "title: \"%s CLI Pipeline - %s\"\n", apiName, title)
	b.WriteString("type: feat\n")
	b.WriteString("status: seed\n")
	fmt.Fprintf(b, "pipeline_phase: %s\n", phase)
	fmt.Fprintf(b, "pipeline_api: %s\n", apiName)
	fmt.Fprintf(b, "date: %s\n", time.Now().Format("2006-01-02"))
	b.WriteString("---\n\n")
}

func writePipelineContext(b *strings.Builder, sd SeedData) {
	b.WriteString("## Context\n\n")
	fmt.Fprintf(b, "- Pipeline directory: %s\n", sd.PipelineDir)
	fmt.Fprintf(b, "- Output directory: %s\n", sd.OutputDir)
	fmt.Fprintf(b, "- Spec URL: %s\n\n", sd.SpecURL)
}

func loadResearchForPlanState(state *PipelineState) (*ResearchResult, error) {
	for _, dir := range []string{state.ResearchDir(), state.PipelineDir()} {
		research, err := LoadResearch(dir)
		if err == nil {
			return research, nil
		}
	}
	return nil, fmt.Errorf("research not found")
}

func loadDogfoodForPlanState(state *PipelineState) (*DogfoodReport, error) {
	for _, dir := range []string{state.ProofsDir(), state.PipelineDir(), state.EffectiveWorkingDir()} {
		report, err := LoadDogfoodResults(dir)
		if err == nil {
			return report, nil
		}
	}
	return nil, fmt.Errorf("dogfood results not found")
}

func loadScorecardForPlanState(state *PipelineState) (*Scorecard, error) {
	for _, dir := range []string{state.ProofsDir(), state.PipelineDir()} {
		scorecard, err := LoadScorecard(dir)
		if err == nil {
			return scorecard, nil
		}
	}
	return nil, fmt.Errorf("scorecard not found")
}

func loadAgentReadinessForPlanState(state *PipelineState) (*AgentReadinessReport, error) {
	return LoadAgentReadinessReport(state.PipelineDir())
}

func LoadAgentReadinessReport(dir string) (*AgentReadinessReport, error) {
	path := filepath.Join(dir, "agent-readiness.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	report := &AgentReadinessReport{Path: path}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "|"))
		if trimmed == "" {
			continue
		}
		if report.Verdict == "" {
			report.Verdict = parseAgentReadinessVerdict(trimmed)
		}
		if finding, ok := normalizeAgentReadinessFinding(line); ok {
			report.Findings = append(report.Findings, finding)
		}
	}
	if report.Verdict == "" {
		return nil, fmt.Errorf("agent-readiness verdict not found in %s", path)
	}
	return report, nil
}

func parseAgentReadinessVerdict(line string) AgentReadinessVerdict {
	clean := strings.Trim(strings.TrimSpace(line), "*` ")
	clean = strings.ReplaceAll(clean, "**", "")
	lower := strings.ToLower(clean)
	for _, prefix := range []string{"phase verdict:", "verdict:", "phase verdict -", "verdict -"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(clean[len(prefix):])
		if rest == "" {
			return ""
		}
		token := strings.Trim(strings.Fields(rest)[0], "*`.,;:()[]{} ")
		switch strings.ToLower(token) {
		case "pass":
			return AgentReadinessPass
		case "warn":
			return AgentReadinessWarn
		case "degrade":
			return AgentReadinessDegrade
		default:
			return ""
		}
	}
	return ""
}

func normalizeAgentReadinessFinding(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "- ") || strings.HasPrefix(lower, "* "):
		finding := strings.TrimSpace(trimmed[2:])
		return finding, finding != ""
	case strings.HasPrefix(trimmed, "|"):
		return normalizeAgentReadinessTableFinding(readinessTableCells(trimmed))
	default:
		return "", false
	}
}

func readinessTableCells(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		if cell != "" {
			cells = append(cells, cell)
		}
	}
	return cells
}

func normalizeAgentReadinessTableFinding(cells []string) (string, bool) {
	if len(cells) < 2 || isAgentReadinessTableSeparator(cells) || isAgentReadinessTableHeader(cells) {
		return "", false
	}
	for i, cell := range cells {
		lower := strings.ToLower(cell)
		for _, severity := range []string{"blocker", "friction"} {
			if strings.HasPrefix(lower, severity+":") {
				return cell, true
			}
			if lower == severity {
				rest := nonHeaderTableCells(cells[i+1:])
				if len(rest) == 0 {
					return "", false
				}
				return capitalizeASCII(severity) + ": " + strings.Join(rest, " - "), true
			}
		}
	}
	return "", false
}

func isAgentReadinessTableSeparator(cells []string) bool {
	for _, cell := range cells {
		if strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}

func isAgentReadinessTableHeader(cells []string) bool {
	for _, cell := range cells {
		if !isAgentReadinessHeaderCell(cell) {
			return false
		}
	}
	return true
}

func isAgentReadinessHeaderCell(cell string) bool {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "severity", "finding", "description", "tool", "evidence", "impact", "command", "notes", "blocker", "friction":
		return true
	default:
		return false
	}
}

func nonHeaderTableCells(cells []string) []string {
	var out []string
	for _, cell := range cells {
		if !isAgentReadinessHeaderCell(cell) {
			out = append(out, cell)
		}
	}
	return out
}

func capitalizeASCII(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
