package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEndpointBinaryResponseDetection(t *testing.T) {
	tests := []struct {
		name            string
		contentTypes    []string
		wantBinary      bool
		wantJSON        bool
		wantAcceptFlag  bool
		wantDefaultMime string
	}{
		{
			name:            "unspecified defaults to JSON",
			wantBinary:      false,
			wantJSON:        true,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/octet-stream",
		},
		{
			name:            "plain JSON is not binary",
			contentTypes:    []string{"application/json"},
			wantBinary:      false,
			wantJSON:        true,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/octet-stream",
		},
		{
			name:            "problem JSON suffix is JSON",
			contentTypes:    []string{"application/problem+json"},
			wantBinary:      false,
			wantJSON:        true,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/octet-stream",
		},
		{
			name:            "PDF only is binary",
			contentTypes:    []string{"application/pdf"},
			wantBinary:      true,
			wantJSON:        false,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/pdf",
		},
		{
			name:            "XML alternative stays structured",
			contentTypes:    []string{"application/json", "application/xml"},
			wantBinary:      false,
			wantJSON:        true,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/octet-stream",
		},
		{
			name:            "JSON plus PDF needs accept chooser and defaults binary",
			contentTypes:    []string{"application/json", "application/pdf"},
			wantBinary:      true,
			wantJSON:        true,
			wantAcceptFlag:  true,
			wantDefaultMime: "application/pdf",
		},
		{
			name:            "normalizes parameters",
			contentTypes:    []string{"Application/Vnd.Openxmlformats-Officedocument.Spreadsheetml.Sheet; charset=binary"},
			wantBinary:      true,
			wantJSON:        false,
			wantAcceptFlag:  false,
			wantDefaultMime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := Endpoint{ResponseContentTypes: tt.contentTypes}
			assert.Equal(t, tt.wantBinary, endpoint.HasBinaryResponse())
			assert.Equal(t, tt.wantJSON, endpoint.HasJSONResponse())
			assert.Equal(t, tt.wantAcceptFlag, endpoint.NeedsAcceptFlag())
			assert.Equal(t, tt.wantDefaultMime, endpoint.DefaultBinaryAccept())
		})
	}
}

func TestAPISpecHasBinaryResponsesWalksSubResources(t *testing.T) {
	api := &APISpec{
		Resources: map[string]Resource{
			"invoices": {
				Endpoints: map[string]Endpoint{
					"get": {ResponseContentTypes: []string{"application/json"}},
				},
				SubResources: map[string]Resource{
					"document": {
						Endpoints: map[string]Endpoint{
							"print": {ResponseContentTypes: []string{"application/pdf"}},
						},
					},
				},
			},
		},
	}

	assert.True(t, api.HasBinaryResponses())
	assert.False(t, (&APISpec{Resources: map[string]Resource{
		"pets": {Endpoints: map[string]Endpoint{
			"get": {ResponseContentTypes: []string{"application/json", "application/xml"}},
		}},
	}}).HasBinaryResponses())
}
