package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// generateLearnEventsCLI generates a learn-enabled CLI with the store
// surface, the shape the measurement layer spans (events store API +
// recall/teach/candidate instrumentation + `learnings stats`).
func generateLearnEventsCLI(t *testing.T, name string) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	return outputDir
}

// TestGenerateLearnEvents_EmitsMeasurementSurface verifies the U11
// measurement layer lands in the emitted tree: the events store API
// (best-effort insert, retention prune, forget cascade, stats
// aggregation with zero-division guards), the surface-detection seam,
// command-side instrumentation on recall/teach/forget/confirm/reject,
// the `learnings stats` command registered under the learnings group,
// and the teach-time PII warning.
func TestGenerateLearnEvents_EmitsMeasurementSurface(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnEventsCLI(t, "levents")

	events := readEmitted(t, outputDir, "internal", "store", "events.go")
	for _, want := range []string{
		// the events store API surface
		"func (s *Store) InsertLearnEvent(",
		"func (s *Store) PruneLearnEvents(",
		"func (s *Store) DeleteLearnEventsByFamilyHash(",
		"func (s *Store) LearnEventStats(",
		// retention defaults per R21
		"DefaultLearnEventMaxRows = 10000",
		"90 * 24 * time.Hour",
		// surface detection seam: env-var read + in-process setter the
		// MCP entrypoint can call (wired by a later unit)
		"func LearnEventSurface()",
		"func SetLearnEventSurface(",
		`learnSurfaceEnvVar = "LEVENTS_LEARN_SURFACE"`,
		// stats join by row id with the family-hash fallback for legacy rows
		"h.matched_row_id = t.matched_row_id",
		"h.query_family_hash = t.query_family_hash",
	} {
		require.Contains(t, events, want, "emitted store/events.go must contain %q", want)
	}

	recall := readEmitted(t, outputDir, "internal", "learn", "recall.go")
	for _, want := range []string{
		// row-id linkage: hits carry the search_learnings row id so the
		// command-side recall_hit event joins teach-to-reuse by row id
		"LearningID",
		// family hash + PII helpers exported for the command side
		"func FamilyHash(",
		"func ScanPII(",
	} {
		require.Contains(t, recall, want, "emitted learn/recall.go must contain %q", want)
	}

	teach := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	for _, want := range []string{
		// command-side instrumentation, best-effort by contract
		"recordRecallEvents",
		"store.LearnEventTeach",
		"store.LearnEventForget",
		// forget cascade half of R18
		"DeleteLearnEventsByFamilyHash",
		// PII guard on the teach path warns to stderr, never blocks
		"learn.ScanPII",
		// inline playbook JSON, mutually exclusive with --playbook-file
		`"playbook-json"`,
	} {
		require.Contains(t, teach, want, "emitted cli/teach.go must contain %q", want)
	}

	candidates := readEmitted(t, outputDir, "internal", "cli", "learnings_candidates.go")
	require.Contains(t, candidates, "store.LearnEventCandidateConfirmed",
		"confirm must record a candidate_confirmed event")
	require.Contains(t, candidates, "store.LearnEventCandidateRejected",
		"reject must record a candidate_rejected event")

	stats := readEmitted(t, outputDir, "internal", "cli", "learnings_stats.go")
	for _, want := range []string{
		`Use:   "stats"`,
		`"mcp:read-only": "true"`,
		"PruneLearnEvents(0, 0)",
		// the four headline metrics in the JSON contract
		`json:"recall_hit_rate"`,
		`json:"teach_to_reuse"`,
		`json:"playbook_resolution_rate"`,
		`json:"candidates_confirmed"`,
	} {
		require.Contains(t, stats, want, "emitted cli/learnings_stats.go must contain %q", want)
	}

	// One clean registration path: stats registers on the learnings
	// group in teach.go's newLearningsCmd.
	require.Contains(t, teach, "newLearningsStatsCmd(",
		"learnings stats must register on the learnings group")
}

// TestGenerateLearnEvents_EmittedStoreTestsPass compiles the emitted
// module and runs the emitted events-store suite against real SQLite
// under -race: insert validation + surface detection, retention prune
// (row cap drops oldest, age cutoff), forget cascade by family hash,
// stats aggregation (row-id join primary, family-hash fallback,
// playbook-amend subtraction, zero-division guards), and concurrent
// insert safety.
func TestGenerateLearnEvents_EmittedStoreTestsPass(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted learn events store skipped in -short mode")
	}

	outputDir := generateLearnEventsCLI(t, "leventsrun")
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-race", "./internal/store",
		"-run", "TestLearnEvent|TestPruneLearnEvents|TestDeleteLearnEventsByFamilyHash|TestLearnEventStats", "-count=1")
}

// TestGenerateLearnEvents_EmittedCLITestsPass runs the emitted CLI-side
// instrumentation suite under -race: the miss->teach->hit sequence with
// row-id linkage, alias-mediated reuse credit (family-hash-only join
// would miss it), insert-failure isolation (closed DB never fails
// recall), concurrent teach+recall, the PII stderr warning, the forget
// cascade, inline --playbook-json (and both-flags error), stats JSON
// with zero-division guards, and the confirm/reject event inserts.
func TestGenerateLearnEvents_EmittedCLITestsPass(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted learn events CLI surface skipped in -short mode")
	}

	outputDir := generateLearnEventsCLI(t, "levcli")
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-race", "./internal/cli",
		"-run", "TestLearnEvents_|TestLearningsStats_|TestTeachCommand_PII|TestTeachCommand_PlaybookJSON", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/learn",
		"-run", "TestRecall_HitCarriesLearningID|TestFamilyHash_|TestScanPII_", "-count=1")
}
