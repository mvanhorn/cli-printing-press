package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
		manifest string
	}{
		{name: "missing"},
		{name: "malformed", manifest: "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
			writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), "package cli\n")
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
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "tail.go"), "package cli\nfunc tail(resource string) {}\n")
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

func writeToolsManifestForResourcePathTest(t *testing.T, dir string, manifest ToolsManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ToolsManifestFilename), data, 0o644))
}
