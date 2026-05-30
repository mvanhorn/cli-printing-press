# Phase 1: Catalog branch, research checklist & brief template

Lazy-loaded body for the Phase 1 research brief. Read and apply after the fetch-docs pointer, before evaluating Phase 1.6/1.7/1.8. Output: one build-driving brief at `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-brief.md`.

Before starting research, check if the API has a built-in catalog entry:

```bash
cli-printing-press catalog show <api> --json 2>/dev/null
```

If the catalog has an entry for this API, branch on the entry type:

**Spec-based entry** (`spec_url` populated) — present the user with a choice:

> "<API> is in the built-in catalog (spec: <spec_url>). Use the catalog config to skip discovery, or run full discovery?"

- If catalog config: use the spec_url from the catalog entry, skip the research/discovery phase
- If full discovery: proceed with the normal research workflow

**Wrapper-only entry** (no `spec_url`, `wrapper_libraries` populated) — this is a reverse-engineered API that has no official spec but has known community libraries. The catalog entry is a **discovery aid only**: `cli-printing-press generate` requires `--spec` and does not consume wrapper-library metadata, so there is no direct generation path from a wrapper-only entry today. Tell the user this up front via `AskUserQuestion`:

> "<API> has no official spec. The catalog knows about these community-maintained wrappers, but the Printing Press cannot generate a CLI directly from a wrapper. The next step has to be browser-sniffing the upstream to author an internal YAML spec, browser-sniffing or HAR-capturing the dominant source first and then using the multi-source aggregator pattern for secondary hand-authored sources, or hand-writing a Go module that imports the wrapper. Which path do you want?"

Present each `wrapper_libraries` entry alongside the question with language, integration mode, and notes so the user can see what implementation backing exists. Example for `google-flights`:
- **krisukox/google-flights-api** (Go, native, MIT) — Pure Go, importable; single-binary CLI with no runtime deps.

Record the user's choice (and the selected wrapper, when relevant) in `$API_RUN_DIR/state.json` under an `implementation` field so later phases can read it. For wrapper or hand-written-module paths, use `{ "kind": "wrapper", "library": "<name>", "url": "<url>", "integration_mode": "native|subprocess|html-scrape", "next_step": "browser-sniff|hand-written-module" }`. For the aggregator path, use `{ "kind": "aggregator-pattern", "dominant_source": "<source>", "spec_source": "browser-sniff|har|provided-spec", "spec_path": "<path-to-generated-spec>", "secondary_sources": ["<source>"], "next_step": "aggregator-pattern" }`; do not populate `library` or `integration_mode` unless a specific secondary source is backed by a wrapper. This field is for skill bookkeeping; the generator does not currently read it. If the user picks browser-sniff, route into the Phase 1.7 browser-sniff path to produce a spec, then run `generate --spec` against it. If the user picks the aggregator path, first route the dominant source through Phase 1.7 browser-sniff or HAR capture to produce the primary spec, then read and apply [references/aggregator-pattern.md](references/aggregator-pattern.md): generate from that spec, then hand-author the secondary source clients and `sources` command tree. If the user picks a hand-written module, stop the press here and hand off — there is no generator path to drop them into.

**No catalog hit** — proceed normally without mentioning the catalog.

**Adding new wrapper-only APIs:** drop a YAML file in `catalog/` with `wrapper_libraries` populated and rebuild the binary. No skill changes needed.

Write one build-driving brief, not a stack of phase essays.

The brief must answer:

1. What is this API actually used for?
2. What are the top 3-5 power-user workflows?
3. What are the top table-stakes competitor features?
4. What data deserves a local store?
5. Why would someone install this CLI instead of the incumbent?
6. What is the product name and thesis?

Research checklist:
- Find the spec or docs source. For docs pages whose details affect generation, fetch the raw page with `fetch-docs.sh`, then read/grep the returned path directly.
- Find the top 1-2 competitors
- **Check GitHub issues on the top wrapper/SDK repo for "403", "blocked", "broken", "deprecated", "rate limit".** If multiple issues report the API is inaccessible or broken, flag this in the research brief as a reachability risk. This is critical for unofficial/reverse-engineered APIs.
- Find official and popular SDK wrappers on npm (`site:npmjs.com`) and PyPI (`site:pypi.org`)
- Find 2-3 concrete user pain points
- Identify the highest-gravity entities
- Pick the top 3-5 commands that matter most

Do not produce separate mandatory documents for:
- workflow ideation
- parity audit
- data-layer prediction
- product thesis

Put them in the one brief.

Write:

`$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-brief.md`

Suggested shape:

```markdown
# <API> CLI Brief

## API Identity
- Domain:
- Users:
- Data profile:

## Reachability Risk
- [None / Low / High] [evidence: e.g., "6 open issues on reteps/redfin about 403 errors since 2025"]
- Tier/permission hints from 4xx body: [omit when absent; otherwise quote the matched bounded line(s) from Phase 1.9]
- Probe-safe endpoint used: [omit when absent; otherwise "<METHOD> <path>" from `x-pp-safe-probe`]

## Top Workflows
1. ...

## Table Stakes
- ...

## Data Layer
- Primary entities:
- Sync cursor:
- FTS/search:

## Codebase Intelligence
- [DeepWiki findings if available, otherwise omit this section]
- Source: DeepWiki analysis of {owner}/{repo}
- Auth: [token type, header, env var pattern]
- Data model: [primary entities and relationships]
- Rate limiting: [limits and behavior]
- Architecture: [key insight about internal design]

## User Vision
- [USER_BRIEFING_CONTEXT if provided, otherwise omit this section]

## Source Priority
- [Only present for combo CLIs. Copy the confirmed ordering from `source-priority.json`.]
- Primary: <Source A> — [spec state: official / community-wrapper / no-spec-browser-sniff-required] — [auth: free / paid]
- Secondary: <Source B> — [...]
- Tertiary: <Source C> — [...]
- **Economics:** [e.g., "Primary is free; paid key for <Source B> is scoped to its own commands only."]
- **Inversion risk:** [e.g., "Primary has no OpenAPI; secondary has 53-endpoint spec. Do NOT let spec completeness invert the ordering."]

## Product Thesis
- Name:
- Why it should exist:

## Build Priorities
1. ...
2. ...
3. ...
```
