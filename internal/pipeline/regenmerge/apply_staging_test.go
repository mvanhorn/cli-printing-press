package regenmerge

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stageSentinelName = "attacker-owned-sentinel.txt"
	stageSentinelBody = "a sentinel file"
	publishedMarker   = "published-marker"
)

func TestApplyIgnoresPrecreatedScratchDirs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		pub, fresh := stagingFixture(t)
		planted := plantPredictableScratch(t, filepath.Dir(pub), filepath.Base(pub))
		require.NoError(t, os.Chmod(pub, 0o750))

		var stagePath, bakPath string
		restore := setScratchHook(t, func(stage, bak string) {
			if filepath.Dir(stage) != filepath.Dir(pub) {
				return
			}
			stagePath, bakPath = stage, bak
			if runtime.GOOS != "windows" {
				info, err := os.Lstat(stage)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "staging dir is owner-only until publish")
			}
		})
		defer restore()

		report := mustClassify(t, pub, fresh)
		require.NoError(t, Apply(report, Options{Force: true}))
		require.True(t, report.Applied)

		assert.NotEmpty(t, stagePath)
		assert.NotEmpty(t, bakPath)
		assert.NotEqual(t, stagePath, bakPath)
		_, err := os.Lstat(stagePath)
		assert.ErrorIs(t, err, os.ErrNotExist, "staging dir is renamed into place, not left beside the CLI")
		_, err = os.Lstat(bakPath)
		assert.ErrorIs(t, err, os.ErrNotExist, "bak dir is removed after a successful swap")

		assertScratchUntouched(t, planted)
		assert.NotContains(t, treeText(t, pub), stageSentinelBody)
		assert.Contains(t, readFileString(t, filepath.Join(pub, "keep.txt")), publishedMarker)
		assert.Contains(t, readFileString(t, filepath.Join(pub, "internal", "cli", "root.go")), "newFreshCmd")
		assertDirPerm(t, pub, 0o750)
		assertDirPerm(t, filepath.Join(pub, "internal"), 0o755)
	})

	t.Run("failure", func(t *testing.T) {
		pub, fresh := writablePair(t)
		planted := plantPredictableScratch(t, filepath.Dir(pub), filepath.Base(pub))
		before := treeText(t, pub)

		report := &MergeReport{
			CLIDir:   pub,
			FreshDir: fresh,
			Files: []FileClassification{{
				Path:    "internal/cli/missing.go",
				Verdict: VerdictTemplatedClean,
			}},
		}
		err := Apply(report, Options{Force: true})
		require.Error(t, err)
		assert.False(t, report.Applied)
		assert.Equal(t, before, treeText(t, pub))
		assertScratchUntouched(t, planted)
		assertNoLeakedScratch(t, filepath.Dir(pub), planted)
	})
}

func TestApplyConcurrentScratchDirsDoNotCollide(t *testing.T) {
	pub, fresh := stagingFixture(t)
	parent := filepath.Dir(pub)
	planted := plantPredictableScratch(t, parent, filepath.Base(pub))

	const n = 8
	var mu sync.Mutex
	stages := make([]string, 0, n)
	baks := make([]string, 0, n)
	ready := make(chan struct{})
	release := make(chan struct{})
	var readyOnce sync.Once
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	restore := setScratchHook(t, func(stage, bak string) {
		if filepath.Dir(stage) != parent {
			return
		}
		mu.Lock()
		stages = append(stages, stage)
		baks = append(baks, bak)
		full := len(stages) == n
		mu.Unlock()
		if full {
			readyOnce.Do(func() { close(ready) })
		}
		<-release
	})
	defer restore()

	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			report, err := Classify(pub, fresh, Options{Force: true})
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = Apply(report, Options{Force: true})
		}(i)
	}
	close(start)

	select {
	case <-ready:
	case <-time.After(20 * time.Second):
		unblock()
		wg.Wait()
		t.Fatalf("timed out waiting for %d staging dirs; got %d", n, len(stages))
	}
	assertUniquePaths(t, stages)
	assertUniquePaths(t, baks)
	for _, path := range append(append([]string{}, stages...), baks...) {
		assert.NotContains(t, planted.paths, path)
		assert.True(t, strings.HasPrefix(filepath.Base(path), filepath.Base(pub)+".regen-merge-"), path)
	}
	unblock()
	wg.Wait()

	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	assert.GreaterOrEqual(t, ok, 1, "one publish should finish; errors: %v", errs)
	assert.Contains(t, readFileString(t, filepath.Join(pub, "keep.txt")), publishedMarker)
	assert.NotContains(t, treeText(t, pub), stageSentinelBody)
	assertScratchUntouched(t, planted)
}

func TestApplyIgnoresPrecreatedScratchSymlinks(t *testing.T) {
	if !symlinksWork(t) {
		t.Skip("symlinks unavailable")
	}
	for _, outcome := range []string{"success", "failure"} {
		t.Run(outcome, func(t *testing.T) {
			pub, fresh := stagingFixture(t)
			parent := filepath.Dir(pub)
			target := filepath.Join(parent, "symlink-target")
			require.NoError(t, os.MkdirAll(target, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(target, stageSentinelName), []byte(stageSentinelBody), 0o644))
			links := plantPredictableSymlinks(t, parent, filepath.Base(pub), target)

			var err error
			if outcome == "success" {
				err = Apply(mustClassify(t, pub, fresh), Options{Force: true})
				require.NoError(t, err)
			} else {
				err = Apply(&MergeReport{
					CLIDir:   pub,
					FreshDir: fresh,
					Files: []FileClassification{{
						Path:    "internal/cli/missing.go",
						Verdict: VerdictTemplatedClean,
					}},
				}, Options{Force: true})
				require.Error(t, err)
			}

			for _, link := range links {
				info, lerr := os.Lstat(link)
				require.NoError(t, lerr, link)
				assert.NotEqual(t, 0, info.Mode()&os.ModeSymlink, "%s should still be the planted symlink", link)
			}
			assert.Equal(t, stageSentinelBody, readFileString(t, filepath.Join(target, stageSentinelName)))
			assert.NotContains(t, treeText(t, pub), stageSentinelBody)
		})
	}
}

func TestApplyRefusesSwappedStagingDir(t *testing.T) {
	pub, fresh := writablePair(t)
	parent := filepath.Dir(pub)
	before := treeText(t, pub)
	var swapped string

	restore := setScratchHook(t, func(stage, _ string) {
		if filepath.Dir(stage) != parent {
			return
		}
		held := stage + ".held"
		require.NoError(t, os.Rename(stage, held))
		require.NoError(t, os.Mkdir(stage, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(stage, "foreign.txt"), []byte("foreign"), 0o644))
		swapped = stage
		t.Cleanup(func() { _ = os.RemoveAll(held) })
	})
	defer restore()

	err := Apply(mustClassify(t, pub, fresh), Options{Force: true})
	require.Error(t, err)
	assert.Equal(t, before, treeText(t, pub))
	assert.Equal(t, "foreign", readFileString(t, filepath.Join(swapped, "foreign.txt")))
}

func TestApplyRefusesSwappedBakDir(t *testing.T) {
	pub, fresh := writablePair(t)
	parent := filepath.Dir(pub)
	before := treeText(t, pub)
	var swapped string

	restore := setScratchHook(t, func(stage, bak string) {
		if filepath.Dir(stage) != parent {
			return
		}
		held := bak + ".held"
		require.NoError(t, os.Rename(bak, held))
		require.NoError(t, os.Mkdir(bak, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(bak, "foreign.txt"), []byte("foreign-bak"), 0o644))
		swapped = bak
		t.Cleanup(func() { _ = os.RemoveAll(held) })
	})
	defer restore()

	err := Apply(mustClassify(t, pub, fresh), Options{Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed before publish")
	assert.Equal(t, before, treeText(t, pub))
	assert.Equal(t, "foreign-bak", readFileString(t, filepath.Join(swapped, "foreign.txt")))
}

func TestApplyDoesNotRemoveSwappedStageSymlink(t *testing.T) {
	if !symlinksWork(t) {
		t.Skip("symlinks unavailable")
	}
	pub, fresh := writablePair(t)
	parent := filepath.Dir(pub)
	precious := filepath.Join(parent, "precious")
	require.NoError(t, os.MkdirAll(precious, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(precious, "keep"), []byte("precious"), 0o644))
	before := treeText(t, pub)
	var link string

	restore := setScratchHook(t, func(stage, _ string) {
		if filepath.Dir(stage) != parent {
			return
		}
		held := stage + ".held"
		require.NoError(t, os.Rename(stage, held))
		require.NoError(t, os.Symlink(precious, stage))
		link = stage
		t.Cleanup(func() { _ = os.RemoveAll(held) })
	})
	defer restore()

	err := Apply(mustClassify(t, pub, fresh), Options{Force: true})
	require.Error(t, err)
	info, lerr := os.Lstat(link)
	require.NoError(t, lerr)
	assert.NotEqual(t, os.FileMode(0), info.Mode()&os.ModeSymlink)
	assert.Equal(t, "precious", readFileString(t, filepath.Join(precious, "keep")))
	assert.Equal(t, before, treeText(t, pub))
}

func TestApplyBakSymlinkTargetSurvives(t *testing.T) {
	if !symlinksWork(t) {
		t.Skip("symlinks unavailable")
	}
	pub, fresh := writablePair(t)
	parent := filepath.Dir(pub)
	precious := filepath.Join(parent, "precious")
	require.NoError(t, os.MkdirAll(precious, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(precious, "keep"), []byte("precious"), 0o644))
	before := treeText(t, pub)

	restore := setScratchHook(t, func(stage, bak string) {
		if filepath.Dir(stage) != parent {
			return
		}
		require.NoError(t, os.Symlink(precious, filepath.Join(bak, bakChildName)))
	})
	defer restore()

	err := Apply(mustClassify(t, pub, fresh), Options{Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Equal(t, "precious", readFileString(t, filepath.Join(precious, "keep")))
	assert.Equal(t, before, treeText(t, pub))
}

func TestApplyRefusesForeignBakChild(t *testing.T) {
	pub, fresh := writablePair(t)
	parent := filepath.Dir(pub)
	before := treeText(t, pub)

	restore := setScratchHook(t, func(stage, bak string) {
		if filepath.Dir(stage) != parent {
			return
		}
		dest := filepath.Join(bak, bakChildName)
		require.NoError(t, os.MkdirAll(dest, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dest, "foreign.txt"), []byte("foreign"), 0o644))
	})
	defer restore()

	err := Apply(mustClassify(t, pub, fresh), Options{Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Equal(t, before, treeText(t, pub))
	assert.NotContains(t, treeText(t, pub), "foreign")
}

func TestApplyRejectsNonDirectory(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	err := Apply(&MergeReport{CLIDir: file, FreshDir: t.TempDir()}, Options{Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	entries, rerr := os.ReadDir(parent)
	require.NoError(t, rerr)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), "regen-merge")
	}
}

func TestMkdirOwnedParentMustBeDirectory(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := mkdirOwned(file, "cli.regen-merge-")
	require.Error(t, err)
}

func TestMkdirOwnedDoesNotAdoptExistingDir(t *testing.T) {
	parent := t.TempDir()
	existing := filepath.Join(parent, "cli.regen-merge-1")
	require.NoError(t, os.MkdirAll(existing, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing, stageSentinelName), []byte(stageSentinelBody), 0o644))

	created, err := mkdirOwned(parent, "cli.regen-merge-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = created.remove() })
	assert.NotEqual(t, existing, created.path)
	assert.True(t, created.matches())
	assert.Equal(t, stageSentinelBody, readFileString(t, filepath.Join(existing, stageSentinelName)))
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o700), created.info.Mode().Perm())
	}
}

func TestRemoveRefusesSwappedDirectory(t *testing.T) {
	parent := t.TempDir()
	created, err := mkdirOwned(parent, "cli.regen-merge-")
	require.NoError(t, err)
	held := created.path + ".held"
	require.NoError(t, os.Rename(created.path, held))
	require.NoError(t, os.Mkdir(created.path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(created.path, "foreign.txt"), []byte("foreign"), 0o644))

	err = created.remove()
	require.ErrorIs(t, err, errScratchNotOwned)
	assert.Equal(t, "foreign", readFileString(t, filepath.Join(created.path, "foreign.txt")))
	_ = os.RemoveAll(held)
}

func TestOpenDirNoFollowRejectsSymlink(t *testing.T) {
	if !symlinksWork(t) {
		t.Skip("symlinks unavailable")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.Chmod(target, 0o755))
	link := filepath.Join(parent, "link")
	require.NoError(t, os.Symlink(target, link))

	_, err := openDirNoFollow(link)
	require.Error(t, err)
	assertDirPerm(t, target, 0o755)
}

func TestScratchPatternSanitizes(t *testing.T) {
	assert.Equal(t, "published.regen-merge-", scratchPattern("published", "regen-merge-"))
	assert.Equal(t, "a-b.regen-merge-bak-", scratchPattern("a/b", "regen-merge-bak-"))
	assert.Equal(t, "a-b.regen-merge-", scratchPattern("a\\b", "regen-merge-"))
	assert.Equal(t, "star-name.regen-merge-", scratchPattern("star*name", "regen-merge-"))
	assert.Equal(t, "cli.regen-merge-", scratchPattern("..", "regen-merge-"))
	assert.NotContains(t, scratchPattern("a/b*", "regen-merge-"), "/")
	assert.NotContains(t, scratchPattern("a/b*", "regen-merge-"), "*")
}

func TestChmodOwnedRestoresMode(t *testing.T) {
	parent := t.TempDir()
	created, err := mkdirOwned(parent, "cli.regen-merge-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = created.remove() })
	require.NoError(t, chmodOwned(created, 0o750))
	assertDirPerm(t, created.path, 0o750)
	assert.True(t, created.matches())
	if runtime.GOOS == "windows" {
		return
	}
	require.NoError(t, chmodOwned(created, os.ModeSetgid|0o750))
	info, err := os.Lstat(created.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750)|os.ModeSetgid, info.Mode()&(os.ModePerm|os.ModeSetgid))
}

type plantedScratch struct {
	stages map[string]string
	baks   []string
	paths  map[string]struct{}
}

func plantPredictableScratch(t *testing.T, parent, base string) plantedScratch {
	t.Helper()
	now := time.Now().Unix()
	planted := plantedScratch{
		stages: map[string]string{},
		paths:  map[string]struct{}{},
	}
	for ts := now - 1; ts <= now+10; ts++ {
		stage := filepath.Join(parent, fmt.Sprintf("%s.regen-merge-%d", base, ts))
		require.NoError(t, os.MkdirAll(stage, 0o755))
		sentinel := filepath.Join(stage, stageSentinelName)
		require.NoError(t, os.WriteFile(sentinel, []byte(stageSentinelBody), 0o644))
		planted.stages[stage] = stageSentinelBody

		altBase := flipCase(base)
		if altBase != base {
			alt := filepath.Join(parent, fmt.Sprintf("%s.regen-merge-%d", altBase, ts))
			if err := os.Mkdir(alt, 0o755); err == nil {
				require.NoError(t, os.WriteFile(filepath.Join(alt, stageSentinelName), []byte(stageSentinelBody), 0o644))
				planted.stages[alt] = stageSentinelBody
			}
		}

		bak := filepath.Join(parent, fmt.Sprintf("%s.regen-merge-bak-%d", base, ts))
		require.NoError(t, os.MkdirAll(bak, 0o755))
		planted.baks = append(planted.baks, bak)
	}
	for path := range planted.stages {
		planted.paths[path] = struct{}{}
	}
	for _, path := range planted.baks {
		planted.paths[path] = struct{}{}
	}
	return planted
}

func plantPredictableSymlinks(t *testing.T, parent, base, target string) []string {
	t.Helper()
	now := time.Now().Unix()
	var links []string
	for ts := now - 1; ts <= now+10; ts++ {
		for _, name := range []string{
			fmt.Sprintf("%s.regen-merge-%d", base, ts),
			fmt.Sprintf("%s.regen-merge-bak-%d", base, ts),
		} {
			link := filepath.Join(parent, name)
			require.NoError(t, os.Symlink(target, link))
			links = append(links, link)
		}
	}
	return links
}

func assertScratchUntouched(t *testing.T, planted plantedScratch) {
	t.Helper()
	for path, body := range planted.stages {
		assert.Equal(t, body, readFileString(t, filepath.Join(path, stageSentinelName)), path)
	}
	for _, path := range planted.baks {
		info, err := os.Lstat(path)
		require.NoError(t, err, path)
		assert.True(t, info.IsDir(), path)
		entries, err := os.ReadDir(path)
		require.NoError(t, err)
		assert.Empty(t, entries, "pre-created bak %s was written or replaced", path)
	}
}

func assertNoLeakedScratch(t *testing.T, parent string, planted plantedScratch) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	for _, entry := range entries {
		path := filepath.Join(parent, entry.Name())
		if _, ok := planted.paths[path]; ok {
			continue
		}
		assert.NotContains(t, entry.Name(), ".regen-merge-", "leftover scratch dir %s", entry.Name())
	}
}

func assertUniquePaths(t *testing.T, paths []string) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, path := range paths {
		_, dup := seen[path]
		assert.False(t, dup, "shared scratch path %s", path)
		seen[path] = struct{}{}
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		assert.Equal(t, os.FileMode(0), info.Mode()&os.ModeSymlink)
	}
	assert.Len(t, seen, len(paths))
	assert.GreaterOrEqual(t, len(paths), 2)
}

func assertDirPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		return
	}
	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm())
}

func setScratchHook(t *testing.T, fn func(stage, bak string)) func() {
	t.Helper()
	prev := applyScratchHook.Load()
	applyScratchHook.Store(&fn)
	return func() { applyScratchHook.Store(prev) }
}

func stagingFixture(t *testing.T) (string, string) {
	t.Helper()
	header := "// Generated by CLI Printing Press. DO NOT EDIT.\npackage cli\n\n"
	return writableTree(t, map[string]string{
		"keep.txt":             publishedMarker + "\n",
		"go.mod":               "module example.com/stage\n\ngo 1.22\n",
		"internal/cli/root.go": header + "func Execute() {}\n",
	}, map[string]string{
		"go.mod":               "module example.com/stage\n\ngo 1.22\n",
		"internal/cli/root.go": header + "func Execute() {}\nfunc newFreshCmd() {}\n",
	})
}

func writablePair(t *testing.T) (string, string) {
	t.Helper()
	return writableTree(t, map[string]string{
		"keep.txt": publishedMarker + "\n",
	}, map[string]string{
		"keep.txt": "fresh\n",
	})
}

func writableTree(t *testing.T, pubFiles, freshFiles map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	pub := filepath.Join(root, "published")
	fresh := filepath.Join(root, "fresh")
	writeTree(t, pub, pubFiles)
	writeTree(t, fresh, freshFiles)
	return pub, fresh
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

func mustClassify(t *testing.T, pub, fresh string) *MergeReport {
	t.Helper()
	report, err := Classify(pub, fresh, Options{Force: true})
	require.NoError(t, err)
	return report
}

func treeText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(&b, "%s\n%s\n", filepath.ToSlash(rel), data)
		return nil
	})
	require.NoError(t, err)
	return b.String()
}

func symlinksWork(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		return false
	}
	_ = os.Remove(link)
	return true
}

func flipCase(s string) string {
	if strings.ToUpper(s) != s {
		return strings.ToUpper(s)
	}
	return strings.ToLower(s)
}
