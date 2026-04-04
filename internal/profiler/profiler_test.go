package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilePetstore(t *testing.T) {
	profile := Profile(petstoreSpec())

	assert.False(t, profile.HighVolume)
	assert.False(t, profile.NeedsSearch)
	assert.False(t, profile.HasRealtime)
	assert.Equal(t, []string{"export", "import"}, profile.RecommendedFeatures())
}

func TestProfileDiscord(t *testing.T) {
	profile := Profile(discordSpec())

	assert.True(t, profile.HighVolume)
	assert.True(t, profile.NeedsSearch)
	assert.True(t, profile.HasRealtime)
	assert.True(t, profile.HasChronological)
	assert.True(t, profile.HasDependencies)
	assert.ElementsMatch(t, []string{"sync", "search", "store", "export", "import", "tail", "analytics"}, profile.RecommendedFeatures())
	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}
	assert.Contains(t, syncNames, "messages")
	assert.Contains(t, profile.SearchableFields["messages"], "content")
}

func TestProfileMinimal(t *testing.T) {
	profile := Profile(minimalSpec())

	assert.False(t, profile.HighVolume)
	assert.False(t, profile.NeedsSearch)
	assert.False(t, profile.HasRealtime)
	assert.False(t, profile.HasChronological)
	assert.False(t, profile.HasDependencies)
	assert.Zero(t, profile.CRUDResources)
	assert.Equal(t, []string{"export", "import"}, profile.RecommendedFeatures())
}

func TestProfileEnumExpansion(t *testing.T) {
	// Simulates the postman-explore pattern: one list endpoint serves multiple
	// entity types via an enum query param (entityType=collection|workspace|api|flow).
	// The profiler should expand this into separate sync resources.
	// Uses distinct resource names to test enum expansion independently of naming.
	s := &spec.APISpec{
		Name: "postman-explore",
		Resources: map[string]spec.Resource{
			"networkentity": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/v1/api/networkentity",
						Params: []spec.Param{
							{
								Name:     "entityType",
								Type:     "string",
								Required: true,
								Enum:     []string{"collection", "workspace", "api", "flow"},
							},
							{Name: "limit", Type: "integer"},
							{Name: "offset", Type: "integer"},
						},
						Pagination: &spec.Pagination{
							CursorParam: "offset",
							LimitParam:  "limit",
						},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
			"team": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/v1/api/team",
						Params: []spec.Param{
							{Name: "limit", Type: "integer"},
						},
						Pagination: &spec.Pagination{
							CursorParam: "offset",
							LimitParam:  "limit",
						},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)

	syncNames := make([]string, len(profile.SyncableResources))
	syncPaths := make(map[string]string)
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
		syncPaths[sr.Name] = sr.Path
	}

	// 5 resources: 4 from enum expansion + 1 from teams
	assert.Len(t, profile.SyncableResources, 5)
	assert.Contains(t, syncNames, "collection")
	assert.Contains(t, syncNames, "workspace")
	assert.Contains(t, syncNames, "api")
	assert.Contains(t, syncNames, "flow")
	assert.Contains(t, syncNames, "team")

	// Expanded paths include the enum value as a query param
	assert.Equal(t, "/v1/api/networkentity?entityType=collection", syncPaths["collection"])
	assert.Equal(t, "/v1/api/networkentity?entityType=workspace", syncPaths["workspace"])
	assert.Equal(t, "/v1/api/networkentity?entityType=api", syncPaths["api"])
	// Teams endpoint keeps its own resource
	assert.Equal(t, "/v1/api/team", syncPaths["team"])
}

func TestProfileEnumExpansion_NoExpansionForNonEnum(t *testing.T) {
	// Standard API without enum params should not be affected
	profile := Profile(petstoreSpec())

	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}

	// Petstore has no enum query params — should NOT expand
	assert.NotContains(t, syncNames, "available")
	assert.NotContains(t, syncNames, "pending")
	assert.NotContains(t, syncNames, "sold")
}

func TestToVisionaryPlan(t *testing.T) {
	profile := Profile(discordSpec())
	plan := profile.ToVisionaryPlan("discord")

	require.NotNil(t, plan)
	assert.Equal(t, "discord", plan.APIName)
	assert.Equal(t, "high", plan.Identity.DataProfile.Volume)
	assert.Equal(t, "high", plan.Identity.DataProfile.SearchNeed)
	assert.True(t, plan.Identity.DataProfile.Realtime)

	areas := make(map[string]string)
	for _, decision := range plan.Architecture {
		areas[decision.Area] = decision.NeedLevel
	}
	assert.Equal(t, "high", areas["persistence"])
	assert.Equal(t, "high", areas["search"])
	assert.Equal(t, "high", areas["realtime"])

	featureTemplates := make(map[string][]string)
	for _, feature := range plan.Features {
		featureTemplates[feature.Name] = feature.TemplateNames
		assert.GreaterOrEqual(t, feature.TotalScore, 8)
	}
	assert.Equal(t, []string{"sync.go.tmpl"}, featureTemplates["sync"])
	assert.Equal(t, []string{"search.go.tmpl"}, featureTemplates["search"])
	assert.Equal(t, []string{"store.go.tmpl"}, featureTemplates["store"])
	assert.Equal(t, []string{"tail.go.tmpl"}, featureTemplates["tail"])
	assert.Equal(t, []string{"analytics.go.tmpl"}, featureTemplates["analytics"])
}

func petstoreSpec() *spec.APISpec {
	return &spec.APISpec{
		Name: "petstore",
		Resources: map[string]spec.Resource{
			"pets": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/pets",
						Response: spec.ResponseDef{Type: "array"},
					},
					"get": {
						Method:   "GET",
						Path:     "/pets/{petId}",
						Response: spec.ResponseDef{Type: "object"},
					},
					"create": {
						Method: "POST",
						Path:   "/pets",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
							{Name: "status", Type: "string", Enum: []string{"available", "pending", "sold"}},
						},
					},
					"update": {
						Method: "PUT",
						Path:   "/pets/{petId}",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
						},
					},
					"delete": {
						Method: "DELETE",
						Path:   "/pets/{petId}",
					},
					"findByStatus": {
						Method:   "GET",
						Path:     "/pets/findByStatus",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "status", Type: "string"},
						},
					},
				},
			},
			"store": {
				Endpoints: map[string]spec.Endpoint{
					"inventory": {
						Method:   "GET",
						Path:     "/store/inventory",
						Response: spec.ResponseDef{Type: "object"},
					},
					"order": {
						Method: "POST",
						Path:   "/store/order",
						Body: []spec.Param{
							{Name: "pet_id", Type: "integer"},
						},
					},
				},
			},
			"user": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/users",
						Response: spec.ResponseDef{Type: "array"},
					},
					"get": {
						Method:   "GET",
						Path:     "/users/{username}",
						Response: spec.ResponseDef{Type: "object"},
					},
					"create": {
						Method: "POST",
						Path:   "/users",
						Body: []spec.Param{
							{Name: "username", Type: "string"},
						},
					},
				},
			},
		},
	}
}

func minimalSpec() *spec.APISpec {
	return &spec.APISpec{
		Name: "minimal",
		Resources: map[string]spec.Resource{
			"widgets": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/widgets",
						Response: spec.ResponseDef{Type: "array"},
					},
					"get": {
						Method:   "GET",
						Path:     "/widgets/{widgetId}",
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
		},
	}
}

func discordSpec() *spec.APISpec {
	paginatedList := func(path string) spec.Endpoint {
		return spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Response:   spec.ResponseDef{Type: "array"},
			Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "before"},
		}
	}

	return &spec.APISpec{
		Name: "discord",
		Resources: map[string]spec.Resource{
			"guilds": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/guilds"),
					"get": {
						Method:   "GET",
						Path:     "/guilds/{guild_id}",
						Response: spec.ResponseDef{Type: "object"},
					},
					"create": {
						Method: "POST",
						Path:   "/guilds",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
							{Name: "region", Type: "string"},
							{Name: "status", Type: "string", Enum: []string{"active", "archived", "deleted"}},
						},
					},
					"update": {
						Method: "PATCH",
						Path:   "/guilds/{guild_id}",
						Body: []spec.Param{
							{Name: "name", Type: "string"},
							{Name: "state", Type: "string", Enum: []string{"draft", "active", "paused"}},
						},
					},
					"delete": {
						Method: "DELETE",
						Path:   "/guilds/{guild_id}",
					},
				},
			},
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/channels"),
					"create": {
						Method: "POST",
						Path:   "/channels",
						Body: []spec.Param{
							{Name: "guild_id", Type: "string"},
							{Name: "name", Type: "string"},
							{Name: "topic", Type: "string"},
						},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/channels/{channel_id}/messages"),
					"create": {
						Method: "POST",
						Path:   "/channels/{channel_id}/messages",
						Body: []spec.Param{
							{Name: "channel_id", Type: "string"},
							{Name: "author_id", Type: "string"},
							{Name: "content", Type: "string"},
							{Name: "title", Type: "string"},
							{Name: "summary", Type: "string"},
							{Name: "content_type", Type: "string"},
							{Name: "visibility", Type: "string"},
							{Name: "status", Type: "string", Enum: []string{"draft", "queued", "sent"}},
							{Name: "thread_id", Type: "string"},
							{Name: "reply_to_id", Type: "string"},
							{Name: "embed_title", Type: "string"},
							{Name: "embed_description", Type: "string"},
						},
					},
					"upload": {
						Method: "POST",
						Path:   "/channels/{channel_id}/attachments",
						Body: []spec.Param{
							{Name: "channel_id", Type: "string"},
							{Name: "file", Type: "file", Format: "binary"},
						},
					},
				},
			},
			"members": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/guilds/{guild_id}/members"),
					"create": {
						Method: "POST",
						Path:   "/guilds/{guild_id}/members",
						Body: []spec.Param{
							{Name: "guild_id", Type: "string"},
							{Name: "user_id", Type: "string"},
							{Name: "nick", Type: "string"},
						},
					},
				},
			},
			"roles": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/guilds/{guild_id}/roles"),
					"create": {
						Method: "POST",
						Path:   "/guilds/{guild_id}/roles",
						Body: []spec.Param{
							{Name: "guild_id", Type: "string"},
							{Name: "name", Type: "string"},
						},
					},
				},
			},
			"threads": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/channels/{channel_id}/threads"),
					"create": {
						Method: "POST",
						Path:   "/channels/{channel_id}/threads",
						Body: []spec.Param{
							{Name: "channel_id", Type: "string"},
							{Name: "name", Type: "string"},
						},
					},
				},
			},
			"reactions": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/channels/{channel_id}/messages/{message_id}/reactions"),
					"create": {
						Method: "POST",
						Path:   "/channels/{channel_id}/messages/{message_id}/reactions",
						Body: []spec.Param{
							{Name: "channel_id", Type: "string"},
							{Name: "message_id", Type: "string"},
							{Name: "emoji", Type: "string"},
						},
					},
				},
			},
			"webhooks": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/webhooks"),
					"create": {
						Method: "POST",
						Path:   "/webhooks",
						Body: []spec.Param{
							{Name: "channel_id", Type: "string"},
							{Name: "callback_url", Type: "string"},
						},
					},
				},
			},
			"audit-logs": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/guilds/{guild_id}/audit-logs",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "before"},
						Params: []spec.Param{
							{Name: "before", Type: "string", Description: "Return entries before this timestamp"},
							{Name: "sort", Type: "string", Description: "Sort by created timestamp descending"},
						},
					},
				},
			},
			"notifications": {
				Endpoints: map[string]spec.Endpoint{
					"list": paginatedList("/users/{user_id}/notifications"),
					"create": {
						Method: "POST",
						Path:   "/users/{user_id}/notifications",
						Body: []spec.Param{
							{Name: "user_id", Type: "string"},
							{Name: "message", Type: "string"},
						},
					},
				},
			},
		},
	}
}
