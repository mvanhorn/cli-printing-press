# Phase 1.5 Steps 1.5a–1.5c: Search, Read Source, Catalog & Transcendence

Lazy-loaded body for the ecosystem-search and absorb-manifest authoring steps. Read and apply after the browser-sniff-gate marker pre-flight check passes, before Step 1.5c.5 (always-spawn novel-features subagent).

### Step 1.5a: Search for every tool that touches this API

Run these searches in parallel:

1. **WebSearch**: `"<API name>" Claude Code plugin site:github.com`
2. **WebSearch**: `"<API name>" MCP server model context protocol`
3. **WebSearch**: `"<API name>" Claude skill SKILL.md site:github.com`
4. **WebSearch**: `"<API name>" CLI tool site:github.com` (competing CLIs)
5. **WebSearch**: `"<API name>" CLI site:npmjs.com` (npm packages)
6. **Raw fetch**: Check `github.com/anthropics/claude-plugins-official/tree/main/external_plugins` for official plugins with the helper from [references/fetch-docs.md](references/fetch-docs.md), or with `gh api` when it can return the file/listing directly.
7. **WebSearch**: `"<API name>" MCP site:lobehub.com OR site:mcpmarket.com OR site:fastmcp.me`
8. **WebSearch**: `"<API name>" automation script workflow site:github.com`
9. **WebSearch**: `"<API name>" SDK wrapper site:npmjs.com`
10. **WebSearch**: `"<API name>" client library site:pypi.org`

### Step 1.5a.5: Read MCP source code (if found)

If step 1.5a discovered MCP server repos with public source code on GitHub, read the actual source to extract ground-truth API usage — not just README feature descriptions.

**Time budget:** Max 3 minutes total. If extraction is unproductive, fall back to README-only research.

**For the top 1-2 MCP repos found:**

1. **Identify the main source file.** Use `gh api`, raw GitHub URLs, or the helper from [references/fetch-docs.md](references/fetch-docs.md) to inspect the repo tree and source files without a summarization layer. Find the entry point — typically `src/index.ts`, `server.ts`, `server.py`, `main.go`, or a `tools/` directory. MCP servers are usually small (one main file + tool definitions).

2. **Extract three things:**
   - **API endpoint paths**: Look for HTTP client calls (`fetch(`, `axios.`, `requests.`, `http.Get`, `client.`) and extract the URL paths (e.g., `GET /v1/issues`, `POST /graphql`). These are the endpoints the MCP maintainer proved work.
   - **Auth patterns**: Look for how the MCP constructs auth headers — token format (`Bearer`, `Bot`, `Basic`), header name (`Authorization`, `X-API-Key`), environment variable names. This informs our auth setup guidance.
   - **Response field selections**: Look for which fields are extracted from API responses — these are the high-gravity fields that power users actually need.

3. **Feed into absorb manifest.** In step 1.5b, endpoints extracted from source get attributed as `<MCP name> (source)` in the "Best Source" column, distinguishing them from README-derived features. Source-extracted endpoints are high-confidence signals — the maintainer verified they work.

4. **Feed auth patterns into research brief.** If the MCP source reveals token format (e.g., `xoxp-` for Slack, `sk_live_` for Stripe), credential setup steps, or required scopes, note them in the Phase 1 brief's auth section. These hints improve the generated CLI's auth onboarding.

**Skip this step when:**
- No MCP repos were found in 1.5a
- MCP repos are private or archived
- The MCP is a monorepo where the relevant server is hard to locate within 3 minutes

### Step 1.5a.6: DeepWiki Codebase Analysis (if GitHub repos found)

If Phase 1 or Step 1.5a discovered GitHub repos for the API (SDK repos, server repos, MCP server repos), query DeepWiki for a semantic understanding of how the API works - architecture, auth flows, data models, error handling. This complements crowd-sniff (endpoints) and MCP source reading (auth headers) with "how things actually work" context.

**Time budget:** 2 minutes max. If DeepWiki is slow or unavailable, skip silently.

**Run in parallel** with Steps 1.5a through 1.5a.5 when possible. DeepWiki queries do not depend on MCP source reading results.

Read and follow [references/deepwiki-research.md](references/deepwiki-research.md) for the query procedure: wiki structure fetch, targeted section extraction (auth, data model, architecture), and synthesis into the research brief and absorb manifest.

**Skip this step when:**
- No GitHub repos were discovered during Phase 1 or Step 1.5a
- The API is trivially simple (1-2 endpoints, no auth)

### Step 1.5b: Catalog every feature into the absorb manifest

For EACH tool found, list EVERY feature/tool/command it provides. Then define how our CLI matches AND beats it:

```markdown
## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-----------|-------------------|-------------|
| 1 | Search issues by text | Linear MCP search_issues | <api>-pp-cli search | Works offline, regex, SQL composable |
| 2 | Create issue | Linear MCP create_issue | <api>-pp-cli issue create --stdin --dry-run | Agent-native, scriptable, idempotent |
| 3 | Sprint board view | jira-cli sprint view | <api>-pp-cli sprint view | Historical velocity, offline |
```

Every row = a feature we MUST build. No exceptions. If someone else has it, we have it AND it works offline, with --json, --dry-run, typed exit codes, and SQLite persistence.

SDK wrapper methods should be treated as features to absorb — each public method/function is a feature the CLI should match.

**Our Implementation must start with a parseable disposition.** Use one of these prefixes so Phase 3 can verify the row mechanically:
- `<api>-pp-cli <clean command path>` for a promoted or hand-built Cobra command path that must resolve via `<binary> <path> --help`.
- `(generated endpoint) <resource> <endpoint>` for generator-emitted typed endpoint commands that retain the upstream resource shape and are covered by the generated endpoint surface.
- `(behavior in <api>-pp-cli <command path>) ...` for features implemented as flags, modes, output shapes, or store behavior inside another command. The named command path still must resolve; the prose after the closing parenthesis explains the behavior to verify later.
- `(stub) ...` only for explicitly approved stubs per the rule below.

Do not leave `Our Implementation` as freeform prose like `FTS5 offline search` or `SQLite-backed sprint query`. If the row maps to a clean user-facing command, put that command path first. If it does not, choose the explicit disposition that explains why Phase 3 should not treat the whole cell as a new command path.

**Stubs must be explicit.** If any row in the manifest will ship as a stub (placeholder implementation that emits "not yet wired" / "wip" messaging), start `Our Implementation` with `(stub)` plus a one-line reason why the full implementation is deferred (e.g., "(stub - requires paid API)", "(stub - requires headless Chrome)"). If the manifest also has a `Status` column, set that value to `(stub)` too, but the `Our Implementation` prefix is the Phase 3 gate's source of truth. Do NOT quietly ship stubs for features the user approved as shipping scope.

The Phase Gate 1.5 prose showcase (below) MUST read out stub items separately so the user explicitly approves the stub list. After approval, Phase 3 builds shipping-scope features fully and stubs with honest messaging; no mid-build downgrade from shipping-scope to stub is permitted. If an agent discovers during Phase 3 that a shipping-scope feature cannot be implemented in-session, they must return to Phase 1.5 with a revised manifest — not unilaterally downgrade to a stub.

### Step 1.5c: Identify transcendence features

Start with the users, not the technology. The best features come from understanding
who uses this service, what their rituals are, and what questions they can't answer
today. "What can SQLite do?" is the wrong question. "What would make a power user
say 'I need this'?" is the right one.

The actual brainstorming runs as a Task subagent in Step 1.5c.5 below — customer
model → 2× candidates → adversarial cut. Step 1.5c is the motivation; do not
generate transcendence features inline here.

The transcendence table in the manifest (Step 1.5d) renders rows in this shape,
which mirrors the subagent's `### Survivors` output. The `Buildability` column
tags each row `spec-emits` or `hand-code` per
[references/novel-features-subagent.md](references/novel-features-subagent.md)
so the Phase Gate 1.5 hand-code count has a source of truth in the manifest.
The optional `Long Description` column carries agent-facing disambiguation
text for Phase 3 Cobra `Long` fields; use `none` when no sibling redirect is
needed:

```markdown
### Transcendence (only possible with our approach)
| # | Feature | Command | Buildability | Why Only We Can Do This | Long Description |
|---|---------|---------|--------------|------------------------|------------------|
| 1 | Bottleneck detection | bottleneck | hand-code | Requires local join across issues + assignees + cycle data | Use this command to find cross-team work blockage. Do NOT use it for personal recency checks; use 'since' instead. |
| 2 | Velocity trends | velocity --weeks 4 | hand-code | Requires historical cycle snapshots in SQLite | none |
| 3 | What did I miss | since 2h | hand-code | Requires time-windowed aggregation no single API call provides | Use this command for recent personal changes. Do NOT use it for backlog bottlenecks; use 'bottleneck' instead. |
```

Minimum 5 transcendence features. These are the commands that differentiate the CLI.
