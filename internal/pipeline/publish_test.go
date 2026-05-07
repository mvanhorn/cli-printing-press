package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyDir(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, src string)
		check func(t *testing.T, src, dst string)
	}{
		{
			name: "regular files and directories",
			setup: func(t *testing.T, src string) {
				require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(src, "root.txt"), []byte("root"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nested"), 0o644))
			},
			check: func(t *testing.T, _, dst string) {
				assert.FileExists(t, filepath.Join(dst, "root.txt"))
				assert.FileExists(t, filepath.Join(dst, "sub", "nested.txt"))
				data, err := os.ReadFile(filepath.Join(dst, "root.txt"))
				require.NoError(t, err)
				assert.Equal(t, "root", string(data))
			},
		},
		{
			name: "internal file symlink preserved as symlink",
			setup: func(t *testing.T, src string) {
				target := filepath.Join(src, "target.txt")
				require.NoError(t, os.WriteFile(target, []byte("target"), 0o644))
				require.NoError(t, os.Symlink("target.txt", filepath.Join(src, "link.txt")))
			},
			check: func(t *testing.T, _, dst string) {
				linkPath := filepath.Join(dst, "link.txt")
				info, err := os.Lstat(linkPath)
				require.NoError(t, err)
				assert.NotZero(t, info.Mode()&os.ModeSymlink, "expected symlink, got regular file")
			},
		},
		{
			name: "internal directory symlink preserved as symlink",
			setup: func(t *testing.T, src string) {
				targetDir := filepath.Join(src, "target-dir")
				require.NoError(t, os.MkdirAll(targetDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(targetDir, "big.bin"), []byte("data"), 0o644))
				require.NoError(t, os.Symlink("target-dir", filepath.Join(src, "linked-dir")))
			},
			check: func(t *testing.T, _, dst string) {
				linkPath := filepath.Join(dst, "linked-dir")
				info, err := os.Lstat(linkPath)
				require.NoError(t, err)
				assert.NotZero(t, info.Mode()&os.ModeSymlink,
					"directory symlink should be preserved as a symlink, not followed")
				targetData, err := os.ReadFile(filepath.Join(dst, "target-dir", "big.bin"))
				require.NoError(t, err)
				assert.Equal(t, "data", string(targetData))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "src")
			dst := filepath.Join(t.TempDir(), "dst")
			require.NoError(t, os.MkdirAll(src, 0o755))

			tt.setup(t, src)
			require.NoError(t, CopyDir(src, dst))
			tt.check(t, src, dst)
		})
	}
}

func TestCopyDirRejectsExternalSymlinks(t *testing.T) {
	tests := []struct {
		name       string
		linkName   string
		linkTarget string
	}{
		{
			name:       "absolute external target",
			linkName:   "external.txt",
			linkTarget: filepath.Join(t.TempDir(), "outside.txt"),
		},
		{
			name:       "relative target escaping root",
			linkName:   "escape.txt",
			linkTarget: filepath.Join("..", "outside.txt"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			require.NoError(t, os.MkdirAll(src, 0o755))

			outside := filepath.Join(root, "outside.txt")
			require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
			require.NoError(t, os.Symlink(tt.linkTarget, filepath.Join(src, tt.linkName)))

			err := CopyDir(src, dst)
			require.Error(t, err)
			assert.ErrorContains(t, err, "points outside source tree")
		})
	}
}

// publishManifestEnvSetup wires PRINTING_PRESS_HOME/SCOPE/REPO_ROOT to a temp dir
// so RunRoot()/PipelineDir()/PublishedLibraryRoot() resolve under the test sandbox.
// Returns the temp root and a state seeded with the given run ID.
func publishManifestEnvSetup(t *testing.T, runID string) (string, *PipelineState) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)
	state := NewStateWithRun("test-api", filepath.Join(tmp, "working", "test-api-pp-cli"), runID, "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	return tmp, state
}

// readPublishedManifest reads the manifest from the given dir for assertions.
func readPublishedManifest(t *testing.T, dir string) CLIManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var m CLIManifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// writeResearchAt writes a ResearchResult to the given directory's research.json.
func writeResearchAt(t *testing.T, dir string, r *ResearchResult) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.MarshalIndent(r, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "research.json"), data, 0o644))
}

// TestWriteCLIManifestForPublish_NovelFeaturesFromSkillFlowResearch covers the
// printing-press skill flow: research.json lives at <RunRoot>/research.json
// (the run root itself, not the pipeline subdir). Before this fix, LoadResearch
// only checked PipelineDir and silently missed this convention — manifest shipped
// with empty novel_features and publish validate failed (cal-com retro #334 F2).
func TestWriteCLIManifestForPublish_NovelFeaturesFromSkillFlowResearch(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-skill-flow")

	// Skill flow: research.json at RunRoot, NOT under pipeline/.
	built := []NovelFeature{
		{Name: "One-shot booking", Command: "book", Description: "Compose slots-find + reserve + create + confirm."},
		{Name: "Today's agenda", Command: "today", Description: "Read today's bookings from local store."},
	}
	writeResearchAt(t, RunRoot(state.RunID), &ResearchResult{
		APIName:            "test-api",
		NovelFeaturesBuilt: &built,
	})

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 2, "novel_features should be populated from RunRoot/research.json")
	assert.Equal(t, "book", m.NovelFeatures[0].Command)
	assert.Equal(t, "today", m.NovelFeatures[1].Command)
}

func TestWriteCLIManifestForPublishKeepsCatalogDisplayNameOverTitleFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	state := NewStateWithRun("producthunt", filepath.Join(tmp, "working", "producthunt-pp-cli"), "20260507-display-name", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
openapi: "3.0.0"
info:
  title: Producthunt API
  version: "1.0"
paths: {}
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, "Product Hunt", m.DisplayName)
}

// TestWriteCLIManifestForPublish_NovelFeaturesFromPrintFlowResearch covers the
// printing-press print flow: research.json lives at <RunRoot>/pipeline/research.json
// alongside phase artifacts. The fallback path keeps print-flow CLIs working.
func TestWriteCLIManifestForPublish_NovelFeaturesFromPrintFlowResearch(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-print-flow")

	// Print flow: research.json under PipelineDir, NOT at RunRoot.
	built := []NovelFeature{
		{Name: "Conflicts", Command: "conflicts", Description: "Find overlaps."},
	}
	writeResearchAt(t, state.PipelineDir(), &ResearchResult{
		APIName:            "test-api",
		NovelFeaturesBuilt: &built,
	})

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 1, "novel_features should populate from PipelineDir/research.json")
	assert.Equal(t, "conflicts", m.NovelFeatures[0].Command)
}

// TestWriteCLIManifestForPublish_NovelFeaturesPreservedFromCarryForward covers
// the defense-in-depth path: research.json missing (deleted, not yet written),
// but the existing manifest in the staging dir already has novel_features from
// generate time. The carry-forward block must preserve them so publish doesn't
// silently strip a populated field.
func TestWriteCLIManifestForPublish_NovelFeaturesPreservedFromCarryForward(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-carry-forward")

	// Pre-populate the staging dir's existing manifest with novel_features.
	existing := CLIManifest{
		SchemaVersion: 1,
		APIName:       "test-api",
		CLIName:       "test-api-pp-cli",
		NovelFeatures: []NovelFeatureManifest{
			{Name: "Today", Command: "today", Description: "Today's bookings."},
		},
	}
	existingData, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, CLIManifestFilename), existingData, 0o644))

	// No research.json anywhere. Publish should preserve the carry-forward value.
	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 1, "carry-forward should preserve generate-time novel_features")
	assert.Equal(t, "today", m.NovelFeatures[0].Command)
}

func TestWriteCLIManifestForPublish_AuthEnvVarSpecsPreservedFromCarryForward(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260505-auth-env-specs-carry-forward")

	existing := CLIManifest{
		SchemaVersion:   1,
		APIName:         "test-api",
		CLIName:         "test-api-pp-cli",
		AuthType:        "api_key",
		AuthEnvVars:     []string{"TEST_API_TOKEN"},
		AuthEnvVarSpecs: []spec.AuthEnvVar{{Name: "TEST_API_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true}},
	}
	existingData, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, CLIManifestFilename), existingData, 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, []string{"TEST_API_TOKEN"}, m.AuthEnvVars)
	assert.Equal(t, []spec.AuthEnvVar{{Name: "TEST_API_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true}}, m.AuthEnvVarSpecs)
}

// TestWriteCLIManifestForPublish_NovelFeaturesResearchOverridesCarryForward
// covers the precedence rule: when both research.json (post-dogfood) and the
// existing manifest (generate-time) have novel_features, research wins because
// post-dogfood verification is the source of truth.
func TestWriteCLIManifestForPublish_NovelFeaturesResearchOverridesCarryForward(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-research-wins")

	// Stale generate-time manifest with one feature.
	existing := CLIManifest{
		SchemaVersion: 1,
		APIName:       "test-api",
		NovelFeatures: []NovelFeatureManifest{
			{Name: "Stale", Command: "stale", Description: "Outdated."},
		},
	}
	existingData, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, CLIManifestFilename), existingData, 0o644))

	// Fresh post-dogfood research with two features (different from stale).
	built := []NovelFeature{
		{Name: "Book", Command: "book", Description: "One-shot booking."},
		{Name: "Today", Command: "today", Description: "Today's agenda."},
	}
	writeResearchAt(t, RunRoot(state.RunID), &ResearchResult{
		APIName:            "test-api",
		NovelFeaturesBuilt: &built,
	})

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 2, "research should override carry-forward")
	commands := []string{m.NovelFeatures[0].Command, m.NovelFeatures[1].Command}
	assert.ElementsMatch(t, []string{"book", "today"}, commands, "research-loaded features replace stale carry-forward")
}

// TestWriteCLIManifestForPublish_EmptyResearchKeepsCarryForward covers the
// edge case where research.json exists but novel_features_built is empty
// (e.g., a run where no novel features survived dogfood). The empty research
// must NOT clobber a populated carry-forward — the carry-forward represents
// the most-recent verified data the system has.
func TestWriteCLIManifestForPublish_EmptyResearchKeepsCarryForward(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-empty-research")

	// Carry-forward has one feature.
	existing := CLIManifest{
		SchemaVersion: 1,
		APIName:       "test-api",
		NovelFeatures: []NovelFeatureManifest{
			{Name: "Today", Command: "today", Description: "Today's agenda."},
		},
	}
	existingData, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, CLIManifestFilename), existingData, 0o644))

	// Research has empty NovelFeaturesBuilt.
	emptyBuilt := []NovelFeature{}
	writeResearchAt(t, RunRoot(state.RunID), &ResearchResult{
		APIName:            "test-api",
		NovelFeaturesBuilt: &emptyBuilt,
	})

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 1, "empty research must not clobber populated carry-forward")
	assert.Equal(t, "today", m.NovelFeatures[0].Command)
}

// TestWriteCLIManifestForPublish_NoResearchNoExistingManifest covers the
// genuinely-empty case: no research.json, no prior manifest. The published
// manifest has no novel_features (correct — there are none to publish).
// publish validate's transcendence check will then fail with the existing
// "no novel features recorded" message.
func TestWriteCLIManifestForPublish_NoResearchNoExistingManifest(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-empty-everything")

	// No research.json, no existing manifest in WorkingDir.
	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Empty(t, m.NovelFeatures, "no novel_features when neither research nor prior manifest has any")
}
