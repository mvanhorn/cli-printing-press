package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePetstore(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "petstore", parsed.Name)
	assert.Equal(t, "", parsed.BaseURL)
	assert.Equal(t, "/api/v3", parsed.BasePath)
	// REST specs must leave the GraphQL-only fields unset; the generated
	// graphql_client template is gated on isGraphQLSpec so a stray value here
	// would silently leak into REST clients that never call POST /graphql.
	assert.Empty(t, parsed.GraphQLEndpointPath)
	assert.Empty(t, parsed.EndpointTemplateVars)
	assert.NotEmpty(t, parsed.Resources)

	hasEndpoint := false
	for _, resource := range parsed.Resources {
		if len(resource.Endpoints) > 0 {
			hasEndpoint = true
			break
		}
	}
	assert.True(t, hasEndpoint)

	assert.NotEmpty(t, parsed.Types)
	assert.Contains(t, parsed.Types, "Pet")
}

func TestParseLenientStubsMissingLocalSchemaRefs(t *testing.T) {
	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  widget:
                    $ref: "#/components/schemas/MissingWidget"
components:
  schemas:
    Present:
      type: object
      properties:
        id:
          type: string
`)

	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = ParseLenient(specBytes)
	})

	require.NoError(t, err)
	require.NotNil(t, parsed)
	normalized, normalizeErr := normalizeSpecData(specBytes)
	require.NoError(t, normalizeErr)
	stubbed, count, stubErr := stubMissingLocalSchemaRefs(normalized)
	require.NoError(t, stubErr)
	require.Equal(t, 1, count)
	var root map[string]any
	require.NoError(t, json.Unmarshal(stubbed, &root))
	schema := root["components"].(map[string]any)["schemas"].(map[string]any)["MissingWidget"].(map[string]any)
	assert.Equal(t, "object", schema["type"])
	assert.Equal(t, true, schema["additionalProperties"])
	assert.Contains(t, parsed.Types, "MissingWidget")
	assert.Contains(t, warnings, `warning: stubbing missing local schema ref "MissingWidget"`)
}

func TestParseMissingLocalSchemaRefsStayStrictByDefault(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/MissingWidget"
components:
  schemas: {}
`)

	_, err := Parse(specBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `map key "MissingWidget" not found`)
}

func TestParseStrictRefsDisablesLenientLocalSchemaStubs(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/MissingWidget"
components:
  schemas: {}
`)

	_, err := ParseWithOptions(specBytes, ParseOptions{Lenient: true, StrictRefs: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing local schema refs: MissingWidget")
}

func TestParseStrictRefsFailsBeforeLenientPathStripping(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /broken:
    get:
      operationId: getBroken
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/MissingWidget"
  /healthy:
    get:
      operationId: getHealthy
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
components:
  schemas: {}
`)

	_, err := ParseWithOptions(specBytes, ParseOptions{Lenient: true, StrictRefs: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing local schema refs: MissingWidget")
}

func TestParseLenientIgnoresRefStringsInsideExampleData(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Example Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
              example:
                widget:
                  $ref: "#/components/schemas/MissingWidget"
components:
  schemas: {}
`)

	parsed, err := ParseLenient(specBytes)
	require.NoError(t, err)
	assert.NotContains(t, parsed.Types, "MissingWidget")
}

func TestParseLenientDoesNotStubNestedLocalSchemaPointers(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Nested Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/MissingWidget/properties/id"
components:
  schemas: {}
`)

	_, err := ParseLenient(specBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `map key "MissingWidget" not found`)
}

func TestParseLenientCleanSpecDoesNotWarnAboutSchemaStubs(t *testing.T) {
	specBytes := []byte(`
openapi: 3.0.3
info:
  title: Clean API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Widget"
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
`)

	var err error
	warnings := captureWarnings(t, func() {
		_, err = ParseLenient(specBytes)
	})

	require.NoError(t, err)
	assert.NotContains(t, warnings, "stubbing missing local schema ref")
}

func TestParseMarksFieldSelectorParamsWithSyncDefault(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Field Selector API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /tasks:
    get:
      operationId: listTasks
      parameters:
        - name: opt_fields
          in: query
          description: Fields to return in the response.
          schema:
            type: string
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        gid:
                          type: string
                        completed:
                          type: boolean
                        assignee:
                          type: object
                          properties:
                            gid:
                              type: string
                            name:
                              type: string
                        custom_fields:
                          type: array
                          items:
                            type: object
                            properties:
                              gid:
                                type: string
                              display_value:
                                type: string
`))
	require.NoError(t, err)

	tasks := parsed.Resources["tasks"].Endpoints["list"]
	require.Len(t, tasks.Params, 1)
	assert.Equal(t, spec.ParamPurposeFieldSelector, tasks.Params[0].Purpose)
	assert.Equal(t, "gid,assignee.gid,completed,custom_fields.gid", tasks.Params[0].FieldSelectorDefault)
}

func TestMapParametersOnlyMarksQueryFieldSelectors(t *testing.T) {
	t.Parallel()

	pathItem := &openapi3.PathItem{}
	op := &openapi3.Operation{
		Parameters: openapi3.Parameters{
			{
				Value: &openapi3.Parameter{
					Name:        "fields",
					In:          openapi3.ParameterInPath,
					Description: "Fields to return in the response.",
					Required:    true,
					Schema:      openapi3.NewStringSchema().NewRef(),
				},
			},
			{
				Value: &openapi3.Parameter{
					Name:        "opt_fields",
					In:          openapi3.ParameterInQuery,
					Description: "Fields to return in the response.",
					Schema:      openapi3.NewStringSchema().NewRef(),
				},
			},
		},
	}

	params := mapParameters(pathItem, op)
	require.Len(t, params, 2)

	byName := make(map[string]spec.Param, len(params))
	for _, param := range params {
		byName[param.Name] = param
	}

	assert.Empty(t, byName["fields"].Purpose, "path params must not become sync query field selectors")
	assert.Equal(t, spec.ParamPurposeFieldSelector, byName["opt_fields"].Purpose)
}

func readAICLargeSpec(tb testing.TB) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "artic-openapi.json"))
	require.NoError(tb, err)
	return data
}

func countEndpoints(resources map[string]spec.Resource) int {
	total := 0
	for _, resource := range resources {
		total += len(resource.Endpoints)
		total += countEndpoints(resource.SubResources)
	}
	return total
}

func TestParseAICLargeSpecCompletes(t *testing.T) {
	t.Parallel()

	data := readAICLargeSpec(t)

	type parseResult struct {
		spec *spec.APISpec
		err  error
	}
	done := make(chan parseResult, 1)
	go func() {
		parsed, err := Parse(data)
		done <- parseResult{spec: parsed, err: err}
	}()

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, "art-institution-chicago", result.spec.Name)
		require.NotEmpty(t, result.spec.Resources)
		require.GreaterOrEqual(t, countEndpoints(result.spec.Resources), 100)
		require.Contains(t, result.spec.Resources, "artworks")
		require.Contains(t, result.spec.Resources["artworks"].Endpoints, "list")
		require.Contains(t, result.spec.Resources["artworks"].Endpoints, "get")
		search := findEndpoint(t, result.spec, "/search")
		assert.GreaterOrEqual(t, len(search.Params), 6, "expected shared parameter refs on /search to resolve")
	case <-time.After(60 * time.Second):
		t.Fatal("AIC OpenAPI 3.1 spec parse did not complete within 60 seconds")
	}
}

func BenchmarkLargeSpec(b *testing.B) {
	data := readAICLargeSpec(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parsed, err := Parse(data)
		if err != nil {
			b.Fatalf("parse AIC spec: %v", err)
		}
		if len(parsed.Resources) == 0 {
			b.Fatal("expected parsed resources")
		}
	}
}

func TestParseFileResolvesLocalRefsRelativeToSpecDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appsDir := filepath.Join(dir, "apps")
	commonDir := filepath.Join(dir, "common")
	require.NoError(t, os.MkdirAll(appsDir, 0o755))
	require.NoError(t, os.MkdirAll(commonDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(commonDir, "schemas.json"), []byte(`{
  "components": {
    "schemas": {
      "Widget": {
        "type": "object",
        "properties": {
          "id": {"type": "string"}
        }
      }
    }
  }
}`), 0o644))

	specPath := filepath.Join(appsDir, "openapi.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
openapi: 3.0.3
info:
  title: Modular Widgets
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "../common/schemas.json#/components/schemas/Widget"
`), 0o644))

	parsed, err := ParseFile(specPath)
	require.NoError(t, err)

	var foundWidgetID bool
	for _, typ := range parsed.Types {
		for _, field := range typ.Fields {
			if field.Name == "id" && field.Type == "string" {
				foundWidgetID = true
			}
		}
	}
	assert.True(t, foundWidgetID, "external schema fields must be available after local ref resolution")
}

func TestParseWithPathRejectsRemoteRefsInStrictMode(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: 3.0.3
info:
  title: Remote Ref
  version: 1.0.0
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "https://example.com/schemas.json#/components/schemas/Widget"
`)

	_, err := ParseWithPath(data, filepath.Join(t.TempDir(), "openapi.yaml"))
	require.ErrorContains(t, err, "encountered disallowed external reference")
}

func TestParsePreservesResponseDiscriminatorAndEnumFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Mixed Network
  version: 1.0.0
paths:
  /network-entities:
    get:
      operationId: listNetworkEntities
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      $ref: "#/components/schemas/NetworkEntity"
components:
  schemas:
    NetworkEntity:
      type: object
      discriminator:
        propertyName: type
        mapping:
          workspace: "#/components/schemas/Workspace"
          collection: "#/components/schemas/Collection"
      properties:
        type:
          type: string
          enum: [workspace, collection]
        id:
          type: string
    Workspace:
      type: object
      properties:
        id:
          type: string
    Collection:
      type: object
      properties:
        id:
          type: string
`))
	require.NoError(t, err)

	endpoint := parsed.Resources["network-entities"].Endpoints["list"]
	require.NotNil(t, endpoint.Response.Discriminator)
	assert.Equal(t, "type", endpoint.Response.Discriminator.Field)
	assert.Equal(t, map[string]string{
		"collection": "Collection",
		"workspace":  "Workspace",
	}, endpoint.Response.Discriminator.Mapping)

	var typeField spec.TypeField
	for _, field := range parsed.Types["NetworkEntity"].Fields {
		if field.Name == "type" {
			typeField = field
			break
		}
	}
	assert.Equal(t, []string{"workspace", "collection"}, typeField.Enum)
}

// TestParseRegistersInlineListResponseItemTypes pins that inline list-
// response item schemas land in APISpec.Types under the synthetic name
// mapResponse stores in endpoint.Response.Item. Without this, a list
// endpoint whose item schema is declared inline produces a Types miss and
// the generated store table degrades to id/data/synced_at, losing every
// typed column for that resource.
func TestParseRegistersInlineListResponseItemTypes(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Inline Issues API
  version: 1.0.0
paths:
  /issues:
    get:
      operationId: listIssues
      parameters:
        - name: filter
          in: query
          schema:
            type: string
        - name: state
          in: query
          schema:
            type: string
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: integer
                    title:
                      type: string
                    state:
                      type: string
                    created_at:
                      type: string
                      format: date-time
`))
	require.NoError(t, err)

	endpoint := parsed.Resources["issues"].Endpoints["list"]
	require.NotEmpty(t, endpoint.Response.Item, "list endpoint should resolve a response item type name")

	typeDef, ok := parsed.Types[endpoint.Response.Item]
	require.True(t, ok, "inline response item schema must register into Types under %q",
		endpoint.Response.Item)

	fieldsByName := map[string]spec.TypeField{}
	for _, f := range typeDef.Fields {
		fieldsByName[f.Name] = f
	}

	for _, want := range []string{"id", "title", "state", "created_at"} {
		_, ok := fieldsByName[want]
		assert.True(t, ok, "expected response field %q registered under inline item type", want)
	}

	// Format hint must propagate so DATETIME columns survive end-to-end.
	assert.Equal(t, "date-time", fieldsByName["created_at"].Format,
		"created_at format must be carried through TypeField for DATETIME mapping")

	// Request-side query parameters must NOT bleed into the response type.
	for _, leak := range []string{"filter"} {
		_, leaked := fieldsByName[leak]
		assert.False(t, leaked, "request parameter %q must not appear in response Type", leak)
	}
}

// TestParseInlineItemTypesNamespacedByResource pins that two resources
// whose default GET endpoints share an endpointName ("list") and both
// declare inline (no-$ref, no-title) array-item schemas get distinct
// Types entries. Without resource-namespacing, both registrations would
// land on the same synthetic name and the second resource would silently
// inherit the first's response shape — re-introducing the wrong-columns
// bug class for cross-resource cases.
func TestParseInlineItemTypesNamespacedByResource(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Two Resources
  version: 1.0.0
paths:
  /issues:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    issue_field:
                      type: string
  /users:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    user_field:
                      type: string
`))
	require.NoError(t, err)

	issuesItem := parsed.Resources["issues"].Endpoints["list"].Response.Item
	usersItem := parsed.Resources["users"].Endpoints["list"].Response.Item
	require.NotEmpty(t, issuesItem)
	require.NotEmpty(t, usersItem)
	assert.NotEqual(t, issuesItem, usersItem,
		"two resources with default-named GET endpoints must produce distinct synthetic item type names")

	issuesType, ok := parsed.Types[issuesItem]
	require.True(t, ok)
	usersType, ok := parsed.Types[usersItem]
	require.True(t, ok)

	issuesNames := []string{}
	for _, f := range issuesType.Fields {
		issuesNames = append(issuesNames, f.Name)
	}
	usersNames := []string{}
	for _, f := range usersType.Fields {
		usersNames = append(usersNames, f.Name)
	}
	assert.Contains(t, issuesNames, "issue_field")
	assert.NotContains(t, issuesNames, "user_field",
		"issues item type must not contain users' fields")
	assert.Contains(t, usersNames, "user_field")
	assert.NotContains(t, usersNames, "issue_field",
		"users item type must not contain issues' fields")
}

// TestParseRegistersInlineSingleObjectResponseTypes pins that detail-only
// resources (GET /x/{id} with a single-object inline response) get their
// per-item schema registered into Types. Without registration, BuildSchema
// would degrade these tables to id/data/synced_at and lose typed columns
// for any API that exposes only a detail endpoint.
func TestParseRegistersInlineSingleObjectResponseTypes(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Detail Only
  version: 1.0.0
paths:
  /widgets/{id}:
    get:
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  name:
                    type: string
                  created_at:
                    type: string
                    format: date-time
`))
	require.NoError(t, err)

	endpoint := parsed.Resources["widgets"].Endpoints["get"]
	require.NotEmpty(t, endpoint.Response.Item)

	typeDef, ok := parsed.Types[endpoint.Response.Item]
	require.True(t, ok, "single-object inline response should register a Types entry under %q", endpoint.Response.Item)

	names := []string{}
	for _, f := range typeDef.Fields {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "name")
	assert.Contains(t, names, "created_at")
}

// TestParseAndBuildSchemaSourcesColumnsFromResponse is the end-to-end
// regression for the OpenAPI parser → BuildSchema seam. A list endpoint
// with filter/sort/pagination query parameters must produce a SQLite
// table whose columns mirror the response item schema, not the request-
// side parameters — otherwise sync stores nothing in those columns and
// SQL queries silently return NULL.
func TestParseAndBuildSchemaSourcesColumnsFromResponse(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Issues
  version: 1.0.0
paths:
  /issues:
    get:
      operationId: listIssues
      parameters:
        - name: filter
          in: query
          schema: { type: string }
        - name: state
          in: query
          schema: { type: string }
        - name: labels
          in: query
          schema: { type: string }
        - name: sort
          in: query
          schema: { type: string }
        - name: per_page
          in: query
          schema: { type: integer }
        - name: page
          in: query
          schema: { type: integer }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Issue"
components:
  schemas:
    Issue:
      type: object
      properties:
        id:
          type: integer
        number:
          type: integer
        title:
          type: string
        body:
          type: string
        state:
          type: string
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
`))
	require.NoError(t, err)

	tables := generator.BuildSchema(parsed)
	var issues *generator.TableDef
	for i := range tables {
		if tables[i].Name == "issues" {
			issues = &tables[i]
			break
		}
	}
	require.NotNil(t, issues, "issues table should be emitted from the parsed spec")

	cols := map[string]string{}
	for _, c := range issues.Columns {
		cols[c.Name] = c.Type
	}

	// Response-derived columns must be present.
	for _, want := range []string{"number", "title", "body", "state", "created_at", "updated_at"} {
		assert.Contains(t, cols, want, "expected column %q sourced from Issue schema", want)
	}

	// Query-param leaks the bug used to cause:
	for _, leak := range []string{"filter", "labels", "sort", "per_page", "page"} {
		assert.NotContains(t, cols, leak, "request param %q must not appear as a column", leak)
	}

	// Format hint flows through OpenAPI parser → TypeField → sqliteType.
	assert.Equal(t, "DATETIME", cols["created_at"], "date-time format must map to DATETIME column")
}

func TestParseMapsAllOfRequestBodyFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Banking API
  version: 1.0.0
paths:
  /account/{accountId}/transactions:
    post:
      operationId: createTransaction
      parameters:
        - name: accountId
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount, paymentMethod, recipientId, purpose]
              properties:
                amount:
                  allOf:
                    - $ref: "#/components/schemas/PositiveDollar"
                  description: Amount of USD you want to send.
                paymentMethod:
                  allOf:
                    - $ref: "#/components/schemas/PaymentMethod"
                recipientId:
                  allOf:
                    - $ref: "#/components/schemas/RecipientId"
                purpose:
                  allOf:
                    - $ref: "#/components/schemas/PaymentPurpose"
                    - $ref: "#/components/schemas/PurposeMetadata"
                details:
                  type: object
                  properties:
                    name:
                      type: string
                      description: Specific detail name.
                  allOf:
                    - $ref: "#/components/schemas/BaseDetails"
                mixed:
                  allOf:
                    - $ref: "#/components/schemas/PurposeMetadata"
                    - $ref: "#/components/schemas/StringEnumWrapper"
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
components:
  schemas:
    PositiveDollar:
      type: number
      format: double
      description: A positive dollar amount with at least 1 cent.
    PaymentMethod:
      type: string
      enum: [ach, check, domesticWire]
    RecipientId:
      type: string
      format: uuid
    PaymentPurpose:
      type: object
      properties:
        simple:
          type: string
    PurposeMetadata:
      type: object
      required: [memo]
      properties:
        memo:
          type: string
    BaseDetails:
      type: object
      properties:
        inherited:
          type: string
        name:
          type: string
          description: Base detail name.
    StringEnumWrapper:
      allOf:
        - type: string
          enum: [simple, complex]
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/account/{accountId}/transactions")
	byName := map[string]spec.Param{}
	for _, param := range endpoint.Body {
		byName[param.Name] = param
	}

	assert.Equal(t, "float", byName["amount"].Type)
	assert.Equal(t, "double", byName["amount"].Format)
	assert.Equal(t, "string", byName["paymentMethod"].Type)
	assert.Equal(t, []string{"ach", "check", "domesticWire"}, byName["paymentMethod"].Enum)
	assert.Equal(t, "string", byName["recipientId"].Type)
	assert.Equal(t, "uuid", byName["recipientId"].Format)
	assert.Equal(t, "object", byName["purpose"].Type)
	assert.Equal(t, []spec.Param{
		{Name: "memo", Type: "string", Required: true, Description: "Memo"},
		{Name: "simple", Type: "string", Description: "Simple"},
	}, byName["purpose"].Fields)
	assert.Equal(t, []spec.Param{
		{Name: "inherited", Type: "string", Description: "Inherited"},
		{Name: "name", Type: "string", Description: "Specific detail name."},
	}, byName["details"].Fields)
	assert.Equal(t, "object", byName["mixed"].Type)
	assert.Equal(t, []spec.Param{
		{Name: "memo", Type: "string", Required: true, Description: "Memo"},
	}, byName["mixed"].Fields)
}

const dataEnvelopeAllOfTaskSpec = `
openapi: 3.0.3
info:
  title: Task API
  version: 1.0.0
servers:
  - url: https://api.example.test
paths:
  /tasks/{task_gid}:
    patch:
      operationId: updateTask
      parameters:
        - name: task_gid
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                data:
                  $ref: "#/components/schemas/TaskUpdateRequest"
      responses:
        "200":
          description: ok
components:
  schemas:
    TaskBase:
      type: object
      required: [name]
      properties:
        completed:
          type: boolean
          description: Whether the task is complete.
        due_on:
          type: string
          format: date
          description: Date the task is due.
        html_notes:
          type: string
          description: HTML formatted text for the task notes.
        name:
          type: string
          description: Name of the task.
        notes:
          type: string
          description: Plain text task notes.
    TaskRequestBase:
      allOf:
        - $ref: "#/components/schemas/TaskBase"
        - type: object
          properties:
            assignee:
              type: string
              description: GID of the assignee.
    TaskUpdateRequest:
      allOf:
        - $ref: "#/components/schemas/TaskRequestBase"
        - type: object
          properties:
            custom_type:
              type: string
              description: GID of a task custom type.
`

func TestParseMapsDataEnvelopeAllOfRequestBodyFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(dataEnvelopeAllOfTaskSpec))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "PATCH", "/tasks/{task_gid}")
	require.Len(t, endpoint.Body, 1)
	data := endpoint.Body[0]
	require.Equal(t, "data", data.Name)
	require.Equal(t, "object", data.Type)

	fields := map[string]spec.Param{}
	for _, field := range data.Fields {
		fields[field.Name] = field
	}
	for _, want := range []string{"assignee", "completed", "custom_type", "due_on", "html_notes", "name", "notes"} {
		assert.Contains(t, fields, want)
	}
	assert.True(t, fields["name"].Required)
	assert.Equal(t, "bool", fields["completed"].Type)
	assert.Equal(t, "date", fields["due_on"].Format)
	assert.Equal(t, "string", fields["html_notes"].Type)
	assert.Equal(t, "HTML formatted text for the task notes.", fields["html_notes"].Description)
}

func TestGenerateDataEnvelopeAllOfBodyFlags(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("OpenAPI generated CLI compile coverage runs in the generated-test CI lane")
	}

	parsed, err := Parse([]byte(dataEnvelopeAllOfTaskSpec))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(parsed.Name))
	gen := generator.New(parsed, outputDir)
	gen.VisionSet = generator.VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	binaryPath := filepath.Join(outputDir, naming.CLI(parsed.Name))
	runGo(t, outputDir, "mod", "tidy")
	runGo(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(parsed.Name))

	helpOut, err := exec.Command(binaryPath, "tasks", "update-task", "--help").CombinedOutput()
	require.NoError(t, err, string(helpOut))
	help := string(helpOut)
	for _, want := range []string{
		"--data-name",
		"--data-notes",
		"--data-html-notes",
		"--data-assignee",
		"--data-completed",
		"--data-due-on",
		"--data-custom-type",
	} {
		assert.Contains(t, help, want)
	}
	assert.NotContains(t, help, "--data-data-")

	cmd := exec.Command(binaryPath, "tasks", "update-task", "123", "--data-html-notes", "<body>foo</body>", "--dry-run")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	bodyJSON := extractDryRunBody(t, string(out))
	var body struct {
		Data struct {
			HTMLNotes string `json:"html_notes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(bodyJSON), &body))
	assert.Equal(t, "<body>foo</body>", body.Data.HTMLNotes)
}

func TestParseRecursiveRequestBodyFieldsStopsAtCycle(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Recursive API
  version: 1.0.0
paths:
  /nodes:
    post:
      operationId: createNode
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [node]
              properties:
                node:
                  $ref: "#/components/schemas/Node"
      responses:
        "200":
          description: ok
components:
  schemas:
    Node:
      type: object
      required: [name]
      properties:
        name:
          type: string
        child:
          $ref: "#/components/schemas/Node"
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/nodes")
	require.Len(t, endpoint.Body, 1)
	require.Equal(t, "node", endpoint.Body[0].Name)
	assert.Equal(t, "object", endpoint.Body[0].Type)

	fieldsByName := map[string]spec.Param{}
	for _, field := range endpoint.Body[0].Fields {
		fieldsByName[field.Name] = field
	}
	assert.Equal(t, "string", fieldsByName["name"].Type)
	assert.Equal(t, "object", fieldsByName["child"].Type)
	assert.Empty(t, fieldsByName["child"].Fields)
}

func TestParseOneOfRequestBodyEmitsJSONFallback(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: DNS API
  version: 1.0.0
paths:
  /zones/{zoneId}/records:
    post:
      operationId: createRecord
      parameters:
        - name: zoneId
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              oneOf:
                - $ref: "#/components/schemas/ARecord"
                - $ref: "#/components/schemas/CNAMERecord"
      responses:
        "200":
          description: ok
components:
  schemas:
    ARecord:
      type: object
      required: [type, name, content]
      properties:
        type: {type: string, enum: [A]}
        name: {type: string}
        content: {type: string}
    CNAMERecord:
      type: object
      required: [type, name, content]
      properties:
        type: {type: string, enum: [CNAME]}
        name: {type: string}
        content: {type: string}
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/zones/{zoneId}/records")
	assert.True(t, endpoint.BodyJSONFallback, "endpoint with oneOf body should opt into --body-json fallback")
	assert.Empty(t, endpoint.Body, "BodyJSONFallback endpoints expose a single --body-json flag, not typed body params")
	assert.Equal(t, "application/json", endpoint.RequestContentType, "fallback endpoints should default to application/json")
}

func TestParseAnyOfRequestBodyEmitsJSONFallback(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Block API
  version: 1.0.0
paths:
  /blocks:
    post:
      operationId: createBlock
      requestBody:
        required: true
        content:
          application/json:
            schema:
              anyOf:
                - type: object
                  properties:
                    paragraph: {type: string}
                - type: object
                  properties:
                    heading: {type: string}
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/blocks")
	assert.True(t, endpoint.BodyJSONFallback)
	assert.True(t, endpoint.BodyRequired, "requestBody.required should thread through to Endpoint.BodyRequired")
	assert.Empty(t, endpoint.Body)
}

// TestParseOneOfRequestBodyPreservesVendorJSONContentType confirms a
// non-default JSON content type (application/vnd.api+json,
// application/problem+json, etc.) round-trips through the fallback path.
// The runtime decode is content-type-agnostic; what matters is that the
// declared type isn't multipart or form-urlencoded.
func TestParseOneOfRequestBodyPreservesVendorJSONContentType(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Vendor API
  version: 1.0.0
paths:
  /events:
    post:
      operationId: createEvent
      requestBody:
        content:
          application/vnd.api+json:
            schema:
              oneOf:
                - type: object
                  properties:
                    type: {type: string}
                - type: object
                  properties:
                    kind: {type: string}
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/events")
	assert.True(t, endpoint.BodyJSONFallback)
	assert.Equal(t, "application/vnd.api+json", endpoint.RequestContentType)
}

// TestParseOneOfRequestBodyMultipartDoesNotEmitJSONFallback covers the
// guard that prevents emitting a --body-json flag for non-JSON content
// types. The runtime JSON branch of command_endpoint.go.tmpl is not
// wired for multipart, so a fallback flag there would be dead.
func TestParseOneOfRequestBodyMultipartDoesNotEmitJSONFallback(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Upload API
  version: 1.0.0
paths:
  /uploads:
    post:
      operationId: createUpload
      requestBody:
        content:
          multipart/form-data:
            schema:
              oneOf:
                - type: object
                  properties:
                    file: {type: string, format: binary}
                - type: object
                  properties:
                    url: {type: string}
      responses:
        "200":
          description: ok
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/uploads")
	assert.False(t, endpoint.BodyJSONFallback, "multipart oneOf should not opt into the JSON fallback")
	assert.Empty(t, endpoint.Body)
}

func TestParseStytchOpenAPI(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "stytch", parsed.Name)
	assert.NotEmpty(t, parsed.BaseURL)
	assert.NotEmpty(t, parsed.Resources)
	assert.NotEmpty(t, parsed.Types)

	totalEndpoints := 0
	for _, resource := range parsed.Resources {
		totalEndpoints += len(resource.Endpoints)
		for _, sub := range resource.SubResources {
			totalEndpoints += len(sub.Endpoints)
		}
	}
	assert.Greater(t, totalEndpoints, 10)
}

func TestParseGmailOAuth2(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, "Authorization", parsed.Auth.Header)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/auth", parsed.Auth.AuthorizationURL)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/token", parsed.Auth.TokenURL)
	assert.NotEmpty(t, parsed.Auth.Scopes)
	// gmail uses authorization_code flow; OAuth2Grant stays empty so the
	// EffectiveOAuth2Grant() default of "authorization_code" applies.
	assert.Equal(t, "", parsed.Auth.OAuth2Grant)
}

func TestParseOAuth2ClientCredentialsFlow(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Auth0Mgmt
  version: "1.0"
servers:
  - url: https://example.auth0.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.auth0.com/oauth/token
          scopes:
            read:users: Read user profiles
            write:users: Manage users
paths:
  /api/v2/users:
    get:
      operationId: list users
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "parser keeps bearer_token shape; grant lives on OAuth2Grant")
	assert.Equal(t, "client_credentials", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.auth0.com/oauth/token", parsed.Auth.TokenURL)
	assert.Empty(t, parsed.Auth.AuthorizationURL, "client_credentials flow has no user redirect")
	assert.Equal(t, []string{"read:users", "write:users"}, parsed.Auth.Scopes)
}

func TestParseOAuth2BothFlowsPrefersClientCredentials(t *testing.T) {
	t.Parallel()

	// When a single OAuth2 scheme declares both authorizationCode and
	// clientCredentials flows, the parser prefers clientCredentials —
	// server-to-server is the more common shape for printed CLIs (which
	// run in CI/scripts, not interactive browsers). Spec authors override
	// post-import by setting OAuth2Grant explicitly.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Hybrid
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/oauth/authorize
          tokenUrl: https://example.com/oauth/token-ac
          scopes:
            user: User access
        clientCredentials:
          tokenUrl: https://example.com/oauth/token-cc
          scopes:
            admin: Admin access
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "client_credentials", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.com/oauth/token-cc", parsed.Auth.TokenURL,
		"clientCredentials tokenUrl wins, not authorizationCode's")
	assert.Empty(t, parsed.Auth.AuthorizationURL, "no user redirect for the cc flow")
	assert.Equal(t, []string{"admin"}, parsed.Auth.Scopes,
		"clientCredentials scopes win, not authorizationCode's")
}

func TestParseOAuth2ClientCredentialsMissingTokenURLSkipsBranch(t *testing.T) {
	t.Parallel()

	// Malformed spec: clientCredentials block exists but has no tokenUrl.
	// Parser should skip the cc branch and fall through to the next flow
	// (or leave fields empty if no other flows exist), not crash.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Malformed
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: ""
          scopes: {}
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes: {}
paths:
  /v1/foo:
    get:
      operationId: foo
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	// Falls through to authorizationCode since cc had no tokenUrl.
	assert.Equal(t, "", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.com/token", parsed.Auth.TokenURL)
	assert.Equal(t, "https://example.com/auth", parsed.Auth.AuthorizationURL)
}

func TestParseAuthPreferenceSelectsNamedScheme(t *testing.T) {
	t.Parallel()

	// Many real-world specs (Atlassian Jira, Confluence, Bitbucket, GitLab)
	// advertise both OAuth2 (with full authorizationCode flow) and HTTP Basic.
	// The default selector picks OAuth2 — correct for hosted multi-tenant
	// integrations but wrong for personal-token CLIs. AuthPreference lets the
	// catalog (or a generate caller) pin the simpler scheme.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Atlassian-like
  version: "1.0"
servers:
  - url: https://example.atlassian.net
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            read: read access
    basicAuth:
      type: http
      scheme: basic
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - basicAuth: []
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`)

	defaultParsed, err := Parse(specBytes)
	require.NoError(t, err)
	assert.Equal(t, "OAuth2", defaultParsed.Auth.Scheme, "without preference, OAuth2+AC wins by design")
	assert.Equal(t, "bearer_token", defaultParsed.Auth.Type)

	preferred, err := ParseWithOptions(specBytes, ParseOptions{AuthPreference: "basicAuth"})
	require.NoError(t, err)
	assert.Equal(t, "api_key", preferred.Auth.Type, "preference pins HTTP Basic")
	assert.Equal(t, "basicAuth", preferred.Auth.Scheme)
	assert.Equal(t, "Basic {username}:{password}", preferred.Auth.Format)
}

func TestParseAuthPreferenceCaseInsensitive(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Mixed
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://api.example.com/authorize
          tokenUrl: https://api.example.com/token
          scopes:
            read: read
    BasicAuth:
      type: http
      scheme: basic
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - BasicAuth: []
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`)

	preferred, err := ParseWithOptions(specBytes, ParseOptions{AuthPreference: "basicauth"})
	require.NoError(t, err)
	assert.Equal(t, "api_key", preferred.Auth.Type)
	assert.Equal(t, "BasicAuth", preferred.Auth.Scheme, "match is case-insensitive but result preserves spec casing")
}

func TestParseAuthPreferenceUnknownNameFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// An unknown preference name must not fail parse; it falls through to the
	// default selector so a typo in catalog yaml degrades gracefully.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Mixed
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://api.example.com/authorize
          tokenUrl: https://api.example.com/token
          scopes:
            read: read
    basicAuth:
      type: http
      scheme: basic
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - basicAuth: []
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`)

	parsed, err := ParseWithOptions(specBytes, ParseOptions{AuthPreference: "doesNotExist"})
	require.NoError(t, err)
	assert.Equal(t, "OAuth2", parsed.Auth.Scheme, "unknown preference falls back to default selector")
	assert.Equal(t, "bearer_token", parsed.Auth.Type)
}

func TestParseAuthPreferenceCanSelectUnusedComponentsScheme(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Preference Override
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            read: read access
paths:
  /v1/items:
    get:
      operationId: list items
      responses: {"200": {description: ok}}
`)

	parsed, err := ParseWithOptions(specBytes, ParseOptions{AuthPreference: "OAuth2"})
	require.NoError(t, err)

	assert.Equal(t, "OAuth2", parsed.Auth.Scheme, "explicit auth preference must override usage-based filtering")
	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"PREFERENCE_OVERRIDE_OAUTH2"}, parsed.Auth.EnvVars)
	assert.Equal(t, "https://auth.example.com/authorize", parsed.Auth.AuthorizationURL)
	assert.Equal(t, "https://auth.example.com/token", parsed.Auth.TokenURL)
}

func TestParseBearerPreservedOverOAuth2AuthCode(t *testing.T) {
	t.Parallel()

	// GitHub-style shape: an http/bearer scheme alongside a full OAuth2
	// authorizationCode flow. The scoring system in schemePriorityScore
	// pins schemePriorityBearer = 0 < schemePriorityOAuth2AuthCode = 200
	// precisely because Bearer is the simplest scheme for a CLI to use.
	// Default selection must keep Bearer; AuthPreference must still let
	// callers opt into OAuth2 when they want the 3LO dance.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: GitHub-like
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            read: read access
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - BearerAuth: []
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`)

	defaultParsed, err := Parse(specBytes)
	require.NoError(t, err)
	assert.Equal(t, "BearerAuth", defaultParsed.Auth.Scheme, "default selection must keep Bearer over OAuth2+AC")
	assert.Equal(t, "bearer_token", defaultParsed.Auth.Type)

	preferred, err := ParseWithOptions(specBytes, ParseOptions{AuthPreference: "OAuth2"})
	require.NoError(t, err)
	assert.Equal(t, "OAuth2", preferred.Auth.Scheme, "AuthPreference must still let callers opt into OAuth2")
	assert.Equal(t, "bearer_token", preferred.Auth.Type)
	assert.Equal(t, "https://auth.example.com/authorize", preferred.Auth.AuthorizationURL)
	assert.Equal(t, "https://auth.example.com/token", preferred.Auth.TokenURL)
}

func TestParseSecuritySchemeReferenceCountBeatsTypePriority(t *testing.T) {
	t.Parallel()

	var paths strings.Builder
	for i := range 10 {
		fmt.Fprintf(&paths, `  /v1/oauth-items/%d:
    get:
      operationId: list oauth items %d
      security:
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`, i, i)
	}
	paths.WriteString(`  /v1/events:
    post:
      operationId: create event
      security:
        - ConversionToken: []
      responses: {"200": {description: ok}}
`)

	specBytes := fmt.Appendf(nil, `openapi: "3.0.3"
info:
  title: Pinterest-like
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ConversionToken:
      type: http
      scheme: bearer
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            read: read access
paths:
%s`, paths.String())

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "OAuth2", parsed.Auth.Scheme, "scheme used by most operations must become the primary auth scheme")
	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"PINTEREST_LIKE_OAUTH2"}, parsed.Auth.EnvVars)
	assert.Equal(t, "https://auth.example.com/authorize", parsed.Auth.AuthorizationURL)
	assert.Equal(t, "https://auth.example.com/token", parsed.Auth.TokenURL)
}

func TestParseOperationSecurityOverridesRootSecurityCandidates(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Override Root
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            read: read access
paths:
  /v1/overridden:
    get:
      operationId: list overridden
      security:
        - OAuth2: [read]
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "OAuth2", parsed.Auth.Scheme, "operation-level security overrides root security and must stay selectable")
	assert.Equal(t, []string{"OVERRIDE_ROOT_OAUTH2"}, parsed.Auth.EnvVars)
}

func TestParseInheritedRootSecurityContributesToReferenceCount(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Root Majority
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
    BearerEventToken:
      type: http
      scheme: bearer
paths:
  /v1/root-a:
    get:
      operationId: list root a
      responses: {"200": {description: ok}}
  /v1/root-b:
    get:
      operationId: list root b
      responses: {"200": {description: ok}}
  /v1/events:
    post:
      operationId: create event
      security:
        - BearerEventToken: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "ApiKeyAuth", parsed.Auth.Scheme, "root security inherited by most operations must count toward primary auth selection")
	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, []string{"ROOT_MAJORITY_API_KEY"}, parsed.Auth.EnvVars)
}

func TestBearerSchemeNameCanSpecializeEnvVar(t *testing.T) {
	t.Parallel()

	spec := []byte(`openapi: "3.0.3"
info:
  title: Sentry
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    auth_token:
      type: http
      scheme: bearer
paths:
  /api/0/organizations/:
    get:
      operationId: List Your Organizations
      security:
        - auth_token: []
      responses:
        "200":
          description: ok
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"SENTRY_AUTH_TOKEN"}, parsed.Auth.EnvVars)
}

func TestSelectSecuritySchemeMultiSchemePrefersBearer(t *testing.T) {
	t.Parallel()

	// Cloudflare-shape: a spec advertises a modern http+bearer scheme
	// alongside a legacy email + apiKey pair. The parser must pick the
	// bearer scheme so the generated CLI reads CF_API_TOKEN and sets
	// Authorization: Bearer, not X-Auth-Email.
	spec := []byte(`openapi: "3.0.3"
info:
  title: Cloudflare-shape
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - api_email: []
    api_key: []
  - api_token: []
components:
  securitySchemes:
    api_email:
      type: apiKey
      in: header
      name: X-Auth-Email
    api_key:
      type: apiKey
      in: header
      name: X-Auth-Key
    api_token:
      type: http
      scheme: bearer
paths:
  /v1/zones:
    get:
      operationId: listZones
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "bearer outranks apiKey-header")
	assert.Equal(t, "Authorization", parsed.Auth.Header)
	assert.Equal(t, "api_token", parsed.Auth.Scheme, "selected scheme name must surface for downstream env-var derivation")
}

func TestSelectSecuritySchemeRespectsRootSecurityFilter(t *testing.T) {
	t.Parallel()

	// Datadog-shape: components declares an OAuth2 marketplace scheme
	// alongside the primary apiKey scheme, but root-level security only
	// references the apiKey. The parser must restrict candidates to the
	// root-declared schemes so the marketplace OAuth2 does not win pass 1.
	spec := []byte(`openapi: "3.0.3"
info:
  title: Datadog-shape
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - apiKeyAuth: []
components:
  securitySchemes:
    AuthZ:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://api.example.com/oauth2/authorize
          tokenUrl: https://api.example.com/oauth2/token
          scopes:
            read: read access
    apiKeyAuth:
      type: apiKey
      in: header
      name: DD-API-KEY
paths:
  /v1/series:
    get:
      operationId: queryMetrics
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type, "components-only OAuth2 must not outrank root-declared apiKey")
	assert.Equal(t, "DD-API-KEY", parsed.Auth.Header)
	assert.Equal(t, "apiKeyAuth", parsed.Auth.Scheme)
}

func TestSelectSecuritySchemeFallsBackToComponentsWhenRootSecurityEmpty(t *testing.T) {
	t.Parallel()

	// A spec with `security: []` (or only `{}` no-auth requirements at
	// root) does not constrain candidates — fall back to all schemes in
	// components, sorted alphabetically, and apply the type priority.
	spec := []byte(`openapi: "3.0.3"
info:
  title: NoRootSecurity
  version: "1.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    LegacyAPIKey:
      type: apiKey
      in: header
      name: X-Legacy-Key
    ModernBearer:
      type: http
      scheme: bearer
paths:
  /v1/items:
    get:
      operationId: listItems
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "type priority still applies when root security is absent")
	assert.Equal(t, "ModernBearer", parsed.Auth.Scheme)
}

func TestSelectSecuritySchemeSingleSchemeUnchanged(t *testing.T) {
	t.Parallel()

	// Negative case from the issue acceptance criteria: single-scheme specs
	// must continue to emit identical output.
	specTemplate := `openapi: "3.0.3"
info: {title: X, version: "1.0"}
servers: [{url: https://api.example.com}]
security: [{Only: []}]
components:
  securitySchemes:
    Only:
%s
paths:
  /v1/x: {get: {operationId: getX, responses: {"200": {description: ok}}}}
`

	cases := []struct {
		name     string
		scheme   string
		wantType string
		wantHdr  string
	}{
		{
			name:     "apiKey only",
			scheme:   "      type: apiKey\n      in: header\n      name: X-API-Key",
			wantType: "api_key",
			wantHdr:  "X-API-Key",
		},
		{
			name:     "bearer only",
			scheme:   "      type: http\n      scheme: bearer",
			wantType: "bearer_token",
			wantHdr:  "Authorization",
		},
		{
			name:     "basic only",
			scheme:   "      type: http\n      scheme: basic",
			wantType: "api_key",
			wantHdr:  "Authorization",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := Parse(fmt.Appendf(nil, specTemplate, tc.scheme))
			require.NoError(t, err)
			assert.Equal(t, tc.wantType, parsed.Auth.Type)
			assert.Equal(t, tc.wantHdr, parsed.Auth.Header)
		})
	}
}

func TestSelectSecuritySchemeOAuth2PasswordBeatsAPIKey(t *testing.T) {
	t.Parallel()

	// OAuth2 Password (ROPC) is deprecated but real specs still use it for
	// server-to-server flows alongside an apiKey alternative. The old 3-pass
	// selector returned any well-formed oauth2 before any apiKey; the new
	// priority-score selector must preserve that ordering or such specs
	// regress silently to the apiKey scheme. See PR #1238 review feedback.
	spec := []byte(`openapi: "3.0.3"
info:
  title: ROPC-shape
  version: "1.0"
servers:
  - url: https://api.example.com
security:
  - ropcAuth: []
  - apiKeyAuth: []
components:
  securitySchemes:
    ropcAuth:
      type: oauth2
      flows:
        password:
          tokenUrl: https://api.example.com/oauth/token
          scopes:
            read: read access
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /v1/items:
    get:
      operationId: listItems
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "well-formed ROPC oauth2 must outrank co-declared apiKey")
	assert.Equal(t, "ropcAuth", parsed.Auth.Scheme)
}

func TestSkipUnderscoreFields(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
components:
  schemas:
    Item:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        _errors:
          type: object
        _internal:
          type: string
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)

	item, ok := parsed.Types["Item"]
	require.True(t, ok)

	// Should have id and name but NOT _errors or _internal
	fieldNames := make([]string, 0)
	for _, f := range item.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	assert.Contains(t, fieldNames, "id")
	assert.Contains(t, fieldNames, "name")
	assert.NotContains(t, fieldNames, "_errors")
	assert.NotContains(t, fieldNames, "_internal")
}

func TestParseReadsXDisplayName(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Cal.com API v2
  x-display-name: "Cal.com"
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)
	assert.Equal(t, "Cal.com", parsed.DisplayName)
	assert.False(t, parsed.DisplayNameDerivedFromTitle)
}

func TestParseTrimsWhitespaceFromXDisplayName(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Test API
  x-display-name: "  Brand Name  "
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)
	assert.Equal(t, "Brand Name", parsed.DisplayName)
	assert.False(t, parsed.DisplayNameDerivedFromTitle)
}

// TestParseDerivesDisplayNameFromTitle locks the dual contract when no
// x-display-name extension is set: slug is ASCII-folded for filesystem and
// shell safety, display_name keeps Unicode for the human-facing surfaces
// (manifest.json, .printing-press.json, MCP server identity).
func TestParseDerivesDisplayNameFromTitle(t *testing.T) {
	cases := []struct {
		name        string
		title       string
		wantSlug    string
		wantDisplay string
	}{
		{name: "ascii", title: "Test API", wantSlug: "test", wantDisplay: "Test"},
		{name: "precomposed_accent", title: "Café Bistro API", wantSlug: "cafe-bistro", wantDisplay: "Café Bistro"},
		{name: "fused_diacritics", title: "Strüdel Service API", wantSlug: "strudel-service", wantDisplay: "Strüdel Service"},
		{name: "non_latin_script", title: "東京 API", wantSlug: "dong-jing", wantDisplay: "東京"},
		{name: "single_token_accent", title: "PokéAPI", wantSlug: "pokeapi", wantDisplay: "Pokéapi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := fmt.Appendf(nil, `
openapi: "3.0.0"
info:
  title: %s
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
`, tc.title)
			parsed, err := Parse(spec)
			require.NoError(t, err)
			assert.Equal(t, tc.wantSlug, parsed.Name)
			assert.Equal(t, tc.wantDisplay, parsed.DisplayName)
			assert.True(t, parsed.DisplayNameDerivedFromTitle)
		})
	}
}

func TestIsOpenAPI(t *testing.T) {
	t.Parallel()

	openAPIYAML := []byte(`
openapi: 3.0.0
info:
  title: Demo
  version: 1.0.0
paths: {}
`)
	openAPIJSON := []byte(`{"openapi":"3.0.1","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`)
	swagger20YAML := []byte(`swagger: "2.0"
info:
  title: Demo
  version: 1.0.0
paths: {}
`)
	swagger20JSON := []byte(`{"swagger":"2.0","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`)
	internalYAML := []byte(`
name: demo
base_url: https://api.example.com
resources:
  users:
    endpoints:
      list:
        method: GET
        path: /users
`)

	assert.True(t, IsOpenAPI(openAPIYAML))
	assert.True(t, IsOpenAPI(openAPIJSON))
	assert.True(t, IsOpenAPI(swagger20YAML))
	assert.True(t, IsOpenAPI(swagger20JSON))
	assert.False(t, IsOpenAPI(internalYAML))
}

func TestGenerateFromOpenAPICompiles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("OpenAPI generated CLI compile coverage runs in the generated-test CI lane")
	}

	tests := []struct {
		name     string
		specFile string
	}{
		{name: "petstore", specFile: "petstore.yaml"},
		{name: "stytch", specFile: "stytch.yaml"},
	}

	for _, tt := range tests {
		tt := tt //nolint:modernize // keep the parallel subtest capture explicit
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", tt.specFile))
			require.NoError(t, err)

			parsed, err := Parse(data)
			require.NoError(t, err)

			outputDir := filepath.Join(t.TempDir(), naming.CLI(parsed.Name))
			gen := generator.New(parsed, outputDir)
			require.NoError(t, gen.Generate())

			runGo(t, outputDir, "mod", "tidy")
			runGo(t, outputDir, "build", "./...")
		})
	}
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func extractDryRunBody(t *testing.T, output string) string {
	t.Helper()

	const bodyMarker = "  Body:\n"
	start := strings.Index(output, bodyMarker)
	require.NotEqual(t, -1, start, output)
	start += len(bodyMarker)

	end := strings.Index(output[start:], "\n\n(dry run")
	require.NotEqual(t, -1, end, output)
	return output[start : start+end]
}

func TestSanitizeResourceName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", "users"},
		{"user-accounts", "user_accounts"},
		{"../../../etc/passwd", "etc_passwd"},
		{"foo/bar", "foo_bar"},
		{"foo\\bar", "foo_bar"},
		{"..", ""},
		{".", ""},
		{"___", ""},
		{"normal_name", "normal_name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeResourceName(toSnakeCase(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPathSegmentsStripsGenericAPIPrefix(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		basePath  string
		wantFirst string
	}{
		{"strips api prefix", "/v1/api/users", "", "users"},
		{"strips apis prefix", "/v2/apis/teams", "", "teams"},
		{"strips rest prefix", "/rest/orders", "", "orders"},
		{"keeps non-generic prefix", "/v1/billing/invoices", "", "billing"},
		{"keeps api when no sub-segments", "/api", "", "api"},
		{"keeps api when followed by path param", "/api/{id}", "", "api"},
		{"keeps rest when followed by path param", "/rest/{job_id}/runs", "", "rest"},
		{"strips version then api", "/v1/api/networkentity", "", "networkentity"},
		{"strips api then version", "/api/v2/pokemon", "", "pokemon"},
		{"strips version then api then version", "/v2/api/v1/pokemon", "", "pokemon"},
		{"strips api then numeric version", "/api/0/organizations", "", "organizations"},
		{"strips dotted numeric version", "/1.0/search/artists", "", "search"},
		{"strips dotted v-prefixed version", "/v1.0/search/artists", "", "search"},
		{"strips beta version", "/v1beta2/{parent}/repositories", "", "{parent}"},
		{"strips alpha version", "/v1alpha1/{parent}/services", "", "{parent}"},
		{"strips p beta version", "/v1p1beta1/{parent}/sessions", "", "{parent}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments := pathSegmentsAfterBase(tt.path, tt.basePath)
			require.NotEmpty(t, segments)
			assert.Equal(t, tt.wantFirst, segments[0])
		})
	}
}

func TestOperationIDToName(t *testing.T) {
	tests := []struct {
		operationID  string
		resourceName string
		want         string
	}{
		{operationID: "api_user_v1_create", resourceName: "users", want: "create"},
		{operationID: "api_user_v1_delete_biometric_registration", resourceName: "users", want: "delete-biometric-registration"},
		{operationID: "api_user_v1_connected_apps", resourceName: "users", want: "connected-apps"},
		{operationID: "api_user_v1_get", resourceName: "users", want: "get"},
		{operationID: "api_user_v1_search", resourceName: "users", want: "search"},
		{operationID: "listPets", resourceName: "pet", want: "list"},
		{operationID: "createPet", resourceName: "pet", want: "create"},
		{operationID: "getPetById", resourceName: "pet", want: "get-by-id"},
		{operationID: "addPet", resourceName: "pet", want: "add"},
		{operationID: "deletePet", resourceName: "pet", want: "delete"},
		{operationID: "findPetsByStatus", resourceName: "pet", want: "find-by-status"},
		{operationID: "findPetsByTags", resourceName: "pet", want: "find-by-tags"},
		{operationID: "getInventory", resourceName: "store", want: "get-inventory"},
		{operationID: "placeOrder", resourceName: "store", want: "place-order"},
		{operationID: "createUser", resourceName: "user", want: "create"},
		{operationID: "loginUser", resourceName: "user", want: "login"},
		{operationID: "GetApplicationCommandPermissions", resourceName: "applications", want: "get-command-permissions"},
		{operationID: "", resourceName: "users", want: ""},
		{operationID: "list", resourceName: "users", want: "list"},
		// Cal.com-style: controller class names + embedded version dates
		{operationID: "BookingsController_2024-08-13_getBooking", resourceName: "bookings", want: "get"},
		{operationID: "BookingsController_2024-08-13_createBooking", resourceName: "bookings", want: "create"},
		{operationID: "EventTypesController_2024-06-14_getEventTypes", resourceName: "event-types", want: "get"},
		// Controller suffix without date
		{operationID: "OrganizationsController_getOrg", resourceName: "organizations", want: "get-org"},
		// No controller/version pattern — should be unchanged
		{operationID: "getBookingByUid", resourceName: "bookings", want: "get-by-uid"},
		{operationID: "run.projects.locations.services.revisions.delete", resourceName: "revisions", want: "delete"},
		{operationID: "run.projects.locations.services.getIamPolicy", resourceName: "services", want: "get-iam-policy"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID+"_"+tt.resourceName, func(t *testing.T) {
			got := operationIDToName(tt.operationID, tt.resourceName, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceAndSubFromGoogleOperationID(t *testing.T) {
	tests := []struct {
		name         string
		operationID  string
		wantPrimary  string
		wantSub      string
		wantName     string
		nameResource string
	}{
		{
			name:         "project location resource",
			operationID:  "run.projects.locations.services.create",
			wantPrimary:  "services",
			nameResource: "services",
			wantName:     "create",
		},
		{
			name:         "nested subresource keeps owning parent",
			operationID:  "run.projects.locations.services.revisions.delete",
			wantPrimary:  "services",
			wantSub:      "revisions",
			nameResource: "revisions",
			wantName:     "delete",
		},
		{
			name:         "deep chain uses immediate parent",
			operationID:  "run.projects.locations.jobs.executions.tasks.list",
			wantPrimary:  "executions",
			wantSub:      "tasks",
			nameResource: "tasks",
			wantName:     "list",
		},
		{
			name:         "organization location scope",
			operationID:  "example.organizations.locations.widgets.get",
			wantPrimary:  "widgets",
			nameResource: "widgets",
			wantName:     "get",
		},
		{
			name:         "billing account scope",
			operationID:  "example.billingAccounts.invoices.list",
			wantPrimary:  "invoices",
			nameResource: "invoices",
			wantName:     "list",
		},
		{
			name:        "non Google scope ignored",
			operationID: "gmail.users.messages.list",
		},
		{
			name:         "unscoped Google Discovery operation",
			operationID:  "bigquery.tables.getIamPolicy",
			wantPrimary:  "tables",
			nameResource: "tables",
			wantName:     "get-iam-policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrimary, gotSub := resourceAndSubFromGoogleOperationID(tt.operationID, false)
			if tt.name == "unscoped Google Discovery operation" {
				gotPrimary, gotSub = resourceAndSubFromGoogleOperationID(tt.operationID, true)
			}
			assert.Equal(t, tt.wantPrimary, gotPrimary)
			assert.Equal(t, tt.wantSub, gotSub)
			assert.Equal(t, tt.wantName, googleOperationIDEndpointName(tt.operationID, tt.nameResource))
		})
	}
}

func TestParseGoogleDiscoveryResourceFallback(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "google-discovery-run.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.NotContains(t, parsed.Resources, "name-run")
	assert.NotContains(t, parsed.Resources, "name-wait")
	assert.NotContains(t, parsed.Resources, "resource-get-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-set-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-test-iam-permissions")

	services := parsed.Resources["services"]
	require.Contains(t, services.Endpoints, "create")
	require.Contains(t, services.Endpoints, "patch")
	require.Contains(t, services.Endpoints, "list")
	require.Contains(t, services.Endpoints, "get-iam-policy")
	require.Contains(t, services.Endpoints, "set-iam-policy")
	require.Contains(t, services.Endpoints, "test-iam-permissions")
	require.Contains(t, services.SubResources, "revisions")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "delete")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "get")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "list")

	assert.NotContains(t, parsed.Resources, "jobs")
	jobs := parsed.Resources["google-cloud-run-jobs"]
	require.Contains(t, jobs.Endpoints, "create")
	require.Contains(t, jobs.Endpoints, "list")
	require.Contains(t, jobs.Endpoints, "run")
	require.Contains(t, jobs.SubResources, "executions")
	assert.Contains(t, jobs.SubResources["executions"].Endpoints, "list")

	executions := parsed.Resources["executions"]
	require.Contains(t, executions.SubResources, "tasks")
	assert.Contains(t, executions.SubResources["tasks"].Endpoints, "list")

	operations := parsed.Resources["operations"]
	assert.Contains(t, operations.Endpoints, "list")
	assert.Contains(t, operations.Endpoints, "wait")

	locations := parsed.Resources["locations"]
	assert.Contains(t, locations.Endpoints, "list")
}

func TestParseGoogleDiscoveryUnscopedOperationIDFallbackRequiresGoogleOrigin(t *testing.T) {
	t.Parallel()

	base := `openapi: 3.0.3
info:
  title: BigQuery API
  version: v2
%s
paths:
  /{resource}:getIamPolicy:
    get:
      operationId: bigquery.tables.getIamPolicy
      parameters:
        - name: resource
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`

	parsed, err := Parse(fmt.Appendf(nil, base, "  x-providerName: googleapis.com\n"))
	require.NoError(t, err)
	require.Contains(t, parsed.Resources, "tables")
	assert.Contains(t, parsed.Resources["tables"].Endpoints, "get-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-get-iam-policy")

	parsed, err = Parse(fmt.Appendf(nil, base, ""))
	require.NoError(t, err)
	assert.NotContains(t, parsed.Resources, "tables")
	assert.Contains(t, parsed.Resources, "resource-get-iam-policy")
}

func TestReclassifyPathParamModifiers(t *testing.T) {
	tests := []struct {
		name           string
		params         []spec.Param
		wantPositional []string // names that should stay positional
		wantFlags      []string // names that should become flags
	}{
		{
			name: "pagination params become flags",
			params: []spec.Param{
				{Name: "page", Type: "int", Positional: true},
				{Name: "pageSize", Type: "int", Positional: true},
			},
			wantPositional: nil,
			wantFlags:      []string{"page", "pageSize"},
		},
		{
			name: "entity ID stays positional",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      nil,
		},
		{
			name: "mixed: storeId positional, page/pageSize flags",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
				{Name: "page", Type: "int", Positional: true},
				{Name: "pageSize", Type: "int", Positional: true},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      []string{"page", "pageSize"},
		},
		{
			name: "enum param becomes flag",
			params: []spec.Param{
				{Name: "serviceType", Type: "string", Positional: true, Enum: []string{"PICK", "DEL"}},
			},
			wantPositional: nil,
			wantFlags:      []string{"serviceType"},
		},
		{
			name: "date param becomes flag",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
				{Name: "date", Type: "string", Positional: true, Format: "date"},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      []string{"date"},
		},
		{
			name: "param with default becomes flag",
			params: []spec.Param{
				{Name: "version", Type: "string", Positional: true, Default: "v2"},
			},
			wantPositional: nil,
			wantFlags:      []string{"version"},
		},
		{
			name: "non-positional params unchanged",
			params: []spec.Param{
				{Name: "lang", Type: "string", Positional: false},
			},
			wantPositional: nil,
			wantFlags:      nil, // already a flag, not reclassified
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reclassifyPathParamModifiers(tt.params)

			var gotPositional, gotFlags []string
			for _, p := range tt.params {
				if p.Positional {
					gotPositional = append(gotPositional, p.Name)
				} else if p.PathParam {
					gotFlags = append(gotFlags, p.Name)
				}
			}
			assert.Equal(t, tt.wantPositional, gotPositional, "positional params")
			assert.Equal(t, tt.wantFlags, gotFlags, "reclassified flag params")
		})
	}
}

func TestReclassifyPathParamDefaults(t *testing.T) {
	params := []spec.Param{
		{Name: "page", Type: "int", Positional: true},
		{Name: "pageSize", Type: "int", Positional: true},
		{Name: "serviceType", Type: "string", Positional: true, Enum: []string{"PICK", "DEL"}},
	}
	reclassifyPathParamModifiers(params)

	assert.Equal(t, 1, params[0].Default, "page default should be 1")
	assert.Equal(t, 10, params[1].Default, "pageSize default should be 10")
	assert.Equal(t, "PICK", params[2].Default, "enum default should be first value")
}

func TestParsePreservesDefaultedPathParamsDuringGlobalFilter(t *testing.T) {
	data := []byte(`
openapi: 3.0.0
info:
  title: GraphQL Routing API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /graphql/{pathQueryId}/Followers:
    get:
      operationId: getFollowers
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: followers123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /graphql/{pathQueryId}/Following:
    get:
      operationId: getFollowing
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: following123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /graphql/{pathQueryId}/Likes:
    get:
      operationId: getLikes
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: likes123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	for _, path := range []string{
		"/graphql/{pathQueryId}/Followers",
		"/graphql/{pathQueryId}/Following",
		"/graphql/{pathQueryId}/Likes",
	} {
		endpoint := findEndpoint(t, parsed, path)
		var routingParam *spec.Param
		for i := range endpoint.Params {
			if endpoint.Params[i].Name == "pathQueryId" {
				routingParam = &endpoint.Params[i]
				break
			}
		}
		if assert.NotNil(t, routingParam, "pathQueryId should survive global-param filtering") {
			assert.True(t, routingParam.PathParam, "defaulted path param should remain a URL substitution flag")
			assert.False(t, routingParam.Positional, "defaulted path param should stay flag-shaped")
			assert.NotNil(t, routingParam.Default, "operation-specific query id default should be preserved")
		}
	}
}

func TestParsePreservesRequiredQueryParamsDuringGlobalFilter(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: 3.0.0
info:
  title: Narrow Required Query API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /search:
    get:
      operationId: searchList
      parameters:
        - in: query
          name: part
          required: true
          schema:
            type: string
            default: snippet
            enum: [snippet, id]
        - in: query
          name: prettyPrint
          schema:
            type: boolean
        - in: query
          name: q
          schema:
            type: string
      responses:
        "200":
          description: ok
  /videos:
    get:
      operationId: videosList
      parameters:
        - in: query
          name: part
          required: true
          schema:
            type: string
            default: snippet
            enum: [snippet, statistics]
        - in: query
          name: prettyPrint
          schema:
            type: boolean
        - in: query
          name: id
          schema:
            type: string
      responses:
        "200":
          description: ok
  /channels:
    get:
      operationId: channelsList
      parameters:
        - in: query
          name: part
          required: true
          schema:
            type: string
            default: snippet
            enum: [snippet, contentDetails]
        - in: query
          name: prettyPrint
          schema:
            type: boolean
        - in: query
          name: id
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	wantEnums := map[string][]string{
		"/search":   {"snippet", "id"},
		"/videos":   {"snippet", "statistics"},
		"/channels": {"snippet", "contentDetails"},
	}
	for _, path := range []string{"/search", "/videos", "/channels"} {
		endpoint := findEndpoint(t, parsed, path)
		var part *spec.Param
		for i := range endpoint.Params {
			switch endpoint.Params[i].Name {
			case "part":
				part = &endpoint.Params[i]
			case "prettyPrint":
				t.Fatalf("%s should filter optional prevalent query param prettyPrint", path)
			}
		}
		if assert.NotNil(t, part, "%s should preserve required prevalent query param part", path) {
			assert.True(t, part.Required)
			assert.Equal(t, "snippet", part.Default)
			assert.Equal(t, wantEnums[path], part.Enum)
		}
	}
}

// Real-world OpenAPI specs (Tally, others) frequently include {placeholder}
// tokens in a path template without declaring the corresponding parameter at
// either the operation or path-item level. The path template is then the only
// source of truth for what's required. Without synthesizing a Param entry,
// the generated CLI emits a literal `{organizationId}` URL segment and every
// request 404s. Mirrors the same enrichment the internal YAML loader applies.
func TestParseSynthesizesUndeclaredPathPlaceholders(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.0
info:
  title: Hierarchical API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /organizations/{organizationId}/invites:
    get:
      summary: List organization invites
      responses:
        "200":
          description: ok
  /organizations/{organizationId}/users:
    get:
      summary: List organization users
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	for _, path := range []string{
		"/organizations/{organizationId}/invites",
		"/organizations/{organizationId}/users",
	} {
		endpoint := findEndpoint(t, parsed, path)
		var orgID *spec.Param
		for i := range endpoint.Params {
			if endpoint.Params[i].Name == "organizationId" {
				orgID = &endpoint.Params[i]
				break
			}
		}
		if assert.NotNilf(t, orgID, "path %q must surface an organizationId param even when operation declared none", path) {
			assert.True(t, orgID.Positional, "synthesized path placeholders must be positional")
			assert.True(t, orgID.Required, "synthesized path placeholders must be required")
		}
	}
}

func TestCleanSpecName(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "Swagger Petstore - OpenAPI 3.0", want: "petstore"},
		{title: "Discord HTTP API (Preview)", want: "discord"},
		{title: "Stytch API", want: "stytch"},
		{title: "GitHub REST API", want: "github"},
		{title: "", want: "api"},
		// Apostrophes in brand names should be stripped, not hyphenated
		{title: "Domino's Pizza API", want: "dominos-pizza"},
		{title: "McDonald's API", want: "mcdonalds"},
		{title: "Lowe's Home Improvement", want: "lowes-home-improvement"},
		// Unicode right single quotation mark
		{title: "Domino\u2019s Pizza API", want: "dominos-pizza"},
		// Multiple apostrophes
		{title: "Rock'n'Roll API", want: "rocknroll"},
		// Precomposed accents:
		{title: "Pok\u00e9mon API", want: "pokemon"},
		{title: "Caf\u00e9 Reservations", want: "cafe-reservations"},
		{title: "Na\u00efve Bayes API", want: "naive-bayes"},
		// Fused-diacritic Latin:
		{title: "Gro\u00dfhandel API", want: "grosshandel"},
		{title: "Encyclop\u00e6dia API", want: "encyclopaedia"},
		{title: "\u00d8rsted Energy", want: "orsted-energy"},
		{title: "\u0141\u00f3d\u017a Transit", want: "lodz-transit"},
		{title: "\u00deingvellir Tours", want: "thingvellir-tours"},
		// Non-Latin scripts:
		{title: "\u6771\u4eac API", want: "dong-jing"},
		{title: "\u0440\u0443\u0441\u0441\u043a\u0438\u0439 API", want: "russkii"},
		{title: "\u0394elta API", want: "delta"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanSpecName(tt.title))
		})
	}
}

func TestHumanizeDescription(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "Connectedapps", want: "Connected apps"},
		{input: "DeleteBiometricRegistration", want: "Delete biometric registration"},
		{input: "Already normal text", want: "Already normal text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, humanizeDescription(tt.input))
		})
	}
}

func TestDetectRequiredHeaders(t *testing.T) {
	t.Parallel()

	t.Run("versioned API with required header on all operations", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "versioned-api.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		require.Len(t, parsed.RequiredHeaders, 1)
		assert.Equal(t, "X-Api-Version", parsed.RequiredHeaders[0].Name)
		assert.Equal(t, "2024-01-01", parsed.RequiredHeaders[0].Value)
	})

	t.Run("petstore has no required headers", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Empty(t, parsed.RequiredHeaders)
	})

	t.Run("stytch has no required headers (optional session headers)", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Empty(t, parsed.RequiredHeaders)
	})

	t.Run("multi-version header tracks per-endpoint overrides", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "multi-version-header.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		// Global header should be the majority value (2024-08-13 appears on 3 of 6 ops)
		require.Len(t, parsed.RequiredHeaders, 1)
		assert.Equal(t, "cal-api-version", parsed.RequiredHeaders[0].Name)
		assert.Equal(t, "2024-08-13", parsed.RequiredHeaders[0].Value)

		// Event-types endpoints should have header overrides with 2024-06-14
		eventTypes := parsed.Resources["event-types"]
		require.NotNil(t, eventTypes)
		for eName, ep := range eventTypes.Endpoints {
			require.NotEmpty(t, ep.HeaderOverrides, "event-types endpoint %q should have header overrides", eName)
			assert.Equal(t, "cal-api-version", ep.HeaderOverrides[0].Name)
			assert.Equal(t, "2024-06-14", ep.HeaderOverrides[0].Value)
		}

		// Bookings endpoints should NOT have overrides (they match the global default)
		bookings := parsed.Resources["bookings"]
		require.NotNil(t, bookings)
		for eName, ep := range bookings.Endpoints {
			assert.Empty(t, ep.HeaderOverrides, "bookings endpoint %q should not have overrides (matches global)", eName)
		}
	})

	t.Run("authorization header excluded even if required on all ops", func(t *testing.T) {
		headers, perEndpoint := detectRequiredHeaders(nil, spec.AuthConfig{})
		assert.Empty(t, headers)
		assert.Empty(t, perEndpoint)
	})
}

func TestInferDescriptionAuth(t *testing.T) {
	t.Parallel()

	t.Run("bearer in description, no securitySchemes", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "bearer-in-description.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, "header", parsed.Auth.In)
		assert.True(t, parsed.Auth.Inferred)
		assert.NotEmpty(t, parsed.Auth.EnvVars)
		assert.Contains(t, parsed.Auth.EnvVars[0], "_TOKEN")
	})

	t.Run("GitHub-style token prose infers bearer auth", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "prose-bearer-auth.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, []string{"GITHUB_TOKEN"}, parsed.Auth.EnvVars)
		assert.True(t, parsed.Auth.Inferred)
	})

	t.Run("specific bearer prose signals infer bearer auth independently", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
		}{
			{name: "personal access token", description: "Authenticate with a personal access token."},
			{name: "fine-grained PAT", description: "Authenticate with a fine-grained PAT."},
			{name: "app installation token", description: "Authenticate with an app installation token."},
			{name: "OAuth app token", description: "Authenticate with an OAuth app token."},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := inferDescriptionAuth(&openapi3.T{
					Info: &openapi3.Info{Description: tt.description},
				}, "github", spec.AuthConfig{Type: "none"})

				assert.Equal(t, "bearer_token", result.Type)
				assert.Equal(t, []string{"GITHUB_TOKEN"}, result.EnvVars)
				assert.True(t, result.Inferred)
			})
		}
	})

	t.Run("explicit empty securitySchemes opts out of prose inference", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: GitHub
  version: "1.0.0"
  description: "Authenticate requests with Authorization: Bearer TOKEN."
components:
  securitySchemes: {}
  schemas:
    Repository:
      type: object
paths:
  /repos:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "none", parsed.Auth.Type)
		assert.False(t, parsed.Auth.Inferred)
	})

	t.Run("explicit empty securitySchemes keeps query-param inference", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
components:
  securitySchemes: {}
paths:
  /a:
    get:
      parameters:
        - name: api_key
          in: query
          schema:
            type: string
      responses:
        "200":
          description: OK
  /b:
    get:
      parameters:
        - name: api_key
          in: query
          schema:
            type: string
      responses:
        "200":
          description: OK
  /c:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "api_key", parsed.Auth.Type)
		assert.Equal(t, "query", parsed.Auth.In)
		assert.Equal(t, []string{"EXAMPLE_API_KEY"}, parsed.Auth.EnvVars)
	})

	t.Run("explicit empty securitySchemes keeps operation-level bearer inference", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
components:
  securitySchemes: {}
paths:
  /a:
    get:
      parameters:
        - name: Authorization
          in: header
          required: true
          description: Bearer token credential.
          schema:
            type: string
      responses:
        "200":
          description: OK
  /b:
    get:
      parameters:
        - name: Authorization
          in: header
          required: true
          description: Bearer token credential.
          schema:
            type: string
      responses:
        "200":
          description: OK
  /c:
    get:
      parameters:
        - name: Authorization
          in: header
          required: true
          description: Bearer token credential.
          schema:
            type: string
      responses:
        "200":
          description: OK
  /d:
    get:
      parameters:
        - name: Authorization
          in: header
          required: true
          description: Bearer token credential.
          schema:
            type: string
      responses:
        "200":
          description: OK
  /e:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, []string{"EXAMPLE_TOKEN"}, parsed.Auth.EnvVars)
		assert.True(t, parsed.Auth.Inferred)
	})

	t.Run("components without securitySchemes still allows prose inference", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: GitHub
  version: "1.0.0"
  description: "Authenticate requests with Authorization: Bearer TOKEN."
components:
  schemas:
    Repository:
      type: object
paths:
  /repos:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, []string{"GITHUB_TOKEN"}, parsed.Auth.EnvVars)
		assert.True(t, parsed.Auth.Inferred)
	})

	t.Run("petstore has explicit auth, not inferred", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.False(t, parsed.Auth.Inferred)
		assert.NotEqual(t, "none", parsed.Auth.Type)
	})

	t.Run("stytch has explicit auth, not inferred", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.False(t, parsed.Auth.Inferred)
	})

	t.Run("no auth keywords in description stays none", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "A simple API for managing widgets and gadgets.",
			},
		}
		result := inferDescriptionAuth(doc, "widgets", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type)
		assert.False(t, result.Inferred)
	})

	t.Run("negation suppresses inference", func(t *testing.T) {
		result := inferDescriptionAuth(nil, "test", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type)

		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "This API does not require Bearer authentication",
			},
		}
		result = inferDescriptionAuth(doc, "test", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type, "negated 'Bearer' should not trigger inference")
		assert.False(t, result.Inferred)
	})

	t.Run("api_key keyword produces api_key type", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Authenticate with your API key in the Authorization header",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "EXAMPLE_API_KEY", result.EnvVars[0])
		assert.True(t, result.Inferred)
	})

	t.Run("scans past negated match to find positive mention", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Sandbox requests do not require a bearer token, but production requests use a bearer token for authentication.",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "bearer_token", result.Type, "should find the second non-negated 'bearer' mention")
		assert.True(t, result.Inferred)
	})

	t.Run("Notion bearer token not falsely negated", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Use your Notion bearer token to authenticate",
			},
		}
		result := inferDescriptionAuth(doc, "notion", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "bearer_token", result.Type, "'Notion' contains 'no' but should not trigger negation")
		assert.True(t, result.Inferred)
	})

	t.Run("custom header X-Api-Key extracted from description", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Send your API key in the X-Api-Key header",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "X-Api-Key", result.Header, "should extract X-Api-Key, not default to Authorization")
		assert.True(t, result.Inferred)
	})

	t.Run("nil doc returns fallback", func(t *testing.T) {
		fb := spec.AuthConfig{Type: "none"}
		assert.Equal(t, fb, inferDescriptionAuth(nil, "test", fb))
	})
}

func TestInferredAuthEnvVarsAreASCIISafe(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: PokéAPI
  version: "1.0.0"
  description: Authenticate with your API key in the Authorization header.
servers:
  - url: https://api.example.com
paths:
  /pokemon:
    get:
      summary: List pokemon
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	require.NotEmpty(t, parsed.Auth.EnvVars)
	assert.Equal(t, "POKEAPI_API_KEY", parsed.Auth.EnvVars[0])
}

func TestGenericAPIKeySchemeNamesUseAPIKeyEnvVar(t *testing.T) {
	t.Parallel()

	tests := []string{
		"ApiKeyAuth",
		"APIKey",
		"ApiKeyAuth_v2",
		"auth",
	}

	for _, schemeName := range tests {
		t.Run(schemeName, func(t *testing.T) {
			t.Parallel()

			yamlSpec := fmt.Appendf(nil, `openapi: "3.0.3"
info:
  title: FlightGoat
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    %s:
      type: apiKey
      in: header
      name: x-apikey
paths:
  /flights:
    get:
      responses:
        "200":
          description: OK
`, schemeName)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			assert.Equal(t, "api_key", parsed.Auth.Type)
			assert.Equal(t, []string{"FLIGHTGOAT_API_KEY"}, parsed.Auth.EnvVars)
		})
	}
}

func TestSpeakeasyAuthExampleOverridesDerivedEnvVar(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Dub API
  version: "1.0.0"
servers:
  - url: https://api.dub.co
components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
      x-speakeasy-example: DUB_API_KEY
paths:
  /links:
    get:
      security:
        - token: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"DUB_API_KEY"}, parsed.Auth.EnvVars)
}

func TestSpeakeasyAuthExampleRemapsInferredFormatPlaceholder(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Discord
  version: "1.0.0"
servers:
  - url: https://discord.com/api
components:
  securitySchemes:
    BotToken:
      type: apiKey
      in: header
      name: Authorization
      x-speakeasy-example: DISCORD_TOKEN
paths:
  /users/@me:
    get:
      security:
        - BotToken: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"DISCORD_TOKEN"}, parsed.Auth.EnvVars)
	assert.Equal(t, "Bot {token}", parsed.Auth.Format)
}

func TestSpeakeasyAuthExampleDoesNotOverrideExplicitEnvVars(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: OAuth Client
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ClientCredentials:
      type: oauth2
      x-auth-env-vars:
        - OAUTH_CLIENT_ID
        - OAUTH_CLIENT_SECRET
      x-speakeasy-example: OAUTH_TOKEN
      flows:
        clientCredentials:
          tokenUrl: https://api.example.com/oauth/token
          scopes: {}
paths:
  /widgets:
    get:
      security:
        - ClientCredentials: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET"}, parsed.Auth.EnvVars)
}

func TestOpenAPIOAuthRefreshTokenMechanism(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ext  string
		want string
	}{
		{name: "absent leaves field empty", ext: "", want: ""},
		{name: "scope:offline (WHOOP shape)", ext: "scope:offline", want: "scope:offline"},
		{name: "scope:offline.access (X/Twitter shape)", ext: "scope:offline.access", want: "scope:offline.access"},
		{name: "query:access_type=offline (Google shape)", ext: "query:access_type=offline", want: "query:access_type=offline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := ""
			if tt.ext != "" {
				ext = "      x-oauth-refresh-token-mechanism: \"" + tt.ext + "\"\n"
			}
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: OAuth API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
` + ext + `      flows:
        authorizationCode:
          authorizationUrl: https://api.example.com/oauth/authorize
          tokenUrl: https://api.example.com/oauth/token
          scopes:
            read: Read access
paths:
  /widgets:
    get:
      security:
        - OAuth2: [read]
      responses:
        "200":
          description: OK
`)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Auth.RefreshTokenMechanism)
		})
	}
}

func TestOpenAPIAuthOverrideExtensions(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: FlightGoat
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
      x-auth-env-vars:
        - FLIGHTAWARE_API_KEY
      x-auth-optional: true
      x-auth-key-url: https://flightaware.com/commercial/aeroapi/
      x-auth-instructions: Sign up for FlightAware AeroAPI and copy the personal API key.
      x-auth-title: FlightAware AeroAPI Key
      x-auth-description: Optional FlightAware AeroAPI credential for enriched flight data.
paths:
  /flights:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, []string{"FLIGHTAWARE_API_KEY"}, parsed.Auth.EnvVars)
	assert.True(t, parsed.Auth.Optional)
	assert.Equal(t, "https://flightaware.com/commercial/aeroapi/", parsed.Auth.KeyURL)
	assert.Equal(t, "Sign up for FlightAware AeroAPI and copy the personal API key.", parsed.Auth.Instructions)
	assert.Equal(t, "FlightAware AeroAPI Key", parsed.Auth.Title)
	assert.Equal(t, "Optional FlightAware AeroAPI credential for enriched flight data.", parsed.Auth.Description)
}

// TestOpenAPIAuthSubtype covers the x-auth-subtype extension on a bearer
// security scheme. The parser accepts the auth0_spa_in_memory value and
// silently drops unrecognized values (typos surface as the field being
// empty, not as a confusing later validation error).
func TestOpenAPIAuthSubtype(t *testing.T) {
	t.Parallel()

	t.Run("auth0_spa_in_memory round-trips", func(t *testing.T) {
		t.Parallel()
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: SpaService
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      x-auth-subtype: auth0_spa_in_memory
paths:
  /me:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "auth0_spa_in_memory", parsed.Auth.Subtype)
	})

	t.Run("unknown subtype is dropped", func(t *testing.T) {
		t.Parallel()
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: SpaService
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      x-auth-subtype: some_typo_subtype
paths:
  /me:
    get:
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Empty(t, parsed.Auth.Subtype,
			"unknown subtype values should not round-trip")
	})
}

func TestOpenAPIAuthKeyURLInference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		expected string
	}{
		{
			name: "explicit x-auth-key-url wins over inference",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
externalDocs:
  url: https://docs.example.com/rest-api/
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
      description: "Visit https://example.com/wrong-page to get a key"
      x-auth-key-url: https://example.com/keys
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "https://example.com/keys",
		},
		{
			name: "url from security scheme description",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
externalDocs:
  url: https://docs.example.com/rest-api/
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
      description: "Generate a token at https://example.com/account/api-keys."
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "https://example.com/account/api-keys",
		},
		{
			name: "no inference when only externalDocs.url is set (docs URL is not a credentials page)",
			yaml: `openapi: "3.0.3"
info:
  title: Figma API
  version: "1.0.0"
externalDocs:
  url: https://developers.figma.com/docs/rest-api/
servers:
  - url: https://api.figma.com
components:
  securitySchemes:
    PersonalAccessToken:
      type: apiKey
      in: header
      name: X-Figma-Token
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "",
		},
		{
			name: "no inference when only info.contact.url is set (homepage is not a credentials page)",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  contact:
    url: https://example.com/developers
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "",
		},
		{
			name: "info.description URL only used when auth-related cues present",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  description: "Generate an API key at https://example.com/account/keys before calling."
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "https://example.com/account/keys",
		},
		{
			name: "info.description URL ignored without auth cue",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  description: "See https://example.com/changelog for release notes."
  contact:
    url: https://example.com/developers
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "",
		},
		{
			name: "no inference when auth.type is none",
			yaml: `openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
externalDocs:
  url: https://docs.example.com/rest-api/
servers:
  - url: https://api.example.com
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse([]byte(tc.yaml))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, parsed.Auth.KeyURL)
		})
	}
}

func TestOpenAPIAuthEnvVarsPopulateRichDefaults(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-env-vars:
        - TODOIST_API_KEY
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVar{
		Name:      "TODOIST_API_KEY",
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}, parsed.Auth.EnvVarSpecs[0])
}

func TestOpenAPIHTTPBasicAuthDefaultsToUsernamePasswordEnvVars(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Twilio
  version: "1.0.0"
servers:
  - url: https://api.twilio.com
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
security:
  - basicAuth: []
paths:
  /Accounts:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, "Authorization", parsed.Auth.Header)
	assert.Equal(t, "Basic {username}:{password}", parsed.Auth.Format)
	assert.Equal(t, []string{"TWILIO_USERNAME", "TWILIO_PASSWORD"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 2)
	assert.Equal(t, spec.AuthEnvVar{
		Name:      "TWILIO_USERNAME",
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: false,
		Inferred:  true,
	}, parsed.Auth.EnvVarSpecs[0])
	assert.Equal(t, spec.AuthEnvVar{
		Name:      "TWILIO_PASSWORD",
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}, parsed.Auth.EnvVarSpecs[1])
}

func TestOpenAPIHTTPBasicAuthHonorsAuthVarsOverride(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Twilio
  version: "1.0.0"
servers:
  - url: https://api.twilio.com
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
      x-auth-vars:
        - name: TWILIO_ACCOUNT_SID
          kind: per_call
          required: true
          sensitive: false
        - name: TWILIO_AUTH_TOKEN
          kind: per_call
          required: true
          sensitive: true
security:
  - basicAuth: []
paths:
  /Accounts:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 2)
	assert.False(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
	assert.True(t, parsed.Auth.EnvVarSpecs[1].Sensitive)
}

func TestOpenAPIAuthVarsRichOverride(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-vars:
        - name: TODOIST_API_KEY
          kind: per_call
          required: true
          sensitive: true
          description: Todoist API key.
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVar{
		Name:        "TODOIST_API_KEY",
		Kind:        spec.AuthEnvVarKindPerCall,
		Required:    true,
		Sensitive:   true,
		Description: "Todoist API key.",
	}, parsed.Auth.EnvVarSpecs[0])
}

func TestOpenAPIAuthVarsPreservesExplicitSensitiveFalse(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Public Slug API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    AccountSlug:
      type: apiKey
      in: header
      name: X-Account-Slug
      x-auth-vars:
        - name: PUBLIC_ACCOUNT_SLUG
          kind: per_call
          required: true
          sensitive: false
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.False(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
}

func TestOpenAPIAuthVarsMalformedFallsBackToDefaults(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-vars:
        - name: TODOIST_API_KEY
          kind: per_call
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = Parse(yamlSpec)
	})
	require.NoError(t, err)

	assert.Contains(t, warnings, "components.securitySchemes.ApiKeyAuth.x-auth-vars is malformed")
	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVarKindPerCall, parsed.Auth.EnvVarSpecs[0].Kind)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Required)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
}

// ServiceTitan-shape: an apiKey scheme carrying a per-tenant header credential
// sits alongside an OAuth2 client_credentials scheme. selectSecurityScheme
// picks the OAuth2 half (bearer Authorization); without sibling-scheme
// collection, the apiKey half is dropped and every request returns 401.
func TestComposedApiKeyPlusOAuthCollectsAdditionalHeader(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: ServiceTitan-shape
  version: "1.0.0"
servers:
  - url: https://api.servicetitan.io
security:
  - apiKeyHeader: []
    oauth: []
components:
  securitySchemes:
    apiKeyHeader:
      type: apiKey
      in: header
      name: ST-App-Key
      x-auth-vars:
        - name: ST_APP_KEY
          kind: per_call
          required: true
          sensitive: true
    oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://auth.servicetitan.io/connect/token
          scopes: {}
paths:
  /tenant/{tenant}/customers:
    get:
      operationId: listCustomers
      parameters:
        - name: tenant
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "OAuth2 cc beats apiKey-header in scheme priority")
	assert.Equal(t, "oauth", parsed.Auth.Scheme)
	require.Len(t, parsed.Auth.AdditionalHeaders, 1, "sibling apiKey scheme must surface as an additional header")
	additional := parsed.Auth.AdditionalHeaders[0]
	assert.Equal(t, "ST-App-Key", additional.Header)
	assert.Equal(t, "header", additional.In)
	assert.Equal(t, "apiKeyHeader", additional.Scheme)
	assert.Equal(t, "ST_APP_KEY", additional.EnvVar.Name)
	assert.Equal(t, spec.AuthEnvVarKindPerCall, additional.EnvVar.Kind)
	assert.True(t, additional.EnvVar.Required)
	assert.True(t, additional.EnvVar.Sensitive)
}

// Single-scheme apiKey (no sibling OAuth) must keep its existing single-scheme
// emission path: the per_call envvar lives on EnvVarSpecs, and
// AdditionalHeaders stays empty so the generator does not emit duplicate
// Config fields or req.Header.Set calls.
func TestSingleApiKeySchemeDoesNotPopulateAdditionalHeaders(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Single ApiKey
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
      x-auth-vars:
        - name: EXAMPLE_API_KEY
          kind: per_call
          required: true
          sensitive: true
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Empty(t, parsed.Auth.AdditionalHeaders, "single-scheme path must not duplicate the primary envvar as additional")
}

// OR-alternative auth: two security requirement objects, each with a single
// scheme — the API accepts EITHER scheme, not both. Sibling detection must
// NOT promote the unused alternative as an additional header. The winning
// scheme runs alone; the alternative is only relevant if the user picks it.
func TestSiblingApiKeyInDifferentRequirementGroupIsSkipped(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: or-alternatives
  version: "1.0.0"
servers:
  - url: https://api.example.com
security:
  - bearer: []
  - apiKey: []
components:
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    apiKey:
      type: apiKey
      in: header
      name: X-Alternative-Key
      x-auth-vars:
        - name: EXAMPLE_ALTERNATIVE_KEY
          kind: per_call
          required: true
          sensitive: true
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)
	assert.Empty(t, parsed.Auth.AdditionalHeaders,
		"OR alternative schemes must not surface as required siblings")
}

// A sibling apiKey scheme that omits the x-auth-vars extension is silently
// skipped: collectAdditionalAuthHeaders never invents env-var names. The
// scheme's per-call credential simply isn't covered, and the primary auth
// remains untouched.
func TestSiblingApiKeyWithoutAuthVarsIsSkipped(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: no-auth-vars-sibling
  version: "1.0.0"
servers:
  - url: https://api.example.com
security:
  - bearer: []
    apiKey: []
components:
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    apiKey:
      type: apiKey
      in: header
      name: X-Sibling-Key
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)
	assert.Empty(t, parsed.Auth.AdditionalHeaders)
}

// Sibling apiKey-in-query schemes are skipped: the issue this addresses is
// header-only (ST-App-Key, Stripe-Signature, Atlassian-Token). A query-param
// sibling would imply mixing query-auth with bearer Authorization, which is
// not a shape this fix supports, and pretending it works would silently emit
// broken code.
func TestSiblingApiKeyInQueryIsSkipped(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: query-sibling
  version: "1.0.0"
servers:
  - url: https://api.example.com
security:
  - bearer: []
    queryKey: []
components:
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    queryKey:
      type: apiKey
      in: query
      name: api_key
      x-auth-vars:
        - name: EXAMPLE_QUERY_KEY
          kind: per_call
          required: true
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)
	assert.Empty(t, parsed.Auth.AdditionalHeaders)
}

func TestOperationLevelHeterogeneousSiblingApiKeysAreSkipped(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: heterogeneous-operation-siblings
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiIntegrationCode:
      type: apiKey
      in: header
      name: ApiIntegrationCode
      x-auth-env-vars:
        - EXAMPLE_INTEGRATION_CODE
    Secret:
      type: apiKey
      in: header
      name: Secret
      x-auth-env-vars:
        - EXAMPLE_SECRET
    UserName:
      type: apiKey
      in: header
      name: UserName
      x-auth-env-vars:
        - EXAMPLE_USERNAME
paths:
  /tickets:
    get:
      security:
        - ApiIntegrationCode: []
          Secret: []
      responses:
        "200":
          description: OK
  /companies:
    get:
      security:
        - ApiIntegrationCode: []
          UserName: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "ApiIntegrationCode", parsed.Auth.Scheme)
	assert.Empty(t, parsed.Auth.AdditionalHeaders,
		"operation-specific sibling sets cannot be represented as global additional headers")
}

func TestOpenAPIAuthClassifiesCookieAndOAuth2ClientCredentialsEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []spec.AuthEnvVar
	}{
		{
			name: "cookie auth is harvested",
			body: `openapi: "3.0.3"
info:
  title: Cookie API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: sessionid
paths:
  /me:
    get:
      responses:
        "200":
          description: OK
`,
			want: []spec.AuthEnvVar{{
				Name:      "COOKIE_COOKIE_AUTH",
				Kind:      spec.AuthEnvVarKindHarvested,
				Required:  true,
				Sensitive: true,
				Inferred:  true,
			}},
		},
		{
			name: "oauth2 client credentials are auth flow inputs",
			body: `openapi: "3.0.3"
info:
  title: Auth0Mgmt
  version: "1.0"
servers:
  - url: https://example.auth0.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.auth0.com/oauth/token
          scopes:
            read:users: Read user profiles
paths:
  /api/v2/users:
    get:
      security:
        - OAuth2: []
      responses:
        "200":
          description: OK
`,
			want: []spec.AuthEnvVar{
				{Name: "AUTH0MGMT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false, Inferred: true},
				{Name: "AUTH0MGMT_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true, Inferred: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := Parse([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Auth.EnvVarSpecs)
			if parsed.Auth.OAuth2Grant == spec.OAuth2GrantClientCredentials {
				require.Len(t, parsed.Auth.EnvVars, len(parsed.Auth.EnvVarSpecs))
				for i, envVar := range parsed.Auth.EnvVarSpecs {
					assert.Equal(t, envVar.Name, parsed.Auth.EnvVars[i])
				}
			}
		})
	}
}

func TestOpenAPINoSecurityHasNoAuthEnvVars(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Public API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "none", parsed.Auth.Type)
	assert.Empty(t, parsed.Auth.EnvVars)
	assert.Empty(t, parsed.Auth.EnvVarSpecs)
}

func TestOpenAPIAuthVarsCanConsolidateLegacyMultipleEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extension string
		want      []spec.AuthEnvVar
	}{
		{
			name: "legacy override preserves multiple generated entries",
			extension: `      x-auth-env-vars:
        - TRIGGER_SECRET_KEY
        - TRIGGER_DEV_API_KEY`,
			want: []spec.AuthEnvVar{
				{Name: "TRIGGER_SECRET_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
				{Name: "TRIGGER_DEV_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
			},
		},
		{
			name: "rich override consolidates to declared credential",
			extension: `      x-auth-env-vars:
        - TRIGGER_SECRET_KEY
        - TRIGGER_DEV_API_KEY
      x-auth-vars:
        - name: TRIGGER_SECRET_KEY
          kind: per_call
          required: true
          sensitive: true`,
			want: []spec.AuthEnvVar{
				{Name: "TRIGGER_SECRET_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yamlSpec := fmt.Appendf(nil, `openapi: "3.0.3"
info:
  title: Trigger Dev
  version: "1.0.0"
servers:
  - url: https://api.trigger.dev
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
%s
paths:
  /runs:
    get:
      responses:
        "200":
          description: OK
`, tt.extension)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Auth.EnvVarSpecs)
		})
	}
}

func TestInferOperationLevelBearer(t *testing.T) {
	t.Parallel()

	t.Run("detects bearer auth from required Authorization header params", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "auth-header-param.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, "header", parsed.Auth.In)
		assert.True(t, parsed.Auth.Inferred)
		assert.NotEmpty(t, parsed.Auth.EnvVars, "EnvVars must be populated for verify")
	})

	t.Run("does not trigger when Authorization params below threshold", func(t *testing.T) {
		// 1 out of 5 operations = 20% < 80% threshold
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{Responses: openapi3.NewResponses()},
			}
			if i == 0 { // only first has Authorization param
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
					}},
				}
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("detects auth at exact eighty percent threshold", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{Responses: openapi3.NewResponses()},
			}
			if i < 4 {
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
					}},
				}
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "bearer_token", result.Type)
		assert.Equal(t, []string{"TEST_API_TOKEN"}, result.EnvVars)
		assert.True(t, result.Inferred)
	})

	t.Run("does not infer bearer without bearer signal", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true,
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("does not infer bearer from negated bearer signal", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true, Description: "Do not use Bearer prefix.",
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("optional Authorization param not counted", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: false, Description: "Bearer token credential",
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("top-level security declaration disables inline inference", func(t *testing.T) {
		doc := &openapi3.T{
			Info:     &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths:    &openapi3.Paths{},
			Security: openapi3.SecurityRequirements{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("explicit securitySchemes still wins over header param", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.False(t, parsed.Auth.Inferred, "explicit auth should not be marked as inferred")
	})
}

func TestInferHeaderParamAPIKeyAuth(t *testing.T) {
	t.Parallel()

	doc := &openapi3.T{
		Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
		Paths: &openapi3.Paths{},
	}
	for _, path := range []string{"/a", "/b", "/c", "/d"} {
		doc.Paths.Set(path, &openapi3.PathItem{
			Get: &openapi3.Operation{
				Responses: openapi3.NewResponses(),
				Parameters: openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "xi-api-key", In: "header", Required: false,
					}},
				},
			},
		})
	}

	result := mapAuth(doc, "elevenlabs")
	assert.Equal(t, "api_key", result.Type)
	assert.Equal(t, "header", result.In)
	assert.Equal(t, "xi-api-key", result.Header)
	assert.Equal(t, []string{"ELEVENLABS_API_KEY"}, result.EnvVars)
	assert.True(t, result.Inferred)
}

func TestAuthTierPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("explicit securitySchemes wins over description keywords", func(t *testing.T) {
		// Gmail has both securitySchemes AND description that could mention auth
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.False(t, parsed.Auth.Inferred, "explicit auth from securitySchemes should not be marked as inferred")
	})

	t.Run("query-param auth tier 2 wins over description tier 3", func(t *testing.T) {
		// Build a minimal spec with auth-like query params on >30% of ops
		// AND bearer keyword in description. Tier 2 should win.
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "This API uses Bearer token authentication.",
			},
			Paths: &openapi3.Paths{},
		}
		// Add 5 operations, 3 with api_key query param (60% > 30% threshold)
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
				},
			}
			if i < 3 { // first 3 have api_key param
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{
						Value: &openapi3.Parameter{
							Name:     "api_key",
							In:       "query",
							Required: false,
						},
					},
				}
			}
			doc.Paths.Set(path, pathItem)
		}

		// Run mapAuth directly — it should pick up query-param auth (tier 2)
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "query", result.In, "tier 2 query-param auth should win over tier 3 description")
		assert.False(t, result.Inferred, "query-param auth is not 'inferred from description'")
	})
}

func TestNoAuthDetection(t *testing.T) {
	t.Parallel()

	t.Run("mixed-auth fixture: per-operation security overrides", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "mixed-auth.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		// stores.listStores has security: [] — should be NoAuth
		stores := parsed.Resources["stores"]
		require.NotNil(t, stores)
		for _, e := range stores.Endpoints {
			if e.Path == "/stores" && e.Method == "GET" {
				assert.True(t, e.NoAuth, "stores GET with security:[] should be NoAuth")
			}
		}

		// menus.getMenu has security: [{}] — should be NoAuth
		menus := parsed.Resources["menus"]
		require.NotNil(t, menus)
		for _, e := range menus.Endpoints {
			if e.Path == "/menus" && e.Method == "GET" {
				assert.True(t, e.NoAuth, "menus GET with security:[{}] should be NoAuth")
			}
		}

		// orders.listOrders inherits global ApiKeyAuth — should NOT be NoAuth
		orders := parsed.Resources["orders"]
		require.NotNil(t, orders)
		for _, e := range orders.Endpoints {
			if e.Path == "/orders" && e.Method == "GET" {
				assert.False(t, e.NoAuth, "orders GET inheriting global auth should not be NoAuth")
			}
			if e.Path == "/orders" && e.Method == "POST" {
				assert.False(t, e.NoAuth, "orders POST with explicit ApiKeyAuth should not be NoAuth")
			}
		}

		// account.getAccount inherits global ApiKeyAuth — should NOT be NoAuth
		account := parsed.Resources["account"]
		require.NotNil(t, account)
		for _, e := range account.Endpoints {
			if e.Path == "/account" && e.Method == "GET" {
				assert.False(t, e.NoAuth, "account GET inheriting global auth should not be NoAuth")
			}
		}
	})

	t.Run("spec with no auth at all marks all endpoints NoAuth", func(t *testing.T) {
		t.Parallel()
		// Build a spec with no securitySchemes, no global security
		doc := &openapi3.T{
			OpenAPI: "3.0.3",
			Info:    &openapi3.Info{Title: "No Auth API", Version: "1.0.0"},
			Paths:   &openapi3.Paths{},
			Servers: openapi3.Servers{{URL: "https://api.example.com"}},
		}
		doc.Paths.Set("/items", &openapi3.PathItem{
			Get: &openapi3.Operation{
				Summary:   "List items",
				Responses: openapi3.NewResponses(),
			},
		})
		doc.Paths.Set("/items/{id}", &openapi3.PathItem{
			Get: &openapi3.Operation{
				Summary:   "Get item",
				Responses: openapi3.NewResponses(),
				Parameters: openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "id", In: "path", Required: true,
						Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					}},
				},
			},
		})

		parsed, err := Parse(mustMarshalJSON(t, doc))
		require.NoError(t, err)

		assert.Equal(t, "none", parsed.Auth.Type)
		// All endpoints should be NoAuth via post-parse sweep
		for _, r := range parsed.Resources {
			for eName, e := range r.Endpoints {
				assert.True(t, e.NoAuth, "endpoint %s should be NoAuth in no-auth spec", eName)
			}
		}
	})

	t.Run("global security empty array marks inherited endpoints NoAuth", func(t *testing.T) {
		t.Parallel()
		// Use raw YAML to preserve the security: [] distinction
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Global Empty Security
  version: "1.0.0"
servers:
  - url: https://api.example.com
security: []
components:
  securitySchemes:
    ApiKey:
      type: apiKey
      name: X-Api-Key
      in: header
paths:
  /public:
    get:
      summary: Public endpoint
      responses:
        "200":
          description: OK
  /private:
    get:
      summary: Private endpoint
      security:
        - ApiKey: []
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		// /public inherits global security:[] — should be NoAuth
		foundPublic := false
		foundPrivate := false
		for _, r := range parsed.Resources {
			for _, e := range r.Endpoints {
				if e.Path == "/public" {
					assert.True(t, e.NoAuth, "/public should be NoAuth from global security:[]")
					foundPublic = true
				}
				if e.Path == "/private" {
					assert.False(t, e.NoAuth, "/private has explicit ApiKey requirement")
					foundPrivate = true
				}
			}
		}
		assert.True(t, foundPublic, "should have found /public endpoint")
		assert.True(t, foundPrivate, "should have found /private endpoint")
	})

	t.Run("anonymous security alternative on every operation makes whole API no-auth", func(t *testing.T) {
		t.Parallel()
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Optional Auth API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
    cookieAuth:
      type: apiKey
      in: cookie
      name: sessionid
paths:
  /pokemon:
    get:
      summary: List pokemon
      security:
        - cookieAuth: []
        - basicAuth: []
        - {}
      responses:
        "200":
          description: OK
  /pokemon/{id}:
    get:
      summary: Get pokemon
      security:
        - cookieAuth: []
        - basicAuth: []
        - {}
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "none", parsed.Auth.Type)
		for _, r := range parsed.Resources {
			for _, e := range r.Endpoints {
				assert.True(t, e.NoAuth, "%s %s should be public", e.Method, e.Path)
			}
		}
	})

	t.Run("petstore still parses without regression", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "petstore", parsed.Name)
		assert.True(t, len(parsed.Resources) > 0, "petstore should have resources")
	})
}

func TestParseTierRoutingExtensions(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Tiered API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-tier-routing:
  default_tier: free
  tiers:
    free:
      auth:
        type: none
    paid:
      base_url: https://paid.api.example.com
      auth:
        type: api_key
        in: query
        header: api_key
        env_vars: [TIERED_PAID_KEY]
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /items:
    x-tier: free
    get:
      summary: List items
      responses:
        "200":
          description: ok
  /items/{id}:
    get:
      summary: Get item
      x-tier: paid
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.True(t, parsed.HasTierRouting())
	assert.Equal(t, "free", parsed.TierRouting.DefaultTier)
	assert.Equal(t, "none", parsed.TierRouting.Tiers["free"].Auth.Type)
	assert.Equal(t, "https://paid.api.example.com", parsed.TierRouting.Tiers["paid"].BaseURL)
	items := parsed.Resources["items"]
	require.NotNil(t, items.Endpoints)
	assert.Equal(t, "free", findEndpointByPath(items, "/items").Tier)
	assert.Equal(t, "paid", findEndpointByPath(items, "/items/{id}").Tier)
}

func TestParseTierRoutingExtensionFromInfo(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Tiered API
  version: 1.0.0
  x-tier-routing:
    default_tier: free
    tiers:
      free:
        auth:
          type: none
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.True(t, parsed.HasTierRouting())
	assert.Equal(t, "free", parsed.TierRouting.DefaultTier)
	assert.Equal(t, "none", parsed.TierRouting.Tiers["free"].Auth.Type)
}

func TestParseTierRoutingRejectsAnonymousSecurityOnCredentialTier(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Contradictory Tier API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-tier-routing:
  default_tier: free
  tiers:
    free:
      auth:
        type: none
    paid:
      auth:
        type: bearer_token
        env_vars: [TIERED_PAID_TOKEN]
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /items:
    get:
      summary: List paid items
      x-tier: paid
      security: []
      responses:
        "200":
          description: ok
`)

	_, err := Parse(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_auth")
	assert.Contains(t, err.Error(), "paid")
}

func findEndpointByPath(resource spec.Resource, path string) spec.Endpoint {
	for _, endpoint := range resource.Endpoints {
		if endpoint.Path == path {
			return endpoint
		}
	}
	for _, sub := range resource.SubResources {
		for _, endpoint := range sub.Endpoints {
			if endpoint.Path == path {
				return endpoint
			}
		}
	}
	return spec.Endpoint{}
}

func TestPathPriorityScore(t *testing.T) {
	t.Parallel()

	t.Run("admin paths score lower than user paths", func(t *testing.T) {
		assert.Greater(t, pathPriorityScore("/users"), pathPriorityScore("/admin/users"))
		assert.Greater(t, pathPriorityScore("/channels"), pathPriorityScore("/admin.conversations"))
		assert.Greater(t, pathPriorityScore("/messages"), pathPriorityScore("/internal/metrics"))
		assert.Greater(t, pathPriorityScore("/items"), pathPriorityScore("/system/health"))
		assert.Greater(t, pathPriorityScore("/teams"), pathPriorityScore("/management/roles"))
	})

	t.Run("shallow paths score higher than deep paths", func(t *testing.T) {
		assert.Greater(t, pathPriorityScore("/users"), pathPriorityScore("/users/{id}/posts/{postId}/comments"))
	})

	t.Run("short paths get bonus", func(t *testing.T) {
		short := pathPriorityScore("/users")
		long := pathPriorityScore("/a/b/c/d")
		assert.Greater(t, short, long)
	})
}

func TestPathPriorityScoreSortOrder(t *testing.T) {
	t.Parallel()

	// Build 600 paths: 100 admin.* paths and 500 user-facing paths.
	var paths []string
	for i := range 100 {
		paths = append(paths, fmt.Sprintf("/admin.resource%d/action", i))
	}
	for i := range 500 {
		paths = append(paths, fmt.Sprintf("/resource%d", i))
	}

	// Sort by priority score descending, alphabetical tiebreaker.
	sort.SliceStable(paths, func(i, j int) bool {
		si, sj := pathPriorityScore(paths[i]), pathPriorityScore(paths[j])
		if si != sj {
			return si > sj
		}
		return paths[i] < paths[j]
	})

	// With a 500-path cap, all admin paths should be in the tail (indices 500+).
	const maxResources = 500
	kept := paths[:maxResources]
	dropped := paths[maxResources:]

	for _, p := range dropped {
		assert.Contains(t, p, "admin", "expected only admin paths to be dropped, but got: %s", p)
	}
	for _, p := range kept {
		assert.NotContains(t, p, "admin", "expected no admin paths in kept set, but got: %s", p)
	}
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

// TestSelectDescription locks in that the OpenAPI parser prefers
// the long-form `description` over the short `summary` when both are
// present. The earlier rule ("if summary has spaces, use it")
// inverted the priority for the common case where a multi-word
// summary sits alongside a rich description, and was the root cause
// behind 47 thin-mcp-description findings on the scrape-creators
// CLI even though every endpoint had rich source description text.
func TestSelectDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		summary     string
		description string
		want        string
	}{
		{
			name:        "rich description wins over multi-word summary",
			summary:     "Get credit balance",
			description: "Returns the number of API credits remaining on your Scrape Creators account.",
			want:        "Returns the number of API credits remaining on your Scrape Creators account.",
		},
		{
			name:        "rich description wins over single-word summary",
			summary:     "Profile",
			description: "Fetches public profile data for a TikTok user by their handle.",
			want:        "Fetches public profile data for a TikTok user by their handle.",
		},
		{
			name:        "summary used when description empty",
			summary:     "Get the user",
			description: "",
			want:        "Get the user",
		},
		{
			name:        "shorter description (placeholder) falls back to summary",
			summary:     "Returns the order with full line items and shipping address",
			description: "TODO",
			want:        "Returns the order with full line items and shipping address",
		},
		{
			name:        "mangled operationID summary is humanized when alone",
			summary:     "GetUserById",
			description: "",
			want:        "Get user by id",
		},
		{
			name:        "both empty returns empty",
			summary:     "",
			description: "",
			want:        "",
		},
		{
			name:        "description-only case",
			summary:     "",
			description: "Returns recent orders.",
			want:        "Returns recent orders.",
		},
		{
			name:        "description equal length to summary still prefers description",
			summary:     "abc",
			description: "xyz",
			want:        "xyz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := selectDescription(tt.summary, tt.description)
			assert.Equal(t, tt.want, got)
		})
	}
}

// findEndpoint walks resource endpoints (top-level and sub-resource) returning
// the first endpoint whose path matches. Test helper.
func findEndpoint(t *testing.T, parsed *spec.APISpec, path string) spec.Endpoint {
	t.Helper()
	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			if e.Path == path {
				return e
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if e.Path == path {
					return e
				}
			}
		}
	}
	t.Fatalf("no endpoint found at path %q", path)
	return spec.Endpoint{}
}

func TestParseReadsXResourceIDAndXCritical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string // OpenAPI path key — kept stable across cases
		extraExt     string // extra path-item extensions injected raw
		wantIDField  string
		wantCritical bool
	}{
		{
			name: "x-resource-id explicit string wins over schema fallbacks",
			extraExt: `    x-resource-id: ticker
    x-critical: true
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical accepts string \"true\"",
			extraExt: `    x-resource-id: ticker
    x-critical: "true"
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical accepts string \"1\"",
			extraExt: `    x-resource-id: ticker
    x-critical: "1"
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical false (bool) leaves resource non-critical",
			extraExt: `    x-resource-id: ticker
    x-critical: false
`,
			wantIDField:  "ticker",
			wantCritical: false,
		},
		{
			name: "x-critical non-truthy string treated as false",
			extraExt: `    x-resource-id: ticker
    x-critical: "maybe"
`,
			wantIDField:  "ticker",
			wantCritical: false,
		},
		{
			name: "malformed x-resource-id integer ignored, falls back to id",
			extraExt: `    x-resource-id: 123
`,
			wantIDField:  "id", // fallback tier 2: response schema declares id
			wantCritical: false,
		},
		{
			name:         "no extensions: response-schema fallback picks id",
			extraExt:     ``,
			wantIDField:  "id",
			wantCritical: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
` + tt.extraExt + `    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
                    label:
                      type: string
`)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, "/widgets")
			assert.Equal(t, tt.wantIDField, ep.IDField, "IDField")
			assert.Equal(t, tt.wantCritical, ep.Critical, "Critical")
		})
	}
}

func TestParseIDFieldFallbackChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemaYAML string
		wantID     string
	}{
		{
			name: "tier 2: id present (required)",
			schemaYAML: `                  type: object
                  required: [id]
                  properties:
                    id: {type: string}
                    label: {type: string}
`,
			wantID: "id",
		},
		{
			name: "tier 2: id present (optional) still wins",
			schemaYAML: `                  type: object
                  properties:
                    id: {type: string}
                    label: {type: string}
`,
			wantID: "id",
		},
		{
			name: "tier 3.5: gid wins over name (Asana shape)",
			schemaYAML: `                  type: object
                  properties:
                    gid: {type: string}
                    name: {type: string}
                    resource_type: {type: string}
`,
			wantID: "gid",
		},
		{
			name: "tier 3.5: sid wins over name (Twilio shape)",
			schemaYAML: `                  type: object
                  properties:
                    sid: {type: string}
                    name: {type: string}
                    friendly_name: {type: string}
`,
			wantID: "sid",
		},
		{
			name: "tier 3.5: uid wins over name",
			schemaYAML: `                  type: object
                  properties:
                    uid: {type: string}
                    name: {type: string}
`,
			wantID: "uid",
		},
		{
			name: "tier 3.5: uuid wins over name",
			schemaYAML: `                  type: object
                  properties:
                    uuid: {type: string}
                    name: {type: string}
`,
			wantID: "uuid",
		},
		{
			name: "tier 3.5: guid wins over name",
			schemaYAML: `                  type: object
                  properties:
                    guid: {type: string}
                    name: {type: string}
`,
			wantID: "guid",
		},
		{
			name: "tier 3.5: id wins over gid (tier 2 takes precedence)",
			schemaYAML: `                  type: object
                  properties:
                    id: {type: string}
                    gid: {type: string}
                    name: {type: string}
`,
			wantID: "id",
		},
		{
			name: "tier 3: name when id absent",
			schemaYAML: `                  type: object
                  properties:
                    name: {type: string}
                    description: {type: string}
`,
			wantID: "name",
		},
		{
			name: "tier 4: first required scalar when id and name absent",
			schemaYAML: `                  type: object
                  required: [ticker, market]
                  properties:
                    market: {type: string}
                    ticker: {type: string}
                    description: {type: string}
`,
			wantID: "ticker",
		},
		{
			name: "tier 4: object-typed required field is skipped, next scalar wins",
			schemaYAML: `                  type: object
                  required: [meta, code]
                  properties:
                    meta:
                      type: object
                      properties:
                        version: {type: string}
                    code: {type: integer}
`,
			wantID: "code",
		},
		{
			name: "tier 5: bottoms out when no required scalar exists",
			schemaYAML: `                  type: object
                  properties:
                    payload:
                      type: object
                      properties:
                        x: {type: string}
`,
			wantID: "",
		},
		{
			// A required boolean must not be picked as the PK — booleans
			// collapse N rows onto "true"/"false" during upsert.
			name: "tier 5: boolean required field is skipped",
			schemaYAML: `                  type: object
                  required: [is_active, sku]
                  properties:
                    is_active: {type: boolean}
                    sku: {type: string}
`,
			wantID: "sku",
		},
		{
			// A required enum-restricted string must not be picked — enums
			// have hand-picked low cardinality and collapse distinct rows onto
			// the same PK during upsert.
			name: "tier 5: enum-restricted string is skipped",
			schemaYAML: `                  type: object
                  required: [status, ticker]
                  properties:
                    status:
                      type: string
                      enum: [active, paused, closed]
                    ticker: {type: string}
`,
			wantID: "ticker",
		},
		{
			// A required date-time field must not be picked — timestamps are
			// structurally non-identifier-shaped and often shared across
			// batches of records.
			name: "tier 5: date-time formatted field is skipped",
			schemaYAML: `                  type: object
                  required: [created_at, order_number]
                  properties:
                    created_at:
                      type: string
                      format: date-time
                    order_number: {type: string}
`,
			wantID: "order_number",
		},
		{
			// Date-only format must also be skipped — same uniqueness concern
			// as date-time.
			name: "tier 5: date-only formatted field is skipped",
			schemaYAML: `                  type: object
                  required: [delivery_date, tracking_code]
                  properties:
                    delivery_date:
                      type: string
                      format: date
                    tracking_code: {type: string}
`,
			wantID: "tracking_code",
		},
		{
			// All required fields are non-plausible-PK — empty result so
			// templates fall through to runtime fallbacks instead of locking
			// in a poison override.
			name: "tier 5: empty when only boolean/enum/date-time required fields exist",
			schemaYAML: `                  type: object
                  required: [is_active, status, created_at]
                  properties:
                    is_active: {type: boolean}
                    status:
                      type: string
                      enum: [active, paused]
                    created_at:
                      type: string
                      format: date-time
`,
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
` + tt.schemaYAML)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, "/things")
			assert.Equal(t, tt.wantID, ep.IDField)
			assert.False(t, ep.Critical)
		})
	}
}

// TestParseIDFieldEnvelopeUnwrapping covers list responses whose payload is an
// object envelope wrapping a single named array (e.g. {events: [...],
// cursor: "..."}; many list APIs use this shape with the resource name as the
// array key). The profiler must descend into the array's item schema and pick
// the item's PK, not a scalar sibling on the wrapper.
func TestParseIDFieldEnvelopeUnwrapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemaYAML string
		wantID     string
	}{
		{
			name: "named-array envelope with cursor sibling: descends into items",
			schemaYAML: `              schema:
                type: object
                required: [events, cursor]
                properties:
                  events:
                    type: array
                    items:
                      type: object
                      required: [event_ticker]
                      properties:
                        event_ticker: {type: string}
                        title: {type: string}
                  cursor: {type: string}
`,
			wantID: "event_ticker",
		},
		{
			name: "named-array envelope with object-typed sibling: still descends",
			schemaYAML: `              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                      required: [sku]
                      properties:
                        sku: {type: string}
                  pagination:
                    type: object
                    properties:
                      next: {type: string}
`,
			wantID: "sku",
		},
		{
			name: "data-wrapper envelope still works (preserved fast path)",
			schemaYAML: `              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
                  cursor: {type: string}
`,
			wantID: "id",
		},
		{
			name: "two top-level arrays: ambiguous, falls back to wrapper",
			schemaYAML: `              schema:
                type: object
                properties:
                  event_positions:
                    type: array
                    items: {type: object}
                  market_positions:
                    type: array
                    items: {type: object}
`,
			wantID: "",
		},
		{
			// A malformed array property (no items) sits alongside a
			// well-formed one. singleArrayProperty must skip the malformed
			// entry without it counting toward the "exactly one" cap, so the
			// well-formed sibling still wins and PK detection succeeds.
			name: "named-array envelope with one malformed sibling: well-formed array still wins",
			schemaYAML: `              schema:
                type: object
                properties:
                  events:
                    type: array
                    items:
                      type: object
                      required: [event_ticker]
                      properties:
                        event_ticker: {type: string}
                  legacy:
                    type: array
                  cursor: {type: string}
`,
			wantID: "event_ticker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          description: OK
          content:
            application/json:
` + tt.schemaYAML)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, "/things")
			assert.Equal(t, tt.wantID, ep.IDField)
		})
	}
}

// TestParseIDFieldResourcePrefixedHeuristic covers list responses whose item
// schemas key off `<singular_resource>_id` (or `_uuid`/`_guid`) instead of a
// bare `id`. Without this heuristic, APIs like podscan whose Category items
// only carry `category_id` would fall through every fallback tier and leave
// IDField empty, causing sync to silently drop every row.
func TestParseIDFieldResourcePrefixedHeuristic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		schemaYAML string
		wantID     string
	}{
		{
			name: "plural resource picks <singular>_id",
			path: "/categories",
			schemaYAML: `                  type: object
                  properties:
                    category_id: {type: string}
                    category_name: {type: string}
                    category_display_name: {type: string}
`,
			wantID: "category_id",
		},
		{
			name: "singular resource picks <name>_id",
			path: "/user",
			schemaYAML: `                  type: object
                  properties:
                    user_id: {type: string}
                    user_name: {type: string}
`,
			wantID: "user_id",
		},
		{
			name: "id wins over <singular>_id (REST convention)",
			path: "/categories",
			schemaYAML: `                  type: object
                  properties:
                    id: {type: string}
                    category_id: {type: string}
`,
			wantID: "id",
		},
		{
			name: "<singular>_id wins over name",
			path: "/categories",
			schemaYAML: `                  type: object
                  properties:
                    name: {type: string}
                    category_id: {type: string}
`,
			wantID: "category_id",
		},
		{
			name: "_uuid suffix is recognized when _id is absent",
			path: "/sessions",
			schemaYAML: `                  type: object
                  properties:
                    session_uuid: {type: string}
                    started_at: {type: string}
`,
			wantID: "session_uuid",
		},
		{
			name: "_guid suffix is recognized when _id and _uuid are absent",
			path: "/devices",
			schemaYAML: `                  type: object
                  properties:
                    device_guid: {type: string}
                    last_seen: {type: string}
`,
			wantID: "device_guid",
		},
		{
			name: "camelCase property name normalizes to snake match",
			path: "/categories",
			schemaYAML: `                  type: object
                  properties:
                    categoryId: {type: string}
                    categoryName: {type: string}
`,
			wantID: "categoryId",
		},
		{
			name: "kebab-case path resource singularizes correctly",
			path: "/auth-tokens",
			schemaYAML: `                  type: object
                  properties:
                    auth_token_id: {type: string}
                    issued_at: {type: string}
`,
			wantID: "auth_token_id",
		},
		{
			name: "_id precedence: prefers _id over _uuid",
			path: "/categories",
			schemaYAML: `                  type: object
                  properties:
                    category_id: {type: string}
                    category_uuid: {type: string}
`,
			wantID: "category_id",
		},
		{
			name: "no <singular>_id falls through to remaining tiers",
			path: "/things",
			schemaYAML: `                  type: object
                  properties:
                    name: {type: string}
                    other_id: {type: string}
`,
			wantID: "name",
		},
		{
			// Without the irregulars override, `movies` would singularize
			// via the `ies → y` rule to `movy`, missing `movie_id`.
			name: "ie-ending stem keeps singular form (movies → movie)",
			path: "/movies",
			schemaYAML: `                  type: object
                  properties:
                    movie_id: {type: string}
                    title: {type: string}
`,
			wantID: "movie_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  ` + tt.path + `:
    get:
      operationId: list
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
` + tt.schemaYAML)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, tt.path)
			assert.Equal(t, tt.wantID, ep.IDField)
		})
	}
}

// TestParseXResourceIDAppliesToEveryOperationOnPath exercises the "extensions
// live on the path item" rule — both GET and POST operations under /widgets
// inherit the x-resource-id and x-critical values, even though x-critical is
// only meaningful for the syncable list endpoint.
func TestParseXResourceIDAppliesToEveryOperationOnPath(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    x-resource-id: widget_uid
    x-critical: true
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: {type: string}
    post:
      operationId: createWidget
      responses:
        "201":
          description: Created
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	var seen int
	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			if e.Path == "/widgets" {
				assert.Equal(t, "widget_uid", e.IDField, "method=%s", e.Method)
				assert.True(t, e.Critical, "method=%s", e.Method)
				seen++
			}
		}
	}
	assert.Equal(t, 2, seen, "expected GET + POST on /widgets to inherit extensions")
}

// TestParsePetstoreXExtensionsBaseline ensures the existing OpenAPI fixture
// (no x-resource-id, no x-critical) is unaffected — IDField falls through to
// the schema-fallback path, Critical stays false.
func TestParsePetstoreXExtensionsBaseline(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			assert.False(t, e.Critical, "%s %s: Critical must default to false", e.Method, e.Path)
		}
	}
}

// captureWarnings swaps the package-level warnWriter for an in-memory
// buffer, runs fn, and returns whatever fn wrote via warnf. Restores
// warnWriter on exit so other tests aren't affected. Tests are NOT
// parallel-safe with this helper because warnWriter is package-global —
// callers must not call t.Parallel().
func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	prev := warnWriter
	var buf bytes.Buffer
	warnWriter = &buf
	defer func() { warnWriter = prev }()
	fn()
	return buf.String()
}

// TestParseFrameworkCollisionRenamesAndWarns asserts that an OpenAPI
// path producing a top-level resource name in ReservedCobraUseNames is
// renamed to <api-slug>-<original> and a warning is emitted naming both
// forms.
func TestParseFrameworkCollisionRenamesAndWarns(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /version:
    get:
      operationId: listVersions
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	resourceNames := make([]string, 0, len(parsed.Resources))
	for name := range parsed.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	assert.NotContains(t, resourceNames, "version", "version resource must be renamed; raw `version` would shadow framework cobra command")
	assert.Contains(t, resourceNames, "testapi-version", "renamed resource must use <api-slug>-<original> form")
	assert.Contains(t, resourceNames, "widgets", "non-colliding resources are unchanged")

	assert.Contains(t, output, `"version"`, "warning must name the original resource")
	assert.Contains(t, output, `"testapi-version"`, "warning must name the renamed resource")
	assert.Contains(t, output, "shadow framework cobra command", "warning must explain the failure mode")
}

// TestParseFrameworkCollisionLeavesNonCollidingSpecsAlone asserts specs
// without a colliding resource produce byte-identical resource maps —
// no spurious renames, no warnings emitted.
func TestParseFrameworkCollisionLeavesNonCollidingSpecsAlone(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	require.Contains(t, parsed.Resources, "widgets")
	assert.NotContains(t, output, "shadow framework cobra command", "non-colliding spec must not emit a collision warning")
}

func TestParseFrameworkCollisionAllowsConditionalFrameworkNamesWhenInactive(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /health:
    get:
      operationId: getHealth
      responses:
        "200":
          description: ok
  /api/auth/check-email:
    get:
      operationId: checkEmail
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	require.Contains(t, parsed.Resources, "health", "health is not a parser-time collision unless the generator emits the health insight command")
	require.Contains(t, parsed.Resources, "auth", "public auth resources should keep their natural name when no framework auth command is emitted")
	assert.NotContains(t, parsed.Resources, "testapi-health")
	assert.NotContains(t, parsed.Resources, "testapi-auth")
	assert.NotContains(t, output, "shadow framework cobra command")
}

// TestParseFrameworkCollisionExemptsSubresources verifies sub-resources
// don't trigger the collision check — paths like /games/{id}/version
// produce a `version` sub-resource under `games`, which registers as a
// subcommand of `games` rather than at the root, so it can't shadow the
// framework's top-level `version` command.
func TestParseFrameworkCollisionExemptsSubresources(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /games:
    get:
      operationId: listGames
      responses:
        "200":
          description: ok
  /games/{id}/version:
    get:
      operationId: getGameVersion
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	// The path /games/{id}/version creates a `games` resource with a
	// `version` sub-resource — neither needs renaming. Top-level `games`
	// stays as-is; sub-resource `version` lives under it.
	require.Contains(t, parsed.Resources, "games")
	games := parsed.Resources["games"]
	assert.Contains(t, games.SubResources, "version", "version path under games should land as a sub-resource")
	assert.NotContains(t, parsed.Resources, "version", "top-level version resource must not exist — sub-resources are exempt from rename")
	assert.NotContains(t, parsed.Resources, "testapi-version", "no rename should fire for sub-resource paths")
	assert.NotContains(t, output, "shadow framework cobra command", "sub-resources must not trigger the collision warning")
}

// TestParseFrameworkCollisionFallsBackToApiSlugWhenSpecNameEmpty pins
// the empty-slug fallback: when out.Name is empty, the rename uses "api"
// as the slug so the result never has a leading hyphen.
func TestParseFrameworkCollisionFallsBackToApiSlugWhenSpecNameEmpty(t *testing.T) {
	// info.title omitted forces cleanSpecName to return its default ("api"),
	// which the parser then refuses (line 167 in parser.go), so we simulate
	// the empty-slug path by directly invoking renameForFrameworkCollision
	// against a spec.APISpec with Name == "".
	out := &spec.APISpec{Name: "", Resources: map[string]spec.Resource{}}
	output := captureWarnings(t, func() {
		renamed := renameForFrameworkCollision(out, "version", "/version")
		assert.Equal(t, "api-version", renamed, "empty Name must fall back to `api` so the result never starts with a hyphen")
	})
	assert.Contains(t, output, `"api-version"`)
}

// TestParseFrameworkCollisionSelfCollisionBumpsSuffix covers the rare
// case where <api-slug>-<original> itself collides with another resource
// already in out.Resources. The implementation falls through to a
// numeric suffix (-2, -3, ...) until unique.
func TestParseFrameworkCollisionSelfCollisionBumpsSuffix(t *testing.T) {
	out := &spec.APISpec{
		Name: "testapi",
		Resources: map[string]spec.Resource{
			"testapi-version": {}, // pre-existing — forces suffix bump
		},
	}
	output := captureWarnings(t, func() {
		renamed := renameForFrameworkCollision(out, "version", "/version")
		assert.Equal(t, "testapi-version-2", renamed, "first-fallback should suffix -2 when the primary rename target already exists")
	})
	assert.Contains(t, output, `"testapi-version-2"`)
}

// TestFilterGlobalParamsRequiresMinEndpoints pins the open-meteo regression:
// a single-endpoint spec with many query parameters used to have all its
// params stripped because every param trivially appeared on 100% of
// endpoints (1/1) and the >80% global-filter threshold matched. The filter
// now requires at least 3 endpoints before it considers any pattern
// "global" — fewer endpoints means there's nothing meaningful to compare.
func TestFilterGlobalParamsRequiresMinEndpoints(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: 3.0.0
info:
  title: TestAPI
  version: "1.0"
paths:
  /v1/forecast:
    get:
      operationId: list
      tags: [forecast]
      parameters:
        - name: latitude
          in: query
          required: true
          schema: {type: number}
        - name: longitude
          in: query
          required: true
          schema: {type: number}
        - name: hourly
          in: query
          schema: {type: string}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
`
	spec, err := Parse([]byte(specYAML))
	require.NoError(t, err)

	resource, ok := spec.Resources["forecast"]
	require.True(t, ok, "forecast resource should exist")
	endpoint, ok := resource.Endpoints["list"]
	require.True(t, ok, "list endpoint should exist")
	assert.Len(t, endpoint.Params, 3, "single-endpoint spec must keep its params; the global-filter must not strip them")

	names := map[string]bool{}
	for _, p := range endpoint.Params {
		names[p.Name] = true
	}
	for _, want := range []string{"latitude", "longitude", "hourly"} {
		assert.True(t, names[want], "param %q must be preserved", want)
	}
}

// TestParsePerOperationServersFallback covers the case where a spec has no
// top-level `servers:` block but each operation declares its own. The parser
// must walk per-operation servers and pick the most common one as base URL.
// Pre-fix this hit https://api.example.com and produced a CLI that DNS-failed
// every call — see cli-printing-press#510 for the open-meteo report.
func TestParsePerOperationServersFallback(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Per-Op Servers Test
  version: "1.0"
paths:
  /forecast:
    get:
      operationId: forecast
      servers:
        - url: https://api.example.com
      responses:
        '200':
          description: OK
  /historical:
    get:
      operationId: historical
      servers:
        - url: https://archive.example.com
      responses:
        '200':
          description: OK
  /weather:
    get:
      operationId: weather
      servers:
        - url: https://api.example.com
      responses:
        '200':
          description: OK
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	// api.example.com appears 2x, archive.example.com appears 1x — most-common wins.
	assert.Equal(t, "https://api.example.com", parsed.BaseURL)
}

// TestParsePerOperationServersFallbackTieBreak verifies deterministic
// tie-breaking: when two server URLs appear with equal frequency, the
// lexicographically smaller one wins so the output doesn't churn run-to-run.
func TestParsePerOperationServersFallbackTieBreak(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Tie Break Test
  version: "1.0"
paths:
  /alpha:
    get:
      operationId: alpha
      servers:
        - url: https://b.example.com
      responses:
        '200': {description: OK}
  /beta:
    get:
      operationId: beta
      servers:
        - url: https://a.example.com
      responses:
        '200': {description: OK}
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	// Both URLs appear once; lexicographically smallest wins.
	assert.Equal(t, "https://a.example.com", parsed.BaseURL)
}

// TestParseTopLevelServersStillPreferred verifies the per-operation walk is
// only used as a fallback. When top-level `servers:` is set, the parser must
// continue to use it even if operations also declare their own.
func TestParseTopLevelServersStillPreferred(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Top-Level Wins Test
  version: "1.0"
servers:
  - url: https://global.example.com
paths:
  /thing:
    get:
      operationId: thing
      servers:
        - url: https://override.example.com
      responses:
        '200': {description: OK}
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	assert.Equal(t, "https://global.example.com", parsed.BaseURL)
}

func TestParseOperationServersBecomeEndpointBaseURLOverrides(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Multi Host Test
  version: "1.0"
servers:
  - url: https://api.open-meteo.com/v1
paths:
  /forecast:
    get:
      operationId: forecast
      responses:
        '200': {description: OK}
  /search:
    get:
      operationId: geocoding
      servers:
        - url: https://geocoding-api.open-meteo.com/v1
      responses:
        '200': {description: OK}
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	assert.Equal(t, "https://api.open-meteo.com/v1", parsed.BaseURL)

	var search spec.Endpoint
	found := false
	for _, resource := range parsed.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Path == "/search" {
				search = endpoint
				found = true
			}
		}
	}
	require.True(t, found, "expected /search endpoint to be parsed")
	assert.Equal(t, "https://geocoding-api.open-meteo.com/v1", search.BaseURL)
}

func TestParseMCPExtensionFromRoot(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Large API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-mcp:
  transport: [stdio, http]
  orchestration: code
  endpoint_tools: hidden
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.True(t, parsed.MCP.HasTransport("http"), "expected http transport from x-mcp")
	assert.True(t, parsed.MCP.HasTransport("stdio"), "expected stdio transport from x-mcp")
	assert.True(t, parsed.MCP.IsCodeOrchestration(), "expected code orchestration from x-mcp")
	assert.Equal(t, "hidden", parsed.MCP.EndpointTools)
}

func TestParseMCPFallbackFromRootWarns(t *testing.T) {
	data := []byte(`
openapi: 3.0.3
info:
  title: Legacy MCP API
  version: 1.0.0
servers:
  - url: https://api.example.com
mcp:
  transport: [stdio, http]
  orchestration: code
  endpoint_tools: hidden
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = Parse(data)
	})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.True(t, parsed.MCP.HasTransport("http"), "expected http transport from root mcp fallback")
	assert.True(t, parsed.MCP.HasTransport("stdio"), "expected stdio transport from root mcp fallback")
	assert.True(t, parsed.MCP.IsCodeOrchestration(), "expected code orchestration from root mcp fallback")
	assert.Equal(t, "hidden", parsed.MCP.EndpointTools)
	assert.Contains(t, warnings, "accepted root-level 'mcp:' for backwards compatibility")
	assert.Contains(t, warnings, "rename to 'x-mcp:' per OpenAPI extension convention")
}

func TestParseMCPExtensionBeatsRootMCPFallback(t *testing.T) {
	data := []byte(`
openapi: 3.0.3
info:
  title: Canonical MCP API
  version: 1.0.0
servers:
  - url: https://api.example.com
mcp:
  transport: [stdio]
  orchestration: code
  endpoint_tools: hidden
x-mcp:
  transport: [http]
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = Parse(data)
	})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.True(t, parsed.MCP.HasTransport("http"), "canonical x-mcp must take precedence")
	assert.False(t, parsed.MCP.HasTransport("stdio"), "root mcp fallback must not merge into canonical x-mcp")
	assert.False(t, parsed.MCP.IsCodeOrchestration(), "root mcp fields must not fill missing x-mcp fields")
	assert.Empty(t, parsed.MCP.EndpointTools, "root mcp fields must not fill missing x-mcp fields")
	assert.NotContains(t, warnings, "accepted root-level 'mcp:'", "canonical x-mcp should not emit fallback warning")
}

func TestParseMCPInfoExtensionBeatsRootMCPFallback(t *testing.T) {
	data := []byte(`
openapi: 3.0.3
info:
  title: Info MCP API
  version: 1.0.0
  x-mcp:
    transport: [http]
servers:
  - url: https://api.example.com
mcp:
  transport: [stdio]
  orchestration: code
  endpoint_tools: hidden
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = Parse(data)
	})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.True(t, parsed.MCP.HasTransport("http"), "info.x-mcp must take precedence over root mcp fallback")
	assert.False(t, parsed.MCP.HasTransport("stdio"), "root mcp fallback must not merge into info.x-mcp")
	assert.False(t, parsed.MCP.IsCodeOrchestration(), "root mcp fields must not fill missing info.x-mcp fields")
	assert.Empty(t, parsed.MCP.EndpointTools, "root mcp fields must not fill missing info.x-mcp fields")
	assert.NotContains(t, warnings, "accepted root-level 'mcp:'", "info.x-mcp should not emit fallback warning")
}

func TestParseMCPExtensionFromInfo(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Large API
  version: 1.0.0
  x-mcp:
    transport: [stdio, http]
    orchestration: code
    endpoint_tools: hidden
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.True(t, parsed.MCP.HasTransport("http"), "expected http transport from info.x-mcp")
	assert.True(t, parsed.MCP.IsCodeOrchestration(), "expected code orchestration from info.x-mcp")
	assert.Equal(t, "hidden", parsed.MCP.EndpointTools)
}

func TestParseMCPExtensionAbsentLeavesZeroValue(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Plain API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.Empty(t, parsed.MCP.Transport)
	assert.Empty(t, parsed.MCP.Orchestration)
	assert.Empty(t, parsed.MCP.EndpointTools)
}

func TestParseMCPExtensionRootBeatsInfo(t *testing.T) {
	t.Parallel()
	// Root and info declare mutually exclusive transports so the test
	// distinguishes root-wins from a hypothetical merge that would satisfy
	// both transports simultaneously.
	data := []byte(`
openapi: 3.0.3
info:
  title: Both-Levels API
  version: 1.0.0
  x-mcp:
    transport: [stdio]
servers:
  - url: https://api.example.com
x-mcp:
  transport: [http]
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.True(t, parsed.MCP.HasTransport("http"), "root x-mcp must take precedence over info.x-mcp")
	assert.False(t, parsed.MCP.HasTransport("stdio"), "info.x-mcp transport must not leak through when root x-mcp is set")
}

func TestParseMCPExtensionRoundTripsAddrAndThreshold(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Full MCP API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-mcp:
  transport: [stdio, http]
  addr: ":9090"
  orchestration_threshold: 25
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, ":9090", parsed.MCP.Addr)
	assert.Equal(t, 25, parsed.MCP.OrchestrationThreshold)
}

func TestParseMCPExtensionRoundTripsIntents(t *testing.T) {
	t.Parallel()
	// Intents is the most structurally complex MCPConfig field
	// (nested []Intent -> []IntentParam + []IntentStep with map[string]string Bind).
	// This test catches a bad json tag anywhere in that tree by asserting the
	// whole shape parses cleanly through the JSON marshal/unmarshal roundtrip,
	// then validates against the spec's resources.
	data := []byte(`
openapi: 3.0.3
info:
  title: Intent API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-mcp:
  intents:
    - name: list_all_items
      description: Fetch every item
      params:
        - name: limit
          type: integer
          required: false
          description: Cap on items returned
      steps:
        - endpoint: items.list
          bind:
            limit: ${input.limit}
          capture: items
      returns: items
paths:
  /items:
    get:
      operationId: listItems
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, parsed.MCP.Intents, 1)
	intent := parsed.MCP.Intents[0]
	assert.Equal(t, "list_all_items", intent.Name)
	require.Len(t, intent.Params, 1)
	assert.Equal(t, "limit", intent.Params[0].Name)
	assert.Equal(t, "integer", intent.Params[0].Type)
	require.Len(t, intent.Steps, 1)
	assert.Equal(t, "items.list", intent.Steps[0].Endpoint)
	assert.Equal(t, "${input.limit}", intent.Steps[0].Bind["limit"])
	assert.Equal(t, "items", intent.Steps[0].Capture)
	assert.Equal(t, "items", intent.Returns)
}

func TestParseMCPExtensionRejectsUnknownTransport(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Bad MCP API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-mcp:
  transport: [grpc]
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	_, err := Parse(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transport")
}

func TestParseMultipartRequestBodyPreservesContentType(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Upload API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /assets:
    post:
      operationId: uploadAsset
      summary: Upload asset
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [assetData, filename]
              properties:
                assetData:
                  type: string
                  format: binary
                  description: Asset file
                filename:
                  type: string
                  description: File name
      responses:
        "201":
          description: created
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/assets")
	assert.Equal(t, "multipart/form-data", endpoint.RequestContentType)
	require.Len(t, endpoint.Body, 2)
	byName := map[string]spec.Param{}
	for _, param := range endpoint.Body {
		byName[param.Name] = param
	}
	assert.Equal(t, "binary", byName["assetData"].Format)
	assert.True(t, byName["assetData"].Required)
	assert.True(t, byName["filename"].Required)
}

func TestParseBinaryResponseFormat(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.1.0
info:
  title: Audio API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /v1/audio:
    post:
      operationId: createAudio
      responses:
        "200":
          description: Audio bytes
          content:
            audio/mpeg:
              schema:
                type: string
                format: binary
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/v1/audio")
	assert.Equal(t, spec.ResponseFormatBinary, endpoint.ResponseFormat)
	assert.True(t, endpoint.UsesBinaryResponse())
}

func TestParseFormUrlencodedRequestBodyPreservesContentType(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: OAuth API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /oauth/token:
    post:
      operationId: exchangeToken
      summary: Exchange OAuth token
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              required: [grant_type, client_id]
              properties:
                grant_type:
                  type: string
                client_id:
                  type: string
                client_secret:
                  type: string
                refresh_token:
                  type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/oauth/token")
	assert.Equal(t, "application/x-www-form-urlencoded", endpoint.RequestContentType)
	require.Len(t, endpoint.Body, 4)
	byName := map[string]spec.Param{}
	for _, param := range endpoint.Body {
		byName[param.Name] = param
	}
	assert.True(t, byName["grant_type"].Required)
	assert.True(t, byName["client_id"].Required)
	assert.False(t, byName["client_secret"].Required)
}

// TestParseJSONPreferredOverFormUrlencoded asserts the parser still picks
// application/json when the spec offers both content types — keeping JSON-
// declared specs byte-identical and letting form-only OAuth/legacy endpoints
// surface their wire shape.
func TestParseJSONPreferredOverFormUrlencoded(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Multi Content API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items:
    post:
      operationId: createItem
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                name:
                  type: string
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        "201":
          description: created
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/items")
	assert.Equal(t, "application/json", endpoint.RequestContentType)
}

func findParsedEndpointByPath(t *testing.T, parsed *spec.APISpec, method, path string) spec.Endpoint {
	t.Helper()
	for _, resource := range parsed.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Method == method && endpoint.Path == path {
				return endpoint
			}
		}
		for _, sub := range resource.SubResources {
			for _, endpoint := range sub.Endpoints {
				if endpoint.Method == method && endpoint.Path == path {
					return endpoint
				}
			}
		}
	}
	t.Fatalf("endpoint %s %s not found", method, path)
	return spec.Endpoint{}
}

// TestParseSyncWalkerExtension pins the x-pp-sync-walker operation
// extension shape. The extension declares a hierarchical-walk dependency
// for a child endpoint (parent resource name, optional non-PK key field,
// optional explicit key param). Parsed into Endpoint.Walker.
func TestParseSyncWalkerExtension(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Walker API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /games:
    get:
      summary: List games
      responses:
        "200":
          description: ok
  /games/{game_key}/leagues:
    get:
      summary: List leagues for a game
      x-pp-sync-walker:
        parent: games
        key_field: game_key
        key_param: game_key
      parameters:
        - name: game_key
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /leagues/{league_id}/teams:
    get:
      summary: List teams (walker without key_field)
      x-pp-sync-walker:
        parent: leagues
      parameters:
        - name: league_id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	// Endpoint with full walker config.
	leagues := findParsedEndpointByPath(t, parsed, "GET", "/games/{game_key}/leagues")
	require.NotNil(t, leagues.Walker, "x-pp-sync-walker must populate Endpoint.Walker")
	assert.Equal(t, "games", leagues.Walker.Parent)
	assert.Equal(t, "game_key", leagues.Walker.KeyField)
	assert.Equal(t, "game_key", leagues.Walker.KeyParam)

	// Endpoint with only parent set.
	teams := findParsedEndpointByPath(t, parsed, "GET", "/leagues/{league_id}/teams")
	require.NotNil(t, teams.Walker)
	assert.Equal(t, "leagues", teams.Walker.Parent)
	assert.Empty(t, teams.Walker.KeyField)
	assert.Empty(t, teams.Walker.KeyParam)

	// Endpoint without the extension.
	games := findParsedEndpointByPath(t, parsed, "GET", "/games")
	assert.Nil(t, games.Walker, "endpoint without x-pp-sync-walker must have nil Walker")
}

// TestParseMarksFallbackBaseURLAsPlaceholder pins the contract used by the
// generate command to refuse shipping specs that omit `servers:` entirely.
// The parser falls back to a placeholder URL so in-memory test fixtures keep
// parsing, but the returned spec must carry BaseURLIsPlaceholder=true so
// downstream callers can detect-and-refuse instead of silently writing a
// DNS-failing config.toml.
func TestParseMarksFallbackBaseURLAsPlaceholder(t *testing.T) {
	t.Parallel()

	t.Run("no servers block sets the flag", func(t *testing.T) {
		specYAML := `openapi: "3.0.3"
info:
  title: No Servers Test
  version: "1.0"
paths:
  /thing:
    get:
      operationId: getThing
      responses:
        '200': {description: OK}
`
		parsed, err := Parse([]byte(specYAML))
		require.NoError(t, err)
		assert.True(t, parsed.BaseURLIsPlaceholder, "no-servers spec must mark BaseURL as placeholder")
		assert.Equal(t, spec.PlaceholderBaseURL, parsed.BaseURL)
	})

	t.Run("explicit top-level servers leaves the flag false", func(t *testing.T) {
		specYAML := `openapi: "3.0.3"
info:
  title: With Servers Test
  version: "1.0"
servers:
  - url: https://api.real.com
paths:
  /thing:
    get:
      operationId: getThing
      responses:
        '200': {description: OK}
`
		parsed, err := Parse([]byte(specYAML))
		require.NoError(t, err)
		assert.False(t, parsed.BaseURLIsPlaceholder, "spec with real servers must not be marked placeholder")
		assert.Equal(t, "https://api.real.com", parsed.BaseURL)
	})

	t.Run("per-operation servers leave the flag false", func(t *testing.T) {
		specYAML := `openapi: "3.0.3"
info:
  title: Per-Op Only Test
  version: "1.0"
paths:
  /thing:
    get:
      operationId: getThing
      servers:
        - url: https://api.real.com
      responses:
        '200': {description: OK}
`
		parsed, err := Parse([]byte(specYAML))
		require.NoError(t, err)
		assert.False(t, parsed.BaseURLIsPlaceholder, "spec with per-operation servers must not be marked placeholder")
		assert.Equal(t, "https://api.real.com", parsed.BaseURL)
	})
}

// TestAuthCompanionFromOpenAPI exercises the x-auth-companion extension at
// both scheme level and info level, including the scheme-wins precedence
// when both are set.
func TestAuthCompanionFromOpenAPI(t *testing.T) {
	t.Run("scheme-level x-auth-companion propagates to Auth", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: guestsession
      x-auth-companion:
        login_url: https://www.example.com/account/login
        login_complete_selector: "a[href*=signout]"
        jwt_carrier_cookie: guestsession
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "https://www.example.com/account/login", parsed.Auth.LoginURL)
		assert.Equal(t, "a[href*=signout]", parsed.Auth.LoginCompleteSelector)
		assert.Equal(t, "guestsession", parsed.Auth.JWTCarrierCookie)
		assert.True(t, parsed.Auth.HasCompanionHints())
	})

	t.Run("info-level x-auth-companion fills when scheme omits it", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  x-auth-companion:
    login_url: https://www.example.com/account/login
    jwt_carrier_cookie: guestsession
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: guestsession
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "https://www.example.com/account/login", parsed.Auth.LoginURL)
		assert.Equal(t, "guestsession", parsed.Auth.JWTCarrierCookie)
		assert.True(t, parsed.Auth.HasCompanionHints())
	})

	t.Run("scheme-level beats info-level when both are set", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  x-auth-companion:
    login_url: https://info-level.example.com/login
    jwt_carrier_cookie: info_level_cookie
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: scheme_cookie
      x-auth-companion:
        login_url: https://scheme-level.example.com/login
        jwt_carrier_cookie: scheme_cookie
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "https://scheme-level.example.com/login", parsed.Auth.LoginURL)
		assert.Equal(t, "scheme_cookie", parsed.Auth.JWTCarrierCookie)
	})

	t.Run("info-level fills missing fields when scheme partially declares", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
  x-auth-companion:
    login_url: https://info-level.example.com/login
    login_complete_selector: "a.signout"
    jwt_carrier_cookie: info_cookie
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: scheme_cookie
      x-auth-companion:
        login_url: https://scheme-level.example.com/login
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)
		assert.Equal(t, "https://scheme-level.example.com/login", parsed.Auth.LoginURL, "scheme login_url wins")
		assert.Equal(t, "a.signout", parsed.Auth.LoginCompleteSelector, "info fills missing selector")
		assert.Equal(t, "info_cookie", parsed.Auth.JWTCarrierCookie, "info fills missing carrier")
	})

	t.Run("malformed x-auth-companion at scheme level warns and skips", func(t *testing.T) {
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: guestsession
      x-auth-companion: "not-an-object"
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
		warnings := captureWarnings(t, func() {
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)
			assert.Empty(t, parsed.Auth.LoginURL)
		})
		assert.Contains(t, warnings, "x-auth-companion")
	})
}

// TestAuthCompanionInternalYAMLRoundTripEqualsOpenAPI asserts that an
// equivalent OpenAPI spec and internal YAML spec produce the same Auth
// fields for the companion hints. This is the contract that
// docs/SPEC-EXTENSIONS.md promises: x-auth-companion at scheme level maps
// 1:1 to the internal `auth:` block fields.
func TestAuthCompanionInternalYAMLRoundTripEqualsOpenAPI(t *testing.T) {
	openapiYAML := []byte(`openapi: "3.0.3"
info:
  title: Example
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: guestsession
      x-auth-companion:
        login_url: https://www.example.com/account/login
        login_complete_selector: "a[href*=signout]"
        jwt_carrier_cookie: guestsession
paths:
  /ping:
    get:
      responses:
        "200": { description: OK }
`)
	parsed, err := Parse(openapiYAML)
	require.NoError(t, err)

	// The internal-YAML reference shape, hand-built from the same hints.
	wantLoginURL := "https://www.example.com/account/login"
	wantSelector := "a[href*=signout]"
	wantCarrier := "guestsession"

	assert.Equal(t, wantLoginURL, parsed.Auth.LoginURL)
	assert.Equal(t, wantSelector, parsed.Auth.LoginCompleteSelector)
	assert.Equal(t, wantCarrier, parsed.Auth.JWTCarrierCookie)
}

// TestParseTenantEnvVarExtension: when info.x-tenant-env-var is set, the
// parser registers "tenant" as an EndpointTemplateVar with the declared
// env var as the override so the profiler can include
// /tenant/{tenant}/<resource> paths in flat sync and downstream emitters
// resolve the placeholder against the real env name.
func TestParseTenantEnvVarExtension(t *testing.T) {
	t.Parallel()

	t.Run("info.x-tenant-env-var registers tenant template var + override", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: ServiceTitan CRM
  version: 1.0.0
  x-tenant-env-var: ST_TENANT_ID
servers:
  - url: https://api.servicetitan.io
paths:
  /tenant/{tenant}/customers:
    get:
      summary: List customers
      parameters:
        - name: tenant
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"tenant"}, parsed.EndpointTemplateVars)
		assert.Equal(t, map[string]string{"tenant": "ST_TENANT_ID"}, parsed.EndpointTemplateEnvOverrides)
		assert.Equal(t, "ST_TENANT_ID", parsed.EndpointTemplateEnvName("tenant"))
	})

	t.Run("absent extension leaves both fields empty", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Single Tenant API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Empty(t, parsed.EndpointTemplateVars)
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)
	})

	t.Run("blank extension value is treated as absent", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Bad Annotation
  version: 1.0.0
  x-tenant-env-var: "   "
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Empty(t, parsed.EndpointTemplateVars, "whitespace-only extension must not register a template var")
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)
	})
}

// TestParsePathTemplateEnvVarsExtension: the generic x-path-template-env-vars
// extension is the map-shaped successor to x-tenant-env-var. Each entry binds
// a path placeholder to an env-var override (`env` field, parallel to the
// tenant flow) or a build-time literal substitution (`default` field, which
// bakes the value into operation paths at generation time and drops the
// matching path parameter). When both are set, default wins.
func TestParsePathTemplateEnvVarsExtension(t *testing.T) {
	t.Parallel()

	t.Run("env entry registers template var + override for arbitrary placeholder", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Atlassian Workspace API
  version: 1.0.0
  x-path-template-env-vars:
    workspace:
      env: ATLASSIAN_WORKSPACE
servers:
  - url: https://api.atlassian.com/{workspace}
paths:
  /issues:
    get:
      operationId: listIssues
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"workspace"}, parsed.EndpointTemplateVars)
		assert.Equal(t, map[string]string{"workspace": "ATLASSIAN_WORKSPACE"}, parsed.EndpointTemplateEnvOverrides)
		assert.Equal(t, "ATLASSIAN_WORKSPACE", parsed.EndpointTemplateEnvName("workspace"))
		assert.Empty(t, parsed.EndpointPathParamDefaults, "env-only entries must not populate path-param defaults")
	})

	t.Run("default entry bakes literal into path and drops the matching param", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Gmail Users API
  version: 1.0.0
  x-path-template-env-vars:
    userId:
      default: me
servers:
  - url: https://gmail.googleapis.com
paths:
  /users/{userId}/messages:
    get:
      operationId: listMessages
      parameters:
        - name: userId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"userId": "me"}, parsed.EndpointPathParamDefaults)
		assert.Empty(t, parsed.EndpointTemplateVars, "default-only entries must not register a runtime template var")
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)

		var resolvedPath string
		var resolvedParams []spec.Param
		var walk func(map[string]spec.Resource)
		walk = func(rs map[string]spec.Resource) {
			for _, r := range rs {
				for _, e := range r.Endpoints {
					if e.Method == "GET" && (e.Path == "/users/me/messages" || e.Path == "/users/{userId}/messages") {
						resolvedPath = e.Path
						resolvedParams = e.Params
					}
				}
				if len(r.SubResources) > 0 {
					walk(r.SubResources)
				}
			}
		}
		walk(parsed.Resources)
		require.NotEmpty(t, resolvedPath, "list-messages endpoint not found in parsed resources")
		assert.Equal(t, "/users/me/messages", resolvedPath, "path placeholder must be baked into emitted path")
		for _, p := range resolvedParams {
			assert.NotEqual(t, "userId", p.Name, "resolved path param must be dropped from endpoint Params")
		}
	})

	t.Run("default wins when both default and env are set on the same entry", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Gmail Users API
  version: 1.0.0
  x-path-template-env-vars:
    userId:
      env: GMAIL_USER_ID
      default: me
servers:
  - url: https://gmail.googleapis.com
paths:
  /users/{userId}/messages:
    get:
      operationId: listMessages
      parameters:
        - name: userId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"userId": "me"}, parsed.EndpointPathParamDefaults)
		assert.Empty(t, parsed.EndpointTemplateVars, "when default is set, env is ignored and no runtime template var is registered")
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)
	})

	t.Run("coexists with x-tenant-env-var: both extensions register independently", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Mixed Extensions API
  version: 1.0.0
  x-tenant-env-var: ST_TENANT_ID
  x-path-template-env-vars:
    workspace:
      env: ATLASSIAN_WORKSPACE
servers:
  - url: https://api.example.com/{workspace}
paths:
  /tenant/{tenant}/items:
    get:
      operationId: listItems
      parameters:
        - name: tenant
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"tenant", "workspace"}, parsed.EndpointTemplateVars)
		assert.Equal(t, map[string]string{
			"tenant":    "ST_TENANT_ID",
			"workspace": "ATLASSIAN_WORKSPACE",
		}, parsed.EndpointTemplateEnvOverrides)
		assert.Empty(t, parsed.EndpointPathParamDefaults)
	})

	t.Run("absent extension leaves path-param defaults empty", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Plain API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Empty(t, parsed.EndpointPathParamDefaults)
		assert.Empty(t, parsed.EndpointTemplateVars)
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)
	})

	t.Run("default drop preserves a same-named non-path param on the same operation", func(t *testing.T) {
		// A query (or body) param that shares its name with a substituted
		// path placeholder must survive the path-param drop; only the actual
		// path parameter is resolved away by the default.
		data := []byte(`
openapi: 3.0.3
info:
  title: Gmail Users API
  version: 1.0.0
  x-path-template-env-vars:
    userId:
      default: me
servers:
  - url: https://gmail.googleapis.com
paths:
  /users/{userId}/messages:
    get:
      operationId: listMessages
      parameters:
        - name: userId
          in: path
          required: true
          schema: {type: string}
        - name: userId
          in: query
          required: false
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"userId": "me"}, parsed.EndpointPathParamDefaults)

		var resolvedPath string
		var resolvedParams []spec.Param
		var walk func(map[string]spec.Resource)
		walk = func(rs map[string]spec.Resource) {
			for _, r := range rs {
				for _, e := range r.Endpoints {
					if e.Method == "GET" && e.Path == "/users/me/messages" {
						resolvedPath = e.Path
						resolvedParams = e.Params
					}
				}
				if len(r.SubResources) > 0 {
					walk(r.SubResources)
				}
			}
		}
		walk(parsed.Resources)
		require.NotEmpty(t, resolvedPath, "list-messages endpoint not found in parsed resources")

		var sawPath, sawQuery bool
		for _, p := range resolvedParams {
			if p.Name != "userId" {
				continue
			}
			if p.PathParam {
				sawPath = true
			} else {
				sawQuery = true
			}
		}
		assert.False(t, sawPath, "resolved path param must be dropped from endpoint Params")
		assert.True(t, sawQuery, "same-named query param must be preserved")
	})

	t.Run("default in x-path-template-env-vars overrides a conflicting x-tenant-env-var entry", func(t *testing.T) {
		// When both extensions target the same placeholder, default wins
		// and the runtime entry must not linger in EndpointTemplateVars or
		// EndpointTemplateEnvOverrides — otherwise downstream generators
		// emit dead config / URL-substitution code for a placeholder that
		// no longer exists in any path.
		data := []byte(`
openapi: 3.0.3
info:
  title: Mixed Extensions API
  version: 1.0.0
  x-tenant-env-var: ST_TENANT_ID
  x-path-template-env-vars:
    tenant:
      default: acme
servers:
  - url: https://api.example.com
paths:
  /tenant/{tenant}/items:
    get:
      operationId: listItems
      parameters:
        - name: tenant
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"tenant": "acme"}, parsed.EndpointPathParamDefaults)
		assert.Empty(t, parsed.EndpointTemplateVars, "default must scrub the stale tenant entry from template vars")
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides, "default must scrub the stale tenant entry from env overrides")
	})

	t.Run("whitespace-only env and default values are treated as absent", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Whitespace Extensions
  version: 1.0.0
  x-path-template-env-vars:
    workspace:
      env: "   "
    userId:
      default: "   "
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Empty(t, parsed.EndpointTemplateVars, "whitespace-only env must not register a template var")
		assert.Empty(t, parsed.EndpointTemplateEnvOverrides)
		assert.Empty(t, parsed.EndpointPathParamDefaults, "whitespace-only default must not register a path-param default")
	})
}

// TestParseServerURLVariablesAsTemplateVars covers the multi-tenant SaaS
// case: when servers[0].url declares a `{var}` placeholder backed by a
// Variables block, the parser must preserve the placeholder in BaseURL,
// register the variable as an EndpointTemplateVar, and capture its
// `default:` value so the generator can fall back at runtime when the
// user's env var is unset. Without this, the generator bakes the default
// into BaseURL at generate time and the printed CLI DNS-fails on every
// call against any tenant other than the spec author's example.
func TestParseServerURLVariablesAsTemplateVars(t *testing.T) {
	t.Parallel()

	t.Run("single placeholder with default registers template var", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Freshservice
  version: 1.0.0
servers:
  - url: "https://{domain}/api/v2"
    variables:
      domain:
        default: "yourcompany.freshservice.com"
paths:
  /tickets:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://{domain}/api/v2", parsed.BaseURL,
			"placeholder must survive to BaseURL so the runtime can substitute env-var values")
		assert.Equal(t, []string{"domain"}, parsed.EndpointTemplateVars)
		assert.Equal(t, "yourcompany.freshservice.com", parsed.EndpointTemplateVarDefaults["domain"],
			"variable default must be captured for runtime fallback")
		assert.Equal(t, "FRESHSERVICE_DOMAIN", parsed.EndpointTemplateEnvName("domain"),
			"env var follows the conventional <APINAME>_<UPPER_PLACEHOLDER> rule")
	})

	t.Run("static server URL leaves new fields empty", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Static API
  version: 1.0.0
servers:
  - url: https://api.example.com/v1
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com/v1", parsed.BaseURL)
		assert.Empty(t, parsed.EndpointTemplateVars)
		assert.Empty(t, parsed.EndpointTemplateVarDefaults)
	})

	t.Run("variable without explicit Variables entry strips legacy placeholder", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Dangling Placeholder API
  version: 1.0.0
servers:
  - url: "https://{foo}.example.com"
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://.example.com", parsed.BaseURL,
			"dangling placeholders without Variables entries strip away (legacy behavior)")
		assert.Empty(t, parsed.EndpointTemplateVars)
		assert.Empty(t, parsed.EndpointTemplateVarDefaults)
	})

	t.Run("multiple placeholders preserve order and defaults", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Two Var API
  version: 1.0.0
servers:
  - url: "https://{tenant}.example.com/api/{version}"
    variables:
      tenant:
        default: "demo"
      version:
        default: "v1"
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://{tenant}.example.com/api/{version}", parsed.BaseURL)
		assert.Equal(t, []string{"tenant", "version"}, parsed.EndpointTemplateVars,
			"placeholders must be ordered by left-to-right appearance in the URL")
		assert.Equal(t, "demo", parsed.EndpointTemplateVarDefaults["tenant"])
		assert.Equal(t, "v1", parsed.EndpointTemplateVarDefaults["version"])
	})

	t.Run("placeholder with empty default registers var but no default entry", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Empty Default API
  version: 1.0.0
servers:
  - url: "https://{host}/api"
    variables:
      host:
        default: ""
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://{host}/api", parsed.BaseURL,
			"placeholder still survives so the env var is required at runtime")
		assert.Equal(t, []string{"host"}, parsed.EndpointTemplateVars)
		assert.Empty(t, parsed.EndpointTemplateVarDefaults,
			"empty defaults must not pollute the defaults map — env var becomes the only fallback")
	})

	t.Run("x-tenant-env-var override coexists with server-URL placeholder", func(t *testing.T) {
		data := []byte(`
openapi: 3.0.3
info:
  title: Combo API
  version: 1.0.0
  x-tenant-env-var: COMBO_TENANT_ID
servers:
  - url: "https://{domain}/api/v2"
    variables:
      domain:
        default: "demo.example.com"
paths:
  /tenant/{tenant}/items:
    get:
      parameters:
        - name: tenant
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"tenant", "domain"}, parsed.EndpointTemplateVars,
			"extension-declared placeholders come first, server-URL placeholders follow")
		assert.Equal(t, "COMBO_TENANT_ID", parsed.EndpointTemplateEnvName("tenant"))
		assert.Equal(t, "COMBO_DOMAIN", parsed.EndpointTemplateEnvName("domain"))
		assert.Equal(t, "demo.example.com", parsed.EndpointTemplateVarDefaults["domain"])
		assert.Empty(t, parsed.EndpointTemplateVarDefaults["tenant"],
			"path-positional templates from x-tenant-env-var have no spec-level default")
	})

	t.Run("dangling placeholder after runtime placeholder still gets stripped", func(t *testing.T) {
		// `{api_version}` has no Variables entry — it must strip away rather
		// than survive into BaseURL. The earlier `{domain}` is a runtime
		// placeholder; the strip loop must walk past it (cursor advance),
		// not terminate, or any later dangling marker leaks into every URL.
		data := []byte(`
openapi: 3.0.3
info:
  title: Mixed Placeholder API
  version: 1.0.0
servers:
  - url: "https://{domain}/api/{api_version}"
    variables:
      domain:
        default: "demo.example.com"
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "https://{domain}/api", parsed.BaseURL,
			"`{domain}` preserved for runtime; `{api_version}` stripped because it has no Variables entry (trailing slash trimmed)")
		assert.Equal(t, []string{"domain"}, parsed.EndpointTemplateVars)
		assert.NotContains(t, parsed.BaseURL, "{api_version}",
			"dangling placeholders after a runtime placeholder must not survive into BaseURL")
	})

	t.Run("default with shell-sensitive characters is captured verbatim", func(t *testing.T) {
		// Defaults flow through to the generated config.go as Go string
		// literals; the generator must escape them so a default containing
		// `"`, `\`, or a newline cannot break the printed CLI's compile.
		// Parser-side it stays verbatim — escape is the emit-time concern.
		data := []byte(`
openapi: 3.0.3
info:
  title: Quoted Default API
  version: 1.0.0
servers:
  - url: "https://{host}/api"
    variables:
      host:
        default: "a\"b\\c"
paths:
  /items:
    get:
      responses:
        "200": {description: ok}
`)
		parsed, err := Parse(data)
		require.NoError(t, err)
		assert.Equal(t, `a"b\c`, parsed.EndpointTemplateVarDefaults["host"],
			"default captured verbatim; generator must use %q to escape at emit time")
	})
}

// TestDetectPaginationPreservesParameterCase guards #1353 — Google APIs
// declare `pageSize` (camelCase) and reject the lowercased `pagesize`
// the detector previously stored. The detector matches case-insensitively
// but must store the parameter name as it appears in the spec.
func TestDetectPaginationPreservesParameterCase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paramName string
		wantLimit string
	}{
		{"google camelCase pageSize", "pageSize", "pageSize"},
		{"snake_case page_size", "page_size", "page_size"},
		{"plain lowercase limit", "limit", "limit"},
		{"mixed-case maxResults", "maxResults", "maxResults"},
		{"per_page", "per_page", "per_page"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pag := detectPagination([]spec.Param{{Name: tc.paramName}}, nil)
			require.NotNil(t, pag, "detector should classify %q as a paginator", tc.paramName)
			assert.Equal(t, tc.wantLimit, pag.LimitParam)
		})
	}
}

func TestDetectPaginationPreservesCursorParamCase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paramName string
		wantParam string
		wantType  string
	}{
		{"google camelCase pageToken", "pageToken", "pageToken", "page_token"},
		{"snake_case page_token", "page_token", "page_token", "page_token"},
		{"plain after", "after", "after", "cursor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pag := detectPagination([]spec.Param{{Name: tc.paramName}}, nil)
			require.NotNil(t, pag, "detector should classify %q as a cursor paginator", tc.paramName)
			assert.Equal(t, tc.wantParam, pag.CursorParam)
			assert.Equal(t, tc.wantType, pag.Type)
		})
	}
}

// TestDetectPaginationRecognizesPageIntCursor guards #1296: APIs that
// paginate by integer ?page=N (Freshworks family, Atlassian, HubSpot,
// etc.) used to fall through to the "after" cursor default. Detection
// now classifies a `page`/`pageNumber`/`page[number]` request param as
// a Type="page" paginator so the sync template's page-int fallback
// can advance numerically.
func TestDetectPaginationRecognizesPageIntCursor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paramName string
		wantParam string
	}{
		{"plain page", "page", "page"},
		{"snake_case page_number", "page_number", "page_number"},
		{"camelCase pageNumber", "pageNumber", "pageNumber"},
		{"json:api page[number]", "page[number]", "page[number]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pag := detectPagination([]spec.Param{{Name: tc.paramName}, {Name: "per_page"}}, nil)
			require.NotNil(t, pag, "detector should classify %q as a paginator", tc.paramName)
			assert.Equal(t, tc.wantParam, pag.CursorParam)
			assert.Equal(t, "page", pag.Type)
			assert.Equal(t, "per_page", pag.LimitParam)
		})
	}
}

func TestDetectPaginationRecognizesNestedNumericNextPage(t *testing.T) {
	t.Parallel()

	description := "OK"
	responses := openapi3.NewResponses()
	responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: &description,
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"items": &openapi3.SchemaRef{Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
						}},
						"meta": &openapi3.SchemaRef{Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: openapi3.Schemas{
								"nextPage": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
							},
						}},
					},
				}},
			},
		},
	}})
	op := &openapi3.Operation{Responses: responses}

	pag := detectPagination([]spec.Param{{Name: "page"}, {Name: "limit"}}, op)
	require.NotNil(t, pag)
	assert.Equal(t, "page", pag.CursorParam)
	assert.Equal(t, "page", pag.Type)
	assert.Equal(t, "limit", pag.LimitParam)
	assert.Equal(t, "meta.nextPage", pag.NextCursorPath)
}

func TestDetectPaginationDoesNotWireNestedNextPageWithoutRequestCursor(t *testing.T) {
	t.Parallel()

	description := "OK"
	responses := openapi3.NewResponses()
	responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: &description,
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"items": &openapi3.SchemaRef{Value: &openapi3.Schema{
							Type:  &openapi3.Types{"array"},
							Items: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
						}},
						"meta": &openapi3.SchemaRef{Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: openapi3.Schemas{
								"nextPage": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
							},
						}},
					},
				}},
			},
		},
	}})
	op := &openapi3.Operation{Responses: responses}

	pag := detectPagination([]spec.Param{{Name: "limit"}}, op)
	require.NotNil(t, pag)
	assert.Equal(t, "limit", pag.LimitParam)
	assert.Empty(t, pag.CursorParam)
	assert.Empty(t, pag.NextCursorPath)
}

// TestDetectPaginationCursorBeatsPage guards mixed-form specs: when an
// API declares both `cursor` and `page` (rare but real), cursor must
// win so existing cursor-based sync loops stay on the body-cursor
// extraction path. Regression guard for #1296.
func TestDetectPaginationCursorBeatsPage(t *testing.T) {
	t.Parallel()

	pag := detectPagination([]spec.Param{
		{Name: "cursor"}, {Name: "page"}, {Name: "limit"},
	}, nil)
	require.NotNil(t, pag)
	assert.Equal(t, "cursor", pag.CursorParam, "cursor must win over page when both declared")
	assert.Equal(t, "cursor", pag.Type)
	assert.Equal(t, "limit", pag.LimitParam)
}

// TestDetectPaginationOffsetBeatsPage guards offset-based APIs (Atlassian
// older endpoints, etc.) against the new page branch.
func TestDetectPaginationOffsetBeatsPage(t *testing.T) {
	t.Parallel()

	pag := detectPagination([]spec.Param{
		{Name: "offset"}, {Name: "page"}, {Name: "limit"},
	}, nil)
	require.NotNil(t, pag)
	assert.Equal(t, "offset", pag.CursorParam)
	assert.Equal(t, "offset", pag.Type)
}
