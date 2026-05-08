package openapi

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSwagger2ODataDetectionAndEndpointNormalization(t *testing.T) {
	data := []byte(`{
  "swagger": "2.0",
  "info": {"title": "OData Demo", "version": "1.0.0"},
  "host": "api.example.com",
  "basePath": "/odata/v4",
  "schemes": ["https"],
  "paths": {
    "/Customers": {
      "get": {
        "description": "List customers",
        "responses": {
          "200": {
            "description": "ok",
            "schema": {
              "type": "object",
              "properties": {
                "@odata.context": {"type": "string"},
                "value": {
                  "type": "array",
                  "items": {"$ref": "#/definitions/Customer"}
                }
              }
            }
          }
        }
      }
    },
    "/CurrentPeriod": {
      "get": {
        "description": "CurrentPeriod is an ODATA function - use GET to call",
        "parameters": [
          {"name": "ModuleCode", "in": "query", "type": "string", "required": true}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    },
    "/OEOrder('{DocumentID}')/GenerateInvoice": {
      "post": {
        "description": "OEOrder/GenerateInvoice is an ODATA action - use POST to call",
        "parameters": [
          {"name": "DocumentID", "in": "path", "type": "integer", "required": true},
          {"name": "DocumentDate", "in": "query", "type": "string"},
          {"name": "PeriodID", "in": "query", "type": "integer"}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "definitions": {
    "Customer": {
      "type": "object",
      "properties": {
        "CustomerCode": {"type": "string"},
        "CustomerName": {"type": "string"}
      }
    }
  }
}`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.True(t, parsed.OData)
	assert.Equal(t, "https://api.example.com/odata/v4", parsed.BaseURL)

	list := findParsedEndpointByPath(t, parsed, "GET", "/Customers")
	assert.Equal(t, "value", list.ResponsePath)
	assert.Equal(t, "array", list.Response.Type)
	assert.Equal(t, "DefinitionsCustomer", list.Response.Item)
	assert.Contains(t, publicParamNames(list.Params), "top")
	assert.Contains(t, publicParamNames(list.Params), "filter")
	assert.Contains(t, wireParamNames(list.Params), "$top")

	current := findParsedEndpointByPath(t, parsed, "GET", "/CurrentPeriod")
	assert.Equal(t, spec.ODataOperationFunction, current.ODataOperation)
	assert.NotContains(t, publicParamNames(current.Params), "top")

	action := findParsedEndpointByPath(t, parsed, "POST", "/OEOrder('{DocumentID}')/GenerateInvoice")
	assert.Equal(t, spec.ODataOperationAction, action.ODataOperation)
	require.Len(t, action.Params, 1)
	assert.Equal(t, "DocumentID", action.Params[0].Name)
	assert.Equal(t, "int", action.Params[0].Type)
	assert.Equal(t, []string{"DocumentDate", "PeriodID"}, bodyParamNames(action.Body))
	require.Len(t, action.Body, 2)
	assert.Equal(t, "string", action.Body[0].Type)
	assert.Equal(t, "int", action.Body[1].Type)
}

func TestParseSwagger2BodyParametersAsRequestBody(t *testing.T) {
	data := []byte(`{
  "swagger": "2.0",
  "info": {"title": "OData Body Demo", "version": "1.0.0"},
  "host": "api.example.com",
  "basePath": "/odata/v4",
  "schemes": ["https"],
  "consumes": ["application/json"],
  "paths": {
    "/ExecuteSQL": {
      "post": {
        "description": "ExecuteSQL is an ODATA action - use POST to call",
        "parameters": [
          {
            "name": "body",
            "in": "body",
            "required": true,
            "schema": {"$ref": "#/definitions/ExecuteSqlRequest"}
          }
        ],
        "responses": {"200": {"description": "ok"}}
      }
    },
    "/APAllocateTransactions": {
      "post": {
        "description": "APAllocateTransactions is an ODATA action - use POST to call",
        "parameters": [
          {
            "name": "body",
            "in": "body",
            "required": true,
            "schema": {
              "type": "object",
              "required": ["CreditID", "ApplyDiscount"],
              "properties": {
                "CreditID": {"type": "integer"},
                "ApplyDiscount": {"type": "boolean"},
                "Memo": {"type": "string"}
              }
            }
          }
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "definitions": {
    "ExecuteSqlRequest": {
      "type": "object",
      "required": ["QueryText"],
      "properties": {
        "QueryText": {
          "type": "string",
          "description": "SQL SELECT query"
        }
      }
    }
  }
}`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	executeSQL := findParsedEndpointByPath(t, parsed, "POST", "/ExecuteSQL")
	require.Len(t, executeSQL.Body, 1)
	assert.Equal(t, spec.ODataOperationAction, executeSQL.ODataOperation)
	assert.Equal(t, "QueryText", executeSQL.Body[0].Name)
	assert.Equal(t, "string", executeSQL.Body[0].Type)
	assert.True(t, executeSQL.Body[0].Required)

	allocate := findParsedEndpointByPath(t, parsed, "POST", "/APAllocateTransactions")
	require.Len(t, allocate.Body, 3)
	byName := map[string]spec.Param{}
	for _, param := range allocate.Body {
		byName[param.Name] = param
	}
	assert.Equal(t, "bool", byName["ApplyDiscount"].Type)
	assert.True(t, byName["ApplyDiscount"].Required)
	assert.Equal(t, "int", byName["CreditID"].Type)
	assert.True(t, byName["CreditID"].Required)
	assert.Equal(t, "string", byName["Memo"].Type)
	assert.False(t, byName["Memo"].Required)
	assert.Empty(t, allocate.Params, "Swagger 2 body parameters must not leak into URL params")
}

func publicParamNames(params []spec.Param) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.PublicInputName())
	}
	return out
}

func wireParamNames(params []spec.Param) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.Name)
	}
	return out
}

func bodyParamNames(params []spec.Param) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.Name)
	}
	return out
}
