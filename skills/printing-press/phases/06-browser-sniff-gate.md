## Phase 1.7: Browser-Sniff Gate

After [Phase 1](04-research-brief.md) research, evaluate whether browser-sniffing the live site would improve the spec. This phase MUST produce a decision marker file for every source named in the briefing before [Phase 1.5](08-ecosystem-absorb-gate.md) can proceed.

**Browser discovery is temporary discovery, not a printed-CLI runtime.** Use browser-use, agent-browser, the Claude chrome-MCP (`mcp__claude-in-chrome__*`, when the runtime exposes it), or a manual HAR (optionally augmented with computer-use screenshots for visual guidance, when `mcp__computer-use__*` is exposed) to learn the hidden web contract: URLs, methods, persisted GraphQL hashes, BFF envelopes, response shapes, cookies, CSRF/header construction, HTML/SSR/RSS/JSON-LD surfaces, and whether replay is viable. The final printed CLI must use replayable HTTP, Surf/browser-compatible HTTP, browser-clearance cookie import plus replay, or structured HTML/SSR/RSS extraction. If the only working path requires live page-context execution, HOLD or pivot scope — do not generate a resident browser sidecar transport.

**Automatic offer, explicit consent.** The Printing Press decides when browser discovery should be offered, but opening Chrome, attaching to a browser session, installing browser-use/agent-browser, asking the user to solve a challenge, or driving the user's logged-in Chrome via the chrome-MCP requires explicit user approval through the [Phase 0](03-resolve-and-reuse.md) website choice or the Phase 1.7 `AskUserQuestion` prompt. **Approval at Phase 1.7 covers the full fallback set** including chrome-MCP and computer-use when Step 2c.5's recovery menu later offers them — picking chrome-MCP at the recovery menu is a refinement of the Phase 1.7 consent, not a new consent surface. The disclosure language used at the Phase 1.7 prompt MUST enumerate these possibilities so the user understands what they are approving:

> "Approving browser-sniff means the agent may run browser-use, agent-browser, ask you for a manual DevTools HAR export, or — if the default backends get blocked by an anti-bot gate and your runtime exposes them — drive your already-running Chrome via the chrome-MCP browser extension, or take read-only screenshots of your DevTools window via computer-use to guide you through the HAR export. Capture artifacts are written to `$DISCOVERY_DIR/` and credential headers are stripped at write time. The chrome-MCP option uses your real logged-in Chrome session in a fresh capture tab; the agent never navigates your existing tabs."

If chrome-MCP picks up later in Step 2c.5's recovery menu, do NOT re-fire a per-invocation consent prompt — Phase 1.7's pre-approval covers it. The recovery menu lists chrome-MCP as one of the fallback options the user already pre-approved; the user's selection in the menu is a backend choice, not a new consent step.

### Enforcement: the browser-browser-sniff-gate.json marker file

Phase 1.7 is a hard gate. [Phase 1.5](08-ecosystem-absorb-gate.md) reads a marker file and refuses to proceed without it. The model cannot skip this phase by reasoning around it.

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
- `pre-approved` — user already chose "The website itself" in [Phase 0](03-resolve-and-reuse.md), where the prompt disclosed temporary Chrome/browser capture during generation, so `BROWSER_SNIFF_TARGET_URL` was set and the question was answered there.

**Every path through Phase 1.7 MUST write a marker entry** — approve, decline, and every silent-skip case. There is no code path that proceeds to [Phase 1.5](08-ecosystem-absorb-gate.md) without writing the marker.

**`asked_at` is mandatory.** It must reflect the actual time `AskUserQuestion` was invoked (or the time the silent-skip decision was made). Fabricated timestamps are a plan violation.

### Banned skip reasons

The following rationales are NOT valid reasons to skip the browser-sniff gate. If any of these apply, you MUST still ask the user via `AskUserQuestion` and record their answer in the marker file:

- **"The target is client-rendered and needs Playwright"** — browser capture tools (browser-use, agent-browser) exist specifically to handle client-rendered sites. A hard-to-browser-sniff target is not the same as an impossible one. Ask.
- **"Direct HTTP/curl got 403, 429, Cloudflare, Vercel, WAF, DataDome, or bot-detection HTML"** — direct HTTP reachability failure is exactly when browser capture is valuable. Do not pivot to RSS, docs-only, official API, or a smaller product shape before attempting the approved browser-sniff. Route to cleared-browser capture instead.
- **"Direct HTTP/curl got HTTP `200` but only a content-less shell, interstitial, or deterministic-size truncation"** — a 200-served shell is a clearance or JavaScript challenge, not a clean response. Do not conclude `IP-blocked`, `rate-limited`, or `wait it out` from this shape. Before declaring the target unreachable, climb the ladder: probe-reachability body-check, curl-impersonate/TLS check, real-browser cookie-warm via the cleared-browser path or chrome-MCP when available, then ask the user. Use chrome-MCP to understand the wall even when it cannot export cookie values.
- **"The 3-minute time budget looks tight"** — the time budget applies AFTER the user approves browser-sniff, not before. You do not pre-judge whether a browser-sniff will fit the budget. Ask. If the budget blows after the user approves, fall back per the Time Budget rules below.
- **"We have a substitute data source from another API"** — substituting one source for another is the user's call, not yours. If the user named a specific site or feature (e.g., Kayak /direct), they chose it deliberately. Ask about that exact source. Offering a different data source is a separate conversation AFTER the gate, not a reason to skip it.
- **"Installing browser-use or agent-browser is friction"** — the browser-sniff capture reference already documents the install path. Tooling friction is not a valid skip reason. Ask.
- **"The documentation looks thorough enough"** — the decision matrix already handles this case explicitly. If research found that competitors or community projects reference more endpoints than the spec covers, that IS a gap and you MUST ask.
- **"The user said 'let's go' earlier and implicitly approved everything"** — "let's go" at the briefing stage is consent to proceed with research, not standing approval for every future decision. Ask each gate individually.
- **"The default browser-use / agent-browser path got hard-blocked by a WAF, so the only remaining option is to pivot scope or fall back to RSS/docs"** — this is exactly when the chrome-MCP and computer-use fallback options enter, when the runtime exposes them. Step 1 of `../references/browser-sniff-capture.md` detects which fallback MCPs are available; Step 2c.5 composes the recovery menu including those fallbacks; the gate is "ask before giving up," not "auto-pivot when blocked." Do NOT skip the Step 2c.5 menu. Do NOT pivot scope or substitute an alternate target without first asking the user via that menu.

These banned reasons all fired at once in a past combo-CLI run and caused a user-critical source to be silently swapped out. The marker file exists so this cannot happen again. If you find yourself writing a phrase like "skipping browser-sniff because X" where X is one of the above, stop and call `AskUserQuestion`.

### Combo CLIs: per-source enforcement

When the briefing names multiple sources (e.g., "Google Flights + Kayak + FlightAware"), each named source is evaluated independently. The marker file has one entry per source. All entries must be present before [Phase 1.5](08-ecosystem-absorb-gate.md) can proceed.

**Source identification rule:** source names come from the briefing, verbatim. Use the user's exact wording as the `source_name` (normalized to kebab-case is fine: "Kayak /direct" → `kayak-direct`, "Google Flights" → `google-flights`, "FlightAware" → `flightaware`). Do not merge sources. Do not drop one in favor of another.

**Per-source decision flow:**

For each named source, run the "When to offer browser-sniff" decision matrix independently, using the research findings for THAT source. Each source produces its own `AskUserQuestion` call or its own silent-skip marker entry.

**Combo CLI example** (flightgoat pattern — directional guidance, not prescription):

| Source | Spec state | Expected decision |
|--------|------------|-------------------|
| `flightaware` | Documented OpenAPI spec found (53 endpoints, appears complete) | `skip-silent` with reason `spec-complete` |
| `google-flights` | No official spec, but community wrapper exists (`krisukox/google-flights-api`) | Ask via `AskUserQuestion` → record user's answer |
| `kayak-direct` | No spec, no wrapper, user named this as a key feature | Ask via `AskUserQuestion` → record user's answer |

The marker file for this run would contain three entries. [Phase 1.5](08-ecosystem-absorb-gate.md) would HALT if any were missing.

**When the user cares about only one source:** you still ask for all sources that trigger the gate. The user can decline the others. Asking is cheap. Skipping silently breaks the contract.

### Skip this gate entirely when

These are the only cases where Phase 1.7 is bypassed as a whole (not just skipped for one source). Even in these cases, a marker file with a single `skip-silent` entry is written to satisfy [Phase 1.5](08-ecosystem-absorb-gate.md)'s check:

- User passed `--spec` and the spec is the canonical source for every named source → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-spec" }`
- User passed `--har` → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-har" }`
- `BROWSER_SNIFF_TARGET_URL` is set from [Phase 0](03-resolve-and-reuse.md) (user chose "The website itself") → marker: `{ "source_name": "<api>", "decision": "pre-approved", "reason": "phase-0-website-choice" }`, then go directly to "If user approves browser-sniff"

### Direct HTTP challenge rule

If a reachability probe during [Phase 1](04-research-brief.md) research returns bot-protection evidence (`403`, `429`, `cf-mitigated: challenge`, `x-vercel-mitigated: challenge`, `x-vercel-challenge-token`, AWS WAF, DataDome, PerimeterX, CAPTCHA, "Just a moment", "access denied"), **run the no-browser reachability probe before announcing any browser escalation**:

```bash
cli-printing-press probe-reachability "<url>" --json
```

This is non-negotiable. **Do not present transport tiers as a peer menu for the user to choose between.** Phrases like "Browser-sniff + clearance cookie", "Browser-sniff with Surf-only", "Try without browser at all", or "Browser-sniff, prefer Surf" route the user through implementation choices (Surf vs cookie vs full browser) they don't have context to make. The classifier is `probe-reachability`; the agent runs it and decides. Intent-level menus are fine — "Browser-sniff or HOLD?", "Browser-sniff or pick a different API?", or the standard yes/no browser-sniff offers below all ask about goals, not transport, and remain available.

Escalate consent in the order the agent actually needs it, not bundled up-front:

1. **Runtime probe (silent)** — `probe-reachability` runs without prompting. The user already opted into "the website itself" or equivalent in [Phase 0](03-resolve-and-reuse.md); running an HTTP request needs no further consent.
2. **Browser-sniff offer (intent prompt)** — Phase 1.7's normal "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts ask whether to do browser-sniff at all. These are intent-level. Show them when the discovery matrix says to.
3. **Chrome attach (separate consent if escalation happens)** — when the agent actually needs to open or attach to Chrome (because the discovery flow requires a real browser, or because `mode: browser_clearance_http` means the runtime needs cookie capture), surface that as its own moment so the user knows they may need to solve a challenge or sign in. The user-facing prompts at lines below already disclose Chrome attach as a possibility; that is the right place to confirm. Do not pre-announce Chrome attach when the probe has already settled the runtime as `browser_http` and the spec is complete enough to skip discovery — there is no Chrome attach to announce in that path.

Two concerns are decided here, separately:

- **Runtime** (does the printed CLI need browser-compatible HTTP, a clearance cookie, or live page-context execution?) — settled entirely by `probe-reachability`.
- **Discovery** (does Phase 1.7 need to capture XHR traffic via a real browser to learn endpoints?) — settled by Phase 1.7's normal "When to offer browser-sniff" decision matrix above. Independent of runtime.

The probe runs stdlib HTTP, then Surf with a Chrome TLS fingerprint, and emits one of `standard_http | browser_http | browser_clearance_http | unknown`. Apply `mode` to the **runtime** decision:

- **`mode: standard_http`** — runtime is plain HTTP (the original probe was transient). Continue Phase 1.7 normally; the discovery decision is unchanged.
- **`mode: browser_http`** — **runtime is settled: ship Surf transport** (`UsesBrowserHTTPTransport` will be set in the generator's traffic-analysis hints). The printed CLI will not include `auth login --chrome` for clearance cookies — Surf alone clears the challenge. Continue Phase 1.7's discovery decision normally; the existing "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts (above) are framed around endpoint discovery and are correct as-written. Do **not** add clearance-cookie language to those prompts.
- **`mode: browser_clearance_http`** — both probes hit protection signals. The runtime needs more than Surf (clearance cookie or live page-context execution; the probe cannot distinguish), so a real browser capture is required to find out. Proceed through Phase 1.7's normal browser-sniff offer (intent-level yes/no). The consent for Chrome attach happens at the moment the agent actually opens/attaches, where the user-facing prompts in `../references/browser-sniff-capture.md` already disclose what's about to happen and may ask the user to solve a challenge. Note in the brief that runtime is provisionally `browser_clearance_http` pending capture results.
- **`mode: unknown`** — probes failed at the transport layer (DNS/timeout/5xx). Fall through to the existing browser-sniff offer; the user decides whether to retry or pivot.

When browser-sniff is approved or pre-approved AND the probe says `browser_clearance_http` or `unknown`:
- Do **not** offer alternate CLI shapes (RSS-first, official API, docs-only, narrower scope, "try anyway") before a real browser capture has been attempted.
- Do **not** write the brief as if browser-sniff is complete after only curl/direct HTTP probes.
- If browser automation tooling is unavailable, offer the user a manual HAR path before offering any scope pivot.

Only after the browser capture attempt fails by the criteria in `../references/browser-sniff-capture.md` may you ask whether to pivot to RSS, official API, docs-only, or a smaller CLI scope.

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
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=true` | **Offer authenticated browser-sniff** — the user confirmed a session in [Phase 1.6](05-pre-browser-sniff-auth-intelligence.md) |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=false` | Skip — fall back to `--docs` (write marker with `decision: skip-silent`, `reason: login-required-no-session`) |

**Gap detection heuristic:** If [Phase 1](04-research-brief.md) research found documentation, competitor tools, or community projects that reference significantly more endpoints or features than the resolved spec covers, that's a gap signal. Example: "The Zuplo OpenAPI spec has 42 endpoints, but the Public-ESPN-API docs describe 370+."

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

Then read and follow [references/browser-sniff-capture.md](../references/browser-sniff-capture.md) for the complete
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

Every source named in the briefing must have exactly one entry in `browser-browser-sniff-gate.json`. Before proceeding to [Phase 1.8](07-crowd-sniff-gate.md), re-read the marker file and verify the count matches the number of named sources from the briefing. If a source is missing, return to the decision matrix for that source. [Phase 1.5](08-ecosystem-absorb-gate.md) will HALT if this check fails.

---


Next: phases/07-crowd-sniff-gate.md
