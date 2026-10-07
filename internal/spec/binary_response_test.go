package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasBinaryResponse(t *testing.T) {
	t.Parallel()

	assert.False(t, (*APISpec)(nil).HasBinaryResponse())

	jsonOnly := &APISpec{
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{
				"list": {Method: "GET", Path: "/items"},
			}},
		},
	}
	assert.False(t, jsonOnly.HasBinaryResponse())

	binary := &APISpec{
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{
				"export": {Method: "GET", Path: "/items/export", ResponseFormat: ResponseFormatBinary},
			}},
		},
	}
	assert.True(t, binary.HasBinaryResponse())

	sub := &APISpec{
		Resources: map[string]Resource{
			"items": {SubResources: map[string]Resource{
				"files": {Endpoints: map[string]Endpoint{
					"download": {Method: "GET", Path: "/items/{id}/files/{file_id}", ResponseFormat: ResponseFormatBinary},
				}},
			}},
		},
	}
	assert.True(t, sub.HasBinaryResponse())
	assert.False(t, jsonOnly.HasBinaryResponse())
}
