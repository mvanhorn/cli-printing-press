package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setPressTestEnv(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", filepath.Join(home, "repo"))
	return home
}

func TestNewState(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("test-api", "/tmp/test-api-cli")
	assert.Equal(t, "test-api", s.APIName)
	assert.Equal(t, "/tmp/test-api-cli", s.OutputDir)
	assert.Equal(t, "/tmp/test-api-cli", s.WorkingDir)
	assert.Equal(t, "test-scope", s.Scope)
	assert.NotEmpty(t, s.RunID)
	assert.Len(t, s.Phases, len(PhaseOrder))

	for _, name := range PhaseOrder {
		assert.Equal(t, StatusPending, s.Phases[name].Status)
		assert.NotEmpty(t, s.Phases[name].PlanPath)
	}
}

func TestStateRoundTrip(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("roundtrip-test", "/tmp/rt-cli")
	s.SpecPath = "/tmp/spec.yaml"
	s.Complete(PhasePreflight)
	s.MarkSeedWritten(PhaseScaffold)

	require.NoError(t, s.Save())
	defer os.RemoveAll(RunRoot(s.RunID))

	loaded, err := LoadState("roundtrip-test")
	require.NoError(t, err)

	assert.Equal(t, "roundtrip-test", loaded.APIName)
	assert.Equal(t, s.RunID, loaded.RunID)
	assert.Equal(t, s.Scope, loaded.Scope)
	assert.Equal(t, "/tmp/spec.yaml", loaded.SpecPath)
	assert.Equal(t, StatusCompleted, loaded.Phases[PhasePreflight].Status)
	assert.Equal(t, PlanStatusCompleted, loaded.Phases[PhasePreflight].PlanStatus)
	assert.Equal(t, StatusPlanned, loaded.Phases[PhaseScaffold].Status)
	assert.Equal(t, PlanStatusSeed, loaded.Phases[PhaseScaffold].PlanStatus)
	assert.Equal(t, StatusPending, loaded.Phases[PhaseEnrich].Status)
	assert.Empty(t, loaded.Phases[PhaseEnrich].PlanStatus)
}

func TestNextPhase(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("next-test", "/tmp/test")
	assert.Equal(t, PhasePreflight, s.NextPhase())

	s.Complete(PhasePreflight)
	assert.Equal(t, PhaseResearch, s.NextPhase())

	s.Complete(PhaseResearch)
	assert.Equal(t, PhaseScaffold, s.NextPhase())

	for _, name := range PhaseOrder {
		s.Complete(name)
	}
	assert.Equal(t, "", s.NextPhase())
	assert.True(t, s.IsComplete())
}

func TestPhaseTransitions(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("transition-test", "/tmp/test")

	s.MarkSeedWritten(PhasePreflight)
	assert.Equal(t, StatusPlanned, s.Phases[PhasePreflight].Status)
	assert.Equal(t, PlanStatusSeed, s.Phases[PhasePreflight].PlanStatus)

	s.MarkExpanded(PhasePreflight)
	assert.Equal(t, StatusPlanned, s.Phases[PhasePreflight].Status)
	assert.Equal(t, PlanStatusExpanded, s.Phases[PhasePreflight].PlanStatus)

	s.Start(PhasePreflight)
	assert.Equal(t, StatusExecuting, s.Phases[PhasePreflight].Status)

	s.Complete(PhasePreflight)
	assert.Equal(t, StatusCompleted, s.Phases[PhasePreflight].Status)
	assert.Equal(t, PlanStatusCompleted, s.Phases[PhasePreflight].PlanStatus)

	s.Fail(PhaseScaffold)
	assert.Equal(t, StatusFailed, s.Phases[PhaseScaffold].Status)
}

func TestMarkExpandedFromPendingMarksPlanned(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("expanded-test", "/tmp/test")

	s.MarkExpanded(PhaseScaffold)

	assert.Equal(t, StatusPlanned, s.Phases[PhaseScaffold].Status)
	assert.Equal(t, PlanStatusExpanded, s.Phases[PhaseScaffold].PlanStatus)
}

func TestIsSeedBackwardCompatible(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("seed-test", "/tmp/test")
	assert.True(t, s.IsSeed(PhasePreflight))

	s.MarkSeedWritten(PhasePreflight)
	assert.True(t, s.IsSeed(PhasePreflight))

	s.MarkExpanded(PhasePreflight)
	assert.False(t, s.IsSeed(PhasePreflight))
}

func TestDefaultOutputDir(t *testing.T) {
	home := setPressTestEnv(t)
	tests := []struct {
		name     string
		apiName  string
		expected string
	}{
		{"simple", "stripe", filepath.Join(home, "library", "stripe")},
		{"hyphenated", "my-api", filepath.Join(home, "library", "my-api")},
		{"slug dub", "dub", filepath.Join(home, "library", "dub")},
		{"slug cal-com", "cal-com", filepath.Join(home, "library", "cal-com")},
		{"slug steam-web", "steam-web", filepath.Join(home, "library", "steam-web")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, DefaultOutputDir(tt.apiName))
		})
	}
}

func TestPhaseAgentReadinessInPhaseOrder(t *testing.T) {
	setPressTestEnv(t)
	// PhaseAgentReadiness must be between PhaseReview and PhaseComparative.
	var reviewIdx, agentIdx, compIdx int
	for i, name := range PhaseOrder {
		switch name {
		case PhaseReview:
			reviewIdx = i
		case PhaseAgentReadiness:
			agentIdx = i
		case PhaseComparative:
			compIdx = i
		}
	}
	assert.Greater(t, agentIdx, reviewIdx, "agent-readiness must come after review")
	assert.Less(t, agentIdx, compIdx, "agent-readiness must come before comparative")
}

func TestNextPhaseReturnsAgentReadiness(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("ar-test", "/tmp/test")
	// Complete through PhaseReview.
	for _, name := range PhaseOrder {
		if name == PhaseAgentReadiness {
			break
		}
		s.Complete(name)
	}
	assert.Equal(t, PhaseAgentReadiness, s.NextPhase())
}

func TestNewStatePlanPathStableNumbered(t *testing.T) {
	setPressTestEnv(t)
	s := NewState("path-test", "/tmp/test")
	for _, name := range PhaseOrder {
		expected := filepath.Join(s.PipelineDir(), PlanFilename(name))
		assert.Equal(t, expected, s.Phases[name].PlanPath, "PlanPath for %s", name)
	}
	// Spot-check a few to verify the numbering scheme.
	assert.Contains(t, s.Phases[PhasePreflight].PlanPath, "00-preflight-plan.md")
	assert.Contains(t, s.Phases[PhaseAgentReadiness].PlanPath, "55-agent-readiness-plan.md")
	assert.Contains(t, s.Phases[PhaseShip].PlanPath, "70-ship-plan.md")
}

func TestLoadStateMigratesV1ToV2(t *testing.T) {
	setPressTestEnv(t)
	apiName := "migrate-v1-test"
	dir := PipelineDir(apiName)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	defer os.RemoveAll(dir)

	// Simulate a v1 state file without agent-readiness phase, with index-based
	// paths, and some completed phases missing PlanStatus (pre-PlanStatus code).
	v1State := PipelineState{
		Version:   1,
		APIName:   apiName,
		OutputDir: "/tmp/migrate-cli",
		Phases: map[string]PhaseState{
			PhasePreflight:   {Status: StatusCompleted, PlanStatus: PlanStatusCompleted, PlanPath: dir + "/00-preflight-plan.md"},
			PhaseResearch:    {Status: StatusCompleted, PlanPath: dir + "/01-research-plan.md"}, // no PlanStatus (pre-PlanStatus v1)
			PhaseScaffold:    {Status: StatusCompleted, PlanPath: dir + "/02-scaffold-plan.md"}, // no PlanStatus
			PhaseEnrich:      {Status: StatusCompleted, PlanStatus: PlanStatusCompleted, PlanPath: dir + "/03-enrich-plan.md"},
			PhaseRegenerate:  {Status: StatusCompleted, PlanStatus: PlanStatusCompleted, PlanPath: dir + "/04-regenerate-plan.md"},
			PhaseReview:      {Status: StatusCompleted, PlanStatus: PlanStatusCompleted, PlanPath: dir + "/05-review-plan.md"},
			PhaseComparative: {Status: StatusPending, PlanPath: dir + "/06-comparative-plan.md"},
			PhaseShip:        {Status: StatusPending, PlanPath: dir + "/07-ship-plan.md"},
		},
	}
	data, err := json.MarshalIndent(v1State, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(StatePath(apiName), data, 0o644))

	loaded, err := LoadState(apiName)
	require.NoError(t, err)

	// Version bumped and migrated into runstate.
	assert.Equal(t, currentStateVersion, loaded.Version)
	assert.NotEmpty(t, loaded.RunID)
	assert.Equal(t, "test-scope", loaded.Scope)
	assert.Equal(t, filepath.Join(RunPipelineDir(loaded.RunID), PlanFilename(PhasePreflight)), loaded.Phases[PhasePreflight].PlanPath)

	// New phase was backfilled as completed.
	ar := loaded.Phases[PhaseAgentReadiness]
	assert.Equal(t, StatusCompleted, ar.Status)
	assert.Equal(t, PlanStatusCompleted, ar.PlanStatus)

	// All phases have stable-numbered PlanPaths.
	for _, name := range PhaseOrder {
		expected := filepath.Join(RunPipelineDir(loaded.RunID), PlanFilename(name))
		assert.Equal(t, expected, loaded.Phases[name].PlanPath, "migrated PlanPath for %s", name)
	}

	// Completed phases with empty PlanStatus get backfilled.
	assert.Equal(t, PlanStatusCompleted, loaded.Phases[PhaseResearch].PlanStatus, "PlanStatus backfilled for research")
	assert.Equal(t, PlanStatusCompleted, loaded.Phases[PhaseScaffold].PlanStatus, "PlanStatus backfilled for scaffold")

	// Existing phase statuses preserved (comparative was pending).
	assert.Equal(t, StatusPending, loaded.Phases[PhaseComparative].Status)

	// NextPhase() skips the backfilled agent-readiness and returns comparative.
	assert.Equal(t, PhaseComparative, loaded.NextPhase())
}

func TestPhaseStateJSONIncludesPlanStatus(t *testing.T) {
	state := PhaseState{
		Status:     StatusPlanned,
		PlanPath:   "/tmp/test.md",
		PlanStatus: PlanStatusSeed,
	}

	data, err := json.Marshal(state)
	require.NoError(t, err)

	assert.JSONEq(t, `{"status":"planned","plan_path":"/tmp/test.md","plan_status":"seed"}`, string(data))
}

func TestResolveStatePathPrefersLegacyOverExcludedRunstateScratch(t *testing.T) {
	setPressTestEnv(t)

	apiName := "legacy-upgrade"
	legacyDir := legacyWorkspacePipelineDir(apiName)
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))

	legacyState := PipelineState{
		Version:   1,
		APIName:   apiName,
		OutputDir: "/tmp/legacy-cli",
		Phases: map[string]PhaseState{
			PhasePreflight: {Status: StatusCompleted},
		},
	}
	legacyData, err := json.MarshalIndent(legacyState, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "state.json"), legacyData, 0o644))

	scratch := NewStateWithRun(apiName, filepath.Join(t.TempDir(), "scratch-pp-cli"), "run-scratch", "test-scope")
	scratch.ExcludeFromCurrentResolution = true
	require.NoError(t, scratch.SaveWithoutCurrentPointer())

	resolved, ok := resolveStatePath(apiName)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(legacyDir, "state.json"), resolved)

	loaded, err := LoadState(apiName)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/legacy-cli", loaded.OutputDir)
	assert.NotEqual(t, "run-scratch", loaded.RunID)
}
