---
title: 'feat: Promote html_scrape end-to-end (selector + specgen) when SSR state-blob detected alongside captcha-protected JSON'
type: feat
status: active
date: 2026-05-10
---

# feat: Promote html_scrape end-to-end (selector + specgen) when SSR state-blob detected alongside captcha-protected JSON

## Summary

Wire the browser-sniff pipeline to emit `html_extract.mode: embedded-json` specs when a HAR shows captcha-protected JSON endpoints alongside an SSR state-blob HTML sibling on the same registered domain. The change spans three layers: the existing `isSSREmbeddedData` detector is extended to recognize Yandex's `state-view` and Nuxt's `__NUXT__` / `__APP_INITIAL_STATE__` signatures (today it only covers `__NEXT_DATA__` / `window.__` / `application/ld+json`); the reachability mode selector adds a fifth recommendation that overrides `browser_required` when the two-signal conjunction holds; and the spec-generator's `inferHTMLExtract` learns to emit `embedded-json` mode with the script selector keyed to the matched signature. Today's sniff pipeline only ever emits `page` or `links` modes — `embedded-json` is reachable only via hand-authored or catalog-supplied specs.

---

## Problem Frame

Issue [#953](https://github.com/mvanhorn/cli-printing-press/issues/953) (P1, retro). The original framing proposed a 470-LOC new `internal/scrape/` generator-template family. Audit (posted to #953) ruled that out — the spec layer already accepts `html_extract.mode: embedded-json` (added by the food52 retro), and the runtime correctly handles the resulting CLI shape. Re-audit during this plan's doc-review pass revealed a second misconception: the sniff pipeline DOES NOT emit `embedded-json` mode today. The spec layer's acceptance of that mode is reachable only via operator-authored or catalog-supplied specs — sniff-derived specs go through [`internal/browsersniff/specgen.go`](../../internal/browsersniff/specgen.go) `inferHTMLExtract` which only produces `HTMLExtractModePage` or `HTMLExtractModeLinks`.

Current state by layer:

| Layer | Status | Reference |
|---|---|---|
| Catalog `integration_mode` enum | ✓ `html-scrape` accepted (wrapper-library metadata) | [`internal/catalog/catalog.go:107`](../../internal/catalog/catalog.go#L107) |
| Spec `html_extract.mode` parsing | ✓ `embedded-json` mode defined | [`internal/spec/spec.go:48-54`](../../internal/spec/spec.go#L48) |
| Runtime spec consumption | ✓ generator emits correct CLI shape when spec has embedded-json | (food52 retro work) |
| HAR protocol detection | ✓ `ssr_embedded_data` emitted at 0.85 confidence | [`internal/browsersniff/analysis.go:677-680`](../../internal/browsersniff/analysis.go#L677) |
| State-blob signature coverage | ⚠ only `__NEXT_DATA__` / `window.__` / `application/ld+json` | [`internal/browsersniff/analysis.go:1331-1337`](../../internal/browsersniff/analysis.go#L1331) |
| Reachability mode selector | ✗ chooses among 4 modes; never `html_scrape` | [`internal/browsersniff/analysis.go:902-928`](../../internal/browsersniff/analysis.go#L902) |
| **Sniff → spec emission of `embedded-json`** | ✗ `inferHTMLExtract` only emits `page` / `links` | [`internal/browsersniff/specgen.go:771-782`](../../internal/browsersniff/specgen.go#L771) |

The Yandex Maps retro evidence: JSON `/maps/api/*` endpoints were captcha-gated, but consumer HTML pages on the same registered domain were cold-fetchable and carried a `<script type="application/json" class="state-view">` blob containing the same structured data. Today the selector routes this to `browser_required`; specgen emits `mode: page`. The right outcome is `html_scrape` mode + `mode: embedded-json` spec, which generates a goquery-based extraction CLI instead of a Playwright shell.

**Honest framing on impact.** The original issue claimed "15-25% of curated catalog." Audit shows `kayak` is the only direct fit in the curated catalog today; `google-flights` is wrapper-only; remaining 19 entries are clean JSON. Accurate framing: second occurrence of the pattern in the operator-driven print stream — food52 motivated the spec-level support, yandex motivates analyzer + specgen completion. No quantified catalog-percent claim.

---

## Requirements

- **R1.** The HAR state-blob detector (`isSSREmbeddedData`) recognizes the three signatures the rescope cited that it does not currently match: `state-view`, `__NUXT__`, `__APP_INITIAL_STATE__`. Existing matches (`__NEXT_DATA__`, `window.__`, `application/ld+json`) stay.
- **R2.** State-blob detection requires HTTP status `2xx` (`200-299`) — `304 Not Modified` and similar cached responses are accepted — and body ≥ 10 KB. Smaller bodies likely represent empty templates or challenge pages; the floor rejects those. (Calibration note: original 50 KB threshold was derived from Yandex captures of 540 KB+; lowered to 10 KB after doc review surfaced asymmetric risk — false negatives are silent, false positives are noisy and self-correcting.)
- **R3.** The detector surfaces which state-blob signature matched (one of `__NEXT_DATA__`, `__NUXT__`, `__APP_INITIAL_STATE__`, `state-view`, `application/ld+json`, `window.__`). Downstream consumers need this to pick the right CSS selector for spec emission — `script#__NEXT_DATA__` for Next.js, `script.state-view` for Yandex, etc.
- **R4.** The reachability mode selector adds a fifth recommendation: `html_scrape`. Promotes when both signals are present in the HAR:
  - At least one entry with `Classification == "api"` carries a captcha-tier protection signal (`captcha`, `bot_challenge`, `aws_waf`, `vercel_challenge`) — and that protection signal is attributed to the API entry itself (via `ProtectionObservation.Evidence[i].EntryIndex`), not just present anywhere in the HAR. Cloudflare / Akamai / etc. keep their existing routes (lighter modes that can still reach JSON).
  - At least one HTML response (detected by `Content-Type` containing `html` — the classifier returns `noise` for HTML, so we cannot use Classification here) on the **same registered domain (eTLD+1)** as the protected API entry emits the `ssr_embedded_data` protocol observation. Subdomain-split shapes like `api.example.com` + `www.example.com` qualify.
- **R5.** Priority: `html_scrape` mode overrides `browser_required` when the two-signal conjunction holds. Other modes (`standard_http`, `browser_http`, `browser_clearance_http`) are unchanged because the captcha-tier protection set that triggers `html_scrape` is a strict subset of what triggers `browser_required` today — by construction, this addition affects only the `browser_required` slice of the cascade.
- **R6.** When mode is `html_scrape`, the emitted spec carries `html_extract.mode: embedded-json` with a `script_selector` derived from the matched signature. The mapping:
  - `__NEXT_DATA__` → `script#__NEXT_DATA__`
  - `__NUXT__` → `script#__NUXT__`
  - `__APP_INITIAL_STATE__` → `script#__APP_INITIAL_STATE__`
  - `state-view` → `script.state-view`
  - `application/ld+json` → `script[type="application/ld+json"]`
  - `window.__` → no script-tag selector; emit `mode: embedded-json` with a comment noting hand-tuning needed
- **R7.** Regression: a HAR without the two-signal conjunction routes through the existing four-mode cascade unchanged. Clean JSON HARs still go to `standard_http`; captcha-only HARs (no SSR sibling) still go to `browser_required`.

---

## Key Technical Decisions

### KTD-1. Extend the existing detector and protocol; do not introduce new labels

`ssr_embedded_data` is already in the protocol enum at 0.85 confidence. Extend `isSSREmbeddedData` to cover the three new signatures rather than adding a new protocol label. Keeps the enum stable; downstream consumers (existing test fixtures, scoring) don't need updates.

### KTD-2. Surface the matched signature on the protocol observation

The selector and specgen both need to know which signature matched (so the script-selector can be picked correctly per R6). Two options:
- Store the signature in `ProtocolObservation.Details` (existing map field) under a key like `signature`.
- Add a new `Signature` field to `ProtocolObservation` (more invasive).

Pick the first — `Details` is the right home for protocol-specific data, no struct surgery, and the field is already part of the JSON payload. Selector reads `protocols[ssr_embedded_data].Details["signature"]`; specgen reads the same.

### KTD-3. Same-registered-domain (eTLD+1) matching, not literal hostname

Use the Go ecosystem's `golang.org/x/net/publicsuffix` (or `net/url` if a sufficient helper exists) to compute eTLD+1. The literal-hostname check rejects the canonical Yandex-shape capture where API and HTML live on different subdomains; the eTLD+1 check accepts it. The cross-domain false-positive risk (captcha-protected API on one origin + unrelated marketing SSR on a different origin) is tolerated because (a) the matched pair must share the registered domain — coincidence across organizations is implausible — and (b) the captcha-tier protection requirement is itself a strong filter against benign captures.

### KTD-4. Captcha-tier protection only; not cloudflare/akamai/etc.

The promotion fires only when an API entry carries `captcha`, `bot_challenge`, `aws_waf`, or `vercel_challenge`. These are "JSON is unreachable without a browser" signals. Cloudflare / Akamai / Datadome / PerimeterX / protected_web / login_redirect can frequently be cleared with bearer tokens or session cookies, so those entries should continue routing to `browser_clearance_http` or `browser_http` (lighter modes than `html_scrape`). This means the new branch never pulls a HAR away from a lighter mode — it only pulls from `browser_required` toward `html_scrape`.

### KTD-5. Protection-to-entry attribution via EvidenceRef.EntryIndex

`hasProtection` walks the analysis-level Protections list (one observation per detected protection). Each observation carries `Evidence[i].EntryIndex` linking back to the entry that surfaced the signal. To require "the API entry itself is protected," the new check looks up evidence entries' indices and tests their `Classification`. Naive `hasProtection(captcha) && hasSSREmbeddedData()` would also fire on benign HARs where the SSR HTML page is itself behind Cloudflare (emitting a `cloudflare` protection signal from its own `cf-ray` header) and the JSON is unprotected — the entry-level join prevents that.

### KTD-6. Content-type-based check for the HTML side; not Classification

The classifier returns `"api"` or `"noise"` only — text/html entries earn -2 and land in `"noise"`. R3's "HTML-classified entry" is not a real category. The HTML side of the same-eTLD+1 join walks raw `entries`, filters by `strings.Contains(strings.ToLower(entry.ResponseContentType), "html")`, and tests for the `ssr_embedded_data` protocol on that entry's `EntryIndex`.

### KTD-7. Cascade insertion: after browser_required, conditional override

The existing selector at [`analysis.go:902-928`](../../internal/browsersniff/analysis.go#L902) is a mix of fall-through and guarded branches. The captcha branch (line 907) sets `browser_required` with no guard against override. The new `html_scrape` branch is inserted after the captcha/bot/aws_waf/vercel_challenge branches. It guards on the explicit two-signal conjunction (KTD-5 + KTD-6) and overrides `browser_required` when fired. Other branches that gate on `mode == "standard_http"` (lines 917, 922) are unaffected — they never run when `mode == "html_scrape"` because the override happens after them in real terms (the new branch fires only when captcha/bot has already promoted to `browser_required`).

### KTD-8. Signature passing into specgen: read from ReachabilityAnalysis

`specgen.inferHTMLExtract` is per-EndpointGroup; ReachabilityAnalysis is HAR-level. Plumb the signature down by:
- Adding the signature to `ReachabilityAnalysis` (one field — the recommended html_extract mode and matched script_selector if any).
- `AnalyzeCapture` (specgen.go) calls `classifyReachability` and threads the result into `inferHTMLExtract` via a new parameter or via `EndpointGroup` metadata set during capture analysis.

The implementer picks the exact threading mechanism during work; the plan-time decision is "the signature must reach specgen" not "via this specific struct field."

### KTD-9. Selector emits a script_selector hint; specgen finalizes it

The selector (analyzer side) emits which signature matched. `inferHTMLExtract` consumes that and maps to the canonical CSS selector per R6's table. The mapping table lives in spec.go or specgen.go (closer to specgen since it produces the spec). Keeps the analyzer free of CSS-selector vocabulary; keeps specgen as the single source of truth for what selectors map to.

### KTD-10. No new dependencies except `golang.org/x/net/publicsuffix`

eTLD+1 resolution requires the public suffix list. Go's ecosystem has `golang.org/x/net/publicsuffix` which is the standard choice and likely already pulled in transitively. If not, this is the one new dependency the plan requires. No other new packages.

---

## Implementation Units

### U1. Extend SSR state-blob detector + surface matched signature

**Goal:** Update `isSSREmbeddedData` to detect the three new signatures, add status-class and body-size floors, and return *which* signature matched (not just a bool). Store the signature on the protocol observation's `Details` map.

**Requirements:** R1, R2, R3.

**Dependencies:** none.

**Files:**
- `internal/browsersniff/analysis.go` (modify `isSSREmbeddedData` at line 1331 — refactor signature to return `(matched bool, signature string)`)
- `internal/browsersniff/analysis.go` (modify the detector call site at line 677-680 — pass the signature into the protocol observation's `Details` map)
- `internal/browsersniff/analysis_test.go` (extend tests)

**Approach:**
- Refactor `isSSREmbeddedData(entry EnrichedEntry) bool` → `detectSSREmbeddedData(entry EnrichedEntry) (matched bool, signature string)`. Returns the signature label (one of the six per R3) when matched, empty string otherwise.
- Add status-class check (`entry.ResponseStatus >= 200 && entry.ResponseStatus < 300`) at the top of the function. Rejects challenge pages (403/429/503) and error templates.
- Add body-size check (`len(entry.ResponseBody) >= 10_000`) — calibrated lower than the original 50 KB per doc-review. Named const `ssrEmbeddedDataMinBodySize`.
- Signature priority for the case of multiple matches in one body: pick the first matched in the order listed in R3 (the order is roughly framework-specific → generic, so Next.js wins over `window.__` when both present, which is the typical Next.js shape).
- Update the call site at line 677-680: receive the signature and store as `Details["signature"]` on the `ssr_embedded_data` protocol observation.

**Patterns to follow:** Existing detector body shape at [`analysis.go:1331-1337`](../../internal/browsersniff/analysis.go#L1331); the `Details` map convention used by other detectors (search for `Details: map[string]string{` in analysis.go for existing examples).

**Test scenarios:**
- HTML response with `state-view` script tag, status 200, 20 KB body → matched, signature == `state-view`.
- HTML response with `__NUXT__` script tag, status 200, 20 KB body → matched, signature == `__NUXT__`.
- HTML response with `__APP_INITIAL_STATE__`, status 200, 20 KB body → matched, signature == `__APP_INITIAL_STATE__`.
- HTML response with `__NEXT_DATA__`, status 200, 20 KB body → matched, signature == `__NEXT_DATA__` (regression).
- HTML response with `application/ld+json`, status 200, 20 KB body → matched, signature == `application/ld+json` (regression).
- HTML response with `window.__INITIAL_STATE__`, status 200, 20 KB body → matched, signature == `window.__` (regression).
- HTML response with both `__NEXT_DATA__` and `application/ld+json`, status 200, 20 KB body → matched, signature == `__NEXT_DATA__` (priority).
- HTML response with `__NEXT_DATA__`, status 200, 5 KB body → NOT matched (body floor).
- HTML response with `__NEXT_DATA__`, status 403, 20 KB body → NOT matched (status floor — challenge page).
- HTML response with `__NEXT_DATA__`, status 304, 20 KB body → NOT matched (status floor — only 2xx promotes; 304 is treated as a non-fresh response we cannot inspect reliably).
- Non-HTML response (JSON, XML) with state-blob marker substring → NOT matched (existing content-type guard).
- Call-site verification: a fixture HAR with one SSR HTML entry produces a protocol observation with `Details["signature"]` populated.

**Verification:** `go test ./internal/browsersniff/ -run SSR` passes. `go vet` clean.

---

### U2. Mode selector adds `html_scrape` on captcha-tier + same-eTLD+1 SSR sibling

**Goal:** Teach the reachability selector to override `browser_required` with `html_scrape` when an API entry carries captcha-tier protection AND an HTML entry on the same eTLD+1 emits the `ssr_embedded_data` protocol.

**Requirements:** R4, R5, R7.

**Dependencies:** U1.

**Files:**
- `internal/browsersniff/analysis.go` (modify the reachability cascade at line 902-928 — add the new branch)
- `internal/browsersniff/analysis.go` (add helper functions: `entryHasCaptchaTierProtection`, `entryEmitsSSREmbeddedData`, `sameRegisteredDomain`)
- `internal/browsersniff/analysis_test.go` (extend reachability tests)
- `go.mod` / `go.sum` (add `golang.org/x/net/publicsuffix` if not already pulled in)

**Approach:**
- Define a helper `apiEntryHasCaptchaTierProtection(entries, protections) (entryIndex int, ok bool)`. Walks `protections`, filters to label in `{captcha, bot_challenge, aws_waf, vercel_challenge}`, joins back to entries via `Evidence[i].EntryIndex`, and requires the entry's `Classification == "api"`. Returns the index of the first matching API entry (or `-1, false`).
- Define a helper `htmlEntryHasSSRStateBlobOnRegisteredDomain(entries, protocols, refHost string) (entryIndex int, signature string, ok bool)`. Walks entries, filters to those whose `Content-Type` contains `html` and whose host's eTLD+1 matches `refHost`'s eTLD+1, and checks if any of those entries has an `ssr_embedded_data` protocol observation in `protocols`. Returns the entry index, the signature label (read from `Details["signature"]`), and a match flag.
- In the cascade at line 902-928, after the `browser_required`-setting branches (line 902-911, captcha and browser_rendered), insert a new branch:
  - If `apiEntryHasCaptchaTierProtection` matched: let `refHost = extractHost(entries[apiIdx].URL)`.
  - If `htmlEntryHasSSRStateBlobOnRegisteredDomain(entries, protocols, refHost)` matched: set `mode = "html_scrape"`, confidence = 0.85, reason = `"captcha-tier protection on API + same-registered-domain SSR state blob (signature: <sig>); html_scrape preferred over browser_required"`.
- Same-registered-domain helper: `sameRegisteredDomain(hostA, hostB string) bool` — uses `publicsuffix.EffectiveTLDPlusOne` on both, returns true on match (case-insensitive). Fall-back: if either lookup fails (private TLD, malformed host), require literal hostname equality.
- The new branch must come AFTER the captcha/bot_challenge branches so it can read the already-set `browser_required` mode and override it. It must come BEFORE the cloudflare branch (line 917) which guards on `mode == "standard_http"` — so the cloudflare branch won't run when html_scrape is set.

**Patterns to follow:** Existing cascade structure at [`analysis.go:902-928`](../../internal/browsersniff/analysis.go#L902); existing `hasProtection` / `hasProtocol` / `hasAuth` helpers (they walk top-level lists; the new helpers walk per-entry); the `extractHost` / URL parsing already used throughout the file.

**Test scenarios:**
- HAR with API JSON entry on `api.example.com` returning 403 with captcha protection on that entry + HTML entry on `www.example.com` returning 200 with state-view blob (20 KB) → mode is `html_scrape`. Reason names the signature. (Yandex-shape positive — cross-subdomain.)
- HAR with API JSON on `example.com/api` returning 403/captcha + HTML on `example.com/` with `__NEXT_DATA__` (20 KB) → mode is `html_scrape`. (Same-host positive.)
- HAR with API JSON on `example.com` returning 403/captcha + HTML on `other-site.com` with state blob → mode is `browser_required` (different registered domain). KTD-3.
- HAR with HTML page on `example.com` carrying cloudflare protection (cf-ray on the HTML response) + JSON on `example.com/api` returning 200 (no protection) + SSR state blob on the HTML → mode is `standard_http`. The protection isn't on the API entry; KTD-5 must filter this out.
- HAR with API JSON returning 200 (no protection) + HTML entry with state blob → mode is `standard_http`. No protection, no promotion.
- HAR with API JSON returning 403/cloudflare (NOT captcha-tier) + HTML state-blob sibling → mode is `browser_clearance_http` (cloudflare's existing route). KTD-4.
- HAR with API JSON returning 403/aws_waf + HTML state-blob sibling on same eTLD+1 → mode is `html_scrape`. (aws_waf is in the captcha-tier set.)
- HAR with API JSON returning 403/captcha + HTML entry without state blob → mode is `browser_required`. Baseline preserved.
- HAR with only state-blob HTML, no API entries → mode is `standard_http`. Degenerate case.
- HAR with API JSON returning 403/captcha + HTML state-blob on same registered domain BUT under a different subdomain that's actually a different organization's site (theoretical: same eTLD+1 but different organizational ownership — e.g., a co.uk shared registrar) → not a realistic case in practice; the test asserts the rule's behavior, not a contrived false-positive scenario.
- Regression: all existing reachability tests at the four prior modes continue to pass.

**Verification:** `go test ./internal/browsersniff/` passes including new reachability tests. Existing `TestReachability*` suite green.

---

### U3. Wire specgen to consume the signal and emit `html_extract.mode: embedded-json`

**Goal:** Update `inferHTMLExtract` in `specgen.go` to consume the new selector signal (matched signature from ReachabilityAnalysis) and emit `html_extract.mode: embedded-json` with the correct script selector when the signal is present.

**Requirements:** R6.

**Dependencies:** U2.

**Files:**
- `internal/browsersniff/specgen.go` (modify `inferHTMLExtract` at line 771-782 — add the new mode branch; thread the signature signal in)
- `internal/browsersniff/specgen.go` (modify `AnalyzeCapture` at the entry point to surface ReachabilityAnalysis into `inferHTMLExtract` — either as a new parameter or via an enrichment field on `EndpointGroup`)
- `internal/spec/spec.go` if a new helper is needed for the script_selector mapping (likely not — the mapping per R6 is small enough to live in specgen)
- `internal/browsersniff/specgen_test.go` (add tests)

**Approach:**
- Add a parameter or field that carries the signature down from `ReachabilityAnalysis` into `inferHTMLExtract`. The implementer chooses the threading shape — pass it as an explicit param on `inferHTMLExtract`, OR attach it to the `EndpointGroup` during the capture-analysis phase before `inferHTMLExtract` is called per-group.
- In `inferHTMLExtract`: if the ReachabilityAnalysis says `Mode == "html_scrape"` AND a signature is present, emit `HTMLExtract{Mode: HTMLExtractModeEmbeddedJSON, ScriptSelector: <mapped>}` instead of the current `page`/`links` logic.
- Mapping table (KTD-9):
  - `__NEXT_DATA__` → `script#__NEXT_DATA__`
  - `__NUXT__` → `script#__NUXT__`
  - `__APP_INITIAL_STATE__` → `script#__APP_INITIAL_STATE__`
  - `state-view` → `script.state-view`
  - `application/ld+json` → `script[type="application/ld+json"]`
  - `window.__` → empty selector with a comment noting hand-tuning needed
- For endpoint groups outside the SSR-bearing host's eTLD+1, fall back to the existing `page`/`links` logic — the html_scrape mode applies per-group, not globally. (Most browser-sniff captures center on a single target; cross-domain endpoints in the same HAR are rare and the existing logic handles them.)

**Patterns to follow:** Existing `inferHTMLExtract` shape at [`internal/browsersniff/specgen.go:771-782`](../../internal/browsersniff/specgen.go#L771); existing usage of `HTMLExtractModePage` and `HTMLExtractModeLinks` for the conventional emission path.

**Test scenarios:**
- End-to-end positive: HAR fixture matching U2's Yandex-shape case (captcha-protected API + same-eTLD+1 SSR `state-view` HTML) → run the full capture-analysis flow → assert the emitted spec carries `html_extract.mode: embedded-json` with `script_selector: "script.state-view"`.
- End-to-end positive: HAR with `__NEXT_DATA__` signature → assert emitted spec has `script_selector: "script#__NEXT_DATA__"`.
- End-to-end positive: HAR with `__NUXT__` signature → `script#__NUXT__`.
- End-to-end positive: HAR with `application/ld+json` signature → `script[type="application/ld+json"]`.
- End-to-end control: HAR without the two-signal conjunction → emitted spec has no `html_extract` block (or has `mode: page` / `mode: links` per existing logic).
- End-to-end regression: HAR currently producing `mode: page` → still produces `mode: page` (the new branch is gated on the html_scrape Reachability.Mode signal; absent that signal, the existing logic runs).
- Edge case: HAR with `window.__` signature → emitted spec has `mode: embedded-json` with empty `script_selector` and a comment in the spec or warning in the output noting hand-tuning is required.

**Verification:** `go test ./internal/browsersniff/` passes including new specgen tests. Manual smoke: run `printing-press` against a Yandex-shape HAR fixture, inspect the emitted spec, confirm `html_extract.mode: embedded-json` + the right script_selector.

---

### U4. Test fixtures + golden verification

**Goal:** Add HAR test fixtures for the two-signal positive case + the negative regressions. Update goldens if any existing fixture retroactively matches the new condition.

**Requirements:** R7.

**Dependencies:** U1, U2, U3.

**Files:**
- `internal/browsersniff/testdata/` or wherever existing HAR fixtures live — add minimal Yandex-shape and Next.js-shape HARs.
- `testdata/golden/expected/` — re-run `scripts/golden.sh verify` after U3 lands. If any existing fixture retroactively matches the new two-signal pattern, document the diff in the PR per AGENTS.md golden-update rule.

**Approach:**
- Construct two minimal HARs:
  - **Yandex-shape:** one entry on `api.example.com` returning 403 with captcha headers (cookie redirect to `/showcaptcha`), one entry on `www.example.com` returning 200 with a 20 KB body containing `<script type="application/json" class="state-view">{...}</script>`.
  - **Next.js-shape (same-host):** one entry on `example.com/api/foo` returning 403/captcha, one entry on `example.com/foo` returning 200 with a 20 KB body containing `<script id="__NEXT_DATA__" type="application/json">{...}</script>`.
- Run `scripts/golden.sh verify`. If existing fixtures shift mode, inspect whether the new mode is actually correct for them (the prior `browser_required` may have been a too-heavy recommendation that html_scrape correctly replaces). Update goldens per the golden-update workflow if so.

**Test scenarios:** N/A — verification unit. Test expectation: `scripts/golden.sh verify` passes after any fixture updates; documented in PR description.

**Verification:** `scripts/golden.sh verify` green. PR description names any fixtures touched and the rationale.

---

## Scope Boundaries

### In scope
- Detector extension: add `state-view`, `__NUXT__`, `__APP_INITIAL_STATE__` + status/size floors (U1).
- Signature surfacing on the protocol observation's `Details` map (U1).
- Reachability mode selector promotion (U2) — captcha-tier protection per-API-entry + same-eTLD+1 SSR HTML sibling.
- Specgen emission of `html_extract.mode: embedded-json` with the right `script_selector` per matched signature (U3).
- Golden-fixture verification (U4).

### Deferred to Follow-Up Work
- New `internal/scrape/` generator template family — the rescope already ruled this out. The runtime + spec + (now) sniff layers handle html_scrape end-to-end without a parallel scrape package.
- Multi-page pagination for state-blob extraction — already handled at the spec layer (food52 retro work).
- Anti-bot fallback messaging in generated CLIs — generator concern.
- Tightening the body-size floor based on real-world n>1 evidence — the 10 KB threshold is a conservative starting point; if real captures produce many false positives, retune in a follow-up.
- Operator-facing override (forcing html_scrape mode despite a no-state-blob HAR, or vice versa) — out of scope; if needed, exposes as a flag in a follow-up.
- Detection of inline JSON in HTML attributes (not script tags) — narrow shape, low signal density.

### Outside this work's identity (tracked elsewhere)
- Generator-template work for novel scraping patterns — out of scope; surface a new issue if a printed CLI needs it.
- Public-library catalog re-evaluation of which entries should switch to html_scrape — operator-driven, not analyzer-driven.

---

## System-Wide Impact

- **HAR analyzer:** signature now surfaced on `ProtocolObservation.Details`. New cascade branch in the mode selector. Two new helpers (`apiEntryHasCaptchaTierProtection`, `htmlEntryHasSSRStateBlobOnRegisteredDomain`, `sameRegisteredDomain`).
- **Spec emission:** `inferHTMLExtract` gains a third branch (embedded-json) alongside the existing page/links logic. ReachabilityAnalysis now threaded into specgen.
- **Generated CLIs:** for HARs matching the new pattern, the emitted spec now produces a goquery-based CLI shape instead of a Playwright shell. For all other HARs, no change.
- **Existing tests:** the four prior reachability-mode tests + existing specgen page/links tests must continue to pass. Additions are guarded on the new signal; absent the signal, the existing paths run.
- **Golden fixtures:** may shift if any existing test HAR retroactively matches the new two-signal pattern. U4 covers this.
- **Dependencies:** adds `golang.org/x/net/publicsuffix` if not already pulled in.

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| 10 KB body-size floor too low — captcha pages with embedded `__NEXT_DATA__` carrying ~10 KB of challenge content pass the floor | Medium | False-positive html_scrape promotion on captcha pages; CLI emits empty extraction | Captcha-page bodies are typically < 5 KB; 10 KB is above the typical challenge-page size. If real captures show false positives, raise the floor or add a semantic check (parse the embedded JSON and require >N keys) |
| Signature priority when multiple signatures present | Low | Wrong script_selector picked when an HTML page has both `__NEXT_DATA__` and `application/ld+json` | KTD-1 / R3 fix the order; the typical Next.js shape has both but `__NEXT_DATA__` is the right pick |
| eTLD+1 false positives across organizations sharing a registrar (`*.co.uk` shared hosts) | Very low | Cross-org false positive triggering html_scrape | The two-signal conjunction (captcha-tier API + state-blob HTML) is implausible across organizations; risk is theoretical, not practical |
| Cross-host capture: API on `api-cdn.example.org` (different eTLD+1 from main site `example.com`) — Yandex-shape captures could still split this way for some sites | Low | False negative: routes to browser_required | Documented in Scope Boundaries as a known limitation; operator can hand-edit the spec |
| Test fixtures coincidentally match the new pattern | Low | Existing reachability tests fail | U4 reviews + updates goldens per the AGENTS.md golden-update rule |
| `EvidenceRef.EntryIndex` not consistently populated across all protection detectors | Low | New helper fails to find the originating entry | Trace through `detectProtections` during U2 implementation; if any path doesn't set EntryIndex, fix it as part of U2 |
| `publicsuffix` dependency surfaces a build concern (not already vendored) | Low | go.mod / go.sum change needed | KTD-10 notes this; verify before commit |

---

## Verification Strategy

- Unit tests per implementation unit (table-driven with `testify/assert` per AGENTS.md convention).
- Integration test in U2: HAR fixture → analyzer → assert recommended mode.
- End-to-end test in U3: HAR fixture → analyzer → spec emission → assert `html_extract.mode: embedded-json` + correct `script_selector`.
- `scripts/golden.sh verify` after U3 (and again after U4 if fixtures shift).
- `go test ./...` clean before PR.
- `go vet ./...` and `golangci-lint run ./...` clean.

---

## Notes for the implementer

- **AGENTS.md "machine vs printed CLI":** machine change. Analyzer + specgen are press internals; printed-CLI templates are not touched.
- **AGENTS.md commit style:** scope is `cli` (the Go binary). Type is `feat`. Suggested title: `feat(cli): emit html_extract.mode: embedded-json from sniff pipeline on captcha + SSR state blob (#953)`.
- **Issue claim is already in place** on #953 (assignment + "evaluating" comment + rescope comment posted by the planner).
- **The rescope** documented at the top of #953's comment thread captures the architectural shift this plan now reflects (analyzer + specgen wiring is the real gap, not a new generator package).
- **U3 is the largest unit by code volume.** Don't underestimate it because it's listed third. The threading from `ReachabilityAnalysis` into per-EndpointGroup `inferHTMLExtract` is the load-bearing change; U1 and U2 are preconditions that enable it.
