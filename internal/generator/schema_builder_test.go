package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"camelCase", "camel_case"},
		{"kebab-case", "kebab_case"},
		{"snake_case", "snake_case"},
		{"PascalCase", "pascal_case"},
		{"movie_id", "movie_id"},
		// Dot-notation params (TMDb, Elasticsearch style)
		{"primary_release_date.gte", "primary_release_date_gte"},
		{"vote_average.gte", "vote_average_gte"},
		{"vote_average.lte", "vote_average_lte"},
		{"vote_count.gte", "vote_count_gte"},
		{"field.nested.deep", "field_nested_deep"},
		// Combined dots and hyphens
		{"with.dots-and-hyphens", "with_dots_and_hyphens"},
		// No transformation needed
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toSnakeCase(tt.input))
		})
	}
}

func TestSafeSQLNameQuotesUnsafeIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare identifier", in: "messages", want: "messages"},
		{name: "reserved word", in: "references", want: `"references"`},
		{name: "starts with digit", in: "0", want: `"0"`},
		{name: "derived starts with digit", in: "0_fts", want: `"0_fts"`},
		{name: "contains punctuation", in: "foo/bar", want: `"foo/bar"`},
		{name: "escapes quote", in: `foo"bar`, want: `"foo""bar"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safeSQLName(tt.in))
		})
	}
}

func TestCollectTextFieldNames(t *testing.T) {
	// Fields like tag/label/category/metadata should be picked up for FTS5
	// alongside the core text fields. Motivated by the ESPN retro where
	// "notes" (event tags) were unsearchable until manually added.
	mkFields := func(names ...string) []spec.TypeField {
		fields := make([]spec.TypeField, 0, len(names))
		for _, n := range names {
			fields = append(fields, spec.TypeField{Name: n, Type: "string"})
		}
		return fields
	}

	tests := []struct {
		name     string
		fields   []string
		wantIncl []string
		wantExcl []string
	}{
		{
			name:     "picks up core text fields",
			fields:   []string{"title", "description", "body"},
			wantIncl: []string{"title", "description", "body"},
		},
		{
			name:     "picks up tag-family fields",
			fields:   []string{"name", "tag", "tags", "label", "labels"},
			wantIncl: []string{"name", "tag", "tags", "label", "labels"},
		},
		{
			name:     "picks up category and metadata fields",
			fields:   []string{"title", "category", "categories", "metadata"},
			wantIncl: []string{"title", "category", "categories", "metadata"},
		},
		{
			name:     "picks up notes and note",
			fields:   []string{"note", "notes"},
			wantIncl: []string{"note", "notes"},
		},
		{
			name:     "ignores non-text fields",
			fields:   []string{"id", "created_at", "price"},
			wantExcl: []string{"id", "created_at", "price"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectTextFieldNamesFromFields(mkFields(tt.fields...))
			for _, want := range tt.wantIncl {
				assert.Contains(t, got, want)
			}
			for _, exc := range tt.wantExcl {
				assert.NotContains(t, got, exc)
			}
		})
	}
}

// TestBuildSchema_ColumnsFromResponseSchema pins that domain-table columns
// come from the GET endpoint's response schema (looked up via
// APISpec.Types[endpoint.Response.Item]) and never from request-side query
// or path parameters. Without this pin, a regression where columns mirror
// filter/sort/pagination params silently breaks every SQL-backed novel
// command, since sync can't populate columns the response doesn't contain.
func TestBuildSchema_ColumnsFromResponseSchema(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"issues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/issues",
						Params: []spec.Param{
							{Name: "filter", Type: "string"},
							{Name: "labels", Type: "string"},
							{Name: "sort", Type: "string"},
							{Name: "since", Type: "string", Format: "date-time"},
							{Name: "per_page", Type: "integer"},
							{Name: "page", Type: "integer"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Issue"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Issue": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "integer"},
					{Name: "number", Type: "integer"},
					{Name: "title", Type: "string"},
					{Name: "body", Type: "string"},
					{Name: "state", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
					{Name: "updated_at", Type: "string", Format: "date-time"},
				},
			},
		},
	}

	issues := findTable(BuildSchema(s), "issues")
	if !assert.NotNil(t, issues, "issues table should be emitted") {
		return
	}

	cols := map[string]string{}
	for _, c := range issues.Columns {
		cols[c.Name] = c.Type
	}

	for _, want := range []string{"number", "title", "body", "state", "created_at", "updated_at"} {
		assert.Contains(t, cols, want, "expected column %q from response schema", want)
	}
	for _, leak := range []string{"filter", "labels", "sort", "since", "per_page", "page"} {
		assert.NotContains(t, cols, leak, "request param %q must not appear as a column", leak)
	}
	assert.Equal(t, "DATETIME", cols["created_at"])
	assert.Equal(t, "DATETIME", cols["updated_at"])
}

// TestBuildSchema_ParamResponseNameOverlap asserts that when a request param
// and a response field share a name (common: "state"), the resulting column
// reflects the response field's *type* — not the param's — because the param
// is discarded entirely from column derivation. The fixture deliberately
// gives the param a different type from the response field so a regression
// where the param's type leaked into column emission would be caught.
func TestBuildSchema_ParamResponseNameOverlap(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"issues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Params: []spec.Param{
							// Request-side filter knob, declared as string.
							{Name: "state", Type: "string"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Issue"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Issue": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "integer"},
					{Name: "title", Type: "string"},
					// Response-side: state is an integer here, distinct from
					// the request-param string. Only the response type should
					// drive the emitted column.
					{Name: "state", Type: "integer"},
				},
			},
		},
	}

	issues := findTable(BuildSchema(s), "issues")
	if !assert.NotNil(t, issues) {
		return
	}

	stateCols := []ColumnDef{}
	for _, c := range issues.Columns {
		if c.Name == "state" {
			stateCols = append(stateCols, c)
		}
	}
	assert.Len(t, stateCols, 1, "exactly one state column should exist")
	if len(stateCols) == 1 {
		assert.Equal(t, "INTEGER", stateCols[0].Type,
			"state column type must come from the response field (integer), not the request param (string)")
	}
}

// TestBuildSchema_NoResponseTypeFallback asserts that when the GET endpoint's
// response item cannot be resolved against APISpec.Types — either because
// Response.Item names a type that isn't registered, or because Response.Item
// is empty (spec author left the response declaration off entirely) — the
// table degrades to id/data/synced_at. Hallucinating columns from request
// params would re-introduce the bug class the response-sourcing fix targets.
func TestBuildSchema_NoResponseTypeFallback(t *testing.T) {
	cases := []struct {
		name     string
		response spec.ResponseDef
		types    map[string]spec.TypeDef
	}{
		{
			name:     "Response.Item names an unregistered type",
			response: spec.ResponseDef{Type: "array", Item: "UnknownItem"},
			types:    map[string]spec.TypeDef{},
		},
		{
			name:     "Response.Item is empty (no response declared)",
			response: spec.ResponseDef{},
			types:    map[string]spec.TypeDef{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &spec.APISpec{
				Resources: map[string]spec.Resource{
					"issues": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method: "GET",
								Params: []spec.Param{
									{Name: "filter", Type: "string"},
									{Name: "page", Type: "integer"},
								},
								Response: tc.response,
							},
						},
					},
				},
				Types: tc.types,
			}

			issues := findTable(BuildSchema(s), "issues")
			if !assert.NotNil(t, issues) {
				return
			}

			names := make([]string, 0, len(issues.Columns))
			for _, c := range issues.Columns {
				names = append(names, c.Name)
			}
			assert.ElementsMatch(t, []string{"id", "data", "synced_at"}, names,
				"unresolved response type must yield only the base columns; got %v", names)
		})
	}
}

// findTable returns nil when no match exists so callers can render
// a clearer assertion failure than `tables[0]` panicking.
func findTable(tables []TableDef, name string) *TableDef {
	for i := range tables {
		if tables[i].Name == name {
			return &tables[i]
		}
	}
	return nil
}
