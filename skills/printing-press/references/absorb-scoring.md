# Absorb Scoring: Auto-Suggest Novel Features

> **When to read:** This file is referenced by Phase 1.5 of the printing-press skill.
> Read it during Step 1.5c.5 to run gap analysis, score candidates, and populate the transcendence table.

### Step 1.5c.5: Auto-Suggest Novel Features

**This step runs automatically.** No user interaction. Synthesize ALL research gathered so far (Phase 1 brief + Phase 1.5a ecosystem search + Phase 1.5b absorb manifest) into evidence-backed feature recommendations.

#### User-First Feature Discovery

Before generating features from technical capabilities, think about the humans
who will use this CLI. The best transcendence features come from understanding
user rituals and service-specific content patterns, not from asking "what can
SQLite do?"

##### Step 1: Identify specific user personas (2-4 personas)

Don't say "developers" or "users." Name specific people with specific habits:

- "Someone who checks HN every morning before standup"
- "A hiring manager scanning Who's Hiring threads monthly"
- "A movie buff deciding what to watch tonight"
- "A developer about to post their Show HN launch"

Draw these from the Phase 1 brief's "Users" and "Top Workflows" sections. Each
persona represents a feature surface.

##### Step 2: Map each persona's rituals and frustrations

For each persona, answer:
- **What do they do repeatedly?** (daily/weekly/monthly rituals with this service)
- **What question do they wish they could answer but can't?** (This IS the feature.)
- **What's tedious about their current workflow?** (This IS the automation opportunity.)

Example (HN):
- Persona: "Morning HN checker"
- Ritual: Opens HN, scans top stories, opens a few
- Question they can't answer: "What hit the front page while I was coding?"
- Feature: `hn since 2h` — one command, no setup

##### Step 3: Identify service-specific content patterns

Every service has unique content types, categories, or workflows that define its
identity. These are feature surfaces that generic "CRUD + analytics" thinking misses:

- HN: Show HN, Ask HN, Who's Hiring, Who's Looking (each is a feature surface)
- Spotify: Playlists, Discover Weekly, Wrapped/year-end stats
- GitHub: PRs, Issues, Actions, Discussions (each has its own workflows)
- TMDb: Collections/franchises, Watch providers, Trending

For each content pattern, ask: "What would the power user of THIS specific
feature want that no existing tool provides?"

##### Step 4: Self-vet before presenting

Run every candidate feature through these 5 kill/keep checks. Do this BEFORE
scoring, BEFORE presenting to the user. Cut ruthlessly — the user should only
see features that can actually ship.

| Check | Kill condition | Keep/reframe action |
|-------|---------------|-------------------|
| **LLM dependency** | Feature requires NLP, summarization, sentiment analysis, classification, or semantic grouping | **Reframe as mechanical:** replace "summarize" with "extract top-rated items + stats." Add pipe-friendly output so users can `\| claude "summarize"` themselves. If no mechanical version is useful, **cut**. |
| **External service** | Feature requires a service not in the spec (e.g., scraping a website, calling a third-party API not in the brief) | **Cut** unless the service is free, public, and has no auth. An enrichment API documented in the brief (like OMDb for movie-goat) is fine. |
| **Auth the user doesn't have** | Feature requires write access, OAuth scopes, or paid tiers the user hasn't confirmed | **Gate** behind an auth check, or **cut** if the feature is useless without it. Read-only features using the same auth as other commands are fine. |
| **Scope creep** | Feature is really an application, not a command. Would take >200 lines to implement, needs a TUI, or requires persistent background processes. | **Descope** to the one-command version. "Dashboard" → "summary stats." "Monitor" → "poll once with --watch." If the one-command version isn't useful, **cut**. |
| **Verifiability** | Feature can't be tested in dogfood. No way to verify the output is correct without manual inspection or domain expertise. | **Flag** as low-confidence. Keep only if the value is high enough to justify manual QA. |
| **Reimplementation** | Feature synthesizes API responses locally instead of calling the API. Hand-rolled response builders, hardcoded JSON returned as an "API result," endpoint stubs that return constants, or aggregations computed in-process when the API has an aggregation endpoint. | **Cut or rewrite.** A printed CLI that pretends to call the API is strictly worse than the API call it replaces. The one exception is features that read from the local SQLite store (`stale`, `bottleneck`, `health`, `reconcile`); those are local-data commands, not fake API calls. Dogfood's `reimplementation_check` enforces this at generation time. |

**For each surviving feature, write one sentence proving it's buildable:**
"This uses [specific API endpoint or local data] to compute [specific output]
with no external dependencies."

If you can't write that sentence, the feature fails the vet.

#### Gap Analysis

After the user-first discovery, run these technical analyses to find anything
the persona work missed:

1. **Cross-entity queries** — What joins across synced tables produce insights no single API call can?

2. **User pain points** — From Phase 1 research: npm README "limitations" sections, GitHub issues on competitor repos, community docs mentioning workarounds

3. **Competitor edges** — From the absorb manifest: what does the BEST competitor tool uniquely offer? Can we beat it?

4. **Agent workflow gaps** — What would an AI agent using this CLI wish it could do in one command instead of multiple?

5. **Self-brainstorm** — Answer these questions using the research context gathered so far. Do NOT ask the user — answer them yourself from the research brief, absorb manifest, and ecosystem findings:
   - Based on the research brief's top workflows and user profiles, what workflows does the typical power user of this API do that aren't covered in the absorbed features?
   - Based on competitor repo issues, community pain points, and ecosystem gaps found in Phase 1/1.5, what are the most annoying limitations that a CLI with SQLite could fix?
   - Based on the NOI and domain archetype, what single "killer feature" would make a power user install this CLI over any alternative?
   - (Only when `USER_BRIEFING_CONTEXT` is non-empty) Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't already cover?
   - (Only when DeepWiki codebase analysis is available) Based on the codebase architecture DeepWiki revealed, what compound use cases become possible that the public API docs don't suggest? Look for internal data relationships, queue/worker patterns, or event systems that could power novel CLI features.

#### Generate, Vet, and Score

1. **Generate** 5-12 candidate features from the user-first discovery + gap analysis.
2. **Vet** each through the Step 4 kill/keep checks. Cut or reframe failures.
3. **Score** survivors on 4 dimensions:

| Dimension | Points | Scoring |
|-----------|--------|---------|
| **Domain Fit** | 0-3 | 3=core to this API's power users, 2=useful but niche, 1=tangential, 0=wrong domain |
| **User Pain** | 0-3 | 3=research surfaced explicit demand (community complaints, competitor gap), 2=implied need, 1=speculative, 0=no evidence |
| **Build Feasibility** | 0-2 | 2=API endpoint + local data covers it, 1=needs minor data model additions, 0=requires new infrastructure |
| **Research Backing** | 0-2 | 2=evidence from 2+ sources in Phase 1/1.5 research (web search, community issues, MCP source, DeepWiki analysis each count as 1 source), 1=evidence from 1 source, 0=invented |

**Normalize:** `score_10 = round(raw / 10 * 10)`. Include features scoring >= 5/10.

#### Add to Transcendence Table

Add each qualifying feature as a new row:

```markdown
| # | Feature | Command | Score | How It Works | Evidence |
|---|---------|---------|-------|-------------|----------|
| N | Player comparison | compare "LeBron" "Curry" | 8/10 | Joins player_stats + team + season tables in local SQLite | ESPN community requests, espn_scraper lacks cross-player queries |
```

The "How It Works" column is the buildability proof from Step 4 — one sentence
showing the specific API endpoint or local data that powers the feature.
The "Evidence" column MUST cite specific findings from Phase 1 or Phase 1.5 research.
