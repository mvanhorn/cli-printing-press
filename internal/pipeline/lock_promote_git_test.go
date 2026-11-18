package pipeline

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type promoteGitFixture struct {
	tmp     string
	workDir string
	libDir  string
}

func newPromoteGitFixture(t *testing.T, withLibrary bool) promoteGitFixture {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", tmp)

	workDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test-pp-cli\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))

	libDir := filepath.Join(PublishedLibraryRoot(), "test")
	if withLibrary {
		require.NoError(t, os.MkdirAll(libDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(libDir, "old-file.txt"), []byte("old\n"), 0o644))
	}
	return promoteGitFixture{tmp: tmp, workDir: workDir, libDir: libDir}
}

func (f promoteGitFixture) promote(t *testing.T) error {
	t.Helper()
	_, err := PromoteWorkingCLIWithResult("test-pp-cli", f.workDir, NewMinimalState("test-pp-cli", f.workDir))
	return err
}

func commitGitFile(t *testing.T, dir, name, body, message string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	runGit(t, dir, "add", "--", name)
	runGit(t, dir, "commit", "-qm", message)
	return runGit(t, dir, "rev-parse", "HEAD")
}

func assertGitHistory(t *testing.T, dir, head, count string) {
	t.Helper()
	assert.Equal(t, head, runGit(t, dir, "rev-parse", "HEAD"))
	assert.Equal(t, count, runGit(t, dir, "rev-list", "--count", "HEAD"))
}

func assertNoPromoteDebris(t *testing.T, libDir string) {
	t.Helper()
	assert.NoDirExists(t, libDir+".old")
	assert.NoDirExists(t, libDir+".promoting")
}

func plantStagingGitHook(t *testing.T, workDir, libDir string) func() {
	t.Helper()
	marker := filepath.Join(libDir+".promoting", ".git")
	prefix := workDir + string(os.PathSeparator)
	return SetCopyDirWalkHookForTest(func(path string, d fs.DirEntry) {
		if d == nil || (path != workDir && !strings.HasPrefix(path, prefix)) {
			return
		}
		if _, err := os.Lstat(marker); err == nil {
			return
		}
		_ = os.WriteFile(marker, []byte("staged\n"), 0o644)
	})
}

func TestPromoteWorkingCLI_PathAFirstPrintCarriesWorkdirGit(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	initGitRepository(t, f.workDir)
	head := commitGitFile(t, f.workDir, "notes.txt", "notes\n", "second")
	require.NoError(t, os.MkdirAll(filepath.Join(f.workDir, "vendor", "sub", ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, "vendor", "sub", ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, "vendor", "sub", "keep.go"), []byte("package sub\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(f.workDir, "vendor", "plain"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, "vendor", "plain", ".git"), []byte("gitdir: ../elsewhere\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, "vendor", "plain", "keep.go"), []byte("package plain\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, ".gitmodules"), []byte("[submodule \"sub\"]\n"), 0o644))
	caseVariant := filepath.Join(f.workDir, ".Git")
	distinctCaseVariant := os.Mkdir(caseVariant, 0o755) == nil
	if distinctCaseVariant {
		require.NoError(t, os.WriteFile(filepath.Join(caseVariant, "marker"), []byte("not-history\n"), 0o644))
	}

	decoy := filepath.Join(f.tmp, "decoy")
	require.NoError(t, os.MkdirAll(decoy, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(decoy, "go.mod"), []byte("module decoy\n\ngo 1.21\n"), 0o644))
	decoyHead := initGitRepository(t, decoy)
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))

	require.NoError(t, f.promote(t))

	info, err := os.Lstat(filepath.Join(f.libDir, ".git"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Zero(t, info.Mode()&os.ModeSymlink)
	assertGitHistory(t, f.libDir, head, "2")
	assertGitHistory(t, f.workDir, head, "2")
	assert.Equal(t, decoyHead, runGit(t, decoy, "rev-parse", "HEAD"))
	assert.NoDirExists(t, filepath.Join(f.libDir, "vendor", "sub", ".git"))
	assert.FileExists(t, filepath.Join(f.libDir, "vendor", "sub", "keep.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "vendor", "plain", ".git"))
	assert.FileExists(t, filepath.Join(f.libDir, "vendor", "plain", "keep.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, ".gitmodules"))
	if distinctCaseVariant {
		data, err := os.ReadFile(filepath.Join(f.libDir, ".Git", "marker"))
		require.NoError(t, err)
		assert.Equal(t, "not-history\n", string(data))
	}
	assert.FileExists(t, filepath.Join(f.libDir, "notes.txt"))
	assertNoPromoteDebris(t, f.libDir)

	require.NoError(t, os.RemoveAll(f.workDir))
	assertGitHistory(t, f.libDir, head, "2")
}

func TestPromoteWorkingCLI_PathAFirstPrintRewritesInTreeGitSymlinks(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	initGitRepository(t, f.workDir)
	head := commitGitFile(t, f.workDir, "notes.txt", "notes\n", "second")
	marker := filepath.Join(f.workDir, ".git", "marker")
	require.NoError(t, os.WriteFile(marker, []byte("inside\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(f.workDir, ".git", "subdir"), 0o755))
	if err := os.Symlink(marker, filepath.Join(f.workDir, ".git", "subdir", "abs-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	require.NoError(t, os.Symlink("HEAD", filepath.Join(f.workDir, ".git", "head-link")))

	require.NoError(t, f.promote(t))

	absGot, err := os.Readlink(filepath.Join(f.libDir, ".git", "subdir", "abs-link"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("..", "marker"), absGot)
	assert.False(t, filepath.IsAbs(absGot))
	relGot, err := os.Readlink(filepath.Join(f.libDir, ".git", "head-link"))
	require.NoError(t, err)
	assert.Equal(t, "HEAD", relGot)

	require.NoError(t, os.RemoveAll(f.workDir))
	data, err := os.ReadFile(filepath.Join(f.libDir, ".git", "subdir", "abs-link"))
	require.NoError(t, err)
	assert.Equal(t, "inside\n", string(data))
	headData, err := os.ReadFile(filepath.Join(f.libDir, ".git", "head-link"))
	require.NoError(t, err)
	wantHead, err := os.ReadFile(filepath.Join(f.libDir, ".git", "HEAD"))
	require.NoError(t, err)
	assert.Equal(t, string(wantHead), string(headData))
	assertGitHistory(t, f.libDir, head, "2")
}

func TestPromoteWorkingCLI_PathAFirstPrintWithoutGitOmitsGit(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	require.NoError(t, f.promote(t))
	_, err := os.Lstat(filepath.Join(f.libDir, ".git"))
	assert.True(t, os.IsNotExist(err))
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathABothGitLibraryWins(t *testing.T) {
	f := newPromoteGitFixture(t, true)
	initGitRepository(t, f.libDir)
	libHead := commitGitFile(t, f.libDir, "library.txt", "library\n", "library-two")
	initGitRepository(t, f.workDir)
	workHead := commitGitFile(t, f.workDir, "work.txt", "work\n", "work-two")
	require.NotEqual(t, libHead, workHead)

	require.NoError(t, os.MkdirAll(filepath.Join(f.libDir, "nested", ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.libDir, "nested", ".git", "HEAD"), []byte("nested\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(f.workDir, "vendor", "sub", ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, "vendor", "sub", ".git", "HEAD"), []byte("workdir-nested\n"), 0o644))

	require.NoError(t, f.promote(t))

	assertGitHistory(t, f.libDir, libHead, "2")
	assertGitHistory(t, f.workDir, workHead, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	assert.NoDirExists(t, filepath.Join(f.libDir, "nested", ".git"))
	assert.NoDirExists(t, filepath.Join(f.libDir, "vendor", "sub", ".git"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARestoredBackupGitWinsOverWorkdir(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	backup := f.libDir + ".old"
	require.NoError(t, os.MkdirAll(backup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "old-file.txt"), []byte("old\n"), 0o644))
	initGitRepository(t, backup)
	backupHead := commitGitFile(t, backup, "library.txt", "library\n", "backup-two")

	initGitRepository(t, f.workDir)
	workHead := commitGitFile(t, f.workDir, "work.txt", "work\n", "work-two")
	require.NotEqual(t, backupHead, workHead)

	require.NoError(t, f.promote(t))

	assertGitHistory(t, f.libDir, backupHead, "2")
	assertGitHistory(t, f.workDir, workHead, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARetryAdoptsGitLeftInBackup(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	backup := f.libDir + ".old"
	require.NoError(t, os.MkdirAll(backup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "old-file.txt"), []byte("old\n"), 0o644))
	initGitRepository(t, backup)
	backupHead := commitGitFile(t, backup, "library.txt", "library\n", "backup-two")

	// Crash window: the swap already installed the new tree, and the history
	// rename had not run, so .git exists only in the backup.
	require.NoError(t, os.MkdirAll(f.libDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.libDir, "go.mod"), []byte("module new\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.libDir, "partial.txt"), []byte("partial\n"), 0o644))

	initGitRepository(t, f.workDir)
	workHead := commitGitFile(t, f.workDir, "work.txt", "work\n", "work-two")
	require.NotEqual(t, backupHead, workHead)

	require.NoError(t, f.promote(t))

	assertGitHistory(t, f.libDir, backupHead, "2")
	assertGitHistory(t, f.workDir, workHead, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "partial.txt"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	_, err := os.Lstat(filepath.Join(backup, ".git"))
	assert.True(t, os.IsNotExist(err))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARetryKeepsLibraryGitWhenBackupAlsoHasGit(t *testing.T) {
	f := newPromoteGitFixture(t, true)
	initGitRepository(t, f.libDir)
	libHead := commitGitFile(t, f.libDir, "library.txt", "library\n", "library-two")

	backup := f.libDir + ".old"
	require.NoError(t, os.MkdirAll(backup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "go.mod"), []byte("module backup\n\ngo 1.21\n"), 0o644))
	backupHead := initGitRepository(t, backup)
	require.NotEqual(t, libHead, backupHead)

	require.NoError(t, f.promote(t))

	assertGitHistory(t, f.libDir, libHead, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestRecoverBackupGitMetadata(t *testing.T) {
	t.Run("missing library leaves backup git", func(t *testing.T) {
		tmp := t.TempDir()
		backup := filepath.Join(tmp, "lib.old")
		lib := filepath.Join(tmp, "lib")
		require.NoError(t, os.MkdirAll(filepath.Join(backup, ".git"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(backup, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))

		require.NoError(t, recoverBackupGitMetadata(backup, lib))

		assert.FileExists(t, filepath.Join(backup, ".git", "HEAD"))
		_, err := os.Stat(lib)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("symlink moves without following", func(t *testing.T) {
		tmp := t.TempDir()
		backup := filepath.Join(tmp, "lib.old")
		lib := filepath.Join(tmp, "lib")
		real := filepath.Join(tmp, "real-git")
		require.NoError(t, os.MkdirAll(real, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
		require.NoError(t, os.MkdirAll(backup, 0o755))
		require.NoError(t, os.MkdirAll(lib, 0o755))
		if err := os.Symlink(real, filepath.Join(backup, ".git")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		require.NoError(t, recoverBackupGitMetadata(backup, lib))

		info, err := os.Lstat(filepath.Join(lib, ".git"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&os.ModeSymlink)
		link, err := os.Readlink(filepath.Join(lib, ".git"))
		require.NoError(t, err)
		assert.Equal(t, real, link)
		_, err = os.Lstat(filepath.Join(backup, ".git"))
		assert.True(t, os.IsNotExist(err))
		assert.FileExists(t, filepath.Join(real, "HEAD"))
	})
}

func TestPromoteWorkingCLI_PathAFailureBeforeSwapKeepsLibraryGit(t *testing.T) {
	f := newPromoteGitFixture(t, true)
	initGitRepository(t, f.libDir)
	head := commitGitFile(t, f.libDir, "library.txt", "library\n", "library-two")
	writePatchRecordFile(t, filepath.Join(f.workDir, PatchesDirName), "report.json", PatchRecord{
		SchemaVersion: CurrentPatchesIndexSchemaVersion,
		ID:            "report",
		Files:         []string{"internal/cli/report.go"},
	})
	_, err := AcquireLock("test-pp-cli", "test-scope", false)
	require.NoError(t, err)

	err = f.promote(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recorded patches no longer match the tree being promoted")

	assertGitHistory(t, f.libDir, head, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "main.go"))
	assertNoPromoteDebris(t, f.libDir)
	_, err = os.Stat(LockFilePath("test-pp-cli"))
	assert.NoError(t, err)
}

func TestPromoteWorkingCLI_PathAMoveFailureRollsBackLibraryGit(t *testing.T) {
	f := newPromoteGitFixture(t, true)
	initGitRepository(t, f.libDir)
	head := commitGitFile(t, f.libDir, "library.txt", "library\n", "library-two")
	workHead := initGitRepository(t, f.workDir)
	restore := plantStagingGitHook(t, f.workDir, f.libDir)
	defer restore()

	err := f.promote(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staged git metadata unexpectedly exists")

	assertGitHistory(t, f.libDir, head, "2")
	assertGitHistory(t, f.workDir, workHead, "1")
	assert.FileExists(t, filepath.Join(f.libDir, "old-file.txt"))
	assert.FileExists(t, filepath.Join(f.libDir, "library.txt"))
	assert.NoFileExists(t, filepath.Join(f.libDir, "main.go"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARefusesPlantedStagingGitBeforeSwap(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	initGitRepository(t, f.workDir)
	head := commitGitFile(t, f.workDir, "notes.txt", "notes\n", "second")
	restore := plantStagingGitHook(t, f.workDir, f.libDir)
	defer restore()

	err := f.promote(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staged git metadata unexpectedly exists")

	_, statErr := os.Stat(f.libDir)
	assert.True(t, os.IsNotExist(statErr))
	assertGitHistory(t, f.workDir, head, "2")
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARefusesSymlinkedWorkdirGit(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	real := filepath.Join(f.tmp, "real-repo")
	require.NoError(t, os.MkdirAll(real, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "go.mod"), []byte("module real\n\ngo 1.21\n"), 0o644))
	head := initGitRepository(t, real)
	if err := os.Symlink(filepath.Join(real, ".git"), filepath.Join(f.workDir, ".git")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := f.promote(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "working git metadata is a symlink")

	_, statErr := os.Stat(f.libDir)
	assert.True(t, os.IsNotExist(statErr))
	link, err := os.Readlink(filepath.Join(f.workDir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(real, ".git"), link)
	assertGitHistory(t, real, head, "1")
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathARefusesGitMetadataSymlinkEscape(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	head := initGitRepository(t, f.workDir)
	outside := filepath.Join(f.tmp, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("secret\n"), 0o644))
	if err := os.Symlink(outside, filepath.Join(f.workDir, ".git", "escaped")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := f.promote(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "points outside git metadata")

	_, statErr := os.Stat(f.libDir)
	assert.True(t, os.IsNotExist(statErr))
	assertGitHistory(t, f.workDir, head, "1")
	data, err := os.ReadFile(filepath.Join(outside, "secret"))
	require.NoError(t, err)
	assert.Equal(t, "secret\n", string(data))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathAReprintPreservesSymlinkedLibraryGit(t *testing.T) {
	f := newPromoteGitFixture(t, true)
	real := filepath.Join(f.tmp, "real-repo")
	require.NoError(t, os.MkdirAll(real, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "tracked.txt"), []byte("tracked\n"), 0o644))
	head := initGitRepository(t, real)
	require.NoError(t, os.RemoveAll(filepath.Join(f.libDir, ".git")))
	if err := os.Symlink(filepath.Join(real, ".git"), filepath.Join(f.libDir, ".git")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	other := filepath.Join(f.tmp, "other-repo")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "go.mod"), []byte("module other\n\ngo 1.21\n"), 0o644))
	otherHead := initGitRepository(t, other)
	if err := os.Symlink(filepath.Join(other, ".git"), filepath.Join(f.workDir, ".git")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	require.NoError(t, f.promote(t))

	info, err := os.Lstat(filepath.Join(f.libDir, ".git"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	link, err := os.Readlink(filepath.Join(f.libDir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(real, ".git"), link)
	assertGitHistory(t, f.libDir, head, "1")
	assertGitHistory(t, real, head, "1")
	assertGitHistory(t, other, otherHead, "1")
	assert.FileExists(t, filepath.Join(f.libDir, "main.go"))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathAFirstPrintCarriesGitFile(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	canonical := filepath.Join(f.tmp, "canonical")
	require.NoError(t, os.MkdirAll(canonical, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonical, "go.mod"), []byte("module canonical\n\ngo 1.21\n"), 0o644))
	head := initGitRepository(t, canonical)
	gitdir := "gitdir: " + filepath.Join(canonical, ".git") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.workDir, ".git"), []byte(gitdir), 0o644))

	require.NoError(t, f.promote(t))

	got, err := os.ReadFile(filepath.Join(f.libDir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, gitdir, string(got))
	info, err := os.Lstat(filepath.Join(f.libDir, ".git"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
	assert.Equal(t, head, runGit(t, f.libDir, "rev-parse", "HEAD"))
	workGit, err := os.ReadFile(filepath.Join(f.workDir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, gitdir, string(workGit))
	assertNoPromoteDebris(t, f.libDir)
}

func TestPromoteWorkingCLI_PathAStateSaveFailureKeepsCarriedGit(t *testing.T) {
	f := newPromoteGitFixture(t, false)
	initGitRepository(t, f.workDir)
	head := commitGitFile(t, f.workDir, "notes.txt", "notes\n", "second")

	_, err := AcquireLock("test-pp-cli", "test-scope", false)
	require.NoError(t, err)
	state := NewStateWithRun("test", f.workDir, "run-git-save", "test-scope")
	writePhase5PassForState(t, state, "none")
	require.NoError(t, os.MkdirAll(filepath.Dir(state.PipelineDir()), 0o755))
	require.NoError(t, os.WriteFile(state.PipelineDir(), []byte("not a directory"), 0o644))

	_, err = PromoteWorkingCLIWithResult("test-pp-cli", f.workDir, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli promoted to")
	assert.Contains(t, err.Error(), "state update failed")

	assertGitHistory(t, f.libDir, head, "2")
	assertGitHistory(t, f.workDir, head, "2")
	assert.FileExists(t, filepath.Join(f.libDir, "notes.txt"))
	assertNoPromoteDebris(t, f.libDir)
	_, err = os.Stat(LockFilePath("test-pp-cli"))
	assert.True(t, os.IsNotExist(err))
}
