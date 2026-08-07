---
title: "Codegen used full-Params index for args[] when Cobra populates from Positional-only declaration order"
date: 2026-05-12
category: logic-errors
module: internal/generator/templates
problem_type: logic_error
component: tooling
symptoms:
  - "Generated path-param commands fail at runtime with '<name> is required' no matter what positional the user passes (e.g. keap-pp-cli tags contacts list-for-tag-id-using-get 1992 returns 'tagId is required')"
  - "Bug reproduced in 3 of 3 published CLIs sampled (keap, supabase, learndash) — 5 broken commands total"
  - "phase5-acceptance.json reports 9/9 verify pass on the affected CLI; verify does not invoke path-param commands with real positional values, so the runtime mismatch ships silently"
  - "Sibling path-param commands without a trailing static URL segment (e.g. /v1/contacts/{email}) ship correctly, masking the bug shape from spot checks"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - generator-template
  - path-params
  - positional-args
  - cobra
  - args-indexing
  - printed-cli
  - command-endpoint-template
  - command-promoted-template
---

# Codegen used full-Params index for args[] when Cobra populates from Positional-only declaration order

## Problem

`command_endpoint.go.tmpl` and `command_promoted.go.tmpl` iterated `Endpoint.Params` with `range $i, $p := .Endpoint.Params` and interpolated `$i` into both `args[$i]` reads and `if len(args) < N` guards inside positional-only emission blocks. Cobra populates the runtime `args` slice from CLI positional arguments in Positional-only declaration order, but `Endpoint.Params` interleaves query and header params alongside positional ones. As soon as a non-positional param sat before a positional path param (a common shape — OpenAPI list endpoints routinely declare `limit`/`offset` queries ahead of the path param), the emitted index drifted off the Positional ordinal and the command failed at runtime with `<name> is required`.

The two-positional case where URL order coincides with Positional-only declaration order produced correct code by coincidence (`$i` equalled the Positional ordinal). The single-positional-with-trailing-segment case diverged and emitted a stale index.

## Symptoms

- `keap-pp-cli tags contacts list-for-tag-id-using-get 1992` → `Error: tagId is required` (template emitted `args[2]` / `len(args) < 3`; Cobra had `args = ["1992"]`).
- Reproduced in three published CLIs:
  - **keap** (3 broken commands): `tags_contacts_list-for-tag-id-using-get`, `tags_companies_list-for-tag-id-using-get`, `orders_transactions_list-for-order-using-get`.
  - **supabase** (1): `projects_types_generate-typescript`.
  - **learndash** (1): `users_course-progress_user-module-steps` (the first positional `args[0]` was correct; the second got the full-Params index `args[3]`).
- After patching `args[N]` → `args[0]` and `len(args) < N+1` → `len(args) < 1` locally, the keap command returned the 78 contacts the user was asking about.
- Verify reports 100% pass on the affected CLIs; the matrix exercises path-param commands without supplying real positional values, so the runtime mismatch is invisible to the scorer.

## What Didn't Work

Nothing went down a dead end. The retro report ([#1199](https://github.com/mvanhorn/cli-printing-press/issues/1199)) included diagnosis with high-confidence smoking-gun comparison (working `contacts/{email}/email-addresses` emit at `args[0]` versus broken `tags/{tagId}/contacts` emit at `args[2]` within the same generated CLI). Investigation went directly to the template iteration.

The first design impulse — refactor the 10 affected template loops to `range positionalParams .Endpoint.Params` (filter to positional-only with new index) — was rejected because four of the loops interleave positional and non-positional emission in a single pass. Splitting into two ranges would shift the order of emitted map entries and break golden tests that froze byte-stable output for currently-correct cases.

## Solution

Introduce a template helper `positionalIndex(endpoint, name) -> int` that walks `Endpoint.Params` and returns the Positional-only ordinal of a named param. Route every `args[...]` interpolation and every `len(args) < ...` guard through it. Preserve the existing range structure (so emission order is byte-stable on currently-correct cases) and only swap the interpolated index value.

**Helper** (`internal/generator/generator.go`):

```go
// positionalIndex returns the args[] slot a positional param fills at runtime.
// Cobra populates args from CLI positionals in Positional-only declaration
// order, but Endpoint.Params interleaves query/header params alongside path
// params. The full-Params index drifts from the Positional ordinal as soon as
// a non-positional param sits before a positional one (common when an OpenAPI
// list endpoint declares query params before the path param) and the runtime
// fails with "<name> is required" no matter what positional the user passes.
// Returns -1 if name is not a positional param.
func positionalIndex(e spec.Endpoint, name string) int {
    idx := 0
    for _, p := range e.Params {
        if !p.Positional {
            continue
        }
        if p.Name == name {
            return idx
        }
        idx++
    }
    return -1
}
```

Registered as a template func and used in both templates. The first positional block in each template binds `{{- $pi := positionalIndex $.Endpoint .Name}}` and reuses it (`add $pi 1` for the len-check threshold plus `args[$pi]` for the substitution); the four other sites per template call the helper inline because the value is used once.

**Before** (`command_endpoint.go.tmpl`):

```gotmpl
{{- range $i, $p := .Endpoint.Params}}
{{- if .Positional}}
{{- if gt $i 0}}
            if len(args) < {{add $i 1}} {
                return usageErr(fmt.Errorf("{{.Name}} is required\n..."))
            }
{{- end}}
{{- if pathContainsParam $.Endpoint.Path .Name}}
            path = replacePathParam(path, "{{.Name}}", args[{{$i}}])
{{- end}}
{{- end}}
{{- end}}
```

**After**:

```gotmpl
{{- range .Endpoint.Params}}
{{- if .Positional}}
{{- $pi := positionalIndex $.Endpoint .Name}}
{{- if gt $pi 0}}
            if len(args) < {{add $pi 1}} {
                return usageErr(fmt.Errorf("{{.Name}} is required\n..."))
            }
{{- end}}
{{- if pathContainsParam $.Endpoint.Path .Name}}
            path = replacePathParam(path, "{{.Name}}", args[{{$pi}}])
{{- end}}
{{- end}}
{{- end}}
```

The four interleaved-emission loops (htmlRequestParams, paginatedGet/resolvePaginatedRead maps, params map) keep their `range .Endpoint.Params` structure and only swap `args[{{$i}}]` for `args[{{positionalIndex $.Endpoint .Name}}]`. Both templates received the same treatment at five sites each (10 sites total).

Regression coverage in `internal/generator/path_param_positional_index_test.go`:

- `TestPathParamArgsIndexUsesPositionalOrdinal` pins three shapes against `command_endpoint.go.tmpl`: a single positional with two queries declared before it (the keap shape), two positionals with a query interleaved between (the learndash shape), and a regression baseline of two positionals in URL-matching order (the products/subscriptions shape that already worked).
- `TestPromotedCommandPathParamArgsIndexUsesPositionalOrdinal` pins the same shape against `command_promoted.go.tmpl` (single-endpoint resource that gets promoted to a top-level command).
- `TestPositionalIndexHelper` unit-tests the helper directly: positional ordinal lookup with queries interleaved, multi-positional ordinal lookup with a query between, unknown-name `-1` sentinel.

`go test ./...` 3804 pass; `scripts/golden.sh verify` 17/17 PASS (byte-stable on existing fixtures because none of the frozen fixtures contained the bug shape); `go vet`, `golangci-lint`, `go fmt` clean.

## Why This Works

`Endpoint.Params` is a heterogeneous list — it carries every parameter the spec declares, regardless of where it lands at runtime (positional, flag, body, header). The template's range index reflects position in *that* list. Cobra's `args` slice, by contrast, reflects position in the Positional-only subsequence — the same subsequence the `Use:` line declares with `<a> <b> <c>`, which `positionalArgs(e)` already builds via the same filter-by-`Positional` pass. By extracting that filter into a callable helper (`positionalIndex`), the template can ask the same question the Use line implicitly answers and stay aligned with Cobra's runtime behavior.

The fix keeps the existing loop topology — `range .Endpoint.Params` with `{{- if .Positional}}` gates — instead of switching to a positional-only iteration. That choice is deliberate: four of the loops interleave positional and non-positional emission in one pass, and splitting them would change the order of emitted map literal entries and break golden tests that froze byte-stable output for currently-correct cases. The helper lookup is O(N×M) per template render, but template rendering is offline code generation; the cost is negligible.

The fix is local to one helper plus 10 template interpolations; no caller of `replacePathParam` or the generated handler functions needs to change.

## Prevention

- **This is now the third recurrence in the same template family.** [#171 WU-1](https://github.com/mvanhorn/cli-printing-press/issues/171) fixed an `Endpoint.Params` iteration miscategorization in `command_endpoint.go.tmpl`. [#578](https://github.com/mvanhorn/cli-printing-press/issues/578) fixed the same bug shape on the MCP side in `mcp_tools.go.tmpl` (see [logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md](mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md)). #1199 is the third instance: a different axis (args[] indexing rather than path-vs-query disambiguation) but the same root family — a generator-emitted handler computing an index/category from `Endpoint.Params`'s ordering rather than from a Positional-only or path-template authoritative source. Future template work in this area should default to suspicion.
- **Reuse Positional-only ordinals through a named helper rather than ad-hoc range indices.** `positionalArgs(e)` (which builds the `Use:` string) and `positionalIndex(e, name)` (which routes `args[N]`) are now a matched pair; both filter on `p.Positional` in declaration order. Any new template emission that needs a Cobra-args slot should call `positionalIndex` rather than computing an index from a fresh range.
- **Treat "passes verify" as necessary but not sufficient** for runtime correctness of positional-arg commands. The current verify matrix doesn't supply real positional values to path-param commands, so the failure shape this fix addresses is invisible to scorer-only gates. A printed-CLI dogfood pass that exercises path-param commands with real positionals catches this class of bug; consider that the minimum bar for releasing a CLI with non-trivial path-param surface area.
- **Add golden coverage for new index-shaped emission.** The existing golden fixtures all happened to declare positional params first, so `$i` equalled the Positional ordinal and the byte-stable output was correct by coincidence. A new fixture with a non-positional param declared before a positional path param would lock the helper's behavior into the byte-stable contract. Tracked as a follow-up; the unit + integration tests in `path_param_positional_index_test.go` are the regression net today.
- **Preserve the `positionalIndex` Go doc comment.** The 2026-05-05 retro on the sibling bug flagged that the "why" comment in the handler body was load-bearing — a future drive-by edit nearly recreated the bug by "simplifying" it. The same caution applies here: the comment on `positionalIndex` explains why `Endpoint.Params` ordering differs from Cobra args ordering, and stripping it invites recreation of the bug for the next reader who searches "why isn't `range $i` good enough?"

## Related Issues

- [#1199](https://github.com/mvanhorn/cli-printing-press/issues/1199) — original issue (this fix)
- [#1192](https://github.com/mvanhorn/cli-printing-press/issues/1192) — sibling open issue: `replacePathParam` emits zero path-param encoding (same helper, different bug). Both indicate the path-param emission template is the right place to harden.
- [#1198](https://github.com/mvanhorn/cli-printing-press/issues/1198) — scorer-side gap: verify doesn't exercise path-param positional binding (the reason this shipped silently). Independent fix.
- [#578](https://github.com/mvanhorn/cli-printing-press/issues/578) / [#582](https://github.com/mvanhorn/cli-printing-press/pull/582) — second recurrence (MCP side, same template family).
- [#171 WU-1](https://github.com/mvanhorn/cli-printing-press/issues/171) — first recurrence (CLI side, sibling axis).
- [logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md](mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md) — solution doc for the second recurrence; shares the prevention rationale.
