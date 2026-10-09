package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPPageConfigPagesReadOnlyPOSTLists(t *testing.T) {
	t.Parallel()

	list := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/list",
		Description: "List a folder",
		Mutation:    new(false),
		Body: []spec.Param{
			{Name: "path", Type: "string", Required: true},
			{Name: "limit", Type: "integer"},
		},
	}
	continueEP := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/list/continue",
		Description: "Continue a folder list",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
	}
	search := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/search_v2",
		Description: "Search",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "query", Type: "string", Required: true}},
	}
	searchContinue := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/search/continue_v2",
		Description: "Continue a search",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
	}
	links := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/links",
		Description: "List links",
		Mutation:    new(false),
		Body: []spec.Param{
			{Name: "path", Type: "string"},
			{Name: "cursor", Type: "string"},
		},
	}
	linksWithSibling := spec.Endpoint{
		Method:      "POST",
		Path:        "/shared/links",
		Description: "List links that also have a continue operation",
		Mutation:    new(false),
		Body: []spec.Param{
			{Name: "path", Type: "string"},
			{Name: "cursor", Type: "string"},
		},
	}
	resource := spec.Resource{Endpoints: map[string]spec.Endpoint{
		"list-folder":          list,
		"list-folder-continue": continueEP,
		"search":               search,
		"search-continue":      searchContinue,
		"list-links":           links,
	}}

	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(resource, "delete", spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/delete",
		Description: "Delete",
		Body:        []spec.Param{{Name: "path", Type: "string", Required: true}},
	}))
	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(resource, "list-folder", spec.Endpoint{
		Method:         "POST",
		Path:           "/folders/list",
		Description:    "Binary list",
		Mutation:       new(false),
		ResponseFormat: spec.ResponseFormatBinary,
		Body:           []spec.Param{{Name: "cursor", Type: "string"}},
	}))

	external := `mcpPageConfig{NextCursorPath: "cursor", HasMoreField: "has_more", ContinuationInput: "cursor", ExternalContinuation: true}`
	assert.Equal(t, external, mcpToolPageConfig(resource, "list-folder", list))
	assert.True(t, mcpExposeOpaqueCursor(resource, "list-folder", list))
	assert.Equal(t, external, mcpToolPageConfig(resource, "search", search), "name pairing covers a continue path that does not end in /continue")
	assert.True(t, mcpExposeOpaqueCursor(resource, "search", search))

	sameTool := `mcpPageConfig{CursorParam: "cursor", NextCursorPath: "cursor", HasMoreField: "has_more", ContinuationInput: "cursor", CursorInBody: true}`
	assert.Equal(t, sameTool, mcpToolPageConfig(resource, "list-folder-continue", continueEP))
	assert.False(t, mcpExposeOpaqueCursor(resource, "list-folder-continue", continueEP))
	assert.Equal(t, sameTool, mcpToolPageConfig(resource, "list-links", links))
	assert.False(t, mcpExposeOpaqueCursor(resource, "list-links", links))

	withContinue := spec.Resource{Endpoints: map[string]spec.Endpoint{
		"list-links":          linksWithSibling,
		"list-links-continue": continueEP,
	}}
	assert.Equal(t, sameTool, mcpToolPageConfig(withContinue, "list-links", linksWithSibling), "a cursor input on the list wins over a continue sibling")
	assert.False(t, mcpExposeOpaqueCursor(withContinue, "list-links", linksWithSibling))

	pathOnly := spec.Resource{Endpoints: map[string]spec.Endpoint{
		"browse": {
			Method:      "POST",
			Path:        "/rpc/browse",
			Description: "Browse",
			Mutation:    new(false),
			Body:        []spec.Param{{Name: "path", Type: "string"}},
		},
		"next": {
			Method:      "POST",
			Path:        "/rpc/browse/continue",
			Description: "Browse continue",
			Mutation:    new(false),
			Body:        []spec.Param{{Name: "page_token", Type: "string", Required: true}},
		},
	}}
	assert.Equal(t,
		`mcpPageConfig{NextCursorPath: "page_token", HasMoreField: "has_more", ContinuationInput: "page_token", ExternalContinuation: true}`,
		mcpToolPageConfig(pathOnly, "browse", pathOnly.Endpoints["browse"]),
	)

	camel := spec.Resource{Endpoints: map[string]spec.Endpoint{
		"listFolder": {
			Method:      "POST",
			Path:        "/rpc/folders",
			Description: "List",
			Mutation:    new(false),
			Body:        []spec.Param{{Name: "path", Type: "string"}},
		},
		"listFolderContinue": {
			Method:      "POST",
			Path:        "/rpc/folders/next",
			Description: "Continue",
			Mutation:    new(false),
			Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
		},
	}}
	assert.Equal(t, external, mcpToolPageConfig(camel, "listFolder", camel.Endpoints["listFolder"]))

	declared := spec.Endpoint{
		Method:      "POST",
		Path:        "/items/search",
		Description: "Search items",
		Mutation:    new(false),
		Pagination: &spec.Pagination{
			Type:         "cursor",
			CursorParam:  "page_token",
			HasMoreField: "more",
		},
		Params: []spec.Param{{Name: "page_token", In: "query", Type: "string"}},
	}
	assert.Equal(t,
		`mcpPageConfig{CursorParam: "page_token", NextCursorPath: "page_token", HasMoreField: "more", ContinuationInput: "page_token"}`,
		mcpToolPageConfig(spec.Resource{}, "search", declared),
	)
	assert.True(t, mcpExposeOpaqueCursor(spec.Resource{}, "search", declared))

	offset := spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "List items",
		Mutation:    new(false),
		Pagination: &spec.Pagination{
			Type:        "offset",
			CursorParam: "offset",
			LimitParam:  "limit",
		},
		Params: []spec.Param{{Name: "offset", In: "query", Type: "integer"}},
	}
	assert.Equal(t,
		`mcpPageConfig{CursorParam: "offset", ContinuationInput: "offset"}`,
		mcpToolPageConfig(spec.Resource{}, "list", offset),
	)

	mutating := spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create",
		Mutation:    new(true),
		Pagination:  &spec.Pagination{Type: "cursor", CursorParam: "cursor"},
		Body:        []spec.Param{{Name: "cursor", Type: "string"}},
	}
	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(spec.Resource{}, "create", mutating))
	assert.False(t, mcpExposeOpaqueCursor(spec.Resource{}, "create", mutating))

	getAfter := spec.Endpoint{
		Method: "GET",
		Path:   "/revisions",
		Pagination: &spec.Pagination{
			Type:        "cursor",
			CursorParam: "after",
			LimitParam:  "limit",
		},
		Params: []spec.Param{{Name: "after", Type: "string"}, {Name: "limit", Type: "integer"}},
	}
	assert.Equal(t, `mcpPageConfig{CursorParam: "after", NextCursorPath: "after"}`, mcpPageConfig(getAfter))
	assert.Equal(t, `mcpPageConfig{CursorParam: "after", NextCursorPath: "after"}`, mcpToolPageConfig(spec.Resource{}, "revisions", getAfter))

	members := spec.Resource{Endpoints: map[string]spec.Endpoint{
		"list": {
			Method:      "POST",
			Path:        "/groups/{id}/members",
			Description: "List members",
			Mutation:    new(false),
			Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true}},
			Body:        []spec.Param{{Name: "limit", Type: "integer"}},
		},
		"list-continue": {
			Method:      "POST",
			Path:        "/groups/{id}/members/continue",
			Description: "Continue members",
			Mutation:    new(false),
			Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true}},
			Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
		},
	}}
	parent := spec.Resource{SubResources: map[string]spec.Resource{"members": members}}
	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(parent, "list", members.Endpoints["list"]), "sibling detection stays inside the resource that owns the operation")
	assert.Equal(t, external, mcpToolPageConfig(members, "list", members.Endpoints["list"]))

	nested := spec.Endpoint{
		Method:      "POST",
		Path:        "/items/nested",
		Description: "List with a nested cursor",
		Mutation:    new(false),
		Pagination: &spec.Pagination{
			Type:           "cursor",
			CursorParam:    "cursor",
			NextCursorPath: "paging.cursor",
			HasMoreField:   "paging.has_more",
		},
		Body: []spec.Param{
			{Name: "path", Type: "string"},
			{Name: "paging", Type: "object", Fields: []spec.Param{{Name: "cursor", Type: "string"}}},
		},
	}
	nestedConfig := `mcpPageConfig{CursorParam: "cursor", NextCursorPath: "paging.cursor", HasMoreField: "paging.has_more", ContinuationInput: "paging-cursor", CursorInBody: true, BodyPath: []string{"paging", "cursor"}}`
	assert.Equal(t, nestedConfig, mcpToolPageConfig(spec.Resource{}, "nested-list", nested))
	assert.True(t, mcpExposeOpaqueCursor(spec.Resource{}, "nested-list", nested))
	dottedParam := nested
	dottedParam.Pagination = &spec.Pagination{
		Type:           "cursor",
		CursorParam:    "paging.cursor",
		NextCursorPath: "paging.cursor",
		HasMoreField:   "paging.has_more",
	}
	assert.Equal(t,
		`mcpPageConfig{CursorParam: "paging.cursor", NextCursorPath: "paging.cursor", HasMoreField: "paging.has_more", ContinuationInput: "paging-cursor", CursorInBody: true, BodyPath: []string{"paging", "cursor"}}`,
		mcpToolPageConfig(spec.Resource{}, "nested-list", dottedParam),
	)

	opaque := spec.Endpoint{
		Method:           "POST",
		Path:             "/items/opaque",
		Description:      "Opaque body list",
		Mutation:         new(false),
		BodyJSONFallback: true,
		Pagination:       &spec.Pagination{Type: "cursor", CursorParam: "cursor", HasMoreField: "has_more"},
	}
	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(spec.Resource{}, "opaque-list", opaque))

	opaqueQuery := opaque
	opaqueQuery.Pagination = &spec.Pagination{Type: "cursor", CursorParam: "page_token", HasMoreField: "more"}
	opaqueQuery.Params = []spec.Param{{Name: "page_token", In: "query", Type: "string"}}
	assert.Equal(t,
		`mcpPageConfig{CursorParam: "page_token", NextCursorPath: "page_token", HasMoreField: "more", ContinuationInput: "page_token"}`,
		mcpToolPageConfig(spec.Resource{}, "opaque-query", opaqueQuery),
	)

	raw := spec.Endpoint{
		Method:             "POST",
		Path:               "/items/raw",
		Description:        "Raw body list",
		Mutation:           new(false),
		RequestContentType: "text/plain",
		Body:               []spec.Param{{Name: "cursor", Type: "string"}},
	}
	assert.Equal(t, "mcpPageConfig{}", mcpToolPageConfig(spec.Resource{}, "raw-list", raw))

	form := spec.Endpoint{
		Method:             "POST",
		Path:               "/items/form",
		Description:        "Form list",
		Mutation:           new(false),
		RequestContentType: "application/x-www-form-urlencoded",
		Pagination:         &spec.Pagination{Type: "cursor", CursorParam: "cursor", HasMoreField: "has_more"},
		Body: []spec.Param{
			{Name: "q", Type: "string"},
			{Name: "cursor", Type: "string"},
		},
	}
	assert.Equal(t, sameTool, mcpToolPageConfig(spec.Resource{}, "form-list", form))
}

func TestGeneratedReadOnlyPOSTListPaging(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("rpcpages")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	list := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/list",
		Description: "List a folder",
		Mutation:    new(false),
		Body: []spec.Param{
			{Name: "path", Type: "string", Required: true},
			{Name: "limit", Type: "integer"},
		},
	}
	continueEP := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/list/continue",
		Description: "Continue a folder list",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
	}
	search := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/search_v2",
		Description: "Search",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "query", Type: "string", Required: true}},
	}
	searchContinue := spec.Endpoint{
		Method:      "POST",
		Path:        "/folders/search/continue_v2",
		Description: "Continue a search",
		Mutation:    new(false),
		Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"folders": {
			Description: "Folders",
			Endpoints: map[string]spec.Endpoint{
				"list-folder":          list,
				"list-folder-continue": continueEP,
				"search":               search,
				"search-continue":      searchContinue,
				"delete": {
					Method:      "POST",
					Path:        "/folders/delete",
					Description: "Delete a folder",
					Body:        []spec.Param{{Name: "path", Type: "string", Required: true}},
				},
				"revisions": {
					Method:      "GET",
					Path:        "/folders/revisions",
					Description: "List revisions",
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "after", Type: "string"},
					},
					Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "after"},
				},
				"query-search": {
					Method:      "POST",
					Path:        "/items/search",
					Description: "Search items",
					Mutation:    new(false),
					Pagination:  &spec.Pagination{Type: "cursor", CursorParam: "page_token", HasMoreField: "more"},
					Params: []spec.Param{
						{Name: "q", In: "query", Type: "string"},
						{Name: "page_token", In: "query", Type: "string"},
					},
				},
				"nested-list": {
					Method:      "POST",
					Path:        "/items/nested",
					Description: "List with a nested cursor",
					Mutation:    new(false),
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "cursor",
						NextCursorPath: "paging.cursor",
						HasMoreField:   "paging.has_more",
					},
					Body: []spec.Param{
						{Name: "path", Type: "string"},
						{Name: "paging", Type: "object", Fields: []spec.Param{{Name: "cursor", Type: "string"}}},
					},
				},
				"form-list": {
					Method:             "POST",
					Path:               "/items/form",
					Description:        "List with a form cursor",
					Mutation:           new(false),
					RequestContentType: "application/x-www-form-urlencoded",
					Pagination:         &spec.Pagination{Type: "cursor", CursorParam: "cursor", HasMoreField: "has_more"},
					Body: []spec.Param{
						{Name: "q", Type: "string"},
						{Name: "cursor", Type: "string"},
					},
				},
				"opaque-list": {
					Method:           "POST",
					Path:             "/items/opaque",
					Description:      "List with an opaque JSON body",
					Mutation:         new(false),
					BodyJSONFallback: true,
					Pagination:       &spec.Pagination{Type: "cursor", CursorParam: "cursor", HasMoreField: "has_more"},
				},
			},
			SubResources: map[string]spec.Resource{
				"members": {
					Description: "Members",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:      "POST",
							Path:        "/groups/{id}/members",
							Description: "List members",
							Mutation:    new(false),
							Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true, Positional: true}},
							Body:        []spec.Param{{Name: "limit", Type: "integer"}},
						},
						"list-continue": {
							Method:      "POST",
							Path:        "/groups/{id}/members/continue",
							Description: "Continue members",
							Mutation:    new(false),
							Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true, Positional: true}},
							Body:        []spec.Param{{Name: "cursor", Type: "string", Required: true}},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	toolsCode := stripGoComments(readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go"))
	requireLineContains(t, toolsCode,
		`"/folders/list",`,
		`mcpPageConfig{NextCursorPath: "cursor", HasMoreField: "has_more", ContinuationInput: "cursor", ExternalContinuation: true}`)
	requireLineContains(t, toolsCode,
		`"/folders/list/continue",`,
		`mcpPageConfig{CursorParam: "cursor", NextCursorPath: "cursor", HasMoreField: "has_more", ContinuationInput: "cursor", CursorInBody: true}`)
	requireLineContains(t, toolsCode,
		`"/folders/search_v2",`,
		`ExternalContinuation: true`)
	requireLineContains(t, toolsCode,
		`"/folders/search/continue_v2",`,
		`CursorInBody: true`)
	requireLineContains(t, toolsCode, `"/folders/delete",`, `mcpPageConfig{}`)
	requireLineContains(t, toolsCode, `"/folders/revisions",`, `mcpPageConfig{CursorParam: "after", NextCursorPath: "after"}`)
	requireLineContains(t, toolsCode, `"/groups/{id}/members",`, `ExternalContinuation: true`)
	requireLineContains(t, toolsCode, `"/items/search",`, `mcpPageConfig{CursorParam: "page_token", NextCursorPath: "page_token", HasMoreField: "more", ContinuationInput: "page_token"}`)
	requireLineContains(t, toolsCode, `"/items/nested",`, `BodyPath: []string{"paging", "cursor"}`)
	requireLineContains(t, toolsCode, `"/items/form",`, `CursorInBody: true`)
	requireLineContains(t, toolsCode, `"/items/opaque",`, `mcpPageConfig{}`)
	assert.NotContains(t, toolsCode, `mcplib.WithString("after"`)
	assert.Contains(t, toolsCode, `mcplib.WithString("cursor", mcplib.Description("Opaque pagination cursor returned by a previous MCP response"))`)

	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	runtimeSrc, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "mcp_post_page_runtime_test.txt"))
	require.NoError(t, err)
	envName := naming.EnvPrefix(apiSpec.Name) + "_BASE_URL"
	runtime := strings.ReplaceAll(string(runtimeSrc), "__BASE_URL_ENV__", envName)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "post_page_runtime_test.go"), []byte(runtime), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp/bound", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "TestGeneratedReadOnlyPOSTListPaging", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func requireLineContains(t *testing.T, src, needleA, needleB string) {
	t.Helper()
	for line := range strings.SplitSeq(src, "\n") {
		if strings.Contains(line, needleA) && strings.Contains(line, needleB) {
			return
		}
	}
	t.Fatalf("no generated line contains %q and %q", needleA, needleB)
}
