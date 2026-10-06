package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileLiveSearchIndexIsNotASyncResource(t *testing.T) {
	s := &spec.APISpec{
		Name: "listing-search",
		Resources: map[string]spec.Resource{
			"categories": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/values/categories",
						Response: spec.ResponseDef{Type: "array", Item: "Category"},
					},
				},
			},
			"geo": {
				Endpoints: map[string]spec.Endpoint{
					"regions": {
						Method:   "GET",
						Path:     "/geo/regions",
						Response: spec.ResponseDef{Type: "array", Item: "Region"},
					},
				},
			},
			"ads": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method: "GET",
						Path:   "/search/items",
						Params: []spec.Param{
							{Name: "q", Type: "string"},
							{Name: "start", Type: "integer"},
							{Name: "lim", Type: "integer", Default: 30},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Ad"},
						Pagination: &spec.Pagination{
							Type:        "offset",
							CursorParam: "start",
							LimitParam:  "lim",
						},
					},
					"recommended": {
						Method: "GET",
						Path:   "/search/items/detail-recommended",
						Params: []spec.Param{
							{Name: "urn", Type: "string", Required: true, In: "query"},
							{Name: "lim", Type: "integer", Default: 20},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Ad"},
					},
				},
			},
			"listings": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method:   "GET",
						Path:     "/search/listings",
						IDField:  "urn",
						Response: spec.ResponseDef{Type: "array", Item: "Listing"},
						Pagination: &spec.Pagination{
							Type:        "offset",
							CursorParam: "start",
							LimitParam:  "lim",
						},
					},
				},
			},
			"category-items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/categories/{categoryId}/items",
						Response: spec.ResponseDef{Type: "array", Item: "Ad"},
						Walker:   &spec.WalkerConfig{Parent: "categories", KeyParam: "categoryId"},
						Params:   []spec.Param{{Name: "categoryId", Type: "string", Required: true, Positional: true}},
					},
				},
			},
			"category-search": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method:   "GET",
						Path:     "/search/{categoryId}/items",
						Response: spec.ResponseDef{Type: "array", Item: "Ad"},
						Walker:   &spec.WalkerConfig{Parent: "categories", KeyParam: "categoryId"},
						Params:   []spec.Param{{Name: "categoryId", Type: "string", Required: true, Positional: true}},
						Pagination: &spec.Pagination{
							Type:        "offset",
							CursorParam: "start",
							LimitParam:  "lim",
						},
					},
				},
			},
			"kept-search": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method:   "GET",
						Path:     "/search/{categoryId}/kept",
						Syncable: true,
						Response: spec.ResponseDef{Type: "array", Item: "Ad"},
						Walker:   &spec.WalkerConfig{Parent: "categories", KeyParam: "categoryId"},
						Params:   []spec.Param{{Name: "categoryId", Type: "string", Required: true, Positional: true}},
					},
				},
			},
			"contacts": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method: "POST",
						Path:   "/contacts/search",
						Pagination: &spec.Pagination{
							CursorParam: "startAfter",
							LimitParam:  "limit",
						},
						Response: spec.ResponseDef{Type: "object", Item: "ContactEnvelope"},
					},
				},
			},
			"widgets": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/widgets",
						Response: spec.ResponseDef{Type: "array"},
						Params:   []spec.Param{{Name: "status", In: "query", Type: "string", Required: true}},
					},
				},
			},
			"catalog": {
				Endpoints: map[string]spec.Endpoint{
					"search": {
						Method:   "GET",
						Path:     "/catalog/search",
						Syncable: true,
						Response: spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{
							Type:        "offset",
							CursorParam: "offset",
							LimitParam:  "limit",
						},
					},
				},
			},
			"books": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/query",
						Response: spec.ResponseDef{Type: "array", Item: "Book"},
						Params: []spec.Param{
							{Name: "query", Type: "string", Default: "select * from Book"},
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Category":        {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
			"Region":          {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
			"Ad":              {Fields: []spec.TypeField{{Name: "urn", Type: "string"}, {Name: "subject", Type: "string"}}},
			"Listing":         {Fields: []spec.TypeField{{Name: "urn", Type: "string"}, {Name: "title", Type: "string"}}},
			"ContactEnvelope": {Fields: []spec.TypeField{{Name: "contacts", Type: "array"}}},
			"Book":            {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "title", Type: "string"}}},
		},
	}

	profile := Profile(s)
	byName := map[string]SyncableResource{}
	for _, resource := range profile.SyncableResources {
		byName[resource.Name] = resource
	}

	require.Contains(t, byName, "categories")
	require.Contains(t, byName, "geo")
	assert.Equal(t, "/values/categories", byName["categories"].Path)
	assert.Equal(t, "/geo/regions", byName["geo"].Path)
	assert.False(t, byName["categories"].SkipDefaultSync)
	assert.False(t, byName["geo"].SkipDefaultSync)

	for name, resource := range byName {
		if resource.Path == "/search/{categoryId}/kept" {
			continue
		}
		assert.NotContains(t, resource.Path, "/search/", "%s must not sync a search index", name)
		assert.NotEqual(t, "/search", resource.Path, "%s must not sync a search index", name)
	}
	assert.NotContains(t, byName, "ads")
	assert.NotContains(t, byName, "ads-items")
	assert.NotContains(t, byName, "ads-detail-recommended")
	assert.NotContains(t, byName, "listings", "a recognized id on a search index must not put that index in the default sync set")
	var dependentPaths []string
	for _, dep := range profile.DependentSyncResources {
		dependentPaths = append(dependentPaths, dep.Path)
		if dep.Path != "/search/{categoryId}/kept" {
			assert.NotContains(t, dep.Path, "/search/", "%s must not sync a search index", dep.Name)
		}
	}
	assert.Contains(t, dependentPaths, "/categories/{categoryId}/items", "a walker on a real child collection stays a dependent sync")
	assert.NotContains(t, dependentPaths, "/search/{categoryId}/items", "a walker does not opt a search index into dependent sync")
	assert.Contains(t, dependentPaths, "/search/{categoryId}/kept", "syncable: true still opts a walked search index in")

	require.Contains(t, byName, "contacts", "paginated POST collection search stays a sync source")
	assert.Equal(t, "/contacts/search", byName["contacts"].Path)
	assert.Equal(t, "POST", byName["contacts"].Method)

	require.Contains(t, byName, "widgets", "a required filter on a real collection stays callable via --resources")
	assert.Equal(t, "/widgets", byName["widgets"].Path)
	assert.True(t, byName["widgets"].SkipDefaultSync)
	assert.Contains(t, byName["widgets"].RequiredQueryParams, "status")

	require.Contains(t, byName, "catalog", "explicit syncable: true opts a search index back in")
	assert.Equal(t, "/catalog/search", byName["catalog"].Path)

	require.Contains(t, byName, "books", "a shared SQL /query list is a bulk collection, not a search index")
	assert.Equal(t, "/query", byName["books"].Path)
	assert.False(t, byName["books"].SkipDefaultSync)
}
