package megamcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/internal/version"
)

// RegisterMetaTools registers the 6 agent-facing discovery and activation tools.
func RegisterMetaTools(s *server.MCPServer, am *ActivationManager) {
	s.AddTool(
		mcp.NewTool("library_info",
			mcp.WithDescription("List all available APIs in the printing press library with metadata, auth status, and tool counts"),
		),
		makeLibraryInfoHandler(am),
	)

	s.AddTool(
		mcp.NewTool("setup_guide",
			mcp.WithDescription("Get auth setup instructions for a specific API: env var names, key URL, and example claude mcp add command"),
			mcp.WithString("api_slug", mcp.Required(), mcp.Description("API slug (e.g., 'dub', 'espn')")),
		),
		makeSetupGuideHandler(am),
	)

	s.AddTool(
		mcp.NewTool("activate_api",
			mcp.WithDescription("Activate an API to register its tools for use. Call library_info first to see available APIs."),
			mcp.WithString("api_slug", mcp.Required(), mcp.Description("API slug to activate (e.g., 'dub', 'espn')")),
		),
		makeActivateAPIHandler(am),
	)

	s.AddTool(
		mcp.NewTool("deactivate_api",
			mcp.WithDescription("Deactivate an API and remove its tools"),
			mcp.WithString("api_slug", mcp.Required(), mcp.Description("API slug to deactivate")),
		),
		makeDeactivateAPIHandler(am),
	)

	s.AddTool(
		mcp.NewTool("search_tools",
			mcp.WithDescription("Search for tools across all APIs by keyword. Works on unactivated APIs too."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query to match against tool names and descriptions")),
		),
		makeSearchToolsHandler(am),
	)

	s.AddTool(
		mcp.NewTool("about",
			mcp.WithDescription("Describe this mega MCP server's version, API count, and total tool count"),
		),
		makeAboutHandler(am),
	)
}

// --- library_info ---

// libraryInfoResponse is the JSON structure returned by library_info.
type libraryInfoResponse struct {
	APIs       []libraryInfoAPI `json:"apis"`
	TotalAPIs  int              `json:"total_apis"`
	TotalTools int              `json:"total_tools"`
	Version    string           `json:"version"`
}

type libraryInfoAPI struct {
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	ToolCount        int      `json:"tool_count"`
	PublicToolCount  int      `json:"public_tool_count"`
	AuthType         string   `json:"auth_type"`
	MCPReady         string   `json:"mcp_ready"`
	AuthConfigured   bool     `json:"auth_configured"`
	Activated        bool     `json:"activated"`
	UpgradeAvailable bool     `json:"upgrade_available"`
	NovelFeatures    []string `json:"novel_features,omitempty"`
}

func makeLibraryInfoHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries := am.AllManifests()

		resp := libraryInfoResponse{
			APIs:    make([]libraryInfoAPI, 0, len(entries)),
			Version: version.Get(),
		}

		totalTools := 0
		for _, entry := range entries {
			m := entry.Manifest
			toolCount := len(m.Tools)
			publicToolCount := countPublicTools(m)
			totalTools += toolCount

			api := libraryInfoAPI{
				Slug:             entry.Slug,
				Name:             m.APIName,
				Description:      SanitizeText(m.Description, 200),
				ToolCount:        toolCount,
				PublicToolCount:  publicToolCount,
				AuthType:         m.Auth.Type,
				MCPReady:         m.MCPReady,
				AuthConfigured:   hasAuthConfigured(m),
				Activated:        am.IsActivated(entry.Slug),
				UpgradeAvailable: checkUpgradeAvailable(entry.Slug),
			}

			// Include novel features if present (from manifest description, not a separate field).
			// The ToolsManifest doesn't carry novel features directly, but we can note
			// their existence through the registry. For now, this field is omitted.

			resp.APIs = append(resp.APIs, api)
		}

		resp.TotalAPIs = len(entries)
		resp.TotalTools = totalTools

		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error serializing library info: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// countPublicTools counts tools that have NoAuth=true.
func countPublicTools(m *ToolsManifest) int {
	count := 0
	for _, t := range m.Tools {
		if t.NoAuth {
			count++
		}
	}
	// If auth type is "none", all tools are public.
	if m.Auth.Type == "" || m.Auth.Type == "none" {
		return len(m.Tools)
	}
	return count
}

// checkUpgradeAvailable checks if a local per-API MCP binary exists,
// indicating the user could use a dedicated MCP server with full features.
func checkUpgradeAvailable(slug string) bool {
	libraryRoot := pipeline.PublishedLibraryRoot()

	// Check both directory layouts:
	// 1. ~/printing-press/library/{slug}-pp-cli/cmd/{slug}-pp-mcp/
	// 2. ~/printing-press/library/{slug}/cmd/{slug}-pp-mcp/
	mcpBinary := naming.MCP(slug)

	paths := []string{
		filepath.Join(libraryRoot, naming.CLI(slug), "cmd", mcpBinary),
		filepath.Join(libraryRoot, slug, "cmd", mcpBinary),
	}

	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// --- setup_guide ---

func makeSetupGuideHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := getStringArg(req, "api_slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		entry := am.GetManifest(slug)
		if entry == nil {
			return mcp.NewToolResultError(fmt.Sprintf("API not found: %q", slug)), nil
		}

		m := entry.Manifest

		// No auth API.
		if m.Auth.Type == "" || m.Auth.Type == "none" {
			return mcp.NewToolResultText(fmt.Sprintf(
				"No authentication required for %s — all endpoints are publicly accessible.\n\n"+
					"Activate with: activate_api(%q)", m.APIName, slug)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Setup Guide for %s\n\n", m.APIName))
		sb.WriteString(fmt.Sprintf("**Auth type:** %s\n\n", m.Auth.Type))

		// Env vars.
		if len(m.Auth.EnvVars) > 0 {
			sb.WriteString("**Required environment variables:**\n")
			for _, envVar := range m.Auth.EnvVars {
				sb.WriteString(fmt.Sprintf("- `%s`\n", envVar))
			}
			sb.WriteString("\n")
		}

		// Key URL.
		if m.Auth.KeyURL != "" {
			sb.WriteString(fmt.Sprintf("**Get your API key:** %s\n\n", m.Auth.KeyURL))
		}

		// Example claude mcp add command.
		if len(m.Auth.EnvVars) > 0 {
			sb.WriteString("**Example setup command:**\n```\nclaude mcp add printing-press")
			for _, envVar := range m.Auth.EnvVars {
				sb.WriteString(fmt.Sprintf(" --env %s=<your-key>", envVar))
			}
			sb.WriteString(" -- printing-press-mcp\n```\n\n")
		}

		sb.WriteString(fmt.Sprintf("Once configured, activate with: activate_api(%q)", slug))

		return mcp.NewToolResultText(sb.String()), nil
	}
}

// --- activate_api ---

func makeActivateAPIHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := getStringArg(req, "api_slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		count, err := am.Activate(slug)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to activate API: %v", err)), nil
		}

		// Get example tool names for confirmation message.
		toolNames := am.toolNamesForSlug(slug)
		examples := ""
		if len(toolNames) > 0 {
			limit := 3
			if len(toolNames) < limit {
				limit = len(toolNames)
			}
			examples = "\n\nExample tools:\n"
			for _, name := range toolNames[:limit] {
				examples += fmt.Sprintf("- %s\n", name)
			}
			if len(toolNames) > 3 {
				examples += fmt.Sprintf("- ... and %d more\n", len(toolNames)-3)
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"Activated %q — %d tools registered.%s", slug, count, examples)), nil
	}
}

// --- deactivate_api ---

func makeDeactivateAPIHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := getStringArg(req, "api_slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := am.Deactivate(slug); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to deactivate API: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Deactivated %q — tools removed.", slug)), nil
	}
}

// --- search_tools ---

type searchToolsResponse struct {
	Results []SearchResult `json:"results"`
	Count   int            `json:"count"`
}

func makeSearchToolsHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := getStringArg(req, "query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		results := am.SearchTools(query)

		resp := searchToolsResponse{
			Results: results,
			Count:   len(results),
		}

		if resp.Results == nil {
			resp.Results = make([]SearchResult, 0)
		}

		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error serializing search results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// --- about ---

type aboutResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	TotalAPIs  int    `json:"total_apis"`
	TotalTools int    `json:"total_tools"`
}

func makeAboutHandler(am *ActivationManager) server.ToolHandlerFunc {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries := am.AllManifests()

		totalTools := 0
		for _, entry := range entries {
			totalTools += len(entry.Manifest.Tools)
		}

		resp := aboutResponse{
			Name:       "printing-press-mcp",
			Version:    version.Get(),
			TotalAPIs:  len(entries),
			TotalTools: totalTools,
		}

		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error serializing about info: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// --- helpers ---

// getStringArg extracts a required string argument from an MCP request.
func getStringArg(req mcp.CallToolRequest, name string) (string, error) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return "", fmt.Errorf("missing required argument: %q", name)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	if s == "" {
		return "", fmt.Errorf("argument %q must not be empty", name)
	}
	return s, nil
}
