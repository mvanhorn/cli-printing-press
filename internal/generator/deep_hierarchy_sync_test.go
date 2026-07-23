package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncPreservesDeepHierarchyIdentity(t *testing.T) {
	t.Parallel()

	apiSpec := deepHierarchySpec()
	profile := profiler.Profile(apiSpec)
	require.Len(t, profile.DependentSyncResources, 3)

	depsByPath := make(map[string]profiler.DependentResource, len(profile.DependentSyncResources))
	for _, dep := range profile.DependentSyncResources {
		depsByPath[dep.Path] = dep
	}
	assert.Equal(t, []profiler.DependentPathParam{
		{Param: "accountId", Field: "accounts_id"},
		{Param: "containerId", Field: "id"},
	}, depsByPath["/accounts/{accountId}/containers/{containerId}/custom-workspaces"].PathParams)
	assert.Equal(t, []profiler.DependentPathParam{
		{Param: "accountId", Field: "accounts_id"},
		{Param: "containerId", Field: "containers_id"},
		{Param: "customWorkspaceId", Field: "id"},
	}, depsByPath["/accounts/{accountId}/containers/{containerId}/custom-workspaces/{customWorkspaceId}/tags"].PathParams)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	gen.profile = profile
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "deep_hierarchy_sync_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(deepHierarchyGeneratedTest(apiSpec.Name)), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestDeepHierarchySync", "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

func deepHierarchySpec() *spec.APISpec {
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
		Name:    "deep-hierarchy",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/deep-hierarchy-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"accounts": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/accounts"),
				},
			},
			"containers": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/accounts/{accountId}/containers", "accountId"),
				},
			},
			"custom_workspaces": {
				Endpoints: map[string]spec.Endpoint{
					"list": list(
						"/accounts/{accountId}/containers/{containerId}/custom-workspaces",
						"accountId", "containerId",
					),
				},
			},
			"tags": {
				Endpoints: map[string]spec.Endpoint{
					"list": list(
						"/accounts/{accountId}/containers/{containerId}/custom-workspaces/{customWorkspaceId}/tags",
						"accountId", "containerId", "customWorkspaceId",
					),
				},
			},
			"flat_things": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/flat-things"),
				},
			},
		},
	}
}

func deepHierarchyGeneratedTest(moduleName string) string {
	return `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"` + naming.CLI(moduleName) + `/internal/store"
)

type deepHierarchyClient struct {
	paths []string
}

func (c *deepHierarchyClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	c.paths = append(c.paths, path)
	switch path {
	case "/accounts":
		return json.RawMessage(` + "`" + `[
			{"accountId":"100","name":"Primary Account"},
			{"accountId":"200","name":"Secondary Account"}
		]` + "`" + `), nil
	case "/flat-things":
		return json.RawMessage(` + "`" + `[{"id":"flat-1","flatThingId":"flat-wrong","name":"Flat Thing"}]` + "`" + `), nil
	case "/accounts/100/containers":
		return json.RawMessage(` + "`" + `[
			{"accountId":"100","containerId":"c-1","name":"Web"},
			{"accountId":"100","containerId":"c-2","name":"Server"}
		]` + "`" + `), nil
	case "/accounts/200/containers":
		return json.RawMessage(` + "`" + `[{"accountId":"200","containerId":"c-1","name":"Web"}]` + "`" + `), nil
	case "/accounts/100/containers/c-1/custom-workspaces":
		return json.RawMessage(` + "`" + `[
			{"accountId":"100","containerId":"c-1","customWorkspaceId":"cw-1","name":"Default Workspace"},
			{"accountId":"100","containerId":"c-1","customWorkspaceId":"cw-2","name":"Preview Workspace"}
		]` + "`" + `), nil
	case "/accounts/100/containers/c-2/custom-workspaces":
		return json.RawMessage(` + "`" + `[{"accountId":"100","containerId":"c-2","customWorkspaceId":"cw-1","name":"Default Workspace"}]` + "`" + `), nil
	case "/accounts/200/containers/c-1/custom-workspaces":
		return json.RawMessage(` + "`" + `[]` + "`" + `), nil
	case "/accounts/100/containers/c-1/custom-workspaces/cw-1/tags":
		return json.RawMessage(` + "`" + `[{"accountId":"100","containerId":"c-1","customWorkspaceId":"cw-1","tagId":"t-1","name":"Purchase"}]` + "`" + `), nil
	case "/accounts/100/containers/c-1/custom-workspaces/cw-2/tags":
		return json.RawMessage(` + "`" + `[{"accountId":"100","containerId":"c-1","customWorkspaceId":"cw-2","tagId":"t-1","name":"Purchase"}]` + "`" + `), nil
	case "/accounts/100/containers/c-2/custom-workspaces/cw-1/tags":
		return json.RawMessage(` + "`" + `[]` + "`" + `), nil
	default:
		return nil, fmt.Errorf("unexpected sync path %q", path)
	}
}

func (*deepHierarchyClient) RateLimit() float64 {
	return 0
}

func TestDeepHierarchySync(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	client := &deepHierarchyClient{}
	var events bytes.Buffer
	for _, resource := range []string{"accounts", "flat_things"} {
		result := syncResource(context.Background(), client, db, resource, "", false, 1, false, false, nil, &events)
		if result.Err != nil {
			t.Fatalf("sync %s: %v", resource, result.Err)
		}
	}

	results := syncDependentResources(
		context.Background(), client, db, "", false, 1, false, false,
		nil, nil, &events, 1,
	)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("sync dependent %s: %v", result.Resource, result.Err)
		}
	}

	assertResourceIDs(t, db, "accounts", []string{"100", "200"})
	assertResourceIDs(t, db, "flat_things", []string{"flat-1"})

	defs := dependentResourceDefs()
	var containersResource, customWorkspacesResource, tagsResource string
	for _, def := range defs {
		switch def.PathTemplate {
		case "/accounts/{accountId}/containers":
			containersResource = def.Name
		case "/accounts/{accountId}/containers/{containerId}/custom-workspaces":
			customWorkspacesResource = def.Name
		case "/accounts/{accountId}/containers/{containerId}/custom-workspaces/{customWorkspaceId}/tags":
			tagsResource = def.Name
		}
	}
	if containersResource == "" || customWorkspacesResource == "" || tagsResource == "" {
		t.Fatalf("missing generated dependent defs: %#v", defs)
	}

	assertResourceIDs(t, db, containersResource, []string{
		"c-1\x00100",
		"c-1\x00200",
		"c-2\x00100",
	})
	assertResourceIDs(t, db, customWorkspacesResource, []string{
		"cw-1\x00c-1\x00100",
		"cw-1\x00c-2\x00100",
		"cw-2\x00c-1\x00100",
	})
	assertResourceIDs(t, db, tagsResource, []string{
		"t-1\x00cw-1\x00c-1\x00100",
		"t-1\x00cw-2\x00c-1\x00100",
	})

	for _, path := range client.paths {
		if strings.Contains(path, "Account") || strings.Contains(path, "Default") {
			t.Fatalf("dependent path used display name as identity: %q", path)
		}
	}
}

func assertResourceIDs(t *testing.T, db *store.Store, resource string, want []string) {
	t.Helper()
	rows, err := db.DB().Query(` + "`" + `SELECT id FROM resources WHERE resource_type = ? ORDER BY id` + "`" + `, resource)
	if err != nil {
		t.Fatalf("query %s ids: %v", resource, err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan %s id: %v", resource, err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s ids: %v", resource, err)
	}
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s ids = %v, want %v", resource, got, want)
	}
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
