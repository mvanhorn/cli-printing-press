package pipeline

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubPromoteGitAttribution(t *testing.T, handle, name string) {
	t.Helper()
	orig := lookupPromoteGitAttribution
	lookupPromoteGitAttribution = func() (string, string) { return handle, name }
	t.Cleanup(func() { lookupPromoteGitAttribution = orig })
}

func TestBackfillPromoteManifestAttributionFillsCreatorFromGit(t *testing.T) {
	stubPromoteGitAttribution(t, "tmchow", "Trevin Chow")
	m := CLIManifest{}
	backfillPromoteManifestAttribution(&m)
	assert.Equal(t, "tmchow", m.Printer)
	assert.Equal(t, "Trevin Chow", m.PrinterName)
	require.NotNil(t, m.Creator)
	assert.Equal(t, "tmchow", m.Creator.Handle)
	assert.Equal(t, "Trevin Chow", m.Creator.Name)
}

func TestBackfillPromoteManifestAttributionKeepsExistingCreator(t *testing.T) {
	stubPromoteGitAttribution(t, "tmchow", "Trevin Chow")
	m := CLIManifest{
		Creator:     &spec.Person{Handle: "jane-doe", Name: "Jane Doe"},
		Printer:     "jane-doe",
		PrinterName: "Jane Doe",
	}
	backfillPromoteManifestAttribution(&m)
	require.NotNil(t, m.Creator)
	assert.Equal(t, "jane-doe", m.Creator.Handle)
	assert.Equal(t, "Jane Doe", m.Creator.Name)
}

func TestBackfillPromoteManifestAttributionReplacesPrinterSentinel(t *testing.T) {
	stubPromoteGitAttribution(t, "qazmataz", "qazmataz")
	m := CLIManifest{Printer: "USER", PrinterName: "USER"}
	backfillPromoteManifestAttribution(&m)
	assert.Equal(t, "qazmataz", m.Printer)
	require.NotNil(t, m.Creator)
	assert.Equal(t, "qazmataz", m.Creator.Handle)
}

func TestBackfillPromoteManifestAttributionSkipsSentinelCreator(t *testing.T) {
	stubPromoteGitAttribution(t, "", "")
	m := CLIManifest{Printer: "USER", PrinterName: "USER"}
	backfillPromoteManifestAttribution(&m)
	assert.Nil(t, m.Creator)
}

func TestBackfillPromoteManifestAttributionSkipsSentinelCreatorName(t *testing.T) {
	stubPromoteGitAttribution(t, "alice", "")
	m := CLIManifest{Printer: "USER", PrinterName: "USER"}
	backfillPromoteManifestAttribution(&m)
	require.NotNil(t, m.Creator)
	assert.Equal(t, "alice", m.Creator.Handle)
	assert.Empty(t, m.Creator.Name)
}
