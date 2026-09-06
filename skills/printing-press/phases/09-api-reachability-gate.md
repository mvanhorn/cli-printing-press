## 09-api-reachability-gate (Phase 1.9: API Reachability Gate)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "09-api-reachability-gate"
```

**MANDATORY. Do NOT skip this phase. Do NOT proceed to [Phase 2](10-generate.md) without running this check.**

Before spending tokens on generation, verify the API actually responds to programmatic requests. One real HTTP call. If it fails, STOP.

**Exception for browser-clearance/browser-sniffed website CLIs:** If [Phase 1.7](06-browser-sniff-gate.md) produced a successful browser capture and `$DISCOVERY_DIR/traffic-analysis.json` reports `reachability.mode` as `browser_clearance_http` or `browser_http`, a plain `curl` 403/429 is expected evidence, not a hard stop. In that case the reachability gate passes only if:
- the browser-sniff capture contains useful non-challenge traffic (real API, SSR data, structured HTML, RSS/feed data, or page-context fetch evidence), and
- [Phase 2](10-generate.md) will pass `--traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json"` so the generator can emit browser-compatible HTTP transport and, for `browser_clearance_http`, Chrome cookie import.

Do not treat a persistent browser sidecar as a shippable CLI runtime. Browsers are allowed for Printing Press discovery and reusable auth/clearance capture; ordinary printed CLI commands must replay through direct HTTP, Surf/browser-compatible HTTP, or stored reusable auth state. If traffic analysis reports `browser_required`, return to discovery to find a replayable HTTP/HTML/RSS/SSR surface or HOLD the run.

Useful same-site HTML document pages count as a replayable surface when they return real content, not challenge/login pages. Browser-sniff can promote these into `response_format: html` endpoints so generated commands extract page metadata and filtered links through Surf/direct HTTP instead of keeping a browser sidecar alive.

When hand-authoring a `response_format: html` spec with `html_extract.mode: links`,
document and choose `link_prefixes` as path-segment prefixes. A prefix `/items`
matches `/items` and `/items/...`, but not `/items123.html`; use the parent
directory prefix when the leaf segment has embedded IDs or suffixes. See
[`../references/spec-format.md`](../references/spec-format.md) for the exact contract.

If the browser capture contained only challenge/login/error pages, this exception does not apply.

**Exception for LAN-only / mDNS-discovered APIs:** If the resolved spec's `base_url` is a localhost or loopback placeholder (`http://localhost:<port>`, `http://127.0.0.1:<port>`, or `http://[::1]:<port>`), or [Phase 1](04-research-brief.md) research explicitly identifies the API as LAN-only / SSDP / mDNS-discovered with no stable global origin, do not run the generic curl/WebFetch reachability probe. A probe from the generation host would test the agent's loopback or current network, not the user's appliance, speaker, bridge, or local service.

For this case, record a Phase 1.9 PASS carve-out in the research brief:

```markdown
## Reachability Gate
- Decision: PASS (carve-out)
- Reason: lan-only-no-global-url
- Evidence: <base_url or research line showing localhost, loopback, SSDP, mDNS, or LAN-only discovery>
```

Then proceed to [Phase 2](10-generate.md). Do not write a freeform manual proof for this case, do not call it a missing-API-key skip, and do not use this carve-out for normal public/cloud origins such as `https://api.example.com`; those still run the reachability probe and decision matrix below.

### The Check

Prefer the spec's `auth.verify_path` when it is set; otherwise pick the simplest GET endpoint from the resolved spec (no required params, no auth if possible). If no such endpoint exists, use the spec's base URL. Run one HTTP request and preserve the response body when the server returns a 4xx:

```bash
body_file="$(mktemp "${TMPDIR:-/tmp}/pp-reachability-body.XXXXXX")"
trap 'rm -f "$body_file"' EXIT
status="$(curl -s --max-filesize 65536 -o "$body_file" -w "%{http_code}" -m 10 "<base_url>/<simplest_get_path>" 2>/dev/null || true)"
case "$status" in
  [0-9][0-9][0-9]) ;;
  *) status="000" ;;
esac
printf '%s\n' "$status"
```

Or use `WebFetch` if curl is unavailable. Record the response status and, for any 4xx response body, run the same tier/permission keyword scan against the captured WebFetch body text before deciding. The goal is one real response code plus any 4xx body evidence the API chose to return.

If `status` is any 4xx, inspect the body before deciding. Search it case-insensitively for tier or permission terms:

```bash
grep -Ei 'tier|allowed|permitted|subscription|quota|plan|scope|limit|permission|forbidden|unauthorized|upgrade|trial' "$body_file" | head -20
```

When matched lines are present, add them to the [Phase 1](04-research-brief.md) research brief under:

```markdown
## Reachability Risk
- Tier/permission hints from 4xx body: "<matched line, truncated if needed>"
```

Keep the evidence bounded: include only the lines that explain the access model, trim each line to a readable length, and do not paste bearer tokens, API keys, cookies, or unrelated full response dumps. If the GET returns 2xx/3xx, omit this tier-hint subsection.

Do not probe arbitrary mutation endpoints to discover tier limits. A generic "try a PUT/POST/PATCH/DELETE" rule can create accounts, send messages, capture payments, or mutate user data. Mutation probing is allowed only when the resolved spec or OpenAPI operation explicitly marks that endpoint as probe-safe with `x-pp-safe-probe: true`; the endpoint must be idempotent or otherwise harmless for the real account being used. If no endpoint has that explicit marker, stop after the GET body capture above.

If one or more probe-safe endpoints are declared and the user provided credentials, run exactly one declared probe-safe endpoint as a second reachability probe and apply the same 4xx body capture and tier-keyword extraction. When more than one exists, choose the lowest-risk declared endpoint by preferring methods in this order: HEAD/OPTIONS/GET, then PUT/PATCH, then POST, then DELETE only if it is the only declared safe option. Break ties by choosing the endpoint with the fewest required parameters and avoiding paths with account, billing, payment, deletion, or notification terms when any safer declared option exists. Record which endpoint was probe-safe in the brief so later phases know the evidence came from an opt-in safe probe.

### OAuth2 Grant Probe

For OAuth2 Authorization Code + PKCE CLI implementation/review requirements, read [references/oauth2-pkce-cli-checklist.md](../references/oauth2-pkce-cli-checklist.md).

If the resolved spec declares `auth.type: oauth2` and has an interactive
authorization URL (`authorizationCode` or `implicit` flow in OpenAPI, or an
equivalent internal YAML auth field), the generic reachability check is not
enough. After the base URL check would otherwise pass, verify the OAuth grant
entry point with the user's real public OAuth input before [Phase 2](10-generate.md). This probe
is read-only: it stops at the provider's consent, login, or error page and does
not exchange a code, request a token, or ask the user to approve consent.

Do not run this grant probe for OAuth2 `client_credentials` flows that only have
a token URL. Those are server-to-server credentials, not browser grant flows, and
probing the token endpoint would require secret material or a write-like auth
attempt. The base reachability check plus later mock/live auth verification cover
that shape.

**Required inputs:** Use the `client_id` env var or public auth-flow input
already resolved during [Phase 0.5](03-resolve-and-reuse.md) and Pre-Generation Auth Enrichment. If the
spec exposes `x-auth-vars`, prefer the entry with `kind: auth_flow_input`,
`sensitive: false`, and a name or description identifying it as the OAuth
`client_id`. If the real client id is missing, HOLD before generation and tell
the user exactly which env var to set. Do not substitute a fake client id; fake
ids can produce provider-specific errors that look like transport quirks.

Build the authorize URL from the resolved spec, not from a guessed provider
default:

- `client_id`: the real public client id from the env var above.
- `redirect_uri`: the redirect URI declared in the spec or auth metadata.
- `response_type=code` for authorization-code grants, or the spec's documented
  response type for implicit grants.
- For authorization-code grants, include a safe probe PKCE pair using `S256`.
  Use `probe_reachability_check_pkce_probe_literal` as the code verifier and
  compute the URL-safe SHA-256 challenge from it. The verifier is 43 unreserved
  characters, satisfying the RFC 7636 minimum; providers that do not require
  PKCE ignore these params, and providers that enforce PKCE should advance to
  the login or consent page instead of returning a false `invalid_request`.
- `scope`, `audience`, `tenant`, `state`, `prompt`, or other provider-required
  params when the spec or vendor docs require them. Use a benign probe value for
  `state` if required.

Use a redirect-limited GET and inspect the final URL, response body, and
response class:

```bash
PKCE_VERIFIER="probe_reachability_check_pkce_probe_literal"
PKCE_CHALLENGE=$(printf "%s" "$PKCE_VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
AUTH_URL="<authorization_url_with_required_query_params>"
# Add code_challenge_method=S256 and code_challenge=$PKCE_CHALLENGE to AUTH_URL.
PROBE_BODY_AND_META=$(curl -sS -L --max-redirs 10 -m 15 -w "\n%{http_code} %{url_effective}" -o - "$AUTH_URL" 2>/dev/null)
PROBE_META=$(printf "%s\n" "$PROBE_BODY_AND_META" | tail -n 1)
PROBE_BODY=$(printf "%s\n" "$PROBE_BODY_AND_META" | sed '$d')
printf "%s\n" "$PROBE_META"
printf "%s\n" "$PROBE_BODY" | head -c 8000
printf "\n"
```

Interpret the result before [Phase 2](10-generate.md):

| OAuth probe result | Action |
|--------------------|--------|
| HTTP status is `2xx` or `3xx`, final URL stays on the provider's authorization/login/consent host, does not include `error=`, and the response body does not contain an OAuth error code (`invalid_request`, `invalid_client`, `unauthorized_client`, etc.) | **PASS** - the grant entry point is reachable; proceed to [Phase 2](10-generate.md) |
| Final URL or response body reports `invalid_request`, `invalid_client`, `redirect_uri_mismatch`, `unauthorized_client`, `unsupported_response_type`, or equivalent | **HARD STOP** - OAuth config is misconfigured; surface the provider error and point the user to the mismatched client id, redirect URI, app type, tenant, or required scope |
| HTTP status is `4xx` or `5xx` without a recognizable OAuth error code | **WARN** - flag provider-specific routing or login-shell behavior for manual review before generation |
| Final URL lands on a generic non-OAuth error page, marketing page, or unrelated login landing page | **WARN** - flag endpoint ambiguity or provider-specific routing for manual review before generation |
| Timeout/DNS/connection refused or HTTP status `000` | **WARN** - same handling as the generic reachability WARN |

On HARD STOP, do not generate. Present a specific, provider-neutral message:

> "WARNING: `<API>`'s OAuth authorize probe failed before generation. The
> provider returned `<error_or_final_url>`. Check that the spec's
> `authorization_url`, `redirect_uri`, `response_type`, client id env var, app
> type, tenant, and required scopes match the registered OAuth application."

This OAuth probe is additive to the base reachability gate. Non-OAuth APIs
(`api_key`, `bearer_token`, `cookie`, `composed`, `session_handshake`, `none`)
skip it entirely.

**If the check returns 403/429 with bot-protection evidence and `probe-reachability` has not already run for this URL during [Phase 1.7](06-browser-sniff-gate.md)'s Direct HTTP challenge rule, run it now before consulting the decision matrix:**

```bash
cli-printing-press probe-reachability "<base_url>" --json
```

The matrix below references `probe-reachability` `mode` for the bot-detection rows. If the probe already ran in [Phase 1.7](06-browser-sniff-gate.md), reuse that result; do not re-probe.

### Decision Matrix

| Result | Browser capture result | Traffic-analysis reachability | Action |
|--------|------------------------|-------------------------------|--------|
| 2xx/3xx | Any | Any | **PASS** - proceed to [Phase 2](10-generate.md) |
| 401 (no key provided) | Any | Any | **PASS** - expected when API needs auth and user declined key gate |
| 403/429 with HTML/bot detection | `probe-reachability` returned `browser_http` | runtime is `browser_http` (Surf) | **PASS** - the printed CLI will ship Surf transport which clears the protection. No clearance cookie capture in the printed CLI, regardless of whether browser-sniff also ran for endpoint discovery |
| 403/429 with HTML/bot detection | Successful useful capture | `browser_http` or `browser_clearance_http` | **PASS** - proceed with browser-compatible HTTP / clearance strategy |
| Any | Capture only works through a live page context | `browser_required` | **HOLD** - find a lighter replayable surface before [Phase 2](10-generate.md) |
| 403/429 with HTML/bot detection | No browser capture attempted but browser-sniff approved/pre-approved AND `probe-reachability` returned `browser_clearance_http` or `unknown` | Any | **RETURN TO PHASE 1.7** - attempt cleared-browser capture before pivoting scope |
| 403/429 with HTML/bot detection | Capture contains only challenge/error pages | Any | **HARD STOP** |
| 403 | No successful useful capture | Research found 403 issues | **HARD STOP** |
| 403 | No successful useful capture | No 403 research issues | **WARN** - ask user |
| Timeout/DNS/connection refused | Any | Any | **WARN** - ask user |

On a **RETURN TO PHASE 1.7** verdict, record the discovery-rework handoff so the receipt gate accepts the re-entry into the browser-sniff gate rather than treating it as an out-of-order jump, then re-enter [Phase 1.7](06-browser-sniff-gate.md) instead of following the canonical `Next:` below:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "09-api-reachability-gate" --next "06-browser-sniff-gate" --note "cleared-browser capture retry: <reason>"
```

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

Proceed silently to [Phase 2](10-generate.md).

---

Before following `Next:`, record the successful reachability result in one
short, credential-free note:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "09-api-reachability-gate" --note "<reachable surface and status only>"
```

Next: phases/10-generate.md
