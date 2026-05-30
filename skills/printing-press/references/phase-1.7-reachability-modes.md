# Phase 1.7: Reachability-mode classification & time budget

Lazy-loaded body for the Direct HTTP challenge rule (probe-reachability runtime-mode classification) and the browser-sniff time budget. Read and apply when a Phase 1 reachability probe returns bot-protection evidence, or before starting a browser-sniff capture. The marker WRITE contract, banned-skip reasons, per-source rule, and exit count-check stay in SKILL.md.

### Direct HTTP challenge rule

If a reachability probe during Phase 1 research returns bot-protection evidence (`403`, `429`, `cf-mitigated: challenge`, `x-vercel-mitigated: challenge`, `x-vercel-challenge-token`, AWS WAF, DataDome, PerimeterX, CAPTCHA, "Just a moment", "access denied"), **run the no-browser reachability probe before announcing any browser escalation**:

```bash
cli-printing-press probe-reachability "<url>" --json
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
