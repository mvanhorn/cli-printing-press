package pipeline

import (
	"encoding/json"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A manifest carrying the new creator + contributors shape round-trips
// through JSON unchanged.
func TestCLIManifestCreatorContributorsRoundTrip(t *testing.T) {
	m := CLIManifest{
		SchemaVersion: CurrentCLIManifestSchemaVersion,
		CLIName:       "acme-pp-cli",
		Creator:       &spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"},
		Contributors: []spec.Person{
			{Handle: "jane-doe", Name: "Jane Doe"},
			{Handle: "mvanhorn", Name: "Matt Van Horn"},
		},
	}

	b, err := json.Marshal(m)
	require.NoError(t, err)

	var back CLIManifest
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, m.Creator, back.Creator)
	assert.Equal(t, m.Contributors, back.Contributors)
}

// A legacy manifest written before this change (only owner/printer/printer_name)
// unmarshals with a nil Creator and the legacy fields intact — the resolver
// (U2) is responsible for the fallback; the struct must not lose the data.
func TestCLIManifestLegacyOnlyUnmarshals(t *testing.T) {
	legacy := `{
		"cli_name": "acme-pp-cli",
		"owner": "trevin-chow",
		"printer": "trevin-chow",
		"printer_name": "Trevin Chow"
	}`

	var m CLIManifest
	require.NoError(t, json.Unmarshal([]byte(legacy), &m))
	assert.Nil(t, m.Creator)
	assert.Empty(t, m.Contributors)
	assert.Equal(t, "trevin-chow", m.Owner)
	assert.Equal(t, "trevin-chow", m.Printer)
	assert.Equal(t, "Trevin Chow", m.PrinterName)
}

// During the transition window both shapes coexist; neither is dropped on
// round-trip.
func TestCLIManifestNewAndLegacyCoexist(t *testing.T) {
	both := `{
		"cli_name": "acme-pp-cli",
		"creator": {"handle": "trevin-chow", "name": "Trevin Chow"},
		"contributors": [{"handle": "jane-doe", "name": "Jane Doe"}],
		"owner": "trevin-chow",
		"printer": "trevin-chow",
		"printer_name": "Trevin Chow"
	}`

	var m CLIManifest
	require.NoError(t, json.Unmarshal([]byte(both), &m))
	require.NotNil(t, m.Creator)
	assert.Equal(t, "trevin-chow", m.Creator.Handle)
	assert.Len(t, m.Contributors, 1)
	assert.Equal(t, "trevin-chow", m.Printer)
}

// An empty creator must not serialize as `"creator":{}` in the manifest.
func TestCLIManifestEmptyCreatorOmitted(t *testing.T) {
	m := CLIManifest{SchemaVersion: CurrentCLIManifestSchemaVersion, CLIName: "acme-pp-cli"}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"creator"`)
	assert.NotContains(t, string(b), `"contributors"`)
}
