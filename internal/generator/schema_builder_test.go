package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"camelCase", "camel_case"},
		{"kebab-case", "kebab_case"},
		{"snake_case", "snake_case"},
		{"PascalCase", "pascal_case"},
		{"movie_id", "movie_id"},
		// Dot-notation params (TMDb, Elasticsearch style)
		{"primary_release_date.gte", "primary_release_date_gte"},
		{"vote_average.gte", "vote_average_gte"},
		{"vote_average.lte", "vote_average_lte"},
		{"vote_count.gte", "vote_count_gte"},
		{"field.nested.deep", "field_nested_deep"},
		// Combined dots and hyphens
		{"with.dots-and-hyphens", "with_dots_and_hyphens"},
		// No transformation needed
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toSnakeCase(tt.input))
		})
	}
}

func TestSafeSQLNameQuotesUnsafeIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare identifier", in: "messages", want: "messages"},
		{name: "reserved word", in: "references", want: `"references"`},
		{name: "starts with digit", in: "0", want: `"0"`},
		{name: "derived starts with digit", in: "0_fts", want: `"0_fts"`},
		{name: "contains punctuation", in: "foo/bar", want: `"foo/bar"`},
		{name: "escapes quote", in: `foo"bar`, want: `"foo""bar"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safeSQLName(tt.in))
		})
	}
}

func TestCollectTextFieldNames(t *testing.T) {
	// Fields like tag/label/category/metadata should be picked up for FTS5
	// alongside the core text fields. Motivated by the ESPN retro where
	// "notes" (event tags) were unsearchable until manually added.
	mkFields := func(names ...string) []spec.TypeField {
		fields := make([]spec.TypeField, 0, len(names))
		for _, n := range names {
			fields = append(fields, spec.TypeField{Name: n, Type: "string"})
		}
		return fields
	}

	tests := []struct {
		name     string
		fields   []string
		wantIncl []string
		wantExcl []string
	}{
		{
			name:     "picks up core text fields",
			fields:   []string{"title", "description", "body"},
			wantIncl: []string{"title", "description", "body"},
		},
		{
			name:     "picks up tag-family fields",
			fields:   []string{"name", "tag", "tags", "label", "labels"},
			wantIncl: []string{"name", "tag", "tags", "label", "labels"},
		},
		{
			name:     "picks up category and metadata fields",
			fields:   []string{"title", "category", "categories", "metadata"},
			wantIncl: []string{"title", "category", "categories", "metadata"},
		},
		{
			name:     "picks up notes and note",
			fields:   []string{"note", "notes"},
			wantIncl: []string{"note", "notes"},
		},
		{
			name:     "ignores non-text fields",
			fields:   []string{"id", "created_at", "price"},
			wantExcl: []string{"id", "created_at", "price"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectTextFieldNamesFromFields(mkFields(tt.fields...))
			for _, want := range tt.wantIncl {
				assert.Contains(t, got, want)
			}
			for _, exc := range tt.wantExcl {
				assert.NotContains(t, got, exc)
			}
		})
	}
}

// TestBuildSchema_ColumnsFromResponseSchema pins that domain-table columns
// come from the GET endpoint's response schema (looked up via
// APISpec.Types[endpoint.Response.Item]) and never from request-side query
// or path parameters. Without this pin, a regression where columns mirror
// filter/sort/pagination params silently breaks every SQL-backed novel
// command, since sync can't populate columns the response doesn't contain.
func TestBuildSchema_ColumnsFromResponseSchema(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"issues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Path:   "/issues",
						Params: []spec.Param{
							{Name: "filter", Type: "string"},
							{Name: "labels", Type: "string"},
							{Name: "sort", Type: "string"},
							{Name: "since", Type: "string", Format: "date-time"},
							{Name: "per_page", Type: "integer"},
							{Name: "page", Type: "integer"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Issue"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Issue": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "integer"},
					{Name: "number", Type: "integer"},
					{Name: "title", Type: "string"},
					{Name: "body", Type: "string"},
					{Name: "state", Type: "string"},
					{Name: "created_at", Type: "string", Format: "date-time"},
					{Name: "updated_at", Type: "string", Format: "date-time"},
				},
			},
		},
	}

	issues := findTable(BuildSchema(s), "issues")
	if !assert.NotNil(t, issues, "issues table should be emitted") {
		return
	}

	cols := map[string]string{}
	for _, c := range issues.Columns {
		cols[c.Name] = c.Type
	}

	for _, want := range []string{"number", "title", "body", "state", "created_at", "updated_at"} {
		assert.Contains(t, cols, want, "expected column %q from response schema", want)
	}
	for _, leak := range []string{"filter", "labels", "sort", "since", "per_page", "page"} {
		assert.NotContains(t, cols, leak, "request param %q must not appear as a column", leak)
	}
	assert.Equal(t, "DATETIME", cols["created_at"])
	assert.Equal(t, "DATETIME", cols["updated_at"])
}

// TestBuildSchema_ParamResponseNameOverlap asserts that when a request param
// and a response field share a name (common: "state"), the resulting column
// reflects the response field's *type* — not the param's — because the param
// is discarded entirely from column derivation. The fixture deliberately
// gives the param a different type from the response field so a regression
// where the param's type leaked into column emission would be caught.
func TestBuildSchema_ParamResponseNameOverlap(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"issues": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method: "GET",
						Params: []spec.Param{
							// Request-side filter knob, declared as string.
							{Name: "state", Type: "string"},
						},
						Response: spec.ResponseDef{Type: "array", Item: "Issue"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Issue": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "integer"},
					{Name: "title", Type: "string"},
					// Response-side: state is an integer here, distinct from
					// the request-param string. Only the response type should
					// drive the emitted column.
					{Name: "state", Type: "integer"},
				},
			},
		},
	}

	issues := findTable(BuildSchema(s), "issues")
	if !assert.NotNil(t, issues) {
		return
	}

	stateCols := []ColumnDef{}
	for _, c := range issues.Columns {
		if c.Name == "state" {
			stateCols = append(stateCols, c)
		}
	}
	assert.Len(t, stateCols, 1, "exactly one state column should exist")
	if len(stateCols) == 1 {
		assert.Equal(t, "INTEGER", stateCols[0].Type,
			"state column type must come from the response field (integer), not the request param (string)")
	}
}

// TestBuildSchema_NoResponseTypeFallback asserts that when the GET endpoint's
// response item cannot be resolved against APISpec.Types — either because
// Response.Item names a type that isn't registered, or because Response.Item
// is empty (spec author left the response declaration off entirely) — the
// table degrades to id/data/synced_at. Hallucinating columns from request
// params would re-introduce the bug class the response-sourcing fix targets.
func TestBuildSchema_NoResponseTypeFallback(t *testing.T) {
	cases := []struct {
		name     string
		response spec.ResponseDef
		types    map[string]spec.TypeDef
	}{
		{
			name:     "Response.Item names an unregistered type",
			response: spec.ResponseDef{Type: "array", Item: "UnknownItem"},
			types:    map[string]spec.TypeDef{},
		},
		{
			name:     "Response.Item is empty (no response declared)",
			response: spec.ResponseDef{},
			types:    map[string]spec.TypeDef{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &spec.APISpec{
				Resources: map[string]spec.Resource{
					"issues": {
						Endpoints: map[string]spec.Endpoint{
							"list": {
								Method: "GET",
								Params: []spec.Param{
									{Name: "filter", Type: "string"},
									{Name: "page", Type: "integer"},
								},
								Response: tc.response,
							},
						},
					},
				},
				Types: tc.types,
			}

			issues := findTable(BuildSchema(s), "issues")
			if !assert.NotNil(t, issues) {
				return
			}

			names := make([]string, 0, len(issues.Columns))
			for _, c := range issues.Columns {
				names = append(names, c.Name)
			}
			assert.ElementsMatch(t, []string{"id", "data", "synced_at"}, names,
				"unresolved response type must yield only the base columns; got %v", names)
		})
	}
}

// TestBuildSchema_SubResourceCollisionShardsByParent pins the fix for issue
// #694. When the same sub-resource leaf name appears under multiple parents
// (e.g. /repos/{owner}/{repo}/commits and /gists/{gist_id}/commits in the
// GitHub spec), each shard becomes its own table named "<parent>_<sub>" so
// data from one parent does not silently overwrite the other.
func TestBuildSchema_SubResourceCollisionShardsByParent(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"gists": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/gists"},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/gists/{gist_id}/commits"},
						},
					},
				},
			},
			"repos": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/repos"},
				},
				SubResources: map[string]spec.Resource{
					"commits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/repos/{owner}/{repo}/commits"},
						},
					},
				},
			},
		},
	}

	tables := BuildSchema(s)

	names := make([]string, 0, len(tables))
	byName := make(map[string]TableDef, len(tables))
	for _, t := range tables {
		names = append(names, t.Name)
		byName[t.Name] = t
	}

	assert.Contains(t, names, "gists_commits", "collision should produce a sharded gists_commits table")
	assert.Contains(t, names, "repos_commits", "collision should produce a sharded repos_commits table")
	assert.NotContains(t, names, "commits", "bare commits table is silent data loss when both parents collide")

	gistsCommits := byName["gists_commits"]
	require.Greater(t, len(gistsCommits.Columns), 1, "sharded table must have FK column")
	assert.Equal(t, "gists_id", gistsCommits.Columns[1].Name, "gists_commits FK column is gists_id")

	reposCommits := byName["repos_commits"]
	require.Greater(t, len(reposCommits.Columns), 1, "sharded table must have FK column")
	assert.Equal(t, "repos_id", reposCommits.Columns[1].Name, "repos_commits FK column is repos_id")
}

// TestBuildSchema_SubResourceCollidesWithTopLevel verifies the second
// sharding trigger: when a sub-resource leaf collides with a top-level
// resource of the same name (e.g. Stytch's top-level connected_apps and
// users.connected_apps sub-resource), the sub-resource shards while the
// top-level keeps its bare name.
func TestBuildSchema_SubResourceCollidesWithTopLevel(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"connected_apps": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/connected_apps/clients"},
				},
			},
			"users": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users"},
				},
				SubResources: map[string]spec.Resource{
					"connected_apps": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/users/{user_id}/connected_apps"},
						},
					},
				},
			},
		},
	}

	tables := BuildSchema(s)
	names := make(map[string]TableDef, len(tables))
	for _, t := range tables {
		names[t.Name] = t
	}

	require.Contains(t, names, "connected_apps", "top-level keeps its bare name")
	require.Contains(t, names, "users_connected_apps", "sub-resource shards on top-level collision")
	assert.NotContains(t, names, "connected_apps_users")

	// Top-level table has no FK column; sharded sub-resource's FK column
	// points to its parent.
	topLevel := names["connected_apps"]
	for _, col := range topLevel.Columns {
		assert.NotEqual(t, "users_id", col.Name, "top-level connected_apps must not carry a users_id FK")
	}
	usersConnected := names["users_connected_apps"]
	require.Greater(t, len(usersConnected.Columns), 1)
	assert.Equal(t, "users_id", usersConnected.Columns[1].Name)
}

// TestBuildSchema_TopLevelOnlySharedNameNotSyncable pins the predicate
// alignment between the schema builder and the profiler. A top-level
// resource without a flat list endpoint (e.g. POST-only) still triggers
// sharding for any sub-resource that shares its name, otherwise the
// generator emits two unrelated tables that the runtime conflates.
func TestBuildSchema_TopLevelOnlySharedNameNotSyncable(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			// POST-only top-level — not in syncable, but still a top-level
			// resource that the schema builder emits a table for.
			"audits": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/audits"},
				},
			},
			"users": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/users"},
				},
				SubResources: map[string]spec.Resource{
					"audits": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/users/{user_id}/audits"},
						},
					},
				},
			},
		},
	}

	tables := BuildSchema(s)
	names := make([]string, 0, len(tables))
	for _, t := range tables {
		names = append(names, t.Name)
	}
	assert.Contains(t, names, "audits", "top-level audits keeps its bare table")
	assert.Contains(t, names, "users_audits", "sub-resource shards even when top-level is POST-only")
}

// TestBuildSchema_ThreeWayCollision verifies the multi-parent shard predicate
// emits a per-parent table for every parent, not only the first two.
func TestBuildSchema_ThreeWayCollision(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints:    map[string]spec.Endpoint{"list": {Method: "GET", Path: "/channels"}},
				SubResources: map[string]spec.Resource{"members": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/channels/{channel_id}/members"}}}},
			},
			"groups": {
				Endpoints:    map[string]spec.Endpoint{"list": {Method: "GET", Path: "/groups"}},
				SubResources: map[string]spec.Resource{"members": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/groups/{group_id}/members"}}}},
			},
			"teams": {
				Endpoints:    map[string]spec.Endpoint{"list": {Method: "GET", Path: "/teams"}},
				SubResources: map[string]spec.Resource{"members": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/teams/{team_id}/members"}}}},
			},
		},
	}

	tables := BuildSchema(s)
	names := make(map[string]bool, len(tables))
	for _, t := range tables {
		names[t.Name] = true
	}
	assert.True(t, names["channels_members"])
	assert.True(t, names["groups_members"])
	assert.True(t, names["teams_members"])
	assert.False(t, names["members"], "no bare members table when the leaf collides under three parents")
}

// TestBuildSchema_CamelCaseShardName verifies the shard helper snake-cases
// its inputs so a profiler-emitted DependentResource.Name and a schema-builder
// table name agree byte-for-byte even when the parent resource key is
// camelCase (common in Google Discovery specs).
func TestBuildSchema_CamelCaseShardName(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"userData": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/userData"}},
				SubResources: map[string]spec.Resource{
					"loginEvents": {
						Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/userData/{user_id}/loginEvents"}},
					},
				},
			},
			"adminData": {
				Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/adminData"}},
				SubResources: map[string]spec.Resource{
					"loginEvents": {
						Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/adminData/{admin_id}/loginEvents"}},
					},
				},
			},
		},
	}

	tables := BuildSchema(s)
	names := make(map[string]bool, len(tables))
	for _, t := range tables {
		names[t.Name] = true
	}
	assert.True(t, names["user_data_login_events"], "camelCase parent + sub snake-cases through the shard helper")
	assert.True(t, names["admin_data_login_events"])
}

// TestBuildSchema_SubResourceUniqueKeepsBareName verifies the fix for #694
// does not regress the common case: a sub-resource that appears under exactly
// one parent (e.g. /channels/{channel_id}/messages with no other parent for
// "messages") keeps its bare name "messages" and existing FK column.
func TestBuildSchema_SubResourceUniqueKeepsBareName(t *testing.T) {
	s := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/channels"},
				},
				SubResources: map[string]spec.Resource{
					"messages": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/channels/{channel_id}/messages"},
						},
					},
				},
			},
		},
	}

	tables := BuildSchema(s)

	names := make([]string, 0, len(tables))
	byName := make(map[string]TableDef, len(tables))
	for _, t := range tables {
		names = append(names, t.Name)
		byName[t.Name] = t
	}

	assert.Contains(t, names, "messages", "unique sub-resource keeps bare name")
	assert.NotContains(t, names, "channels_messages", "no shard prefix when leaf name is unique")

	messages := byName["messages"]
	require.Greater(t, len(messages.Columns), 1)
	assert.Equal(t, "channels_id", messages.Columns[1].Name, "FK column matches sole parent")
}

// findTable returns nil when no match exists so callers can render
// a clearer assertion failure than `tables[0]` panicking.
func findTable(tables []TableDef, name string) *TableDef {
	for i := range tables {
		if tables[i].Name == name {
			return &tables[i]
		}
	}
	return nil
}
