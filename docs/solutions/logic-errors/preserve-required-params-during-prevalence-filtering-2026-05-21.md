---
title: Preserve Required Params During Prevalence Filtering
date: 2026-05-21
category: docs/solutions/logic-errors
module: internal/openapi
problem_type: logic_error
component: tooling
symptoms:
  - "A query parameter marked required: true vanished from every generated command when it appeared on every endpoint"
  - Generated CLIs could call the API but could not request the response shape the API required
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [openapi, generator, query-params, required-params, prevalence-filter]
---

# Preserve Required Params During Prevalence Filtering

## Problem

The OpenAPI parser's global query-param filter removed parameters that appeared on more than 80% of endpoints. On narrow specs, this stripped required API selectors such as YouTube-style `part` parameters from every generated command.

## Symptoms

- Generation warned that a global query param was filtered because it appeared on all endpoints.
- The generated CLI still sent valid HTTP requests, but the user had no flag for a spec-required selector that controlled the response shape.
- Optional boilerplate params and required semantic params were treated identically by the prevalence filter.

## What Didn't Work

- Treating this as a printed-CLI problem would only restore one missing flag. The bug was in the generator path that decides which OpenAPI params remain reachable for every future CLI.
- Allowlisting vendor-specific names such as `part` would fix one API family while leaving the same required-vs-prevalent conflict in other specs.

## Solution

Keep the prevalence filter, but narrow its candidate predicate:

```go
func isGlobalFilterCandidate(param spec.Param) bool {
	return !isPathSubstitutionParam(param) && !param.Required
}
```

Use that predicate both when counting prevalent params and when removing them from endpoints. This keeps optional cross-cutting noise filterable while preserving parameters whose `required: true` flag is part of the API contract.

Add regression coverage with a narrow OpenAPI fixture where `part` is required on every endpoint and `prettyPrint` is optional on every endpoint. The required param must survive with its default and endpoint-specific enum values, while the optional prevalent param must still be filtered.

## Why This Works

Prevalence is a useful signal for boilerplate params, but `required: true` is a stronger semantic signal from the spec. A parameter can be present everywhere because it is noise, or because every operation genuinely needs it. The filter must only act on the former class.

Sharing one predicate between counting and removal prevents future drift where a parameter is excluded from one phase but still removed in the other.

## Prevention

- When adding generator filters, separate "candidate for filtering" from "matches the filter threshold" so semantic exemptions live in one named predicate.
- Regression fixtures for broad filters should include both a positive survivor and a negative removal case in the same fixture.
- Avoid vendor-name allowlists when the spec already carries a portable signal such as `required`.

## Related Issues

- GitHub issue: https://github.com/mvanhorn/cli-printing-press/issues/1456
