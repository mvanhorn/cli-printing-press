---
title: "fix: Wave 1 retro blockers — phase5 gate, generate-time run_id, sync --json routing"
type: fix
status: active
date: 2026-05-04
---

# fix: Wave 1 retro blockers — phase5 gate, generate-time run_id, sync --json routing

## Summary

Three small, independent fixes to the Printing Press that unblock universal failure modes in the canonical pipeline: align `phase5_gate.go`'s quick-PASS predicate with the runner's evolved formula in `live_dogfood.go`, stamp `RunID` into `.printing-press.json` at `generate` time (with a SKILL state.json template fix as belt-and-suspenders), and route sync template's NDJSON event lines to stdout under `--json` while keeping human-readable progress on stderr.

---

## Problem Frame

The canonical generation pipeline currently hits three reproducible hard-fails: (1) freshly-generated CLIs whose live-dogfood matrix produces 4–5 passing entries are rejected by `lock promote`'s phase5 gate even though the runner verdict is PASS; (2) `printing-press dogfood --live --write-acceptance` refuses to write the gate marker because the manifest's `run_id` is null on ~12% of v3.x library CLIs; (3) `<cli> sync --json | jq` returns nothing because every JSON event line is emitted to stderr. Each surfaced in retros #586 (Hacker News) and #587 (Trigger.dev) as P1 universal blockers.

---

## Requirements

- R1. `phase5_gate.go`'s `phase5AcceptancePassed` quick arm matches the runner's PASS condition in `finalizeLiveDogfoodReport` exactly: a marker with `level=quick, matrix_size>=4, tests_passed>=min(5, matrix_size)` returns gate-pass; below those thresholds returns gate-fail.
- R2. `phase5_gate.go`'s full arm is unchanged.
- R3. `printing-press generate --research-dir <run-dir>` produces a manifest where `jq .run_id .printing-press.json` returns a non-null `YYYYMMDD-HHMMSS` string matching the run's RunID.
- R4. `printing-press dogfood --live --write-acceptance <path>` runs to completion on a freshly-generated CLI without the "manifest missing run_id" error.
- R5. SKILL.md's Run Initialization state.json template includes `"run_id": "$RUN_ID"`; the bash bootstrap that emits `STATE_FILE` populates the field.
- R6. Existing CLIs with `state.json` containing `run_id` continue to work unchanged.
- R7. `<cli> sync --json 2>/dev/null | jq -c .` parses every line as valid JSON and produces non-empty output (NDJSON contract on stdout under `--json`).
- R8. `<cli> sync` without `--json` continues to produce the existing human-readable stream on stderr; no behavior change in the non-JSON path.
- R9. Sync event schemas (event names, fields, ordering) are unchanged.

---

## Scope Boundaries

- Wave 2/3/4 retro WUs (#593, #595, #596, #598, #599, #600, #601, #602) are out of scope.
- Backfilling `run_id` into already-shipped library CLIs (allrecipes, yahoo-finance, trigger-dev) is out of scope; new generations only.
- Refactoring `phase5_gate.go` beyond the matrix_size + tests_passed alignment is out of scope.
- Adding a `--json`-vs-`--human-friendly` flag-conditional in the sync template is out of scope; the template's existing `humanFriendly` conditional already gates plain-text variants — this work just routes the existing JSON branch correctly.
- Promote-time and package-time run_id handling are unchanged; this work only addresses generate-time emission. (Per #591's explicit boundary; the prior allrecipes #535 / dub #515 observations on those code paths stay deferred.)
- Changing the CLI manifest schema beyond confirming `RunID` flows through is out of scope.

---

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/phase5_gate.go:152-174` — `phase5AcceptancePassed`. Quick arm at lines 155–162 hardcodes `MatrixSize != 6` and `TestsPassed < 5`; full arm at 163–170 is correct.
- `internal/pipeline/live_dogfood.go:880-906` — `finalizeLiveDogfoodReport`. Authoritative verdict logic; quick PASS at line 901 uses `report.MatrixSize >= 4 && report.Passed+report.Skipped >= min(5, report.MatrixSize)`.
- `internal/pipeline/phase5_gate_test.go:42-80` — `TestValidatePhase5Gate_QuickPassAllowsOneNonBlockingMiss` and `TestValidatePhase5Gate_QuickPassRequiresFiveOfSix`. Per-case top-level `Test*` functions, testify, helper `writePhase5GateMarker` for fixture writes. Mirror this shape for new range cases (one function per `(matrixSize, testsPassed, expected)` triple).
- `internal/pipeline/climanifest.go:326-348` — `GenerateManifestParams` struct + `WriteManifestForGenerate`. The struct does not carry `RunID` today; the constructed `CLIManifest` at lines 341–348 never sets `m.RunID`. This is the gap.
- `internal/pipeline/publish.go:176, :203` — `writeCLIManifestForPublish` already stamps `RunID: state.RunID` at publish. Reference for how the field should flow.
- `internal/cli/root.go:161, :321` — call sites of `WriteManifestForGenerate`. Both must pass the new RunID parameter.
- `skills/printing-press/SKILL.md:386-413` — Run Initialization. SKILL constructs `RUN_ID="$(date +%Y%m%d-%H%M%S)"` at line 391 but the state.json template at lines 401–413 has 4 fields and never persists `run_id`.
- `internal/generator/templates/sync.go.tmpl` — 51 `Fprint*` calls on stderr. Issue #592 enumerates the 19 JSON-event-emit lines specifically; the remaining stderr writes are human-readable progress and stay on stderr.
- `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cli/sync.go` and `testdata/golden/expected/generate-tier-routing-api/tier-routing-golden/internal/cli/sync.go` — generated-source goldens that will need re-baselining when sync.go.tmpl changes.
- `testdata/golden/fixtures/dogfood-verdict-matrix/{pass, fail-path-auth-dead, warn-priority}/internal/cli/sync.go` — fixture inputs for the verdict-matrix golden case. Whether they need regen depends on whether the case asserts on stdout/stderr text from sync; verify with `scripts/golden.sh verify` after the template change.
- `scripts/golden.sh` and `docs/GOLDEN.md` — canonical update flow is `scripts/golden.sh update`, then explain the diff in the PR.

### Institutional Learnings

- `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` (active, U4) — already migrated `live_dogfood.go::finalizeLiveDogfoodReport` to the new quick-PASS formula. The current phase5_gate disagreement is post-extraction drift: the gate was added with the old formula and the runner update never propagated. Plan reuses U4's exact predicate.
- `docs/plans/2026-05-04-004-test-live-dogfood-resolve-success-coverage-plan.md` (active) — confirms `internal/pipeline/` test conventions: per-test top-level `Test*` functions, testify, `t.TempDir()`, no table-driven `t.Run` nesting. Apply to U1's new test cases.
- `docs/plans/2026-03-28-002-feat-cli-manifest-plan.md` (completed) — original manifest design explicitly excluded standalone `generate` from manifest writing. That boundary has since been reversed (the writer exists at `climanifest.go:340` today). U2 finishes the contract that the post-3-28 reversal left incomplete by stamping the field that was already in the schema.
- `docs/plans/2026-03-25-feat-elevenlabs-learnings-human-friendly-ndjson-plan.md` (active) — establishes the `--json` agent contract: "stdout must be parseable JSON; events on stderr are progress streams." NDJSON-on-stdout (every line a valid JSON object) **satisfies** this contract — the prohibition is on plain-text events corrupting a JSON array. U4 must produce NDJSON, not interleaved text.
- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md` — ordering invariant: hard-fail short-circuits before threshold-based PASS arms. The runner's `case Failed > 0 || MatrixSize == 0 → FAIL` first / quick-PASS second is the precedent. Verify the gate's switch preserves the same ordering after the predicate change.
- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md` — canonical run_id origin is `state.RunID` on `PipelineState`. Don't invent a new derivation path; thread the same value the publish writer already uses.

### External References

None required. All decisions ground in the local codebase and active plans above.

---

## Key Technical Decisions

- **Three separate PRs, in this order: U1 → U2+U3 → U4.** Each PR is its own review surface (gate logic / generator+skill / template) and can ship independently. PRs do not block each other technically — order is for review-batch sanity (smallest blast radius first; goldens last). U2 and U3 ship together because they are belt-and-suspenders for the same root cause; splitting them would let one half ship without the other, leaving the gap. PR titles per AGENTS.md commit-style: `fix(cli):` for U1 and U4; `fix(cli):` for U2+U3 with the SKILL.md edit explicitly justified in the body as serving a binary contract (alternative: `fix(skills,cli):` if joint-scope syntax is preferred — implementer/PR author chooses at submission time).
- **Gate predicate mirrors the runner exactly.** Replace the gate's `MatrixSize != 6` with `MatrixSize < 4`, and `TestsPassed < 5` with `TestsPassed < min(5, MatrixSize)`. The marker's `TestsPassed` field is the runner's already-aggregated PASS-eligible count; the gate consumes it as-is. Implementation gate: before changing the predicate, the implementer must read `writeLiveDogfoodAcceptance` (`live_dogfood.go:908+`) and confirm whether `TestsPassed` carries `Passed+Skipped` (matches the runner's PASS condition directly) or `Passed` only (predicate must use a different field or the runner's own counts). The predicate change is conditional on this verification — see U1 Approach.
- **`run_id` flows through `GenerateManifestParams`, not via `state.RunID` at the writer.** The writer is standalone (no `PipelineState`); the value must be passed in by callers. The `generate` command does not load a `PipelineState` today (verified: `internal/cli/root.go` has no `LoadState` call before the two `WriteManifestForGenerate` sites at :161 and :321), so the only viable derivation at generate time is `filepath.Base(researchDir)` when `--research-dir` matches `^\d{8}-\d{6}$`. Empty fallback (legacy `--output`-only path) emits a one-line stderr warning that Phase 5 acceptance will refuse to write without it.
- **SKILL state.json template fix updates both the template block AND the bash bootstrap** that writes `STATE_FILE`. Updating only the template wouldn't propagate; the bootstrap is what actually emits the JSON. The SKILL fix is the upstream source so that future `printing-press generate` invocations driven by the skill carry `run_id` from the start, even before U2's basename-derivation fallback.
- **Sync template under `--json` produces NDJSON on stdout.** Each event line is `fmt.Fprintf(cmd.OutOrStdout(), ...)` (or equivalent); the existing `humanFriendly` branch routes plain-text variants to stderr unchanged. The 19 event-emit lines listed in #592 are the specific targets; non-event Fprintf-to-stderr calls stay on stderr.
- **Golden re-baseline for U4.** Run `scripts/golden.sh update` after the template change, inspect the diff, document the expected stderr→stdout shift in the PR description per `docs/GOLDEN.md`. Verify-matrix fixtures will be inspected during U4 to determine if the dogfood-verdict-matrix case asserts on the affected stream.

---

## Open Questions

### Resolved During Planning

- **Are events-on-stderr the canonical agent contract?** Yes for plain-text progress; no for NDJSON-under-`--json`. The ElevenLabs convention prohibits text events corrupting a JSON array on stdout — NDJSON-on-stdout (every line a JSON object) satisfies the contract.
- **Should run_id be in the manifest schema?** Already is (`internal/pipeline/climanifest.go:RunID` field exists). U2 just populates it; no schema change.
- **One PR or three?** Three. Each WU is independent and addresses a different review surface; combining them obscures per-fix review signal.

### Deferred to Implementation

- **`TestsPassed` field semantics in `Phase5GateMarker` (BLOCKING for U1).** Runner aggregates `Passed+Skipped` for its PASS condition; the marker exposes a single `TestsPassed` field. Before changing the predicate, implementer reads `writeLiveDogfoodAcceptance` (`live_dogfood.go:908+`) to confirm which value the marker carries. If the field carries `Passed+Skipped`, the documented predicate (`TestsPassed < min(5, MatrixSize)`) lands as-is. If the field carries `Passed` only, the predicate must change to read the live counts or the marker schema must be extended. This is a prerequisite, not a post-change comment.
- **Whether the verdict-matrix golden fixtures need regeneration (U4).** The dogfood-verdict-matrix case may or may not assert on sync's stdout/stderr text. Run `scripts/golden.sh verify` after the template change; if the case fails, regenerate; if it passes, leave fixtures alone.
- **Exact location of the JSON-emit branch inside `sync.go.tmpl` (U4).** The 19 line numbers in #592 anchor the targets, but the surrounding template structure (whether each line is inside an existing `if humanFriendly`/`else` or at an unconditional emit point) determines whether the fix is a 19-line `os.Stderr→os.Stdout` swap or a structural conditional add. Implementer reads the template before patching; the line numbers themselves may have drifted, so a grep for the JSON-event payload shape (look for the `event:` field literal) is the more reliable anchor.

---

## Implementation Units

- U1. **Align phase5 quick-gate with the runner's PASS condition**

**Goal:** Gate's quick arm passes/fails on exactly the same predicate the runner uses to set `Verdict = "PASS"`, so `lock promote` accepts every CLI the runner accepted.

**Requirements:** R1, R2.

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/phase5_gate.go`
- Modify: `internal/pipeline/phase5_gate_test.go`

**Approach:**
- **Prerequisite:** read `writeLiveDogfoodAcceptance` (`live_dogfood.go:908+`) and confirm what `marker.TestsPassed` carries — `Passed+Skipped` (the runner's PASS-condition aggregate) or `Passed` only. The predicate below assumes `Passed+Skipped`; if the marker actually carries `Passed`, the predicate must read the live `Passed/Skipped` counts the marker also exposes (or the marker schema must be extended). Resolve before patching, not after.
- In `phase5AcceptancePassed`'s quick arm (`phase5_gate.go:155-162`): replace `marker.MatrixSize != 6` with `marker.MatrixSize < 4`; replace `marker.TestsPassed < 5` with `marker.TestsPassed < min(5, marker.MatrixSize)`.
- Update the failure-detail string templates to drop the hardcoded "/6" and use the actual `MatrixSize` (e.g., `"%d/%d tests passed"` formatting).
- Add a one-line comment at the gate site naming the `live_dogfood.go::finalizeLiveDogfoodReport` predicate as the source-of-truth contract this gate mirrors, so future drift is caught at code-review.
- Hard-fail ordering preserved — full arm and skip/unknown-status branches unchanged.

**Patterns to follow:**
- `internal/pipeline/live_dogfood.go:898-905` — runner's verdict switch ordering (Failed-or-empty first, quick-PASS second). Mirror.
- `internal/pipeline/phase5_gate_test.go:42-80` — `TestValidatePhase5Gate_QuickPass*` shape: per-case top-level `Test*` function, testify, `writePhase5GateMarker` helper.

**Test scenarios:**
- *Happy path:* `level=quick, matrix_size=5, tests_passed=5` → `result.Passed == true`.
- *Happy path:* `level=quick, matrix_size=4, tests_passed=4` → `result.Passed == true`.
- *Happy path:* `level=quick, matrix_size=6, tests_passed=5` → `result.Passed == true` (preserves the existing one-skip-allowed semantics for the standard 6-entry matrix).
- *Edge case:* `level=quick, matrix_size=3, tests_passed=3` → `result.Passed == false`, `result.Detail` mentions matrix_size floor of 4.
- *Edge case:* `level=quick, matrix_size=6, tests_passed=4` → `result.Passed == false`, `result.Detail` mentions tests_passed shortfall (4 < min(5, 6) = 5).
- *Edge case:* `level=quick, matrix_size=4, tests_passed=3` → `result.Passed == false` (3 < min(5, 4) = 4).
- *Regression:* existing `TestValidatePhase5Gate_QuickPassRequiresFiveOfSix` (`phase5_gate_test.go:62-80`) still passes after the predicate change — `matrix_size=6, tests_passed=4` is still a fail; check that the failure-detail assertion (`assert.Contains(t, result.Detail, "5/6")`) is updated to the new format if the assertion text matches.

**Verification:**
- `go test ./internal/pipeline/ -run TestValidatePhase5Gate` passes including all new range cases.
- Manual or integration check: a freshly-generated CLI with a quick-level marker showing `matrix_size=5, tests_passed=5` is accepted by `printing-press lock promote`.

---

- U2. **Stamp `RunID` into the manifest at generate time**

**Goal:** `printing-press generate --research-dir <dir>` produces a `.printing-press.json` with a populated `run_id` field, removing the manual `jq` backfill currently required to make Phase 5 acceptance writable.

**Requirements:** R3, R4, R6.

**Dependencies:** None (independent of U1).

**Files:**
- Modify: `internal/pipeline/climanifest.go` (struct + writer)
- Modify: `internal/cli/root.go` (call sites at :161 and :321; thread RunID through)
- Modify: `internal/pipeline/climanifest_test.go` (new test cases for the stamp)

**Approach:**
- Add `RunID string` to `GenerateManifestParams` (`climanifest.go:326-335`).
- In `WriteManifestForGenerate` (`climanifest.go:340`), set `m.RunID = p.RunID`. Mirror `writeCLIManifestForPublish`'s pattern (`publish.go:203`) so both writers handle the field identically.
- In `internal/cli/root.go` at the two call sites (`:161`, `:321`), derive the RunID by basename when `--research-dir` is supplied AND the basename matches `^\d{8}-\d{6}$` (the canonical run_id shape); fall back to empty string otherwise. The `generate` command does not load a `PipelineState` today, so basename-or-empty is the full derivation surface — there is no `state.RunID` priority arm to consider unless `generate` is later refactored to load state. (If a state-loading path is added in a future change, the derivation gains a priority-1 source naturally; this plan does not pre-empt that.)
- Empty fallback preserves the legacy `--output`-only path; emit a one-line `fmt.Fprintln(os.Stderr, ...)` warning that Phase 5 acceptance will refuse to write without it. Deliberate one-time signal, not a blocker.

**Patterns to follow:**
- `internal/pipeline/publish.go:203` — `RunID: state.RunID` stamp inside the publish writer. Same shape, different writer.
- `internal/pipeline/climanifest_test.go` — the same package's test file; testify table-driven, `t.TempDir()`. Match the existing test ordering (the package's `setPressTestEnv` helper if present).

**Test scenarios:**
- *Happy path:* call `WriteManifestForGenerate` with `RunID: "20260504-190931"`, then read the file back via `ReadCLIManifest` — manifest's `RunID` matches input.
- *Edge case:* `RunID: ""` — manifest writes successfully; `RunID` field absent or empty (depends on `omitempty` behavior). No panic, no error.
- *Edge case:* manifest already exists with a `RunID`; calling `RefreshCLIManifestFromSpec` preserves it (regression check; per `climanifest.go:128` comment).
- *Integration:* `printing-press generate --research-dir /tmp/runs/20260504-190931` followed by `jq .run_id .printing-press.json` returns `"20260504-190931"`.
- *Integration:* `printing-press generate` without `--research-dir` emits the warning to stderr and `manifest.RunID == ""`.
- *Integration:* `printing-press dogfood --live --write-acceptance <path>` runs to completion on a freshly-generated CLI without "manifest missing run_id" — covered by U2 + U3 together; test through end-to-end script in `scripts/` if one exists, otherwise document as a manual smoke step in the PR.

**Verification:**
- `go test ./internal/pipeline/ -run TestWriteManifestForGenerate` (new test name) passes.
- `go test ./...` passes overall (no regressions in `climanifest_test.go` or callers).
- Manual smoke: generate a CLI with `--research-dir`, inspect the manifest, run `dogfood --live --write-acceptance` end-to-end without the "missing run_id" error.

---

- U3. **Add `run_id` to SKILL state.json template and bootstrap**

**Goal:** Agents copying the SKILL.md state.json template into their bootstrap script always populate `run_id`, so the file the generator reads upstream of U2's fallback already carries the field.

**Requirements:** R4, R5, R6. (R4 is jointly satisfied by U2 + U3 — U2 alone is the single-PR fallback when SKILL bootstrap is bypassed; U2 + U3 together close the canonical agent-driven generation path.)

**Dependencies:** None functionally; ships in the same PR as U2 (belt-and-suspenders).

**Files:**
- Modify: `skills/printing-press/SKILL.md` (Run Initialization, lines 386–413).

**Approach:**
- Locate the `state.json` template block (`SKILL.md:401-413`). Add a `"run_id": "$RUN_ID"` line to the JSON body.
- Verify the surrounding bash that emits `STATE_FILE` already has `RUN_ID` in scope from the `RUN_ID="$(date +%Y%m%d-%H%M%S)"` line at `SKILL.md:391`. If the heredoc is single-quoted (`<<'EOF'`) the variable won't expand — change to unquoted `<<EOF` and confirm escaping for any other `$` references.
- No code change in this unit beyond the SKILL.md text edit.

**Patterns to follow:**
- Other SKILL.md state-template fields (`api_name`, `working_dir`, `output_dir`, `spec_path`) — same indentation, same `"$VAR"` quoting style.

**Test scenarios:**
- Test expectation: none — pure documentation/template change. Verification is editorial and confirmed at implementation by reading the resulting bootstrap output of a fresh agent run.

**Verification:**
- A dry copy-paste of the SKILL.md state.json block into a shell session produces a `state.json` with a populated `run_id` field matching `$(date +%Y%m%d-%H%M%S)`.
- The next time `/printing-press <api>` runs, `state.json` carries `run_id` before `generate` is invoked. (U2's basename derivation still kicks in independently when `--research-dir` is supplied; the SKILL fix simply ensures the canonical state file is well-formed for any future state-loading consumer.)

---

- U4. **Route sync template's NDJSON event lines to stdout under `--json`**

**Goal:** `<cli> sync --json | jq` works as the canonical agent flow on every printed CLI: structured event lines arrive on stdout (NDJSON, one event per line), human-readable progress remains on stderr.

**Requirements:** R7, R8, R9.

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/templates/sync.go.tmpl` (the 19 JSON-event-emit lines listed in #592)
- Re-baseline: `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cli/sync.go`
- Re-baseline: `testdata/golden/expected/generate-tier-routing-api/tier-routing-golden/internal/cli/sync.go`
- Re-baseline (if affected): `testdata/golden/fixtures/dogfood-verdict-matrix/{pass,fail-path-auth-dead,warn-priority}/internal/cli/sync.go`

**Approach:**
- Read `sync.go.tmpl` around each enumerated line in #592 (254, 284, 318, 384, 390, 403, 425, 442, 453, 471, 473, 490, 505, 533, 538, 1033, 1077, 1084, 1096, 1107). Each is a `fmt.Fprintf(os.Stderr, ...)` emitting a structured JSON event.
- Replace the destination of each enumerated line with `cmd.OutOrStdout()` (or whatever stdout writer the surrounding RunE already uses; the template's existing JSON emit branch in other commands is the precedent).
- Leave non-event `Fprintf(os.Stderr, ...)` calls alone — those are human-readable progress under the `humanFriendly` branch.
- Run `scripts/golden.sh verify` to identify exactly which goldens diverged. Run `scripts/golden.sh update` to re-baseline. Inspect the diff; the only intentional changes should be `os.Stderr → cmd.OutOrStdout()` (or equivalent) on the listed event lines.
- Document the diff in the PR description with a one-paragraph rationale citing #592 and the ElevenLabs NDJSON-on-stdout reconciliation.

**Patterns to follow:**
- The existing `humanFriendly` conditional in `sync.go.tmpl` — already gates plain-text variants to stderr correctly; this fix is the symmetric routing for the JSON variants.
- `docs/GOLDEN.md` — the canonical update flow: verify, update, inspect, explain.
- Prior generator-template polish PRs (e.g., the recent #572 trailing-line fix) for the PR-description shape.

**Test scenarios:**
- *Happy path:* regenerate any sync-having CLI; `<cli> sync --json 2>/dev/null | jq -c .` returns a non-empty NDJSON stream where every line parses as JSON.
- *Happy path:* regenerate a sync-having CLI; `<cli> sync --json | jq -s '.[-1].event'` returns `"sync_summary"` (validates the trailing-summary fix from #572 still holds).
- *Happy path:* `<cli> sync` (no `--json`) produces the same human-readable progress on stderr as before; stdout stays clean.
- *Edge case:* a `--json` invocation with `2>/dev/null` (stderr fully suppressed) still produces the complete NDJSON stream on stdout; no events are lost to stderr.
- *Regression:* existing sync golden cases (`generate-golden-api`, `generate-tier-routing-api`) — re-baselined diff is exactly the stderr→stdout swap for the listed lines, no schema or ordering changes.
- *Regression:* `dogfood-verdict-matrix` golden case still passes after fixture inspection; if it relied on stderr-stream content, fixtures are regenerated and the case re-passes.
- Test expectation for runtime stdout/stderr behavior: none in the harness today (no runtime sync golden); validated by manual smoke and the regenerated source goldens covering the template change.

**Verification:**
- `scripts/golden.sh verify` passes after `update` + diff inspection.
- `go test ./...` passes (no Go test regressions from the template change).
- Manual smoke: generate any sync-having CLI from the catalog (e.g., notion or hackernews), run `<cli> sync --json | jq -c .`, confirm non-empty NDJSON output.

---

## System-Wide Impact

- **Interaction graph:** U2 + U3 affect any code path that reads `manifest.RunID` (notably `live_dogfood.go:916` and `lock promote`). U4 affects every printed CLI's sync command and any agent script piping `sync --json` into a JSON consumer.
- **Error propagation:** U2's empty-RunID fallback emits a stderr warning, not an error — the legacy `--output`-only path remains buildable. Phase 5 acceptance still refuses to write a marker without RunID, which is intentional (the gate contract requires it).
- **State lifecycle risks:** None for U1 (pure predicate change) or U3 (text edit). U2 preserves the existing `RefreshCLIManifestFromSpec` round-trip (preserves `RunID` when re-emitting). U4 is a template-emission change, no runtime state.
- **API surface parity:** U4 changes the contract on what `sync --json` writes to stdout vs stderr — this is a generated-CLI contract change that affects every newly-printed CLI. Document in the PR; existing library CLIs are unaffected unless re-printed.
- **Integration coverage:** U2 + U3 together are the only end-to-end fix for the "Phase 5 acceptance can't write" failure mode. Splitting them would leave a partial fix; document in the PR description that they're intentionally combined.
- **Unchanged invariants:** Phase 5 full-arm logic, `RefreshCLIManifestFromSpec` field-preservation contract, the existing `humanFriendly` branch in sync.go.tmpl, sync event names/ordering, the publish-time manifest writer, all promote-time and package-time run_id behavior.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `TestsPassed` marker field actually carries only `Passed` (not `Passed+Skipped`), making the gate predicate diverge from the runner's. | U1 implementer reads `writeLiveDogfoodAcceptance` first to confirm the field's semantics; predicate aligns to the field's actual contract, not the variable name. Test scenarios cover both `tests_passed=4` (fail) and `tests_passed=5` (pass) at `matrix_size=6` to catch any miscalibration. |
| ElevenLabs convention reviewers push back on "events on stdout" without realizing NDJSON satisfies the contract. | PR description for U4 explicitly cites the ElevenLabs plan, distinguishes plain-text-event pollution from NDJSON-on-stdout, and quotes the issue #592 acceptance criteria. |
| `dogfood-verdict-matrix` golden case asserts on sync stdout/stderr text and fails after U4. | Run `scripts/golden.sh verify` immediately after the template change; if the case fails, regenerate fixtures or adjust the case's assertions to match the new contract. Document in PR. |
| The 19 listed lines in #592 are off by template drift since the issue was filed (template edits between then and now could shift line numbers). | Implementer greps the template for `Fprintf(os.Stderr` patterns producing JSON event payloads (look for `event:` field) rather than relying on line numbers blindly. |

---

## Documentation / Operational Notes

- **U4 PR description** must include: a one-paragraph rationale citing #592 and reconciling NDJSON-on-stdout with the ElevenLabs convention; the `scripts/golden.sh update` diff summary per `docs/GOLDEN.md`; explicit confirmation that human-readable progress paths are unchanged.
- **U2 PR description** must call out the contract change vs the original 2026-03-28 CLIManifest plan ("standalone generate does not write a manifest" → "standalone generate stamps run_id when --research-dir is supplied"). Cross-reference plan `docs/plans/2026-03-28-002-feat-cli-manifest-plan.md` for the prior boundary and explain why it's safely reversed.
- **No README or `docs/PIPELINE.md` updates expected.** The pipeline phases are unchanged; the gate predicate and manifest field are internal contracts. If `docs/PIPELINE.md` documents the phase5 quick gate's specific threshold (`5/6`), update it to the new general formula.
- **Lefthook hooks must pass on each PR.** `gofmt -w` runs on staged Go files; pre-push runs `golangci-lint`. Don't run `gofmt -w .` from repo root (rewrites golden fixtures).
- **No release-please bump narrative needed.** All three PRs are `fix(cli):` per AGENTS.md, which release-please will roll into the next patch release.

---

## Sources & References

- Wave 1 retro WUs: [#589](https://github.com/mvanhorn/cli-printing-press/issues/589), [#591](https://github.com/mvanhorn/cli-printing-press/issues/591) (consolidates closed [#588](https://github.com/mvanhorn/cli-printing-press/issues/588)), [#592](https://github.com/mvanhorn/cli-printing-press/issues/592)
- Parent retros: [#586](https://github.com/mvanhorn/cli-printing-press/issues/586) (Hacker News), [#587](https://github.com/mvanhorn/cli-printing-press/issues/587) (Trigger.dev)
- Related active plan: `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` (U4 introduced the runner formula the gate now diverges from)
- Related active plan: `docs/plans/2026-05-04-004-test-live-dogfood-resolve-success-coverage-plan.md` (test conventions reference)
- Prior completed plan: `docs/plans/2026-03-28-002-feat-cli-manifest-plan.md` (original CLIManifest design with the now-reversed generate-no-manifest boundary)
- Convention reference: `docs/plans/2026-03-25-feat-elevenlabs-learnings-human-friendly-ndjson-plan.md` (`--json` agent contract)
- Related issues for context: [#572](https://github.com/mvanhorn/cli-printing-press/pull/572) (sync trailing-line fix; U4 builds on it), [#535](https://github.com/mvanhorn/cli-printing-press/issues/535) and [#515](https://github.com/mvanhorn/cli-printing-press/issues/515) (related run_id observations on other code paths)
- Code: `internal/pipeline/phase5_gate.go`, `internal/pipeline/live_dogfood.go`, `internal/pipeline/climanifest.go`, `internal/pipeline/publish.go`, `internal/cli/root.go`, `internal/generator/templates/sync.go.tmpl`, `skills/printing-press/SKILL.md`
- Convention docs: `AGENTS.md`, `docs/GOLDEN.md`
