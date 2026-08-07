package generator

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePromotedCommandLongUsesEndpointDescriptionOnly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("placesapi")
	apiSpec.Resources = map[string]spec.Resource{
		"places": {
			Description: "Places",
			Endpoints: map[string]spec.Endpoint{
				"suggest": {
					Method:      "GET",
					Path:        "/places/suggest",
					Description: "Suggest matching places for a typed query",
					Params: []spec.Param{
						{Name: "market", Type: "string", Required: true, Positional: true, Description: "Market"},
						{Name: "locale", Type: "string", Required: true, Positional: true, Description: "Locale"},
						{Name: "query", Type: "string", Required: true, Positional: true, Description: "Query"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_places.go")
	assert.NotContains(t, src, `Shortcut for '`, "promoted command Long must not advertise a dead resource/endpoint path")
	assert.Regexp(t, regexp.MustCompile(`Long:\s+"Suggest matching places for a typed query",`), src,
		"promoted command Long must contain the endpoint description")
}
