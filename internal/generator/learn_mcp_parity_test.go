package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandBlock returns a bounded window of src starting at the given Use
// marker, so annotation assertions stay scoped to one Cobra command
// definition instead of the whole file.
func commandBlock(t *testing.T, src, useMarker string) string {
	t.Helper()
	idx := strings.Index(src, useMarker)
	require.GreaterOrEqual(t, idx, 0, "command marker %q not found in emitted source", useMarker)
	end := min(idx+1500, len(src))
	return src[idx:end]
}

// TestGenerateLearnMCPParity_SharedProtocolSource verifies the R16
// recall-first protocol is generated from one shared source: the emitted
// internal/learn/protocol.go exports RecallFirstProtocol with the full
// protocol content, and BOTH agent surfaces (the MCP context tool and the
// CLI agent-context command) consume that constant rather than embedding
// parallel prose.
func TestGenerateLearnMCPParity_SharedProtocolSource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-proto")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "parity-proto-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	protoSrc := readEmitted(t, outputDir, "internal", "learn", "protocol.go")
	require.Contains(t, protoSrc, "const RecallFirstProtocol",
		"protocol must be an exported constant so both surfaces can reference one source")
	for _, want := range []string{
		// recall-first
		"Recall first",
		`recall "<question>" --agent`,
		// empty-store short-circuit: cold CLIs must not tax every query
		"Empty-store short-circuit",
		"skip recall for the rest of this session",
		// candidates are try-then-confirm, never facts
		"never facts",
		"next_action",
		"learnings confirm",
		"learnings reject",
		// teach contract: PII rule
		"identifiers stripped",
		// never re-teach a surfaced candidate
		"Never re-teach",
	} {
		require.Contains(t, protoSrc, want, "protocol content must contain %q", want)
	}
	require.NotContains(t, protoSrc, "recall --query",
		"protocol must name recall's positional query shape, not a non-existent --query flag")
	// `learnings stats` does not exist yet; the protocol must not name it.
	require.NotContains(t, protoSrc, "learnings stats")

	// Drift test: both surfaces consume the same exported constant, and
	// neither embeds its own copy of the protocol prose.
	mcpSrc := readEmitted(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, "learn.RecallFirstProtocol",
		"handleContext must consume the shared protocol constant")
	require.Contains(t, mcpSrc, `"learn_protocol"`,
		"handleContext JSON must expose the protocol under learn_protocol")
	require.NotContains(t, mcpSrc, "Empty-store short-circuit",
		"the MCP surface must reference the constant, not duplicate the prose")

	agentCtxSrc := readEmitted(t, outputDir, "internal", "cli", "agent_context.go")
	require.Contains(t, agentCtxSrc, "learn.RecallFirstProtocol",
		"agent-context must consume the shared protocol constant")
	require.Contains(t, agentCtxSrc, `json:"learn_protocol"`,
		"agent-context JSON must expose the protocol under learn_protocol")
	require.NotContains(t, agentCtxSrc, "Empty-store short-circuit",
		"the CLI surface must reference the constant, not duplicate the prose")

	requireGeneratedCompiles(t, outputDir)
}

// TestGenerateLearnMCPParity_ProtocolGatedByLearn pins that a learn-disabled
// spec emits neither the protocol source nor any learn_protocol reference on
// either surface, and that the gated emission still compiles.
func TestGenerateLearnMCPParity_ProtocolGatedByLearn(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-gated")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "parity-gated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "learn", "protocol.go"))
	require.True(t, os.IsNotExist(err), "learn-disabled spec must not emit protocol.go")

	mcpSrc := readEmitted(t, outputDir, "internal", "mcp", "tools.go")
	require.NotContains(t, mcpSrc, "learn_protocol")
	require.NotContains(t, mcpSrc, "internal/learn")

	agentCtxSrc := readEmitted(t, outputDir, "internal", "cli", "agent_context.go")
	require.NotContains(t, agentCtxSrc, "learn_protocol")
	require.NotContains(t, agentCtxSrc, "internal/learn")

	requireGeneratedCompiles(t, outputDir)
}

// TestGenerateLearnMCPParity_LocalWriteAnnotations pins the R17
// mcp:local-write tier: teach-playbook and playbook amend carry the
// annotation (emitting destructive=false + openWorld=false through the
// walker), recall keeps mcp:read-only, and the honestly-destructive
// learnings forget / learnings reject carry no local-write hints.
func TestGenerateLearnMCPParity_LocalWriteAnnotations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-annot")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "parity-annot-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	// The walker plumbing: classify defines the annotation tier, the walker
	// maps it to destructive=false + openWorld=false without readOnly.
	classifySrc := readEmitted(t, outputDir, "internal", "mcp", "cobratree", "classify.go")
	require.Contains(t, classifySrc, `LocalWriteAnnotation = "mcp:local-write"`)
	require.Contains(t, classifySrc, "func isMCPLocalWrite(")

	walkerSrc := readEmitted(t, outputDir, "internal", "mcp", "cobratree", "walker.go")
	require.Contains(t, walkerSrc, "isMCPLocalWrite(cmd)")
	require.Contains(t, walkerSrc, "WithOpenWorldHintAnnotation(false)")

	// teach-playbook and playbook amend are local-store-only writers.
	tpSrc := readEmitted(t, outputDir, "internal", "cli", "teach_playbook.go")
	// gofmt aligns map values, so match the key and value separately.
	teachPlaybookBlock := commandBlock(t, tpSrc, `Use:   "teach-playbook"`)
	require.Regexp(t, `"mcp:local-write":\s+"true"`, teachPlaybookBlock,
		"teach-playbook must carry the local-write annotation")
	amendBlock := commandBlock(t, tpSrc, `Use:   "amend"`)
	require.Regexp(t, `"mcp:local-write":\s+"true"`, amendBlock,
		"playbook amend must carry the local-write annotation")

	// recall stays read-only despite its telemetry-class event insert.
	teachSrc := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	recallBlock := commandBlock(t, teachSrc, `Use:   "recall`)
	require.Contains(t, recallBlock, `"mcp:read-only": "true"`,
		"recall must keep mcp:read-only")

	// learnings forget deletes materialized data: honest destructive
	// semantics, no local-write softening.
	forgetBlock := commandBlock(t, teachSrc, `Use:   "forget`)
	require.NotContains(t, forgetBlock, "mcp:local-write",
		"learnings forget must not carry local-write hints")

	// learnings reject tombstones and can delete materialized rows: same.
	candSrc := readEmitted(t, outputDir, "internal", "cli", "learnings_candidates.go")
	confirmBlock := commandBlock(t, candSrc, `Use:   "confirm <id>"`)
	require.Regexp(t, `"mcp:local-write":\s+"true"`, confirmBlock,
		"learnings confirm must carry the local-write annotation")
	rejectBlock := commandBlock(t, candSrc, `Use:   "reject <id>"`)
	require.NotContains(t, rejectBlock, "mcp:local-write",
		"learnings reject must not carry local-write hints")
}

func TestGenerateLearnMCPParity_StatsReadOnlyAllowsTelemetryPrune(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-stats")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "parity-stats-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	statsSrc := readEmitted(t, outputDir, "internal", "cli", "learnings_stats.go")
	statsBlock := commandBlock(t, statsSrc, `Use:   "stats"`)
	require.Contains(t, statsBlock, `"mcp:read-only": "true"`)
	require.Contains(t, statsBlock, "PruneLearnEvents(0, 0)",
		"read-only MCP stats may prune telemetry-class local events")
}

// TestGenerateTeachPlaybookInlineJSON verifies the R16 inline playbook
// write path: `teach-playbook --playbook-json` accepts the playbook body
// with no file on disk, is mutually exclusive with --playbook-file, and
// leaves the existing --playbook-file path untouched. The behavioral
// contract is exercised by the emitted CLI tests.
func TestGenerateTeachPlaybookInlineJSON(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-inline")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "parity-inline-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	tpSrc := readEmitted(t, outputDir, "internal", "cli", "teach_playbook.go")
	require.Contains(t, tpSrc, `"playbook-json"`)
	require.Contains(t, tpSrc, "--playbook-json and --playbook-file are mutually exclusive")
	require.Contains(t, tpSrc, `"playbook-file"`, "existing file path must remain")

	tpTestSrc := readEmitted(t, outputDir, "internal", "cli", "teach_playbook_test.go")
	require.Contains(t, tpTestSrc, "TestTeachPlaybook_InlineJSON")
	require.Contains(t, tpTestSrc, "TestTeachPlaybook_InlineAndFileMutuallyExclusive")

	if testing.Short() {
		t.Skip("emitted teach-playbook behavior tests skipped in -short mode")
	}
	runGoCommand(t, outputDir, "test", "./internal/cli",
		"-run", "TestTeachPlaybook", "-count=1")
}

// TestGenerateLearnMCPParity_CodeOrchestrationExposesLearnTools is the U12
// empirical check: a spec with mcp.orchestration="code" must still expose
// the learn loop (teach/recall/learnings) as MCP tools alongside the
// search/execute pair. The registration is runtime (the cobratree walker
// mirrors the Cobra tree at server start), so the static contract pinned
// here is that the emitted RegisterTools calls BOTH the code-orchestration
// pair AND the unconditional cobratree walker + context tool, and that the
// walker does not classify the learn commands as framework/hidden.
func TestGenerateLearnMCPParity_CodeOrchestrationExposesLearnTools(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parity-orch")
	apiSpec.Learn.Enabled = true
	apiSpec.MCP.Orchestration = "code"
	outputDir := filepath.Join(t.TempDir(), "parity-orch-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	mcpSrc := readEmitted(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, "RegisterCodeOrchestrationTools(s)",
		"code orchestration must register the search/execute pair")
	require.Contains(t, mcpSrc, "cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)",
		"the runtime Cobra-tree mirror must still run under code orchestration so learn commands surface as MCP tools")
	require.Contains(t, mcpSrc, "learn.RecallFirstProtocol",
		"the context tool must carry the learn protocol under code orchestration too")

	// The walker exposes every user-facing command that is not framework,
	// hidden, or endpoint-annotated. Pin that the learn loop commands are
	// none of those in the emitted tree.
	classifySrc := readEmitted(t, outputDir, "internal", "mcp", "cobratree", "classify.go")
	for _, name := range []string{`"teach"`, `"recall"`, `"learnings"`, `"teach-playbook"`, `"playbook"`} {
		require.NotContains(t, classifySrc, name+": true",
			"learn command %s must not be in frameworkCommands", name)
	}
	teachSrc := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	teachBlock := commandBlock(t, teachSrc, `Use:   "teach"`)
	require.NotContains(t, teachBlock, "mcp:hidden")
	recallBlock := commandBlock(t, teachSrc, `Use:   "recall`)
	require.NotContains(t, recallBlock, "mcp:hidden")
}
