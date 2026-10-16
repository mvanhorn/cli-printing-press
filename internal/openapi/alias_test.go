package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeAliasCreateOnlyForCreateShapedPOST(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		path   string
		name   string
		want   string
	}{
		{method: "POST", path: "/widgets", name: "add-widget", want: "create"},
		{method: "POST", path: "/widgets", name: "widgets", want: "create"},
		{method: "POST", path: "/accounts/{id}/widgets", name: "add", want: "create"},
		{method: "POST", path: "/renders", name: "submit", want: ""},
		{method: "POST", path: "/distributors/{id}/cancel", name: "cancel", want: ""},
		{method: "POST", path: "/executables/{uuid}:block", name: "block", want: ""},
		{method: "POST", path: "/tokens/{id}/revoke", name: "revoke", want: ""},
		{method: "GET", path: "/widgets", name: "widgets", want: "list"},
		{method: "GET", path: "/widgets/{id}", name: "show", want: "get"},
		{method: "PATCH", path: "/widgets/{id}", name: "edit-widget", want: "update"},
		{method: "DELETE", path: "/widgets/{id}", name: "remove", want: "delete"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, computeAlias(tt.method, tt.path, tt.name))
		})
	}
}

func TestParsePOSTCreateAliasSkipsActions(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Alias Probe
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
    post:
      operationId: addWidget
      responses:
        "201":
          description: created
  /distributors:
    get:
      operationId: listDistributors
      responses:
        "200":
          description: ok
  /distributors/{id}/cancel:
    post:
      operationId: cancelDistributor
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /executables:
    get:
      operationId: listExecutables
      responses:
        "200":
          description: ok
  /executables/{uuid}:block:
    post:
      operationId: blockExecutable
      parameters:
        - name: uuid
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /renders:
    get:
      operationId: listRenders
      responses:
        "200":
          description: ok
    post:
      operationId: submitRender
      responses:
        "202":
          description: accepted
  /notes:
    get:
      operationId: listNotes
      responses:
        "200":
          description: ok
    post:
      responses:
        "201":
          description: created
`))
	require.NoError(t, err)

	_, addWidget, ok := endpointByMethodPath(parsed.Resources, "POST", "/widgets")
	require.True(t, ok)
	assert.Equal(t, "create", addWidget.Alias)

	cancelName, cancel, ok := endpointByMethodPath(parsed.Resources, "POST", "/distributors/{id}/cancel")
	require.True(t, ok)
	assert.Empty(t, cancel.Alias)
	assert.NotEqual(t, "create", cancelName)

	_, block, ok := endpointByMethodPath(parsed.Resources, "POST", "/executables/{uuid}:block")
	require.True(t, ok)
	assert.Empty(t, block.Alias)

	_, submit, ok := endpointByMethodPath(parsed.Resources, "POST", "/renders")
	require.True(t, ok)
	assert.Empty(t, submit.Alias)

	noteName, note, ok := endpointByMethodPath(parsed.Resources, "POST", "/notes")
	require.True(t, ok)
	assert.Equal(t, "create", noteName)
	assert.Empty(t, note.Alias, "a command already named create must not also alias create")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(parsed.Name))
	require.NoError(t, generator.New(parsed, outputDir).Generate())

	assertCommandCreateAlias(t, outputDir, "/widgets", true)
	assertCommandCreateAlias(t, outputDir, "/distributors/{id}/cancel", false)
	assertCommandCreateAlias(t, outputDir, "/executables/{uuid}:block", false)
	assertCommandCreateAlias(t, outputDir, "/renders", false)
}

func endpointByMethodPath(resources map[string]spec.Resource, method, path string) (string, spec.Endpoint, bool) {
	for _, resource := range resources {
		for name, endpoint := range resource.Endpoints {
			if strings.EqualFold(endpoint.Method, method) && endpoint.Path == path {
				return name, endpoint, true
			}
		}
		if name, endpoint, ok := endpointByMethodPath(resource.SubResources, method, path); ok {
			return name, endpoint, true
		}
	}
	return "", spec.Endpoint{}, false
}

func assertCommandCreateAlias(t *testing.T, outputDir, path string, want bool) {
	t.Helper()
	var matched string
	err := filepath.WalkDir(outputDir, func(file string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".go") {
			return err
		}
		body, readErr := os.ReadFile(file)
		if readErr != nil {
			return readErr
		}
		src := string(body)
		if strings.Contains(src, `"pp:path": "`+path+`"`) && strings.Contains(src, `"pp:method": "POST"`) {
			matched = src
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, matched, "no generated POST command for %s", path)
	const createAlias = `[]string{"create"}`
	if want {
		assert.Contains(t, matched, createAlias)
		return
	}
	assert.NotContains(t, matched, createAlias)
}
