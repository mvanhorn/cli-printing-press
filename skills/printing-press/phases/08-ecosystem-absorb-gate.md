## 08-ecosystem-absorb-gate (Phase 1.5: Ecosystem Absorb Gate)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "08-ecosystem-absorb-gate"
```

THIS IS A MANDATORY STOP GATE. Do not generate until this is complete and approved.

### Pre-flight check: browser-sniff-gate marker

Before any absorb work, verify `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` exists and contains an entry for every source named in the briefing.

**If the file is missing:** HARD STOP. Print:

> Phase 1.7 Browser-Sniff Gate did not record a decision. Return to Phase 1.7 and evaluate the browser-sniff gate for every source named in the briefing.

Do not proceed to Step 1.5a until the file exists.

**If the file exists but is missing an entry for a named source:** HARD STOP. Print:

> Browser-Sniff Gate missing decision for source `<name>`. Return to Phase 1.7 and evaluate the decision matrix for that source.

Do not proceed until every briefing source has a marker entry.

**Resume leniency:** If the run was started by an older version of the skill that didn't write markers, warn and continue — do not hard-fail on legacy resumes. Distinguish by checking whether `state.json` predates the marker contract (the marker file didn't exist before 2026-04-11). New runs always hard-fail on a missing marker.

**Pre-check (existing):** If no spec or HAR file has been resolved by this point and [Phase 1.7](06-browser-sniff-gate.md) (Browser-Sniff Gate) was not evaluated, STOP. Go back and run the browser-sniff gate decision matrix. The absorb manifest depends on knowing the API surface, which requires a spec.

When any of the stop-gate checks at the top of this phase sends you back to Phase 1.7, record that discovery-rework handoff so the receipt gate accepts the re-entry rather than treating it as an out-of-order jump:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "08-ecosystem-absorb-gate" --next "06-browser-sniff-gate" --note "browser-sniff decision missing for <source>"
```

The GOAT CLI doesn't "find gaps." It absorbs EVERY feature from EVERY tool and then transcends with compound use cases nobody thought of. This phase builds the absorb manifest.

### Step 1.5a: Search for every tool that touches this API

Run these searches in parallel:

1. **WebSearch**: `"<API name>" Claude Code plugin site:github.com`
2. **WebSearch**: `"<API name>" MCP server model context protocol`
3. **WebSearch**: `"<API name>" Claude skill SKILL.md site:github.com`
4. **WebSearch**: `"<API name>" CLI tool site:github.com` (competing CLIs)
5. **WebSearch**: `"<API name>" CLI site:npmjs.com` (npm packages)
6. **Raw fetch**: Check `github.com/anthropics/claude-plugins-official/tree/main/external_plugins` for official plugins with the helper from [references/fetch-docs.md](../references/fetch-docs.md), or with `gh api` when it can return the file/listing directly.
7. **WebSearch**: `"<API name>" MCP site:lobehub.com OR site:mcpmarket.com OR site:fastmcp.me`
8. **WebSearch**: `"<API name>" automation script workflow site:github.com`
9. **WebSearch**: `"<API name>" SDK wrapper site:npmjs.com`
10. **WebSearch**: `"<API name>" client library site:pypi.org`

### Step 1.5a.5: Read MCP source code (if found)

If step 1.5a discovered MCP server repos with public source code on GitHub, read the actual source to extract ground-truth API usage — not just README feature descriptions.

**Time budget:** Max 3 minutes total. If extraction is unproductive, fall back to README-only research.

**For the top 1-2 MCP repos found:**

1. **Identify the main source file.** Use `gh api`, raw GitHub URLs, or the helper from [references/fetch-docs.md](../references/fetch-docs.md) to inspect the repo tree and source files without a summarization layer. Find the entry point — typically `src/index.ts`, `server.ts`, `server.py`, `main.go`, or a `tools/` directory. MCP servers are usually small (one main file + tool definitions).

2. **Extract three things:**
   - **API endpoint paths**: Look for HTTP client calls (`fetch(`, `axios.`, `requests.`, `http.Get`, `client.`) and extract the URL paths (e.g., `GET /v1/issues`, `POST /graphql`). These are the endpoints the MCP maintainer proved work.
   - **Auth patterns**: Look for how the MCP constructs auth headers — token format (`Bearer`, `Bot`, `Basic`), header name (`Authorization`, `X-API-Key`), environment variable names. This informs our auth setup guidance.
   - **Response field selections**: Look for which fields are extracted from API responses — these are the high-gravity fields that power users actually need.

3. **Feed into absorb manifest.** In step 1.5b, endpoints extracted from source get attributed as `<MCP name> (source)` in the "Best Source" column, distinguishing them from README-derived features. Source-extracted endpoints are high-confidence signals — the maintainer verified they work.

4. **Feed auth patterns into research brief.** If the MCP source reveals token format (e.g., `xoxp-` for Slack, `sk_live_` for Stripe), credential setup steps, or required scopes, note them in the [Phase 1](04-research-brief.md) brief's auth section. These hints improve the generated CLI's auth onboarding.

**Skip this step when:**
- No MCP repos were found in 1.5a
- MCP repos are private or archived
- The MCP is a monorepo where the relevant server is hard to locate within 3 minutes

### Step 1.5a.6: DeepWiki Codebase Analysis (if GitHub repos found)

If [Phase 1](04-research-brief.md) or Step 1.5a discovered GitHub repos for the API (SDK repos, server repos, MCP server repos), query DeepWiki for a semantic understanding of how the API works - architecture, auth flows, data models, error handling. This complements crowd-sniff (endpoints) and MCP source reading (auth headers) with "how things actually work" context.

**Time budget:** 2 minutes max. If DeepWiki is slow or unavailable, skip silently.

**Run in parallel** with Steps 1.5a through 1.5a.5 when possible. DeepWiki queries do not depend on MCP source reading results.

Read and follow [references/deepwiki-research.md](../references/deepwiki-research.md) for the query procedure: wiki structure fetch, targeted section extraction (auth, data model, architecture), and synthesis into the research brief and absorb manifest.

**Skip this step when:**
- No GitHub repos were discovered during [Phase 1](04-research-brief.md) or Step 1.5a
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

**Our Implementation must start with a parseable disposition.** Use one of these prefixes so [Phase 3](11-build-the-goat.md) can verify the row mechanically:
- `<api>-pp-cli <clean command path>` for a promoted or hand-built Cobra command path that must resolve via `<binary> <path> --help`.
- `(generated endpoint) <resource> <endpoint>` for generator-emitted typed endpoint commands that retain the upstream resource shape and are covered by the generated endpoint surface.
- `(behavior in <api>-pp-cli <command path>) ...` for features implemented as flags, modes, output shapes, or store behavior inside another command. The named command path still must resolve; the prose after the closing parenthesis explains the behavior to verify later.
- `(stub) ...` only for explicitly approved stubs per the rule below.

Do not leave `Our Implementation` as freeform prose like `FTS5 offline search` or `SQLite-backed sprint query`. If the row maps to a clean user-facing command, put that command path first. If it does not, choose the explicit disposition that explains why [Phase 3](11-build-the-goat.md) should not treat the whole cell as a new command path.

**Stubs must be explicit.** If any row in the manifest will ship as a stub (placeholder implementation that emits "not yet wired" / "wip" messaging), start `Our Implementation` with `(stub)` plus a one-line reason why the full implementation is deferred (e.g., "(stub - requires paid API)", "(stub - requires headless Chrome)"). If the manifest also has a `Status` column, set that value to `(stub)` too, but the `Our Implementation` prefix is the [Phase 3](11-build-the-goat.md) gate's source of truth. Do NOT quietly ship stubs for features the user approved as shipping scope.

The Phase Gate 1.5 prose showcase (below) MUST read out stub items separately so the user explicitly approves the stub list. After approval, [Phase 3](11-build-the-goat.md) builds shipping-scope features fully and stubs with honest messaging; no mid-build downgrade from shipping-scope to stub is permitted. If an agent discovers during [Phase 3](11-build-the-goat.md) that a shipping-scope feature cannot be implemented in-session, they must return to Phase 1.5 with a revised manifest — not unilaterally downgrade to a stub.

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
[references/novel-features-subagent.md](../references/novel-features-subagent.md)
so the Phase Gate 1.5 hand-code count has a source of truth in the manifest.
The optional `Long Description` column carries agent-facing disambiguation
text for [Phase 3](11-build-the-goat.md) Cobra `Long` fields; use `none` when no sibling redirect is
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

### Step 1.5c.5: Auto-Suggest Novel Features (subagent)

**Always spawn the subagent — first prints and reprints alike.** The subagent
is the only path that produces this step's outputs (customer model, candidate
list, adversarial cut, killed-candidate audit trail). There is no manual
fallback. Specifically, do not:

- hand-curate the transcendence list from a prior manifest, even when the
  prior looks complete. Prior `research.json` is INPUT to Pass 2(d), never
  a substitute for the spawn.
- fall back to inline brainstorming inside the SKILL.
- skip on cost grounds. With a strong prior the subagent confirms or
  reframes; with no prior it generates from scratch. Run it either way.
- treat disclosure as authorization. Announcing a skip in the gate showcase
  does not make the skip legal.

Read [references/novel-features-subagent.md](../references/novel-features-subagent.md)
for the prior-research discovery snippet, input bundle, prompt template, and
output contract. Run the discovery snippet as written — do not substitute an
`ls` of the manuscripts directory. The snippet's `none` branch (no prior
research) is a first print, not a skip signal.

The only legitimate non-spawn outcome is the pre-flight HALT (brief lacks
user research) defined in the reference file.

### Step 1.5d: Write the manifest artifact

Write to `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`

The manifest now includes compound use cases (Step 1.5c) and auto-suggested + auto-brainstormed features (Step 1.5c.5) in the transcendence table.

### Step 1.5e: Write research.json for README credits

After writing the absorb manifest, also write `$API_RUN_DIR/research.json` so the generator can credit community projects in the README. This file MUST match the `ResearchResult` JSON schema that `loadResearchSources()` expects.

Populate the `alternatives` array from the absorb manifest's source tools list. Include only tools that:
1. Have a GitHub URL (not npm/PyPI landing pages)
2. Actually contributed features to the absorb manifest
3. Are capped at 8 entries, ordered by number of absorbed features (then by stars)

```bash
cat > "$API_RUN_DIR/research.json" <<REOF
{
  "api_name": "<api>",
  "novelty_score": 0,
  "alternatives": [
    {"name": "<tool1>", "url": "<github-url>", "language": "<Go|JavaScript|Python|etc>", "stars": <N>, "command_count": <N>},
    ...
  ],
  "auth": {
    "canonical_env_var": "<CANONICAL_ENV_VAR, omit when unknown>"
  },
  "novel_features": [
    {
      "name": "<Feature Name>",
      "command": "<cli-subcommand>",
      "description": "<One sentence: what the user gets>",
      "rationale": "<One sentence: why only possible with our approach>",
      "example": "<ready-to-run invocation with realistic args, e.g. 'yahoo-finance-pp-cli portfolio perf --agent'>",
      "why_it_matters": "<One sentence aimed at AI agents: when should they reach for this?>",
      "group": "<Theme name clustering similar features, e.g. 'Local state that compounds'>"
    },
    ...
  ],
  "narrative": {
    "display_name": "<Canonical prose name, exact brand casing/spaces, e.g. Product Hunt, GitHub, YouTube, Cal.com>",
    "headline": "<Bold one-sentence value prop: what makes this CLI worth using>",
    "value_prop": "<2-3 sentence expansion rendered beneath the title>",
    "auth_narrative": "<API-specific auth story; omit for simple API-key auth>",
    "quickstart": [
      {"command": "<cli> <real-command-with-real-args>", "comment": "<why this comes first>"},
      ...
    ],
    "troubleshoots": [
      {"symptom": "<user-visible error or symptom>", "fix": "<actionable one-liner>"},
      ...
    ],
    "when_to_use": "<2-4 sentences describing ideal use cases; rendered in SKILL.md only>",
    "anti_triggers": ["<task boundary this CLI should not handle>", ...],
    "recipes": [
      {"title": "<Recipe name>", "command": "<cli> <invocation>", "explanation": "<one-line paragraph>"},
      ...
    ],
    "trigger_phrases": ["<natural phrase that should invoke this CLI's skill>", ...]
  },
  "gaps": [],
  "patterns": [],
  "recommendation": "proceed",
  "researched_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
REOF
```

For each tool, fill in what you know from the research. Stars and command_count are optional (use 0 if unknown). The `language` field should match the primary implementation language. Skip tools that were found during search but contributed zero features to the manifest.

**Novel features rules** (the `novel_features` array populates the README's "Unique Features" section and SKILL.md's "Unique Capabilities" block; MCP exposure comes from the runtime Cobra-tree mirror, not this list):
1. Include all transcendence features from the manifest that scored >= 5/10. Order by score descending.
2. `description` should be user-benefit language, not implementation detail. Good: "See which team members are overloaded before sprint planning." Bad: "Requires local join across issues + assignees + cycle data."
3. `rationale` should explain why this is only possible with our approach. Good: "Requires correlating bookings, schedules, and staff data that only exists together in the local store." Bad: "Cal.com Insights is paid-tier only."
4. `command` must match the actual CLI subcommand that will be built in [Phase 3](11-build-the-goat.md). For subcommands of a resource (e.g., `issues stale`), use the full command path. The first segment must not collide with an active framework cobra command this CLI emits (`recall`, `teach`, `learnings`, `version`, `doctor`, `search`, `sync`, and the rest of the generated root built-ins). The generator skips those root stubs so they cannot shadow the framework command; nest under a non-reserved parent or pick another verb.
5. `example` is a ready-to-run invocation an agent can copy-paste. Use realistic arguments from the API's domain (e.g. `AAPL`, `customer_42`), not `<placeholder>`. Include the `--agent` flag when the feature benefits from structured output.
6. `why_it_matters` is a single agent-facing sentence answering "when should I pick this over a generic API call?"
7. `group` clusters related features under a theme name. Pick 2–5 themes total (e.g. "Local state that compounds", "Agent-native plumbing", "Reachability mitigation"). Use the same `group` string verbatim across features that belong together — exact matches drive README grouping. Leave `group` empty if the CLI has too few novel features to warrant clustering.
8. If the manifest row has a non-`none` `Long Description`, keep that text with the feature implementation notes and use it as the Cobra `Long` field during [Phase 3](11-build-the-goat.md) hand-code. Do not squeeze redirect prose into `description`; `description` stays one-line user-benefit text.
9. If no transcendence features scored >= 5/10, omit the `novel_features` field entirely.
10. Do not add a feature to `novel_features` merely to expose it through MCP. Any user-facing Cobra command becomes an MCP tool automatically unless it sets `cmd.Annotations["mcp:hidden"] = "true"`.

**Auth research rule**:
1. `auth.canonical_env_var` is the single-token credential env var discovered from vendor docs, MCP/source analysis, or dominant SDK/CLI convention (for example `APIFY_TOKEN`, `GITHUB_TOKEN`, `STRIPE_SECRET_KEY`). Omit it when no canonical name is known, when auth is HTTP Basic or another credential pair, or when the auth flow needs richer metadata. Fresh generation reads this env var first and keeps the parser-derived name as a fallback automatically.

**Narrative rules** (the `narrative` object drives README headline, Quick Start, Auth, Troubleshooting, and the entire SKILL.md):
1. `display_name` is the canonical prose name, discovered during research, with exact brand casing and spacing. This is agentic/research-owned, not slug-inferred by Go code. Good: "Product Hunt", "GitHub", "YouTube", "Cal.com". Bad: "Producthunt", "Github", "Youtube", "Cal Com". Use the slug only for binary names, directories, module paths, config paths, and env-var prefixes.
2. `headline` is the bold one-liner rendered beneath the CLI title. Should name the differentiator, not restate the API. Good: "Every Notion feature, plus sync, search, and a local database no other Notion tool has." Bad: "A CLI for the Notion API."
3. `value_prop` expands the headline to 2–3 sentences. Name specific novel features by command where helpful.
4. `auth_narrative` tells the real auth story for this API (crumb handshake, cookie session, OAuth device flow). Omit for standard API-key auth where the generic branch is fine.
5. `quickstart` is a 3–6 step flow using REAL arguments (symbols, IDs, resource names an agent can actually pass). Each step's `comment` explains *why* it runs. This replaces the generic "resource list" first-command fallback.
   - Step 1 of `quickstart` should usually be verify-safe: it should exit 0 when `validate-narrative --full-examples` appends `--dry-run` in a no-credentials environment.
   - Use `<cli> doctor --dry-run` as step 1 (health check, works without auth). Do not use `<cli> auth set-token` as step 1 because it reads a token from stdin and is not a verify-safe runnable first step. Auth setup instructions belong in `auth_narrative` prose only, not as an executable quickstart command.
6. `troubleshoots` captures API-specific failure modes (rate-limit mitigation, cookie expiry, paginated quirks). Each `fix` must be actionable — a command or a concrete setting change.
7. `when_to_use` is SKILL-only narrative. 2–4 sentences describing the kinds of agent tasks this CLI is the right choice for. Not rendered in README.
8. `anti_triggers` is SKILL-only narrative. List common task boundaries that should make an agent choose another tool, official SDK, web UI, or human workflow instead of this CLI. Write concrete "do not use this CLI for X" cases, not vague limitations. Omit the field only when no honest boundary is known.
9. `recipes` are 3–5 worked examples rendered in SKILL.md. Each has a title, a real command, and a one-line explanation. Prefer recipes that exercise novel features. **At least one recipe must pair `--agent` with `--select`** — using dotted paths (e.g. `--select events.shortName,events.competitions.competitors.team.displayName`) when the response is deeply nested. APIs like ESPN, HubSpot, and Linear return tens of KB per call; without a `--select` recipe, agents burn context parsing verbose payloads. Pick a command known to return a large or deeply nested response and show the narrowing pattern. **Regex literals must double-escape backslashes** — write `\\b` not `\b` (and `\\t`, `\\f`, etc.) inside any `command`, `fix`, or other JSON string field. JSON parses `\b` as backspace (0x08), `\f` as form feed (0x0C), and so on, which then leak into the rendered SKILL.md as control bytes that render as nothing in most viewers. The generator's render-time scanner rejects these with a clear offset; double-escape from the start to avoid the error.
10. `trigger_phrases` are natural-language phrases a user might say that should invoke this CLI's skill. Include 3–5 domain-specific phrases (e.g. for a finance CLI: "quote AAPL", "check my portfolio", "options for TSLA") and 2 generic phrases ("use <api-name>", "run <api-name>"). Domain verbs vary — don't just template "use X" variants.
11. All `narrative` fields are optional. Omit fields you can't populate honestly rather than emit filler. The generator falls back to generic content gracefully.
12. **Avoid hardcoded counts in narrative copy when the count tracks a runtime list.** A number embedded in `headline` or `value_prop` ("across N trusted sources", "from N retailers", "queries N vendors") propagates into root.go's Short/Long, the README, the SKILL, the MCP tools description, and `which.go` — every output surface that reads the narrative. When the underlying registry grows or shrinks, the count goes stale across all of those surfaces simultaneously, and a single-line edit to add a source requires hunting down ~10 hardcoded copies. Prefer plural-without-count phrasing ("across the major sources", "from a curated set of retailers") or describe the breadth qualitatively ("dozens of vendors") rather than committing to a specific integer. If a count is load-bearing for the value prop, keep the brief's narrative count-free and have the printed-CLI's README/SKILL author write the count once into a single hand-edited paragraph after generation — accepting that it will need a manual update whenever the registry changes.
13. **Use side-effectful examples only when they are the truthful workflow.** `validate-narrative --strict --full-examples` classifies `auth login`, `auth set-token`, `auth logout`, `auth setup`, `--launch`, and mutating `--apply` examples as side-effectful (see `isSideEffectfulNarrativeExample` in `internal/narrativecheck/narrativecheck.go`) and reports each as an `UNSUPPORTED` warning instead of executing it. These warnings do not fail strict aggregation, so it is valid to show an auth or apply command when that is the honest onboarding or bulk-operation shape. Prefer `doctor` or another read-only invocation as `quickstart[0]` when it teaches the same workflow, but do not strip a real auth or apply step just to appease shipcheck. Non-side-effect unsupported examples still fail strict mode when they cannot dry-run, and missing commands, empty command paths, and failed full examples remain failures.

**Pre-render framework-command check.** Before running `generate --research-dir`,
validate the framework command examples already present in `research.json`.
This catches stable template vocabulary mistakes while the fix is still a
single-file `research.json` edit, before README.md, SKILL.md, `.printing-press.json`,
root help, and other generated surfaces consume the bad narrative.

```bash
cli-printing-press validate-narrative --strict --framework-only \
  --research "$API_RUN_DIR/research.json"
```

If this reports `sync --entities`, `search --entities`, `search --types`, an
absolute date for `sync --since`, or another framework-command flag mismatch,
fix `research.json` now and rerun this check before generation. This is a
cheap floor, not a replacement for [Phase 4](12-shipcheck.md) shipcheck: after the CLI exists,
`shipcheck` still runs `validate-narrative --strict --full-examples` against
the built binary to catch API-specific command paths, generated endpoint flags,
and runtime dry-run failures.

Also write discovery pages if browser-sniff was used. The generator reads these from `$API_RUN_DIR/discovery/browser-sniff-report.md` (which the browser-sniff gate already writes there). No additional action needed for discovery pages -- they are already in the right location.

### Priority inversion check (combo CLIs only)

**Only runs when `source-priority.json` exists from the Multi-Source Priority Gate in [references/run-resolution.md](../references/run-resolution.md).**

Before Phase Gate 1.5, tally the commands/features the manifest attributes to each named source. Compare against the confirmed priority ordering:

- If the primary source has **fewer** commands than any secondary source, this is a **priority inversion** — the free/primary-intent source got demoted because the secondary had more spec coverage.
- If the primary source has **zero** commands (all its features were dropped because it lacked a spec), this is a **hard inversion** — the primary was silently replaced.

When an inversion is detected, HALT before Phase Gate 1.5 and print:

> ⚠ **Priority inversion detected.**
>
> The confirmed primary is **<Source A>** but the manifest gives it <N> commands vs **<Source B>** (secondary) with <M> commands. This usually means the primary's discovery path (browser-sniff, community wrapper, HTML parser) didn't land, and the secondary's clean spec took over.
>
> The user said <Source A> is the headline. Shipping this manifest would invert their stated priority.

Then ask via `AskUserQuestion`:

1. **Re-run discovery for <Source A>** — loop back to [Phase 1.7](06-browser-sniff-gate.md) browser-sniff or [Phase 1.8](07-crowd-sniff-gate.md) crowd-sniff for the primary source specifically. Record this discovery-rework handoff before re-entering the gate — for the browser-sniff loop use the receipt block after the stop-gate checks at the top of this phase; for the crowd-sniff loop:
2. **Accept the inversion** — the user explicitly confirms they're fine with the secondary leading. Record this in `source-priority.json` as `inversion_accepted: true`.
3. **Drop <Source B>** — remove the secondary from the manifest so it can't overshadow the primary.

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "08-ecosystem-absorb-gate" --next "07-crowd-sniff-gate" --note "crowd-sniff rework for <source>"
```

Do not proceed to the prose showcase until this is resolved.

### Phase Gate 1.5

**STOP.** Present the absorb manifest to the user in two parts: a prose showcase, then a question.

The prose showcase and the `AskUserQuestion` are two separate turns. Print the showcase as a plain text reply with every novel feature spelled out, then call `AskUserQuestion` with four short options whose descriptions fit on one line each. The question text is one sentence; the user reads the showcase to decide and the options to act. Cramming the feature list into an option description collapses both turns into one and is the failure mode this gate exists to prevent.

**Part 1: Prose showcase (print before the AskUserQuestion)**

The showcase exists so the user can decide approve / trim / add ideas without asking a follow-up. Cover four things:

1. **Scope** — how many features absorbed across which tools, how many novel on top, how that stacks up against the best existing tool.
2. **Per-novel-feature readout** — one line each: feature name, what the user gets, and the specific evidence or persona that makes it worth building.
3. **Hand-code commitment** — of the M novel features, K will require hand-written Go after generate (each ~50-150 LoC plus `root.go` wiring). State the hand-code count and the auto-emitted count, then list the names of the hand-code features. The manifest transcendence table's `Buildability` column (populated from the subagent per [references/novel-features-subagent.md](../references/novel-features-subagent.md) "Output contract") is the source of truth: count rows tagged `hand-code`; `spec-emits` rows are excluded from the hand-code total. Approving commits the agent to that scope, so the user must see it explicitly before the AskUserQuestion.
4. **Anything else the user should worry about before approving** — stubs, risky dependencies, expensive endpoints, low-confidence ideas.

Show every novel feature that scored ≥5/10. Group by theme if there are more than ~12; never hide features behind "Plus N more" or "see full manifest." If zero qualified, say so plainly: "No novel features scored high enough to recommend. The absorbed features cover the landscape well."

Format is otherwise yours — markdown headings, prose, a numbered list, whatever reads cleanly. The must-haves are the four things above and the ≥5/10 coverage rule.

**Part 2: AskUserQuestion**

> "Ready to generate with the full [N+M]-feature manifest? Or do you have ideas to add?"

Options (each description must be one short line — the showcase already did the explaining):
1. **Approve — generate now** — Start CLI generation with the full manifest
2. **I have ideas to add** — Tell me features from your experience, then we'll generate
3. **Review full manifest** — Show me every absorbed and novel feature before deciding
4. **Trim scope** — The feature count is too ambitious, let's focus on a subset

If user selects **"I have ideas to add"**, ask 3 structured questions targeting personal knowledge the research couldn't surface:

1. "Beyond the [M] ideas above, what workflows do YOU use `<API>` for that we might have missed?"
2. "What frustrates YOU about this API that the research didn't surface?"
3. "What's YOUR killer feature — something only you'd think of?"

If `USER_BRIEFING_CONTEXT` is non-empty, acknowledge it: "You mentioned [summary of their vision] at the start. Want to add more, or does the manifest already cover it?"

Each answer that produces a concrete feature → score and add to the transcendence table. After the brainstorm, return to this gate with the updated manifest.
WAIT for approval. Do NOT generate until approved.

---

Before following `Next:`, record the durable handoff and point `--evidence` at
the approved absorb manifest:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "08-ecosystem-absorb-gate" --evidence "$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md"
```

Next: phases/09-api-reachability-gate.md
