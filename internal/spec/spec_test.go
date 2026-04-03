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
