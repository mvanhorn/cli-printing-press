package profiler

import (
	"bytes"
	"os"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever
// fn wrote to stderr. The swap is single-threaded — safe for go test's
// per-package sequential execution; do not use across parallel subtests
// that both touch stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })
	fn()
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

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

func TestProfileSiblingListEndpoints(t *testing.T) {
	s := &spec.APISpec{
		Name: "trading",
		Resources: map[string]spec.Resource{
			"portfolio": {
				Endpoints: map[string]spec.Endpoint{
					"fills": {
						Method:     "GET",
						Path:       "/portfolio/fills",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
					"orders": {
						Method:     "GET",
						Path:       "/portfolio/orders",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
					"settlements": {
						Method:     "GET",
						Path:       "/portfolio/settlements",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
		},
	}

	profile := Profile(s)

	syncPaths := make(map[string]string)
	for _, resource := range profile.SyncableResources {
		syncPaths[resource.Name] = resource.Path
	}

	assert.Equal(t, "/portfolio/fills", syncPaths["portfolio"])
	assert.Equal(t, "/portfolio/orders", syncPaths["portfolio-orders"])
	assert.Equal(t, "/portfolio/settlements", syncPaths["portfolio-settlements"])
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

func TestProfileRPCStylePostListEndpointSyncable(t *testing.T) {
	s := &spec.APISpec{
		Name: "rpc-post-api",
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"getList": {
						Method: "POST",
						Path:   "/api/items/getList",
						Body: []spec.Param{
							{Name: "limit", Type: "integer"},
							{Name: "cursor", Type: "string"},
							{Name: "view", Type: "string", Default: "summary"},
						},
						Pagination: &spec.Pagination{
							CursorParam:    "cursor",
							LimitParam:     "limit",
							NextCursorPath: "next_cursor",
						},
						Response: spec.ResponseDef{Type: "object", Item: "ItemsResponse"},
					},
					"create": {
						Method:   "POST",
						Path:     "/api/items/create",
						Response: spec.ResponseDef{Type: "object", Item: "Item"},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"send": {
						Method:   "POST",
						Path:     "/api/messages/send",
						Response: spec.ResponseDef{Type: "object", Item: "SendMessageResponse"},
					},
				},
			},
			"widgets": {
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:   "POST",
						Path:     "/api/widgets/create",
						Response: spec.ResponseDef{Type: "object", Item: "Widget"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"ItemsResponse": {
				Fields: []spec.TypeField{
					{Name: "items", Type: "array"},
					{Name: "next_cursor", Type: "string"},
				},
			},
			"Item": {
				Fields: []spec.TypeField{{Name: "id", Type: "string"}},
			},
			"SendMessageResponse": {
				Fields: []spec.TypeField{
					{Name: "ok", Type: "boolean"},
					{Name: "ts", Type: "string"},
				},
			},
			"Widget": {
				Fields: []spec.TypeField{{Name: "id", Type: "string"}},
			},
		},
	}

	profile := Profile(s)

	syncNames := make([]string, len(profile.SyncableResources))
	syncPaths := make(map[string]string)
	syncByName := make(map[string]SyncableResource)
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
		syncPaths[sr.Name] = sr.Path
		syncByName[sr.Name] = sr
	}

	assert.Contains(t, syncNames, "items", "paginated RPC-style POST list endpoint should be syncable")
	assert.Equal(t, "/api/items/getList", syncPaths["items"])
	assert.Equal(t, "POST", syncByName["items"].Method)
	require.Len(t, syncByName["items"].BodyFields, 3)
	assert.Equal(t, "limit", syncByName["items"].BodyFields[0].Name)
	assert.Equal(t, "integer", syncByName["items"].BodyFields[0].Type)
	assert.False(t, syncByName["items"].BodyFields[0].HasDefault)
	assert.Equal(t, "cursor", syncByName["items"].BodyFields[1].Name)
	assert.Equal(t, "string", syncByName["items"].BodyFields[1].Type)
	assert.Equal(t, "view", syncByName["items"].BodyFields[2].Name)
	assert.Equal(t, "string", syncByName["items"].BodyFields[2].Type)
	assert.True(t, syncByName["items"].BodyFields[2].HasDefault)
	assert.Equal(t, "summary", syncByName["items"].BodyFields[2].Default)
	assert.NotContains(t, syncNames, "messages", "POST write endpoints without pagination and wrapper arrays must not be syncable")
	assert.NotContains(t, syncNames, "widgets", "POST create endpoints without pagination must not be syncable")
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

func TestProfileSyncableResourceSupportsCursorOnlyPagination(t *testing.T) {
	s := &spec.APISpec{
		Name: "cursor-only",
		Resources: map[string]spec.Resource{
			"events": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/events",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "starting_after"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.SyncableResources, 1)
	assert.Equal(t, "events", profile.SyncableResources[0].Name)
	assert.True(t, profile.SyncableResources[0].SupportsPagination, "cursor-only pagination must keep sync from stopping after the first page")
}

// TestProfileDependentResources_SharedSubResourceShardsByParent pins the
// fix for issue #694. When the same sub-resource leaf name (e.g. "commits")
// appears under multiple parents (e.g. /gists/{gist_id}/commits and
// /repos/{owner}/{repo}/commits), each parent must produce its own
// DependentResource with a sharded Name so sync writes to the correct
// per-parent table instead of the first-seen parent silently winning.
func TestProfileDependentResources_SharedSubResourceShardsByParent(t *testing.T) {
	s := &spec.APISpec{
		Name: "github",
		Resources: map[string]spec.Resource{
			"gists": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/gists",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
					},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/gists/{gist_id}/commits",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
							},
						},
					},
				},
			},
			"repos": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/repos",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
					},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/repos/{repo_id}/commits",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
							},
						},
					},
				},
			},
		},
	}

	profile := Profile(s)

	depsByName := make(map[string]DependentResource)
	for _, dep := range profile.DependentSyncResources {
		depsByName[dep.Name] = dep
	}

	require.Contains(t, depsByName, "gists_commits", "gists.commits should shard to gists_commits")
	require.Contains(t, depsByName, "repos_commits", "repos.commits should shard to repos_commits")
	assert.NotContains(t, depsByName, "commits", "bare commits would silently merge two parents")

	gistsDep := depsByName["gists_commits"]
	assert.Equal(t, "gists", gistsDep.ParentResource)
	assert.Equal(t, "/gists/{gist_id}/commits", gistsDep.Path)

	reposDep := depsByName["repos_commits"]
	assert.Equal(t, "repos", reposDep.ParentResource)
	assert.Equal(t, "/repos/{repo_id}/commits", reposDep.Path)
}

// TestProfileDependentResources_MultiParamParentPath confirms the walk-context
// parent (from the SubResources tree) wins over the path-param heuristic when
// the path has multiple params and the first one does not match a syncable
// resource. This is the GitHub /repos/{owner}/{repo}/commits shape that the
// path-param-only heuristic mishandles by deriving "owner" as the parent.
func TestProfileDependentResources_MultiParamParentPath(t *testing.T) {
	s := &spec.APISpec{
		Name: "github",
		Resources: map[string]spec.Resource{
			"gists": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/gists",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
					},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/gists/{gist_id}/commits",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
							},
						},
					},
				},
			},
			"repos": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/repos",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
					},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/repos/{owner}/{repo}/commits",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "per_page"},
							},
						},
					},
				},
			},
		},
	}

	profile := Profile(s)

	depsByName := make(map[string]DependentResource)
	for _, dep := range profile.DependentSyncResources {
		depsByName[dep.Name] = dep
	}

	require.Contains(t, depsByName, "repos_commits", "walk-context parent wins over /repos/{owner}/...'s leading param")
	assert.Equal(t, "repos", depsByName["repos_commits"].ParentResource)
}

// TestProfileDependentResources_TopLevelCollisionShards mirrors the schema
// builder's top-level/sub-resource collision case. When the leaf name matches
// a top-level resource, the dependent resource emits a sharded Name so it
// lines up with the sharded sub-resource table.
func TestProfileDependentResources_TopLevelCollisionShards(t *testing.T) {
	s := &spec.APISpec{
		Name: "stytch",
		Resources: map[string]spec.Resource{
			"connected_apps": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/connected_apps/clients",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
			},
			"users": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/users",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
				SubResources: map[string]spec.Resource{
					"connected_apps": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/users/{user_id}/connected_apps",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
							},
						},
					},
				},
			},
		},
	}

	profile := Profile(s)

	depsByName := make(map[string]DependentResource)
	for _, dep := range profile.DependentSyncResources {
		depsByName[dep.Name] = dep
	}

	require.Contains(t, depsByName, "users_connected_apps", "sub-resource shards when leaf collides with a top-level resource")
	assert.NotContains(t, depsByName, "connected_apps")
}

// TestProfileDependentResources_CamelCaseShardSnakeCased pins the snake-case
// step in the shared shard helper. A profiler-emitted DependentResource.Name
// must match the schema builder's table name byte-for-byte even when the
// parent map key is camelCase.
func TestProfileDependentResources_CamelCaseShardSnakeCased(t *testing.T) {
	s := &spec.APISpec{
		Name: "discovery",
		Resources: map[string]spec.Resource{
			"userData": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/userData",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
				SubResources: map[string]spec.Resource{
					"loginEvents": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/userData/{user_id}/loginEvents",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
							},
						},
					},
				},
			},
			"adminData": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/adminData",
						Response:   spec.ResponseDef{Type: "array"},
						Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					},
				},
				SubResources: map[string]spec.Resource{
					"loginEvents": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method:     "GET",
								Path:       "/adminData/{admin_id}/loginEvents",
								Response:   spec.ResponseDef{Type: "array"},
								Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
							},
						},
					},
				},
			},
		},
	}

	profile := Profile(s)

	names := make(map[string]bool)
	for _, dep := range profile.DependentSyncResources {
		names[dep.Name] = true
	}
	assert.True(t, names["user_data_login_events"], "DependentResource.Name snake-cases through the shard helper")
	assert.True(t, names["admin_data_login_events"])
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

// TestProfileSyncableResourceSinceParamPropagation asserts that per-endpoint
// since-like query parameter declarations flow into SyncableResource.SinceParam.
// The sync template uses that field to skip incremental-cursor emission for
// resources whose endpoint does not declare such a parameter, avoiding the
// Notion-style 400 the blind-append behavior used to produce.
func TestProfileSyncableResourceSinceParamPropagation(t *testing.T) {
	s := &spec.APISpec{
		Name: "mixed",
		Resources: map[string]spec.Resource{
			"events": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v1/events",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "since", Type: "string"},
						},
					},
				},
			},
			"audit": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v1/audit",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "updated_after", Type: "string"},
						},
					},
				},
			},
			"posts": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v1/posts",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "modified_since", Type: "string"},
						},
					},
				},
			},
			"changelog": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v1/changelog",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "updated_at", Type: "string"},
						},
					},
				},
			},
			"users": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/v1/users",
						Response: spec.ResponseDef{Type: "array"},
						Params:   []spec.Param{},
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

	require.Contains(t, byName, "events")
	assert.Equal(t, "since", byName["events"].SinceParam, "literal since param should propagate verbatim")

	require.Contains(t, byName, "audit")
	assert.Equal(t, "updated_after", byName["audit"].SinceParam, "spec-declared name (not the profile-wide guess) wins")

	require.Contains(t, byName, "posts")
	assert.Equal(t, "modified_since", byName["posts"].SinceParam, "modified_since heuristic branch")

	require.Contains(t, byName, "changelog")
	assert.Equal(t, "updated_at", byName["changelog"].SinceParam, "updated_at heuristic branch")

	require.Contains(t, byName, "users")
	assert.Empty(t, byName["users"].SinceParam, "endpoints without a since-like param yield empty SinceParam — the sync template treats this as 'do not send'")
}

// TestProfileDependentResourceSinceParamPropagation mirrors
// TestProfileSyncableResourceSinceParamPropagation for parameterized child
// paths so dependent-resource sync gets the same per-endpoint gating as flat
// resources.
func TestProfileDependentResourceSinceParamPropagation(t *testing.T) {
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
						Params: []spec.Param{
							{Name: "channel_id", Type: "string", PathParam: true},
							{Name: "modified_since", Type: "string"},
						},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.DependentSyncResources, 1)
	assert.Equal(t, "modified_since", profile.DependentSyncResources[0].SinceParam)
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

// TestProfileSpecWalker_AugmentsAutoDetected verifies that a spec-declared
// walker on an already-auto-detected dependent endpoint overrides
// ParentResource, ParentIDParam, and KeyField in place rather than creating
// a duplicate entry. /orders/{account_id} would auto-detect "account_id" →
// "accounts" (after _id stripping) — the walker redirects to "customers"
// and pins a non-PK key.
func TestProfileSpecWalker_AugmentsAutoDetected(t *testing.T) {
	s := &spec.APISpec{
		Name: "shop",
		Resources: map[string]spec.Resource{
			"accounts": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/accounts", Response: spec.ResponseDef{Type: "array"}},
				},
			},
			"customers": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/customers", Response: spec.ResponseDef{Type: "array"}, IDField: "customer_key"},
				},
			},
			"orders": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/accounts/{account_id}/orders",
						Response: spec.ResponseDef{Type: "array"},
						Walker: &spec.WalkerConfig{
							Parent:   "customers",
							KeyField: "customer_key",
							KeyParam: "account_id",
						},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.DependentSyncResources, 1, "augment must not duplicate the entry")
	dep := profile.DependentSyncResources[0]
	assert.Equal(t, "customers", dep.ParentResource, "walker must redirect parent away from auto-detect")
	assert.Equal(t, "account_id", dep.ParentIDParam)
	assert.Equal(t, "customer_key", dep.KeyField)
	assert.Equal(t, "/accounts/{account_id}/orders", dep.Path)
}

// TestProfileSpecWalker_SynthesizesMissingDependent verifies that a spec-
// declared walker creates a new DependentResource entry when auto-detection
// would not have linked the endpoint, and that the synthesized Name comes
// from the containing resource (matching detectDependentResources's naming
// convention) rather than the endpoint map key.
func TestProfileSpecWalker_SynthesizesMissingDependent(t *testing.T) {
	s := &spec.APISpec{
		Name: "fantasy",
		Resources: map[string]spec.Resource{
			"games": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/games", Response: spec.ResponseDef{Type: "array"}, IDField: "game_key"},
				},
			},
			"leagues": {
				Endpoints: map[string]spec.Endpoint{
					"fetch_for_game": {
						Method:   "GET",
						Path:     "/games/{game_key}/leagues",
						Response: spec.ResponseDef{Type: "array"},
						Walker: &spec.WalkerConfig{
							Parent:   "games",
							KeyField: "game_key",
						},
					},
				},
			},
		},
	}

	profile := Profile(s)
	// Exactly one dependent for the leagues endpoint, named from the
	// containing resource ("leagues"), not the endpoint key
	// ("fetch_for_game" → "fetch_for_game" via ToSnakeCase).
	require.Len(t, profile.DependentSyncResources, 1)
	dep := profile.DependentSyncResources[0]
	assert.Equal(t, "leagues", dep.Name, "Name must come from resource, not endpoint key")
	assert.Equal(t, "games", dep.ParentResource)
	assert.Equal(t, "game_key", dep.KeyField)
	assert.Equal(t, "game_key", dep.ParentIDParam, "single-placeholder path: KeyParam defaults to firstPathParam")
	assert.Equal(t, "/games/{game_key}/leagues", dep.Path)
}

// TestProfileSpecWalker_SynthesizePropagatesSinceParam verifies that a
// walker-synthesized DependentResource carries through endpoint-level
// SinceParam — incremental sync stays available for walker-declared
// hierarchical children, matching the auto-detect path's behavior.
// Greptile flagged a P1 regression on the initial draft where the
// synthesize branch dropped SinceParam (and Discriminator); this test
// pins the fix.
func TestProfileSpecWalker_SynthesizePropagatesSinceParam(t *testing.T) {
	s := &spec.APISpec{
		Name: "fantasy",
		Resources: map[string]spec.Resource{
			"games": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/games", Response: spec.ResponseDef{Type: "array"}, IDField: "game_key"},
				},
			},
			"leagues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/games/{game_key}/leagues",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "game_key", PathParam: true},
							{Name: "since"},
						},
						Walker: &spec.WalkerConfig{
							Parent:   "games",
							KeyField: "game_key",
						},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.DependentSyncResources, 1)
	dep := profile.DependentSyncResources[0]
	assert.Equal(t, "leagues", dep.Name)
	assert.Equal(t, "since", dep.SinceParam,
		"synthesize branch must propagate SinceParam via metaFromEndpoint — incremental sync depends on it")
}

// TestProfileSpecWalker_NonSyncableParentWarns verifies that a walker
// pointing at a non-syncable parent emits a stderr warning and is dropped.
// Explicit walker:: declarations carry author intent; silently dropping a
// typo'd parent would produce passing builds with missing data.
func TestProfileSpecWalker_NonSyncableParentWarns(t *testing.T) {
	s := &spec.APISpec{
		Name: "fantasy",
		Resources: map[string]spec.Resource{
			// "sports" is not syncable (GET-by-id only, no list).
			"sports": {
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/sports/{sport_id}", Response: spec.ResponseDef{Type: "object"}},
				},
			},
			"leagues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/leagues",
						Response: spec.ResponseDef{Type: "array"},
						Walker: &spec.WalkerConfig{
							Parent:   "sports",
							KeyField: "sport_key",
						},
					},
				},
			},
		},
	}

	var profile *APIProfile
	stderr := captureStderr(t, func() {
		profile = Profile(s)
	})

	assert.Contains(t, stderr, "warning: walker on leagues.list")
	assert.Contains(t, stderr, `parent "sports" is not a syncable resource`)
	for _, dep := range profile.DependentSyncResources {
		assert.NotEqual(t, "leagues", dep.Name,
			"walker with non-syncable parent must be dropped, not produce a DependentResource")
	}
}

// TestProfileSpecWalker_MultiPlaceholderPathWarns verifies that a walker on
// a path with 2+ {...} placeholders requires an explicit key_param. Without
// it, firstPathParam's "first wins" default would silently pick the parent
// slot on a 2-deep path — almost always the wrong slot for the child.
// With explicit key_param, the walker is accepted.
func TestProfileSpecWalker_MultiPlaceholderPathWarns(t *testing.T) {
	t.Run("ambiguous: warn and drop", func(t *testing.T) {
		s := &spec.APISpec{
			Name: "fantasy",
			Resources: map[string]spec.Resource{
				"games": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/games", Response: spec.ResponseDef{Type: "array"}, IDField: "game_key"},
					},
				},
				"rosters": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:   "GET",
							Path:     "/games/{game_key}/leagues/{league_id}/roster",
							Response: spec.ResponseDef{Type: "array"},
							Walker: &spec.WalkerConfig{
								Parent: "games",
								// no key_param — ambiguous on 2-placeholder path
							},
						},
					},
				},
			},
		}
		var profile *APIProfile
		stderr := captureStderr(t, func() {
			profile = Profile(s)
		})
		assert.Contains(t, stderr, "warning: walker on rosters.list")
		assert.Contains(t, stderr, "2 placeholders")
		assert.Contains(t, stderr, "declare key_param explicitly")
		for _, dep := range profile.DependentSyncResources {
			assert.NotEqual(t, "rosters", dep.Name, "ambiguous walker must be dropped")
		}
	})

	t.Run("explicit key_param: accepted", func(t *testing.T) {
		s := &spec.APISpec{
			Name: "fantasy",
			Resources: map[string]spec.Resource{
				"games": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/games", Response: spec.ResponseDef{Type: "array"}, IDField: "game_key"},
					},
				},
				"rosters": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:   "GET",
							Path:     "/games/{game_key}/leagues/{league_id}/roster",
							Response: spec.ResponseDef{Type: "array"},
							Walker: &spec.WalkerConfig{
								Parent:   "games",
								KeyField: "game_key",
								KeyParam: "league_id",
							},
						},
					},
				},
			},
		}
		profile := Profile(s)
		var found bool
		for _, dep := range profile.DependentSyncResources {
			if dep.Name == "rosters" {
				found = true
				assert.Equal(t, "league_id", dep.ParentIDParam, "explicit key_param must be used verbatim")
				assert.Equal(t, "game_key", dep.KeyField)
			}
		}
		assert.True(t, found, "walker with explicit key_param must produce a dependent entry")
	})
}

// Specs that declare pagination via plain offset+count query params (no
// explicit pagination: block) must infer the cursor and limit names from
// those params instead of falling back to "after"/"limit".
func TestProfilePagination_InfersFromPlainParamsWhenNoExplicitBlock(t *testing.T) {
	s := &spec.APISpec{
		Name: "plain-param-pagination",
		Resources: map[string]spec.Resource{
			"agents": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/agents",
						Params:   []spec.Param{{Name: "offset", Type: "int"}, {Name: "count", Type: "int"}},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
			"builds": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/builds",
						Params:   []spec.Param{{Name: "offset", Type: "int"}, {Name: "count", Type: "int"}},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Equal(t, "offset", profile.Pagination.CursorParam, "plain offset param must be picked up")
	assert.Equal(t, "count", profile.Pagination.PageSizeParam, "plain count param must be picked up as limit")
}

// Explicit pagination: blocks must continue to win over plain-param inference.
// Mixing the two on the same endpoint would otherwise double-count or let
// inference shadow the author's deliberate choice.
func TestProfilePagination_ExplicitBlockWinsOverInference(t *testing.T) {
	s := &spec.APISpec{
		Name: "explicit",
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/items",
						Params: []spec.Param{
							{Name: "offset", Type: "int"},
							{Name: "count", Type: "int"},
						},
						Pagination: &spec.Pagination{
							Type:        "cursor",
							CursorParam: "foo",
							LimitParam:  "bar",
						},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Equal(t, "foo", profile.Pagination.CursorParam, "explicit cursor_param must win")
	assert.Equal(t, "bar", profile.Pagination.PageSizeParam, "explicit limit_param must win")
}

// Specs with no recognizable pagination shape must keep the historical
// after/limit defaults so existing golden output doesn't churn.
func TestProfilePagination_NoPaginationParamsKeepsDefaults(t *testing.T) {
	s := &spec.APISpec{
		Name: "no-pagination",
		Resources: map[string]spec.Resource{
			"things": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/things",
						Params:   []spec.Param{{Name: "filter", Type: "string"}},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Equal(t, "after", profile.Pagination.CursorParam)
	assert.Equal(t, "limit", profile.Pagination.PageSizeParam)
}

// Inference must skip path params and positional args even when their names
// match candidate sets (e.g. an /items/{page} path segment named "page").
func TestProfilePagination_SkipsPathAndPositionalParams(t *testing.T) {
	s := &spec.APISpec{
		Name: "scoped",
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/items/{page}",
						Params: []spec.Param{
							{Name: "page", Type: "string", PathParam: true},
							{Name: "offset", Type: "int", Positional: true},
						},
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	assert.Equal(t, "after", profile.Pagination.CursorParam, "path-param 'page' must not be treated as a cursor")
	assert.Equal(t, "limit", profile.Pagination.PageSizeParam)
}

// TestProfileTemplateVarPathBecomesFlatSyncable: paths whose only
// {placeholder} is an EndpointTemplateVar (e.g. /tenant/{tenant}/<resource>
// when the spec declares x-tenant-env-var) are runtime-resolvable through
// buildURL — they should become flat SyncableResources rather than landing
// in DependentSyncResources (which would require iterating a non-existent
// parent table).
func TestProfileTemplateVarPathBecomesFlatSyncable(t *testing.T) {
	s := &spec.APISpec{
		Name:                 "servicetitan",
		EndpointTemplateVars: []string{"tenant"},
		EndpointTemplateEnvOverrides: map[string]string{
			"tenant": "ST_TENANT_ID",
		},
		Resources: map[string]spec.Resource{
			"customers": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/tenant/{tenant}/customers",
						Pagination: &spec.Pagination{CursorParam: "pageToken", LimitParam: "pageSize"},
						Response:   spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	require.Len(t, profile.SyncableResources, 1, "tenant-scoped resource must surface as a flat SyncableResource")
	assert.Equal(t, "customers", profile.SyncableResources[0].Name)
	assert.Equal(t, "/tenant/{tenant}/customers", profile.SyncableResources[0].Path,
		"path must preserve the {tenant} placeholder for buildURL to substitute")
	assert.Empty(t, profile.DependentSyncResources, "tenant placeholder is not a parent context — must not become a DependentResource")
}

// TestProfileMixedPlaceholdersNotPromoted guards against over-eager
// promotion: paths mixing a template-var placeholder with a real parent-
// context placeholder must NOT be promoted to flat sync. The dependent-
// resource matching is governed elsewhere; here we only pin the negative
// (no false promotion) since that's what regression on this change would
// look like.
func TestProfileMixedPlaceholdersNotPromoted(t *testing.T) {
	s := &spec.APISpec{
		Name:                 "servicetitan",
		EndpointTemplateVars: []string{"tenant"},
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/tenant/{tenant}/channels",
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
						Response:   spec.ResponseDef{Type: "array"},
					},
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:     "GET",
						Path:       "/tenant/{tenant}/channels/{channel_id}/messages",
						Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
						Response:   spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	profile := Profile(s)
	flatNames := make([]string, 0, len(profile.SyncableResources))
	for _, r := range profile.SyncableResources {
		flatNames = append(flatNames, r.Name)
	}
	assert.Contains(t, flatNames, "channels", "tenant-only path is flat")
	assert.NotContains(t, flatNames, "messages",
		"a path containing {channel_id} alongside the template var must not flatten into SyncableResources")
}
