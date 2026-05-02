package openapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stringSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{openapi3.TypeString}}}
}

func intSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{openapi3.TypeInteger}}}
}

func objectSchema(props map[string]*openapi3.SchemaRef) *openapi3.Schema {
	return &openapi3.Schema{
		Type:       &openapi3.Types{openapi3.TypeObject},
		Properties: props,
	}
}

// TestParseJSONAPI_FlattensAttributes verifies the parser hoists the
// `attributes` sub-object of a JSON:API Resource Object to the top of the
// generated TypeDef. Without this, every JSON:API spec produces TypeDefs
// where `attributes` is one opaque field and the real columns/flags
// (email, name, etc.) are unreachable to downstream consumers.
//
// JSON:API Resource Object discriminator: schema has all of
// `type` (string), `id` (string), and `attributes` (object). Stripe and
// HubSpot do not have this combination, so the flattening must be a no-op
// for them — guarded by detection, not applied universally.
func TestParseJSONAPI_FlattensAttributes(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "jsonapi-petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.NotEmpty(t, parsed.Types)
	require.Contains(t, parsed.Types, "Pet", "Pet TypeDef must be emitted")

	pet := parsed.Types["Pet"]
	fieldNames := map[string]string{}
	for _, f := range pet.Fields {
		fieldNames[f.Name] = f.Type
	}

	assert.Contains(t, fieldNames, "id", "primary key id must be preserved")
	assert.Contains(t, fieldNames, "name", "attributes.name must be hoisted to top-level")
	assert.Contains(t, fieldNames, "species", "attributes.species must be hoisted to top-level")
	assert.Contains(t, fieldNames, "age", "attributes.age must be hoisted to top-level")

	assert.NotContains(t, fieldNames, "type", "JSON:API discriminator `type` must be dropped")
	assert.NotContains(t, fieldNames, "attributes", "envelope wrapper `attributes` must not survive flattening")
}

func TestParseJSONAPI_DetectsBracketedCursorPagination(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "jsonapi-petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	pets := parsed.Resources["pets"]
	list := pets.Endpoints["list"]
	require.NotNil(t, list.Pagination, "list endpoint must detect JSON:API page[cursor] pagination")
	assert.Equal(t, "cursor", list.Pagination.Type)
	assert.Equal(t, "page[cursor]", list.Pagination.CursorParam)
	assert.Equal(t, "page[size]", list.Pagination.LimitParam)
}

func TestParseJSONAPI_MapsAPIKeyPrefixExtension(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "jsonapi-petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, "Authorization", parsed.Auth.Header)
	assert.Equal(t, "header", parsed.Auth.In)
	assert.Equal(t, "Token-Prefix {token}", parsed.Auth.Format)
}

// TestParseJSONAPI_LeavesNonJSONAPISpecsAlone confirms the flattening is
// gated. Existing OpenAPI fixtures (Stripe shape, HubSpot shape) must
// produce byte-identical TypeDefs to before. petstore.yaml uses a flat
// shape; if its Pet TypeDef changes after this commit, the gate is wrong.
func TestParseJSONAPI_LeavesNonJSONAPISpecsAlone(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.Contains(t, parsed.Types, "Pet")

	pet := parsed.Types["Pet"]
	fieldNames := map[string]bool{}
	for _, f := range pet.Fields {
		fieldNames[f.Name] = true
	}

	// petstore's Pet has top-level name, status, photoUrls — no JSON:API
	// envelope. These must remain present and unmoved.
	assert.True(t, fieldNames["name"], "petstore Pet.name must be present (regression check)")
	assert.True(t, fieldNames["id"], "petstore Pet.id must be present (regression check)")
}

// TestIsJSONAPIResource_Discriminator pins the gating logic. A schema is a
// JSON:API Resource Object only when type+id+attributes are all present
// AND have the right shape. Adjacent shapes (only id+attributes, only
// type+id) must NOT match — they show up in vanilla REST specs.
func TestIsJSONAPIResource_Discriminator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		schema   *openapi3.Schema
		expected bool
	}{
		{
			name: "canonical JSON:API resource object matches",
			schema: objectSchema(map[string]*openapi3.SchemaRef{
				"type": stringSchema(),
				"id":   stringSchema(),
				"attributes": {Value: objectSchema(map[string]*openapi3.SchemaRef{
					"foo": stringSchema(),
				})},
			}),
			expected: true,
		},
		{
			name: "missing attributes does not match",
			schema: objectSchema(map[string]*openapi3.SchemaRef{
				"type": stringSchema(),
				"id":   stringSchema(),
				"foo":  stringSchema(),
			}),
			expected: false,
		},
		{
			name: "non-object attributes does not match",
			schema: objectSchema(map[string]*openapi3.SchemaRef{
				"type":       stringSchema(),
				"id":         stringSchema(),
				"attributes": stringSchema(),
			}),
			expected: false,
		},
		{
			name: "non-string id does not match",
			schema: objectSchema(map[string]*openapi3.SchemaRef{
				"type":       stringSchema(),
				"id":         intSchema(),
				"attributes": {Value: objectSchema(nil)},
			}),
			expected: false,
		},
		{
			name: "missing type does not match",
			schema: objectSchema(map[string]*openapi3.SchemaRef{
				"id": stringSchema(),
				"attributes": {Value: objectSchema(map[string]*openapi3.SchemaRef{
					"foo": stringSchema(),
				})},
			}),
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isJSONAPIResourceSchema(tc.schema)
			assert.Equal(t, tc.expected, got)
		})
	}
}
