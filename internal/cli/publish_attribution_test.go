package cli

import (
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A manifest that carries only the legacy printer fields (e.g. generated
// before the creator model) gets a creator backfilled at publish time so it
// satisfies the creator contract and reaches the registry.
func TestPublishAttributionBackfillsCreatorFromPrinter(t *testing.T) {
	m := manifestWithPublishAttributionFallbacks(pipeline.CLIManifest{
		Printer:     "trevin-chow",
		PrinterName: "Trevin Chow",
	})
	require.NotNil(t, m.Creator)
	assert.Equal(t, "trevin-chow", m.Creator.Handle)
	assert.Equal(t, "Trevin Chow", m.Creator.Name)
}

// A creator-only manifest (no legacy printer fields) backfills printer from the
// creator without consulting git/gh — so it validates on a box with no git
// identity.
func TestPublishAttributionBackfillsPrinterFromCreator(t *testing.T) {
	// Stub git + gh to return nothing: the backfill must come from the creator,
	// not the environment.
	stubPublishIdentityCommands(t, "#!/bin/sh\nexit 1\n", "#!/bin/sh\nexit 1\n")

	m := manifestWithPublishAttributionFallbacks(pipeline.CLIManifest{
		Creator: &spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"},
	})
	assert.Equal(t, "trevin-chow", m.Printer)
	assert.Equal(t, "Trevin Chow", m.PrinterName)
}

// An already-present creator is left untouched by the fallback.
func TestPublishAttributionKeepsExistingCreator(t *testing.T) {
	m := manifestWithPublishAttributionFallbacks(pipeline.CLIManifest{
		Creator:     &spec.Person{Handle: "jane-doe", Name: "Jane Doe"},
		Printer:     "trevin-chow",
		PrinterName: "Trevin Chow",
	})
	require.NotNil(t, m.Creator)
	assert.Equal(t, "jane-doe", m.Creator.Handle)
}

// A missing creator (and no legacy fields to backfill from) is reported.
func TestPublishManifestContractRequiresCreator(t *testing.T) {
	// Stub git + gh to return nothing so the fallback can't resolve attribution
	// from the ambient environment.
	stubPublishIdentityCommands(t, "#!/bin/sh\nexit 1\n", "#!/bin/sh\nexit 1\n")

	issues := validatePublishManifestContract(t.TempDir(), pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "4.2.1",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260509-000000",
	})
	joined := strings.Join(issues, "\n")
	assert.Contains(t, joined, "creator.handle")
	assert.Contains(t, joined, "creator.name")
}
