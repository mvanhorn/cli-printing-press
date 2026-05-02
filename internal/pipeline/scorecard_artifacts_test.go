package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScorecardLoadsResearchFromSiblingResearchDir(t *testing.T) {
	outputDir := t.TempDir()
	runRoot := t.TempDir()
	researchDir := filepath.Join(runRoot, "research")
	proofsDir := filepath.Join(runRoot, "proofs")

	require.NoError(t, os.MkdirAll(researchDir, 0o755))

	research := &ResearchResult{
		APIName: "sample",
		Alternatives: []Alternative{
			{Name: "competitor/sample-cli"},
		},
	}
	data, err := json.MarshalIndent(research, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "research.json"), data, 0o644))

	scorecard, err := RunScorecard(outputDir, proofsDir, "", nil)
	require.NoError(t, err)

	assert.Len(t, scorecard.CompetitorScores, 1)
	assert.FileExists(t, filepath.Join(proofsDir, "scorecard.md"))
}

// TestRunScorecardStripsCLISuffixFromAPIName — APIName lands in the
// Markdown scorecard header and the "Quality Scorecard:" CLI line.
// fullrun passes paths.WorkingCLIDir (always -pp-cli suffixed), and
// library checkouts share that shape; APIName must be the bare slug,
// not the binary name.
func TestRunScorecardStripsCLISuffixFromAPIName(t *testing.T) {
	parent := t.TempDir()
	suffixedDir := filepath.Join(parent, "producthunt-pp-cli")
	require.NoError(t, os.MkdirAll(suffixedDir, 0o755))

	sc, err := RunScorecard(suffixedDir, t.TempDir(), "", nil)
	require.NoError(t, err)
	assert.Equal(t, "producthunt", sc.APIName, "APIName must drop the -pp-cli binary suffix")

	// Bare-slug dirs (local library convention) must pass through unchanged.
	bareDir := filepath.Join(parent, "notion")
	require.NoError(t, os.MkdirAll(bareDir, 0o755))
	scBare, err := RunScorecard(bareDir, t.TempDir(), "", nil)
	require.NoError(t, err)
	assert.Equal(t, "notion", scBare.APIName, "bare-slug dirs must pass through unchanged")
}
