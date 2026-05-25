package regenmerge

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyLearnLoopEmissionFixture confirms that internal/learn/*.go files
// emitted by a newer generator (which the published CLI predates) classify as
// VerdictNewTemplateEmission, the clean-overwrite verdict. Pairs the existing
// import.go fresh-only case in postman-explore with a sibling-package shape so
// regression coverage spans both cli/ and learn/ namespaces.
//
// Per plan 2026-05-23-002, the self-learning recall/teach loop emits a new
// internal/learn/ package on every print. The first regen-merge run against
// any pre-loop published CLI must classify those files as fresh emission,
// not as novel hand-written code.
func TestClassifyLearnLoopEmissionFixture(t *testing.T) {
	t.Parallel()

	pubAbs, err := filepath.Abs("testdata/learn-loop-emission/published")
	require.NoError(t, err)
	freshAbs, err := filepath.Abs("testdata/learn-loop-emission/fresh")
	require.NoError(t, err)

	report, err := Classify(pubAbs, freshAbs, Options{Force: true})
	require.NoError(t, err)
	require.NotNil(t, report)

	verdicts := verdictMap(report)

	// The learn package is fresh-only — published predates the loop.
	assert.Equal(t, VerdictNewTemplateEmission, verdicts["internal/learn/learn.go"],
		"internal/learn/learn.go is fresh-only; classifier must treat new template emission as clean overwrite")
	assert.Equal(t, VerdictNewTemplateEmission, verdicts["internal/learn/store.go"],
		"internal/learn/store.go is fresh-only; same as learn.go")

	// teach.go (a fresh-only cli command paired with the loop) is also new
	// emission. Pinning both paths confirms the verdict applies regardless
	// of which package directory the file lives in.
	assert.Equal(t, VerdictNewTemplateEmission, verdicts["internal/cli/teach.go"],
		"internal/cli/teach.go is fresh-only; new template emission")

	// root.go is templated in both trees; published is a strict subset of fresh
	// (fresh adds the teach AddCommand line, no new top-level decls).
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/root.go"],
		"root.go diff is a call-expression addition, not a top-level decl change")
}
