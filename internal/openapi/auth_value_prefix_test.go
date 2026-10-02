package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAuthValuePrefix(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Sentinel Prefix
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiTokenAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-value-prefix: "ApiToken "
paths:
  /agents:
    get:
      security:
        - ApiTokenAuth: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)
	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, "ApiToken", parsed.Auth.Prefix)
	assert.Empty(t, parsed.Auth.Format)
}

func TestParseAuthValuePrefixYieldsToFormat(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Format Wins
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKey:
      type: apiKey
      in: header
      name: Authorization
      x-prefix: Token
      x-auth-value-prefix: ApiToken
paths:
  /agents:
    get:
      security:
        - ApiKey: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)
	assert.Equal(t, "Token {token}", parsed.Auth.Format)
	assert.Empty(t, parsed.Auth.Prefix)
}

func TestParseAuthValuePrefixIgnoredForQueryKeys(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Query Key
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    QueryKey:
      type: apiKey
      in: query
      name: api_key
      x-auth-value-prefix: ApiToken
paths:
  /agents:
    get:
      security:
        - QueryKey: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)
	assert.Empty(t, parsed.Auth.Prefix)
	assert.Empty(t, parsed.Auth.Format)
}
