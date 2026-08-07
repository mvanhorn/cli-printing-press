---
title: Live dogfood required-param 4xx responses need narrow fixture skips
date: 2026-05-24
category: logic-errors
module: live dogfood
problem_type: logic_error
component: tooling
symptoms:
  - "dogfood --live reports http_4xx failures for endpoints that require input the matrix cannot supply"
  - "scoped APIs cannot produce a passing live marker even when reachable scoped commands work manually"
  - "generic 4xx validation errors risk being hidden if skip heuristics are too broad"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [dogfood, live-dogfood, scorer, required-params, fixtures, http-4xx]
---

# Live dogfood required-param 4xx responses need narrow fixture skips

## Problem

`dogfood --live` can mark healthy scoped APIs as failed when the live matrix invokes a command without an API-required parameter that only a real fixture or operator context can provide. The inverse risk is just as important: a broad 4xx substring heuristic can hide real request-shaping bugs by converting ordinary validation errors into skips.

## Symptoms

- `happy_path` or `json_fidelity` fails with output such as `HTTP 400: {"error":"Please provide email"}`.
- The same API command works when the required scope or fixture value is supplied manually.
- The live dogfood failure summary groups the result as `http_4xx`, blocking acceptance even though the runner never had a usable value to send.

## What Didn't Work

- Treating all 4xx responses as command failures. That misclassifies harness-unsuppliable fixture gaps as generated CLI defects.
- Treating broad phrases like `missing field`, `required field`, or `is required` as fixture gaps. Those phrases also appear in real upstream validation failures and would produce false PASS markers.
- Fixing a single printed CLI. The issue is in the Printing Press scorer: every printed CLI using live dogfood inherits the same matrix behavior.

## Solution

Classify only narrow required-parameter 400/422 responses as `blocked-fixture: required API parameter` skips:

- Require a non-zero subprocess exit.
- Require the live response text to look like HTTP 400 or HTTP 422.
- Match only explicit parameter wording such as `missing parameter`, `missing param`, `required parameter`, or the concrete `Please provide email` shape seen in the Buzz run.
- Skip the paired JSON-fidelity probe when happy-path already established the same blocked fixture reason.
- Keep ordinary 4xx responses, auth requirements, subscription requirements, missing fields, and invalid filters as failures.

Regression coverage should include both positive and negative matrix fixtures. The positive fixture proves the blocked happy path skips without invoking `--json`, and that JSON-fidelity skips when only the JSON probe hits the missing-parameter body. The negative fixture proves an ordinary `HTTP 400: invalid filter` still fails the live dogfood verdict.

## Why This Works

The live dogfood runner is a scorer, not an API-specific fixture generator. When the API says a required parameter is missing and the matrix had no value to supply, the correct result is a skipped blocked fixture, not a failed command. But the skip must stay narrow because false skips are worse than false failures: they can promote a CLI while hiding broken examples or request construction.

The fix follows the existing live dogfood classification pattern for runner credentials: separate harness limitations from product failures, preserve specific skip reasons in the report, and keep the acceptance gate responsible for deciding whether enough real live signal remains.

## Prevention

- Pair every scorer skip heuristic with negative fixtures for nearby real failures.
- Avoid broad substring classifiers when the skipped category has asymmetric risk.
- Prefer explicit command annotations when an API-specific behavior is known ahead of time; use response heuristics only for portable, narrowly identifiable harness limitations.
- Keep fixture tests honest by making skipped subprocess paths fail loudly if they are accidentally invoked.

## Related Issues

- GitHub issue #1859

## Related Docs

- `docs/solutions/logic-errors/live-dogfood-auth-401-runner-credentials-2026-05-23.md`
- `docs/solutions/logic-errors/dogfood-soft-failure-error-path-opt-out-2026-05-22.md`
- `docs/solutions/logic-errors/scoretypefidelity-flag-decl-regex-and-mark-required-2026-05-22.md`
