---
title: "feat: Apply PostHog Agent-First Learnings to Printing Press"
type: feat
status: completed
date: 2026-04-10
deepened: 2026-04-10
---

# Apply PostHog Agent-First Learnings to Printing Press

## Overview

PostHog published their "golden rules of agent-first product engineering" based on two architecture overhauls of their MCP server (now 6K+ DAU). This plan maps their five rules against the Printing Press and identifies concrete, high-leverage improvements to the CLIs the machine prints.

The core insight: PP already does several things PostHog learned the hard way, but has meaningful gaps in three areas - MCP tool richness, semantic abstraction, and agent feedback loops.

## Problem Frame

The Printing Press generates CLI + MCP server pairs from API specs. The CLIs are agent-native (--json, --compact, --stdin, typed exit codes). But the MCP servers are thin endpoint wrappers with no domain context, no composition hints, and no front-loaded knowledge. An agent connecting to a printed MCP server gets a flat list of 50+ tools named after URL paths, with no guidance on which matter or how they compose.

PostHog made the same mistake in their v1 and rewrote twice. Their learnings are directly applicable.

## PostHog Rules vs. Printing Press Today

### Rule 1: Let agents do everything users can

**PostHog's lesson:** Every user action must be agent-accessible. They auto-generate tools from typed endpoints with manual opt-in via YAML configs.

**PP status: Strong.** The machine already generates dual interfaces (CLI for shell agents, MCP for IDE agents) from the same spec. Every endpoint becomes both a Cobra command and an MCP tool. Agent-native flags (--json, --compact, --select, --dry-run, --stdin, --yes) are standard. The scorecard checks for their presence.

**Gap:** Parity between CLI and MCP is surface-level. The CLI has `sync`, `search`, `sql`, workflow commands, and NOI features. The MCP server registers raw endpoint tools but none of the higher-level CLI capabilities. An agent using the MCP can call `issues_list` but cannot call `stale` or `search`. The CLI surface is strictly richer than the MCP surface.

### Rule 2: Meet agents at their level of abstraction

**PostHog's lesson:** They replaced per-endpoint tools (get-insight, get-funnel) with a single `executeSql` tool. Agents already speak SQL fluently, so meeting them there eliminated 3 out of 4 tool calls and unlocked creative queries PostHog hadn't anticipated.

**PP status: Partial.** The CLI has `sql` (raw SQLite queries on synced data), `search` (FTS5 full-text), and domain-specific workflow commands. These ARE the right abstraction for shell agents. But the MCP server doesn't expose them. It exposes the raw API endpoints only, forcing IDE agents to reason at the HTTP-path level.

**Gap:** The MCP server is stuck at PostHog's v1 - one tool per endpoint. The higher-level abstractions (sql, search, stale, health, similar) that make the CLI powerful are invisible to MCP-connected agents. This is the single highest-leverage improvement.

### Rule 3: Front-load universal context

**PostHog's lesson:** Their v1 prompt was "Here are some tools, GLHF." Their v2 front-loads taxonomy, SQL syntax, and critical querying rules at session start. Everything else is deferred.

**PP status: Weak.** The generated MCP server has an `about` tool that returns API name, tool count, and unique capabilities. But there's no rich front-loaded context. No taxonomy ("this API has projects, issues, cycles - here's how they relate"). No query patterns ("always filter by team_id first"). No critical rules ("rate limit is 100/min, batch operations where possible").

**Gap:** Every MCP session starts cold. The agent wastes tokens discovering what the API is about, which tools matter, and how they relate. The research brief from Phase 1 already contains this knowledge but it never flows into the generated MCP server.

### Rule 4: Writing skills is a human skill

**PostHog's lesson:** Skills should contain only what an agent can't discover itself - idiosyncratic knowledge, edge cases, taste. "For retention events, use $pageview by default" - that's a PostHog Certified opinion that prevents agents from producing misleading analysis.

**PP status: Mixed.** The NOI (Non-Obvious Insight) framework does embed taste - "Discord's real value is searchable knowledge base, not just message sending." But this taste lives in the research brief and README, not in the MCP tool descriptions or a generated skills file. An agent connecting via MCP has no access to the NOI insights or domain opinions.

**Gap:** The research brief is rich with domain opinions and edge cases, but they're stranded in markdown files that MCP-connected agents never see. PostHog's approach of embedding opinions directly in tool descriptions and skill files would make printed CLIs meaningfully smarter for agents.

### Rule 5: Treat agents like real users

**PostHog's lesson:** Dogfood headlessly (CLI before UI). Do weekly trace reviews. Build evals from real agent sessions - both good and bad behaviors.

**PP status: Partial.** The machine has dogfood (structural validation), verify (runtime testing), and scorecard (18-dimension quality assessment). The retro system analyzes generation runs for machine improvements. But there's no mechanism to observe how agents USE the generated CLIs after shipping.

**Gap:** PP validates that the CLI builds and responds correctly, but never validates that agents can accomplish real tasks with it. The scorecard checks that --json flag exists but not that an agent can chain three commands to answer "why did signups drop?" No session tracing, no feedback mechanism, no eval loop on agent usage.

## Scope Boundaries

- This plan covers improvements to the Printing Press machine (templates, generator, skills)
- All changes compound across every future printed CLI
- Out of scope: retroactively updating already-published CLIs (espn, linear)
- Out of scope: building a telemetry backend or analytics dashboard
- Out of scope: changing the CLI side (Cobra commands) - focus is on MCP quality and agent context

## Key Technical Decisions

- **Enrich MCP templates rather than build a separate agent layer**: The MCP server template is the right place to add semantic tools, context, and composition hints. No new binaries needed.

- **Flow research brief into generated code**: The Phase 1 research already captures domain taxonomy, critical rules, and edge cases. The generator should extract structured data from research and embed it in the MCP server's context tools.

- **SQL/search as MCP tools**: PostHog's biggest win was exposing SQL. PP already has SQLite + FTS5. Exposing `sql` and `search` as MCP tools is the single highest-leverage change.

- **Lightweight agent feedback over telemetry**: Rather than building a telemetry system, add a `report` MCP tool that writes structured issue files locally. The retro system can then analyze them.

## Implementation Units

- [ ] **Unit 1: Expose high-level CLI commands as MCP tools**

**Goal:** Bridge the CLI/MCP parity gap. Make sync, search, sql, and workflow commands (stale, health, similar, orphans) available as MCP tools, not just CLI commands.

**Requirements:** Rule 1 (let agents do everything), Rule 2 (right abstraction level)

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/mcp_tools.go.tmpl`
- Modify: `internal/generator/generator.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add a second section to the MCP tools template that registers high-level tools alongside the endpoint tools
- `query_sql` - accepts SQL string, runs against local SQLite store, returns JSON results
- `search` - accepts query string + optional resource filter, runs FTS5 search, returns matches
- `sync` - triggers data sync for specified resources (or all)
- Workflow tools registered dynamically based on domain archetype detection (same logic that generates CLI workflow commands)
- These tools should have rich descriptions that explain what they're for, not just what they do

**Patterns to follow:**
- Existing `makeAPIHandler` pattern in mcp_tools.go.tmpl for tool registration
- Existing domain archetype detection in generator.go for conditional workflow tool registration

**Test scenarios:**
- Happy path: Generated MCP server includes query_sql, search, sync tools when data layer is enabled
- Happy path: Workflow tools (stale, health) registered when domain archetype matches
- Edge case: MCP server with no data layer still works (sql/search tools omitted gracefully)
- Edge case: Tool names don't collide with endpoint-derived tool names

**Verification:**
- A generated MCP server exposes sql, search, and sync tools alongside endpoint tools
- An agent can run a SQL query through the MCP without touching the CLI

---

- [ ] **Unit 2: Front-load domain context in MCP server**

**Goal:** Give MCP-connected agents immediate understanding of the API domain, taxonomy, critical rules, and tool composition patterns - without wasting tokens on discovery.

**Requirements:** Rule 3 (front-load universal context)

**Dependencies:** Unit 1 (high-level tools should exist before we describe them)

**Files:**
- Modify: `internal/generator/templates/mcp_tools.go.tmpl`
- Modify: `internal/generator/generator.go`
- Modify: `internal/pipeline/research.go` (to extract structured context from research brief)
- Test: `internal/generator/generator_test.go`

**Approach:**
- Replace the thin `about` tool with a rich `context` tool (or enhance `about`) that returns structured domain knowledge
- Content comes from two sources:
  1. **Spec-derived**: resource taxonomy (what entities exist, how they relate), auth requirements, rate limits, pagination patterns
  2. **Research-derived**: domain opinions, critical querying rules, common workflow patterns, edge cases
- The generator should accept a structured "domain context" object (extracted from the research brief during generation) and embed it in the MCP server
- Context is returned as structured JSON, not a wall of text - agents can parse and use what they need
- Keep it concise: aim for under 2K tokens of context, not a novel

**Patterns to follow:**
- PostHog's approach: taxonomy + SQL syntax + critical rules, loaded at session start
- Existing `aboutDescription` function in mcp_tools.go.tmpl

**Test scenarios:**
- Happy path: Generated MCP server's context tool returns API taxonomy, auth info, rate limit guidance
- Happy path: Domain opinions from research brief appear in context output
- Edge case: API with no research brief still generates useful context from spec alone
- Edge case: Context stays under 2K tokens even for large APIs (50+ resources)

**Verification:**
- An agent connecting to the MCP server can call one tool and understand the API's domain model, key constraints, and recommended query patterns

---

- [ ] **Unit 3: Enrich MCP tool descriptions with agent-useful metadata**

**Goal:** Transform thin endpoint descriptions into rich, agent-useful tool documentation that includes composition hints, expected output shape, and usage guidance.

**Requirements:** Rule 4 (skills are human knowledge)

**Dependencies:** None (can run parallel with Units 1-2)

**Files:**
- Modify: `internal/generator/templates/mcp_tools.go.tmpl`
- Modify: `internal/generator/generator.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Enhance the tool description template to include:
  - What the tool returns (shape hint: "Returns array of {id, name, status, assignee}")
  - Common composition patterns ("Use with issues_list to find issues, then issues_get for detail")
  - Critical constraints ("Rate limited to 100/min. Prefer batch endpoints for bulk operations")
  - When NOT to use it ("For searching across resources, use the search tool instead of iterating list endpoints")
- Keep descriptions concise - agents benefit from dense, factual context, not verbose prose
- Generate these enrichments from OpenAPI response schemas and the research brief's domain knowledge
- Cap description length to avoid bloating the tool list (PostHog found their v1 descriptions ate too many tokens)

**Patterns to follow:**
- PostHog's query-retention.md style: one-line opinions that prevent common mistakes
- Existing `oneline()` filter in generator.go for description formatting

**Test scenarios:**
- Happy path: Tool descriptions include return shape hints derived from response schema
- Happy path: Related tools cross-referenced in descriptions
- Edge case: Tools with no response schema still get useful descriptions
- Edge case: Total description token count stays reasonable (under 500 tokens per tool)

**Verification:**
- MCP tool descriptions are meaningfully richer than the raw OpenAPI endpoint summary
- An agent reading tool descriptions can make informed choices without trial-and-error

---

- [ ] **Unit 4: Add agent_mcp_quality scorecard dimension**

**Goal:** Score the quality of the generated MCP server, not just the CLI. Validate that MCP tools are rich, composable, and agent-usable.

**Requirements:** Rule 5 (treat agents like real users)

**Dependencies:** Units 1-3 (need the enrichments to exist before scoring them)

**Files:**
- Modify: `internal/pipeline/scorecard.go`
- Test: `internal/pipeline/scorecard_test.go`

**Approach:**
- Add a new Tier 1 dimension: `mcp_quality` (replaces or supplements `agent_native`)
- Check for:
  - High-level tools present (sql, search, sync) when data layer exists
  - Context/about tool returns structured domain knowledge
  - Tool descriptions include return shape hints
  - Tool descriptions include composition/cross-reference hints
  - No empty description strings
  - Total tool description token estimate is reasonable (not bloated, not skeletal)
- Score 0-10 like other dimensions
- This is still static analysis (code pattern matching), not dynamic agent testing

**Patterns to follow:**
- Existing scorecard dimension pattern in scorecard.go (presence checks + quality heuristics)

**Test scenarios:**
- Happy path: MCP server with all enrichments scores 8-10
- Happy path: MCP server with only endpoint tools (no sql/search) scores lower
- Edge case: MCP server without data layer doesn't get penalized for missing sql/search
- Error path: Malformed mcp/tools.go file doesn't crash scorer

**Verification:**
- `printing-press scorecard` reports mcp_quality as a scored dimension
- Gap report identifies specific MCP quality improvements when score is low

---

- [ ] **Unit 5: Generate agent playbook from research brief**

**Goal:** Produce a generated skills/playbook file that MCP-connected agents can request, containing domain opinions, edge cases, and workflow recipes - the "idiosyncratic knowledge" PostHog says only humans can provide.

**Requirements:** Rule 4 (skills are human knowledge), Rule 3 (front-load context)

**Dependencies:** Unit 2 (context tool should exist to serve the playbook)

**Files:**
- Modify: `internal/generator/generator.go`
- Create: `internal/generator/templates/agent_playbook.go.tmpl`
- Test: `internal/generator/generator_test.go`

**Approach:**
- During generation, extract from the research brief:
  - NOI rationale ("Discord's real value is searchable knowledge base")
  - Domain edge cases ("For retention, use pageview events, not sign-in events")
  - Common workflow sequences ("To investigate a spike: sync -> sql query -> drill into specific events")
  - Things agents get wrong ("Don't paginate through all results when search would work")
- Generate a structured playbook as a Go-embedded resource
- Expose via MCP as a `get_playbook` tool or include in the context tool response
- The playbook is generated once at build time, not dynamically - it's the research brief's domain knowledge crystallized into agent-consumable form

**Patterns to follow:**
- PostHog's skill files: one-liner opinions, not step-by-step manuals
- Existing README.tmpl for extracting research brief content into generated output

**Test scenarios:**
- Happy path: Generated playbook contains NOI rationale and domain edge cases from research brief
- Happy path: Playbook is accessible via MCP tool call
- Edge case: CLI generated without research brief produces a minimal playbook from spec alone
- Edge case: Playbook stays concise (under 3K tokens) even for complex APIs

**Verification:**
- An agent can request the playbook and receive actionable domain knowledge that wasn't obvious from tool descriptions alone

## System-Wide Impact

- **Interaction graph:** Changes to mcp_tools.go.tmpl affect every future printed MCP server. The generator pipeline flows: spec + research -> generator.go -> templates -> output. Changes are additive, not breaking.
- **Error propagation:** New MCP tools (sql, search) that fail should return structured errors consistent with existing endpoint tools. No new error categories needed.
- **State lifecycle risks:** The sql/search MCP tools depend on local SQLite state from sync. If sync hasn't run, these tools should return a clear "no data synced yet, run sync first" message, not cryptic errors.
- **API surface parity:** This plan intentionally narrows the CLI/MCP parity gap. After implementation, MCP agents should be able to do ~80% of what CLI agents can (the remaining 20% being interactive CLI features like progress bars).
- **Unchanged invariants:** The CLI interface (Cobra commands, flags, output formats) is unchanged. The MCP endpoint-derived tools are unchanged. All changes are additive MCP tools and enriched descriptions.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| MCP tool descriptions bloat token usage | Cap per-tool description length. PostHog found this was a real problem. Monitor total token estimate in scorecard. |
| Research brief extraction is fragile (unstructured markdown -> structured data) | Define a structured "domain context" schema. Generator extracts what it can, falls back to spec-only context gracefully. |
| sql/search MCP tools expose local data that may be stale | Always include sync timestamp in sql/search results. Agents can decide whether to re-sync. |
| Playbook quality varies with research brief quality | Playbook is a best-effort enrichment. Scorecard checks for its presence but doesn't fail generation if research was thin. |

## What PP Already Gets Right (PostHog Would Approve)

For the record, these are areas where PP already embodies PostHog's rules:

1. **Dual interface** (CLI + MCP) from one spec - PostHog's Rule 1
2. **Agent-native flags** (--json, --compact, --stdin, --select, typed exit codes) - Rule 1
3. **Local data layer** (SQLite + FTS5 + sql command) - Rule 2's abstraction principle
4. **NOI framework** embeds domain taste - Rule 4
5. **Dogfood/verify/scorecard** pipeline - Rule 5's quality loop
6. **Retro system** feeds learnings back into the machine - Rule 5's eval loop
7. **Reference files loaded on-demand** in skill instructions - Rule 3's "defer the rest"

The improvements in this plan compound on these strengths rather than rebuilding.

## Sources & References

- PostHog blog post: "The golden rules of agent-first product engineering" (2026-04-09, @posthog on X)
- Existing MCP template: `internal/generator/templates/mcp_tools.go.tmpl`
- Existing scorecard: `internal/pipeline/scorecard.go`
- Existing research pipeline: `internal/pipeline/research.go`
- Prior agent-native plan: `docs/plans/2026-03-25-feat-agent-native-cli-audit-and-improvements-plan.md`
- Prior MCP readiness plan: `docs/plans/2026-04-05-001-feat-mcp-readiness-layer-plan.md`
