package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseStytch(t *testing.T) {
	s, err := Parse("../../testdata/stytch.yaml")
	require.NoError(t, err)

	assert.Equal(t, "stytch", s.Name)
	assert.Equal(t, "Stytch authentication API CLI", s.Description)
	assert.Equal(t, "0.1.0", s.Version)
	assert.Equal(t, "https://api.stytch.com/v1", s.BaseURL)

	// Auth
	assert.Equal(t, "api_key", s.Auth.Type)
	assert.Equal(t, "Authorization", s.Auth.Header)
	assert.Len(t, s.Auth.EnvVars, 2)

	// Resources
	assert.Len(t, s.Resources, 2)
	users := s.Resources["users"]
	assert.Equal(t, "Manage Stytch users", users.Description)
	assert.Len(t, users.Endpoints, 4) // list, get, create, delete

	// Users list endpoint
	list := users.Endpoints["list"]
	assert.Equal(t, "GET", list.Method)
	assert.Equal(t, "/users", list.Path)
	assert.NotNil(t, list.Pagination)
	assert.Equal(t, "cursor", list.Pagination.Type)

	// Users create endpoint
	create := users.Endpoints["create"]
	assert.Equal(t, "POST", create.Method)
	assert.Len(t, create.Body, 2)

	// Sessions
	sessions := s.Resources["sessions"]
	assert.Len(t, sessions.Endpoints, 2)

	// Types
	assert.Len(t, s.Types, 2)
	assert.Len(t, s.Types["User"].Fields, 5)
	assert.Len(t, s.Types["Session"].Fields, 4)
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    APISpec
		wantErr string
	}{
		{
			name:    "empty name",
			spec:    APISpec{BaseURL: "http://x", Resources: map[string]Resource{"a": {Endpoints: map[string]Endpoint{"b": {Method: "GET", Path: "/"}}}}},
			wantErr: "name is required",
		},
		{
			name:    "empty base_url",
			spec:    APISpec{Name: "x", Resources: map[string]Resource{"a": {Endpoints: map[string]Endpoint{"b": {Method: "GET", Path: "/"}}}}},
			wantErr: "base_url is required",
		},
		{
			name:    "no resources",
			spec:    APISpec{Name: "x", BaseURL: "http://x"},
			wantErr: "at least one resource is required",
		},
		{
			name:    "endpoint missing method",
			spec:    APISpec{Name: "x", BaseURL: "http://x", Resources: map[string]Resource{"a": {Endpoints: map[string]Endpoint{"b": {Path: "/"}}}}},
			wantErr: "method is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestVersionPassedThrough(t *testing.T) {
	base := func(v string) APISpec {
		return APISpec{
			Name:      "x",
			Version:   v,
			BaseURL:   "http://x",
			Resources: map[string]Resource{"a": {Endpoints: map[string]Endpoint{"b": {Method: "GET", Path: "/"}}}},
		}
	}

	// Version is the API version (provenance only). It passes through as-is.
	// The CLI version is hardcoded to "1.0.0" in the generated root.go template.
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},             // empty stays empty
		{"1.0.0", "1.0.0"},   // semver preserved
		{"4", "4"},           // non-semver API versions preserved
		{"4.4", "4.4"},       // major.minor preserved
		{"4.17.1", "4.17.1"}, // full semver preserved
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			s := base(tt.input)
			err := s.Validate()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s.Version)
		})
	}
}

func TestNewFields(t *testing.T) {
	s := APISpec{
		Name:    "x",
		BaseURL: "http://x",
		Auth: AuthConfig{
			Type:    "api_key",
			Scheme:  "ApiKeyAuth",
			In:      "header",
			EnvVars: []string{"API_KEY"},
		},
		Resources: map[string]Resource{
			"users": {
				Endpoints: map[string]Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/users",
						ResponsePath: "results.items",
						Params: []Param{
							{
								Name:   "status",
								Type:   "string",
								Enum:   []string{"active", "inactive"},
								Format: "email",
							},
						},
					},
				},
			},
		},
	}

	require.NoError(t, s.Validate())

	endpoint := s.Resources["users"].Endpoints["list"]
	assert.Equal(t, "results.items", endpoint.ResponsePath)
	assert.Equal(t, []string{"active", "inactive"}, endpoint.Params[0].Enum)
	assert.Equal(t, "email", endpoint.Params[0].Format)
	assert.Equal(t, "ApiKeyAuth", s.Auth.Scheme)
	assert.Equal(t, "header", s.Auth.In)
}

func TestEndpointMeta(t *testing.T) {
	t.Parallel()

	t.Run("parse YAML with meta populated", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
        meta:
          source_tier: official-sdk
          source_count: "2"
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		require.NoError(t, s.Validate())
		ep := s.Resources["users"].Endpoints["list"]
		assert.Equal(t, "official-sdk", ep.Meta["source_tier"])
		assert.Equal(t, "2", ep.Meta["source_count"])
	})

	t.Run("parse YAML without meta field", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		ep := s.Resources["users"].Endpoints["list"]
		assert.Nil(t, ep.Meta)
	})

	t.Run("marshal with meta set includes meta section", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{
			Method: "GET",
			Path:   "/users",
			Meta:   map[string]string{"source_tier": "code-search"},
		}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.Contains(t, string(data), "meta:")
		assert.Contains(t, string(data), "source_tier: code-search")
	})

	t.Run("marshal with nil meta omits meta section", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{
			Method: "GET",
			Path:   "/users",
		}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "meta")
	})
}

func TestEndpointNoAuth(t *testing.T) {
	t.Parallel()

	t.Run("parse YAML with no_auth set", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  stores:
    description: Stores
    endpoints:
      list:
        method: GET
        path: /stores
        no_auth: true
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		require.NoError(t, s.Validate())
		ep := s.Resources["stores"].Endpoints["list"]
		assert.True(t, ep.NoAuth)
	})

	t.Run("parse YAML without no_auth field", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		ep := s.Resources["users"].Endpoints["list"]
		assert.False(t, ep.NoAuth)
	})

	t.Run("marshal with no_auth true includes field", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{Method: "GET", Path: "/stores", NoAuth: true}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.Contains(t, string(data), "no_auth: true")
	})

	t.Run("marshal with no_auth false omits field", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{Method: "GET", Path: "/users"}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "no_auth")
	})
}

func TestEndpointIDFieldAndCritical(t *testing.T) {
	t.Parallel()

	t.Run("parse YAML with id_field and critical set", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  tickers:
    description: Tickers
    endpoints:
      list:
        method: GET
        path: /tickers
        id_field: ticker
        critical: true
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		ep := s.Resources["tickers"].Endpoints["list"]
		assert.Equal(t, "ticker", ep.IDField)
		assert.True(t, ep.Critical)
	})

	t.Run("parse YAML without id_field/critical defaults to zero values", func(t *testing.T) {
		t.Parallel()
		input := `
name: test
base_url: http://x
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
`
		var s APISpec
		require.NoError(t, yaml.Unmarshal([]byte(input), &s))
		ep := s.Resources["users"].Endpoints["list"]
		assert.Empty(t, ep.IDField)
		assert.False(t, ep.Critical)
	})

	t.Run("marshal omits id_field/critical when unset", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{Method: "GET", Path: "/users"}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "id_field")
		assert.NotContains(t, string(data), "critical")
	})

	t.Run("marshal includes id_field/critical when set", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{Method: "GET", Path: "/tickers", IDField: "ticker", Critical: true}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.Contains(t, string(data), "id_field: ticker")
		assert.Contains(t, string(data), "critical: true")
	})
}

func TestCountMCPTools(t *testing.T) {
	t.Parallel()
	s := APISpec{
		Name:    "test",
		BaseURL: "http://x",
		Resources: map[string]Resource{
			"stores": {
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/stores", NoAuth: true},
					"get":  {Method: "GET", Path: "/stores/{id}", NoAuth: true},
				},
				SubResources: map[string]Resource{
					"menus": {
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "/stores/{id}/menus", NoAuth: true},
						},
					},
				},
			},
			"orders": {
				Endpoints: map[string]Endpoint{
					"list":   {Method: "GET", Path: "/orders"},
					"create": {Method: "POST", Path: "/orders"},
				},
			},
		},
	}

	total, public := s.CountMCPTools()
	assert.Equal(t, 5, total, "should count all endpoints including sub-resources")
	assert.Equal(t, 3, public, "should count only NoAuth endpoints")
}

// --- Unit 5: YAML Format Safety Net Tests ---

func TestParseBytesYAMLVariations(t *testing.T) {
	t.Parallel()

	t.Run("Python-style YAML with 2-space indent and flow arrays", func(t *testing.T) {
		t.Parallel()
		// Python's yaml.dump produces this style with flow sequences
		input := `name: steamapi
description: "Steam Web API"
version: '0.1.0'
base_url: "https://api.steampowered.com"
auth:
  type: api_key
  header: key
  in: query
  env_vars: [STEAM_API_KEY]
config:
  format: toml
  path: "~/.config/steamapi-pp-cli/config.toml"
resources:
  users:
    description: "Manage users"
    endpoints:
      list:
        method: GET
        path: /users
        description: "List users"
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "steamapi", s.Name)
		assert.Equal(t, "Steam Web API", s.Description)
		assert.Len(t, s.Resources, 1)
		assert.Contains(t, s.Resources, "users")
		assert.Equal(t, "api_key", s.Auth.Type)
		assert.Equal(t, []string{"STEAM_API_KEY"}, s.Auth.EnvVars)
	})

	t.Run("Go-style YAML still works (no regression)", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: api_key
  header: X-Api-Key
  env_vars:
    - TEST_API_KEY
config:
  format: toml
  path: ~/.config/testapi-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "testapi", s.Name)
		assert.Len(t, s.Resources, 1)
		assert.Equal(t, "api_key", s.Auth.Type)
	})

	t.Run("YAML with quoted string values", func(t *testing.T) {
		t.Parallel()
		input := `"name": "quotedapi"
"base_url": "https://api.example.com"
"auth":
  "type": "bearer_token"
  "header": "Authorization"
  "format": "Bearer {token}"
  "env_vars":
    - "QUOTED_TOKEN"
"config":
  "format": "toml"
  "path": "~/.config/quotedapi-pp-cli/config.toml"
"resources":
  "things":
    "description": "Manage things"
    "endpoints":
      "list":
        "method": "GET"
        "path": "/things"
        "description": "List things"
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "quotedapi", s.Name)
		assert.Len(t, s.Resources, 1)
		assert.Equal(t, "bearer_token", s.Auth.Type)
	})

	t.Run("composed auth with cookies and format", func(t *testing.T) {
		t.Parallel()
		input := `name: pagliacciapi
base_url: https://pag-api.azurewebsites.net/api
auth:
  type: composed
  header: Authorization
  format: "PagliacciAuth {customerId}|{authToken}"
  cookie_domain: pagliacci.com
  cookies:
    - customerId
    - authToken
resources:
  store:
    description: Manage stores
    endpoints:
      list:
        method: GET
        path: /Store
        description: List stores
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "composed", s.Auth.Type)
		assert.Equal(t, "Authorization", s.Auth.Header)
		assert.Equal(t, "PagliacciAuth {customerId}|{authToken}", s.Auth.Format)
		assert.Equal(t, "pagliacci.com", s.Auth.CookieDomain)
		assert.Equal(t, []string{"customerId", "authToken"}, s.Auth.Cookies)
	})

	t.Run("cookie auth without cookies field is nil", func(t *testing.T) {
		t.Parallel()
		input := `name: notionapi
base_url: https://api.notion.so
auth:
  type: cookie
  header: Cookie
  cookie_domain: ".notion.so"
resources:
  pages:
    description: Manage pages
    endpoints:
      list:
        method: GET
        path: /pages
        description: List pages
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "cookie", s.Auth.Type)
		assert.Nil(t, s.Auth.Cookies)
	})

	t.Run("invalid YAML still returns error", func(t *testing.T) {
		t.Parallel()
		input := `{{{not valid yaml at all`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
	})

	t.Run("valid YAML but missing required fields still fails validation", func(t *testing.T) {
		t.Parallel()
		input := `name: incomplete
description: Missing base_url and resources
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "base_url is required")
	})
}

func TestOperationsShorthand(t *testing.T) {
	s, err := Parse("../../testdata/operations-shorthand.yaml")
	require.NoError(t, err)

	assert.Equal(t, "testapi", s.Name)
	assert.Len(t, s.Resources, 3)

	t.Run("full CRUD operations expand to 5 endpoints", func(t *testing.T) {
		items := s.Resources["items"]
		assert.Len(t, items.Endpoints, 5)

		// list
		list := items.Endpoints["list"]
		assert.Equal(t, "GET", list.Method)
		assert.Equal(t, "/api/items", list.Path)

		// get
		get := items.Endpoints["get"]
		assert.Equal(t, "GET", get.Method)
		assert.Equal(t, "/api/items/{itemId}", get.Path)
		require.Len(t, get.Params, 1)
		assert.Equal(t, "itemId", get.Params[0].Name)
		assert.True(t, get.Params[0].Required)
		assert.True(t, get.Params[0].Positional)

		// create
		create := items.Endpoints["create"]
		assert.Equal(t, "POST", create.Method)
		assert.Equal(t, "/api/items", create.Path)

		// update
		update := items.Endpoints["update"]
		assert.Equal(t, "PATCH", update.Method)
		assert.Equal(t, "/api/items/{itemId}", update.Path)

		// delete
		del := items.Endpoints["delete"]
		assert.Equal(t, "DELETE", del.Method)
		assert.Equal(t, "/api/items/{itemId}", del.Path)
	})

	t.Run("partial operations expand correctly", func(t *testing.T) {
		cats := s.Resources["categories"]
		assert.Len(t, cats.Endpoints, 3)

		assert.Equal(t, "GET", cats.Endpoints["list"].Method)
		assert.Equal(t, "GET", cats.Endpoints["get"].Method)
		assert.Equal(t, "/api/categories/{categoryId}", cats.Endpoints["get"].Path)
		assert.Equal(t, "POST", cats.Endpoints["search"].Method)
		assert.Equal(t, "/api/categories/search", cats.Endpoints["search"].Path)
	})

	t.Run("explicit endpoints override operations", func(t *testing.T) {
		mixed := s.Resources["mixed"]
		// operations: [list, get] + explicit: [list, special] = 3 total
		assert.Len(t, mixed.Endpoints, 3)

		// Explicit list overrides operations-generated list
		list := mixed.Endpoints["list"]
		assert.Equal(t, "/api/mixed/custom-list", list.Path)
		assert.Equal(t, "Custom list endpoint overrides operations-generated one", list.Description)

		// Operations-generated get
		get := mixed.Endpoints["get"]
		assert.Equal(t, "/api/mixed/{mixedId}", get.Path)

		// Explicit-only special
		special := mixed.Endpoints["special"]
		assert.Equal(t, "POST", special.Method)
		assert.Equal(t, "/api/mixed/special", special.Path)
	})
}

func TestSingularize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"items", "item"},
		{"contacts", "contact"},
		{"companies", "company"},
		{"categories", "category"},
		{"properties", "property"},
		{"addresses", "address"},
		{"statuses", "status"},
		{"deals", "deal"},
		{"data", "data"},
		{"entries", "entry"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, singularize(tt.input))
		})
	}
}

func TestExpandOperationsNoPath(t *testing.T) {
	// Operations without a path should not expand
	input := `name: nopath
description: "Test"
base_url: "https://api.example.com"
resources:
  items:
    description: "No path"
    operations:
      - list
    endpoints:
      fallback:
        method: GET
        path: /items
        description: "Explicit fallback"
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	items := s.Resources["items"]
	// Only the explicit endpoint should exist, operations not expanded
	assert.Len(t, items.Endpoints, 1)
	assert.Contains(t, items.Endpoints, "fallback")
}

func TestEnrichEndpointPathParams(t *testing.T) {
	t.Run("placeholder not declared adds positional param", func(t *testing.T) {
		input := `
name: demo
base_url: http://x
auth: {type: none}
resources:
  filings:
    description: SEC filings
    endpoints:
      get:
        method: GET
        path: /submissions/CIK{cik}.json
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		params := s.Resources["filings"].Endpoints["get"].Params
		require.Len(t, params, 1)
		assert.Equal(t, "cik", params[0].Name)
		assert.True(t, params[0].Positional, "auto-injected placeholder should be positional")
		assert.True(t, params[0].Required, "path placeholder must be required")
		assert.False(t, params[0].PathParam, "auto-injected uses Positional, not PathParam")
	})

	t.Run("placeholder declared as flag is promoted to PathParam", func(t *testing.T) {
		// Reproduces the company-goat bug: spec author declares a param without
		// location:path (or with location:query) while the path template uses
		// {name} as a substitution. Path template wins; existing param must be
		// promoted to PathParam=true so URL substitution and MCP positionalParams
		// emission see it.
		input := `
name: demo
base_url: http://x
auth: {type: none}
resources:
  filings:
    description: SEC filings
    endpoints:
      get:
        method: GET
        path: /submissions/CIK{cik}.json
        params:
          - name: cik
            type: string
            description: 10-digit zero-padded SEC Central Index Key
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		params := s.Resources["filings"].Endpoints["get"].Params
		require.Len(t, params, 1, "should not duplicate the existing param")
		assert.Equal(t, "cik", params[0].Name)
		assert.True(t, params[0].PathParam, "declared param matching {placeholder} must be promoted to PathParam=true")
		assert.True(t, params[0].Required, "path placeholder must be required")
		assert.Equal(t, "10-digit zero-padded SEC Central Index Key", params[0].Description, "author description preserved")
	})

	t.Run("repeated placeholder is enriched once", func(t *testing.T) {
		input := `
name: demo
base_url: http://x
auth: {type: none}
resources:
  duplicates:
    description: weird API
    endpoints:
      twice:
        method: GET
        path: /a/{x}/b/{x}
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		params := s.Resources["duplicates"].Endpoints["twice"].Params
		require.Len(t, params, 1)
		assert.Equal(t, "x", params[0].Name)
	})

	t.Run("body param with same name as placeholder is not promoted", func(t *testing.T) {
		// When the author declared "id" in body AND the path has {id},
		// the body declaration is authoritative — we don't add a phantom
		// path param. Pins this behavior so the promotion path doesn't
		// accidentally widen.
		input := `
name: demo
base_url: http://x
auth: {type: none}
resources:
  things:
    description: things
    endpoints:
      update:
        method: PATCH
        path: /things/{id}
        body:
          - name: id
            type: string
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		params := s.Resources["things"].Endpoints["update"].Params
		// id is in body, not promoted in params — pinning current behavior.
		assert.Empty(t, params, "params should remain empty when placeholder is declared in body")
	})
}

func TestExtraCommandsParse(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
extra_commands:
  - name: dashboard
    description: Favorites at a glance
  - name: boxscore
    description: Full box score for an event
    args: "<event_id>"
  - name: tv airing-today
    description: TV episodes airing today
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, s.ExtraCommands, 3)
	assert.Equal(t, "dashboard", s.ExtraCommands[0].Name)
	assert.Equal(t, "Favorites at a glance", s.ExtraCommands[0].Description)
	assert.Empty(t, s.ExtraCommands[0].Args)
	assert.Equal(t, "<event_id>", s.ExtraCommands[1].Args)
	assert.Equal(t, "tv airing-today", s.ExtraCommands[2].Name)
}

func TestExtraCommandsAbsentIsBackwardCompatible(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, s.ExtraCommands)
}

func TestExtraCommandsValidation(t *testing.T) {
	base := func(extras []ExtraCommand) APISpec {
		return APISpec{
			Name:    "demo",
			BaseURL: "http://x",
			Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
			},
			ExtraCommands: extras,
		}
	}

	tests := []struct {
		name    string
		extras  []ExtraCommand
		wantErr string
	}{
		{
			name:    "missing name",
			extras:  []ExtraCommand{{Description: "no name"}},
			wantErr: "name is required",
		},
		{
			name:    "missing description",
			extras:  []ExtraCommand{{Name: "boxscore"}},
			wantErr: "description is required",
		},
		{
			name:    "uppercase name rejected",
			extras:  []ExtraCommand{{Name: "Boxscore", Description: "x"}},
			wantErr: "must be lowercase command path",
		},
		{
			name:    "underscore in name rejected",
			extras:  []ExtraCommand{{Name: "box_score", Description: "x"}},
			wantErr: "must be lowercase command path",
		},
		{
			name:    "duplicate name rejected",
			extras:  []ExtraCommand{{Name: "boxscore", Description: "first"}, {Name: "boxscore", Description: "second"}},
			wantErr: "appears more than once",
		},
		{
			name:    "more than three segments rejected",
			extras:  []ExtraCommand{{Name: "a b c d", Description: "too deep"}},
			wantErr: "must be lowercase command path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base(tt.extras)
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestExtraCommandsAcceptsValidShapes(t *testing.T) {
	valid := []ExtraCommand{
		{Name: "boxscore", Description: "single leaf"},
		{Name: "tv airing-today", Description: "two segments with hyphen"},
		{Name: "a b-c d", Description: "three segments"},
		{Name: "trending", Description: "no args"},
		{Name: "h2h", Description: "with digits and args", Args: "<team1> <team2>"},
	}
	s := APISpec{
		Name:          "demo",
		BaseURL:       "http://x",
		Resources:     map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
		ExtraCommands: valid,
	}
	require.NoError(t, s.Validate())
}

func TestExtraCommandsRoundTripYAML(t *testing.T) {
	original := APISpec{
		Name:    "demo",
		BaseURL: "http://x",
		Auth:    AuthConfig{Type: "none"},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
		ExtraCommands: []ExtraCommand{
			{Name: "dashboard", Description: "Favorites"},
			{Name: "boxscore", Description: "Box score", Args: "<event_id>"},
		},
	}
	data, err := yaml.Marshal(original)
	require.NoError(t, err)
	var parsed APISpec
	require.NoError(t, yaml.Unmarshal(data, &parsed))
	assert.Equal(t, original.ExtraCommands, parsed.ExtraCommands)
}

func TestCacheShareParse(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
cache:
  enabled: true
  stale_after: 6h
  refresh_timeout: 30s
  env_opt_out: DEMO_NO_AUTO_REFRESH
  resources:
    items: 5m
  commands:
    - name: dashboard
      resources: [items]
share:
  enabled: true
  snapshot_tables:
    - items
    - sync_state
  default_repo: git@github.com:acme/demo-snapshots.git
  default_branch: main
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.True(t, s.Cache.Enabled)
	assert.Equal(t, "6h", s.Cache.StaleAfter)
	assert.Equal(t, "30s", s.Cache.RefreshTimeout)
	assert.Equal(t, "DEMO_NO_AUTO_REFRESH", s.Cache.EnvOptOut)
	assert.Equal(t, "5m", s.Cache.Resources["items"])
	require.Len(t, s.Cache.Commands, 1)
	assert.Equal(t, "dashboard", s.Cache.Commands[0].Name)
	assert.Equal(t, []string{"items"}, s.Cache.Commands[0].Resources)
	assert.True(t, s.Share.Enabled)
	assert.Equal(t, []string{"items", "sync_state"}, s.Share.SnapshotTables)
	assert.Equal(t, "git@github.com:acme/demo-snapshots.git", s.Share.DefaultRepo)
	assert.Equal(t, "main", s.Share.DefaultBranch)
}

func TestCacheShareAbsentIsBackwardCompatible(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.False(t, s.Cache.Enabled)
	assert.False(t, s.Share.Enabled)
	assert.Empty(t, s.Cache.Resources)
	assert.Empty(t, s.Share.SnapshotTables)
}

func TestCacheShareValidation(t *testing.T) {
	base := func(cache CacheConfig, share ShareConfig) APISpec {
		return APISpec{
			Name:    "demo",
			BaseURL: "http://x",
			Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
			},
			Cache: cache,
			Share: share,
		}
	}

	tests := []struct {
		name    string
		cache   CacheConfig
		share   ShareConfig
		wantErr string
	}{
		{
			name:    "share enabled without snapshot_tables",
			share:   ShareConfig{Enabled: true},
			wantErr: "non-empty share.snapshot_tables allowlist",
		},
		{
			name:    "share snapshot_tables set but disabled",
			share:   ShareConfig{Enabled: false, SnapshotTables: []string{"items"}},
			wantErr: "snapshot_tables is set but share.enabled is false",
		},
		{
			name:    "share table auth_tokens rejected",
			share:   ShareConfig{Enabled: true, SnapshotTables: []string{"auth_tokens"}},
			wantErr: "denylist",
		},
		{
			name:    "share table oauth_cache rejected",
			share:   ShareConfig{Enabled: true, SnapshotTables: []string{"oauth_cache"}},
			wantErr: "denylist",
		},
		{
			name:    "share table session_secrets rejected",
			share:   ShareConfig{Enabled: true, SnapshotTables: []string{"session_secrets"}},
			wantErr: "denylist",
		},
		{
			name:    "share table uppercase rejected",
			share:   ShareConfig{Enabled: true, SnapshotTables: []string{"Items"}},
			wantErr: "lowercase SQLite identifier",
		},
		{
			name:    "share table duplicate rejected",
			share:   ShareConfig{Enabled: true, SnapshotTables: []string{"items", "items"}},
			wantErr: "appears more than once",
		},
		{
			name:    "cache stale_after invalid duration",
			cache:   CacheConfig{Enabled: true, StaleAfter: "yesterday"},
			wantErr: "not a valid Go duration",
		},
		{
			name:    "cache refresh_timeout invalid duration",
			cache:   CacheConfig{Enabled: true, RefreshTimeout: "soonish"},
			wantErr: "not a valid Go duration",
		},
		{
			name:    "cache per-resource invalid duration",
			cache:   CacheConfig{Enabled: true, Resources: map[string]string{"items": "eh"}},
			wantErr: "not a valid Go duration",
		},
		{
			name:    "cache command uppercase rejected",
			cache:   CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "Today", Resources: []string{"items"}}}},
			wantErr: "lowercase command path",
		},
		{
			name:    "cache command requires enabled cache",
			cache:   CacheConfig{Commands: []CacheCommand{{Name: "today", Resources: []string{"items"}}}},
			wantErr: "cache.enabled is false",
		},
		{
			name:    "cache command duplicate rejected",
			cache:   CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "today", Resources: []string{"items"}}, {Name: "today", Resources: []string{"items"}}}},
			wantErr: "appears more than once",
		},
		{
			name:    "cache command resources required",
			cache:   CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "today"}}},
			wantErr: "resources must not be empty",
		},
		{
			name:    "cache command unknown resource rejected",
			cache:   CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "today", Resources: []string{"launches"}}}},
			wantErr: "is not declared in resources",
		},
		{
			name:    "cache command duplicate resource rejected",
			cache:   CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "today", Resources: []string{"items", "items"}}}},
			wantErr: "appears more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base(tt.cache, tt.share)
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCacheShareAcceptsValidShapes(t *testing.T) {
	tests := []struct {
		name  string
		cache CacheConfig
		share ShareConfig
	}{
		{
			name:  "cache only, no share",
			cache: CacheConfig{Enabled: true, StaleAfter: "6h", RefreshTimeout: "30s"},
		},
		{
			name:  "cache with per-resource overrides",
			cache: CacheConfig{Enabled: true, StaleAfter: "6h", Resources: map[string]string{"items": "5m", "teams": "24h"}},
		},
		{
			name:  "cache with custom command coverage",
			cache: CacheConfig{Enabled: true, Commands: []CacheCommand{{Name: "dashboard", Resources: []string{"items"}}}},
		},
		{
			name:  "share only, no cache",
			share: ShareConfig{Enabled: true, SnapshotTables: []string{"items", "teams", "sync_state"}},
		},
		{
			name:  "cache and share both enabled",
			cache: CacheConfig{Enabled: true, StaleAfter: "6h"},
			share: ShareConfig{Enabled: true, SnapshotTables: []string{"items"}, DefaultBranch: "main"},
		},
		{
			name:  "composite duration (90m, 1h30m) accepted",
			cache: CacheConfig{Enabled: true, StaleAfter: "1h30m"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := APISpec{
				Name:      "demo",
				BaseURL:   "http://x",
				Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
				Cache:     tt.cache,
				Share:     tt.share,
			}
			require.NoError(t, s.Validate())
		})
	}
}

func TestMCPConfigAbsentIsBackwardCompatible(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, s.MCP.Transport)
	assert.Empty(t, s.MCP.Addr)
	assert.Equal(t, []string{"stdio"}, s.MCP.EffectiveTransports())
	assert.True(t, s.MCP.HasTransport("stdio"))
	assert.False(t, s.MCP.HasTransport("http"))
}

func TestMCPConfigParses(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
mcp:
  transport: [stdio, http]
  addr: ":8123"
resources:
  items:
    description: "Items"
    endpoints:
      list:
        method: GET
        path: /items
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.NoError(t, s.Validate())
	assert.Equal(t, []string{"stdio", "http"}, s.MCP.Transport)
	assert.Equal(t, ":8123", s.MCP.Addr)
	assert.True(t, s.MCP.HasTransport("stdio"))
	assert.True(t, s.MCP.HasTransport("http"))
	assert.True(t, s.MCP.HasTransport("HTTP"), "HasTransport is case-insensitive")
}

func TestMCPConfigValidation(t *testing.T) {
	base := func(mcp MCPConfig) APISpec {
		return APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			MCP:       mcp,
		}
	}

	tests := []struct {
		name    string
		mcp     MCPConfig
		wantErr string
	}{
		{
			name:    "unknown transport rejected",
			mcp:     MCPConfig{Transport: []string{"grpc"}},
			wantErr: "not a supported transport",
		},
		{
			name:    "empty string transport rejected",
			mcp:     MCPConfig{Transport: []string{""}},
			wantErr: "value must not be empty",
		},
		{
			name:    "duplicate transport rejected",
			mcp:     MCPConfig{Transport: []string{"stdio", "stdio"}},
			wantErr: "appears more than once",
		},
		{
			name:    "addr without http rejected",
			mcp:     MCPConfig{Transport: []string{"stdio"}, Addr: ":7777"},
			wantErr: "mcp.addr is set but mcp.transport does not include http",
		},
		{
			name:    "malformed addr rejected",
			mcp:     MCPConfig{Transport: []string{"http"}, Addr: "7777"},
			wantErr: "not a valid bind address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base(tt.mcp)
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMCPIntentsParse(t *testing.T) {
	input := `
name: demo
base_url: http://x
auth:
  type: none
config:
  format: toml
  path: ~/.config/demo/config.toml
mcp:
  endpoint_tools: hidden
  intents:
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
      returns: item
resources:
  items:
    description: "Items"
    endpoints:
      get:
        method: GET
        path: /items/{id}
        params:
          - name: id
            type: string
            required: true
            positional: true
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.NoError(t, s.Validate())
	require.Len(t, s.MCP.Intents, 1)
	assert.Equal(t, "fetch_and_summarize", s.MCP.Intents[0].Name)
	assert.Equal(t, "hidden", s.MCP.EndpointTools)
}

func TestMCPIntentsValidation(t *testing.T) {
	base := func(mcp MCPConfig) APISpec {
		return APISpec{
			Name:    "demo",
			BaseURL: "http://x",
			Resources: map[string]Resource{
				"items": {
					Endpoints: map[string]Endpoint{
						"get":  {Method: "GET", Path: "/items/{id}"},
						"list": {Method: "GET", Path: "/items"},
					},
				},
			},
			MCP: mcp,
		}
	}

	ok := Intent{
		Name:        "get_item",
		Description: "Get an item",
		Params:      []IntentParam{{Name: "id", Type: "string", Required: true, Description: "id"}},
		Steps: []IntentStep{
			{Endpoint: "items.get", Bind: map[string]string{"id": "${input.id}"}, Capture: "item"},
		},
		Returns: "item",
	}

	tests := []struct {
		name    string
		intents []Intent
		tools   string
		wantErr string
	}{
		{
			name:    "unknown endpoint reference rejected",
			intents: []Intent{{Name: "x", Description: "x", Steps: []IntentStep{{Endpoint: "items.delete"}}}},
			wantErr: "does not resolve against the spec",
		},
		{
			name: "undeclared input reference rejected",
			intents: []Intent{{
				Name:        "x",
				Description: "x",
				Steps:       []IntentStep{{Endpoint: "items.get", Bind: map[string]string{"id": "${input.missing}"}}},
			}},
			wantErr: "undeclared input",
		},
		{
			name: "capture referenced before definition rejected",
			intents: []Intent{{
				Name:        "x",
				Description: "x",
				Steps: []IntentStep{
					{Endpoint: "items.get", Bind: map[string]string{"id": "${first.id}"}, Capture: "second"},
				},
			}},
			wantErr: "undeclared capture",
		},
		{
			name: "duplicate intent name rejected",
			intents: []Intent{
				ok,
				{Name: "get_item", Description: "dup", Steps: []IntentStep{{Endpoint: "items.get"}}},
			},
			wantErr: "appears more than once",
		},
		{
			name: "bad intent param type rejected",
			intents: []Intent{{
				Name:        "x",
				Description: "x",
				Params:      []IntentParam{{Name: "id", Type: "object", Description: "bad"}},
				Steps:       []IntentStep{{Endpoint: "items.get"}},
			}},
			wantErr: "must be one of string, integer, boolean",
		},
		{
			name: "missing returns capture rejected",
			intents: []Intent{{
				Name:        "x",
				Description: "x",
				Steps:       []IntentStep{{Endpoint: "items.get", Capture: "item"}},
				Returns:     "not_a_capture",
			}},
			wantErr: "returns \"not_a_capture\" does not match",
		},
		{
			name: "malformed binding rejected",
			intents: []Intent{{
				Name:        "x",
				Description: "x",
				Params:      []IntentParam{{Name: "id", Type: "string", Description: "id"}},
				Steps:       []IntentStep{{Endpoint: "items.get", Bind: map[string]string{"id": "${input}"}}},
			}},
			wantErr: "is not a valid binding",
		},
		{
			name:    "bad endpoint_tools value rejected",
			intents: []Intent{ok},
			tools:   "maybe",
			wantErr: "must be \"visible\" or \"hidden\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcp := MCPConfig{Intents: tt.intents}
			if tt.tools != "" {
				mcp.EndpointTools = tt.tools
			}
			s := base(mcp)
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMCPOrchestrationValidation(t *testing.T) {
	base := func(mcp MCPConfig) APISpec {
		return APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			MCP:       mcp,
		}
	}

	tests := []struct {
		name    string
		mcp     MCPConfig
		wantErr string
	}{
		{
			name:    "unknown orchestration rejected",
			mcp:     MCPConfig{Orchestration: "magic"},
			wantErr: "must be one of endpoint-mirror, intent, code",
		},
		{
			name:    "negative threshold rejected",
			mcp:     MCPConfig{OrchestrationThreshold: -1},
			wantErr: "must be non-negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base(tt.mcp)
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	// Happy paths for orchestration modes and threshold fallback.
	ok := []MCPConfig{
		{}, // absent = endpoint-mirror default
		{Orchestration: "endpoint-mirror"},
		{Orchestration: "intent"},
		{Orchestration: "code"},
		{Orchestration: "code", OrchestrationThreshold: 100},
	}
	for i, mcp := range ok {
		s := base(mcp)
		require.NoError(t, s.Validate(), "case %d", i)
	}
	assert.Equal(t, 50, MCPConfig{}.EffectiveOrchestrationThreshold())
	assert.Equal(t, 100, MCPConfig{OrchestrationThreshold: 100}.EffectiveOrchestrationThreshold())
	assert.True(t, MCPConfig{Orchestration: "code"}.IsCodeOrchestration())
	assert.False(t, MCPConfig{Orchestration: "intent"}.IsCodeOrchestration())
}

func TestMCPConfigAcceptsValidShapes(t *testing.T) {
	tests := []struct {
		name string
		mcp  MCPConfig
	}{
		{name: "empty config (backward compatible)"},
		{name: "stdio only explicit", mcp: MCPConfig{Transport: []string{"stdio"}}},
		{name: "both transports", mcp: MCPConfig{Transport: []string{"stdio", "http"}}},
		{name: "http only", mcp: MCPConfig{Transport: []string{"http"}}},
		{name: "http with default addr", mcp: MCPConfig{Transport: []string{"http"}, Addr: ":7777"}},
		{name: "http with host addr", mcp: MCPConfig{Transport: []string{"stdio", "http"}, Addr: "127.0.0.1:8080"}},
		{name: "uppercase transport normalizes", mcp: MCPConfig{Transport: []string{"HTTP"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := APISpec{
				Name:      "demo",
				BaseURL:   "http://x",
				Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
				MCP:       tt.mcp,
			}
			require.NoError(t, s.Validate())
		})
	}
}

func TestHTTPTransportValidationAndDefaults(t *testing.T) {
	t.Parallel()

	base := APISpec{
		Name:    "demo",
		BaseURL: "http://x",
		Auth:    AuthConfig{Type: "none"},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	assert.Equal(t, HTTPTransportStandard, base.EffectiveHTTPTransport())

	sniffed := base
	sniffed.SpecSource = "sniffed"
	assert.Equal(t, HTTPTransportBrowserChrome, sniffed.EffectiveHTTPTransport())
	require.NoError(t, sniffed.Validate())

	community := base
	community.SpecSource = "community"
	assert.Equal(t, HTTPTransportBrowserChrome, community.EffectiveHTTPTransport())

	h3 := base
	h3.HTTPTransport = HTTPTransportBrowserChromeH3
	assert.Equal(t, HTTPTransportBrowserChromeH3, h3.EffectiveHTTPTransport())
	assert.True(t, h3.UsesBrowserHTTPTransport())
	assert.True(t, h3.UsesBrowserHTTP3Transport())
	assert.True(t, h3.UsesBrowserManagedUserAgent())
	require.NoError(t, h3.Validate())

	override := sniffed
	override.HTTPTransport = HTTPTransportStandard
	assert.Equal(t, HTTPTransportStandard, override.EffectiveHTTPTransport())
	require.NoError(t, override.Validate())

	runtime := base
	runtime.HTTPTransport = "browser-runtime"
	assert.Equal(t, HTTPTransportStandard, runtime.EffectiveHTTPTransport())
	assert.False(t, runtime.UsesBrowserManagedUserAgent())
	require.ErrorContains(t, runtime.Validate(), "http_transport must be one of")

	invalid := base
	invalid.HTTPTransport = "lynx"
	require.ErrorContains(t, invalid.Validate(), "http_transport must be one of")
}

func TestHTMLResponseExtractionValidation(t *testing.T) {
	t.Parallel()

	validHTMLSpec := func() APISpec {
		return APISpec{
			Name:    "webhtml",
			BaseURL: "https://www.example.com",
			Resources: map[string]Resource{
				"posts": {
					Description: "Posts",
					Endpoints: map[string]Endpoint{
						"list": {
							Method:         "GET",
							Path:           "/",
							Description:    "List posts",
							ResponseFormat: ResponseFormatHTML,
							HTMLExtract: &HTMLExtract{
								Mode:         HTMLExtractModeLinks,
								LinkPrefixes: []string{"/products"},
								Limit:        20,
							},
							Response: ResponseDef{Type: "array", Item: "html_link"},
						},
					},
				},
			},
		}
	}

	base := validHTMLSpec()
	require.NoError(t, base.Validate())
	assert.True(t, base.HasHTMLExtraction())
	assert.True(t, base.Resources["posts"].Endpoints["list"].UsesHTMLResponse())

	badFormat := validHTMLSpec()
	ep := badFormat.Resources["posts"].Endpoints["list"]
	ep.ResponseFormat = "xml"
	badFormat.Resources["posts"].Endpoints["list"] = ep
	require.ErrorContains(t, badFormat.Validate(), "response_format must be one of")

	badMethod := validHTMLSpec()
	ep = badMethod.Resources["posts"].Endpoints["list"]
	ep.Method = "POST"
	badMethod.Resources["posts"].Endpoints["list"] = ep
	require.ErrorContains(t, badMethod.Validate(), "html response_format is only supported")
}

func TestHTMLExtract_EmbeddedJSONMode(t *testing.T) {
	t.Parallel()

	embeddedJSON := func() APISpec {
		return APISpec{
			Name:    "nextapp",
			BaseURL: "https://www.example.com",
			Resources: map[string]Resource{
				"recipes": {
					Description: "Recipes",
					Endpoints: map[string]Endpoint{
						"browse": {
							Method:         "GET",
							Path:           "/recipes/{tag}",
							Description:    "Browse recipes by tag",
							ResponseFormat: ResponseFormatHTML,
							HTMLExtract: &HTMLExtract{
								Mode:           HTMLExtractModeEmbeddedJSON,
								ScriptSelector: "script#__NEXT_DATA__",
								JSONPath:       "props.pageProps.recipesByTag.results",
							},
							Response: ResponseDef{Type: "array", Item: "recipe"},
						},
					},
				},
			},
		}
	}

	// Happy path: embedded-json with explicit selector + json_path validates.
	base := embeddedJSON()
	require.NoError(t, base.Validate())
	ep := base.Resources["recipes"].Endpoints["browse"]
	assert.Equal(t, HTMLExtractModeEmbeddedJSON, ep.HTMLExtract.EffectiveMode())
	assert.Equal(t, "script#__NEXT_DATA__", ep.HTMLExtract.EffectiveScriptSelector())

	// Default selector: empty ScriptSelector resolves to the Next.js
	// pages-router default.
	defaults := embeddedJSON()
	depEP := defaults.Resources["recipes"].Endpoints["browse"]
	depEP.HTMLExtract.ScriptSelector = ""
	defaults.Resources["recipes"].Endpoints["browse"] = depEP
	require.NoError(t, defaults.Validate())
	assert.Equal(t, DefaultEmbeddedJSONScriptSelector, depEP.HTMLExtract.EffectiveScriptSelector())

	// Empty json_path is valid (returns whole pageProps).
	emptyPath := embeddedJSON()
	pep := emptyPath.Resources["recipes"].Endpoints["browse"]
	pep.HTMLExtract.JSONPath = ""
	emptyPath.Resources["recipes"].Endpoints["browse"] = pep
	require.NoError(t, emptyPath.Validate())

	// Whitespace-only ScriptSelector is rejected (catch typos like " ").
	badSelector := embeddedJSON()
	bsEP := badSelector.Resources["recipes"].Endpoints["browse"]
	bsEP.HTMLExtract.ScriptSelector = "   "
	badSelector.Resources["recipes"].Endpoints["browse"] = bsEP
	require.ErrorContains(t, badSelector.Validate(), "script_selector cannot be whitespace-only")

	// Unknown mode is still rejected and the error message names embedded-json.
	unknownMode := embeddedJSON()
	uEP := unknownMode.Resources["recipes"].Endpoints["browse"]
	uEP.HTMLExtract.Mode = "rsc-stream"
	unknownMode.Resources["recipes"].Endpoints["browse"] = uEP
	err := unknownMode.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "embedded-json")
}

func TestEnrichPathParams(t *testing.T) {
	t.Parallel()

	t.Run("explicit endpoint with placeholders gets positional Params auto-added", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  customer:
    description: Customer endpoints
    endpoints:
      get:
        method: GET
        path: /Customer/{customerId}
        description: Get customer by ID
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["customer"].Endpoints["get"]
		require.Len(t, ep.Params, 1)
		assert.Equal(t, "customerId", ep.Params[0].Name)
		assert.True(t, ep.Params[0].Positional)
		assert.True(t, ep.Params[0].Required)
		assert.Equal(t, "string", ep.Params[0].Type)
	})

	t.Run("multiple placeholders preserve path order", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  scheduling:
    description: Time windows
    endpoints:
      slots_for_date:
        method: GET
        path: /TimeWindows/{storeId}/{serviceType}/{date}
        description: Available slots for a date
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["scheduling"].Endpoints["slots_for_date"]
		require.Len(t, ep.Params, 3)
		assert.Equal(t, "storeId", ep.Params[0].Name)
		assert.Equal(t, "serviceType", ep.Params[1].Name)
		assert.Equal(t, "date", ep.Params[2].Name)
		for _, p := range ep.Params {
			assert.True(t, p.Positional, "param %q should be Positional", p.Name)
			assert.True(t, p.Required, "param %q should be Required", p.Name)
		}
	})

	t.Run("author-declared params are not duplicated or overwritten", func(t *testing.T) {
		t.Parallel()
		// `customerId` is declared with a custom description and integer type;
		// enrichment must leave it alone.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  customer:
    description: Customer endpoints
    endpoints:
      get:
        method: GET
        path: /Customer/{customerId}
        description: Get customer
        params:
          - name: customerId
            type: integer
            required: true
            positional: true
            description: Numeric customer ID
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["customer"].Endpoints["get"]
		require.Len(t, ep.Params, 1, "should not duplicate the declared param")
		assert.Equal(t, "customerId", ep.Params[0].Name)
		assert.Equal(t, "integer", ep.Params[0].Type, "author's type must be preserved")
		assert.Equal(t, "Numeric customer ID", ep.Params[0].Description, "author's description must be preserved")
	})

	t.Run("endpoint with no placeholders is unchanged", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  store:
    description: Stores
    endpoints:
      list:
        method: GET
        path: /Store
        description: List stores
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["store"].Endpoints["list"]
		assert.Empty(t, ep.Params, "no placeholders should mean no Params added")
	})

	t.Run("operations shorthand still works (regression)", func(t *testing.T) {
		t.Parallel()
		// The shorthand path already populated Params correctly before this
		// change; confirm enrichment doesn't break it.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  items:
    description: Items
    path: /api/items
    operations: [list, get, create, update, delete]
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		getEp := s.Resources["items"].Endpoints["get"]
		require.Len(t, getEp.Params, 1)
		assert.Equal(t, "itemId", getEp.Params[0].Name)
		assert.True(t, getEp.Params[0].Positional)
	})

	t.Run("repeated placeholder in same path is added once", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  pair:
    description: Pair
    endpoints:
      twin:
        method: GET
        path: /Pair/{id}/twin/{id}
        description: Twin by ID
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["pair"].Endpoints["twin"]
		require.Len(t, ep.Params, 1, "repeated placeholder should produce one Param")
		assert.Equal(t, "id", ep.Params[0].Name)
	})

	t.Run("sub-resource endpoints are also enriched", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  store:
    description: Stores
    sub_resources:
      menu:
        description: Per-store menus
        endpoints:
          get:
            method: GET
            path: /Store/{storeId}/Menu/{menuId}
            description: Get menu by store and ID
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["store"].SubResources["menu"].Endpoints["get"]
		require.Len(t, ep.Params, 2)
		assert.Equal(t, "storeId", ep.Params[0].Name)
		assert.Equal(t, "menuId", ep.Params[1].Name)
	})
}

func TestValidateReservedNames(t *testing.T) {
	t.Parallel()

	t.Run("reserved resource name is rejected with a clear rename hint", func(t *testing.T) {
		t.Parallel()
		// `feedback` collides with the reserved feedback.go template that
		// declares the in-band agent feedback channel. Two collisions: file
		// overwrite and `newFeedbackCmd` redeclaration.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  feedback:
    description: Customer feedback
    endpoints:
      submit:
        method: POST
        path: /feedback
        description: Submit feedback
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"feedback"`)
		assert.Contains(t, err.Error(), "reserved Printing Press template")
		assert.Contains(t, err.Error(), "Rename")
		assert.Contains(t, err.Error(), "newFeedbackCmd", "error names the actual generated function")
		assert.Contains(t, err.Error(), `"feedback_resource"`, "error suggests a concrete rename")
	})

	t.Run("multi-word reserved name produces correct PascalCase function name", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  agent_context:
    description: Should be rejected
    endpoints:
      list:
        method: GET
        path: /agent_context
        description: list
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		// The error must name the actual generated function — newAgentContextCmd —
		// not newAgent_contextCmd. The previous capitalize-first variant lied
		// about the function name, which would confuse users debugging the
		// collision.
		assert.Contains(t, err.Error(), "newAgentContextCmd")
		assert.NotContains(t, err.Error(), "newAgent_contextCmd")
	})

	t.Run("auth resource name rejected", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  auth:
    description: Auth surface
    endpoints:
      list:
        method: GET
        path: /auth
        description: list
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"auth"`)
	})

	t.Run("non-reserved name with reserved-substring is allowed", func(t *testing.T) {
		t.Parallel()
		// "customer_feedback" contains "feedback" but is not itself reserved;
		// we only reject exact matches because file emit is by exact name.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  customer_feedback:
    description: Customer feedback (renamed)
    endpoints:
      submit:
        method: POST
        path: /feedback
        description: Submit feedback
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})

	t.Run("sub-resources are NOT subject to the reserved-name check", func(t *testing.T) {
		t.Parallel()
		// Sub-resources emit under <parent>_<sub>.go and produce
		// new<Parent><Sub>Cmd identifiers, so they cannot collide with the
		// single-file templates regardless of name.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  customer:
    description: Customer
    sub_resources:
      feedback:
        description: Customer-feedback sub-resource
        endpoints:
          submit:
            method: POST
            path: /customer/feedback
            description: Submit
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})

	t.Run("known clobbers are all in the set", func(t *testing.T) {
		t.Parallel()
		// Pin a baseline. Removing any of these from ReservedCLIResourceNames
		// without first removing the corresponding generator template is a
		// regression that will reintroduce silent overwrites.
		mustReserve := []string{"feedback", "doctor", "auth", "helpers", "agent_context", "profile", "deliver", "which", "sync", "tail", "search", "client", "cache", "export", "import"}
		for _, name := range mustReserve {
			_, ok := ReservedCLIResourceNames[name]
			assert.True(t, ok, "%q must remain in ReservedCLIResourceNames — losing it would reintroduce silent template overwrites", name)
		}
	})
}

func TestValidateFrameworkCobraCollisions(t *testing.T) {
	t.Parallel()

	t.Run("version resource is rejected with a clear shadow hint", func(t *testing.T) {
		t.Parallel()
		// `version` is in ReservedCobraUseNames but NOT in
		// ReservedCLIResourceNames (no version.go template — the version
		// subcommand is added inside root.go.tmpl), so this validator is
		// what catches it.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  version:
    description: API version metadata
    endpoints:
      list:
        method: GET
        path: /version
        description: List versions
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"version"`, "error must name the colliding resource")
		assert.Contains(t, err.Error(), "shadow framework cobra command", "error must explain the failure mode")
	})

	t.Run("rename suggestion uses api slug when spec name is set", func(t *testing.T) {
		t.Parallel()
		input := `name: pokeapi
base_url: https://pokeapi.co/api/v2
auth:
  type: none
resources:
  version:
    description: Version
    endpoints:
      list:
        method: GET
        path: /version
        description: List
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pokeapi_version", "rename suggestion should incorporate the api slug")
	})

	t.Run("non-colliding resource passes", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  store:
    description: Store
    endpoints:
      list:
        method: GET
        path: /stores
        description: List
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})

	t.Run("substring matches are NOT rejected", func(t *testing.T) {
		t.Parallel()
		// `versioning_history` contains `version` as a substring but is
		// not equal to it after kebab-casing — should pass.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  versioning_history:
    description: Versioning history
    endpoints:
      list:
        method: GET
        path: /versioning_history
        description: List
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})

	t.Run("sub-resources are NOT subject to the framework collision check", func(t *testing.T) {
		t.Parallel()
		// Sub-resources register as subcommands of their parent, not at
		// the root, so they cannot shadow framework commands.
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  game:
    description: Game
    sub_resources:
      version:
        description: Per-game version metadata
        endpoints:
          list:
            method: GET
            path: /game/{id}/version
            description: List
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})
}

func TestCLIDescriptionParses(t *testing.T) {
	t.Parallel()
	input := `name: testapi
base_url: https://api.example.com
cli_description: "Manage testapi resources from the terminal"
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  store:
    description: Stores
    endpoints:
      list:
        method: GET
        path: /stores
        description: List stores
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "Manage testapi resources from the terminal", s.CLIDescription)
}

func TestCLIDescriptionAbsent(t *testing.T) {
	t.Parallel()
	input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  store:
    description: Stores
    endpoints:
      list:
        method: GET
        path: /stores
        description: List stores
`
	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, s.CLIDescription, "field should be empty when not declared")
}

func TestSnakeToPascal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, expected string
	}{
		{"feedback", "Feedback"},
		{"agent_context", "AgentContext"},
		{"customer_feedback", "CustomerFeedback"},
		{"a_b_c", "ABC"},
		{"already_PascalCase", "AlreadyPascalCase"},
		{"", ""},
		{"_leading", "Leading"},
		{"trailing_", "Trailing"},
		{"single", "Single"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, snakeToPascal(tt.input))
		})
	}
}

// TestValidateRejectsResourceBaseURLWithProxyEnvelope — proxy-envelope
// POSTs every request to a single URL; per-resource overrides would
// be silently ignored. Validate must fail-fast so spec authors who
// declare both see the conflict at parse time, not at runtime when
// requests mysteriously route to the wrong host.
func TestValidateRejectsResourceBaseURLWithProxyEnvelope(t *testing.T) {
	t.Parallel()
	s := &APISpec{
		Name:          "proxytest",
		Version:       "0.1.0",
		BaseURL:       "https://proxy.example.com",
		ClientPattern: "proxy-envelope",
		Resources: map[string]Resource{
			"items": {
				BaseURL: "https://other.example.com/v1",
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List"},
				},
			},
		},
	}
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy-envelope")
	assert.Contains(t, err.Error(), "base_url")
}

// TestValidateAcceptsResourceBaseURLWithoutProxyEnvelope — the same
// resource override is accepted when client_pattern is not the proxy
// flavor. Negative cases (no resource override, proxy-envelope alone)
// stay valid.
func TestValidateAcceptsResourceBaseURLWithoutProxyEnvelope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec *APISpec
	}{
		{
			name: "rest client with resource override",
			spec: &APISpec{
				Name:          "ok-multihost",
				Version:       "0.1.0",
				BaseURL:       "https://api.example.com",
				ClientPattern: "rest",
				Resources: map[string]Resource{
					"items": {
						BaseURL: "https://other.example.com/v1",
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "/items", Description: "List"},
						},
					},
				},
			},
		},
		{
			name: "proxy-envelope without any resource override",
			spec: &APISpec{
				Name:          "proxy-only",
				Version:       "0.1.0",
				BaseURL:       "https://proxy.example.com",
				ClientPattern: "proxy-envelope",
				Resources: map[string]Resource{
					"items": {
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "/items", Description: "List"},
						},
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, tc.spec.Validate())
		})
	}
}
