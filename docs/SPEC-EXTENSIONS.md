# OpenAPI Extensions

This document is the canonical reference for Printing Press-specific OpenAPI
`x-*` extensions. OpenAPI allows extension fields anywhere, but the Printing
Press only reads the extensions listed here.

Source of truth: `internal/openapi/parser.go`. This document should be updated
in the same change as any new `Extensions["x-*"]` lookup in that file.

## Summary

| Extension | Location | Parsed field | Required |
|-----------|----------|--------------|----------|
| `x-api-name` | `info` | `APISpec.Name` | No |
| `x-display-name` | `info` | `APISpec.DisplayName` | No |
| `x-website` | `info` | `APISpec.WebsiteURL` | No |
| `x-proxy-routes` | `info` | `APISpec.ProxyRoutes` | No |
| `x-origin` | `info` | Google Discovery resource fallback | No |
| `x-providerName` | `info` | Google Discovery resource fallback | No |
| `x-roles` | root or `info` | `APISpec.Roles` | No |
| `x-tier-routing` | root or `info` | `APISpec.TierRouting` | No |
| `x-rate-class` | root or `info` | `APISpec.RateClass` | No |
| `x-mcp` | root or `info` | `APISpec.MCP` | No |
| `x-cache` | root or `info` | `APISpec.Cache` | No |
| `x-pp-query` | root | `APISpec.QuerySync` | No |
| `x-auth-type` | `components.securitySchemes.<name>` | `APISpec.Auth.Type` | No |
| `x-auth-format` | `components.securitySchemes.<name>` | `APISpec.Auth.Format` | No |
| `x-prefix` | `components.securitySchemes.<name>` | `APISpec.Auth.Format` | No |
| `x-auth-env-vars` | `components.securitySchemes.<name>` | `APISpec.Auth.EnvVars` | No |
| `x-auth-vars` | `components.securitySchemes.<name>` | `APISpec.Auth.EnvVarSpecs` | No |
| `x-speakeasy-example` | `components.securitySchemes.<name>` | `APISpec.Auth.EnvVars` | No |
| `x-auth-optional` | `components.securitySchemes.<name>` | `APISpec.Auth.Optional` | No |
| `x-auth-key-url` | `components.securitySchemes.<name>` | `APISpec.Auth.KeyURL` | No |
| `x-auth-title` | `components.securitySchemes.<name>` | `APISpec.Auth.Title` | No |
| `x-auth-description` | `components.securitySchemes.<name>` | `APISpec.Auth.Description` | No |
| `x-auth-subtype` | `components.securitySchemes.<name>` | `APISpec.Auth.Subtype` | No |
| `x-auth-cookie-domain` | `components.securitySchemes.<name>` | `APISpec.Auth.CookieDomain` | No |
| `x-auth-cookies` | `components.securitySchemes.<name>` | `APISpec.Auth.Cookies` | No |
| `x-auth-companion` | `components.securitySchemes.<name>` or `info` | `APISpec.Auth.LoginURL`, `LoginCompleteSelector`, `JWTCarrierCookie` | No |
| `x-oauth-device-flow` | `components.securitySchemes.<name>` | `APISpec.Auth.OAuth2Grant`, `DeviceAuthorizationURL`, `TokenURL`, `Scopes`, `DefaultClientID` | No |
| `x-oauth-refresh-token-mechanism` | `components.securitySchemes.<name>` | `APISpec.Auth.RefreshTokenMechanism` | No |
| `x-resource-id` | path item | `Endpoint.IDField` | No |
| `x-critical` | path item | `Endpoint.Critical` | No |
| `x-tier` | path item or operation | `Endpoint.Tier` | No |
| `x-data-source-strategy` | path item or operation | `Endpoint.DataSourceStrategy` | No |
| `x-requires-role` | operation | `Endpoint.RequiresRole` | No |
| `x-happy-args` | operation | `Endpoint.HappyArgs` | No |
| `x-pp-resource` | operation | resource name override | No |
| `x-pp-safe-probe` | operation | *skill guidance only; not parsed in parser.go* | No |
| `x-pp-sync-walker` | operation | `Endpoint.Walker` | No |
| `x-pp-dispatch-param` | parameter | `Param.DispatchParam` | No |

## `info` Extensions

### `x-api-name`

Overrides the API slug only when `info.title` does not fold to a usable slug.
The parser first applies its normal name cleaning to `info.title`; `x-api-name`
is only consulted when that result is empty or `api`.

Parsed field: `APISpec.Name`

Rules:
- Optional.
- Must be a string.
- Cleaned with the same slug normalization as `info.title`.
- Ignored when the cleaned value is empty or `api`.
- Ignored when `info.title` already produced a usable slug.

Example:

```yaml
info:
  title: API
  version: "1.0"
  x-api-name: example-service
```

### `x-display-name`

Preserves the human-readable brand name when slug-derived title casing would
deform it.

Parsed field: `APISpec.DisplayName`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- Empty or non-string values leave `DisplayName` empty, so downstream code falls
  back to catalog metadata or slug-derived naming.
- The parser does not enforce a length cap for `x-display-name`. The separate
  `registry.json` display-name fallback used by `mcp-sync` rejects registry
  values longer than 40 characters, but that limit does not apply here.

Example:

```yaml
info:
  title: Cal Com
  version: "1.0"
  x-display-name: Cal.com
```

### `x-website`

Provides a product or vendor website URL when standard OpenAPI metadata does
not carry one.

Parsed field: `APISpec.WebsiteURL`

Rules:
- Optional.
- Must be a string.
- Used only when `info.contact.url` is absent.
- `externalDocs.url` is used after `x-website` if no website URL has been found.
- The parser does not validate the URL shape.

Example:

```yaml
info:
  title: Example Service
  version: "1.0"
  x-website: https://www.example.com
```

### `x-proxy-routes`

Declares route-to-service mapping for the proxy-envelope client pattern.

Parsed field: `APISpec.ProxyRoutes`

Rules:
- Optional.
- Must be a map.
- Map keys are path prefixes.
- Map values must be strings; non-string values are skipped.
- A missing or malformed map leaves `ProxyRoutes` empty.

Example:

```yaml
info:
  title: Example Service
  version: "1.0"
  x-proxy-routes:
    /v1/search: search
    /v1/publish: publishing
```

### `x-origin` / `x-providerName`

Recognized on Google Discovery specs converted by apis.guru. These extensions do
not populate an `APISpec` field; they gate the parser's operationId-based
resource fallback for paths such as `/v2/{name}` and `/{resource}:getIamPolicy`.

Rules:
- Optional.
- `x-providerName: googleapis.com` enables Google Discovery resource fallback.
- `x-origin` enables the fallback when any entry has `format: google` or a
  Discovery URL under `googleapis.com/$discovery`.
- Ignored for non-Google specs.

### `x-tier-routing`

Declares opt-in free/paid credential routing for APIs where some endpoints work
without credentials and other endpoints require a separate paid key or token.

Parsed field: `APISpec.TierRouting`

Rules:
- Optional.
- May be declared at the OpenAPI root or under `info`.
- Requires a `tiers` map when present.
- `default_tier` is optional; endpoints without `x-tier` use global auth when it
  is absent.
- V1 tier auth supports only `none`, `api_key`, and `bearer_token`.
- Credential-bearing tier `base_url` values must be HTTPS and cannot point at
  loopback, private, link-local, or unrelated hosts unless
  `allow_cross_host_auth: true` documents explicit review.
- Incompatible with `client_pattern: proxy-envelope` and with resource- or
  endpoint-level `base_url` overrides when any tier declares its own `base_url`.
- Tier credential env vars are read from the environment at request time; they
  are not serialized into generated config files.

Example:

```yaml
x-tier-routing:
  default_tier: free
  tiers:
    free:
      auth:
        type: none
    paid:
      base_url: https://paid.api.example.com
      auth:
        type: api_key
        in: query
        header: api_key
        env_vars: [EXAMPLE_PAID_KEY]
```

### `x-roles`

Declares the authenticated persona labels that operation-level RBAC gates may
reference.

Parsed field: `APISpec.Roles`

Rules:
- Optional.
- May be declared at the OpenAPI root or under `info`.
- Must be a string list.
- Each role must match `^[A-Za-z][A-Za-z0-9_-]*$`.
- Every operation-level `x-requires-role` value must name one declared role.

Example:

```yaml
x-roles: [parent, student, teacher, admin]
```

### `x-rate-class`

Declares the API's rate-limit operating point so generated sync defaults can
avoid wasteful parallelism on low-total-budget APIs.

Parsed field: `APISpec.RateClass`

Rules:
- Optional.
- May be declared at the OpenAPI root or under `info`. Root takes precedence
  when both are present.
- Must be a string.
- Accepted values are `per-second`, `daily`, `monthly`, and `unlimited`.
- `daily` and `monthly` generate `sync --concurrency` with a default of 1.
  `per-second`, `unlimited`, and absent keep the default of 4.
- This only changes generated sync worker defaults. It does not add runtime
  rate limiting, retries, or backoff.

Example:

```yaml
info:
  title: Low Quota API
  version: "1.0"
  x-rate-class: monthly
```

### `x-mcp`

Declares MCP server shape for the generated CLI. Mirrors the internal YAML
spec's top-level `mcp:` block so OpenAPI specs can opt into the same
pre-generation MCP enrichment recipe (notably the code-orchestration pattern
for large surfaces: `transport: [stdio, http]` + `orchestration: code` +
`endpoint_tools: hidden`).

Parsed field: `APISpec.MCP` (`spec.MCPConfig`)

Rules:
- Optional. Specs without `x-mcp` get the endpoint-mirror surface while they
  remain at or below the orchestration threshold. Small APIs (typed-endpoint
  count at or below `spec.DefaultRemoteTransportEndpointThreshold`, currently
  30) also get the http transport compiled in alongside stdio so the same
  binary can reach cloud-hosted agents. Large APIs above
  `spec.DefaultOrchestrationThreshold` (currently 50) default to the Cloudflare
  MCP pattern (`transport: [stdio, http]`, `orchestration: code`, and
  `endpoint_tools: hidden`) when `orchestration` is unset. Set
  `orchestration: endpoint-mirror` to opt out. Setting `transport` explicitly
  (including `transport: [stdio]`) bypasses the transport default and is
  honored as-is.
- May be declared at the OpenAPI root or under `info`. Root takes precedence
  when both are present.
- For backwards compatibility, root-level `mcp:` is also accepted when
  canonical `x-mcp` is absent. The parser emits a warning asking authors to
  rename it to `x-mcp`. `x-mcp` and `mcp` are never merged; any canonical
  `x-mcp` declaration, including under `info`, wins as a complete config.
- Shape mirrors the internal YAML `mcp:` block field-for-field: `transport`,
  `addr`, `intents`, `endpoint_tools`, `orchestration`,
  `orchestration_threshold`.
- Validated by `validateMCP` at spec load (same allowlist as internal YAML):
  unknown transports and malformed addresses are rejected.

Example:

```yaml
x-mcp:
  transport: [stdio, http]
  orchestration: code
  endpoint_tools: hidden
```

### `x-cache`

Declares cache-freshness and auto-refresh behavior for generated CLIs. Mirrors
the internal YAML spec's top-level `cache:` block so OpenAPI specs with a
store-backed sync surface can opt into the same freshness machinery.

Parsed field: `APISpec.Cache` (`spec.CacheConfig`)

Rules:
- Optional. Specs without `x-cache` keep today's behavior: no freshness helper
  or auto-refresh hook is emitted unless cache is configured elsewhere.
- May be declared at the OpenAPI root or under `info`. Root takes precedence
  when both are present.
- Shape mirrors the internal YAML `cache:` block field-for-field: `enabled`,
  `stale_after`, `refresh_timeout`, `env_opt_out`, `resources`, `commands`.
- Validated by the same cache/share validation as internal YAML specs. Duration
  fields must be Go duration strings, `commands` require `enabled: true`, and
  command resource names must refer to parsed resources.

Example:

```yaml
x-cache:
  enabled: true
  stale_after: 6h
  refresh_timeout: 30s
  env_opt_out: EXAMPLE_NO_AUTO_REFRESH
  resources:
    quotes: 5m
  commands:
    - name: dashboard
      resources: [quotes]
```

### `x-pp-query`

Declares the SQL-query-endpoint sync shape (QuickBooks Online, Salesforce SOQL):
an API where every list resource is read through one shared endpoint with an
injected `SELECT`-style query, results wrapped in an entity-named envelope, and
paging carried inside the query text. Mirrors the internal YAML spec's top-level
`query_sync:` block. When present, the sync generator emits the query injection,
the response-envelope unwrap, and the in-query offset-paging loop **by
construction** — gated so a normal REST list API's generated output is
byte-identical to today.

Parsed field: `APISpec.QuerySync` (`spec.QuerySyncConfig`)

Rules:
- Optional. Specs without `x-pp-query` keep today's REST sync behavior exactly.
- Declared at the OpenAPI root (the internal YAML form lives in the top-level
  `query_sync:` block). All query dialect text lives in the hint, never the
  generator — the template substitutes the `{entity}`, `{start}`, and `{limit}`
  placeholders at runtime.
- A resource participates only when its list endpoint's path equals `path` AND
  it declares a `response_path` (e.g. `QueryResponse.<Entity>`); the per-resource
  entity name is taken from that endpoint's response item (e.g. `Invoice`). Raw
  passthrough resources on the same path with no `response_path` are skipped.
- Fields: `path` (required, the shared query endpoint, e.g. `/query`);
  `query_param` (the param carrying the SELECT, default `query`);
  `query_template` (required, the SELECT + paging clause with
  `{entity}`/`{start}`/`{limit}` placeholders); `version_param` + `version_value`
  (an optional extra param sent on every query call); `envelope_key` (the
  result-envelope object key joined to the runtime extractor list, e.g.
  `QueryResponse`); `page_size` (in-query page size and offset stride, default
  `1000`).

Example:

```yaml
x-pp-query:
  path: /query
  query_param: query
  query_template: "select * from {entity} startposition {start} maxresults {limit}"
  version_param: minorversion
  version_value: "75"
  envelope_key: QueryResponse
  page_size: 1000
```

### `x-tenant-env-var`

Declares the env-var name that resolves the implicit `{tenant}` path
placeholder for multi-tenant SaaS APIs whose every path is
`/tenant/{tenant}/<resource>`. Without this annotation, the generator
classifies tenant-templated paths as parent-context-dependent and emits an
empty `defaultSyncResources` / `syncResourcePath` map; sync silently no-ops
and every downstream offline command ships broken.

Parsed fields: `APISpec.EndpointTemplateVars` (`tenant` added),
`APISpec.EndpointTemplateEnvOverrides["tenant"]` (env-var name), and
`APISpec.GlobalPathTemplateVars` when `{tenant}` is present on at least
80% of endpoints and can safely map to a root persistent flag.

Rules:
- Optional. Specs without `x-tenant-env-var` keep single-tenant behavior;
  no `{tenant}`-aware emission, no spurious env reads.
- Declared under `info` only (path-positional templates are spec-wide).
- Value must be a non-empty string after `TrimSpace`. Whitespace-only
  values are treated as absent.
- The placeholder name is `tenant`. Specs that use a different
  placeholder (`{workspace}`, `{org}`) should set
  `EndpointTemplateVars` + `EndpointTemplateEnvOverrides` directly in
  internal YAML until this extension generalizes.

Effect on generated output (when set):
- The profiler treats `/.../{tenant}/...` paths as standalone-listable, so
  the resource becomes a flat `SyncableResource` rather than a
  `DependentSyncResource`.
- The emitted `config.go` reads the override env-var name (e.g.
  `ST_TENANT_ID`) into `Config.TemplateVars["tenant"]` at `Load()` time.
- The emitted `url.go` `buildURL` substitutes `{tenant}` from
  `Config.TemplateVars` at request time and names the override env var in
  the actionable error when the value is missing.
- When `{tenant}` appears on at least 80% of endpoints and its public
  flag name does not collide with existing root flags, the emitted root
  command exposes `--tenant` as an optional override for the same
  `Config.TemplateVars["tenant"]` value. Matching per-command
  `{tenant}` positionals are removed; sparse path params remain
  per-command inputs.
- Typed MCP endpoint tools for tenant-scoped paths expose optional
  `tenant` input that overrides the env/config value for that one call.
- The emitted `sync.go` filters `{tenant}` out of the unresolved-key
  warning so per-tenant paths don't get skipped as "requires parent
  context".

Example:

```yaml
info:
  title: ServiceTitan CRM
  version: 1.0.0
  x-tenant-env-var: ST_TENANT_ID
```

### `x-path-template-env-vars`

Generic, map-shaped successor to `x-tenant-env-var`. Each entry binds a
path placeholder to an object with two optional fields. The `env` field
registers a runtime env-var override for the placeholder, flowing into
the same `EndpointTemplateVars` / `EndpointTemplateEnvOverrides` bucket
that `x-tenant-env-var` populates — suitable for BaseURL placeholders
such as Atlassian's `{workspace}` or GitHub's `{org}`. The `default`
field bakes a literal into operation paths at generation time and drops
the matching path parameter, suitable for canonical always-valid values
such as Gmail's `userId='me'` for the authenticated user. When both are
set on the same entry, `default` wins and `env` is ignored — the
placeholder is fully resolved before runtime substitution sees it.

Parsed fields: `APISpec.EndpointTemplateVars`,
`APISpec.EndpointTemplateEnvOverrides`,
`APISpec.EndpointPathParamDefaults`, and
`APISpec.GlobalPathTemplateVars` for env-backed placeholders that meet
the same 80% common-path promotion rule.

Rules:
- Optional. Specs without this extension keep prior behavior; the new
  field stays empty and no generated output changes.
- Declared under `info` only (path-positional templates are spec-wide).
- Coexists with `x-tenant-env-var`; both feed the same template-vars
  bucket. The `tenant` placeholder may be set by either extension.
- `env` and `default` values must be non-empty after `TrimSpace`.
  Whitespace-only values are treated as absent on the entry.
- Entries with neither `env` nor `default` set are skipped silently.
- Env-backed placeholders that appear in at least 80% of endpoint paths
  are promoted to optional root persistent flags (for example
  `{workspace}` -> `--workspace`) when the derived flag and Go field
  names do not collide with existing root command flags. Matching
  per-command path positionals are removed, while same-named
  non-path positionals and sparse path params remain command inputs.
- Typed MCP endpoint tools for promoted env-backed path placeholders
  expose optional per-call inputs that override env/config values before
  URL substitution.

Effect on generated output (when set):
- `env`-set entries behave exactly like `x-tenant-env-var` for the
  declared placeholder: the emitted `config.go`, `url.go`, and `sync.go`
  resolve the placeholder against the named env var at runtime, and the
  profiler treats `/.../{placeholder}/...` paths as standalone-listable.
- `default`-set entries are baked in at parse time: every operation
  path under `Resources` has `{placeholder}` replaced with the literal,
  and the matching path parameter is dropped from each endpoint's
  `Params`. The printed CLI exposes neither a placeholder nor a flag
  for the resolved parameter.

Examples:

Runtime env-var override for a BaseURL placeholder, parallel to
`x-tenant-env-var`:

```yaml
info:
  title: Atlassian API
  version: 1.0.0
  x-path-template-env-vars:
    workspace:
      env: ATLASSIAN_WORKSPACE
```

Build-time literal substitution that bakes a canonical value into every
operation path and drops the matching path parameter:

```yaml
info:
  title: Gmail Users API
  version: 1.0.0
  x-path-template-env-vars:
    userId:
      default: me
```

## Security Scheme Extensions

Security scheme extensions are read from
`components.securitySchemes.<scheme-name>`. They can declare composed cookie
auth or override install/config metadata when the API spec's service identity
differs from the product identity exposed by the printed CLI.

When `components.securitySchemes` is absent, the parser may infer simple
bearer auth from clear API-wide prose such as `Authorization: Bearer`,
`personal access token`, `fine-grained PAT`, `app installation token`, or
`OAuth app token`. An explicitly empty block disables that prose fallback:

```yaml
components:
  securitySchemes: {}
```

### `x-auth-type`

Marks an API key scheme as composed auth.

Parsed field: `APISpec.Auth.Type`

Rules:
- Optional.
- Must be the exact string `composed` to take effect.
- Only read for OpenAPI `apiKey` security schemes.
- Any other value leaves the normal API key mapping in place.

### `x-auth-format`

Template used to assemble the composed auth header or cookie value.

Parsed field: `APISpec.Auth.Format`

Rules:
- Optional.
- Only read when `x-auth-type: composed`.
- Must be a string.

### `x-prefix`

Declares a literal token prefix for header API key schemes.

Parsed field: `APISpec.Auth.Format`

Rules:
- Optional.
- Only read for OpenAPI `apiKey` security schemes with `in: header`.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- When present, the parser stores `"<prefix> {token}"` in `Auth.Format`.
- Ignored for query API keys and non-API-key auth schemes.

Example:

```yaml
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: Authorization
      x-prefix: Klaviyo-API-Key
```

### `x-auth-env-vars`

Overrides the generated credential environment variable names.

Parsed field: `APISpec.Auth.EnvVars`

Rules:
- Optional.
- Must be a list of strings. A single string is also accepted for convenience.
- Leading and trailing whitespace is trimmed from each item.
- Empty and non-string list items are ignored.
- When at least one non-empty item is present, the list replaces the parser's
  generated env var names.
- On apiKey/header schemes that appear beside the selected auth scheme in the
  same AND security requirement, the first non-empty item supplies the
  generated per-call env var for that sibling header. Use `x-auth-vars` for
  richer metadata or when more than one credential variable belongs to the
  same scheme.
- If a sibling apiKey/header scheme omits `x-auth-env-vars` and `x-auth-vars`,
  the parser derives a required per-call env var from the API slug and header
  name, for example `DISPATCH_ST_APP_KEY` for `ST-App-Key`.

Catalog-driven equivalent: when a catalog entry declares `auth_env_vars`, the
generator layers the canonical names on top of the parser-derived default at
runtime without editing the upstream spec. The catalog list takes precedence,
the parser default trails as a backwards-compat fallback, and the rebuilt env
var list is emitted as an OR-case (any one satisfies auth). The catalog field
is ignored for HTTP Basic auth (credential-pair shape); declare basic-auth
env var pairs via `x-auth-env-vars` on the security scheme instead. See
[`docs/CATALOG.md`](CATALOG.md#auth_env_vars).

### `x-auth-vars`

Overrides the generated credential environment variable metadata.

Parsed field: `APISpec.Auth.EnvVarSpecs`

Rules:
- Optional.
- Must be a list of objects.
- Each object must include `name`, `kind`, `required`, and `sensitive`.
- `name` must be a non-empty string.
- `kind` must be one of `per_call`, `auth_flow_input`, or `harvested`.
- `required` and `sensitive` must be booleans.
- `description` is optional and must be a string when present.
- Group IDs and legacy aliases are not parsed. Express OR relationships in
  `description` text and by marking each alternative `required: false`.
- Use either `x-auth-env-vars` for legacy name-only overrides or `x-auth-vars`
  for rich metadata. If both are present, `x-auth-vars` wins.
- Malformed values are ignored with a warning, and the parser falls back to the
  generated auth env-var defaults.

Example:

```yaml
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: Authorization
      x-auth-vars:
        - name: TODOIST_API_KEY
          kind: per_call
          required: true
          sensitive: true
          description: Todoist API key.
```

### `x-speakeasy-example`

Uses a Speakeasy security-scheme example as the credential environment variable
name when it is shaped like a shell env var.

Parsed field: `APISpec.Auth.EnvVars`

Rules:
- Optional.
- Must be a string shaped like an uppercase environment variable name, for
  example `DUB_API_KEY`.
- Ignored when `x-auth-env-vars` is present.
- Ignored when the selected auth config has multiple env vars.
- Ignored when the value looks like a token value instead of an env var name.

### `x-auth-optional`

Marks the credential as optional for install/config surfaces.

Parsed field: `APISpec.Auth.Optional`

Rules:
- Optional.
- Must be a boolean.
- `true` makes MCPB `user_config.required` false even for auth types that
  normally require credentials.

### `x-auth-key-url`

Declares the page where users can get a credential.

Parsed field: `APISpec.Auth.KeyURL`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- The parser does not validate the URL shape.

When the extension is absent and the spec has any auth, the parser falls back
through the following sources in order and uses the first plausible HTTPS URL:

1. The selected security scheme's `description` (extracted via regex).
2. `info.description`, but only when the surrounding text mentions
   credential-related cues (`token`, `api key`, `credential`, `register`,
   `sign up`, etc.) so an unrelated URL doesn't get picked.

`externalDocs.url` and `info.contact.url` are intentionally **not** fallbacks
for `KeyURL`. Those almost always point at the API's docs landing page or the
company homepage, neither of which is where users actually create a token.
When `KeyURL` ends up empty, the printed CLI uses `WebsiteURL` (already
populated from `externalDocs.url`, `info.contact.url`, and `x-website`) under
a separate `See API docs: <URL>` line — honest framing for those URLs.

Catalog YAML's `auth_key_url:` (see [`CATALOG.md`](CATALOG.md)) overrides the
inference. The result drives the printed CLI's `Get a key at: <URL>` output in
auth prompts and `doctor`.

### `x-auth-instructions`

Free-form one-line guidance shown alongside `x-auth-key-url`, e.g. "Settings →
Personal access tokens → Generate new". The printed CLI surfaces this under
the URL in auth prompts, `doctor`, and the `auth setup` command.

Parsed field: `APISpec.Auth.Instructions`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- Use this when `x-auth-key-url` lands on a docs page rather than the keys UI;
  the URL says where to start, the instruction says what to do once there.

Catalog YAML's `auth_instructions:` overrides any spec-supplied value.

### `x-auth-title`

Overrides the title shown for the credential field in install/config surfaces.

Parsed field: `APISpec.Auth.Title`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- Used when the selected auth scheme has a single env var. Multiple env vars
  keep env-var-name titles to avoid duplicate field labels.

### `x-auth-description`

Overrides the full description shown for the credential field in install/config
surfaces.

Parsed field: `APISpec.Auth.Description`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- Used as the complete description when the selected auth scheme has a single
  env var. When omitted, the generator builds a description from env var name,
  display name, optionality, and `x-auth-key-url`.

Example:

```yaml
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
      x-auth-env-vars:
        - FLIGHTAWARE_API_KEY
      x-auth-optional: true
      x-auth-key-url: https://flightaware.com/commercial/aeroapi/
      x-auth-title: FlightAware AeroAPI Key
      x-auth-description: Optional FlightAware AeroAPI credential for enriched flight data.
```

### `x-auth-subtype`

Refines `Auth.Type` for runtime flows that need a different credential-capture
path than the base type implies. Today the only recognized value is
`auth0_spa_in_memory`: a bearer-token spec whose access token is held by the
Auth0 SPA SDK with `cacheLocation: memory`. Cookie/localStorage extractors have
no path to such a token (it lives in JS heap only), so the generator emits a
`--auth0-spa` flag on `auth login --chrome` that drives a Chrome DevTools
Protocol outbound-Authorization interceptor instead.

Parsed field: `APISpec.Auth.Subtype`

Rules:

- Optional.
- Must be a string.
- Recognized values: `auth0_spa_in_memory`. Other values are silently dropped
  by the parser; the in-spec value never round-trips unless it matches a known
  subtype.
- Spec-level validation rejects `auth.subtype: auth0_spa_in_memory` paired with
  any non-empty `auth.type` other than `bearer_token`. Auth0 SPA tokens are
  always Authorization-bearer values; combining the subtype with `api_key` or
  `cookie` would silently emit a CDP path against a credential that isn't a
  JWT.
- Sniff-time detection (`internal/browsersniff/auth0_spa.go`) sets the subtype
  automatically when an `/oauth/token` response carries `access_token` in the
  JSON body without a JWT-shaped Set-Cookie on the same response. Authors
  rarely need to set it by hand.

Example:

```yaml
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      x-auth-subtype: auth0_spa_in_memory
```

### `x-auth-cookie-domain`

Domain used when extracting named cookies for composed auth.

Parsed field: `APISpec.Auth.CookieDomain`

Rules:
- Optional.
- Only read when `x-auth-type: composed`.
- Must be a string.

### `x-auth-cookies`

Cookie names required to fill the composed auth format.

Parsed field: `APISpec.Auth.Cookies`

Rules:
- Optional.
- Only read when `x-auth-type: composed`.
- Must be a list.
- List items must be strings; non-string items are skipped.

Example:

```yaml
components:
  securitySchemes:
    browserSession:
      type: apiKey
      in: header
      name: Authorization
      x-auth-type: composed
      x-auth-format: "Session {session_id}:{csrf_token}"
      x-auth-cookie-domain: app.example.com
      x-auth-cookies:
        - session_id
        - csrf_token
```

### `x-auth-companion`

Declares the deterministic hints the generated CLI needs to hand off to
`press-auth login` non-interactively. With these set, `auth login --chrome
--auto-login` shells out to `press-auth login <domain> --login-url ...
--jwt-carrier-cookie ...` and the user never has to remember those values.

Parsed fields:

- `login_url` -> `APISpec.Auth.LoginURL`
- `login_complete_selector` -> `APISpec.Auth.LoginCompleteSelector`
- `jwt_carrier_cookie` -> `APISpec.Auth.JWTCarrierCookie`

Placement:

- Allowed on `components.securitySchemes.<name>` and on `info`. Scheme-level
  fields win over info-level when both are set. Info-level is intended for
  specs that declare composed auth without a single named security scheme.

Rules:

- All three sub-fields are optional. When absent, the generated CLI prints
  a hint instructing the user to invoke `press-auth login` manually.
- `login_url` must parse as a URL and use `https://` (or `http://localhost`
  / `http://127.0.0.1`). Plain `http://` against other hosts would leak the
  captured cookies to a network sniffer; the parser rejects it.
- `login_complete_selector` is opaque — pass through verbatim as a CSS
  selector. The parser does not validate it beyond non-emptiness.
- `jwt_carrier_cookie` should match one of the names listed in `cookies`.
  A mismatch surfaces as a stderr warning at parse time (typo surfacing)
  rather than a hard error.
- Only these three public hints are embedded into the generated CLI's
  source. Cookie values, JWT tokens, and other user-secret material never
  appear in generated constants — press-auth captures and stores those
  itself.

Internal YAML equivalent (under the `auth:` block):

```yaml
auth:
  type: composed
  cookie_domain: example.com
  cookies:
    - session_id
    - guestsession
  login_url: https://www.example.com/account/login
  login_complete_selector: "a[href*=signout]"
  jwt_carrier_cookie: guestsession
```

OpenAPI security-scheme placement:

```yaml
components:
  securitySchemes:
    browserSession:
      type: apiKey
      in: cookie
      name: guestsession
      x-auth-companion:
        login_url: https://www.example.com/account/login
        login_complete_selector: "a[href*=signout]"
        jwt_carrier_cookie: guestsession
```

OpenAPI info-level placement (for specs without a named scheme):

```yaml
info:
  title: Example
  x-auth-companion:
    login_url: https://www.example.com/account/login
    login_complete_selector: "a[href*=signout]"
    jwt_carrier_cookie: guestsession
```

### `x-oauth-device-flow`

Declares OAuth 2.0 device authorization grant metadata for CLI-first OAuth
flows. OpenAPI 3.0 does not have a native `deviceCode` flow, so the Printing
Press reads this extension from an OAuth2 security scheme and emits a generated
`auth login --device-code` command plus refresh-token handling.

Parsed fields: `APISpec.Auth.OAuth2Grant=device_code`,
`APISpec.Auth.DeviceAuthorizationURL`, `APISpec.Auth.TokenURL`,
`APISpec.Auth.Scopes`, `APISpec.Auth.DefaultClientID`.

Rules:

- Optional. When present, the parser treats the security scheme as a bearer
  OAuth flow backed by stored access tokens.
- Must be an object.
- `deviceAuthorizationUrl` (or `device_authorization_url`) and `tokenUrl` (or
  `token_url`) are required by `APISpec.Validate()` when
  `oauth2_grant: device_code`.
- `scopes` may be a string or list of strings. Lists are sorted for stable
  generation.
- `defaultClientId` (or `default_client_id`) is optional. When absent, the
  generated CLI prompts for `--client-id` or the inferred `<API>_CLIENT_ID`
  environment variable.

Example:

```yaml
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      x-oauth-device-flow:
        deviceAuthorizationUrl: https://login.example.com/common/oauth2/v2.0/devicecode
        tokenUrl: https://login.example.com/common/oauth2/v2.0/token
        defaultClientId: public-client-id
        scopes:
          - Calendars.Read
          - Mail.Read
```

### `x-oauth-refresh-token-mechanism`

Declares how the authorization endpoint should be asked to issue a refresh
token. Providers diverge: Google reads `access_type=offline` as a query
parameter, while WHOOP, X/Twitter, and others read a magic scope value
(`offline`, `offline.access`, `offline_access`) instead. The generator emits
neither by default because a Google-shaped silent default silently breaks
other providers (broken refresh path is invisible until access-token TTL
expires).

Parsed field: `APISpec.Auth.RefreshTokenMechanism`

Rules:

- Optional. Only consumed by the authorization_code grant template; ignored
  for other grants and non-OAuth2 auth.
- Must be a string. Leading and trailing whitespace on the whole value is
  trimmed.
- Two exact-match prefixes are accepted:
  - `scope:<value>` appends `<value>` to the scope list. No query param is
    added.
  - `query:<key>=<value>` sets the query parameter exactly once. No scope
    change.
- Malformed values (empty key, empty value, missing `=` for `query`, unknown
  prefix, uppercase prefix) are ignored and produce no emission.
- For `query:<key>=<value>`, the reserved authorization-URL parameter names
  `client_id`, `redirect_uri`, `response_type`, `state`, and `scope` are
  rejected. Permitting them would let a spec author silently overwrite the
  generator's CSRF state token or core OAuth params.
- Note: the single-mechanism shape cannot express Google's two-param recipe
  (`access_type=offline` + `prompt=consent`). The first param is sufficient
  for refresh-token issuance on initial consent; the second forces re-consent
  on subsequent logins to keep the refresh-token contract alive. Specs that
  need both should declare one via this extension and add the other through a
  future multi-mechanism syntax (out of scope here).

Example:

```yaml
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      x-oauth-refresh-token-mechanism: scope:offline
      flows:
        authorizationCode:
          authorizationUrl: https://api.example.com/oauth/authorize
          tokenUrl: https://api.example.com/oauth/token
          scopes:
            read: Read access
```

### `x-url-name` and `x-param-url-names`

Overrides the URL query key for a parameter without changing the public
CLI/MCP input name. Use this for APIs whose documented flag name or shared
OpenAPI component name differs from the exact wire query key a specific
endpoint accepts.

Parsed field: `Param.URLName`

Rules:

- `x-url-name` is allowed on an OpenAPI Parameter Object and applies wherever
  that parameter object is used.
- `x-param-url-names` is allowed on a Path Item Object or Operation Object. It
  is an object mapping parameter names to URL query keys. Operation entries
  override path-item entries.
- The parameter's `name` remains the public input identity used for generated
  Go identifiers, CLI flags, MCP public names, and manifest public names.
- The override only changes generated URL query emission, MCP `WireName`, and
  manifest `wire_name`.
- Empty names, empty URL keys, and non-string override values are ignored with
  warnings.

Example:

```yaml
paths:
  /opportunities/search:
    get:
      x-param-url-names:
        locationId: location_id
      parameters:
        - $ref: "#/components/parameters/LocationId"

  /opportunities/pipelines:
    get:
      parameters:
        - $ref: "#/components/parameters/LocationId"

components:
  parameters:
    LocationId:
      name: locationId
      in: query
      schema:
        type: string
```

In this example both endpoints keep the same public `locationId` input, but
only `/opportunities/search` sends `?location_id=` on the wire.

### `x-pp-dispatch-param`

Marks a query parameter as a fixed dispatch discriminator whose `default` value
selects the upstream route rather than tuning the request. Generated runnable
examples keep that default instead of substituting a synthetic dogfood value.

Parsed field: `Param.DispatchParam`

Use this for shared-path APIs where a query parameter such as `type` or
`action` selects the report or operation. Do not use it for ordinary filters,
limits, page sizes, or other tunable inputs.

Example:

```yaml
paths:
  /:
    get:
      operationId: getDomainRank
      parameters:
        - name: report
          in: query
          x-pp-dispatch-param: true
          schema:
            type: string
            default: domain_rank
```

## Path Item Extensions

Path item extensions are read from a path object, beside its HTTP operations.
They apply to every operation under that path because sync identity and critical
resource status are resource-scoped, and operation-level data-source strategy
can override the path default.

### `x-resource-id`

Declares the response field that should be used as the primary key when sync
stores resources locally.

Parsed field: `Endpoint.IDField`

Rules:
- Optional.
- Must be a string.
- Leading and trailing whitespace is trimmed.
- Non-string values emit a warning and are ignored.
- An empty or missing value falls through to the parser's response-schema
  fallback chain: `id`, then `name`, then the first required scalar field.
- Applies to every operation on the path item.

Example:

```yaml
paths:
  /widgets:
    x-resource-id: widget_uid
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
```

### `x-critical`

Marks a syncable resource as essential. Generated sync commands fail the run
when a critical resource fails, while non-critical resource failures can be
reported as warnings unless `--strict` is used.

Parsed field: `Endpoint.Critical`

Rules:
- Optional.
- Defaults to `false`.
- Accepts native booleans.
- Also accepts the strings `"true"` and `"1"` as true, case-insensitive after
  trimming.
- The strings `"false"`, `"0"`, and `""` are false.
- Other string values emit a warning and are false.
- Non-boolean, non-string values emit a warning and are false.
- Applies to every operation on the path item.

Example:

```yaml
paths:
  /accounts:
    x-critical: true
    get:
      operationId: listAccounts
      responses:
        "200":
          description: OK
```

### `x-tier`

Selects a tier declared by `x-tier-routing` for a path item or one operation.

Parsed field: `Endpoint.Tier`

Rules:
- Optional.
- Must be a string.
- Operation-level `x-tier` overrides path-item-level `x-tier`.
- The value must name a tier in `x-tier-routing.tiers`.
- `security: []` / `security: [{}]` must not be combined with an auth-bearing
  tier. Use a `none` tier for anonymous endpoints.

Example:

```yaml
paths:
  /public/search:
    x-tier: free
    get:
      responses:
        "200": {description: ok}
  /premium/search:
    get:
      x-tier: paid
      responses:
        "200": {description: ok}
```

### `x-data-source-strategy`

Declares how a generated read command should honor the global
`--data-source auto|local|live` flag.

Parsed field: `Endpoint.DataSourceStrategy`

Rules:
- Optional.
- May be declared on a path item or operation.
- Operation-level values override path-item-level values.
- Must be one of `auto`, `local`, or `live`.
- `auto` keeps the normal live-with-local-fallback behavior for store-backed
  reads.
- `local` makes the command use local synced data for `auto` and `local`, and
  reject `--data-source live` with a clear no-live-equivalent error.
- `live` makes the command use the remote API for `auto` and `live`, and reject
  `--data-source local` with a clear no-local-data-source error.

Example:

```yaml
paths:
  /reports/snapshot:
    get:
      x-data-source-strategy: local
      responses:
        "200": {description: ok}
```

### `x-requires-role`

Requires the authenticated account to have one of the declared `x-roles` before
the generated command calls the API.

Parsed field: `Endpoint.RequiresRole`

Rules:
- Optional.
- Must be on an operation, not the root, `info`, or path item.
- Must be a string naming a role declared by `x-roles`.
- The generator emits the guard framework and endpoint call site. How a printed
  CLI discovers the authenticated account's role remains API-specific.

Example:

```yaml
x-roles: [parent, student, teacher, admin]
paths:
  /users:
    get:
      operationId: listUsers
      x-requires-role: admin
      responses:
        "200": {description: ok}
```

### `x-pp-resource`

Overrides the resource bucket for one OpenAPI operation. Use it when the path
parser would derive a reserved Printing Press template name such as `search`,
or when the upstream path shape does not expose the intended resource name.

Parsed field: resource map key in `APISpec.Resources`

Rules:
- Optional.
- Must be a string.
- The value is sanitized to the same resource-name form the parser uses for
  path-derived names.
- Non-string values emit a warning and are ignored.
- Applies only to the operation where it appears.

Example:

```yaml
paths:
  /search:
    post:
      operationId: searchNotes
      x-pp-resource: notes_search
      responses:
        "200": {description: ok}
```

### `x-pp-safe-probe`

Marks a mutation endpoint as explicitly safe for the Phase 1.9 reachability gate
to call once as an optional second probe after the low-risk GET/body capture.
This extension is consumed by Printing Press skill guidance rather than the Go
OpenAPI parser; it documents author intent for agents reviewing a resolved spec.

Parsed field: none; consumed by skill guidance only

Rules:
- Optional.
- Must be on an operation, not the root, `info`, or path item.
- Accepts native boolean `true` only.
- Use only for idempotent or otherwise harmless operations for the real account
  being used.
- Absence or any value other than native boolean `true` means mutation probing
  is not allowed; agents must stop after the GET/body reachability capture.

Example:

```yaml
paths:
  /webhooks/test:
    post:
      x-pp-safe-probe: true
      responses:
        "200": {description: ok}
```

### `x-happy-args`

Declares live-dogfood happy-path fixture arguments for one operation. Use it
when generic synthesized inputs cannot satisfy the endpoint contract, such as a
search endpoint that requires `q` or a lookup endpoint that requires one of
several conditional query flags.

Parsed field: `Endpoint.HappyArgs`

Rules:
- Optional.
- Must be on an operation, not the root, `info`, or path item.
- Must be a string in the runtime annotation format consumed by
  `pp:happy-args`.
- Tokens are semicolon-separated. `<label>=value` overlays synthesized
  positional args, and `--flag=value` overlays or adds flag/value pairs.
- Empty or whitespace-only values behave the same as absence.

Example:

```yaml
paths:
  /referents:
    get:
      operationId: listReferents
      x-happy-args: "--song-id=378195"
      responses:
        "200": {description: ok}
```

### `x-pp-sync-walker`

Declares a hierarchical-walk dependency for a child endpoint. Synthesizes (or
augments) a dependent-resource entry so the generator's existing
parent-child sync machinery handles the fan-out — fetch the parent, extract
the named field from each parent record, substitute it into the child path,
fetch each child.

Use this when the auto-detected parent-child link in the profiler would miss
your endpoint or pick the wrong parent. Common cases:

- The child path's placeholder name does not match a parent resource (e.g.
  `/games/{game_key}/leagues` — `game_key` does not stem to "games" via the
  default `_id`/`_key` stripping).
- The parent placeholder lives in a matrix or query parameter rather than the
  path, so the path has no `{placeholder}` for auto-detection to read.
- The child path uses a parent field that is not the parent's primary key
  (e.g. Yahoo Fantasy's `game_key`, Reddit's `subreddit` name).

Parsed field: `Endpoint.Walker` (a `*spec.WalkerConfig`)

Rules:
- Optional.
- Operation-level only. (No path-item-level form today.)
- `parent` (string, required): the resource name to iterate. The parent must
  itself be a syncable resource (i.e., have a flat-list endpoint). Walkers
  pointing at non-syncable parents emit a `warning:` to stderr at generate
  time and are dropped.
- `key_field` (string, optional): the field to extract from each parent
  record for substitution into the child path. Defaults to the parent's
  primary key. Set this when the child path needs a non-PK field.
- `key_param` (string, optional): the placeholder name in the child path
  that receives the extracted value. Defaults to the first (and only)
  `{placeholder}` in the child path when there is exactly one. **Required
  explicitly when the child path has 0 or 2+ placeholders** — the
  single-placeholder default would otherwise pick the wrong slot (or no
  slot at all). The generator warns and drops the walker when it's ambiguous
  and `key_param` is missing.
- Walker-emitted dependents flow through the same `syncDependentResource`
  machinery as auto-detected ones, so concurrency/retry/cursor/Upsert
  behavior is identical.

Internal YAML emits this as `walker:` on the endpoint with the same
sub-field names (`parent`, `key_field`, `key_param`). Both surfaces parse
to the same `WalkerConfig` struct.

Example:

```yaml
paths:
  /games:
    get:
      summary: List games (parent for the walker below)
      responses:
        "200": {description: ok}
  /games/{game_key}/leagues:
    get:
      summary: List leagues for a game
      x-pp-sync-walker:
        parent: games
        key_field: game_key
        key_param: game_key
      parameters:
        - name: game_key
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
```

### `x-streaming`

Declares that a spec has WebSocket-primary ingest with REST metadata refresh.
Internal YAML uses the same shape as `streaming:` at the top level.

Parsed field: `APISpec.Streaming` (`spec.StreamingConfig`)

Rules:
- Optional. When omitted, REST-only generation is unchanged.
- `transport` must currently be `websocket`.
- `url` must be an absolute `ws://` or `wss://` URL.
- `framing` may be `single_object_per_frame` or `newline_delimited_json`.
  Empty defaults to `single_object_per_frame`.
- `metadata.endpoint` is optional, but required when any metadata sub-field
  is declared.
- `metadata.refresh_cadence` is a Go duration. Empty defaults to `30s`.
- `metadata.statuses` defaults to `[live, pending]`.
- `metadata.primary_key` defaults to `id`.

When present, the generator emits `live ws sync`, `live rest sync`,
`internal/wsclient`, and local SQLite tables for stream frames, stream
metadata, and `<api>_rebase_log` lifecycle events.

Example:

```yaml
x-streaming:
  transport: websocket
  url: "wss://api.example.com/v1/ws"
  subscribe_shape: '{"type":"subscribe","channels":["events"]}'
  framing: newline_delimited_json
  metadata:
    endpoint: "/v1/events"
    refresh_cadence: 30s
    statuses: [live, pending]
    primary_key: event_id
```
