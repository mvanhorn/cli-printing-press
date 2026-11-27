package cli

import (
	"archive/zip"
	"bytes"
	"context"
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

func TestAppSniffCommandAuditAndHARAndApkeep(t *testing.T) {
	t.Parallel()

	// 1. --audit-cli JSON mode without --output prints structured drift report.
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "bundle.js"), []byte(`
		const SHARDS = { pecan: "https://pecan.api.example-audit.test" };
		fetch("https://api.example-audit.test/v1/items", { method: "GET" });
		fetch("https://api.example-audit.test/v1/missing", { method: "POST" });
	`), 0o644))

	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "client"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "client", "client.go"), []byte(`
		package client
		func list() { _ = "/v1/items" }
	`), 0o644))

	var auditOut, auditErr bytes.Buffer
	auditCmd := newAppSniffCmdWithOptions(appSniffOptions{stdout: &auditOut, stderr: &auditErr})
	auditCmd.SetArgs([]string{"--app", srcDir, "--audit-cli", cliDir, "--json"})
	require.NoError(t, auditCmd.Execute())

	var auditPayload struct {
		Audit struct {
			CoveredCount  int `json:"covered_count"`
			MissingCount  int `json:"missing_count"`
			MissingShards []struct {
				Host string `json:"host"`
			} `json:"missing_shards"`
		} `json:"audit"`
	}
	require.NoError(t, json.Unmarshal(auditOut.Bytes(), &auditPayload))
	assert.Equal(t, 1, auditPayload.Audit.CoveredCount)
	assert.Equal(t, 1, auditPayload.Audit.MissingCount)
	require.Len(t, auditPayload.Audit.MissingShards, 1)
	assert.Equal(t, "pecan.api.example-audit.test", auditPayload.Audit.MissingShards[0].Host)

	// 2. --apkeep with injected CommandRunner + --har + --host + --top.
	harPath := filepath.Join(t.TempDir(), "cap.har")
	require.NoError(t, os.WriteFile(harPath, []byte(`{
		"log": {
			"entries": [
				{"request":{"method":"POST","url":"https://api.example-mobile.test/v1/sessions","headers":[]},"response":{"status":200,"content":{"mimeType":"application/json","text":"{}"}}},
				{"request":{"method":"GET","url":"https://other.example-other.test/v1/dropme","headers":[]},"response":{"status":200,"content":{"mimeType":"application/json","text":"{}"}}}
			]
		}
	}`), 0o644))

	fakeRunner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "apkeep", name)
		destDir := args[len(args)-1]
		apkPath := filepath.Join(destDir, "com.example.mobile.apk")
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("assets/index.android.bundle")
		require.NoError(t, err)
		_, err = w.Write([]byte(`fetch("https://api.example-mobile.test/v1/devices", { method: "GET" }); fetch("https://api.example-mobile.test/v1/profile", { method: "GET" });`))
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, os.WriteFile(apkPath, buf.Bytes(), 0o644))
		return []byte("downloaded"), nil
	}

	outDir := t.TempDir()
	specPath := filepath.Join(outDir, "mobile-spec.yaml")
	var mobOut, mobErr bytes.Buffer
	mobCmd := newAppSniffCmdWithOptions(appSniffOptions{runner: fakeRunner, stdout: &mobOut, stderr: &mobErr})
	mobCmd.SetArgs([]string{
		"--apkeep", "com.example.mobile",
		"--har", harPath,
		"--host", "api.example-mobile.test",
		"--top", "2",
		"--name", "Example Mobile",
		"--output", specPath,
		"--json",
	})
	require.NoError(t, mobCmd.Execute())

	var mobSummary map[string]any
	require.NoError(t, json.Unmarshal(mobOut.Bytes(), &mobSummary))
	assert.Equal(t, float64(2), mobSummary["endpoints"])
	assert.Equal(t, "https://api.example-mobile.test", mobSummary["primary_base_url"])

	parsed, err := spec.Parse(specPath)
	require.NoError(t, err)
	for _, r := range parsed.Resources {
		for _, ep := range r.Endpoints {
			assert.NotContains(t, ep.Path, "dropme")
		}
	}
}
