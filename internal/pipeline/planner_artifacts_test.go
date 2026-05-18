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

func TestGenerateShipPlanHoldsOnAgentReadinessDegrade(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 80,
		},
		OverallGrade: "A",
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))
	require.NoError(t, os.WriteFile(filepath.Join(state.PipelineDir(), "agent-readiness.md"), []byte(`## Agent Readiness

Phase verdict: Degrade

- Blocker: mutating commands can run under verify mode
- Friction: help omits automation examples
`), 0o644))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Quality: PASS")
	assert.Contains(t, shipPlan, "Agent readiness: HOLD")
	assert.Contains(t, shipPlan, "reports Degrade")
	assert.Contains(t, shipPlan, "Blocker: mutating commands can run under verify mode")
	assert.Contains(t, shipPlan, "**HOLD**")
	assert.NotContains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
}

func TestGenerateShipPlanShipsWhenScorecardAndReadinessPass(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 80,
		},
		OverallGrade: "A",
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))
	require.NoError(t, os.WriteFile(filepath.Join(state.PipelineDir(), "agent-readiness.md"), []byte(`## Agent Readiness

Phase verdict: Pass
`), 0o644))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Quality: PASS")
	assert.Contains(t, shipPlan, "Agent readiness: PASS")
	assert.Contains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
	assert.NotContains(t, shipPlan, "**HOLD**")
}

func TestGenerateShipPlanHoldsWhenAgentReadinessMissing(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 80,
		},
		OverallGrade: "A",
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Quality: PASS")
	assert.Contains(t, shipPlan, "Agent readiness: HOLD - missing pipeline/agent-readiness.md")
	assert.Contains(t, shipPlan, "**HOLD**")
	assert.NotContains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
}

func TestGenerateShipPlanIgnoresReadinessOutsidePipelineDir(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 80,
		},
		OverallGrade: "A",
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))
	require.NoError(t, os.WriteFile(filepath.Join(state.ProofsDir(), "agent-readiness.md"), []byte(`## Agent Readiness

Phase verdict: Pass
`), 0o644))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Agent readiness: HOLD - missing pipeline/agent-readiness.md")
	assert.Contains(t, shipPlan, "**HOLD**")
	assert.NotContains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
}

func TestGenerateShipPlanHoldsWhenScorecardMissing(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	require.NoError(t, os.WriteFile(filepath.Join(state.PipelineDir(), "agent-readiness.md"), []byte(`## Agent Readiness

Phase verdict: Pass
`), 0o644))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Quality: HOLD - missing scorecard.json")
	assert.Contains(t, shipPlan, "Agent readiness: PASS")
	assert.Contains(t, shipPlan, "**HOLD**")
	assert.NotContains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
}

func TestGenerateShipPlanHoldsWhenAgentReadinessVerdictMissing(t *testing.T) {
	setPressTestEnv(t)

	state := NewStateWithRun("sample", filepath.Join(t.TempDir(), "sample-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())
	scorecard := &Scorecard{
		APIName: "sample",
		Steinberger: SteinerScore{
			Percentage: 80,
		},
		OverallGrade: "A",
	}
	require.NoError(t, writeScorecardJSON(scorecard, state.ProofsDir()))
	require.NoError(t, os.WriteFile(filepath.Join(state.PipelineDir(), "agent-readiness.md"), []byte(`## Agent Readiness

A non-Pass verdict such as Degrade must block ship, but this prose is not the verdict line.
`), 0o644))

	shipPlan, err := GenerateNextPlan(state, PhaseShip)
	require.NoError(t, err)
	assert.Contains(t, shipPlan, "Agent readiness: HOLD - missing pipeline/agent-readiness.md")
	assert.Contains(t, shipPlan, "**HOLD**")
	assert.NotContains(t, shipPlan, "**SHIP** - Quality and agent-readiness gates both pass.")
}

func TestParseAgentReadinessVerdictRequiresExplicitVerdictLine(t *testing.T) {
	assert.Equal(t, AgentReadinessDegrade, parseAgentReadinessVerdict("Phase verdict: **Degrade**"))
	assert.Equal(t, AgentReadinessWarn, parseAgentReadinessVerdict("**Verdict:** Warn"))
	assert.Empty(t, parseAgentReadinessVerdict("A non-Pass verdict such as Degrade must block ship."))
}

func TestLoadAgentReadinessReportCollectsTableFindings(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-readiness.md"), []byte(`## Agent Readiness

Phase verdict: Degrade

| Severity | Finding |
|---|---|
| Blocker | mutating commands can run under verify mode |
`), 0o644))

	report, err := LoadAgentReadinessReport(dir)
	require.NoError(t, err)
	assert.Equal(t, AgentReadinessDegrade, report.Verdict)
	assert.Contains(t, report.Findings, "| Blocker | mutating commands can run under verify mode |")
}
