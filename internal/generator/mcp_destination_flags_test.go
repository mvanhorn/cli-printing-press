package generator

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPExportBlocksFilesystemDestinationFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcpdest")
	outputDir := filepath.Join(t.TempDir(), "mcpdest-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	const runtimeTest = `package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPExportDestinationFlagsBlocked(t *testing.T) {
	t.Setenv("MCPDEST_CLI_PATH", "fixture-cli")

	s := server.NewMCPServer("mcpdest", "test")
	RegisterTools(s)
	tools := s.ListTools()
	entry, ok := tools["export"]
	if !ok {
		t.Fatalf("export tool missing from tools/list: %#v", tools)
	}
	props := entry.Tool.InputSchema.Properties
	for _, want := range []string{"resource", "id", "format", "limit", "no-cache"} {
		if _, ok := props[want]; !ok {
			t.Fatalf("export tool schema missing %q: %#v", want, props)
		}
	}
	for _, hidden := range []string{"audit-dir", "db", "o", "output", "receipt-file", "yes"} {
		if _, ok := props[hidden]; ok {
			t.Fatalf("filesystem destination %q leaked into export tool schema: %#v", hidden, props)
		}
		result, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
			Name:      "export",
			Arguments: map[string]any{"resource": "items", hidden: "/tmp/evil"},
		}})
		if err != nil {
			t.Fatalf("export handler returned transport error for %q: %v", hidden, err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("export handler accepted filesystem destination %q: %#v", hidden, result)
		}
		text := mcpTextContent(t, result)
		if !strings.Contains(text, "unknown MCP parameter") || !strings.Contains(text, hidden) {
			t.Fatalf("export handler error for %q = %q", hidden, text)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "export_destination_flags_test.go"), []byte(runtimeTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCPExportDestinationFlagsBlocked$", "-count=1")
}

func TestGeneratedMCPBlocksCommandLocalDBFlag(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcpdb")
	outputDir := filepath.Join(t.TempDir(), "mcpdb-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, Sync: true, Analytics: true, MCP: true}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{
			{
				Title:       "Analytics over a custom store",
				Command:     "mcpdb-pp-cli analytics --db=<path> --agent",
				Explanation: "Run analytics against a caller-chosen SQLite store.",
			},
			{
				Title:       "Analytics window over a custom store",
				Command:     "mcpdb-pp-cli analytics --db=<path> --window=7d --agent",
				Explanation: "Run a bounded analytics window against a caller-chosen SQLite store.",
			},
		},
	}
	require.NoError(t, gen.Generate())

	shellout := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	require.Contains(t, shellout, `"db":           true`)
	for _, name := range mcpBlockedDestinationFlagNames() {
		require.Containsf(t, shellout, strconv.Quote(name)+":", "emitted blockedDestinationFlags missing %q", name)
	}
	classify := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "classify.go")
	require.Contains(t, classify, `FlagWriteSinksAnnotation = "mcp:write-flags"`)
	require.Contains(t, classify, "func flagWriteSinkNames(")
	typemap := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "typemap.go")
	require.Contains(t, typemap, "flagWriteSinkNames(cmd)")

	intents := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.NotContains(t, intents, `mcplib.WithString("db"`)
	require.NotContains(t, intents, `appendRecipeStringFlag(args, "db"`)
	require.NotContains(t, intents, `mcplib.NewTool("analytics_over_a_custom_store"`)
	require.Contains(t, intents, `mcplib.NewTool("analytics_window_over_a_custom_store"`)
	require.Contains(t, intents, `appendRecipeStringFlag(args, "window"`)

	const runtimeTest = `package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPToolsHideDBParameter(t *testing.T) {
	t.Setenv("MCPDB_CLI_PATH", "fixture-cli")

	s := server.NewMCPServer("mcpdb", "test")
	RegisterTools(s)
	for name, entry := range s.ListTools() {
		if _, ok := entry.Tool.InputSchema.Properties["db"]; ok {
			t.Fatalf("tool %q exposes a db parameter: %#v", name, entry.Tool.InputSchema.Properties)
		}
	}

	entry, ok := s.ListTools()["analytics"]
	if !ok {
		t.Fatal("analytics tool missing from tools/list")
	}
	result, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "analytics",
		Arguments: map[string]any{"db": "/tmp/evil.db"},
	}})
	if err != nil {
		t.Fatalf("analytics handler returned transport error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("analytics handler accepted db: %#v", result)
	}
	text := mcpTextContent(t, result)
	if !strings.Contains(text, "unknown MCP parameter") || !strings.Contains(text, "db") {
		t.Fatalf("analytics handler error = %q", text)
	}

	recipeEntry, ok := s.ListTools()["analytics_window_over_a_custom_store"]
	if !ok {
		t.Fatal("recipe tool missing from tools/list")
	}
	if _, ok := recipeEntry.Tool.InputSchema.Properties["window"]; !ok {
		t.Fatalf("recipe tool schema missing window: %#v", recipeEntry.Tool.InputSchema.Properties)
	}
	if _, ok := recipeEntry.Tool.InputSchema.Properties["db"]; ok {
		t.Fatalf("recipe tool exposes a db parameter: %#v", recipeEntry.Tool.InputSchema.Properties)
	}

	oldPath, oldErr := recipeCLIPath, recipeCLIPathErr
	t.Cleanup(func() {
		recipeCLIPath, recipeCLIPathErr = oldPath, oldErr
	})
	recipeCLIPath = writeRecipeIntentRecorder(t)
	recipeCLIPathErr = nil
	recipeResult, err := recipeEntry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "analytics_window_over_a_custom_store",
		Arguments: map[string]any{"window": "14d", "db": "/tmp/evil.db"},
	}})
	if err != nil {
		t.Fatalf("recipe handler returned transport error: %v", err)
	}
	argv := mcpTextContent(t, recipeResult)
	if !strings.Contains(argv, "--window=14d") {
		t.Fatalf("recipe handler argv missing window override: %q", argv)
	}
	if strings.Contains(argv, "--db") || strings.Contains(argv, "/tmp/evil.db") {
		t.Fatalf("recipe handler forwarded db: %q", argv)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "db_flag_blocked_test.go"), []byte(runtimeTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCPToolsHideDBParameter$", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^Test(BlockedStructuredArgsOnlyDropsInheritedRootFlags|ToolOptionsHideBlockedRootFlagsButKeepLocalCollisions|WriteSinkFlagsStayOutOfMCPSchemaAndArgv|InheritedWriteSinkFlagsStayOutOfMCPSchemaAndArgv|ShadowedPersistentWriteFlagStaysAvailable|FlagWriteSinkNamesParsesAnnotation)$", "-count=1")
}

func TestGeneratedRecipeToolsBlockAnnotatedWriteFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcprecipe")
	outputDir := filepath.Join(t.TempDir(), "mcprecipe-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{{
			Title:       "Archive bundle",
			Command:     "mcprecipe-pp-cli arc bundle --save-to=/tmp/out --window=7d --file=in.csv --json",
			Explanation: "Bundle an archive to a caller-chosen path.",
		}},
	}
	require.NoError(t, gen.Generate())

	intents := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intents, "recipeDestinationBlocked")
	require.Contains(t, intents, `cobratree.DestinationFlagBlocked`)
	require.Contains(t, intents, "recipeCommandPath")
	require.Contains(t, intents, `"save-to"`)

	const runtimeTest = `package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

func TestRecipeToolBlocksInheritedWriteFlag(t *testing.T) {
	root := &cobra.Command{Use: "mcprecipe"}
	archive := &cobra.Command{
		Use:     "archive",
		Aliases: []string{"arc"},
		Annotations: map[string]string{
			"mcp:write-flags": "save-to",
		},
	}
	archive.PersistentFlags().String("save-to", "", "destination")
	bundle := &cobra.Command{Use: "bundle"}
	bundle.Flags().String("window", "", "window")
	bundle.Flags().String("file", "", "input")
	archive.AddCommand(bundle)
	root.AddCommand(archive)

	oldRoot := recipeCommandRoot
	t.Cleanup(func() { recipeCommandRoot = oldRoot })
	recipeCommandRoot = func() *cobra.Command { return root }

	s := server.NewMCPServer("mcprecipe", "test")
	RegisterTools(s)
	entry, ok := s.ListTools()["archive_bundle"]
	if !ok {
		t.Fatal("recipe tool missing from tools/list")
	}
	props := entry.Tool.InputSchema.Properties
	if _, ok := props["save_to"]; ok {
		t.Fatalf("recipe tool exposes save_to: %#v", props)
	}
	for _, kept := range []string{"window", "file"} {
		if _, ok := props[kept]; !ok {
			t.Fatalf("recipe tool schema missing %q: %#v", kept, props)
		}
	}

	oldPath, oldErr := recipeCLIPath, recipeCLIPathErr
	t.Cleanup(func() {
		recipeCLIPath, recipeCLIPathErr = oldPath, oldErr
	})
	recipeCLIPath = writeRecipeIntentRecorder(t)
	recipeCLIPathErr = nil

	rejected, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "archive_bundle",
		Arguments: map[string]any{"save_to": "/tmp/victim.txt", "window": "14d"},
	}})
	if err != nil {
		t.Fatalf("recipe handler returned transport error: %v", err)
	}
	if rejected == nil || !rejected.IsError {
		t.Fatalf("recipe handler accepted save_to: %#v", rejected)
	}
	text := mcpTextContent(t, rejected)
	if !strings.Contains(text, "unknown MCP parameter") || !strings.Contains(text, "save_to") {
		t.Fatalf("recipe handler error = %q", text)
	}
	if strings.Contains(text, "/tmp/victim.txt") || strings.Contains(text, "--save-to") {
		t.Fatalf("recipe handler forwarded save-to: %q", text)
	}

	result, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "archive_bundle",
		Arguments: map[string]any{"window": "14d", "file": "in.csv"},
	}})
	if err != nil {
		t.Fatalf("recipe handler returned transport error: %v", err)
	}
	argv := mcpTextContent(t, result)
	if strings.Contains(argv, "--save-to") || strings.Contains(argv, "/tmp/out") || strings.Contains(argv, "/tmp/victim.txt") {
		t.Fatalf("recipe handler forwarded a destination: %q", argv)
	}
	if !strings.Contains(argv, "arc bundle") || !strings.Contains(argv, "--window=14d") || !strings.Contains(argv, "--file=in.csv") {
		t.Fatalf("recipe handler dropped a non-sink flag: %q", argv)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "recipe_write_flag_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestRecipeToolBlocksInheritedWriteFlag$", "-count=1")
}
