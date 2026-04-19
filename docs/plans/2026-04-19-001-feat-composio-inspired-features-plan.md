---
title: "feat: Composio-inspired features for the Printing Press - stack-ranked absorption plan"
type: feat
status: active
date: 2026-04-19
deepened: 2026-04-19
---

# feat: Composio-inspired features for the Printing Press - stack-ranked absorption plan

## Overview

Composio is a hosted agent-integration platform covering 1,000+ toolkits. Their CLI, MCP hosting, trigger system, and unified auth layer are the strongest reference points on the market for what a "super CLI" looks like in 2026. This plan does a deep feature inventory of Composio, cross-references it against what the Printing Press already ships (machine + library + megamcp), stack-ranks the absorbable features, and proposes implementation units for the top tier only.

The plan is intentionally research-first and decision-forward. The stack ranking is the primary artifact. Implementation units are scoped to the S-tier and A-tier items; B and C tiers are deferred to Future Considerations so the user can approve the cut before we start cutting work.

## Problem Frame

The Printing Press is a CLI factory. For any API, it generates a printed CLI plus a matching MCP server with local SQLite, FTS5 search, compound commands, domain archetypes, verify/dogfood/scorecard, and optional sniff-gate discovery. The library ships 21 printed CLIs and an aggregate megamcp server with dynamic activation, setup_guide, and cross-API tool search.

Composio solves a different shape of the same problem. Instead of generating one great local CLI per API, they host one platform that normalises auth, execution, discovery, and event delivery across thousands of APIs. A Composio user runs `composio login` once, runs `composio link linkedin`, and then every agent-framework (Claude Agent SDK, OpenAI Agents, LangChain, MCP clients) has authenticated LinkedIn access without per-API onboarding.

The gap the user wants to close: which Composio features would move the Printing Press forward without breaking its local-first, agent-native, "two binaries per API + no backend" philosophy? That is the question this plan answers.

## Requirements Trace

- R1. Inventory Composio's CLI, MCP, triggers, auth, and ecosystem features in enough depth to make informed decisions.
- R2. Inventory the Printing Press's current feature set (machine, library, megamcp) in enough depth to spot real gaps.
- R3. Stack-rank Composio features by absorbability into the Printing Press using explicit criteria (impact, reach, effort, fit, moat).
- R4. For the top ranked items, produce implementation units concrete enough to hand to `/ce:work` without another planning pass.
- R5. Explicitly mark out-of-scope items so we do not drift into cloning the parts of Composio that fight PP's philosophy (hosted dashboard, Python sandbox, multi-tenant SaaS).

## Scope Boundaries

In scope:
- Research synthesis and stack ranking of Composio features worth absorbing.
- Implementation units for the S-tier and A-tier features.
- Impact analysis across the machine, printed CLIs, and library/megamcp.

Out of scope (explicit non-goals):
- Building a hosted backend or dashboard.
- Python sandbox or remote filesystem for tool execution.
- Multi-tenant per-user MCP URLs.
- Rewriting printed CLIs to route through a central server.
- White-label or branded auth screens.
- Copying Composio's intent-routing LLM layer (agents already search_tools via megamcp).

## Context & Research

### Composio feature inventory

Collected from composio.dev, docs.composio.dev, their CLI reference page, MCP overview, triggers doc, and the ComposioHQ/skills repo.

CLI surface (from `composio --help` / docs):

| Command | What it does |
|---------|--------------|
| `composio login [-y]` | One-time auth with Composio backend. `-y` for CI. |
| `composio whoami` | Show account + workspace. |
| `composio link <toolkit>` | Kicks off OAuth for a specific toolkit (LinkedIn, Stripe, Slack, etc.). `--no-wait` prints auth URL and exits. |
| `composio connected-accounts list --toolkits X` | Inventory of linked accounts. |
| `composio tools list --toolkit X` | Tool catalog per toolkit. |
| `composio tools info TOOL_SLUG` | Schema / params / description for one tool. |
| `composio search "query" [--toolkits X] [--human]` | Cross-toolkit semantic search. JSON by default. |
| `composio execute TOOL_SLUG -d <json\|@file\|->` | Run a tool. `--dry-run`, `--get-schema`, `--skip-connection-check`, `--parallel`. |
| `composio proxy --toolkit X -X METHOD /path [-H ...] [-d ...]` | Authenticated raw HTTP using linked creds. |
| `composio run --file workflow.ts` | Execute a TypeScript/JS workflow file. |
| `composio generate ts [--toolkits X] [--compact] [--transpiled] [--type-tools]` | Typed SDK codegen. |
| `composio generate py [--toolkits X]` | Python codegen. |
| `composio dev init` | Initialise local dev context. |
| `composio dev playground-execute --user-id X` | Test against playground user. |
| `composio dev listen --toolkits X [--table]` | Live-tail triggers and logs. |
| `composio dev logs tools` / `logs triggers` | Historical logs. |
| `composio artifacts cwd` | Path to session artifacts. |
| Env: `COMPOSIO_API_KEY`, `COMPOSIO_BASE_URL`, `COMPOSIO_CACHE_DIR`, `COMPOSIO_SESSION_DIR`, `COMPOSIO_LOG_LEVEL`, `COMPOSIO_DISABLE_TELEMETRY`, webhook secret. |

MCP surface:
- Server config created via dashboard or API, exposes a subset of toolkits and `allowed_tools`.
- `composio.mcp.generate(user_id, mcp_config_id)` returns a per-user URL: `https://backend.composio.dev/v3/mcp/SERVER_ID?user_id=USER_ID`.
- Requires `x-api-key` header when `require_mcp_api_key` is on.
- Works with any MCP client (Claude Desktop, Cursor, OpenAI Agents, Windsurf, Cline).

Triggers surface:
- Trigger types per toolkit (e.g. `GITHUB_COMMIT_EVENT`, `SLACK_NEW_MESSAGE`, `GMAIL_NEW_EMAIL`).
- Webhook delivery for apps with native webhooks; polling (15-min floor) otherwise.
- Create trigger instance scoped to user + connected account + config params.
- Webhook signature verification built-in.

Agent-framework integrations: Claude Agent SDK, Anthropic SDK, OpenAI Agents, OpenAI, Gemini, Vercel AI, LangChain, LangGraph, CrewAI, LlamaIndex, Mastra, Cloudflare Workers. Python and TypeScript parity for most.

Claude Code integration: `npx skills add composiohq/skills` installs skills for tool-router, auth, toolkits, triggers. Heavy emphasis on "identify user, create session, get tools" as the canonical flow.

Security posture: SOC2 + ISO 27001:2022, bring-your-own-cloud, fine-grained data access controls, managed OAuth rotation, scoped inline authorization.

### Printing Press feature inventory (current state, 2026-04-19)

Machine (`cli-printing-press`):
- Generates `<api>-pp-cli` (Cobra) plus `<api>-pp-mcp` (MCP server) from OpenAPI 3, GraphQL SDL, HAR, or internal YAML spec.
- Agent-native flags: `--json`, `--select`, `--dry-run`, `--stdin`, `--csv`, `--compact`, `--quiet`, `--yes`, `--no-input`, `--no-cache`, `--no-color`. Auto-JSON when piped. Typed exits (0/2/3/4/5/7).
- Domain archetypes (PM, Communication, Payments, Infrastructure, Content) auto-generate `stale`, `orphans`, `load`, `reconcile`, `health`, `similar`, `channel-health`.
- Local-first data layer: domain-specific SQLite tables, FTS5, cursor sync, `sync`/`search`/`sql`/`tail`.
- Quality pipeline: dogfood (structural), verify (runtime), scorecard (two-tier), shipcheck (combined), emboss, polish, retro.
- Codex mode for token savings, sniff gate for no-spec APIs, crowd-sniff for ecosystem-absorb manifest.
- 18 APIs in catalog (Asana, DigitalOcean, Discord, Front, GitHub, HubSpot, LaunchDarkly, Pipedrive, Plaid, Postman, SendGrid, Sentry, Square, Stripe, Stytch, Telegram, Twilio, Petstore).

Library (`printing-press-library`):
- 21 printed CLIs + 19 MCP servers + `/ppl` router skill + 21 `pp-*` focused skills.
- Plugin marketplace for Claude Code.
- Per-CLI auth today is env-var based: `ESPN_KEY`, `HUBSPOT_ACCESS_TOKEN`, `DUB_TOKEN`, `LINEAR_API_KEY`, `CAL_COM_TOKEN`, `KALSHI_API_KEY`, etc. No shared credential store.
- Auth types present across the catalog: `none`, `api_key`, `bearer_token`, `composed` (cookie auth), OAuth implied for some (e.g. Linear). No unified OAuth helper.

Megamcp (`internal/megamcp/`):
- Aggregate MCP server that loads all printed MCP manifests and exposes 6 meta-tools: `library_info`, `setup_guide`, `activate_api`, `deactivate_api`, `search_tools`, `about`.
- Dynamic tool registration/deregistration via `ActivationManager`.
- Fail-closed auth via `hasAuthConfigured(manifest)` and `ApplyAuthFormat` (placeholder substitution from env vars).
- Max 32KB response to agent, 10MB over the wire. Response classification for error telemetry.
- Already solves a large chunk of the "Composio-style unified MCP server" problem on the MCP side.

Gap summary (the interesting bit):
- CLI side has no unified `pp execute`, `pp search`, `pp proxy`. Every tool lives behind its printed CLI binary (e.g. `espn-pp-cli scores`) or the `/pp-espn` skill. There is no cross-CLI execution entry point to parallel megamcp's MCP-side unification.
- No central auth manager. Each printed CLI reads its own env vars. Users manage many tokens. No OAuth helper, no token refresh, no keychain story.
- No triggers / event delivery. Polling is manual via `sync` commands. No webhook receiver.
- No `--parallel` batch execution across tools.
- No `allowed_tools` filter on MCP servers.
- No per-CLI `llms.txt` artefact.

### External references

- Composio CLI reference: https://composio.dev/toolkits/linkedin/framework/cli
- Composio docs: https://docs.composio.dev
- Composio skills for Claude Code: https://github.com/ComposioHQ/skills
- Composio monorepo: https://github.com/ComposioHQ/composio
- Related prior PP plan: `docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md` (already shipped).

### Institutional learnings referenced

- PP philosophy "absorb then transcend" (README L84-94): absorb every feature from every competitor, then compound with SQLite + agent-native layer. Composio absorption lives at the root of this philosophy.
- `AGENTS.md` machine-vs-printed discipline: many Composio features are machine-level (generator changes that affect every future CLI); fewer are printed-CLI-level.
- Glossary: megamcp shows the pattern for aggregate servers and meta-tools. It is the right place to extend for `allowed_tools` and parallel activation.

## Key Technical Decisions

KTD-1. Landing repo for the super-CLI entry point is `cli-printing-press` (not `printing-press-library`). Rationale: the command surface is a machine capability that must ship to every user the moment they install the press. Putting it in the library couples it to plugin install order. It belongs next to the generator binary so it travels together.

KTD-2. The super-CLI takes the shape of subcommands on the existing `printing-press` binary: `printing-press execute`, `printing-press search`, `printing-press info`, `printing-press proxy`, `printing-press list`, `printing-press auth ...`, `printing-press trigger ...`. Rationale: one binary to install, one `/install mvanhorn/cli-printing-press` to get everything. A separate `pp` binary would duplicate distribution, release tooling, and updater concerns. A `/ppl` skill alias can still surface the same commands for humans.

KTD-3. The unified auth layer is additive. Printed CLIs continue to read env vars as primary. A new `auth resolve <api>` helper is read by generator templates and returns the first available credential in order: process env -> `PP_AUTH_FILE` override -> shared secure store -> keychain. Rationale: every existing printed CLI keeps working with zero template changes on the hot path. Migration is pull, not push.

KTD-4. The shared credential store is file-based at `~/.pp/credentials.json` with `chmod 600`, with a pluggable backend interface so macOS keychain / Linux Secret Service / Windows Credential Manager can be wired in later without changing the public API. Rationale: ship value now; keychain is a P2 follow-up once the interface is stable.

KTD-5. OAuth flows are implemented per-API via a small `authproviders/` registry. Each provider is a Go file with `AuthURL`, `TokenExchange`, `Refresh`, and metadata (scopes, redirect URI handling). Rationale: we cannot reuse Composio's hosted OAuth broker and we will not build one. Per-provider code is honest work and stays under 150 lines per provider for most APIs.

KTD-6. Triggers are polling-first. `printing-press trigger run` is a local daemon that schedules per-trigger polls using existing `sync` cursors from each printed CLI's store, then forwards diffs to the configured sink (webhook URL, stdout-as-JSONL, file). Webhook-receiver mode is a phase-two add for APIs with native webhooks. Rationale: PP already has cursor-based sync everywhere; polling triggers are a 200-line wrapper over that. Webhook receivers require a long-running HTTP server with signature verification and are a larger lift.

KTD-7. `allowed_tools` and `denied_tools` are expressed as printed-MCP flags (`<api>-pp-mcp --allow "tool1,tool2"`) and as megamcp flags (`printing-press mega-mcp --allow espn:scores_get,espn:teams_list`). Rationale: symmetry between the per-API MCP and the aggregate server. Security and token economy both benefit.

KTD-8. Parallel execution is exposed at two layers: `printing-press execute --parallel` accepts `-d @batch.json` where the batch is an array of `{api, tool, params}`, and megamcp adds a `batch_execute` meta-tool that accepts the same shape. Rationale: matches Composio's `--parallel` affordance without new infra.

KTD-9. `printing-press execute` is a raw-HTTP-through-manifest path. It intentionally does NOT invoke local SQLite, compound commands (`stale`, `orphans`, `load`), domain archetypes, FTS5 search, or `--select`/`--compact` projection. When the user wants the local data layer or compound semantics, they must use `<api>-pp-cli` directly. The super-CLI is a discovery + batching + auth-unification surface, not a replacement for the printed CLIs. Rationale: without this boundary, `execute` silently diverges from the printed CLI for the same tool name, and the Unit 2 verification step generates false-positive parity bugs every time a printed CLI ships a new compound command. Adjust Unit 2 verification to parity-on-raw-HTTP-status-and-body only.

KTD-10. Super-CLI verbs live under a dedicated `printing-press run` subcommand group to preserve the top-level namespace for generator-only commands. The surface becomes `printing-press run list`, `run info`, `run search`, `run execute`, `run proxy`. A compact alias `pp run ...` can ship later. Rationale: `list`, `search`, `info` are high-value English verbs that a future generator feature (catalog search, spec info) will want. Grouping under `run` both signals "this is the runtime role" and reserves the top-level space for the generator role.

KTD-11. Manifest schema versioning is a blocking precondition, not a phase-five addition. `tools-manifest.json` gets a `schema_version: int` field (start at 1). The manifest reader in `internal/megamcp/manifest.go` refuses unknown major versions with an actionable error, and refuses older versions when reading features that require a newer version. Rationale: KTD-2 makes every future manifest field a backward-compat contract between two release trains (generator releases vs library-manifest regenerations). Without an explicit schema gate, Unit 5's new trigger-ready field and every other additive field becomes a silent-fail surface.

KTD-12. Shared `internal/apihttp/` returns a transport-neutral `Response` type (`{StatusCode, Body, Headers}`), not an `mcp.CallToolResult`. MCP-side wraps the Response into `mcp.NewToolResultText/Error` and applies the 32KB `maxAgentResponse` truncation in that wrapper. CLI-side maps status codes to typed exits (0/2/3/4/5/7) in its own wrapper. Host validation and credential redaction stay in `apihttp` because both callers need them. Rationale: without this split the shared package grows "cli-mode flag" branches in every response path and the 32KB agent cap silently applies to CLI output.

KTD-13. Auth-resolution read order makes the env-var-wins choice explicit and surfaces divergence rather than hiding it. Order: process env -> `PP_AUTH_FILE` override -> shared store -> keychain -> nil. `auth doctor` treats "env var and store both present with different values" as a yellow finding and names the shell file the env var came from where detectable (`~/.zshrc`, `~/.bashrc`, loaded profile). `auth link` prints a warning after a successful link if the corresponding env var is set in the current shell, with copy-paste `unset` guidance. `auth list` adds a "shadowed by env" column. Rationale: env-var-wins is simple but creates a silent-stale-token class of bugs. The detection surface must be first-class, not left to user intuition.

## Feature Stack Ranking

Scoring criteria, each 1-5:
- Impact: agent/human experience delta.
- Reach: how many printed CLIs it lights up.
- Effort (inverted): lower effort -> higher score.
- Fit: alignment with local-first, no-backend, agent-native philosophy.
- Moat: how much harder it is for a thin-wrapper competitor to replicate.

| Rank | Feature | Impact | Reach | Effort | Fit | Moat | Total | Tier |
|------|---------|--------|-------|--------|-----|------|-------|------|
| 1 | Unified super-CLI (`printing-press execute`/`search`/`info`/`proxy`/`list`) | 5 | 5 | 4 | 5 | 4 | 23 | S |
| 2 | Unified auth manager (`auth login`/`auth link`/`auth status`/`auth doctor`) | 5 | 5 | 3 | 4 | 5 | 22 | S |
| 3 | Local triggers system (`trigger add`/`trigger run`) | 5 | 4 | 3 | 4 | 5 | 21 | A |
| 4 | `allowed_tools` + `denied_tools` on `<api>-pp-mcp` and megamcp | 3 | 5 | 5 | 5 | 2 | 20 | A |
| 5 | `--parallel` batch execution across tools (CLI + megamcp) | 4 | 4 | 5 | 4 | 2 | 19 | A |
| 6 | `printing-press run --file workflow.yaml` (scriptable chains) | 3 | 3 | 2 | 4 | 3 | 15 | B |
| 7 | Per-CLI `llms.txt` / `llms-full.txt` artefact | 3 | 5 | 5 | 4 | 1 | 18 | B |
| 8 | Typed SDK codegen (`printing-press generate ts\|py`) | 2 | 3 | 2 | 3 | 2 | 12 | B |
| 9 | Browser-based playground for printed CLIs | 2 | 3 | 1 | 2 | 2 | 10 | C |
| 10 | Per-user multi-tenant MCP URL hosting | 2 | 5 | 1 | 1 | 2 | 11 | C |
| 11 | Python-sandboxed tool execution | 2 | 3 | 1 | 1 | 2 | 9 | C |
| 12 | Hosted dashboard for connections/triggers/logs | 3 | 5 | 1 | 1 | 2 | 12 | C |
| 13 | White-label auth screens | 1 | 2 | 2 | 2 | 1 | 8 | C |

Stack-ranking commentary:

S-tier is where PP wins back the ground Composio covers for one-command discovery and one-credential onboarding, without giving up local-first. These are the two features the user will feel in week one.

A-tier adds event-driven workflows, safety (tool filtering), and throughput (parallel). Each is valuable independently; together they turn the printed MCPs into a production-grade agent substrate.

B-tier is nice to have. llms.txt is basically free and should probably ship alongside S-tier. The workflow YAML and codegen can wait until there is user demand signal.

C-tier is where we explicitly refuse to follow Composio. A hosted dashboard and multi-tenant MCP service would fork PP away from its shipped posture; Python sandboxing does not fit Go-binary distribution; white-label auth has no PP customer.

## High-Level Technical Design

> This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

### Surface shape for the super-CLI

Super-CLI verbs live under the reserved `run` namespace (KTD-10) to preserve the top-level namespace for generator-only commands.

```
printing-press                   # existing generator entry point
  generate ...                   # existing
  verify ...                     # existing
  scorecard ...                  # existing
  emboss ...                     # existing
  dogfood ...                    # existing
  mega-mcp [--allow ...] ...     # existing, extended (Unit 4)

  run list                       # new: installed printed CLIs + auth status
  run info <api> [<tool>]        # new: manifest / schema lookup
  run search <query> [--api X]   # new: delegates to megamcp-style search across manifests
  run execute <api> <tool> [-d .]# new: raw-HTTP-through-manifest (KTD-9); does NOT use local SQLite or compound commands
          [--parallel -d @batch.json]
          [--dry-run] [--get-schema] [--skip-auth-check]
  run proxy --api X -X METHOD /path  # new: authenticated raw HTTP using unified auth
          [-H ...] [-d ...]

  auth login                     # new: no-op placeholder; future SSO
       link <api>                # new: per-API OAuth (state+PKCE) / api-key capture (tty only)
       list [--api X]            # new: linked creds + expiry + shadowed-by-env + env-differs columns
       status                    # new: quick summary
       doctor                    # new: probe creds + env divergence + perms + sync-path hygiene
       revoke <api>
       fix-perms                 # new: restore 0600/0700 on ~/.pp and contents
       rotate-secret             # new: webhook sink secret rotation (Unit 5)

  trigger add <api> <event>      # new: create a polling trigger
          --sink webhook|stdout|file
          --url https://...      # webhook only, https enforced unless --insecure
          --secret <value>       # webhook only, auto-generated if omitted
          --every 5m
          list
          run                    # daemon mode, HydrateForRequest only
          logs
          remove <id>
          rotate-secret <id> [--overlap 10m]
```

### Credential resolution order (read path)

```
hasCred(api) =
  os.Getenv(primaryEnvVar)          # printed CLI's current path, unchanged
    || os.Getenv(PP_AUTH_FILE)      # operator override
    || authstore.Read(api)           # shared JSON file at ~/.pp/credentials.json
    || keychain.Read(api)            # P2 follow-up, same interface
    || nil
```

Two hydration helpers (KTD-13, KTD-3):

- `authload.HydrateForRequest(ctx, slug) -> string` returns the token for use directly in a request header. Token does not land in process env. Preferred for all new printed CLIs and mandatory for the trigger daemon (Unit 5).
- `authload.Hydrate(slug, envVarName)` populates `os.Setenv` before a CLI's legacy auth code reads it. Kept for already-printed CLIs we do not want to re-emit. Documented as the less-safe path.

Printed CLIs keep their existing env-var semantics. Env-var-wins over store is preserved, but `auth doctor` and `auth list` surface divergence so stale `.zshrc` entries are visible instead of silently shadowing a fresh link.

### Trigger polling loop

```
for each trigger in store:
  tick := schedule.Next(trigger)
  wait until tick
  cursor := store.GetCursor(trigger.id)
  diff := printedCLI(trigger.api).Sync(--since cursor, --json)
  if diff is empty: continue
  emit(trigger.sink, trigger.event, diff)
  store.SetCursor(trigger.id, diff.nextCursor)
```

Sinks: `webhook` POSTs signed JSON; `stdout` writes JSONL for the agent to tail; `file` appends to a rotated log.

### Generator change for future CLIs

Templates add one line: every printed CLI imports `cliutil/authload` and calls `authload.Hydrate("api-slug", "PRIMARY_ENV_VAR")` in root command `PersistentPreRun`. That is the entire surface area in the printed CLI. All the logic lives in the shared package.

## Implementation Units

- [ ] Unit 0: Manifest schema versioning (blocking precondition)

  Goal: Add `schema_version` to `tools-manifest.json`, enforce it in the manifest reader, and establish the backward-compat contract before any new manifest fields ship.

  Requirements: R3, R4, R5

  Dependencies: None. Must land before Units 1, 2, or 5.

  Files:
  - Modify: `internal/megamcp/manifest.go` to add `SchemaVersion int` on `ToolsManifest` and refuse unknown major versions when reading.
  - Modify: `internal/pipeline/toolsmanifest.go` to emit `schema_version: 1` on generation.
  - Modify: the writer path for the `printing-press-library` registry to tolerate mixed-version manifests.
  - Test: `internal/megamcp/manifest_test.go` extends to cover version-missing, version-current, version-future-major, version-future-minor.

  Approach:
  - Current version 1. A missing field is treated as version 1 to keep all shipped manifests valid.
  - Future-major refusal is actionable: "manifest schema_version=2 is newer than this printing-press build; upgrade to at least vX.Y."
  - Future-minor with unknown fields is accepted with a debug-level log ("ignoring unknown field X in manifest"). Never fail on forward-compat minor bumps.

  Patterns to follow: `internal/megamcp/manifest.go` structure; existing manifest tests.

  Test scenarios:
  - Happy path: manifest with no `schema_version` loads as v1.
  - Happy path: manifest with `schema_version: 1` and known fields loads.
  - Edge case: manifest with `schema_version: 1` and an unknown field (forward-compat minor) loads with one warning log and no error.
  - Error path: manifest with `schema_version: 2` returns a typed error and a named upgrade instruction; no partial registration.
  - Integration: megamcp started against a library containing one v1 manifest and one v2 manifest registers only the v1 one and logs the v2 skip at warn level.

  Verification: `printing-press run list` shows accepted and skipped manifests distinctly so users can diagnose version mismatches without reading logs.

- [ ] Unit 1: Super-CLI skeleton - run list, run info, run search

  Goal: Ship `printing-press run list`, `printing-press run info`, `printing-press run search` as thin readers over the existing megamcp registry code, under the reserved `run` namespace (KTD-10).

  Requirements: R3, R4

  Dependencies: Unit 0.

  Files:
  - Create: `internal/supercli/registry.go` (wrapper reusing `internal/megamcp/registry.go`)
  - Create: `internal/supercli/list.go`, `internal/supercli/info.go`, `internal/supercli/search.go`
  - Create: `internal/cli/supercli_cmd.go` (Cobra wiring)
  - Modify: `cmd/printing-press/main.go` to register the subcommand group
  - Test: `internal/supercli/registry_test.go`, `internal/supercli/list_test.go`, `internal/supercli/search_test.go`

  Approach:
  - Share the same manifest loader megamcp uses so a newly-installed printed CLI is discoverable without a re-registration step.
  - `list` output: table in TTY, JSON when piped. Columns: api, cli-binary, mcp-binary, tool_count, auth_type, auth_status (green/red based on env + store).
  - `info <api>` returns the full `tools-manifest.json`. `info <api> <tool>` returns a single tool with its parameters.
  - `search <query>` reuses `makeSearchToolsHandler` logic from megamcp directly; result shape matches.

  Patterns to follow: `internal/megamcp/metatools.go` handlers.

  Test scenarios:
  - Happy path: `list` returns all 21 library manifests when the library is installed.
  - Happy path: `info espn` returns the ESPN manifest.
  - Happy path: `search "scores"` ranks `espn:scores_get` above unrelated tools.
  - Edge case: `info unknown-api` returns exit 3 and a typed error with suggested APIs.
  - Edge case: empty library returns `[]` in JSON mode, not an error.

  Verification: `list`, `info`, and `search` all pass `--json | jq` round-trips; auto-JSON triggers when piped.

- [ ] Unit 2: Super-CLI execute + proxy

  Goal: Ship `printing-press run execute <api> <tool>` and `printing-press run proxy --api X -X METHOD /path` under the reserved `run` namespace (KTD-10), explicitly as raw-HTTP-through-manifest paths that do NOT invoke local data layer or compound commands (KTD-9).

  Requirements: R3, R4

  Dependencies: Unit 0, Unit 1.

  Files:
  - Create: `internal/apihttp/execute.go` with the transport-neutral `Response` type (KTD-12).
  - Create: `internal/apihttp/hosts.go` (lift host validation from `internal/megamcp/handler.go`).
  - Create: `internal/apihttp/redact.go` (lift credential redaction).
  - Modify: `internal/megamcp/handler.go` to wrap `apihttp.Execute` and own the 32KB truncation and MCP-specific result shaping.
  - Create: `internal/supercli/execute.go`, `internal/supercli/proxy.go`, `internal/supercli/batch.go`.
  - Modify: `internal/cli/supercli_cmd.go` to wire under `run execute` / `run proxy`.
  - Test: `internal/apihttp/execute_test.go`, `internal/supercli/execute_test.go`, `internal/supercli/proxy_test.go`, `internal/supercli/batch_test.go`, plus `internal/megamcp/handler_test.go` regression coverage for the wrapper.

  Approach:
  - `apihttp.Execute(ctx, manifest, tool, args) -> (Response, error)` builds the URL with path-param substitution, applies auth via `internal/authstore` (Unit 3) with env-var fallback, enforces host allowlist, and returns `{StatusCode, Body, Headers}` without any transport-specific shaping.
  - MCP wrapper applies the 32KB `maxAgentResponse` truncation and returns `mcp.CallToolResult`.
  - CLI wrapper maps status codes to typed exits: 2xx -> 0, 401/403 -> 4, 404 -> 3, 429 -> 7 (after one backoff), 4xx-other -> 2, 5xx -> 5. Prints table for human, JSON when piped. Does not truncate; the user can pipe to `head` if they want.
  - `proxy` skips the tool registry, resolves auth for the named API, and makes the raw request. Mirrors curl: `-X`, `-H` (repeatable), `-d`.
  - All commands honour `--dry-run`, `--get-schema`, `--skip-auth-check`, and auto-JSON.
  - Execute does not read any printed CLI's SQLite store and does not run compound commands (KTD-9). Help text states this explicitly and points users to `<api>-pp-cli` for those features.

  Execution note: Characterization-first. Write the wrapper test for megamcp's existing behaviour before extracting, assert parity post-extraction, then layer the CLI wrapper. `internal/megamcp/handler.go:28` is the lifting target.

  Patterns to follow: `internal/megamcp/handler.go`, `internal/megamcp/auth.go`.

  Test scenarios:
  - Happy path: `run execute espn scores_get --league nfl` returns valid JSON.
  - Happy path: `run proxy --api dub -X GET /links` returns the raw API response body; no local-store augmentation.
  - Happy path: megamcp wrapper around `apihttp.Execute` still truncates at 32KB; CLI wrapper does not.
  - Edge case: missing required path param returns exit 2 and names the missing placeholder.
  - Edge case: `--skip-auth-check` lets the caller hit an unauthenticated endpoint without a store lookup.
  - Error path: 401 from the API returns exit 4 with an actionable "run `printing-press auth link <api>`" hint.
  - Error path: 429 with retry-after returns exit 7 after one backoff attempt.
  - Error path: host not in the manifest's allowlist returns exit 2 with a "host not permitted by manifest" message; same behaviour on both wrappers.
  - Integration: `run execute --parallel -d @batch.json` runs three tool calls concurrently and returns responses in input order, not completion order.
  - Integration: extracting `apihttp` does not regress existing megamcp handler tests; characterization suite passes pre and post extraction.

  Verification: parity on raw HTTP status-and-body between `run execute espn X` and `espn-pp-cli X --json` for five tools (KTD-9 scope: bodies only, not `--select`, `--compact`, or compound outputs). Megamcp handler snapshot tests continue to pass after extraction.

- [ ] Unit 3: Unified auth store + `auth` subcommand group

  Goal: Ship `printing-press auth link`, `auth list`, `auth status`, `auth doctor`, `auth revoke`, and the shared credential store that backs them, with explicit threat model, PKCE+state on OAuth, and divergence detection between env vars and store.

  Requirements: R3, R4, R5

  Dependencies: Unit 0, Unit 1.

  Threat model (accepted in P1, revisited at keychain P2):
  - Same-UID processes (malicious npm/pip postinstall, VS Code extensions, shell plugins) can read `~/.pp/credentials.json`. 0600 does not defend here. Keychain backend (KTD-4) is the mitigation path; P2 gate is "before trigger daemon general availability OR before $100 user, whichever comes first."
  - Backup and sync exfiltration via Time Machine, iCloud Desktop, Dropbox, rsync. Mitigation: `~/.pp/` directory ships with a `.nosync` marker file and the README documents how to exclude it from common backup tools.
  - Shell history / argv leakage during api-key prompts. Mitigation: `auth link` reads secrets only from tty with echo disabled, refuses argv-provided secrets (`--token` flag rejected), and scrubs crash reports of argv containing high-entropy strings.
  - Crash dumps and core files. Mitigation: plan explicitly does not address; documented as out-of-scope for P1.
  - No at-rest encryption and no file integrity. Mitigation: accepted in P1 with an HMAC-SHA256 over each entry using a device-local secret derived from the hostname and a random salt stored in `~/.pp/.device-key` (0600). Tampering detected, not prevented.
  - Concurrent writers. Mitigation: single-writer lock via `~/.pp/.credentials.lock` (flock on Linux/macOS, LockFileEx on Windows). Lock contention returns exit 5 with a "another auth operation in progress" message.

  Files:
  - Create: `internal/authstore/store.go` (file-based backend at `~/.pp/credentials.json`, `chmod 600`, atomic writes)
  - Create: `internal/authstore/backend.go` (pluggable `Backend` interface; file backend today; keychain stub)
  - Create: `internal/authstore/providers/` (registry of OAuth providers by api slug)
  - Create: `internal/authstore/providers/github.go`, `providers/linear.go`, `providers/slack.go` (initial three)
  - Create: `internal/cli/auth_cmd.go`
  - Create: `internal/cliutil/authload/` package emitted into every printed CLI
  - Modify: generator templates to emit one `authload.Hydrate("<slug>", "<PRIMARY_ENV>")` call in root command `PersistentPreRun`
  - Test: `internal/authstore/store_test.go`, `internal/authstore/providers/github_test.go`, `internal/cliutil/authload/authload_test.go`

  Approach:
  - Store schema: `{api: {type, token, refresh_token, expires_at, scopes, linked_at, entry_hmac}}`. One file, one slug per key. `entry_hmac` verifies integrity on read.
  - Parent dir `~/.pp/` is 0700 with an atomic mkdir. `credentials.json` is 0600 written via `O_EXCL` tempfile + rename under the `.credentials.lock`.
  - `auth link <api>` OAuth flow hardening:
    - Bind loopback callback on `127.0.0.1` only (never `0.0.0.0` or `localhost` DNS resolution).
    - Generate a cryptographically random `state` per flow; reject callbacks with missing or mismatching state.
    - Use PKCE (RFC 7636) with S256 challenge/verifier on every OAuth flow. Providers that do not support PKCE are documented, not silently downgraded.
    - Exact redirect URI match; plan a per-provider registration table in `internal/authstore/providers/README.md` that names each provider's accepted redirect URI form (fixed-port, `127.0.0.1` wildcard-port, or out-of-band).
    - Callback server accepts exactly one request, shuts down in under 120 seconds, and times out with exit 4.
    - Enumerate callback error codes: `access_denied`, `server_error`, `invalid_scope`, `invalid_request`, missing-`code`, non-2xx token-exchange response. Each maps to exit 4 with a distinct message.
    - OIDC-bearing providers (Google, some enterprise) verify `iss` and `nonce` when ID tokens are present; others skip.
  - `auth link <api>` api-key mode: tty-only prompt with echo disabled, refuses argv-sourced secrets.
  - `auth doctor`:
    - For every installed printed CLI, probe credential presence, expiry, and env-vs-store divergence (KTD-13).
    - "Env var and store differ" is a yellow finding with the originating shell file where detectable.
    - Missing 0600 perms on `credentials.json` or 0700 on `~/.pp/` is a red finding with a `fix-perms` suggestion.
    - Dir listed in a known sync path (`~/Dropbox`, `~/iCloud`, `~/OneDrive`) is a yellow finding with exclusion guidance.
  - `auth link <api>` post-success: if the corresponding env var is set in the caller's environment, print a warning with copy-paste `unset` guidance; do not silently override.
  - `auth list` column model: `api`, `type`, `linked_at`, `expires_at`, `scopes`, `shadowed_by_env` (yes/no), `env_differs` (yes/no).
  - `auth revoke <api>` removes the entry and posts to the provider's revoke endpoint when available.
  - Generator change is additive and uses `HydrateForRequest` rather than `os.Setenv` for new printed CLIs. Templates import `cliutil/authload` and call `authload.HydrateForRequest(ctx, "<slug>")` inside HTTP client construction so tokens live in request headers and not in process env. The legacy `Hydrate` that populates env vars remains available for backward compatibility with already-printed CLIs.
  - Child-process guardrails: `authload` documents a `SafeExec` helper that strips `*_TOKEN`/`*_KEY`/`*_SECRET` env vars before spawning subprocesses. Printed-CLI templates that shell out (e.g. to `curl`, `git`, `gh`) must use `SafeExec`, enforced by the scorer.
  - Trigger daemon (Unit 5) MUST use `HydrateForRequest`, never `Hydrate`, because env-resident tokens in a long-running process are a material escalation.

  Execution note: Test-first on `authstore`. The file layout, concurrent-write safety, permission bits, PKCE/state round-trips, and env-shadow detection are the kind of thing we want covered before behaviour depends on them. This section touches security surfaces; route through `compound-engineering:review:security-sentinel` before merge.

  Patterns to follow: `internal/megamcp/auth.go` for the placeholder-substitution read path. Keep env-var-as-truth semantics intact.

  Test scenarios:
  - Happy path: `auth link github` completes OAuth round-trip against a stubbed authorization server with state + PKCE verification and writes the token to the store with 0600 perms.
  - Happy path: after `auth link`, a printed CLI that does not have its env var set reads its token via `HydrateForRequest` and successfully calls the API; token never appears in process env.
  - Happy path: `auth list --json` reports `shadowed_by_env: true` when an env var is set and `env_differs: true` when its value does not match the store.
  - Edge case: store file does not exist; `auth status` reports empty cleanly.
  - Edge case: env var is set AND store has a different value; env var wins but `auth doctor` yellow-flags the divergence with the originating shell file.
  - Edge case: store file has wrong perms; `auth doctor` reports a red finding and `auth fix-perms` restores 0600 and 0700 on the parent dir.
  - Edge case: callback with missing `state` or `state` mismatch returns exit 4 without corrupting the store.
  - Edge case: callback with `code` but token exchange returns 5xx returns exit 5 with a retry hint.
  - Edge case: OAuth flow where provider does not support PKCE returns a named warning during `auth link` and refuses to proceed unless `--allow-no-pkce` is passed; records the downgrade in the entry metadata.
  - Edge case: concurrent `auth link` invocations serialise via the lock file; the second call reports "another auth operation in progress" and exits 5.
  - Edge case: api-key prompt rejects an argv-supplied `--token` value with an actionable error.
  - Edge case: tampered `credentials.json` (HMAC mismatch) returns exit 4 on read with a "credentials file integrity failure; re-run auth link" message.
  - Error path: OAuth callback with `error=access_denied` returns exit 4 and does not corrupt the store.
  - Error path: token refresh returns 401; `auth doctor` marks the creds expired, does not crash.
  - Error path: loopback callback binds successfully but no request arrives within 120s; returns exit 4 and shuts down the listener.
  - Integration: three printed CLIs share one store; each sees only its own slug's token via `HydrateForRequest`; none of them see any slug's token in `os.Environ()`.
  - Integration: printed CLI using `SafeExec` to invoke `curl` does not leak `*_TOKEN` to the child process's environment.

  Verification: on a clean machine, `printing-press auth link github && github-pp-cli issues list` succeeds without the user ever exporting `GITHUB_TOKEN`, and `ps eww` during the call does not show the token in the printed CLI's environment.

- [ ] Unit 4: `allowed_tools` and `denied_tools` on per-API MCP and megamcp

  Goal: Ship `--allow` and `--deny` flags on printed MCP servers and the aggregate server.

  Requirements: R3

  Dependencies: None. Can ship in parallel with Units 1-3.

  Files:
  - Modify: megamcp's `internal/megamcp/activation.go` and `metatools.go` to accept allow/deny lists (slug-scoped like `espn:scores_get`).
  - Modify: generator template for `cmd/<api>-mcp/main.go` to accept `--allow` / `--deny` flags.
  - Test: `internal/megamcp/activation_test.go` (extend), plus a new template-output snapshot test.

  Approach:
  - Both lists are comma-separated. Deny wins over allow. Empty allow means "all except denied".
  - megamcp additionally accepts `--allow-api` / `--deny-api` for whole-API gating.
  - Scorer recognises the new flag as a ship-ready feature so we do not penalise CLIs that now expose it.

  Execution note: Extend the existing activation tests rather than adding a parallel test file; the behaviour is a filter layered on top of activation.

  Test scenarios:
  - Happy path: `--allow espn:scores_get` exposes only `scores_get`.
  - Happy path: `--deny espn:scores_delete` hides one tool; others remain.
  - Edge case: both `--allow` and `--deny` set; deny wins over allow conflicts.
  - Edge case: unknown tool name in `--allow` logs a warning, does not crash.
  - Integration: megamcp with `--allow-api espn,dub` only surfaces those two manifests in `library_info`.

  Verification: `mcp list-tools` on a filtered server returns exactly the expected set across three configurations.

- [ ] Unit 5: Trigger daemon (polling mode)

  Goal: Ship `printing-press trigger add`, `trigger list`, `trigger run`, `trigger logs`, `trigger remove` with polling-based delivery.

  Requirements: R3, R4

  Dependencies: Unit 1 (registry), Unit 3 (auth resolution).

  Files:
  - Create: `internal/triggers/store.go`, `internal/triggers/runner.go`, `internal/triggers/sinks.go`
  - Create: `internal/cli/trigger_cmd.go`
  - Modify: printed-CLI template spec so every sync-capable manifest declares its "trigger-ready" resources in `tools-manifest.json` (additive field, backward compatible).
  - Test: `internal/triggers/runner_test.go`, `internal/triggers/sinks_test.go`

  Approach:
  - `trigger add <api> <event> --sink --every` validates the event exists in the API's manifest, writes a trigger record to `~/.pp/triggers.json` (0600, 0700 parent), does not start a daemon.
  - `trigger run` reads the file and schedules polls using `time.Ticker`. For each trigger it shells out (via `SafeExec` so tokens do not leak into child env) to the printed CLI's incremental sync or list-since command and diffs.
  - Daemon auth: uses `authstore.HydrateForRequest` at request time only (KTD-13, Unit 3). Tokens never land in the daemon's process env because the daemon is long-lived and `/proc/<pid>/environ` is readable same-UID.
  - Sinks:
    - `webhook`: signed POST over HTTPS. Plain `http://` is refused unless `--insecure` is explicitly passed. Signing is HMAC-SHA256 over the canonical raw body bytes (before any JSON re-serialization). Header: `X-PP-Signature: t=<unix-seconds>,v1=<hex-hmac>`. Receivers reject signatures with a timestamp older than 300 seconds (replay window). Each delivery also carries `X-PP-Delivery-Id: <uuidv4>` for receiver-side idempotency.
    - `stdout`: writes JSONL for an agent to tail.
    - `file`: appends with rotation at 10MB.
  - Secret origin and rotation:
    - On first `trigger add --sink webhook`, auto-generate a 32-byte random secret, print it exactly once with a "save this now" warning, and store it in `~/.pp/triggers.json` under the sink config (0600).
    - `trigger rotate-secret <id>` generates a new secret and accepts both old and new signatures for an overlap window (default 10 minutes, configurable via `--overlap`). During the window, each outbound request carries both `X-PP-Signature` (v1=new) and `X-PP-Signature-Prev` (v1=old) so receivers can cut over with zero missed deliveries.
    - A user-supplied `--secret` flag on `trigger add` is supported for receivers that already have a secret from another system.
    - A missing secret refuses to start the daemon with exit 2 and a named remediation; there is no silent unsigned-send path.
  - Delivery semantics: at-least-once; receiver-side dedup expected via `X-PP-Delivery-Id`. Failed deliveries retry with exponential backoff up to 6 attempts over ~30 minutes, then queue to disk with bounded depth (default 1000 events per trigger).
  - `trigger logs` reads the rotated log.

  Execution note: Start with a failing integration test that runs `trigger add` + `trigger run` against a mock HTTP server with seeded data, and asserts the webhook was called with the expected diff.

  Patterns to follow: `sync` cursor handling in printed CLIs. Reuse the cursor format wherever possible so a trigger and an ad-hoc `sync` do not fight each other.

  Test scenarios:
  - Happy path: new Linear issue appears; trigger fires; webhook receives HTTPS POST with `X-PP-Signature` (HMAC-SHA256 verifies against stored secret), `X-PP-Delivery-Id` (UUIDv4), and body with `event: linear.issue_created`.
  - Happy path: `trigger list --json` returns every configured trigger with next-poll time.
  - Happy path: `trigger rotate-secret <id> --overlap 10m` emits both `X-PP-Signature` and `X-PP-Signature-Prev` for 10 minutes, then drops the old secret.
  - Edge case: API returns the same cursor twice; no duplicate events emitted (daemon-side idempotency).
  - Edge case: receiver is reachable but returns 5xx; event is queued to disk and retried with exponential backoff up to 6 attempts.
  - Edge case: `--sink webhook --url http://example` without `--insecure` is refused at `trigger add` with exit 2.
  - Edge case: request with timestamp older than 300s replay window is correctly rejected by a verifying receiver (test both sides).
  - Edge case: missing webhook secret refuses to start the daemon with exit 2 and a named remediation.
  - Error path: the printed CLI is not installed; `trigger add` fails with exit 3 and points to `printing-press run list` to see what is installed.
  - Error path: polled API returns 429; runner respects retry-after and extends the tick for that trigger only.
  - Error path: daemon process crash mid-delivery; on restart the undelivered event replays exactly-once per `X-PP-Delivery-Id` (at-least-once delivery, receiver-side dedup expected).
  - Integration: two triggers against the same API share one HTTP connection pool and do not exceed the API's rate limit budget.
  - Integration: long-running daemon with 24h uptime shows no authentication token in `/proc/<pid>/environ` (HydrateForRequest confirmed).

  Verification: 24-hour soak with three triggers against Linear, GitHub, and HubSpot printed CLIs shows zero missed events, signed deliveries verify on the receiver, and `ps eww` on the daemon shows no tokens in env for the entire run.

- [ ] Unit 6: `execute --parallel` and megamcp `batch_execute`

  Goal: Concurrent execution across tools.

  Requirements: R3

  Dependencies: Unit 2.

  Files:
  - Modify: `internal/supercli/execute.go` to accept `--parallel` when input is a batch array.
  - Modify: `internal/megamcp/metatools.go` to register `batch_execute`.
  - Test: `internal/supercli/execute_test.go` (extend), `internal/megamcp/metatools_test.go` (extend)

  Approach:
  - Bounded concurrency via `cliutil.FanoutRun` (already exists). Default fanout of 8. Per-API rate-limit aware: each API gets its own semaphore derived from manifest rate-limit metadata if present, else defaults.
  - Results returned in input order regardless of completion order.
  - Any one failure does not abort the batch; the response array contains typed errors per index.

  Patterns to follow: `cliutil.FanoutRun` (see glossary).

  Test scenarios:
  - Happy path: batch of three tools across three APIs returns three results in input order.
  - Edge case: one of three fails; result at that index is a typed error, other two succeed.
  - Edge case: all three target the same API; per-API semaphore prevents flooding.
  - Integration: batch with 20 tools across 4 APIs completes in under 2x the slowest single call.

  Verification: load test with 100-tool batch against mock servers shows linear-ish concurrency scaling up to the configured fanout cap.

## System-Wide Impact

- Interaction graph: the super-CLI and megamcp converge on one shared `internal/apihttp` package for request building and auth resolution; both depend on `internal/authstore` going forward. Changes to either ripple to both.
- Error propagation: typed exits (0/2/3/4/5/7) are preserved across the super-CLI, meaning agents do not need new error-handling code to consume `execute`/`proxy` alongside existing printed CLIs.
- State lifecycle risks: the credential store and the trigger store both live at `~/.pp/`. Concurrent writers (two `printing-press auth link` invocations) need atomic-write discipline. Same applies to `~/.pp/triggers.json` under `trigger add` contention.
- API surface parity: every new subcommand needs a parallel MCP meta-tool in megamcp so agents that prefer MCP do not lose reach. `list` -> `library_info` (exists), `search` -> `search_tools` (exists), `execute` -> `batch_execute` (new), `auth` ops -> `auth_status`/`auth_link_url` meta-tools (new, link flow is interactive so the MCP version returns the URL and expects the user to complete in-browser).
- Integration coverage: the store file is accessed by both the super-CLI and every printed CLI via `authload.Hydrate`. Unit tests covering single-process behaviour are necessary but not sufficient; we need one cross-process integration test that exercises "super-CLI writes, printed CLI reads".
- Unchanged invariants: printed CLIs keep reading env vars as their primary credential source. Absence of the unified store does not break them. Generators keep producing the same two binaries per API. The scorecard bar stays where it is.

## Risks & Dependencies

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| OAuth providers differ enough per API that `authproviders/` turns into a maintenance tax | Med | Med | Start with three providers (GitHub, Linear, Slack). Require each new provider to fit a 150-line budget; if it does not, capture in retro and reshape the interface before the fourth |
| Unified auth store becomes a secret-exfiltration target (plaintext JSON) | Med | High | 0600 perms on the file, feature-flag the keychain backend and complete it in a P2 follow-up before promoting the store to default |
| Trigger daemon drifts from printed CLI sync cursors and double-fires | Med | Med | Share the cursor source of truth rather than duplicating it; test with the same DB that the printed CLI writes to |
| Super-CLI grows into a monolith that couples to every printed CLI's internals | Med | Med | The boundary is the manifest. Super-CLI reads manifests and uses the generic HTTP handler. If a printed CLI has behaviour the super-CLI cannot reach, that is a signal the manifest needs to be richer, not that the super-CLI needs a shortcut |
| We ship an `auth` story that feels worse than Composio's because we lack a hosted OAuth broker | High | Med | Set expectations: we will not be cloning Composio's 1,000-toolkit OAuth coverage. We ship the interface and the top five providers, and document how to add the sixth. Positioning emphasises local-first as the feature |
| megamcp allowed_tools filter collides with activation logic | Low | Med | Extend existing activation tests rather than forking a new code path |
| Parallel execution breaks fragile APIs without per-API rate budgets | Med | Med | Default fanout of 8; per-API semaphores read from manifest metadata; first-class retry-after handling |
| Agents get confused by two ways to do the same thing (`<api>-pp-cli X` vs `printing-press run execute api X`), especially since they have different semantics (KTD-9) | Med | Med | Documentation emphasises `run execute` as raw-HTTP-through-manifest, printed CLIs as the full-local-layer path. Help text on both surfaces names the other and states the scope boundary. Scorer adds a dimension for "does the help text point to the sibling surface" |
| Manifest schema drift between generator and runtime versions causes silent-fail on new fields | Med | High | Unit 0 blocks; `schema_version` field and reader gate enforce the contract before any additive field ships |
| Secrets exfiltration via child-process env inheritance, core dumps, or `/proc/<pid>/environ` | Med | High | `HydrateForRequest` keeps tokens out of process env; `SafeExec` strips secret env vars from subprocesses; trigger daemon mandated to use `HydrateForRequest`; file-based store documented as a same-UID risk until keychain P2 ships |
| OAuth loopback flow missing state/PKCE/exact-URI hardening | Med | High | Explicitly specified in Unit 3 Approach; PKCE is default-on; state verification is mandatory; providers without PKCE require an explicit downgrade flag and record the downgrade in entry metadata |
| Webhook signature scheme is homegrown or ambiguous | Med | Med | Unit 5 commits to HMAC-SHA256 + `X-PP-Signature` + timestamp replay window + `X-PP-Delivery-Id`; rotation primitive with overlap window; refuses plain HTTP without `--insecure` |

## Future Considerations (B and C tier)

Not in this plan. Record decisions here so we do not relitigate.

- B-tier candidate: `printing-press run --file workflow.yaml`. Revisit when we see a concrete user case we cannot serve with `ppl` skills plus `execute --parallel`.
- B-tier candidate: `printing-press generate ts|py` typed SDK codegen. Revisit when we have a frontend agent customer.
- B-tier candidate: per-CLI `llms.txt`. Cheap. Bundle it opportunistically when we next touch the README generator.
- C-tier refused: hosted dashboard, multi-tenant per-user MCP URL hosting, Python sandbox, white-label auth screens. These require a backend or a runtime we do not want to own.

## Sources & References

- CLI reference: https://composio.dev/toolkits/linkedin/framework/cli
- Platform docs: https://docs.composio.dev
- MCP overview: https://docs.composio.dev/docs/mcp-overview
- Triggers: https://docs.composio.dev/docs/triggers
- Claude Code skills: https://github.com/ComposioHQ/skills
- Composio monorepo: https://github.com/ComposioHQ/composio
- PP megamcp plan (shipped): `docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md`
- PP megamcp implementation: `internal/megamcp/`
- PP README (feature inventory): `README.md`
- PP AGENTS.md (machine-vs-printed discipline, glossary): `AGENTS.md`
- Library registry: `~/printing-press-library/registry.json`
