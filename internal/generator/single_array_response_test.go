package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDetailResponseWithSingleArrayKeepsWholeObject(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Single Array Detail
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /accounts/{id}:
    get:
      operationId: getAccount
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  ID: {type: string}
                  Name: {type: string}
                  Color: {type: string}
                  ApiTokens:
                    type: array
                    items: {type: string}
`))
	require.NoError(t, err)

	endpoint := apiSpec.Resources["accounts"].Endpoints["get"]
	require.Equal(t, "object", endpoint.Response.Type)
	require.Empty(t, endpoint.ResponsePath)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "single_array_response_test.go"), []byte(`package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetailResponseWithSingleArrayKeepsWholeObject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/get" {
			t.Fatalf("path = %q, want /accounts/get", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`{"ID":"account-1","Name":"Primary","Color":"blue","ApiTokens":["token-1"]}`+"`"+`)
	}))
	defer server.Close()
	t.Setenv("SINGLE_ARRAY_DETAIL_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"accounts", "get", "account-1", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}
	for _, want := range []string{"account-1", "Primary", "blue", "token-1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("detail response lost %q: stdout=%s stderr=%s", want, stdout.String(), stderr.String())
		}
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDetailResponseWithSingleArrayKeepsWholeObject", "-count=1")
}
