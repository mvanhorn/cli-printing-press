package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedLocalSQLiteMCPUsesCobraMirrorInsteadOfHTTPTools(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("localstore")
	apiSpec.Source = spec.SourceLocalSQLite
	apiSpec.BaseURL = ""
	apiSpec.Resources["domains"] = spec.Resource{
		Description: "Manage domains",
		Endpoints: map[string]spec.Endpoint{
			"get":  {Method: "GET", Path: "/domains/{domain_id}", Description: "Get domain"},
			"list": {Method: "GET", Path: "/domains", Description: "List domains"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.NotContains(t, mcpSrc, `mcplib.NewTool("items_list"`)
	require.NotContains(t, mcpSrc, `makeAPIHandler("GET", "/items"`)
	require.Contains(t, mcpSrc, `func mcpLocalStoreMeta(db *store.Store)`)
	require.Contains(t, mcpSrc, `"source": "local"`)
	require.Contains(t, mcpSrc, `"oldest_synced_at"`)
	require.Contains(t, mcpSrc, `"meta":         meta`)
	require.Contains(t, mcpSrc, "cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)")

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.NotContains(t, commandSrc, `"pp:endpoint"`)
	require.Contains(t, commandSrc, `"pp:method": "GET"`)
	require.Contains(t, commandSrc, `"pp:path": "/items"`)
	require.Contains(t, commandSrc, `"mcp:read-only": "true"`)

	resourceCommandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "domains_list.go")
	require.NotContains(t, resourceCommandSrc, `"pp:endpoint"`)
	require.Contains(t, resourceCommandSrc, `"pp:method": "GET"`)
	require.Contains(t, resourceCommandSrc, `"pp:path": "/domains"`)
	require.Contains(t, resourceCommandSrc, `"mcp:read-only": "true"`)

	requireGeneratedCompiles(t, outputDir)
}
