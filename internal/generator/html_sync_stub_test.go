package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHTMLMajoritySyncOmittedWhenOnlyPageModeCandidates(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-majority")
	apiSpec.Cache.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"pages": {
			Description: "HTML pages",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         "GET",
					Path:           "/pages",
					Description:    "List pages",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
					Response:       spec.ResponseDef{Type: "array"},
				},
				"show": {
					Method:         "GET",
					Path:           "/pages/{id}",
					Description:    "Show a page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
					Response:       spec.ResponseDef{Type: "object"},
				},
				"archive": {
					Method:         "GET",
					Path:           "/archive",
					Description:    "List archived pages",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
					Response:       spec.ResponseDef{Type: "array"},
				},
			},
		},
		"typed_api": {
			Description: "Typed API data",
			Endpoints: map[string]spec.Endpoint{
				"status": {
					Method:      "GET",
					Path:        "/api/status",
					Description: "Status",
					Response:    spec.ResponseDef{Type: "object"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	workflowSrc := readGeneratedFile(t, outputDir, "internal", "cli", "channel_workflow.go")
	readmeSrc := readGeneratedFile(t, outputDir, "README.md")
	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")

	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	assert.NotContains(t, rootSrc, "newSyncCmd(flags)")
	assert.NotContains(t, helpersSrc, "func syncErrorJSON(")
	assert.NotContains(t, helpersSrc, "func parseSyncUserParams(")
	assert.NotContains(t, helpersSrc, "func parseSyncKVFlags(")
	assert.NotContains(t, helpersSrc, "func isSyncAccessWarning(")
	assert.NotContains(t, workflowSrc, "newWorkflowArchiveCmd")
	assert.NotContains(t, workflowSrc, "workflow archive")
	assert.Contains(t, workflowSrc, "Add a site-specific sync command to populate the store.")
	assert.NotContains(t, readmeSrc, "## Freshness")
	assert.NotContains(t, readmeSrc, "meta.freshness")
	assert.NotContains(t, skillSrc, "## Freshness Contract")
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateHTMLMajorityWithJSONSyncKeepsGenericSync(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-majority-json-sync")
	apiSpec.Cache.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"pages": {
			Description: "HTML detail pages",
			Endpoints: map[string]spec.Endpoint{
				"one":   htmlPageEndpoint("/pages/{id}/one", "Page one", "object"),
				"two":   htmlPageEndpoint("/pages/{id}/two", "Page two", "object"),
				"three": htmlPageEndpoint("/pages/{id}/three", "Page three", "object"),
				"four":  htmlPageEndpoint("/pages/{id}/four", "Page four", "object"),
				"five":  htmlPageEndpoint("/pages/{id}/five", "Page five", "object"),
				"six":   htmlPageEndpoint("/pages/{id}/six", "Page six", "object"),
				"seven": htmlPageEndpoint("/pages/{id}/seven", "Page seven", "object"),
			},
		},
		"articles": {
			Description: "Articles",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/articles", Description: "List articles", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"authors": {
			Description: "Authors",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/authors", Description: "List authors", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"tags": {
			Description: "Tags",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/tags", Description: "List tags", Response: spec.ResponseDef{Type: "array"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	workflowSrc := readGeneratedFile(t, outputDir, "internal", "cli", "channel_workflow.go")
	readmeSrc := readGeneratedFile(t, outputDir, "README.md")
	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")

	assert.Contains(t, syncSrc, "func syncResource(")
	assert.NotContains(t, syncSrc, "generic spec-driven sync template does not fit predominantly non-JSON endpoints")
	assert.Contains(t, workflowSrc, "newWorkflowArchiveCmd")
	assert.Contains(t, workflowSrc, "workflow archive")
	assert.Contains(t, readmeSrc, "## Freshness")
	assert.Contains(t, skillSrc, "## Freshness Contract")
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestHTMLSyncStubFallbackUsesInclusiveThreshold(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-threshold")
	apiSpec.Resources = map[string]spec.Resource{
		"pages": {
			Description: "HTML pages",
			Endpoints: map[string]spec.Endpoint{
				"one":   htmlPageEndpoint("/pages/{id}/one", "Page one", "object"),
				"two":   htmlPageEndpoint("/pages/{id}/two", "Page two", "object"),
				"three": htmlPageEndpoint("/pages/{id}/three", "Page three", "object"),
				"four":  htmlPageEndpoint("/pages/{id}/four", "Page four", "object"),
				"five":  htmlPageEndpoint("/pages/{id}/five", "Page five", "object"),
				"six":   htmlPageEndpoint("/pages/{id}/six", "Page six", "object"),
				"seven": htmlPageEndpoint("/pages/{id}/seven", "Page seven", "object"),
			},
		},
		"typed_api": {
			Description: "JSON API",
			Endpoints: map[string]spec.Endpoint{
				"one":   {Method: "GET", Path: "/api/one", Description: "API one", Response: spec.ResponseDef{Type: "object"}},
				"two":   {Method: "GET", Path: "/api/two", Description: "API two", Response: spec.ResponseDef{Type: "object"}},
				"three": {Method: "GET", Path: "/api/three", Description: "API three", Response: spec.ResponseDef{Type: "object"}},
			},
		},
	}

	gen := &Generator{Spec: apiSpec}
	assert.True(t, gen.shouldEmitHTMLSyncStub())
}

func TestGenerateEmbeddedJSONHTMLMajorityKeepsGenericSync(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("embedded-json-majority")
	apiSpec.Resources = map[string]spec.Resource{
		"pages": {
			Description: "Embedded JSON pages",
			Endpoints: map[string]spec.Endpoint{
				"list":    embeddedJSONHTMLEndpoint("/pages", "List pages", "array"),
				"show":    embeddedJSONHTMLEndpoint("/pages/{id}", "Show a page", "object"),
				"archive": embeddedJSONHTMLEndpoint("/archive", "List archived pages", "array"),
			},
		},
		"typed_api": {
			Description: "Typed API data",
			Endpoints: map[string]spec.Endpoint{
				"status": {
					Method:      "GET",
					Path:        "/api/status",
					Description: "Status",
					Response:    spec.ResponseDef{Type: "object"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")

	assert.Contains(t, syncSrc, "func syncResource(")
	assert.Contains(t, helpersSrc, "func syncErrorJSON(")
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateJSONMajoritySyncKeepsGenericTemplate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-majority")
	apiSpec.Resources["pages"] = spec.Resource{
		Description: "HTML pages",
		Endpoints: map[string]spec.Endpoint{
			"show": {
				Method:         "GET",
				Path:           "/pages/{id}",
				Description:    "Show a page",
				ResponseFormat: spec.ResponseFormatHTML,
				HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
				Response:       spec.ResponseDef{Type: "object"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")

	assert.Contains(t, syncSrc, "func syncResource(")
	assert.Contains(t, helpersSrc, "func syncErrorJSON(")
	assert.Contains(t, helpersSrc, "func parseSyncUserParams(")
	assert.Contains(t, helpersSrc, "func parseSyncKVFlags(")
	assert.Contains(t, helpersSrc, "func isSyncAccessWarning(")
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func embeddedJSONHTMLEndpoint(path, description, responseType string) spec.Endpoint {
	return spec.Endpoint{
		Method:         "GET",
		Path:           path,
		Description:    description,
		ResponseFormat: spec.ResponseFormatHTML,
		HTMLExtract: &spec.HTMLExtract{
			Mode:     spec.HTMLExtractModeEmbeddedJSON,
			JSONPath: "props.pageProps.items",
		},
		Response: spec.ResponseDef{Type: responseType},
	}
}

func htmlPageEndpoint(path, description, responseType string) spec.Endpoint {
	return spec.Endpoint{
		Method:         "GET",
		Path:           path,
		Description:    description,
		ResponseFormat: spec.ResponseFormatHTML,
		HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
		Response:       spec.ResponseDef{Type: responseType},
	}
}

// TestGenerateBinarySyncablesEmitSyncStub covers the #4342 shape: a CLI whose
// syncable resources are binary/text (sitemap indexes, downloads) has no JSON
// enumeration for spec-driven sync, so it must get the sync stub — not a
// generated sync that stamps sync_state and reports success over an empty
// store. Before the fix, binary resources counted as JSON-syncable and diluted
// the HTML-page-mode ratio below the stub threshold.
func TestGenerateBinarySyncablesEmitSyncStub(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("binary-syncables")
	apiSpec.Resources = map[string]spec.Resource{
		"page": {
			Description: "Documentation sitemap",
			Endpoints: map[string]spec.Endpoint{
				"index": {
					Method:         "GET",
					Path:           "/sitemap.xml",
					Description:    "Fetch the documentation sitemap",
					ResponseFormat: spec.ResponseFormatBinary,
					Response:       spec.ResponseDef{Type: "array"},
				},
			},
		},
		"product": {
			Description: "Product sitemap",
			Endpoints: map[string]spec.Endpoint{
				"index": {
					Method:         "GET",
					Path:           "/product-sitemap.xml",
					Description:    "Fetch the product sitemap",
					ResponseFormat: spec.ResponseFormatBinary,
					Response:       spec.ResponseDef{Type: "array"},
				},
			},
		},
		"support": {
			Description: "Support article export",
			Endpoints: map[string]spec.Endpoint{
				"index": {
					Method:         "GET",
					Path:           "/support-index.txt",
					Description:    "Fetch the support article index",
					ResponseFormat: spec.ResponseFormatText,
					Response:       spec.ResponseDef{Type: "array"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.True(t, gen.shouldEmitHTMLSyncStub())
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "generic spec-driven sync template does not fit predominantly non-JSON endpoints")
	assert.NotContains(t, syncSrc, "func syncResource(")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

// TestGenerateMinorityBinarySyncableKeepsGenericSync guards the other
// direction: one binary resource among JSON list resources must not push the
// CLI into the stub — the JSON resources still enumerate fine.
func TestGenerateMinorityBinarySyncableKeepsGenericSync(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("binary-minority")
	apiSpec.Resources = map[string]spec.Resource{
		"sitemap": {
			Description: "Sitemap",
			Endpoints: map[string]spec.Endpoint{
				"index": {
					Method:         "GET",
					Path:           "/sitemap.xml",
					Description:    "Fetch the sitemap",
					ResponseFormat: spec.ResponseFormatBinary,
					Response:       spec.ResponseDef{Type: "array"},
				},
			},
		},
		"articles": {
			Description: "Articles",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/articles", Description: "List articles", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"authors": {
			Description: "Authors",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/authors", Description: "List authors", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"tags": {
			Description: "Tags",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/tags", Description: "List tags", Response: spec.ResponseDef{Type: "array"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.False(t, gen.shouldEmitHTMLSyncStub())
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "func syncResource(")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
