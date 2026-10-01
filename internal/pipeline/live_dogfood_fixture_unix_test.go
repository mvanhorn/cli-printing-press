//go:build !windows

package pipeline

import (
	"fmt"
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

func TestCopyCLIDirFixturesSkipsLinkCyclesAndCapsFanout(t *testing.T) {
	cliDir := t.TempDir()
	scratch := t.TempDir()
	set := filepath.Join(cliDir, "set")
	require.NoError(t, os.MkdirAll(set, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(set, "a.txt"), []byte("a"), 0o644))
	// Ten links back to the same directory would copy 10^8 trees without a
	// cycle check.
	for i := range 10 {
		require.NoError(t, os.Symlink(".", filepath.Join(set, "loop"+string(rune('0'+i)))))
	}
	done := make(chan error, 1)
	go func() {
		done <- copyCLIDirFixtures([]string{"cmd", "set"}, 1, cliDir, scratch)
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("fixture copy did not bound linked fan-out")
	}
	got, err := os.ReadFile(filepath.Join(scratch, "set", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(got))
	_, err = os.Stat(filepath.Join(scratch, "set", "loop0", "a.txt"))
	assert.True(t, os.IsNotExist(err), "a link back to an ancestor directory must not be copied again")
}

func TestCopyCLIDirFixturesFailsOverBudget(t *testing.T) {
	cliDir := t.TempDir()
	scratch := t.TempDir()
	set := filepath.Join(cliDir, "set")
	require.NoError(t, os.MkdirAll(set, 0o755))
	for i := range liveDogfoodFixtureMaxFiles + 1 {
		require.NoError(t, os.WriteFile(filepath.Join(set, fmt.Sprintf("f%05d", i)), nil, 0o644))
	}
	err := copyCLIDirFixtures([]string{"cmd", "set"}, 1, cliDir, scratch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestCopyCLIDirFixturesSharesBudgetAcrossArgs(t *testing.T) {
	cliDir := t.TempDir()
	scratch := t.TempDir()
	half := liveDogfoodFixtureMaxFiles/2 + 1
	for _, dir := range []string{"one", "two"} {
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, dir), 0o755))
		for i := range half {
			require.NoError(t, os.WriteFile(filepath.Join(cliDir, dir, fmt.Sprintf("f%05d", i)), nil, 0o644))
		}
	}
	require.NoError(t, copyCLIDirFixtures([]string{"cmd", "one"}, 1, cliDir, scratch))
	err := copyCLIDirFixtures([]string{"cmd", "one", "two"}, 1, cliDir, t.TempDir())
	require.Error(t, err, "two fixture args together exceed the one shared budget")
	assert.Contains(t, err.Error(), "exceeds")
}

func TestCopyCLIDirFixturesChargesRepeatedFixtureOnce(t *testing.T) {
	cliDir := t.TempDir()
	scratch := t.TempDir()
	set := filepath.Join(cliDir, "set")
	require.NoError(t, os.MkdirAll(set, 0o755))
	for i := range liveDogfoodFixtureMaxFiles/2 + 1 {
		require.NoError(t, os.WriteFile(filepath.Join(set, fmt.Sprintf("f%05d", i)), nil, 0o644))
	}
	// Named twice, the set is still under the cap once.
	require.NoError(t, copyCLIDirFixtures([]string{"cmd", "./set", "--images=@set"}, 1, cliDir, scratch))
	_, err := os.Stat(filepath.Join(scratch, "set", "f00000"))
	require.NoError(t, err)
}
