package generator

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const largeMCPSurfaceWarning = `warning: spec exposes %d MCP endpoint tools (>%d threshold). The default
         endpoint-mirror surface burns agent context at this scale and will
         score poorly on the scorecard's MCP architectural dimensions. Consider
         enriching the spec's mcp: (internal YAML) or x-mcp: (OpenAPI) block
         before generation:
           mcp:
             transport: [stdio, http]    # remote-capable; reaches hosted agents
             orchestration: code         # thin <api>_search + <api>_execute pair
             endpoint_tools: hidden      # suppress raw per-endpoint mirrors
         See docs/SPEC-EXTENSIONS.md for the full mcp:/x-mcp: schema.
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
	total := s.TypedEndpointCount()
	if total <= threshold || s.MCP.IsCodeOrchestration() {
		return
	}
	fmt.Fprintf(w, largeMCPSurfaceWarning, total, threshold)
}

func warnUnannotatedMutations(s *spec.APISpec, w io.Writer) {
	if s == nil || w == nil {
		return
	}
	resourceNames := make([]string, 0, len(s.Resources))
	for resourceName := range s.Resources {
		resourceNames = append(resourceNames, resourceName)
	}
	sort.Strings(resourceNames)
	for _, resourceName := range resourceNames {
		resource := s.Resources[resourceName]

		endpointNames := make([]string, 0, len(resource.Endpoints))
		for endpointName := range resource.Endpoints {
			endpointNames = append(endpointNames, endpointName)
		}
		sort.Strings(endpointNames)
		for _, endpointName := range endpointNames {
			endpoint := resource.Endpoints[endpointName]
			warnUnannotatedMutation(w, resourceName, endpointName, endpoint)
		}

		subResourceNames := make([]string, 0, len(resource.SubResources))
		for subName := range resource.SubResources {
			subResourceNames = append(subResourceNames, subName)
		}
		sort.Strings(subResourceNames)
		for _, subName := range subResourceNames {
			subResource := resource.SubResources[subName]
			subEndpointNames := make([]string, 0, len(subResource.Endpoints))
			for endpointName := range subResource.Endpoints {
				subEndpointNames = append(subEndpointNames, endpointName)
			}
			sort.Strings(subEndpointNames)
			for _, endpointName := range subEndpointNames {
				endpoint := subResource.Endpoints[endpointName]
				warnUnannotatedMutation(w, resourceName+"."+subName, endpointName, endpoint)
			}
		}
	}
}

func warnUnannotatedMutation(w io.Writer, resourceName, endpointName string, endpoint spec.Endpoint) {
	method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return
	}
	if !endpointIsWriteCommand(endpoint, endpointName) || endpointMCPDestructive(endpoint) {
		return
	}
	fmt.Fprintf(w, "warning: command %s.%s is an unannotated mutation; agents will not see destructive signal - annotate explicitly\n", resourceName, endpointName)
}
