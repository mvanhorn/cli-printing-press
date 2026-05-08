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

func TestGenerateBinaryResponseCommandsUseRawOutputPath(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:    "binary",
		Version: "1.0.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/binary-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"exports": {
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/exports/{id}",
						Description: "Get export metadata",
						Params: []spec.Param{
							{Name: "id", Type: "string", Required: true, Positional: true, Description: "Export id"},
						},
					},
					"print": {
						Method:               "GET",
						Path:                 "/exports/{id}/print",
						Description:          "Print export",
						ResponseContentTypes: []string{"application/json", "application/pdf"},
						Params: []spec.Param{
							{Name: "id", Type: "string", Required: true, Positional: true, Description: "Export id"},
							{Name: "format", Type: "string", Description: "Format"},
						},
					},
				},
			},
			"statement": {
				Endpoints: map[string]spec.Endpoint{
					"download": {
						Method:               "GET",
						Path:                 "/statement",
						Description:          "Download statement",
						ResponseContentTypes: []string{"text/csv"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	endpointData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "exports_print.go"))
	require.NoError(t, err)
	endpointSource := string(endpointData)
	assert.Contains(t, endpointSource, `var outputPath string`)
	assert.Contains(t, endpointSource, `var flagAccept string`)
	assert.Contains(t, endpointSource, `data, _, err := c.Raw("GET", path, params, nil, nil, accept)`)
	assert.Contains(t, endpointSource, `return writeBinaryOutput(cmd.OutOrStdout(), data, outputPath)`)
	assert.Contains(t, endpointSource, `cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write binary response to file")`)
	assert.Contains(t, endpointSource, `cmd.Flags().StringVar(&flagAccept, "accept", "application/pdf", "Response MIME type to request")`)
	assert.NotContains(t, endpointSource, `wrapWithProvenance`)

	promotedData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_statement.go"))
	require.NoError(t, err)
	promotedSource := string(promotedData)
	assert.Contains(t, promotedSource, `cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write binary response to file")`)
	assert.Contains(t, promotedSource, `data, _, err := c.Raw("GET", path, params, nil, nil, accept)`)
	assert.NotContains(t, promotedSource, `flagAccept`)

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
