package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

// TestGenerateDeduplicatesNestedBodyFieldCollidingWithSiblingScalar guards
// the dot-flatten/sibling-scalar collision class. When a body schema declares
// both a top-level convenience scalar (e.g., `leadAccountId`) and a nested
// object whose dot-flattened path camelizes to the same Go identifier (e.g.,
// `lead.accountId`), both produce `bodyLeadAccountId` after camelization. The
// dedup pass must walk Body recursively so the post-flatten collision is
// detected; otherwise the generated handler emits duplicate `var
// bodyLeadAccountId` declarations and refuses to compile.
func TestGenerateDeduplicatesNestedBodyFieldCollidingWithSiblingScalar(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-nested")
	apiSpec.Resources["components"] = spec.Resource{
		Description: "Components",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/components",
				Description: "Create a component with deprecated and canonical lead fields",
				Body: []spec.Param{
					{Name: "leadAccountId", Type: "string", Description: "Deprecated convenience scalar"},
					{Name: "lead", Type: "object", Description: "Canonical nested object", Fields: []spec.Param{
						{Name: "accountId", Type: "string", Description: "Account id of the lead"},
					}},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/components/{id}",
				Description: "Get one component",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-nested-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	bodyVars, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "components_create.go"))

	assertNoDuplicates(t, bodyVars,
		"a nested-object leaf must produce a Go identifier distinct from a sibling scalar that camelizes to the same name")
	assertNoDuplicates(t, flagBindings,
		"a nested-object leaf must register a cobra flag name distinct from a sibling scalar")
	require.Len(t, bodyVars, 2,
		"both the convenience scalar and the nested field must survive dedup")
	assert.Contains(t, bodyVars, "bodyLeadAccountId",
		"one of the colliding fields keeps the canonical Go identifier")
	assert.Contains(t, bodyVars, "bodyLeadAccountId2",
		"the deduped field uses the _2 suffix convention")
	assert.Contains(t, flagBindings, "lead-account-id",
		"one of the colliding fields keeps the canonical cobra flag name")
	assert.Contains(t, flagBindings, "lead-account-id-2",
		"the deduped field's cobra flag carries the -2 suffix")
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

// TestFlattenCollidingBodyFields_NestedPrefixShape covers the Atlassian
// ProjectComponent shape: a top-level scalar `leadAccountId` plus a
// sibling `lead` object whose nested `accountId` would expand to the
// same Go identifier `bodyLeadAccountId`. The parser-side seenCamelNames
// dedup only checks top-level names, so the collision surfaces in the
// generator. flattenCollidingBodyFields must clear the offending
// parent's Fields so it falls through to the JSON-blob branch.
func TestFlattenCollidingBodyFields_NestedPrefixShape(t *testing.T) {
	t.Parallel()

	body := []spec.Param{
		{Name: "leadAccountId", Type: "string"},
		{
			Name: "lead",
			Type: "object",
			Fields: []spec.Param{
				{Name: "accountId", Type: "string"},
				{Name: "displayName", Type: "string"},
			},
		},
	}

	got := flattenCollidingBodyFields(body)

	require.Len(t, got, 2)
	assert.Equal(t, "leadAccountId", got[0].Name)
	assert.Empty(t, got[0].Fields, "top-level scalar is untouched")
	assert.Equal(t, "lead", got[1].Name)
	assert.Empty(t, got[1].Fields,
		"colliding parent must have Fields cleared so it falls through to JSON-blob")
}

// TestFlattenCollidingBodyFields_NoCollisionPassesThrough guards the
// common case: when nested expansion is collision-free the helper must
// not strip Fields. Two unrelated objects with non-colliding leaf names
// (the canonical start/end DateTimeTimeZone example from #957) must
// round-trip with Fields intact.
func TestFlattenCollidingBodyFields_NoCollisionPassesThrough(t *testing.T) {
	t.Parallel()

	body := []spec.Param{
		{
			Name: "start",
			Type: "object",
			Fields: []spec.Param{
				{Name: "dateTime", Type: "string"},
				{Name: "timeZone", Type: "string"},
			},
		},
		{
			Name: "end",
			Type: "object",
			Fields: []spec.Param{
				{Name: "dateTime", Type: "string"},
				{Name: "timeZone", Type: "string"},
			},
		},
	}

	got := flattenCollidingBodyFields(body)

	require.Len(t, got, 2)
	for _, p := range got {
		assert.Len(t, p.Fields, 2, "nested object %q keeps its 2 Fields", p.Name)
	}
}

// TestGenerateProjectComponentShapeCompiles is the end-to-end regression
// for the Atlassian Jira validate-catalog failure: a POST endpoint whose
// body contains both `leadAccountId` (scalar) and `lead` (object with
// nested `accountId`) must produce a generated CLI that compiles. Before
// the flattenCollidingBodyFields pass, this shape emitted two
// `var bodyLeadAccountId string` declarations and failed govulncheck's
// load step with "redeclared in this block".
func TestGenerateProjectComponentShapeCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-nested")
	apiSpec.Resources["components"] = spec.Resource{
		Description: "Components",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/components",
				Description: "Create a component (Jira ProjectComponent shape)",
				Body: []spec.Param{
					{Name: "leadAccountId", Type: "string", Description: "Lead user account ID (top-level)"},
					{
						Name:        "lead",
						Type:        "object",
						Description: "Lead user details (nested object)",
						Fields: []spec.Param{
							{Name: "accountId", Type: "string", Description: "Account ID inside the lead object"},
							{Name: "displayName", Type: "string", Description: "Display name"},
						},
					},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/components/{id}",
				Description: "Get one component",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-nested-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	bodyVars, _ := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "components_create.go"))

	assertNoDuplicates(t, bodyVars,
		"nested-prefix collision must not produce duplicate `var body<X>` declarations")
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
