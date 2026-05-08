package pipeline

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSeedProducesThinSeedTemplates(t *testing.T) {
	data := SeedData{
		APIName:     "petstore",
		OutputDir:   "tmp/petstore-cli",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecSource:  "manual",
		PipelineDir: "docs/plans/petstore-pipeline",
	}

	for _, phase := range PhaseOrder {
		t.Run(phase, func(t *testing.T) {
			rendered, err := RenderSeed(phase, data)
			require.NoError(t, err)

			require.Contains(t, rendered, "status: seed")
			require.Contains(t, rendered, "pipeline_phase: "+phase)
			require.Contains(t, rendered, "pipeline_api: "+data.APIName)
			require.NotContains(t, rendered, "## Implementation Units")
			require.Contains(t, rendered, "## What This Phase Must Produce")
			require.Contains(t, rendered, "## Prior Phase Outputs")

			lines := strings.Split(rendered, "\n")
			require.GreaterOrEqual(t, len(lines), 6)
			require.Equal(t, "---", lines[0])
			require.Contains(t, rendered, "type: feat")
			require.Contains(t, rendered, "date: ")
			if phase == PhaseScaffold || phase == PhaseRegenerate {
				require.Contains(t, rendered, "All eight")
				require.Contains(t, rendered, "govulncheck")
				require.NotContains(t, rendered, "All seven")
			}
		})
	}
}
