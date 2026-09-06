# Run resolution

Resolve what to build, from what source, and in what order before entering
`03-resolve-and-reuse`. This file owns the orientation prompt, the briefing prompt, and the
priority gate for combo runs. It sets `USER_BRIEFING_CONTEXT`, `AUTH_CONTEXT`,
and `SOURCE_PRIORITY`. YAML, JSON, local paths, and URLs are all valid spec
inputs for the generation and verification tools.

After [preflight](../phases/01-preflight.md) has completed, check whether the user provided arguments. Handle two cases:

## No Arguments: Orientation

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

## With Arguments: Briefing

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

**Do not add additional options** — auth is already handled by the API Key Gate in [Phase 0](../phases/03-resolve-and-reuse.md) and Phase 1.6 in [phases/05-pre-browser-sniff-auth-intelligence.md](../phases/05-pre-browser-sniff-auth-intelligence.md) downstream. A user who wants to volunteer auth context can do so via option 2's free-text response. The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Let's go (recommended)"**, proceed to the Multi-Source Priority Gate below (or, for single-source runs, directly to Phase 0 in [phases/03-resolve-and-reuse.md](../phases/03-resolve-and-reuse.md)).

If the user picks **"I have context to share"**, capture their free-text response as `USER_BRIEFING_CONTEXT`. The response may include:

- **Vision / specific features** — captured as-is. This context will be:
  - Added to the Phase 1 Research Brief in [phases/04-research-brief.md](../phases/04-research-brief.md) under a `## User Vision` section
  - Used as a 4th self-brainstorm question in Phase 1.5c.5 in [phases/08-ecosystem-absorb-gate.md](../phases/08-ecosystem-absorb-gate.md): "Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't cover?"
  - Referenced at the Phase Gate 1.5 absorb gate in [phases/08-ecosystem-absorb-gate.md](../phases/08-ecosystem-absorb-gate.md): "You mentioned [summary] at the start. Want to add more, or does the manifest already cover it?"
- **Auth context** — if the user mentions an API key, env var, or logged-in browser session, set the corresponding `AUTH_CONTEXT` fields so the API Key Gate in [Phase 0](../phases/03-resolve-and-reuse.md) and Pre-Browser-Sniff Auth Intelligence in [Phase 1.6](../phases/05-pre-browser-sniff-auth-intelligence.md) do not re-ask.

## Multi-Source Priority Gate

After the briefing question resolves, inspect the user's original argument AND any `USER_BRIEFING_CONTEXT` they provided. If together they name **two or more distinct services, sites, or APIs** (e.g., "Google Flights and Kayak", "Notion + Linear combo CLI", "flightgoat: Google Flights, Kayak.com/direct, and FlightAware"), this is a combo CLI and priority ordering MUST be confirmed before Phase 1 research in [phases/04-research-brief.md](../phases/04-research-brief.md).

**Why this gate exists:** Phase 1 research defaults to the first resolvable spec as the primary source. When the user listed services in a specific order, that order is their intent — but the generator's spec-first bias will silently invert it (picking a well-documented paid API over a free reverse-engineered one the user actually wanted as the headline feature). This has caused real user-visible failures where the CLI shipped with the wrong primary and required a paid API key for what the user intended as the free primary command.

**Parse the order from the prose.** Use the user's wording verbatim. Commas, "then", "and", explicit "primary/secondary", or numbered lists all signal ordering. If the user wrote "Google Flights, Kayak, FlightAware" — that is the order. Do not reorder by spec availability, tier, or ease of generation.

**Confirm via `AskUserQuestion`:**

> "You mentioned **<Source A>**, **<Source B>**, and **<Source C>**. I'll treat **<Source A>** as the primary — it gets the headline commands, the top of the README, and the first-run experience. Is that the right order?"

Options:
1. **Yes, that order is correct** — Proceed with `SOURCE_PRIORITY=[A, B, C]` captured to run state.
2. **Different order** — User provides the correct ordering; capture it.
3. **They're peers, no primary** — Rare; capture as equal weighting but warn the user that one will still lead the README.

Keep the confirmed ordering in session state as `SOURCE_PRIORITY`. Do **not**
write `$API_RUN_DIR/source-priority.json` here: that directory is created in
[02-run-initialization](../phases/02-run-initialization.md). After that phase
allocates the run directory, persist:

```json
{
  "sources": ["google-flights", "kayak-direct", "flightaware"],
  "confirmed_at": "<ISO timestamp>",
  "raw_user_phrasing": "<verbatim text that established the order>"
}
```

**Phase 1 in [phases/04-research-brief.md](../phases/04-research-brief.md) MUST consult this file.** When selecting a spec source, the primary source wins even if it has no spec and a later source has a clean OpenAPI. When the primary has no official spec, flag that openly in the brief under `## Source Priority` (see the template in [phases/04-research-brief.md](../phases/04-research-brief.md)) and route to the browser-sniff/docs path for the primary — do not promote a secondary source just because its spec is cleaner.

**Economics check.** If the confirmed primary source is free (no API key required) AND the generator's default path would make the primary CLI commands require a paid key (because the auth applies broadly or because a paid secondary source is bleeding into the primary path), surface the tradeoff explicitly before generating:

> "The primary source (**<Source A>**) is free, but the default path would require a **<paid key>** for the headline commands because <reason>. Options: (1) keep primary free, gate only the secondary commands on the paid key; (2) require the paid key for everything; (3) drop the paid source."

Default to option 1 unless the user overrides. Record the decision in `source-priority.json` under `auth_scoping`.

**Single-source runs:** If only one service is named, skip this gate entirely — no ordering to confirm.
