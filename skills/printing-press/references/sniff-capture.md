# Sniff Capture Implementation

> **When to read:** This file is referenced by Phase 1.7 of the printing-press skill.
> Read it when the user approves sniff (browser-use or agent-browser capture of live API traffic).

### Cardinal Rules

1. **ALWAYS use browser-use for capture.** Do NOT substitute curl probing, JS bundle grepping, or agent-browser auto-connect for a proper browser-use interactive sniff. Agent-browser is for session transfer only (grabbing cookies from a running Chrome). The capture — browsing pages, collecting URLs, intercepting requests — MUST use browser-use.

2. **Do NOT skip auth discovery when the session expires.** *(Only applies when `AUTH_SESSION_AVAILABLE=true` — the user confirmed they're logged in.)* If a Chrome profile loads but the session has expired (login page visible instead of account page), offer headed login as a fallback. Never proceed without auth just because the profile session was stale. For anonymous sniffs (no auth context), this rule does not apply.

3. **Use click-based SPA navigation after installing interceptors.** `browser-use open` triggers a full page reload which resets the JS context and destroys fetch/XHR interceptors. After installing interceptors, navigate by clicking links (`browser-use eval "document.querySelector('a[href*=account]').click()"` or `browser-use click`). Only use `browser-use open` for the first page load or when you need to re-install interceptors.

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

**When Chrome IS running**, use agent-browser to grab cookies, then ask the user to quit Chrome so browser-use can load the profile for capture:

Present via `AskUserQuestion`:
> "Chrome is running. I'll grab your cookies, then need you to quit Chrome so I can sniff with full page access."
>
> 1. **Grab session, then quit Chrome** (Recommended) — "I save your cookies via agent-browser, you quit Chrome, then I sniff with browser-use using your profile. Full DOM access."
> 2. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I sniff. ~1 minute."
> 3. **I'll export a HAR file** — "You browse the site in DevTools, export the HAR."

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
> 2. **Log in within a new browser window** — "I'll open a visible browser. You log in, then I sniff."
> 3. **I'll export a HAR file**

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
> 1. **Log in manually** — "I'll open a headed browser. You log in, then I sniff."
> 2. **Continue without auth** — "Sniff only public endpoints"
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
> 1. **Open headed browser to log in** (Recommended) — "I'll open a visible browser. You log in, then I continue the sniff."
> 2. **Continue without auth** — "Sniff only public endpoints"

Do NOT silently proceed without auth when the session has expired. The authenticated surface is often the most valuable part of the API (order history, rewards, saved data).

If cookies are verified, proceed to Steps 2a/2b capture flow with the authenticated session loaded. The session state file is stored at `$DISCOVERY_DIR/session-state.json`.

#### Step 2a: browser-use CLI capture (preferred)

Claude drives browser-use directly via CLI commands — no LLM key needed, no Python API versioning issues. Uses the browser's native Performance API to collect API endpoint URLs from each page.

**IMPORTANT: Run the page collection loop in foreground, not background.** The loop takes ~60-90 seconds for 10-15 pages. Background execution has unreliable output capture for shell functions that call browser-use. Always run this inline.

**Step 2a.1: Build the user flow plan**

From the primary sniff goal (Step 0 in the SKILL.md), derive the interactive steps a real user would take to accomplish that goal. This is NOT a list of pages to load -- it is a sequence of actions.

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
   - Record the scheme (e.g., `Bearer`, `PagliacciAuth`, `Token`, custom)
   - **Trace values back to cookies.** Read `document.cookie` and match literal values from the captured header against cookie values:
     ```bash
     browser-use eval "document.cookie"
     ```
     For each cookie `name=value`, check if `value` appears as a substring in the Authorization header. When a match is found, record the cookie name and which part of the header it corresponds to.
   - **Construct the format string.** Replace each literal cookie value in the header with `{cookieName}`:
     - Example: header `PagliacciAuth 2432962|FD44DA6A-...`, cookies `customerId=2432962; authToken=FD44DA6A-...`
     - Format string: `PagliacciAuth {customerId}|{authToken}`
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

7. **If auth pages redirect to login.** The session may have expired between the time the user confirmed login and the sniff reaches this step. Report: "Auth pages redirected to login — session may have expired. Auth-only endpoints not discovered." Do NOT fail the sniff — the public endpoints are still valid. Proceed to Step 2a.2 with the public set only.

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

SPA frameworks (Angular, React, Vue, Next.js) compile all API endpoint paths into their main JS bundle. Extracting these paths supplements the sniff with endpoints that no user flow visits (admin features, migration tools, rarely-used settings).

**When to run:** After completing the interactive sniff (Steps 2a.1–2a.2.5). This is supplementary — the sniff is primary because it provides response shapes, auth patterns, and parameter types. Bundle extraction only gives endpoint paths.

**Skip when:** The site is server-rendered HTML without JS bundles, or the sniff already discovered 20+ endpoints and the API surface appears complete.

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

3. **Merge with sniff results.** Append bundle-discovered endpoints to `$SNIFF_URLS`. Mark their provenance:
   ```bash
   # Append bundle-only endpoints (not already in sniff-urls.txt)
   # In the discovery report, mark these as "discovered: bundle"
   ```

4. **Record API config.** If the bundle reveals useful config (API version headers, auth token construction, rate limit hints), note them in the discovery report's Sniff Configuration section.

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

2. **Walk the user flow** using the snapshot-reason-act loop:
   - Use the user flow plan from Step 2a.1 (same flow applies to both backends)
   - For each step in the flow:
     - `agent-browser snapshot -i` to see the current page state
     - Find the interactive element for this step (button, form, link, dropdown)
     - Click/fill/submit it
     - `agent-browser wait --network-idle` after each interaction
     - Apply sniff pacing between interactions (1s initial, adaptive per Sniff Pacing rules)
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
   # Apply sniff pacing between response body fetches
   # These are direct API calls and most likely to trigger rate limits
   ```
   Combine HAR metadata + response bodies into an enriched capture JSON at `$DISCOVERY_DIR/sniff-capture.json`.

4. **Stop HAR recording**:
   ```bash
   agent-browser network har stop "$DISCOVERY_DIR/sniff-capture.har"
   ```

#### Step 2c: Thin-results safety check

After completing the primary user flow capture (browser-use or agent-browser), count unique API endpoints discovered. If fewer than 5 unique endpoints:

1. **Diagnose, don't accept.** Thin results from an SPA almost always mean the agent loaded pages without interacting. Ask yourself: did I click buttons? Did I fill forms? Did I submit anything? Did I scroll to trigger lazy loads? If the answer is "I mostly just navigated to URLs," that is the problem.

2. **Re-sniff with interaction.** Go back to the page where results were thinnest. Take a snapshot. Find interactive elements. Click the most prominent one. Wait for network activity. Repeat for at least 3 interactions before accepting thin results.

3. **Compare against known endpoints.** If Phase 1 research found community wrappers documenting N endpoints but the sniff found fewer than N/2, the sniff missed something. Community wrappers are a floor, not a ceiling -- they represent what someone else already reverse-engineered, often years ago. The real API surface is almost certainly larger.

4. **Report the gap honestly.** If re-sniffing with interaction still produces thin results, report: "Sniff captured X endpoints but community wrappers document Y. The site may use WebSocket, protobuf, server-side rendering, or other techniques that resist browser capture." Do NOT conclude "the API has few endpoints" when the real answer may be "I didn't interact enough to trigger them."

If the thin-results check triggers a re-sniff that discovers additional endpoints, merge the new captures with the originals before proceeding to Step 3.

#### Step 2d: Cookie auth validation (authenticated sniff only)

**Skip this step if:** The sniff was anonymous (no session transfer in Step 1d), or the API uses API key / Bearer token auth rather than cookie-based session auth.

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

5. **On Pass:** Proceed to Step 3. The sniff report (Step 5) should note: "Cookie auth validated — the generated CLI will support `auth login --chrome`."

6. **On Fail:** Inform the user via the conversation:
   > "Authenticated endpoints were discovered, but cookie replay failed (likely CSRF tokens or strict cookie policies). The generated CLI will include these endpoints but won't offer `auth login --chrome`. You'll need to manually provide auth tokens via environment variables."

   Set `Auth.Type = "none"` in the capture's auth section. Include the authenticated endpoints in the spec (they're still valid endpoints), but the CLI won't have a browser auth path. Note the failure reason in the sniff report.

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

1. **User Goal Flow** — The primary sniff goal and each step attempted.
   - Goal: [e.g., "Order a pizza for delivery"]
   - Steps completed: [numbered list of steps taken, with which API operations each step triggered]
   - Steps skipped: [any steps that couldn't be completed, with reason]
   - Secondary flows attempted: [any additional workflows beyond the primary goal]
   - Coverage: [X of Y planned steps completed]

2. **Pages & Interactions** — List every URL browsed and interaction performed during the sniff, in order. Include the page purpose and what was clicked/filled/submitted (e.g., "Homepage -- clicked 'Delivery' button", "Address modal -- entered '350 5th Ave', clicked 'Continue'").

3. **Sniff Configuration** — Backend used (browser-use, agent-browser, or manual HAR), pacing settings (initial delay, final effective rate), and proxy pattern detection result (proxy-envelope detected / not detected, with the proxy URL if applicable).

4. **Endpoints Discovered** — A markdown table with columns: Method, Path, Status Code, Content-Type, Auth. One row per unique endpoint observed. The Auth column is "public" or "auth-required" (based on Step 2a.1.5 classification). If no authenticated flow was run, omit the Auth column.

5. **Coverage Analysis** — What resource types were exercised (e.g., "collections, workspaces, teams, categories") and what was likely missed. Compare against the Phase 1 research brief to identify gaps (e.g., "Brief mentions 'flows' but no flow endpoints were discovered during sniff").

6. **Response Samples** — For each unique response shape (keyed by status code + content-type category), include a truncated sample:
   - JSON/text responses: first 2KB or 100 lines, whichever is smaller
   - Binary responses (images, protobuf, etc.): skip content, include a metadata note: `Binary response: <content-type>, <size> bytes`
   - Aim for one sample per unique shape, not one per endpoint

7. **Rate Limiting Events** — Any 429 responses encountered, delays applied, and effective sniff rate achieved (e.g., "Sniffed 7 endpoints at ~1.5 req/s effective rate, one 429 at request #4").

8. **Authentication Context** — Whether the sniff used an authenticated session. If yes: transfer method used (auto-connect / profile / headed login / HAR), which endpoints were only reachable with auth (e.g., "order history, saved addresses, rewards required login"), the auth header scheme discovered (e.g., "Authorization: PagliacciAuth {customerId}|{authToken}", "Bearer token from localStorage"), and confirmation that session state was excluded from manuscript archiving. If no: "No authenticated session used."

9. **Bundle Extraction** — If JS bundle extraction ran (Step 2a.2.7), list: the bundle URL analyzed, the API base URL discovered, endpoints found only in the bundle (not during interactive sniff), and any API config extracted (version headers, auth construction patterns). If bundle extraction did not run, omit this section.
