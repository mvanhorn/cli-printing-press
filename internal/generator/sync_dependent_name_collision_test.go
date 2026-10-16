package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDependentSyncNamesRejectsCollision(t *testing.T) {
	t.Parallel()

	g := &Generator{profile: &profiler.APIProfile{
		SyncableResources:      []profiler.SyncableResource{{Name: "groups", Path: "/groups"}},
		DependentSyncResources: []profiler.DependentResource{{Name: "groups", ParentResource: "groups", Path: "/groups/{group_id}/users"}},
	}}
	err := g.validateDependentSyncNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "groups")
}

func TestGeneratedSyncRenamesDependentThatCollidesWithTopLevelResource(t *testing.T) {
	t.Parallel()

	apiSpec := groupMembersCollisionSpec()
	profile := profiler.Profile(apiSpec)
	syncable := map[string]struct{}{}
	for _, resource := range profile.SyncableResources {
		if resource.SkipDefaultSync {
			continue
		}
		syncable[resource.Name] = struct{}{}
	}
	require.Contains(t, syncable, "groups")
	require.Contains(t, syncable, "channels")

	depsByPath := map[string]profiler.DependentResource{}
	for _, dep := range profile.DependentSyncResources {
		depsByPath[dep.Path] = dep
		_, collides := syncable[dep.Name]
		assert.False(t, collides, "dependent %s name %s collides with a top-level resource", dep.Path, dep.Name)
	}
	require.Equal(t, "groups_users", depsByPath["/groups/{group_id}/users"].Name)
	require.Equal(t, "groups", depsByPath["/groups/{group_id}/users"].ParentResource)
	require.Equal(t, "messages", depsByPath["/channels/{channel_id}/messages"].Name)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string")
	assert.Contains(t, defaults, `"groups"`)
	assert.Contains(t, defaults, `"channels"`)
	assert.NotContains(t, defaults, `"groups_users"`)
	assert.NotContains(t, defaults, `"messages"`)
	assert.Contains(t, syncSrc, `{Name: "groups_users", ParentTable: "groups", ParentIDParam: "group_id", PathTemplate: "/groups/{group_id}/users"`)
	assert.Contains(t, syncSrc, `{Name: "messages", ParentTable: "channels"`)

	testPath := filepath.Join(outputDir, "internal", "cli", "group_members_sync_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(groupMembersGeneratedSyncTest(apiSpec.Name)), 0o644))

	runGoCommandRequired(t, outputDir, "test", "-run", "TestGroupMembersCollisionSync", "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedSyncDisambiguatesDependentFromUnresolvedCollection(t *testing.T) {
	t.Parallel()

	apiSpec := groupMembersCollisionSpec()
	apiSpec.Resources["groups_users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:     "GET",
				Path:       "/projects/{project_id}/members",
				Response:   spec.ResponseDef{Type: "array"},
				Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
				Params: []spec.Param{{
					Name:       "project_id",
					Type:       "string",
					Required:   true,
					Positional: true,
				}},
			},
		},
	}

	profile := profiler.Profile(apiSpec)
	syncable := map[string]profiler.SyncableResource{}
	for _, resource := range profile.SyncableResources {
		syncable[resource.Name] = resource
	}
	require.Contains(t, syncable, "groups")
	require.Contains(t, syncable, "groups_users")
	assert.True(t, syncable["groups_users"].SkipDefaultSync)
	assert.Equal(t, "/projects/{project_id}/members", syncable["groups_users"].Path)

	depsByPath := map[string]profiler.DependentResource{}
	for _, dep := range profile.DependentSyncResources {
		depsByPath[dep.Path] = dep
		_, collides := syncable[dep.Name]
		assert.Falsef(t, collides, "dependent %s name %s collides with a top-level resource", dep.Path, dep.Name)
	}
	require.Equal(t, "groups_users_get", depsByPath["/groups/{group_id}/users"].Name)
	require.Equal(t, "groups", depsByPath["/groups/{group_id}/users"].ParentResource)
	require.Equal(t, "messages", depsByPath["/channels/{channel_id}/messages"].Name)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string")
	assert.Contains(t, defaults, `"groups"`)
	assert.Contains(t, defaults, `"channels"`)
	assert.NotContains(t, defaults, `"groups_users"`)
	assert.NotContains(t, defaults, `"messages"`)
	paths := generatedFunctionBody(t, syncSrc, "func syncResourcePath(")
	compactPaths := strings.Join(strings.Fields(paths), " ")
	assert.Contains(t, compactPaths, `"groups": "/groups"`)
	assert.Contains(t, compactPaths, `"groups_users": "/projects/{project_id}/members"`)
	assert.NotContains(t, compactPaths, "groups_users_get")
	assert.Contains(t, syncSrc, `{Name: "groups_users_get", ParentTable: "groups", ParentIDParam: "group_id", PathTemplate: "/groups/{group_id}/users"`)
	assert.NotContains(t, syncSrc, `{Name: "groups_users", ParentTable: "groups"`)
	assert.Contains(t, syncSrc, `{Name: "messages", ParentTable: "channels"`)

	requireGeneratedCompiles(t, outputDir)
}

func groupMembersCollisionSpec() *spec.APISpec {
	list := func(path string, params ...string) spec.Endpoint {
		endpoint := spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Response:   spec.ResponseDef{Type: "array"},
			Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
		}
		for _, param := range params {
			endpoint.Params = append(endpoint.Params, spec.Param{
				Name:       param,
				Type:       "string",
				Required:   true,
				Positional: true,
			})
		}
		return endpoint
	}

	return &spec.APISpec{
		Name:    "group-members",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/group-members-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"groups": {
				Endpoints: map[string]spec.Endpoint{
					"list":  list("/groups"),
					"users": list("/groups/{group_id}/users", "group_id"),
				},
			},
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/channels"),
				},
			},
			"messages": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/channels/{channel_id}/messages", "channel_id"),
				},
			},
		},
	}
}

func groupMembersGeneratedSyncTest(moduleName string) string {
	return `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"` + naming.CLI(moduleName) + `/internal/store"
)

type groupMembersClient struct {
	paths []string
}

func (c *groupMembersClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	c.paths = append(c.paths, path)
	switch path {
	case "/groups":
		return json.RawMessage(` + "`" + `[{"id":"g1","name":"Admins"}]` + "`" + `), nil
	case "/channels":
		return json.RawMessage(` + "`" + `[{"id":"c1","name":"General"}]` + "`" + `), nil
	case "/groups/g1/users":
		return json.RawMessage(` + "`" + `[{"id":"u1","name":"Ada"}]` + "`" + `), nil
	case "/channels/c1/messages":
		return json.RawMessage(` + "`" + `[{"id":"m1","text":"hi"}]` + "`" + `), nil
	default:
		return nil, fmt.Errorf("unexpected sync path %q", path)
	}
}

func (*groupMembersClient) RateLimit() float64 { return 0 }

func TestGroupMembersCollisionSync(t *testing.T) {
	flat := flatSyncResources(defaultSyncResources())
	if !containsString(flat, "groups") || !containsString(flat, "channels") {
		t.Fatalf("flat sync plan = %v, want groups and channels", flat)
	}
	if containsString(flat, "groups_users") || containsString(flat, "messages") {
		t.Fatalf("flat sync plan includes a dependent name: %v", flat)
	}
	filtered := flatSyncResources([]string{"groups"})
	if len(filtered) != 1 || filtered[0] != "groups" {
		t.Fatalf("flatSyncResources([groups]) = %v, want [groups]", filtered)
	}

	var usersName, messagesName string
	for _, dep := range dependentResourceDefs() {
		switch dep.PathTemplate {
		case "/groups/{group_id}/users":
			usersName = dep.Name
			if dep.ParentTable != "groups" {
				t.Fatalf("users parent = %q, want groups", dep.ParentTable)
			}
		case "/channels/{channel_id}/messages":
			messagesName = dep.Name
		}
	}
	if usersName != "groups_users" {
		t.Fatalf("colliding dependent name = %q, want groups_users", usersName)
	}
	if messagesName != "messages" {
		t.Fatalf("non-colliding dependent name = %q, want messages", messagesName)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	client := &groupMembersClient{}
	var events bytes.Buffer
	for _, resource := range []string{"groups", "channels"} {
		result := syncResource(context.Background(), client, db, resource, "", false, 1, false, false, nil, &events)
		if result.Err != nil {
			t.Fatalf("sync %s: %v", resource, result.Err)
		}
		if result.Count == 0 {
			t.Fatalf("sync %s stored no rows", resource)
		}
	}
	results := syncDependentResources(context.Background(), client, db, "", false, 1, false, false, nil, nil, &events, 1)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("sync dependent %s: %v", result.Resource, result.Err)
		}
	}

	assertResourceCount(t, db, "groups", 1)
	assertResourceCount(t, db, "channels", 1)
	assertResourceCount(t, db, usersName, 1)
	assertResourceCount(t, db, messagesName, 1)

	for _, path := range client.paths {
		switch path {
		case "/groups", "/channels", "/groups/g1/users", "/channels/c1/messages":
		default:
			t.Fatalf("unexpected path %q in %v", path, client.paths)
		}
	}
	if !containsString(client.paths, "/groups") || !containsString(client.paths, "/groups/g1/users") {
		t.Fatalf("paths = %v, want both the top-level groups list and its members", client.paths)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertResourceCount(t *testing.T, db *store.Store, resource string, want int) {
	t.Helper()
	var got int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = ?` + "`" + `, resource).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", resource, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", resource, got, want)
	}
}
`
}
