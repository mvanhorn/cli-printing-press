---
title: "MCPB auth env-var kind filters can drift from verifier expectations"
date: 2026-05-24
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: authentication
symptoms:
  - "Published-library verify-manifest reports server.mcp_config.env missing auth_flow_input or harvested env vars"
  - "Generated MCPB manifest user_config only prompts for per_call auth env vars"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - auth
  - env-vars
  - mcpb
  - manifests
  - verify-manifest
---

# MCPB auth env-var kind filters can drift from verifier expectations

## Problem

Multi-credential OAuth CLIs can declare `auth.env_var_specs` entries for setup inputs, harvested tokens, and per-call request credentials. The public library verifier compares `.printing-press.json` `auth_env_vars` against MCPB `server.mcp_config.env`, so the MCPB manifest must surface every declared env var, not only the ones used directly on each API request.

## Symptoms

- `verify-manifest` fails a freshly published CLI with `server.mcp_config.env missing entries for [...]`.
- The generated MCPB `manifest.json` contains only `per_call` env vars in `user_config` and `server.mcp_config.env`.
- Hand-editing `manifest.json` to add the missing OAuth client or harvested-token env vars makes the downstream verifier pass.

## What Didn't Work

- Treating `auth_flow_input` and `harvested` as non-runtime values inside MCPB manifest generation. That classification is useful for UX copy and auth-flow behavior, but it is the wrong filter for the manifest contract because the host still needs to collect or forward those values.
- Relying on `.printing-press.json` to record all env vars while MCPB reclassifies and drops some of them. The two artifacts then describe different credential surfaces.

## Solution

Use normalized `EnvVarSpecs` as the authoritative manifest input and include every declared env var kind in MCPB `user_config` plus `server.mcp_config.env`. Preserve kind only to shape the install prompt text:

```go
for _, envVar := range envVarSpecs {
    if envVar.Name == "" {
        continue
    }
    envVar.Kind = envVar.EffectiveKind()
    seen[envVar.Name] = struct{}{}
    filtered = append(filtered, envVar)
}
```

Add regression coverage that asserts `per_call`, `auth_flow_input`, and `harvested` env vars all appear in both MCPB launch env and user_config, including default description text for non-per-call values without explicit prose.

## Why This Works

`auth.env_var_specs.kind` answers "how this credential participates in auth", not "whether the MCPB host must know about it." A setup client secret or harvested refresh token may not be sent with every request, but the installed MCP server still needs a configured value. Emitting all declared specs keeps `.printing-press.json`, MCPB `manifest.json`, and the library verifier on the same credential contract while still allowing kind-aware UX.

## Prevention

- Treat `EnvVarSpecs` as the rich source of truth for generated auth surfaces. Downstream emitters should not independently decide which declared env vars are real.
- When changing manifest generation, test the emitted artifact fields that downstream verifiers read, not only the helper that builds them.
- Use kind-aware description defaults instead of kind-based omission when the value should be visible to installers or agents.

## Related Issues

- Issue: [#1945](https://github.com/mvanhorn/cli-printing-press/issues/1945)
- Related learning: `docs/solutions/design-patterns/auth-envvar-rich-model-2026-05-05.md`
