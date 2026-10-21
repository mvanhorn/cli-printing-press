package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCursorRequestParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pag      Pagination
		params   []Param
		body     []Param
		want     string
		wantErr  string
		wantErr2 string
	}{
		{
			name: "exact offset",
			pag:  Pagination{Type: "cursor", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", Type: "string"},
			},
			want: "offset",
		},
		{
			name: "exact cursor",
			pag:  Pagination{Type: "cursor", NextCursorPath: "cursor"},
			params: []Param{
				{Name: "limit", Type: "integer"},
				{Name: "cursor", Type: "string"},
			},
			want: "cursor",
		},
		{
			name: "preserves wire casing",
			pag:  Pagination{Type: "cursor", NextCursorPath: "pagetoken"},
			params: []Param{
				{Name: "PageToken", Type: "string"},
			},
			want: "PageToken",
		},
		{
			name: "url name wins over flag name",
			pag:  Pagination{Type: "cursor", NextCursorPath: "cursor"},
			params: []Param{
				{Name: "cursor", URLName: "$cursor", Type: "string"},
			},
			want: "$cursor",
		},
		{
			name: "dotted leaf matches exactly",
			pag:  Pagination{Type: "cursor", NextCursorPath: "pageInfo.endCursor"},
			params: []Param{
				{Name: "after", Type: "string"},
				{Name: "endCursor", Type: "string"},
				{Name: "first", Type: "integer"},
			},
			want: "endCursor",
		},
		{
			name: "unique cursor-shaped param",
			pag:  Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "pagination_limit", Type: "integer"},
				{Name: "pagination_cursor", Type: "string"},
			},
			want: "pagination_cursor",
		},
		{
			name: "next cursor falls back to after",
			pag:  Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "after", Type: "string"},
			},
			want: "after",
		},
		{
			name: "exact leaf wins over other cursor params",
			pag:  Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "pagination_cursor", Type: "string"},
				{Name: "next_cursor", Type: "string"},
			},
			want: "next_cursor",
		},
		{
			name: "page token field matches pageToken",
			pag:  Pagination{Type: "page_token", NextCursorPath: "nextPageToken"},
			params: []Param{
				{Name: "pageToken", Type: "string"},
			},
			want: "pageToken",
		},
		{
			name: "explicit cursor param stays",
			pag:  Pagination{Type: "cursor", CursorParam: "after", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "cursor", Type: "string"},
			},
			want: "after",
		},
		{
			name: "explicit unknown cursor param stays",
			pag:  Pagination{Type: "cursor", CursorParam: "not-declared", NextCursorPath: "next_cursor"},
			want: "not-declared",
		},
		{
			name: "explicit spaced cursor param stays",
			pag:  Pagination{Type: "cursor", CursorParam: " after ", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "cursor", Type: "string"},
			},
			want: " after ",
		},
		{
			name: "whitespace cursor param is empty",
			pag:  Pagination{Type: "cursor", CursorParam: "   ", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", Type: "string"},
			},
			want: "offset",
		},
		{
			name: "offset pagination is unchanged",
			pag:  Pagination{Type: "offset", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", Type: "string"},
			},
		},
		{
			name: "page pagination is unchanged",
			pag:  Pagination{Type: "page", NextCursorPath: "page"},
			params: []Param{
				{Name: "page", Type: "string"},
			},
		},
		{
			name: "id walk is unchanged",
			pag:  Pagination{Type: PaginationTypeIDWalk, NextCursorPath: "next"},
			params: []Param{
				{Name: "next", Type: "string"},
			},
		},
		{
			name: "none is unchanged",
			pag:  Pagination{Type: PaginationTypeNone, NextCursorPath: "next"},
			params: []Param{
				{Name: "next", Type: "string"},
			},
		},
		{
			name: "unknown type is unchanged",
			pag:  Pagination{Type: "link", NextCursorPath: "next"},
			params: []Param{
				{Name: "next", Type: "string"},
			},
		},
		{
			name: "cursor without a response path stays empty",
			pag:  Pagination{Type: "cursor", LimitParam: "limit"},
			params: []Param{
				{Name: "limit", Type: "integer"},
			},
		},
		{
			name: "empty type uses the response path",
			pag:  Pagination{NextCursorPath: "cursor"},
			params: []Param{
				{Name: "cursor", Type: "string"},
			},
			want: "cursor",
		},
		{
			name: "type casing and spaces",
			pag:  Pagination{Type: " Cursor ", NextCursorPath: "cursor"},
			params: []Param{
				{Name: "cursor", Type: "string"},
			},
			want: "cursor",
		},
		{
			name: "page token type with spaces",
			pag:  Pagination{Type: " page_token ", NextCursorPath: "page_token"},
			params: []Param{
				{Name: "page_token", Type: "string"},
			},
			want: "page_token",
		},
		{
			name: "integer offset param",
			pag:  Pagination{Type: "cursor", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", Type: "integer"},
			},
			want: "offset",
		},
		{
			name: "explicit query location",
			pag:  Pagination{Type: "cursor", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", In: "QUERY", Type: "string"},
			},
			want: "offset",
		},
		{
			name: "blank param does not block a match",
			pag:  Pagination{Type: "cursor", NextCursorPath: "offset"},
			params: []Param{
				{Name: "", Type: "string"},
				{Name: "offset", Type: "string"},
			},
			want: "offset",
		},
		{
			name: "after falls back to starting_after",
			pag:  Pagination{Type: "cursor", NextCursorPath: "after"},
			params: []Param{
				{Name: "starting_after", Type: "string"},
			},
			want: "starting_after",
		},
		{
			name: "continuation falls back to continuation token",
			pag:  Pagination{Type: "cursor", NextCursorPath: "continuation"},
			params: []Param{
				{Name: "continuation_token", Type: "string"},
			},
			want: "continuation_token",
		},
		{
			name: "digit inside a cursor field still falls back",
			pag:  Pagination{Type: "cursor", NextCursorPath: "cursor2"},
			params: []Param{
				{Name: "after", Type: "string"},
			},
			want: "after",
		},
		{
			name: "leading underscore is a simple cursor field",
			pag:  Pagination{Type: "cursor", NextCursorPath: "_cursor"},
			params: []Param{
				{Name: "pagination_cursor", Type: "string"},
			},
			want: "pagination_cursor",
		},
		{
			name: "unicode letter stays a simple cursor field",
			pag:  Pagination{Type: "cursor", NextCursorPath: "cursoré"},
			params: []Param{
				{Name: "pagination_cursor", Type: "string"},
			},
			want: "pagination_cursor",
		},
		{
			name:    "dotted path does not invent after",
			pag:     Pagination{Type: "cursor", NextCursorPath: "pageInfo.endCursor"},
			params:  []Param{{Name: "after", Type: "string"}, {Name: "first", Type: "integer"}},
			wantErr: "pageInfo.endCursor",
		},
		{
			name:     "ambiguous cursor params",
			pag:      Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params:   []Param{{Name: "after", Type: "string"}, {Name: "cursor", Type: "string"}},
			wantErr:  "next_cursor",
			wantErr2: "after, cursor",
		},
		{
			name:    "offset does not fall back",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "pagination_cursor", Type: "string"}},
			wantErr: "offset",
		},
		{
			name:    "page field does not fall back",
			pag:     Pagination{Type: "cursor", NextCursorPath: "page"},
			params:  []Param{{Name: "pagination_cursor", Type: "string"}},
			wantErr: "page",
		},
		{
			name:    "next cursor with no params",
			pag:     Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			wantErr: "next_cursor",
		},
		{
			name:    "cursor with no params",
			pag:     Pagination{Type: "cursor", NextCursorPath: "cursor"},
			wantErr: "cursor",
		},
		{
			name:    "header cursor is not a query param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "cursor"},
			params:  []Param{{Name: "cursor", In: "header", Type: "string"}},
			wantErr: "cursor",
		},
		{
			name:    "cookie cursor is not a query param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "cursor"},
			params:  []Param{{Name: "cursor", In: "cookie", Type: "string"}},
			wantErr: "cursor",
		},
		{
			name:    "date formatted after is not a cursor",
			pag:     Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params:  []Param{{Name: "after", Type: "string", Format: "date-time"}},
			wantErr: "next_cursor",
		},
		{
			name: "date formatted after yields to a real cursor param",
			pag:  Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params: []Param{
				{Name: "after", Type: "string", Format: "date"},
				{Name: "pagination_cursor", Type: "string"},
			},
			want: "pagination_cursor",
		},
		{
			name:    "date formatted exact name is not reused",
			pag:     Pagination{Type: "cursor", NextCursorPath: "after"},
			params:  []Param{{Name: "after", Type: "string", Format: "date-time"}},
			wantErr: "after",
		},
		{
			name:    "positional offset is not a query param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "offset", Type: "string", Positional: true}},
			wantErr: "offset",
		},
		{
			name:    "path offset is not a query param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "offset", Type: "string", PathParam: true}},
			wantErr: "offset",
		},
		{
			name:    "flag name is not the request name",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "page_size", FlagName: "offset", Type: "string"}},
			wantErr: "offset",
		},
		{
			name:    "body field is not a query param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			body:    []Param{{Name: "offset", Type: "string"}},
			wantErr: "offset",
		},
		{
			name:    "array offset is not a cursor param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "offset", Type: "Array"}},
			wantErr: "offset",
		},
		{
			name:    "object offset is not a cursor param",
			pag:     Pagination{Type: "cursor", NextCursorPath: "offset"},
			params:  []Param{{Name: "offset", Type: "object"}},
			wantErr: "offset",
		},
		{
			name: "two wires for one leaf",
			pag:  Pagination{Type: "cursor", NextCursorPath: "offset"},
			params: []Param{
				{Name: "offset", Type: "string"},
				{Name: "Offset", URLName: "pageOffset", Type: "string"},
			},
			wantErr:  "offset",
			wantErr2: "offset, pageOffset",
		},
		{
			name:    "hyphenated field does not fall back",
			pag:     Pagination{Type: "cursor", NextCursorPath: "next-cursor"},
			params:  []Param{{Name: "pagination_cursor", Type: "string"}},
			wantErr: "next-cursor",
		},
		{
			name: "hyphenated field can match exactly",
			pag:  Pagination{Type: "cursor", NextCursorPath: "next-cursor"},
			params: []Param{
				{Name: "next-cursor", Type: "string"},
			},
			want: "next-cursor",
		},
		{
			name:    "leading digit is not a simple field",
			pag:     Pagination{Type: "cursor", NextCursorPath: "1cursor"},
			params:  []Param{{Name: "pagination_cursor", Type: "string"}},
			wantErr: "1cursor",
		},
		{
			name:    "access token is not cursor-shaped",
			pag:     Pagination{Type: "cursor", NextCursorPath: "next_cursor"},
			params:  []Param{{Name: "access_token", Type: "string"}},
			wantErr: "next_cursor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pag := tt.pag
			spec := &APISpec{Resources: map[string]Resource{
				"items": {Endpoints: map[string]Endpoint{
					"list": {Pagination: &pag, Params: tt.params, Body: tt.body},
					"get":  {Method: "GET", Path: "/items/{id}"},
				}},
			}}
			err := spec.ResolveCursorRequestParams()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.ErrorContains(t, err, "cursor_param")
				if tt.wantErr2 != "" {
					require.ErrorContains(t, err, tt.wantErr2)
				}
				if tt.wantErr2 != "" {
					require.ErrorContains(t, err, "more than one query parameter")
				} else {
					require.ErrorContains(t, err, "no request parameter")
				}
				assert.Equal(t, tt.pag.CursorParam, pag.CursorParam)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, pag.CursorParam)
			assert.Equal(t, tt.pag.Type, pag.Type)
		})
	}
}

func TestResolveCursorRequestParamsNilAndEmpty(t *testing.T) {
	t.Parallel()

	var spec *APISpec
	require.NoError(t, spec.ResolveCursorRequestParams())
	require.NoError(t, (&APISpec{}).ResolveCursorRequestParams())
}

func TestResolveCursorRequestParamsIsIdempotent(t *testing.T) {
	t.Parallel()

	pag := &Pagination{Type: "cursor", NextCursorPath: "offset"}
	spec := &APISpec{Resources: map[string]Resource{
		"items": {Endpoints: map[string]Endpoint{
			"list": {Pagination: pag, Params: []Param{{Name: "offset", Type: "string"}}},
		}},
	}}
	require.NoError(t, spec.ResolveCursorRequestParams())
	require.Equal(t, "offset", pag.CursorParam)
	require.NoError(t, spec.ResolveCursorRequestParams())
	assert.Equal(t, "offset", pag.CursorParam)

	pag.CursorParam = "explicit"
	require.NoError(t, spec.ResolveCursorRequestParams())
	assert.Equal(t, "explicit", pag.CursorParam)
}

func TestResolveCursorRequestParamsSubresource(t *testing.T) {
	t.Parallel()

	pag := &Pagination{Type: "cursor", NextCursorPath: "cursor"}
	spec := &APISpec{Resources: map[string]Resource{
		"teams": {SubResources: map[string]Resource{
			"members": {SubResources: map[string]Resource{
				"roles": {Endpoints: map[string]Endpoint{
					"list": {Pagination: pag, Params: []Param{{Name: "cursor", Type: "string"}}},
				}},
			}},
		}},
	}}
	require.NoError(t, spec.ResolveCursorRequestParams())
	assert.Equal(t, "cursor", pag.CursorParam)

	bad := &Pagination{Type: "cursor", NextCursorPath: "pageInfo.endCursor"}
	spec.Resources["teams"].SubResources["members"].SubResources["roles"].Endpoints["list"] = Endpoint{
		Pagination: bad,
		Params:     []Param{{Name: "after", Type: "string"}},
	}
	err := spec.ResolveCursorRequestParams()
	require.ErrorContains(t, err, `resource "teams" sub-resource "members/roles" endpoint "list"`)
	require.ErrorContains(t, err, "pageInfo.endCursor")
	assert.Empty(t, bad.CursorParam)
}

func TestResolveCursorRequestParamsDoesNotPartiallyFill(t *testing.T) {
	t.Parallel()

	good := &Pagination{Type: "cursor", NextCursorPath: "offset"}
	dotted := &Pagination{Type: "cursor", NextCursorPath: "pageInfo.endCursor"}
	ambiguous := &Pagination{Type: "cursor", NextCursorPath: "next_cursor"}
	spec := &APISpec{Resources: map[string]Resource{
		"alpha": {Endpoints: map[string]Endpoint{
			"list": {
				Pagination: dotted,
				Params:     []Param{{Name: "after", Type: "string"}, {Name: "first", Type: "integer"}},
			},
		}},
		"middle": {Endpoints: map[string]Endpoint{
			"list": {
				Pagination: good,
				Params:     []Param{{Name: "offset", Type: "string"}},
			},
		}},
		"zeta": {Endpoints: map[string]Endpoint{
			"list": {
				Pagination: ambiguous,
				Params:     []Param{{Name: "cursor", Type: "string"}, {Name: "after", Type: "string"}},
			},
		}},
	}}

	err := spec.ResolveCursorRequestParams()
	require.EqualError(t, err, strings.Join([]string{
		"cursor pagination is missing a request parameter:",
		`resource "alpha" endpoint "list": cursor pagination response path "pageInfo.endCursor" has no request parameter; set cursor_param to the query parameter that sends the cursor back on follow-up requests`,
		`resource "zeta" endpoint "list": cursor pagination response path "next_cursor" matches more than one query parameter (after, cursor); set cursor_param explicitly`,
	}, "\n"))
	assert.Empty(t, good.CursorParam)
	assert.Empty(t, dotted.CursorParam)
	assert.Empty(t, ambiguous.CursorParam)
}

func TestParseCursorFieldFillsRequestParam(t *testing.T) {
	t.Parallel()

	parsed, err := ParseBytes([]byte(`
name: offset-cursor
base_url: https://api.example.com
auth:
  type: none
resources:
  records:
    endpoints:
      list:
        method: GET
        path: /records
        params:
          - name: offset
            type: string
        pagination:
          type: cursor
          cursor_field: offset
`))
	require.NoError(t, err)
	pag := parsed.Resources["records"].Endpoints["list"].Pagination
	require.NotNil(t, pag)
	assert.Equal(t, "offset", pag.CursorParam)
	assert.Equal(t, "offset", pag.NextCursorPath)
	assert.Equal(t, "cursor", pag.Type)
}

func TestParseOmittedPaginationTypeFillsRequestParam(t *testing.T) {
	t.Parallel()

	parsed, err := ParseBytes([]byte(`
name: omitted-cursor-type
base_url: https://api.example.com
auth:
  type: none
resources:
  records:
    endpoints:
      list:
        method: GET
        path: /records
        params:
          - name: cursor
            type: string
        pagination:
          cursor_field: cursor
`))
	require.NoError(t, err)
	pag := parsed.Resources["records"].Endpoints["list"].Pagination
	require.NotNil(t, pag)
	assert.Empty(t, pag.Type)
	assert.Equal(t, "cursor", pag.CursorParam)
	assert.Equal(t, "cursor", pag.NextCursorPath)
}

func TestParseDottedCursorFieldWithoutRequestParam(t *testing.T) {
	t.Parallel()

	_, err := ParseBytes([]byte(`
name: dotted-cursor
base_url: https://api.example.com
auth:
  type: none
resources:
  nodes:
    endpoints:
      list:
        method: GET
        path: /graphql
        params:
          - name: first
            type: integer
          - name: after
            type: string
        pagination:
          type: cursor
          cursor_field: pageInfo.endCursor
`))
	require.ErrorContains(t, err, "validation:")
	require.ErrorContains(t, err, "pageInfo.endCursor")
	require.ErrorContains(t, err, "cursor_param")
	require.ErrorContains(t, err, "no request parameter")
	assert.NotContains(t, err.Error(), "after,")
}

func TestParseAmbiguousCursorField(t *testing.T) {
	t.Parallel()

	_, err := ParseBytes([]byte(`
name: ambiguous-cursor
base_url: https://api.example.com
auth:
  type: none
resources:
  items:
    endpoints:
      list:
        method: GET
        path: /items
        params:
          - name: after
            type: string
          - name: cursor
            type: string
        pagination:
          type: cursor
          cursor_field: next_cursor
`))
	require.ErrorContains(t, err, "validation:")
	require.ErrorContains(t, err, "next_cursor")
	require.ErrorContains(t, err, "after, cursor")
}
