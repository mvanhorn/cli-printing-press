package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPickNovelFeaturesForManifest_PrefersBuilt confirms that when both
// NovelFeaturesBuilt (dogfood-verified) and NovelFeatures (planned) are
// populated, the built list wins.
func TestPickNovelFeaturesForManifest_PrefersBuilt(t *testing.T) {
	t.Parallel()
	built := []NovelFeature{
		{Name: "Built feature", Command: "do-thing", Description: "shipped"},
	}
	r := &ResearchResult{
		NovelFeatures:      []NovelFeature{{Name: "Planned only", Command: "x"}, {Name: "Other", Command: "y"}},
		NovelFeaturesBuilt: &built,
	}
	got := pickNovelFeaturesForManifest(r)
	require.Len(t, got, 1)
	assert.Equal(t, "Built feature", got[0].Name)
}

// TestPickNovelFeaturesForManifest_FallsBackToPlanned confirms the
// fallback when dogfood didn't run (NovelFeaturesBuilt is nil) — the
// plan was rebuilt via /printing-press but `dogfood` never ran or never
// wrote the built list.
func TestPickNovelFeaturesForManifest_FallsBackToPlanned(t *testing.T) {
	t.Parallel()
	r := &ResearchResult{
		NovelFeatures: []NovelFeature{
			{Name: "Planned A", Command: "a"},
			{Name: "Planned B", Command: "b"},
		},
	}
	got := pickNovelFeaturesForManifest(r)
	require.Len(t, got, 2)
	assert.Equal(t, "Planned A", got[0].Name)
}

// TestPickNovelFeaturesForManifest_FallsBackOnEmptyBuilt confirms that
// an explicitly-empty NovelFeaturesBuilt (dogfood ran but nothing
// shipped) doesn't suppress the planned list. This is debatable as
// product policy — an empty Built could mean "ran and verified zero",
// but in practice agents writing food52-style runs often emit
// NovelFeaturesBuilt: [] before populating it. Falling back avoids
// silent novel_features loss.
func TestPickNovelFeaturesForManifest_FallsBackOnEmptyBuilt(t *testing.T) {
	t.Parallel()
	emptyBuilt := []NovelFeature{}
	r := &ResearchResult{
		NovelFeatures:      []NovelFeature{{Name: "Planned", Command: "p"}},
		NovelFeaturesBuilt: &emptyBuilt,
	}
	got := pickNovelFeaturesForManifest(r)
	require.Len(t, got, 1)
	assert.Equal(t, "Planned", got[0].Name)
}

// TestLoadResearchForPromote_PipelineDir verifies the canonical path
// (pipeline/research.json) is consulted first when state.RunID is set.
func TestLoadResearchForPromote_PipelineDir(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	state := &PipelineState{APIName: "myapi", RunID: "run-001"}

	pipelineDir := state.PipelineDir()
	require.NoError(t, os.MkdirAll(pipelineDir, 0o755))
	r := ResearchResult{
		APIName:       "myapi",
		NovelFeatures: []NovelFeature{{Name: "From pipeline", Command: "cmd"}},
	}
	data, _ := json.Marshal(r)
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "research.json"), data, 0o644))

	got, source, err := loadResearchForPromote(state)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "From pipeline", got.NovelFeatures[0].Name)
	assert.Equal(t, pipelineDir, source)
}

// TestLoadResearchForPromote_RunRoot verifies the fallback to run-root
// research.json when pipeline/research.json is absent. This is the
// shape the skill-driven flow writes today.
func TestLoadResearchForPromote_RunRoot(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	state := &PipelineState{APIName: "myapi", RunID: "run-002"}

	runRoot := RunRoot(state.RunID)
	require.NoError(t, os.MkdirAll(runRoot, 0o755))
	// Note: do NOT create pipeline/research.json — the run-root path
	// is the only source.
	r := ResearchResult{
		APIName:       "myapi",
		NovelFeatures: []NovelFeature{{Name: "From run root", Command: "cmd"}},
	}
	data, _ := json.Marshal(r)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, "research.json"), data, 0o644))

	got, source, err := loadResearchForPromote(state)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "From run root", got.NovelFeatures[0].Name)
	assert.Equal(t, runRoot, source)
}

// TestLoadResearchForPromote_MinimalStateGlob verifies the
// minimal-state path: state.RunID is empty (NewMinimalState), so the
// loader globs the scoped runstate root and picks the most recent
// research.json matching the API name.
func TestLoadResearchForPromote_MinimalStateGlob(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	// Two competing runs for myapi; one for otherapi (should be ignored).
	for _, fixture := range []struct {
		runID   string
		apiName string
		feature string
		mtime   time.Time
	}{
		{"run-old", "myapi", "Old myapi feature", time.Now().Add(-2 * time.Hour)},
		{"run-recent", "myapi", "Recent myapi feature", time.Now().Add(-30 * time.Minute)},
		{"run-other", "otherapi", "Other API feature", time.Now()},
	} {
		runDir := RunRoot(fixture.runID)
		require.NoError(t, os.MkdirAll(runDir, 0o755))
		r := ResearchResult{
			APIName:       fixture.apiName,
			NovelFeatures: []NovelFeature{{Name: fixture.feature, Command: "cmd"}},
		}
		data, _ := json.Marshal(r)
		path := filepath.Join(runDir, "research.json")
		require.NoError(t, os.WriteFile(path, data, 0o644))
		require.NoError(t, os.Chtimes(path, fixture.mtime, fixture.mtime))
	}

	// Minimal-state: no RunID set
	state := NewMinimalState("myapi-pp-cli", "/some/working/dir")
	require.Empty(t, state.RunID, "NewMinimalState should not set RunID")

	got, source, err := loadResearchForPromote(state)
	require.NoError(t, err)
	require.NotNil(t, got, "should find a research.json via glob")
	assert.Equal(t, "Recent myapi feature", got.NovelFeatures[0].Name,
		"should pick the most recent matching API")
	assert.NotEmpty(t, source)
}

// TestLoadResearchForPromote_MinimalStateNoMatch verifies graceful
// behavior when no research.json matches the API name in
// minimal-state — returns (nil, "") rather than crashing or picking a
// non-matching file.
func TestLoadResearchForPromote_MinimalStateNoMatch(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	runDir := RunRoot("run-other")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	r := ResearchResult{
		APIName:       "otherapi",
		NovelFeatures: []NovelFeature{{Name: "Other", Command: "cmd"}},
	}
	data, _ := json.Marshal(r)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "research.json"), data, 0o644))

	state := NewMinimalState("myapi-pp-cli", "/some/dir")
	got, source, err := loadResearchForPromote(state)
	require.NoError(t, err)
	assert.Nil(t, got, "no match for myapi should return nil")
	assert.Empty(t, source)
}

// TestLoadResearchForPromote_NoRunstateRoot verifies graceful behavior
// when the runstate directory doesn't exist at all (fresh workspace,
// or PRINTING_PRESS_HOME pointing somewhere with no prior runs).
func TestLoadResearchForPromote_NoRunstateRoot(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	state := NewMinimalState("myapi-pp-cli", "/some/dir")
	got, source, err := loadResearchForPromote(state)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Empty(t, source)
}

func TestLoadResearchForPromote_MinimalStateGlobReportsMalformedResearch(t *testing.T) {
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")

	runDir := RunRoot("run-bad")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	path := filepath.Join(runDir, "research.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"api_name":"myapi","novel_features":[{"name":1}]}`), 0o644))

	state := NewMinimalState("myapi-pp-cli", "/some/dir")
	got, source, err := loadResearchForPromote(state)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Empty(t, source)
	assert.Contains(t, err.Error(), "research.json at "+path+" failed to parse:")
}

func TestLoadMatchingResearchSkipsVanishedCandidate(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing", "research.json")
	validDir := filepath.Join(dir, "valid")
	require.NoError(t, os.MkdirAll(validDir, 0o755))

	r := ResearchResult{
		APIName:       "myapi",
		NovelFeatures: []NovelFeature{{Name: "From remaining candidate", Command: "cmd"}},
	}
	data, _ := json.Marshal(r)
	validPath := filepath.Join(validDir, "research.json")
	require.NoError(t, os.WriteFile(validPath, data, 0o644))

	got, source, err := loadMatchingResearch([]researchCandidate{
		{path: missingPath, mtime: time.Now()},
		{path: validPath, mtime: time.Now().Add(-time.Minute)},
	}, "myapi")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "From remaining candidate", got.NovelFeatures[0].Name)
	assert.Equal(t, validPath, source)
}
