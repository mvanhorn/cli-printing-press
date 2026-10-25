package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourcePathContractCheckFailsManifestMismatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), `package cli
var resourceReadPaths = map[string]string{
    "dns-zones": "/v3/dnsZones",
}
var resourceWritePaths = map[string]string{
    "dns-zones": "/v3/dnsZones",
}
var resourceReadConfigs = map[string]int{}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func tail(resource string) { path, _ := resourceReadPath(resource); _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: "/v3/dns-zones"}}})

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 0, results[0].Score)
	assert.Contains(t, results[0].Error, `"dns-zones" resolves to /v3/dnsZones`)
}

func TestResourcePathContractCheckPassesMappedAndLegacyMatchingPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tail   string
		shared string
	}{
		{
			name: "mapped",
			tail: `package cli
func tail(resource string) { path, _ := resourceReadPath(resource); _ = path }
`,
			shared: `package cli
var resourceReadPaths = map[string]string{
    "dns-zones": "/v3/dnsZones",
}
var resourceWritePaths = map[string]string{}
var resourceReadConfigs = map[string]int{}
`,
		},
		{
			name: "legacy matching",
			tail: `package cli
func tailKnownResources() []string { return []string{"items"} }
func tail(resource string) { path := "/" + resource; _ = path }
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), tc.tail)
			if tc.shared != "" {
				writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), tc.shared)
			}
			manifestPath := "/items"
			if tc.name == "mapped" {
				manifestPath = "/v3/dnsZones"
			}
			writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: manifestPath}}})
			results := runResourcePathContractChecks(dir)
			require.Len(t, results, 1)
			assert.Equal(t, 3, results[0].Score)
		})
	}
}

func TestResourcePathContractCheckCatchesLegacyDerivedMismatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func tailKnownResources() []string { return []string{"dns-zones"} }
func tail(resource string) { path := "/" + resource; _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: "/v3/dnsZones"}}})
	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 0, results[0].Score)
	assert.Contains(t, results[0].Error, `"dns-zones" builds /dns-zones`)
}

func TestResourcePathContractCheckFailsWithoutUsableManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		manifest string
	}{
		{name: "missing client call", source: "package cli\nfunc tail() { flags.newClient() }\n"},
		{name: "malformed client call", source: "package cli\nfunc tail() { flags.newClient() }\n", manifest: "{"},
		{name: "missing resolver", source: "package cli\nfunc tail(resource string) { path, _ := resourceReadPath(resource); _ = path }\n"},
		{name: "missing legacy derivation", source: "package cli\nfunc run(resource string) { path := \"/\" + resource; _ = path }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), tc.source)
			if tc.manifest != "" {
				writeTestFile(t, filepath.Join(dir, ToolsManifestFilename), tc.manifest)
			}
			results := runResourcePathContractChecks(dir)
			require.Len(t, results, 1)
			assert.Equal(t, 0, results[0].Score)
			assert.Contains(t, results[0].Error, "cannot validate generated resource paths")
		})
	}
}

func TestResourcePathContractCheckRejectsUnrecognizedResolver(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), "package cli\nfunc tail(resource string) { flags.newClient() }\n")
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{})
	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 0, results[0].Score)
	assert.Contains(t, results[0].Error, "does not use the emitted resource path resolver")
}

func TestResourcePathContractCheckRejectsLegacyImportDerivation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "import.go"), `package cli
func run(resource string) { path := "/" + resource; _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{})
	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 0, results[0].Score)
	assert.Contains(t, results[0].Error, "import derives paths from arbitrary resource names")
}

func TestResourcePathContractCheckAllowsCookieManifestOmission(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), `package cli
var resourceReadPaths = map[string]string{
    "items": "/items",
}
var resourceWritePaths = map[string]string{}
var resourceReadConfigs = map[string]int{}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func run(resource string) { path, _ := resourceReadPath(resource); _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Auth: ManifestAuth{Type: "cookie"}})
	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 3, results[0].Score)
}

func TestResourcePathContractCheckExemptsLocalStoreDumper(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		source  string
	}{
		{
			name:    "sqlite store reader",
			command: "export.go",
			source: `package cli

import (
	"database/sql"

	"example.com/cli/internal/store"
)

func export(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = store.Open(dbPath)
	return err
}
`,
		},
		{
			name:    "helper-only local reader",
			command: "tail.go",
			source: `package cli

// pp:data-source local

func tail(dbPath string) error {
	_, err := loadStoredListings(dbPath)
	return err
}
`,
		},
		{
			name:    "hand-written localstore helper",
			command: "export.go",
			source: `package cli

import (
	"database/sql"

	"example.com/cli/internal/localstore"
)

func export(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = localstore.Dump(db)
	return err
}
`,
		},
		{
			name:    "hand-written helper package",
			command: "import.go",
			source: `package cli

import "example.com/cli/internal/catalog"

func run(dbPath string) error {
	_, err := catalog.Listings(dbPath)
	return err
}
`,
		},
		{
			name:    "unsupported import",
			command: "import.go",
			source: `package cli

func run(resource string) error {
	return fmt.Errorf("import is not supported for %q", resource)
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", tc.command), tc.source)
			writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: "/items"}}})

			assert.Empty(t, runResourcePathContractChecks(dir))
		})
	}
}

func TestResourcePathContractCheckExemptsLocalStoreDumperWithoutManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
	}{
		{name: "missing"},
		{name: "malformed", manifest: "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", "export.go"), `package cli

import "example.com/cli/internal/localstore"

func export(dbPath string) error {
	_, err := localstore.Dump(dbPath)
	return err
}
`)
			if tc.manifest != "" {
				writeTestFile(t, filepath.Join(dir, ToolsManifestFilename), tc.manifest)
			}
			assert.Empty(t, runResourcePathContractChecks(dir))
		})
	}
}

func TestResourcePathContractCheckFailsAPICommandMissingResolver(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		source  string
	}{
		{
			name:    "generated client call",
			command: "export.go",
			source: `package cli

import "example.com/cli/internal/client"

func export() error {
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	_, err = c.Get(ctx, "/items", nil)
	return err
}
`,
		},
		{
			name:    "hidden client call",
			command: "export.go",
			source: `package cli

// pp:client-call

func export() error { return fetchAll() }
`,
		},
		{
			name:    "local annotation does not hide a client call",
			command: "tail.go",
			source: `package cli

// pp:data-source local

func tail() error {
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	_, err = c.Get(ctx, "/items", nil)
	return err
}
`,
		},
		{
			name:    "generated client import",
			command: "tail.go",
			source: `package cli

import "example.com/cli/internal/client"

func tail() (*client.Client, error) {
	return client.New(cfg)
}
`,
		},
		{
			name:    "client call beside a localstore helper",
			command: "export.go",
			source: `package cli

import (
	"database/sql"

	"example.com/cli/internal/localstore"
)

func export(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, dumpErr := localstore.Dump(db); dumpErr != nil {
		return dumpErr
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	_, err = c.Get(ctx, "/items", nil)
	return err
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", tc.command), tc.source)
			writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: "/items"}}})

			results := runResourcePathContractChecks(dir)
			require.Len(t, results, 1)
			assert.Equal(t, 0, results[0].Score)
			assert.Contains(t, results[0].Error, "does not use the emitted resource path resolver")
		})
	}
}

func TestResourcePathContractCheckScoresAPICommandBesideLocalStoreDumper(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), `package cli
var resourceReadPaths = map[string]string{
    "items": "/items",
}
var resourceWritePaths = map[string]string{}
var resourceReadConfigs = map[string]int{}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func tail(resource string) { path, _ := resourceReadPath(resource); _ = path }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "export.go"), `package cli

import "example.com/cli/internal/store"

func export(dbPath string) error {
	_, err := store.Open(dbPath)
	return err
}
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{Method: "GET", Path: "/items"}}})

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, "resource-path:tail", results[0].Command)
	assert.Equal(t, 3, results[0].Score)
}

func TestResourcePathContractCheckJoinsManifestBaseURL(t *testing.T) {
	dir := resourcePathOverrideFixture(t, ToolsManifest{Tools: []ManifestTool{{
		Method:  "GET",
		Path:    "/us/iphone/charts/36",
		BaseURL: "https://apps.apple.com",
	}}})

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 3, results[0].Score, results[0].Error)
}

func TestResourcePathContractCheckPassesHandPatchedAbsoluteManifestPath(t *testing.T) {
	dir := resourcePathOverrideFixture(t, ToolsManifest{Tools: []ManifestTool{{
		Method: "GET",
		Path:   "https://apps.apple.com/us/iphone/charts/36",
	}}})

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 3, results[0].Score, results[0].Error)
}

func TestResourcePathContractCheckFailsWhenOverrideBaseIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest ToolsManifest
	}{
		{
			name: "raw spec path",
			manifest: ToolsManifest{Tools: []ManifestTool{{
				Method: "GET",
				Path:   "/us/iphone/charts/36",
			}}},
		},
		{
			name: "wrong prefix",
			manifest: ToolsManifest{Tools: []ManifestTool{{
				Method:  "GET",
				Path:    "/foo",
				BaseURL: "https://labs.google/fx",
			}}},
		},
		{
			name: "tool absent",
			manifest: ToolsManifest{Tools: []ManifestTool{{
				Method: "GET",
				Path:   "/items",
			}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := resourcePathOverrideFixture(t, tc.manifest)
			results := runResourcePathContractChecks(dir)
			require.Len(t, results, 1)
			assert.Equal(t, 0, results[0].Score)
			assert.Contains(t, results[0].Error, "https://apps.apple.com/us/iphone/charts/36")
		})
	}
}

func TestResourcePathContractCheckJoinsPathPrefixBase(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), `package cli
var resourceReadPaths = map[string]string{
    "trpc": "https://labs.google/fx/api/trpc/foo",
}
var resourceWritePaths = map[string]string{}
var resourceReadConfigs = map[string]int{}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func tail(resource string) { path, _ := resourceReadPath(resource); _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, ToolsManifest{Tools: []ManifestTool{{
		Method:  "GET",
		Path:    "/foo",
		BaseURL: "https://labs.google/fx/api/trpc",
	}}})

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, 3, results[0].Score, results[0].Error)
}

func TestResourcePathContractCheckScoresResolverBesideStoreImport(t *testing.T) {
	dir := resourcePathOverrideFixture(t, ToolsManifest{Tools: []ManifestTool{{
		Method:  "GET",
		Path:    "/us/iphone/charts/36",
		BaseURL: "https://apps.apple.com",
	}}})
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli

import "example.com/cli/internal/store"

func tail(resource string) error {
	if _, err := store.Open("db"); err != nil {
		return err
	}
	path, err := resourceReadPath(resource)
	_ = path
	return err
}
`)

	results := runResourcePathContractChecks(dir)
	require.Len(t, results, 1)
	assert.Equal(t, "resource-path:tail", results[0].Command)
	assert.Equal(t, 3, results[0].Score, results[0].Error)
}

func TestGeneratedOverrideCLIAgreesWithResourcePathContract(t *testing.T) {
	api := &spec.APISpec{
		Name:      "charts",
		Version:   "0.1.0",
		BaseURL:   "https://api.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Learn:     spec.LearnConfig{Disabled: true},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/charts-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"categories": {
				BaseURL: "https://apps.apple.com",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/us/iphone/charts/36", Response: spec.ResponseDef{Type: "array"}},
					"get":    {Method: "GET", Path: "/us/iphone/charts/{id}", Response: spec.ResponseDef{Type: "object"}},
					"create": {Method: "POST", Path: "/us/iphone/charts/36", Body: []spec.Param{{Name: "name", Type: "string"}}},
				},
			},
			"quotes": {
				BaseURL: "https://resource.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/search", BaseURL: "https://endpoint.example.com/v1", Response: spec.ResponseDef{Type: "array"}},
					"create": {Method: "POST", Path: "/search", BaseURL: "https://endpoint.example.com/v1", Body: []spec.Param{{Name: "name", Type: "string"}}},
				},
			},
			"items": {Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Response: spec.ResponseDef{Type: "array"}},
				"get":    {Method: "GET", Path: "/items/{id}", Response: spec.ResponseDef{Type: "object"}},
				"create": {Method: "POST", Path: "/items", Body: []spec.Param{{Name: "name", Type: "string"}}},
			}},
			"books": {
				BaseURL: "https://gamma.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "https://clob.example.com/book", Response: spec.ResponseDef{Type: "array"}},
				},
			},
			"parents": {
				BaseURL: "https://parent.example.com",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/parents", Response: spec.ResponseDef{Type: "array"}},
				},
				SubResources: map[string]spec.Resource{
					"children": {Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/parents/{id}/children", Response: spec.ResponseDef{Type: "array"}},
					}},
				},
			},
		},
	}

	dir := t.TempDir()
	gen := generator.New(api, dir)
	gen.VisionSet = generator.VisionTemplateSet{Export: true, Import: true, Tail: true, MCP: true}
	require.NoError(t, gen.Generate())
	require.NoError(t, WriteToolsManifest(dir, api))

	paths, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "resource_paths.go"))
	require.NoError(t, err)
	resourcePaths := string(paths)
	assert.Contains(t, resourcePaths, "https://apps.apple.com/us/iphone/charts/36")
	assert.Contains(t, resourcePaths, "https://endpoint.example.com/v1/search")
	assert.Contains(t, resourcePaths, `"/items"`)
	assert.Contains(t, resourcePaths, "https://clob.example.com/book")
	assert.Contains(t, resourcePaths, "https://parent.example.com/parents")
	assert.Contains(t, resourcePaths, "https://apps.apple.com/us/iphone/charts/{id}")
	assert.NotContains(t, resourcePaths, "parents/{id}/children")

	mcp, err := os.ReadFile(filepath.Join(dir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	assert.Contains(t, string(mcp), "https://apps.apple.com/us/iphone/charts/36")
	assert.Contains(t, string(mcp), "https://parent.example.com/parents/{id}/children")

	manifest, err := ReadToolsManifest(dir)
	require.NoError(t, err)
	categories := manifestToolByName(t, manifest.Tools, "categories_list")
	assert.Equal(t, "/us/iphone/charts/36", categories.Path)
	assert.Equal(t, "https://apps.apple.com", categories.BaseURL)
	children := manifestToolByName(t, manifest.Tools, "parents_children_list")
	assert.Equal(t, "/parents/{id}/children", children.Path)
	assert.Equal(t, "https://parent.example.com", children.BaseURL)
	books := manifestToolByName(t, manifest.Tools, "books_list")
	assert.Equal(t, "https://clob.example.com/book", books.Path)
	assert.Empty(t, books.BaseURL)

	assertResourcePathScores(t, dir, map[string]int{
		"resource-path:tail":   3,
		"resource-path:export": 3,
		"resource-path:import": 3,
	})

	for i := range manifest.Tools {
		manifest.Tools[i].BaseURL = ""
	}
	writeToolsManifestForResourcePathTest(t, dir, *manifest)
	results := runResourcePathContractChecks(dir)
	var failed bool
	for _, result := range results {
		if result.Score == 0 {
			failed = true
			assert.Contains(t, result.Error, "https://apps.apple.com/us/iphone/charts/36")
		}
	}
	assert.True(t, failed, "stripping base_url must fail the contract")
}

func TestGeneratedOperationServerCLIAgreesWithResourcePathContract(t *testing.T) {
	parsed, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Listings
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /listings:
    get:
      operationId: listListings
      servers:
        - url: https://zimmo.example/api
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object}
    post:
      operationId: createListing
      servers:
        - url: https://zimmo.example/api
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name: {type: string}
      responses:
        "201": {description: created}
  /foo:
    get:
      operationId: listFoo
      servers:
        - url: https://labs.google/fx/api/trpc
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object}
  /health:
    get:
      operationId: listHealth
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items: {type: object}
`))
	require.NoError(t, err)
	parsed.Owner = "test-owner"
	parsed.OwnerName = "Test Author"
	parsed.Learn.Disabled = true
	if parsed.Auth.Type == "" {
		parsed.Auth.Type = "none"
	}
	if parsed.Config.Format == "" {
		parsed.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/listings-pp-cli/config.toml"}
	}

	dir := t.TempDir()
	gen := generator.New(parsed, dir)
	gen.VisionSet = generator.VisionTemplateSet{Export: true, Import: true, Tail: true, MCP: true}
	require.NoError(t, gen.Generate())
	require.NoError(t, WriteToolsManifest(dir, parsed))

	paths, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "resource_paths.go"))
	require.NoError(t, err)
	assert.Contains(t, string(paths), "https://zimmo.example/api/listings")
	assert.Contains(t, string(paths), "https://labs.google/fx/api/trpc/foo")
	assert.Contains(t, string(paths), `"/health"`)

	manifest, err := ReadToolsManifest(dir)
	require.NoError(t, err)
	var listingsBase, fooBase, healthBase string
	var sawListings, sawFoo, sawHealth bool
	for _, tool := range manifest.Tools {
		switch tool.Path {
		case "/listings":
			sawListings = true
			listingsBase = tool.BaseURL
		case "/foo":
			sawFoo = true
			fooBase = tool.BaseURL
		case "/health":
			sawHealth = true
			healthBase = tool.BaseURL
		}
	}
	require.True(t, sawListings && sawFoo && sawHealth)
	assert.Equal(t, "https://zimmo.example/api", listingsBase)
	assert.Equal(t, "https://labs.google/fx/api/trpc", fooBase)
	assert.Empty(t, healthBase)

	assertResourcePathScores(t, dir, map[string]int{
		"resource-path:tail":   3,
		"resource-path:export": 3,
		"resource-path:import": 3,
	})

	for i := range manifest.Tools {
		if manifest.Tools[i].Path == "/listings" {
			manifest.Tools[i].BaseURL = ""
		}
	}
	writeToolsManifestForResourcePathTest(t, dir, *manifest)
	results := runResourcePathContractChecks(dir)
	var failed bool
	for _, result := range results {
		if result.Score == 0 {
			failed = true
			assert.Contains(t, result.Error, "https://zimmo.example/api/listings")
		}
	}
	assert.True(t, failed, "a listings tool without its operation server must fail")
}

func assertResourcePathScores(t *testing.T, dir string, want map[string]int) {
	t.Helper()
	results := runResourcePathContractChecks(dir)
	got := make(map[string]int, len(results))
	errs := make(map[string]string, len(results))
	for _, result := range results {
		got[result.Command] = result.Score
		errs[result.Command] = result.Error
	}
	assert.Equal(t, want, got, errs)
}

func resourcePathOverrideFixture(t *testing.T, manifest ToolsManifest) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "resource_paths.go"), `package cli
var resourceReadPaths = map[string]string{
    "categories": "https://apps.apple.com/us/iphone/charts/36",
}
var resourceWritePaths = map[string]string{}
var resourceReadConfigs = map[string]int{}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), `package cli
func tail(resource string) { path, _ := resourceReadPath(resource); _ = path }
`)
	writeToolsManifestForResourcePathTest(t, dir, manifest)
	return dir
}

func writeToolsManifestForResourcePathTest(t *testing.T, dir string, manifest ToolsManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ToolsManifestFilename), data, 0o644))
}
