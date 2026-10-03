package openapi

import (
	"os"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitTokenSpecEmitsBothCredentials(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../testdata/golden/fixtures/split-token-auth.yaml")
	require.NoError(t, err)
	parsed, err := Parse(body)
	require.NoError(t, err)

	prefix := naming.EnvPrefix(parsed.Name)
	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, "serverToken", parsed.Auth.Scheme)
	assert.Equal(t, "X-Server-Token", parsed.Auth.Header)
	assert.Equal(t, []string{prefix + "_SERVER_TOKEN"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, prefix+"_SERVER_TOKEN", parsed.Auth.EnvVarSpecs[0].Name)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Required)

	require.Len(t, parsed.Auth.AdditionalHeaders, 1)
	additional := parsed.Auth.AdditionalHeaders[0]
	assert.Equal(t, "X-Account-Token", additional.Header)
	assert.Equal(t, "header", additional.In)
	assert.Equal(t, "accountToken", additional.Scheme)
	assert.True(t, additional.PerOperation)
	assert.Equal(t, prefix+"_ACCOUNT_TOKEN", additional.EnvVar.Name)
	assert.Equal(t, spec.AuthEnvVarKindPerCall, additional.EnvVar.Kind)
	assert.False(t, additional.EnvVar.Required)
	assert.True(t, additional.EnvVar.Sensitive)

	assert.Equal(t, map[string]string{
		"GET /messages":      "serverToken",
		"GET /messages/{id}": "serverToken",
		"GET /servers/push":  "serverToken",
		"GET /servers":       "accountToken",
		"GET /servers/{id}":  "accountToken",
	}, endpointAuthSchemes(parsed))
}

func TestEqualUsageSplitTokenKeepsAlphabeticalPrimary(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Equal Split
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    serverToken:
      type: apiKey
      in: header
      name: X-Server-Token
    accountToken:
      type: apiKey
      in: header
      name: X-Account-Token
paths:
  /messages:
    get:
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /account:
    get:
      security:
        - accountToken: []
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)

	prefix := naming.EnvPrefix(parsed.Name)
	assert.Equal(t, "accountToken", parsed.Auth.Scheme, "equal usage keeps the alphabetically first scheme as primary")
	assert.Equal(t, []string{prefix + "_ACCOUNT_TOKEN"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.AdditionalHeaders, 1)
	assert.Equal(t, "serverToken", parsed.Auth.AdditionalHeaders[0].Scheme)
	assert.True(t, parsed.Auth.AdditionalHeaders[0].PerOperation)
	assert.Equal(t, prefix+"_SERVER_TOKEN", parsed.Auth.AdditionalHeaders[0].EnvVar.Name)
	assert.Equal(t, "accountToken", endpointAuthSchemes(parsed)["GET /account"])
	assert.Equal(t, "serverToken", endpointAuthSchemes(parsed)["GET /messages"])
}

func TestMixedExclusiveAndANDKeepsANDOperationUnscoped(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Mixed Split
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    serverToken:
      type: apiKey
      in: header
      name: X-Server-Token
    accountToken:
      type: apiKey
      in: header
      name: X-Account-Token
paths:
  /messages:
    get:
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /messages/{id}:
    get:
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /account:
    get:
      security:
        - accountToken: []
      responses:
        "200":
          description: OK
  /both:
    get:
      security:
        - serverToken: []
          accountToken: []
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)

	assert.Equal(t, "serverToken", parsed.Auth.Scheme)
	require.Len(t, parsed.Auth.AdditionalHeaders, 1)
	assert.Equal(t, "accountToken", parsed.Auth.AdditionalHeaders[0].Scheme)
	assert.True(t, parsed.Auth.AdditionalHeaders[0].PerOperation)
	schemes := endpointAuthSchemes(parsed)
	assert.Equal(t, "serverToken", schemes["GET /messages"])
	assert.Equal(t, "accountToken", schemes["GET /account"])
	assert.Empty(t, schemes["GET /both"], "an AND requirement is not a sole scheme, so it keeps spec-level auth")
}

func TestSplitTokenAuthSchemeSurvivesPathParamDefault(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Default Split
  version: "1.0.0"
  x-path-template-env-vars:
    userId:
      default: me
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    serverToken:
      type: apiKey
      in: header
      name: X-Server-Token
    accountToken:
      type: apiKey
      in: header
      name: X-Account-Token
paths:
  /users/{userId}/messages:
    get:
      parameters:
        - name: userId
          in: path
          required: true
          schema:
            type: string
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /account:
    get:
      security:
        - accountToken: []
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)

	assert.Equal(t, "serverToken", endpointAuthSchemes(parsed)["GET /users/me/messages"])
	assert.Equal(t, "accountToken", endpointAuthSchemes(parsed)["GET /account"])
}

func TestOAuthExclusiveLoserIsNotPromoted(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Key Or OAuth
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: X-API-Key
    oauth:
      type: http
      scheme: bearer
paths:
  /items:
    get:
      security:
        - apiKey: []
      responses:
        "200":
          description: OK
  /items/{id}:
    get:
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      security:
        - apiKey: []
      responses:
        "200":
          description: OK
  /admin:
    get:
      security:
        - oauth: []
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)
	assert.Equal(t, "apiKey", parsed.Auth.Scheme)
	assert.Empty(t, parsed.Auth.AdditionalHeaders)
	for _, scheme := range endpointAuthSchemes(parsed) {
		assert.Empty(t, scheme)
	}
}

func TestCollidingExclusiveEnvNamesDoNotSplit(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Generic Keys
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKey:
      type: apiKey
      in: header
      name: X-Api-Key
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-Other-Key
paths:
  /a:
    get:
      security:
        - ApiKey: []
      responses:
        "200":
          description: OK
  /b:
    get:
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)
	assert.Empty(t, parsed.Auth.AdditionalHeaders)
	for _, scheme := range endpointAuthSchemes(parsed) {
		assert.Empty(t, scheme)
	}
}

func endpointAuthSchemes(api *spec.APISpec) map[string]string {
	out := map[string]string{}
	if api == nil {
		return out
	}
	var walk func(spec.Resource)
	walk = func(resource spec.Resource) {
		for _, endpoint := range resource.Endpoints {
			out[endpoint.Method+" "+endpoint.Path] = endpoint.AuthScheme
		}
		for _, sub := range resource.SubResources {
			walk(sub)
		}
	}
	for _, resource := range api.Resources {
		walk(resource)
	}
	return out
}
