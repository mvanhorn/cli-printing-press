---
title: HTML response-format transforms must run before sync item extraction
date: 2026-05-22
category: docs/solutions/logic-errors
module: cli-printing-press
problem_type: logic_error
component: tooling
symptoms:
  - Generated HTML extraction corrupted non-UTF-8 pages such as ISO-8859-1.
  - Cached HTML responses lost header-only charset hints.
  - Generated sync commands stored raw HTML as one row for html_extract resources.
  - Dependent sync resources missed the same HTML extraction path before page item extraction.
root_cause: missing_workflow_step
resolution_type: code_fix
severity: medium
tags: [html-extraction, sync, generator, charset, response-format]
---

# HTML response-format transforms must run before sync item extraction

## Problem

Generated CLIs mishandled endpoints marked with `response_format: html` or `html_extract`. Direct HTML extraction converted raw bytes to a string before parsing, while sync paths skipped HTML extraction entirely before attempting JSON-style item extraction.

## Symptoms

- Pages served with a non-UTF-8 charset, such as ISO-8859-1, produced corrupted extracted text.
- HTML-backed sync resources stored one raw HTML document row instead of extracted link or page objects.
- Dependent sync resources had the same missing transform path as flat sync resources.
- A dependent sync HTML extraction failure could abort later parents if handled as a whole-resource error.

## What Didn't Work

- Parsing with `xhtml.Parse(strings.NewReader(string(raw)))` assumed the response bytes were already valid UTF-8.
- Preserving only the response body in the HTTP response cache meant cache hits could not replay the `Content-Type` charset hint.
- Calling `extractPageItems` directly on HTML responses treated the original document as the item payload instead of first normalizing it through the endpoint's `html_extract` contract.
- Fixing only direct endpoint commands was insufficient because generated sync and dependent sync use separate templates.

## Solution

Decode HTML response bytes with the charset-aware reader before parsing, passing through the response `Content-Type` so header-only charset declarations are honored:

```go
reader, err := charset.NewReader(bytes.NewReader(raw), contentType)
if err != nil {
	reader = bytes.NewReader(raw)
}
return xhtml.Parse(reader)
```

Persist the response `Content-Type` beside cached response bodies for generated clients that have HTML extraction, and restore it into `LastContentType()` on cache hits.

Propagate the chosen list endpoint's HTML response contract through profiling:

```go
UsesHTMLResponse bool
HTMLExtract      *spec.HTMLExtract
```

Then have generated sync apply the response transform before page item extraction:

```go
if opts, ok := syncHTMLExtractionOptions(resource, "", c.RequestBaseURL(), c.LastContentType(), path, params); ok {
	data, err = extractHTMLResponse(data, opts)
}
items, nextCursor, hasMore := extractPageItems(data, pageSize.cursorParam)
```

Apply the same flow to dependent sync resources, using parent-qualified option keys such as `channels.messages` so a flat resource and a dependent resource with the same name cannot collide. Emit `RequestBaseURL` and `LastContentType` only for generated clients that need HTML extraction, so non-HTML generated clients do not gain unrelated surface area.

In dependent sync, treat HTML extraction errors like fetch errors: report the failing parent and break out of that parent's page loop, but continue with later parents.

## Why This Works

`charset.NewReader` lets the HTML parser consume response bytes using the page's declared or detected charset instead of forcing an early UTF-8 conversion.

The cache metadata sidecar preserves the same charset input on cache hits that live responses had, which keeps repeated syncs from decoding cached HTML differently.

Running HTML extraction before `extractPageItems` restores the intended pipeline:

```text
HTTP response bytes -> extracted HTML data -> page item extraction -> store rows
```

Carrying `UsesHTMLResponse` and `HTMLExtract` through the profiler keeps the template decision resource-specific and avoids hardcoding behavior for one API.

## Prevention

- Cover response-format transforms with generated runtime tests, not only parser or template assertions.
- When changing response transforms, verify direct commands, flat sync, and dependent sync paths.
- Include non-UTF-8 HTML fixtures, including header-only charset cases, so byte-to-string conversions fail visibly.
- Re-run header-only charset fixtures from the generated response cache, not only from a live test server.
- For dependent sync transforms, include one failing parent followed by one successful parent so early-return regressions are visible.
- For generated sync behavior, assert stored row shape rather than command success alone.

## Related Issues

- mvanhorn/cli-printing-press#1499
