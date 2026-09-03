package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDefaultSyncOmitsNonJSONResources(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("non-json-default-sync")
	apiSpec.Cache.Enabled = true
	apiSpec.Resources["literature"] = spec.Resource{
		Description: "HTML catalog",
		Endpoints: map[string]spec.Endpoint{
			"index": {
				Method:         "GET",
				Path:           "/literature.aspx",
				Description:    "List literature",
				ResponseFormat: spec.ResponseFormatHTML,
				HTMLExtract: &spec.HTMLExtract{
					Mode:         spec.HTMLExtractModeLinks,
					LinkPrefixes: []string{"/download/files"},
				},
				Response: spec.ResponseDef{Type: "array"},
			},
		},
	}
	apiSpec.Resources["sitemaps"] = spec.Resource{
		Description: "Binary sitemap",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:         "GET",
				Path:           "/sitemap.bin",
				Description:    "Download sitemap",
				ResponseFormat: spec.ResponseFormatBinary,
				Response:       spec.ResponseDef{Type: "array"},
			},
		},
	}
	apiSpec.Resources["exports"] = spec.Resource{
		Description: "Plain-text export",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:         "GET",
				Path:           "/export.txt",
				Description:    "Text export",
				ResponseFormat: spec.ResponseFormatText,
				Response:       spec.ResponseDef{Type: "array"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string")
	assert.Contains(t, defaults, `"items"`)
	assert.NotContains(t, defaults, `"literature"`)
	assert.NotContains(t, defaults, `"sitemaps"`)
	assert.NotContains(t, defaults, `"exports"`)

	known := generatedFunctionBody(t, syncSrc, "func knownSyncResourceNames() []string")
	paths := generatedFunctionBody(t, syncSrc, "func syncResourcePath(resource string) (string, error)")

	// SkipDefaultSync resources stay in the sync catalog so `--resources`
	// can still target them; they must not be the bare-sync default.
	assert.Contains(t, known, `"literature"`)
	assert.Contains(t, known, `"sitemaps"`)
	assert.Contains(t, known, `"exports"`)
	assert.Contains(t, paths, `"literature":`)
	assert.Contains(t, paths, `"sitemaps":`)
	assert.Contains(t, paths, `"exports":`)

	requireGeneratedCompiles(t, outputDir)
}
