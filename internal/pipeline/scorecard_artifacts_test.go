package pipeline

import (
	"encoding/json"
	"fmt"
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

func TestScorecardTierDenominators(t *testing.T) {
	infra, domain := ScorecardTierDenominators(nil)
	assert.Equal(t, 0, infra)
	assert.Equal(t, 0, domain)

	infra, domain = ScorecardTierDenominators(&Scorecard{})
	assert.Equal(t, 200, infra)
	assert.Equal(t, 60, domain)

	_, domain = ScorecardTierDenominators(&Scorecard{UnscoredDimensions: []string{DimPathValidity, DimAuthProtocol}})
	assert.Equal(t, 40, domain)

	_, domain = ScorecardTierDenominators(&Scorecard{UnscoredDimensions: []string{DimTypeFidelity}})
	assert.Equal(t, 55, domain)
}

func TestWriteScorecardMarkdownNamesDenominatorAndCalibration(t *testing.T) {
	dir := t.TempDir()
	pipelineDir := t.TempDir()
	sc, err := RunScorecard(dir, pipelineDir, "", nil)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(pipelineDir, "scorecard.md"))
	require.NoError(t, err)
	infra, domain := ScorecardTierDenominators(sc)
	assert.Contains(t, string(data), fmt.Sprintf("Denominator: infrastructure %d, domain %d", infra, domain))
	assert.NotContains(t, string(data), "Calibration:")
	assert.Empty(t, sc.Steinberger.CalibrationNote)

	weak := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(weak, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(weak, "internal", "cli", "root.go"), []byte("package cli\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(weak, "README.md"), []byte("# Fixture\n"), 0o644))
	calibratedDir := t.TempDir()
	scored, err := RunScorecard(weak, calibratedDir, "", &VerifyReport{
		Mode:         "mock",
		PassRate:     100,
		Passed:       10,
		Total:        10,
		DataPipeline: true,
		Verdict:      "PASS",
	})
	require.NoError(t, err)
	require.NotEmpty(t, scored.Steinberger.CalibrationNote)
	assert.Less(t, scored.Steinberger.README, 10)
	body, err := os.ReadFile(filepath.Join(calibratedDir, "scorecard.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "Calibration: "+scored.Steinberger.CalibrationNote)
}
