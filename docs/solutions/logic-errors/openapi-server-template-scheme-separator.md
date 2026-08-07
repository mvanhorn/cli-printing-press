---
title: "OpenAPI server template schemes need separator-aware normalization"
date: 2026-05-24
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: tooling
symptoms:
  - "OpenAPI servers such as {protocol}://{hostpath} parsed as a relative base path instead of an absolute base URL"
  - "Generated dry-run output showed GET /http:/localhost:41184/notes instead of GET http://localhost:41184/notes"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - openapi
  - server-url
  - endpoint-template-vars
  - url-normalization
  - generator
---

# OpenAPI server template schemes need separator-aware normalization

## Problem

OpenAPI specs can declare server URLs whose scheme and host are both template variables, such as `{protocol}://{host}/v1`. The parser preserved the variables for runtime substitution, but slash normalization collapsed the separator to `{protocol}:/{host}/v1`, causing the generated client to treat the eventual request URL as relative.

## Symptoms

- A spec declaring `servers: [{url: "{protocol}://{hostpath}"}]` produced dry-run output like `GET /http:/localhost:41184/notes`.
- Literal server URLs and templated hosts with literal schemes, such as `https://{domain}/api/v2`, still worked, which made the failure look narrower than the server-template path really was.

## What Didn't Work

- Treating the problem as only a runtime `buildURL` issue would miss the parser classification bug. Once `resolveServerURLTemplate` stores `{protocol}:/{host}` as `BasePath`, the generated client has already lost the absolute-URL shape.
- Preserving every `://` substring naively creates a legacy compatibility regression: dangling scheme placeholders without `variables:` strip to `://host`, and that empty-scheme shape must still fall through to the older slash cleanup.

## Solution

Make URL slash normalization separator-aware when the substring before `://` is non-empty, and separately classify a full runtime placeholder scheme as an absolute templated base URL.

```go
func hasRuntimeTemplateScheme(s string, defaults map[string]string) bool {
    scheme, _, ok := strings.Cut(s, "://")
    if !ok {
        return false
    }
    matches := templateVarPattern.FindStringSubmatch(scheme)
    if len(matches) != 2 || matches[0] != scheme {
        return false
    }
    _, ok = defaults[matches[1]]
    return ok
}
```

The parser coverage should pin three cases together:

- `{protocol}://{host}/v1` with declared variables remains a `BaseURL`.
- `{stage}/{version}` remains a relative `BasePath`.
- A dangling `{protocol}://host` with no declared variable follows legacy strip-and-normalize behavior instead of preserving an empty scheme.

Generated-runtime coverage should also exercise `buildURL("{protocol}://{host}", "/notes", vars)` so parser success is tied to the URL that the printed CLI actually sends.

## Why This Works

The literal `://` separator is part of the URL grammar, not just duplicate slash noise. Normalization can collapse duplicate slashes in the rest of the URL, but it must not collapse the separator once a real or runtime-templated scheme exists.

`hasRuntimeTemplateScheme` keeps the absolute-vs-relative decision explicit. It only upgrades a templated server URL to `BaseURL` when the scheme is exactly one preserved runtime placeholder backed by a server variable, so adjacent path placeholders remain relative and unresolved placeholders keep legacy behavior.

## Prevention

- When normalizing URLs that may still contain runtime placeholders, preserve grammar separators before doing broad string cleanup.
- Pair parser tests with generated-runtime tests for URL-template bugs. A correct `BaseURL` parse is not enough unless the emitted client builds the same absolute URL after substitution.
- Keep negative tests for legacy unresolved placeholders when broadening template-variable support. OpenAPI permits placeholders that this generator intentionally strips when no runtime substitution path exists.

## Related Issues

- [#1962](https://github.com/mvanhorn/cli-printing-press/issues/1962) - original issue.
- [#1383](https://github.com/mvanhorn/cli-printing-press/issues/1383) - related server URL parsing area, different symptom.
