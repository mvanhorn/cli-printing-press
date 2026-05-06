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
| `x-tier-routing` | root or `info` | `APISpec.TierRouting` | No |
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
| `x-auth-cookie-domain` | `components.securitySchemes.<name>` | `APISpec.Auth.CookieDomain` | No |
| `x-auth-cookies` | `components.securitySchemes.<name>` | `APISpec.Auth.Cookies` | No |
| `x-resource-id` | path item | `Endpoint.IDField` | No |
| `x-critical` | path item | `Endpoint.Critical` | No |
| `x-tier` | path item or operation | `Endpoint.Tier` | No |

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
- Incompatible with `client_pattern: proxy-envelope` and with resource-level
  `base_url` overrides when any tier declares its own `base_url`.
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

## Security Scheme Extensions

Security scheme extensions are read from
`components.securitySchemes.<scheme-name>`. They can declare composed cookie
auth or override install/config metadata when the API spec's service identity
differs from the product identity exposed by the printed CLI.

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

## Path Item Extensions

Path item extensions are read from a path object, beside its HTTP operations.
They apply to every operation under that path because sync identity and critical
resource status are resource-scoped.

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
