# Absorb Scoring: Auto-Suggest Novel Features

> **When to read:** This file is referenced by Phase 1.5 of the printing-press skill.
> Read it during Step 1.5c.5 to run gap analysis, score candidates, and populate the transcendence table.

### Step 1.5c.5: Auto-Suggest Novel Features

**This step runs automatically.** No user interaction. Synthesize ALL research gathered so far (Phase 1 brief + Phase 1.5a ecosystem search + Phase 1.5b absorb manifest) into evidence-backed feature recommendations.

#### Gap Analysis

Analyze these 5 categories using data already gathered — do NOT run new searches:

1. **Domain-specific opportunities** — Based on the API Identity from Phase 1 brief. What intelligence does this domain uniquely enable?
   - Sports APIs → trend analysis, player comparison, game alerts, fantasy projections
   - Project management APIs → bottleneck detection, velocity trends, workload balance, stale issue radar
   - Payments APIs → reconciliation, revenue trends, dispute tracking, churn prediction
   - Communication APIs → response time analytics, channel health, thread summarization
   - CRM APIs → pipeline velocity, deal scoring, contact engagement trends

2. **User pain points** — From Phase 1 research: npm README "limitations" sections, GitHub issues on competitor repos, community docs mentioning workarounds, PyPI package descriptions mentioning what's missing

3. **Competitor edges** — From the absorb manifest: what does the BEST competitor tool uniquely offer that nobody else has? Can we beat it with the SQLite layer?

4. **Cross-entity queries** — What joins across synced tables produce insights no single API call can? (This overlaps with Step 1.5c but approaches it from the data model, not the use case)

5. **Agent workflow gaps** — What would an AI agent using this CLI wish it could do in one command instead of multiple? (e.g., "show me everything about X" commands, bulk operations, pre-flight checks)

6. **Self-brainstorm** — Answer these questions using the research context gathered so far. Do NOT ask the user — answer them yourself from the research brief, absorb manifest, and ecosystem findings:
   - Based on the research brief's top workflows and user profiles, what workflows does the typical power user of this API do that aren't covered in the absorbed features?
   - Based on competitor repo issues, community pain points, and ecosystem gaps found in Phase 1/1.5, what are the most annoying limitations that a CLI with SQLite could fix?
   - Based on the NOI and domain archetype, what single "killer feature" would make a power user install this CLI over any alternative?
   - (Only when `USER_BRIEFING_CONTEXT` is non-empty) Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't already cover?

#### Generate and Score Candidates

Generate 3-8 novel feature ideas (across all 6 categories). For each, score on 4 dimensions:

| Dimension | Points | Scoring |
|-----------|--------|---------|
| **Domain Fit** | 0-3 | 3=core to this API's power users, 2=useful but niche, 1=tangential, 0=wrong domain |
| **User Pain** | 0-3 | 3=research surfaced explicit demand (community complaints, competitor gap), 2=implied need, 1=speculative, 0=no evidence |
| **Build Feasibility** | 0-2 | 2=SQLite store + existing sync covers it, 1=needs minor data model additions, 0=requires new infrastructure |
| **Research Backing** | 0-2 | 2=evidence from 2+ sources in Phase 1/1.5 research, 1=evidence from 1 source, 0=invented |

**Normalize:** `score_10 = round(raw / 10 * 10)`. Include features scoring >= 5/10.

#### Add to Transcendence Table

Add each qualifying feature as a new row in the transcendence table:

```markdown
| # | Feature | Command | Why Only We Can Do This | Score | Evidence |
|---|---------|---------|------------------------|-------|----------|
| N | Player comparison | compare "LeBron" "Curry" | Requires local join across player stats + team + season data | 8/10 | ESPN community requests, espn_scraper lacks cross-player queries |
```

The "Evidence" column MUST cite specific findings from Phase 1 or Phase 1.5 research. No unsupported assertions.
