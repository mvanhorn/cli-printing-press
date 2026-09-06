package generator

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratorSkipsReservedNovelFeatureRootCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("recall-collide")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Prior-art recall",
			Command:     "recall",
			Description: "Look up prior research notes.",
			Example:     "recall-collide-pp-cli recall --json",
		},
		{
			Name:        "Audit cache",
			Command:     "audit",
			Description: "Audit local cache state.",
			Example:     "recall-collide-pp-cli audit --json",
		},
	}
	stderr, err := captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.Contains(t, stderr, `warning: novel feature command "recall" would shadow framework cobra command "recall"`)
	assert.Contains(t, stderr, "recall-collide_recall")
	assert.NotContains(t, stderr, `warning: novel feature command "audit"`)

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newRecallCmd(flags, learnCfg))")
	assert.NotContains(t, root, "newNovelRecallCmd")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelAuditCmd(flags))")

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "recall.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "recall_test.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "audit.go"))

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "reserved_novel_runtime_test.go"), []byte(`package cli

import "testing"

func TestReservedNovelDoesNotDuplicateRecall(t *testing.T) {
	var matches int
	var short string
	for _, command := range RootCmd().Commands() {
		if command.Name() == "recall" {
			matches++
			short = command.Short
		}
	}
	if matches != 1 {
		t.Fatalf("root contains %d recall commands, want exactly one", matches)
	}
	if short == "" || short == "Look up prior research notes." {
		t.Fatalf("framework recall was shadowed by the novel stub: Short = %q", short)
	}
}

func TestUnreservedNovelStillWires(t *testing.T) {
	command, _, err := RootCmd().Find([]string{"audit"})
	if err != nil {
		t.Fatalf("Find(audit) error = %v", err)
	}
	if command == nil || command.Name() != "audit" {
		t.Fatalf("Find(audit) = %#v", command)
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestReservedNovelDoesNotDuplicateRecall|TestUnreservedNovelStillWires")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorEmitsReservedNovelWhenFrameworkCommandInactive(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("recall-free")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Prior-art recall",
		Command:     "recall",
		Description: "Look up prior research notes.",
		Example:     "recall-free-pp-cli recall --json",
	}}
	stderr, err := captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.NotContains(t, stderr, `would shadow framework cobra command "recall"`)
	require.False(t, apiSpec.Learn.Enabled)

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NotContains(t, root, "newRecallCmd(flags, learnCfg)")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelRecallCmd(flags))")

	recall := readGeneratedFile(t, outputDir, "internal", "cli", "recall.go")
	assert.Contains(t, recall, `Use:         "recall"`)
	assert.Contains(t, recall, `TODO: implement novel feature %q", "recall"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorEmitsNovelFeatureCommandStubs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("apify")
	items := apiSpec.Resources["items"]
	items.Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create an item",
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Actor call wrapper",
			Command:     "call",
			Description: "Call an actor with idempotent tags.",
			Rationale:   "Agents need to run actors without re-creating identical jobs.",
			Example:     "apify-pp-cli call apify/web-scraper --tag skill=reddit-digest --dedupe-key daily --ttl 24h --wait --agent",
		},
		{
			Name:        "Run classifier",
			Command:     "runs classify",
			Description: "Classify recent runs by failure mode.",
			Rationale:   "Agents need a bounded view of run failures.",
			Example:     "apify-pp-cli runs classify run-123 --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelCallCmd(flags))")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelRunsCmd(flags))")
	assert.Less(t, strings.Index(root, "addNovelCommandIfAbsent(rootCmd, newNovelCallCmd(flags))"),
		strings.Index(root, "for _, hook := range novelCommandHooks {"),
		"generated novel parents must attach before registerNovelCommand hooks run")

	call := readGeneratedFile(t, outputDir, "internal", "cli", "call.go")
	assert.NotContains(t, call, "Generated by CLI Printing Press")
	assert.Contains(t, call, "Novel command scaffold")
	assert.Contains(t, call, "// pp:data-source auto")
	assert.Contains(t, call, "auto, local, live, or computed")
	assert.Contains(t, call, `Use:         "call"`)
	assert.Contains(t, call, `Example:     "  apify-pp-cli call apify/web-scraper --tag skill=reddit-digest --dedupe-key daily --ttl 24h --wait --agent"`)
	assert.Contains(t, call, `"mcp:read-only": "false"`)
	assert.Contains(t, call, `"pp:data-source": "auto"`)
	assert.Contains(t, call, `"pp:novel-scaffold": "true"`)
	assert.Contains(t, call, `StringSliceVar(&flagTag, "tag", nil`)
	assert.Contains(t, call, `StringVar(&flagDedupeKey, "dedupe-key", ""`)
	assert.Contains(t, call, `StringVar(&flagTtl, "ttl", ""`)
	assert.Contains(t, call, `BoolVar(&flagWait, "wait", false`)
	assert.NotContains(t, call, `"agent"`)
	assert.Contains(t, call, `TODO: implement novel feature %q", "call"`)
	assert.Contains(t, call, `return writeDryRun(cmd.OutOrStdout(), flags, "call")`)
	assert.NotContains(t, call, "if dryRunOK(flags) {\n\t\t\t\treturn nil\n")

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "runs.go")
	assert.Contains(t, parent, `Use:         "runs"`)
	assert.Contains(t, parent, `Short:       "Work with runs"`)
	assert.Contains(t, parent, `Example:     "  apify-pp-cli runs classify run-123 --limit 10"`)
	assert.Contains(t, parent, "RunE:        parentNoSubcommandRunE(flags)")
	assert.Contains(t, parent, "addNovelCommandIfAbsent(cmd, newNovelRunsClassifyCmd(flags))")
	assert.NotContains(t, parent, `"pp:novel-scaffold"`)

	classify := readGeneratedFile(t, outputDir, "internal", "cli", "runs_classify.go")
	assert.Contains(t, classify, "// pp:data-source auto")
	assert.Contains(t, classify, `Use:         "classify"`)
	assert.Contains(t, classify, `Example:     "  apify-pp-cli runs classify run-123 --limit 10"`)
	assert.Contains(t, classify, `"mcp:read-only": "false"`)
	assert.Contains(t, classify, `"pp:data-source": "auto"`)
	assert.Contains(t, classify, `"pp:novel-scaffold": "true"`)
	assert.Contains(t, classify, `StringVar(&flagLimit, "limit", ""`)
	assert.Contains(t, classify, `TODO: implement novel feature %q", "runs classify"`)
	assert.Contains(t, classify, `return writeDryRun(cmd.OutOrStdout(), flags, "runs classify")`)

	testSrc := readGeneratedFile(t, outputDir, "internal", "cli", "call_test.go")
	assert.NotContains(t, testSrc, "Generated by CLI Printing Press")
	assert.Contains(t, testSrc, "Novel command scaffold tests")
	assert.NotContains(t, testSrc, "t.Skip")
	assert.Contains(t, testSrc, `for _, want := range []string{"Usage:", "call"}`)
	// New smoke test exercises --help on the wired command path so a missing
	// AddCommand or panicking RunE is caught before review.
	assert.Contains(t, testSrc, `func TestNovelCallHelpWires(t *testing.T)`)
	assert.Contains(t, testSrc, `cmd.SetArgs([]string{"call", "--help"})`)

	var runtimeTest strings.Builder
	runtimeTest.WriteString(`package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNovelFeatureStubsResolveAtRuntime(t *testing.T) {
	cases := [][]string{
		{"call", "--help"},
		{"runs", "--help"},
		{"runs", "classify", "--help"},
		{"call", "apify/web-scraper", "--dry-run"},
	}
	for _, args := range cases {
		cmd := RootCmd()
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("RootCmd(%v) error = %v", args, err)
		}
		if args[len(args)-1] == "--help" && len(args) == 2 && !strings.Contains(out.String(), "Examples:") {
			t.Fatalf("RootCmd(%v) help missing Examples section:\n%s", args, out.String())
		}
	}
}

`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_stub_runtime_test.go"), []byte(runtimeTest.String()), 0o644))
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/cli")
}

func TestGeneratorRegistersNovelFeatureUnderFrameworkParent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("framework-novel")
	apiSpec.Streaming = spec.StreamingConfig{
		Transport: spec.StreamingTransportWebSocket,
		URL:       "wss://api.example.com/v1/ws",
		Framing:   spec.StreamingFramingNDJSON,
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Verify sync",
		Command:     "sync verify",
		Description: "Verify synchronized data.",
		Example:     "framework-novel-pp-cli sync verify",
	}, {
		Name:        "Verify live stream",
		Command:     "live verify",
		Description: "Verify the live stream.",
		Example:     "framework-novel-pp-cli live verify",
	}, {
		Name:        "Explain help",
		Command:     "help explain",
		Description: "Explain help topics.",
		Example:     "framework-novel-pp-cli help explain",
	}, {
		Name:        "Check completion",
		Command:     "completion check",
		Description: "Check completion setup.",
		Example:     "framework-novel-pp-cli completion check",
	}}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, `rootCmd.Find(strings.Fields("sync"))`)
	assert.Contains(t, root, "addNovelCommandIfAbsent(parent, newNovelSyncVerifyCmd(flags))")
	assert.Contains(t, root, "addNovelCommandIfAbsent(parent, newNovelLiveVerifyCmd(flags))")

	// Exercise the generated Cobra tree so this regression catches a missing
	// framework-parent AddCommand rather than only checking template text.
	testPath := filepath.Join(outputDir, "internal", "cli", "framework_novel_wiring_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import "testing"

func TestNovelFrameworkParentReachable(t *testing.T) {
	for _, path := range [][]string{
		{"sync", "verify"},
		{"live", "verify"},
		{"help", "explain"},
		{"completion", "check"},
	} {
		command, _, err := RootCmd().Find(path)
		if err != nil {
			t.Fatalf("Find(%v) error = %v", path, err)
		}
		if command == nil || command.Name() != path[len(path)-1] {
			t.Fatalf("Find(%v) = %#v", path, command)
		}
	}
}
`), 0o644))

	cmd := exec.Command("go", "test", "-mod=mod", "./internal/cli", "-run", "TestNovelFrameworkParentReachable", "-count=1")
	cmd.Dir = outputDir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	plainDir := filepath.Join(t.TempDir(), naming.CLI("framework-plain"))
	plainSpec := minimalSpec("framework-plain")
	plainGen := New(plainSpec, plainDir)
	plainGen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, plainGen.Generate())
	plainRoot := readGeneratedFile(t, plainDir, "internal", "cli", "root.go")
	assert.NotContains(t, plainRoot, `rootCmd.Find(strings.Fields(`)
}

func TestGeneratorClassifiesNovelFeatureReadOnlyFromFeatureIntentOnly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("intent-novel")
	items := apiSpec.Resources["items"]
	items.SubResources = map[string]spec.Resource{
		"history": {
			Endpoints: map[string]spec.Endpoint{
				"listHistory": {Method: "GET", Path: "/items/{id}/history"},
			},
		},
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Dial customer",
			Command:     "afk ask",
			Description: "Dial a phone number and place an outbound call.",
			Rationale:   "Agents need to ask a person a question.",
			Example:     "intent-novel-pp-cli afk ask --to +15555550123",
		},
		{
			Name:        "Cost report",
			Command:     "cost",
			Description: "Read cost totals from the local SQLite store.",
			Rationale:   "Agents need local spend visibility.",
			Example:     "intent-novel-pp-cli cost --json",
		},
		{
			Name:        "Transcript grep",
			Command:     "transcripts grep",
			Description: "Search transcripts in the local cache with FTS.",
			Rationale:   "Agents need local transcript lookup.",
			Example:     "intent-novel-pp-cli transcripts grep renewal",
		},
		{
			Name:        "Ambiguous helper",
			Command:     "assist",
			Description: "Coordinate the next recommended action.",
			Rationale:   "Agents need a workflow shortcut.",
			Example:     "intent-novel-pp-cli assist",
		},
		{
			Name:        "Set status",
			Command:     "set status",
			Description: "Set status using the latest workflow state.",
			Rationale:   "Agents need to update external state.",
			Example:     "intent-novel-pp-cli set status done",
		},
		{
			Name:        "Run query",
			Command:     "run query",
			Description: "Run query against the remote system.",
			Rationale:   "Agents need to execute an action query.",
			Example:     "intent-novel-pp-cli run query --name nightly",
		},
		{
			Name:        "Replay history",
			Command:     "replay history",
			Description: "Replay history into the remote workflow.",
			Rationale:   "Agents need to reproduce an earlier action.",
			Example:     "intent-novel-pp-cli replay history call-123",
		},
		{
			Name:        "Promote status",
			Command:     "promote status",
			Description: "Promote status after review.",
			Rationale:   "Agents need to advance the remote workflow.",
			Example:     "intent-novel-pp-cli promote status approved",
		},
	}
	require.NoError(t, gen.Generate())

	ask := readGeneratedFile(t, outputDir, "internal", "cli", "afk_ask.go")
	assert.Contains(t, ask, `"mcp:read-only": "false"`)
	cost := readGeneratedFile(t, outputDir, "internal", "cli", "cost.go")
	assert.Contains(t, cost, `"mcp:read-only": "true"`)
	grep := readGeneratedFile(t, outputDir, "internal", "cli", "transcripts_grep.go")
	assert.Contains(t, grep, `"mcp:read-only": "true"`)
	assist := readGeneratedFile(t, outputDir, "internal", "cli", "assist.go")
	assert.Contains(t, assist, `"mcp:read-only": "false"`)
	setStatus := readGeneratedFile(t, outputDir, "internal", "cli", "set_status.go")
	assert.Contains(t, setStatus, `"mcp:read-only": "false"`)
	runQuery := readGeneratedFile(t, outputDir, "internal", "cli", "run_query.go")
	assert.Contains(t, runQuery, `"mcp:read-only": "false"`)
	replayHistory := readGeneratedFile(t, outputDir, "internal", "cli", "replay_history.go")
	assert.Contains(t, replayHistory, `"mcp:read-only": "false"`)
	promoteStatus := readGeneratedFile(t, outputDir, "internal", "cli", "promote_status.go")
	assert.Contains(t, promoteStatus, `"mcp:read-only": "false"`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorDoesNotTreatInferredGETMutationAsReadOnlySpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("get-mutation-novel")
	items := apiSpec.Resources["items"]
	items.Endpoints["startItem"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/{id}/start",
		Description: "Start an item",
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Call actor",
			Command:     "call",
			Description: "Call an actor.",
			Rationale:   "Agents need to run actors.",
			Example:     "get-mutation-novel-pp-cli call",
		},
	}
	require.NoError(t, gen.Generate())

	call := readGeneratedFile(t, outputDir, "internal", "cli", "call.go")
	assert.NotContains(t, call, `Annotations: map[string]string{"mcp:read-only": "true"}`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorDoesNotTreatExplicitGETMutationAsReadOnlySpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("explicit-get-mutation-novel")
	items := apiSpec.Resources["items"]
	items.Endpoints["restartItem"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/{id}/restart",
		Description: "Restart an item",
		Mutation:    new(true),
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Call actor",
			Command:     "call",
			Description: "Call an actor.",
			Rationale:   "Agents need to run actors.",
			Example:     "explicit-get-mutation-novel-pp-cli call",
		},
	}
	require.NoError(t, gen.Generate())

	call := readGeneratedFile(t, outputDir, "internal", "cli", "call.go")
	assert.NotContains(t, call, `Annotations: map[string]string{"mcp:read-only": "true"}`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedNovelHookDoesNotDuplicateGeneratedStub(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novel-hook-collision")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Call actor",
		Command:     "call",
		Description: "Call an actor.",
		Example:     "novel-hook-collision-pp-cli call",
	}}
	require.NoError(t, gen.Generate())

	// A preserved novel file may register the same command as the generated
	// scaffold. The hand-authored hook must win without creating a shadowed
	// sibling in the Cobra tree.
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_call_hook.go"), []byte(`package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, &cobra.Command{Use: "call", Short: "hand-authored"})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_hook_collision_test.go"), []byte(`package cli

import "testing"

func TestNovelHookCollisionHasSingleReachableCommand(t *testing.T) {
	var matches int
	var short string
	for _, command := range RootCmd().Commands() {
		if command.Name() == "call" {
			matches++
			short = command.Short
		}
	}
	if matches != 1 {
		t.Fatalf("root contains %d call commands, want exactly one", matches)
	}
	if short != "hand-authored" {
		t.Fatalf("generated stub shadowed hook command: Short = %q", short)
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestNovelHookCollisionHasSingleReachableCommand")
}

func TestNovelHookDirectAddCommandReplacesGeneratedScaffold(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novel-hook-addcommand")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Call actor",
		Command:     "call",
		Description: "Call an actor.",
		Example:     "novel-hook-addcommand-pp-cli call",
	}}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "preferImplementedNovelCommands(rootCmd)")
	hookIdx := strings.Index(root, "for _, hook := range novelCommandHooks {")
	preferIdx := strings.Index(root, "preferImplementedNovelCommands(rootCmd)")
	require.Greater(t, hookIdx, -1)
	require.Greater(t, preferIdx, hookIdx)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_call_addcommand_hook.go"), []byte(`package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		_ = flags
		root.AddCommand(&cobra.Command{
			Use:   "call",
			Short: "hand-authored",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), "hook call")
				return nil
			},
		})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_hook_addcommand_test.go"), []byte(`package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNovelHookDirectAddCommandWins(t *testing.T) {
	var matches int
	var short string
	for _, command := range RootCmd().Commands() {
		if command.Name() == "call" {
			matches++
			short = command.Short
		}
	}
	if matches != 1 {
		t.Fatalf("root contains %d call commands, want exactly one", matches)
	}
	if short != "hand-authored" {
		t.Fatalf("generated stub shadowed direct AddCommand hook: Short = %q", short)
	}

	cmd := RootCmd()
	cmd.SetArgs([]string{"call"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("call: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "hook call") {
		t.Fatalf("hand-authored AddCommand hook did not run:\n%s", out.String())
	}
	if strings.Contains(out.String(), "TODO: implement novel feature") {
		t.Fatalf("TODO scaffold still ran:\n%s", out.String())
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestNovelHookDirectAddCommandWins")
}

func TestNovelHookAttachesChildUnderGeneratedNovelParent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novel-hook-parent")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Batch summarize",
		Command:     "batch summarize",
		Description: "Summarize a batch.",
		Example:     "novel-hook-parent-pp-cli batch summarize",
	}}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	stubIdx := strings.Index(root, "addNovelCommandIfAbsent(rootCmd, newNovelBatchCmd(flags))")
	hookIdx := strings.Index(root, "for _, hook := range novelCommandHooks {")
	require.Greater(t, stubIdx, -1)
	require.Greater(t, hookIdx, stubIdx, "hooks must run after generated novel parents attach")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_batch_hook.go"), []byte(`package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		parent, _, err := root.Find([]string{"batch"})
		if err != nil {
			return
		}
		addNovelCommandIfAbsent(parent, &cobra.Command{
			Use:   "summarize",
			Short: "hand-authored",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), "hook summarize")
				return nil
			},
		})
		addNovelCommandIfAbsent(parent, &cobra.Command{Use: "extra", Short: "hook-child"})
		addNovelCommandIfAbsent(root, &cobra.Command{Use: "standalone", Short: "new-top-level"})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_hook_parent_test.go"), []byte(`package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNovelHookExtendsGeneratedParent(t *testing.T) {
	root := RootCmd()
	summarize, _, err := root.Find([]string{"batch", "summarize"})
	if err != nil {
		t.Fatalf("Find(batch summarize) error = %v", err)
	}
	if summarize == nil || summarize.Short != "hand-authored" {
		t.Fatalf("TODO scaffold shadowed hook child: %#v", summarize)
	}

	extra, _, err := root.Find([]string{"batch", "extra"})
	if err != nil {
		t.Fatalf("Find(batch extra) error = %v", err)
	}
	if extra == nil || extra.Short != "hook-child" {
		t.Fatalf("hook child missing under generated novel parent: %#v", extra)
	}

	var standalone int
	for _, command := range root.Commands() {
		if command.Name() == "standalone" {
			standalone++
		}
		if command.Name() == "batch" {
			var summarizeCount int
			for _, child := range command.Commands() {
				if child.Name() == "summarize" {
					summarizeCount++
				}
			}
			if summarizeCount != 1 {
				t.Fatalf("batch contains %d summarize commands, want 1", summarizeCount)
			}
		}
	}
	if standalone != 1 {
		t.Fatalf("root contains %d standalone commands, want 1", standalone)
	}

	cmd := RootCmd()
	cmd.SetArgs([]string{"batch", "summarize"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("batch summarize: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "hook summarize") {
		t.Fatalf("implemented hook child did not run:\n%s", out.String())
	}
	if strings.Contains(out.String(), "TODO: implement novel feature") {
		t.Fatalf("TODO scaffold still ran:\n%s", out.String())
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestNovelHookExtendsGeneratedParent")
}

func TestImplementedNovelScaffoldSurvivesInPlaceRegen(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novel-impl-survive")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Audit cache",
		Command:     "audit",
		Description: "Audit local cache state.",
		Example:     "novel-impl-survive-pp-cli audit",
	}}
	require.NoError(t, gen.Generate())

	scaffoldPath := filepath.Join(outputDir, "internal", "cli", "audit.go")
	scaffold, err := os.ReadFile(scaffoldPath)
	require.NoError(t, err)
	implemented := strings.Replace(string(scaffold),
		`return fmt.Errorf("TODO: implement novel feature %q", "audit")`,
		`fmt.Fprintln(cmd.OutOrStdout(), "implemented audit")
			return nil`,
		1)
	require.NotEqual(t, string(scaffold), implemented)
	require.NoError(t, os.WriteFile(scaffoldPath, []byte(implemented), 0o644))

	require.NoError(t, gen.Generate())
	got, err := os.ReadFile(scaffoldPath)
	require.NoError(t, err)
	assert.Equal(t, implemented, string(got), "implemented novel scaffold must survive an in-place stub pass")
	assert.NotContains(t, string(got), "TODO: implement novel feature")

}

func TestGeneratorRenamesPatternPackCommandCollidingWithNovelCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pattern-pack-collision")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store: true,
		Sync:  true,
		Workflows: []string{
			"workflows/pm_orphans.go.tmpl",
		},
	}
	gen.NovelFeatures = []NovelFeature{{
		Name:        "List orphans",
		Command:     "orphans",
		Description: "List orphaned work.",
		Example:     "pattern-pack-collision-pp-cli orphans",
	}}
	require.NoError(t, gen.Generate())

	orphans := readGeneratedFile(t, outputDir, "internal", "cli", "pm_orphans.go")
	assert.Contains(t, orphans, `Use:   "pattern-pack-collision-orphans"`)
	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "newPatternPackCollisionOrphansCmd(flags)")
	assert.Contains(t, root, "newNovelOrphansCmd(flags)")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "pattern_pack_collision_test.go"), []byte(`package cli

import (
	"io"
	"testing"
)

func TestPatternPackAndNovelCommandsAreBothReachable(t *testing.T) {
	root := RootCmd()
	seen := map[string]int{}
	for _, command := range root.Commands() {
		seen[command.Name()]++
	}
	if seen["orphans"] != 1 || seen["pattern-pack-collision-orphans"] != 1 {
		t.Fatalf("root command names = %#v, want one novel orphans and one renamed pattern-pack command", seen)
	}
	root.SetArgs([]string{"orphans", "--help"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("novel orphans command is not executable: %v", err)
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPatternPackAndNovelCommandsAreBothReachable")
}

func TestGeneratorRenamesPatternPackConstructorWhenThreeWayNamesCollide(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("three-way-collision")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"health": {
			Description: "API health endpoint",
			Endpoints: map[string]spec.Endpoint{
				"get":    {Method: "GET", Path: "/health", Description: "Health"},
				"status": {Method: "GET", Path: "/health/status", Description: "Health status"},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:    true,
		Sync:     true,
		Insights: []string{"insights/health_score.go.tmpl"},
	}
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Health command",
		Command:     "health",
		Description: "Show health information.",
		Example:     "three-way-collision-pp-cli health",
	}}
	require.NoError(t, gen.Generate())

	insight := readGeneratedFile(t, outputDir, "internal", "cli", "health_score.go")
	assert.Contains(t, insight, "func newThreeWayCollisionHealthCmd(flags *rootFlags) *cobra.Command")
	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newThreeWayCollisionHealthCmd(flags))")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestGeneratorNormalizesBareFlagNovelFeatureExample(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bareflag")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Inspect state",
			Command:     "inspect",
			Description: "Inspect current state.",
			Example:     "--json",
		},
	}
	require.NoError(t, gen.Generate())

	inspect := readGeneratedFile(t, outputDir, "internal", "cli", "inspect.go")
	assert.Contains(t, inspect, `Example:     "  bareflag-pp-cli inspect --json"`)
	assert.NotContains(t, inspect, `Example:     "--json"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorPreservesQuotedNovelFeatureExampleArguments(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("quoted")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Inspect state",
			Command:     "inspect",
			Description: "Inspect current state.",
			Example:     `quoted-pp-cli inspect --query "weekly digest" --json`,
		},
	}
	require.NoError(t, gen.Generate())

	inspect := readGeneratedFile(t, outputDir, "internal", "cli", "inspect.go")
	assert.Contains(t, inspect, `Example:     "  quoted-pp-cli inspect --query 'weekly digest' --json"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorSanitizesNovelFeatureCommandStubFilenames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("suffixnovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Wake Windows",
			Command:     "wake-windows",
			Description: "Wake Windows hosts.",
			Example:     "suffixnovel-pp-cli wake-windows --json",
		},
		{
			Name:        "Wake Windowsill",
			Command:     "wake-windowsill",
			Description: "Wake windowsill devices.",
			Example:     "suffixnovel-pp-cli wake-windowsill --json",
		},
	}
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "wake_windows_cmd.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "wake_windows_cmd_test.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "wake_windows.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "wake_windowsill.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "wake_windowsill_test.go"))

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelWakeWindowsCmd(flags))")
	assert.Contains(t, root, "addNovelCommandIfAbsent(rootCmd, newNovelWakeWindowsillCmd(flags))")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorMigratesLegacyNovelFeatureBuildTagFilenames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacynovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	cliDir := filepath.Join(outputDir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "wake_windows.go"), []byte(`package cli

func newNovelWakeWindowsCmd(flags *rootFlags) {}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "wake_windows_test.go"), []byte(`package cli
`), 0o644))

	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Wake Windows",
		Command:     "wake-windows",
		Description: "Wake Windows hosts.",
		Example:     "legacynovel-pp-cli wake-windows --json",
	}}
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(cliDir, "wake_windows.go"))
	require.NoFileExists(t, filepath.Join(cliDir, "wake_windows_test.go"))
	require.FileExists(t, filepath.Join(cliDir, "wake_windows_cmd.go"))
	require.FileExists(t, filepath.Join(cliDir, "wake_windows_cmd_test.go"))
}

func TestGeneratorOmitsExampleWhenNovelFeatureHasNoExample(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("noexample")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Audit cache",
			Command:     "audit",
			Description: "Audit local cache state.",
			// No Example: a feature without research example must emit no
			// Cobra Example field (never `Example: ""`).
		},
	}
	require.NoError(t, gen.Generate())

	audit := readGeneratedFile(t, outputDir, "internal", "cli", "audit.go")
	assert.Contains(t, audit, `Use:         "audit"`)
	assert.NotContains(t, audit, "Example:")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorEmitsBoundCtxHelperForNovelCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("timeoutnovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Sibling client scan",
			Command:     "scan",
			Description: "Scan via a sibling client.",
			Example:     "timeoutnovel-pp-cli scan --json",
		},
	}
	require.NoError(t, gen.Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.Contains(t, helpers, "func boundCtx(parent context.Context, flags *rootFlags) (context.Context, context.CancelFunc)")
	assert.Contains(t, helpers, "return context.WithTimeout(parent, flags.timeout)")

	var runtimeTest strings.Builder
	runtimeTest.WriteString(`package cli

import (
	"context"
	"testing"
	"time"
)

func TestBoundCtxAppliesRootTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel := boundCtx(parent, &rootFlags{timeout: 25 * time.Millisecond})
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("boundCtx did not apply a deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("deadline already expired")
	}
}

func TestBoundCtxNoopsWithoutTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel := boundCtx(parent, &rootFlags{})
	defer cancel()
	if ctx != parent {
		t.Fatalf("boundCtx should return the parent context when timeout is unset")
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "bound_ctx_runtime_test.go"), []byte(runtimeTest.String()), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorSkipsNovelFeatureWiringForAbsorbedEndpointCollisions(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("make")
	apiSpec.Resources = map[string]spec.Resource{
		"scenarios": {
			Description: "Manage scenarios",
			Endpoints: map[string]spec.Endpoint{
				"get-qrcode": {Method: "GET", Path: "/scenarios/{id}/qrcode", Description: "Get scenario QR code"},
				"list":       {Method: "GET", Path: "/scenarios", Description: "List scenarios"},
				"run":        {Method: "POST", Path: "/scenarios/{id}/run", Description: "Run scenario"},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Blocking scenario run",
			Command:     "scenarios run --wait",
			Description: "Run a scenario and wait for completion.",
			Example:     "make-pp-cli scenarios run scenario-123 --wait",
		},
		{
			Name:        "Cross-team scenario list",
			Command:     "scenarios list --all-teams",
			Description: "List scenarios across every team.",
			Example:     "make-pp-cli scenarios list --all-teams",
		},
		{
			Name:        "Scenario QR watcher",
			Command:     "scenarios get-qrcode --watch",
			Description: "Watch a scenario QR code until it changes.",
			Example:     "make-pp-cli scenarios get-qrcode scenario-123 --watch",
		},
		{
			Name:        "Scenario health",
			Command:     "scenarios health --limit 10",
			Description: "Summarize scenario health.",
			Example:     "make-pp-cli scenarios health --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "scenarios.go")
	assert.Contains(t, parent, "cmd.AddCommand(newScenariosListCmd(flags))")
	assert.Contains(t, parent, "cmd.AddCommand(newScenariosRunCmd(flags))")
	assert.Contains(t, parent, "addNovelCommandIfAbsent(cmd, newNovelScenariosHealthCmd(flags))")
	assert.NotContains(t, parent, "newNovelScenariosGetQrcodeCmd")
	assert.NotContains(t, parent, "newNovelScenariosListCmd")
	assert.NotContains(t, parent, "newNovelScenariosRunCmd")

	health := readGeneratedFile(t, outputDir, "internal", "cli", "scenarios_health.go")
	assert.Contains(t, health, `TODO: implement novel feature %q", "scenarios health"`)
	requireGeneratedCompiles(t, outputDir)

	require.NoError(t, gen.Generate())
	parent = readGeneratedFile(t, outputDir, "internal", "cli", "scenarios.go")
	assert.Contains(t, parent, "addNovelCommandIfAbsent(cmd, newNovelScenariosHealthCmd(flags))")
	assert.NotContains(t, parent, "newNovelScenariosGetQrcodeCmd")
	assert.NotContains(t, parent, "newNovelScenariosListCmd")
	assert.NotContains(t, parent, "newNovelScenariosRunCmd")
}

func TestGeneratorSkipsNovelFeatureWiringForExistingCommandFileCollisions(t *testing.T) {
	apiSpec := minimalSpec("existingnovel")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "items_audit.go"), []byte(`package cli

import "github.com/spf13/cobra"

func newItemsAuditCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{Use: "audit"}
}
`), 0o644))

	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Bulk export",
			Command:     "export --format json",
			Description: "Export data with extra filtering.",
			Example:     "existingnovel-pp-cli export --format json",
		},
		{
			Name:        "Item audit",
			Command:     "items audit --dry-run",
			Description: "Audit items from a hand-authored child command.",
			Example:     "existingnovel-pp-cli items audit --dry-run",
		},
	}
	stderr, err := captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.Contains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go without expected constructor newNovelItemsAuditCmd; skipping novel stub`)
	assert.NotContains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go; leaving existing file unchanged`)

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newExportCmd(flags))")
	assert.NotContains(t, root, "newNovelExportCmd")

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.NotContains(t, parent, "newNovelItemsAuditCmd")
	requireGeneratedCompiles(t, outputDir)

	stderr, err = captureNovelFeatureStderr(t, gen.Generate)
	require.NoError(t, err)
	assert.Contains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go without expected constructor newNovelItemsAuditCmd; skipping novel stub`)
	assert.NotContains(t, stderr, `warning: novel feature command "items audit" maps to existing internal/cli/items_audit.go; leaving existing file unchanged`)
	root = readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newExportCmd(flags))")
	assert.NotContains(t, root, "newNovelExportCmd")
	parent = readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.NotContains(t, parent, "newNovelItemsAuditCmd")
	requireGeneratedCompiles(t, outputDir)
}

func captureNovelFeatureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	stderrSwapMu.Lock()
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		os.Stderr = oldErr
		stderrSwapMu.Unlock()
	}()
	callErr := fn()
	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return string(data), callErr
}

func TestGeneratorWiresNovelChildrenUnderPromotedResource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promonovel")
	apiSpec.Resources = map[string]spec.Resource{
		"qr": {
			Description: "Manage QR codes",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/qr/{id}", Description: "Get QR code"},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "QR watcher",
			Command:     "qr --watch",
			Description: "Watch the promoted QR command.",
			Example:     "promonovel-pp-cli qr qr-123 --watch",
		},
		{
			Name:        "QR health",
			Command:     "qr health --limit 10",
			Description: "Summarize QR health.",
			Example:     "promonovel-pp-cli qr health --limit 10",
		},
	}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "rootCmd.AddCommand(newQrPromotedCmd(flags))")
	assert.NotContains(t, root, "newNovelQrCmd")

	promoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_qr.go")
	assert.Contains(t, promoted, "addNovelCommandIfAbsent(cmd, newNovelQrHealthCmd(flags))")
	health := readGeneratedFile(t, outputDir, "internal", "cli", "qr_health.go")
	assert.Contains(t, health, `TODO: implement novel feature %q", "qr health"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratorSkipsNovelFeatureStubsWhenNoCommandPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("stubless")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:        "Flag-only feature",
		Command:     "--global-search",
		Description: "A cross-cutting flag should not emit a fake command.",
	}}
	require.NoError(t, gen.Generate())

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NotContains(t, root, "newNovel")
	_, err := os.Stat(filepath.Join(outputDir, "internal", "cli", "global_search.go"))
	assert.True(t, os.IsNotExist(err))
}

func TestNovelFeatureScaffoldDryRunEmitsEnvelope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("noveldryrun")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Inspect item",
			Command:     "inspect <id>",
			Description: "Inspect one item.",
			Example:     "noveldryrun-pp-cli inspect item-123",
		},
		{
			Name:        "Audit cache",
			Command:     "audit",
			Description: "Audit local cache state.",
			Example:     "noveldryrun-pp-cli audit",
		},
	}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	inspect := readGeneratedFile(t, outputDir, "internal", "cli", "inspect.go")
	audit := readGeneratedFile(t, outputDir, "internal", "cli", "audit.go")
	for name, src := range map[string]string{"inspect.go": inspect, "audit.go": audit} {
		require.Contains(t, src, "return writeDryRun(cmd.OutOrStdout(), flags,", "%s must report its dry-run short-circuit", name)
		require.NotContains(t, src, "if dryRunOK(flags) {\n\t\t\t\treturn nil\n", "%s still has a silent dry-run return", name)
	}
	dryIdx := strings.Index(inspect, "if dryRunOK(flags) {")
	helpIdx := strings.Index(inspect, "if len(args) == 0 {")
	require.GreaterOrEqual(t, dryIdx, 0)
	require.GreaterOrEqual(t, helpIdx, 0)
	assert.Less(t, dryIdx, helpIdx, "dry-run short-circuit must precede the positional help gate")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	for _, tc := range []struct {
		name   string
		args   []string
		action string
	}{
		{name: "flag-only", args: []string{"audit", "--dry-run", "--json"}, action: "audit"},
		{name: "positional without args", args: []string{"inspect", "--dry-run", "--json"}, action: "inspect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _ := runGeneratedBinary(t, binaryPath, tc.args...)
			var envelope struct {
				DryRun bool   `json:"dry_run"`
				Action string `json:"action"`
				Would  string `json:"would"`
			}
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope), "stdout: %q", stdout)
			require.True(t, envelope.DryRun)
			require.Equal(t, tc.action, envelope.Action)
			require.NotEmpty(t, envelope.Would)
		})
	}
}

func TestGeneratorNovelFeatureHelpGuardRequiresPositionalUse(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novelargs")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Inspect item",
			Command:     "inspect <id>",
			Description: "Inspect one item.",
			Example:     "novelargs-pp-cli inspect item-123 --format json",
		},
		{
			Name:        "Metric report",
			Command:     "report",
			Description: "Report metrics selected by flags.",
			Example:     "novelargs-pp-cli report --metric latency",
		},
		{
			Name:        "Audit",
			Command:     "audit",
			Description: "Audit local cache state.",
			Example:     "novelargs-pp-cli audit",
		},
		{
			Name:        "Filter",
			Command:     "filter --state [active|inactive]",
			Description: "Search items, filtered by flag.",
			Example:     "novelargs-pp-cli filter --state active",
		},
	}
	require.NoError(t, gen.Generate())

	inspect := readGeneratedFile(t, outputDir, "internal", "cli", "inspect.go")
	assert.Contains(t, inspect, `Use:         "inspect <id>"`)
	assert.Contains(t, inspect, "if len(args) == 0 {")
	assert.Contains(t, inspect, "return cmd.Help()")

	report := readGeneratedFile(t, outputDir, "internal", "cli", "report.go")
	assert.NotContains(t, report, "return cmd.Help()")
	assert.Contains(t, report, "// validate required flags here")
	assert.Contains(t, report, "if dryRunOK(flags) {")
	assert.Contains(t, report, `return writeDryRun(cmd.OutOrStdout(), flags, "report")`)
	assert.Contains(t, report, `TODO: implement novel feature %q", "report"`)

	audit := readGeneratedFile(t, outputDir, "internal", "cli", "audit.go")
	assert.NotContains(t, audit, "return cmd.Help()")
	assert.Contains(t, audit, "// validate required flags here")
	assert.Contains(t, audit, "if dryRunOK(flags) {")
	assert.Contains(t, audit, `return writeDryRun(cmd.OutOrStdout(), flags, "audit")`)
	assert.Contains(t, audit, `TODO: implement novel feature %q", "audit"`)

	// A bracket/angle placeholder inside a flag-value hint is NOT a positional
	// (#2592 regression guard): no args-based Help guard, and the flag-value
	// hint must not leak into the cobra Use string.
	filter := readGeneratedFile(t, outputDir, "internal", "cli", "filter.go")
	assert.NotContains(t, filter, "return cmd.Help()")
	assert.Contains(t, filter, "// validate required flags here")
	assert.NotContains(t, filter, "[active|inactive]")
}

func TestGeneratorNovelFeatureParentShortHasNoTODO(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("novelparent")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Snapshot diff",
			Command:     "snapshot diff",
			Description: "Compare two snapshots.",
			Example:     "novelparent-pp-cli snapshot diff before after",
		},
		{
			Name:        "Snapshot list",
			Command:     "snapshot list",
			Description: "List snapshots.",
			Example:     "novelparent-pp-cli snapshot list",
		},
		{
			Name:        "Single command",
			Command:     "single",
			Description: "A single-segment novel command.",
			Example:     "novelparent-pp-cli single",
		},
	}
	require.NoError(t, gen.Generate())

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "snapshot.go")
	assert.Contains(t, parent, `Short:       "Work with snapshot"`)
	assert.Contains(t, parent, `Example:     "  novelparent-pp-cli snapshot diff before after"`)
	assert.NotContains(t, parent, `Short:       "TODO`)
	assert.NotContains(t, parent, `subcommands:`)

	single := readGeneratedFile(t, outputDir, "internal", "cli", "single.go")
	assert.Contains(t, single, `Short:       "A single-segment novel command."`)
	assert.NotContains(t, single, `subcommands:`)
}

func TestGeneratorLeavesAuthoredParentGroupExampleUntouched(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("authoredparent")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "After-call workspace",
			Command:     "afk",
			Description: "Manage after-call knowledge workflows.",
			Example:     "authoredparent-pp-cli afk --help",
		},
		{
			Name:        "After-call history",
			Command:     "afk history",
			Description: "Read call history from the local ledger.",
			Example:     "authoredparent-pp-cli afk history --limit 5",
		},
	}
	require.NoError(t, gen.Generate())

	parent := readGeneratedFile(t, outputDir, "internal", "cli", "afk.go")
	assert.Contains(t, parent, `Example:     "  authoredparent-pp-cli afk --help"`)
	assert.NotContains(t, parent, `Example:     "  authoredparent-pp-cli afk history --limit 5"`)
	requireGeneratedCompiles(t, outputDir)
}
