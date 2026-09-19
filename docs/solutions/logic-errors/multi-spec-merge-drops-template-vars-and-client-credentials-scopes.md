---
title: Multi-spec merge dropped endpoint template vars and client-credentials scopes
date: 2026-09-16
category: logic-errors
module: multi-spec OpenAPI generation
problem_type: logic_error
component: authentication
symptoms:
  - "Multi-spec print of a tenant-scoped API had no root --tenant flag; {tenant} became a per-command positional on every command"
  - "Generated resolveClientCredentialsScope literal held only the first spec's scopes, so the token mint requested one module's scopes"
  - "Single-spec dry runs of the same modules looked correct, so the defect only surfaced on the merged print"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags: [multi-spec, template-vars, x-tenant-env-var, client-credentials, oauth, scope-union, generator]
---

# Multi-spec merge dropped endpoint template vars and client-credentials scopes

## Problem
`mergeSpecsWithOptions` in `internal/cli/root.go` builds the merged `spec.APISpec` field by field. Two families of fields never made it across: the endpoint template-variable bindings (`EndpointTemplateVars`, `EndpointTemplateEnvOverrides`, `EndpointTemplateVarDefaults`, `EndpointPathParamDefaults`) and, for OAuth2 `client_credentials`, the per-spec scopes. A 25-module ServiceTitan print (every path `/tenant/{tenant}/...`, root `x-tenant-env-var`, one shared token URL) lost its `--tenant` flag and minted tokens scoped to the accounting module only.

## Symptoms
- The merged dry run reported the expected resource and endpoint counts and printed no warning; nothing in `--dry-run` output shows template bindings, so the loss was invisible until a real generate.
- The emitted root command had no `--tenant` persistent flag and `config.go` never read the tenant env var.
- `internal/cli/auth.go` in the generated tree carried a scope literal from one module; every other module's calls would mint an under-scoped token.

## What Didn't Work
- Reading the merge for the tenant case alone. `InferEndpointTemplateVarsFromBaseURLs` looked like a recovery path, but it deliberately ignores endpoint-path placeholders and only handles base-URL variables.
- Carrying `GlobalPathTemplateVars` forward from the source specs. `PromoteGlobalPathTemplateVars` keeps pre-seeded globals without re-checking the 80% coverage threshold, so a placeholder global in one small spec would force a root flag on every merged command. Review caught this before merge.
- Expecting `compatibleOAuthScopeAuth` to union client-credentials scopes. It returned false whenever the selected auth had no authorization URL, with a carve-out only for the Google service-account subtype; two-legged grants never carry an authorization URL, so the union never ran.

## Solution
Two changes in `internal/cli/root.go` (upstream PRs #4727 and #4729, unmerged as of this writing):

1. **#4727:** `applyMultiSpecTemplateVars` runs at the end of the merge. It unions `EndpointTemplateVars` (first declaration wins), `EndpointTemplateEnvOverrides` (first wins; a conflicting later name warns on stderr naming both specs), and the two default maps through a shared `mergeFirstWins` helper, then re-runs `DropCollidingEndpointTemplateEnvOverrides` because the merged CLI has its own name and auth model. It sets `GlobalPathTemplateVars` to nil so the generator re-derives globals from the merged endpoints.
2. **#4729:** `compatibleOAuthScopeAuth` checks type and effective grant first, then treats `client_credentials` as compatible when normalized token URLs and refresh-token mechanisms match; the authorization-URL requirement still governs every other grant.

Proof on this PR (#4727) lives in `internal/cli/generate_test.go`: merge tests for each template-var union, conflict, later-omission, and global-rederive case, plus `TestGenerateMultiSpecKeepsTenantTemplateBinding`, which generates from two tenant-scoped specs, asserts the emitted `--tenant` flag and `ST_TENANT_ID` config wiring, and runs `go build` on the generated module. `TestGenerateMultiSpecUnionsOAuthScopes` is a pre-existing authorization-code generate test that asserts the emitted scope literal and does not compile the generated module; it is not client-credentials coverage.

Client-credentials generate+compile proof is on sibling PR #4729 (`TestGenerateMultiSpecUnionsClientCredentialsScopes`), which asserts the `resolveClientCredentialsScope` literal and compiles the generated module.

## Why This Works
The merge is the only place the per-spec bindings meet, so it is the only place a union can happen; leaving globals empty routes the threshold decision back to the one function that owns it. For scopes, the compatibility predicate already used the token URL as the authority for the Google case; extending that rule to every client-credentials pair unions exactly the scopes one token endpoint can grant while keeping authorization-code flows on the stricter check documented in `multi-spec-auth-scope-union-2026-05-22.md`.

## Prevention
- A multi-spec print is not proven by `--dry-run`; generate to a scratch directory and grep the emitted `root.go`, `config.go`, and `auth.go` for the bindings and scope literal before trusting the merge.
- When adding a field to `spec.APISpec` that per-spec parsing populates, add it to `applyMultiSpecTemplateVars` (or the relevant merge helper) and to `TestMergeSpecsUnionsEndpointTemplateVarsAndEnvOverrides` in the same change; the merge struct literal silently drops anything it does not name.
- Fields that a later derivation step recomputes (global promotion) must not be seeded from sources; re-derive on the merged shape.
- Generator test runs compile many generated modules into the isolated build cache under `~/.cache/printing-press/go-build`, which nothing trims; clear it between heavy runs until upstream issue #4730 lands.

## Related Issues
- Upstream issues #4726, #4728 and PRs #4727, #4729
- `docs/solutions/logic-errors/multi-spec-auth-scope-union-2026-05-22.md` (compatibility-predicate shape this change extends)
- Upstream issue #4730 (unbounded isolated GOCACHE)
