---
title: "AuthHeader must not clobber Load-derived auth provenance"
date: 2026-05-24
category: logic-errors
module: internal/generator
problem_type: logic_error
component: authentication
symptoms:
  - "doctor --json reports auth_source as env:<NAME> for credentials saved in the config file"
  - "AuthHeader returns the correct Authorization header while mutating AuthSource to the wrong origin"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - auth
  - auth-source
  - config-template
  - doctor
  - provenance
---

# AuthHeader must not clobber Load-derived auth provenance

## Problem

Generated CLIs use `Config.AuthSource` to explain where credentials came from in diagnostics such as `doctor --json`. `Load()` can distinguish env-var credentials from config-file credentials, but `AuthHeader()` also stamped `AuthSource` while returning the header.

That made a disk-stored token look like an env-var token whenever the generated field name was populated, because `AuthHeader()` could not tell whether `Load()` filled that field from the environment or from the config file.

## Symptoms

- `auth set-token` saves a token to `config.toml`, with no env var set.
- `Load()` labels the credential as `config`.
- Calling `AuthHeader()` returns the correct header but changes `AuthSource` to `env:<NAME>`.
- `doctor --json` reports the wrong `auth_source`, which is misleading for humans and agents that use diagnostics to understand credential setup.

## What Didn't Work

- Treating `AuthHeader()` as the place to derive origin from populated credential fields. At that point the field value has already been merged from config and env sources, so field presence is no longer provenance.
- Guarding every `AuthHeader()` source assignment unconditionally. That preserved config-file provenance, but broke cases where the returned header intentionally came from a higher-priority credential than the source label set during `Load()`, such as bearer-refresh access tokens or OAuth2 access tokens winning over setup env vars.

## Solution

Keep provenance determination in `Load()` and make `AuthHeader()` avoid overwriting a non-empty source when it is returning the same env-backed field:

```go
if c.TokenField != "" {
    if c.AuthSource == "" {
        c.AuthSource = "env:TOKEN_ENV"
    }
    return "Bearer " + c.TokenField
}
```

For branches where `AuthHeader()` deliberately returns a credential that takes precedence over env-backed fields, let the branch correct stale env labels:

```go
if c.AccessToken != "" {
    if c.AuthSource == "" || strings.HasPrefix(c.AuthSource, "env:") {
        c.AuthSource = "oauth2"
    }
    return "Bearer " + c.AccessToken
}
```

Bearer-refresh is stricter: a refreshed `AccessToken` always wins over stale generated env-var fields, so the branch stamps `bearer_refresh` even when `Load()` saw an env var.

## Why This Works

`Load()` is the only point that still knows where each credential value came from. Preserving its label fixes normal config-vs-env diagnostics. The explicit exceptions keep `auth_source` aligned with the credential that actually produced the outbound header when runtime precedence intentionally overrides a stale env-backed value.

## Prevention

- When generated getters return credential material, test both the returned header and any diagnostic provenance they mutate.
- Add generated-runtime tests for config-file credentials with env vars unset, env-over-config credentials, and precedence exceptions where `AccessToken` wins over env setup fields.
- Update golden fixtures for template changes only after inspecting that the diff matches the intended generated code shape.

## Related Issues

- Issue: [#1970](https://github.com/mvanhorn/cli-printing-press/issues/1970)
- Related learning: `docs/solutions/design-patterns/auth-envvar-rich-model-2026-05-05.md`
