package regenmerge

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generatedmarker"
)

// MarkerlessFilesWouldDrop is the --force confirmation set: snapshot files
// the merge will not preserve and whose deletion would discard operator-owned
// Go. Generator-marked files, unimplemented novel scaffolds, and spec-derived
// literal drift stay off the list so fusion-guard refresh is not treated as a
// hand-authored delete.
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
		if !wouldLoseHandAuthoredSnapshot(freshDir, fc) {
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

func wouldLoseHandAuthoredSnapshot(freshDir string, fc FileClassification) bool {
	if snapshotHasUniqueDecls(fc) {
		return true
	}
	// Same declaration names can still carry a hand-authored replacement
	// that NovelOnly overwrites. Ask only when fresh is generator-owned so
	// spec-derived value drift in markerless generated files stays quiet.
	return generatedmarker.HasInFile(filepath.Join(freshDir, fc.Path))
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
