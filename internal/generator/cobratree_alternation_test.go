package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAlternationPositionalPropertyNames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("altprop")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{Name: "Where is", Command: "where-is <ip|hostname|mac>", Description: "Locate a host."},
		{Name: "Record data", Command: "record <A|AAAA|CNAME|TXT> <name> <value>", Description: "Store one record."},
		{Name: "Show item", Command: "show <id|name> <id_name>", Description: "Show one item."},
	}
	require.NoError(t, gen.Generate())

	where := readGeneratedFile(t, outputDir, "internal", "cli", "where_is.go")
	require.Contains(t, where, `"where-is <ip|hostname|mac>"`)
	record := readGeneratedFile(t, outputDir, "internal", "cli", "record.go")
	require.Contains(t, record, `"record <A|AAAA|CNAME|TXT> <name> <value>"`)
	show := readGeneratedFile(t, outputDir, "internal", "cli", "show.go")
	require.Contains(t, show, `"show <id|name> <id_name>"`)
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "mcp", "mirror_property_names_test.go"), "TestMirroredMCPPropertyNames")
	requireGeneratedCompiles(t, outputDir)

	proof := `package mcp

import (
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestAlternationPlaceholderOnRealRoot(t *testing.T) {
	s := server.NewMCPServer("altprop", "test")
	RegisterTools(s)
	where := mirrorByCommand(t, s, "where-is")
	if _, ok := where.InputSchema.Properties["ip|hostname|mac"]; ok {
		t.Fatal("raw alternation key leaked into where-is")
	}
	desc := propertyDescription(t, where, "ip_hostname_mac")
	if !strings.Contains(desc, "<ip|hostname|mac>") {
		t.Fatalf("where-is description = %q", desc)
	}
	show := mirrorByCommand(t, s, "show")
	if _, ok := show.InputSchema.Properties["id_name"]; !ok {
		t.Fatalf("show properties = %#v", show.InputSchema.Properties)
	}
	if _, ok := show.InputSchema.Properties["id_name_2"]; !ok {
		t.Fatalf("show properties = %#v", show.InputSchema.Properties)
	}
	record := mirrorByCommand(t, s, "record")
	for _, name := range []string{"A_AAAA_CNAME_TXT", "name", "value"} {
		if _, ok := record.InputSchema.Properties[name]; !ok {
			t.Fatalf("record missing %s: %#v", name, record.InputSchema.Properties)
		}
	}
}

func mirrorByCommand(t *testing.T, s *server.MCPServer, command string) mcplib.Tool {
	t.Helper()
	for _, entry := range s.ListTools() {
		if entry == nil || entry.Tool.Meta == nil {
			continue
		}
		got, _ := entry.Tool.Meta.AdditionalFields["pp:cli-command"].(string)
		if got == command {
			return entry.Tool
		}
	}
	t.Fatalf("mirror %q was not registered", command)
	return mcplib.Tool{}
}

func propertyDescription(t *testing.T, tool mcplib.Tool, name string) string {
	t.Helper()
	raw, ok := tool.InputSchema.Properties[name]
	if !ok {
		t.Fatalf("property %q missing: %#v", name, tool.InputSchema.Properties)
	}
	schema, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("property %q schema = %T", name, raw)
	}
	desc, _ := schema["description"].(string)
	return desc
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "alternation_root_test.go"), []byte(proof), 0o644))

	cobratreeOut, err := runGoCommandOutput(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^TestPositionalAlternationPlaceholderSanitizesKey$", "-count=1", "-v")
	require.NoError(t, err, cobratreeOut)
	require.Contains(t, cobratreeOut, "--- PASS: TestPositionalAlternationPlaceholderSanitizesKey")
	require.NotContains(t, cobratreeOut, "no tests to run")

	mcpOut, err := runGoCommandOutput(t, outputDir, "test", "./internal/mcp", "-run", "^(TestMirroredMCPPropertyNames|TestAlternationPlaceholderOnRealRoot)$", "-count=1", "-v")
	require.NoError(t, err, mcpOut)
	require.Contains(t, mcpOut, "--- PASS: TestMirroredMCPPropertyNames")
	require.Contains(t, mcpOut, "--- PASS: TestAlternationPlaceholderOnRealRoot")
	require.NotContains(t, mcpOut, "no tests to run")
}

func TestGeneratedDeviceMirroredPropertyNames(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	mcpOut, err := runGoCommandOutput(t, outputDir, "test", "./internal/mcp", "-run", "^TestMirroredMCPPropertyNames$", "-count=1", "-v")
	require.NoError(t, err, mcpOut)
	require.Contains(t, mcpOut, "--- PASS: TestMirroredMCPPropertyNames")
	require.NotContains(t, mcpOut, "no tests to run")

	cobratreeOut, err := runGoCommandOutput(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^TestPositionalAlternationPlaceholderSanitizesKey$", "-count=1", "-v")
	require.NoError(t, err, cobratreeOut)
	require.Contains(t, cobratreeOut, "--- PASS: TestPositionalAlternationPlaceholderSanitizesKey")
	require.NotContains(t, cobratreeOut, "no tests to run")
}
