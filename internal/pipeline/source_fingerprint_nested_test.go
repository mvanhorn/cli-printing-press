package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gitSHA1   = "0123456789abcdef0123456789abcdef01234567"
	gitSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func writeTreeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func newFingerprintTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTreeFile(t, filepath.Join(root, "go.mod"), "module example\n")
	writeTreeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeTreeFile(t, filepath.Join(root, "internal", "cli", "root.go"), "package cli\n")
	return root
}

func fsIsCaseSensitive(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	lower := filepath.Join(dir, "probe-a")
	upper := filepath.Join(dir, "probe-A")
	require.NoError(t, os.WriteFile(lower, []byte("a"), 0o644))
	f, err := os.OpenFile(upper, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false
	}
	require.NoError(t, f.Close())
	return true
}

func TestCaptureSourceFingerprintNestedTreesDoNotChangeDigest(t *testing.T) {
	root := newFingerprintTree(t)
	before, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Contains(t, before.Files, "go.mod")
	assert.Contains(t, before.Files, "main.go")
	assert.Contains(t, before.Files, "internal/cli/root.go")

	worktree := filepath.Join(root, ".claude", "worktrees", "reprint")
	writeTreeFile(t, filepath.Join(worktree, ".git"), "gitdir: ../repo.git/worktrees/reprint\n")
	writeTreeFile(t, filepath.Join(worktree, "cmd", "reprint", "main.go"), "package main\n")
	writeTreeFile(t, filepath.Join(root, "tools", "go.mod"), "module tools\n")
	writeTreeFile(t, filepath.Join(root, "tools", "main.go"), "package main\n")
	writeTreeFile(t, filepath.Join(root, "tools", "sub", "extra.go"), "package sub\n")
	writeTreeFile(t, filepath.Join(root, "deps", "lib", ".git", "HEAD"), "ref: refs/heads/main\n")
	writeTreeFile(t, filepath.Join(root, "deps", "lib", "lib.go"), "package lib\n")
	writeTreeFile(t, filepath.Join(root, "deps", "sha", ".git", "HEAD"), gitSHA1+"\n")
	writeTreeFile(t, filepath.Join(root, "deps", "sha", "sha.go"), "package sha\n")
	writeTreeFile(t, filepath.Join(root, "deps", "sha256", ".git", "HEAD"), gitSHA256+"\n")
	writeTreeFile(t, filepath.Join(root, "deps", "sha256", "sha.go"), "package sha256\n")
	writeTreeFile(t, filepath.Join(root, "deps", "padded", ".git"), "\n  gitdir: ../repo.git/worktrees/padded\n")
	writeTreeFile(t, filepath.Join(root, "deps", "padded", "padded.go"), "package padded\n")

	after, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Equal(t, before.Digest, after.Digest)
	assert.Equal(t, before.Files, after.Files)
	for _, hidden := range []string{
		".claude/worktrees/reprint/cmd/reprint/main.go",
		"tools/go.mod",
		"tools/main.go",
		"tools/sub/extra.go",
		"deps/lib/lib.go",
		"deps/sha/sha.go",
		"deps/sha256/sha.go",
		"deps/padded/padded.go",
	} {
		assert.NotContains(t, after.Files, hidden)
	}

	writeTreeFile(t, filepath.Join(root, ".claude", "helper.go"), "package helper\n")
	withHelper, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Contains(t, withHelper.Files, ".claude/helper.go")
	assert.Contains(t, withHelper.Files, "internal/cli/root.go")
	assert.NotEqual(t, before.Digest, withHelper.Digest)

	writeTreeFile(t, filepath.Join(worktree, "cmd", "reprint", "main.go"), "package main\n\nvar changed = 1\n")
	writeTreeFile(t, filepath.Join(root, "tools", "main.go"), "package main\n\nvar changed = 1\n")
	still, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Equal(t, withHelper.Digest, still.Digest)

	writeTreeFile(t, filepath.Join(root, "internal", "cli", "root.go"), "package cli\n\nvar changed = 1\n")
	drifted, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.NotEqual(t, still.Digest, drifted.Digest)
	assert.NotEqual(t, still.Files["internal/cli/root.go"], drifted.Files["internal/cli/root.go"])
}

func TestValidatePhase5GateIgnoresNestedTreesAndRejectsRealEdits(t *testing.T) {
	cliDir := newFingerprintTree(t)
	source, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	proofsDir := t.TempDir()
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "test",
		RunID:             "run-1",
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: source.Digest,
		SourceFiles:       source.Files,
		AuthContext:       Phase5AuthContext{Type: "none"},
	})
	manifest := CLIManifest{APIName: "test", RunID: "run-1", AuthType: "none"}

	writeTreeFile(t, filepath.Join(cliDir, ".claude", "worktrees", "reprint", ".git"), "gitdir: ../repo.git/worktrees/reprint\n")
	writeTreeFile(t, filepath.Join(cliDir, ".claude", "worktrees", "reprint", "cmd", "main.go"), "package main\n")
	writeTreeFile(t, filepath.Join(cliDir, "tools", "go.mod"), "module tools\n")
	writeTreeFile(t, filepath.Join(cliDir, "tools", "main.go"), "package main\n")

	result := ValidatePhase5Gate(proofsDir, manifest, cliDir)
	require.True(t, result.Passed, result.Detail)

	writeTreeFile(t, filepath.Join(cliDir, "internal", "cli", "root.go"), "package cli\n\nvar changed = 1\n")
	result = ValidatePhase5Gate(proofsDir, manifest, cliDir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "internal/cli/root.go")

	writeTreeFile(t, filepath.Join(cliDir, "internal", "util", "util.go"), "package util\n")
	result = ValidatePhase5Gate(proofsDir, manifest, cliDir)
	assert.Contains(t, result.Detail, "internal/util/util.go")
}

func TestCaptureSourceFingerprintHashesRootDespiteGitMetadata(t *testing.T) {
	t.Run("gitdir file", func(t *testing.T) {
		root := newFingerprintTree(t)
		writeTreeFile(t, filepath.Join(root, ".git"), "gitdir: ../repo.git/worktrees/cli\n")
		assertRootSourceStillTracked(t, root)
	})
	t.Run("git directory", func(t *testing.T) {
		root := newFingerprintTree(t)
		writeTreeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		assertRootSourceStillTracked(t, root)
	})
}

func assertRootSourceStillTracked(t *testing.T, root string) {
	t.Helper()
	before, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Contains(t, before.Files, "main.go")
	assert.Contains(t, before.Files, "go.mod")
	assert.Contains(t, before.Files, "internal/cli/root.go")

	writeTreeFile(t, filepath.Join(root, "main.go"), "package main\n\nvar changed = 1\n")
	after, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.NotEqual(t, before.Digest, after.Digest)
	assert.NotEqual(t, before.Files["main.go"], after.Files["main.go"])
}

func TestCaptureSourceFingerprintDecoysDoNotHidePackages(t *testing.T) {
	outside := t.TempDir()
	writeTreeFile(t, filepath.Join(outside, "go.mod"), "module hidden\n")
	writeTreeFile(t, filepath.Join(outside, "gitdir"), "gitdir: ../repo.git/worktrees/hidden\n")
	writeTreeFile(t, filepath.Join(outside, "HEAD"), "ref: refs/heads/main\n")

	tests := []struct {
		name  string
		plant func(t *testing.T, pkg string)
	}{
		{
			name: "git file without gitdir prefix",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git"), "not a checkout\n")
			},
		},
		{
			name: "empty git file",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git"), "")
			},
		},
		{
			name: "gitdir prefix only in the middle",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git"), "note\ngitdir: ../x\n")
			},
		},
		{
			name: "uppercase gitdir prefix",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git"), "GITDIR: ../x\n")
			},
		},
		{
			name: "git dir without HEAD",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "config"), "[core]\n")
			},
		},
		{
			name: "git dir with invalid HEAD",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), "hello\n")
			},
		},
		{
			name: "git dir with empty HEAD",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), "")
			},
		},
		{
			name: "HEAD ref missing the required space",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), "ref:refs/heads/main\n")
			},
		},
		{
			name: "HEAD ref with no target",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), "ref:    \n")
			},
		},
		{
			name: "HEAD is not a sha",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), "gggggggggggggggggggggggggggggggggggggggg\n")
			},
		},
		{
			name: "HEAD sha is the wrong length",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD"), gitSHA1[:39]+"\n")
			},
		},
		{
			name: "HEAD is a directory",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, ".git", "HEAD", "keep"), "ref: refs/heads/main\n")
			},
		},
		{
			name: "symlink git file",
			plant: func(t *testing.T, pkg string) {
				require.NoError(t, os.Symlink(filepath.Join(outside, "gitdir"), filepath.Join(pkg, ".git")))
			},
		},
		{
			name: "symlink HEAD",
			plant: func(t *testing.T, pkg string) {
				require.NoError(t, os.MkdirAll(filepath.Join(pkg, ".git"), 0o755))
				require.NoError(t, os.Symlink(filepath.Join(outside, "HEAD"), filepath.Join(pkg, ".git", "HEAD")))
			},
		},
		{
			name: "symlink go.mod",
			plant: func(t *testing.T, pkg string) {
				require.NoError(t, os.Symlink(filepath.Join(outside, "go.mod"), filepath.Join(pkg, "go.mod")))
			},
		},
		{
			name: "go.mod directory",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, "go.mod", "keep.txt"), "not a module file\n")
			},
		},
		{
			name: "go.sum without go.mod",
			plant: func(t *testing.T, pkg string) {
				writeTreeFile(t, filepath.Join(pkg, "go.sum"), "sum\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newFingerprintTree(t)
			pkg := filepath.Join(root, "internal", "cli")
			tt.plant(t, pkg)

			before, err := CaptureSourceFingerprint(root)
			require.NoError(t, err)
			assert.Contains(t, before.Files, "internal/cli/root.go")
			assert.Contains(t, before.Files, "main.go")
			assert.Contains(t, before.Files, "go.mod")

			writeTreeFile(t, filepath.Join(pkg, "root.go"), "package cli\n\nvar changed = 1\n")
			after, err := CaptureSourceFingerprint(root)
			require.NoError(t, err)
			assert.NotEqual(t, before.Digest, after.Digest)
			assert.NotEqual(t, before.Files["internal/cli/root.go"], after.Files["internal/cli/root.go"])
		})
	}
}

func TestCaptureSourceFingerprintDoesNotFollowSymlinkedDirs(t *testing.T) {
	root := newFingerprintTree(t)
	outside := t.TempDir()
	writeTreeFile(t, filepath.Join(outside, "sneaky.go"), "package sneaky\n")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))
	require.NoError(t, os.Symlink(filepath.Join(root, "internal", "cli"), filepath.Join(root, "alias")))

	before, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.NotContains(t, before.Files, "linked/sneaky.go")
	assert.NotContains(t, before.Files, "alias/root.go")
	assert.Contains(t, before.Files, "internal/cli/root.go")

	writeTreeFile(t, filepath.Join(outside, "sneaky.go"), "package sneaky\n\nvar changed = 1\n")
	after, err := CaptureSourceFingerprint(root)
	require.NoError(t, err)
	assert.Equal(t, before.Digest, after.Digest)
}

func TestCaptureSourceFingerprintCaseLookalikes(t *testing.T) {
	t.Run("GO.MOD", func(t *testing.T) {
		root := newFingerprintTree(t)
		pkg := filepath.Join(root, "internal", "cli")
		writeTreeFile(t, filepath.Join(pkg, "GO.MOD"), "module hidden\n")
		source, err := CaptureSourceFingerprint(root)
		require.NoError(t, err)
		_, folded := os.Lstat(filepath.Join(pkg, "go.mod"))
		if folded == nil && !fsIsCaseSensitive(t) {
			assert.NotContains(t, source.Files, "internal/cli/root.go")
			return
		}
		require.Error(t, folded)
		assert.Contains(t, source.Files, "internal/cli/root.go")
		writeTreeFile(t, filepath.Join(pkg, "root.go"), "package cli\n\nvar changed = 1\n")
		after, err := CaptureSourceFingerprint(root)
		require.NoError(t, err)
		assert.NotEqual(t, source.Files["internal/cli/root.go"], after.Files["internal/cli/root.go"])
	})

	t.Run("dot GIT directory", func(t *testing.T) {
		root := newFingerprintTree(t)
		pkg := filepath.Join(root, "internal", "cli")
		writeTreeFile(t, filepath.Join(pkg, ".GIT", "HEAD"), "ref: refs/heads/main\n")
		source, err := CaptureSourceFingerprint(root)
		require.NoError(t, err)
		_, folded := os.Lstat(filepath.Join(pkg, ".git", "HEAD"))
		if folded == nil && !fsIsCaseSensitive(t) {
			assert.NotContains(t, source.Files, "internal/cli/root.go")
			return
		}
		require.Error(t, folded)
		assert.Contains(t, source.Files, "internal/cli/root.go")
	})

	t.Run("dot Git file", func(t *testing.T) {
		root := newFingerprintTree(t)
		pkg := filepath.Join(root, "internal", "cli")
		writeTreeFile(t, filepath.Join(pkg, ".Git"), "gitdir: ../x\n")
		source, err := CaptureSourceFingerprint(root)
		require.NoError(t, err)
		_, folded := os.Lstat(filepath.Join(pkg, ".git"))
		if folded == nil && !fsIsCaseSensitive(t) {
			assert.NotContains(t, source.Files, "internal/cli/root.go")
			return
		}
		require.Error(t, folded)
		assert.Contains(t, source.Files, "internal/cli/root.go")
	})
}

func TestSourceFingerprintGitMarkersRejectMissingPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	assert.False(t, sourceFingerprintGitdirFile(missing))
	assert.False(t, sourceFingerprintGitHEAD(missing))
	assert.False(t, sourceFingerprintModuleRoot(t.TempDir()))
	assert.False(t, sourceFingerprintGitCheckout(t.TempDir()))
	assert.False(t, sourceFingerprintSeparateTree(t.TempDir()))
}

func TestSourceFingerprintGitHEADText(t *testing.T) {
	assert.True(t, sourceFingerprintGitHEADText("ref: refs/heads/main\n"))
	assert.True(t, sourceFingerprintGitHEADText(gitSHA1))
	assert.True(t, sourceFingerprintGitHEADText(gitSHA256+"\n"))
	assert.False(t, sourceFingerprintGitHEADText(""))
	assert.False(t, sourceFingerprintGitHEADText("ref:"))
	assert.False(t, sourceFingerprintGitHEADText("ref:    "))
	assert.False(t, sourceFingerprintGitHEADText(gitSHA1[:39]))
	assert.False(t, sourceFingerprintGitHEADText("gggggggggggggggggggggggggggggggggggggggg"))
}
