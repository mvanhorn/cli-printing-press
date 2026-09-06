package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRejectsDerivedCommandNameCollisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources map[string]spec.Resource
		wantPaths []string
	}{
		{
			name: "endpoint collides with top-level resource",
			resources: map[string]spec.Resource{
				"abilities": {
					Description: "Manage abilities",
					Endpoints: map[string]spec.Endpoint{
						"categories": {Method: "GET", Path: "/abilities/categories", Description: "List ability categories"},
						"list":       {Method: "GET", Path: "/abilities", Description: "List abilities"},
					},
				},
				"abilities_categories": {
					Description: "Manage ability categories",
					Endpoints: map[string]spec.Endpoint{
						"get":  {Method: "GET", Path: "/abilities/categories/{id}", Description: "Get an ability category"},
						"list": {Method: "GET", Path: "/abilities/categories", Description: "List ability categories"},
					},
				},
			},
			wantPaths: []string{`resource "abilities" endpoint "categories"`, `resource "abilities_categories"`},
		},
		{
			name: "normalized endpoint names collide",
			resources: map[string]spec.Resource{
				"abilities": {
					Description: "Manage abilities",
					Endpoints: map[string]spec.Endpoint{
						"ability-categories": {Method: "GET", Path: "/abilities/categories", Description: "List ability categories"},
						"ability_categories": {Method: "GET", Path: "/abilities/categories/{id}", Description: "Get an ability category"},
					},
				},
			},
			wantPaths: []string{`resource "abilities" endpoint "ability-categories"`, `resource "abilities" endpoint "ability_categories"`},
		},
		{
			name: "normalized resource command paths collide",
			resources: map[string]spec.Resource{
				"ISteamUser": {
					Description: "Manage Steam users",
					Endpoints: map[string]spec.Endpoint{
						"get":  {Method: "GET", Path: "/steam/users/{id}", Description: "Get a Steam user"},
						"list": {Method: "GET", Path: "/steam/users", Description: "List Steam users"},
					},
				},
				"SteamUser": {
					Description: "Manage alternate Steam users",
					Endpoints: map[string]spec.Endpoint{
						"get":  {Method: "GET", Path: "/alternate/steam/users/{id}", Description: "Get an alternate Steam user"},
						"list": {Method: "GET", Path: "/alternate/steam/users", Description: "List alternate Steam users"},
					},
				},
			},
			wantPaths: []string{`resource "ISteamUser"`, `resource "SteamUser"`},
		},
		{
			name: "promoted output path collides after normalization",
			resources: map[string]spec.Resource{
				"foo_bar": {
					Description: "List foo bars",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/foo-bars", Description: "List foo bars"},
					},
				},
				"promoted": {
					Description: "Manage promoted records",
					Endpoints: map[string]spec.Endpoint{
						"foo-bar": {Method: "GET", Path: "/promoted/foo-bars", Description: "List promoted foo bars"},
						"list":    {Method: "GET", Path: "/promoted", Description: "List promoted records"},
					},
				},
			},
			wantPaths: []string{`resource "foo_bar" endpoint "list"`, `resource "promoted" endpoint "foo-bar"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("command-surface-collision")
			apiSpec.Resources = tt.resources

			outputDir := filepath.Join(t.TempDir(), "command-surface-collision-pp-cli")
			err := New(apiSpec, outputDir).Generate()
			require.Error(t, err)
			for _, want := range tt.wantPaths {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestGenerateAllowsDistinctDerivedCommandNames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("command-surface-distinct")
	apiSpec.Resources = map[string]spec.Resource{
		"abilities": {
			Description: "Manage abilities",
			Endpoints: map[string]spec.Endpoint{
				"ability-categories": {Method: "GET", Path: "/abilities/categories", Description: "List ability categories"},
				"list":               {Method: "GET", Path: "/abilities", Description: "List abilities"},
			},
		},
		"abilities_categories": {
			Description: "Manage ability categories",
			Endpoints: map[string]spec.Endpoint{
				"get":  {Method: "GET", Path: "/abilities/categories/{id}", Description: "Get an ability category"},
				"list": {Method: "GET", Path: "/abilities/categories", Description: "List ability categories"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "command-surface-distinct-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateAllowsDistinctPromotedOutputPathsAfterNormalization(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-command-surface-distinct")
	apiSpec.Resources = map[string]spec.Resource{
		"foo_linux": {
			Description: "List Linux foo records",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/foo-linux", Description: "List Linux foo records"},
			},
		},
		"promoted_foo_linux": {
			Description: "Manage promoted Linux foo records",
			Endpoints: map[string]spec.Endpoint{
				"get":  {Method: "GET", Path: "/promoted/foo-linux/{id}", Description: "Get a promoted Linux foo record"},
				"list": {Method: "GET", Path: "/promoted/foo-linux", Description: "List promoted Linux foo records"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-command-surface-distinct-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_foo-linux.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "promoted_foo_linux_cmd.go"))
	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateRejectsFrameworkCommandCollisionAfterNormalization(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("framework-command-surface-collision")
	apiSpec.Resources = map[string]spec.Resource{
		"AgentContext": {
			Description: "Manage API agent context",
			Endpoints: map[string]spec.Endpoint{
				"get":  {Method: "GET", Path: "/agent-context/{id}", Description: "Get API agent context"},
				"list": {Method: "GET", Path: "/agent-context", Description: "List API agent context"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "framework-command-surface-collision-pp-cli")
	err := New(apiSpec, outputDir).Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `derived command path "agent-context"`)
	assert.Contains(t, err.Error(), `resource "AgentContext"`)
	assert.Contains(t, err.Error(), "emitted framework command")
}

func TestCommandSurfaceIncludesSiblingsOfPromotedEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-command-surface")
	apiSpec.GraphQLEndpointPath = "/graphql"
	apiSpec.Resources = map[string]spec.Resource{
		"custom_views": {
			Description: "Manage custom views",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/graphql",
					Description: "Get a custom view",
					Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
				},
				"list": {
					Method:      "GET",
					Path:        "/graphql",
					Description: "List custom views",
					Pagination:  &spec.Pagination{Type: "cursor", LimitParam: "first", CursorParam: "after"},
				},
			},
		},
	}

	promotedCommands, _, promotedEndpointNames := buildPromotedCommandPlan(apiSpec)
	assert.Equal(t, "get", promotedEndpointNames["custom_views"])
	assert.Equal(t,
		[]string{"custom-views", "custom-views list"},
		expectedCommandPaths(buildCommandSurface(apiSpec, promotedCommands)),
	)
}

func TestGeneratedSurfaceParityDetectsArtificialDrop(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("command-surface-parity")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"create": {Method: "POST", Path: "/orders", Description: "Create an order"},
				"list":   {Method: "GET", Path: "/orders", Description: "List orders"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "command-surface-parity-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	parentPath := filepath.Join(outputDir, "internal", "cli", "orders.go")
	parentSource, err := os.ReadFile(parentPath)
	require.NoError(t, err)
	const registration = "\tcmd.AddCommand(newOrdersCreateCmd(flags))\n"
	dropped := strings.Replace(string(parentSource), registration, "", 1)
	require.NotEqual(t, string(parentSource), dropped, "fixture must remove the create endpoint registration")
	require.NoError(t, os.WriteFile(parentPath, []byte(dropped), 0o644))

	cmd := exec.Command("go", "test", "-mod=mod", "./internal/cli", "-run", "^TestDeclaredAPISurfaceReachable$", "-count=1")
	cmd.Dir = outputDir
	cacheDir, err := goBuildCacheDir(outputDir)
	require.NoError(t, err)
	cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, string(output))
	assert.Contains(t, string(output), "orders create")
}

func collectionItemCollisionSpec() *spec.APISpec {
	apiSpec := minimalSpec("collection-item-collision")
	apiSpec.Resources = map[string]spec.Resource{
		"users": {
			Description: "Manage users",
			Endpoints: map[string]spec.Endpoint{
				"email": {Method: "GET", Path: "/users/email", Description: "Check whether an email is in use"},
				"list":  {Method: "GET", Path: "/users", Description: "List users"},
			},
			SubResources: map[string]spec.Resource{
				"email": {
					Description: "Manage a user's email",
					Endpoints: map[string]spec.Endpoint{
						"update": {
							Method:      "PUT",
							Path:        "/users/{userId}/email",
							Description: "Update a user's email",
							Params:      []spec.Param{{Name: "userId", In: "path", Type: "string", Required: true, Positional: true}},
						},
					},
				},
			},
		},
	}
	return apiSpec
}

func TestGenerateAllowsCollectionEndpointAndItemSubResourceSameLeaf(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "collection-item-collision-pp-cli")
	require.NoError(t, New(collectionItemCollisionSpec(), outputDir).Generate())

	collectionPath := filepath.Join(outputDir, "internal", "cli", "users_email.go")
	itemParentPath := filepath.Join(outputDir, "internal", "cli", "users_item_email.go")
	itemEndpointPath := filepath.Join(outputDir, "internal", "cli", "users_item_email_update.go")
	require.FileExists(t, collectionPath)
	require.FileExists(t, itemParentPath)
	require.FileExists(t, itemEndpointPath)
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "users_email_update.go"))

	collectionSrc, err := os.ReadFile(collectionPath)
	require.NoError(t, err)
	assert.Contains(t, string(collectionSrc), "func newUsersEmailCmd(")
	assert.Regexp(t, `Use:\s+"email"`, string(collectionSrc))

	itemParentSrc, err := os.ReadFile(itemParentPath)
	require.NoError(t, err)
	assert.Contains(t, string(itemParentSrc), "func newUsersItemEmailCmd(")
	assert.Regexp(t, `Use:\s+"item-email"`, string(itemParentSrc))
	assert.Contains(t, string(itemParentSrc), "newUsersItemEmailUpdateCmd(flags)")

	itemEndpointSrc, err := os.ReadFile(itemEndpointPath)
	require.NoError(t, err)
	assert.Contains(t, string(itemEndpointSrc), "func newUsersItemEmailUpdateCmd(")

	usersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "users.go"))
	require.NoError(t, err)
	assert.Contains(t, string(usersSrc), "newUsersEmailCmd(flags)")
	assert.Contains(t, string(usersSrc), "newUsersItemEmailCmd(flags)")

	surface := buildCommandSurface(collectionItemCollisionSpec(), nil)
	assert.Equal(t, []string{"users", "users email", "users item-email", "users item-email update", "users list"}, expectedCommandPaths(surface))

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateKeepsUniqueCollectionAndItemStems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources map[string]spec.Resource
		wantFiles []string
		wantIdent string
		wantUse   string
	}{
		{
			name: "collection endpoint only",
			resources: map[string]spec.Resource{
				"users": {
					Description: "Manage users",
					Endpoints: map[string]spec.Endpoint{
						"email": {Method: "GET", Path: "/users/email", Description: "Check whether an email is in use"},
						"list":  {Method: "GET", Path: "/users", Description: "List users"},
					},
				},
			},
			wantFiles: []string{"users_email.go"},
			wantIdent: "func newUsersEmailCmd(",
			wantUse:   `Use:\s+"email"`,
		},
		{
			name: "item sub-resource only",
			resources: map[string]spec.Resource{
				"users": {
					Description: "Manage users",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/users", Description: "List users"},
					},
					SubResources: map[string]spec.Resource{
						"email": {
							Description: "Manage a user's email",
							Endpoints: map[string]spec.Endpoint{
								"update": {
									Method:      "PUT",
									Path:        "/users/{userId}/email",
									Description: "Update a user's email",
									Params:      []spec.Param{{Name: "userId", In: "path", Type: "string", Required: true, Positional: true}},
								},
							},
						},
					},
				},
			},
			wantFiles: []string{"users_email.go", "users_email_update.go"},
			wantIdent: "func newUsersEmailCmd(",
			wantUse:   `Use:\s+"email"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("collection-item-unique")
			apiSpec.Resources = tt.resources
			outputDir := filepath.Join(t.TempDir(), "collection-item-unique-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			for _, name := range tt.wantFiles {
				require.FileExists(t, filepath.Join(outputDir, "internal", "cli", name))
			}
			require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "users_item_email.go"))
			require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "users_item_email_update.go"))

			parentSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "users_email.go"))
			require.NoError(t, err)
			assert.Contains(t, string(parentSrc), tt.wantIdent)
			assert.Regexp(t, tt.wantUse, string(parentSrc))

			requireGeneratedCompiles(t, outputDir)
		})
	}
}

func TestGenerateCollectionItemCollisionFromOpenAPI(t *testing.T) {
	t.Parallel()

	const doc = `openapi: "3.0.3"
info:
  title: Collection Item Collision API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      operationId: list
      summary: List users
      responses:
        "200":
          description: OK
  /users/email:
    get:
      operationId: email
      summary: Check whether an email is in use
      responses:
        "200":
          description: OK
  /users/{userId}/email:
    put:
      operationId: update
      summary: Update a user's email
      parameters:
        - name: userId
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`
	apiSpec, err := openapi.Parse([]byte(doc))
	require.NoError(t, err)
	require.Contains(t, apiSpec.Resources, "users")
	require.Contains(t, apiSpec.Resources["users"].Endpoints, "email")
	require.Contains(t, apiSpec.Resources["users"].SubResources, "email")

	apiSpec.Owner = "test-owner"
	apiSpec.OwnerName = "Test Author"
	outputDir := filepath.Join(t.TempDir(), "collection-item-openapi-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "users_email.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "users_item_email.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "users_item_email_update.go"))
	requireGeneratedCompiles(t, outputDir)
}
