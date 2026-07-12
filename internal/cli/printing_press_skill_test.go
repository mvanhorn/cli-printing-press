package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintingPressSkillSideEffectNarrativeGuidance(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "Step 1 of `quickstart` should usually be verify-safe")
	require.Contains(t, content, "Use `<cli> doctor --dry-run` as step 1")
	require.Contains(t, content, "reports each as an `UNSUPPORTED` warning instead of executing it")
	require.Contains(t, content, "These warnings do not fail strict aggregation")
	require.Contains(t, content, "Non-side-effect unsupported examples still fail strict mode")
}

func TestPrintingPressSkillMCPEnrichmentGate(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
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

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
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

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
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

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "Exception for LAN-only / mDNS-discovered APIs")
	require.Contains(t, content, "http://localhost:<port>")
	require.Contains(t, content, "http://127.0.0.1:<port>")
	require.Contains(t, content, "http://[::1]:<port>")
	require.Contains(t, content, "SSDP / mDNS-discovered")
	require.Contains(t, content, "Reason: lan-only-no-global-url")
	require.Contains(t, content, "Then proceed to Phase 2")
	require.Contains(t, content, "do not use this carve-out for normal public/cloud origins such as `https://api.example.com`")
	require.Contains(t, content, "those still run the reachability probe and decision matrix below")
}

func TestPrintingPressSkillRebuildsStaleRepoLocalBinary(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)
	setupChecks, err := os.ReadFile("../../skills/printing-press/references/setup-checks.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "_source_press_version()")
	require.Contains(t, content, "_rebuild_local_press_bin_if_stale()")
	require.Contains(t, content, "[local-binary-stale] local build v$_local_v is older than source v$_source_v")
	require.Contains(t, content, "go build -o ./cli-printing-press ./cmd/cli-printing-press")
	require.Contains(t, content, "[local-binary-rebuilt] rebuilt $_scope_dir/cli-printing-press")
	require.Contains(t, content, "hooks can be absent or")
	require.NotContains(t, content, "always newer than the go-install version")

	setupContent := string(setupChecks)
	require.Contains(t, setupContent, "[local-binary-stale]` / `[local-binary-rebuilt]")
	require.Contains(t, setupContent, "The repo-mode local binary was older than the checked-out source version")
}
