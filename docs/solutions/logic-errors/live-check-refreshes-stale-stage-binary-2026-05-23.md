---
title: Live-check refreshes stale staged binaries before sampling
date: 2026-05-23
category: logic-errors
module: scorecard live-check
problem_type: logic_error
component: tooling
symptoms:
  - "scorecard --live-check reports unknown command for hand-built novel commands"
  - "shipcheck samples build/stage/bin even after source changes add commands under internal/cli"
root_cause: missing_workflow_step
resolution_type: code_fix
severity: medium
tags: [scorecard, live-check, shipcheck, staged-binary, novel-commands]
---

# Live-check refreshes stale staged binaries before sampling

## Problem

`scorecard --live-check` can sample a generated CLI through `build/stage/bin/<cli>`, the binary emitted during generation validation. Phase 3 hand-built novel commands update Go source after that binary is created, so the sample probe can report `unknown command` for commands that are present and working in current source.

## Symptoms

- `scorecard --live-check` reports `0/N` sample probes passing with `unknown command "<novel-command>"`.
- Rebuilding `build/stage/bin/<cli>` manually makes the same sample probes pass without source changes.
- Directly invoking a freshly rebuilt root or `bin/` CLI works, while the staged binary remains stale.

## What Didn't Work

- Choosing the freshest binary among existing candidates helps when a root or `bin/` binary was rebuilt, but it does not help when `build/stage/bin/<cli>` is the only runnable candidate.
- Rebuilding the staged binary unconditionally fixes correctness but adds avoidable work when the staged binary, or a same-name fallback binary, is already current.

## Solution

Make live-check treat the staged binary as a refreshable artifact:

- Find the existing staged binary for the same candidate name live-check would resolve.
- Compare its mtime to Go sources under `cmd/<cli>/` and `internal/`.
- Rebuild to a temporary file and replace the staged binary only when the staged binary is older and no same-name runnable fallback is already current.
- Report the refresh action in `LiveCheckResult` so human and JSON scorecard output can show whether the staged binary was rebuilt, fresh, skipped, or failed.
- Convert a failed stale-stage refresh into a scorecard error so shipcheck does not pass after the freshness guard fails.

## Why This Works

The root cause is not command registration. It is an artifact freshness gap between generated validation output and later hand-authored source changes. The freshness check belongs in the live-check path because that is the code choosing and sampling the binary. It keeps the no-op case cheap while ensuring the sampled command surface matches the source tree when no fresher binary is available.

## Prevention

- Regression tests should include both `cmd/<cli>/` changes and `internal/` changes, because novel command implementation usually lands under `internal/cli`.
- Tests should cover no-op paths as well as rebuild paths: fresh staged binary, fresh same-name fallback binary, and stale preferred staged binary with lower-priority fallback.
- When a verification tool repairs or refreshes an artifact before checking behavior, expose that action in structured output and fail the verification leg if the refresh fails.

## Related Issues

- GitHub issue #1555
