package pipeline

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestMergeOverlay(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name: "test",
		Resources: map[string]spec.Resource{
			"messages": {
				Description: "old description",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/messages",
						Description: "List messages",
						Params: []spec.Param{
							{Name: "userId", Type: "string", Required: true, Positional: true},
							{Name: "maxResults", Type: "integer"},
						},
					},
				},
			},
		},
	}

	newDesc := "Manage email messages"
	defaultUser := "me"

	overlay := &SpecOverlay{
		Resources: map[string]ResourceOverlay{
			"messages": {
				Description: &newDesc,
				Endpoints: map[string]EndpointOverlay{
					"list": {
						Params: []ParamPatch{
							{Name: "userId", Default: &defaultUser},
						},
					},
				},
			},
		},
	}

	MergeOverlay(apiSpec, overlay)

	assert.Equal(t, "Manage email messages", apiSpec.Resources["messages"].Description)
	assert.Equal(t, "me", apiSpec.Resources["messages"].Endpoints["list"].Params[0].Default)
	assert.Nil(t, apiSpec.Resources["messages"].Endpoints["list"].Params[1].Default)
}

func TestMergeOverlayNilSafe(t *testing.T) {
	MergeOverlay(nil, nil)
	MergeOverlay(&spec.APISpec{}, nil)
	MergeOverlay(nil, &SpecOverlay{})
}
