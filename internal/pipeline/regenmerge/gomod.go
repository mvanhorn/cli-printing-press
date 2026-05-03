package regenmerge

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// planGoModMerge reads both go.mod files and builds the merge plan: published
// `module` line + fresh `go`/`require` + smart `replace` union (local-path
// replaces from published win over fresh; version-replaces from fresh win
// over published when both have a target for the same path).
//
// Returns nil GoModMerge if neither tree has a go.mod (trivially nothing to
// merge).
func planGoModMerge(publishedDir, freshDir string) (*GoModMerge, error) {
	pubPath := filepath.Join(publishedDir, "go.mod")
	freshPath := filepath.Join(freshDir, "go.mod")
	pubData, pubErr := os.ReadFile(pubPath)
	freshData, freshErr := os.ReadFile(freshPath)
	if pubErr != nil && freshErr != nil {
		return nil, nil
	}
	if pubErr != nil {
		return nil, fmt.Errorf("reading published go.mod: %w", pubErr)
	}
	if freshErr != nil {
		return nil, fmt.Errorf("reading fresh go.mod: %w", freshErr)
	}

	pubMF, err := modfile.Parse(pubPath, pubData, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing published go.mod: %w", err)
	}
	freshMF, err := modfile.Parse(freshPath, freshData, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing fresh go.mod: %w", err)
	}
	if pubMF.Module == nil {
		return nil, fmt.Errorf("published go.mod has no module declaration")
	}

	plan := &GoModMerge{
		PreservedModulePath: pubMF.Module.Mod.Path,
	}

	// Compute added/removed requires for the report.
	pubReqs := map[string]string{}
	for _, r := range pubMF.Require {
		pubReqs[r.Mod.Path] = r.Mod.Version
	}
	freshReqs := map[string]string{}
	for _, r := range freshMF.Require {
		freshReqs[r.Mod.Path] = r.Mod.Version
	}
	for path, ver := range freshReqs {
		if _, ok := pubReqs[path]; !ok {
			plan.AddedRequires = append(plan.AddedRequires, fmt.Sprintf("%s %s", path, ver))
		}
	}
	// Published-only requires are preserved (see GoModMerge.PreservedRequires).
	for path, ver := range pubReqs {
		if _, ok := freshReqs[path]; !ok {
			plan.PreservedRequires = append(plan.PreservedRequires, fmt.Sprintf("%s %s", path, ver))
		}
	}

	// Smart replace handling: local-path replaces in published win.
	for _, r := range pubMF.Replace {
		if isLocalPathReplace(r.New.Path) {
			plan.PreservedReplaces = append(plan.PreservedReplaces,
				fmt.Sprintf("%s => %s", r.Old.Path, r.New.Path))
		}
	}

	return plan, nil
}

// renderMergedGoMod produces the actual merged go.mod bytes from the two
// inputs. Used by U4's Apply step. Caller writes the bytes.
func renderMergedGoMod(publishedDir, freshDir string) ([]byte, error) {
	pubPath := filepath.Join(publishedDir, "go.mod")
	freshPath := filepath.Join(freshDir, "go.mod")
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, err
	}
	freshData, err := os.ReadFile(freshPath)
	if err != nil {
		return nil, err
	}
	pubMF, err := modfile.Parse(pubPath, pubData, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing published go.mod: %w", err)
	}
	freshMF, err := modfile.Parse(freshPath, freshData, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing fresh go.mod: %w", err)
	}
	if pubMF.Module == nil {
		return nil, fmt.Errorf("published go.mod has no module declaration")
	}

	// Start with fresh's require/replace/exclude as a base, then graft
	// published's module path and adjust replaces.
	merged := &modfile.File{}
	if err := merged.AddModuleStmt(pubMF.Module.Mod.Path); err != nil {
		return nil, fmt.Errorf("setting module path: %w", err)
	}
	if freshMF.Go != nil {
		if err := merged.AddGoStmt(freshMF.Go.Version); err != nil {
			return nil, fmt.Errorf("setting go version: %w", err)
		}
	}
	// Merged require set: union of published and fresh.
	//
	// Fresh wins on shared paths (newer generator templates pin newer
	// versions, and dropping fresh's pin would silently downgrade indirect
	// deps). Published-only requires are preserved so deps the agent added
	// after the original generation (e.g., `go get modernc.org/sqlite` for
	// a hand-built local store) survive a regen-merge. Without this, the
	// merged go.mod would drop the dep and `go build` would fail with
	// "no required module provides package <X>" on the next build.
	freshReqPaths := map[string]bool{}
	for _, r := range freshMF.Require {
		freshReqPaths[r.Mod.Path] = true
		if err := merged.AddRequire(r.Mod.Path, r.Mod.Version); err != nil {
			return nil, fmt.Errorf("adding fresh require %s: %w", r.Mod.Path, err)
		}
	}
	for _, r := range pubMF.Require {
		if freshReqPaths[r.Mod.Path] {
			continue
		}
		if err := merged.AddRequire(r.Mod.Path, r.Mod.Version); err != nil {
			return nil, fmt.Errorf("adding published-only require %s: %w", r.Mod.Path, err)
		}
	}

	// Replace handling: build a set of paths fresh handles, then add
	// fresh's replaces; for paths fresh DOESN'T cover, take published's
	// replace if it's a local-path; for paths fresh DOES cover but
	// published has a local-path replace for it, prefer published.
	freshReplacePaths := map[string]bool{}
	for _, r := range freshMF.Replace {
		freshReplacePaths[r.Old.Path] = true
	}
	for _, r := range pubMF.Replace {
		if isLocalPathReplace(r.New.Path) {
			// Local-path replace in published always wins. Add unconditionally;
			// if fresh had the same path, the published one overrides
			// because we add it after.
			if err := merged.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version); err != nil {
				return nil, fmt.Errorf("adding published replace %s: %w", r.Old.Path, err)
			}
			continue
		}
		// Non-local-path replace in published — only carry forward if
		// fresh doesn't have a replace for the same path.
		if !freshReplacePaths[r.Old.Path] {
			if err := merged.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version); err != nil {
				return nil, fmt.Errorf("adding published replace %s: %w", r.Old.Path, err)
			}
		}
	}
	// Add fresh's replaces only if published didn't already place a
	// local-path replace for the same path.
	pubLocalPaths := map[string]bool{}
	for _, r := range pubMF.Replace {
		if isLocalPathReplace(r.New.Path) {
			pubLocalPaths[r.Old.Path] = true
		}
	}
	for _, r := range freshMF.Replace {
		if pubLocalPaths[r.Old.Path] {
			continue
		}
		if err := merged.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version); err != nil {
			return nil, fmt.Errorf("adding fresh replace %s: %w", r.Old.Path, err)
		}
	}

	// Exclude: union.
	seenExcl := map[string]bool{}
	for _, e := range append(pubMF.Exclude, freshMF.Exclude...) {
		key := e.Mod.Path + "@" + e.Mod.Version
		if seenExcl[key] {
			continue
		}
		seenExcl[key] = true
		if err := merged.AddExclude(e.Mod.Path, e.Mod.Version); err != nil {
			return nil, fmt.Errorf("adding exclude %s: %w", e.Mod.Path, err)
		}
	}

	// Retract: published's only. Retracts require go 1.16+; if the merged
	// go directive is missing or older, skip retracts rather than letting
	// AddRetract fail (which would corrupt the merge silently if the caller
	// swallowed the render error).
	if supportsRetract(merged.Go) {
		for _, r := range pubMF.Retract {
			if err := merged.AddRetract(r.VersionInterval, r.Rationale); err != nil {
				return nil, fmt.Errorf("adding retract: %w", err)
			}
		}
	}

	merged.Cleanup()
	return merged.Format()
}

// supportsRetract reports whether the merged go directive permits retract
// directives. Retracts require go 1.16+. A missing go directive is treated as
// unsupported.
func supportsRetract(g *modfile.Go) bool {
	if g == nil {
		return false
	}
	parts := strings.SplitN(g.Version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major > 1 {
		return true
	}
	return major == 1 && minor >= 16
}

// isLocalPathReplace reports whether a replace directive's target is a local
// path (relative or absolute filesystem path) rather than a module identifier.
func isLocalPathReplace(target string) bool {
	return strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/")
}
