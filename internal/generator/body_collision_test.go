package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
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

// TestGenerateDeduplicatesNestedTreesCollapsedToSameIdent guards the
// case where two distinct nested-object paths whose joined camelized
// segments collapse to the same Go identifier. Project.Customer.name
// and ProjectCustomer.name (a sibling object literally named
// projectCustomer) both produce bodyProjectCustomerName, and the dedup
// pass must rename one of them.
func TestGenerateDeduplicatesNestedTreesCollapsedToSameIdent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-collapsed")
	apiSpec.Resources["entries"] = spec.Resource{
		Description: "Entries",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/entries",
				Description: "Two nested objects whose joined paths camelize identically",
				Body: []spec.Param{
					{Name: "project", Type: "object", Description: "Project wrapper", Fields: []spec.Param{
						{Name: "customer", Type: "object", Description: "Customer (nested via project)", Fields: []spec.Param{
							{Name: "name", Type: "string", Description: "Name via project.customer"},
						}},
					}},
					{Name: "projectCustomer", Type: "object", Description: "Project-customer (sibling at top)", Fields: []spec.Param{
						{Name: "name", Type: "string", Description: "Name via projectCustomer"},
					}},
				},
			},
			"get": {Method: "GET", Path: "/entries/{id}", Description: "Get one"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-collapsed-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	bodyVars, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "entries_create.go"))

	assertNoDuplicates(t, bodyVars,
		"nested paths whose joined camelized identifiers collapse to the same Go name must dedupe")
	assertNoDuplicates(t, flagBindings,
		"nested paths whose joined camelized flag names collapse must dedupe")
	require.Len(t, bodyVars, 2,
		"both collapsed paths must survive dedup as distinct Go identifiers")
	assert.Contains(t, flagBindings, "project-customer-name",
		"the first registrant keeps the canonical cobra flag name")
	assert.Contains(t, flagBindings, "project-customer-name-2",
		"the deduped sibling carries the -2 suffix")
}

// TestGenerateDeduplicatesConvergentNestedBodyPaths guards two distinct
// nested body paths that converge on the same trailing segments
// (Project.Customer.name AND Project.PostalAddress.Customer.name). The
// identPrefix-based walker joins the full path so both leaves produce
// unique identifiers (bodyProjectCustomerName vs
// bodyProjectPostalAddressCustomerName) without falling back on the _N
// suffix.
func TestGenerateDeduplicatesConvergentNestedBodyPaths(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-convergent")
	apiSpec.Resources["assets"] = spec.Resource{
		Description: "Assets",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/assets",
				Description: "Create an asset whose body has convergent nested paths",
				Body: []spec.Param{
					{Name: "project", Type: "object", Description: "Project root", Fields: []spec.Param{
						{Name: "customer", Type: "object", Description: "Direct customer", Fields: []spec.Param{
							{Name: "name", Type: "string", Description: "Customer name (direct)"},
						}},
						{Name: "postalAddress", Type: "object", Description: "Postal address", Fields: []spec.Param{
							{Name: "customer", Type: "object", Description: "Postal address customer", Fields: []spec.Param{
								{Name: "name", Type: "string", Description: "Customer name (via address)"},
							}},
						}},
					}},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/assets/{id}",
				Description: "Get one asset",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-convergent-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	bodyVars, flagBindings := parseBodyDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "assets_create.go"))

	assertNoDuplicates(t, bodyVars,
		"convergent nested paths must produce distinct Go identifiers")
	assertNoDuplicates(t, flagBindings,
		"convergent nested paths must register distinct cobra flag names")
	require.Len(t, bodyVars, 2,
		"both convergent name leaves must survive as distinct Go identifiers")
	assert.Contains(t, bodyVars, "bodyProjectCustomerName",
		"the direct project.customer.name leaf produces this identifier")
	assert.Contains(t, bodyVars, "bodyProjectPostalAddressCustomerName",
		"the project.postalAddress.customer.name leaf produces this identifier via path-prefix joining")
}

// TestGenerateDeduplicatesCyclicRefBodyShape drives the full OpenAPI
// parse → dedupe → render path on a body schema where $refs chain
// through multiple paths that re-enter the same component schema. The
// parser's cycle detection terminates the recursion when it revisits a
// schema pointer, producing leaf entries at each cut point. The
// generator's dedupe pass then walks the resulting tree and uniquifies
// any collisions so every cycle-cut leaf and every direct leaf emit
// distinct Go identifiers.
func TestGenerateDeduplicatesCyclicRefBodyShape(t *testing.T) {
	t.Parallel()

	yaml := `openapi: 3.0.0
info:
  title: cyclic-shape
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /asset:
    post:
      operationId: createAsset
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Asset'
      responses:
        '200':
          description: ok
  /asset/{id}:
    get:
      operationId: getAsset
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        '200':
          description: ok
components:
  schemas:
    Asset:
      type: object
      properties:
        project: {$ref: '#/components/schemas/Project'}
    Project:
      type: object
      properties:
        customer: {$ref: '#/components/schemas/Customer'}
    Customer:
      type: object
      properties:
        name: {type: string}
        ledgerAccount: {$ref: '#/components/schemas/LedgerAccount'}
        postalAddress: {$ref: '#/components/schemas/PostalAddress'}
    LedgerAccount:
      type: object
      properties:
        vatType: {$ref: '#/components/schemas/VatType'}
    VatType:
      type: object
      properties:
        customer: {$ref: '#/components/schemas/Customer'}
    PostalAddress:
      type: object
      properties:
        customer: {$ref: '#/components/schemas/Customer'}
`
	apiSpec, err := openapi.Parse([]byte(yaml))
	require.NoError(t, err)

	apiSpec.Name = "cyclic-shape"
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	if apiSpec.Auth.Type == "" {
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"CYCLIC_SHAPE_TOKEN"},
		}
	}
	apiSpec.Config = spec.ConfigSpec{
		Format: "toml",
		Path:   "~/.config/cyclic-shape-pp-cli/config.toml",
	}

	outputDir := filepath.Join(t.TempDir(), "cyclic-shape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// The endpoint file name uses the resource derived by the parser. The
	// asset resource path is /asset; the POST endpoint becomes <resource>_<op>.go.
	postFile := filepath.Join(outputDir, "internal", "cli", "asset_create-asset.go")
	if _, err := os.Stat(postFile); err != nil {
		postFile = filepath.Join(outputDir, "internal", "cli", "asset_create.go")
	}
	require.FileExists(t, postFile,
		"the createAsset POST command file must exist")

	bodyVars, flagBindings := parseBodyDeclarations(t, postFile)

	assertNoDuplicates(t, bodyVars,
		"cyclic-ref body shape must produce distinct Go identifiers for every emitted var")
	assertNoDuplicates(t, flagBindings,
		"cyclic-ref body shape must register distinct cobra flag names")

	// The cycle-cut shape produces three leaves: the direct
	// project.customer.name string and two cycle-cut customer objects
	// at distinct paths.
	require.Len(t, bodyVars, 3,
		"every direct and cycle-cut leaf must survive dedup as a distinct Go identifier")
	assert.Contains(t, bodyVars, "bodyProjectCustomerName",
		"the direct project.customer.name leaf must emit bodyProjectCustomerName")
	assert.Contains(t, bodyVars, "bodyProjectCustomerLedgerAccountVatTypeCustomer",
		"the ledgerAccount.vatType.customer cycle-cut leaf must emit a unique identifier under its full path")
	assert.Contains(t, bodyVars, "bodyProjectCustomerPostalAddressCustomer",
		"the postalAddress.customer cycle-cut leaf must emit a unique identifier under its full path")
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
