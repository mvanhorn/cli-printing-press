package pipeline

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MCPTokenEstimate reports the approximate token weight of a generated MCP
// tool surface. Inspired by Cloudflare's Code Mode MCP which serves 3000+
// operations in under 1000 tokens (see 2026-04-13 Wrangler post).
//
// Token estimate uses the simple chars/4 heuristic. Good enough for
// relative comparison across printed CLIs and for catching regressions.
// Swap for a real tokenizer if a retro finds the heuristic misleading.
type MCPTokenEstimate struct {
	TotalChars  int           `json:"total_chars"`
	TotalTokens int           `json:"total_tokens"`
	ToolCount   int           `json:"tool_count"`
	PerTool     []MCPToolSize `json:"per_tool,omitempty"`
	TopHeaviest []MCPToolSize `json:"top_heaviest,omitempty"`
}

type MCPToolSize struct {
	Name   string `json:"name"`
	Chars  int    `json:"chars"`
	Tokens int    `json:"tokens"`
}

// mcpToolsPath is the conventional location of the generated MCP tool
// registrations in a printed CLI.
func mcpToolsPath(dir string) string {
	return filepath.Join(dir, "internal", "mcp", "tools.go")
}

// estimateMCPTokens reads the generated MCP tool surface and returns an
// estimate of its total token weight, plus per-tool breakdown when
// individual tool definitions can be isolated. Returns a zero-valued
// estimate (TotalTokens == 0) if no MCP surface exists.
func estimateMCPTokens(dir string) MCPTokenEstimate {
	content, err := os.ReadFile(mcpToolsPath(dir))
	if err != nil {
		return MCPTokenEstimate{}
	}
	src := string(content)
	if !strings.Contains(src, "RegisterTools") {
		// File exists but is a stub — treat as no MCP surface.
		return MCPTokenEstimate{}
	}

	// The agent-facing weight of an MCP tool is the name plus description
	// plus every parameter name and description. Rather than parsing
	// mcp-go's builder API perfectly, we approximate by extracting all
	// string literals in the file — the vast majority of bytes an agent
	// sees come from those literals.
	literalRe := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	toolRe := regexp.MustCompile(`mcplib\.NewTool\(\s*"([^"]+)"`)

	literals := literalRe.FindAllString(src, -1)
	totalChars := 0
	for _, lit := range literals {
		totalChars += len(lit) - 2 // strip surrounding quotes
	}

	// Per-tool sizes: slice the source between consecutive NewTool() calls
	// and count literal chars within each slice.
	toolStarts := toolRe.FindAllStringSubmatchIndex(src, -1)
	toolNames := toolRe.FindAllStringSubmatch(src, -1)
	perTool := make([]MCPToolSize, 0, len(toolNames))
	for i, match := range toolNames {
		name := match[1]
		start := toolStarts[i][0]
		var end int
		if i+1 < len(toolStarts) {
			end = toolStarts[i+1][0]
		} else {
			end = len(src)
		}
		chunk := src[start:end]
		chunkChars := 0
		for _, lit := range literalRe.FindAllString(chunk, -1) {
			chunkChars += len(lit) - 2
		}
		perTool = append(perTool, MCPToolSize{
			Name:   name,
			Chars:  chunkChars,
			Tokens: chunkChars / 4,
		})
	}

	est := MCPTokenEstimate{
		TotalChars:  totalChars,
		TotalTokens: totalChars / 4,
		ToolCount:   len(perTool),
		PerTool:     perTool,
	}

	// Top-3 heaviest tools so authors know where to trim descriptions.
	if len(perTool) > 0 {
		heaviest := make([]MCPToolSize, len(perTool))
		copy(heaviest, perTool)
		sort.Slice(heaviest, func(i, j int) bool {
			return heaviest[i].Chars > heaviest[j].Chars
		})
		top := min(len(heaviest), 3)
		est.TopHeaviest = heaviest[:top]
	}

	return est
}

// scoreMCPTokenEfficiency scores 0-10 based on the token weight of the
// generated MCP surface. Returns (score, scored) where scored is false
// for CLIs without an MCP surface so the dimension can be excluded from
// the scorecard denominator.
//
// Scoring bands are calibrated against Cloudflare's <1000 tokens for
// 3000 operations. Printed CLIs typically have far fewer operations, so
// the per-tool target is more meaningful than the absolute total. Bands:
//
//   - per-tool <= 80 tokens: full marks (10)
//   - per-tool <= 160 tokens: partial (7)
//   - per-tool <= 320 tokens: partial (4)
//   - per-tool > 320 tokens: 0
//
// Empty or missing MCP surface returns (0, false) so the dimension is
// added to UnscoredDimensions.
func scoreMCPTokenEfficiency(dir string) (int, bool) {
	est := estimateMCPTokens(dir)
	if est.ToolCount == 0 {
		return 0, false
	}
	avgTokensPerTool := est.TotalTokens / est.ToolCount
	switch {
	case avgTokensPerTool <= 80:
		return 10, true
	case avgTokensPerTool <= 160:
		return 7, true
	case avgTokensPerTool <= 320:
		return 4, true
	default:
		return 0, true
	}
}
