package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeIntentToolDescriptionDropsUnimplementedThenClause(t *testing.T) {
	t.Parallel()

	api := minimalSpec("seats")
	api.Resources = map[string]spec.Resource{
		"availability": {
			Endpoints: map[string]spec.Endpoint{
				"search": {Method: "GET", Path: "/search", Description: "Search award availability"},
			},
		},
	}
	intent := spec.Intent{
		Name:        "find_best_award",
		Description: "Find the best award, then fetch bookable trip detail for the top result. Defaults to business",
		Params: []spec.IntentParam{
			{Name: "origin", Type: "string", Required: true, Description: "Origin airport"},
			{Name: "cabin", Type: "string", Description: "Cabin class"},
		},
		Steps: []spec.IntentStep{
			{Endpoint: "availability.search", Capture: "results"},
		},
	}

	got := composeIntentToolDescription(api, intent)
	assert.Contains(t, got, "Find the best award")
	assert.Contains(t, got, "Search award availability")
	assert.NotContains(t, got, "then fetch bookable trip detail")
	assert.NotContains(t, got, "Defaults to business")
}

func TestIntentParamsForEmitAppliesDocumentedDefault(t *testing.T) {
	t.Parallel()

	intent := spec.Intent{
		Name:        "find_best_award",
		Description: "Find the best award. Defaults to business",
		Params: []spec.IntentParam{
			{Name: "origin", Type: "string", Required: true, Description: "Origin airport"},
			{Name: "cabin", Type: "string", Description: "Cabin class"},
		},
		Steps: []spec.IntentStep{{Endpoint: "availability.search"}},
	}

	params := intentParamsForEmit(intent)
	require.Len(t, params, 2)
	assert.Equal(t, "business", params[1].Default)
	assert.Equal(t, `"business"`, intentParamDefaultGo(params[1]))
}

func TestIntentIsReadOnlyRequiresEveryStep(t *testing.T) {
	t.Parallel()

	api := minimalSpec("mix")
	api.Resources = map[string]spec.Resource{
		"items": {
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items"},
				"create": {Method: "POST", Path: "/items"},
				"delete": {Method: "DELETE", Path: "/items/{id}"},
			},
		},
	}

	assert.True(t, intentIsReadOnly(api, spec.Intent{
		Steps: []spec.IntentStep{{Endpoint: "items.list"}},
	}))
	assert.False(t, intentIsReadOnly(api, spec.Intent{
		Steps: []spec.IntentStep{{Endpoint: "items.list"}, {Endpoint: "items.create"}},
	}))
	assert.True(t, intentIsDestructive(api, spec.Intent{
		Steps: []spec.IntentStep{{Endpoint: "items.delete"}},
	}))
	assert.False(t, intentIsDestructive(api, spec.Intent{
		Steps: []spec.IntentStep{{Endpoint: "items.create"}},
	}))
}
