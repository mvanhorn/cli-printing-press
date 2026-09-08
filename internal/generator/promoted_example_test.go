package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromotedExampleRewritesCollapsedEndpointToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-collapse")
	apiSpec.Resources = map[string]spec.Resource{
		"allocation": {
			Description: "Allocation",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/allocation",
					Description: "Get allocation",
					Example:     "  promoted-ex-collapse-pp-cli allocation get --form-code ABC --month 1",
					Params: []spec.Param{
						{Name: "form-code", Type: "string", Required: true},
						{Name: "month", Type: "string", Required: true},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-ex-collapse-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_allocation.go")
	assert.Contains(t, src, `Example:     "  promoted-ex-collapse-pp-cli allocation --form-code ABC --month 1"`)
	assert.NotContains(t, src, `allocation get --form-code`)
}

func TestPromotedExampleRewritePreservesQuotedMultiwordValues(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-quote")
	apiSpec.Resources = map[string]spec.Resource{
		"allocation": {
			Description: "Allocation",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/allocation",
					Description: "Get allocation",
					Example:     `  promoted-ex-quote-pp-cli allocation get --name "Jane Doe"`,
					Params:      []spec.Param{{Name: "name", Type: "string", Required: true}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-ex-quote-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_allocation.go")
	assert.Contains(t, src, `--name 'Jane Doe'`)
	assert.NotContains(t, src, `--name Jane Doe`)
	assert.NotContains(t, src, `allocation get --name`)
}

func TestPromotedExampleRewritesRenamedFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-flags")
	apiSpec.Resources = map[string]spec.Resource{
		"downloads": {
			Description: "Downloads",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/downloads",
					Description: "List downloads",
					Example:     "  promoted-ex-flags-pp-cli downloads list --term notices --paged 1",
					Params: []spec.Param{
						{Name: "term", FlagName: "category", Type: "string"},
						{Name: "paged", FlagName: "page", Type: "integer"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-ex-flags-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_downloads.go")
	assert.Contains(t, src, `Example:     "  promoted-ex-flags-pp-cli downloads --category notices --page 1"`)
	assert.NotContains(t, src, `--term`)
	assert.NotContains(t, src, `--paged`)
}

func TestPromotedExampleKeepsAlreadyRegisteredVerbatim(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-ok")
	apiSpec.Resources = map[string]spec.Resource{
		"lookup": {
			Description: "Lookup",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/lookup",
					Description: "Lookup a page",
					Example:     "  promoted-ex-ok-pp-cli lookup --q example-page",
					Params:      []spec.Param{{Name: "q", Type: "string", Required: true}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-ex-ok-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_lookup.go")
	assert.Contains(t, src, `Example:     "  promoted-ex-ok-pp-cli lookup --q example-page"`)
}

func TestPromotedExampleRefusesUnregisteredCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-refuse")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Assets",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/assets",
					Description: "Get assets",
					Example:     "  promoted-ex-refuse-pp-cli assets browse --year 2025",
					Params:      []spec.Param{{Name: "year", Type: "string"}},
				},
			},
		},
	}

	err := New(apiSpec, filepath.Join(t.TempDir(), "promoted-ex-refuse-pp-cli")).Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assets")
	assert.Contains(t, err.Error(), "not registered")
}

func TestPromotedExampleRefusesUnregisteredFlag(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-ex-flag-refuse")
	apiSpec.Resources = map[string]spec.Resource{
		"assets": {
			Description: "Assets",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/assets",
					Description: "Get assets",
					Example:     "  promoted-ex-flag-refuse-pp-cli assets --bogus 1",
					Params:      []spec.Param{{Name: "year", Type: "string"}},
				},
			},
		},
	}

	err := New(apiSpec, filepath.Join(t.TempDir(), "promoted-ex-flag-refuse-pp-cli")).Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assets")
	assert.Contains(t, err.Error(), "not registered")
}
