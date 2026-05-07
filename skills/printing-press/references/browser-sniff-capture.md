# Browser-Sniff Capture Implementation

> **When to read:** This file is referenced by Phase 1.7 of the printing-press skill.
> Read it when the user approves temporary browser discovery (browser-use, agent-browser, or manual HAR capture of live site traffic).
>
> **Context:** This file documents what happens AFTER Phase 1.7 decides to browser-sniff. The decision itself — approve, decline, or silent-skip — is recorded in `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` by Phase 1.7 before this reference is loaded. Phase 1.5 refuses to proceed without that marker file. See SKILL.md Phase 1.7 "Enforcement: the browser-browser-sniff-gate.json marker file" for the contract.
>
> Browser discovery is a temporary generation-time aid. It exists to learn URLs, methods, request bodies, persisted GraphQL hashes, BFF envelopes, auth/header construction, response shapes, and replayability. It is not permission to generate a printed CLI that keeps a browser open for normal commands.

### Cardinal Rules

1. **Prefer browser-use CLI mode for capture, but keep valid fallbacks.** Use browser-use CLI mode when available because it gives stable open/eval/scroll control and response interception without an LLM key. If browser-use is unavailable or incompatible, the valid fallback set is: `agent-browser` (when it can produce equivalent network capture artifacts), the Claude chrome-MCP (`mcp__claude-in-chrome__*` — drives the user's existing Chrome session via the browser extension; see Step 2e), computer-use as a visual-feedback aid for the manual-HAR flow (`mcp__computer-use__*` — read-only on browsers, used to take screenshots and guide the user; see Step 1d's manual-HAR expansion), or asking the user for a manual DevTools HAR. Do NOT substitute curl probing, JS bundle grepping, or agent-browser auto-connect alone for an approved browser capture. Detection of which fallbacks are actually available happens in Step 1; menu composition for failure-recovery selection is in Step 2c.5. Rule 5 (replayability) applies to all of these — fallback shape does not relax the success criterion.

2. **Do NOT skip auth discovery when the session expires.** *(Only applies when `AUTH_SESSION_AVAILABLE=true` — the user confirmed they're logged in.)* If a Chrome profile loads but the session has expired (login page visible instead of account page), offer headed login as a fallback. Never proceed without auth just because the profile session was stale. For anonymous sniffs (no auth context), this rule does not apply.

3. **Use click-based SPA navigation after installing interceptors.** `browser-use open` triggers a full page reload which resets the JS context and destroys fetch/XHR interceptors. After installing interceptors, navigate by clicking links (`browser-use eval "document.querySelector('a[href*=account]').click()"` or `browser-use click`). Only use `browser-use open` for the first page load or when you need to re-install interceptors.

4. **Run `printing-press probe-reachability` before announcing any browser escalation, and don't expose transport tiers to the user as peer choices.** If research or preflight saw Cloudflare/Vercel/WAF/DataDome/PerimeterX/CAPTCHA evidence, the *first* action is the no-browser probe — not a Chrome-attach prompt and not a transport-tier menu like "Browser-sniff + clearance cookie / Browser-sniff Surf-only / HAR / Hold". Intent menus are fine (yes/no, browser-sniff or pivot, etc.); the wrong shape is forcing the user to pick between Surf vs cookie vs full browser, which is the classifier's job. Many passive challenges (Vercel TLS-fingerprint mitigation, lighter Cloudflare gates) clear with Surf alone, no cookie, no setup. If `probe-reachability` returns `mode: browser_http`, the printed CLI will ship Surf transport with zero clearance-cookie capture — runtime is settled silently. (Browser-sniff for endpoint *discovery* is a separate decision handled by Phase 1.7's normal matrix; if that matrix says to ask, the existing intent-level prompts already disclose Chrome attach as a possibility — that's the right place for that consent, not bundled into a transport-tier menu.) Only when the probe returns `browser_clearance_http` or `unknown` should you tell the user direct HTTP is blocked and proceed with a real browser capture. Do NOT replace the target with RSS/docs/official API or ask for a smaller CLI shape until after browser capture has failed by the criteria below.

5. **Replayability is the success criterion.** A browser capture succeeds only when it produces a shippable surface: replayable API calls, persisted-query registry entries, browser-clearance cookies that can be imported and replayed, or structured HTML/SSR/RSS/JSON-LD extraction targets. If the only observed path requires live page-context execution, report HOLD or return to discovery for a lighter surface. Do not continue as if resident browser transport is acceptable.

### If user approves browser-sniff

#### Browser-Sniff Pacing

When making API calls during browser-sniff (browser-use eval, fetch, or direct HTTP requests), apply adaptive pacing to avoid rate limits:

1. **Start conservative**: Wait 1 second between API calls
2. **Ramp up on success**: After 5 consecutive successful calls, reduce the delay by 20% (minimum 0.3 seconds)
3. **Back off on 429**: If you get a rate-limited response (HTTP 429), immediately double the delay and log: "Rate limited — increasing delay to Xs"
4. **Hard stop on repeated 429s**: If you hit 3 consecutive 429s, pause for 30 seconds before continuing
5. **Never abort**: Rate limiting during browser-sniff is recoverable. Always continue after the backoff — do not abort discovery due to rate limits

Track the current delay mentally. Report the effective rate when summarizing browser-sniff results: "Sniffed N endpoints at ~X req/s effective rate."

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
# Fall back to agent-browser only if it can provide equivalent network capture artifacts.
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

If a tool is found, report: "Using **<tool>** for temporary traffic capture during generation (CLI-driven mode — no LLM key needed)." and proceed to Step 1c to verify compatibility.

**Important:** browser-use has two modes: autonomous Agent mode (requires an LLM API key like ANTHROPIC_API_KEY) and CLI mode (open/eval/scroll — no key needed). **Always use CLI mode for browser-sniff.** It is more reliable, version-stable, and does not require the user to provide an additional API key. Do NOT attempt to use browser-use's Python `Agent` class — it requires an LLM key that may not be available.

**Also detect: optional MCP-driven fallback backends.** Two additional capture options exist when the runtime exposes them — they enter only on failure-recovery in Step 2c.5 or as opt-in choices in Step 1b's install picker, never as defaults. Detection is **agent-prose, not shell-probe** — the agent inspects its own available and deferred tool lists (visible in system reminders / the deferred-tool block / `ToolSearch` catalog) and asserts the flags inline in its reasoning. Bash blocks cannot read the agent's own tool registry; do not write `command -v mcp__claude-in-chrome__` style probes.

> **Inspect your tool catalog now and assert these flags in your reasoning, not as shell vars.** They are read by Step 2c.5 (recovery menu composition) and Step 2e (chrome-MCP capture playbook) later in this same conversation turn.
>
> - If `mcp__claude-in-chrome__*` tools appear in your available or deferred tool list, set `CHROME_MCP_AVAILABLE=true`. Otherwise `false`.
> - If `mcp__computer-use__*` tools appear in your available or deferred tool list, set `COMPUTER_USE_AVAILABLE=true`. Otherwise `false`.
> - The probe is intentionally cheap — actual schema loading via `ToolSearch` is deferred until the user picks the option from a menu.
> - On platforms that don't expose a deferred-tool list (non-Claude-Code targets running this skill via plugin install), the agent observes neither MCP and both flags default to `false`. Downstream behavior is unchanged from today's.

If either MCP flag is true, extend the status report:

> "Using **<tool>** for traffic capture. Fallbacks available: chrome-MCP, computer-use." (list whichever flags are true)

#### Step 1b: Install capture tool (if none found)

If neither tool is installed, offer to install via `AskUserQuestion`. Do not install automatically:

> "No browser automation tool found. I need one to temporarily inspect the live site during generation. Which would you like to install?"
>
> Options:
> 1. **Install browser-use (Recommended)** — "CLI-driven browser automation for generation-time capture. Requires Python."
> 2. **Install agent-browser** — "Alternative capture backend when it can provide network artifacts. Requires Node.js."
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

After detection (Step 1) or installation (Step 1b), verify the installed version supports the CLI commands the browser-sniff process needs.

**For browser-use** — The CLI 2.0 commands (`open`, `eval`, `scroll`, `close`) all shipped in **v0.12.3**. Versions before that have an incomplete or experimental CLI that won't work for browser-sniff.

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

> "Found **<tool>** v<version>, but browser-sniff requires v<min-version>+ for CLI capture commands. Would you like to upgrade?"
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

If the tool passes the version check, proceed to Step 1d (if authenticated browser-sniff) or Step 2a/2b (if anonymous browser-sniff).

#### Step 1d: Session Transfer (authenticated browser-sniff only)

This step only runs when the user chose "authenticated browser-sniff" (from Phase 1.7's sniff-as-primary or sniff-as-enrichment prompts, or when `AUTH_SESSION_AVAILABLE=true` and the user confirmed).

**Situation detection:**
```bash
CHROME_RUNNING=false
if pgrep -x "Google Chrome" >/dev/null 2>&1; then
  CHROME_RUNNING=true
fi
```

**When Chrome IS running**, use agent-browser to grab cookies, then ask the user to quit Chrome so browser-use can load the profile for capture:

Present via `AskUserQuestion`:
> "Chrome is running. I'll grab your cookies, then need you to quit Chrome so I can browser-sniff with full page access."
>
> 1. **Grab session, then quit Chrome** (Recommended) — "I save your cookies via agent-browser, you quit Chrome, then I browser-sniff with browser-use using your profile. Full DOM access."
> 2. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I browser-sniff."
> 3. **I'll export a HAR file** — "You browse the site in DevTools and export the HAR. I use it for discovery, then keep only surfaces that pass lightweight replayability checks."

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

For option 1 after cookies are saved and Chrome is quit:
```bash
# Start browser-use with the Chrome profile (has all saved cookies/logins)
browser-use --profile "Default" open <url>
```

**When Chrome is NOT running**, prefer browser-use (loads real Chrome profile with all cookies):

Present via `AskUserQuestion`:
> "Chrome isn't running. I can load your Chrome profile directly — all your saved logins will be available."
>
> 1. **Use your Chrome profile** (Recommended, requires browser-use) — "Loads your real Chrome profile. Zero setup."
> 2. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I browser-sniff."
> 3. **I'll export a HAR file** — "I use the HAR for discovery, then keep only replayable HTTP/HTML/RSS/SSR/API surfaces in the printed CLI."

For option 1 (browser-use profile reuse):
```bash
browser-use open <url> --profile "Default"
```
If browser-use is not available, fall back to agent-browser headed login.

If Chrome profile lock error occurs (Chrome is actually running): "Chrome's profile is locked. Quit Chrome first, or switch to option 2."

**Session transfer vs capture are separate concerns.** Use agent-browser for session transfer only (grabbing cookies from a running Chrome). Always use browser-use for the actual capture (Steps 2a.*) because it has full DOM access via eval, scroll, click, and snapshot. Agent-browser's auto-connect mode cannot access the DOM or run eval — it can navigate and record HAR but cannot interact with pages.

Recommended flow when Chrome IS running:
1. Use agent-browser `--auto-connect state save` to grab cookies
2. Close agent-browser daemon
3. Ask user to quit Chrome
4. Start browser-use `--profile "Default"` for capture (loads the same cookies via the Chrome profile)

When Chrome is NOT running:
- Use browser-use `--profile "Default"` directly for both session and capture

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

**For HAR export (option 3):** Guide the user through the DevTools HAR-export flow. Make clear that a HAR is discovery input, not a promise that every captured HTML/XHR route becomes a printed CLI command. After analyzing the HAR, keep only surfaces that replay through lightweight HTTP/Surf/browser-compatible HTTP, browser-clearance cookie import plus replay, or structured HTML/SSR/RSS extraction. If the HAR only proves live page-context execution works, HOLD or pivot scope.

**Chrome 147+ DevTools HAR export — concrete instructions.** Recent Chrome versions (147+) removed "Save all as HAR with content" from the right-click menu in the Network panel. The download-arrow icon at the top of the Network panel is now the only stable export path. Walk the user through these steps in order — they are the steps a user got stuck on in a recent session, so the language is deliberately literal:

1. **Open DevTools.** `Cmd+Option+I` (macOS) or `Ctrl+Shift+I` (Windows/Linux). If DevTools is already open but on the wrong tab, the next step covers it.
2. **Switch to the Network panel.** It is in the top tab strip alongside Elements, Console, Sources, Performance. If DevTools is narrow, the Network tab may be hidden behind a `>>` overflow chevron at the right end of the tab strip — click `>>` and pick **Network**. Do not pick "Recorder" — that is a different panel.
3. **Confirm recording is on.** A red dot at the top-left of the Network panel means recording is on. If it is gray/black, click it once to enable.
4. **Check "Preserve log" and "Disable cache"** — both are checkboxes in the Network panel toolbar. Preserve log keeps records across navigations; disable cache forces fresh requests so the HAR contains real network activity.
5. **Clear any prior requests.** Click the 🚫 (clear / no-entry) icon in the toolbar to start with an empty log.
6. **Reproduce the user flow on the target site** — navigate, click into the section the printed CLI needs, scroll, interact. Wait for network activity to settle between actions.
7. **Export the HAR.** Click the **download-arrow icon** at the top-left of the Network panel toolbar (between the upload-arrow icon `↑` and the record/clear icons — it looks like a `↓` arrow with a horizontal bar underneath). A macOS/Windows save dialog opens. Save as `<api>-capture.har` somewhere accessible like `~/Downloads/`.
8. **Tell the agent the path.** The agent runs `printing-press browser-sniff --har <path>` next.

**Computer-use visual-feedback-loop (only when `COMPUTER_USE_AVAILABLE=true`).** Computer-use cannot click or type into Chrome (browsers are tier-"read" — visible in screenshots, but input is blocked). Its value here is closing the loop with the user when text instructions get them stuck. Pattern:

1. **Before screenshotting, instruct the user to collapse the Network panel detail pane** so only the request list is visible. The detail pane shows full request and response headers including `Authorization` and `Cookie`; collapsing it before each screenshot keeps credentials out of the captured PNG. (Click the `×` on any open request detail, or click in the request list to deselect.)
2. **Take a screenshot via `mcp__computer-use__screenshot`** at each instruction checkpoint (after step 1, after step 2, after step 7).
3. **Display the screenshot inline AND describe what the agent sees in 1-2 sentences.** This is mandatory — silent storage helps no one. Pattern: `Read` the saved PNG path so the image shows in the response, then say something like "I see your DevTools is on the Recorder tab, not Network — click `>>` in the tab strip and pick Network." The screenshot becomes part of the agent's reasoning AND the user-facing feedback.
4. **Save screenshots to `$DISCOVERY_DIR/devtools-help-*.png`** during the session so they're available for inline display and so manuscripts archiving can clean them up.
5. **Phase 5.5 cleanup:** the archive-time cleanup must explicitly delete `$DISCOVERY_DIR/devtools-help-*.png` — these are ephemeral debug aids, not durable artifacts, and the text-based credential scrubber in `secret-protection.md` cannot redact PNG contents. Make sure the `cleanup` block in the run wrap-up includes `rm -f "$DISCOVERY_DIR"/devtools-help-*.png 2>/dev/null` or equivalent.

If `COMPUTER_USE_AVAILABLE=false`, the text-only instructions above stand on their own. Do not skip them just because computer-use isn't available — the click path itself is the load-bearing fix.

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

> "Session transfer failed — no `<target-domain>` cookies found in the browser. The browser-sniff would run unauthenticated."
>
> 1. **Log in manually** — "I'll open a headed browser. You log in, then I browser-sniff."
> 2. **Continue without auth** — "Browser-Sniff only public endpoints"
> 3. **Provide HAR manually** — "Export a HAR yourself from browser DevTools"

**After loading a Chrome profile**, also verify the session is actually active on the target site. Cookies may exist but be expired:

```bash
# Navigate to the site and check for login indicators
browser-use eval "var login=document.querySelector('a[href*=login],a[href*=signin],[class*=sign-in],[class*=login-btn]');
var account=document.querySelector('a[href*=account],a[href*=profile],[class*=logged-in],[class*=user-menu]');
login && !account ? 'SESSION_EXPIRED' : account ? 'SESSION_ACTIVE' : 'UNKNOWN'"
```

If the result is `SESSION_EXPIRED` (login link visible, no account link), the profile cookies have expired. Present via `AskUserQuestion`:

> "Your browser session for `<site>` has expired (login page visible). I need a fresh login to discover authenticated endpoints."
>
> 1. **Open headed browser to log in** (Recommended) — "I'll open a visible browser. You log in, then I continue the browser-sniff."
> 2. **Continue without auth** — "Browser-Sniff only public endpoints"

Do NOT silently proceed without auth when the session has expired. The authenticated surface is often the most valuable part of the API (order history, rewards, saved data).

If cookies are verified, proceed to Steps 2a/2b capture flow with the authenticated session loaded. The session state file is stored at `$DISCOVERY_DIR/session-state.json`.

#### Step 2a.0: Direct-API-probe fallback (try before browser-use when WAF-protected)

When `probe-reachability` returns `mode: browser_http` (Cloudflare, Vercel, or another WAF challenge in front of the site), browser-use can still get blocked at runtime even though `surf` would clear the challenge. Before launching browser-use, attempt a direct curl probe with a Chrome User-Agent to see whether the site's API surface answers without a resident browser.

This is most likely to succeed against:

- Sites with a discoverable proxy-envelope endpoint (a single internal route such as `/_api/ws/proxy`, `/api/graphql`, or `/internal/rpc` that fronts the public API).
- Sites whose public API responds to a Chrome UA without JS challenge (the WAF gates HTML pages but exempts `/api/*` paths).

Probe pattern:

```bash
# Pick a candidate endpoint surfaced by the Phase 1 research brief or
# the proxy-envelope detection step above.
PROBE_URL='https://<site>/<candidate-api-path>'

curl -s -o /tmp/probe-response.json -w '%{http_code}\n' \
  -A 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36' \
  -H 'Accept: application/json' \
  "$PROBE_URL"
```

Decision criteria:

- **HTTP 200 with structured JSON** — the direct probe is viable. Capture a handful of representative endpoint responses to `$DISCOVERY_DIR/direct-probe-*.json`, then proceed to Step 2a or fall through to Step 2b (manual HAR via DevTools) for the structured browser-sniff capture. **Do not skip the structured capture** — `printing-press browser-sniff` needs a HAR or enriched-capture JSON, not loose curl responses, and the replayability check still has to run against the captured envelope.
- **HTTP 403/429 with a Cloudflare challenge body** (`<title>Just a moment...</title>`, `cf_chl_opt`, Vercel/Akamai equivalents) — the WAF is blocking direct probes. Fall through to browser-use; the captured surface will need Surf transport and possibly a clearance-cookie step.
- **HTTP 401/403 with a structured auth error** (`{"error":"unauthenticated"}`, `WWW-Authenticate: Bearer`) — direct probing works but the path is auth-only. Document the path in the brief and route to the authenticated-flow capture.

The replayability check still applies regardless of probe outcome: any endpoint discovered through direct probing must round-trip through `surf` with the same Chrome TLS fingerprint the printed CLI will ship, or the captured URLs are unusable in production.

Example: a public read-only catalog site fronted by Cloudflare exposes `/_api/ws/proxy` (the internal proxy-envelope) and answers a Chrome-UA `POST` with the right service/method/path body. The direct probe confirmed the envelope shape and request fields in seconds; the structured browser-sniff still ran via DevTools HAR to capture the full set of paths the proxy fronts.

#### Step 2a: browser-use CLI capture (preferred)

Claude drives browser-use directly via CLI commands — no LLM key needed, no Python API versioning issues. Uses the browser's native Performance API to collect API endpoint URLs from each page.

**IMPORTANT: Run the page collection loop in foreground, not background.** The loop takes ~60-90 seconds for 10-15 pages. Background execution has unreliable output capture for shell functions that call browser-use. Always run this inline.

##### browser-use eval returns synchronous values only

The `browser-use eval <expr>` CLI returns only synchronous JSON-serializable values. Any expression whose last value is a Promise returns `None`, even though the Promise may fire correctly in the background. This is a common trap when probing authenticated endpoints via inline `fetch()` from the page context.

**Wrong (returns `None` before the Promise resolves):**
```bash
browser-use eval "fetch('/api/cookbook', {credentials:'include'}).then(r => r.text())"
# result: None  (looks like the fetch failed, but it didn't)
```

**Right (kick off the fetch, store the result in a window var, read back on a later eval):**
```bash
# Step 1: kick off
browser-use eval "window.__probe = 'pending'; fetch('/api/cookbook', {credentials:'include'}).then(r => r.text()).then(t => { window.__probe = t.slice(0, 3000); }).catch(e => { window.__probe = 'ERR: ' + e; }); 'kicked'"

# Step 2: read back (can be immediate if you just want to confirm the interceptor ran;
# do one or two more evals as no-ops to let the Promise resolve)
browser-use eval "1"
browser-use eval "window.__probe"
```

The pattern is safe because subsequent `eval` calls run in the same JS context — browser-use keeps the tab open — so `window` state persists between calls.

When the stored value reads as `"pending"`, the Promise hasn't resolved yet. Explicitly handle the error case (`.catch(e => window.__probe = 'ERR: ' + e)`) so you can distinguish "in flight" from "errored".

For XHR-based interceptor captures, the same pattern applies: install the interceptor once, then issue the fetch without consuming the Promise in the eval return value. Read accumulated logs from `window.__apilog` on subsequent calls.

**Step 2a.1: Build the user flow plan**

From the primary browser-sniff goal (Step 0 in the SKILL.md), derive the interactive steps a real user would take to accomplish that goal. This is NOT a list of pages to load -- it is a sequence of actions.

Example for "Order a pizza for delivery" (Domino's):
1. Click "Delivery" on homepage
2. Enter a delivery address, click "Continue"
3. Confirm a store from the results
4. Browse the menu (click a category like "Specialty Pizzas")
5. Add an item to cart (click "Add to Order")
6. View cart (click cart icon)
7. Proceed toward checkout (STOP before entering payment)

Example for "Create an issue" (Linear):
1. Click "New Issue" from the sidebar
2. Fill in title and description
3. Assign to a team/project
4. Set priority and labels
5. Submit (or preview if dry-run)

Example for "Check today's scores" (ESPN):
1. Load the homepage (scores are front-page content for read-heavy sites)
2. Click a sport (NFL, NBA, etc.)
3. Click a specific game for the box score / play-by-play
4. Click standings
5. Click a team for the team page

Each step triggers API calls that page loads alone would miss. After the primary flow, add 1-2 secondary flows from the research brief's other top workflows (e.g., "Check rewards," "Track an order").

**Step 2a.1.5: Authenticated flow (when `AUTH_SESSION_AVAILABLE=true`)**

When the user confirmed a logged-in session (AUTH_SESSION_AVAILABLE=true from Phase 1.6), add authenticated page visits AFTER the primary flow. The primary flow discovers the public API surface; the authenticated flow discovers what's behind the login wall.

1. **Record the public endpoint set.** Before visiting auth pages, note which endpoints have been discovered so far. These are the "public set" — reachable without session cookies.

2. **Visit account/profile pages.** Navigate to common authenticated URLs. Try these patterns in order, stopping at the first that loads a real page (not a redirect to login):
   - `/account`, `/my-account`, `/profile`, `/settings`
   - `/orders`, `/order-history`, `/my-orders`
   - `/rewards`, `/loyalty`, `/my-deals-and-rewards`
   - `/addresses`, `/saved-addresses`, `/payment-methods`

   Also derive page URLs from the research brief's top workflows. If the brief mentions "order history" or "rewards" or "saved addresses," visit the corresponding pages even if they don't match the common patterns above.

3. **Interact with auth pages.** Apply the SPA interaction rule below — click tabs, expand sections, trigger lazy loads. Auth pages often have sub-sections (e.g., "Recent Orders" tab, "Rewards History" tab) that fire separate API calls.

4. **Classify endpoints.** After visiting auth pages, compare the new endpoints against the public set:
   - Endpoints that appear ONLY during auth page visits → classify as **auth-required**
   - Endpoints that appear in both public and auth visits → classify as **public**
   - Record the classification in the discovery report's Endpoints table (add an "Auth" column)

5. **Discover the auth header pattern.** Many SPAs don't send cookies directly — they read tokens from cookies/localStorage and construct an `Authorization` header. Install an XHR header interceptor and trigger a client-side navigation (click a link — do NOT use `browser-use open`) to capture the actual request headers:

   ```bash
   # Install header interceptor
   browser-use eval "window.__authHeaders={};
   const _s=XMLHttpRequest.prototype.setRequestHeader;
   XMLHttpRequest.prototype.setRequestHeader=function(k,v){
     if(k.toLowerCase()==='authorization')window.__authHeaders[k]=v.substring(0,100);
     _s.apply(this,arguments)};'OK'"

   # Navigate via SPA click (preserves interceptor)
   browser-use eval "document.querySelector('a[href*=account],a[href*=orders],a[href*=rewards]').click()"
   sleep 3

   # Collect captured auth headers
   browser-use eval "JSON.stringify(window.__authHeaders)"
   ```

   **If an Authorization header is found:**
   - Record the scheme (e.g., `Bearer`, `CustomerAuth`, `Token`, custom)
   - **Trace values back to cookies.** Read `document.cookie` and match literal values from the captured header against cookie values:
     ```bash
     browser-use eval "document.cookie"
     ```
     For each cookie `name=value`, check if `value` appears as a substring in the Authorization header. When a match is found, record the cookie name and which part of the header it corresponds to.
   - **Construct the format string.** Replace each literal cookie value in the header with `{cookieName}`:
     - Example: header `CustomerAuth 2432962|FD44DA6A-...`, cookies `customerId=2432962; authToken=FD44DA6A-...`
     - Format string: `CustomerAuth {customerId}|{authToken}`
   - **Write composed auth into the spec.** When building the spec YAML, include:
     ```yaml
     auth:
       type: composed
       header: Authorization
       format: "<format string with {cookieName} placeholders>"
       cookie_domain: <site domain>
       cookies:
         - <cookie1Name>
         - <cookie2Name>
     ```
     This tells the generator to emit `auth login --chrome` that reads those specific cookies and composes the header. The user never sees the format — the CLI handles it.
   - Also check localStorage for token sources:
     ```bash
     browser-use eval "JSON.stringify(Object.keys(localStorage).filter(k=>k.match(/token|auth|session/i)))"
     ```
   - Use the composed header in Step 2d validation (not cookie replay — construct the actual header from extracted cookies)
   - Record the auth scheme in the discovery report

   **If cookie matching fails** (header values don't match any cookie values — possibly URL-encoded or hashed), fall back to recording the auth scheme without composed config. The printed CLI will use generic token auth. Report: "Auth header discovered but could not trace values to cookies."

   **If no Authorization header found** but auth endpoints returned data (from step 4), the API likely uses cookie-based auth directly. Write `auth.type: cookie` into the spec and proceed to Step 2d with cookie replay.

   **If the interceptor captured nothing** (page didn't fire API calls), try clicking a different link or scrolling the page. If still nothing after 2 attempts, proceed to Step 2d with cookie replay as a fallback.

6. **Trigger auth validation.** If any auth-required endpoints were found, Step 2d (Cookie/token auth validation) MUST run. Use whichever auth method was discovered in step 5:
   - If an Authorization header was found → replay with that header
   - If no header found → try cookie replay
   This is what propagates `Auth.Type` and auth config into the spec.

7. **If auth pages redirect to login.** The session may have expired between the time the user confirmed login and the browser-sniff reaches this step. Report: "Auth pages redirected to login — session may have expired. Auth-only endpoints not discovered." Do NOT fail the browser-sniff — the public endpoints are still valid. Proceed to Step 2a.2 with the public set only.

**SPA interaction rule:** On each page/state, take a snapshot first. Look for interactive elements (buttons, forms, dropdowns, tabs). Click through them. SPAs fire API calls on interaction, not on page load. If you load a page and see no XHR activity, that means you need to interact with the page, not that there is nothing to find.

**SPA navigation rule:** After installing fetch/XHR interceptors, do NOT use `browser-use open` to navigate between pages — it triggers a full page reload which destroys the interceptors. Instead, navigate by clicking links:
```bash
# Good: SPA navigation preserves interceptors
browser-use eval "document.querySelector('a[href*=\"/orders\"]').click()"
# or
browser-use click "Orders"

# Bad: full reload destroys interceptors
browser-use open "https://site.com/orders"
```
Only use `browser-use open` for the initial page load (before interceptors exist) or when you intentionally want to re-install interceptors on a fresh page.

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

# Early interactive-challenge check. If this finds Cloudflare/Vercel/WAF
# challenge assets or a challenge title/body, stop the capture attempt and
# route to the challenge-only recovery prompt below instead of burning the
# browser-sniff time budget waiting for Playwright to auto-solve it.
browser-use eval "var urls=performance.getEntriesByType('resource').map(e=>e.name).join('\n');var text=(document.title+' '+document.body.innerText+' '+document.documentElement.innerHTML).toLowerCase();JSON.stringify({challenge: urls.includes('/cdn-cgi/challenge-platform') || text.includes('just a moment') || text.includes('cf-turnstile') || text.includes('x-vercel-challenge') || text.includes('captcha'), title: document.title});"

# Apply browser-sniff pacing delay (starting at 1s, adapts per Browser-Sniff Pacing rules above)
browser-use scroll down  # Trigger lazy-loaded content
sleep 1
# Apply browser-sniff pacing delay before next eval call

# Collect API URLs via Performance API (browser-native, no injection needed)
browser-use eval "var e=performance.getEntriesByType('resource');var u=[];for(var i=0;i<e.length;i++){var n=e[i].name;if(n.indexOf('<api-domain-1>')>-1||n.indexOf('<api-domain-2>')>-1)u.push(n);}u.join('|||');"

# Parse the result and append to collection file
# The eval output is "result: url1|||url2|||url3"
# Split on ||| and append each URL to the file
```

Replace `<api-domain-1>`, `<api-domain-2>` etc. with the API domains discovered in Phase 1 research (e.g., `api.espn.com`, `sports.core.api`, `site.web.api`).

**Why Performance API:** It is built into every browser, captures all resource loads (including those that fire before any JS interceptor could be injected), survives within a page lifecycle, and returns simple URL strings. Do NOT use `fetch`/`XMLHttpRequest` monkey-patching — it breaks on page navigation.

**Step 2a.2.5: GraphQL BFF detection**

After collecting URLs, check whether the site uses a GraphQL BFF pattern. This is common in modern SPAs (Domino's, Notion, Shopify storefronts) where all API traffic goes through a single `/graphql` or `/api/graphql` endpoint.

**Detection signal:** If >50% of captured XHR/fetch POST URLs resolve to the same path (e.g., `/api/web-bff/graphql`, `/graphql`, `/api/graphql`), classify as a GraphQL BFF.

**If GraphQL BFF detected:**

1. **Extract operation names from POST bodies.** The URL alone tells you nothing — all calls go to the same endpoint. The value is in the request bodies.

   For agent-browser:
   ```bash
   # List all XHR requests
   agent-browser network requests --type xhr --json
   # For each POST to the GraphQL endpoint, get the full request including body:
   agent-browser network request <request-id> --json
   # Parse: look for operationName and query fields in the request body
   ```

   For browser-use: inject a fetch interceptor BEFORE browsing auth/interaction pages. This captures POST bodies that the Performance API misses:
   ```bash
   browser-use eval "window.__gqlOps=[];const _f=window.fetch;window.fetch=async function(){const r=await _f.apply(this,arguments);try{if(arguments[0]&&arguments[0].toString().includes('graphql')&&arguments[1]&&arguments[1].body){const b=JSON.parse(arguments[1].body);if(b.operationName)window.__gqlOps.push({op:b.operationName,vars:Object.keys(b.variables||{})})}}catch(e){}return r}"
   ```
   After browsing, collect:
   ```bash
   browser-use eval "JSON.stringify(window.__gqlOps)"
   ```

2. **Record operations.** For each unique `operationName`, record:
   - Operation name (e.g., `GetStoreMenu`, `AddToCart`, `GetOrderHistory`)
   - Type: query (read) or mutation (write) — infer from the `query` field prefix or from naming convention (`Get*` = query, `Add*`/`Create*`/`Update*`/`Delete*` = mutation)
   - Variable keys (e.g., `storeId`, `productCode`) — these become CLI flags
   - Domain group — group by prefix (e.g., `Store*`, `Menu*`, `Order*`, `Account*`)

3. **Write to discovery report.** Replace (or supplement) the "Endpoints Discovered" table with a "GraphQL Operations" table:
   ```
   | Operation | Type | Variables | Domain |
   |-----------|------|-----------|--------|
   | GetStoreMenu | query | storeId, lang | Store |
   | AddToCart | mutation | productCode, qty | Order |
   ```

4. **Feed into spec building.** When building the OpenAPI spec from discovered operations, each GraphQL operation becomes a spec path: `POST /graphql#OperationName`. The operation name goes in `operationId`. Variables become request body properties. This is compatible with the existing generator — it sees each operation as a distinct POST endpoint.

**If NOT a GraphQL BFF:** Skip this step. The existing URL-based discovery flow handles REST APIs.

**Step 2a.2.7: JS bundle endpoint extraction (supplementary)**

SPA frameworks (Angular, React, Vue, Next.js) compile all API endpoint paths into their main JS bundle. Extracting these paths supplements the browser-sniff with endpoints that no user flow visits (admin features, migration tools, rarely-used settings).

**When to run:** After completing the interactive browser-sniff (Steps 2a.1–2a.2.5). This is supplementary — the browser-sniff is primary because it provides response shapes, auth patterns, and parameter types. Bundle extraction only gives endpoint paths.

**Skip when:** The site is server-rendered HTML without JS bundles, or the browser-sniff already discovered 20+ endpoints and the API surface appears complete.

1. **Find the main bundle:**
   ```bash
   browser-use eval "Array.from(document.querySelectorAll('script[src]')).map(s=>s.src).filter(s=>s.includes(location.hostname)&&(s.includes('main')||s.includes('app'))).join('\\n')"
   ```

2. **Download and extract API paths:**
   ```bash
   curl -s "<bundle-url>" | python3 -c "
   import sys, re
   content = sys.stdin.read()
   
   # Find the API base URL config (common patterns)
   base_match = re.search(r'(apiUrl|baseUrl|API_URL)[^\"]*\"(https?://[^\"]+)\"', content)
   if base_match:
       print(f'API base: {base_match.group(2)}')
   
   # Extract capitalized path segments (API routes)
   paths = re.findall(r'\"(/[A-Z][a-zA-Z]+(?:/[A-Z]?[a-zA-Z]*)*)\"|\"(/[a-z]+/[a-zA-Z]+)\"', content)
   unique = sorted(set(p[0] or p[1] for p in paths if p[0] or p[1]))
   for p in unique:
       print(f'  {p}')
   
   # Extract HTTP method calls
   calls = re.findall(r'\.(get|post|put|delete|patch)\([^)]*\"(/[A-Za-z][A-Za-z0-9/\${}]+)', content)
   for method, path in sorted(set(calls)):
       print(f'  {method.upper()} {path}')
   "
   ```

3. **Merge with browser-sniff results.** Append bundle-discovered endpoints to `$SNIFF_URLS`. Mark their provenance:
   ```bash
   # Append bundle-only endpoints (not already in sniff-urls.txt)
   # In the discovery report, mark these as "discovered: bundle"
   ```

4. **Record API config.** If the bundle reveals useful config (API version headers, auth token construction, rate limit hints), note them in the discovery report's Browser-Sniff Configuration section.

**Step 2a.3: Deduplicate and normalize**

After collecting from all pages:
```bash
# Strip query parameters and deduplicate to find unique API path patterns
cat "$SNIFF_URLS" | sed 's/\?.*//' | sort -u > "$DISCOVERY_DIR/browser-sniff-unique-paths.txt"
```

**Step 2a.4: Generate enriched capture**

The Performance API gives us URLs but not response bodies. To feed `printing-press browser-sniff`, we need to call each unique API endpoint and capture the response:

```bash
# For each unique API URL, fetch it and build a simple capture file
# printing-press browser-sniff accepts HAR or enriched capture JSON
# When fetching each unique API URL to build enriched capture:
# Apply browser-sniff pacing between requests (1s initial, adaptive per Browser-Sniff Pacing rules)
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

2. **Walk the user flow** using the snapshot-reason-act loop:
   - Use the user flow plan from Step 2a.1 (same flow applies to both backends)
   - For each step in the flow:
     - `agent-browser snapshot -i` to see the current page state
     - Find the interactive element for this step (button, form, link, dropdown)
     - Click/fill/submit it
     - `agent-browser wait --network-idle` after each interaction
     - Apply browser-sniff pacing between interactions (1s initial, adaptive per Browser-Sniff Pacing rules)
   - After completing the primary flow, run 1-2 secondary flows
   - Skip: navigation links, footer links, social media buttons, cookie/consent banners
   - Fill forms with realistic sample data based on the domain (real-looking addresses, names, etc.)

3. **Capture response bodies** (agent-browser HAR omits them):
   ```bash
   agent-browser network requests --type xhr,fetch --json
   ```
   For each API request (filter by JSON content-type, skip analytics domains):
   ```bash
   agent-browser network request <request-id> --json
   # Apply browser-sniff pacing between response body fetches
   # These are direct API calls and most likely to trigger rate limits
   ```
   Combine HAR metadata + response bodies into an enriched capture JSON at `$DISCOVERY_DIR/browser-sniff-capture.json`.

4. **Stop HAR recording**:
   ```bash
   agent-browser network har stop "$DISCOVERY_DIR/browser-sniff-capture.har"
   ```

#### Step 2e: Claude chrome-MCP capture (failure-recovery fallback)

Use this backend when (a) the user picks chrome-MCP from the Step 2c.5 recovery menu after `browser-use` or `agent-browser` was hard-blocked by an anti-bot gate, or (b) the user opted into chrome-MCP up-front from the Step 1b install picker. Detection is set in Step 1 (`CHROME_MCP_AVAILABLE`). Skip this section entirely if `CHROME_MCP_AVAILABLE=false`.

**Why chrome-MCP exists as a fallback.** chrome-MCP drives the user's already-running Chrome via a browser extension, so capture happens inside the user's logged-in session — no cookie transfer step (unlike Step 1d), and the WAF sees a real Chrome instance with the user's normal fingerprint. This is exactly what unblocks targets that hard-blocked headless Chromium-based capture (browser-use, agent-browser) under Akamai / DataDome / sophisticated bot detection.

**Auth surface caveat.** chrome-MCP captures every request the user's tab fires, including ones carrying `Authorization` headers, session cookies, OAuth tokens, and JWTs from the live session. The credential strip rules below are mandatory, not advisory.

**Prerequisites.**

- The Chrome browser extension for chrome-MCP must be installed and connected. If `tabs_context_mcp` returns an error indicating the extension isn't connected, instruct the user via `AskUserQuestion` to connect the extension and retry. If they decline, fall back to the Step 2c.5 recovery menu with chrome-MCP removed (the menu re-fires per the menu re-fire rule in Step 2c.5).
- Chrome must be visible and unminimized (chrome-MCP cannot drive a hidden window).
- For authenticated discovery: the user should already be logged in to `<site>` in their Chrome session. No separate cookie transfer step.

**Tab scope rule (mandatory — not optional).**

Always create a fresh capture tab via `mcp__claude-in-chrome__tabs_create_mcp`, even if `tabs_context_mcp` shows a tab already open at `<site>`. Reasons:

- The user's existing tab may have unsaved work (a draft message, a partially-filled form, a search the user is mid-way through). Navigating it away is data loss.
- Other open tabs may be sensitive (banking, internal tools, personal email). The agent has no way to classify them; the safe default is to never touch them.
- A fresh tab gives the agent a clean DOM and clean network log scoped to the discovery target.

`tabs_context_mcp` is called for awareness only — to confirm the extension is connected and to log how many tabs the user has open. **Never `navigate` an existing tab.** Always `tabs_create_mcp` → `navigate` in the new tab. After capture, close the capture tab via `mcp__claude-in-chrome__tabs_close_mcp` so the user's tab strip returns to its original state.

**Body-capture interceptor (mandatory — install BEFORE navigation).**

`mcp__claude-in-chrome__read_network_requests` returns request metadata (URL, method, request headers, response status) but **does not include response bodies**. The downstream `internal/browsersniff/parser.go` consumer requires `response_body` populated per entry, so an extra step is mandatory.

Two viable approaches:

1. **Recommended: in-page interceptor installed before navigation.** Use `mcp__claude-in-chrome__javascript_tool` to install a `fetch` and `XMLHttpRequest` interceptor in the new tab BEFORE navigating to `<site>`. The interceptor pushes each completed request's body into `window.__capture_bodies` keyed by URL+method. After capture, `javascript_tool` reads `window.__capture_bodies` back. This avoids re-firing requests against a wary WAF.

2. **Fallback: `javascript_tool` re-fetches each captured URL after the fact.** Mirrors the `agent-browser network request <id> --json` enrichment loop in Step 2b. Use this only when option 1 fails (e.g., the page wraps `fetch` itself and the interceptor can't shadow it). Re-fetching may trip rate limits — apply the pacing rules below.

The interceptor sketch (illustrative, adapt to the page's actual fetch shape):

```javascript
// Run via mcp__claude-in-chrome__javascript_tool BEFORE mcp__claude-in-chrome__navigate
window.__capture_bodies = {};
const _origFetch = window.fetch;
window.fetch = async function(...args) {
  const resp = await _origFetch.apply(this, args);
  const cloned = resp.clone();
  const url = typeof args[0] === 'string' ? args[0] : args[0].url;
  const method = (args[1] && args[1].method) || 'GET';
  cloned.text().then(body => {
    window.__capture_bodies[`${method} ${url}`] = body;
  }).catch(() => {});
  return resp;
};
// XHR interceptor analogous; install both
```

**Capture flow (full sequence).**

1. `mcp__claude-in-chrome__tabs_context_mcp` — awareness only; confirm extension is connected
2. `mcp__claude-in-chrome__tabs_create_mcp` — fresh capture tab
3. `mcp__claude-in-chrome__javascript_tool` — install fetch + XHR body interceptor in the new tab
4. `mcp__claude-in-chrome__navigate` — open the discovery target URL
5. Interaction loop (per the same "click + scroll + interact" guidance the browser-use flow uses):
   - `mcp__claude-in-chrome__read_page` to find interactive elements
   - `mcp__claude-in-chrome__find` + `mcp__claude-in-chrome__left_click` (or `form_input` for forms) to interact
   - For SPAs that hydrate via JS without a fresh navigation, do at least 3 meaningful interactions before reading the network log
6. `mcp__claude-in-chrome__read_network_requests` — pull request metadata
7. `mcp__claude-in-chrome__javascript_tool` — read back `window.__capture_bodies`
8. Optional: `mcp__claude-in-chrome__get_page_text` for SSR-rendered content
9. Merge metadata + bodies into the enriched-capture JSON shape (next subsection)
10. `mcp__claude-in-chrome__tabs_close_mcp` — clean up the capture tab

**Output format.**

Write to `$DISCOVERY_DIR/browser-sniff-capture.json` using the `EnrichedCapture` shape from `internal/browsersniff/types.go`:

- Top-level: `target_url`, `captured_at` (RFC3339), `interaction_rounds` (count of meaningful interactions), `auth` (object describing how auth flows for this capture; `{"type": "chrome_mcp_session", "site": "<site>"}` is fine), `entries[]`
- Per-entry: `method`, `url`, `request_headers` (object), `response_headers` (object), `request_body` (string, may be empty), `response_body` (string, populated from interceptor), `response_status` (int), `response_content_type` (string), `classification` (leave empty; analyzer fills), `is_noise` (leave false; analyzer fills)

This is the same shape `agent-browser`'s enriched-capture JSON uses (Step 2b lines 685-695), so downstream parsing is unchanged.

**Write-time credential strip (mandatory — not optional).**

Before writing the entry to `$DISCOVERY_DIR/browser-sniff-capture.json`, scrub credentials from `request_headers` and `response_headers`:

- Remove headers with names matching (case-insensitive): `Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization`, `X-Api-Key`, `X-Auth-Token`, `X-Session-Id`, and any header matching the regex `/^x-.*-(token|key|auth|session|secret)$/i`.
- For URLs containing query parameters that look like tokens (`access_token=`, `api_key=`, `token=`, `key=`, `signature=`, `auth=`, `password=`), redact the value to `REDACTED` in the stored URL.

Cross-reference `secret-protection.md` for the canonical scrub list — when the canonical list updates, this section must update too. The strip happens at write time so the artifact never sits on disk with live credentials, even briefly. Phase 5.5 archive-time strip is a defense-in-depth backstop, not the primary control for chrome-MCP captures.

**Pacing scope.**

The adaptive-pacing rules at the top of "If user approves browser-sniff" (start at 1s, ramp down on success, double on 429, hard-stop after 3 consecutive 429s) apply to operations the agent initiates: `navigate` between pages, `javascript_tool`-fired `fetch()` calls, and any re-fetch loop in the fallback body-capture approach. They do **not** apply to `read_network_requests` itself — that's observational and reads what the page already fired. Apply pacing where the agent is the request source, not the observer.

**Failure detection (multi-trigger).**

A chrome-MCP capture has not succeeded just because `read_network_requests` returned 142 entries — the entries may all be challenge-page noise. Before reporting success, check all three triggers; any one means "WAF is also blocking chrome-MCP, treat as capture failure":

1. **HTTP status pattern.** Count entries by status. If fewer than 30% returned 2xx, OR more than 30% returned 403/429, the capture is failing.
2. **Body markers.** Scan response bodies for known challenge sentinels: `Just a moment` (Cloudflare), `cf_chl_opt` (Cloudflare), `_abck` references in HTML (Akamai), `<title>Access Denied</title>` (Akamai/F5), `Pardon Our Interruption` (PerimeterX), `<title>Bot or Not?</title>` (Akamai). Any sentinel in any body triggers detection.
3. **Page sentinel.** `mcp__claude-in-chrome__get_page_text` after the interaction loop — if the visible page contains "verifying you are human", "checking your browser", "captcha" (case-insensitive), or "access denied", treat as challenge.

When any trigger fires, do NOT write the artifact. Re-fire the Step 2c.5 recovery menu with chrome-MCP removed from the option list (per the menu re-fire rule in Step 2c.5). Record the failure in the marker file's `reason` field: `chrome-mcp captured N requests but failure detection triggered: <which trigger fired>`.

**Replayability check.**

Even when failure detection passes, the same replayability constraint from cardinal rule 5 applies: the captured surface must round-trip through Surf with the same Chrome TLS fingerprint the printed CLI will ship, or the captured URLs are unusable in production. Do not skip this check just because the user's authenticated browser session worked — the printed CLI does not have the user's authenticated browser session.

#### Step 2c: Thin-results safety check

After completing the primary user flow capture (browser-use or agent-browser), count unique API endpoints discovered. If fewer than 5 unique endpoints:

1. **Diagnose, don't accept.** Thin results from an SPA almost always mean the agent loaded pages without interacting. Ask yourself: did I click buttons? Did I fill forms? Did I submit anything? Did I scroll to trigger lazy loads? If the answer is "I mostly just navigated to URLs," that is the problem.

2. **Re-sniff with interaction.** Go back to the page where results were thinnest. Take a snapshot. Find interactive elements. Click the most prominent one. Wait for network activity. Repeat for at least 3 interactions before accepting thin results.

3. **Compare against known endpoints.** If Phase 1 research found community wrappers documenting N endpoints but the browser-sniff found fewer than N/2, the browser-sniff missed something. Community wrappers are a floor, not a ceiling -- they represent what someone else already reverse-engineered, often years ago. The real API surface is almost certainly larger.

4. **Report the gap honestly.** If re-sniffing with interaction still produces thin results, report: "Browser-Sniff captured X endpoints but community wrappers document Y. The site may use WebSocket, protobuf, server-side rendering, or other techniques that resist browser capture." Do NOT conclude "the API has few endpoints" when the real answer may be "I didn't interact enough to trigger them."

If the thin-results check triggers a re-sniff that discovers additional endpoints, merge the new captures with the originals before proceeding to Step 3.

#### Step 2c.5: Challenge-only capture safety check

After capture, inspect the collected responses before generating a spec. A browser-sniff is **not successful** if it only captured challenge, login, or access-denied pages.

Treat the capture as failed when all or nearly all captured target-site responses match one of these:
- HTTP `403` or `429` HTML with Cloudflare/Vercel/WAF/DataDome/PerimeterX/CAPTCHA markers
- titles or body text such as "Just a moment", "Access denied", "Please enable JavaScript", "captcha", "challenge"
- only login redirects/pages when the user expected an authenticated capture
- no API-looking requests, no SSR embedded data, no structured HTML/feed data, and no page-context fetch evidence

When this happens, do not continue to Phase 2 with a challenge-page spec. Compose the recovery menu **per the availability of the MCP fallback flags set in Step 1** — the menu shape changes based on what's reachable in this runtime. Use the table below to pick the option set, then present via `AskUserQuestion`.

**Menu composition table.** Always read `CHROME_MCP_AVAILABLE` and `COMPUTER_USE_AVAILABLE` from your reasoning (set in Step 1). Pick the row matching the flag combination:

| `CHROME_MCP_AVAILABLE` | `COMPUTER_USE_AVAILABLE` | Menu options (in order) |
|---|---|---|
| false | false | (1) Try cleared-browser capture again, (2) I'll provide a HAR from DevTools, (3) Discuss alternate CLI scope — current behavior, no change |
| false | true | (1) Try cleared-browser capture again, (2) I'll provide a HAR from DevTools — I'll guide you with screenshots of your DevTools window, (3) Discuss alternate CLI scope |
| true | false | (1) Try cleared-browser capture again, (2) **Try chrome-MCP** — recommended on anti-bot trigger, (3) I'll provide a HAR from DevTools, (4) Discuss alternate CLI scope |
| true | true | (1) Try cleared-browser capture again, (2) **Try chrome-MCP** — recommended on anti-bot trigger, (3) I'll provide a HAR from DevTools — I'll guide you with screenshots of your DevTools window, (4) Discuss alternate CLI scope |

**Recommended-badge rule.** When the menu fires because of an anti-bot block (the trigger criteria above: 403/429 with WAF markers, "Just a moment", challenge titles, login-redirect-only when authenticated capture was expected), chrome-MCP carries the **(Recommended)** badge whenever it is present in the menu, regardless of whether computer-use is also detected — chrome-MCP is the highest-leverage path against an anti-bot block because it uses the user's real Chrome session. In other failure modes (thin results from the Step 2c check, time-budget bailout), no option carries the Recommended badge — let the user pick based on context.

**Question stem (teach the chrome-MCP mechanic when present).** When chrome-MCP is in the menu and the user has not seen this menu before in the current session, the question stem must teach the mechanic in one line. The user is mid-failure and may have never encountered chrome-MCP-as-fallback.

When chrome-MCP IS in the menu:

> "The browser capture only saw challenge or login pages, so it did not discover the real website data/API surface. The Chrome-extension MCP can capture from your existing Chrome window — pick this if your Chrome is open and you're already logged in to the target site. What should we do next?"

When chrome-MCP is NOT in the menu (current 3-option case, unchanged):

> "The browser capture only saw challenge or login pages, so it did not discover the real website data/API surface. What should we do next?"

**Fixed option labels and bodies.** Use these exact strings as the `AskUserQuestion` option labels and descriptions — the implementer should not paraphrase. Labels are short (4-7 words), self-contained (some harnesses render labels without descriptions), and front-load the differentiator. Composition is per the table above; pick the option set for the flag combination, then mark Recommended where the rule above says.

- **"Try cleared-browser capture again"** — "Open/attach Chrome, solve the challenge, then repeat the browser-sniff with the previous backend."
- **"Try chrome-MCP"** — "Capture from your already-running Chrome window via the Chrome extension MCP. Uses your logged-in session, no cookie transfer. The agent will create a fresh tab and close it when done."
- **"I'll provide a HAR from DevTools"** — "You browse the target site in your Chrome and export a HAR. The agent analyzes it for discovery, then keeps only routes that pass replayability checks."
- **"I'll provide a HAR from DevTools — I'll guide you with screenshots of your DevTools window"** — "Same as 'I'll provide a HAR from DevTools', plus the agent uses computer-use to take screenshots of your Chrome window at each step and tells you what it sees so you don't get stuck on the export flow."
- **"Discuss alternate CLI scope"** — "Step away from this discovery target and consider RSS/docs/official API/narrower-scope proposals. Only pick this when the other options have been tried."

**Routing on selection.**

- "Try cleared-browser capture again" → re-run the previously-attempted backend (browser-use or agent-browser) per the existing capture flow. If it fails again with the same trigger, re-fire this menu with this option's body amended to "(this option already failed once; consider another path)" — the option stays in the menu but loses any badge.
- "Try chrome-MCP" → load the chrome-MCP MCP tools via `ToolSearch` (`select:mcp__claude-in-chrome__*`), then proceed to **Step 2e** for the capture playbook.
- "I'll provide a HAR from DevTools" (either variant) → proceed to the manual-HAR flow at Step 1d (the existing branch). When the user picks the augmented variant, the manual-HAR body augmentation in Step 1d kicks in — see Step 1d for the computer-use visual-feedback-loop.
- "Discuss alternate CLI scope" → leave this flow and return to upstream scope discussion. Record `decision: alternate_scope` in the marker file.

**Menu re-fire on backend failure (session-scoped state).** When the user picks a backend from this menu and that backend ALSO fails (per the Step 2c.5 trigger criteria for browser-use/agent-browser/manual-HAR, or per the Step 2e multi-trigger failure detection for chrome-MCP), re-fire this exact menu with the failed option removed. Track which backends have been tried in your reasoning across this same recovery session — this is a session-scoped tracking concern, not a marker-file field. Stop re-firing when only "Discuss alternate CLI scope" remains in the menu, or when the user picks "Discuss alternate CLI scope" themselves.

**Marker file `reason` field convention.** When recording the chosen backend in the `browser-browser-sniff-gate.json` marker, name it explicitly so future audit can identify which backend produced the capture. Examples: `reason: "chrome-mcp captured 142 requests; SSR enriched after Akamai block on agent-browser"`, `reason: "manual-HAR with computer-use guidance after browser-use blocked"`. The `reason` field is free-form per the existing schema; no migration needed.

Only the "Discuss alternate CLI scope" option may lead to an RSS-first, official API, docs-only, or narrower-scope proposal. Record the failed capture in `$DISCOVERY_DIR/browser-sniff-report.md` if a report is written.

If direct HTTP is blocked but the page does not require live page-context execution, try the lightweight replay path before proposing a resident browser runtime: Surf/Chrome-compatible HTTP with modern TLS/UA headers, uTLS-style Chrome ClientHello where available, browser-clearance cookie import plus replay, or structured HTML/SSR/RSS extraction. These are discovery and replayability aids, not permission to ship a browser sidecar transport.

#### Step 2d: Cookie auth validation (authenticated browser-sniff only)

**Skip this step if:** The browser-sniff was anonymous (no session transfer in Step 1d), or the API uses API key / Bearer token auth rather than cookie-based session auth.

**Purpose:** Before promising `auth login --chrome` in the generated CLI, validate that browser cookies actually produce authenticated responses when replayed outside the browser context. Some APIs use CSRF tokens, SameSite cookie policies, or other mechanisms that prevent cookie-only replay.

**Validation procedure:**

1. **Select a test endpoint.** Pick one endpoint from the capture that returned HTTP 200 and appears to require authentication (e.g., a user-specific resource like `/api/me`, `/account`, or `/orders`).

2. **Replay with cookies.** Using `curl` or the capture tool, replay the request with the captured cookies attached:
   ```bash
   curl -s -o /dev/null -w "%{http_code}" \
     -H "Cookie: <captured-cookie-string>" \
     "https://<api-domain>/<test-endpoint>"
   ```
   Expected: HTTP 200 (or the same status as during capture).

3. **Replay without cookies.** Replay the same request with no cookies:
   ```bash
   curl -s -o /dev/null -w "%{http_code}" \
     "https://<api-domain>/<test-endpoint>"
   ```
   Expected: HTTP 401, 403, or a redirect to a login page.

4. **Evaluate results:**

   | With cookies | Without cookies | Verdict |
   |-------------|----------------|---------|
   | 200 | 401/403/302 | **Pass** — cookie auth works. Set `Auth.Type = "cookie"` and `CookieDomain` in the spec. The generated CLI will include `auth login --chrome`. |
   | 200 | 200 (same content) | **Not required** — cookies aren't needed for this endpoint. Check other endpoints; if none require auth, set `Auth.Type = "none"`. |
   | 401/403 | 401/403 | **Fail** — cookies don't replay (likely CSRF, SameSite, or IP binding). Warn the user and do not offer browser auth. |
   | Other | Any | **Inconclusive** — try a different test endpoint. If all attempts are inconclusive after 3 endpoints, treat as Fail. |

5. **On Pass:** Proceed to Step 3. The browser-sniff report (Step 5) should note: "Cookie auth validated — the generated CLI will support `auth login --chrome`."

6. **On Fail:** Inform the user via the conversation:
   > "Authenticated endpoints were discovered, but cookie replay failed (likely CSRF tokens or strict cookie policies). The generated CLI will include these endpoints but won't offer `auth login --chrome`. You'll need to manually provide auth tokens via environment variables."

   Set `Auth.Type = "none"` in the capture's auth section. Include the authenticated endpoints in the spec (they're still valid endpoints), but the CLI won't have a browser auth path. Note the failure reason in the browser-sniff report.

#### Step 3: Analyze capture

Run browser-sniff on the captured traffic. Always write the structured traffic analysis to the discovery directory so it is archived with the manuscript:
```bash
printing-press browser-sniff --har "$DISCOVERY_DIR/browser-sniff-capture.har" --name <api> --output "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"
```

If using agent-browser's enriched capture format instead:
```bash
printing-press browser-sniff --har "$DISCOVERY_DIR/browser-sniff-capture.json" --name <api> --output "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"
```

If hand-writing or repairing `$DISCOVERY_DIR/traffic-analysis.json`, inspect the canonical schema first:

```bash
printing-press schema traffic-analysis > "$DISCOVERY_DIR/traffic-analysis.schema.json"
```

Two fields trip up hand-edits often enough to call out:

- **`version`** is the literal string `"1"` — not semver, not `"1.0.0"`. The downstream parser rejects any other value with `unsupported traffic analysis version`.
- **Confidence fields** are numbers from `0` to `1`, not strings such as `"high"`.

#### Step 4: Report and update spec source

Report: "Browser-Sniff discovered **N endpoints** across **M resources**. [X new endpoints not in the original spec.]"

Read `$DISCOVERY_DIR/traffic-analysis.json` before reporting. If it includes:
- `"reachability": {"mode": "browser_clearance_http", ...}` — report: "Direct HTTP is blocked; generation will use browser-compatible HTTP plus `auth login --chrome` cookie import. After generation, test whether Surf + imported cookies can replay the captured requests without a resident browser."
- Useful same-site HTML document captures — report: "Browser-Sniff found replayable HTML pages; generation can emit `response_format: html` commands that extract metadata and filtered links without a resident browser."
- `"reachability": {"mode": "browser_required", ...}` — report: "The captured surface appears to require live page-context execution. This is not a shippable runtime shape for ordinary printed CLI commands. Return to discovery for a Surf/direct/browser-clearance replayable surface such as HTML, SSR data, RSS, JSON-LD, or a lighter internal endpoint, or HOLD the run."

Also report the runtime shape the printed CLI will use: standard HTTP, Surf/browser-compatible HTTP, browser-clearance cookie import plus replay, structured HTML/SSR/RSS extraction, or HOLD because no replayable surface was found.

Update the spec source for Phase 2:
- **Enrichment mode**: Phase 2 will use `--spec <original> --spec <sniff-spec> --name <api>` to merge both
- **Primary mode**: Phase 2 will use `--spec <sniff-spec>` directly

#### Step 5: Write browser-sniff discovery report

Write a structured browser-sniff provenance report to `$DISCOVERY_DIR/browser-sniff-report.md`. This report preserves the discovery evidence so a future maintainer can reproduce or extend the browser-sniff.

The report must contain these sections:

1. **User Goal Flow** — The primary browser-sniff goal and each step attempted.
   - Goal: [e.g., "Order a pizza for delivery"]
   - Steps completed: [numbered list of steps taken, with which API operations each step triggered]
   - Steps skipped: [any steps that couldn't be completed, with reason]
   - Secondary flows attempted: [any additional workflows beyond the primary goal]
   - Coverage: [X of Y planned steps completed]

2. **Pages & Interactions** — List every URL browsed and interaction performed during the browser-sniff, in order. Include the page purpose and what was clicked/filled/submitted (e.g., "Homepage -- clicked 'Delivery' button", "Address modal -- entered '350 5th Ave', clicked 'Continue'").

3. **Browser-Sniff Configuration** — Backend used (browser-use, agent-browser, or manual HAR), pacing settings (initial delay, final effective rate), and proxy pattern detection result (proxy-envelope detected / not detected, with the proxy URL if applicable).

4. **Endpoints Discovered** — A markdown table with columns: Method, Path, Status Code, Content-Type, Auth. One row per unique endpoint observed. The Auth column is "public" or "auth-required" (based on Step 2a.1.5 classification). If no authenticated flow was run, omit the Auth column.

5. **Traffic Analysis** — Summarize `$DISCOVERY_DIR/traffic-analysis.json`:
   - Protocols observed (labels + confidence, e.g., `rest_json`, `graphql`, `google_batchexecute`, `ssr_embedded_data`)
   - Auth signals (candidate types, header/query/cookie names only -- never values)
   - Parameter-name evidence from forms, input labels, placeholder text, SDK/source names, and request context. Preserve enough detail to justify any later `flag_name` enrichment.
   - Protection signals (Cloudflare/CAPTCHA/login redirects/protected-web hints)
   - Generation hints (e.g., `requires_browser_auth`, `requires_js_rendering`, `requires_protected_client`, `has_rpc_envelope`)
   - Candidate commands worth considering
   - Warnings such as raw protocol envelopes, GraphQL error-only responses, HTML challenge pages, empty payloads, or weak schema evidence
   Treat warnings as discovery evidence, not publish blockers.

6. **Coverage Analysis** — What resource types were exercised (e.g., "collections, workspaces, teams, categories") and what was likely missed. Compare against the Phase 1 research brief to identify gaps (e.g., "Brief mentions 'flows' but no flow endpoints were discovered during browser-sniff").

7. **Response Samples** — For each unique response shape (keyed by status code + content-type category), include a truncated sample:
   - JSON/text responses: first 2KB or 100 lines, whichever is smaller
   - Binary responses (images, protobuf, etc.): skip content, include a metadata note: `Binary response: <content-type>, <size> bytes`
   - Aim for one sample per unique shape, not one per endpoint

8. **Rate Limiting Events** — Any 429 responses encountered, delays applied, and effective browser-sniff rate achieved (e.g., "Sniffed 7 endpoints at ~1.5 req/s effective rate, one 429 at request #4").

9. **Authentication Context** — Whether the browser-sniff used an authenticated session. If yes: transfer method used (auto-connect / profile / headed login / HAR), which endpoints were only reachable with auth (e.g., "order history, saved addresses, rewards required login"), the auth header scheme discovered (e.g., "Authorization: CustomerAuth {customerId}|{authToken}", "Bearer token from localStorage"), and confirmation that session state was excluded from manuscript archiving. If no: "No authenticated session used."

10. **Bundle Extraction** — If JS bundle extraction ran (Step 2a.2.7), list: the bundle URL analyzed, the API base URL discovered, endpoints found only in the bundle (not during interactive browser-sniff), and any API config extracted (version headers, auth construction patterns). If bundle extraction did not run, omit this section.
