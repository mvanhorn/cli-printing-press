---
when-to-read: When implementing, reviewing, or generating OAuth2 Authorization Code + PKCE login flows for a printed CLI.
---

# OAuth2 PKCE CLI Checklist

Use this checklist for printed CLI commands that obtain user-context tokens through an OAuth2 browser authorization flow. It applies to hand-written novel commands and to any future generator/template support for OAuth2 Authorization Code + PKCE.

## Required behavior

- Generate cryptographically random `state` per login attempt.
- Generate a cryptographically random PKCE verifier per login attempt.
- Use `code_challenge_method=S256` and a URL-safe SHA-256 `code_challenge`.
- Validate callback `state` before exchanging any authorization code.
- Bind loopback callback listeners only to loopback hosts (`127.0.0.1`, `localhost`, or `::1`).
- Require the redirect URI to match the provider registration and the command's listener.
- Store access tokens, refresh tokens, scopes, and expiry metadata through the CLI's normal config/storage path.
- Keep cookie/browser-session login paths distinct from OAuth2 token acquisition paths.

## Timeout handling

Use separate timeout contexts for separate phases:

1. Browser callback wait: long enough for the user to complete login/consent.
2. Token exchange: shorter network timeout after the callback has arrived.

Do not pass the browser-wait context into the token exchange. A user who authorizes near the callback deadline should not get an ambiguous token-exchange failure caused by leftover callback time.

## Machine-output and browser-launch rules

OAuth login commands often have two ways to get the user to the authorize URL:

- open the browser automatically; or
- print the authorize URL for manual copy/paste.

Never suppress both paths.

If `--json` or another machine-output mode suppresses human-readable stdout, and `--no-open` disables browser launch, the command must either:

1. fail fast with an actionable usage error; or
2. implement an explicit machine protocol that emits structured data such as `authorize_url`, `state_id`, `expires_at`, and a documented continuation command.

Do not silently wait for a callback that cannot happen.

Under `cliutil.IsVerifyEnv()`, do not open browsers or dial OS handlers. Prefer a typed verify/dry-run result or a usage error that names the non-opening mode.

## JSON cleanliness

- Do not print prose to stdout in `--json` mode.
- If warnings are needed in JSON mode, put them in the JSON envelope or write them to stderr only when that matches the CLI's existing error contract.
- Keep final success output machine-parseable and free of OAuth secrets.

## Test requirements

Add focused tests for:

- authorize URL construction, including `state`, scope, redirect URI, S256 challenge, and response type;
- callback state mismatch;
- end-to-end loopback flow with a local token server;
- token storage metadata, including refresh-token presence, scopes, and expiry when applicable;
- browser-open failure or `--no-open` URL printing behavior;
- impossible mode combinations such as `--json --no-open`;
- timeout behavior for callback wait versus token exchange when practical.

Run `go test -race` for tests involving callback servers, token servers, channels, or goroutines.

## Race-safety rules for tests

HTTP test handlers run in separate goroutines. Do not write a plain `bool`, map, slice, or struct field in an `httptest.Server` handler and read it from the main test goroutine without synchronization.

Use one of:

- `sync/atomic.Bool` or another atomic type;
- a channel;
- a mutex-protected struct.

A completed `http.Client.Do` call is not a general synchronization primitive for arbitrary server-side writes.

## Review red flags

Treat these as blockers or at least high-priority review comments:

- plain `bool` or unsynchronized state shared between HTTP handler goroutines and test goroutines;
- the same context used for both browser callback wait and token exchange;
- `--json` hides the authorize URL while `--no-open` prevents browser launch;
- browser auto-open runs under `PRINTING_PRESS_VERIFY=1`;
- OAuth2 user-context tokens are conflated with app-only bearer tokens or cookie-capture login;
- callback accepts non-loopback redirect hosts without an explicit, reviewed reason;
- token responses are logged or printed in full;
- generated docs tell users to use an app-only bearer token for user-context writes or personal reads.

## When to promote this into generator code

This checklist is enough for hand-written novel OAuth commands. If `cli-printing-press` starts generating OAuth2 PKCE login commands from specs/templates, promote these rules into the generator and golden tests so every printed CLI inherits them by construction.
