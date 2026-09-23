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
// SELECT * FROM widgets succeeds and the suite goes red. A resource spelled
// like the sentinel is legal too; schema naming rewrites its hyphen, so the
// quoted query still misses.
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
		"pp-missing-table": {
			Description: "Resource whose domain table normalizes away from the SQL sentinel",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/pp-missing-table", Description: "List rows", Response: spec.ResponseDef{Type: "array", Item: "Widget"}},
				"get":  {Method: "GET", Path: "/pp-missing-table/{id}", Description: "Get a row", Response: spec.ResponseDef{Type: "object", Item: "Widget"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "widgetdepot-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeText := string(storeSrc)
	assert.Contains(t, storeText, `CREATE TABLE IF NOT EXISTS "widgets"`,
		"the fixture must emit a widgets domain table or the collision is not exercised")
	assert.Contains(t, storeText, `CREATE TABLE IF NOT EXISTS "pp_missing_table"`,
		"the hyphenated resource must still emit its normalized domain table")
	assert.NotContains(t, storeText, `CREATE TABLE IF NOT EXISTS "pp-missing-table"`,
		"schema naming must not preserve the hyphenated sentinel as a table")

	toolsTest, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools_test.go"))
	require.NoError(t, err)
	toolsTestSrc := string(toolsTest)
	assert.Contains(t, toolsTestSrc, `const missingTable = "pp-missing-table"`)
	assert.Contains(t, toolsTestSrc, "`SELECT * FROM \"` + missingTable + `\"`")
	assert.NotContains(t, toolsTestSrc, "SELECT * FROM widgets")

	runGoCommand(t, outputDir, "test", "./...")
}
