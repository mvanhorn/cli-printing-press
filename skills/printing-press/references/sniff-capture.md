# Sniff Capture Implementation

> **When to read:** This file is referenced by Phase 1.7 of the printing-press skill.
> Read it when the user approves sniff (browser-use or agent-browser capture of live API traffic).

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
