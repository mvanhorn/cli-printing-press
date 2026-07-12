package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
						{Name: "facets", Type: "array", Description: "Facet filters"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `func (c *Client) PostForm(ctx context.Context, path string, fields url.Values) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `func (c *Client) PostFormWithHeaders(ctx context.Context, path string, fields url.Values, headers map[string]string) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `func (c *Client) PostQueryFormWithParams(ctx context.Context, path string, params map[string]string, fields url.Values) (json.RawMessage, int, error)`)
	assert.Contains(t, clientSrc, `return c.doRead(ctx, "POST", path, params, formRequestBody{Fields: fields}, nil)`)
	assert.Contains(t, clientSrc, `type formRequestBody struct {`)
	assert.Contains(t, clientSrc, `func encodeFormBody(body formRequestBody) ([]byte, string, error)`)
	assert.Contains(t, clientSrc, `body.Fields.Encode()`)
	assert.Contains(t, clientSrc, `req.Header.Set("Content-Type", contentType)`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "oauth_token.go")
	assert.Contains(t, endpointSrc, `"net/url"`)
	assert.Contains(t, endpointSrc, `fields := url.Values{}`)
	assert.Contains(t, endpointSrc, `fields.Set("grant_type", bodyGrantType)`)
	assert.Contains(t, endpointSrc, `fields.Set("client_id", bodyClientId)`)
	assert.Contains(t, endpointSrc, `c.PostFormWithParams(cmd.Context(), path, params, fields)`)
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
	assert.Contains(t, venuesSrc, `c.PostQueryFormWithParams(cmd.Context(), path, params, fields)`)
	assert.NotContains(t, venuesSrc, `c.PostFormWithParams(cmd.Context(), path, params, fields)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `RequestContentType: "application/x-www-form-urlencoded"`)
	assert.Contains(t, mcpSrc, `formFields := url.Values{}`)
	assert.Contains(t, mcpSrc, `data, _, err = c.PostQueryFormWithParams(ctx, path, params, formFields)`)

	// An array/object body param binds to its native JSON type even on an
	// x-www-form-urlencoded endpoint (not WithString), then flows through
	// mcpFormFieldValue, which JSON-encodes a native composite rather than
	// rendering it with Go's "%v". This is the form twin of the multipart path
	// and locks it against a future helper refactor silently re-breaking it.
	assert.Contains(t, mcpSrc, `mcplib.WithArray("facets"`)
	assert.Contains(t, mcpSrc, `formFields.Set(binding.WireName, mcpFormFieldValue(v))`)
	assert.Regexp(t, `(?s)func mcpFormFieldValue\(v any\) string \{.*?json\.Marshal\(v\)`, mcpSrc)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateReadOnlyFormHTMLTableEndpoint(t *testing.T) {
	t.Parallel()

	requests := make(chan struct{}, 1)
	server := http.NewServeMux()
	server.HandleFunc("/contracts", func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- struct{}{}:
		default:
		}
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "table", r.Form.Get("ajax"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<table><thead><tr><th>Player</th><th>Salary</th></tr></thead><tbody><tr><td>Ada Lovelace</td><td>$100</td><td><table><tr><td>layout</td><td>ignored</td></tr></table></td></tr><tr><td>Grace Hopper</td><td>$200</td></tr></tbody></table>`))
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	apiSpec := minimalSpec("formhtml")
	apiSpec.BaseURL = httpServer.URL
	apiSpec.Resources = map[string]spec.Resource{
		"contracts": {
			Description: "Contract tables",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:             "POST",
					Path:               "/contracts",
					Description:        "List contract rows",
					RequestContentType: "application/x-www-form-urlencoded",
					ResponseFormat:     spec.ResponseFormatHTML,
					HTMLExtract:        &spec.HTMLExtract{Mode: spec.HTMLExtractModeTable},
					Response:           spec.ResponseDef{Type: "array", Item: "html_table_row"},
					Meta:               map[string]string{"mcp:read-only": "true"},
					Body: []spec.Param{
						{Name: "ajax", Type: "string", Default: "table", Description: "Captured table fragment mode"},
					},
				},
			},
		},
	}

	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_contracts.go")
	assert.Contains(t, endpointSrc, `c.PostQueryFormWithParams(cmd.Context(), path, params, fields)`)
	assert.Contains(t, endpointSrc, `"mcp:read-only": "true"`)
	assert.NotContains(t, endpointSrc, `c.PostFormWithParams(cmd.Context(), path, params, fields)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `data, _, err = c.PostQueryFormWithParams(ctx, path, params, formFields)`)

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "formhtml-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/formhtml-pp-cli")

	cmd := exec.Command(binaryPath, "contracts", "list", "--json")
	cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1", "FORMHTML_BASE_URL="+httpServer.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	select {
	case <-requests:
	default:
		t.Fatal("read-only form POST should bypass verify-mode mutation short-circuit")
	}

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(out, &rows), string(out))
	require.Len(t, rows, 2)
	assert.Equal(t, "Ada Lovelace", rows[0]["Player"])
	assert.Equal(t, "$100", rows[0]["Salary"])
	assert.Equal(t, "Grace Hopper", rows[1]["Player"])
	assert.Equal(t, "$200", rows[1]["Salary"])
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
	assert.NotContains(t, clientSrc, `func (c *Client) PostQueryForm`)
	assert.NotContains(t, clientSrc, `func (c *Client) PutForm`)
	assert.NotContains(t, clientSrc, `func (c *Client) PatchForm`)
	assert.NotContains(t, clientSrc, "formRequestBody")
	assert.NotContains(t, clientSrc, "encodeFormBody")
}
