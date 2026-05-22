---
title: "scoreTypeFidelity flag-decl regex over-matches and MarkFlagRequired reward conflicts with the SKILL"
date: 2026-05-22
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: scorer
symptoms:
  - "Hand-authored CLIs lose 1pt on type_fidelity because the description word-count check averages a short kebab token"
  - "Flag names ending in non-id tokens (e.g. price-paid-cents) classify as ID flags via the 'paid' substring and fail the all-StringVar rule"
  - "SKILL-compliant CLIs without MarkFlagRequired calls cap at 4/5 on the type_fidelity dim by design"
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - internal/pipeline/scorecard.go
tags:
  - scorer
  - type-fidelity
  - regex
  - skill-conflict
---

# scoreTypeFidelity flag-decl regex over-matches and MarkFlagRequired reward conflicts with the SKILL

## Problem

`scoreTypeFidelity` in `internal/pipeline/scorecard.go` had three independent defects that systematically penalised SKILL-compliant CLIs:

1. The package-level `flagDeclRe` regex used `[^,]+` for the variable-pointer and untyped-arg captures. `[^,]+` matches newlines, so consecutive `cmd.Flags()....` calls bled into one another and the first call's description capture pulled in the next call's flag name.
2. The ID-flag classifier was `strings.Contains(name, "id")` with no word boundary. Kebab-case flag names that *contained* the substring `id` inside another word (e.g. `price-paid-cents` → matched on the `id` inside `paid`) were classified as ID flags, then graded as non-`StringVar` failures.
3. The scorer awarded `+1` when a sample of command files contained `MarkFlagRequired(...)` three or more times. The SKILL's Phase-3 build checklist explicitly forbids `MarkFlagRequired` on hand-authored novel commands because Cobra evaluates it before `RunE`, which makes the verifier's `--dry-run` probe fail with `required flag "X" not set`. A SKILL-compliant CLI cannot earn this point.

## Symptoms

- `printing-press scorecard` reports `type_fidelity` 1pt below the dim cap on CLIs whose flags use long, descriptive help strings — the average drops because the first flag's "description" capture is a short kebab-case token from the next line.
- A flag named `price-paid-cents` (or any name containing `id` mid-word) classifies as a non-`StringVar` ID flag and costs the 2-point ID-fidelity sub-check.
- SKILL-compliant CLIs that route required-flag validation through `RunE` have no honest path to the historical 5-point cap because the only +1 path the SKILL forbids was the difference.

## What Didn't Work

- Renaming flags to dodge the `id` substring (e.g. `price-paid-cents` → `price-cents`) is a per-CLI workaround and propagates into the printed CLI's public surface for no real reason.
- Adding `MarkFlagRequired` calls "for the point" breaks `verify --dry-run` and is forbidden by the SKILL.
- Re-shaping flag declarations to single-line forms is a regression in readability for the same per-CLI workaround pattern.

## Resolution

- Tighten the regex to `[^,\n]+` in both capture segments so the match cannot span newlines.
- Extract `isIDFlagName(name string) bool` and replace the substring check with kebab-case word boundaries: equal to `id`, prefix `id-`, suffix `-id`, or contains `-id-`.
- Remove the `MarkFlagRequired` reward (the `requiredRe`, the `requiredCount` accumulator, and the `>=3` block). Required-flag validation belongs inside `RunE` per the SKILL.
- Cover each fix with dedicated tests in `scorecard_tier2_test.go`: `TestIsIDFlagName` (table-driven word-boundary cases including the `price-paid-cents` regression), `TestScoreTypeFidelity_FlagDeclRegexBoundedToOneLine` (consecutive `Flags()` calls don't pollute each other's capture), and `TestScoreTypeFidelity_DoesNotRewardMarkFlagRequired` (a fixture with three `MarkFlagRequired` calls scores the same as one with none).

## Why It Works

The regex constraint is the smallest change that bounds each capture to a single Go statement; `[^,\n]+` is already the convention used elsewhere in the scorer for flag-call parsing. The kebab-case predicate matches the actual naming convention generated CLIs use, so it has no false positives on real flag names. Dropping the `MarkFlagRequired` reward removes the direct scorer-versus-SKILL conflict; the achievable maximum for `scoreTypeFidelity` is now `+2` (ID-flag check) `+1` (description-length check) `+1` (dummy-guard-absence check) `= 4`. The dim's structural allocation in the tier rollup (`tier2Max` base) still leaves a 5-slot for `type_fidelity`, so the in-function clamp now matches the achievable max (4) rather than the structural slot (5). Reconciling the tier rollup itself is a separate concern outside this fix's scope because it changes the percentage for every printed CLI and needs a coordinated golden refresh.

## Verification

- `go test ./internal/pipeline/ -run 'TestScoreTypeFidelity|TestIsIDFlagName' -count=1` — all new tests pass.
- `go test ./...` — full suite passes.
- `scripts/golden.sh verify` — passes without golden diffs; no existing fixture relied on the dropped reward path.

## Related Issues

- Closes #1720 — `scoreTypeFidelity` regex over-matches across newlines and `id` substring false positive.
- Closes #1559 — `scoreTypeFidelity` rewards `MarkFlagRequired` which the SKILL forbids.
- Related to #1561 — same pattern of scorer substring greps drifting from the SKILL's sanctioned scaffolding (different dimension).
