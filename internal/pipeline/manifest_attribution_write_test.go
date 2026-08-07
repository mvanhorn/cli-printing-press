package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readManifest(t *testing.T, dir string) CLIManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var m CLIManifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// A generate run writes the creator + contributors and the dual-written
// legacy fields side by side.
func TestWriteManifestForGenerate_CreatorContributorsAndDualWrite(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:      "acme",
		SpecSrcs:     []string{"https://example.com/openapi.json"},
		OutputDir:    dir,
		RunID:        "20260527-101010",
		Creator:      spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"},
		Contributors: []spec.Person{{Handle: "jane-doe", Name: "Jane Doe"}},
		Owner:        "trevin-chow",
		Printer:      "trevin-chow",
		PrinterName:  "Trevin Chow",
	}))

	m := readManifest(t, dir)
	require.NotNil(t, m.Creator)
	assert.Equal(t, "trevin-chow", m.Creator.Handle)
	assert.Equal(t, "Trevin Chow", m.Creator.Name)
	require.Len(t, m.Contributors, 1)
	assert.Equal(t, "jane-doe", m.Contributors[0].Handle)
	// Dual-written legacy fields.
	assert.Equal(t, "trevin-chow", m.Owner)
	assert.Equal(t, "trevin-chow", m.Printer)
	assert.Equal(t, "Trevin Chow", m.PrinterName)
}

// A same-lineage regen that carries no attribution preserves the persisted
// creator and contributors.
func TestWriteManifestForGenerate_SameLineagePreserves(t *testing.T) {
	dir := t.TempDir()
	base := GenerateManifestParams{
		APIName:   "acme",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
	}
	first := base
	first.RunID = "20260527-101010"
	first.Creator = spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"}
	first.Contributors = []spec.Person{{Handle: "jane-doe", Name: "Jane Doe"}}
	require.NoError(t, WriteManifestForGenerate(first))

	// Regen with no attribution (nil contributors, zero creator).
	second := base
	second.RunID = "20260601-120000"
	require.NoError(t, WriteManifestForGenerate(second))

	m := readManifest(t, dir)
	require.NotNil(t, m.Creator, "creator must survive a same-lineage regen")
	assert.Equal(t, "trevin-chow", m.Creator.Handle)
	require.Len(t, m.Contributors, 1, "contributors must survive a same-lineage regen")
	assert.Equal(t, "jane-doe", m.Contributors[0].Handle)
}

// AE5: a cross-lineage regen (different api_name) must not resurrect the prior
// CLI's contributor list.
func TestWriteManifestForGenerate_CrossLineageNoResurrection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:      "old-api",
		SpecSrcs:     []string{"https://example.com/old.json"},
		OutputDir:    dir,
		RunID:        "20260527-101010",
		Creator:      spec.Person{Handle: "old-creator", Name: "Old Creator"},
		Contributors: []spec.Person{{Handle: "old-contrib", Name: "Old Contrib"}},
	}))

	// Regen for a DIFFERENT API into the same dir, carrying no contributors.
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "new-api",
		SpecSrcs:  []string{"https://example.com/new.json"},
		OutputDir: dir,
		RunID:     "20260601-120000",
		Creator:   spec.Person{Handle: "new-creator", Name: "New Creator"},
	}))

	m := readManifest(t, dir)
	assert.Equal(t, "new-api", m.APIName)
	assert.Empty(t, m.Contributors, "cross-lineage regen must not resurrect the prior contributors")
	require.NotNil(t, m.Creator)
	assert.Equal(t, "new-creator", m.Creator.Handle, "creator must be the new run's, not the stale one")
}

// A non-nil empty contributors slice is the explicit-clear signal.
func TestWriteManifestForGenerate_ExplicitClearContributors(t *testing.T) {
	dir := t.TempDir()
	base := GenerateManifestParams{
		APIName:   "acme",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
		Creator:   spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"},
	}
	first := base
	first.RunID = "20260527-101010"
	first.Contributors = []spec.Person{{Handle: "jane-doe", Name: "Jane Doe"}}
	require.NoError(t, WriteManifestForGenerate(first))

	second := base
	second.RunID = "20260601-120000"
	second.Contributors = []spec.Person{} // explicit clear
	require.NoError(t, WriteManifestForGenerate(second))

	m := readManifest(t, dir)
	assert.Empty(t, m.Contributors, "explicit empty slice must clear the persisted contributors")
	require.NotNil(t, m.Creator, "creator is permanent and must remain")
}
