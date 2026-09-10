package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_StripsCapturedResourceIDsFromFlagDefaults(t *testing.T) {
	t.Parallel()

	const (
		cliID = "cli_a1b2c3d4e5f6g7h8i9j0"
		hash  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	capture := &browsersniff.EnrichedCapture{
		TargetURL: "https://www.example.com",
		Entries: []browsersniff.EnrichedEntry{
			graphQLBFFCaptureEntry("GetClient", `{"id":"`+cliID+`","clientId":"`+cliID+`"}`, hash),
			graphQLBFFCaptureEntry("GetProperty", `{"id":"prop_k9m2n3p4q5r6s7t8u9v0"}`, hash),
		},
	}
	apiSpec, err := browsersniff.AnalyzeCapture(capture)
	require.NoError(t, err)
	apiSpec.HTTPTransport = spec.HTTPTransportStandard
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/example-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "example-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	var commandSrc strings.Builder
	err = filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		commandSrc.Write(data)
		return nil
	})
	require.NoError(t, err)
	src := commandSrc.String()
	require.Contains(t, src, `StringVar(&bodyVariables, "variables"`)
	assert.NotContains(t, src, cliID)
	assert.NotContains(t, src, "prop_k9m2n3p4q5r6s7t8u9v0")
	assert.Contains(t, src, "cli_example0000000000000")
	assert.Contains(t, src, "prop_example0000000000000")
	assert.Contains(t, src, hash)

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerate_StripsCapturedResourceIDsFromDirtySpecDefaults(t *testing.T) {
	t.Parallel()

	const cliID = "cli_a1b2c3d4e5f6g7h8i9j0"
	cases := []struct {
		name   string
		source string
	}{
		{name: "legacy-empty", source: ""},
		{name: "sniffed", source: "sniffed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cliName := "dirtyids" + strings.ReplaceAll(tc.name, "-", "")
			apiSpec := &spec.APISpec{
				Name:        cliName,
				Description: "Dirty capture-id defaults",
				Version:     "0.1.0",
				BaseURL:     "https://api.example.com",
				SpecSource:  tc.source,
				Auth:        spec.AuthConfig{Type: "none"},
				Config:      spec.ConfigSpec{Format: "toml", Path: "~/.config/" + cliName + "-pp-cli/config.toml"},
				Resources: map[string]spec.Resource{
					"clients": {
						Description: "Clients",
						Endpoints: map[string]spec.Endpoint{
							"get": {
								Method:      "POST",
								Path:        "/graphql",
								Description: "Get a client",
								Body: []spec.Param{
									{Name: "operationName", Type: "string", Required: true, Default: "GetClient"},
									{Name: "variables", Type: "object", Default: map[string]any{"id": cliID}},
								},
							},
						},
					},
				},
				Types: map[string]spec.TypeDef{},
			}

			outputDir := filepath.Join(t.TempDir(), cliName+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			var commandSrc string
			err := filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
					return walkErr
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				if strings.Contains(string(data), `StringVar(&bodyVariables, "variables"`) {
					commandSrc = string(data)
				}
				return nil
			})
			require.NoError(t, err)
			require.NotEmpty(t, commandSrc)
			assert.NotContains(t, commandSrc, cliID)
			assert.Contains(t, commandSrc, `StringVar(&bodyVariables, "variables", "{\"id\":\"cli_example0000000000000\"}"`)

			requireGeneratedCompiles(t, outputDir)
		})
	}
}

func TestGenerate_PreservesAuthoredResourceIDDefaults(t *testing.T) {
	t.Parallel()

	const (
		uuid  = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
		cliID = "cli_a1b2c3d4e5f6g7h8i9j0"
	)
	apiSpec := &spec.APISpec{
		Name:        "authoredids",
		Description: "Authored OpenAPI defaults",
		Version:     "0.1.0",
		BaseURL:     "https://api.example.com",
		SpecSource:  "official",
		Auth:        spec.AuthConfig{Type: "none"},
		Config:      spec.ConfigSpec{Format: "toml", Path: "~/.config/authoredids-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"clients": {
				Description: "Clients",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/clients/{id}",
						Description: "Get a client",
						Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Default: uuid}},
					},
				},
			},
			"search": {
				Description: "Search",
				Endpoints: map[string]spec.Endpoint{
					"run": {
						Method:      "POST",
						Path:        "/graphql",
						Description: "Search clients",
						Body: []spec.Param{
							{Name: "operationName", Type: "string", Required: true, Default: "GetClient"},
							{Name: "variables", Type: "object", Default: map[string]any{"id": cliID}},
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}

	outputDir := filepath.Join(t.TempDir(), "authoredids-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	var commandSrc strings.Builder
	err := filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		commandSrc.Write(data)
		return nil
	})
	require.NoError(t, err)
	src := commandSrc.String()
	assert.Contains(t, src, uuid)
	assert.Contains(t, src, cliID)
	assert.NotContains(t, src, "cli_example0000000000000")
	assert.NotContains(t, src, "00000000-0000-4000-8000-000000000000")

	requireGeneratedCompiles(t, outputDir)
}

func TestSanitizeCapturedResourceIDsForGenerate(t *testing.T) {
	t.Parallel()

	const uuid = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	newSpec := func(source string) *spec.APISpec {
		return &spec.APISpec{
			SpecSource: source,
			Resources: map[string]spec.Resource{
				"clients": {
					Endpoints: map[string]spec.Endpoint{
						"get": {Params: []spec.Param{{Name: "id", Type: "string", Default: uuid}}},
					},
				},
			},
		}
	}

	for _, source := range []string{"official", "community", "docs"} {
		apiSpec := newSpec(source)
		sanitizeCapturedResourceIDsForGenerate(apiSpec)
		assert.Equal(t, uuid, apiSpec.Resources["clients"].Endpoints["get"].Params[0].Default, source)
	}

	for _, source := range []string{"", "sniffed"} {
		apiSpec := newSpec(source)
		sanitizeCapturedResourceIDsForGenerate(apiSpec)
		assert.Nil(t, apiSpec.Resources["clients"].Endpoints["get"].Params[0].Default, source)
	}
}
