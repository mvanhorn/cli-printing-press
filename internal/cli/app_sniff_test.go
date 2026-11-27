package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppSniffCommandEndToEndAndSchemaValid(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "api.js"), []byte(`
		const API_BASE = "https://api.example-widget.test";
		async function run() {
			await fetch("https://api.example-widget.test/v1/widgets", { method: "GET" });
			await fetch("https://api.example-widget.test/v1/widgets/${id}", {
				method: "POST",
				headers: { "authorization": "Bearer " + tok },
				body: JSON.stringify({ name: "sprocket", color: "blue" })
			});
			await axios.delete("https://api.example-widget.test/v1/search");
		}
	`), 0o644))

	outDir := t.TempDir()
	specPath := filepath.Join(outDir, "widget-spec.yaml")
	analysisPath := filepath.Join(outDir, "traffic-analysis.json")
	manifestPath := filepath.Join(outDir, "app-sniff-manifest.json")

	var stdout, stderr bytes.Buffer
	cmd := newAppSniffCmdWithOptions(appSniffOptions{stdout: &stdout, stderr: &stderr})
	cmd.SetArgs([]string{
		"--app", srcDir,
		"--name", "Example Widget",
		"--output", specPath,
		"--analysis-output", analysisPath,
		"--manifest-output", manifestPath,
		"--json",
	})

	require.NoError(t, cmd.Execute())

	var summary map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &summary))
	assert.Equal(t, specPath, summary["spec_path"])
	assert.Equal(t, analysisPath, summary["analysis_path"])
	assert.Equal(t, "https://api.example-widget.test", summary["primary_base_url"])

	// 1. Validate emitted spec parses through Printing Press's canonical spec loader.
	parsedSpec, err := spec.Parse(specPath)
	require.NoError(t, err)
	assert.Equal(t, "example-widget", parsedSpec.Name)
	assert.Equal(t, "https://api.example-widget.test", parsedSpec.BaseURL)
	assert.Equal(t, "sniffed", parsedSpec.SpecSource)
	assert.NotEmpty(t, parsedSpec.Resources)

	// 2. Validate emitted traffic-analysis.json parses via ReadTrafficAnalysis AND
	// satisfies Printing Press's strict traffic-analysis JSON Schema.
	analysis, err := browsersniff.ReadTrafficAnalysis(analysisPath)
	require.NoError(t, err)
	assert.Equal(t, "1", analysis.Version)
	assert.NotEmpty(t, analysis.EndpointClusters)

	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(trafficAnalysisSchemaJSON)))
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("traffic-analysis.schema.json", schemaDoc))
	compiledSchema, err := c.Compile("traffic-analysis.schema.json")
	require.NoError(t, err)

	analysisRaw, err := os.ReadFile(analysisPath)
	require.NoError(t, err)
	analysisDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(analysisRaw))
	require.NoError(t, err)
	require.NoError(t, compiledSchema.Validate(analysisDoc), "emitted traffic-analysis.json must satisfy schema")

	// 3. Verify manifest written cleanly.
	assert.FileExists(t, manifestPath)
}

func TestAppSniffCommandRequiresSingleSource(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	cmd := newAppSniffCmdWithOptions(appSniffOptions{stdout: &stdout, stderr: &stderr})
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify one input source")

	cmd2 := newAppSniffCmdWithOptions(appSniffOptions{stdout: &stdout, stderr: &stderr})
	cmd2.SetArgs([]string{"--app", "/tmp/a", "--android", "/tmp/b"})
	err = cmd2.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one primary source")
}
