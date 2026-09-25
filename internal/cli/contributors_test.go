package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContributorsAddSyncsReadmeAndNotice(t *testing.T) {
	dir := t.TempDir()
	writeContributorFixture(t, dir)

	cmd := newContributorsCmd()
	cmd.SetArgs([]string{"add", "--dir", dir, "--handle", "h", "--name", "n"})
	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	assert.Contains(t, output, "Recorded contributor n (@h)")

	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Created by [@creator](https://github.com/creator) (Creator Name).")
	assert.Contains(t, string(readme), "Contributors: [@h](https://github.com/h) (n).")

	notice, err := os.ReadFile(filepath.Join(dir, "NOTICE"))
	require.NoError(t, err)
	assert.Contains(t, string(notice), "Created by Creator Name (@creator).")
	assert.Contains(t, string(notice), "Contributors:")
	assert.Contains(t, string(notice), "  - n (@h)")

	skill, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "---\nauthor: \"Creator Name\"\n---\n", string(skill))

	beforeReadme := string(readme)
	beforeNotice := string(notice)
	cmd = newContributorsCmd()
	cmd.SetArgs([]string{"add", "--dir", dir, "--handle", "h", "--name", "n"})
	output, err = runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	assert.Contains(t, output, "No change:")

	readme, err = os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	notice, err = os.ReadFile(filepath.Join(dir, "NOTICE"))
	require.NoError(t, err)
	assert.Equal(t, beforeReadme, string(readme))
	assert.Equal(t, beforeNotice, string(notice))
}

func TestContributorsAddRepairsSurfacesWhenAlreadyRecorded(t *testing.T) {
	dir := t.TempDir()
	writeContributorFixture(t, dir)
	manifest, err := pipeline.ReadCLIManifest(dir)
	require.NoError(t, err)
	manifest.Contributors = []spec.Person{{Handle: "h", Name: "n"}}
	require.NoError(t, pipeline.WriteCLIManifest(dir, manifest))

	cmd := newContributorsCmd()
	cmd.SetArgs([]string{"add", "--dir", dir, "--handle", "h", "--name", "n"})
	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	assert.Contains(t, output, "Synced contributor surfaces")

	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Contributors: [@h](https://github.com/h) (n).")
	assert.Contains(t, string(readme), "Created by [@creator](https://github.com/creator) (Creator Name).")
}

func TestContributorsAddLeavesAttributionUnchangedWhenNoticeMissing(t *testing.T) {
	dir := t.TempDir()
	writeContributorFixture(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "NOTICE")))
	beforeManifest, err := os.ReadFile(filepath.Join(dir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	beforeReadme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)

	cmd := newContributorsCmd()
	cmd.SetArgs([]string{"add", "--dir", dir, "--handle", "h", "--name", "n"})
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOTICE is missing")

	manifest, err := os.ReadFile(filepath.Join(dir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	assert.Equal(t, string(beforeManifest), string(manifest))
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, string(beforeReadme), string(readme))
	_, statErr := os.Stat(filepath.Join(dir, "NOTICE"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestPublishManifestContractChecksContributorSurfaces(t *testing.T) {
	stubPublishIdentityCommands(t,
		"",
		`#!/bin/sh
if [ "$1" = "api" ] && [ "$2" = "users/tmchow" ]; then
  echo '{"login":"tmchow","name":"Trevin Chow"}'
  exit 0
fi
exit 1
`,
	)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("Created by [@creator](https://github.com/creator) (Creator Name).\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "NOTICE"), []byte("Created by Creator Name (@creator).\n"), 0o644))

	manifest := pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "4.2.1",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260509-000000",
		Printer:              "tmchow",
		PrinterName:          "Trevin Chow",
		Creator:              &spec.Person{Handle: "creator", Name: "Creator Name"},
		Contributors:         []spec.Person{{Handle: "h", Name: "n"}},
	}
	issues := validatePublishManifestContract(dir, manifest)
	assert.Contains(t, issues, "README contributor byline does not match manifest contributors")
	assert.Contains(t, issues, "NOTICE contributor block does not match manifest contributors")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("Created by [@creator](https://github.com/creator) (Creator Name).\nContributors: [@h](https://github.com/h) (n).\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "NOTICE"), []byte("Created by Creator Name (@creator).\nContributors:\n  - n (@h)\n"), 0o644))
	issues = validatePublishManifestContract(dir, manifest)
	assert.Empty(t, issues)
}

func writeContributorFixture(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, pipeline.WriteCLIManifest(dir, pipeline.CLIManifest{
		SchemaVersion: pipeline.CurrentCLIManifestSchemaVersion,
		APIName:       "test",
		CLIName:       "test-pp-cli",
		Creator:       &spec.Person{Handle: "creator", Name: "Creator Name"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n\nCreated by [@creator](https://github.com/creator) (Creator Name).\n\n## Install\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "NOTICE"), []byte("test-pp-cli\nCreated by Creator Name (@creator).\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nauthor: \"Creator Name\"\n---\n"), 0o644))
}
