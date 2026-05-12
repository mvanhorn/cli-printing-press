package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestPaginatedGetEmitsTruncationWarning verifies that generated CLIs include
// the emitTruncationWarning helper and that paginatedGet calls it on the
// single-page path. The warning is the signal agents rely on to detect
// page-1 truncation when --all is not passed (issue #1137).
func TestPaginatedGetEmitsTruncationWarning(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-warn")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-warn-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "func emitTruncationWarning(",
		"generated helpers.go should define emitTruncationWarning")
	require.Contains(t, string(helpersSrc), "emitTruncationWarning(data, nextCursorPath, hasMoreField)",
		"paginatedGet should call emitTruncationWarning on the single-page path")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}
