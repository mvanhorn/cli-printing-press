//go:build !windows

package pipeline

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyCLIDirFixturesFollowsInTreeSymlinksAndSkipsSpecialFiles(t *testing.T) {
	cliDir := t.TempDir()
	scratch := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "real.png"), []byte("img"), 0o644))
	require.NoError(t, os.Symlink("real.png", filepath.Join(cliDir, "link.png")))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(cliDir, "escape.txt")))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "set"), 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(cliDir, "set", "pipe"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "set", "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "shared", "inner"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "shared", "inner", "b.txt"), []byte("b"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join("..", "shared"), filepath.Join(cliDir, "set", "linked")))

	done := make(chan error, 1)
	go func() {
		done <- copyCLIDirFixtures([]string{"cmd", "link.png", "escape.txt", "set"}, 1, cliDir, scratch)
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("fixture copy blocked on a special file")
	}
	got, err := os.ReadFile(filepath.Join(scratch, "link.png"))
	require.NoError(t, err)
	assert.Equal(t, "img", string(got))
	_, err = os.Stat(filepath.Join(scratch, "escape.txt"))
	assert.True(t, os.IsNotExist(err), "symlink leaving the CLI dir must not be followed")
	_, err = os.Stat(filepath.Join(scratch, "set", "pipe"))
	assert.True(t, os.IsNotExist(err), "FIFOs must not be copied")
	got, err = os.ReadFile(filepath.Join(scratch, "set", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(got))
	got, err = os.ReadFile(filepath.Join(scratch, "set", "linked", "inner", "b.txt"))
	require.NoError(t, err, "an in-tree directory symlink inside a fixture dir must be copied")
	assert.Equal(t, "b", string(got))
}
