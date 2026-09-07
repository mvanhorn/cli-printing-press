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

func TestIntentParamDefaultGoRejectsInvalidTypedLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		param   spec.IntentParam
		wantGo  string
		wantDef string
	}{
		{
			name:    "integer parses",
			param:   spec.IntentParam{Name: "limit", Type: "integer", Default: "12"},
			wantGo:  "12",
			wantDef: "12",
		},
		{
			name:    "integer canonicalizes",
			param:   spec.IntentParam{Name: "limit", Type: "integer", Default: "-03"},
			wantGo:  "-3",
			wantDef: "-3",
		},
		{
			name:    "invalid integer is not a Go literal",
			param:   spec.IntentParam{Name: "limit", Type: "integer", Default: "12x"},
			wantGo:  `"12x"`,
			wantDef: "",
		},
		{
			name:    "bool true tokens",
			param:   spec.IntentParam{Name: "include", Type: "boolean", Default: "yes"},
			wantGo:  "true",
			wantDef: "true",
		},
		{
			name:    "bool false tokens stay false",
			param:   spec.IntentParam{Name: "include", Type: "boolean", Default: "false"},
			wantGo:  "false",
			wantDef: "false",
		},
		{
			name:    "unrecognized bool is not coerced to false",
			param:   spec.IntentParam{Name: "include", Type: "boolean", Default: "maybe"},
			wantGo:  `"maybe"`,
			wantDef: "",
		},
		{
			name:    "string stays quoted",
			param:   spec.IntentParam{Name: "cabin", Type: "string", Default: "business"},
			wantGo:  `"business"`,
			wantDef: "business",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intentParamsForEmit(spec.Intent{Params: []spec.IntentParam{tt.param}})
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantDef, got[0].Default)
			assert.Equal(t, tt.wantGo, intentParamDefaultGo(tt.param))
		})
	}
}

func TestIntentParamsForEmitDropsInvalidDocumentedIntegerDefault(t *testing.T) {
	t.Parallel()

	got := intentParamsForEmit(spec.Intent{
		Params: []spec.IntentParam{{
			Name:        "limit",
			Type:        "integer",
			Description: "Page size. Defaults to 12x",
		}},
	})
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Default)
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
	assert.False(t, intentIsDestructive(api, spec.Intent{
		Steps: []spec.IntentStep{{Endpoint: "items.list"}, {Endpoint: "items.create"}},
	}))
}
