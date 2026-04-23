package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserSniffCmdRejectsDomainMismatchOnAuthFrom(t *testing.T) {
	t.Parallel()

	cmd := newBrowserSniffCmd()
	outputPath := filepath.Join(t.TempDir(), "spec.yaml")
	cmd.SetArgs([]string{
		"--har", filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"),
		"--auth-from", filepath.Join("..", "..", "testdata", "sniff", "sample-auth-capture-mismatch.json"),
		"--output", outputPath,
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.EqualError(t, err, "auth captured for other.example.com cannot be used with hn.algolia.com (domain mismatch)")
}

func TestBrowserSniffCmdWritesSpecAndExplicitTrafficAnalysis(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "spec.yaml")
	analysisPath := filepath.Join(t.TempDir(), "traffic-analysis.json")
	cmd := newBrowserSniffCmd()
	cmd.SetArgs([]string{
		"--har", filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"),
		"--output", outputPath,
		"--analysis-output", analysisPath,
	})

	require.NoError(t, cmd.Execute())

	require.FileExists(t, outputPath)
	data, err := os.ReadFile(analysisPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version": "1"`)
	assert.Contains(t, string(data), `"endpoint_clusters"`)
}

func TestBrowserSniffCmdDerivesTrafficAnalysisPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "sample-spec.yaml")
	cmd := newBrowserSniffCmd()
	cmd.SetArgs([]string{
		"--har", filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"),
		"--output", outputPath,
	})

	require.NoError(t, cmd.Execute())

	require.FileExists(t, outputPath)
	require.FileExists(t, filepath.Join(dir, "sample-spec-traffic-analysis.json"))
}

func TestBrowserSniffCmdReportsTrafficAnalysisWriteFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(blockingFile, []byte("not a dir"), 0o600))

	cmd := newBrowserSniffCmd()
	cmd.SetArgs([]string{
		"--har", filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"),
		"--output", filepath.Join(dir, "spec.yaml"),
		"--analysis-output", filepath.Join(blockingFile, "traffic-analysis.json"),
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing traffic analysis:")
	assert.NoFileExists(t, filepath.Join(dir, "spec.yaml"))
}

func TestWriteBrowserSniffOutputsRestoresExistingFilesWhenSpecPublishFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	require.NoError(t, os.WriteFile(analysisPath, []byte("old analysis"), 0o600))

	blockingDir := filepath.Join(dir, "published-spec")
	require.NoError(t, os.Mkdir(blockingDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(blockingDir, "marker"), []byte("keep"), 0o600))

	apiSpec := &spec.APISpec{
		Name:        "sample",
		Description: "Sample API",
		Version:     "0.1.0",
		BaseURL:     "https://api.example.com",
		Auth:        spec.AuthConfig{Type: "none"},
		Config:      spec.ConfigSpec{Format: "toml", Path: "~/.config/sample-pp-cli/config.toml"},
		Resources:   map[string]spec.Resource{},
		Types:       map[string]spec.TypeDef{},
	}

	err := writeBrowserSniffOutputs(apiSpec, &browsersniff.TrafficAnalysis{Version: "1"}, blockingDir, analysisPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preparing spec publish:")

	data, readErr := os.ReadFile(analysisPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old analysis", string(data))
	assert.DirExists(t, blockingDir)
	assert.FileExists(t, filepath.Join(blockingDir, "marker"))
}

// newRootCmdForTest mirrors Execute()'s command tree construction for test-level
// command dispatch assertions.
func newRootCmdForTest() *cobra.Command {
	root := &cobra.Command{Use: "printing-press", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newBrowserSniffCmd())
	root.AddCommand(newCrowdSniffCmd())
	return root
}

func TestLegacySniffCommandReturnsUnknownCommand(t *testing.T) {
	t.Parallel()

	root := newRootCmdForTest()
	root.SetArgs([]string{"sniff", "--har", "/tmp/whatever.har"})
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))

	err := root.Execute()
	require.Error(t, err, "invoking legacy 'sniff' must fail after the rename")
	assert.Contains(t, err.Error(), "unknown command", "cobra should surface an unknown-command error")
}

func TestBrowserSniffAppearsInHelp(t *testing.T) {
	t.Parallel()

	root := newRootCmdForTest()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})

	require.NoError(t, root.Execute())
	out := buf.String()
	assert.Contains(t, out, "browser-sniff", "browser-sniff should be listed in help")
	assert.NotContains(t, lineWithToken(out, "sniff"), "\n  sniff ", "bare 'sniff' should not appear as a top-level command in help")
}

// lineWithToken is a trivial helper — the NotContains check above looks for the
// subcommand indent pattern cobra uses when listing commands.
func lineWithToken(s, _ string) string {
	// Normalize to make the NotContains assertion robust across cobra versions.
	return "\n" + strings.ReplaceAll(s, "\r\n", "\n")
}

func TestCrowdSniffStillWorksAfterBrowserSniffRename(t *testing.T) {
	t.Parallel()

	root := newRootCmdForTest()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"crowd-sniff", "--help"})

	require.NoError(t, root.Execute(), "crowd-sniff --help must still succeed after browser-sniff rename")
	out := buf.String()
	assert.Contains(t, out, "crowd-sniff", "crowd-sniff help output should reference the command name")
}
