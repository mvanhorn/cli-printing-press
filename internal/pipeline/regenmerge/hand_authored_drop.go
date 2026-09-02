package regenmerge

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generatedmarker"
)

// MarkerlessFilesWouldDrop lists snapshot-relative paths that MergeIntoFreshTree
// would leave as fresh emission (not copy from snapshot) and that lack the
// generator marker. Unimplemented novel TODO scaffolds are omitted because
// fusion-guard rules may refresh them.
func MarkerlessFilesWouldDrop(snapshotDir, freshDir string, report *MergeReport, opts Options) []string {
	if report == nil {
		return nil
	}
	var dropped []string
	for _, fc := range report.Files {
		if shouldPreserveFromSnapshot(fc, snapshotDir, freshDir, opts) {
			continue
		}
		if !isMarkerlessHandAuthoredSnapshotFile(snapshotDir, freshDir, fc.Path) {
			continue
		}
		snapPath := filepath.Join(snapshotDir, fc.Path)
		freshPath := filepath.Join(freshDir, fc.Path)
		sameAsFresh, err := filesEqual(snapPath, freshPath)
		if err == nil && sameAsFresh {
			continue
		}
		if opts.BaseDir != "" {
			sameAsBase, berr := filesEqual(snapPath, filepath.Join(opts.BaseDir, fc.Path))
			if berr == nil && sameAsBase {
				continue
			}
		}
		if !snapshotHasUniqueDecls(fc) {
			continue
		}
		dropped = append(dropped, fc.Path)
	}
	slices.Sort(dropped)
	return dropped
}

func isMarkerlessHandAuthoredSnapshotFile(snapshotDir, freshDir, rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".go") {
		return false
	}
	snapPath := filepath.Join(snapshotDir, rel)
	if _, err := os.Stat(snapPath); err != nil {
		return false
	}
	if generatedmarker.HasInFile(snapPath) {
		return false
	}
	return !isUnimplementedNovelCommandScaffold(snapshotDir, freshDir, rel)
}

func snapshotHasUniqueDecls(fc FileClassification) bool {
	if fc.Verdict == VerdictNovelCollision {
		return true
	}
	return fc.DeclSetDelta != nil && len(fc.DeclSetDelta.InPublishedNotFresh) > 0
}

func isUnimplementedNovelCommandScaffold(snapshotDir, freshDir, rel string) bool {
	if isHandAuthoredNovelCommandScaffold(snapshotDir, freshDir, rel) {
		return false
	}
	if !strings.HasPrefix(rel, "internal/cli/") || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false
	}
	freshData, err := os.ReadFile(filepath.Join(freshDir, rel))
	if err != nil {
		return false
	}
	if hasGeneratedMarkerBytes(freshData) || !bytes.Contains(freshData, []byte(novelCommandScaffoldMarker)) {
		return false
	}
	snapshotData, err := os.ReadFile(filepath.Join(snapshotDir, rel))
	if err != nil {
		return false
	}
	return bytes.Contains(snapshotData, []byte(novelCommandScaffoldTODO))
}
