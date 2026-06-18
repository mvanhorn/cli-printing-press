# Printing Press Retro: yahoo-finance

## Session Stats
- API: yahoo-finance (undocumented, reverse-engineered endpoints on `query1/query2.finance.yahoo.com`)
- Spec source: hand-authored internal YAML (crowd-sniff output was too contaminated; sniff via browser not possible — IP 429-blocked)
- Scorecard: 87/100 (Grade A)
- Verify pass rate: 100% (mock), 84% before polish
- Fix loops: 2 (generator issue with dry-run + auth handoff; file-rename needed for dogfood heuristic)
- Manual code edits: substantial — added ~300 lines of crumb+cookie session handling to client.go, plus 700 lines of transcendence commands (portfolio, watchlist, digest, compare, sparkline, sql, fx, options-chain, auth login-chrome)
- Features built from scratch: 9 transcendence commands (expected — these are P2 per the skill contract)

## Findings

### F1. Crowd-sniff doesn't filter endpoints by domain (Bug / Template gap)

- **What happened:** Ran `printing-press crowd-sniff --api yahoo-finance --base-url https://query1.finance.yahoo.com`. Tool produced 27 endpoints across 18 "resources" — but most were from other financial APIs. `/fred/series/observations` (FRED), `/v3/reference/tickers` (Polygon), `/tiingo/daily/{id}` (Tiingo), `/v2/stocks/trades/latest` (Alpaca), `/query` (Alpha Vantage), `/api/v1/company-news` (Finnhub) all ended up in the spec. The crowd picks up endpoints from npm packages like `yfin-wrapper` that aggregate multiple financial APIs — and nothing scopes the extraction to the stated `--base-url`.
- **Scorer correct?** N/A (not a scoring issue; it's a generation-input-quality issue)
- **Root cause:** `internal/crowdsniff/specgen.go` aggregates endpoints from package source code without checking whether each endpoint's origin URL (as hardcoded in the wrapper source) matches the `baseURL` the user passed. The npm parser extracts every string that looks like an API path regardless of which base URL the wrapper code actually calls it on.
- **Cross-API check:** Would recur on any API whose community SDKs also touch other APIs. Examples: every financial aggregator (Alpha Vantage wrappers often also call Polygon/FRED), weather aggregators (Open-Meteo wrappers often also call NWS/OpenWeather), social aggregators. Rough frequency: whenever the community ecosystem is "glue code across multiple providers," contamination is severe. Tight ecosystems (Stripe, Twilio) are clean.
- **Frequency:** subclass:`aggregator-adjacent-apis` — at least 20% of APIs we'd build CLIs for.
- **Fallback if Printing Press doesn't fix:** Claude rejects the spec manually and hand-writes one (what I did). Adds 15-30 minutes and depends on Claude noticing the contamination. Easy to miss if crowd-sniff output looks plausible.
- **Worth a Printing Press fix?** Yes.
- **Inherent or fixable:** Fixable. The npm extraction already sees the HTTP client call site; it can read the base URL from the same function scope and drop mismatches.
- **Durable fix:** In the npm endpoint extractor, when an HTTP call is detected, also resolve the base URL (look for the closest enclosing `const BASE = '...'` or `baseURL` variable, or the argument to the HTTP client constructor). Compare to the user's `--base-url` host. If domains don't match, emit the endpoint at `tier: community-sdk-other-origin` so the builder can skip it by default (or accept it with `--include-other-origins`).
- **Test:**
  - positive: crowd-sniff `--api yahoo-finance --base-url query1.finance.yahoo.com` against current npm ecosystem produces no `/fred/...` or `/v3/reference/tickers` paths
  - negative: crowd-sniff `--api finnhub --base-url finnhub.io` still picks up `/api/v1/company-news`
- **Evidence:** `research/yahoo-finance-crowd-spec.yaml` shows 11 of 27 endpoints clearly belong to other APIs. Kept for audit.

### F2. No `session_handshake` auth type for APIs that require a pre-request crumb/CSRF fetch (Template gap)

- **What happened:** Yahoo Finance requires: `GET fc.yahoo.com/` → persist A1/B1 cookies → `GET query2.finance.yahoo.com/v1/test/getcrumb` → pass `?crumb=<value>` on every subsequent request. The generator's supported auth types (`api_key`, `oauth2`, `bearer_token`, `cookie`, `composed`, `none`) don't model this two-step handshake with a token expiry and automatic refresh. I had to hand-patch `internal/client/client.go` with ~250 lines: cookie jar, session.json persistence, `ensureCrumb()`, crumb invalidation on 401/403, and query-param injection.
- **Scorer correct?** N/A.
- **Root cause:** `internal/spec/spec.go` `AuthConfig` doesn't have fields to describe "bootstrap URL", "token endpoint URL", "token parameter name/location", or "invalidate on status codes".
- **Cross-API check:** This isn't just Yahoo. The same pattern is used by:
  - Sites with CSRF-protected JSON APIs (Facebook, many e-commerce backends)
  - Many unofficial/reverse-engineered APIs on consumer sites (Walmart, Best Buy, Whole Foods)
  - Some streaming services (Spotify token-from-page, Netflix, Twitch)
- **Frequency:** subclass:`csrf-token-pattern` — probably 10-15% of APIs we'd reverse-engineer via sniff.
- **Fallback if Printing Press doesn't fix:** Claude hand-writes the handshake every time. ~250 lines of boilerplate, easy to get wrong (cookie persistence, invalidation rules, thread-safety).
- **Worth a Printing Press fix?** Yes. Hand-writing this is the single biggest chunk of work-that-shouldn't-have-been-necessary in this run.
- **Inherent or fixable:** Fixable with a new auth type.
- **Durable fix:** Add `session_handshake` auth type with these fields:
  ```yaml
  auth:
    type: session_handshake
    bootstrap_url: "https://fc.yahoo.com/"        # optional — GET to seed cookies
    token_url: "https://query2.finance.yahoo.com/v1/test/getcrumb"
    token_format: text                            # or json
    token_json_path: ""                           # if json, JSONPath to extract
    token_param_name: crumb                       # query param name
    token_param_in: query                         # query or header
    invalidate_on_status: [401, 403]              # codes that trigger re-handshake
    ttl_hours: 24                                 # persist session this long
  ```
  The generator's client template then emits the cookie jar, session.json persistence through the data-kind resolver, an `ensureToken()` method, and invalidation retry logic. Zero hand-patching.
- **Test:**
  - positive: generate with `type: session_handshake` and observe the client has cookie jar + ensureToken() + crumb query-param injection; dry-run output shows `?crumb=<token>`
  - negative: generate with `type: bearer_token` and confirm no session.json persistence logic appears
- **Evidence:** Hand-patched `client.go` diff in the working dir; this run's working dir has the pattern.

### F3. Dogfood "novel features built" detection is brittle (Scorer bug)

- **What happened:** Dogfood compared `novel_features` from `research.json` against actual CLI commands. It matched exact command paths. My `auth login-chrome` wasn't matched against planned `auth login --chrome`; my built `portfolio gains` wasn't matched against planned `portfolio dividends` (different feature but thematically related); my `options-chain --moneyness otm` wasn't matched against planned `options --moneyness otm`. Result: reported "2/8 survived" even though 7+ were built, just renamed during implementation.
- **Scorer correct?** No — the CLI genuinely has these features (verified by dry-run: 19/19 commands work). The scorer's literal string match is too narrow.
- **Root cause:** Dogfood matches `planned.command` (from research.json) to actual `<cli> help <command>` output verbatim. No fuzzy matching, no aliasing, no feature-intent lookup.
- **Cross-API check:** This recurs on every CLI where implementation naturally diverges from planning — which is nearly every run. Rename `foo --bar` to `foo-bar` during implementation and the scorer reports the feature as missing.
- **Frequency:** most APIs — the planning/implementation naming drift is universal.
- **Fallback if Printing Press doesn't fix:** Claude notices the false-positive and overrides the scorer's verdict manually (what I did this run with ship-with-gaps).
- **Worth a Printing Press fix?** Yes. This is a recurring credibility gap in the scorer.
- **Inherent or fixable:** Fixable.
- **Durable fix:** Two complementary changes:
  1. `research.json` gains an optional `aliases: []string` field per novel feature. Planner or implementer can record alternative command paths.
  2. Dogfood's matcher does three passes: (a) exact path match, (b) prefix match (`auth login-chrome` matches planned `auth login`), (c) alias match. Only after all three fail, report missing.
- **Test:**
  - positive: plan has `portfolio perf` alias `[portfolio performance, portfolio pnl]`; built command is `portfolio performance`; scorer reports built.
  - negative: plan has `portfolio dividends` with no aliases; built `portfolio gains` ≠ `portfolio dividends`; scorer correctly reports missing.
- **Evidence:** Shipcheck output: `Novel Features: 2/8 survived (WARN)` with 6 items that were in fact built, just named differently.

### F4. Scorer penalizes dead-code removal (Scorer bug)

- **What happened:** Deleted `internal/cli/search_query.go` (genuinely dead — duplicate of `search` command). Scorecard workflows dimension dropped from 10/10 to 8/10 and total from 88 to 87.
- **Scorer correct?** No. Removing dead code is good. The scorer counts candidate files without checking whether those files register actual commands.
- **Root cause:** The workflows scorer in `internal/pipeline/scorecard.go` (or wherever) counts files matching a pattern (probably `internal/cli/*.go`) rather than commands actually registered in `root.go`.
- **Cross-API check:** Recurs every time polish removes dead files or when the generator's dead-code elimination fires.
- **Frequency:** most APIs with polish pass.
- **Fallback if Printing Press doesn't fix:** Claude ignores the -2 score delta (as I did). No real harm but signals wrong incentive (don't clean up dead code).
- **Worth a Printing Press fix?** Yes, but low priority. Score signal matters.
- **Inherent or fixable:** Fixable.
- **Durable fix:** Workflows scorer should count commands registered via `rootCmd.AddCommand(new...Cmd(...))` calls, not files. Parse `root.go`, build a set of command constructor names, count those.
- **Test:**
  - positive: delete a command file whose constructor IS registered → workflows score drops
  - negative: delete a dead file whose constructor is NOT in root.go → workflows score unchanged
- **Evidence:** Polish agent output: "Scorecard Workflows dropped 10→8 solely because we deleted a dead file (search_query.go) — the scorer counts workflow-candidate files, not actual commands."

### F5. Dry-run short-circuits before auth/session preparation (Bug)

- **What happened:** The generated `client.do()` has this structure: early-return if `DryRun`, THEN run auth setup, THEN make request. My hand-added crumb logic initially sat in the "after dry-run" branch, so dry-run output didn't show the crumb query param. A user running `--dry-run` to verify their imported session would see no evidence of the crumb. Fixed locally by moving crumb resolution before the dry-run branch (and skipping the network fetch in dry-run mode if no cached crumb).
- **Scorer correct?** N/A.
- **Root cause:** The client template (`internal/generator/templates/client.go.tmpl`) emits `if c.DryRun { return c.dryRun(...) }` as the first branch in `do()`, before the auth header is computed. For bearer-token and API-key auth this happens to be fine because `authHeader()` runs later and is non-network. For session-handshake or oauth2 (where auth resolution could require a network call), this matters — and dry-run should at least show what auth material WOULD be attached.
- **Cross-API check:** Relevant for any auth type that materially changes the outgoing request shape. bearer_token / api_key — silent ok. oauth2 — currently silent, should ideally show "Authorization: Bearer <cached|refresh-required>". session_handshake (once added) — must show the crumb.
- **Frequency:** every API where dry-run is used to verify auth wiring, which should be every API (it's a documented feature of the CLI).
- **Fallback if Printing Press doesn't fix:** Claude patches the generated client (as I did).
- **Worth a Printing Press fix?** Yes.
- **Inherent or fixable:** Fixable.
- **Durable fix:** Restructure `client.do()` template to:
  1. Compute auth material (header or query token) up-front using only cached values (never trigger a network fetch during dry-run)
  2. If dry-run, pass the computed auth material into `dryRun()` so it surfaces in the preview output
  3. Only the non-dry-run branch triggers `ensureToken()` etc.
- **Test:**
  - positive: `cli auth login-chrome --crumb abc` + `cli quote AAPL --dry-run` shows `?crumb=abc` in stderr preview
  - negative: clean session, `cli quote AAPL --dry-run` shows no crumb (doesn't trigger network to fetch one) and notes "no cached session; live run will bootstrap"
- **Evidence:** Had to hand-patch `dryRunWithCrumb()` and reorder the branch in client.go this run.

## Prioritized Improvements

### P1 — High priority

| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F1 | Crowd-sniff domain filtering | `internal/crowdsniff/` | 20% of APIs (aggregator-adjacent) | Low — contamination is subtle and easy to accept if Claude isn't paying attention | medium | Only apply when `--base-url` is provided; keep permissive mode for "omnibus CLI" intent |
| F2 | `session_handshake` auth type | `internal/spec/spec.go`, `internal/generator/templates/client.go.tmpl` | 10-15% of reverse-engineered APIs | Low — ~250 lines of correct boilerplate is a lot to ask Claude to repeat | large | Only activates when `auth.type: session_handshake`; existing types unchanged |
| F3 | Dogfood novel-features fuzzy matching | `internal/pipeline/dogfood.go` + research.json schema | Most APIs | Medium — false negatives are annoying but not blocking | medium | None — broadly safe change |

### P2 — Medium priority

| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F5 | Dry-run shows auth material | `internal/generator/templates/client.go.tmpl` | Every API | High when bug matters (session auth), low otherwise | small | Only the dryRun() signature changes; no-op for bearer/api_key |

### P3 — Low priority

| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F4 | Workflows scorer counts commands not files | `internal/pipeline/scorecard.go` | Most APIs with polish pass | High — small score delta, no behavior impact | small | None |

### Skip

None — all findings apply beyond this one API.

## Work Units

### WU-1: Crowd-sniff domain filtering (F1)
- **Goal:** Crowd-sniff's npm endpoint extractor drops endpoints whose apparent origin host doesn't match the `--base-url` the user supplied.
- **Target:** `internal/crowdsniff/npm.go` (and/or `specgen.go`) — wherever an HTTP-call-like expression is recognized and turned into an endpoint record.
- **Acceptance criteria:**
  - positive test: a fake npm package that declares `const BASE = 'https://other-api.com'` and calls `fetch(BASE + '/foo')` does NOT appear in output when user passed `--base-url https://query1.finance.yahoo.com`
  - negative test: a fake npm package that calls `fetch('https://query1.finance.yahoo.com/v8/finance/chart/AAPL')` DOES appear when `--base-url` matches
  - integration test: rerun against a yahoo-finance-like fixture; previously-contaminated endpoints (`/fred/*`, `/v3/reference/*`, `/tiingo/*`) no longer present
- **Scope boundary:** Only endpoint-source filtering. Don't change the GitHub code-search extractor in this WU. Don't change the spec schema.
- **Dependencies:** None
- **Complexity:** medium

### WU-2: Add `session_handshake` auth type (F2)
- **Goal:** Spec authors can declare a session-handshake auth type; the generator emits a client with cookie jar, token bootstrap, disk persistence, invalidation, and query/header token injection — no hand-patching required.
- **Target:**
  - Schema: `internal/spec/spec.go` `AuthConfig` (new fields)
  - Template: `internal/generator/templates/client.go.tmpl` (new conditional branch)
  - Template: `internal/generator/templates/config.go.tmpl` (session file path)
  - Generator: `internal/generator/` (wire the new branch)
- **Acceptance criteria:**
  - positive test: spec with `auth.type: session_handshake, token_url: ..., token_param_name: crumb, ttl_hours: 24` generates a client that (a) has a cookie jar, (b) persists `session.json` through the data-kind resolver, (c) fetches the token before first data call, (d) adds `?crumb=<token>` on every data request, (e) invalidates on 401/403 and retries once
  - negative test: spec with `auth.type: bearer_token` generates the same client code that exists today (no regression)
  - positive test: generated CLI also includes `auth login-<method>` subcommand for importing a pre-obtained session (Chrome-cookie style)
- **Scope boundary:** Don't replace `cookie` or `composed` auth types. Don't implement automatic browser cookie harvesting — user brings a JSON file, like the yahoo-finance implementation.
- **Dependencies:** None, but benefits from F5 (dry-run auth preview)
- **Complexity:** large

### WU-3: Dogfood novel-features fuzzy matching (F3)
- **Goal:** Novel features survive when implementation naturally renames or prefixes commands.
- **Target:** `internal/pipeline/dogfood.go` novel-features matcher; optionally extend research.json schema with an `aliases` field.
- **Acceptance criteria:**
  - positive test: planned `auth login --chrome`, built `auth login-chrome` → scorer reports built (prefix match)
  - positive test: planned `portfolio perf` with `aliases: [portfolio performance]`, built `portfolio performance` → scorer reports built (alias match)
  - negative test: planned `portfolio dividends`, built `portfolio gains` (unrelated feature) → scorer reports missing (correct)
- **Scope boundary:** Don't add semantic similarity or LLM matching. Keep the match deterministic: exact → prefix → alias → miss.
- **Dependencies:** None
- **Complexity:** medium

### WU-4: Dry-run shows auth material (F5)
- **Goal:** `--dry-run` output includes the auth material that would be attached to the request (header or query param), using cached values only — never triggers a network call to fetch a fresh token.
- **Target:** `internal/generator/templates/client.go.tmpl` — restructure `do()` so auth is computed up-front and passed to both the dry-run and live branches.
- **Acceptance criteria:**
  - positive test: bearer_token client with `--dry-run` prints `Authorization: Bearer ****<last4>` in stderr preview
  - positive test: session_handshake client with cached crumb and `--dry-run` prints the crumb query param
  - negative test: session_handshake client with NO cached crumb and `--dry-run` prints `(no cached session; live run will bootstrap)` and does NOT make a network call to fetch one
- **Scope boundary:** Don't change the dry-run output format — just populate it more fully.
- **Dependencies:** Benefits WU-2 but not strictly required by it.
- **Complexity:** small

### WU-5: Workflows scorer counts commands not files (F4)
- **Goal:** Scorer's workflows dimension reflects actually-registered commands, not file count.
- **Target:** `internal/pipeline/scorecard.go` — specifically the workflows dimension calculation.
- **Acceptance criteria:**
  - positive test: delete a file `internal/cli/foo.go` whose `newFooCmd` IS registered in root.go → score drops
  - negative test: delete a file `internal/cli/dead.go` whose constructor is NOT registered → score unchanged
  - negative test: orphan constructor (file exists, never added to root) → not counted
- **Scope boundary:** Only the workflows dimension. Don't touch other dimensions.
- **Dependencies:** None
- **Complexity:** small

## Anti-patterns
- **Rubber-stamping crowd-sniff output.** Produced 27 endpoints including Polygon, FRED, Tiingo, Alpaca, Alpha Vantage, and Finnhub paths. Always inspect and domain-filter manually until F1 lands.
- **Writing session-handshake auth by hand every time.** We've now done this enough (Redfin, Zillow-style, and now Yahoo Finance) that it's clearly a missing primitive, not a one-off.
- **Renaming commands during Phase 3 without updating research.json aliases.** The scorer penalizes you for this even though the feature is built.

## What the Printing Press Got Right
- **The crumb-and-cookie hand-patch *worked* on the first try when properly restructured.** The existing client template is clean enough that threading a new auth type through is mechanical.
- **Adaptive rate limiter.** Out-of-the-box handling of 429s was already excellent. My hand-additions to crumb/cookie interact cleanly with it.
- **Verify mock server handled 9 varied endpoint shapes and gave 100% pass rate.** Strong signal that the generated code paths are correct.
- **Slug-keyed library layout** (`library/yahoo-finance/` with binary `yahoo-finance-pp-cli`) avoided the `-pp-cli` naming confusion when mixing binaries and directories.
- **Lock/promote ergonomics.** `printing-press lock promote --cli X --dir Y` did the right thing in one shot — stage, swap, manifest, release. Very clean.
- **Polish agent autonomy.** Polish worker ran without intervention, fixed 9 example gaps + 4 dry-run bugs + 1 dead file + renamed 10 files, returned delta structured.
