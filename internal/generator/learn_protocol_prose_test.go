package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedProseCarriesLearnProtocol pins the U14 prose contract on
// the three generated docs: the SKILL.md protocol is candidate-aware
// with an unconditional teach and PII discipline, AGENTS.md carries the
// loop's own success definition (kill criteria), and README.md names
// self-capture and the stats readout. Asserts rendered output, not
// template source, so template-variable or gating regressions fail here.
func TestGeneratedProseCarriesLearnProtocol(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-prose")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-prose-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	for _, want := range []string{
		// Candidates branch: try-then-confirm, never a parallel protocol.
		"candidates_present",
		"try-then-confirm, never facts",
		"`learnings confirm <id>` only after the trial verified the behavior",
		"NEVER re-teach something recall surfaced as a candidate",
		// Unconditional teach anchor.
		"Teaching is unconditional",
		// PII discipline: structural query, identifiers stripped, warn-not-block.
		"identifiers stripped",
		"warns, but does not block",
		// Empty-store short-circuit.
		"skip recall for the rest of this session",
		// Graceful degradation on older binaries.
		"driving an older binary",
		// Lookup refresh warning names sync.
		"lookup_refresh_available",
		// Self-measurement readout exists.
		"learn-prose-pp-cli learnings stats",
	} {
		assert.Contains(t, skill, want, "SKILL.md learn protocol missing %q", want)
	}
	// The old bookkeeping burdens are gone.
	assert.NotContains(t, skill, ">5 calls", "SKILL.md must drop the >5 calls playbook gate")
	assert.NotContains(t, skill, "more than 5 tool calls", "SKILL.md must drop the call-count playbook gate")

	agents := readGeneratedFile(t, outputDir, "AGENTS.md")
	for _, want := range []string{
		"try-then-confirm, never facts",
		"Never re-teach something recall surfaced as a candidate",
		"teaching is unconditional",
		// Kill criteria: local-only measurement, numeric denominator.
		"Measurement is local-only",
		"`learn_events`",
		"50+ recall events",
		"not earning its keep",
		// One-way schema stamp note stays consistent with (not verbatim
		// from) the README's.
		"schema stamp is one-way",
		"learnings stats",
	} {
		assert.Contains(t, agents, want, "AGENTS.md learn section missing %q", want)
	}

	readme := readGeneratedFile(t, outputDir, "README.md")
	for _, want := range []string{
		"self-captures",
		"learnings candidates",
		"learnings stats",
		"schema version stamp is one-way",
	} {
		assert.Contains(t, readme, want, "README.md self-learning section missing %q", want)
	}
}

// TestGeneratedProseOmitsLearnProtocolWhenDisabled verifies the
// {{if .Learn.Enabled}} gates still hold after the U14 rewrite: none of
// the learn-loop prose renders into a learn-disabled print.
func TestGeneratedProseOmitsLearnProtocolWhenDisabled(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-prose-off")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-prose-off-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	assert.NotContains(t, skill, "## Automatic learning")
	assert.NotContains(t, skill, "candidates_present")
	assert.NotContains(t, skill, "Teaching is unconditional")

	agents := readGeneratedFile(t, outputDir, "AGENTS.md")
	assert.NotContains(t, agents, "## Self-Learning Loop")
	assert.NotContains(t, agents, "50+ recall events")
	assert.NotContains(t, agents, "learnings stats")

	readme := readGeneratedFile(t, outputDir, "README.md")
	assert.NotContains(t, readme, "### Self-learning loop")
	assert.NotContains(t, readme, "learnings stats")
}

// TestGeneratedLearnProtocolOmitsSyncAdviceForExplicitStoreOnlyPlan pins the
// rendered SKILL.md contract for an explicit store-only plan over a syncable
// API: the data layer exists, but the generated sync command does not.
func TestGeneratedLearnProtocolOmitsSyncAdviceForExplicitStoreOnlyPlan(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-prose-store-only")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-prose-store-only-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	require.True(t, gen.hasDataLayer(), "explicit store-only plan must retain its data layer")
	require.False(t, gen.hasGeneratedSyncImplementation(), "explicit store-only plan must not gain generated sync")
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))

	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	assert.NotContains(t, skill, "lookup_refresh_available",
		"SKILL.md must not advise running a sync command that was not generated")
}
