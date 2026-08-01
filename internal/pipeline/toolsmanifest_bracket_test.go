package pipeline

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifestBracketParamNameAndWireName pins the manifest half of the MCP
// property-key sanitization: a vendor bracket query param (fathom-style
// recorded_by[]) must surface in tools-manifest.json with the SANITIZED
// public name and the raw bracket form preserved as wire_name, while a clean
// param keeps wire_name omitted (no churn for legal names).
func TestManifestBracketParamNameAndWireName(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "bracket-manifest",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"meetings": {
				Description: "Meetings",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/meetings",
						Description: "List meetings",
						Params: []spec.Param{
							{Name: "recorded_by[]", Type: "array", ItemType: "string", Description: "Recorder emails"},
							{Name: "cursor", Type: "string", Description: "Pagination cursor"},
						},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))
	got, err := ReadToolsManifest(dir)
	require.NoError(t, err)

	require.Len(t, got.Tools, 1)
	tool := got.Tools[0]
	require.Equal(t, "meetings_list", tool.Name)
	require.Len(t, tool.Params, 2)

	assert.Equal(t, "recorded_by", tool.Params[0].Name,
		"manifest param name must be the sanitized public input name")
	assert.Equal(t, "recorded_by[]", tool.Params[0].WireName,
		"manifest wire_name must preserve the raw bracket wire key")

	assert.Equal(t, "cursor", tool.Params[1].Name)
	assert.Equal(t, "", tool.Params[1].WireName,
		"legal names must keep wire_name omitted — no churn when name == wire name")
}

// TestManifestNamesLockstepWithSchemaKeys pins the manifest-suffix residual:
// uniqueManifestParamName's silent "-2" suffixing must be a NO-OP for every
// spec that generates. foo[] and foo collide pre-dedup (their public names
// both sanitize to "foo"); the generator's flag-collision dedup pass rescues
// the collision by assigning an IdentName, so post-generation every manifest
// param name must equal its param's PublicInputName() verbatim — the manifest
// suffixer invents nothing and manifest names cannot diverge from the emitted
// MCP schema keys on the Generate path. (Residual-collision specs never get
// here — they fail generation via the generator's post-dedup assertion. The
// direct-WriteToolsManifest publish path bypasses the generator gate by
// construction; that pre-existing residual is dispositioned in the upstream
// PR's Output Contract.)
func TestManifestNamesLockstepWithSchemaKeys(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:    "lockstep",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/lockstep-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Params: []spec.Param{
							{Name: "foo[]", Type: "array", ItemType: "string", Description: "Array form"},
							{Name: "foo", Type: "string", Description: "Scalar form"},
						},
					},
				},
			},
		},
	}

	// Generate() runs the flag-collision dedup pass over the SAME in-memory
	// spec, assigning final IdentNames (internal/pipeline already imports
	// internal/generator, so this direction is cycle-free).
	require.NoError(t, generator.New(apiSpec, t.TempDir()).Generate())

	ep := apiSpec.Resources["items"].Endpoints["list"]
	require.NotEqual(t, ep.Params[0].PublicInputName(), ep.Params[1].PublicInputName(),
		"dedup must have rescued the foo[]/foo public-name collision")

	tool := buildManifestTool("items_list", ep.Description, "", ep,
		func(p spec.Param) string { return p.Description })
	require.Len(t, tool.Params, len(ep.Params))
	for i, p := range ep.Params {
		assert.Equal(t, p.PublicInputName(), tool.Params[i].Name,
			"manifest param %d name must equal PublicInputName() verbatim — the manifest suffixer must be a no-op on the Generate path", i)
	}
}
