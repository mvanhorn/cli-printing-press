package pipeline

import (
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseFixtureForSchemaShapes(t *testing.T) {
	t.Parallel()

	objectItems := map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
	tests := []struct {
		name    string
		schema  map[string]any
		root    map[string]any
		wantOK  bool
		want    nestedDataEnvelopeFixture
		scalar  string
		scalarV any
	}{
		{
			name: "top-level envelope",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"TotalCount": map[string]any{"type": "integer"},
					"Bounces":    objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "Bounces"},
			scalar:  "TotalCount",
			scalarV: 2,
		},
		{
			name: "data array envelope forces success true",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{"type": "boolean", "default": false},
					"data":    objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureDataArray, ArrayKey: "data"},
			scalar:  "success",
			scalarV: true,
		},
		{
			name: "nested data object",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{"type": "boolean"},
					"data": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"items": objectItems,
						},
					},
				},
			},
			wantOK: true,
			want:   nestedDataEnvelopeFixture{Kind: responseFixtureNestedDataObject, ArrayKey: "items"},
		},
		{
			name: "bare array",
			schema: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "object"},
			},
		},
		{
			name: "several arrays",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items":  objectItems,
					"errors": objectItems,
				},
			},
		},
		{
			name: "additionalProperties only",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		{
			name: "additionalProperties beside a single array still matches",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"total_items": map[string]any{"type": "integer"},
					"audiences":   objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "audiences"},
			scalar:  "total_items",
			scalarV: 2,
		},
		{
			name: "string array is not a list envelope",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		{
			name: "array without scalar siblings",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"items": objectItems},
			},
		},
		{
			name: "object sibling",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": objectItems,
					"meta":  map[string]any{"type": "object", "properties": map[string]any{"page": map[string]any{"type": "integer"}}},
				},
			},
		},
		{
			name: "malformed property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"total": map[string]any{"type": "integer"},
					"items": "nope",
				},
			},
		},
		{
			name:   "unresolved ref",
			schema: map[string]any{"$ref": "#/components/schemas/Missing"},
		},
		{
			name:   "empty schema",
			schema: map[string]any{},
		},
		{
			name: "declared integer default",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"TotalCount": map[string]any{"type": "integer", "default": 5},
					"Bounces":    objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "Bounces"},
			scalar:  "TotalCount",
			scalarV: 5,
		},
		{
			name: "non-success boolean keeps its default",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"archived": map[string]any{"type": "boolean", "default": false},
					"items":    objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items"},
			scalar:  "archived",
			scalarV: false,
		},
		{
			name: "identity scalar beside an object array still matches",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"notes": objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "notes"},
			scalar:  "id",
			scalarV: "mock",
		},
		{
			name: "capitalized Data is a top-level array key",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"TotalCount": map[string]any{"type": "integer"},
					"Data":       objectItems,
				},
			},
			wantOK: true,
			want:   nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "Data"},
		},
		{
			name: "all-failure status enum falls through",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "enum": []any{"error", "failed"}},
					"items":  objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items"},
			scalar:  "status",
			scalarV: "mock",
		},
		{
			name: "non-string enum value is kept",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":  map[string]any{"type": "integer", "enum": []any{0, 1}},
					"items": objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items"},
			scalar:  "code",
			scalarV: 0,
		},
		{
			name: "numeric continuation is null",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"next_offset": map[string]any{"type": "number", "default": 1.5},
					"items":       objectItems,
				},
			},
			wantOK:  true,
			want:    nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items"},
			scalar:  "next_offset",
			scalarV: nil,
		},
		{
			name: "allOf parts that are not objects do not match",
			schema: map[string]any{
				"allOf": []any{"nope", 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture, ok := responseFixtureForSchema(tt.schema, tt.root)
			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.want.Kind, fixture.Kind)
			assert.Equal(t, tt.want.ArrayKey, fixture.ArrayKey)
			if tt.scalar != "" {
				value, present := fixture.Scalars[tt.scalar]
				require.True(t, present, tt.scalar)
				assert.Equal(t, tt.scalarV, value)
			}
		})
	}
}

func TestResponseFixtureContinuationAndFailureScalars(t *testing.T) {
	t.Parallel()

	items := map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
	fixture, ok := responseFixtureForSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"has_more":    map[string]any{"type": "boolean", "default": true},
			"hasMore":     map[string]any{"type": "boolean"},
			"next_cursor": map[string]any{"type": "string", "default": "page-2"},
			"nextPage":    map[string]any{"type": "integer", "default": 3},
			"status":      map[string]any{"type": "string", "enum": []any{"error", "ok"}},
			"note":        map[string]any{"type": "string", "default": "failed"},
			"items":       items,
		},
	}, nil)
	require.True(t, ok)
	assert.Equal(t, false, fixture.Scalars["has_more"])
	assert.Equal(t, false, fixture.Scalars["hasMore"])
	assert.Equal(t, "", fixture.Scalars["next_cursor"])
	assert.Nil(t, fixture.Scalars["nextPage"])
	assert.Equal(t, "ok", fixture.Scalars["status"])
	assert.Equal(t, "mock", fixture.Scalars["note"])
}

func TestResponseFixtureFlattensAllOf(t *testing.T) {
	t.Parallel()

	root := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"Page": map[string]any{
					"type":       "object",
					"properties": map[string]any{"total_count": map[string]any{"type": "integer"}},
				},
			},
		},
	}
	schema := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/components/schemas/Page"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
				},
			},
		},
	}
	fixture, ok := responseFixtureForSchema(schema, root)
	require.True(t, ok)
	assert.Equal(t, "items", fixture.ArrayKey)
	assert.Equal(t, 2, fixture.Scalars["total_count"])
}

func TestResponseFixtureAllOfDepthLimit(t *testing.T) {
	t.Parallel()

	leaf := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"total": map[string]any{"type": "integer"},
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
	}
	wrap := func(depth int) map[string]any {
		schema := leaf
		for range depth {
			schema = map[string]any{"allOf": []any{
				map[string]any{"type": "object", "properties": map[string]any{}},
				schema,
			}}
		}
		return schema
	}

	_, shallowOK := responseFixtureForSchema(wrap(2), nil)
	assert.True(t, shallowOK)
	_, deepOK := responseFixtureForSchema(wrap(10), nil)
	assert.False(t, deepOK)
}

func TestDetectNestedDataEnvelopeFixturesRejectsUndecodableSpec(t *testing.T) {
	t.Parallel()

	assert.Nil(t, detectNestedDataEnvelopeFixtures(nil))
	assert.Nil(t, detectNestedDataEnvelopeFixtures([]byte("   ")))
	assert.Nil(t, detectNestedDataEnvelopeFixtures([]byte("[")))
	assert.Nil(t, detectNestedDataEnvelopeFixtures([]byte(":")))
	assert.Nil(t, detectNestedDataEnvelopeFixtures([]byte("paths: []\n")))

	fixtures := detectNestedDataEnvelopeFixtures([]byte(`{"openapi":"3.0.3","paths":{"/items":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"total":{"type":"integer"},"items":{"type":"array","items":{"type":"object"}}}}}}}}}}}}`))
	require.Contains(t, fixtures, responseFixtureKey{Method: "GET", Path: "/items"})
	assert.Equal(t, responseFixtureTopLevel, fixtures[responseFixtureKey{Method: "GET", Path: "/items"}].Kind)
}

func TestEnvelopePropertyKeyAndContinuation(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "has_more", envelopePropertyKey("hasMore"))
	assert.Equal(t, "has_more", envelopePropertyKey("HasMore"))
	assert.Equal(t, "has_more", envelopePropertyKey("has-more"))
	assert.Equal(t, "next_page_token", envelopePropertyKey("NextPageToken"))
	assert.Equal(t, "total_count", envelopePropertyKey("TotalCount"))

	for _, key := range []string{"has_more", "hasMore", "HasMore", "has-more", "nextPage", "NextPageToken", "nextCursor", "IsTruncated", "next_url"} {
		assert.True(t, isContinuationScalarKey(key), key)
	}
	for _, key := range []string{"success", "ok", "TotalCount", "total_count", "object", "url", "page"} {
		assert.False(t, isContinuationScalarKey(key), key)
	}
	for _, key := range []string{"success", "Success", "ok", "is_success", "isSuccess"} {
		assert.True(t, isSuccessScalarKey(key), key)
	}
	assert.False(t, isSuccessScalarKey("TotalCount"))
}

func TestRenderResponseFixtureFallbacks(t *testing.T) {
	t.Parallel()

	top := renderResponseFixture(nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, Scalars: map[string]any{"total": 2}})
	assert.Contains(t, top, `"items"`)
	assert.Contains(t, top, `"total":2`)

	nested := renderResponseFixture(nestedDataEnvelopeFixture{})
	assert.Contains(t, nested, `"success":true`)
	assert.Contains(t, nested, `"pagination":{"total":2}`)

	data := renderResponseFixture(nestedDataEnvelopeFixture{
		Kind:     responseFixtureDataArray,
		ArrayKey: "data",
		Scalars:  map[string]any{"success": true},
	})
	assert.Contains(t, data, `"data":[`)
	assert.Contains(t, data, `"success":true`)
}

func TestMarshalMockBodyReturnsFallback(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "fallback", marshalMockBody(map[string]any{"bad": make(chan int)}, "fallback"))
	assert.JSONEq(t, `{"ok":true}`, marshalMockBody(map[string]any{"ok": true}, "fallback"))
}

func TestResponseFixtureForRequestMatchesMethodAndPath(t *testing.T) {
	t.Parallel()

	top := nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items", Scalars: map[string]any{"total": 2}}
	other := nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "rows", Scalars: map[string]any{"total": 2}}
	exact := nestedDataEnvelopeFixture{Kind: responseFixtureDataArray, ArrayKey: "data", Scalars: map[string]any{"success": true}}
	spec := &openAPISpec{NestedDataEnvelopes: map[responseFixtureKey]nestedDataEnvelopeFixture{
		{Method: "GET", Path: "/users/{id}/items"}:      top,
		{Method: "GET", Path: "/users/{user_id}/items"}: other,
		{Method: "GET", Path: "/users/abc/items"}:       exact,
		{Method: "POST", Path: "/widgets"}:              top,
	}}

	got, ok := responseFixtureForRequest(spec, "get", "/users/abc/items")
	require.True(t, ok)
	assert.Equal(t, "data", got.ArrayKey)

	got, ok = responseFixtureForRequest(spec, "GET", "/users/zzz/items")
	require.True(t, ok)
	assert.Equal(t, "items", got.ArrayKey, "overlapping patterns pick the lexicographically first spec path")

	_, ok = responseFixtureForRequest(spec, "POST", "/users/abc/items")
	assert.False(t, ok)
	_, ok = responseFixtureForRequest(spec, "DELETE", "/widgets")
	assert.False(t, ok)
	_, ok = responseFixtureForRequest(nil, "GET", "/widgets")
	assert.False(t, ok)
	_, ok = responseFixtureForRequest(&openAPISpec{}, "GET", "/widgets")
	assert.False(t, ok)

	got, ok = responseFixtureForRequest(spec, "post", "/widgets")
	require.True(t, ok)
	assert.Equal(t, "items", got.ArrayKey)
}

func TestExpectedMockSyncRowsUsesSyncRequests(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, expectedMockSyncRows(nil))
	assert.Equal(t, 0, expectedMockSyncRows(&openAPISpec{}))

	envelope := nestedDataEnvelopeFixture{Kind: responseFixtureTopLevel, ArrayKey: "items", Scalars: map[string]any{"total": 2}}
	spec := &openAPISpec{
		NestedDataEnvelopes: map[responseFixtureKey]nestedDataEnvelopeFixture{
			{Method: "POST", Path: "/items"}: envelope,
		},
		SyncRequests: []responseFixtureKey{{Method: "GET", Path: "/items"}},
	}
	assert.Equal(t, 0, expectedMockSyncRows(spec))

	spec.NestedDataEnvelopes[responseFixtureKey{Method: "GET", Path: "/items"}] = envelope
	assert.Equal(t, len(mockListItems()), expectedMockSyncRows(spec))

	postOnly := &openAPISpec{
		NestedDataEnvelopes: map[responseFixtureKey]nestedDataEnvelopeFixture{
			{Method: "POST", Path: "/widgets"}: envelope,
		},
		SyncRequests: []responseFixtureKey{{Method: "POST", Path: "/widgets?kind=open"}},
	}
	assert.Equal(t, 0, expectedMockSyncRows(postOnly), "query string is not part of the mock path")

	postOnly.SyncRequests = []responseFixtureKey{{Method: "POST", Path: "/widgets"}}
	assert.Equal(t, len(mockListItems()), expectedMockSyncRows(postOnly))
}

func TestSyncFixturePathDropsQuery(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/items", syncFixturePath(" /items?entityType=widget "))
	assert.Equal(t, "/items", syncFixturePath("/items"))
	assert.Empty(t, syncFixturePath("   "))
}

func TestDefaultSyncRequestKeysStripsQueryAndSkipsUnrelatedFixtures(t *testing.T) {
	t.Parallel()

	api := &apispec.APISpec{
		Resources: map[string]apispec.Resource{
			"items": {
				Endpoints: map[string]apispec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/items?entityType=widget",
						Response: apispec.ResponseDef{Type: "array", Item: "item"},
					},
				},
			},
		},
		Types: map[string]apispec.TypeDef{
			"item": {Fields: []apispec.TypeField{{Name: "id", Type: "integer"}}},
		},
	}
	envelopes := map[responseFixtureKey]nestedDataEnvelopeFixture{
		{Method: "GET", Path: "/items"}: {Kind: responseFixtureTopLevel, ArrayKey: "items", Scalars: map[string]any{"total": 2}},
	}
	assert.Nil(t, defaultSyncRequestKeys(nil, envelopes))
	assert.Nil(t, defaultSyncRequestKeys(api, nil))

	keys := defaultSyncRequestKeys(api, envelopes)
	require.Equal(t, []responseFixtureKey{{Method: "GET", Path: "/items"}}, keys)
	assert.Equal(t, len(mockListItems()), expectedMockSyncRows(&openAPISpec{
		NestedDataEnvelopes: envelopes,
		SyncRequests:        keys,
	}))

	resource := api.Resources["items"]
	off := false
	resource.Syncable = &off
	api.Resources["items"] = resource
	assert.Empty(t, defaultSyncRequestKeys(api, envelopes))
}
