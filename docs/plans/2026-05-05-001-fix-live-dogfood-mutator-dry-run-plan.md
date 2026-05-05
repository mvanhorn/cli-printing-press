---
title: "fix: Default live dogfood happy_path on mutators to --dry-run"
type: fix
status: active
date: 2026-05-05
origin: https://github.com/mvanhorn/cli-printing-press/issues/595
---

# fix: Default live dogfood happy_path on mutators to --dry-run

## Summary

Eliminate the placeholder-body false-failure class in live dogfood by routing `happy_path` (and the `json_fidelity` variant that derives from it) on mutating endpoint mirrors through `--dry-run` instead of a real signed `POST`/`PUT`/`PATCH`/`DELETE`. Preserve the "API rejection on bad input" signal by adding a new `error_path_real` test kind that sends the original example without `--dry-run` and expects a 4xx (non-zero exit). All change is local to `internal/pipeline/live_dogfood.go` plus its test file.

---

## Problem Frame

Phase 5 full-matrix dogfood on Kalshi produced 56/344 failures; ~50 of those were `happy_path` or `json_fidelity` tests on write-side endpoint mirrors (`api-keys create`, `communications create-quote`, `portfolio batch-create-orders`, `portfolio apply-subaccount-transfer`, etc.) where the matrix synthesizes an empty/placeholder request body, sends a real signed mutation, and the API rejects with 400/404. The same commands work correctly when invoked with valid domain-shaped bodies. The placeholder-failure class scales linearly with the number of mutating endpoints in the spec, so every retro on a write-heavy API surfaces the same noise. The polish/promote gates and Phase 5 acceptance marker trust the dogfood verdict, so the noise weakens an already load-bearing scoring signal.

---

## Requirements

- R1. After the change, a live dogfood run on a CLI with mutating endpoints reports zero `happy_path` or `json_fidelity` failures whose root cause is "API rejected an empty/placeholder body."
- R2. The `happy_path` test on a mutating endpoint still verifies: command builds the request URL, sets auth headers, validates required-field shape, and (under `--json`) emits valid JSON via the dry-run preview envelope.
- R3. A CLI whose generated `--dry-run` is broken (template regression, build error, exit != 0 from the dry-run preview path) still surfaces a `happy_path` failure — the dry-run injection must not mask binary-level failures.
- R4. A new `error_path_real` test kind preserves the "API rejection on bad input" signal: sends the original example (no `--dry-run`) against the live API and expects non-zero exit.
- R5. `error_path` semantics for non-mutating commands (search-shape, get-shape with placeholder id, etc.) are unchanged.

---

## Scope Boundaries

- Out: discovering valid IDs from prior list responses to fully populate placeholder bodies (next P2 increment, distinct from this fix).
- Out: changing the existing `error_path` test (search-shape branch and the mutating-leaf deny-list overlay both stay as-is).
- Out: extending `agent-context` JSON to carry HTTP method per command. The leaf-name heuristic (`isMutatingLeaf`) is sufficient for the failure class targeted here; method-aware detection is a follow-up if novel commands ever need it.
- Out: any change to the generator templates, the dry-run preview envelope shape, or the persistent `--dry-run` flag wiring on root.

---

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/live_dogfood.go:584` — `runLiveDogfoodCommand` is the per-command test-matrix construction site. Today it emits four results per command: `help`, `happy_path`, `json_fidelity`, `error_path`.
- `internal/pipeline/live_dogfood.go:214` — existing `mutatingVerbs` set and `isMutatingLeaf(name)` predicate already feed the `error_path` strategy split. Reuse rather than introducing a parallel predicate.
- `internal/pipeline/live_dogfood.go:848` — `commandSupportsJSON(help)` uses the unscoped `extractFlagNames(help)` scan. The new `commandSupportsDryRun` helper should mirror this shape (no `extractFlagsSection` scoping).
- `internal/pipeline/live_dogfood.go:852` — `appendJSONArg` is idempotent and shows the canonical "append a global flag once" pattern. The dry-run injection should mirror that idempotency.
- `internal/pipeline/live_dogfood.go:660` — `liveDogfoodCommandTakesArg` and the surrounding `error_path` block: this is where the new `error_path_real` test slots in cleanly without cross-contaminating the existing `error_path` strategy.
- `internal/pipeline/dogfood.go:1779` — `extractFlagNameRe = --([a-z][-a-z0-9]*)` matches `dry-run` correctly; no regex change needed.
- `internal/generator/templates/root.go.tmpl:147` — `--dry-run` is a persistent root flag, so every generated cobra command surfaces it under `Global Flags:` in `--help`. The detection gate is reliable for generated commands.
- `internal/generator/templates/command_endpoint.go.tmpl:64,108` — `--dry-run` short-circuits the required-flag enforcement on POST/PUT/PATCH commands, so the dry-run path runs even when the example omits required body fields.
- `internal/generator/templates/client.go.tmpl:732` — `dryRun(...)` writes the request preview to stderr and returns `json.RawMessage('{"dry_run": true}')` to the caller; the endpoint command then wraps it in the action envelope.
- `internal/generator/templates/command_endpoint.go.tmpl:406-464` — for non-GET/non-HEAD methods, the action envelope is `{"action", "resource", "path", "status", "success", "dry_run", "data"}` printed via `printOutput` to stdout. This is what the `--dry-run --json` path produces; it is valid JSON and preserves the json_fidelity contract.
- `internal/pipeline/live_dogfood_test.go:586` — `writeLiveDogfoodFixture` is the shell-script fixture builder; the `widgets delete` block (lines 1203-1233) is the closest existing analogue for a mutator with help text. New fixture entries should follow this shape and add `--dry-run` to their `Global Flags:` block.

### Institutional Learnings

- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md` — phase5 acceptance marker is consumed by the polish/promote gates; reducing matrix noise without weakening signal is the explicit aim. This change tightens scorer accuracy without changing the marker schema.
- Prior plan `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` (issue #573) extended the same module with the camelCase ID handling + kind-aware `error_path` split. That plan's pattern of "add a kind-aware branch and skip-with-reason on the rest" is the template this plan follows.

### External References

None — this is a local pipeline-module change with no external API contract questions.

---

## Key Technical Decisions

- **Detection gate is `isMutatingLeaf(leaf) && commandSupportsDryRun(help)`.** Reuses the existing leaf-name heuristic (sufficient for the failure class) and adds a defensive `--dry-run` check so hand-written novel commands that share a mutating leaf name (e.g., a `delete` that doesn't expose `--dry-run`) keep today's behavior.
- **`commandSupportsDryRun` uses the unscoped `extractFlagNames(help)` scan, matching `commandSupportsJSON`.** `--dry-run` is a global persistent flag in every generated CLI; it is not the kind of cross-reference token that contaminates `Examples:` blocks the way `--query` does for search-shape detection. Section scoping would be over-engineering.
- **`error_path_real` is gated by the same predicate as the dry-run injection.** Only mutating endpoints that the matrix is now sending under `--dry-run` need the compensating real-call test; non-mutating commands keep today's single `error_path` entry. This keeps the matrix size growth bounded to "+1 entry per mutator" instead of "+1 entry per command."
- **`error_path_real` skips when positional resolution skips, mirroring `happy_path` today.** If the chain-walker can't source a real id for `update <id>`, sending the example with a literal placeholder id would just confirm the URL is wrong — no useful body-rejection signal. Skipping keeps the verdict honest.
- **`error_path_real` reuses the original (pre-`--dry-run`) `happyArgs` from positional resolution.** No separate args plumbing — it is the args we would have sent today before the injection. This minimizes surface area and ensures the new test exercises exactly the request the old `happy_path` was inadvertently exercising.
- **No change to `finalizeLiveDogfoodReport` verdict math.** It already counts results by `Status`, not by `Kind`; adding a new kind is invisible to the verdict gate. Verified by reading the switch at `live_dogfood.go:881`.
- **No change to the `Phase5GateMarker` schema.** `MatrixSize`, `TestsPassed`, `TestsSkipped`, `TestsFailed` already aggregate across all kinds.

---

## Open Questions

### Resolved During Planning

- **Q: Does `--dry-run --json` produce valid JSON on stdout for mutators?** Yes — `command_endpoint.go.tmpl:406-464` wraps the dry-run sentinel in the action envelope and prints it via `printOutput`. `json_fidelity` will pass.
- **Q: Should the gate also detect HTTP method (not just leaf name)?** No, deferred. The leaf-name heuristic catches the issue's failure class. Method-aware detection requires extending `agent-context` JSON; not warranted by this work unit.
- **Q: Should the flag-name scan be section-scoped?** No — `commandSupportsJSON` doesn't scope, and `--dry-run` doesn't have the same Examples-cross-reference contamination risk that `--query` does.

### Deferred to Implementation

- **Counter naming in fixture diagnostics.** The exact reason strings on skipped/failed `error_path_real` results are best chosen while writing the test fixture; expect to mirror today's `"missing runnable example"` / `"help check failed"` phrasing.
- **Whether to surface `error_path_real` count in the live-dogfood log line.** Today's logging walks `report.Tests`; if the per-kind tally is helpful for retro triage, decide that during implementation review of `runLiveDogfoodCommand`'s call site.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

Per-command decision matrix for `runLiveDogfoodCommand` after `help` succeeds and `happyArgs` resolve:

| Command shape                          | `isMutatingLeaf` | `--dry-run` advertised | happy_path args             | json_fidelity args                    | error_path           | error_path_real          |
|----------------------------------------|------------------|------------------------|-----------------------------|---------------------------------------|----------------------|--------------------------|
| Read (`get`, `list`, search)           | false            | true                   | example as-is               | example + `--json`                    | today's strategy     | not emitted              |
| Mutator with dry-run (`create`, etc.)  | true             | true                   | example + `--dry-run`       | example + `--dry-run --json`          | today's strategy     | example as-is, expect != 0 |
| Mutator without dry-run (novel CLI)    | true             | false                  | example as-is               | example + `--json`                    | today's strategy     | not emitted              |
| Resolve-skipped (no list companion)    | either           | either                 | skip                        | skip                                  | runs independently   | skip                     |

The block at `runLiveDogfoodCommand:584` already gates `happy_path` and `json_fidelity` on `resolveSkipped`. The change inserts the gate-and-inject step between resolution and execution, and adds a parallel `error_path_real` block that consumes the original (pre-injection) `happyArgs`. The existing `error_path` block at `live_dogfood.go:660` is untouched.

---

## Implementation Units

- U1. **Add `commandSupportsDryRun` helper and `LiveDogfoodTestErrorReal` constant**

**Goal:** Introduce the typed primitives the rest of the change uses, with no behavior change to the matrix.

**Requirements:** R4 (constant is needed for the new kind), R2 (helper gates the safe injection)

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/live_dogfood.go`
- Test: `internal/pipeline/live_dogfood_test.go`

**Approach:**
- Add `LiveDogfoodTestErrorReal LiveDogfoodTestKind = "error_path_real"` to the `LiveDogfoodTestKind` const block.
- Add `commandSupportsDryRun(help string) bool` mirroring the shape of `commandSupportsJSON`: `slices.Contains(extractFlagNames(help), "dry-run")`.
- Place the helper near `commandSupportsJSON` for adjacency.

**Patterns to follow:**
- `commandSupportsJSON` at `internal/pipeline/live_dogfood.go:848` — same one-liner predicate, unscoped flag scan.
- The `LiveDogfoodTestKind` const block at `internal/pipeline/live_dogfood.go:28` — append the new constant in declaration order.

**Test scenarios:**
- Happy path: help text containing `--dry-run` under `Global Flags:` returns `true`.
- Happy path: help text without `--dry-run` returns `false`.
- Edge case: `--dry-run` mentioned only in `Examples:` still returns `true` — this is intentional and matches `commandSupportsJSON` behavior; lock it in so a future "add scoping" refactor doesn't silently change semantics.
- Edge case: empty help string returns `false`.

**Verification:**
- `go build ./internal/pipeline/...` succeeds.
- `go test ./internal/pipeline/ -run TestCommandSupportsDryRun` passes.
- No matrix-level test changes — the constant and helper are unused at this point.

---

- U2. **Inject `--dry-run` into happy_path and json_fidelity for mutating endpoints**

**Goal:** Replace the placeholder-body real-call failure class with a dry-run preview that still exercises URL/auth/header/JSON-shape signal.

**Requirements:** R1, R2, R3

**Dependencies:** U1.

**Files:**
- Modify: `internal/pipeline/live_dogfood.go`
- Test: `internal/pipeline/live_dogfood_test.go`

**Approach:**
- In `runLiveDogfoodCommand`, after `resolveCommandPositionals` returns successfully (the non-`resolveSkipped` branch at `live_dogfood.go:630`), compute `useDryRun := isMutatingLeaf(command.Path[len(command.Path)-1]) && commandSupportsDryRun(command.Help)`.
- If `useDryRun`, derive `dryHappyArgs := appendDryRunArg(happyArgs)` (a new idempotent helper modelled on `appendJSONArg`) and use it for the `happy_path` run and as the base for `json_fidelity`.
- `json_fidelity` continues to call `appendJSONArg`; ordering is `[..., --dry-run, --json]` — both flags are persistent, order doesn't matter.
- The existing `error_path` block below this one is unchanged. The original `happyArgs` (pre-injection) must be preserved in scope for U3 to consume; do not shadow the variable.

**Patterns to follow:**
- `appendJSONArg` at `internal/pipeline/live_dogfood.go:852` — idempotent flag append, exact pattern.
- `commandSupportsJSON` gating at `live_dogfood.go:641` — same conditional shape (`if commandSupportsJSON(...)`).

**Test scenarios:**
- Covers R1. Happy path on mutator: `widgets create` fixture advertises `--dry-run` under Global Flags, fixture exits 0 only when `--dry-run` is present; matrix produces a passing `happy_path` result with args `[..., --dry-run]`.
- Covers R2. json_fidelity on mutator: fixture exits 0 with valid JSON envelope on `[..., --dry-run, --json]`; matrix produces a passing `json_fidelity` result.
- Covers R3. Error path: fixture configured to exit non-zero when `--dry-run` is present (simulating a broken dry-run preview); matrix surfaces a failed `happy_path`.
- Edge case: mutator command whose help omits `--dry-run` (hand-written novel command in the fixture) gets the today-behavior — `happy_path` runs without `--dry-run`, fixture's real-call branch executes.
- Edge case: non-mutator command (e.g., `widgets get`) with `--dry-run` advertised never gets the injection — verify args do not contain `--dry-run`.
- Integration: existing `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` still passes — the rich fixture's read commands are unaffected.

**Verification:**
- `go test ./internal/pipeline/ -run 'TestRunLiveDogfood|TestCommandSupports'` passes.
- Manual matrix audit on a fixture: a mutator with `--dry-run` advertised emits exactly one `happy_path` and one `json_fidelity` result, both with the flag in `Args`.

---

- U3. **Emit `error_path_real` test for mutators with --dry-run advertised**

**Goal:** Preserve the "API rejection on placeholder body" signal that the old `happy_path` inadvertently performed, in a dedicated test kind whose intent is explicit.

**Requirements:** R4, R5

**Dependencies:** U2 (consumes the same `useDryRun` predicate and the original `happyArgs`).

**Files:**
- Modify: `internal/pipeline/live_dogfood.go`
- Test: `internal/pipeline/live_dogfood_test.go`

**Approach:**
- In `runLiveDogfoodCommand`, when `useDryRun` is true, after the existing `error_path` block runs, append an `error_path_real` test entry. The args are the original `happyArgs` (post-positional-resolution but pre-`--dry-run` injection).
- Pass criteria: `exitCode != 0`. Fail criteria: `exitCode == 0` (with reason `"expected non-zero exit for placeholder body"`). This mirrors the existing non-search `error_path` strategy at `live_dogfood.go:704-712`.
- When `resolveSkipped` is true, emit a `LiveDogfoodTestErrorReal` `LiveDogfoodStatusSkip` entry with the same reason as `happy_path` so reviewers see the skip is consistent across the kind family.
- When `useDryRun` is false (non-mutator, or mutator without `--dry-run` advertised), do not emit `error_path_real` at all — this keeps non-mutator commands at their current matrix shape.

**Patterns to follow:**
- The non-search `error_path` strategy at `internal/pipeline/live_dogfood.go:704-712` — same pass/fail rule.
- `skippedLiveDogfoodResult` at `live_dogfood.go:824` for the resolve-skip case.

**Test scenarios:**
- Covers R4. error_path_real positive: fixture for a mutator with `--dry-run` advertised exits 1 when called with the original example (placeholder body); matrix emits a passing `error_path_real` result.
- Covers R4. error_path_real failure: fixture exits 0 when called with placeholder body (simulating an over-permissive API); matrix emits a failed `error_path_real` with the "expected non-zero" reason.
- Edge case: mutator without `--dry-run` advertised emits no `error_path_real` entry — assert kind is absent from `report.Tests` for that command.
- Edge case: non-mutator command (`widgets get`) emits no `error_path_real` entry.
- Edge case: mutator with positional resolution skipped emits a skipped `error_path_real` whose `Reason` matches the `happy_path` skip reason.
- Verdict invariance: a fixture run that previously passed continues to pass — the new entry adds matrix size but contributes a `LiveDogfoodStatusPass`. Lock this in via an updated `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` assertion on `MatrixSize`.

**Verification:**
- `go test ./internal/pipeline/...` passes (full package, since the verdict-counting paths are exercised by multiple tests).
- Phase 5 acceptance marker on a passing fixture run still records `Status: pass` and the bumped `MatrixSize` matches `Passed + Failed` (no `Skipped` leakage).

---

- U4. **Run golden harness and full test suite as a no-impact sanity check**

**Goal:** Confirm this pipeline change does not perturb generator output or any golden artifact.

**Requirements:** All — this is the integration-level confidence pass.

**Dependencies:** U3.

**Files:**
- No file modifications expected. If a golden does diff, that is signal — investigate the cause before regenerating.

**Approach:**
- Run `scripts/golden.sh verify`. The change is in `internal/pipeline/live_dogfood.go`, not under `internal/generator/templates/`, so generator output should be byte-identical. Expectation: clean pass.
- Run `go test ./...` from the repo root.
- Run `go vet ./...` and `go fmt ./...`.

**Test expectation:** none -- this unit is verification of existing tests, not new behavior.

**Verification:**
- `scripts/golden.sh verify` exits 0 with no diffs.
- `go test ./...` passes.
- `golangci-lint run ./...` passes (pre-push hook mirrors CI; running it locally is a fast feedback loop).

---

## System-Wide Impact

- **Interaction graph:** Change is local to the live dogfood matrix in `internal/pipeline/`. The polish/promote gates and Phase 5 acceptance marker consume the verdict, not the per-kind breakdown — verdict semantics are unchanged.
- **Error propagation:** `error_path_real` failures (API quietly accepts a placeholder body) propagate to `report.Failed` exactly like any other failure, flipping verdict to `FAIL`. This is the intended new signal.
- **State lifecycle risks:** Mutators run today as real signed mutations. Under the new flow, `happy_path` runs as a dry-run preview — _safer_, since it removes the chance of an accidental write against a live account. `error_path_real` retains one real call per mutator (same as today's count), so the call volume against the live API does not grow.
- **API surface parity:** `LiveDogfoodTestKind` is a public type in the `pipeline` package. Adding a constant is additive and binary-compatible. No callers branch on the closed set of kinds.
- **Integration coverage:** `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` exercises `MatrixSize`, `TestsPassed`, `TestsSkipped`, `TestsFailed` end-to-end — that test must pass with the new kind in play.
- **Unchanged invariants:** `LiveDogfoodReport` JSON shape, `Phase5GateMarker` JSON shape, `LiveDogfoodTestResult` field set, the existing four test kinds and their semantics, the `error_path` strategy split (search vs non-search vs mutator-deny-list) — all explicitly preserved.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `error_path_real` introduces new failures on APIs that quietly accept placeholder bodies (return 2xx). | This is intended signal — flag it in the retro path so the example or example-emission gets tightened. The matrix grows by 1 entry per mutator; a single new failure does not flip a previously-clean run as long as the dry-run happy_path passes. Document in `Documentation / Operational Notes`. |
| Hand-written novel commands using mutating leaf names but no `--dry-run` flag get the today-behavior, reintroducing placeholder failures. | The defensive `commandSupportsDryRun` gate keeps these on the old strategy. Acceptable — novel commands are a small minority and authors can opt in by exposing `--dry-run` (the persistent root flag is already inherited automatically for cobra commands defined via `rootCmd.AddCommand`). |
| `--dry-run --json` envelope shape regression breaks json_fidelity silently. | Existing test on the dry-run preview path lives at the generator template level; this plan's tests pin the matrix-level expectation independently. A regression in the envelope template would surface in U2's json_fidelity test before reaching downstream CLIs. |
| Counter or skip-reason drift makes Phase 5 acceptance marker ambiguous. | `finalizeLiveDogfoodReport` counts by `Status`, not `Kind`; the new kind threads through unchanged. U3's verdict-invariance test locks this in. |

---

## Documentation / Operational Notes

- No `SKILL.md` change is required — the live dogfood matrix is internal pipeline behavior, not an agent-facing surface. The skill that runs Phase 5 (`printing-press-polish`, `printing-press`) consumes the verdict, not the kind enumeration.
- Mention in the next retro that mutator placeholder-body failures should now be triaged as `error_path_real` (intended signal) vs `happy_path` (dry-run regression) — the kind name disambiguates the root cause for the agent doing the triage.
- No release-note entry needed; the `LiveDogfoodReport` JSON is internal to the pipeline. If a downstream consumer ever lands that pretty-prints the matrix, they will see the new kind and should treat it as additive.

---

## Sources & References

- **Origin issue:** [#595 — [P2] WU-1: Live dogfood happy_path on mutators defaults to --dry-run](https://github.com/mvanhorn/cli-printing-press/issues/595)
- **Parent retro:** [#594 — Retro: Kalshi (reprint)](https://github.com/mvanhorn/cli-printing-press/issues/594)
- **Related issue (extends):** [#573 — [P1] WU-2: Live dogfood matrix accuracy — camelCase ID example values + error_path command-kind dispatch](https://github.com/mvanhorn/cli-printing-press/issues/573)
- **Related plan (same module pattern):** `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md`
- **Target file:** `internal/pipeline/live_dogfood.go`
- **Test file:** `internal/pipeline/live_dogfood_test.go`
- **Dry-run preview wiring:** `internal/generator/templates/client.go.tmpl:732`, `internal/generator/templates/command_endpoint.go.tmpl:406-464`
- **Persistent dry-run flag:** `internal/generator/templates/root.go.tmpl:147`
