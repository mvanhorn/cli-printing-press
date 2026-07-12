package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedGraphQLImportIsNotREST verifies that for a GraphQL spec the
// import command is rendered from graphql_import.go.tmpl (clear unsupported
// error) instead of the REST import.go.tmpl, which would POST /<resource> per
// record and always 400 against a GraphQL-only API.
func TestGeneratedGraphQLImportIsNotREST(t *testing.T) {
	t.Parallel()

	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)
	require.True(t, isGraphQLSpec(gqlSpec), "test fixture must be a GraphQL spec")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	gen := New(gqlSpec, outputDir)
	// Force the import vision command on so Generate() emits import.go without
	// depending on the fixture's feature scores.
	gen.VisionSet = VisionTemplateSet{Import: true, MCP: true}
	require.NoError(t, gen.Generate())

	importGo := readGeneratedFile(t, outputDir, "internal", "cli", "import.go")

	assert.Contains(t, importGo, "import is not supported for",
		"GraphQL import.go must return the unsupported-on-GraphQL error")
	assert.Contains(t, importGo, "create --help",
		"GraphQL import.go must point users at the typed create command")
	assert.NotContains(t, importGo, "c.Post(",
		"GraphQL import.go must not fire REST POST requests")
	assert.NotContains(t, importGo, `path := "/" + resource`,
		"GraphQL import.go must not build a REST resource path")
	// Surface-compat claim in graphql_import.go.tmpl: the same flags the REST
	// import command registers must exist here so scripts/SKILL references that
	// pass them don't hit "unknown flag" on a GraphQL CLI.
	for _, flag := range []string{`"input"`, `"dry-run"`, `"batch-size"`} {
		assert.Contains(t, importGo, flag,
			"GraphQL import.go must register %s for surface compatibility with the REST import command", flag)
	}

	requireGeneratedCompiles(t, outputDir)
}

// TestGeneratedRESTImportStaysREST locks the R8 invariant: a REST spec keeps
// the REST import template (POST per record). The GraphQL fix must not change
// REST CLIs.
func TestGeneratedRESTImportStaysREST(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("restimportapi")
	apiSpec.Resources["items"].Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create item",
		Body:        []spec.Param{{Name: "name", Type: "string", Required: true}},
		Response:    spec.ResponseDef{Type: "object"},
	}
	require.False(t, isGraphQLSpec(apiSpec), "minimalSpec must be a REST spec")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Import: true, MCP: true}
	require.NoError(t, gen.Generate())

	importGo := readGeneratedFile(t, outputDir, "internal", "cli", "import.go")

	assert.Contains(t, importGo, "c.Post(",
		"REST import.go must keep issuing POST requests")
	assert.NotContains(t, importGo, "import is not supported for",
		"REST import.go must not carry the GraphQL unsupported error")

	requireGeneratedCompiles(t, outputDir)
}
