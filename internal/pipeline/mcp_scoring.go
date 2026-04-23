package pipeline

import (
	"os"
	"path/filepath"
	"strings"
)

// Thresholds for the tool-design dimension. Intent-grouping is only worth
// scoring when the endpoint mirror would otherwise emit enough tools for
// agents to feel the pain — small APIs (under ~10 endpoints) are fine as a
// plain mirror and shouldn't be docked for not using intents.
const toolDesignMinEndpoints = 10

// Threshold above which an endpoint-mirror surface counts as the article's
// named anti-pattern for large APIs. Matches the spec's
// DefaultOrchestrationThreshold; kept independent here so scoring does not
// couple to the spec package's default.
const surfaceStrategyLargeThreshold = 50

// mcpSurface captures the shape of the generated MCP surface inferred from
// the CLI source tree. Each scorer consumes this summary instead of
// rediscovering the file layout.
type mcpSurface struct {
	present         bool
	mainPath        string // cmd/<cli>-pp-mcp/main.go
	toolsPath       string // internal/mcp/tools.go
	intentsPresent  bool   // internal/mcp/intents.go exists
	codeOrchPresent bool   // internal/mcp/code_orch.go exists
	endpointTools   int    // count of endpoint-mirror NewTool(...) registrations
	intentTools     int    // count of intent tool registrations inside intents.go
}

// detectMCPSurface walks the printed CLI's generated layout and returns a
// summary of the MCP surface. A nil return (present=false) signals that the
// CLI does not emit an MCP server — the caller should mark every MCP-only
// dimension as unscored.
func detectMCPSurface(dir string) mcpSurface {
	var s mcpSurface

	// Find the cmd/<name>-pp-mcp/main.go by matching the suffix rather than
	// deriving the name; keeps this scorer decoupled from naming.CLI / naming.MCP.
	cmdDir := filepath.Join(dir, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return s
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), "-pp-mcp") {
			s.mainPath = filepath.Join(cmdDir, e.Name(), "main.go")
			if _, err := os.Stat(s.mainPath); err == nil {
				s.present = true
			}
			break
		}
	}
	if !s.present {
		return s
	}

	s.toolsPath = filepath.Join(dir, "internal", "mcp", "tools.go")
	if data, err := os.ReadFile(s.toolsPath); err == nil {
		s.endpointTools = strings.Count(string(data), "mcplib.NewTool(")
	}

	if data, err := os.ReadFile(filepath.Join(dir, "internal", "mcp", "intents.go")); err == nil {
		s.intentsPresent = true
		s.intentTools = strings.Count(string(data), "mcplib.NewTool(")
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "mcp", "code_orch.go")); err == nil {
		s.codeOrchPresent = true
	}
	return s
}

// scoreMCPRemoteTransport scores 0-10 based on the transports the generated
// binary compiles support for. The article's first pattern is "build remote
// servers for maximum reach" — stdio-only servers cannot reach cloud-hosted
// agents, so they score below the default line. Servers that compile in
// both transports (stdio for local plus http for hosted) get full marks.
//
// Returns (0, false) when the CLI emits no MCP surface so the dimension can
// be excluded from the tier-1 denominator.
func scoreMCPRemoteTransport(dir string) (int, bool) {
	s := detectMCPSurface(dir)
	if !s.present {
		return 0, false
	}
	data, err := os.ReadFile(s.mainPath)
	if err != nil {
		return 0, false
	}
	body := string(data)
	hasStdio := strings.Contains(body, "server.ServeStdio")
	hasHTTP := strings.Contains(body, "NewStreamableHTTPServer") || strings.Contains(body, "ServeStreamableHTTP")
	switch {
	case hasStdio && hasHTTP:
		return 10, true
	case hasHTTP:
		return 7, true
	case hasStdio:
		return 5, true
	default:
		return 0, true
	}
}

// scoreMCPToolDesign scores 0-10 based on whether the MCP surface was
// designed around agent-facing intents (article's second pattern) or left
// as a one-to-one endpoint mirror (article's named anti-pattern). Small
// APIs are unscored because intent-grouping has little value at low
// endpoint counts.
//
// Scoring:
//   - code-orchestration: 10 (explicitly thin surface — article's reference shape)
//   - intents present + ratio >= 0.3: 10 (solid coverage)
//   - intents present + ratio <  0.3: 7  (some coverage)
//   - endpoint-mirror only: 5 (baseline — works, but leaves value on the table)
//
// When present but endpoint count < toolDesignMinEndpoints, returns
// (0, false) — the decision doesn't meaningfully affect an agent at small
// surface sizes.
func scoreMCPToolDesign(dir string) (int, bool) {
	s := detectMCPSurface(dir)
	if !s.present {
		return 0, false
	}
	if s.codeOrchPresent {
		return 10, true
	}
	if s.endpointTools < toolDesignMinEndpoints {
		return 0, false
	}
	if s.intentsPresent && s.intentTools > 0 {
		ratio := float64(s.intentTools) / float64(s.endpointTools+s.intentTools)
		if ratio >= 0.3 {
			return 10, true
		}
		return 7, true
	}
	return 5, true
}

// scoreMCPSurfaceStrategy scores 0-10 based on whether the MCP surface
// strategy matches the API's size. Very large APIs (50+ endpoints) that
// ship as a plain endpoint-mirror are the article's named anti-pattern —
// even well-grouped intents eventually overflow context at those sizes.
//
// Scoring (only when endpointTools + intentTools > surfaceStrategyLargeThreshold):
//   - code-orchestration: 10 (matches Cloudflare's reference case)
//   - intents present: 7  (mitigates the problem, doesn't eliminate it)
//   - endpoint-mirror:  2  (named anti-pattern at scale)
//
// Small APIs are (0, false) — the decision does not matter at low scale.
func scoreMCPSurfaceStrategy(dir string) (int, bool) {
	s := detectMCPSurface(dir)
	if !s.present {
		return 0, false
	}
	total := s.endpointTools + s.intentTools
	if !s.codeOrchPresent && total <= surfaceStrategyLargeThreshold {
		return 0, false
	}
	if s.codeOrchPresent {
		return 10, true
	}
	if s.intentsPresent && s.intentTools > 0 {
		return 7, true
	}
	return 2, true
}
