package pipeline

import (
	"fmt"
	"strings"
	"time"
)

// PlanContext aggregates outputs from completed phases for dynamic plan generation.
type PlanContext struct {
	SeedData  SeedData
	Research  *ResearchResult
	Dogfood   *DogfoodReport
	Scorecard *Scorecard
	Learnings *LearningsDB
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
	b.WriteString("- Generator entrypoint: printing-press generate --spec <url> --output <dir>\n")
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

	// Dynamic: ship/hold decision based on scores
	if ctx.Scorecard != nil {
		b.WriteString("## Ship Decision\n\n")
		if ctx.Scorecard.Steinberger.Percentage >= 65 {
			fmt.Fprintf(&b, "**SHIP** - Quality score %d%% (grade %s) meets threshold.\n\n", ctx.Scorecard.Steinberger.Percentage, ctx.Scorecard.OverallGrade)
		} else {
			fmt.Fprintf(&b, "**HOLD** - Quality score %d%% (grade %s) is below 65%% threshold.\n", ctx.Scorecard.Steinberger.Percentage, ctx.Scorecard.OverallGrade)
			b.WriteString("Fix the gaps identified in the scorecard before shipping.\n\n")
		}
	}

	b.WriteString("## What This Phase Must Produce\n\n")
	fmt.Fprintf(&b, "- Git repository initialized in %s\n", ctx.SeedData.OutputDir)
	b.WriteString("- GoReleaser config validated\n")
	fmt.Fprintf(&b, "- Morning report in %s\n", ctx.SeedData.PipelineDir)

	return b.String(), nil
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
