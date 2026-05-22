package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

func TestBrowserSniffCmdPreserveHostsWritesBaseURLOverrides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.json")
	outputPath := filepath.Join(dir, "spec.yaml")
	capture := browsersniff.EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []browsersniff.EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				ResponseContentType: "application/json",
				ResponseBody:        `{"status":"ok"}`,
			},
			{Method: "GET", URL: "https://api.example.com/v1/items", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"items":[]}`},
			{Method: "GET", URL: "https://api.example.com/v1/items/item_1", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"id":"item_1"}`},
			{Method: "GET", URL: "https://partner.example.net/v1/profiles", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"profiles":[]}`},
		},
	}
	data, err := json.Marshal(capture)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(capturePath, data, 0o600))

	cmd := newBrowserSniffCmd()
	cmd.SetArgs([]string{
		"--har", capturePath,
		"--output", outputPath,
		"--preserve-hosts",
	})

	require.NoError(t, cmd.Execute())

	specData, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	parsed, err := spec.ParseBytes(specData)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", parsed.BaseURL)
	require.Contains(t, parsed.Resources, "profiles")
	profiles := parsed.Resources["profiles"].Endpoints["list_profiles"]
	assert.Equal(t, "https://partner.example.net", profiles.BaseURL)
}

func TestPrintingPressSkillDocumentsPreserveHostsForComboHAR(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"),
		filepath.Join("..", "..", "skills", "printing-press", "references", "browser-sniff-capture.md"),
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		text := string(data)
		assert.Contains(t, text, "source-priority.json")
		assert.Contains(t, text, "two or more sources")
		assert.Contains(t, text, "--preserve-hosts")
	}
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

	_, err := writeBrowserSniffOutputs(apiSpec, &browsersniff.TrafficAnalysis{Version: "1"}, nil, blockingDir, analysisPath, "", browsersniff.AnalyzeOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preparing spec publish:")

	data, readErr := os.ReadFile(analysisPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old analysis", string(data))
	assert.DirExists(t, blockingDir)
	assert.FileExists(t, filepath.Join(blockingDir, "marker"))
}

func TestWriteBrowserSniffOutputsWritesSamplesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "out-spec.yaml")
	analysisPath := browsersniff.DefaultTrafficAnalysisPath(specPath)
	samplesPath := browsersniff.DefaultSamplesPath(specPath)

	capture := &browsersniff.EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []browsersniff.EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders: map[string]string{
					"Authorization": "Bearer eyJ.t.x",
					"Accept":        "application/json",
				},
			},
		},
	}

	apiSpec, err := browsersniff.AnalyzeCapture(capture)
	require.NoError(t, err)
	trafficAnalysis, err := browsersniff.AnalyzeTraffic(capture)
	require.NoError(t, err)

	written, err := writeBrowserSniffOutputs(apiSpec, trafficAnalysis, capture, specPath, analysisPath, samplesPath, browsersniff.AnalyzeOptions{})
	require.NoError(t, err)
	assert.Positive(t, written, "at least one sample file should be written")

	assert.FileExists(t, specPath)
	assert.FileExists(t, analysisPath)
	assert.DirExists(t, samplesPath)

	entries, err := os.ReadDir(samplesPath)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "samples directory should have at least one file")
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(samplesPath, entry.Name()))
		require.NoError(t, err)
		assert.Contains(t, string(data), browsersniff.RedactedSentinel, "Authorization should be redacted")
		assert.NotContains(t, string(data), "eyJ.t.x", "raw token must not leak")
	}
}

func TestWriteBrowserSniffOutputsRestoresSamplesDirOnSpecFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "out-spec.yaml")
	analysisPath := browsersniff.DefaultTrafficAnalysisPath(specPath)
	samplesPath := browsersniff.DefaultSamplesPath(specPath)

	// Pre-existing samples directory with a marker file. A subsequent failure
	// during spec publish must restore the pre-existing samples directory
	// intact (no half-overwritten state).
	require.NoError(t, os.MkdirAll(samplesPath, 0o755))
	markerPath := filepath.Join(samplesPath, "pre-existing.txt")
	require.NoError(t, os.WriteFile(markerPath, []byte("keep me"), 0o600))

	// Block spec publish by creating a non-empty directory at outputPath.
	require.NoError(t, os.MkdirAll(specPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(specPath, "blocker"), []byte("x"), 0o600))

	capture := &browsersniff.EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []browsersniff.EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}
	apiSpec, err := browsersniff.AnalyzeCapture(capture)
	require.NoError(t, err)
	trafficAnalysis, err := browsersniff.AnalyzeTraffic(capture)
	require.NoError(t, err)

	_, err = writeBrowserSniffOutputs(apiSpec, trafficAnalysis, capture, specPath, analysisPath, samplesPath, browsersniff.AnalyzeOptions{})
	require.Error(t, err, "should fail because outputPath is a non-empty directory")

	// Pre-existing samples directory must be restored intact.
	assert.DirExists(t, samplesPath)
	preserved, readErr := os.ReadFile(markerPath)
	require.NoError(t, readErr)
	assert.Equal(t, "keep me", string(preserved))
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
