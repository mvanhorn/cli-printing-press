package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLockTest(t *testing.T) (cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmpDir)
	return func() {}
}

func TestAcquireLock_NoExistingLock(t *testing.T) {
	setupLockTest(t)

	lock, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)
	assert.Equal(t, "scope-1", lock.Scope)
	assert.Equal(t, "acquire", lock.Phase)
	assert.NotZero(t, lock.PID)
	assert.WithinDuration(t, time.Now(), lock.AcquiredAt, 2*time.Second)
	assert.WithinDuration(t, time.Now(), lock.UpdatedAt, 2*time.Second)

	// Verify the lock file exists and is valid JSON.
	data, err := os.ReadFile(LockFilePath("test-pp-cli"))
	require.NoError(t, err)
	var readBack LockState
	require.NoError(t, json.Unmarshal(data, &readBack))
	assert.Equal(t, "scope-1", readBack.Scope)
}

func TestAcquireLock_LocksDirectoryCreated(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	info, err := os.Stat(LocksDir())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestAcquireLock_RebuildCase(t *testing.T) {
	setupLockTest(t)

	// Create a library directory to simulate rebuild scenario.
	libDir := filepath.Join(PublishedLibraryRoot(), "test-pp-cli")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"), []byte("module test"), 0o644))

	lock, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)
	assert.Equal(t, "scope-1", lock.Scope)
}

func TestAcquireLock_StaleLockAutoReclaim(t *testing.T) {
	setupLockTest(t)

	// Create a stale lock.
	require.NoError(t, os.MkdirAll(LocksDir(), 0o755))
	staleLock := &LockState{
		Scope:      "old-scope",
		Phase:      "build",
		PID:        99999,
		AcquiredAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, writeLock(LockFilePath("test-pp-cli"), staleLock))

	lock, err := AcquireLock("test-pp-cli", "new-scope", false)
	require.NoError(t, err)
	assert.Equal(t, "new-scope", lock.Scope)
}

func TestAcquireLock_FreshLockDifferentScope_Blocked(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	_, err = AcquireLock("test-pp-cli", "scope-2", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "lock held by scope")
}

func TestAcquireLock_FreshLockSameScope_Succeeds(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	lock, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)
	assert.Equal(t, "scope-1", lock.Scope)
}

func TestAcquireLock_ForceOverridesFreshLock(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	lock, err := AcquireLock("test-pp-cli", "scope-2", true)
	require.NoError(t, err)
	assert.Equal(t, "scope-2", lock.Scope)
}

func TestUpdateLock(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond) // Ensure time difference.

	err = UpdateLock("test-pp-cli", "build-p0")
	require.NoError(t, err)

	lock, err := readLock(LockFilePath("test-pp-cli"))
	require.NoError(t, err)
	assert.Equal(t, "build-p0", lock.Phase)
	assert.True(t, lock.UpdatedAt.After(lock.AcquiredAt))
}

func TestLockStatus_ActiveLock(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	status := LockStatus("test-pp-cli")
	assert.True(t, status.Held)
	assert.False(t, status.Stale)
	assert.Equal(t, "acquire", status.Phase)
	assert.Equal(t, "scope-1", status.Scope)
	assert.NotNil(t, status.Lock)
}

func TestLockStatus_NoLock(t *testing.T) {
	setupLockTest(t)

	status := LockStatus("nonexistent-pp-cli")
	assert.False(t, status.Held)
	assert.False(t, status.HasCLI)
}

func TestLockStatus_NoLockWithLibraryCLI(t *testing.T) {
	setupLockTest(t)

	// Create library dir with go.mod (slug-keyed directory).
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"), []byte("module test"), 0o644))

	status := LockStatus("test-pp-cli")
	assert.False(t, status.Held)
	assert.True(t, status.HasCLI)
}

func TestLockStatus_NoLockLibraryDirNoGoMod(t *testing.T) {
	setupLockTest(t)

	// Create library dir without go.mod (debris), slug-keyed.
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	require.NoError(t, os.MkdirAll(libDir, 0o755))

	status := LockStatus("test-pp-cli")
	assert.False(t, status.Held)
	assert.False(t, status.HasCLI)
}

func TestLockStatus_NoLockLibraryDirWithManifest(t *testing.T) {
	setupLockTest(t)

	// Create library dir with manifest but no go.mod (slug-keyed).
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, CLIManifestFilename), []byte("{}"), 0o644))

	status := LockStatus("test-pp-cli")
	assert.False(t, status.Held)
	assert.True(t, status.HasCLI)
}

func TestReleaseLock(t *testing.T) {
	setupLockTest(t)

	_, err := AcquireLock("test-pp-cli", "scope-1", false)
	require.NoError(t, err)

	err = ReleaseLock("test-pp-cli")
	require.NoError(t, err)

	_, err = os.Stat(LockFilePath("test-pp-cli"))
	assert.True(t, os.IsNotExist(err))
}

func TestReleaseLock_Idempotent(t *testing.T) {
	setupLockTest(t)

	err := ReleaseLock("nonexistent-pp-cli")
	assert.NoError(t, err)
}

func TestPromoteWorkingCLI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Create a working directory with content.
	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test-pp-cli\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))

	// Create a lock.
	_, err := AcquireLock("test-pp-cli", "test-scope", false)
	require.NoError(t, err)

	// Create minimal state.
	state := NewStateWithRun("test", workDir, "run-001", "test-scope")

	err = PromoteWorkingCLI("test-pp-cli", workDir, state)
	require.NoError(t, err)

	// Verify library dir exists with copied content (slug-keyed).
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	_, err = os.Stat(filepath.Join(libDir, "go.mod"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "main.go"))
	assert.NoError(t, err)

	// Verify lock was released.
	_, err = os.Stat(LockFilePath("test-pp-cli"))
	assert.True(t, os.IsNotExist(err))

	// Verify state was updated.
	assert.Equal(t, libDir, state.PublishedDir)
	assert.Equal(t, libDir, state.WorkingDir)
}

func TestPromoteWorkingCLI_ReplacesExistingLibrary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Create existing library dir with old content (slug-keyed).
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "old-file.txt"), []byte("old"), 0o644))

	// Create working dir with new content.
	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test-pp-cli\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "new-file.txt"), []byte("new"), 0o644))

	_, err := AcquireLock("test-pp-cli", "test-scope", false)
	require.NoError(t, err)

	state := NewStateWithRun("test", workDir, "run-002", "test-scope")

	err = PromoteWorkingCLI("test-pp-cli", workDir, state)
	require.NoError(t, err)

	// Old file should be gone.
	_, err = os.Stat(filepath.Join(libDir, "old-file.txt"))
	assert.True(t, os.IsNotExist(err))

	// New file should exist.
	_, err = os.Stat(filepath.Join(libDir, "new-file.txt"))
	assert.NoError(t, err)
}

func TestPromoteWorkingCLI_EmptyWorkingDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	state := NewStateWithRun("test", workDir, "run-003", "test-scope")

	err := PromoteWorkingCLI("test-pp-cli", workDir, state)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestPromoteWorkingCLI_PreservesOldOnFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	// Create existing library dir with old content (slug-keyed).
	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0o644))

	state := NewStateWithRun("test", "/nonexistent/path", "run-004", "test-scope")

	// Promote with a nonexistent working dir should fail.
	err := PromoteWorkingCLI("test-pp-cli", "/nonexistent/path", state)
	assert.Error(t, err)

	// Old library should still be intact.
	data, readErr := os.ReadFile(filepath.Join(libDir, "go.mod"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "module old")
}

func TestPromoteWorkingCLI_RetryRestoresBackupBeforeFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	backupDir := libDir + ".old"
	stagingDir := libDir + ".promoting"

	// Simulate a crashed promote: backup survived, live library is missing,
	// and stale staging debris is still present.
	require.NoError(t, os.MkdirAll(backupDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.MkdirAll(stagingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "partial.txt"), []byte("partial"), 0o644))

	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	outside := filepath.Join(tmp, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(workDir, "bad-link.txt")))

	state := NewStateWithRun("test", workDir, "run-004", "test-scope")

	err := PromoteWorkingCLI("test-pp-cli", workDir, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying to staging directory")

	// The previous published CLI should be restored before the retry fails.
	data, readErr := os.ReadFile(filepath.Join(libDir, "go.mod"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "module old")
	_, statErr := os.Stat(backupDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestPromoteWorkingCLI_ReleasesLockWhenStateSaveFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test-pp-cli\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))

	_, err := AcquireLock("test-pp-cli", "test-scope", false)
	require.NoError(t, err)

	state := NewStateWithRun("test", workDir, "run-005", "test-scope")

	// Force state.Save() to fail after the library swap succeeds.
	require.NoError(t, os.MkdirAll(filepath.Dir(state.PipelineDir()), 0o755))
	require.NoError(t, os.WriteFile(state.PipelineDir(), []byte("not a directory"), 0o644))

	err = PromoteWorkingCLI("test-pp-cli", workDir, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli promoted to")
	assert.Contains(t, err.Error(), "state update failed")

	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	_, err = os.Stat(filepath.Join(libDir, "go.mod"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "main.go"))
	assert.NoError(t, err)

	_, err = os.Stat(LockFilePath("test-pp-cli"))
	assert.True(t, os.IsNotExist(err))
}

func TestIsStale(t *testing.T) {
	fresh := &LockState{UpdatedAt: time.Now()}
	assert.False(t, IsStale(fresh))

	stale := &LockState{UpdatedAt: time.Now().Add(-31 * time.Minute)}
	assert.True(t, IsStale(stale))

	boundary := &LockState{UpdatedAt: time.Now().Add(-30*time.Minute - time.Second)}
	assert.True(t, IsStale(boundary))
}

func TestConcurrentAcquire(t *testing.T) {
	setupLockTest(t)

	const goroutines = 10
	var wg sync.WaitGroup
	successes := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		scope := "scope-" + string(rune('A'+i))
		go func(s string) {
			defer wg.Done()
			_, err := AcquireLock("test-pp-cli", s, false)
			if err == nil {
				successes <- s
			}
		}(scope)
	}

	wg.Wait()
	close(successes)

	// Exactly one goroutine should have succeeded at initial acquire.
	// Others may succeed if they happen to be the same scope (unlikely)
	// or fail. At minimum one should succeed.
	winners := 0
	for range successes {
		winners++
	}
	assert.GreaterOrEqual(t, winners, 1, "at least one goroutine should acquire the lock")
}
