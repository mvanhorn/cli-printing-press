package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

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

func TestPrintingPressSkillSQLiteNovelCommandsGuardMissingMirror(t *testing.T) {
	t.Parallel()

	content := readPrintingPressSkill(t)
	require.Contains(t, content, "For SQLite-backed novel commands only")
	require.Contains(t, content, "live execution without `--dry-run`, before the user has run `sync`")
	require.Contains(t, content, "os.Stat(dbPath); os.IsNotExist(statErr)")
	require.Contains(t, content, "flags.asJSON || flags.agent")
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

	expected := []string{
		"01-preflight.md",
		"02-run-initialization.md",
		"03-resolve-and-reuse.md",
		"04-research-brief.md",
		"05-pre-browser-sniff-auth-intelligence.md",
		"06-browser-sniff-gate.md",
		"07-crowd-sniff-gate.md",
		"08-ecosystem-absorb-gate.md",
		"09-api-reachability-gate.md",
		"10-generate.md",
		"11-build-the-goat.md",
		"12-shipcheck.md",
		"13-sync-param-drop-gate.md",
		"14-agentic-skill-review.md",
		"15-readme-skill-agents-correctness-audit.md",
		"16-agentic-output-review.md",
		"17-local-code-review.md",
		"18-dogfood-testing.md",
		"19-polish.md",
		"20-promote-and-archive.md",
		"21-next-steps.md",
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

	expected := []string{
		"02-run-initialization",
		"03-resolve-and-reuse",
		"04-research-brief",
		"05-pre-browser-sniff-auth-intelligence",
		"06-browser-sniff-gate",
		"07-crowd-sniff-gate",
		"08-ecosystem-absorb-gate",
		"09-api-reachability-gate",
		"10-generate",
		"11-build-the-goat",
		"12-shipcheck",
		"13-sync-param-drop-gate",
		"14-agentic-skill-review",
		"15-readme-skill-agents-correctness-audit",
		"16-agentic-output-review",
		"17-local-code-review",
		"18-dogfood-testing",
		"19-polish",
		"20-promote-and-archive",
		"21-next-steps",
	}

	preflight, err := os.ReadFile("../../skills/printing-press/phases/01-preflight.md")
	require.NoError(t, err)
	require.NotContains(t, string(preflight), "phase-receipt enter")
	require.Contains(t, string(preflight), "Phase receipts begin only after Phase 2")

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
		require.Equal(t, 1, strings.Count(content, "phase-receipt complete"), "%s must record completion exactly once", phase)
		require.Contains(t, content, `--phase "`+phase+`"`)
		require.NotContains(t, content, "--phase-file")
		require.NotContains(t, content, "--next")
	}

	router, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)
	require.Contains(t, string(router), "Receipts own sequencing only")
	require.Contains(t, string(router), "never copied into manuscripts")
	require.Contains(t, string(router), "Never write secrets, credentials")
	require.Contains(t, string(router), "`previous` receipt")
	require.Contains(t, string(router), "`phase_receipt_log`")
	require.Contains(t, string(router), "restart pointer")
}
