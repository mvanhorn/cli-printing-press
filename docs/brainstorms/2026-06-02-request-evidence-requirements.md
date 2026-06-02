---
date: 2026-06-02
topic: request-evidence-for-sniffed-cli-generation
---

# Request Evidence: Wire-Faithful Generation for Sniffed APIs

## Problem Frame

The press currently treats inferred specs as the wire authority for every input mode. For OpenAPI specs and well-documented APIs that is correct. For `spec_source: browser-sniffed` it is lossy — the inferred spec is a compression of the HAR, and the inference layer routinely discards information that the capture had verbatim. This surfaces as wire bugs in the printed CLI that cannot be diagnosed from the spec alone.

This document was driven by a 2026-06-02 experiment that exercised the full pipeline against macOS-native-app traffic for the first time:

1. **mitmproxy installed and CA trusted in the system keychain.**
2. **Capture run A (system proxy, baseline noise).** macOS Wi-Fi proxy set to `127.0.0.1:8080`. Slack desktop driven for ~60s. Result: 185-entry HAR, ~118 from other apps (YouTube/HackMD/Hetzner). The press emitted 6 endpoints inside a fake `cache` resource, `description: "Discovered API spec for youtube"`, `auth.cookie_domain: .youtube.com`, spurious `aws_waf` protection from HackMD, and bearer-token auth candidates from unrelated apps.
3. **Capture run B (`mitmdump --mode local:Slack`).** mitmproxy 12's Local Redirect Mode hooks via a macOS Network Extension and intercepts traffic from processes whose name matches. HAR shrank to 65 entries, 5 hosts all `*.slack.com`. The press emitted 15 endpoints across 11 resources (`conversations.history`, `apps.profile.get`, `dnd.teamInfo`, `bookmarks.list`, etc.), zero noise warnings, auth correctly identified as cookie, primary host correctly chosen as `edgeapi.slack.com`.
4. **Print stage.** `generate --spec ... --spec-source browser-sniffed --traffic-analysis ...` emitted a 70-file Go project, built a 62 MB binary, passed every quality gate (`go mod tidy`, govulncheck, vet, build, `--help`, `version`, `doctor`) and produced an MCP bundle.
5. **Live test.** Cookies extracted from the HAR, `SLACK_COOKIES` set, `slack-pp-cli dnd.team-info --json` invoked. Got HTTP 404. After patching `client.go` in-place to fix the cookie header, the call still hit 404 because the spec routed it to `edgeapi.slack.com` (the chosen primary) instead of `premai.slack.com` (where `dnd.teamInfo` actually lives). An `edgeapi` endpoint (`cache-resource create-counts`) was then tried — got HTTP 200 with `{"ok":false,"error":"invalid_auth"}` because Slack also wants the `xoxc-...` workspace token in the multipart body, which the press didn't capture.

Two takeaways:

- **`mitmdump --mode local:<AppName>` is the right capture primitive for native macOS apps.** Per-PID filtering at capture time replaces every host-blocklist heuristic in the analyzer. No press code change needed.
- **The press's spec-driven codegen is the wire-authority bug.** Every live-test failure ran through the inference layer throwing away information the HAR had verbatim. Per-endpoint host, multipart bodies, cookie header structure, classified telemetry headers — all of it is present in the capture and absent from the spec.

This document was reviewed twice by `codex exec`. The first pass reframed "exemplar-driven generation" into "evidence-anchored request templates" (don't replace specs; add a sidecar for sniffed sources). The second pass validated the phased plan below and corrected sequencing.

## Direction

Introduce `RequestEvidence` as a new primitive consumed by the generator when `spec_source: browser-sniffed`. The existing spec→codegen path stays — for OpenAPI, docs-derived, and plan-based generation it is correct and benefits from optional-field inference the capture cannot see. Sniffed sources gain a parallel evidence path that the generator prefers when present, with the spec retained as the human-readable view and the fallback codegen path.

Constants observed in a capture are classified, never invisibly baked. Five classes:

| Class | Description | Example |
|---|---|---|
| `protocol-constant` | HTTP-mandated headers, content negotiation | `accept`, `content-type`, `user-agent` |
| `semantic-default` | App-meaningful, identical across exemplars, user-overridable | `_x_app_name=client`, locale fields |
| `auth-secret` | Authentication material, must be runtime-supplied | `xox[abcdpr]-...`, `eyJ...` JWTs, session cookies |
| `volatile-drop` | Tracing/fingerprint/per-request noise that should not be replayed | `x-b3-traceid`, `_x_b3_*`, multipart boundaries, request fingerprints |
| `unknown` | Anything unclassified — surface as required param, force user input | unfamiliar field names |

The classifier vocabulary is shared between capture-time emission (sidecar) and codegen-time consumption (template). A blocklist-only telemetry filter would itself be an invisible baking path — anything dropped from CLI flags must be tagged with its class so the printed CLI's `doctor` / `agent-context` can explain what was elided and why.

## Validated Plan

Six PR-sized phases. Each is independently landable; phase N+1 may depend on N as noted.

### PR 1 — Cookie/session-auth template split

**Goal:** Distinguish API-key-in-named-cookie auth from full-session-cookie auth in the generated client.

**Scope:**
- `internal/generator/templates/client.go.tmpl:1349` currently emits `req.AddCookie(&http.Cookie{Name: "{{...}}", Value: authHeader})` for every `in: cookie` auth. This is correct when the cookie is a *named* token bearer (`api_key=...`) but wrong when the value is a full `name=value; name2=value2` `Cookie` header.
- Split by a new spec-level discriminator (`auth.cookie_mode: named_token | session_header`, defaulting to `named_token` for backwards compatibility).
- Sniffed specs that capture a real browser/native session emit `auth.cookie_mode: session_header`, generating `req.Header.Set("Cookie", authHeader)`.
- Update golden tests in `testdata/golden/expected/generate-golden-api-cookie-auth/` to cover both branches.

**Acceptance:**
- Existing `api_key`-cookie tests pass unchanged.
- New `session_header` golden test produces a client that calls `req.Header.Set("Cookie", ...)` and does not trigger Go's `net/http: invalid byte ';' in Cookie.Value` warning when the cookie string contains multiple `name=value` pairs.
- `browser-sniff` emits `cookie_mode: session_header` when the captured `Cookie` header in the dominant primary-host entries carries ≥ 2 name=value pairs.

**Files:**
- `internal/generator/templates/client.go.tmpl` (template branch)
- `internal/spec/spec.go` (CookieMode field on Auth)
- `internal/browsersniff/specgen.go` (emit cookie_mode at inference time)
- `testdata/golden/expected/generate-golden-api-cookie-auth/` (extend coverage)

### PR 2 — Per-endpoint host preserved by default for sniffed sources

**Goal:** Stop the press from collapsing a multi-host sniffed API into one primary host. Each captured endpoint keeps its observed host.

**Scope:**
- `--preserve-hosts` flag already exists. Make it the default when `spec_source == browser-sniffed`.
- Populate `Endpoint.BaseURL` from `EndpointGroup.Host` in `specgen.go` for every endpoint whose host differs from the chosen primary.
- Verify telemetry / blocklist filtering does not collapse host distribution before the primary-host decision is made (regression risk: an aggressive blocklist that drops one host's entries could shift the primary).
- Verify `EndpointGroup.Host` survives the dedup pass — `DeduplicateTrafficEndpoints` already keys on host but a downstream pass may flatten.

**Depends on:** none. Standalone wire fix. Highest-leverage single change — was the cause of 10/15 endpoint failures in the experiment.

**Acceptance:**
- For a HAR with N hosts, the generated spec has at least one endpoint per host (assuming each host had API traffic).
- A new test fixture (synthetic two-host HAR) generates a CLI where each endpoint's runtime client targets its observed host.
- `--preserve-hosts=false` overrides the default for `spec_source: browser-sniffed`.

**Files:**
- `internal/browsersniff/specgen.go` (host preservation)
- `internal/cli/root.go` and/or `browser_sniff.go` (default flag value when sniffed)

### PR 3 — Body parsing (multipart + form-urlencoded)

**Goal:** When the capture has structured request bodies, extract their fields as endpoint params.

**Scope:**
- Extend `internal/browsersniff/parser.go::convertHAREntry` to parse:
  - `application/x-www-form-urlencoded` via `url.ParseQuery`
  - `multipart/form-data` via `mime/multipart.NewReader` using the boundary in the content-type header
- Surface fields as endpoint params alongside query params, with a `content_location: body_form | body_multipart | query` tag so downstream codegen can build the right request body shape.
- Auth-secret detection at this layer too — values matching `xox[abcdpr]-` / `eyJ` JWT shape get the `auth-secret` class so they become env-var-required at codegen.

**Depends on:** none. Independent of PR 4/5 architecture but must land before PR 4 because the evidence model needs body shape from day one.

**Acceptance:**
- Generated CLI for a multipart-body endpoint exposes the body fields as flags (or env-var requirements for `auth-secret`-classed fields).
- A captured `token=xoxc-...` body field shows up as a required env var in the printed CLI (not a `--token` flag where the secret would land in shell history).

**Files:**
- `internal/browsersniff/parser.go`
- `internal/browsersniff/specgen.go` (consume body fields)
- `internal/generator/templates/client.go.tmpl` (encode multipart at runtime)

### PR 4 — `internal/wireevidence/` package + classifier artifact

**Goal:** A capture-agnostic evidence model that the generator can consume without importing `browsersniff`.

**Scope:**
- New package `internal/wireevidence/` with:
  - `RequestEvidence` struct: method, base URL (the observed host), normalized path, exemplar list, per-slot classification (header / query field / body field)
  - Classifier with the five-class vocabulary above
  - Volatility detection: a slot is "constant" if identical across all exemplars; "varying" otherwise. Classification rules consume both the constancy signal and name-pattern signals (e.g. `x-b3-*` → `volatile-drop` regardless of constancy).
  - JSON serialization to a `request-evidence.json` sidecar emitted next to the spec at sniff time.
- Browsersniff builds the sidecar from `EndpointGroup` (one entry per group, all entries as exemplars) and writes it via `--evidence-output` flag (paralleling `--analysis-output`).
- Sidecar is not yet consumed by the generator — that's PR 5.

**Depends on:** PR 3 (needs parsed body fields in the evidence model from the start).

**Acceptance:**
- `cli-printing-press browser-sniff --har <h>` emits `<spec>-request-evidence.json` next to the spec.
- The JSON structure is documented in `docs/SPEC-EXTENSIONS.md` (or a new doc) alongside the existing traffic-analysis schema.
- Unit tests for the classifier covering each of the five classes plus the "all-exemplars-identical-but-still-volatile" case (tracing IDs).

**Files:**
- new: `internal/wireevidence/evidence.go`, `internal/wireevidence/classifier.go`, `internal/wireevidence/evidence_test.go`
- `internal/browsersniff/specgen.go` (call into wireevidence to build the sidecar)
- `internal/cli/browser_sniff.go` (the `--evidence-output` flag)

### PR 5 — Generator consumes the sidecar (sanitized embed)

**Goal:** When `request-evidence.json` is present and `spec_source: browser-sniffed`, the generator builds requests from the evidence rather than the spec.

**Scope:**
- `client.go.tmpl` gains a `{{if .RequestEvidence}}` branch per endpoint that:
  - Sets host from evidence `base_url` (overriding the spec)
  - Replays `protocol-constant` and `semantic-default` headers/fields verbatim, with `--header` / `--<field>` override capability
  - Reads `auth-secret`-classed fields from the configured env var
  - Drops `volatile-drop`-classed slots entirely
  - Surfaces `unknown`-classed slots as required CLI flags
- Sanitization before embed: walk the evidence, redact any value classified `auth-secret` (replace with `${ENV_VAR}` placeholder), then `//go:embed` the redacted JSON into the binary. Original sidecar stays on disk for debug.
- Fallback: if no evidence file is present or the spec source is not sniffed, current behavior unchanged.

**Depends on:** PR 4.

**Acceptance:**
- A two-host sniffed CLI built from the evidence routes each endpoint to its observed host without `--preserve-hosts` being passed (the evidence's `base_url` is the source of truth).
- Building a CLI from a capture that included auth-secret slots produces a binary whose embedded JSON has those slots redacted (verified by scanning the binary for the literal secret).
- Non-sniffed sources (e.g., the `stytch.yaml` golden spec) generate identically before and after this PR.

**Files:**
- `internal/generator/templates/client.go.tmpl`
- `internal/generator/generator.go` (load + sanitize + embed evidence)
- new: `internal/generator/evidence_embed.go` (redaction + embed wiring)

### PR 6 — Golden-wire validation gate

**Goal:** A new quality gate that asserts the generated client constructs wire-byte-identical requests to the captured exemplars (modulo redacted auth slots).

**Scope:**
- New `--validate` step: for each evidence entry, invoke the generated client function with the recorded args (auth slots filled with the original captured values from the un-redacted sidecar on disk), capture the constructed `http.Request`, and assert it matches the captured request on: method, URL, headers (less-redacted set), query keys+values, body bytes.
- No live API calls. Strictly offline byte-equality.
- Tolerates ordering differences in headers and form fields.
- Fails the build if any endpoint's reconstructed wire differs from its evidence.

**Depends on:** PR 5.

**Acceptance:**
- A regression in any of PRs 1–5 is caught by this gate (verify by reverting one PR's change and confirming the gate fires).
- The Slack experiment HAR (or a synthetic equivalent stored as test fixture) round-trips: capture → generate → validate, all green.
- Gate adds < 10s to total generate time for a 50-endpoint spec.

**Files:**
- `internal/generator/validate.go` (new gate)
- `internal/generator/generator.go` (wire into the existing validation phase)
- testdata fixture HAR for the gate

## Out of scope (separate small PRs, unrelated to wire reconstruction)

- Reserved resource name auto-rename (`cache` → `cache_resource`) instead of hard error at parse time. Affects all input modes.
- Resource grouping by dot-prefix in sniffed paths (`apps.profile.get` collapsing into an `apps` group with `profile.get` endpoint).
- `{ok, error}` envelope unwrap detection (output-quality, not wire-correctness).
- Skill reference at `skills/printing-press/references/macos-app-sniff-capture.md` documenting the `mitmdump --mode local:<AppName>` workflow for Phase 1.7's gate. (Not in this branch — separate skill PR.)

## Open implementation questions

These have working defaults but may need revisiting once PRs 1–3 are in:

1. **How should `cookie_mode` be auto-promoted on existing OpenAPI specs that declare `in: cookie`?** Default: `named_token` (current behavior). User can override in the spec file. Risk: a real-world OpenAPI spec that does mean session-header semantics would need manual annotation.
2. **Body-field name normalization for multipart fields with hyphens (`user-id`) vs. snake-case (`user_id`).** The press's flag-naming convention strongly prefers kebab-case CLI flags. Need a deterministic mapping that doesn't collide.
3. **What constitutes "identical across all exemplars" when there's only one exemplar?** Currently the experiment captured 1–2 calls per endpoint. With N=1, every slot looks constant. Mitigation: lean harder on name-pattern signals when exemplar count is low; mark the evidence entry with a `low_confidence` flag the generator surfaces in `doctor`.
4. **Redaction format inside the embedded sidecar.** Use `${ENV_NAME}` placeholders that the template branch resolves, or use a sentinel + a runtime-resolution helper? The placeholder form is simpler but bleeds env-var names into the binary; the sentinel form is more flexible but adds a lookup step.

## Sequencing notes

- **PRs 1 + 2 + 3 can land in any order or concurrently.** They are independent wire fixes.
- **PR 4 depends on PR 3** because the evidence model needs body shape from day one.
- **PR 5 depends on PR 4** (generator consumes the sidecar).
- **PR 6 depends on PR 5** (the validation gate needs the new codegen path to validate against).
- **All six should land before any new sniffed CLI is published.** Existing published sniffed CLIs (factor75, etc.) are not impacted unless regenerated.

## Verification of artifacts referenced

This document refers to runtime artifacts from the 2026-06-02 experiment. They live under `/tmp/press-experiment/` on the author's machine (will not survive a reboot) and are intentionally not committed:
- `slack-local.har` (1.2 MB clean capture)
- `out-local/slack-spec.yaml` (15 endpoints, 11 resources)
- `out-local/slack-traffic-analysis.json` (analysis sidecar)
- `slack-pp-cli/` (printed Go project, 70 files, 62 MB binary)

If those artifacts are needed for regression test fixtures (e.g., PR 6), they should be re-captured, redacted of any session-identifying values, and committed under `testdata/sniff/`.

## Codex review trail

Two `codex exec` review passes on 2026-06-02:

1. First pass reframed the proposal from "exemplar-driven generation replaces specs" to "evidence-anchored request templates" as an additive sidecar. Introduced the five-class constant classifier. Listed counter-examples (page-1-only captures, single-200 hiding optional fields, ephemeral signatures, sequence-dependent calls, mutating endpoints, single-sample classification ambiguity).
2. Second pass validated the six-phase plan. Corrections incorporated above: PR 1 is not a one-liner (must split named-token from session-header), PR 0.3 telemetry blocklist moved into PR 4's classifier vocabulary, PR 3 must precede PR 4 (body shape is part of the evidence model), PR 6 should land as offline golden-wire validation before any further classifier sophistication.

Prompts and responses live in `/tmp/press-experiment/codex-prompt.md`, `/tmp/press-experiment/codex-plan-review.md`, and the response file. They are not committed.
