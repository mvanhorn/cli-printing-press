package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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

// mcpCodeOrchPath is the conventional location of the code-orchestration
// thin-surface tool registrations (search + execute pair) in a printed CLI
// that opted into mcp.orchestration: code.
func mcpCodeOrchPath(dir string) string {
	return filepath.Join(dir, "internal", "mcp", "code_orch.go")
}

// runtimeSurfaceDefault matches the default branch of `<API>_MCP_SURFACE`
// selection logic in a printed CLI's MCP main.go. The API prefix varies
// per CLI (DUB_, NOTION_, etc.) but the env-var suffix is stable. Captures
// the literal default value (typically "thin", "full", or "both").
var runtimeSurfaceDefault = regexp.MustCompile(`os\.Getenv\("[A-Z0-9_]+_MCP_SURFACE"\)[\s\S]{0,200}?surface\s*=\s*"([^"]+)"`)

// canonicalMCPSurfacePath returns the file the scorer should read for token
// efficiency: the file containing the tool registrations the agent actually
// loads under the runtime default surface.
//
// When the printed CLI opts into mcp.orchestration: code AND its main.go
// defaults DUB_MCP_SURFACE (or equivalent) to "thin", the agent loads
// internal/mcp/code_orch.go (the search+execute pair), NOT
// internal/mcp/tools.go (the typed endpoint mirrors). The scorer should
// reflect that.
//
// Falls back to internal/mcp/tools.go when:
//   - code_orch.go does not exist (CLI didn't opt into orchestration),
//   - main.go has no surface-selection logic (older CLIs with no env var),
//   - default surface is "full" or "both" (tools.go IS the agent surface).
//
// This is the architectural fix for retro umbrella issue #516 WU-A4: the
// scorer must evaluate what the agent sees, not what the static templates
// emit. Two surfaces co-exist in code_orch CLIs; the runtime default
// dictates which one drives the catalog tax.
func canonicalMCPSurfacePath(dir string) string {
	toolsPath := mcpToolsPath(dir)
	codeOrchPath := mcpCodeOrchPath(dir)

	if _, err := os.Stat(codeOrchPath); err != nil {
		return toolsPath
	}
	mainPath := mcpMainPath(dir)
	if mainPath == "" {
		return toolsPath
	}
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		return toolsPath
	}
	match := runtimeSurfaceDefault.FindStringSubmatch(string(mainSrc))
	if len(match) < 2 {
		return toolsPath
	}
	if match[1] == "thin" {
		return codeOrchPath
	}
	return toolsPath
}

// estimateMCPTokens reads the generated MCP tool surface and returns an
// estimate of its total token weight, plus per-tool breakdown when
// individual tool definitions can be isolated. Returns a zero-valued
// estimate (TotalTokens == 0) if no MCP surface exists.
//
// Reads canonicalMCPSurfacePath, which respects runtime surface selection
// in main.go (e.g., DUB_MCP_SURFACE defaulting to "thin" loads
// code_orch.go's 2-tool surface, not tools.go's 53-tool endpoint mirror).
// Pre-#516 behavior was unconditional read of tools.go; that mis-scored
// any CLI whose default surface was the thin pair.
func estimateMCPTokens(dir string) MCPTokenEstimate {
	content, err := os.ReadFile(canonicalMCPSurfacePath(dir))
	if err != nil {
		return MCPTokenEstimate{}
	}
	src := string(content)
	if !strings.Contains(src, "RegisterTools") && !strings.Contains(src, "RegisterCodeOrchestrationTools") {
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

	if strings.Contains(src, "cobratree.RegisterAll(") {
		runtimeTools := estimateCobratreeRuntimeTokens(dir)
		for _, tool := range runtimeTools {
			totalChars += tool.Chars
			perTool = append(perTool, tool)
		}
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

// cobratreeFrameworkCommands mirrors the generated cobratree classify
// template's framework skip set. The scorer cannot import generated code
// from a printed CLI, so it keeps the same names here for static estimates.
var cobratreeFrameworkCommands = map[string]bool{
	"about":         true,
	"agent-context": true,
	"api":           true,
	"auth":          true,
	"completion":    true,
	"doctor":        true,
	"feedback":      true,
	"help":          true,
	"profile":       true,
	"search":        true,
	"sql":           true,
	"version":       true,
	"which":         true,
}

type cobraCommandLiteral struct {
	use         string
	short       string
	long        string
	hidden      bool
	annotations map[string]string
	runnable    bool
}

type cobraSourceCommand struct {
	literal    cobraCommandLiteral
	hasLiteral bool
	children   []string
}

type mcpCobraCommandKind int

const (
	mcpCobraNovel mcpCobraCommandKind = iota
	mcpCobraEndpoint
	mcpCobraFramework
	mcpCobraHidden
)

func estimateCobratreeRuntimeTokens(dir string) []MCPToolSize {
	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)
	if len(files) == 0 {
		return nil
	}

	commands := map[string]*cobraSourceCommand{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			continue
		}
		collectCobraSourceCommands(file, commands)
	}
	return reachableCobratreeRuntimeTools(commands)
}

// collectCobraSourceCommands builds a constructor graph for generated Cobra
// commands. The runtime walker starts from RootCmd/newRootCmd and follows
// AddCommand edges, so the scorer does the same instead of counting every
// runnable command literal that happens to exist in internal/cli.
func collectCobraSourceCommands(file *ast.File, commands map[string]*cobraSourceCommand) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil {
			continue
		}
		fnName := fn.Name.Name
		if fnName != "RootCmd" && fnName != "newRootCmd" && (!strings.HasPrefix(fnName, "new") || !strings.HasSuffix(fnName, "Cmd")) {
			continue
		}
		rec := sourceCommandRecord(commands, fnName)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.CompositeLit:
				if !rec.hasLiteral && isCobraCommandLiteral(node) {
					rec.literal = parseCobraCommandLiteral(node)
					rec.hasLiteral = true
				}
			case *ast.CallExpr:
				if !isAddCommandCall(node) {
					return true
				}
				for _, arg := range node.Args {
					if child := cobraConstructorCallName(arg); child != "" {
						rec.children = append(rec.children, child)
					}
				}
			}
			return true
		})
	}
}

func sourceCommandRecord(commands map[string]*cobraSourceCommand, name string) *cobraSourceCommand {
	rec := commands[name]
	if rec == nil {
		rec = &cobraSourceCommand{}
		commands[name] = rec
	}
	return rec
}

func reachableCobratreeRuntimeTools(commands map[string]*cobraSourceCommand) []MCPToolSize {
	var tools []MCPToolSize
	visited := map[string]bool{}
	var walk func(string)
	walk = func(fnName string) {
		rec := commands[fnName]
		if rec == nil {
			return
		}
		for _, childName := range rec.children {
			if visited[childName] {
				continue
			}
			visited[childName] = true
			child := commands[childName]
			if child == nil || !child.hasLiteral {
				continue
			}
			kind := cobratreeCommandKind(child.literal)
			if kind == mcpCobraHidden || kind == mcpCobraFramework {
				continue
			}
			if kind == mcpCobraNovel && child.literal.runnable {
				if tool, ok := estimateCobratreeCommandTool(child.literal); ok {
					tools = append(tools, tool)
				}
			}
			walk(childName)
		}
	}
	walk("RootCmd")
	walk("newRootCmd")
	return tools
}

func isAddCommandCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "AddCommand"
}

func cobraConstructorCallName(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || !strings.HasPrefix(ident.Name, "new") || !strings.HasSuffix(ident.Name, "Cmd") {
		return ""
	}
	return ident.Name
}

func parseCobraCommandLiteral(lit *ast.CompositeLit) cobraCommandLiteral {
	cmd := cobraCommandLiteral{annotations: map[string]string{}}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			cmd.use = stringLiteralValue(kv.Value)
		case "Short":
			cmd.short = stringLiteralValue(kv.Value)
		case "Long":
			cmd.long = stringLiteralValue(kv.Value)
		case "Hidden":
			cmd.hidden = identBoolValue(kv.Value)
		case "Annotations":
			cmd.annotations = normalizedStringMapLiteral(kv.Value)
		case "Run", "RunE":
			cmd.runnable = true
		}
	}
	return cmd
}

func estimateCobratreeCommandTool(cmd cobraCommandLiteral) (MCPToolSize, bool) {
	name := mcpCobraUseName(cmd.use)
	if name == "" || cmd.hidden || !cmd.runnable {
		return MCPToolSize{}, false
	}
	if cobratreeCommandKind(cmd) != mcpCobraNovel {
		return MCPToolSize{}, false
	}
	toolName := naming.SnakeIdentifier(name)
	if toolName == "" {
		return MCPToolSize{}, false
	}
	description := cmd.long
	if description == "" {
		description = cmd.short
	}
	if description == "" {
		description = "Run `" + name + "` through the companion CLI binary."
	}
	chars := len(toolName) + len(description)
	return MCPToolSize{
		Name:   "cobratree:" + toolName,
		Chars:  chars,
		Tokens: chars / 4,
	}, true
}

func cobratreeCommandKind(cmd cobraCommandLiteral) mcpCobraCommandKind {
	name := mcpCobraUseName(cmd.use)
	if name == "" || cmd.hidden || annotationIsTrueValue(cmd.annotations["mcp:hidden"]) {
		return mcpCobraHidden
	}
	if strings.TrimSpace(cmd.annotations["pp:endpoint"]) != "" {
		return mcpCobraEndpoint
	}
	if cobratreeFrameworkCommands[name] {
		return mcpCobraFramework
	}
	return mcpCobraNovel
}

func annotationIsTrueValue(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "true" || v == "1" || v == "yes"
}

func mcpCobraUseName(use string) string {
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func identBoolValue(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "true"
}

func normalizedStringMapLiteral(expr ast.Expr) map[string]string {
	raw := stringMapLiteral(expr)
	out := map[string]string{}
	for key, value := range raw {
		out[key] = strings.ToLower(strings.TrimSpace(value))
	}
	return out
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
