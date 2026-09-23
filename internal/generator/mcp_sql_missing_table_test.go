package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPSQLMissingTableSentinelSurvivesWidgetsResource locks the generated
// MCP SQL test to a table name the store will not create. A spec that
// declares a widgets resource emits a widgets domain table, so a hardcoded
// SELECT * FROM widgets succeeds and the suite goes red.
func TestMCPSQLMissingTableSentinelSurvivesWidgetsResource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("widgetdepot")
	apiSpec.Types = map[string]spec.TypeDef{
		"Widget": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "name", Type: "string"},
			{Name: "colour", Type: "string"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Manage widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/widgets", Description: "List widgets", Response: spec.ResponseDef{Type: "array", Item: "Widget"}},
				"get":  {Method: "GET", Path: "/widgets/{id}", Description: "Get a widget", Response: spec.ResponseDef{Type: "object", Item: "Widget"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "widgetdepot-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	assert.Contains(t, string(storeSrc), `CREATE TABLE IF NOT EXISTS "widgets"`,
		"the fixture must emit a widgets domain table or the collision is not exercised")

	toolsTest, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools_test.go"))
	require.NoError(t, err)
	toolsTestSrc := string(toolsTest)
	assert.Contains(t, toolsTestSrc, `const missingTable = "__pp_missing_table__"`)
	assert.NotContains(t, toolsTestSrc, "SELECT * FROM widgets")

	runGoCommand(t, outputDir, "test", "./...")
}
