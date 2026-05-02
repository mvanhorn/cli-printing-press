# CLI Printing Press

[![CI](https://github.com/mvanhorn/cli-printing-press/actions/workflows/lint.yml/badge.svg)](https://github.com/mvanhorn/cli-printing-press/actions/workflows/lint.yml)
[![Golden](https://github.com/mvanhorn/cli-printing-press/actions/workflows/golden.yml/badge.svg)](https://github.com/mvanhorn/cli-printing-press/actions/workflows/golden.yml)
[![Release](https://img.shields.io/github/v/release/mvanhorn/cli-printing-press?display_name=tag&sort=semver)](https://github.com/mvanhorn/cli-printing-press/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/mvanhorn/cli-printing-press)](go.mod)
[![License](https://img.shields.io/github/license/mvanhorn/cli-printing-press)](LICENSE)

Nothing is more valuable than time and money. In a world of AI agents, that's speed and token spend. A well-designed CLI is muscle memory for an agent: no hunting through docs, no wrong turns, no wasted tokens. We built the Printing Press to print the best CLIs in the world for agents.

It reads the official API docs, studies every popular community CLI and MCP server, sniffs the web for the APIs nobody published (think Google Flights or Dominos), and applies the power-user playbook Peter Steinberger proved with [discrawl](https://github.com/steipete/discrawl) and [gogcli](https://github.com/steipete/gogcli) - local SQLite, compound commands, agent-native flags. It fuses all of that and prints a token-efficient Go CLI plus a Claude Code skill plus an MCP server for any API or any website.

Three CLIs printed by the press, installable today:

- ESPN (sniffed, no official API). _"Tonight's NBA playoff games with live score, series state, each team's leading scorer's stat line, and any injury or lineup news from the last 24 hours."_ Returns everything in one call.
- flight-goat (Kayak nonstop search plus sniffed Google Flights). _"Non-stop flights over 8 hours from Seattle for 4 people, Dec 24 to Jan 1, cheapest first."_ Two sources stitched into one query.
- linear-pp-cli (50ms against a local SQLite mirror). _"Every blocked issue whose blocker has been stuck for a week."_ Compound queries the API can't answer.

Browse the full catalog of printed CLIs at [printingpress.dev](https://printingpress.dev) or in the [Printing Press Library](https://github.com/mvanhorn/printing-press-library). 24 CLIs across 17 categories, 17 with full MCP servers.

## Install

The Printing Press has two parts, and you need both:

1. **The binary** (`printing-press`) — the engine. Does research, generation, verification, scoring.
2. **The Claude Code plugin** — adds the `/printing-press` slash command. This is your primary interface; the skill drives the binary behind the scenes.

The plugin alone has nothing to call; the binary alone works but skips the curated agent loop.

**Prerequisites:** [Go 1.22+](https://go.dev/dl/) and [Claude Code](https://claude.ai/code).

### 1. Install the binary

```bash
go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest
```

Verify: `printing-press --version`.

If you skip this step, the plugin will run `go install` for you on first use (assuming Go is on your PATH). Doing it upfront avoids a slow first run. If `go install` fails with `410 Gone` or an auth error, see [Troubleshooting](#troubleshooting).

### 2. Install the plugin — pick one path

Both paths give you the `/printing-press` slash command. They differ in how skills are kept up to date and whether parallel runs are easy.

#### Option A — Clone and run (recommended for active users)

Loads the plugin directly from a local clone of this repo. `git pull` is your update mechanism, so you always have the latest skills with no plugin-marketplace lag. Add `-w` to start the session in a fresh git worktree — handy for printing multiple CLIs in parallel without runs stomping on each other.

```bash
git clone https://github.com/mvanhorn/cli-printing-press.git
cd cli-printing-press
claude --plugin-dir .          # load this repo's skills directly
claude --plugin-dir . -w       # ...in a new worktree (parallel runs)
```

Update with `git pull` whenever you want fresh skills. The binary from step 1 still does the heavy lifting; `--plugin-dir .` only changes where the plugin loads from.

#### Option B — Plugin marketplace (one-shot, auto-updated by Claude)

Stable releases, kept up to date by Claude's plugin update mechanism. Run inside Claude Code:

```text
/plugin marketplace add mvanhorn/cli-printing-press
/plugin install cli-printing-press@cli-printing-press
```

Verify (either path): type `/printing-press` in Claude Code and confirm the slash command appears.

### Optional: pre-built CLI library

To browse and install pre-built CLIs from the [Printing Press Library](https://github.com/mvanhorn/printing-press-library) catalog (24 CLIs across 17 categories), add a second plugin in Claude Code:

```text
/plugin marketplace add mvanhorn/printing-press-library
/plugin install printing-press-library@printing-press-library
```

This adds the `/ppl` slash command for browsing and installing CLIs from the public catalog.

## Quick start

```bash
/printing-press Notion                               # Print a CLI for an API by name
/printing-press https://postman.com/explore         # ...or point it at a website (no spec needed)
```

`/printing-press` is the slash command from the Claude Code plugin. It drives the `printing-press` binary you installed — research, generation, scoring, and shipcheck all run through the binary. Two parts, one workflow.

One command. Lean loop. Produces a Go CLI plus an MCP server that absorbs every feature from every competing tool, then transcends with compound use cases only possible with local data. REST, GraphQL, or browser-sniffed traffic. No OpenAPI spec required.

Each run produces two binaries (`<api>-pp-cli` plus `<api>-pp-mcp`), research documents, verification proofs, and a Quality Score.

By default, active and published output are separated:

- Active managed runs work in `~/printing-press/.runstate/<scope>/runs/<run-id>/working/<api>-pp-cli`
- Published CLIs go to `~/printing-press/library/<api>`
- Archived manuscripts go to `~/printing-press/manuscripts/<api>/<run-id>/`
- Manuscripts are split into `research/`, `proofs/`, `discovery/`, and `pipeline/`

`<scope>` is derived from the current git checkout path, so parallel worktrees do not stomp on each other. If you pass `--output`, that overrides the generated CLI location for that command.

### Codex mode (60% fewer Opus tokens)

```bash
/printing-press HubSpot codex    # Offload code generation to Codex CLI
/printing-press HubSpot          # Standard Opus mode (default)
```

When you add `codex`, Phase 3's code generation tasks are delegated to Codex CLI. Claude stays the brain (research, planning, scoring, review). Codex does the hands (writing Go code from scoped prompts). Same quality, 60% fewer Opus tokens. If Codex fails 3 times in a row, the press falls back to doing it locally, no manual intervention needed.

### Improve an existing CLI

Two second-pass paths with different scopes.

**Polish** — targeted fix-up. Diagnostics (dogfood, verify, scorecard, output review), fixes verify failures, removes dead code, cleans descriptions and READMEs, offers to publish. Auto-runs as Phase 5.5 of every generation; can also run standalone:

```bash
/printing-press-polish notion
```

**Emboss** — full second-pass cycle. Audits baseline, re-researches what's changed, identifies top 5 improvements, rebuilds, re-verifies, reports the delta. Use when you want significant additions, not just fixes:

```bash
/printing-press emboss notion              # By API name
/printing-press emboss notion-pp-cli       # By CLI name
/printing-press emboss ~/printing-press/library/notion          # By full path
```

### Publish

When you're happy with a CLI, publish it to the library:

```bash
/printing-press-publish linear                       # Validates, packages, creates PR
```

## Why these CLIs win

Most generators wrap endpoints and stop. Printing Press generates CLIs that understand the domain.

Local-first data layer. High-gravity resources get domain-specific SQLite tables (not JSON blobs), FTS5 full-text search indexes, and incremental sync with cursor tracking. `sync` pulls data down. `search` finds it in milliseconds. `sql` lets power users query directly. All offline, all local.

Machine-owned freshness. Store-backed CLIs can opt into a bounded pre-read refresh via `cache.enabled` so `--data-source auto` keeps the local store current without a manual `sync`. `--data-source local` and `--data-source live` give you full control.

Compound commands no wrapper can do. Once data lives in SQLite, commands like `stale`, `health`, `bottleneck`, and `reconcile` become possible - they join across resources and analyze history. A stateless API wrapper literally cannot do this.

Agent-native by default. Human-friendly tables when you're in a terminal. Auto-JSON when piped, no `--json` flag needed. `--compact` drops to high-gravity fields only (60-80% fewer tokens). Typed exit codes (`0`/`2`/`3`/`4`/`5`/`7`) let agents self-correct without parsing error text. `--dry-run` for safe exploration. Every flag exists because an AI agent will call it thousands of times a day.

No spec? No problem. Don't have an OpenAPI spec? Point the press at a website. It launches a browser, captures traffic, reverse-engineers the API, and generates the spec for you. ESPN, Postman Explore, internal tools - if you can click through it, the press can build a CLI for it.

Dual interface from one spec. Every API gets a Cobra CLI (`<api>-pp-cli`) and an MCP server (`<api>-pp-mcp`). Same client, same store, same auth. Shell agents use the CLI. IDE agents use MCP. Zero code duplication.

Verified, not vibes. Four mechanical checks - scorecard, dogfood, proof-of-behavior, live API smoke test - catch hallucinated paths, dead flags, auth mismatches, and broken data pipelines before you ship.

Credits its sources. Every generated README includes a Sources and Inspiration section that credits the ecosystem tools studied during research. We built on giants' shoulders and we say so.

## The non-obvious insight

Every API has a secret identity. The data it exposes is useful for something its creators never designed for. The printing press finds that secret and builds a CLI around it.

The Non-Obvious Insight (NOI) is a one-sentence reframe:

```
"[API] isn't just [obvious thing]. It's [non-obvious thing].
 Every [data point] is a signal about [hidden truth]."
```

| API | What they think it is | What it actually is |
|-----|----------------------|---------------------|
| Discord | A chat app | A searchable knowledge base. Every message thread is institutional memory. |
| Linear | An issue tracker | A team behavior observatory. Every state change is a signal about how your team actually works vs. how they think they work. |
| Stripe | A payment processor | A business health monitor. Every failed charge and churn event is a signal about product-market fit. |
| GitHub | A code host | An engineering culture fingerprint. Every review turnaround and merge pattern is a signal about how your team ships. |
| Notion | A doc editor | A knowledge decay detector. Every stale page and orphaned database is a signal about what your team has forgotten. |
| HubSpot | A CRM | Your company's relationship memory. Every deal stage transition, email open, and meeting log is a signal about pipeline health and rep performance. |
| Slack | Messaging | An organizational nervous system. Every response time and channel silence is a signal about team health. |
| ESPN | Sports data | A betting intelligence terminal. Every injury report, lineup change, and odds movement is a signal about game outcomes. |

The NOI is the creative DNA of every CLI the press generates. Phase 0 cannot complete without one. If the LLM can't write an NOI, the research wasn't deep enough.

The printing press automates what Steinberger does intuitively: look at an API, see what power users actually do with it, and build the commands that matter. Then also wrap every endpoint for completeness.

## The thinking behind it

### Every endpoint. Every insight. One command.

Discord's API has 300+ endpoints. Most generators stop there - wrap every endpoint, ship it, done. But [discrawl](https://github.com/steipete/discrawl) - Peter Steinberger's Discord tool - ignores most of them. It ships 11 commands: `sync`, `search`, `sql`, `tail`, `mentions`, `members`. 583 stars.

Why does the 11-command tool win? Because Steinberger saw something Discord's own API designers didn't: conversations are institutional knowledge. Every message thread is a document that should be archived, indexed, and searched locally. Those 11 commands embody that insight. The 300 endpoint wrappers don't.

Until now, you had to choose: breadth (wrap every endpoint) or depth (understand the user). The printing press eliminates that choice. It generates the full API surface AND matches every feature the top competitor has AND adds the discrawl-style intelligence layer AND an MCP server. One spec in. Everything out.

### Absorb and transcend

The GOAT CLI isn't built by finding gaps. It's built by absorbing every good idea and compounding on top.

Layer 1, absorb. Before generating, the ecosystem absorb gate catalogs every feature from every Claude Code plugin, MCP server, community skill, competing CLI, and automation script for your API. Every feature becomes a row in the absorb manifest, something our CLI must match AND beat with offline support, agent-native output, and SQLite persistence. The system even auto-suggests novel features it thinks are missing from the ecosystem before you approve the manifest.

Layer 2, transcend. Once you have everything in SQLite, compound use cases emerge that no stateless tool can do. Velocity tracking requires historical cycle data. Churn risk requires joining charges + subscriptions. Bottleneck detection requires the full issue relationship graph. These are the Non-Obvious Insight commands, and they only work because Layer 1 put everything in a local database.

The GOAT = everything everyone else does + everything nobody else thought of.

### The creativity ladder

Most API CLIs stop at Rung 1. The printing press climbs to Rung 5.

| Rung | What it is | Auto-generated? | Example |
|------|-----------|-----------------|---------|
| 1 | API wrapper commands | Yes (from spec) | `issue create --title "..."` |
| 2 | Output formatting | Yes (always) | `--json`, `--select`, `--csv`, `--dry-run` |
| 3 | Local persistence | Yes (conditional) | `sync`, `search`, `export`, `tail` |
| 4 | Domain analytics | Yes (from archetype) | `stale --days 30`, `orphans`, `load` |
| 5 | Behavioral insights | Yes (from archetype) | `health` (composite score), `similar` (duplicate detection) |

Rung 3 is table stakes. Rung 4 is where discrawl lives. Rung 5 is where nobody else is yet.

The press generates the API wrapper in Phase 2 (Rung 1-2). Then it generates the discrawl-style data layer and workflow commands in Phase 3 (Rung 3-5) from domain archetype templates. Both in one run.

### How we knew this was real

When choosing between Peter Steinberger's [gogcli](https://github.com/steipete/gogcli) (6.5K+ stars, Go) and Google's official [Workspace CLI](https://github.com/googleworkspace/cli) (10K+ stars in a week, Rust), we ran [/last30days](https://github.com/mvanhorn/last30days-skill) - a recency research skill - across 34 X posts, 5 YouTube videos, and 10 web sources.

The verdict: use gogcli. The newer, official tool with 10x the API coverage lost to the older third-party one. As one user put it: "my preference is 100% gogcli since I have my agent working a lot with Google Docs and sheets, and gogcli just makes him able to do what he needs to do."

Breadth doesn't beat depth. Understanding the user beats understanding the API.

### Why CLIs plus MCP

The NOI is the creative intelligence. The printing press generates both interfaces from one spec:

- `<api>-pp-cli`. Cobra CLI for humans plus shell agents (Claude Code, Codex, Gemini CLI).
- `<api>-pp-mcp`. MCP server for Claude Desktop, Cursor, Windsurf, Cline. Auto-discovered, no shell needed.

Same `internal/client`, same `internal/store`, same auth. Two binaries, zero code duplication.

CLIs win for agents. 100x fewer tokens than MCP tool definitions. LLMs were trained on shell interactions. Exit code 0 = done. `--json | jq` is a first-class composition pattern.

MCP wins for IDE integration. Claude Desktop and Cursor discover tools automatically via MCP. No shell needed. The MCP server exposes the same operations as the CLI, including the data layer (sync, search, sql).

```
One spec  ->  printing-press generate  ->  <api>-pp-cli (cobra)  +  <api>-pp-mcp (MCP server)
                                            |                       |
                                            same internal/client, internal/store
```

Every API that gets a CLI+MCP becomes instantly accessible to every AI coding tool. The printing press is the factory.

## How it works

The fast path is a lean loop. Artifacts still matter, but only when they directly improve the next phase.

```
Phase 0     Resolve + Reuse           (1-3 min)    Reuse research, detect tokens, resolve spec or URL
Phase 1     Research Brief            (5-10 min)   API identity, competitors, data layer, product thesis
Phase 1.5   Ecosystem Absorb Gate     (5-10 min)   Catalog every MCP/skill/CLI feature -> absorb manifest + novel suggestions
Phase 1.7   Browser-Sniff Gate        (2-5 min)    Browser capture, HAR import, discovery provenance
Phase 2     Generate                  (1-2 min)    Go CLI + MCP server from spec with validation
Phase 3     Build The GOAT            (10-20 min)  ALL absorbed features + transcendence commands
Phase 4     Shipcheck                 (3-8 min)    Dogfood + verify --fix + scorecard as one verification block
Phase 5     Live Smoke (optional)     (2-5 min)    Read-only API smoke + data-flow check
```

Three entry paths. Got an OpenAPI spec? Use `--spec`. Got a URL to a website with no docs? The browser-sniff gate launches a browser, captures traffic, and generates the spec. Got a HAR file from DevTools? Pass `--har`. The press handles all three.

19 APIs in the catalog. Asana, DigitalOcean, Discord, Front, GitHub, HubSpot, LaunchDarkly, Pipedrive, Plaid, Postman, Product Hunt, SendGrid, Sentry, Square, Stripe, Stytch, Telegram, Twilio, plus Petstore for testing. Each pre-verified with spec URL, auth type, and category.

Discovery provenance. When the press sniffs a website, it archives everything - pages visited, endpoints discovered, response samples, rate limiting events, and `traffic-analysis.json` with protocol/auth/protection signals and discovery warnings - into a `discovery/` manuscript alongside the research and proofs. Full audit trail.

Full pipeline contract. The fast path above compresses a longer 9-phase managed pipeline: preflight, research, scaffold, enrich, regenerate, review, agent-readiness, comparative, ship. Inputs, outputs, gates, and artifacts for each phase are documented in [docs/PIPELINE.md](docs/PIPELINE.md). Use it when you want to stop at any phase, resume later, re-run one step, or port the flow to another tool.

### MCP spec surface

The generator ships three opt-in knobs on the spec's `mcp:` block, aligned with Anthropic's [production-agent MCP guidance](https://www.anthropic.com/news/building-agents-that-reach-production-systems-with-mcp):

```yaml
mcp:
  transport: [stdio, http]        # remote-capable for cloud-hosted agents; default [stdio]
  addr: ":7777"                   # default bind for the http transport
  orchestration: code             # "endpoint-mirror" (default), "intent", or "code"
  endpoint_tools: hidden          # suppress raw endpoint tools when intents cover the surface
  intents:                        # compose multi-step tools declaratively
    - name: create_issue_from_thread
      description: "Create an issue from a Slack thread."
      params:
        - { name: thread_id, type: string, required: true, description: "slack thread id" }
      steps:
        - endpoint: messages.get_thread
          bind: { thread_id: "${input.thread_id}" }
          capture: thread
        - endpoint: issues.create
          bind: { title: "${thread.subject}", description: "${thread.body}" }
          capture: issue
      returns: issue
```

Run `printing-press mcp-audit` after changes to see which library CLIs would benefit from the new surface.

## What gets generated

Designed for AI agents. Every flag, every output format, every exit code is chosen because an agent will consume it. Human-friendly table output in the terminal. Auto-JSON when piped, no flag needed. `--compact` drops to high-gravity fields only (id, name, status, timestamps), 60-80% fewer tokens. Typed exit codes (`0`=success, `2`=usage, `3`=not found, `4`=auth, `5`=API, `7`=rate limited) let agents self-correct in one retry without parsing error text. `--dry-run` lets agents explore safely. Humans benefit from all of this too. Agent-native design is just good CLI design taken seriously.

Agent-first flags (every command): `--json`, `--select`, `--dry-run`, `--stdin`, `--csv`, `--compact`, `--quiet`, `--yes`, `--no-input`, `--no-cache`, `--no-color`. Auto-JSON when piped (no `--json` needed). Typed exit codes as above.

Actionable errors. Errors include the specific flag/arg that's wrong, the correct usage pattern, and the command path. Agents self-correct in one retry.

Bounded output. List commands show "Showing N results. To narrow: add --limit, --json --select, or filter flags." Token-conscious `--compact` mode returns only high-gravity fields, 60-80% fewer tokens.

Table stakes features (from the absorb gate). Every feature the top competitor has, classified and built before novel features. If schpet/linear-cli has `start` (git branch from issue), you get it. If 4ier/notion-cli has human-friendly filters, you get it. Anti-gaming rules prevent scorecard optimization over real features.

Data layer (high-gravity entities). Domain-specific SQLite tables with proper columns (not JSON blobs), FTS5 full-text search, incremental sync with cursor tracking, `sql` command for raw queries, domain-specific `UpsertX()` and `SearchX()` methods.

Workflow commands (from archetype): `stale`, `orphans`, `load`, `channel-health`, `reconcile`, etc.

Insight commands (Rung 5): `health` (composite score), `similar` (duplicate detection), `trends`, `bottleneck`, `forecast`, `patterns`.

Production-ready output. Command name normalization (`retrieve-a` -> `get`, `post` -> `create`, `patch` -> `update`); `.printing-press.json` provenance manifest; "Sources and Inspiration" section in each generated README; proxy-envelope support for APIs that wrap requests in a POST envelope; adaptive rate limiting on browser-sniffed APIs (start slow, ramp on success, back off on 429); minimum 1 test file per package; `.goreleaser.yaml` plus Homebrew formula plus GitHub Actions CI; REST or GraphQL specs both supported; MCP server auto-emitted at `cmd/api-mcp/main.go`; cursor-based pagination, batch SQLite transactions, tuned pragmas, `--since` incremental sync, and `--concurrency` parallel workers in every `sync` (discrawl-inspired).

### Domain archetypes

The profiler classifies every API into a domain archetype and auto-generates the right workflow + insight commands:

| Archetype | Detected by | Auto-generated commands |
|-----------|------------|------------------------|
| Project Management | issue/task/ticket resources, assignee fields, priority levels | `stale`, `orphans`, `load`, `health`, `similar` |
| Communication | message/channel/thread resources, threading fields | `channel-health`, `message-stats`, `health`, `similar` |
| Payments | charge/payment/invoice resources, amount/currency fields | `reconcile`, `revenue`, `health`, `similar` |
| Infrastructure | server/deploy/instance resources | `health`, `similar` |
| Content | document/page/block resources | `health`, `similar` |

The archetype is detected automatically from the spec. The entity mapper figures out which resource is the "primary entity" (issues for PM, messages for comms, charges for payments) and wires the templates accordingly.

## Verification & quality

Four mechanical checks before a CLI can ship: a two-tier scorecard, structural dogfood, proof of behavior, and (when an API key is present) a live read-only smoke test. No vibes, no self-assessment.

Three benchmarks shape the scorecard, all must pass:

1. Architecture (discrawl benchmark). Does it have a real data layer: domain-specific SQLite, FTS5, incremental sync, workflow commands?
2. Quality (gogcli benchmark). Does the code have proper output modes, typed errors, agent-native flags, doctor, README with cookbook?
3. Features (competitor benchmark). Would a user of the top competitor switch to this CLI?

Architecture without features is a toy. Features without architecture is a thin wrapper. Quality without either is polished nothing.

### Two-tier scorecard (100 points)

Inspired by Peter Steinberger's [gogcli](https://github.com/steipete/gogcli). Two tiers, weighted 50/50. Grade A = 85+.

Tier 1: infrastructure (50 points). Does the skeleton have the right patterns?

| Dimension | What it checks |
|-----------|---------------|
| Output Modes | --json, --csv, --select, --quiet, --compact, auto-JSON when piped |
| Auth | OAuth flow, format-aware headers (Bot/Bearer/Basic from spec) |
| Error Handling | Typed exits, retry with backoff, actionable error messages |
| Agent-Native | --json, --select, --dry-run, --stdin, --no-input, --compact, --yes |
| + 5 more | Terminal UX, README, Doctor, Local Cache, Breadth |

Tier 2: domain correctness (50 points). Does the code actually work?

| Dimension | What it checks |
|-----------|---------------|
| Path Validity | Generated paths exist in the OpenAPI spec |
| Auth Protocol | Auth format matches spec's securitySchemes |
| Data Pipeline | Sync calls domain-specific UpsertX(), not generic Upsert() |
| Sync Correctness | Real resources, nested paths, pagination, incremental cursors |
| Type Fidelity | String IDs (not int), required params marked, quality descriptions |
| Dead Code | No unwired flags, no uncalled functions, no ghost tables |

Why two tiers? A scorecard that only checks syntax ("does this string exist in the file?") misses semantics ("does this code actually work?"). The two-tier system forces both breadth and depth.

Anti-gaming rules prevent optimizing for score instead of features. Table stakes (features competitors have) are Priority 1. Scorecard optimization is Priority 4.

### Verification commands

```bash
# Quality scorecard: two-tier scoring (infrastructure + domain correctness)
printing-press scorecard --dir ./my-pp-cli --spec ./openapi.json

# Dogfood: catches dead flags, dead functions, auth mismatches, invalid paths
printing-press dogfood --dir ./my-pp-cli --spec ./openapi.json

# Runtime verification: tests every command against real API or mock server
printing-press verify --dir ./my-pp-cli --spec ./openapi.json --api-key $TOKEN

# Emboss audit: baseline snapshot for improvement cycle
printing-press emboss --dir ./my-pp-cli --spec ./openapi.json --audit-only
```

### Proof of behavior

The scorecard checks structure. Proof of Behavior checks data flow: does `sync.go` actually call `UpsertMessage` on a table that `search.go` queries?

Four behavioral proofs:

- Path Proof. Every URL in generated commands exists in the OpenAPI spec.
- Flag Proof. Every registered flag is referenced in at least one command.
- Pipeline Proof. Every SQLite table has a WRITE path (sync) and READ path (search/query).
- Auth Proof. Auth header format matches the spec's securitySchemes.

If any proof fails, auto-remediation removes dead code and re-verifies. Hallucinated paths and auth mismatches are hard FAIL gates.

### Live API testing

When you provide an API key at the start, Phase 5 runs read-only tests against the real API:

```
LIVE API TEST RESULTS
=====================
Auth:     PASS (200 OK on doctor)
List:     3/3 passed (users, channels, guilds)
Get:      1/1 passed (user abc123)
Sync:     PASS (5 pages synced, 12 blocks)
Search:   PASS (3 results for "a")

Verdict:  PASS - CLI works against real API
```

Safety: GET only, --limit 1, 10s timeout, stops on 401. Never creates, posts, or deletes anything.

### Auth doctor

`printing-press auth doctor` scans every installed printed CLI's `tools-manifest.json` and reports whether its declared env vars are set, unset, or suspicious. Fingerprints show the first four characters of each set value, never the full token.

```bash
printing-press auth doctor
printing-press auth doctor --json
```

Useful when an agent hits a 401 on a printed CLI: one command shows whether the token is missing, truncated, or shadowed by a stale value without having to inspect shell config. Offline, read-only, and exits 0 even when findings include "not set" or "suspicious" because this is diagnostic, not gating.

### Ship loop

"Is this shippable?" triggers a fix cycle: identify top 3 issues, fix them, re-score. Max 3 iterations. No more dead-end assessments.

## Library

Published CLIs live in the [Printing Press Library](https://github.com/mvanhorn/printing-press-library), organized by category. 24 CLIs across 17 categories, 17 with full MCP servers. Browse at [printingpress.dev](https://printingpress.dev) or run `/ppl` after installing the Library plugin.

A small sample, see the [full catalog](https://github.com/mvanhorn/printing-press-library#catalog) for all 24:

| CLI | Category | What it does |
|-----|----------|--------------|
| `espn-pp-cli` | Media and Entertainment | ESPN sports data: scores, stats, standings across 17 sports. |
| `flightgoat-pp-cli` | Travel | Kayak nonstop search plus sniffed Google Flights, in one call. |
| `linear-pp-cli` | Project Management | 50ms compound queries against a local Linear mirror. |
| `kalshi-pp-cli` | Payments | Trade prediction markets from the terminal. |
| `recipe-goat-pp-cli` | Food and Dining | Trust-aware ranking across 37 recipe sites. |

Each published CLI ships a research manuscript, verification proofs, and a `.printing-press.json` provenance manifest.

## Troubleshooting

**`go install` returns `410 Gone` or an auth error.** The repo is currently private. Set `GOPRIVATE` so Go fetches directly from GitHub instead of the public module proxy, and ensure you have GitHub access (SSH key or `gh auth login`):

```bash
GOPRIVATE=github.com/mvanhorn/* go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest
```

After the public launch, the plain command works for everyone.

**`/printing-press` slash command doesn't appear in Claude Code.** Restart your Claude Code session after installing the plugin. For Option A, confirm `claude --plugin-dir .` was run from the cloned repo. For Option B, run `/plugin list` to verify the install.

**`printing-press: command not found` after a successful `go install`.** `$GOPATH/bin` (default `~/go/bin`) isn't on your `PATH`. Add it to your shell profile.

**Live API smoke test reports 401.** Token unset or stale. Run `printing-press auth doctor` to see which env vars are missing or suspicious before reading shell config.

**Browser-sniff captures no useful endpoints.** The site likely uses websockets, gRPC, or aggressive bot detection. Try a HAR export from DevTools (`/printing-press --har ./capture.har`) instead of the live browser flow.

**Codex mode falls back to local generation.** Expected behavior after 3 consecutive Codex failures. Standard Opus mode takes over with no manual intervention.

## Limitations

- **Requires Go 1.22+ and Claude Code.** No standalone distribution today; the slash command is the supported entry point.
- **Generated CLIs are domain-shaped, not vendor-replacements.** A `<api>-pp-cli` covers the agent power-user surface, not every back-office knob a vendor's official CLI ships.
- **Browser-sniff requires manual capture.** You point a browser at the site (or import a HAR); the press doesn't crawl autonomously.
- **Live verify is read-only.** Phase 5 runs GET only and never mutates. Real write-path coverage lives in unit tests and the dogfood structural checks.
- **Scoring is structural, not end-user QA.** A Grade A scorecard means the CLI follows the patterns; it doesn't replace using the CLI in anger for an afternoon.
- **`regen-merge` is macOS+Linux only.** Windows isn't supported for the regen-merge subcommand today.

## FAQ

**Why not Speakeasy, Fern, or openapi-generator?** Those wrap endpoints. We wrap endpoints AND generate the discrawl-style data layer (SQLite, FTS5, sync, compound commands) AND the MCP server AND the agent-native UX (typed exit codes, `--compact`, auto-JSON-when-piped). The output is shaped for an agent that will call it thousands of times a day.

**Does it work without Claude?** The binary works standalone (research, generation, verification, scoring), but the curated agent loop — research absorption, novel-feature suggestion, ship cycle — runs through the `/printing-press` slash command in Claude Code. Bring your own agent loop if you want to skip it.

**Does it require an OpenAPI spec?** No. Three input modes: a spec (`--spec`), a HAR file (`--har`), or just a URL. The browser-sniff gate launches a browser, captures traffic, and reverse-engineers the spec for sites that don't publish one.

**How fresh is the local SQLite cache?** Configurable. Without `cache.enabled`, you run `sync` manually. With `cache.enabled` and `--data-source auto`, the CLI runs a bounded pre-read refresh before serving local data. `--data-source local` skips refresh; `--data-source live` bypasses the cache entirely.

**What does shipping a CLI cost in tokens?** Standard Opus mode runs end-to-end on Opus. Codex mode (`/printing-press <api> codex`) offloads Phase 3 code generation to Codex CLI for ~60% fewer Opus tokens. Both produce equivalent quality.

**Can I run the verification tools on a CLI I built by hand?** Yes. `printing-press scorecard`, `dogfood`, and `verify` accept any directory + spec. Tier 1 of the scorecard checks for agent-native patterns; Tier 2 checks paths/auth/data-flow against the spec.

## Contributing

Bug reports, feature requests, and PRs are welcome. A few conventions to know before opening a PR:

- **Conventional commits + scopes.** PR titles follow `type(scope): description` with `cli`, `skills`, or `ci` as the scope. The `PR Title` GitHub Action enforces this. See [AGENTS.md](AGENTS.md) for the full rule set.
- **Releases are automated.** release-please opens release PRs as conventional commits accumulate. Don't manually edit version numbers — three files stay in sync via release-please annotations.
- **Pre-commit and pre-push hooks.** `lefthook install --reset-hooks-path` wires up `gofmt` (pre-commit) and `golangci-lint` (pre-push). The same lint config runs in CI.
- **Conventions and glossary live in [AGENTS.md](AGENTS.md).** Read it before larger changes — terminology like "machine vs printed CLI", "absorb gate", and "shipcheck" is documented there.

For larger changes, opening an issue first to align on scope is helpful.

## Development

```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
go fmt ./...
golangci-lint run ./...
```

A pre-push lefthook hook runs `golangci-lint` on changed files; the same config (`.golangci.yml`) runs in CI.

Install hooks with:

```bash
brew install lefthook
lefthook install --reset-hooks-path
```

Use `--reset-hooks-path` so stale local `core.hooksPath` settings do not block hook sync. Avoid `lefthook install --force` unless intentionally overriding a custom hooks path.

If you installed via [Option A — Clone and run](#option-a--clone-and-run-recommended-for-active-users), `claude --plugin-dir .` already loads `/printing-press` from your working copy, so local skill edits take effect on the next session start. See [AGENTS.md](AGENTS.md) for full conventions, glossary, and release flow.

### Golden Output Harness

Golden output checks compare deterministic, offline `printing-press` commands against committed stdout, stderr, exit-code, and selected artifact fixtures:

```bash
scripts/golden.sh verify
```

Use update mode only after an intentional behavior change:

```bash
scripts/golden.sh update
```

The harness rebuilds `./printing-press`, writes actual outputs under `.gotmp/golden/actual`, and compares them to `testdata/golden/expected`. Cases live under `testdata/golden/cases/<case-name>/`; `command.txt` defines the offline command, and `artifacts.txt` lists behaviorally important generated files to compare. Normalization is intentionally narrow: machine-specific paths, deterministic JSON formatting, and known provenance fields like generated timestamps. CI runs this as a separate `Golden` workflow, not inside `go test ./...`.

The generated-CLI golden uses `testdata/golden/fixtures/golden-api.yaml`, a purpose-built OpenAPI fixture for the Printing Press. Extend that fixture when the machine gains new deterministic generation capabilities that should be protected by artifact goldens. Update mode refuses dirty worktrees unless `GOLDEN_ALLOW_DIRTY=1` is set, so fixture churn stays intentional.

## Credits

- Peter Steinberger ([@steipete](https://github.com/steipete)). [discrawl](https://github.com/steipete/discrawl) and [gogcli](https://github.com/steipete/gogcli) set the bar. The quality scoring system is inspired by his work; discrawl's sync architecture directly influenced the printing press templates.
- Trevin Chow ([@trevin](https://x.com/trevin)). [10 Principles for Agent Native CLIs](https://trevinsays.com/p/10-principles-for-agent-native-clis) shaped the agent-first template design. Co-creator shipping PRs daily.
- Ramp ([@tryramp](https://github.com/ramp-public/ramp-cli)). Their agent-first CLI inspired auto-JSON piping, --no-input, and --compact output.
- The community filers and contributors whose issues and PRs nudged the catalog forward.

## License

MIT
