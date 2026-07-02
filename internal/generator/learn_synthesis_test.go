package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateSynthesisEmitsFiles verifies the U10 teach-time playbook
// synthesis templates land under internal/learn/ when the spec opts
// into the self-learning loop, and that teach.go carries the one
// additive post-teach hook composed AFTER candidate promotion
// (promotion first; synthesis only reaches a family that still lacks
// a playbook). Content assertions here; the compile + behavior
// contract rides the existing TestGenerateLearnPackageCompilesAndTests
// run of the emitted internal/learn tests (synthesize_test.go is part
// of that package).
func TestGenerateSynthesisEmitsFiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("synth-emit")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "synth-emit-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	for _, rel := range []string{
		"internal/learn/synthesize.go",
		"internal/learn/synthesize_test.go",
	} {
		_, err := os.Stat(filepath.Join(outputDir, rel))
		require.NoError(t, err, "expected emitted file %s", rel)
	}

	synthBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "learn", "synthesize.go"))
	require.NoError(t, err)
	synth := string(synthBody)
	for _, want := range []string{
		// Entry point the teach hook calls.
		"func SynthesizePlaybookCandidate(",
		// Episode reading consumes the committed journal API — never a
		// parallel reader.
		"ReadJournalFrom(",
		// Harness silence: verify/dogfood/capture switches leave no
		// candidate rows.
		"JournalCaptureDisabled()",
		// Quarantine: the product is a playbook_candidate row via the
		// candidate store, and a rejected-signature tombstone is
		// respected (the observation is dropped, not resurrected).
		"store.CandidateClassPlaybookCandidate",
		"DeriveCandidate(",
		"store.CandidateStatusRejected",
		// Dedup gate: a family already covered by a playbook (including
		// one the promotion hook just materialized) synthesizes nothing.
		"GetPlaybookByFamily(",
		// Payload matches the confirm materializer's contract
		// (playbookCandidatePayload in the emitted internal/cli).
		`json:"query_family"`,
		`json:"playbook_json"`,
		`json:"notes_text,omitempty"`,
		// The synthesized playbook records its step count.
		"ExpectedToolCalls:",
		// The learn-command family never becomes a step.
		`"recall"`,
		`"teach"`,
		`"teach-playbook"`,
		`"teach-pattern"`,
		`"teach-lookup"`,
		`"learnings"`,
		`"playbook"`,
		// Framework skip names mirror learnHookSkipList in root.go.tmpl;
		// the mirror comment names the canonical source.
		"learnHookSkipList",
		`"agent-context"`,
		`"doctor"`,
		`"completion"`,
	} {
		require.Contains(t, synth, want, "synthesize.go missing %q", want)
	}
	// Materialization is the confirm command's job: synthesis must never
	// write learning_playbooks rows itself.
	require.NotContains(t, synth, "UpsertPlaybook(", "synthesis must not materialize playbooks; that is `learnings confirm`'s job")
	require.NotContains(t, synth, "AppendPlaybookNotes(", "synthesis must not materialize playbook notes; that is `learnings confirm`'s job")

	teachBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "teach.go"))
	require.NoError(t, err)
	teach := string(teachBody)
	for _, want := range []string{
		"learn.SynthesizePlaybookCandidate(",
		"learn.JournalSessionKey()",
	} {
		require.Contains(t, teach, want, "teach.go missing %q", want)
	}
	// Composition order: the shared-key-space promotion hook runs
	// first; synthesis is invoked after it so a promoted candidate's
	// materialized playbook short-circuits synthesis.
	promoAt := strings.Index(teach, "promoteCandidateOnTeach(")
	synthAt := strings.Index(teach, "learn.SynthesizePlaybookCandidate(")
	require.Greater(t, promoAt, -1, "teach.go must keep the candidate promotion hook")
	require.Greater(t, synthAt, promoAt, "synthesis hook must run after candidate promotion in teach.go")
}

// TestGenerateSynthesisGatedOff verifies the synthesis files do not
// emit when Learn.Enabled is false — the non-learn shape carries no
// synthesis surface.
func TestGenerateSynthesisGatedOff(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("synth-gated")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "synth-gated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "learn", "synthesize.go"))
	require.True(t, os.IsNotExist(err), "internal/learn/synthesize.go must not exist when Learn.Enabled=false")
}
