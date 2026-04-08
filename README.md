# CLI Printing Press

Just making a CLI is not hard. Making a CLI that understands the power user is extremely hard. And the power user in 2026 is an AI agent.

Claude Code, Codex, Gemini CLI, Cursor - they call CLIs thousands of times a day. Every printing press CLI is designed for agents first: `--json` by default when piped, typed exit codes for self-correction, `--compact` for token efficiency, `--dry-run` for safe exploration. Humans get the same great experience, but agents are the primary design target.

```bash
/printing-press HubSpot                              # From the catalog (18 APIs ready)
/printing-press --spec ./openapi.yaml                # From a local spec
/printing-press --har ./capture.har --name ESPN      # From captured browser traffic
/printing-press https://postman.com/explore          # From a URL (auto-detects intent)
/printing-press HubSpot codex                        # Codex mode - 60% fewer Opus tokens
/printing-press emboss notion                        # Second pass: improve an existing CLI
```

One command. Lean loop. Produces a Go CLI + MCP server that absorbs every feature from every competing tool, then transcends with compound use cases only possible with local data. REST, GraphQL, or sniffed traffic - no OpenAPI spec required.

### Get it

Install the binary, then start a Claude Code session and run each command one after another:

```bash
go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest
```

```
/plugin marketplace add mvanhorn/cli-printing-press
/plugin install cli-printing-press@cli-printing-press
/reload-plugins
```

## Why These CLIs Win

Most generators wrap endpoints and stop. Printing Press generates CLIs that **understand the domain**.

**Local-first data layer.** High-gravity resources get domain-specific SQLite tables (not JSON blobs), FTS5 full-text search indexes, and incremental sync with cursor tracking. `sync` pulls data down. `search` finds it in milliseconds. `sql` lets power users query directly. All offline, all local.

**Compound commands no wrapper can do.** Once data lives in SQLite, commands like `stale`, `health`, `bottleneck`, and `reconcile` become possible - they join across resources and analyze history. A stateless API wrapper literally cannot do this.

**Agent-native by default.** Human-friendly tables when you're in a terminal. Auto-JSON when piped - no `--json` flag needed. `--compact` drops to high-gravity fields only (60-80% fewer tokens). Typed exit codes (`0`/`2`/`3`/`4`/`5`/`7`) let agents self-correct without parsing error text. `--dry-run` for safe exploration. Every flag exists because an AI agent will call it thousands of times a day.

**No spec? No problem.** Don't have an OpenAPI spec? Point the press at a website. It launches a browser, captures traffic, reverse-engineers the API, and generates the spec for you. ESPN, Postman Explore, internal tools - if you can click through it, the press can build a CLI for it.

**Dual interface from one spec.** Every API gets a Cobra CLI (`<api>-pp-cli`) and an MCP server (`<api>-pp-mcp`). Same client, same store, same auth. Shell agents use the CLI. IDE agents use MCP. Zero code duplication.

**Verified, not vibes.** Four mechanical checks - scorecard, dogfood, proof-of-behavior, live API smoke test - catch hallucinated paths, dead flags, auth mismatches, and broken data pipelines before you ship.

**Credits its sources.** Every generated README includes a Sources & Inspiration section that credits the ecosystem tools studied during research. We built on giants' shoulders and we say so.

## Every Endpoint. Every Insight. One Command.

Discord's API has 300+ endpoints. Most generators stop there - wrap every endpoint, ship it, done. But [discrawl](https://github.com/steipete/discrawl) - Peter Steinberger's Discord tool - ignores most of them. It ships 11 commands: `sync`, `search`, `sql`, `tail`, `mentions`, `members`. **583 stars.**

Why does the 11-command tool win? Because Steinberger saw something Discord's own API designers didn't: **conversations are institutional knowledge.** Every message thread is a document that should be archived, indexed, and searched locally. Those 11 commands embody that insight. The 300 endpoint wrappers don't.

Until now, you had to choose: breadth (wrap every endpoint) or depth (understand the user). The printing press eliminates that choice. It generates the full API surface AND matches every feature the top competitor has AND adds the discrawl-style intelligence layer AND an MCP server. One spec in. Everything out.

## Absorb & Transcend

The GOAT CLI isn't built by finding gaps. It's built by absorbing every good idea and compounding on top.

**Layer 1 - Absorb:** Before generating, the ecosystem absorb gate catalogs every feature from every Claude Code plugin, MCP server, community skill, competing CLI, and automation script for your API. Every feature becomes a row in the absorb manifest - something our CLI must match AND beat with offline support, agent-native output, and SQLite persistence. The system even auto-suggests novel features it thinks are missing from the ecosystem before you approve the manifest.

**Layer 2 - Transcend:** Once you have everything in SQLite, compound use cases emerge that no stateless tool can do. Velocity tracking requires historical cycle data. Churn risk requires joining charges + subscriptions. Bottleneck detection requires the full issue relationship graph. These are the Non-Obvious Insight commands - and they only work because Layer 1 put everything in a local database.

The GOAT = everything everyone else does + everything nobody else thought of.

## The Non-Obvious Insight

Every API has a secret identity. The data it exposes is useful for something its creators never designed for. The printing press finds that secret and builds a CLI around it.

The **Non-Obvious Insight (NOI)** is a one-sentence reframe:

```
"[API] isn't just [obvious thing]. It's [non-obvious thing].
 Every [data point] is a signal about [hidden truth]."
```

| API | What they think it is | What it actually is |
|-----|----------------------|---------------------|
| Discord | A chat app | A **searchable knowledge base**. Every message thread is institutional memory. |
| Linear | An issue tracker | A **team behavior observatory**. Every state change is a signal about how your team actually works vs. how they think they work. |
| Stripe | A payment processor | A **business health monitor**. Every failed charge and churn event is a signal about product-market fit. |
| GitHub | A code host | An **engineering culture fingerprint**. Every review turnaround and merge pattern is a signal about how your team ships. |
| Notion | A doc editor | A **knowledge decay detector**. Every stale page and orphaned database is a signal about what your team has forgotten. |
| HubSpot | A CRM | Your company's **relationship memory**. Every deal stage transition, email open, and meeting log is a signal about pipeline health and rep performance. |
| Slack | Messaging | An **organizational nervous system**. Every response time and channel silence is a signal about team health. |
| ESPN | Sports data | A **betting intelligence terminal**. Every injury report, lineup change, and odds movement is a signal about game outcomes. |

The NOI is the creative DNA of every CLI the press generates. Phase 0 cannot complete without one. If the LLM can't write an NOI, the research wasn't deep enough.

The printing press automates what Steinberger does intuitively: look at an API, see what power users actually do with it, and build the commands that matter - then also wrap every endpoint for completeness.

## How I Knew This Was Real

I was choosing between Peter Steinberger's [gogcli](https://github.com/steipete/gogcli) (6.5K+ stars, Go) and Google's official [Workspace CLI](https://github.com/googleworkspace/cli) (10K+ stars in a week, Rust). I ran [/last30days](https://github.com/mvanhorn/last30days-skill) - my recency research skill - across 34 X posts, 5 YouTube videos, and 10 web sources.

The verdict: **use gogcli**. The newer, official tool with 10x the API coverage lost to the older third-party one. As one user put it: "my preference is 100% gogcli since I have my agent working a lot with Google Docs and sheets, and gogcli just makes him able to do what he needs to do."

Breadth doesn't beat depth. Understanding the user beats understanding the API.

## The Creativity Ladder

Most API CLIs stop at Rung 1. The printing press climbs to Rung 5.

| Rung | What It Is | Auto-Generated? | Example |
|------|-----------|-----------------|---------|
| 1 | API wrapper commands | Yes (from spec) | `issue create --title "..."` |
| 2 | Output formatting | Yes (always) | `--json`, `--select`, `--csv`, `--dry-run` |
| 3 | Local persistence | Yes (conditional) | `sync`, `search`, `export`, `tail` |
| 4 | **Domain analytics** | **Yes (from archetype)** | `stale --days 30`, `orphans`, `load` |
| 5 | **Behavioral insights** | **Yes (from archetype)** | `health` (composite score), `similar` (duplicate detection) |

Rung 3 is table stakes. Rung 4 is where discrawl lives. Rung 5 is where nobody else is yet.

The press generates the API wrapper in Phase 2 (Rung 1-2). Then it generates the discrawl-style data layer and workflow commands in Phase 3 (Rung 3-5) from domain archetype templates. Both in one run.

## Why Not Just CLIs - CLIs + MCP

The NOI is the creative intelligence. The printing press generates **both interfaces** from one spec:

- **`<api>-pp-cli`** - Cobra CLI for humans + shell agents (Claude Code, Codex, Gemini CLI)
- **`<api>-pp-mcp`** - MCP server for Claude Desktop, Cursor, Windsurf, Cline - auto-discovered, no shell needed

Same `internal/client`, same `internal/store`, same auth. Two binaries, zero code duplication.

**CLIs win for agents:** 100x fewer tokens than MCP tool definitions. LLMs were trained on shell interactions. Exit code 0 = done. `--json | jq` is a first-class composition pattern.

**MCP wins for IDE integration:** Claude Desktop and Cursor discover tools automatically via MCP. No shell needed. The MCP server exposes the same operations as the CLI - including the data layer (sync, search, sql).

```
One spec  -->  printing-press generate  -->  <api>-pp-cli (cobra)  +  <api>-pp-mcp (MCP server)
                                              |                         |
                                              same internal/client, internal/store
```

Every API that gets a CLI+MCP becomes instantly accessible to every AI coding tool. The printing press is the factory.

## Domain Archetypes

The profiler classifies every API into a domain archetype and auto-generates the right workflow + insight commands:

| Archetype | Detected By | Auto-Generated Commands |
|-----------|------------|------------------------|
| **Project Management** | issue/task/ticket resources, assignee fields, priority levels | `stale`, `orphans`, `load`, `health`, `similar` |
| **Communication** | message/channel/thread resources, threading fields | `channel-health`, `message-stats`, `health`, `similar` |
| **Payments** | charge/payment/invoice resources, amount/currency fields | `reconcile`, `revenue`, `health`, `similar` |
| **Infrastructure** | server/deploy/instance resources | `health`, `similar` |
| **Content** | document/page/block resources | `health`, `similar` |

The archetype is detected automatically from the spec. The entity mapper figures out which resource is the "primary entity" (issues for PM, messages for comms, charges for payments) and wires the templates accordingly.

## How It Works

The fast path is a lean loop. Artifacts still matter, but only when they directly improve the next phase.

```
Phase 0     Resolve + Reuse           (1-3 min)    Reuse research, detect tokens, resolve spec or URL
Phase 1     Research Brief            (5-10 min)   API identity, competitors, data layer, product thesis
Phase 1.5   Ecosystem Absorb Gate    (5-10 min)   Catalog every MCP/skill/CLI feature → absorb manifest + novel suggestions
Phase 1.7   Sniff Gate (if needed)   (2-5 min)    Browser capture, HAR import, discovery provenance
Phase 2     Generate                  (1-2 min)    Go CLI + MCP server from spec with validation
Phase 3     Build The GOAT            (10-20 min)  ALL absorbed features + transcendence commands
Phase 4     Shipcheck                 (3-8 min)    Dogfood + verify --fix + scorecard as one verification block
Phase 5     Live Smoke (optional)     (2-5 min)    Read-only API smoke + data-flow check
```

**Three entry paths.** Got an OpenAPI spec? Use `--spec`. Got a URL to a website with no docs? The sniff gate launches a browser, captures traffic, and generates the spec. Got a HAR file from DevTools? Pass `--har`. The press handles all three.

**18 APIs in the catalog.** Asana, DigitalOcean, Discord, Front, GitHub, HubSpot, LaunchDarkly, Pipedrive, Plaid, Postman, SendGrid, Sentry, Square, Stripe, Stytch, Telegram, Twilio - plus Petstore for testing. Each pre-verified with spec URL, auth type, and category.

**Discovery provenance.** When the press sniffs a website, it archives everything - pages visited, endpoints discovered, response samples, rate limiting events - into a `discovery/` manuscript alongside the research and proofs. Full audit trail.

### Codex Mode (opt-in)

```bash
/printing-press HubSpot codex    # Offload code generation to Codex CLI (~60% Opus token savings)
/printing-press HubSpot          # Standard Opus mode (default)
```

When you add `codex`, Phase 3's code generation tasks are delegated to Codex CLI. Claude stays the brain (research, planning, scoring, review). Codex does the hands (writing Go code from scoped prompts). Same quality, 60% fewer Opus tokens. If Codex fails 3 times in a row, the press falls back to doing it locally - no manual intervention needed.

### Emboss Mode (second pass)

```bash
/printing-press emboss notion              # By API name
/printing-press emboss notion-pp-cli       # By CLI name
/printing-press emboss ~/printing-press/library/notion          # By full path
```

Already generated a CLI? Emboss runs a focused improvement cycle: audit baseline (verify + scorecard), re-research what's changed, identify top 5 improvements, build them, re-verify, report the delta. Offered at the end of every run - never triggered automatically.

## What Gets Generated

**Designed for AI agents.** Every flag, every output format, every exit code is chosen because an agent will consume it. Human-friendly table output in the terminal. Auto-JSON when piped - no flag needed. `--compact` drops to high-gravity fields only (id, name, status, timestamps) - 60-80% fewer tokens. Typed exit codes (`0`=success, `2`=usage, `3`=not found, `4`=auth, `5`=API, `7`=rate limited) let agents self-correct in one retry without parsing error text. `--dry-run` lets agents explore safely. Humans benefit from all of this too - agent-native design is just good CLI design taken seriously.

**Agent-first flags** (every command): `--json`, `--select`, `--dry-run`, `--stdin`, `--csv`, `--compact`, `--quiet`, `--yes`, `--no-input`, `--no-cache`, `--no-color`. Auto-JSON when piped (no `--json` needed). Typed exit codes (`0`=success, `2`=usage, `3`=not found, `4`=auth, `5`=API, `7`=rate limited).

**Actionable errors**: errors include the specific flag/arg that's wrong, the correct usage pattern, and the command path. Agents self-correct in one retry.

**Bounded output**: list commands show "Showing N results. To narrow: add --limit, --json --select, or filter flags." Token-conscious `--compact` mode returns only high-gravity fields - 60-80% fewer tokens.

**Table stakes features** (from the absorb gate): every feature the top competitor has, classified and built before novel features. If schpet/linear-cli has `start` (git branch from issue), you get it. If 4ier/notion-cli has human-friendly filters, you get it. Anti-gaming rules prevent scorecard optimization over real features.

**Data layer** (high-gravity entities): domain-specific SQLite tables with proper columns (not JSON blobs), FTS5 full-text search, incremental sync with cursor tracking, `sql` command for raw queries, domain-specific `UpsertX()` and `SearchX()` methods.

**Workflow commands** (from archetype): `stale`, `orphans`, `load`, `channel-health`, `reconcile`, etc.

**Insight commands** (Rung 5): `health` (composite score), `similar` (duplicate detection), `trends`, `bottleneck`, `forecast`, `patterns`.

**Command name normalization**: generated names like `retrieve-a` become `get`, `post` becomes `create`, `patch` becomes `update`. Clean names, not operationId garbage.

**Provenance manifest**: every generated CLI includes a `.printing-press.json` with spec source, generation timestamp, press version, category, and run ID. In that manifest, `api_name` is the canonical API identity and `cli_name` is the executable name; slug-keyed library directories are a separate concern. Self-describing even in isolation.

**Source credits**: the generated README includes a "Sources & Inspiration" section crediting the ecosystem tools and competing projects studied during research.

**Proxy-envelope support**: for APIs that wrap all requests in a POST envelope (like Postman's `_api/ws/proxy`), the press detects the pattern during sniffing and generates a specialized client.

**Adaptive rate limiting**: sniffed APIs get conservative pacing - start slow, ramp up on success, back off on 429s. Never aborts, always recovers.

**Tests**: minimum 1 test file per package (store, cli). Table-driven tests for data layer queries and workflow commands.

**Distribution scaffold**: `.goreleaser.yaml`, Homebrew formula, GitHub Actions CI. A CLI that can only be `go install`'d is not a real CLI.

**REST + GraphQL**: OpenAPI specs generate full CLIs. GraphQL SDL files are parsed with Relay pagination detection and produce the same domain-specific output.

**MCP server** (auto-generated): Every CLI gets a companion `cmd/api-mcp/main.go` that exposes the same operations as MCP tools. Same client, same store, same auth. Works with `claude mcp add ./bin/api-mcp`.

**Sync performance** (discrawl-inspired): Cursor-based pagination, batch SQLite transactions, tuned pragmas (`synchronous=NORMAL`, `mmap_size=256MB`), `--since` incremental sync, `--concurrency` parallel workers, progress reporting to stderr.

## Quality Scoring - Three Benchmarks

Three benchmarks, not one. All must pass:

1. **Architecture** (discrawl benchmark): Does it have a real data layer - domain-specific SQLite, FTS5, incremental sync, workflow commands?
2. **Quality** (gogcli benchmark): Does the code have proper output modes, typed errors, agent-native flags, doctor, README with cookbook?
3. **Features** (competitor benchmark): Would a user of the top competitor switch to this CLI?

Architecture without features is a toy. Features without architecture is a thin wrapper. Quality without either is polished nothing.

Inspired by Peter Steinberger's [gogcli](https://github.com/steipete/gogcli). Two tiers, 100 points max, weighted 50/50. Grade A = 85+.

**Tier 1: Infrastructure** (50 points) - does the skeleton have the right patterns?

| Dimension | What It Checks |
|-----------|---------------|
| Output Modes | --json, --csv, --select, --quiet, --compact, auto-JSON when piped |
| Auth | OAuth flow, format-aware headers (Bot/Bearer/Basic from spec) |
| Error Handling | Typed exits, retry with backoff, actionable error messages |
| Agent-Native | --json, --select, --dry-run, --stdin, --no-input, --compact, --yes |
| + 5 more | Terminal UX, README, Doctor, Local Cache, Breadth |

**Tier 2: Domain Correctness** (50 points) - does the code actually work?

| Dimension | What It Checks |
|-----------|---------------|
| Path Validity | Generated paths exist in the OpenAPI spec |
| Auth Protocol | Auth format matches spec's securitySchemes |
| Data Pipeline | Sync calls domain-specific UpsertX(), not generic Upsert() |
| Sync Correctness | Real resources, nested paths, pagination, incremental cursors |
| Type Fidelity | String IDs (not int), required params marked, quality descriptions |
| Dead Code | No unwired flags, no uncalled functions, no ghost tables |

**Why two tiers?** A scorecard that only checks syntax ("does this string exist in the file?") misses semantics ("does this code actually work?"). The two-tier system forces both breadth and depth.

Anti-gaming rules prevent optimizing for score instead of features. Table stakes (features competitors have) are Priority 1. Scorecard optimization is Priority 4.

```bash
# Runtime verification: tests every command against real API or mock server
printing-press verify --dir ./hubspot-pp-cli --spec ./hubspot-spec.json --api-key $TOKEN

# Emboss audit: baseline snapshot for improvement cycle
printing-press emboss --dir ./hubspot-pp-cli --spec ./hubspot-spec.json --audit-only

# Quality scorecard: two-tier structural scoring
printing-press scorecard --dir ./hubspot-pp-cli --spec ./hubspot-spec.json

# Mechanical dogfood: catches dead flags, invalid paths, auth mismatches
printing-press dogfood --dir ./hubspot-pp-cli --spec ./hubspot-spec.json
```

## Library

Published CLIs live in [printing-press-library](https://github.com/mvanhorn/printing-press-library), organized by category.

| CLI | Category | API | Install |
|-----|----------|-----|---------|
| `espn-pp-cli` | Media & Entertainment | ESPN sports data - scores, stats, standings across 17 sports and 139 leagues | `go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/espn-pp-cli/...@latest` |
| `linear-pp-cli` | Project Management | Linear - issues, cycles, teams, projects with GraphQL | `go install github.com/mvanhorn/printing-press-library/library/project-management/linear-pp-cli/...@latest` |

Each published CLI includes research manuscripts, verification proofs, and a `.printing-press.json` provenance manifest. The full catalog has **18 APIs** ready to generate - run `/printing-press-catalog` to browse.

## Quick Start

### Install

Install the binary (requires Go 1.22+):

```bash
go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest
```

Then start a Claude Code session and run each command one after another:

```
/plugin marketplace add mvanhorn/cli-printing-press
/plugin install cli-printing-press@cli-printing-press
/reload-plugins
```

No repo checkout needed. The binary embeds its own catalog data and the plugin provides the `/printing-press` skill.

### Run It

```bash
/printing-press HubSpot                              # From the catalog
/printing-press --spec ./openapi.yaml                # From a local spec
/printing-press --har ./capture.har --name ESPN      # From captured browser traffic
/printing-press https://postman.com/explore          # From a URL (auto-detects intent)
/printing-press Stripe codex                         # Codex mode - 60% fewer Opus tokens
```

Each run produces two binaries (`<api>-pp-cli` + `<api>-pp-mcp`), research documents, verification proofs, and a Quality Score.

By default, active and published output are separated:

- Active managed runs work in `~/printing-press/.runstate/<scope>/runs/<run-id>/working/<api>-pp-cli`
- Published CLIs go to `~/printing-press/library/<api>`
- Archived manuscripts go to `~/printing-press/manuscripts/<api>/<run-id>/`
- Manuscripts are split into `research/`, `proofs/`, `discovery/`, and `pipeline/`

`<scope>` is derived from the current git checkout path, so parallel worktrees do not stomp on each other. If you pass `--output`, that overrides the generated CLI location for that command.

### Publish

When you're happy with a CLI, publish it to the library:

```bash
/printing-press-publish linear                       # Validates, packages, creates PR
```

## Verification Tools

Four layers of mechanical validation - no vibes, no self-assessment.

```bash
# Quality Scorecard: two-tier scoring (infrastructure + domain correctness)
printing-press scorecard --dir ./my-pp-cli --spec ./openapi.json

# Dogfood: catches dead flags, dead functions, auth mismatches, invalid paths
printing-press dogfood --dir ./my-pp-cli --spec ./openapi.json
```

### Proof of Behavior

The scorecard checks structure. Proof of Behavior checks data flow - does `sync.go` actually call `UpsertMessage` on a table that `search.go` queries?

Four behavioral proofs:
- **Path Proof**: Every URL in generated commands exists in the OpenAPI spec
- **Flag Proof**: Every registered flag is referenced in at least one command
- **Pipeline Proof**: Every SQLite table has a WRITE path (sync) and READ path (search/query)
- **Auth Proof**: Auth header format matches the spec's securitySchemes

If any proof fails, auto-remediation removes dead code and re-verifies. Hallucinated paths and auth mismatches are hard FAIL gates.

### Live API Testing

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

### Ship Loop

"Is this shippable?" triggers a fix cycle: identify top 3 issues, fix them, re-score. Max 3 iterations. No more dead-end assessments.

## Development

### Build locally

```bash
go build -o ./printing-press ./cmd/printing-press
```

Always use `./printing-press` (relative path). Multiple worktrees may run concurrently - absolute paths like `/tmp/printing-press` will collide.

### Use local skills

The repo is a Claude Code plugin. To test your local skill changes instead of the marketplace version:

```bash
claude --plugin-dir .
```

This loads skills, agents, and hooks from your working copy. Inside the session, `/printing-press` runs your local `SKILL.md`, not the installed marketplace version. After editing a skill, run `/reload-plugins` to pick up changes without restarting.

### Lint setup

Install git hooks so lint errors are caught before they reach CI:

```bash
brew install lefthook
lefthook install
```

This adds a pre-push hook that runs `golangci-lint` on changed files. The same linter config (`.golangci.yml`) runs in CI - lefthook just catches failures locally first.

If you also want `golangci-lint` locally (v2 required — the config uses `version: "2"`):

```bash
# macOS
brew install golangci-lint

# or via the official install script
curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin
```

## Credits

- **Peter Steinberger** ([@steipete](https://github.com/steipete)) - [discrawl](https://github.com/steipete/discrawl) and [gogcli](https://github.com/steipete/gogcli) set the bar. The quality scoring system is inspired by his work. discrawl's sync architecture directly influenced the printing press templates.
- **Trevin Chow** ([@trevin](https://x.com/trevin)) - [7 Principles for Agent-Friendly CLIs](https://x.com/trevin/status/2037250000821059933) shaped the agent-first template design. Co-builder shipping PRs daily.
- **Ramp** ([@tryramp](https://github.com/ramp-public/ramp-cli)) - Their agent-first CLI inspired auto-JSON piping, --no-input, and --compact output.

## License

MIT
