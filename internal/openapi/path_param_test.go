package openapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
)

func TestMapParametersPathDetection(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		paramName  string
		paramIn    string
		wantInPath bool
	}{
		{
			name:       "conforming path param",
			path:       "/users/{id}",
			paramName:  "id",
			paramIn:    openapi3.ParameterInPath,
			wantInPath: true,
		},
		{
			name:       "non-conforming: missing from path string",
			path:       "/users",
			paramName:  "id",
			paramIn:    openapi3.ParameterInPath,
			wantInPath: false,
		},
		{
			name:       "non-conforming: in query but in path string",
			path:       "/users/{id}",
			paramName:  "id",
			paramIn:    openapi3.ParameterInQuery,
			wantInPath: true,
		},
		{
			name:       "multiple path segments",
			path:       "/orgs/{org}/repos/{repo}",
			paramName:  "repo",
			paramIn:    openapi3.ParameterInPath,
			wantInPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pathItem := &openapi3.PathItem{}
			op := &openapi3.Operation{
				Parameters: openapi3.Parameters{
					{
						Value: &openapi3.Parameter{
							Name: tt.paramName,
							In:   tt.paramIn,
						},
					},
				},
			}

			params := mapParameters(tt.path, pathItem, op)
			found := false
			for _, p := range params {
				if p.Name == tt.paramName {
					assert.Equal(t, tt.wantInPath, p.PathParam)
					found = true
				}
			}
			assert.True(t, found, "parameter %s not found in mapped params", tt.paramName)
		})
	}
}

func TestMapParametersReclassificationPreservesPathDetection(t *testing.T) {
	// A parameter that is in the path template but gets reclassified as a flag
	// (e.g. because it has a default value).
	path := "/items/{page}"
	op := &openapi3.Operation{
		Parameters: openapi3.Parameters{
			{
				Value: &openapi3.Parameter{
					Name:     "page",
					In:       openapi3.ParameterInPath,
					Required: false,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type:    &openapi3.Types{openapi3.TypeInteger},
							Default: 1,
						},
					},
				},
			},
		},
	}

	params := mapParameters(path, &openapi3.PathItem{}, op)
	assert.Len(t, params, 1)
	p := params[0]
	assert.Equal(t, "page", p.Name)
	assert.True(t, p.PathParam, "should be detected in path template")
	assert.False(t, p.Positional, "should be reclassified as a flag due to default value")
}
