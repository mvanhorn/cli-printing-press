package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestRecipeNarrativeEmitsMCPIntentTools(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("intentrecipes")
	outputDir := filepath.Join(t.TempDir(), "intentrecipes-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{
			{
				Title:       "Batch with quota guard",
				Command:     "intentrecipes-pp-cli coin batch --file=fixtures/certs.txt --dry-run --json",
				Explanation: "Forecast a batch without spending quota.",
			},
			{
				Title:       "Rank with numeric limit",
				Command:     "intentrecipes-pp-cli coin rank --limit=5 --json",
				Explanation: "Rank recent coins with a bounded result count.",
			},
			{
				Title:       "Plain lookup",
				Command:     "intentrecipes-pp-cli coin facts 12345678",
				Explanation: "A single endpoint-style lookup is already covered by command mirroring.",
			},
			{
				Title:       "Piped analysis",
				Command:     "intentrecipes-pp-cli coin facts --json | jq .cert",
				Explanation: "Shell pipelines are not lifted into generated MCP handlers.",
			},
		},
	}

	require.NoError(t, gen.Generate())

	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, tools, "RegisterIntents(s)")

	intents := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intents, `mcplib.NewTool("batch_with_quota_guard"`)
	require.Contains(t, intents, `mcplib.WithString("file"`)
	require.Contains(t, intents, `mcplib.WithBoolean("dry_run"`)
	require.Contains(t, intents, `mcplib.NewTool("rank_with_numeric_limit"`)
	require.Contains(t, intents, `mcplib.WithNumber("limit"`)
	require.Contains(t, intents, `cobratree.RunCLICommand(ctx, recipeCLIPath, args)`)
	require.NotContains(t, intents, "CombinedOutput")
	require.Contains(t, intents, `mcplib.NewTool("plain_lookup"`)
	require.Contains(t, intents, `mcplib.WithString("id"`)
	require.NotContains(t, intents, `mcplib.NewTool("piped_analysis"`)

	_ = readGeneratedFile(t, outputDir, "internal", "mcp", "recipe_intents_test.go")

	shellout := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	require.Contains(t, shellout, `exec.CommandContext(ctx, binPath, args...)`)
	require.Contains(t, shellout, `const shelloutCaptureLimit = bound.MaxBytes + 1`)
	require.Contains(t, shellout, `stdout := newCappedCapture()`)
	require.Contains(t, shellout, `stderr := newCappedCapture()`)
	require.Contains(t, shellout, `cmd.Stdout = stdout`)
	require.Contains(t, shellout, `cmd.Stderr = stderr`)
	require.NotContains(t, shellout, "CombinedOutput")

	runGoCommandRequired(t, outputDir, "test", "./internal/mcp")
}

func TestRecipeIntentDerivationSkipsTrivialAndUnsafeRecipes(t *testing.T) {
	t.Parallel()

	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Verify and extract", Command: "demo-pp-cli cert verify --cert <cert> --select=cert,grade --include-details=true --dry-run --dry_run --json"},
			{Title: "Just list", Command: "demo-pp-cli cert list"},
			{Title: "Pipeline", Command: "demo-pp-cli cert list --json | jq ."},
		},
	}, nil)

	require.Len(t, intents, 1)
	require.Equal(t, "verify_and_extract", intents[0].Name)
	require.Equal(t, []string{"cert", "verify", "--json"}, intents[0].Command)
	require.Len(t, intents[0].Params, 5)
	require.Equal(t, "cert", intents[0].Params[0].FlagName)
	require.Equal(t, "cert", intents[0].Params[0].InputName)
	require.Equal(t, "Cert", intents[0].Params[0].GoName)
	require.True(t, intents[0].Params[0].Required)
	require.Equal(t, "select", intents[0].Params[1].FlagName)
	require.Equal(t, "select", intents[0].Params[1].InputName)
	require.Equal(t, "cert,grade", intents[0].Params[1].Default)
	require.True(t, intents[0].Params[1].UseEquals)
	require.Equal(t, "include-details", intents[0].Params[2].FlagName)
	require.Equal(t, "include_details", intents[0].Params[2].InputName)
	require.Equal(t, recipeIntentParamString, intents[0].Params[2].Type)
	require.Equal(t, "true", intents[0].Params[2].Default)
	require.True(t, intents[0].Params[2].UseEquals)
	require.Equal(t, "dry-run", intents[0].Params[3].FlagName)
	require.Equal(t, "dry_run", intents[0].Params[3].InputName)
	require.Equal(t, "DryRun", intents[0].Params[3].GoName)
	require.Equal(t, "dry_run", intents[0].Params[4].FlagName)
	require.Equal(t, "dry_run2", intents[0].Params[4].InputName)
	require.Equal(t, "DryRun2", intents[0].Params[4].GoName)
}

func TestRecipeIntentDerivationSkipsAmbiguousSeparatedFlagValue(t *testing.T) {
	t.Parallel()

	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Ambiguous bool and positional", Command: "demo-pp-cli generate --force artifacts/ --json"},
		},
	}, nil)

	require.Empty(t, intents)
}

func TestRecipeIntentDerivationBindsPositionals(t *testing.T) {
	t.Parallel()

	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Scan site", Command: "demo-pp-cli advice https://example.com --copy --json"},
			{Title: "Get thing", Command: "demo-pp-cli get 12345678 --json"},
			{Title: "Upgrade release", Command: "demo-pp-cli upgrade v1.2.3 --json"},
			{Title: "Lookup slug", Command: "demo-pp-cli recipes my-best-brownies --json"},
			{Title: "Unbindable word", Command: "demo-pp-cli team add engineering --role=owner --json"},
		},
	}, nil)

	require.Len(t, intents, 4)
	require.Equal(t, []string{"advice", "--json"}, intents[0].Command)
	require.Len(t, intents[0].Params, 2)
	require.True(t, intents[0].Params[0].Positional)
	require.True(t, intents[0].Params[0].Required)
	require.Equal(t, "url", intents[0].Params[0].InputName)
	require.Equal(t, "Url", intents[0].Params[0].GoName)
	require.Equal(t, "copy", intents[0].Params[1].InputName)
	require.Equal(t, recipeIntentParamBoolean, intents[0].Params[1].Type)
	require.Len(t, intents[0].Args, 4)
	require.True(t, intents[0].Args[0].Static)
	require.Equal(t, "advice", intents[0].Args[0].Token)
	require.True(t, intents[0].Args[1].Param.Positional)
	require.Equal(t, "url", intents[0].Args[1].Param.InputName)
	require.Equal(t, "copy", intents[0].Args[2].Param.FlagName)
	require.True(t, intents[0].Args[3].Static)
	require.Equal(t, "--json", intents[0].Args[3].Token)

	require.Equal(t, []string{"get", "--json"}, intents[1].Command)
	require.True(t, intents[1].Params[0].Positional)
	require.Equal(t, "id", intents[1].Params[0].InputName)

	require.Equal(t, []string{"upgrade", "--json"}, intents[2].Command)
	require.True(t, intents[2].Params[0].Positional)
	require.Equal(t, "version", intents[2].Params[0].InputName)

	require.Equal(t, []string{"recipes", "--json"}, intents[3].Command)
	require.True(t, intents[3].Params[0].Positional)
	require.Equal(t, "slug", intents[3].Params[0].InputName)
}

func TestRecipeIntentGenerationBindsPositionalHandlerArgs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("positionrecipes")
	outputDir := filepath.Join(t.TempDir(), "positionrecipes-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{{
			Title:       "Scan site",
			Command:     "positionrecipes-pp-cli advice https://example.com --copy --json",
			Explanation: "Scan a site and return copy-paste fixes.",
		}},
	}

	require.NoError(t, gen.Generate())

	intents := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intents, `mcplib.WithString("url"`)
	require.Contains(t, intents, `appendRecipePositional(args, input["url"], true)`)
	require.NotContains(t, intents, `mcplib.WithString("args"`)
	require.NotContains(t, intents, "https://example.com")
	adviceIdx := strings.Index(intents, `args = append(args, "advice")`)
	urlIdx := strings.Index(intents, `appendRecipePositional(args, input["url"], true)`)
	copyIdx := strings.Index(intents, `appendRecipeBoolFlag(args, "copy", input["copy"], true)`)
	jsonIdx := strings.Index(intents, `args = append(args, "--json")`)
	require.NotEqual(t, -1, adviceIdx)
	require.NotEqual(t, -1, urlIdx)
	require.NotEqual(t, -1, copyIdx)
	require.NotEqual(t, -1, jsonIdx)
	require.Less(t, adviceIdx, urlIdx)
	require.Less(t, urlIdx, copyIdx)
	require.Less(t, copyIdx, jsonIdx)

	runGoCommandRequired(t, outputDir, "test", "./internal/mcp")
}

func TestRecipeIntentDerivationSkipsShellVariables(t *testing.T) {
	t.Parallel()

	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Shell env config", Command: "demo-pp-cli run --config=${CONFIG_FILE:-default.json} --json"},
			{Title: "Bare env token", Command: "demo-pp-cli run --config $CONFIG_FILE --json"},
		},
	}, nil)

	require.Empty(t, intents)
}

func TestRecipeIntentNameAvoidsSpecIntentCollisions(t *testing.T) {
	t.Parallel()

	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Batch report", Command: "demo-pp-cli batch run --file=one.json --json"},
			{Title: "Batch report", Command: "demo-pp-cli batch run --file=two.json --json"},
		},
	}, map[string]bool{"batch_report": true})

	require.Len(t, intents, 2)
	require.Equal(t, "batch_report_2", intents[0].Name)
	require.Equal(t, "batch_report_3", intents[1].Name)
}

func TestRecipeIntentNamesReserveGeneratedMCPSurface(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("demo")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Response: spec.ResponseDef{Type: "object"}},
			},
		},
	}
	reserved := reservedMCPToolNames(apiSpec, VisionTemplateSet{MCP: true, Search: true, Store: true}, []NovelFeature{
		{Command: "batch report"},
	})
	intents := buildRecipeIntents("demo", &ReadmeNarrative{
		Recipes: []Recipe{
			{Title: "Context", Command: "demo-pp-cli cert verify --cert=one.pem --json"},
			{Title: "Items list", Command: "demo-pp-cli cert verify --cert=two.pem --json"},
			{Title: "Search", Command: "demo-pp-cli cert verify --cert=three.pem --json"},
			{Title: "Batch report", Command: "demo-pp-cli cert verify --cert=four.pem --json"},
		},
	}, reserved)

	require.Len(t, intents, 4)
	require.Equal(t, "context_2", intents[0].Name)
	require.Equal(t, "items_list_2", intents[1].Name)
	require.Equal(t, "search_2", intents[2].Name)
	require.Equal(t, "batch_report_2", intents[3].Name)
}

func TestRecipeIntentGenerationDoesNotEmitEmptyIntentFile(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("trivialrecipes")
	outputDir := filepath.Join(t.TempDir(), "trivialrecipes-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{{Title: "Plain lookup", Command: "trivialrecipes-pp-cli items list"}},
	}

	require.NoError(t, gen.Generate())
	_, err := os.Stat(filepath.Join(outputDir, "internal", "mcp", "intents.go"))
	require.True(t, os.IsNotExist(err), "trivial single-call recipes should not create intents.go")

	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.False(t, strings.Contains(tools, "RegisterIntents(s)"))
}
