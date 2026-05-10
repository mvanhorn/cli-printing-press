package generator

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointCommandDropsUnusedClientImport pins the post-render fixup that
// strips `<module>/internal/client` from per-endpoint command files when the
// rendered body never references the `client` package.
//
// The endpoint template emits the import for the GraphQL list/get path, but
// other branches reach the rendered file without producing a `client.X`
// reference — most prominently when a GraphQL spec records list/get endpoints
// with method POST (which is the wire-correct method, since GraphQL queries
// POST to /graphql). Without the fixup, Go's strict unused-import rule fires
// and `go build` fails on every such CLI.
func TestEndpointCommandDropsUnusedClientImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		method          string
		expectImport    bool // whether the generated endpoint command keeps the client import
		expectClientUse bool
	}{
		{
			name:            "graphql_post_drops_unused_import",
			method:          "POST",
			expectImport:    false,
			expectClientUse: false,
		},
		{
			name:            "graphql_get_keeps_used_import",
			method:          "GET",
			expectImport:    true,
			expectClientUse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := &spec.APISpec{
				Name:    tt.name,
				Version: "0.1.0",
				BaseURL: "https://api.example.com/graphql",
				Owner:   "test-owner",
				Auth: spec.AuthConfig{
					Type:    "api_key",
					Header:  "Authorization",
					Format:  "Bearer {token}",
					EnvVars: []string{"X_TOKEN"},
				},
				Resources: map[string]spec.Resource{
					"accounts": {
						Description: "Accounts",
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: tt.method, Path: "/graphql", Description: "List accounts"},
							"get": {Method: tt.method, Path: "/graphql", Description: "Get account",
								Params: []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, Description: "Account ID"}},
							},
						},
					},
				},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			for _, fname := range []string{"accounts_list.go", "accounts_get.go"} {
				src := readGeneratedFile(t, outputDir, "internal", "cli", fname)
				hasImport := strings.Contains(src, `/internal/client"`)
				hasUsage := strings.Contains(src, "client.")
				assert.Equal(t, tt.expectImport, hasImport, "%s import presence", fname)
				assert.Equal(t, tt.expectClientUse, hasUsage, "%s client.X usage", fname)
			}
		})
	}
}

// TestEndpointCommandBuildsPostFixup runs `go build` on a generated module
// whose endpoints would have shipped an unused client import before the
// fixup landed. Catches future regressions of the underlying compile error
// even if the import-vs-usage assertions in the unit test above drift.
func TestEndpointCommandBuildsPostFixup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go build smoke test in short mode")
	}
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "buildcheck",
		Version: "0.1.0",
		BaseURL: "https://api.example.com/graphql",
		Owner:   "test-owner",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"X_TOKEN"},
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Description: "Accounts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "POST", Path: "/graphql", Description: "List accounts"},
					"get": {Method: "POST", Path: "/graphql", Description: "Get account",
						Params: []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, Description: "Account ID"}},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "buildcheck-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = outputDir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed:\n%s", out)
	}

	build := exec.Command("go", "build", "./...")
	build.Dir = outputDir
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build failed:\n%s", out)
}
