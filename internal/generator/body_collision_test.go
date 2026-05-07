package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDeduplicatesCamelCollidingBodyFields covers issue #287, the
// body-field analogue of #275 F-2. Two body fields whose Go identifiers
// collapse to the same `body<Camel>` after camelization (e.g., `start_time`
// and `StartTime` both yield `bodyStartTime`) currently produce duplicate
// `var body<X>` declarations and refuse to compile. The fix mirrors F-2:
// extend the dedup pass to walk Endpoint.Body and uniquify IdentName when
// body fields would collide.
func TestGenerateDeduplicatesCamelCollidingBodyFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-body")
	apiSpec.Resources["events"] = spec.Resource{
		Description: "Events",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/events",
				Description: "Create an event with a custom timestamp",
				Body: []spec.Param{
					{Name: "start_time", Type: "string", Description: "Snake-case form"},
					{Name: "StartTime", Type: "string", Description: "PascalCase form"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/events/{id}",
				Description: "Get one event",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-body-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	bodyVars, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "events_create.go"))

	assertNoDuplicates(t, bodyVars,
		"each body field must produce a distinct Go identifier")
	assertNoDuplicates(t, flagBindings,
		"each body field must register a distinct cobra flag name")
	require.Len(t, bodyVars, 2,
		"both body fields must still be represented after dedup")
}

// TestGenerateRenamesBodyFieldCollidingWithQueryParam guards the cross-
// namespace cobra flag collision: a body field and a query param can each
// register a cobra flag with the same name, and cobra rejects the second
// registration at runtime. The dedup pass must rename one side so the CLI
// flags stay distinct.
func TestGenerateRenamesBodyFieldCollidingWithQueryParam(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-cross")
	apiSpec.Resources["posts"] = spec.Resource{
		Description: "Posts",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/posts",
				Description: "Create a post; the dry-run query param shares a name with a body field",
				Params: []spec.Param{
					{Name: "tags", Type: "string", Description: "Query filter for tags"},
				},
				Body: []spec.Param{
					{Name: "tags", Type: "string", Description: "Tags to set on the post"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/posts/{id}",
				Description: "Get one post",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-cross-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "posts_create.go"))

	assertNoDuplicates(t, flagBindings,
		"--tags from a body field must not collide with --tags from a query param")
	assert.Contains(t, flagBindings, "tags",
		"the first registrant keeps the canonical flag name")
	assert.Contains(t, flagBindings, "tags-2",
		"the colliding body field gets the deduped public flag name")

	mcpTools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	mcpSource := string(mcpTools)
	assert.Contains(t, mcpSource, `mcplib.WithString("tags"`)
	assert.Contains(t, mcpSource, `mcplib.WithString("tags-2"`)
	assert.Contains(t, mcpSource, `PublicName: "tags", WireName: "tags", Location: "query"`)
	assert.Contains(t, mcpSource, `PublicName: "tags-2", WireName: "tags", Location: "body"`)
}

// TestGenerateRenamesBodyFieldCollidingWithStdin guards against a body field
// literally named `stdin` colliding with the `--stdin` flag the template
// emits for POST/PUT/PATCH endpoints (command_endpoint.go.tmpl:525).
func TestGenerateRenamesBodyFieldCollidingWithStdin(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-stdin")
	apiSpec.Resources["uploads"] = spec.Resource{
		Description: "Uploads",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/uploads",
				Description: "Create an upload",
				Body: []spec.Param{
					{Name: "stdin", Type: "string", Description: "A field unfortunately named stdin"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/uploads/{id}",
				Description: "Get one upload",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-stdin-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "uploads_create.go"))

	assertNoDuplicates(t, flagBindings,
		"the body field named 'stdin' must not collide with the template's --stdin flag")
}

// parseBodyDeclarations returns the names of all `var bodyXxx` declarations
// and the literal cobra flag names registered. Cobra registrations may come
// from either flag<X> or body<X> Go identifiers, so the flag-binding return
// covers the full namespace.
func parseBodyDeclarations(t *testing.T, path string) (vars, bindings []string) {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err, "read generated file")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err, "generated file must parse as Go")

	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			if decl.Tok != token.VAR {
				return true
			}
			for _, sp := range decl.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					// Match body<Suffix> declarations only; the bare `body`
					// variable is the request-body map the template uses
					// to assemble the JSON payload, not a per-field var.
					if len(name.Name) > 4 && strings.HasPrefix(name.Name, "body") {
						vars = append(vars, name.Name)
					}
				}
			}
		case *ast.CallExpr:
			sel, ok := decl.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(sel.Sel.Name, "Var") {
				return true
			}
			if len(decl.Args) < 2 {
				return true
			}
			lit, ok := decl.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			bindings = append(bindings, strings.Trim(lit.Value, `"`))
		}
		return true
	})
	return vars, bindings
}
