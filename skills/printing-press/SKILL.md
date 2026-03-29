---
name: printing-press
description: Generate a ship-ready CLI for an API with a lean research -> generate -> build -> shipcheck loop.
version: 2.0.0
min-binary-version: "0.2.0"
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - WebFetch
  - WebSearch
  - AskUserQuestion
  - Agent
---

# /printing-press

Generate the best useful CLI for an API without burning an hour on phase theater.

```bash
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press emboss notion
/printing-press emboss notion-pp-cli
/printing-press emboss ~/printing-press/library/notion-pp-cli
```

## What Changed In v2

The old skill inflated the path to ship:
- too many mandatory research documents before code existed
- too many separate late-stage validation phases after code existed
- too many chances to discover obvious failures late

This version uses one lean loop:
1. Resolve the spec and write one research brief
2. Generate
3. Build the highest-value gaps
4. Run one shipcheck block
5. Optionally run live API smoke tests

Artifacts are still written, but only the ones that materially help the next step.

## Modes

### Default

Normal mode. Claude does research, generation orchestration, implementation, and verification.

### Codex Mode

If the arguments include `codex` or `--codex`, offload pure code-writing tasks to Codex CLI.

Use Codex for:
- writing store/data-layer code
- writing workflow commands
- fixing dead flags / dead code / path issues
- README cookbook edits

Keep on Claude:
- research and product positioning
- choosing which gaps matter
- verification results and ship decisions

If Codex fails 3 times in a row, stop delegating and finish locally.

### Emboss Mode

If the arguments start with `emboss`, this is a second-pass improvement cycle for an existing generated CLI.

```bash
/printing-press emboss notion-pp-cli
/printing-press emboss notion
/printing-press emboss ~/printing-press/library/notion-pp-cli
```

#### Emboss Name Resolution

The CLI accepts a name or path directly (`printing-press emboss notion`). If the CLI errors with "no CLI named X found," search `$PRESS_LIBRARY/` for close matches and use `AskUserQuestion` to let the user pick. Show at most 4 matches, sorted by directory modification time (most recent first), with human-friendly relative timestamps (e.g. "generated 2 hours ago").

#### Emboss Cycle

Emboss is:
1. audit baseline
2. quick re-research
3. top-5 gap analysis
4. implement improvements
5. re-audit and report delta

Do not run emboss automatically.

## Rules

- Optimize for time-to-ship, not time-to-document.
- Reuse prior research whenever it is already good enough.
- Do not split one idea across multiple mandatory artifacts.
- Do not create a separate narrative phase for dogfood, dead-code audit, runtime verification, and final score. Treat them as one shipcheck block.
- Run cheap, high-signal checks early.
- Fix blockers and high-leverage failures first.
- Reuse the same spec path across `generate`, `dogfood`, `verify`, and `scorecard`.
- YAML, JSON, local paths, and URLs are all valid spec inputs for the verification tools.
- Maximum 2 verification fix loops unless the user explicitly asks for more.

## Setup

Before doing anything else:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.2.0
if ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    export PATH="$HOME/go/bin:$PATH"
    echo "Added ~/go/bin to PATH"
  elif command -v go >/dev/null 2>&1; then
    echo "printing-press not found. Installing..."
    GOPRIVATE=github.com/mvanhorn/* go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest
    export PATH="$HOME/go/bin:$PATH"
  else
    echo "printing-press binary not found and Go is not installed."
    echo "Install Go first, then run:  go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest"
    return 1 2>/dev/null || exit 1
  fi
fi

# Derive scope: prefer git repo root, fall back to CWD
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, warn the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest` to update." Continue anyway but surface the warning prominently.

After you know `<api>`, initialize the run-scoped artifact paths:

```bash
RUN_ID="$(date +%Y%m%d-%H%M%S)"
API_RUN_DIR="$PRESS_RUNSTATE/runs/$RUN_ID"
RESEARCH_DIR="$API_RUN_DIR/research"
PROOFS_DIR="$API_RUN_DIR/proofs"
PIPELINE_DIR="$API_RUN_DIR/pipeline"
STAMP="$(date +%Y-%m-%d-%H%M%S)"

mkdir -p "$RESEARCH_DIR" "$PROOFS_DIR" "$PIPELINE_DIR"
STATE_FILE="$API_RUN_DIR/state.json"
```

Maintain a lightweight state file at `$STATE_FILE` so `/printing-press-score` can rediscover the current run. It should always contain:

```json
{
  "api_name": "<api>",
  "working_dir": "<absolute cli dir>",
  "output_dir": "<absolute cli dir>",
  "spec_path": "<absolute spec path if known>"
}
```

Active mutable work lives under `$PRESS_RUNSTATE/`. Published CLIs live under `$PRESS_LIBRARY/`. Archived research and verification evidence live under `$PRESS_MANUSCRIPTS/<api>/<run-id>/`. Do not write mutable run artifacts into the repo checkout.

Examples of the current naming/layout to preserve:
- `discord-pp-cli/internal/store/store.go`
- `linear-pp-cli stale --days 30 --team ENG`
- `github.com/mvanhorn/discord-pp-cli`

## Outputs

Every run writes up to 5 concise artifacts under the current managed run and archives them to `$PRESS_MANUSCRIPTS/<api>/<run-id>/`:

1. `research/<stamp>-feat-<api>-pp-cli-brief.md`
2. `research/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`
3. `proofs/<stamp>-fix-<api>-pp-cli-build-log.md`
4. `proofs/<stamp>-fix-<api>-pp-cli-shipcheck.md`
5. `proofs/<stamp>-fix-<api>-pp-cli-live-smoke.md` (only if live testing runs)

These do not need to be 200+ lines. Keep them dense, evidence-backed, and directly useful.

## Phase 0: Resolve And Reuse

Before new research:

1. Resolve the spec source.
   - If the user passed `--har <path>`, this is a HAR-first run. Run `printing-press sniff --har <path> --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"` to generate a spec from captured traffic. Use the generated spec as the primary spec source for the rest of the pipeline. Skip the sniff gate in Phase 1.7 (sniff already ran).
   - If the user passed `--spec`, use it directly (existing behavior).
   - Otherwise, proceed with normal discovery (catalog, KnownSpecs, apis-guru, web search).
2. Check for prior research in:
   - `$PRESS_MANUSCRIPTS/<api>/*/research/*`
   - `$REPO_ROOT/docs/plans/*<api>*` (legacy fallback)
3. Reuse good prior work instead of redoing it.
4. **API Key Gate** — Check whether this API requires authentication, then handle accordingly.

**First, determine if the API needs auth.** Use these signals:
- The spec has no `security` or `securityDefinitions` section → likely no auth needed
- The API's endpoints are accessible without authentication (e.g., ESPN's undocumented endpoints, weather APIs, public data feeds) — note: "no auth required" does NOT mean the service has an official public API
- No env var matching the API name exists AND no known token pattern applies
- Community docs or npm/PyPI wrappers describe the API as "no auth required"

**If no auth is required**, skip the key gate entirely. Proceed with: "No authentication required for `<API>` — skipping API key gate." Do NOT call it "a public API" unless the service officially publishes one. Many services (ESPN, etc.) have unauthenticated endpoints without having an official API. Live smoke testing in Phase 5 will work without a key.

**If the API DOES require auth**, run the key gate:

Token detection order:
- GitHub: `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`
- Discord: `DISCORD_TOKEN`, `DISCORD_BOT_TOKEN`
- Linear: `LINEAR_API_KEY`
- Notion: `NOTION_TOKEN`
- Stripe: `STRIPE_SECRET_KEY`
- Generic: `API_KEY`, `API_TOKEN`

**If a token IS found**, stop and explain:
> Found `<ENV_VAR>` in your environment. This key will be used **only** for read-only live smoke testing in Phase 5 — listing, fetching, and health checks. It will never be used for write operations (create, update, delete). OK to use it?

- If the user approves → proceed with the key available for Phase 5.
- If the user declines → proceed without the key and display: "Live smoke testing (Phase 5) will be skipped. The CLI will still be generated and verified against mock responses."

**If no token is found**, stop and ask:
> No API key detected for `<API>`. You can provide one now for read-only live smoke testing in Phase 5, or continue without it.
>
> Set it with `export <ENV_VAR>=<your-key>` or paste the key here.

- If the user provides a key → proceed with the key available for Phase 5.
- If the user declines → proceed without the key and display: "Live smoke testing (Phase 5) will be skipped. The CLI will still be generated and verified against mock responses."

Resolve the API key gate (or skip it for public APIs) before moving to Phase 1.

## Phase 1: Research Brief

Before starting research, check if the API has a built-in catalog entry:

```bash
printing-press catalog show <api> --json 2>/dev/null
```

If the catalog has an entry for this API, present the user with a choice:

> "<API> is in the built-in catalog (spec: <spec_url>). Use the catalog config to skip discovery, or run full discovery?"

- If catalog config: use the spec_url from the catalog entry, skip the research/discovery phase
- If full discovery: proceed with the normal research workflow
- If the catalog doesn't have this API: proceed normally without mentioning the catalog

Write one build-driving brief, not a stack of phase essays.

The brief must answer:

1. What is this API actually used for?
2. What are the top 3-5 power-user workflows?
3. What are the top table-stakes competitor features?
4. What data deserves a local store?
5. Why would someone install this CLI instead of the incumbent?
6. What is the product name and thesis?

Research checklist:
- Find the spec or docs source
- Find the top 1-2 competitors
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

## Top Workflows
1. ...

## Table Stakes
- ...

## Data Layer
- Primary entities:
- Sync cursor:
- FTS/search:

## Product Thesis
- Name:
- Why it should exist:

## Build Priorities
1. ...
2. ...
3. ...
```

## Phase 1.7: Sniff Gate

After Phase 1 research, evaluate whether sniffing the live site would improve the spec. Skip this gate entirely if the user already passed `--har` or `--spec` (spec source is already resolved).

**IMPORTANT:** When the decision matrix below says "Offer sniff", you MUST ask the user via `AskUserQuestion`. Do NOT silently decide to skip sniff because the docs look thorough — the user should make that call. The only case where you skip silently is "spec appears complete" (no gap detected).

### When to offer sniff

| Spec found? | Research shows gaps? | Auth required? | Action |
|-------------|---------------------|----------------|--------|
| Yes | Yes — docs or competitors show significantly more endpoints than the spec | No | **MUST offer sniff as enrichment** |
| Yes | No — spec appears complete | Any | Skip silently |
| No | Community docs exist (e.g., Public-ESPN-API) | No | **MUST offer sniff OR --docs** — present both options so the user decides |
| No | No docs found either | No | **MUST offer sniff as primary discovery** |
| No | N/A | Yes (login required) | Skip — fall back to `--docs` |

**Gap detection heuristic:** If Phase 1 research found documentation, competitor tools, or community projects that reference significantly more endpoints or features than the resolved spec covers, that's a gap signal. Example: "The Zuplo OpenAPI spec has 42 endpoints, but the Public-ESPN-API docs describe 370+."

### Sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more (competitors reference M+ features). Want me to sniff `<url>` to discover endpoints the spec missed? I'll check for browser-use or agent-browser and install if needed."
>
> Options:
> 1. **Yes — sniff and merge** (browse the site, capture traffic, merge discovered endpoints with the existing spec. Installs capture tools if needed.)
> 2. **No — use existing spec** (proceed with what we have)

### Sniff as primary (no spec found)

Present to the user via `AskUserQuestion`:

> "No OpenAPI spec found for `<API>`. Want me to sniff `<likely-url>` to discover the API from live traffic? I'll check for browser-use or agent-browser and install if needed."
>
> Options:
> 1. **Yes — sniff the live site** (browse `<url>`, capture API calls, generate a spec. Installs capture tools if needed.)
> 2. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 3. **No — I'll provide a spec or HAR** (user will supply input manually)

### If user approves sniff

#### Step 1: Detect capture tool

Check which browser automation tool is available, in preference order:

```bash
# Prefer browser-use (autonomous agent, HAR includes response bodies)
if command -v browser-use >/dev/null 2>&1 || uvx browser-use --version >/dev/null 2>&1; then
  SNIFF_BACKEND="browser-use"
# Fall back to agent-browser (precise control, Claude drives the loop)
elif command -v agent-browser >/dev/null 2>&1; then
  SNIFF_BACKEND="agent-browser"
else
  SNIFF_BACKEND="none"
fi
```

If a tool is found, report: "Using **<tool>** for traffic capture." and proceed to Step 2.

#### Step 1b: Install capture tool (if none found)

If `SNIFF_BACKEND="none"`, neither capture tool is installed. Offer to install one via `AskUserQuestion`:

> "No browser automation tool found. I need one to sniff the live site. Which would you like to install?"
>
> Options:
> 1. **Install browser-use (Recommended)** — "Autonomous agent that explores sites on its own. HAR includes response bodies. Requires Python. ~2 min install."
> 2. **Install agent-browser** — "Lighter install (~30s). I'll drive the browsing. Requires Node.js."
> 3. **Skip — I'll provide a HAR manually** — "Export a HAR yourself from browser DevTools and provide the path."

**If user picks browser-use:**

```bash
# Detect Python package manager
if command -v uv >/dev/null 2>&1; then
  uv pip install browser-use
elif command -v pip >/dev/null 2>&1; then
  pip install browser-use
else
  echo "Neither uv nor pip found. Install Python first: https://www.python.org/downloads/"
  # Fall back to asking about agent-browser or manual HAR
fi
```

After install, re-run detection. If `browser-use` is now available, set `SNIFF_BACKEND="browser-use"` and proceed to Step 2a. If install failed, show the error and offer agent-browser as alternative or fall back to manual HAR.

**If user picks agent-browser:**

```bash
# Detect Node.js package manager
if command -v brew >/dev/null 2>&1; then
  brew install agent-browser
elif command -v npm >/dev/null 2>&1; then
  npm install -g agent-browser
else
  echo "Neither brew nor npm found. Install Node.js first: https://nodejs.org/"
  # Fall back to manual HAR
fi
```

After install, re-run detection. If `agent-browser` is now available, set `SNIFF_BACKEND="agent-browser"` and proceed to Step 2b. If install failed, show the error and fall back to manual HAR.

**If user picks manual HAR**, ask the user for a HAR file path and skip to Step 3.

#### Step 2a: browser-use capture (preferred)

browser-use runs its own autonomous agent loop — it explores the site without Claude needing to drive each click. Its HAR captures response bodies natively, which websniff needs for schema inference.

Construct a domain-aware exploration task from Phase 1 research findings:

```
Browse <target-url>. Your goal is to discover as many API endpoints as possible.

Strategy:
- Use the main search/filter features with realistic sample data (<domain_hints_from_research>)
- Click through different sections and categories
- Try pagination, sorting, and filter combinations
- Interact with forms, dropdowns, and buttons
- Avoid: navigation to external sites, login prompts, cookie banners

Explore for up to 100 steps or until you have covered the main features.
```

Run:
```bash
browser-use run "<exploration_task>" --headless --har-path "$API_RUN_DIR/sniff-capture.har"
```

If `--har-path` is not a supported CLI flag, use a thin wrapper:
```python
import asyncio
from browser_use import Agent, Browser, BrowserProfile
profile = BrowserProfile(headless=True, record_har_path="$API_RUN_DIR/sniff-capture.har", record_har_content="embed")
browser = Browser(profile=profile)
agent = Agent(task="<exploration_task>", browser=browser, max_steps=100)
asyncio.run(agent.run())
```

#### Step 2b: agent-browser capture (fallback)

If browser-use is not available, use agent-browser with Claude driving the exploration. **Note:** agent-browser HAR does not include response bodies. Use the enriched capture workflow to get them.

1. **Browse and capture**:
   ```bash
   agent-browser open <target-url> --headless
   agent-browser network har start
   ```

2. **Explore the site** using the snapshot-reason-act loop:
   - `agent-browser snapshot -i` to see the page
   - Identify interactive elements: search boxes, filters, buttons, dropdowns, pagination
   - Prioritize: search forms > filters > action buttons > dropdowns > pagination
   - Skip: navigation links, footer links, social media buttons, cookie/consent banners
   - Fill forms with realistic sample data based on the domain
   - `agent-browser wait --network-idle` after each interaction
   - Repeat for up to 5 rounds or until no new API endpoints appear for 2 consecutive rounds

3. **Capture response bodies** (agent-browser HAR omits them):
   ```bash
   agent-browser network requests --type xhr,fetch --json
   ```
   For each API request (filter by JSON content-type, skip analytics domains):
   ```bash
   agent-browser network request <request-id> --json
   ```
   Combine HAR metadata + response bodies into an enriched capture JSON at `$API_RUN_DIR/sniff-capture.json`.

4. **Stop HAR recording**:
   ```bash
   agent-browser network har stop "$API_RUN_DIR/sniff-capture.har"
   ```

#### Step 3: Analyze capture

Run websniff on the captured traffic:
```bash
printing-press sniff --har "$API_RUN_DIR/sniff-capture.har" --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"
```

If using agent-browser's enriched capture format instead:
```bash
printing-press sniff --har "$API_RUN_DIR/sniff-capture.json" --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"
```

#### Step 4: Report and update spec source

Report: "Sniff discovered **N endpoints** across **M resources**. [X new endpoints not in the original spec.]"

Update the spec source for Phase 2:
- **Enrichment mode**: Phase 2 will use `--spec <original> --spec <sniff-spec> --name <api>` to merge both
- **Primary mode**: Phase 2 will use `--spec <sniff-spec>` directly

### If user declines sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

## Phase 1.5: Ecosystem Absorb Gate

THIS IS A MANDATORY STOP GATE. Do not generate until this is complete and approved.

The GOAT CLI doesn't "find gaps." It absorbs EVERY feature from EVERY tool and then transcends with compound use cases nobody thought of. This phase builds the absorb manifest.

### Step 1.5a: Search for every tool that touches this API

Run these searches in parallel:

1. **WebSearch**: `"<API name>" Claude Code plugin site:github.com`
2. **WebSearch**: `"<API name>" MCP server model context protocol`
3. **WebSearch**: `"<API name>" Claude skill SKILL.md site:github.com`
4. **WebSearch**: `"<API name>" CLI tool site:github.com` (competing CLIs)
5. **WebSearch**: `"<API name>" CLI site:npmjs.com` (npm packages)
6. **WebFetch**: Check `github.com/anthropics/claude-plugins-official/tree/main/external_plugins` for official plugin
7. **WebSearch**: `"<API name>" MCP site:lobehub.com OR site:mcpmarket.com OR site:fastmcp.me`
8. **WebSearch**: `"<API name>" automation script workflow site:github.com`
9. **WebSearch**: `"<API name>" SDK wrapper site:npmjs.com`
10. **WebSearch**: `"<API name>" client library site:pypi.org`

### Step 1.5b: Catalog every feature into the absorb manifest

For EACH tool found, list EVERY feature/tool/command it provides. Then define how our CLI matches AND beats it:

```markdown
## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-----------|-------------------|-------------|
| 1 | Search issues by text | Linear MCP search_issues | FTS5 offline search | Works offline, regex, SQL composable |
| 2 | Create issue | Linear MCP create_issue | --stdin batch, --dry-run | Agent-native, scriptable, idempotent |
| 3 | Sprint board view | jira-cli sprint view | SQLite-backed sprint query | Historical velocity, offline |
```

Every row = a feature we MUST build. No exceptions. If someone else has it, we have it AND it works offline, with --json, --dry-run, typed exit codes, and SQLite persistence.

SDK wrapper methods should be treated as features to absorb — each public method/function is a feature the CLI should match.

### Step 1.5c: Identify compound use cases

What compound use cases become possible ONLY when ALL absorbed features live in SQLite together?

```markdown
### Transcendence (only possible with our local data layer)
| # | Feature | Command | Why Only We Can Do This |
|---|---------|---------|------------------------|
| 1 | Bottleneck detection | bottleneck | Requires local join across issues + assignees + cycle data |
| 2 | Velocity trends | velocity --weeks 4 | Requires historical cycle snapshots in SQLite |
| 3 | Duplicate detection | similar "login bug" | Requires FTS5 across ALL issue text + comments |
```

Minimum 5 compound use case features. These are the NOI commands.

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

#### Generate and Score Candidates

Generate 3-5 novel feature ideas. For each, score on 4 dimensions:

| Dimension | Points | Scoring |
|-----------|--------|---------|
| **Domain Fit** | 0-3 | 3=core to this API's power users, 2=useful but niche, 1=tangential, 0=wrong domain |
| **User Pain** | 0-3 | 3=research surfaced explicit demand (community complaints, competitor gap), 2=implied need, 1=speculative, 0=no evidence |
| **Build Feasibility** | 0-2 | 2=SQLite store + existing sync covers it, 1=needs minor data model additions, 0=requires new infrastructure |
| **Research Backing** | 0-2 | 2=evidence from 2+ sources in Phase 1/1.5 research, 1=evidence from 1 source, 0=invented |

**Normalize:** `score_10 = round(raw / 10 * 10)`. Include features scoring ≥ 5/10.

#### Add to Transcendence Table

Add each qualifying feature as a new row in the transcendence table:

```markdown
| # | Feature | Command | Why Only We Can Do This | Score | Evidence |
|---|---------|---------|------------------------|-------|----------|
| N | Player comparison | compare "LeBron" "Curry" | Requires local join across player stats + team + season data | 8/10 | ESPN community requests, espn_scraper lacks cross-player queries |
```

The "Evidence" column MUST cite specific findings from Phase 1 or Phase 1.5 research. No unsupported assertions.

### Step 1.5d: Write the manifest artifact

Write to `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`

The manifest now includes both compound use cases (Step 1.5c) and auto-suggested features (Step 1.5c.5) in the transcendence table.

### Phase Gate 1.5

**STOP.** Present the absorb manifest to the user via `AskUserQuestion`:

"Found [N] features across [X] tools (MCPs, skills, CLIs, scripts). Our CLI will absorb all [N] and add [M] transcendence features ([K] auto-suggested with scores). Total: [N+M] features. This is [Z]% more than the best existing tool."

Options:
1. **Approve — generate now** — Start CLI generation with the full manifest
2. **Brainstorm more features** — Interactive dialogue to explore your own feature ideas before building
3. **Review the research** — Show me the full brief and manifest before deciding
4. **Trim scope** — The feature count is too ambitious, let's focus on a subset

If user selects **"Brainstorm more features"**, run a lightweight feature brainstorm:

1. "What workflows do you personally use `<API>` for that aren't covered in the manifest?"
2. "What's annoying about existing tools for `<API>` that you wish someone would fix?"
3. "If this CLI could do one magical thing, what would make you say 'I need this'?"

Each answer that produces a concrete feature → add to the transcendence table. After the brainstorm, return to this gate with the updated manifest.
WAIT for approval. Do NOT generate until approved.

---

## Phase 2: Generate

Use the resolved spec source and generate immediately.

OpenAPI / internal YAML:

```bash
printing-press generate \
  --spec <spec-path-or-url> \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --lenient --validate
```

Sniff-enriched (original spec + sniff-discovered spec):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-sniff-spec.yaml" \
  --name <api> \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --lenient --validate
```

Sniff-only (no original spec, sniff was the primary source):

```bash
printing-press generate \
  --spec "$RESEARCH_DIR/<api>-sniff-spec.yaml" \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --lenient --validate
```

Docs-only:

```bash
printing-press generate \
  --docs <docs-url> \
  --name <api> \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --validate
```

GraphQL-only APIs:
- Generate scaffolding only in Phase 2
- Build real commands in Phase 3 using a GraphQL client wrapper

After generation:
- note skipped complex body fields
- fix only blocking generation failures here
- do not start broad polish work yet

If generation fails:
- fix the specific blocker
- retry at most 2 times
- prefer generator fixes over manual generated-code surgery when the failure is systemic

## Phase 3: Build The GOAT

Build comprehensively. The absorb manifest from Phase 1.5 IS the feature list.

Priority 0 (foundation):
- data layer for ALL primary entities from the manifest
- sync/search/SQL path - this is what makes transcendence possible

Priority 1 (absorb - match everything):
- ALL absorbed features from the Phase 1.5 manifest
- Every feature from every competing tool, matched and beaten with agent-native output
- This is NOT "top 3-5" - it is the FULL manifest

Priority 2 (transcend - build what nobody else has):
- ALL transcendence features from Phase 1.5
- The NOI commands that only work because everything is in SQLite
- These are the commands that make someone say "I need this"

Priority 3 (polish):
- skipped complex request bodies that block important commands
- naming cleanup for ugly operationId-derived commands
- tests for non-trivial store/workflow logic

### Agent Build Checklist (per command)

After building each command in Priority 1 and Priority 2, verify these 7 principles are met. These map 1:1 to what Phase 4.9's agent readiness reviewer will check - apply them now so the review becomes a confirmation, not a catch-all.

1. **Non-interactive**: No TTY prompts, no `bufio.Scanner(os.Stdin)`, works in CI without a terminal
2. **Structured output**: `--json` produces valid JSON, `--select` filters fields correctly
3. **Progressive help**: `--help` shows realistic examples with domain-specific values (not "abc123")
4. **Actionable errors**: Error messages name the specific flag/arg that's wrong and the correct usage
5. **Safe retries**: Mutation commands support `--dry-run`, idempotent where possible
6. **Composability**: Exit codes are typed (0/2/3/4/5/7), output pipes to `jq` cleanly
7. **Bounded responses**: `--compact` returns only high-gravity fields, list commands have `--limit`

### Search Dedup Rule

When building cross-entity search commands, use per-table FTS search methods individually. Do NOT combine per-table search with the generic `db.Search()` — this causes duplicate results because the same entities exist in both `resources_fts` and per-table FTS indexes.

### Priority 1 Review Gate

After completing ALL Priority 1 (absorbed) features, BEFORE starting Priority 2 (transcendence):

Pick 3 random commands from Priority 1. Run each with:
```bash
<cli> <command> --help          # Does it show realistic examples?
<cli> <command> --dry-run       # Does it show the request without sending?
<cli> <command> --json          # Does it produce valid JSON?
```

If any of the 3 fail, there's a systemic issue. Fix it across all commands before proceeding. This catches problems like "--dry-run not wired" or "--json outputs table instead of JSON" early, when they're cheap to fix.

Get Priority 0 and 1 working first (the foundation and absorbed features), pass the review gate, then build Priority 2 (transcendence), then verify.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`

Include:
- what was built
- what was intentionally deferred
- skipped body fields that remain
- any generator limitations found

## Phase 4: Shipcheck

Run one combined verification block.

```bash
printing-press dogfood   --dir "$PRESS_LIBRARY/<api>-pp-cli" --spec <same-spec>
printing-press verify    --dir "$PRESS_LIBRARY/<api>-pp-cli" --spec <same-spec> --fix
printing-press scorecard --dir "$PRESS_LIBRARY/<api>-pp-cli" --spec <same-spec>
```

Interpretation:
- `dogfood` catches dead flags, dead helpers, invalid paths, example drift, and broken data wiring
- `verify` catches runtime breakage and runs the auto-fix loop for common failures
- `scorecard` is the structural quality snapshot, not the source of truth by itself

Fix order:
1. generation blockers or build breaks
2. invalid paths and auth mismatches
3. dead flags / dead functions / ghost tables
4. broken dry-run and runtime command failures
5. scorecard-only polish gaps

Ship threshold:
- `verify` verdict is `PASS` or high `WARN` with 0 critical failures
- `dogfood` no longer fails because of spec parsing, binary path, or skipped examples
- `scorecard` is at least 65, or meaningfully improved and no core behavior is broken

Maximum 2 shipcheck loops by default.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-shipcheck.md`

Include:
- command outputs and scores
- top blockers found
- fixes applied
- before/after verify pass rate
- before/after scorecard total
- final ship recommendation: `ship`, `ship-with-gaps`, or `hold`

## Phase 5: Optional Live Smoke

Only run this if a token is available and the user agreed.

Use read-only smoke tests:
- `--help`
- one or two representative GET/list commands
- sync/search/health path if a local data layer exists

If live smoke finds bugs:
- fix only the real bug
- re-run the shipcheck block once

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-live-smoke.md`

## Fast Guidance

### When to use `printing-press print`

Use `printing-press print <api>` only when the user explicitly wants a resumable on-disk pipeline with phase seeds. It is optional.

The fast path for `/printing-press <API>` is:
- brief
- generate
- build
- shipcheck

### When to stop researching

Stop when you can answer:
- what to build first
- what data to persist
- what incumbent features cannot be missing

If the next research step does not change those answers, stop and generate.

### What not to do

Do not:
- write 5 separate mandatory research documents
- defer all workflows to "future work"
- skip verification because the CLI compiles
- treat scorecard alone as ship proof
- discover YAML/URL spec incompatibility late and manually convert specs if the tools can already consume them
- rerun the whole late-phase gauntlet for cosmetic README polish
- skip features because "the MCP already handles that" (absorb everything, beat it with offline + agent-native)
- build only "top 3-5 workflows" when the absorb manifest has 15+ (build them ALL, then transcend)
- generate before the Phase 1.5 Ecosystem Absorb Gate is approved
- call a CLI "GOAT" without matching every feature the best competitor has

### What counts as success

Success is:
- a generated CLI that gets to shipcheck without generator blockers
- verification tools working against the same spec the user generated from
- one or two fix loops, not a maze of re-entry phases
- a CLI that is plausibly shippable today, not a perfect design memo
