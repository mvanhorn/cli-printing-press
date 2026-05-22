---
title: "Ship plan agent-readiness gate"
date: 2026-05-18
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
symptoms:
  - "PhaseShip could emit SHIP when the scorecard passed even though agent-readiness reported Degrade"
  - "Agent-readiness findings appeared in pipeline artifacts but did not affect the generated ship decision"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - pipeline
  - agent-readiness
  - ship-gate
  - artifact-parsing
  - scorecard
---

# Ship plan agent-readiness gate

## Problem

The managed pipeline's ship plan treated the scorecard threshold as the only load-bearing ship gate. A run could produce `pipeline/agent-readiness.md` with a non-Pass verdict and still generate a ship plan that said `SHIP` when the scorecard cleared 65%.

## Symptoms

- PhaseShip emitted `SHIP` for a high-scoring CLI even when agent-readiness reported `Degrade`.
- Readiness Blocker and Friction findings were visible in artifacts, but the ship decision ignored them.
- Missing readiness evidence was effectively neutral instead of blocking ship.

## What Didn't Work

- Reading only the scorecard in `GenerateNextPlan` kept quality and readiness as independent reports, but only quality became a gate.
- Loose markdown parsing would have created false positives from prose mentioning "non-Pass" or "Degrade" without declaring a verdict.
- Falling back to `proofs/agent-readiness.md` would have let stale or non-canonical proof data satisfy the gate when the canonical `pipeline/agent-readiness.md` was missing.

## Solution

Make PhaseShip compose quality and agent-readiness as independent gates:

- Load readiness only for `PhaseShip`, and only from `state.PipelineDir()/agent-readiness.md`.
- Parse a narrow verdict contract: `Phase verdict: Pass|Warn|Degrade` or `Verdict: Pass|Warn|Degrade`.
- Treat missing, malformed, `Warn`, and `Degrade` readiness reports as `HOLD`.
- Keep scorecard and readiness independent in the rendered plan so operators can see which axis failed.
- Update the agent-readiness seed and `docs/PIPELINE.md` to require the exact artifact the ship gate consumes.

## Why This Works

The root cause was not the readiness reviewer itself. The failure was at the artifact contract boundary: the ship plan did not consume the readiness phase's canonical output. Loading the canonical artifact and parsing only its explicit verdict line makes readiness load-bearing without turning plan generation into a hard error path.

Keeping missing or malformed readiness as a rendered `HOLD` also preserves the reusable planning contract. `GenerateNextPlan` can still produce a useful handoff document, but the document cannot silently recommend shipping without the required evidence.

## Prevention

- When a later phase gates on an earlier phase's artifact, update the producer seed, consumer parser, docs, and regression tests in the same change.
- Do not treat absent evidence as neutral in ship gates. Missing scorecard or readiness data should render an explicit `HOLD`.
- Add false-positive parser tests for prose that mentions verdict words without declaring the verdict.
- Add stale-location tests when a canonical runstate path has tempting fallback locations.

## Related Issues

- GitHub issue #1354
- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md`
- `docs/solutions/logic-errors/scorer-dogfood-composed-header-auth-and-example-continuations-2026-05-05.md`
- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
