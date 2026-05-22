---
title: "OpenAPI security scheme selection by operation usage"
date: 2026-05-22
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: authentication
symptoms:
  - "Multi-scheme OpenAPI specs could pick a narrow security scheme as primary even when most operations used a different scheme"
  - "Generated config and manifest auth_env_vars could point at a one-off endpoint credential instead of the API-wide credential"
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - testing_framework
tags:
  - openapi-parser
  - auth-selection
  - security-schemes
  - operation-security
---

# OpenAPI security scheme selection by operation usage

## Problem

OpenAPI specs can declare several `components.securitySchemes`, but not every scheme represents the API-wide credential. If the parser picks by declaration order or type priority alone, a scheme used by one operation can become the generated CLI's primary auth.

## Symptoms

- A Pinterest-like spec declared both OAuth2 and a narrow bearer token, but the narrow bearer scheme won primary selection.
- Generated `Auth.EnvVars` and downstream manifests followed the wrong selected scheme.
- Manual generated-CLI patches could fix `config.go`, but manifest metadata stayed wrong because the parser remained the source of truth.

## What Didn't Work

- **Components-only ranking.** `components.securitySchemes` describes available schemes, not which one the operation surface primarily uses.
- **Type priority alone.** Keeping bearer above OAuth2 is still useful for equal alternatives, but it should not let a one-off bearer endpoint outrank a broadly referenced OAuth2 scheme.
- **Filtering only by root security.** Operation-level `security` overrides root security, so candidate selection must consider effective operation security rather than root declarations alone.

## Solution

Count effective security requirements across operations before applying the existing scheme type priority. For each operation, use operation-level `security` when present, otherwise inherit root `security`. Count each scheme at most once per operation, then select the highest reference count; equal counts keep the existing `schemePriorityScore` tie-break.

`AuthPreference` remains an explicit override: it resolves directly against `components.securitySchemes` before usage-based filtering.

## Why This Works

The selected auth scheme now reflects what the generated CLI will use for the broad operation surface, while retaining established behavior for equal alternatives, single-scheme specs, no-operation fallback cases, and explicit caller preferences.

## Prevention

- Add parser fixtures where a high-priority scheme is used narrowly and a lower-priority scheme is used broadly.
- Cover root-inherited operations mixed with operation-level overrides, since both feed the same effective security histogram.
- Keep `AuthPreference` tests independent from the default selector so explicit catalog or caller overrides cannot be broken by ranking changes.

## Related Issues

- [Issue #1532](https://github.com/mvanhorn/cli-printing-press/issues/1532)
- [Inline Authorization params: conservative bearer inference](inline-authorization-param-bearer-inference-2026-05-05.md)
