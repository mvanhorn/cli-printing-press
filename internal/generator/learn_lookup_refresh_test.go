package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// readEmitted loads one emitted file from an already-generated output
// tree and returns its contents as a string.
func readEmitted(t *testing.T, outputDir string, rel ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(outputDir, filepath.Join(rel...)))
	require.NoError(t, err)
	return string(body)
}

// generateLearnRefreshCLI generates a learn-enabled CLI with the store
// + sync vision surface, the shape the lookup-refresh loop spans
// (lookups store + recall + match + sync command).
func generateLearnRefreshCLI(t *testing.T, name string) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	return outputDir
}

// TestGenerateLookupStoreEmitsSyncedPriorityTier verifies the emitted
// lookups store carries the four-tier source priority ladder
// (taught > inferred > synced > seeded) in BOTH hardcoded CASE sites —
// Lookup and LookupAll. Updating only one function silently inverts
// the intended priority for the other read path, so the count of each
// tier line must be exactly 2.
func TestGenerateLookupStoreEmitsSyncedPriorityTier(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnRefreshCLI(t, "lkrefresh-tier")
	got := readEmitted(t, outputDir, "internal", "learn", "lookups", "store.go")

	for _, tier := range []string{
		"WHEN 'taught' THEN 0",
		"WHEN 'inferred' THEN 1",
		"WHEN 'synced' THEN 2",
		"WHEN 'seeded' THEN 3",
		"ELSE 4",
	} {
		if n := strings.Count(got, tier); n != 2 {
			t.Errorf("lookups/store.go: priority tier %q appears %d times, want 2 (Lookup AND LookupAll)", tier, n)
		}
	}

	// The scanner and the recall-miss capture both live in the lookups
	// package so the sync command and the recall path share one owner
	// for the learn_recall_misses table.
	for _, want := range []string{
		"/internal/cliutil",
		"func RefreshFromSynced(",
		"func RecordMisses(",
		"cliutil.IsVerifyEnv() || cliutil.IsDogfoodEnv()",
		"learn_recall_misses",
		"DefaultSyncedPerKindCap",
		`SourceSynced   = "synced"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lookups/store.go missing %q", want)
		}
	}
}

// TestGenerateRecallEmitsMissCaptureAndRefreshWarning verifies the
// emitted recall path records unresolvable entities as lookup misses
// and that the stateless run-sync warning code ships in match.go's
// stable warning vocabulary.
func TestGenerateRecallEmitsMissCaptureAndRefreshWarning(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnRefreshCLI(t, "lkrefresh-recall")

	recall := readEmitted(t, outputDir, "internal", "learn", "recall.go")
	for _, want := range []string{
		"lookups.RecordMisses(",
		"TopWarningLookupRefreshAvailable",
	} {
		if !strings.Contains(recall, want) {
			t.Errorf("learn/recall.go missing %q", want)
		}
	}

	match := readEmitted(t, outputDir, "internal", "learn", "match.go")
	if !strings.Contains(match, `TopWarningLookupRefreshAvailable = "lookup_refresh_available"`) {
		t.Errorf("learn/match.go missing stable warning code lookup_refresh_available")
	}
}

// TestGenerateSyncWiresPostSyncLookupRefresh verifies the sync command
// explicitly invokes the lookup-refresh scanner after a successful
// sync. The PersistentPreRunE learn hook skips `sync` via
// shouldSkipLearnHook, so this explicit call is the only wiring point;
// losing it silently disables the staleness-heal loop.
func TestGenerateSyncWiresPostSyncLookupRefresh(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnRefreshCLI(t, "lkrefresh-sync")
	got := readEmitted(t, outputDir, "internal", "cli", "sync.go")

	for _, want := range []string{
		"refreshLookupsFromSyncedStore(",
		"lookups.RefreshFromSynced(",
		"learn.ResourceEntitiesFromJSON(",
		"!cliutil.IsDogfoodEnv()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cli/sync.go missing %q", want)
		}
	}
}

// TestGenerateSyncLookupRefreshGatedOff verifies a non-learn CLI's
// sync command carries none of the lookup-refresh surface.
func TestGenerateSyncLookupRefreshGatedOff(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("lkrefresh-off")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "lkrefresh-off-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	got := readEmitted(t, outputDir, "internal", "cli", "sync.go")
	for _, banned := range []string{"RefreshFromSynced", "refreshLookupsFromSyncedStore", "/internal/learn"} {
		if strings.Contains(got, banned) {
			t.Errorf("cli/sync.go must not contain %q when Learn.Enabled=false", banned)
		}
	}
}

// TestGenerateLookupRefreshCompilesAndTests drives the emitted module
// through a full build plus the emitted lookups-package test suite, so
// the cross-file contract (sync.go -> lookups.RefreshFromSynced,
// recall.go -> lookups.RecordMisses) is proven at compile level and
// the template-shipped scanner/priority scenario tests run against
// real SQLite. The wider internal/learn test suite (which includes the
// recall-side miss-capture scenarios) is owned by
// TestGenerateLearnPackageCompilesAndTests.
func TestGenerateLookupRefreshCompilesAndTests(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted lookup-refresh surface skipped in -short mode")
	}

	outputDir := generateLearnRefreshCLI(t, "lkrefresh-built")
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/learn/lookups/...")
}
