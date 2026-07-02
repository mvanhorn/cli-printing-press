package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateJournalEmitsFiles verifies the U4 invocation-journal
// templates land under internal/learn/ when the spec opts into the
// self-learning loop, and that root.go carries the two journal write
// concerns: the parse-failure enrichment (failed flag + suggestion)
// and the post-ExecuteC outcome write. Content assertions here; the
// compile + behavior contract rides the existing
// TestGenerateLearnPackageCompilesAndTests run of the emitted
// internal/learn tests (journal_test.go is part of that package).
func TestGenerateJournalEmitsFiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("journal-emit")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "journal-emit-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	for _, rel := range []string{
		"internal/learn/journal.go",
		"internal/learn/journal_test.go",
	} {
		_, err := os.Stat(filepath.Join(outputDir, rel))
		require.NoError(t, err, "expected emitted file %s", rel)
	}

	journalBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "learn", "journal.go"))
	require.NoError(t, err)
	journal := string(journalBody)
	for _, want := range []string{
		// Dated append-only segments + (segment, byte) offset contract.
		"journalSegmentPrefix",
		`"journal-"`,
		"type JournalOffset struct",
		"func ReadJournalFrom(",
		"func LoadJournalOffset(",
		"func StoreJournalOffset(",
		// Append path + fail-open entry point used by root.go.
		"func AppendJournalEntry(",
		"func JournalInvocation(",
		// Session key: env var else parent-pid lineage.
		"JOURNAL_EMIT_LEARN_SESSION",
		// Silence switches (verify/dogfood handled via cliutil).
		"JOURNAL_EMIT_NO_LEARN",
		"JOURNAL_EMIT_LEARN_NO_CAPTURE",
		"cliutil.IsVerifyEnv()",
		"cliutil.IsDogfoodEnv()",
		// Redaction: token-bearing flag names never carry values.
		"redacted",
	} {
		require.Contains(t, journal, want, "journal.go missing %q", want)
	}
	// Caps must delete whole segments, never truncate in place: no
	// truncation calls anywhere in the journal implementation.
	require.NotContains(t, journal, "os.Truncate", "journal cleanup must delete whole segments, never truncate in place")
	require.NotContains(t, journal, ".Truncate(", "journal cleanup must delete whole segments, never truncate in place")

	rootBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootBody)
	for _, want := range []string{
		// Post-run write site: code after ExecuteC returns, one site
		// for all outcomes (Cobra PostRun hooks are skipped on RunE
		// error and must not be used).
		"rootCmd.ExecuteC()",
		"learn.JournalInvocation(",
		// Parse-failure enrichment at the suggestFlag site.
		"journalFailedFlag",
		"journalSuggestedFlag",
		// Verb chain from the resolved command; os.Args fallback
		// matches registered command names only.
		"journalVerbChain(",
	} {
		require.Contains(t, root, want, "root.go missing %q", want)
	}
	require.NotContains(t, root, "PersistentPostRun", "journal write must not ride a Cobra PostRun hook")
	require.NotContains(t, root, "PostRunE", "journal write must not ride a Cobra PostRun hook")
}

// TestGenerateJournalGatedOff verifies the journal files and the
// root.go wiring do not emit when Learn.Enabled is false — the
// non-learn shape keeps the plain rootCmd.Execute() path.
func TestGenerateJournalGatedOff(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("journal-gated")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "journal-gated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "learn", "journal.go"))
	require.True(t, os.IsNotExist(err), "internal/learn/journal.go must not exist when Learn.Enabled=false")

	rootBody, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootBody)
	require.NotContains(t, root, "JournalInvocation", "non-learn root.go must carry no journal wiring")
	require.NotContains(t, root, "journalVerbChain", "non-learn root.go must carry no journal wiring")
	require.True(t, strings.Contains(root, "rootCmd.Execute()") || strings.Contains(root, "rootCmd.ExecuteC()"),
		"non-learn root.go must still execute the command tree")
}
