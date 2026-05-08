# OData v4 Support

This document describes how the generator detects OData v4 services and what
generated CLIs look like when OData mode is on.

## Detection

OData mode is enabled when any of these signals are present in the spec:

- `basePath` (Swagger 2) or `servers[].url` (OpenAPI 3) contains `/odata`.
- The raw spec body references OData metadata fields such as `@odata.context`
  or `@odata.etag`.
- Parsed responses are predominantly collection envelopes shaped as
  `{ "value": [...] }`.
- The generator was invoked with `--odata`, which forces OData conventions on
  even when none of the above signals are present.

Swagger 2.0 specs are normalized before parsing so OData services that publish
Swagger 2 still preserve:

- `host` + `schemes` + `basePath` joined into an OpenAPI-style server URL.
- Top-level primitive parameter `type`/`format` lifted into parameter schemas.
- Response `schema` entries lifted into JSON response content blocks.
- `parameters` entries with `in: body` lifted into a `requestBody` so root-level
  POST actions expose body fields as flags rather than dropping them.

## Generated CLI Behavior

### Read query options (collections + entity reads)

When OData mode is on, every generated read endpoint (`list` and `get`) gets
these query option flags wired to OData system query options:

| Flag | OData option |
|---|---|
| `--top int` | `$top` |
| `--skip int` | `$skip` |
| `--filter string` | `$filter` |
| `--orderby string` | `$orderby` |
| `--expand string` | `$expand` |
| `--count` | `$count` |
| `--search string` | `$search` |

The existing global `--select` flag is also forwarded as `$select`, then still
filters rendered output client-side after the response arrives.

OData `{ "value": [...] }` envelopes are unwrapped before subcommand list
output is filtered, so commands like `--select` and `--csv` receive the inner
collection rather than the wrapper.

### Function calls

Generated OData function calls compose the path-call syntax:

```text
/Invoice('477')/Print(Destination='Disk%20File',Format='Adobe%20PDF',FileName='out.pdf')
```

Rules applied:

- Function parameters are emitted inside the final path segment's parentheses.
- Parameters are comma-separated `Name=Value` pairs.
- String values are single-quoted; embedded apostrophes are doubled, then the
  whole quoted value is URL-escaped.
- Numeric and boolean values are not quoted.
- UUID/GUID-formatted values are not quoted.
- Existing keyed parent segments such as `Customer('ALFKI')` and
  GUID-keyed segments remain intact.

### Action bodies

OData action endpoints (POST) move non-path action parameters into the JSON
request body. The generated command exposes one flag per body parameter and
also accepts `--stdin` to provide the entire body as JSON. This applies to
both bound actions (`/Customer('ALFKI')/Reset(...)`) and unbound actions at
the root (`/ResetAll(...)`).

### Binary responses

When an OData function or action declares non-JSON response content (e.g.
`application/pdf`, `text/csv`, spreadsheet types), or when a `*/Print`
endpoint's `Format` enum contains values like `Adobe PDF`, `CSV File`, or
`XLSX File`, the generated command:

- Adds `--output FILE` (and `-o`) to write the body to disk.
- Skips the JSON output pipeline entirely (no provenance wrap, no `--select`).
- Streams the response body raw to `--output` if set, or to stdout.
- Errors when stdout is a TTY and no `--output` is supplied, with a hint to
  use `--output FILE`.

Non-OData generated CLIs are unaffected: the raw download helper is only
emitted when the spec actually contains binary-capable endpoints.

## Verification

Focused unit tests:

```text
go test ./internal/spec ./internal/openapi ./internal/generator \
  -run 'Test(DetectOData|ApplyOData|ParseSwagger2OData|GenerateOData|BodyMap|Promoted)'
```

Full suite plus golden gate:

```text
go test ./...
bash scripts/golden.sh verify
```
