package openapi

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPaginationODataWireNames(t *testing.T) {
	t.Parallel()

	pag := detectPagination([]spec.Param{
		{Name: "top", Type: "int", URLName: "$top"},
		{Name: "skip", Type: "int", URLName: "$skip"},
		{Name: "count", Type: "bool", URLName: "$count"},
	}, nil)
	require.NotNil(t, pag)
	assert.Equal(t, "$top", pag.LimitParam)
	assert.Equal(t, "$skip", pag.CursorParam)
	assert.Equal(t, "offset", pag.Type)

	plain := detectPagination([]spec.Param{
		{Name: "limit", Type: "int"},
		{Name: "offset", Type: "int"},
	}, nil)
	require.NotNil(t, plain)
	assert.Equal(t, "limit", plain.LimitParam)
	assert.Equal(t, "offset", plain.CursorParam)
	assert.Equal(t, "offset", plain.Type)
}

func TestParseDollarODataParamsKeepWireNames(t *testing.T) {
	t.Parallel()

	api, err := Parse([]byte(`openapi: 3.0.3
info:
  title: Dollar Params
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      parameters:
        - name: $top
          in: query
          schema: {type: integer}
        - name: $skip
          in: query
          schema: {type: integer}
        - name: $count
          in: query
          schema: {type: boolean}
        - name: $.xgafv
          in: query
          schema: {type: string}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: {type: string}
`))
	require.NoError(t, err)

	var params []spec.Param
	for _, resource := range api.Resources {
		for _, ep := range resource.Endpoints {
			if ep.Path == "/items" {
				params = ep.Params
			}
		}
	}
	require.NotEmpty(t, params)
	byName := map[string]spec.Param{}
	for _, param := range params {
		byName[param.Name] = param
		assert.NotContains(t, param.Name, "xgafv")
		assert.NotEqual(t, "$.xgafv", param.URLName)
	}
	require.Contains(t, byName, "top")
	assert.Equal(t, "$top", byName["top"].URLName)
	require.Contains(t, byName, "skip")
	assert.Equal(t, "$skip", byName["skip"].URLName)
	require.Contains(t, byName, "count")
	assert.Equal(t, "$count", byName["count"].URLName)
	assert.Equal(t, "bool", byName["count"].Type)
	_, hasXgafv := byName["xgafv"]
	assert.False(t, hasXgafv, "$.xgafv is not an identifier and must stay skipped")

	pag := detectPagination(params, nil)
	require.NotNil(t, pag)
	assert.Equal(t, "$top", pag.LimitParam)
	assert.Equal(t, "$skip", pag.CursorParam)
	assert.Equal(t, "offset", pag.Type)
}
