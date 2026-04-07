package megamcp

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ActivationManager manages the state of which APIs are activated
// and handles dynamic tool registration/deregistration.
type ActivationManager struct {
	mu        sync.RWMutex
	server    *server.MCPServer
	manifests map[string]*APIEntry    // all loaded manifests by slug
	activated map[string]bool         // which slugs have been activated
	clients   map[string]*http.Client // per-API HTTP clients
}

// NewActivationManager creates a new ActivationManager with all loaded API entries.
func NewActivationManager(s *server.MCPServer, entries []*APIEntry) *ActivationManager {
	manifests := make(map[string]*APIEntry, len(entries))
	for _, entry := range entries {
		manifests[entry.Slug] = entry
	}
	return &ActivationManager{
		server:    s,
		manifests: manifests,
		activated: make(map[string]bool),
		clients:   make(map[string]*http.Client),
	}
}

// Activate registers tools for the given API slug. Returns the number of tools
// registered. Idempotent: calling twice for the same slug returns the same count
// without duplicating tools.
func (am *ActivationManager) Activate(slug string) (int, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return 0, fmt.Errorf("API not found: %q", slug)
	}

	if am.activated[slug] {
		// Already activated — return existing tool count.
		return len(entry.Manifest.Tools), nil
	}

	// Create a per-API HTTP client.
	client := &http.Client{Timeout: 30 * time.Second}
	am.clients[slug] = client

	// Register each tool with the normalized prefix.
	prefix := entry.NormalizedPrefix
	var tools []server.ServerTool
	for _, tool := range entry.Manifest.Tools {
		toolName := prefix + "__" + tool.Name

		// Build MCP tool definition with parameter schema.
		toolOpts := []mcp.ToolOption{
			mcp.WithDescription(SanitizeText(tool.Description, 500)),
		}
		for _, param := range tool.Params {
			paramOpts := []mcp.PropertyOption{
				mcp.Description(SanitizeText(param.Description, 200)),
			}
			if param.Required {
				paramOpts = append(paramOpts, mcp.Required())
			}
			toolOpts = append(toolOpts, mcp.WithString(param.Name, paramOpts...))
		}

		mcpTool := mcp.NewTool(toolName, toolOpts...)
		handler := MakeToolHandler(entry.Manifest, tool, client)
		tools = append(tools, server.ServerTool{Tool: mcpTool, Handler: handler})
	}

	// AddTools sends tools/list_changed notification automatically.
	if len(tools) > 0 {
		am.server.AddTools(tools...)
	}

	am.activated[slug] = true
	return len(entry.Manifest.Tools), nil
}

// Deactivate removes all tools for the given API slug.
func (am *ActivationManager) Deactivate(slug string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return fmt.Errorf("API not found: %q", slug)
	}

	if !am.activated[slug] {
		return fmt.Errorf("API %q is not currently activated", slug)
	}

	// Collect tool names to delete.
	prefix := entry.NormalizedPrefix
	var names []string
	for _, tool := range entry.Manifest.Tools {
		names = append(names, prefix+"__"+tool.Name)
	}

	// DeleteTools sends tools/list_changed notification automatically.
	if len(names) > 0 {
		am.server.DeleteTools(names...)
	}

	delete(am.activated, slug)
	delete(am.clients, slug)
	return nil
}

// IsActivated returns whether the given API slug is currently activated.
func (am *ActivationManager) IsActivated(slug string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.activated[slug]
}

// GetManifest returns the APIEntry for a given slug, or nil if not found.
func (am *ActivationManager) GetManifest(slug string) *APIEntry {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.manifests[slug]
}

// AllManifests returns all loaded API entries.
func (am *ActivationManager) AllManifests() []*APIEntry {
	am.mu.RLock()
	defer am.mu.RUnlock()
	result := make([]*APIEntry, 0, len(am.manifests))
	for _, entry := range am.manifests {
		result = append(result, entry)
	}
	return result
}

// SearchResult represents a matching tool from a search query.
type SearchResult struct {
	APISlug     string `json:"api_slug"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
}

// SearchTools searches across ALL manifests (not just activated) for tools
// matching the given query. Case-insensitive substring match on tool name
// and description.
func (am *ActivationManager) SearchTools(query string) []SearchResult {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if query == "" {
		return nil
	}

	lowerQuery := strings.ToLower(query)
	var results []SearchResult

	for _, entry := range am.manifests {
		prefix := entry.NormalizedPrefix
		for _, tool := range entry.Manifest.Tools {
			fullName := prefix + "__" + tool.Name
			if strings.Contains(strings.ToLower(fullName), lowerQuery) ||
				strings.Contains(strings.ToLower(tool.Description), lowerQuery) {
				results = append(results, SearchResult{
					APISlug:     entry.Slug,
					ToolName:    fullName,
					Description: tool.Description,
				})
			}
		}
	}

	return results
}

// toolNamesForSlug returns the full prefixed tool names for a given API slug.
// Used for display purposes.
func (am *ActivationManager) toolNamesForSlug(slug string) []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return nil
	}

	names := make([]string, 0, len(entry.Manifest.Tools))
	for _, tool := range entry.Manifest.Tools {
		names = append(names, entry.NormalizedPrefix+"__"+tool.Name)
	}
	return names
}
