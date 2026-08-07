package generator

import (
	"fmt"
	"io"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const largeMCPSurfaceDefaultInfo = `info: applied Cloudflare MCP pattern (%d endpoints > %d threshold): orchestration code, endpoint_tools hidden, %s. Set mcp.orchestration: endpoint-mirror to opt out.
`

// applyLargeMCPSurfaceDefault honors the contract on
// spec.MCPConfig.OrchestrationThreshold: when the typed-endpoint surface
// exceeds the effective threshold and the spec has not explicitly selected an
// orchestration mode, apply the Cloudflare-style thin MCP surface by default.
func applyLargeMCPSurfaceDefault(s *spec.APISpec, w io.Writer) {
	if s == nil {
		return
	}
	result := s.ApplyLargeMCPSurfaceDefault()
	if result.Applied {
		transportNote := "preserved explicit transport"
		if result.TransportDefaulted {
			transportNote = "transport [stdio,http]"
		}
		fmt.Fprintf(w, largeMCPSurfaceDefaultInfo, result.EndpointCount, result.Threshold, transportNote)
	}
}
