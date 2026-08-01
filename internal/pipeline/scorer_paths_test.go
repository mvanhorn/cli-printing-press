package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindResearchDirRequiresTargetOwnership(t *testing.T) {
	runRoot := t.TempDir()
	cliDir := filepath.Join(runRoot, "working", "demo-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// An unrelated ancestor must not be selected or mutated by callers that
	// write novel_features_built after implicit discovery.
	unrelated := &ResearchResult{
		APIName:       "other-api",
		NovelFeatures: []NovelFeature{{Name: "Other", Command: "other"}},
	}
	require.NoError(t, writeResearchJSON(unrelated, runRoot))

	require.Equal(t, cliDir, FindResearchDir(cliDir))
	data, err := os.ReadFile(filepath.Join(runRoot, "research.json"))
	require.NoError(t, err)
	var after ResearchResult
	require.NoError(t, json.Unmarshal(data, &after))
	require.Nil(t, after.NovelFeaturesBuilt)
}

func TestFindResearchDirAcceptsMatchingTargetOwnership(t *testing.T) {
	runRoot := t.TempDir()
	cliDir := filepath.Join(runRoot, "working", "demo-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, writeResearchJSON(&ResearchResult{APIName: "demo"}, runRoot))
	stateData, err := json.Marshal(PipelineState{APIName: "demo", RunID: "run-1", WorkingDir: cliDir})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, "state.json"), stateData, 0o644))

	require.Equal(t, runRoot, FindResearchDir(cliDir))
}

func TestFindResearchDirRejectsDifferentRunForSameAPI(t *testing.T) {
	runRoot := t.TempDir()
	cliDir := filepath.Join(runRoot, "working", "demo-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	manifestData, err := json.Marshal(CLIManifest{APIName: "demo", RunID: "current-run"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), manifestData, 0o644))
	require.NoError(t, writeResearchJSON(&ResearchResult{APIName: "demo"}, runRoot))
	state := PipelineState{APIName: "demo", RunID: "previous-run", WorkingDir: cliDir}
	stateData, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, "state.json"), stateData, 0o644))

	require.Equal(t, cliDir, FindResearchDir(cliDir))
}

func TestFindResearchDirAcceptsMatchingRunForSameAPI(t *testing.T) {
	runRoot := t.TempDir()
	cliDir := filepath.Join(runRoot, "working", "demo-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	manifestData, err := json.Marshal(CLIManifest{APIName: "demo", RunID: "current-run"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), manifestData, 0o644))
	require.NoError(t, writeResearchJSON(&ResearchResult{APIName: "demo"}, runRoot))
	state := PipelineState{APIName: "demo", RunID: "current-run", WorkingDir: cliDir}
	stateData, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, "state.json"), stateData, 0o644))

	require.Equal(t, runRoot, FindResearchDir(cliDir))
}
