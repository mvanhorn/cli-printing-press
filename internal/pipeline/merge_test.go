package pipeline

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, MergeOverlay(apiSpec, overlay))

	assert.Equal(t, "Manage email messages", apiSpec.Resources["messages"].Description)
	assert.Equal(t, "me", apiSpec.Resources["messages"].Endpoints["list"].Params[0].Default)
	assert.Nil(t, apiSpec.Resources["messages"].Endpoints["list"].Params[1].Default)
}

func TestMergeOverlayNilSafe(t *testing.T) {
	require.NoError(t, MergeOverlay(nil, nil))
	require.NoError(t, MergeOverlay(&spec.APISpec{}, nil))
	require.NoError(t, MergeOverlay(nil, &SpecOverlay{}))
}

func TestMergeOverlayPublicParamNames(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name: "test",
		Resources: map[string]spec.Resource{
			"stores": {
				Endpoints: map[string]spec.Endpoint{
					"find": {
						Method: "GET",
						Path:   "/stores",
						Params: []spec.Param{
							{Name: "s", Type: "string", FlagName: "street-address", Aliases: []string{"street"}},
							{Name: "c", Type: "string"},
						},
						Body: []spec.Param{
							{Name: "delivery_window", Type: "string", FlagName: "window"},
						},
					},
				},
			},
		},
	}
	address := "address"
	cityAliases := []string{"c"}
	bodyAliases := []string{}
	overlay := &SpecOverlay{
		Resources: map[string]ResourceOverlay{
			"stores": {
				Endpoints: map[string]EndpointOverlay{
					"find": {
						Params: []ParamPatch{
							{Name: "s", FlagName: &address},
							{Name: "c", Aliases: &cityAliases},
						},
						Body: []ParamPatch{
							{Name: "delivery_window", ClearFlagName: true, Aliases: &bodyAliases},
						},
					},
				},
			},
		},
	}

	require.NoError(t, MergeOverlay(apiSpec, overlay))
	endpoint := apiSpec.Resources["stores"].Endpoints["find"]
	assert.Equal(t, "s", endpoint.Params[0].Name)
	assert.Equal(t, "address", endpoint.Params[0].FlagName)
	assert.Equal(t, []string{"street"}, endpoint.Params[0].Aliases)
	assert.Equal(t, "c", endpoint.Params[1].Name)
	assert.Equal(t, []string{"c"}, endpoint.Params[1].Aliases)
	assert.Empty(t, endpoint.Body[0].FlagName)
	assert.Empty(t, endpoint.Body[0].Aliases)
}

func TestMergeOverlayRejectsEmptyFlagName(t *testing.T) {
	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"stores": {
				Endpoints: map[string]spec.Endpoint{
					"find": {Params: []spec.Param{{Name: "s"}}},
				},
			},
		},
	}
	empty := ""
	overlay := &SpecOverlay{
		Resources: map[string]ResourceOverlay{
			"stores": {
				Endpoints: map[string]EndpointOverlay{
					"find": {Params: []ParamPatch{{Name: "s", FlagName: &empty}}},
				},
			},
		},
	}

	err := MergeOverlay(apiSpec, overlay)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flag_name must not be empty")
}
