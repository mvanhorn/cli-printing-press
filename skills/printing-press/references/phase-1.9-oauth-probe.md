# Phase 1.9: OAuth2 Grant Probe

Lazy-loaded body for the OAuth2 grant-reachability probe. Runs only when the resolved spec declares `auth.type: oauth2` with an interactive authorization URL (authorizationCode/implicit). Read and apply before Phase 2 when that condition holds; otherwise skip.

### OAuth2 Grant Probe

If the resolved spec declares `auth.type: oauth2` and has an interactive
authorization URL (`authorizationCode` or `implicit` flow in OpenAPI, or an
equivalent internal YAML auth field), the generic reachability check is not
enough. After the base URL check would otherwise pass, verify the OAuth grant
entry point with the user's real public OAuth input before Phase 2. This probe
is read-only: it stops at the provider's consent, login, or error page and does
not exchange a code, request a token, or ask the user to approve consent.

Do not run this grant probe for OAuth2 `client_credentials` flows that only have
a token URL. Those are server-to-server credentials, not browser grant flows, and
probing the token endpoint would require secret material or a write-like auth
attempt. The base reachability check plus later mock/live auth verification cover
that shape.

**Required inputs:** Use the `client_id` env var or public auth-flow input
already resolved during Phase 0.5 and Pre-Generation Auth Enrichment. If the
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

Interpret the result before Phase 2:

| OAuth probe result | Action |
|--------------------|--------|
| HTTP status is `2xx` or `3xx`, final URL stays on the provider's authorization/login/consent host, does not include `error=`, and the response body does not contain an OAuth error code (`invalid_request`, `invalid_client`, `unauthorized_client`, etc.) | **PASS** - the grant entry point is reachable; proceed to Phase 2 |
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
