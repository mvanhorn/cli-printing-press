# The Printing Press Pipeline

This document is the portable contract for the 9-phase generation pipeline the Printing Press runs when producing a CLI for an API. It describes what each phase consumes, what it produces, what gates it has to pass, and what artifacts it leaves behind. Everything here is implementation-agnostic: anyone porting this flow to another tool, another language, or another agent host should be able to follow this document without reading the Go source.

## Two views of the same work

The Printing Press has a fast path and a managed path.

The fast path is the `/printing-press` skill. It runs the flow end to end in one session, produces a CLI plus an MCP server, and reports back. The high-level step list lives in `README.md` under "How It Works."

The managed path is the 9-phase pipeline. It breaks the same work into phases the user can stop at, resume, re-run, and inspect. Each phase has its own plan file, its own artifacts directory, and its own gate. This is what `printing-press pipeline` creates. It is also how this contract should be read: the fast path compresses these phases, it does not replace them.

Both paths converge on the same quality bar. A CLI produced by the fast path should score the same as one produced by the managed path.

## Phase order

    preflight  ->  research  ->  scaffold  ->  enrich  ->  regenerate  ->  review  ->  agent-readiness  ->  comparative  ->  ship

Phase names and ordering are defined in `internal/pipeline/state.go` (`PhaseOrder`). Phase numbering for plan-file prefixes lives in the same file (`phaseNumber`) and uses gaps (0, 10, 20 ...) so future phases can slot in without renaming existing files. The per-phase seed plans that describe each phase to the operator live in `internal/pipeline/seeds.go`.

## Shared run directory layout

Every managed run gets three sibling directories under the run root:

- `pipeline/`  per-phase plan files, `state.json`, and phase-produced JSON artifacts
- `research/`  research-phase artifacts and discovery snapshots
- `proofs/`    review-phase artifacts: dogfood, verification, scorecard

The working CLI tree lives separately under the API's output directory until `ship` promotes it.

## Phase status model

Every phase has two orthogonal status fields:

| Field | Values |
|-------|--------|
| `Status`     | pending, planned, executing, completed, failed |
| `PlanStatus` | seed, expanded, completed |

`Status` tracks the execution state of the phase. `PlanStatus` tracks the state of the phase's plan file: a freshly-initialized phase has a `seed` plan; once expanded by the planner it becomes `expanded`; once the phase finishes it becomes `completed`. Both are defined in `internal/pipeline/state.go`.

## Phases

Each phase below lists four fields: what it consumes, what it produces, what gates it must pass, and which artifacts it leaves on disk.

### 1. preflight

Purpose: confirm the local environment and the source inputs are fit for a pipeline run.

Inputs:
- Operator intent (API name, spec source)
- Local Go toolchain
- Printing Press binary

Outputs:
- Verified Go environment
- Verified printing-press binary
- Downloaded and validated OpenAPI spec for the target API
- `conventions.json` in the pipeline directory

Gates:
- Spec parses cleanly as OpenAPI 3.0+ (or the configured alternate format)
- printing-press binary version is recent enough for the pipeline features in use

Artifacts:
- `pipeline/conventions.json`
- Downloaded spec under the research directory

### 2. research

Purpose: discover and score the existing CLI landscape for the target API before committing to generation.

Inputs:
- Validated spec URL from preflight
- `catalog/<api>.yaml` if the API is catalog-known (for `known_alternatives`)

Outputs:
- `research.json` in the pipeline directory with:
  - Discovered alternative CLIs (name, URL, language, stars)
  - Novelty score (1-10)
  - Recommendation: `proceed`, `proceed-with-gaps`, or `skip`
  - Gap analysis: what alternatives miss
  - Pattern analysis: what alternatives do well

Gates:
- If the novelty score is 3 or lower, the phase flags "an official or mature CLI already exists; consider whether this CLI adds value." The operator can still proceed.

Artifacts:
- `pipeline/research.json`
- Any competitor-identification notes under the research directory

### 3. scaffold

Purpose: generate the first working CLI from the validated spec.

Inputs:
- `conventions.json` from preflight
- Validated spec URL and downloaded spec source

Outputs:
- Generated CLI source tree under the API's output directory
- Working CLI binary for the target API

Gates:
- All seven generator quality gates pass: `go mod tidy`, `go vet`, `go build`, binary build, `--help`, version, `doctor`

Artifacts:
- Full CLI source tree in the output directory
- Compiled binary

### 4. enrich

Purpose: capture the spec enrichments missed by the first generation pass, without editing the source spec directly.

Inputs:
- `conventions.json` from preflight
- Scaffold-generated CLI in the output directory

Outputs:
- `overlay.yaml` in the pipeline directory
- At least one verified enrichment over the source spec
- Overlay content valid for downstream merge

Gates:
- Overlay merges cleanly against the source spec (checked in the regenerate phase before merge is committed)

Artifacts:
- `pipeline/overlay.yaml`

### 5. regenerate

Purpose: merge the enrichments into the source spec and regenerate the CLI without losing quality.

Inputs:
- `overlay.yaml` from enrich
- Original scaffolded CLI in the output directory

Outputs:
- Merged spec artifact
- Re-generated CLI in the output directory

Gates:
- Overlay merge completes without conflicts
- All seven generator quality gates pass again after regeneration

Artifacts:
- Merged spec (format follows the source spec)
- Updated CLI source tree and binary

### 6. review

Purpose: evaluate the regenerated CLI with one shipcheck block - dogfood, runtime verification, and scorecard together.

Inputs:
- Working CLI binary from regenerate (or from scaffold if regenerate was skipped)
- Spec used to generate the CLI

Outputs:
- `dogfood-results.json` in the pipeline directory
- `verification-report.json` in the output directory
- `scorecard.md` in the pipeline directory
- `review.md` summarizing the combined shipcheck result

Gates:
- Dogfood: structural checks (path validity, auth, dead flags, wiring) pass at the configured tier
- Verify: runtime behavioral checks against the real API or a mock server return PASS (or WARN after an auto-remediation pass)
- Scorecard: overall grade clears the operator's configured threshold

Artifacts:
- `pipeline/dogfood-results.json`
- `proofs/verification-report.json`
- `pipeline/scorecard.md`
- `pipeline/review.md`

### 7. agent-readiness

Purpose: run the `compound-engineering:cli-agent-readiness-reviewer` against the generated CLI and apply its recommended fixes in a severity-gated loop (two passes at most).

Inputs:
- Runtime verification results from review
- Working CLI binary in the output directory

Outputs:
- Reviewer scorecard across seven principles and three severities
- Fix implementation log (which fixes were applied, which were skipped or reverted)
- Phase verdict: `Pass`, `Warn`, or `Degrade`

Gates:
- `Pass` - zero Blockers and zero Frictions
- `Warn` - Frictions remain but no Blockers
- `Degrade` - Blockers remain; phase fails

Artifacts:
- Reviewer scorecard document in the pipeline directory
- Fix log in the pipeline directory

### 8. comparative

Purpose: score the generated CLI against the discovered alternatives on six dimensions.

Inputs:
- `research.json` from research
- `dogfood-results.json` from review
- Working CLI binary

Outputs:
- `comparative-analysis.md` in the pipeline directory with:
  - Score table (this CLI vs each alternative, 100 points maximum)
  - Gap summary
  - Advantage summary
  - Ship recommendation: `ship`, `ship-with-gaps`, or `hold`

Scoring dimensions:

| Dimension          | Points | How measured                                          |
|--------------------|--------|-------------------------------------------------------|
| Breadth            | 20     | Command count ratio vs the best alternative          |
| Install friction   | 20     | Single binary = 20, clone+build = 15, runtime = 10   |
| Auth UX            | 15     | env var + config = 15, env only = 10, manual = 5     |
| Output formats     | 15     | 5 per format (JSON, table, plain)                    |
| Agent friendliness | 15     | `--json` (5) + `--dry-run` (5) + non-interactive (5) |
| Freshness          | 15     | under 30d = 15, under 90d = 10, under 1yr = 5, older = 0 |

Gates:
- Ship recommendation is `ship` or `ship-with-gaps` (hold fails the phase)

Artifacts:
- `pipeline/comparative-analysis.md`

### 9. ship

Purpose: package the generated CLI output and produce the final handoff report.

Inputs:
- Review score and `review.md` from the review phase
- Working CLI binary ready for handoff

Outputs:
- Git repository initialized in the output directory
- Morning report written in the pipeline directory
- `.printing-press.json` provenance manifest at the CLI's root

Gates:
- All prior phases are `completed`
- Output directory contains a valid CLI source tree and compiled binary

Artifacts:
- Initialized git repo in the output directory
- Morning report in the pipeline directory
- Provenance manifest at the CLI root

## Related flows

Two other Printing Press entrypoints run flows that share structure with the managed pipeline:

- `printing-press run` drives `MakeBestCLI` in `internal/pipeline/fullrun.go`. It compresses the full flow into one call and reports a single `FullRunResult`. Its internal step list (research, generate, polish, coverage, dogfood, verification, workflow-verify, scorecard, fix plans, publish) maps to the managed phases but does not use the phase state machine.
- The `/printing-press` skill uses its own high-level Phase 0..5 layout documented in `README.md`. That layout is a compression of the same work, not a competing contract.

Both flows should produce artifacts that match the shape described here for the phases they cover.

## Shipcheck and phase gates

The `review` phase is where the shipcheck block runs - dogfood, verify, and scorecard together. These three checks also exist as standalone subcommands (`printing-press dogfood`, `printing-press verify`, `printing-press scorecard`) for operators who want to re-run a single check without advancing the pipeline. See `AGENTS.md` glossary entries for `dogfood`, `verify`, `scorecard`, and `shipcheck` for the canonical definitions.

## Keeping this document in sync

Phase names, ordering, and numbering are authoritative in `internal/pipeline/state.go`. Per-phase intent, inputs, outputs, and pointers are authoritative in `internal/pipeline/seeds.go`. When those files change, update this document in the same PR. A future follow-up may generate this document from those sources automatically.
