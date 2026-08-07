package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaTrafficAnalysisPrintsJSONSchema(t *testing.T) {
	cmd := newSchemaCmd()
	cmd.SetArgs([]string{"traffic-analysis"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &schema))
	assert.Equal(t, "CLI Printing Press traffic-analysis.json", schema["title"])
	properties := schema["properties"].(map[string]any)
	secondaryHosts := properties["secondary_hosts"].(map[string]any)
	assert.Equal(t, "#/$defs/secondary_host", secondaryHosts["items"].(map[string]any)["$ref"])
	auth := properties["auth"].(map[string]any)
	authProperties := auth["properties"].(map[string]any)
	captchaPreflight := authProperties["captcha_preflight"].(map[string]any)
	defs := schema["$defs"].(map[string]any)
	assert.Contains(t, defs, "secondary_host")
	assert.Contains(t, output, `"confidence": {"type": "number"`)
	assert.Equal(t, "boolean", captchaPreflight["type"])
	assert.Contains(t, output, `"endpoint_clusters"`)
}

func TestSchemaPhase5MarkerPrintsJSONSchema(t *testing.T) {
	cmd := newSchemaCmd()
	cmd.SetArgs([]string{"phase5-marker", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &schema))
	assert.Equal(t, "CLI Printing Press phase5-acceptance.json", schema["title"])
	// Every field here must be one the runner can actually emit; see
	// TestPhase5Marker_SchemaDeclaresNoPhantomProperties. cli_name,
	// tests_total, completed_at and summary were removed because
	// Phase5GateMarker never produced them — a schema promise nothing kept.
	for _, field := range []string{
		"schema_version",
		"run_id",
		"api_name",
		"level",
		"status",
		"matrix_size",
		"tests_passed",
		"tests_skipped",
		"tests_unverified",
		"tests_failed",
		"coverage_hollow",
		"hollow_features",
		"skip_reason",
		"source_fingerprint",
		"source_files",
	} {
		assert.Contains(t, output, `"`+field+`"`)
	}
}

func TestSchemaPhase5SkipPrintsJSONSchema(t *testing.T) {
	cmd := newSchemaCmd()
	cmd.SetArgs([]string{"phase5-skip"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &schema))
	assert.Equal(t, "CLI Printing Press phase5-skip.json", schema["title"])
	for _, field := range []string{"schema_version", "run_id", "api_name", "cli_name", "status", "skip_reason", "auth_context", "source_fingerprint", "source_files"} {
		assert.Contains(t, output, `"`+field+`"`)
	}
	assert.Contains(t, output, `"local_network_only"`)
}

func TestSchemaUnknownNameFails(t *testing.T) {
	cmd := newSchemaCmd()
	cmd.SetArgs([]string{"unknown-name"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown command "unknown-name"`)
}
