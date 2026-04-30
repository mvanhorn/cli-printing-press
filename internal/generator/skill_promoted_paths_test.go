// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillEmitsPromotedLeafPath confirms that when buildPromotedCommandPlan
// promotes a single-op resource to a leaf, the SKILL.md Command Reference
// emits the leaf form (`<cli> qr`), not the operation-id form
// (`<cli> qr get-qrcode`). The phantom op-id path is what the library repo's
// CI unknown-command verifier rejects.
func TestSkillEmitsPromotedLeafPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("imageapi")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {Method: "GET", Path: "/qr", Description: "Retrieve a QR code"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "imageapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "`imageapi-pp-cli qr` —",
		"single-op resource qr/get-qrcode should emit the leaf form `qr`")
	assert.NotContains(t, content, "`imageapi-pp-cli qr get-qrcode`",
		"phantom op-id path must not be emitted for promoted single-op resources")
}

// TestSkillRetainsOpIDFormForMultiOp confirms that resources with two or more
// endpoints continue to emit `<cli> <resource> <endpoint>` for each endpoint —
// the promotion gate fires only on single-op resources, and multi-op resources
// must keep their parent-and-children command tree.
func TestSkillRetainsOpIDFormForMultiOp(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multiopapi")
	apiSpec.Resources["links"] = spec.Resource{
		Description: "Manage links",
		Endpoints: map[string]spec.Endpoint{
			"create": {Method: "POST", Path: "/links", Description: "Create a link"},
			"delete": {Method: "DELETE", Path: "/links/{id}", Description: "Delete a link"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "multiopapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "`multiopapi-pp-cli links create`",
		"multi-op resource should emit per-endpoint paths")
	assert.Contains(t, content, "`multiopapi-pp-cli links delete`",
		"multi-op resource should emit per-endpoint paths")
	assert.NotContains(t, content, "`multiopapi-pp-cli links` —",
		"multi-op resource must not collapse to a leaf form")
}

// TestSkillMixedPromotedAndMulti confirms that a spec with one single-op
// resource and one multi-op resource produces both shapes correctly in the
// same SKILL.md.
func TestSkillMixedPromotedAndMulti(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mixedapi")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {Method: "GET", Path: "/qr", Description: "Retrieve a QR code"},
		},
	}
	apiSpec.Resources["links"] = spec.Resource{
		Description: "Manage links",
		Endpoints: map[string]spec.Endpoint{
			"create": {Method: "POST", Path: "/links", Description: "Create a link"},
			"delete": {Method: "DELETE", Path: "/links/{id}", Description: "Delete a link"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "mixedapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "`mixedapi-pp-cli qr` —",
		"single-op qr resource should emit leaf form")
	assert.NotContains(t, content, "`mixedapi-pp-cli qr get-qrcode`",
		"single-op qr should not emit phantom op-id path")
	assert.Contains(t, content, "`mixedapi-pp-cli links create`",
		"multi-op links resource should keep per-endpoint emission")
	assert.Contains(t, content, "`mixedapi-pp-cli links delete`",
		"multi-op links resource should keep per-endpoint emission")
}

// TestSkillPromotedLeafWithPositionalArgs confirms that a promoted single-op
// resource whose endpoint has a positional argument emits the leaf form WITH
// the positional preserved (e.g. `<cli> qr <url>`), not the operation-id form
// (`<cli> qr get-qrcode <url>`).
func TestSkillPromotedLeafWithPositionalArgs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("posargapi")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {
				Method:      "GET",
				Path:        "/qr",
				Description: "Retrieve a QR code",
				Params: []spec.Param{
					{Name: "url", Type: "string", Required: true, Positional: true, Description: "Target URL"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "posargapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "`posargapi-pp-cli qr <url>`",
		"promoted leaf form should preserve positional args from the endpoint")
	assert.NotContains(t, content, "`posargapi-pp-cli qr get-qrcode`",
		"phantom op-id path must not be emitted")
}

// TestGeneratorPopulatesPromotedMaps confirms that Generate() populates the
// PromotedResourceNames and PromotedEndpointNames maps on the Generator
// struct before rendering, so SKILL/README templates can read them via
// readmeData(). Regression guard for the Generate() call-order change that
// makes U1 work — buildPromotedCommandPlan must run BEFORE renderSingleFiles.
func TestGeneratorPopulatesPromotedMaps(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promomapapi")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {Method: "GET", Path: "/qr", Description: "Retrieve a QR code"},
		},
	}
	// Multi-op resource: should NOT appear in PromotedResourceNames
	apiSpec.Resources["links"] = spec.Resource{
		Description: "Manage links",
		Endpoints: map[string]spec.Endpoint{
			"create": {Method: "POST", Path: "/links", Description: "Create a link"},
			"delete": {Method: "DELETE", Path: "/links/{id}", Description: "Delete a link"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promomapapi-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.NotNil(t, gen.PromotedResourceNames, "PromotedResourceNames must be populated")
	assert.True(t, gen.PromotedResourceNames["qr"], "single-op qr should be marked promoted")
	assert.False(t, gen.PromotedResourceNames["links"], "multi-op links must NOT be marked promoted")

	require.NotNil(t, gen.PromotedEndpointNames, "PromotedEndpointNames must be populated")
	assert.Equal(t, "get-qrcode", gen.PromotedEndpointNames["qr"],
		"PromotedEndpointNames maps the resource to its single endpoint name")
}

// TestReadmeDataCarriesPromotedMaps confirms that readmeData() forwards the
// Generator's promoted maps into readmeTemplateData so skill.md.tmpl can
// read them via {{index $.PromotedResourceNames $name}}.
func TestReadmeDataCarriesPromotedMaps(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("readmemapapi")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {Method: "GET", Path: "/qr", Description: "Retrieve a QR code"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "readmemapapi-pp-cli")
	gen := New(apiSpec, outputDir)
	// Generate populates the maps on g; readmeData reads them back out.
	require.NoError(t, gen.Generate())

	data := gen.readmeData()
	require.NotNil(t, data.PromotedResourceNames)
	assert.True(t, data.PromotedResourceNames["qr"])
	require.NotNil(t, data.PromotedEndpointNames)
	assert.Equal(t, "get-qrcode", data.PromotedEndpointNames["qr"])
}

// Sanity: the SKILL.md file's command reference does not mention a phantom
// path in any of the above test cases. This is partial coverage for the
// `unknown-command` semantic check that the library repo's CI runs (full
// coverage lands in U2 when the check is implemented in this repo's
// canonical script).
func TestSkillCommandReferenceDoesNotEmitPhantomPaths(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("phantomcheck")
	apiSpec.Resources["qr"] = spec.Resource{
		Description: "Generate QR codes",
		Endpoints: map[string]spec.Endpoint{
			"get-qrcode": {Method: "GET", Path: "/qr", Description: "Retrieve a QR code"},
		},
	}
	apiSpec.Resources["tokens"] = spec.Resource{
		Description: "Issue access tokens",
		Endpoints: map[string]spec.Endpoint{
			"create-referrals-embed": {Method: "POST", Path: "/tokens/embed/referrals/links", Description: "Create a referrals embed token"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "phantomcheck-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	// Neither phantom op-id path may appear in the Command Reference.
	cmdRefStart := strings.Index(content, "## Command Reference")
	require.GreaterOrEqual(t, cmdRefStart, 0, "SKILL must have a Command Reference section")
	cmdRef := content[cmdRefStart:]

	assert.NotContains(t, cmdRef, "qr get-qrcode",
		"Command Reference must not emit the phantom qr/get-qrcode path")
	assert.NotContains(t, cmdRef, "tokens create-referrals-embed",
		"Command Reference must not emit the phantom tokens/create-referrals-embed path")
}
