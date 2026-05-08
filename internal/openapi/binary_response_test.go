package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSwagger2ProducesMarksBinaryResponses(t *testing.T) {
	data := []byte(`{
  "swagger": "2.0",
  "info": {"title": "Binary Demo", "version": "1.0.0"},
  "host": "api.example.com",
  "schemes": ["https"],
  "paths": {
    "/Invoices('{DocumentID}')/Print": {
      "get": {
        "description": "Print invoice",
        "produces": ["application/pdf", "application/json"],
        "parameters": [
          {"name": "DocumentID", "in": "path", "type": "string", "required": true}
        ],
        "responses": {
          "200": {"description": "PDF", "schema": {"type": "file"}}
        }
      }
    }
  }
}`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "GET", "/Invoices('{DocumentID}')/Print")
	assert.Equal(t, []string{"application/json", "application/pdf"}, endpoint.ResponseContentTypes)
	assert.True(t, endpoint.HasBinaryResponse())
	assert.True(t, endpoint.NeedsAcceptFlag())
	assert.Equal(t, "application/pdf", endpoint.DefaultBinaryAccept())
}

func TestParseOpenAPI3ContentMarksBinaryOnlyResponses(t *testing.T) {
	data := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Binary Demo", "version": "1.0.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/exports/{id}": {
      "get": {
        "operationId": "getExport",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "XLSX export",
            "content": {
              "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": {
                "schema": {"type": "string", "format": "binary"}
              }
            }
          }
        }
      }
    }
  }
}`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "GET", "/exports/{id}")
	assert.Equal(t, []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, endpoint.ResponseContentTypes)
	assert.True(t, endpoint.HasBinaryResponse())
	assert.False(t, endpoint.HasJSONResponse())
	assert.False(t, endpoint.NeedsAcceptFlag())
}
