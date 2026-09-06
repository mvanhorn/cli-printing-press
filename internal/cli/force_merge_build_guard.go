package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// Whole-tree copy is the recovery unit: merge overwrites files, injects
// AddCommand calls, prunes decls, and rewrites go.mod in place.
func backupFreshTree(freshDir string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "printing-press-fresh-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	backup := filepath.Join(tmp, "tree")
	if err := pipeline.CopyDir(freshDir, backup); err != nil {
		cleanup()
		return "", nil, err
	}
	return backup, cleanup, nil
}

// A preserve that the fresh tree does not need must not ship a build the
// fresh emission did not have. When the merged tree fails to compile and
// the pre-merge fresh tree compiles, restore fresh and rematch NovelOnly:
// novels stay, overlapping templated preserves are dropped as a group.
// --validate=false skips this gate so generation does not tidy or build.
// If rematch still fails, keep the snapshot rather than silently dropping
// novels. If fresh also fails to compile, leave the merge as-is so an
// environment failure is not treated as a preserve break.
func repairPreserveBuildBreak(snapshotDir, freshDir, freshBackup string, currentSpecBytes []byte, validate, yes bool) error {
	if !validate || !generatedTreeHasGoMod(freshDir) {
		return nil
	}
	if compileGeneratedTree(freshDir) == nil {
		return nil
	}
	if compileGeneratedTree(freshBackup) != nil {
		return nil
	}
	if err := replaceTree(freshDir, freshBackup); err != nil {
		return fmt.Errorf("restoring fresh tree after preserve build break: %w", err)
	}
	gomodMerged, err := mergeForceSnapshot(snapshotDir, freshDir, currentSpecBytes, true, yes)
	if err != nil {
		return err
	}
	if gomodMerged {
		retidyAfterMerge(freshDir)
	}
	if err := compileGeneratedTree(freshDir); err != nil {
		return fmt.Errorf("preserved files reintroduced a fresh-generation build break: %w", err)
	}
	return nil
}

func generatedTreeHasGoMod(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

func replaceTree(dst, src string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("removing merged tree %s: %w", dst, err)
	}
	if err := pipeline.CopyDir(src, dst); err != nil {
		return fmt.Errorf("restoring %s from %s: %w", dst, src, err)
	}
	return nil
}

func compileGeneratedTree(dir string) error {
	if !generatedTreeHasGoMod(dir) {
		return nil
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build ./...: %w\n%s", err, out)
	}
	return nil
}
