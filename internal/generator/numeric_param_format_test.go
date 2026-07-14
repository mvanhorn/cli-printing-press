package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedNumericPathAndQueryParamsUsePlainDecimalFormatting(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("numeric-param-format")
	apiSpec.Resources = map[string]spec.Resource{
		"tasks": {
			Description: "Tasks",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/task/{task_id}",
					Description: "Get a task",
					Params: []spec.Param{
						{Name: "task_id", Type: "string", Required: true, Positional: true},
						{Name: "team_id", Type: "number", Required: true, Description: "Workspace ID"},
					},
					Response: spec.ResponseDef{Type: "object", Item: "Task"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Task": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "numeric-param-format-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpersSrc, "func formatCLIParamValue(v any) string")
	require.Contains(t, helpersSrc, "strconv.FormatFloat(f, 'f', -1, 64)")

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, "func formatMCPParamValue(v any) string")
	require.Contains(t, mcpSrc, `return url.PathEscape(formatMCPParamValue(v))`)
	require.Contains(t, mcpSrc, `path = strings.Replace(path, placeholder, mcpPathValue(v), 1)`)
	require.Contains(t, mcpSrc, `params[binding.WireName] = formatMCPParamValue(v)`)
	require.NotContains(t, mcpSrc, `params[binding.WireName] = fmt.Sprintf("%v", v)`)

	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, "numeric-param-format-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/numeric-param-format-pp-cli")

	stdout, stderr := runGeneratedBinary(t, binaryPath, "tasks", "get", "CS-27102", "--team-id", "4653482", "--dry-run")
	output := stdout + stderr
	require.Contains(t, output, "team_id=4653482")
	require.NotContains(t, output, "4.653482e")
	require.False(t, strings.Contains(output, "e+06"), "dry-run output used scientific notation:\n%s", output)
}
