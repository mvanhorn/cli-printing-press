package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	intentHintsNA    = "n/a"
	intentHintsOK    = "ok"
	intentHintsStale = "stale"
)

// newMCPAuditCmd reports the MCP surface strategy for every CLI in the
// local library, so the operator can see at a glance which printed CLIs
// are ready for production agents and which would benefit from a
// regenerate under the new spec surface (remote transport, intent tools,
// code-orchestration).
//
// Diagnostic only — never exits non-zero for findings. Use the scorecard
// (gated by the ship pipeline) when you need a gating signal.
func newMCPAuditCmd() *cobra.Command {
	var asJSON bool
	var libraryPath string

	cmd := &cobra.Command{
		Use:   "mcp-audit",
		Short: "Report MCP surface shape for every installed printed CLI",
		Long: `Walks each CLI under ~/printing-press/library/<api>/ and reports the
current MCP surface strategy — transport, tool design, intent safety
annotations, and whether the shape matches the API's size. Useful after
a machine change to see which CLIs would benefit from a regenerate.

Diagnostic only. Exit 0 regardless of findings.`,
		Example: `  cli-printing-press mcp-audit
  cli-printing-press mcp-audit --library ~/printing-press/library --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := libraryPath
			if path == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("resolving home: %w", err)
				}
				path = filepath.Join(home, "printing-press", "library")
			}
			findings, err := runMCPAudit(path)
			if err != nil {
				return err
			}
			if asJSON {
				data, err := json.MarshalIndent(findings, "", "  ")
				if err != nil {
					return fmt.Errorf("encoding findings: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			renderMCPAuditTable(cmd.OutOrStdout(), findings)
			return nil
		},
	}

	cmd.Flags().StringVar(&libraryPath, "library", "", "library root (default ~/printing-press/library)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

// MCPAuditFinding summarizes one CLI's MCP shape. Stable JSON field names
// so downstream tools (ci scripts, the future emboss-audit loop) can
// consume this without depending on go struct ordering.
type MCPAuditFinding struct {
	API         string `json:"api"`         // library directory basename
	HasMCP      bool   `json:"has_mcp"`     // the CLI emits an MCP server
	Transport   string `json:"transport"`   // "stdio", "http", "both", "unknown", "n/a"
	ToolDesign  string `json:"tool_design"` // "endpoint-mirror", "intent", "code-orch", "n/a"
	EndpointCt  int    `json:"endpoint_count"`
	IntentCt    int    `json:"intent_count"`
	IntentHints string `json:"intent_hints"` // "ok", "stale", "n/a"
	Recommend   string `json:"recommend"`    // short, actionable suggestion
}

// runMCPAudit walks every immediate subdirectory of libraryPath and reports
// one finding per CLI. Missing MCP surfaces are reported, not skipped, so
// the operator sees which CLIs lack one entirely.
func runMCPAudit(libraryPath string) ([]MCPAuditFinding, error) {
	entries, err := os.ReadDir(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("reading library %s: %w", libraryPath, err)
	}

	var findings []MCPAuditFinding
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		findings = append(findings, auditLibraryCLI(filepath.Join(libraryPath, e.Name()), e.Name()))
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].API < findings[j].API })
	return findings, nil
}

// auditLibraryCLI inspects a single library entry. It relies on string
// heuristics over file contents rather than requiring a manifest file
// because older printed CLIs predate the tools-manifest schema.
func auditLibraryCLI(dir, api string) MCPAuditFinding {
	f := MCPAuditFinding{API: api}

	var mainPath string
	cmdDir := filepath.Join(dir, "cmd")
	cmdEntries, err := os.ReadDir(cmdDir)
	if err == nil {
		for _, ce := range cmdEntries {
			if ce.IsDir() && strings.HasSuffix(ce.Name(), "-pp-mcp") {
				mainPath = filepath.Join(cmdDir, ce.Name(), "main.go")
				break
			}
		}
	}
	if mainPath == "" {
		f.Transport = "n/a"
		f.ToolDesign = "n/a"
		f.IntentHints = intentHintsNA
		f.Recommend = "no MCP surface — consider adding mcp: block to enable an MCP server"
		return f
	}
	f.HasMCP = true

	if data, err := os.ReadFile(mainPath); err == nil {
		body := string(data)
		hasStdio := strings.Contains(body, "server.ServeStdio")
		hasHTTP := strings.Contains(body, "NewStreamableHTTPServer") || strings.Contains(body, "ServeStreamableHTTP")
		switch {
		case hasStdio && hasHTTP:
			f.Transport = "both"
		case hasHTTP:
			f.Transport = "http"
		case hasStdio:
			f.Transport = "stdio"
		default:
			f.Transport = "unknown"
		}
	} else {
		f.Transport = "unknown"
	}

	if data, err := os.ReadFile(filepath.Join(dir, "internal", "mcp", "tools.go")); err == nil {
		f.EndpointCt = strings.Count(string(data), "mcplib.NewTool(")
	}
	intentsPresent := false
	var intentsBody string
	if data, err := os.ReadFile(filepath.Join(dir, "internal", "mcp", "intents.go")); err == nil {
		intentsPresent = true
		intentsBody = string(data)
		f.IntentCt = strings.Count(intentsBody, "mcplib.NewTool(")
	}
	codeOrch := false
	if _, err := os.Stat(filepath.Join(dir, "internal", "mcp", "code_orch.go")); err == nil {
		codeOrch = true
	}

	switch {
	case codeOrch:
		f.ToolDesign = "code-orch"
	case intentsPresent:
		f.ToolDesign = "intent"
	default:
		f.ToolDesign = "endpoint-mirror"
	}

	hints, recs := inspectIntentSurface(intentsBody, f.IntentCt)
	f.IntentHints = hints
	f.Recommend = mergeRecommendations(recommendForFinding(f), recs)
	return f
}

func inspectIntentSurface(body string, intentCt int) (string, []string) {
	if intentCt == 0 {
		return intentHintsNA, nil
	}
	var recs []string
	if strings.Count(body, "WithOpenWorldHintAnnotation") < intentCt {
		recs = append(recs, "reprint: intent tools lack safety annotations")
	}
	if intentHasInvalidTypedDefault(body) {
		recs = append(recs, "reprint: intent typed defaults are not valid Go literals")
	}
	if len(recs) == 0 {
		return intentHintsOK, nil
	}
	return intentHintsStale, recs
}

func intentHasInvalidTypedDefault(body string) bool {
	const assign = "] = "
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, `input["`) {
			continue
		}
		_, rhs, ok := strings.Cut(trimmed, assign)
		if !ok {
			continue
		}
		rhs = strings.TrimSpace(strings.TrimSuffix(rhs, ";"))
		if !validIntentDefaultGo(rhs) {
			return true
		}
	}
	return false
}

func validIntentDefaultGo(rhs string) bool {
	if rhs == "true" || rhs == "false" {
		return true
	}
	if strings.HasPrefix(rhs, `"`) {
		return true
	}
	_, err := strconv.ParseInt(rhs, 10, 64)
	return err == nil
}

func mergeRecommendations(base string, extra []string) string {
	if len(extra) == 0 {
		return base
	}
	if base == "" || base == "ok" {
		return strings.Join(extra, "; ")
	}
	return base + "; " + strings.Join(extra, "; ")
}

// recommendForFinding produces a one-line action hint. The hints match the
// spec surface introduced in U1-U3 so the operator can map directly from
// audit output to the mcp: fields they should add.
func recommendForFinding(f MCPAuditFinding) string {
	if !f.HasMCP {
		return "no MCP surface — add mcp: block to enable"
	}
	var hints []string
	if f.Transport == "stdio" {
		hints = append(hints, "set mcp.transport: [stdio, http] for cloud-agent reach")
	}
	switch f.ToolDesign {
	case "endpoint-mirror":
		if f.EndpointCt > 50 {
			hints = append(hints, "set mcp.orchestration: code (endpoint count past threshold)")
		} else if f.EndpointCt >= 10 {
			hints = append(hints, "declare mcp.intents for common workflows")
		}
	case "intent":
		if f.EndpointCt > 50 {
			hints = append(hints, "consider mcp.orchestration: code for the full surface")
		}
	}
	if len(hints) == 0 {
		return "ok"
	}
	return strings.Join(hints, "; ")
}

// renderMCPAuditTable produces the human-readable table. Kept narrow on
// purpose — operators run this interactively, and wide tables stop being
// readable past ~80 columns. Recommend is the last column so long hints
// wrap cleanly in terminals.
func renderMCPAuditTable(w interface{ Write(p []byte) (int, error) }, findings []MCPAuditFinding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "(no CLIs found in library)")
		return
	}
	header := fmt.Sprintf("%-20s  %-10s  %-14s  %6s  %6s  %s", "API", "Transport", "ToolDesign", "Epts", "Itnts", "Recommend")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("-", len(header)))
	for _, f := range findings {
		fmt.Fprintf(w, "%-20s  %-10s  %-14s  %6d  %6d  %s\n",
			truncate(f.API, 20), f.Transport, f.ToolDesign, f.EndpointCt, f.IntentCt, f.Recommend)
	}
}

// truncate cuts s to at most n display characters, appending "…"
// when shortened. Walks runes (not bytes) to keep multibyte
// characters intact at the boundary.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
