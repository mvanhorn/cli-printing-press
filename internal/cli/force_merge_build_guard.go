package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// backupFreshTree copies freshDir so a later preserve rematch can restore the
// pre-merge emission. Merge mutates files in place (overwrites, AddCommand
// injection, decl pruning, go.mod), so a whole-tree copy is the recovery unit.
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

// repairPreserveBuildBreak compiles the merged tree and, when that fails while
// the pre-merge fresh tree compiles, restores fresh and rematches with
// NovelOnly. Novel files stay; templated preserves that the fresh tree does
// not need and that made the merge fail to build are dropped. If the rematch
// still fails to compile, the error is returned and the snapshot stays in
// place — novels that themselves break the new templates are not silently
// dropped. If fresh also fails to compile, the merged tree is left as-is so
// an environment/toolchain failure is not mistaken for a preserve break.
func repairPreserveBuildBreak(snapshotDir, freshDir, freshBackup string, currentSpecBytes []byte) error {
	if !generatedTreeHasGoMod(freshDir) {
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
	gomodMerged, err := mergeForceSnapshot(snapshotDir, freshDir, currentSpecBytes, true)
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
