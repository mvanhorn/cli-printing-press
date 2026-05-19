package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateFormRequestBodyUsesFormClient validates that endpoints declaring
// application/x-www-form-urlencoded request bodies are routed through the
// new c.PostForm helper rather than c.Post — covers the OAuth and Resy bug
// described in #921.
func TestGenerateFormRequestBodyUsesFormClient(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("formapi")
	apiSpec.Resources = map[string]spec.Resource{
		"oauth": {
			Description: "OAuth token operations",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/oauth/clients",
					Description: "List OAuth clients",
				},
				"token": {
					Method:             "POST",
					Path:               "/oauth/token",
					Description:        "Exchange OAuth token",
					RequestContentType: "application/x-www-form-urlencoded",
					Body: []spec.Param{
						{Name: "grant_type", Type: "string", Required: true, Description: "Grant type"},
						{Name: "client_id", Type: "string", Required: true, Description: "Client id"},
						{Name: "client_secret", Type: "string", Description: "Client secret"},
					},
				},
			},
		},
		"venues": {
			Description: "Venue operations",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:             "POST",
					Path:               "/venues/search",
					Description:        "Search venues",
					RequestContentType: "application/x-www-form-urlencoded",
					Body: []spec.Param{
						{Name: "struct_data", Type: "string", Format: "json", Required: true, Description: "JSON-encoded query payload"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `func (c *Client) PostForm(path string, fields url.Values) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `func (c *Client) PostFormWithHeaders(path string, fields url.Values, headers map[string]string) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `type formRequestBody struct {`)
	assert.Contains(t, clientSrc, `func encodeFormBody(body formRequestBody) ([]byte, string, error)`)
	assert.Contains(t, clientSrc, `body.Fields.Encode()`)
	assert.Contains(t, clientSrc, `req.Header.Set("Content-Type", contentType)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "oauth_token.go")
	assert.Contains(t, endpointSrc, `"net/url"`)
	assert.Contains(t, endpointSrc, `fields := url.Values{}`)
	assert.Contains(t, endpointSrc, `fields.Set("grant_type", bodyGrantType)`)
	assert.Contains(t, endpointSrc, `fields.Set("client_id", bodyClientId)`)
	assert.Contains(t, endpointSrc, `c.PostFormWithParams(path, params, fields)`)
	assert.NotContains(t, endpointSrc, `var stdinBody bool`)
	assert.NotContains(t, endpointSrc, `c.Post(path, body)`)
	// Required-flag check should fire at top-level, not inside `if !stdinBody`.
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "grant-type")`)
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "client-id")`)

	// JSON-string body field validates as JSON before sending. Single-endpoint
	// resource collapses to the promoted form rather than a per-endpoint file.
	venuesSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_venues.go")
	assert.Contains(t, venuesSrc, `if !json.Valid([]byte(bodyStructData))`)
	assert.Contains(t, venuesSrc, `fields.Set("struct_data", bodyStructData)`)
	assert.Contains(t, venuesSrc, `c.PostFormWithParams(path, params, fields)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `RequestContentType: "application/x-www-form-urlencoded"`)
	assert.Contains(t, mcpSrc, `formFields := url.Values{}`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostFormWithParams(path, params, formFields)`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateNonFormSpecOmitsFormHelpers asserts the negative case: a spec
// with no form-encoded endpoints generates client.go without any form-specific
// imports, types, or methods. Guards the byte-identical-output criterion in
// #921's acceptance criteria.
func TestGenerateNonFormSpecOmitsFormHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plainapi")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	// Form-helper method declarations must be absent (HTTPClient.PostForm
	// from net/http is fine — only the *Client receiver method is gated).
	assert.NotContains(t, clientSrc, `func (c *Client) PostForm`)
	assert.NotContains(t, clientSrc, `func (c *Client) PutForm`)
	assert.NotContains(t, clientSrc, `func (c *Client) PatchForm`)
	assert.NotContains(t, clientSrc, "formRequestBody")
	assert.NotContains(t, clientSrc, "encodeFormBody")
}
