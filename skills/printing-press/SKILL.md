---
name: printing-press
description: Generate a ship-ready CLI for an API with a lean research -> generate -> build -> shipcheck loop.
version: 2.0.0
min-binary-version: "0.3.0"
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

- **Do not ship a CLI that hasn't been behaviorally tested against real targets.** `go build` and `verify` pass-rate are structural signals, not correctness signals. Phase 5's mechanical test matrix runs every subcommand + `--json` + error paths; if that matrix was not executed, the CLI is not shippable. Quick Check is the floor; Full Dogfood is required when the user asks for thoroughness.
- **Bugs found during dogfood are fix-before-ship, not "file for v0.2".** If a 1-3 file edit resolves it, do it now. `ship-with-gaps` is deprecated as a default verdict (see Phase 4). Context is freshest in-session; a v0.2 backlog that may never be revisited ships known-broken CLIs.
- **Features approved in Phase 1.5 are shipping scope.** Do not downgrade a shipping-scope feature to a stub mid-build. If implementation becomes infeasible, return to Phase 1.5 with a revised manifest and get explicit re-approval.
- **Do not quote human-time estimates for sub-tasks** ("~15-30 min", "~1 hour", "quick fix") in `AskUserQuestion` options, phase descriptions, or reference docs. The agent does the work, not the user; agent-fabricated estimates are notoriously bad and train users to distrust the prompt. Describe scope instead (lines of code, files touched, relative size). The carve-outs are wall-clock estimates for genuinely time-bound things: the whole-CLI run (set the user's expectation up front — most CLIs take 30+ minutes), tool installs (`go install` takes ~10 seconds), and printing-press subcommands that do network-bound work (crowd-sniff scans npm + GitHub, ~5-10 minutes). Anything bounded by agent reasoning time is not time-bound — describe scope.
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

During Phase 5.6 (archiving) and before publishing, read and apply
[references/secret-protection.md](references/secret-protection.md) for:
- Exact-value scanning and auto-redaction of artifacts
- HAR auth stripping (headers, query strings, cookies)
- API key handling rules during the run
- Session state cleanup ordering

## Preflight

**This section MUST run before any user-facing prompt — including the Orientation and Briefing flow below.** A missing binary or available upgrade is information the user needs *before* they commit to an API. Do not invoke `AskUserQuestion`, print the orientation prose, or otherwise engage the user until preflight has completed and any signals from `references/setup-checks.md` have been handled.

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.3.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
# The lefthook build hook keeps ./printing-press current after every commit/pull,
# so it's always newer than the go-install version.
if [ -x "$_scope_dir/printing-press" ] && [ -d "$_scope_dir/cmd/printing-press" ]; then
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/printing-press"
elif ! command -v printing-press >/dev/null 2>&1; then
  # Augment PATH if the binary is in ~/go/bin but not on the user's interactive PATH.
  if [ -x "$HOME/go/bin/printing-press" ]; then
    export PATH="$HOME/go/bin:$PATH"
  else
    # Refuse: the printing-press binary is required and we will not auto-install
    # it. The README's two-step install (binary + plugin) is the source of truth;
    # silent auto-install hides failure modes (GOPRIVATE auth, network, wrong
    # GOPATH) inside an opaque skill invocation.
    echo ""
    echo "[setup-error] printing-press binary not found."
    echo ""
    if command -v go >/dev/null 2>&1; then
      echo "Install it in your terminal:"
      echo "  GOPRIVATE=github.com/mvanhorn/* go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest"
      echo ""
      echo "(GOPRIVATE is required while the repo is private. The plain command works after the public launch.)"
    else
      echo "Go 1.22+ is also not installed. Install Go from https://go.dev/dl/, then:"
      echo "  GOPRIVATE=github.com/mvanhorn/* go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest"
    fi
    echo ""
    echo "Verify with: printing-press --version"
    echo "Then re-run /printing-press."
    return 1 2>/dev/null || exit 1
  fi
fi

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

# --- Latest-version advisory (throttled, fail-open) ---
# Once per 24h, check whether a newer printing-press release exists and print a
# one-line notice if so. Uses `go list` so it works for private modules via
# GOPRIVATE and public modules via the proxy. Runs in every context — devs
# ahead of latest stay silent (comparison handles it), devs behind latest get
# the same nudge anyone else does.
PRESS_VERCHECK_FILE="$PRESS_HOME/.version-check"
PRESS_VERCHECK_TTL=86400
_now_ts=$(date +%s)
_should_check=true
if [ -f "$PRESS_VERCHECK_FILE" ] && [ -z "$PRESS_VERCHECK_FORCE" ]; then
  _last_ts=$(awk -F= '/^last_check=/{print $2}' "$PRESS_VERCHECK_FILE" 2>/dev/null)
  if [ -n "$_last_ts" ] && [ "$((_now_ts - _last_ts))" -lt "$PRESS_VERCHECK_TTL" ]; then
    _should_check=false
  fi
fi

if [ "$_should_check" = "true" ] && command -v go >/dev/null 2>&1; then
  _installed=$(printing-press version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')
  _latest=$(GOPRIVATE=github.com/mvanhorn/* go list -m -versions github.com/mvanhorn/cli-printing-press/v3 2>/dev/null | awk '{print $NF}' | sed 's/^v//')

  if [ -n "$_installed" ] && [ -n "$_latest" ] && [ "$_installed" != "$_latest" ]; then
    # sort -V is not fully semver-aware: it ranks "3.0.0-rc1" above "3.0.0" instead of below.
    # Acceptable today (we don't ship pre-release tags); revisit if we ever do.
    _newer=$(printf "v%s\nv%s\n" "$_installed" "$_latest" | sort -V | tail -1 | sed 's/^v//')
    if [ "$_newer" = "$_latest" ]; then
      # Marker for the skill prose below to detect and offer an interactive upgrade.
      # The skill reads PRESS_UPGRADE_AVAILABLE / PRESS_UPGRADE_INSTALLED from this output.
      echo ""
      echo "[upgrade-available] printing-press v$_latest is available (you have v$_installed)"
      echo "PRESS_UPGRADE_AVAILABLE=$_latest"
      echo "PRESS_UPGRADE_INSTALLED=$_installed"
      echo ""
    fi
  fi

  printf "last_check=%s\nlatest=%s\n" "$_now_ts" "${_latest:-unknown}" > "$PRESS_VERCHECK_FILE" 2>/dev/null || true
fi

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
    # Model and reasoning effort inherit from ~/.codex/config.toml. Do not pin -m / -c here.
    CODEX_MODEL=$(grep -E '^model[[:space:]]*=' ~/.codex/config.toml 2>/dev/null | head -1 | sed -E 's/^model[[:space:]]*=[[:space:]]*"?([^"]+)"?.*$/\1/')
    [ -z "$CODEX_MODEL" ] && CODEX_MODEL="codex default"
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

**MANDATORY: Read and apply [references/setup-checks.md](references/setup-checks.md) immediately after the setup contract bash block runs, before any other action.** It handles three signals the contract emits to stdout: `[setup-error]` (refuse to run, surface the install instructions), `[upgrade-available]` (interactive `AskUserQuestion` prompt + optional upgrade), and the min-binary-version compatibility check. Skipping the reference will cause the skill to proceed with a missing or out-of-date binary. Do not skip.

Only after preflight completes successfully (no `[setup-error]`; any `[upgrade-available]` was offered to the user) should you proceed to the Orientation & Briefing section below.

## Orientation & Briefing

After preflight has completed, check whether the user provided arguments. Handle two cases:

### No Arguments: Orientation

If the user typed `/printing-press` with no arguments (no API name, no `--spec`, no `--har`, no URL), print an orientation and ask what they'd like to build:

> The Printing Press generates a fully functional CLI for any API. You give it an API name, a spec file, or a URL. It researches the landscape, catalogs every feature that exists in any competing tool, invents novel features of its own, then generates a Go CLI that matches and beats everything out there — with offline search, agent-native output, and a local SQLite data layer.
>
> By the end, you'll have a working CLI in `~/printing-press/library/` that you can use for yourself, ship on your own, or apply to add to the printing-press library.
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
> **What you will have at the end:** A fully functional CLI at `~/printing-press/library/<api>` that you can use yourself, ship on your own, or apply to add to the printing-press library.
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

**Do not add additional options** — auth is already handled by Phase 0.5 (API Key Gate) and Phase 1.6 (Pre-Browser-Sniff Auth Intelligence) downstream. A user who wants to volunteer auth context can do so via option 2's free-text response. The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Let's go (recommended)"**, proceed to the Multi-Source Priority Gate (or, for single-source runs, directly to Phase 0).

If the user picks **"I have context to share"**, capture their free-text response as `USER_BRIEFING_CONTEXT`. The response may include:

- **Vision / specific features** — captured as-is. This context will be:
  - Added to the Phase 1 Research Brief under a `## User Vision` section
  - Used as a 4th self-brainstorm question in Phase 1.5c.5: "Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't cover?"
  - Referenced at the Phase Gate 1.5 absorb gate: "You mentioned [summary] at the start. Want to add more, or does the manifest already cover it?"
- **Auth context** — if the user mentions an API key, env var, or logged-in browser session, set the corresponding `AUTH_CONTEXT` fields so the API Key Gate (Phase 0.5) and Pre-Browser-Sniff Auth Intelligence (Phase 1.6) do not re-ask.

### Multi-Source Priority Gate

After the briefing question resolves, inspect the user's original argument AND any `USER_BRIEFING_CONTEXT` they provided. If together they name **two or more distinct services, sites, or APIs** (e.g., "Google Flights and Kayak", "Notion + Linear combo CLI", "flightgoat: Google Flights, Kayak.com/direct, and FlightAware"), this is a combo CLI and priority ordering MUST be confirmed before Phase 1 research.

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

**Phase 1 MUST consult this file.** When selecting a spec source, the primary source wins even if it has no spec and a later source has a clean OpenAPI. When the primary has no official spec, flag that openly in the brief under `## Source Priority` (see template below) and route to the browser-sniff/docs path for the primary — do not promote a secondary source just because its spec is cleaner.

**Economics check.** If the confirmed primary source is free (no API key required) AND the generator's default path would make the primary CLI commands require a paid key (because the auth applies broadly or because a paid secondary source is bleeding into the primary path), surface the tradeoff explicitly before generating:

> "The primary source (**<Source A>**) is free, but the default path would require a **<paid key>** for the headline commands because <reason>. Options: (1) keep primary free, gate only the secondary commands on the paid key; (2) require the paid key for everything; (3) drop the paid source."

Default to option 1 unless the user overrides. Record the decision in `source-priority.json` under `auth_scoping`.

**Single-source runs:** If only one service is named, skip this gate entirely — no ordering to confirm.

---

## Run Initialization

After you know `<api>` (from the Orientation & Briefing flow above; preflight already ran at the top), initialize the run-scoped artifact paths:

```bash
RUN_ID="$(date +%Y%m%d-%H%M%S)"
API_RUN_DIR="$PRESS_RUNSTATE/runs/$RUN_ID"
RESEARCH_DIR="$API_RUN_DIR/research"
PROOFS_DIR="$API_RUN_DIR/proofs"
PIPELINE_DIR="$API_RUN_DIR/pipeline"
DISCOVERY_DIR="$API_RUN_DIR/discovery"
CLI_WORK_DIR="$API_RUN_DIR/working/<api>-pp-cli"
STAMP="$(date +%Y-%m-%d-%H%M%S)"

mkdir -p "$RESEARCH_DIR" "$PROOFS_DIR" "$PIPELINE_DIR" "$CLI_WORK_DIR"
STATE_FILE="$API_RUN_DIR/state.json"
```

Maintain a lightweight state file at `$STATE_FILE` so `/printing-press-score` can rediscover the current run. It should always contain:

```json
{
  "api_name": "<api>",
  "working_dir": "$CLI_WORK_DIR",
  "output_dir": "$CLI_WORK_DIR",
  "spec_path": "<absolute spec path if known>"
}
```

Do not create a `go.work` file in `$CLI_WORK_DIR`. Generated modules must build and test as standalone modules; a mismatched workspace `go` directive can break Go 1.25+ toolchains and lefthook checks. Editor/gopls workspace noise is cosmetic and must not be traded for broken `go build` or `go test`.

There are exactly three durable writable locations. Every generated artifact this
skill preserves goes to one of them:

- **`$PRESS_RUNSTATE/`** — mutable working state for the current run (research, proofs, pipeline artifacts, plans, intermediate docs)
- **`$PRESS_LIBRARY/`** — published CLIs (`<api-slug>/` subdirectories)
- **`$PRESS_MANUSCRIPTS/`** — archived run evidence (research, proofs, discovery)

Short-lived command captures may use `/tmp/printing-press/` with unique `mktemp`
paths and must be deleted after use.

Examples of the current naming/layout:
- `~/printing-press/library/notion/` — published CLI directory (keyed by API slug)
- `notion-pp-cli` — the binary name inside the directory
- `/printing-press emboss notion` — emboss accepts both slug and CLI name
- `discord-pp-cli/internal/store/store.go` — internal source paths still use CLI name
- `linear-pp-cli stale --days 30 --team ENG` — binary invocations use CLI name
- `github.com/mvanhorn/discord-pp-cli` — Go module paths use CLI name

## Outputs

Every run writes up to 5 concise artifacts under the current managed run and archives them to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/`:

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
     2. **label:** `"The <SiteName> website itself"` — **description:** `"Build from the website itself — I may open or attach to Chrome during generation to capture site traffic, then generate a lightweight CLI from replayable HTTP/HTML surfaces"`

   The user can also pick the automatic "Other" option to describe what they're after in free text.

   **Routing after disambiguation:**
   - "<SiteName>'s official API" → use `<api>` as the argument, proceed with normal discovery (Phase 1 research, then Phase 1.7 browser-sniff gate evaluates independently as usual)
   - "The <SiteName> website itself" → use `<api>` as the argument, set `BROWSER_SNIFF_TARGET_URL=<url>`. Proceed to Phase 1 research. When Phase 1.7 is reached, skip the browser-sniff gate decision and go directly to "If user approves browser-sniff" (the user already approved temporary browser discovery in Phase 0 — do not re-ask). Use `BROWSER_SNIFF_TARGET_URL` as the starting URL for browser capture. The printed CLI must still use a replayable runtime surface; do not ship a resident browser transport.
   - "Other" → read the user's free-form response and adapt

   **End of URL detection.** The remaining spec resolution rules apply when the argument is NOT a URL:

   - If the user passed `--har <path>`, this is a HAR-first run. Run `printing-press browser-sniff --har <path> --name <api> --output "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"` to generate a spec and traffic analysis from captured traffic. Use the generated spec as the primary spec source for the rest of the pipeline. Skip the browser-sniff gate in Phase 1.7 (browser-sniff already ran).
   - If the user passed `--spec`, use it directly (existing behavior).
   - Otherwise, proceed with normal discovery (catalog, KnownSpecs, apis-guru, web search).
2. Check for prior research in:
   - `$PRESS_MANUSCRIPTS/<api-slug>/*/research/*`
3. Reuse good prior work instead of redoing it.
4. **Library Check** — Check if a CLI for this API already exists in the library or is actively being built, and present the user with context and options.

   First, check lock status to detect active builds:

   ```bash
   LOCK_STATUS=$(printing-press lock status --cli <api>-pp-cli --json 2>/dev/null)
   LOCK_HELD=$(echo "$LOCK_STATUS" | grep -o '"held"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_STALE=$(echo "$LOCK_STATUS" | grep -o '"stale"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_PHASE=$(echo "$LOCK_STATUS" | grep -o '"phase"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"phase"[[:space:]]*:[[:space:]]*"//;s/"//')
   LOCK_AGE=$(echo "$LOCK_STATUS" | grep -o '"age_seconds"[[:space:]]*:[[:space:]]*[0-9]*' | head -1 | sed 's/.*: *//')
   ```

   Then check the library directory:

   ```bash
   CLI_DIR="$PRESS_LIBRARY/<api>"
   HAS_LIBRARY=false
   HAS_GOMOD=false
   if [ -d "$CLI_DIR" ]; then
     HAS_LIBRARY=true
     if [ -f "$CLI_DIR/go.mod" ]; then
       HAS_GOMOD=true
     fi
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

   **Decision matrix:**

   | Library dir? | Lock? | Stale? | Has go.mod? | Action |
   |-------------|-------|--------|-------------|--------|
   | No | No | N/A | N/A | Proceed normally |
   | No | Yes | No | N/A | Warn: "Actively being built (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | No | Yes | Yes | N/A | Offer reclaim: "Interrupted build detected (stale since `<age>`s ago). Reclaim and start fresh?" |
   | Yes | No | N/A | Yes | Existing "Found existing" flow (see below) |
   | Yes | No | N/A | No | Debris: "Found `<api>` directory in library but it appears incomplete (no go.mod). Clean up and start fresh?" If user approves, `rm -rf "$CLI_DIR"` and proceed normally. |
   | Yes | Yes | No | Any | Warn: "Actively being rebuilt (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | Yes | Yes | Yes | Any | Offer reclaim: "Interrupted rebuild detected (stale since `<age>`s ago). Reclaim and start fresh?" |

   **If actively locked (not stale):** Present via `AskUserQuestion` with options to wait, pick a different API, or force-reclaim (`printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE" --force`).

   **If stale lock:** Reclaiming is automatic on `lock acquire` in Phase 2. If user approves, proceed normally — the lock acquire in Phase 2 will auto-reclaim the stale lock.

   **If library exists with go.mod and no lock (completed CLI):** Display context and present options using `AskUserQuestion`:

   > Found existing `<api>` in library (last modified `<date>`).

   If `PRESS_VERSION` is available, append: `Built with printing-press v<version>.`

   If prior research was also found (step 2), include the research summary alongside the library info.

   Then ask:
   1. **"Generate a fresh CLI"** — Re-runs the Printing Press into a working directory, overwrites generated code, then rebuilds transcendence features. Prior research is reused if recent.
   2. **"Improve existing CLI"** — Keeps all current code, audits for quality gaps, implements top improvements. The Printing Press is not re-run.
   3. **"Review prior research first"** — Show the full research brief and absorb manifest before deciding.

   If the user picks option 1, proceed to Phase 1 (research) and then Phase 2 (generate) as normal.
   If the user picks option 2, invoke `/printing-press-polish <api>` to improve the existing CLI.
   If the user picks option 3, display the prior research, then re-present options 1 and 2.

   **MANDATORY when re-using prior research after a binary upgrade.** If the user picks "Generate a fresh CLI" (option 1) AND `PRESS_VERSION` from the manifest differs from the current binary's version (parse both via semver and compare; only fire when the leading minor or major segment changed — patch-level deltas don't trigger this), prompt the user once before kicking off Phase 1 research.

   Construct the prompt's "what changed" list from these category buckets — the categories are stable across versions; the specific machine deltas inside each category are not. Read `docs/CHANGELOG.md` (or run `git log --oneline v<PRESS_VERSION>..v<CURRENT> -- internal/`) and tag each notable change to one of these buckets:

   | Category | Affects prior-brief assumption about... |
   |---|---|
   | **Transport / reachability** | Which sources are reachable, what auth/clearance is needed, which clients (stdlib, Surf, browser-clearance) the brief assumed |
   | **Scoring rubrics** | What Phase 1.5/scorecard dimensions the brief targets, whether prior "high-priority" features still rank as such |
   | **Auth modes** | Whether brief's auth choice (api-key, cookie, composed, oauth) is still the right pick, whether new modes unlock new endpoints |
   | **MCP surface** | Whether brief's MCP shape (endpoint-mirror vs intent vs code-orchestration) matches the latest emit defaults |
   | **Discovery** | Whether browser-sniff / crowd-sniff workflows changed, whether prior gate decisions are still valid |

   For the prompt itself, list only the buckets that have at least one notable change between the two versions. If the CHANGELOG / git log is unavailable, list all five buckets generically and let the user decide.

   > "The prior `<api>` was generated with printing-press v`<PRESS_VERSION>`. The current binary is v`<CURRENT>`. Categories where the machine has changed since then: `<applicable buckets>`. Each can invalidate prior research assumptions. Re-validate the prior brief against the current machine before reusing it?"

   Options:
   1. **Yes, re-validate the prior research** — fold the validation into Phase 1 (briefly re-probe reachability for previously-blocked sources, confirm scoring still classifies the prior CLI's pattern correctly, etc.) before reusing the brief.
   2. **No, reuse the prior research as-is** — proceed with the brief verbatim, even if the underlying machine assumptions are stale.

   The prompt forces the user to acknowledge the version delta and explicitly accept (or refuse) re-validation. Skip it entirely on first generation, on same-version regenerations, or when no prior manifest exists.

   If no CLI exists in the library and no lock is active, skip this step and proceed normally.

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

**When `BROWSER_SNIFF_TARGET_URL` is set:** Skip the catalog check, spec/docs search, and SDK wrapper search — none of these exist for an undocumented website feature. Focus research on understanding what the site/feature does, who uses it, what workflows it supports, and what competitors offer similar functionality. The spec will come from browser-sniffing in Phase 1.7.

Before starting research, check if the API has a built-in catalog entry:

```bash
printing-press catalog show <api> --json 2>/dev/null
```

If the catalog has an entry for this API, branch on the entry type:

**Spec-based entry** (`spec_url` populated) — present the user with a choice:

> "<API> is in the built-in catalog (spec: <spec_url>). Use the catalog config to skip discovery, or run full discovery?"

- If catalog config: use the spec_url from the catalog entry, skip the research/discovery phase
- If full discovery: proceed with the normal research workflow

**Wrapper-only entry** (no `spec_url`, `wrapper_libraries` populated) — this is a reverse-engineered API that has no official spec but has known community libraries the generator can use as implementation backing. Do not try to resolve or browser-sniff a spec. Instead, surface the wrapper options to the user via `AskUserQuestion`:

> "<API> has no official spec. The catalog knows about these community-maintained implementations:"

Present each `wrapper_libraries` entry as a selectable option with language, integration mode, and notes. Example for `google-flights`:
- **krisukox/google-flights-api** (Go, native, MIT) — Pure Go, importable; single-binary CLI with no runtime deps.
- **punitarani/fli** (Python, subprocess, MIT) — broader feature coverage (multi-leg, cabin class); requires Python 3.10+ at runtime.

Capture the user's choice and record it in `$API_RUN_DIR/state.json` under an `implementation` field: `{ "library": "<name>", "url": "<url>", "integration_mode": "native|subprocess|html-scrape" }`. Phase 3 generation reads this to decide whether to `go get` a wrapper, emit a subprocess shell-out, or emit HTML-scrape code. Skip the spec-analysis step entirely — there is no spec.

**No catalog hit** — proceed normally without mentioning the catalog.

**Adding new wrapper-only APIs:** drop a YAML file in `catalog/` with `wrapper_libraries` populated and rebuild the binary. No skill changes needed.

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

## Codebase Intelligence
- [DeepWiki findings if available, otherwise omit this section]
- Source: DeepWiki analysis of {owner}/{repo}
- Auth: [token type, header, env var pattern]
- Data model: [primary entities and relationships]
- Rate limiting: [limits and behavior]
- Architecture: [key insight about internal design]

## User Vision
- [USER_BRIEFING_CONTEXT if provided, otherwise omit this section]

## Source Priority
- [Only present for combo CLIs. Copy the confirmed ordering from `source-priority.json`.]
- Primary: <Source A> — [spec state: official / community-wrapper / no-spec-browser-sniff-required] — [auth: free / paid]
- Secondary: <Source B> — [...]
- Tertiary: <Source C> — [...]
- **Economics:** [e.g., "Primary is free; paid key for <Source B> is scoped to its own commands only."]
- **Inversion risk:** [e.g., "Primary has no OpenAPI; secondary has 53-endpoint spec. Do NOT let spec completeness invert the ordering."]

## Product Thesis
- Name:
- Why it should exist:

## Build Priorities
1. ...
2. ...
3. ...
```

**MANDATORY: Before proceeding to Phase 1.5 (Absorb Gate), you MUST evaluate Phase 1.6 (Pre-Browser-Sniff Auth Intelligence), Phase 1.7 (Browser-Sniff Gate), and Phase 1.8 (Crowd-Sniff Gate) below.** If no spec source has been resolved yet (no `--spec`, no `--har`, no catalog spec URL), the browser-sniff gate decision matrix MUST be evaluated. Do not skip to Phase 1.5.

**Phase 1.5 will refuse to proceed without a `browser-browser-sniff-gate.json` marker file.** Phase 1.7 writes this file with one entry per source (one entry for single-source CLIs, one entry per named source for combo CLIs). Missing marker = HARD STOP back to Phase 1.7. See Phase 1.7 "Enforcement" below for the contract.

## Phase 1.6: Pre-Browser-Sniff Auth Intelligence

After Phase 1 research completes, analyze findings to proactively assess what auth context the user could provide. This step uses research intelligence to ask the right question before browser-sniffing starts, rather than waiting for the user to volunteer "I logged in."

**Skip this step if:** The briefing (Orientation & Briefing section) already captured auth context (`AUTH_CONTEXT` is set from the user selecting "I have an API key or I'm logged in").

**Classify the API's auth profile from research findings:**

| Signal from research | Auth profile | What to ask |
|---------------------|-------------|-------------|
| Community wrappers use API keys (e.g., `STRIPE_SECRET_KEY`), MCP source shows `Authorization: Bearer` headers, spec has `security` section | **API key auth** | "Do you have an API key for `<API>`?" |
| Site has user accounts, research found auth-only features (order history, saved items, rewards, account settings), login pages exist | **Browser session auth** | "This API has authenticated endpoints ([list specific features from research, e.g., order history, saved addresses, rewards]). Are you logged in to `<site>` in your browser? The browser-sniff will find more endpoints if you are." |
| Endpoints accessible without auth, no login-gated features found, community wrappers describe API as "no auth required" | **No auth needed** | Skip this step silently |
| Both API key AND browser session features found | **Dual auth** | Ask about both: API key for smoke testing, browser session for browser-sniff |

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
> "`<API>` has authenticated endpoints ([list features]). Are you logged in to `<site>` in your browser? If so, the generated CLI will support `auth login --chrome` — you'll be able to authenticate just by being logged into the site in Chrome. No API key needed."
>
> 1. **Yes, I'm logged in** — I'll use your session during browser-sniff and enable browser auth in the CLI
> 2. **No, but I can log in** — I'll help you log in before browser-sniffing
> 3. **No, skip authenticated endpoints** — browser-sniff only public endpoints

Set `AUTH_SESSION_AVAILABLE=true` if the user selects option 1 or 2. The Browser-Sniff Gate (Phase 1.7) will use this flag. After traffic capture, Step 2d in [references/browser-sniff-capture.md](references/browser-sniff-capture.md) validates that cookie replay works before enabling browser auth in the generated CLI.

**For dual auth:** Ask about both in sequence — API key first (simple env var check), then browser session.

---

## Phase 1.7: Browser-Sniff Gate

After Phase 1 research, evaluate whether browser-sniffing the live site would improve the spec. This phase MUST produce a decision marker file for every source named in the briefing before Phase 1.5 can proceed.

**Browser discovery is temporary discovery, not a printed-CLI runtime.** Use browser-use, agent-browser, the Claude chrome-MCP (`mcp__claude-in-chrome__*`, when the runtime exposes it), or a manual HAR (optionally augmented with computer-use screenshots for visual guidance, when `mcp__computer-use__*` is exposed) to learn the hidden web contract: URLs, methods, persisted GraphQL hashes, BFF envelopes, response shapes, cookies, CSRF/header construction, HTML/SSR/RSS/JSON-LD surfaces, and whether replay is viable. The final printed CLI must use replayable HTTP, Surf/browser-compatible HTTP, browser-clearance cookie import plus replay, or structured HTML/SSR/RSS extraction. If the only working path requires live page-context execution, HOLD or pivot scope — do not generate a resident browser sidecar transport.

**Automatic offer, explicit consent.** The Printing Press decides when browser discovery should be offered, but opening Chrome, attaching to a browser session, installing browser-use/agent-browser, asking the user to solve a challenge, or driving the user's logged-in Chrome via the chrome-MCP requires explicit user approval through the Phase 0 website choice or the Phase 1.7 `AskUserQuestion` prompt. **Approval at Phase 1.7 covers the full fallback set** including chrome-MCP and computer-use when Step 2c.5's recovery menu later offers them — picking chrome-MCP at the recovery menu is a refinement of the Phase 1.7 consent, not a new consent surface. The disclosure language used at the Phase 1.7 prompt MUST enumerate these possibilities so the user understands what they are approving:

> "Approving browser-sniff means the agent may run browser-use, agent-browser, ask you for a manual DevTools HAR export, or — if the default backends get blocked by an anti-bot gate and your runtime exposes them — drive your already-running Chrome via the chrome-MCP browser extension, or take read-only screenshots of your DevTools window via computer-use to guide you through the HAR export. Capture artifacts are written to `$DISCOVERY_DIR/` and credential headers are stripped at write time. The chrome-MCP option uses your real logged-in Chrome session in a fresh capture tab; the agent never navigates your existing tabs."

If chrome-MCP picks up later in Step 2c.5's recovery menu, do NOT re-fire a per-invocation consent prompt — Phase 1.7's pre-approval covers it. The recovery menu lists chrome-MCP as one of the fallback options the user already pre-approved; the user's selection in the menu is a backend choice, not a new consent step.

### Enforcement: the browser-browser-sniff-gate.json marker file

Phase 1.7 is a hard gate. Phase 1.5 reads a marker file and refuses to proceed without it. The model cannot skip this phase by reasoning around it.

**Marker file location:** `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json`

**Marker file shape:**

```json
{
  "run_id": "20260411-000903",
  "sources": [
    {
      "source_name": "<exact name from briefing, e.g., kayak-direct>",
      "decision": "approved | declined | skip-silent | pre-approved",
      "reason": "<one-line justification>",
      "asked_at": "2026-04-11T00:10:00Z"
    }
  ]
}
```

**Decision values:**

- `approved` — user selected a browser-sniff option via `AskUserQuestion`. Proceed to "If user approves browser-sniff".
- `declined` — user explicitly declined browser-sniff via `AskUserQuestion`. Proceed to "If user declines browser-sniff".
- `skip-silent` — gate was silently skipped per the decision matrix (spec complete, `--har` provided, `--spec` provided, or login required with `AUTH_SESSION_AVAILABLE=false`). The `reason` field names which.
- `pre-approved` — user already chose "The website itself" in Phase 0, where the prompt disclosed temporary Chrome/browser capture during generation, so `BROWSER_SNIFF_TARGET_URL` was set and the question was answered there.

**Every path through Phase 1.7 MUST write a marker entry** — approve, decline, and every silent-skip case. There is no code path that proceeds to Phase 1.5 without writing the marker.

**`asked_at` is mandatory.** It must reflect the actual time `AskUserQuestion` was invoked (or the time the silent-skip decision was made). Fabricated timestamps are a plan violation.

### Banned skip reasons

The following rationales are NOT valid reasons to skip the browser-sniff gate. If any of these apply, you MUST still ask the user via `AskUserQuestion` and record their answer in the marker file:

- **"The target is client-rendered and needs Playwright"** — browser capture tools (browser-use, agent-browser) exist specifically to handle client-rendered sites. A hard-to-browser-sniff target is not the same as an impossible one. Ask.
- **"Direct HTTP/curl got 403, 429, Cloudflare, Vercel, WAF, DataDome, or bot-detection HTML"** — direct HTTP reachability failure is exactly when browser capture is valuable. Do not pivot to RSS, docs-only, official API, or a smaller product shape before attempting the approved browser-sniff. Route to cleared-browser capture instead.
- **"The 3-minute time budget looks tight"** — the time budget applies AFTER the user approves browser-sniff, not before. You do not pre-judge whether a browser-sniff will fit the budget. Ask. If the budget blows after the user approves, fall back per the Time Budget rules below.
- **"We have a substitute data source from another API"** — substituting one source for another is the user's call, not yours. If the user named a specific site or feature (e.g., Kayak /direct), they chose it deliberately. Ask about that exact source. Offering a different data source is a separate conversation AFTER the gate, not a reason to skip it.
- **"Installing browser-use or agent-browser is friction"** — the browser-sniff capture reference already documents the install path. Tooling friction is not a valid skip reason. Ask.
- **"The documentation looks thorough enough"** — the decision matrix already handles this case explicitly. If research found that competitors or community projects reference more endpoints than the spec covers, that IS a gap and you MUST ask.
- **"The user said 'let's go' earlier and implicitly approved everything"** — "let's go" at the briefing stage is consent to proceed with research, not standing approval for every future decision. Ask each gate individually.
- **"The default browser-use / agent-browser path got hard-blocked by a WAF, so the only remaining option is to pivot scope or fall back to RSS/docs"** — this is exactly when the chrome-MCP and computer-use fallback options enter, when the runtime exposes them. Step 1 of `references/browser-sniff-capture.md` detects which fallback MCPs are available; Step 2c.5 composes the recovery menu including those fallbacks; the gate is "ask before giving up," not "auto-pivot when blocked." Do NOT skip the Step 2c.5 menu. Do NOT pivot scope or substitute an alternate target without first asking the user via that menu.

These banned reasons all fired at once in a past combo-CLI run and caused a user-critical source to be silently swapped out. The marker file exists so this cannot happen again. If you find yourself writing a phrase like "skipping browser-sniff because X" where X is one of the above, stop and call `AskUserQuestion`.

### Combo CLIs: per-source enforcement

When the briefing names multiple sources (e.g., "Google Flights + Kayak + FlightAware"), each named source is evaluated independently. The marker file has one entry per source. All entries must be present before Phase 1.5 can proceed.

**Source identification rule:** source names come from the briefing, verbatim. Use the user's exact wording as the `source_name` (normalized to kebab-case is fine: "Kayak /direct" → `kayak-direct`, "Google Flights" → `google-flights`, "FlightAware" → `flightaware`). Do not merge sources. Do not drop one in favor of another.

**Per-source decision flow:**

For each named source, run the "When to offer browser-sniff" decision matrix independently, using the research findings for THAT source. Each source produces its own `AskUserQuestion` call or its own silent-skip marker entry.

**Combo CLI example** (flightgoat pattern — directional guidance, not prescription):

| Source | Spec state | Expected decision |
|--------|------------|-------------------|
| `flightaware` | Documented OpenAPI spec found (53 endpoints, appears complete) | `skip-silent` with reason `spec-complete` |
| `google-flights` | No official spec, but community wrapper exists (`fli`) | Ask via `AskUserQuestion` → record user's answer |
| `kayak-direct` | No spec, no wrapper, user named this as a key feature | Ask via `AskUserQuestion` → record user's answer |

The marker file for this run would contain three entries. Phase 1.5 would HALT if any were missing.

**When the user cares about only one source:** you still ask for all sources that trigger the gate. The user can decline the others. Asking is cheap. Skipping silently breaks the contract.

### Skip this gate entirely when

These are the only cases where Phase 1.7 is bypassed as a whole (not just skipped for one source). Even in these cases, a marker file with a single `skip-silent` entry is written to satisfy Phase 1.5's check:

- User passed `--spec` and the spec is the canonical source for every named source → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-spec" }`
- User passed `--har` → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-har" }`
- `BROWSER_SNIFF_TARGET_URL` is set from Phase 0 (user chose "The website itself") → marker: `{ "source_name": "<api>", "decision": "pre-approved", "reason": "phase-0-website-choice" }`, then go directly to "If user approves browser-sniff"

### Direct HTTP challenge rule

If a reachability probe during Phase 1 research returns bot-protection evidence (`403`, `429`, `cf-mitigated: challenge`, `x-vercel-mitigated: challenge`, `x-vercel-challenge-token`, AWS WAF, DataDome, PerimeterX, CAPTCHA, "Just a moment", "access denied"), **run the no-browser reachability probe before announcing any browser escalation**:

```bash
printing-press probe-reachability "<url>" --json
```

This is non-negotiable. **Do not present transport tiers as a peer menu for the user to choose between.** Phrases like "Browser-sniff + clearance cookie", "Browser-sniff with Surf-only", "Try without browser at all", or "Browser-sniff, prefer Surf" route the user through implementation choices (Surf vs cookie vs full browser) they don't have context to make. The classifier is `probe-reachability`; the agent runs it and decides. Intent-level menus are fine — "Browser-sniff or HOLD?", "Browser-sniff or pick a different API?", or the standard yes/no browser-sniff offers below all ask about goals, not transport, and remain available.

Escalate consent in the order the agent actually needs it, not bundled up-front:

1. **Runtime probe (silent)** — `probe-reachability` runs without prompting. The user already opted into "the website itself" or equivalent in Phase 0; running an HTTP request needs no further consent.
2. **Browser-sniff offer (intent prompt)** — Phase 1.7's normal "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts ask whether to do browser-sniff at all. These are intent-level. Show them when the discovery matrix says to.
3. **Chrome attach (separate consent if escalation happens)** — when the agent actually needs to open or attach to Chrome (because the discovery flow requires a real browser, or because `mode: browser_clearance_http` means the runtime needs cookie capture), surface that as its own moment so the user knows they may need to solve a challenge or sign in. The user-facing prompts at lines below already disclose Chrome attach as a possibility; that is the right place to confirm. Do not pre-announce Chrome attach when the probe has already settled the runtime as `browser_http` and the spec is complete enough to skip discovery — there is no Chrome attach to announce in that path.

Two concerns are decided here, separately:

- **Runtime** (does the printed CLI need browser-compatible HTTP, a clearance cookie, or live page-context execution?) — settled entirely by `probe-reachability`.
- **Discovery** (does Phase 1.7 need to capture XHR traffic via a real browser to learn endpoints?) — settled by Phase 1.7's normal "When to offer browser-sniff" decision matrix above. Independent of runtime.

The probe runs stdlib HTTP, then Surf with a Chrome TLS fingerprint, and emits one of `standard_http | browser_http | browser_clearance_http | unknown`. Apply `mode` to the **runtime** decision:

- **`mode: standard_http`** — runtime is plain HTTP (the original probe was transient). Continue Phase 1.7 normally; the discovery decision is unchanged.
- **`mode: browser_http`** — **runtime is settled: ship Surf transport** (`UsesBrowserHTTPTransport` will be set in the generator's traffic-analysis hints). The printed CLI will not include `auth login --chrome` for clearance cookies — Surf alone clears the challenge. Continue Phase 1.7's discovery decision normally; the existing "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts (above) are framed around endpoint discovery and are correct as-written. Do **not** add clearance-cookie language to those prompts.
- **`mode: browser_clearance_http`** — both probes hit protection signals. The runtime needs more than Surf (clearance cookie or live page-context execution; the probe cannot distinguish), so a real browser capture is required to find out. Proceed through Phase 1.7's normal browser-sniff offer (intent-level yes/no). The consent for Chrome attach happens at the moment the agent actually opens/attaches, where the user-facing prompts in `references/browser-sniff-capture.md` already disclose what's about to happen and may ask the user to solve a challenge. Note in the brief that runtime is provisionally `browser_clearance_http` pending capture results.
- **`mode: unknown`** — probes failed at the transport layer (DNS/timeout/5xx). Fall through to the existing browser-sniff offer; the user decides whether to retry or pivot.

When browser-sniff is approved or pre-approved AND the probe says `browser_clearance_http` or `unknown`:
- Do **not** offer alternate CLI shapes (RSS-first, official API, docs-only, narrower scope, "try anyway") before a real browser capture has been attempted.
- Do **not** write the brief as if browser-sniff is complete after only curl/direct HTTP probes.
- If browser automation tooling is unavailable, offer the user a manual HAR path before offering any scope pivot.

Only after the browser capture attempt fails by the criteria in `references/browser-sniff-capture.md` may you ask whether to pivot to RSS, official API, docs-only, or a smaller CLI scope.

### Time budget

The browser-sniff gate should complete within 3 minutes of the user approving browser-sniff. If browser automation tooling fails to produce results after 3 minutes of attempts, fall back immediately:
- If a spec already exists (enrichment mode): "Browser-Sniff failed after 3 minutes — proceeding with existing spec."
- If no spec exists (primary mode): "Browser-Sniff failed after 3 minutes — falling back to --docs generation."
- If browser-sniff was approved or pre-approved and direct HTTP showed challenge/bot-protection evidence, do **not** auto-fall back to docs/official API, even when `BROWSER_SNIFF_TARGET_URL` is unset. Ask whether the user wants to provide a HAR manually, retry cleared-browser capture, or discuss alternate CLI scope.

Do NOT spend time debugging tool integration issues. Browser-sniff is a temporary discovery aid, not the product runtime. If the first approach fails, fall back to the next option — do not retry the same broken approach.

**The time budget applies AFTER the user approves.** Do not use it as a reason to skip the gate before asking.

### When to offer browser-sniff

| Spec found? | Research shows gaps? | Auth required? | Action |
|-------------|---------------------|----------------|--------|
| Yes | Yes — docs or competitors show significantly more endpoints than the spec | No | **MUST offer browser-sniff as enrichment** |
| Yes | No — spec appears complete | Any | Skip silently (write marker with `decision: skip-silent`) |
| No | Community docs exist (e.g., Public-ESPN-API) | No | **MUST offer browser-sniff OR --docs** — present both options so the user decides |
| No | No docs found either | No | **MUST offer browser-sniff as primary discovery** |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=true` | **Offer authenticated browser-sniff** — the user confirmed a session in Phase 1.6 |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=false` | Skip — fall back to `--docs` (write marker with `decision: skip-silent`, `reason: login-required-no-session`) |

**Gap detection heuristic:** If Phase 1 research found documentation, competitor tools, or community projects that reference significantly more endpoints or features than the resolved spec covers, that's a gap signal. Example: "The Zuplo OpenAPI spec has 42 endpoints, but the Public-ESPN-API docs describe 370+."

**When the decision matrix says "Offer browser-sniff", you MUST ask the user via `AskUserQuestion`.** Skipping the question and writing a `skip-silent` marker is a contract violation — `skip-silent` is only valid when the matrix says "Skip silently" or one of the Banned Skip Reasons is the only thing holding you back (in which case, you should be asking anyway).

Every browser-sniff approval prompt must make the consent boundary explicit:
- browser discovery may open or attach to Chrome during generation,
- it may ask the user to log in or solve a challenge,
- it may request permission to install or upgrade browser-use/agent-browser if missing,
- the printed CLI will only ship if discovery finds a replayable surface and will not keep a browser running as normal command transport.

### Browser-Sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more (competitors reference M+ features). Want me to use temporary browser discovery on `<url>` to find replayable endpoints the spec missed? I may open or attach to Chrome during generation, and I will ask before installing or upgrading browser-use/agent-browser."
>
> Options:
> 1. **Yes — browser-sniff and merge** (temporarily open or attach to Chrome during generation, capture traffic, then merge only replayable discovered endpoints with the existing spec. Ask before installing capture tools.)
> 2. **No — use existing spec** (proceed with what we have)

### Browser-Sniff as primary (no spec found)

Present to the user via `AskUserQuestion`. **If `AUTH_SESSION_AVAILABLE=true`**, include an authenticated browser-sniff option:

> "No OpenAPI spec found for `<API>`. Want me to browser-sniff `<likely-url>` to discover the API from live traffic?"
>
> Options:
> 1. **Yes — authenticated browser-sniff** (temporarily open or attach to Chrome during generation, use your browser session to discover public and authenticated traffic, and generate only replayable CLI surfaces. Recommended since you confirmed a session.) *(Only show when `AUTH_SESSION_AVAILABLE=true`)*
> 2. **Yes — browser-sniff the live site** (temporarily browse `<url>` anonymously, capture API/HTML traffic, and generate a spec only from replayable surfaces. Ask before installing capture tools.)
> 3. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 4. **No — I'll provide a spec or HAR** (user will supply input manually)

When `AUTH_SESSION_AVAILABLE=false`, show only options 2-4 (the existing 3-option prompt).

### If user approves browser-sniff

**Before doing anything else, write the marker entry** for this source:

```json
{
  "source_name": "<normalized name from briefing>",
  "decision": "approved",
  "reason": "<which option they picked, e.g., 'authenticated browser-sniff' or 'browser-sniff and merge'>",
  "asked_at": "<current ISO8601 timestamp>"
}
```

Append it to `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` (create the file if it doesn't exist).

#### Step 0: Identify the User Goal

Before building the capture plan, answer one question: **What does the end user of this CLI actually want to do?**

Read the research brief's Top Workflows. The #1 workflow IS the primary browser-sniff goal. State it in one sentence:
- Domino's: "Order a pizza for delivery"
- Linear: "Create an issue and assign it to a sprint"
- Stripe: "Create a payment intent and confirm it"
- ESPN: "Check today's scores and standings"
- Notion: "Create a page and organize it in a database"

If the API is read-only (news, weather, data feeds), the primary goal is "fetch and filter data" and the flow is search/filter/paginate rather than a multi-step transaction.

The browser-sniff will walk through this goal as an interactive user flow. Secondary workflows become secondary browser-sniff passes if time permits.

State the goal explicitly before proceeding: "Primary browser-sniff goal: [goal]. I will walk through this as a user flow."

Then read and follow [references/browser-sniff-capture.md](references/browser-sniff-capture.md) for the complete
browser-sniff implementation: tool detection, installation, session transfer, browser-use/agent-browser/manual HAR
capture, replayability analysis, and discovery report writing.

### If user declines browser-sniff

**Write the marker entry** for this source before proceeding:

```json
{
  "source_name": "<normalized name from briefing>",
  "decision": "declined",
  "reason": "<which option they picked, e.g., 'use existing spec' or 'use docs instead'>",
  "asked_at": "<current ISO8601 timestamp>"
}
```

Append it to `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json`.

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

### Before leaving Phase 1.7

Every source named in the briefing must have exactly one entry in `browser-browser-sniff-gate.json`. Before proceeding to Phase 1.8, re-read the marker file and verify the count matches the number of named sources from the briefing. If a source is missing, return to the decision matrix for that source. Phase 1.5 will HALT if this check fails.

---

## Phase 1.8: Crowd-Sniff Gate

After Phase 1.7 (Browser-Sniff Gate), evaluate whether mining community signals (npm SDKs and GitHub code search) would improve the spec. Skip this gate entirely if the user already passed `--spec` (spec source is already resolved and appears complete).

**Time budget:** The crowd-sniff gate should complete within 10 minutes. If `printing-press crowd-sniff` fails or times out, fall back immediately:
- If a spec already exists: "Crowd-sniff failed — proceeding with existing spec."
- If no spec exists: "Crowd-sniff failed — falling back to --docs generation."

### When to offer crowd-sniff

| Spec found? | Research shows gaps? | Action |
|-------------|---------------------|--------|
| Yes | Yes — competitors or community projects reference more endpoints | **Offer crowd-sniff as enrichment** |
| Yes | No — spec appears complete | Skip silently |
| No | Community SDKs exist on npm | **Offer crowd-sniff as primary discovery** |
| No | No SDKs or code found | Skip — fall back to `--docs` |

### Crowd-sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more. Want me to search npm packages and GitHub code for `<api>` to discover additional endpoints? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff and merge** (search npm SDKs and GitHub code, merge discovered endpoints with the existing spec)
> 2. **No — use existing spec** (proceed with what we have)

### Crowd-sniff as primary (no spec found)

Present to the user via `AskUserQuestion`:

> "No OpenAPI spec found for `<API>`. Want me to search npm packages and GitHub code to discover the API from community usage? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff the community** (search npm SDKs and GitHub code, generate a spec from discovered endpoints)
> 2. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 3. **No — I'll provide a spec or HAR** (user will supply input manually)

### If user approves crowd-sniff

Read and follow [references/crowd-sniff.md](references/crowd-sniff.md) for the crowd-sniff
command, provenance capture, and discovery report writing.

### If user declines crowd-sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

## Phase 1.5: Ecosystem Absorb Gate

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

**Pre-check (existing):** If no spec or HAR file has been resolved by this point and Phase 1.7 (Browser-Sniff Gate) was not evaluated, STOP. Go back and run the browser-sniff gate decision matrix. The absorb manifest depends on knowing the API surface, which requires a spec.

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
| 1 | Search issues by text | Linear MCP search_issues | FTS5 offline search | Works offline, regex, SQL composable |
| 2 | Create issue | Linear MCP create_issue | --stdin batch, --dry-run | Agent-native, scriptable, idempotent |
| 3 | Sprint board view | jira-cli sprint view | SQLite-backed sprint query | Historical velocity, offline |
```

Every row = a feature we MUST build. No exceptions. If someone else has it, we have it AND it works offline, with --json, --dry-run, typed exit codes, and SQLite persistence.

SDK wrapper methods should be treated as features to absorb — each public method/function is a feature the CLI should match.

**Stubs must be explicit.** If any row in the manifest will ship as a stub (placeholder implementation that emits "not yet wired" / "wip" messaging), add a `Status` column with value `(stub)` and a one-line reason why the full implementation is deferred (e.g., "(stub — requires paid API)", "(stub — requires headless Chrome)"). Do NOT quietly ship stubs for features the user approved as shipping scope.

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
which the subagent's `### Survivors` output already matches:

```markdown
### Transcendence (only possible with our approach)
| # | Feature | Command | Why Only We Can Do This |
|---|---------|---------|------------------------|
| 1 | Bottleneck detection | bottleneck | Requires local join across issues + assignees + cycle data |
| 2 | Velocity trends | velocity --weeks 4 | Requires historical cycle snapshots in SQLite |
| 3 | What did I miss | since 2h | Requires time-windowed aggregation no single API call provides |
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

Read [references/novel-features-subagent.md](references/novel-features-subagent.md)
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
4. `command` must match the actual CLI subcommand that will be built in Phase 3. For subcommands of a resource (e.g., `issues stale`), use the full command path.
5. `example` is a ready-to-run invocation an agent can copy-paste. Use realistic arguments from the API's domain (e.g. `AAPL`, `customer_42`), not `<placeholder>`. Include the `--agent` flag when the feature benefits from structured output.
6. `why_it_matters` is a single agent-facing sentence answering "when should I pick this over a generic API call?"
7. `group` clusters related features under a theme name. Pick 2–5 themes total (e.g. "Local state that compounds", "Agent-native plumbing", "Reachability mitigation"). Use the same `group` string verbatim across features that belong together — exact matches drive README grouping. Leave `group` empty if the CLI has too few novel features to warrant clustering.
8. If no transcendence features scored >= 5/10, omit the `novel_features` field entirely.
9. Do not add a feature to `novel_features` merely to expose it through MCP. Any user-facing Cobra command becomes an MCP tool automatically unless it sets `cmd.Annotations["mcp:hidden"] = "true"`.

**Narrative rules** (the `narrative` object drives README headline, Quick Start, Auth, Troubleshooting, and the entire SKILL.md):
1. `display_name` is the canonical prose name, discovered during research, with exact brand casing and spacing. This is agentic/research-owned, not slug-inferred by Go code. Good: "Product Hunt", "GitHub", "YouTube", "Cal.com". Bad: "Producthunt", "Github", "Youtube", "Cal Com". Use the slug only for binary names, directories, module paths, config paths, and env-var prefixes.
2. `headline` is the bold one-liner rendered beneath the CLI title. Should name the differentiator, not restate the API. Good: "Every Notion feature, plus sync, search, and a local database no other Notion tool has." Bad: "A CLI for the Notion API."
3. `value_prop` expands the headline to 2–3 sentences. Name specific novel features by command where helpful.
4. `auth_narrative` tells the real auth story for this API (crumb handshake, cookie session, OAuth device flow). Omit for standard API-key auth where the generic branch is fine.
5. `quickstart` is a 3–6 step flow using REAL arguments (symbols, IDs, resource names an agent can actually pass). Each step's `comment` explains *why* it runs. This replaces the generic "resource list" first-command fallback.
6. `troubleshoots` captures API-specific failure modes (rate-limit mitigation, cookie expiry, paginated quirks). Each `fix` must be actionable — a command or a concrete setting change.
7. `when_to_use` is SKILL-only narrative. 2–4 sentences describing the kinds of agent tasks this CLI is the right choice for. Not rendered in README.
8. `recipes` are 3–5 worked examples rendered in SKILL.md. Each has a title, a real command, and a one-line explanation. Prefer recipes that exercise novel features. **At least one recipe must pair `--agent` with `--select`** — using dotted paths (e.g. `--select events.shortName,events.competitions.competitors.team.displayName`) when the response is deeply nested. APIs like ESPN, HubSpot, and Linear return tens of KB per call; without a `--select` recipe, agents burn context parsing verbose payloads. Pick a command known to return a large or deeply nested response and show the narrowing pattern. **Regex literals must double-escape backslashes** — write `\\b` not `\b` (and `\\t`, `\\f`, etc.) inside any `command`, `fix`, or other JSON string field. JSON parses `\b` as backspace (0x08), `\f` as form feed (0x0C), and so on, which then leak into the rendered SKILL.md as control bytes that render as nothing in most viewers. The generator's render-time scanner rejects these with a clear offset; double-escape from the start to avoid the error.
9. `trigger_phrases` are natural-language phrases a user might say that should invoke this CLI's skill. Include 3–5 domain-specific phrases (e.g. for a finance CLI: "quote AAPL", "check my portfolio", "options for TSLA") and 2 generic phrases ("use <api-name>", "run <api-name>"). Domain verbs vary — don't just template "use X" variants.
10. All `narrative` fields are optional. Omit fields you can't populate honestly rather than emit filler. The generator falls back to generic content gracefully.
11. **Avoid hardcoded counts in narrative copy when the count tracks a runtime list.** A number embedded in `headline` or `value_prop` ("across N trusted sources", "from N retailers", "queries N vendors") propagates into root.go's Short/Long, the README, the SKILL, the MCP tools description, and `which.go` — every output surface that reads the narrative. When the underlying registry grows or shrinks, the count goes stale across all of those surfaces simultaneously, and a single-line edit to add a source requires hunting down ~10 hardcoded copies. Prefer plural-without-count phrasing ("across the major sources", "from a curated set of retailers") or describe the breadth qualitatively ("dozens of vendors") rather than committing to a specific integer. If a count is load-bearing for the value prop, keep the brief's narrative count-free and have the printed-CLI's README/SKILL author write the count once into a single hand-edited paragraph after generation — accepting that it will need a manual update whenever the registry changes.

Also write discovery pages if browser-sniff was used. The generator reads these from `$API_RUN_DIR/discovery/browser-sniff-report.md` (which the browser-sniff gate already writes there). No additional action needed for discovery pages -- they are already in the right location.

### Priority inversion check (combo CLIs only)

**Only runs when `source-priority.json` exists from the Multi-Source Priority Gate.**

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

1. **Re-run discovery for <Source A>** — loop back to Phase 1.7 browser-sniff or Phase 1.8 crowd-sniff for the primary source specifically.
2. **Accept the inversion** — the user explicitly confirms they're fine with the secondary leading. Record this in `source-priority.json` as `inversion_accepted: true`.
3. **Drop <Source B>** — remove the secondary from the manifest so it can't overshadow the primary.

Do not proceed to the prose showcase until this is resolved.

### Phase Gate 1.5

**STOP.** Present the absorb manifest to the user in two parts: a prose showcase, then a question.

The prose showcase and the `AskUserQuestion` are two separate turns. Print the showcase as a plain text reply with every novel feature spelled out, then call `AskUserQuestion` with four short options whose descriptions fit on one line each. The question text is one sentence; the user reads the showcase to decide and the options to act. Cramming the feature list into an option description collapses both turns into one and is the failure mode this gate exists to prevent.

**Part 1: Prose showcase (print before the AskUserQuestion)**

The showcase exists so the user can decide approve / trim / add ideas without asking a follow-up. Cover three things:

1. **Scope** — how many features absorbed across which tools, how many novel on top, how that stacks up against the best existing tool.
2. **Per-novel-feature readout** — one line each: feature name, what the user gets, and the specific evidence or persona that makes it worth building.
3. **Anything the user should worry about before approving** — stubs, risky dependencies, expensive endpoints, low-confidence ideas.

Show every novel feature that scored ≥5/10. Group by theme if there are more than ~12; never hide features behind "Plus N more" or "see full manifest." If zero qualified, say so plainly: "No novel features scored high enough to recommend. The absorbed features cover the landscape well."

Format is otherwise yours — markdown headings, prose, a numbered list, whatever reads cleanly. The must-haves are the three things above and the ≥5/10 coverage rule.

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

## Phase 1.9: API Reachability Gate

**MANDATORY. Do NOT skip this phase. Do NOT proceed to Phase 2 without running this check.**

Before spending tokens on generation, verify the API actually responds to programmatic requests. One real HTTP call. If it fails, STOP.

**Exception for browser-clearance/browser-sniffed website CLIs:** If Phase 1.7 produced a successful browser capture and `$DISCOVERY_DIR/traffic-analysis.json` reports `reachability.mode` as `browser_clearance_http` or `browser_http`, a plain `curl` 403/429 is expected evidence, not a hard stop. In that case the reachability gate passes only if:
- the browser-sniff capture contains useful non-challenge traffic (real API, SSR data, structured HTML, RSS/feed data, or page-context fetch evidence), and
- Phase 2 will pass `--traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json"` so the generator can emit browser-compatible HTTP transport and, for `browser_clearance_http`, Chrome cookie import.

Do not treat a persistent browser sidecar as a shippable CLI runtime. Browsers are allowed for Printing Press discovery and reusable auth/clearance capture; ordinary printed CLI commands must replay through direct HTTP, Surf/browser-compatible HTTP, or stored reusable auth state. If traffic analysis reports `browser_required`, return to discovery to find a replayable HTTP/HTML/RSS/SSR surface or HOLD the run.

Useful same-site HTML document pages count as a replayable surface when they return real content, not challenge/login pages. Browser-sniff can promote these into `response_format: html` endpoints so generated commands extract page metadata and filtered links through Surf/direct HTTP instead of keeping a browser sidecar alive.

If the browser capture contained only challenge/login/error pages, this exception does not apply.

### The Check

Pick the simplest GET endpoint from the resolved spec (no required params, no auth if possible). If no such endpoint exists, use the spec's base URL. Run one HTTP request:

```bash
curl -s -o /dev/null -w "%{http_code}" -m 10 "<base_url>/<simplest_get_path>" 2>/dev/null
```

Or use `WebFetch` if curl is unavailable. The goal is one real response code.

**If the check returns 403/429 with bot-protection evidence and `probe-reachability` has not already run for this URL during Phase 1.7's Direct HTTP challenge rule, run it now before consulting the decision matrix:**

```bash
printing-press probe-reachability "<base_url>" --json
```

The matrix below references `probe-reachability` `mode` for the bot-detection rows. If the probe already ran in Phase 1.7, reuse that result; do not re-probe.

### Decision Matrix

| Result | Browser capture result | Traffic-analysis reachability | Action |
|--------|------------------------|-------------------------------|--------|
| 2xx/3xx | Any | Any | **PASS** - proceed to Phase 2 |
| 401 (no key provided) | Any | Any | **PASS** - expected when API needs auth and user declined key gate |
| 403/429 with HTML/bot detection | `probe-reachability` returned `browser_http` | runtime is `browser_http` (Surf) | **PASS** - the printed CLI will ship Surf transport which clears the protection. No clearance cookie capture in the printed CLI, regardless of whether browser-sniff also ran for endpoint discovery |
| 403/429 with HTML/bot detection | Successful useful capture | `browser_http` or `browser_clearance_http` | **PASS** - proceed with browser-compatible HTTP / clearance strategy |
| Any | Capture only works through a live page context | `browser_required` | **HOLD** - find a lighter replayable surface before Phase 2 |
| 403/429 with HTML/bot detection | No browser capture attempted but browser-sniff approved/pre-approved AND `probe-reachability` returned `browser_clearance_http` or `unknown` | Any | **RETURN TO PHASE 1.7** - attempt cleared-browser capture before pivoting scope |
| 403/429 with HTML/bot detection | Capture contains only challenge/error pages | Any | **HARD STOP** |
| 403 | No successful useful capture | Research found 403 issues | **HARD STOP** |
| 403 | No successful useful capture | No 403 research issues | **WARN** - ask user |
| Timeout/DNS/connection refused | Any | Any | **WARN** - ask user |

### On HARD STOP

Present via `AskUserQuestion`:

> "WARNING: `<API>` appears to block programmatic access. [what failed: e.g., 'HTTP 403 with HTML error page', 'browser-sniff gate failed with bot detection', 'reteps/redfin has 6+ issues about 403 errors']. Building a CLI against an unreachable API wastes time and tokens."
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

### Pre-Generation Auth Enrichment

Before generating, check whether the resolved spec has auth. This matters most for
browser-sniffed and crowd-sniffed specs where the mechanical auth detection may have failed
(e.g., session expired during browser-sniff, SDK didn't expose auth patterns).

**Check the spec:**
- For internal YAML specs: look for `auth:` section with `type:` not equal to `"none"`
- For OpenAPI specs: look for `components.securitySchemes` or `security` sections

**If auth is missing** (`type: none` or no auth section) AND Phase 1 research found
auth signals, enrich the spec before generation:

1. Check the research brief for auth mentions (Bearer, API key, token, cookie, OAuth)
2. Check Phase 1.5a MCP source code analysis for auth patterns (header names, token formats)
3. Check Phase 1.6 Pre-Browser-Sniff Auth Intelligence results (if the user confirmed auth)

If any source identified auth, **edit the spec YAML** to add the auth section before
running generate. For internal YAML specs:

```yaml
auth:
  type: bearer_token    # or api_key, depending on what research found
  header: Authorization # or the specific header from MCP source
  in: header
  env_vars:
    - <API_NAME>_TOKEN  # bearer_token → _TOKEN, api_key → _API_KEY
```

For OpenAPI specs, add an `info.description` mention if one doesn't exist — the
parser's `inferDescriptionAuth` will detect it automatically.

**Why enrich before generation, not after:** The generator's templates (config, client,
doctor, auth, README) all read `Auth.*` fields from the spec. Patching config.go after
generation only fixes env var support — it misses the doctor auth check, client auth
header, README auth section, and auth command setup. Enriching the spec means every
template produces correct auth from the start.

**When to skip:** If the API genuinely doesn't need auth (ESPN public endpoints, weather
APIs, public data feeds), don't invent auth. The signal must come from research — not
from guessing. No research mention of auth = no enrichment.

#### Tagging endpoints `no_auth: true` (composed/cookie auth APIs)

For APIs whose `auth.type` is `cookie`, `composed`, or `session_handshake` — i.e.,
auth that requires interactive setup (browser cookie capture, multi-step token
exchange) — audit each endpoint individually for whether it actually needs
authentication. The default of `no_auth: false` means "auth required"; flip it to
`no_auth: true` for endpoints that work without credentials.

Typical unauthenticated endpoints worth tagging:

- **Auth-flow primitives:** login, registration, password-reset, email-confirm,
  refresh-token, OAuth callback. The user isn't authenticated when calling these —
  they ARE the auth flow.
- **Public discovery:** store/location finder, menu browse, public catalog,
  category listing, public search, public product detail.
- **Health/metadata:** health checks, version probes, capability flags, sitemap.

Why this matters: the `no_auth` count drives downstream decisions. Specifically,
a composed-auth API with zero `no_auth: true` tags previously got labeled
`mcp_ready: cli-only` and was suppressed from MCPB manifest emission, which
broke the Claude Desktop install path entirely. The current generator
(post-2.5) ships a manifest regardless, but the readiness label, scorecard
breadth dimension, and SKILL.md prose all read better when the count
reflects reality.

If unsure whether an endpoint requires auth, the safe default is `no_auth: false`
(auth required) — over-tagging can mislead users to expect tools that won't work.

**Example for a composed-auth pizza-ordering API:**

```yaml
resources:
  account:
    endpoints:
      register:
        method: POST
        path: /account/register
        no_auth: true   # registering means you're not yet authenticated
      login:
        method: POST
        path: /account/login
        no_auth: true   # the auth flow itself
      profile:
        method: GET
        path: /account/profile
        # no_auth defaults to false — needs auth to view your own profile
  stores:
    endpoints:
      find:
        method: GET
        path: /stores/near
        no_auth: true   # public store finder
  cart:
    endpoints:
      checkout:
        method: POST
        path: /cart/checkout
        # no_auth defaults to false — placing an order needs auth
```

### Pre-Generation MCP Enrichment

Before generating, count the spec's MCP tool surface and decide whether to opt
into the spec's `mcp:` enrichment fields. This matters most for medium-to-large
APIs (>30 tools) where the default endpoint-mirror surface scores poorly on the
scorecard's MCP architectural dimensions and burns agent context at runtime.

**Why before generation, not after:** the generator emits the MCP server's
`main.go`, `tools.go`, `intents.go`, `code_orch.go`, `tools-manifest.json`, and
README MCP section from the spec at generate-time. Patching after generation
fragments across 4+ files, won't be byte-identical, and the polish skill cannot
fix it (polish doesn't re-run generation). Enriching the spec means every
template emits the right surface from the start.

**Count the tool surface.** Two parts:

1. **Typed endpoints** — count `endpoints` across all `resources` (and
   `sub_resources`) in the spec. These become per-endpoint MCP tools at
   generate-time.
2. **Cobratree-walked tools** — the runtime walker registers user-facing Cobra
   commands as MCP tools. Estimate as: `extra_commands` count + ~13 framework
   tools that ship by default (sql, search, context, sync, stale, doctor,
   reconcile, etc., minus framework-skipped). When novel features are planned,
   add their estimated command count.

The total is what an agent loads at MCP server start.

**Decision table:**

| Total tools | Action |
|-------------|--------|
| <30 | Skip — default endpoint-mirror surface is fine. |
| 30–50 | Ask the user. Suggest `mcp.transport: [stdio, http]` for remote reach; suggest `mcp.intents` if there are clear multi-step workflows. |
| >50 | Default to recommending the Cloudflare pattern (transport + code orchestration + hidden endpoint tools). The generator will also print a warning at this size. |

**The Cloudflare pattern** (recommended for large surfaces) — edit the spec's
`mcp:` block before running `generate`:

```yaml
mcp:
  transport: [stdio, http]    # remote-capable; reaches hosted agents
  orchestration: code         # thin <api>_search + <api>_execute pair
  endpoint_tools: hidden      # suppress raw per-endpoint mirrors
  intents:                    # optional; named multi-step intents
    - name: <intent_name>
      description: <agent-facing intent description>
      params: [...]
      steps: [...]
```

`mcp.transport: [stdio, http]` adds HTTP streamable transport so cloud-hosted
agents (Managed Agents, web clients) can connect. `mcp.orchestration: code`
emits the thin search+execute pair that covers the full surface in ~1K tokens.
`mcp.endpoint_tools: hidden` removes the raw per-endpoint tools that would
otherwise still show up alongside the orchestration pair.

**Smaller-surface variants:**

- Just want remote reach? `mcp.transport: [stdio, http]` alone is fine.
- Have 3–5 obvious multi-step workflows but <50 endpoints? Add `mcp.intents`
  without code orchestration; leave `endpoint_tools` at default (visible).

**When to skip entirely:** small APIs (<30 tools), one-shot specs that won't
be installed as MCP servers, or APIs where the user explicitly opts out of MCP
enrichment.

**Verifying after generation:** the scorecard's `mcp_remote_transport`,
`mcp_tool_design`, and `mcp_surface_strategy` dimensions reflect the choices
above. A correctly enriched spec for a >50 tool API should score 10/10 on all
three. If polish later reports these dims weak, that's a sign this enrichment
step was skipped — re-run generation with the enriched spec rather than
trying to fix it in polish.

### Lock and Generate

Before running any generate command, acquire the build lock:

```bash
printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE"
```

If acquire fails (another session holds a fresh lock), present the lock status to the user and let them decide: wait, use a different CLI name, force-reclaim, or pick a different API.

OpenAPI / internal YAML:

```bash
printing-press generate \
  --spec <spec-path-or-url> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --force --lenient --validate
```

Browser-browser-sniff-enriched (original spec + browser-sniff-discovered spec):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --spec-source browser-sniffed \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
# If proxy pattern was detected during browser-sniff, add:
#   --client-pattern proxy-envelope
```

Sniff-only (no original spec, browser-sniff was the primary source):

```bash
printing-press generate \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --spec-source browser-sniffed \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
# If proxy pattern was detected during browser-sniff, add:
#   --client-pattern proxy-envelope
```

Crowd-browser-sniff-enriched (original spec + crowd-discovered spec):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --force --lenient --validate
```

Crowd-sniff-only (no original spec, crowd-sniff was the primary source):

```bash
printing-press generate \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --force --lenient --validate
```

Both browser-sniff + crowd-sniff (merged with original):

```bash
printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
```

Docs-only:

```bash
printing-press generate \
  --docs <docs-url> \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --force --validate
```

GraphQL-only APIs:
- Generate scaffolding only in Phase 2
- Build real commands in Phase 3 using a GraphQL client wrapper

After generation:

**Verify the CLI description.** The root cobra command's `Short:` text comes from
the spec's `cli_description` field when set, then falls back to `narrative.headline`
in `research.json`, then to a generic `"Manage <api> resources via the <api> API"`.
The first two phrasings describe what the *CLI does* ("Manage payments, subscriptions,
and invoices via the Stripe API"); the generic fallback often does too but reads as
template filler. Open `$CLI_WORK_DIR/internal/cli/root.go`, find the `Short:` field,
and confirm it reads as user-facing CLI purpose rather than API description. If it
needs a rewrite, prefer adding a `cli_description:` line to the spec and regenerating
over hand-editing the generated file.

**REQUIRED: Preserve README sections.** The generated README contains 5 standard sections
that the scorecard checks for: Quick Start, Agent Usage, Health Check, Troubleshooting, and
Cookbook. When rewriting the README for this API during Phase 3, **preserve all 5 sections**.
You may add additional sections that help users of this specific API (e.g., "Rate Limits",
"Pagination", "Authentication Setup"), but never remove the standard ones.

**REQUIRED: Verify auth was generated.** Check if the generated `config.go` has auth
env var support (look for `os.Getenv` calls for API key variables). If the
pre-generation auth enrichment ran correctly, this should already be present. If not
(enrichment was missed or the spec was ambiguous), this is the safety net: check the
Phase 1 research brief for auth requirements and manually add env var support to
`config.go` using the pattern: add `APIKey`/`APIKeySource` fields to the Config struct,
and `os.Getenv("<API>_API_KEY")` in the Load function.

**REQUIRED: Validate narrative `command` strings resolve in the CLI tree.**
The LLM (or human) authoring `research.json` can name commands that don't actually
exist in the generated CLI — `<cli> stats` when the real shape is `<cli> reports stats`,
or a command that was dropped because its endpoint had a complex body. Without a check,
the broken commands ship to the README's Quick Start (`narrative.quickstart`) and the
SKILL's recipes (`narrative.recipes`); users copy-paste them and hit `unknown command`
on the very first invocation.

Build the CLI binary, then run `printing-press validate-narrative` against it. The
subcommand walks every `narrative.quickstart[].command` and `narrative.recipes[].command`,
strips the binary name and trailing arguments, and runs `<binary> <words> --help` for
each. The check is offline — no live API access needed.

```bash
QUICKSTART_BINARY="$CLI_WORK_DIR/<api>-pp-cli"
go build -o "$QUICKSTART_BINARY" "$CLI_WORK_DIR/cmd/<api>-pp-cli"

printing-press validate-narrative --strict \
  --research "$API_RUN_DIR/research.json" \
  --binary "$QUICKSTART_BINARY"
```

`--strict` exits non-zero on any missing command, empty subcommand-words entry, or
empty narrative (both sections omitted). Drop `--strict` to get a warn-only report,
or add `--json` for machine-readable output.

If any commands are reported missing, fix them in `research.json` before continuing.
Common causes:

- Resource was renamed during generation (typically the spec uses `users` but the LLM
  wrote `user` in research.json).
- The endpoint exists but is hidden (had a complex body and was dropped from the
  promoted-command surface; reach it via the typed `<resource> <endpoint>` form).
- The command name is a placeholder (`<cli> example`) that should have been replaced
  with a real path.

`narrative.quickstart` drives the README Quick Start and `narrative.recipes` drives
the SKILL.md recipes; getting either wrong silently ships copy-paste-broken examples
to users. The `--help`-walk check is the cheapest catch and runs offline against the
just-built binary — no live API access needed.

After the description rewrite, update the lock heartbeat:

```bash
printing-press lock update --cli <api>-pp-cli --phase generate
```

Then:
- note skipped complex body fields
- fix only blocking generation failures here
- do not start broad polish work yet

If generation fails:
- fix the specific blocker
- retry at most 2 times
- prefer generator fixes over manual generated-code surgery when the failure is systemic
- if retries are exhausted, release the lock and stop:
  ```bash
  printing-press lock release --cli <api>-pp-cli
  ```

## Phase 3: Build The GOAT

<!-- CODEX_PHASE3_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](references/codex-delegation.md)
for the delegation pattern, task type templates, and circuit breaker logic.

When `CODEX_MODE` is false, skip this section.
<!-- CODEX_PHASE3_END -->

Build comprehensively. The absorb manifest from Phase 1.5 IS the feature list.

**macOS framework access:** When the plan or manifest specifies macOS framework APIs (ScreenCaptureKit, CoreGraphics, CoreAudio, Vision, Shortcuts, etc.), use the Swift subprocess bridge pattern - Go shells out to `swift -e '<inline script>'`. Swift is always available with Xcode CLT. Do NOT attempt Python+PyObjC - it requires separate installation and is unreliable across Python distributions. Reference `agent-capture-pp-cli/internal/capture/cgwindow.go` as the canonical example of this pattern.

Priority 0 (foundation):
- data layer for ALL primary entities from the manifest
- sync/search/SQL path - this is what makes transcendence possible

After completing Priority 0, update the lock heartbeat:
```bash
printing-press lock update --cli <api>-pp-cli --phase build-p0
```

Priority 1 (absorb - match everything):
- ALL absorbed features from the Phase 1.5 manifest
- Every feature from every competing tool, matched and beaten with agent-native output
- This is NOT "top 3-5" - it is the FULL manifest

**Lock heartbeat rule for long priority levels:** If Priority 1 has more than 5 features, update the lock heartbeat after every 3-5 features to prevent the 30-minute staleness threshold from triggering mid-build:
```bash
printing-press lock update --cli <api>-pp-cli --phase build-p1-progress
```

Priority 2 (transcend - build what nobody else has):
- ALL transcendence features from Phase 1.5
- The NOI commands that only work because everything is in SQLite
- These are the commands that make someone say "I need this"

**Lock heartbeat rule for Priority 2:** Same rule as Priority 1 — if Priority 2 has more than 3 transcendence features, update the heartbeat after every 2-3 features:
```bash
printing-press lock update --cli <api>-pp-cli --phase build-p2-progress
```

After completing Priority 2, update the lock heartbeat:
```bash
printing-press lock update --cli <api>-pp-cli --phase build-p2
```

Priority 3 (polish):
- skipped complex request bodies that block important commands
- naming cleanup for ugly operationId-derived commands
- tests for non-trivial store/workflow logic
- enrich terse flag descriptions: review generated command flags. If any description is under 5 words or is generic spec-derived text (e.g., "access key", "The player"), improve it using the research brief. For example, change "access key" to "Steam API key (get one at steamcommunity.com/dev/apikey)". Focus on auth keys, IDs, and filter parameters.

### Agent Build Checklist (per command)

After building each command in Priority 1 and Priority 2, verify these 10 principles are met. These map 1:1 to what Phase 4.9's agent readiness reviewer will check - apply them now so the review becomes a confirmation, not a catch-all.

1. **Non-interactive**: No TTY prompts, no `bufio.Scanner(os.Stdin)`, works in CI without a terminal
2. **Structured output**: `--json` produces valid JSON, `--select` filters fields correctly. Hand-written novel commands that build a Go-typed slice/struct and emit JSON MUST call `printJSONFiltered(cmd.OutOrStdout(), v, flags)` instead of `flags.printJSON(cmd, v)`. The helper marshals the value and routes the bytes through `printOutputWithFlags`, picking up `--select`, `--compact`, `--csv`, and `--quiet` for free; direct `flags.printJSON` drops every one of those flags silently. Endpoint-mirror commands already use `printOutputWithFlags`; this rule covers hand-written novel commands. Verify with `<cli> <novel> --json --select <field> | jq 'keys'` returning only the requested fields.
3. **Progressive help**: `--help` shows realistic examples with domain-specific values (not "abc123"). **Use `Example: strings.Trim(\`...\`, "\n")` (preserves leading 2-space indent) NOT `strings.TrimSpace(\`...\`)` (strips it).** TrimSpace makes the first example line unindented; dogfood's example-detection parser is tolerant of this in current versions, but the indented form renders correctly across every Cobra version and is the convention used by every generated command.
4. **Actionable errors**: Error messages name the specific flag/arg that's wrong and the correct usage
5. **Safe retries**: Mutation commands support `--dry-run`, idempotent where possible
6. **Composability**: Exit codes are typed (0/2/3/4/5/7/10 as applicable), output pipes to `jq` cleanly
7. **Bounded responses**: `--compact` returns only high-gravity fields, list commands have `--limit`
8. **Verify-friendly RunE**: Hand-written commands MUST NOT use `Args: cobra.MinimumNArgs(N)` or `MarkFlagRequired(...)`. Cobra evaluates both before RunE runs, so a `--dry-run` guard inside RunE cannot reach if those gates fail. Verify probes commands with `--dry-run` and expects exit 0; commands with hard arg/flag gates fail those probes. Instead: validate inside RunE, fall through to `cmd.Help()` for help-only invocations, and short-circuit on `dryRunOK(flags)` before any IO.
   - **Use string for "positional OR flag" commands**: when a command accepts a positional `<x>` OR a flag `--y` as alternatives (e.g., `snapshot <co>` or `snapshot --domain example.com`), declare `Use: "<cmd> [x]"` with **square brackets** (optional), not `<x>` (required). Validate "exactly one of x or --y" inside RunE. Required positionals declared with angle brackets break verify-skill recipes that use the flag-only form.
9. **Side-effect commands stay quiet under verify**: Any hand-written command that performs a visible side effect (opens a browser tab, sends a notification, plays audio, dials out to an OS handler) MUST follow both halves of the convention:
   - **Print by default; opt in to the action.** The default behavior prints what would happen (`would launch: <url>`); a flag like `--launch` / `--send` / `--play` is required to actually do it. food52's `open` command is the reference shape — see `internal/cli/open.go` after retro #337.
   - **Short-circuit when `cliutil.IsVerifyEnv()` returns true.** The Printing Press verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess; commands that ignore it can spam the user's environment during a verify pass even with the print-by-default flag pattern. The helper is generated into every CLI's `internal/cliutil/verifyenv.go`. Pattern:
     ```go
     if cliutil.IsVerifyEnv() {
         fmt.Fprintln(cmd.OutOrStdout(), "would launch:", url)
         return nil
     }
     ```
   This is defense-in-depth: the verifier also runs a heuristic side-effect classifier, but it can miss commands whose `--help` text and source don't match the heuristics. The env-var check is the floor.
10. **Per-source rate limiting**: any hand-written client in a sibling internal package (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc. — anything not generator-emitted) that makes outbound HTTP calls MUST use `cliutil.AdaptiveLimiter` and surface `*cliutil.RateLimitError` when 429 retries are exhausted. Empty-on-throttle is indistinguishable from "no data exists" and silently corrupts downstream queries. Read [references/per-source-rate-limiting.md](references/per-source-rate-limiting.md) when authoring a sibling client. Enforced at generation time by dogfood's `source_client_check`.

#### Verify-friendly RunE template

Use this shape for every hand-written transcendence command. The generator emits the `dryRunOK` helper into `internal/cli/helpers.go`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if len(args) == 0 {
        return cmd.Help()
    }
    if dryRunOK(flags) {
        return nil
    }
    // ... real work ...
}
```

Why both checks: the `len(args) == 0` branch handles `<cli> mycommand --help` invocations gracefully; the `dryRunOK` branch handles verify's `<cli> mycommand <fixture> --dry-run` probes. Spec-derived commands generated by the Printing Press already follow this pattern -- this rule keeps hand-written novel-feature commands consistent with them.

### Phase 3 delegation: require feature-level acceptance

When Phase 3 implementation is delegated to a sub-agent (via `Agent` tool or Codex), the delegation prompt MUST require behavioral acceptance tests per major feature, not just "does the command build and run." Agents consistently over-report success when the contract is only "command executes without error."

Required in every Phase 3 delegation prompt:

1. **Per-feature acceptance assertions** that check output content, not just exit codes. Examples the prompt should make concrete:
   - Search/ranker: "After `<cli> goat 'brownies'`, assert at least 3 of the top 5 results contain 'brown' in their title or URL. If fewer, the extractor is broken."
   - Lookup: "After `<cli> sub buttermilk --json`, assert the parsed JSON is an array of objects with `substitute`, `ratio`, `context` fields."
   - Transform: "After `<cli> recipe get <known-url> --servings 6`, assert the output ingredient quantities differ from the `--servings 4` invocation (scaling actually ran)."
2. **Absence-of-correctness tests** for every feature whose correct answer can be empty or complete:
   - Calendar/window commands: "Given `--days N`, assert exactly N rows are returned, including zero-count days."
   - Drift/diff commands: "Given only one snapshot or no changed values, assert the command returns `[]` rather than fabricating drift."
   - Alert/watch commands: "Given no matching records, assert empty output plus an honest reason, not stale or unrelated data."
3. **Negative tests** per filter/search command: run with a deliberately-mismatching query and assert the result set does NOT contain irrelevant items.
4. **No parent-command delegation without flags.** If a parent command delegates to a leaf command's `RunE`, the parent must declare every flag the delegate accepts. Prefer group parents that show help over aliasing a parent to a child.
5. **Structured pass/fail report** in the agent's response (raw output of each assertion, not a summary).

A Phase 3 delegation that reports PASS without behavioral assertions is treated as untrusted — re-run acceptance tests before accepting the result.

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

After passing the Priority 1 Review Gate, update the lock heartbeat:
```bash
printing-press lock update --cli <api>-pp-cli --phase build-p1
```

Get Priority 0 and 1 working first (the foundation and absorbed features), pass the review gate, then build Priority 2 (transcendence), then verify.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`

Include:
- what was built
- what was intentionally deferred
- skipped body fields that remain
- any generator limitations found

### Phase 3 Completion Gate

**MANDATORY. Do NOT proceed to Phase 4 until this gate passes.**

Before moving to shipcheck, verify the build log against the absorb manifest:

1. Count the transcendence features in the Phase 1.5 manifest's transcendence table
2. Count the transcendence commands actually built (present in `internal/cli/` and registered in `root.go`)
3. If built < manifest count, **STOP**. List the missing features and build them before proceeding
4. **Test presence for pure-logic novel packages.** Every Go package you created under `internal/` for novel-feature logic (parsers, matchers, scalers, scrapers — anything that isn't command wiring) must have a `_test.go` with at least one table-driven happy-path test per exported function. `printing-press dogfood` surfaces violations as structural issues: pure-logic packages with zero tests fail shipcheck; packages with fewer than 3 test functions are flagged as warnings for Phase 4.85's agentic review. Trivial placeholder tests pass the file-presence check but are the wrong shape — write real assertions or the review catches you.

The generator handles Priority 0 (data layer) and most of Priority 1 (absorbed API endpoints). Priority 2 (transcendence) is always hand-built — the generator does not produce these. If you skip Priority 2, the CLI ships without the features that differentiate it from every other tool.

**Starter templates for novel commands.** Cobra wiring is mechanical and consistent across novel features; the actual feature work lives in the RunE body. Copy the wrapper below and one of the RunE skeletons that follows, fill in the placeholders from the absorb manifest's transcendence row (`Name`, `Command`, `Description`, `Example`, `WhyItMatters`), and replace the body comments with your implementation. Dogfood, verify, and scorecard still apply to the result — the templates raise the floor without changing what shipcheck checks.

```go
// internal/cli/<command>.go — replace <command> with the kebab leaf
// of NovelFeature.Command (e.g., "issues stale" → "issues_stale.go").
package cli

import (
	"github.com/spf13/cobra"
	// add: "encoding/json", "fmt", "<module>/internal/store", etc. as needed
)

func newXxxCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "<leaf-of-Command>",                    // e.g. "stale" for "issues stale"
		Short:   "<NovelFeature.Description, one line>", // truncate to ~70 chars
		Long:    "<optional: Description + WhyItMatters>", // omit if Short is enough
		Example: "  <cli>-pp-cli <Command> --json",       // from NovelFeature.Example
		Annotations: map[string]string{
			// Set "mcp:read-only": "true" only when the command does NOT mutate
			// external state (lookups, comparisons, aggregations, render views).
			// Omit the whole map for commands that mutate (post, delete, write file).
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Pick the matching RunE skeleton below (API-call or store-query),
			// then implement the feature-specific path/query/parsing/formatting.
			return nil
		},
	}
	// cmd.Flags().StringVar(...) — add flags from the planned --flag list, if any
	return cmd
}

// Multi-word Commands like "issues stale": this constructor is registered as
// a child of the matching spec-resource parent (newIssuesCmd) — wire the
// AddCommand call inside root.go via local-variable capture:
//   issuesCmd := newIssuesCmd(flags)
//   issuesCmd.AddCommand(newIssuesStaleCmd(flags))
//   rootCmd.AddCommand(issuesCmd)
// Single-word Commands register directly: rootCmd.AddCommand(newXxxCmd(flags)).
```

**RunE skeleton — API-call shape** (live data via the generated client):

```go
RunE: func(cmd *cobra.Command, args []string) error {
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	// Replace path with the absorbed endpoint or hand-rolled URL. Use
	// cliutil.FanoutRun for any --site/--source/--region CSV fan-out;
	// re-implementing fanout inline is the recipe-goat silent-drop bug.
	data, err := c.Get("/api/v1/path", nil)
	if err != nil {
		return fmt.Errorf("fetching <resource>: %w", err)
	}
	// Parse data into your feature's view. Use cliutil.CleanText for any
	// text extracted from HTML or schema.org JSON-LD; re-implementing
	// HTML-entity unescape inline is the &#39; bug class.
	var view yourViewType // = parse(data)
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	// Human/terminal output (table or pretty print).
	return nil
},
```

**RunE skeleton — store-query shape** (offline data via the local SQLite):

```go
// Declare these alongside the cmd literal, before return cmd:
//   var dbPath string
//   cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

RunE: func(cmd *cobra.Command, args []string) error {
	if dbPath == "" {
		dbPath = defaultDBPath("<cli>-pp-cli") // replace <cli> with the API slug
	}
	db, err := store.OpenWithContext(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	// Replace the query with your aggregation. The store schema mirrors
	// the synced resources; `printing-press dogfood --json` shows the
	// table list. SQL must be SELECT-only; the search/sql gates reject
	// mutating statements.
	rows, err := db.DB().QueryContext(cmd.Context(), `SELECT id, data FROM <table> WHERE ...`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var results []yourRowType // scan rows into the slice
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	// Human/terminal output.
	return nil
},
```

For features that combine both (cache an API response in the store, or fall through to live when the local store is stale), nest one skeleton inside the other and use the `--data-source auto/local/live` flag pattern from the generated `sync` command.

**Shared helpers available to novel code:** The generator emits `internal/cliutil/` in every CLI. When authoring novel commands, prefer `cliutil.FanoutRun` for any aggregation command (any `--site`/`--source`/`--region` CSV fan-out) and `cliutil.CleanText` for any text extracted from HTML or schema.org JSON-LD. Re-implementing these inline is how recipe-goat's trending silent-drop and `&#39;` entity bugs shipped.

**Typed exit-code verification:** If a novel command intentionally returns a non-zero code for a non-error control-flow result, add `cmd.Annotations["pp:typed-exit-codes"] = "0,<code>"` (or the equivalent `Annotations: map[string]string{...}` literal) and document the same command-specific codes in its help. Do not list the global failure palette in command help unless those exits should count as a verify pass for that command; keep general exit-code troubleshooting in README/SKILL prose.

**MCP exposure:** The generator emits `internal/mcp/cobratree/`, and the MCP binary mirrors the Cobra tree at startup. When you add, rename, or remove a user-facing Cobra command, the MCP surface follows automatically. Two annotations control how each command appears as an MCP tool:

- `cmd.Annotations["mcp:hidden"] = "true"` — exclude the command from the MCP surface entirely. Use only for debug/internal commands that should not become agent tools.
- `cmd.Annotations["mcp:read-only"] = "true"` — declare that this command does not modify external state. The MCP server attaches `readOnlyHint: true` to the resulting tool, so hosts like Claude Desktop don't bucket it under "write/delete tools" and demand permission per call. Apply this to every novel command whose only effect is reading from the API or the local store: lookups, comparisons, aggregations, render-only views, status checks. Skip it for commands that mutate external state (orders, posts, deletes) or write to user-visible files outside the local cache.

Endpoint-mirror tools the generator emits from the spec already get the right annotations automatically (`GET` → read-only, `DELETE` → destructive, etc.) — `mcp:read-only` is only needed on hand-authored Cobra commands the spec doesn't cover.

Do not rationalize skipping transcendence features because "the CLI already works for live API interaction." The absorb manifest was approved by the user. Build what was approved.

## Phase 4: Shipcheck

Run one combined verification block via the `shipcheck` umbrella, which runs all five legs (dogfood, verify, workflow-verify, verify-skill, scorecard) in canonical order, propagates exit codes, and prints a per-leg verdict summary. The umbrella is the canonical Phase 4 invocation; running the legs individually is supported but not recommended (operators have skipped legs that way and shipped broken CLIs).

Before running shipcheck, update the lock heartbeat:
```bash
printing-press lock update --cli <api>-pp-cli --phase shipcheck
```

```bash
printing-press shipcheck \
  --dir "$CLI_WORK_DIR" \
  --spec <same-spec> \
  --research-dir "$API_RUN_DIR"
```

The umbrella defaults to `verify --fix` (auto-repair common failures) and `scorecard --live-check` (sample novel-feature output against real targets). Use `--no-fix` for a read-only pass, `--no-live-check` to skip live sampling, or `--json` for a structured envelope (suppresses per-leg output for clean piping). Pass `--api-key` / `--env-var` through to verify when live testing needs a credential, or `--strict` to make verify-skill treat likely-false-positive findings as failures.

If a leg fails, re-run that one leg standalone (e.g., `printing-press verify-skill --dir <CLI_WORK_DIR>`) for focused iteration; once it passes, re-run the full `shipcheck` umbrella to confirm no regression in the others.

Interpretation:
- `dogfood` catches dead flags, dead helpers, invalid paths, example drift, broken data wiring, command tree/config field wiring bugs, stale static MCP surfaces, and novel features that were planned but not built
- `verify` catches runtime breakage and runs the auto-fix loop for common failures
- `workflow-verify` tests the primary workflow end-to-end using the verification manifest (workflow_verify.yaml). Three verdicts: workflow-pass, workflow-fail, unverified-needs-auth
- `verify-skill` checks that every `--flag` and command path in SKILL.md actually exists in the shipped CLI source. Catches bogus examples invented by the absorb LLM (e.g., `search --max-time` when `--max-time` is a `tonight` flag). Exit 1 = findings to fix; exit 0 = SKILL is honest.
- `scorecard` is the structural quality snapshot, not the source of truth by itself

Fix order (update heartbeat between each fix category to prevent stale lock during long fix loops):
1. generation blockers or build breaks
2. invalid paths and auth mismatches
3. dead flags / dead functions / ghost tables
4. broken dry-run and runtime command failures
5. missing novel features (see below)
6. scorecard-only polish gaps

**Missing novel features fix (step 5):** Dogfood writes `novel_features_built` to research.json — only features whose commands actually exist. The original `novel_features` (aspirational list from absorb) is preserved for the audit trail. Dogfood also syncs the generated `.printing-press.json` `novel_features`, `README.md` `## Unique Features` block, `SKILL.md` `## Unique Capabilities` block, and `internal/cli/root.go` `--help` Highlights block from `novel_features_built`; if none survived, it removes the rendered README/SKILL/root help blocks. Dogfood prints `dogfood: synced ... from novel_features_built` for every rendered artifact it changes. After dogfood:

1. Inspect the dogfood planned-vs-built delta
2. Build missing approved features when they are still in scope
3. Rerun dogfood so research.json, `.printing-press.json`, README.md, SKILL.md, and root `--help` Highlights are all synced from the verified set
4. Audit surrounding README/SKILL/root help prose, recipes, trigger phrases, and examples for indirect references to dropped features
5. Log which features were dropped (planned vs built delta)

After fixing each category, update the heartbeat:
```bash
printing-press lock update --cli <api>-pp-cli --phase shipcheck-fixing
```

<!-- CODEX_PHASE4_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](references/codex-delegation.md)
for the Phase 4 fix delegation pattern.

When `CODEX_MODE` is false, fix bugs directly.
<!-- CODEX_PHASE4_END -->

Ship threshold (the umbrella's verdict is the canonical signal — all of these must hold for `shipcheck` to exit 0):
- `shipcheck` exits 0. The umbrella's per-leg summary table shows every leg PASS. A non-zero exit is a fix-before-ship blocker, period — do not ship if the umbrella is red.
- `verify` verdict is `PASS` or high `WARN` with 0 critical failures
- `dogfood` no longer fails because of spec parsing, binary path, or skipped examples
- `dogfood` wiring checks pass (no unregistered commands, no config field mismatches)
- `workflow-verify` verdict is `workflow-pass` or `unverified-needs-auth` (not `workflow-fail`). Exception: if the spec or traffic analysis marks browser-session/browser-clearance auth as required, `unverified-needs-auth` is a `hold` verdict until `auth login --chrome`, `doctor --json`, and a read-only browser-session proof pass against the real site.
- `verify-skill` exits 0 (no mechanical mismatches between SKILL.md and CLI source). Treat non-zero as a fix-before-ship blocker — the SKILL is what agents read; if it lies about the CLI, the lie ships.
- `scorecard` is at least 65 and **no flagship or approved-in-Phase-1.5 feature returns wrong/empty output**

**Behavioral correctness is part of the ship threshold, not just structural quality.** A Grade A scorecard with a broken flagship feature (e.g., `goat "brownies"` returning a chili recipe) does NOT pass the ship threshold. Run a sample invocation of every novel-feature command before declaring shipcheck complete.

**Per-source row for combo CLIs (synthetic spec, multiple data sources).** For every named source in a combo CLI (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) the dogfood test matrix MUST add one row per source: with the source's limiter exhausted (or the upstream genuinely throttling), assert that the user-facing command surfaces a typed `*cliutil.RateLimitError` referencing the source — not empty JSON / `0 results`. A passing row says: "the CLI distinguishes 'no data' from 'we got rate-limited' for this source." The matrix-builder derives rows from the command tree by default; for combo CLIs, also derive rows from the source list. `source_client_check` catches the static signal that throttling is silently swallowed; only the runtime row proves the user-visible behavior.

Maximum 2 shipcheck loops by default.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-shipcheck.md`

Include:
- command outputs and scores
- top blockers found
- fixes applied
- before/after verify pass rate
- before/after scorecard total
- final ship recommendation: `ship` or `hold`

**Verdict rules:**
- `ship`: all ship-threshold conditions met AND no known functional bugs in shipping-scope features.
- `hold`: one or more conditions missing, OR functional bugs exist that cannot be fixed in-session.

`ship-with-gaps` is deprecated as a default verdict. It is NOT valid for bugs that require only 1-3 file edits; those MUST be fixed before ship. It is only acceptable when (a) a bug genuinely requires a refactor, external dependency change, or API access not available in-session, AND (b) the bug is clearly documented with a `## Known Gaps` block in both the shipcheck report and the generated README. If an agent cannot meet both (a) and (b), the verdict is `hold`, not `ship-with-gaps`.

If the final verdict is `hold`, release the lock without promoting to library:
```bash
printing-press lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR` for potential future retry. Proceed to Phase 5.6 to archive manuscripts (archiving still happens on hold).

## Phase 4.8: Agentic SKILL Review

**Runs after shipcheck, before Phase 5.** `verify-skill` (Phase 4) is a mechanical check — it catches wrong flags on wrong commands, undeclared flags, and positional-arg count mismatches. It cannot catch **semantic** issues that only a reader notices:

- A trigger phrase promises behavior the CLI doesn't have ("plan dinners for the week" when there's no `meal-plan suggest`, only manual `meal-plan set`)
- A novel-feature description says the feature does X; the actual command does Y
- The AuthNarrative mentions `auth login --chrome` when the CLI's auth subcommands are only `set-token`/`logout`/`status`
- Novel features shipped as stubs aren't labeled as such in the SKILL (contradicts Phase 1.5 stub-marking rule)
- Recipes/worked examples produce output that doesn't match their prose claims
- Trigger phrases sound agent-natural or sound like marketing copy

### Dispatch

Use the Agent tool (general-purpose or a dedicated reviewer) with this prompt contract:

> Review the SKILL.md at `$CLI_WORK_DIR/SKILL.md` against the shipped CLI. You have these ground-truth sources:
>
> - `<cli> --help` output — enumerate it recursively if needed.
> - The absorb manifest in `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`.
> - The `research.json` `novel_features` (planned) and `novel_features_built` (verified) fields.
> - The README at `$CLI_WORK_DIR/README.md`.
>
> For each of these semantic checks, report findings under 50 words each:
>
> 1. **Trigger phrases match capabilities.** Does every trigger phrase in the SKILL's description frontmatter correspond to something the CLI can actually do? Flag phrases that imply missing capabilities.
> 2. **Verified-set alignment.** The SKILL's "Unique Capabilities" commands must exactly match `novel_features_built` from research.json. Planned-only features from `novel_features` must not appear there after dogfood sync. Any extra or missing command is a finding.
> 3. **Novel-feature descriptions match commands.** For each feature in the "Unique Capabilities" section, run `<cli> <command> --help` and verify the description matches the actual behavior. Mismatches are findings.
> 4. **Stub/gated disclosure.** If a feature that remains in `novel_features_built` is intentionally stubbed, CF-gated, unavailable without external setup, or returns a known-gap response, the SKILL must label that limitation where an agent decides whether to use the command. Unlabeled limitations are findings.
> 5. **Auth narrative accuracy.** Read the auth section. Does every `auth login/set-token/status` invocation mentioned actually exist on the CLI? Does the narrative match the CLI's auth type (api_key vs cookie vs session_handshake)?
> 6. **Recipe output claims.** For the worked examples, does the prose claim match what the command actually produces? (Not the exact output — the shape and intent.)
> 7. **Marketing-copy smell.** Does the SKILL read like ad copy ("comprehensive", "seamless", "powerful") instead of concrete capability descriptions? Those phrases are findings.
>
> Return a list of findings. For each: check name, severity (error/warning), line number, one-sentence fix. If SKILL passes all seven checks, return "PASS — no findings."

### Gate

- If the reviewer returns PASS, proceed to Phase 5.
- If the reviewer returns findings of severity `error`, fix them before Phase 5. Same fix-now contract as other shipcheck findings.
- If the reviewer returns only `warning` findings, surface them to the user and proceed if they approve.

### Why agentic vs template-only

A template-level check would require every possible semantic mismatch to be pattern-matchable against source. Many aren't — "does this trigger phrase correspond to what the CLI does" is an LLM-shaped question. Accept the token cost for the catch.

### Known blind spots

The agent can't verify runtime behavior without running commands; stick to help-text and source-based claims. For runtime-behavior claims (e.g., "returns 5 matching recipes"), Phase 5 dogfood is the right gate.

## Phase 4.9: README/SKILL Correctness Audit

**Runs after Phase 4.8, before Phase 5.** Phase 4.8 reviews whether the SKILL's trigger phrases and major claims match shipped behavior. Phase 4.9 reviews the two user-facing artifacts as documents: README.md and SKILL.md must not contain boilerplate that does not apply to this CLI.

Use the Agent tool or review directly with this prompt contract:

> Audit `$CLI_WORK_DIR/README.md` and `$CLI_WORK_DIR/SKILL.md` for factual correctness against the shipped CLI. Ground truth is `<cli> --help` recursively, `$CLI_WORK_DIR/internal/cli/*.go`, `$RESEARCH_DIR/research.json`, and the absorb manifest.
>
> Check:
> - Every command, subcommand, flag, exit code, config path, and example resolves to the printed CLI.
> - README `## Unique Features` and SKILL `## Unique Capabilities` match `novel_features_built`; planned-only features from `novel_features` are not claimed after dogfood sync.
> - Surrounding prose, recipes, trigger phrases, and examples do not indirectly promise planned features that dogfood dropped.
> - No placeholder literals remain in executable examples (`<cli>`, `<command>`, `<resource>`, `<CLI>`).
> - Boilerplate matches the CLI shape: no CRUD/retry/create-stdin/delete/cache/auth/async-job claims unless the CLI actually implements them.
> - Read-only CLIs say they are read-only and do not imply create/update/delete support.
> - No-auth CLIs omit auth troubleshooting and auth exit-code claims unless the binary can raise them.
> - Stubbed, CF-gated, or unavailable commands are disclosed where an agent decides whether to use the CLI.
> - The SKILL has anti-triggers: common requests this CLI should not handle.
> - Brand/display names use the canonical prose name from research, not only the slug.
> - Marketing phrases map to real commands; invented feature names are findings.
>
> Return findings with file, line, severity, and fix. If both files are correct, return `PASS — README/SKILL correctness verified`.

**Gate:** Any error finding is fix-before-Phase-5. Warnings may proceed only when they are explicitly explained in the acceptance report.

## Phase 4.85: Agentic Output Review

**Runs after Phase 4.8, before Phase 5.** Phase 4.8 reviews SKILL.md prose against the shipped CLI. Phase 4.85 reviews the CLI's **actual command output** for plausibility bugs that rule-based checks can't encode (substring-match relevance failures, format bugs, silent source drops, ranking failures). The dispatch prompt, gate logic, and known blind spots live in the `printing-press-output-review` sub-skill — single source of truth shared with the polish skill (which runs the same review during its diagnostic loop).

Invoke the sub-skill via the Skill tool:

```
Skill(
  skill: "cli-printing-press:printing-press-output-review",
  args: "$CLI_WORK_DIR"
)
```

The sub-skill carries `context: fork` so the reviewer agent's diagnostic chatter stays isolated from this generation flow. It returns a `---OUTPUT-REVIEW-RESULT---` block with `status: PASS|WARN|SKIP` and a list of findings.

**Wave B rollout policy:** all findings surface as **warnings**, not blockers. Shipcheck does not fail on Phase 4.85 findings. Log the findings to `manuscripts/<api>/<run>/proofs/phase-4.85-findings.md` and surface them to the user. The user decides case by case whether to fix before shipping. Wave B calibrates false-positive rates before Wave C flips errors to blocking.

## Phase 5: Dogfood Testing

**MANDATORY when an API key is available. Do NOT skip or shortcut this phase.**

Shipcheck verified commands start and return exit codes. Dogfood verifies the CLI
produces correct, useful output for real workflows. These are different checks.

### Step 1: Ask the user for depth

Present via `AskUserQuestion`:

> "Shipcheck passed. How thoroughly should I test against the live API?"
>
> 1. **Full dogfood (recommended)** — Complete mechanical test matrix across every leaf subcommand, including help, happy-path, JSON parse validation, output-mode fidelity, and error paths. Includes write-side lifecycle only with an approved disposable fixture/sandbox plan.
> 2. **Quick check** — A compromise subset when the user explicitly wants speed or full dogfood would consume unapproved real-world cost/side effects.

**Recommendation rule:** Full dogfood is the default recommendation. Do not downgrade because of ordinary time cost; a few extra minutes is cheap compared with the generation run and the cost of shipping a broken CLI. Recommend Quick only when the user asks for speed or when full live testing would create unapproved real-world cost/side effects (paid credits, outbound messages, public posts, real orders, irreversible deletes, invites, bookings, charges). Potential mutation is not itself a reason to downgrade: if the user approves a test account/workspace/calendar/project or the CLI can create and clean up disposable fixtures, Full dogfood remains recommended.

There is no skip option when an API key is available or the API requires no
auth. Phase 5 auto-skips ONLY when the API requires auth AND no key is
available: display "No API key available — skipping live dogfood testing.
The CLI was verified against exit codes and dry-run only."

For APIs with `auth.type: none` (or no auth section in the spec), Phase 5
is MANDATORY — the API is freely testable without any credentials. Do not
skip testing just because no API key was detected. No-auth APIs are the
easiest to test and the most embarrassing to ship untested.

Do NOT proceed without asking. Do NOT substitute an ad-hoc smoke test. If some commands cannot be exercised because fixture values are missing, classify them as `BLOCKED_FIXTURE` and file/fix the machine gap; do not use that as a reason to recommend Quick.

### Step 2: Build the test matrix mechanically

**Full dogfood is not a judgment call about "enough."** Build the test matrix from the CLI's actual command tree:

1. Parse `<cli> --help` recursively until every leaf subcommand is enumerated.
2. Write the full command list to `$PROOFS_DIR/<stamp>-dogfood-matrix.txt` before running any tests.
3. For each leaf subcommand, generate at minimum these tests:
   - **Help check**: `<cli> <subcmd> --help` returns exit 0 and produces an Examples section.
   - **Happy path**: one invocation with realistic args. Exit 0 expected.
   - **JSON fidelity**: append `--json` to the happy path; pipe through `python3 -c "import sys,json; json.load(sys.stdin)"` to assert valid JSON.
   - **Error path** (when the command takes an arg): one invocation with a deliberately bad arg (invalid ID, malformed date, non-existent URL). Exit non-zero expected.
4. Render a live progress line at start: `Dogfood matrix: N leaves × 3-4 tests = M tests total. Running...`
5. Report pass/fail per test, accumulate to a final tally, and write `$PROOFS_DIR/<stamp>-dogfood-results.md`.

**Critical: pipe-free exit-code checks.** A shell command like `"$BIN" foo | tail -2` captures `tail`'s exit code, not the binary's. Always run as:

```bash
DOGFOOD_TMP_DIR="/tmp/printing-press/dogfood"
mkdir -p "$DOGFOOD_TMP_DIR"
OUT_FILE="$(mktemp "$DOGFOOD_TMP_DIR/<api>-out-XXXXXX")"
"$BIN" <subcmd> <args> > "$OUT_FILE" 2>&1
code=$?
# then check $code directly
rm -f "$OUT_FILE"
```

Never use `"$BIN" ... && echo ok || echo fail` for exit-code testing — short-circuit and unpredictable piping masks real failures.

**Quick check (auto-selected test subset):**
1. `doctor` — auth valid, API reachable.
2. 3-5 list commands — return data, not empty.
3. `sync --full` → data appears in local store.
4. `search "<term from synced data>"` — finds results.
5. One list command with `--json`, `--select <fields>`, `--csv` — all produce correct output.
6. One transcendence command — produces output that relates to the query (not just non-empty: verify relevance by checking output content contains query tokens or expected shape).

**Full dogfood adds to the matrix:**
- Every approved feature in the Phase 1.5 manifest gets a sample invocation with domain-realistic args.
- For every command that takes an arg, one error-path test.
- For every command that supports `--json`, one JSON parse validation.
- For write-side commands (when API key + user consent): create test entity with obviously-test data, verify in subsequent list/get, test one mutation, verify change.

**Binary support:** future versions of `printing-press dogfood --live` will run this matrix as a single command — see issue #198. Until that ships, the agent must construct the matrix manually from `--help` and run it.

### Step 3: Fix issues inline

When a test fails, fix it immediately — do not accumulate failures. Tag each fix:
- **CLI fix** — specific to this printed CLI
- **Printing Press issue** — should be fixed in the Printing Press (note for retro)

### Step 4: Report and gate

Write a structured acceptance report. This report is **required** — Phase 5.6
checks for it before promoting.

```
Acceptance Report: <api>
  Level: Quick Check / Full Dogfood
  Tests: N/M passed
  Failures:
    - [command]: expected [X], got [Y]
  Fixes applied: K
    - [each fix]
  Printing Press issues: J
    - [each issue for retro]
  Gate: PASS / FAIL
```

**Acceptance threshold:**
- Quick Check: 5/6 core tests must pass. Auth (`doctor`) or sync failure is automatic FAIL.
- Full Dogfood: every mandatory test in the matrix must pass. A single broken flagship feature is automatic FAIL. Auth/sync failures are automatic FAIL.

**Bugs surfaced in Phase 5 must be fixed now, not deferred.** Do not offer the user a "ship as-is and file for v0.2" option when the fix is a 1-3 file edit. Present a "Fix now" (default), "Fix critical only", "Hold (don't ship)" set. Deferring bugs to a v0.2 backlog is an anti-pattern — context is freshest in-session, and a backlog that may never be revisited ships known-broken CLIs.

**Gate = PASS:** proceed to Phase 5.5 (Polish).

**Gate = FAIL:** fix issues inline (Step 3) and re-run failing tests, up to
2 fix loops. If the gate still fails after 2 loops, put the CLI on hold:
```bash
printing-press lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR`. Proceed to Phase 5.6 to archive
manuscripts (archiving still happens on hold). Tag the failure reason in the
acceptance report so the next run can learn from it.

See [references/dogfood-testing.md](references/dogfood-testing.md) for additional
guidance on common failure patterns and what NOT to test.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-acceptance.md`

## Phase 5.5: Polish

**Always runs.** Invoke the `printing-press-polish` skill to run diagnostics, fix quality issues, and return a delta. The polish skill carries `context: fork` in its frontmatter, so its diagnostic-fix-rediagnose loop runs in a forked context — diagnostic spam, fix iterations, and re-audits stay scoped to the polish session and don't pollute this generation flow. The skill is autonomous — no user input needed. The goal is to ship the best CLI possible, not the fastest.

Invoke via the Skill tool (**foreground** — must complete before promoting):

```
Skill(
  skill: "cli-printing-press:printing-press-polish",
  args: "$CLI_WORK_DIR"
)
```

**Pass `$CLI_WORK_DIR` (the absolute working-dir path), not the API slug.** Phase 5.5 fires before Phase 5.6 promotes the working CLI to the library, so `$PRESS_LIBRARY/<slug>/` either doesn't exist yet or contains the *prior* run's CLI. If you paraphrase the args to the slug (e.g., `args: "producthunt"`), polish silently operates on the stale library copy.

The polish skill runs the full diagnostic-fix-rediagnose loop including MCP tool quality polish (via `printing-press tools-audit` plus the playbook at `references/tools-polish.md`) and ends its response with a `---POLISH-RESULT---` block containing scorecard/verify/tools-audit before/after, fixes applied, and a ship recommendation.

Parse the result block. Display the delta to the user:

```
Polish pass:
  Verify:      86% → 93% (+7%)
  Scorecard:   92  → 94  (+2)
  Tools-audit: 76  → 0   pending findings
  Fixed: [summary of fixes_applied from result]
```

**Verdict override:** If the polish skill's `ship_recommendation` is `hold` and the Phase 4 verdict was `ship`, downgrade to `hold`. Release the lock without promoting.

Write the polish skill's full response to:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-polish.md`

## Phase 5.6: Promote and Archive

### Acceptance gate check

Before promoting, verify the acceptance artifact exists when an API key was
available during this run:

- If `$PROOFS_DIR/*-acceptance.md` exists with `Gate: PASS` → proceed to promote.
- If `$PROOFS_DIR/*-acceptance.md` exists with `Gate: FAIL` → CLI is on hold. Do NOT promote. Proceed to Archive Manuscripts.
- If no acceptance artifact exists AND an API key was available → Phase 5 was skipped. Go back and run it. Do NOT promote without it.
- If no acceptance artifact exists AND no API key was available → acceptable. Proceed to promote (the CLI was verified mechanically only).

### Promote to Library

If the shipcheck verdict is `ship`, promote the verified CLI from the working directory to the library. This must happen BEFORE archiving — the CLI in the library is the primary deliverable.

```bash
# Promote verified CLI to library (copies working dir, writes manifest, releases lock)
printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"
```

The `promote` command handles the full sequence: stages the working directory, atomically swaps it into `$PRESS_LIBRARY/<api>` (slug-keyed), writes the `.printing-press.json` manifest, updates the `CurrentRunPointer`, and releases the lock — all in one step. The `--cli` flag accepts the CLI binary name; the Go code translates to the slug-keyed library path internally.

If the shipcheck verdict is `hold`, the lock was already released in Phase 4. Do NOT promote. The working copy stays in `$CLI_WORK_DIR` and is not copied to the library.

### Archive Manuscripts

Archive the run's research, proofs, and discovery artifacts to `$PRESS_MANUSCRIPTS/`
**unconditionally** after promotion (or after lock release for `hold` verdicts). This
happens regardless of the shipcheck verdict — even a `hold` run produces research
and proofs that future runs should be able to reuse.

Archiving and publishing are separate concerns. Archiving preserves research for
future `/printing-press` runs on the same API. Publishing ships the CLI to the
library repo. A run that isn't ready to publish still produces valuable research.

```bash
# Archive under API slug (e.g., steam-web), matching the slug-keyed library layout.
API_SLUG="<api>"
mkdir -p "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID"
cp -r "$RESEARCH_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research" 2>/dev/null || true
cp -f "$API_RUN_DIR/research.json" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research.json" 2>/dev/null || true
cp -r "$PROOFS_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs" 2>/dev/null || true

# Archive discovery artifacts (browser-sniff captures, URL lists, traffic analysis, browser-sniff report).
# Remove session state before archiving — contains authentication cookies/tokens.
rm -f "$DISCOVERY_DIR/session-state.json"

# Strip response bodies from HAR before archiving to control size.
if [ -d "$DISCOVERY_DIR" ]; then
  for har in "$DISCOVERY_DIR"/browser-sniff-capture.har "$DISCOVERY_DIR"/browser-sniff-capture.json; do
    if [ -f "$har" ] && command -v jq >/dev/null 2>&1; then
      jq 'del(.log.entries[].response.content.text)' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har" || rm -f "${har}.stripped"
    fi
  done
  cp -r "$DISCOVERY_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/discovery" 2>/dev/null || true
fi
```

**MANDATORY: After archiving, you MUST proceed to Phase 6 (Publish) below. Do not print a summary and stop. Do not treat archiving as the end of the run. The run ends when the user has been asked about publishing (or the verdict is `hold`).**

## Phase 6: Publish

**This phase is NOT optional.** Every run with a `ship` verdict MUST reach this point. Do not skip it.

After archiving, offer to publish the CLI to the library repo.

### Gate

Use the most recent shipcheck verdict:
- if Phase 5 reran shipcheck after a live-smoke fix, use that rerun verdict
- otherwise use the Phase 4 verdict

Skip this phase entirely if the final shipcheck verdict is `hold`. Only proceed for `ship`.

### Check for existing PR

Run a lightweight check for your own open publish PR. The `--author @me` filter avoids matching someone else's PR for the same API slug.

```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/<api>" --state open --author @me --json number,url --jq '.[0]' 2>/dev/null
```

If this fails (gh not authenticated, network error, etc.), continue without PR context — the publish skill will handle auth in its own Step 1.

### Offer

Present via `AskUserQuestion`:

**If an existing open PR was found:**

> "<api> passed shipcheck. There's an open publish PR (#N). Want to update it with this version?"
>
> 1. **Yes — update PR #N** (re-validate, re-package, and push to the existing PR)
> 2. **No — I'm done**

**If no existing PR:**

> "<api> passed shipcheck ([score]/100, verify [pass-rate]%). What do you want to do?"
>
> 1. **Publish now** (validate, package, and open a PR)
> 2. **Polish first** (run `/printing-press-polish` to fix verify failures, dead code, and README before publishing)
> 3. **Run retro** (analyze the session to find improvements for the Printing Press)
> 4. **Done for now**

If the shipcheck report contains a `## Known Gaps` block (the rare case where `ship-with-gaps` was justified per the Phase 4 rules — a refactor or external-dependency blocker), prepend: "Note: shipcheck documented known gaps (see the shipcheck report above)." and recommend the polish option.

### If "Publish now"

Invoke `/printing-press publish <api>`. The publish skill handles everything from there.

### If "Polish first"

Invoke `/printing-press-polish <api>`. The polish skill runs diagnostics, fixes issues, reports the delta, and offers its own publish at the end.

### If "Run retro"

Invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

### If "Done for now"

End normally. The CLI is in `$PRESS_LIBRARY/<api>` and the user can run `/printing-press publish` or `/printing-press-polish` later.

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
