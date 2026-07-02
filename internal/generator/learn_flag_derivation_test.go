package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateFlagDerivation_EmitsFiles verifies the U7 flag-correction
// derivation templates land under internal/learn/ when the spec opts
// into the self-learning loop, and that root.go wires exactly one
// post-run derivation call after the journal write. Content assertions
// here; the compile + behavior contract rides
// TestGenerateFlagDerivationEmittedTestsRun below.
func TestGenerateFlagDerivation_EmitsFiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("flag-derive")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "flag-derive-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	for _, rel := range []string{
		"internal/learn/derive.go",
		"internal/learn/derive_test.go",
	} {
		_, err := os.Stat(filepath.Join(outputDir, rel))
		require.NoError(t, err, "expected emitted file %s", rel)
	}

	deriveBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "learn", "derive.go"))
	require.NoError(t, err)
	derive := string(deriveBody)
	for _, want := range []string{
		// The derivation entry point and its narrow store dependency.
		"func DeriveFlagCorrections(",
		"type CandidateStore interface",
		"store.CandidateClassFlagAlias",
		// Bounded tail scan rides the journal's persisted cursor.
		"LoadJournalOffset()",
		"ReadJournalFrom(",
		"StoreJournalOffset(",
		// The documented pairing window.
		"flagCorrectionWindow",
		// Skipped under the same switches the journal honors.
		"JournalCaptureDisabled()",
	} {
		require.Contains(t, derive, want, "derive.go missing %q", want)
	}
	// The derivation pass consumes the journal read-only: it must never
	// append entries of its own (derive-on-derive noise).
	require.NotContains(t, derive, "AppendJournalEntry(", "derivation must never write journal entries")
	require.NotContains(t, derive, "JournalInvocation(", "derivation must never write journal entries")

	rootBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootBody)
	for _, want := range []string{
		// One post-run call site, sharing the journal's deferred site.
		"deriveFlagCorrections(",
		"learn.DeriveFlagCorrections(",
		// Learn-family commands never trigger derivation.
		"learnFamilyCommands",
		// The whole-tree flag existence probe for the pairing rule.
		"commandTreeHasFlag(",
	} {
		require.Contains(t, root, want, "root.go missing %q", want)
	}
	// Derivation runs after the invocation's own journal entry lands so
	// the entry it just wrote is visible to the tail scan.
	journalCallAt := strings.Index(root, "journalInvocation(&flags")
	deriveCallAt := strings.Index(root, "deriveFlagCorrections(&flags")
	require.GreaterOrEqual(t, journalCallAt, 0, "root.go missing journalInvocation call")
	require.GreaterOrEqual(t, deriveCallAt, 0, "root.go missing deriveFlagCorrections call")
	require.Greater(t, deriveCallAt, journalCallAt, "derivation must run after the journal write in the deferred post-ExecuteC site")
	require.Equal(t, 1, strings.Count(root, "deriveFlagCorrections(&flags"), "exactly one derivation call site")
}

// TestGenerateFlagDerivation_GatedOff verifies the derivation files and
// root.go wiring do not emit when Learn.Enabled is false.
func TestGenerateFlagDerivation_GatedOff(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("flag-derive-off")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "flag-derive-off-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "learn", "derive.go"))
	require.True(t, os.IsNotExist(err), "internal/learn/derive.go must not exist when Learn.Enabled=false")

	rootBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootBody)
	require.NotContains(t, root, "deriveFlagCorrections", "non-learn root.go must carry no derivation wiring")
	require.NotContains(t, root, "DeriveFlagCorrections", "non-learn root.go must carry no derivation wiring")
}

// TestGenerateFlagDerivationEmittedTestsRun drives the emitted
// derivation tests (real SQLite store + real journal segments in a
// temp state dir) through `go test` scoped to the derivation suite, so
// a template regression that produces shape-valid but wrong Go fails
// here rather than in a printed CLI.
func TestGenerateFlagDerivationEmittedTestsRun(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted derivation suite skipped in -short mode")
	}

	apiSpec := minimalSpec("flag-derive-run")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "flag-derive-run-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "-run", "^TestDeriveFlagCorrections", "-count=1", "./internal/learn/")
}
