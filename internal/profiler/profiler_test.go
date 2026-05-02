package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
	// Messages has a parameterized path (/channels/{channel_id}/messages) so it
	// should NOT be in flat SyncableResources - it goes to DependentSyncResources.
	assert.NotContains(t, syncNames, "messages")
	assert.Contains(t, profile.SearchableFields["messages"], "content")

	// Dependent resources should be detected for parameterized paths
	depNames := make([]string, len(profile.DependentSyncResources))
	for i, dr := range profile.DependentSyncResources {
		depNames[i] = dr.Name
	}
	assert.Contains(t, depNames, "messages")
	assert.Contains(t, depNames, "threads")
	assert.Contains(t, depNames, "members")
	assert.Contains(t, depNames, "roles")
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

	// 6 resources: 4 from enum expansion + networkentity itself + teams
	assert.Len(t, profile.SyncableResources, 6)
	assert.Contains(t, syncNames, "collection")
	assert.Contains(t, syncNames, "workspace")
	assert.Contains(t, syncNames, "api")
	assert.Contains(t, syncNames, "flow")
	assert.Contains(t, syncNames, "networkentity")
	assert.Contains(t, syncNames, "team")

	// Expanded paths include the enum value as a query param
	assert.Equal(t, "/v1/api/networkentity?entityType=collection", syncPaths["collection"])
	assert.Equal(t, "/v1/api/networkentity?entityType=workspace", syncPaths["workspace"])
	assert.Equal(t, "/v1/api/networkentity?entityType=api", syncPaths["api"])
	// Teams endpoint keeps its own resource
	assert.Equal(t, "/v1/api/team", syncPaths["team"])
}

func TestProfileDiscriminatorDispatchFromResponseTypeEnum(t *testing.T) {
	s := &spec.APISpec{
		Name: "mixed-network",
		Resources: map[string]spec.Resource{
			"network_entities": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/network-entities",
						Response:   spec.ResponseDef{Type: "array", Item: "NetworkEntity"},
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
			"workspaces":  {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/workspaces"}}},
			"collections": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/collections"}}},
			"teams":       {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/teams"}}},
		},
		Types: map[string]spec.TypeDef{
			"NetworkEntity": {
				Fields: []spec.TypeField{
					{Name: "type", Type: "string", Enum: []string{"workspace", "collection", "team", "unknown"}},
					{Name: "id", Type: "string"},
				},
			},
		},
	}

	profile := Profile(s)

	var mixed SyncableResource
	for _, resource := range profile.SyncableResources {
		if resource.Name == "network_entities" {
			mixed = resource
			break
		}
	}
	require.Equal(t, "network_entities", mixed.Name)
	require.Equal(t, "type", mixed.Discriminator.Field)
	assert.Equal(t, []DiscriminatorMapping{
		{Value: "collection", Resource: "collections"},
		{Value: "team", Resource: "teams"},
		{Value: "workspace", Resource: "workspaces"},
	}, mixed.Discriminator.Mappings)
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

func TestProfileDateRangeParam(t *testing.T) {
	s := &spec.APISpec{
		Name: "sportsdata",
		Resources: map[string]spec.Resource{
			"scoreboard": {
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method: "GET",
						Path:   "/scoreboard",
						Params: []spec.Param{
							{Name: "dates", Type: "string", Description: "Date range YYYYMMDD-YYYYMMDD"},
							{Name: "limit", Type: "int", Default: 100},
						},
						Response: spec.ResponseDef{Type: "object", Item: "ScoreboardResponse"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"ScoreboardResponse": {
				Fields: []spec.TypeField{
					{Name: "events", Type: "string"},
					{Name: "leagues", Type: "string"},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Equal(t, "dates", profile.Pagination.DateRangeParam)
}

func TestProfileDateRangeParam_SingularDateNotMatched(t *testing.T) {
	s := &spec.APISpec{
		Name: "calendar",
		Resources: map[string]spec.Resource{
			"events": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/events",
						Params:   []spec.Param{{Name: "date", Type: "string"}, {Name: "limit", Type: "int"}},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Empty(t, profile.Pagination.DateRangeParam, "singular 'date' must not match DateRangeParam")
}

func TestProfileWrapperObjectDetection(t *testing.T) {
	s := &spec.APISpec{
		Name: "sportsdata",
		Resources: map[string]spec.Resource{
			"scoreboard": {
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:   "GET",
						Path:     "/scoreboard",
						Response: spec.ResponseDef{Type: "object", Item: "ScoreboardResponse"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"ScoreboardResponse": {
				Fields: []spec.TypeField{
					{Name: "events", Type: "string"},
					{Name: "leagues", Type: "string"},
				},
			},
		},
	}

	profile := Profile(s)
	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}
	assert.Contains(t, syncNames, "scoreboard", "wrapper-object endpoint with 'events' field should be syncable")
}

func TestProfileWrapperObjectDetection_NoFalsePositive(t *testing.T) {
	s := &spec.APISpec{
		Name: "config",
		Resources: map[string]spec.Resource{
			"settings": {
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:   "GET",
						Path:     "/settings",
						Response: spec.ResponseDef{Type: "object", Item: "Settings"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Settings": {
				Fields: []spec.TypeField{
					{Name: "theme", Type: "string"},
					{Name: "language", Type: "string"},
				},
			},
		},
	}

	profile := Profile(s)
	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}
	assert.NotContains(t, syncNames, "settings", "non-wrapper object should not be syncable")
}

func TestProfileSimpleListEndpointSyncable(t *testing.T) {
	// Simulates the trigger-dev pattern: resources with parameterless GET list
	// endpoints that return untyped objects (no wrapper field in types map, no
	// pagination). These should still be syncable.
	s := &spec.APISpec{
		Name: "trigger-dev",
		Resources: map[string]spec.Resource{
			"deployments": {
				Endpoints: map[string]spec.Endpoint{
					"listDeployments": {
						Method:   "GET",
						Path:     "/v3/deployments",
						Response: spec.ResponseDef{Type: "object"},
					},
					"get": {
						Method:   "GET",
						Path:     "/v3/deployments/{deploymentId}",
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"batches": {
				Endpoints: map[string]spec.Endpoint{
					"listBatches": {
						Method:   "GET",
						Path:     "/v3/batches",
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"runs": {
				Endpoints: map[string]spec.Endpoint{
					"listRuns": {
						Method:   "GET",
						Path:     "/v3/runs",
						Response: spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{
							CursorParam: "cursor",
							LimitParam:  "perPage",
						},
					},
				},
			},
			"envvars": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v3/projects/{projectRef}/envvars/{env}",
						Response: spec.ResponseDef{Type: "object"},
					},
				},
			},
			"query": {
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method: "POST",
						Path:   "/v3/query",
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

	// deployments and batches have parameterless GET list endpoints
	assert.Contains(t, syncNames, "deployments", "parameterless GET list endpoint should be syncable")
	assert.Contains(t, syncNames, "batches", "parameterless GET list endpoint should be syncable")
	assert.Equal(t, "/v3/deployments", syncPaths["deployments"])
	assert.Equal(t, "/v3/batches", syncPaths["batches"])

	// runs has pagination so it should also be syncable
	assert.Contains(t, syncNames, "runs")

	// envvars has path params so it should be excluded
	assert.NotContains(t, syncNames, "envvars", "compound-path resource should not be syncable")

	// query is POST-only so it should be excluded
	assert.NotContains(t, syncNames, "query", "POST-only resource should not be syncable")
}

func TestProfileDependentResources(t *testing.T) {
	// A spec with /channels (flat) and /channels/{channelId}/messages (parameterized)
	// should produce a DependentResource linking messages to channels.
	s := &spec.APISpec{
		Name: "messaging",
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/channels",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/channels/{channelId}/messages",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
		},
	}

	profile := Profile(s)

	// channels should be in flat SyncableResources
	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}
	assert.Contains(t, syncNames, "channels")
	assert.NotContains(t, syncNames, "messages", "parameterized path should not be in flat sync")

	// messages should be a dependent resource with channels as parent
	require.Len(t, profile.DependentSyncResources, 1)
	dep := profile.DependentSyncResources[0]
	assert.Equal(t, "messages", dep.Name)
	assert.Equal(t, "channels", dep.ParentResource)
	assert.Equal(t, "channelId", dep.ParentIDParam)
	assert.Equal(t, "/channels/{channelId}/messages", dep.Path)
}

func TestProfileDependentResources_NoParentNoDependent(t *testing.T) {
	// If the parent resource doesn't exist as a flat syncable, no dependent is created.
	s := &spec.APISpec{
		Name: "orphan",
		Resources: map[string]spec.Resource{
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/channels/{channelId}/messages",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Empty(t, profile.DependentSyncResources, "no parent resource means no dependent detection")
}

// TestProfileSyncableResourcePropagatesIDFieldAndCritical asserts that the new
// per-endpoint metadata flows into SyncableResource. The OpenAPI parser is
// responsible for resolving IDField (x-resource-id → id → name → required
// scalar) before the profiler runs; the profiler's job is to pick the right
// endpoint per resource and copy the resolved values through.
func TestProfileSyncableResourcePropagatesIDFieldAndCritical(t *testing.T) {
	s := &spec.APISpec{
		Name: "tickers",
		Resources: map[string]spec.Resource{
			"tickers": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/tickers",
						Response: spec.ResponseDef{Type: "array"},
						IDField:  "ticker",
						Critical: true,
					},
				},
			},
			"events": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/events",
						Response: spec.ResponseDef{Type: "array"},
						IDField:  "id",
						// Critical not set — defaults to false.
					},
				},
			},
		},
	}

	profile := Profile(s)
	byName := make(map[string]SyncableResource, len(profile.SyncableResources))
	for _, r := range profile.SyncableResources {
		byName[r.Name] = r
	}

	require.Contains(t, byName, "tickers")
	assert.Equal(t, "ticker", byName["tickers"].IDField)
	assert.True(t, byName["tickers"].Critical)

	require.Contains(t, byName, "events")
	assert.Equal(t, "id", byName["events"].IDField)
	assert.False(t, byName["events"].Critical)
}

// TestProfileSyncableResourceUnsetMetadata pins the negative case — a spec with
// no IDField/Critical on its endpoints emits a SyncableResource with both
// fields zero-valued. Lets templates fall through to the runtime fallback list.
func TestProfileSyncableResourceUnsetMetadata(t *testing.T) {
	s := &spec.APISpec{
		Name: "widgets",
		Resources: map[string]spec.Resource{
			"widgets": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/widgets",
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.SyncableResources, 1)
	assert.Equal(t, "widgets", profile.SyncableResources[0].Name)
	assert.Empty(t, profile.SyncableResources[0].IDField)
	assert.False(t, profile.SyncableResources[0].Critical)
}

// TestProfileDependentResourcePropagatesIDFieldAndCritical asserts that the
// per-endpoint IDField/Critical metadata also flows into DependentResource for
// parameterized child paths. Without this, x-resource-id and x-critical
// annotations on a child path-item silently get dropped, and the
// override/critical maps in the generated sync code only cover flat resources.
func TestProfileDependentResourcePropagatesIDFieldAndCritical(t *testing.T) {
	s := &spec.APISpec{
		Name: "chat",
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/channels",
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/channels/{channel_id}/messages",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
						IDField:    "msg_id",
						Critical:   true,
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.DependentSyncResources, 1)
	dep := profile.DependentSyncResources[0]
	assert.Equal(t, "messages", dep.Name)
	assert.Equal(t, "channels", dep.ParentResource)
	assert.Equal(t, "channel_id", dep.ParentIDParam)
	assert.Equal(t, "/channels/{channel_id}/messages", dep.Path)
	assert.Equal(t, "msg_id", dep.IDField)
	assert.True(t, dep.Critical)
}

// TestProfileDependentResourceUnsetMetadata pins the negative case — a
// parameterized child path with no IDField/Critical emits a DependentResource
// with both fields zero-valued, leaving the runtime fallback list intact.
func TestProfileDependentResourceUnsetMetadata(t *testing.T) {
	s := &spec.APISpec{
		Name: "chat",
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/channels",
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/channels/{channel_id}/messages",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.DependentSyncResources, 1)
	assert.Empty(t, profile.DependentSyncResources[0].IDField)
	assert.False(t, profile.DependentSyncResources[0].Critical)
}

// TestProfileSyncableResourceShorterPathWinsMetadata asserts that when two
// candidate endpoints can populate the same syncable resource, the shorter-path
// rule that already governs the Path field also picks the IDField/Critical
// values — i.e., the metadata always reflects the endpoint sync will actually
// call.
func TestProfileSyncableResourceShorterPathWinsMetadata(t *testing.T) {
	s := &spec.APISpec{
		Name: "things",
		Resources: map[string]spec.Resource{
			"things": {
				Endpoints: map[string]spec.Endpoint{
					"longList": {
						Method:   "GET",
						Path:     "/v1/things/all",
						Response: spec.ResponseDef{Type: "array"},
						IDField:  "loser",
						Critical: false,
					},
					"shortList": {
						Method:   "GET",
						Path:     "/v1/things",
						Response: spec.ResponseDef{Type: "array"},
						IDField:  "winner",
						Critical: true,
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.SyncableResources, 1)
	assert.Equal(t, "/v1/things", profile.SyncableResources[0].Path)
	assert.Equal(t, "winner", profile.SyncableResources[0].IDField)
	assert.True(t, profile.SyncableResources[0].Critical)
}
