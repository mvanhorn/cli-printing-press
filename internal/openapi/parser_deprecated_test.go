package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCarriesOperationDeprecatedFlag(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Deprecation Flag API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /audiences:
    post:
      deprecated: true
      operationId: createAudience
      summary: Create an audience
      responses:
        "200":
          description: ok
    get:
      operationId: listAudiences
      summary: List audiences
      responses:
        "200":
          description: ok
  /live:
    get:
      operationId: getLive
      summary: Live endpoint
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	var sawDeprecated, sawLive bool
	for _, resource := range parsed.Resources {
		for _, endpoint := range resource.Endpoints {
			switch endpoint.Path {
			case "/audiences":
				if endpoint.Method == "POST" {
					assert.True(t, endpoint.Deprecated)
					sawDeprecated = true
				}
				if endpoint.Method == "GET" {
					assert.False(t, endpoint.Deprecated)
				}
			case "/live":
				assert.False(t, endpoint.Deprecated)
				sawLive = true
			}
		}
	}
	assert.True(t, sawDeprecated)
	assert.True(t, sawLive)
}
