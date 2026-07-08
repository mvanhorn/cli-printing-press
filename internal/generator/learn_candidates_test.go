package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateLearnCandidates_EmitsStoreLifecycle verifies the U5
// candidate-store template lands beside learnings.go/playbooks.go in
// the emitted internal/store package with the full state-machine
// surface, the single-statement signature upsert, and structural
// quarantine (the candidates store never touches the verified
// search_learnings table).
func TestGenerateLearnCandidates_EmitsStoreLifecycle(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cand-store")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "cand-store-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	candGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "candidates.go"))
	require.NoError(t, err)
	src := string(candGo)
	for _, want := range []string{
		"func (s *Store) DeriveCandidate(",
		"func (s *Store) ListCandidates(",
		"func (s *Store) GetCandidate(",
		"func (s *Store) ConfirmCandidate(",
		"func (s *Store) ConfirmCandidateWithPlaybook(",
		"func (s *Store) ConfirmCandidateWithPlaybookNote(",
		"func (s *Store) RejectCandidate(",
		"func (s *Store) ExpireCandidates(",
		"func (s *Store) PurgeCandidates(",
		// cross-process race contract: one statement resolves the
		// same-signature race into one insert + one sightings bump
		"ON CONFLICT(derivation_signature)",
		// tombstone contract: rejected signatures never re-derive
		"ErrConfirmedFlagAliasReject",
	} {
		require.Contains(t, src, want, "emitted candidates store must contain %q", want)
	}
	// Quarantine is structural: the candidate lifecycle never reads or
	// writes the verified learnings table.
	require.NotContains(t, src, "search_learnings",
		"the candidates store must never touch search_learnings")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "store", "candidates_test.go"))
	require.NoError(t, err, "emitted store must include candidates_test.go")
}

// TestGenerateLearnCandidates_EmitsControlSurface verifies the
// `learnings candidates|confirm|reject|purge` control commands land in
// internal/cli, registered under the existing learnings group, with
// the MCP annotations and typed-exit-code declarations from R10/KTD12,
// and that teach.go carries the R11 teach-promotion hook.
func TestGenerateLearnCandidates_EmitsControlSurface(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cand-cli")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "cand-cli-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	cliGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "learnings_candidates.go"))
	require.NoError(t, err)
	src := string(cliGo)
	for _, want := range []string{
		`Use:   "candidates"`,
		`Use:   "confirm <id>"`,
		`Use:   "reject <id>"`,
		`Use:   "purge"`,
		"func registerLearningsCandidateCommands(",
		"func promoteCandidateOnTeach(",
		// R10: rejecting a confirmed flag correction names the repair path
		"playbook amend",
		// intentional non-zero exits are declared for verify
		"pp:typed-exit-codes",
	} {
		require.Contains(t, src, want, "emitted learnings_candidates.go must contain %q", want)
	}
	// `learnings candidates` is read-only; purge is operator-only.
	require.Contains(t, src, `"mcp:read-only": "true"`)
	require.Contains(t, src, `"mcp:hidden": "true"`)
	confirmIdx := strings.Index(src, `Use:   "confirm <id>"`)
	require.Greater(t, confirmIdx, 0)
	confirmBlock := src[confirmIdx:min(confirmIdx+1200, len(src))]
	require.Contains(t, confirmBlock, `"mcp:local-write": "true"`,
		"confirm must carry local-write MCP hints")
	require.Contains(t, src, "confirmAndMaterializeCandidate(",
		"confirm must materialize and mark the candidate confirmed in one store transaction")
	require.NotContains(t, src, "materializeCandidate(",
		"confirm must not regress to separate materialize + confirm writes")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "learnings_candidates_test.go"))
	require.NoError(t, err, "emitted cli must include learnings_candidates_test.go")

	teachGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "teach.go"))
	require.NoError(t, err)
	teachSrc := string(teachGo)
	require.Contains(t, teachSrc, "promoteCandidateOnTeach(",
		"teach.go must invoke the R11 teach-promotion hook")
	require.Contains(t, teachSrc, "registerLearningsCandidateCommands(cmd, flags)",
		"the control commands must register under the existing learnings group")
}

// TestGenerateLearnCandidates_GatedByLearnEnabled pins that a
// learn-disabled spec emits neither the candidates store nor the
// control commands.
func TestGenerateLearnCandidates_GatedByLearnEnabled(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cand-gated")
	// Post-flip, learn is on by default; `Enabled = false` is a
	// documented no-op (a plain bool can't distinguish it from unset),
	// so the opt-out is `Disabled = true`.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "cand-gated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	for _, gone := range []string{
		filepath.Join("internal", "store", "candidates.go"),
		filepath.Join("internal", "store", "candidates_test.go"),
		filepath.Join("internal", "cli", "learnings_candidates.go"),
		filepath.Join("internal", "cli", "learnings_candidates_test.go"),
	} {
		_, err := os.Stat(filepath.Join(outputDir, gone))
		require.True(t, os.IsNotExist(err), "learn-disabled spec must not emit %s", gone)
	}
}

// TestGenerateLearnCandidatesLifecycleRunsWithRaceDetector compiles the
// emitted CLI and runs the candidate store-lifecycle and control-command
// tests under -race: derive/bump, tombstone no-op, confirm
// materialization at confidence 2, playbook rollback, the flag-correction
// no-reject rule, TTL expiry + reopen, teach promotion, and the
// same-signature goroutine race.
func TestGenerateLearnCandidatesLifecycleRunsWithRaceDetector(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("emitted candidate lifecycle race test skipped in -short mode")
	}

	apiSpec := minimalSpec("cand-race")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "cand-race-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "./internal/store",
		"-run", "TestDeriveCandidate|TestConfirmCandidate|TestRejectCandidate|TestExpireCandidates|TestPurgeCandidates",
		"-race", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/cli",
		"-run", "TestLearningsCandidates|TestLearningsConfirm|TestLearningsReject|TestLearningsPurge|TestTeachPromotes",
		"-race", "-count=1")
}
