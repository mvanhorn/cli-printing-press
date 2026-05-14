package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: a promoted endpoint that is both UsesBinaryResponse and
// paginated previously emitted a top-level `headerOverrides` declaration
// that the paginated helper call never referenced, so the generated Go
// file failed to compile with "headerOverrides declared and not used".
// The fix moves headerOverrides into each branch that actually uses it.
func TestGenerateBinaryPaginatedPromotedCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("audioapi")
	apiSpec.Resources = map[string]spec.Resource{
		"voices": {
			Description: "Voices",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         "GET",
					Path:           "/voices",
					Description:    "List voices",
					ResponseFormat: spec.ResponseFormatBinary,
					Pagination: &spec.Pagination{
						Type:           "cursor",
						LimitParam:     "limit",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Params: []spec.Param{
						{Name: "limit", Type: "integer", Description: "Page size"},
						{Name: "after", Type: "string", Description: "Cursor"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_voices.go")
	assert.NotContains(t, endpointSrc, `headerOverrides := map[string]string{"X-Printing-Press-Binary-Response": "true"}`,
		"paginated promoted branch must not declare an unused headerOverrides")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}
