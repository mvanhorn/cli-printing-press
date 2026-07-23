---
name: printing-press
description: Set up a new integration, connector, or CLI binding for any API. Wrap or generate a ship-ready Go CLI from an OpenAPI, HAR, or Postman spec via the lean research -> generate -> build -> shipcheck loop. Use when the user says build a CLI, wrap this API, set up a new integration, add a connector, integrate with a service, or names an API by domain.
version: 2.0.0
min-binary-version: "4.0.0"
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
created_by: user
---

# /printing-press

Generate the best useful CLI for an API without burning an hour on phase theater.

```bash
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press https://postman.com/explore
/printing-press https://postman.com
```

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

### Polish Mode (Standalone Skill)

For second-pass improvements to an existing CLI, use the standalone polish skill:

```bash
/printing-press-polish redfin
```

See the `printing-press-polish` skill for details. It runs diagnostics, fixes verify failures, removes dead code, cleans up descriptions and README, and offers to publish.

## Rules

- **Do not ship a CLI that hasn't been behaviorally tested against real targets.** `go build` and `verify` pass-rate are structural signals, not correctness signals. Phase 5's mechanical test matrix in [phases/18-dogfood-testing.md](phases/18-dogfood-testing.md) runs every subcommand + `--json` + error paths; if that matrix was not executed, the CLI is not shippable. Quick Check is the floor; Full Dogfood is required when the user asks for thoroughness.
- **Bugs found during dogfood are fix-before-ship, not "file for v0.2".** If a 1-3 file edit resolves it, do it now. `ship-with-gaps` is deprecated as a default verdict (see Phase 4 in [phases/12-shipcheck.md](phases/12-shipcheck.md)). Context is freshest in-session; a v0.2 backlog that may never be revisited ships known-broken CLIs.
- **Features approved in Phase 1.5 are shipping scope.** Do not downgrade a shipping-scope feature to a stub mid-build. If implementation becomes infeasible, return to Phase 1.5 in [phases/08-ecosystem-absorb-gate.md](phases/08-ecosystem-absorb-gate.md) with a revised manifest and get explicit re-approval.
- **Do not quote human-time estimates for sub-tasks** ("~15-30 min", "~1 hour", "quick fix") in `AskUserQuestion` options, phase descriptions, or reference docs. The agent does the work, not the user; agent-fabricated estimates are notoriously bad and train users to distrust the prompt. Describe scope instead (lines of code, files touched, relative size). The carve-outs are wall-clock estimates for genuinely time-bound things: the whole-CLI run (set the user's expectation up front — most CLIs take 30+ minutes), tool installs (`go install` takes ~10 seconds), and printing-press subcommands that do network-bound work (crowd-sniff scans npm + GitHub, ~5-10 minutes). Anything bounded by agent reasoning time is not time-bound — describe scope.
- **Use raw captures for contract research.** When reading official docs, auth/error/rate-limit pages, endpoint references, OpenAPI/Postman links, or source pages whose exact identifiers affect the generated CLI, read [references/fetch-docs.md](references/fetch-docs.md) and use its `fetch-docs.sh` helper. Reserve `WebFetch` for quick TL;DR reads where losing field-level details is acceptable.
- Optimize for time-to-ship, not time-to-document.
- Reuse prior research whenever it is already good enough.
- Do not split one idea across multiple mandatory artifacts.
- Durable files produced by this skill go under `$PRESS_RUNSTATE/` (working state) or `$PRESS_MANUSCRIPTS/` (archived). Short-lived command captures may use `/tmp/printing-press/` and must be removed after use.
- Do not create a separate narrative phase for dogfood, dead-code audit, runtime verification, and final score. Treat them as one shipcheck block.
- Run cheap, high-signal checks early.
- Fix blockers and high-leverage failures first.
- Reuse the same spec path across `generate`, `dogfood`, `verify`, and `scorecard`.
- YAML, JSON, local paths, and URLs are all valid spec inputs for the verification tools.
- Maximum 2 verification fix loops unless the user explicitly asks for more.

## Secret & PII Protection (Cardinal Rules)

**These rules are non-negotiable. They apply at ALL times during a run.**

API key **values**, token **values**, passwords, and session cookies must NEVER
appear in any artifact: source code, manuscripts, proofs, READMEs, HARs, or
anything committed to git. Env var **names** (e.g., `STEAM_API_KEY`) and
placeholders (e.g., `"your-key-here"`) are safe.

During Phase 5.6 in [phases/20-promote-and-archive.md](phases/20-promote-and-archive.md) and before publishing, read and apply
[references/secret-protection.md](references/secret-protection.md) for:
- Exact-value scanning and auto-redaction of artifacts
- HAR auth stripping (headers, query strings, cookies)
- API key handling rules during the run
- Session state cleanup ordering

## Phase Index

**Never execute a phase from memory. When you enter a phase, Read its file from phases/ first.**

Start with preflight before any user-facing prompt. After preflight succeeds, return here and complete Orientation & Briefing. Then enter Run Initialization and continue in the file order below, following each file's final `Next:` pointer.

| File | One-line purpose | Gates |
|---|---|---|
| [phases/01-preflight.md](phases/01-preflight.md) | Validate the binary, toolchain, disk, browser tools, and setup contract. | Yes |
| [phases/02-run-initialization.md](phases/02-run-initialization.md) | Allocate run-scoped state and working paths. | No |
| [phases/03-resolve-and-reuse.md](phases/03-resolve-and-reuse.md) | Resolve inputs, prior work, source priority, and auth. | Yes |
| [phases/04-research-brief.md](phases/04-research-brief.md) | Produce the build-driving research brief. | No |
| [phases/05-pre-browser-sniff-auth-intelligence.md](phases/05-pre-browser-sniff-auth-intelligence.md) | Gather auth context before capture. | No |
| [phases/06-browser-sniff-gate.md](phases/06-browser-sniff-gate.md) | Decide and record browser discovery per source. | Yes |
| [phases/07-crowd-sniff-gate.md](phases/07-crowd-sniff-gate.md) | Decide whether community code should enrich the spec. | Yes |
| [phases/08-ecosystem-absorb-gate.md](phases/08-ecosystem-absorb-gate.md) | Approve the absorb and transcendence manifest. | Yes |
| [phases/09-api-reachability-gate.md](phases/09-api-reachability-gate.md) | Prove a shippable runtime surface is reachable. | Yes |
| [phases/10-generate.md](phases/10-generate.md) | Enrich the spec, lock, and generate the CLI. | No |
| [phases/11-build-the-goat.md](phases/11-build-the-goat.md) | Implement every approved feature. | Yes |
| [phases/12-shipcheck.md](phases/12-shipcheck.md) | Run verification and reach ship or hold. | Yes |
| [phases/13-sync-param-drop-gate.md](phases/13-sync-param-drop-gate.md) | Compare authored and captured request keys. | Yes |
| [phases/14-agentic-skill-review.md](phases/14-agentic-skill-review.md) | Review printed-SKILL claims against commands. | Yes |
| [phases/15-readme-skill-agents-correctness-audit.md](phases/15-readme-skill-agents-correctness-audit.md) | Audit user-facing document correctness. | Yes |
| [phases/16-agentic-output-review.md](phases/16-agentic-output-review.md) | Review live output for semantic bugs. | No |
| [phases/17-local-code-review.md](phases/17-local-code-review.md) | Review and autofix source before live testing. | Yes |
| [phases/18-dogfood-testing.md](phases/18-dogfood-testing.md) | Exercise real workflows through the live matrix. | Yes |
| [phases/19-polish.md](phases/19-polish.md) | Run diagnostic, fix, and rediagnose. | Yes |
| [phases/20-promote-and-archive.md](phases/20-promote-and-archive.md) | Enforce acceptance, promote, and archive evidence. | Yes |
| [phases/21-next-steps.md](phases/21-next-steps.md) | Route outcomes to publish, polish, retro, journal, or done. | Yes |

## Orientation & Briefing

After [preflight](phases/01-preflight.md) has completed, check whether the user provided arguments. Handle two cases:

### No Arguments: Orientation

If the user typed `/printing-press` with no arguments (no API name, no `--spec`, no `--har`, no URL), print an orientation and ask what they'd like to build:

> The Printing Press generates a fully functional CLI for any API. You give it an API name, a spec file, or a URL. It researches the landscape, catalogs every feature that exists in any competing tool, invents novel features of its own, then generates a Go CLI that matches and beats everything out there — with offline search, agent-native output, and a local SQLite data layer.
>
> By the end, you'll have a working CLI in `$PRESS_LIBRARY/` that you can use for yourself, ship on your own, or apply to add to the printing-press library.
>
> The process takes 30-60 minutes depending on API complexity. Simple APIs with official specs (Stripe, GitHub) are faster. Undocumented APIs that need discovery (ESPN, Domino's) take longer.

Print these example invocations as plain text BEFORE the `AskUserQuestion` call (so they appear as context above the question, not as competing menu options):

```
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press https://postman.com
```

Then ask via `AskUserQuestion`:

- **question:** `"What API would you like to build a CLI for?"`
- **header:** `"API target"`
- **multiSelect:** `false`
- **options:**
  1. **label:** `"Type it (recommended)"` — **description:** `"Provide an API name, URL, spec path, or HAR file via the 'Other' option below."`
  2. **label:** `"Browse existing CLIs first"` — **description:** `"Visit the public library to see what's already been printed before deciding what to build."`

**Do not add additional options** — no "Show me popular options", no pre-populated buttons for Notion / Stripe / GitHub / Linear / Discord. The example invocations above already cover the common shapes, and most popular APIs are already in the public library (offering to re-print them is noise). The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Type it (recommended)"**, they will provide their answer via the auto "Other" field. Set their input as the argument and proceed to the briefing below.

If the user picks **"Browse existing CLIs first"**, print the public library URL prominently and try to open it in the browser, then end the skill so the user can browse before deciding:

```bash
echo ""
echo "Public library: https://github.com/mvanhorn/printing-press-library"
echo "(If you have the Printing Press Library plugin, you can also run /ppl in Claude Code.)"
echo ""
command -v open >/dev/null 2>&1 && open https://github.com/mvanhorn/printing-press-library
```

After printing, end the skill cleanly. Do not proceed to briefing or research — the user is exploring, not building yet. They can re-invoke `/printing-press <api>` once they've decided.

### With Arguments: Briefing

When the user provided an argument (API name, `--spec`, `--har`, or URL), print a brief process overview. This sets expectations and collects any upfront context. (Preflight has already run at this point.)

Print as prose, matching the style of the example below:

> Very well. Setting the type for `<API>`.
>
> **Here is how this will proceed:**
> 1. I shall research `<API>` across the internet: official docs, community wrappers, competing CLIs, MCP servers, and npm/PyPI packages
> 2. I shall catalog every feature that exists in any tool, then devise novel features of my own that no existing tool offers
> 3. I shall present what I found and what I invented — you will have a chance to add your own ideas or adjust the plan before I build
> 4. I shall generate a Go CLI, build every feature from the plan, then verify quality through dogfood, runtime verification, and scoring
>
> **What you will have at the end:** A fully functional CLI at `$PRESS_LIBRARY/<api>` that you can use yourself, ship on your own, or apply to add to the printing-press library.
>
> **Time:** 30-60 minutes depending on API complexity.
>
> **Things that help if you have them:**
> - An API key (for live smoke testing at the end)
> - A logged-in browser session (for discovering authenticated endpoints)
> - A spec file or HAR capture (skips discovery)

If the user provided `--spec`, adapt: "You have provided a spec, so I shall skip discovery and proceed directly to analysis and generation. Should be faster."

If the user provided `--har`, adapt: "You have provided a HAR capture, so I shall generate a spec from your traffic and skip browser browser-sniffing."

Then ask via `AskUserQuestion`:

- **question:** `"Anything you want me to know before I begin? A vision for what this CLI should do, specific features you care about, or auth context I should have?"`
- **header:** `"Briefing"`
- **multiSelect:** `false`
- **options:**
  1. **label:** `"Let's go (recommended)"` — **description:** `"Start research now. I'll ask about API keys, browser auth, or other context when I need them."`
  2. **label:** `"I have context to share"` — **description:** `"Tell me your vision, specific features, or auth context (API key, logged-in browser session) before research starts."`

**Do not add additional options** — auth is already handled by the API Key Gate in [Phase 0](phases/03-resolve-and-reuse.md) and Phase 1.6 in [phases/05-pre-browser-sniff-auth-intelligence.md](phases/05-pre-browser-sniff-auth-intelligence.md) downstream. A user who wants to volunteer auth context can do so via option 2's free-text response. The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Let's go (recommended)"**, proceed to the Multi-Source Priority Gate (or, for single-source runs, directly to Phase 0 in [phases/03-resolve-and-reuse.md](phases/03-resolve-and-reuse.md)).

If the user picks **"I have context to share"**, capture their free-text response as `USER_BRIEFING_CONTEXT`. The response may include:

- **Vision / specific features** — captured as-is. This context will be:
  - Added to the Phase 1 Research Brief in [phases/04-research-brief.md](phases/04-research-brief.md) under a `## User Vision` section
  - Used as a 4th self-brainstorm question in Phase 1.5c.5 in [phases/08-ecosystem-absorb-gate.md](phases/08-ecosystem-absorb-gate.md): "Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't cover?"
  - Referenced at the Phase Gate 1.5 absorb gate in [phases/08-ecosystem-absorb-gate.md](phases/08-ecosystem-absorb-gate.md): "You mentioned [summary] at the start. Want to add more, or does the manifest already cover it?"
- **Auth context** — if the user mentions an API key, env var, or logged-in browser session, set the corresponding `AUTH_CONTEXT` fields so the API Key Gate in [Phase 0](phases/03-resolve-and-reuse.md) and Pre-Browser-Sniff Auth Intelligence in [Phase 1.6](phases/05-pre-browser-sniff-auth-intelligence.md) do not re-ask.

### Multi-Source Priority Gate

After the briefing question resolves, inspect the user's original argument AND any `USER_BRIEFING_CONTEXT` they provided. If together they name **two or more distinct services, sites, or APIs** (e.g., "Google Flights and Kayak", "Notion + Linear combo CLI", "flightgoat: Google Flights, Kayak.com/direct, and FlightAware"), this is a combo CLI and priority ordering MUST be confirmed before Phase 1 research in [phases/04-research-brief.md](phases/04-research-brief.md).

**Why this gate exists:** Phase 1 research defaults to the first resolvable spec as the primary source. When the user listed services in a specific order, that order is their intent — but the generator's spec-first bias will silently invert it (picking a well-documented paid API over a free reverse-engineered one the user actually wanted as the headline feature). This has caused real user-visible failures where the CLI shipped with the wrong primary and required a paid API key for what the user intended as the free primary command.

**Parse the order from the prose.** Use the user's wording verbatim. Commas, "then", "and", explicit "primary/secondary", or numbered lists all signal ordering. If the user wrote "Google Flights, Kayak, FlightAware" — that is the order. Do not reorder by spec availability, tier, or ease of generation.

**Confirm via `AskUserQuestion`:**

> "You mentioned **<Source A>**, **<Source B>**, and **<Source C>**. I'll treat **<Source A>** as the primary — it gets the headline commands, the top of the README, and the first-run experience. Is that the right order?"

Options:
1. **Yes, that order is correct** — Proceed with `SOURCE_PRIORITY=[A, B, C]` captured to run state.
2. **Different order** — User provides the correct ordering; capture it.
3. **They're peers, no primary** — Rare; capture as equal weighting but warn the user that one will still lead the README.

Write the confirmed ordering to `$API_RUN_DIR/source-priority.json`:

```json
{
  "sources": ["google-flights", "kayak-direct", "flightaware"],
  "confirmed_at": "<ISO timestamp>",
  "raw_user_phrasing": "<verbatim text that established the order>"
}
```

**Phase 1 in [phases/04-research-brief.md](phases/04-research-brief.md) MUST consult this file.** When selecting a spec source, the primary source wins even if it has no spec and a later source has a clean OpenAPI. When the primary has no official spec, flag that openly in the brief under `## Source Priority` (see the template in [phases/04-research-brief.md](phases/04-research-brief.md)) and route to the browser-sniff/docs path for the primary — do not promote a secondary source just because its spec is cleaner.

**Economics check.** If the confirmed primary source is free (no API key required) AND the generator's default path would make the primary CLI commands require a paid key (because the auth applies broadly or because a paid secondary source is bleeding into the primary path), surface the tradeoff explicitly before generating:

> "The primary source (**<Source A>**) is free, but the default path would require a **<paid key>** for the headline commands because <reason>. Options: (1) keep primary free, gate only the secondary commands on the paid key; (2) require the paid key for everything; (3) drop the paid source."

Default to option 1 unless the user overrides. Record the decision in `source-priority.json` under `auth_scoping`.

**Single-source runs:** If only one service is named, skip this gate entirely — no ordering to confirm.

---

## Outputs

Every run writes up to 5 concise artifacts under the current managed run and archives them to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/`:

1. `research/<stamp>-feat-<api>-pp-cli-brief.md`
2. `research/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`
3. `proofs/<stamp>-fix-<api>-pp-cli-build-log.md`
4. `proofs/<stamp>-fix-<api>-pp-cli-shipcheck.md`
5. `proofs/<stamp>-fix-<api>-pp-cli-live-smoke.md` (only if live testing runs)

These do not need to be 200+ lines. Keep them dense, evidence-backed, and directly useful.
