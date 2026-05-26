# Raw Documentation Fetching

> **When to read:** Before Phase 1 docs research, Phase 1.5 absorb reads, or any step that extracts contract details from documentation or source pages.

Use `fetch-docs.sh` for fidelity-sensitive reads. The helper captures the raw response body with `curl`, preserves the final status code, caches successful reads for 24 hours, and returns the local file path for direct `Read`/`Grep` inspection.

## When To Use It

Use the helper for:

- official API docs, auth guides, rate limits, error handling, pagination, webhooks, and per-endpoint reference pages
- OpenAPI, Postman, AsyncAPI, JSON Schema, YAML, markdown, and raw source links
- GitHub source files or plugin indexes where exact identifiers, enum values, header names, paths, and casing matter
- re-checking any docs URL that `WebFetch` reported as missing, vague, or contradictory

Reserve `WebFetch` for quick TL;DR reads of blog posts, community articles, or pages where exact field-level contract details are not being carried into the generated CLI.

## Command

```bash
skills/printing-press/references/fetch-docs.sh [--ttl=<seconds>] [--force] [--md] <url>
```

Default behavior:

- follows redirects and compressed responses
- fails loudly on any final HTTP status other than `200`
- writes captures under `${TMPDIR:-/tmp}/printing-press-fetch-docs/`
- prints `path=`, `status=`, `content_type=`, and `effective_url=`
- reuses a successful capture for 24 hours unless `--force` or `--ttl=0` is set

Use `--force` or `--ttl=0` when validating route quirks such as trailing-slash `404` vs non-trailing-slash `200`.

Use `--md` only when reader-mode markdown is helpful. If `readability-cli` and `turndown-cli` are installed, the helper converts HTML to markdown; otherwise it tries `npx --yes` for those tools. If conversion is unavailable or fails, it keeps the raw capture and prints a warning. For navigation discovery, raw HTML is usually better than reader-mode markdown because nav links are part of the contract surface.

## Required Agent Handling

After fetching:

1. Read or grep the returned `path=` file directly.
2. Record non-200 statuses as reachability evidence instead of summarizing them away.
3. When two URL variants differ only by trailing slash, capitalization, `.html`, or a docs-build quirk, fetch both variants and preserve both status codes in the brief.
4. Copy exact enum values, header names, field names, status codes, path casing, and examples from the raw capture when they affect spec authoring or CLI flags.
5. Do not base generator-contract decisions on `WebFetch` summaries when the raw capture is available.
