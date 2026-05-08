---
module: internal/generator
date: "2026-05-08"
problem_type: logic_error
component: tooling
severity: medium
symptoms:
  - "config.go emitted AccessToken/RefreshToken/TokenExpiry/ClientID/ClientSecret fields when auth.type is none"
  - "client.go emitted refreshAccessToken() and AccessToken-driven refresh check when shouldEmitAuth() returned false"
  - "Unused time and net/url imports emitted in config.go and client.go for no-auth specs"
root_cause: logic_error
resolution_type: code_fix
tags:
  - generator
  - auth
  - dead-code
  - templates
  - shouldEmitAuth
  - no-auth
  - co-gating
  - test-coverage
---

## Problem

The Printing Press generator gates `auth.go` emission, the `root.go` `HasAuthCommand` registration, and the scorecard's `scoreAuth` exemption on `Generator.shouldEmitAuth()` (`internal/generator/generator.go:1808`). Two additional artifacts were never wired through the same gate: `config.go.tmpl` and `client.go.tmpl`.

For any spec where `shouldEmitAuth()` returns false — `auth.type: "none"` AND no `AuthorizationURL` AND no `graphql_persisted_query` traffic-analysis hint — the generator emitted:

- **`config.go`**: `AccessToken`, `RefreshToken`, `TokenExpiry`, `ClientID`, `ClientSecret` fields; `SaveTokens()` and `ClearTokens()` methods; the `time` import those methods use.
- **`client.go`**: `refreshAccessToken()` function; an `AccessToken`-driven refresh check inside `authHeader()`; the `net/url` import that function uses.

None of these symbols had any caller in the emitted CLI. They were dead code with no path to populate them — `auth.go` (the only caller of `SaveTokens`/`ClearTokens` on the CLI surface) was correctly suppressed.

The predicate:

```go
// internal/generator/generator.go
func (g *Generator) shouldEmitAuth() bool {
    return g.Spec.Auth.Type != "none" ||
        g.Spec.Auth.AuthorizationURL != "" ||
        g.hasTrafficAnalysisHint("graphql_persisted_query")
}
```

The predicate's doc comment enumerates the call sites that "must agree". Before PR #704, only three of the five did.

## Symptoms

- Generated CLIs for no-auth specs compiled successfully but shipped dead `AccessToken`/`RefreshToken`/`TokenExpiry`/`ClientID`/`ClientSecret` fields, `SaveTokens`/`ClearTokens` methods, and `refreshAccessToken()` with no callers.
- Triage of one such generated CLI concluded "config emission disagrees with auth-template selection — must be a parser bug" (issue #695). The diagnosis was a misread (the fields are universal, not OAuth-specific), but the underlying orphan emission was real.
- No compile error. Dead code is valid Go. The defect was invisible without inspecting generated output against the auth gate predicate.

## What Didn't Work

Two community-proposed fixes were evaluated and rejected before the correct fix was identified.

**Parser-side fallback (rejected).** The proposal was to scan `info.description` for `Authorization: Bearer` prose and operation parameter examples for `*-Token` headers, with the goal of synthesizing a bearer scheme when the spec lacks `securitySchemes`. Verified against the motivating spec (GitHub's upstream OpenAPI):

- `info.description` is six words: `"GitHub's v3 REST API."` — no auth keywords.
- Zero `Authorization` header parameters in any operation.
- Zero top-level `security` blocks; zero `components.securitySchemes`.

`internal/openapi/parser.go`'s existing `inferDescriptionAuth` already covers `bearer`, `access token`, `auth token`, `api key`, `api_key`, `authorization header`. None appear in GitHub's spec. A new regex over the same source would not fire.

**Generator-side fail-fast (rejected).** This proposal built on the misread: it assumed the emitted fields were OAuth-specific and proposed an error when "config has OAuth shape" but no auth template fires. The fields were not OAuth-specific — they were the universal token storage emitted unconditionally for every auth type. A check on "OAuth shape" would either always fire (false positive) or require gating the fields first — at which point the gating *is* the fix, not a check.

**Triage of the originating retro.** The user pain that motivated issue #695 (a generated GitHub CLI with no `auth` command for an API that requires auth) was a workflow execution failure, not a generator bug. `skills/printing-press/SKILL.md:1456` already mandates a Pre-Generation Auth Enrichment step that should have edited the spec when research finds auth signals. The retro that filed #695 noted "10 transcendence features approved at Phase 1.5; 0 implemented" — the enrichment step was skipped. The community-proposed parser fix would have papered over a missed workflow step rather than fixing the generator (auto memory [claude]: "retro default is don't-file — file only when the machine could have raised the floor AND it's generalizable"). The narrower cleanup (this fix) *was* generalizable across every no-auth spec, which is why it landed; the originally-proposed parser fix was not.

## Solution

[PR #704](https://github.com/mvanhorn/cli-printing-press/pull/704). Added `HasAuthCommand bool` to two template-data structs and gated the orphan symbols on that field.

**New struct + field** in `internal/generator/generator.go`:

```go
type configTemplateData struct {
    *spec.APISpec
    HasAuthCommand bool
}

type clientTemplateData struct {
    *spec.APISpec
    HasGraphQLPersistedQueries bool
    // Populated by Generator.shouldEmitAuth() so this template gate stays in
    // sync with auth.go emission, root.go registration, and scoreAuth.
    HasAuthCommand bool
}
```

**Population in `renderSingleFiles`** (`internal/generator/generator.go`):

```go
case "client.go.tmpl":
    data = &clientTemplateData{
        APISpec:                    g.Spec,
        HasGraphQLPersistedQueries: g.hasTrafficAnalysisHint("graphql_persisted_query"),
        HasAuthCommand:             g.shouldEmitAuth(),
    }
case "config.go.tmpl":
    data = &configTemplateData{
        APISpec:        g.Spec,
        HasAuthCommand: g.shouldEmitAuth(),
    }
```

**`config.go.tmpl` gate** (`internal/generator/templates/config.go.tmpl`): `time` import, all five token fields, and `SaveTokens`/`ClearTokens` are wrapped in `{{- if .HasAuthCommand}} ... {{- end}}`.

**`client.go.tmpl` gate** (`internal/generator/templates/client.go.tmpl`): `net/url` import, `refreshAccessToken()`, and the `AccessToken`-driven refresh check inside `authHeader()` are gated. The `authHeader()` block extends the existing `client_credentials` chain to `{{- else if .HasAuthCommand}}` so the OR-logic matches `shouldEmitAuth()`'s three OR-branches.

**Whitespace gymnastic.** `{{- if .HasAuthCommand}}\n\nfunc...` (with an embedded blank line inside the block) preserves the blank-line separator between functions when the gate fires. The leading `{{- ` strips the preceding blank line in the template source; the embedded blank line replaces it on emission. Required to satisfy `gofmt` and avoid a golden-output diff.

## Why This Works

`Generator.shouldEmitAuth()` is the canonical predicate. Plumbing its return value into both template-data structs makes the templates gate on the exact same condition that gates `auth.go`. All five call sites enumerated in the predicate's doc comment now agree by construction.

When `shouldEmitAuth()` is false, no token field, no token method, no refresh function, and no associated import reach the emitted CLI. When it is true, the full scaffolding emits as before. No caller in either branch is left without its callee.

## Prevention

**1. Predicates whose doc comment says "must agree" need every call site visible at the predicate definition.** Adding a sixth gated artifact in the future is at risk of silently skipping the gate by passing `g.Spec` directly via the `default` arm of the `renderSingleFiles` switch (residual risk surfaced by the maintainability reviewer in PR #704's code review). Prefer explicit `case` arms for every template that needs the gate value, even when the only reason for the arm is to attach the predicate output to template data.

**2. Conditional-emission tests must pin every branch of the predicate, not just the common case.** A regression that short-circuits `shouldEmitAuth()` to one OR-arm passes a single-case test. The test added in PR #704 (`internal/generator/auth_none_dead_code_test.go`) is table-driven with one row per OR-branch:

```go
tests := []struct {
    name                 string
    auth                 spec.AuthConfig
    trafficAnalysisHints []string
    expect               func(t *testing.T, src, sym string)
}{
    {name: "no_auth_omits_scaffolding",
     auth: spec.AuthConfig{Type: "none"},
     expect: func(t *testing.T, src, sym string) { assert.NotContains(t, src, sym) }},
    {name: "api_key_keeps_scaffolding",
     auth: spec.AuthConfig{Type: "api_key", Header: "Authorization", Format: "Bearer {token}", EnvVars: []string{"MYAPI_TOKEN"}},
     expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) }},
    {name: "none_with_authorization_url_keeps_scaffolding",
     auth: spec.AuthConfig{Type: "none", AuthorizationURL: "https://example.com/oauth/authorize"},
     expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) }},
    {name: "none_with_graphql_persisted_query_keeps_scaffolding",
     auth: spec.AuthConfig{Type: "none"},
     trafficAnalysisHints: []string{"graphql_persisted_query"},
     expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) }},
}
```

**3. String-presence tests on generated source can't catch import-gating mistakes.** `assert.NotContains(t, src, "TokenExpiry")` passes even if a stray `time.Time{}` reference left elsewhere in the template would prevent the generated CLI from compiling (unused import or undefined symbol). After gating symbols out of a template, pair the string-presence assert with `go build` on the generated output:

```go
runGoCommand(t, outputDir, "mod", "tidy")
runGoCommand(t, outputDir, "build", "./...")
```

Use the existing `runGoCommand` helper (`internal/generator/generator_test.go:981`); it auto-skips under `-short` so the unit lane stays fast and full CI catches the regression.

## Related

- [`docs/solutions/logic-errors/inline-authorization-param-bearer-inference-2026-05-05.md`](../logic-errors/inline-authorization-param-bearer-inference-2026-05-05.md) — same broad problem class (auth-emission gates drifting across files) but the inverse failure mode: parser *under-inferring* bearer auth and producing `auth.type: "none"` for an API that needs auth. Together the two docs establish the co-gating invariant from both directions of failure.
- [`docs/solutions/design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md`](../design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md) — establishes `auth.Type` branching as the team's accepted idiom for auth-conditional template emission. PR #704 extends that idiom to structural code files (`config.go`, `client.go`) via the same predicate.
- [`docs/solutions/best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md`](../best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md) — template changes that alter the `config.go` / `client.go` field set may break printing-press-library CI; coordinate landing.
- Issue #695 (open) — the originating triage filing. PR #704 closes the dead-code half; the user pain (no `auth` command on a generated GitHub CLI) is workflow-level, not a generator bug. See PR #704 description for full triage.
