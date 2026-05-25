package spec

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// captureWarnings runs fn with the package warning sink redirected to a
// buffer and returns everything written to it. This replaces an older
// approach that reassigned the process-wide os.Stderr, which was racy and
// coupled the assertion to a global file handle.
func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()

	old := warnWriter
	var buf bytes.Buffer
	warnWriter = &buf
	t.Cleanup(func() { warnWriter = old })

	fn()
	return buf.String()
}

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

func TestParsePublicParamNames(t *testing.T) {
	yamlSpec := []byte(`
name: public-params
base_url: https://api.example.com
auth:
  type: none
resources:
  stores:
    endpoints:
      find:
        method: GET
        path: /stores
        params:
          - name: s
            flag_name: address
            aliases: [s]
            type: string
            description: Street address
      search:
        method: POST
        path: /stores/search
        body:
          - name: startAfter
            body_name: searchAfter
            type: array
            description: Pagination cursor
`)
	s, err := ParseBytes(yamlSpec)
	require.NoError(t, err)
	param := s.Resources["stores"].Endpoints["find"].Params[0]
	assert.Equal(t, "s", param.Name)
	assert.Equal(t, "address", param.FlagName)
	assert.Equal(t, []string{"s"}, param.Aliases)
	bodyParam := s.Resources["stores"].Endpoints["search"].Body[0]
	assert.Equal(t, "startAfter", bodyParam.Name)
	assert.Equal(t, "searchAfter", bodyParam.BodyName)

	jsonSpec := []byte(`{
  "name": "public-params-json",
  "base_url": "https://api.example.com",
  "auth": {"type": "none"},
  "resources": {
    "stores": {
      "endpoints": {
        "find": {
          "method": "GET",
          "path": "/stores",
          "params": [
            {"name": "c", "flag_name": "city", "aliases": ["c"], "type": "string"}
          ]
        }
      }
    }
  }
}`)
	s, err = ParseBytes(jsonSpec)
	require.NoError(t, err)
	param = s.Resources["stores"].Endpoints["find"].Params[0]
	assert.Equal(t, "c", param.Name)
	assert.Equal(t, "city", param.FlagName)
	assert.Equal(t, []string{"c"}, param.Aliases)
}

func TestParseBodyObjectSchema(t *testing.T) {
	yamlSpec := []byte(`
name: rich-body
base_url: https://api.example.com
auth:
  type: none
resources:
  queries:
    endpoints:
      execute:
        method: POST
        path: /graphql
        body:
          type: object
          required: [query]
          properties:
            query:
              type: string
              description: GraphQL document
            variables:
              type: object
              description: GraphQL variables
            options:
              type: object
              properties:
                includeNulls:
                  type: bool
                  required: true
            queries:
              type: array
              items:
                type: object
                required: [query]
                properties:
                  query:
                    type: string
`)
	s, err := ParseBytes(yamlSpec)
	require.NoError(t, err)

	body := s.Resources["queries"].Endpoints["execute"].Body
	require.Len(t, body, 4)
	assert.Equal(t, Param{Name: "query", Type: "string", Required: true, Description: "GraphQL document"}, body[0])
	assert.Equal(t, "variables", body[1].Name)
	assert.Equal(t, "object", body[1].Type)
	assert.Equal(t, "GraphQL variables", body[1].Description)
	require.Len(t, body[2].Fields, 1)
	assert.Equal(t, "includeNulls", body[2].Fields[0].Name)
	assert.Equal(t, "bool", body[2].Fields[0].Type)
	assert.True(t, body[2].Fields[0].Required)
	assert.Equal(t, "queries", body[3].Name)
	assert.Equal(t, "array", body[3].Type)
	require.Len(t, body[3].Fields, 1)
	assert.Equal(t, "query", body[3].Fields[0].Name)
	assert.True(t, body[3].Fields[0].Required)
}

func TestParseCSVArrayBodyParamMetadata(t *testing.T) {
	yamlSpec := []byte(`
name: csv-array-body
base_url: https://api.example.com
auth:
  type: none
resources:
  messages:
    endpoints:
      send:
        method: POST
        path: /messages
        body:
          - name: attendees
            type: string_csv_array
            item_type: object
            item_template:
              emailAddress:
                address: $value
              type: required
            description: Attendees
`)
	s, err := ParseBytes(yamlSpec)
	require.NoError(t, err)

	param := s.Resources["messages"].Endpoints["send"].Body[0]
	assert.Equal(t, "string_csv_array", param.Type)
	assert.Equal(t, "object", param.ItemType)
	require.IsType(t, map[string]any{}, param.ItemTemplate)
	template := param.ItemTemplate.(map[string]any)
	assert.Equal(t, "required", template["type"])
}

func TestParseCSVArrayObjectTemplateMustBeObject(t *testing.T) {
	yamlSpec := []byte(`
name: csv-array-body
base_url: https://api.example.com
auth:
  type: none
resources:
  messages:
    endpoints:
      send:
        method: POST
        path: /messages
        body:
          - name: attendees
            type: string_csv_array
            item_type: object
            item_template:
              - not
              - an
              - object
`)
	_, err := ParseBytes(yamlSpec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string_csv_array item_type object requires item_template to be an object")
}

func TestParseBodySchemaWrapper(t *testing.T) {
	yamlSpec := []byte(`
name: rich-body-wrapper
base_url: https://api.example.com
auth:
  type: none
resources:
  datasets:
    endpoints:
      execute:
        method: POST
        path: /datasets/{datasetId}/executeQueries
        params:
          - name: datasetId
            type: string
            positional: true
        body:
          schema:
            type: object
            properties:
              queries:
                type: array
                items:
                  type: object
                  properties:
                    query:
                      type: string
`)
	s, err := ParseBytes(yamlSpec)
	require.NoError(t, err)

	endpoint := s.Resources["datasets"].Endpoints["execute"]
	assert.True(t, endpoint.BodySet)
	require.Len(t, endpoint.Body, 1)
	assert.Equal(t, "queries", endpoint.Body[0].Name)
	assert.Equal(t, "array", endpoint.Body[0].Type)
	require.Len(t, endpoint.Body[0].Fields, 1)
	assert.Equal(t, "query", endpoint.Body[0].Fields[0].Name)
}

func TestParseBodyObjectSchemaJSON(t *testing.T) {
	jsonSpec := []byte(`{
  "name": "rich-body-json",
  "base_url": "https://api.example.com",
  "auth": {"type": "none"},
  "resources": {
    "graphql": {
      "endpoints": {
        "execute": {
          "method": "POST",
          "path": "/graphql",
          "body": {
            "properties": {
              "query": {"type": "string", "required": true},
              "variables": {"type": "object"}
            }
          }
        }
      }
    }
  }
}`)
	s, err := ParseBytes(jsonSpec)
	require.NoError(t, err)

	body := s.Resources["graphql"].Endpoints["execute"].Body
	require.Len(t, body, 2)
	assert.Equal(t, "query", body[0].Name)
	assert.True(t, body[0].Required)
	assert.Equal(t, "variables", body[1].Name)
	assert.Equal(t, "object", body[1].Type)
}

func TestParseBodyNullIsEmpty(t *testing.T) {
	yamlSpec := []byte(`
name: null-body
base_url: https://api.example.com
auth:
  type: none
resources:
  queries:
    endpoints:
      yamlNull:
        method: POST
        path: /graphql
        body: ~
`)
	s, err := ParseBytes(yamlSpec)
	require.NoError(t, err)
	assert.Empty(t, s.Resources["queries"].Endpoints["yamlNull"].Body)

	jsonSpec := []byte(`{
  "name": "null-body-json",
  "base_url": "https://api.example.com",
  "auth": {"type": "none"},
  "resources": {
    "queries": {
      "endpoints": {
        "jsonNull": {
          "method": "POST",
          "path": "/graphql",
          "body": null
        }
      }
    }
  }
}`)
	s, err = ParseBytes(jsonSpec)
	require.NoError(t, err)
	assert.Empty(t, s.Resources["queries"].Endpoints["jsonNull"].Body)
}

func TestParseBodyObjectSchemaMalformed(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing properties",
			body: `
          schema:
            type: object`,
			want: "must declare properties",
		},
		{
			name: "non object root",
			body: `
          type: string
          properties:
            query:
              type: string`,
			want: `must be type object with properties, got "string"`,
		},
		{
			name: "non mapping properties",
			body: `
          properties:
            - query`,
			want: "body properties at line",
		},
		{
			name: "non mapping property schema",
			body: `
          properties:
            tags:
              - string`,
			want: `body property "tags"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlSpec := []byte(`
name: malformed-body
base_url: https://api.example.com
auth:
  type: none
resources:
  queries:
    endpoints:
      execute:
        method: POST
        path: /graphql
        body:` + tt.body + `
`)
			_, err := ParseBytes(yamlSpec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "cannot unmarshal")
		})
	}
}

func TestValidateNameRequiresKebabSlug(t *testing.T) {
	baseSpec := func(name string) []byte {
		return []byte(`
name: ` + name + `
base_url: https://api.example.com
auth:
  type: none
resources:
  items:
    endpoints:
      list:
        method: GET
        path: /items
`)
	}

	tests := []struct {
		name    string
		spec    []byte
		wantErr string
	}{
		{
			name:    "spaces",
			spec:    baseSpec("NSE India"),
			wantErr: `spec name must be a kebab-case slug (got "NSE India"); try "nse-india"`,
		},
		{
			name:    "multi-word brand",
			spec:    baseSpec("Google Flights"),
			wantErr: `spec name must be a kebab-case slug (got "Google Flights"); try "google-flights"`,
		},
		{
			name:    "trailing hyphen",
			spec:    baseSpec("google-flights-"),
			wantErr: `spec name must be a kebab-case slug (got "google-flights-"); try "google-flights"`,
		},
		{
			name:    "doubled hyphen",
			spec:    baseSpec("google--flights"),
			wantErr: `spec name must be a kebab-case slug (got "google--flights"); try "google-flights"`,
		},
		{
			name: "valid slug",
			spec: baseSpec("nse-india"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBytes(tt.spec)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParamPublicInputName(t *testing.T) {
	assert.Equal(t, "address", Param{Name: "s", FlagName: "address"}.PublicInputName())
	assert.Equal(t, "store_code", Param{Name: "store_code"}.PublicInputName())
	assert.Equal(t, "id-2", Param{Name: "id", IdentName: "id_2"}.PublicInputName())
	assert.Equal(t, "start-time-2", Param{Name: "StartTime>", IdentName: "StartTime>_2"}.PublicInputName())
}

func TestParamBodyWireName(t *testing.T) {
	assert.Equal(t, "startAfter", Param{Name: "startAfter"}.BodyWireName())
	assert.Equal(t, "searchAfter", Param{Name: "startAfter", BodyName: "searchAfter"}.BodyWireName())
}

func TestValidatePublicParamNames(t *testing.T) {
	cases := []struct {
		name    string
		param   Param
		wantErr string
	}{
		{
			name:    "uppercase flag name",
			param:   Param{Name: "s", FlagName: "Street", Type: "string"},
			wantErr: `flag_name "Street" must be lowercase kebab-case`,
		},
		{
			name:    "underscore alias",
			param:   Param{Name: "s", FlagName: "address", Aliases: []string{"street_address"}, Type: "string"},
			wantErr: `alias "street_address" must be lowercase kebab-case`,
		},
		{
			name:    "alias duplicates public name",
			param:   Param{Name: "s", FlagName: "address", Aliases: []string{"address"}, Type: "string"},
			wantErr: `alias "address" duplicates its public name`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := APISpec{
				Name:    "public-params",
				BaseURL: "https://api.example.com",
				Auth:    AuthConfig{Type: "none"},
				Resources: map[string]Resource{
					"stores": {
						Endpoints: map[string]Endpoint{
							"find": {Method: "GET", Path: "/stores", Params: []Param{tt.param}},
						},
					},
				},
			}
			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseRejectsExplicitEmptyFlagName(t *testing.T) {
	yamlSpec := []byte(`
name: public-params
base_url: https://api.example.com
auth:
  type: none
resources:
  stores:
    endpoints:
      find:
        method: GET
        path: /stores
        params:
          - name: s
            flag_name: ""
            type: string
`)
	_, err := ParseBytes(yamlSpec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flag_name must not be empty")

	jsonSpec := []byte(`{
  "name": "public-params-json",
  "base_url": "https://api.example.com",
  "auth": {"type": "none"},
  "resources": {
    "stores": {
      "endpoints": {
        "find": {
          "method": "GET",
          "path": "/stores",
          "params": [{"name": "s", "flag_name": "", "type": "string"}]
        }
      }
    }
  }
}`)
	_, err = ParseBytes(jsonSpec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flag_name must not be empty")
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

// validateAdditionalAuthHeaders covers six distinct error paths; this table
// hits each one and confirms the happy path still validates.
func TestValidateAdditionalAuthHeadersErrors(t *testing.T) {
	t.Parallel()

	baseSpec := func(auth AuthConfig) APISpec {
		return APISpec{
			Name:    "auth-api",
			BaseURL: "https://api.example.com",
			Auth:    auth,
			Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
			},
		}
	}
	perCall := func(name string) AuthEnvVar {
		return AuthEnvVar{Name: name, Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true}
	}
	tests := []struct {
		name    string
		auth    AuthConfig
		wantErr string
	}{
		{
			name: "happy path: per_call sibling with header validates",
			auth: AuthConfig{
				Type:   "bearer_token",
				Header: "Authorization",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "ST-App-Key", In: "header", EnvVar: perCall("ST_APP_KEY")},
				},
			},
		},
		{
			name: "happy path: per_call sibling with query validates",
			auth: AuthConfig{
				Type:   "api_key",
				Header: "key",
				In:     "query",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "token", In: "query", EnvVar: perCall("TRELLO_TOKEN")},
				},
			},
		},
		{
			name: "missing header",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "", EnvVar: perCall("ST_APP_KEY")},
				},
			},
			wantErr: "auth.additional_headers[0].header is required",
		},
		{
			name: "missing env_var name",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "ST-App-Key", EnvVar: AuthEnvVar{Kind: AuthEnvVarKindPerCall}},
				},
			},
			wantErr: "auth.additional_headers[0].env_var.name is required",
		},
		{
			name: "duplicate header",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "X-Same", EnvVar: perCall("FIRST_KEY")},
					{Header: "X-Same", EnvVar: perCall("SECOND_KEY")},
				},
			},
			wantErr: `auth.additional_headers contains duplicate header "X-Same"`,
		},
		{
			name: "unsupported placement",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "X-Key", In: "cookie", EnvVar: perCall("COOKIE_KEY")},
				},
			},
			wantErr: `auth.additional_headers[0].in must be "header" or "query" (got "cookie")`,
		},
		{
			name: "duplicate env_var name",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "X-First", EnvVar: perCall("SAME_KEY")},
					{Header: "X-Second", EnvVar: perCall("SAME_KEY")},
				},
			},
			wantErr: `auth.additional_headers contains duplicate env_var.name "SAME_KEY"`,
		},
		{
			name: "collision with primary EnvVarSpecs",
			auth: AuthConfig{
				Type:        "bearer_token",
				EnvVarSpecs: []AuthEnvVar{{Name: "SHARED_KEY", Kind: AuthEnvVarKindPerCall, Required: true}},
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "ST-App-Key", EnvVar: perCall("SHARED_KEY")},
				},
			},
			wantErr: `auth.additional_headers[0].env_var.name "SHARED_KEY" collides with env_var_specs`,
		},
		{
			name: "non-per_call kind",
			auth: AuthConfig{
				Type: "bearer_token",
				AdditionalHeaders: []AdditionalAuthHeader{
					{Header: "ST-App-Key", EnvVar: AuthEnvVar{Name: "ST_APP_KEY", Kind: AuthEnvVarKindAuthFlowInput, Required: true}},
				},
			},
			wantErr: `auth.additional_headers[0].env_var.kind must be "per_call" (got "auth_flow_input")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := baseSpec(tt.auth)
			err := candidate.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAuthEnvVarSpecsNormalizeAndValidate(t *testing.T) {
	baseSpec := func(auth AuthConfig) APISpec {
		return APISpec{
			Name:    "auth-api",
			BaseURL: "https://api.example.com",
			Auth:    auth,
			Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
			},
		}
	}

	tests := []struct {
		name              string
		auth              AuthConfig
		wantEnvVars       []string
		wantEnvVarSpecs   []AuthEnvVar
		wantWarning       string
		wantCanonicalName string
	}{
		{
			name: "rich specs back-derive legacy env vars",
			auth: AuthConfig{
				Type: "api_key",
				EnvVarSpecs: []AuthEnvVar{{
					Name:        "TODOIST_API_KEY",
					Kind:        AuthEnvVarKindPerCall,
					Required:    true,
					Sensitive:   true,
					Description: "Todoist API key.",
				}},
			},
			wantEnvVars: []string{"TODOIST_API_KEY"},
			wantEnvVarSpecs: []AuthEnvVar{{
				Name:        "TODOIST_API_KEY",
				Kind:        AuthEnvVarKindPerCall,
				Required:    true,
				Sensitive:   true,
				Description: "Todoist API key.",
			}},
			wantCanonicalName: "TODOIST_API_KEY",
		},
		{
			name:        "legacy env vars lazily derive rich specs",
			auth:        AuthConfig{Type: "api_key", EnvVars: []string{"TODOIST_API_KEY"}},
			wantEnvVars: []string{"TODOIST_API_KEY"},
			wantEnvVarSpecs: []AuthEnvVar{{
				Name:      "TODOIST_API_KEY",
				Kind:      AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
				Inferred:  true,
			}},
			wantCanonicalName: "TODOIST_API_KEY",
		},
		{
			name: "consistent legacy and rich specs keep rich metadata",
			auth: AuthConfig{
				Type:    "api_key",
				EnvVars: []string{"PUBLIC_ACCOUNT_SLUG"},
				EnvVarSpecs: []AuthEnvVar{{
					Name:      "PUBLIC_ACCOUNT_SLUG",
					Kind:      AuthEnvVarKindPerCall,
					Required:  false,
					Sensitive: false,
				}},
			},
			wantEnvVars: []string{"PUBLIC_ACCOUNT_SLUG"},
			wantEnvVarSpecs: []AuthEnvVar{{
				Name:      "PUBLIC_ACCOUNT_SLUG",
				Kind:      AuthEnvVarKindPerCall,
				Required:  false,
				Sensitive: false,
			}},
			wantCanonicalName: "PUBLIC_ACCOUNT_SLUG",
		},
		{
			name: "inconsistent legacy and rich specs warn and rich specs win",
			auth: AuthConfig{
				Type:    "api_key",
				EnvVars: []string{"WRONG_API_KEY"},
				EnvVarSpecs: []AuthEnvVar{{
					Name:      "TODOIST_API_KEY",
					Kind:      AuthEnvVarKindPerCall,
					Required:  true,
					Sensitive: true,
				}},
			},
			wantEnvVars: []string{"TODOIST_API_KEY"},
			wantEnvVarSpecs: []AuthEnvVar{{
				Name:      "TODOIST_API_KEY",
				Kind:      AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			}},
			wantWarning:       "warning: auth env_vars disagree with env_var_specs; using env_var_specs",
			wantCanonicalName: "TODOIST_API_KEY",
		},
		{
			name:              "no-auth empty env vars remains empty",
			auth:              AuthConfig{Type: "none"},
			wantCanonicalName: "",
		},
		{
			name: "or case accepts optional described alternatives",
			auth: AuthConfig{
				Type: "bearer_token",
				EnvVarSpecs: []AuthEnvVar{
					{Name: "SLACK_BOT_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
					{Name: "SLACK_USER_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
				},
			},
			wantEnvVars: []string{"SLACK_BOT_TOKEN", "SLACK_USER_TOKEN"},
			wantEnvVarSpecs: []AuthEnvVar{
				{Name: "SLACK_BOT_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
				{Name: "SLACK_USER_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
			},
			wantCanonicalName: "SLACK_BOT_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := baseSpec(tt.auth)
			stderr := captureWarnings(t, func() {
				require.NoError(t, candidate.Validate())
			})

			assert.Contains(t, stderr, tt.wantWarning)
			assert.Equal(t, tt.wantEnvVars, candidate.Auth.EnvVars)
			assert.Equal(t, tt.wantEnvVarSpecs, candidate.Auth.EnvVarSpecs)
			if tt.wantCanonicalName == "" {
				assert.Nil(t, candidate.Auth.CanonicalEnvVar())
			} else if assert.NotNil(t, candidate.Auth.CanonicalEnvVar()) {
				assert.Equal(t, tt.wantCanonicalName, candidate.Auth.CanonicalEnvVar().Name)
			}
		})
	}
}

func TestAuthEnvVarSpecsRejectDuplicateNames(t *testing.T) {
	s := APISpec{
		Name:    "auth-api",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "api_key",
			EnvVarSpecs: []AuthEnvVar{
				{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	require.ErrorContains(t, s.Validate(), `auth.env_var_specs contains duplicate name "TODOIST_API_KEY"`)
}

func TestAuthEnvVarSpecsRejectIndependentORGroups(t *testing.T) {
	s := APISpec{
		Name:    "auth-api",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer_token",
			EnvVarSpecs: []AuthEnvVar{
				{Name: "FIRST_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SECOND_TOKEN."},
				{Name: "SECOND_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR FIRST_TOKEN."},
				{Name: "THIRD_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR FOURTH_TOKEN."},
				{Name: "FOURTH_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR THIRD_TOKEN."},
			},
		},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	require.ErrorContains(t, s.Validate(), "auth: detected 2+ independent OR-groups in EnvVarSpecs")
}

func TestAuthEnvVarSpecsAcceptNonCrossReferencingORDescriptions(t *testing.T) {
	s := APISpec{
		Name:    "auth-api",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer_token",
			EnvVarSpecs: []AuthEnvVar{
				{Name: "FIRST_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR use OAuth."},
				{Name: "SECOND_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR use dashboard."},
			},
		},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	require.NoError(t, s.Validate())
}

func TestAuthEnvVarSpecsAcceptTransitiveORGroup(t *testing.T) {
	s := APISpec{
		Name:    "auth-api",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer_token",
			EnvVarSpecs: []AuthEnvVar{
				{Name: "FIRST_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR SECOND_TOKEN."},
				{Name: "SECOND_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR THIRD_TOKEN."},
				{Name: "THIRD_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR FOURTH_TOKEN."},
				{Name: "FOURTH_TOKEN", Kind: AuthEnvVarKindPerCall, Sensitive: true, Description: "Set this OR FIRST_TOKEN."},
			},
		},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	require.NoError(t, s.Validate())
}

func TestNormalizeEnvVarSpecsEmptyNameIsIdempotent(t *testing.T) {
	auth := AuthConfig{
		Type:    "api_key",
		EnvVars: []string{"TODOIST_API_KEY"},
		EnvVarSpecs: []AuthEnvVar{
			{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	auth.NormalizeEnvVarSpecs("")
	once := append([]string(nil), auth.EnvVars...)
	auth.NormalizeEnvVarSpecs("")

	assert.Equal(t, once, auth.EnvVars)
}

func TestAuthEnvVarSpecsParseYAML(t *testing.T) {
	parsed, err := ParseBytes([]byte(`
name: todoist
base_url: https://api.todoist.com
auth:
  type: api_key
  header: Authorization
  env_var_specs:
    - name: TODOIST_API_KEY
      kind: per_call
      required: true
      sensitive: true
resources:
  tasks:
    endpoints:
      list:
        method: GET
        path: /tasks
`))
	require.NoError(t, err)

	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, AuthEnvVarKindPerCall, parsed.Auth.EnvVarSpecs[0].Kind)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Required)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
}

func TestCanonicalEnvVarSelection(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
		want string
	}{
		{
			name: "prefers first required per-call entry over first harvested entry",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "SESSION_COOKIE", Kind: AuthEnvVarKindHarvested, Required: true, Sensitive: true},
				{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			}},
			want: "TODOIST_API_KEY",
		},
		{
			name: "uses first required per-call entry in source order",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "FIRST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				{Name: "SECOND_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			}},
			want: "FIRST_API_KEY",
		},
		{
			name: "falls back to lazy-derived legacy env var",
			auth: AuthConfig{EnvVars: []string{"LEGACY_API_KEY"}},
			want: "LEGACY_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.auth.CanonicalEnvVar()
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestIsAuthEnvVarORCase(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
		want bool
	}{
		{
			name: "all required per-call entries are OR case",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "FIRST_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true},
				{Name: "SECOND_API_KEY", Kind: AuthEnvVarKindPerCall, Required: true},
			}},
			want: true,
		},
		{
			name: "legacy env vars list is OR case",
			auth: AuthConfig{EnvVars: []string{"FIRST_API_KEY", "SECOND_API_KEY"}},
			want: true,
		},
		{
			name: "all non-required per-call entries are OR case",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "SLACK_BOT_TOKEN", Kind: AuthEnvVarKindPerCall},
				{Name: "SLACK_USER_TOKEN", Kind: AuthEnvVarKindPerCall},
			}},
			want: true,
		},
		{
			name: "mixed kinds are not OR case",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "SESSION_COOKIE", Kind: AuthEnvVarKindHarvested},
				{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall},
			}},
			want: false,
		},
		{
			name: "single entry is not OR case",
			auth: AuthConfig{EnvVarSpecs: []AuthEnvVar{
				{Name: "TODOIST_API_KEY", Kind: AuthEnvVarKindPerCall},
			}},
			want: false,
		},
		{
			name: "empty specs are not OR case",
			auth: AuthConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.auth.IsAuthEnvVarORCase())
		})
	}
}

func TestTierAuthEnvVarSpecsNormalizeAndValidate(t *testing.T) {
	s := APISpec{
		Name:    "tier-api",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer_token",
			EnvVarSpecs: []AuthEnvVar{{
				Name:      "GLOBAL_TOKEN",
				Kind:      AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			}},
		},
		TierRouting: TierRoutingConfig{
			DefaultTier: "paid",
			Tiers: map[string]TierConfig{
				"paid": {
					BaseURL: "https://api.example.com/paid",
					Auth: AuthConfig{
						Type:    "api_key",
						Header:  "x-api-key",
						EnvVars: []string{"PAID_API_KEY"},
					},
				},
			},
		},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items", Tier: "paid"}}},
		},
	}

	require.NoError(t, s.Validate())
	assert.Equal(t, []string{"GLOBAL_TOKEN"}, s.Auth.EnvVars)
	assert.Equal(t, []string{"PAID_API_KEY"}, s.TierRouting.Tiers["paid"].Auth.EnvVars)
	require.Len(t, s.TierRouting.Tiers["paid"].Auth.EnvVarSpecs, 1)
	assert.Equal(t, AuthEnvVarKindPerCall, s.TierRouting.Tiers["paid"].Auth.EnvVarSpecs[0].Kind)
	assert.True(t, s.TierRouting.Tiers["paid"].Auth.EnvVarSpecs[0].Required)
}

// TestThrottleShapeShopifyValue pins the wire value of ThrottleShapeShopify
// to "shopify" because the graphql_client.go.tmpl template gates its
// Shopify parser block on the literal string "shopify" (Go templates can't
// import the constant). A silent rename of the Go constant would leave the
// template gate stranded; this test surfaces the mismatch immediately.
func TestThrottleShapeShopifyValue(t *testing.T) {
	assert.Equal(t, "shopify", string(ThrottleShapeShopify),
		"changing this value requires updating graphql_client.go.tmpl's gate to match")
}

func TestBearerRefreshValidate(t *testing.T) {
	base := APISpec{
		Name:    "browser-api",
		BaseURL: "https://api.example.com",
		Auth:    AuthConfig{Type: "bearer_token"},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	valid := base
	valid.BearerRefresh = BearerRefreshConfig{
		BundleURL: "https://example.com/main.js",
		Pattern:   `"(AAAAAAAA[^"]+)"`,
	}
	require.NoError(t, valid.Validate())

	missingPattern := base
	missingPattern.BearerRefresh = BearerRefreshConfig{BundleURL: "https://example.com/main.js"}
	require.ErrorContains(t, missingPattern.Validate(), "bearer_refresh.pattern is required")

	invalidURL := base
	invalidURL.BearerRefresh = BearerRefreshConfig{BundleURL: "http://example.com/main.js", Pattern: `AAAA`}
	require.ErrorContains(t, invalidURL.Validate(), `bearer_refresh.bundle_url must start with "https://"`)

	invalidPattern := base
	invalidPattern.BearerRefresh = BearerRefreshConfig{BundleURL: "https://example.com/main.js", Pattern: `[`}
	require.ErrorContains(t, invalidPattern.Validate(), "bearer_refresh.pattern is not a valid regexp")

	for _, tc := range []struct {
		name    string
		mutate  func(*APISpec)
		wantErr string
	}{
		{
			name: "api key auth",
			mutate: func(s *APISpec) {
				s.Auth.Type = "api_key"
			},
			wantErr: `bearer_refresh requires auth.type "bearer_token"`,
		},
		{
			name: "no auth",
			mutate: func(s *APISpec) {
				s.Auth.Type = "none"
			},
			wantErr: `bearer_refresh requires auth.type "bearer_token"`,
		},
		{
			name: "cookie auth",
			mutate: func(s *APISpec) {
				s.Auth.Type = "cookie"
			},
			wantErr: `bearer_refresh requires auth.type "bearer_token"`,
		},
		{
			name: "composed auth",
			mutate: func(s *APISpec) {
				s.Auth.Type = "composed"
			},
			wantErr: `bearer_refresh requires auth.type "bearer_token"`,
		},
		{
			name: "client credentials",
			mutate: func(s *APISpec) {
				s.Auth.OAuth2Grant = OAuth2GrantClientCredentials
			},
			wantErr: `bearer_refresh is incompatible with auth.oauth2_grant "client_credentials"`,
		},
		{
			name: "tier routing",
			mutate: func(s *APISpec) {
				s.TierRouting = TierRoutingConfig{
					DefaultTier: "free",
					Tiers: map[string]TierConfig{
						"free": {Auth: AuthConfig{Type: "none"}},
					},
				}
			},
			wantErr: "bearer_refresh is incompatible with tier_routing auth",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			tc.mutate(&candidate)
			require.ErrorContains(t, candidate.Validate(), tc.wantErr)
		})
	}
}

// TestThrottlingValidate guards the named-adapter contract: enabling
// throttling without a Shape (or with an unrecognized one) must fail at
// spec-load time, not silently emit Shopify-shape parser code for an API
// that isn't Shopify. The off case stays unconditional — specs that don't
// use throttling never have to think about Shape.
func TestThrottlingValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ThrottlingConfig
		wantErr string
	}{
		{
			name: "off is always valid",
			cfg:  ThrottlingConfig{},
		},
		{
			name: "off with stray shape is still valid",
			cfg:  ThrottlingConfig{Shape: ThrottleShapeShopify},
		},
		{
			name: "shopify shape is valid when enabled",
			cfg:  ThrottlingConfig{Enabled: true, Shape: ThrottleShapeShopify},
		},
		{
			name:    "enabled without shape is rejected",
			cfg:     ThrottlingConfig{Enabled: true},
			wantErr: "throttling.shape is required",
		},
		{
			name:    "unknown shape is rejected",
			cfg:     ThrottlingConfig{Enabled: true, Shape: "github-graphql"},
			wantErr: "not recognized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateThrottling(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAuthPrefixValidate(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantErr string
	}{
		{name: "empty is valid (defaults to Bearer)", prefix: ""},
		{name: "Bearer is valid", prefix: "Bearer"},
		{name: "Token is valid", prefix: "Token"},
		{name: "lowercase token is valid", prefix: "token"},
		{name: "PRIVATE-TOKEN is valid (hyphen is a token char)", prefix: "PRIVATE-TOKEN"},
		{name: "embedded quote is rejected", prefix: `Token"`, wantErr: "separator character"},
		{name: "backslash is rejected", prefix: `Token\`, wantErr: "separator character"},
		{name: "carriage return is rejected", prefix: "Token\r", wantErr: "non-printable"},
		{name: "newline is rejected", prefix: "Token\n", wantErr: "non-printable"},
		{name: "space is rejected", prefix: "Token Foo", wantErr: "non-printable"},
		{name: "non-ASCII is rejected", prefix: "Tøken", wantErr: "non-ASCII"},
		{name: "over-long prefix is rejected", prefix: strings.Repeat("A", 33), wantErr: "32-character cap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuthPrefix(AuthConfig{Prefix: tt.prefix})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAuthSubtypeValidate(t *testing.T) {
	tests := []struct {
		name    string
		auth    AuthConfig
		wantErr string
	}{
		{name: "empty subtype is valid", auth: AuthConfig{Type: "bearer_token"}},
		{
			name: "auth0_spa_in_memory with bearer_token is valid",
			auth: AuthConfig{Type: "bearer_token", Subtype: AuthSubtypeAuth0SPAInMemory},
		},
		{
			name: "auth0_spa_in_memory with empty Type is valid",
			auth: AuthConfig{Subtype: AuthSubtypeAuth0SPAInMemory},
		},
		{
			name:    "auth0_spa_in_memory with api_key is rejected",
			auth:    AuthConfig{Type: "api_key", Subtype: AuthSubtypeAuth0SPAInMemory},
			wantErr: `requires auth.type "bearer_token"`,
		},
		{
			name:    "unknown subtype is rejected",
			auth:    AuthConfig{Type: "bearer_token", Subtype: "auth0_spa_localstorage"},
			wantErr: "is not recognized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuthSubtype(tt.auth)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAPISpecValidate_RejectsBadAuthPrefix(t *testing.T) {
	build := func(prefix string) APISpec {
		return APISpec{
			Name:    "prefix-validate",
			BaseURL: "https://api.example.com",
			Auth: AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Prefix:  prefix,
				EnvVars: []string{"PREFIX_VALIDATE_TOKEN"},
			},
			Resources: map[string]Resource{
				"items": {
					Endpoints: map[string]Endpoint{
						"list": {Method: "GET", Path: "/items"},
					},
				},
			},
		}
	}

	t.Run("valid prefix passes Validate()", func(t *testing.T) {
		s := build("Token")
		require.NoError(t, s.Validate())
	})

	t.Run("embedded quote is rejected at the APISpec level", func(t *testing.T) {
		s := build(`Token"`)
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth.prefix")
	})
}

func TestAuthConfigHeaderPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "empty defaults to Bearer", prefix: "", want: "Bearer"},
		{name: "whitespace-only defaults to Bearer", prefix: "   ", want: "Bearer"},
		{name: "Token is preserved", prefix: "Token", want: "Token"},
		{name: "surrounding whitespace is trimmed", prefix: "  Token  ", want: "Token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AuthConfig{Prefix: tt.prefix}.HeaderPrefix()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOAuth2GrantValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AuthConfig
		wantErr string
	}{
		{name: "empty is valid (defaults to authorization_code)", cfg: AuthConfig{}},
		{name: "explicit authorization_code is valid", cfg: AuthConfig{OAuth2Grant: OAuth2GrantAuthorizationCode}},
		{name: "client_credentials is valid", cfg: AuthConfig{OAuth2Grant: OAuth2GrantClientCredentials}},
		{
			name: "device_code is valid with required endpoints",
			cfg: AuthConfig{
				OAuth2Grant:            OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "https://login.example.com/device",
				TokenURL:               "https://login.example.com/token",
			},
		},
		{
			name: "device_code allows localhost http endpoints",
			cfg: AuthConfig{
				OAuth2Grant:            OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "http://localhost:8080/device",
				TokenURL:               "http://127.0.0.1:8080/token",
			},
		},
		{
			// Cross-checking against AuthConfig.Type is intentionally skipped
			// (the field is meaningless for non-oauth2 types but harmless to
			// declare); validation should accept this combo.
			name: "set on a non-oauth2 type is accepted (ignored at template time)",
			cfg:  AuthConfig{Type: "api_key", OAuth2Grant: OAuth2GrantClientCredentials},
		},
		{
			name:    "device_code requires device endpoint",
			cfg:     AuthConfig{OAuth2Grant: OAuth2GrantDeviceCode, TokenURL: "https://login.example.com/token"},
			wantErr: `auth.device_authorization_url is required`,
		},
		{
			name:    "device_code requires token endpoint",
			cfg:     AuthConfig{OAuth2Grant: OAuth2GrantDeviceCode, DeviceAuthorizationURL: "https://login.example.com/device"},
			wantErr: `auth.token_url is required`,
		},
		{
			name: "device_code rejects plain http device endpoint",
			cfg: AuthConfig{
				OAuth2Grant:            OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "http://login.example.com/device",
				TokenURL:               "https://login.example.com/token",
			},
			wantErr: `auth.device_authorization_url uses http://`,
		},
		{
			name: "device_code rejects plain http token endpoint",
			cfg: AuthConfig{
				OAuth2Grant:            OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "https://login.example.com/device",
				TokenURL:               "http://login.example.com/token",
			},
			wantErr: `auth.token_url uses http://`,
		},
		{
			name:    "typo (e.g. authorisation) is rejected",
			cfg:     AuthConfig{OAuth2Grant: "authorisation_code"},
			wantErr: "not recognized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOAuth2Grant(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSessionHandshakeValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AuthConfig
		wantErr string
	}{
		{name: "non-session_handshake type is unaffected", cfg: AuthConfig{Type: "api_key"}},
		{
			name: "valid session_handshake config",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
				TokenParamIn:    "query",
			},
		},
		{
			name: "TokenParamIn empty is accepted (template defaults to query)",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
			},
		},
		{
			name: "header attachment is accepted",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
				TokenParamIn:    "header",
			},
		},
		{
			name: "missing SessionTokenURL is rejected",
			cfg: AuthConfig{
				Type:           "session_handshake",
				TokenParamName: "crumb",
			},
			wantErr: "auth.session_token_url is required",
		},
		{
			name: "missing TokenParamName is rejected (would emit q.Set(\"\", token))",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
			},
			wantErr: "auth.token_param_name is required",
		},
		{
			name: "title-cased TokenParamIn is rejected (template byte-compares)",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
				TokenParamIn:    "Header",
			},
			wantErr: `auth.token_param_in "Header" is not recognized`,
		},
		{
			name: "uppercase TokenParamIn is rejected",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
				TokenParamIn:    "QUERY",
			},
			wantErr: "not recognized",
		},
		{
			name: "unknown TokenParamIn value is rejected",
			cfg: AuthConfig{
				Type:            "session_handshake",
				SessionTokenURL: "https://api.example.com/token",
				TokenParamName:  "crumb",
				TokenParamIn:    "cookie",
			},
			wantErr: "not recognized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSessionHandshake(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestEffectiveOAuth2Grant(t *testing.T) {
	tests := []struct {
		name string
		cfg  AuthConfig
		want string
	}{
		{name: "empty defaults to authorization_code", cfg: AuthConfig{}, want: OAuth2GrantAuthorizationCode},
		{name: "whitespace-only also defaults", cfg: AuthConfig{OAuth2Grant: "   "}, want: OAuth2GrantAuthorizationCode},
		{name: "explicit authorization_code round-trips", cfg: AuthConfig{OAuth2Grant: OAuth2GrantAuthorizationCode}, want: OAuth2GrantAuthorizationCode},
		{name: "client_credentials round-trips", cfg: AuthConfig{OAuth2Grant: OAuth2GrantClientCredentials}, want: OAuth2GrantClientCredentials},
		{name: "device_code round-trips", cfg: AuthConfig{OAuth2Grant: OAuth2GrantDeviceCode}, want: OAuth2GrantDeviceCode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.EffectiveOAuth2Grant())
		})
	}
}

func TestParseRefreshTokenMechanism(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ParsedRefreshTokenMechanism
	}{
		{name: "empty", raw: "", want: ParsedRefreshTokenMechanism{}},
		{name: "whitespace only", raw: "   ", want: ParsedRefreshTokenMechanism{}},
		{name: "scope offline", raw: "scope:offline", want: ParsedRefreshTokenMechanism{Kind: RefreshTokenMechanismKindScope, Scope: "offline"}},
		{name: "scope offline.access", raw: "scope:offline.access", want: ParsedRefreshTokenMechanism{Kind: RefreshTokenMechanismKindScope, Scope: "offline.access"}},
		{name: "scope offline_access", raw: "scope:offline_access", want: ParsedRefreshTokenMechanism{Kind: RefreshTokenMechanismKindScope, Scope: "offline_access"}},
		{name: "query access_type", raw: "query:access_type=offline", want: ParsedRefreshTokenMechanism{Kind: RefreshTokenMechanismKindQuery, Key: "access_type", Value: "offline"}},
		{name: "query empty key rejected", raw: "query:=offline", want: ParsedRefreshTokenMechanism{}},
		{name: "query empty value rejected", raw: "query:access_type=", want: ParsedRefreshTokenMechanism{}},
		{name: "query missing equals rejected", raw: "query:access_type", want: ParsedRefreshTokenMechanism{}},
		{name: "unknown prefix rejected", raw: "header:Foo=bar", want: ParsedRefreshTokenMechanism{}},
		{name: "missing colon rejected", raw: "scope offline", want: ParsedRefreshTokenMechanism{}},
		{name: "case-sensitive prefix rejected", raw: "Scope:offline", want: ParsedRefreshTokenMechanism{}},
		{name: "reserved key state rejected", raw: "query:state=foo", want: ParsedRefreshTokenMechanism{}},
		{name: "reserved key client_id rejected", raw: "query:client_id=foo", want: ParsedRefreshTokenMechanism{}},
		{name: "reserved key redirect_uri rejected", raw: "query:redirect_uri=foo", want: ParsedRefreshTokenMechanism{}},
		{name: "reserved key response_type rejected", raw: "query:response_type=token", want: ParsedRefreshTokenMechanism{}},
		{name: "reserved key scope rejected", raw: "query:scope=offline", want: ParsedRefreshTokenMechanism{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AuthConfig{RefreshTokenMechanism: tt.raw}
			assert.Equal(t, tt.want, cfg.ParseRefreshTokenMechanism())
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

func TestParseLearnConfig(t *testing.T) {
	t.Run("happy path: full learn block parses and validates", func(t *testing.T) {
		s, err := Parse("testdata/learn_example.yaml")
		require.NoError(t, err)
		require.NoError(t, s.Validate())
		assert.True(t, s.Learn.Enabled)
		require.Len(t, s.Learn.TickerPatterns, 2)
		// Every declared pattern must compile as a Go regexp.
		for i, pattern := range s.Learn.TickerPatterns {
			_, compileErr := regexp.Compile(pattern)
			require.NoErrorf(t, compileErr, "ticker_patterns[%d] = %q", i, pattern)
		}
		assert.Equal(t, []string{"the", "of", "and"}, s.Learn.Stopwords)
		require.Contains(t, s.Learn.EntityLookupSeeds, "country")
		require.Contains(t, s.Learn.EntityLookupSeeds, "team")
		country := s.Learn.EntityLookupSeeds["country"]
		require.Len(t, country, 2)
		assert.Equal(t, "USA", country[0].Canonical)
		assert.Equal(t, []string{"united states", "america", "us"}, country[0].Aliases)
		assert.Equal(t, "Portugal", country[1].Canonical)
	})

	t.Run("empty block: learn: {} parses with Enabled false and validates", func(t *testing.T) {
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
learn: {}
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		require.NoError(t, s.Validate())
		assert.False(t, s.Learn.Enabled)
		assert.Empty(t, s.Learn.TickerPatterns)
		assert.Empty(t, s.Learn.Stopwords)
		assert.Empty(t, s.Learn.EntityLookupSeeds)
	})

	t.Run("absent block: omitting learn yields zero-value LearnConfig", func(t *testing.T) {
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
		require.NoError(t, s.Validate())
		assert.Equal(t, LearnConfig{}, s.Learn)
	})

	t.Run("invalid ticker regex returns validation error naming pattern and regex error", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled:        true,
				TickerPatterns: []string{"[unclosed"},
			},
		}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ticker_patterns[0]")
		assert.Contains(t, err.Error(), "not a valid Go regexp")
	})

	t.Run("bad seed kind with whitespace rejected", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled: true,
				EntityLookupSeeds: map[string][]LookupSeed{
					"bad name with spaces": {{Canonical: "X"}},
				},
			},
		}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity_lookup_seeds")
		assert.Contains(t, err.Error(), "bad name with spaces")
	})

	t.Run("bad seed kind with hyphen rejected (only underscore allowed)", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled: true,
				EntityLookupSeeds: map[string][]LookupSeed{
					"bad-kind": {{Canonical: "X"}},
				},
			},
		}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad-kind")
	})

	t.Run("empty canonical rejected", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled: true,
				EntityLookupSeeds: map[string][]LookupSeed{
					"country": {{Canonical: ""}},
				},
			},
		}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canonical must not be empty")
	})

	t.Run("duplicate canonical within same kind rejected", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled: true,
				EntityLookupSeeds: map[string][]LookupSeed{
					"country": {
						{Canonical: "USA"},
						{Canonical: "USA"},
					},
				},
			},
		}
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "appears more than once")
	})

	t.Run("whitespace stopwords filtered, only meaningful entries retained", func(t *testing.T) {
		s := APISpec{
			Name:      "demo",
			BaseURL:   "http://x",
			Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
			Learn: LearnConfig{
				Enabled:   true,
				Stopwords: []string{" ", "  ", "valid", "\t", "also-valid"},
			},
		}
		require.NoError(t, s.Validate())
		assert.Equal(t, []string{"valid", "also-valid"}, s.Learn.Stopwords)
	})
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
	assert.True(t, MCPConfig{}.EndpointMirrorsVisible())
	assert.True(t, MCPConfig{EndpointTools: "visible"}.EndpointMirrorsVisible())
	assert.False(t, MCPConfig{EndpointTools: "hidden"}.EndpointMirrorsVisible())
	assert.False(t, MCPConfig{EndpointTools: "visible", Orchestration: "code"}.EndpointMirrorsVisible())
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

// TestEffectiveMCPTransportsSmallAPIDefault locks the issue #1603 contract:
// when mcp.transport is unset and the typed-endpoint surface is small, the
// resolved transport list adds http alongside stdio so the same binary can
// reach cloud-hosted agents. Explicit transport lists (including ["stdio"])
// bypass the default and are honored as-is.
func TestEffectiveMCPTransportsSmallAPIDefault(t *testing.T) {
	t.Parallel()

	mkSpec := func(endpoints int, transport []string) *APISpec {
		s := &APISpec{
			Name:      "demo",
			BaseURL:   "https://api.example.com",
			Auth:      AuthConfig{Type: "none"},
			Resources: map[string]Resource{},
			MCP:       MCPConfig{Transport: transport},
		}
		r := Resource{Endpoints: map[string]Endpoint{}}
		for i := range endpoints {
			r.Endpoints[fmt.Sprintf("get_%d", i)] = Endpoint{Method: "GET", Path: fmt.Sprintf("/items/%d", i)}
		}
		s.Resources["items"] = r
		return s
	}

	tests := []struct {
		name      string
		endpoints int
		transport []string
		want      []string
		wantHTTP  bool
	}{
		{name: "small API empty transport gets stdio+http", endpoints: 6, transport: nil, want: []string{"stdio", "http"}, wantHTTP: true},
		{name: "boundary at threshold still gets http", endpoints: DefaultRemoteTransportEndpointThreshold, transport: nil, want: []string{"stdio", "http"}, wantHTTP: true},
		{name: "above threshold stays stdio-only", endpoints: DefaultRemoteTransportEndpointThreshold + 1, transport: nil, want: []string{"stdio"}, wantHTTP: false},
		{name: "explicit stdio is honored even at small scale", endpoints: 6, transport: []string{"stdio"}, want: []string{"stdio"}, wantHTTP: false},
		{name: "explicit stdio,http passes through", endpoints: 6, transport: []string{"stdio", "http"}, want: []string{"stdio", "http"}, wantHTTP: true},
		{name: "explicit http-only passes through", endpoints: 100, transport: []string{"http"}, want: []string{"http"}, wantHTTP: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mkSpec(tt.endpoints, tt.transport)
			assert.Equal(t, tt.want, s.EffectiveMCPTransports())
			assert.Equal(t, tt.wantHTTP, s.HasMCPTransport("http"))
			// Case-insensitive lookup mirrors MCPConfig.HasTransport.
			assert.Equal(t, tt.wantHTTP, s.HasMCPTransport("HTTP"))
		})
	}

	// Nil receiver is safe and returns the stdio fallback.
	var nilSpec *APISpec
	assert.Equal(t, []string{"stdio"}, nilSpec.EffectiveMCPTransports())
	assert.False(t, nilSpec.HasMCPTransport("http"))

	// MCPConfig.EffectiveTransports stays unconditioned on endpoint count;
	// it's still the right helper for spec validation and mcp_audit.
	assert.Equal(t, []string{"stdio"}, MCPConfig{}.EffectiveTransports())
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
	assert.Equal(t, HTTPTransportBrowserChromeH2, sniffed.EffectiveHTTPTransport())
	require.NoError(t, sniffed.Validate())

	browserHTTP := base
	browserHTTP.HTTPTransport = HTTPTransportBrowserHTTP
	assert.Equal(t, HTTPTransportBrowserHTTP, browserHTTP.EffectiveHTTPTransport())
	assert.False(t, browserHTTP.UsesBrowserHTTPTransport())
	assert.True(t, browserHTTP.UsesHTTP2DisabledTransport())
	assert.False(t, browserHTTP.UsesBrowserManagedUserAgent())
	require.NoError(t, browserHTTP.Validate())

	community := base
	community.SpecSource = "community"
	assert.Equal(t, HTTPTransportBrowserChromeH2, community.EffectiveHTTPTransport())

	browserChrome := base
	browserChrome.HTTPTransport = HTTPTransportBrowserChrome
	assert.Equal(t, HTTPTransportBrowserChrome, browserChrome.EffectiveHTTPTransport())
	assert.True(t, browserChrome.UsesBrowserHTTPTransport())
	assert.False(t, browserChrome.UsesBrowserHTTP2Transport())
	assert.False(t, browserChrome.UsesBrowserHTTP3Transport())
	require.NoError(t, browserChrome.Validate())

	h2 := base
	h2.HTTPTransport = HTTPTransportBrowserChromeH2
	assert.Equal(t, HTTPTransportBrowserChromeH2, h2.EffectiveHTTPTransport())
	assert.True(t, h2.UsesBrowserHTTPTransport())
	assert.True(t, h2.UsesBrowserHTTP2Transport())
	assert.False(t, h2.UsesBrowserHTTP3Transport())
	assert.True(t, h2.UsesBrowserManagedUserAgent())
	require.NoError(t, h2.Validate())

	h3 := base
	h3.HTTPTransport = HTTPTransportBrowserChromeH3
	assert.Equal(t, HTTPTransportBrowserChromeH3, h3.EffectiveHTTPTransport())
	assert.True(t, h3.UsesBrowserHTTPTransport())
	assert.True(t, h3.UsesBrowserHTTP3Transport())
	assert.False(t, h3.UsesBrowserHTTP2Transport())
	assert.True(t, h3.UsesBrowserManagedUserAgent())
	require.NoError(t, h3.Validate())

	override := sniffed
	override.HTTPTransport = HTTPTransportStandard
	assert.Equal(t, HTTPTransportStandard, override.EffectiveHTTPTransport())
	require.NoError(t, override.Validate())

	cookieHTML := base
	cookieHTML.Auth.Type = "cookie"
	cookieHTML.Resources = map[string]Resource{
		"pages": {
			Endpoints: map[string]Endpoint{
				"show": {Method: "GET", Path: "/page", ResponseFormat: ResponseFormatHTML},
			},
		},
	}
	assert.Equal(t, HTTPTransportBrowserChromeH2, cookieHTML.EffectiveHTTPTransport())

	composedHTML := cookieHTML
	composedHTML.Auth.Type = "composed"
	assert.Equal(t, HTTPTransportBrowserChromeH2, composedHTML.EffectiveHTTPTransport())

	jsonCookie := cookieHTML
	jsonCookie.Resources = map[string]Resource{
		"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
	}
	assert.Equal(t, HTTPTransportStandard, jsonCookie.EffectiveHTTPTransport())

	cookieHTMLOverride := cookieHTML
	cookieHTMLOverride.HTTPTransport = HTTPTransportStandard
	assert.Equal(t, HTTPTransportStandard, cookieHTMLOverride.EffectiveHTTPTransport())

	runtime := base
	runtime.HTTPTransport = "browser-runtime"
	assert.Equal(t, HTTPTransportStandard, runtime.EffectiveHTTPTransport())
	assert.False(t, runtime.UsesBrowserManagedUserAgent())
	require.ErrorContains(t, runtime.Validate(), "http_transport must be one of")

	requiredUA := base
	requiredUA.RequiredHeaders = []RequiredHeader{{Name: "user-agent", Value: "Mozilla/5.0"}}
	assert.True(t, requiredUA.HasRequiredHeader("User-Agent"))
	assert.False(t, requiredUA.HasRequiredHeader("Referer"))

	invalid := base
	invalid.HTTPTransport = "lynx"
	require.ErrorContains(t, invalid.Validate(), "http_transport must be one of")
}

func TestRateClassValidationAndSyncConcurrencyDefaults(t *testing.T) {
	t.Parallel()

	base := APISpec{
		Name:    "demo",
		BaseURL: "http://x",
		Auth:    AuthConfig{Type: "none"},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}

	cases := []struct {
		name      string
		rateClass string
		want      int
	}{
		{name: "absent", want: 4},
		{name: "per-second", rateClass: RateClassPerSecond, want: 4},
		{name: "unlimited", rateClass: RateClassUnlimited, want: 4},
		{name: "daily", rateClass: RateClassDaily, want: 1},
		{name: "monthly", rateClass: RateClassMonthly, want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := base
			s.RateClass = tc.rateClass
			require.NoError(t, s.Validate())
			assert.Equal(t, tc.want, s.SyncDefaultConcurrency())
		})
	}

	invalid := base
	invalid.RateClass = "weekly"
	require.ErrorContains(t, invalid.Validate(), "rate_class must be one of")
}

func TestUsesBrowserLikeUserAgent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec *APISpec
		want bool
	}{
		{
			name: "documented JSON API stays script-shaped",
			spec: &APISpec{
				Name:    "stripe",
				BaseURL: "https://api.stripe.com",
				Auth:    AuthConfig{Type: "bearer"},
			},
			want: false,
		},
		{
			name: "kind: synthetic flips to browser-shaped",
			spec: &APISpec{
				Name:    "bbquality",
				BaseURL: "https://bbquality.nl",
				Kind:    KindSynthetic,
				Auth:    AuthConfig{Type: "bearer"},
			},
			want: true,
		},
		{
			name: "cookie auth flips to browser-shaped",
			spec: &APISpec{
				Name:    "marktplaats",
				BaseURL: "https://www.marktplaats.nl",
				Auth:    AuthConfig{Type: "cookie"},
			},
			want: true,
		},
		{
			name: "composed auth flips to browser-shaped",
			spec: &APISpec{
				Name:    "picnic",
				BaseURL: "https://storefront-prod.nl.picnicinternational.com",
				Auth:    AuthConfig{Type: "composed"},
			},
			want: true,
		},
		{
			name: "session_handshake auth flips to browser-shaped",
			spec: &APISpec{
				Name:    "openart",
				BaseURL: "https://openart.ai",
				Auth:    AuthConfig{Type: "session_handshake"},
			},
			want: true,
		},
		{
			name: "auth.type casing is normalized",
			spec: &APISpec{
				Name:    "cookieUpper",
				BaseURL: "https://example.com",
				Auth:    AuthConfig{Type: "  Cookie  "},
			},
			want: true,
		},
		{
			// Nil spec must reach the nil-receiver guard; dispatching via
			// a typed pointer (not a name-string comparison) ensures the
			// guard stays under test even if the case name is renamed.
			name: "nil spec is safe and returns false",
			spec: nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.spec.UsesBrowserLikeUserAgent())
		})
	}
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

func TestEffectiveDataSourceStrategy(t *testing.T) {
	t.Parallel()

	resource := Resource{DataSourceStrategy: DataSourceStrategyLocal}
	endpoint := Endpoint{}
	assert.Equal(t, DataSourceStrategyLocal, EffectiveDataSourceStrategy(resource, endpoint))

	endpoint.DataSourceStrategy = DataSourceStrategyLive
	assert.Equal(t, DataSourceStrategyLive, EffectiveDataSourceStrategy(resource, endpoint))

	assert.Equal(t, DataSourceStrategyAuto, EffectiveDataSourceStrategy(Resource{}, Endpoint{}))
}

func TestValidateDataSourceStrategy(t *testing.T) {
	t.Parallel()

	base := APISpec{
		Name:    "strategy",
		Version: "1.0",
		BaseURL: "https://api.example.com",
		Auth:    AuthConfig{Type: "none"},
		Config:  ConfigSpec{Format: "toml", Path: "~/.config/strategy/config.toml"},
		Resources: map[string]Resource{
			"items": {
				DataSourceStrategy: DataSourceStrategyAuto,
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/items", DataSourceStrategy: DataSourceStrategyLocal},
				},
			},
		},
	}
	require.NoError(t, base.Validate())

	bad := base
	items := bad.Resources["items"]
	endpoint := items.Endpoints["list"]
	endpoint.DataSourceStrategy = "remote"
	items.Endpoints["list"] = endpoint
	bad.Resources["items"] = items
	require.ErrorContains(t, bad.Validate(), "data_source_strategy")
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

	t.Run("auth resource is allowed when no auth command is emitted", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: none
resources:
  auth:
    description: Public auth helpers
    endpoints:
      check_email:
        method: GET
        path: /api/auth/check-email
        description: Check email
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
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
		mustReserve := []string{"feedback", "doctor", "auth", "helpers", "agent_context", "profile", "deliver", "which", "sync", "tail", "search", "client", "cache", "export", "import", "refresh_bearer"}
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

	t.Run("health resource passes until the generator emits a health command", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: none
resources:
  health:
    description: API health
    endpoints:
      get:
        method: GET
        path: /health
        description: Health
`
		_, err := ParseBytes([]byte(input))
		require.NoError(t, err)
	})

	t.Run("login resource is rejected for oauth2 auth-code CLIs", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: oauth2
  authorization_url: https://auth.example.com/oauth/authorize
  token_url: https://auth.example.com/oauth/token
resources:
  login:
    description: API login endpoint
    endpoints:
      create:
        method: POST
        path: /login
        description: Log in
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"login"`)
		assert.Contains(t, err.Error(), "shadow framework cobra command")
	})

	t.Run("login resource is rejected for bearer auth-code CLIs", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  authorization_url: https://auth.example.com/oauth/authorize
  token_url: https://auth.example.com/oauth/token
resources:
  login:
    description: API login endpoint
    endpoints:
      create:
        method: POST
        path: /login
        description: Log in
`
		_, err := ParseBytes([]byte(input))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"login"`)
		assert.Contains(t, err.Error(), "shadow framework cobra command")
	})

	t.Run("login resource passes for non-oauth CLIs", func(t *testing.T) {
		t.Parallel()
		input := `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
  login:
    description: API login endpoint
    endpoints:
      create:
        method: POST
        path: /login
        description: Log in
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

func TestValidateRejectsAbsoluteEndpointPathWithProxyEnvelope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*APISpec)
	}{
		{
			name: "top-level endpoint",
			mutate: func(s *APISpec) {
				items := s.Resources["items"]
				items.Endpoints["list"] = Endpoint{
					Method:      "GET",
					Path:        "https://other.example.com/items",
					Description: "List",
				}
				s.Resources["items"] = items
			},
		},
		{
			name: "subresource endpoint",
			mutate: func(s *APISpec) {
				items := s.Resources["items"]
				items.SubResources = map[string]Resource{
					"children": {
						Endpoints: map[string]Endpoint{
							"list": {
								Method:      "GET",
								Path:        "https://children.example.com/items",
								Description: "List children",
							},
						},
					},
				}
				s.Resources["items"] = items
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &APISpec{
				Name:          "proxytest",
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
			}
			tc.mutate(s)

			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "proxy-envelope")
			assert.Contains(t, err.Error(), "absolute endpoint paths")
		})
	}
}

func TestValidateRejectsEndpointBaseURLWithAbsoluteEndpointPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*APISpec)
	}{
		{
			name: "top-level endpoint",
			mutate: func(s *APISpec) {
				items := s.Resources["items"]
				items.Endpoints["list"] = Endpoint{
					Method:      "GET",
					Path:        "https://absolute.example.com/items",
					BaseURL:     "https://override.example.com/v1",
					Description: "List",
				}
				s.Resources["items"] = items
			},
		},
		{
			name: "subresource endpoint",
			mutate: func(s *APISpec) {
				items := s.Resources["items"]
				items.SubResources = map[string]Resource{
					"children": {
						Endpoints: map[string]Endpoint{
							"list": {
								Method:      "GET",
								Path:        "https://children.example.com/items",
								BaseURL:     "https://override.example.com/v1",
								Description: "List children",
							},
						},
					},
				}
				s.Resources["items"] = items
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &APISpec{
				Name:    "absolute-conflict",
				Version: "0.1.0",
				BaseURL: "https://api.example.com",
				Resources: map[string]Resource{
					"items": {
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "/items", Description: "List"},
						},
					},
				},
			}
			tc.mutate(s)

			err := s.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base_url")
			assert.Contains(t, err.Error(), "absolute endpoint path")
		})
	}
}

// TestValidateRejectsBasePathWithProxyEnvelope — proxy-envelope routes via
// the envelope's Service/Path fields, not a URL-level prefix; a BasePath
// would be silently ignored by the proxy. Validate must fail-fast.
func TestValidateRejectsBasePathWithProxyEnvelope(t *testing.T) {
	t.Parallel()
	s := &APISpec{
		Name:          "proxypath",
		Version:       "0.1.0",
		BaseURL:       "https://proxy.example.com",
		BasePath:      "/api/v1",
		ClientPattern: "proxy-envelope",
		Resources: map[string]Resource{
			"items": {
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List"},
				},
			},
		},
	}
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy-envelope")
	assert.Contains(t, err.Error(), "base_path")
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

func TestParseTierRouting(t *testing.T) {
	t.Parallel()
	input := `
name: tiered
description: Tiered API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TIERED_TOKEN]
tier_routing:
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
        env_vars: [TIERED_PAID_KEY]
resources:
  results:
    description: Search
    tier: free
    endpoints:
      list:
        method: GET
        path: /search
        description: Search public results
      premium:
        method: GET
        path: /premium/search
        description: Search premium results
        tier: paid
`

	s, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.True(t, s.HasTierRouting())
	assert.Equal(t, "free", s.TierRouting.DefaultTier)
	assert.Equal(t, "none", s.TierRouting.Tiers["free"].Auth.Type)
	assert.Equal(t, "https://paid.api.example.com", s.TierRouting.Tiers["paid"].BaseURL)
	assert.Equal(t, []string{"TIERED_PAID_KEY"}, s.TierRouting.Tiers["paid"].Auth.EnvVars)
	assert.Equal(t, "free", s.Resources["results"].Tier)
	assert.Equal(t, "paid", s.Resources["results"].Endpoints["premium"].Tier)
	assert.Equal(t, "free", s.EffectiveTier(s.Resources["results"], s.Resources["results"].Endpoints["list"]))
	assert.Equal(t, "paid", s.EffectiveTier(s.Resources["results"], s.Resources["results"].Endpoints["premium"]))
}

func TestInferEndpointTemplateVarsFromBaseURLs(t *testing.T) {
	t.Parallel()

	s := &APISpec{
		Name:                 "templated",
		BaseURL:              "https://{tenant}.api.example.com",
		BasePath:             "/{base_path}",
		GraphQLEndpointPath:  "/admin/{version}/graphql",
		EndpointTemplateVars: []string{"explicit", "tenant"},
		TierRouting: TierRoutingConfig{Tiers: map[string]TierConfig{
			"z": {BaseURL: "https://{tier_z}.api.example.com"},
			"a": {BaseURL: "https://{tier_a}.api.example.com"},
		}},
		Resources: map[string]Resource{
			"z_resource": {
				BaseURL: "https://{resource_z}.api.example.com",
				Endpoints: map[string]Endpoint{
					"z_endpoint": {BaseURL: "https://{endpoint_z}.api.example.com"},
					"a_endpoint": {BaseURL: "https://{endpoint_a}.api.example.com"},
				},
				SubResources: map[string]Resource{
					"z_sub": {BaseURL: "https://{sub_z}.api.example.com"},
					"a_sub": {BaseURL: "https://{sub_a}.api.example.com"},
				},
			},
			"a_resource": {
				BaseURL: "https://{resource_a}.api.example.com/{tenant}",
			},
		},
	}

	s.InferEndpointTemplateVarsFromBaseURLs()

	assert.Equal(t, []string{
		"explicit",
		"tenant",
		"base_path",
		"version",
		"tier_a",
		"tier_z",
		"resource_a",
		"resource_z",
		"endpoint_a",
		"endpoint_z",
		"sub_a",
		"sub_z",
	}, s.EndpointTemplateVars)
}

func TestInferEndpointTemplateVarsFromBaseURLsFastPathSources(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*APISpec)
		want   string
	}{
		{
			name: "base path",
			mutate: func(s *APISpec) {
				s.BasePath = "/{base_path}"
			},
			want: "base_path",
		},
		{
			name: "graphql endpoint path",
			mutate: func(s *APISpec) {
				s.GraphQLEndpointPath = "/admin/{version}/graphql"
			},
			want: "version",
		},
		{
			name: "tier base url",
			mutate: func(s *APISpec) {
				s.TierRouting = TierRoutingConfig{Tiers: map[string]TierConfig{
					"paid": {BaseURL: "https://{tier}.api.example.com"},
				}}
			},
			want: "tier",
		},
		{
			name: "resource base url",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.BaseURL = "https://{resource}.api.example.com"
				s.Resources["items"] = resource
			},
			want: "resource",
		},
		{
			name: "endpoint base url",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.Endpoints["list"] = Endpoint{Method: "GET", Path: "/items", BaseURL: "https://{endpoint}.api.example.com"}
				s.Resources["items"] = resource
			},
			want: "endpoint",
		},
		{
			name: "absolute endpoint path",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.Endpoints["list"] = Endpoint{Method: "GET", Path: "https://{endpoint_path}.api.example.com/items/{id}"}
				s.Resources["items"] = resource
			},
			want: "endpoint_path",
		},
		{
			name: "subresource base url",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.SubResources = map[string]Resource{
					"children": {BaseURL: "https://{subresource}.api.example.com"},
				}
				s.Resources["items"] = resource
			},
			want: "subresource",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &APISpec{
				Name:    "templated",
				BaseURL: "https://api.example.com",
				Resources: map[string]Resource{
					"items": {
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "/items"},
						},
					},
				},
			}
			tc.mutate(s)

			s.InferEndpointTemplateVarsFromBaseURLs()

			assert.Equal(t, []string{tc.want}, s.EndpointTemplateVars)
		})
	}
}

func TestInferEndpointTemplateVarsIgnoresAbsoluteEndpointPathParams(t *testing.T) {
	t.Parallel()

	s := &APISpec{
		Name:    "templated",
		BaseURL: "https://api.example.com",
		Resources: map[string]Resource{
			"items": {
				Endpoints: map[string]Endpoint{
					"get": {
						Method: "GET",
						Path:   "https://api.example.com/items/{id}",
						Params: []Param{{Name: "id", Type: "string", Positional: true}},
					},
				},
			},
		},
	}

	s.InferEndpointTemplateVarsFromBaseURLs()

	assert.Empty(t, s.EndpointTemplateVars,
		"path-segment params in absolute endpoint paths should stay command inputs, not env-backed template vars")
}

func TestValidateTierRouting(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*APISpec)
		wantErr string
	}{
		{
			name: "resource selector and endpoint override",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.Tier = "free"
				endpoint := resource.Endpoints["get"]
				endpoint.Tier = "paid"
				resource.Endpoints["get"] = endpoint
				s.Resources["items"] = resource
			},
		},
		{
			name: "no default leaves unselected endpoints on global auth",
			mutate: func(s *APISpec) {
				s.TierRouting.DefaultTier = ""
			},
		},
		{
			name: "unknown endpoint tier",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				endpoint := resource.Endpoints["list"]
				endpoint.Tier = "enterprise"
				resource.Endpoints["list"] = endpoint
				s.Resources["items"] = resource
			},
			wantErr: "unknown tier",
		},
		{
			name: "credential tier requires env var",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.Auth.EnvVars = nil
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "env_vars",
		},
		{
			name: "unsupported auth type",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.Auth.Type = "oauth2"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "unsupported auth type",
		},
		{
			name: "unsupported placement",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.Auth.In = "cookie"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "auth.in",
		},
		{
			name: "query auth requires parameter name",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.Auth.In = "query"
				tier.Auth.Header = ""
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "header",
		},
		{
			name: "format placeholder must be declared",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.Auth.Format = "Token {missing}"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "placeholder",
		},
		{
			name: "no_auth conflicts with credential tier",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				endpoint := resource.Endpoints["list"]
				endpoint.NoAuth = true
				endpoint.Tier = "paid"
				resource.Endpoints["list"] = endpoint
				s.Resources["items"] = resource
			},
			wantErr: "no_auth",
		},
		{
			name: "proxy envelope conflict",
			mutate: func(s *APISpec) {
				s.ClientPattern = "proxy-envelope"
			},
			wantErr: "proxy-envelope",
		},
		{
			name: "resource base url conflict",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.BaseURL = "https://other.example.com"
				s.Resources["items"] = resource
			},
			wantErr: "base_url",
		},
		{
			name: "absolute endpoint path conflict",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				endpoint := resource.Endpoints["list"]
				endpoint.Path = "https://other.example.com/items"
				resource.Endpoints["list"] = endpoint
				s.Resources["items"] = resource
			},
			wantErr: "absolute endpoint paths",
		},
		{
			name: "subresource absolute endpoint path conflict",
			mutate: func(s *APISpec) {
				resource := s.Resources["items"]
				resource.SubResources = map[string]Resource{
					"children": {
						Endpoints: map[string]Endpoint{
							"list": {Method: "GET", Path: "https://children.example.com/items", Description: "List children"},
						},
					},
				}
				s.Resources["items"] = resource
			},
			wantErr: "absolute endpoint paths",
		},
		{
			name: "auth-bearing tier through resource base url must pass host review",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = ""
				s.TierRouting.Tiers["paid"] = tier
				resource := s.Resources["items"]
				resource.BaseURL = "https://paid.example.net"
				endpoint := resource.Endpoints["list"]
				endpoint.Tier = "paid"
				resource.Endpoints["list"] = endpoint
				s.Resources["items"] = resource
			},
			wantErr: "cross-host",
		},
		{
			name: "auth-bearing tier through reviewed resource base url is accepted",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = ""
				tier.AllowCrossHostAuth = true
				s.TierRouting.Tiers["paid"] = tier
				resource := s.Resources["items"]
				resource.BaseURL = "https://paid.example.net"
				endpoint := resource.Endpoints["list"]
				endpoint.Tier = "paid"
				resource.Endpoints["list"] = endpoint
				s.Resources["items"] = resource
			},
		},
		{
			name: "auth-bearing tier base url must be https",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = "http://paid.api.example.com"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "https",
		},
		{
			name: "auth-bearing tier base url rejects loopback",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = "https://127.0.0.1"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "loopback",
		},
		{
			name: "cross host auth requires explicit review",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = "https://paid.example.net"
				s.TierRouting.Tiers["paid"] = tier
			},
			wantErr: "cross-host",
		},
		{
			name: "cross host auth accepts explicit review",
			mutate: func(s *APISpec) {
				tier := s.TierRouting.Tiers["paid"]
				tier.BaseURL = "https://paid.example.net"
				tier.AllowCrossHostAuth = true
				s.TierRouting.Tiers["paid"] = tier
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := validTierRoutingSpec()
			tc.mutate(spec)
			err := spec.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func validTierRoutingSpec() *APISpec {
	return &APISpec{
		Name:    "tiered",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type:    "bearer_token",
			EnvVars: []string{"TIERED_TOKEN"},
		},
		TierRouting: TierRoutingConfig{
			DefaultTier: "free",
			Tiers: map[string]TierConfig{
				"free": {
					Auth: AuthConfig{Type: "none"},
				},
				"paid": {
					BaseURL: "https://paid.api.example.com",
					Auth: AuthConfig{
						Type:    "api_key",
						In:      "query",
						Header:  "api_key",
						EnvVars: []string{"TIERED_PAID_KEY"},
					},
				},
			},
		},
		Resources: map[string]Resource{
			"items": {
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
					"get":  {Method: "GET", Path: "/items/{id}", Description: "Get item"},
				},
			},
		},
	}
}

// TestValidateRejectsReservedPlaceholderHost guards against the OLX-style
// regression in #818 where a browser-sniff emitter shipped
// `https://example.com/resource` as a real endpoint path, compiling cleanly
// into the runtime client and failing only on first live call. The validator
// must reject any absolute URL field whose bare host is one of the IETF
// reserved documentation hostnames (RFC 2606 / RFC 6761), while leaving
// relative paths and subdomained hosts (api.example.com, used as legitimate
// test scaffolding throughout the codebase) untouched.
func TestValidateRejectsReservedPlaceholderHost(t *testing.T) {
	baseValid := func() APISpec {
		return APISpec{
			Name:    "demo",
			BaseURL: "https://api.example.com",
			Auth:    AuthConfig{Type: "none"},
			Resources: map[string]Resource{
				"items": {
					Endpoints: map[string]Endpoint{
						"list": {Method: "GET", Path: "/items"},
					},
				},
			},
		}
	}

	cases := []struct {
		name       string
		mutate     func(s *APISpec)
		wantErr    string
		wantNoErr  bool
		wantHostIn string
	}{
		{
			name: "spec-level base_url with bare example.com host is rejected",
			mutate: func(s *APISpec) {
				s.BaseURL = "https://example.com"
			},
			wantHostIn: "example.com",
		},
		{
			name: "endpoint path with bare example.com host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "https://example.com/resource"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: "example.com",
		},
		{
			name: "endpoint path with example.org host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "http://example.org/v1/things"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: "example.org",
		},
		{
			name: "endpoint base_url override with example.net host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.BaseURL = "https://example.net"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: "example.net",
		},
		{
			name: "endpoint path with example.test host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "https://example.test/resource"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: "example.test",
		},
		{
			name: "endpoint path with example.invalid host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "https://example.invalid/resource"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: "example.invalid",
		},
		{
			name: "endpoint path with bare example host is rejected",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "https://example/resource"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantHostIn: `"example"`,
		},
		{
			name: "resource base_url override with example.com host is rejected",
			mutate: func(s *APISpec) {
				r := s.Resources["items"]
				r.BaseURL = "https://example.com"
				s.Resources["items"] = r
			},
			wantHostIn: "example.com",
		},
		{
			name: "sub-resource endpoint with placeholder host is rejected",
			mutate: func(s *APISpec) {
				s.Resources["items"] = Resource{
					Endpoints: map[string]Endpoint{
						"list": {Method: "GET", Path: "/items"},
					},
					SubResources: map[string]Resource{
						"reviews": {
							Endpoints: map[string]Endpoint{
								"get": {Method: "GET", Path: "https://example.com/reviews"},
							},
						},
					},
				}
			},
			wantHostIn: "example.com",
		},
		{
			name:      "relative path passes",
			mutate:    func(s *APISpec) {},
			wantNoErr: true,
		},
		{
			name: "subdomained example.com base_url passes",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.BaseURL = "https://api.example.com/v2"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantNoErr: true,
		},
		{
			name: "subdomained example.com path passes",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "https://geocoding-api.example.com/v1/search"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantNoErr: true,
		},
		{
			name: "query string containing example.com passes (relative path)",
			mutate: func(s *APISpec) {
				ep := s.Resources["items"].Endpoints["list"]
				ep.Path = "/proxy?url=https://example.com/x"
				s.Resources["items"].Endpoints["list"] = ep
			},
			wantNoErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := baseValid()
			tc.mutate(&s)
			err := s.Validate()
			if tc.wantNoErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantHostIn)
			assert.Contains(t, err.Error(), "reserved placeholder host")
		})
	}
}

// TestWalkerConfig_YAMLRoundTrip catches future regressions in WalkerConfig
// YAML tags or the Walker field's omitempty on Endpoint. The Walker pointer
// and all three sub-fields must survive a marshal → unmarshal cycle.
func TestWalkerConfig_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("populated walker survives round-trip", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{
			Method: "GET",
			Path:   "/games/{game_key}/leagues",
			Walker: &WalkerConfig{
				Parent:   "games",
				KeyField: "game_key",
				KeyParam: "game_key",
			},
		}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.Contains(t, string(data), "walker:")
		assert.Contains(t, string(data), "parent: games")
		assert.Contains(t, string(data), "key_field: game_key")
		assert.Contains(t, string(data), "key_param: game_key")

		var roundTripped Endpoint
		require.NoError(t, yaml.Unmarshal(data, &roundTripped))
		require.NotNil(t, roundTripped.Walker)
		assert.Equal(t, "games", roundTripped.Walker.Parent)
		assert.Equal(t, "game_key", roundTripped.Walker.KeyField)
		assert.Equal(t, "game_key", roundTripped.Walker.KeyParam)
	})

	t.Run("nil walker omits the section", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{Method: "GET", Path: "/games"}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "walker:")
	})

	t.Run("walker with only parent omits optional sub-fields", func(t *testing.T) {
		t.Parallel()
		ep := Endpoint{
			Method: "GET",
			Path:   "/leagues/{league_id}/teams",
			Walker: &WalkerConfig{Parent: "leagues"},
		}
		data, err := yaml.Marshal(ep)
		require.NoError(t, err)
		assert.Contains(t, string(data), "parent: leagues")
		assert.NotContains(t, string(data), "key_field")
		assert.NotContains(t, string(data), "key_param")
	})
}

// TestAuthCompanionParseRoundTrip exercises the press-auth companion hints
// (login_url, login_complete_selector, jwt_carrier_cookie). Round-trips
// through ParseBytes so the YAML tags are exercised, not just the struct
// field assignments.
func TestAuthCompanionParseRoundTrip(t *testing.T) {
	t.Run("all three companion fields parse cleanly", func(t *testing.T) {
		yamlSpec := []byte(`name: example-api
base_url: https://api.example.com
auth:
  type: composed
  format: "Cookie {session}"
  cookie_domain: example.com
  cookies:
    - session
    - guestsession
  login_url: https://www.example.com/account/login
  login_complete_selector: "a[href*=signout]"
  jwt_carrier_cookie: guestsession
resources:
  items:
    endpoints:
      list:
        method: GET
        path: /items
`)
		s, err := ParseBytes(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "https://www.example.com/account/login", s.Auth.LoginURL)
		assert.Equal(t, "a[href*=signout]", s.Auth.LoginCompleteSelector)
		assert.Equal(t, "guestsession", s.Auth.JWTCarrierCookie)
		assert.True(t, s.Auth.HasCompanionHints())
	})

	t.Run("no companion fields leaves zero values and HasCompanionHints false", func(t *testing.T) {
		yamlSpec := []byte(`name: example-api
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [EXAMPLE_API_TOKEN]
resources:
  items:
    endpoints:
      list:
        method: GET
        path: /items
`)
		s, err := ParseBytes(yamlSpec)
		require.NoError(t, err)
		assert.Empty(t, s.Auth.LoginURL)
		assert.Empty(t, s.Auth.LoginCompleteSelector)
		assert.Empty(t, s.Auth.JWTCarrierCookie)
		assert.False(t, s.Auth.HasCompanionHints())
	})
}

func TestAuthCompanionValidate(t *testing.T) {
	tests := []struct {
		name    string
		auth    AuthConfig
		wantErr string
	}{
		{
			name: "https login_url is valid",
			auth: AuthConfig{LoginURL: "https://example.com/login"},
		},
		{
			name: "http localhost is valid",
			auth: AuthConfig{LoginURL: "http://localhost:8080/login"},
		},
		{
			name: "http 127.0.0.1 is valid",
			auth: AuthConfig{LoginURL: "http://127.0.0.1/login"},
		},
		{
			name:    "plain http elsewhere is rejected",
			auth:    AuthConfig{LoginURL: "http://example.com/login"},
			wantErr: "only https://",
		},
		{
			name:    "ftp scheme is rejected",
			auth:    AuthConfig{LoginURL: "ftp://example.com/"},
			wantErr: "must use http or https",
		},
		{
			name: "empty is valid (companion is optional)",
			auth: AuthConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuthCompanion(tt.auth)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestAuthCompanionJWTCarrierCookieNotInCookiesWarns asserts that a
// jwt_carrier_cookie that isn't in the cookies list surfaces a non-fatal
// warning, not a hard error. Plausible typo, surface it but don't block.
func TestAuthCompanionJWTCarrierCookieNotInCookiesWarns(t *testing.T) {
	auth := AuthConfig{
		Type:             "composed",
		Cookies:          []string{"session", "csrf_token"},
		JWTCarrierCookie: "guestsesion", // intentional typo
	}
	warnings := captureWarnings(t, func() {
		err := validateAuthCompanion(auth)
		require.NoError(t, err)
	})
	assert.Contains(t, warnings, "jwt_carrier_cookie")
	assert.Contains(t, warnings, "guestsesion")
}

// TestAPISpecValidate_RejectsBadLoginURL plugs validateAuthCompanion into
// the full APISpec validation path to confirm a malformed login_url
// surfaces at Validate() time, not just when the helper is called
// directly.
func TestAPISpecValidate_RejectsBadLoginURL(t *testing.T) {
	build := func(loginURL string) APISpec {
		return APISpec{
			Name:    "companion-validate",
			BaseURL: "https://api.example.com",
			Auth: AuthConfig{
				Type:         "composed",
				CookieDomain: "example.com",
				Cookies:      []string{"session"},
				LoginURL:     loginURL,
				EnvVars:      []string{"COMPANION_VALIDATE_TOKEN"},
			},
			Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}},
			},
		}
	}

	t.Run("valid login_url passes Validate()", func(t *testing.T) {
		s := build("https://example.com/login")
		require.NoError(t, s.Validate())
	})

	t.Run("plain http is rejected at APISpec level", func(t *testing.T) {
		s := build("http://example.com/login")
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "login_url")
	})
}

// TestAuthHasCompanionHints documents the partial-hint state.
// LoginCompleteSelector and JWTCarrierCookie are optional, so LoginURL
// alone is enough to enable cookie-only companion login integration.
func TestAuthHasCompanionHints(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
		want bool
	}{
		{name: "all fields set", auth: AuthConfig{LoginURL: "https://example.com/login", JWTCarrierCookie: "session", LoginCompleteSelector: "a"}, want: true},
		{name: "login_url and carrier set, selector omitted", auth: AuthConfig{LoginURL: "https://example.com/login", JWTCarrierCookie: "session"}, want: true},
		{name: "login_url only", auth: AuthConfig{LoginURL: "https://example.com/login"}, want: true},
		{name: "carrier only", auth: AuthConfig{JWTCarrierCookie: "session"}, want: false},
		{name: "empty", auth: AuthConfig{}, want: false},
		{name: "whitespace-only login_url", auth: AuthConfig{LoginURL: "   ", JWTCarrierCookie: "session"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.auth.HasCompanionHints())
		})
	}
}

// TestAuthHasCookies pins the predicate that drives cookie-jar wiring in
// client.go.tmpl and the WriteCookieJarFromMap call in auth_browser.go.tmpl.
// It must gate on the cookie list, not Auth.Type, because composed-auth
// specs without auth.cookies have nothing to persist.
func TestAuthHasCookies(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
		want bool
	}{
		{name: "cookie-typed with cookie list", auth: AuthConfig{Type: "cookie", Cookies: []string{"session_id"}}, want: true},
		{name: "composed-typed with cookie list", auth: AuthConfig{Type: "composed", Cookies: []string{"session_id", "csrf"}}, want: true},
		{name: "composed-typed without cookie list", auth: AuthConfig{Type: "composed"}, want: false},
		{name: "bearer with no cookies", auth: AuthConfig{Type: "bearer_token"}, want: false},
		{name: "empty", auth: AuthConfig{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.auth.HasCookies())
		})
	}
}

func TestPromoteParamsToBodyForWriteEndpoints(t *testing.T) {
	t.Parallel()

	const header = `name: testapi
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars: [TESTAPI_TOKEN]
resources:
`

	t.Run("POST endpoint with params and no body promotes to body", func(t *testing.T) {
		t.Parallel()
		input := header + `  messages:
    description: Slack-style message endpoints
    endpoints:
      post_message:
        method: POST
        path: /chat.postMessage
        description: Send a message
        params:
          - name: channel
            type: string
            required: true
          - name: text
            type: string
            required: true
          - name: thread_ts
            type: string
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["messages"].Endpoints["post_message"]
		assert.Empty(t, ep.Params, "non-path params should have moved to Body")
		require.Len(t, ep.Body, 3)
		bodyNames := []string{ep.Body[0].Name, ep.Body[1].Name, ep.Body[2].Name}
		assert.ElementsMatch(t, []string{"channel", "text", "thread_ts"}, bodyNames)
	})

	t.Run("POST endpoint preserves path placeholders in Params", func(t *testing.T) {
		t.Parallel()
		input := header + `  widgets:
    description: Widget endpoints
    endpoints:
      activate:
        method: POST
        path: /widgets/{id}/activate
        description: Activate a widget
        params:
          - name: reason
            type: string
            required: true
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["widgets"].Endpoints["activate"]
		require.Len(t, ep.Params, 1, "id placeholder should remain in Params")
		assert.Equal(t, "id", ep.Params[0].Name)
		assert.True(t, ep.Params[0].Positional)
		require.Len(t, ep.Body, 1)
		assert.Equal(t, "reason", ep.Body[0].Name)
	})

	t.Run("POST endpoint with explicit body is left untouched", func(t *testing.T) {
		t.Parallel()
		input := header + `  items:
    description: Item endpoints
    endpoints:
      create:
        method: POST
        path: /items
        description: Create item
        params:
          - name: org_id
            type: string
        body:
          - name: name
            type: string
            required: true
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["items"].Endpoints["create"]
		require.Len(t, ep.Params, 1)
		assert.Equal(t, "org_id", ep.Params[0].Name)
		require.Len(t, ep.Body, 1)
		assert.Equal(t, "name", ep.Body[0].Name)
	})

	t.Run("GET endpoint params are not promoted", func(t *testing.T) {
		t.Parallel()
		input := header + `  lookup:
    description: Lookup endpoints
    endpoints:
      query:
        method: GET
        path: /lookup
        description: Lookup
        params:
          - name: q
            type: string
            required: true
          - name: limit
            type: integer
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["lookup"].Endpoints["query"]
		require.Len(t, ep.Params, 2)
		assert.Empty(t, ep.Body)
	})

	t.Run("PUT and PATCH are also promoted", func(t *testing.T) {
		t.Parallel()
		input := header + `  records:
    description: Record endpoints
    endpoints:
      replace:
        method: PUT
        path: /records/{id}
        description: Replace record
        params:
          - name: name
            type: string
      modify:
        method: PATCH
        path: /records/{id}
        description: Patch record
        params:
          - name: status
            type: string
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		put := s.Resources["records"].Endpoints["replace"]
		require.Len(t, put.Body, 1)
		assert.Equal(t, "name", put.Body[0].Name)

		patch := s.Resources["records"].Endpoints["modify"]
		require.Len(t, patch.Body, 1)
		assert.Equal(t, "status", patch.Body[0].Name)
	})

	t.Run("DELETE is not promoted", func(t *testing.T) {
		t.Parallel()
		input := header + `  records:
    description: Record endpoints
    endpoints:
      remove:
        method: DELETE
        path: /records/{id}
        description: Delete record
        params:
          - name: cascade
            type: boolean
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["records"].Endpoints["remove"]
		names := make([]string, len(ep.Params))
		for i, p := range ep.Params {
			names[i] = p.Name
		}
		assert.ElementsMatch(t, []string{"id", "cascade"}, names, "DELETE keeps the {id} placeholder enrichPathParams injected and the cascade query param")
		assert.Empty(t, ep.Body, "DELETE keeps cascade as a query/flag, not body")
	})

	t.Run("subresource endpoints are walked", func(t *testing.T) {
		t.Parallel()
		input := header + `  channels:
    description: Channel endpoints
    sub_resources:
      messages:
        description: Channel messages
        endpoints:
          post:
            method: POST
            path: /channels/{channelId}/messages
            description: Post message
            params:
              - name: text
                type: string
                required: true
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["channels"].SubResources["messages"].Endpoints["post"]
		require.Len(t, ep.Body, 1)
		assert.Equal(t, "text", ep.Body[0].Name)
	})

	t.Run("explicit empty body: [] opts out of promotion", func(t *testing.T) {
		t.Parallel()
		input := header + `  pipelines:
    description: Pipeline endpoints
    endpoints:
      trigger:
        method: POST
        path: /pipelines/trigger
        description: Trigger a pipeline
        params:
          - name: dry_run
            type: boolean
        body: []
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["pipelines"].Endpoints["trigger"]
		assert.True(t, ep.BodySet, "explicit `body: []` should set BodySet")
		assert.Empty(t, ep.Body, "explicit empty body stays empty")
		require.Len(t, ep.Params, 1, "params stay as query params when author opted out")
		assert.Equal(t, "dry_run", ep.Params[0].Name)
	})

	t.Run("mixed params and explicit body leaves params as query strings", func(t *testing.T) {
		t.Parallel()
		input := header + `  uploads:
    description: Upload endpoints
    endpoints:
      create:
        method: POST
        path: /uploads
        description: Create upload
        params:
          - name: idempotency_key
            type: string
        body:
          - name: filename
            type: string
            required: true
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["uploads"].Endpoints["create"]
		assert.True(t, ep.BodySet)
		require.Len(t, ep.Params, 1, "idempotency_key stays as query/flag, not silently moved into body")
		assert.Equal(t, "idempotency_key", ep.Params[0].Name)
		require.Len(t, ep.Body, 1)
		assert.Equal(t, "filename", ep.Body[0].Name)
	})

	t.Run("absent body key leaves BodySet false and triggers promotion", func(t *testing.T) {
		t.Parallel()
		input := header + `  notes:
    description: Note endpoints
    endpoints:
      create:
        method: POST
        path: /notes
        description: Create note
        params:
          - name: title
            type: string
            required: true
`
		s, err := ParseBytes([]byte(input))
		require.NoError(t, err)
		ep := s.Resources["notes"].Endpoints["create"]
		assert.False(t, ep.BodySet, "no body key in source -> BodySet false")
		require.Len(t, ep.Body, 1, "title was promoted to body")
		assert.Equal(t, "title", ep.Body[0].Name)
	})
}
