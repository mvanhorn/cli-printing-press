package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDeleteEndpointWithJSONBodyUsesRequestBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("deletebody")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/assets", Description: "List assets"},
				"delete": {
					Method:             "DELETE",
					Path:               "/assets",
					Description:        "Delete assets",
					RequestContentType: "application/json",
					Body: []spec.Param{
						{Name: "force", Type: "bool"},
						{Name: "ids", Type: "array", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "assets_delete.go")
	assert.Contains(t, endpointSrc, `bodyMap := map[string]any{}`)
	assert.Contains(t, endpointSrc, `bodyMap["ids"] = parsed`)
	assert.Contains(t, endpointSrc, `if cmd.Flags().Changed("force")`)
	assert.Contains(t, endpointSrc, `bodyMap["force"] = bodyForce`)
	assert.Contains(t, endpointSrc, `c.DeleteWithBody(cmd.Context(), path, body)`)
	assert.NotContains(t, endpointSrc, `params["ids"]`)
	assert.NotContains(t, endpointSrc, `params["force"]`)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `func (c *Client) DeleteWithBody(ctx context.Context, path string, body any) (json.RawMessage, int, error)`)

	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratedDeleteEndpointRuntimeSplitsQueryAndBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("deletewire")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/assets", Description: "List assets"},
				"delete": {
					Method:             "DELETE",
					Path:               "/assets",
					Description:        "Delete assets",
					RequestContentType: "application/json",
					Params: []spec.Param{
						{Name: "mode", Type: "string"},
					},
					Body: []spec.Param{
						{Name: "force", Type: "bool"},
						{Name: "ids", Type: "array", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteEndpointSendsQueryAndJSONBody(t *testing.T) {
	var gotMethod, gotPath, gotRawQuery string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	t.Setenv("DELETEWIRE_BASE_URL", server.URL)

	flags := &rootFlags{asJSON: true}
	cmd := newAssetsDeleteCmd(flags)
	cmd.SetArgs([]string{"--ids", ` + "`" + `["a","b"]` + "`" + `, "--force", "--mode", "preview"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/assets" {
		t.Fatalf("path = %q, want /assets", gotPath)
	}
	if gotRawQuery != "mode=preview" {
		t.Fatalf("query = %q, want mode=preview", gotRawQuery)
	}
	if _, ok := gotBody["mode"]; ok {
		t.Fatalf("query param leaked into body: %#v", gotBody)
	}
	ids, ok := gotBody["ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("ids body = %#v, want [a b]", gotBody["ids"])
	}
	force, ok := gotBody["force"].(bool)
	if !ok || !force {
		t.Fatalf("force body = %#v, want true", gotBody["force"])
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "delete_body_wire_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDeleteEndpointSendsQueryAndJSONBody", "-count=1")
}

func TestGenerateDeleteEndpointWithoutBodyKeepsQueryParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("deleteparams")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/assets", Description: "List assets"},
				"delete": {
					Method:      "DELETE",
					Path:        "/assets",
					Description: "Delete assets",
					Params: []spec.Param{
						{Name: "force", Type: "bool"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "assets_delete.go")
	assert.Contains(t, endpointSrc, `params["force"] = formatCLIParamValue(flagForce)`)
	assert.Contains(t, endpointSrc, `c.DeleteWithParams(cmd.Context(), path, params)`)
	assert.NotContains(t, endpointSrc, `DeleteWithBody`)

	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratePromotedDeleteEndpointWithJSONBodyUsesRequestBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promodeletebody")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"delete": {
					Method:             "DELETE",
					Path:               "/assets",
					Description:        "Delete assets",
					RequestContentType: "application/json",
					Body: []spec.Param{
						{Name: "ids", Type: "array", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_assets.go")
	assert.Contains(t, endpointSrc, `bodyMap := map[string]any{}`)
	assert.Contains(t, endpointSrc, `bodyMap["ids"] = parsed`)
	assert.Contains(t, endpointSrc, `c.DeleteWithBody(cmd.Context(), path, body)`)
	assert.NotContains(t, endpointSrc, `params["ids"]`)

	runGoCommand(t, outputDir, "build", "./...")
}

func TestGeneratePromotedDeleteEndpointWithJSONBodyAndParamsUsesRequestBodyAndQueryParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promodeletebodyparams")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Manage assets",
			Endpoints: map[string]spec.Endpoint{
				"delete": {
					Method:             "DELETE",
					Path:               "/assets",
					Description:        "Delete assets",
					RequestContentType: "application/json",
					Params: []spec.Param{
						{Name: "mode", Type: "string"},
					},
					Body: []spec.Param{
						{Name: "ids", Type: "array", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_assets.go")
	assert.Contains(t, endpointSrc, `params["mode"] = formatCLIParamValue(flagMode)`)
	assert.Contains(t, endpointSrc, `bodyMap := map[string]any{}`)
	assert.Contains(t, endpointSrc, `bodyMap["ids"] = parsed`)
	assert.Contains(t, endpointSrc, `c.DeleteWithParamsAndBody(cmd.Context(), path, params, body)`)
	assert.NotContains(t, endpointSrc, `params["ids"]`)

	runGoCommand(t, outputDir, "build", "./...")
}
