# Internal YAML Spec Format Reference

Use this format when no OpenAPI spec is available.

## 1. Complete Schema Reference

```yaml
# Root object (APISpec)
name: my-api                     # string (REQUIRED) CLI binary prefix, e.g. "my-api"
description: "My API CLI"       # string shown in command help
version: "0.1.0"                # string baked into generated binary
base_url: "https://api.example.com/v1" # string (REQUIRED) default base API URL
http_transport: standard          # string optional: standard | browser-http | browser-chrome | browser-chrome-h3
health_check_path: "/health"      # string optional doctor reachability path; defaults to /
required_headers:                 # []RequiredHeader optional headers sent on every request
  - name: User-Agent
    value: "Mozilla/5.0 ..."

auth:                             # object (AuthConfig)
  type: api_key                   # string: api_key | oauth2 | bearer_token | none
  header: "Authorization"        # string header name to set
  format: "Bearer {token}"       # string format template for auth header value
  env_vars:                       # []string env vars used for auth material
    - EXAMPLE_API_TOKEN
  scheme: bearerAuth              # string optional OpenAPI security scheme name
  in: header                      # string optional: header | query | cookie

config:                           # object (ConfigSpec)
  format: toml                    # string config format: toml | yaml (other values fall back to json tags)
  path: "~/.config/my-api/config.toml" # string config file path

resources:                        # map[string]Resource (REQUIRED: at least one key)
  users:                          # resource key becomes top-level command: <name>-cli users
    description: "Manage users"  # string resource help text
    base_url: "https://directory.example.com/v1" # string optional override for this resource and inherited sub-resources
    endpoints:                    # map[string]Endpoint (REQUIRED: at least one key)
      list:                       # endpoint key becomes subcommand: users list
        method: GET               # string (REQUIRED) must be one of GET | POST | PUT | DELETE
        path: "/users"           # string (REQUIRED) API path; supports {param} placeholders
        description: "List users" # string endpoint help text
        params:                   # []Param query/path parameters
          - name: limit           # string upstream wire key; request serialization always uses this value
            flag_name: page-size  # string optional public CLI/MCP/docs name; agent-authored from evidence
            aliases: []           # []string optional hidden compatibility flag spellings
            type: int             # string type: string | int | bool | float
            required: false       # bool whether Cobra marks the flag required
            positional: false     # bool true => consumes positional CLI arg and fills {name} in path
            default: 100          # any default value (type should match param type)
            description: "Max results" # string flag description
            fields: []            # []Param nested fields for object-like params
            enum: []              # []string optional enum hints/constraints
            format: ""           # string optional format hint (date-time, email, uri, etc.)
        body:                     # []Param request body fields (primarily for POST/PUT)
          - name: email
            # flag_name and aliases are optional here too; omit unless evidence supports them
            type: string
            required: true
            positional: false
            default: null
            description: "User email"
            fields: []
            enum: []
            format: email
        response:                 # object (ResponseDef)
          type: array             # string response shape: object | array
          item: User              # string type name referenced from `types`
        pagination:               # object (Pagination) optional
          type: cursor            # string pagination style: cursor | offset | page_token
          cursor_field: cursor    # string response field containing next cursor
          has_more_field: data.has_more # string response field indicating more results
        response_path: data       # string optional path to extract list payload from wrapper response

types:                            # map[string]TypeDef named response/body models
  User:                           # type name referenced by response.item
    fields:                       # []TypeField
      - name: user_id             # string field name
        type: string              # string field type (typically string/int/bool/float)
```

## 2. Annotated Example (`testdata/stytch.yaml`)

```yaml
name: stytch # CLI binary prefix => stytch-cli
description: "Stytch authentication API CLI" # Root help text and README summary
version: "0.1.0" # Printed by `stytch-cli version`
base_url: "https://api.stytch.com/v1" # Default base URL endpoint paths are joined against
health_check_path: "/sessions" # Optional doctor reachability path; omit to probe /

auth:
  type: api_key # Uses API key style auth
  header: "Authorization" # Header key set on outgoing requests
  format: "Basic {project_id}:{secret}" # Expected auth value format
  env_vars:
    - STYTCH_PROJECT_ID # Credential source env var #1
    - STYTCH_SECRET # Credential source env var #2

config:
  format: toml # Generated config struct tags use TOML
  path: "~/.config/stytch-cli/config.toml" # Default config file location

resources:
  users: # Creates `stytch-cli users ...`
    description: "Manage Stytch users"
    endpoints:
      list: # Creates `stytch-cli users list`
        method: GET # Generates a query-parameter style command
        path: "/users"
        description: "List all users"
        params:
          - name: limit # Exposed as `--limit`
            type: int
            default: 100 # Default flag value
            description: "Max users to return"
          - name: cursor # Exposed as `--cursor`
            type: string
            description: "Pagination cursor"
        response:
          type: array # Command expects a list-like response
          item: User # Rows map to `types.User`
        pagination:
          type: cursor # Enables cursor pagination helpers
          cursor_field: "cursor" # Field that contains next cursor token
          has_more_field: "results.has_more" # Field used to detect continuation

      get: # Creates `stytch-cli users get <user_id>`
        method: GET
        path: "/users/{user_id}" # Placeholder filled from positional arg
        description: "Get a user by ID"
        params:
          - name: user_id
            type: string
            required: true
            positional: true # Required so CLI arg is mapped into `{user_id}`
            description: "User ID"
        response:
          type: object
          item: User

      create: # Creates `stytch-cli users create --email ...`
        method: POST # Generates body-field flags
        path: "/users"
        description: "Create a new user"
        body:
          - name: email
            type: string
            description: "User email"
          - name: phone_number
            type: string
            description: "User phone number"
        response:
          type: object
          item: User

      delete: # Creates `stytch-cli users delete <user_id>`
        method: DELETE
        path: "/users/{user_id}"
        params:
          - name: user_id
            type: string
            required: true
            positional: true
            description: "User ID"

  sessions: # Creates `stytch-cli sessions ...`
    description: "Manage user sessions"
    endpoints:
      list: # Creates `stytch-cli sessions list --user_id ...`
        method: GET
        path: "/sessions"
        description: "List sessions for a user"
        params:
          - name: user_id
            type: string
            required: true
            description: "User ID"
        response:
          type: array
          item: Session

      revoke: # Creates `stytch-cli sessions revoke --session_id ...`
        method: POST
        path: "/sessions/revoke"
        description: "Revoke a session"
        body:
          - name: session_id
            type: string
            required: true
            description: "Session ID to revoke"

types:
  User: # Referenced by response.item: User
    fields:
      - name: user_id
        type: string
      - name: email
        type: string
      - name: phone_number
        type: string
      - name: status
        type: string
      - name: created_at
        type: string

  Session: # Referenced by response.item: Session
    fields:
      - name: session_id
        type: string
      - name: user_id
        type: string
      - name: started_at
        type: string
      - name: expires_at
        type: string
```

## 3. Public Parameter Names

`name` is the upstream wire key. The generator uses it for query strings, path
substitution, and JSON body keys. Do not change `name` just to make a prettier
CLI.

`flag_name` is the preferred public name shown in generated CLI flags, examples,
typed MCP schemas, and `tools-manifest.json`. Add it only when source evidence
makes the meaning clear. Valid values are lowercase kebab-case.

`aliases` are accepted Cobra flag spellings for compatibility. They bind to the
same backing variable as `flag_name` and are hidden from generated help. Do not
copy raw wire keys into `aliases` unless they are already valid lowercase
kebab-case public flags.

Example:

```yaml
params:
  - name: s
    flag_name: address
    aliases: [s]
    type: string
    required: true
    description: Street address
  - name: c
    flag_name: city
    aliases: [c]
    type: string
    required: true
    description: City, state, zip
```

Here `--address` and `--city` are the generated public names, `--s` and `--c`
remain compatibility aliases, and requests still send upstream keys `s` and `c`.

## 4. Validation Rules

Validation in `spec.Validate()` enforces:

- `name` is required
- root `base_url` is required unless `base_path` is supplied
- at least one `resources` entry is required
- every resource must have at least one endpoint
- every endpoint must have both `method` and `path`
- `resources.<name>.base_url` is allowed for resources that live on another host; sub-resources inherit the parent override unless they set their own `base_url`
- resource `base_url` overrides cannot be combined with `client_pattern: proxy-envelope`, because proxy-envelope clients POST every request to the root `base_url`
- `flag_name` and `aliases` must be lowercase kebab-case, non-empty when present, and collision-free within their command surface

## 5. Common Mistakes

These commonly cause generation/build failures or incorrect CLI behavior:

- Missing required fields (`name`, root `base_url`, resource endpoints, endpoint `method`, endpoint `path`)
- Invalid `method` values (generator templates only handle `GET`, `POST`, `PUT`, `DELETE`)
- Missing `path` on endpoints
- Defining `body` params on `GET` endpoints (allowed in YAML, but ignored by GET command generation)
- Forgetting `positional: true` for params used in `/{path_placeholders}`
- Using parameter types outside supported scalar set: `string`, `int`, `bool`, `float`
- Putting a website/feed host in the root `base_url` just to make one resource work; keep the root API host as the default and use a resource `base_url` override for the outlier surface

## 5. Type Mapping

| YAML Type | Go Type | Cobra Flag | Zero Value |
|-----------|---------|------------|------------|
| `string` | `string` | `StringVar` | `""` |
| `int` | `int` | `IntVar` | `0` |
| `bool` | `bool` | `BoolVar` | `false` |
| `float` | `float64` | `Float64Var` | `0.0` |
