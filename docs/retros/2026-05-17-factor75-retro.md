# Printing Press Retro: factor75

## Session Stats

- API: factor75 (Factor75 / HelloFresh BFF on `www.factor75.com`, Auth0 SPA SDK + Cloudflare)
- Spec source: browser-sniffed, hand-augmented; the printed CLI has been live for some time and was being dogfooded against a real user account
- Retro trigger: live dogfooding turned up `deliveries pending` misclassifying the user's already-customized cart as "auto-preselected." Three further machine-side gaps surfaced during the debug session.
- Outcome: printed-CLI fix shipped in the same change as this retro (syncer.go now forwards `servings` + `delivery-option` to `/gw/my-deliveries/menu`). Three systemic findings filed against the machine.

## Findings

### F1. Hand-authored sync drops URL personalization params; cart shape silently degrades to generic preselect

- **What happened:** The Factor75 printed CLI's `internal/syncer/syncer.go` calls `GET /gw/my-deliveries/menu?week=...` with 5 query params (`week`, `country`, `locale`, `subscription`, `product-sku`). The live website calls the same endpoint with 11 params, adding `servings`, `delivery-option`, `postcode`, `preference`, `customerPlanId`, and `include-future-feedback`. With the trimmed param set, the gateway returns a generic plan preselect (3 distinct meals × qty 6 = 18) instead of the user's actual cart (11 distinct meals × qty 1–3 = 15). The CLI's local store has been silently storing a non-user-specific snapshot for weeks; downstream features (`deliveries pending` classification, `favorites suggest` recency-exclusion, `history` analytics) all inherit the bad data.
- **Scorer correct?** N/A — neither the live-dogfood gate nor scorecard compares the agent's hand-written HTTP request shape against the browser-sniff capture of the same endpoint. Both signed off.
- **Root cause:** Hand-authored sync code under `internal/syncer/` is exempt from `cobratree`'s mechanical endpoint surface checks. The agent who wrote the syncer picked the params that looked obviously needed and dropped the rest. There is no skill checklist gate or scorer rule that flags param-cardinality drop against the captured network log for the same endpoint.
- **Cross-API check:** Will recur on any CLI whose sync is hand-authored against a sniffed BFF endpoint that personalizes its response based on plan / locale / household params. Examples likely affected today by inspection of similar shapes:
  - HelloFresh / Green Chef / EveryPlate (same BFF family as Factor75 — `/gw/my-deliveries/menu` is the HF whitelabel endpoint, every brand on this gateway has the same personalization param contract)
  - Most subscription-meal / household-plan APIs (Blue Apron, Sun Basket if reverse-engineered, Daily Harvest)
  - Any sniffed CLI whose sync target is a list-or-menu endpoint with `customerPlanId`/`accountId`/`tenantId`-style scoping
- **Frequency:** subclass `personalization-param-drop`. Tightly bounded to APIs whose response *shape* matches across personalized vs default modes but whose *content* diverges — the difference isn't visible at structural test time, only when comparing against the user's reality.
- **Fallback if Printing Press doesn't fix:** the printed CLI ships with stale data forever, and the user only catches it by visually comparing the CLI to the website. Caught in this run because the user pointed at the website mid-debug; would not have been caught by any existing gate.
- **Worth a Printing Press fix?** Yes — this is a class of bug, not a one-off.
- **Inherent or fixable:** Fixable. The sniff capture already records the full URL (with all 11 params) for each endpoint. A skill-side review gate or scorer check can grep the generated/hand-authored sync code for the endpoint path and compare param sets.
- **Durable fix (skill + scorer):** Two complementary gates:
  1. **Skill Phase 4.x diff check:** for every `client.Get(<path>, params)` (or equivalent for POST bodies) the printed CLI invokes in its sync path, locate the matching entry in `research.json` / browser-sniff `traffic-analysis.json` for the same path. If the captured query set is a strict superset of what the code passes, fail the phase with the dropped keys named in the diagnostic. Acceptance: a Factor75-shaped diff (5 of 11 params used) flags loudly.
  2. **Scorer dogfood enrichment (`comp:scorer`):** Add a `personalization_param_drop` check that compares the live CLI's outgoing request URLs (capturable via the existing live-dogfood request log) to the sniff capture for the same endpoint. Same diff, different layer. Catches drift introduced after the spec was authored.
- **Test:**
  - positive: fixture spec where browser-sniff captured `GET /menu?a=1&b=2&c=3&d=4` and the generated/authored sync code calls `GET /menu?a=1&b=2` — flag with "dropped params: c, d on /menu".
  - positive: same path called with extra params not in the capture (`a,b,c,d,e`) — no flag.
  - negative: path that doesn't appear in the sniff capture (transcendence-only synthetic call) — no flag.
- **Evidence:** `~/printing-press/library/factor75/internal/syncer/syncer.go:102-112` (pre-fix, 5 params); browser-sniff network log row for `/gw/my-deliveries/menu` shows 11 params at request time; comparison against the user's website-rendered W22 cart shows distinct meals = 11 vs CLI store = 3.

### F2. `looksLikeJWT` accepts short tracking cookies; `auth login --chrome` saves them silently

- **What happened:** `factor75-pp-cli auth login --chrome` reported `OK Extracted JWT from cookie jar for factor75.com` and saved a 31-character string (`01KRPVRYA2SNQT9BAGD6984WAG_.tt.1`) as the access token. That string is a Cloudflare visitor/session cookie that happens to have three dot-separated base64url segments. Every subsequent API call returned HTTP 401 with `"invalid character 'Ó' looking for beginning of value"` — the gateway tried to JSON-decode the JWT payload and got binary garbage.
- **Scorer correct?** N/A — this is generator-emitted runtime code, not scored content.
- **Root cause:** `internal/cli/auth.go::looksLikeJWT` (lines 824-846 in the Factor75 printed CLI, but this is generator-emitted template code) validates a JWT by checking segment count (3) and per-segment charset (base64url). It does not validate minimum length. A real Auth0 RS256 access token is 800+ characters; a Cloudflare `__cf_bm`-shaped cookie can be 30. Both pass the heuristic.
- **Cross-API check:** Every sniffed CLI emitted from the `auth login --chrome` (cookie-jar) template inherits this `looksLikeJWT`. The bug fires whenever the target domain's cookie jar contains any value with 3 base64url segments — Cloudflare bot-manager cookies (`__cf_bm`), some Mixpanel distinct IDs, segment-style A/B test identifiers, etc. Tightly bounded by which sites front their auth with Cloudflare, but that's a large fraction of consumer SaaS today.
- **Frequency:** subclass `false-positive-jwt-shape`. Any Cloudflare-fronted site (Factor75 here; likely Substack, Reddit-adjacent, many SaaS consumer endpoints). The CF cookie is set even when the user *is* authenticated, so this isn't restricted to "unauthenticated" scrapes.
- **Fallback if Printing Press doesn't fix:** the user gets a confusing 401 chain that points at "Token is expired" or "invalid character" decode errors and has to manually inspect `config.toml` to discover the saved "JWT" is 31 chars long.
- **Worth a Printing Press fix?** Yes — the heuristic is the gate; tightening it costs nothing.
- **Inherent or fixable:** Fixable. RS256/HS256 Auth0/OIDC tokens have predictable minimum sizes: the header alone is typically 36+ chars after base64url, the payload 100+, signature 256+ for RS256. A floor of total length ≥ 150 chars with header segment ≥ 20 chars eliminates every observed false-positive without rejecting legitimate JWTs (smallest realistic JWT seen in the wild ≈ 200 chars total).
- **Durable fix (template):** In `internal/cli/auth.go::looksLikeJWT` template:
  - Reject if total length < 150 chars
  - Reject if header segment < 20 chars
  - Keep current segment-count + charset checks
  - Emit a more specific user-facing diagnostic when the rejection is "looks like a JWT but is too short" — "this looks like a Cloudflare or tracking cookie, not a JWT; check the site's auth flow."
- **Test:**
  - positive: real Auth0 RS256 access token (800+ chars) accepted
  - positive: minimal HS256 JWT with realistic 30-char header / 80-char payload / 40-char signature (~150 chars) accepted
  - negative: `01KRPVRYA2SNQT9BAGD6984WAG_.tt.1` rejected with the "too short" reason
  - negative: `__cf_bm=AAA.BBB.CCC` shape rejected
- **Evidence:** `/Users/noway/.config/factor75-pp-cli/config.toml` post-`auth login --chrome` contained the short cookie verbatim; `find` in Chrome localStorage for the same domain showed no JWT-shaped key (confirming no localStorage path either, see F3).

### F3. `auth login --chrome` cannot reach Auth0 SPA SDK tokens stored in-memory

- **What happened:** Factor75 uses the Auth0 SPA SDK with the `memory` cache option — the access token lives in JavaScript heap memory and is injected into each XHR via an HTTP interceptor. It is **not** in the cookie jar (only short-lived session/CSRF cookies are) and it is **not** in localStorage (we confirmed by enumerating every key on factor75.com). `auth login --chrome` shells out to `pycookiecheat`/`cookies`, both of which are cookie-SQLite readers; neither has any path to in-memory state or localStorage. Net effect: no working Chrome-driven auth import for any Auth0-SPA-SDK-on-`memory` site. The workaround is to copy-paste the `Authorization: Bearer ...` value from DevTools Network tab.
- **Scorer correct?** N/A.
- **Root cause:** The `auth login --chrome` template only knows one strategy (cookie jar → `findJWTInCookies`). It has no fallback for the (increasingly common) case where the SPA SDK holds the token in JS-memory. There's also no detector that warns "the captured network log shows `/oauth/token` calls but the site exposes no JWT-shaped cookie" — that signature would predict the gap before the user hits it.
- **Cross-API check:** Affects every site using Auth0 SPA SDK v2+ with `cacheLocation: 'memory'` (which is the SDK default since v2.0). Factor75 / HelloFresh family is one example. Anecdotally many B2C SaaS use this pattern post-2024 because `cacheLocation: 'localstorage'` was deprecated as XSS-unsafe. The localStorage variant would at least be readable through a different extractor.
- **Frequency:** subclass `auth0-spa-in-memory`. Bounded to Auth0 SPA SDK users but that's a meaningful slice of B2C SaaS.
- **Fallback if Printing Press doesn't fix:** Manual DevTools paste each time the token expires (every 30 minutes for Factor75). The user also needs to know how to edit `config.toml` directly because no `auth set-token <jwt>` subcommand exists.
- **Worth a Printing Press fix?** Yes — both for the import path *and* for the missing escape hatch.
- **Inherent or fixable:** Two-part fixable.
  - **Detection (sniff-time):** if the browser-sniff capture has `/oauth/token` calls and the response carries `access_token` but no JWT-shaped cookie is observed in the response Set-Cookie headers, mark the site as `auth0_spa_in_memory: true` in the spec / research.json. This routes the generator to emit a different `auth login` flow.
  - **Runtime extraction:** spawn Chrome via CDP with `--remote-debugging-port`, attach to an open factor75.com tab, execute `window.<auth0SdkVariable>.getTokenSilently()` from page context to extract the token. Falls back gracefully when the SDK variable isn't pinnable. Alternatively, install a service-worker-style fetch interceptor via CDP `Fetch.enable` and grab the next outbound Authorization header.
  - **Escape hatch:** a built-in `auth set-token <jwt>` subcommand so the manual paste flow doesn't require editing config.toml. Validates against `looksLikeJWT` (post-F2 fix). Trivial to add to the auth template.
- **Test:**
  - positive: fixture sniff capture with `/oauth/token` + `cacheLocation: memory` Auth0 SDK signature → spec carries `auth.subtype: auth0_spa_in_memory` and generated `auth login --chrome --auth0-spa` uses CDP path.
  - positive: `auth set-token <real-jwt>` saves and validates; subsequent `doctor` passes.
  - negative: `auth set-token <31-char-cookie>` is rejected by `looksLikeJWT` floor (F2's fix).
- **Evidence:** factor75.com localStorage enumeration showed 50+ keys, zero matching `/auth|token|jwt|bearer/i`; cookie jar contained only Cloudflare/CSRF cookies (no JWT shape); the user's manual workflow (paste from DevTools Network → Authorization header → manually edit `config.toml`) was the only path that worked.

## Frequency rollup

- **F1:** subclass `personalization-param-drop` — every CLI whose hand-authored sync targets a personalization-aware list/menu endpoint. Tight enough not to require a generator change, broad enough to need a skill-level gate.
- **F2:** subclass `false-positive-jwt-shape` — every CLI using the `auth login --chrome` cookie-jar template against a Cloudflare-fronted site (large fraction).
- **F3:** subclass `auth0-spa-in-memory` — every Auth0 SPA SDK v2+ site with default `cacheLocation: memory` (growing).

## Related printed-CLI fix (already shipped in this change)

`~/printing-press/library/factor75/internal/syncer/syncer.go`:
- Added `DeliveryOption.Handle` to `apiDelivery` struct.
- Per-week `menuParams` now sources `subscription`/`product-sku` from the current week's delivery (not the primary), and adds `servings` and `delivery-option`.
- Test added: `TestSyncForwardsPersonalizationParamsToMenuEndpoint`.

The `customerPlanId`/`postcode`/`preference` params are still missing; verifying whether they're additionally required will need a fresh JWT round trip. If the 4-param-added shape still returns the generic preselect, a follow-up will introduce a `/gw/v1/profile/me` call to source those.

## Labels (intent — apply on file)

`retro`, `priority:P2` (dominant), `comp:skill` (F1's primary gate), `comp:generator` (F2 + F3 template work), `comp:scorer` (F1's scorer-side enrichment).
