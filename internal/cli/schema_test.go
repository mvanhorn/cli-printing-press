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
	assert.Contains(t, output, `"confidence": {"type": "number"`)
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
	for _, field := range []string{
		"schema_version",
		"run_id",
		"api_name",
		"cli_name",
		"level",
		"status",
		"matrix_size",
		"tests_total",
		"tests_passed",
		"tests_failed",
		"completed_at",
		"summary",
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
	for _, field := range []string{"schema_version", "run_id", "api_name", "cli_name", "status", "skip_reason", "auth_context"} {
		assert.Contains(t, output, `"`+field+`"`)
	}
}

func TestSchemaUnknownNameFails(t *testing.T) {
	cmd := newSchemaCmd()
	cmd.SetArgs([]string{"unknown-name"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown command "unknown-name"`)
}
