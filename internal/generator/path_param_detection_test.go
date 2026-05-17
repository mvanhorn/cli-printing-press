package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestPreparePathParamDetection(t *testing.T) {
	s := &spec.APISpec{
		BaseURL: "https://{tenant}.api.com",
		Resources: map[string]spec.Resource{
			"widgets": {
				BaseURL: "/v1",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Path: "/items",
						Params: []spec.Param{
							{Name: "tenant"}, // Should be PathParam
							{Name: "q"},      // Should NOT be PathParam
						},
					},
				},
				SubResources: map[string]spec.Resource{
					"sub": {
						Endpoints: map[string]spec.Endpoint{
							"get": {
								Path: "/{subId}/details",
								Params: []spec.Param{
									{Name: "tenant"}, // Should be PathParam
									{Name: "subId"},  // Should be PathParam
								},
							},
						},
					},
				},
			},
		},
	}

	gen := &Generator{Spec: s}
	gen.prepare()

	// Check top-level resource endpoint
	list := s.Resources["widgets"].Endpoints["list"]
	for _, p := range list.Params {
		if p.Name == "tenant" {
			assert.True(t, p.PathParam, "tenant should be detected as PathParam in global BaseURL")
		}
		if p.Name == "q" {
			assert.False(t, p.PathParam, "q should NOT be detected as PathParam")
		}
	}

	// Check sub-resource endpoint
	subGet := s.Resources["widgets"].SubResources["sub"].Endpoints["get"]
	for _, p := range subGet.Params {
		if p.Name == "tenant" {
			assert.True(t, p.PathParam, "tenant should be detected as PathParam in global BaseURL for sub-resource")
		}
		if p.Name == "subId" {
			assert.True(t, p.PathParam, "subId should be detected as PathParam in endpoint path")
		}
	}
}
