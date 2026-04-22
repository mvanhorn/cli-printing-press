---
title: "feat: Machine Hardening Bundle - Pipeline Contract, Live-Verify Trust, and Feature Discovery"
type: feat
status: active
date: 2026-04-22
---

# feat: Machine Hardening Bundle - Pipeline Contract, Live-Verify Trust, and Feature Discovery

## Overview

Four observations have accumulated from generating the current library of printed CLIs. Each points to a seam in the generator that already exists but is under-documented, under-enforced, or under-exposed. This plan bundles the four fixes into one machine-level change so every future printed CLI benefits:

1. Externalize the 9-phase pipeline as a portable contract document.
2. Add a "hand-rolled-response" gate at absorb-manifest review and at dogfood structural checks.
3. Make live-API verification a first-class scorecard dimension instead of collapsing it with mock-backed verify.
4. Emit a `which <capability>` resolver in every printed CLI, sourced from the feature index we already embed.

The bundle is scoped to the machine. No per-CLI carve-outs.

## Problem Frame

**Pipeline knowledge is locked inside Go.** `internal/pipeline/state.go` names the 9 phases. `internal/pipeline/fullrun.go` orchestrates them. `AGENTS.md` glossary defines terms. There is no single portable document that describes inputs, outputs, and gates for each phase. Onboarding, cross-tool parity work (e.g., Codex mode, any future agent-host target), and outside contributors all have to piece the contract together from source code and scattered prose.

**Hand-rolled endpoint bodies slip through.** Novel-feature commands sometimes return hardcoded JSON, compute aggregations in-process instead of calling the API's aggregation endpoint, or stub "returns 200 OK" without calling the client at all. The absorb manifest's existing kill/keep checks do not flag this pattern, and dogfood's `NovelFeaturesCheck` only checks whether a manifest feature appears in code - not whether the code actually talks to the API. When it slips through, verify catches it only when a mock and the real API disagree, which is one step too late.

**Mock-backed verify looks identical to live verify in the scorecard.** `AGENTS.md` already defines verify as running "against the real API (read-only) or a mock server." But the Tier 2 dimensions treat a PASS identically regardless of mode. A CLI that has never touched the real API can carry the same letter grade as one that has. That is misleading to anyone reading the scorecard to decide whether to install the CLI.

**Agents cannot query our own feature index.** Every printed CLI embeds a generated skill.md that lists every feature with command, description, group, and rationale (sourced from the `NovelFeature` struct in `internal/generator/generator.go`). Agents still parse the `--help` tree at runtime to find the right command. The curated index we already produce should be queryable directly.

## Requirements Trace

- R1. Produce `docs/PIPELINE.md` that lists each of the 9 phases with inputs, outputs, gates, and artifacts. A contributor with no access to the Go source should be able to describe what each phase consumes and produces.
- R2. Catch hand-rolled endpoint bodies at two points: at absorb-manifest review (a new kill check) and at dogfood (a structural check on novel-feature command bodies). Preserve legitimate local-SQLite-derived features (`stale`, `bottleneck`, `health`, `reconcile`).
- R3. Make live-API verification a distinct scorecard signal. A CLI that passed verify against the real API is distinguishable in the scorecard report from one that only passed against a mock.
- R4. Every printed CLI ships a `which <capability>` command that returns top matching subcommands from the embedded feature index. Typed exit code on no match so agents can branch without parsing error text.
- R5. No breaking changes to existing printed CLIs. Spot-check two library CLIs before landing weight changes; cap per-CLI grade movement at one letter.

## Scope Boundaries

- Not adding a REPL mode to printed CLIs. The host agent (Claude Code, Codex, Gemini CLI) is already the REPL. Nesting creates a second prompt, duplicates session management, and confuses keyboard semantics.
- Not adding an undo log. `--dry-run` already covers the pre-commit case, and inverse-operation knowledge is not derivable from an OpenAPI spec alone.
- Not changing phase ordering, renaming any phase, or introducing a new phase.
- Not modifying how printed CLIs distribute (`go install`, goreleaser, marketplace).

### Deferred to Separate Tasks

- Generating `PIPELINE.md` from Go comments so it cannot drift from `internal/pipeline/state.go`. Worth doing; hand-maintained for v1 to unblock R1 without building a new tool.
- Upgrading the `which` ranker beyond naive token matching (e.g., embeddings, fuzzy matching). Revisit after two library CLIs show real-world agent queries.

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/state.go` - phase enum (`PhasePreflight` through `PhaseShip`) and `PhaseOrder`.
- `internal/pipeline/fullrun.go` - orchestration; `FullRunResult` struct at lines 20-60.
- `internal/pipeline/scorecard.go` - `Scorecard`, `SteinerScore` with 18 dimensions at lines 32-69; weighting comment at line 66.
- `internal/pipeline/verify.go` - verify runner producing `VerifyReport` with PASS/WARN/FAIL verdicts.
- `internal/pipeline/live_check.go` - the existing live-mode check that Unit 3 threads into scoring.
- `internal/pipeline/dogfood.go` - `DogfoodReport` with `NovelFeaturesCheck` and the matcher in `novel_features_matcher_test.go`.
- `internal/pipeline/selfimprove.go` - `GenerateFixPlans` at lines 90-160 enumerates scoring dimensions for per-dim fix plans.
- `internal/generator/generator.go` - `NovelFeature` struct: `Command`, `Description`, `Rationale`, `Example`, `Group`, `WhyItMatters`.
- `internal/generator/templates/skill.md.tmpl` - per-CLI skill template (90 lines) that renders from `NovelFeature`.
- `internal/generator/templates/root.go.tmpl` line 111 - `--agent` flag expansion; the pattern for adding a new root subcommand.
- `internal/generator/templates/helpers.go.tmpl` - `isTerminal` and output-duality helpers; the pattern for table-vs-JSON output.
- `skills/printing-press/references/absorb-scoring.md` - Phase 1.5 kill/keep checks, current five.
- `AGENTS.md` glossary rows for `verify`, `dogfood`, `scorecard`, `doctor`, `shipcheck`.

### Institutional Learnings

- `AGENTS.md` scorer-coupling rule: "When adding a capability that affects scoring, update the scorer in the same change." Unit 3 must bundle the scorer change with the gate change.
- `AGENTS.md` machine-default: every unit here is a machine change, not a printed-CLI change. Each must generalize across APIs, spec formats, and auth patterns.
- Prior plan `docs/plans/2026-04-13-001-feat-machine-output-verification-plan.md` established the output-verification pattern; Unit 3 extends it rather than introducing a parallel track.

### External References

None. All four units are grounded in the current state of the PP repo.

## Key Technical Decisions

- **`docs/PIPELINE.md` lives at repo root `docs/`, not inside `AGENTS.md`.** Rationale: AGENTS.md is a conventions document for contributors; PIPELINE.md is a contract document for anyone (contributor or downstream tool) who needs to understand or implement PP's pipeline. Separating them lets readers who never open AGENTS.md find the pipeline spec immediately and keeps AGENTS.md from growing further.

- **The hand-rolled-response check is a new kill check (#6) plus a sibling dogfood check, not an extension of `NovelFeaturesCheck`.** Rationale: `NovelFeaturesCheck` answers "does the absorb-manifest feature appear in code?" The new check answers "does the code actually call the API it claims to wrap?" Different question, different failure mode, different fix plan. Keeping them separate produces actionable error messages instead of a single overloaded check.

- **`LiveAPIVerification` is its own Tier 2 dimension.** Rationale: folding this signal into `DataPipelineIntegrity` or `AgentNative` would dilute both dimensions and make the selfimprove fix plan generic and unhelpful. A distinct dimension is legible in the scorecard report, generates a targeted per-dim fix plan in `selfimprove.go`, and costs only one new field on `SteinerScore`.

- **Reconcile the Tier 1 / Tier 2 point split before adjusting weights.** The comment at `internal/pipeline/scorecard.go` line 66 says "50% infrastructure + 50% domain," while the `AGENTS.md` glossary says "Tier 1: 60 pts, Tier 2: 40 pts." Unit 3 picks one source of truth and updates the other before reallocating any weight, so the new dimension lands in a consistent scoring frame.

- **`which` reads the embedded skill.md feature index, not the live Cobra help tree.** Rationale: the help tree is a rendering of the command registry; the skill.md is the already-curated feature list with groupings and rationale. Ranking against the curated list gives better answers with less code and keeps `which` deterministic across runs.

- **Ranking is naive (exact-token, then substring, then group-tag), no embeddings.** Rationale: a typical printed CLI has 20-40 features; semantic retrieval is overkill. Naive ranking ships in one template and one test file. Revisit if library telemetry shows agents getting wrong answers on ambiguous queries.

## Open Questions

### Resolved During Planning

- **Should we add a REPL to printed CLIs?** No. See Scope Boundaries.
- **Should we add an undo log?** No. See Scope Boundaries.
- **Hand-maintain `PIPELINE.md` or generate it from Go?** Hand-maintain for v1. Generation is Deferred to Separate Tasks.
- **Which tier does live-verify belong to?** Tier 2 (Domain Correctness). It measures whether the CLI actually works against the real API, which is a correctness signal, not an infrastructure-presence signal.

### Deferred to Implementation

- Exact point weight for `LiveAPIVerification` within Tier 2. Depends on the Tier 1/Tier 2 reconciliation outcome and on spot-checking two library CLIs so overall grades don't shift by more than one letter.
- Whether the dogfood body scan uses AST parsing (precise but heavier) or string matching (faster but with false-positive risk). Decide after prototyping against one known-good and one known-bad fixture.
- Whether `which` returns 1 or 3 top matches by default. Decide after spot-checking against two library CLIs to see which return shape matches actual agent expectations.
- Whether the embedded feature index is serialized as a Go literal in a generated `.go` file or as an embedded JSON file via `embed.FS`. Either works; pick the one that keeps the template simpler.

## Implementation Units

- [ ] **Unit 1: Externalize the 9-phase pipeline as `docs/PIPELINE.md`**

**Goal:** Produce a portable, human-readable contract document for the pipeline so contributors, cross-tool parity work, and downstream readers can understand PP's generation flow without reading Go.

**Requirements:** R1

**Dependencies:** None

**Files:**
- Create: `docs/PIPELINE.md`
- Modify: `README.md` (add a "Pipeline Contract" link near the "Why These CLIs Win" section)
- Modify: `AGENTS.md` (replace any scattered phase prose with a pointer to `docs/PIPELINE.md`; preserve the glossary rows for `verify`, `dogfood`, `scorecard`, `shipcheck`, `doctor`)

**Approach:**
- Document each of the 9 phases (`preflight`, `research`, `scaffold`, `enrich`, `regenerate`, `review`, `agent-readiness`, `comparative`, `ship`) with four fields each: inputs (what the phase consumes), outputs (what it writes), gates (what must pass), artifacts (files produced under `manuscripts/<api>/<run>/`).
- Include a short preamble explaining the machine vs printed CLI distinction from `AGENTS.md` so the document is self-contained.
- Close with a "Keeping this document in sync" note pointing to `internal/pipeline/state.go` as the source of truth for phase names and ordering.

**Test scenarios:**
- Test expectation: none - this is documentation. Verification is by review against `internal/pipeline/state.go` and a markdown-lint pass.

**Verification:**
- `docs/PIPELINE.md` exists and lists all 9 phases in the same order as `PhaseOrder` in `internal/pipeline/state.go`.
- `README.md` links to it in a visible location.
- `AGENTS.md` no longer duplicates phase content; the glossary rows remain.
- A reader with no access to the Go source can correctly describe what each phase consumes and produces.

- [ ] **Unit 2: Hand-rolled-response gate at absorb and dogfood**

**Goal:** Refuse to ship a CLI whose novel-feature commands synthesize API responses locally instead of calling the API they claim to wrap, while preserving legitimate commands that read from the local SQLite store.

**Requirements:** R2

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press/references/absorb-scoring.md` (add Kill Check #6: "reimplements endpoint server-side," with examples and the SQLite-derived carve-out)
- Modify: `AGENTS.md` (add a new "Anti-Reimplementation" subsection under "Machine vs Printed CLI")
- Modify: `internal/pipeline/dogfood.go` (add `ReimplementationCheck` as a sibling to `NovelFeaturesCheck` in `DogfoodReport`; populate it from a body-scan function)
- Create: `internal/pipeline/reimplementation_check.go` (the body-scan helper)
- Test: `internal/pipeline/reimplementation_check_test.go`
- Test: `internal/pipeline/dogfood_test.go` (extend with new-check scenarios)

**Approach:**
- Kill Check #6 wording draws the line at "any novel feature whose implementation synthesizes API responses locally instead of calling the API." Examples in absorb-scoring.md: computing list totals in-process instead of using an aggregation endpoint, hardcoding enum mappings the API returns, stubbing "returns 200 OK" without calling the generated client.
- The body-scan helper for a novel-feature command function looks for three signals: (a) at least one call through the generated API client package, (b) no dominant-path return of a hardcoded JSON or struct literal that matches the endpoint schema, (c) no dominant-path return of a constant string presented as an API response.
- SQLite-derived commands (`stale`, `bottleneck`, `health`, `reconcile`) are exempt: if the body calls through the generated `store` package, the command is treated as a local-data command and the scan does not apply.
- Severity: WARN by default; FAIL when `DataPipelineIntegrity` also drops, so legitimate local-SQLite features are not over-punished.

**Test scenarios:**
- Happy path: a novel-feature command body that calls the generated client and transforms the response passes both the kill check and the dogfood scan.
- Happy path: a SQLite-derived command (`stale`) that calls the `store` package but no API client passes the scan (exempted as a local-data command).
- Error path: a novel-feature command body that returns a constant string with no client calls is flagged by the dogfood scan with message text mentioning "hand-rolled response."
- Error path: a novel-feature command body that returns a hardcoded struct literal matching the endpoint schema is flagged.
- Edge case: a novel-feature command body that calls both the API client and the store (e.g., caches response) passes the scan.
- Edge case: an empty function body (placeholder) is flagged with a distinct message "empty body, no implementation."
- Integration: running full `dogfood` on a fixture CLI with one compliant and one non-compliant novel-feature command produces a report naming only the non-compliant one.

**Verification:**
- Seeded synthetic absorb manifest entries that should fail Kill Check #6 are rejected in review output.
- `dogfood` on a fixture with a reimplemented feature reports the violation; the same command with an added API-client call passes.
- `AGENTS.md` renders cleanly with the new subsection; markdown lint passes.
- `selfimprove.GenerateFixPlans` produces an actionable fix plan when the new check fails.

- [ ] **Unit 3: `LiveAPIVerification` scorecard dimension**

**Goal:** Make live-API verification a distinct, legible signal in the scorecard so a CLI that passed verify against the real API is distinguishable from one that only passed against a mock.

**Requirements:** R3, R5

**Dependencies:** None (independent of Units 1, 2, 4)

**Files:**
- Modify: `internal/pipeline/scorecard.go` (add `LiveAPIVerification int` to `SteinerScore`; add `scoreLiveAPIVerification` function; assign a weight within the Tier 2 budget after the tier-split reconciliation)
- Modify: `internal/pipeline/verify.go` (add a `Mode` field to `VerifyReport` with values `live`, `mock`, `skipped`; source it from the existing live-vs-mock invocation path)
- Modify: `internal/pipeline/live_check.go` (expose the live-mode signal on `VerifyReport` via `Mode`)
- Modify: `internal/pipeline/fullrun.go` (thread `VerifyReport.Mode` into scorecard computation)
- Modify: `internal/pipeline/selfimprove.go` (add the new dimension to the `GenerateFixPlans` dimension list so a low score produces an actionable fix plan)
- Modify: `AGENTS.md` **or** `internal/pipeline/scorecard.go` line 66 comment (reconcile the Tier 1 / Tier 2 split before adjusting weights; fix whichever source currently disagrees with the final, authoritative split)
- Test: `internal/pipeline/scorecard_test.go` (new scenarios)
- Test: `internal/pipeline/verify_test.go` or `internal/pipeline/live_check_test.go` (extend for `Mode` reporting)

**Approach:**
- `VerifyReport.Mode` is an enum with values `live`, `mock`, `skipped`. Assigned based on the existing flags that already distinguish mock-server verify from live verify.
- `scoreLiveAPIVerification` returns 0 for `skipped` or `mock`, scales with live pass ratio, caps at the dimension max when live pass ratio >= 0.95. When verify ran in `mock` or `skipped` mode, the dimension also appears in `UnscoredDimensions` with a reason string ("verify ran against mock server" / "verify was skipped").
- Weight allocation: borrow 4-5 pts from the existing Tier 2 budget. Before reallocating, resolve the Tier 1/Tier 2 discrepancy (scorecard.go comment vs AGENTS.md glossary). Validate the final split against two library CLIs so overall grades don't shift by more than one letter.
- Ship phase gate remains configurable. The new dimension raises visibility without auto-blocking publish, unless the operator opts in to strict mode.

**Test scenarios:**
- Happy path: verify ran live with 9/10 passing - dimension scores 9/10.
- Happy path: verify ran live with 10/10 passing - dimension scores 10/10 and caps.
- Edge case: verify ran in mock mode - dimension scores 0 and appears in `UnscoredDimensions` with reason "verify ran against mock server."
- Edge case: verify was skipped entirely - dimension scores 0 and appears in `UnscoredDimensions` with reason "verify was skipped."
- Error path: `VerifyReport.Mode` is an unexpected value - scoring function returns 0, logs a warning, does not panic.
- Integration: `fullrun` end-to-end on a fixture with live verify produces a scorecard where `LiveAPIVerification` is populated and the total reflects the reallocation.
- Integration: `selfimprove.GenerateFixPlans` produces a fix plan for the new dimension when it scores low.

**Verification:**
- `go test ./internal/pipeline/...` passes with all new scenarios.
- `printing-press scorecard --api <fixture>` output shows the new dimension in both human and JSON formats.
- Spot-check on two library CLIs: neither drops more than one letter grade after the reallocation.
- Tier 1/Tier 2 split is consistent between `scorecard.go` and `AGENTS.md` glossary.

- [ ] **Unit 4: `<cli> which <capability>` feature-to-command resolver**

**Goal:** Every printed CLI can answer "which command handles X" from its embedded feature index so agents do not have to parse the full help tree to discover a feature.

**Requirements:** R4

**Dependencies:** None

**Files:**
- Create: `internal/generator/templates/which.go.tmpl`
- Create: `internal/generator/templates/which_test.go.tmpl`
- Modify: `internal/generator/templates/root.go.tmpl` (wire `which` as a root subcommand alongside existing ones)
- Modify: `internal/generator/templates/skill.md.tmpl` (document the `which` command in the agent recipes section of every generated skill)
- Modify: `internal/generator/generator.go` (ensure the `NovelFeature` list plus headline features are available to the `which` template as the embedded index, with `Command`, `Description`, `Group`, and any keyword tags)

**Approach:**
- `which` accepts a free-text query and returns the top 1-3 matching commands, ranked by: exact token match on `Command` (highest), substring match on `Description`, group-tag match on `Group`. Ties broken by declaration order in the feature index.
- Data source is the embedded feature index generated at build time from the same `NovelFeature` slice that populates the generated skill.md. No runtime scraping of help text, no external registry.
- Output rules match every other generated command: JSON when piped or when `--json` is set, table when TTY. Exit code `0` on match, `2` on no match (typed exit so agents can branch without parsing error text).
- On no-match, print a single-line hint: `no match; try '<cli> --help' or '<cli> search <term>'`.

**Test scenarios:**
- Happy path: query "search messages" against a Slack-style fixture returns the `search` command as the top match.
- Happy path: query "stale tickets" against a Linear-style fixture returns the `stale` command.
- Happy path: piped output produces JSON; TTY output produces a 3-column table of command, description, score.
- Edge case: empty query returns the full feature list in ranked-by-group order, exit 0.
- Edge case: query matches by group tag only (no exact or substring match) returns the group's primary command with a lower score.
- Error path: query with no matches exits 2 and prints the hint line on stderr.
- Integration: running `<cli> which <q>` on a generated fixture CLI with a known feature index returns results consistent with the skill.md for that CLI.

**Verification:**
- Generated CLIs pass the existing quality gates plus a new `which` smoke test added to the generated test file.
- `doctor` on a freshly generated fixture CLI confirms `which` is wired and has a populated feature index.
- Manual spot-check against two library CLIs: a query for a known headline feature returns that feature as the top result.
- `which` appears in the generated skill.md agent recipes section for every regenerated CLI.

## System-Wide Impact

- **Interaction graph:** Unit 3 couples `verify.go`, `live_check.go`, `fullrun.go`, `scorecard.go`, and `selfimprove.go` through the new `VerifyReport.Mode` field. Units 1, 2, 4 are independent of this chain. Unit 2 adds a new `ReimplementationCheck` sibling to `NovelFeaturesCheck` in `DogfoodReport`. Unit 4 adds a subcommand in generated code paths but does not touch shared packages.
- **Error propagation:** Unit 2's body-scan must not panic on unusual but valid Go (e.g., closures, goroutines). Unit 3 must treat unexpected `Mode` values as "score 0, log, continue" rather than crashing the scorecard. Unit 4's `which` must surface spec-parsing errors at generation time, not hide them.
- **State lifecycle risks:** None. No new persistent state, no migrations, no cache layer.
- **API surface parity:** The printing-press binary itself gains no new flags or commands in Units 1-3. Unit 4 adds a subcommand to every printed CLI, which is an additive change safe across existing users.
- **Integration coverage:** Unit 3's happy path is exercised by a `FULL_RUN=1` integration test against at least one real API fixture so the live-mode scoring is not only unit-tested. Unit 2's integration scenario requires a fixture with two novel-feature commands (one compliant, one not) to prove the scan flags only the offender.
- **Unchanged invariants:** 9-phase pipeline ordering, phase names, existing scorecard dimension semantics, `--agent` flag expansion, `verify --fix` behavior, and the `-pp-cli` binary naming convention.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Weight reallocation in Unit 3 shifts existing CLI grades unexpectedly | Reconcile the Tier 1/Tier 2 split first; spot-check two library CLIs before landing; cap grade movement at one letter; document the reallocation in CHANGELOG. |
| Unit 2's body-scan produces false positives on legitimate local-SQLite-derived features | Explicit exemption for commands that call the generated `store` package; test scenario locks this behavior; WARN-by-default severity so a borderline case surfaces without blocking. |
| `docs/PIPELINE.md` drifts from `internal/pipeline/state.go` over time | v1 is hand-maintained; add a pointer in AGENTS.md reminding authors to update both together; generation from Go is a follow-up task already captured in Deferred to Separate Tasks. |
| Unit 4's naive ranking misleads agents on ambiguous queries | Typed exit code `2` signals no confident match; skill.md documents that agents should fall back to `<cli> --help` or `<cli> search` on exit 2; ranker upgrade path is captured in Deferred to Separate Tasks. |
| Plan touches AGENTS.md, which is user-facing - formatting must follow the existing glossary-table conventions | Reuse existing glossary-style tables; run markdown lint; review diff against current AGENTS.md before landing. |
| Unit 3's integration test requires a live API fixture, which may be flaky | Keep the live fixture scoped to a stable read-only endpoint; fall back to the existing mock fixture for CI; use `FULL_RUN=1` to gate live tests so they don't run in every commit. |

## Documentation / Operational Notes

- `CHANGELOG.md` entries per unit at commit time using the `feat(cli)` scope.
- README adds a "Pipeline Contract" link near the "Why These CLIs Win" section.
- Every printed CLI's README template picks up `which` in its command list on the next regeneration or `emboss` cycle. Existing library CLIs will gain `which` when next re-run.
- Unit 3's CHANGELOG entry explicitly documents the Tier 1/Tier 2 reconciliation and any per-dim weight changes so operators spot-checking scores understand grade movement.

## Alternative Approaches Considered

- **Add a nested REPL to every printed CLI.** Rejected. The host agent is already the REPL; nesting creates a second prompt and duplicates session management that the host provides. Host-level REPLs already give history, multi-step coordination, and undo at the conversation layer.
- **Add an undo log template backed by a `transactions` SQLite table.** Rejected for now. `--dry-run` covers the preview-before-commit case, and true inverse-op knowledge is not derivable from an OpenAPI spec. Revisit if a concrete write-heavy CLI (Linear bulk ops, HubSpot imports) hits real pain.
- **Make `which` a runtime help-text parser instead of reading the embedded feature index.** Rejected. Parsing help text couples `which` to the Cobra output format and is fragile across Cobra upgrades. Reading the embedded index gives a deterministic, versioned source of truth.
- **Fold `LiveAPIVerification` into an existing scorecard dimension.** Rejected. It dilutes the existing dimension, makes the selfimprove fix plan generic and unhelpful, and hides the signal from the scorecard report. One extra field on `SteinerScore` is a cheap way to keep the signal legible.
- **Generate `PIPELINE.md` from Go comments immediately.** Rejected for v1. Shipping a hand-maintained contract now is more valuable than blocking on a generation tool. Generation is captured as a follow-up.

## Sources & References

- Pipeline phase definitions: `internal/pipeline/state.go`
- Pipeline orchestration: `internal/pipeline/fullrun.go`
- Scorecard structure: `internal/pipeline/scorecard.go` lines 32-69
- Verify mode distinction: `AGENTS.md` glossary entry for `verify`
- Dogfood structural checks: `internal/pipeline/dogfood.go` and `internal/pipeline/novel_features_matcher_test.go`
- Absorb manifest kill/keep checks: `skills/printing-press/references/absorb-scoring.md`
- Per-CLI skill template: `internal/generator/templates/skill.md.tmpl`
- Novel feature struct: `internal/generator/generator.go`
- Prior related plan: `docs/plans/2026-04-13-001-feat-machine-output-verification-plan.md`
