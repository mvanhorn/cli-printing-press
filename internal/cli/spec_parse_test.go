package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const internalYAMLTypeProse = `name: typeprose
description: Payments API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/typeprose-pp-cli/config.toml
resources:
  payments:
    description: Manage payments
    endpoints:
      list:
        method: GET
        path: /payments
        description: List payments
        params:
          - name: payment_type
            type: string
            description: Free-text payment type label. The accepted type Query and scalar value set is undocumented.
`

const internalYAMLTypeProseBroken = `name: typeprose
description: Payments API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
resources:
  payments:
    endpoints:
      list:
        method: GET
        path: /payments
        params:
          - name: payment_type
            description: Free-text payment type label. The accepted type Query and scalar value set is undocumented.
resources:
  other:
    endpoints:
      list:
        method: GET
        path: /other
`

func TestParseSpecBytesInternalYAMLWithTypeProse(t *testing.T) {
	t.Parallel()

	parsed, err := parseSpecBytes("internal-spec.yaml", []byte(internalYAMLTypeProse), openapi.ParseOptions{})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.Equal(t, "typeprose", parsed.Name)
	require.Contains(t, parsed.Resources, "payments")
	assert.Equal(t, "Free-text payment type label. The accepted type Query and scalar value set is undocumented.",
		parsed.Resources["payments"].Endpoints["list"].Params[0].Description)
}

func TestParseSpecBytesInternalYAMLStructuralErrorNotGraphQL(t *testing.T) {
	t.Parallel()

	_, err := parseSpecBytes("internal-spec.yaml", []byte(internalYAMLTypeProseBroken), openapi.ParseOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec structural error")
	assert.Contains(t, err.Error(), "duplicate top-level key")
	assert.NotContains(t, err.Error(), "GraphQL")
	assert.NotContains(t, err.Error(), "no GraphQL root operation types found")
}

func TestParseSpecBytesOpenAPIErrorNotGraphQL(t *testing.T) {
	t.Parallel()

	broken := []byte(`openapi: "3.0.3"
info:
  title: Broken Payments
  description: Free-text payment type label mentioning type Query
paths: {}
`)
	_, err := parseSpecBytes("openapi.yaml", broken, openapi.ParseOptions{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no GraphQL root operation types found")
}

func TestParseSpecBytesFlowStyleResourcesWithTypeProse(t *testing.T) {
	t.Parallel()

	data := []byte(`name: flowprose
description: |
  type Query {
    ignored: String
  }
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
resources: {payments: {description: Manage payments, endpoints: {list: {method: GET, path: /payments}}}}
`)
	parsed, err := parseSpecBytes("internal-spec.yaml", data, openapi.ParseOptions{})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.Equal(t, "flowprose", parsed.Name)
	require.Contains(t, parsed.Resources, "payments")
}

func TestParseSpecBytesGraphQLFieldsWithIndentedCloser(t *testing.T) {
	t.Parallel()

	sdl := []byte("type Query {\nname: String\nresources: [Widget!]!\nwidget(id: ID!): Widget!\n  }\n\ntype Widget {\n  id: ID!\n  name: String!\n}\n")
	parsed, err := parseSpecBytes("schema.graphql", sdl, openapi.ParseOptions{})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.NotEmpty(t, parsed.GraphQLEndpointPath)
}

func TestParseSpecBytesGraphQLFieldsNamedNameAndResources(t *testing.T) {
	t.Parallel()

	sdl := []byte("type Query {\nname: String\nresources: [Widget!]!\nwidget(id: ID!): Widget!\n}\n\ntype Widget {\n  id: ID!\n  name: String!\n}\n")
	parsed, err := parseSpecBytes("schema.graphql", sdl, openapi.ParseOptions{})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.NotEmpty(t, parsed.GraphQLEndpointPath)
	assert.NotContains(t, parsed.Resources, "payments")
}

func TestParseSpecBytesGraphQLSDLStillParses(t *testing.T) {
	t.Parallel()

	sdl := []byte("type Query {\n  widget(id: ID!): Widget!\n}\n\ntype Widget {\n  id: ID!\n  name: String!\n}\n")
	parsed, err := parseSpecBytes("schema.graphql", sdl, openapi.ParseOptions{})
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.NotEmpty(t, parsed.GraphQLEndpointPath)
}

func TestGenerateCmdAcceptsInternalYAMLTypeProse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "internal-spec.yaml")
	outputDir := filepath.Join(dir, "typeprose")
	require.NoError(t, os.WriteFile(specPath, []byte(internalYAMLTypeProse), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
	})
	require.NoError(t, cmd.Execute())
	assert.DirExists(t, outputDir)
}

func TestGenerateCmdReportsInternalYAMLErrorNotGraphQL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "internal-spec.yaml")
	outputDir := filepath.Join(dir, "typeprose")
	require.NoError(t, os.WriteFile(specPath, []byte(internalYAMLTypeProseBroken), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
	})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), specPath)
	assert.Contains(t, err.Error(), "spec structural error")
	assert.NotContains(t, err.Error(), "no GraphQL root operation types found")
	assert.NoDirExists(t, outputDir)
}
