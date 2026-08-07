package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestIsCursorParam(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"cursor exact", "cursor", true},
		{"cursor uppercase", "CURSOR", true},
		{"min_cursor", "min_cursor", true},
		{"max_cursor", "max_cursor", true},
		{"next_cursor", "next_cursor", true},
		{"page", "page", true},
		{"page_token", "page_token", true},
		{"next_page_token", "next_page_token", true},
		{"min_time", "min_time", true},
		{"max_time", "max_time", true},
		{"offset", "offset", true},

		{"page_size is not a cursor", "page_size", false},
		{"limit is not a cursor", "limit", false},
		{"per_page is not a cursor", "per_page", false},
		{"id is not a cursor", "id", false},
		{"user_id is not a cursor", "user_id", false},
		{"threshold is not a cursor", "threshold", false},
		{"empty string", "", false},
		{"unrelated word", "handle", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCursorParam(tt.in))
		})
	}
}

func TestCobraFlagFuncForParamCursorOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		paramName string
		paramType string
		want      string
	}{
		{"cursor declared as number maps to StringVar", "cursor", "float", "StringVar"},
		{"cursor declared as integer maps to StringVar", "cursor", "int", "StringVar"},
		{"min_time as float maps to StringVar", "min_time", "float", "StringVar"},
		{"page as int maps to StringVar", "page", "int", "StringVar"},
		{"max_cursor as float maps to StringVar", "max_cursor", "float", "StringVar"},

		{"page_size keeps IntVar (not a cursor)", "page_size", "int", "IntVar"},
		{"threshold keeps Float64Var (not a cursor)", "threshold", "float", "Float64Var"},
		{"limit keeps IntVar (not a cursor)", "limit", "int", "IntVar"},

		{"user_id keeps StringVar via existing isIDParam path", "user_id", "int", "StringVar"},
		{"plain string flag unaffected", "handle", "string", "StringVar"},
		{"plain bool flag unaffected", "trim", "bool", "BoolVar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, cobraFlagFuncForParam(tt.paramName, tt.paramType))
		})
	}
	assert.Equal(t, "StringVar", cobraFlagFuncForParamRequired("all_day", "bool", true, false))
	assert.Equal(t, "BoolVar", cobraFlagFuncForParamRequired("all_day", "bool", true, true))
}

func TestGoTypeForParamCursorOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		paramName string
		paramType string
		want      string
	}{
		{"cursor float becomes string", "cursor", "float", "string"},
		{"min_time int becomes string", "min_time", "int", "string"},
		{"page int becomes string", "page", "int", "string"},

		{"page_size int stays int", "page_size", "int", "int"},
		{"threshold float stays float", "threshold", "float", "float64"},
		{"user_id int becomes string via isIDParam", "user_id", "int", "string"},
		{"integer alias maps to int", "first", "integer", "int"},
		{"boolean alias maps to bool", "published", "boolean", "bool"},
		{"number alias maps to float64", "cost", "number", "float64"},
		{"integer cursor alias still becomes string", "page", "integer", "string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, goTypeForParam(tt.paramName, tt.paramType))
		})
	}
	assert.Equal(t, "string", goTypeForParamRequired("all_day", "bool", true, false))
	assert.Equal(t, "bool", goTypeForParamRequired("all_day", "bool", true, true))
}

func TestCobraFlagFuncAcceptsSpecScalarAliases(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "IntVar", cobraFlagFunc("integer"))
	assert.Equal(t, "BoolVar", cobraFlagFunc("boolean"))
	assert.Equal(t, "Float64Var", cobraFlagFunc("number"))
	assert.Equal(t, "StringVar", cobraFlagFuncForParam("cursor", "integer"))
}

// TestMCPBindingFunc pins that OpenAPI-parsed shapes ("int", "float",
// "bool") and internal-spec literals ("integer", "number", "boolean")
// produce the same MCP binding, and that array/object params bind to their
// native MCP type (WithArray/WithObject) rather than the WithString default.
func TestMCPBindingFunc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typ  string
		want string
	}{
		{"integer", "WithNumber"},
		{"int", "WithNumber"},
		{"number", "WithNumber"},
		{"float", "WithNumber"},
		{"boolean", "WithBoolean"},
		{"bool", "WithBoolean"},
		{"string", "WithString"},
		{"", "WithString"},
		{"object", "WithObject"},
		{"array", "WithArray"},
		{"INTEGER", "WithNumber"},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, mcpBindingFunc(tt.typ))
		})
	}
}

// TestMCPParamDefaultValue pins that a composite (array/object) param default is
// serialized as JSON — matching how native array/object live values are now
// encoded — while scalar defaults keep their "%v" rendering. Without the JSON
// branch an array default would be injected on the wire as Go's "[a b c]".
func TestMCPParamDefaultValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		p      spec.Param
		want   string
		wantOK bool
	}{
		{"nil default", spec.Param{Type: "array"}, "", false},
		{"scalar int", spec.Param{Type: "integer", Default: 25}, "25", true},
		{"scalar string", spec.Param{Type: "string", Default: "later"}, "later", true},
		{"array default", spec.Param{Type: "array", Default: []any{"a", "b"}}, `["a","b"]`, true},
		{"object default", spec.Param{Type: "object", Default: map[string]any{"k": "v"}}, `{"k":"v"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mcpParamDefaultValue(tt.p)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultAndZeroValuesAcceptSpecScalarAliases(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "25", defaultVal(spec.Param{Name: "first", Type: "integer", Default: 25}))
	assert.Equal(t, "true", defaultVal(spec.Param{Name: "published", Type: "boolean", Default: true}))
	assert.Equal(t, "1.500000", defaultVal(spec.Param{Name: "cost", Type: "number", Default: 1.5}))
	assert.Equal(t, "0", zeroVal("integer"))
	assert.Equal(t, "false", zeroVal("boolean"))
	assert.Equal(t, "0.0", zeroVal("number"))
	assert.Equal(t, `""`, defaultValForParamRequired(spec.Param{Name: "all_day", Type: "boolean"}, true, false))
	assert.Equal(t, `""`, zeroValForParamRequired("all_day", "boolean", true, false))
}

func TestDefaultValForParamCursorOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    spec.Param
		want string
	}{
		{
			name: "cursor float with no default → empty string",
			p:    spec.Param{Name: "cursor", Type: "float"},
			want: `""`,
		},
		{
			name: "min_time int with no default → empty string",
			p:    spec.Param{Name: "min_time", Type: "int"},
			want: `""`,
		},
		{
			name: "page int with default 1 → quoted string",
			p:    spec.Param{Name: "page", Type: "int", Default: 1},
			want: `"1"`,
		},
		{
			name: "user_id int with default still routes through isIDParam",
			p:    spec.Param{Name: "user_id", Type: "int"},
			want: `""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, defaultValForParam(tt.p))
		})
	}
}
