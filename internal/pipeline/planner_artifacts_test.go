package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNextPlanLoadsArtifactsFromRunstateDirs(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())

	research := &ResearchResult{
		APIName:        "sample",
		NoveltyScore:   8,
		Recommendation: "proceed",
		Alternatives: []Alternative{
			{Name: "competitor/sample-cli"},
		},
	}
	researchData, err := json.MarshalIndent(research, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(state.ResearchDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.ResearchDir(), "research.json"), researchData, 0o644))

	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 72,
		},
		OverallGrade: "B",
		GapReport:    []string{"example gap"},
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))

	scaffoldPlan, err := GenerateNextPlan(state, PhaseScaffold)
	require.NoError(t, err)
	assert.Contains(t, scaffoldPlan, "Novelty score:** 8/10 (proceed)")
	assert.Contains(t, scaffoldPlan, "All eight")
	assert.Contains(t, scaffoldPlan, "govulncheck")

	comparativePlan, err := GenerateNextPlan(state, PhaseComparative)
	require.NoError(t, err)
	assert.Contains(t, comparativePlan, "Overall: 72% (B)")
	assert.Contains(t, comparativePlan, "example gap")
}
