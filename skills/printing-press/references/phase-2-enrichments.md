# Phase 2: Pre-Generation Enrichments

Lazy-loaded bodies for the four Phase 2 pre-generation enrichment steps (Category, Cache, Auth, MCP). The dispatcher keeps a skeleton naming each step and what it decides; this file holds the full procedure. Read the relevant section in full before the Lock-and-Generate invocation. Catalog-mode runs skip Category/Cache enrichment.

### Pre-Generation Category Enrichment

Before generating a non-catalog CLI, set the spec's top-level `category` before
running `generate`. The category must come from the Phase 1 research brief's
domain judgment, mapped to the public catalog enum documented in
`docs/CATALOG.md`.

Non-catalog means the run is based on browser-sniffed traffic, HAR capture,
docs-derived specs, or a hand-authored internal spec rather than
`cli-printing-press generate <name>` using a built-in catalog entry. For
internal YAML specs, add:

```yaml
category: <catalog-category>
```

If the source is an OpenAPI file and the workflow has an editable overlay or
derived internal spec, carry the same top-level category into that generated
spec artifact before the final `generate` invocation. If there is no editable
spec artifact, such as direct `--docs` generation, pass
`--category <catalog-category>` on the final `generate` invocation. Do not add
the category after generation just to satisfy publish; the generated manifest,
README, and SKILL install section must all come from the same category-aware
spec, or `verify-skill canonical-sections` can drift.

Catalog-mode runs skip this step: keep the built-in catalog entry's category
unchanged, even if Phase 1 research would classify the API differently.

### Pre-Generation Cache Enrichment

Before generating, decide whether the spec should opt into generator-owned cache
freshness. The generator already has the freshness helpers and auto-refresh hook,
but it emits them only when the spec declares `cache.enabled: true` and the CLI
has a real sync path. Stateful catalog-shaped CLIs otherwise serve local data
exactly as it was last synced, which caps the cache freshness score and can leave
agents reading stale SQLite rows without a warning.

Enable cache freshness only when the resolved spec, profiler output, or absorb
manifest shows at least one covered read path backed by a syncable resource that
`sync` can refresh from the upstream API before serving. Do not enable it from
Phase 1 research notes or scorecard goals alone. Leave it disabled for stateless
read-through wrappers and for local stores that are primarily per-user working
state, such as carts, drafts, or other session-owned data where a pre-read
refresh could replace the user's local state with a different snapshot. Also
leave it disabled for quota-metered, paid, rate-limited, or expensive bulk
refresh APIs unless the refresh path is cheap, bounded, and clearly valuable;
those CLIs should rely on manual `sync` plus the generated `doctor` cache report
instead of surprising users with pre-read upstream calls.

Catalog-mode runs skip this step: keep the built-in catalog entry's cache
settings unchanged. Do not pass a flag or patch generated files after the fact;
cache freshness must come from the spec that drives generation.

For internal YAML specs, add the cache block before the final `generate`
invocation only when at least one generated syncable resource read command will
be covered automatically, or `cache.commands` will register a real hand-authored
store-reading command:

```yaml
cache:
  enabled: true
  stale_after: 168h        # choose a domain-appropriate default
  refresh_timeout: 30s     # optional; blank uses the generated runtime default
```

Generated resource list/get/search commands are covered automatically from the
syncable resources profile. Use `cache.commands` only for hand-authored novel
commands that read the local store and are not generated resource commands. The
command `name` is the Cobra path without the binary name, and every listed
resource must be declared in `resources:` and classified as syncable.

```yaml
cache:
  enabled: true
  stale_after: 168h        # choose a domain-appropriate default
  commands:
    - name: <novel-read-command>
      resources: [<resource-name>]
```

Pick `stale_after` from the domain's update cadence: shorter for live feeds or
rapidly changing inventory, longer for reference catalogs and archival data. Do
not enable cache just to satisfy the scorecard if there is no upstream refresh
path or no user value in pre-read freshness; the generator intentionally skips
the helpers when they would be dead code.

### Pre-Generation Auth Enrichment

Before generating, check whether the resolved spec has auth. This matters most for
browser-sniffed and crowd-sniffed specs where the mechanical auth detection may have failed
(e.g., session expired during browser-sniff, SDK didn't expose auth patterns).

**Check the spec:**
- For internal YAML specs: look for `auth:` section with `type:` not equal to `"none"`
- For OpenAPI specs: look for `components.securitySchemes` or `security` sections

**If auth is missing** (`type: none` or no auth section) AND Phase 1 research found
auth signals, enrich the spec before generation:

1. Check the research brief for auth mentions (Bearer, API key, token, cookie, OAuth)
2. Check Phase 1.5a MCP source code analysis for auth patterns (header names, token formats)
3. Check Phase 1.6 Pre-Browser-Sniff Auth Intelligence results (if the user confirmed auth)

If any source identified auth, **edit the spec YAML** to add the auth section before
running generate. Catalog-mode runs (`cli-printing-press generate <name>` where `<name>`
is in `catalog/`) can skip the spec edit when the catalog entry declares
`auth_env_vars` — those canonical names are applied automatically and the
parser's name-derived default name is retained as a trailing fallback so
operators on existing setups don't need a rename. For internal YAML specs:

```yaml
auth:
  type: bearer_token    # or api_key, depending on what research found
  header: Authorization # or the specific header from MCP source
  in: header
  env_vars:
    - <API_NAME>_TOKEN  # bearer_token → _TOKEN, api_key → _API_KEY
```

When research or source metadata names a real single-token env var, record it
in `research.json` as `auth.canonical_env_var`; fresh generation reads that
name first and keeps the parser-derived env var as a trailing fallback. When
you are editing an internal YAML spec directly, use only the canonical name in
`env_vars`; do not add guessed slug-based aliases.

For OpenAPI specs, choose the security scheme by wire format, not by whether
the token feels like an API key. Use `type: http` with `scheme: bearer` when
the upstream API sends `Authorization: Bearer <token>`, including PAT-shaped
tokens such as Slack `xoxp`, Notion integration tokens, Linear API keys, and
GitHub PATs. Use `type: apiKey` only when the API sends the configured value
as the raw header or query value, such as `X-API-Key: <token>` or
`Authorization: <token>` with no scheme prefix. The generator adds the
`Bearer ` prefix for `http` bearer schemes; `apiKey` sends exactly the
configured value and will not add a prefix.

Quick test: if upstream docs or live traffic show `Authorization: Bearer
<token>`, model it as `http` bearer. If they show `X-API-Key: <token>`,
`?api_key=<token>`, or `Authorization: <token>` with no scheme prefix, model it
as `apiKey`.

For OpenAPI specs, prefer `x-auth-env-vars` on the selected security scheme
when the wrapper slug differs from the underlying API brand.

**If auth IS present** in the spec but Phase 1 evidence shows the slug-derived
env var will differ from the canonical name users have already set for this
API, enrich the spec with the canonical name before generation. The
slug-derivation rule (security-scheme slug uppercased plus `_TOKEN` /
`_API_KEY` / `_OAUTH2` per type) rarely matches the canonical name for
established APIs. Common shapes:

- Stripe (bearer): canonical `STRIPE_SECRET_KEY`, not slug-derived `STRIPE_OAUTH2`
- HubSpot (bearer): canonical `HUBSPOT_PRIVATE_APP_TOKEN`, not slug-derived `HUBSPOT_API_KEY`
- Twilio (HTTP Basic, two-var pair): canonical `TWILIO_ACCOUNT_SID` + `TWILIO_AUTH_TOKEN`, not slug-derived `TWILIO_USERNAME` + `TWILIO_PASSWORD`
- Keap (OAuth2 authorization-code): canonical `KEAP_SERVICE_ACCOUNT_KEY`, not slug-derived `KEAP_OAUTH2`

Walk through:

1. Compute the slug-derived env var the generator will pick (security-scheme
   slug, uppercased, plus the type-suffix above; HTTP Basic produces a
   `_USERNAME` + `_PASSWORD` pair; OAuth2 `client_credentials` produces a
   `_CLIENT_ID` + `_CLIENT_SECRET` pair).
2. Check Phase 1 research, Phase 1.5a MCP source code analysis, and community
   wrapper READMEs for a canonical env var name documented by the vendor or
   in widespread use.
3. If they differ and the canonical name is a single-token credential, record
   it in `research.json` as `auth.canonical_env_var`. The generator will read
   the canonical name first and retain the slug-derived form as a fallback.
   If you are editing the source spec directly instead, add `x-auth-env-vars`
   on the selected security scheme (OpenAPI) or set `auth.env_vars` to the
   canonical name (internal YAML). Use only the canonical name in the spec
   edit; do not retain guessed aliases there. For HTTP Basic, supply the full
   two-entry canonical pair (username position first, password position
   second) via `x-auth-env-vars`. For OAuth2
   `client_credentials`, the parser silently re-applies the
   `CLIENT_ID`/`CLIENT_SECRET` default when `x-auth-env-vars` has fewer
   than two entries (see `applyAuthEnvVarDefaults` in
   `internal/openapi/parser.go`); if the canonical secret is a single
   service-account token for a `client_credentials` flow, use
   `x-auth-vars` instead (next section) so the override is preserved.
4. If research surfaces no canonical name distinct from the slug-derived
   form, do nothing. The slug-derived name is fine, and a spurious
   `x-auth-env-vars` would just shadow it with the same value.

```yaml
# Bearer / API-key single-token case (Stripe, HubSpot, Keap on
# authorization-code grant, most apiKey schemes).
components:
  securitySchemes:
    keapOAuth2:
      type: oauth2
      flows:
        authorizationCode: { ... }
      x-auth-env-vars:
        - KEAP_SERVICE_ACCOUNT_KEY
```

```yaml
# HTTP Basic two-var canonical pair (Twilio).
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
      x-auth-env-vars:
        - TWILIO_ACCOUNT_SID
        - TWILIO_AUTH_TOKEN
```

Skipping this step pushes the agent into hand-patching
`internal/config/config.go` Load and `internal/cli/doctor.go` env-var
checks after a `doctor` FAIL against the operator's real environment.
Enriching the spec avoids that round-trip.

For OpenAPI bearer-token specs that need richer env-var metadata (kind
classification, optional credentials, OR-group relationships), keep the
security scheme as `http` bearer and put `x-auth-vars` on that scheme. Do not
switch to `apiKey` just to attach the richer metadata.

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: xoxp
      x-auth-vars:
        - name: SLACK_BOT_TOKEN
          kind: per_call
          required: false
          sensitive: true
          description: Set this OR `SLACK_USER_TOKEN` for workspace API calls.
        - name: SLACK_USER_TOKEN
          kind: per_call
          required: false
          sensitive: true
          description: Set this OR `SLACK_BOT_TOKEN` for user-scoped API calls.
```

For OpenAPI raw-key schemes that need richer env-var metadata, keep `apiKey`
and place `x-auth-vars` on the raw-key scheme.

```yaml
components:
  securitySchemes:
    rawHeaderKey:
      type: apiKey
      in: header
      name: X-API-Key
      x-auth-vars:
        - name: <API_NAME>_API_KEY
          kind: per_call
          required: true
          sensitive: true
          description: Raw API key header value.
```

See `docs/SPEC-EXTENSIONS.md` for the canonical `x-auth-vars` schema.

`kind` controls who supplies the value:
- `per_call` is the default user-supplied credential used by normal commands.
- `auth_flow_input` is only needed during `auth login`.
- `harvested` is populated by the auth login flow into local config.

`sensitive: true` means credential material that must be redacted in logs and
agent context. Use `sensitive: false` for public configuration, such as an OAuth
`client_id`.

Encode AND/OR relationships with each var's `required` flag plus `description`
text. There is no first-class group syntax. For OR cases, mark each alternative
`required: false` and name the other option in the description. For AND cases,
mark each required member `required: true`.

The parser auto-classifies cookie schemes as `harvested` and OAuth2
`client_credentials` inputs as `auth_flow_input`. Add `x-auth-vars` only when
overriding those defaults or resolving multi-scheme ambiguity.

For OpenAPI specs, add an `info.description` mention if one doesn't exist — the
parser's `inferDescriptionAuth` will detect it automatically.

**Why enrich before generation, not after:** The generator's templates (config, client,
doctor, auth, README) all read `Auth.*` fields from the spec. Patching config.go after
generation only fixes env var support — it misses the doctor auth check, client auth
header, README auth section, and auth command setup. Enriching the spec means every
template produces correct auth from the start.

**When to skip:** If the API genuinely doesn't need auth (ESPN public endpoints, weather
APIs, public data feeds), don't invent auth. The signal must come from research — not
from guessing. No research mention of auth = no enrichment.

#### Public Parameter Name Enrichment

Before generating, inspect endpoint params and body fields whose API names would
make poor CLI or MCP inputs, especially one-letter keys, punctuation-heavy keys,
or names that only make sense inside the upstream protocol. When clear evidence
shows the user-facing meaning, author `flag_name` in the internal spec or overlay
and add `aliases` only for compatibility spellings.

This is an agent judgment step, not a generator inference step. The skill uses
research, docs, SDK/source names, browser-sniff form labels, traffic-analysis
request context, and endpoint workflow evidence to decide the semantic name.
The Printing Press CLI validates and propagates that authored data through Cobra
flags, generated examples, typed MCP schemas, and `tools-manifest.json`; it must
not guess that a cryptic key always means the same thing across APIs.

Run the deterministic inventory before generation when a spec may contain
cryptic wire names:

```bash
cli-printing-press public-param-audit --spec <spec-or-overlay-output> --ledger <runstate>/public-param-audit.json --strict
```

The command does not decide that every finding needs a public flag. It identifies
parameters that need an agent decision. For each pending finding, either:

- Author `flag_name` and compatibility `aliases` in the spec or overlay, then rerun
  the audit so the finding becomes resolved from the spec itself.
- Record `decision: "skip"` in the ledger with `source_evidence` and `skip_reason`
  when source material shows no public rename is warranted.

A ledger entry with `decision: "flag_name"` or `proposed_flag_name` is only a note
to the agent; it is not complete until the spec or overlay actually contains the
public name. Strict mode fails on unreviewed findings, not on evidence-backed
skips. A one-letter wire name alone is enough to enter the inventory, but not
enough to author a rename.

Good evidence:
- Explicit parameter descriptions, vendor docs, or SDK argument names.
- Browser-sniff form/input labels and the interaction that produced the request.
- Traffic-analysis context tying the parameter to a specific endpoint workflow.
- Existing manuscript notes or reviewed examples that use the same task-level term.

Bad evidence:
- A one-letter name alone.
- Generic descriptions such as "query parameter" or "string value".
- Ambiguous sample values with no endpoint context.
- Global assumptions such as "`s` means search" or "`c` means city".

Prefer concise task-level names an agent would naturally use on that command. For
a store-locator endpoint with `s` described as `Street address` and `c` described
as `City, state, zip`, author:

```yaml
params:
  - name: s
    flag_name: address
    aliases: [s]
    description: Street address
  - name: c
    flag_name: city
    aliases: [c]
    description: City, state, zip
```

The upstream wire keys remain `s` and `c`; generated users and agents see
`address` and `city`. If the evidence is unclear after research, leave
`flag_name` unset, record the reviewed source material and evidence gap in the
audit ledger, and preserve the gap in the manuscript rather than inventing a
friendly name.

#### Free/Paid Tier Routing Enrichment

If Phase 1 finds that the headline commands should stay free but secondary
enrichment needs a paid key, declare tier routing in the spec before generation.
Do this only when research identifies a real split; do not invent tiers for a
single-auth API.

Detection signals:
- Research or source-priority notes say the primary source is free but a secondary
  source needs a paid/API key.
- Some endpoints are documented as public while adjacent enrichment endpoints are
  documented as paid, partner, premium, or quota-gated.
- Browser/crowd sniffing found public endpoints and SDK/MCP research found a
  separate credential for expanded coverage.
- Sniffed specs carry per-endpoint `observed_auth` (a list of lowercased request
  header names observed during capture, e.g. `[authorization]` or `[x-api-key]`).
  An empty or missing `observed_auth` on an endpoint is evidence that the request
  went anonymously; a populated list is evidence the endpoint required auth. The
  same per-endpoint signal is mirrored on `TrafficAnalysis.endpoint_clusters[].observed_auth`
  in the traffic-analysis sidecar. Treat both as observation-only — not a security
  scheme declaration — and corroborate with documentation before declaring a tier.

Action:
- Internal YAML: add `tier_routing` plus `tier` on the affected resource or
  endpoint.
- OpenAPI: add `x-tier-routing` at the root or under `info`, and add `x-tier`
  to the path item or operation.
- Use `auth.type: none` for the free tier.
- Use only `api_key` or `bearer_token` for credential tiers in v1.
- If a credential tier uses a different `base_url`, it must be HTTPS and same
  host-family unless `allow_cross_host_auth: true` records explicit review.
- Do not combine `no_auth: true` or OpenAPI `security: []` with a credential tier.

Skip when:
- All useful commands require the same credential.
- The paid source would become the primary headline surface instead of enrichment.
- The auth split requires OAuth, cookie, composed, or session-handshake tier auth;
  handle that as a normal auth-mode decision for now.

Example:

```yaml
tier_routing:
  default_tier: free
  tiers:
    free:
      auth: {type: none}
    paid:
      auth:
        type: api_key
        in: query
        header: api_key
        env_vars: [EXAMPLE_PAID_KEY]
resources:
  search:
    tier: free
    endpoints:
      list:
        method: GET
        path: /search
      enrich:
        method: GET
        path: /paid/search
        tier: paid
```

#### Tagging endpoints `no_auth: true` (composed/cookie auth APIs)

For APIs whose `auth.type` is `cookie`, `composed`, or `session_handshake` — i.e.,
auth that requires interactive setup (browser cookie capture, multi-step token
exchange) — audit each endpoint individually for whether it actually needs
authentication. The default of `no_auth: false` means "auth required"; flip it to
`no_auth: true` for endpoints that work without credentials.

Typical unauthenticated endpoints worth tagging:

- **Auth-flow primitives:** login, registration, password-reset, email-confirm,
  refresh-token, OAuth callback. The user isn't authenticated when calling these —
  they ARE the auth flow.
- **Public discovery:** store/location finder, menu browse, public catalog,
  category listing, public search, public product detail.
- **Health/metadata:** health checks, version probes, capability flags, sitemap.

Why this matters: the `no_auth` count drives downstream decisions. Specifically,
a composed-auth API with zero `no_auth: true` tags previously got labeled
`mcp_ready: cli-only` and was suppressed from MCPB manifest emission, which
broke the Claude Desktop install path entirely. The current generator
(post-2.5) ships a manifest regardless, but the readiness label, scorecard
breadth dimension, and SKILL.md prose all read better when the count
reflects reality.

If unsure whether an endpoint requires auth, the safe default is `no_auth: false`
(auth required) — over-tagging can mislead users to expect tools that won't work.

**Example for a composed-auth pizza-ordering API:**

```yaml
resources:
  account:
    endpoints:
      register:
        method: POST
        path: /account/register
        no_auth: true   # registering means you're not yet authenticated
      login:
        method: POST
        path: /account/login
        no_auth: true   # the auth flow itself
      profile:
        method: GET
        path: /account/profile
        # no_auth defaults to false — needs auth to view your own profile
  stores:
    endpoints:
      find:
        method: GET
        path: /stores/near
        no_auth: true   # public store finder
  cart:
    endpoints:
      checkout:
        method: POST
        path: /cart/checkout
        # no_auth defaults to false — placing an order needs auth
```

#### Cookie/composed HTML transport

For specs with `auth.type: cookie` or `auth.type: composed` and any
`response_format: html` endpoint, treat browser fingerprint compatibility as
the safe default. The generator emits Surf-backed Chrome transport for that
shape unless the spec explicitly says `http_transport: standard`.

Before setting an explicit standard opt-out, run
`cli-printing-press probe-reachability` against a representative HTML GET endpoint.
If the probe returns `standard_http`, record `http_transport: standard` in the
spec. If it returns `browser_http`, leave the default or set `http_transport:
browser-chrome`. If it returns `browser_clearance_http`, return to the
browser-clearance flow above so the generated CLI has both browser-compatible
HTTP and reusable browser auth proof.

For cookie/composed-auth CLIs, recommend the `press-auth` companion binary —
it captures cookies once via a controlled Chrome window and serves them to
generated CLIs on demand, avoiding the on-disk session-cookie blind spot
that breaks `auth login --chrome` against a daily Chrome profile. See
[references/auth-companion.md](references/auth-companion.md) for the
recommendation flow, install command, and debug playbook.

### Pre-Generation MCP Enrichment

Before generating, count the spec's MCP tool surface and decide whether to opt
into the spec's `mcp:` enrichment fields. This matters most for medium-to-large
APIs (>30 tools) where the default endpoint-mirror surface scores poorly on the
scorecard's MCP architectural dimensions and burns agent context at runtime.

**Why before generation, not after:** the generator emits the MCP server's
`main.go`, `tools.go`, `intents.go`, `code_orch.go`, `tools-manifest.json`, and
README MCP section from the spec at generate-time. Patching after generation
fragments across 4+ files, won't be byte-identical, and the polish skill cannot
fix it (polish doesn't re-run generation). Enriching the spec means every
template emits the right surface from the start.

**Count the tool surface.** Two parts:

1. **Typed endpoints** — count `endpoints` across all `resources` (and
   `sub_resources`) in the spec. These become per-endpoint MCP tools at
   generate-time.
2. **Cobratree-walked tools** — the runtime walker registers user-facing Cobra
   commands as MCP tools. Estimate as: `extra_commands` count + ~13 framework
   tools that ship by default (sql, search, context, sync, stale, doctor,
   reconcile, etc., minus framework-skipped). When novel features are planned,
   add their estimated command count.

The total is what an agent loads at MCP server start.

**Decision table:**

| Total tools | Action |
|-------------|--------|
| <30 | Skip — default endpoint-mirror surface is fine. |
| 30–50 | Ask the user. Suggest `mcp.transport: [stdio, http]` for remote reach; suggest `mcp.intents` if there are clear multi-step workflows. |
| >50 | The generator auto-applies the Cloudflare pattern (transport + code orchestration + hidden endpoint tools) unless `mcp.orchestration` / `x-mcp.orchestration` is explicitly set. |

**Mandatory >50 endpoint-tools confirmation.** If the pre-generation count
predicts more than 50 endpoint tools, expect `generate` to print an informational
line beginning `info: applied Cloudflare MCP pattern`. This is the intended
default and does not require a blocking question. Before verification, polish,
dogfood, or publish, confirm the generated MCP surface is the thin
`<api>_search` + `<api>_execute` pair. If the user explicitly wants raw
endpoint tools past the threshold, set `mcp.orchestration: endpoint-mirror`
(internal YAML) or `x-mcp.orchestration: endpoint-mirror` (OpenAPI) before
regenerating.

**The Cloudflare pattern** (default for large surfaces without explicit
orchestration) — the generator applies this shape automatically. Add the spec
block only when you need to make the choice explicit or preserve it across
older generator versions:

```yaml
mcp:
  transport: [stdio, http]    # remote-capable; reaches hosted agents
  orchestration: code         # thin <api>_search + <api>_execute pair
  endpoint_tools: hidden      # suppress raw per-endpoint mirrors
  intents:                    # optional; named multi-step intents
    - name: fetch_and_summarize
      description: Fetch an item then summarize it
      params:
        - name: item_id
          type: string
          required: true
          description: item identifier
      steps:
        - endpoint: items.get
          bind:
            id: ${input.item_id}
          capture: item
        - endpoint: items.summarize
          bind:
            body: ${item.body}
          capture: summary
      returns: summary
```

`mcp.transport: [stdio, http]` adds HTTP streamable transport so cloud-hosted
agents (Managed Agents, web clients) can connect. `mcp.orchestration: code`
emits the thin search+execute pair that covers the full surface in ~1K tokens.
`mcp.endpoint_tools: hidden` removes the raw per-endpoint tools that would
otherwise still show up alongside the orchestration pair.

For OpenAPI input specs, declare these fields under `x-mcp:` at the document
root (OpenAPI 3.0 `x-*` vendor extensions). The shape is identical to the
internal-YAML `mcp:` block above — same field names, just nested under a
vendor-extension key. See [`docs/SPEC-EXTENSIONS.md`](../../docs/SPEC-EXTENSIONS.md) for the canonical
schema and `info`-level placement option.

**Smaller-surface variants:**

- Just want remote reach? Small APIs (at or under
  `spec.DefaultRemoteTransportEndpointThreshold` typed endpoints) get `[stdio, http]`
  by default — no spec edit needed. Set `mcp.transport: [stdio, http]` explicitly only
  when the API is above the threshold and still wants remote reach.
- Have 3–5 obvious multi-step workflows but <50 endpoints? Add `mcp.intents`
  without code orchestration; leave `endpoint_tools` at default (visible).

**When to skip entirely:** small APIs (<30 tools), one-shot specs that won't
be installed as MCP servers, or APIs where the user explicitly opts out of MCP
enrichment.

**Verifying after generation:** the scorecard's `mcp_remote_transport`,
`mcp_tool_design`, and `mcp_surface_strategy` dimensions reflect the choices
above. A correctly enriched spec for a >50 tool API should score 10/10 on all
three. If polish later reports these dims weak, that's a sign this enrichment
step was skipped — re-run generation with the enriched spec rather than
trying to fix it in polish.

