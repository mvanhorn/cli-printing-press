package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGenericSearchParamName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{name: "q", want: true},
		{name: "query", want: true},
		{name: "Query", want: true},
		{name: "search", want: true},
		{name: "keyword", want: true},
		{name: "term", want: true},
		{name: "searchTerm", want: true},
		{name: "queryText", want: true},
		{name: "searchtext", want: true},
		{name: "text", want: true},
		{name: "filter[query]", want: true},
		{name: "Filter[Query]", want: true},
		{name: "scope[query]", want: true},
		{name: "q[query]", want: true},
		{name: "filter[q]", want: true},
		{name: "filter[searchTerm]", want: true},
		{name: "scope[filter][query]", want: true},
		{name: "filter[text]", want: true},
		{name: "queryString", want: false},
		{name: "queriedAt", want: false},
		{name: "searchTermCount", want: false},
		{name: "search_term", want: false},
		{name: "myquery", want: false},
		{name: "filter[queryString]", want: false},
		{name: "filter[queriedAt]", want: false},
		{name: "filter[searchTermCount]", want: false},
		{name: "filter[query][eq]", want: false},
		{name: "query[items]", want: false},
		{name: "filter[query]extra", want: false},
		{name: "[query]", want: false},
		{name: "filter[]", want: false},
		{name: "filter[query", want: false},
		{name: "", want: false},
		{name: "limit", want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isGenericSearchParamName(tt.name))
		})
	}
}

func TestProfileGenericSearchElection(t *testing.T) {
	queryParam := func(name string) spec.Param {
		return spec.Param{Name: name, Type: "string", In: "query"}
	}
	pathParam := func(name string) spec.Param {
		return spec.Param{Name: name, Type: "string", In: "path", PathParam: true, Required: true, Positional: true}
	}

	cases := []struct {
		name         string
		resources    map[string]spec.Resource
		templateVars []string
		wantPath     string
		wantMethod   string
		wantParam    string
		wantNoLive   bool
	}{
		{
			name: "scoped search only is local",
			resources: map[string]spec.Resource{
				"contacts": searchEndpointResource("GET", "/spaces/{spaceId}/contacts/search", []spec.Param{
					pathParam("spaceId"),
					queryParam("filter[query]"),
				}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "shorter unbound path loses to bindable filter query",
			resources: map[string]spec.Resource{
				"short":    searchEndpointResource("GET", "/x/{id}/search", []spec.Param{pathParam("id")}, nil),
				"contacts": searchEndpointResource("GET", "/contacts/search", []spec.Param{queryParam("filter[query]")}, nil),
				"scoped":   searchEndpointResource("GET", "/spaces/{spaceId}/contacts/search", []spec.Param{pathParam("spaceId"), queryParam("filter[query]")}, nil),
			},
			wantPath:   "/contacts/search",
			wantMethod: "GET",
			wantParam:  "filter[query]",
		},
		{
			name: "parameterless search stays ahead of a longer bracketed endpoint",
			resources: map[string]spec.Resource{
				"global":   searchEndpointResource("GET", "/search", nil, nil),
				"contacts": searchEndpointResource("GET", "/contacts/search", []spec.Param{queryParam("filter[query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q",
		},
		{
			name: "explicit q is unchanged when a longer bracketed endpoint exists",
			resources: map[string]spec.Resource{
				"global":   searchEndpointResource("GET", "/search", []spec.Param{queryParam("q")}, nil),
				"contacts": searchEndpointResource("GET", "/contacts/search", []spec.Param{queryParam("filter[query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q",
		},
		{
			name: "plain query term and search names are unchanged",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/items/search", []spec.Param{queryParam("term")}, nil),
			},
			wantPath:   "/items/search",
			wantMethod: "GET",
			wantParam:  "term",
		},
		{
			name: "declared non-search param does not invent q or retarget",
			resources: map[string]spec.Resource{
				"global":   searchEndpointResource("GET", "/search", []spec.Param{queryParam("limit")}, nil),
				"contacts": searchEndpointResource("GET", "/contacts/search", []spec.Param{queryParam("query")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "only limit disables live search",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("limit")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "substring names are not search params",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("queryString")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "queriedAt is not a search param",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("queriedAt")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "searchTermCount is not a search param",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("searchTermCount")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "searchTerm plain name still matches",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("searchTerm")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "searchTerm",
		},
		{
			name: "scope and q wrappers use the original wire name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("scope[query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "scope[query]",
		},
		{
			name: "q wrapper keeps the original name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("q[query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q[query]",
		},
		{
			name: "bracket case is preserved on the wire",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("Filter[Query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "Filter[Query]",
		},
		{
			name: "nested innermost search name is the wire name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("scope[filter][query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "scope[filter][query]",
		},
		{
			name: "operator suffix is not a search param",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("filter[query][eq]")}, nil),
			},
			wantNoLive: true,
		},
		{
			name: "first matching query param wins",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("q"), queryParam("query")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q",
		},
		{
			name: "later q does not override an earlier query",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("query"), queryParam("q")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "query",
		},
		{
			name: "url name is the wire name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{{
					Name: "query", Type: "string", In: "query", URLName: "filter[query]",
				}}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "filter[query]",
		},
		{
			name: "limit beside a bracketed param still sends the bracketed name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("limit"), queryParam("filter[query]")}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "filter[query]",
		},
		{
			name: "header only keeps the q default",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{{
					Name: "X-Request-Id", Type: "string", In: "header",
				}}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q",
		},
		{
			name: "header named query is still the wire name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{{
					Name: "query", Type: "string", In: "header",
				}}, nil),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "query",
		},
		{
			name: "endpoint template var path stays live",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/orgs/{org}/search", []spec.Param{pathParam("org")}, nil),
			},
			templateVars: []string{"org"},
			wantPath:     "/orgs/{org}/search",
			wantMethod:   "GET",
			wantParam:    "q",
		},
		{
			name: "template var plus unbound path param is not live",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/orgs/{org}/spaces/{spaceId}/search", []spec.Param{
					pathParam("org"),
					pathParam("spaceId"),
				}, nil),
			},
			templateVars: []string{"org"},
			wantNoLive:   true,
		},
		{
			name: "hyphenated placeholder is not live",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search/{space-id}", nil, nil),
			},
			wantNoLive: true,
		},
		{
			name: "malformed brace is not live",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search/{", nil, nil),
			},
			wantNoLive: true,
		},
		{
			name: "get body query is ignored when a query param is not a search name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", []spec.Param{queryParam("limit")}, []spec.Param{queryParam("query")}),
			},
			wantNoLive: true,
		},
		{
			name: "get with no query params keeps q even if the body names query",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("GET", "/search", nil, []spec.Param{queryParam("query")}),
			},
			wantPath:   "/search",
			wantMethod: "GET",
			wantParam:  "q",
		},
		{
			name: "post body bracket name is the wire name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("POST", "/search", nil, []spec.Param{queryParam("filter[query]")}),
			},
			wantPath:   "/search",
			wantMethod: "POST",
			wantParam:  "filter[query]",
		},
		{
			name: "post body without a search name disables live search",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("POST", "/search", nil, []spec.Param{{Name: "limit", Type: "integer"}}),
			},
			wantNoLive: true,
		},
		{
			name: "post body search name overrides an earlier query name",
			resources: map[string]spec.Resource{
				"items": searchEndpointResource("POST", "/search", []spec.Param{queryParam("q")}, []spec.Param{queryParam("query")}),
			},
			wantPath:   "/search",
			wantMethod: "POST",
			wantParam:  "query",
		},
		{
			name: "name containing search on a bindable path still elects",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"searchItems": {Method: "GET", Path: "/items"},
					},
				},
			},
			wantPath:   "/items",
			wantMethod: "GET",
			wantParam:  "q",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			profile := Profile(&spec.APISpec{
				Name:                 "search-election",
				Resources:            tt.resources,
				EndpointTemplateVars: tt.templateVars,
			})
			if tt.wantNoLive {
				assert.Empty(t, profile.SearchEndpointPath)
				assert.Empty(t, profile.SearchQueryParam)
				assert.Empty(t, profile.SearchEndpointMethod)
				assert.Empty(t, profile.SearchBodyFields)
				return
			}
			assert.Equal(t, tt.wantPath, profile.SearchEndpointPath)
			assert.Equal(t, tt.wantMethod, profile.SearchEndpointMethod)
			assert.Equal(t, tt.wantParam, profile.SearchQueryParam)
		})
	}
}

func TestProfilePostSearchKeepsNonQueryBodyDefaults(t *testing.T) {
	profile := Profile(&spec.APISpec{
		Name: "post-search",
		Resources: map[string]spec.Resource{
			"items": searchEndpointResource("POST", "/search", nil, []spec.Param{
				{Name: "filter[query]", Type: "string"},
				{Name: "limit", Type: "integer", Required: true},
				{Name: "mode", Type: "string", Enum: []string{"all", "any"}},
				{Name: "note", Type: "string"},
				{Name: "include", Type: "boolean"},
				{Name: "extra", Type: "object"},
				{Name: "tags", Type: "array"},
				{Name: "types", Type: "array", Fields: []spec.Param{{Enum: []string{"a", "b"}}}},
				{Name: "kinds", Type: "array", Enum: []string{"one", "two"}},
				{Name: "label", Type: "string", Default: "custom"},
			}),
		},
	})

	require.Equal(t, "/search", profile.SearchEndpointPath)
	require.Equal(t, "POST", profile.SearchEndpointMethod)
	require.Equal(t, "filter[query]", profile.SearchQueryParam)
	require.Equal(t, []SearchBodyField{
		{Name: "limit", Type: "integer", Required: true, Default: 10},
		{Name: "mode", Type: "string", Default: "all"},
		{Name: "note", Type: "string", Default: ""},
		{Name: "include", Type: "boolean", Default: true},
		{Name: "extra", Type: "object", Default: map[string]any{}},
		{Name: "tags", Type: "array", Default: []any{}},
		{Name: "types", Type: "array", Default: []string{"a", "b"}},
		{Name: "kinds", Type: "array", Default: []string{"one", "two"}},
		{Name: "label", Type: "string", Default: "custom"},
	}, profile.SearchBodyFields)
}

func TestProfileScopedSearchDoesNotDropListSync(t *testing.T) {
	profile := Profile(&spec.APISpec{
		Name: "cobot-like",
		Resources: map[string]spec.Resource{
			"contacts": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/contacts",
						Response: spec.ResponseDef{Type: "array", Item: "Contact"},
					},
					"search": {
						Method: "GET",
						Path:   "/spaces/{spaceId}/contacts/search",
						Params: []spec.Param{{Name: "filter[query]", Type: "string", In: "query"}},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Contact": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		},
	})

	assert.Empty(t, profile.SearchEndpointPath)
	require.Contains(t, profile.SyncableResourceNames(), "contacts")
	var contacts SyncableResource
	for _, resource := range profile.SyncableResources {
		if resource.Name == "contacts" {
			contacts = resource
		}
	}
	assert.Equal(t, "/contacts", contacts.Path)
	assert.Equal(t, "GET", contacts.Method)
}

func searchEndpointResource(method, path string, params, body []spec.Param) spec.Resource {
	return spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method: method,
				Path:   path,
				Params: params,
				Body:   body,
			},
		},
	}
}
