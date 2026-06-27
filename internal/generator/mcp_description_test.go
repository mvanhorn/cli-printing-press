package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPDescriptionSynthesizesThinOperationDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("thinmcp")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"Actions": {
			Description: "Action operations",
			Endpoints: map[string]spec.Endpoint{
				"List": {
					Method:      "GET",
					Path:        "/Actions",
					Description: "Use this to return multiple Actions.<br>Requires authentication.",
					Response:    spec.ResponseDef{Type: "array", Item: "Action"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "thinmcp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	tools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(tools)

	assert.Contains(t, content, `mcplib.WithDescription("List actions. Returns array of Action.")`)
	assert.NotContains(t, strings.ToLower(content), "use this to return multiple")
}
