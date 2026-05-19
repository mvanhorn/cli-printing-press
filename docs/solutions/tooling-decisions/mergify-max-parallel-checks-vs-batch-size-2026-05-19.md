---
title: "Mergify max_parallel_checks is free; only batch_size is paywalled"
date: 2026-05-19
category: tooling-decisions
module: ci/mergify
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Tuning Mergify merge queue throughput on the OSS / free plan"
  - "Greptile or other human-review tools silent-skip mergify/merge-queue/* bot branches and gate the queue"
  - "Deciding whether to raise batch_size or max_parallel_checks to speed up a slow queue"
related_components:
  - development_workflow
tags:
  - mergify
  - merge-queue
  - max-parallel-checks
  - batch-size
  - greptile
  - oss-plan
  - queue-conditions
  - merge-conditions
---

# Mergify max_parallel_checks is free; only batch_size is paywalled

## Context

`mvanhorn/cli-printing-press` runs on Mergify's free OSS plan. The merge queue had been pinned to the most conservative possible config — `batch_size: 1`, `max_parallel_checks: 1`, a single `queue_conditions` block — under the belief that both throughput knobs were paywalled features. They aren't. Only `batch_size > 1` is paid. `max_parallel_checks > 1` is free, and not using it leaves a real speedup on the floor.

The wrong-turn history:

- **PR #1306** raised `batch_size: 1 → 3` plus `speculative_checks: 2` (the older Mergify name for `max_parallel_checks`). The queue immediately dequeued PRs with `Cannot use Merge Queue batch. The 'Merge Queue Batch' feature requires a higher subscription tier`.
- **PR #1321** reverted both knobs to 1 and moved Greptile into Mergify conditions to restore stable merging.
- **PR #1336** narrowed the config to a single-block `queue_conditions` setup, locking the queue into in-place mode.
- The institutional takeaway became "both knobs are paywalled on OSS." Filed in private memory, propagated into other repos.

A second concern reinforced the same conservative config: Greptile silent-skips bot-owned branches (`mergify/merge-queue/*` draft PRs). With only `queue_conditions` (no `merge_conditions`), the team was implicitly running in-place mode and never observed the draft-PR + Greptile interaction. The fear was that adding draft PRs would silently bypass Greptile.

Mergify support clarified on 2026-05-19:

- `batch_size > 1` is the paid "Merge Queue Batch" feature, which groups multiple PRs into one queue position.
- `max_parallel_checks > 1` is free. It lets Mergify check multiple PRs in parallel via individual `mergify/merge-queue/*` draft PRs — one PR per draft, no batching.
- Greptile not posting on bot branches is expected. The human-review gate belongs on the **source** PR before queue entry, not on the speculative draft.

PR #1668 codified the corrected configuration.

## Guidance

Treat `batch_size` and `max_parallel_checks` as two independent decisions:

1. **`batch_size`**: keep at `1` on the OSS plan. Any value > 1 trips the paid-tier dequeue with `Cannot use Merge Queue batch`.
2. **`max_parallel_checks`**: free to raise above 1. Pick a value that matches realistic queue depth; 3 is a reasonable starting point for a small-team repo. Mergify's default has been 5 since June 2025, so a missing setting silently inherits that default.

Adding a `merge_conditions` block (distinct from `queue_conditions`) is what flips Mergify into **draft_pr** mode, which is what makes parallel speculation actually parallelize across PRs. With only `queue_conditions`, the queue runs in-place and `max_parallel_checks > 1` has nothing to parallelize.

When you adopt draft_pr mode, structure gating across **three blocks**, each enforcing the right conditions against the right PR:

- **`queue_conditions`** — gates entry on the *source* PR. Put human-review signals here: review-thread resolution, code-review tools (Greptile), and all CI checks.
- **`merge_conditions`** — gates merge after the *draft* PR's CI passes. Put *only* the CI checks that run on bot branches here. Do **not** put Greptile or review-thread counts here — Greptile silent-skips bot branches, and draft PRs have no review threads.
- **`merge_protections`** — re-enforces source-PR conditions before the final GitHub merge. Belt-and-suspenders: ensures speculation can't bypass the human-review gate.

## Why This Matters

The corrected config gives free throughput we were leaving on the floor. With `max_parallel_checks: 1`, queued PRs serialize behind each other's full CI run. With `max_parallel_checks: 3`, three PRs speculate in parallel via independent draft branches; the first one to land merges immediately, and the others either land in sequence (if still applicable to the new `main`) or re-queue with one less position ahead.

Two non-obvious failure modes to avoid:

- **Raising `max_parallel_checks` without adding `merge_conditions`** — nothing breaks, but nothing speeds up either. Mergify stays in in-place mode and parallel checks have no draft PRs to run against. The change appears to take effect in config review and quietly does nothing.
- **Putting Greptile or `#review-threads-unresolved = 0` in `merge_conditions`** — the queue stalls forever waiting for signals that will never appear on bot branches. The dequeue message is unhelpful; the symptom looks like Mergify is broken.

## When to Apply

Apply this guidance when **all** of the following hold:

- The repo is on Mergify's free OSS plan (or any plan where batch is paywalled but parallel-check isn't — verify against current Mergify pricing).
- Queue depth regularly exceeds 1 (i.e., PRs wait behind each other's CI).
- CI runs are long enough that serialization is a real cost.
- A code-review tool (Greptile, Coderabbit, similar) silent-skips bot-owned branches.

Skip this if queue depth is reliably 0–1 PRs (no parallelism to extract) or if your code-review tool *does* run on bot branches (the `merge_conditions` split is less critical, though still cleaner).

## Examples

**Before** — in-place mode, max parallelism of 1:

```yaml
queue_rules:
  - name: default
    queue_conditions:
      - base = main
      - "#review-threads-unresolved = 0"
      - check-success = build-and-test
      - check-success = generated-test
      - or:
        - check-success = Greptile Review
        - check-neutral = Greptile Review
        - check-skipped = Greptile Review
    batch_size: 1
    merge_method: squash

merge_queue:
  max_parallel_checks: 1
```

**After** — draft_pr mode, parallel speculation, three-tier gating:

```yaml
queue_rules:
  - name: default
    # Source PR gating: human-review signals + all CI checks.
    queue_conditions:
      - base = main
      - "#review-threads-unresolved = 0"
      - check-success = build-and-test
      - check-success = generated-test
      - or:
        - check-success = Greptile Review
        - check-neutral = Greptile Review
        - check-skipped = Greptile Review

    # Draft PR gating: only CI checks that run on bot branches.
    # No Greptile (silent-skips bots), no review-thread count (drafts have none).
    merge_conditions:
      - check-success = build-and-test
      - check-success = generated-test

    batch_size: 1            # paid above this; keep at 1
    merge_method: squash
    update_method: merge

merge_queue:
  max_parallel_checks: 3     # free; raised from 1

# Re-enforce source-PR gates before the actual GitHub merge.
merge_protections:
  - name: require-ready-label-and-ci
    success_conditions:
      - or:
        - label = ready-to-merge
        # release-please carve-out elided
      - "#review-threads-unresolved = 0"
      - check-success = build-and-test
      - check-success = generated-test
      - or:
        - check-success = Greptile Review
        - check-neutral = Greptile Review
        - check-skipped = Greptile Review
```

## Three rules to remember

1. **`batch_size > 1` requires paid Mergify; `max_parallel_checks > 1` does not.** Different features, only the former is paywalled on OSS.
2. **`merge_conditions` is the switch that turns on draft_pr mode.** Without it, `max_parallel_checks > 1` has nothing to parallelize.
3. **Greptile silent-skips bot branches by design.** Put Greptile gates in `queue_conditions` and `merge_protections` (both evaluate the source PR), never in `merge_conditions` (evaluates the draft branch).

## Related

- PR #1306 — the regression: raised `batch_size` and `speculative_checks` together, hit the paid-tier dequeue.
- PR #1321 — the over-correction: reverted both knobs, calcified the "both paywalled" belief.
- PR #1336 — narrowed to single-block in-place mode.
- PR #1668 — the fix: split gating into three blocks, raised `max_parallel_checks` to 3, kept `batch_size` at 1.
