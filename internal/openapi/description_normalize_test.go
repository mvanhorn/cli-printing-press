package openapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseHandlesObjectDescription covers issue #275 F-4. DigitalOcean's
// public OpenAPI spec uses `description: { $ref: "description.yml#/foo" }`
// at the tag level — pointing the description at an external file — instead
// of the string the OpenAPI 3.0 spec mandates. kin-openapi's `Tag.Description`
// is `string`, so the YAML decode fails with
// `cannot unmarshal object into field Tag.description of type string` and
// the spec refuses to load.
//
// Pre-process the spec data to flatten any non-scalar `description` value
// to an empty string before kin-openapi sees it. Descriptions are
// documentation, not load-bearing for code generation; losing the text is
// the right trade-off for keeping the spec parseable.
func TestParseHandlesObjectDescription(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: "3.0.0"
info:
  title: doapi
  version: "0.1.0"
servers:
  - url: https://api.example.com
tags:
  - name: Account
    description:
      $ref: "description.yml#/account"
  - name: Droplets
    description: Manage compute instances.
paths:
  /accounts:
    get:
      summary: List accounts
      responses:
        '200':
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err, "spec with object-shaped tag description must parse")
	assert.Equal(t, "doapi", parsed.Name,
		"normalization must not corrupt unrelated fields")
	assert.NotEmpty(t, parsed.Resources,
		"endpoints under the spec must still be discovered")
}

// TestParseDoesNotFlattenPropertyNamedDescription guards against the
// flatten pass mistaking a schema property literally named "description" for
// the structural OpenAPI description field. Stytch's spec has the shape
// `properties: { description: { type: string } }`; if the flatten replaced
// the property's schema with an empty string, kin-openapi would reject the
// spec with `cannot unmarshal string into field Schema.properties of type
// openapi3.Schema`.
func TestParseDoesNotFlattenPropertyNamedDescription(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: "3.0.0"
info:
  title: roleapi
  version: "0.1.0"
servers:
  - url: https://api.example.com
paths:
  /roles:
    get:
      summary: List roles
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Role'
components:
  schemas:
    Role:
      type: object
      properties:
        role_id:
          type: string
        description:
          type: string
        permissions:
          type: array
          items:
            type: string
      required:
        - role_id
        - description
        - permissions
`)

	parsed, err := Parse(data)
	require.NoError(t, err, "schema with a property named 'description' must parse")
	require.Contains(t, parsed.Types, "Role")
	role := parsed.Types["Role"]

	var hasDescriptionField bool
	for _, f := range role.Fields {
		if f.Name == "description" {
			hasDescriptionField = true
			assert.Equal(t, "string", f.Type,
				"property named 'description' must keep its declared schema type")
		}
	}
	assert.True(t, hasDescriptionField,
		"the description property must survive normalization")
}

// TestParseHandlesObjectDescriptionAtMultipleLevels guards against vendors
// applying the object-shaped description pattern beyond tags. Walk-and-flatten
// must operate on every description in the tree, not just tag-level ones.
func TestParseHandlesObjectDescriptionAtMultipleLevels(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: "3.0.0"
info:
  title: nestedapi
  version: "0.1.0"
  description:
    $ref: "intro.yml#/main"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      summary: List
      description:
        $ref: "operation.yml#/widgets"
      responses:
        '200':
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err, "spec with object-shaped descriptions at info and operation levels must parse")
	assert.Equal(t, "nestedapi", parsed.Name)
}

func TestNormalizeSpecDataConvertsSchemaExamplesMap(t *testing.T) {
	t.Parallel()

	data := []byte(`
openapi: "3.0.0"
info:
  title: Legacy Examples
  version: "0.1.0"
paths:
  /actions:
    post:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                actionParameters:
                  type: object
                  examples:
                    first:
                      value: one
                    second:
                      value: two
            examples:
              named:
                value:
                  actionParameters:
                    value: preserved
      responses:
        '200':
          description: ok
components:
  schemas:
    Action:
      type: object
      examples:
        named:
          actionParameters:
            value: preserved
    OnlyExamples:
      examples:
        first:
          value: structurally schema
`)

	normalized, err := normalizeSpecData(data)
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(normalized, &root))

	actionParameters := root["paths"].(map[string]any)["/actions"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["actionParameters"].(map[string]any)
	require.IsType(t, []any{}, actionParameters["examples"])
	assert.Len(t, actionParameters["examples"], 2)

	mediaType := root["paths"].(map[string]any)["/actions"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
	require.IsType(t, map[string]any{}, mediaType["examples"])

	component := root["components"].(map[string]any)["schemas"].(map[string]any)["Action"].(map[string]any)
	require.IsType(t, []any{}, component["examples"])
	assert.Len(t, component["examples"], 1)

	onlyExamples := root["components"].(map[string]any)["schemas"].(map[string]any)["OnlyExamples"].(map[string]any)
	require.IsType(t, []any{}, onlyExamples["examples"])
	assert.Len(t, onlyExamples["examples"], 1)

	_, err = Parse(data)
	require.NoError(t, err, "schema examples in map form must parse after normalization")
}
