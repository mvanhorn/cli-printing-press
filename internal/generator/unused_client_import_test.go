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
// The endpoint template may emit the import for GraphQL-shaped endpoints, but
// several branches reach the rendered file without producing a `client.X`
// reference. Without the fixup, Go's strict unused-import rule fires and
// `go build` fails on every such CLI.
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
			name:            "graphql_get_drops_unused_import",
			method:          "GET",
			expectImport:    false,
			expectClientUse: false,
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
							"get": {Method: tt.method, Path: "/graphql", Description: "Get account",
								Params: []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, Description: "Account ID"}},
							},
							"search": {Method: tt.method, Path: "/graphql", Description: "Search accounts"},
						},
					},
				},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			src := readGeneratedFile(t, outputDir, "internal", "cli", "accounts_get.go")
			hasImport := strings.Contains(src, `/internal/client"`)
			hasUsage := strings.Contains(src, "client.")
			assert.Equal(t, tt.expectImport, hasImport, "accounts_get.go import presence")
			assert.Equal(t, tt.expectClientUse, hasUsage, "accounts_get.go client.X usage")
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
