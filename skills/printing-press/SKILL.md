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
/printing-press https://postman.com/explore
/printing-press https://postman.com
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

### Polish Mode (Standalone Skill)

For second-pass improvements to an existing CLI, use the standalone polish skill:

```bash
/printing-press-polish redfin
```

See the `printing-press-polish` skill for details. It runs diagnostics, fixes verify failures, removes dead code, cleans up descriptions and README, and offers to publish.

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

## Orientation & Briefing

Before setup, check whether the user provided arguments. Handle two cases:

### No Arguments: Orientation

If the user typed `/printing-press` with no arguments (no API name, no `--spec`, no `--har`, no URL), print an orientation and ask what they'd like to build:

> The Printing Press generates a fully functional CLI for any API. You give it an API name, a spec file, or a URL. It researches the landscape, catalogs every feature that exists in any competing tool, invents novel features of its own, then generates a Go CLI that matches and beats everything out there — with offline search, agent-native output, and a local SQLite data layer.
>
> By the end, you'll have a working CLI in `~/printing-press/library/` that you can use for yourself, ship on your own, or apply to add to the printing-press library.
>
> The process takes 10-40 minutes depending on API complexity. Simple APIs with official specs (Stripe, GitHub) are faster. Undocumented APIs that need discovery (ESPN, Domino's) take longer.

Then present examples and ask via `AskUserQuestion`:

> "What API would you like to build a CLI for?"

Options:
1. **I'll type it** — (User provides the API name, URL, or spec path via the automatic "Other" option)

Show example invocations alongside:
```
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press https://postman.com
```

After receiving the answer, set it as the argument and proceed to the briefing below.

### With Arguments: Briefing

When the user provided an argument (API name, `--spec`, `--har`, or URL), print a brief process overview before the setup contract runs. This sets expectations and collects any upfront context.

Print as prose (in Victorian voice):

> Very well. Setting the type for `<API>`.
>
> **Here is how this will proceed:**
> 1. I shall research `<API>` across the internet: official docs, community wrappers, competing CLIs, MCP servers, and npm/PyPI packages
> 2. I shall catalog every feature that exists in any tool, then devise novel features of my own that no existing tool offers
> 3. I shall present what I found and what I invented — you will have a chance to add your own ideas or adjust the plan before I build
> 4. I shall generate a Go CLI, build every feature from the plan, then verify quality through dogfood, runtime verification, and scoring
>
> **What you will have at the end:** A fully functional CLI at `~/printing-press/library/<api>-pp-cli` that you can use yourself, ship on your own, or apply to add to the printing-press library.
>
> **Time:** 10-40 minutes depending on API complexity.
>
> **Things that help if you have them:**
> - An API key (for live smoke testing at the end)
> - A logged-in browser session (for discovering authenticated endpoints)
> - A spec file or HAR capture (skips discovery)

If the user provided `--spec`, adapt: "You have provided a spec, so I shall skip discovery and proceed directly to analysis and generation. Should be faster."

If the user provided `--har`, adapt: "You have provided a HAR capture, so I shall generate a spec from your traffic and skip browser sniffing."

Then ask via `AskUserQuestion`:

**question:** "Anything you want me to know before I begin? A vision for what this CLI should do, specific features you care about, or context I should have?"

**options:**
1. **No, let's go** — "Start the process with default intelligence"
2. **I have context to share** — "Tell me your vision, priorities, or specific features you care about"
3. **I have an API key or I'm logged in** — "Share auth context now so I can plan for authenticated discovery and testing"

If the user selects **"No, let's go"**, proceed to Setup immediately.

If the user selects **"I have context to share"**, capture their free-text response as `USER_BRIEFING_CONTEXT`. This context will be:
- Added to the Phase 1 Research Brief under a `## User Vision` section
- Used as a 4th self-brainstorm question in Phase 1.5c.5: "Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't cover?"
- Referenced at the Phase Gate 1.5 absorb gate: "You mentioned [summary] at the start. Want to add more, or does the manifest already cover it?"

If the user selects **"I have an API key or I'm logged in"**, ask which one and capture it. Set `AUTH_CONTEXT` fields so the API Key Gate (Phase 0.5) and Pre-Sniff Auth Intelligence (Phase 1.6, if implemented) do not re-ask.

---

## Setup

Read and apply [references/voice.md](references/voice.md) for this session.

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

# --- Codex mode detection (must run as part of setup, not a separate step) ---
# Codex mode: opt-in only. User must pass "codex" or "--codex" to enable.
if echo "$ARGUMENTS" | grep -qiE '(^| )(--?codex|codex)( |$)'; then
  CODEX_MODE=true
else
  CODEX_MODE=false
fi

# Environment guard: don't delegate if already inside a Codex sandbox
if [ "$CODEX_MODE" = "true" ]; then
  if [ -n "$CODEX_SANDBOX" ] || [ -n "$CODEX_SESSION_ID" ]; then
    CODEX_MODE=false
  fi
fi

# Health check: verify codex binary exists
if [ "$CODEX_MODE" = "true" ]; then
  if command -v codex >/dev/null 2>&1; then
    CODEX_MODEL=$(codex config get model 2>/dev/null || echo "gpt-5.4")
    echo "Codex mode enabled (model: $CODEX_MODEL). Code-writing tasks will be delegated to Codex."
  else
    echo "Codex CLI not found - running in standard mode."
    CODEX_MODE=false
  fi
fi

# Circuit breaker state
CODEX_CONSECUTIVE_FAILURES=0
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
DISCOVERY_DIR="$API_RUN_DIR/discovery"
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

   **URL Detection** — If the argument contains `://`, it's a URL. Determine whether it's a spec or a website before proceeding.

   **Step 1: Content probe.** Fetch the URL (light GET via `WebFetch`) and inspect the response:
   - Check the `Content-Type` header and the first few lines of the body.
   - If the fetch fails (timeout, 404, DNS error), skip to Step 2 — treat it as a website.

   If the content starts with `openapi:`, `swagger:`, or is valid JSON containing an `"openapi"` or `"swagger"` key → it's a spec. Treat as `--spec` and proceed directly. No disambiguation needed.

   If the content is a HAR file (JSON with `"log"` and `"entries"` keys) → treat as `--har` and proceed directly.

   **Step 2: Disambiguation.** If the content is HTML or the probe failed, ask the user what they want. Extract the site name from the hostname (e.g., `postman.com` → "Postman", `app.linear.app` → "Linear"). Derive `<api>` from the site name using the same `cleanSpecName` normalization the generator uses.

   Use `AskUserQuestion` with:
   - **question:** `"What kind of CLI do you want for <SiteName>?"`
   - **header:** `"CLI target"`
   - **multiSelect:** `false`
   - **options:**
     1. **label:** `"<SiteName>'s official API"` — **description:** `"Build a CLI for <SiteName>'s documented API (e.g. REST endpoints, webhooks, OAuth)"`
     2. **label:** `"The <SiteName> website itself"` — **description:** `"Build a CLI that does what the website does — I'll figure out the underlying API by exploring the site"`

   The user can also pick the automatic "Other" option to describe what they're after in free text.

   **Routing after disambiguation:**
   - "<SiteName>'s official API" → use `<api>` as the argument, proceed with normal discovery (Phase 1 research, then Phase 1.7 sniff gate evaluates independently as usual)
   - "The <SiteName> website itself" → use `<api>` as the argument, set `SNIFF_TARGET_URL=<url>`. Proceed to Phase 1 research. When Phase 1.7 is reached, skip the sniff gate decision and go directly to "If user approves sniff" (the user already approved in Phase 0 — do not re-ask). Use `SNIFF_TARGET_URL` as the starting URL for browser capture.
   - "Other" → read the user's free-form response and adapt

   **End of URL detection.** The remaining spec resolution rules apply when the argument is NOT a URL:

   - If the user passed `--har <path>`, this is a HAR-first run. Run `printing-press sniff --har <path> --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"` to generate a spec from captured traffic. Use the generated spec as the primary spec source for the rest of the pipeline. Skip the sniff gate in Phase 1.7 (sniff already ran).
   - If the user passed `--spec`, use it directly (existing behavior).
   - Otherwise, proceed with normal discovery (catalog, KnownSpecs, apis-guru, web search).
2. Check for prior research in:
   - `$PRESS_MANUSCRIPTS/<api>/*/research/*`
   - `$REPO_ROOT/docs/plans/*<api>*` (legacy fallback)
3. Reuse good prior work instead of redoing it.
4. **Library Check** — Check if a CLI for this API already exists in the library and present the user with context and options.

   ```bash
   CLI_DIR="$PRESS_LIBRARY/<api>-pp-cli"
   if [ -d "$CLI_DIR" ]; then
     # Read manifest if available
     MANIFEST="$CLI_DIR/.printing-press.json"
     if [ -f "$MANIFEST" ]; then
       PRESS_VERSION=$(cat "$MANIFEST" | grep -o '"printing_press_version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"printing_press_version"[[:space:]]*:[[:space:]]*"//;s/"//')
       GENERATED_AT=$(cat "$MANIFEST" | grep -o '"generated_at"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"generated_at"[[:space:]]*:[[:space:]]*"//;s/"//')
     fi
     # Get directory modification time as fallback
     CLI_MTIME=$(stat -f "%Sm" -t "%Y-%m-%d" "$CLI_DIR" 2>/dev/null || stat -c "%y" "$CLI_DIR" 2>/dev/null | cut -d' ' -f1)
   fi
   ```

   If the directory exists, display context and present options using `AskUserQuestion`:

   > Found existing `<cli-name>` in library (last modified `<date>`).

   If `PRESS_VERSION` is available, append: `Built with printing-press v<version>.`

   If prior research was also found (step 2), include the research summary alongside the library info.

   Then ask:
   1. **"Generate a fresh CLI"** — Re-runs the generator into the same directory (`--force`), overwrites generated code, then rebuilds transcendence features. Prior research is reused if recent. ~15-20 min.
   2. **"Improve existing CLI"** — Keeps all current code, audits for quality gaps, implements top improvements. The generator is not re-run. ~10 min.
   3. **"Review prior research first"** — Show the full research brief and absorb manifest before deciding.

   If the user picks option 1, proceed to Phase 1 (research) and then Phase 2 (generate) as normal.
   If the user picks option 2, invoke `/printing-press-polish <cli-name>` to improve the existing CLI.
   If the user picks option 3, display the prior research, then re-present options 1 and 2.

   If no CLI exists in the library, skip this step and proceed normally.

5. **API Key Gate** — Check whether this API requires authentication, then handle accordingly.

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

**When `SNIFF_TARGET_URL` is set:** Skip the catalog check, spec/docs search, and SDK wrapper search — none of these exist for an undocumented website feature. Focus research on understanding what the site/feature does, who uses it, what workflows it supports, and what competitors offer similar functionality. The spec will come from sniffing in Phase 1.7.

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
- **Check GitHub issues on the top wrapper/SDK repo for "403", "blocked", "broken", "deprecated", "rate limit".** If multiple issues report the API is inaccessible or broken, flag this in the research brief as a reachability risk. This is critical for unofficial/reverse-engineered APIs.
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

## Reachability Risk
- [None / Low / High] [evidence: e.g., "6 open issues on reteps/redfin about 403 errors since 2025"]

## Top Workflows
1. ...

## Table Stakes
- ...

## Data Layer
- Primary entities:
- Sync cursor:
- FTS/search:

## User Vision
- [USER_BRIEFING_CONTEXT if provided, otherwise omit this section]

## Product Thesis
- Name:
- Why it should exist:

## Build Priorities
1. ...
2. ...
3. ...
```

**MANDATORY: Before proceeding to Phase 1.5 (Absorb Gate), you MUST evaluate Phase 1.6 (Pre-Sniff Auth Intelligence), Phase 1.7 (Sniff Gate), and Phase 1.8 (Crowd Sniff Gate) below.** If no spec source has been resolved yet (no `--spec`, no `--har`, no catalog spec URL), the sniff gate decision matrix MUST be evaluated. Do not skip to Phase 1.5.

## Phase 1.6: Pre-Sniff Auth Intelligence

After Phase 1 research completes, analyze findings to proactively assess what auth context the user could provide. This step uses research intelligence to ask the right question before sniffing starts, rather than waiting for the user to volunteer "I logged in."

**Skip this step if:** The briefing (Orientation & Briefing section) already captured auth context (`AUTH_CONTEXT` is set from the user selecting "I have an API key or I'm logged in").

**Classify the API's auth profile from research findings:**

| Signal from research | Auth profile | What to ask |
|---------------------|-------------|-------------|
| Community wrappers use API keys (e.g., `STRIPE_SECRET_KEY`), MCP source shows `Authorization: Bearer` headers, spec has `security` section | **API key auth** | "Do you have an API key for `<API>`?" |
| Site has user accounts, research found auth-only features (order history, saved items, rewards, account settings), login pages exist | **Browser session auth** | "This API has authenticated endpoints ([list specific features from research, e.g., order history, saved addresses, rewards]). Are you logged in to `<site>` in your browser? The sniff will find more endpoints if you are." |
| Endpoints accessible without auth, no login-gated features found, community wrappers describe API as "no auth required" | **No auth needed** | Skip this step silently |
| Both API key AND browser session features found | **Dual auth** | Ask about both: API key for smoke testing, browser session for sniff |

**Name the specific features the user would unlock.** Do not say "auth would help." Say "This API has order history, saved addresses, and rewards that require a logged-in session."

**Where signals come from:**
- Phase 1 brief's "Data profile" and "Top Workflows" sections
- Phase 1.5a MCP source code analysis (auth patterns, token formats)
- Community wrapper README "auth" or "authentication" sections
- The API Key Gate's token detection (Phase 0.5) — if it already found a key, don't re-ask

**For API key auth:** Present via `AskUserQuestion`:
> "Do you have an API key for `<API>`? It will be used for read-only live smoke testing in Phase 5."
>
> 1. **Yes** — user provides the key or confirms it's in the environment
> 2. **No, continue without it** — skip live smoke testing

If the user provides a key, set it in `AUTH_CONTEXT` so the API Key Gate (Phase 0.5) does not re-ask.

**For browser session auth:** Present via `AskUserQuestion`:
> "`<API>` has authenticated endpoints ([list features]). Are you logged in to `<site>` in your browser? The sniff will discover more endpoints if you are."
>
> 1. **Yes, I'm logged in** — I'll use your session during sniff
> 2. **No, but I can log in** — I'll help you log in before sniffing
> 3. **No, skip authenticated endpoints** — sniff only public endpoints

Set `AUTH_SESSION_AVAILABLE=true` if the user selects option 1 or 2. The Sniff Gate (Phase 1.7) will use this flag.

**For dual auth:** Ask about both in sequence — API key first (simple env var check), then browser session.

---

## Phase 1.7: Sniff Gate

After Phase 1 research, evaluate whether sniffing the live site would improve the spec. Skip this gate entirely if the user already passed `--har` or `--spec` (spec source is already resolved). If `SNIFF_TARGET_URL` is set (user chose "The website itself" in Phase 0), skip the decision matrix and go directly to "If user approves sniff" — the user already approved, and `SNIFF_TARGET_URL` is the starting URL.

**IMPORTANT:** When the decision matrix below says "Offer sniff", you MUST ask the user via `AskUserQuestion`. Do NOT silently decide to skip sniff because the docs look thorough — the user should make that call. The only case where you skip silently is "spec appears complete" (no gap detected).

**Time budget:** The sniff gate should complete within 3 minutes of the user approving sniff. If browser automation tooling fails to produce results after 3 minutes of attempts, fall back immediately:
- If a spec already exists (enrichment mode): "Sniff failed after 3 minutes — proceeding with existing spec."
- If no spec exists (primary mode): "Sniff failed after 3 minutes — falling back to --docs generation."
Do NOT spend time debugging tool integration issues. The sniff is optional enrichment, not a blocking requirement. If the first approach fails, fall back to the next option — do not retry the same broken approach.

### When to offer sniff

| Spec found? | Research shows gaps? | Auth required? | Action |
|-------------|---------------------|----------------|--------|
| Yes | Yes — docs or competitors show significantly more endpoints than the spec | No | **MUST offer sniff as enrichment** |
| Yes | No — spec appears complete | Any | Skip silently |
| No | Community docs exist (e.g., Public-ESPN-API) | No | **MUST offer sniff OR --docs** — present both options so the user decides |
| No | No docs found either | No | **MUST offer sniff as primary discovery** |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=true` | **Offer authenticated sniff** — the user confirmed a session in Phase 1.6 |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=false` | Skip — fall back to `--docs` |

**Gap detection heuristic:** If Phase 1 research found documentation, competitor tools, or community projects that reference significantly more endpoints or features than the resolved spec covers, that's a gap signal. Example: "The Zuplo OpenAPI spec has 42 endpoints, but the Public-ESPN-API docs describe 370+."

### Sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more (competitors reference M+ features). Want me to sniff `<url>` to discover endpoints the spec missed? I'll check for browser-use or agent-browser and install if needed."
>
> Options:
> 1. **Yes — sniff and merge** (browse the site, capture traffic, merge discovered endpoints with the existing spec. Installs capture tools if needed.)
> 2. **No — use existing spec** (proceed with what we have)

### Sniff as primary (no spec found)

Present to the user via `AskUserQuestion`. **If `AUTH_SESSION_AVAILABLE=true`**, include an authenticated sniff option:

> "No OpenAPI spec found for `<API>`. Want me to sniff `<likely-url>` to discover the API from live traffic?"
>
> Options:
> 1. **Yes — authenticated sniff** (use your browser session to discover both public and authenticated endpoints. Recommended since you confirmed a session.) *(Only show when `AUTH_SESSION_AVAILABLE=true`)*
> 2. **Yes — sniff the live site** (browse `<url>` anonymously, capture API calls, generate a spec. Installs capture tools if needed.)
> 3. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 4. **No — I'll provide a spec or HAR** (user will supply input manually)

When `AUTH_SESSION_AVAILABLE=false`, show only options 2-4 (the existing 3-option prompt).

### If user approves sniff

#### Sniff Pacing

When making API calls during sniff (browser-use eval, fetch, or direct HTTP requests), apply adaptive pacing to avoid rate limits:

1. **Start conservative**: Wait 1 second between API calls
2. **Ramp up on success**: After 5 consecutive successful calls, reduce the delay by 20% (minimum 0.3 seconds)
3. **Back off on 429**: If you get a rate-limited response (HTTP 429), immediately double the delay and log: "Rate limited — increasing delay to Xs"
4. **Hard stop on repeated 429s**: If you hit 3 consecutive 429s, pause for 30 seconds before continuing
5. **Never abort**: Rate limiting during sniff is recoverable. Always continue after the backoff — do not abort discovery due to rate limits

Track the current delay mentally. Report the effective rate when summarizing sniff results: "Sniffed N endpoints at ~X req/s effective rate."

#### Proxy Pattern Detection

After capturing API traffic, check if the API uses a proxy-envelope pattern:

1. **Same-URL signal**: If all captured XHR/fetch URLs resolve to the same path (e.g., all calls go to `_api/ws/proxy`), the API likely uses a proxy pattern
2. **Envelope signal**: If intercepted request bodies contain `service`, `method`, and `path` keys (or similar routing fields), it's a proxy-envelope
3. **Confirmation**: If both signals are present, classify as `client_pattern: proxy-envelope`

When a proxy pattern is detected:
- Note the proxy URL (it becomes the spec's `servers[0].url`)
- Extract the service routing from request bodies — build an `x-proxy-routes` map of path prefixes to service names
- Write `x-proxy-routes` into the generated spec's `info` extensions:
  ```yaml
  info:
    x-proxy-routes:
      /v1/api/: publishing
      /search-all: search
  ```
- Pass `--client-pattern proxy-envelope` to the generate command in Phase 2

#### Step 1: Detect capture tools

Check which browser automation tools are available:

```bash
# Prefer browser-use (CLI-driven, Performance API collection)
if command -v browser-use >/dev/null 2>&1 || uvx browser-use --help >/dev/null 2>&1; then
  SNIFF_BACKEND="browser-use"
# Fall back to agent-browser (CLI-driven, Claude drives the loop)
elif command -v agent-browser >/dev/null 2>&1; then
  SNIFF_BACKEND="agent-browser"
else
  SNIFF_BACKEND="none"
fi

# Check if browser-use can run in autonomous agent mode (optional, not required)
BROWSER_USE_HAS_LLM=false
if [ -n "$ANTHROPIC_API_KEY" ] || [ -n "$OPENAI_API_KEY" ] || [ -n "$BROWSER_USE_API_KEY" ]; then
  BROWSER_USE_HAS_LLM=true
fi
```

If a tool is found, report: "Using **<tool>** for traffic capture (CLI-driven mode — no LLM key needed)." and proceed to Step 1c to verify compatibility.

**Important:** browser-use has two modes: autonomous Agent mode (requires an LLM API key like ANTHROPIC_API_KEY) and CLI mode (open/eval/scroll — no key needed). **Always use CLI mode for sniff.** It is more reliable, version-stable, and does not require the user to provide an additional API key. Do NOT attempt to use browser-use's Python `Agent` class — it requires an LLM key that may not be available.

#### Step 1b: Install capture tool (if none found)

If neither tool is installed, offer to install via `AskUserQuestion`:

> "No browser automation tool found. I need one to sniff the live site. Which would you like to install?"
>
> Options:
> 1. **Install browser-use (Recommended)** — "CLI-driven browser automation. Claude drives the browsing via open/eval/scroll commands. Requires Python. ~2 min install."
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

After install, re-run detection. If `browser-use` is now available, set `SNIFF_BACKEND="browser-use"` and proceed to Step 1c. If install failed, show the error and offer agent-browser as alternative or fall back to manual HAR.

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

After install, re-run detection. If `agent-browser` is now available, set `SNIFF_BACKEND="agent-browser"` and proceed to Step 1c. If install failed, show the error and fall back to manual HAR.

**If user picks manual HAR**, ask the user for a HAR file path and skip to Step 3.

#### Step 1c: Verify capture tool compatibility

After detection (Step 1) or installation (Step 1b), verify the installed version supports the CLI commands the sniff process needs.

**For browser-use** — The CLI 2.0 commands (`open`, `eval`, `scroll`, `close`) all shipped in **v0.12.3**. Versions before that have an incomplete or experimental CLI that won't work for sniff.

```bash
# browser-use has no --version flag; get version from pip metadata
BROWSER_USE_VERSION=$(pip show browser-use 2>/dev/null | grep -i '^Version:' | awk '{print $2}')
MIN_BROWSER_USE="0.12.3"

# Compare versions (lexicographic sort works for dotted semver)
if printf '%s\n' "$MIN_BROWSER_USE" "$BROWSER_USE_VERSION" | sort -V | head -1 | grep -qx "$MIN_BROWSER_USE"; then
  BROWSER_USE_COMPAT=true
else
  BROWSER_USE_COMPAT=false
fi
```

**For agent-browser** — check that the `network` subcommand exists (needed for HAR capture):

```bash
AGENT_BROWSER_VERSION=$(agent-browser --version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
if agent-browser network --help >/dev/null 2>&1; then
  AGENT_BROWSER_COMPAT=true
else
  AGENT_BROWSER_COMPAT=false
fi
```

**If the selected tool fails the compatibility check**, offer to upgrade via `AskUserQuestion`:

> "Found **<tool>** v<version>, but sniff requires v<min-version>+ for CLI capture commands. Would you like to upgrade?"
>
> Options:
> 1. **Yes — upgrade <tool>** — runs the appropriate upgrade command (see below)
> 2. **Try <other-tool> instead** — switch to the other backend (install it if needed)
> 3. **Skip — I'll provide a HAR manually**

**Upgrade commands:**

- **browser-use**: `uv pip install --upgrade browser-use` (if `uv` available) or `pip install --upgrade browser-use`
- **agent-browser**: `brew upgrade agent-browser` (if brew-installed) or `npm update -g agent-browser`

After upgrade, re-check the version. If the upgrade resolves the issue, proceed to Step 2. If it doesn't, offer the next fallback (other tool or manual HAR).

**Do NOT upgrade automatically.** Always ask permission first — upgrading packages can have side effects on the user's environment.

If the tool passes the version check, proceed to Step 1d (if authenticated sniff) or Step 2a/2b (if anonymous sniff).

#### Step 1d: Session Transfer (authenticated sniff only)

This step only runs when the user chose "authenticated sniff" (from Phase 1.7's sniff-as-primary or sniff-as-enrichment prompts, or when `AUTH_SESSION_AVAILABLE=true` and the user confirmed).

**Situation detection:**
```bash
CHROME_RUNNING=false
if pgrep -x "Google Chrome" >/dev/null 2>&1; then
  CHROME_RUNNING=true
fi
```

**When Chrome IS running**, prefer agent-browser (attaches to live browser without closing it):

Present via `AskUserQuestion`:
> "Chrome is running. I can attach to it and grab your session."
>
> 1. **Grab session from your Chrome** (Recommended) — "Saves your cookies, then sniffs in a separate headless browser. Chrome stays untouched."
> 2. **Sniff in your Chrome directly** — "Stays connected to your real Chrome. You'll see pages changing during the sniff (~60-90 seconds). Simplest approach — no daemon juggling."
> 3. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I sniff. ~1 minute."
> 4. **I'll export a HAR file** — "You browse the site in DevTools, export the HAR."

For option 1 (save-then-restore):

**IMPORTANT:** `--auto-connect`, `--state`, `--profile`, and `--headed` are daemon launch options in agent-browser. They only take effect when starting a new daemon. You MUST close the daemon between save and load.

```bash
# Grab cookies from running Chrome
agent-browser --auto-connect state save "$DISCOVERY_DIR/session-state.json" 2>&1

# Close the auto-connect daemon so --state can start a fresh one
agent-browser close 2>&1

# Start a new headless daemon with the saved auth state
agent-browser --state "$DISCOVERY_DIR/session-state.json" open <url>
```
If auto-connect fails (no debug port), explain: "Chrome doesn't have remote debugging enabled. Quit Chrome and relaunch with `--remote-debugging-port=9222`, or pick option 2."

For option 2 (stay in auto-connect mode):
```bash
# Stay connected to the user's real Chrome — all cookies are already present
agent-browser --auto-connect open <url>
agent-browser network har start
# ... browse pages (user will see their Chrome tabs changing) ...
agent-browser network har stop <path>
# No close/restart needed — daemon stays connected to real Chrome
```

For option 1 with browser-use (if agent-browser not available):
```bash
browser-use open <url> --connect
```

**When Chrome is NOT running**, prefer browser-use (loads real Chrome profile with all cookies):

Present via `AskUserQuestion`:
> "Chrome isn't running. I can load your Chrome profile directly — all your saved logins will be available."
>
> 1. **Use your Chrome profile** (Recommended, requires browser-use) — "Loads your real Chrome profile. Zero setup."
> 2. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I sniff."
> 3. **I'll export a HAR file**

For option 1 (browser-use profile reuse):
```bash
browser-use open <url> --profile "Default"
```
If browser-use is not available, fall back to agent-browser headed login.

If Chrome profile lock error occurs (Chrome is actually running): "Chrome's profile is locked. Quit Chrome first, or switch to option 2."

**When both tools are available**, recommend the situationally better one:
- Chrome running: prefer agent-browser `--auto-connect`
- Chrome not running: prefer browser-use `--profile "Default"`

**For headed login (option 2 with either tool):**
```bash
# agent-browser
agent-browser --headed --session-name "<api>-auth" open <login-url>
# or browser-use
browser-use open <login-url> --headed --session "<api>-auth"
```
Instruct the user: "A browser window is open. Please log in to `<site>`. Let me know when you're done."
After login, save state:
```bash
agent-browser state save "$DISCOVERY_DIR/session-state.json"
```
Close the headed browser and restart headless with the saved state.

**For HAR export (option 3):** Guide the user through DevTools > Network > Save all as HAR. Then use `--har` path.

**After any session transfer method**, verify cookies transferred before proceeding:

```bash
# Verify auth cookies are present for the target domain
COOKIES=$(agent-browser cookies get --json 2>/dev/null)
if echo "$COOKIES" | grep -q "<target-domain>"; then
  echo "Session transfer verified — found <target-domain> cookies."
else
  echo "WARNING: No <target-domain> cookies found."
fi
```

If no target-domain cookies are found, present via `AskUserQuestion`:

> "Session transfer failed — no `<target-domain>` cookies found in the browser. The sniff would run unauthenticated."
>
> 1. **Try auto-connect mode instead** — "Stay connected to your real Chrome where you're already logged in"
> 2. **Log in manually** — "I'll open a headed browser. You log in, then I sniff."
> 3. **Continue without auth** — "Sniff only public endpoints"
> 4. **Provide HAR manually** — "Export a HAR yourself from browser DevTools"

If cookies are verified, proceed to Steps 2a/2b capture flow with the authenticated session loaded. The session state file is stored at `$DISCOVERY_DIR/session-state.json`.

#### Step 2a: browser-use CLI capture (preferred)

Claude drives browser-use directly via CLI commands — no LLM key needed, no Python API versioning issues. Uses the browser's native Performance API to collect API endpoint URLs from each page.

**IMPORTANT: Run the page collection loop in foreground, not background.** The loop takes ~60-90 seconds for 10-15 pages. Background execution has unreliable output capture for shell functions that call browser-use. Always run this inline.

**Step 2a.1: Build the page list**

From Phase 1 research, identify 10-15 target pages that exercise different parts of the API. Include:
- Homepage
- Scoreboard/listing pages for each major resource (scores, standings, teams)
- Detail pages (individual team, player, event)
- Search results
- Stats/leaders pages
- News pages

**Step 2a.2: Collect API URLs**

Open a headless browser session, then visit each page and collect API URLs using the Performance API:

```bash
# Start collection
mkdir -p "$DISCOVERY_DIR"
SNIFF_URLS="$DISCOVERY_DIR/sniff-urls.txt"
> "$SNIFF_URLS"

# For EACH target page (run this loop in foreground — do NOT use run_in_background):
browser-use open "<target-page-url>"
sleep 4  # Wait for initial page load API calls to complete
# Apply sniff pacing delay (starting at 1s, adapts per Sniff Pacing rules above)
browser-use scroll down  # Trigger lazy-loaded content
sleep 1
# Apply sniff pacing delay before next eval call

# Collect API URLs via Performance API (browser-native, no injection needed)
browser-use eval "var e=performance.getEntriesByType('resource');var u=[];for(var i=0;i<e.length;i++){var n=e[i].name;if(n.indexOf('<api-domain-1>')>-1||n.indexOf('<api-domain-2>')>-1)u.push(n);}u.join('|||');"

# Parse the result and append to collection file
# The eval output is "result: url1|||url2|||url3"
# Split on ||| and append each URL to the file
```

Replace `<api-domain-1>`, `<api-domain-2>` etc. with the API domains discovered in Phase 1 research (e.g., `api.espn.com`, `sports.core.api`, `site.web.api`).

**Why Performance API:** It is built into every browser, captures all resource loads (including those that fire before any JS interceptor could be injected), survives within a page lifecycle, and returns simple URL strings. Do NOT use `fetch`/`XMLHttpRequest` monkey-patching — it breaks on page navigation.

**Step 2a.3: Deduplicate and normalize**

After collecting from all pages:
```bash
# Strip query parameters and deduplicate to find unique API path patterns
cat "$SNIFF_URLS" | sed 's/\?.*//' | sort -u > "$DISCOVERY_DIR/sniff-unique-paths.txt"
```

**Step 2a.4: Generate enriched capture**

The Performance API gives us URLs but not response bodies. To feed `printing-press sniff`, we need to call each unique API endpoint and capture the response:

```bash
# For each unique API URL, fetch it and build a simple capture file
# printing-press sniff accepts HAR or enriched capture JSON
# When fetching each unique API URL to build enriched capture:
# Apply sniff pacing between requests (1s initial, adaptive per Sniff Pacing rules)
# On 429: double delay, log, continue with remaining URLs
```

Alternatively, if the URL count is small enough, the unique path patterns alone are sufficient to identify what the existing spec is missing — compare against the spec and report the gap without needing full HAR capture.

**Step 2a.5: Close browser**

```bash
browser-use close
```

#### Step 2b: agent-browser capture (fallback)

If browser-use is not available, use agent-browser with Claude driving the exploration. **Note:** agent-browser HAR does not include response bodies. Use the enriched capture workflow to get them.

1. **Browse and capture**:
   ```bash
   # agent-browser is headless by default; use --headed to show the browser window
   agent-browser open <target-url>
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
   - Apply sniff pacing between interactions (1s initial, adaptive per Sniff Pacing rules)

3. **Capture response bodies** (agent-browser HAR omits them):
   ```bash
   agent-browser network requests --type xhr,fetch --json
   ```
   For each API request (filter by JSON content-type, skip analytics domains):
   ```bash
   agent-browser network request <request-id> --json
   # Apply sniff pacing between response body fetches
   # These are direct API calls and most likely to trigger rate limits
   ```
   Combine HAR metadata + response bodies into an enriched capture JSON at `$DISCOVERY_DIR/sniff-capture.json`.

4. **Stop HAR recording**:
   ```bash
   agent-browser network har stop "$DISCOVERY_DIR/sniff-capture.har"
   ```

#### Step 3: Analyze capture

Run websniff on the captured traffic:
```bash
printing-press sniff --har "$DISCOVERY_DIR/sniff-capture.har" --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"
```

If using agent-browser's enriched capture format instead:
```bash
printing-press sniff --har "$DISCOVERY_DIR/sniff-capture.json" --name <api> --output "$RESEARCH_DIR/<api>-sniff-spec.yaml"
```

#### Step 4: Report and update spec source

Report: "Sniff discovered **N endpoints** across **M resources**. [X new endpoints not in the original spec.]"

Update the spec source for Phase 2:
- **Enrichment mode**: Phase 2 will use `--spec <original> --spec <sniff-spec> --name <api>` to merge both
- **Primary mode**: Phase 2 will use `--spec <sniff-spec>` directly

#### Step 5: Write sniff discovery report

Write a structured sniff provenance report to `$DISCOVERY_DIR/sniff-report.md`. This report preserves the discovery evidence so a future maintainer can reproduce or extend the sniff.

The report must contain these sections:

1. **Pages Visited** — List every URL browsed during the sniff, in order. Include the page purpose (e.g., "Homepage", "Search results for 'stripe'", "Team detail page").

2. **Sniff Configuration** — Backend used (browser-use, agent-browser, or manual HAR), pacing settings (initial delay, final effective rate), and proxy pattern detection result (proxy-envelope detected / not detected, with the proxy URL if applicable).

3. **Endpoints Discovered** — A markdown table with columns: Method, Path, Status Code, Content-Type. One row per unique endpoint observed.

4. **Coverage Analysis** — What resource types were exercised (e.g., "collections, workspaces, teams, categories") and what was likely missed. Compare against the Phase 1 research brief to identify gaps (e.g., "Brief mentions 'flows' but no flow endpoints were discovered during sniff").

5. **Response Samples** — For each unique response shape (keyed by status code + content-type category), include a truncated sample:
   - JSON/text responses: first 2KB or 100 lines, whichever is smaller
   - Binary responses (images, protobuf, etc.): skip content, include a metadata note: `Binary response: <content-type>, <size> bytes`
   - Aim for one sample per unique shape, not one per endpoint

6. **Rate Limiting Events** — Any 429 responses encountered, delays applied, and effective sniff rate achieved (e.g., "Sniffed 7 endpoints at ~1.5 req/s effective rate, one 429 at request #4").

7. **Authentication Context** — Whether the sniff used an authenticated session. If yes: transfer method used (auto-connect / profile / headed login / HAR), which endpoints were only reachable with auth (e.g., "order history, saved addresses, rewards required login"), and confirmation that session state was excluded from manuscript archiving. If no: "No authenticated session used."

### If user declines sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

## Phase 1.8: Crowd Sniff Gate

After Phase 1.7 (Sniff Gate), evaluate whether mining community signals (npm SDKs and GitHub code search) would improve the spec. Skip this gate entirely if the user already passed `--spec` (spec source is already resolved and appears complete).

**Time budget:** The crowd sniff gate should complete within 5 minutes. If `printing-press crowd-sniff` fails or times out, fall back immediately:
- If a spec already exists: "Crowd sniff failed — proceeding with existing spec."
- If no spec exists: "Crowd sniff failed — falling back to --docs generation."

### When to offer crowd sniff

| Spec found? | Research shows gaps? | Action |
|-------------|---------------------|--------|
| Yes | Yes — competitors or community projects reference more endpoints | **Offer crowd sniff as enrichment** |
| Yes | No — spec appears complete | Skip silently |
| No | Community SDKs exist on npm | **Offer crowd sniff as primary discovery** |
| No | No SDKs or code found | Skip — fall back to `--docs` |

### Crowd sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more. Want me to search npm packages and GitHub code for `<api>` to discover additional endpoints? This typically takes 2-4 minutes."
>
> Options:
> 1. **Yes — crowd sniff and merge** (search npm SDKs and GitHub code, merge discovered endpoints with the existing spec)
> 2. **No — use existing spec** (proceed with what we have)

### Crowd sniff as primary (no spec found)

Present to the user via `AskUserQuestion`:

> "No OpenAPI spec found for `<API>`. Want me to search npm packages and GitHub code to discover the API from community usage? This typically takes 2-4 minutes."
>
> Options:
> 1. **Yes — crowd sniff the community** (search npm SDKs and GitHub code, generate a spec from discovered endpoints)
> 2. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 3. **No — I'll provide a spec or HAR** (user will supply input manually)

### If user approves crowd sniff

Ensure the discovery directory exists:

```bash
mkdir -p "$DISCOVERY_DIR"
```

Run the crowd-sniff command and capture both the spec and JSON provenance:

```bash
printing-press crowd-sniff --api <api> --output "$RESEARCH_DIR/<api>-crowd-spec.yaml" --json > "$DISCOVERY_DIR/crowd-sniff-provenance.json"
```

If the API has a known base URL from Phase 1 research, pass it:

```bash
printing-press crowd-sniff --api <api> --base-url <known-base-url> --output "$RESEARCH_DIR/<api>-crowd-spec.yaml" --json > "$DISCOVERY_DIR/crowd-sniff-provenance.json"
```

Report the results: "Crowd sniff discovered **N endpoints** across **M resources** (X from npm, Y from GitHub)."

**Feed into Phase 2:**
- **Enrichment mode**: Phase 2 will use `--spec <original> --spec <crowd-spec> --name <api>` to merge both
- **Primary mode**: Phase 2 will use `--spec <crowd-spec>` directly

#### Write crowd-sniff discovery report

Write a structured crowd-sniff provenance report to `$DISCOVERY_DIR/crowd-sniff-report.md`. This report preserves the discovery evidence so a future maintainer can see what community sources informed the spec.

The report must contain these sections:

1. **npm Packages Analyzed** — List each SDK package examined: name, version, download count, recency. Note which packages yielded endpoints and which were empty/irrelevant.

2. **GitHub Repos Searched** — The search queries used, repos matched, and freshness of each repo. Note the GitHub token status (authenticated with broader results, or unauthenticated with rate-limited results).

3. **Endpoints Discovered** — A markdown table with columns: Method, Path, Source Tier (official-sdk / community-sdk / code-search), Source Count (seen in N independent sources). Sorted by source tier then frequency.

4. **Base URL Resolution** — Candidates discovered and which was selected, with rationale (e.g., "Found in 3 npm packages: https://api.notion.com").

5. **Auth Patterns Detected** — Authentication patterns found in SDK code (API key headers, bearer tokens, OAuth flows). Include the header name or env variable convention when visible.

6. **Coverage Summary** — Total endpoints found, breakdown by source tier, and any gaps compared to the Phase 1 research brief (e.g., "Brief mentions webhooks but no webhook endpoints found in community code").

### If user declines crowd sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

## Phase 1.5: Ecosystem Absorb Gate

THIS IS A MANDATORY STOP GATE. Do not generate until this is complete and approved.

**Pre-check:** If no spec or HAR file has been resolved by this point and Phase 1.7 (Sniff Gate) was not evaluated, STOP. Go back and run the sniff gate decision matrix. The absorb manifest depends on knowing the API surface, which requires a spec.

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

### Step 1.5a.5: Read MCP source code (if found)

If step 1.5a discovered MCP server repos with public source code on GitHub, read the actual source to extract ground-truth API usage — not just README feature descriptions.

**Time budget:** Max 3 minutes total. If extraction is unproductive, fall back to README-only research.

**For the top 1-2 MCP repos found:**

1. **Identify the main source file.** WebFetch the repo root to find the entry point — typically `src/index.ts`, `server.ts`, `server.py`, `main.go`, or a `tools/` directory. MCP servers are usually small (one main file + tool definitions).

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

6. **Self-brainstorm** — Answer these questions using the research context gathered so far. Do NOT ask the user — answer them yourself from the research brief, absorb manifest, and ecosystem findings:
   - Based on the research brief's top workflows and user profiles, what workflows does the typical power user of this API do that aren't covered in the absorbed features?
   - Based on competitor repo issues, community pain points, and ecosystem gaps found in Phase 1/1.5, what are the most annoying limitations that a CLI with SQLite could fix?
   - Based on the NOI and domain archetype, what single "killer feature" would make a power user install this CLI over any alternative?
   - (Only when `USER_BRIEFING_CONTEXT` is non-empty) Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't already cover?

#### Generate and Score Candidates

Generate 3-8 novel feature ideas (across all 6 categories). For each, score on 4 dimensions:

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

The manifest now includes compound use cases (Step 1.5c) and auto-suggested + auto-brainstormed features (Step 1.5c.5) in the transcendence table.

### Phase Gate 1.5

**STOP.** Present the absorb manifest to the user in two parts: a prose showcase, then a question.

**Part 1: Prose showcase (print before the AskUserQuestion)**

Print as regular text output:

> ## Absorb Summary
>
> I cataloged **[N] features** across [X] tools ([tool names]). Our CLI will match every one of them with offline search, --json output, and agent-native flags.
>
> ## Novel Features (my ideas, not found in any existing tool)
>
> Beyond absorbing what exists, I came up with [M] features that no existing tool has. Here are the top 3:
>
> 1. **[Feature name]** ([score]/10) — [one-line description]. Evidence: [what research finding inspired this].
> 2. **[Feature name]** ([score]/10) — [one-line description]. Evidence: [source].
> 3. **[Feature name]** ([score]/10) — [one-line description]. Evidence: [source].
>
> Plus [M-3] more in the full manifest.
>
> Total: [N+M] features, [Z]% more than [best existing tool name] ([best tool feature count]).

If fewer than 3 novel features scored >= 5/10, show all qualifying features instead of "top 3." If 0 qualified, note: "No novel features scored high enough to recommend. The absorbed features cover the landscape well."

**Part 2: AskUserQuestion**

> "Ready to generate with the full [N+M]-feature manifest? Or do you have ideas to add?"

Options:
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

## Phase 1.9: API Reachability Gate

**MANDATORY. Do NOT skip this phase. Do NOT proceed to Phase 2 without running this check.**

Before spending tokens on generation, verify the API actually responds to programmatic requests. One real HTTP call. If it fails, STOP.

### The Check

Pick the simplest GET endpoint from the resolved spec (no required params, no auth if possible). If no such endpoint exists, use the spec's base URL. Run one HTTP request:

```bash
curl -s -o /dev/null -w "%{http_code}" -m 10 "<base_url>/<simplest_get_path>" 2>/dev/null
```

Or use `WebFetch` if curl is unavailable. The goal is one real response code.

### Decision Matrix

| Result | Sniff gate failed? | Research found 403 issues? | Action |
|--------|-------------------|---------------------------|--------|
| 2xx/3xx | Any | Any | **PASS** - proceed to Phase 2 |
| 401 (no key provided) | No | No | **PASS** - expected when API needs auth and user declined key gate |
| 403 with HTML/bot detection | Any | Any | **HARD STOP** |
| 403 | Yes (bot detection) | Any | **HARD STOP** |
| 403 | No | Yes (issues found) | **HARD STOP** |
| 403 | No | No | **WARN** - ask user |
| Timeout/DNS/connection refused | Any | Any | **WARN** - ask user |

### On HARD STOP

Present via `AskUserQuestion`:

> "WARNING: `<API>` appears to block programmatic access. [what failed: e.g., 'HTTP 403 with HTML error page', 'sniff gate failed with bot detection', 'reteps/redfin has 6+ issues about 403 errors']. Building a CLI against an unreachable API wastes time and tokens."
>
> 1. **Try anyway** - proceed knowing the CLI may not work against the live API
> 2. **Pick a different API** - start over
> 3. **Done** - stop here

### On WARN

Present via `AskUserQuestion`:

> "The API returned [error]. This might be temporary, or it might mean programmatic access is blocked. Want to proceed?"
>
> 1. **Yes - proceed** - generate the CLI anyway
> 2. **No - stop** - pick a different API or provide a spec manually

### On PASS

Proceed silently to Phase 2.

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
  --spec-source sniffed \
  --force --lenient --validate
# If proxy pattern was detected during sniff, add:
#   --client-pattern proxy-envelope
```

Sniff-only (no original spec, sniff was the primary source):

```bash
printing-press generate \
  --spec "$RESEARCH_DIR/<api>-sniff-spec.yaml" \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --spec-source sniffed \
  --force --lenient --validate
# If proxy pattern was detected during sniff, add:
#   --client-pattern proxy-envelope
```

Crowd-sniff-enriched (original spec + crowd-discovered spec):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --lenient --validate
```

Crowd-sniff-only (no original spec, crowd sniff was the primary source):

```bash
printing-press generate \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --output "$PRESS_LIBRARY/<api>-pp-cli" \
  --force --lenient --validate
```

Both sniff + crowd-sniff (merged with original):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-sniff-spec.yaml" \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
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

**REQUIRED: Rewrite the CLI description.** The generator copies the spec's `description` field
as the CLI's `Short` help text. Spec descriptions describe the *API* ("Payment processing API")
but CLI help should describe what the *CLI does* ("Manage payments, subscriptions, and invoices
via the Stripe API"). Open `$PRESS_LIBRARY/<api>-pp-cli/internal/cli/root.go`, find the
`Short:` field on the root cobra command, and rewrite it as a concise, user-facing description
of the CLI's purpose. Use the product thesis from the Phase 1 brief to inform the rewrite.

Then:
- note skipped complex body fields
- fix only blocking generation failures here
- do not start broad polish work yet

If generation fails:
- fix the specific blocker
- retry at most 2 times
- prefer generator fixes over manual generated-code surgery when the failure is systemic

## Phase 3: Build The GOAT

<!-- CODEX_PHASE3_START -->
### Codex Delegation (Phase 3)

When `CODEX_MODE` is true, delegate code-writing tasks to Codex CLI. Claude still decides WHAT to build and in what order. Codex does the hands — writing Go functions.

**Delegation loop for each priority task:**

1. **Decompose** the current priority level into discrete tasks (one per command/feature from the absorb manifest).

2. **For each task**, follow this delegation cycle:

   a. **Read context** — Read the relevant source files from the generated CLI to extract actual code for the prompt. Use `head -50`, `grep -A 20`, or `cat` to get real code, not descriptions.

   b. **Snapshot** — Create a clean restore point before Codex writes anything:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && git add -A && git stash push -m "pre-codex-task"
   ```

   c. **Assemble prompt** — Build a CODEX_PROMPT using the appropriate task type template (see below).

   d. **Delegate** — Pipe to Codex:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -c 'model_reasoning_effort="medium"' \
     -m "gpt-5.4" \
     -
   ```

   e. **Validate** — Check the result:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && go build ./... && go vet ./...
   ```
   Also verify `git diff --stat` shows a non-empty diff.

   f. **On success** — Discard the restore point and reset the failure counter:
   ```bash
   git stash drop 2>/dev/null
   ```
   Set `CODEX_CONSECUTIVE_FAILURES=0`.

   g. **On failure** (build fails, vet fails, empty diff, or Codex error) — Revert and fall back:
   ```bash
   git checkout -- . && git stash pop 2>/dev/null
   ```
   Increment `CODEX_CONSECUTIVE_FAILURES`. Claude implements this task directly (standard non-codex path).

   h. **Circuit breaker** — If `CODEX_CONSECUTIVE_FAILURES` reaches 3:
   ```bash
   echo "Codex disabled after 3 consecutive failures — completing in standard mode."
   CODEX_MODE=false
   ```
   All remaining tasks in Phase 3 (and Phase 4) use Claude directly.

3. **After each priority level**, run the same quality checks as non-codex mode (e.g., Priority 1 Review Gate).

**Task type prompt templates:**

All templates follow this structure. Paste ACTUAL CODE in the CURRENT CODE section — never descriptions of code.

**Store table task:**
```
TASK: Add <entity> table with Upsert and Search methods to the SQLite store.

FILES TO MODIFY:
- internal/store/store.go

CURRENT CODE (existing table pattern):
$(grep -A 30 "CREATE TABLE IF NOT EXISTS" internal/store/store.go | head -40)

EXPECTED CHANGE:
Create a new table for <entity> with columns: <fields from spec>.
Add Upsert<Entity>(ctx, item) and Search<Entity>(ctx, query) methods following the existing pattern.
Add FTS5 virtual table if entity has searchable text fields.

CONVENTIONS:
- Package: store
- Use the same CreateTable/Upsert/Search pattern as existing tables
- Error handling: return fmt.Errorf("upsert <entity>: %w", err)
- All table names are snake_case

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/store/store.go
- Keep changes under 200 lines
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

**Workflow command task:**
```
TASK: Create the <command> subcommand for <api>-pp-cli.

FILES TO MODIFY:
- internal/cli/<command>.go (create new)

CURRENT CODE (cobra command pattern from an existing command):
$(cat internal/cli/<existing-command>.go | head -60)

CURRENT CODE (root command registration):
$(grep -A 5 "AddCommand" internal/cli/root.go)

EXPECTED CHANGE:
Create a <command> command that:
<plain English description of what the command does, from the absorb manifest>

Must support: --json, --select, --compact, --limit, --dry-run (for mutations).
Must have realistic --help examples with domain-specific values.

CONVENTIONS:
- Package: cli
- Use cobra.Command pattern matching existing commands
- Error handling: return fmt.Errorf with context
- Progress output: fmt.Fprintf(os.Stderr, ...)
- Register with rootCmd.AddCommand in root.go

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/cli/<command>.go and internal/cli/root.go
- Keep changes under 200 lines per file
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

**Transcendence command task:**
```
TASK: Create the <command> transcendence command — a compound query across local SQLite data.

FILES TO MODIFY:
- internal/cli/<command>.go (create new)

CURRENT CODE (available store methods):
$(grep -E "^func \(db \*DB\)" internal/store/store.go | head -20)

CURRENT CODE (cobra pattern):
$(cat internal/cli/<existing-command>.go | head -40)

EXPECTED CHANGE:
Create a <command> command that:
<plain English description — what entities it joins, what insight it produces>

This command ONLY works because all data is in local SQLite.
Must support: --json, --select, --compact, --limit.

CONVENTIONS:
- Package: cli
- Query across tables using db methods, not raw SQL in CLI layer
- Format output as a table by default, JSON with --json

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/cli/<command>.go and internal/cli/root.go
- Keep changes under 200 lines per file
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

When `CODEX_MODE` is false, skip this entire section and proceed with the standard build flow below.

<!-- CODEX_PHASE3_END -->

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

<!-- CODEX_PHASE4_START -->
### Codex Delegation (Phase 4 Fixes)

When `CODEX_MODE` is true, delegate each bug fix to Codex. The shipcheck tools themselves (dogfood, verify, scorecard) always run on Claude — they are Go binary executions. Only the CODE FIXES are delegated.

**For each bug identified from dogfood/verify/scorecard output:**

1. **Read the finding** — identify the exact file, the issue, and what needs to change.

2. **Read the code** — extract the actual broken code for context:
   ```bash
   grep -n -A 10 "<broken pattern or function name>" "$PRESS_LIBRARY/<api>-pp-cli/<file>"
   ```

3. **Snapshot** before Codex writes:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && git add -A && git stash push -m "pre-codex-fix"
   ```

4. **Assemble and delegate** using the fix prompt template:
   ```
   TASK: Fix <finding summary from dogfood/verify>.

   FILES TO MODIFY:
   - <exact file path>

   CURRENT CODE (the broken section):
   <actual code from the file — use grep -A or head/tail, not descriptions>

   BUG:
   <the dogfood/verify finding, verbatim>

   EXPECTED FIX:
   <plain English description of the correct behavior>

   CONSTRAINTS:
   - Do NOT run git commit, git push, or git add
   - Do NOT modify files outside the listed path
   - Keep changes under 50 lines
   - Run: go build ./... && go vet ./...

   VERIFY: After making changes, run:
     cd . && go build ./... && go vet ./...
   ```

   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -c 'model_reasoning_effort="medium"' \
     -m "gpt-5.4" \
     -
   ```

5. **Validate** — same as Phase 3: `go build`, `go vet`, non-empty diff.

6. **On success** — `git stash drop`, reset `CODEX_CONSECUTIVE_FAILURES=0`.

7. **On failure** — `git checkout -- . && git stash pop`, increment `CODEX_CONSECUTIVE_FAILURES`, Claude fixes this bug directly.

8. **Circuit breaker** — shares the same counter from Phase 3. If already disabled, all fixes use Claude.

When `CODEX_MODE` is false, fix bugs directly as in standard mode.

<!-- CODEX_PHASE4_END -->

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

## Phase 5.5: Archive Manuscripts

Archive the run's research, proofs, and discovery artifacts to `$PRESS_MANUSCRIPTS/`
**unconditionally** after shipcheck completes (or after live smoke if it ran). This
happens regardless of the shipcheck verdict — even a `hold` run produces research
and proofs that future runs should be able to reuse.

Archiving and publishing are separate concerns. Archiving preserves research for
future `/printing-press` runs on the same API. Publishing ships the CLI to the
library repo. A run that isn't ready to publish still produces valuable research.

```bash
mkdir -p "$PRESS_MANUSCRIPTS/<api>/$RUN_ID"
cp -r "$RESEARCH_DIR" "$PRESS_MANUSCRIPTS/<api>/$RUN_ID/research" 2>/dev/null || true
cp -r "$PROOFS_DIR" "$PRESS_MANUSCRIPTS/<api>/$RUN_ID/proofs" 2>/dev/null || true

# Archive discovery artifacts (sniff captures, URL lists, sniff report).
# Remove session state before archiving — contains authentication cookies/tokens.
rm -f "$DISCOVERY_DIR/session-state.json"

# Strip response bodies from HAR before archiving to control size.
if [ -d "$DISCOVERY_DIR" ]; then
  for har in "$DISCOVERY_DIR"/sniff-capture.har "$DISCOVERY_DIR"/sniff-capture.json; do
    if [ -f "$har" ] && command -v jq >/dev/null 2>&1; then
      jq 'del(.log.entries[].response.content.text)' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har" || rm -f "${har}.stripped"
    fi
  done
  cp -r "$DISCOVERY_DIR" "$PRESS_MANUSCRIPTS/<api>/$RUN_ID/discovery" 2>/dev/null || true
fi
```

**MANDATORY: After archiving, you MUST proceed to Phase 6 (Publish) below. Do not print a summary and stop. Do not treat archiving as the end of the run. The run ends when the user has been asked about publishing (or the verdict is `hold`).**

## Phase 6: Publish

**This phase is NOT optional.** Every run with a `ship` or `ship-with-gaps` verdict MUST reach this point. Do not skip it.

After archiving, offer to publish the CLI to the library repo.

### Gate

Use the most recent shipcheck verdict:
- if Phase 5 reran shipcheck after a live-smoke fix, use that rerun verdict
- otherwise use the Phase 4 verdict

Skip this phase entirely if the final shipcheck verdict is `hold`. Only proceed for `ship` or `ship-with-gaps`.

### Check for existing PR

Run a lightweight check for your own open publish PR. The `--author @me` filter avoids matching someone else's PR for the same CLI name.

```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/<cli-name>" --state open --author @me --json number,url --jq '.[0]' 2>/dev/null
```

If this fails (gh not authenticated, network error, etc.), continue without PR context — the publish skill will handle auth in its own Step 1.

### Offer

Present via `AskUserQuestion`:

**If an existing open PR was found:**

> "<cli-name> passed shipcheck. There's an open publish PR (#N). Want to update it with this version?"
>
> 1. **Yes — update PR #N** (re-validate, re-package, and push to the existing PR)
> 2. **No — I'm done**

**If no existing PR:**

> "<cli-name> passed shipcheck ([score]/100, verify [pass-rate]%). What do you want to do?"
>
> 1. **Publish now** (validate, package, and open a PR)
> 2. **Polish first** (run `/printing-press-polish` to fix verify failures, dead code, and README before publishing)
> 3. **Run retro** (analyze the session to find improvements for the generator)
> 4. **Done for now**

If the verdict was `ship-with-gaps`, prepend: "Note: shipcheck found minor gaps (see the shipcheck report above)." and recommend the polish option.

### If "Publish now"

Invoke `/printing-press publish <cli-name>`. The publish skill handles everything from there.

### If "Polish first"

Invoke `/printing-press-polish <cli-name>`. The polish skill runs diagnostics, fixes issues, reports the delta, and offers its own publish at the end.

### If "Run retro"

Invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

### If "Done for now"

End normally. The CLI is in `$PRESS_LIBRARY/<api>-pp-cli` and the user can run `/printing-press publish` or `/printing-press-polish` later.

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
