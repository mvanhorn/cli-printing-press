---
title: Default mutator probes to dry-run; carve real-API rejection into a separate test kind
date: 2026-05-05
category: docs/solutions/design-patterns
module: live_dogfood
problem_type: design_pattern
component: testing_framework
severity: high
applies_when:
  - "Verification harness probes mutating endpoints (POST/PUT/PATCH/DELETE) without authoritative example bodies"
  - "Mutator shape is detectable cheaply (HTTP method annotation, leaf name, generator metadata)"
  - "System under test exposes a previewable mode (--dry-run, terraform plan, kubectl --dry-run=client, --no-commit, --generate-cli-skeleton)"
  - "Real calls have side effects, monetary cost, or rate-limit pressure"
  - "Harness emits a verdict consumed by a downstream gate (scoring, CI, promotion)"
related_components:
  - pipeline
  - generator
  - scorer
tags:
  - dry-run
  - mutating-endpoints
  - layered-verification
  - test-harness-design
  - error-path-real
---

# Default mutator probes to dry-run; carve real-API rejection into a separate test kind

## Context

A generic test harness that probes mutating endpoints of a CLI or API client faces an oracle problem: it can construct a syntactically plausible invocation, but it usually does not have an authoritative example body shaped like real domain data. When the harness sends that placeholder body as a real signed mutation, the API rejects it (4xx) for reasons that have nothing to do with the artifact under test — required fields missing, invalid IDs, schema mismatches. Those rejections look like product failures in the matrix.

Concrete evidence from `live_dogfood` in this repo: a full-matrix sweep on Kalshi produced ~50/56 false `happy_path` failures from this exact class — every mutating endpoint mirror (`api-keys create`, `communications create-quote`, `portfolio batch-create-orders`, `portfolio apply-subaccount-transfer`, etc.) failing because the harness sent placeholder-shaped bodies the API correctly refused. Downstream gates (scoring, promotion) trusted the verdict, so the noise weakened a load-bearing signal. The class generalizes to any harness that sweeps an API surface without ground-truth fixtures, and it scales linearly with the number of mutating endpoints in a spec.

The problem was first surfaced during the Kalshi run on 2026-05-05 (session history); the test scaffolding that made the failure pattern legible had landed in PR #583 the prior day, after which the placeholder-failure class became unmistakable. (session history)

## Guidance

Split mutator probing into two dedicated tests, gated on a cheap shape-detector:

1. **Default the "does this command work" probe to a preview mode.** Route mutating leaves through `--dry-run` (or `--plan`, `--no-commit`, `--check`, `--generate-cli-skeleton`). Preview exercises the structural signal — auth resolution, URL construction, header set, JSON envelope shape — without side effects or API rejection noise.
2. **Carve out the "API rejects bad input" probe as its own test kind.** Send the original example without `--dry-run` and assert non-zero exit. Failure here means the API silently accepted garbage, which is real signal and worth a `Fail`.

The detection gate is two-pronged:

```go
useDryRun := isMutatingLeaf(leaf) && commandSupportsDryRun(command.Help)
```

The second leg matters: hand-written novel commands sharing a mutator-shaped name (`delete`, `create`) but lacking the preview flag fall through to today's behavior rather than getting silently downgraded to a probe that cannot run. The cost of being too aggressive (injecting `--dry-run` on a command that does not support it and getting an unknown-flag error) is higher than the cost of being too conservative (leaving a hand-written novel command on its old path).

Per-shape decision table:

| Command shape | preview path (happy_path) | rejection check (error_path_real) |
|---|---|---|
| Read (`get`, `list`) | example as-is | not emitted |
| Mutator with preview flag | example + `--dry-run` | example as-is, expect != 0 |
| Mutator without preview flag | example as-is (legacy) | not emitted |
| Positional resolution skipped | skip | skip with same reason |

The non-emission rows are load-bearing: matrix size grows by exactly one entry per mutator-with-preview, not one per command, and shapes that cannot honestly run the new test are skipped rather than faked.

## Why This Matters

- **Signal quality.** When 50+ of 56 matrix entries fail for the same non-product reason, the verdict is unusable for ranking or gating. Two narrowly-scoped tests with explicit intent beat one broad test that mixes structural and semantic concerns.
- **Layered verification (auto memory [claude]).** This is the same shape as "smarter mechanical verify + agent acceptance test before ship," applied at a finer grain inside one harness: a cheap structural check (preview) plus a targeted real-call check (rejection assertion). Each leg has a single failure mode the reviewer can interpret.
- **Operational safety.** Real `DELETE` / `POST` / `PATCH` against live accounts during a generic test sweep risks data loss, cost, and rate-limit pressure. Preview-by-default removes that blast radius for the bulk of probe volume; the carved-out rejection check makes one real call per mutator (no net growth in API call volume vs the pre-fix matrix).
- **Verdict invariance under matrix expansion.** When the new kind threads through the report counters by `Status` and not by `Kind`, downstream gates need no schema change. The marker stays backward-compatible.

## When to Apply

- The harness probes endpoints without authoritative example bodies.
- Mutator shape is detectable cheaply (HTTP method annotation, leaf name like `create`/`delete`/`update`, generator metadata).
- The system under test exposes a previewable mode (`--dry-run`, `terraform plan`, `kubectl --dry-run=client`, `git --no-commit`, `aws --generate-cli-skeleton`).
- Real calls have side effects, monetary cost, or rate-limit pressure.
- The harness emits a verdict consumed by a downstream gate (scoring, CI, promotion).

If the harness *does* have ground-truth fixtures for every endpoint, this pattern is unnecessary — fixtures replace the placeholder problem entirely. The pattern earns its keep precisely when ground-truth is unavailable or impractical to maintain.

## Examples

The matrix routing decision lives in `runLiveDogfoodCommand` in `internal/pipeline/live_dogfood.go`. The pattern was first applied here in [PR #614](https://github.com/mvanhorn/cli-printing-press/pull/614), which is the canonical reference for the gate logic, the new test kind, and the helpers below:

```go
leaf := command.Path[len(command.Path)-1]
useDryRun := isMutatingLeaf(leaf) && commandSupportsDryRun(command.Help)

// happy_path: route through preview when the gate fires
runArgs := happyArgs
if useDryRun {
    runArgs = appendDryRunArg(happyArgs)  // idempotent append
}
// happyRun + jsonRun both consume runArgs

// error_path_real: original happyArgs (post-positional-resolution,
// pre-dry-run injection); expect non-zero exit
if useDryRun {
    if resolveSkipped {
        // skip with same reason as happy_path for triage parity
    } else {
        errorRealRun := runLiveDogfoodProcess(..., happyArgs, ...)
        if errorRealRun.exitCode != 0 {
            errorRealResult.Status = LiveDogfoodStatusPass
        } else {
            errorRealResult.Status = LiveDogfoodStatusFail
            errorRealResult.Reason = "expected non-zero exit for placeholder body"
        }
    }
}
```

The two helpers (`commandSupportsDryRun`, `appendDryRunArg`) are deliberate parallel siblings of the existing `commandSupportsJSON` / `appendJSONArg` pair — same one-line predicate shape, same idempotency contract. When a third sibling appears, generalize then.

Counter-example — what to avoid: testing the API's body validation through the structural-check test. Mixing those concerns made the harness's verdict ambiguous (was the CLI broken, or did the API just reject our placeholder?) and produced the linear-scaling false-failure class. Each test kind should have one failure mode the reviewer can interpret in isolation.

## Related

- `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` — sibling "mutators need separate handling" pattern, applied at the client-cache layer instead of the test-harness layer.
- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md` — prior `live_dogfood` / scorer accuracy fix; same module, different failure class.
- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md` — scorer architecture context that this pattern operates within.
- GitHub: [#595](https://github.com/mvanhorn/cli-printing-press/issues/595) (closed by [PR #614](https://github.com/mvanhorn/cli-printing-press/pull/614)) — direct parent work unit.
- GitHub: [#573](https://github.com/mvanhorn/cli-printing-press/issues/573) — prior precedent for kind-based test dispatch in `live_dogfood.go` (camelCase ID + `error_path` kind dispatch).
- GitHub: [#594](https://github.com/mvanhorn/cli-printing-press/issues/594) — Kalshi reprint retro that surfaced the placeholder-failure finding.
