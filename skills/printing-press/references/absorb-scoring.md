# Absorb Scoring Rubric: Novel Features

> **When to read:** This file is the scoring rubric for the novel-features
> subagent (Phase 1.5 Step 1.5c.5). It defines kill/keep checks, the
> 4-dimension score, the transcendence table format, and reprint verdict
> rules. The subagent reads this file as part of its operational playbook —
> the SKILL no longer applies it inline.
>
> Brainstorming flow (personas, candidate generation, gap analysis, cut pass)
> lives in [novel-features-subagent.md](novel-features-subagent.md). This
> file owns the static rubric only.

## Kill/keep checks

Apply these to every candidate BEFORE scoring. Cut ruthlessly — survivors must
be features that can actually ship.

| Check | Kill condition | Keep/reframe action |
|-------|---------------|-------------------|
| **LLM dependency** | Feature requires NLP, summarization, sentiment analysis, classification, or semantic grouping | **Reframe as mechanical:** replace "summarize" with "extract top-rated items + stats." Add pipe-friendly output so users can `\| claude "summarize"` themselves. If no mechanical version is useful, **cut**. |
| **External service** | Feature requires a service not in the spec (e.g., scraping a website, calling a third-party API not in the brief) | **Cut** unless the service is free, public, and has no auth. An enrichment API documented in the brief (like OMDb for movie-goat) is fine. |
| **Auth the user doesn't have** | Feature requires write access, OAuth scopes, or paid tiers the user hasn't confirmed | **Gate** behind an auth check, or **cut** if the feature is useless without it. Read-only features using the same auth as other commands are fine. |
| **Scope creep** | Feature is really an application, not a command. Would take >200 lines to implement, needs a TUI, or requires persistent background processes. | **Descope** to the one-command version. "Dashboard" → "summary stats." "Monitor" → "poll once with --watch." If the one-command version isn't useful, **cut**. |
| **Verifiability** | Feature can't be tested in dogfood. No way to verify the output is correct without manual inspection or domain expertise. | **Flag** as low-confidence. Keep only if the value is high enough to justify manual QA. |
| **Reimplementation** | Feature synthesizes API responses locally instead of calling the API. Hand-rolled response builders, hardcoded JSON returned as an "API result," endpoint stubs that return constants, or aggregations computed in-process when the API has an aggregation endpoint. | **Cut or rewrite.** A printed CLI that pretends to call the API is strictly worse than the API call it replaces. The one exception is features that read from the local SQLite store (`stale`, `bottleneck`, `health`, `reconcile`); those are local-data commands, not fake API calls. Dogfood's `reimplementation_check` enforces this at generation time. |

**Buildability proof.** For each surviving feature, write one sentence:

> "This uses [specific API endpoint or local data] to compute [specific output] with no external dependencies."

If you can't write that sentence, the feature fails the vet.

## Score (4 dimensions, raw 0-10)

Apply to candidates that survive the kill/keep checks.

| Dimension | Points | Scoring |
|-----------|--------|---------|
| **Domain Fit** | 0-3 | 3=core to this API's power users, 2=useful but niche, 1=tangential, 0=wrong domain |
| **User Pain** | 0-3 | 3=research surfaced explicit demand (community complaints, competitor gap), 2=implied need, 1=speculative, 0=no evidence |
| **Build Feasibility** | 0-2 | 2=API endpoint + local data covers it, 1=needs minor data model additions, 0=requires new infrastructure |
| **Research Backing** | 0-2 | 2=evidence from 2+ sources in Phase 1/1.5 research (web search, community issues, MCP source, DeepWiki analysis each count as 1 source), 1=evidence from 1 source, 0=invented |

**Normalize:** `score_10 = round(raw / 10 * 10)`. Include features scoring >= 5/10.

## Transcendence Table Format

Survivors render as rows in the absorb manifest's transcendence table:

```markdown
| # | Feature | Command | Score | How It Works | Evidence |
|---|---------|---------|-------|-------------|----------|
| N | Player comparison | compare "LeBron" "Curry" | 8/10 | Joins player_stats + team + season tables in local SQLite | ESPN community requests, espn_scraper lacks cross-player queries |
```

The "How It Works" column is the buildability proof — one sentence showing the
specific API endpoint or local data that powers the feature.

The "Evidence" column MUST cite specific findings from Phase 1 or Phase 1.5
research. "Power users would love this" is not evidence.

## Reprint verdict rules

Reprints occur when a prior `research.json` exists at
`$PRESS_LIBRARY/<api>/research.json` (provenance) or under
`$PRESS_MANUSCRIPTS/<api-slug>/*/research.json` (archived runs). The subagent
loads prior `novel_features` (planned) and `novel_features_built` (shipped),
re-scores each against the current personas, and tags every prior feature with
exactly one verdict:

| Verdict | When | Pool action |
|---------|------|-------------|
| **Keep** | Persona fit, score ≥ 5/10, buildable | Add with prior `command` reused so the reprint stays compatible |
| **Reframe** | Right idea, wrong shape — persona fit exists but command/scope drifted | Add with a new `command`/`description`; flag the rename |
| **Drop** | No persona fit, score < 5/10, or unbuildable now | Exclude; record one-line reason for the manifest |

**Surface in the manifest.** Reprint runs add a `Source` column to the
transcendence table: `prior (kept)`, `prior (reframed from <old-command>)`, or
`new`. Below the table, list dropped prior features with their one-line
justifications so the user can override the drop at the Phase 1.5 gate review.
Prior features are never silently absorbed and never silently dropped.
