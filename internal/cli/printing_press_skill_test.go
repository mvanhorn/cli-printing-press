package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func readPrintingPressSkill(t *testing.T) string {
	t.Helper()

	paths, err := filepath.Glob("../../skills/printing-press/phases/*.md")
	require.NoError(t, err)
	paths = append([]string{"../../skills/printing-press/SKILL.md"}, paths...)

	var content strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		content.Write(data)
		content.WriteByte('\n')
	}
	return content.String()
}

func TestPrintingPressSkillSideEffectNarrativeGuidance(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "Step 1 of `quickstart` should usually be verify-safe")
	require.Contains(t, content, "Use `<cli> doctor --dry-run` as step 1")
	require.Contains(t, content, "reports each as an `UNSUPPORTED` warning instead of executing it")
	require.Contains(t, content, "These warnings do not fail strict aggregation")
	require.Contains(t, content, "Non-side-effect unsupported examples still fail strict mode")
}

func TestPrintingPressSkillMCPEnrichmentGate(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "Mandatory >50 endpoint-tools confirmation")
	require.Contains(t, content, "info: applied Cloudflare MCP pattern")
	require.Contains(t, content, "does not require a blocking question")
	require.Contains(t, content, "This is the only count that selects the >50 automatic")
	require.Contains(t, content, "code orchestration will not shrink them")
	require.Contains(t, content, "This collapses typed endpoint mirrors, not the runtime command mirror")
	require.Contains(t, content, "covers the typed-endpoint surface")
	require.Contains(t, content, "cmd.Annotations[\"mcp:hidden\"]")
	require.Contains(t, content, "mcp.orchestration: endpoint-mirror")
	require.Contains(t, content, "x-mcp.orchestration: endpoint-mirror")
	require.Contains(t, content, "For OpenAPI input specs, declare these fields under `x-mcp:`")
	require.Contains(t, content, "internal-YAML `mcp:` block")
}

func TestPrintingPressSkillTranscendenceCollectorSliceInit(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "results := make([]yourRowType, 0, len(rawRows))")
	require.Contains(t, content, "empty marshals")
	require.NotContains(t, content, "var results []yourRowType")

	// The aggregation skeleton's other collector slices must use make() too, so
	// empty results marshal as [] not null across every emitted slice.
	require.Contains(t, content, "failures := make([]fetchFailure, 0)")
	require.Contains(t, content, "successfulItems := make([]yourEntryType, 0)")
	require.NotContains(t, content, "var failures []fetchFailure")
	require.NotContains(t, content, "var successfulItems []yourEntryType")
}

func TestPrintingPressSkillEmptyResultOutputContract(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "Empty results and output modes")
	require.Contains(t, content, "wantsHumanTable(cmd.OutOrStdout(), flags)")
	require.Contains(t, content, "printJSONFiltered(cmd.OutOrStdout(), rows, flags)")
	require.Contains(t, content, "printJSONFiltered(cmd.OutOrStdout(), make([]yourRowType, 0), flags)")
	require.Contains(t, content, "make([]yourRowType, 0)")
	require.NotContains(t, content, "if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly)")
	require.Contains(t, content, "// human-only empty-result prose;")
	for _, flag := range []string{"`--json`", "`--agent`", "`--csv`"} {
		require.Contains(t, content, flag)
	}

	machineIdx := strings.Index(content, "wantsHumanTable(cmd.OutOrStdout(), flags)")
	humanProseIdx := strings.Index(content, "// human-only empty-result prose;")
	require.GreaterOrEqual(t, machineIdx, 0)
	require.GreaterOrEqual(t, humanProseIdx, 0)
	require.Less(t, machineIdx, humanProseIdx,
		"machine output must be selected before human-only empty-result prose")
}

func TestPrintingPressSkillSQLiteNovelCommandsGuardMissingMirror(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "For SQLite-backed novel commands only")
	require.Contains(t, content, "live execution without `--dry-run`, before the user has run `sync`")
	require.Contains(t, content, "os.Stat(dbPath); os.IsNotExist(statErr)")
	require.Contains(t, content, "!wantsHumanTable(cmd.OutOrStdout(), flags)")
	require.Contains(t, content, "printJSONFiltered(cmd.OutOrStdout(), make([]yourRowType, 0), flags)")
	require.Contains(t, content, "The unconditional `return nil` is intentional")
	require.Contains(t, content, "store.OpenWithContext")

	guard := strings.Index(content, "os.Stat(dbPath); os.IsNotExist(statErr)")
	openStore := strings.Index(content, "store.OpenWithContext(ctx, dbPath)")
	require.NotEqual(t, -1, openStore, "full store.OpenWithContext(ctx, dbPath) call not found in SKILL.md")
	require.Less(t, guard, openStore, "missing-mirror guard should be shown before opening SQLite")
}

func TestPrintingPressSkillReachabilityGateAllowsLANOnlyCarveout(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "Exception for LAN-only / mDNS-discovered APIs")
	require.Contains(t, content, "http://localhost:<port>")
	require.Contains(t, content, "http://127.0.0.1:<port>")
	require.Contains(t, content, "http://[::1]:<port>")
	require.Contains(t, content, "SSDP / mDNS-discovered")
	require.Contains(t, content, "Reason: lan-only-no-global-url")
	require.Contains(t, content, "Then proceed to [Phase 2]")
	require.Contains(t, content, "do not use this carve-out for normal public/cloud origins such as `https://api.example.com`")
	require.Contains(t, content, "those still run the reachability probe and decision matrix below")
}

func TestPrintingPressSkillRebuildsStaleRepoLocalBinary(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	setupChecks, err := os.ReadFile("../../skills/printing-press/references/setup-checks.md")
	require.NoError(t, err)

	require.Contains(t, content, "_source_press_version()")
	require.Contains(t, content, "_rebuild_local_press_bin_if_stale()")
	require.Contains(t, content, "[local-binary-stale] local build v$_local_v is older than source v$_source_v")
	require.Contains(t, content, "go build -o ./cli-printing-press ./cmd/cli-printing-press")
	require.Contains(t, content, "[local-binary-rebuilt] rebuilt $_scope_dir/cli-printing-press")
	require.Contains(t, content, `"$PRINTING_PRESS_BIN" phase-receipt --help`)
	require.Contains(t, content, "binary lacks the phase-receipt helper required by this skill")
	require.Contains(t, content, "hooks can be absent or")
	require.NotContains(t, content, "always newer than the go-install version")

	setupContent := string(setupChecks)
	require.Contains(t, setupContent, "[local-binary-stale]` / `[local-binary-rebuilt]")
	require.Contains(t, setupContent, "The repo-mode local binary was older than the checked-out source version")
}

func TestPrintingPressSkillPhaseChainIntegrity(t *testing.T) {
	t.Parallel()

	// The phase files are the receipt phases plus preflight, which runs before a
	// ledger exists. Source the receipt phases from the binary so this on-disk
	// check stays pinned to the graph the state machine enforces.
	expected := []string{"01-preflight.md"}
	for _, phase := range pipeline.PrintingPressReceiptPhases() {
		expected = append(expected, phase+".md")
	}

	paths, err := filepath.Glob("../../skills/printing-press/phases/*.md")
	require.NoError(t, err)
	var found []string
	for _, path := range paths {
		found = append(found, filepath.Base(path))
	}
	require.Equal(t, expected, found, "phases/ must contain exactly the expected files in order")

	router, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	// The phase index is a table whose rows number and link each phase file.
	// Parse the rows in document order and require both the displayed step and
	// file link to match their canonical positions.
	rowPattern := regexp.MustCompile(`(?m)^\| ([0-9]+) \| \[phases/([^\]]+)\]\(phases/([^)]+)\) \|`)
	var indexRows []string
	for i, match := range rowPattern.FindAllStringSubmatch(string(router), -1) {
		require.Equal(t, strconv.Itoa(i+1), match[1], "phase index step must match its execution position")
		require.Equal(t, match[2], match[3], "phase index link text and target must agree")
		indexRows = append(indexRows, match[2])
	}
	require.Equal(t, expected, indexRows, "router phase index rows must list every phase file in execution order")

	for i, name := range expected {
		data, err := os.ReadFile(filepath.Join("../../skills/printing-press/phases", name))
		require.NoError(t, err)
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		last := lines[len(lines)-1]
		if i == len(expected)-1 {
			require.Equal(t, "Next: return to the router", last, "%s must close the chain", name)
		} else {
			require.Equal(t, "Next: phases/"+expected[i+1], last, "%s must point at the next phase", name)
		}
	}
}

func TestPrintingPressSkillPhaseReceiptsEnforceEveryHandoff(t *testing.T) {
	t.Parallel()

	// Source the canonical phase order from the binary so the skill files are
	// checked against the graph the state machine actually enforces.
	expected := pipeline.PrintingPressReceiptPhases()

	preflight, err := os.ReadFile("../../skills/printing-press/phases/01-preflight.md")
	require.NoError(t, err)
	require.NotContains(t, string(preflight), "phase-receipt enter")
	require.Contains(t, string(preflight), "Phase receipts begin only after `02-run-initialization`")

	// Completion-block count is 1 (canonical) plus one recorded `--next` command
	// per documented alternate, derived from the binary graph rather than a
	// third hand-maintained map.

	// Every `phase-receipt complete` occurrence in a phase file must be a full,
	// executable command, not a prose fragment: it names the ledger, the run, and
	// its own phase. This closes the hole where a bare substring match let a
	// non-executable snippet satisfy the contract.
	completeCommand := regexp.MustCompile(`phase-receipt complete[^\n]*`)

	for i, phase := range expected {
		path := filepath.Join("../../skills/printing-press/phases", phase+".md")
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		content := string(data)
		if i == 0 {
			require.Contains(t, content, `phase-receipt init \`)
			require.Contains(t, content, `--phase "02-run-initialization"`)
			continue
		}

		require.Equal(t, 1, strings.Count(content, "phase-receipt enter"), "%s must record entry exactly once", phase)

		wantBlocks := 1 + len(pipeline.PrintingPressAlternateNextPhases(phase))
		require.Equal(t, wantBlocks, strings.Count(content, "phase-receipt complete"), "%s must record exactly %d completion block(s)", phase, wantBlocks)

		occurrences := completeCommand.FindAllString(content, -1)
		require.Equal(t, wantBlocks, len(occurrences), "%s: every phase-receipt complete must be a single-line command", phase)
		for _, occurrence := range occurrences {
			require.Contains(t, occurrence, `--file "$PHASE_RECEIPT_LOG"`, "%s: complete command must name the ledger: %q", phase, occurrence)
			require.Contains(t, occurrence, `--run-id "$RUN_ID"`, "%s: complete command must name the run: %q", phase, occurrence)
			require.Contains(t, occurrence, `--phase "`+phase+`"`, "%s: complete command must name its own phase: %q", phase, occurrence)
		}

		alternates := pipeline.PrintingPressAlternateNextPhases(phase)
		if len(alternates) > 0 {
			for _, alt := range alternates {
				require.Contains(t, content, `--next "`+alt+`"`, "%s must record its documented alternate handoff to %s", phase, alt)
			}
		} else {
			require.NotContains(t, content, "--next")
		}
		require.Contains(t, content, `--phase "`+phase+`"`)
		require.NotContains(t, content, "--phase-file")
	}

	// The receipt contract itself lives in the reference the router points at, so
	// the always-loaded spine stays thin.
	receipts, err := os.ReadFile("../../skills/printing-press/references/phase-receipts.md")
	require.NoError(t, err)
	require.Contains(t, string(receipts), "Receipts own sequencing only")
	require.Contains(t, string(receipts), "never copied into manuscripts")
	require.Contains(t, string(receipts), "Never write secrets, credentials")
	require.Contains(t, string(receipts), "`previous` receipt")
	require.Contains(t, string(receipts), "`phase_receipt_log`")
	require.Contains(t, string(receipts), "restart pointer")
}

func TestPrintingPressSkillRouterStaysAThinSpine(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)
	router := string(data)

	require.Less(t, len(strings.Split(router, "\n")), 100, "SKILL.md must stay a thin always-loaded spine")
	require.Contains(t, router, "**Result:**")
	require.Contains(t, router, "**Next consumer:**")
	require.Contains(t, router, "**Done:**")
	require.Contains(t, router, "**Intent:**")
	require.Contains(t, router, "Use when the user says build a CLI")
	require.Contains(t, router, "**Never execute a phase from memory. When you enter a phase, Read its file from phases/ first.**")

	// Run resolution and the receipt contract are loaded on demand, not restated
	// in the router.
	require.Contains(t, router, "references/run-resolution.md")
	require.Contains(t, router, "references/phase-receipts.md")

	// The phase index is a bare Step | File table: per-phase purpose and gate
	// columns are the phase files' job.
	require.NotContains(t, router, "One-line purpose")
	require.NotContains(t, router, "Gates")
}

func TestPrintingPressSkillAgenticSkillReviewGateDoesNotSkipToDogfood(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/phases/14-agentic-skill-review.md")
	require.NoError(t, err)
	content := string(data)
	gateStart := strings.Index(content, "### Gate")
	require.Greater(t, gateStart, 0)
	rest := content[gateStart:]
	gateEnd := strings.Index(rest, "### Why agentic")
	require.Greater(t, gateEnd, 0)
	gate := rest[:gateEnd]
	require.Contains(t, gate, "15-readme-skill-agents-correctness-audit.md")
	require.NotContains(t, gate, "18-dogfood-testing.md")
}
