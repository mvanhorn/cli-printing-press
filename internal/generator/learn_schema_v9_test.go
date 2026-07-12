package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// generateLearnStore renders a minimal spec with the requested learn state
// and returns the emitted store.go source plus the output dir for follow-up
// reads (README, emitted tests).
func generateLearnStore(t *testing.T, name string, learnEnabled bool) (string, string) {
	t.Helper()

	apiSpec := minimalSpec(name)
	if learnEnabled {
		apiSpec.Learn.Enabled = true
	} else {
		// Post-flip: opt out so this test exercises the non-learn shape it asserts.
		apiSpec.Learn.Disabled = true
	}
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	return string(storeGo), outputDir
}

// TestLearnSchemaV9_EnabledEmitsV9WithCandidateAndEventTables pins the v9
// schema bump: a learn-enabled store advances StoreSchemaVersion to 9 and
// carries the learn_candidates and learn_events tables (with their CHECK
// constraints and indexes) as additive CREATE IF NOT EXISTS migrations.
func TestLearnSchemaV9_EnabledEmitsV9WithCandidateAndEventTables(t *testing.T) {
	t.Parallel()

	src, _ := generateLearnStore(t, "learn-v9-enabled", true)

	require.Contains(t, src, "const StoreSchemaVersion = 9")
	require.NotContains(t, src, "const StoreSchemaVersion = 8")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS learn_candidates",
		"class TEXT NOT NULL CHECK(class IN ('flag_alias','playbook_candidate'))",
		"derivation_signature TEXT NOT NULL UNIQUE",
		"status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','confirmed','rejected','expired'))",
		"CREATE INDEX IF NOT EXISTS idx_learn_candidates_status ON learn_candidates(status)",
		"CREATE INDEX IF NOT EXISTS idx_learn_candidates_family ON learn_candidates(query_family)",
		"CREATE TABLE IF NOT EXISTS learn_events",
		"event TEXT NOT NULL CHECK(event IN ('recall_hit','recall_miss','recall_playbook_hit','teach','teach_playbook','amend','forget','candidate_confirmed','candidate_rejected'))",
		"surface TEXT CHECK(surface IN ('cli','mcp'))",
		"CREATE INDEX IF NOT EXISTS idx_learn_events_event_ts ON learn_events(event, ts)",
	} {
		require.Contains(t, src, want, "learn-enabled store must emit %q", want)
	}
}

// TestLearnSchemaV9_FTSContentPinIsUnconditional pins the decouple:
// resourcesFTSContentSchemaVersion stays 4 in BOTH learn shapes. The old
// conditional 8 rode the learn bump by accident and forced a full FTS
// content rewrite on every v4-v7 learn store open; with the pin, v4 and v8
// stores opened by a v9 binary take the additive-only migration path.
func TestLearnSchemaV9_FTSContentPinIsUnconditional(t *testing.T) {
	t.Parallel()

	enabled, _ := generateLearnStore(t, "learn-v9-fts-enabled", true)
	require.Contains(t, enabled, "const resourcesFTSContentSchemaVersion = 4")
	require.NotContains(t, enabled, "const resourcesFTSContentSchemaVersion = 8")

	disabled, _ := generateLearnStore(t, "learn-v9-fts-disabled", false)
	require.Contains(t, disabled, "const resourcesFTSContentSchemaVersion = 4")
	require.Contains(t, disabled, "const StoreSchemaVersion = 4")
	for _, gone := range []string{"learn_candidates", "learn_events"} {
		require.NotContains(t, disabled, gone,
			"learn-disabled spec must not emit the %s migration", gone)
	}
}

// TestLearnSchemaV9_ReadmeDocumentsOneWayStamp verifies the generated README
// tells users the version stamp is one-way: an older binary refuses a store
// already stamped at the newer version.
func TestLearnSchemaV9_ReadmeDocumentsOneWayStamp(t *testing.T) {
	t.Parallel()

	_, outputDir := generateLearnStore(t, "learn-v9-readme", true)
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readme), "schema version stamp is one-way",
		"README learn section must document the one-way schema version stamp")
	require.Contains(t, string(readme), "older", "README learn section must warn that older binaries refuse the upgraded store")
}

// TestLearnSchemaV9_EmittedStoreTestsPass runs the emitted store package
// tests under -race. This executes the emitted migration scenarios for real:
// fresh v9 open with both new tables, the v4->v9 additive open with the FTS
// content preserved (no rewrite), the v8->v9 additive upgrade with learn
// data intact, and the newer-store refusal.
func TestLearnSchemaV9_EmittedStoreTestsPass(t *testing.T) {
	t.Parallel()

	_, outputDir := generateLearnStore(t, "learn-v9-emitted", true)
	runGoCommand(t, outputDir, "test", "-race", "-short", "./internal/store/...")
}
