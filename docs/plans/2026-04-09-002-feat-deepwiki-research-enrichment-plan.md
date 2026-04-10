---
title: "feat: Add DeepWiki analysis to Phase 1 research"
type: feat
status: completed
date: 2026-04-09
---

# feat: Add DeepWiki analysis to Phase 1 research

## Overview

Add DeepWiki as a research source during Phase 1 (Research Brief) and Phase 1.5a (MCP Source Code Analysis). When the research phase discovers a GitHub repo for an API or its SDK, query DeepWiki's MCP server to get a structured understanding of how the API works - auth patterns, data models, architecture, and internal design. This complements crowd-sniff (which finds endpoints) and MCP source reading (which finds auth headers) with "how things actually work" context.

## Problem Frame

The Printing Press research phase currently gathers API intelligence from: web search, npm/PyPI package listings, MCP server READMEs, GitHub issues, and (via crowd-sniff) SDK source code grepping. What it lacks is a semantic understanding of how an API is designed - its data model relationships, authentication flows, rate limiting behavior, error handling patterns, and architectural decisions.

DeepWiki generates AI-analyzed documentation for any GitHub repo. It explains how the codebase works, not just what endpoints exist. For the Printing Press, this means: given an API's SDK repo or server repo, DeepWiki can explain the auth flow, data model, and internal design in a way that directly improves the Phase 1 brief's "API Identity," "Data Layer," and "Product Thesis" sections.

**User's observation:** "they say how things work really well" - this is exactly the gap in current research.

## Requirements Trace

- R1. Query DeepWiki for repos discovered during Phase 1/1.5a research (SDK repos, API server repos, MCP server repos)
- R2. Extract actionable intelligence: auth patterns, data models, entity relationships, rate limiting, error handling
- R3. Feed DeepWiki findings into the Research Brief and Absorb Manifest
- R4. Run in parallel with existing research (no additional latency on the critical path)
- R5. Graceful degradation - if DeepWiki is unavailable or returns poor results, research continues without it

## Scope Boundaries

- This is a skill-level change only (SKILL.md and possibly a new reference file). No Go binary changes.
- DeepWiki is queried via its HTTP API (`/api/wiki_cache`), not via MCP server connection (the skill runs in a context where MCP tools are available but adding a new MCP server mid-session is not practical)
- No changes to spec resolution (DeepWiki does not produce OpenAPI specs)
- No changes to the generator, scorer, or verifier

## Context & Research

### Relevant Code and Patterns

- `skills/printing-press/SKILL.md` - Phase 1 research, Phase 1.5a ecosystem search, Phase 1.5a.5 MCP source reading
- `skills/printing-press/references/absorb-scoring.md` - Gap analysis uses research findings
- `skills/printing-press/references/crowd-sniff.md` - Crowd sniff discovers repos, these are the same repos DeepWiki would analyze
- `internal/crowdsniff/npm.go` - npm search discovers SDK repo URLs (GitHub URLs extracted from package metadata)
- `internal/pipeline/research.go` - Go-side research discovers GitHub repos via search API

### Institutional Learnings

- **Pagliacci retro**: Sniff quality is fragile, especially for auth discovery and SPA navigation. DeepWiki could pre-identify auth patterns before sniffing, reducing the sniff's burden.
- **Postman retro**: Reverse-engineered specs include wrong enum values from client-side code. Server-side repo analysis via DeepWiki could provide accurate server-validated values.
- **Crowd-sniff gives structure, not meaning**: Crowd-sniff discovers endpoint paths and auth header patterns. DeepWiki would explain what those endpoints do and how data flows between them. These are complementary, not overlapping.

### External References

- DeepWiki MCP server: `https://mcp.deepwiki.com/` (no auth, 3 tools: `ask_question`, `read_wiki_structure`, `read_wiki_contents`)
- DeepWiki HTTP API: `https://deepwiki.com/api/wiki_cache/{owner}/{repo}` (cached wiki data)
- URL pattern: `https://deepwiki.com/{owner}/{repo}`

## Key Technical Decisions

- **HTTP API over MCP connection**: Use `WebFetch` against DeepWiki's HTTP endpoints rather than connecting an MCP server mid-session. Simpler, no session config changes, and the skill already has `WebFetch` in its allowed tools. The MCP tools (`ask_question`, `read_wiki_contents`) are richer but require MCP server connection setup that would complicate the skill.

- **Query with targeted questions, not raw wiki dump**: Rather than fetching the entire wiki and parsing it, use DeepWiki's page structure to fetch specific sections relevant to CLI generation: authentication, data models, rate limiting, error handling. This keeps the context window lean.

- **Parallel execution with existing Phase 1.5a searches**: DeepWiki queries run alongside the 10 existing web searches in Phase 1.5a, not after them. This adds zero latency to the research phase.

- **Skill-level integration only**: The Go binary does not need to know about DeepWiki. The skill agent fetches and synthesizes DeepWiki content the same way it does MCP README reading and web search results.

## Open Questions

### Resolved During Planning

- **Q: Where does DeepWiki fit in the pipeline?** After Phase 1 discovers the API's GitHub repos (SDK repos, server repos) and during Phase 1.5a when the agent is reading MCP source code. The repos are already known at this point.

- **Q: What if the API has no GitHub repo?** DeepWiki only works for GitHub repos. For APIs without public repos (e.g., Stripe's server is closed-source), DeepWiki would analyze the SDK repo (e.g., `stripe/stripe-node`), which still provides valuable insight into how the API is designed from the client perspective.

- **Q: Will this slow down the research phase?** No. The queries run in parallel with existing Phase 1.5a web searches. DeepWiki responses are typically fast (cached wiki data).

### Deferred to Implementation

- **Q: What's the optimal set of questions to ask DeepWiki?** Needs experimentation. Start with: auth flow, data model, rate limiting, error handling. Adjust based on which questions produce the most actionable intelligence for the brief.

- **Q: Should DeepWiki analysis feed into the auto-suggest novel features (Step 1.5c.5)?** Likely yes, as a 7th gap analysis category. But the exact scoring dimensions need to be determined during implementation.

## Implementation Units

- [ ] **Unit 1: Add DeepWiki research step to SKILL.md**

**Goal:** Add a new sub-step in Phase 1.5a that queries DeepWiki for repos discovered during research

**Requirements:** R1, R2, R4, R5

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press/SKILL.md` (Phase 1.5a section)
- Create: `skills/printing-press/references/deepwiki-research.md` (extracted reference for the DeepWiki query procedure)

**Approach:**
- Add a new step 1.5a.6 (after MCP source reading in 1.5a.5) titled "DeepWiki Codebase Analysis"
- When Phase 1 or Phase 1.5a discovers a GitHub repo URL (from npm package metadata, GitHub search, MCP server discovery), extract the `owner/repo` path
- Use `WebFetch` against `https://deepwiki.com/{owner}/{repo}` to get the wiki structure
- Then fetch 2-3 targeted pages: authentication, data model/schema, and architecture/overview
- Extract: auth flow description, entity relationships, rate limit behavior, error patterns
- The reference file contains the detailed procedure (which pages to fetch, what to extract, how to handle failures)
- Time budget: 2 minutes max. If DeepWiki is slow or unavailable, skip silently with a log note
- Run in parallel with existing 1.5a searches (not sequentially after them)

**Patterns to follow:**
- Phase 1.5a.5 (MCP source code reading) - same pattern: discover repo, read specific parts, extract structured intelligence, feed into absorb manifest
- Time budget pattern from sniff gate (3 min) and crowd sniff gate (5 min)

**Test scenarios:**
- Happy path: API has a popular SDK repo on GitHub (e.g., `stripe/stripe-node`), DeepWiki returns wiki with auth and data model sections, findings enrich the brief
- Edge case: API has no GitHub repo (closed-source) - step is skipped silently
- Edge case: DeepWiki returns empty wiki or 404 - step is skipped silently, research continues
- Edge case: DeepWiki returns a wiki but the targeted sections don't exist - extract what's available, skip missing sections
- Error path: WebFetch times out or fails - step is skipped, logged as "DeepWiki unavailable"

**Verification:**
- Running `/printing-press` on an API with a popular GitHub repo (e.g., Discord) produces a brief that references DeepWiki findings
- Running `/printing-press` on an API without a repo still completes normally
- No additional latency observed (parallel execution)

- [ ] **Unit 2: Feed DeepWiki findings into Brief and Absorb Manifest**

**Goal:** Integrate DeepWiki intelligence into the Research Brief template and Absorb Manifest scoring

**Requirements:** R3

**Dependencies:** Unit 1

**Files:**
- Modify: `skills/printing-press/SKILL.md` (Phase 1 brief template, Phase 1.5c.5 gap analysis)
- Modify: `skills/printing-press/references/absorb-scoring.md` (add DeepWiki as evidence source)

**Approach:**
- Add a `## Codebase Intelligence` section to the Phase 1 brief template (after "Data Layer", before "User Vision") - only populated when DeepWiki returned useful findings
- In absorb-scoring.md, add DeepWiki findings as a valid evidence source for the "Research Backing" scoring dimension (0-2 points). DeepWiki architectural analysis counts as 1 source for the "2+ sources" threshold
- In Phase 1.5c.5 self-brainstorm, add a 5th question (when DeepWiki data is available): "Based on the codebase architecture DeepWiki revealed, what compound use cases become possible that the public API docs don't suggest?"

**Patterns to follow:**
- "User Vision" section pattern - optional, only populated when context is available
- Absorb scoring's existing evidence attribution pattern ("ESPN community requests, espn_scraper lacks X")

**Test scenarios:**
- Happy path: DeepWiki findings appear in the brief under "Codebase Intelligence" and are cited as evidence in absorb manifest transcendence features
- Edge case: No DeepWiki data available - "Codebase Intelligence" section is omitted from the brief, scoring proceeds without it
- Integration: A novel feature scores higher because DeepWiki provided a second evidence source (pushing Research Backing from 1 to 2)

**Verification:**
- Brief template renders correctly with and without the Codebase Intelligence section
- Absorb scoring correctly counts DeepWiki as an evidence source

## System-Wide Impact

- **Interaction graph:** DeepWiki queries happen inside the Phase 1.5a agent parallel fan-out. No new callbacks, middleware, or observers.
- **Error propagation:** DeepWiki failures are swallowed at the step level (graceful skip). They do not propagate to Phase 1.5 or block the absorb gate.
- **State lifecycle risks:** None. DeepWiki data is ephemeral - it's synthesized into the brief and manifest, not persisted separately.
- **API surface parity:** No changes to any CLI, binary, or published interface.
- **Unchanged invariants:** The spec resolution chain, generator, scorer, verifier, and publisher are all unchanged. DeepWiki enriches research quality; it does not change the pipeline structure.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| DeepWiki service unavailable or slow | 2-minute time budget + graceful skip. Research continues without it. |
| DeepWiki returns hallucinated/inaccurate information | DeepWiki findings are evidence for scoring, not ground truth. They enrich the brief but don't override spec-derived endpoints or crowd-sniff-discovered auth patterns. |
| Context window pressure from large wiki pages | Fetch only 2-3 targeted sections, not the full wiki. Extract structured intelligence, not raw text. |
| DeepWiki API changes or rate limits | No auth required currently. If they add rate limits, the 2-minute timeout handles it. If the API changes, the reference file is easy to update. |

## Sources & References

- DeepWiki: `https://deepwiki.com`
- DeepWiki MCP: `https://mcp.deepwiki.com/`
- Related skill sections: Phase 1.5a.5 (MCP source reading), Phase 1.5c.5 (auto-suggest features)
- Pagliacci retro: auth discovery gaps that DeepWiki could help fill
- Postman retro: server-side validation accuracy from codebase analysis
