---
title: "Novel feature command-depth drift between docs and Cobra wiring"
date: 2026-05-23
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
related_components:
  - documentation
  - testing_framework
symptoms:
  - "README Unique Features and root help Highlights advertised a novel command at one depth while Cobra registered it at another."
  - "Dogfood counted the feature as built through leaf fallback and did not warn about the advertised path drift."
  - "Scorecard output had no corresponding warning when research metadata was available."
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - dogfood
  - scorecard
  - novel-features
  - cobra-tree
  - readme-drift
---

# Novel feature command-depth drift between docs and Cobra wiring

## Problem

Novel feature advertising comes from `research.json`, while the final command tree comes from hand-authored Cobra wiring. A feature can be advertised as `grab` in README and root help, then implemented as `assets grab`, and the old dogfood check still counted it as present because the leaf name existed somewhere in the tree.

## Symptoms

- Generated README and root help can claim a copy-paste command shape that is not registered at that path.
- `novel_features_built` can preserve the feature as shipped even though the advertised path is stale.
- Scorecard and dogfood do not tell the agent whether the fix is to promote the command or update the research example.

## What Didn't Work

- Leaf-only matching was useful as a compatibility fallback for partially reconstructed command trees, but it erased the difference between `grab` and `assets grab`.
- Checking only the `command` field missed the user-facing `example` field, which is the exact command shown in docs.
- Static tree reconstruction that only followed `new*Cmd` factories missed hand-authored helpers such as `assetsGrabSubcmd(flags)`.

## Solution

Keep the existing "is the feature present" matching behavior, then add a second depth-drift diagnostic:

- Parse the advertised path from `example` when it begins with a printed CLI binary, falling back to `command` otherwise.
- Prefer full command-path matches. When only a leaf match explains the feature, compare the advertised path to all known registered paths with the same leaf.
- Emit `novel_features_check.depth_mismatches` in dogfood JSON and include the same detail in dogfood human output and issue collection.
- Add `novel_feature_depth_mismatches` and a gap-report entry to scorecard when a research directory is available.
- Broaden static command-tree reconstruction to follow arbitrary Cobra factory functions and variable-assigned `AddCommand` calls, not just direct `new*Cmd` calls.

## Why This Works

The feature-presence check and the path-correctness check answer different questions. Leaf fallback can remain permissive so dogfood does not falsely claim a feature is missing when static tree reconstruction is incomplete. The new depth diagnostic only fires when there is a known registered path whose leaf matches the advertised command but whose full path differs.

The broader command-tree reconstruction reduces false depth warnings by recovering common hand-authored Cobra wiring before the checker falls back to leaf evidence. Scorecard reuses the same mismatch logic so agents see the drift whether they run dogfood or inspect scorecard output.

## Prevention

- Treat fallback matchers as lossy: when they preserve compatibility, add a separate diagnostic for what they hide.
- When generated docs render runnable examples, validate the rendered example path, not only the planner-facing command field.
- Test both directions of path drift: advertised root vs nested registration, and advertised nested path vs root registration.
- Include issue-list or gap-report tests for warnings that must guide the remediation path.

## Related Issues

- GitHub issue: #1596
- Related: `docs/solutions/logic-errors/scorer-dogfood-composed-header-auth-and-example-continuations-2026-05-05.md`
- Related: `docs/solutions/logic-errors/reachable-scorer-surfaces-non-rest-clis-2026-05-21.md`
