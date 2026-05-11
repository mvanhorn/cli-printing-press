# Catalog Entry Validation

Catalog entries in `catalog/` are validated by [`internal/catalog/catalog.go`](../internal/catalog/catalog.go). Keep the inline `AGENTS.md` rule in sync with that validator; when the validator's applicability or allowed values change, update the inline trigger sentence in the same PR.

## Purpose

The embedded catalog is the Printing Press's curated blueprint set. It should preserve representative source patterns that teach and test how to print different classes of CLIs, not exhaustively mirror every printed CLI in the public library.

Do not use the embedded catalog as the primary way to reprint an existing CLI. Reprint from the current local or public library artifact when that artifact already exists, because it carries the fuller source of truth: the spec that actually shipped, generated code, manifest, README, skill, auth notes, and any post-generation fixes. A catalog entry is intentionally smaller and more lossy.

Add entries when they broaden or sharpen the blueprint set: official OpenAPI, docs-derived specs, sniffed/browser APIs, wrapper/community-backed APIs, HTML or sitemap extraction, unusual auth, read-only versus mutating surfaces, or other distinct generator/runtime patterns. Do not add near-duplicates merely because they are useful APIs or members of the same product category.

## Why the inline rule is strict

The catalog is embedded into the printing-press binary via `catalog.FS`, so a bad entry is not a local typo; it becomes part of every rebuilt binary. The inline `AGENTS.md` rule keeps the write-time fence close to the edit, while this doc carries the longer rationale and the wrapper-only shape.

`category` and `tier` are deliberately finite enums because they drive catalog browsing, risk expectations, and downstream copy. `other` is the public catch-all. `example` is accepted only as a test-only bucket for fixtures such as `catalog/petstore.yaml`; do not use it for real catalog entries.

## Inclusion rubric

Passing schema validation is not enough. The catalog is a curated blueprint list for APIs and sites that exercise distinct Printing Press generation patterns. Add an entry only when all of these are true:

- The target demonstrates a meaningfully different source, auth, runtime, data-shaping, or agent-surface pattern from entries already present.
- The target has a concrete user-facing workflow that belongs in a generated CLI or MCP surface.
- The target has a maintained, reachable source path: a vendor spec, official docs that can support an in-repo spec, a verified sniffed/browser-captured spec, a credible community wrapper, or a live public HTML/JSON surface.
- The generation route is reproducible from the entry and PR evidence. A reviewer should understand whether `printing-press generate <name>` works directly, whether a local in-repo spec is required, or whether the entry is only wrapper/discovery backing.
- The auth model is explicit. Public, optional-auth, API-key, OAuth, cookie, browser-token, or harvested-token flows must be described accurately, and personalized flows must not be implied to work anonymously.
- The entry identifies the safe default surface. Read/browse/sync operations are easier to include than mutating actions; external actions such as ordering, sending, posting, purchasing, or launching a browser need explicit opt-in behavior in the generated CLI.

Do not add entries that are only ideas for future scraping, depend on dead or unmaintained endpoints with no live fallback, require private app secrets without a legal/user-supplied auth path, duplicate an existing catalog pattern without a distinct lesson, or duplicate an existing public-library artifact without explaining why the embedded blueprint needs a new source entry.

## Source types

Use the source type to set reviewer expectations:

- `official`: Vendor-published OpenAPI or equivalent machine-readable spec. Prefer this when available.
- `docs`: An in-repo spec derived from official docs. The PR must link the docs and identify any endpoints inferred from live probing or non-doc sources.
- `sniffed`: A spec derived from browser or traffic capture. The PR must name the capture surface, auth/session assumptions, and one or more live smoke checks.
- `community`: A third-party spec or library-backed source. The PR must explain why the source is credible and whether it is maintained.
- Wrapper-only: No direct spec. The entry documents a wrapper library or reverse-engineering technique. Use this when the source is useful catalog backing but does not by itself make `printing-press generate <name>` work.

If an entry should be directly generatable from the catalog, provide a real `spec_url` and `spec_format`. For specs maintained in this repo, place them under `catalog/specs/` and point `spec_url` at the raw GitHub URL. Wrapper-only entries are acceptable, but they must not imply direct generation unless the generator has a concrete spec path.

## Evidence checklist

For each catalog PR, include the following in the PR body:

- Source URL(s) and source type.
- Live smoke evidence for the primary source path, with dates or command output summarized.
- Auth requirements and which commands are public, optional-auth, or auth-required.
- Scope boundaries for risky or personalized flows.
- Whether `testdata/golden/expected/catalog-list/stdout.txt` changed, and why.
- Verification commands actually run, or a clear reason they were not run.

For HTML or scrape-backed entries, also verify crawlability and extractable data, not just page availability. A `200` on a marketing page is not enough; the PR must show that the data needed for the intended CLI surface is present in public HTML, embedded JSON, sitemaps, or documented endpoints.

## Wrapper-only entries

Wrapper-only entries are the carve-out where `spec_url` and `spec_format` stop being required. The validator treats an entry as wrapper-only when `wrapper_libraries` is non-empty and `spec_url` is empty. In that shape:

- `name`, `display_name`, `description`, `category`, and `tier` are still required.
- `wrapper_libraries[*].name`, `.url`, `.language`, and `.integration_mode` are required.
- Wrapper library URLs must use HTTPS.
- `spec_format` is optional, but if present it must still be one of the allowed formats.

Use the wrapper-only carve-out only when the API is genuinely reached through wrapper libraries or documented reverse-engineering backing rather than a direct spec. A wrapper-only entry should name what is proven today and what remains out of scope. If the intended result is a generated CLI, prefer adding or referencing an internal spec instead of leaving the catalog entry as a scrape note.

Wrapper-only entries should not:

- Claim that the generator will emit endpoints unless a real spec path exists.
- Depend on dead community wrappers without a live replacement source.
- Include mutating workflows as default-safe just because a wrapper exposes them.
- Treat private mobile-app endpoints, extracted client secrets, or personalized app flows as public API surface without an auth model.

If the validator or enum values change, update both this doc and the inline `AGENTS.md` rule together.

## Bearer refresh metadata

Catalog entries for browser-facing APIs with rotating public client bearer tokens may declare `bearer_refresh`. When present, both `bearer_refresh.bundle_url` and `bearer_refresh.pattern` are required, the bundle URL must use HTTPS, and the pattern must compile as a Go regexp.

The generator copies this metadata into the printed CLI so `doctor --refresh-bearer` and the agent-accessible `refresh-bearer` command can refresh the user's stored token from the live source bundle.

## Auth key URL

Catalog entries may declare `auth_key_url:` — an HTTPS page where the user can obtain credentials (personal access token, API key, OAuth client, etc.). The generator surfaces it in the printed CLI's auth prompts and `doctor` output as `Get a key at: <URL>`.

Precedence:
- Catalog `auth_key_url` overrides any URL from the spec.
- Otherwise, an OpenAPI spec's [`x-auth-key-url`](SPEC-EXTENSIONS.md#x-auth-key-url) is used.
- Otherwise, the parser infers a URL from the selected security scheme's `description`, then from `info.description` when the surrounding text mentions credential cues. `externalDocs.url` and `info.contact.url` are intentionally **not** fallbacks — those typically point at the docs landing page or company homepage, not the keys UI. When `KeyURL` is empty, the printed CLI surfaces those URLs under a separate `See API docs:` line instead. See [`SPEC-EXTENSIONS.md`](SPEC-EXTENSIONS.md#x-auth-key-url) for details.

Set `auth_key_url:` when the inference would land on a generic homepage and you know the specific token-acquisition page. The validator only checks that the URL starts with `https://`; it does not probe reachability.

## Auth instructions

Catalog entries may also declare `auth_instructions:` — a one-line string of free-form guidance ("Settings → Personal access tokens → Generate new") that the printed CLI prints under the `Get a key at:` line. Use this when the URL lands on a docs page rather than the keys UI: the URL says where to start, the instruction says what to do once there.

Catalog `auth_instructions` overrides any value from the spec's [`x-auth-instructions`](SPEC-EXTENSIONS.md#x-auth-instructions) extension. The printed CLI surfaces it in auth prompts, `doctor`, and the new `auth setup` command (which also takes `--launch` to open the URL in a browser).
