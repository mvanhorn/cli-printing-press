## Phase 1: Research Brief

**When `BROWSER_SNIFF_TARGET_URL` is set:** Skip spec/docs search and SDK wrapper search — none of these exist for an undocumented website feature. Focus research on understanding what the site/feature does, who uses it, what workflows it supports, and what competitors offer similar functionality. The spec will come from browser-sniffing in [Phase 1.7](06-browser-sniff-gate.md).

Before reading documentation, read [references/fetch-docs.md](../references/fetch-docs.md). Use `fetch-docs.sh` for the API's primary docs, OpenAPI/Postman links, auth guides, error handling, rate limits, pagination, webhooks, and any per-endpoint reference page. Preserve exact status codes and inspect the returned local file directly so enum values, field constraints, casing, examples, and nav/link variants are not lost through summarization.

There is no built-in catalog lookup in this repo. New CLIs start from researched specs, documentation, browser/crowd/device discovery, or a reprint of an existing local/public-library artifact. If the target already exists in the public library, use the reprint/publish workflow rather than adding source metadata here.

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

**MANDATORY: Before proceeding to [Phase 1.5](08-ecosystem-absorb-gate.md) (Absorb Gate), you MUST evaluate [Phase 1.6](05-pre-browser-sniff-auth-intelligence.md) (Pre-Browser-Sniff Auth Intelligence), [Phase 1.7](06-browser-sniff-gate.md) (Browser-Sniff Gate), and [Phase 1.8](07-crowd-sniff-gate.md) (Crowd-Sniff Gate) in their linked files.** If no spec source has been resolved yet (no `--spec`, no `--har`, no researched spec URL), the browser-sniff gate decision matrix MUST be evaluated. Do not skip to [Phase 1.5](08-ecosystem-absorb-gate.md).

**[Phase 1.5](08-ecosystem-absorb-gate.md) will refuse to proceed without a `browser-browser-sniff-gate.json` marker file.** [Phase 1.7](06-browser-sniff-gate.md) writes this file with one entry per source (one entry for single-source CLIs, one entry per named source for combo CLIs). Missing marker = HARD STOP back to [Phase 1.7](06-browser-sniff-gate.md). See the "Enforcement" section in [Phase 1.7](06-browser-sniff-gate.md) for the contract.


Next: phases/05-pre-browser-sniff-auth-intelligence.md
