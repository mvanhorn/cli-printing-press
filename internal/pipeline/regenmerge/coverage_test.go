package regenmerge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerdictPublishedOnlyTemplatedFires builds a synthetic fixture where a
// .go file with the templated marker exists in published but not in fresh —
// the template stopped emitting it. Verdict must be PUBLISHED-ONLY-TEMPLATED
// so the human can decide to delete or keep.
func TestVerdictPublishedOnlyTemplatedFires(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := buildSyntheticFixture(t, map[string]string{
		"go.mod":                "module example.com/x\ngo 1.22\n",
		"internal/cli/stale.go": templatedHeader + "package cli\n\nfunc Stale() {}\n",
	}, map[string]string{
		"go.mod": "module example.com/x\ngo 1.22\n",
	})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)
	assert.Equal(t, VerdictPublishedOnlyTemplated, verdicts["internal/cli/stale.go"],
		"file with marker present only in published is stale template emission")
}

// TestVerdictNovelCollisionFires builds a fixture where both trees have a
// file at the same path, neither carries the marker, and decl-sets are
// disjoint. This is a coincidental path collision; published wins.
func TestVerdictNovelCollisionFires(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := buildSyntheticFixture(t, map[string]string{
		"go.mod":                  "module example.com/x\ngo 1.22\n",
		"internal/cli/collide.go": "package cli\n\nfunc HandWritten() {}\n",
	}, map[string]string{
		"go.mod":                  "module example.com/x\ngo 1.22\n",
		"internal/cli/collide.go": "package cli\n\nfunc Generated() {}\n",
	})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)
	assert.Equal(t, VerdictNovelCollision, verdicts["internal/cli/collide.go"],
		"disjoint decl-sets with no marker = NOVEL-COLLISION")
}

// TestApplyRejectsDirtyGitTreeWithoutForce confirms the documented contract:
// --apply on a dirty git tree returns an error pointing at --force.
func TestApplyRejectsDirtyGitTreeWithoutForce(t *testing.T) {
	t.Parallel()

	pubDir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", pubDir, "init", "-q").Run())
	require.NoError(t, exec.Command("git", "-C", pubDir, "config", "user.email", "test@example.com").Run())
	require.NoError(t, exec.Command("git", "-C", pubDir, "config", "user.name", "test").Run())
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "uncommitted.go"), []byte("package x\n"), 0o644))

	report := &MergeReport{CLIDir: pubDir}
	err := Apply(report, Options{Force: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes",
		"dirty git tree must be rejected without --force")
}

// TestApplyRejectsNonRepoWithoutForce confirms the tightened contract: a
// directory that's not a git repo also fails the precondition without
// --force, since silently allowing it bypasses the documented safety check.
func TestApplyRejectsNonRepoWithoutForce(t *testing.T) {
	t.Parallel()

	pubDir := t.TempDir() // not a git repo

	report := &MergeReport{CLIDir: pubDir}
	err := Apply(report, Options{Force: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git status failed",
		"non-repo dir must fail the assertGitClean precondition without --force")
}

// TestInjectAddCommandsFailsWithoutHostFn confirms the failure mode when a
// LostRegistration points at a host file whose AddCommand-bearing function
// has been removed. Apply must surface the error rather than silently skip.
func TestInjectAddCommandsFailsWithoutHostFn(t *testing.T) {
	t.Parallel()

	hostPath := filepath.Join(t.TempDir(), "no_host_fn.go")
	require.NoError(t, os.WriteFile(hostPath, []byte("package cli\n\nfunc unrelated() {}\n"), 0o644))

	err := injectAddCommands(hostPath, []string{"cmd.AddCommand(newFooCmd(flags))"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no function with AddCommand",
		"missing host function must surface as a clear error")
}

// TestMergeReportJSONShapeStable pins the JSON contract for the agent surface.
// This isn't a golden file (the test would need updates with every fixture
// tweak); it pins the field names that downstream agents branch on.
func TestMergeReportJSONShapeStable(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := postmanFixture(t)
	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	data, err := json.Marshal(report)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	// Top-level keys agents key off.
	for _, k := range []string{"cli_dir", "fresh_dir", "applied", "files"} {
		assert.Contains(t, raw, k, "MergeReport JSON must keep field %q", k)
	}

	// Per-file fields.
	files, ok := raw["files"].([]any)
	require.True(t, ok, "files must be a JSON array")
	require.NotEmpty(t, files)
	first, ok := files[0].(map[string]any)
	require.True(t, ok)
	for _, k := range []string{"path", "verdict", "applied"} {
		assert.Contains(t, first, k, "FileClassification JSON must keep field %q", k)
	}

	// Verdict values are stable strings, not enums.
	assert.IsType(t, "", first["verdict"], "verdict must serialize as a string")
}

// --- helpers ---

const templatedHeader = `// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

`

// buildSyntheticFixture writes the given path-content map into pub/ and fresh/
// subdirs of a tempdir and returns their absolute paths. Useful for verdicts
// that don't warrant a full testdata/ fixture.
func buildSyntheticFixture(t *testing.T, pubFiles, freshFiles map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	pubDir := filepath.Join(root, "published")
	freshDir := filepath.Join(root, "fresh")
	for _, layout := range []struct {
		dir   string
		files map[string]string
	}{
		{pubDir, pubFiles},
		{freshDir, freshFiles},
	} {
		for rel, content := range layout.files {
			full := filepath.Join(layout.dir, rel)
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
		}
	}
	return pubDir, freshDir
}
