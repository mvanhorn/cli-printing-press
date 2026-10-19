package profiler

import (
	"math"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncQueryParamDefaultsExcludeSyncOwnedKeys pins which spec defaults reach
// the syncer. The syncer assigns the paging, since, sort, and date-range keys
// itself, and does so inside conditionals -- a seeded default would survive the
// branch where it deliberately withheld the key. Everything else is scoping the
// endpoint command already sends, and must be seeded so both surfaces address
// the same slice of the API.
func TestSyncQueryParamDefaultsExcludeSyncOwnedKeys(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/tickets",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
			{Name: "updated_since", In: "query", Type: "string", Default: "2020-01-01"},
			{Name: "sort", In: "query", Type: "string", Default: "updated_at:asc"},
			{Name: "dates", In: "query", Type: "string", Default: "last_30_days"},
			{Name: "cursor", In: "query", Type: "string", Default: "abc"},
			{Name: "limit", In: "query", Type: "integer", Default: 25},
			{Name: "include_closed", In: "query", Type: "boolean", Default: true},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		cursor:    "cursor",
		limit:     "limit",
		since:     "updated_since",
		sort:      "sort",
		dateRange: syncDateRangeParamNames,
	})

	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "workspace-id", Value: "ws-42"},
		{Name: "include_closed", Value: "true"},
	}, got)
}

// TestSyncQueryParamDefaultsFromEndpointHonorsDetectedSort proves the sort
// exclusion is wired to the same detection the generated syncResourceSortParam
// is emitted from, rather than to a hardcoded name, so the two cannot drift.
func TestSyncQueryParamDefaultsFromEndpointHonorsDetectedSort(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/tickets",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "updated_since", In: "query", Type: "string"},
			{Name: "order_by", In: "query", Type: "string", Default: "updated_at:asc"},
		},
	}

	sortParam, sortValue := detectEndpointSyncSort(endpoint)
	assert.Equal(t, "order_by", sortParam, "an ascending last-modified sort must be detected")
	assert.Equal(t, "updated_at:asc", sortValue)

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		since:     "updated_since",
		sort:      sortParam,
		dateRange: syncDateRangeParamNames,
	})
	assert.Empty(t, got, "the detected sort param must not be seeded under any spelling")
}

func TestSyncQueryParamDefaultsWidenOpenStatusFilter(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "all"}},
			{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "status", Value: "all"},
		{Name: "workspace-id", Value: "ws-42"},
	}, got)
}

func TestSyncQueryParamDefaultsWidenOpenStateToAny(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "any"}},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "any"}}, got)
}

func TestSyncQueryParamDefaultsDoNotWidenActiveOrUnrelatedDefaults(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/subscriptions",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "active", Enum: []string{"active", "canceled", "all"}},
			{Name: "sort", In: "query", Type: "string", Default: "new"},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "status", Value: "active"},
		{Name: "sort", Value: "new"},
	}, got)
}

func TestSyncQueryParamSeedHiddenHistoryWhenEnumHasNoAll(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed"}},
		},
	}

	seed := syncQueryParamSeedFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.Defaults)
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.HiddenHistory)
}

func TestSyncParamsOverlayWinsAndSuppressesHiddenHistory(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "all"}},
		},
		SyncParams: map[string]string{"status": "open"},
	}

	seed := syncQueryParamSeedFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.Defaults)
	assert.Empty(t, seed.HiddenHistory, "explicit sync_params status=open is the intended scope")
}

func TestSyncParamsAddsAllHistoryWithoutSpecDefault(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Enum: []string{"open", "closed", "all"}},
		},
		SyncParams: map[string]string{"status": "all"},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{limit: "limit"})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "all"}}, got)
}

func TestSyncQueryParamDefaultsKeepFirstPageCursorDefault(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/bounces",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "offset", In: "query", Type: "integer", Required: true, Default: 0},
			{Name: "count", In: "query", Type: "integer", Default: 25},
			{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		cursor:     "offset",
		cursorType: "offset",
		limit:      "count",
	})
	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "offset", Value: "0"},
		{Name: "workspace-id", Value: "ws-42"},
	}, got)
}

func TestSyncQueryParamDefaultsStillExcludeTokenCursor(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method: "GET",
		Path:   "/charges",
		Params: []spec.Param{
			{Name: "starting_after", In: "query", Type: "string", Required: true, Default: "ch_1"},
			{Name: "limit", In: "query", Type: "integer", Default: 10},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		cursor:     "starting_after",
		cursorType: "cursor",
		limit:      "limit",
	})
	assert.Empty(t, got)
}

func TestRequiredSyncQueryParamsIncludesFirstPageCursor(t *testing.T) {
	t.Parallel()

	offset := spec.Endpoint{
		Method: "GET",
		Path:   "/bounces",
		Params: []spec.Param{
			{Name: "offset", In: "query", Type: "integer", Required: true, Default: 0},
			{Name: "count", In: "query", Type: "integer", Required: true, Default: 2},
			{Name: "since", In: "query", Type: "string", Required: true},
		},
	}
	assert.Equal(t, []string{"offset", "since"}, requiredSyncQueryParamsFromEndpoint(offset, syncOwnedParams{
		cursor:     "offset",
		cursorType: "offset",
		limit:      "count",
		since:      "since",
		dateRange:  syncDateRangeParamNames,
	}, "/bounces"))

	page := spec.Endpoint{
		Method: "GET",
		Path:   "/commands",
		Params: []spec.Param{
			{Name: "page", In: "query", Type: "integer", Required: true},
			{Name: "per_page", In: "query", Type: "integer", Required: true},
		},
	}
	assert.Equal(t, []string{"page"}, requiredSyncQueryParamsFromEndpoint(page, syncOwnedParams{
		cursor:     "page",
		cursorType: "page",
		limit:      "per_page",
	}, "/commands"))

	token := spec.Endpoint{
		Method: "GET",
		Path:   "/charges",
		Params: []spec.Param{
			{Name: "starting_after", In: "query", Type: "string", Required: true},
			{Name: "limit", In: "query", Type: "integer", Required: true},
		},
	}
	assert.Empty(t, requiredSyncQueryParamsFromEndpoint(token, syncOwnedParams{
		cursor:     "starting_after",
		cursorType: "cursor",
		limit:      "limit",
	}, "/charges"))

	pageToken := spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		Params: []spec.Param{
			{Name: "page_token", In: "query", Type: "string", Required: true},
			{Name: "page_size", In: "query", Type: "integer", Required: true},
		},
	}
	assert.Empty(t, requiredSyncQueryParamsFromEndpoint(pageToken, syncOwnedParams{
		cursor:     "page_token",
		cursorType: "page_token",
		limit:      "page_size",
	}, "/items"))
}

func TestSyncPaginationInitialCursor(t *testing.T) {
	t.Parallel()

	min := func(v float64) *float64 { return &v }
	nan := math.NaN()
	posInf := math.Inf(1)

	tests := []struct {
		name   string
		end    spec.Endpoint
		cursor string
		typ    string
		want   string
	}{
		{
			name:   "offset default zero int",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Default: 0}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "offset default zero float",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Default: float64(0)}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "default wins over a higher minimum",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Default: 0, Minimum: min(5)}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "required page without a default",
			end:    cursorEndpoint(spec.Param{Name: "page", In: "query", Type: "integer", Required: true}),
			cursor: "page", typ: "page", want: "1",
		},
		{
			name:   "required page minimum",
			end:    cursorEndpoint(spec.Param{Name: "page", In: "query", Type: "integer", Required: true, Minimum: min(3)}),
			cursor: "page", typ: "page", want: "3",
		},
		{
			name:   "zero-based page",
			end:    cursorEndpoint(spec.Param{Name: "page", In: "query", Type: "integer", Required: true, Minimum: min(0)}),
			cursor: "page", typ: "page", want: "0",
		},
		{
			name:   "one-based offset minimum",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Minimum: min(1)}),
			cursor: "offset", typ: "offset", want: "1",
		},
		{
			name:   "non-finite minimum falls through to offset zero",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Minimum: &nan}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "infinite minimum falls through to offset zero",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Minimum: &posInf}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "optional offset without a default",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer"}),
			cursor: "offset", typ: "offset", want: "",
		},
		{
			name:   "optional offset with default zero",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Default: 0, ServerDefault: true}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "empty string default is not a position",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true, Default: ""}),
			cursor: "offset", typ: "offset", want: "0",
		},
		{
			name:   "token cursor",
			end:    cursorEndpoint(spec.Param{Name: "starting_after", In: "query", Type: "string", Required: true}),
			cursor: "starting_after", typ: "cursor", want: "",
		},
		{
			name:   "page token",
			end:    cursorEndpoint(spec.Param{Name: "page_token", In: "query", Type: "string", Required: true}),
			cursor: "page_token", typ: "page_token", want: "",
		},
		{
			name:   "missing cursor type",
			end:    cursorEndpoint(spec.Param{Name: "offset", In: "query", Type: "integer", Required: true}),
			cursor: "offset", typ: "", want: "",
		},
		{
			name:   "cursor param absent",
			end:    cursorEndpoint(spec.Param{Name: "limit", In: "query", Type: "integer", Required: true}),
			cursor: "offset", typ: "offset", want: "",
		},
		{
			name: "body offset",
			end: spec.Endpoint{Method: "POST", Path: "/search", Body: []spec.Param{
				{Name: "offset", In: "body", Type: "integer", Required: true},
			}},
			cursor: "offset", typ: "offset", want: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, syncPaginationInitialCursorFromEndpoint(tt.end, tt.cursor, tt.typ))
		})
	}
}

func cursorEndpoint(param spec.Param) spec.Endpoint {
	return spec.Endpoint{Method: "GET", Path: "/items", Params: []spec.Param{param}}
}

func TestSyncPaginationDefaultsKeepTokenNameOffOffsetDefault(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:     "GET",
		Path:       "/charges",
		Pagination: &spec.Pagination{Type: "offset", LimitParam: "limit"},
		Params: []spec.Param{
			{Name: "starting_after", In: "query", Type: "string", Required: true},
			{Name: "limit", In: "query", Type: "integer", Default: 2},
		},
	}

	cursor, cursorType, limit, pageSize := syncPaginationDefaultsFromEndpoint(endpoint)
	assert.Equal(t, "starting_after", cursor)
	assert.Equal(t, "cursor", cursorType)
	assert.Equal(t, "limit", limit)
	assert.Equal(t, 2, pageSize)
	assert.Empty(t, syncPaginationInitialCursorFromEndpoint(endpoint, cursor, cursorType))
	assert.NotContains(t, requiredSyncQueryParamsFromEndpoint(endpoint, syncOwnedParams{
		cursor:     cursor,
		cursorType: cursorType,
		limit:      limit,
	}, endpoint.Path), "starting_after")

	custom := spec.Endpoint{
		Method:     "GET",
		Path:       "/rows",
		Pagination: &spec.Pagination{Type: "offset", CursorParam: "start", LimitParam: "limit"},
		Params: []spec.Param{
			{Name: "start", In: "query", Type: "integer", Required: true},
			{Name: "limit", In: "query", Type: "integer", Default: 2},
		},
	}
	cursor, cursorType, _, _ = syncPaginationDefaultsFromEndpoint(custom)
	assert.Equal(t, "start", cursor)
	assert.Equal(t, "offset", cursorType)
	assert.Equal(t, "0", syncPaginationInitialCursorFromEndpoint(custom, cursor, cursorType))
}

func TestProfileFirstPageCursorFromOpenAPI(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: First Page Cursor
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /bounces:
    get:
      operationId: listBounces
      parameters:
        - name: offset
          in: query
          required: true
          schema: {type: integer, default: 0}
        - name: count
          in: query
          required: true
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /reviews:
    get:
      operationId: listReviews
      parameters:
        - name: offset
          in: query
          required: true
          schema: {type: integer, default: 0}
        - name: limit
          in: query
          required: true
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /commands:
    get:
      operationId: listCommands
      parameters:
        - name: page
          in: query
          required: true
          schema: {type: integer}
        - name: per_page
          in: query
          required: true
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /tokens:
    get:
      operationId: listTokens
      parameters:
        - name: starting_after
          in: query
          required: true
          schema: {type: string}
        - name: limit
          in: query
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /positions:
    get:
      operationId: listPositions
      parameters:
        - name: offset
          in: query
          required: true
          schema: {type: integer, minimum: 0, exclusiveMinimum: true}
        - name: limit
          in: query
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /bounded:
    get:
      operationId: listBounded
      parameters:
        - name: offset
          in: query
          required: true
          schema:
            type: integer
            allOf:
              - minimum: 1
        - name: limit
          in: query
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
  /bounded-ref:
    get:
      operationId: listBoundedRef
      parameters:
        - name: offset
          in: query
          required: true
          schema:
            $ref: "#/components/schemas/OneBasedOffset"
        - name: limit
          in: query
          schema: {type: integer, default: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object, properties: {id: {type: string}}}
components:
  schemas:
    OneBasedOffset:
      type: integer
      allOf:
        - minimum: 1
`))
	require.NoError(t, err)

	byPath := map[string]SyncableResource{}
	for _, resource := range Profile(apiSpec).SyncableResources {
		byPath[resource.Path] = resource
	}

	bounces := byPath["/bounces"]
	assert.Equal(t, "offset", bounces.PaginationCursorType)
	assert.Equal(t, "0", bounces.PaginationInitialCursor)
	assert.Contains(t, bounces.RequiredQueryParams, "offset")
	assert.NotContains(t, bounces.RequiredQueryParams, "count")

	reviews := byPath["/reviews"]
	assert.Equal(t, "0", reviews.PaginationInitialCursor)

	commands := byPath["/commands"]
	assert.Equal(t, "page", commands.PaginationCursorType)
	assert.Equal(t, "1", commands.PaginationInitialCursor)
	assert.Contains(t, commands.RequiredQueryParams, "page")
	assert.NotContains(t, commands.RequiredQueryParams, "per_page")

	tokens := byPath["/tokens"]
	assert.Equal(t, "cursor", tokens.PaginationCursorType)
	assert.Empty(t, tokens.PaginationInitialCursor)
	assert.NotContains(t, tokens.RequiredQueryParams, "starting_after")

	positions := byPath["/positions"]
	assert.Equal(t, "1", positions.PaginationInitialCursor)

	boundedParam, ok := queryParamByPath(apiSpec, "/bounded", "offset")
	require.True(t, ok)
	require.NotNil(t, boundedParam.Minimum)
	assert.Equal(t, 1.0, *boundedParam.Minimum)
	assert.Nil(t, boundedParam.Default)
	bounded := byPath["/bounded"]
	assert.Equal(t, "offset", bounded.PaginationCursorType)
	assert.Equal(t, "1", bounded.PaginationInitialCursor)

	refParam, ok := queryParamByPath(apiSpec, "/bounded-ref", "offset")
	require.True(t, ok)
	require.NotNil(t, refParam.Minimum)
	assert.Equal(t, 1.0, *refParam.Minimum)
	assert.Equal(t, "1", byPath["/bounded-ref"].PaginationInitialCursor)
}

func queryParamByPath(apiSpec *spec.APISpec, path, name string) (spec.Param, bool) {
	if apiSpec == nil {
		return spec.Param{}, false
	}
	for _, resource := range apiSpec.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Path != path {
				continue
			}
			for _, param := range endpoint.Params {
				if param.Name == name && param.In == "query" {
					return param, true
				}
			}
		}
	}
	return spec.Param{}, false
}
