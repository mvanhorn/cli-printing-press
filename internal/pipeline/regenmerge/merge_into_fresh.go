package regenmerge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// regenmergeGeneratorOwnedDirs lists internal/<name>/ subtrees the generator
// owns end-to-end. Files inside these directories that don't appear in the
// classifier's report (typically non-Go files like text fixtures) are NOT
// swept from snapshot into fresh during MergeIntoFreshTree — fresh's
// emission is authoritative for everything under these roots. Canonical
// source for the regen pipeline; the parallel list in internal/cli/root.go
// (alongside the dead in-memory preserve helpers) tracks the same set and
// will be removed when those helpers are deleted.
var regenmergeGeneratorOwnedDirs = map[string]struct{}{
	"cli":     {},
	"cliutil": {},
	"mcp":     {},
	"cache":   {},
	"client":  {},
	"config":  {},
	"share":   {},
	"store":   {},
	"types":   {},
}

// MergeIntoFreshTree merges hand-edits from a snapshot directory into a
// freshly-emitted CLI tree. Companion to Apply for the regen-from-spec
// workflow: where Apply runs stage-and-swap with the published tree as the
// destination, MergeIntoFreshTree mutates the freshly-generated tree
// in-place using the snapshot as the recovery path.
//
// Steps in order:
//  1. Per-file verdict switch — copy preserve-worthy files from snapshot
//     into fresh; no-op on TEMPLATED-CLEAN / NEW-TEMPLATE-EMISSION;
//     leave PUBLISHED-ONLY-TEMPLATED files alone (fresh didn't emit them).
//  2. Re-inject lost AddCommand calls into fresh-derived host files.
//  3. Merge go.mod requires/replaces from snapshot into fresh's go.mod via
//     renderMergedGoMod (preserves hand-added deps for novel packages).
//  4. Sweep snapshot for non-classified files (README.md, Makefile, etc.)
//     under non-generator-owned directories and copy any that don't exist
//     in fresh.
//
// Symlinks at any preserve path or sweep path are refused — the caller is
// expected to have validated the snapshot/fresh directory shape upstream.
//
// When opts.NovelOnly is true, only NOVEL and NOVEL-COLLISION verdicts are
// preserved; TEMPLATED-WITH-ADDITIONS, TEMPLATED-BODY-DRIFT, and
// TEMPLATED-VALUE-DRIFT files are left as fresh emitted them, and lost
// AddCommand re-injection is skipped. The non-classified file sweep and
// go.mod merge still run because both are spec-orthogonal — non-Go files
// and go.mod require additions are valid preservation targets even when
// the fresh spec differs from the snapshot's.
func MergeIntoFreshTree(snapshotDir, freshDir string, report *MergeReport, opts Options) error {
	if report == nil {
		return errors.New("nil report")
	}
	if _, err := os.Stat(snapshotDir); err != nil {
		return fmt.Errorf("snapshot dir %s: %w", snapshotDir, err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		return fmt.Errorf("fresh dir %s: %w", freshDir, err)
	}

	for i := range report.Files {
		fc := &report.Files[i]
		switch fc.Verdict {
		case VerdictTemplatedClean, VerdictNewTemplateEmission, VerdictPublishedOnlyTemplated:
			// fresh's emission is authoritative; nothing to copy from snapshot.
		case VerdictNovel, VerdictNovelCollision:
			if err := copyPreserveFile(snapshotDir, freshDir, fc.Path); err != nil {
				return err
			}
			fc.Applied = true
		case VerdictTemplatedWithAdditions, VerdictTemplatedBodyDrift, VerdictTemplatedValueDrift:
			if opts.NovelOnly {
				continue
			}
			if err := copyPreserveFile(snapshotDir, freshDir, fc.Path); err != nil {
				return err
			}
			fc.Applied = true
		default:
			return fmt.Errorf("unhandled verdict %q for %s", fc.Verdict, fc.Path)
		}
	}

	if !opts.NovelOnly {
		for i := range report.LostRegistrations {
			lr := &report.LostRegistrations[i]
			if len(lr.Calls) == 0 {
				continue
			}
			hostPath := filepath.Join(freshDir, lr.HostFile)
			if err := injectAddCommands(hostPath, lr.Calls); err != nil {
				return fmt.Errorf("re-injecting AddCommand into %s: %w", lr.HostFile, err)
			}
			lr.Applied = true
		}
	}

	if report.GoMod != nil {
		mergedBytes, err := renderMergedGoMod(snapshotDir, freshDir)
		switch {
		case err == nil:
			if writeErr := writeFileAtomic(filepath.Join(freshDir, "go.mod"), mergedBytes); writeErr != nil {
				return fmt.Errorf("writing merged go.mod: %w", writeErr)
			}
			report.GoMod.Merged = true
		case errors.Is(err, fs.ErrNotExist):
			// Either tree lacks a go.mod; leave fresh's emission alone.
		default:
			return fmt.Errorf("rendering merged go.mod: %w", err)
		}
	}

	if err := sweepNonClassifiedFiles(snapshotDir, freshDir); err != nil {
		return fmt.Errorf("sweeping non-classified snapshot files: %w", err)
	}

	report.Applied = true
	return nil
}

// copyPreserveFile copies snapshot/rel → fresh/rel, refusing symlinks and
// creating parent dirs as needed.
func copyPreserveFile(snapshotDir, freshDir, rel string) error {
	src := filepath.Join(snapshotDir, rel)
	dst := filepath.Join(freshDir, rel)

	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("statting snapshot file %s: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to preserve symlinked snapshot file: %s", rel)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading snapshot file %s: %w", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating parent for %s: %w", rel, err)
	}
	if err := writeFileAtomic(dst, data); err != nil {
		return fmt.Errorf("writing preserved %s: %w", rel, err)
	}
	return nil
}

// sweepNonClassifiedFiles walks the snapshot for files that the classifier
// did not see (non-Go, non-module files like README.md, Makefile,
// .printing-press.json) and copies any that don't exist in fresh AND don't
// live under a generator-owned directory. Symlinks are refused.
func sweepNonClassifiedFiles(snapshotDir, freshDir string) error {
	return filepath.WalkDir(snapshotDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(snapshotDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if !shouldWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			if isGeneratorOwnedInternalDir(relSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldWalkDir(filepath.Base(filepath.Dir(path))) {
			return nil
		}
		if shouldClassifyFile(relSlash) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to sweep symlinked snapshot file: %s", relSlash)
		}
		dst := filepath.Join(freshDir, rel)
		if _, err := os.Stat(dst); err == nil {
			// fresh already emitted at this path; fresh wins.
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("statting fresh path %s: %w", relSlash, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading snapshot file %s: %w", relSlash, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("creating parent for swept %s: %w", relSlash, err)
		}
		if err := writeFileAtomic(dst, data); err != nil {
			return fmt.Errorf("writing swept %s: %w", relSlash, err)
		}
		return nil
	})
}

// isGeneratorOwnedInternalDir reports whether relSlash names a directory
// under internal/ that the generator owns end-to-end. Used by the sweep to
// avoid copying random non-Go content into a directory the generator
// regenerates from scratch each run.
func isGeneratorOwnedInternalDir(relSlash string) bool {
	const prefix = "internal/"
	rest, ok := strings.CutPrefix(relSlash, prefix)
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(rest, "/")
	if first == "" {
		return false
	}
	_, owned := regenmergeGeneratorOwnedDirs[first]
	return owned
}
