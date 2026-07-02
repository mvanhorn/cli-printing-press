package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// generateLearnRecallCandidatesCLI generates a learn-enabled CLI with
// the store surface, the shape the recall candidates section spans
// (candidates store + recall envelope + learnings control commands).
func generateLearnRecallCandidatesCLI(t *testing.T, name string) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	return outputDir
}

// TestGenerateRecallCandidates_EmitsEnvelopeSurface verifies the U6
// recall-envelope candidates section lands in the emitted learn
// package: a structurally separate `candidates` array (omitted when
// empty), a bounded open-status SELECT on the hot path, next-action
// steps composed against the stamped binary name so the confirm step
// byte-matches the registered `learnings confirm` command, and the
// stable candidates_present warning code in match.go's vocabulary.
func TestGenerateRecallCandidates_EmitsEnvelopeSurface(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnRecallCandidatesCLI(t, "recand")

	recall := readEmitted(t, outputDir, "internal", "learn", "recall.go")
	for _, want := range []string{
		// structurally separate envelope section, key omitted when empty
		// (gofmt aligns the struct field, so the type and tag are
		// asserted separately from the field name)
		"type Candidate struct",
		"`json:\"candidates,omitempty\"`",
		"`json:\"next_action\"`",
		// the installed binary name is stamped so next_action steps are
		// copy-exact for agents
		`candidateCommandBinary = "recand-pp-cli"`,
		// the confirm step targets the literal registered command names
		"learnings confirm %d",
		// one bounded query on the hot path, open rows only
		"status = 'open'",
		"LIMIT",
		"candidateScanLimit",
		// cap and warning wiring
		"candidateSurfaceCap",
		"TopWarningCandidatesPresent",
	} {
		require.Contains(t, recall, want, "emitted recall.go must contain %q", want)
	}

	match := readEmitted(t, outputDir, "internal", "learn", "match.go")
	require.Contains(t, match, `TopWarningCandidatesPresent = "candidates_present"`,
		"emitted match.go must pin the stable candidates_present code")

	// Byte-exact parity with the control surface: the command the
	// next_action step names must actually be registered.
	cli := readEmitted(t, outputDir, "internal", "cli", "learnings_candidates.go")
	require.Contains(t, cli, `Use:   "confirm <id>"`,
		"the confirm step's target command must be registered under learnings")
}

// TestGenerateRecallCandidates_EmittedRecallTestsPass compiles the
// emitted module and runs the emitted recall + warning-vocabulary test
// suite against real SQLite: candidate surfacing with two-step
// next-actions, the cap-3 deterministic ranking, confirmed/rejected/
// expired exclusion, the byte-stable empty envelope, the
// capture-disabled read-only surfacing, and the pinned warning codes.
func TestGenerateRecallCandidates_EmittedRecallTestsPass(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted recall candidates surface skipped in -short mode")
	}

	outputDir := generateLearnRecallCandidatesCLI(t, "recandrun")
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/learn",
		"-run", "TestRecall_|TestEnvelopeWarningCodesStable", "-count=1")
}
