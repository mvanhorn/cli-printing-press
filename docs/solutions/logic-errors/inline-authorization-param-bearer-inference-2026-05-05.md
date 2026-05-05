---
title: "Inline Authorization params: conservative bearer inference"
date: 2026-05-05
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: authentication
symptoms:
  - "OpenAPI specs without top-level securitySchemes or security failed to infer bearer_token auth from required operation-level Authorization header params"
  - "Cal.com-style specs exposed Authorization as inline operation parameters instead of reusable OpenAPI security declarations"
  - "Generated CLIs could omit bearer auth wiring unless manual Phase 2 enrichment patched auth metadata"
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - testing_framework
tags:
  - openapi-parser
  - bearer-auth
  - authorization-header
  - inline-parameters
  - security-inference
---

# Inline Authorization params: conservative bearer inference

## Problem

Some OpenAPI specs declare authentication as a required operation-level `Authorization` header parameter instead of using `components.securitySchemes` or top-level `security`. The parser treated those specs as unauthenticated, so generated CLIs could miss bearer auth setup unless the skill flow manually enriched the spec before generation.

## Symptoms

- Cal.com-style specs had no top-level OpenAPI security declaration, but most operations required an `Authorization` header.
- Parsed specs could end up with `auth.type: none` even though the API required bearer credentials.
- Manual auth enrichment became the fallback for a spec shape the parser could infer safely.

## What Didn't Work

- **Description-only auth inference.** Specs may have sparse descriptions, or descriptions may mention bearer tokens in examples, warnings, migration notes, or unrelated prose.
- **Treating any `Authorization` header as bearer.** Adjacent auth-shape sessions showed that auth-looking fields can be non-bearer, query-key, composed, or session-handshake auth. A permissive "Authorization means bearer" rule would repeat that class of bug (session history).
- **Running inline inference when explicit security exists.** `securitySchemes` and top-level `security` are the canonical OpenAPI declarations. A fallback must not compete with them.

## Solution

Add a fourth-tier fallback in `mapAuth`: after explicit security schemes, query-param auth, and description auth all fail, infer bearer auth from inline operation parameters only when the evidence is broad and scheme-specific.

The fallback has 4 guards:

1. The spec has no `components.securitySchemes`.
2. The spec has no top-level `security` declaration.
3. At least 80% of operations have a required `Authorization` header parameter.
4. The header parameter or its schema description contains a non-negated Bearer mention.

```go
func inferOperationLevelBearer(doc *openapi3.T, name string, fallback spec.AuthConfig) spec.AuthConfig {
    if doc == nil || doc.Paths == nil {
        return fallback
    }
    if hasTopLevelSecurityDeclaration(doc) {
        return fallback
    }

    authParamCount := 0
    hasBearerSignal := false
    totalOps := 0

    // Count operations with a required Authorization header and require a
    // non-negated Bearer signal before synthesizing bearer_token auth.
}
```

Use the parser's existing helpers instead of creating nearby duplicate semantics:

```go
func requiredAuthorizationParam(pathItem *openapi3.PathItem, op *openapi3.Operation) (*openapi3.Parameter, bool) {
    for _, p := range mergeParameters(pathItem, op) {
        if p.In == openapi3.ParameterInHeader && strings.EqualFold(p.Name, "Authorization") && p.Required {
            return p, true
        }
    }
    return nil, false
}
```

`mergeParameters` preserves the parser's existing path-level and operation-level parameter precedence. `findUnnegated` avoids false positives from descriptions such as `Do not use Bearer prefix.`

## Why This Works

The fix recognizes a real generated-spec pattern without turning auth inference into guesswork. Broad required-header coverage is structural evidence that the API expects auth on most operations. A non-negated Bearer mention on the parameter or schema narrows that auth shape to bearer-token auth.

The explicit-security guard keeps the fallback behind authoritative spec declarations. Reusing `mergeParameters` keeps parameter resolution consistent with the rest of `internal/openapi/parser.go`, so path-level and operation-level params do not drift between inference paths.

## Prevention

- Order auth inference from most authoritative to least authoritative: explicit security schemes, query-param auth, description auth, then inline operation-level bearer inference.
- Require both structural evidence and textual scheme evidence before inferring bearer auth from header params.
- Use negation-aware matching for auth-scheme mentions.
- Reuse parser helpers such as `mergeParameters` when adding inference over OpenAPI parameters.
- Preserve regression coverage for:
  - positive Cal.com-style inline `Authorization` params
  - exact 80% coverage threshold
  - below-threshold coverage
  - missing Bearer signal
  - negated Bearer signal
  - optional `Authorization` header ignored
  - explicit `securitySchemes` or `security` winning over fallback inference

## Related Issues

- [mvanhorn/cli-printing-press#600](https://github.com/mvanhorn/cli-printing-press/issues/600) -- direct issue for operation-level Authorization bearer inference
- [mvanhorn/cli-printing-press#634](https://github.com/mvanhorn/cli-printing-press/pull/634) -- implementation PR
- [mvanhorn/cli-printing-press#597](https://github.com/mvanhorn/cli-printing-press/issues/597) -- parent Cal.com retro
- [mvanhorn/cli-printing-press#517](https://github.com/mvanhorn/cli-printing-press/issues/517) -- related Phase 2 auth enrichment umbrella
