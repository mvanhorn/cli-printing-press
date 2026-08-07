package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiPathParamPositionalsFollowPathOrder pins #3369. When a spec declares
// its path parameters deepest-first — the reverse of their URL path order, the
// shape the OpenAPI parser produces from a parameters array that lists
// project_name before account_id (Cloudflare's
// /accounts/{account_id}/pages/projects/{project_name} does this) — the
// generated command must still list and bind positionals in path order:
//
//	Use: "... <account_id> <project_name>"
//	args[0] -> account_id, args[1] -> project_name
//
// Before the fix the command emitted "<project_name> <account_id>" with the
// bindings reversed, so passing args in natural URL order routed each value
// into the wrong path slot and returned a misleading 404 that did not point at
// argument order.
func TestMultiPathParamPositionalsFollowPathOrder(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "pathorder",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/pathorder-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"pages": {
				Description: "Pages",
				SubResources: map[string]spec.Resource{
					"projects": {
						Description: "Pages projects",
						Endpoints: map[string]spec.Endpoint{
							// Path params declared deepest-first (project_name
							// before account_id), the reverse of their order in the
							// URL path — exactly what the OpenAPI parser appends from
							// a parameters array in that order.
							"get-project": {
								Method:      "GET",
								Path:        "/accounts/{account_id}/pages/projects/{project_name}",
								Description: "Get a pages project",
								Params: []spec.Param{
									{Name: "project_name", Type: "string", Required: true, Positional: true},
									{Name: "account_id", Type: "string", Required: true, Positional: true},
								},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pages_projects_get-project.go"))
	require.NoError(t, err)
	body := string(src)

	// Use string and Example list positionals in URL path order, not the
	// reverse parameters-array order the spec declared.
	assert.Contains(t, body, "<account_id> <project_name>",
		"positionals must appear in URL path order in the Use/Example strings")
	assert.NotContains(t, body, "<project_name> <account_id>",
		"must not emit positionals in reverse path order")

	// Bindings resolve each path param to the matching path-order arg slot.
	assert.Contains(t, body, `path = replacePathParam(path, "account_id", args[0])`,
		"first path param (account_id) binds to args[0]")
	assert.Contains(t, body, `path = replacePathParam(path, "project_name", args[1])`,
		"second path param (project_name) binds to args[1]")
	assert.NotContains(t, body, `path = replacePathParam(path, "account_id", args[1])`,
		"must not bind account_id to the deeper arg slot")
	assert.NotContains(t, body, `path = replacePathParam(path, "project_name", args[0])`,
		"must not bind project_name to the shallow arg slot")

	requireGeneratedCompiles(t, outputDir)
}

// TestMultiPathParamPositionalsFollowPathOrderFromOpenAPI is the end-to-end
// counterpart: it proves the reversal originates in the OpenAPI parser (which
// appends path params in parameters-array order) and that the generated command
// is corrected to path order. Two operations keep the resource off the
// single-endpoint promotion path so this exercises command_endpoint.go.tmpl.
func TestMultiPathParamPositionalsFollowPathOrderFromOpenAPI(t *testing.T) {
	t.Parallel()

	yaml := `openapi: 3.0.0
info:
  title: Reverse Params
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /accounts/{account_id}/pages/projects/{project_name}:
    get:
      summary: Get a pages project
      parameters:
        - name: project_name
          in: path
          required: true
          schema: {type: string}
        - name: account_id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
    delete:
      summary: Delete a pages project
      parameters:
        - name: project_name
          in: path
          required: true
          schema: {type: string}
        - name: account_id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`
	apiSpec, err := openapi.Parse([]byte(yaml))
	require.NoError(t, err)
	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test"

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "*.go"))
	require.NoError(t, err)

	var body string
	for _, m := range matches {
		data, readErr := os.ReadFile(m)
		require.NoError(t, readErr)
		if strings.Contains(string(data), `replacePathParam(path, "account_id"`) {
			body = string(data)
			break
		}
	}
	require.NotEmpty(t, body, "expected a generated command that substitutes account_id")

	assert.Contains(t, body, "<account_id> <project_name>",
		"end-to-end: positionals must appear in URL path order")
	assert.NotContains(t, body, "<project_name> <account_id>",
		"end-to-end: must not emit positionals in reverse path order")
	assert.Contains(t, body, `path = replacePathParam(path, "account_id", args[0])`,
		"end-to-end: account_id binds to args[0]")
	assert.Contains(t, body, `path = replacePathParam(path, "project_name", args[1])`,
		"end-to-end: project_name binds to args[1]")

	requireGeneratedCompiles(t, outputDir)
}

// TestPromotedCommandMultiPathParamPositionalsFollowPathOrder covers the
// promoted-command variant (command_promoted.go.tmpl), which emits a separate
// file but shares the positional helpers. A single-endpoint resource with its
// path params declared deepest-first must still promote to a command that lists
// and binds them in URL path order.
func TestPromotedCommandMultiPathParamPositionalsFollowPathOrder(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promopathorder",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/promopathorder-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// Single-endpoint resource — promotes to a top-level command.
			// Path params declared deepest-first (project_name before
			// account_id), the reverse of their URL path order.
			"projects": {
				Description: "Pages projects",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/accounts/{account_id}/pages/projects/{project_name}",
						Description: "Get a pages project",
						Params: []spec.Param{
							{Name: "project_name", Type: "string", Required: true, Positional: true},
							{Name: "account_id", Type: "string", Required: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "promoted_*.go"))
	require.NoError(t, err)
	require.Lenf(t, matches, 1, "expected exactly one promoted_*.go file, got %v", matches)
	data, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	body := string(data)

	assert.Contains(t, body, "<account_id> <project_name>",
		"promoted command: positionals must appear in URL path order")
	assert.NotContains(t, body, "<project_name> <account_id>",
		"promoted command: must not emit positionals in reverse path order")
	assert.Contains(t, body, `path = replacePathParam(path, "account_id", args[0])`,
		"promoted command: account_id binds to args[0]")
	assert.Contains(t, body, `path = replacePathParam(path, "project_name", args[1])`,
		"promoted command: project_name binds to args[1]")

	requireGeneratedCompiles(t, outputDir)
}
