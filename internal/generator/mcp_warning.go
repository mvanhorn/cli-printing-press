package generator

import (
	"fmt"
	"io"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const largeMCPSurfaceWarning = `warning: spec exposes %d MCP endpoint tools (>%d threshold). The default
         endpoint-mirror surface burns agent context at this scale and will
         score poorly on the scorecard's MCP architectural dimensions. Consider
         enriching the spec's mcp: block before generation:
           mcp:
             transport: [stdio, http]    # remote-capable; reaches hosted agents
             orchestration: code         # thin <api>_search + <api>_execute pair
             endpoint_tools: hidden      # suppress raw per-endpoint mirrors
         See docs/SPEC-EXTENSIONS.md for the full mcp: schema.
`

// warnUnenrichedLargeMCPSurface honors the contract on
// spec.MCPConfig.OrchestrationThreshold: when the typed-endpoint surface
// exceeds the effective threshold and the spec hasn't opted into code
// orchestration, recommend the enrichment pattern. Informational only —
// does not gate generation or alter rendered output.
func warnUnenrichedLargeMCPSurface(s *spec.APISpec, w io.Writer) {
	if s == nil {
		return
	}
	threshold := s.MCP.EffectiveOrchestrationThreshold()
	total := countTypedEndpoints(s)
	if total <= threshold || s.MCP.IsCodeOrchestration() {
		return
	}
	fmt.Fprintf(w, largeMCPSurfaceWarning, total, threshold)
}

func countTypedEndpoints(s *spec.APISpec) int {
	if s == nil {
		return 0
	}
	n := 0
	for _, r := range s.Resources {
		n += len(r.Endpoints)
		for _, sub := range r.SubResources {
			n += len(sub.Endpoints)
		}
	}
	return n
}
