package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncOmitsLiveSearchIndexes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("listing-search")
	apiSpec.Resources = map[string]spec.Resource{
		"categories": {
			Description: "Categories",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/values/categories",
					Response: spec.ResponseDef{Type: "array", Item: "Category"},
				},
			},
		},
		"geo": {
			Description: "Regions",
			Endpoints: map[string]spec.Endpoint{
				"regions": {
					Method:   "GET",
					Path:     "/geo/regions",
					Response: spec.ResponseDef{Type: "array", Item: "Region"},
				},
			},
		},
		"ads": {
			Description: "Ad search",
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
			Description: "Listing search with a declared id",
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
			Description: "Items under a category",
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
			Description: "Search under a category",
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
			Description: "Opt-in walked search",
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
			Description: "Contact search",
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
			Description: "Widgets filtered by status",
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
			Description: "Opt-in catalog search",
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
			Description: "SQL collection",
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
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Category":        {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Region":          {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Ad":              {Fields: []spec.TypeField{{Name: "urn", Type: "string"}, {Name: "subject", Type: "string"}}},
		"Listing":         {Fields: []spec.TypeField{{Name: "urn", Type: "string"}, {Name: "title", Type: "string"}}},
		"ContactEnvelope": {Fields: []spec.TypeField{{Name: "contacts", Type: "array"}}},
		"Book":            {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "title", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string")
	assert.Contains(t, defaults, `"categories"`)
	assert.Contains(t, defaults, `"geo"`)
	assert.NotContains(t, defaults, `"ads"`)
	assert.NotContains(t, defaults, `"listings"`)
	assert.NotContains(t, defaults, `"widgets"`)

	paths := generatedFunctionBody(t, syncSrc, "func syncResourcePath(resource string) (string, error)")
	assert.Contains(t, paths, `"/values/categories"`)
	assert.Contains(t, paths, `"/geo/regions"`)
	assert.Contains(t, paths, `"/contacts/search"`)
	assert.Contains(t, paths, `"/widgets"`)
	assert.Contains(t, paths, `"/catalog/search"`)
	assert.Contains(t, paths, `"/query"`)
	assert.NotContains(t, paths, `"/search/items"`)
	assert.NotContains(t, paths, "detail-recommended")
	assert.NotContains(t, paths, `"/search/listings"`)

	dependents := generatedFunctionBody(t, syncSrc, "func dependentResourceDefs() []dependentResourceDef")
	assert.Contains(t, dependents, `"/categories/{categoryId}/items"`)
	assert.NotContains(t, dependents, `"/search/{categoryId}/items"`)
	assert.Contains(t, dependents, `"/search/{categoryId}/kept"`)
	for _, name := range []string{`"ads"`, `"ads-items"`, `"ads-detail-recommended"`, `"listings"`} {
		assert.NotContains(t, paths, name+":", "sync must not offer %s", name)
	}

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedReadOnlyPostsEmitNoImport(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("readonly-posts")
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Record lookups",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/records",
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
				"lookup": {
					Method:   "POST",
					Path:     "/records/lookup",
					Mutation: new(false),
					Body:     []spec.Param{{Name: "urn", Type: "string", Required: true}},
					Response: spec.ResponseDef{Type: "object", Item: "Record"},
				},
			},
		},
		"notes": {
			Description: "Note search",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:   "POST",
					Path:     "/notes/search",
					Mutation: new(false),
					Body:     []spec.Param{{Name: "q", Type: "string"}},
					Response: spec.ResponseDef{Type: "array", Item: "Note"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
		"Note":   {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, Import: true, MCP: true}
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "import.go"))
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NotContains(t, rootSrc, "newImportCmd(flags)")

	pathsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "resource_paths.go")
	writePaths := resourceWritePathsBlock(pathsSrc)
	assert.NotContains(t, writePaths, `"/records/lookup"`)
	assert.NotContains(t, writePaths, `"/notes/search"`)
	assert.NotRegexp(t, `(?m)^\s+"[^"]+": `, writePaths)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedImportKeepsCreateAndSkipsReadPost(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mixed-posts")
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/records",
					Response: spec.ResponseDef{Type: "array", Item: "Record"},
				},
				"create": {
					Method:   "POST",
					Path:     "/records",
					Body:     []spec.Param{{Name: "name", Type: "string", Required: true}},
					Response: spec.ResponseDef{Type: "object", Item: "Record"},
				},
				"lookup": {
					Method:   "POST",
					Path:     "/records/lookup",
					Mutation: new(false),
					Body:     []spec.Param{{Name: "urn", Type: "string", Required: true}},
					Response: spec.ResponseDef{Type: "object", Item: "Record"},
				},
			},
		},
		"notes": {
			Description: "Note search",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:   "POST",
					Path:     "/notes/search",
					Mutation: new(false),
					Body:     []spec.Param{{Name: "q", Type: "string"}},
					Response: spec.ResponseDef{Type: "array", Item: "Note"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Note":   {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, Import: true, MCP: true}
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "import.go"))
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, "newImportCmd(flags)")

	pathsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "resource_paths.go")
	writePaths := resourceWritePathsBlock(pathsSrc)
	assert.Contains(t, writePaths, `"records": "/records"`)
	assert.NotContains(t, writePaths, `"/records/lookup"`)
	assert.NotContains(t, writePaths, `"/notes/search"`)
	assert.NotContains(t, writePaths, `"notes"`)

	requireGeneratedCompiles(t, outputDir)
}

func resourceWritePathsBlock(src string) string {
	const startMark = "var resourceWritePaths = map[string]string{"
	_, rest, ok := strings.Cut(src, startMark)
	if !ok {
		return ""
	}
	block, _, ok := strings.Cut(rest, "\n}")
	if !ok {
		return rest
	}
	return block
}
