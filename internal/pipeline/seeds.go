package pipeline

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// SeedData holds the context for rendering plan seeds.
type SeedData struct {
	APIName     string
	OutputDir   string
	SpecURL     string
	SpecSource  string
	PipelineDir string
}

var seedTemplates = map[string]string{
	PhaseResearch: `---
title: "{{.APIName}} CLI Pipeline - Research: Discover Alternatives"
type: feat
status: seed
pipeline_phase: research
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Discover existing CLI tools for the {{.APIName}} API and assess whether generating a new one adds value.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- research.json in {{.PipelineDir}} with:
  - List of discovered alternative CLIs (name, URL, language, stars)
  - Novelty score (1-10)
  - Recommendation: proceed, proceed-with-gaps, or skip
  - Gap analysis: what alternatives miss
  - Pattern analysis: what alternatives do well

## Steps

1. Check catalog/{{.APIName}}.yaml for known_alternatives field
2. Search GitHub for "{{.APIName}} cli" repos sorted by stars
3. Deduplicate and score alternatives
4. If novelty score <= 3, flag: "Official CLI exists - consider whether this CLI adds value"
5. Write research.json

## Prior Phase Outputs

- Validated spec URL from preflight

## Codebase Pointers

- Research logic: internal/pipeline/research.go
- Catalog entries: catalog/
- Known specs registry: internal/pipeline/discover.go
`,
	PhaseComparative: `---
title: "{{.APIName}} CLI Pipeline - Comparative Analysis"
type: feat
status: seed
pipeline_phase: comparative
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Score the generated {{.APIName}} CLI against discovered alternatives on 6 dimensions.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- comparative-analysis.md in {{.PipelineDir}} with:
  - Score table (our CLI vs each alternative, 100 points max)
  - Gap summary: what we're missing
  - Advantage summary: what we have that others don't
  - Ship recommendation: ship, ship-with-gaps, or hold

## Scoring Dimensions (100 points max)

| Dimension | Points | How Measured |
|-----------|--------|-------------|
| Breadth | 20 | Command count ratio vs best alternative |
| Install Friction | 20 | Go binary = 20, clone+build = 15, runtime = 10 |
| Auth UX | 15 | env var + config = 15, env only = 10, manual = 5 |
| Output Formats | 15 | 5 per format (JSON, table, plain) |
| Agent Friendliness | 15 | --json (5) + --dry-run (5) + non-interactive (5) |
| Freshness | 15 | <30d = 15, <90d = 10, <1yr = 5, >1yr = 0 |

## Prior Phase Outputs

- research.json from research phase
- dogfood-results.json from review phase
- Working CLI binary in {{.OutputDir}}

## Codebase Pointers

- Comparative logic: internal/pipeline/comparative.go
- Research results: {{.PipelineDir}}/research.json
- Dogfood results: {{.PipelineDir}}/dogfood-results.json
`,
	PhasePreflight: `---
title: "{{.APIName}} CLI Pipeline - Phase 0: Preflight"
type: feat
status: seed
pipeline_phase: preflight
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Verify the local environment and source inputs needed to run the {{.APIName}} CLI pipeline.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- Verified Go environment for the pipeline run
- Verified printing-press binary for local generation work
- Downloaded and validated OpenAPI spec for {{.APIName}}
- conventions.json in {{.PipelineDir}}

## Prior Phase Outputs

None.

## Codebase Pointers

- Build entrypoint: go build ./cmd/printing-press
- OpenAPI parsing: internal/openapi/parser.go
- Pipeline discovery flow: internal/pipeline/discover.go
`,
	PhaseScaffold: `---
title: "{{.APIName}} CLI Pipeline - Phase 1: Scaffold"
type: feat
status: seed
pipeline_phase: scaffold
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Generate the first working {{.APIName}} CLI from the validated OpenAPI spec.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- Generated CLI source tree in {{.OutputDir}}
- All eight generator quality gates passing, including default-mode govulncheck
- Working CLI binary for {{.APIName}}

## Prior Phase Outputs

- conventions.json from preflight in {{.PipelineDir}}
- Validated spec URL and downloaded spec source for generation

## Codebase Pointers

- Generator entrypoint: printing-press generate --spec <url> --output <dir>
- Generator implementation: internal/generator/
- Quality gate logic in the generator flow under internal/generator/
`,
	PhaseEnrich: `---
title: "{{.APIName}} CLI Pipeline - Phase 2: Enrich"
type: feat
status: seed
pipeline_phase: enrich
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Produce a focused overlay that captures useful spec enrichments missing from the original generation pass.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- overlay.yaml in {{.PipelineDir}}
- At least one verified enrichment for the source spec
- Overlay content that is valid for downstream merge and regeneration

## Prior Phase Outputs

- conventions.json from preflight in {{.PipelineDir}}
- Scaffold-generated CLI in {{.OutputDir}}

## Codebase Pointers

- Overlay model and helpers: internal/pipeline/overlay.go
- Overlay merge preparation: internal/pipeline/merge.go
- Source spec artifact downloaded during preflight
`,
	PhaseRegenerate: `---
title: "{{.APIName}} CLI Pipeline - Phase 3: Regenerate"
type: feat
status: seed
pipeline_phase: regenerate
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Merge the enrichments into the source spec and regenerate the CLI without losing quality.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- Re-generated CLI in {{.OutputDir}} using the merged overlay
- Merged spec artifact suitable for regeneration
- All eight quality gates still passing after regeneration, including default-mode govulncheck

## Prior Phase Outputs

- overlay.yaml from enrich in {{.PipelineDir}}
- Original scaffolded CLI in {{.OutputDir}}

## Codebase Pointers

- Overlay merge implementation: internal/pipeline/merge.go
- MergeOverlay function in internal/pipeline/merge.go
- Generator entrypoint: printing-press generate
`,
	PhaseReview: `---
title: "{{.APIName}} CLI Pipeline - Phase 4: Review"
type: feat
status: seed
pipeline_phase: review
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Evaluate the generated CLI with one shipcheck block: dogfood, runtime verification, workflow verification, skill verification, narrative validation, and scorecard evidence.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}
- Sandbox note: petstore is sandbox-safe for Tier 3 dogfooding

## What This Phase Must Produce

- dogfood-results.json in {{.PipelineDir}}
- verification-report.json in {{.OutputDir}}
- scorecard.md in {{.PipelineDir}}
- review.md in {{.PipelineDir}} summarizing the combined shipcheck result

## Prior Phase Outputs

- Working CLI binary from regenerate, or scaffold if regenerate was skipped

## Codebase Pointers

- printing-press dogfood --dir {{.OutputDir}} --spec <spec>
- printing-press verify --dir {{.OutputDir}} --spec <spec> --fix
- printing-press workflow-verify --dir {{.OutputDir}}
- printing-press verify-skill --dir {{.OutputDir}}
- printing-press validate-narrative --strict --full-examples --research {{.PipelineDir}}/research.json --binary {{.OutputDir}}/<cli-binary>
- printing-press scorecard --dir {{.OutputDir}} --spec <spec>
- Generated CLI binary and help surfaces in {{.OutputDir}}
`,
	PhaseAgentReadiness: `---
title: "{{.APIName}} CLI Pipeline - Agent Readiness Review"
type: feat
status: seed
pipeline_phase: agent-readiness
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Run the compound-engineering:cli-agent-readiness-reviewer agent on the generated {{.APIName}} CLI
and implement its fixes in a severity-gated loop (max 2 passes) until no Blockers or Frictions remain.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- Agent readiness reviewer scorecard (7 principles x severity)
- Fix implementation log (which fixes were applied, which were skipped/reverted)
- Phase verdict: Pass (zero Blockers and Frictions), Warn (Frictions remain), or Degrade (Blockers remain)

## Prior Phase Outputs

- Runtime verification results from Phase 4.8 (pass rate, data pipeline status)
- Working CLI binary in {{.OutputDir}}

## Codebase Pointers

- Reviewer agent: compound-engineering:cli-agent-readiness-reviewer (external plugin)
- Plugin dependency declared in .claude/settings.json
- Phase 4.8 analog: SKILL.md Phase 4.8 (Runtime Verification)
- If the run started in codex mode, preserve that mode here: reviewer runs in Claude, but each accepted fix patch is delegated to Codex and then verified in Claude
`,
	PhaseShip: `---
title: "{{.APIName}} CLI Pipeline - Phase 5: Ship"
type: feat
status: seed
pipeline_phase: ship
pipeline_api: {{.APIName}}
date: {{now}}
---

# Phase Goal

Package the generated CLI output and produce the final handoff report for humans.

## Context

- Pipeline directory: {{.PipelineDir}}
- Output directory: {{.OutputDir}}
- Spec URL: {{.SpecURL}}
- Spec source: {{.SpecSource}}

## What This Phase Must Produce

- Git repository initialized in {{.OutputDir}}
- Morning report written in {{.PipelineDir}}

## Prior Phase Outputs

- Review score and review.md from the review phase
- Working CLI binary ready for packaging and handoff

## Codebase Pointers

- Output CLI tree in {{.OutputDir}}
- Review artifacts in {{.PipelineDir}}
- Morning report format from SKILL.md Workflow 4 Step 6
`,
}

// RenderSeed renders a plan seed template for the given phase.
func RenderSeed(phase string, data SeedData) (string, error) {
	tmplStr, ok := seedTemplates[phase]
	if !ok {
		return "", fmt.Errorf("no seed template for phase %q", phase)
	}

	tmpl, err := template.New(phase).Funcs(template.FuncMap{
		"now": func() string {
			return time.Now().Format("2006-01-02")
		},
	}).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing seed template for %s: %w", phase, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering seed template for %s: %w", phase, err)
	}

	return buf.String(), nil
}
