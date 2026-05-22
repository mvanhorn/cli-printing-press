---
title: "Dogfood soft-failure endpoints need explicit error-path opt-outs"
date: 2026-05-22
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
related_components:
  - testing_framework
  - documentation
symptoms:
  - "Live dogfood error_path failed commands whose upstream API returned HTTP 200 plus an empty success envelope for bogus IDs."
  - "Printed CLI maintainers had to add API-specific empty-result heuristics only to make dogfood expect a non-zero exit."
  - "The dogfood report could not distinguish skipped-on-purpose commands from ordinary no-argument skips for this soft-failure class."
root_cause: missing_tooling
resolution_type: code_fix
severity: medium
tags:
  - dogfood
  - error-path
  - cobra-annotations
  - soft-failure
  - scorer
  - live-testing
---

# Dogfood soft-failure endpoints need explicit error-path opt-outs

## Problem

The live dogfood `error_path` probe sends `__printing_press_invalid__` as the first positional argument and expects a non-zero exit. That works for strict lookup APIs, but it produces false failures for APIs that treat unknown IDs as a successful empty result.

PCGS image lookups exposed the gap: unknown certificate IDs returned a successful envelope with no images, so the generated CLI correctly exited 0 while dogfood marked the command as failed.

## Symptoms

- Dogfood reported `error_path` failures with reason `expected non-zero exit for invalid argument`.
- Printed CLI maintainers were pushed toward local response-shape heuristics such as "empty images plus false image flags means not found."
- Those heuristics coupled CLI exit semantics to dogfood's probe rather than to the upstream API's documented behavior.

## What Didn't Work

1. Adding per-API "empty means not found" logic to printed CLIs. That may satisfy dogfood, but it invents semantics when the upstream API intentionally returns a success envelope.
2. Broadly classifying empty results as errors in dogfood. Search and list-style APIs legitimately return empty result sets under exit 0, and dogfood already has separate search-shaped handling.
3. Skipping all command checks for these APIs. Help, happy-path, and JSON-fidelity still provide useful coverage; only the synthetic bad-ID probe is incompatible.

## Solution

Add an explicit Cobra annotation that lets command authors opt out of the incompatible probe:

```go
cmd.Annotations["pp:no-error-path-probe"] = "true"
```

The live dogfood runner checks that annotation after help, happy-path, and JSON-fidelity have run. When it is present, dogfood emits an `error_path` result with:

```json
{
  "status": "skip",
  "reason": "no-error-path-probe annotation"
}
```

The runner does not invoke the binary with `__printing_press_invalid__` for that command, so the report shape makes the intentional skip distinct from a pass that exercised the bad argument. The regression fixture also proves help, happy-path, and JSON-fidelity still pass for the annotated command.

## Why This Works

The root cause was not a broken generated CLI. The bug was dogfood treating one synthetic invalid-argument expectation as universal across APIs. Some APIs expose "no data for that ID" as a successful empty payload, and a wrapper CLI should not reinterpret that as an error unless the API contract says so.

An explicit annotation is narrow and auditable:

- It requires a command author to mark the exceptional case deliberately.
- It preserves the rest of live dogfood's mechanical coverage.
- It keeps the skip reason stable in JSON reports.
- It avoids adding fragile response-shape inference to either dogfood or individual printed CLIs.

## Prevention

1. When a dogfood probe assumes a cross-API contract, check whether the upstream API actually exposes that contract or whether the probe needs an explicit opt-out.
2. Prefer command annotations for known exceptional behavior over response-shape heuristics in printed CLIs.
3. Keep skip reasons stable and specific so reports distinguish intentional coverage gaps from environmental skips.
4. Regression-test both sides of a probe opt-out: the skipped probe must not invoke the binary, and unrelated probes must still run.
5. Update Printing Press skill guidance when a machine-owned verifier gains a new supported annotation, so future maintainers do not carry manual workarounds.

## Related Issues

- `#1548` -- dogfood error_path probe needs per-command opt-out for soft-failure APIs.

## Related Docs

- `docs/solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md` -- precedent for narrow dogfood skips with explicit reasons.
- `docs/solutions/logic-errors/scorer-dogfood-composed-header-auth-and-example-continuations-2026-05-05.md` -- scorer and dogfood checks should follow runtime behavior instead of forcing per-CLI workarounds.
- `docs/solutions/design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md` -- adjacent guidance for preferring explicit mechanisms when inference has asymmetric failure costs.
