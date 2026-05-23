---
title: Live dogfood treats runner-auth 401s as credential-unavailable after one retry
date: 2026-05-23
category: logic-errors
module: live dogfood
problem_type: logic_error
component: tooling
symptoms:
  - "dogfood --live reports HTTP 401 failures for auth-required CLIs"
  - "direct command invocation succeeds with the same API credential environment"
  - "missing or stale runner credentials poison the happy_path and json_fidelity matrix"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [dogfood, live-dogfood, auth, credentials, subprocess-env, retry]
---

# Live dogfood treats runner-auth 401s as credential-unavailable after one retry

## Problem

`dogfood --live` can report auth-shaped HTTP 401 responses as hard matrix failures for auth-required CLIs even after subprocess environment propagation has been ruled out. That makes the live dogfood verdict look like product breakage when the runner credentials are unavailable or a transient upstream auth response succeeds on retry.

## Symptoms

- `happy_path` and `json_fidelity` fail with output such as `HTTP 401` and `Couldn't authenticate you`.
- The same command succeeds when invoked directly with the same API credential environment.
- Clearing the API credential produces a normal upstream auth denial instead of a local missing-env error, so the matrix records a 401 fail instead of a credential-unavailable skip.

## What Didn't Work

- Assuming scoped subprocess HOME/XDG handling dropped API credentials. Targeted tests showed arbitrary `*_API_TOKEN` and `*_API_KEY` values survive `scopeSubprocessHome()` and `applyDefaultSubprocessEnv()`.
- Treating every 401 as a real endpoint failure. For live dogfood, the runner credential state is part of the test harness, and a harness-auth denial should not be mixed into API behavior failures.

## Solution

Keep the subprocess env audit as a guardrail, then handle auth-shaped 401s in the live dogfood runner:

- Retry one auth-shaped HTTP 401 inside the original subprocess timeout budget.
- Recognize common runner-auth denial text such as `Couldn't authenticate you`, `Could not authenticate you`, `not authenticated`, and `unauthorized`.
- If the retry still returns the same auth-shaped 401, classify the result as `unavailable for runner credentials` instead of a hard HTTP failure.
- When `happy_path` establishes that runner credentials are unavailable, skip `json_fidelity` with the same reason rather than executing and retrying the same denied command again.

## Why This Works

The live dogfood matrix is supposed to measure generated CLI behavior against a live API. A runner-auth denial is different from a command implementation bug: it means the harness cannot prove the command against the live service. Classifying persistent auth denial as a skip preserves the existing acceptance floor, which still fails when there is no live happy/json pass, while keeping credential problems out of the HTTP-failure bucket.

The retry covers the narrow transient case without relaxing the command timeout. If the second attempt succeeds, the command contributes a real pass. If it does not, the matrix records the credential limitation and avoids duplicate JSON probes after the happy path already proved the runner cannot authenticate.

## Prevention

- Add env propagation tests whenever subprocess HOME/XDG scoping changes so API credential env vars stay outside the scoped-home filter.
- Test live dogfood retry behavior through both the raw subprocess helper and the matrix result path.
- When a harness-level failure mode is expected for auth-required APIs, classify it distinctly from real endpoint failures and keep the acceptance gate responsible for deciding whether enough live signal remains.

## Related Issues

- GitHub issue #1573
