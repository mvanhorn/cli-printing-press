# Printing Press Retro: Domino's Pizza

## Session Stats
- API: Domino's Pizza (unofficial REST API + GraphQL BFF)
- Spec source: Sniffed (browser sniff + community wrapper research, hand-built OpenAPI spec)
- Scorecard: 84/100 Grade A
- Verify pass rate: 92% (12/13)
- Fix loops: 1
- Manual code edits: 3 (root description, dead code removal, config fields)
- Features built from scratch: 4 (stores.go, menu.go, order.go, track.go — 640 lines)

## Findings

### 1. Sniff doesn't explore authenticated endpoints when AUTH_SESSION_AVAILABLE (skill instruction gap)

- **What happened:** User confirmed they're logged in to Domino's. Sniff connected to Chrome and captured traffic. But the user-flow plan only walked the anonymous ordering flow (homepage → order page). It never visited account pages (order history, rewards, saved addresses, profile) — the authenticated surface was entirely missed. The CLI ships without order history, rewards, or saved address commands.
- **Root cause:** The skill's sniff-capture.md Step 2a.1 says "After the primary flow, add 1-2 secondary flows from the research brief's other top workflows (e.g., 'Check rewards,' 'Track an order')." But this is a suggestion, not a gate. When `AUTH_SESSION_AVAILABLE=true`, there is no instruction to specifically browse authenticated pages. Claude followed the primary flow plan and stopped.
- **Cross-API check:** Any API with user accounts has an authenticated surface: Notion (workspace settings, billing), Linear (user profile, org settings), Stripe (dashboard data). When a user says "I'm logged in," the sniff should always explore what that unlocks. This applies across all input methods — browser sniff is the only way to discover cookie-authenticated endpoints.
- **Frequency:** Most APIs — any service with user accounts and browser-session auth.
- **Fallback if machine doesn't fix it:** Claude must remember to browse account pages manually. Reliability: sometimes — Claude followed the skill's flow plan, which didn't include auth pages. The Domino's run proves Claude doesn't catch this reliably.
- **Worth a machine fix?** Yes. This is the core value proposition of the chrome-auth-sniff feature — without it, authenticated endpoints are systematically missed.
- **Inherent or fixable:** Fixable. The sniff-capture.md needs an explicit "authenticated flow" section that activates when `AUTH_SESSION_AVAILABLE=true`.
- **Durable fix:** Add to sniff-capture.md Step 2a.1:
  ```
  When AUTH_SESSION_AVAILABLE=true, add authenticated flows AFTER the primary flow:
  1. Browse to account/profile page (e.g., /account, /profile, /my-account)
  2. Browse to order history page (e.g., /orders, /order-history)
  3. Browse to saved addresses / payment methods page
  4. Browse to rewards / loyalty page
  These pages trigger API calls that are only visible with session cookies.
  Compare endpoints discovered on auth pages vs public pages to classify
  which endpoints require authentication.
  ```
  Condition: `AUTH_SESSION_AVAILABLE=true`
  Guard: Skip when sniffing anonymously
- **Test:** Sniff a site where user confirmed login → verify auth pages are in the flow plan and auth-only endpoints appear in the discovery report.
- **Evidence:** Domino's sniff discovered only 2 pages (homepage, order page) despite user confirming logged-in session. Order history, rewards, saved addresses never visited.

### 2. Sniff doesn't inspect GraphQL POST bodies (skill instruction gap)

- **What happened:** Sniff discovered 9 POST requests to `/api/web-bff/graphql` but never inspected the request bodies to extract operation names. The GraphQL operations were discarded as "not inspected." This means the sniff can't distinguish between different GraphQL operations or understand the API surface of a GraphQL BFF.
- **Root cause:** The sniff-capture.md focuses on REST endpoint URL patterns. There's no procedure for GraphQL discovery: extracting `operationName` from POST bodies, classifying operations by type (query vs mutation), or building a spec from GraphQL operations.
- **Cross-API check:** GraphQL BFFs are increasingly common. Notion uses one. Linear uses GraphQL natively. Shopify, GitHub, and many modern platforms use GraphQL. Any SPA that routes all API calls through a single `/graphql` or `/api/graphql` endpoint will hit this gap.
- **Frequency:** API subclass: GraphQL BFF — estimated 20-30% of modern web apps.
- **Fallback if machine doesn't fix it:** Claude must manually intercept POST bodies, parse operationName fields, and build the spec. Reliability: never done automatically — Claude didn't even attempt it in this run.
- **Worth a machine fix?** Yes. GraphQL BFFs are common enough that the sniff needs a procedure for them.
- **Inherent or fixable:** Fixable. The sniff needs a "GraphQL detection" step.
- **Durable fix:** Add to sniff-capture.md after Step 2a.2:
  ```
  Step 2a.2.5: GraphQL BFF detection
  If all or most captured XHR URLs resolve to the same POST endpoint
  (e.g., /api/graphql, /graphql, /api/web-bff/graphql):
  1. Classify as GraphQL BFF
  2. For each captured POST request to that endpoint:
     - Extract the request body (use agent-browser network request <id>
       or browser-use eval to intercept fetch)
     - Parse the operationName and query fields
     - Record: operationName, variables schema, response shape
  3. Build the spec from GraphQL operations instead of REST paths
  4. Group operations by domain (e.g., store*, menu*, order*, account*)
  
  For agent-browser: use `network requests --type xhr --json` and then
  `network request <id> --json` to get POST bodies.
  For browser-use: inject a fetch interceptor before browsing:
    browser-use eval "const _origFetch=window.fetch;window.fetch=async(...a)=>{const r=await _origFetch(...a);if(a[0]?.includes?.('graphql'))console.log(JSON.stringify({url:a[0],body:a[1]?.body}));return r;}"
  ```
  Condition: Multiple POST requests to the same URL path
  Guard: Skip for standard REST APIs with distinct URL paths
- **Test:** Sniff a GraphQL BFF site → verify operations are extracted from POST bodies and appear in the spec.
- **Evidence:** 9 GraphQL operations captured by URL but zero operation names extracted.

### 3. Cookie auth classification missing from sniff → spec pipeline (skill instruction gap + binary gap)

- **What happened:** User is logged in. Sniff connected to Chrome with cookies. But the spec was hand-built from community wrapper knowledge with `Auth.Type` unset. The generator's cookie auth template (`auth_browser.go.tmpl`) was never triggered. The generated CLI has no `auth login --chrome` command despite the infrastructure existing.
- **Root cause:** Two gaps:
  1. **Skill gap:** Step 2d (cookie auth validation) only runs after endpoints are discovered. But the Domino's sniff skipped authenticated endpoints entirely (Finding #1), so Step 2d had nothing to validate.
  2. **Pipeline gap:** The hand-built spec didn't include auth config. When the spec is built manually (not by `printing-press sniff --har`), there's no mechanism to inject `Auth.Type: cookie` and `CookieDomain`.
- **Cross-API check:** Every cookie-authenticated API hits this if the sniff doesn't discover auth endpoints. The specgen already handles cookie auth when it comes through the HAR pipeline — the gap is in the manual/partial sniff path.
- **Frequency:** API subclass: cookie-authenticated websites — most websites with user accounts.
- **Fallback if machine doesn't fix it:** Claude must manually set Auth.Type in the spec. Reliability: never — Claude didn't do it in this run because there was no signal to trigger it.
- **Worth a machine fix?** Yes. The chrome-auth-sniff branch built the downstream infrastructure but the upstream discovery path doesn't feed it.
- **Inherent or fixable:** Fixable. Fixing Finding #1 (auth flow in sniff) feeds auth endpoints into the discovery. Then Step 2d validates cookie replay. Then the spec gets `Auth.Type: cookie`. Then the generator emits `auth_browser.go.tmpl`.
- **Durable fix:** This is primarily fixed by Finding #1 (discover auth endpoints) + ensuring Step 2d runs when auth endpoints are found. The spec-building step (whether `printing-press sniff --har` or manual) must propagate auth config. For the manual path: the skill should instruct Claude to add auth fields to hand-built specs when `AUTH_SESSION_AVAILABLE=true` and auth endpoints were discovered.
- **Test:** Sniff a cookie-auth site while logged in → generated CLI has `auth login --chrome` command.
- **Evidence:** User confirmed login. Generator has `auth_browser.go.tmpl`. Generated CLI has no `auth login --chrome`.

### 4. User-friendly command aliases built by hand every time (template gap)

- **What happened:** The generator produces commands named after API path segments: `power find-stores`, `orderstorage get-tracker-data`, `tracker-presentation-service track-order`. These are correct but unusable. 640 lines of Go (stores.go, menu.go, order.go, track.go) were written by hand to add `stores find`, `menu browse`, `order place`, `track` aliases.
- **Root cause:** The generator derives command structure from the spec's path hierarchy. `/power/store-locator` → parent `power`, child `store-locator`. This is mechanical and correct but produces ugly UX. No template exists for user-friendly top-level groupings.
- **Cross-API check:** Every sniffed API has this problem because path segments are implementation details, not user-facing concepts. Spec-based APIs are better (operationId gives some signal) but still produce `operationId`-derived names that need human mapping. The Steam CLI had the same issue.
- **Frequency:** Most APIs — especially sniffed ones where path segments are not user-friendly.
- **Fallback if machine doesn't fix it:** Claude writes alias commands manually. Reliability: always catches it (the ugly names are obvious), but 640 lines per CLI is expensive and the implementations are boilerplate. Claude delegates to a subagent which takes 2+ minutes.
- **Worth a machine fix?** Yes. This is the most time-consuming manual work in every generation. The generator could emit a command alias mapping.
- **Inherent or fixable:** Partially fixable. The generator can't always guess the best user-facing grouping, but it can:
  1. Detect common resource patterns (store, menu, order, track) and emit top-level aliases
  2. Emit alias stubs that Claude fills in (reduces boilerplate from 640 lines to ~100)
  3. Accept a `command_aliases` section in the spec extensions that maps path-derived names to user-friendly names
- **Durable fix:** Medium-term: Add `x-command-aliases` extension support to the spec parser. The skill instruction during Phase 2 would tell Claude to write aliases into the spec before generation. Short-term: the skill instruction can tell Claude to identify the top 4-5 resource groups from the spec and create alias commands following the existing helper patterns. This is still manual but guided.
  Condition: Always (all APIs benefit from friendlier names)
  Guard: None needed — aliases are additive, they don't break existing commands
- **Test:** Generate a CLI from a sniffed spec → verify user-friendly top-level commands exist alongside generated path-based commands.
- **Evidence:** 640 lines of hand-written alias code across 4 files.

### 5. Local build vs go-install binary mismatch (skill instruction gap)

- **What happened:** The setup contract resolved `printing-press` to `~/go/bin/printing-press` (v0.4.0, installed via `go install`) instead of the local build at `./printing-press` which has unreleased features (polish, workflow-verify, chrome auth). The entire Domino's run used the old binary, missing `polish --remove-dead-code`, `workflow-verify`, and the chrome auth generator template.
- **Root cause:** The setup contract checked `command -v printing-press` which found the global install first. The local build (kept current by lefthook post-merge hook) was available but not on PATH.
- **Cross-API check:** Every run from inside the printing-press repo hits this if the go-install version is older than the local build.
- **Frequency:** Every run from inside the repo (development workflow).
- **Worth a machine fix?** Yes — already fixed in this session. Setup contract now prepends `$_scope_dir` to PATH when `./printing-press` exists and `cmd/printing-press/` is present (confirming we're in the repo).
- **Inherent or fixable:** Fixed. Commit `bb33d2d`.
- **Durable fix:** Already applied to all 4 skill setup contracts.
- **Test:** Run any skill from inside the repo → `which printing-press` resolves to `./printing-press`.
- **Evidence:** `printing-press polish` returned "unknown command" because v0.4.0 doesn't have it.

### 6. Spec title apostrophe mangled CLI name (generator bug)

- **What happened:** Spec title "Domino's Pizza API" → `cleanSpecName()` → `domino-s-pizza` → CLI name `domino-s-pizza-pp-cli`. The apostrophe was converted to a hyphen, producing a bad directory and binary name.
- **Root cause:** `cleanSpecName()` replaces non-alphanumeric characters with hyphens. Apostrophes in brand names (Domino's, McDonald's, Wendy's) become hyphens.
- **Cross-API check:** Any API with an apostrophe in the title: Domino's, McDonald's, Lowe's, Macy's, Sam's Club, Kohl's. Common in food/retail APIs.
- **Frequency:** API subclass: brand names with apostrophes — maybe 5-10% of consumer APIs.
- **Fallback if machine doesn't fix it:** Claude manually edits the spec title before generation. Reliability: sometimes — Claude noticed it after the fact and regenerated, but it cost a full regeneration cycle.
- **Worth a machine fix?** Yes. Simple fix: strip apostrophes before hyphenating.
- **Inherent or fixable:** Fixable.
- **Durable fix:** In `cleanSpecName()` (internal/generator or internal/naming): strip apostrophes before the general non-alphanumeric-to-hyphen replacement. `"Domino's"` → `"Dominos"` → `"dominos"`.
  Condition: Spec title contains apostrophe
  Guard: Only strips apostrophes, not other punctuation (hyphens, dots already handled)
- **Test:** `cleanSpecName("Domino's Pizza API")` → `"dominos-pizza"` (not `"domino-s-pizza"`).
- **Evidence:** First generation produced `domino-s-pizza-pp-cli`, required spec edit and full regeneration.

## Prioritized Improvements

### Fix the Scorer
No scorer bugs identified in this run.

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 6 | Strip apostrophes in cleanSpecName | internal/naming | subclass: brand names | sometimes | small | apostrophe-only |
| 5 | Local build preference in setup contract | skills/*/SKILL.md | every repo run | never (wrong binary used) | small | already done |

### Do Next (needs design/planning)
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 1 | Auth-aware sniff flow when AUTH_SESSION_AVAILABLE | skills/printing-press/references/sniff-capture.md | most APIs | sometimes | medium | AUTH_SESSION_AVAILABLE gate |
| 2 | GraphQL BFF operation discovery | skills/printing-press/references/sniff-capture.md | subclass: GraphQL BFF (~25%) | never | medium | same-URL POST detection |
| 3 | Cookie auth propagation through sniff → spec → generator | sniff-capture.md + spec builder | subclass: cookie-auth sites | never | medium | depends on #1 |
| 4 | User-friendly command alias generation | generator templates or skill | most APIs | always but expensive (640 lines) | large | all APIs |

### Skip
| # | Fix | Why unlikely to recur |
|---|-----|----------------------|
| (none) | | |

## Work Units

### WU-1: Authenticated Sniff Flow (findings #1, #3)
- **Goal:** When AUTH_SESSION_AVAILABLE=true, the sniff automatically explores authenticated pages (account, order history, rewards, profile) and classifies endpoints as auth-required vs public. Cookie auth propagates through to the generated spec.
- **Target files:**
  - `skills/printing-press/references/sniff-capture.md` — add authenticated flow section to Step 2a.1
  - `skills/printing-press/SKILL.md` — ensure Phase 1.6 → Phase 1.7 → sniff-capture handoff preserves AUTH_SESSION_AVAILABLE
- **Acceptance criteria:**
  - Sniff a cookie-auth site while logged in → auth pages visited, auth-only endpoints in discovery report
  - Step 2d validates cookie replay → spec gets Auth.Type=cookie, CookieDomain set
  - Generated CLI has `auth login --chrome` when cookie auth detected
  - Sniff an anonymous site (no login) → auth flow skipped, no regression
- **Scope boundary:** Does NOT change the generator templates (auth_browser.go.tmpl already exists). Does NOT change specgen.go (already handles cookie auth from HAR). Only changes the skill instructions to feed the existing pipeline.
- **Complexity:** medium (skill instruction changes, 2 files, needs careful flow design)

### WU-2: GraphQL BFF Discovery (finding #2)
- **Goal:** When the sniff detects multiple POST requests to the same URL path (GraphQL BFF pattern), it extracts operation names from request bodies and builds the spec from GraphQL operations instead of REST paths.
- **Target files:**
  - `skills/printing-press/references/sniff-capture.md` — add Step 2a.2.5 for GraphQL detection
- **Acceptance criteria:**
  - Sniff a GraphQL BFF site → operation names extracted, spec groups operations by domain
  - Sniff a standard REST API → GraphQL detection skipped, no regression
- **Scope boundary:** Does NOT add GraphQL schema introspection. Only discovers operations from captured traffic. Does NOT change the generator's command naming (that's WU-3 territory).
- **Complexity:** medium (skill instruction changes, browser eval for POST body interception)

### WU-3: Strip Apostrophes in cleanSpecName (finding #6)
- **Goal:** Brand names with apostrophes produce clean CLI names without mangled hyphens.
- **Target files:**
  - `internal/naming/naming.go` (or wherever `cleanSpecName` lives — verify with grep)
- **Acceptance criteria:**
  - `cleanSpecName("Domino's Pizza API")` → `"dominos-pizza"`
  - `cleanSpecName("McDonald's API")` → `"mcdonalds"`
  - `cleanSpecName("Lowe's Home Improvement")` → `"lowes-home-improvement"`
  - `cleanSpecName("Stripe API")` → `"stripe"` (no regression for names without apostrophes)
- **Scope boundary:** Only strips apostrophes. Does not change handling of other punctuation.
- **Complexity:** small (1 file, 1 line change + tests)

## Anti-patterns

- **Declaring "no auth needed" based on the ordering API alone.** The REST ordering pipeline doesn't need auth, but the account/profile surface does. When an API has both public and authenticated surfaces, the sniff must explore both.
- **Stopping sniff exploration when community wrappers document the public API.** Community wrappers document the anonymous surface. The authenticated surface is often undocumented and only discoverable by sniffing with a logged-in session.

## What the Machine Got Right

- **Community wrapper research was thorough.** Found all major tools (node-dominos-pizza-api, apizza, MCPizza, pizzapi) and extracted accurate endpoint paths. The REST API probe confirmed all endpoints respond correctly.
- **Spec-from-research worked well.** Building an OpenAPI spec from community-documented endpoints produced a functional CLI that scored 84/100 on first generation. The spec had correct paths, parameters, and response schemas.
- **Generator quality gates all passed.** 7/7 gates on first try. No build failures.
- **Rate limiting compliance.** The sniff respected pacing rules and didn't trigger any 429s.
- **Setup contract local-build fix is clean.** Detects repo context via `cmd/printing-press/` directory, doesn't break standalone installs.
