package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncResourcesExamplePrefersParentWithDependent(t *testing.T) {
	t.Parallel()

	syncable := []profiler.SyncableResource{
		{Name: "accounts"},
		{Name: "projects"},
		{Name: "users"},
	}
	dependent := []profiler.DependentResource{{Name: "project_items", ParentResource: "projects"}}

	assert.Equal(t, "projects,accounts", syncResourcesExample(syncable, dependent))
}

func TestSelectExampleUsesFirstSyncableResponseFields(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"initiatives": {Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/initiatives",
					Response: spec.ResponseDef{Type: "array", Item: "Initiative"},
				},
			}},
		},
		Types: map[string]spec.TypeDef{
			"Initiative": {Fields: []spec.TypeField{
				{Name: "idIniziativa"},
				{Name: "titoloIniziativa"},
				{Name: "stato"},
				{Name: "dataFine"},
			}},
		},
	}

	syncable := []profiler.SyncableResource{{Name: "initiatives", Path: "/initiatives", Method: "GET"}}
	assert.Equal(t, "idIniziativa,titoloIniziativa,stato", selectExample(apiSpec, syncable))
}

func TestSelectExampleOmitsWhenResponseFieldsAreUnknown(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Response: spec.ResponseDef{Type: "array"}},
			}},
		},
	}

	syncable := []profiler.SyncableResource{{Name: "items", Path: "/items", Method: "GET"}}
	assert.Empty(t, selectExample(apiSpec, syncable))
}

func TestSelectExampleRejectsUnsafeFieldNames(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Response: spec.ResponseDef{Type: "array", Item: "Item"}},
			}},
		},
		Types: map[string]spec.TypeDef{
			"Item": {Fields: []spec.TypeField{{Name: "meta.id"}}},
		},
	}

	syncable := []profiler.SyncableResource{{Name: "items", Path: "/items", Method: "GET"}}
	assert.Empty(t, selectExample(apiSpec, syncable))
}

func TestCompactFieldMapLiteralUsesDocumentedWireFields(t *testing.T) {
	t.Parallel()

	types := map[string]spec.TypeDef{
		"Event": {Fields: []spec.TypeField{
			{Name: "id"},
			{Name: "odds"},
			{Name: "odds"},
			{Name: "event_name"},
			{Name: ""},
		}},
	}

	assert.Equal(t, `map[string]bool{"id": true, "odds": true, "event_name": true}`, compactFieldMapLiteral("Event", types))
	assert.Equal(t, "nil", compactFieldMapLiteral("Missing", types))
}

func TestGeneratedExamplesUseAPIModelAcrossDocsAndSyncHelp(t *testing.T) {
	t.Parallel()

	apiSpec := modelExamplesSpec("model-examples")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	readme := readGeneratedFile(t, outputDir, "README.md")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	sync := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	wantResources := syncResourcesExample(gen.profile.SyncableResources, gen.profile.DependentSyncResources)

	for _, content := range []string{readme, skill} {
		assert.Contains(t, content, "--select invoice_id,title,state")
		assert.NotContains(t, content, "--select id,name,status")
	}
	assert.Contains(t, root, "--select invoice_id,title,state")
	assert.NotContains(t, root, "--select id,name,status")
	assert.Equal(t, "invoices,zcustomers", wantResources)
	assert.Contains(t, sync, "--resources invoices,zcustomers")
	assert.NotContains(t, sync, "--resources channels,messages")
}

func TestGeneratedGraphQLSyncExampleUsesAPIModel(t *testing.T) {
	t.Parallel()

	apiSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	sync := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	want := syncResourcesExample(gen.profile.SyncableResources, gen.profile.DependentSyncResources)
	require.NotEmpty(t, want, "GraphQL fixture should expose at least one sync resource")
	assert.Contains(t, sync, "--resources "+want)
	assert.NotContains(t, sync, "--resources channels,messages")
}

func TestGeneratedDocsOmitUnverifiableSelectExample(t *testing.T) {
	t.Parallel()

	apiSpec := modelExamplesSpec("unknown-select-example")
	apiSpec.Resources["aempty"] = spec.Resource{Endpoints: map[string]spec.Endpoint{
		"list": {Method: "GET", Path: "/aempty", Response: spec.ResponseDef{Type: "array"}, IDField: "aempty_id"},
	}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	readme := readGeneratedFile(t, outputDir, "README.md")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Empty(t, selectExampleForCommand(apiSpec))
	assert.Contains(t, readme, "--select <field>[,<field>...]")
	assert.NotContains(t, skill, "--select id,name,status")
	assert.NotContains(t, skill, " --select ")
	assert.NotContains(t, root, "--select id,name,status")
}

func modelExamplesSpec(name string) *spec.APISpec {
	return &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MODEL_EXAMPLES_TOKEN"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/" + name + "-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"invoices": {Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/invoices", Response: spec.ResponseDef{Type: "array", Item: "Invoice"}, IDField: "invoice_id"},
			}},
			"zcustomers": {Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/customers", Response: spec.ResponseDef{Type: "array", Item: "Customer"}, IDField: "customer_id"},
			}},
		},
		Types: map[string]spec.TypeDef{
			"Invoice":  {Fields: []spec.TypeField{{Name: "invoice_id"}, {Name: "title"}, {Name: "state"}}},
			"Customer": {Fields: []spec.TypeField{{Name: "customer_id"}, {Name: "display_name"}}},
		},
	}
}
