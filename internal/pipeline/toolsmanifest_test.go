package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteToolsManifest_MultipleResources(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:        "petstore",
		Description: "A sample pet store API",
		BaseURL:     "https://petstore.example.com/v3",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {PETSTORE_KEY}",
			In:      "header",
			EnvVars: []string{"PETSTORE_KEY"},
			KeyURL:  "https://petstore.example.com/keys",
		},
		RequiredHeaders: []spec.RequiredHeader{
			{Name: "X-Api-Version", Value: "2024-01-01"},
		},
		Resources: map[string]spec.Resource{
			"Pets": {
				Description: "Pet operations",
				Endpoints: map[string]spec.Endpoint{
					"List": {
						Method:      "GET",
						Path:        "/pets",
						Description: "List all pets",
						Params: []spec.Param{
							{Name: "limit", Type: "integer", Required: false, Description: "Max items to return"},
							{Name: "status", Type: "string", Required: false, Description: "Filter by status"},
						},
					},
					"Get": {
						Method:      "GET",
						Path:        "/pets/{petId}",
						Description: "Get a pet by ID",
						Params: []spec.Param{
							{Name: "petId", Type: "string", Required: true, Positional: true, Description: "The pet ID"},
						},
					},
					"Create": {
						Method:      "POST",
						Path:        "/pets",
						Description: "Create a new pet",
						Body: []spec.Param{
							{Name: "name", Type: "string", Required: true, Description: "Pet name"},
							{Name: "tag", Type: "string", Required: false, Description: "Pet tag"},
						},
					},
				},
			},
			"Store": {
				Description: "Store operations",
				Endpoints: map[string]spec.Endpoint{
					"GetInventory": {
						Method:      "GET",
						Path:        "/store/inventory",
						Description: "Returns pet inventories by status",
					},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// API-level metadata
	assert.Equal(t, "petstore", got.APIName)
	assert.Equal(t, "https://petstore.example.com/v3", got.BaseURL)
	assert.Equal(t, "A sample pet store API", got.Description)
	assert.Equal(t, "full", got.MCPReady)

	// Auth
	assert.Equal(t, "api_key", got.Auth.Type)
	assert.Equal(t, "Authorization", got.Auth.Header)
	assert.Equal(t, "Bearer {PETSTORE_KEY}", got.Auth.Format)
	assert.Equal(t, "header", got.Auth.In)
	assert.Equal(t, []string{"PETSTORE_KEY"}, got.Auth.EnvVars)
	assert.Equal(t, "https://petstore.example.com/keys", got.Auth.KeyURL)

	// Required headers
	require.Len(t, got.RequiredHeaders, 1)
	assert.Equal(t, "X-Api-Version", got.RequiredHeaders[0].Name)
	assert.Equal(t, "2024-01-01", got.RequiredHeaders[0].Value)

	// Tools: 4 total (3 from Pets + 1 from Store), sorted by resource then endpoint
	require.Len(t, got.Tools, 4)

	// Pets comes before Store alphabetically
	assert.Equal(t, "pets_create", got.Tools[0].Name)
	assert.Equal(t, "POST", got.Tools[0].Method)
	assert.Equal(t, "/pets", got.Tools[0].Path)

	assert.Equal(t, "pets_get", got.Tools[1].Name)
	assert.Equal(t, "GET", got.Tools[1].Method)
	assert.Equal(t, "/pets/{petId}", got.Tools[1].Path)

	assert.Equal(t, "pets_list", got.Tools[2].Name)
	assert.Equal(t, "GET", got.Tools[2].Method)
	assert.Equal(t, "/pets", got.Tools[2].Path)

	assert.Equal(t, "store_get_inventory", got.Tools[3].Name)
	assert.Equal(t, "GET", got.Tools[3].Method)
	assert.Equal(t, "/store/inventory", got.Tools[3].Path)
}

func TestWriteToolsManifest_SubResources(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "guild-api",
		BaseURL: "https://api.guild.com",
		Auth:    spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"GUILD_TOKEN"}},
		Resources: map[string]spec.Resource{
			"Guild": {
				Endpoints: map[string]spec.Endpoint{
					"Get": {Method: "GET", Path: "/guilds/{guildId}", Description: "Get a guild",
						Params: []spec.Param{
							{Name: "guildId", Type: "string", Required: true, Positional: true},
						}},
				},
				SubResources: map[string]spec.Resource{
					"Members": {
						Endpoints: map[string]spec.Endpoint{
							"List": {Method: "GET", Path: "/guilds/{guildId}/members", Description: "List guild members",
								Params: []spec.Param{
									{Name: "guildId", Type: "string", Required: true, Positional: true},
									{Name: "limit", Type: "integer", Required: false},
								}},
							"Get": {Method: "GET", Path: "/guilds/{guildId}/members/{userId}", Description: "Get a guild member",
								Params: []spec.Param{
									{Name: "guildId", Type: "string", Required: true, Positional: true},
									{Name: "userId", Type: "string", Required: true, Positional: true},
								}},
						},
					},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 3)

	// Top-level endpoint
	assert.Equal(t, "guild_get", got.Tools[0].Name)

	// Sub-resource endpoints: three-segment names
	assert.Equal(t, "guild_members_get", got.Tools[1].Name)
	assert.Equal(t, "guild_members_list", got.Tools[2].Name)
}

func TestWriteToolsManifest_ParamLocationClassification(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "test-api",
		BaseURL: "https://api.test.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"Update": {
						Method: "PUT",
						Path:   "/items/{itemId}",
						Params: []spec.Param{
							{Name: "itemId", Type: "string", Required: true, Positional: true, Description: "Item identifier"},
							{Name: "filter", Type: "string", Required: false, Description: "Optional filter"},
						},
						Body: []spec.Param{
							{Name: "name", Type: "string", Required: true, Description: "Item name"},
							{Name: "tags", Type: "string", Required: false, Description: "Item tags"},
						},
					},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 1)
	tool := got.Tools[0]
	require.Len(t, tool.Params, 4)

	// Positional → path
	assert.Equal(t, "itemId", tool.Params[0].Name)
	assert.Equal(t, "path", tool.Params[0].Location)
	assert.True(t, tool.Params[0].Required)

	// Non-positional regular param → query
	assert.Equal(t, "filter", tool.Params[1].Name)
	assert.Equal(t, "query", tool.Params[1].Location)
	assert.False(t, tool.Params[1].Required)

	// Body params → body
	assert.Equal(t, "name", tool.Params[2].Name)
	assert.Equal(t, "body", tool.Params[2].Location)
	assert.True(t, tool.Params[2].Required)

	assert.Equal(t, "tags", tool.Params[3].Name)
	assert.Equal(t, "body", tool.Params[3].Location)
	assert.False(t, tool.Params[3].Required)
}

func TestWriteToolsManifest_PublicParamNames(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "public-params",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"stores": {
				Description: "Stores",
				Endpoints: map[string]spec.Endpoint{
					"find": {
						Method:      "GET",
						Path:        "/stores",
						Description: "Find stores",
						Params: []spec.Param{
							{Name: "s", FlagName: "address", Aliases: []string{"s"}, Type: "string", Required: true, Description: "Street address"},
						},
					},
					"create": {
						Method:      "POST",
						Path:        "/stores",
						Description: "Create store",
						Body: []spec.Param{
							{Name: "store_code", FlagName: "store-code", Aliases: []string{"code"}, Type: "string", Required: true, Description: "Store code"},
						},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))
	got, err := ReadToolsManifest(dir)
	require.NoError(t, err)

	require.Len(t, got.Tools, 2)
	var find, create ManifestTool
	for _, tool := range got.Tools {
		switch tool.Name {
		case "stores_find":
			find = tool
		case "stores_create":
			create = tool
		}
	}
	require.Len(t, find.Params, 1)
	assert.Equal(t, "address", find.Params[0].Name)
	assert.Equal(t, "s", find.Params[0].WireName)
	assert.Equal(t, []string{"s"}, find.Params[0].Aliases)

	require.Len(t, create.Params, 1)
	assert.Equal(t, "store-code", create.Params[0].Name)
	assert.Equal(t, "store_code", create.Params[0].WireName)
	assert.Equal(t, []string{"code"}, create.Params[0].Aliases)
}

func TestWriteToolsManifest_IdentNamePublicParamName(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "deduped-params",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"posts": {
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method: "POST",
						Path:   "/posts",
						Params: []spec.Param{
							{Name: "id", Type: "string", Description: "Query ID"},
						},
						Body: []spec.Param{
							{Name: "id", IdentName: "id_2", Type: "string", Description: "Body ID"},
						},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))
	got, err := ReadToolsManifest(dir)
	require.NoError(t, err)

	require.Len(t, got.Tools, 1)
	require.Len(t, got.Tools[0].Params, 2)
	assert.Equal(t, "id", got.Tools[0].Params[0].Name)
	assert.Empty(t, got.Tools[0].Params[0].WireName)
	assert.Equal(t, "id-2", got.Tools[0].Params[1].Name)
	assert.Equal(t, "id", got.Tools[0].Params[1].WireName)
}

// TestWriteToolsManifest_ReclassifiedPathParamKeepsPathLocation pins
// the path location for path params that reclassifyPathParamModifiers
// converted from positional args to flags (e.g., enum-typed path
// params like /v2/calendars/{calendar} where calendar has
// enum: [apple, google, office365]). Without checking PathParam
// alongside Positional, these end up location: "query" and the
// description-override agent can't tell they're URL-substituted.
func TestWriteToolsManifest_ReclassifiedPathParamKeepsPathLocation(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "test-api",
		BaseURL: "https://api.test.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Calendars": {
				Endpoints: map[string]spec.Endpoint{
					"Disconnect": {
						Method: "POST",
						Path:   "/calendars/{calendar}/disconnect",
						Params: []spec.Param{
							{
								Name:        "calendar",
								Type:        "string",
								Description: "Calendar provider",
								Enum:        []string{"apple", "google", "office365"},
								Default:     "apple",
								Positional:  false, // reclassified: enum + default → flag
								PathParam:   true,  // ...but still substituted into URL
								Required:    false, // CLI default fills in if omitted
							},
						},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)
	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 1)
	require.Len(t, got.Tools[0].Params, 1)
	calendar := got.Tools[0].Params[0]
	assert.Equal(t, "calendar", calendar.Name)
	assert.Equal(t, "path", calendar.Location, "reclassified path param must still be location: path")
	assert.False(t, calendar.Required, "default fills in if omitted; agent may skip the param")
}

func TestWriteToolsManifest_CompactsRepeatedParamDescriptions(t *testing.T) {
	dir := t.TempDir()
	sharedDescription := "Select additional nested resource fields to include in the response. Use comma-separated field names such as owner, permissions, metadata, relationships, and auditTrail; unsupported values are ignored by the upstream API."
	parsed := &spec.APISpec{
		Name:    "test-api",
		BaseURL: "https://api.test.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
					},
					"Search": {
						Method:      "GET",
						Path:        "/items/search",
						Description: "Search items",
						Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
					},
					"Recent": {
						Method:      "GET",
						Path:        "/items/recent",
						Description: "List recent items",
						Params:      []spec.Param{{Name: "expand", Type: "string", Description: sharedDescription}},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)
	assert.NotContains(t, string(data), sharedDescription,
		"tools-manifest.json should not repeat long shared parameter descriptions verbatim")

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got.Tools, 3)
	for _, tool := range got.Tools {
		require.Len(t, tool.Params, 1)
		assert.Equal(t, "Select additional nested resource fields to include in the response.", tool.Params[0].Description)
	}
}

func TestWriteToolsManifest_PreservesUniqueLongParamDescriptions(t *testing.T) {
	dir := t.TempDir()
	description := "Filter records by a curated vendor-specific field path, including deeply nested owner metadata, lifecycle state, and audit fields. Allowed values: owner.profile.email, lifecycle.status, audit.actor, audit.requestId."
	parsed := &spec.APISpec{
		Name:    "test-api",
		BaseURL: "https://api.test.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
						Params:      []spec.Param{{Name: "filter", Type: "string", Description: description}},
					},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got.Tools, 1)
	require.Len(t, got.Tools[0].Params, 1)
	assert.Equal(t, naming.OneLineNormalize(description), got.Tools[0].Params[0].Description)
	assert.Contains(t, got.Tools[0].Params[0].Description, "audit.requestId")
}

func TestWriteToolsManifest_AuthConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "auth-test",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-API-Key",
			Format:  "{MY_KEY}",
			In:      "header",
			EnvVars: []string{"MY_KEY", "MY_BACKUP_KEY"},
			KeyURL:  "https://example.com/api-keys",
		},
		Resources: map[string]spec.Resource{
			"Things": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/things", Description: "List things"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "api_key", got.Auth.Type)
	assert.Equal(t, "X-API-Key", got.Auth.Header)
	assert.Equal(t, "{MY_KEY}", got.Auth.Format)
	assert.Equal(t, "header", got.Auth.In)
	assert.Equal(t, []string{"MY_KEY", "MY_BACKUP_KEY"}, got.Auth.EnvVars)
	assert.Equal(t, []spec.AuthEnvVar{
		{Name: "MY_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
		{Name: "MY_BACKUP_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
	}, got.Auth.EnvVarSpecs)
	assert.Equal(t, "https://example.com/api-keys", got.Auth.KeyURL)
}

func TestManifestAuthEffectiveEnvVarSpecsLegacyFallback(t *testing.T) {
	got := (ManifestAuth{
		Type:    "api_key",
		EnvVars: []string{"LEGACY_TOKEN"},
	}).EffectiveEnvVarSpecs()
	assert.Equal(t, []spec.AuthEnvVar{{
		Name:      "LEGACY_TOKEN",
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}}, got)
}

func TestWriteToolsManifest_NoAuthEndpointsFlagged(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "mixed-auth",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"TOKEN"}},
		Resources: map[string]spec.Resource{
			"Data": {
				Endpoints: map[string]spec.Endpoint{
					"PublicList": {Method: "GET", Path: "/data", Description: "Public data", NoAuth: true},
					"PrivateGet": {Method: "GET", Path: "/data/{id}", Description: "Private data",
						Params: []spec.Param{{Name: "id", Type: "string", Positional: true, Required: true}}},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 2)
	// Sorted: PrivateGet before PublicList
	assert.Equal(t, "data_private_get", got.Tools[0].Name)
	assert.False(t, got.Tools[0].NoAuth)

	assert.Equal(t, "data_public_list", got.Tools[1].Name)
	assert.True(t, got.Tools[1].NoAuth)
}

func TestWriteToolsManifest_CookieAuthOnlyNoAuthEndpoints(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "cookie-api",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "cookie", EnvVars: []string{"COOKIE"}},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"PublicList":  {Method: "GET", Path: "/items", Description: "Public items", NoAuth: true},
					"PrivateGet":  {Method: "GET", Path: "/items/{id}", Description: "Private item"},
					"PublicCount": {Method: "GET", Path: "/items/count", Description: "Public item count", NoAuth: true},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// Only NoAuth endpoints should be included for cookie auth
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "items_public_count", got.Tools[0].Name)
	assert.Equal(t, "items_public_list", got.Tools[1].Name)
	assert.Equal(t, "partial", got.MCPReady)
}

func TestWriteToolsManifest_ComposedAuthOnlyNoAuthEndpoints(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "composed-api",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "composed", EnvVars: []string{"AUTH_TOKEN"}},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"PublicList": {Method: "GET", Path: "/items", Description: "Public items", NoAuth: true},
					"PrivateGet": {Method: "GET", Path: "/items/{id}", Description: "Private item"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 1)
	assert.Equal(t, "items_public_list", got.Tools[0].Name)
}

func TestWriteToolsManifest_EmptyDescription(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "no-desc",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/items"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "", got.Description)
	require.Len(t, got.Tools, 1)
	assert.Equal(t, "", got.Tools[0].Description)
}

func TestWriteToolsManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:        "roundtrip-api",
		Description: "A test API for round-trip verification",
		BaseURL:     "https://api.roundtrip.com",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {RT_TOKEN}",
			In:      "header",
			EnvVars: []string{"RT_TOKEN"},
			KeyURL:  "https://roundtrip.com/keys",
		},
		RequiredHeaders: []spec.RequiredHeader{
			{Name: "X-Version", Value: "2"},
		},
		HTTPTransport: spec.HTTPTransportBrowserChromeH3,
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/items", Description: "List items", NoAuth: true,
						Params: []spec.Param{
							{Name: "limit", Type: "integer", Required: false, Description: "Max results"},
						}},
					"Get": {Method: "GET", Path: "/items/{id}", Description: "Get an item",
						Params: []spec.Param{
							{Name: "id", Type: "string", Required: true, Positional: true, Description: "Item ID"},
						}},
					"Create": {Method: "POST", Path: "/items", Description: "Create item",
						Body: []spec.Param{
							{Name: "name", Type: "string", Required: true, Description: "Item name"},
						},
						HeaderOverrides: []spec.RequiredHeader{
							{Name: "Content-Type", Value: "application/json"},
						},
					},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	// Read back
	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// Verify all API-level fields
	assert.Equal(t, "roundtrip-api", got.APIName)
	assert.Equal(t, "https://api.roundtrip.com", got.BaseURL)
	assert.Equal(t, "A test API for round-trip verification", got.Description)
	assert.Equal(t, "full", got.MCPReady) // bearer_token → full
	assert.Equal(t, spec.HTTPTransportBrowserChromeH3, got.HTTPTransport)
	assert.Equal(t, "bearer_token", got.Auth.Type)
	assert.Equal(t, "Authorization", got.Auth.Header)
	assert.Equal(t, "Bearer {RT_TOKEN}", got.Auth.Format)
	assert.Equal(t, "header", got.Auth.In)
	assert.Equal(t, []string{"RT_TOKEN"}, got.Auth.EnvVars)
	assert.Equal(t, "https://roundtrip.com/keys", got.Auth.KeyURL)
	require.Len(t, got.RequiredHeaders, 1)
	assert.Equal(t, "X-Version", got.RequiredHeaders[0].Name)
	assert.Equal(t, "2", got.RequiredHeaders[0].Value)

	// Verify tools
	require.Len(t, got.Tools, 3)

	// items_create
	assert.Equal(t, "items_create", got.Tools[0].Name)
	assert.Equal(t, "POST", got.Tools[0].Method)
	assert.Equal(t, "/items", got.Tools[0].Path)
	assert.False(t, got.Tools[0].NoAuth)
	require.Len(t, got.Tools[0].Params, 1)
	assert.Equal(t, "body", got.Tools[0].Params[0].Location)
	require.Len(t, got.Tools[0].HeaderOverrides, 1)
	assert.Equal(t, "Content-Type", got.Tools[0].HeaderOverrides[0].Name)

	// items_get
	assert.Equal(t, "items_get", got.Tools[1].Name)
	assert.Equal(t, "GET", got.Tools[1].Method)
	assert.Equal(t, "/items/{id}", got.Tools[1].Path)
	require.Len(t, got.Tools[1].Params, 1)
	assert.Equal(t, "path", got.Tools[1].Params[0].Location)

	// items_list
	assert.Equal(t, "items_list", got.Tools[2].Name)
	assert.True(t, got.Tools[2].NoAuth)
	require.Len(t, got.Tools[2].Params, 1)
	assert.Equal(t, "query", got.Tools[2].Params[0].Location)
}

func TestWriteToolsManifest_DeterministicJSON(t *testing.T) {
	parsed := &spec.APISpec{
		Name:    "deterministic",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Zebra": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/zebras", Description: "List zebras"},
				},
			},
			"Apple": {
				Endpoints: map[string]spec.Endpoint{
					"Get":  {Method: "GET", Path: "/apples/{id}", Description: "Get apple"},
					"List": {Method: "GET", Path: "/apples", Description: "List apples"},
				},
			},
		},
	}

	// Write twice to different dirs
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	require.NoError(t, WriteToolsManifest(dir1, parsed))
	require.NoError(t, WriteToolsManifest(dir2, parsed))

	data1, err := os.ReadFile(filepath.Join(dir1, ToolsManifestFilename))
	require.NoError(t, err)
	data2, err := os.ReadFile(filepath.Join(dir2, ToolsManifestFilename))
	require.NoError(t, err)

	// Byte-identical output
	assert.Equal(t, string(data1), string(data2))

	// Verify ordering: Apple before Zebra
	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data1, &got))
	require.Len(t, got.Tools, 3)
	assert.Equal(t, "apple_get", got.Tools[0].Name)
	assert.Equal(t, "apple_list", got.Tools[1].Name)
	assert.Equal(t, "zebra_list", got.Tools[2].Name)
}

func TestWriteToolsManifest_NilSpec(t *testing.T) {
	dir := t.TempDir()
	err := WriteToolsManifest(dir, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWriteToolsManifest_NonexistentDir(t *testing.T) {
	parsed := &spec.APISpec{
		Name:    "test",
		BaseURL: "https://api.test.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"X": {Endpoints: map[string]spec.Endpoint{
				"Y": {Method: "GET", Path: "/x"},
			}},
		},
	}
	err := WriteToolsManifest("/nonexistent/path/does/not/exist", parsed)
	assert.Error(t, err)
}

func TestWriteToolsManifest_NoAuthType(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "no-auth-api",
		BaseURL: "https://api.public.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Data": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/data", Description: "Public data"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "none", got.Auth.Type)
	assert.Empty(t, got.Auth.Header)
	assert.Empty(t, got.Auth.EnvVars)
	assert.Equal(t, "full", got.MCPReady)
}

func TestWriteToolsManifest_RequiredHeadersIncluded(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "headers-api",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", EnvVars: []string{"KEY"}},
		RequiredHeaders: []spec.RequiredHeader{
			{Name: "X-Version", Value: "3"},
			{Name: "X-Client", Value: "printing-press"},
		},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/items"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.RequiredHeaders, 2)
	assert.Equal(t, "X-Version", got.RequiredHeaders[0].Name)
	assert.Equal(t, "3", got.RequiredHeaders[0].Value)
	assert.Equal(t, "X-Client", got.RequiredHeaders[1].Name)
	assert.Equal(t, "printing-press", got.RequiredHeaders[1].Value)
}

func TestWriteToolsManifest_HeaderOverrides(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "overrides-api",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"Upload": {
						Method: "POST", Path: "/items/upload", Description: "Upload an item",
						HeaderOverrides: []spec.RequiredHeader{
							{Name: "Content-Type", Value: "multipart/form-data"},
						},
					},
					"List": {
						Method: "GET", Path: "/items", Description: "List items",
						// No header overrides
					},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 2)
	// List (no overrides)
	assert.Equal(t, "items_list", got.Tools[0].Name)
	assert.Nil(t, got.Tools[0].HeaderOverrides)
	// Upload (with overrides)
	assert.Equal(t, "items_upload", got.Tools[1].Name)
	require.Len(t, got.Tools[1].HeaderOverrides, 1)
	assert.Equal(t, "Content-Type", got.Tools[1].HeaderOverrides[0].Name)
	assert.Equal(t, "multipart/form-data", got.Tools[1].HeaderOverrides[0].Value)
}

func TestWriteToolsManifest_MCPDescriptionAnnotations(t *testing.T) {
	dir := t.TempDir()
	// 2 public, 5 auth-required → public is minority → public gets "(public)" annotation
	parsed := &spec.APISpec{
		Name:    "mixed-api",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", EnvVars: []string{"KEY"}},
		Resources: map[string]spec.Resource{
			"A": {
				Endpoints: map[string]spec.Endpoint{
					"E1": {Method: "GET", Path: "/a/1", Description: "Public endpoint 1", NoAuth: true},
					"E2": {Method: "GET", Path: "/a/2", Description: "Public endpoint 2", NoAuth: true},
					"E3": {Method: "GET", Path: "/a/3", Description: "Private endpoint 1"},
					"E4": {Method: "GET", Path: "/a/4", Description: "Private endpoint 2"},
					"E5": {Method: "GET", Path: "/a/5", Description: "Private endpoint 3"},
					"E6": {Method: "GET", Path: "/a/6", Description: "Private endpoint 4"},
					"E7": {Method: "GET", Path: "/a/7", Description: "Private endpoint 5"},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// Find the public endpoints — they should have "(public)" annotation
	for _, tool := range got.Tools {
		if tool.NoAuth {
			assert.Contains(t, tool.Description, "(public)", "public minority endpoints should be annotated")
		}
	}
}

func TestWriteToolsManifest_EmptyParamType(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "empty-type",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"Items": {
				Endpoints: map[string]spec.Endpoint{
					"List": {Method: "GET", Path: "/items",
						Params: []spec.Param{
							{Name: "filter", Type: "", Required: false}, // empty type
						}},
				},
			},
		},
	}

	err := WriteToolsManifest(dir, parsed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	require.NoError(t, err)

	var got ToolsManifest
	require.NoError(t, json.Unmarshal(data, &got))

	require.Len(t, got.Tools, 1)
	require.Len(t, got.Tools[0].Params, 1)
	assert.Equal(t, "string", got.Tools[0].Params[0].Type, "empty type should default to string")
}

func TestWriteToolsManifest_TierRoutingMetadata(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "tiered",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"GLOBAL_TOKEN"}},
		TierRouting: spec.TierRoutingConfig{
			DefaultTier: "free",
			Tiers: map[string]spec.TierConfig{
				"free": {Auth: spec.AuthConfig{Type: "none"}},
				"paid": {
					BaseURL: "https://paid.api.example.com",
					Auth: spec.AuthConfig{
						Type:    "api_key",
						In:      "query",
						Header:  "api_key",
						EnvVars: []string{"PAID_KEY"},
					},
				},
			},
		},
		Resources: map[string]spec.Resource{
			"items": {
				Tier: "free",
				Endpoints: map[string]spec.Endpoint{
					"list":    {Method: "GET", Path: "/items"},
					"premium": {Method: "GET", Path: "/items/premium", Tier: "paid"},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))
	got, err := ReadToolsManifest(dir)
	require.NoError(t, err)

	require.NotNil(t, got.TierRouting)
	assert.Equal(t, "free", got.TierRouting.DefaultTier)
	assert.Equal(t, "none", got.TierRouting.Tiers["free"].Auth.Type)
	assert.Equal(t, "https://paid.api.example.com", got.TierRouting.Tiers["paid"].BaseURL)
	assert.Equal(t, []string{"PAID_KEY"}, got.TierRouting.Tiers["paid"].Auth.EnvVars)

	require.Len(t, got.Tools, 2)
	assert.Equal(t, "free", got.Tools[0].Tier)
	assert.Equal(t, "paid", got.Tools[1].Tier)
}

func TestWriteToolsManifest_TierRoutingEffectiveAuth(t *testing.T) {
	dir := t.TempDir()
	parsed := &spec.APISpec{
		Name:    "tiered",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "cookie", EnvVars: []string{"SESSION_COOKIE"}},
		TierRouting: spec.TierRoutingConfig{
			Tiers: map[string]spec.TierConfig{
				"free": {Auth: spec.AuthConfig{Type: "none"}},
				"paid": {
					Auth: spec.AuthConfig{
						Type:    "api_key",
						Header:  "api_key",
						EnvVars: []string{"PAID_KEY"},
					},
				},
			},
		},
		Resources: map[string]spec.Resource{
			"items": {
				Tier: "free",
				Endpoints: map[string]spec.Endpoint{
					"list":    {Method: "GET", Path: "/items"},
					"premium": {Method: "GET", Path: "/items/premium", Tier: "paid"},
				},
			},
			"global": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/global"},
				},
			},
		},
	}

	require.NoError(t, WriteToolsManifest(dir, parsed))
	got, err := ReadToolsManifest(dir)
	require.NoError(t, err)

	require.Len(t, got.Tools, 2)
	assert.Equal(t, "items_list", got.Tools[0].Name)
	assert.True(t, got.Tools[0].NoAuth)
	assert.Equal(t, "items_premium", got.Tools[1].Name)
	assert.False(t, got.Tools[1].NoAuth)
	assert.Equal(t, "paid", got.Tools[1].Tier)
}

func TestNormalizeAuthFormat(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		envVars []string
		want    string
	}{
		{
			name:    "already uses env var name",
			format:  "Bearer {DUB_TOKEN}",
			envVars: []string{"DUB_TOKEN"},
			want:    "Bearer {DUB_TOKEN}",
		},
		{
			name:    "derived placeholder replaced with env var",
			format:  "Bearer {token}",
			envVars: []string{"DUB_TOKEN"},
			want:    "Bearer {DUB_TOKEN}",
		},
		{
			name:    "semantic access_token replaced",
			format:  "Bearer {access_token}",
			envVars: []string{"GITHUB_TOKEN"},
			want:    "Bearer {GITHUB_TOKEN}",
		},
		{
			name:    "multi-part derived placeholder",
			format:  "Basic {project_id}:{secret}",
			envVars: []string{"STYTCH_PROJECT_ID", "STYTCH_SECRET"},
			want:    "Basic {STYTCH_PROJECT_ID}:{STYTCH_SECRET}",
		},
		{
			name:    "empty format stays empty",
			format:  "",
			envVars: []string{"TOKEN"},
			want:    "",
		},
		{
			name:    "no env vars stays unchanged",
			format:  "Bearer {token}",
			envVars: nil,
			want:    "Bearer {token}",
		},
		{
			name:    "api_key semantic alias replaced",
			format:  "ApiKey {api_key}",
			envVars: []string{"STEAM_WEB_API_KEY"},
			want:    "ApiKey {STEAM_WEB_API_KEY}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAuthFormat(tt.format, tt.envVars)
			assert.Equal(t, tt.want, got)
		})
	}
}
