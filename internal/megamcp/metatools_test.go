package megamcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMetaToolsServer creates a server with meta-tools registered for testing.
func newMetaToolsServer() (*server.MCPServer, *ActivationManager) {
	s := newTestServer()
	entries := newTestAPIEntries()
	am := NewActivationManager(s, entries)
	RegisterMetaTools(s, am)
	return s, am
}

// callMetaTool invokes a registered meta-tool by name with given arguments.
func callMetaTool(t *testing.T, s *server.MCPServer, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	tool := s.GetTool(toolName)
	require.NotNil(t, tool, "tool %q should be registered", toolName)
	result, err := tool.Handler(t.Context(), makeToolRequest(args))
	require.NoError(t, err)
	return result
}

func TestRegisterMetaTools_AllRegistered(t *testing.T) {
	s, _ := newMetaToolsServer()

	tools := s.ListTools()
	expected := []string{"library_info", "setup_guide", "activate_api", "deactivate_api", "search_tools", "about"}
	for _, name := range expected {
		assert.NotNil(t, tools[name], "meta-tool %q should be registered", name)
	}
}

// --- library_info ---

func TestLibraryInfo_ReturnsAllAPIs(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "library_info", nil)
	assert.False(t, result.IsError)

	var resp libraryInfoResponse
	text := extractResultText(result)
	require.NoError(t, json.Unmarshal([]byte(text), &resp))

	assert.Equal(t, 3, resp.TotalAPIs)
	assert.Equal(t, 6, resp.TotalTools) // 3 ESPN + 2 Dub + 1 Public
	assert.NotEmpty(t, resp.Version)
	assert.Len(t, resp.APIs, 3)
}

func TestLibraryInfo_AuthConfiguredReflectsEnvVars(t *testing.T) {
	s, _ := newMetaToolsServer()

	// Without env var set.
	result := callMetaTool(t, s, "library_info", nil)
	var resp libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	// Find ESPN entry — auth should not be configured.
	for _, api := range resp.APIs {
		if api.Slug == "espn" {
			assert.False(t, api.AuthConfigured, "ESPN auth should not be configured without env var")
		}
		if api.Slug == "public-api" {
			assert.True(t, api.AuthConfigured, "no-auth API should report auth as configured")
		}
	}

	// Now set the env var.
	t.Setenv("ESPN_KEY", "test-key")
	result2 := callMetaTool(t, s, "library_info", nil)
	var resp2 libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result2)), &resp2))

	for _, api := range resp2.APIs {
		if api.Slug == "espn" {
			assert.True(t, api.AuthConfigured, "ESPN auth should be configured with env var set")
		}
	}
}

func TestLibraryInfo_ActivatedStatus(t *testing.T) {
	s, am := newMetaToolsServer()

	// Before activation.
	result := callMetaTool(t, s, "library_info", nil)
	var resp libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	for _, api := range resp.APIs {
		assert.False(t, api.Activated, "no API should be activated initially")
	}

	// Activate ESPN.
	_, err := am.Activate("espn")
	require.NoError(t, err)

	result2 := callMetaTool(t, s, "library_info", nil)
	var resp2 libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result2)), &resp2))

	for _, api := range resp2.APIs {
		if api.Slug == "espn" {
			assert.True(t, api.Activated, "ESPN should be activated")
		} else {
			assert.False(t, api.Activated)
		}
	}
}

func TestLibraryInfo_NoAPIsLoaded(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, nil)
	RegisterMetaTools(s, am)

	result := callMetaTool(t, s, "library_info", nil)
	assert.False(t, result.IsError)

	var resp libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	assert.Equal(t, 0, resp.TotalAPIs)
	assert.Equal(t, 0, resp.TotalTools)
	assert.Empty(t, resp.APIs)
}

func TestLibraryInfo_PublicToolCount(t *testing.T) {
	s := newTestServer()
	entries := []*APIEntry{
		{
			Slug:             "mixed",
			NormalizedPrefix: "mixed",
			Manifest: &ToolsManifest{
				APIName: "Mixed",
				Auth:    ManifestAuth{Type: "api_key", EnvVars: []string{"MIX_KEY"}},
				Tools: []ManifestTool{
					{Name: "public_get", NoAuth: true},
					{Name: "private_get", NoAuth: false},
					{Name: "also_private", NoAuth: false},
				},
			},
		},
	}
	am := NewActivationManager(s, entries)
	RegisterMetaTools(s, am)

	result := callMetaTool(t, s, "library_info", nil)
	var resp libraryInfoResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	require.Len(t, resp.APIs, 1)
	assert.Equal(t, 3, resp.APIs[0].ToolCount)
	assert.Equal(t, 1, resp.APIs[0].PublicToolCount)
}

// --- setup_guide ---

func TestSetupGuide_APIKeyAuth(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "setup_guide", map[string]any{"api_slug": "espn"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "ESPN")
	assert.Contains(t, text, "ESPN_KEY")
	assert.Contains(t, text, "api_key")
	assert.Contains(t, text, "claude mcp add")
}

func TestSetupGuide_BearerTokenAuth(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "setup_guide", map[string]any{"api_slug": "dub"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "DUB_TOKEN")
	assert.Contains(t, text, "bearer_token")
}

func TestSetupGuide_NoAuthAPI(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "setup_guide", map[string]any{"api_slug": "public-api"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "No authentication required")
}

func TestSetupGuide_UnknownSlug(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "setup_guide", map[string]any{"api_slug": "nonexistent"})
	assert.True(t, result.IsError)
	assertResultContains(t, result, "API not found")
}

func TestSetupGuide_MissingArg(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "setup_guide", nil)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "missing required argument")
}

func TestSetupGuide_WithKeyURL(t *testing.T) {
	s := newTestServer()
	entries := []*APIEntry{
		{
			Slug:             "withurl",
			NormalizedPrefix: "withurl",
			Manifest: &ToolsManifest{
				APIName: "WithURL",
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"MY_KEY"},
					KeyURL:  "https://example.com/settings/keys",
				},
				Tools: []ManifestTool{
					{Name: "data_get", Method: "GET", Path: "/data"},
				},
			},
		},
	}
	am := NewActivationManager(s, entries)
	RegisterMetaTools(s, am)

	result := callMetaTool(t, s, "setup_guide", map[string]any{"api_slug": "withurl"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "https://example.com/settings/keys")
}

// --- activate_api ---

func TestActivateAPI_RegistersTools(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "activate_api", map[string]any{"api_slug": "espn"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "Activated")
	assert.Contains(t, text, "3 tools registered")

	// Verify tools registered on server.
	tools := s.ListTools()
	assert.NotNil(t, tools["espn__scores_get"])
}

func TestActivateAPI_Idempotent(t *testing.T) {
	s, _ := newMetaToolsServer()

	result1 := callMetaTool(t, s, "activate_api", map[string]any{"api_slug": "espn"})
	assert.False(t, result1.IsError)

	result2 := callMetaTool(t, s, "activate_api", map[string]any{"api_slug": "espn"})
	assert.False(t, result2.IsError)

	// Should still have the same tool count.
	assertResultContains(t, result2, "3 tools registered")
}

func TestActivateAPI_UnknownSlug(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "activate_api", map[string]any{"api_slug": "nonexistent"})
	assert.True(t, result.IsError)
	assertResultContains(t, result, "API not found")
}

func TestActivateAPI_MissingArg(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "activate_api", nil)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "missing required argument")
}

func TestActivateAPI_ShowsExampleTools(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "activate_api", map[string]any{"api_slug": "espn"})
	assert.False(t, result.IsError)

	text := extractResultText(result)
	assert.Contains(t, text, "Example tools")
	assert.Contains(t, text, "espn__")
}

// --- deactivate_api ---

func TestDeactivateAPI_RemovesTools(t *testing.T) {
	s, am := newMetaToolsServer()

	_, err := am.Activate("espn")
	require.NoError(t, err)

	result := callMetaTool(t, s, "deactivate_api", map[string]any{"api_slug": "espn"})
	assert.False(t, result.IsError)
	assertResultContains(t, result, "Deactivated")

	// Verify tools removed from server.
	tools := s.ListTools()
	assert.Nil(t, tools["espn__scores_get"])
}

func TestDeactivateAPI_NotActivated(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "deactivate_api", map[string]any{"api_slug": "espn"})
	assert.True(t, result.IsError)
	assertResultContains(t, result, "not currently activated")
}

func TestDeactivateAPI_UnknownSlug(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "deactivate_api", map[string]any{"api_slug": "nonexistent"})
	assert.True(t, result.IsError)
	assertResultContains(t, result, "API not found")
}

// --- search_tools ---

func TestSearchTools_FindsMatches(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "search_tools", map[string]any{"query": "scores"})
	assert.False(t, result.IsError)

	var resp searchToolsResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	assert.Equal(t, 1, resp.Count)
	assert.Equal(t, "espn", resp.Results[0].APISlug)
	assert.Equal(t, "espn__scores_get", resp.Results[0].ToolName)
}

func TestSearchTools_EmptyResults(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "search_tools", map[string]any{"query": "pizza"})
	assert.False(t, result.IsError)

	var resp searchToolsResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	assert.Equal(t, 0, resp.Count)
	assert.Empty(t, resp.Results)
}

func TestSearchTools_SearchesAcrossUnactivatedAPIs(t *testing.T) {
	s, _ := newMetaToolsServer()

	// Neither ESPN nor Dub are activated, but search should still find tools.
	result := callMetaTool(t, s, "search_tools", map[string]any{"query": "list"})
	assert.False(t, result.IsError)

	var resp searchToolsResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	// Should match: espn__teams_list, dub__links_list, public_api__entries_list
	assert.GreaterOrEqual(t, resp.Count, 3)
}

func TestSearchTools_MissingArg(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "search_tools", nil)
	assert.True(t, result.IsError)
	assertResultContains(t, result, "missing required argument")
}

// --- about ---

func TestAbout_ReturnsVersionAndCounts(t *testing.T) {
	s, _ := newMetaToolsServer()

	result := callMetaTool(t, s, "about", nil)
	assert.False(t, result.IsError)

	var resp aboutResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	assert.Equal(t, "printing-press-mcp", resp.Name)
	assert.NotEmpty(t, resp.Version)
	assert.Equal(t, 3, resp.TotalAPIs)
	assert.Equal(t, 6, resp.TotalTools) // 3 + 2 + 1
}

func TestAbout_NoAPIs(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, nil)
	RegisterMetaTools(s, am)

	result := callMetaTool(t, s, "about", nil)
	assert.False(t, result.IsError)

	var resp aboutResponse
	require.NoError(t, json.Unmarshal([]byte(extractResultText(result)), &resp))

	assert.Equal(t, 0, resp.TotalAPIs)
	assert.Equal(t, 0, resp.TotalTools)
}

// --- getStringArg ---

func TestGetStringArg_Valid(t *testing.T) {
	req := makeToolRequest(map[string]any{"api_slug": "espn"})
	val, err := getStringArg(req, "api_slug")
	require.NoError(t, err)
	assert.Equal(t, "espn", val)
}

func TestGetStringArg_Missing(t *testing.T) {
	req := makeToolRequest(nil)
	_, err := getStringArg(req, "api_slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestGetStringArg_Empty(t *testing.T) {
	req := makeToolRequest(map[string]any{"api_slug": ""})
	_, err := getStringArg(req, "api_slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestGetStringArg_WrongType(t *testing.T) {
	req := makeToolRequest(map[string]any{"api_slug": 42})
	_, err := getStringArg(req, "api_slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")
}

// --- countPublicTools ---

func TestCountPublicTools_MixedAuth(t *testing.T) {
	m := &ToolsManifest{
		Auth: ManifestAuth{Type: "api_key"},
		Tools: []ManifestTool{
			{Name: "public", NoAuth: true},
			{Name: "private1", NoAuth: false},
			{Name: "private2", NoAuth: false},
		},
	}
	assert.Equal(t, 1, countPublicTools(m))
}

func TestCountPublicTools_NoAuth(t *testing.T) {
	m := &ToolsManifest{
		Auth: ManifestAuth{Type: "none"},
		Tools: []ManifestTool{
			{Name: "a"},
			{Name: "b"},
		},
	}
	// All tools are public when auth type is "none".
	assert.Equal(t, 2, countPublicTools(m))
}

func TestCountPublicTools_EmptyAuthType(t *testing.T) {
	m := &ToolsManifest{
		Auth: ManifestAuth{Type: ""},
		Tools: []ManifestTool{
			{Name: "a"},
		},
	}
	assert.Equal(t, 1, countPublicTools(m))
}
