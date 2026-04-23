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
