//go:build !windows

package pipeline

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromoteWorkingCLI_PathARefusesFifoGitMetadata(t *testing.T) {
	t.Run("top-level", func(t *testing.T) {
		f := newPromoteGitFixture(t, false)
		require.NoError(t, syscall.Mkfifo(filepath.Join(f.workDir, ".git"), 0o644))

		err := f.promote(t)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a regular file or directory")

		_, statErr := os.Stat(f.libDir)
		assert.True(t, os.IsNotExist(statErr))
		info, err := os.Lstat(filepath.Join(f.workDir, ".git"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&os.ModeNamedPipe)
		assertNoPromoteDebris(t, f.libDir)
	})

	t.Run("inside git dir", func(t *testing.T) {
		f := newPromoteGitFixture(t, false)
		head := initGitRepository(t, f.workDir)
		require.NoError(t, syscall.Mkfifo(filepath.Join(f.workDir, ".git", "blocked"), 0o644))

		err := f.promote(t)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a regular file or directory")

		_, statErr := os.Stat(f.libDir)
		assert.True(t, os.IsNotExist(statErr))
		assertGitHistory(t, f.workDir, head, "1")
		info, err := os.Lstat(filepath.Join(f.workDir, ".git", "blocked"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&os.ModeNamedPipe)
		assertNoPromoteDebris(t, f.libDir)
	})
}
