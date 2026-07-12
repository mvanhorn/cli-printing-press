package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

func TestCopyDirStripsGitPlumbing(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(src, 0o755))

	// Top-level .git/ from a stray `git init` in a working CLI dir.
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".git", "objects", "pack"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".git", "objects", "pack", "pack-abc.idx"), []byte("idx"), 0o644))

	// Nested .git/ (submodule).
	require.NoError(t, os.MkdirAll(filepath.Join(src, "vendor", "submod", ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "vendor", "submod", ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "vendor", "submod", "module.go"), []byte("package submod"), 0o644))

	// .gitmodules at the root.
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitmodules"), []byte("[submodule \"submod\"]\n\tpath = vendor/submod\n"), 0o644))

	// Legitimate CLI content that must be preserved.
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitignore"), []byte("/build\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitattributes"), []byte("* text=auto\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main"), 0o644))

	require.NoError(t, CopyDir(src, dst))

	assert.NoDirExists(t, filepath.Join(dst, ".git"))
	assert.NoDirExists(t, filepath.Join(dst, "vendor", "submod", ".git"))
	assert.NoFileExists(t, filepath.Join(dst, ".gitmodules"))

	assert.FileExists(t, filepath.Join(dst, ".gitignore"))
	assert.FileExists(t, filepath.Join(dst, ".gitattributes"))
	assert.FileExists(t, filepath.Join(dst, "main.go"))
	assert.FileExists(t, filepath.Join(dst, "vendor", "submod", "module.go"))
}

// A worktree-style ".git" can be a regular file pointing at the parent
// gitdir, or a symlink pointing outside the source tree. CopyDir would
// otherwise reject the symlink for escaping the source root, defeating the
// strip. Both shapes must drop silently like the directory shape.
func TestCopyDirStripsGitFileAndSymlink(t *testing.T) {
	t.Run("git as regular file", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		require.NoError(t, os.MkdirAll(src, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(src, ".git"), []byte("gitdir: /elsewhere\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main"), 0o644))

		require.NoError(t, CopyDir(src, dst))

		assert.NoFileExists(t, filepath.Join(dst, ".git"))
		assert.FileExists(t, filepath.Join(dst, "main.go"))
	})

	t.Run("git as external symlink", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "src")
		dst := filepath.Join(root, "dst")
		require.NoError(t, os.MkdirAll(src, 0o755))

		outside := filepath.Join(root, "real-gitdir")
		require.NoError(t, os.MkdirAll(outside, 0o755))
		require.NoError(t, os.Symlink(outside, filepath.Join(src, ".git")))
		require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main"), 0o644))

		require.NoError(t, CopyDir(src, dst))

		_, err := os.Lstat(filepath.Join(dst, ".git"))
		assert.True(t, os.IsNotExist(err), "external .git symlink should be skipped, not copied or rejected")
		assert.FileExists(t, filepath.Join(dst, "main.go"))
	})
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

func TestCopyPublishableManuscriptDirFiltersSymlinks(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, os.MkdirAll(src, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(src, "notes.txt"), []byte("notes"), 0o644))
	require.NoError(t, os.Symlink("notes.txt", filepath.Join(src, "notes-link.txt")))
	require.NoError(t, os.Symlink("notes.txt", filepath.Join(src, "capture.har")))
	require.NoError(t, os.WriteFile(filepath.Join(src, "raw-capture.har"), []byte("cookie: secret"), 0o644))
	require.NoError(t, os.Symlink("raw-capture.har", filepath.Join(src, "raw-capture-link.txt")))

	largeCapture := filepath.Join(src, "large-capture.bin")
	largeFile, err := os.Create(largeCapture)
	require.NoError(t, err)
	require.NoError(t, largeFile.Truncate(publishableManuscriptMaxCaptureBytes))
	require.NoError(t, largeFile.Close())
	require.NoError(t, os.Symlink("large-capture.bin", filepath.Join(src, "large-link.bin")))

	require.NoError(t, CopyPublishableManuscriptDir(src, dst))

	linkInfo, err := os.Lstat(filepath.Join(dst, "notes-link.txt"))
	require.NoError(t, err)
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "ordinary internal symlink should be preserved")
	assert.FileExists(t, filepath.Join(dst, "notes.txt"))
	assert.NoFileExists(t, filepath.Join(dst, "capture.har"))
	assert.NoFileExists(t, filepath.Join(dst, "raw-capture.har"))
	assert.NoFileExists(t, filepath.Join(dst, "raw-capture-link.txt"))
	assert.NoFileExists(t, filepath.Join(dst, "large-capture.bin"))
	assert.NoFileExists(t, filepath.Join(dst, "large-link.bin"))
}

// TestCopyPublishableManuscriptDirExcludesDownloadedSources ensures a research
// `sources/` subtree (downloaded third-party reference repos) never ships into a
// published CLI — it is local research input, not authored manuscript output, and
// publishing copies of other projects' code is a licensing + secret/PII exposure.
func TestCopyPublishableManuscriptDirExcludesDownloadedSources(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")

	// Authored manuscript output that must ship.
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "brief.md"), []byte("our synthesis"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "proofs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "proofs", "shipcheck.txt"), []byte("ok"), 0o644))

	// Downloaded third-party reference repos under research/sources/ that must NOT ship.
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research", "sources", "thirdparty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "sources", "thirdparty", "lib.py"), []byte("# someone else's code"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "sources", "README.md"), []byte("vendored readme"), 0o644))

	require.NoError(t, CopyPublishableManuscriptDir(src, dst))

	assert.FileExists(t, filepath.Join(dst, "research", "brief.md"))
	assert.FileExists(t, filepath.Join(dst, "proofs", "shipcheck.txt"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "sources"))
	assert.NoFileExists(t, filepath.Join(dst, "research", "sources", "thirdparty", "lib.py"))
	assert.NoFileExists(t, filepath.Join(dst, "research", "sources", "README.md"))
}

func TestCopyPublishableManuscriptDirExcludesRawBrowserSniffCaptures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")

	require.NoError(t, os.MkdirAll(filepath.Join(src, "discovery", "bundles"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "discovery", "batch-01"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "discovery", "v2", "bundles"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research", "squire-browser-sniff-spec-samples"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "probe-001.json"), []byte(`{"email":"customer@gmail.com"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "batch-01", "probe-002.json"), []byte(`{"email":"customer@gmail.com"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "capture.har"), []byte("raw har"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "bundles", "app.js"), []byte("window.email='customer@gmail.com'"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "v2", "bundles", "app.js"), []byte("window.email='customer@gmail.com'"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "squire-browser-sniff-spec-samples", "sample.json"), []byte(`{"email":"customer@gmail.com"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "traffic-analysis.json"), []byte(`{"auth_stripped":true}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "browser-sniff-report.md"), []byte("# Report"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "brief.md"), []byte("our synthesis"), 0o644))

	require.NoError(t, CopyPublishableManuscriptDir(src, dst))

	assert.NoFileExists(t, filepath.Join(dst, "discovery", "probe-001.json"))
	assert.NoFileExists(t, filepath.Join(dst, "discovery", "batch-01", "probe-002.json"))
	assert.NoFileExists(t, filepath.Join(dst, "discovery", "capture.har"))
	assert.NoDirExists(t, filepath.Join(dst, "discovery", "bundles"))
	assert.NoDirExists(t, filepath.Join(dst, "discovery", "v2", "bundles"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "squire-browser-sniff-spec-samples"))
	assert.FileExists(t, filepath.Join(dst, "discovery", "traffic-analysis.json"))
	assert.FileExists(t, filepath.Join(dst, "discovery", "browser-sniff-report.md"))
	assert.FileExists(t, filepath.Join(dst, "research", "brief.md"))
}

func TestCopyPublishableManuscriptDirCanIncludeRawBrowserSniffCaptures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")

	require.NoError(t, os.MkdirAll(filepath.Join(src, "discovery", "bundles"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research", "squire-browser-sniff-spec-samples"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research", "sources", "thirdparty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "probe-001.json"), []byte(`{"status":"sample"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "capture.har"), []byte("raw har"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "discovery", "bundles", "app.js"), []byte("console.log('sample')"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "squire-browser-sniff-spec-samples", "sample.json"), []byte(`{"status":"sample"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "sources", "thirdparty", "lib.py"), []byte("# third party"), 0o644))
	largeFile, err := os.Create(filepath.Join(src, "large-authored-artifact.bin"))
	require.NoError(t, err)
	require.NoError(t, largeFile.Truncate(publishableManuscriptMaxCaptureBytes))
	require.NoError(t, largeFile.Close())

	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, dst, PublishableManuscriptCopyOptions{IncludeRawCaptures: true}))

	assert.FileExists(t, filepath.Join(dst, "discovery", "probe-001.json"))
	assert.FileExists(t, filepath.Join(dst, "discovery", "capture.har"))
	assert.FileExists(t, filepath.Join(dst, "discovery", "bundles", "app.js"))
	assert.FileExists(t, filepath.Join(dst, "research", "squire-browser-sniff-spec-samples", "sample.json"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "sources"))
	assert.NoFileExists(t, filepath.Join(dst, "large-authored-artifact.bin"))
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

func TestWriteCLIManifestForPublishPreservesGeneratedDescription(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	rich := "Local-first CLI for the Roam HQ API (chat, On-Air events, transcripts, SCIM, webhooks) with offline FTS search and agent-friendly JSON output."
	state := NewStateWithRun("asana", filepath.Join(tmp, "working", "asana-pp-cli"), "20260521-description", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	writeManifest(t, state.WorkingDir, CLIManifest{
		APIName:     "asana",
		Description: rich,
	})
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
name: asana
description: API-shaped fallback copy.
cli_description: Spec CLI copy that should not replace generated manifest copy during publish.
version: "1.0"
base_url: https://api.example.com
auth:
  type: none
resources: {}
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, rich, m.Description)
	assert.False(t, strings.HasSuffix(m.Description, "..."))
}

func TestWriteCLIManifestForPublishReplacesLiteralEllipsisDescription(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	state := NewStateWithRun("asana", filepath.Join(tmp, "working", "asana-pp-cli"), "20260521-description", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	writeManifest(t, state.WorkingDir, CLIManifest{
		APIName:     "asana",
		Description: "Legacy generated description...",
	})
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
name: asana
description: API-shaped fallback copy.
version: "1.0"
base_url: https://api.example.com
auth:
  type: none
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, "API-shaped fallback copy.", m.Description)
	assert.False(t, strings.HasSuffix(m.Description, "..."))
}

func TestWriteCLIManifestForPublishReplacesLiteralEllipsisDescriptionFromSyntheticSpec(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	state := NewStateWithRun("synthetic-local", filepath.Join(tmp, "working", "synthetic-local-pp-cli"), "20260521-description", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	writeManifest(t, state.WorkingDir, CLIManifest{
		APIName:     "synthetic-local",
		Description: "Legacy generated description...",
	})
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
name: synthetic-local
description: Synthetic local command line interface description without early punctuation so publish still emits usable metadata after rejecting legacy ellipsis descriptions
version: "1.0"
base_url: https://api.example.com
auth:
  type: none
resources: {}
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.NotEmpty(t, m.Description)
	assert.False(t, strings.HasSuffix(m.Description, "..."))
}

func TestWriteCLIManifestForPublishRebasesAuthEnvAfterNameOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	state := NewStateWithRun("elevenlabs", filepath.Join(tmp, "working", "elevenlabs-pp-cli"), "20260513-elevenlabs", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
openapi: "3.1.0"
info:
  title: ElevenLabs API Documentation
  version: "1.0"
paths:
  /v1/models:
    get:
      operationId: getModels
      parameters:
        - name: xi-api-key
          in: header
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, []string{"ELEVENLABS_API_KEY"}, m.AuthEnvVars)
	assert.Empty(t, m.AuthKeyURL)

	tools, err := ReadToolsManifest(state.WorkingDir)
	require.NoError(t, err)
	assert.Equal(t, "elevenlabs", tools.APIName)
	assert.Equal(t, "https://api.example.com", tools.BaseURL)
	assert.Equal(t, []string{"ELEVENLABS_API_KEY"}, tools.Auth.EnvVars)
}

func TestWriteCLIManifestForPublishPopulatesCategoryFromSpec(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	state := NewStateWithRun("synthetic-travel-cli", filepath.Join(tmp, "working", "synthetic-travel-cli-pp-cli"), "20260508-category", "test-scope")
	require.NoError(t, os.MkdirAll(state.WorkingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.WorkingDir, "spec.yaml"), []byte(`
name: synthetic-travel-cli
description: Synthetic travel CLI
version: "1.0"
base_url: https://api.example.com
category: travel
auth:
  type: none
resources:
  trips:
    description: Trips
    endpoints:
      list:
        method: GET
        path: /trips
`), 0o644))

	require.NoError(t, writeCLIManifestForPublish(state, state.WorkingDir))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Equal(t, "travel", m.Category)
}

// TestWriteCLIManifestForPublish_NovelFeaturesFromPrintFlowResearch covers the
// cli-printing-press print flow: research.json lives at <RunRoot>/pipeline/research.json
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

// TestWriteCLIManifestForPublish_HydratesFromManifestRunIDAcrossScopes covers
// the deferred-publish flow: generate wrote a manifest with run_id under one
// PRESS_SCOPE, then promote runs from a different shell/cwd with a different
// scope. The source manifest's run_id must still let promote find research.json.
func TestWriteCLIManifestForPublish_HydratesFromManifestRunIDAcrossScopes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "publish-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	runID := "20260508-070627"
	workingDir := filepath.Join(tmp, "working", "test-api-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, WriteCLIManifest(workingDir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "test-api",
		CLIName:       "test-api-pp-cli",
		RunID:         runID,
	}))

	built := []NovelFeature{
		{Name: "Cross-scope feature", Command: "feature", Description: "Loaded from the original run scope."},
	}
	originalRunRoot := filepath.Join(RunstateRoot(), "generate-scope", "runs", runID)
	writeResearchAt(t, originalRunRoot, &ResearchResult{
		APIName:            "test-api",
		NovelFeaturesBuilt: &built,
	})

	state := NewMinimalState("test-api-pp-cli", workingDir)
	require.Empty(t, state.RunID, "current scope has no registry entry")

	require.NoError(t, writeCLIManifestForPublish(state, workingDir))

	m := readPublishedManifest(t, workingDir)
	assert.Equal(t, runID, m.RunID)
	require.Len(t, m.NovelFeatures, 1)
	assert.Equal(t, "feature", m.NovelFeatures[0].Command)
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

	// No research.json anywhere. Publish should preserve the carry-forward value
	// without warning that manual enrichment is required.
	var stderr string
	require.NoError(t, captureStderr(t, &stderr, func() error {
		return writeCLIManifestForPublish(state, state.WorkingDir)
	}))

	m := readPublishedManifest(t, state.WorkingDir)
	require.Len(t, m.NovelFeatures, 1, "carry-forward should preserve generate-time novel_features")
	assert.Equal(t, "today", m.NovelFeatures[0].Command)
	assert.NotContains(t, stderr, "manifest will require manual enrichment before publish")
	assert.Contains(t, stderr, "preserving existing novel_features")
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
	var stderr string
	require.NoError(t, captureStderr(t, &stderr, func() error {
		return writeCLIManifestForPublish(state, state.WorkingDir)
	}))

	m := readPublishedManifest(t, state.WorkingDir)
	assert.Empty(t, m.NovelFeatures, "no novel_features when neither research nor prior manifest has any")
	assert.Contains(t, stderr, "warning: could not locate originating run's research.json at "+
		filepath.Join(state.RunRoot(), "research.json")+" or "+
		filepath.Join(state.PipelineDir(), "research.json"))
	assert.Contains(t, stderr, "manifest will require manual enrichment before publish")
	assert.NotContains(t, stderr, "read failed")
	assert.NotContains(t, stderr, "failed to parse")
}

func TestWriteCLIManifestForPublishReportsMalformedResearchJSON(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-bad-research")

	runRoot := RunRoot(state.RunID)
	require.NoError(t, os.MkdirAll(runRoot, 0o755))
	path := filepath.Join(runRoot, "research.json")
	body := []byte(`{"api_name":"test-api","novel_features":[{"name":1}]}`)
	require.NoError(t, os.WriteFile(path, body, 0o644))

	var stderr string
	require.NoError(t, captureStderr(t, &stderr, func() error {
		return writeCLIManifestForPublish(state, state.WorkingDir)
	}))

	assert.Contains(t, stderr, "debug: research.json at "+path+" failed to parse:")
	assert.NotContains(t, stderr, "research.json not found at")
}

func TestWriteCLIManifestForPublishReportsUnreadableResearchJSON(t *testing.T) {
	_, state := publishManifestEnvSetup(t, "20260427-unreadable-research")

	runRoot := RunRoot(state.RunID)
	path := filepath.Join(runRoot, "research.json")
	require.NoError(t, os.MkdirAll(path, 0o755))

	var stderr string
	require.NoError(t, captureStderr(t, &stderr, func() error {
		return writeCLIManifestForPublish(state, state.WorkingDir)
	}))

	assert.Contains(t, stderr, "debug: research.json at "+path+" read failed:")
	assert.NotContains(t, stderr, "research.json not found at")
}
